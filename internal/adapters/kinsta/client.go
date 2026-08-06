package kinsta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const baseURL = "https://api.kinsta.com/v2"

// MaxLogLines is the ceiling the Kinsta API documents for the lines parameter.
const MaxLogLines = 20000

// DefaultLogLines mirrors the API default.
const DefaultLogLines = 1000

type Client struct {
	apiKey    string
	companyID string
	http      *http.Client
	// logHTTP has a longer budget than http: a 20.000-line log file is several
	// megabytes, which does not reliably fit in the 15s the metadata calls use.
	logHTTP *http.Client
	// base overrides the API root in tests; empty means the real API.
	base string
}

func (c *Client) root() string {
	if c.base != "" {
		return c.base
	}
	return baseURL
}

func NewClient(apiKey, companyID string) *Client {
	return &Client{
		apiKey:    apiKey,
		companyID: companyID,
		http:      &http.Client{Timeout: 15 * time.Second},
		logHTTP:   &http.Client{Timeout: 90 * time.Second},
	}
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.getWith(ctx, c.http, path, out)
}

func (c *Client) getWith(ctx context.Context, hc *http.Client, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.root()+path, nil)
	if err != nil {
		return fmt.Errorf("kinsta request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := hc.Do(req)
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

// GetEnvironmentLogs returns the raw contents of one of the environment's log
// files. fileName is error, access or kinsta-cache-perf; lines is clamped to
// the documented 1..MaxLogLines range.
//
// The endpoint is rate limited to 35 requests per minute, so callers should not
// poll it. The returned string is the log as-is, newline separated: nginx's
// error.log format, with PHP messages wrapped inside "FastCGI sent in stderr".
func (c *Client) GetEnvironmentLogs(ctx context.Context, envID, fileName string, lines int) (string, error) {
	switch fileName {
	case "error", "access", "kinsta-cache-perf":
	default:
		return "", fmt.Errorf("onbekend logbestand %q (verwacht error, access of kinsta-cache-perf)", fileName)
	}
	if envID == "" {
		return "", fmt.Errorf("environment-id ontbreekt")
	}
	if lines <= 0 {
		lines = DefaultLogLines
	}
	if lines > MaxLogLines {
		lines = MaxLogLines
	}

	q := url.Values{}
	q.Set("file_name", fileName)
	q.Set("lines", strconv.Itoa(lines))

	var resp struct {
		Environment struct {
			ContainerInfo struct {
				Logs string `json:"logs"`
			} `json:"container_info"`
		} `json:"environment"`
	}
	path := "/sites/environments/" + url.PathEscape(envID) + "/logs?" + q.Encode()
	if err := c.getWith(ctx, c.logHTTP, path, &resp); err != nil {
		return "", err
	}
	return resp.Environment.ContainerInfo.Logs, nil
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
