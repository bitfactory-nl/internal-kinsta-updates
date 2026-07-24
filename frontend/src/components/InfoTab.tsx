import { useState, useEffect } from 'react'
import ExternalLink from './ExternalLink'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { MakeTarget, MakeResult } from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { Project } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'

interface Props { project: Project }

// Targets to show as primary buttons (in order)
const PRIMARY_TARGETS = ['up', 'down', 'build', 'update', 'install']

const deployTypeLabel: Record<string, string> = {
  wordpress_kinsta:  'WordPress / Kinsta',
  wordpress_transip: 'WordPress / TransIP',
  wordpress_5_2:     'WordPress 5.2',
}

function LinkRow({ label, url }: { label: string; url: string }) {
  if (!url) return null
  return (
    <div className="flex items-center gap-4 px-4 py-3.5">
      <span className="w-[52px] shrink-0 text-[11px] font-semibold tracking-[.05em] text-fg-muted uppercase">{label}</span>
      <ExternalLink
        href={url}
        className="flex-1 truncate font-mono text-[13px] font-medium text-accent hover:text-accent-2 hover:underline transition-colors"
      >
        {url}
      </ExternalLink>
      <button
        onClick={() => navigator.clipboard.writeText(url)}
        title="Kopieer URL"
        className="w-6 h-6 shrink-0 inline-flex items-center justify-center rounded-md border border-border text-fg-faint hover:text-fg hover:bg-hover font-mono text-xs transition-colors"
      >
        ⧉
      </button>
    </div>
  )
}

function MakePanel({ projectId }: { projectId: string }) {
  const [targets, setTargets] = useState<MakeTarget[]>([])
  const [running, setRunning] = useState<string | null>(null)
  const [result, setResult] = useState<MakeResult | null>(null)
  const [showOutput, setShowOutput] = useState(false)

  useEffect(() => {
    Services.MakeService.HasMakefile(projectId).then(has => {
      if (has) Services.MakeService.GetTargets(projectId).then(t => setTargets(t ?? []))
    }).catch(() => {})
  }, [projectId])

  if (targets.length === 0) return null

  const run = async (target: string) => {
    setRunning(target)
    setResult(null)
    setShowOutput(true)
    try {
      const r = await Services.MakeService.Run(projectId, target)
      setResult(r)
    } catch (e) {
      setResult({ target, output: String(e), success: false })
    } finally {
      setRunning(null)
    }
  }

  const primary = targets.filter(t => PRIMARY_TARGETS.includes(t.name))
    .sort((a, b) => PRIMARY_TARGETS.indexOf(a.name) - PRIMARY_TARGETS.indexOf(b.name))
  const secondary = targets.filter(t => !PRIMARY_TARGETS.includes(t.name))
  const dockerTargets = primary.filter(t => t.name === 'up' || t.name === 'down')
  const makeTargets = primary.filter(t => t.name !== 'up' && t.name !== 'down')

  return (
    <div>
      <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">
        Docker / Make
      </h3>
      <div className="bg-panel border border-border rounded-[11px] p-4 space-y-3">
        {/* Primary targets */}
        <div className="flex flex-wrap items-center gap-[9px]">
          {dockerTargets.map(t => (
            <button
              key={t.name}
              onClick={() => run(t.name)}
              disabled={running !== null}
              className={`inline-flex items-center gap-[7px] px-[15px] py-2 text-[12.5px] font-semibold rounded-lg transition-colors disabled:opacity-50
                ${t.name === 'up' ? 'text-green bg-green-soft hover:brightness-95' : 'text-red bg-red-soft hover:brightness-95'}`}
            >
              {running === t.name && <span className="animate-spin inline-block text-xs">↻</span>}
              {t.name === 'up' ? '▶ up' : '■ down'}
            </button>
          ))}
          {dockerTargets.length > 0 && (makeTargets.length > 0 || secondary.length > 0) && (
            <div className="w-px h-5 bg-border mx-[3px]" />
          )}
          {makeTargets.map(t => (
            <button
              key={t.name}
              onClick={() => run(t.name)}
              disabled={running !== null}
              className="inline-flex items-center gap-[7px] px-3.5 py-2 text-[12.5px] font-medium font-mono text-fg bg-panel-2 border border-border rounded-lg hover:bg-hover transition-colors disabled:opacity-50"
            >
              {running === t.name && <span className="animate-spin inline-block text-xs">↻</span>}
              {`make ${t.name}`}
            </button>
          ))}
          {secondary.length > 0 && (
            <select
              onChange={e => { if (e.target.value) { run(e.target.value); e.target.value = '' } }}
              disabled={running !== null}
              className="px-3.5 py-2 text-[12.5px] font-medium text-fg-muted bg-panel-2 border border-border rounded-lg outline-none disabled:opacity-50 cursor-pointer hover:bg-hover transition-colors"
            >
              <option value="">Meer…</option>
              {secondary.map(t => <option key={t.name} value={t.name}>{t.name}</option>)}
            </select>
          )}
        </div>

        {/* Output */}
        {showOutput && result && (
          <div className="relative">
            <div className={`rounded-lg border p-2.5 text-[11px] font-mono whitespace-pre-wrap max-h-40 overflow-y-auto
              ${result.success ? 'bg-panel-2 border-border text-fg-muted' : 'bg-red-soft border-border text-red'}`}>
              {result.output || (result.success ? '✓ Klaar' : 'Mislukt')}
            </div>
            <button
              onClick={() => setShowOutput(false)}
              className="absolute top-1 right-1 text-fg-faint hover:text-fg text-xs px-1 transition-colors"
            >✕</button>
          </div>
        )}
        {running && (
          <div className="flex items-center gap-2 text-xs text-fg-muted">
            <span className="animate-spin inline-block">↻</span>
            <span>make {running} uitvoeren…</span>
          </div>
        )}
      </div>
    </div>
  )
}

export default function InfoTab({ project }: Props) {
  const deploy = project.deploy
  const typeLabel = deployTypeLabel[deploy?.type] ?? deploy?.type ?? '—'
  const links = deploy?.link ?? {}

  const hasLinks = links.test || links.acc || links.prod

  const changeCount = (project.git?.staged?.length ?? 0)
    + (project.git?.unstaged?.length ?? 0)
    + (project.git?.untracked?.length ?? 0)

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="max-w-[920px] px-[34px] pt-[30px] pb-[60px]">
        {/* Header */}
        <div className="flex items-start justify-between gap-4 mb-2">
          <div className="min-w-0">
            <h1 className="text-2xl font-bold tracking-[-.02em] text-fg truncate">{project.displayName}</h1>
            <p className="font-mono font-[450] text-[12.5px] text-fg-muted mt-[5px] truncate">{project.path}</p>
          </div>
          {deploy?.type && (
            <span className="shrink-0 text-[11px] font-semibold text-purple bg-accent-soft px-[11px] py-[5px] rounded-[7px]">
              {typeLabel}
            </span>
          )}
        </div>

        {/* Git branch chip */}
        {project.git?.isRepo && (
          <div className="inline-flex items-center gap-[7px] font-mono text-xs font-medium text-fg-muted bg-panel border border-border px-[11px] py-1.5 rounded-lg mb-[30px]">
            <span className="text-fg-faint">Branch</span>
            <span>⑂ {project.git.branch}</span>
            {(project.git.ahead ?? 0) > 0 && (
              <span className="text-green text-[11px]">↑{project.git.ahead}</span>
            )}
            {(project.git.behind ?? 0) > 0 && (
              <span className="text-red text-[11px]">↓{project.git.behind}</span>
            )}
          </div>
        )}
        {!project.git?.isRepo && <div className="mb-[30px]" />}

        {/* URLs */}
        {hasLinks && (
          <div className="mb-7">
            <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">
              Omgevingen
            </h3>
            <div className="bg-panel border border-border rounded-[11px] overflow-hidden divide-y divide-border">
              <LinkRow label="Test" url={links.test ?? ''} />
              <LinkRow label="Acc" url={links.acc ?? ''} />
              <LinkRow label="Prod" url={links.prod ?? ''} />
            </div>
          </div>
        )}

        {/* Extra info rows */}
        <div className="mb-7">
          <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">
            Details
          </h3>
          <div className="bg-panel border border-border rounded-[11px] overflow-hidden divide-y divide-border">
            <div className="flex items-center px-4 py-3">
              <span className="w-[150px] shrink-0 text-[13px] font-[450] text-fg-muted">Type</span>
              <span className="text-[13px] font-medium text-fg">{typeLabel}</span>
            </div>
            <div className="flex items-center px-4 py-3">
              <span className="w-[150px] shrink-0 text-[13px] font-[450] text-fg-muted">Git repo</span>
              <span className={`text-[13px] font-medium ${project.git?.isRepo ? 'text-green' : 'text-fg-muted'}`}>
                {project.git?.isRepo ? 'Ja' : 'Nee'}
              </span>
            </div>
            {project.git?.isRepo && (
              <div className="flex items-center px-4 py-3">
                <span className="w-[150px] shrink-0 text-[13px] font-[450] text-fg-muted">Wijzigingen</span>
                <span className={`text-[13px] font-medium ${changeCount > 0 ? 'text-amber' : 'text-fg-muted'}`}>
                  {changeCount > 0 ? `${changeCount} ongecommit` : 'Schoon'}
                </span>
              </div>
            )}
          </div>
        </div>

        {/* Makefile / Docker actions */}
        <MakePanel projectId={project.id} />
      </div>
    </div>
  )
}
