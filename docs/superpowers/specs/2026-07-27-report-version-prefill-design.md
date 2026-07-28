# Rapportage: automatische versie-prefill + opmerkingenveld

**Datum:** 2026-07-27 · **Status:** goedgekeurd door Jeffrey

## Doel

De "Server software & frameworks"-tabel in de servicecontract-rapportage wordt nu
grotendeels handmatig ingevuld. Deze feature vult de kolommen **Huidige versie**,
**Laatste versie** en **Ondersteund tot** automatisch tijdens de bestaande
Prefill-actie, splitst PHP in productie- en lokale-buildversie, en voegt een vrij
opmerkingenveld toe aan rapport en PDF.

## Software-tabel: rijen en bronnen

| Rij | Huidige versie | Laatste (LTS) | Ondersteund tot |
|---|---|---|---|
| PHP (productie) | Kinsta API `php_engine_version` | nieuwste actieve PHP-branch | security-EOL van huidige branch |
| PHP (lokaal) | `FROM …/service-php/php:<ver>` uit `.bitfactory/docker/php-fpm/Dockerfile.dev`, fallback `Dockerfile` | idem | idem (eigen branch) |
| Node | `image: …/service-node/node:<tag>` uit `docker-compose.yaml`, fallback `FROM …/node:<ver> AS frontend` in de Dockerfile | nieuwste actieve Node-LTS-lijn | EOL van huidige major |
| MariaDB | handmatig | nieuwste MariaDB LTS | alleen indien Huidig ingevuld |
| WordPress | Kinsta `wordpress_version` (live) | laatste WP-release | EOL van huidige WP-branch |

## Gedrag

- Prefill blijft een **expliciete knopactie**; elke bron is best-effort en
  non-fataal (bestaande filosofie). Een cel wordt alleen overschreven als de bron
  een waarde oplevert; handmatige invullingen blijven anders staan.
- **"Ondersteund tot"** = security-support-einddatum van de *huidige* versie,
  weergegeven als `dd-mm-jjjj`.
- **Normalisatie:** `php8.3` → `8.3`; registry-suffixen (`-jit`, `-bfN`) worden
  gestript voor branch-matching; getoonde waarde is de genormaliseerde versie
  (bijv. `24.16.0`).
- **Bron-refs:** repo-bestanden worden gelezen van `origin/<default branch>` via
  `gitcli.ShowFile` (zelfde aanpak als inventory), met werkmap-fallback als de
  origin-ref ontbreekt.
- **Migratie:** bestaande drafts met een oude "PHP"-rij worden bij openen
  idempotent omgezet: hernoemen naar "PHP (productie)" en "PHP (lokaal)" direct
  erna invoegen. Nieuwe skeletons bevatten de vijf rijen hierboven.
- Een `${TAG_NODE}`-variabele (of andere niet-resolvebare tag) wordt
  overgeslagen; het veld blijft leeg.

## EOL-databron

`https://endoflife.date/api/<product>.json` voor `php`, `nodejs`, `mariadb` en
`wordpress`. Eén gratis JSON-API, geen key. In-memory cache van 24 uur; fouten
degraderen stil (cel blijft leeg). Overwogen alternatieven — hardcoded tabel
(veroudert) en per-product officiële bronnen (meer breekpunten) — afgewezen.

Selectieregels:
- "Laatste": PHP → hoogste cycle met lopende support; Node → hoogste cycle met
  `lts != false` en lopende support; MariaDB → hoogste LTS-cycle met lopende
  support; WordPress → hoogste cycle (`latest` van die cycle als patchversie).
- "Ondersteund tot": `eol`-datum van de cycle waarin de huidige versie valt.

## Architectuur

- **Nieuw** `internal/adapters/endoflife`: kleine client + cache.
- **Nieuw** parser-helpers (eigen bestand in `services`): Dockerfile-`FROM` →
  PHP/Node-versie; docker-compose → node-image-tag.
- `report_service.go`: `prefillFromRepo` (PHP lokaal, Node) en `prefillEOL`
  (Laatste/OndersteundTot alle rijen) naast bestaande Kinsta/security-prefill.
- `domain.Report` krijgt `Opmerkingen string`.
- ReportTab: textarea-sectie "Overige opmerkingen" onderaan; opgeslagen in draft.
- PDF-template: sectie "Overige opmerkingen" als laatste blok, alleen gerenderd
  bij niet-lege tekst.
- Bindings regenereren met `wails3 generate bindings -ts -d frontend/bindings`.

## Tests

- Table-driven tests voor parsers (fixtures naar voorbeeld van echte projecten:
  `php:8.3-jit`, `node:24.16.0-bf3`, `${TAG_NODE}`, ontbrekende bestanden).
- EOL-adapter: cycle-matching, "laatste actieve" selectie, cache-gedrag (fake
  HTTP-server).
- Prefill-integratie via bestaande test-seams; migratietest oude "PHP"-rij;
  template-rendertest voor het opmerkingenveld (aan/afwezig).

## Buiten scope

- MariaDB huidige versie automatisch uitlezen (bron ontbreekt).
- Persistente EOL-cache op schijf.
- Automatische periodieke prefill (blijft knopactie).
