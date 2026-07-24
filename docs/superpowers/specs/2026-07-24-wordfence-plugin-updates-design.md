# Wordfence-kwetsbaarheden & bulk plugin-updates — Design

**Datum:** 2026-07-24
**Status:** Goedgekeurd (ontwerp), klaar voor implementatieplan

## Doel

Een nieuw globaal menu-item waarmee de gebruiker de Wordfence vulnerability-feed
ophaalt en lokaal opslaat, deze vergelijkt met de WordPress-plugins van alle
gescande projecten, en kwetsbare plugins in één handeling bijwerkt: per project
een update-branch afgetakt van de release-branch, met een stash-melding als de
worktree niet schoon is.

## Beslissingen

| Onderwerp | Keuze |
|-----------|-------|
| Pluginbron | Lokale repos: `wp-content/plugins/*/`, versie uit plugin-header |
| Update-mechanisme | Download laatste stabiele versie van wp.org, vervang bestanden, commit |
| Betaalde/onbekende plugins | Niet automatisch updaten; markeren als "handmatig" |
| Branch-basis | Default branch van de repo (conventie: `release/*`); anders overslaan met waarschuwing |
| Doelversie | Laatste stabiele versie (wp.org) |
| Output | Lokale branch + commit (geen push/PR) |
| Feed-opslag | Volledige feed cachen; UI toont laatste 50 met "meer laden" |
| API-key | Instelbaar in settings, opgeslagen in macOS keychain |

## Architectuur

### Backend (Go)

**Config** — `internal/config/schema.go` + `global.go`:

```go
type WordfenceGlobal struct {
    APIKey string `yaml:"api_key"` // keychain:rdm.wordfence.apiKey of literal (dev)
}
// Global krijgt veld: Wordfence WordfenceGlobal `yaml:"wordfence"`
```

Hergebruikt bestaand `ResolveSecret` / `keychain:`-patroon.

**Adapters:**

- `internal/adapters/wordfence/client.go`
  - `Fetch(ctx, apiKey) ([]byte, error)` — GET
    `https://www.wordfence.com/api/intelligence/v3/vulnerabilities/production`
    met header `Authorization: Bearer <key>`.
  - Parse-functie naar `domain.Vulnerability`: title, cve, cvss-score + severity,
    published/updated datum, en `software[]` met `{type, slug, affected_versions
    (range from/to + inclusiviteit), patched, patched_versions}`.
  - Alleen entries met `type == "plugin"` zijn relevant voor matching.

- `internal/adapters/wporg/client.go`
  - `LatestVersion(ctx, slug) (version string, downloadURL string, err error)` via
    `https://api.wordpress.org/plugins/info/1.0/{slug}.json` (velden `version`,
    `download_link`). Slug niet gevonden → sentinel-fout `ErrNotOnWporg`.
  - `Download(ctx, url) ([]byte, error)`.

- `internal/adapters/wpplugins/reader.go`
  - `ReadInstalled(projectPath) ([]InstalledPlugin, error)` — zoekt
    `wp-content/plugins/*/` (op elke diepte binnen de repo; eerste match telt),
    slug = mapnaam, versie uit `Version:`-header in het hoofd-PHP-bestand,
    fallback `Stable tag:` in `readme.txt`.

**Services:**

- `internal/services/wordfence_service.go`
  - `Refresh(ctx) error` — fetch via adapter, schrijf raw JSON naar
    `~/.config/rdm/wordfence-production.json` + meta (`fetched_at`, `count`).
  - `List() ([]domain.Vulnerability, error)` — lees + parse cache.
  - `LastFetched() (time.Time, int)`.
  - `MatchProjects() ([]ProjectVulnReport, error)` — voor elk gescand project:
    lees geïnstalleerde plugins, kruis met de plugin-vulns uit de feed via
    version-range-matching; per match: aanbevolen laatste versie (wp.org) +
    bron-status (`wporg` / `manual`).

- `internal/services/wordfence_update_service.go`
  - `Plan(selection) ([]ProjectUpdatePlan, error)`.
  - Per project bij uitvoeren:
    1. Bepaal default branch; als die niet aan `release/*` voldoet → status
       `skipped_no_release`, waarschuwing.
    2. `GetStatus` — dirty worktree → status `needs_stash` (pauzeert dit project).
    3. `CreateBranch("security/wordfence-YYYY-MM-DD", from=defaultBranch)`.
    4. Per plugin: download laatste zip van wp.org, unzip, vervang
       `wp-content/plugins/{slug}`, `git add`, commit
       `fix(security): update {slug} {oud}→{nieuw} (Wordfence)`.
    5. Rapporteer per plugin/project.
  - `StashAndContinue(projectID)` — `StashSave` daarna verdergaan.
  - Hergebruikt `GitService` (CreateBranch, GetStatus, StashSave, StageAll, Commit).

- Kleine helper `version_compare` (PHP-stijl, dot-split numeriek met
  string-fallback) voor range-matching; geen externe semver-lib (plugin-versies
  zijn niet strikt semver).

### Frontend (React)

- **Globaal menu-item** in de sidebar-header (`App.tsx`), naast Zoeken/Batch:
  nieuwe state `showWordfence`, wederzijds uitsluitend met de andere panelen.
- **`frontend/src/components/WordfencePage.tsx`:**
  - Kop: "Vernieuwen"-knop, laatst-opgehaald-tijd + aantal vulns.
  - Kwetsbaarhedenlijst: laatste 50 (op published-datum), knop "meer laden".
  - "Vergelijk met projecten" → per-project tabel: plugin-slug, huidige versie →
    veilige versie, CVE/severity, bron-status. Checkboxes (default alles
    aangevinkt) om te de-selecteren.
  - "Update geselecteerde" → voortgang + resultaat per project.
  - Dirty worktree → inline waarschuwing met knop "Stash & doorgaan".
- **`SettingsPage.tsx`:** nieuwe sectie "Wordfence" met API Key-veld
  (keychain-opslag, zoals Kinsta/Anthropic).

## Data-flow

1. Open Wordfence-view → Vernieuwen → adapter fetcht feed → cache op schijf + parse.
2. Vergelijk → service leest lokale plugins per project, matcht → findings.
3. Selecteer + Update → per project: release-branch-check → dirty-check
   (stash-prompt) → branch → download+vervang+commit → rapport.

## Foutafhandeling

- Ontbrekende API-key → duidelijke melding + link naar settings.
- Netwerk-/downloadfout bij fetch of plugin → per item, overige gaan door.
- Slug niet op wp.org → status "handmatig", niet geüpdatet.
- Default branch ≠ `release/*` → project overgeslagen met waarschuwing.
- Dirty worktree → project pauzeert tot de gebruiker stash kiest.

## Tests

- Wordfence-feed-parsing + version-range-matching (unit).
- `version_compare` helper (unit, randgevallen).
- wp.org-client met fake HTTP-server.
- Plugin-header/readme-reader (fixtures).
- Update-runner tegen tijdelijke git-repo (bestaand testpatroon in git-tests).

## Scope-grenzen (YAGNI)

- Geen push/PR-automatisering (blijft lokaal).
- Geen betaalde-plugin-download (alleen wp.org; betaald = handmatig markeren).
- Geen minimale-patch-versie-logica (altijd laatste stabiel).
- Geen achtergrond-scheduler; ophalen/vergelijken is handmatig getriggerd.
