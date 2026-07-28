import { useEffect, useState, useCallback } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { WPCoreReport, CoreUpdateResult } from '../../bindings/github.com/rdm/sites-tool/internal/services'
import { GlobeIcon, RefreshIcon, CloudDownloadIcon } from './icons'
import VersionColumns, { VersionColumnsHeader } from './VersionColumns'
import ExternalLink from './ExternalLink'

// emptyCoreResult is een leeg resultaat om een frontend-fout in dezelfde vorm
// bij de rij te kunnen tonen als een backend-resultaat.
const emptyCoreResult: CoreUpdateResult = {
  projectId: '', projectName: '', status: '', from: '', to: '',
  branch: '', pullRequestUrl: '', error: '',
}

// coreUpdateLabel geeft de tekst per statuscode uit CoreUpdateResult.
function coreUpdateLabel(status: string): string {
  switch (status) {
    case 'pr_created': return 'PR aangemaakt'
    case 'exists': return 'PR bestond al'
    case 'up_to_date': return 'release-branch al actueel'
    case 'skipped_no_release': return 'overgeslagen: geen release-branch'
    default: return 'mislukt'
  }
}

export default function WordPressPage() {
  const [report, setReport] = useState<WPCoreReport | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [filter, setFilter] = useState('')
  const [fetching, setFetching] = useState(false)
  const [fetchNote, setFetchNote] = useState<string | null>(null)
  // Per project de uitkomst van de laatste core-update, plus welke projecten
  // nu bezig zijn (bij de bulk-actie kunnen dat er meer zijn).
  const [updates, setUpdates] = useState<Record<string, CoreUpdateResult>>({})
  const [updating, setUpdating] = useState<Record<string, boolean>>({})
  const [bulkBusy, setBulkBusy] = useState(false)
  const [bulkNote, setBulkNote] = useState<string | null>(null)

  const load = useCallback(async () => {
    setBusy(true); setError(null)
    try {
      setReport(await Services.InventoryService.WordPress())
    } catch (e) {
      setError(String(e))
    } finally {
      setBusy(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  const fetchAll = async () => {
    setFetching(true); setError(null); setFetchNote(null)
    try {
      const res = await Services.InventoryService.FetchAll()
      setFetchNote(`${res.fetched} repo${res.fetched !== 1 ? "'s" : ''} gefetcht${res.errors.length ? ` · ${res.errors.length} mislukt` : ''}`)
      if (res.errors.length) setError(res.errors.join('\n'))
      await load()
    } catch (e) {
      setError(String(e))
    } finally {
      setFetching(false)
    }
  }

  // updateProject vraagt de backend een branch + PR te maken voor één project.
  const updateProject = useCallback(async (projectId: string): Promise<CoreUpdateResult | null> => {
    const target = report?.latestVersion
    if (!target) return null
    setUpdating(prev => ({ ...prev, [projectId]: true }))
    try {
      const res = await Services.WPCoreUpdateService.UpdateProject(projectId, target)
      setUpdates(prev => ({ ...prev, [projectId]: res }))
      return res
    } catch (e) {
      // Fout bij deze ene rij: bij de rij tonen, niet als paginabrede melding
      // die na een bulk-run over de samenvatting heen blijft staan.
      setUpdates(prev => ({
        ...prev,
        [projectId]: { ...emptyCoreResult, projectId, to: target, status: 'error', error: String(e) },
      }))
      return null
    } finally {
      setUpdating(prev => {
        const next = { ...prev }
        delete next[projectId]
        return next
      })
    }
  }, [report?.latestVersion])

  const outdatedProjects = (report?.projects ?? []).filter(p => p.outdated)
  const outdated = outdatedProjects.length

  // updateAll werkt de verouderde projecten één voor één bij. Een mislukking
  // stopt de rest niet; aan het eind volgt een samenvatting.
  const updateAll = async () => {
    const target = report?.latestVersion
    if (!target || outdatedProjects.length === 0) return
    const ok = window.confirm(
      `Voor ${outdatedProjects.length} project(en) een branch update/wordpress-${target} ` +
      `aanmaken vanaf de release-branch en een pull request openen?\n\n` +
      `Er wordt niets naar de release-branch gepusht en niets op live gewijzigd.`
    )
    if (!ok) return

    setBulkBusy(true); setError(null); setBulkNote(null)
    const tally = { pr: 0, exists: 0, skipped: 0, failed: 0 }
    try {
      for (const p of outdatedProjects) {
        const res = await updateProject(p.projectId)
        if (!res) { tally.failed++; continue }
        if (res.status === 'pr_created') tally.pr++
        else if (res.status === 'exists') tally.exists++
        else if (res.status === 'skipped_no_release') tally.skipped++
        else tally.failed++
      }
      setBulkNote(
        `${tally.pr} PR('s) aangemaakt · ${tally.exists} bestonden al · ` +
        `${tally.skipped} overgeslagen · ${tally.failed} mislukt`
      )
    } finally {
      setBulkBusy(false)
    }
  }

  const q = filter.trim().toLowerCase()
  const shown = (report?.projects ?? []).filter(p =>
    !q || p.projectName.toLowerCase().includes(q) ||
    p.githubVersion.toLowerCase().includes(q) || p.localVersion.toLowerCase().includes(q)
  )

  return (
    <div className="flex-1 flex flex-col min-h-0 overflow-hidden bg-bg">
      {/* ── toolbar ── */}
      <div className="h-14 px-[22px] bg-panel border-b border-border shrink-0 flex items-center gap-3">
        <span className="w-8 h-8 flex items-center justify-center rounded-lg bg-hover text-fg-muted shrink-0">
          <GlobeIcon size={16} />
        </span>
        <div className="flex-1 min-w-0">
          <h2 className="text-[15px] font-bold tracking-[-.01em] text-fg leading-tight truncate">WordPress</h2>
          {report && (
            <p className="text-[11px] text-fg-faint leading-tight truncate">
              {report.latestVersion
                ? <>laatste versie <span className="font-mono text-fg">{report.latestVersion}</span></>
                : 'laatste versie onbekend'}
              {outdated > 0 && <> · <span className="text-amber">{outdated} verouderd</span></>}
              {' · GitHub-kolom wordt automatisch bijgewerkt'}
            </p>
          )}
        </div>
        <input
          type="search"
          placeholder="Zoek project of versie…"
          value={filter}
          onChange={e => setFilter(e.target.value)}
          className="w-[200px] bg-bg text-[12.5px] text-fg placeholder-fg-faint rounded-lg px-3 py-[6px]
                     outline-none border border-border focus:border-accent focus:ring-1 focus:ring-accent/30"
        />
        <button onClick={() => void fetchAll()} disabled={fetching || busy}
          title="git fetch in alle project-repo's, zodat origin/… de actuele remote-stand heeft"
          className="px-3 py-1.5 bg-panel border border-border rounded-lg text-[12.5px] font-medium
                     text-fg hover:bg-hover disabled:opacity-50 transition-colors shrink-0 flex items-center gap-1.5">
          <span className={`inline-flex ${fetching ? 'animate-spin' : ''}`}>
            <CloudDownloadIcon size={13} />
          </span>
          {fetching ? 'Fetchen…' : 'Fetch alles'}
        </button>
        {outdated > 0 && report?.latestVersion && (
          <button onClick={() => void updateAll()} disabled={bulkBusy || busy || fetching}
            title={`Voor elk verouderd project een branch update/wordpress-${report.latestVersion} met pull request aanmaken`}
            className="px-3 py-1.5 bg-panel border border-amber/50 rounded-lg text-[12.5px] font-medium
                       text-amber hover:bg-hover disabled:opacity-50 transition-colors shrink-0">
            {bulkBusy ? 'Updaten…' : `Alles updaten (${outdated})`}
          </button>
        )}
        <button onClick={() => void load()} disabled={busy || fetching || bulkBusy}
          className="px-3 py-1.5 bg-accent hover:bg-accent-2 text-white text-[12.5px] font-semibold
                     rounded-lg disabled:opacity-50 transition-colors shrink-0 flex items-center gap-1.5">
          <span className={`inline-flex ${busy ? 'animate-spin' : ''}`}>
            <RefreshIcon size={13} />
          </span>
          {busy ? 'Bezig…' : 'Vernieuwen'}
        </button>
      </div>

      {/* ── list ── */}
      <div className="flex-1 overflow-y-auto px-6 py-4">
        {error && <p className="text-[12.5px] text-red mb-3 whitespace-pre-line">{error}</p>}
        {fetchNote && <p className="text-[12px] text-fg-faint mb-3">{fetchNote}</p>}
        {bulkNote && <p className="text-[12px] text-fg-muted mb-3">{bulkNote}</p>}
        {Object.keys(updates).length > 0 && (
          <p className="text-[12px] text-fg-faint mb-3">
            PR gemerged? De versies hierboven komen uit de laatst gefetchte stand van de
            release-branch — klik <span className="text-fg">Fetch alles</span> om de nieuwe
            versies te zien.
          </p>
        )}

        {!report && busy && (
          <p className="text-[12.5px] text-fg-faint">Versies lezen uit alle projecten…</p>
        )}
        {report && shown.length === 0 && (
          <p className="text-[12.5px] text-fg-faint">
            {filter ? 'Geen resultaten voor deze zoekopdracht.' : 'Geen WordPress-projecten gevonden.'}
          </p>
        )}

        {report && shown.length > 0 && (
          <div className="border border-border rounded-lg overflow-hidden">
            <div className="flex items-center gap-2 px-3 py-1 bg-panel/40 border-b border-border">
              <span className="flex-1" />
              <VersionColumnsHeader />
              <span className="w-[190px] shrink-0" />
            </div>
            {shown.map(p => (
              <div key={p.projectId}
                   className="flex items-center gap-2 px-3 py-2 text-[12.5px] border-b border-border/40 last:border-b-0">
                <span className="text-fg truncate">{p.projectName}</span>
                <span className="font-mono text-[10.5px] text-fg-faint truncate flex-1"
                      title={`Versie gelezen van ${p.ref}`}>
                  ⑂ {p.ref}
                </span>
                <VersionColumns local={p.localVersion} github={p.githubVersion}
                                latest={report.latestVersion} outdated={p.outdated}
                                localBehind={p.localBehind} />
                <span className="w-[190px] shrink-0 flex items-center justify-end gap-2">
                {p.outdated && report.latestVersion && (
                  <>
                    {updates[p.projectId] && (
                      updates[p.projectId].pullRequestUrl
                        ? <ExternalLink href={updates[p.projectId].pullRequestUrl}
                            className="text-[11px] text-accent hover:underline">
                            {coreUpdateLabel(updates[p.projectId].status)}
                          </ExternalLink>
                        : <span className={`text-[11px] ${updates[p.projectId].status === 'error' ? 'text-red' : 'text-fg-faint'}`}
                                title={updates[p.projectId].error}>
                            {coreUpdateLabel(updates[p.projectId].status)}
                          </span>
                    )}
                    <button onClick={() => void updateProject(p.projectId)}
                      disabled={!!updating[p.projectId] || bulkBusy || busy}
                      title={`Branch update/wordpress-${report.latestVersion} met pull request aanmaken vanaf de release-branch`}
                      className="px-2 py-[3px] bg-panel border border-border rounded-md text-[11px] font-medium
                                 text-fg hover:bg-hover disabled:opacity-50 transition-colors">
                      {updating[p.projectId] ? 'Bezig…' : `Update → ${report.latestVersion}`}
                    </button>
                  </>
                )}
                </span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
