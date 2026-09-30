# WordPress-updates via de bridge: inzicht, uitvoeren en vlootoverzicht

**Datum:** 2026-09-30 · **Status:** ontwerp goedgekeurd door Jeffrey, spec ter review
**Bouwt voort op:** `2026-09-29-wp-fleet-sites-design.md` (WPSite, Keychain `wp:<id>`, `WPSiteService`)

## Doel

Voor elke gekoppelde WordPress-site — ongeacht provider (Kinsta, TransIP, …) — in de
tool zien welke core-, plugin-, thema- en vertaalupdates er klaarstaan, die updates
vanuit de tool uitvoeren, en één overzicht over de hele vloot. De data komt rechtstreeks
van de `nb-mcp-bridge` mu-plugin (dezelfde bron als wp-fleet's `list_updates`), zonder
Docker.

Na een update op de **SatisPress-depotsite** moet git bijblijven:
- plugins → de bestaande `UpdateWatcher` in `satispress-theme` stuurt al een
  `repository_dispatch`, waarna `.github/workflows/plugin-update.yml` de PR maakt (zo
  ontstonden PR #42–#45). De bridge update via `Plugin_Upgrader::bulk_upgrade`, dat
  `upgrader_process_complete` afvuurt, dus de tool hoeft hier niets te doen;
- core → WordPress komt in `web-satispress` uit Composer (`roots/wordpress`, map
  `public/wp`, niet in git). De tool maakt na een geslaagde live core-update zelf een PR
  met de bijgewerkte `composer.json`/`composer.lock`.

## Niet in scope

- Thema's bijwerken (alleen tonen).
- Composer-PR's voor klantsites (deel C, later — raakt het Renovate-plan).
- Bulk-uitvoeren over meerdere sites tegelijk.
- Wijzigingen aan de bridge of aan `web-satispress`.

## Gegevensmodel

`domain.WPSite` krijgt één veld:

```yaml
depot: true   # dit is de SatisPress-depotsite
```

- JSON `depot`, YAML `depot`, `omitempty`. Hooguit één site mag `depot: true` hebben
  (validatie in `validateWPSites`, melding `site "<id>" is al de depotsite`).
- Wordt, net als `bridge`, bij `Update` gewoon uit het formulier overgenomen (anders dan
  `bridge` is dit een gebruikerskeuze).

## Backend

### Lezen — `internal/services/wpsite_updates.go`

`func (s *WPSiteService) AvailableUpdates(siteID string, refresh bool) (SiteUpdates, error)`

- Site via `find`, wachtwoord via `secret`. Geen wachtwoord → fout
  `geen Application Password opgeslagen`. `site.Bridge == false` → fout
  `nb-mcp-bridge is niet actief op deze site — test eerst de verbinding`.
- `GET <url>/wp-json/nb-mcp/v1/updates` (`?refresh=1` bij refresh), Basic Auth,
  `Accept: application/json`. Timeout 15 s, bij refresh 60 s (bridge vraagt dan zelf
  wp.org opnieuw).
- Foutcodes: 401 → `gebruiker of Application Password klopt niet`; 403 → de `message` uit
  de WordPress-foutbody (bijv. `DISALLOW_FILE_MODS` op Composer-sites; `update_plugins`
  ontbreekt); 404 → `nb-mcp-bridge ontbreekt op deze site`; overig/netwerk →
  `site niet bereikbaar (HTTP <code>)`. Nooit het wachtwoord in een melding.

```go
type SiteUpdates struct {
	SiteID       string            `json:"siteId"`
	CheckedAt    string            `json:"checkedAt"`
	Core         []CoreUpdateOffer `json:"core"`
	Plugins      []ItemUpdate      `json:"plugins"`
	Themes       []ItemUpdate      `json:"themes"`
	Translations int               `json:"translations"`
}
type CoreUpdateOffer struct { Current, Version, Type string } // Type: major | minor
type ItemUpdate struct { ID, Name, CurrentVersion, NewVersion string } // ID = plugin-bestand of stylesheet
```

(JSON-tags camelCase.) Bridge-velden: `checked_at`, `core[].{current,version,type}`,
`plugins[].{plugin,name,current_version,new_version}`,
`themes[].{stylesheet,name,current_version,new_version}`, `translations.count`.

`func (s *WPSiteService) FleetUpdates(refresh bool) ([]FleetSiteUpdates, error)`

- Alle sites met wachtwoord; concurrency 4; een fout op één site stopt de rest niet.

```go
type FleetSiteUpdates struct {
	Site        WPSiteView  `json:"site"`
	ProjectID   string      `json:"projectId"`   // leeg bij losse site
	Updates     *SiteUpdates `json:"updates"`    // nil bij fout
	Error       string      `json:"error"`
}
```

`ProjectID` wordt via de projectlijst gezocht op `ProjectPath` (nieuwe lookup-methode op
de bestaande `WPProjectLookup`-interface: `List() []domain.Project`).

### Uitvoeren — `internal/services/wpsite_update_run.go`

| Methode | Bridge | Body |
|---|---|---|
| `UpdatePlugins(siteID string, plugins []string, all bool) (RunResult, error)` | `POST /nb-mcp/v1/updates/plugins` | `{"plugins": [...]}`; bij `all` de lijst uit een verse `AvailableUpdates` |
| `UpdateCore(siteID string, allowMajor bool) (RunResult, error)` | `POST /nb-mcp/v1/updates/core` | `{"allow_major": bool}` |
| `UpdateTranslations(siteID string) (RunResult, error)` | `POST /nb-mcp/v1/updates/translations` | — |

- `read_only`-site → fout vóór de netwerkcall: `deze site staat op alleen-lezen`.
- Timeout 5 minuten. Zelfde foutafhandeling als lezen.
- Eén uitvoering per site tegelijk (claim/release per site-id, zoals `WPCoreUpdateService`);
  een tweede → fout `er loopt al een update voor deze site`.

```go
type RunResult struct {
	Items       []RunItem `json:"items"`
	PullRequest string    `json:"pullRequest"` // URL, alleen bij core op de depotsite
	ActionsURL  string    `json:"actionsUrl"`  // alleen bij plugins op de depotsite
	Warning     string    `json:"warning"`     // bijv. PR maken mislukt terwijl update wel lukte
}
type RunItem struct { Name, From, To, Error string; Success bool }
```

### PR na core-update op de depotsite — `internal/services/depot_core_pr.go`

Alleen als `site.Depot && site.ProjectPath != ""` en minstens één core-item `Success`.

1. Doelversie = `To` van het core-item. Branch `update/wordpress-<versie>`.
2. Bestaat er al een open PR voor die branch → die URL teruggeven (idempotent).
3. Tijdelijke worktree vanaf `origin/<default-branch>` (default uit `origin/HEAD`), via de
   bestaande worktree-/push-hulpmiddelen van `WPCoreUpdateService`.
4. In de worktree: past `<versie>` binnen de huidige constraint van `roots/wordpress`,
   dan `composer update roots/wordpress --no-install --no-scripts --no-plugins`; anders
   `composer require roots/wordpress:^<major>.<minor> --no-install --no-scripts
   --no-plugins --no-update` gevolgd door dezelfde `composer update`.
5. Alleen `composer.json`/`composer.lock` stagen; commit
   `chore: WordPress naar <versie>`; push; PR tegen de default-branch met titel
   `WordPress <versie>` en body die vermeldt dat de live site al is bijgewerkt.
6. Worktree altijd opruimen.

- Composer-binary zoeken zoals `docker.DockerBin()`: `RDM_COMPOSER`, PATH, vaste paden
  (`/opt/homebrew/bin/composer`, `/usr/local/bin/composer`,
  `~/Library/Application Support/Herd/bin/composer`), login-shell.
- Mislukt de PR terwijl de live update lukte → `RunResult.Warning` met de reden, geen
  fout (de update is gebeurd; de gebruiker moet de PR zelf maken).

### Actions-link bij plugins op de depotsite

`ActionsURL = https://github.com/<owner>/<repo>/actions/workflows/plugin-update.yml`,
owner/repo uit de git-remote van het gekoppelde project. Geen project → leeg.

## UI

### Blok "Beschikbaar op de site" — `frontend/src/components/SiteUpdatesPanel.tsx`

Props: `siteId`, `isDepot`, `readOnly`. Gebruikt in:
- `UpdatesTab.tsx` bovenaan, als `WPSiteService.ForProject(project.id)` een site geeft
  (los van Kinsta/provider);
- `WPSitePage.tsx` (losse site) onder het formulier.

Inhoud: tijdstip + **Verversen** (refresh); core (huidig → nieuw, major/minor) met
**WordPress bijwerken** en vinkje "ook major toestaan" (standaard uit); plugins-tabel met
vinkje per regel, **Geselecteerde bijwerken** en **Alles bijwerken**; thema's alleen
tonen; vertalingen (aantal) met **Vertalingen bijwerken**. Niets te doen → "Alles is
bijgewerkt".

Bevestiging altijd via `bevestig()` (nooit `window.confirm`):
- depotsite, plugins: "De PR komt via de SatisPress-workflow op GitHub."
- depotsite, core: "Daarna maakt de tool een Composer-PR in <repo>."
- andere site: "Deze wijziging komt niet in git; de volgende deploy draait hem terug."

Na afloop: resultaatlijst per item (✓/✗, van → naar, fout), PR-link of Actions-link,
eventuele waarschuwing, daarna automatisch verversen. Knoppen verspringen niet
(`KnopTekst`-patroon uit `WPSiteForm`). Alleen-lezen-site: uitvoerknoppen uit met uitleg.

### Pagina "WordPress-vloot" — `frontend/src/components/FleetUpdatesPage.tsx`

Nieuw item in de linkerbalk. Tabel met alle gekoppelde sites: naam, core-update ja/nee,
aantal plugin-updates, aantal thema-updates, tijdstip, fout. **Alles verversen**
(refresh); bij openen zonder refresh. Klik op een regel → project (Updates-tab) of losse
site. Sortering: sites met updates bovenaan, dan fouten, dan bijgewerkt.

### Formulier

`WPSiteForm` krijgt het vinkje **"Dit is de SatisPress-depotsite"**.

## Foutafhandeling

| Situatie | Gedrag |
|---|---|
| Geen koppeling bij project | blok niet tonen |
| Geen wachtwoord / bridge uit | melding in het blok, geen netwerkcall |
| 401 / 403 / 404 / onbereikbaar | melding zoals hierboven, update-branches blijven werken |
| Composer-site (`DISALLOW_FILE_MODS`) | 403-melding van de bridge doorgeven |
| Read-only-site | uitvoerknoppen uit; backend weigert ook |
| Tweede update tegelijk op dezelfde site | fout "er loopt al een update" |
| Live update ok, PR mislukt | resultaat + waarschuwing met reden |
| Eén site faalt in vlootoverzicht | alleen die regel toont de fout |

## Testen

- **Go (TDD):** `httptest`-TLS-server voor `AvailableUpdates` (gewone lijst, refresh-param,
  401/403-met-body/404/500, bridge uit, geen wachtwoord, wachtwoord nooit in fout),
  `FleetUpdates` (mix van ok en fout, concurrency ≤ 4), `UpdatePlugins`/`UpdateCore`/
  `UpdateTranslations` (body, resultaat-mapping, read-only-weigering, dubbele claim),
  depot-core-PR met nep-git/composer/pull-client (bestaande PR, binnen/buiten constraint,
  alleen composer-bestanden gestaged, worktree opgeruimd, PR-fout → warning), depot-uniek
  in `validateWPSites`, composer-binary-zoeker.
- **Frontend:** `tsc --noEmit` + `npm run build:dev`.
- **Handmatig:** op de SatisPress-site: overzicht tonen, één plugin bijwerken → PR via de
  workflow verschijnt, core-minor bijwerken → Composer-PR; vlootoverzicht met minstens één
  foutende site.
