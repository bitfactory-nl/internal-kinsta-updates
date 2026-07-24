import { useState, useEffect } from 'react'
import type { ReactNode } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { UpdateBranch, UpdateDetail } from '../../bindings/github.com/rdm/sites-tool/internal/services'
import WorkflowsPanel from './WorkflowsPanel'

interface Props {
  projectId: string
  currentBranch: string
  onBranchCheckedOut: () => void
}

function formatDate(dateStr: string): string {
  // branch date format: 2026-04-30T11-57-29 (colons replaced by dashes by git)
  // Normalize to ISO: replace last two dashes in time part back to colons
  const normalized = dateStr.replace(/(\d{4}-\d{2}-\d{2}T\d{2})-(\d{2})-(\d{2})/, '$1:$2:$3')
  const d = new Date(normalized)
  if (isNaN(d.getTime())) return dateStr
  return d.toLocaleDateString('nl-NL', {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function timeAgo(dateStr: string): string {
  const normalized = dateStr.replace(/(\d{4}-\d{2}-\d{2}T\d{2})-(\d{2})-(\d{2})/, '$1:$2:$3')
  const d = new Date(normalized)
  if (isNaN(d.getTime())) return ''
  const diff = Math.floor((Date.now() - d.getTime()) / 1000)
  if (diff < 60) return `${diff}s geleden`
  if (diff < 3600) return `${Math.floor(diff / 60)}m geleden`
  if (diff < 86400) return `${Math.floor(diff / 3600)}u geleden`
  return `${Math.floor(diff / 86400)}d geleden`
}

function Section({ title, count, children }: { title: string; count: number; children: ReactNode }) {
  if (count === 0) return null
  return (
    <div className="mt-3">
      <p className="text-[11px] font-semibold uppercase tracking-wide text-fg-faint mb-1.5">
        {title} <span className="text-fg-muted">({count})</span>
      </p>
      <div className="flex flex-col gap-1">{children}</div>
    </div>
  )
}

function Row({ label, badge, badgeClass }: { label: string; badge?: string; badgeClass?: string }) {
  return (
    <div className="flex items-center gap-2 text-[12px] font-mono text-fg">
      <span className="truncate">{label}</span>
      {badge && <span className={`text-[10px] px-1.5 py-0.5 rounded ${badgeClass ?? 'bg-panel-2 text-fg-muted'}`}>{badge}</span>}
    </div>
  )
}

function UpdateDetailPanel({ detail }: { detail: UpdateDetail }) {
  const empty =
    detail.wpCore.length + detail.wpPlugins.length + detail.wpThemes.length +
    detail.npmApplied.length + detail.npmAvailableMajors.length === 0
  if (empty) return <p className="text-fg-faint text-xs italic py-2">Geen update-details in deze branch.</p>
  return (
    <div>
      {detail.source === 'fallback' && (
        <p className="text-[11px] text-fg-faint mt-2">
          ℹ️ Deze branch is gemaakt vóór het update-manifest — hieronder staat alleen wat in de branch zelf zit.
          NPM-updates en beschikbare majors verschijnen bij nieuwe branches automatisch.
        </p>
      )}
      <Section title="WordPress core" count={detail.wpCore.length}>
        {detail.wpCore.map(c => (
          <Row key={c.version} label={c.version} badge={c.updateType} badgeClass={c.updateType === 'major' ? 'bg-amber-soft text-amber' : 'bg-green-soft text-green'} />
        ))}
      </Section>
      <Section title="Plugins" count={detail.wpPlugins.length}>
        {detail.wpPlugins.map(p => <Row key={p.name} label={`${p.name}  ${p.from} → ${p.to}`} />)}
      </Section>
      <Section title="Thema's" count={detail.wpThemes.length}>
        {detail.wpThemes.map(p => <Row key={p.name} label={`${p.name}  ${p.from} → ${p.to}`} />)}
      </Section>
      <Section title="NPM — uitgevoerd" count={detail.npmApplied.length}>
        {detail.npmApplied.map(p => <Row key={p.name} label={`${p.name}  ${p.from} → ${p.to}`} badge={p.type} />)}
      </Section>
      <Section title="⚠️ NPM majors — niet uitgevoerd (meerwerk)" count={detail.npmAvailableMajors.length}>
        {detail.npmAvailableMajors.map(p => <Row key={p.name} label={`${p.name}  ${p.from} → ${p.to}`} badge="major" badgeClass="bg-amber-soft text-amber" />)}
      </Section>
    </div>
  )
}

export default function UpdatesTab({ projectId, currentBranch, onBranchCheckedOut }: Props) {
  const [branches, setBranches] = useState<UpdateBranch[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [checkingOut, setCheckingOut] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [details, setDetails] = useState<Record<string, UpdateDetail>>({})
  const [detailLoading, setDetailLoading] = useState<string | null>(null)

  const toggleExpand = (shortName: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(shortName)) {
        next.delete(shortName)
        return next
      }
      next.add(shortName)
      if (!details[shortName]) {
        setDetailLoading(shortName)
        Services.GitService.GetUpdateBranchDetail(projectId, shortName)
          .then(d => { if (d) setDetails(prevDetails => ({ ...prevDetails, [shortName]: d })) })
          .catch(e => setError(String(e)))
          .finally(() => setDetailLoading(null))
      }
      return next
    })
  }

  const fetchBranches = () => {
    setError(null)
    setLoading(true)
    Services.GitService.GetUpdateBranches(projectId)
      .then(b => setBranches(b ?? []))
      .catch(e => setError(String(e)))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    setBranches(null)
    fetchBranches()
  }, [projectId])

  const checkout = async (branch: UpdateBranch) => {
    setCheckingOut(branch.shortName)
    try {
      await Services.GitService.CheckoutBranch(projectId, branch.shortName)
      onBranchCheckedOut()
      fetchBranches()
    } catch (e) {
      setError(String(e))
    } finally {
      setCheckingOut(null)
    }
  }

  if (loading) {
    return (
      <div className="flex-1 flex items-center justify-center gap-2 text-fg-faint text-sm">
        <span className="animate-spin inline-block">↻</span>
      </div>
    )
  }

  if (error) {
    return <div className="m-4 bg-red-soft text-red px-3 py-2 rounded-lg text-xs">{error}</div>
  }

  if (!branches || branches.length === 0) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center gap-2 text-fg-faint text-[13px] italic py-12">
        <p>Geen update branches gevonden</p>
        <p className="text-xs text-fg-faint not-italic">
          Verwacht: <code className="font-mono text-fg-muted">automated/wp-updates-*</code>, <code className="font-mono text-fg-muted">automated/updates-*</code> of <code className="font-mono text-fg-muted">Updates - *</code>
        </p>
      </div>
    )
  }

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-hidden">
      <div className="flex-1 overflow-y-auto">
        <div className="max-w-[960px] px-7 pt-6 pb-[50px]">
          <WorkflowsPanel projectId={projectId} />

          {/* Summary card */}
          <div className="flex items-center gap-4 bg-panel border border-border rounded-xl px-5 py-[18px] mb-6">
            <div className="w-11 h-11 rounded-[11px] bg-amber-soft flex items-center justify-center font-mono font-semibold text-[17px] text-amber shrink-0">
              {branches.length}
            </div>
            <div className="flex-1 min-w-0">
              <div className="text-[15px] font-semibold text-fg">
                {branches.length} update branch{branches.length !== 1 ? 'es' : ''}
              </div>
            </div>
            <button
              onClick={() => {
                setLoading(true)
                Services.GitService.Fetch(projectId)
                  .then(() => fetchBranches())
                  .catch(e => { setError(String(e)); setLoading(false) })
              }}
              className="bg-panel-2 border border-border text-fg-muted text-[12.5px] font-semibold px-[15px] py-[9px] rounded-[9px] hover:bg-hover transition-colors"
              title="Fetch en ververs"
            >
              ⟳ Ververs
            </button>
          </div>

          {/* Branch list */}
          <div className="bg-panel border border-border rounded-[11px] overflow-hidden divide-y divide-border">
            {branches.map(branch => {
              const isActive = branch.shortName === currentBranch
              const isOpen = expanded.has(branch.shortName)
              const detail = details[branch.shortName]
              return (
                <div key={branch.shortName}>
                  <div className={`px-4 py-3 flex items-center gap-3.5 ${isActive ? 'bg-sel' : 'hover:bg-hover'} transition-colors`}>
                    {/* Expand chevron */}
                    <button
                      onClick={() => toggleExpand(branch.shortName)}
                      className="text-fg-faint hover:text-fg text-xs w-4 shrink-0"
                      title={isOpen ? 'Inklappen' : 'Uitklappen'}
                    >
                      {isOpen ? '▾' : '▸'}
                    </button>

                    {/* Status dot */}
                    <span
                      className={`w-2 h-2 rounded-full shrink-0 ${isActive ? 'bg-accent' : branch.isLocal ? 'bg-green' : 'bg-fg-faint'}`}
                      title={isActive ? 'Actieve branch' : branch.isLocal ? 'Lokaal aanwezig' : 'Alleen remote'}
                    />

                    {/* Branch info */}
                    <div className="flex-1 min-w-0 cursor-pointer" onClick={() => toggleExpand(branch.shortName)}>
                      <p className={`text-[13px] font-mono truncate ${isActive ? 'text-fg font-semibold' : 'text-fg font-medium'}`}>{branch.shortName}</p>
                      <p className="text-[11.5px] font-[450] text-fg-faint mt-0.5">
                        {formatDate(branch.dateStr)}
                        <span className="ml-2">{timeAgo(branch.dateStr)}</span>
                      </p>
                    </div>

                    {/* Action */}
                    {isActive ? (
                      <span className="shrink-0 text-[11.5px] font-semibold text-accent bg-accent-soft px-3 py-[5px] rounded-[7px]">
                        ● actief
                      </span>
                    ) : branch.isLocal ? (
                      <button
                        onClick={() => checkout(branch)}
                        disabled={checkingOut !== null}
                        className="shrink-0 px-3 py-[5px] text-[11.5px] font-semibold text-green bg-green-soft rounded-[7px] hover:brightness-95 transition-colors disabled:opacity-50 flex items-center gap-1.5"
                      >
                        {checkingOut === branch.shortName
                          ? <><span className="animate-spin inline-block text-xs">↻</span> Schakelen…</>
                          : '⇄ Schakel'}
                      </button>
                    ) : (
                      <button
                        onClick={() => checkout(branch)}
                        disabled={checkingOut !== null}
                        className="shrink-0 px-3 py-[5px] text-[11.5px] font-semibold text-accent bg-accent-soft rounded-[7px] hover:brightness-95 transition-colors disabled:opacity-50 flex items-center gap-1.5"
                      >
                        {checkingOut === branch.shortName
                          ? <><span className="animate-spin inline-block text-xs">↻</span> Checken…</>
                          : '↓ Checkout'}
                      </button>
                    )}
                  </div>

                  {isOpen && (
                    <div className="px-4 pb-4 pl-[38px] bg-panel-2/40">
                      {detailLoading === branch.shortName && !detail ? (
                        <div className="text-fg-faint text-xs py-2"><span className="animate-spin inline-block">↻</span> Laden…</div>
                      ) : detail ? (
                        <UpdateDetailPanel detail={detail} />
                      ) : null}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        </div>
      </div>
    </div>
  )
}
