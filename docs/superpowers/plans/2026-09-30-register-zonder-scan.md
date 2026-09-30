# Zelfbeheerd register in plaats van mapscan — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De projectlijst komt uit een zelfbeheerd register (WordPress-sites uit `wp-sites.yml`, optioneel met lokale map, plus lokale projecten uit `local-projects.yml`) in plaats van een mapscan; functies zonder vereiste koppeling staan op slot in het menu; de Kinsta-koppeling wordt een site-veld en de Kinsta-tab verdwijnt.

**Architecture:** `ProjectService` houdt zijn API (`List/Get/RefreshOne/BatchStatus`) maar vult zijn lijst via `Reload()` uit het register. Items met een map houden hun bestaande ID (`sha256(pad)[:8]`), sites zonder map krijgen `site:<id>`. De Kinsta-koppeling van een site wordt bij `Reload` in `Project.Config.Kinsta.SiteID` gezet, zodat alle bestaande Kinsta-afnemers ongewijzigd werken. Services die een map nodig hebben gaan via één guard `requireLocal`. Een `ImportService` zet de oude scan eenmalig om.

**Tech Stack:** Go 1.25, Wails v3 alpha.80, `gopkg.in/yaml.v3`, standaard `testing`, React 18 + TypeScript + Tailwind v4.

**Spec:** `docs/superpowers/specs/2026-09-30-register-zonder-scan-design.md`

## Global Constraints

- Werk in `/Users/jeffreyt/Projects/RDM-Sites-tool-wp-fleet`, branch `feature/wp-fleet-sites`. Daar draait `task dev` voor de gebruiker: geen builds naar `bin/`, geen `task`-commando's behalve `task common:generate:bindings`; `go test ./internal/...` / `go vet ./internal/...` zijn veilig.
- Registerbestanden: `~/.config/rdm/wp-sites.yml` (bestaand) en `~/.config/rdm/local-projects.yml` (nieuw, `projects: [{path, name}]`), 0600, map 0700, atomisch, paden met `~`.
- Project-ID: item met map → `sha256(pad)[:8]` hex (ongewijzigd, `projectID(path)`); site zonder map → `site:<site-id>`.
- Een pad mag niet tegelijk een site-`project_path` en een lokaal project zijn; melding `map <pad> staat al in het register`.
- `WPSite` krijgt `KinstaSiteID` / `KinstaEnvID` (YAML `kinsta_site_id`/`kinsta_env_id`, JSON `kinstaSiteId`/`kinstaEnvId`, `omitempty`). Voorrang: site-veld boven `.rdm/config.yml`.
- Guard-melding (letterlijk): `koppel eerst een lokale map`.
- Menu-tooltips (letterlijk): `vereist een lokale map`, `vereist een Kinsta-koppeling`, `vereist een bridge-koppeling`.
- Zijbalklabels: `lokaal`, `online`, `map niet gevonden`; filterchip `alleen online`.
- Nieuwe globale instelling `clone_dir` (YAML), default `~/Projects`.
- `projects_roots` alleen nog gelezen door het importvenster.
- Bevestigingen via `bevestig()`; knoppen met `KnopTekst`; Nederlandse teksten; Go standaard `testing` + gofmt; frontend `npx tsc --noEmit && npm run build:dev`.
- Bindings na publieke Go-wijzigingen regenereren en meecommitten: `PATH="$HOME/go/bin:$PATH" task common:generate:bindings`.
- Commits: conventional, eindigen met `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

**Rulings (plan t.o.v. spec):**
- Kinsta-voorstel op URL (`SuggestSiteForURL`) is een heuristiek op de sitenaam: het Kinsta-sitemodel in de tool heeft geen domeinen. Genormaliseerde host (zonder `www.`, zonder TLD, zonder niet-alfanumerieke tekens) wordt vergeleken met genormaliseerde `Name` en `DisplayName`; bij precies één treffer is dat het voorstel. Kost bij fout: geen of een verkeerd voorstel, de gebruiker kiest zelf.
- De Kinsta-omgeving (`kinsta_env_id`) wordt in het formulier alleen gekozen als de site meer dan één omgeving heeft; anders leeg (de bestaande code kiest dan `live`). Kost bij fout: één extra keuze.
- `WPSitePage` blijft alleen als formulier voor "WordPress-site toevoegen"; een bestaande site zonder map opent voortaan `ProjectDetail` (ID `site:<id>`).

---

## Bestandsoverzicht

| Bestand | Status | Verantwoordelijkheid |
|---|---|---|
| `internal/domain/wpsite.go` | wijzigen | Kinsta-velden |
| `internal/domain/localproject.go` | nieuw | `LocalProject` |
| `internal/domain/project.go` | wijzigen | `SiteID`, `Local`, `Missing` op `Project` |
| `internal/domain/git.go` | wijzigen | `Local`, `SiteID`, `Missing` op `ProjectStatusSummary` |
| `internal/config/localprojects.go` (+test) | nieuw | `local-projects.yml` + register-validatie |
| `internal/config/schema.go`, `global.go` | wijzigen | `CloneDir` |
| `internal/services/project_service.go` (+test) | wijzigen | `Reload` uit register, weg: scan/roots |
| `internal/services/local_guard.go` (+test) | nieuw | `requireLocal`, `localProjects` |
| diverse services | wijzigen | guard + filter |
| `internal/services/register_service.go` (+test) | nieuw | lokale projecten + map koppelen |
| `internal/services/wpsite_service.go` | wijzigen | `ForProject` via `SiteID`, reload na mutatie |
| `internal/services/kinsta_suggest.go` (+test) | nieuw | `SuggestSiteForURL` |
| `internal/services/org_sync_clone.go` (+test) | wijzigen | `clone_dir` + registreren |
| `internal/services/import_service.go` (+test) | nieuw | import van de oude scan |
| `internal/app/app.go` | wijzigen | wiring |
| `frontend/src/App.tsx` | wijzigen | zijbalk, +-menu, importvenster |
| `frontend/src/components/ImportDialog.tsx` | nieuw | importvenster |
| `frontend/src/components/ProjectDetail.tsx` | wijzigen | menu-gating, Kinsta-tab weg |
| `frontend/src/components/ProjectSettingsTab.tsx` | wijzigen | blok Lokale map, site-formulier voor site-only |
| `frontend/src/components/WPSiteForm.tsx` | wijzigen | veld Kinsta-site |
| `frontend/src/components/SettingsPage.tsx` | wijzigen | `clone_dir` |

---

### Task 1: Datamodel en `local-projects.yml`

**Files:**
- Modify: `internal/domain/wpsite.go`, `internal/domain/project.go` (`Project`), `internal/domain/git.go` (`ProjectStatusSummary`)
- Create: `internal/domain/localproject.go`
- Create: `internal/config/localprojects.go`, Test: `internal/config/localprojects_test.go`
- Modify: `internal/config/schema.go`, `internal/config/global.go`

**Interfaces:**
- Produces:
  - `WPSite.KinstaSiteID`, `WPSite.KinstaEnvID` (tags zie Global Constraints)
  - `type LocalProject struct { Path string \`yaml:"path" json:"path"\`; Name string \`yaml:"name,omitempty" json:"name"\` }`
  - `Project` + `SiteID string \`json:"siteId"\``, `Local bool \`json:"local"\``, `Missing bool \`json:"missing"\``
  - `ProjectStatusSummary` + `SiteID string \`json:"siteId"\``, `Local bool \`json:"local"\``, `Missing bool \`json:"missing"\``
  - `func LocalProjectsPath() string`, `func LoadLocalProjects(path string) ([]domain.LocalProject, error)` (ontbrekend → leeg; paden geëxpandeerd), `func SaveLocalProjects(path string, ps []domain.LocalProject) error`
  - `func ValidateRegister(sites []domain.WPSite, locals []domain.LocalProject) error` — pad-botsing over beide lijsten + dubbel pad binnen `locals`
  - `Global.CloneDir string \`yaml:"clone_dir"\``, default `~/Projects` in `defaultGlobal` en `applyDefaults` (als leeg)

- [ ] **Step 1: Falende tests** — `internal/config/localprojects_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestLocalProjectsRoundtrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := LocalProjectsPath()
	in := []domain.LocalProject{{Path: filepath.Join(home, "Projects", "web-laravel"), Name: "Laravel"}}
	if err := SaveLocalProjects(path, in); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "path: ~/Projects/web-laravel") {
		t.Fatalf("pad niet met ~:\n%s", raw)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("rechten %v", info.Mode().Perm())
	}
	got, err := LoadLocalProjects(path)
	if err != nil || len(got) != 1 || got[0].Path != in[0].Path || got[0].Name != "Laravel" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestLocalProjectsOntbrekend(t *testing.T) {
	got, err := LoadLocalProjects(filepath.Join(t.TempDir(), "nee.yml"))
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestLocalProjectsWeigertLeegOfDubbelPad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "l.yml")
	if err := SaveLocalProjects(p, []domain.LocalProject{{Path: ""}}); err == nil {
		t.Fatal("leeg pad moet geweigerd worden")
	}
	if err := SaveLocalProjects(p, []domain.LocalProject{{Path: "/a"}, {Path: "/a"}}); err == nil {
		t.Fatal("dubbel pad moet geweigerd worden")
	}
}

func TestValidateRegisterPadBotsing(t *testing.T) {
	sites := []domain.WPSite{{ID: "aa", Name: "a", URL: "https://a.nl", Username: "u", ProjectPath: "/p"}}
	err := ValidateRegister(sites, []domain.LocalProject{{Path: "/p"}})
	if err == nil || !strings.Contains(err.Error(), "map /p staat al in het register") {
		t.Fatalf("fout %v", err)
	}
	if err := ValidateRegister(sites, []domain.LocalProject{{Path: "/q"}}); err != nil {
		t.Fatal(err)
	}
}

func TestCloneDirDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	g, _ := LoadGlobal()
	if g.CloneDir != "~/Projects" {
		t.Fatalf("clone_dir %q", g.CloneDir)
	}
}
```

- [ ] **Step 2: Run, verwacht FAIL** — `go test ./internal/config/ -run 'LocalProjects|ValidateRegister|CloneDir' -v` → `undefined: LocalProjectsPath`.

- [ ] **Step 3: Implementeer**

`internal/domain/localproject.go`:

```go
package domain

// LocalProject is een lokale map zonder WordPress-koppeling (Laravel, Pimcore,
// overig) die de gebruiker zelf aan de tool heeft toegevoegd.
type LocalProject struct {
	Path string `yaml:"path" json:"path"`
	Name string `yaml:"name,omitempty" json:"name"`
}
```

In `WPSite` (na `Depot`):

```go
	// KinstaSiteID/KinstaEnvID koppelen de site aan Kinsta; ze gaan voor op
	// een koppeling in .rdm/config.yml en werken ook zonder lokale map.
	KinstaSiteID string `json:"kinstaSiteId,omitempty" yaml:"kinsta_site_id,omitempty"`
	KinstaEnvID  string `json:"kinstaEnvId,omitempty" yaml:"kinsta_env_id,omitempty"`
```

`Project` en `ProjectStatusSummary`: velden uit de Interfaces toevoegen (met Nederlands commentaar: `SiteID` = id in wp-sites.yml, leeg voor lokaal project; `Local` = heeft een lokale map; `Missing` = map staat in het register maar bestaat niet).

`internal/config/localprojects.go`:

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/rdm/sites-tool/internal/domain"
)

type localProjectsFile struct {
	Projects []domain.LocalProject `yaml:"projects"`
}

// LocalProjectsPath is het per-gebruiker bestand met lokale projecten die geen
// WordPress-site zijn.
func LocalProjectsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rdm", "local-projects.yml")
}

func LoadLocalProjects(path string) ([]domain.LocalProject, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []domain.LocalProject{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lees %s: %w", path, err)
	}
	var f localProjectsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for i := range f.Projects {
		f.Projects[i].Path = ExpandHome(f.Projects[i].Path)
	}
	if err := validateLocalProjects(f.Projects); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if f.Projects == nil {
		f.Projects = []domain.LocalProject{}
	}
	return f.Projects, nil
}

func SaveLocalProjects(path string, ps []domain.LocalProject) error {
	if err := validateLocalProjects(ps); err != nil {
		return fmt.Errorf("local-projects: %w", err)
	}
	out := make([]domain.LocalProject, len(ps))
	for i, p := range ps {
		p.Path = CompressHome(p.Path)
		out[i] = p
	}
	data, err := yaml.Marshal(localProjectsFile{Projects: out})
	if err != nil {
		return fmt.Errorf("local-projects: %w", err)
	}
	return atomischSchrijven(path, data)
}

func validateLocalProjects(ps []domain.LocalProject) error {
	gezien := map[string]bool{}
	for i, p := range ps {
		if p.Path == "" {
			return fmt.Errorf("project %d: pad is verplicht", i+1)
		}
		key := filepath.Clean(p.Path)
		if gezien[key] {
			return fmt.Errorf("map %s staat al in het register", p.Path)
		}
		gezien[key] = true
	}
	return nil
}

// ValidateRegister controleert dat geen map tegelijk aan een site en aan een
// lokaal project hangt.
func ValidateRegister(sites []domain.WPSite, locals []domain.LocalProject) error {
	paden := map[string]bool{}
	for _, s := range sites {
		if s.ProjectPath != "" {
			paden[filepath.Clean(s.ProjectPath)] = true
		}
	}
	for _, p := range locals {
		if paden[filepath.Clean(p.Path)] {
			return fmt.Errorf("map %s staat al in het register", p.Path)
		}
	}
	return validateLocalProjects(locals)
}
```

Refactor het tijdelijk-bestand-en-hernoemen uit `SaveWPSites` naar een gedeelde `func atomischSchrijven(path string, data []byte) error` in `wpsites.go` (MkdirAll 0700, CreateTemp in dezelfde map, Chmod 0600, Write, Close, Rename) en laat `SaveWPSites` hem gebruiken. De bestaande WPSites-tests moeten blijven slagen.

`schema.go`: `CloneDir string \`yaml:"clone_dir"\`` in `Global` (met commentaar "map waarin Org-sync nieuwe repo's clonet"). `global.go`: `CloneDir: "~/Projects"` in `defaultGlobal`, en in `applyDefaults` `if g.CloneDir == "" { g.CloneDir = "~/Projects" }`.

- [ ] **Step 4: Run, verwacht PASS** — `go test ./internal/config/ ./internal/domain/ -v` (alle config- en domain-tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/domain internal/config
git add internal/domain internal/config
git commit -m "feat: register-datamodel met lokale projecten en Kinsta-velden op de site

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: `ProjectService` uit het register

**Files:**
- Modify: `internal/services/project_service.go`
- Test: `internal/services/project_service_register_test.go`
- Modify: callers of `NewProjectService`, `Scan`, `GetRoots`, `AddRoot`, `RemoveRoot` (`internal/app/app.go`; `internal/services/org_sync_clone.go` — alleen compileerbaar houden, Task 5 herschrijft `cloneRoot`; tests die `NewProjectService([]string{...})` gebruiken)

**Interfaces:**
- Consumes: Task 1 (`LoadWPSites`, `LoadLocalProjects`, `WPSitesPath`, `LocalProjectsPath`, nieuwe velden).
- Produces:
  - `func NewProjectService(sitesPath, localsPath string) *ProjectService`
  - `func (s *ProjectService) Reload() ([]domain.Project, error)` — vervangt `Scan`
  - `List`, `Get`, `UpdateProjectConfig`, `RefreshOne`, `BatchStatus` blijven (zelfde signatuur)
  - `AddRoot`, `RemoveRoot`, `GetRoots`, `Scan` verdwijnen
  - `func SiteProjectID(siteID string) string` → `"site:" + siteID`

- [ ] **Step 1: Falende tests** — `project_service_register_test.go`:

```go
package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

func registerFixture(t *testing.T) (*ProjectService, string, string, string, string) {
	t.Helper()
	dir := t.TempDir()
	wpDir := filepath.Join(dir, "web-a")
	laravelDir := filepath.Join(dir, "web-laravel")
	os.MkdirAll(wpDir, 0o755)
	os.MkdirAll(laravelDir, 0o755)
	os.WriteFile(filepath.Join(wpDir, "deploy_conf.json"), []byte(`{"type":"wordpress_kinsta","link":{"prod":"https://a.nl"}}`), 0o644)
	sites := filepath.Join(dir, "wp-sites.yml")
	locals := filepath.Join(dir, "local-projects.yml")
	if err := config.SaveWPSites(sites, []domain.WPSite{
		{ID: "site-a", Name: "Site A", URL: "https://a.nl", Username: "u", ProjectPath: wpDir, KinstaSiteID: "k-123"},
		{ID: "depot", Name: "Depot", URL: "https://depot.nl", Username: "u", KinstaSiteID: "k-9"},
		{ID: "weg", Name: "Weg", URL: "https://weg.nl", Username: "u", ProjectPath: filepath.Join(dir, "bestaat-niet")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveLocalProjects(locals, []domain.LocalProject{{Path: laravelDir, Name: "Laravel"}}); err != nil {
		t.Fatal(err)
	}
	return NewProjectService(sites, locals), wpDir, laravelDir, sites, locals
}

func TestReloadBouwtDrieSoorten(t *testing.T) {
	svc, wpDir, laravelDir, _, _ := registerFixture(t)
	ps, err := svc.Reload()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]domain.Project{}
	for _, p := range ps {
		byID[p.ID] = p
	}
	a := byID[projectID(wpDir)]
	if !a.Local || a.Missing || a.SiteID != "site-a" || a.Path != wpDir || a.DisplayName != "Site A" {
		t.Fatalf("site met map: %+v", a)
	}
	if a.Config.Kinsta == nil || a.Config.Kinsta.SiteID != "k-123" {
		t.Fatalf("kinsta-koppeling van de site ontbreekt: %+v", a.Config.Kinsta)
	}
	d := byID["site:depot"]
	if d.Local || d.Path != "" || d.SiteID != "depot" || d.Deploy.Link.Prod != "https://depot.nl" || d.Config.Kinsta == nil || d.Config.Kinsta.SiteID != "k-9" {
		t.Fatalf("site zonder map: %+v", d)
	}
	if d.Provider != domain.ProviderKinsta {
		t.Fatalf("provider %q", d.Provider)
	}
	l := byID[projectID(laravelDir)]
	if !l.Local || l.SiteID != "" || l.DisplayName != "Laravel" {
		t.Fatalf("lokaal project: %+v", l)
	}
	var weg domain.Project
	for _, p := range ps {
		if p.SiteID == "weg" {
			weg = p
		}
	}
	if !weg.Local || !weg.Missing {
		t.Fatalf("verdwenen map: %+v", weg)
	}
	if len(ps) != 4 {
		t.Fatalf("aantal %d", len(ps))
	}
}

func TestReloadKinstaSiteGaatVoorRdm(t *testing.T) {
	svc, wpDir, _, _, _ := registerFixture(t)
	os.MkdirAll(filepath.Join(wpDir, ".rdm"), 0o755)
	os.WriteFile(filepath.Join(wpDir, ".rdm", "config.yml"), []byte("kinsta:\n  site_id: oud\n"), 0o644)
	ps, _ := svc.Reload()
	for _, p := range ps {
		if p.SiteID == "site-a" && p.Config.Kinsta.SiteID != "k-123" {
			t.Fatalf("site-veld moet voorgaan: %q", p.Config.Kinsta.SiteID)
		}
	}
}

func TestBatchStatusMarkeertLocal(t *testing.T) {
	svc, _, _, _, _ := registerFixture(t)
	svc.Reload()
	for _, s := range svc.BatchStatus() {
		if s.ProjectID == "site:depot" && (s.Local || s.SiteID != "depot") {
			t.Fatalf("depot-summary %+v", s)
		}
	}
}

func TestSiteProjectID(t *testing.T) {
	if SiteProjectID("x") != "site:x" {
		t.Fatal()
	}
}
```

- [ ] **Step 2: Run, verwacht FAIL** — `go test ./internal/services/ -run 'Reload|BatchStatusMarkeert|SiteProjectID' -v` (compileert niet: `NewProjectService` signatuur).

- [ ] **Step 3: Implementeer**

Vervang in `project_service.go` de velden `roots` door `sitesPath, localsPath string`, verwijder `GetRoots`, `AddRoot`, `RemoveRoot`, `persistRoots`, `Scan`, en voeg toe:

```go
func NewProjectService(sitesPath, localsPath string) *ProjectService {
	return &ProjectService{sitesPath: sitesPath, localsPath: localsPath}
}

// SiteProjectID is het project-ID van een WordPress-site zonder lokale map.
func SiteProjectID(siteID string) string { return "site:" + siteID }

// Reload bouwt de projectlijst uit het register: sites (met of zonder map) en
// lokale projecten. Items met een map houden het ID op basis van het pad, zodat
// opgeslagen historie (testruns, rapporten, mediascans) blijft aansluiten.
func (s *ProjectService) Reload() ([]domain.Project, error) {
	sites, err := config.LoadWPSites(s.sitesPath)
	if err != nil {
		return nil, err
	}
	locals, err := config.LoadLocalProjects(s.localsPath)
	if err != nil {
		return nil, err
	}
	found := make([]domain.Project, 0, len(sites)+len(locals))
	for _, site := range sites {
		found = append(found, s.projectForSite(site))
	}
	for _, lp := range locals {
		p, _ := s.loadProject(lp.Path)
		if lp.Name != "" {
			p.DisplayName = lp.Name
		}
		p.Local = true
		p.Missing = !dirExists(lp.Path)
		found = append(found, p)
	}
	s.mu.Lock()
	s.projects = found
	s.mu.Unlock()
	return found, nil
}

func (s *ProjectService) projectForSite(site domain.WPSite) domain.Project {
	var p domain.Project
	if site.ProjectPath != "" {
		p, _ = s.loadProject(site.ProjectPath)
		p.Local = true
		p.Missing = !dirExists(site.ProjectPath)
	} else {
		p = domain.Project{
			ID:         SiteProjectID(site.ID),
			Provider:   domain.ProviderNone,
			Deploy:     domain.DeployConf{Type: "wordpress", Link: domain.DeployLinks{Prod: site.URL}},
			LastScanAt: time.Now(),
		}
	}
	p.SiteID = site.ID
	p.DisplayName = site.Name
	if p.Deploy.Link.Prod == "" {
		p.Deploy.Link.Prod = site.URL
	}
	if site.KinstaSiteID != "" {
		if p.Config.Kinsta == nil {
			p.Config.Kinsta = &domain.KinstaProjectCfg{}
		}
		p.Config.Kinsta.SiteID = site.KinstaSiteID
		p.Provider = domain.ProviderKinsta
	}
	return p
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
```

Houd `loadProject` zoals het is (het leest `.rdm`, `deploy_conf.json`, git-status; bij een niet-bestaande map geeft `gitread.Status` een lege status). Let op: `loadProject` kopieert `cfg` uit `.rdm`; mutatie van `p.Config.Kinsta` in `projectForSite` moet op een eigen kopie gebeuren — maak in `projectForSite` eerst `k := domain.KinstaProjectCfg{}; if p.Config.Kinsta != nil { k = *p.Config.Kinsta }; k.SiteID = site.KinstaSiteID; p.Config.Kinsta = &k`.

`RefreshOne`: sla projecten met `!Local || Missing` over voor git-status (retourneer ze ongewijzigd). `BatchStatus`: vul `SiteID`, `Local`, `Missing`; roep `gitread.Status` alleen aan als `Local && !Missing`.

Callers:
- `internal/app/app.go`: `project := services.NewProjectService(config.WPSitesPath(), config.LocalProjectsPath())` en direct daarna `_, _ = project.Reload()` (fout loggen via `log.Printf`, niet fataal).
- `org_sync_clone.go`: vervang `s.projects.Scan()` door `s.projects.Reload()`; `cloneRoot` gebruikt tijdelijk `expandHome("~/Projects")` (Task 5 maakt dit `clone_dir`).
- Bestaande tests die `NewProjectService([]string{...})` + `Scan()` gebruiken (`grep -rn "NewProjectService\|\.Scan()" internal`): zet ze om naar een register-fixture of vul `s.projects` direct via een test-helper `newProjectServiceWith(ps []domain.Project) *ProjectService` in een `_test.go`. Pas geen testbedoeling aan.

- [ ] **Step 4: Run, verwacht PASS** — `go test ./internal/... 2>&1 | grep -E "^(FAIL|---)"` leeg; `go vet ./internal/...`.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/services internal/app
git add internal/services internal/app
git commit -m "feat: projectlijst uit het register in plaats van een mapscan

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Guard voor functies die een lokale map nodig hebben

**Files:**
- Create: `internal/services/local_guard.go`, Test: `internal/services/local_guard_test.go`
- Modify: de `pathFor`/project-opzoekplekken en de lussen over alle projecten in: `git_service.go`, `security_service.go`, `make_service.go`, `search_service.go`, `batch_service.go`, `test_service.go`, `log_fix_service.go`, `media_service.go`, `migration_service.go`, `db_clone_service.go`, `db_editor_service.go`, `report_service.go`, `plugin_local.go`, `inventory_service.go` (+ `inventory_sync.go`), `wpcore_update_service.go`, `bulk_update_service.go`, `wordfence_service.go`, `wordfence_update_service.go`, `org_sync_service.go`, `editor_service.go`

**Interfaces:**
- Produces:
  - `var errGeenLokaleMap = errors.New("koppel eerst een lokale map")`
  - `func requireLocal(p domain.Project) (string, error)` — `!p.Local || p.Path == ""` → `errGeenLokaleMap`; `p.Missing` → `fmt.Errorf("map %s niet gevonden", p.Path)`; anders `p.Path`
  - `func localProjects(ps []domain.Project) []domain.Project` — alleen `Local && !Missing`

- [ ] **Step 1: Falende tests**

```go
package services

import (
	"errors"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestRequireLocal(t *testing.T) {
	if _, err := requireLocal(domain.Project{ID: "site:x"}); !errors.Is(err, errGeenLokaleMap) {
		t.Fatalf("site zonder map: %v", err)
	}
	if _, err := requireLocal(domain.Project{Local: true, Missing: true, Path: "/weg"}); err == nil || err.Error() != "map /weg niet gevonden" {
		t.Fatalf("missing: %v", err)
	}
	if p, err := requireLocal(domain.Project{Local: true, Path: "/ok"}); err != nil || p != "/ok" {
		t.Fatalf("ok: %q %v", p, err)
	}
}

func TestLocalProjectsFilter(t *testing.T) {
	in := []domain.Project{{ID: "a", Local: true, Path: "/a"}, {ID: "site:b"}, {ID: "c", Local: true, Missing: true, Path: "/c"}}
	got := localProjects(in)
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("got %+v", got)
	}
}

func TestGitServiceZonderMap(t *testing.T) {
	ps := newProjectServiceWith([]domain.Project{{ID: "site:x", SiteID: "x"}})
	g := NewGitService(ps)
	if _, err := g.GetStatus("site:x"); err == nil || err.Error() != "koppel eerst een lokale map" {
		t.Fatalf("fout %v", err)
	}
}
```

(`newProjectServiceWith` is de test-helper uit Task 2; staat hij nog niet in een `_test.go`, voeg hem daar toe: `func newProjectServiceWith(ps []domain.Project) *ProjectService { s := &ProjectService{}; s.projects = ps; return s }`.)

Voeg per aangepaste service met een bestaand testbestand één test toe die een `site:`-project gebruikt en de guardfout verwacht, voor minstens: `SearchService` (lus slaat site-only over), `InventoryService` (site-only komt niet in het overzicht), `BulkUpdateService` (site-only geen kandidaat), `MigrationService`, `DBCloneService`. Gebruik de bestaande fakes/fixtures van die testbestanden.

- [ ] **Step 2: Run, verwacht FAIL** — `go test ./internal/services/ -run 'RequireLocal|LocalProjectsFilter|ZonderMap' -v`.

- [ ] **Step 3: Implementeer** `local_guard.go` volgens de Interfaces (Nederlands commentaar: "één plek die zegt of een functie een lokale checkout heeft"). Pas daarna in elke genoemde service de plek aan waar een project-ID naar een pad wordt omgezet: vervang `p.Path` door `path, err := requireLocal(p); if err != nil { return …, err }`. Vervang lussen over `s.projects.List()` / `projects.List()` die een checkout nodig hebben door `localProjects(...)`. Services die alleen de Kinsta-API gebruiken (`kinsta_service.go`, `log_service.go`, `vuln_scan_service.go`) blijven ongemoeid. `report_service.go`: alleen de delen die de repo lezen krijgen de guard.

Werk bestand voor bestand; draai na elk bestand `go build ./internal/... && go test ./internal/services/ -run '<service>' `.

- [ ] **Step 4: Run, verwacht PASS** — `go test ./internal/... 2>&1 | grep -E "^(FAIL|---)"` leeg; `go vet ./internal/...`.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/services
git add internal/services
git commit -m "feat: functies die een lokale map nodig hebben weigeren netjes zonder map

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Register beheren — lokale projecten, map koppelen, sites herladen

**Files:**
- Create: `internal/services/register_service.go`, Test: `internal/services/register_service_test.go`
- Modify: `internal/services/wpsite_service.go` (`WPProjectLookup` + `Reload`, `ForProject` via `SiteID`, reload na mutaties, `SuggestForProject` voor lokaal project), test-fake `wpNepProjecten`

**Interfaces:**
- Consumes: Task 1–3.
- Produces:
  - `type RegisterService struct` met `func NewRegisterService(projects *ProjectService, sites *WPSiteService, localsPath string) *RegisterService`, `SetApp(app *application.App)`
  - `func (s *RegisterService) AddLocalProject() (*domain.Project, error)` — map-dialoog; annuleren → `nil, nil`
  - `func (s *RegisterService) AddLocalProjectPath(path, name string) (domain.Project, error)`
  - `func (s *RegisterService) RemoveLocalProject(projectID string) error` — alleen lokale projecten (geen site); verwijdert uit `local-projects.yml`, raakt de map niet
  - `func (s *RegisterService) ChooseFolder() (string, error)` — alleen de dialoog, pad terug (leeg bij annuleren)
  - `func (s *RegisterService) LinkSiteFolder(siteID, path string) error` — zet `WPSite.ProjectPath` (via `WPSiteService.Update` met behoud van wachtwoord), valideert met `ValidateRegister` tegen `local-projects.yml`; staat hetzelfde pad als lokaal project geregistreerd → dat lokale project wordt verwijderd (de map "wordt" de site)
  - `func (s *RegisterService) ConvertLocalToSite(projectID string) (domain.WPSite, error)` — voorstel voor het siteformulier (via `WPSiteService.SuggestForProject`)
  - `WPProjectLookup` krijgt `Reload() ([]domain.Project, error)`; `WPSiteService` roept na `Create/Update/Unlink/Delete` en `TestConnection`-mutatie `s.projects.Reload()` aan (fout negeren, loggen)
  - `WPSiteService.ForProject(projectID)`: als het project een `SiteID` heeft → die site; anders bestaande pad-match

- [ ] **Step 1: Falende tests** — `internal/services/register_service_test.go`. `registerFixture` uit Task 2 geeft ook de paden van beide registerbestanden terug.

```go
package services

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/config"
)

func regFixture(t *testing.T) (*RegisterService, *ProjectService, *WPSiteService, string, string, string) {
	t.Helper()
	ps, wpDir, laravelDir, sitesPath, localsPath := registerFixture(t)
	if _, err := ps.Reload(); err != nil {
		t.Fatal(err)
	}
	wp := NewWPSiteService(sitesPath, ps, &wpNepSecrets{m: map[string]string{}})
	return NewRegisterService(ps, wp, localsPath), ps, wp, wpDir, laravelDir, localsPath
}

func TestAddLocalProjectPathEnReload(t *testing.T) {
	rs, ps, _, _, _, _ := regFixture(t)
	nieuw := t.TempDir()
	p, err := rs.AddLocalProjectPath(nieuw, "Pimcore")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != projectID(nieuw) || !p.Local || p.DisplayName != "Pimcore" {
		t.Fatalf("project %+v", p)
	}
	if _, ok := ps.Get(projectID(nieuw)); !ok {
		t.Fatal("niet herladen")
	}
}

func TestAddLocalProjectPathWeigertSitePad(t *testing.T) {
	rs, _, _, wpDir, _, _ := regFixture(t)
	_, err := rs.AddLocalProjectPath(wpDir, "")
	if err == nil || !strings.Contains(err.Error(), "staat al in het register") {
		t.Fatalf("fout %v", err)
	}
}

func TestRemoveLocalProjectWeigertSite(t *testing.T) {
	rs, _, _, wpDir, _, _ := regFixture(t)
	err := rs.RemoveLocalProject(projectID(wpDir))
	if err == nil || err.Error() != "dit is een WordPress-site; ontkoppel of verwijder de site" {
		t.Fatalf("fout %v", err)
	}
}

func TestRemoveLocalProject(t *testing.T) {
	rs, ps, _, _, laravelDir, localsPath := regFixture(t)
	if err := rs.RemoveLocalProject(projectID(laravelDir)); err != nil {
		t.Fatal(err)
	}
	locals, _ := config.LoadLocalProjects(localsPath)
	if len(locals) != 0 {
		t.Fatalf("nog %+v", locals)
	}
	if _, ok := ps.Get(projectID(laravelDir)); ok {
		t.Fatal("project staat nog in de lijst")
	}
}

func TestLinkSiteFolderNeemtLokaalProjectOver(t *testing.T) {
	rs, ps, wp, _, laravelDir, localsPath := regFixture(t)
	if err := rs.LinkSiteFolder("depot", laravelDir); err != nil {
		t.Fatal(err)
	}
	site, _ := wp.find("depot")
	if filepath.Clean(site.ProjectPath) != filepath.Clean(laravelDir) {
		t.Fatalf("project_path %q", site.ProjectPath)
	}
	locals, _ := config.LoadLocalProjects(localsPath)
	if len(locals) != 0 {
		t.Fatalf("lokaal project niet overgenomen: %+v", locals)
	}
	p, ok := ps.Get(projectID(laravelDir))
	if !ok || p.SiteID != "depot" {
		t.Fatalf("project %+v ok=%v", p, ok)
	}
	if _, ok := ps.Get("site:depot"); ok {
		t.Fatal("site:depot hoort niet meer te bestaan")
	}
}

func TestForProjectViaSiteID(t *testing.T) {
	_, _, wp, _, _, _ := regFixture(t)
	v, err := wp.ForProject("site:depot")
	if err != nil || v == nil || v.Site.ID != "depot" {
		t.Fatalf("v %+v err %v", v, err)
	}
}
```

Werk `wpNepProjecten` bij met `func (n wpNepProjecten) Reload() ([]domain.Project, error) { return n.List(), nil }`.

- [ ] **Step 2: Run, verwacht FAIL.**

- [ ] **Step 3: Implementeer** volgens de Interfaces. `RemoveLocalProject` op een site-ID of een project met `SiteID != ""` → fout `dit is een WordPress-site; ontkoppel of verwijder de site`. Map-dialoog zoals de oude `AddRoot` (`s.app.Dialog.OpenFile().SetTitle(...).CanChooseFiles(false).CanChooseDirectories(true).PromptForSingleSelection()`), titel `Kies de projectmap`. Na elke mutatie `s.projects.Reload()`.

- [ ] **Step 4: Run, verwacht PASS** — `go test ./internal/services/ -v -run 'Register|LocalProject|LinkSiteFolder|ForProject|WPSite'` en de volledige `./internal/...`.

- [ ] **Step 5: Commit** — `feat: lokale projecten en mapkoppeling beheren in het register`.

---

### Task 5: Kinsta-voorstel op URL en Org-sync naar `clone_dir`

**Files:**
- Create: `internal/services/kinsta_suggest.go`, Test: `internal/services/kinsta_suggest_test.go`
- Modify: `internal/services/org_sync_clone.go` (+ bestaande test), `internal/services/org_sync_service.go` (constructor/velden)

**Interfaces:**
- Produces:
  - `type KinstaSuggestion struct { SiteID string \`json:"siteId"\`; Name string \`json:"name"\`; Found bool \`json:"found"\` }`
  - `func (s *KinstaService) SuggestSiteForURL(url string) (KinstaSuggestion, error)` — gebruikt `ListSites`; pure helper `suggestKinstaSite(sites []kinsta.Site, url string) KinstaSuggestion` (heuristiek uit de plan-ruling)
  - Org-sync: `cloneRoot` gebruikt `cfg.CloneDir` (ge-expand, moet bestaan → anders fout `kloonmap <pad> is niet te openen`); na elke geslaagde clone `register.AddLocalProjectPath(pad, naam)`, tenzij een site met `deploy_conf.json` link.prod gelijk aan de site-URL bestaat → dan `LinkSiteFolder(siteID, pad)`. `OrgSyncService` krijgt daarvoor een veld van een interface `orgRegister interface { AddLocalProjectPath(path, name string) (domain.Project, error); LinkSiteFolder(siteID, path string) error }` en de sitelijst via `config.LoadWPSites(config.WPSitesPath())` (of een geïnjecteerde functie voor tests).

- [ ] **Step 1: Falende tests**

```go
func TestSuggestKinstaSite(t *testing.T) {
	sites := []kinsta.Site{{ID: "1", Name: "ekcoating", DisplayName: "EK Coating"}, {ID: "2", Name: "satispress", DisplayName: "SatisPress"}}
	for url, want := range map[string]string{
		"https://www.ekcoating.nl":        "1",
		"https://satispress.nobears.dev/": "2",
		"https://onbekend.nl":             "",
	} {
		got := suggestKinstaSite(sites, url)
		if got.SiteID != want || got.Found != (want != "") {
			t.Errorf("%s: %+v, want %q", url, got, want)
		}
	}
	// Twee treffers → geen voorstel.
	dubbel := append(sites, kinsta.Site{ID: "3", Name: "ek-coating"})
	if got := suggestKinstaSite(dubbel, "https://ekcoating.nl"); got.Found {
		t.Fatalf("dubbelzinnig: %+v", got)
	}
}
```

Normalisatie: host zonder poort, zonder `www.`, eerste label (vóór de eerste `.`) — `satispress.nobears.dev` → `satispress`; namen lowercased zonder niet-alfanumerieke tekens (`ek-coating` → `ekcoating`). Match als genormaliseerde `Name` of `DisplayName` gelijk is aan het genormaliseerde label.

Org-sync: pas de bestaande clone-test aan zodat `cloneRoot` uit `CloneDir` komt en een fake `orgRegister` registreert welke paden zijn toegevoegd; test ook de link-variant (fake sitelijst met matching URL).

- [ ] **Step 2–4:** RED → implementeer → GREEN (`go test ./internal/services/ -run 'SuggestKinsta|OrgSync|Clone' -v`, daarna volledige suite).
- [ ] **Step 5: Commit** — `feat: Kinsta-voorstel op URL en org-sync kloont naar de kloonmap`.

---

### Task 6: Importvenster (backend)

**Files:**
- Create: `internal/services/import_service.go`, Test: `internal/services/import_service_test.go`

**Interfaces:**
- Produces:
  - `type ImportCandidate struct { Path, Name, DeployType, URL, KinstaSiteID string; WordPress bool }` (JSON camelCase)
  - `type ImportChoice struct { Path string; AsSite bool }`
  - `func NewImportService(cfg *config.Global, sites *WPSiteService, register *RegisterService, sitesPath, localsPath string) *ImportService`
  - `func (s *ImportService) Needed() (bool, error)` — beide registerbestanden leeg én `cfg.ProjectsRoots` bevat minstens één bestaande map én vlag `import_done` in `config.yml` niet gezet
  - `func (s *ImportService) Candidates() ([]ImportCandidate, error)` — oude scan: elke submap van elke root; `WordPress = strings.HasPrefix(deploy.Type, "wordpress")`; `URL = deploy.Link.Prod`; `KinstaSiteID` uit `.rdm/config.yml`; `Name` uit `.rdm` display_name of mapnaam; gesorteerd op naam
  - `func (s *ImportService) Apply(choices []ImportChoice) (int, error)` — `AsSite` → `WPSiteService.CreateWithoutPassword(site)` (nieuw, zie hieronder) met `ID = SuggestWPSiteID(mapnaam)`, `Name`, `URL`, `Username: "mcp-bot"`, `ProjectPath`, `KinstaSiteID`; anders `AddLocalProjectPath`; zet daarna `import_done: true` in `config.yml` (ook bij 0 keuzes = overslaan); geeft het aantal toegevoegde items terug; een fout bij één item stopt niet, alle fouten worden gebundeld teruggegeven
  - `func (s *WPSiteService) CreateWithoutPassword(site domain.WPSite) (WPSiteView, error)` — als `Create`, zonder wachtwoord (geen Keychain-item)
  - `Global.ImportDone bool \`yaml:"import_done"\``

- [ ] **Step 1: Falende tests** — `internal/services/import_service_test.go`:

```go
package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rdm/sites-tool/internal/config"
)

type importFix struct {
	svc         *ImportService
	cfg         *config.Global
	sec         *wpNepSecrets
	root        string
	sitesPath   string
	localsPath  string
}

func importFixture(t *testing.T) importFix {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, "Projects")
	mk := func(name, deploy, rdm string) {
		d := filepath.Join(root, name)
		os.MkdirAll(d, 0o755)
		if deploy != "" {
			os.WriteFile(filepath.Join(d, "deploy_conf.json"), []byte(deploy), 0o644)
		}
		if rdm != "" {
			os.MkdirAll(filepath.Join(d, ".rdm"), 0o755)
			os.WriteFile(filepath.Join(d, ".rdm", "config.yml"), []byte(rdm), 0o644)
		}
	}
	mk("web-bnl", `{"type":"wordpress_kinsta","link":{"prod":"https://b.nl"}}`, "kinsta:\n  site_id: k-b\n")
	mk("web-anl", `{"type":"wordpress_transip","link":{"prod":"https://a.nl"}}`, "")
	mk("tool-laravel", `{"type":"laravel_9"}`, "")
	sitesPath, localsPath := config.WPSitesPath(), config.LocalProjectsPath()
	ps := NewProjectService(sitesPath, localsPath)
	sec := &wpNepSecrets{m: map[string]string{}}
	wp := NewWPSiteService(sitesPath, ps, sec)
	cfg := &config.Global{ProjectsRoots: []string{root}}
	rs := NewRegisterService(ps, wp, localsPath)
	return importFix{svc: NewImportService(cfg, wp, rs, sitesPath, localsPath), cfg: cfg, sec: sec, root: root, sitesPath: sitesPath, localsPath: localsPath}
}

func TestImportNeededEnCandidates(t *testing.T) {
	f := importFixture(t)
	need, err := f.svc.Needed()
	if err != nil || !need {
		t.Fatalf("needed %v %v", need, err)
	}
	c, err := f.svc.Candidates()
	if err != nil || len(c) != 3 {
		t.Fatalf("candidates %+v %v", c, err)
	}
	if c[0].Name != "tool-laravel" || c[0].WordPress {
		t.Fatalf("sortering/vlag: %+v", c[0])
	}
	if c[1].Name != "web-anl" || !c[1].WordPress || c[1].URL != "https://a.nl" {
		t.Fatalf("anl %+v", c[1])
	}
	if c[2].KinstaSiteID != "k-b" || c[2].DeployType != "wordpress_kinsta" {
		t.Fatalf("bnl %+v", c[2])
	}
}

func TestImportApply(t *testing.T) {
	f := importFixture(t)
	n, err := f.svc.Apply([]ImportChoice{
		{Path: filepath.Join(f.root, "web-bnl"), AsSite: true},
		{Path: filepath.Join(f.root, "tool-laravel"), AsSite: false},
	})
	if err != nil || n != 2 {
		t.Fatalf("n %d err %v", n, err)
	}
	sites, _ := config.LoadWPSites(f.sitesPath)
	if len(sites) != 1 || sites[0].ID != "web-bnl" || sites[0].URL != "https://b.nl" || sites[0].KinstaSiteID != "k-b" || sites[0].Username != "mcp-bot" {
		t.Fatalf("sites %+v", sites)
	}
	if len(f.sec.m) != 0 {
		t.Fatalf("er mag geen Keychain-item zijn: %v", f.sec.m)
	}
	locals, _ := config.LoadLocalProjects(f.localsPath)
	if len(locals) != 1 {
		t.Fatalf("locals %+v", locals)
	}
	if !f.cfg.ImportDone {
		t.Fatal("ImportDone niet gezet")
	}
	if need, _ := f.svc.Needed(); need {
		t.Fatal("Needed moet nu false zijn")
	}
}

func TestImportOverslaan(t *testing.T) {
	f := importFixture(t)
	if n, err := f.svc.Apply(nil); err != nil || n != 0 {
		t.Fatalf("n %d err %v", n, err)
	}
	if need, _ := f.svc.Needed(); need {
		t.Fatal("na overslaan geen import meer")
	}
	g, _ := config.LoadGlobal()
	if !g.ImportDone {
		t.Fatal("import_done niet opgeslagen")
	}
}

func TestImportIdBotsing(t *testing.T) {
	f := importFixture(t)
	// Tweede map die hetzelfde id-voorstel oplevert als web-bnl.
	d := filepath.Join(t.TempDir(), "web-bnl")
	os.MkdirAll(d, 0o755)
	os.WriteFile(filepath.Join(d, "deploy_conf.json"), []byte(`{"type":"wordpress_kinsta","link":{"prod":"https://b2.nl"}}`), 0o644)
	_, err := f.svc.Apply([]ImportChoice{{Path: filepath.Join(f.root, "web-bnl"), AsSite: true}, {Path: d, AsSite: true}})
	if err != nil {
		t.Fatal(err)
	}
	sites, _ := config.LoadWPSites(f.sitesPath)
	if len(sites) != 2 || sites[1].ID != "web-bnl-2" {
		t.Fatalf("sites %+v", sites)
	}
}
```

`Apply` slaat `ImportDone` op via `config.SaveGlobal(*cfg)` na `cfg.ImportDone = true` (zelfde patroon als `SettingsService`).

- [ ] **Step 2–4:** RED → implementeer (ID-botsing: voeg `-2`, `-3`, … toe tot uniek) → GREEN.
- [ ] **Step 5: Commit** — `feat: eenmalige import van de oude projectscan`.

---

### Task 7: Wiring + bindings

**Files:**
- Modify: `internal/app/app.go` (`Services` struct: `Register`, `Import`; `NewServices`; `Wails()`), `main.go` (`services.Register.SetApp(a)`)
- Generated: `frontend/bindings/**`

- [ ] **Step 1:** `RegisterService` en `ImportService` aanmaken en binden; `WPSiteService` krijgt de `ProjectService` als lookup (heeft nu `Reload`).
- [ ] **Step 2:** `go build ./internal/... && go test ./internal/...`.
- [ ] **Step 3:** Bindings regenereren; controleer dat `ProjectService` geen `AddRoot/RemoveRoot/GetRoots/Scan` meer heeft en wel `Reload`; `RegisterService` en `ImportService` bestaan; `WPSite.kinstaSiteId`, `Project.siteId/local/missing`, `ProjectStatusSummary.siteId/local/missing` staan in de modellen.
- [ ] **Step 4:** `cd frontend && npx tsc --noEmit` — dit FAALT nu op `App.tsx` (oude `Scan/AddRoot/...`-aanroepen). Dat is verwacht; noteer de fouten in het rapport. Task 8 lost ze op. Commit toch (bindings + Go), zodat Task 8 op de nieuwe bindings bouwt.
- [ ] **Step 5: Commit** — `feat: register- en importservice aan de app koppelen`.

---

### Task 8: Zijbalk, +-menu en importvenster

**Files:**
- Modify: `frontend/src/App.tsx`
- Create: `frontend/src/components/ImportDialog.tsx`
- Modify: `frontend/src/components/WPSitePage.tsx` (alleen nog "nieuwe site")

**Interfaces:**
- Consumes: bindings `ProjectService.{Reload, BatchStatus, RefreshOne}`, `RegisterService.{AddLocalProject, ChooseFolder, LinkSiteFolder}`, `ImportService.{Needed, Candidates, Apply}`, `WPSiteService.Create`.

- [ ] **Step 1: App.tsx**
  - Verwijder: roots-state, `GetRoots/AddRoot/RemoveRoot`-aanroepen, het roots-blok in de zijbalk, `EmptyState` met "Folder toevoegen", de aparte sectie "WordPress-sites" en `wpSites`-state voor losse sites.
  - `doScan` → `doReload`: `await ProjectService.Reload()` dan `BatchStatus()`; knop **Verversen** (bestaand icoon) roept dit aan.
  - `ProjectRow` toont rechts precies één label: `s.missing ? 'map niet gevonden' : s.local ? 'lokaal' : 'online'`, in dezelfde kleine stijl als de bestaande badges.
  - Filterchips: voeg vóór de deploy-type-chips een chip **alleen online** toe (filter `!s.local`).
  - Knop **+** in de zijbalk-kop opent een klein menu (absoluut gepositioneerde `div`, sluit bij klik buiten) met **WordPress-site toevoegen** (→ `setWpSelected(null)`, bestaand formulierpad) en **Lokaal project toevoegen** (→ `RegisterService.AddLocalProject()` → bij resultaat `doReload()` en `selectProject(p.id)`).
  - Lege lijst: tekst `Nog niets toegevoegd. Gebruik + om een WordPress-site of lokaal project toe te voegen.`
  - Na opslaan van een nieuwe site in `WPSitePage` (`onSaved`): `doReload()` en het project `site:<id>` (of het pad-ID als er een map is) selecteren; `setWpSelected(undefined)`.
  - Importvenster: bij mount `ImportService.Needed()` → true → `<ImportDialog onKlaar={() => { setImport(false); doReload() }} />`.
- [ ] **Step 2: `ImportDialog.tsx`** — modaal (zelfde stijl als `AppUpdateDialog`): haalt `Candidates()`, toont een lijst met vinkje per regel (standaard aan als `wordPress`), kolommen naam, type (`deployType`), URL; per aangevinkte WordPress-regel een tweede keuze "als site" (standaard aan) vs "als lokaal project"; knoppen **Importeren** (→ `Apply(keuzes)`, toon aantal/fouten, dan `onKlaar`) en **Overslaan** (→ `Apply([])`, dan `onKlaar`). Uitleg bovenaan: `De tool scant geen mappen meer. Kies welke projecten je wilt houden; Application Passwords vul je daarna per site in.`
- [ ] **Step 3: WPSitePage** — alleen nog de "nieuwe site"-variant (`view === null`); verwijder de takken voor bestaande losse sites en `projectMissing`/`onUnlinked` (die lopen nu via ProjectDetail). Voeg in het formulier een knop **Map kiezen (optioneel)** toe die `RegisterService.ChooseFolder()` aanroept en het pad in `projectPath` zet.
- [ ] **Step 4: Verifieer** — `npx tsc --noEmit && npm run build:dev` groen (App.tsx-fouten uit Task 7 opgelost).
- [ ] **Step 5: Commit** — `feat: zijbalk uit het register met toevoegmenu en importvenster`.

---

### Task 9: Menu met vergrendelde functies, Kinsta-tab weg

**Files:**
- Modify: `frontend/src/components/ProjectDetail.tsx`

- [ ] **Step 1:** Bepaal bovenaan:

```tsx
  const heeftMap = project.local && !project.missing
  const heeftKinsta = !!project.config?.kinsta?.siteId
  const [bridgeSite, setBridgeSite] = useState<boolean>(false)
  useEffect(() => {
    let actueel = true
    setBridgeSite(false)
    Services.WPSiteService.ForProject(project.id)
      .then(v => { if (actueel) setBridgeSite(!!v?.hasPassword && !!v?.site.bridge) })
      .catch(() => {})
    return () => { actueel = false }
  }, [project.id])
```

- [ ] **Step 2:** Breid `NavItem` uit met `locked?: string` (tooltip-tekst). Bouw de groepen volgens de tabel in de spec, zonder `.filter` op vereisten maar met `locked`:
  - SOURCE CONTROL: alle items `locked: heeftMap ? undefined : 'vereist een lokale map'`
  - WORDPRESS (alleen tonen als `project.siteId || project.deploy?.type?.startsWith('wordpress')`): Updates (`locked` als `!bridgeSite && !heeftMap` → `'vereist een bridge-koppeling'`), Plugins/Security/Tests (`vereist een lokale map`), Logs (`vereist een Kinsta-koppeling`), Media-analyse (eerst Kinsta, dan map)
  - MIGRATIE (zelfde zichtbaarheid als WORDPRESS): Database, Media, Instellingen: eerst map, dan Kinsta; DB-editor alleen als `dbEditorBeschikbaar` (ongewijzigd)
  - TOOLS: Terminal (`vereist een Kinsta-koppeling`, zichtbaar als `heeftKinsta || project.siteId`)
  - KLANT: Rapportage (`vereist een lokale map`)
  - Verwijder het item `kinsta` en de render-tak `activeTab === 'kinsta'` + import van `KinstaTab`.
- [ ] **Step 3:** Render een vergrendeld item als knop met `opacity-50`, een 🔒-teken rechts (`text-[10px]`) en `title={it.locked}`; klik → `setActiveTab('projectsettings')`. `Tooltip` gebruiken als dat in dit bestand al gebeurt, anders `title`.
- [ ] **Step 4:** Header: editor-, fetch-, pull-, push-knoppen alleen als `heeftMap`; branch-badge alleen bij `heeftMap && status?.isRepo`. `refreshStatus` alleen als `heeftMap`. DB-editor `Info`-call alleen als `heeftMap`.
- [ ] **Step 5:** `npx tsc --noEmit && npm run build:dev`; commit `feat: menu toont vergrendelde functies en de Kinsta-tab is weg`. Verwijder `KinstaTab.tsx` alleen als niets anders hem importeert (`grep -rn KinstaTab frontend/src`).

---

### Task 10: Projectinstellingen, Kinsta-site in het formulier, kloonmap

**Files:**
- Modify: `frontend/src/components/ProjectSettingsTab.tsx`, `frontend/src/components/WPSiteForm.tsx`, `frontend/src/components/SettingsPage.tsx`
- Modify (Go, klein): `internal/services/settings_service.go` (`CloneDir` in `AppSettings` + Get/Save) + test

- [ ] **Step 1: Go** — `AppSettings.CloneDir string \`json:"cloneDir"\``; `Get` vult hem, `Save` schrijft `cfg.CloneDir` (leeg → `~/Projects`). Test in `settings_service_test.go`. Bindings regenereren.
- [ ] **Step 2: ProjectSettingsTab**
  - Blok **Lokale map** bovenaan: pad of `niet gekoppeld`; voor een site: knoppen **Map kiezen** (`RegisterService.ChooseFolder()` → `RegisterService.LinkSiteFolder(project.siteId, pad)` → `onRefresh`) en **Ontkoppelen** (`WPSiteService.Unlink(project.siteId)`, via `bevestig`); voor een lokaal project (geen `siteId`): **Uit de lijst verwijderen** (`RegisterService.RemoveLocalProject`, via `bevestig`, tekst `De map zelf blijft staan.`) en **Als WordPress-site koppelen** (`RegisterService.ConvertLocalToSite(project.id)` → toon `WPSiteForm` met dat voorstel, `isNew`).
  - Blokken die een map nodig hebben (Lokale URL, SSH-toegang, "Acceptatie & productie" uit `deploy_conf.json`) alleen tonen als `project.local && !project.missing`.
  - WordPress MCP-blok: voor een site (`project.siteId`) altijd tonen (ook zonder map, ongeacht `DetectBridge`); `DetectBridge` alleen aanroepen bij een map.
  - Na elke wijziging `onRefresh()` (de parent herlaadt het register).
- [ ] **Step 3: WPSiteForm** — veld **Kinsta-site**: een `<select>` gevuld via `KinstaService.ListSites()` (optie `— geen —` + per site `displayName || name`); bij een nieuwe site of lege koppeling eerst `KinstaService.SuggestSiteForURL(site.url)` en bij `found` voorselecteren met de tekst `voorgesteld op basis van de URL`. Kinsta niet geconfigureerd of fout → veld uitgeschakeld met de fouttekst. Waarde in `site.kinstaSiteId`. Omgevingskeuze: als de gekozen site >1 omgeving heeft (`KinstaService.GetSiteDetails`), een tweede select voor `kinstaEnvId` (optie `live (standaard)` = leeg).
- [ ] **Step 4: SettingsPage** — invoerveld **Kloonmap** in het bestaande instellingenblok voor Git/Org-sync (zelfde stijl), opgeslagen via `SettingsService.Save`.
- [ ] **Step 5:** `go test ./internal/...`, `npx tsc --noEmit && npm run build:dev`; commit `feat: lokale map en Kinsta-site koppelen vanuit de projectinstellingen`.

---

### Task 11: Handmatige verificatie

- [ ] **Step 1:** `go test ./internal/...`, frontend-build groen.
- [ ] **Step 2 (gebruiker):** dev-server herstarten (Go gewijzigd).
- [ ] **Step 3:** Eerste start → importvenster met de mappen uit `~/Projects`; WordPress-mappen aangevinkt; importeren.
- [ ] **Step 4:** Een geïmporteerd project met testruns/rapporten → historie nog zichtbaar (zelfde ID).
- [ ] **Step 5:** Nieuwe WordPress-site zonder map toevoegen → label `online`; Updates werkt na wachtwoord + Test verbinding; Source Control, Migratie op slot met de juiste tooltip; klik → Projectinstellingen.
- [ ] **Step 6:** Map koppelen → tabs vrij, label `lokaal`, ID gelijk aan het pad-ID.
- [ ] **Step 7:** Kinsta-site kiezen → Logs en Terminal werken zonder `.rdm`-koppeling.
- [ ] **Step 8:** Lokaal project toevoegen/verwijderen; Org-sync kloont naar de kloonmap en het project verschijnt.
- [ ] **Step 9:** Rapporteer per stap.
