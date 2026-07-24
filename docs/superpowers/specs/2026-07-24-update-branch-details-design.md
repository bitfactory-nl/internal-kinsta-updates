# Ontwerp: update-details in PR én in de tool

Datum: 2026-07-24
Status: goedgekeurd (brainstorm)
Betrokken repos: `internal-kinsta-updates` (deze repo, reusable workflow) + de RDM Sites Tool (Go/Wails + React frontend, deze repo).

## Doel

Twee samenhangende wensen:

1. **In de PR** duidelijk terugzien wélke updates zijn uitgevoerd én wélke
   **npm major-updates** beschikbaar zijn maar bewust **niet** automatisch worden
   uitgevoerd (dat is meerwerk richting klanten).
2. **In de tool** per update-branch alle uitgevoerde/beschikbare updates
   (WordPress core, plugins, thema's én npm) kunnen inzien door de branch uit te
   klappen.

Beide features delen één databron: een gestructureerd manifest dat de workflow
in de update-branch schrijft.

## Scope-beslissingen (uit brainstorm)

- De sectie "beschikbare majors, niet auto-uitgevoerd" in de PR is **npm-only**.
  WordPress core/plugins/thema's blijven gerapporteerd zoals nu (het bestaande
  gedrag verandert niet).
- De tool-uitklap toont **alle** types (WordPress core, plugins, thema's, npm).
- De tool-uitklap moet **voor alle branches** werken: manifest wanneer aanwezig,
  anders een fallback die het bestaande `.wp-update-log` + de `package.json`-diff
  parseert.
- `--target minor` blijft de enige die daadwerkelijk npm-versies bumpt. Majors
  worden nooit toegepast via de action — enkel **gerapporteerd**.
- De tool is en blijft **read-only** t.o.v. de klant-repo's (alleen `git show` /
  `git diff`).

## Data-contract: `.updates.json`

De workflow schrijft dit bestand bij elke check in de update-branch, naast het
bestaande `.wp-update-log`.

```json
{
  "generatedAt": "2026-07-20T09:00:57Z",
  "wordpress": {
    "core":    [{ "version": "6.9.5", "updateType": "minor" },
                { "version": "7.0.2", "updateType": "major" }],
    "plugins": [{ "name": "acfml", "from": "2.2.3", "to": "2.2.4" }],
    "themes":  []
  },
  "npm": {
    "applied":         [{ "name": "sass", "from": "1.99.0", "to": "1.101.7", "type": "minor" }],
    "availableMajors": [{ "name": "eslint", "from": "9.39.2", "to": "10.7.0" }]
  }
}
```

Veldnotities:

- `wordpress.core[].updateType`: `"minor"` of `"major"`, afgeleid uit
  `wp core check-update`.
- `wordpress.plugins[]` / `themes[]`: `name`, `from`, `to` uit
  `wp plugin list --update=available` resp. `wp theme list ...`.
- `npm.applied[].type`: `"minor"` of `"patch"`, afgeleid uit de from/to-versies.
- `npm.availableMajors[]`: npm-packages waarvan de major-versie verspringt en die
  dus niet door `--target minor` zijn opgepakt.

## Deel A — Workflow + PR

Wijzigingen in `.github/workflows/check-updates.yml`:

1. **WP-output parsen.** In de bestaande `github-script`-stap de SSH-stdout
   (`WORDPRESS CORE` / `PLUGINS` / `THEMES` tab-tabellen) parsen naar de
   `wordpress.*`-structuur.
2. **npm-majors bepalen.** Een **tweede, read-only ncu-pass** toevoegen in de
   npm-stap: `npx npm-check-updates --jsonUpgraded` (default target = latest).
   Bepaal `availableMajors` = upgrades waar `major(from) !== major(to)`, minus de
   packages die al door de `--target minor`-pass zijn toegepast.
3. **Manifest committen.** `.updates.json` als extra blob meenemen in de
   branch-commit (naast `.wp-update-log` en, indien npm-wijzigingen,
   `package.json` + `package-lock.json`).
4. **PR-body** krijgt drie secties:
   - `### WordPress updates` (ongewijzigd)
   - `### NPM updates (minor + patch)` — uitgevoerd (ongewijzigd)
   - `### ⚠️ Beschikbare major updates — NIET automatisch uitgevoerd (meerwerk)`
     — lijst met npm-majors (`name from → to`); sectie alleen tonen als er zijn.

De manifest-generatie draait ongeacht of er wijzigingen zijn, maar de branch/PR
wordt (net als nu) alleen aangemaakt wanneer er WP- of npm-updates zijn.

## Deel B — Tool: uitklapbare update-branch

### Backend (Go, `internal/services/git_service.go`)

Nieuw type dat het manifest spiegelt, plus een `source`-indicatie. `PackageUpdate`
bevat `Name`, `From`, `To` en een optioneel `Type` (`"minor"`/`"patch"`, gebruikt
voor `NpmApplied`); `WPCoreUpdate` bevat `Version` + `UpdateType`.

```go
type PackageUpdate struct {
    Name string `json:"name"`
    From string `json:"from"`
    To   string `json:"to"`
    Type string `json:"type,omitempty"` // "minor" | "patch" (npm applied)
}

type UpdateDetail struct {
    Source          string              `json:"source"` // "manifest" | "fallback"
    GeneratedAt     string              `json:"generatedAt,omitempty"`
    WPCore          []WPCoreUpdate      `json:"wpCore"`
    WPPlugins       []PackageUpdate     `json:"wpPlugins"`
    WPThemes        []PackageUpdate     `json:"wpThemes"`
    NpmApplied      []PackageUpdate     `json:"npmApplied"`
    NpmAvailableMajors []PackageUpdate  `json:"npmAvailableMajors"`
}
```

Nieuwe methode (lazy — alleen bij uitklappen aangeroepen):

```go
func (s *GitService) GetUpdateBranchDetail(projectID, shortName string) (*UpdateDetail, error)
```

Strategie:

1. `git show <ref>:.updates.json` → parse → `source = "manifest"`.
2. **Fallback** wanneer het manifest ontbreekt (`source = "fallback"`):
   - `git show <ref>:.wp-update-log` → parse de CORE/PLUGINS/THEMES-tabellen.
   - npm: `git diff <base>...<ref> -- package.json` → afgeleide `NpmApplied`
     (from→to). `NpmAvailableMajors` blijft leeg.
3. `<ref>` = `origin/<shortName>` (remote), zodat het werkt zonder lokale
   checkout. `<base>` = de default branch van het project.

### Frontend (`frontend/src/components/UpdatesTab.tsx`)

- Elke branch-rij wordt uitklapbaar met een chevron (▸/▾). State
  `expanded: Set<shortName>`; het detail wordt per branch **lazy** geladen en
  gecached.
- Uitgeklapt tonen we gegroepeerde secties met tellingen:
  - **WordPress** — core (minor/major-badge), plugins (from→to), thema's
  - **NPM — uitgevoerd** (minor/patch, from→to)
  - **⚠️ NPM majors — niet uitgevoerd (meerwerk)** — alleen als er zijn; bij een
    fallback-branch een subtiele notitie dat majors-info niet beschikbaar is.
- Per rij een loading-spinner tijdens ophalen; lege secties worden verborgen.
- Bestaande acties (Schakel / Checkout / ● actief) blijven ongewijzigd naast de
  chevron.

## Componentgrenzen

- **Workflow** (`check-updates.yml`): produceert `.updates.json` + PR-body.
  Enige verantwoordelijkheid: detecteren en rapporteren.
- **Manifest** (`.updates.json`): het contract tussen workflow en tool.
- **Backend** (`GetUpdateBranchDetail`): leest een branch (read-only) en levert
  een `UpdateDetail`. Kent manifest én fallback; geen UI-kennis.
- **Frontend** (`UpdatesTab`): rendert `UpdateDetail`. Geen git-kennis.

## Teststrategie

- **Workflow**: JS-parselogica voor WP-tabellen en de npm-majors-berekening als
  losse, unit-testbare functies (fixtures op basis van echte
  `wp ... list`-output en `ncu --jsonUpgraded`-output).
- **Backend (Go)**: table-driven tests voor de manifest-parse en de
  fallback-parse (`.wp-update-log` + `package.json`-diff), inclusief de
  20-juli-branch als fixture (WP-only, geen npm).
- **Frontend**: rendering-test van `UpdatesTab` met een `UpdateDetail` uit
  manifest én uit fallback (majors-notitie zichtbaar bij fallback).

## Foutafhandeling

- Ontbrekend manifest → fallback (geen fout).
- Ontbrekend `.wp-update-log` én ontbrekend manifest → leeg `UpdateDetail` met
  duidelijke lege-staat in de UI.
- `git show`/`git diff` faalt → fout teruggeven; UI toont een inline
  foutmelding per rij (bestaand patroon).

## Bewust buiten scope (YAGNI)

- WordPress plugin/thema-majors apart flaggen (WP-CLI geeft geen semver-type).
- Majors ergens automatisch toepassen.
- Historische/gemergede branches herstructureren of manifest achteraf toevoegen.
