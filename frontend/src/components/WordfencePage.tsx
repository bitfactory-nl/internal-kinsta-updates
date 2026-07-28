import { useEffect, useMemo, useState, useCallback } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type {
  FeedMeta,
  ProjectVulnReport,
  ProjectUpdateResult,
} from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { Vulnerability } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import { ShieldIcon, RefreshIcon, CloseIcon } from './icons'
import ExternalLink from './ExternalLink'

interface Props { onClose: () => void }

type Tab = 'feed' | 'projects'
type SortBy = 'date' | 'score'

const SHOW_LIMIT = 50

// CVSS rating -> badge color. Reuses the same severity tokens as
// SecurityTab's severityStyles; Wordfence rating strings are exactly
// Critical | High | Medium | Low.
const CVSS_BADGE_STYLES: Record<string, string> = {
  critical: 'text-red bg-red-soft',
  high: 'text-orange bg-orange-soft',
  medium: 'text-amber bg-amber-soft',
  low: 'text-fg-muted bg-hover',
}

function CvssBadge({ score, rating, vector }: { score?: number; rating?: string; vector?: string }) {
  if (!score || score <= 0) return null
  const cls = CVSS_BADGE_STYLES[(rating ?? '').toLowerCase()] ?? CVSS_BADGE_STYLES.low
  return (
    <span
      title={vector || undefined}
      className={`w-10 shrink-0 text-center text-[11px] font-bold font-mono py-[3px] rounded-[5px] ${cls}`}
    >
      {score.toFixed(1)}
    </span>
  )
}

// Go's zero time.Time ("0001-01-01T00:00:00Z") means "not set"; treat it the
// same as a missing value.
function isZeroDate(iso?: string | null): boolean {
  return !iso || iso.startsWith('0001-01-01')
}

// Formats as dd-mm-jjjj using UTC getters: the feed's dates are calendar
// dates (anchored at UTC midnight), so UTC avoids off-by-one-day shifts for
// viewers west of UTC.
function formatNlDate(iso?: string | null): string | null {
  if (isZeroDate(iso)) return null
  const d = new Date(iso as string)
  if (isNaN(d.getTime())) return null
  const dd = String(d.getUTCDate()).padStart(2, '0')
  const mm = String(d.getUTCMonth() + 1).padStart(2, '0')
  return `${dd}-${mm}-${d.getUTCFullYear()}`
}

function TabBtn({ active, onClick, children }: {
  active: boolean; onClick: () => void; children: React.ReactNode
}) {
  return (
    <button
      onClick={onClick}
      className={`px-1 py-2.5 -mb-px text-[12.5px] font-medium border-b-2 transition-colors select-none
        ${active
          ? 'border-accent text-fg'
          : 'border-transparent text-fg-muted hover:text-fg'}`}
    >
      {children}
    </button>
  )
}

export default function WordfencePage({ onClose }: Props) {
  const [tab, setTab] = useState<Tab>('feed')
  const [vulns, setVulns] = useState<Vulnerability[]>([])
  const [limit, setLimit] = useState(SHOW_LIMIT)
  const [meta, setMeta] = useState<FeedMeta | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [reports, setReports] = useState<ProjectVulnReport[] | null>(null)
  const [selected, setSelected] = useState<Record<string, Set<string>>>({})
  const [results, setResults] = useState<Record<string, ProjectUpdateResult>>({})
  const [filter, setFilter] = useState('')
  const [sortBy, setSortBy] = useState<SortBy>('date')

  const loadCache = useCallback(async () => {
    const list = (await Services.WordfenceService.List()) ?? []
    setVulns(list)
    setMeta(await Services.WordfenceService.LastFetched())
  }, [])

  useEffect(() => { loadCache().catch(e => setError(String(e))) }, [loadCache])

  const refresh = async () => {
    setBusy(true); setError(null)
    try {
      await Services.WordfenceService.Refresh()
      await loadCache()
    } catch (e) { setError(String(e)) } finally { setBusy(false) }
  }

  const compare = async () => {
    setBusy(true); setError(null); setResults({})
    try {
      const reps = (await Services.WordfenceService.MatchProjects()) ?? []
      setReports(reps)
      // preselect all wporg-sourced findings
      const pre: Record<string, Set<string>> = {}
      for (const r of reps) {
        pre[r.projectId] = new Set(
          r.findings.filter(f => f.source === 'wporg').map(f => f.slug),
        )
      }
      setSelected(pre)
    } catch (e) { setError(String(e)) } finally { setBusy(false) }
  }

  const toggle = (pid: string, slug: string) => {
    setSelected(prev => {
      const next = { ...prev }
      const set = new Set(next[pid] ?? [])
      set.has(slug) ? set.delete(slug) : set.add(slug)
      next[pid] = set
      return next
    })
  }

  const applyProject = async (pid: string, autoStash: boolean) => {
    const slugs = Array.from(selected[pid] ?? [])
    if (slugs.length === 0) return
    setBusy(true)
    try {
      const res = await Services.WordfenceUpdateService.ApplyProject({ projectId: pid, slugs }, autoStash)
      setResults(prev => ({ ...prev, [pid]: res }))
    } catch (e) {
      setError(String(e))
    } finally { setBusy(false) }
  }

  const updateSelected = async () => {
    for (const r of reports ?? []) {
      if ((selected[r.projectId]?.size ?? 0) > 0) {
        await applyProject(r.projectId, false)
      }
    }
  }

  // Opening the projects tab runs the comparison automatically the first time.
  const openProjectsTab = () => {
    setTab('projects')
    if (!reports && !busy && vulns.length > 0) void compare()
  }

  // Search filters on title, CVE-ID, software slug, researcher name and
  // rating; sort is either by publish date (default) or CVSS score (high→low).
  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase()
    const list = !q ? vulns : vulns.filter(v =>
      v.title.toLowerCase().includes(q) ||
      (v.cve ?? '').toLowerCase().includes(q) ||
      (v.severity ?? '').toLowerCase().includes(q) ||
      (v.researchers ?? []).some(r => r.toLowerCase().includes(q)) ||
      (v.software ?? []).some(s => s.slug.toLowerCase().includes(q))
    )
    return [...list].sort((a, b) => sortBy === 'score'
      ? (b.cvssScore ?? 0) - (a.cvssScore ?? 0)
      : (b.published ?? '').localeCompare(a.published ?? ''))
  }, [vulns, filter, sortBy])

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-hidden bg-bg">
      {/* ── toolbar ── */}
      <div className="h-14 px-[22px] bg-panel border-b border-border shrink-0 flex items-center gap-3">
        <span className="w-8 h-8 flex items-center justify-center rounded-lg bg-hover text-fg-muted shrink-0">
          <ShieldIcon size={16} />
        </span>
        <div className="flex-1 min-w-0">
          <h2 className="text-[15px] font-bold tracking-[-.01em] text-fg leading-tight truncate">
            CVE kwetsbaarheden
          </h2>
          {meta && meta.count > 0 && (
            <p className="text-[11px] text-fg-faint leading-tight truncate">
              {meta.count} CVE&apos;s · bijgewerkt {new Date(meta.fetchedAt).toLocaleString()}
            </p>
          )}
        </div>
        <button onClick={refresh} disabled={busy}
          className="px-3 py-1.5 bg-accent hover:bg-accent-2 text-white text-[12.5px] font-semibold
                     rounded-lg disabled:opacity-50 transition-colors shrink-0 flex items-center gap-1.5">
          <span className={`inline-flex ${busy ? 'animate-spin' : ''}`}>
            <RefreshIcon size={13} />
          </span>
          {busy ? 'Bezig…' : 'Vernieuwen'}
        </button>
        <button onClick={onClose}
          className="w-7 h-7 flex items-center justify-center rounded-md text-fg-muted
                     hover:text-fg hover:bg-hover transition-colors shrink-0"
          title="Sluiten">
          <CloseIcon size={15} />
        </button>
      </div>

      {/* ── tabs ── */}
      <div className="px-6 bg-panel border-b border-border shrink-0 flex items-center gap-5">
        <TabBtn active={tab === 'feed'} onClick={() => setTab('feed')}>
          CVE-feed{vulns.length > 0 ? ` (${vulns.length})` : ''}
        </TabBtn>
        <TabBtn active={tab === 'projects'} onClick={openProjectsTab}>
          Getroffen projecten{reports ? ` (${reports.length})` : ''}
        </TabBtn>
        {tab === 'feed' && vulns.length > 0 && (
          <div className="ml-auto flex items-center gap-2 py-2">
            <div className="flex items-center gap-0.5 bg-bg border border-border rounded-lg p-0.5">
              <button
                onClick={() => setSortBy('date')}
                className={`px-2 py-1 rounded-[6px] text-[11px] font-semibold transition-colors
                  ${sortBy === 'date' ? 'bg-panel text-fg shadow-sm' : 'text-fg-muted hover:text-fg'}`}
              >
                Datum
              </button>
              <button
                onClick={() => setSortBy('score')}
                className={`px-2 py-1 rounded-[6px] text-[11px] font-semibold transition-colors
                  ${sortBy === 'score' ? 'bg-panel text-fg shadow-sm' : 'text-fg-muted hover:text-fg'}`}
              >
                Score
              </button>
            </div>
            <input
              type="search"
              placeholder="Zoek CVE, plugin, onderzoeker…"
              value={filter}
              onChange={e => setFilter(e.target.value)}
              className="w-[200px] bg-bg text-[12.5px] text-fg placeholder-fg-faint rounded-lg px-3 py-[6px]
                         outline-none border border-border focus:border-accent focus:ring-1 focus:ring-accent/30"
            />
          </div>
        )}
      </div>

      <div className="flex-1 overflow-y-auto px-6 py-5 space-y-6">
        {error && <p className="text-[12.5px] text-red">{error}</p>}

        {tab === 'feed' ? (
        <div>
          {vulns.length === 0 && !busy && (
            <p className="text-[12.5px] text-fg-faint">
              Nog geen feed opgehaald — klik op Vernieuwen.
            </p>
          )}
          {vulns.length > 0 && filtered.length === 0 && (
            <p className="text-[12.5px] text-fg-faint">Geen resultaten voor deze zoekopdracht.</p>
          )}
          <ul className="space-y-1">
            {filtered.slice(0, limit).map(v => {
              const published = formatNlDate(v.published)
              const updated = formatNlDate(v.updated)
              const researchers = v.researchers ?? []
              return (
                <li key={v.id} className="flex items-start gap-2.5 text-[12.5px] text-fg-muted border-b border-border/50 py-1.5">
                  <CvssBadge score={v.cvssScore} rating={v.severity} vector={v.cvssVector} />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-baseline gap-2">
                      <span className="font-mono text-fg shrink-0">{v.software?.[0]?.slug ?? '—'}</span>
                      <span className="truncate">{v.title}</span>
                    </div>
                    <div className="flex items-center gap-2 mt-0.5 text-[11px] text-fg-faint flex-wrap">
                      {v.cve && (
                        <ExternalLink
                          href={v.cveLink || `https://nvd.nist.gov/vuln/detail/${v.cve}`}
                          className="font-mono text-accent hover:text-accent-2 transition-colors"
                        >
                          {v.cve}
                        </ExternalLink>
                      )}
                      {researchers.length > 0 && (
                        <span className="truncate max-w-[200px]" title={researchers.join(', ')}>
                          {researchers.slice(0, 2).join(', ')}{researchers.length > 2 ? ' e.a.' : ''}
                        </span>
                      )}
                      {published && (
                        <span title={updated ? `Bijgewerkt: ${updated}` : undefined}>{published}</span>
                      )}
                    </div>
                  </div>
                </li>
              )
            })}
          </ul>
          {filtered.length > limit && (
            <button onClick={() => setLimit(l => l + SHOW_LIMIT)}
              className="mt-2 text-[12px] text-accent hover:underline">Meer laden ({filtered.length - limit})</button>
          )}
        </div>
        ) : (
          <div className="space-y-4">
            <div className="flex items-center gap-2">
              <button onClick={compare} disabled={busy || vulns.length === 0}
                className="px-3 py-1.5 bg-panel border border-border rounded-lg text-[12.5px] font-medium
                           text-fg hover:bg-hover disabled:opacity-50 transition-colors">
                Opnieuw vergelijken
              </button>
              <button onClick={updateSelected} disabled={busy || !reports || reports.length === 0}
                className="ml-auto px-3 py-1.5 bg-accent hover:bg-accent-2 text-white text-[13px] font-semibold rounded-lg disabled:opacity-50">
                Update geselecteerde
              </button>
            </div>
            {!reports && busy && (
              <p className="text-[12.5px] text-fg-faint">Vergelijken met projecten…</p>
            )}
            {!reports && !busy && (
              <p className="text-[12.5px] text-fg-faint">
                {vulns.length === 0
                  ? 'Haal eerst de CVE-feed op via Vernieuwen.'
                  : 'Nog niet vergeleken — klik op "Opnieuw vergelijken".'}
              </p>
            )}
            {reports?.length === 0 && <p className="text-[12.5px] text-fg-faint">Geen kwetsbare plugins gevonden.</p>}
            {reports?.map(r => {
              const res = results[r.projectId]
              return (
                <div key={r.projectId} className="border border-border rounded-lg p-3">
                  <p className="text-[13px] font-semibold text-fg mb-2">{r.projectName}</p>
                  <ul className="space-y-1">
                    {r.findings.map(f => (
                      <li key={f.slug} className="flex items-center gap-2 text-[12.5px]">
                        <input type="checkbox"
                          checked={selected[r.projectId]?.has(f.slug) ?? false}
                          disabled={f.source === 'manual'}
                          onChange={() => toggle(r.projectId, f.slug)} />
                        <span className="font-mono text-fg">{f.slug}</span>
                        <span className="text-fg-faint">{f.installedVersion} → {f.latestVersion || '?'}</span>
                        {f.cve && <span className="text-fg-faint">{f.cve}</span>}
                        {f.source === 'manual' && <span className="text-amber">handmatig (niet op wp.org)</span>}
                      </li>
                    ))}
                  </ul>
                  {res?.status === 'needs_stash' && (
                    <div className="mt-2 text-[12px] text-amber flex items-center gap-2">
                      Werkboom heeft wijzigingen.
                      <button onClick={() => applyProject(r.projectId, true)}
                        className="px-2 py-0.5 bg-amber/20 border border-amber/40 rounded text-amber hover:bg-amber/30">
                        Stash &amp; doorgaan
                      </button>
                    </div>
                  )}
                  {res?.status === 'skipped_no_release' && (
                    <p className="mt-2 text-[12px] text-fg-faint">Overgeslagen: {res.error}</p>
                  )}
                  {res?.status === 'nothing' && (
                    <p className="mt-2 text-[12px] text-fg-faint">Geen plugins bijgewerkt.</p>
                  )}
                  {res?.status === 'error' && <p className="mt-2 text-[12px] text-red">Fout: {res.error}</p>}
                  {res?.status === 'updated' && (
                    <p className="mt-2 text-[12px] text-green">Bijgewerkt op branch <span className="font-mono">{res.branch}</span></p>
                  )}
                  {res?.stashed && (
                    <p className="mt-2 text-[12px] text-amber">
                      Lokale wijzigingen zijn gestasht — gebruik <span className="font-mono">git stash pop</span> om ze terug te halen.
                    </p>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}


