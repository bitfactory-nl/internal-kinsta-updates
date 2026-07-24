// internal/adapters/wporg/client.go
package wporg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ErrNotFound means the slug is unknown on wp.org (likely a premium/custom plugin).
var ErrNotFound = errors.New("plugin not found on wp.org")

const infoBaseURL = "https://api.wordpress.org"

type Client struct {
	BaseURL string
	http    *http.Client
}

func NewClient() *Client {
	return &Client{BaseURL: infoBaseURL, http: &http.Client{Timeout: 60 * time.Second}}
}

type infoResponse struct {
	Version      string `json:"version"`
	DownloadLink string `json:"download_link"`
}

// LatestVersion returns the latest stable version and its zip download URL.
func (c *Client) LatestVersion(ctx context.Context, slug string) (string, string, error) {
	url := fmt.Sprintf("%s/plugins/info/1.0/%s.json", c.BaseURL, slug)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("wporg info: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}
	// wp.org responds with the JSON literal `null` for unknown slugs.
	if resp.StatusCode == http.StatusNotFound || string(body) == "null" || len(body) == 0 {
		return "", "", ErrNotFound
	}
	var info infoResponse
	if err := json.Unmarshal(body, &info); err != nil {
		return "", "", fmt.Errorf("wporg info parse: %w", err)
	}
	if info.Version == "" || info.DownloadLink == "" {
		return "", "", ErrNotFound
	}
	return info.Version, info.DownloadLink, nil
}

// Download fetches the plugin zip bytes.
func (c *Client) Download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wporg download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wporg download: status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
