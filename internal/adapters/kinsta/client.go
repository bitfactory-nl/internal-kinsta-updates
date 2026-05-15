package kinsta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const baseURL = "https://api.kinsta.com/v2"

type Client struct {
	apiKey    string
	companyID string
	http      *http.Client
}

func NewClient(apiKey, companyID string) *Client {
	return &Client{
		apiKey:    apiKey,
		companyID: companyID,
		http:      &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("kinsta request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("kinsta http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("kinsta api error: status %d voor %s", resp.StatusCode, path)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ListSites returns all sites for the configured company.
func (c *Client) ListSites(ctx context.Context) ([]Site, error) {
	var resp struct {
		Company struct {
			Sites []Site `json:"sites"`
		} `json:"company"`
	}
	if err := c.get(ctx, "/sites?company="+c.companyID, &resp); err != nil {
		return nil, err
	}
	if resp.Company.Sites == nil {
		return make([]Site, 0), nil
	}
	return resp.Company.Sites, nil
}

// GetSite fetches basic site info by ID.
func (c *Client) GetSite(ctx context.Context, siteID string) (*Site, error) {
	var resp struct {
		Site Site `json:"site"`
	}
	if err := c.get(ctx, "/sites/"+siteID, &resp); err != nil {
		return nil, err
	}
	return &resp.Site, nil
}

// GetEnvironments returns full environment details for a site, including PHP and WP versions.
func (c *Client) GetEnvironments(ctx context.Context, siteID string) ([]Environment, error) {
	var resp struct {
		Site struct {
			Environments []Environment `json:"environments"`
		} `json:"site"`
	}
	if err := c.get(ctx, "/sites/"+siteID+"/environments", &resp); err != nil {
		return nil, err
	}
	if resp.Site.Environments == nil {
		return make([]Environment, 0), nil
	}
	return resp.Site.Environments, nil
}

// GetEnvironmentPlugins returns plugins for an environment using the non-deprecated wp-plugins endpoint.
func (c *Client) GetEnvironmentPlugins(ctx context.Context, envID string) ([]Plugin, error) {
	var resp struct {
		Environment struct {
			Plugins struct {
				Items []Plugin `json:"items"`
			} `json:"plugins"`
		} `json:"environment"`
	}
	if err := c.get(ctx, "/sites/environments/"+envID+"/wp-plugins", &resp); err != nil {
		return nil, err
	}
	if resp.Environment.Plugins.Items == nil {
		return make([]Plugin, 0), nil
	}
	return resp.Environment.Plugins.Items, nil
}

// GetEnvironmentThemes returns themes for an environment using the non-deprecated wp-themes endpoint.
func (c *Client) GetEnvironmentThemes(ctx context.Context, envID string) ([]Theme, error) {
	var resp struct {
		Environment struct {
			Themes struct {
				Items []Theme `json:"items"`
			} `json:"themes"`
		} `json:"environment"`
	}
	if err := c.get(ctx, "/sites/environments/"+envID+"/wp-themes", &resp); err != nil {
		return nil, err
	}
	if resp.Environment.Themes.Items == nil {
		return make([]Theme, 0), nil
	}
	return resp.Environment.Themes.Items, nil
}
