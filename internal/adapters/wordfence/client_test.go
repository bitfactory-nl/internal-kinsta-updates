package wordfence

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer testkey" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"x":{"id":"x"}}`))
	}))
	defer srv.Close()

	c := NewClient("testkey")
	c.BaseURL = srv.URL
	data, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(data) != `{"x":{"id":"x"}}` {
		t.Errorf("unexpected body: %s", data)
	}
}

func TestFetchUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewClient("wrong")
	c.BaseURL = srv.URL
	if _, err := c.Fetch(context.Background()); err == nil {
		t.Fatal("expected error on 401")
	}
}
