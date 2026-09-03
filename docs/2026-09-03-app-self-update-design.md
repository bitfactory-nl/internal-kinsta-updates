# Zelf-update van de tool — ontwerp

Datum: 2026-09-03

## Doel

De tool controleert zelf of er een nieuwere versie van zichzelf op GitHub staat,
vraagt de gebruiker of die geïnstalleerd mag worden, zet de nieuwe versie over de
bestaande installatie heen inclusief de bijbehorende nabewerkingscommando's, en
laat na de herstart zien wat er veranderd is.

Controlemomenten:

- bij het opstarten van de app (kort na het starten, niet blokkerend);
- daarna elke 6 uur zolang de app draait (4× per dag);
- handmatig via een knop in Instellingen.

## Uitgangssituatie

- Repo: `bitfactory-nl/internal-kinsta-updates` (privé).
- Releases worden gebouwd door `.github/workflows/release.yml` op tags `v*.*.*`
  en hebben één asset: `RDM-Sites-Tool-<tag>-macOS.zip` met daarin de
  ad-hoc gesigneerde `rdm-sites-tool.app`. Geen notarisatie.
- De release-body bevat nu alléén installatie-instructies, geen changelog.
- De app kent zijn eigen versie niet: `build/config.yml` zegt `0.0.1`,
  `build/darwin/Info.plist` zegt `0.1.0`, de laatste release is `v0.2.9`.
- De handmatige nabewerking na installatie bestaat uit twee stappen:
  quarantine-vlag verwijderen en de Playwright-chromium installeren.

## Beslissingen

| Punt | Keuze |
| --- | --- |
| Installatiemechanisme | Helper-script dat de app na afsluiten vervangt en opnieuw start |
| Changelog | Automatisch uit commits, gegenereerd in de release-body |
| Token | Hergebruik `plugin_repo.github_token`, met optionele override |
| Popup-herhaling | Eén popup per versie; daarna alleen de sidebar-badge |
| Repo-locatie | Constante in de source, geen instelling |
| Bundle-ID | Nu omgezet naar `nl.nobears.kinsta-updater` |
| Keychain-service | Zelfde naam, met eenmalige migratie van de bestaande keys |
| Playwright-install | Alleen wanneer de verwachte chromium ontbreekt |

## Onderdelen

### Versiebesef

Nieuw pakket `internal/version`:

- `version.go` — `var Version = "dev"`, gevuld via ldflags
  `-X github.com/rdm/sites-tool/internal/version.Version=<tag>`.
- `compare.go` — `IsNewer(candidate, current string) bool`. Parseert `vX.Y.Z`,
  vergelijkt numeriek per component, behandelt een pre-release-suffix als lager
  dan de bijbehorende release, en geeft `false` bij onparseerbare invoer (een
  onbekende versie is nooit "nieuwer").

`build/darwin/Taskfile.yml` geeft `APP_VERSION` mee aan de ldflags van
`build:native` en `build:docker`; `release.yml` zet die op `github.ref_name`.
Lokale builds houden `"dev"`.

Bij `Version == "dev"`, of wanneer het uitvoerbare bestand niet in een
`.app`-bundle blijkt te staan, is zelf-update uitgeschakeld: Instellingen laat
dat als tekst zien en de achtergrondloop start niet.

### GitHub-toegang

`internal/adapters/github/releases.go`:

- `LatestRelease(ctx) (domain.Release, error)` — `GET /repos/{repo}/releases/latest`,
  levert tag, body, en de asset (id, naam, grootte) die matcht op
  `RDM-Sites-Tool-*-macOS.zip`.
- `DownloadAsset(ctx, assetID int64, w io.Writer, onProgress func(done, total int64)) error`
  — `Accept: application/octet-stream`, volgt de redirect naar de
  opslaglocatie, rapporteert voortgang via de callback.

De repo van de tool zelf staat als constante in `internal/services/update_service.go`
(`selfRepo = "bitfactory-nl/internal-kinsta-updates"`). De bestaande
`github.Client` is aan de plugin-repo gebonden; de release-functies krijgen een
eigen constructor die alleen token en repo nodig heeft.

### Domeinmodel

`internal/domain/update.go`:

```go
type UpdateStatus struct {
    CurrentVersion   string        // "v0.2.9" of "dev"
    Enabled          bool          // false in dev-builds
    AutoCheck        bool
    LastCheck        time.Time
    LastError        string        // leeg als de laatste check lukte
    Available        *AvailableUpdate
}

type AvailableUpdate struct {
    Version   string
    Changes   []ChangeEntry
    Skipped   bool          // gebruiker klikte "Later" voor deze versie
    SizeBytes int64
}

type ChangeEntry struct {
    Kind string // "nieuw" | "opgelost" | "overig"
    Text string
}
```

### Configuratie en state

Gebruikersinstellingen in `~/.config/rdm/config.yml` (`internal/config/schema.go`),
nieuwe sectie:

```yaml
updates:
  auto_check: true          # standaard aan
  github_token: ""          # leeg = plugin_repo.github_token gebruiken
```

Runtime-state in `~/.config/rdm/update-state.json`, met dezelfde atomaire
tmp-plus-rename-schrijfwijze als `org-sync.json`:

```json
{
  "last_check": "2026-09-03T09:00:00Z",
  "skipped_version": "v0.2.10",
  "last_run_version": "v0.2.9",
  "installed_changes": [{ "kind": "nieuw", "text": "..." }],
  "installed_version": "v0.2.10",
  "install_log": "/Users/…/.config/rdm/logs/update-20260903-091500.log"
}
```

`installed_changes` wordt weggeschreven op het moment van installeren, zodat het
"wat is er nieuw"-venster na de herstart geen netwerk en geen token nodig heeft.

### Service

`internal/services/update_service.go` — de aan Wails gebonden service:

- `Status() domain.UpdateStatus`
- `Check() (domain.UpdateStatus, error)` — handmatig, negeert het check-interval
- `Install() error` — start de installatie; keert alleen terug bij een fout,
  want bij succes sluit de app zichzelf
- `Skip(version string) error` — legt de weggeklikte versie vast
- `WhatsNew() *domain.AvailableUpdate` — leeg tenzij deze start de eerste is na
  een geslaagde update. Bij het opstarten vergelijkt de service `version.Version`
  met `last_run_version` uit de state; verschillen die en komt `version.Version`
  overeen met `installed_version`, dan levert `WhatsNew()` de bewaarde
  `installed_changes`. Daarna wordt `last_run_version` op `version.Version` gezet,
  zodat het venster één keer verschijnt.
- `InstallLog() (string, error)` — inhoud van het laatste update-logbestand

`internal/services/update_check.go` — achtergrondloop in de stijl van
`VulnScanService.Start()`: eerste check 20 seconden na het opstarten, daarna elke
6 uur; `Stop()` sluit netjes af. Geen popup als: er geen nieuwere versie is, de
versie al is overgeslagen, of de check faalde.

`internal/services/update_notes.go` — parseert de `## Wijzigingen`-sectie uit de
release-body naar `[]ChangeEntry`. Ontbreekt die sectie (alle bestaande
releases), dan een lege lijst; de UI toont dan "geen details beschikbaar".

`internal/services/update_install.go` — de installatie, in deze volgorde, zó dat
er niets aan de bestaande installatie verandert voordat alles klaarstaat:

1. Doelmap van de huidige bundle beschrijfbaar? Zo niet: stoppen met de melding
   dat de update handmatig moet.
2. Asset downloaden naar een tempmap, met voortgang naar de UI.
3. Uitpakken met `ditto -x -k`.
4. Valideren: precies één `.app` in het archief, met een uitvoerbare
   `Contents/MacOS/rdm-sites-tool` en een `Contents/Info.plist`.
5. State bijwerken met `installed_version` en `installed_changes`.
6. Helper-script renderen uit een `go:embed`-template, uitvoerbaar maken en
   losgekoppeld starten met het eigen PID en de paden als argumenten.
7. De app afsluiten.

Events naar de frontend via de bestaande `emitter`: `updates:available`,
`updates:progress`, `updates:error`.

### Helper-script

`internal/services/update_script.sh.tmpl`, gerenderd naar de tempmap. Alle
output naar `~/.config/rdm/logs/update-<tijdstip>.log`:

1. Wacht tot het meegegeven PID verdwenen is (maximaal 30 seconden, daarna
   `kill`).
2. Huidige bundle naar `<naam>.app.bak`; nieuwe bundle met `ditto` op zijn
   plaats zetten. Mislukt dat, dan `.bak` terugzetten en met een foutcode
   stoppen.
3. `xattr -dr com.apple.quarantine` op de nieuwe bundle.
4. Playwright-chromium, alleen wanneer nodig: als `node` bestaat, vraag
   Playwright zelf naar het verwachte pad
   (`node -e "process.stdout.write(require('playwright').chromium.executablePath())"`
   vanuit `Contents/Resources/sidecar`) en installeer alleen als dat pad niet
   bestaat, met
   `node node_modules/playwright/cli.js install chromium`. Dit dekt zowel een
   nieuwere Playwright met een andere chromium-revisie als een machine waar de
   browser nog nooit is opgehaald. Ontbreekt `node`, dan een waarschuwing in het
   log en doorgaan — de app werkt dan, alleen de PDF-export, de tests en de
   mediascan niet.
5. `open` de nieuwe app.

De nieuwe versie verwijdert bij het opstarten een achtergebleven `.app.bak`. Dat
opruimen is tegelijk het bewijs dat de nieuwe build daadwerkelijk start; blijft
de `.bak` staan, dan is er handmatig een werkende versie terug te zetten.

### Frontend

- `frontend/src/components/UpdateDialog.tsx` — modal met "v0.2.9 → v0.2.10", de
  wijzigingen gegroepeerd onder Nieuw / Opgelost / Overig, en de knoppen **Nu
  installeren** en **Later**. Tijdens de installatie verandert dezelfde dialog in
  een voortgangsweergave: download-percentage, daarna "app wordt vervangen…".
  Dezelfde component dient in `whatsnew`-modus als het venster **"Bijgewerkt naar
  v0.2.10"** na de herstart, met één knop om te sluiten en een link naar het
  update-log wanneer het script iets meldde.
- `frontend/src/App.tsx` — onder in de rail een regel `↑ Update beschikbaar
  (v0.2.10)` die de dialog heropent, plus een stip op het instellingen-icoon.
  Verdwijnt pas wanneer de update geïnstalleerd is. De popup verschijnt één keer
  per versie: `Skip` zet `skipped_version`, en pas een nieuwere versie doorbreekt
  dat weer.
- `frontend/src/components/SettingsPage.tsx` — sectie *App-updates* met de
  huidige versie, het tijdstip van de laatste check, een knop **Nu controleren**,
  een toggle *Automatisch controleren (4× per dag)*, het optionele token-veld en
  een link **Toon update-log**. Ook de foutmelding van een mislukte check staat
  hier; die verschijnt nergens als popup.

### Release-workflow

`.github/workflows/release.yml`:

- `fetch-depth: 0` bij de checkout, nodig voor de commit-historie.
- Nieuwe stap die uit `git log <vorige tag>..<tag> --pretty=%s` een
  `## Wijzigingen`-sectie bouwt: `feat:` onder **Nieuw**, `fix:` onder
  **Opgelost**, de rest onder **Overig**; merge-commits worden overgeslagen. De
  sectie komt bóven de bestaande installatietekst in de release-body.
- `APP_VERSION=${{ github.ref_name }}` meegeven aan `task package`.
- De versie stampen in `Info.plist` zodat Finder en het "Over"-venster kloppen.

### Identiteit van de app: weg van het privédomein

`micromanage.nl` is het privédomein van de auteur en hoort niet op de laptops van
collega's te staan. De app-identiteit wordt daarom `nl.nobears.kinsta-updater`,
passend bij de partij die de tool uitgeeft en bij de productnaam die de app al
voert ("Kinsta Updater"). Te wijzigen:

- `build/config.yml` — `productIdentifier`, `companyName: "No Bears"`,
  `copyright: "(c) 2026, No Bears"`.
- `build/darwin/Info.plist` — wordt hieruit gegenereerd
  (`wails3 task common:update:build-assets`); staat nu nog op de placeholder
  `com.example.rdmsitestool` en `© 2026, My Company`.
- `internal/config/keychain.go` — de service-naam.
- `SPEC.md:862` — de documentatie die de oude service-naam noemt.

**Keychain-migratie.** De keys (`rdm.kinsta.apiKey`, `rdm.github.token`,
`rdm.anthropic.apiKey`, `rdm.wordfence.apiKey`) staan onder de oude
service-naam. Zonder migratie vindt de app ze niet meer en moet iedereen alles
opnieuw invullen. Daarom in `internal/config/keychain.go` een
`MigrateKeychainService()`, één keer aangeroepen bij het opstarten vanuit
`app.LoadConfig()`: per bekende account, als de key onder de nieuwe service
ontbreekt maar onder de oude bestaat, wordt hij overgezet. Idempotent, en stil
als er niets te migreren is.

De oude items blijven staan in plaats van verwijderd te worden: een
`security delete-generic-password` kan een keychain-prompt opleveren, en dat is
een slechte verrassing tijdens het opstarten. De release-notes vermelden dat ze
in Keychain Access in één keer op de oude service-naam te selecteren zijn.

**Waarom nu.** Niets in de code hangt van de bundle-ID af — de keychain gebruikt
een eigen service-naam en notificaties lopen via `osascript` — maar de
macOS-permissies die aan de app hangen (zoals toegang tot de projectenmap)
resetten wél, bij iedereen, één keer. Dit is de laatste release die iedereen
handmatig installeert en waarvan de notes gelezen worden; doe je het later, dan
resetten die permissies midden in een automatische update zonder dat iemand
begrijpt waarom. Het staat in de release-notes en in het "wat is er nieuw"-venster.
Tegelijk gaan `build/config.yml` en `Info.plist` naar het echte versienummer.

## Foutafhandeling

| Situatie | Gedrag |
| --- | --- |
| Geen netwerk, DNS-fout | Stil; `last_error` in Instellingen, geen popup, geen badge |
| 401 / 403 (token weg of te weinig rechten) | Melding in Instellingen met de hint dat het token ontbreekt of geen toegang heeft tot de repo |
| Rate limit | Melding in Instellingen; volgende check gewoon over 6 uur |
| Geen release, of geen asset die matcht | Melding in Instellingen; geen popup |
| Download afgebroken of onvolledig | Afbreken vóór stap 5; bestaande installatie ongemoeid |
| Zip corrupt of zonder `.app` | Idem: afbreken vóór stap 5 |
| Doelmap niet beschrijfbaar | Melding met het pad en het advies handmatig te installeren |
| `ditto` faalt tijdens de swap | Script zet `.bak` terug en stopt met foutcode; het log blijft staan |
| `node` ontbreekt | Waarschuwing in het log; update slaagt, sidecar-functies werken niet |

## Tests

- `internal/version/compare_test.go` — nieuwer, gelijk, ouder, pre-release,
  ontbrekende `v`-prefix, onparseerbare invoer.
- `internal/adapters/github/releases_test.go` — tegen een `httptest`-server: body
  en asset parsen, de juiste asset kiezen tussen meerdere, 401 en 404, download
  met redirect, voortgangscallback.
- `internal/services/update_notes_test.go` — `## Wijzigingen` parseren,
  groepering per type, ontbrekende sectie, een body met alleen de oude
  installatietekst.
- `internal/services/update_check_test.go` — overgeslagen versie geeft geen
  popup, gelijke of oudere versie geeft geen popup, state wordt bewaard en
  teruggelezen, dev-build start de loop niet.
- `internal/services/update_install_test.go` — staging met een nepzip: validatie
  van een archief zonder `.app`, met twee `.app`s, en met een niet-uitvoerbaar
  binair bestand; scriptrendering vergeleken met de verwachte paden en
  gecontroleerd met `bash -n`. Het script wordt in de tests niet uitgevoerd.
- `internal/config/keychain_test.go` — de migratie met een injecteerbare
  get/set in plaats van de echte `security`-CLI: key alleen onder de oude naam
  (wordt overgezet), key onder beide namen (nieuwe blijft ongemoeid), key onder
  geen van beide (geen fout), en tweemaal achter elkaar draaien (idempotent).
- Handmatig, door Jeffrey: een testtag (bijvoorbeeld `v0.2.10`) pushen en de
  volledige flow op een echte installatie draaien. Dit is de enige manier om de
  swap, de keychain-migratie, de permissie-herprompt na de bundle-ID-wijziging en
  de herstart te verifiëren.

## Buiten scope

- Windows en Linux: de installatie is macOS-specifiek.
- Notarisatie en een Developer ID-signatuur.
- Terugrollen naar een oudere versie vanuit de UI (de `.app.bak` maakt dat
  handmatig mogelijk).
- Delta-updates; de hele zip (~12 MB) wordt binnengehaald.
