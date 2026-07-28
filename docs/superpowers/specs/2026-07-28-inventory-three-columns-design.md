# Inventory: lokaal / GitHub / laatste in drie kolommen

**Datum:** 2026-07-28 · **Status:** goedgekeurd door Jeffrey

## Doel

De overzichten WordPress, Plugins en Thema's tonen nu één versie per project,
gelezen uit de lokaal gefetchte `origin/<default branch>`. Daardoor blijft een
op GitHub gemergde wijziging onzichtbaar tot iemand handmatig "Fetch alles"
klikt (vastgesteld 2026-07-28 met web-afcnl). Deze feature laat drie versies
naast elkaar zien — **lokaal**, **GitHub** en **laatste** — en houdt de
GitHub-kolom automatisch actueel.

## Bronnen per kolom

| Kolom | Bron |
|---|---|
| Lokaal | de werkmap van het project (wat de developer nu heeft) |
| GitHub | `origin/<default branch>`, automatisch bijgewerkt wanneer de remote vooruit is |
| Laatste | wp.org (bestaand) |

## Actueel houden van de GitHub-kolom

1. Per project wordt met het bestaande GitHub-token de commit-SHA van de
   default branch opgehaald (één lichte REST-call per repo, 5 minuten
   in-memory gecached).
2. Wijkt die SHA af van de lokale `origin/<default branch>`, dan fetcht de tool
   alleen dat project. Normaal zijn dat nul tot twee repo's.
3. Daarna worden beide kolommen lokaal gelezen: werkmap en `origin/…`.

Best-effort: zonder token, zonder netwerk of bij een API-fout valt de tool
terug op de bestaande situatie (lokaal gefetchte stand), zonder foutmelding die
het overzicht blokkeert. De "Fetch alles"-knop blijft bestaan als vangnet.

## Verouderd-markering

"Verouderd" en de teller in de kop volgen de **GitHub-kolom**: dat is de stand
die naar productie gaat en waartegen de update-knop een PR opent. Wijkt de
lokale kolom af van GitHub, dan krijgt die cel een eigen, subtiele markering
("je checkout loopt achter") zonder als verouderd te tellen.

## Architectuur

- `internal/adapters/github`: `BranchSHA(ctx, owner, repo, branch)` op de
  bestaande PR-client-stijl (`GET /repos/{o}/{r}/commits/{branch}`, alleen de
  sha).
- `internal/services/inventory_sync.go` (nieuw): SHA-vergelijking per project
  met TTL-cache en gerichte fetch; pure helpers waar mogelijk zodat ze zonder
  netwerk te testen zijn.
- `InventoryProjectRef`: `Version` wordt `LocalVersion` + `GithubVersion`;
  `Outdated` blijft (nu op basis van GitHub) en `LocalBehind bool` erbij.
  `Ref` blijft het label van de GitHub-bron.
- `InventoryService.Plugins/Themes/WordPress` lezen beide bronnen.
- Frontend: drie kolommen in `WordPressPage`, `InventoryPage` (plugins/thema's).

## Tests

- `BranchSHA` tegen `httptest` (succes, 404, auth-header).
- SHA-vergelijking: vooruit / gelijk / lokale ref ontbreekt / API-fout →
  verwachte fetch-beslissing, met fakes (geen netwerk, geen echte repo's).
- Model: outdated volgt GitHub-kolom, LocalBehind bij afwijkende werkmap.

## Buiten scope

- Bestanden rechtstreeks via de GitHub API lezen (te veel calls voor
  plugins/thema's).
- Automatisch pullen van de werkmap van de gebruiker.
