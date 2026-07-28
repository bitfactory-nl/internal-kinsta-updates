// internal/adapters/wporg/client.go
package wporg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
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

// LatestThemeVersion returns the latest version of a wp.org theme.
// Unknown slugs (custom/premium themes) yield ErrNotFound.
func (c *Client) LatestThemeVersion(ctx context.Context, slug string) (string, error) {
	reqURL := fmt.Sprintf("%s/themes/info/1.1/?action=theme_information&request[slug]=%s",
		c.BaseURL, neturl.QueryEscape(slug))
	body, status, err := c.get(ctx, reqURL)
	if err != nil {
		return "", fmt.Errorf("wporg theme info: %w", err)
	}
	// The themes API answers `false` (and older variants `null`) for unknown slugs.
	if status == http.StatusNotFound || string(body) == "false" || string(body) == "null" || len(body) == 0 {
		return "", ErrNotFound
	}
	var info struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return "", fmt.Errorf("wporg theme info parse: %w", err)
	}
	if info.Version == "" {
		return "", ErrNotFound
	}
	return info.Version, nil
}

// LatestCoreVersion returns the current WordPress core version.
func (c *Client) LatestCoreVersion(ctx context.Context) (string, error) {
	body, status, err := c.get(ctx, c.BaseURL+"/core/version-check/1.7/")
	if err != nil {
		return "", fmt.Errorf("wporg core version: %w", err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("wporg core version: status %d", status)
	}
	var resp struct {
		Offers []struct {
			Current string `json:"current"`
		} `json:"offers"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("wporg core version parse: %w", err)
	}
	if len(resp.Offers) == 0 || resp.Offers[0].Current == "" {
		return "", fmt.Errorf("wporg core version: geen offers in antwoord")
	}
	return resp.Offers[0].Current, nil
}

// CoreDownloadURL bouwt de download-URL voor de "no-content" zip van een
// WordPress core-versie (bevat wp-admin/wp-includes/rootbestanden, maar geen
// wp-content). Pure functie, dus zonder git of netwerk te testen.
// Een lege of whitespace-only version levert "" op; whitespace en een
// eventuele leading "v" (bijv. uit een tag als "v6.5.3") worden getrimd.
func CoreDownloadURL(version string) string {
	v := strings.TrimSpace(version)
	if v == "" {
		return ""
	}
	v = strings.TrimPrefix(v, "v")
	return fmt.Sprintf("https://wordpress.org/wordpress-%s-no-content.zip", v)
}

// get performs a GET and returns the body and status code.
func (c *Client) get(ctx context.Context, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
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
