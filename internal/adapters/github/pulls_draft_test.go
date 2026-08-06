package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// De draft-vlag moet daadwerkelijk in de payload staan: een PR die als gewone PR
// opengaat, leest als werk dat al beoordeeld is.
func TestCreateDraftPullZetDraftInDePayload(t *testing.T) {
	var ontvangen map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &ontvangen)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"number":7,"html_url":"https://github.com/acme/repo/pull/7","draft":true}`))
	}))
	defer srv.Close()

	c := NewPullClient("token")
	c.baseURL = srv.URL

	pr, err := c.CreateDraftPull(context.Background(), "acme", "repo", "fix/log-abc", "main", "titel", "body")
	if err != nil {
		t.Fatalf("CreateDraftPull: %v", err)
	}
	if pr.HTMLURL != "https://github.com/acme/repo/pull/7" {
		t.Errorf("url = %q", pr.HTMLURL)
	}
	if draft, _ := ontvangen["draft"].(bool); !draft {
		t.Errorf("draft ontbreekt in payload: %v", ontvangen)
	}
}

// Een gewone CreatePull mag geen draft-vlag meesturen, zodat bestaand gedrag
// (de WordPress core-updates) niet verandert.
func TestCreatePullStuurtGeenDraftVlag(t *testing.T) {
	var ontvangen map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &ontvangen)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"number":8,"html_url":"u"}`))
	}))
	defer srv.Close()

	c := NewPullClient("token")
	c.baseURL = srv.URL

	if _, err := c.CreatePull(context.Background(), "acme", "repo", "h", "b", "t", "body"); err != nil {
		t.Fatalf("CreatePull: %v", err)
	}
	if _, aanwezig := ontvangen["draft"]; aanwezig {
		t.Errorf("draft hoort weg te blijven: %v", ontvangen)
	}
}
