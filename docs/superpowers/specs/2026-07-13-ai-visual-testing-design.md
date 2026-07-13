# Design: AI visuele + functionele test-engine (Feature A)

**Datum:** 2026-07-13
**Project:** RDM Sites Tool (Wails v3, Go + React/Tailwind, macOS)
**Status:** ontwerp ter review

## Context

De agency draait per klant WordPress-sites met automatische update-branches
(`automated/wp-updates-*`). Voordat een update naar productie gaat wil het team
kunnen controleren of de update de site niet stukmaakt — visueel én functioneel —
over meerdere pagina's/journeys heen. Dit gebeurt nu handmatig.

Deze feature laat een gebruiker van de tool **happy flows** (scenario's) op twee
omgevingen afspelen en de uitkomsten laten vergelijken door Claude (Anthropic API):
visuele afwijkingen, console-errors en HTTP-statuscodes.

Dit is **Feature A** van twee. Feature B (klant-PDF in Bitfactory-huisstijl) wordt
apart ontworpen en hergebruikt de opgeslagen testresultaten van deze feature.

## Doel & scope

**In scope:**
- Vrij koppelbare omgevingen: `local`, `acc`, `prod` — kies per run een paar
  (default `local ↔ prod`). De "release"-kant is de baseline, de andere de "update".
- Happy flows die één keer per project worden ingesteld, meegecommit worden en door
  het team uitgebreid kunnen worden.
- Flows in natuurlijke taal beschreven; Claude genereert een bewerkbare stappenlijst.
- Deterministisch afspelen met Claude-self-heal bij een falende stap.
- AI visuele vergelijking met categorisatie + ernst (inhoud/data-verschillen worden
  laag/negeerbaar gelabeld i.p.v. als fout).
- Console-errors en statuscodes als **regressies t.o.v. de release-kant**.
- Runs worden lokaal bewaard met historie (incl. screenshots).

**Out of scope (nu):**
- Feature B (PDF-generatie) — apart ontwerp.
- Geplande/automatische runs (cron). Runs zijn voorlopig handmatig.
- Deploy-triggers (de tool deployt zelf geen branches).

## Beslissingen (uit brainstorm)

| Onderwerp | Keuze |
|---|---|
| Wat vergelijken | Vrij koppelbare omgevingen local/acc/prod; default local↔prod |
| Lokale URL | Nieuw `local`-veld in projectconfig |
| Pagina-set | Door gebruiker gedefinieerde **happy flows**, uitbreidbaar |
| Flow-diepte | Pagina-bezoeken + interacties (klik/typ/login) + assertions |
| Authoring | Natuurlijke taal → Claude genereert bewerkbare stappen |
| Uitvoering | Deterministisch afspelen + Claude self-heal bij falen |
| Visueel oordeel | Categoriseren (layout-break / ontbrekend / styling / alleen-inhoud) + ernst |
| Console/status | Regressies t.o.v. release + harde fouten (5xx, gebroken assets) altijd |
| Toegang | HTTP basic-auth per omgeving; login als flow-stap; IP/VPN door machine zelf |
| Model | Haiku/Sonnet/Opus, autonoom per taak gekozen en afgeschaald; handmatige override |
| Resultaten | Opslaan met historie per project |
| Browser-motor | Playwright-sidecar (Node), aangestuurd via `os/exec` |
| Huisstijl | TestsTab volgt bestaande Tailwind-tokens (IBM Plex, light+dark) |

## Architectuur

Go/Wails blijft de kern. Drie nieuwe bouwstenen, één uitbreiding.

```
Frontend (React)
  TestsTab (nieuw)           flows beheren · run starten · resultaten + historie
        ↕ Wails bindings
Go services
  test_service (nieuw)       orchestratie: flows laden, run uitvoeren, historie
  config (uitbreiding)       local-URL + basic-auth/testaccount (keychain-refs)
        ↕
Go adapters
  adapters/browser (nieuw)   spawnt Playwright-sidecar via os/exec, JSON I/O
  adapters/claude  (nieuw)   Anthropic API-client + model-routing (H/S/O)
        ↓ os/exec                     ↓ https
Externe processen
  Playwright-sidecar (Node)  speelt flows af, levert screenshot/console/netwerk
  Anthropic API              visuele vergelijking + categorisatie + self-heal
```

### Componenten & verantwoordelijkheden

- **`test_service`** — enige plek die een run orkestreert. Laadt flows, resolvet
  omgevingen/secrets, roept `adapters/browser` en `adapters/claude` aan, bouwt de
  `TestRun`, schrijft historie. Bevat geen browser- of HTTP-details.
- **`adapters/browser`** — dun Go-laagje dat de sidecar start, een run-opdracht als
  JSON doorgeeft en per-stap-resultaten (screenshots als bestandspaden, console,
  netwerk) terugleest. Kent Playwright niet inhoudelijk buiten het protocol.
- **Playwright-sidecar** — Node-script (gebundeld). Ontvangt een run-opdracht,
  opent per omgeving een context (met `httpCredentials` voor basic-auth), speelt
  stappen af, legt per stap screenshot + `page.on('console')` + `page.on('response')`
  vast, en meldt falende stappen terug zodat Go de self-heal kan aansturen.
- **`adapters/claude`** — Anthropic API-client. Twee taken: (1) authoring
  (NL → stappen), (2) visuele vergelijking + self-heal. Verantwoordelijk voor
  model-routing en het teruggeven van gestructureerde output.
- **`config`** — uitbreiding met `local`-URL en toegang (keychain-refs). Hergebruikt
  het bestaande `keychain:`-patroon en `ResolveSecret`.

### Distributie-impact
De Node-runtime, Playwright en een Chromium worden meegebundeld in de macOS-app.
Dit vergroot de app-omvang; te ondervangen via de Wails-buildstap (opgenomen in het
implementatieplan). De sidecar draait headless.

## Datamodel

### Flow — gecommit in `.rdm/flows.yml` (geen secrets)
```yaml
- name: Contactformulier
  steps:
    - navigate: /
    - click: "Cookies accepteren"        # NL-omschrijving; resolved selector gecached
    - navigate: /contact
    - type: { veld: "E-mail", waarde: "test@example.com" }
    - click: "Verstuur"
    - assert: "Bedankt-bericht zichtbaar"
```
Stap-types: `navigate`, `click`, `type`, `login`, `wait`, `assert`.
Elke `click`/`type`-stap bewaart een NL-omschrijving én een laatst-werkende selector
(zodat runs deterministisch en snel zijn; self-heal werkt de selector bij).
Een `login`-stap gebruikt het geconfigureerde `test_account`; `target` is daar optioneel
(mag het login-pad bevatten). `wait` en `login` vereisen dus geen `target`.

### Omgeving + toegang — in `.rdm.yml` (bestaande gecommitte projectconfig), secrets in Keychain
```yaml
# .rdm.yml (acc/prod-URLs blijven uit deploy_conf.json komen)
environments:
  local: https://cefetra.test
  acc:   # uit deploy_conf.json
  prod:  # uit deploy_conf.json
basic_auth:
  acc: { user: bitfactory, pass: "keychain:rdm.basicauth.cefetra.acc" }
test_account:
  user: tester, pass: "keychain:rdm.testaccount.cefetra"
```
`local` is het nieuwe veld. `acc`/`prod` blijven uit `deploy_conf.json` komen.

### TestRun — lokaal, gitignored (met screenshots)
Bewaard onder een app-datamap per project (bijv.
`~/.config/rdm/test-runs/<project>/<timestamp>/`). Bevat run-metadata (datum,
omgevingspaar, gebruikte modellen), en per stap: paden naar beide screenshots,
`consoleRegressies[]`, `statusRegressies[]`, en `findings[]`
(`{categorie, ernst, waar, omschrijving}`).

## Uitvoering van een run

1. **Start** in TestsTab: kies flow(s) + omgevingspaar; tool toont kostenschatting.
2. **Resolve**: URLs uit config, secrets uit Keychain.
3. **Sidecar start**: Playwright opent beide omgevingen als aparte contexts (basic-auth).
4. **Afspelen** (beide kanten parallel): deterministisch via de gecachte selector;
   per stap screenshot + console + netwerkstatus.
5. **Self-heal**: element niet gevonden → Claude krijgt de accessibility-snapshot,
   vindt het opnieuw, selector wordt bijgewerkt; de flow breekt niet.
6. **Visuele vergelijking (AI)**: Claude vergelijkt de twee screenshots per stap en
   categoriseert met ernst. Model auto-gekozen (Haiku triage → escaleer naar
   Sonnet/Opus bij lage zekerheid of dubbelzinnige verschillen).
7. **Console/status-diff**: errors/4xx/5xx die nieuw zijn op de update-kant t.o.v.
   release, plus harde fouten (5xx, gebroken assets) altijd.
8. **Opslaan & tonen**: alles → `TestRun`, lokaal bewaard; resultaten + historie in UI.

### Model-routing
Autonoom per taak, afgeschaald waar mogelijk:
- **Haiku** — triage/goedkope oordelen (bv. "zichtbaar identiek?").
- **Sonnet** — standaard visuele vergelijking + self-heal.
- **Opus** — escalatie bij twijfel/dubbelzinnigheid of hoge impact.
Handmatige override per project/run mogelijk. Kostenschatting vóór de run
(stappen × screenshots × verwacht model).

## UI (TestsTab) — huisstijl

Nieuwe tab in het projectdetail, náást Updates/Kinsta/Security. Gebruikt de
bestaande Tailwind-tokens (`bg-panel`, `panel-2`, `text-fg`/`fg-muted`/`fg-faint`,
`border-border`, `accent`/`accent-soft`, `red`/`red-soft`, `amber`/`amber-soft`,
`green`/`green-soft`), IBM Plex Sans/Mono, en werkt in light + dark — consistent met
`UpdatesTab`.

- **Bovenbalk**: omgevingspaar-kiezer (default lokaal ↔ prod), model (auto/override),
  kostenschatting, **Run**-knop (accent).
- **Links**: flow-lijst (uit `.rdm/flows.yml`) + "nieuwe flow (beschrijf in taal)".
- **Rechts**: per stap twee screenshots naast elkaar (release vs update) met markering
  op de afwijking; findings met ernst-chip (hoog → `red`, laag → `amber`) en
  categorie in mono; daaronder de nieuwe console/status-regressies.
- **Historie**: eerdere runs terug te kijken; voedt later feature B.

## Foutafhandeling

- Ontbrekende config (geen `local`-URL, geen API-key, geen basic-auth waar nodig):
  duidelijke, niet-blokkerende melding in de UI vóór de run start.
- Sidecar niet beschikbaar / crasht: run faalt netjes met de sidecar-stderr in de UI.
- Claude-API-fout (rate limit, timeout): retry met backoff; bij aanhoudend falen valt
  die stap terug op "onbeoordeeld" i.p.v. de hele run te laten falen.
- Self-heal lukt niet: stap gemarkeerd als "kon element niet vinden", flow gaat door
  waar mogelijk; duidelijk zichtbaar in het resultaat.
- Timeouts per stap en per run via `context.Context` (bestaand Go-patroon).

## Beveiliging

- Geen secrets in git: `.rdm/flows.yml` en config bevatten alleen `keychain:`-refs.
- Anthropic API-key, basic-auth-wachtwoorden en testaccount in macOS Keychain via het
  bestaande `security`-CLI-patroon (`config.ResolveSecret`).
- Run-historie (kan screenshots met data bevatten) staat lokaal en is gitignored.
- Testdata in flows moet fictief zijn (geen echte persoonsgegevens).

## Teststrategie

- **Go unit-tests** (table-driven): console/status-regressie-diff (pure functie),
  flow-parsing/validatie, model-routing-beslissing, config-resolutie. Doel ≥80%.
- **Sidecar-protocol**: contracttest op de JSON I/O tussen Go en de Node-sidecar
  met een gemockte pagina/fixture.
- **`adapters/claude`**: test-seam zodat de Anthropic-call gefaket kan worden
  (net als `downloadZip` in `plugin_service`); geen echte API-calls in CI.
- **Handmatige e2e**: één echte run op een testproject vóór oplevering.

## Aannames

- Chrome/Chromium via Playwright meegebundeld; geen externe browser vereist.
- De machine van de gebruiker heeft al toegang tot acc (IP/VPN) waar van toepassing.
- Eén Anthropic API-key op app-niveau (niet per project).

## Open punten voor het bouwplan

- Exact IPC-protocol Go ↔ sidecar (stdin/stdout JSON-lijnen vs. lokale socket).
- Precieze bundeling van Node/Playwright in de Wails-build voor macOS.
- Opslaglocatie run-historie definitief kiezen (app-datamap vs. project `.rdm/`).
