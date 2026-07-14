import { useState, useEffect, useCallback, useRef } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { ProjectWorkflow } from '../../bindings/github.com/rdm/sites-tool/internal/services'
import ExternalLink from './ExternalLink'

interface Props {
  projectId: string
}

// Fouten die duiden op een repo zonder (GitHub-)remote horen niet als kapotte
// paneel te tonen; die projecten hebben simpelweg geen GitHub Actions.
function isRemoteMissing(message: string): boolean {
  return message.toLowerCase().includes('remote')
}

function getErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

function compactTime(dateStr: string): string {
  const d = new Date(dateStr)
  if (isNaN(d.getTime())) return ''
  const diff = Math.floor((Date.now() - d.getTime()) / 1000)
  if (diff < 60) return `${diff}s`
  if (diff < 3600) return `${Math.floor(diff / 60)}m`
  if (diff < 86400) return `${Math.floor(diff / 3600)}u`
  return d.toLocaleDateString('nl-NL', { day: 'numeric', month: 'short' })
}

interface StatusChip {
  label: string
  className: string
}

function statusChipFor(wf: ProjectWorkflow): StatusChip {
  if (wf.runStatus === 'completed' && wf.runConclusion === 'success') {
    return { label: '✓ geslaagd', className: 'text-green bg-green-soft' }
  }
  if (wf.runStatus === 'completed' && wf.runConclusion === 'failure') {
    return { label: '✗ mislukt', className: 'text-red bg-red-soft' }
  }
  if (wf.runStatus === 'in_progress' || wf.runStatus === 'queued') {
    const t = compactTime(wf.runAt)
    return { label: `⏳ bezig${t ? ' · ' + t : ''}`, className: 'text-amber bg-amber-soft' }
  }
  if (wf.runStatus === '') {
    return { label: 'nog niet gedraaid', className: 'text-fg-faint bg-panel-2' }
  }
  return {
    label: `${wf.runStatus}${wf.runConclusion ? ' / ' + wf.runConclusion : ''}`,
    className: 'text-fg-muted bg-panel-2',
  }
}

function WorkflowRow({
  wf,
  dispatching,
  disabled,
  onDispatch,
}: {
  wf: ProjectWorkflow
  dispatching: boolean
  disabled: boolean
  onDispatch: () => void
}) {
  const chip = statusChipFor(wf)
  const chipEl = (
    <span className={`text-[11.5px] font-semibold px-2.5 py-[3px] rounded-[7px] whitespace-nowrap ${chip.className}`}>
      {chip.label}
    </span>
  )

  return (
    <div className="px-4 py-3 flex items-center gap-3.5 hover:bg-hover transition-colors">
      <div className="flex-1 min-w-0">
        <p className="text-[13px] font-medium text-fg truncate">{wf.name}</p>
        <p className="text-[11.5px] font-mono text-fg-faint truncate mt-0.5">{wf.path}</p>
      </div>

      {wf.runUrl ? (
        <ExternalLink
          href={wf.runUrl}
          title="Bekijk run op GitHub"
          className="shrink-0 inline-flex items-center gap-1 hover:brightness-95 transition-colors"
        >
          {chipEl}
          <span className="text-fg-faint text-[10px]">↗</span>
        </ExternalLink>
      ) : (
        <span className="shrink-0">{chipEl}</span>
      )}

      <button
        onClick={onDispatch}
        disabled={disabled}
        className="shrink-0 px-3 py-[5px] text-[11.5px] font-semibold text-accent bg-accent-soft rounded-[7px] hover:brightness-95 transition-colors disabled:opacity-50 flex items-center gap-1.5"
      >
        {dispatching
          ? <><span className="animate-spin inline-block text-xs">↻</span> Bezig…</>
          : '▶ Start'}
      </button>
    </div>
  )
}

export default function WorkflowsPanel({ projectId }: Props) {
  const [workflows, setWorkflows] = useState<ProjectWorkflow[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [dispatchingId, setDispatchingId] = useState<number | null>(null)

  // Kept in sync every render so async callbacks can detect a project switch
  // that happened after they were kicked off, and bail out instead of
  // clobbering the now-current project's state with stale results.
  const projectRef = useRef(projectId)
  projectRef.current = projectId

  const reloadTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const load = useCallback((): Promise<void> => {
    const pid = projectId
    setLoading(true)
    return Services.SecurityService.ListWorkflows(pid)
      .then(w => {
        if (projectRef.current !== pid) return
        setWorkflows(w ?? [])
        setError(null)
      })
      .catch(e => {
        if (projectRef.current !== pid) return
        setError(getErrorMessage(e))
      })
      .finally(() => {
        if (projectRef.current !== pid) return
        setLoading(false)
      })
  }, [projectId])

  useEffect(() => {
    if (reloadTimerRef.current) {
      clearTimeout(reloadTimerRef.current)
      reloadTimerRef.current = null
    }
    setWorkflows(null)
    setError(null)
    setDispatchingId(null)
    load()
  }, [projectId, load])

  useEffect(() => {
    return () => {
      if (reloadTimerRef.current) clearTimeout(reloadTimerRef.current)
    }
  }, [])

  const dispatch = async (workflowId: number) => {
    const pid = projectId
    setError(null)
    setDispatchingId(workflowId)
    try {
      await Services.SecurityService.DispatchWorkflow(pid, workflowId)
      await new Promise<void>(resolve => {
        reloadTimerRef.current = setTimeout(() => {
          reloadTimerRef.current = null
          resolve()
        }, 2000)
      })
      if (projectRef.current !== pid) return
      await load()
    } catch (e) {
      if (projectRef.current === pid) setError(getErrorMessage(e))
    } finally {
      if (projectRef.current === pid) setDispatchingId(null)
    }
  }

  // Nog geen (succesvolle) data geladen: alleen tonen bij een échte fout,
  // anders blijft het tabblad clean (niet-GitHub projecten, nog aan het laden).
  if (workflows === null) {
    if (loading || !error || isRemoteMissing(error)) return null
    return (
      <div className="bg-panel border border-border rounded-[11px] mb-6">
        <div className="flex items-center justify-between px-4 py-3 border-b border-border">
          <div className="text-[13px] font-semibold text-fg">GitHub Actions</div>
          <button
            onClick={() => load()}
            className="text-fg-muted text-[13px] hover:text-fg transition-colors"
            title="Ververs"
          >
            ⟳
          </button>
        </div>
        <div className="m-4 bg-red-soft text-red px-3 py-2 rounded-lg text-xs">{error}</div>
      </div>
    )
  }

  if (workflows.length === 0) return null

  return (
    <div className="bg-panel border border-border rounded-[11px] overflow-hidden mb-6">
      <div className="flex items-center justify-between px-4 py-3 border-b border-border">
        <div className="text-[13px] font-semibold text-fg">GitHub Actions</div>
        <button
          onClick={() => load()}
          className="text-fg-muted text-[13px] hover:text-fg transition-colors"
          title="Ververs"
        >
          ⟳
        </button>
      </div>
      {error && !isRemoteMissing(error) && (
        <div className="mx-4 mt-3 bg-red-soft text-red px-3 py-2 rounded-lg text-xs">{error}</div>
      )}
      <div className="divide-y divide-border">
        {workflows.map(wf => (
          <WorkflowRow
            key={wf.id}
            wf={wf}
            dispatching={dispatchingId === wf.id}
            disabled={dispatchingId !== null}
            onDispatch={() => dispatch(wf.id)}
          />
        ))}
      </div>
    </div>
  )
}
