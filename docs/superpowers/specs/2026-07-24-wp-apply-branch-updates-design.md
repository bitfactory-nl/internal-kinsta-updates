# Ontwerp: WordPress-updates uitvoeren in de update-branch + handmatige acties in de tool

Datum: 2026-07-24
Status: goedgekeurd (brainstorm)
Bouwt voort op: [2026-07-24-update-branch-details-design.md](2026-07-24-update-branch-details-design.md) (manifest v1, npm-fix, uitklapbare branches).

## Doel

1. De wekelijkse check voert WordPress-updates (core, gratis .org-plugins,
   .org-thema's) **daadwerkelijk uit in de update-branch**: de nieuwe bestanden
   worden gedownload en gecommit, zodat de PR de echte code-diff toont.
   **Er wordt niets op een server uitgevoerd** — de gebruiker test lokaal en
   deployt later handmatig.
2. Updates die niet automatisch kunnen (premium/licentie, niet in git, download
   mislukt) krijgen status **manual** en zijn **prominent zichtbaar** in de
   Updates-tab van de tool, zodat de gebruiker ze later handmatig kan doen.

## Scope-beslissingen (uit brainstorm)

- **WP core**: bijwerken naar de **hoogste** aangeboden versie, inclusief
  majors (alleen npm blijft beperkt tot minor/patch).
- **Plugins/thema's**: alléén automatisch bijwerken met **.org-bewijs**
  (zie Veiligheidsmodel). Premium-vendors blijven handmatig; een aparte
  per-vendor POC (license-keys via org-secrets) is een **later, los traject**.
- **Branch-only**: de workflow raakt geen enkele server aan (de bestaande
  SSH-stap blijft read-only checken).
- Geen extra infrastructuur (geen SatisPress/mirror).
- Vertalingen (`wp-content/languages`) staan niet in git → buiten scope.

## Veiligheidsmodel (kern)

Een plugin/thema wordt alléén automatisch bijgewerkt als:

1. de slug gesaneerd is (`^[a-z0-9-]+$`), én
2. de wordpress.org-API de slug kent, én
3. de .org-versie **exact gelijk** is aan de `update_version` die de site
   rapporteert (bewijs dat de update van .org komt, niet van een
   premium-kanaal), én
4. het pakket in git getrackt is (`git ls-files` niet leeg voor die map).

Faalt één criterium → status `manual` met reden. Zo kan een PRO-plugin nooit
door een gratis .org-versie worden overschreven en wordt server-drift (plugin
op server maar niet in git) niet stilzwijgend geïntroduceerd.

Downloads gebeuren uitsluitend van `https://downloads.wordpress.org/` en
`https://api.wordpress.org/` (URL-validatie vóór download; de core
`package_url` uit de SSH-output wordt als data behandeld en gevalideerd).
Zips worden in een tempdir uitgepakt; alleen de verwachte map wordt verplaatst.

## Deel A — Workflow (`.github/workflows/check-updates.yml`)

Nieuwe stap "WP-updates toepassen" (na de SSH-check, vóór commit):

1. **WP-root detecteren**: `git ls-files '*wp-includes/version.php'` →
   bv. `public/`. Geen match → core krijgt status `manual`, reden
   "core niet in git".
2. **Core**: kies de hoogste versie uit de check-update-rijen, download de
   officiële `package_url` (nl_NL), pak uit en vervang `wp-admin/`,
   `wp-includes/` en de overige root-bestanden uit de zip (`index.php`,
   `wp-*.php`, `license.txt`, `readme.html`, …). **`wp-content/` wordt
   nooit aangeraakt**; `wp-config.php` zit niet in de core-zip en kan dus
   nooit overschreven worden (alleen `wp-config-sample.php`, dat in de
   meeste repos gitignored is).
3. **Plugins**: per update-kandidaat het veiligheidsmodel doorlopen; bij
   groen licht oude map verwijderen en nieuwe zip uitpakken. Elke faal is
   per-item (`manual` + reden), nooit een harde workflow-fout. `unzip`
   ontbreekt op de runner → alles `manual` met die reden.
4. **Thema's**: identiek via de themes-API.
5. **Committen via echte git** (de blob-API kan geen duizenden
   core-bestanden aan): werkboom muteren → `git checkout -b
   automated/updates-<date>` → `git add -A` (gitignore beschermt) →
   commit → push. PR-aanmaak en oude-branch-cleanup blijven via
   github-script. De npm-stap blijft zoals hij is (werkt al op de werkboom).
6. **PR-body** krijgt een sectie *"⚠️ Handmatig bijwerken (premium)"* met
   naam, van→naar en reden, naast de bestaande secties (WordPress
   uitgevoerd, NPM minor/patch, NPM-majors). Plus een notitie dat na deploy
   database-migraties kunnen draaien.

De beslislogica (core-keuze, .org-verificatie, manifest v2, sanitering,
PR-secties) komt als **unit-getest Node-module** `scripts/updates/wp-apply.js`
met een embedded copy in de workflow-YAML en een drift-test — zelfde patroon
als de bestaande manifest-helpers. De download/unzip/fs-glue is dunne bash.

## Deel B — Manifest v2 + backend + tool

### Manifest v2 (`.updates.json`)

```json
{
  "generatedAt": "…",
  "wordpress": {
    "core":    [{ "version": "7.0.2", "updateType": "major", "status": "applied" }],
    "plugins": [
      { "name": "svg-support",  "from": "2.5.14", "to": "2.5.17", "status": "applied" },
      { "name": "gravityforms", "from": "2.10.2", "to": "2.10.5", "status": "manual", "reason": "premium" }
    ],
    "themes": []
  },
  "npm": { "applied": [], "availableMajors": [] }
}
```

- `status`: `"applied"` | `"manual"`; afwezig = onbekend (oude manifesten).
- `reason` (alleen bij `manual`): `"premium"` | `"niet in git"` |
  `"download mislukt"` | `"unzip ontbreekt"` | `"core niet in git"`.
- npm-deel ongewijzigd (v1-compatibel).

### Go-backend (`internal/services/git_service.go`)

- `PackageUpdate` + `WPCoreUpdate` krijgen `Status`/`Reason`
  (`omitempty`). v1-manifesten en de fallback-parse blijven werken
  (status leeg = alleen gerapporteerd).
- `GetUpdateBranchDetail` verandert verder niet.

### Tool-UI (`frontend/src/components/UpdatesTab.tsx`)

1. **Amber badge op de branch-rij** zonder uitklappen: `⚠️ n handmatig`.
   Details worden daarvoor **eager** geladen bij het openen van de tab
   (het gaat om enkele branches; per branch een paar `git show`-calls).
2. Uitgeklapt: sectie **"⚠️ Handmatig bijwerken (premium)" bovenaan** in
   een amber paneel met naam, van→naar en reden. Daaronder de uitgevoerde
   updates (core/plugins/thema's met ✓), NPM uitgevoerd, NPM-majors.
3. Fallback-branches (geen manifest): geen badge; bestaande info-banner
   legt uit dat status-informatie ontbreekt.

## Componentgrenzen

- **wp-apply-module** (`scripts/updates/wp-apply.js`): pure beslislogica,
  unit-getest; weet niets van bash/fs.
- **Workflow**: orkestratie + dunne bash-glue (download/unzip/git).
- **Manifest v2**: contract tussen workflow, PR-body en tool.
- **Backend/Frontend**: lezen en tonen; geen apply-kennis.

## Teststrategie

- **Node**: fixtures voor .org-API-antwoorden (match, mismatch, 404),
  core-rij-keuze (minor+major → hoogste), sanitering, manifest v2,
  PR-sectie-rendering; drift-test voor de embedded copy.
- **Go**: v2-manifest parse (status/reason), v1-compat, fallback ongewijzigd.
- **Lokaal E2E zonder server**: de apply-stap wordt op de lokale
  vanluyken-kloon nagespeeld (echte .org-downloads, alleen lokale branch) —
  verifieert root-detectie, core-replace, plugin-verificatie en de
  premium-lijst, vóór enige merge.
- **Frontend**: visueel in de draaiende dev-app.

## Foutafhandeling

- Elke item-fout → `manual` + reden; de run gaat door.
- Geen enkele update uitvoerbaar → branch/PR zoals nu (rapportage), alle
  items `manual`.
- Git push/PR-fouten → workflow-fout (zichtbaar in Actions), geen halve
  branches: branch wordt pas gepusht na succesvolle commit.

## Bewust buiten scope

- Premium-vendor-downloads via license-keys (aparte POC; vereist
  org-secrets en per-vendor R&D; kandidaat-volgorde: WP Rocket of WPMU DEV
  eerst, domain-locked vendors zoals WPML mogelijk nooit).
- Server-side uitvoeren van updates of DB-migraties.
- mu-plugins, dropins, vertalingen.
- Wijzigen van de npm-flow (blijft minor/patch-only).
