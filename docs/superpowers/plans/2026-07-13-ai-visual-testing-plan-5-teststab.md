# AI Visual Testing — Plan 5: TestsTab frontend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De TestsTab-UI in de bestaande huisstijl: flows beheren + NL-authoring, een run starten met omgevingspaar/model, resultaten tonen (screenshots naast elkaar, gecategoriseerde findings, console/status-regressies) en run-historie terugkijken.

**Architecture:** Een nieuwe React-component `TestsTab.tsx` die de gegenereerde `Services.TestService`-bindings gebruikt (ListFlows/SaveFlows/AuthorFlow/Run/ListRuns/GetRun). Screenshots (lokale bestandspaden) worden getoond via een nieuwe Go-methode `TestService.Screenshot(path) → data-URL` (pad-gevalideerd binnen de historie-map). De tab wordt in `ProjectDetail` gehangen onder de WORDPRESS-groep.

**Tech Stack:** React 18 + Tailwind v4 (bestaande tokens: `bg-panel`, `panel-2`, `fg`/`fg-muted`/`fg-faint`, `border`, `accent`/`accent-soft`, `red`/`red-soft`, `amber`/`amber-soft`, `green`), IBM Plex Sans/Mono, Wails bindings.

**Depends on:** Plan 4 (TestService + bindings). **Branch:** blijf op `feature/ai-visual-testing`.

**Spec:** [../specs/2026-07-13-ai-visual-testing-design.md](../specs/2026-07-13-ai-visual-testing-design.md) · **Roadmap:** [2026-07-13-ai-visual-testing-roadmap.md](2026-07-13-ai-visual-testing-roadmap.md)

---

## File Structure

- Modify: `internal/services/runstore.go` — `Owns(path)` containment check.
- Modify: `internal/services/test_service.go` — `Screenshot(path) (string, error)`.
- Modify: `internal/services/runstore_test.go` + `test_service_test.go` — tests.
- Create: `frontend/src/components/TestsTab.tsx` — de UI.
- Modify: `frontend/src/components/ProjectDetail.tsx` — registreer de tab.
- Regenerate: `frontend/bindings/...` (na de Go-methode).

---

## Task 1: Go — Screenshot als data-URL (pad-gevalideerd)

**Files:** Modify `internal/services/runstore.go`, `internal/services/test_service.go`; tests in `runstore_test.go`, `test_service_test.go`.

- [ ] **Step 1: Schrijf de falende tests**

Append to `internal/services/runstore_test.go`:
```go
func TestRunStoreOwns(t *testing.T) {
	base := t.TempDir()
	store := NewRunStore(base)
	inside := filepath.Join(base, "p", "r", "screenshots", "s0.png")
	if !store.Owns(inside) {
		t.Error("expected Owns=true for path inside base")
	}
	if store.Owns(filepath.Join(base, "..", "etc", "passwd")) {
		t.Error("expected Owns=false for traversal path")
	}
	if store.Owns("/totally/elsewhere.png") {
		t.Error("expected Owns=false for unrelated path")
	}
}
```
(Add `"path/filepath"` to the runstore_test.go imports.)

Append to `internal/services/test_service_test.go`:
```go
func TestServiceScreenshot(t *testing.T) {
	base := t.TempDir()
	ps := NewProjectService(nil)
	svc := NewTestService(ps, &config.Global{}, NewRunStore(base), &fakeRunner{})

	png := filepath.Join(base, "p", "r", "screenshots", "s0.png")
	writePNG(t, png)

	url, err := svc.Screenshot(png)
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Errorf("not a data URL: %q", url)
	}

	if _, err := svc.Screenshot("/etc/passwd"); err == nil {
		t.Error("expected error for path outside history")
	}
}
```
(Add `"strings"` to the test_service_test.go imports.)

- [ ] **Step 2: Run de tests — moeten falen**

Run: `go test ./internal/services/ -run 'TestRunStoreOwns|TestServiceScreenshot' -v`
Expected: FAIL — undefined `Owns`, `Screenshot`.

- [ ] **Step 3: Implementatie**

Add to `internal/services/runstore.go` (add `"strings"` to imports):
```go
// Owns reports whether path is inside the store's base directory (guards the
// Screenshot endpoint against reading arbitrary files).
func (s *RunStore) Owns(path string) bool {
	base, err := filepath.Abs(s.baseDir)
	if err != nil {
		return false
	}
	p, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(base, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
```

Add to `internal/services/test_service.go` (add `"encoding/base64"` to imports):
```go
// Screenshot returns a run screenshot as a data: URL for the webview. The path
// must live inside the run-history directory.
func (s *TestService) Screenshot(path string) (string, error) {
	if !s.store.Owns(path) {
		return "", fmt.Errorf("pad buiten historie: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("lees screenshot: %w", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), nil
}
```

- [ ] **Step 4: Run de tests — moeten slagen**

Run: `go test ./internal/services/ -run 'TestRunStoreOwns|TestServiceScreenshot' -v`
Expected: PASS

- [ ] **Step 5: Regenerate bindings + commit**

Run: `task common:generate:bindings 2>&1 | tail -3`
Expected: bindings bijgewerkt (Screenshot verschijnt in `testservice.ts`).

```bash
git add internal/services/runstore.go internal/services/test_service.go internal/services/runstore_test.go internal/services/test_service_test.go frontend/bindings/
git commit -m "feat(testing): screenshot data-URL endpoint for the webview"
```

---

## Task 2: TestsTab.tsx (huisstijl)

**Files:** Create `frontend/src/components/TestsTab.tsx`.

- [ ] **Step 1: Schrijf de component**

Create `frontend/src/components/TestsTab.tsx`:
```tsx
import { useState, useEffect, useCallback } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { Flow, TestRun, StepResult, Finding, Regression } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'

interface Props {
  projectId: string
}

type EnvKey = 'local' | 'acc' | 'prod'
type ModelTier = '' | 'haiku' | 'sonnet' | 'opus'

const ENV_LABELS: Record<EnvKey, string> = { local: 'Lokaal', acc: 'Acc', prod: 'Prod' }

function Screenshot({ projectId, path, label }: { projectId: string; path: string; label: string }) {
  const [url, setUrl] = useState<string | null>(null)
  const [err, setErr] = useState(false)
  useEffect(() => {
    let alive = true
    if (!path) { setUrl(null); return }
    Services.TestService.Screenshot(path)
      .then(u => { if (alive) setUrl(u) })
      .catch(() => { if (alive) setErr(true) })
    return () => { alive = false }
  }, [projectId, path])
  return (
    <div className="flex-1 min-w-0">
      <div className="text-[10px] uppercase tracking-wide text-fg-faint mb-1">{label}</div>
      <div className="rounded-lg border border-border bg-panel-2 overflow-hidden aspect-[4/3] flex items-center justify-center">
        {err ? <span className="text-[11px] text-fg-faint">geen screenshot</span>
          : url ? <img src={url} alt={label} className="max-w-full max-h-full object-contain" />
          : <span className="animate-spin text-fg-faint text-sm">↻</span>}
      </div>
    </div>
  )
}

function SeverityChip({ f }: { f: Finding }) {
  const hi = f.severity === 'hoog'
  return (
    <span className={`text-[9.5px] font-bold px-2 py-px rounded ${hi ? 'bg-red-soft text-red' : 'bg-amber-soft text-amber'}`}>
      {(f.severity ?? '').toUpperCase()}
    </span>
  )
}

function StepCard({ projectId, step }: { projectId: string; step: StepResult }) {
  const findings = step.findings ?? []
  const regressions = step.regressions ?? []
  const clean = findings.length === 0 && regressions.length === 0 && !step.error
  return (
    <div className="bg-panel border border-border rounded-xl p-4 mb-3">
      <div className="flex items-center gap-2 mb-3">
        <span className="text-[11px] font-mono text-fg-faint">stap {step.index}</span>
        <span className="text-[12px] font-medium text-fg">{step.action}</span>
        {clean && <span className="ml-auto text-[11px] text-green">✔ geen regressies</span>}
        {step.error && <span className="ml-auto text-[11px] text-red">⚠ {step.error}</span>}
      </div>
      <div className="flex gap-3 mb-3">
        <Screenshot projectId={projectId} path={step.screenshotBase} label="Release" />
        <Screenshot projectId={projectId} path={step.screenshotUpdate} label="Update" />
      </div>
      {findings.map((f: Finding, i: number) => (
        <div key={i} className="flex items-center gap-2 text-[12.5px] py-1">
          <SeverityChip f={f} />
          <span className="font-mono text-[10.5px] text-fg-faint">{f.category}</span>
          <span className="text-fg-muted">{f.description}{f.where ? ` (${f.where})` : ''}</span>
        </div>
      ))}
      {regressions.map((r: Regression, i: number) => (
        <div key={i} className="flex items-center gap-2 text-[12px] py-0.5 font-mono">
          <span className={r.hard ? 'text-red' : 'text-orange'}>
            {r.kind === 'console' ? 'console' : 'status'} ✦ nieuw:
          </span>
          <span className="text-fg-muted">{r.detail}</span>
        </div>
      ))}
    </div>
  )
}

export default function TestsTab({ projectId }: Props) {
  const [flows, setFlows] = useState<Flow[]>([])
  const [selectedFlow, setSelectedFlow] = useState<string>('')
  const [baselineEnv, setBaselineEnv] = useState<EnvKey>('prod')
  const [updateEnv, setUpdateEnv] = useState<EnvKey>('local')
  const [model, setModel] = useState<ModelTier>('')
  const [running, setRunning] = useState(false)
  const [run, setRun] = useState<TestRun | null>(null)
  const [history, setHistory] = useState<TestRun[]>([])
  const [error, setError] = useState<string | null>(null)
  const [authoring, setAuthoring] = useState(false)
  const [authorText, setAuthorText] = useState('')

  const loadFlows = useCallback(() => {
    Services.TestService.ListFlows(projectId)
      .then(f => setFlows(f ?? []))
      .catch(e => setError(String(e)))
  }, [projectId])

  const loadHistory = useCallback(() => {
    Services.TestService.ListRuns(projectId)
      .then(r => setHistory(r ?? []))
      .catch(() => {})
  }, [projectId])

  useEffect(() => {
    setRun(null); setError(null)
    loadFlows(); loadHistory()
  }, [projectId, loadFlows, loadHistory])

  useEffect(() => {
    if (flows.length && !flows.some(f => f.name === selectedFlow)) setSelectedFlow(flows[0].name)
  }, [flows, selectedFlow])

  const doRun = async () => {
    if (!selectedFlow) return
    setRunning(true); setError(null); setRun(null)
    try {
      const result = await Services.TestService.Run(projectId, selectedFlow, baselineEnv, updateEnv, model)
      setRun(result)
      loadHistory()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setRunning(false)
    }
  }

  const doAuthor = async () => {
    const desc = authorText.trim()
    if (!desc) return
    setError(null)
    try {
      const steps = await Services.TestService.AuthorFlow(projectId, desc)
      const name = window.prompt('Naam voor deze flow?')
      if (!name) return
      const next = [...flows, { name, steps } as Flow]
      await Services.TestService.SaveFlows(projectId, next)
      setAuthoring(false); setAuthorText('')
      loadFlows(); setSelectedFlow(name)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  const viewRun = async (r: TestRun) => {
    try {
      const full = await Services.TestService.GetRun(projectId, r.id)
      setRun(full)
    } catch (e) {
      setError(String(e))
    }
  }

  const steps = run?.steps ?? []

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-hidden">
      {/* Run config bar */}
      <div className="shrink-0 flex items-center gap-2 px-6 py-3 border-b border-border bg-panel flex-wrap">
        <select value={selectedFlow} onChange={e => setSelectedFlow(e.target.value)}
          className="bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg">
          {flows.length === 0 && <option value="">geen flows</option>}
          {flows.map(f => <option key={f.name} value={f.name}>{f.name}</option>)}
        </select>
        <span className="text-fg-faint text-xs">op</span>
        <select value={updateEnv} onChange={e => setUpdateEnv(e.target.value as EnvKey)}
          className="bg-panel-2 border border-border rounded-lg px-2 py-1.5 text-[12.5px] text-fg">
          {(['local', 'acc', 'prod'] as EnvKey[]).map(k => <option key={k} value={k}>{ENV_LABELS[k]}</option>)}
        </select>
        <span className="text-fg-faint text-xs">↔</span>
        <select value={baselineEnv} onChange={e => setBaselineEnv(e.target.value as EnvKey)}
          className="bg-panel-2 border border-border rounded-lg px-2 py-1.5 text-[12.5px] text-fg">
          {(['prod', 'acc', 'local'] as EnvKey[]).map(k => <option key={k} value={k}>{ENV_LABELS[k]} (release)</option>)}
        </select>
        <select value={model} onChange={e => setModel(e.target.value as ModelTier)}
          className="bg-panel-2 border border-border rounded-lg px-2 py-1.5 text-[12.5px] text-fg-muted">
          <option value="">model: auto</option>
          <option value="haiku">haiku</option>
          <option value="sonnet">sonnet</option>
          <option value="opus">opus</option>
        </select>
        <button onClick={doRun} disabled={running || !selectedFlow}
          className="ml-auto bg-accent text-white text-[12.5px] font-semibold px-4 py-1.5 rounded-lg hover:brightness-110 disabled:opacity-50 transition">
          {running ? <span className="animate-spin inline-block">↻</span> : '▶ Run'}
        </button>
        <button onClick={() => setAuthoring(a => !a)}
          className="text-[12.5px] text-fg-muted border border-border rounded-lg px-3 py-1.5 hover:bg-hover transition">
          + Flow
        </button>
      </div>

      {authoring && (
        <div className="shrink-0 px-6 py-3 border-b border-border bg-panel-2">
          <textarea value={authorText} onChange={e => setAuthorText(e.target.value)}
            placeholder="Beschrijf de flow in natuurlijke taal, bijv. 'Ga naar de homepage, accepteer cookies, open het menu en ga naar Contact.'"
            className="w-full h-20 bg-panel border border-border rounded-lg p-2.5 text-[12.5px] text-fg resize-none" />
          <div className="flex justify-end gap-2 mt-2">
            <button onClick={() => setAuthoring(false)} className="text-[12px] text-fg-muted px-3 py-1.5">Annuleer</button>
            <button onClick={doAuthor} className="text-[12px] font-semibold text-white bg-accent px-3 py-1.5 rounded-lg">Genereer stappen</button>
          </div>
        </div>
      )}

      {error && <div className="shrink-0 mx-6 mt-3 bg-red-soft text-red px-3 py-2 rounded-lg text-xs">{error}</div>}

      <div className="flex-1 min-h-0 overflow-y-auto px-6 py-4">
        {run ? (
          <>
            <div className="text-[12px] text-fg-muted mb-3">
              {run.flowName} · {ENV_LABELS[run.baselineEnv as EnvKey]} ↔ {ENV_LABELS[run.updateEnv as EnvKey]} · {steps.length} stappen
            </div>
            {steps.map((s: StepResult, i: number) => <StepCard key={i} projectId={projectId} step={s} />)}
          </>
        ) : (
          <div className="text-fg-faint text-[13px] italic py-10 text-center">
            {running ? 'Test loopt…' : 'Kies een flow en druk op Run.'}
          </div>
        )}

        {history.length > 0 && (
          <div className="mt-6">
            <div className="text-[10px] font-semibold tracking-wide text-fg-faint mb-2">HISTORIE</div>
            <div className="bg-panel border border-border rounded-xl divide-y divide-border">
              {history.map(r => (
                <button key={r.id} onClick={() => viewRun(r)}
                  className="w-full flex items-center gap-3 px-4 py-2.5 text-left hover:bg-hover transition">
                  <span className="text-[12px] font-mono text-fg-muted">{r.id}</span>
                  <span className="text-[12px] text-fg">{r.flowName}</span>
                  <span className="ml-auto text-[11px] text-fg-faint">
                    {ENV_LABELS[r.baselineEnv as EnvKey]} ↔ {ENV_LABELS[r.updateEnv as EnvKey]}
                  </span>
                </button>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Verifieer types**

Run: `cd frontend && npx tsc --noEmit 2>&1 | head -20`
Expected: geen fouten in `TestsTab.tsx`. (Als de bindings andere veldnamen hebben — bv. `screenshotBase` vs `ScreenshotBase` — corrigeer naar wat `domain/models.ts` daadwerkelijk exporteert; open dat bestand om de exacte property-namen te controleren.)

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/TestsTab.tsx
git commit -m "feat(frontend): TestsTab UI in house style"
```

---

## Task 3: Registreer de tab in ProjectDetail

**Files:** Modify `frontend/src/components/ProjectDetail.tsx`.

- [ ] **Step 1: Wijzigingen**

- Import bovenaan toevoegen: `import TestsTab from './TestsTab'`
- Type `TabId` uitbreiden met `| 'tests'`.
- In de WORDPRESS-groep, na de `security`-regel, toevoegen (zichtbaar als het een repo is):
```tsx
        ...(status?.isRepo ? [{ id: 'tests' as TabId, label: 'Tests' }] : []),
```
- Bij de tab-content, na de `security`-regel toevoegen:
```tsx
          {activeTab === 'tests' && <TestsTab projectId={project.id} />}
```

- [ ] **Step 2: Verifieer + build**

Run: `cd frontend && npx tsc --noEmit 2>&1 | head -20 && npm run build 2>&1 | tail -8`
Expected: geen type-fouten; build slaagt.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ProjectDetail.tsx
git commit -m "feat(frontend): add Tests tab to project detail"
```

---

## Task 4: Verificatie (optioneel: live preview)

- [ ] **Step 1: Go + frontend groen**

Run: `go test -race ./internal/... && cd frontend && npx tsc --noEmit`
Expected: ok + geen type-fouten.

- [ ] **Step 2 (optioneel, handmatig): draai de app**

`task dev` en open een WordPress-project → tab **Tests**. Controleer: flow-lijst laadt, "+ Flow" genereert stappen, een Run toont screenshots naast elkaar + findings/regressies, historie verschijnt. (Vereist een geconfigureerde Anthropic-key + de sidecar; anders toont de UI nette foutmeldingen.)

- [ ] **Step 3: Commit (indien nodig)**

```bash
git add -A && git commit -m "chore(frontend): tests tab polish" || echo "niets te committen"
```

---

## Self-Review — dekking t.o.v. spec

- **Flow-beheer + NL-authoring in de UI** → Task 2 (flow-select, "+ Flow" → AuthorFlow → SaveFlows). ✓
- **Run-config: omgevingspaar + model** → Task 2 (env/model selects, default prod↔local). ✓
- **Resultaten: screenshots naast elkaar, findings met ernst, regressies** → Task 2 (`StepCard`, `Screenshot`, `SeverityChip`). ✓
- **Historie terugkijken** → Task 2 (history-lijst + `GetRun`). ✓
- **Huisstijl** → bestaande tokens (`bg-panel`, `accent`, `red`/`amber`/`green`, IBM Plex). ✓
- **Screenshots tonen in webview** → Task 1 (`Screenshot` data-URL, pad-gevalideerd). ✓
- **In ProjectDetail** → Task 3. ✓

**Bewust later:** kostenschatting vooraf (nu niet getoond — vergt een aparte estimate-call); sidecar-bundeling in de macOS-build (dev gebruikt `RDM_SIDECAR`/repo-pad); verfijnde flow-editor (nu NL-genereren + opslaan, geen stap-voor-stap editor).

**Placeholder-scan:** geen TBD/TODO; Go-code volledig met TDD; de component is volledig. TS-veldnamen verifiëren tegen `domain/models.ts` in Task 2 Step 2. ✓
**Type-consistentie:** component gebruikt `Services.TestService.{ListFlows,SaveFlows,AuthorFlow,Run,ListRuns,GetRun,Screenshot}` (allen in de bindings na Task 1) en domain-types `Flow`/`TestRun`/`StepResult`/`Finding`/`Regression`. ✓
