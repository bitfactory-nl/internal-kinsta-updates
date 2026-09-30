# WordPress-updates via de bridge — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Voor elke gekoppelde WordPress-site beschikbare updates tonen en uitvoeren via de nb-mcp-bridge, een vlootoverzicht bieden, en na een core-update op de SatisPress-depotsite automatisch een Composer-PR maken.

**Architecture:** `WPSiteService` krijgt een kleine bridge-HTTP-laag (`wpsite_bridge.go`) waarop lees- (`wpsite_updates.go`) en uitvoermethodes (`wpsite_update_run.go`) bouwen. De Composer-PR voor de depotsite zit in een apart onderdeel (`depot_core_pr.go`) dat de bestaande worktree/PR-hulpmiddelen van `WPCoreUpdateService` hergebruikt en via een interface in `WPSiteService` wordt geïnjecteerd. De frontend krijgt een herbruikbaar `SiteUpdatesPanel` (Updates-tab + losse-sitepagina) en een pagina `FleetUpdatesPage`.

**Tech Stack:** Go 1.25, Wails v3 alpha.80, standaard `testing` + `net/http/httptest`, React 18 + TypeScript + Tailwind v4, Composer 2.9 CLI, GitHub REST via `internal/adapters/github`.

**Spec:** `docs/superpowers/specs/2026-09-30-wp-updates-via-bridge-design.md` (bouwt voort op `2026-09-29-wp-fleet-sites-design.md`)

## Global Constraints

- Werk in `/Users/jeffreyt/Projects/RDM-Sites-tool-wp-fleet`, branch `feature/wp-fleet-sites`. Daar draait `task dev` (de gebruiker test live): **draai geen `go test`/`go build` die naar `bin/` schrijft**; `go test ./internal/...` en `go vet ./internal/...` zijn veilig (schrijven niet naar `bin/`). Geen `task package`.
- Bridge-routes: `GET /wp-json/nb-mcp/v1/updates[?refresh=1]`, `POST /wp-json/nb-mcp/v1/updates/plugins` `{"plugins":[...]}`, `POST /wp-json/nb-mcp/v1/updates/core` `{"allow_major":bool}`, `POST /wp-json/nb-mcp/v1/updates/translations`.
- Timeouts: lezen 15 s, lezen met refresh 60 s, uitvoeren 5 min.
- Foutmeldingen (letterlijk): geen wachtwoord → `geen Application Password opgeslagen`; bridge uit → `nb-mcp-bridge is niet actief op deze site — test eerst de verbinding`; 401 → `gebruiker of Application Password klopt niet`; 403/409 → `message` uit de WordPress-foutbody (fallback `WordPress weigerde het verzoek (HTTP <code>)`); 404 → `nb-mcp-bridge ontbreekt op deze site`; overig/netwerk → `site niet bereikbaar (HTTP <code>)` resp. `site niet bereikbaar`; read-only → `deze site staat op alleen-lezen`; dubbele run → `er loopt al een update voor deze site`.
- Het wachtwoord komt nooit in een foutmelding, log of UI.
- Hooguit één site `depot: true`; melding `site "<id>" is al de depotsite`.
- Depot-core-PR: branch `update/wordpress-<versie>`, commit/PR-titel `chore: WordPress naar <versie>` / `WordPress <versie>`, alleen `composer.json` en `composer.lock` gestaged, worktree altijd opruimen, bestaande open PR → die URL teruggeven. Mislukt de PR na een geslaagde live update → `RunResult.Warning`, geen fout.
- Actions-URL: `https://github.com/<owner>/<repo>/actions/workflows/plugin-update.yml`.
- Vlootoverzicht: concurrency 4; fout op één site stopt de rest niet.
- Bevestigingen altijd via `bevestig()` uit `frontend/src/lib/bevestig.ts` (nooit `window.confirm`). Knoppen verspringen niet (`KnopTekst`).
- Thema's alleen tonen, niet bijwerken.
- Go: standaard `testing`, `gofmt -w` op gewijzigde bestanden, commentaar en meldingen in het Nederlands. Frontend: geen testframework — `cd frontend && npx tsc --noEmit && npm run build:dev`.
- Bindings regenereren na publieke Go-wijzigingen: `PATH="$HOME/go/bin:$PATH" task common:generate:bindings` (vanuit de worktree-root).
- Commits: conventional, eindigen met `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

**Ruling (plan t.o.v. spec):** de spec zegt "past de versie binnen de constraint dan alleen `composer update`, anders eerst `composer require`". Het plan doet altijd `composer require roots/wordpress:^<major>.<minor> --no-update` gevolgd door `composer update roots/wordpress --with-dependencies --no-install --no-scripts --no-plugins`. Reden: geen eigen semver-evaluatie nodig; bij een patch-update blijft de constraint gelijk, bij een minor/major wordt hij de nieuwe ondergrens. Kost bij fout: de constraint in `composer.json` wordt strakker dan nodig (reviewbaar in de PR).

---

## Bestandsoverzicht

| Bestand | Status | Verantwoordelijkheid |
|---|---|---|
| `internal/domain/wpsite.go` | wijzigen | veld `Depot` |
| `internal/config/wpsites.go` (+test) | wijzigen | uniek-depot-validatie |
| `internal/services/wpsite_service.go` | wijzigen | `WPProjectLookup.List`, constructor met depot-hulp, `longClient` |
| `internal/services/wpsite_bridge.go` (+test) | nieuw | `bridgeCall` + foutvertaling |
| `internal/services/wpsite_updates.go` (+test) | nieuw | `AvailableUpdates`, `FleetUpdates`, types |
| `internal/services/wpsite_update_run.go` (+test) | nieuw | `UpdatePlugins`, `UpdateCore`, `UpdateTranslations`, claim |
| `internal/services/depot_core_pr.go` (+test) | nieuw | `DepotCorePR`-interface + implementatie, composer-zoeker |
| `internal/app/app.go` | wijzigen | depot-hulp injecteren |
| `frontend/src/components/KnopTekst.tsx` | nieuw | gedeelde knoptekst (uit `WPSiteForm`) |
| `frontend/src/components/WPSiteForm.tsx` | wijzigen | depot-vinkje, `KnopTekst` importeren |
| `frontend/src/components/SiteUpdatesPanel.tsx` | nieuw | blok "Beschikbaar op de site" |
| `frontend/src/components/UpdatesTab.tsx` | wijzigen | panel bovenaan |
| `frontend/src/components/WPSitePage.tsx` | wijzigen | panel onder formulier |
| `frontend/src/components/FleetUpdatesPage.tsx` | nieuw | vlootoverzicht |
| `frontend/src/App.tsx` | wijzigen | railitem + view `fleet` |

---

### Task 1: Depot-rol op `WPSite`

**Files:**
- Modify: `internal/domain/wpsite.go` (struct `WPSite`)
- Modify: `internal/config/wpsites.go` (`validateWPSites`)
- Test: `internal/config/wpsites_test.go`

**Interfaces:**
- Produces: `WPSite.Depot bool` (JSON `depot,omitempty`, YAML `depot,omitempty`).

- [ ] **Step 1: Falende test** — voeg toe aan `internal/config/wpsites_test.go`:

```go
func TestSaveWPSitesWeigertTweeDepots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wp-sites.yml")
	sites := []domain.WPSite{
		{ID: "aa", Name: "a", URL: "https://a.nl", Username: "u", Depot: true},
		{ID: "bb", Name: "b", URL: "https://b.nl", Username: "u", Depot: true},
	}
	err := SaveWPSites(path, sites)
	if err == nil || !strings.Contains(err.Error(), `site "bb" is al de depotsite`) &&
		!strings.Contains(err.Error(), `site "aa" is al de depotsite`) {
		t.Fatalf("verwacht depot-fout, kreeg %v", err)
	}
}

func TestWPSitesDepotRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wp-sites.yml")
	if err := SaveWPSites(path, []domain.WPSite{{ID: "aa", Name: "a", URL: "https://a.nl", Username: "u", Depot: true}}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "depot: true") {
		t.Fatalf("depot niet opgeslagen:\n%s", raw)
	}
	got, err := LoadWPSites(path)
	if err != nil || !got[0].Depot {
		t.Fatalf("got %+v, %v", got, err)
	}
}
```

- [ ] **Step 2: Run, verwacht FAIL**

Run: `go test ./internal/config/ -run 'Depot' -v`
Expected: FAIL — `unknown field Depot`.

- [ ] **Step 3: Implementeer**

In `internal/domain/wpsite.go`, in `WPSite` na `Bridge`:

```go
	// Depot markeert de SatisPress-depotsite: na een core-update daar maakt de
	// tool een Composer-PR. Er is er hooguit één.
	Depot bool `json:"depot,omitempty" yaml:"depot,omitempty"`
```

In `internal/config/wpsites.go`, in `validateWPSites`: houd een `depot := ""` bij in de lus, en na de project-check:

```go
		if s.Depot {
			if depot != "" {
				return fmt.Errorf("site %q is al de depotsite", depot)
			}
			depot = s.ID
		}
```

(De melding noemt de site die al depot was; de test accepteert beide ids.)

- [ ] **Step 4: Run, verwacht PASS**

Run: `go test ./internal/config/ ./internal/domain/ -v -run 'Depot|WPSite'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/domain/wpsite.go internal/config/wpsites.go internal/config/wpsites_test.go
git add internal/domain/wpsite.go internal/config/wpsites.go internal/config/wpsites_test.go
git commit -m "feat: depot-rol voor de SatisPress-site

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Bridge-HTTP-laag + `AvailableUpdates`

**Files:**
- Modify: `internal/services/wpsite_service.go` (interface `WPProjectLookup`, nieuwe helper `longClient`)
- Modify: `internal/services/wpsite_service_test.go` (`wpNepProjecten` krijgt `List`)
- Create: `internal/services/wpsite_bridge.go`
- Create: `internal/services/wpsite_updates.go`
- Test: `internal/services/wpsite_updates_test.go`

**Interfaces:**
- Consumes: `WPSiteService.find(id)`, `.secret(id)`, veld `http *http.Client` (Transport wordt in tests vervangen), `domain.WPSite.Bridge`.
- Produces:
  - `WPProjectLookup` = `{ Get(id string) (domain.Project, bool); List() []domain.Project }`
  - `func (s *WPSiteService) longClient(timeout time.Duration) *http.Client`
  - `func (s *WPSiteService) bridgeCall(site domain.WPSite, pw, method, path string, body any, timeout time.Duration, out any) error`
  - consts `bridgeReadTimeout = 15*time.Second`, `bridgeRefreshTimeout = 60*time.Second`, `bridgeRunTimeout = 5*time.Minute`
  - types `SiteUpdates`, `CoreUpdateOffer`, `ItemUpdate` (zie code)
  - `func (s *WPSiteService) AvailableUpdates(siteID string, refresh bool) (SiteUpdates, error)`
  - `func (s *WPSiteService) siteMetSecret(siteID string) (domain.WPSite, string, error)` (gedeelde precheck)

- [ ] **Step 1: `wpNepProjecten` uitbreiden** — in `internal/services/wpsite_service_test.go` onder de bestaande `Get`:

```go
func (n wpNepProjecten) List() []domain.Project {
	out := make([]domain.Project, 0, len(n))
	for _, p := range n {
		out = append(out, p)
	}
	return out
}
```

- [ ] **Step 2: Falende tests** — `internal/services/wpsite_updates_test.go`:

```go
package services

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

const updatesJSON = `{
 "checked_at":"2026-09-30T08:00:00Z",
 "core":[{"current":"7.1.3","version":"7.1.2","type":"minor","locale":"nl_NL","response":"upgrade"}],
 "plugins":[{"plugin":"akismet/akismet.php","name":"Akismet","current_version":"5.3","new_version":"5.4","package_available":true}],
 "themes":[{"stylesheet":"twentytwentyfive","name":"TT5","current_version":"1.0","new_version":"1.1","package_available":true}],
 "translations":{"count":3}
}`

// bridgeServer beantwoordt alleen goed geauthenticeerde verzoeken en onthoudt
// het laatste pad (met query) en de body.
type bridgeServer struct {
	srv      *httptest.Server
	lastPath string
	lastBody string
	status   int
	respBody string
}

func nieuweBridgeServer(t *testing.T, status int, body string) *bridgeServer {
	t.Helper()
	b := &bridgeServer{status: status, respBody: body}
	b.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pw, ok := r.BasicAuth()
		if !ok || user != "mcp-bot" || pw != "goed" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		b.lastPath = r.URL.RequestURI()
		buf := new(strings.Builder)
		_, _ = io.Copy(buf, r.Body)
		b.lastBody = buf.String()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(b.status)
		_, _ = w.Write([]byte(b.respBody))
	}))
	t.Cleanup(b.srv.Close)
	return b
}

// bridgeSite maakt een site met bridge aan die naar de testserver wijst.
func bridgeSite(t *testing.T, b *bridgeServer, pw string, mut func(*domain.WPSite)) *WPSiteService {
	t.Helper()
	svc, _, _ := nieuweWPSiteService(t)
	svc.http.Transport = b.srv.Client().Transport
	s := domain.WPSite{ID: "klant", Name: "Klant", URL: b.srv.URL + "/", Username: "mcp-bot", Bridge: true}
	if mut != nil {
		mut(&s)
	}
	if _, err := svc.Create(s, pw); err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestAvailableUpdatesMapping(t *testing.T) {
	b := nieuweBridgeServer(t, 200, updatesJSON)
	svc := bridgeSite(t, b, "goed", nil)
	got, err := svc.AvailableUpdates("klant", false)
	if err != nil {
		t.Fatal(err)
	}
	if b.lastPath != "/wp-json/nb-mcp/v1/updates" {
		t.Fatalf("pad %q", b.lastPath)
	}
	if got.SiteID != "klant" || got.CheckedAt != "2026-09-30T08:00:00Z" || got.Translations != 3 {
		t.Fatalf("kop: %+v", got)
	}
	if len(got.Core) != 1 || got.Core[0].Version != "7.1.2" || got.Core[0].Type != "minor" {
		t.Fatalf("core: %+v", got.Core)
	}
	if len(got.Plugins) != 1 || got.Plugins[0] != (ItemUpdate{ID: "akismet/akismet.php", Name: "Akismet", CurrentVersion: "5.3", NewVersion: "5.4"}) {
		t.Fatalf("plugins: %+v", got.Plugins)
	}
	if len(got.Themes) != 1 || got.Themes[0].ID != "twentytwentyfive" || got.Themes[0].NewVersion != "1.1" {
		t.Fatalf("themes: %+v", got.Themes)
	}
}

func TestAvailableUpdatesRefreshParam(t *testing.T) {
	b := nieuweBridgeServer(t, 200, updatesJSON)
	svc := bridgeSite(t, b, "goed", nil)
	if _, err := svc.AvailableUpdates("klant", true); err != nil {
		t.Fatal(err)
	}
	if b.lastPath != "/wp-json/nb-mcp/v1/updates?refresh=1" {
		t.Fatalf("pad %q", b.lastPath)
	}
}

func TestAvailableUpdatesFouten(t *testing.T) {
	cases := []struct {
		naam   string
		status int
		body   string
		pw     string
		mut    func(*domain.WPSite)
		want   string
	}{
		{"401", 200, updatesJSON, "fout-geheim", nil, "gebruiker of Application Password klopt niet"},
		{"403 met body", 403, `{"code":"nb_mcp_file_mods_disabled","message":"File modifications are disabled on this site (DISALLOW_FILE_MODS is true)."}`, "goed", nil, "DISALLOW_FILE_MODS"},
		{"403 zonder body", 403, ``, "goed", nil, "WordPress weigerde het verzoek (HTTP 403)"},
		{"404", 404, `{"code":"rest_no_route"}`, "goed", nil, "nb-mcp-bridge ontbreekt op deze site"},
		{"500", 500, ``, "goed", nil, "site niet bereikbaar (HTTP 500)"},
		{"bridge uit", 200, updatesJSON, "goed", func(s *domain.WPSite) { s.Bridge = false }, "nb-mcp-bridge is niet actief"},
	}
	for _, c := range cases {
		b := nieuweBridgeServer(t, c.status, c.body)
		svc := bridgeSite(t, b, c.pw, c.mut)
		_, err := svc.AvailableUpdates("klant", false)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: fout %v, want %q", c.naam, err, c.want)
		}
		if err != nil && strings.Contains(err.Error(), c.pw) {
			t.Errorf("%s: wachtwoord in fout", c.naam)
		}
	}
}

func TestAvailableUpdatesZonderWachtwoord(t *testing.T) {
	b := nieuweBridgeServer(t, 200, updatesJSON)
	svc, sec, _ := nieuweWPSiteService(t)
	svc.http.Transport = b.srv.Client().Transport
	if _, err := svc.Create(domain.WPSite{ID: "klant", Name: "K", URL: b.srv.URL, Username: "mcp-bot", Bridge: true}, "goed"); err != nil {
		t.Fatal(err)
	}
	delete(sec.m, "wp:klant")
	_, err := svc.AvailableUpdates("klant", false)
	if err == nil || err.Error() != "geen Application Password opgeslagen" {
		t.Fatalf("fout %v", err)
	}
	if b.lastPath != "" {
		t.Fatal("er mocht geen netwerkcall zijn")
	}
}

```


- [ ] **Step 3: Run, verwacht FAIL**

Run: `go test ./internal/services/ -run 'AvailableUpdates' -v`
Expected: FAIL — `svc.AvailableUpdates undefined`.

- [ ] **Step 4: `WPProjectLookup` en `longClient`** — in `internal/services/wpsite_service.go`:

```go
// WPProjectLookup is het deel van ProjectService dat WPSiteService nodig heeft.
type WPProjectLookup interface {
	Get(id string) (domain.Project, bool)
	List() []domain.Project
}
```

en onder `weigerDowngrade`:

```go
// longClient is de gewone client met een andere timeout. Transport en
// redirectbeleid blijven gelijk, zodat tests (Transport) en de https→http-
// weigering ook hier gelden.
func (s *WPSiteService) longClient(timeout time.Duration) *http.Client {
	return &http.Client{Transport: s.http.Transport, CheckRedirect: s.http.CheckRedirect, Timeout: timeout}
}
```

- [ ] **Step 5: `internal/services/wpsite_bridge.go`**

```go
package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

const (
	bridgeReadTimeout    = 15 * time.Second
	bridgeRefreshTimeout = 60 * time.Second
	bridgeRunTimeout     = 5 * time.Minute
)

// siteMetSecret doet de gedeelde precheck voor elke bridge-aanroep: site
// bestaat, wachtwoord staat in de Keychain, bridge is actief.
func (s *WPSiteService) siteMetSecret(siteID string) (domain.WPSite, string, error) {
	site, err := s.find(siteID)
	if err != nil {
		return domain.WPSite{}, "", err
	}
	pw, err := s.secret(siteID)
	if err != nil || pw == "" {
		return domain.WPSite{}, "", errors.New("geen Application Password opgeslagen")
	}
	if !site.Bridge {
		return domain.WPSite{}, "", errors.New("nb-mcp-bridge is niet actief op deze site — test eerst de verbinding")
	}
	return site, pw, nil
}

// bridgeCall doet één verzoek naar de nb-mcp-bridge en decodeert het antwoord
// in out. Foutmeldingen noemen nooit het wachtwoord en nooit de transportfout
// zelf (die kan de URL met details bevatten).
func (s *WPSiteService) bridgeCall(site domain.WPSite, pw, method, path string, body any, timeout time.Duration, out any) error {
	var rdr io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(data)
	}
	url := strings.TrimRight(site.URL, "/") + "/wp-json/nb-mcp/v1" + path
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		return errors.New("site niet bereikbaar")
	}
	req.SetBasicAuth(site.Username, pw)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.longClient(timeout).Do(req)
	if err != nil {
		return errors.New("site niet bereikbaar")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return errors.New("onverwacht antwoord van de nb-mcp-bridge")
		}
		return nil
	}
	return bridgeFout(resp.StatusCode, raw)
}

// bridgeFout vertaalt een HTTP-fout van WordPress naar een leesbare melding.
func bridgeFout(status int, raw []byte) error {
	switch status {
	case http.StatusUnauthorized:
		return errors.New("gebruiker of Application Password klopt niet")
	case http.StatusNotFound:
		return errors.New("nb-mcp-bridge ontbreekt op deze site")
	case http.StatusForbidden, http.StatusConflict:
		var wpErr struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &wpErr) == nil && strings.TrimSpace(wpErr.Message) != "" {
			msg := strings.TrimSpace(wpErr.Message)
			if len(msg) > 300 {
				msg = msg[:300]
			}
			return errors.New(msg)
		}
		return fmt.Errorf("WordPress weigerde het verzoek (HTTP %d)", status)
	default:
		return fmt.Errorf("site niet bereikbaar (HTTP %d)", status)
	}
}
```

- [ ] **Step 6: `internal/services/wpsite_updates.go`**

```go
package services

import (
	"net/http"
)

// SiteUpdates zijn de updates die WordPress zelf op de site klaar heeft staan.
type SiteUpdates struct {
	SiteID       string            `json:"siteId"`
	CheckedAt    string            `json:"checkedAt"`
	Core         []CoreUpdateOffer `json:"core"`
	Plugins      []ItemUpdate      `json:"plugins"`
	Themes       []ItemUpdate      `json:"themes"`
	Translations int               `json:"translations"`
}

type CoreUpdateOffer struct {
	Current string `json:"current"`
	Version string `json:"version"`
	Type    string `json:"type"` // major | minor
}

// ItemUpdate is één plugin (ID = plugin-bestand) of thema (ID = stylesheet).
type ItemUpdate struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	CurrentVersion string `json:"currentVersion"`
	NewVersion     string `json:"newVersion"`
}

// bridgeUpdates is het antwoord van GET /nb-mcp/v1/updates.
type bridgeUpdates struct {
	CheckedAt string `json:"checked_at"`
	Core      []struct {
		Current string `json:"current"`
		Version string `json:"version"`
		Type    string `json:"type"`
	} `json:"core"`
	Plugins []struct {
		Plugin         string `json:"plugin"`
		Name           string `json:"name"`
		CurrentVersion string `json:"current_version"`
		NewVersion     string `json:"new_version"`
	} `json:"plugins"`
	Themes []struct {
		Stylesheet     string `json:"stylesheet"`
		Name           string `json:"name"`
		CurrentVersion string `json:"current_version"`
		NewVersion     string `json:"new_version"`
	} `json:"themes"`
	Translations struct {
		Count int `json:"count"`
	} `json:"translations"`
}

// AvailableUpdates vraagt de bridge welke updates er klaarstaan. Met refresh
// laat de bridge WordPress eerst opnieuw bij wp.org kijken (trager).
func (s *WPSiteService) AvailableUpdates(siteID string, refresh bool) (SiteUpdates, error) {
	site, pw, err := s.siteMetSecret(siteID)
	if err != nil {
		return SiteUpdates{}, err
	}
	path, timeout := "/updates", bridgeReadTimeout
	if refresh {
		path, timeout = "/updates?refresh=1", bridgeRefreshTimeout
	}
	var raw bridgeUpdates
	if err := s.bridgeCall(site, pw, http.MethodGet, path, nil, timeout, &raw); err != nil {
		return SiteUpdates{}, err
	}
	out := SiteUpdates{
		SiteID: siteID, CheckedAt: raw.CheckedAt, Translations: raw.Translations.Count,
		Core: []CoreUpdateOffer{}, Plugins: []ItemUpdate{}, Themes: []ItemUpdate{},
	}
	for _, c := range raw.Core {
		out.Core = append(out.Core, CoreUpdateOffer{Current: c.Current, Version: c.Version, Type: c.Type})
	}
	for _, p := range raw.Plugins {
		out.Plugins = append(out.Plugins, ItemUpdate{ID: p.Plugin, Name: p.Name, CurrentVersion: p.CurrentVersion, NewVersion: p.NewVersion})
	}
	for _, t := range raw.Themes {
		out.Themes = append(out.Themes, ItemUpdate{ID: t.Stylesheet, Name: t.Name, CurrentVersion: t.CurrentVersion, NewVersion: t.NewVersion})
	}
	return out, nil
}
```

- [ ] **Step 7: Run, verwacht PASS**

Run: `go test ./internal/services/ -run 'AvailableUpdates|WPSite|TestConnection|Fleet' -v`
Expected: PASS (ook alle bestaande WPSite-, TestConnection- en Fleet-tests: `ProjectService` voldoet al aan `List()`; controleer met `go vet ./internal/...`).

- [ ] **Step 8: Commit**

```bash
gofmt -w internal/services/wpsite_*.go
git add internal/services/wpsite_service.go internal/services/wpsite_service_test.go internal/services/wpsite_bridge.go internal/services/wpsite_updates.go internal/services/wpsite_updates_test.go
git commit -m "feat: beschikbare updates van een site via de nb-mcp-bridge

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Vlootoverzicht `FleetUpdates`

**Files:**
- Modify: `internal/services/wpsite_updates.go`
- Test: `internal/services/wpsite_updates_test.go`

**Interfaces:**
- Consumes: `AvailableUpdates`, `List()` (service), `s.projects.List()`.
- Produces:
  - `type FleetSiteUpdates struct { Site WPSiteView; ProjectID string; Updates *SiteUpdates; Error string }` (JSON `site, projectId, updates, error`)
  - `func (s *WPSiteService) FleetUpdates(refresh bool) ([]FleetSiteUpdates, error)` — volgorde gelijk aan `List()`; sites zonder wachtwoord krijgen `Error: "geen Application Password opgeslagen"` zonder netwerkcall
  - const `fleetConcurrency = 4`

- [ ] **Step 1: Falende tests** — toevoegen aan `wpsite_updates_test.go`:

```go
func TestFleetUpdatesMixEnProjectID(t *testing.T) {
	ok := nieuweBridgeServer(t, 200, updatesJSON)
	kapot := nieuweBridgeServer(t, 500, ``)
	svc, _, projDir := nieuweWPSiteService(t)
	// Beide TLS-testservers gebruiken hetzelfde testcertificaat; één Transport volstaat.
	svc.http.Transport = ok.srv.Client().Transport
	if _, err := svc.Create(domain.WPSite{ID: "een", Name: "Een", URL: ok.srv.URL, Username: "mcp-bot", Bridge: true, ProjectPath: projDir}, "goed"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(domain.WPSite{ID: "twee", Name: "Twee", URL: kapot.srv.URL, Username: "mcp-bot", Bridge: true}, "goed"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.FleetUpdates(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len %d", len(got))
	}
	if got[0].Site.Site.ID != "een" || got[0].Updates == nil || got[0].Error != "" || got[0].ProjectID != "p1" {
		t.Fatalf("een: %+v", got[0])
	}
	if got[1].Site.Site.ID != "twee" || got[1].Updates != nil || !strings.Contains(got[1].Error, "HTTP 500") || got[1].ProjectID != "" {
		t.Fatalf("twee: %+v", got[1])
	}
}

func TestFleetUpdatesConcurrencyMaxVier(t *testing.T) {
	var mu sync.Mutex
	bezig, max := 0, 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		bezig++
		if bezig > max {
			max = bezig
		}
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		bezig--
		mu.Unlock()
		w.Write([]byte(updatesJSON))
	}))
	t.Cleanup(srv.Close)
	svc, _, _ := nieuweWPSiteService(t)
	svc.http.Transport = srv.Client().Transport
	for i := 0; i < 9; i++ {
		id := fmt.Sprintf("site-%d", i)
		if _, err := svc.Create(domain.WPSite{ID: id, Name: id, URL: srv.URL, Username: "mcp-bot", Bridge: true}, "goed"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.FleetUpdates(false); err != nil {
		t.Fatal(err)
	}
	if max > fleetConcurrency || max < 2 {
		t.Fatalf("max gelijktijdig %d, want 2..%d", max, fleetConcurrency)
	}
}
```

Voeg `"fmt"`, `"sync"`, `"time"` toe aan de imports van het testbestand.

- [ ] **Step 2: Run, verwacht FAIL**

Run: `go test ./internal/services/ -run 'FleetUpdates' -v`
Expected: FAIL — `svc.FleetUpdates undefined`.

- [ ] **Step 3: Implementeer** — toevoegen aan `wpsite_updates.go` (imports `"sync"` erbij):

```go
const fleetConcurrency = 4

// FleetSiteUpdates is één regel in het vlootoverzicht.
type FleetSiteUpdates struct {
	Site      WPSiteView   `json:"site"`
	ProjectID string       `json:"projectId"`
	Updates   *SiteUpdates `json:"updates"`
	Error     string       `json:"error"`
}

// FleetUpdates haalt voor alle sites de beschikbare updates op, hooguit vier
// tegelijk. Een fout op één site komt in die regel en stopt de rest niet.
func (s *WPSiteService) FleetUpdates(refresh bool) ([]FleetSiteUpdates, error) {
	views, err := s.List()
	if err != nil {
		return nil, err
	}
	projectByPath := map[string]string{}
	for _, p := range s.projects.List() {
		projectByPath[p.Path] = p.ID
	}
	out := make([]FleetSiteUpdates, len(views))
	sem := make(chan struct{}, fleetConcurrency)
	var wg sync.WaitGroup
	for i, v := range views {
		out[i] = FleetSiteUpdates{Site: v, ProjectID: projectByPath[v.Site.ProjectPath]}
		if v.Site.ProjectPath == "" {
			out[i].ProjectID = ""
		}
		if !v.HasPassword {
			out[i].Error = "geen Application Password opgeslagen"
			continue
		}
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			u, err := s.AvailableUpdates(id, refresh)
			if err != nil {
				out[i].Error = err.Error()
				return
			}
			out[i].Updates = &u
		}(i, v.Site.ID)
	}
	wg.Wait()
	return out, nil
}
```

- [ ] **Step 4: Run, verwacht PASS**

Run: `go test ./internal/services/ -run 'FleetUpdates|AvailableUpdates' -v -race`
Expected: PASS, geen race.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/services/wpsite_updates.go internal/services/wpsite_updates_test.go
git add internal/services/wpsite_updates.go internal/services/wpsite_updates_test.go
git commit -m "feat: vlootoverzicht van beschikbare WordPress-updates

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Updates uitvoeren (zonder PR)

**Files:**
- Create: `internal/services/wpsite_update_run.go`
- Test: `internal/services/wpsite_update_run_test.go`
- Modify: `internal/services/wpsite_service.go` (velden `runMu`, `running`)

**Interfaces:**
- Consumes: `siteMetSecret`, `bridgeCall`, `bridgeRunTimeout`, `AvailableUpdates`.
- Produces:
  - `type RunItem struct { Name, From, To, Error string; Success bool }` (JSON `name, from, to, error, success`)
  - `type RunResult struct { Items []RunItem; PullRequest, ActionsURL, Warning string }` (JSON `items, pullRequest, actionsUrl, warning`)
  - `func (s *WPSiteService) UpdatePlugins(siteID string, plugins []string, all bool) (RunResult, error)`
  - `func (s *WPSiteService) UpdateCore(siteID string, allowMajor bool) (RunResult, error)`
  - `func (s *WPSiteService) UpdateTranslations(siteID string) (RunResult, error)`
  - `func (s *WPSiteService) claimRun(siteID string) bool`, `releaseRun(siteID string)`
  - `func (s *WPSiteService) afterRun(site domain.WPSite, kind string, res *RunResult)` — in deze taak een no-op; Task 6 vult hem

- [ ] **Step 1: Velden** — in `WPSiteService` (wpsite_service.go) toevoegen:

```go
	runMu   sync.Mutex
	running map[string]bool
```

- [ ] **Step 2: Falende tests** — `internal/services/wpsite_update_run_test.go`:

```go
package services

import (
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestUpdatePluginsBodyEnResultaat(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{"results":[
	 {"plugin":"akismet/akismet.php","success":true,"from":"5.3","to":"5.4"},
	 {"plugin":"kapot/kapot.php","success":false,"from":"1.0","to":"1.0","error":"Download failed."}]}`)
	svc := bridgeSite(t, b, "goed", nil)
	res, err := svc.UpdatePlugins("klant", []string{"akismet/akismet.php", "kapot/kapot.php"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if b.lastPath != "/wp-json/nb-mcp/v1/updates/plugins" || b.lastBody != `{"plugins":["akismet/akismet.php","kapot/kapot.php"]}` {
		t.Fatalf("verzoek %q %q", b.lastPath, b.lastBody)
	}
	want := []RunItem{
		{Name: "akismet/akismet.php", From: "5.3", To: "5.4", Success: true},
		{Name: "kapot/kapot.php", From: "1.0", To: "1.0", Error: "Download failed."},
	}
	if len(res.Items) != 2 || res.Items[0] != want[0] || res.Items[1] != want[1] {
		t.Fatalf("items %+v", res.Items)
	}
}

func TestUpdatePluginsAllGebruiktBeschikbareLijst(t *testing.T) {
	calls := 0
	b := nieuweBridgeServer(t, 200, updatesJSON)
	svc := bridgeSite(t, b, "goed", nil)
	// Eerste call (GET /updates) geeft de lijst; daarna antwoorden we met results.
	orig := b.srv.Config.Handler
	b.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method == http.MethodPost {
			b.respBody = `{"results":[{"plugin":"akismet/akismet.php","success":true,"from":"5.3","to":"5.4"}]}`
		}
		orig.ServeHTTP(w, r)
	})
	res, err := svc.UpdatePlugins("klant", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || b.lastBody != `{"plugins":["akismet/akismet.php"]}` || len(res.Items) != 1 {
		t.Fatalf("calls %d body %q items %+v", calls, b.lastBody, res.Items)
	}
}

func TestUpdatePluginsLeegZonderAll(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{}`)
	svc := bridgeSite(t, b, "goed", nil)
	if _, err := svc.UpdatePlugins("klant", nil, false); err == nil {
		t.Fatal("verwacht fout bij lege selectie")
	}
}

func TestUpdateCoreBodyEnResultaat(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{"success":true,"from":"7.1.1","to":"7.1.2"}`)
	svc := bridgeSite(t, b, "goed", nil)
	res, err := svc.UpdateCore("klant", true)
	if err != nil {
		t.Fatal(err)
	}
	if b.lastPath != "/wp-json/nb-mcp/v1/updates/core" || b.lastBody != `{"allow_major":true}` {
		t.Fatalf("verzoek %q %q", b.lastPath, b.lastBody)
	}
	if len(res.Items) != 1 || res.Items[0] != (RunItem{Name: "WordPress", From: "7.1.1", To: "7.1.2", Success: true}) {
		t.Fatalf("items %+v", res.Items)
	}
}

func TestUpdateCoreMajorGeblokkeerd409(t *testing.T) {
	b := nieuweBridgeServer(t, 409, `{"code":"nb_mcp_major_blocked","message":"Major update 8.0 blocked; pass allow_major."}`)
	svc := bridgeSite(t, b, "goed", nil)
	_, err := svc.UpdateCore("klant", false)
	if err == nil || !strings.Contains(err.Error(), "allow_major") {
		t.Fatalf("fout %v", err)
	}
}

func TestUpdateTranslations(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{"success":false,"count":2,"failed":1,"errors":["Vertaling mislukt."]}`)
	svc := bridgeSite(t, b, "goed", nil)
	res, err := svc.UpdateTranslations("klant")
	if err != nil {
		t.Fatal(err)
	}
	if b.lastPath != "/wp-json/nb-mcp/v1/updates/translations" {
		t.Fatalf("pad %q", b.lastPath)
	}
	if len(res.Items) != 1 || res.Items[0].Success || res.Items[0].Name != "Vertalingen (2 bijgewerkt, 1 mislukt)" || res.Items[0].Error != "Vertaling mislukt." {
		t.Fatalf("items %+v", res.Items)
	}
}

func TestUpdateReadOnlyWeigertZonderNetwerk(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{}`)
	svc := bridgeSite(t, b, "goed", func(s *domain.WPSite) { s.ReadOnly = true })
	for naam, run := range map[string]func() error{
		"plugins": func() error { _, err := svc.UpdatePlugins("klant", []string{"a/a.php"}, false); return err },
		"core":    func() error { _, err := svc.UpdateCore("klant", false); return err },
		"vert":    func() error { _, err := svc.UpdateTranslations("klant"); return err },
	} {
		if err := run(); err == nil || err.Error() != "deze site staat op alleen-lezen" {
			t.Errorf("%s: fout %v", naam, err)
		}
	}
	if b.lastPath != "" {
		t.Fatal("er mocht geen netwerkcall zijn")
	}
}

func TestUpdateDubbeleRunGeweigerd(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{}`)
	svc := bridgeSite(t, b, "goed", nil)
	if !svc.claimRun("klant") {
		t.Fatal("eerste claim moet lukken")
	}
	defer svc.releaseRun("klant")
	if _, err := svc.UpdateCore("klant", false); err == nil || err.Error() != "er loopt al een update voor deze site" {
		t.Fatalf("fout %v", err)
	}
}
```

Voeg `"net/http"` toe aan de imports.

- [ ] **Step 3: Run, verwacht FAIL**

Run: `go test ./internal/services/ -run 'Update(Plugins|Core|Translations|ReadOnly|Dubbele)' -v`
Expected: FAIL — `undefined: RunItem`.

- [ ] **Step 4: Implementeer `internal/services/wpsite_update_run.go`**

```go
package services

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/rdm/sites-tool/internal/domain"
)

type RunItem struct {
	Name    string `json:"name"`
	From    string `json:"from"`
	To      string `json:"to"`
	Error   string `json:"error"`
	Success bool   `json:"success"`
}

type RunResult struct {
	Items       []RunItem `json:"items"`
	PullRequest string    `json:"pullRequest"`
	ActionsURL  string    `json:"actionsUrl"`
	Warning     string    `json:"warning"`
}

func (s *WPSiteService) claimRun(siteID string) bool {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.running[siteID] {
		return false
	}
	if s.running == nil {
		s.running = map[string]bool{}
	}
	s.running[siteID] = true
	return true
}

func (s *WPSiteService) releaseRun(siteID string) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	delete(s.running, siteID)
}

// startRun doet de precheck voor een uitvoering: site, wachtwoord, bridge,
// niet read-only, en geen andere run op dezelfde site. De caller moet
// releaseRun aanroepen als err nil is.
func (s *WPSiteService) startRun(siteID string) (domain.WPSite, string, error) {
	site, err := s.find(siteID)
	if err != nil {
		return domain.WPSite{}, "", err
	}
	if site.ReadOnly {
		return domain.WPSite{}, "", errors.New("deze site staat op alleen-lezen")
	}
	site, pw, err := s.siteMetSecret(siteID)
	if err != nil {
		return domain.WPSite{}, "", err
	}
	if !s.claimRun(siteID) {
		return domain.WPSite{}, "", errors.New("er loopt al een update voor deze site")
	}
	return site, pw, nil
}

func (s *WPSiteService) UpdatePlugins(siteID string, plugins []string, all bool) (RunResult, error) {
	site, pw, err := s.startRun(siteID)
	if err != nil {
		return RunResult{}, err
	}
	defer s.releaseRun(siteID)
	if all {
		u, err := s.AvailableUpdates(siteID, false)
		if err != nil {
			return RunResult{}, err
		}
		plugins = plugins[:0]
		for _, p := range u.Plugins {
			plugins = append(plugins, p.ID)
		}
	}
	if len(plugins) == 0 {
		return RunResult{}, errors.New("geen plugins geselecteerd")
	}
	var raw struct {
		Results []struct {
			Plugin  string `json:"plugin"`
			Success bool   `json:"success"`
			From    string `json:"from"`
			To      string `json:"to"`
			Error   string `json:"error"`
		} `json:"results"`
	}
	if err := s.bridgeCall(site, pw, http.MethodPost, "/updates/plugins", map[string][]string{"plugins": plugins}, bridgeRunTimeout, &raw); err != nil {
		return RunResult{}, err
	}
	res := RunResult{Items: []RunItem{}}
	for _, r := range raw.Results {
		res.Items = append(res.Items, RunItem{Name: r.Plugin, From: r.From, To: r.To, Error: r.Error, Success: r.Success})
	}
	s.afterRun(site, "plugins", &res)
	return res, nil
}

func (s *WPSiteService) UpdateCore(siteID string, allowMajor bool) (RunResult, error) {
	site, pw, err := s.startRun(siteID)
	if err != nil {
		return RunResult{}, err
	}
	defer s.releaseRun(siteID)
	var raw struct {
		Success bool   `json:"success"`
		From    string `json:"from"`
		To      string `json:"to"`
		Error   string `json:"error"`
	}
	if err := s.bridgeCall(site, pw, http.MethodPost, "/updates/core", map[string]bool{"allow_major": allowMajor}, bridgeRunTimeout, &raw); err != nil {
		return RunResult{}, err
	}
	res := RunResult{Items: []RunItem{{Name: "WordPress", From: raw.From, To: raw.To, Error: raw.Error, Success: raw.Success}}}
	s.afterRun(site, "core", &res)
	return res, nil
}

func (s *WPSiteService) UpdateTranslations(siteID string) (RunResult, error) {
	site, pw, err := s.startRun(siteID)
	if err != nil {
		return RunResult{}, err
	}
	defer s.releaseRun(siteID)
	var raw struct {
		Success bool     `json:"success"`
		Count   int      `json:"count"`
		Failed  int      `json:"failed"`
		Errors  []string `json:"errors"`
	}
	if err := s.bridgeCall(site, pw, http.MethodPost, "/updates/translations", nil, bridgeRunTimeout, &raw); err != nil {
		return RunResult{}, err
	}
	name := fmt.Sprintf("Vertalingen (%d bijgewerkt", raw.Count)
	if raw.Failed > 0 {
		name += fmt.Sprintf(", %d mislukt", raw.Failed)
	}
	name += ")"
	return RunResult{Items: []RunItem{{Name: name, Success: raw.Success, Error: strings.Join(raw.Errors, " ")}}}, nil
}

// afterRun koppelt na een uitvoering de PR/Actions-informatie van de
// depotsite. Task 6 vult dit; tot dan doet het niets.
func (s *WPSiteService) afterRun(site domain.WPSite, kind string, res *RunResult) {}
```

- [ ] **Step 5: Run, verwacht PASS**

Run: `go test ./internal/services/ -run 'Update(Plugins|Core|Translations|ReadOnly|Dubbele)|AvailableUpdates|FleetUpdates|WPSite' -v`
Expected: PASS. (Werkt `TestUpdatePluginsAllGebruiktBeschikbareLijst` niet omdat `httptest.Server.Config.Handler` na start niet te vervangen is, bouw dan in die test een eigen `httptest.NewTLSServer` met een handler die bij GET `updatesJSON` en bij POST de results geeft, en onthoud de POST-body.)

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/services/wpsite_update_run.go internal/services/wpsite_update_run_test.go internal/services/wpsite_service.go
git add internal/services/wpsite_update_run.go internal/services/wpsite_update_run_test.go internal/services/wpsite_service.go
git commit -m "feat: plugin-, core- en vertaalupdates uitvoeren via de bridge

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Composer-PR voor de depotsite

**Files:**
- Create: `internal/services/depot_core_pr.go`
- Test: `internal/services/depot_core_pr_test.go`

**Interfaces:**
- Consumes: `coreGit`, `corePulls`, `gitCoreOps{}`, `reWPVersie`, `github.ParseRepoFromRemote`, `github.NewPullClient`, `config.ResolveSecret` (uit `wpcore_update_service.go` / adapters).
- Produces:
  - `type DepotCorePR interface { OpenCorePR(ctx context.Context, projectPath, version string) (string, error); PluginWorkflowURL(ctx context.Context, projectPath string) (string, error) }`
  - `type composerRunner interface { Run(ctx context.Context, dir string, args ...string) (string, error) }`
  - `type depotGit interface { coreGit; StagePathsIn(ctx context.Context, worktreePath string, paths ...string) error }`
  - `func NewDepotCorePR(cfg *config.Global) DepotCorePR`
  - `func newDepotCorePRForTest(git depotGit, pulls corePulls, composer composerRunner, tmpBase string) *depotCorePRMaker`
  - `func ComposerBin() string` (zoekt `RDM_COMPOSER`, PATH, `/opt/homebrew/bin/composer`, `/usr/local/bin/composer`, `~/Library/Application Support/Herd/bin/composer`, login-shell `command -v composer`)
  - `func (gitCoreOps) StagePathsIn(ctx context.Context, worktreePath string, paths ...string) error`

- [ ] **Step 1: Falende tests** — `internal/services/depot_core_pr_test.go`:

```go
package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/adapters/github"
)

type fakeDepotGit struct {
	remote       string
	defaultBr    string
	worktreeDir  string
	addedBranch  string
	addedFrom    string
	removed      bool
	stagedPaths  []string
	committed    string
	pushed       string
}

func (f *fakeDepotGit) DefaultBranchName(context.Context, string) (string, error) { return f.defaultBr, nil }
func (f *fakeDepotGit) RemoteURL(context.Context, string) (string, error)        { return f.remote, nil }
func (f *fakeDepotGit) Fetch(context.Context, string) error                        { return nil }
func (f *fakeDepotGit) PrepareWorktree(context.Context, string, string) error      { return nil }
func (f *fakeDepotGit) AddWorktree(_ context.Context, _, wt, branch, from string) error {
	f.worktreeDir, f.addedBranch, f.addedFrom = wt, branch, from
	return os.MkdirAll(wt, 0o755)
}
func (f *fakeDepotGit) RemoveWorktree(context.Context, string, string) error { f.removed = true; return nil }
func (f *fakeDepotGit) StageAllIn(context.Context, string) error {
	return errors.New("StageAllIn mag niet gebruikt worden")
}
func (f *fakeDepotGit) StagePathsIn(_ context.Context, _ string, paths ...string) error {
	f.stagedPaths = paths
	return nil
}
func (f *fakeDepotGit) CommitIn(_ context.Context, _, msg string) error { f.committed = msg; return nil }
func (f *fakeDepotGit) PushBranch(_ context.Context, _, br string) error { f.pushed = br; return nil }

type fakeComposer struct {
	calls [][]string
	err   error
}

func (f *fakeComposer) Run(_ context.Context, _ string, args ...string) (string, error) {
	f.calls = append(f.calls, args)
	return "", f.err
}

func depotFixture(t *testing.T) (*depotCorePRMaker, *fakeDepotGit, *fakeCorePulls, *fakeComposer) {
	t.Helper()
	g := &fakeDepotGit{remote: "git@github.com:bitfactory-nl/internal-satispressnobearsdev.git", defaultBr: "main"}
	p := &fakeCorePulls{created: &github.PullRequest{Number: 46, HTMLURL: "https://github.com/bitfactory-nl/internal-satispressnobearsdev/pull/46"}}
	c := &fakeComposer{}
	return newDepotCorePRForTest(g, p, c, t.TempDir()), g, p, c
}

func TestOpenCorePRHappyPath(t *testing.T) {
	m, g, p, c := depotFixture(t)
	url, err := m.OpenCorePR(context.Background(), "/repo", "7.2.1")
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://github.com/bitfactory-nl/internal-satispressnobearsdev/pull/46" {
		t.Fatalf("url %q", url)
	}
	if g.addedBranch != "update/wordpress-7.2.1" || g.addedFrom != "origin/main" {
		t.Fatalf("worktree %q van %q", g.addedBranch, g.addedFrom)
	}
	want := [][]string{
		{"require", "roots/wordpress:^7.2", "--no-update", "--no-scripts", "--no-plugins"},
		{"update", "roots/wordpress", "--with-dependencies", "--no-install", "--no-scripts", "--no-plugins"},
	}
	if len(c.calls) != 2 || strings.Join(c.calls[0], " ") != strings.Join(want[0], " ") || strings.Join(c.calls[1], " ") != strings.Join(want[1], " ") {
		t.Fatalf("composer %v", c.calls)
	}
	if strings.Join(g.stagedPaths, ",") != "composer.json,composer.lock" {
		t.Fatalf("staged %v", g.stagedPaths)
	}
	if g.committed != "chore: WordPress naar 7.2.1" || g.pushed != "update/wordpress-7.2.1" || !g.removed {
		t.Fatalf("commit %q push %q removed %v", g.committed, g.pushed, g.removed)
	}
	if p.lastHead != "update/wordpress-7.2.1" || p.lastBase != "main" || p.lastTitle != "WordPress 7.2.1" {
		t.Fatalf("pr %q→%q %q", p.lastHead, p.lastBase, p.lastTitle)
	}
	if _, err := os.Stat(g.worktreeDir); !os.IsNotExist(err) {
		t.Fatal("worktree-map niet opgeruimd")
	}
}

func TestOpenCorePRBestaandePR(t *testing.T) {
	m, g, p, c := depotFixture(t)
	p.open = &github.PullRequest{HTMLURL: "https://github.com/x/y/pull/9"}
	url, err := m.OpenCorePR(context.Background(), "/repo", "7.2.1")
	if err != nil || url != "https://github.com/x/y/pull/9" {
		t.Fatalf("url %q err %v", url, err)
	}
	if len(c.calls) != 0 || g.addedBranch != "" {
		t.Fatal("bij een bestaande PR mag er niets gebouwd worden")
	}
}

func TestOpenCorePRComposerFoutRuimtOp(t *testing.T) {
	m, g, _, c := depotFixture(t)
	c.err = errors.New("Your requirements could not be resolved")
	if _, err := m.OpenCorePR(context.Background(), "/repo", "7.2.1"); err == nil || !strings.Contains(err.Error(), "composer") {
		t.Fatalf("fout %v", err)
	}
	if !g.removed || g.pushed != "" {
		t.Fatalf("removed %v pushed %q", g.removed, g.pushed)
	}
}

func TestOpenCorePRWeigertRareVersie(t *testing.T) {
	m, _, _, _ := depotFixture(t)
	if _, err := m.OpenCorePR(context.Background(), "/repo", "7.2; rm -rf /"); err == nil {
		t.Fatal("verwacht fout")
	}
}

func TestPluginWorkflowURL(t *testing.T) {
	m, _, _, _ := depotFixture(t)
	url, err := m.PluginWorkflowURL(context.Background(), "/repo")
	if err != nil || url != "https://github.com/bitfactory-nl/internal-satispressnobearsdev/actions/workflows/plugin-update.yml" {
		t.Fatalf("url %q err %v", url, err)
	}
}

func TestComposerBinViaEnv(t *testing.T) {
	p := filepath.Join(t.TempDir(), "composer")
	os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("RDM_COMPOSER", p)
	resetComposerBin()
	if got := ComposerBin(); got != p {
		t.Fatalf("got %q", got)
	}
}
```

- [ ] **Step 2: Run, verwacht FAIL**

Run: `go test ./internal/services/ -run 'OpenCorePR|PluginWorkflowURL|ComposerBin' -v`
Expected: FAIL — `undefined: newDepotCorePRForTest`.

- [ ] **Step 3: Implementeer `internal/services/depot_core_pr.go`**

```go
package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/gitcli"
	"github.com/rdm/sites-tool/internal/adapters/github"
	"github.com/rdm/sites-tool/internal/config"
)

// DepotCorePR maakt na een live core-update op de SatisPress-depotsite de
// bijbehorende Composer-PR, en weet waar de plugin-workflow draait.
type DepotCorePR interface {
	OpenCorePR(ctx context.Context, projectPath, version string) (string, error)
	PluginWorkflowURL(ctx context.Context, projectPath string) (string, error)
}

type composerRunner interface {
	Run(ctx context.Context, dir string, args ...string) (string, error)
}

type depotGit interface {
	coreGit
	StagePathsIn(ctx context.Context, worktreePath string, paths ...string) error
}

func (gitCoreOps) StagePathsIn(ctx context.Context, worktreePath string, paths ...string) error {
	_, err := gitcli.Run(ctx, worktreePath, append([]string{"add", "--"}, paths...)...)
	return err
}

type depotCorePRMaker struct {
	git      depotGit
	pulls    func() (corePulls, error)
	composer composerRunner
	tmpBase  string
}

// NewDepotCorePR bouwt de echte implementatie; het GitHub-token wordt pas bij
// gebruik opgehaald (zelfde token als de plugin-repo, zoals WPCoreUpdateService).
func NewDepotCorePR(cfg *config.Global) DepotCorePR {
	return &depotCorePRMaker{
		git: gitCoreOps{},
		pulls: func() (corePulls, error) {
			token, err := config.ResolveSecret(cfg.PluginRepo.GithubToken)
			if err != nil || token == "" {
				return nil, errors.New("github token niet geconfigureerd (zie Instellingen)")
			}
			return github.NewPullClient(token), nil
		},
		composer: execComposer{},
		tmpBase:  filepath.Join(os.TempDir(), "rdm-depot-core"),
	}
}

func newDepotCorePRForTest(git depotGit, pulls corePulls, composer composerRunner, tmpBase string) *depotCorePRMaker {
	return &depotCorePRMaker{git: git, pulls: func() (corePulls, error) { return pulls, nil }, composer: composer, tmpBase: tmpBase}
}

func (m *depotCorePRMaker) ownerRepo(ctx context.Context, repoDir string) (string, string, error) {
	remote, err := m.git.RemoteURL(ctx, repoDir)
	if err != nil {
		return "", "", fmt.Errorf("origin-URL lezen: %w", err)
	}
	return github.ParseRepoFromRemote(remote)
}

func (m *depotCorePRMaker) PluginWorkflowURL(ctx context.Context, projectPath string) (string, error) {
	owner, repo, err := m.ownerRepo(ctx, projectPath)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://github.com/%s/%s/actions/workflows/plugin-update.yml", owner, repo), nil
}

func (m *depotCorePRMaker) OpenCorePR(ctx context.Context, projectPath, version string) (string, error) {
	if !reWPVersie.MatchString(version) {
		return "", fmt.Errorf("ongeldige WordPress-versie %q", version)
	}
	parts := strings.Split(version, ".")
	constraint := "^" + parts[0] + "." + parts[1]
	branch := "update/wordpress-" + version
	title := "WordPress " + version

	owner, repo, err := m.ownerRepo(ctx, projectPath)
	if err != nil {
		return "", err
	}
	pulls, err := m.pulls()
	if err != nil {
		return "", err
	}
	if open, err := pulls.FindOpenPull(ctx, owner, repo, branch); err == nil && open != nil {
		return open.HTMLURL, nil
	}
	if err := m.git.Fetch(ctx, projectPath); err != nil {
		return "", fmt.Errorf("git fetch: %w", err)
	}
	def, err := m.git.DefaultBranchName(ctx, projectPath)
	if err != nil {
		return "", fmt.Errorf("default-branch bepalen: %w", err)
	}
	wt := filepath.Join(m.tmpBase, fmt.Sprintf("%s-wp-%s", filepath.Base(projectPath), version))
	if err := m.git.PrepareWorktree(ctx, projectPath, wt); err != nil {
		return "", fmt.Errorf("oude worktree opruimen: %w", err)
	}
	if err := m.git.AddWorktree(ctx, projectPath, wt, branch, "origin/"+def); err != nil {
		return "", fmt.Errorf("worktree aanmaken: %w", err)
	}
	defer func() {
		_ = m.git.RemoveWorktree(ctx, projectPath, wt)
		_ = os.RemoveAll(wt)
	}()
	if _, err := m.composer.Run(ctx, wt, "require", "roots/wordpress:"+constraint, "--no-update", "--no-scripts", "--no-plugins"); err != nil {
		return "", fmt.Errorf("composer require: %w", err)
	}
	if _, err := m.composer.Run(ctx, wt, "update", "roots/wordpress", "--with-dependencies", "--no-install", "--no-scripts", "--no-plugins"); err != nil {
		return "", fmt.Errorf("composer update: %w", err)
	}
	if err := m.git.StagePathsIn(ctx, wt, "composer.json", "composer.lock"); err != nil {
		return "", fmt.Errorf("stagen: %w", err)
	}
	if err := m.git.CommitIn(ctx, wt, "chore: WordPress naar "+version); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	if err := m.git.PushBranch(ctx, wt, branch); err != nil {
		return "", fmt.Errorf("push: %w", err)
	}
	body := fmt.Sprintf("De live depotsite is via de RDM Sites Tool al bijgewerkt naar WordPress %s.\n\n"+
		"Deze PR zet `roots/wordpress` in composer.json/composer.lock gelijk, zodat de volgende deploy dezelfde versie installeert.", version)
	pr, err := pulls.CreatePull(ctx, owner, repo, branch, def, title, body)
	if err != nil {
		return "", fmt.Errorf("PR aanmaken: %w", err)
	}
	return pr.HTMLURL, nil
}

// execComposer draait de echte composer-binary.
type execComposer struct{}

func (execComposer) Run(ctx context.Context, dir string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, ComposerBin(), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSER_NO_INTERACTION=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		regels := strings.Split(strings.TrimSpace(string(out)), "\n")
		return string(out), fmt.Errorf("%w: %s", err, regels[len(regels)-1])
	}
	return string(out), nil
}

var (
	composerEenmalig sync.Once
	composerGevonden string
)

// ComposerBin zoekt de composer-binary; zelfde reden als docker.DockerBin: een
// vanuit Finder gestarte app erft de shell-PATH niet.
func ComposerBin() string {
	composerEenmalig.Do(func() { composerGevonden = zoekComposer() })
	return composerGevonden
}

func resetComposerBin() { composerEenmalig = sync.Once{} }

func zoekComposer() string {
	if p := strings.TrimSpace(os.Getenv("RDM_COMPOSER")); p != "" {
		return p
	}
	if p, err := exec.LookPath("composer"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{"/opt/homebrew/bin/composer", "/usr/local/bin/composer",
		filepath.Join(home, "Library", "Application Support", "Herd", "bin", "composer")} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, shell, "-ilc", "command -v composer").Output(); err == nil {
		if p := strings.TrimSpace(string(out)); p != "" {
			return p
		}
	}
	return "composer"
}
```

Controleer dat `github.ParseRepoFromRemote` drie waarden teruggeeft (`owner, repo, err`) zoals in `repoVanRemote`; `fakeCorePulls` (uit `wpcore_update_service_test.go`) registreert `lastHead`, `lastBase`, `lastTitle`.

- [ ] **Step 4: Run, verwacht PASS**

Run: `go test ./internal/services/ -run 'OpenCorePR|PluginWorkflowURL|ComposerBin|WPCore' -v`
Expected: PASS (ook de bestaande WPCore-tests: `gitCoreOps` kreeg alleen een extra methode).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/services/depot_core_pr.go internal/services/depot_core_pr_test.go
git add internal/services/depot_core_pr.go internal/services/depot_core_pr_test.go
git commit -m "feat: Composer-PR voor WordPress core op de depotsite

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: PR/Actions-link na een run + app-koppeling + bindings

**Files:**
- Modify: `internal/services/wpsite_service.go` (veld `depot DepotCorePR`, constructor `NewWPSiteServiceWithDepot`)
- Modify: `internal/services/wpsite_update_run.go` (`afterRun`)
- Test: `internal/services/wpsite_update_run_test.go`
- Modify: `internal/app/app.go`
- Generated: `frontend/bindings/**`

**Interfaces:**
- Consumes: `DepotCorePR`, `NewDepotCorePR(cfg)` (Task 5); `RunResult` (Task 4).
- Produces:
  - `func NewWPSiteServiceWithDepot(path string, projects WPProjectLookup, secrets WPSecretStore, depot DepotCorePR) *WPSiteService` (package-functie, geen methode — Wails bindt alleen methodes)
  - `NewWPSiteService` roept die aan met `nil`.
  - Bindings: `WPSiteService.{AvailableUpdates, FleetUpdates, UpdatePlugins, UpdateCore, UpdateTranslations}`, modellen `SiteUpdates`, `CoreUpdateOffer`, `ItemUpdate`, `FleetSiteUpdates`, `RunResult`, `RunItem`, `WPSite.depot`.

- [ ] **Step 1: Falende tests** — toevoegen aan `wpsite_update_run_test.go`:

```go
type fakeDepot struct {
	prURL      string
	prErr      error
	prVersion  string
	prPath     string
	actionsURL string
}

func (f *fakeDepot) OpenCorePR(_ context.Context, path, version string) (string, error) {
	f.prPath, f.prVersion = path, version
	return f.prURL, f.prErr
}
func (f *fakeDepot) PluginWorkflowURL(context.Context, string) (string, error) {
	return f.actionsURL, nil
}

func depotSite(t *testing.T, b *bridgeServer, d *fakeDepot, depot bool) *WPSiteService {
	t.Helper()
	svc, _, projDir := nieuweWPSiteService(t)
	svc.depot = d
	svc.http.Transport = b.srv.Client().Transport
	if _, err := svc.Create(domain.WPSite{ID: "klant", Name: "K", URL: b.srv.URL, Username: "mcp-bot", Bridge: true, Depot: depot, ProjectPath: projDir}, "goed"); err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestUpdateCoreDepotMaaktPR(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{"success":true,"from":"7.1.1","to":"7.2.1"}`)
	d := &fakeDepot{prURL: "https://github.com/x/y/pull/1"}
	svc := depotSite(t, b, d, true)
	res, err := svc.UpdateCore("klant", false)
	if err != nil || res.PullRequest != "https://github.com/x/y/pull/1" || d.prVersion != "7.2.1" || res.Warning != "" {
		t.Fatalf("res %+v err %v depot %+v", res, err, d)
	}
}

func TestUpdateCoreDepotPRFoutWordtWaarschuwing(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{"success":true,"from":"7.1.1","to":"7.2.1"}`)
	svc := depotSite(t, b, &fakeDepot{prErr: errors.New("github token niet geconfigureerd")}, true)
	res, err := svc.UpdateCore("klant", false)
	if err != nil || res.PullRequest != "" || !strings.Contains(res.Warning, "github token niet geconfigureerd") {
		t.Fatalf("res %+v err %v", res, err)
	}
}

func TestUpdateCoreGeenDepotGeenPR(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{"success":true,"from":"7.1.1","to":"7.2.1"}`)
	d := &fakeDepot{prURL: "x"}
	svc := depotSite(t, b, d, false)
	res, _ := svc.UpdateCore("klant", false)
	if res.PullRequest != "" || d.prVersion != "" {
		t.Fatalf("geen PR verwacht: %+v", res)
	}
}

func TestUpdateCoreMisluktGeenPR(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{"success":false,"from":"7.1.1","to":"7.1.1","error":"No core update is available."}`)
	d := &fakeDepot{prURL: "x"}
	svc := depotSite(t, b, d, true)
	res, _ := svc.UpdateCore("klant", false)
	if res.PullRequest != "" || d.prVersion != "" {
		t.Fatalf("geen PR verwacht: %+v", res)
	}
}

func TestUpdatePluginsDepotGeeftActionsURL(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{"results":[{"plugin":"a/a.php","success":true,"from":"1","to":"2"}]}`)
	d := &fakeDepot{actionsURL: "https://github.com/x/y/actions/workflows/plugin-update.yml"}
	svc := depotSite(t, b, d, true)
	res, err := svc.UpdatePlugins("klant", []string{"a/a.php"}, false)
	if err != nil || res.ActionsURL != d.actionsURL {
		t.Fatalf("res %+v err %v", res, err)
	}
}
```

Voeg `"context"` en `"errors"` toe aan de imports.

- [ ] **Step 2: Run, verwacht FAIL**

Run: `go test ./internal/services/ -run 'Depot|ActionsURL|GeenPR' -v`
Expected: FAIL — `svc.depot undefined`.

- [ ] **Step 3: Implementeer**

In `wpsite_service.go`: veld `depot DepotCorePR` in `WPSiteService`, en vervang de constructor door:

```go
func NewWPSiteService(path string, projects WPProjectLookup, secrets WPSecretStore) *WPSiteService {
	return NewWPSiteServiceWithDepot(path, projects, secrets, nil)
}

// NewWPSiteServiceWithDepot geeft de service de hulp voor Composer-PR's op de
// depotsite. Een package-functie, geen methode: Wails bindt alle methodes.
func NewWPSiteServiceWithDepot(path string, projects WPProjectLookup, secrets WPSecretStore, depot DepotCorePR) *WPSiteService {
	return &WPSiteService{
		path: path, projects: projects, secrets: secrets, depot: depot,
		http: &http.Client{Timeout: 15 * time.Second, CheckRedirect: weigerDowngrade},
	}
}
```

In `wpsite_update_run.go`: vervang `afterRun` (imports `"context"`, `"time"` erbij):

```go
// afterRun koppelt de depot-informatie aan het resultaat: bij plugins de
// link naar de workflow die de PR maakt, bij een geslaagde core-update een
// eigen Composer-PR. Een mislukte PR is een waarschuwing, geen fout: de live
// update is dan al gebeurd.
func (s *WPSiteService) afterRun(site domain.WPSite, kind string, res *RunResult) {
	if s.depot == nil || !site.Depot || site.ProjectPath == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	switch kind {
	case "plugins":
		for _, it := range res.Items {
			if it.Success {
				if url, err := s.depot.PluginWorkflowURL(ctx, site.ProjectPath); err == nil {
					res.ActionsURL = url
				}
				return
			}
		}
	case "core":
		if len(res.Items) == 0 || !res.Items[0].Success {
			return
		}
		url, err := s.depot.OpenCorePR(ctx, site.ProjectPath, res.Items[0].To)
		if err != nil {
			res.Warning = "WordPress is live bijgewerkt, maar de Composer-PR is niet gemaakt: " + err.Error()
			return
		}
		res.PullRequest = url
	}
}
```

In `internal/app/app.go` de regel met `NewWPSiteService(...)` vervangen door:

```go
	wpSite := services.NewWPSiteServiceWithDepot(config.WPSitesPath(), project, services.KeychainSecrets{}, services.NewDepotCorePR(&cfg.Global))
```

- [ ] **Step 4: Run, verwacht PASS**

Run: `go test ./internal/... 2>&1 | grep -E "^(FAIL|ok)" | grep -v "^ok"` (leeg = alles ok; gitcli runner_test is bekend flaky) en `go vet ./internal/...`.

- [ ] **Step 5: Bindings**

Run: `cd /Users/jeffreyt/Projects/RDM-Sites-tool-wp-fleet && PATH="$HOME/go/bin:$PATH" task common:generate:bindings`
Controleer: `grep -n "AvailableUpdates\|FleetUpdates\|UpdatePlugins\|UpdateCore\|UpdateTranslations" frontend/bindings/github.com/rdm/sites-tool/internal/services/wpsiteservice.ts` toont alle vijf; `grep -n "class SiteUpdates\|class RunResult\|class FleetSiteUpdates" frontend/bindings/github.com/rdm/sites-tool/internal/services/models.ts` vindt ze; `grep -n "depot" frontend/bindings/github.com/rdm/sites-tool/internal/domain/models.ts`. Controleer ook dat `NewWPSiteServiceWithDepot` NIET in de bindings staat. Daarna `cd frontend && npx tsc --noEmit`.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/services/wpsite_service.go internal/services/wpsite_update_run.go internal/services/wpsite_update_run_test.go internal/app/app.go
git add internal/services/wpsite_service.go internal/services/wpsite_update_run.go internal/services/wpsite_update_run_test.go internal/app/app.go frontend/bindings
git commit -m "feat: Composer-PR en workflow-link na updates op de depotsite

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: `KnopTekst` delen + depot-vinkje in het formulier

**Files:**
- Create: `frontend/src/components/KnopTekst.tsx`
- Modify: `frontend/src/components/WPSiteForm.tsx`

**Interfaces:**
- Produces: `export default function KnopTekst({ tekst, bezig }: { tekst: string; bezig: boolean })`.

- [ ] **Step 1: Verplaats `KnopTekst`** — knip de functie `KnopTekst` (met haar commentaar) uit `WPSiteForm.tsx` en zet hem als default export in `frontend/src/components/KnopTekst.tsx`:

```tsx
// KnopTekst houdt de knop even breed als hij bezig is: de tekst blijft
// (onzichtbaar) staan en de spinner ligt eroverheen. Anders verspringen de
// knoppen ernaast midden in de opacity-overgang, en WebKit laat dan
// spookkopieën van de tekst achter.
export default function KnopTekst({ tekst, bezig }: { tekst: string; bezig: boolean }) {
  return (
    <span className="relative inline-flex items-center justify-center">
      <span className={bezig ? 'invisible' : undefined}>{tekst}</span>
      {bezig && (
        <span className="absolute inset-0 flex items-center justify-center" aria-label="bezig">
          <span className="animate-spin inline-block">↻</span>
        </span>
      )}
    </span>
  )
}
```

en in `WPSiteForm.tsx`: `import KnopTekst from './KnopTekst'`.

- [ ] **Step 2: Depot-vinkje** — in `WPSiteForm.tsx`, direct na het "Alleen lezen"-label:

```tsx
      <label className="flex items-center gap-2 text-[12px] text-fg-muted mb-3">
        <input type="checkbox" checked={!!site.depot} onChange={e => zet('depot', e.target.checked)} />
        Dit is de SatisPress-depotsite (na een core-update maakt de tool een Composer-PR)
      </label>
```

- [ ] **Step 3: Verifieer**

Run: `cd frontend && npx tsc --noEmit && npm run build:dev`
Expected: geen fouten.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/KnopTekst.tsx frontend/src/components/WPSiteForm.tsx
git commit -m "feat: depotsite aanvinken in het WordPress-siteformulier

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: `SiteUpdatesPanel` in Updates-tab en losse-sitepagina

**Files:**
- Create: `frontend/src/components/SiteUpdatesPanel.tsx`
- Modify: `frontend/src/components/UpdatesTab.tsx`
- Modify: `frontend/src/components/WPSitePage.tsx`

**Interfaces:**
- Consumes: bindings `Services.WPSiteService.{ForProject, AvailableUpdates, UpdatePlugins, UpdateCore, UpdateTranslations}`, modellen `WPSiteView`, `SiteUpdates`, `RunResult`; `bevestig`; `KnopTekst`; `Foutvak`.
- Produces: `export default function SiteUpdatesPanel({ view }: { view: WPSiteView })`.

- [ ] **Step 1: Maak `SiteUpdatesPanel.tsx`**

```tsx
import { useCallback, useEffect, useState } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { WPSiteView, SiteUpdates, RunResult } from '../../bindings/github.com/rdm/sites-tool/internal/services/models'
import Foutvak from './Foutvak'
import KnopTekst from './KnopTekst'
import { bevestig } from '../lib/bevestig'
import ExternalLink from './ExternalLink'

type Actie = 'verversen' | 'plugins' | 'alles' | 'core' | 'vertalingen'

const knop = 'bg-accent text-white text-[12px] font-semibold px-3 py-1.5 rounded-lg hover:brightness-110 disabled:opacity-50 transition'
const knopStil = 'border border-border text-fg-muted text-[12px] px-3 py-1.5 rounded-lg hover:bg-hover disabled:opacity-50 transition'

function foutTekst(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

export default function SiteUpdatesPanel({ view }: { view: WPSiteView }) {
  const site = view.site
  const [data, setData] = useState<SiteUpdates | null>(null)
  const [actie, setActie] = useState<Actie | null>(null)
  const [fout, setFout] = useState<string | null>(null)
  const [gekozen, setGekozen] = useState<Set<string>>(new Set())
  const [major, setMajor] = useState(false)
  const [resultaat, setResultaat] = useState<RunResult | null>(null)
  const bezig = actie !== null

  const laad = useCallback(async (refresh: boolean) => {
    setFout(null)
    try {
      const u = await Services.WPSiteService.AvailableUpdates(site.id, refresh)
      setData(u); setGekozen(new Set())
    } catch (e) {
      setFout(foutTekst(e))
    }
  }, [site.id])

  useEffect(() => { setData(null); setResultaat(null); void laad(false) }, [laad])

  const waarschuwing = site.depot
    ? null
    : 'Deze wijziging komt niet in git; de volgende deploy draait hem terug.'

  const voerUit = async (a: Actie, titel: string, uitleg: string, werk: () => Promise<RunResult>) => {
    if (!(await bevestig(titel, [uitleg, waarschuwing].filter(Boolean).join('\n\n')))) return
    setActie(a); setFout(null); setResultaat(null)
    try {
      setResultaat(await werk())
      await laad(false)
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setActie(null)
    }
  }

  const verversen = async () => {
    setActie('verversen')
    await laad(true)
    setActie(null)
  }

  const kanUitvoeren = !site.readOnly && !bezig
  const core = data?.core ?? []
  const plugins = data?.plugins ?? []
  const themes = data?.themes ?? []
  const niksTeDoen = data && core.length === 0 && plugins.length === 0 && themes.length === 0 && data.translations === 0

  return (
    <div className="bg-panel border border-border rounded-xl p-4 mb-6">
      <div className="flex items-center gap-3 mb-3">
        <div className="text-[12px] font-semibold text-fg flex-1">Beschikbaar op de site</div>
        {data?.checkedAt && <span className="text-[11px] text-fg-faint">gecontroleerd {new Date(data.checkedAt).toLocaleString('nl-NL')}</span>}
        <button onClick={verversen} disabled={bezig} className={knopStil}><KnopTekst tekst="Verversen" bezig={actie === 'verversen'} /></button>
      </div>

      {fout && <Foutvak fout={fout} className="mb-3" />}
      {site.readOnly && <p className="text-[11.5px] text-fg-faint mb-3">Deze site staat op alleen-lezen: bijwerken vanuit de tool staat uit.</p>}
      {!data && !fout && <p className="text-[12px] text-fg-faint">Laden…</p>}
      {niksTeDoen && <p className="text-[12px] text-green">Alles is bijgewerkt.</p>}

      {core.length > 0 && (
        <div className="mb-4">
          <p className="text-[11px] font-semibold uppercase tracking-wide text-fg-faint mb-1.5">WordPress</p>
          {core.map(c => (
            <div key={c.version} className="flex items-center gap-2 text-[12px] font-mono text-fg mb-1">
              <span>{c.current} → {c.version}</span>
              <span className={`text-[10px] px-1.5 py-0.5 rounded ${c.type === 'major' ? 'bg-amber-soft text-amber' : 'bg-panel-2 text-fg-muted'}`}>{c.type}</span>
            </div>
          ))}
          <div className="flex items-center gap-3 mt-2">
            <button disabled={!kanUitvoeren} className={knop}
              onClick={() => voerUit('core', 'WordPress bijwerken?',
                site.depot ? 'Daarna maakt de tool een Composer-PR in de gekoppelde repo.' : `WordPress op ${site.name} bijwerken.`,
                () => Services.WPSiteService.UpdateCore(site.id, major))}>
              <KnopTekst tekst="WordPress bijwerken" bezig={actie === 'core'} />
            </button>
            <label className="flex items-center gap-1.5 text-[11.5px] text-fg-muted">
              <input type="checkbox" checked={major} onChange={e => setMajor(e.target.checked)} /> ook major toestaan
            </label>
          </div>
        </div>
      )}

      {plugins.length > 0 && (
        <div className="mb-4">
          <p className="text-[11px] font-semibold uppercase tracking-wide text-fg-faint mb-1.5">Plugins ({plugins.length})</p>
          <table className="w-full text-[12px]">
            <tbody>
              {plugins.map(p => (
                <tr key={p.id} className="border-t border-border">
                  <td className="py-1 w-6">
                    <input type="checkbox" checked={gekozen.has(p.id)} disabled={!kanUitvoeren}
                      onChange={e => setGekozen(prev => {
                        const next = new Set(prev)
                        if (e.target.checked) { next.add(p.id) } else { next.delete(p.id) }
                        return next
                      })} />
                  </td>
                  <td className="py-1 text-fg">{p.name}</td>
                  <td className="py-1 font-mono text-fg-muted text-right">{p.currentVersion} → {p.newVersion}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <div className="flex items-center gap-2 mt-2">
            <button disabled={!kanUitvoeren || gekozen.size === 0} className={knop}
              onClick={() => voerUit('plugins', 'Plugins bijwerken?',
                `${gekozen.size} plugin(s) bijwerken.${site.depot ? ' De PR komt via de SatisPress-workflow op GitHub.' : ''}`,
                () => Services.WPSiteService.UpdatePlugins(site.id, [...gekozen], false))}>
              <KnopTekst tekst="Geselecteerde bijwerken" bezig={actie === 'plugins'} />
            </button>
            <button disabled={!kanUitvoeren} className={knopStil}
              onClick={() => voerUit('alles', 'Alle plugins bijwerken?',
                `Alle ${plugins.length} plugins bijwerken.${site.depot ? ' De PR komt via de SatisPress-workflow op GitHub.' : ''}`,
                () => Services.WPSiteService.UpdatePlugins(site.id, [], true))}>
              <KnopTekst tekst="Alles bijwerken" bezig={actie === 'alles'} />
            </button>
          </div>
        </div>
      )}

      {themes.length > 0 && (
        <div className="mb-4">
          <p className="text-[11px] font-semibold uppercase tracking-wide text-fg-faint mb-1.5">Thema's ({themes.length}) — alleen ter info</p>
          {themes.map(t => (
            <div key={t.id} className="flex text-[12px] py-0.5">
              <span className="text-fg flex-1">{t.name}</span>
              <span className="font-mono text-fg-muted">{t.currentVersion} → {t.newVersion}</span>
            </div>
          ))}
        </div>
      )}

      {data && data.translations > 0 && (
        <div className="flex items-center gap-3 mb-2">
          <span className="text-[12px] text-fg flex-1">{data.translations} vertaling(en) klaar</span>
          <button disabled={!kanUitvoeren} className={knopStil}
            onClick={() => voerUit('vertalingen', 'Vertalingen bijwerken?', 'Alle klaarstaande vertalingen bijwerken.',
              () => Services.WPSiteService.UpdateTranslations(site.id))}>
            <KnopTekst tekst="Vertalingen bijwerken" bezig={actie === 'vertalingen'} />
          </button>
        </div>
      )}

      {resultaat && (
        <div className="mt-3 border-t border-border pt-3 text-[12px]">
          {resultaat.items.map((it, i) => (
            <div key={i} className={it.success ? 'text-green' : 'text-red'}>
              {it.success ? '✓' : '✗'} {it.name} {it.from && `${it.from} → ${it.to}`} {it.error && `— ${it.error}`}
            </div>
          ))}
          {resultaat.pullRequest && <div className="mt-1.5">PR: <ExternalLink href={resultaat.pullRequest}>{resultaat.pullRequest}</ExternalLink></div>}
          {resultaat.actionsUrl && <div className="mt-1.5">De PR wordt gemaakt door de workflow: <ExternalLink href={resultaat.actionsUrl}>bekijk de run</ExternalLink></div>}
          {resultaat.warning && <p className="mt-1.5 text-amber">{resultaat.warning}</p>}
        </div>
      )}
    </div>
  )
}
```

Controleer `ExternalLink`'s props (`grep -n "export default function ExternalLink" -A5 frontend/src/components/ExternalLink.tsx`); gebruik die prop-namen. Bestaat `text-green`/`text-amber`/`bg-amber-soft` niet, neem de classes die elders voor succes/waarschuwing gebruikt worden.

- [ ] **Step 2: UpdatesTab** — hernoem in `UpdatesTab.tsx` de huidige `export default function UpdatesTab(...)` naar `function UpdateBranches(...)` (zelfde props, zelfde body). Voeg een nieuwe default export toe:

```tsx
export default function UpdatesTab(props: Props) {
  const [koppeling, setKoppeling] = useState<WPSiteView | null>(null)
  useEffect(() => {
    let actueel = true
    setKoppeling(null)
    Services.WPSiteService.ForProject(props.projectId)
      .then(v => { if (actueel) setKoppeling(v ?? null) })
      .catch(() => { if (actueel) setKoppeling(null) })
    return () => { actueel = false }
  }, [props.projectId])

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-y-auto">
      {koppeling && (
        <div className="max-w-[960px] px-7 pt-6">
          <SiteUpdatesPanel view={koppeling} />
        </div>
      )}
      <UpdateBranches {...props} />
    </div>
  )
}
```

Imports erbij: `SiteUpdatesPanel`, `type WPSiteView` uit services/models, en `* as Services` uit de services-bindings als die nog niet geïmporteerd is (check de bestaande import-naam in het bestand en hergebruik die). De vroege returns van `UpdateBranches` (laden/fout/geen branches) blijven werken binnen de kolom.

- [ ] **Step 3: WPSitePage** — onder de form-kaart (binnen de scrollcontainer), alleen voor een bestaande site met wachtwoord:

```tsx
        {view && view.hasPassword && (
          <div className="max-w-[760px] mt-4">
            <SiteUpdatesPanel view={view} />
          </div>
        )}
```

met `import SiteUpdatesPanel from './SiteUpdatesPanel'`.

- [ ] **Step 4: Verifieer**

Run: `cd frontend && npx tsc --noEmit && npm run build:dev && grep -rn "window.confirm" src/components | grep -v Tooltip`
Expected: geen fouten, grep leeg.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/SiteUpdatesPanel.tsx frontend/src/components/UpdatesTab.tsx frontend/src/components/WPSitePage.tsx
git commit -m "feat: beschikbare updates tonen en uitvoeren in de Updates-tab

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Pagina "WordPress-vloot"

**Files:**
- Create: `frontend/src/components/FleetUpdatesPage.tsx`
- Modify: `frontend/src/App.tsx` (type `View`, railitem, detailpaneel-tak)

**Interfaces:**
- Consumes: `Services.WPSiteService.FleetUpdates(refresh)`, model `FleetSiteUpdates`; `selectProject(id)` en de losse-site-selectie in `App.tsx`.
- Produces: `export default function FleetUpdatesPage({ onOpenProject, onOpenSite }: { onOpenProject: (projectId: string) => void; onOpenSite: (v: WPSiteView) => void })`.

- [ ] **Step 1: Maak `FleetUpdatesPage.tsx`**

```tsx
import { useCallback, useEffect, useState } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { FleetSiteUpdates, WPSiteView } from '../../bindings/github.com/rdm/sites-tool/internal/services/models'
import Foutvak from './Foutvak'
import KnopTekst from './KnopTekst'

interface Props {
  onOpenProject: (projectId: string) => void
  onOpenSite: (v: WPSiteView) => void
}

function aantal(r: FleetSiteUpdates): number {
  const u = r.updates
  return u ? u.core.length + u.plugins.length + u.themes.length : 0
}

// Sites met updates bovenaan, dan fouten, dan bijgewerkte sites.
function rang(r: FleetSiteUpdates): number {
  if (aantal(r) > 0) return 0
  if (r.error) return 1
  return 2
}

export default function FleetUpdatesPage({ onOpenProject, onOpenSite }: Props) {
  const [rijen, setRijen] = useState<FleetSiteUpdates[] | null>(null)
  const [bezig, setBezig] = useState(false)
  const [fout, setFout] = useState<string | null>(null)

  const laad = useCallback(async (refresh: boolean) => {
    setBezig(true); setFout(null)
    try {
      const r = await Services.WPSiteService.FleetUpdates(refresh)
      setRijen([...r].sort((a, b) => rang(a) - rang(b) || a.site.site.name.localeCompare(b.site.site.name)))
    } catch (e) {
      setFout(e instanceof Error ? e.message : String(e))
    } finally {
      setBezig(false)
    }
  }, [])

  useEffect(() => { void laad(false) }, [laad])

  const open = (r: FleetSiteUpdates) => (r.projectId ? onOpenProject(r.projectId) : onOpenSite(r.site))

  return (
    <div className="flex-1 flex flex-col min-h-0 overflow-hidden">
      <div className="h-[56px] px-[22px] border-b border-border bg-panel shrink-0 flex items-center gap-2">
        <h2 className="text-[15px] font-bold tracking-[-.01em] text-fg flex-1">WordPress-vloot</h2>
        <button onClick={() => laad(true)} disabled={bezig}
          className="border border-border text-fg-muted text-[12px] px-3 py-1.5 rounded-lg hover:bg-hover disabled:opacity-50 transition">
          <KnopTekst tekst="Alles verversen" bezig={bezig} />
        </button>
      </div>
      <div className="flex-1 min-h-0 overflow-y-auto px-6 py-4">
        {fout && <Foutvak fout={fout} className="mb-3" />}
        {rijen && rijen.length === 0 && <p className="text-[12px] text-fg-faint">Nog geen gekoppelde WordPress-sites.</p>}
        {rijen && rijen.length > 0 && (
          <table className="w-full text-[12.5px] bg-panel border border-border rounded-xl overflow-hidden">
            <thead>
              <tr className="text-left text-[11px] uppercase tracking-wide text-fg-faint">
                <th className="px-3 py-2">Site</th>
                <th className="px-3 py-2">Core</th>
                <th className="px-3 py-2">Plugins</th>
                <th className="px-3 py-2">Thema's</th>
                <th className="px-3 py-2">Gecontroleerd</th>
                <th className="px-3 py-2">Status</th>
              </tr>
            </thead>
            <tbody>
              {rijen.map(r => (
                <tr key={r.site.site.id} onClick={() => open(r)} className="border-t border-border hover:bg-hover cursor-pointer">
                  <td className="px-3 py-2 text-fg">{r.site.site.name}{r.site.site.depot && <span className="ml-1.5 text-[10px] text-accent">depot</span>}</td>
                  <td className="px-3 py-2 font-mono">{r.updates ? (r.updates.core[0]?.version ?? '—') : ''}</td>
                  <td className="px-3 py-2 font-mono">{r.updates ? r.updates.plugins.length : ''}</td>
                  <td className="px-3 py-2 font-mono">{r.updates ? r.updates.themes.length : ''}</td>
                  <td className="px-3 py-2 text-fg-faint">{r.updates?.checkedAt ? new Date(r.updates.checkedAt).toLocaleString('nl-NL') : ''}</td>
                  <td className={`px-3 py-2 ${r.error ? 'text-red' : aantal(r) > 0 ? 'text-amber' : 'text-green'}`}>
                    {r.error || (aantal(r) > 0 ? `${aantal(r)} update(s)` : 'bijgewerkt')}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: App.tsx**
  1. `type View` uitbreiden met `'fleet'`.
  2. Import: `import FleetUpdatesPage from './components/FleetUpdatesPage'`.
  3. Railitem direct na het `WordPress`-item:

```tsx
          <NavItem
            icon={<GlobeIcon size={15} />}
            label="WordPress-vloot"
            active={view === 'fleet'}
            onClick={() => setView('fleet')}
          />
```

  4. Detailpaneel-tak vóór `) : view === 'orgsync' ? (`:

```tsx
      ) : view === 'fleet' ? (
        <ErrorBoundary label="WordPress-vloot error">
          <FleetUpdatesPage
            onOpenProject={id => { setView('projects'); selectProject(id) }}
            onOpenSite={v => { setView('projects'); setSelected(null); setWpSelected(v) }}
          />
        </ErrorBoundary>
```

Na `selectProject(id)` moet de Updates-tab actief worden als dat simpel kan (controleer of `ProjectDetail` een initiële tab-prop heeft; zo niet: laat het bij het openen van het project en noteer dat in het rapport — geen nieuwe prop-plumbing in deze taak).

- [ ] **Step 3: Verifieer**

Run: `cd frontend && npx tsc --noEmit && npm run build:dev`
Expected: geen fouten.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/FleetUpdatesPage.tsx frontend/src/App.tsx
git commit -m "feat: pagina WordPress-vloot met updates over alle sites

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: Handmatige verificatie op de SatisPress-site

**Files:** geen (tenzij reparatie nodig).

De gebruiker test in de draaiende dev-app (`task dev` in de worktree; na Go-wijzigingen de dev-server volledig herstarten — de watcher herbouwt Go niet betrouwbaar).

- [ ] **Step 1:** `go test ./internal/...` en `cd frontend && npx tsc --noEmit && npm run build:dev` groen.
- [ ] **Step 2:** Dev-server herstarten (vraag de gebruiker of doe het via de eigen terminaltab), app openen.
- [ ] **Step 3:** web-satispress → Projectinstellingen → vinkje "depotsite" aan → Opslaan.
- [ ] **Step 4:** Updates-tab toont "Beschikbaar op de site" met de actuele lijst; **Verversen** werkt.
- [ ] **Step 5 (met de gebruiker, bevestiging vooraf):** één plugin bijwerken → resultaat ✓ + link naar de Actions-run; in GitHub verschijnt een `plugin-update/<slug>-<versie>`-PR.
- [ ] **Step 6 (alleen als er een core-update klaarstaat, met de gebruiker):** WordPress bijwerken → ✓ + link naar een PR `update/wordpress-<versie>` die alleen `composer.json`/`composer.lock` wijzigt.
- [ ] **Step 7:** WordPress-vloot toont de SatisPress-site; een site zonder bridge/wachtwoord toont de fout in zijn eigen regel.
- [ ] **Step 8:** Rapporteer per stap de uitkomst.
