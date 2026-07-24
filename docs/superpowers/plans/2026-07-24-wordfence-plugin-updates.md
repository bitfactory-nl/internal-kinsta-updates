# Wordfence Plugin Updates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een globaal menu-item dat de Wordfence vulnerability-feed ophaalt en cachet, deze vergelijkt met de WordPress-plugins in `public/wp-content/plugins/*/` van alle gescande projecten, en kwetsbare plugins in bulk bijwerkt via een update-branch per project.

**Architecture:** Go-backend met adapters (Wordfence-feed, wp.org-download, lokale plugin-reader) en twee services (feed/cache/matching + update-runner die bestaande `GitService`-primitieven hergebruikt). React-frontend met een nieuw top-level paneel en een Wordfence-sectie in Settings. API-key via bestaand `keychain:`/`ResolveSecret`-patroon.

**Tech Stack:** Go 1.25, Wails v3, `net/http` + `archive/zip` (stdlib), React + TypeScript + Tailwind, `gopkg.in/yaml.v3`.

## Global Constraints

- Plugin-pad in elk project: `public/wp-content/plugins/*/` (slug = mapnaam).
- Update-branchnaam: `security/wordfence-YYYY-MM-DD`.
- Commit-bericht per plugin: `fix(security): update {slug} {oud}→{nieuw} (Wordfence)`.
- Basis-ref = default branch van de repo; moet matchen op glob `release/*`, anders project overslaan met reden `skipped_no_release`.
- Doelversie = laatste stabiele versie van wp.org. Slug niet op wp.org → status `manual`, niet updaten.
- Output blijft lokaal: branch + commit, geen push/PR.
- Feed volledig cachen naar `~/.config/rdm/wordfence-production.json`; UI toont standaard 50.
- API-key opgeslagen als string in `~/.config/rdm/config.yml` (zoals Kinsta/Anthropic), literal of `keychain:`-referentie; opgelost via `config.ResolveSecret`.
- Alle nieuwe Go-code onder module `github.com/rdm/sites-tool`.
- Tests draaien met `go test ./...`.

---

### Task 1: Version-compare helper

PHP-`version_compare`-stijl vergelijker; nodig voor Wordfence range-matching. Plugin-versies zijn niet strikt semver, dus dot-split met numeriek-vs-string vergelijking.

**Files:**
- Create: `internal/services/version.go`
- Test: `internal/services/version_test.go`

**Interfaces:**
- Produces: `func compareVersions(a, b string) int` (returns -1 als a<b, 0 als gelijk, 1 als a>b).

- [ ] **Step 1: Write the failing test**

```go
package services

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.2.0", "1.10.0", -1},
		{"2.0", "1.9.9", 1},
		{"1.2", "1.2.0", 0},
		{"1.2.3", "1.2", 1},
		{"1.0.0-beta", "1.0.0", -1},
		{"", "1.0", -1},
		{"1.0", "", 1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/ -run TestCompareVersions -v`
Expected: FAIL (`undefined: compareVersions`).

- [ ] **Step 3: Write minimal implementation**

```go
package services

import (
	"strconv"
	"strings"
)

// compareVersions compares two dotted version strings PHP-style.
// Numeric segments compare numerically; if a segment is non-numeric it
// compares lexically. A missing segment counts as lower than a present one,
// except a present numeric 0 equals a missing segment ("1.2" == "1.2.0").
func compareVersions(a, b string) int {
	as := splitVersion(a)
	bs := splitVersion(b)
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var x, y string
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		if c := compareSegment(x, y); c != 0 {
			return c
		}
	}
	return 0
}

func splitVersion(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	// Treat . - _ + as separators.
	repl := strings.NewReplacer("-", ".", "_", ".", "+", ".")
	return strings.Split(repl.Replace(v), ".")
}

func compareSegment(x, y string) int {
	if x == y {
		return 0
	}
	xn, xerr := strconv.Atoi(x)
	yn, yerr := strconv.Atoi(y)
	switch {
	case xerr == nil && yerr == nil:
		if xn < yn {
			return -1
		}
		if xn > yn {
			return 1
		}
		return 0
	case xerr == nil && yerr != nil:
		// numeric segment (e.g. "0") ranks above a non-numeric/pre-release (e.g. "beta")
		if y == "" {
			if xn == 0 {
				return 0
			}
			return 1
		}
		return 1
	case xerr != nil && yerr == nil:
		if x == "" {
			if yn == 0 {
				return 0
			}
			return -1
		}
		return -1
	default:
		if x < y {
			return -1
		}
		return 1
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/services/ -run TestCompareVersions -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/version.go internal/services/version_test.go
git commit -m "feat(updates): version-compare helper for vuln range matching"
```

---

### Task 2: Wordfence domain types + feed parser

Domain-types en een parser die de ruwe Wordfence v3-JSON omzet naar onze types. Alleen plugin-software is relevant.

**Files:**
- Create: `internal/domain/wordfence.go`
- Create: `internal/adapters/wordfence/parse.go`
- Test: `internal/adapters/wordfence/parse_test.go`

**Interfaces:**
- Produces (domain): `Vulnerability{ID, Title, CVE string; CVSSScore float64; Severity string; Published time.Time; Software []AffectedSoftware}` en `AffectedSoftware{Type, Slug, AffectedFrom string; FromInclusive bool; AffectedTo string; ToInclusive bool; PatchedVersions []string}`.
- Produces (adapter): `func ParseFeed(data []byte) ([]domain.Vulnerability, error)`.

- [ ] **Step 1: Write the failing test**

```go
package wordfence

import "testing"

const sampleFeed = `{
  "abc-123": {
    "id": "abc-123",
    "title": "Contact Form 7 <= 5.3.1 - File Upload",
    "cve": "CVE-2020-1234",
    "cvss": {"score": "7.5", "rating": "High"},
    "published": "2021-01-05T00:00:00.000Z",
    "software": [
      {
        "type": "plugin",
        "slug": "contact-form-7",
        "affected_versions": {
          "* - 5.3.1": {
            "from_version": "*",
            "from_inclusive": true,
            "to_version": "5.3.1",
            "to_inclusive": true
          }
        },
        "patched": true,
        "patched_versions": ["5.3.2"]
      }
    ]
  }
}`

func TestParseFeed(t *testing.T) {
	vulns, err := ParseFeed([]byte(sampleFeed))
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	if len(vulns) != 1 {
		t.Fatalf("want 1 vuln, got %d", len(vulns))
	}
	v := vulns[0]
	if v.CVE != "CVE-2020-1234" || v.CVSSScore != 7.5 {
		t.Errorf("meta mismatch: %+v", v)
	}
	if len(v.Software) != 1 {
		t.Fatalf("want 1 software, got %d", len(v.Software))
	}
	s := v.Software[0]
	if s.Type != "plugin" || s.Slug != "contact-form-7" {
		t.Errorf("software mismatch: %+v", s)
	}
	if s.AffectedFrom != "*" || s.AffectedTo != "5.3.1" || !s.ToInclusive {
		t.Errorf("range mismatch: %+v", s)
	}
	if len(s.PatchedVersions) != 1 || s.PatchedVersions[0] != "5.3.2" {
		t.Errorf("patched mismatch: %+v", s)
	}
}

func TestParseFeedEmpty(t *testing.T) {
	vulns, err := ParseFeed([]byte(`{}`))
	if err != nil {
		t.Fatalf("ParseFeed empty: %v", err)
	}
	if len(vulns) != 0 {
		t.Errorf("want 0, got %d", len(vulns))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/adapters/wordfence/ -v`
Expected: FAIL (package/function undefined).

- [ ] **Step 3a: Create the domain types**

```go
// internal/domain/wordfence.go
package domain

import "time"

// Vulnerability is one Wordfence vulnerability record (plugin-relevant subset).
type Vulnerability struct {
	ID        string             `json:"id"`
	Title     string             `json:"title"`
	CVE       string             `json:"cve"`
	CVSSScore float64            `json:"cvssScore"`
	Severity  string             `json:"severity"`
	Published time.Time          `json:"published"`
	Software  []AffectedSoftware `json:"software"`
}

// AffectedSoftware is one affected plugin/theme entry with a version range.
type AffectedSoftware struct {
	Type            string   `json:"type"` // plugin | theme | core
	Slug            string   `json:"slug"`
	AffectedFrom    string   `json:"affectedFrom"` // "*" or version; "" = unbounded
	FromInclusive   bool     `json:"fromInclusive"`
	AffectedTo      string   `json:"affectedTo"` // "*" or version; "" = unbounded
	ToInclusive     bool     `json:"toInclusive"`
	PatchedVersions []string `json:"patchedVersions"`
}
```

- [ ] **Step 3b: Create the parser**

```go
// internal/adapters/wordfence/parse.go
package wordfence

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

type rawVuln struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CVE       string `json:"cve"`
	CVSS      struct {
		Score  string `json:"score"`
		Rating string `json:"rating"`
	} `json:"cvss"`
	Published string        `json:"published"`
	Software  []rawSoftware `json:"software"`
}

type rawSoftware struct {
	Type             string                    `json:"type"`
	Slug             string                    `json:"slug"`
	AffectedVersions map[string]rawVersionSpan `json:"affected_versions"`
	Patched          bool                      `json:"patched"`
	PatchedVersions  []string                  `json:"patched_versions"`
}

type rawVersionSpan struct {
	FromVersion   string `json:"from_version"`
	FromInclusive bool   `json:"from_inclusive"`
	ToVersion     string `json:"to_version"`
	ToInclusive   bool   `json:"to_inclusive"`
}

// ParseFeed converts the Wordfence v3 production feed (a JSON object keyed by
// vulnerability id) into domain.Vulnerability values. Non-plugin software is
// preserved but callers filter on Type == "plugin".
func ParseFeed(data []byte) ([]domain.Vulnerability, error) {
	var raw map[string]rawVuln
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make([]domain.Vulnerability, 0, len(raw))
	for id, rv := range raw {
		v := domain.Vulnerability{
			ID:       firstNonEmpty(rv.ID, id),
			Title:    rv.Title,
			CVE:      rv.CVE,
			Severity: rv.CVSS.Rating,
		}
		if f, err := strconv.ParseFloat(rv.CVSS.Score, 64); err == nil {
			v.CVSSScore = f
		}
		if t, err := time.Parse(time.RFC3339, rv.Published); err == nil {
			v.Published = t
		}
		for _, rs := range rv.Software {
			for _, span := range rs.AffectedVersions {
				v.Software = append(v.Software, domain.AffectedSoftware{
					Type:            rs.Type,
					Slug:            rs.Slug,
					AffectedFrom:    span.FromVersion,
					FromInclusive:   span.FromInclusive,
					AffectedTo:      span.ToVersion,
					ToInclusive:     span.ToInclusive,
					PatchedVersions: rs.PatchedVersions,
				})
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/adapters/wordfence/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/wordfence.go internal/adapters/wordfence/parse.go internal/adapters/wordfence/parse_test.go
git commit -m "feat(wordfence): domain types + feed parser"
```

---

### Task 3: Wordfence HTTP fetch client

Haalt de productiefeed op met Bearer-auth. Kleine wrapper met injecteerbare base-URL en http.Client voor tests.

**Files:**
- Create: `internal/adapters/wordfence/client.go`
- Test: `internal/adapters/wordfence/client_test.go`

**Interfaces:**
- Consumes: `ParseFeed` (Task 2).
- Produces: `func NewClient(apiKey string) *Client`, `func (c *Client) Fetch(ctx context.Context) ([]byte, error)`, en veld `c.BaseURL` (overschrijfbaar in tests). Fetch retourneert de ruwe JSON-bytes.

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/adapters/wordfence/ -run TestFetch -v`
Expected: FAIL (`undefined: NewClient`).

- [ ] **Step 3: Write minimal implementation**

```go
// internal/adapters/wordfence/client.go
package wordfence

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const productionFeedURL = "https://www.wordfence.com/api/intelligence/v3/vulnerabilities/production"

type Client struct {
	apiKey  string
	BaseURL string
	http    *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:  apiKey,
		BaseURL: productionFeedURL,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// Fetch downloads the raw production feed JSON.
func (c *Client) Fetch(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wordfence fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wordfence fetch: status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/adapters/wordfence/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/wordfence/client.go internal/adapters/wordfence/client_test.go
git commit -m "feat(wordfence): HTTP fetch client with Bearer auth"
```

---

### Task 4: wp.org plugin client

Resolve de laatste stabiele versie + download-URL en download de zip.

**Files:**
- Create: `internal/adapters/wporg/client.go`
- Test: `internal/adapters/wporg/client_test.go`

**Interfaces:**
- Produces: `var ErrNotFound = errors.New("plugin not found on wp.org")`; `func NewClient() *Client` met veld `BaseURL`; `func (c *Client) LatestVersion(ctx context.Context, slug string) (version, downloadURL string, err error)`; `func (c *Client) Download(ctx context.Context, url string) ([]byte, error)`.

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/adapters/wporg/ -v`
Expected: FAIL (package undefined).

- [ ] **Step 3: Write minimal implementation**

```go
// internal/adapters/wporg/client.go
package wporg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ErrNotFound means the slug is unknown on wp.org (likely a premium/custom plugin).
var ErrNotFound = errors.New("plugin not found on wp.org")

const infoBaseURL = "https://api.wordpress.org"

type Client struct {
	BaseURL string
	http    *http.Client
}

func NewClient() *Client {
	return &Client{BaseURL: infoBaseURL, http: &http.Client{Timeout: 60 * time.Second}}
}

type infoResponse struct {
	Version      string `json:"version"`
	DownloadLink string `json:"download_link"`
}

// LatestVersion returns the latest stable version and its zip download URL.
func (c *Client) LatestVersion(ctx context.Context, slug string) (string, string, error) {
	url := fmt.Sprintf("%s/plugins/info/1.0/%s.json", c.BaseURL, slug)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("wporg info: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}
	// wp.org responds with the JSON literal `null` for unknown slugs.
	if resp.StatusCode == http.StatusNotFound || string(body) == "null" || len(body) == 0 {
		return "", "", ErrNotFound
	}
	var info infoResponse
	if err := json.Unmarshal(body, &info); err != nil {
		return "", "", fmt.Errorf("wporg info parse: %w", err)
	}
	if info.Version == "" || info.DownloadLink == "" {
		return "", "", ErrNotFound
	}
	return info.Version, info.DownloadLink, nil
}

// Download fetches the plugin zip bytes.
func (c *Client) Download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wporg download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wporg download: status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/adapters/wporg/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/wporg/client.go internal/adapters/wporg/client_test.go
git commit -m "feat(wporg): plugin info + download client"
```

---

### Task 5: Local installed-plugin reader

Leest geïnstalleerde plugins uit `public/wp-content/plugins/*/`: slug (mapnaam) + versie (`Version:`-header in een PHP-bestand, fallback `Stable tag:` in readme.txt).

**Files:**
- Create: `internal/adapters/wpplugins/reader.go`
- Test: `internal/adapters/wpplugins/reader_test.go`

**Interfaces:**
- Produces: `type InstalledPlugin struct{ Slug, Version, Dir string }`; `func ReadInstalled(projectPath string) ([]InstalledPlugin, error)`. Retourneert lege slice (geen error) als de pluginmap niet bestaat.

- [ ] **Step 1: Write the failing test**

```go
package wpplugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadInstalled(t *testing.T) {
	root := t.TempDir()
	pdir := filepath.Join(root, "public", "wp-content", "plugins", "contact-form-7")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	main := "<?php\n/*\nPlugin Name: Contact Form 7\nVersion: 5.9.2\n*/\n"
	if err := os.WriteFile(filepath.Join(pdir, "wp-contact-form-7.php"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}

	// readme-only plugin (Stable tag fallback)
	rdir := filepath.Join(root, "public", "wp-content", "plugins", "akismet")
	if err := os.MkdirAll(rdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rdir, "akismet.php"), []byte("<?php\n// no header\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rdir, "readme.txt"), []byte("=== Akismet ===\nStable tag: 5.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadInstalled(root)
	if err != nil {
		t.Fatalf("ReadInstalled: %v", err)
	}
	versions := map[string]string{}
	for _, p := range got {
		versions[p.Slug] = p.Version
	}
	if versions["contact-form-7"] != "5.9.2" {
		t.Errorf("cf7 version = %q", versions["contact-form-7"])
	}
	if versions["akismet"] != "5.3" {
		t.Errorf("akismet version = %q", versions["akismet"])
	}
}

func TestReadInstalledNoDir(t *testing.T) {
	got, err := ReadInstalled(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/adapters/wpplugins/ -v`
Expected: FAIL (package undefined).

- [ ] **Step 3: Write minimal implementation**

```go
// internal/adapters/wpplugins/reader.go
package wpplugins

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// InstalledPlugin is one plugin directory under public/wp-content/plugins.
type InstalledPlugin struct {
	Slug    string `json:"slug"`
	Version string `json:"version"`
	Dir     string `json:"dir"`
}

var (
	headerVersionRe = regexp.MustCompile(`(?im)^[ \t/*]*Version:\s*(.+?)\s*$`)
	stableTagRe     = regexp.MustCompile(`(?im)^\s*Stable tag:\s*(.+?)\s*$`)
)

// PluginsDir is the fixed location of plugins within a project checkout.
func PluginsDir(projectPath string) string {
	return filepath.Join(projectPath, "public", "wp-content", "plugins")
}

// ReadInstalled scans public/wp-content/plugins/*/ and returns each plugin's
// slug and version. A missing plugins directory yields an empty slice.
func ReadInstalled(projectPath string) ([]InstalledPlugin, error) {
	base := PluginsDir(projectPath)
	entries, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []InstalledPlugin
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(base, e.Name())
		ver := readPluginVersion(dir)
		out = append(out, InstalledPlugin{Slug: e.Name(), Version: ver, Dir: dir})
	}
	return out, nil
}

func readPluginVersion(dir string) string {
	// Prefer a Version: header from any top-level .php file.
	files, _ := os.ReadDir(dir)
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(strings.ToLower(f.Name()), ".php") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			continue
		}
		if m := headerVersionRe.FindSubmatch(data); m != nil {
			return strings.TrimSpace(string(m[1]))
		}
	}
	// Fallback: Stable tag from readme.txt.
	if data, err := os.ReadFile(filepath.Join(dir, "readme.txt")); err == nil {
		if m := stableTagRe.FindSubmatch(data); m != nil {
			return strings.TrimSpace(string(m[1]))
		}
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/adapters/wpplugins/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/wpplugins/reader.go internal/adapters/wpplugins/reader_test.go
git commit -m "feat(wpplugins): local installed-plugin reader"
```

---

### Task 6: Config + Settings for Wordfence API key

Voeg `Wordfence` toe aan het config-schema, defaults, en de settings DTO/Get/Save. Daarna het UI-veld.

**Files:**
- Modify: `internal/config/schema.go`
- Modify: `internal/services/settings_service.go`
- Modify: `frontend/src/components/SettingsPage.tsx`
- Test: `internal/services/settings_service_test.go` (create if absent)

**Interfaces:**
- Consumes: bestaande `config.Global`.
- Produces: `config.Global.Wordfence.APIKey` (yaml `wordfence.api_key`); `AppSettings.WordfenceAPIKey` (json `wordfenceApiKey`).

- [ ] **Step 1: Write the failing test**

```go
// internal/services/settings_service_test.go
package services

import (
	"testing"

	"github.com/rdm/sites-tool/internal/config"
)

func TestSettingsWordfenceRoundTrip(t *testing.T) {
	cfg := &config.Global{Editor: "cursor"}
	s := NewSettingsService(cfg)
	in := s.Get()
	in.WordfenceAPIKey = "wf-secret"
	// Save writes to disk; only assert the in-memory cfg mutation here by
	// ignoring the persistence error path via a temp HOME.
	t.Setenv("HOME", t.TempDir())
	if err := s.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if cfg.Wordfence.APIKey != "wf-secret" {
		t.Errorf("cfg not updated: %q", cfg.Wordfence.APIKey)
	}
	if got := s.Get(); got.WordfenceAPIKey != "wf-secret" {
		t.Errorf("Get roundtrip: %q", got.WordfenceAPIKey)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/ -run TestSettingsWordfence -v`
Expected: FAIL (`in.WordfenceAPIKey undefined`).

- [ ] **Step 3a: Add Wordfence to the config schema**

In `internal/config/schema.go`, add the field to `Global` and a type:

```go
type Global struct {
	ProjectsRoots []string        `yaml:"projects_roots"`
	Editor        string          `yaml:"editor"`
	Kinsta        KinstaGlobal    `yaml:"kinsta"`
	PluginRepo    PluginRepo      `yaml:"plugin_repo"`
	Notifications Notifications   `yaml:"notifications"`
	Git           GitGlobal       `yaml:"git"`
	AI            AIGlobal        `yaml:"ai"`
	Wordfence     WordfenceGlobal `yaml:"wordfence"`
}

type WordfenceGlobal struct {
	APIKey string `yaml:"api_key"` // keychain:rdm.wordfence.apiKey or literal (dev)
}
```

- [ ] **Step 3b: Extend the settings DTO + Get/Save**

In `internal/services/settings_service.go`, add the field and wire it:

```go
// in AppSettings struct, add:
	WordfenceAPIKey  string `json:"wordfenceApiKey"`
```

```go
// in Get(), add to the returned struct:
		WordfenceAPIKey:  s.cfg.Wordfence.APIKey,
```

```go
// in Save(), before the final return, add:
	s.cfg.Wordfence.APIKey = settings.WordfenceAPIKey
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/services/ -run TestSettingsWordfence -v`
Expected: PASS.

- [ ] **Step 5: Add the Settings UI section**

In `frontend/src/components/SettingsPage.tsx`, add a state toggle near the others:

```tsx
  const [showWordfenceKey, setShowWordfenceKey] = useState(false)
```

Add a new `<section>` after the Kinsta section (mirror the Kinsta API Key field markup, using `inputClass`):

```tsx
        {/* Wordfence */}
        <section>
          <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">
            Wordfence
          </h3>
          <div className="flex items-center gap-3">
            <label className="text-[12.5px] text-fg-muted w-28 shrink-0">API Key</label>
            <input
              type={showWordfenceKey ? 'text' : 'password'}
              className={inputClass}
              placeholder="wfi_…"
              value={settings.wordfenceApiKey}
              onChange={e => update('wordfenceApiKey', e.target.value)}
            />
            <button
              type="button"
              className="text-fg-muted hover:text-fg text-xs"
              onClick={() => setShowWordfenceKey(v => !v)}
            >{showWordfenceKey ? 'Verberg' : 'Toon'}</button>
          </div>
          <p className="text-[11px] text-fg-faint mt-1.5">
            Wordfence Intelligence → API-key voor de <span className="text-fg">production</span> vulnerability-feed.
          </p>
        </section>
```

> Note: `settings.wordfenceApiKey` compiles after the Wails bindings are regenerated in Task 10. Until then the TS type lacks the field; that is expected and resolved by the bindings step.

- [ ] **Step 6: Commit**

```bash
git add internal/config/schema.go internal/services/settings_service.go internal/services/settings_service_test.go frontend/src/components/SettingsPage.tsx
git commit -m "feat(settings): Wordfence API key config + UI field"
```

---

### Task 7: WordfenceService — fetch, cache, list, match

De hoofd-service: fetch+cache naar schijf, list vanuit cache, en `MatchProjects` die lokale plugins kruist met de feed en de wp.org-doelversie bepaalt.

**Files:**
- Create: `internal/services/wordfence_service.go`
- Test: `internal/services/wordfence_service_test.go`

**Interfaces:**
- Consumes: `config.Global` + `config.ResolveSecret`; `wordfence.NewClient/ParseFeed`; `wporg.NewClient` (via een interface voor tests); `wpplugins.ReadInstalled`; `ProjectService.List()`; `compareVersions` (Task 1).
- Produces:
  - `type FeedMeta struct{ FetchedAt time.Time; Count int }`
  - `type VulnFinding struct{ Slug, InstalledVersion, LatestVersion, Source, CVE, Severity, Title, VulnID string }`
  - `type ProjectVulnReport struct{ ProjectID, ProjectName, Path string; Findings []VulnFinding; Skipped bool; SkipReason string }`
  - `func NewWordfenceService(cfg *config.Global, projects *ProjectService) *WordfenceService`
  - `func (s *WordfenceService) Refresh(ctx context.Context) (FeedMeta, error)`
  - `func (s *WordfenceService) List() ([]domain.Vulnerability, error)`
  - `func (s *WordfenceService) LastFetched() FeedMeta`
  - `func (s *WordfenceService) MatchProjects() ([]ProjectVulnReport, error)`
  - `func isVersionAffected(v string, sw domain.AffectedSoftware) bool` (unexported, tested)

- [ ] **Step 1: Write the failing test**

```go
package services

import (
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestIsVersionAffected(t *testing.T) {
	sw := domain.AffectedSoftware{
		Type:          "plugin",
		Slug:          "cf7",
		AffectedFrom:  "*",
		FromInclusive: true,
		AffectedTo:    "5.3.1",
		ToInclusive:   true,
	}
	if !isVersionAffected("5.3.1", sw) {
		t.Error("5.3.1 should be affected (inclusive upper bound)")
	}
	if isVersionAffected("5.3.2", sw) {
		t.Error("5.3.2 should NOT be affected")
	}
	if !isVersionAffected("1.0", sw) {
		t.Error("1.0 should be affected (from *)")
	}

	sw2 := domain.AffectedSoftware{
		Type: "plugin", Slug: "x",
		AffectedFrom: "2.0", FromInclusive: false,
		AffectedTo: "2.5", ToInclusive: false,
	}
	if isVersionAffected("2.0", sw2) {
		t.Error("2.0 excluded by from_inclusive=false")
	}
	if !isVersionAffected("2.3", sw2) {
		t.Error("2.3 in open range")
	}
	if isVersionAffected("2.5", sw2) {
		t.Error("2.5 excluded by to_inclusive=false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/ -run TestIsVersionAffected -v`
Expected: FAIL (`undefined: isVersionAffected`).

- [ ] **Step 3: Write the implementation**

```go
// internal/services/wordfence_service.go
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/wordfence"
	"github.com/rdm/sites-tool/internal/adapters/wpplugins"
	"github.com/rdm/sites-tool/internal/adapters/wporg"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// wporgResolver is the subset of the wp.org client used here (test seam).
type wporgResolver interface {
	LatestVersion(ctx context.Context, slug string) (string, string, error)
}

type FeedMeta struct {
	FetchedAt time.Time `json:"fetchedAt"`
	Count     int       `json:"count"`
}

type VulnFinding struct {
	Slug             string `json:"slug"`
	InstalledVersion string `json:"installedVersion"`
	LatestVersion    string `json:"latestVersion"`
	Source           string `json:"source"` // "wporg" | "manual"
	CVE              string `json:"cve"`
	Severity         string `json:"severity"`
	Title            string `json:"title"`
	VulnID           string `json:"vulnId"`
}

type ProjectVulnReport struct {
	ProjectID   string        `json:"projectId"`
	ProjectName string        `json:"projectName"`
	Path        string        `json:"path"`
	Findings    []VulnFinding `json:"findings"`
	Skipped     bool          `json:"skipped"`
	SkipReason  string        `json:"skipReason"`
}

type WordfenceService struct {
	cfg      *config.Global
	projects *ProjectService
	wporg    wporgResolver

	mu   sync.RWMutex
	meta FeedMeta
}

func NewWordfenceService(cfg *config.Global, projects *ProjectService) *WordfenceService {
	s := &WordfenceService{cfg: cfg, projects: projects, wporg: wporg.NewClient()}
	if m, err := readFeedMeta(); err == nil {
		s.meta = m
	}
	return s
}

func feedPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rdm", "wordfence-production.json")
}

func metaPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rdm", "wordfence-meta.json")
}

// Refresh downloads the feed, caches it, and records metadata.
func (s *WordfenceService) Refresh(ctx context.Context) (FeedMeta, error) {
	key, err := config.ResolveSecret(s.cfg.Wordfence.APIKey)
	if err != nil {
		return FeedMeta{}, fmt.Errorf("wordfence api key: %w", err)
	}
	if key == "" {
		return FeedMeta{}, fmt.Errorf("wordfence API-key niet geconfigureerd (Instellingen → Wordfence)")
	}
	data, err := wordfence.NewClient(key).Fetch(ctx)
	if err != nil {
		return FeedMeta{}, err
	}
	vulns, err := wordfence.ParseFeed(data)
	if err != nil {
		return FeedMeta{}, fmt.Errorf("wordfence feed parse: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(feedPath()), 0o700); err != nil {
		return FeedMeta{}, err
	}
	if err := os.WriteFile(feedPath(), data, 0o600); err != nil {
		return FeedMeta{}, err
	}
	meta := FeedMeta{FetchedAt: time.Now(), Count: len(vulns)}
	if b, err := json.Marshal(meta); err == nil {
		_ = os.WriteFile(metaPath(), b, 0o600)
	}
	s.mu.Lock()
	s.meta = meta
	s.mu.Unlock()
	return meta, nil
}

// List returns the cached, parsed feed.
func (s *WordfenceService) List() ([]domain.Vulnerability, error) {
	data, err := os.ReadFile(feedPath())
	if os.IsNotExist(err) {
		return []domain.Vulnerability{}, nil
	}
	if err != nil {
		return nil, err
	}
	return wordfence.ParseFeed(data)
}

func (s *WordfenceService) LastFetched() FeedMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.meta
}

func readFeedMeta() (FeedMeta, error) {
	data, err := os.ReadFile(metaPath())
	if err != nil {
		return FeedMeta{}, err
	}
	var m FeedMeta
	return m, json.Unmarshal(data, &m)
}

// MatchProjects cross-references installed plugins against the cached feed.
func (s *WordfenceService) MatchProjects() ([]ProjectVulnReport, error) {
	vulns, err := s.List()
	if err != nil {
		return nil, err
	}
	// Index affected plugin ranges by slug.
	type vref struct {
		sw    domain.AffectedSoftware
		vuln  domain.Vulnerability
	}
	bySlug := map[string][]vref{}
	for _, v := range vulns {
		for _, sw := range v.Software {
			if sw.Type != "plugin" || sw.Slug == "" {
				continue
			}
			bySlug[sw.Slug] = append(bySlug[sw.Slug], vref{sw: sw, vuln: v})
		}
	}

	latestCache := map[string]string{} // slug -> latest version ("" = manual)
	var reports []ProjectVulnReport
	for _, p := range s.projects.List() {
		installed, err := wpplugins.ReadInstalled(p.Path)
		if err != nil {
			continue
		}
		rep := ProjectVulnReport{ProjectID: p.ID, ProjectName: p.DisplayName, Path: p.Path}
		for _, ip := range installed {
			refs := bySlug[ip.Slug]
			if len(refs) == 0 || ip.Version == "" {
				continue
			}
			hit := false
			var chosen vref
			for _, r := range refs {
				if isVersionAffected(ip.Version, r.sw) {
					hit = true
					chosen = r
					break
				}
			}
			if !hit {
				continue
			}
			latest, ok := latestCache[ip.Slug]
			if !ok {
				v, _, err := s.wporg.LatestVersion(context.Background(), ip.Slug)
				if err != nil {
					v = "" // manual
				}
				latest = v
				latestCache[ip.Slug] = v
			}
			source := "wporg"
			if latest == "" {
				source = "manual"
			}
			rep.Findings = append(rep.Findings, VulnFinding{
				Slug:             ip.Slug,
				InstalledVersion: ip.Version,
				LatestVersion:    latest,
				Source:           source,
				CVE:              chosen.vuln.CVE,
				Severity:         chosen.vuln.Severity,
				Title:            chosen.vuln.Title,
				VulnID:           chosen.vuln.ID,
			})
		}
		if len(rep.Findings) > 0 {
			reports = append(reports, rep)
		}
	}
	return reports, nil
}

// isVersionAffected reports whether v falls in the affected range of sw.
func isVersionAffected(v string, sw domain.AffectedSoftware) bool {
	// Lower bound.
	if sw.AffectedFrom != "" && sw.AffectedFrom != "*" {
		c := compareVersions(v, sw.AffectedFrom)
		if c < 0 || (c == 0 && !sw.FromInclusive) {
			return false
		}
	}
	// Upper bound.
	if sw.AffectedTo != "" && sw.AffectedTo != "*" {
		c := compareVersions(v, sw.AffectedTo)
		if c > 0 || (c == 0 && !sw.ToInclusive) {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/services/ -run TestIsVersionAffected -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/wordfence_service.go internal/services/wordfence_service_test.go
git commit -m "feat(wordfence): feed cache, list, and project matching service"
```

---

### Task 8: gitcli default-branch helper + GitService wrapper

Bepaal de default branch van een repo, met glob-check op `release/*`.

**Files:**
- Modify: `internal/adapters/gitcli/commands.go`
- Modify: `internal/services/git_service.go`
- Test: `internal/adapters/gitcli/commands_test.go` (create if absent)

**Interfaces:**
- Produces (gitcli): `func DefaultBranch(ctx context.Context, dir string) (string, error)` — probeert `git symbolic-ref --short refs/remotes/origin/HEAD` (strip `origin/`), fallback naar de hoogste `release/*` branch, anders de huidige HEAD-branch.
- Produces (GitService): `func (s *GitService) DefaultBranch(projectID string) (string, error)`.

- [ ] **Step 1: Write the failing test**

```go
package gitcli

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@t.nl"},
		{"config", "user.name", "t"},
		{"checkout", "-q", "-b", "release/1.0.x"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestDefaultBranchFallsBackToRelease(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	got, err := DefaultBranch(context.Background(), dir)
	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}
	if got != "release/1.0.x" {
		t.Errorf("got %q want release/1.0.x", got)
	}
	_ = filepath.Base(dir)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/adapters/gitcli/ -run TestDefaultBranch -v`
Expected: FAIL (`undefined: DefaultBranch`).

- [ ] **Step 3: Write the implementation**

Add to `internal/adapters/gitcli/commands.go`:

```go
// DefaultBranch returns the repository's default branch. It prefers the remote
// HEAD (origin/HEAD), falls back to the highest-sorted release/* branch, and
// finally to the current HEAD branch.
func DefaultBranch(ctx context.Context, dir string) (string, error) {
	if out, err := Run(ctx, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		b := strings.TrimSpace(out)
		b = strings.TrimPrefix(b, "origin/")
		if b != "" {
			return b, nil
		}
	}
	if out, err := Run(ctx, dir, "branch", "--list", "release/*", "--format=%(refname:short)", "--sort=-refname"); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				return line, nil
			}
		}
	}
	out, err := Run(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("default branch: %w", err)
	}
	return strings.TrimSpace(out), nil
}
```

Add to `internal/services/git_service.go`:

```go
// DefaultBranch returns the repository's default branch for a project.
func (s *GitService) DefaultBranch(projectID string) (string, error) {
	path, err := s.pathFor(projectID)
	if err != nil {
		return "", fmt.Errorf("default branch: %w", err)
	}
	ctx, cancel := s.ctxDefault()
	defer cancel()
	return gitcli.DefaultBranch(ctx, path)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/adapters/gitcli/ -run TestDefaultBranch -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/gitcli/commands.go internal/adapters/gitcli/commands_test.go internal/services/git_service.go
git commit -m "feat(git): default-branch detection with release/* fallback"
```

---

### Task 9: WordfenceUpdateService — bulk update runner

Voert per project de update uit: release-branch-check, dirty-check (met stash-optie), branch aanmaken, per plugin downloaden/uitpakken/vervangen/committen.

**Files:**
- Create: `internal/services/wordfence_update_service.go`
- Test: `internal/services/wordfence_update_service_test.go`

**Interfaces:**
- Consumes: `GitService` (DefaultBranch, GetStatus, CreateBranch, StashSave, StageAll, Commit); `ProjectService.List()`; `wporg` client (test seam interface `pluginDownloader`); `wpplugins.PluginsDir`.
- Produces:
  - `type UpdateSelection struct{ ProjectID string; Slugs []string }`
  - `type PluginUpdateResult struct{ Slug, From, To, Status, Error string }`
  - `type ProjectUpdateResult struct{ ProjectID, ProjectName, Status, Branch, Error string; Plugins []PluginUpdateResult }`
  - `func NewWordfenceUpdateService(git *GitService, projects *ProjectService) *WordfenceUpdateService`
  - `func (s *WordfenceUpdateService) ApplyProject(sel UpdateSelection, autoStash bool) ProjectUpdateResult`
  - Project-status waarden: `"updated"`, `"needs_stash"`, `"skipped_no_release"`, `"error"`, `"nothing"`.
  - `func extractZipReplace(zipData []byte, pluginsDir, slug string) error` (unexported, tested)

- [ ] **Step 1: Write the failing test**

```go
package services

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func makeZip(t *testing.T, slug, filename, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create(slug + "/" + filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractZipReplace(t *testing.T) {
	root := t.TempDir()
	pluginsDir := filepath.Join(root, "public", "wp-content", "plugins")
	old := filepath.Join(pluginsDir, "cf7")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	// stale file that must be gone after replace
	if err := os.WriteFile(filepath.Join(old, "stale.php"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	zipData := makeZip(t, "cf7", "wp-cf7.php", "<?php // v5.9.2")
	if err := extractZipReplace(zipData, pluginsDir, "cf7"); err != nil {
		t.Fatalf("extractZipReplace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(old, "stale.php")); !os.IsNotExist(err) {
		t.Error("stale file should be removed")
	}
	got, err := os.ReadFile(filepath.Join(old, "wp-cf7.php"))
	if err != nil || string(got) != "<?php // v5.9.2" {
		t.Errorf("new file missing/wrong: %v %q", err, got)
	}
}

func TestExtractZipReplaceRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	pluginsDir := filepath.Join(root, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("../evil.php")
	_, _ = f.Write([]byte("x"))
	_ = zw.Close()
	if err := extractZipReplace(buf.Bytes(), pluginsDir, "evil"); err == nil {
		t.Error("expected error on path traversal")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/ -run TestExtractZip -v`
Expected: FAIL (`undefined: extractZipReplace`).

- [ ] **Step 3: Write the implementation**

```go
// internal/services/wordfence_update_service.go
package services

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/wpplugins"
	"github.com/rdm/sites-tool/internal/adapters/wporg"
	"github.com/rdm/sites-tool/internal/domain"
)

// pluginDownloader is the subset of the wp.org client used here (test seam).
type pluginDownloader interface {
	LatestVersion(ctx context.Context, slug string) (string, string, error)
	Download(ctx context.Context, url string) ([]byte, error)
}

type UpdateSelection struct {
	ProjectID string   `json:"projectId"`
	Slugs     []string `json:"slugs"`
}

type PluginUpdateResult struct {
	Slug   string `json:"slug"`
	From   string `json:"from"`
	To     string `json:"to"`
	Status string `json:"status"` // updated | manual | error
	Error  string `json:"error"`
}

type ProjectUpdateResult struct {
	ProjectID   string               `json:"projectId"`
	ProjectName string               `json:"projectName"`
	Status      string               `json:"status"` // updated | needs_stash | skipped_no_release | error | nothing
	Branch      string               `json:"branch"`
	Error       string               `json:"error"`
	Plugins     []PluginUpdateResult `json:"plugins"`
}

type WordfenceUpdateService struct {
	git      *GitService
	projects *ProjectService
	wporg    pluginDownloader
}

func NewWordfenceUpdateService(git *GitService, projects *ProjectService) *WordfenceUpdateService {
	return &WordfenceUpdateService{git: git, projects: projects, wporg: wporg.NewClient()}
}

func (s *WordfenceUpdateService) projectByID(id string) (domain.Project, bool) {
	for _, p := range s.projects.List() {
		if p.ID == id {
			return p, true
		}
	}
	return domain.Project{}, false
}

// ApplyProject updates the selected plugins in one project. When the worktree
// is dirty and autoStash is false, it returns status "needs_stash" without
// changing anything; the frontend re-calls with autoStash=true after the user
// confirms.
func (s *WordfenceUpdateService) ApplyProject(sel UpdateSelection, autoStash bool) ProjectUpdateResult {
	p, ok := s.projectByID(sel.ProjectID)
	res := ProjectUpdateResult{ProjectID: sel.ProjectID, ProjectName: p.DisplayName}
	if !ok {
		res.Status = "error"
		res.Error = "project niet gevonden"
		return res
	}
	if len(sel.Slugs) == 0 {
		res.Status = "nothing"
		return res
	}

	// 1. Default branch must match release/*.
	def, err := s.git.DefaultBranch(sel.ProjectID)
	if err != nil {
		res.Status = "error"
		res.Error = err.Error()
		return res
	}
	if !strings.HasPrefix(def, "release/") {
		res.Status = "skipped_no_release"
		res.Error = fmt.Sprintf("default branch %q voldoet niet aan release/*", def)
		return res
	}

	// 2. Dirty check.
	st, err := s.git.GetStatus(sel.ProjectID)
	if err != nil {
		res.Status = "error"
		res.Error = err.Error()
		return res
	}
	if isDirty(st) {
		if !autoStash {
			res.Status = "needs_stash"
			return res
		}
		if err := s.git.StashSave(sel.ProjectID, "wordfence-update auto-stash"); err != nil {
			res.Status = "error"
			res.Error = "stash mislukt: " + err.Error()
			return res
		}
	}

	// 3. Create branch from default.
	branch := "security/wordfence-" + time.Now().Format("2006-01-02")
	if err := s.git.CreateBranch(sel.ProjectID, branch, def); err != nil {
		res.Status = "error"
		res.Error = "branch aanmaken mislukt: " + err.Error()
		return res
	}
	res.Branch = branch

	// 4. Update each plugin.
	pluginsDir := wpplugins.PluginsDir(p.Path)
	ctx := context.Background()
	anyUpdated := false
	for _, slug := range sel.Slugs {
		pr := PluginUpdateResult{Slug: slug}
		installed := currentVersion(pluginsDir, slug)
		pr.From = installed

		ver, url, err := s.wporg.LatestVersion(ctx, slug)
		if err != nil {
			pr.Status = "manual"
			pr.Error = "niet op wp.org"
			res.Plugins = append(res.Plugins, pr)
			continue
		}
		pr.To = ver
		data, err := s.wporg.Download(ctx, url)
		if err != nil {
			pr.Status = "error"
			pr.Error = err.Error()
			res.Plugins = append(res.Plugins, pr)
			continue
		}
		if err := extractZipReplace(data, pluginsDir, slug); err != nil {
			pr.Status = "error"
			pr.Error = err.Error()
			res.Plugins = append(res.Plugins, pr)
			continue
		}
		if err := s.git.StageAll(sel.ProjectID); err != nil {
			pr.Status = "error"
			pr.Error = err.Error()
			res.Plugins = append(res.Plugins, pr)
			continue
		}
		msg := fmt.Sprintf("fix(security): update %s %s→%s (Wordfence)", slug, installed, ver)
		if err := s.git.Commit(sel.ProjectID, msg, false); err != nil {
			pr.Status = "error"
			pr.Error = err.Error()
			res.Plugins = append(res.Plugins, pr)
			continue
		}
		pr.Status = "updated"
		anyUpdated = true
		res.Plugins = append(res.Plugins, pr)
	}

	if anyUpdated {
		res.Status = "updated"
	} else {
		res.Status = "nothing"
	}
	return res
}

func isDirty(st domain.GitStatus) bool {
	return len(st.Staged) > 0 || len(st.Unstaged) > 0 || len(st.Untracked) > 0 || len(st.Conflicted) > 0
}

func currentVersion(pluginsDir, slug string) string {
	// Reuse the reader against the parent-of-pluginsDir project root.
	// pluginsDir = <root>/public/wp-content/plugins
	root := filepath.Dir(filepath.Dir(filepath.Dir(pluginsDir)))
	installed, _ := wpplugins.ReadInstalled(root)
	for _, ip := range installed {
		if ip.Slug == slug {
			return ip.Version
		}
	}
	return ""
}

// extractZipReplace removes plugins/<slug> and extracts the plugin zip into it.
// wp.org zips contain a top-level <slug>/ directory.
func extractZipReplace(zipData []byte, pluginsDir, slug string) error {
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	target := filepath.Join(pluginsDir, slug)
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("remove old plugin: %w", err)
	}
	cleanBase := filepath.Clean(pluginsDir) + string(os.PathSeparator)
	for _, f := range zr.File {
		dest := filepath.Join(pluginsDir, f.Name)
		if !strings.HasPrefix(filepath.Clean(dest)+string(os.PathSeparator), cleanBase) &&
			filepath.Clean(dest) != filepath.Clean(pluginsDir) {
			return fmt.Errorf("unsafe path in zip: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		out.Close()
		rc.Close()
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/services/ -run TestExtractZip -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/wordfence_update_service.go internal/services/wordfence_update_service_test.go
git commit -m "feat(wordfence): bulk plugin update runner (branch + stash + replace + commit)"
```

---

### Task 10: Register services + regenerate bindings

Registreer beide services in `app.go` en genereer de Wails-bindings zodat de frontend ze (en het nieuwe settings-veld) kan aanroepen.

**Files:**
- Modify: `internal/app/app.go`

**Interfaces:**
- Consumes: `NewWordfenceService` (Task 7), `NewWordfenceUpdateService` (Task 9).
- Produces: `Services.Wordfence` + `Services.WordfenceUpdate` en hun Wails-registratie; TS-bindings onder `frontend/bindings/...`.

- [ ] **Step 1: Add fields to the Services struct**

In `internal/app/app.go`, add to `type Services struct`:

```go
	Wordfence       *services.WordfenceService
	WordfenceUpdate *services.WordfenceUpdateService
```

- [ ] **Step 2: Construct them in NewServices**

In `NewServices`, before the `return &Services{...}`, add:

```go
	git := services.NewGitService(project)
	wordfence := services.NewWordfenceService(&cfg.Global, project)
	wordfenceUpdate := services.NewWordfenceUpdateService(git, project)
```

Then change the returned struct to reuse `git` and add the two fields:

```go
		Git:             git,
		// ... existing fields ...
		Wordfence:       wordfence,
		WordfenceUpdate: wordfenceUpdate,
```

(Replace the existing `Git: services.NewGitService(project),` line with `Git: git,`.)

- [ ] **Step 3: Register in Wails()**

In `func (s *Services) Wails()`, add to the returned slice:

```go
		application.NewService(s.Wordfence),
		application.NewService(s.WordfenceUpdate),
```

- [ ] **Step 4: Build the backend**

Run: `go build ./...`
Expected: compiles with no errors.

- [ ] **Step 5: Regenerate bindings**

Run: `wails3 generate bindings -d frontend/bindings`
(If the flag differs for this Wails alpha, run `task dev` briefly which regenerates bindings on start, then stop it.)
Expected: new/updated files under `frontend/bindings/github.com/rdm/sites-tool/internal/services` for `WordfenceService` and `WordfenceUpdateService`, and `AppSettings` now includes `wordfenceApiKey`.

- [ ] **Step 6: Commit**

```bash
git add internal/app/app.go frontend/bindings
git commit -m "feat(app): register Wordfence services + regenerate bindings"
```

---

### Task 11: Frontend — WordfencePage component

Het nieuwe paneel: feed ophalen/tonen, vergelijken met projecten, selecteren en bulk-updaten met stash-afhandeling.

**Files:**
- Create: `frontend/src/components/WordfencePage.tsx`

**Interfaces:**
- Consumes (bindings): `WordfenceService.Refresh()`, `WordfenceService.List()`, `WordfenceService.LastFetched()`, `WordfenceService.MatchProjects()`, `WordfenceUpdateService.ApplyProject(sel, autoStash)`.
- Produces: `export default function WordfencePage({ onClose }: { onClose: () => void })`.

- [ ] **Step 1: Create the component**

```tsx
import { useEffect, useState, useCallback } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type {
  Vulnerability,
  ProjectVulnReport,
  ProjectUpdateResult,
} from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'

interface Props { onClose: () => void }

const SHOW_LIMIT = 50

export default function WordfencePage({ onClose }: Props) {
  const [vulns, setVulns] = useState<Vulnerability[]>([])
  const [limit, setLimit] = useState(SHOW_LIMIT)
  const [meta, setMeta] = useState<{ fetchedAt: string; count: number } | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [reports, setReports] = useState<ProjectVulnReport[] | null>(null)
  const [selected, setSelected] = useState<Record<string, Set<string>>>({})
  const [results, setResults] = useState<Record<string, ProjectUpdateResult>>({})

  const loadCache = useCallback(async () => {
    const list = (await Services.WordfenceService.List()) ?? []
    list.sort((a, b) => (b.published ?? '').localeCompare(a.published ?? ''))
    setVulns(list)
    setMeta(await Services.WordfenceService.LastFetched())
  }, [])

  useEffect(() => { loadCache().catch(e => setError(String(e))) }, [loadCache])

  const refresh = async () => {
    setBusy(true); setError(null)
    try {
      await Services.WordfenceService.Refresh()
      await loadCache()
    } catch (e) { setError(String(e)) } finally { setBusy(false) }
  }

  const compare = async () => {
    setBusy(true); setError(null); setResults({})
    try {
      const reps = (await Services.WordfenceService.MatchProjects()) ?? []
      setReports(reps)
      // preselect all wporg-sourced findings
      const pre: Record<string, Set<string>> = {}
      for (const r of reps) {
        pre[r.projectId] = new Set(
          r.findings.filter(f => f.source === 'wporg').map(f => f.slug),
        )
      }
      setSelected(pre)
    } catch (e) { setError(String(e)) } finally { setBusy(false) }
  }

  const toggle = (pid: string, slug: string) => {
    setSelected(prev => {
      const next = { ...prev }
      const set = new Set(next[pid] ?? [])
      set.has(slug) ? set.delete(slug) : set.add(slug)
      next[pid] = set
      return next
    })
  }

  const applyProject = async (pid: string, autoStash: boolean) => {
    const slugs = Array.from(selected[pid] ?? [])
    if (slugs.length === 0) return
    setBusy(true)
    try {
      const res = await Services.WordfenceUpdateService.ApplyProject({ projectId: pid, slugs }, autoStash)
      setResults(prev => ({ ...prev, [pid]: res }))
    } catch (e) {
      setError(String(e))
    } finally { setBusy(false) }
  }

  const updateSelected = async () => {
    for (const r of reports ?? []) {
      if ((selected[r.projectId]?.size ?? 0) > 0) {
        await applyProject(r.projectId, false)
      }
    }
  }

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-hidden bg-bg">
      <div className="h-14 px-6 bg-panel border-b border-border shrink-0 flex items-center gap-3">
        <h2 className="text-[15px] font-bold text-fg flex-1">Wordfence kwetsbaarheden</h2>
        <button onClick={refresh} disabled={busy}
          className="px-3 py-1.5 bg-accent hover:bg-accent-2 text-white text-[13px] font-semibold rounded-lg disabled:opacity-50">
          {busy ? 'Bezig…' : 'Vernieuwen'}
        </button>
        <button onClick={onClose} className="text-fg-muted hover:text-fg text-lg leading-none" title="Sluiten">✕</button>
      </div>

      <div className="flex-1 overflow-y-auto px-6 py-5 space-y-6">
        {error && <p className="text-[12.5px] text-red">{error}</p>}
        {meta && (
          <p className="text-[11px] text-fg-faint">
            {meta.count} kwetsbaarheden · laatst opgehaald {meta.fetchedAt ? new Date(meta.fetchedAt).toLocaleString() : '—'}
          </p>
        )}

        <div>
          <div className="flex items-center gap-3 mb-2">
            <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase">Feed</h3>
            <button onClick={compare} disabled={busy || vulns.length === 0}
              className="ml-auto px-3 py-1 bg-panel border border-border rounded-lg text-[12px] hover:bg-hover disabled:opacity-50">
              Vergelijk met projecten
            </button>
          </div>
          <ul className="space-y-1">
            {vulns.slice(0, limit).map(v => (
              <li key={v.id} className="text-[12.5px] text-fg-muted border-b border-border/50 py-1">
                <span className="font-mono text-fg">{v.software?.[0]?.slug ?? '—'}</span>
                {' · '}{v.title}
                {v.cve && <span className="ml-2 text-fg-faint">{v.cve}</span>}
              </li>
            ))}
          </ul>
          {vulns.length > limit && (
            <button onClick={() => setLimit(l => l + SHOW_LIMIT)}
              className="mt-2 text-[12px] text-accent hover:underline">Meer laden ({vulns.length - limit})</button>
          )}
        </div>

        {reports && (
          <div className="space-y-4">
            <div className="flex items-center gap-3">
              <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase">Getroffen projecten</h3>
              <button onClick={updateSelected} disabled={busy}
                className="ml-auto px-3 py-1.5 bg-accent hover:bg-accent-2 text-white text-[13px] font-semibold rounded-lg disabled:opacity-50">
                Update geselecteerde
              </button>
            </div>
            {reports.length === 0 && <p className="text-[12.5px] text-fg-faint">Geen kwetsbare plugins gevonden.</p>}
            {reports.map(r => {
              const res = results[r.projectId]
              return (
                <div key={r.projectId} className="border border-border rounded-lg p-3">
                  <p className="text-[13px] font-semibold text-fg mb-2">{r.projectName}</p>
                  <ul className="space-y-1">
                    {r.findings.map(f => (
                      <li key={f.slug} className="flex items-center gap-2 text-[12.5px]">
                        <input type="checkbox"
                          checked={selected[r.projectId]?.has(f.slug) ?? false}
                          disabled={f.source === 'manual'}
                          onChange={() => toggle(r.projectId, f.slug)} />
                        <span className="font-mono text-fg">{f.slug}</span>
                        <span className="text-fg-faint">{f.installedVersion} → {f.latestVersion || '?'}</span>
                        {f.cve && <span className="text-fg-faint">{f.cve}</span>}
                        {f.source === 'manual' && <span className="text-amber">handmatig (niet op wp.org)</span>}
                      </li>
                    ))}
                  </ul>
                  {res?.status === 'needs_stash' && (
                    <div className="mt-2 text-[12px] text-amber flex items-center gap-2">
                      Werkboom heeft wijzigingen.
                      <button onClick={() => applyProject(r.projectId, true)}
                        className="px-2 py-0.5 bg-amber/20 border border-amber/40 rounded text-amber hover:bg-amber/30">
                        Stash &amp; doorgaan
                      </button>
                    </div>
                  )}
                  {res?.status === 'skipped_no_release' && (
                    <p className="mt-2 text-[12px] text-fg-faint">Overgeslagen: {res.error}</p>
                  )}
                  {res?.status === 'error' && <p className="mt-2 text-[12px] text-red">Fout: {res.error}</p>}
                  {res?.status === 'updated' && (
                    <p className="mt-2 text-[12px] text-green">Bijgewerkt op branch <span className="font-mono">{res.branch}</span></p>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
```

> Note: exact TS type names (`Vulnerability`, `ProjectVulnReport`, `ProjectUpdateResult`) come from the generated bindings in Task 10. If a generated name differs (e.g. namespaced), adjust the import to match the actual path under `frontend/bindings`.

- [ ] **Step 2: Type-check the frontend**

Run: `cd frontend && npx tsc --noEmit`
Expected: no type errors (after Task 10 bindings exist).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/WordfencePage.tsx
git commit -m "feat(ui): Wordfence vulnerabilities & bulk-update panel"
```

---

### Task 12: Wire the global menu item in App.tsx

Voeg de knop + paneel-state toe aan de sidebar-header, wederzijds uitsluitend met de andere panelen.

**Files:**
- Modify: `frontend/src/App.tsx`

**Interfaces:**
- Consumes: `WordfencePage` (Task 11).

- [ ] **Step 1: Import and add state**

Near the top imports:

```tsx
import WordfencePage from './components/WordfencePage'
```

With the other panel state (`showBatch`/`showSearch`/`showSettings`):

```tsx
  const [showWordfence, setShowWordfence] = useState(false)
```

- [ ] **Step 2: Add the toolbar button**

In the header `IconBtn` group (next to Search/Batch), add:

```tsx
          <IconBtn onClick={() => { setShowWordfence(w => !w); setShowSearch(false); setShowBatch(false) }} title="Wordfence kwetsbaarheden" drag>
            🛡
          </IconBtn>
```

And ensure the other toggles also close Wordfence — add `setShowWordfence(false)` to the Search and Batch `onClick` handlers and to the Settings button handler.

- [ ] **Step 3: Render the panel**

In the main content conditional chain (`showSettings ? … : showSearch ? … : showBatch ? …`), add a branch before `showBatch` (or anywhere in the chain):

```tsx
      ) : showWordfence ? (
        <ErrorBoundary>
          <WordfencePage onClose={() => setShowWordfence(false)} />
        </ErrorBoundary>
```

- [ ] **Step 4: Type-check + run**

Run: `cd frontend && npx tsc --noEmit`
Expected: no type errors.

Run: `task dev`
Expected: app starts; the 🛡 button opens the Wordfence panel; Settings shows a Wordfence API-key field.

- [ ] **Step 5: Manual verification**

- Zet een Wordfence API-key in Instellingen → Wordfence, sla op.
- Open het 🛡-paneel, klik Vernieuwen → aantal + tijd verschijnen; feed-lijst toont 50 met "Meer laden".
- Klik "Vergelijk met projecten" → getroffen projecten met checkboxes.
- Klik "Update geselecteerde" → per project resultaat; bij dirty worktree verschijnt "Stash & doorgaan".

- [ ] **Step 6: Commit**

```bash
git add frontend/src/App.tsx
git commit -m "feat(ui): global Wordfence menu item in sidebar"
```

---

## Final Verification

- [ ] Run full backend tests: `go test ./...` → all pass.
- [ ] Run `go build ./...` → clean.
- [ ] Run `cd frontend && npx tsc --noEmit` → clean.
- [ ] Manual smoke test per Task 12, Step 5.

## Self-Review Notes

- **Spec coverage:** globaal menu-item (T11/T12), feed ophalen + opslaan (T3/T7), 50 tonen + meer laden (T11), vergelijken met `public/wp-content/plugins/*/` (T5/T7), bulk-update per project (T9), branch vanaf release/* (T8/T9), dirty-worktree melding + stash (T9/T11), API-key in settings (T6). Alle spec-secties gedekt.
- **wp.org-only:** betaalde/onbekende plugins → status `manual`, checkbox disabled, niet geüpdatet (T7/T9/T11).
- **Type-consistentie:** `ApplyProject(sel, autoStash)` gebruikt overal `UpdateSelection{projectId, slugs}`; statuswaarden identiek in backend en UI-checks.
