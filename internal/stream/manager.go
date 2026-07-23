package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/mlapointe/smoothie/internal/cache"
	"github.com/mlapointe/smoothie/internal/hub"
	"github.com/mlapointe/smoothie/internal/ratelimit"
	"github.com/mlapointe/smoothie/internal/source"
	"github.com/mlapointe/smoothie/internal/store"
)

// Manager coordinates upstream pools, live fan-out, and VOD cache.
type Manager struct {
	DB     *store.DB
	Hub    *hub.Hub
	Cache  *cache.Cache
	Client *http.Client

	mu    sync.Mutex
	pools map[string]*ratelimit.Pool // sourceID -> pool
}

// Options configures the stream manager.
type Options struct {
	Cache *cache.Cache
}

// New creates a stream manager.
func New(db *store.DB, opts ...Options) *Manager {
	m := &Manager{
		DB:  db,
		Hub: hub.New(hub.Options{IdleGrace: 3 * time.Second}),
		Client: &http.Client{
			// No global timeout — streams are long-lived; use context cancel.
			Timeout: 0,
		},
		pools: make(map[string]*ratelimit.Pool),
	}
	if len(opts) > 0 {
		m.Cache = opts[0].Cache
	}
	return m
}

func (m *Manager) poolFor(src *store.Source) *ratelimit.Pool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.pools[src.ID]; ok {
		return p
	}
	max := 2
	var lim source.IPTVLimits
	if src.LimitsJSON != "" {
		_ = json.Unmarshal([]byte(src.LimitsJSON), &lim)
		if lim.MaxConcurrentUpstreams > 0 {
			max = lim.MaxConcurrentUpstreams
		}
	}
	p := ratelimit.NewPool(max)
	m.pools[src.ID] = p
	return p
}

// OpenLive returns a reader that joins the fan-out session for a live channel.
// One upstream is opened per channel key; concurrent clients share it.
func (m *Manager) OpenLive(ctx context.Context, ch *store.Channel) (io.ReadCloser, error) {
	src, err := m.DB.GetSource(ch.SourceID)
	if err != nil {
		return nil, err
	}
	pool := m.poolFor(src)
	key := ch.SourceID + ":" + ch.ID

	open := func() (io.ReadCloser, error) {
		lease, err := pool.Acquire(ctx)
		if err != nil {
			return nil, fmt.Errorf("stream: no upstream slot: %w", err)
		}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ch.StreamURL, nil)
		if err != nil {
			lease.Release()
			return nil, err
		}
		req.Header.Set("User-Agent", "Smoothie/0.1")
		resp, err := m.Client.Do(req)
		if err != nil {
			lease.Release()
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_ = resp.Body.Close()
			lease.Release()
			return nil, fmt.Errorf("stream: upstream status %d", resp.StatusCode)
		}
		return &leasedBody{ReadCloser: resp.Body, lease: lease}, nil
	}

	sub, err := m.Hub.Subscribe(ctx, key, open)
	if err != nil {
		return nil, err
	}
	return sub, nil
}

// VODResult is a progressive VOD body (cache hit or upstream fill).
type VODResult struct {
	Body          io.ReadCloser
	ContentType   string
	ContentLength int64 // -1 if unknown
	ContentRange  string
	CacheHit      bool
	StatusCode    int
}

// OpenVOD serves VOD via local cache when possible (progressive fill on miss).
// Range on a validated cache file is served locally (no upstream).
func (m *Manager) OpenVOD(ctx context.Context, ch *store.Channel, rangeHeader string) (*VODResult, error) {
	ext := guessExt(ch.StreamURL, ch.Name)
	key := ch.SourceID + ":" + ch.ID

	if m.Cache != nil {
		if o, err := m.Cache.Get(key); err == nil && o.State == cache.StateValidated {
			// Full file or Range from disk
			if rangeHeader == "" {
				r, err := m.openCachedFile(o.Path)
				if err == nil {
					return &VODResult{
						Body: r, ContentType: contentTypeForExt(ext),
						ContentLength: o.Size, CacheHit: true, StatusCode: http.StatusOK,
					}, nil
				}
			} else if rng, ok := parseRangeHeader(rangeHeader, o.Size); ok {
				r, length, cr, err := openFileRange(o.Path, rng, o.Size)
				if err == nil {
					return &VODResult{
						Body: r, ContentType: contentTypeForExt(ext),
						ContentLength: length, ContentRange: cr,
						CacheHit: true, StatusCode: http.StatusPartialContent,
					}, nil
				}
			}
		}

		// Miss / incomplete: progressive fill only when no Range
		if rangeHeader == "" {
			src, err := m.DB.GetSource(ch.SourceID)
			if err != nil {
				return nil, err
			}
			pool := m.poolFor(src)
			lease, err := pool.Acquire(ctx)
			if err != nil {
				return nil, fmt.Errorf("stream: no upstream slot: %w", err)
			}

			upBody, expected, ct, err := m.fetchUpstreamBody(ctx, ch, "", src)
			if err != nil {
				lease.Release()
				return nil, err
			}
			var lim source.IPTVLimits
			if src.LimitsJSON != "" {
				_ = json.Unmarshal([]byte(src.LimitsJSON), &lim)
			}
			body := io.ReadCloser(upBody)
			if lim.MaxUpstreamBPS > 0 {
				body = &rateLimitedBody{r: body, lim: ratelimit.NewByteLimiter(lim.MaxUpstreamBPS)}
			}
			body = &leasedBody{ReadCloser: body, lease: lease}

			_, reader, err := m.Cache.OpenOrFill(ctx, key, body, expected, ext)
			if err != nil {
				_ = body.Close()
				return nil, err
			}
			if ct == "" {
				ct = contentTypeForExt(ext)
			}
			return &VODResult{
				Body: reader, ContentType: ct, ContentLength: expected,
				CacheHit: false, StatusCode: http.StatusOK,
			}, nil
		}
	}

	// Direct / Range proxy (no cache tee or incomplete cache + Range)
	return m.openVODDirect(ctx, ch, rangeHeader)
}

func (m *Manager) openVODDirect(ctx context.Context, ch *store.Channel, rangeHeader string) (*VODResult, error) {
	src, err := m.DB.GetSource(ch.SourceID)
	if err != nil {
		return nil, err
	}
	pool := m.poolFor(src)
	lease, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("stream: no upstream slot: %w", err)
	}
	body, expected, ct, err := m.fetchUpstreamBody(ctx, ch, rangeHeader, src)
	if err != nil {
		lease.Release()
		return nil, err
	}
	var lim source.IPTVLimits
	if src.LimitsJSON != "" {
		_ = json.Unmarshal([]byte(src.LimitsJSON), &lim)
	}
	r := io.ReadCloser(body)
	if lim.MaxUpstreamBPS > 0 {
		r = &rateLimitedBody{r: r, lim: ratelimit.NewByteLimiter(lim.MaxUpstreamBPS)}
	}
	r = &leasedBody{ReadCloser: r, lease: lease}
	status := http.StatusOK
	if rangeHeader != "" {
		status = http.StatusPartialContent
	}
	if ct == "" {
		ct = contentTypeForExt(guessExt(ch.StreamURL, ch.Name))
	}
	return &VODResult{Body: r, ContentType: ct, ContentLength: expected, StatusCode: status}, nil
}

func (m *Manager) fetchUpstreamBody(ctx context.Context, ch *store.Channel, rangeHeader string, src *store.Source) (io.ReadCloser, int64, string, error) {
	_ = src
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ch.StreamURL, nil)
	if err != nil {
		return nil, -1, "", err
	}
	req.Header.Set("User-Agent", "Smoothie/0.1")
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	resp, err := m.Client.Do(req)
	if err != nil {
		return nil, -1, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, -1, "", fmt.Errorf("stream: upstream status %d", resp.StatusCode)
	}
	var expected int64 = -1
	if resp.ContentLength > 0 {
		expected = resp.ContentLength
	}
	return resp.Body, expected, resp.Header.Get("Content-Type"), nil
}

func (m *Manager) openCachedFile(path string) (io.ReadCloser, error) {
	// use progressive open helper via cache package by reading file
	return openFile(path)
}

func guessExt(url, name string) string {
	u := strings.ToLower(url)
	for _, ext := range []string{".mkv", ".mp4", ".avi", ".m4v", ".mov", ".ts"} {
		if strings.Contains(u, ext) {
			return ext
		}
	}
	if e := path.Ext(name); e != "" {
		return e
	}
	return ".mp4"
}

func contentTypeForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".mkv":
		return "video/x-matroska"
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".ts":
		return "video/mp2t"
	default:
		return "application/octet-stream"
	}
}

// ActiveLiveSessions reports fan-out session count.
func (m *Manager) ActiveLiveSessions() int {
	return m.Hub.ActiveSessions()
}

type leasedBody struct {
	io.ReadCloser
	lease *ratelimit.Lease
	once  sync.Once
}

func (b *leasedBody) Close() error {
	var err error
	b.once.Do(func() {
		err = b.ReadCloser.Close()
		if b.lease != nil {
			b.lease.Release()
		}
	})
	return err
}

type rateLimitedBody struct {
	r   io.ReadCloser
	lim *ratelimit.ByteLimiter
}

func (b *rateLimitedBody) Read(p []byte) (int, error) {
	n, err := b.lim.WaitN(context.Background(), len(p))
	if err != nil {
		return 0, err
	}
	return b.r.Read(p[:n])
}

func (b *rateLimitedBody) Close() error {
	return b.r.Close()
}
