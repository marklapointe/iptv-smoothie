package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/mlapointe/smoothie/internal/hub"
	"github.com/mlapointe/smoothie/internal/ratelimit"
	"github.com/mlapointe/smoothie/internal/source"
	"github.com/mlapointe/smoothie/internal/store"
)

// Manager coordinates upstream pools and live fan-out.
type Manager struct {
	DB     *store.DB
	Hub    *hub.Hub
	Client *http.Client

	mu    sync.Mutex
	pools map[string]*ratelimit.Pool // sourceID -> pool
}

// New creates a stream manager.
func New(db *store.DB) *Manager {
	return &Manager{
		DB:  db,
		Hub: hub.New(hub.Options{IdleGrace: 3 * time.Second}),
		Client: &http.Client{
			// No global timeout — streams are long-lived; use context cancel.
			Timeout: 0,
		},
		pools: make(map[string]*ratelimit.Pool),
	}
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

// OpenVOD proxies a VOD stream with optional rate limit (progressive).
// Caller must Close resp.Body; that releases the upstream pool lease.
func (m *Manager) OpenVOD(ctx context.Context, ch *store.Channel, rangeHeader string) (*http.Response, error) {
	src, err := m.DB.GetSource(ch.SourceID)
	if err != nil {
		return nil, err
	}
	pool := m.poolFor(src)
	lease, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("stream: no upstream slot: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ch.StreamURL, nil)
	if err != nil {
		lease.Release()
		return nil, err
	}
	req.Header.Set("User-Agent", "Smoothie/0.1")
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
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

	body := io.ReadCloser(resp.Body)
	var bps int64
	var lim source.IPTVLimits
	if src.LimitsJSON != "" {
		_ = json.Unmarshal([]byte(src.LimitsJSON), &lim)
		bps = lim.MaxUpstreamBPS
	}
	if bps > 0 {
		body = &rateLimitedBody{r: body, lim: ratelimit.NewByteLimiter(bps)}
	}
	resp.Body = &leasedBody{ReadCloser: body, lease: lease}
	return resp, nil
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
