package endoflife

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// phpJSON is een ingekorte echte respons van endoflife.date/api/php.json.
// Let op de union-types: lts/eol/support zijn bool óf "YYYY-MM-DD".
const phpJSON = `[
  {"cycle":"8.5","eol":"2029-12-31","latest":"8.5.8","lts":false,"support":"2027-12-31"},
  {"cycle":"8.3","eol":"2027-12-31","latest":"8.3.32","lts":false,"support":"2025-12-31"}
]`

func newTestClient(t *testing.T, hits *atomic.Int32, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := NewClient()
	c.baseURL = srv.URL
	return c
}

func TestCyclesParsesUnionFields(t *testing.T) {
	var hits atomic.Int32
	c := newTestClient(t, &hits, phpJSON)

	cycles, err := c.Cycles(context.Background(), "php")
	if err != nil {
		t.Fatalf("Cycles: %v", err)
	}
	if len(cycles) != 2 {
		t.Fatalf("len = %d, want 2", len(cycles))
	}
	got := cycles[0]
	if got.Cycle != "8.5" || got.Latest != "8.5.8" {
		t.Errorf("cycle/latest = %q/%q", got.Cycle, got.Latest)
	}
	if got.LTS.IsDate || got.LTS.Bool {
		t.Errorf("LTS = %+v, want bool false", got.LTS)
	}
	wantEOL := time.Date(2029, 12, 31, 0, 0, 0, 0, time.UTC)
	if !got.EOL.IsDate || !got.EOL.Date.Equal(wantEOL) {
		t.Errorf("EOL = %+v, want date %s", got.EOL, wantEOL)
	}
}

func TestCyclesCachesPerProduct(t *testing.T) {
	var hits atomic.Int32
	c := newTestClient(t, &hits, phpJSON)

	for i := 0; i < 3; i++ {
		if _, err := c.Cycles(context.Background(), "php"); err != nil {
			t.Fatalf("Cycles #%d: %v", i, err)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("http hits = %d, want 1 (cache)", n)
	}

	c.now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	if _, err := c.Cycles(context.Background(), "php"); err != nil {
		t.Fatalf("Cycles na TTL: %v", err)
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("http hits = %d, want 2 (TTL verlopen)", n)
	}
}
