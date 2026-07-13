# AI Visual Testing — Plan 2: Playwright-sidecar + adapters/browser Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een Node/Playwright-sidecar die een flow deterministisch afspeelt op twee omgevingen en per stap screenshot + console-errors + HTTP-statuscodes vastlegt, plus een dunne Go-adapter (`internal/adapters/browser`) die de sidecar aanstuurt en het JSON-protocol vertaalt naar Go-types.

**Architecture:** De Go-adapter is thin: hij marshalt een `RunRequest` naar JSON, start de sidecar via `os/exec` (JSON over stdin/stdout), en unmarshalt de `RunResponse`. De adapter doet GEEN vergelijking (dat is Plan 4 met `domain.DiffRegressions`) en roept GEEN Claude aan (Plan 3). De sidecar speelt stappen deterministisch af met de gecachte selector; faalt een stap, dan legt hij de fout + een accessibility-snapshot vast (input voor self-heal in Plan 3) en stopt die flow.

**Tech Stack:** Go (`os/exec`, `encoding/json`), Node.js ≥ 20, Playwright (`playwright` npm, Chromium). CI-tests gebruiken een nep-sidecar (shell-script) en vereisen geen Node/Playwright.

**Depends on:** Plan 1 (gebruikt `domain.Flow`, `domain.Step`, `domain.StepType` constanten). **Branch:** blijf op `feature/ai-visual-testing`.

**Spec:** [../specs/2026-07-13-ai-visual-testing-design.md](../specs/2026-07-13-ai-visual-testing-design.md) · **Roadmap:** [2026-07-13-ai-visual-testing-roadmap.md](2026-07-13-ai-visual-testing-roadmap.md)

---

## File Structure

- Create: `sidecar/package.json` — Node-project met Playwright als dependency.
- Create: `sidecar/runner.mjs` — de Playwright-runner (leest RunRequest van stdin, schrijft RunResponse naar stdout).
- Create: `sidecar/README.md` — hoe te installeren/smoke-testen.
- Create: `sidecar/testdata/fixture-a.html`, `sidecar/testdata/fixture-b.html` — statische fixtures voor de handmatige smoke-test.
- Create: `internal/adapters/browser/protocol.go` — `RunRequest`/`RunResponse` + subtypes (het contract).
- Create: `internal/adapters/browser/protocol_test.go` — JSON round-trip test van het contract.
- Create: `internal/adapters/browser/runner.go` — `Runner` die de sidecar spawnt.
- Create: `internal/adapters/browser/runner_test.go` — contracttest met nep-sidecar (shell-script), geen Node nodig.
- Modify: `.gitignore` — negeer `sidecar/node_modules/`.

Het `internal/adapters/browser` dir bestaat nog niet (Plan 1 noemde alleen lege `github`/`ssh` adapters). Wailsbindings/service-wiring komen in Plan 4 — NIET in dit plan.

---

## Task 1: Protocol-types (Go)

**Files:**
- Create: `internal/adapters/browser/protocol.go`
- Test: `internal/adapters/browser/protocol_test.go`

- [ ] **Step 1: Schrijf de falende test**

Create `internal/adapters/browser/protocol_test.go`:
```go
package browser

import (
	"encoding/json"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestRunRequestJSONRoundTrip(t *testing.T) {
	req := RunRequest{
		Baseline:      EnvTarget{URL: "https://prod.example.com", BasicAuth: &BasicCred{User: "bf", Pass: "s"}},
		Update:        EnvTarget{URL: "https://local.test"},
		TestAccount:   &AccountCred{User: "tester", Pass: "pw"},
		ScreenshotDir: "/tmp/run1",
		TimeoutMs:     30000,
		Flow: domain.Flow{Name: "F", Steps: []domain.Step{
			{Action: domain.StepNavigate, Target: "/"},
		}},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got RunRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Baseline.BasicAuth == nil || got.Baseline.BasicAuth.User != "bf" {
		t.Errorf("basic auth lost: %+v", got.Baseline)
	}
	if got.Flow.Steps[0].Action != domain.StepNavigate {
		t.Errorf("flow lost: %+v", got.Flow)
	}
	if got.TimeoutMs != 30000 {
		t.Errorf("timeout lost: %d", got.TimeoutMs)
	}
}

func TestRunResponseJSONRoundTrip(t *testing.T) {
	in := `{
      "steps": [
        {"index":0,"action":"navigate",
         "baseline":{"screenshot":"/t/b0.png","consoleErrors":["x"],"statusCodes":{"/":200}},
         "update":{"screenshot":"/t/u0.png","consoleErrors":[],"statusCodes":{"/":200}},
         "error":"","snapshot":""}
      ],
      "error": ""
    }`
	var resp RunResponse
	if err := json.Unmarshal([]byte(in), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Steps) != 1 {
		t.Fatalf("want 1 step, got %d", len(resp.Steps))
	}
	s := resp.Steps[0]
	if s.Baseline.Screenshot != "/t/b0.png" || s.Baseline.StatusCodes["/"] != 200 {
		t.Errorf("baseline parse: %+v", s.Baseline)
	}
	if len(s.Baseline.ConsoleErrors) != 1 || s.Baseline.ConsoleErrors[0] != "x" {
		t.Errorf("console parse: %+v", s.Baseline)
	}
}
```

- [ ] **Step 2: Run de test — moet falen**

Run: `go test ./internal/adapters/browser/ -v`
Expected: FAIL — package/types undefined.

- [ ] **Step 3: Schrijf de implementatie**

Create `internal/adapters/browser/protocol.go`:
```go
// Package browser drives the Node/Playwright sidecar that replays flows on two
// environments and captures screenshots, console errors and HTTP status codes.
package browser

import "github.com/rdm/sites-tool/internal/domain"

// BasicCred is HTTP basic-auth for one environment (already resolved, not a ref).
type BasicCred struct {
	User string `json:"user"`
	Pass string `json:"pass"`
}

// AccountCred is a site login used by `login` steps (already resolved).
type AccountCred struct {
	User string `json:"user"`
	Pass string `json:"pass"`
}

// EnvTarget is one side of a comparison.
type EnvTarget struct {
	URL       string     `json:"url"`
	BasicAuth *BasicCred `json:"basicAuth,omitempty"`
}

// RunRequest is the sidecar input (sent as JSON on stdin).
type RunRequest struct {
	Baseline      EnvTarget    `json:"baseline"`
	Update        EnvTarget    `json:"update"`
	TestAccount   *AccountCred `json:"testAccount,omitempty"`
	Flow          domain.Flow  `json:"flow"`
	ScreenshotDir string       `json:"screenshotDir"`
	TimeoutMs     int          `json:"timeoutMs"`
}

// SideObservation is what the sidecar captured for one side of one step.
type SideObservation struct {
	Screenshot    string         `json:"screenshot"`
	ConsoleErrors []string       `json:"consoleErrors"`
	StatusCodes   map[string]int `json:"statusCodes"`
}

// SidecarStepResult is the raw per-step outcome (no comparison done here).
type SidecarStepResult struct {
	Index    int             `json:"index"`
	Action   string          `json:"action"`
	Baseline SideObservation `json:"baseline"`
	Update   SideObservation `json:"update"`
	Error    string          `json:"error,omitempty"`
	Snapshot string          `json:"snapshot,omitempty"` // a11y tree on failure (self-heal input, Plan 3)
}

// RunResponse is the sidecar output (JSON on stdout).
type RunResponse struct {
	Steps []SidecarStepResult `json:"steps"`
	Error string              `json:"error,omitempty"`
}
```

- [ ] **Step 4: Run de test — moet slagen**

Run: `go test ./internal/adapters/browser/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/browser/protocol.go internal/adapters/browser/protocol_test.go
git commit -m "feat(browser): add sidecar protocol types"
```

---

## Task 2: Go-adapter Runner (met nep-sidecar contracttest)

**Files:**
- Create: `internal/adapters/browser/runner.go`
- Test: `internal/adapters/browser/runner_test.go`

- [ ] **Step 1: Schrijf de falende test**

Create `internal/adapters/browser/runner_test.go`:
```go
package browser

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

// writeFakeSidecar writes a POSIX shell script that ignores stdin and prints a
// canned RunResponse, so the exec+stdin/stdout+JSON plumbing is tested without
// Node/Playwright.
func writeFakeSidecar(t *testing.T, stdout string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake.sh")
	body := "#!/bin/sh\ncat >/dev/null\n"
	if stdout != "" {
		// single-quote-safe: no single quotes in our canned JSON
		body += "printf '%s' '" + stdout + "'\n"
	}
	if exitCode != 0 {
		body += "exit 1\n"
	}
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func TestRunnerParsesResponse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake sidecar uses /bin/sh")
	}
	canned := `{"steps":[{"index":0,"action":"navigate","baseline":{"screenshot":"/t/b.png","consoleErrors":[],"statusCodes":{"/":200}},"update":{"screenshot":"/t/u.png","consoleErrors":[],"statusCodes":{"/":200}}}]}`
	script := writeFakeSidecar(t, canned, 0)

	r := &Runner{Bin: "/bin/sh", Args: []string{script}}
	resp, err := r.Run(context.Background(), RunRequest{
		Flow: domain.Flow{Name: "F", Steps: []domain.Step{{Action: domain.StepNavigate, Target: "/"}}},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(resp.Steps) != 1 || resp.Steps[0].Baseline.StatusCodes["/"] != 200 {
		t.Fatalf("unexpected resp: %+v", resp)
	}
}

func TestRunnerNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake sidecar uses /bin/sh")
	}
	script := writeFakeSidecar(t, "", 1)
	r := &Runner{Bin: "/bin/sh", Args: []string{script}}
	if _, err := r.Run(context.Background(), RunRequest{}); err == nil {
		t.Fatal("expected error on non-zero exit")
	}
}

func TestRunnerBadJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake sidecar uses /bin/sh")
	}
	script := writeFakeSidecar(t, "not json", 0)
	r := &Runner{Bin: "/bin/sh", Args: []string{script}}
	if _, err := r.Run(context.Background(), RunRequest{}); err == nil {
		t.Fatal("expected error on bad JSON")
	}
}
```

- [ ] **Step 2: Run de test — moet falen**

Run: `go test ./internal/adapters/browser/ -run TestRunner -v`
Expected: FAIL — undefined `Runner`.

- [ ] **Step 3: Schrijf de implementatie**

Create `internal/adapters/browser/runner.go`:
```go
package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// Runner spawns the Node/Playwright sidecar. Bin/Args are overridable for tests.
type Runner struct {
	Bin  string   // default "node"
	Args []string // default [scriptPath]
}

// NewRunner returns a Runner that invokes `node <scriptPath>`.
func NewRunner(scriptPath string) *Runner {
	return &Runner{Bin: "node", Args: []string{scriptPath}}
}

// Run sends req to the sidecar as JSON on stdin and parses the RunResponse from
// stdout. A non-zero exit or unparseable output is an error (stderr included).
func (r *Runner) Run(ctx context.Context, req RunRequest) (RunResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return RunResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	cmd := exec.CommandContext(ctx, r.Bin, r.Args...)
	cmd.Stdin = bytes.NewReader(data)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return RunResponse{}, fmt.Errorf("sidecar exec: %w: %s", err, errb.String())
	}

	var resp RunResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		return RunResponse{}, fmt.Errorf("parse sidecar response: %w", err)
	}
	if resp.Error != "" {
		return resp, fmt.Errorf("sidecar reported: %s", resp.Error)
	}
	return resp, nil
}
```

- [ ] **Step 4: Run de test — moet slagen**

Run: `go test ./internal/adapters/browser/ -v`
Expected: PASS (alle tests)

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/browser/runner.go internal/adapters/browser/runner_test.go
git commit -m "feat(browser): spawn sidecar via os/exec with contract test"
```

---

## Task 3: Sidecar-scaffold (package.json + gitignore + README)

**Files:**
- Create: `sidecar/package.json`
- Create: `sidecar/README.md`
- Modify: `.gitignore`

- [ ] **Step 1: Maak `sidecar/package.json`**

Create `sidecar/package.json`:
```json
{
  "name": "rdm-sites-tool-sidecar",
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "description": "Playwright sidecar for the RDM Sites Tool AI visual testing feature",
  "engines": { "node": ">=20" },
  "scripts": {
    "install-browser": "playwright install chromium",
    "smoke": "node runner.mjs < testdata/smoke-request.json"
  },
  "dependencies": {
    "playwright": "^1.48.0"
  }
}
```

- [ ] **Step 2: Negeer node_modules**

Add to `.gitignore` (append a line):
```
sidecar/node_modules/
```

- [ ] **Step 3: Schrijf `sidecar/README.md`**

Create `sidecar/README.md`:
```markdown
# Playwright sidecar

Speelt een happy flow af op twee omgevingen en levert per stap screenshot,
console-errors en HTTP-statuscodes. Aangestuurd door het Go-pakket
`internal/adapters/browser` via JSON over stdin/stdout.

## Installatie (eenmalig, lokaal / in de app-build)
```bash
cd sidecar
npm install
npm run install-browser   # downloadt Chromium
```

## Protocol
- **stdin:** één JSON-object (`RunRequest`, zie `internal/adapters/browser/protocol.go`).
- **stdout:** één JSON-object (`RunResponse`).
- Fouten in één stap komen in `steps[].error` (+ `steps[].snapshot` met de
  accessibility-tree voor self-heal); een fatale fout komt in top-level `error`.

## Smoke-test (handmatig; vereist Chromium)
Serveer de fixtures en draai een request. Zie `testdata/`. Deze test is niet in
CI opgenomen omdat Chromium een grote download is.
```

- [ ] **Step 4: Commit**

```bash
git add sidecar/package.json sidecar/README.md .gitignore
git commit -m "chore(sidecar): scaffold Node/Playwright sidecar project"
```

---

## Task 4: Sidecar-runner (Playwright)

**Files:**
- Create: `sidecar/runner.mjs`
- Create: `sidecar/testdata/fixture-a.html`
- Create: `sidecar/testdata/fixture-b.html`

Dit is Node/Playwright-code; verificatie gebeurt via een handmatige smoke-test (Step 3-5), niet in de Go-CI.

- [ ] **Step 1: Schrijf `sidecar/runner.mjs`**

Create `sidecar/runner.mjs`:
```js
// Reads a RunRequest (JSON) from stdin, replays the flow on two environments,
// and writes a RunResponse (JSON) to stdout. Deterministic replay: uses the
// step's cached selector when present, else a natural-language fallback. On a
// step failure it records the error + an accessibility snapshot and stops.
import { chromium } from 'playwright'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

function readStdin() {
  return JSON.parse(readFileSync(0, 'utf8'))
}

async function openSide(target, timeout) {
  const browser = await chromium.launch()
  const context = await browser.newContext({
    ignoreHTTPSErrors: true,
    httpCredentials: target.basicAuth
      ? { username: target.basicAuth.user, password: target.basicAuth.pass }
      : undefined,
  })
  const page = await context.newPage()
  page.setDefaultTimeout(timeout)
  const consoleErrors = []
  page.on('console', (m) => { if (m.type() === 'error') consoleErrors.push(m.text()) })
  page.on('pageerror', (e) => consoleErrors.push(String(e)))
  const statusCodes = {}
  page.on('response', (r) => { statusCodes[r.url()] = r.status() })
  return { browser, context, page, consoleErrors, statusCodes, baseURL: target.url }
}

// Resolve a locator for click/type/assert: prefer the cached CSS selector,
// else fall back to a text/label match from the natural-language target.
function locate(page, step) {
  if (step.selector && step.selector.trim() !== '') return page.locator(step.selector)
  return page.getByText(step.target, { exact: false }).first()
}

async function applyStep(side, step, testAccount) {
  const { page, baseURL } = side
  switch (step.action) {
    case 'navigate':
      await page.goto(new URL(step.target, baseURL).toString(), { waitUntil: 'domcontentloaded' })
      break
    case 'click':
      await locate(page, step).click()
      break
    case 'type':
      await locate(page, step).fill(step.value)
      break
    case 'login': {
      // WordPress default login form; target may override the login path.
      const path = step.target && step.target.trim() !== '' ? step.target : '/wp-login.php'
      await page.goto(new URL(path, baseURL).toString(), { waitUntil: 'domcontentloaded' })
      if (testAccount) {
        await page.fill('#user_login', testAccount.user)
        await page.fill('#user_pass', testAccount.pass)
        await page.click('#wp-submit')
        await page.waitForLoadState('domcontentloaded')
      }
      break
    }
    case 'wait': {
      const ms = Number(step.target)
      if (!Number.isNaN(ms) && ms > 0) await page.waitForTimeout(ms)
      else if (step.target) await locate(page, step).waitFor()
      break
    }
    case 'assert':
      // Throws (caught by caller) if not visible within the timeout.
      await locate(page, step).waitFor({ state: 'visible' })
      break
    default:
      throw new Error(`unknown action: ${step.action}`)
  }
}

async function shoot(side, dir, name) {
  const path = join(dir, name)
  await side.page.screenshot({ path, fullPage: true })
  return path
}

async function main() {
  const req = readStdin()
  const timeout = req.timeoutMs && req.timeoutMs > 0 ? req.timeoutMs : 30000
  const resp = { steps: [], error: '' }

  let baseline, update
  try {
    baseline = await openSide(req.baseline, timeout)
    update = await openSide(req.update, timeout)

    for (let i = 0; i < req.flow.steps.length; i++) {
      const step = req.flow.steps[i]
      const result = {
        index: i,
        action: step.action,
        baseline: { screenshot: '', consoleErrors: [], statusCodes: {} },
        update: { screenshot: '', consoleErrors: [], statusCodes: {} },
        error: '',
        snapshot: '',
      }
      try {
        await applyStep(baseline, step, req.testAccount)
        await applyStep(update, step, req.testAccount)
      } catch (e) {
        result.error = String(e && e.message ? e.message : e)
        // Capture an accessibility snapshot of the update side for self-heal.
        try { result.snapshot = JSON.stringify(await update.page.accessibility.snapshot()) } catch {}
      }
      // Screenshots + observation snapshots even on failure (best effort).
      try { result.baseline.screenshot = await shoot(baseline, req.screenshotDir, `s${i}-baseline.png`) } catch {}
      try { result.update.screenshot = await shoot(update, req.screenshotDir, `s${i}-update.png`) } catch {}
      result.baseline.consoleErrors = [...baseline.consoleErrors]
      result.update.consoleErrors = [...update.consoleErrors]
      result.baseline.statusCodes = { ...baseline.statusCodes }
      result.update.statusCodes = { ...update.statusCodes }
      resp.steps.push(result)
      if (result.error) break // stop the flow on first failure
    }
  } catch (e) {
    resp.error = String(e && e.message ? e.message : e)
  } finally {
    if (baseline) await baseline.browser.close().catch(() => {})
    if (update) await update.browser.close().catch(() => {})
  }

  process.stdout.write(JSON.stringify(resp))
}

main().catch((e) => {
  process.stdout.write(JSON.stringify({ steps: [], error: String(e && e.message ? e.message : e) }))
  process.exit(0)
})
```

- [ ] **Step 2: Schrijf de fixtures**

Create `sidecar/testdata/fixture-a.html`:
```html
<!doctype html>
<html><head><title>Fixture A</title></head>
<body><h1>Welkom</h1><button>Cookies accepteren</button><p>Bedankt-bericht zichtbaar</p></body></html>
```

Create `sidecar/testdata/fixture-b.html` (identical structure, one visual change to eyeball diffs later):
```html
<!doctype html>
<html><head><title>Fixture B</title></head>
<body><h1 style="color:red">Welkom</h1><button>Cookies accepteren</button><p>Bedankt-bericht zichtbaar</p></body></html>
```

- [ ] **Step 3: Handmatige smoke-test — installeer**

Run:
```bash
cd sidecar && npm install && npm run install-browser
```
Expected: Playwright + Chromium geïnstalleerd (grote download; kan enkele minuten duren).

- [ ] **Step 4: Handmatige smoke-test — draai een run**

Serve the fixtures on two ports and run a request. Run (from `sidecar/`):
```bash
# serve fixtures
(cd testdata && python3 -m http.server 8801 >/dev/null 2>&1 &) 
(cd testdata && python3 -m http.server 8802 >/dev/null 2>&1 &)
mkdir -p /tmp/rdm-smoke
printf '%s' '{
  "baseline": {"url":"http://localhost:8801/fixture-a.html"},
  "update":   {"url":"http://localhost:8802/fixture-b.html"},
  "screenshotDir": "/tmp/rdm-smoke",
  "timeoutMs": 15000,
  "flow": {"name":"smoke","steps":[
    {"action":"navigate","target":""},
    {"action":"click","target":"Cookies accepteren"},
    {"action":"assert","target":"Bedankt-bericht zichtbaar"}
  ]}
}' | node runner.mjs
```
Expected: één JSON-object op stdout met 3 steps, elk met een `baseline.screenshot` en `update.screenshot` pad, `statusCodes` met 200, en `error:""`. In `/tmp/rdm-smoke` staan `s0-baseline.png`…`s2-update.png`.

- [ ] **Step 5: Verifieer de screenshots kort**

Run: `ls -la /tmp/rdm-smoke/`
Expected: 6 PNG-bestanden, niet leeg. (Optioneel openen om de rode `h1` op de update-kant te zien.)

- [ ] **Step 6: Commit**

```bash
git add sidecar/runner.mjs sidecar/testdata/
git commit -m "feat(sidecar): Playwright runner replays flow and captures per-step data"
```

> Als `npm install`/Chromium in deze omgeving niet lukt: commit de code toch (Step 6), noteer dat de smoke-test lokaal nog moet draaien, en rapporteer DONE_WITH_CONCERNS. De Go-CI-tests (Task 1-2) blijven groen zonder Node.

---

## Task 5: Volledige verificatie

- [ ] **Step 1: Go-tests + vet**

Run: `go test -race ./internal/adapters/browser/ && go vet ./internal/adapters/browser/`
Expected: PASS + schoon.

- [ ] **Step 2: Bevestig dat de rest nog bouwt**

Run: `go build ./internal/...`
Expected: geen fouten.

- [ ] **Step 3: Commit (indien iets gewijzigd)**

```bash
git add -A && git commit -m "chore(browser): vet/format clean" || echo "niets te committen"
```

---

## Self-Review — dekking t.o.v. spec

- **Sidecar speelt flow af op twee omgevingen** → Task 4 (`runner.mjs`, `openSide` × 2). ✓
- **Basic-auth per omgeving** → Task 1 (`EnvTarget.BasicAuth`) + Task 4 (`httpCredentials`). ✓
- **Login als flow-stap met testaccount** → Task 4 (`applyStep` case `login`). ✓
- **Per stap: screenshot + console-errors + statuscodes** → Task 4 (`shoot`, `page.on('console')`, `page.on('response')`). ✓
- **Deterministisch afspelen via gecachte selector** → Task 4 (`locate`: selector first, NL fallback). ✓
- **Self-heal-input bij falen** → Task 4 (`result.snapshot` = a11y-tree; flow stopt). Consumptie in Plan 3. ✓
- **Go-adapter is thin, JSON over os/exec** → Task 2 (`Runner.Run`). ✓
- **CI zonder Node/Playwright** → Task 2 (nep-sidecar shell-script). ✓

**Bewust NIET in dit plan:** visuele vergelijking + `DiffRegressions`-toepassing (Plan 4), Claude self-heal/authoring (Plan 3), bundeling in de Wails-build + service-wiring/bindings (Plan 4), UI (Plan 5).

**Placeholder-scan:** geen TBD/TODO; alle Go-stappen hebben volledige code, de sidecar heeft volledige code + een concrete handmatige smoke-procedure. ✓
**Type-consistentie:** `RunRequest`/`RunResponse`/`EnvTarget`/`BasicCred`/`AccountCred`/`SideObservation`/`SidecarStepResult` consistent tussen `protocol.go`, de tests en het JSON dat `runner.mjs` produceert (veldnamen: `screenshot`, `consoleErrors`, `statusCodes`, `snapshot`). ✓

---

## Post-review revisies (na uitvoering)

De code-review op `runner.mjs` (tegen de daadwerkelijk geïnstalleerde Playwright **1.61.1**,
niet de `^1.48.0` uit `package.json`) vond drie echte bugs; gefixt in commit `2849660`.
Als je Task 4 opnieuw uitvoert, gebruik meteen deze correcties in `runner.mjs`:

1. **a11y-snapshot:** `page.accessibility` is verwijderd in Playwright 1.61. Gebruik
   `await failedSide.page.locator('html').ariaSnapshot()` (levert een YAML-string; `Snapshot`
   blijft gewoon een string-hint voor self-heal in Plan 3).
2. **Per-stap observaties:** reset `consoleErrors`/`statusCodes` ná het wegschrijven van elke
   stap *in place* (`side.consoleErrors.length = 0`; `for (const k in side.statusCodes) delete side.statusCodes[k]`) —
   niet heraanwijzen, want de page-listeners houden de originele referentie vast. Anders
   herhalen latere stappen de observaties van eerdere stappen.
3. **Snapshot van de falende kant:** speel baseline en update apart af, houd `failedSide` bij,
   en neem de `ariaSnapshot` van de kant die daadwerkelijk faalde (niet altijd `update`).
4. **Geen browser-lek:** wikkel `newContext`/`newPage` in `openSide` in try/catch die
   `browser.close()` aanroept en de fout doorgooit bij init-falen na `launch()`.

Geverifieerd: succes-run (3 stappen, statuscodes alleen op de navigate-stap), bewust-falende
run (stap met niet-lege `error` én YAML-`snapshot`, loop stopt daar), en per-stap-isolatie van
console/status. De sidecar draait aantoonbaar end-to-end met echte Chromium.
