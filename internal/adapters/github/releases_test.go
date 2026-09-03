package github

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestReleaseClient(t *testing.T, handler http.HandlerFunc) *ReleaseClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewReleaseClient("test-token", "bitfactory-nl/internal-kinsta-updates")
	c.baseURL = srv.URL
	return c
}

const releaseJSON = `{
  "tag_name": "v0.2.10",
  "body": "## Wijzigingen\n\n### Nieuw\n- Zelf-update\n",
  "assets": [
    {"id": 11, "name": "checksums.txt", "size": 120},
    {"id": 42, "name": "RDM-Sites-Tool-v0.2.10-macOS.zip", "size": 12230392}
  ]
}`

func TestLatestReleaseParseertTagBodyEnAsset(t *testing.T) {
	var gotPath, gotAuth, gotVersie string
	c := newTestReleaseClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotVersie = r.Header.Get("X-GitHub-Api-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(releaseJSON))
	})

	rel, err := c.LatestRelease(context.Background())
	if err != nil {
		t.Fatalf("LatestRelease: %v", err)
	}

	if gotPath != "/repos/bitfactory-nl/internal-kinsta-updates/releases/latest" {
		t.Errorf("pad = %q", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, wil Bearer test-token", gotAuth)
	}
	if gotVersie != "2022-11-28" {
		t.Errorf("X-GitHub-Api-Version = %q", gotVersie)
	}
	if rel.TagName != "v0.2.10" {
		t.Errorf("TagName = %q, wil v0.2.10", rel.TagName)
	}
	if !strings.Contains(rel.Body, "## Wijzigingen") {
		t.Errorf("Body = %q, wil de wijzigingen-sectie", rel.Body)
	}
	if rel.Asset.ID != 42 || rel.Asset.Size != 12230392 {
		t.Errorf("Asset = %+v, wil id 42 en de zip-grootte", rel.Asset)
	}
}

func TestLatestReleaseZonderMacOSAsset(t *testing.T) {
	c := newTestReleaseClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v0.2.10","body":"","assets":[{"id":1,"name":"notes.txt","size":10}]}`))
	})

	_, err := c.LatestRelease(context.Background())
	if !errors.Is(err, ErrNoMacOSAsset) {
		t.Fatalf("err = %v, wil ErrNoMacOSAsset", err)
	}
}

func TestLatestReleaseFoutcodes(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		c := newTestReleaseClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"nope"}`))
		})

		_, err := c.LatestRelease(context.Background())
		if err == nil {
			t.Fatalf("status %d gaf geen fout", status)
		}
		if !strings.Contains(err.Error(), "401") && !strings.Contains(err.Error(), "403") && !strings.Contains(err.Error(), "404") {
			t.Errorf("foutmelding bij status %d = %q, wil de statuscode erin", status, err.Error())
		}
	}
}

func TestDownloadAssetSchrijftBytesEnMeldtVoortgang(t *testing.T) {
	payload := bytes.Repeat([]byte("z"), 3000)
	var gotAccept, gotPath string
	c := newTestReleaseClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotPath = r.URL.Path
		w.Header().Set("Content-Length", "3000")
		_, _ = w.Write(payload)
	})

	var buf bytes.Buffer
	var laatsteDone, laatsteTotal int64
	err := c.DownloadAsset(context.Background(), 42, &buf, func(done, total int64) {
		laatsteDone, laatsteTotal = done, total
	})
	if err != nil {
		t.Fatalf("DownloadAsset: %v", err)
	}

	if gotPath != "/repos/bitfactory-nl/internal-kinsta-updates/releases/assets/42" {
		t.Errorf("pad = %q", gotPath)
	}
	if gotAccept != "application/octet-stream" {
		t.Errorf("Accept = %q, wil application/octet-stream", gotAccept)
	}
	if buf.Len() != len(payload) {
		t.Errorf("geschreven bytes = %d, wil %d", buf.Len(), len(payload))
	}
	if laatsteDone != 3000 || laatsteTotal != 3000 {
		t.Errorf("laatste voortgang = %d/%d, wil 3000/3000", laatsteDone, laatsteTotal)
	}
}

func TestDownloadAssetVolgtRedirect(t *testing.T) {
	opslag := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	t.Cleanup(opslag.Close)

	c := newTestReleaseClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, opslag.URL, http.StatusFound)
	})

	var buf bytes.Buffer
	if err := c.DownloadAsset(context.Background(), 42, &buf, nil); err != nil {
		t.Fatalf("DownloadAsset: %v", err)
	}
	if buf.String() != "payload" {
		t.Errorf("inhoud = %q, wil payload", buf.String())
	}
}

func TestDownloadAssetFoutstatus(t *testing.T) {
	c := newTestReleaseClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	var buf bytes.Buffer
	if err := c.DownloadAsset(context.Background(), 42, &buf, nil); err == nil {
		t.Error("DownloadAsset gaf geen fout bij status 404")
	}
}
