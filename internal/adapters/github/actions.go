package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ActionsClient reads GitHub Actions artifacts from arbitrary repositories.
type ActionsClient struct {
	token   string
	baseURL string
	http    *http.Client
}

// NewActionsClient builds a client authenticated with a personal access token.
func NewActionsClient(token string) *ActionsClient {
	return &ActionsClient{
		token:   token,
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// Artifact is a GitHub Actions workflow artifact.
type Artifact struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Expired   bool      `json:"expired"`
	CreatedAt time.Time `json:"created_at"`
}

type artifactsResponse struct {
	Artifacts []Artifact `json:"artifacts"`
}

// ListArtifacts returns the most recent artifacts of repo ("org/name"), newest first.
func (c *ActionsClient) ListArtifacts(ctx context.Context, repo string) ([]Artifact, error) {
	url := fmt.Sprintf("%s/repos/%s/actions/artifacts?per_page=50", c.baseURL, repo)
	body, err := c.get(ctx, url, "application/vnd.github+json")
	if err != nil {
		return nil, fmt.Errorf("list artifacts %s: %w", repo, err)
	}

	var resp artifactsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse artifacts %s: %w", repo, err)
	}
	return resp.Artifacts, nil
}

// DownloadArtifact returns the zip bytes of an artifact.
func (c *ActionsClient) DownloadArtifact(ctx context.Context, repo string, id int64) ([]byte, error) {
	url := fmt.Sprintf("%s/repos/%s/actions/artifacts/%d/zip", c.baseURL, repo, id)
	body, err := c.get(ctx, url, "application/vnd.github+json")
	if err != nil {
		return nil, fmt.Errorf("download artifact %d van %s: %w", id, repo, err)
	}
	return body, nil
}

func (c *ActionsClient) get(ctx context.Context, url, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github api error: status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
