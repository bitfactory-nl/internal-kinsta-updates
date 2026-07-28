# WordPress core-update met branch + PR

**Datum:** 2026-07-28 · **Status:** goedgekeurd door Jeffrey

## Doel

In de WordPress-tab twee acties toevoegen: per project een knop om WordPress core
naar de laatste versie te updaten, en één knop om dat voor alle verouderde
projecten te doen. Elke update levert een aparte branch vanaf de default
release-branch plus een pull request op — nooit een directe commit op de
release-branch en nooit iets op live.

## Flow per project

1. **Checks vooraf** (skip met reden, geen fout):
   - project staat als verouderd in het WP-overzicht;
   - default branch matcht `release/*` (zelfde regel als de wordfence-flow);
   - bestaat de branch + open PR voor deze doelversie al → status `exists` met
     PR-link (idempotent, dubbel klikken is veilig).
2. **Tijdelijke worktree**: `git fetch`, dan worktree met verse branch
   `update/wordpress-<versie>` vanaf `origin/<release-branch>`. De checkout van
   de gebruiker blijft onaangeroerd; geen stash nodig.
3. **Core vervangen** met de **no-content zip**
   (`https://wordpress.org/wordpress-<versie>-no-content.zip`, entries onder
   `wordpress/`, bevat géén `wp-content`):
   - WP-root in dit project is `public/` (conventie, zie `wpplugins.PluginsDir`).
   - Verwijder `public/wp-admin/` en `public/wp-includes/` volledig.
   - Verwijder rootbestanden die óf in de nieuwe zip zitten, óf matchen op het
     core-patroon (`wp-*.php`, `index.php`, `xmlrpc.php`, `readme.html`,
     `license.txt`) — zo verdwijnen ook door WP verwijderde core-bestanden.
   - **Altijd behouden:** `public/wp-config.php`. Niet-core bestanden (bijv.
     `public/custom-cronfile.php`) matchen geen patroon en blijven staan.
   - Pak de zip uit naar `public/` (zip-prefix `wordpress/` strippen).
4. **Commit + push**: `fix(wordpress): update WordPress core <van>→<naar>`,
   push naar origin.
5. **PR aanmaken** via de GitHub API met het bestaande `GithubToken` uit
   Instellingen; base = default release-branch; owner/repo uit de origin-URL
   (ssh- en https-vorm). Body: van→naar + noot dat WP de eventuele DB-upgrade
   zelf doet na deploy.
6. **Opruimen**: worktree verwijderen, ook bij fouten.

## UI (WordPress-tab)

- Per verouderde rij een knop **"Update → \<versie\>"**; spinner tijdens het
  draaien, daarna PR-link of de skipreden.
- Bovenaan **"Alles updaten (\<n\>)"** met bevestigingsdialoog; projecten
  worden sequentieel afgewerkt met live status per rij. Eén mislukking stopt de
  rest niet; afsluitende samenvatting.

## Veiligheid

Eindpunt is altijd een PR die een mens reviewt, merget en deployt. Geen
auto-merge, geen push naar de release-branch, geen actie op live-omgevingen.

## Architectuur

- `internal/adapters/wporg`: core-download-URL (no-content) toevoegen;
  `Download` bestaat al.
- `internal/adapters/github`: `CreatePull` + `FindOpenPull` (REST, bestaand
  token) en `ParseRepoFromRemote` voor owner/repo uit de origin-URL.
- `internal/adapters/gitcli`: `WorktreeAdd` / `WorktreeRemove`.
- Nieuwe `internal/services/wpcore_update_service.go` met één bound method
  `UpdateProject(projectID string) ProjectCoreUpdateResult`; de frontend heeft
  de projectlijst al en itereert zelf voor de bulk-actie (zelfde patroon als de
  wordfence-apply).
- Core-vervanglogica in een eigen bestand met pure functies zodat ze zonder git
  te testen zijn.

## Tests

- origin-URL-parsing (ssh, https, met/zonder `.git`);
- vervang-/uitsluitlogica op een fixture-boom (custom bestand blijft,
  `wp-config.php` blijft, verwijderd core-bestand verdwijnt);
- github-adapter tegen `httptest` (PR aanmaken, bestaande open PR vinden);
- service-flow met fakes: skip-redenen, idempotentie, opruimen bij fout.

## Buiten scope

- Plugin/thema-updates via deze knop (bestaat al via de wordfence-flow).
- Auto-merge of deploy.
- Composer-beheerde WP-installaties (deze projecten committen core in git).
