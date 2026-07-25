import { useEffect, useState, useCallback } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { WPCoreReport } from '../../bindings/github.com/rdm/sites-tool/internal/services'
import { GlobeIcon, RefreshIcon, CloudDownloadIcon } from './icons'

export default function WordPressPage() {
  const [report, setReport] = useState<WPCoreReport | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [filter, setFilter] = useState('')
  const [fetching, setFetching] = useState(false)
  const [fetchNote, setFetchNote] = useState<string | null>(null)

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

  const outdated = report?.projects.filter(p => p.outdated).length ?? 0

  const q = filter.trim().toLowerCase()
  const shown = (report?.projects ?? []).filter(p =>
    !q || p.projectName.toLowerCase().includes(q) || p.version.toLowerCase().includes(q)
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
              {' · vergeleken met de default branch per project'}
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
        <button onClick={() => void load()} disabled={busy || fetching}
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
            {shown.map(p => (
              <div key={p.projectId}
                   className="flex items-center gap-2 px-3 py-2 text-[12.5px] border-b border-border/40 last:border-b-0">
                <span className="text-fg truncate">{p.projectName}</span>
                <span className="font-mono text-[10.5px] text-fg-faint truncate flex-1"
                      title={`Versie gelezen van ${p.ref}`}>
                  ⑂ {p.ref}
                </span>
                <span className={`font-mono ${p.outdated ? 'text-amber' : 'text-fg-muted'}`}>
                  {p.version}
                </span>
                {p.outdated && report.latestVersion && (
                  <span className="font-mono text-fg-faint">→ {report.latestVersion}</span>
                )}
                {!p.outdated && report.latestVersion && (
                  <span className="text-[11px] text-green">actueel</span>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
