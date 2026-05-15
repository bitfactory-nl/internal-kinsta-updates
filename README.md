# RDM Sites Tool

Overzicht van alle projecten in je lokale projecten-map — git-status, branches, Kinsta-updates en meer, in één desktop-app.

## Downloaden & installeren (macOS)

1. Ga naar de [nieuwste release](https://github.com/bitfactory-nl/internal-kinsta-updates/releases/latest)
2. Download `RDM-Sites-Tool-vX.X.X-macOS.zip`
3. Pak het zip-bestand uit (dubbelklik)
4. Sleep `rdm-sites-tool.app` naar je **Applications** map
5. **Eerste keer openen:** open Terminal en voer dit commando uit:
   ```bash
   xattr -d com.apple.quarantine "/Applications/rdm-sites-tool.app"
   ```
   Daarna opent de app gewoon met dubbelklik.

> macOS Sequoia blokkeert apps van buiten de App Store. Het bovenstaande commando verwijdert de quarantaine-markering eenmalig.

## Configuratie

Klik op het ⚙ icoon in de app om in te stellen:

- **Projects folder(s)** — één of meerdere mappen met je lokale projecten
- **Kinsta API key** — te vinden in je [Kinsta-dashboard](https://my.kinsta.com/company/api-keys)
- **Kinsta Company ID** — te vinden in je Kinsta-dashboard
- **Editor** — welke code-editor opent bij "Open in editor" (bijv. Cursor, VS Code)

## Wat kan de app?

| Tab | Wat je ziet / kunt doen |
|-----|------------------------|
| **Info** | Hosting-platform (Kinsta / AWS / VPS), framework, PHP-versie |
| **Changes** | Uncommitted bestanden, diff bekijken, stagen en commiten |
| **History** | Commit-log met visuele branch-graaf |
| **Branches** | Lokale en remote branches, checkout, merge, delete |
| **Updates** | Open update-PR's van de wekelijkse Kinsta-check |
| **Kinsta** | Live omgevingen, deployments, cache-flush |
| **Blame** | Bestandshistorie per regel |
| **Stash / Tags** | Stashes beheren, tags bekijken |

## Updates

Nieuwe versies verschijnen automatisch als [release](https://github.com/bitfactory-nl/internal-kinsta-updates/releases). Download de nieuwe zip en vervang de app in je Applications-map.

---

## Voor ontwikkelaars

### Vereisten

- Go 1.25+
- Node 24+
- Wails v3: `go install github.com/wailsapp/wails/v3/cmd/wails3@latest`

### Lokaal draaien

```bash
cd frontend && npm ci && cd ..
wails3 dev
```

### Bouwen

```bash
wails3 build
```

De app verschijnt in `bin/`.
