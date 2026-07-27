# Rapportage versie-prefill + opmerkingenveld — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De "Server software & frameworks"-tabel in de rapportage automatisch vullen (PHP productie/lokaal, Node, MariaDB-LTS, WordPress met Laatste + Ondersteund-tot via endoflife.date) en een vrij opmerkingenveld toevoegen aan rapport en PDF.

**Architecture:** Nieuwe kleine adapter `internal/adapters/endoflife` (HTTP + 24u in-memory cache). Parser- en selectiehelpers in `internal/services/report_versions.go`. `ReportService.Prefill` krijgt twee extra best-effort stappen (`prefillFromRepo`, `prefillEOL`) via test-seam interfaces, zoals de bestaande Kinsta/security-prefill. Frontend: één extra `SectionCard` met textarea; bindings regenereren.

**Tech Stack:** Go 1.x (stdlib, geen nieuwe deps), Wails v3 alpha, React/TS frontend, endoflife.date JSON API.

**Spec:** `docs/superpowers/specs/2026-07-27-report-version-prefill-design.md`

## Global Constraints

- Branch: `feature/report-version-prefill`. Conventional commits, **geen** Co-Authored-By (attributie staat globaal uit).
- Alle prefill-bronnen zijn best-effort en non-fataal; een cel wordt alleen overschreven als de bron een waarde oplevert.
- "Ondersteund tot" = `eol`-datum (security-einde) van de cycle van de **huidige** versie, formaat `dd-mm-jjjj` (`t.Format("02-01-2006")`).
- Componentnamen exact: `PHP (productie)`, `PHP (lokaal)`, `MariaDB`, `Node`, `WordPress`.
- Versienormalisatie: `php8.3`→`8.3`, suffix na eerste `-` strippen (`8.3-jit`→`8.3`, `24.16.0-bf3`→`24.16.0`), leading `v` strippen. Tags met `$` (variabelen zoals `${TAG_NODE}`) → lege string.
- Repo-bestanden lezen op `origin/<default branch>` (val terug op lokale default branch, dan werkmap) — zelfde aanpak als `InventoryService.projectRef`.
- Go: `gofmt`, table-driven tests, `go test -race ./...` groen voor elke commit.
- Wails-omgeving: `wails3` zit in `~/go/bin`; bindings regenereren met `-ts` vlag. Nooit twee dev-servers tegelijk.

---

### Task 1: endoflife.date adapter

**Files:**
- Create: `internal/adapters/endoflife/client.go`
- Test: `internal/adapters/endoflife/client_test.go`

**Interfaces:**
- Consumes: niets (stdlib).
- Produces: `endoflife.NewClient() *Client`, `(*Client).Cycles(ctx, product string) ([]Cycle, error)`, `type Cycle struct{ Cycle, Latest string; LTS, EOL, Support Flex }`, `type Flex struct{ IsDate bool; Date time.Time; Bool bool }`. Task 2 en 5 gebruiken deze types.

- [ ] **Step 1: Schrijf de falende test**

```go
package endoflife

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// phpJSON is een ingekorte echte respons van endoflife.date/api/php.json.
// Let op de union-types: lts/eol/support zijn bool óf "YYYY-MM-DD".
const phpJSON = `[
  {"cycle":"8.5","eol":"2029-12-31","latest":"8.5.8","lts":false,"support":"2027-12-31"},
  {"cycle":"8.3","eol":"2027-12-31","latest":"8.3.32","lts":false,"support":"2025-12-31"}
]`

func newTestClient(t *testing.T, hits *int, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := NewClient()
	c.baseURL = srv.URL
	return c
}

func TestCyclesParsesUnionFields(t *testing.T) {
	hits := 0
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
	hits := 0
	c := newTestClient(t, &hits, phpJSON)

	for i := 0; i < 3; i++ {
		if _, err := c.Cycles(context.Background(), "php"); err != nil {
			t.Fatalf("Cycles #%d: %v", i, err)
		}
	}
	if hits != 1 {
		t.Errorf("http hits = %d, want 1 (cache)", hits)
	}

	c.now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	if _, err := c.Cycles(context.Background(), "php"); err != nil {
		t.Fatalf("Cycles na TTL: %v", err)
	}
	if hits != 2 {
		t.Errorf("http hits = %d, want 2 (TTL verlopen)", hits)
	}
}
```

- [ ] **Step 2: Run test — verwacht FAIL**

Run: `go test ./internal/adapters/endoflife/ -run TestCycles -v`
Expected: compile-fout ("undefined: Client" e.d.)

- [ ] **Step 3: Minimale implementatie**

```go
// Package endoflife is een kleine client voor https://endoflife.date/api,
// met een in-memory cache van 24 uur per product (feed verandert zelden en
// prefill mag geen herhaalde requests veroorzaken).
package endoflife

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Flex is endoflife.date's union-veld: false | true | "YYYY-MM-DD".
type Flex struct {
	IsDate bool
	Date   time.Time
	Bool   bool
}

func (f *Flex) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return fmt.Errorf("endoflife: ongeldige datum %q: %w", s, err)
		}
		f.IsDate, f.Date = true, t
		return nil
	}
	var v bool
	if err := json.Unmarshal(b, &v); err != nil {
		return fmt.Errorf("endoflife: veld is geen datum of bool: %w", err)
	}
	f.Bool = v
	return nil
}

// Cycle is één release-lijn van een product.
type Cycle struct {
	Cycle   string `json:"cycle"`
	Latest  string `json:"latest"`
	LTS     Flex   `json:"lts"`
	EOL     Flex   `json:"eol"`
	Support Flex   `json:"support"`
}

type cacheEntry struct {
	cycles  []Cycle
	fetched time.Time
}

// Client haalt release-cycli op met een TTL-cache per product.
type Client struct {
	http    *http.Client
	baseURL string
	ttl     time.Duration
	now     func() time.Time

	mu    sync.Mutex
	cache map[string]cacheEntry
}

func NewClient() *Client {
	return &Client{
		http:    &http.Client{Timeout: 15 * time.Second},
		baseURL: "https://endoflife.date/api",
		ttl:     24 * time.Hour,
		now:     time.Now,
		cache:   map[string]cacheEntry{},
	}
}

// Cycles geeft de release-cycli voor product (bijv. "php", "nodejs").
func (c *Client) Cycles(ctx context.Context, product string) ([]Cycle, error) {
	c.mu.Lock()
	if e, ok := c.cache[product]; ok && c.now().Sub(e.fetched) < c.ttl {
		c.mu.Unlock()
		return e.cycles, nil
	}
	c.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/"+product+".json", nil)
	if err != nil {
		return nil, fmt.Errorf("endoflife: request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("endoflife: fetch %s: %w", product, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("endoflife: fetch %s: status %d", product, resp.StatusCode)
	}
	var cycles []Cycle
	if err := json.NewDecoder(resp.Body).Decode(&cycles); err != nil {
		return nil, fmt.Errorf("endoflife: decode %s: %w", product, err)
	}

	c.mu.Lock()
	c.cache[product] = cacheEntry{cycles: cycles, fetched: c.now()}
	c.mu.Unlock()
	return cycles, nil
}
```

- [ ] **Step 4: Run test — verwacht PASS**

Run: `go test ./internal/adapters/endoflife/ -race -v`
Expected: PASS (beide tests)

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/endoflife/
git commit -m "feat(endoflife): adapter voor endoflife.date met 24u cache"
```

---

### Task 2: Versie-parsers en selectielogica

**Files:**
- Create: `internal/services/report_versions.go`
- Test: `internal/services/report_versions_test.go`

**Interfaces:**
- Consumes: `endoflife.Cycle`/`Flex` (Task 1), bestaand `compareVersions` (`internal/services/version.go`), `gitcli.ShowFile`, `gitcli.DefaultBranch`, `gitcli.RefExists`.
- Produces (gebruikt door Task 4/5):
  - `normalizeVersion(raw string) string`
  - `phpFromDockerfile(content []byte) string`
  - `nodeFromCompose(content []byte) string`
  - `nodeFromDockerfile(content []byte) string`
  - `latestActive(product string, cycles []endoflife.Cycle, now time.Time) string`
  - `supportedUntil(current string, cycles []endoflife.Cycle) string`
  - `type repoFileReader interface{ ReadProjectFile(p domain.Project, relPath string) ([]byte, error) }`
  - `type GitRepoFiles struct{}` (implementatie op origin/default met werkmap-fallback)

- [ ] **Step 1: Schrijf de falende tests** (table-driven; fixtures naar echte projectbestanden)

```go
package services

import (
	"testing"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/endoflife"
)

func TestNormalizeVersion(t *testing.T) {
	tests := []struct{ in, want string }{
		{"php8.3", "8.3"},
		{"8.3-jit", "8.3"},
		{"24.16.0-bf3", "24.16.0"},
		{"v20.1", "20.1"},
		{"  22.17 ", "22.17"},
		{"${TAG_NODE}", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeVersion(tt.in); got != tt.want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPHPFromDockerfile(t *testing.T) {
	tests := []struct {
		name, content, want string
	}{
		{"bitfactory dev", "FROM europe-docker.pkg.dev/bitfactory-nl/service-php/php:8.3-jit\nRUN true", "8.3"},
		{"bitfactory prod multi-stage", "FROM europe-docker.pkg.dev/bitfactory-nl/service-php/php:8.3 AS composer\nFROM europe-docker.pkg.dev/bitfactory-nl/service-node/node:20.12.2 AS frontend", "8.3"},
		{"plain image", "FROM php:8.2-fpm-alpine", "8.2"},
		{"geen php", "FROM nginx:1.23", ""},
	}
	for _, tt := range tests {
		if got := phpFromDockerfile([]byte(tt.content)); got != tt.want {
			t.Errorf("%s: phpFromDockerfile = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestNodeFromComposeAndDockerfile(t *testing.T) {
	compose := "services:\n  node:\n    image: europe-docker.pkg.dev/bitfactory-nl/service-node/node:24.16.0-bf3\n"
	if got := nodeFromCompose([]byte(compose)); got != "24.16.0" {
		t.Errorf("nodeFromCompose = %q, want 24.16.0", got)
	}
	variable := "services:\n  node:\n    image: europe-docker.pkg.dev/bitfactory-nl/service-node/node:${TAG_NODE}\n"
	if got := nodeFromCompose([]byte(variable)); got != "" {
		t.Errorf("nodeFromCompose(variabele) = %q, want \"\"", got)
	}
	dockerfile := "FROM europe-docker.pkg.dev/bitfactory-nl/service-node/node:20.12.2 AS frontend\n"
	if got := nodeFromDockerfile([]byte(dockerfile)); got != "20.12.2" {
		t.Errorf("nodeFromDockerfile = %q, want 20.12.2", got)
	}
}

// eolFlex bouwt een Flex uit een datumstring, of een bool bij "".
func eolFlex(t *testing.T, date string, b bool) endoflife.Flex {
	t.Helper()
	if date == "" {
		return endoflife.Flex{Bool: b}
	}
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		t.Fatalf("eolFlex: %v", err)
	}
	return endoflife.Flex{IsDate: true, Date: d}
}

func TestLatestActive(t *testing.T) {
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	php := []endoflife.Cycle{
		{Cycle: "8.5", Latest: "8.5.8", Support: eolFlex(t, "2027-12-31", false), EOL: eolFlex(t, "2029-12-31", false)},
		{Cycle: "8.3", Latest: "8.3.32", Support: eolFlex(t, "2025-12-31", false), EOL: eolFlex(t, "2027-12-31", false)},
	}
	node := []endoflife.Cycle{
		// 26 wordt pas in oktober 2026 LTS: mag nu nog niet gekozen worden.
		{Cycle: "26", Latest: "26.5.0", LTS: eolFlex(t, "2026-10-28", false), EOL: eolFlex(t, "2029-04-30", false)},
		{Cycle: "24", Latest: "24.18.0", LTS: eolFlex(t, "2025-10-28", false), EOL: eolFlex(t, "2028-04-30", false)},
		{Cycle: "25", Latest: "25.9.0", LTS: eolFlex(t, "", false), EOL: eolFlex(t, "2026-06-01", false)},
	}
	maria := []endoflife.Cycle{
		{Cycle: "12.3", Latest: "12.3.2", LTS: eolFlex(t, "", true), EOL: eolFlex(t, "2029-06-30", false)},
		{Cycle: "12.2", Latest: "12.2.2", LTS: eolFlex(t, "", false), EOL: eolFlex(t, "2026-05-13", false)},
	}
	wp := []endoflife.Cycle{
		{Cycle: "7.0", Latest: "7.0.2", EOL: eolFlex(t, "", false)}, // eol=false: nog geen EOL
		{Cycle: "6.9", Latest: "6.9.5", EOL: eolFlex(t, "2026-05-20", false)},
	}
	tests := []struct {
		product string
		cycles  []endoflife.Cycle
		want    string
	}{
		{"php", php, "8.5.8"},
		{"nodejs", node, "24.18.0"},
		{"mariadb", maria, "12.3.2"},
		{"wordpress", wp, "7.0.2"},
		{"php", nil, ""},
	}
	for _, tt := range tests {
		if got := latestActive(tt.product, tt.cycles, now); got != tt.want {
			t.Errorf("latestActive(%s) = %q, want %q", tt.product, got, tt.want)
		}
	}
}

func TestSupportedUntil(t *testing.T) {
	cycles := []endoflife.Cycle{
		{Cycle: "8.3", EOL: eolFlex(t, "2027-12-31", false)},
		{Cycle: "24", EOL: eolFlex(t, "2028-04-30", false)},
		{Cycle: "7.0", EOL: eolFlex(t, "", false)}, // bool false: geen datum bekend
	}
	tests := []struct{ current, want string }{
		{"8.3", "31-12-2027"},
		{"8.3.32", "31-12-2027"},
		{"24.16.0", "30-04-2028"},
		{"7.0.2", ""}, // eol=false → leeg laten
		{"5.6", ""},   // onbekende cycle
		{"", ""},
	}
	for _, tt := range tests {
		if got := supportedUntil(tt.current, cycles); got != tt.want {
			t.Errorf("supportedUntil(%q) = %q, want %q", tt.current, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run tests — verwacht FAIL (compile-fout)**

Run: `go test ./internal/services/ -run 'TestNormalizeVersion|TestPHPFromDockerfile|TestNodeFrom|TestLatestActive|TestSupportedUntil' -v`

- [ ] **Step 3: Implementatie `report_versions.go`**

```go
package services

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/endoflife"
	"github.com/rdm/sites-tool/internal/adapters/gitcli"
	"github.com/rdm/sites-tool/internal/domain"
)

// normalizeVersion strips vendor-ruis van een versietag: "php8.3" -> "8.3",
// "24.16.0-bf3" -> "24.16.0", "v20.1" -> "20.1". Tags met een niet
// geresolvede variabele ("${TAG_NODE}") leveren "" op.
func normalizeVersion(raw string) string {
	v := strings.TrimSpace(strings.ToLower(raw))
	if v == "" || strings.Contains(v, "$") {
		return ""
	}
	v = strings.TrimPrefix(v, "php")
	v = strings.TrimPrefix(v, "v")
	if i := strings.Index(v, "-"); i >= 0 {
		v = v[:i]
	}
	return v
}

var (
	rePHPFrom  = regexp.MustCompile(`(?m)^\s*FROM\s+\S*php:([^\s]+)`)
	reNodeImg  = regexp.MustCompile(`(?m)image:\s*\S*node:([^\s"']+)`)
	reNodeFrom = regexp.MustCompile(`(?m)^\s*FROM\s+\S*node:([^\s]+)`)
)

// phpFromDockerfile leest de PHP-versie uit de eerste php-FROM-regel
// (bitfactory service-php of plain php-image).
func phpFromDockerfile(content []byte) string {
	m := rePHPFrom.FindSubmatch(content)
	if m == nil {
		return ""
	}
	return normalizeVersion(string(m[1]))
}

// nodeFromCompose leest de node-image-tag uit docker-compose.yaml.
func nodeFromCompose(content []byte) string {
	m := reNodeImg.FindSubmatch(content)
	if m == nil {
		return ""
	}
	return normalizeVersion(string(m[1]))
}

// nodeFromDockerfile leest de node-versie uit een FROM-regel (frontend stage).
func nodeFromDockerfile(content []byte) string {
	m := reNodeFrom.FindSubmatch(content)
	if m == nil {
		return ""
	}
	return normalizeVersion(string(m[1]))
}

// cycleActive meldt of een cycle nog niet EOL is op t.
func cycleActive(c endoflife.Cycle, t time.Time) bool {
	if c.EOL.IsDate {
		return c.EOL.Date.After(t)
	}
	return !c.EOL.Bool // eol=false: nog geen EOL-datum bekend
}

// latestActive kiest de "Laatste versie" per product:
// php: hoogste cycle met lopende active support; nodejs: hoogste cycle die al
// LTS is en nog niet EOL; mariadb: hoogste LTS-cycle die nog niet EOL is;
// overig (wordpress): hoogste niet-EOL cycle. Geeft de laatste patchrelease
// van die cycle terug.
func latestActive(product string, cycles []endoflife.Cycle, now time.Time) string {
	var best *endoflife.Cycle
	for i := range cycles {
		c := &cycles[i]
		var ok bool
		switch product {
		case "php":
			ok = c.Support.IsDate && c.Support.Date.After(now)
		case "nodejs":
			ok = c.LTS.IsDate && !c.LTS.Date.After(now) && cycleActive(*c, now)
		case "mariadb":
			ok = !c.LTS.IsDate && c.LTS.Bool && cycleActive(*c, now)
		default:
			ok = cycleActive(*c, now)
		}
		if !ok {
			continue
		}
		if best == nil || compareVersions(c.Cycle, best.Cycle) > 0 {
			best = c
		}
	}
	if best == nil {
		return ""
	}
	return best.Latest
}

// supportedUntil geeft de EOL-datum (dd-mm-jjjj) van de cycle waar current in
// valt, of "" als de cycle onbekend is of geen EOL-datum heeft.
func supportedUntil(current string, cycles []endoflife.Cycle) string {
	current = normalizeVersion(current)
	if current == "" {
		return ""
	}
	var match *endoflife.Cycle
	for i := range cycles {
		c := &cycles[i]
		if current != c.Cycle && !strings.HasPrefix(current, c.Cycle+".") {
			continue
		}
		if match == nil || len(c.Cycle) > len(match.Cycle) {
			match = c // langste (meest specifieke) cycle wint
		}
	}
	if match == nil || !match.EOL.IsDate {
		return ""
	}
	return match.EOL.Date.Format("02-01-2006")
}

// repoFileReader leest een projectbestand voor de rapportage-prefill
// (test seam voor GitRepoFiles).
type repoFileReader interface {
	ReadProjectFile(p domain.Project, relPath string) ([]byte, error)
}

// GitRepoFiles leest bestanden op origin/<default branch> zodat het rapport
// de gecommitte staat beschrijft, ook als de werkmap op een feature-branch
// staat. Zonder bruikbare ref valt hij terug op de werkmap.
type GitRepoFiles struct{}

func (GitRepoFiles) ReadProjectFile(p domain.Project, relPath string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if ref := projectFileRef(ctx, p.Path); ref != "" {
		if b, err := gitcli.ShowFile(ctx, p.Path, ref, relPath); err == nil {
			return b, nil
		}
	}
	return os.ReadFile(filepath.Join(p.Path, relPath))
}

// projectFileRef is de package-brede variant van InventoryService.projectRef:
// origin/<default> als die bestaat, anders de lokale default branch, anders
// "" (werkmap).
func projectFileRef(ctx context.Context, path string) string {
	def, err := gitcli.DefaultBranch(ctx, path)
	if err != nil || def == "" {
		return ""
	}
	if gitcli.RefExists(ctx, path, "origin/"+def) {
		return "origin/" + def
	}
	if gitcli.RefExists(ctx, path, def) {
		return def
	}
	return ""
}
```

Herstructureer daarna `InventoryService.projectRef` (in `internal/services/inventory_service.go`, regel ±198) tot een dunne wrapper zodat de logica op één plek leeft:

```go
func (s *InventoryService) projectRef(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return projectFileRef(ctx, path)
}
```

- [ ] **Step 4: Run tests — verwacht PASS**

Run: `go test ./internal/services/ -race -run 'TestNormalizeVersion|TestPHPFromDockerfile|TestNodeFrom|TestLatestActive|TestSupportedUntil' -v`
Run ook: `go test ./internal/services/ -race -run TestInventory` (wrapper-refactor mag niets breken)

- [ ] **Step 5: Commit**

```bash
git add internal/services/report_versions.go internal/services/report_versions_test.go internal/services/inventory_service.go
git commit -m "feat(report): versie-parsers en EOL-selectielogica voor prefill"
```

---

### Task 3: Opmerkingenveld in domain + PDF-template

**Files:**
- Modify: `internal/domain/report.go` (Report struct)
- Modify: `internal/services/report_template.html` (sectie + CSS)
- Test: `internal/services/report_template_test.go` (uitbreiden)

**Interfaces:**
- Produces: `domain.Report.Opmerkingen string` (json `"opmerkingen"`), gebruikt door Task 5 (frontend via bindings) en de bestaande `renderReportHTML`.

- [ ] **Step 1: Schrijf de falende test** (toevoegen aan `report_template_test.go`)

```go
func TestRenderReportHTMLOpmerkingen(t *testing.T) {
	r := domain.Report{ProjectID: "p", Period: "Q3 2026", Opmerkingen: "Regel één\nRegel twee"}
	html, err := renderReportHTML(r, "")
	if err != nil {
		t.Fatalf("renderReportHTML: %v", err)
	}
	if !strings.Contains(html, "Overige opmerkingen") || !strings.Contains(html, "Regel één") {
		t.Errorf("opmerkingen-sectie ontbreekt in html")
	}

	leeg, err := renderReportHTML(domain.Report{ProjectID: "p", Period: "Q3 2026"}, "")
	if err != nil {
		t.Fatalf("renderReportHTML leeg: %v", err)
	}
	if strings.Contains(leeg, "Overige opmerkingen") {
		t.Errorf("lege opmerkingen moeten geen sectie renderen")
	}
}
```

(Voeg `"strings"` toe aan de imports van de testfile als die er nog niet staat.)

- [ ] **Step 2: Run test — verwacht FAIL** (`Opmerkingen` bestaat niet)

Run: `go test ./internal/services/ -run TestRenderReportHTMLOpmerkingen -v`

- [ ] **Step 3: Implementatie**

In `internal/domain/report.go`, `Report` struct, direct boven `UpdatedAt`:

```go
	// Opmerkingen is vrije tekst van de developer/updater, onderaan het
	// rapport (en de PDF) getoond.
	Opmerkingen string `json:"opmerkingen"`
```

In `internal/services/report_template.html`, ná de AVG-sectie en vóór `<footer>`:

```html
  {{if .Opmerkingen}}
  <section>
    <h2>Overige opmerkingen</h2>
    <p class="opmerkingen">{{.Opmerkingen}}</p>
  </section>
  {{end}}
```

In het `<style>`-blok van dezelfde file (bij de andere regels):

```css
  .opmerkingen { white-space: pre-wrap; }
```

- [ ] **Step 4: Run tests — verwacht PASS**

Run: `go test ./internal/services/ -race -run TestRenderReport -v`

- [ ] **Step 5: Commit**

```bash
git add internal/domain/report.go internal/services/report_template.html internal/services/report_template_test.go
git commit -m "feat(report): vrij opmerkingenveld in rapportmodel en PDF"
```

---

### Task 4: Skeleton-split, migratie en Kinsta-hernoeming

**Files:**
- Modify: `internal/services/report_service.go` (constanten, `skeletonReport`, `GetReport`, `prefillFromKinsta`)
- Test: `internal/services/report_service_test.go`

**Interfaces:**
- Consumes: `normalizeVersion` (Task 2).
- Produces: constanten `compPHPProd = "PHP (productie)"`, `compPHPLocal = "PHP (lokaal)"`, `compMariaDB = "MariaDB"`, `compNode = "Node"`, `compWordPress = "WordPress"`; `migrateSoftwareRows(rows []domain.SoftwareRow) []domain.SoftwareRow`. Task 5 gebruikt de constanten.

- [ ] **Step 1: Schrijf de falende tests** (toevoegen aan `report_service_test.go`)

```go
func TestMigrateSoftwareRowsSplitsPHP(t *testing.T) {
	oud := []domain.SoftwareRow{
		{Component: "PHP", Huidig: "8.2", Opmerking: "handmatig"},
		{Component: "MariaDB"},
	}
	rows := migrateSoftwareRows(oud)
	if len(rows) != 3 {
		t.Fatalf("len = %d, want 3", len(rows))
	}
	if rows[0].Component != compPHPProd || rows[0].Huidig != "8.2" || rows[0].Opmerking != "handmatig" {
		t.Errorf("rij 0 = %+v, want hernoemde PHP-rij met behoud van waarden", rows[0])
	}
	if rows[1].Component != compPHPLocal {
		t.Errorf("rij 1 = %+v, want ingevoegde PHP (lokaal)", rows[1])
	}

	// Idempotent: nogmaals migreren verandert niets.
	again := migrateSoftwareRows(rows)
	if len(again) != 3 || again[1].Component != compPHPLocal {
		t.Errorf("migratie is niet idempotent: %+v", again)
	}
}
```

Pas daarnaast in de bestaande skeleton-test de verwachte componentnamen aan naar de vijf rijen: `PHP (productie)`, `PHP (lokaal)`, `MariaDB`, `Node`, `WordPress` (zoek de assertion op de `Software`-rijen van het skeleton). En in de bestaande Kinsta-prefilltest: verwachting `"PHP"` wordt `compPHPProd` en de waarde `"php8.3"` wordt genormaliseerd `"8.3"` (pas de fixture/assert daarop aan).

- [ ] **Step 2: Run tests — verwacht FAIL**

Run: `go test ./internal/services/ -run 'TestMigrate|TestSkeleton|TestPrefill' -v`

- [ ] **Step 3: Implementatie** (in `report_service.go`)

Constanten bovenin (na de imports):

```go
// Componentnamen in de "Server software & frameworks"-tabel. De prefill
// matcht op deze exacte strings.
const (
	compPHPProd   = "PHP (productie)"
	compPHPLocal  = "PHP (lokaal)"
	compMariaDB   = "MariaDB"
	compNode      = "Node"
	compWordPress = "WordPress"
)
```

`skeletonReport`: vervang de `Software`-slice door:

```go
		Software: []domain.SoftwareRow{
			{Component: compPHPProd},
			{Component: compPHPLocal},
			{Component: compMariaDB},
			{Component: compNode},
			{Component: compWordPress},
		},
```

Nieuwe functie + aanroep in `GetReport` (op het stored-pad, ná de skeleton-check):

```go
// migrateSoftwareRows werkt drafts van vóór de PHP-splitsing bij: de rij
// "PHP" wordt "PHP (productie)" en "PHP (lokaal)" wordt direct erna
// ingevoegd. Idempotent; geeft een nieuwe slice terug.
func migrateSoftwareRows(rows []domain.SoftwareRow) []domain.SoftwareRow {
	out := make([]domain.SoftwareRow, 0, len(rows)+1)
	heeftLokaal := false
	for _, row := range rows {
		if row.Component == compPHPLocal {
			heeftLokaal = true
		}
	}
	for _, row := range rows {
		if row.Component == "PHP" {
			row.Component = compPHPProd
			out = append(out, row)
			if !heeftLokaal {
				out = append(out, domain.SoftwareRow{Component: compPHPLocal})
			}
			continue
		}
		out = append(out, row)
	}
	return out
}
```

In `GetReport`, vervang `return stored, nil` door:

```go
	stored.Software = migrateSoftwareRows(stored.Software)
	return stored, nil
```

In `prefillFromKinsta`, vervang de twee `setSoftwareHuidig`-regels door:

```go
			setSoftwareHuidig(r, compPHPProd, normalizeVersion(e.ContainerInfo.PHPEngineVersion))
			setSoftwareHuidig(r, compWordPress, e.WordPressVersion)
```

- [ ] **Step 4: Run tests — verwacht PASS**

Run: `go test ./internal/services/ -race -run 'TestMigrate|TestSkeleton|TestPrefill|TestGetReport' -v`

- [ ] **Step 5: Commit**

```bash
git add internal/services/report_service.go internal/services/report_service_test.go
git commit -m "feat(report): PHP-splitsing in productie/lokaal met draft-migratie"
```

---

### Task 5: prefillFromRepo + prefillEOL + wiring

**Files:**
- Modify: `internal/services/report_service.go` (struct, constructor, `Prefill`, twee nieuwe prefill-functies, `setSoftware*`-helpers)
- Modify: `internal/app/app.go:56` (wiring)
- Test: `internal/services/report_service_test.go`

**Interfaces:**
- Consumes: Task 1 (`endoflife.Cycle`), Task 2 (parsers, `latestActive`, `supportedUntil`, `repoFileReader`, `GitRepoFiles`), Task 4 (constanten).
- Produces: `NewReportService(projects, kinsta, security, store, pdf, eol, repo)` — **let op:** bestaande aanroepen krijgen `, nil, nil` erbij; `reportEOL` interface.

- [ ] **Step 1: Schrijf de falende tests**

```go
type fakeEOL struct {
	byProduct map[string][]endoflife.Cycle
}

func (f *fakeEOL) Cycles(_ context.Context, product string) ([]endoflife.Cycle, error) {
	c, ok := f.byProduct[product]
	if !ok {
		return nil, fmt.Errorf("onbekend product %q", product)
	}
	return c, nil
}

type fakeRepoFiles struct {
	files map[string]string // relPath -> inhoud
}

func (f *fakeRepoFiles) ReadProjectFile(_ domain.Project, relPath string) ([]byte, error) {
	c, ok := f.files[relPath]
	if !ok {
		return nil, os.ErrNotExist
	}
	return []byte(c), nil
}

func TestPrefillFromRepoAndEOL(t *testing.T) {
	dir := t.TempDir()
	projects := &fakeReportProjects{projects: map[string]domain.Project{
		"p1": {DisplayName: "Klant", Path: "/tmp/x"},
	}}
	repo := &fakeRepoFiles{files: map[string]string{
		".bitfactory/docker/php-fpm/Dockerfile.dev": "FROM europe-docker.pkg.dev/bitfactory-nl/service-php/php:8.3-jit",
		"docker-compose.yaml":                       "services:\n  node:\n    image: europe-docker.pkg.dev/bitfactory-nl/service-node/node:24.10\n",
	}}
	eol := &fakeEOL{byProduct: map[string][]endoflife.Cycle{
		"php": {
			{Cycle: "8.5", Latest: "8.5.8", Support: eolFlex(t, "2027-12-31", false), EOL: eolFlex(t, "2029-12-31", false)},
			{Cycle: "8.3", Latest: "8.3.32", Support: eolFlex(t, "2025-12-31", false), EOL: eolFlex(t, "2027-12-31", false)},
		},
		"nodejs": {
			{Cycle: "24", Latest: "24.18.0", LTS: eolFlex(t, "2025-10-28", false), EOL: eolFlex(t, "2028-04-30", false)},
		},
		"mariadb": {
			{Cycle: "12.3", Latest: "12.3.2", LTS: eolFlex(t, "", true), EOL: eolFlex(t, "2029-06-30", false)},
		},
		"wordpress": {
			{Cycle: "7.0", Latest: "7.0.2", EOL: eolFlex(t, "", false)},
		},
	}}
	svc := NewReportService(projects, nil, nil, NewReportStore(dir), nil, eol, repo)

	r, err := svc.Prefill("p1", "Q3 2026")
	if err != nil {
		t.Fatalf("Prefill: %v", err)
	}
	get := func(component string) domain.SoftwareRow {
		for _, row := range r.Software {
			if row.Component == component {
				return row
			}
		}
		t.Fatalf("rij %q ontbreekt: %+v", component, r.Software)
		return domain.SoftwareRow{}
	}

	if row := get(compPHPLocal); row.Huidig != "8.3" || row.OndersteundTot != "31-12-2027" || row.Laatste != "8.5.8" {
		t.Errorf("PHP (lokaal) = %+v", row)
	}
	if row := get(compNode); row.Huidig != "24.10" || row.OndersteundTot != "30-04-2028" || row.Laatste != "24.18.0" {
		t.Errorf("Node = %+v", row)
	}
	// Geen Kinsta in deze test: PHP (productie) blijft leeg maar krijgt wel "Laatste".
	if row := get(compPHPProd); row.Huidig != "" || row.Laatste != "8.5.8" || row.OndersteundTot != "" {
		t.Errorf("PHP (productie) = %+v", row)
	}
	// MariaDB: alleen Laatste (Huidig onbekend).
	if row := get(compMariaDB); row.Laatste != "12.3.2" || row.OndersteundTot != "" {
		t.Errorf("MariaDB = %+v", row)
	}
}

func TestPrefillRepoFallbackNaarDockerfile(t *testing.T) {
	dir := t.TempDir()
	projects := &fakeReportProjects{projects: map[string]domain.Project{
		"p1": {DisplayName: "Klant", Path: "/tmp/x"},
	}}
	repo := &fakeRepoFiles{files: map[string]string{
		".bitfactory/docker/php-fpm/Dockerfile": "FROM europe-docker.pkg.dev/bitfactory-nl/service-php/php:8.2 AS composer\nFROM europe-docker.pkg.dev/bitfactory-nl/service-node/node:20.12.2 AS frontend",
	}}
	svc := NewReportService(projects, nil, nil, NewReportStore(dir), nil, nil, repo)

	r, err := svc.Prefill("p1", "Q3 2026")
	if err != nil {
		t.Fatalf("Prefill: %v", err)
	}
	for _, row := range r.Software {
		switch row.Component {
		case compPHPLocal:
			if row.Huidig != "8.2" {
				t.Errorf("PHP (lokaal) fallback = %q, want 8.2", row.Huidig)
			}
		case compNode:
			if row.Huidig != "20.12.2" {
				t.Errorf("Node fallback = %q, want 20.12.2", row.Huidig)
			}
		}
	}
}
```

(Imports van de testfile aanvullen met `"os"`, `"fmt"`, `"context"`, `endoflife`-package voor zover ze ontbreken.)

- [ ] **Step 2: Update bestaande constructor-aanroepen en run — verwacht FAIL op nieuw gedrag**

Alle bestaande `NewReportService(a, b, c, d, e)`-aanroepen in `report_service_test.go` krijgen `, nil, nil` erbij (10 plekken, mechanisch). Run: `go test ./internal/services/ -run TestPrefill -v` — nieuwe tests falen (functies bestaan nog niet), bestaande compileren weer.

- [ ] **Step 3: Implementatie** (in `report_service.go`)

Interface + struct + constructor:

```go
// reportEOL is de subset van *endoflife.Client die ReportService nodig heeft
// (test seam).
type reportEOL interface {
	Cycles(ctx context.Context, product string) ([]endoflife.Cycle, error)
}
```

Voeg velden toe aan `ReportService`:

```go
	eol  reportEOL
	repo repoFileReader
```

Constructor wordt:

```go
func NewReportService(projects reportProjects, kinsta reportKinsta, security reportSecurity, store *ReportStore, pdf reportPDF, eol reportEOL, repo repoFileReader) *ReportService {
	return &ReportService{projects: projects, kinsta: kinsta, security: security, store: store, pdf: pdf, eol: eol, repo: repo}
}
```

In `Prefill`, na `s.prefillFromKinsta(&r, p)`:

```go
	s.prefillFromRepo(&r, p)
	s.prefillEOL(&r)
```

Nieuwe functies (onder `prefillFromSecurity`):

```go
// prefillFromRepo vult "PHP (lokaal)" en "Node" uit de projectbestanden
// (.bitfactory Dockerfile en docker-compose). Best-effort: leesfouten en
// niet-parsebare bestanden slaan de betreffende cel over.
func (s *ReportService) prefillFromRepo(r *domain.Report, p domain.Project) {
	if s.repo == nil || p.Path == "" {
		return
	}
	php := ""
	if b, err := s.repo.ReadProjectFile(p, ".bitfactory/docker/php-fpm/Dockerfile.dev"); err == nil {
		php = phpFromDockerfile(b)
	}
	var dockerfile []byte
	if php == "" {
		if b, err := s.repo.ReadProjectFile(p, ".bitfactory/docker/php-fpm/Dockerfile"); err == nil {
			dockerfile = b
			php = phpFromDockerfile(b)
		}
	}
	setSoftwareHuidig(r, compPHPLocal, php)

	node := ""
	if b, err := s.repo.ReadProjectFile(p, "docker-compose.yaml"); err == nil {
		node = nodeFromCompose(b)
	}
	if node == "" {
		if dockerfile == nil {
			if b, err := s.repo.ReadProjectFile(p, ".bitfactory/docker/php-fpm/Dockerfile"); err == nil {
				dockerfile = b
			}
		}
		node = nodeFromDockerfile(dockerfile)
	}
	setSoftwareHuidig(r, compNode, node)
}

// eolProducts koppelt rapportrijen aan endoflife.date-producten.
var eolProducts = map[string]string{
	compPHPProd:   "php",
	compPHPLocal:  "php",
	compNode:      "nodejs",
	compMariaDB:   "mariadb",
	compWordPress: "wordpress",
}

// prefillEOL vult "Laatste versie" (nieuwste actieve/LTS-release) en
// "Ondersteund tot" (EOL-datum van de huidige versie) voor elke bekende rij.
// Best-effort per product; zonder Huidig blijft "Ondersteund tot" leeg.
func (s *ReportService) prefillEOL(r *domain.Report) {
	if s.eol == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	now := time.Now()
	for i := range r.Software {
		product, ok := eolProducts[r.Software[i].Component]
		if !ok {
			continue
		}
		cycles, err := s.eol.Cycles(ctx, product)
		if err != nil {
			continue
		}
		if v := latestActive(product, cycles, now); v != "" {
			r.Software[i].Laatste = v
		}
		if d := supportedUntil(r.Software[i].Huidig, cycles); d != "" {
			r.Software[i].OndersteundTot = d
		}
	}
}
```

(Voeg `endoflife`-import toe aan `report_service.go`. `nodeFromDockerfile(nil)` op een nil-slice geeft gewoon "" — regex op lege input.)

In `internal/app/app.go` regel 56 wordt de wiring:

```go
	reportSvc := services.NewReportService(project, kinsta, security, reportStore, pdfRunner, endoflife.NewClient(), services.GitFileReader())
```

Daarvoor: in Task 2 heet het concrete type **`GitRepoFiles`** (exported struct, geen constructor nodig): in `report_versions.go` definieer je `type GitRepoFiles struct{}` met de methode `func (GitRepoFiles) ReadProjectFile(...)`. In `app.go` gebruik je `services.GitRepoFiles{}`. Het test-seam-interface `repoFileReader` blijft privaat.

En voeg de import toe in `app.go`: `"github.com/rdm/sites-tool/internal/adapters/endoflife"`.

- [ ] **Step 4: Run alle service-tests — verwacht PASS**

Run: `go test ./internal/services/ ./internal/app/ -race`
Expected: PASS. Run daarna `go build ./...` (app compileert met nieuwe wiring).

- [ ] **Step 5: Commit**

```bash
git add internal/services/ internal/app/app.go
git commit -m "feat(report): prefill van PHP lokaal, Node en EOL/LTS-data via endoflife.date"
```

---

### Task 6: Frontend — bindings, textarea, tsc

**Files:**
- Regenerate: `frontend/bindings/github.com/rdm/sites-tool/internal/domain/models.ts` (via wails3)
- Modify: `frontend/src/components/ReportTab.tsx` (updateField-type + nieuwe SectionCard)

**Interfaces:**
- Consumes: `domain.Report.Opmerkingen` uit Task 3 (wordt TS-property `opmerkingen`), bestaande `SectionCard`, `updateField`, `Report`-class.

- [ ] **Step 1: Regenereer bindings** (LET OP de `-ts` vlag — zonder genereert deze alpha JavaScript)

```bash
PATH="$HOME/go/bin:$PATH" wails3 generate bindings -ts -d frontend/bindings
```

Verifieer: `grep -n "opmerkingen" frontend/bindings/github.com/rdm/sites-tool/internal/domain/models.ts` → property aanwezig.

- [ ] **Step 2: ReportTab aanpassen**

In `frontend/src/components/ReportTab.tsx:248` het union-type verbreden:

```tsx
  const updateField = (field: 'clientName' | 'websiteName' | 'opmerkingen', value: string) => {
    if (!report) return
    setReport(new Report({ ...report, [field]: value }))
  }
```

Direct ná de `SectionCard title="AVG-check"` (rond regel 415), vóór de sluitende `</>`:

```tsx
            <SectionCard title="Overige opmerkingen">
              <textarea
                value={report.opmerkingen}
                onChange={e => updateField('opmerkingen', e.target.value)}
                rows={5}
                placeholder="Vrije ruimte voor extra informatie van de developer/updater…"
                className="w-full bg-panel border border-border rounded-[9px] px-3 py-2 text-[13px] text-fg outline-none focus:border-accent focus:ring-1 focus:ring-accent/30 resize-y"
              />
            </SectionCard>
```

- [ ] **Step 3: Typecheck**

Run: `cd frontend && npx tsc --noEmit`
Expected: geen fouten.

- [ ] **Step 4: Commit**

```bash
git add frontend/bindings frontend/src/components/ReportTab.tsx
git commit -m "feat(frontend): opmerkingenveld in rapportage-editor"
```

---

### Task 7: Eindverificatie

**Files:** geen nieuwe; verificatie + handmatige check.

- [ ] **Step 1: Volledige testrun + vet**

```bash
go test -race ./... && go vet ./... && gofmt -l . | grep -v frontend || true
```
Expected: alle packages PASS, geen vet-meldingen, geen ongeformatteerde files.

- [ ] **Step 2: Dev-server herstart** (bekende gotcha: venster blijft anders op oude UI/backend hangen)

```bash
pkill -9 -f "wails3"; pkill -9 -f "rdm-sites-tool.dev"; sleep 1; lsof -ti :9245 | xargs kill -9 2>/dev/null; PATH="$HOME/go/bin:$PATH" task dev
```

- [ ] **Step 3: Handmatige check in de app** (door Jeffrey)

- Open een Kinsta-project → Rapportage-tab → knop **Prefill**.
- Verwacht: vijf software-rijen; PHP (productie) + WordPress uit Kinsta; PHP (lokaal) + Node uit repo-bestanden; Laatste + Ondersteund tot gevuld (dd-mm-jjjj); MariaDB alleen Laatste.
- Typ tekst in "Overige opmerkingen" → Opslaan → PDF-export → sectie staat onderaan de PDF; leeg veld → geen sectie.

- [ ] **Step 4: Code-review** (aparte reviewer-lane, geen zelf-goedkeuring)

Dispatch de `code-reviewer` agent op `git diff main...feature/report-version-prefill`; verwerk CRITICAL/HIGH bevindingen.
