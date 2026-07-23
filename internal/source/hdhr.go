package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/mlapointe/smoothie/internal/store"
)

// HDHRConfig is stored in Source.ConfigJSON for type hdhomerun.
type HDHRConfig struct {
	BaseURL string `json:"base_url"` // e.g. http://192.168.1.50
}

// HDHRDiscover is SiliconDust discover.json.
type HDHRDiscover struct {
	FriendlyName    string `json:"FriendlyName"`
	ModelNumber     string `json:"ModelNumber"`
	FirmwareVersion string `json:"FirmwareVersion"`
	DeviceID        string `json:"DeviceID"`
	BaseURL         string `json:"BaseURL"`
	LineupURL       string `json:"LineupURL"`
	TunerCount      int    `json:"TunerCount"`
}

// HDHRLineupEntry is one lineup.json row.
type HDHRLineupEntry struct {
	GuideNumber string `json:"GuideNumber"`
	GuideName   string `json:"GuideName"`
	URL         string `json:"URL"`
	HD          int    `json:"HD"`
}

// ParseHDHRConfig unmarshals HDHomeRun config.
func ParseHDHRConfig(raw string) (HDHRConfig, error) {
	var c HDHRConfig
	if raw == "" {
		return c, fmt.Errorf("source: empty hdhomerun config")
	}
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return c, err
	}
	if c.BaseURL == "" {
		return c, fmt.Errorf("source: base_url required")
	}
	return c, nil
}

// HDHRClient talks to a device.
type HDHRClient struct {
	HTTP *http.Client
}

func defaultHDHRHTTP() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

// Discover probes baseURL/discover.json.
func (c *HDHRClient) Discover(ctx context.Context, baseURL string) (*HDHRDiscover, error) {
	if c.HTTP == nil {
		c.HTTP = defaultHDHRHTTP()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trimSlash(baseURL)+"/discover.json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("hdhr: discover status %d", resp.StatusCode)
	}
	var d HDHRDiscover
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, err
	}
	return &d, nil
}

// Lineup fetches lineup.json.
func (c *HDHRClient) Lineup(ctx context.Context, baseURL string) ([]HDHRLineupEntry, error) {
	if c.HTTP == nil {
		c.HTTP = defaultHDHRHTTP()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trimSlash(baseURL)+"/lineup.json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("hdhr: lineup status %d", resp.StatusCode)
	}
	var list []HDHRLineupEntry
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	return list, nil
}

// RefreshHDHR replaces channels for an hdhomerun source from the device lineup.
func (r *Refresher) RefreshHDHR(ctx context.Context, sourceID string) (*RefreshResult, error) {
	src, err := r.DB.GetSource(sourceID)
	if err != nil {
		return nil, err
	}
	if src.Type != store.SourceTypeHDHomeRun {
		return nil, fmt.Errorf("source: not hdhomerun")
	}
	cfg, err := ParseHDHRConfig(src.ConfigJSON)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	cli := &HDHRClient{HTTP: r.Client}
	disc, err := cli.Discover(ctx, cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	lineup, err := cli.Lineup(ctx, cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	if err := r.DB.DeleteChannelsBySource(src.ID); err != nil {
		return nil, err
	}
	res := &RefreshResult{SourceID: src.ID, FetchedURL: cfg.BaseURL + "/lineup.json"}
	batch := make([]store.Channel, 0, 64)
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
	for _, e := range lineup {
		url := e.URL
		if url == "" {
			// common pattern
			url = fmt.Sprintf("%s:5004/auto/v%s", trimSlash(cfg.BaseURL), e.GuideNumber)
		}
		batch = append(batch, store.Channel{
			SourceID:  src.ID,
			RemoteKey: "hdhr-" + disc.DeviceID + "-" + e.GuideNumber,
			Name:      e.GuideName,
			GroupName: "OTA " + disc.FriendlyName,
			Kind:      store.ChannelKindLive,
			StreamURL: url,
			Enabled:   true,
			MetaJSON:  fmt.Sprintf(`{"guide_number":%q,"device_id":%q,"tuner_count":%d}`, e.GuideNumber, disc.DeviceID, disc.TunerCount),
		})
		res.Total++
		res.Live++
		if len(batch) >= 100 {
			if err := flush(); err != nil {
				return nil, err
			}
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	// stamp limits from tuner count if empty
	if src.LimitsJSON == "" && disc.TunerCount > 0 {
		src.LimitsJSON = fmt.Sprintf(`{"max_concurrent_upstreams":%d}`, disc.TunerCount)
		_ = r.DB.GORM().Model(src).Update("limits_json", src.LimitsJSON)
	}
	res.DurationMS = time.Since(start).Milliseconds()
	return res, nil
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
