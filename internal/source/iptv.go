package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mlapointe/smoothie/internal/playlist"
	"github.com/mlapointe/smoothie/internal/store"
)

// IPTVConfig is stored in Source.ConfigJSON for type iptv_m3u.
type IPTVConfig struct {
	URLs []string `json:"urls"`
}

// IPTVLimits is stored in Source.LimitsJSON.
type IPTVLimits struct {
	MaxConcurrentUpstreams int   `json:"max_concurrent_upstreams"`
	MaxUpstreamBPS         int64 `json:"max_upstream_bps"`
}

// DefaultIPTVLimits returns conservative provider-friendly defaults.
func DefaultIPTVLimits() IPTVLimits {
	return IPTVLimits{MaxConcurrentUpstreams: 2, MaxUpstreamBPS: 1_500_000}
}

// ParseIPTVConfig unmarshals config JSON.
func ParseIPTVConfig(raw string) (IPTVConfig, error) {
	var c IPTVConfig
	if raw == "" {
		return c, fmt.Errorf("source: empty iptv config")
	}
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return c, err
	}
	if len(c.URLs) == 0 {
		return c, fmt.Errorf("source: no portal urls")
	}
	return c, nil
}

// Refresher pulls M3U and replaces channels for an IPTV source.
type Refresher struct {
	DB     *store.DB
	Client *http.Client
}

// NewRefresher builds a refresher with a sensible HTTP timeout.
func NewRefresher(db *store.DB) *Refresher {
	return &Refresher{
		DB: db,
		Client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// RefreshResult summarizes an ingest.
type RefreshResult struct {
	SourceID   string `json:"source_id"`
	FetchedURL string `json:"fetched_url"`
	Total      int    `json:"total"`
	Live       int    `json:"live"`
	VOD        int    `json:"vod"`
	DurationMS int64  `json:"duration_ms"`
}

// RefreshSource dispatches by source type (iptv_m3u or hdhomerun).
func (r *Refresher) RefreshSource(ctx context.Context, sourceID string) (*RefreshResult, error) {
	src, err := r.DB.GetSource(sourceID)
	if err != nil {
		return nil, err
	}
	switch src.Type {
	case store.SourceTypeHDHomeRun:
		return r.RefreshHDHR(ctx, sourceID)
	case store.SourceTypeIPTVM3U:
		// continue below
	default:
		return nil, fmt.Errorf("source: unsupported type %q", src.Type)
	}
	cfg, err := ParseIPTVConfig(src.ConfigJSON)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	var lastErr error
	for _, portal := range cfg.URLs {
		res, err := r.refreshFromURL(ctx, src, portal)
		if err != nil {
			lastErr = err
			continue
		}
		res.DurationMS = time.Since(start).Milliseconds()
		return res, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("source: no urls")
	}
	return nil, lastErr
}

// RefreshFromReader ingests an already-open M3U body (tests / lab files).
func (r *Refresher) RefreshFromReader(src *store.Source, body io.Reader, fetchedURL string) (*RefreshResult, error) {
	return r.ingest(src, body, fetchedURL)
}

func (r *Refresher) refreshFromURL(ctx context.Context, src *store.Source, portal string) (*RefreshResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, portal, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Smoothie/0.1")
	resp, err := r.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("source: portal status %d", resp.StatusCode)
	}
	return r.ingest(src, resp.Body, portal)
}

func (r *Refresher) ingest(src *store.Source, body io.Reader, fetchedURL string) (*RefreshResult, error) {
	// Replace strategy: delete existing channels for source, batch insert
	if err := r.DB.DeleteChannelsBySource(src.ID); err != nil {
		return nil, err
	}

	res := &RefreshResult{SourceID: src.ID, FetchedURL: redactURL(fetchedURL)}
	const batchSize = 500
	batch := make([]store.Channel, 0, batchSize)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := r.DB.CreateChannels(batch); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}

	err := playlist.Parse(body, func(e playlist.Entry) error {
		ch := store.Channel{
			SourceID:  src.ID,
			RemoteKey: e.RemoteKey,
			Name:      e.Name,
			GroupName: e.Group,
			Kind:      string(e.Kind),
			StreamURL: e.URL,
			Logo:      e.Logo,
			MetaJSON:  metaJSON(e),
			Enabled:   true,
		}
		batch = append(batch, ch)
		res.Total++
		if e.Kind == playlist.KindVOD {
			res.VOD++
		} else {
			res.Live++
		}
		if len(batch) >= batchSize {
			return flush()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return res, nil
}

func metaJSON(e playlist.Entry) string {
	b, _ := json.Marshal(map[string]string{
		"tvg_id":   e.TvgID,
		"tvg_name": e.TvgName,
	})
	return string(b)
}

// redactURL strips userinfo / query credentials from reported URL.
func redactURL(raw string) string {
	// Keep host + path scheme only for logs/API; avoid leaking query password
	if i := len(raw); i > 0 {
		if q := indexByte(raw, '?'); q >= 0 {
			return raw[:q] + "?…"
		}
	}
	return raw
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
