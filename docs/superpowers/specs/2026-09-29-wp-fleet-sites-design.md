# WordPress-sites koppelen aan wp-fleet

**Datum:** 2026-09-29 · **Status:** ontwerp goedgekeurd door Jeffrey, spec ter review

## Doel

Per WordPress-site de WP-gebruiker en het Application Password in de tool kunnen
opgeven, zonder dat die gegevens in een projectmap of klantrepo staan. De tool wordt
daarmee de bron voor de `wp-fleet-mcp`-container die Claude Code gebruikt: geen
`.env` met wachtwoorden en geen handgeschreven `sites.json` meer in de repo van de
MCP-server.

Twee soorten sites:

1. **Gekoppeld aan een project** — een bestaande map uit de projectenmap. De scan
   blijft zoals hij is; alle huidige features blijven werken.
2. **Losse site** — geen map (bijv. de SatisPress-site). Zelf toe te voegen en te
   verwijderen.

Achtergrond en afweging: onderzoek in
`internal-sites-satispress-mcp/wp-fleet-in-rdm-tool/` (variant *config-hybride*,
aanbevolen als eerste stap).

## Niet in scope

- wp-fleet-image op een vaste versie in een registry publiceren (image-referentie is
  wel instelbaar).
- Go-adapter voor WP REST, drift-dashboard, live data in de tool.
- AI-fix-runs afschermen met `--strict-mcp-config` (aparte taak).
- Wijzigingen aan `wp-fleet-mcp` zelf.
- Team-deling van de sitelijst (lijst is per gebruiker).

## Gegevensmodel

Nieuw bestand `~/.config/rdm/wp-sites.yml` (per gebruiker, rechten 0600):

```yaml
sites:
  - id: ekcoating
    name: EK Coating
    url: https://www.ekcoating.nl
    username: mcp-bot
    project_path: ~/Projects/web-ekcoatingsnl   # leeg = losse site
    tags: [kinsta, production]
    read_only: false
    bridge: true
```

| Veld | Regel |
|---|---|
| `id` | verplicht, uniek, `^[a-z0-9][a-z0-9-]{1,48}$` (zelfde regel als wp-fleet); na aanmaken niet te wijzigen |
| `name` | verplicht |
| `url` | verplicht, `https://`; `http://` alleen voor `localhost`/`*.test`, dan `allowHttp` in `sites.json` |
| `username` | verplicht |
| `project_path` | optioneel; opgeslagen met `~` i.p.v. home-pad; hooguit één site per project |
| `tags`, `read_only`, `bridge` | optioneel; `bridge` wordt door *Test verbinding* gezet |

- **Secret:** Application Password in de Keychain onder de bestaande service
  `nl.nobears.kinsta-updater`, account `wp:<id>`. Nooit in YAML, logs of UI-output.
- **Koppeling via `project_path`, niet via project-ID.** Het project-ID is een hash
  van het pad; bij verplaatsen van een map toont de UI "map niet gevonden" met een
  knop om opnieuw te koppelen, in plaats van dat de koppeling stil verdwijnt.
- **Verwijderen** van een site wist ook het Keychain-item. Ontkoppelen van een
  project maakt er een losse site van (niets gaat verloren); verwijderen is een
  aparte, bevestigde actie.

## Componenten

| Unit | Plaats | Verantwoordelijkheid |
|---|---|---|
| `WPSite` | `internal/domain/wpsite.go` | struct + `Validate()` |
| `WPSiteStore` | `internal/config/wpsites.go` | laden/valideren/atomisch opslaan van `wp-sites.yml` |
| Keychain-fix | `internal/config/keychain.go` | `keychainSetIn` zet het wachtwoord niet meer in argv |
| `WPSiteService` | `internal/services/wpsite_service.go` | Wails-bound: `List`, `Create`, `Update`, `Delete`, `SetPassword`, `TestConnection`, `DetectBridge(projectID)`, `ForProject(projectID)` |
| `wpfleet`-adapter | `internal/adapters/wpfleet/` | `BuildSitesJSON`, `EnvName(id)`, `DockerArgs`, `Launch` (proces starten, streams en signalen doorgeven) |
| `WPFleetService` | `internal/services/wpfleet_service.go` | Wails-bound: `Status` (geregistreerd? aantal sites/ok), `Register`, `Unregister` |
| `mcp-launch` | `main.go` | subcommando vóór Wails-init, roept `wpfleet.Launch` aan |

Globale config krijgt een sectie:

```yaml
wpfleet:
  image: wp-fleet-mcp:latest
  read_only: false        # → MCP_READ_ONLY
```

### Bridge-detectie bij een project

`DetectBridge` zoekt `nb-mcp-bridge.php` (of map `nb-mcp-bridge/`) in
`<project>/public/wp-content/mu-plugins/` — dezelfde `public/`-conventie als
`wpplugins.PluginsDir`. Gevonden → blok "WordPress MCP" wordt getoond. Bestaat de
koppeling al, dan wordt het blok altijd getoond.

### Test verbinding

1. `GET <url>/wp-json/` met Basic Auth (user + wachtwoord) → bereikbaar en auth ok?
2. `GET <url>/wp-json/nb-mcp/v1/status` → bridge aanwezig?

Resultaat: `ok`, `auth_failed`, `no_bridge` (auth ok, bridge ontbreekt → `bridge:
false`), `unreachable`, met een korte melding. Timeout 15 s. Het wachtwoord komt
nooit in foutmeldingen.

## UI

- **Projectinstellingen → blok "WordPress MCP"** (zichtbaar volgens bridge-detectie):
  velden id (voorgesteld uit mapnaam), URL (vooraf ingevuld uit
  `deploy_conf.json` → productie-link), user (standaard `mcp-bot`), Application
  Password (alleen invoer; toont "opgeslagen" als het in de Keychain staat), tags,
  read-only. Knoppen: *Opslaan*, *Test verbinding*, *Ontkoppelen*.
- **Zijbalk → sectie "WordPress-sites"** onder de projecten: losse sites, knop
  *+ Site*. Klik opent hetzelfde formulier zonder projectveld, plus *Verwijderen*.
- **Instellingen → blok "wp-fleet":** image-referentie, globaal read-only,
  status (geregistreerd bij Claude Code ja/nee, aantal sites, aantal zonder
  wachtwoord), knoppen *Registreer bij Claude Code* / *Verwijder registratie*.

## `mcp-launch`

Claude Code start `<app-binary> mcp-launch`. stdout is het MCP-kanaal: alle logs
naar stderr.

1. Laad globale config en `wp-sites.yml`. Ongeldig → exit 1 met bestand + regel.
2. Per site wachtwoord uit Keychain. Ontbreekt → site overslaan, waarschuwing op
   stderr.
3. `sites.json` naar `os.MkdirTemp` (0700), bestand 0600. Per site:
   `passwordEnv: WP_<ID>_APP_PASSWORD` (id in hoofdletters, `-` → `_`).
4. Start:
   ```
   docker run -i --rm --init
     -e WP_<ID>_APP_PASSWORD …          (alleen namen)
     [-e MCP_READ_ONLY=true]
     -v <tmp>/sites.json:/app/config/sites.json:ro
     -v wp-fleet-logs:/app/logs
     <image>
   ```
   Waarden gaan via `cmd.Env`, nooit via argv. Docker-binary via bestaande
   `docker.DockerBin()`. `--init` omdat Node als PID 1 SIGTERM negeert.
5. stdin/stdout/stderr doorgeven, SIGINT/SIGTERM doorsturen, exitcode van docker
   overnemen, tijdelijke map altijd opruimen.

### Registratie

*Registreer* voert uit: `claude mcp remove wp-fleet -s user` (fout negeren als
niet bestaand), daarna `claude mcp add wp-fleet -s user -- <os.Executable()>
mcp-launch`. Dit vervangt de huidige registratie met `--env-file`. Draait de app
niet vanuit `/Applications` (dev-build), dan eerst een waarschuwing dat het pad niet
stabiel is. `Status` leest `claude mcp get wp-fleet` om te bepalen of de
registratie naar deze binary wijst.

## Foutafhandeling

| Situatie | Gedrag |
|---|---|
| Docker draait niet | `mcp-launch` exit ≠ 0, stderr: "Docker draait niet — start Docker Desktop" |
| Image ontbreekt | exit ≠ 0 met hint `docker build -t wp-fleet-mcp .` in de wp-fleet-repo |
| Keychain-item ontbreekt | site overslaan + reden op stderr; UI-status telt het mee |
| `wp-sites.yml` ongeldig | UI toont fout met regel, bestand blijft onaangeroerd; `mcp-launch` exit 1 |
| Dubbele `id` of dubbel project | `Create`/`Update` weigert met duidelijke melding |
| `claude` CLI niet gevonden | *Registreer* toont melding + het commando om zelf te draaien |
| Map van gekoppeld project weg | "map niet gevonden" + *Opnieuw koppelen* |

## Testen (TDD)

- **Unit**
  - `WPSite.Validate`: id-regex, https-regel, verplichte velden.
  - `WPSiteStore`: roundtrip, `~`-normalisatie, atomisch schrijven, ongeldige YAML.
  - `EnvName`, `BuildSitesJSON` (golden file), `DockerArgs` — test dat geen
    secret-waarde in de args voorkomt.
  - `keychainSetIn`: secret komt niet in de argumentenlijst.
- **Service**
  - `WPSiteService` CRUD met nep-Keychain (uniek id, één site per project,
    delete wist Keychain).
  - `TestConnection` tegen `httptest.Server` voor alle vier uitkomsten.
  - `DetectBridge` met tijdelijke mapstructuur.
  - `WPFleetService.Register` met nep-`claude`-runner (juiste argumenten).
- **Handmatig**
  - Site koppelen → *Registreer* → `claude mcp list` toont `wp-fleet` ✓ →
    in Claude `list_sites` en `site_check` op één site.
  - Docker stoppen → Claude toont serverfout met de Docker-melding.

## Open punten

- Werkt `security` bij Keychain-lezen vanuit een door Claude Code gestart
  subproces zonder prompt? Verwachting ja (item is door `security` zelf
  aangemaakt); verifiëren in de handmatige test.
