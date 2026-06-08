// Package github reads the private paid-plugin repository: a manifest of plugin
// versions plus the plugin zip files referenced by that manifest.
package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
	"gopkg.in/yaml.v3"
)

const (
	defaultBaseURL  = "https://api.github.com"
	defaultRef      = "main"
	manifestPath    = "manifest.yml"
	rawAcceptHeader = "application/vnd.github.raw"
)

// Client reads files from a single private GitHub repository over the REST API.
type Client struct {
	token   string
	repo    string // "org/repo-name"
	ref     string // branch or tag
	baseURL string
	http    *http.Client
}

// NewClient builds a client for repo (format "org/repo-name"). An empty ref
// defaults to "main".
func NewClient(token, repo, ref string) *Client {
	if ref == "" {
		ref = defaultRef
	}
	return &Client{
		token:   token,
		repo:    repo,
		ref:     ref,
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// manifestFile is the on-disk shape of manifest.yml in the plugin repo.
type manifestFile struct {
	Plugins []manifestEntry `yaml:"plugins"`
}

type manifestEntry struct {
	Slug          string    `yaml:"slug"`
	DisplayName   string    `yaml:"display_name"`
	LatestVersion string    `yaml:"latest_version"`
	ZipPath       string    `yaml:"zip_path"`
	UpdatedAt     time.Time `yaml:"updated_at"`
}

// GetManifest fetches and parses manifest.yml, returning one PaidPlugin per entry.
func (c *Client) GetManifest(ctx context.Context) ([]domain.PaidPlugin, error) {
	raw, err := c.getRawFile(ctx, manifestPath)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}

	var mf manifestFile
	if err := yaml.Unmarshal(raw, &mf); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	plugins := make([]domain.PaidPlugin, 0, len(mf.Plugins))
	for _, e := range mf.Plugins {
		plugins = append(plugins, domain.PaidPlugin{
			Slug:          e.Slug,
			DisplayName:   e.DisplayName,
			LatestVersion: e.LatestVersion,
			ZipPath:       e.ZipPath,
			UpdatedAt:     e.UpdatedAt,
			DownloadURL:   c.contentsURL(e.ZipPath),
		})
	}
	return plugins, nil
}

// DownloadZip returns the raw bytes of a plugin zip at the given repo path.
func (c *Client) DownloadZip(ctx context.Context, path string) ([]byte, error) {
	raw, err := c.getRawFile(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("download zip %q: %w", path, err)
	}
	return raw, nil
}

// getRawFile fetches a repo file's raw bytes via the contents API.
func (c *Client) getRawFile(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.contentsURL(path), nil)
	if err != nil {
		return nil, fmt.Errorf("github request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", rawAcceptHeader)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github api error: status %d voor %s", resp.StatusCode, path)
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) contentsURL(path string) string {
	return fmt.Sprintf("%s/repos/%s/contents/%s?ref=%s",
		c.baseURL, c.repo, path, url.QueryEscape(c.ref))
}
