import { useState, useEffect, useCallback } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { Flow, TestRun, StepResult, Finding, Regression } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import type { EnvKey as DomainEnvKey, ModelTier as DomainModelTier } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'

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
      const result = await Services.TestService.Run(
        projectId,
        selectedFlow,
        baselineEnv as unknown as DomainEnvKey,
        updateEnv as unknown as DomainEnvKey,
        model as unknown as DomainModelTier,
      )
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
