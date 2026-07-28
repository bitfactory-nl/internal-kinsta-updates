package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestActionsClient(t *testing.T, handler http.HandlerFunc) *ActionsClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewActionsClient("test-token")
	c.baseURL = srv.URL
	return c
}

func TestListWorkflows(t *testing.T) {
	const body = `{"workflows":[
		{"id":123,"name":"Check Updates","path":".github/workflows/update.yml","state":"active"},
		{"id":456,"name":"Old Workflow","path":".github/workflows/old.yml","state":"disabled_manually"}
	]}`

	var gotPath, gotAuth, gotAccept string
	c := newTestActionsClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		_, _ = w.Write([]byte(body))
	})

	workflows, err := c.ListWorkflows(context.Background(), "acme/repo")
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(workflows) != 2 {
		t.Fatalf("got %d workflows, want 2", len(workflows))
	}
	if workflows[0].ID != 123 || workflows[0].Name != "Check Updates" || workflows[0].State != "active" {
		t.Errorf("workflow[0] = %+v", workflows[0])
	}
	if !strings.HasSuffix(gotPath, "/repos/acme/repo/actions/workflows") {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Errorf("Accept = %q", gotAccept)
	}
}

func TestListWorkflowsError(t *testing.T) {
	c := newTestActionsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if _, err := c.ListWorkflows(context.Background(), "acme/repo"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestLatestRun(t *testing.T) {
	const body = `{"workflow_runs":[
		{"status":"completed","conclusion":"success","html_url":"https://github.com/acme/repo/actions/runs/1","created_at":"2026-01-15T10:00:00Z"}
	]}`

	var gotPath string
	c := newTestActionsClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(body))
	})

	run, err := c.LatestRun(context.Background(), "acme/repo", 123)
	if err != nil {
		t.Fatalf("LatestRun: %v", err)
	}
	if run == nil {
		t.Fatal("expected a run, got nil")
	}
	if run.Status != "completed" || run.Conclusion != "success" {
		t.Errorf("run = %+v", run)
	}
	if !strings.HasSuffix(gotPath, "/repos/acme/repo/actions/workflows/123/runs") {
		t.Errorf("path = %q", gotPath)
	}
}

func TestLatestRunNoRuns(t *testing.T) {
	c := newTestActionsClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"workflow_runs":[]}`))
	})

	run, err := c.LatestRun(context.Background(), "acme/repo", 123)
	if err != nil {
		t.Fatalf("LatestRun: %v", err)
	}
	if run != nil {
		t.Errorf("expected nil run when no runs exist, got %+v", run)
	}
}

func TestDefaultBranch(t *testing.T) {
	c := newTestActionsClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"default_branch":"main"}`))
	})

	branch, err := c.DefaultBranch(context.Background(), "acme/repo")
	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}
	if branch != "main" {
		t.Errorf("branch = %q, want main", branch)
	}
}

func TestDispatchWorkflowSuccess(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody dispatchRequest
	c := newTestActionsClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusNoContent)
	})

	err := c.DispatchWorkflow(context.Background(), "acme/repo", 123, "main")
	if err != nil {
		t.Fatalf("DispatchWorkflow: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if !strings.HasSuffix(gotPath, "/repos/acme/repo/actions/workflows/123/dispatches") {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody.Ref != "main" {
		t.Errorf("body ref = %q, want main", gotBody.Ref)
	}
}

func TestDispatchWorkflowNoTrigger(t *testing.T) {
	c := newTestActionsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	})

	err := c.DispatchWorkflow(context.Background(), "acme/repo", 123, "main")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "handmatige start") {
		t.Errorf("error = %q, want Dutch 422 message", err.Error())
	}
}

func TestBranchSHA(t *testing.T) {
	var gotPath, gotAuth string
	c := newTestActionsClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"sha":"61b9c727b447e5f446c40c58f1492b790a84a98a","commit":{}}`))
	})

	sha, err := c.BranchSHA(context.Background(), "bitfactory-nl/web-afcnl", "release/1.0.x")
	if err != nil {
		t.Fatalf("BranchSHA: %v", err)
	}
	if sha != "61b9c727b447e5f446c40c58f1492b790a84a98a" {
		t.Errorf("sha = %q", sha)
	}
	if gotPath != "/repos/bitfactory-nl/web-afcnl/commits/release/1.0.x" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(gotAuth, "test-token") {
		t.Errorf("Authorization ontbreekt: %q", gotAuth)
	}
}

func TestBranchSHAFoutBijOnbekendeBranch(t *testing.T) {
	c := newTestActionsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	})
	if _, err := c.BranchSHA(context.Background(), "o/r", "release/9.9.x"); err == nil {
		t.Fatal("verwachtte een fout bij 404")
	}
}
