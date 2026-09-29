# WordPress-sites koppelen aan wp-fleet — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Per WordPress-site (gekoppeld aan een projectmap of los) WP-user en Application Password in de tool beheren, en de tool de `wp-fleet-mcp`-container voor Claude Code laten starten zonder secrets op schijf.

**Architecture:** Een per-gebruiker bestand `~/.config/rdm/wp-sites.yml` met `WPSite`-records (secret in de Keychain als `wp:<id>`), een Wails-service voor CRUD/bridge-detectie/verbindingstest, een pure `wpfleet`-adapter die `sites.json` en docker-argumenten bouwt, en een subcommando `mcp-launch` in de app-binary dat Claude Code als MCP-server start. De folder-scan blijft ongewijzigd.

**Tech Stack:** Go 1.25, Wails v3 alpha.80, `gopkg.in/yaml.v3`, standaard `testing` (geen testify), React 18 + TypeScript + Tailwind v4, macOS `security`-CLI, Docker, Claude Code CLI.

**Spec:** `docs/superpowers/specs/2026-09-29-wp-fleet-sites-design.md`

## Global Constraints

- Werk in de worktree `/Users/jeffreyt/Projects/RDM-Sites-tool-wp-fleet` op branch `feature/wp-fleet-sites`. Nooit in `/Users/jeffreyt/Projects/RDM-Sites-tool` (daar draait `task dev`).
- `id`: `^[a-z0-9][a-z0-9-]{1,48}$`, uniek, na aanmaken niet te wijzigen.
- `url`: `https://`; `http://` alleen als host `localhost` is of eindigt op `.test` (dan `allowHttp: true` in `sites.json`).
- Keychain-service blijft `nl.nobears.kinsta-updater`; account voor een site is `wp:<id>`.
- Secrets nooit in argv, YAML, logs, foutmeldingen of UI-output.
- `wp-sites.yml`: rechten 0600, map 0700, atomisch schrijven, `project_path` opgeslagen met `~`.
- `mcp-launch`: stdout is het MCP-kanaal — alle eigen output naar stderr.
- Env-naam per site: `WP_<ID in hoofdletters, - → _>_APP_PASSWORD`.
- Docker-run altijd met `-i --rm --init`, `-v <tmp>/sites.json:/app/config/sites.json:ro`, `-v wp-fleet-logs:/app/logs`.
- Standaard image `wp-fleet-mcp:latest`; standaard WP-user `mcp-bot`.
- Test verbinding: timeout 15 s; uitkomsten `ok`, `auth_failed`, `no_bridge`, `unreachable`.
- Go-tests: `go test ./internal/...` (niet `./...`: `main.go` embedt `frontend/dist`).
- Commentaar en UI-teksten in het Nederlands, zoals de rest van de codebase. Commits: conventional commits, eindigen met `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

---

## Bestandsoverzicht

| Bestand | Status | Verantwoordelijkheid |
|---|---|---|
| `internal/config/keychain.go` | wijzigen | `keychainSetIn` via `security -i` + stdin |
| `internal/config/keychain_set_test.go` | nieuw | tests voor het schrijfcommando |
| `internal/domain/wpsite.go` (+`_test`) | nieuw | `WPSite`, `Validate`, `AllowHTTP`, `SuggestWPSiteID` |
| `internal/config/wpsites.go` (+`_test`) | nieuw | laden/opslaan `wp-sites.yml`, `~`-conversie |
| `internal/config/schema.go`, `global.go` | wijzigen | sectie `wpfleet` + default image |
| `internal/adapters/wpfleet/config.go` (+`_test`, `testdata/sites.golden.json`) | nieuw | `EnvName`, `BuildSitesJSON`, `DockerArgs` |
| `internal/adapters/wpfleet/launch.go` (+`_test`) | nieuw | `Launch`: tmp-config, docker-checks, proces, signalen |
| `internal/app/mcp_launch.go` (+`_test`) | nieuw | `RunMCPLaunch` — koppelt config, Keychain en `Launch` |
| `main.go` | wijzigen | subcommando `mcp-launch` |
| `internal/services/wpsite_service.go` (+`_test`) | nieuw | CRUD, `ForProject`, `SuggestForProject`, `DetectBridge` |
| `internal/services/wpsite_connection.go` (+`_test`) | nieuw | `TestConnection` |
| `internal/services/wpfleet_service.go` (+`_test`) | nieuw | `Status`, `Register`, `Unregister`, `SaveSettings` |
| `internal/app/app.go` | wijzigen | services aanmaken en binden |
| `frontend/src/components/WPSiteForm.tsx` | nieuw | gedeeld formulier |
| `frontend/src/components/ProjectSettingsTab.tsx` | wijzigen | blok "WordPress MCP" |
| `frontend/src/components/WPSitePage.tsx` | nieuw | detailpaneel voor een losse site |
| `frontend/src/App.tsx` | wijzigen | zijbalksectie "WordPress-sites" |
| `frontend/src/components/WPFleetSettings.tsx` | nieuw | instellingenblok "wp-fleet" |
| `frontend/src/components/SettingsPage.tsx` | wijzigen | `WPFleetSettings` renderen |

---

### Task 0: Worktree voorbereiden

**Files:** geen codewijzigingen.

- [ ] **Step 1: Controleer de worktree en branch**

Run: `cd /Users/jeffreyt/Projects/RDM-Sites-tool-wp-fleet && git status -sb && git log --oneline -2`
Expected: `## feature/wp-fleet-sites`, laatste commits zijn het plan en de spec.

- [ ] **Step 2: Frontend-dependencies installeren (worktree heeft geen node_modules)**

Run: `cd /Users/jeffreyt/Projects/RDM-Sites-tool-wp-fleet/frontend && npm ci`
Expected: `added … packages`.

- [ ] **Step 3: Baseline Go-tests**

Run: `cd /Users/jeffreyt/Projects/RDM-Sites-tool-wp-fleet && go test ./internal/...`
Expected: alles `ok` (noteer eventuele al falende tests; die zijn niet van deze feature).

---

### Task 1: Keychain-schrijven zonder secret in argv

**Files:**
- Modify: `internal/config/keychain.go:49-61` (`keychainSetIn`)
- Test: `internal/config/keychain_set_test.go`

**Interfaces:**
- Produces: ongewijzigde publieke API `config.KeychainSet(account, password string) error`; nieuw intern `addPasswordCommand(service, account, password string) (string, error)` en seam `var securityStdin func(input string) error`.

- [ ] **Step 1: Schrijf de falende tests**

```go
package config

import (
	"strings"
	"testing"
)

func TestAddPasswordCommandEscapet(t *testing.T) {
	got, err := addPasswordCommand("svc", "wp:klant-a", `ab cd"ef\gh`)
	if err != nil {
		t.Fatal(err)
	}
	want := `add-generic-password -s "svc" -a "wp:klant-a" -w "ab cd\"ef\\gh" -U` + "\n"
	if got != want {
		t.Fatalf("command\n got: %q\nwant: %q", got, want)
	}
}

func TestAddPasswordCommandWeigertRegeleinde(t *testing.T) {
	for _, pw := range []string{"a\nb", "a\rb"} {
		if _, err := addPasswordCommand("svc", "acc", pw); err == nil {
			t.Fatalf("verwacht fout voor %q", pw)
		}
	}
	if _, err := addPasswordCommand("svc", "a\nb", "pw"); err == nil {
		t.Fatal("verwacht fout voor account met regeleinde")
	}
}

func TestKeychainSetInGeeftSecretViaStdin(t *testing.T) {
	var input string
	orig := securityStdin
	securityStdin = func(in string) error { input = in; return nil }
	t.Cleanup(func() { securityStdin = orig })

	if err := keychainSetIn("svc", "acc", "geheim-123"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(input, `-w "geheim-123"`) {
		t.Fatalf("secret niet in stdin-invoer: %q", input)
	}
}
```

- [ ] **Step 2: Run de tests, verwacht FAIL**

Run: `go test ./internal/config/ -run 'AddPasswordCommand|KeychainSetInGeeft' -v`
Expected: FAIL — `undefined: addPasswordCommand` / `undefined: securityStdin`.

- [ ] **Step 3: Implementeer**

Vervang `keychainSetIn` in `internal/config/keychain.go` en voeg `"errors"` toe aan de imports:

```go
// securityStdin draait de security-CLI in interactieve modus en geeft het
// commando via stdin door. Zo staat het secret nooit in de procesargumenten,
// die andere lokale processen via ps kunnen lezen. security -i geeft ook bij
// een mislukt commando exit 0, dus elke uitvoer geldt als fout.
var securityStdin = func(input string) error {
	cmd := exec.Command("security", "-i")
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	if msg := strings.TrimSpace(string(out)); msg != "" {
		return errors.New(msg)
	}
	return nil
}

// quoteSecurity zet een waarde tussen dubbele quotes zoals security -i die
// parset: backslash en dubbele quote krijgen een backslash ervoor.
func quoteSecurity(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// addPasswordCommand bouwt één regel voor security -i. Een regeleinde zou de
// regel afbreken en de rest als nieuw commando laten lezen, dus dat weigeren we.
func addPasswordCommand(service, account, password string) (string, error) {
	for _, v := range []string{service, account, password} {
		if strings.ContainsAny(v, "\r\n") {
			return "", errors.New("keychain: waarde mag geen regeleinde bevatten")
		}
	}
	// -U werkt bij als het item al bestaat, en voegt het anders toe.
	return fmt.Sprintf("add-generic-password -s %s -a %s -w %s -U\n",
		quoteSecurity(service), quoteSecurity(account), quoteSecurity(password)), nil
}

func keychainSetIn(service, account, password string) error {
	line, err := addPasswordCommand(service, account, password)
	if err != nil {
		return err
	}
	if err := securityStdin(line); err != nil {
		return fmt.Errorf("keychain set %q: %w", account, err)
	}
	return nil
}
```

- [ ] **Step 4: Run de tests, verwacht PASS**

Run: `go test ./internal/config/ -v -run 'AddPasswordCommand|KeychainSetInGeeft|Migrate'`
Expected: PASS (ook de bestaande migratietests in `keychain_test.go`).

- [ ] **Step 5: Echte Keychain-probe (wegwerp-item)**

Run:
```bash
printf '%s\n' 'add-generic-password -s "rdm.plan.probe" -a "probe" -w "ab cd\"ef\\gh" -U' | security -i
security find-generic-password -s rdm.plan.probe -a probe -w
security delete-generic-password -s rdm.plan.probe -a probe
```
Expected: geen uitvoer bij de eerste regel, daarna exact `ab cd"ef\gh`, daarna "password has been deleted". (Op 2026-09-29 al zo bevestigd.)

- [ ] **Step 6: Commit**

```bash
git add internal/config/keychain.go internal/config/keychain_set_test.go
git commit -m "fix: keychain-secret via stdin i.p.v. argv doorgeven

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Domeintype `WPSite`

**Files:**
- Create: `internal/domain/wpsite.go`
- Test: `internal/domain/wpsite_test.go`

**Interfaces:**
- Produces:
  - `type WPSite struct { ID, Name, URL, Username, ProjectPath string; Tags []string; ReadOnly, Bridge bool }` (JSON: `id,name,url,username,projectPath,tags,readOnly,bridge`; YAML: `id,name,url,username,project_path,tags,read_only,bridge`)
  - `func (s WPSite) Validate() error`
  - `func (s WPSite) AllowHTTP() bool`
  - `func SuggestWPSiteID(name string) string`

- [ ] **Step 1: Schrijf de falende tests**

```go
package domain

import "testing"

func geldigeSite() WPSite {
	return WPSite{ID: "klant-a", Name: "Klant A", URL: "https://www.klant-a.nl", Username: "mcp-bot"}
}

func TestWPSiteValidateGeldig(t *testing.T) {
	if err := geldigeSite().Validate(); err != nil {
		t.Fatalf("verwacht geldig: %v", err)
	}
}

func TestWPSiteValidateFouten(t *testing.T) {
	cases := map[string]func(*WPSite){
		"id leeg":             func(s *WPSite) { s.ID = "" },
		"id hoofdletter":      func(s *WPSite) { s.ID = "Klant" },
		"id één teken":        func(s *WPSite) { s.ID = "a" },
		"id begint met -":     func(s *WPSite) { s.ID = "-klant" },
		"id te lang":          func(s *WPSite) { s.ID = "a123456789012345678901234567890123456789012345678x" },
		"naam leeg":           func(s *WPSite) { s.Name = " " },
		"user leeg":           func(s *WPSite) { s.Username = "" },
		"url leeg":            func(s *WPSite) { s.URL = "" },
		"url http extern":     func(s *WPSite) { s.URL = "http://www.klant-a.nl" },
		"url ftp":             func(s *WPSite) { s.URL = "ftp://klant-a.nl" },
		"url zonder host":     func(s *WPSite) { s.URL = "https://" },
	}
	for naam, mut := range cases {
		s := geldigeSite()
		mut(&s)
		if err := s.Validate(); err == nil {
			t.Errorf("%s: verwacht fout", naam)
		}
	}
}

func TestWPSiteAllowHTTP(t *testing.T) {
	for url, want := range map[string]bool{
		"http://localhost:8080":   true,
		"http://klant-a.test":     true,
		"https://klant-a.test":    false,
		"http://www.klant-a.nl":   false,
		"https://www.klant-a.nl":  false,
	} {
		s := geldigeSite()
		s.URL = url
		if got := s.AllowHTTP(); got != want {
			t.Errorf("%s: AllowHTTP=%v, want %v", url, got, want)
		}
		if want {
			if err := s.Validate(); err != nil {
				t.Errorf("%s: lokale http moet geldig zijn: %v", url, err)
			}
		}
	}
}

func TestSuggestWPSiteID(t *testing.T) {
	for in, want := range map[string]string{
		"web-ekcoatingsnl":  "web-ekcoatingsnl",
		"EK Coating B.V.":   "ek-coating-b-v",
		"__Kennisbank__":    "kennisbank",
		"Café Één":          "caf-n",
	} {
		if got := SuggestWPSiteID(in); got != want {
			t.Errorf("SuggestWPSiteID(%q) = %q, want %q", in, got, want)
		}
	}
	lang := SuggestWPSiteID("abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz")
	if len(lang) != 49 {
		t.Errorf("lengte %d, want 49", len(lang))
	}
}
```

- [ ] **Step 2: Run, verwacht FAIL**

Run: `go test ./internal/domain/ -run 'WPSite|SuggestWPSiteID' -v`
Expected: FAIL — `undefined: WPSite`.

- [ ] **Step 3: Implementeer `internal/domain/wpsite.go`**

```go
package domain

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// WPSite is een WordPress-site waarmee wp-fleet mag praten: gekoppeld aan een
// projectmap (ProjectPath gevuld) of los. Het Application Password staat
// nooit hier maar in de Keychain onder "wp:<ID>".
type WPSite struct {
	ID          string   `json:"id" yaml:"id"`
	Name        string   `json:"name" yaml:"name"`
	URL         string   `json:"url" yaml:"url"`
	Username    string   `json:"username" yaml:"username"`
	ProjectPath string   `json:"projectPath,omitempty" yaml:"project_path,omitempty"`
	Tags        []string `json:"tags,omitempty" yaml:"tags,omitempty"`
	ReadOnly    bool     `json:"readOnly" yaml:"read_only"`
	Bridge      bool     `json:"bridge" yaml:"bridge"`
}

// Zelfde regel als wp-fleet-mcp's sites.json, zodat elke id daar ook geldig is.
var wpSiteIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,48}$`)

func (s WPSite) Validate() error {
	if !wpSiteIDPattern.MatchString(s.ID) {
		return fmt.Errorf("id %q ongeldig: kleine letters, cijfers en -, 2 tot 49 tekens", s.ID)
	}
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("naam is verplicht")
	}
	if strings.TrimSpace(s.Username) == "" {
		return errors.New("WP-gebruiker is verplicht")
	}
	u, err := url.Parse(s.URL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("url %q ongeldig", s.URL)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !s.AllowHTTP() {
			return errors.New("url moet https zijn (http alleen voor localhost of *.test)")
		}
	default:
		return fmt.Errorf("url %q moet met https:// beginnen", s.URL)
	}
	return nil
}

// AllowHTTP meldt of de site een lokale http-URL heeft. Application Passwords
// werken alleen over http als WordPress in local/development draait.
func (s WPSite) AllowHTTP() bool {
	u, err := url.Parse(s.URL)
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || strings.HasSuffix(host, ".test")
}

var nietIDTeken = regexp.MustCompile(`[^a-z0-9]+`)

// SuggestWPSiteID maakt van een naam of mapnaam een voorstel voor een id.
// Het resultaat kan nog ongeldig zijn (bijv. te kort); Validate beslist.
func SuggestWPSiteID(name string) string {
	id := nietIDTeken.ReplaceAllString(strings.ToLower(name), "-")
	id = strings.Trim(id, "-")
	if len(id) > 49 {
		id = strings.TrimRight(id[:49], "-")
	}
	return id
}
```

- [ ] **Step 4: Run, verwacht PASS**

Run: `go test ./internal/domain/ -run 'WPSite|SuggestWPSiteID' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/wpsite.go internal/domain/wpsite_test.go
git commit -m "feat: domeintype WPSite met validatie

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: `wp-sites.yml` laden en opslaan

**Files:**
- Create: `internal/config/wpsites.go`
- Test: `internal/config/wpsites_test.go`

**Interfaces:**
- Consumes: `domain.WPSite`, `(WPSite).Validate()` (Task 2)
- Produces:
  - `func WPSitesPath() string` → `~/.config/rdm/wp-sites.yml`
  - `func LoadWPSites(path string) ([]domain.WPSite, error)` — ontbrekend bestand → lege slice; `ProjectPath` terug als absoluut pad
  - `func SaveWPSites(path string, sites []domain.WPSite) error` — valideert, schrijft `~`-paden, atomisch, 0600
  - `func ExpandHome(p string) string`, `func CompressHome(p string) string`

- [ ] **Step 1: Schrijf de falende tests**

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestWPSitesRoundtripEnTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".config", "rdm", "wp-sites.yml")

	sites := []domain.WPSite{
		{ID: "klant-a", Name: "Klant A", URL: "https://a.nl", Username: "mcp-bot",
			ProjectPath: filepath.Join(home, "Projects", "web-a"), Tags: []string{"kinsta"}, Bridge: true},
		{ID: "depot", Name: "Depot", URL: "https://depot.nl", Username: "mcp-bot"},
	}
	if err := SaveWPSites(path, sites); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "project_path: ~/Projects/web-a") {
		t.Fatalf("project_path niet met ~ opgeslagen:\n%s", raw)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("rechten %v, want 0600", info.Mode().Perm())
	}

	got, err := LoadWPSites(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ProjectPath != filepath.Join(home, "Projects", "web-a") || got[1].ProjectPath != "" {
		t.Fatalf("onverwacht resultaat: %+v", got)
	}
}

func TestLoadWPSitesOntbrekendBestand(t *testing.T) {
	got, err := LoadWPSites(filepath.Join(t.TempDir(), "bestaat-niet.yml"))
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want lege lijst zonder fout", got, err)
	}
}

func TestLoadWPSitesOngeldig(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"yaml-syntax":     "sites: [\n",
		"ongeldige site":  "sites:\n  - id: X\n    name: a\n    url: https://a.nl\n    username: u\n",
		"dubbele id":      "sites:\n  - {id: aa, name: a, url: https://a.nl, username: u}\n  - {id: aa, name: b, url: https://b.nl, username: u}\n",
		"dubbel project":  "sites:\n  - {id: aa, name: a, url: https://a.nl, username: u, project_path: /p}\n  - {id: bb, name: b, url: https://b.nl, username: u, project_path: /p}\n",
	}
	for naam, inhoud := range cases {
		p := filepath.Join(dir, naam+".yml")
		os.WriteFile(p, []byte(inhoud), 0o600)
		if _, err := LoadWPSites(p); err == nil {
			t.Errorf("%s: verwacht fout", naam)
		} else if !strings.Contains(err.Error(), "wp-sites") && naam != "yaml-syntax" {
			t.Errorf("%s: foutmelding noemt bestand niet: %v", naam, err)
		}
	}
}

func TestSaveWPSitesWeigertOngeldigEnLaatBestandStaan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wp-sites.yml")
	goed := []domain.WPSite{{ID: "aa", Name: "a", URL: "https://a.nl", Username: "u"}}
	if err := SaveWPSites(path, goed); err != nil {
		t.Fatal(err)
	}
	fout := []domain.WPSite{{ID: "X", Name: "a", URL: "https://a.nl", Username: "u"}}
	if err := SaveWPSites(path, fout); err == nil {
		t.Fatal("verwacht fout")
	}
	got, _ := LoadWPSites(path)
	if len(got) != 1 || got[0].ID != "aa" {
		t.Fatalf("bestand is aangetast: %+v", got)
	}
}
```

- [ ] **Step 2: Run, verwacht FAIL**

Run: `go test ./internal/config/ -run 'WPSites' -v`
Expected: FAIL — `undefined: SaveWPSites`.

- [ ] **Step 3: Implementeer `internal/config/wpsites.go`**

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/rdm/sites-tool/internal/domain"
)

type wpSitesFile struct {
	Sites []domain.WPSite `yaml:"sites"`
}

// WPSitesPath is het per-gebruiker bestand met WordPress-sites voor wp-fleet.
// Het staat bewust buiten elke projectmap: het hoort bij de gebruiker, niet bij
// de klantrepo.
func WPSitesPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rdm", "wp-sites.yml")
}

func LoadWPSites(path string) ([]domain.WPSite, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []domain.WPSite{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lees %s: %w", path, err)
	}
	var f wpSitesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for i := range f.Sites {
		f.Sites[i].ProjectPath = ExpandHome(f.Sites[i].ProjectPath)
	}
	if err := validateWPSites(f.Sites); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if f.Sites == nil {
		f.Sites = []domain.WPSite{}
	}
	return f.Sites, nil
}

func SaveWPSites(path string, sites []domain.WPSite) error {
	if err := validateWPSites(sites); err != nil {
		return fmt.Errorf("wp-sites: %w", err)
	}
	out := make([]domain.WPSite, len(sites))
	for i, s := range sites {
		s.ProjectPath = CompressHome(s.ProjectPath)
		out[i] = s
	}
	data, err := yaml.Marshal(wpSitesFile{Sites: out})
	if err != nil {
		return fmt.Errorf("wp-sites: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("wp-sites map: %w", err)
	}
	// Atomisch: eerst een tijdelijk bestand naast het doel, dan hernoemen. Een
	// crash halverwege laat zo nooit een half bestand achter.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".wp-sites-*.yml")
	if err != nil {
		return fmt.Errorf("wp-sites tijdelijk bestand: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("wp-sites rechten: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("wp-sites schrijven: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("wp-sites schrijven: %w", err)
	}
	return os.Rename(tmp.Name(), path)
}

func validateWPSites(sites []domain.WPSite) error {
	ids := map[string]bool{}
	paden := map[string]string{}
	for i, s := range sites {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("wp-sites site %d (%s): %w", i+1, s.ID, err)
		}
		if ids[s.ID] {
			return fmt.Errorf("wp-sites: id %q komt dubbel voor", s.ID)
		}
		ids[s.ID] = true
		if s.ProjectPath != "" {
			if ander, ok := paden[s.ProjectPath]; ok {
				return fmt.Errorf("wp-sites: project %s is al gekoppeld aan %q", s.ProjectPath, ander)
			}
			paden[s.ProjectPath] = s.ID
		}
	}
	return nil
}

// ExpandHome zet een pad dat met ~/ begint om naar een absoluut pad.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

// CompressHome schrijft een pad onder de home-map als ~/…, zodat het bestand
// niet aan één gebruikersnaam vastzit.
func CompressHome(p string) string {
	home, _ := os.UserHomeDir()
	if home != "" && (p == home || strings.HasPrefix(p, home+string(filepath.Separator))) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
```

- [ ] **Step 4: Run, verwacht PASS**

Run: `go test ./internal/config/ -run 'WPSites' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/wpsites.go internal/config/wpsites_test.go
git commit -m "feat: wp-sites.yml per gebruiker laden en opslaan

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Globale config-sectie `wpfleet`

**Files:**
- Modify: `internal/config/schema.go` (veld in `Global` + nieuw type)
- Modify: `internal/config/global.go` (`defaultGlobal`, `applyDefaults`)
- Test: `internal/config/wpfleet_config_test.go`

**Interfaces:**
- Produces: `Global.WPFleet WPFleetGlobal`; `type WPFleetGlobal struct { Image string; ReadOnly bool }` (yaml `image`, `read_only`); const `DefaultWPFleetImage = "wp-fleet-mcp:latest"`.

- [ ] **Step 1: Falende test**

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWPFleetDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	g, err := LoadGlobal() // geen bestand → defaults
	if err != nil {
		t.Fatal(err)
	}
	if g.WPFleet.Image != DefaultWPFleetImage || g.WPFleet.ReadOnly {
		t.Fatalf("defaults: %+v", g.WPFleet)
	}

	// Bestaand bestand zonder wpfleet-sectie krijgt ook de default image.
	p := filepath.Join(home, ".config", "rdm", "config.yml")
	os.MkdirAll(filepath.Dir(p), 0o700)
	os.WriteFile(p, []byte("editor: cursor\n"), 0o600)
	g, err = LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if g.WPFleet.Image != DefaultWPFleetImage {
		t.Fatalf("image = %q", g.WPFleet.Image)
	}
}
```

- [ ] **Step 2: Run, verwacht FAIL**

Run: `go test ./internal/config/ -run TestWPFleetDefaults -v`
Expected: FAIL — `g.WPFleet undefined`.

- [ ] **Step 3: Implementeer**

In `internal/config/schema.go`, voeg aan `Global` toe (na `Updates`):

```go
	WPFleet       WPFleetGlobal   `yaml:"wpfleet"`
```

en onderaan:

```go
// DefaultWPFleetImage is het lokaal gebouwde wp-fleet-mcp-image. Een vaste
// versie uit een registry volgt later; tot dan is de referentie instelbaar.
const DefaultWPFleetImage = "wp-fleet-mcp:latest"

// WPFleetGlobal regelt hoe de tool de wp-fleet-mcp-container start.
type WPFleetGlobal struct {
	Image    string `yaml:"image"`
	ReadOnly bool   `yaml:"read_only"` // → MCP_READ_ONLY=true
}
```

In `internal/config/global.go`: in `defaultGlobal()` het veld `WPFleet: WPFleetGlobal{Image: DefaultWPFleetImage},` toevoegen, en in `applyDefaults`:

```go
	if g.WPFleet.Image == "" {
		g.WPFleet.Image = DefaultWPFleetImage
	}
```

- [ ] **Step 4: Run, verwacht PASS**

Run: `go test ./internal/config/ -v`
Expected: PASS (alle config-tests).

- [ ] **Step 5: Commit**

```bash
git add internal/config/schema.go internal/config/global.go internal/config/wpfleet_config_test.go
git commit -m "feat: config-sectie wpfleet met image en read-only

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: `wpfleet`-adapter — `sites.json` en docker-argumenten

**Files:**
- Create: `internal/adapters/wpfleet/config.go`
- Create: `internal/adapters/wpfleet/testdata/sites.golden.json`
- Test: `internal/adapters/wpfleet/config_test.go`

**Interfaces:**
- Consumes: `domain.WPSite`, `(WPSite).AllowHTTP()` (Task 2)
- Produces:
  - `func EnvName(id string) string`
  - `func BuildSitesJSON(sites []domain.WPSite) ([]byte, error)`
  - `type RunOptions struct { Image string; ReadOnly bool; SitesJSONPath string; EnvNames []string }`
  - `func DockerArgs(o RunOptions) []string`

- [ ] **Step 1: Golden file `internal/adapters/wpfleet/testdata/sites.golden.json`**

```json
{
  "sites": [
    {
      "id": "klant-a",
      "name": "Klant A",
      "url": "https://www.klant-a.nl",
      "username": "mcp-bot",
      "passwordEnv": "WP_KLANT_A_APP_PASSWORD",
      "tags": [
        "kinsta"
      ],
      "readOnly": false,
      "allowHttp": false,
      "bridge": true
    },
    {
      "id": "lokaal",
      "name": "Lokaal",
      "url": "http://lokaal.test",
      "username": "admin",
      "passwordEnv": "WP_LOKAAL_APP_PASSWORD",
      "tags": [],
      "readOnly": true,
      "allowHttp": true,
      "bridge": false
    }
  ]
}
```

- [ ] **Step 2: Falende tests `config_test.go`**

```go
package wpfleet

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestEnvName(t *testing.T) {
	if got := EnvName("klant-a-2"); got != "WP_KLANT_A_2_APP_PASSWORD" {
		t.Fatalf("got %q", got)
	}
}

func TestBuildSitesJSONGolden(t *testing.T) {
	sites := []domain.WPSite{
		{ID: "klant-a", Name: "Klant A", URL: "https://www.klant-a.nl", Username: "mcp-bot",
			ProjectPath: "/Users/x/Projects/web-a", Tags: []string{"kinsta"}, Bridge: true},
		{ID: "lokaal", Name: "Lokaal", URL: "http://lokaal.test", Username: "admin", ReadOnly: true},
	}
	got, err := BuildSitesJSON(sites)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile("testdata/sites.golden.json")
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Fatalf("sites.json wijkt af:\n%s", got)
	}
	if bytes.Contains(got, []byte("/Users/x")) {
		t.Fatal("project_path hoort niet in sites.json")
	}
}

func TestDockerArgsBevatAlleenNamen(t *testing.T) {
	args := DockerArgs(RunOptions{
		Image:         "wp-fleet-mcp:latest",
		ReadOnly:      true,
		SitesJSONPath: "/tmp/x/sites.json",
		EnvNames:      []string{"WP_A_APP_PASSWORD", "WP_B_APP_PASSWORD"},
	})
	joined := strings.Join(args, " ")
	want := "run -i --rm --init -e WP_A_APP_PASSWORD -e WP_B_APP_PASSWORD -e MCP_READ_ONLY=true " +
		"-v /tmp/x/sites.json:/app/config/sites.json:ro -v wp-fleet-logs:/app/logs wp-fleet-mcp:latest"
	if joined != want {
		t.Fatalf("args\n got: %s\nwant: %s", joined, want)
	}
	for _, a := range args {
		if strings.Contains(a, "=") && strings.HasPrefix(a, "WP_") {
			t.Fatalf("waarde van een secret-variabele in argv: %s", a)
		}
	}
}

func TestDockerArgsZonderReadOnly(t *testing.T) {
	args := strings.Join(DockerArgs(RunOptions{Image: "img", SitesJSONPath: "/s.json"}), " ")
	if strings.Contains(args, "MCP_READ_ONLY") {
		t.Fatalf("MCP_READ_ONLY hoort er niet in: %s", args)
	}
}
```

- [ ] **Step 3: Run, verwacht FAIL**

Run: `go test ./internal/adapters/wpfleet/ -v`
Expected: FAIL — package bestaat nog niet / `undefined: EnvName`.

- [ ] **Step 4: Implementeer `config.go`**

```go
// Package wpfleet bouwt de configuratie voor de wp-fleet-mcp-container en
// start die. De tool is de bron van de sitelijst; secrets gaan alleen via de
// procesomgeving naar docker, nooit via argv of een bestand.
package wpfleet

import (
	"encoding/json"
	"strings"

	"github.com/rdm/sites-tool/internal/domain"
)

type fleetSite struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	Username    string   `json:"username"`
	PasswordEnv string   `json:"passwordEnv"`
	Tags        []string `json:"tags"`
	ReadOnly    bool     `json:"readOnly"`
	AllowHTTP   bool     `json:"allowHttp"`
	Bridge      bool     `json:"bridge"`
}

type fleetFile struct {
	Sites []fleetSite `json:"sites"`
}

// EnvName is de omgevingsvariabele waarin wp-fleet het Application Password
// van een site zoekt.
func EnvName(id string) string {
	return "WP_" + strings.ToUpper(strings.ReplaceAll(id, "-", "_")) + "_APP_PASSWORD"
}

// BuildSitesJSON zet sites om naar het sites.json-formaat van wp-fleet-mcp.
// project_path is iets van de tool en gaat niet mee.
func BuildSitesJSON(sites []domain.WPSite) ([]byte, error) {
	out := fleetFile{Sites: make([]fleetSite, 0, len(sites))}
	for _, s := range sites {
		tags := s.Tags
		if tags == nil {
			tags = []string{}
		}
		out.Sites = append(out.Sites, fleetSite{
			ID: s.ID, Name: s.Name, URL: s.URL, Username: s.Username,
			PasswordEnv: EnvName(s.ID), Tags: tags,
			ReadOnly: s.ReadOnly, AllowHTTP: s.AllowHTTP(), Bridge: s.Bridge,
		})
	}
	return json.MarshalIndent(out, "", "  ")
}

// RunOptions beschrijft één start van de container.
type RunOptions struct {
	Image         string
	ReadOnly      bool
	SitesJSONPath string
	EnvNames      []string
}

// DockerArgs bouwt de argumenten voor `docker run`. Van de secret-variabelen
// staan alleen de namen erin: `-e NAAM` laat docker de waarde uit de eigen
// omgeving overnemen. --init omdat Node als PID 1 SIGTERM negeert.
func DockerArgs(o RunOptions) []string {
	args := []string{"run", "-i", "--rm", "--init"}
	for _, n := range o.EnvNames {
		args = append(args, "-e", n)
	}
	if o.ReadOnly {
		args = append(args, "-e", "MCP_READ_ONLY=true")
	}
	return append(args,
		"-v", o.SitesJSONPath+":/app/config/sites.json:ro",
		"-v", "wp-fleet-logs:/app/logs",
		o.Image,
	)
}
```

- [ ] **Step 5: Run, verwacht PASS**

Run: `go test ./internal/adapters/wpfleet/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/adapters/wpfleet/
git commit -m "feat: wpfleet-adapter bouwt sites.json en docker-argumenten

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: `wpfleet.Launch` — container starten

**Files:**
- Create: `internal/adapters/wpfleet/launch.go`
- Test: `internal/adapters/wpfleet/launch_test.go`

**Interfaces:**
- Consumes: `BuildSitesJSON`, `EnvName`, `DockerArgs`, `RunOptions` (Task 5); `docker.DockerBin()` uit `internal/adapters/docker`
- Produces:
  - `type SecretFunc func(id string) (string, error)`
  - `type LaunchConfig struct { Sites []domain.WPSite; Image string; ReadOnly bool; Secret SecretFunc; Stdin io.Reader; Stdout, Stderr io.Writer; BaseEnv []string }`
  - `func Launch(ctx context.Context, c LaunchConfig) (int, error)` — exitcode van docker; fout alleen als docker niet gestart kon worden
  - `var ErrDockerDown`, `var ErrImageMissing` (sentinel errors)

- [ ] **Step 1: Falende tests `launch_test.go`**

Gebruikt het standaard helper-process-patroon: `execCommandContext` start deze testbinary opnieuw als nep-docker.

```go
package wpfleet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

// nepDocker laat execCommandContext deze testbinary als "docker" starten.
func nepDocker(t *testing.T, modus string) {
	t.Helper()
	orig, origBin := execCommandContext, dockerBin
	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--"}, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "WPFLEET_HELPER=1", "WPFLEET_MODUS="+modus)
		return cmd
	}
	dockerBin = func() string { return "docker" }
	t.Cleanup(func() { execCommandContext, dockerBin = orig, origBin })
}

// TestHelperProcess is geen echte test: het is de nep-docker.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("WPFLEET_HELPER") != "1" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	modus := os.Getenv("WPFLEET_MODUS")
	switch args[0] {
	case "info":
		if modus == "down" {
			fmt.Fprintln(os.Stderr, "Cannot connect to the Docker daemon")
			os.Exit(1)
		}
		os.Exit(0)
	case "image":
		if modus == "noimage" {
			os.Exit(1)
		}
		os.Exit(0)
	case "run":
		// Rapporteer wat we zagen: args, of de secret in de env zit, en of
		// sites.json leesbaar was.
		var sitesPath string
		for i, a := range args {
			if a == "-v" && strings.HasSuffix(args[i+1], ":/app/config/sites.json:ro") {
				sitesPath = strings.TrimSuffix(args[i+1], ":/app/config/sites.json:ro")
			}
		}
		sites, _ := os.ReadFile(sitesPath)
		json.NewEncoder(os.Stdout).Encode(map[string]any{
			"args":   args,
			"secret": os.Getenv("WP_KLANT_A_APP_PASSWORD"),
			"sites":  string(sites),
			"dir":    filepath.Dir(sitesPath),
		})
		os.Exit(7)
	}
	os.Exit(2)
}

func testSites() []domain.WPSite {
	return []domain.WPSite{
		{ID: "klant-a", Name: "A", URL: "https://a.nl", Username: "mcp-bot"},
		{ID: "klant-b", Name: "B", URL: "https://b.nl", Username: "mcp-bot"},
	}
}

func secretVoorA(id string) (string, error) {
	if id == "klant-a" {
		return "geheim-a", nil
	}
	return "", errors.New("niet gevonden")
}

func TestLaunchGeeftSecretViaEnvEnRuimtOp(t *testing.T) {
	nepDocker(t, "ok")
	var stdout, stderr bytes.Buffer
	code, err := Launch(context.Background(), LaunchConfig{
		Sites: testSites(), Image: "img", Secret: secretVoorA,
		Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
		BaseEnv: append(os.Environ(), "WPFLEET_HELPER=1", "WPFLEET_MODUS=ok"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 7 {
		t.Fatalf("exitcode %d, want 7 (van docker doorgegeven)", code)
	}
	var seen struct {
		Args   []string `json:"args"`
		Secret string   `json:"secret"`
		Sites  string   `json:"sites"`
		Dir    string   `json:"dir"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &seen); err != nil {
		t.Fatalf("stdout: %q (%v)", stdout.String(), err)
	}
	if seen.Secret != "geheim-a" {
		t.Fatalf("secret niet in env van docker: %q", seen.Secret)
	}
	for _, a := range seen.Args {
		if strings.Contains(a, "geheim-a") {
			t.Fatalf("secret in argv: %v", seen.Args)
		}
	}
	if !strings.Contains(seen.Sites, `"klant-a"`) || strings.Contains(seen.Sites, `"klant-b"`) {
		t.Fatalf("sites.json hoort alleen klant-a te bevatten: %s", seen.Sites)
	}
	if !strings.Contains(stderr.String(), "klant-b") {
		t.Fatalf("waarschuwing voor klant-b ontbreekt op stderr: %q", stderr.String())
	}
	if _, err := os.Stat(seen.Dir); !os.IsNotExist(err) {
		t.Fatalf("tijdelijke map %s niet opgeruimd", seen.Dir)
	}
}

func TestLaunchDockerDown(t *testing.T) {
	nepDocker(t, "down")
	var stderr bytes.Buffer
	_, err := Launch(context.Background(), LaunchConfig{
		Sites: testSites(), Image: "img", Secret: secretVoorA,
		Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &stderr,
	})
	if !errors.Is(err, ErrDockerDown) {
		t.Fatalf("err = %v, want ErrDockerDown", err)
	}
}

func TestLaunchImageOntbreekt(t *testing.T) {
	nepDocker(t, "noimage")
	_, err := Launch(context.Background(), LaunchConfig{
		Sites: testSites(), Image: "img", Secret: secretVoorA,
		Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
	})
	if !errors.Is(err, ErrImageMissing) {
		t.Fatalf("err = %v, want ErrImageMissing", err)
	}
}
```

- [ ] **Step 2: Run, verwacht FAIL**

Run: `go test ./internal/adapters/wpfleet/ -run 'Launch' -v`
Expected: FAIL — `undefined: Launch`.

- [ ] **Step 3: Implementeer `launch.go`**

```go
package wpfleet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/docker"
	"github.com/rdm/sites-tool/internal/domain"
)

var (
	ErrDockerDown   = errors.New("Docker draait niet — start Docker Desktop")
	ErrImageMissing = errors.New("wp-fleet-image ontbreekt")
)

// Test seams.
var (
	execCommandContext = exec.CommandContext
	dockerBin          = docker.DockerBin
)

type SecretFunc func(id string) (string, error)

type LaunchConfig struct {
	Sites    []domain.WPSite
	Image    string
	ReadOnly bool
	Secret   SecretFunc
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	BaseEnv  []string // meestal os.Environ()
}

// Launch start wp-fleet-mcp in docker en geeft de exitcode van docker terug.
// Stdout is het MCP-kanaal: Launch schrijft zelf alleen naar Stderr.
func Launch(ctx context.Context, c LaunchConfig) (int, error) {
	sites, env := sitesMetSecret(c)

	if err := checkDocker(ctx, c.Image); err != nil {
		return 1, err
	}

	dir, err := os.MkdirTemp("", "wp-fleet-*")
	if err != nil {
		return 1, fmt.Errorf("tijdelijke map: %w", err)
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0o700); err != nil {
		return 1, fmt.Errorf("tijdelijke map: %w", err)
	}

	data, err := BuildSitesJSON(sites)
	if err != nil {
		return 1, err
	}
	sitesPath := filepath.Join(dir, "sites.json")
	if err := os.WriteFile(sitesPath, data, 0o600); err != nil {
		return 1, fmt.Errorf("sites.json: %w", err)
	}

	names := make([]string, 0, len(sites))
	for _, s := range sites {
		names = append(names, EnvName(s.ID))
	}
	cmd := execCommandContext(ctx, dockerBin(), DockerArgs(RunOptions{
		Image: c.Image, ReadOnly: c.ReadOnly, SitesJSONPath: sitesPath, EnvNames: names,
	})...)
	cmd.Env = append(append([]string{}, c.BaseEnv...), env...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = c.Stdin, c.Stdout, c.Stderr

	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("docker starten: %w", err)
	}
	stop := forwardSignals(cmd)
	defer stop()

	err = cmd.Wait()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	if err != nil {
		return 1, fmt.Errorf("docker: %w", err)
	}
	return 0, nil
}

// sitesMetSecret houdt alleen sites met een wachtwoord over en geeft de
// bijbehorende NAAM=waarde-regels voor de procesomgeving terug.
func sitesMetSecret(c LaunchConfig) ([]domain.WPSite, []string) {
	var sites []domain.WPSite
	var env []string
	for _, s := range c.Sites {
		pw, err := c.Secret(s.ID)
		if err != nil || pw == "" {
			fmt.Fprintf(c.Stderr, "wp-fleet: site %s overgeslagen — geen Application Password in de Keychain\n", s.ID)
			continue
		}
		sites = append(sites, s)
		env = append(env, EnvName(s.ID)+"="+pw)
	}
	return sites, env
}

func checkDocker(ctx context.Context, image string) error {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := execCommandContext(cctx, dockerBin(), "info", "--format", "{{.ServerVersion}}").Run(); err != nil {
		return ErrDockerDown
	}
	if err := execCommandContext(cctx, dockerBin(), "image", "inspect", image).Run(); err != nil {
		return fmt.Errorf("%w: %s — bouw het met `docker build -t %s .` in de wp-fleet-repo", ErrImageMissing, image, image)
	}
	return nil
}

// forwardSignals stuurt SIGINT/SIGTERM door naar docker, zodat Claude Code de
// container netjes kan stoppen.
func forwardSignals(cmd *exec.Cmd) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-ch:
				_ = cmd.Process.Signal(sig)
			case <-done:
				return
			}
		}
	}()
	return func() { signal.Stop(ch); close(done) }
}
```

Let op: de fout in `sitesMetSecret` wordt bewust niet in de melding gezet — die kan uitvoer van `security` bevatten.

- [ ] **Step 4: Run, verwacht PASS**

Run: `go test ./internal/adapters/wpfleet/ -v`
Expected: PASS (alle tests uit Task 5 en 6).

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/wpfleet/launch.go internal/adapters/wpfleet/launch_test.go
git commit -m "feat: wpfleet.Launch start de container met secrets via env

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Subcommando `mcp-launch`

**Files:**
- Create: `internal/app/mcp_launch.go`
- Test: `internal/app/mcp_launch_test.go`
- Modify: `main.go:17-23` (naast `--version`)

**Interfaces:**
- Consumes: `config.LoadGlobal`, `config.LoadWPSites`, `config.WPSitesPath`, `config.ResolveSecret`, `config.KeychainPrefix` (bestaand + Task 3/4); `wpfleet.Launch`, `wpfleet.LaunchConfig` (Task 6)
- Produces: `func RunMCPLaunch(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) int`; const `WPSecretAccountPrefix = "wp:"` en `func WPSecretAccount(id string) string` in package `config` (gebruikt door Task 8).

- [ ] **Step 1: Voeg de account-helper toe aan `internal/config/wpsites.go`**

```go
// WPSecretAccountPrefix is het Keychain-account-voorvoegsel voor het
// Application Password van een WordPress-site.
const WPSecretAccountPrefix = "wp:"

func WPSecretAccount(id string) string { return WPSecretAccountPrefix + id }
```

- [ ] **Step 2: Falende test `internal/app/mcp_launch_test.go`**

```go
package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunMCPLaunchOngeldigBestand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	p := filepath.Join(home, ".config", "rdm", "wp-sites.yml")
	os.MkdirAll(filepath.Dir(p), 0o700)
	os.WriteFile(p, []byte("sites:\n  - id: X\n"), 0o600)

	var stdout, stderr bytes.Buffer
	code := RunMCPLaunch(context.Background(), strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exitcode %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout moet leeg blijven (MCP-kanaal): %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "wp-sites.yml") {
		t.Fatalf("stderr noemt het bestand niet: %q", stderr.String())
	}
}
```

- [ ] **Step 3: Run, verwacht FAIL**

Run: `go test ./internal/app/ -run TestRunMCPLaunch -v`
Expected: FAIL — `undefined: RunMCPLaunch`.

- [ ] **Step 4: Implementeer `internal/app/mcp_launch.go`**

```go
package app

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/rdm/sites-tool/internal/adapters/wpfleet"
	"github.com/rdm/sites-tool/internal/config"
)

// RunMCPLaunch is het subcommando dat Claude Code start als wp-fleet-server.
// Stdout is het MCP-kanaal; alles wat de tool zelf meldt gaat naar stderr.
// Bewust niet via LoadConfig: de Keychain-migratie daar kan om toestemming
// vragen, en dat hoort niet in een proces zonder venster.
func RunMCPLaunch(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) int {
	g, err := config.LoadGlobal()
	if err != nil {
		fmt.Fprintf(stderr, "wp-fleet: %v\n", err)
		return 1
	}
	sites, err := config.LoadWPSites(config.WPSitesPath())
	if err != nil {
		fmt.Fprintf(stderr, "wp-fleet: %v\n", err)
		return 1
	}
	code, err := wpfleet.Launch(ctx, wpfleet.LaunchConfig{
		Sites:    sites,
		Image:    g.WPFleet.Image,
		ReadOnly: g.WPFleet.ReadOnly,
		Secret: func(id string) (string, error) {
			return config.ResolveSecret(config.KeychainPrefix + config.WPSecretAccount(id))
		},
		Stdin: stdin, Stdout: stdout, Stderr: stderr,
		BaseEnv: os.Environ(),
	})
	if err != nil {
		fmt.Fprintf(stderr, "wp-fleet: %v\n", err)
	}
	return code
}
```

- [ ] **Step 5: Wijzig `main.go`** — direct na het `--version`-blok (voeg `"context"` toe aan de imports):

```go
	// Claude Code start de app zo als MCP-server voor wp-fleet: zonder venster,
	// met stdin/stdout als MCP-kanaal.
	if len(os.Args) > 1 && os.Args[1] == "mcp-launch" {
		os.Exit(app.RunMCPLaunch(context.Background(), os.Stdin, os.Stdout, os.Stderr))
	}
```

- [ ] **Step 6: Run, verwacht PASS**

Run: `go test ./internal/app/ ./internal/config/ -v -run 'RunMCPLaunch|WPSites'`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/app/mcp_launch.go internal/app/mcp_launch_test.go internal/config/wpsites.go main.go
git commit -m "feat: subcommando mcp-launch start wp-fleet vanuit de app

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: `WPSiteService` — beheer, projectkoppeling en bridge-detectie

**Files:**
- Create: `internal/services/wpsite_service.go`
- Test: `internal/services/wpsite_service_test.go`

**Interfaces:**
- Consumes: `domain.WPSite`, `domain.SuggestWPSiteID` (Task 2); `config.LoadWPSites`, `config.SaveWPSites`, `config.WPSecretAccount`, `config.KeychainSet`, `config.KeychainDelete`, `config.ResolveSecret`, `config.KeychainPrefix`; `domain.Project` (`ID`, `Path`, `DisplayName`, `Deploy.Link.Prod`)
- Produces:
  - `type WPProjectLookup interface { Get(id string) (domain.Project, bool) }` (voldaan door `*ProjectService`)
  - `type WPSecretStore interface { Set(account, value string) error; Get(account string) (string, error); Delete(account string) error }` + `type KeychainSecrets struct{}`
  - `type WPSiteView struct { Site domain.WPSite \`json:"site"\`; HasPassword bool \`json:"hasPassword"\`; ProjectMissing bool \`json:"projectMissing"\` }`
  - `func NewWPSiteService(path string, projects WPProjectLookup, secrets WPSecretStore) *WPSiteService`
  - Methods: `List() ([]WPSiteView, error)`, `ForProject(projectID string) (*WPSiteView, error)`, `SuggestForProject(projectID string) (domain.WPSite, error)`, `DetectBridge(projectID string) (bool, error)`, `Create(site domain.WPSite, password string) (WPSiteView, error)`, `Update(site domain.WPSite, password string) (WPSiteView, error)`, `Unlink(id string) error`, `Delete(id string) error`, `Stats() (total, withoutPassword int, err error)`
  - interne helpers voor Task 9: `(s *WPSiteService) find(id string) (domain.WPSite, error)`, `(s *WPSiteService) saveSite(site domain.WPSite) error`, `(s *WPSiteService) secret(id string) (string, error)`

- [ ] **Step 1: Falende tests**

```go
package services

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

type wpNepSecrets struct{ m map[string]string }

func (n *wpNepSecrets) Set(a, v string) error { n.m[a] = v; return nil }
func (n *wpNepSecrets) Get(a string) (string, error) {
	if v, ok := n.m[a]; ok {
		return v, nil
	}
	return "", errors.New("niet gevonden")
}
func (n *wpNepSecrets) Delete(a string) error { delete(n.m, a); return nil }

type wpNepProjecten map[string]domain.Project

func (n wpNepProjecten) Get(id string) (domain.Project, bool) { p, ok := n[id]; return p, ok }

func nieuweWPSiteService(t *testing.T) (*WPSiteService, *wpNepSecrets, string) {
	t.Helper()
	dir := t.TempDir()
	projDir := filepath.Join(dir, "web-ekcoatingsnl")
	os.MkdirAll(filepath.Join(projDir, "public", "wp-content", "mu-plugins"), 0o755)
	p := domain.Project{ID: "p1", Path: projDir, DisplayName: "web-ekcoatingsnl"}
	p.Deploy.Link.Prod = "https://www.ekcoating.nl"
	sec := &wpNepSecrets{m: map[string]string{}}
	svc := NewWPSiteService(filepath.Join(dir, "wp-sites.yml"), wpNepProjecten{"p1": p}, sec)
	return svc, sec, projDir
}

func testWPSite(id, projectPath string) domain.WPSite {
	return domain.WPSite{ID: id, Name: id, URL: "https://" + id + ".nl", Username: "mcp-bot", ProjectPath: projectPath}
}

func TestWPSiteCreateBewaartSecretApart(t *testing.T) {
	svc, sec, _ := nieuweWPSiteService(t)
	v, err := svc.Create(testWPSite("depot", ""), "pw-1")
	if err != nil {
		t.Fatal(err)
	}
	if !v.HasPassword || sec.m["wp:depot"] != "pw-1" {
		t.Fatalf("secret niet in Keychain: %+v %v", v, sec.m)
	}
	list, _ := svc.List()
	if len(list) != 1 || list[0].Site.ID != "depot" {
		t.Fatalf("list: %+v", list)
	}
}

func TestWPSiteCreateWeigertDubbeleID(t *testing.T) {
	svc, _, _ := nieuweWPSiteService(t)
	svc.Create(testWPSite("depot", ""), "pw")
	if _, err := svc.Create(testWPSite("depot", ""), "pw"); err == nil {
		t.Fatal("verwacht fout bij dubbele id")
	}
}

func TestWPSiteCreateOngeldigSchrijftGeenSecret(t *testing.T) {
	svc, sec, _ := nieuweWPSiteService(t)
	s := testWPSite("depot", "")
	s.URL = "http://depot.nl"
	if _, err := svc.Create(s, "pw"); err == nil {
		t.Fatal("verwacht validatiefout")
	}
	if len(sec.m) != 0 {
		t.Fatalf("secret achtergebleven: %v", sec.m)
	}
}

func TestWPSiteUpdateLeegWachtwoordHoudtBestaand(t *testing.T) {
	svc, sec, _ := nieuweWPSiteService(t)
	svc.Create(testWPSite("depot", ""), "pw-1")
	s := testWPSite("depot", "")
	s.Name = "Depot nieuw"
	if _, err := svc.Update(s, ""); err != nil {
		t.Fatal(err)
	}
	if sec.m["wp:depot"] != "pw-1" {
		t.Fatalf("wachtwoord overschreven: %v", sec.m)
	}
	if _, err := svc.Update(testWPSite("bestaat-niet", ""), ""); err == nil {
		t.Fatal("verwacht fout voor onbekende id")
	}
}

func TestWPSiteForProjectEnUnlinkEnDelete(t *testing.T) {
	svc, sec, projDir := nieuweWPSiteService(t)
	if v, _ := svc.ForProject("p1"); v != nil {
		t.Fatal("verwacht nil zonder koppeling")
	}
	svc.Create(testWPSite("ekcoating", projDir), "pw")
	v, err := svc.ForProject("p1")
	if err != nil || v == nil || v.Site.ID != "ekcoating" {
		t.Fatalf("ForProject: %+v %v", v, err)
	}
	if err := svc.Unlink("ekcoating"); err != nil {
		t.Fatal(err)
	}
	if v, _ := svc.ForProject("p1"); v != nil {
		t.Fatal("koppeling niet verwijderd")
	}
	list, _ := svc.List()
	if len(list) != 1 || list[0].Site.ProjectPath != "" {
		t.Fatalf("ontkoppelen moet een losse site overhouden: %+v", list)
	}
	if err := svc.Delete("ekcoating"); err != nil {
		t.Fatal(err)
	}
	if _, ok := sec.m["wp:ekcoating"]; ok {
		t.Fatal("Keychain-item niet gewist")
	}
}

func TestWPSiteProjectMissing(t *testing.T) {
	svc, _, _ := nieuweWPSiteService(t)
	svc.Create(testWPSite("weg", "/bestaat/niet"), "pw")
	list, _ := svc.List()
	if !list[0].ProjectMissing {
		t.Fatal("ProjectMissing moet true zijn")
	}
}

func TestWPSiteSuggestForProject(t *testing.T) {
	svc, _, projDir := nieuweWPSiteService(t)
	s, err := svc.SuggestForProject("p1")
	if err != nil {
		t.Fatal(err)
	}
	want := domain.WPSite{ID: "web-ekcoatingsnl", Name: "web-ekcoatingsnl", URL: "https://www.ekcoating.nl",
		Username: "mcp-bot", ProjectPath: projDir}
	if s.ID != want.ID || s.Name != want.Name || s.URL != want.URL || s.Username != want.Username || s.ProjectPath != want.ProjectPath {
		t.Fatalf("got %+v, want %+v", s, want)
	}
}

func TestWPSiteDetectBridge(t *testing.T) {
	svc, _, projDir := nieuweWPSiteService(t)
	mu := filepath.Join(projDir, "public", "wp-content", "mu-plugins")
	if ok, _ := svc.DetectBridge("p1"); ok {
		t.Fatal("geen bridge verwacht")
	}
	os.WriteFile(filepath.Join(mu, "nb-mcp-bridge.php"), []byte("<?php"), 0o644)
	if ok, _ := svc.DetectBridge("p1"); !ok {
		t.Fatal("bridge-bestand niet gevonden")
	}
	os.Remove(filepath.Join(mu, "nb-mcp-bridge.php"))
	os.MkdirAll(filepath.Join(mu, "nb-mcp-bridge"), 0o755)
	if ok, _ := svc.DetectBridge("p1"); !ok {
		t.Fatal("bridge-map niet gevonden")
	}
	if _, err := svc.DetectBridge("onbekend"); err == nil {
		t.Fatal("verwacht fout voor onbekend project")
	}
}

func TestWPSiteStats(t *testing.T) {
	svc, sec, _ := nieuweWPSiteService(t)
	svc.Create(testWPSite("aa", ""), "pw")
	svc.Create(testWPSite("bb", ""), "pw")
	delete(sec.m, "wp:bb")
	total, zonder, err := svc.Stats()
	if err != nil || total != 2 || zonder != 1 {
		t.Fatalf("Stats = %d, %d, %v", total, zonder, err)
	}
}
```

- [ ] **Step 2: Run, verwacht FAIL**

Run: `go test ./internal/services/ -run 'WPSite' -v`
Expected: FAIL — `undefined: NewWPSiteService`.

- [ ] **Step 3: Implementeer `internal/services/wpsite_service.go`**

```go
package services

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// WPProjectLookup is het deel van ProjectService dat WPSiteService nodig heeft.
type WPProjectLookup interface {
	Get(id string) (domain.Project, bool)
}

// WPSecretStore abstraheert de Keychain zodat de service te testen is.
type WPSecretStore interface {
	Set(account, value string) error
	Get(account string) (string, error)
	Delete(account string) error
}

// KeychainSecrets is de echte WPSecretStore.
type KeychainSecrets struct{}

func (KeychainSecrets) Set(a, v string) error { return config.KeychainSet(a, v) }
func (KeychainSecrets) Get(a string) (string, error) {
	return config.ResolveSecret(config.KeychainPrefix + a)
}
func (KeychainSecrets) Delete(a string) error { return config.KeychainDelete(a) }

// WPSiteView is wat de frontend van een site ziet: nooit het wachtwoord zelf.
type WPSiteView struct {
	Site           domain.WPSite `json:"site"`
	HasPassword    bool          `json:"hasPassword"`
	ProjectMissing bool          `json:"projectMissing"`
}

// WPSiteService beheert de WordPress-sites voor wp-fleet (wp-sites.yml + Keychain).
type WPSiteService struct {
	mu       sync.Mutex
	path     string
	projects WPProjectLookup
	secrets  WPSecretStore
	http     *http.Client
}

const defaultWPUser = "mcp-bot"

func NewWPSiteService(path string, projects WPProjectLookup, secrets WPSecretStore) *WPSiteService {
	return &WPSiteService{
		path: path, projects: projects, secrets: secrets,
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *WPSiteService) List() ([]WPSiteView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sites, err := config.LoadWPSites(s.path)
	if err != nil {
		return nil, err
	}
	out := make([]WPSiteView, 0, len(sites))
	for _, site := range sites {
		out = append(out, s.view(site))
	}
	return out, nil
}

func (s *WPSiteService) ForProject(projectID string) (*WPSiteView, error) {
	p, ok := s.projects.Get(projectID)
	if !ok {
		return nil, fmt.Errorf("project %s niet gevonden", projectID)
	}
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, v := range list {
		if v.Site.ProjectPath == p.Path {
			return &v, nil
		}
	}
	return nil, nil
}

// SuggestForProject vult een nieuw formulier voor een project voor.
func (s *WPSiteService) SuggestForProject(projectID string) (domain.WPSite, error) {
	p, ok := s.projects.Get(projectID)
	if !ok {
		return domain.WPSite{}, fmt.Errorf("project %s niet gevonden", projectID)
	}
	return domain.WPSite{
		ID:          domain.SuggestWPSiteID(filepath.Base(p.Path)),
		Name:        p.DisplayName,
		URL:         p.Deploy.Link.Prod,
		Username:    defaultWPUser,
		ProjectPath: p.Path,
	}, nil
}

// DetectBridge kijkt of de nb-mcp-bridge mu-plugin in de checkout staat.
func (s *WPSiteService) DetectBridge(projectID string) (bool, error) {
	p, ok := s.projects.Get(projectID)
	if !ok {
		return false, fmt.Errorf("project %s niet gevonden", projectID)
	}
	mu := filepath.Join(p.Path, "public", "wp-content", "mu-plugins")
	for _, naam := range []string{"nb-mcp-bridge.php", "nb-mcp-bridge"} {
		if _, err := os.Stat(filepath.Join(mu, naam)); err == nil {
			return true, nil
		}
	}
	return false, nil
}

func (s *WPSiteService) Create(site domain.WPSite, password string) (WPSiteView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sites, err := config.LoadWPSites(s.path)
	if err != nil {
		return WPSiteView{}, err
	}
	if err := site.Validate(); err != nil {
		return WPSiteView{}, err
	}
	for _, bestaand := range sites {
		if bestaand.ID == site.ID {
			return WPSiteView{}, fmt.Errorf("id %q bestaat al", site.ID)
		}
	}
	if password == "" {
		return WPSiteView{}, errors.New("Application Password is verplicht bij een nieuwe site")
	}
	if err := s.secrets.Set(config.WPSecretAccount(site.ID), password); err != nil {
		return WPSiteView{}, err
	}
	if err := config.SaveWPSites(s.path, append(sites, site)); err != nil {
		_ = s.secrets.Delete(config.WPSecretAccount(site.ID))
		return WPSiteView{}, err
	}
	return s.view(site), nil
}

// Update wijzigt een bestaande site; een leeg wachtwoord laat het bestaande staan.
func (s *WPSiteService) Update(site domain.WPSite, password string) (WPSiteView, error) {
	if err := s.saveSite(site); err != nil {
		return WPSiteView{}, err
	}
	if password != "" {
		if err := s.secrets.Set(config.WPSecretAccount(site.ID), password); err != nil {
			return WPSiteView{}, err
		}
	}
	return s.view(site), nil
}

// Unlink maakt van een gekoppelde site een losse site; er gaat niets verloren.
func (s *WPSiteService) Unlink(id string) error {
	site, err := s.find(id)
	if err != nil {
		return err
	}
	site.ProjectPath = ""
	return s.saveSite(site)
}

func (s *WPSiteService) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sites, err := config.LoadWPSites(s.path)
	if err != nil {
		return err
	}
	rest := make([]domain.WPSite, 0, len(sites))
	gevonden := false
	for _, site := range sites {
		if site.ID == id {
			gevonden = true
			continue
		}
		rest = append(rest, site)
	}
	if !gevonden {
		return fmt.Errorf("site %q niet gevonden", id)
	}
	if err := config.SaveWPSites(s.path, rest); err != nil {
		return err
	}
	// Een ontbrekend Keychain-item is geen fout: het doel is dat het weg is.
	_ = s.secrets.Delete(config.WPSecretAccount(id))
	return nil
}

// Stats telt sites en sites zonder wachtwoord, voor het instellingenblok.
func (s *WPSiteService) Stats() (int, int, error) {
	list, err := s.List()
	if err != nil {
		return 0, 0, err
	}
	zonder := 0
	for _, v := range list {
		if !v.HasPassword {
			zonder++
		}
	}
	return len(list), zonder, nil
}

func (s *WPSiteService) find(id string) (domain.WPSite, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sites, err := config.LoadWPSites(s.path)
	if err != nil {
		return domain.WPSite{}, err
	}
	for _, site := range sites {
		if site.ID == id {
			return site, nil
		}
	}
	return domain.WPSite{}, fmt.Errorf("site %q niet gevonden", id)
}

// saveSite vervangt de site met dezelfde id.
func (s *WPSiteService) saveSite(site domain.WPSite) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sites, err := config.LoadWPSites(s.path)
	if err != nil {
		return err
	}
	for i := range sites {
		if sites[i].ID == site.ID {
			next := append([]domain.WPSite{}, sites...)
			next[i] = site
			return config.SaveWPSites(s.path, next)
		}
	}
	return fmt.Errorf("site %q niet gevonden", site.ID)
}

func (s *WPSiteService) secret(id string) (string, error) {
	return s.secrets.Get(config.WPSecretAccount(id))
}

func (s *WPSiteService) view(site domain.WPSite) WPSiteView {
	pw, err := s.secret(site.ID)
	missing := false
	if site.ProjectPath != "" {
		if _, statErr := os.Stat(site.ProjectPath); statErr != nil {
			missing = true
		}
	}
	return WPSiteView{Site: site, HasPassword: err == nil && pw != "", ProjectMissing: missing}
}
```

- [ ] **Step 4: Run, verwacht PASS**

Run: `go test ./internal/services/ -run 'WPSite' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/wpsite_service.go internal/services/wpsite_service_test.go
git commit -m "feat: WPSiteService voor sites, projectkoppeling en bridge-detectie

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Test verbinding

**Files:**
- Create: `internal/services/wpsite_connection.go`
- Test: `internal/services/wpsite_connection_test.go`

**Interfaces:**
- Consumes: `WPSiteService` + `find`, `saveSite`, `secret`, veld `http` (Task 8)
- Produces: `type ConnectionResult struct { Status string \`json:"status"\`; Message string \`json:"message"\` }`; consts `ConnOK="ok"`, `ConnAuthFailed="auth_failed"`, `ConnNoBridge="no_bridge"`, `ConnUnreachable="unreachable"`; `func (s *WPSiteService) TestConnection(id string) (ConnectionResult, error)`

- [ ] **Step 1: Falende tests**

```go
package services

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func wpServer(t *testing.T, usersMe, bridge int) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pw, ok := r.BasicAuth()
		if !ok || user != "mcp-bot" || pw != "goed" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/wp-json/wp/v2/users/me":
			w.WriteHeader(usersMe)
		case "/wp-json/nb-mcp/v1/status":
			w.WriteHeader(bridge)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testVerbinding(t *testing.T, srv *httptest.Server, pw string) (ConnectionResult, domain.WPSite) {
	t.Helper()
	svc, _, _ := nieuweWPSiteService(t)
	svc.http = srv.Client()
	s := domain.WPSite{ID: "klant", Name: "Klant", URL: srv.URL, Username: "mcp-bot"}
	if _, err := svc.Create(s, pw); err != nil {
		t.Fatal(err)
	}
	res, err := svc.TestConnection("klant")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := svc.find("klant")
	return res, got
}

func TestTestConnectionOK(t *testing.T) {
	res, site := testVerbinding(t, wpServer(t, 200, 200), "goed")
	if res.Status != ConnOK || !site.Bridge {
		t.Fatalf("res %+v, bridge %v", res, site.Bridge)
	}
}

func TestTestConnectionGeenBridge(t *testing.T) {
	res, site := testVerbinding(t, wpServer(t, 200, 404), "goed")
	if res.Status != ConnNoBridge || site.Bridge {
		t.Fatalf("res %+v, bridge %v", res, site.Bridge)
	}
}

func TestTestConnectionAuthFout(t *testing.T) {
	res, _ := testVerbinding(t, wpServer(t, 200, 200), "fout-wachtwoord")
	if res.Status != ConnAuthFailed {
		t.Fatalf("res %+v", res)
	}
	if strings.Contains(res.Message, "fout-wachtwoord") {
		t.Fatal("wachtwoord in melding")
	}
}

func TestTestConnectionOnbereikbaar(t *testing.T) {
	srv := wpServer(t, 200, 200)
	url := srv.URL
	svc, _, _ := nieuweWPSiteService(t)
	svc.http = srv.Client()
	svc.Create(domain.WPSite{ID: "klant", Name: "K", URL: url, Username: "mcp-bot"}, "goed")
	srv.Close()
	res, err := svc.TestConnection("klant")
	if err != nil || res.Status != ConnUnreachable {
		t.Fatalf("res %+v, err %v", res, err)
	}
}
```

- [ ] **Step 2: Run, verwacht FAIL**

Run: `go test ./internal/services/ -run 'TestConnection' -v`
Expected: FAIL — `undefined: ConnectionResult`.

- [ ] **Step 3: Implementeer `wpsite_connection.go`**

```go
package services

import (
	"fmt"
	"net/http"
	"strings"
)

const (
	ConnOK          = "ok"
	ConnAuthFailed  = "auth_failed"
	ConnNoBridge    = "no_bridge"
	ConnUnreachable = "unreachable"
)

type ConnectionResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// TestConnection controleert auth (users/me vereist een ingelogde gebruiker)
// en de bridge, en zet de bridge-vlag van de site op de uitkomst.
func (s *WPSiteService) TestConnection(id string) (ConnectionResult, error) {
	site, err := s.find(id)
	if err != nil {
		return ConnectionResult{}, err
	}
	pw, err := s.secret(id)
	if err != nil || pw == "" {
		return ConnectionResult{Status: ConnAuthFailed, Message: "geen Application Password opgeslagen"}, nil
	}
	base := strings.TrimRight(site.URL, "/")

	code, err := s.get(base+"/wp-json/wp/v2/users/me", site.Username, pw)
	if err != nil {
		return ConnectionResult{Status: ConnUnreachable, Message: "site niet bereikbaar"}, nil
	}
	if code == http.StatusUnauthorized || code == http.StatusForbidden {
		return ConnectionResult{Status: ConnAuthFailed, Message: "gebruiker of Application Password klopt niet"}, nil
	}
	if code != http.StatusOK {
		return ConnectionResult{Status: ConnUnreachable, Message: fmt.Sprintf("WordPress antwoordde HTTP %d", code)}, nil
	}

	code, err = s.get(base+"/wp-json/nb-mcp/v1/status", site.Username, pw)
	if err != nil {
		return ConnectionResult{Status: ConnUnreachable, Message: "bridge-check niet bereikbaar"}, nil
	}
	res := ConnectionResult{Status: ConnOK, Message: "verbinding en bridge in orde"}
	site.Bridge = code == http.StatusOK
	if !site.Bridge {
		res = ConnectionResult{Status: ConnNoBridge, Message: "verbinding ok, nb-mcp-bridge ontbreekt"}
	}
	if err := s.saveSite(site); err != nil {
		return ConnectionResult{}, err
	}
	return res, nil
}

// get doet een GET met Basic Auth en geeft alleen de statuscode terug; de
// fout zelf kan de URL bevatten maar nooit het wachtwoord.
func (s *WPSiteService) get(url, user, pw string) (int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.SetBasicAuth(user, pw)
	resp, err := s.http.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}
```

- [ ] **Step 4: Run, verwacht PASS**

Run: `go test ./internal/services/ -run 'WPSite|TestConnection' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/wpsite_connection.go internal/services/wpsite_connection_test.go
git commit -m "feat: verbinding en bridge van een WordPress-site testen

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: `WPFleetService` — registratie bij Claude Code en instellingen

**Files:**
- Create: `internal/services/wpfleet_service.go`
- Test: `internal/services/wpfleet_service_test.go`

**Interfaces:**
- Consumes: `WPSiteService.Stats()` (Task 8); `config.Global`, `config.SaveGlobal`, `config.WPFleetGlobal` (Task 4); `claudecode.Bin()` uit `internal/adapters/claudecode`
- Produces:
  - `type ClaudeRunner func(ctx context.Context, args ...string) (string, error)`
  - `type FleetStatus struct { Registered, PointsToThisApp, StableLocation bool; Executable, Image string; ReadOnly bool; Sites, SitesWithoutPassword int }` (JSON camelCase)
  - `func NewWPFleetService(cfg *config.Global, sites *WPSiteService, run ClaudeRunner, executable func() (string, error)) *WPFleetService`
  - Methods: `Status() (FleetStatus, error)`, `Register() (FleetStatus, error)`, `Unregister() error`, `SaveSettings(image string, readOnly bool) error`
  - `func DefaultClaudeRunner(ctx context.Context, args ...string) (string, error)`

- [ ] **Step 1: Falende tests**

```go
package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/config"
)

type nepClaude struct {
	calls    [][]string
	getOut   string
	getErr   error
	addErr   error
}

func (n *nepClaude) run(_ context.Context, args ...string) (string, error) {
	n.calls = append(n.calls, args)
	switch {
	case len(args) >= 2 && args[1] == "get":
		return n.getOut, n.getErr
	case len(args) >= 2 && args[1] == "add":
		return "", n.addErr
	}
	return "", nil
}

func nieuweFleet(t *testing.T, exe string) (*WPFleetService, *nepClaude, *config.Global) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	sites, _, _ := nieuweWPSiteService(t)
	cfg := &config.Global{WPFleet: config.WPFleetGlobal{Image: config.DefaultWPFleetImage}}
	nc := &nepClaude{getErr: errors.New("not found")}
	svc := NewWPFleetService(cfg, sites, nc.run, func() (string, error) { return exe, nil })
	return svc, nc, cfg
}

const appExe = "/Applications/rdm-sites-tool.app/Contents/MacOS/rdm-sites-tool"

func TestFleetRegisterVoertRemoveEnAddUit(t *testing.T) {
	svc, nc, _ := nieuweFleet(t, appExe)
	nc.getErr = nil
	nc.getOut = "wp-fleet:\n  Command: " + appExe + "\n  Args: mcp-launch\n"
	st, err := svc.Register()
	if err != nil {
		t.Fatal(err)
	}
	var add []string
	for _, c := range nc.calls {
		if c[1] == "add" {
			add = c
		}
	}
	want := []string{"mcp", "add", "wp-fleet", "-s", "user", "--", appExe, "mcp-launch"}
	if strings.Join(add, " ") != strings.Join(want, " ") {
		t.Fatalf("add = %v, want %v", add, want)
	}
	if nc.calls[0][1] != "remove" {
		t.Fatalf("eerste aanroep moet remove zijn: %v", nc.calls)
	}
	if !st.StableLocation || !st.Registered || !st.PointsToThisApp {
		t.Fatalf("status na registratie: %+v", st)
	}
}

func TestFleetRegisterClaudeOntbreekt(t *testing.T) {
	svc, nc, _ := nieuweFleet(t, appExe)
	nc.addErr = errors.New("executable file not found")
	_, err := svc.Register()
	if err == nil || !strings.Contains(err.Error(), "claude mcp add wp-fleet -s user -- "+appExe+" mcp-launch") {
		t.Fatalf("fout moet het handmatige commando noemen: %v", err)
	}
}

func TestFleetStatus(t *testing.T) {
	svc, nc, _ := nieuweFleet(t, "/Users/x/Projects/RDM/bin/rdm-sites-tool.dev.app/Contents/MacOS/rdm-sites-tool")
	st, err := svc.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.Registered || st.StableLocation {
		t.Fatalf("status %+v", st)
	}
	nc.getErr = nil
	nc.getOut = "wp-fleet:\n  Command: docker\n  Args: run -i --rm --env-file /x/.env wp-fleet-mcp\n"
	st, _ = svc.Status()
	if !st.Registered || st.PointsToThisApp {
		t.Fatalf("oude registratie hoort niet naar deze app te wijzen: %+v", st)
	}
}

func TestFleetSaveSettings(t *testing.T) {
	svc, _, cfg := nieuweFleet(t, appExe)
	if err := svc.SaveSettings("ghcr.io/org/wp-fleet:1.2.0", true); err != nil {
		t.Fatal(err)
	}
	if cfg.WPFleet.Image != "ghcr.io/org/wp-fleet:1.2.0" || !cfg.WPFleet.ReadOnly {
		t.Fatalf("cfg %+v", cfg.WPFleet)
	}
	g, _ := config.LoadGlobal()
	if g.WPFleet.Image != "ghcr.io/org/wp-fleet:1.2.0" {
		t.Fatalf("niet opgeslagen: %+v", g.WPFleet)
	}
	if err := svc.SaveSettings("  ", false); err == nil {
		t.Fatal("lege image moet geweigerd worden")
	}
}
```

- [ ] **Step 2: Run, verwacht FAIL**

Run: `go test ./internal/services/ -run 'Fleet' -v`
Expected: FAIL — `undefined: NewWPFleetService`.

- [ ] **Step 3: Implementeer `wpfleet_service.go`**

```go
package services

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/claudecode"
	"github.com/rdm/sites-tool/internal/config"
)

const wpFleetServerName = "wp-fleet"

type ClaudeRunner func(ctx context.Context, args ...string) (string, error)

// DefaultClaudeRunner draait de Claude Code CLI; Bin() vindt hem ook als de app
// vanuit Finder is gestart.
func DefaultClaudeRunner(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, claudecode.Bin(), args...).CombinedOutput()
	return string(out), err
}

type FleetStatus struct {
	Registered           bool   `json:"registered"`
	PointsToThisApp      bool   `json:"pointsToThisApp"`
	StableLocation       bool   `json:"stableLocation"`
	Executable           string `json:"executable"`
	Image                string `json:"image"`
	ReadOnly             bool   `json:"readOnly"`
	Sites                int    `json:"sites"`
	SitesWithoutPassword int    `json:"sitesWithoutPassword"`
}

type WPFleetService struct {
	cfg        *config.Global
	sites      *WPSiteService
	run        ClaudeRunner
	executable func() (string, error)
}

func NewWPFleetService(cfg *config.Global, sites *WPSiteService, run ClaudeRunner, executable func() (string, error)) *WPFleetService {
	return &WPFleetService{cfg: cfg, sites: sites, run: run, executable: executable}
}

func (s *WPFleetService) Status() (FleetStatus, error) {
	exe, err := s.executable()
	if err != nil {
		return FleetStatus{}, fmt.Errorf("pad van de app: %w", err)
	}
	total, zonder, err := s.sites.Stats()
	if err != nil {
		return FleetStatus{}, err
	}
	st := FleetStatus{
		Executable:           exe,
		StableLocation:       strings.HasPrefix(exe, "/Applications/"),
		Image:                s.cfg.WPFleet.Image,
		ReadOnly:             s.cfg.WPFleet.ReadOnly,
		Sites:                total,
		SitesWithoutPassword: zonder,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if out, err := s.run(ctx, "mcp", "get", wpFleetServerName); err == nil {
		st.Registered = true
		st.PointsToThisApp = strings.Contains(out, exe) && strings.Contains(out, "mcp-launch")
	}
	return st, nil
}

// Register vervangt een bestaande wp-fleet-registratie (bijv. de oude
// docker run met --env-file) door deze app als launcher.
func (s *WPFleetService) Register() (FleetStatus, error) {
	exe, err := s.executable()
	if err != nil {
		return FleetStatus{}, fmt.Errorf("pad van de app: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Bestaat hij niet, dan faalt remove; dat is prima.
	_, _ = s.run(ctx, "mcp", "remove", wpFleetServerName, "-s", "user")
	args := []string{"mcp", "add", wpFleetServerName, "-s", "user", "--", exe, "mcp-launch"}
	if out, err := s.run(ctx, args...); err != nil {
		return FleetStatus{}, fmt.Errorf("registreren mislukt (%v %s) — voer zelf uit: claude %s",
			err, strings.TrimSpace(out), strings.Join(args, " "))
	}
	return s.Status()
}

func (s *WPFleetService) Unregister() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := s.run(ctx, "mcp", "remove", wpFleetServerName, "-s", "user"); err != nil {
		return fmt.Errorf("registratie verwijderen mislukt: %v %s", err, strings.TrimSpace(out))
	}
	return nil
}

func (s *WPFleetService) SaveSettings(image string, readOnly bool) error {
	image = strings.TrimSpace(image)
	if image == "" {
		return errors.New("image-referentie is verplicht")
	}
	next := *s.cfg
	next.WPFleet = config.WPFleetGlobal{Image: image, ReadOnly: readOnly}
	if err := config.SaveGlobal(next); err != nil {
		return err
	}
	*s.cfg = next
	return nil
}
```

- [ ] **Step 4: Run, verwacht PASS**

Run: `go test ./internal/services/ -run 'Fleet|WPSite|TestConnection' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/wpfleet_service.go internal/services/wpfleet_service_test.go
git commit -m "feat: WPFleetService registreert wp-fleet bij Claude Code

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 11: Services koppelen en bindings genereren

**Files:**
- Modify: `internal/app/app.go` (struct `Services`, `NewServices`, `Wails()`)
- Generated: `frontend/bindings/**`

**Interfaces:**
- Consumes: `NewWPSiteService`, `KeychainSecrets`, `NewWPFleetService`, `DefaultClaudeRunner` (Task 8–10); `config.WPSitesPath()` (Task 3)
- Produces: frontend-bindings `Services.WPSiteService.*` en `Services.WPFleetService.*`; TS-modellen `WPSite` (domain), `WPSiteView`, `ConnectionResult`, `FleetStatus` (services).

- [ ] **Step 1: Wijzig `internal/app/app.go`**

In de struct `Services` na `Update`:

```go
	WPSite          *services.WPSiteService
	WPFleet         *services.WPFleetService
```

In `NewServices`, na `plugin := …` (voeg `"os"` en `config` toe aan de imports als ze ontbreken):

```go
	wpSite := services.NewWPSiteService(config.WPSitesPath(), project, services.KeychainSecrets{})
	wpFleet := services.NewWPFleetService(&cfg.Global, wpSite, services.DefaultClaudeRunner, os.Executable)
```

en in de literal `&Services{…}`:

```go
		WPSite:          wpSite,
		WPFleet:         wpFleet,
```

In `Wails()` onderaan de lijst:

```go
		application.NewService(s.WPSite),
		application.NewService(s.WPFleet),
```

- [ ] **Step 2: Compileer en test**

Run: `go build ./internal/... && go test ./internal/...`
Expected: geen fouten, alles `ok`.

- [ ] **Step 3: Bindings genereren**

Run: `cd /Users/jeffreyt/Projects/RDM-Sites-tool-wp-fleet && PATH="$HOME/go/bin:$PATH" wails3 task common:generate:bindings`
Expected: `Processed: … 30 Services …`. Controleer:
`ls frontend/bindings/github.com/rdm/sites-tool/internal/services/ | grep -i wp` toont `wpsiteservice.ts` en `wpfleetservice.ts`; `grep -n "class WPSite\b\|class WPSiteView\|class FleetStatus\|class ConnectionResult" -r frontend/bindings` vindt alle vier.

- [ ] **Step 4: Frontend typecheck (bindings compileren)**

Run: `cd frontend && npx tsc --noEmit`
Expected: geen fouten.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go frontend/bindings
git commit -m "feat: WPSite- en WPFleet-service aan de app koppelen

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 12: Gedeeld formulier `WPSiteForm` + blok in projectinstellingen

**Files:**
- Create: `frontend/src/components/WPSiteForm.tsx`
- Modify: `frontend/src/components/ProjectSettingsTab.tsx`

**Interfaces:**
- Consumes: `Services.WPSiteService.{ForProject, SuggestForProject, DetectBridge, Create, Update, TestConnection, Unlink, Delete}` (Task 11); modellen `WPSite` (`bindings/.../internal/domain/models`), `WPSiteView`, `ConnectionResult` (`bindings/.../internal/services/models`)
- Produces: `<WPSiteForm initial={WPSite} isNew={boolean} hasPassword={boolean} projectMissing={boolean} onSaved={(v: WPSiteView) => void} onUnlink?={() => void} onDelete?={() => void} />`

- [ ] **Step 1: Controleer de import-paden van de modellen**

Run: `ls frontend/bindings/github.com/rdm/sites-tool/internal/services/models.ts frontend/bindings/github.com/rdm/sites-tool/internal/domain/models.ts`
Expected: beide bestaan. Gebruik ze zoals `ProjectSettingsTab.tsx` de domain-modellen importeert.

- [ ] **Step 2: Maak `frontend/src/components/WPSiteForm.tsx`**

```tsx
import { useEffect, useState } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { WPSite } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import type { WPSiteView, ConnectionResult } from '../../bindings/github.com/rdm/sites-tool/internal/services/models'
import Foutvak from './Foutvak'

interface Props {
  initial: WPSite
  isNew: boolean
  hasPassword: boolean
  projectMissing?: boolean
  onSaved: (v: WPSiteView) => void
  onUnlink?: () => void
  onDelete?: () => void
}

const invoer = 'w-full bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg mb-2'
const knop = 'bg-accent text-white text-[12.5px] font-semibold px-4 py-1.5 rounded-lg hover:brightness-110 disabled:opacity-50 transition'
const knopStil = 'border border-border text-fg-muted text-[12.5px] px-3 py-1.5 rounded-lg hover:bg-hover disabled:opacity-50 transition'

const verbindingKleur: Record<string, string> = {
  ok: 'text-green', no_bridge: 'text-amber', auth_failed: 'text-red', unreachable: 'text-red',
}

function foutTekst(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

export default function WPSiteForm({ initial, isNew, hasPassword, projectMissing, onSaved, onUnlink, onDelete }: Props) {
  const [site, setSite] = useState<WPSite>(initial)
  const [tags, setTags] = useState((initial.tags ?? []).join(', '))
  const [wachtwoord, setWachtwoord] = useState('')
  const [bewaard, setBewaard] = useState(hasPassword)
  const [nieuw, setNieuw] = useState(isNew)
  const [bezig, setBezig] = useState(false)
  const [fout, setFout] = useState<string | null>(null)
  const [verbinding, setVerbinding] = useState<ConnectionResult | null>(null)

  useEffect(() => {
    setSite(initial); setTags((initial.tags ?? []).join(', '))
    setWachtwoord(''); setBewaard(hasPassword); setNieuw(isNew)
    setFout(null); setVerbinding(null)
  }, [initial.id, initial.projectPath, isNew, hasPassword])

  const zet = (veld: keyof WPSite, waarde: string | boolean) => setSite(s => ({ ...s, [veld]: waarde } as WPSite))

  const opslaan = async () => {
    setBezig(true); setFout(null)
    const volledig = { ...site, tags: tags.split(',').map(t => t.trim()).filter(Boolean) } as WPSite
    try {
      const v = nieuw
        ? await Services.WPSiteService.Create(volledig, wachtwoord)
        : await Services.WPSiteService.Update(volledig, wachtwoord)
      setWachtwoord(''); setBewaard(v.hasPassword); setNieuw(false)
      onSaved(v)
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setBezig(false)
    }
  }

  const testen = async () => {
    setBezig(true); setFout(null); setVerbinding(null)
    try {
      setVerbinding(await Services.WPSiteService.TestConnection(site.id))
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setBezig(false)
    }
  }

  const verwijderen = async () => {
    if (!onDelete) return
    if (!window.confirm(`Site "${site.name}" en het opgeslagen Application Password verwijderen?`)) return
    try {
      await Services.WPSiteService.Delete(site.id)
      onDelete()
    } catch (e) {
      setFout(foutTekst(e))
    }
  }

  const ontkoppelen = async () => {
    if (!onUnlink) return
    try {
      await Services.WPSiteService.Unlink(site.id)
      onUnlink()
    } catch (e) {
      setFout(foutTekst(e))
    }
  }

  return (
    <div>
      {fout && <Foutvak fout={fout} className="mb-3" />}
      {projectMissing && (
        <p className="text-[11.5px] text-amber mb-3">Gekoppelde map niet gevonden: {site.projectPath}</p>
      )}

      <label className="block text-[11px] text-fg-muted mb-1">Id (vast na aanmaken)</label>
      <input value={site.id} disabled={!nieuw} onChange={e => zet('id', e.target.value)}
        className={`${invoer} font-mono disabled:opacity-60`} />

      <label className="block text-[11px] text-fg-muted mb-1">Naam</label>
      <input value={site.name} onChange={e => zet('name', e.target.value)} className={invoer} />

      <label className="block text-[11px] text-fg-muted mb-1">URL</label>
      <input value={site.url} onChange={e => zet('url', e.target.value)} placeholder="https://www.klant.nl"
        className={`${invoer} font-mono`} />

      <label className="block text-[11px] text-fg-muted mb-1">WP-gebruiker</label>
      <input value={site.username} onChange={e => zet('username', e.target.value)} className={invoer} />

      <label className="block text-[11px] text-fg-muted mb-1">Application Password</label>
      <input type="password" value={wachtwoord} onChange={e => setWachtwoord(e.target.value)}
        placeholder={bewaard ? 'opgeslagen in de Keychain' : 'xxxx xxxx xxxx xxxx xxxx xxxx'}
        title="Wordt alleen in de macOS-Keychain bewaard. Leeg laten houdt het bestaande wachtwoord."
        className={invoer} />

      <label className="block text-[11px] text-fg-muted mb-1">Tags (komma-gescheiden)</label>
      <input value={tags} onChange={e => setTags(e.target.value)} placeholder="kinsta, production" className={invoer} />

      <label className="flex items-center gap-2 text-[12px] text-fg-muted mb-3">
        <input type="checkbox" checked={site.readOnly} onChange={e => zet('readOnly', e.target.checked)} />
        Alleen lezen (wp-fleet weigert schrijfacties voor deze site)
      </label>

      <div className="flex flex-wrap items-center gap-2">
        <button onClick={opslaan} disabled={bezig || !site.id || !site.url || (nieuw && !wachtwoord)} className={knop}>
          {bezig ? <span className="animate-spin inline-block">↻</span> : 'Opslaan'}
        </button>
        {!nieuw && <button onClick={testen} disabled={bezig} className={knopStil}>Test verbinding</button>}
        {!nieuw && onUnlink && <button onClick={ontkoppelen} className={knopStil}>Ontkoppelen</button>}
        {!nieuw && onDelete && <button onClick={verwijderen} className={`${knopStil} text-red`}>Verwijderen</button>}
        {verbinding && (
          <span className={`text-[11.5px] ${verbindingKleur[verbinding.status] ?? 'text-fg-muted'}`}>
            {verbinding.message}
          </span>
        )}
      </div>
    </div>
  )
}
```

Controleer dat de kleur-classes `text-green`, `text-amber`, `text-red` in het thema bestaan: `grep -rhoE "text-(green|amber|red)\b" frontend/src | sort | uniq -c`. Ontbreekt `text-amber`, gebruik dan de class die elders voor waarschuwingen wordt gebruikt (bijv. in `Foutvak.tsx`).

- [ ] **Step 3: Blok in `ProjectSettingsTab.tsx`**

Voeg imports toe:

```tsx
import WPSiteForm from './WPSiteForm'
import type { WPSite } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import type { WPSiteView } from '../../bindings/github.com/rdm/sites-tool/internal/services/models'
```

State (bij de andere `useState`s):

```tsx
  const [wpKoppeling, setWpKoppeling] = useState<WPSiteView | null>(null)
  const [wpVoorstel, setWpVoorstel] = useState<WPSite | null>(null)
  const [wpBridge, setWpBridge] = useState(false)
```

Laad-functie en effect (na het bestaande `useEffect`):

```tsx
  const laadWp = () => {
    Services.WPSiteService.ForProject(project.id)
      .then(v => setWpKoppeling(v ?? null))
      .catch(e => setFout(foutTekst(e)))
    Services.WPSiteService.SuggestForProject(project.id).then(setWpVoorstel).catch(() => {})
    Services.WPSiteService.DetectBridge(project.id).then(setWpBridge).catch(() => setWpBridge(false))
  }
  useEffect(laadWp, [project.id])
```

Onderaan in de JSX, na het blok "SSH-toegang" (geef dat blok `mb-4`):

```tsx
      {(wpBridge || wpKoppeling) && (wpKoppeling || wpVoorstel) && (
        <div className="bg-panel border border-border rounded-xl p-4">
          <div className="text-[12px] font-semibold text-fg mb-1">WordPress MCP</div>
          <p className="text-[10.5px] text-fg-faint mb-3">
            {wpKoppeling
              ? 'Gekoppeld aan wp-fleet. Gegevens staan in ~/.config/rdm/wp-sites.yml, het wachtwoord in de Keychain — niet in deze repo.'
              : 'nb-mcp-bridge gevonden in deze checkout. Vul de gegevens in om deze site aan wp-fleet toe te voegen.'}
          </p>
          <WPSiteForm
            initial={wpKoppeling ? wpKoppeling.site : wpVoorstel!}
            isNew={!wpKoppeling}
            hasPassword={wpKoppeling?.hasPassword ?? false}
            projectMissing={wpKoppeling?.projectMissing}
            onSaved={v => setWpKoppeling(v)}
            onUnlink={laadWp}
          />
        </div>
      )}
```

- [ ] **Step 4: Typecheck**

Run: `cd frontend && npx tsc --noEmit`
Expected: geen fouten. (Klaagt TS dat `WPSite` een class is met verplichte constructor bij de spread in `zet`, dan is de `as WPSite`-cast voldoende; bindings-classes hebben alleen velden.)

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/WPSiteForm.tsx frontend/src/components/ProjectSettingsTab.tsx
git commit -m "feat: WordPress MCP-koppeling in projectinstellingen

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 13: Losse sites in de zijbalk

**Files:**
- Create: `frontend/src/components/WPSitePage.tsx`
- Modify: `frontend/src/App.tsx` (state, sidebar-sectie boven de footer, detailpaneel-tak)

**Interfaces:**
- Consumes: `WPSiteForm` (Task 12); `Services.WPSiteService.List` (Task 11)
- Produces: `<WPSitePage view={WPSiteView | null} onSaved={(v) => void} onDeleted={() => void} />` — `view === null` betekent "nieuwe site".

- [ ] **Step 1: Maak `WPSitePage.tsx`**

```tsx
import WPSiteForm from './WPSiteForm'
import type { WPSite } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import type { WPSiteView } from '../../bindings/github.com/rdm/sites-tool/internal/services/models'

interface Props {
  view: WPSiteView | null
  onSaved: (v: WPSiteView) => void
  onDeleted: () => void
}

const leeg = { id: '', name: '', url: 'https://', username: 'mcp-bot', tags: [], readOnly: false, bridge: false } as unknown as WPSite

export default function WPSitePage({ view, onSaved, onDeleted }: Props) {
  return (
    <div className="flex-1 flex flex-col min-h-0 overflow-hidden">
      <div className="h-[56px] px-[22px] border-b border-border bg-panel shrink-0 flex items-center gap-2">
        <h2 className="text-[15px] font-bold tracking-[-.01em] text-fg flex-1">
          {view ? view.site.name : 'Nieuwe WordPress-site'}
        </h2>
      </div>
      <div className="flex-1 min-h-0 overflow-y-auto px-6 py-4">
        <div className="bg-panel border border-border rounded-xl p-4 max-w-[560px]">
          <p className="text-[10.5px] text-fg-faint mb-3">
            Losse site zonder projectmap. Gegevens staan in ~/.config/rdm/wp-sites.yml, het wachtwoord in de Keychain.
          </p>
          <WPSiteForm
            initial={view ? view.site : leeg}
            isNew={!view}
            hasPassword={view?.hasPassword ?? false}
            onSaved={onSaved}
            onDelete={onDeleted}
          />
        </div>
      </div>
    </div>
  )
}
```

- [ ] **Step 2: State en laden in `App.tsx`**

Imports:

```tsx
import WPSitePage from './components/WPSitePage'
import type { WPSiteView } from '../bindings/github.com/rdm/sites-tool/internal/services/models'
```

In `App()` bij de andere state:

```tsx
  const [wpSites, setWpSites] = useState<WPSiteView[]>([])
  // undefined = geen site geselecteerd, null = formulier voor een nieuwe site
  const [wpSelected, setWpSelected] = useState<WPSiteView | null | undefined>(undefined)

  const laadWpSites = useCallback(() => {
    ProjectService.WPSiteService.List()
      .then(list => setWpSites(list.filter(v => !v.site.projectPath)))
      .catch(() => setWpSites([]))
  }, [])
  useEffect(laadWpSites, [laadWpSites])
```

Zorg dat `selectProject(...)` ook `setWpSelected(undefined)` doet (voeg die regel toe aan het begin van de bestaande functie `selectProject`).

- [ ] **Step 3: Zijbalk-sectie** — direct vóór `{/* footer */}` in de projects-sub-sidebar:

```tsx
          {/* losse WordPress-sites (zonder projectmap) */}
          <div className="border-t border-border px-2 pt-2 pb-1 shrink-0 max-h-[30%] overflow-y-auto">
            <div className="flex items-center px-1.5 mb-1">
              <span className="text-[10.5px] font-semibold tracking-[.08em] text-fg-faint uppercase flex-1">
                WordPress-sites
              </span>
              <button onClick={() => { setSelected(null); setWpSelected(null) }} title="Losse site toevoegen"
                className="text-fg-muted hover:text-fg p-0.5 rounded">
                <PlusIcon size={12} />
              </button>
            </div>
            {wpSites.length === 0 ? (
              <p className="text-[11px] text-fg-faint px-1.5 py-1">Nog geen losse sites</p>
            ) : wpSites.map(v => (
              <button key={v.site.id} onClick={() => { setSelected(null); setWpSelected(v) }}
                className={`w-full text-left px-2 py-1 rounded-md text-[12.5px] truncate
                  ${wpSelected && wpSelected.site.id === v.site.id ? 'bg-accent-soft text-accent' : 'text-fg hover:bg-hover'}`}>
                {v.site.name}
                {!v.hasPassword && <span className="ml-1.5 text-[10.5px] text-fg-faint">geen wachtwoord</span>}
              </button>
            ))}
          </div>
```

Controleer dat `PlusIcon` een `size`-prop accepteert (`grep -n "export function PlusIcon" -A3 frontend/src/components/icons.tsx`); anders zonder prop gebruiken.

- [ ] **Step 4: Detailpaneel-tak** — in de keten vóór `) : selected ? (`:

```tsx
      ) : view === 'projects' && wpSelected !== undefined ? (
        <ErrorBoundary key={wpSelected?.site.id ?? 'nieuw'} label="WordPress-site error">
          <WPSitePage
            view={wpSelected}
            onSaved={v => { setWpSelected(v); laadWpSites() }}
            onDeleted={() => { setWpSelected(undefined); laadWpSites() }}
          />
        </ErrorBoundary>
```

- [ ] **Step 5: Typecheck**

Run: `cd frontend && npx tsc --noEmit`
Expected: geen fouten.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/WPSitePage.tsx frontend/src/App.tsx
git commit -m "feat: losse WordPress-sites in de zijbalk toevoegen en beheren

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 14: Instellingenblok "wp-fleet"

**Files:**
- Create: `frontend/src/components/WPFleetSettings.tsx`
- Modify: `frontend/src/components/SettingsPage.tsx` (render vóór `{/* App-updates */}`)

**Interfaces:**
- Consumes: `Services.WPFleetService.{Status, Register, Unregister, SaveSettings}`; model `FleetStatus` (Task 11)
- Produces: `<WPFleetSettings />`

- [ ] **Step 1: Maak `WPFleetSettings.tsx`**

```tsx
import { useEffect, useState } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { FleetStatus } from '../../bindings/github.com/rdm/sites-tool/internal/services/models'
import Foutvak from './Foutvak'

function foutTekst(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

export default function WPFleetSettings() {
  const [status, setStatus] = useState<FleetStatus | null>(null)
  const [image, setImage] = useState('')
  const [readOnly, setReadOnly] = useState(false)
  const [bezig, setBezig] = useState(false)
  const [fout, setFout] = useState<string | null>(null)
  const [opgeslagen, setOpgeslagen] = useState(false)

  const laad = () => {
    Services.WPFleetService.Status()
      .then(s => { setStatus(s); setImage(s.image); setReadOnly(s.readOnly) })
      .catch(e => setFout(foutTekst(e)))
  }
  useEffect(laad, [])

  const opslaan = async () => {
    setBezig(true); setFout(null); setOpgeslagen(false)
    try {
      await Services.WPFleetService.SaveSettings(image, readOnly)
      setOpgeslagen(true)
      laad()
    } catch (e) { setFout(foutTekst(e)) } finally { setBezig(false) }
  }

  const registreren = async () => {
    if (status && !status.stableLocation && !window.confirm(
      `Deze app draait niet vanuit /Applications (${status.executable}). ` +
      'Claude Code verwijst dan naar een pad dat kan verdwijnen. Toch registreren?')) return
    setBezig(true); setFout(null)
    try { setStatus(await Services.WPFleetService.Register()) }
    catch (e) { setFout(foutTekst(e)) } finally { setBezig(false) }
  }

  const afmelden = async () => {
    setBezig(true); setFout(null)
    try { await Services.WPFleetService.Unregister(); laad() }
    catch (e) { setFout(foutTekst(e)) } finally { setBezig(false) }
  }

  const registratieTekst = !status ? '…'
    : !status.registered ? 'niet geregistreerd'
    : status.pointsToThisApp ? '✓ geregistreerd, wijst naar deze app'
    : 'geregistreerd, maar niet via deze app (oude configuratie)'

  return (
    <section>
      <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">wp-fleet</h3>
      {fout && <Foutvak fout={fout} className="mb-2.5" />}
      <div className="bg-panel border border-border rounded-[11px] divide-y divide-border">
        <div className="flex items-center gap-4 px-4 py-3">
          <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Image</label>
          <input value={image} onChange={e => { setImage(e.target.value); setOpgeslagen(false) }}
            className="flex-1 bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg font-mono" />
        </div>
        <div className="flex items-center gap-4 px-4 py-3">
          <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Alleen lezen</label>
          <input type="checkbox" checked={readOnly} onChange={e => { setReadOnly(e.target.checked); setOpgeslagen(false) }} />
          <span className="text-[11.5px] text-fg-faint flex-1">wp-fleet registreert dan geen schrijf-tools</span>
          <button onClick={opslaan} disabled={bezig || !image}
            className="bg-accent text-white text-[12.5px] font-semibold px-4 py-1.5 rounded-lg hover:brightness-110 disabled:opacity-50 transition">
            Opslaan
          </button>
          {opgeslagen && <span className="text-[11.5px] text-green">✓</span>}
        </div>
        <div className="flex items-center gap-4 px-4 py-3">
          <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Sites</label>
          <span className="text-[12.5px] text-fg flex-1">
            {status ? `${status.sites} gekoppeld${status.sitesWithoutPassword ? ` · ${status.sitesWithoutPassword} zonder wachtwoord (worden overgeslagen)` : ''}` : '…'}
          </span>
        </div>
        <div className="flex items-center gap-4 px-4 py-3">
          <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Claude Code</label>
          <span className="text-[12.5px] text-fg flex-1">{registratieTekst}</span>
          <button onClick={registreren} disabled={bezig}
            className="bg-accent text-white text-[12.5px] font-semibold px-4 py-1.5 rounded-lg hover:brightness-110 disabled:opacity-50 transition">
            Registreer bij Claude Code
          </button>
          {status?.registered && (
            <button onClick={afmelden} disabled={bezig}
              className="border border-border text-fg-muted text-[12.5px] px-3 py-1.5 rounded-lg hover:bg-hover disabled:opacity-50 transition">
              Verwijder registratie
            </button>
          )}
        </div>
      </div>
    </section>
  )
}
```

- [ ] **Step 2: Render in `SettingsPage.tsx`**

Import `import WPFleetSettings from './WPFleetSettings'` en plaats `<WPFleetSettings />` direct vóór `{/* App-updates */}`.

- [ ] **Step 3: Typecheck**

Run: `cd frontend && npx tsc --noEmit`
Expected: geen fouten.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/WPFleetSettings.tsx frontend/src/components/SettingsPage.tsx
git commit -m "feat: instellingenblok wp-fleet met registratie bij Claude Code

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 15: End-to-end verificatie

**Files:** geen (tenzij er iets gerepareerd moet worden).

Deze stappen vragen de gebruiker: de dev-server draait in de hoofdcheckout en er mag maar één `task dev` tegelijk draaien.

- [ ] **Step 1: Volledige Go-tests en frontend-build in de worktree**

Run: `cd /Users/jeffreyt/Projects/RDM-Sites-tool-wp-fleet && go test ./internal/... && cd frontend && npm run build:dev`
Expected: alles `ok`, build zonder fouten.

- [ ] **Step 2: Vraag de gebruiker de dev-server in de hoofdcheckout te stoppen** (Ctrl-C in tab "rdm dev"), en start daarna in de worktree:

Run (in een terminaltab van de gebruiker): `cd /Users/jeffreyt/Projects/RDM-Sites-tool-wp-fleet && PATH="$HOME/go/bin:$PATH" task dev`

- [ ] **Step 3: Handmatig in de app**
  1. Open een project waarvan de checkout `public/wp-content/mu-plugins/nb-mcp-bridge.php` bevat → tab Projectinstellingen toont "WordPress MCP" met id/naam/URL voorgevuld.
  2. Vul het Application Password van `mcp-bot` in → Opslaan → "Test verbinding" geeft "verbinding en bridge in orde".
  3. Controleer: `cat ~/.config/rdm/wp-sites.yml` bevat de site met `project_path: ~/…` en **geen** wachtwoord; `security find-generic-password -s nl.nobears.kinsta-updater -a wp:<id> -w` geeft het wachtwoord.
  4. Zijbalk → "+" bij WordPress-sites → voeg de SatisPress-site toe → verschijnt in de lijst.

- [ ] **Step 4: Launcher los testen (zonder Claude)**

Run: `printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"probe","version":"0"}}}' | /Users/jeffreyt/Projects/RDM-Sites-tool-wp-fleet/bin/rdm-sites-tool.dev.app/Contents/MacOS/rdm-sites-tool mcp-launch`
Expected: op stdout één JSON-RPC-antwoord met `serverInfo`; op stderr eventueel "overgeslagen"-regels. Tijdens het draaien: `ps -ef | grep "docker run"` toont alleen `-e WP_…`-namen, geen wachtwoorden.

- [ ] **Step 5: Registratie en Claude**
  1. Instellingen → wp-fleet → "Registreer bij Claude Code" (bevestig de dev-pad-waarschuwing).
  2. Run: `claude mcp get wp-fleet` → Command wijst naar de app-binary, Args `mcp-launch`.
  3. In een nieuwe Claude Code-sessie: `list_sites` toont de gekoppelde sites; `site_check` op één site slaagt.
  4. Docker Desktop stoppen → nieuwe sessie → `claude mcp list` toont wp-fleet als mislukt; de melding "Docker draait niet — start Docker Desktop" staat in de MCP-log.

- [ ] **Step 6: Registratie terugzetten na de test**

Vraag de gebruiker of de registratie op de dev-build mag blijven staan. Zo niet: "Verwijder registratie", en na installatie van een release in `/Applications` opnieuw registreren.

- [ ] **Step 7: Rapporteer** de uitkomst per stap; open punt uit de spec (Keychain-lezen vanuit een door Claude gestart subproces zonder prompt) expliciet als bevestigd of niet.
