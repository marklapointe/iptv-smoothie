package emby

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to Emby Server with an API key (Premiere/admin key).
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// MediaFolder is a library root from Emby.
type MediaFolder struct {
	Name string `json:"Name"`
	Id   string `json:"Id"`
	Path string `json:"Path"`
}

// New builds a client. baseURL like http://emby.lan:8096
func New(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) url(path string, q url.Values) string {
	u := c.BaseURL + path
	if q == nil {
		q = url.Values{}
	}
	if c.APIKey != "" {
		q.Set("api_key", c.APIKey)
	}
	if enc := q.Encode(); enc != "" {
		u += "?" + enc
	}
	return u
}

func (c *Client) do(ctx context.Context, method, path string, q url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.url(path, q), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Emby-Token", c.APIKey)
	return c.HTTP.Do(req)
}

// Ping hits /System/Info/Public or authenticated System/Info.
func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodGet, "/System/Info", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("emby: status %d", resp.StatusCode)
	}
	return nil
}

// MediaFolders lists library folders.
func (c *Client) MediaFolders(ctx context.Context) ([]MediaFolder, error) {
	resp, err := c.do(ctx, http.MethodGet, "/Library/MediaFolders", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("emby: media folders status %d", resp.StatusCode)
	}
	var wrap struct {
		Items []MediaFolder `json:"Items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrap); err != nil {
		// some builds return bare array
		return nil, err
	}
	return wrap.Items, nil
}

// RefreshLibrary starts a full library scan.
func (c *Client) RefreshLibrary(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodPost, "/Library/Refresh", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("emby: refresh status %d", resp.StatusCode)
	}
	return nil
}

// RefreshItem refreshes a specific item/folder by Emby id.
func (c *Client) RefreshItem(ctx context.Context, itemID string) error {
	q := url.Values{}
	q.Set("Recursive", "true")
	q.Set("ImageRefreshMode", "Default")
	q.Set("MetadataRefreshMode", "Default")
	resp, err := c.do(ctx, http.MethodPost, "/Items/"+url.PathEscape(itemID)+"/Refresh", q)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("emby: item refresh status %d", resp.StatusCode)
	}
	return nil
}
