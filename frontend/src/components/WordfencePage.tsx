import { useEffect, useState, useCallback } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type {
  FeedMeta,
  ProjectVulnReport,
  ProjectUpdateResult,
} from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { Vulnerability } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import { ShieldIcon, RefreshIcon, CloseIcon } from './icons'

interface Props { onClose: () => void }

const SHOW_LIMIT = 50

export default function WordfencePage({ onClose }: Props) {
  const [vulns, setVulns] = useState<Vulnerability[]>([])
  const [limit, setLimit] = useState(SHOW_LIMIT)
  const [meta, setMeta] = useState<FeedMeta | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [reports, setReports] = useState<ProjectVulnReport[] | null>(null)
  const [selected, setSelected] = useState<Record<string, Set<string>>>({})
  const [results, setResults] = useState<Record<string, ProjectUpdateResult>>({})

  const loadCache = useCallback(async () => {
    const list = (await Services.WordfenceService.List()) ?? []
    list.sort((a, b) => (b.published ?? '').localeCompare(a.published ?? ''))
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
        <button onClick={compare} disabled={busy || vulns.length === 0}
          className="px-3 py-1.5 bg-panel border border-border rounded-lg text-[12.5px] font-medium
                     text-fg hover:bg-hover disabled:opacity-50 transition-colors shrink-0">
          Vergelijk met projecten
        </button>
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

      <div className="flex-1 overflow-y-auto px-6 py-5 space-y-6">
        {error && <p className="text-[12.5px] text-red">{error}</p>}

        <div>
          <div className="flex items-center gap-3 mb-2">
            <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase">CVE-feed</h3>
          </div>
          <ul className="space-y-1">
            {vulns.slice(0, limit).map(v => (
              <li key={v.id} className="text-[12.5px] text-fg-muted border-b border-border/50 py-1">
                <span className="font-mono text-fg">{v.software?.[0]?.slug ?? '—'}</span>
                {' · '}{v.title}
                {v.cve && <span className="ml-2 text-fg-faint">{v.cve}</span>}
              </li>
            ))}
          </ul>
          {vulns.length > limit && (
            <button onClick={() => setLimit(l => l + SHOW_LIMIT)}
              className="mt-2 text-[12px] text-accent hover:underline">Meer laden ({vulns.length - limit})</button>
          )}
        </div>

        {reports && (
          <div className="space-y-4">
            <div className="flex items-center gap-3">
              <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase">Getroffen projecten</h3>
              <button onClick={updateSelected} disabled={busy}
                className="ml-auto px-3 py-1.5 bg-accent hover:bg-accent-2 text-white text-[13px] font-semibold rounded-lg disabled:opacity-50">
                Update geselecteerde
              </button>
            </div>
            {reports.length === 0 && <p className="text-[12.5px] text-fg-faint">Geen kwetsbare plugins gevonden.</p>}
            {reports.map(r => {
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
