import { useState, useEffect } from 'react'
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

const deployTypeBadge: Record<string, string> = {
  wordpress_kinsta:  'bg-violet-500/20 text-violet-700 ring-violet-500/30',
  wordpress_transip: 'bg-sky-500/20 text-sky-700 ring-sky-500/30',
  wordpress_5_2:     'bg-blue-500/20 text-blue-700 ring-blue-500/30',
}

function LinkRow({ label, url }: { label: string; url: string }) {
  if (!url) return null
  return (
    <div className="flex items-center gap-3 py-2 border-b border-black/[0.06]">
      <span className="text-[11px] text-gray-600 w-12 shrink-0 uppercase tracking-wide">{label}</span>
      <a
        href={url}
        target="_blank"
        rel="noopener noreferrer"
        className="text-sm text-indigo-700 hover:text-indigo-800 truncate flex-1 font-mono hover:underline transition-colors"
        onClick={e => e.stopPropagation()}
      >
        {url}
      </a>
      <button
        onClick={() => navigator.clipboard.writeText(url)}
        title="Kopieer URL"
        className="text-gray-600 hover:text-gray-800 text-xs shrink-0 transition-colors px-1"
      >
        ⎘
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

  return (
    <div>
      <h3 className="text-[11px] font-semibold uppercase tracking-wider text-gray-600 mb-3 px-1">
        Docker / Make
      </h3>
      <div className="bg-black/[0.03] rounded-xl p-3 space-y-3">
        {/* Primary targets */}
        <div className="flex flex-wrap gap-2">
          {primary.map(t => (
            <button
              key={t.name}
              onClick={() => run(t.name)}
              disabled={running !== null}
              className={`px-3 py-1.5 text-xs font-medium rounded-lg transition-colors disabled:opacity-50 flex items-center gap-1.5
                ${t.name === 'up'
                  ? 'bg-emerald-500/20 hover:bg-emerald-500/30 text-emerald-800'
                  : t.name === 'down'
                  ? 'bg-red-500/20 hover:bg-red-500/30 text-red-800'
                  : 'bg-black/[0.07] hover:bg-black/[0.10] text-gray-800'}`}
            >
              {running === t.name && <span className="animate-spin inline-block text-xs">↻</span>}
              {t.name === 'up' ? '▶ up' : t.name === 'down' ? '■ down' : `make ${t.name}`}
            </button>
          ))}
          {secondary.length > 0 && (
            <select
              onChange={e => { if (e.target.value) { run(e.target.value); e.target.value = '' } }}
              disabled={running !== null}
              className="bg-black/[0.05] text-xs text-gray-600 rounded-lg px-2 py-1.5 outline-none
                         border border-black/[0.10] disabled:opacity-50 cursor-pointer"
            >
              <option value="">Meer…</option>
              {secondary.map(t => <option key={t.name} value={t.name}>{t.name}</option>)}
            </select>
          )}
        </div>

        {/* Output */}
        {showOutput && result && (
          <div className="relative">
            <div className={`rounded-lg p-2 text-[11px] font-mono whitespace-pre-wrap max-h-40 overflow-y-auto
              ${result.success ? 'bg-gray-200 text-gray-700' : 'bg-red-100 text-red-700'}`}>
              {result.output || (result.success ? '✓ Klaar' : 'Mislukt')}
            </div>
            <button
              onClick={() => setShowOutput(false)}
              className="absolute top-1 right-1 text-gray-600 hover:text-gray-800 text-xs px-1"
            >✕</button>
          </div>
        )}
        {running && (
          <div className="flex items-center gap-2 text-xs text-gray-600">
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
  const typeBadge = deployTypeBadge[deploy?.type] ?? 'bg-gray-200 text-gray-700 ring-gray-400/50'
  const links = deploy?.link ?? {}

  const hasLinks = links.test || links.acc || links.prod

  return (
    <div className="flex-1 overflow-y-auto p-4 space-y-5">
      {/* Header info */}
      <div className="bg-black/[0.03] rounded-xl p-4 space-y-3">
        <div className="flex items-start gap-3">
          <div className="flex-1 min-w-0">
            <h2 className="text-base font-semibold text-gray-900 truncate">{project.displayName}</h2>
            <p className="text-xs text-gray-600 font-mono mt-0.5 truncate">{project.path}</p>
          </div>
          {deploy?.type && (
            <span className={`text-[11px] font-medium px-2 py-0.5 rounded ring-1 shrink-0 ${typeBadge}`}>
              {typeLabel}
            </span>
          )}
        </div>

        {/* Git branch info */}
        {project.git?.isRepo && (
          <div className="flex items-center gap-2 text-xs">
            <span className="text-gray-600">Branch</span>
            <span className="font-mono text-gray-700">{project.git.branch}</span>
            {(project.git.ahead ?? 0) > 0 && (
              <span className="text-emerald-600 text-[11px]">↑{project.git.ahead}</span>
            )}
            {(project.git.behind ?? 0) > 0 && (
              <span className="text-red-600 text-[11px]">↓{project.git.behind}</span>
            )}
          </div>
        )}
      </div>

      {/* URLs */}
      {hasLinks && (
        <div>
          <h3 className="text-[11px] font-semibold uppercase tracking-wide text-gray-600 mb-1 px-1">
            Omgevingen
          </h3>
          <div className="bg-black/[0.03] rounded-xl px-4 divide-y divide-black/[0.06]">
            <LinkRow label="Test" url={links.test ?? ''} />
            <LinkRow label="Acc" url={links.acc ?? ''} />
            <LinkRow label="Prod" url={links.prod ?? ''} />
          </div>
        </div>
      )}

      {/* Extra info rows */}
      <div>
        <h3 className="text-[11px] font-semibold uppercase tracking-wide text-gray-600 mb-1 px-1">
          Details
        </h3>
        <div className="bg-black/[0.03] rounded-xl px-4 divide-y divide-black/[0.06] text-xs">
          <div className="flex items-center gap-3 py-2">
            <span className="text-gray-600 w-24 shrink-0">Type</span>
            <span className="text-gray-700">{typeLabel}</span>
          </div>
          <div className="flex items-center gap-3 py-2">
            <span className="text-gray-600 w-24 shrink-0">Git repo</span>
            <span className={project.git?.isRepo ? 'text-emerald-600' : 'text-gray-600'}>
              {project.git?.isRepo ? 'Ja' : 'Nee'}
            </span>
          </div>
          {project.git?.isRepo && (
            <div className="flex items-center gap-3 py-2">
              <span className="text-gray-600 w-24 shrink-0">Wijzigingen</span>
              <span className={
                (project.git.staged?.length ?? 0) + (project.git.unstaged?.length ?? 0) + (project.git.untracked?.length ?? 0) > 0
                  ? 'text-amber-500'
                  : 'text-gray-600'
              }>
                {(() => {
                  const n = (project.git.staged?.length ?? 0) + (project.git.unstaged?.length ?? 0) + (project.git.untracked?.length ?? 0)
                  return n > 0 ? `${n} ongecommit` : 'Schoon'
                })()}
              </span>
            </div>
          )}
        </div>
      </div>

      {/* Makefile / Docker actions */}
      <MakePanel projectId={project.id} />
    </div>
  )
}
