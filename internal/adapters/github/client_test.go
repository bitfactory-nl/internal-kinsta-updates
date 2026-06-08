package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient("test-token", "acme/wp-paid-plugins", "")
	c.baseURL = srv.URL
	return c
}

func TestNewClientDefaultsRef(t *testing.T) {
	if got := NewClient("t", "acme/repo", "").ref; got != "main" {
		t.Fatalf("ref = %q, want main", got)
	}
	if got := NewClient("t", "acme/repo", "release").ref; got != "release" {
		t.Fatalf("ref = %q, want release", got)
	}
}

func TestGetManifest(t *testing.T) {
	const manifest = `plugins:
  - slug: acme-pro
    display_name: ACME Pro
    latest_version: 1.2.3
    zip_path: plugins/acme-pro/acme-pro-1.2.3.zip
    updated_at: 2026-01-15T10:00:00Z
  - slug: beta-forms
    display_name: Beta Forms
    latest_version: 0.9.0
    zip_path: plugins/beta-forms/beta-forms-0.9.0.zip
    updated_at: 2026-02-01T08:30:00Z
`
	var gotAuth, gotAccept, gotPath string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(manifest))
	})

	plugins, err := c.GetManifest(context.Background())
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if len(plugins) != 2 {
		t.Fatalf("got %d plugins, want 2", len(plugins))
	}

	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotAccept != rawAcceptHeader {
		t.Errorf("Accept = %q, want %q", gotAccept, rawAcceptHeader)
	}
	if !strings.HasSuffix(gotPath, "/repos/acme/wp-paid-plugins/contents/manifest.yml") {
		t.Errorf("path = %q", gotPath)
	}

	p := plugins[0]
	if p.Slug != "acme-pro" || p.DisplayName != "ACME Pro" || p.LatestVersion != "1.2.3" {
		t.Errorf("plugin[0] = %+v", p)
	}
	if p.UpdatedAt.Year() != 2026 || p.UpdatedAt.Month() != 1 {
		t.Errorf("updatedAt = %v", p.UpdatedAt)
	}
	if !strings.Contains(p.DownloadURL, "plugins/acme-pro/acme-pro-1.2.3.zip") {
		t.Errorf("downloadURL = %q", p.DownloadURL)
	}
	if !strings.Contains(p.DownloadURL, "ref=main") {
		t.Errorf("downloadURL missing ref: %q", p.DownloadURL)
	}
}

func TestGetManifestErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "http error status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
		},
		{
			name: "invalid yaml",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("plugins: [: not yaml"))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, tt.handler)
			if _, err := c.GetManifest(context.Background()); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestDownloadZip(t *testing.T) {
	want := []byte("PK\x03\x04 fake zip bytes")
	var gotPath string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write(want)
	})

	got, err := c.DownloadZip(context.Background(), "plugins/acme-pro/acme-pro-1.2.3.zip")
	if err != nil {
		t.Fatalf("DownloadZip: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if !strings.HasSuffix(gotPath, "/contents/plugins/acme-pro/acme-pro-1.2.3.zip") {
		t.Errorf("path = %q", gotPath)
	}
}

func TestDownloadZipError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	if _, err := c.DownloadZip(context.Background(), "x.zip"); err == nil {
		t.Fatal("expected error, got nil")
	}
}
