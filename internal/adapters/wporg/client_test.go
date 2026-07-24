package wporg

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLatestVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/plugins/info/1.0/contact-form-7.json" {
			_, _ = w.Write([]byte(`{"version":"5.9.2","download_link":"https://downloads.wordpress.org/plugin/contact-form-7.5.9.2.zip"}`))
			return
		}
		// wp.org returns literal null (200) for unknown slugs
		_, _ = w.Write([]byte(`null`))
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL

	ver, url, err := c.LatestVersion(context.Background(), "contact-form-7")
	if err != nil {
		t.Fatalf("LatestVersion: %v", err)
	}
	if ver != "5.9.2" || url == "" {
		t.Errorf("got %q %q", ver, url)
	}

	if _, _, err := c.LatestVersion(context.Background(), "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}
