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

func TestLatestVersionNotFoundVariants(t *testing.T) {
	// Test ErrNotFound with HTTP 404
	srvNotFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`not found`))
	}))
	defer srvNotFound.Close()

	c := NewClient()
	c.BaseURL = srvNotFound.URL

	if _, _, err := c.LatestVersion(context.Background(), "any-slug"); !errors.Is(err, ErrNotFound) {
		t.Errorf("404 case: want ErrNotFound, got %v", err)
	}

	// Test ErrNotFound with HTTP 200 but empty body
	srvEmpty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte{})
	}))
	defer srvEmpty.Close()

	c.BaseURL = srvEmpty.URL

	if _, _, err := c.LatestVersion(context.Background(), "any-slug"); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty body case: want ErrNotFound, got %v", err)
	}
}

func TestDownload(t *testing.T) {
	// Simulate a zip file with PK magic bytes
	zipBytes := []byte{0x50, 0x4b, 0x03, 0x04} // PK zip magic bytes + dummy content
	zipBytes = append(zipBytes, []byte("dummy zip content")...)

	// Test success: 200 with zip bytes
	srvSuccess := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(zipBytes)
	}))
	defer srvSuccess.Close()

	c := NewClient()
	got, err := c.Download(context.Background(), srvSuccess.URL)
	if err != nil {
		t.Fatalf("Download success: %v", err)
	}
	if string(got) != string(zipBytes) {
		t.Errorf("got bytes %v, want %v", got, zipBytes)
	}

	// Test failure: 500 status
	srvError := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`error`))
	}))
	defer srvError.Close()

	if _, err := c.Download(context.Background(), srvError.URL); err == nil {
		t.Error("Download 500: want error, got nil")
	}
}

func TestLatestThemeVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/themes/info/1.1/" && r.URL.Query().Get("request[slug]") == "twentytwentyfour" {
			_, _ = w.Write([]byte(`{"name":"Twenty Twenty-Four","version":"1.1"}`))
			return
		}
		_, _ = w.Write([]byte(`false`))
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL

	ver, err := c.LatestThemeVersion(context.Background(), "twentytwentyfour")
	if err != nil {
		t.Fatalf("LatestThemeVersion: %v", err)
	}
	if ver != "1.1" {
		t.Errorf("got %q", ver)
	}
	if _, err := c.LatestThemeVersion(context.Background(), "custom-child"); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestCoreDownloadURL(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{"gewone versie", "6.5.3", "https://wordpress.org/wordpress-6.5.3-no-content.zip"},
		{"leading v wordt getrimd", "v6.5.3", "https://wordpress.org/wordpress-6.5.3-no-content.zip"},
		{"omringende whitespace wordt getrimd", "  6.5.3  ", "https://wordpress.org/wordpress-6.5.3-no-content.zip"},
		{"whitespace en leading v samen", " v6.5.3 ", "https://wordpress.org/wordpress-6.5.3-no-content.zip"},
		{"lege string geeft lege string", "", ""},
		{"whitespace-only geeft lege string", "   ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CoreDownloadURL(tt.version); got != tt.want {
				t.Errorf("CoreDownloadURL(%q) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}
}

func TestLatestCoreVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/core/version-check/1.7/" {
			_, _ = w.Write([]byte(`{"offers":[{"current":"6.5.3"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL

	ver, err := c.LatestCoreVersion(context.Background())
	if err != nil {
		t.Fatalf("LatestCoreVersion: %v", err)
	}
	if ver != "6.5.3" {
		t.Errorf("got %q", ver)
	}
}
