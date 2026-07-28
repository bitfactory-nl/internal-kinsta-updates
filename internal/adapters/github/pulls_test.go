package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestPullClient(t *testing.T, handler http.HandlerFunc) *PullClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewPullClient("test-token")
	c.baseURL = srv.URL
	return c
}

// --- ParseRepoFromRemote ---

func TestParseRepoFromRemote(t *testing.T) {
	tests := []struct {
		name      string
		remote    string
		wantOwner string
		wantRepo  string
	}{
		{
			name:      "scp-achtige ssh-vorm",
			remote:    "git@github.com:bitfactory-nl/web-afcnl.git",
			wantOwner: "bitfactory-nl",
			wantRepo:  "web-afcnl",
		},
		{
			name:      "https met .git-suffix",
			remote:    "https://github.com/bitfactory-nl/web-afcnl.git",
			wantOwner: "bitfactory-nl",
			wantRepo:  "web-afcnl",
		},
		{
			name:      "https zonder .git-suffix",
			remote:    "https://github.com/bitfactory-nl/web-afcnl",
			wantOwner: "bitfactory-nl",
			wantRepo:  "web-afcnl",
		},
		{
			name:      "ssh:// URL-vorm",
			remote:    "ssh://git@github.com/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "trailing slash en omringende whitespace",
			remote:    "  https://github.com/bitfactory-nl/web-afcnl.git/ \n",
			wantOwner: "bitfactory-nl",
			wantRepo:  "web-afcnl",
		},
		{
			name:      "scp-achtig met trailing slash",
			remote:    "git@github.com:bitfactory-nl/web-afcnl.git/",
			wantOwner: "bitfactory-nl",
			wantRepo:  "web-afcnl",
		},
		{
			name:      "hoofdlettergevoeligheid host",
			remote:    "https://GitHub.com/bitfactory-nl/web-afcnl.git",
			wantOwner: "bitfactory-nl",
			wantRepo:  "web-afcnl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := ParseRepoFromRemote(tt.remote)
			if err != nil {
				t.Fatalf("ParseRepoFromRemote(%q): onverwachte fout: %v", tt.remote, err)
			}
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("ParseRepoFromRemote(%q) = (%q, %q), want (%q, %q)", tt.remote, owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

func TestParseRepoFromRemoteErrors(t *testing.T) {
	tests := []struct {
		name   string
		remote string
	}{
		{name: "lege string", remote: ""},
		{name: "alleen whitespace", remote: "   "},
		{name: "niet-github host via https", remote: "https://gitlab.com/owner/repo.git"},
		{name: "niet-github host via scp-vorm", remote: "git@gitlab.com:owner/repo.git"},
		{name: "onparseerbare tekst", remote: "dit is geen remote-url"},
		{name: "ontbrekende repo in pad", remote: "https://github.com/onlyowner"},
		{name: "scheme zonder host of pad", remote: "https://"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := ParseRepoFromRemote(tt.remote)
			if err == nil {
				t.Fatalf("ParseRepoFromRemote(%q): verwachtte een fout, kreeg owner=%q repo=%q", tt.remote, owner, repo)
			}
		})
	}
}

// --- FindOpenPull ---

func TestFindOpenPullEmpty(t *testing.T) {
	var gotPath, gotAuth string
	var gotQuery = map[string]string{}
	c := newTestPullClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotQuery["state"] = r.URL.Query().Get("state")
		gotQuery["head"] = r.URL.Query().Get("head")
		_, _ = w.Write([]byte(`[]`))
	})

	pr, err := c.FindOpenPull(context.Background(), "acme", "repo", "update/wordpress-7.0.2")
	if err != nil {
		t.Fatalf("FindOpenPull: onverwachte fout: %v", err)
	}
	if pr != nil {
		t.Fatalf("pr = %+v, want nil zonder open PR", pr)
	}
	if !strings.HasSuffix(gotPath, "/repos/acme/repo/pulls") {
		t.Errorf("path = %q", gotPath)
	}
	if gotQuery["state"] != "open" {
		t.Errorf("state-query = %q, want open", gotQuery["state"])
	}
	if gotQuery["head"] != "acme:update/wordpress-7.0.2" {
		t.Errorf("head-query = %q, want acme:update/wordpress-7.0.2", gotQuery["head"])
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

func TestFindOpenPullFound(t *testing.T) {
	const body = `[
		{"number":42,"html_url":"https://github.com/acme/repo/pull/42","state":"open"},
		{"number":41,"html_url":"https://github.com/acme/repo/pull/41","state":"open"}
	]`
	var gotAuth, gotAccept string
	c := newTestPullClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		_, _ = w.Write([]byte(body))
	})

	pr, err := c.FindOpenPull(context.Background(), "acme", "repo", "update/wordpress-7.0.2")
	if err != nil {
		t.Fatalf("FindOpenPull: onverwachte fout: %v", err)
	}
	if pr == nil {
		t.Fatal("pr = nil, want de eerste open PR")
	}
	if pr.Number != 42 || pr.HTMLURL != "https://github.com/acme/repo/pull/42" || pr.State != "open" {
		t.Errorf("pr = %+v", pr)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Errorf("Accept = %q", gotAccept)
	}
}

func TestFindOpenPullHTTPError(t *testing.T) {
	c := newTestPullClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	})

	if _, err := c.FindOpenPull(context.Background(), "acme", "repo", "head"); err == nil {
		t.Fatal("verwachtte een fout, kreeg nil")
	}
}

// --- CreatePull ---

func TestCreatePullSuccess(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody struct {
		Title string `json:"title"`
		Head  string `json:"head"`
		Base  string `json:"base"`
		Body  string `json:"body"`
	}
	c := newTestPullClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":42,"html_url":"https://github.com/acme/repo/pull/42","state":"open"}`))
	})

	pr, err := c.CreatePull(context.Background(), "acme", "repo", "update/wordpress-7.0.2", "release/1.0.x", "WordPress core 7.0.2", "Automatische update.")
	if err != nil {
		t.Fatalf("CreatePull: onverwachte fout: %v", err)
	}
	if pr == nil || pr.Number != 42 || pr.HTMLURL != "https://github.com/acme/repo/pull/42" || pr.State != "open" {
		t.Errorf("pr = %+v", pr)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if !strings.HasSuffix(gotPath, "/repos/acme/repo/pulls") {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotBody.Title != "WordPress core 7.0.2" || gotBody.Head != "update/wordpress-7.0.2" || gotBody.Base != "release/1.0.x" || gotBody.Body != "Automatische update." {
		t.Errorf("request body = %+v", gotBody)
	}
}

func TestCreatePull422WithMessage(t *testing.T) {
	tests := []struct {
		name        string
		respBody    string
		wantMessage string
	}{
		{
			name: "pr bestaat al",
			respBody: `{
				"message": "Validation Failed",
				"errors": [
					{"resource":"PullRequest","code":"custom","message":"A pull request already exists for acme:update/wordpress-7.0.2."}
				],
				"documentation_url": "https://docs.github.com/rest/pulls/pulls#create-a-pull-request"
			}`,
			wantMessage: "A pull request already exists for acme:update/wordpress-7.0.2.",
		},
		{
			name: "geen commits tussen branches",
			respBody: `{
				"message": "Validation Failed",
				"errors": [
					{"resource":"PullRequest","code":"custom","message":"No commits between release/1.0.x and update/wordpress-7.0.2"}
				],
				"documentation_url": "https://docs.github.com/rest/pulls/pulls#create-a-pull-request"
			}`,
			wantMessage: "No commits between release/1.0.x and update/wordpress-7.0.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestPullClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(tt.respBody))
			})

			pr, err := c.CreatePull(context.Background(), "acme", "repo", "head", "base", "titel", "body")
			if err == nil {
				t.Fatal("verwachtte een fout, kreeg nil")
			}
			if pr != nil {
				t.Errorf("pr = %+v, want nil bij 422", pr)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf("error = %q, wil bericht %q bevatten", err.Error(), tt.wantMessage)
			}
		})
	}
}

func TestCreatePullOtherHTTPError(t *testing.T) {
	c := newTestPullClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("interne serverfout"))
	})

	_, err := c.CreatePull(context.Background(), "acme", "repo", "head", "base", "titel", "body")
	if err == nil {
		t.Fatal("verwachtte een fout, kreeg nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, wil statuscode 500 bevatten", err.Error())
	}
	if !strings.Contains(err.Error(), "interne serverfout") {
		t.Errorf("error = %q, wil de body-snippet bevatten", err.Error())
	}
}
