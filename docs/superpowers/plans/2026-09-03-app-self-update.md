# Zelf-update van de app — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De app controleert bij het opstarten, elke 6 uur en op verzoek of er een nieuwere release op GitHub staat, vraagt de gebruiker of die geïnstalleerd mag worden, vervangt zichzelf via een helper-script inclusief de nabewerkingscommando's, en laat na de herstart zien wat er veranderd is.

**Architecture:** Een nieuw `internal/version`-pakket geeft de app versiebesef via ldflags. Een `ReleaseClient` in `internal/adapters/github` leest de laatste release en downloadt de asset. Een `UpdateService` in `internal/services` combineert die twee met een state-bestand, een achtergrondloop en Wails-events; de installatie zet alles klaar in een tempmap en draagt de eigenlijke swap over aan een gegenereerd bash-script dat wacht tot de app is afgesloten. De frontend krijgt één dialog (popup, voortgang en "wat is er nieuw" in één component), een badge in de rail en een sectie in Instellingen.

**Tech Stack:** Go 1.25 (alleen stdlib plus de al aanwezige `gopkg.in/yaml.v3`), Wails v3, React 19 + TypeScript + Tailwind, GitHub REST API v2022-11-28, bash + macOS `ditto`/`xattr`/`open`, GitHub Actions.

## Global Constraints

- Doelplatform is uitsluitend macOS; `ditto`, `xattr` en `open` mogen zonder fallback gebruikt worden.
- Geen nieuwe Go-dependencies. HTTP met `net/http`, JSON met `encoding/json`.
- Commentaar in de code en alle UI-teksten en foutmeldingen in het Nederlands, zoals in de nieuwere bestanden van deze repo (`internal/services/org_sync_service.go` is de referentie voor toon en detailniveau).
- De app-identiteit wordt `nl.nobears.kinsta-updater`; `companyName` wordt `No Bears`, copyright `(c) 2026, No Bears`. De oude identiteit `nl.micromanage.rdm-sites-tool` mag na dit plan alleen nog voorkomen als `legacyKeychainService`.
- Eigen repo staat als constante in de code: `bitfactory-nl/internal-kinsta-updates`. Geen instelling.
- Release-asset heet `RDM-Sites-Tool-<tag>-macOS.zip`; selectie gebeurt op het suffix `-macOS.zip`, niet op de volledige naam.
- Zelf-update is uit wanneer `version.Version` gelijk is aan `dev` of het uitvoerbare bestand niet in een `.app`-bundle staat.
- Bestanden blijven klein en gericht: geen bestand boven ~300 regels, splits op verantwoordelijkheid.
- Draai tijdens het werken aan dit plan geen `task dev` naast `go test` of `task package`: die herbouwen `bin/` gelijktijdig en dat levert een codesign-race op.
- Voer `go test ./...` uit vanuit de repo-root; frontend-typecheck met `cd frontend && npx tsc --noEmit`.

---

### Task 1: Versiebesef via ldflags

**Files:**
- Create: `internal/version/version.go`
- Create: `internal/version/compare.go`
- Test: `internal/version/compare_test.go`
- Modify: `main.go:11-19` (begin van `main`)
- Modify: `build/darwin/Taskfile.yml:30-46` (`build:native`, `vars.BUILD_FLAGS`)

**Interfaces:**
- Consumes: niets.
- Produces: `version.Version string` (variabele), `version.IsDev() bool`, `version.IsNewer(candidate, current string) bool`.

- [ ] **Step 1: Write the failing test**

Create `internal/version/compare_test.go`:

```go
package version

import "testing"

func TestIsNewer(t *testing.T) {
	cases := []struct {
		naam      string
		candidate string
		current   string
		want      bool
	}{
		{"patch hoger", "v0.2.10", "v0.2.9", true},
		{"minor hoger", "v0.3.0", "v0.2.99", true},
		{"major hoger", "v1.0.0", "v0.99.99", true},
		{"gelijk", "v0.2.9", "v0.2.9", false},
		{"ouder", "v0.2.8", "v0.2.9", false},
		{"zonder v-prefix werkt ook", "0.2.10", "0.2.9", true},
		{"prefix gemengd", "v0.2.10", "0.2.9", true},
		{"pre-release is ouder dan release", "v0.3.0-rc1", "v0.3.0", false},
		{"release is nieuwer dan pre-release", "v0.3.0", "v0.3.0-rc1", true},
		{"pre-release van hogere versie is nieuwer", "v0.4.0-rc1", "v0.3.0", true},
		{"dev als huidige versie updatet nooit", "v9.9.9", "dev", false},
		{"onparseerbare kandidaat", "latest", "v0.2.9", false},
		{"te weinig componenten", "v0.3", "v0.2.9", false},
		{"lege kandidaat", "", "v0.2.9", false},
		{"letters in een component", "v0.a.1", "v0.2.9", false},
	}

	for _, c := range cases {
		t.Run(c.naam, func(t *testing.T) {
			if got := IsNewer(c.candidate, c.current); got != c.want {
				t.Errorf("IsNewer(%q, %q) = %v, wil %v", c.candidate, c.current, got, c.want)
			}
		})
	}
}

func TestIsDev(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })

	Version = "dev"
	if !IsDev() {
		t.Error("IsDev() = false voor \"dev\", wil true")
	}
	Version = ""
	if !IsDev() {
		t.Error("IsDev() = false voor een lege versie, wil true")
	}
	Version = "v0.2.9"
	if IsDev() {
		t.Error("IsDev() = true voor v0.2.9, wil false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/version/ -v`
Expected: FAIL — `no Go files in .../internal/version` (het pakket bestaat nog niet).

- [ ] **Step 3: Write minimal implementation**

Create `internal/version/version.go`:

```go
// Package version houdt bij welke versie van de app draait. Version wordt bij
// het bouwen gestempeld met de git-tag; blijft dat achterwege, dan is dit een
// lokale build en staat zelf-update uit.
package version

// Version is de versie van deze build, gezet via
// -ldflags "-X github.com/rdm/sites-tool/internal/version.Version=v0.3.0".
var Version = "dev"

// IsDev meldt of dit een build zonder versiestempel is. Zulke builds bieden
// geen updates aan: ze zouden zichzelf vervangen door een release waarvan niet
// vast te stellen is of die nieuwer is dan de code die nu draait.
func IsDev() bool {
	return Version == "" || Version == "dev"
}
```

Create `internal/version/compare.go`:

```go
package version

import (
	"strconv"
	"strings"
)

// parsed is een ontlede versie: de drie nummers plus een eventueel
// pre-release-suffix ("rc1" uit "v0.3.0-rc1").
type parsed struct {
	nums [3]int
	pre  string
}

// parse ontleedt "v0.3.1", "0.3.1" of "0.3.1-rc2". Een v-prefix is optioneel.
// Alles wat niet uit precies drie niet-negatieve getallen bestaat, geldt als
// onparseerbaar.
func parse(v string) (parsed, bool) {
	s := strings.TrimPrefix(strings.TrimSpace(v), "v")
	if s == "" {
		return parsed{}, false
	}

	var p parsed
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		p.pre = s[i+1:]
		s = s[:i]
	}

	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return parsed{}, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return parsed{}, false
		}
		p.nums[i] = n
	}
	return p, true
}

// IsNewer meldt of candidate een hogere versie is dan current. Onparseerbare
// invoer aan welke kant dan ook geeft false: een versie die we niet begrijpen —
// waaronder "dev" als huidige versie — is nooit een reden om een update aan te
// bieden. Bij gelijke nummers telt een pre-release lager dan de bijbehorende
// release, dus v0.3.0 is nieuwer dan v0.3.0-rc1 en niet andersom.
func IsNewer(candidate, current string) bool {
	c, ok := parse(candidate)
	if !ok {
		return false
	}
	cur, ok := parse(current)
	if !ok {
		return false
	}

	for i := 0; i < 3; i++ {
		if c.nums[i] != cur.nums[i] {
			return c.nums[i] > cur.nums[i]
		}
	}
	return c.pre == "" && cur.pre != ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/version/ -v`
Expected: PASS — alle subtests van `TestIsNewer` en `TestIsDev`.

- [ ] **Step 5: Voeg `--version` toe aan main.go**

Zo is de ldflags-stempel van buitenaf controleerbaar. Wijzig het begin van `main()` in `main.go`; de bestaande imports `embed` en `log` blijven staan, `fmt`, `os` en het version-pakket komen erbij:

```go
import (
	"embed"
	"fmt"
	"log"
	"os"

	"github.com/rdm/sites-tool/internal/app"
	"github.com/rdm/sites-tool/internal/version"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Zonder venster te openen de versie melden: hiermee is te controleren of de
	// build daadwerkelijk een versiestempel heeft meegekregen.
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(version.Version)
		return
	}

	cfg, err := app.LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
```

- [ ] **Step 6: Verifieer de ldflags-stempel**

Run:
```bash
go build -ldflags "-X github.com/rdm/sites-tool/internal/version.Version=v9.9.9" -o /tmp/versiecheck . && /tmp/versiecheck --version && go build -o /tmp/versiecheck-dev . && /tmp/versiecheck-dev --version
```
Expected: eerst `v9.9.9`, daarna `dev`.

- [ ] **Step 7: Geef de Taskfile de versie mee**

In `build/darwin/Taskfile.yml`, taak `build:native`, blok `vars:`. Voeg `VERSION_FLAG` toe en neem die op in beide takken van `BUILD_FLAGS`:

```yaml
    vars:
      # De versie komt uit APP_VERSION (in CI de git-tag). Zonder die env-var
      # blijft het "dev" en biedt de build geen updates aan.
      VERSION_FLAG:
        sh: 'echo "-X github.com/rdm/sites-tool/internal/version.Version=${APP_VERSION:-dev}"'
      BUILD_FLAGS: '{{if eq .DEV "true"}}{{if .EXTRA_TAGS}}-tags {{.EXTRA_TAGS}} {{end}}-buildvcs=false -gcflags=all="-l" -ldflags="{{.VERSION_FLAG}}"{{else}}-tags production{{if .EXTRA_TAGS}},{{.EXTRA_TAGS}}{{end}} -trimpath -buildvcs=false -ldflags="-w -s {{.VERSION_FLAG}}"{{end}}'
      DEFAULT_OUTPUT: '{{.BIN_DIR}}/{{.APP_NAME}}'
      OUTPUT: '{{ .OUTPUT | default .DEFAULT_OUTPUT }}'
```

De cross-compile via Docker (`build:docker`) krijgt deze vlag niet: releases worden op een macOS-runner gebouwd en lopen dus altijd via `build:native`. Dat is bewust en hoeft niet opgelost te worden.

- [ ] **Step 8: Verifieer dat de Taskfile-build nog werkt**

Run: `APP_VERSION=v9.9.9 wails3 task build && ./bin/rdm-sites-tool --version`
Expected: de build slaagt en print `v9.9.9`.

- [ ] **Step 9: Commit**

```bash
git add internal/version main.go build/darwin/Taskfile.yml
git commit -m "feat: versiebesef via ldflags plus --version vlag"
```

---

### Task 2: App-identiteit naar nl.nobears.kinsta-updater, met keychain-migratie

**Files:**
- Modify: `internal/config/keychain.go` (volledige herschrijving, blijft klein)
- Test: `internal/config/keychain_test.go`
- Modify: `internal/app/app.go:16-22` (`LoadConfig`)
- Modify: `build/config.yml:8-12`
- Modify: `build/darwin/Info.plist:10-11,16-17,25`
- Modify: `SPEC.md:862`

**Interfaces:**
- Consumes: niets.
- Produces: `config.MigrateKeychainService() []string` — geeft de accounts terug die zijn overgezet.

- [ ] **Step 1: Write the failing test**

Create `internal/config/keychain_test.go`:

```go
package config

import (
	"errors"
	"fmt"
	"testing"
)

// nepKeychain vervangt de macOS security-CLI: een map per service-naam, zodat
// de migratie te testen is zonder de echte keychain van de ontwikkelaar aan te
// raken (die immers prompts kan opleveren en per machine verschilt).
type nepKeychain struct {
	items map[string]map[string]string // service -> account -> waarde
	sets  int
}

func newNepKeychain() *nepKeychain {
	return &nepKeychain{items: map[string]map[string]string{}}
}

func (n *nepKeychain) get(service, account string) (string, error) {
	v, ok := n.items[service][account]
	if !ok {
		return "", errors.New("niet gevonden")
	}
	return v, nil
}

func (n *nepKeychain) set(service, account, waarde string) error {
	if n.items[service] == nil {
		n.items[service] = map[string]string{}
	}
	n.items[service][account] = waarde
	n.sets++
	return nil
}

// installeerNep hangt de nepkeychain aan de hooks en zet ze na de test terug.
func installeerNep(t *testing.T, n *nepKeychain) {
	t.Helper()
	origGet, origSet := getFromService, setInService
	getFromService, setInService = n.get, n.set
	t.Cleanup(func() { getFromService, setInService = origGet, origSet })
}

func TestMigrateKeychainServiceZetOudeKeysOver(t *testing.T) {
	n := newNepKeychain()
	_ = n.set(legacyKeychainService, "rdm.kinsta.apiKey", "kinsta-geheim")
	_ = n.set(legacyKeychainService, "rdm.github.token", "ghp_oud")
	n.sets = 0
	installeerNep(t, n)

	migrated := MigrateKeychainService()

	if len(migrated) != 2 {
		t.Fatalf("migrated = %v, wil 2 accounts", migrated)
	}
	if got := n.items[keychainService]["rdm.kinsta.apiKey"]; got != "kinsta-geheim" {
		t.Errorf("kinsta-key onder nieuwe service = %q, wil %q", got, "kinsta-geheim")
	}
	if got := n.items[keychainService]["rdm.github.token"]; got != "ghp_oud" {
		t.Errorf("github-token onder nieuwe service = %q, wil %q", got, "ghp_oud")
	}
	// De oude items blijven staan: verwijderen kan een keychain-prompt geven.
	if got := n.items[legacyKeychainService]["rdm.kinsta.apiKey"]; got != "kinsta-geheim" {
		t.Errorf("oude kinsta-key = %q, wil ongemoeid", got)
	}
}

func TestMigrateKeychainServiceLaatNieuweWaardeStaan(t *testing.T) {
	n := newNepKeychain()
	_ = n.set(legacyKeychainService, "rdm.kinsta.apiKey", "oud")
	_ = n.set(keychainService, "rdm.kinsta.apiKey", "nieuw")
	installeerNep(t, n)

	if migrated := MigrateKeychainService(); len(migrated) != 0 {
		t.Fatalf("migrated = %v, wil leeg", migrated)
	}
	if got := n.items[keychainService]["rdm.kinsta.apiKey"]; got != "nieuw" {
		t.Errorf("waarde = %q, wil de bestaande %q", got, "nieuw")
	}
}

func TestMigrateKeychainServiceZonderOudeKeys(t *testing.T) {
	n := newNepKeychain()
	installeerNep(t, n)

	if migrated := MigrateKeychainService(); len(migrated) != 0 {
		t.Fatalf("migrated = %v, wil leeg", migrated)
	}
	if n.sets != 0 {
		t.Errorf("aantal writes = %d, wil 0", n.sets)
	}
}

func TestMigrateKeychainServiceIsIdempotent(t *testing.T) {
	n := newNepKeychain()
	_ = n.set(legacyKeychainService, "rdm.wordfence.apiKey", "wf")
	installeerNep(t, n)

	eerste := MigrateKeychainService()
	tweede := MigrateKeychainService()

	if len(eerste) != 1 {
		t.Fatalf("eerste ronde = %v, wil 1 account", eerste)
	}
	if len(tweede) != 0 {
		t.Fatalf("tweede ronde = %v, wil leeg", tweede)
	}
}

func TestMigratedAccountsDekkenAlleServiceNamen(t *testing.T) {
	// Vangt de fout waarbij later een nieuwe keychain-key wordt toegevoegd
	// zonder die in de migratielijst op te nemen.
	wil := []string{
		"rdm.kinsta.apiKey",
		"rdm.github.token",
		"rdm.anthropic.apiKey",
		"rdm.wordfence.apiKey",
	}
	if len(migratedAccounts) != len(wil) {
		t.Fatalf("migratedAccounts = %v, wil %v", migratedAccounts, wil)
	}
	for i, acct := range wil {
		if migratedAccounts[i] != acct {
			t.Errorf("migratedAccounts[%d] = %q, wil %q", i, migratedAccounts[i], acct)
		}
	}
}

func TestKeychainServiceNamen(t *testing.T) {
	if keychainService != "nl.nobears.kinsta-updater" {
		t.Errorf("keychainService = %q, wil nl.nobears.kinsta-updater", keychainService)
	}
	if legacyKeychainService != "nl.micromanage.rdm-sites-tool" {
		t.Errorf("legacyKeychainService = %q, wil de oude naam", legacyKeychainService)
	}
	fmt.Sprint(keychainService) // houdt de import in gebruik bij aanpassingen
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run Keychain -v`
Expected: FAIL — `undefined: getFromService`, `undefined: legacyKeychainService`, `undefined: MigrateKeychainService`, `undefined: migratedAccounts`.

- [ ] **Step 3: Write minimal implementation**

Vervang de inhoud van `internal/config/keychain.go`:

```go
package config

import (
	"fmt"
	"os/exec"
	"strings"
)

// keychainService is de service-naam waaronder de tool zijn secrets bewaart.
const keychainService = "nl.nobears.kinsta-updater"

// legacyKeychainService is de service-naam die de tool tot en met v0.2.9
// gebruikte, gebaseerd op een privédomein. MigrateKeychainService haalt
// bestaande keys daar eenmalig weg zodat niemand zijn API-keys opnieuw hoeft in
// te vullen.
const legacyKeychainService = "nl.micromanage.rdm-sites-tool"

// migratedAccounts zijn alle keychain-accounts die de tool zelf schrijft. Komt
// er een nieuwe key bij, dan hoort die hier ook in — anders raakt hij bij een
// volgende naamswijziging verloren.
var migratedAccounts = []string{
	"rdm.kinsta.apiKey",
	"rdm.github.token",
	"rdm.anthropic.apiKey",
	"rdm.wordfence.apiKey",
}

// Hooks op de security-CLI, zodat de migratie te testen is zonder de echte
// keychain aan te raken.
var (
	getFromService = keychainGetFrom
	setInService   = keychainSetIn
)

func keychainGetFrom(service, account string) (string, error) {
	out, err := exec.Command(
		"security", "find-generic-password",
		"-s", service,
		"-a", account,
		"-w",
	).Output()
	if err != nil {
		return "", fmt.Errorf("keychain get %q: %w", account, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func keychainSetIn(service, account, password string) error {
	// -U werkt bij als het item al bestaat, en voegt het anders toe.
	if err := exec.Command(
		"security", "add-generic-password",
		"-s", service,
		"-a", account,
		"-w", password,
		"-U",
	).Run(); err != nil {
		return fmt.Errorf("keychain set %q: %w", account, err)
	}
	return nil
}

func keychainGet(account string) (string, error) {
	return getFromService(keychainService, account)
}

func KeychainSet(account, password string) error {
	return setInService(keychainService, account, password)
}

func KeychainDelete(account string) error {
	if err := exec.Command(
		"security", "delete-generic-password",
		"-s", keychainService,
		"-a", account,
	).Run(); err != nil {
		return fmt.Errorf("keychain delete %q: %w", account, err)
	}
	return nil
}

// MigrateKeychainService zet secrets die nog onder de oude service-naam staan
// over naar de nieuwe, en geeft terug welke accounts zijn overgezet. Best
// effort en idempotent: een account dat onder de nieuwe naam al een waarde
// heeft blijft ongemoeid, en fouten worden stil overgeslagen — een mislukte
// migratie mag het opstarten nooit blokkeren.
//
// De oude items worden bewust niet verwijderd: een delete op de keychain kan om
// toestemming vragen, en zo'n prompt tijdens het opstarten is een slechtere
// ervaring dan een achtergebleven item dat de gebruiker desgewenst zelf in
// Keychain Access opruimt.
func MigrateKeychainService() []string {
	var migrated []string
	for _, account := range migratedAccounts {
		if v, err := getFromService(keychainService, account); err == nil && v != "" {
			continue
		}
		v, err := getFromService(legacyKeychainService, account)
		if err != nil || v == "" {
			continue
		}
		if err := setInService(keychainService, account, v); err != nil {
			continue
		}
		migrated = append(migrated, account)
	}
	return migrated
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS — de nieuwe keychain-tests en de bestaande tests in dit pakket.

- [ ] **Step 5: Roep de migratie aan bij het opstarten**

In `internal/app/app.go`, functie `LoadConfig`:

```go
func LoadConfig() (Config, error) {
	// Eenmalig: secrets die nog onder de oude service-naam staan overzetten.
	// Best effort — bij een lege lijst is er simpelweg niets te migreren.
	if migrated := config.MigrateKeychainService(); len(migrated) > 0 {
		log.Printf("keychain: %d secret(s) overgezet naar de nieuwe service-naam: %v", len(migrated), migrated)
	}

	g, err := config.LoadGlobal()
	if err != nil {
		return Config{}, err
	}
	return Config{Global: g}, nil
}
```

Voeg `"log"` toe aan de imports van `internal/app/app.go`.

- [ ] **Step 6: Trek de app-identiteit recht**

In `build/config.yml`, blok `info`:

```yaml
info:
  companyName: "No Bears" # The name of the company
  productName: "Kinsta Updater" # The name of the application
  productIdentifier: "nl.nobears.kinsta-updater" # The unique product identifier
  description: "Git & deployment dashboard" # The application description
  copyright: "(c) 2026, No Bears" # Copyright text
```

In `build/darwin/Info.plist` — pas `CFBundleIdentifier` en het copyright aan. Bewerk dit bestand met de hand en draai níet `wails3 task common:update:build-assets`: dat genereert alle build-assets opnieuw en overschrijft ook het icoon en de dev-plist.

```xml
        <key>CFBundleIdentifier</key>
            <string>nl.nobears.kinsta-updater</string>
        <key>CFBundleVersion</key>
            <string>0.2.9</string>
        <key>CFBundleGetInfoString</key>
            <string>Git &amp; deployment dashboard</string>
        <key>CFBundleShortVersionString</key>
            <string>0.2.9</string>
```

en onderaan:

```xml
        <key>NSHumanReadableCopyright</key>
            <string>© 2026, No Bears</string>
```

De versienummers hier zijn de bodem: Task 10 stempelt bij elke release het echte nummer erover.

- [ ] **Step 7: Werk de documentatie bij**

In `SPEC.md:862` staat de oude service-naam. Vervang `nl.micromanage.rdm-sites-tool` door `nl.nobears.kinsta-updater` en voeg één zin toe:

```
Use `github.com/zalando/go-keyring`. Service name: `nl.nobears.kinsta-updater`. Keys die nog onder de oude naam `nl.micromanage.rdm-sites-tool` staan, worden bij het opstarten eenmalig overgezet door `config.MigrateKeychainService()`. On first run, user pastes Kinsta API key and GitHub token; both stored in Keychain. Config references them as `keychain:rdm.kinsta.apiKey`.
```

- [ ] **Step 8: Controleer dat de oude naam nergens anders meer staat**

Run: `grep -rni "micromanage" --exclude-dir=.git --exclude-dir=node_modules --exclude-dir=bin --exclude-dir=dist . | grep -v "legacyKeychainService\|docs/2026-09-03\|SPEC.md:862\|keychain_test.go"`
Expected: geen uitvoer.

- [ ] **Step 9: Run the full test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/config/keychain.go internal/config/keychain_test.go internal/app/app.go build/config.yml build/darwin/Info.plist SPEC.md
git commit -m "refactor: app-identiteit naar nl.nobears.kinsta-updater met keychain-migratie"
```

---

### Task 3: Domeinmodel en het state-bestand

**Files:**
- Create: `internal/domain/update.go`
- Create: `internal/services/update_state.go`
- Test: `internal/services/update_state_test.go`

**Interfaces:**
- Consumes: niets.
- Produces:
  - `domain.UpdateStatus{CurrentVersion string; Enabled, AutoCheck bool; LastCheck time.Time; LastError string; Available *domain.AvailableUpdate}`
  - `domain.AvailableUpdate{Version string; Changes []domain.ChangeEntry; Skipped bool; SizeBytes int64}`
  - `domain.ChangeEntry{Kind, Text string}` met `domain.ChangeNieuw`, `domain.ChangeOpgelost`, `domain.ChangeOverig`
  - `domain.UpdateProgress{Phase string; Done, Total int64}` met `domain.PhaseDownload`, `domain.PhaseUitpakken`, `domain.PhaseVervangen`
  - `services.updateState` (intern), `services.loadUpdateState(path string) (updateState, error)`, `services.saveUpdateState(path string, st updateState) error`, `services.DefaultUpdateStatePath() string`

- [ ] **Step 1: Write the failing test**

Create `internal/services/update_state_test.go`:

```go
package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestLoadUpdateStateOntbrekendBestand(t *testing.T) {
	st, err := loadUpdateState(filepath.Join(t.TempDir(), "bestaat-niet.json"))
	if err != nil {
		t.Fatalf("loadUpdateState: %v", err)
	}
	if st.SkippedVersion != "" || !st.LastCheck.IsZero() {
		t.Errorf("state = %+v, wil een lege state", st)
	}
}

func TestSaveEnLoadUpdateStateRondrit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "update-state.json")
	wil := updateState{
		LastCheck:        time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC),
		SkippedVersion:   "v0.2.10",
		LastRunVersion:   "v0.2.9",
		InstalledVersion: "v0.2.10",
		InstalledChanges: []domain.ChangeEntry{{Kind: domain.ChangeNieuw, Text: "Zelf-update"}},
		InstallLog:       "/tmp/update.log",
	}

	if err := saveUpdateState(path, wil); err != nil {
		t.Fatalf("saveUpdateState: %v", err)
	}

	got, err := loadUpdateState(path)
	if err != nil {
		t.Fatalf("loadUpdateState: %v", err)
	}
	if !got.LastCheck.Equal(wil.LastCheck) {
		t.Errorf("LastCheck = %v, wil %v", got.LastCheck, wil.LastCheck)
	}
	if got.SkippedVersion != wil.SkippedVersion || got.LastRunVersion != wil.LastRunVersion {
		t.Errorf("versies = %+v, wil %+v", got, wil)
	}
	if len(got.InstalledChanges) != 1 || got.InstalledChanges[0].Text != "Zelf-update" {
		t.Errorf("InstalledChanges = %+v, wil één regel", got.InstalledChanges)
	}
}

func TestSaveUpdateStateLaatGeenTempbestandenAchter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update-state.json")

	if err := saveUpdateState(path, updateState{SkippedVersion: "v1.0.0"}); err != nil {
		t.Fatalf("saveUpdateState: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "update-state.json" {
		var namen []string
		for _, e := range entries {
			namen = append(namen, e.Name())
		}
		t.Errorf("map bevat %v, wil alleen update-state.json", namen)
	}
}

func TestLoadUpdateStateKapotteJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-state.json")
	if err := os.WriteFile(path, []byte("{dit is geen json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := loadUpdateState(path); err == nil {
		t.Error("loadUpdateState gaf geen fout bij kapotte JSON")
	}
}

func TestDefaultUpdateStatePath(t *testing.T) {
	got := DefaultUpdateStatePath()
	if filepath.Base(got) != "update-state.json" {
		t.Errorf("pad = %q, wil eindigen op update-state.json", got)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("pad = %q, wil een absoluut pad", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/ -run UpdateState -v`
Expected: FAIL — `undefined: loadUpdateState`, `undefined: updateState`, `undefined: DefaultUpdateStatePath`.

- [ ] **Step 3: Write the domain types**

Create `internal/domain/update.go`:

```go
package domain

import "time"

// Soorten wijzigingen zoals ze in de release-notes gegroepeerd staan.
const (
	ChangeNieuw    = "nieuw"
	ChangeOpgelost = "opgelost"
	ChangeOverig   = "overig"
)

// Fasen van een installatie, gebruikt in UpdateProgress.Phase.
const (
	PhaseDownload   = "download"
	PhaseUitpakken  = "uitpakken"
	PhaseVervangen  = "vervangen"
)

// ChangeEntry is één regel uit de release-notes.
type ChangeEntry struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// AvailableUpdate beschrijft een release die nieuwer is dan de draaiende versie.
type AvailableUpdate struct {
	Version   string        `json:"version"`
	Changes   []ChangeEntry `json:"changes"`
	Skipped   bool          `json:"skipped"`
	SizeBytes int64         `json:"sizeBytes"`
}

// UpdateStatus is wat de frontend nodig heeft om de badge, de popup en de
// sectie in Instellingen te tonen.
type UpdateStatus struct {
	CurrentVersion string           `json:"currentVersion"`
	Enabled        bool             `json:"enabled"`
	AutoCheck      bool             `json:"autoCheck"`
	LastCheck      time.Time        `json:"lastCheck"`
	LastError      string           `json:"lastError"`
	Available      *AvailableUpdate `json:"available"`
}

// UpdateProgress wordt tijdens het installeren naar de frontend gestuurd.
// Total is 0 wanneer de omvang van een fase niet bekend is.
type UpdateProgress struct {
	Phase string `json:"phase"`
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}
```

- [ ] **Step 4: Write the state store**

Create `internal/services/update_state.go`:

```go
package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

// updateState is de runtime-state van de zelf-update: wat er is gecontroleerd,
// wat de gebruiker heeft weggeklikt, en welke changelog bij de laatst
// geïnstalleerde versie hoort. Dit is bewust geen gebruikersinstelling en staat
// daarom niet in config.yml.
type updateState struct {
	LastCheck        time.Time            `json:"last_check"`
	SkippedVersion   string               `json:"skipped_version"`
	LastRunVersion   string               `json:"last_run_version"`
	InstalledVersion string               `json:"installed_version"`
	InstalledChanges []domain.ChangeEntry `json:"installed_changes"`
	InstallLog       string               `json:"install_log"`
}

// DefaultUpdateStatePath is ~/.config/rdm/update-state.json.
func DefaultUpdateStatePath() string {
	home, err := os.UserHomeDir()
	return defaultUpdateStatePathFrom(home, err)
}

// defaultUpdateStatePathFrom bouwt het pad uit een (home, err) paar zoals
// os.UserHomeDir() dat teruggeeft. Nooit terugvallen op een relatief pad: de
// cwd van een .app-bundle is onvoorspelbaar.
func defaultUpdateStatePathFrom(home string, err error) string {
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "rdm", "update-state.json")
	}
	return filepath.Join(home, ".config", "rdm", "update-state.json")
}

// loadUpdateState leest de state. Een ontbrekend bestand is geen fout: dan is
// er nog nooit gecontroleerd.
func loadUpdateState(path string) (updateState, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return updateState{}, nil
	}
	if err != nil {
		return updateState{}, fmt.Errorf("update-state lezen: %w", err)
	}

	var st updateState
	if err := json.Unmarshal(data, &st); err != nil {
		return updateState{}, fmt.Errorf("update-state parsen: %w", err)
	}
	return st, nil
}

// saveUpdateState schrijft eerst naar een tijdelijk bestand in dezelfde map en
// hernoemt dat vervolgens. os.Rename is atomair binnen hetzelfde filesystem,
// dus een crash halverwege laat het bestaande bestand ongemoeid in plaats van
// afgekapte JSON achter te laten.
func saveUpdateState(path string, st updateState) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("update-state map aanmaken: %w", err)
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("update-state serialiseren: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".update-state-*.tmp")
	if err != nil {
		return fmt.Errorf("tijdelijk update-state bestand aanmaken: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op als het renamen lukte

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("update-state schrijven: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("update-state sluiten: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("rechten op update-state zetten: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("update-state plaatsen: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/services/ -run UpdateState -v`
Expected: PASS — vijf tests.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/update.go internal/services/update_state.go internal/services/update_state_test.go
git commit -m "feat: domeinmodel en state-bestand voor zelf-update"
```

---

### Task 4: Release-client voor GitHub

**Files:**
- Create: `internal/adapters/github/releases.go`
- Test: `internal/adapters/github/releases_test.go`

**Interfaces:**
- Consumes: niets.
- Produces:
  - `github.Release{TagName, Body string; Asset ReleaseAsset}`
  - `github.ReleaseAsset{ID int64; Name string; Size int64}`
  - `github.NewReleaseClient(token, repo string) *ReleaseClient`
  - `(*ReleaseClient).LatestRelease(ctx context.Context) (Release, error)`
  - `(*ReleaseClient).DownloadAsset(ctx context.Context, assetID int64, w io.Writer, onProgress func(done, total int64)) error`
  - `github.ErrNoMacOSAsset error`

- [ ] **Step 1: Write the failing test**

Create `internal/adapters/github/releases_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/adapters/github/ -run Release -v`
Expected: FAIL — `undefined: ReleaseClient`, `undefined: NewReleaseClient`, `undefined: ErrNoMacOSAsset`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/adapters/github/releases.go`:

```go
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// macOSAssetSuffix is waar de app-zip in een release aan te herkennen is.
	// Op het suffix matchen en niet op de volledige naam, want die bevat de tag.
	macOSAssetSuffix = "-macOS.zip"

	// maxReleaseBodyBytes begrenst het gelezen JSON-antwoord. Release-bodies zijn
	// hooguit enkele kilobytes; een kapot antwoord mag geen onbegrensd geheugen
	// opslokken.
	maxReleaseBodyBytes = 1 << 20 // 1 MB

	// progressStep is hoe vaak de voortgangscallback tijdens een download
	// afgaat: elke 256 KB is genoeg voor een vloeiende balk zonder de
	// event-bus te overladen.
	progressStep = 256 << 10
)

// ErrNoMacOSAsset betekent dat de release bestaat maar geen macOS-zip bevat —
// bijvoorbeeld doordat de build-workflow is gefaald nadat de tag was gepusht.
var ErrNoMacOSAsset = errors.New("github: release heeft geen macOS-asset")

// ReleaseAsset is een bestand dat aan een release hangt.
type ReleaseAsset struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// Release is een GitHub-release met de asset die de macOS-app bevat.
type Release struct {
	TagName string
	Body    string
	Asset   ReleaseAsset
}

// releaseJSON is de vorm waarin de REST API een release teruggeeft.
type releaseJSON struct {
	TagName string         `json:"tag_name"`
	Body    string         `json:"body"`
	Assets  []ReleaseAsset `json:"assets"`
}

// ReleaseClient leest releases van één repository. Los van Client, die aan de
// plugin-repo en de contents-API gebonden is.
type ReleaseClient struct {
	token   string
	repo    string // "org/repo-name"
	baseURL string
	http    *http.Client
}

// NewReleaseClient bouwt een client voor repo (formaat "org/repo-name").
func NewReleaseClient(token, repo string) *ReleaseClient {
	return &ReleaseClient{
		token:   token,
		repo:    repo,
		baseURL: defaultBaseURL,
		// Ruim genoeg voor een zip van ~12 MB op een matige verbinding.
		http: &http.Client{Timeout: 10 * time.Minute},
	}
}

// LatestRelease haalt de nieuwste niet-draft, niet-prerelease release op en
// zoekt daarin de macOS-asset.
func (c *ReleaseClient) LatestRelease(ctx context.Context) (Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", c.baseURL, c.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, fmt.Errorf("release-request bouwen: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("release ophalen: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Release{}, fmt.Errorf("github api: status %d bij het ophalen van de laatste release van %s", resp.StatusCode, c.repo)
	}

	var rj releaseJSON
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxReleaseBodyBytes)).Decode(&rj); err != nil {
		return Release{}, fmt.Errorf("release parsen: %w", err)
	}

	asset, ok := pickMacOSAsset(rj.Assets)
	if !ok {
		return Release{}, fmt.Errorf("%w (tag %s)", ErrNoMacOSAsset, rj.TagName)
	}
	return Release{TagName: rj.TagName, Body: rj.Body, Asset: asset}, nil
}

// pickMacOSAsset kiest de asset die de app-bundle bevat.
func pickMacOSAsset(assets []ReleaseAsset) (ReleaseAsset, bool) {
	for _, a := range assets {
		if strings.HasSuffix(a.Name, macOSAssetSuffix) {
			return a, true
		}
	}
	return ReleaseAsset{}, false
}

// DownloadAsset streamt een release-asset naar w. onProgress mag nil zijn; is
// hij gezet, dan wordt hij elke 256 KB en één keer aan het eind aangeroepen met
// het aantal geschreven bytes en de totale omvang (0 als de server die niet
// meldt).
func (c *ReleaseClient) DownloadAsset(ctx context.Context, assetID int64, w io.Writer, onProgress func(done, total int64)) error {
	url := fmt.Sprintf("%s/repos/%s/releases/assets/%d", c.baseURL, c.repo, assetID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("download-request bouwen: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	// De API antwoordt met een redirect naar de opslaglocatie; http.Client volgt
	// die zelf.
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("asset downloaden: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github api: status %d bij het downloaden van asset %d", resp.StatusCode, assetID)
	}

	total := resp.ContentLength
	if total < 0 {
		total = 0
	}

	var done, sindsMelding int64
	buf := make([]byte, 64<<10)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("asset wegschrijven: %w", writeErr)
			}
			done += int64(n)
			sindsMelding += int64(n)
			if onProgress != nil && sindsMelding >= progressStep {
				onProgress(done, total)
				sindsMelding = 0
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("asset lezen: %w", readErr)
		}
	}

	if onProgress != nil {
		onProgress(done, total)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/adapters/github/ -v`
Expected: PASS — de zes nieuwe release-tests plus de bestaande tests van dit pakket.

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/github/releases.go internal/adapters/github/releases_test.go
git commit -m "feat: release-client die de laatste release en de macOS-asset leest"
```

---

### Task 5: Changelog uit de release-body

**Files:**
- Create: `internal/services/update_notes.go`
- Test: `internal/services/update_notes_test.go`

**Interfaces:**
- Consumes: `domain.ChangeEntry`, `domain.ChangeNieuw`, `domain.ChangeOpgelost`, `domain.ChangeOverig` (Task 3).
- Produces: `services.parseChangelog(body string) []domain.ChangeEntry`.

- [ ] **Step 1: Write the failing test**

Create `internal/services/update_notes_test.go`:

```go
package services

import (
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestParseChangelogGroepeertPerKop(t *testing.T) {
	body := "## Wijzigingen\n" +
		"\n" +
		"### Nieuw\n" +
		"- Zelf-update van de app\n" +
		"- Badge in de sidebar\n" +
		"\n" +
		"### Opgelost\n" +
		"- Versie bleef op de oude waarde staan\n" +
		"\n" +
		"### Overig\n" +
		"- Afhankelijkheden bijgewerkt\n" +
		"\n" +
		"## Installatie\n" +
		"\n" +
		"1. Download de zip\n"

	got := parseChangelog(body)

	wil := []domain.ChangeEntry{
		{Kind: domain.ChangeNieuw, Text: "Zelf-update van de app"},
		{Kind: domain.ChangeNieuw, Text: "Badge in de sidebar"},
		{Kind: domain.ChangeOpgelost, Text: "Versie bleef op de oude waarde staan"},
		{Kind: domain.ChangeOverig, Text: "Afhankelijkheden bijgewerkt"},
	}
	if len(got) != len(wil) {
		t.Fatalf("aantal regels = %d, wil %d (%+v)", len(got), len(wil), got)
	}
	for i := range wil {
		if got[i] != wil[i] {
			t.Errorf("regel %d = %+v, wil %+v", i, got[i], wil[i])
		}
	}
}

func TestParseChangelogStoptBijDeVolgendeH2(t *testing.T) {
	body := "## Wijzigingen\n\n### Nieuw\n- Eerste\n\n## Installatie\n\n### Nieuw\n- Niet meenemen\n"

	got := parseChangelog(body)

	if len(got) != 1 || got[0].Text != "Eerste" {
		t.Errorf("regels = %+v, wil alleen \"Eerste\"", got)
	}
}

func TestParseChangelogZonderWijzigingenSectie(t *testing.T) {
	// Alle releases tot en met v0.2.9 hebben alleen installatie-instructies.
	body := "## Installatie\n\n1. Download `RDM-Sites-Tool-v0.2.9-macOS.zip`\n2. Pak uit\n"

	if got := parseChangelog(body); len(got) != 0 {
		t.Errorf("regels = %+v, wil leeg", got)
	}
}

func TestParseChangelogLegeBody(t *testing.T) {
	if got := parseChangelog(""); len(got) != 0 {
		t.Errorf("regels = %+v, wil leeg", got)
	}
}

func TestParseChangelogRegelsZonderKopWordenOverig(t *testing.T) {
	body := "## Wijzigingen\n\n- Losse regel zonder subkop\n"

	got := parseChangelog(body)

	if len(got) != 1 || got[0].Kind != domain.ChangeOverig || got[0].Text != "Losse regel zonder subkop" {
		t.Errorf("regels = %+v, wil één overig-regel", got)
	}
}

func TestParseChangelogNegeertLegeBulletsEnSterretjes(t *testing.T) {
	body := "## Wijzigingen\n\n### Nieuw\n* Met een sterretje\n-\n-    \n- Met streepje\n"

	got := parseChangelog(body)

	if len(got) != 2 {
		t.Fatalf("regels = %+v, wil 2", got)
	}
	if got[0].Text != "Met een sterretje" || got[1].Text != "Met streepje" {
		t.Errorf("regels = %+v", got)
	}
}

func TestParseChangelogIsNietGevoeligVoorHoofdlettersEnCRLF(t *testing.T) {
	body := "## wijzigingen\r\n\r\n### NIEUW\r\n- Werkt ook zo\r\n"

	got := parseChangelog(body)

	if len(got) != 1 || got[0].Kind != domain.ChangeNieuw || got[0].Text != "Werkt ook zo" {
		t.Errorf("regels = %+v, wil één nieuw-regel", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/ -run ParseChangelog -v`
Expected: FAIL — `undefined: parseChangelog`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/services/update_notes.go`:

```go
package services

import (
	"strings"

	"github.com/rdm/sites-tool/internal/domain"
)

// changelogKoppen verbindt de subkoppen uit de release-notes met de soorten uit
// het domeinmodel. De sleutels staan in kleine letters; de vergelijking is
// hoofdletterongevoelig.
var changelogKoppen = map[string]string{
	"nieuw":    domain.ChangeNieuw,
	"opgelost": domain.ChangeOpgelost,
	"overig":   domain.ChangeOverig,
}

// parseChangelog haalt de "## Wijzigingen"-sectie uit een release-body en zet
// de bullets om in ChangeEntry's, gegroepeerd op de subkop waaronder ze staan.
// Regels vóór de eerste subkop tellen als "overig". Ontbreekt de sectie — zoals
// in alle releases tot en met v0.2.9, die alleen installatie-instructies
// bevatten — dan is het resultaat leeg en meldt de UI dat er geen details zijn.
func parseChangelog(body string) []domain.ChangeEntry {
	var (
		entries []domain.ChangeEntry
		inSectie bool
		kind     = domain.ChangeOverig
	)

	for _, ruw := range strings.Split(body, "\n") {
		regel := strings.TrimSpace(strings.TrimSuffix(ruw, "\r"))

		if strings.HasPrefix(regel, "## ") {
			// Een nieuwe H2 opent de sectie, of sluit hem weer (bijvoorbeeld
			// "## Installatie" dat op de wijzigingen volgt).
			inSectie = strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(regel, "## ")), "wijzigingen")
			kind = domain.ChangeOverig
			continue
		}
		if !inSectie {
			continue
		}

		if strings.HasPrefix(regel, "### ") {
			kop := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(regel, "### ")))
			if k, ok := changelogKoppen[kop]; ok {
				kind = k
			} else {
				kind = domain.ChangeOverig
			}
			continue
		}

		if !strings.HasPrefix(regel, "- ") && !strings.HasPrefix(regel, "* ") {
			continue
		}
		tekst := strings.TrimSpace(regel[2:])
		if tekst == "" {
			continue
		}
		entries = append(entries, domain.ChangeEntry{Kind: kind, Text: tekst})
	}

	return entries
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/services/ -run ParseChangelog -v`
Expected: PASS — zeven tests.

- [ ] **Step 5: Commit**

```bash
git add internal/services/update_notes.go internal/services/update_notes_test.go
git commit -m "feat: changelog-parser voor de wijzigingen-sectie uit release-notes"
```


---

### Task 6: UpdateService met achtergrondloop en wiring

**Files:**
- Create: `internal/services/update_service.go`
- Create: `internal/services/update_check.go`
- Test: `internal/services/update_service_test.go`
- Modify: `internal/config/schema.go` (nieuw `Updates`-veld op `Global`)
- Modify: `internal/config/global.go:62-90` (`defaultGlobal`, `applyDefaults`)
- Modify: `internal/app/app.go` (`Services`, `NewServices`, `Wails`)
- Modify: `main.go` (`SetApp` + `Start`)

**Interfaces:**
- Consumes: `version.Version`, `version.IsNewer` (Task 1); `updateState`, `loadUpdateState`, `saveUpdateState`, `DefaultUpdateStatePath` (Task 3); `github.NewReleaseClient`, `github.Release`, `github.ReleaseAsset` (Task 4); `parseChangelog` (Task 5); `eventEmitter` uit `internal/services/ssh_service.go:18`.
- Produces:
  - `config.UpdatesGlobal{AutoCheck *bool; GithubToken string}` met methode `AutoCheckEnabled() bool`, bereikbaar als `cfg.Updates`
  - `services.NewUpdateService(cfg *config.Global) *UpdateService`
  - `(*UpdateService).SetApp(app *application.App)`, `.Start()`, `.Stop()`
  - `(*UpdateService).Status() domain.UpdateStatus`, `.Check() (domain.UpdateStatus, error)`, `.Skip(version string) error`, `.WhatsNew() *domain.AvailableUpdate`, `.InstallLog() (string, error)`
  - intern voor Task 7: veld `s.asset github.ReleaseAsset`, `s.bundlePath`, `s.logDir`, `s.newClient`, `s.token()`, `s.emitProgress(domain.UpdateProgress)`, `bundlePathFor(exe string) string`, `DefaultUpdateLogDir() string`

- [ ] **Step 1: Voeg de configuratie-sectie toe**

In `internal/config/schema.go`: veld op `Global` en het nieuwe type erbij.

```go
type Global struct {
	ProjectsRoots []string        `yaml:"projects_roots"`
	Editor        string          `yaml:"editor"` // cursor | vscode | phpstorm
	DBApp         string          `yaml:"db_app"` // Sequel Ace | TablePlus | ... (optioneel)
	Kinsta        KinstaGlobal    `yaml:"kinsta"`
	PluginRepo    PluginRepo      `yaml:"plugin_repo"`
	Notifications Notifications   `yaml:"notifications"`
	Git           GitGlobal       `yaml:"git"`
	AI            AIGlobal        `yaml:"ai"`
	Wordfence     WordfenceGlobal `yaml:"wordfence"`
	Updates       UpdatesGlobal   `yaml:"updates"`
}

// UpdatesGlobal regelt de zelf-update van de tool.
type UpdatesGlobal struct {
	// AutoCheck is een pointer zodat een config.yml zonder updates-sectie —
	// wat elke bestaande installatie is — niet als "uitgezet" wordt gelezen.
	// applyDefaults vult nil aan met true.
	AutoCheck *bool `yaml:"auto_check"`

	// GithubToken is optioneel: leeg betekent dat het token van de plugin-repo
	// wordt gebruikt. Formaat als elders: keychain:rdm.github.token of een
	// literal (alleen voor dev).
	GithubToken string `yaml:"github_token,omitempty"`
}

// AutoCheckEnabled meldt of automatisch controleren aan staat; niet ingevuld
// betekent aan.
func (u UpdatesGlobal) AutoCheckEnabled() bool {
	return u.AutoCheck == nil || *u.AutoCheck
}
```

In `internal/config/global.go`, in `defaultGlobal()` binnen de `Global{...}`-literal:

```go
		PluginRepo:    PluginRepo{Ref: "main"},
		Updates:       UpdatesGlobal{AutoCheck: boolPtr(true)},
```

en onderaan `applyDefaults`:

```go
	if g.Updates.AutoCheck == nil {
		g.Updates.AutoCheck = boolPtr(true)
	}
}

// boolPtr is een hulpje voor optionele yaml-booleans, waar nil "niet ingevuld"
// betekent en dus iets anders is dan false.
func boolPtr(b bool) *bool { return &b }
```

- [ ] **Step 2: Write the failing test**

Create `internal/services/update_service_test.go`:

```go
package services

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/github"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// nepFetcher vervangt de GitHub-client.
type nepFetcher struct {
	rel     github.Release
	relErr  error
	aanroep int
}

func (n *nepFetcher) LatestRelease(context.Context) (github.Release, error) {
	n.aanroep++
	return n.rel, n.relErr
}

func (n *nepFetcher) DownloadAsset(context.Context, int64, io.Writer, func(int64, int64)) error {
	return errors.New("niet gebruikt in deze test")
}

// nepEmitter legt de events vast die de service uitstuurt.
type nepEmitter struct {
	events []string
	data   []any
}

func (n *nepEmitter) Emit(name string, data ...any) bool {
	n.events = append(n.events, name)
	if len(data) > 0 {
		n.data = append(n.data, data[0])
	}
	return true
}

// testService bouwt een UpdateService die niets van de echte omgeving
// aanraakt: eigen state-pad in een tempmap, een nep-bundle-pad zodat de service
// zich "geïnstalleerd" waant, en een injecteerbare client.
func testService(t *testing.T, huidig string, f *nepFetcher) (*UpdateService, *nepEmitter) {
	t.Helper()
	dir := t.TempDir()
	em := &nepEmitter{}
	autoAan := true
	s := &UpdateService{
		cfg: &config.Global{
			PluginRepo: config.PluginRepo{GithubToken: "ghp_test"},
			Updates:    config.UpdatesGlobal{AutoCheck: &autoAan},
		},
		statePath:  filepath.Join(dir, "update-state.json"),
		logDir:     filepath.Join(dir, "logs"),
		bundlePath: filepath.Join(dir, "Kinsta Updater.app"),
		current:    huidig,
		emitter:    em,
		newClient:  func(string, string) releaseFetcher { return f },
	}
	return s, em
}

func nieuweRelease(tag, body string) github.Release {
	return github.Release{
		TagName: tag,
		Body:    body,
		Asset:   github.ReleaseAsset{ID: 42, Name: "RDM-Sites-Tool-" + tag + "-macOS.zip", Size: 12230392},
	}
}

func TestCheckVindtNieuwereVersieEnEmitEvent(t *testing.T) {
	f := &nepFetcher{rel: nieuweRelease("v0.2.10", "## Wijzigingen\n\n### Nieuw\n- Zelf-update\n")}
	s, em := testService(t, "v0.2.9", f)

	status, err := s.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if status.Available == nil {
		t.Fatal("Available = nil, wil v0.2.10")
	}
	if status.Available.Version != "v0.2.10" {
		t.Errorf("versie = %q, wil v0.2.10", status.Available.Version)
	}
	if status.Available.SizeBytes != 12230392 {
		t.Errorf("SizeBytes = %d", status.Available.SizeBytes)
	}
	if len(status.Available.Changes) != 1 || status.Available.Changes[0].Kind != domain.ChangeNieuw {
		t.Errorf("Changes = %+v, wil één nieuw-regel", status.Available.Changes)
	}
	if status.LastCheck.IsZero() {
		t.Error("LastCheck is niet gezet")
	}
	if len(em.events) != 1 || em.events[0] != "updates:available" {
		t.Errorf("events = %v, wil één updates:available", em.events)
	}
	if s.asset.ID != 42 {
		t.Errorf("asset = %+v, wil id 42 bewaard voor de installatie", s.asset)
	}
}

func TestCheckZonderNieuwereVersie(t *testing.T) {
	f := &nepFetcher{rel: nieuweRelease("v0.2.9", "")}
	s, em := testService(t, "v0.2.9", f)

	status, err := s.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if status.Available != nil {
		t.Errorf("Available = %+v, wil nil", status.Available)
	}
	if len(em.events) != 0 {
		t.Errorf("events = %v, wil geen", em.events)
	}
}

func TestCheckMeldtOvergeslagenVersieZonderEvent(t *testing.T) {
	f := &nepFetcher{rel: nieuweRelease("v0.2.10", "")}
	s, em := testService(t, "v0.2.9", f)

	if err := s.Skip("v0.2.10"); err != nil {
		t.Fatalf("Skip: %v", err)
	}

	status, err := s.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if status.Available == nil || !status.Available.Skipped {
		t.Fatalf("Available = %+v, wil Skipped true (de badge blijft, de popup niet)", status.Available)
	}
	if len(em.events) != 0 {
		t.Errorf("events = %v, wil geen popup-event voor een overgeslagen versie", em.events)
	}
}

func TestCheckSkipGeldtNietVoorEenNogNieuwereVersie(t *testing.T) {
	s, em := testService(t, "v0.2.9", &nepFetcher{rel: nieuweRelease("v0.2.10", "")})
	if err := s.Skip("v0.2.10"); err != nil {
		t.Fatalf("Skip: %v", err)
	}

	s.newClient = func(string, string) releaseFetcher {
		return &nepFetcher{rel: nieuweRelease("v0.2.11", "")}
	}
	status, err := s.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if status.Available == nil || status.Available.Skipped {
		t.Fatalf("Available = %+v, wil v0.2.11 niet overgeslagen", status.Available)
	}
	if len(em.events) != 1 {
		t.Errorf("events = %v, wil één event voor de nieuwere versie", em.events)
	}
}

func TestCheckBewaartFoutZonderEvent(t *testing.T) {
	f := &nepFetcher{relErr: errors.New("status 401")}
	s, em := testService(t, "v0.2.9", f)

	if _, err := s.Check(); err == nil {
		t.Fatal("Check gaf geen fout")
	}
	status := s.Status()
	if status.LastError == "" {
		t.Error("LastError is leeg, wil de foutmelding voor Instellingen")
	}
	if len(em.events) != 0 {
		t.Errorf("events = %v, wil geen popup bij een mislukte check", em.events)
	}
}

func TestCheckZonderTokenGeeftDuidelijkeFout(t *testing.T) {
	f := &nepFetcher{rel: nieuweRelease("v0.2.10", "")}
	s, _ := testService(t, "v0.2.9", f)
	s.cfg.PluginRepo.GithubToken = ""

	_, err := s.Check()
	if !errors.Is(err, errGeenUpdateToken) {
		t.Fatalf("err = %v, wil errGeenUpdateToken", err)
	}
	if f.aanroep != 0 {
		t.Error("er is een API-aanroep gedaan zonder token")
	}
}

func TestCheckGebruiktTokenOverride(t *testing.T) {
	var gotToken string
	f := &nepFetcher{rel: nieuweRelease("v0.2.10", "")}
	s, _ := testService(t, "v0.2.9", f)
	s.cfg.Updates.GithubToken = "ghp_override"
	s.newClient = func(token, repo string) releaseFetcher {
		gotToken = token
		return f
	}

	if _, err := s.Check(); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if gotToken != "ghp_override" {
		t.Errorf("token = %q, wil de override", gotToken)
	}
}

func TestCheckIsUitInDevBuild(t *testing.T) {
	f := &nepFetcher{rel: nieuweRelease("v9.9.9", "")}
	s, em := testService(t, "dev", f)

	status, err := s.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if status.Enabled {
		t.Error("Enabled = true in een dev-build")
	}
	if f.aanroep != 0 {
		t.Error("een dev-build heeft de API aangeroepen")
	}
	if len(em.events) != 0 {
		t.Errorf("events = %v, wil geen", em.events)
	}
}

func TestCheckIsUitBuitenEenAppBundle(t *testing.T) {
	f := &nepFetcher{rel: nieuweRelease("v0.2.10", "")}
	s, _ := testService(t, "v0.2.9", f)
	s.bundlePath = ""

	status, _ := s.Check()
	if status.Enabled {
		t.Error("Enabled = true zonder .app-bundle")
	}
	if f.aanroep != 0 {
		t.Error("de API is aangeroepen zonder .app-bundle")
	}
}

func TestWhatsNewNaEenGeslaagdeUpdate(t *testing.T) {
	s, _ := testService(t, "v0.2.10", &nepFetcher{})
	st := updateState{
		LastRunVersion:   "v0.2.9",
		InstalledVersion: "v0.2.10",
		InstalledChanges: []domain.ChangeEntry{{Kind: domain.ChangeNieuw, Text: "Zelf-update"}},
	}
	if err := saveUpdateState(s.statePath, st); err != nil {
		t.Fatal(err)
	}

	got := s.WhatsNew()
	if got == nil {
		t.Fatal("WhatsNew = nil, wil de changelog van v0.2.10")
	}
	if got.Version != "v0.2.10" || len(got.Changes) != 1 {
		t.Errorf("WhatsNew = %+v", got)
	}

	// Tweede aanroep: het venster hoort maar één keer te komen.
	if tweede := s.WhatsNew(); tweede != nil {
		t.Errorf("tweede WhatsNew = %+v, wil nil", tweede)
	}
}

func TestWhatsNewBijEersteStartOoit(t *testing.T) {
	s, _ := testService(t, "v0.2.9", &nepFetcher{})

	if got := s.WhatsNew(); got != nil {
		t.Fatalf("WhatsNew = %+v, wil nil bij een verse installatie", got)
	}

	st, err := loadUpdateState(s.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if st.LastRunVersion != "v0.2.9" {
		t.Errorf("LastRunVersion = %q, wil v0.2.9 zodat het venster later niet onterecht komt", st.LastRunVersion)
	}
}

func TestWhatsNewNegeertEenVreemdeInstalledVersion(t *testing.T) {
	// Handmatig een oudere build teruggezet: de bewaarde changelog hoort niet
	// bij de versie die nu draait.
	s, _ := testService(t, "v0.2.9", &nepFetcher{})
	if err := saveUpdateState(s.statePath, updateState{
		LastRunVersion:   "v0.2.8",
		InstalledVersion: "v0.2.10",
		InstalledChanges: []domain.ChangeEntry{{Kind: domain.ChangeNieuw, Text: "Iets anders"}},
	}); err != nil {
		t.Fatal(err)
	}

	if got := s.WhatsNew(); got != nil {
		t.Errorf("WhatsNew = %+v, wil nil", got)
	}
}

func TestBundlePathFor(t *testing.T) {
	cases := []struct {
		exe string
		wil string
	}{
		{"/Applications/Kinsta Updater.app/Contents/MacOS/rdm-sites-tool", "/Applications/Kinsta Updater.app"},
		{"/Users/x/Projects/RDM-Sites-tool/bin/rdm-sites-tool", ""},
		{"/Users/x/bin/Contents/MacOS/rdm-sites-tool", ""},
		{"/tmp/thing.app/Contents/rdm-sites-tool", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := bundlePathFor(c.exe); got != c.wil {
			t.Errorf("bundlePathFor(%q) = %q, wil %q", c.exe, got, c.wil)
		}
	}
}

func TestStartDraaitNietAlsAutoCheckUitStaat(t *testing.T) {
	f := &nepFetcher{rel: nieuweRelease("v0.2.10", "")}
	s, _ := testService(t, "v0.2.9", f)
	uit := false
	s.cfg.Updates.AutoCheck = &uit

	s.Start()
	t.Cleanup(s.Stop)

	time.Sleep(50 * time.Millisecond)
	if f.aanroep != 0 {
		t.Error("de loop heeft gecontroleerd terwijl automatisch controleren uit staat")
	}
}

func TestStartControleertNaDeInitieleVertraging(t *testing.T) {
	f := &nepFetcher{rel: nieuweRelease("v0.2.10", "")}
	s, em := testService(t, "v0.2.9", f)
	s.initialDelay = 10 * time.Millisecond
	s.interval = time.Hour

	s.Start()
	t.Cleanup(s.Stop)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if f.aanroep > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if f.aanroep == 0 {
		t.Fatal("de loop heeft niet gecontroleerd")
	}
	if len(em.events) == 0 {
		t.Error("de loop stuurde geen updates:available")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/services/ -run 'Check|WhatsNew|BundlePath|Start' -v`
Expected: FAIL — `undefined: UpdateService`, `undefined: releaseFetcher`, `undefined: errGeenUpdateToken`, `undefined: bundlePathFor`.

- [ ] **Step 4: Write the service**

Create `internal/services/update_service.go`:

```go
package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/github"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
	"github.com/rdm/sites-tool/internal/version"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// selfRepo is de repository waar de tool zelf woont. Bewust een constante en
// geen instelling: dit is de thuisbasis van de app, geen keuze van de
// gebruiker.
const selfRepo = "bitfactory-nl/internal-kinsta-updates"

// errGeenUpdateToken betekent dat er nergens een GitHub-token staat. De repo is
// privé, dus zonder token is zelfs controleren onmogelijk.
var errGeenUpdateToken = errors.New("geen GitHub-token: vul er een in bij Instellingen om updates te kunnen ophalen")

// releaseFetcher is het deel van de GitHub-release-API dat deze service nodig
// heeft; *github.ReleaseClient voldoet eraan.
type releaseFetcher interface {
	LatestRelease(ctx context.Context) (github.Release, error)
	DownloadAsset(ctx context.Context, assetID int64, w io.Writer, onProgress func(done, total int64)) error
}

// UpdateService controleert of er een nieuwere release van de tool zelf is en
// installeert die op verzoek.
type UpdateService struct {
	cfg        *config.Global
	statePath  string
	logDir     string
	bundlePath string // pad van de draaiende .app, leeg buiten een bundle
	current    string // versie van deze build
	newClient  func(token, repo string) releaseFetcher

	initialDelay time.Duration
	interval     time.Duration

	app     *application.App
	emitter eventEmitter

	mu        sync.Mutex
	available *domain.AvailableUpdate
	asset     github.ReleaseAsset
	lastError string
	stop      chan struct{}
	running   bool
}

// NewUpdateService bouwt de service op basis van de draaiende binary: de versie
// komt uit de ldflags-stempel, de bundle uit het pad van het uitvoerbare
// bestand.
func NewUpdateService(cfg *config.Global) *UpdateService {
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	return &UpdateService{
		cfg:          cfg,
		statePath:    DefaultUpdateStatePath(),
		logDir:       DefaultUpdateLogDir(),
		bundlePath:   bundlePathFor(exe),
		current:      version.Version,
		newClient:    func(token, repo string) releaseFetcher { return github.NewReleaseClient(token, repo) },
		initialDelay: initialUpdateCheckDelay,
		interval:     updateCheckInterval,
	}
}

// SetApp injects the Wails app reference (called after app creation).
func (s *UpdateService) SetApp(app *application.App) {
	s.app = app
	s.emitter = app.Event
}

// DefaultUpdateLogDir is ~/.config/rdm/logs.
func DefaultUpdateLogDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "rdm", "logs")
	}
	return filepath.Join(home, ".config", "rdm", "logs")
}

// bundlePathFor levert het pad van de .app-bundle waarin exe staat, of "" als
// exe daar niet in zit — een los gebouwd binair bestand uit bin/ bijvoorbeeld.
// Zonder bundle is er niets te vervangen en staat zelf-update uit.
func bundlePathFor(exe string) string {
	if exe == "" {
		return ""
	}
	macos := filepath.Dir(exe)
	if filepath.Base(macos) != "MacOS" {
		return ""
	}
	contents := filepath.Dir(macos)
	if filepath.Base(contents) != "Contents" {
		return ""
	}
	app := filepath.Dir(contents)
	if filepath.Ext(app) != ".app" {
		return ""
	}
	return app
}

// enabled meldt of deze build zichzelf mag bijwerken.
func (s *UpdateService) enabled() bool {
	return s.bundlePath != "" && s.current != "" && s.current != "dev"
}

// token levert het GitHub-token: de override uit de updates-sectie, en anders
// dat van de plugin-repo. Keychain-referenties worden hier opgelost.
func (s *UpdateService) token() (string, error) {
	ref := strings.TrimSpace(s.cfg.Updates.GithubToken)
	if ref == "" {
		ref = strings.TrimSpace(s.cfg.PluginRepo.GithubToken)
	}
	if ref == "" {
		return "", errGeenUpdateToken
	}
	token, err := config.ResolveSecret(ref)
	if err != nil {
		return "", fmt.Errorf("GitHub-token uit de keychain lezen: %w", err)
	}
	if strings.TrimSpace(token) == "" {
		return "", errGeenUpdateToken
	}
	return token, nil
}

// Status is wat de frontend nodig heeft voor de badge, de popup en de sectie in
// Instellingen.
func (s *UpdateService) Status() domain.UpdateStatus {
	st, _ := loadUpdateState(s.statePath)

	s.mu.Lock()
	defer s.mu.Unlock()
	return domain.UpdateStatus{
		CurrentVersion: s.current,
		Enabled:        s.enabled(),
		AutoCheck:      s.cfg.Updates.AutoCheckEnabled(),
		LastCheck:      st.LastCheck,
		LastError:      s.lastError,
		Available:      s.available,
	}
}

// Check haalt de laatste release op en vergelijkt die met de draaiende versie.
// Is er een nieuwere die niet is overgeslagen, dan gaat er een
// "updates:available"-event naar de frontend. Een mislukte check levert een
// fout op en zet LastError, maar stuurt geen event: een popup over een
// netwerkfout onderbreekt het werk zonder dat er iets te kiezen valt.
func (s *UpdateService) Check() (domain.UpdateStatus, error) {
	if !s.enabled() {
		return s.Status(), nil
	}

	token, err := s.token()
	if err != nil {
		s.setError(err)
		return s.Status(), err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rel, err := s.newClient(token, selfRepo).LatestRelease(ctx)
	if err != nil {
		s.setError(err)
		return s.Status(), err
	}

	st, _ := loadUpdateState(s.statePath)
	st.LastCheck = time.Now()
	if err := saveUpdateState(s.statePath, st); err != nil {
		// Niet fataal: de check zelf is gelukt, alleen het onthouden faalde.
		s.setError(err)
	}

	if !version.IsNewer(rel.TagName, s.current) {
		s.mu.Lock()
		s.available = nil
		s.lastError = ""
		s.mu.Unlock()
		return s.Status(), nil
	}

	upd := &domain.AvailableUpdate{
		Version:   rel.TagName,
		Changes:   parseChangelog(rel.Body),
		Skipped:   st.SkippedVersion == rel.TagName,
		SizeBytes: rel.Asset.Size,
	}

	s.mu.Lock()
	s.available = upd
	s.asset = rel.Asset
	s.lastError = ""
	emitter := s.emitter
	s.mu.Unlock()

	if !upd.Skipped && emitter != nil {
		emitter.Emit("updates:available", upd)
	}
	return s.Status(), nil
}

// Skip legt vast dat de gebruiker deze versie heeft weggeklikt: de popup komt
// niet terug, de badge in de rail blijft staan.
func (s *UpdateService) Skip(v string) error {
	st, err := loadUpdateState(s.statePath)
	if err != nil {
		return err
	}
	st.SkippedVersion = v
	if err := saveUpdateState(s.statePath, st); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.available != nil && s.available.Version == v {
		kopie := *s.available
		kopie.Skipped = true
		s.available = &kopie
	}
	return nil
}

// WhatsNew levert de changelog van de update die net is geïnstalleerd, en
// alleen bij de eerste start daarna. De vergelijking gaat via de state: wijkt
// LastRunVersion af van de draaiende versie en hoort de bewaarde changelog bij
// die versie, dan is dit de eerste start na een update. Daarna wordt
// LastRunVersion bijgewerkt, zodat het venster één keer verschijnt.
func (s *UpdateService) WhatsNew() *domain.AvailableUpdate {
	st, err := loadUpdateState(s.statePath)
	if err != nil {
		return nil
	}
	if st.LastRunVersion == s.current {
		return nil
	}

	var uit *domain.AvailableUpdate
	if st.LastRunVersion != "" && st.InstalledVersion == s.current {
		uit = &domain.AvailableUpdate{Version: s.current, Changes: st.InstalledChanges}
	}

	st.LastRunVersion = s.current
	_ = saveUpdateState(s.statePath, st)
	return uit
}

// InstallLog geeft de inhoud van het laatste update-logbestand, voor de link in
// Instellingen en het "wat is er nieuw"-venster.
func (s *UpdateService) InstallLog() (string, error) {
	st, err := loadUpdateState(s.statePath)
	if err != nil {
		return "", err
	}
	if st.InstallLog == "" {
		return "", nil
	}
	data, err := os.ReadFile(st.InstallLog)
	if err != nil {
		return "", fmt.Errorf("update-log lezen: %w", err)
	}
	return string(data), nil
}

// setError bewaart een foutmelding voor de sectie in Instellingen.
func (s *UpdateService) setError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastError = err.Error()
}

// emitProgress stuurt een voortgangsstap naar de frontend.
func (s *UpdateService) emitProgress(p domain.UpdateProgress) {
	s.mu.Lock()
	emitter := s.emitter
	s.mu.Unlock()
	if emitter != nil {
		emitter.Emit("updates:progress", p)
	}
}
```

Voeg `"io"` toe aan de imports (nodig voor `releaseFetcher`).

- [ ] **Step 5: Write the background loop**

Create `internal/services/update_check.go`:

```go
package services

import (
	"log"
	"os"
	"time"
)

const (
	// updateCheckInterval is 6 uur: vier controles per etmaal terwijl de app
	// open staat.
	updateCheckInterval = 6 * time.Hour

	// initialUpdateCheckDelay houdt de eerste controle uit het opstartpad: de
	// app moet eerst zichtbaar en bruikbaar zijn, en pas daarna het netwerk op.
	initialUpdateCheckDelay = 20 * time.Second
)

// Start begint de achtergrondloop en ruimt een achtergebleven back-upbundle op.
// No-op wanneer de loop al draait, in een dev-build, of als automatisch
// controleren uit staat.
func (s *UpdateService) Start() {
	s.cleanupBackupBundle()

	if !s.enabled() || !s.cfg.Updates.AutoCheckEnabled() {
		return
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stop = make(chan struct{})
	stop := s.stop
	initial := s.initialDelay
	interval := s.interval
	s.mu.Unlock()

	if initial <= 0 {
		initial = initialUpdateCheckDelay
	}
	if interval <= 0 {
		interval = updateCheckInterval
	}

	go func() {
		timer := time.NewTimer(initial)
		defer timer.Stop()
		for {
			select {
			case <-stop:
				return
			case <-timer.C:
				// De toggle wordt bij elke ronde opnieuw gelezen, zodat hem
				// uitzetten in Instellingen direct werkt zonder herstart.
				if s.cfg.Updates.AutoCheckEnabled() {
					if _, err := s.Check(); err != nil {
						log.Printf("update-check: %v", err)
					}
				}
				timer.Reset(interval)
			}
		}
	}()
}

// Stop halts the background check loop.
func (s *UpdateService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		close(s.stop)
		s.running = false
	}
}

// cleanupBackupBundle verwijdert de <naam>.app.bak die het update-script
// achterlaat. Dat dit lukt is tegelijk het bewijs dat de nieuwe build start:
// blijft de back-up staan, dan is er nog een werkende versie terug te zetten.
func (s *UpdateService) cleanupBackupBundle() {
	if s.bundlePath == "" {
		return
	}
	bak := s.bundlePath + ".bak"
	if _, err := os.Stat(bak); err != nil {
		return
	}
	if err := os.RemoveAll(bak); err != nil {
		log.Printf("oude app-back-up opruimen: %v", err)
		return
	}
	log.Printf("oude app-back-up opgeruimd: %s", bak)
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/services/ -run 'Check|WhatsNew|BundlePath|Start' -v`
Expected: PASS — vijftien tests.

- [ ] **Step 7: Wire the service into the app**

In `internal/app/app.go` — voeg het veld toe aan `Services` (onder `BulkUpdate`):

```go
	OrgSync         *services.OrgSyncService
	BulkUpdate      *services.BulkUpdateService
	Update          *services.UpdateService
```

in `NewServices`, in de returned literal (onder `BulkUpdate:`):

```go
		BulkUpdate:      services.NewBulkUpdateService(&cfg.Global, project, git, plugin),
		Update:          services.NewUpdateService(&cfg.Global),
```

en in `Wails()` (onder de regel voor `s.BulkUpdate`):

```go
		application.NewService(s.BulkUpdate),
		application.NewService(s.Update),
```

In `main.go`, bij de andere `SetApp`-aanroepen en naast `services.VulnScan.Start()`:

```go
	services.BulkUpdate.SetApp(a)
	services.Update.SetApp(a)

	// Start the background vulnerability scan loop (no-op if alerts disabled).
	services.VulnScan.Start()

	// Controleer op een nieuwere versie van de tool zelf: kort na het opstarten
	// en daarna elke 6 uur. No-op in dev-builds.
	services.Update.Start()
```

- [ ] **Step 8: Regenerate the frontend bindings**

Run: `wails3 task common:generate:bindings`
Expected: `frontend/bindings/github.com/rdm/sites-tool/internal/services/updateservice.ts` verschijnt en `models.ts` krijgt `UpdateStatus`, `AvailableUpdate`, `ChangeEntry` en `UpdateProgress`.

Controleer: `ls frontend/bindings/github.com/rdm/sites-tool/internal/services/updateservice.ts && grep -c "AvailableUpdate" frontend/bindings/github.com/rdm/sites-tool/internal/domain/models.ts`

- [ ] **Step 9: Run the full suite and build**

Run: `go test ./... && wails3 task build`
Expected: PASS en een geslaagde build.

- [ ] **Step 10: Commit**

```bash
git add internal/services/update_service.go internal/services/update_check.go internal/services/update_service_test.go internal/config/schema.go internal/config/global.go internal/app/app.go main.go frontend/bindings
git commit -m "feat: UpdateService die 4x per dag en op verzoek naar releases kijkt"
```

---

### Task 7: Installatie via een helper-script

**Files:**
- Create: `internal/services/update_install.go`
- Create: `internal/services/update_script.sh.tmpl`
- Test: `internal/services/update_install_test.go`

**Interfaces:**
- Consumes: alles uit Task 6 (`s.asset`, `s.bundlePath`, `s.logDir`, `s.newClient`, `s.token()`, `s.emitProgress`, `s.app`), `domain.UpdateProgress` en zijn fase-constanten (Task 3).
- Produces: `(*UpdateService).Install() error`; intern `validateStagedApp(root string) (string, error)`, `renderUpdateScript(path string, d scriptData) error`, `scriptData{PID int; BundlePath, StagedApp, LogPath string}`.

- [ ] **Step 1: Write the failing test**

Create `internal/services/update_install_test.go`:

```go
package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// maakBundle bouwt een minimale .app-structuur in dir en geeft het pad terug.
func maakBundle(t *testing.T, dir, naam string, uitvoerbaar bool) string {
	t.Helper()
	app := filepath.Join(dir, naam)
	macos := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	mode := os.FileMode(0o644)
	if uitvoerbaar {
		mode = 0o755
	}
	if err := os.WriteFile(filepath.Join(macos, "rdm-sites-tool"), []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestValidateStagedAppVindtDeBundle(t *testing.T) {
	dir := t.TempDir()
	wil := maakBundle(t, dir, "Kinsta Updater.app", true)

	got, err := validateStagedApp(dir)
	if err != nil {
		t.Fatalf("validateStagedApp: %v", err)
	}
	if got != wil {
		t.Errorf("pad = %q, wil %q", got, wil)
	}
}

func TestValidateStagedAppZonderBundle(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hoi"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := validateStagedApp(dir); err == nil {
		t.Fatal("validateStagedApp gaf geen fout voor een archief zonder .app")
	}
}

func TestValidateStagedAppMetTweeBundles(t *testing.T) {
	dir := t.TempDir()
	maakBundle(t, dir, "Een.app", true)
	maakBundle(t, dir, "Twee.app", true)

	_, err := validateStagedApp(dir)
	if err == nil {
		t.Fatal("validateStagedApp gaf geen fout voor twee bundles")
	}
	if !strings.Contains(err.Error(), "twee") && !strings.Contains(err.Error(), "meer dan") {
		t.Errorf("foutmelding = %q, wil uitleggen dat er meer dan één bundle is", err.Error())
	}
}

func TestValidateStagedAppZonderUitvoerbaarBinair(t *testing.T) {
	dir := t.TempDir()
	maakBundle(t, dir, "Kinsta Updater.app", false)

	if _, err := validateStagedApp(dir); err == nil {
		t.Fatal("validateStagedApp accepteerde een niet-uitvoerbaar binair bestand")
	}
}

func TestValidateStagedAppZonderInfoPlist(t *testing.T) {
	dir := t.TempDir()
	app := maakBundle(t, dir, "Kinsta Updater.app", true)
	if err := os.Remove(filepath.Join(app, "Contents", "Info.plist")); err != nil {
		t.Fatal(err)
	}

	if _, err := validateStagedApp(dir); err == nil {
		t.Fatal("validateStagedApp accepteerde een bundle zonder Info.plist")
	}
}

func TestRenderUpdateScriptIsGeldigBashEnBevatDePaden(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "update.sh")
	d := scriptData{
		PID:        4242,
		BundlePath: "/Applications/Kinsta Updater.app",
		StagedApp:  filepath.Join(dir, "staged", "Kinsta Updater.app"),
		LogPath:    filepath.Join(dir, "update.log"),
	}

	if err := renderUpdateScript(script, d); err != nil {
		t.Fatalf("renderUpdateScript: %v", err)
	}

	// Syntaxcontrole zonder uitvoeren: bash -n leest het script alleen.
	cmd := exec.Command("bash", "-n", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bash -n: %v\n%s", err, out)
	}

	data, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	inhoud := string(data)
	for _, wil := range []string{
		"4242",
		d.BundlePath,
		d.StagedApp,
		d.LogPath,
		"ditto",
		"com.apple.quarantine",
		"playwright",
		"open",
	} {
		if !strings.Contains(inhoud, wil) {
			t.Errorf("script bevat %q niet", wil)
		}
	}

	info, err := os.Stat(script)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("rechten = %v, wil uitvoerbaar", info.Mode().Perm())
	}
}

func TestRenderUpdateScriptQuotPadenMetSpaties(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "update.sh")
	d := scriptData{
		PID:        1,
		BundlePath: "/Applications/Kinsta Updater.app",
		StagedApp:  "/tmp/rdm update/Kinsta Updater.app",
		LogPath:    "/tmp/rdm logs/update.log",
	}

	if err := renderUpdateScript(script, d); err != nil {
		t.Fatalf("renderUpdateScript: %v", err)
	}

	data, _ := os.ReadFile(script)
	// Elk pad moet tussen dubbele quotes staan, anders breekt het script op de
	// spatie in "Kinsta Updater.app".
	for _, pad := range []string{d.BundlePath, d.StagedApp, d.LogPath} {
		if !strings.Contains(string(data), `"`+pad+`"`) {
			t.Errorf("pad %q staat niet gequote in het script", pad)
		}
	}
}

func TestInstallZonderBeschikbareUpdate(t *testing.T) {
	s, _ := testService(t, "v0.2.9", &nepFetcher{})

	if err := s.Install(); err == nil {
		t.Fatal("Install gaf geen fout zonder beschikbare update")
	}
}

func TestInstallInDevBuild(t *testing.T) {
	s, _ := testService(t, "dev", &nepFetcher{})

	if err := s.Install(); err == nil {
		t.Fatal("Install gaf geen fout in een dev-build")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/ -run 'ValidateStagedApp|RenderUpdateScript|Install' -v`
Expected: FAIL — `undefined: validateStagedApp`, `undefined: renderUpdateScript`, `undefined: scriptData`, `s.Install undefined`.

- [ ] **Step 3: Write the script template**

Create `internal/services/update_script.sh.tmpl`:

```bash
#!/bin/bash
# Vervangt de app-bundle nadat de app zelf is afgesloten, draait de
# nabewerkingscommando's en start de nieuwe versie. Dit script wordt door de
# tool gegenereerd en losgekoppeld gestart; alle uitvoer gaat naar het
# logbestand, want er is op dat moment geen venster meer om iets in te tonen.
set -uo pipefail

LOG="{{.LogPath}}"
mkdir -p "$(dirname "$LOG")"
exec >>"$LOG" 2>&1

BUNDLE="{{.BundlePath}}"
STAGED="{{.StagedApp}}"
BAK="${BUNDLE}.bak"

echo "=== update gestart $(date -Iseconds) ==="
echo "pid={{.PID}}"
echo "bundle=$BUNDLE"
echo "nieuw=$STAGED"

# 1. Wachten tot de app echt weg is. Een bundle vervangen terwijl het proces
#    nog draait kan, maar dan blijft de oude versie in het geheugen met een
#    halve nieuwe installatie eronder.
for _ in $(seq 1 30); do
  kill -0 "{{.PID}}" 2>/dev/null || break
  sleep 1
done
if kill -0 "{{.PID}}" 2>/dev/null; then
  echo "app reageert niet binnen 30s, wordt afgesloten"
  kill "{{.PID}}" 2>/dev/null || true
  sleep 2
fi

# 2. Oude bundle apart zetten en de nieuwe erin. Mislukt het kopiëren, dan gaat
#    de oude versie terug: liever de vorige versie dan geen werkende app.
rm -rf "$BAK"
if ! mv "$BUNDLE" "$BAK"; then
  echo "FOUT: kon de bestaande app niet wegzetten; er is niets gewijzigd"
  exit 1
fi
if ! ditto "$STAGED" "$BUNDLE"; then
  echo "FOUT: kopiëren van de nieuwe app mislukte; oude versie wordt teruggezet"
  rm -rf "$BUNDLE"
  mv "$BAK" "$BUNDLE"
  exit 1
fi
echo "nieuwe app geplaatst"

# 3. Quarantine-vlag weghalen, zodat de app zonder Terminal-commando opent.
xattr -dr com.apple.quarantine "$BUNDLE" 2>/dev/null || true

# 4. Playwright-chromium, en alleen wanneer die ontbreekt. Playwright zelf weet
#    welke browserversie bij deze sidecar hoort; bestaat dat pad al, dan is er
#    niets te doen. Een nieuwere Playwright verwacht een andere revisie en komt
#    dus automatisch op "ontbreekt" uit.
SIDECAR="$BUNDLE/Contents/Resources/sidecar"
if [ -d "$SIDECAR" ]; then
  if command -v node >/dev/null 2>&1; then
    CHROMIUM=$(cd "$SIDECAR" && node -e "process.stdout.write(require('playwright').chromium.executablePath())" 2>/dev/null || true)
    if [ -n "$CHROMIUM" ] && [ -x "$CHROMIUM" ]; then
      echo "chromium staat al goed: $CHROMIUM"
    else
      echo "chromium ontbreekt of hoort bij een andere versie, wordt opgehaald"
      if ! (cd "$SIDECAR" && node node_modules/playwright/cli.js install chromium); then
        echo "LET OP: installeren van chromium mislukte; PDF-export, tests en mediascan werken nog niet"
      fi
    fi
  else
    echo "LET OP: node ontbreekt op deze machine; PDF-export, tests en mediascan werken niet tot Node 20+ is geïnstalleerd"
  fi
else
  echo "LET OP: geen sidecar in de nieuwe bundle gevonden"
fi

# 5. Nieuwe versie starten. Die ruimt de back-up op en toont wat er nieuw is.
echo "=== update klaar $(date -Iseconds) ==="
open "$BUNDLE"
```

- [ ] **Step 4: Write the installer**

Create `internal/services/update_install.go`:

```go
package services

import (
	"context"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

//go:embed update_script.sh.tmpl
var updateScriptTemplate embed.FS

// stagedBinaryName is de naam van het uitvoerbare bestand in de bundle; komt
// uit CFBundleExecutable in build/darwin/Info.plist.
const stagedBinaryName = "rdm-sites-tool"

// scriptData zijn de waarden die in het helper-script worden ingevuld.
type scriptData struct {
	PID        int
	BundlePath string
	StagedApp  string
	LogPath    string
}

// Install haalt de beschikbare update binnen, zet hem klaar in een tempmap, en
// draagt het vervangen over aan een los script. Bij succes keert deze functie
// niet terug op een zinvolle manier: de app sluit zichzelf af.
//
// De volgorde is bewust: alles wat kan mislukken gebeurt vóórdat er iets aan de
// bestaande installatie verandert. Faalt de download, het uitpakken of de
// controle, dan staat er nog steeds een werkende app.
func (s *UpdateService) Install() error {
	if !s.enabled() {
		return fmt.Errorf("zelf-update is uitgeschakeld in deze build (versie %q)", s.current)
	}

	s.mu.Lock()
	beschikbaar := s.available
	asset := s.asset
	s.mu.Unlock()

	if beschikbaar == nil || asset.ID == 0 {
		return fmt.Errorf("geen update beschikbaar om te installeren; controleer eerst op updates")
	}

	// Kan de bestaande installatie überhaupt vervangen worden? Dit eerst
	// vragen, zodat een gebruiker zonder schrijfrechten niet eerst 12 MB
	// downloadt om daarna alsnog te stranden.
	doelMap := filepath.Dir(s.bundlePath)
	if err := mapIsBeschrijfbaar(doelMap); err != nil {
		return fmt.Errorf("geen schrijfrechten op %s: installeer de update handmatig (%w)", doelMap, err)
	}

	token, err := s.token()
	if err != nil {
		return err
	}

	werkMap, err := os.MkdirTemp("", "rdm-update-*")
	if err != nil {
		return fmt.Errorf("tijdelijke map aanmaken: %w", err)
	}
	// De werkmap blijft staan tot het script klaar is; die ruimt macOS zelf op.

	zipPad := filepath.Join(werkMap, "update.zip")
	if err := s.downloadNaar(token, asset.ID, zipPad); err != nil {
		return err
	}

	uitgepakt := filepath.Join(werkMap, "uitgepakt")
	s.emitProgress(domain.UpdateProgress{Phase: domain.PhaseUitpakken})
	if err := os.MkdirAll(uitgepakt, 0o755); err != nil {
		return fmt.Errorf("uitpakmap aanmaken: %w", err)
	}
	// ditto pakt uit met behoud van symlinks en rechten; unzip doet dat niet
	// betrouwbaar voor een .app-bundle.
	if out, err := exec.Command("ditto", "-x", "-k", zipPad, uitgepakt).CombinedOutput(); err != nil {
		return fmt.Errorf("update uitpakken: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	nieuweApp, err := validateStagedApp(uitgepakt)
	if err != nil {
		return err
	}

	logPad := filepath.Join(s.logDir, fmt.Sprintf("update-%s.log", time.Now().Format("20060102-150405")))

	// De changelog nu bewaren: na de herstart is er geen netwerk of token nodig
	// om te tonen wat er veranderd is.
	st, _ := loadUpdateState(s.statePath)
	st.InstalledVersion = beschikbaar.Version
	st.InstalledChanges = beschikbaar.Changes
	st.InstallLog = logPad
	if err := saveUpdateState(s.statePath, st); err != nil {
		return err
	}

	scriptPad := filepath.Join(werkMap, "update.sh")
	if err := renderUpdateScript(scriptPad, scriptData{
		PID:        os.Getpid(),
		BundlePath: s.bundlePath,
		StagedApp:  nieuweApp,
		LogPath:    logPad,
	}); err != nil {
		return err
	}

	s.emitProgress(domain.UpdateProgress{Phase: domain.PhaseVervangen})
	if err := startLosgekoppeld(scriptPad); err != nil {
		return err
	}

	// Even ademruimte zodat de frontend de laatste voortgangsstap nog toont.
	time.Sleep(300 * time.Millisecond)
	if s.app != nil {
		s.app.Quit()
	}
	return nil
}

// downloadNaar streamt de asset naar pad en meldt de voortgang.
func (s *UpdateService) downloadNaar(token string, assetID int64, pad string) error {
	f, err := os.Create(pad)
	if err != nil {
		return fmt.Errorf("downloadbestand aanmaken: %w", err)
	}
	defer f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	err = s.newClient(token, selfRepo).DownloadAsset(ctx, assetID, f, func(done, total int64) {
		s.emitProgress(domain.UpdateProgress{Phase: domain.PhaseDownload, Done: done, Total: total})
	})
	if err != nil {
		return fmt.Errorf("update downloaden: %w", err)
	}
	return f.Sync()
}

// validateStagedApp controleert dat het uitgepakte archief precies één
// app-bundle bevat die er compleet uitziet, en geeft het pad daarvan terug.
func validateStagedApp(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("uitgepakte update lezen: %w", err)
	}

	var bundles []string
	for _, e := range entries {
		if e.IsDir() && filepath.Ext(e.Name()) == ".app" {
			bundles = append(bundles, filepath.Join(root, e.Name()))
		}
	}

	switch {
	case len(bundles) == 0:
		return "", fmt.Errorf("de gedownloade update bevat geen .app-bundle")
	case len(bundles) > 1:
		return "", fmt.Errorf("de gedownloade update bevat meer dan één .app-bundle (%d); dat hoort niet en wordt niet geïnstalleerd", len(bundles))
	}

	app := bundles[0]
	if _, err := os.Stat(filepath.Join(app, "Contents", "Info.plist")); err != nil {
		return "", fmt.Errorf("de gedownloade update mist Contents/Info.plist")
	}

	bin := filepath.Join(app, "Contents", "MacOS", stagedBinaryName)
	info, err := os.Stat(bin)
	if err != nil {
		return "", fmt.Errorf("de gedownloade update mist Contents/MacOS/%s", stagedBinaryName)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("het binaire bestand in de gedownloade update is niet uitvoerbaar")
	}
	return app, nil
}

// renderUpdateScript schrijft het helper-script naar pad en maakt het
// uitvoerbaar.
func renderUpdateScript(pad string, d scriptData) error {
	tmpl, err := template.ParseFS(updateScriptTemplate, "update_script.sh.tmpl")
	if err != nil {
		return fmt.Errorf("update-script template lezen: %w", err)
	}

	f, err := os.OpenFile(pad, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		return fmt.Errorf("update-script aanmaken: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, d); err != nil {
		return fmt.Errorf("update-script schrijven: %w", err)
	}
	return nil
}

// startLosgekoppeld start het script in een eigen sessie, zodat het blijft
// draaien nadat deze app is afgesloten.
func startLosgekoppeld(scriptPad string) error {
	cmd := exec.Command("/bin/bash", scriptPad)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("update-script starten: %w", err)
	}
	// Niet wachten: het script overleeft dit proces met opzet.
	go func() { _ = cmd.Wait() }()
	return nil
}

// mapIsBeschrijfbaar meldt of de map beschrijfbaar is voor de huidige
// gebruiker, getest door er een bestand in aan te maken en weer te verwijderen.
func mapIsBeschrijfbaar(dir string) error {
	f, err := os.CreateTemp(dir, ".rdm-schrijftest-*")
	if err != nil {
		return err
	}
	naam := f.Name()
	f.Close()
	return os.Remove(naam)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/services/ -run 'ValidateStagedApp|RenderUpdateScript|Install' -v`
Expected: PASS — negen tests.

- [ ] **Step 6: Run the full suite**

Run: `go test ./... && go vet ./...`
Expected: PASS, geen vet-meldingen.

- [ ] **Step 7: Regenerate the bindings and build**

Run: `wails3 task common:generate:bindings && wails3 task build`
Expected: `Install` staat in `frontend/bindings/.../updateservice.ts`, de build slaagt.

- [ ] **Step 8: Commit**

```bash
git add internal/services/update_install.go internal/services/update_script.sh.tmpl internal/services/update_install_test.go frontend/bindings
git commit -m "feat: update installeren via een helper-script dat de bundle vervangt"
```

---

### Task 8: Popup, voortgang en "wat is er nieuw" in de frontend

**Files:**
- Create: `frontend/src/components/AppUpdateDialog.tsx`
- Modify: `frontend/src/App.tsx` (imports, state, effect, badge in de rail-footer, dialog onderaan de render)

De naam is `AppUpdateDialog` en niet `UpdateDialog`: er bestaat al een `UpdatesTab.tsx` voor WordPress-updates, en die twee moeten niet met elkaar te verwarren zijn.

**Interfaces:**
- Consumes: `UpdateService.Status()`, `.Check()`, `.Install()`, `.Skip(version)`, `.WhatsNew()` en de types `UpdateStatus`, `AvailableUpdate`, `ChangeEntry`, `UpdateProgress` uit de bindings (Task 6/7); events `updates:available`, `updates:progress`.
- Produces: default export `AppUpdateDialog`, props `{ mode: 'available' | 'whatsnew'; currentVersion: string; update: AvailableUpdate; onLater: () => void; onKlaar: () => void }`.

- [ ] **Step 1: Write the dialog component**

Create `frontend/src/components/AppUpdateDialog.tsx`:

```tsx
import { useEffect, useState } from 'react'
import { Events } from '@wailsio/runtime'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { AvailableUpdate, UpdateProgress } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'

interface Props {
  /** 'available' vraagt om te installeren; 'whatsnew' toont wat er net is bijgewerkt. */
  mode: 'available' | 'whatsnew'
  currentVersion: string
  update: AvailableUpdate
  /** Wegklikken zonder installeren; alleen relevant in de 'available'-modus. */
  onLater: () => void
  /** Sluiten na een geslaagde installatie of in de 'whatsnew'-modus. */
  onKlaar: () => void
}

const KOP_PER_SOORT: Record<string, string> = {
  nieuw: 'Nieuw',
  opgelost: 'Opgelost',
  overig: 'Overig',
}
const SOORT_ORDE = ['nieuw', 'opgelost', 'overig']

function mb(bytes: number): string {
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

/** Wijzigingen per soort, in een vaste volgorde en zonder lege groepen. */
function Wijzigingen({ update }: { update: AvailableUpdate }) {
  const regels = update.changes ?? []
  if (regels.length === 0) {
    return (
      <p className="text-[12.5px] text-fg-faint">
        Geen details beschikbaar voor deze versie.
      </p>
    )
  }
  return (
    <div className="space-y-3">
      {SOORT_ORDE.map(soort => {
        const vanSoort = regels.filter(r => r.kind === soort)
        if (vanSoort.length === 0) return null
        return (
          <div key={soort}>
            <h4 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-1.5">
              {KOP_PER_SOORT[soort] ?? soort}
            </h4>
            <ul className="space-y-1">
              {vanSoort.map((r, i) => (
                <li key={`${soort}-${i}`} className="text-[12.5px] text-fg-muted flex gap-2">
                  <span className="text-accent shrink-0">•</span>
                  <span>{r.text}</span>
                </li>
              ))}
            </ul>
          </div>
        )
      })}
    </div>
  )
}

export default function AppUpdateDialog({ mode, currentVersion, update, onLater, onKlaar }: Props) {
  const [installeren, setInstalleren] = useState(false)
  const [voortgang, setVoortgang] = useState<UpdateProgress | null>(null)
  const [fout, setFout] = useState<string | null>(null)

  // Voortgang komt van de backend; de app sluit zichzelf zodra het
  // helper-script is gestart, dus de laatste fase blijft kort in beeld.
  useEffect(() => {
    const stop = Events.On('updates:progress', (ev: { data: UpdateProgress[] | UpdateProgress }) => {
      const p = Array.isArray(ev.data) ? ev.data[0] : ev.data
      if (p) setVoortgang(p)
    })
    return () => stop()
  }, [])

  const installeer = async () => {
    setInstalleren(true)
    setFout(null)
    try {
      await Services.UpdateService.Install()
      // Lukt dit, dan sluit de app zichzelf en is deze regel niet meer zichtbaar.
      onKlaar()
    } catch (e) {
      setFout(String(e))
      setInstalleren(false)
    }
  }

  const later = async () => {
    try {
      await Services.UpdateService.Skip(update.version)
    } catch {
      // Niet erg: dan komt de popup bij een volgende check nog één keer.
    }
    onLater()
  }

  const percentage = voortgang && voortgang.total > 0
    ? Math.round((voortgang.done / voortgang.total) * 100)
    : null

  const faseTekst = (() => {
    if (!voortgang) return 'Voorbereiden…'
    if (voortgang.phase === 'download') {
      return percentage !== null ? `Downloaden… ${percentage}%` : 'Downloaden…'
    }
    if (voortgang.phase === 'uitpakken') return 'Uitpakken en controleren…'
    if (voortgang.phase === 'vervangen') return 'App wordt vervangen, hij start straks zelf opnieuw…'
    return 'Bezig…'
  })()

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-6"
      role="dialog"
      aria-modal="true"
      aria-label={mode === 'available' ? 'Update beschikbaar' : 'Bijgewerkt'}
    >
      <div className="w-full max-w-[520px] max-h-full flex flex-col bg-panel border border-border rounded-[14px] shadow-2xl overflow-hidden">
        <div className="px-5 py-4 border-b border-border">
          <h3 className="text-[15px] font-bold text-fg">
            {mode === 'available' ? 'Update beschikbaar' : `Bijgewerkt naar ${update.version}`}
          </h3>
          <p className="text-[12px] text-fg-faint mt-0.5 font-mono">
            {mode === 'available'
              ? `${currentVersion} → ${update.version}${update.sizeBytes > 0 ? ` · ${mb(update.sizeBytes)}` : ''}`
              : currentVersion}
          </p>
        </div>

        <div className="px-5 py-4 overflow-y-auto flex-1 min-h-0">
          <Wijzigingen update={update} />
          {mode === 'whatsnew' && (
            <p className="text-[11.5px] text-fg-faint mt-4">
              De macOS-permissies van deze app zijn opnieuw gevraagd doordat de
              app-identiteit is gewijzigd. Je API-keys zijn automatisch
              overgezet.
            </p>
          )}
        </div>

        {installeren && (
          <div className="px-5 py-3 border-t border-border">
            <p className="text-[12.5px] text-fg-muted mb-2">{faseTekst}</p>
            <div className="h-1.5 rounded-full bg-panel-2 overflow-hidden">
              <div
                className={`h-full bg-accent transition-[width] duration-200 ${percentage === null ? 'animate-pulse w-1/3' : ''}`}
                style={percentage !== null ? { width: `${percentage}%` } : undefined}
              />
            </div>
          </div>
        )}

        {fout && (
          <div className="px-5 py-3 border-t border-border">
            <p className="text-[12.5px] text-red">{fout}</p>
          </div>
        )}

        <div className="px-5 py-3 bg-panel border-t border-border flex items-center gap-3 justify-end">
          {mode === 'available' ? (
            <>
              <button
                onClick={later}
                disabled={installeren}
                className="bg-panel-2 border border-border text-fg-muted text-[12.5px] font-semibold px-[15px] py-[9px]
                           rounded-[9px] hover:bg-hover disabled:opacity-50 transition-colors"
              >
                Later
              </button>
              <button
                onClick={installeer}
                disabled={installeren}
                className="bg-accent text-white text-[12.5px] font-semibold px-[18px] py-[9px] rounded-[9px]
                           hover:bg-accent-2 disabled:opacity-50 transition-colors flex items-center gap-2"
              >
                {installeren && <span className="animate-spin inline-block text-xs">↻</span>}
                Nu installeren
              </button>
            </>
          ) : (
            <button
              onClick={onKlaar}
              className="bg-accent text-white text-[12.5px] font-semibold px-[18px] py-[9px] rounded-[9px]
                         hover:bg-accent-2 transition-colors"
            >
              Aan de slag
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Wire it into App.tsx — imports**

Voeg bij de imports van `frontend/src/App.tsx` toe:

```tsx
import { Events } from '@wailsio/runtime'
import AppUpdateDialog from './components/AppUpdateDialog'
import type { AvailableUpdate } from '../bindings/github.com/rdm/sites-tool/internal/domain/models'
```

- [ ] **Step 3: Add the state and the effect**

Binnen de `App`-component, bij de overige `useState`-regels:

```tsx
  // Zelf-update: de badge in de rail volgt `appUpdate`, de popup `updateOpen`.
  const [appUpdate, setAppUpdate] = useState<AvailableUpdate | null>(null)
  const [updateOpen, setUpdateOpen] = useState(false)
  const [appVersie, setAppVersie] = useState('')
  const [watIsNieuw, setWatIsNieuw] = useState<AvailableUpdate | null>(null)
```

en een effect dat één keer bij het opstarten loopt:

```tsx
  useEffect(() => {
    // Eerst: is dit de eerste start na een geslaagde update?
    Services.UpdateService.WhatsNew()
      .then(w => { if (w) setWatIsNieuw(w) })
      .catch(() => {})

    Services.UpdateService.Status()
      .then(s => {
        setAppVersie(s.currentVersion)
        if (s.available) {
          setAppUpdate(s.available)
          // Een al weggeklikte versie geeft alleen de badge, geen popup.
          if (!s.available.skipped) setUpdateOpen(true)
        }
      })
      .catch(() => {})

    // De achtergrondloop meldt een nieuwe versie via dit event.
    const stop = Events.On('updates:available', (ev: { data: AvailableUpdate[] | AvailableUpdate }) => {
      const u = Array.isArray(ev.data) ? ev.data[0] : ev.data
      if (!u) return
      setAppUpdate(u)
      setUpdateOpen(true)
    })
    return () => stop()
  }, [])
```

`Services` is hier de bestaande import `ProjectService`; gebruik dezelfde alias als de rest van het bestand — in `App.tsx` heet die `ProjectService`, dus schrijf `ProjectService.UpdateService.WhatsNew()`. Controleer de bovenste importregel (`import * as ProjectService from '../bindings/.../internal/services'`) en gebruik die naam consequent.

- [ ] **Step 4: Add the badge to the rail footer**

In `frontend/src/App.tsx`, in het footer-blok van de rail (nu `NavItem` voor Instellingen plus `ThemeSwitcher`), bóven het `NavItem`:

```tsx
        {/* footer: update-melding + settings + thema */}
        <div className="px-2 py-3 border-t border-rail-border space-y-2">
          {appUpdate && (
            <button
              onClick={() => setUpdateOpen(true)}
              title={`Versie ${appUpdate.version} is beschikbaar`}
              className="w-full flex items-center gap-2 h-9 px-3 rounded-nav text-[12.5px] font-semibold
                         bg-accent/15 text-accent-2 hover:bg-accent/25 transition-colors select-none"
            >
              <span className="shrink-0 flex items-center"><CloudDownloadIcon size={14} /></span>
              <span className="truncate">Update {appUpdate.version}</span>
              <span className="w-1.5 h-1.5 rounded-full bg-accent-2 shrink-0" />
            </button>
          )}
          <NavItem
            icon={<GearIcon size={15} />}
            label="Instellingen"
            active={view === 'settings'}
            onClick={() => setView('settings')}
          />
          <ThemeSwitcher />
        </div>
```

- [ ] **Step 5: Render the dialogs**

Aan het einde van de JSX van `App`, direct vóór de afsluitende `</div>` van de buitenste container:

```tsx
      {/* Zelf-update: eerst het "wat is er nieuw"-venster na een update, en
          anders de vraag om te installeren. Nooit beide tegelijk. */}
      {watIsNieuw ? (
        <AppUpdateDialog
          mode="whatsnew"
          currentVersion={appVersie}
          update={watIsNieuw}
          onLater={() => setWatIsNieuw(null)}
          onKlaar={() => setWatIsNieuw(null)}
        />
      ) : updateOpen && appUpdate ? (
        <AppUpdateDialog
          mode="available"
          currentVersion={appVersie}
          update={appUpdate}
          onLater={() => setUpdateOpen(false)}
          onKlaar={() => setUpdateOpen(false)}
        />
      ) : null}
```

- [ ] **Step 6: Typecheck the frontend**

Run: `cd frontend && npx tsc --noEmit`
Expected: geen fouten. Struikelt het over de vorm van de event-payload, kijk dan in `frontend/src/components/LogsTab.tsx:135` hoe `Events.On` daar getypeerd wordt en volg die stijl.

- [ ] **Step 7: Verify it renders**

Run: `wails3 task build`
Expected: de build slaagt.

Handmatige controle (jij, niet de agent): start de app met `wails3 task dev`. In een dev-build staat zelf-update uit, dus er hoort géén badge en géén popup te verschijnen — dat is het bewijs dat de dev-guard werkt. De echte popup komt pas bij de verificatie met de testtag in Task 10.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/components/AppUpdateDialog.tsx frontend/src/App.tsx
git commit -m "feat: update-dialog met voortgang en badge in de rail"
```

---

### Task 9: Sectie App-updates in Instellingen

**Files:**
- Modify: `internal/services/settings_service.go` (twee velden op `AppSettings`, `Get`, `Save`)
- Modify: `internal/services/settings_service_test.go` (rondrit met de nieuwe velden)
- Modify: `frontend/src/components/SettingsPage.tsx` (nieuwe sectie, boven "Configuratie")

**Interfaces:**
- Consumes: `config.UpdatesGlobal` (Task 6), `UpdateService.Status()`, `.Check()`, `.InstallLog()` uit de bindings.
- Produces: `AppSettings.UpdatesAutoCheck bool` (`json:"updatesAutoCheck"`) en `AppSettings.UpdatesGithubToken string` (`json:"updatesGithubToken"`).

- [ ] **Step 1: Write the failing test**

Voeg toe aan `internal/services/settings_service_test.go`:

```go
func TestSettingsRondritMetUpdateVelden(t *testing.T) {
	autoAan := true
	cfg := &config.Global{
		Editor:  "cursor",
		Updates: config.UpdatesGlobal{AutoCheck: &autoAan, GithubToken: "keychain:rdm.github.token"},
	}
	s := NewSettingsService(cfg)

	got := s.Get()
	if !got.UpdatesAutoCheck {
		t.Error("UpdatesAutoCheck = false, wil true")
	}
	if got.UpdatesGithubToken != "keychain:rdm.github.token" {
		t.Errorf("UpdatesGithubToken = %q", got.UpdatesGithubToken)
	}
}

func TestSettingsSaveZetAutoCheckUit(t *testing.T) {
	// SaveGlobal schrijft naar ~/.config/rdm/config.yml; die kant is hier niet
	// interessant, alleen dat de waarde in cfg landt. HOME wijzen naar een
	// tempmap houdt de echte config van de gebruiker buiten de test.
	t.Setenv("HOME", t.TempDir())

	autoAan := true
	cfg := &config.Global{Editor: "cursor", Updates: config.UpdatesGlobal{AutoCheck: &autoAan}}
	s := NewSettingsService(cfg)

	instellingen := s.Get()
	instellingen.UpdatesAutoCheck = false
	instellingen.UpdatesGithubToken = " ghp_met_spaties "
	if err := s.Save(instellingen); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if cfg.Updates.AutoCheckEnabled() {
		t.Error("AutoCheckEnabled() = true na uitzetten")
	}
	if cfg.Updates.GithubToken != "ghp_met_spaties" {
		t.Errorf("GithubToken = %q, wil getrimd", cfg.Updates.GithubToken)
	}
}
```

Controleer of `settings_service_test.go` het `config`-pakket al importeert; zo niet, voeg `"github.com/rdm/sites-tool/internal/config"` toe.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/ -run Settings -v`
Expected: FAIL — `got.UpdatesAutoCheck undefined`.

- [ ] **Step 3: Write minimal implementation**

In `internal/services/settings_service.go`, twee velden onderaan `AppSettings`:

```go
	WordfenceAPIKey     string `json:"wordfenceApiKey"`
	UpdatesAutoCheck    bool   `json:"updatesAutoCheck"`
	UpdatesGithubToken  string `json:"updatesGithubToken"`
}
```

in `Get()`, onderaan de literal:

```go
		WordfenceAPIKey:     s.cfg.Wordfence.APIKey,
		UpdatesAutoCheck:    s.cfg.Updates.AutoCheckEnabled(),
		UpdatesGithubToken:  s.cfg.Updates.GithubToken,
	}
```

en in `Save()`, vóór de afsluitende `return`:

```go
	s.cfg.Wordfence.APIKey = settings.WordfenceAPIKey
	// AutoCheck is een pointer: expliciet zetten, zodat een uitgezette toggle
	// niet als "niet ingevuld" wordt weggeschreven en dus weer aan zou staan.
	autoCheck := settings.UpdatesAutoCheck
	s.cfg.Updates.AutoCheck = &autoCheck
	s.cfg.Updates.GithubToken = strings.TrimSpace(settings.UpdatesGithubToken)
	return config.SaveGlobal(*s.cfg)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/services/ -run Settings -v`
Expected: PASS.

- [ ] **Step 5: Regenerate the bindings**

Run: `wails3 task common:generate:bindings`
Expected: `updatesAutoCheck` en `updatesGithubToken` staan in `frontend/bindings/github.com/rdm/sites-tool/internal/services/models.ts`.

- [ ] **Step 6: Add the settings section**

In `frontend/src/components/SettingsPage.tsx`. Eerst state en een laadeffect erbij, naast de bestaande `useState`-regels:

```tsx
  const [updateStatus, setUpdateStatus] = useState<UpdateStatus | null>(null)
  const [controleren, setControleren] = useState(false)
  const [updateLog, setUpdateLog] = useState<string | null>(null)
  const [showUpdatesToken, setShowUpdatesToken] = useState(false)
```

met de bijbehorende import:

```tsx
import type { AppSettings, UpdateStatus } from '../../bindings/github.com/rdm/sites-tool/internal/services'
```

Let op: `UpdateStatus` staat in `internal/domain/models`, niet bij de services. Kijk na het genereren van de bindings waar het type landt en importeer het daar vandaan — `grep -rl "UpdateStatus" frontend/bindings` wijst het uit.

Laad de status naast de bestaande `Services.SettingsService.Get()`-aanroep:

```tsx
  useEffect(() => {
    Services.SettingsService.Get().then(s => setSettings(s)).catch(() => {})
    Services.UpdateService.Status().then(s => setUpdateStatus(s)).catch(() => {})
  }, [])

  const controleerNu = async () => {
    setControleren(true)
    try {
      setUpdateStatus(await Services.UpdateService.Check())
    } catch {
      // De foutmelding komt via Status().lastError terug.
      try { setUpdateStatus(await Services.UpdateService.Status()) } catch { /* stil */ }
    } finally {
      setControleren(false)
    }
  }
```

En de sectie zelf, direct vóór het blok `{/* Config file location */}`:

```tsx
        {/* App-updates */}
        <section>
          <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">
            App-updates
          </h3>
          <div className="bg-panel border border-border rounded-[11px] divide-y divide-border">
            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Huidige versie</label>
              <span className="text-[12.5px] text-fg font-mono flex-1">
                {updateStatus?.currentVersion || '—'}
              </span>
              <button
                onClick={controleerNu}
                disabled={controleren || !updateStatus?.enabled}
                className="bg-panel-2 border border-border text-fg-muted text-[12px] font-semibold px-3 py-1.5
                           rounded-[8px] hover:bg-hover disabled:opacity-50 transition-colors flex items-center gap-1.5"
              >
                {controleren && <span className="animate-spin inline-block text-[10px]">↻</span>}
                Nu controleren
              </button>
            </div>

            {!updateStatus?.enabled && (
              <div className="px-4 py-3">
                <p className="text-[11.5px] text-fg-faint">
                  Zelf-update staat uit in deze build: hij is lokaal gebouwd of
                  draait niet vanuit een <code className="font-mono">.app</code>.
                </p>
              </div>
            )}

            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Laatste check</label>
              <span className="text-[12.5px] text-fg-muted flex-1">
                {updateStatus?.lastCheck && !updateStatus.lastCheck.startsWith('0001-01-01')
                  ? new Date(updateStatus.lastCheck).toLocaleString('nl-NL')
                  : 'nog niet gecontroleerd'}
              </span>
            </div>

            {updateStatus?.available && (
              <div className="px-4 py-3">
                <p className="text-[12.5px] text-fg">
                  Versie <span className="font-mono">{updateStatus.available.version}</span> is beschikbaar
                  {updateStatus.available.skipped ? ' (overgeslagen)' : ''}.
                </p>
              </div>
            )}

            {updateStatus?.lastError && (
              <div className="px-4 py-3">
                <p className="text-[12.5px] text-red">{updateStatus.lastError}</p>
              </div>
            )}

            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Automatisch</label>
              <button
                onClick={() => update('updatesAutoCheck', !settings.updatesAutoCheck)}
                className={`relative w-9 h-5 rounded-full transition-colors ${
                  settings.updatesAutoCheck ? 'bg-accent' : 'bg-panel-2 border border-border'
                }`}
              >
                <span className={`absolute top-0.5 left-0.5 w-4 h-4 bg-white rounded-full shadow transition-transform ${
                  settings.updatesAutoCheck ? 'translate-x-4' : 'translate-x-0'
                }`} />
              </button>
              <span className="text-[12.5px] text-fg-muted">
                Controleer bij het opstarten en daarna elke 6 uur
              </span>
            </div>

            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Token</label>
              <input
                type={showUpdatesToken ? 'text' : 'password'}
                value={settings.updatesGithubToken}
                onChange={e => update('updatesGithubToken', e.target.value)}
                placeholder="leeg = token van de plugin-repo"
                className={inputClass}
              />
              <button
                onClick={() => setShowUpdatesToken(v => !v)}
                className="text-[11.5px] text-fg-muted hover:text-fg shrink-0"
              >
                {showUpdatesToken ? 'verberg' : 'toon'}
              </button>
            </div>

            <div className="px-4 py-3">
              <button
                onClick={async () => {
                  try {
                    setUpdateLog(await Services.UpdateService.InstallLog() || 'Nog geen update-log.')
                  } catch (e) {
                    setUpdateLog(String(e))
                  }
                }}
                className="text-[11.5px] text-accent hover:underline"
              >
                Toon update-log
              </button>
              {updateLog !== null && (
                <pre className="mt-2 max-h-48 overflow-auto text-[11px] font-mono text-fg-muted
                                bg-bg border border-border rounded-[8px] p-2 whitespace-pre-wrap">
                  {updateLog}
                </pre>
              )}
            </div>
          </div>
        </section>
```

- [ ] **Step 7: Typecheck and build**

Run: `cd frontend && npx tsc --noEmit && cd .. && go test ./... && wails3 task build`
Expected: geen typefouten, tests groen, build slaagt.

- [ ] **Step 8: Commit**

```bash
git add internal/services/settings_service.go internal/services/settings_service_test.go frontend/src/components/SettingsPage.tsx frontend/bindings
git commit -m "feat: sectie App-updates in Instellingen met handmatige check"
```

---

### Task 10: Release-workflow met changelog en versiestempel

**Files:**
- Create: `.github/scripts/changelog.sh`
- Modify: `.github/workflows/release.yml` (checkout-diepte, changelog-stap, `APP_VERSION`, plist-stempel, body)

**Interfaces:**
- Consumes: `internal/services/update_notes.go` verwacht een `## Wijzigingen`-sectie met de subkoppen `### Nieuw`, `### Opgelost` en `### Overig` (Task 5); de Taskfile leest `APP_VERSION` (Task 1).
- Produces: `.github/scripts/changelog.sh <tag>` schrijft de markdown-sectie naar stdout.

- [ ] **Step 1: Write the changelog script**

Create `.github/scripts/changelog.sh`:

```bash
#!/usr/bin/env bash
# Bouwt de "## Wijzigingen"-sectie voor de release-notes uit de commits sinds de
# vorige v-tag. De app parseert precies deze vorm (zie
# internal/services/update_notes.go), dus de koppen moeten letterlijk
# "### Nieuw", "### Opgelost" en "### Overig" heten.
#
# Gebruik: .github/scripts/changelog.sh v0.2.10
set -euo pipefail

TAG="${1:?geef de tag mee, bijvoorbeeld v0.2.10}"

# --match voorkomt dat de bewegende tag kinsta-latest als "vorige versie" wordt
# gekozen; die staat immers ook in de tag-lijst.
PREV="$(git describe --tags --abbrev=0 --match 'v*.*.*' "${TAG}^" 2>/dev/null || true)"
if [ -n "$PREV" ]; then
  RANGE="${PREV}..${TAG}"
else
  RANGE="$TAG"
fi

SUBJECTS="$(git log --no-merges --pretty=format:%s "$RANGE")"

nieuw=""
opgelost=""
overig=""

while IFS= read -r subject; do
  [ -n "$subject" ] || continue
  case "$subject" in
    feat:*|feat\(*\):*)
      nieuw="${nieuw}- ${subject#*: }"$'\n' ;;
    fix:*|fix\(*\):*)
      opgelost="${opgelost}- ${subject#*: }"$'\n' ;;
    *)
      # Overige commits met een conventional-commit prefix laten we de prefix
      # houden: "chore: deps bijwerken" is zonder dat woord minder duidelijk.
      overig="${overig}- ${subject}"$'\n' ;;
  esac
done <<< "$SUBJECTS"

if [ -z "$nieuw" ] && [ -z "$opgelost" ] && [ -z "$overig" ]; then
  exit 0
fi

echo "## Wijzigingen"
echo
if [ -n "$nieuw" ]; then
  echo "### Nieuw"
  printf '%s' "$nieuw"
  echo
fi
if [ -n "$opgelost" ]; then
  echo "### Opgelost"
  printf '%s' "$opgelost"
  echo
fi
if [ -n "$overig" ]; then
  echo "### Overig"
  printf '%s' "$overig"
  echo
fi
```

- [ ] **Step 2: Make it executable and test it against a real range**

Run:
```bash
chmod +x .github/scripts/changelog.sh && .github/scripts/changelog.sh v0.2.9
```
Expected: een `## Wijzigingen`-blok met de commits tussen `v0.2.8` en `v0.2.9`, met minstens één `### `-kop en bullets die met `- ` beginnen.


- [ ] **Step 3: Verify the parser accepts that output**

De uitvoer van het script moet door `parseChangelog` heen komen; anders staat er straks een changelog in de release die de app niet leest. Controleer dat met een tijdelijke test.

Schrijf `internal/services/zz_changelog_echt_test.go`:

```go
package services

import (
	"os"
	"testing"
)

// Tijdelijke controle: de uitvoer van .github/scripts/changelog.sh moet door
// parseChangelog heen komen. Verwijder dit bestand na de controle.
func TestEchteChangelogWordtGeparseerd(t *testing.T) {
	data, err := os.ReadFile("/tmp/changelog.md")
	if err != nil {
		t.Skip("geen /tmp/changelog.md")
	}
	got := parseChangelog(string(data))
	if len(got) == 0 {
		t.Fatalf("parseChangelog gaf geen regels voor:\n%s", data)
	}
	t.Logf("%d regels geparseerd", len(got))
}
```

Run: `.github/scripts/changelog.sh v0.2.9 > /tmp/changelog.md && go test ./internal/services/ -run EchteChangelog -v`
Expected: PASS met een logregel als `4 regels geparseerd`. Krijg je 0 regels, dan wijken de koppen in het script af van wat `changelogKoppen` in `internal/services/update_notes.go` verwacht.

Verwijder daarna het testbestand: `rm internal/services/zz_changelog_echt_test.go`

- [ ] **Step 4: Wire the script into the release workflow**

In `.github/workflows/release.yml`. De checkout heeft de volledige historie nodig, anders vindt `git describe` de vorige tag niet:

```yaml
      - name: Checkout code
        uses: actions/checkout@v4.1.6
        with:
          fetch-depth: 0
```

Voeg ná "Install Task" en vóór "Build and package app" twee stappen toe:

```yaml
      - name: Genereer de changelog
        id: changelog
        run: |
          chmod +x .github/scripts/changelog.sh
          .github/scripts/changelog.sh "${{ github.ref_name }}" > /tmp/changelog.md
          echo "--- changelog ---"
          cat /tmp/changelog.md

      - name: Stempel de versie in Info.plist
        run: |
          VERSION="${GITHUB_REF_NAME#v}"
          /usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString ${VERSION}" build/darwin/Info.plist
          /usr/libexec/PlistBuddy -c "Set :CFBundleVersion ${VERSION}" build/darwin/Info.plist
          plutil -p build/darwin/Info.plist | grep -i version
```

Geef de build de versie mee — pas de bestaande stap aan:

```yaml
      - name: Build and package app
        run: task package
        env:
          CGO_ENABLED: 1
          APP_VERSION: ${{ github.ref_name }}
```

- [ ] **Step 5: Prepend the changelog to the release body**

In de stap "Create GitHub release", in het `script:`-blok. Lees het changelog-bestand en zet het bóven de bestaande installatietekst. Direct ná `const zipPath = path.join('bin', zipName);`:

```javascript
            // De app leest deze sectie uit de release-body en toont hem in de
            // update-popup en het "wat is er nieuw"-venster. Ontbreekt hij
            // (geen commits, of het script faalde), dan valt de app terug op
            // "geen details beschikbaar".
            let changelog = '';
            try {
              changelog = fs.readFileSync('/tmp/changelog.md', 'utf8').trim();
            } catch (e) {
              core.warning(`geen changelog gevonden: ${e.message}`);
            }
```

en maak van de bestaande `const body = \`## Installatie ...\`` een samenstelling — vervang de regel waarop `const body` begint door:

```javascript
            const installatie = `## Installatie
```

(de rest van de bestaande template-string blijft ongewijzigd staan, inclusief het afsluitende backtick met puntkomma), en voeg direct ná die template-string toe:

```javascript
            const body = changelog ? `${changelog}\n\n${installatie}` : installatie;
```

- [ ] **Step 6: Add the keychain and permission note to the release body**

In diezelfde template-string, onderaan bij "## Configuratie", een extra blok. Dit hoort maar bij één release, maar het is de release waarin de app-identiteit verandert:

```javascript
            ## Eenmalig bij deze versie

            De app-identiteit is gewijzigd van \`com.example.rdmsitestool\` naar
            \`nl.nobears.kinsta-updater\`. Daardoor vraagt macOS één keer opnieuw
            toegang tot je projectenmap. Je API-keys worden bij de eerste start
            automatisch overgezet; je hoeft niets opnieuw in te vullen.

            De oude keychain-items blijven staan onder de service
            \`nl.micromanage.rdm-sites-tool\`. Wil je die opruimen: open Keychain
            Access, zoek op die naam en verwijder de gevonden items.

            Vanaf deze versie controleert de app zelf op updates: bij het
            opstarten en daarna elke 6 uur. Je kunt dat uitzetten bij
            Instellingen → App-updates.`;
```

Let op dat de template-string precies één keer wordt afgesloten; controleer met `node --check` in de volgende stap.

- [ ] **Step 7: Validate the workflow**

Run:
```bash
node -e "const y=require('fs').readFileSync('.github/workflows/release.yml','utf8'); const m=y.match(/script: \|\n([\s\S]*?)\n      - name|script: \|\n([\s\S]*)$/); const js=(m&&(m[1]||m[2])||'').replace(/^\s{12}/gm,''); new Function(js); console.log('javascript in de workflow is syntactisch geldig')"
```
Expected: `javascript in de workflow is syntactisch geldig`. Faalt dit met een syntaxfout, dan is de template-string niet goed afgesloten.

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/release.yml')); print('yaml ok')"`
Expected: `yaml ok`.

- [ ] **Step 8: Commit**

```bash
git add .github/scripts/changelog.sh .github/workflows/release.yml
git commit -m "ci: changelog uit commits en versiestempel in de release"
```

- [ ] **Step 9: Push the branch and open a PR**

```bash
git push -u origin HEAD
gh pr create --title "Zelf-update van de app" --body "$(cat <<'PRBODY'
## Wat dit toevoegt

De tool controleert bij het opstarten, elke 6 uur en op verzoek of er een nieuwere release op GitHub staat, vraagt of die geïnstalleerd mag worden, vervangt zichzelf via een helper-script inclusief de nabewerkingscommando's, en laat na de herstart zien wat er veranderd is.

- `internal/version` — versiebesef via ldflags, plus een `--version` vlag
- `internal/adapters/github/releases.go` — laatste release en asset-download met voortgang
- `internal/services/update_*.go` — check-loop (4x per dag), state, changelog-parser, installatie
- `frontend/src/components/AppUpdateDialog.tsx` + badge in de rail + sectie in Instellingen
- `.github/scripts/changelog.sh` — `## Wijzigingen` uit de commits sinds de vorige tag

## Meegenomen opruiming

App-identiteit van `com.example.rdmsitestool` naar `nl.nobears.kinsta-updater`, met een eenmalige keychain-migratie zodat niemand zijn API-keys opnieuw hoeft in te vullen. macOS vraagt daardoor één keer opnieuw om toegang tot de projectenmap.

## Test plan

- [x] `go test ./...`
- [x] `go vet ./...`
- [x] `cd frontend && npx tsc --noEmit`
- [x] `wails3 task build`
- [ ] Testtag pushen (bijv. `v0.2.10`) en de volledige flow op een echte installatie draaien: popup, download, swap, keychain-migratie, permissie-herprompt, herstart en het "wat is er nieuw"-venster
PRBODY
)"
```

---

## Verificatie na het samenvoegen

Deze stappen kan een agent niet doen; ze vragen een echte release en een echte installatie.

- [ ] Tag `v0.2.10` pushen en wachten tot de workflow klaar is.
- [ ] Controleren dat de release-notes een `## Wijzigingen`-sectie hebben met de commits van deze branch.
- [ ] De zip handmatig installeren in `/Applications` (dit is de laatste keer dat dat nodig is) en controleren dat de app opent, dat de keychain-migratie de API-keys heeft meegenomen, en dat Instellingen → App-updates de juiste versie toont.
- [ ] Daarna `v0.2.11` taggen met één triviale commit erin, en in de draaiende app de popup afwachten of op "Nu controleren" klikken. Installeren, en controleren dat: de voortgang loopt, de app afsluit en zelf opnieuw start, `~/.config/rdm/logs/update-*.log` de stappen bevat, de `.app.bak` is opgeruimd, en het "Bijgewerkt naar v0.2.11"-venster de wijzigingen toont.
- [ ] Controleren dat de Playwright-stap in het log wordt overgeslagen wanneer chromium al goed staat.
