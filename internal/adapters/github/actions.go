package github

import (
	"bytes"
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

// Workflow is a GitHub Actions workflow definition.
type Workflow struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	State string `json:"state"`
}

type workflowsResponse struct {
	Workflows []Workflow `json:"workflows"`
}

// ListWorkflows returns all workflows defined in repo ("org/name").
func (c *ActionsClient) ListWorkflows(ctx context.Context, repo string) ([]Workflow, error) {
	url := fmt.Sprintf("%s/repos/%s/actions/workflows", c.baseURL, repo)
	body, err := c.get(ctx, url, "application/vnd.github+json")
	if err != nil {
		return nil, fmt.Errorf("list workflows %s: %w", repo, err)
	}

	var resp workflowsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse workflows %s: %w", repo, err)
	}
	return resp.Workflows, nil
}

// WorkflowRun is a single run of a GitHub Actions workflow.
type WorkflowRun struct {
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
	CreatedAt  string `json:"created_at"`
}

type workflowRunsResponse struct {
	WorkflowRuns []WorkflowRun `json:"workflow_runs"`
}

// LatestRun returns the most recent run of workflowID in repo, or nil when the
// workflow has never run.
func (c *ActionsClient) LatestRun(ctx context.Context, repo string, workflowID int64) (*WorkflowRun, error) {
	url := fmt.Sprintf("%s/repos/%s/actions/workflows/%d/runs?per_page=1", c.baseURL, repo, workflowID)
	body, err := c.get(ctx, url, "application/vnd.github+json")
	if err != nil {
		return nil, fmt.Errorf("latest run workflow %d van %s: %w", workflowID, repo, err)
	}

	var resp workflowRunsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse workflow runs %d van %s: %w", workflowID, repo, err)
	}
	if len(resp.WorkflowRuns) == 0 {
		return nil, nil
	}
	return &resp.WorkflowRuns[0], nil
}

type repoResponse struct {
	DefaultBranch string `json:"default_branch"`
}

// DefaultBranch returns the default branch of repo ("org/name").
func (c *ActionsClient) DefaultBranch(ctx context.Context, repo string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s", c.baseURL, repo)
	body, err := c.get(ctx, url, "application/vnd.github+json")
	if err != nil {
		return "", fmt.Errorf("default branch %s: %w", repo, err)
	}

	var resp repoResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse repo %s: %w", repo, err)
	}
	return resp.DefaultBranch, nil
}

// commitResponse is the subset of the commit endpoint we need.
type commitResponse struct {
	SHA string `json:"sha"`
}

// BranchSHA returns the head commit SHA of branch in repo ("org/name"). Used to
// see whether a local origin/<branch> ref is behind the remote, so only repos
// that actually moved need a git fetch.
func (c *ActionsClient) BranchSHA(ctx context.Context, repo, branch string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/commits/%s", c.baseURL, repo, branch)
	body, err := c.get(ctx, url, "application/vnd.github+json")
	if err != nil {
		return "", fmt.Errorf("branch sha %s@%s: %w", repo, branch, err)
	}

	var resp commitResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse commit %s@%s: %w", repo, branch, err)
	}
	if resp.SHA == "" {
		return "", fmt.Errorf("branch sha %s@%s: leeg antwoord", repo, branch)
	}
	return resp.SHA, nil
}

type dispatchRequest struct {
	Ref string `json:"ref"`
}

// DispatchWorkflow triggers a workflow_dispatch run of workflowID on ref.
// A 422 from the API means the workflow has no workflow_dispatch trigger.
func (c *ActionsClient) DispatchWorkflow(ctx context.Context, repo string, workflowID int64, ref string) error {
	url := fmt.Sprintf("%s/repos/%s/actions/workflows/%d/dispatches", c.baseURL, repo, workflowID)
	payload, err := json.Marshal(dispatchRequest{Ref: ref})
	if err != nil {
		return fmt.Errorf("marshal dispatch payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("github request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("github http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnprocessableEntity {
		return fmt.Errorf("workflow ondersteunt geen handmatige start")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github api error: status %d", resp.StatusCode)
	}
	return nil
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
