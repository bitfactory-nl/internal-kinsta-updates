package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestOrgClient(t *testing.T, handler http.HandlerFunc) *OrgClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewOrgClient("test-token")
	c.baseURL = srv.URL
	return c
}

// makeRepoPage bouwt n OrgRepo-items programmatisch, met een oplopende naam
// zodat elke pagina onderscheidbaar is.
func makeRepoPage(n int, prefix string) []OrgRepo {
	repos := make([]OrgRepo, 0, n)
	for i := 0; i < n; i++ {
		repos = append(repos, OrgRepo{
			Name:          fmt.Sprintf("%s-repo-%03d", prefix, i),
			FullName:      fmt.Sprintf("bitfactory-nl/%s-repo-%03d", prefix, i),
			HTMLURL:       fmt.Sprintf("https://github.com/bitfactory-nl/%s-repo-%03d", prefix, i),
			DefaultBranch: "main",
			PushedAt:      "2026-01-15T10:00:00Z",
		})
	}
	return repos
}

// --- ListOrgRepos ---

func TestListOrgReposPaginates(t *testing.T) {
	page1 := makeRepoPage(100, "p1")
	page2 := makeRepoPage(3, "p2")

	var gotAuth []string
	var gotPages []string
	var gotPerPage []string
	var gotSort []string

	c := newTestOrgClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/bitfactory-nl/repos" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		gotAuth = append(gotAuth, r.Header.Get("Authorization"))
		gotPages = append(gotPages, r.URL.Query().Get("page"))
		gotPerPage = append(gotPerPage, r.URL.Query().Get("per_page"))
		gotSort = append(gotSort, r.URL.Query().Get("sort"))

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			json.NewEncoder(w).Encode(page1)
		case "2":
			json.NewEncoder(w).Encode(page2)
		default:
			t.Fatalf("onverwachte pagina: %s", r.URL.Query().Get("page"))
		}
	})

	repos, err := c.ListOrgRepos(context.Background(), "bitfactory-nl")
	if err != nil {
		t.Fatalf("ListOrgRepos error: %v", err)
	}
	if len(repos) != 103 {
		t.Fatalf("len(repos) = %d, want 103", len(repos))
	}

	if want := []string{"1", "2"}; !equalStrSlices(gotPages, want) {
		t.Fatalf("pages = %v, want %v", gotPages, want)
	}
	for _, pp := range gotPerPage {
		if pp != "100" {
			t.Fatalf("per_page = %q, want 100", pp)
		}
	}
	for _, s := range gotSort {
		if s != "full_name" {
			t.Fatalf("sort = %q, want full_name", s)
		}
	}
	for _, a := range gotAuth {
		if a != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want Bearer test-token", a)
		}
	}
}

func TestListOrgReposEmptyOrgNoHTTPCall(t *testing.T) {
	called := false
	c := newTestOrgClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	_, err := c.ListOrgRepos(context.Background(), "")
	if err == nil {
		t.Fatal("verwachtte fout bij lege org, kreeg nil")
	}
	if called {
		t.Fatal("verwachtte geen HTTP-call bij lege org")
	}
}

func TestListOrgReposNonOKStatus(t *testing.T) {
	c := newTestOrgClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"rate limited"}`))
	})

	_, err := c.ListOrgRepos(context.Background(), "bitfactory-nl")
	if err == nil {
		t.Fatal("verwachtte fout bij 403, kreeg nil")
	}
}

func TestListOrgReposPageCeiling(t *testing.T) {
	// Handler geeft altijd een volle pagina van 100 items terug, dus de
	// paginatielus mag nooit vanzelf stoppen — dit test het plafond.
	full := makeRepoPage(100, "full")

	c := newTestOrgClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(full)
	})

	_, err := c.ListOrgRepos(context.Background(), "bitfactory-nl")
	if err == nil {
		t.Fatal("verwachtte fout bij bereikt paginaplafond, kreeg nil")
	}
}

// --- GetContentsRaw ---

func TestGetContentsRawHappyPath(t *testing.T) {
	const want = `{"provider":"kinsta","site_id":"abc123"}`

	var gotAccept string
	var gotRefPresent bool
	c := newTestOrgClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/bitfactory-nl/web-afcnl/contents/deploy_conf.json" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		gotAccept = r.Header.Get("Accept")
		gotRefPresent = r.URL.Query().Has("ref")
		w.Write([]byte(want))
	})

	got, err := c.GetContentsRaw(context.Background(), "bitfactory-nl", "web-afcnl", "deploy_conf.json", "")
	if err != nil {
		t.Fatalf("GetContentsRaw error: %v", err)
	}
	if string(got) != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	if gotAccept != "application/vnd.github.raw+json" {
		t.Fatalf("Accept = %q, want application/vnd.github.raw+json", gotAccept)
	}
	if gotRefPresent {
		t.Fatal("verwachtte geen ref-parameter wanneer ref leeg is")
	}
}

func TestGetContentsRawWithRef(t *testing.T) {
	var gotRef string
	c := newTestOrgClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotRef = r.URL.Query().Get("ref")
		w.Write([]byte("ok"))
	})

	_, err := c.GetContentsRaw(context.Background(), "bitfactory-nl", "web-afcnl", "deploy_conf.json", "release")
	if err != nil {
		t.Fatalf("GetContentsRaw error: %v", err)
	}
	if gotRef != "release" {
		t.Fatalf("ref = %q, want release", gotRef)
	}
}

func TestGetContentsRawNotFound(t *testing.T) {
	c := newTestOrgClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	})

	_, err := c.GetContentsRaw(context.Background(), "bitfactory-nl", "web-afcnl", "deploy_conf.json", "")
	if err == nil {
		t.Fatal("verwachtte fout bij 404, kreeg nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("errors.Is(err, ErrNotFound) = false, err: %v", err)
	}
	if !strings.Contains(err.Error(), "deploy_conf.json") {
		t.Fatalf("foutmelding bevat pad niet: %v", err)
	}
}

func TestGetContentsRawServerError(t *testing.T) {
	c := newTestOrgClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom"))
	})

	_, err := c.GetContentsRaw(context.Background(), "bitfactory-nl", "web-afcnl", "deploy_conf.json", "")
	if err == nil {
		t.Fatal("verwachtte fout bij 500, kreeg nil")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatal("errors.Is(err, ErrNotFound) = true, want false voor 500")
	}
}

func equalStrSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
