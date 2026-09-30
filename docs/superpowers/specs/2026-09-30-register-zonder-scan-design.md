# Zelfbeheerd register in plaats van mapscan

**Datum:** 2026-09-30 · **Status:** ontwerp goedgekeurd door Jeffrey, spec ter review
**Bouwt voort op:** `2026-09-29-wp-fleet-sites-design.md`, `2026-09-30-wp-updates-via-bridge-design.md`

## Doel

De projectlijst komt niet meer uit een scan van `~/Projects`. De gebruiker voegt zelf toe:
- **WordPress-sites** (via de bridge: URL, WP-user, Application Password), optioneel gekoppeld aan een lokale map;
- **lokale projecten** (niet-WordPress: Laravel, Pimcore, overig) door een map te kiezen.

Sites zonder lokale map zijn volwaardig te beheren voor alles wat via de bridge of de Kinsta-API loopt. Functies die een lokale checkout nodig hebben staan in het menu zichtbaar maar uitgeschakeld tot er een koppeling is. De Kinsta-tab verdwijnt; de Kinsta-koppeling wordt een veld van de site.

## Migratie via de bridge — besluit

Migratie (database + uploads van productie naar lokaal en terug, met klonen/anonimiseren) blijft via SSH en een lokale koppeling lopen. Een bridge-route die een volledige database of alle uploads over HTTP levert is afgewezen: klant-PII achter één Application Password, timeouts bij grote sites, en SSH/Kinsta-back-ups doen dit al. In het menu valt Migratie daarom onder "vereist lokale map + Kinsta".

## Niet in scope

- Media-analyse zonder SSH (via REST/bridge) — apart ontwerp.
- Non-Kinsta SSH-hosts (TransIP) voor Logs/Terminal — ongewijzigd (werken zoals nu via `.rdm`-SSH waar dat al kon).
- Wijzigingen aan de bridge.

## Gegevensmodel

### `~/.config/rdm/wp-sites.yml` (bestaand)

`domain.WPSite` krijgt:

```yaml
kinsta_site_id: 1a2b3c…   # optioneel
kinsta_env_id: 4d5e6f…    # optioneel, productieomgeving
```

JSON `kinstaSiteId`, `kinstaEnvId`; beide `omitempty`.

### `~/.config/rdm/local-projects.yml` (nieuw)

```yaml
projects:
  - path: ~/Projects/web-sponsoreenpupnl
    name: Sponsor een pup
```

Regels: `path` verplicht, uniek, opgeslagen met `~`; `name` optioneel (default: mapnaam). Een pad mag niet tegelijk een site-`project_path` zijn (validatie over beide bestanden bij opslaan). Zelfde opslagregels als `wp-sites.yml` (0600, atomisch).

### `projects_roots`

Blijft in `config.yml` staan maar wordt alleen nog gelezen door het importvenster. Nieuwe instelling `clone_dir` (default `~/Projects`) voor Org-sync.

## Project-ID's

| Item | ID | `Path` | `Local` |
|---|---|---|---|
| Site met map | `sha256(pad)[:8]` hex (ongewijzigd) | pad | true |
| Lokaal project | idem | pad | true |
| Site zonder map | `site:<site-id>` | leeg | false |

Zo blijven `test-runs/`, `media-scans/`, `reports/`, `db-clones/` (gekeyd op project-ID) werken zonder migratie. `domain.Project` krijgt `SiteID string` en `Local bool`. Een gekoppelde map die niet bestaat: project blijft in de lijst met `Local: true`, `Missing: true` (bestaand `projectMissing`-idee), git-status leeg.

## Backend

- **`ProjectService`**: `Scan()` wordt `Reload()`: leest `wp-sites.yml` + `local-projects.yml`, bouwt `[]domain.Project` (voor lokale items zoals nu `.rdm/config.yml`, `deploy_conf.json`, git-status; voor sites zonder map: `DisplayName` = site-naam, `Provider` kinsta als `kinsta_site_id` gezet, `Deploy.Link.Prod` = site-URL). `AddRoot`/`RemoveRoot`/`GetRoots` verdwijnen uit de bindings.
- **Nieuwe `LocalProjectService`** (of methodes op `ProjectService`): `AddLocalProject()` (map-dialoog), `RemoveLocalProject(id)`, `LinkSiteFolder(siteID)` (map-dialoog → `WPSite.ProjectPath`), `UnlinkSiteFolder` = bestaand `WPSiteService.Unlink`.
- **`pathFor`-guard**: elke service die een map nodig heeft krijgt bij `Path == ""` de fout `koppel eerst een lokale map`. Centraal via één helper `requireLocal(p domain.Project) (string, error)` in `internal/services`.
- **Kinsta-koppeling**: `KinstaService.GetLinkedSiteID(projectID)` → eerst `WPSite.KinstaSiteID` van de bijbehorende site, anders `Config.Kinsta.SiteID` uit `.rdm`. `LinkSite` schrijft voortaan naar de site (`WPSiteService.Update`). `EnvironmentSSH`, Logs, VulnScan, Report volgen `GetLinkedSiteID`. Nieuwe `KinstaService.SuggestSiteForURL(url) (siteID, envID string, ok bool)` (match op de primaire domeinen van de Kinsta-omgevingen).
- **Globale pagina's** (Batch, Search, Inventory, WordPress, BulkUpdate, Wordfence, Org-sync-status) itereren over `Local && !Missing` projecten.
- **Org-sync**: kloont naar `clone_dir`; na een geslaagde clone wordt de map als lokaal project toegevoegd (of, als er een site met dezelfde repo-URL/`deploy_conf` link.prod bestaat, als map aan die site gekoppeld).
- **Import** (`ImportService`): `Candidates() []ImportCandidate` (oude scan over `projects_roots`: pad, naam, deploy-type, voorgestelde site met URL uit `deploy_conf.json`, Kinsta-link uit `.rdm`), `Apply(selection []ImportChoice) error` (maakt sites met `project_path` — zonder wachtwoord — of lokale projecten), `Needed() bool` (register leeg en `projects_roots` gevuld). Sites zonder wachtwoord verschijnen met "geen wachtwoord".

## UI

### Zijbalk
- Eén lijst (sites + lokale projecten) met label **lokaal** / **online** / **map niet gevonden**; bestaande deploy-type-chips + chip **alleen online**.
- Knop **+** → keuzemenu **WordPress-site toevoegen** (siteformulier, map optioneel) / **Lokaal project toevoegen** (map-dialoog).
- Weg: scanmappen-beheer, Scan-knop, aparte sectie "WordPress-sites". Blijft: **Verversen** (git-status van lokale items).

### Menu per project (`ProjectDetail.tsx`)

Ontbrekende vereiste → item zichtbaar, uitgeschakeld, slotje + tooltip (`vereist een lokale map` / `vereist een Kinsta-koppeling` / `vereist een bridge-koppeling`); klik opent **Projectinstellingen**.

| Groep | Tab | Vereist |
|---|---|---|
| OVERVIEW | Info | — |
| SOURCE CONTROL | alle tabs | lokale map |
| WORDPRESS | Updates | bridge-koppeling (branches-deel alleen met lokale map) |
| WORDPRESS | Plugins, Security, Tests | lokale map |
| WORDPRESS | Logs | Kinsta-koppeling |
| WORDPRESS | Media-analyse | Kinsta-koppeling + lokale map |
| MIGRATIE | Database, Media, Instellingen, DB-editor | lokale map + Kinsta-koppeling |
| TOOLS | Terminal | Kinsta-koppeling |
| KLANT | Rapportage | lokale map |
| INSTELLINGEN | Projectinstellingen | — |

De Kinsta-tab verdwijnt. Header-knoppen (editor/fetch/pull/push) alleen met lokale map.

### Projectinstellingen
- Blok **Lokale map**: huidig pad of "niet gekoppeld", knoppen **Map kiezen** / **Ontkoppelen**.
- Siteformulier: nieuw veld **Kinsta-site** (select uit `KinstaService.ListSites`, met voorstel op URL; leeg = geen koppeling) en daarmee de productie-omgeving.
- Voor een lokaal project (geen site): knop **Als WordPress-site koppelen** opent het siteformulier met dit pad.

### Importvenster
Modaal bij `ImportService.Needed()`: lijst met vinkjes (WordPress-mappen aangevinkt), per regel type en voorgestelde URL; knoppen **Importeren** / **Overslaan** (overslaan = leeg beginnen; venster komt niet terug zolang het register niet leeg is).

## Foutafhandeling

| Situatie | Gedrag |
|---|---|
| Service met map aangeroepen zonder map | fout `koppel eerst een lokale map` |
| Gekoppelde map weg | label "map niet gevonden", lokale tabs uitgeschakeld met die uitleg |
| Pad dubbel (site én lokaal project) | opslaan geweigerd met melding |
| Kinsta-API niet bereikbaar in formulier | veld toont fout, overige velden bruikbaar |
| Import: map zonder `deploy_conf.json` | wordt lokaal project, niet aangevinkt |

## Testen

- **Go (TDD):** `local-projects.yml` roundtrip/validatie; `Reload` levert juiste ID's/`Local`/`Path` voor de drie soorten; pad-botsing over beide bestanden; `requireLocal`-fout; `GetLinkedSiteID` voorkeur site > `.rdm`; `SuggestSiteForURL`; `ImportService.Candidates/Apply/Needed`; Org-sync voegt geklonde map toe.
- **Frontend:** `tsc --noEmit` + `build:dev`.
- **Handmatig:** eerste start met importvenster; site zonder map toevoegen → Updates werkt, lokale tabs op slot; map koppelen → tabs vrij; Kinsta-site kiezen → Logs/Terminal werken; historie (testruns/rapporten) van een geïmporteerd project nog zichtbaar.
