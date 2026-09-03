import { useEffect, useState, useCallback } from 'react'
import VersionColumns, { VersionColumnsHeader } from './VersionColumns'
import ExternalLink from './ExternalLink'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { InventoryItem, BulkApplyResult } from '../../bindings/github.com/rdm/sites-tool/internal/services'
import { PackageIcon, PaletteIcon, RefreshIcon, ChevronIcon, CloudDownloadIcon } from './icons'
import { bevestig } from '../lib/bevestig'

interface Props {
  kind: 'plugins' | 'themes'
}

export default function InventoryPage({ kind }: Props) {
  const [items, setItems] = useState<InventoryItem[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [filter, setFilter] = useState('')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [fetching, setFetching] = useState(false)
  const [fetchNote, setFetchNote] = useState<string | null>(null)

  // Bulk bijwerken vanuit de referentie-installatie: bulkTarget is de slug die
  // net geselecteerd wordt, bulkSelection de gekozen projecten daarbij.
  const [bulkTarget, setBulkTarget] = useState<string | null>(null)
  const [bulkSelection, setBulkSelection] = useState<Set<string>>(new Set())
  const [bulkBusy, setBulkBusy] = useState(false)
  const [bulkResult, setBulkResult] = useState<BulkApplyResult | null>(null)
  const [bulkError, setBulkError] = useState<string | null>(null)
  // Mergen gebeurt per project, dus ook de "bezig"- en uitkomststaat is per
  // project: één projectId dat nu merget, en per project de uitkomst.
  const [mergeBezig, setMergeBezig] = useState<string | null>(null)
  const [mergeUitkomst, setMergeUitkomst] = useState<Record<string, string>>({})

  const title = kind === 'plugins' ? 'Plugins' : "Thema's"
  const Icon = kind === 'plugins' ? PackageIcon : PaletteIcon

  const load = useCallback(async () => {
    setBusy(true); setError(null)
    try {
      const result = kind === 'plugins'
        ? await Services.InventoryService.Plugins()
        : await Services.InventoryService.Themes()
      setItems(result ?? [])
    } catch (e) {
      setError(String(e))
    } finally {
      setBusy(false)
    }
  }, [kind])

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

  const toggle = (slug: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      next.has(slug) ? next.delete(slug) : next.add(slug)
      return next
    })
  }

  const bulkStarten = (it: InventoryItem) => {
    setBulkTarget(it.slug)
    setBulkResult(null); setBulkError(null)
    setExpanded(prev => new Set(prev).add(it.slug))
    // Voorselectie: de projecten die nu al als verouderd gelden. De rest kan
    // erbij aangevinkt worden, dat blokkeert niets.
    setBulkSelection(new Set(it.projects.filter(p => p.outdated).map(p => p.projectId)))
  }

  const bulkAnnuleren = () => {
    setBulkTarget(null); setBulkSelection(new Set()); setBulkResult(null); setBulkError(null)
    setMergeUitkomst({}); setMergeBezig(null)
  }

  // Alleen een verouderd project kan aangevinkt worden: staat de
  // referentie-versie er al, dan valt er niets bij te werken en zou een
  // "update" een commit zonder inhoudelijke wijziging opleveren. Het vinkje is
  // in de UI al uitgeschakeld; deze controle houdt de staat kloppend, zodat
  // hij ook na een verversing (waarin een project actueel kan zijn geworden)
  // niet stil in de selectie blijft hangen.
  const bulkToggleProject = (it: InventoryItem, projectId: string) => {
    if (!it.projects.some(p => p.projectId === projectId && p.outdated)) return
    setBulkSelection(prev => {
      const next = new Set(prev)
      next.has(projectId) ? next.delete(projectId) : next.add(projectId)
      return next
    })
  }

  // mergen voert de merge op GitHub uit. Onomkeerbaar richting de default
  // branch, dus altijd eerst bevestigen — de knop alleen is niet genoeg.
  const mergen = async (r: BulkApplyResult['results'][number]) => {
    const nummer = r.pullRequestNumber
    if (!nummer) return
    const ok = await bevestig(
      `Pull request van ${r.projectName} mergen`,
      `De pull request wordt op GitHub gemerged naar de default branch van ${r.projectName}.\n\n` +
      `Dit is niet met één klik terug te draaien.`,
    )
    if (!ok) return
    setMergeBezig(r.projectId)
    try {
      const res = await Services.PluginService.MergePluginPullRequest(r.projectId, nummer)
      setMergeUitkomst(h => ({
        ...h,
        [r.projectId]: res.merged ? `✓ gemerged${res.sha ? ` (${res.sha.slice(0, 7)})` : ''}` : (res.message || 'niet gemerged'),
      }))
    } catch (e) {
      setMergeUitkomst(h => ({ ...h, [r.projectId]: `✗ ${String(e)}` }))
    } finally {
      setMergeBezig(null)
    }
  }

  const bulkToepassen = async (it: InventoryItem) => {
    const ids = Array.from(bulkSelection)
    if (ids.length === 0) return
    const namen = it.projects.filter(p => bulkSelection.has(p.projectId)).map(p => p.projectName)
    const ok = await bevestig(
      `${it.slug} bijwerken vanuit de referentie-installatie`,
      `${ids.length} project(en) worden bijgewerkt naar de versie uit de referentie-installatie` +
      (it.latestVersion ? ` (${it.latestVersion})` : '') + `.\n\n` +
      `Per project: openstaand werk gaat automatisch in een stash (je krijgt te zien welke), ` +
      `de update komt op een eigen branch chore/plugin-${it.slug}-${it.latestVersion || '<versie>'} ` +
      `afgetakt van de default branch, en die branch wordt gepusht met een pull request erop.\n\n` +
      `${namen.join('\n')}`,
    )
    if (!ok) return

    setBulkBusy(true); setBulkError(null); setBulkResult(null)
    try {
      const res = await Services.PluginService.ApplyPluginToProjects(it.slug, ids)
      setBulkResult(res)
      await load()
    } catch (e) {
      setBulkError(String(e))
    } finally {
      setBulkBusy(false)
    }
  }

  const q = filter.trim().toLowerCase()
  const shown = (items ?? []).filter(it =>
    !q ||
    it.slug.toLowerCase().includes(q) ||
    it.projects.some(p => p.projectName.toLowerCase().includes(q))
  )
  const outdatedTotal = (items ?? []).reduce((n, it) => n + it.outdatedCount, 0)

  return (
    <div className="flex-1 flex flex-col min-h-0 overflow-hidden bg-bg">
      {/* ── toolbar ── */}
      <div className="h-14 px-[22px] bg-panel border-b border-border shrink-0 flex items-center gap-3">
        <span className="w-8 h-8 flex items-center justify-center rounded-lg bg-hover text-fg-muted shrink-0">
          <Icon size={16} />
        </span>
        <div className="flex-1 min-w-0">
          <h2 className="text-[15px] font-bold tracking-[-.01em] text-fg leading-tight truncate">{title}</h2>
          {items && (
            <p className="text-[11px] text-fg-faint leading-tight truncate">
              {items.length} {kind === 'plugins' ? 'plugins' : "thema's"}
              {outdatedTotal > 0 && <> · <span className="text-amber">{outdatedTotal} verouderd</span></>}
              {' · vergeleken met de default branch per project'}
            </p>
          )}
        </div>
        <input
          type="search"
          placeholder={kind === 'plugins' ? 'Zoek plugin of project…' : 'Zoek thema of project…'}
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

        {!items && busy && (
          <p className="text-[12.5px] text-fg-faint">Verzamelen uit alle projecten…</p>
        )}
        {items && shown.length === 0 && (
          <p className="text-[12.5px] text-fg-faint">
            {filter ? 'Geen resultaten voor dit filter.' : `Geen ${title.toLowerCase()} gevonden in de projecten.`}
          </p>
        )}

        <div className="space-y-1">
          {shown.map(it => {
            const open = expanded.has(it.slug)
            return (
              <div key={it.slug} className="border border-border rounded-lg overflow-hidden">
                {/* item row */}
                <div className="w-full flex items-center gap-2.5 px-3 py-2 hover:bg-hover transition-colors">
                  <button
                    onClick={() => toggle(it.slug)}
                    className="flex items-center gap-2.5 min-w-0 text-left"
                  >
                    <span className="text-fg-faint shrink-0"><ChevronIcon size={13} open={open} /></span>
                    <span className="font-mono text-[12.5px] text-fg truncate">{it.slug}</span>
                  </button>

                  {it.outdatedCount > 0 && (
                    <span className="text-[10.5px] font-semibold text-amber bg-amber/10 border border-amber/30
                                     rounded-full px-2 py-px shrink-0">
                      {it.outdatedCount} verouderd
                    </span>
                  )}

                  {/* Alleen aanbieden als er iets te doen is: staat de
                      referentie-versie al in élk project, dan is deze knop een
                      lijst met louter uitgeschakelde vinkjes. Een bewuste
                      her-installatie kan nog per project via de Plugins-tab. */}
                  {kind === 'plugins' && it.source === 'reference' && it.outdatedCount > 0 && (
                    <button
                      onClick={() => bulkStarten(it)}
                      title="Deze versie komt uit de referentie-installatie; werk 'm bij in de gekozen projecten"
                      className="text-[10.5px] font-semibold text-accent bg-accent/10 border border-accent/30
                                 rounded-full px-2 py-px shrink-0 hover:bg-accent/20 transition-colors"
                    >
                      ↑ bijwerken vanuit referentie
                    </button>
                  )}

                  <span className="ml-auto text-[11px] text-fg-faint shrink-0">
                    {it.projects.length} project{it.projects.length !== 1 ? 'en' : ''}
                  </span>
                  <span className="text-[12px] font-mono shrink-0 w-[90px] text-right flex items-center justify-end gap-1">
                    {it.source === 'reference' && (
                      <span title="Laatste versie komt uit de referentie-installatie, niet wp.org"
                            className="text-[9px] font-bold px-1 py-px rounded bg-accent/15 text-accent">REF</span>
                    )}
                    {it.latestVersion
                      ? <span className="text-fg">{it.latestVersion}</span>
                      : <span className="text-fg-faint" title="Niet op wp.org — handmatig bijhouden">—</span>}
                  </span>
                </div>

                {/* per-project versions */}
                {open && (
                  <ul className="border-t border-border bg-panel/40">
                    <li className="flex items-center gap-2 px-3 py-1 pl-9 border-b border-border/40">
                      {bulkTarget === it.slug && <span className="w-4 shrink-0" />}
                      <span className="flex-1" />
                      <VersionColumnsHeader />
                    </li>
                    {it.projects.map(p => (
                      <li key={p.projectId + p.githubVersion + p.localVersion}
                          className="flex items-center gap-2 px-3 py-1.5 pl-9 text-[12px] border-b border-border/40 last:border-b-0">
                        {bulkTarget === it.slug && (
                          // Een disabled input krijgt geen pointer-events, dus de
                          // uitleg hangt aan de span eromheen.
                          <span className="shrink-0 flex items-center"
                                title={p.outdated
                                  ? undefined
                                  : `Al op ${it.latestVersion || 'de referentie-versie'} — niets bij te werken`}>
                            <input type="checkbox" checked={bulkSelection.has(p.projectId)}
                                   disabled={!p.outdated}
                                   onChange={() => bulkToggleProject(it, p.projectId)}
                                   className="accent-accent disabled:opacity-30 disabled:cursor-not-allowed" />
                          </span>
                        )}
                        <span className="text-fg truncate">{p.projectName}</span>
                        <span className="font-mono text-[10.5px] text-fg-faint truncate flex-1"
                              title={`GitHub-versie gelezen van ${p.ref}`}>
                          ⑂ {p.ref}
                        </span>
                        <VersionColumns local={p.localVersion} github={p.githubVersion}
                                        latest={it.latestVersion} outdated={p.outdated}
                                        localBehind={p.localBehind} />
                      </li>
                    ))}

                    {bulkTarget === it.slug && (
                      <li className="px-3 py-2 pl-9 border-t border-border/40 bg-panel">
                        <div className="flex items-center gap-2">
                          <button
                            onClick={() => void bulkToepassen(it)}
                            disabled={bulkBusy || bulkSelection.size === 0}
                            className="bg-accent text-white text-[11.5px] font-semibold px-3 py-1 rounded-lg
                                       hover:brightness-110 disabled:opacity-50 transition"
                          >
                            {bulkBusy ? 'Bezig…' : `Bijwerken in ${bulkSelection.size} project(en)`}
                          </button>
                          <button onClick={bulkAnnuleren} className="text-[11.5px] text-fg-muted hover:text-fg transition-colors">
                            Annuleren
                          </button>
                        </div>
                        {bulkError && (
                          <p className="mt-1.5 bg-red-soft text-red px-2.5 py-1.5 rounded-lg text-[11px]">{bulkError}</p>
                        )}
                        {bulkResult && (
                          <div className="mt-1.5 text-[11px] space-y-0.5">
                            {bulkResult.results.map(r => {
                              const kleur = r.status === 'updated' ? 'text-green'
                                : r.status === 'unchanged' ? 'text-fg-muted' : 'text-red'
                              const tekst = r.status === 'updated'
                                ? `✓ ${r.projectName}: ${r.from || 'nieuw'} → ${r.to} (op ${r.branch})`
                                : r.status === 'unchanged'
                                  ? `= ${r.projectName}: stond er al in, niets gewijzigd`
                                  : `✗ ${r.projectName}: ${r.error}`
                              return (
                                <div key={r.projectId}>
                                  <p className={kleur}>{tekst}</p>
                                  {/* Een geparkeerde wijziging moet je terug kunnen vinden. */}
                                  {r.stash && (
                                    <p className="pl-3 text-amber">
                                      ⇣ werk gestasht: <span className="font-mono">{r.stash}</span>
                                    </p>
                                  )}
                                  {r.pullRequestUrl && (
                                    <p className="pl-3 flex items-center gap-2">
                                      <ExternalLink href={r.pullRequestUrl}
                                        className="text-accent hover:underline cursor-pointer">↗ pull request</ExternalLink>
                                      {/* Alleen tonen als dit token op deze repo mag mergen. */}
                                      {r.canMerge && !!r.pullRequestNumber && !mergeUitkomst[r.projectId] && (
                                        <button
                                          onClick={() => void mergen(r)}
                                          disabled={mergeBezig === r.projectId}
                                          className="text-[10.5px] font-semibold text-green bg-green-soft border border-green/30
                                                     rounded-full px-2 py-px hover:brightness-95 disabled:opacity-50 transition"
                                        >
                                          {mergeBezig === r.projectId ? 'Mergen…' : 'Mergen'}
                                        </button>
                                      )}
                                      {mergeUitkomst[r.projectId] && (
                                        <span className={mergeUitkomst[r.projectId].startsWith('✓') ? 'text-green' : 'text-red'}>
                                          {mergeUitkomst[r.projectId]}
                                        </span>
                                      )}
                                    </p>
                                  )}
                                  {r.pullRequestError && (
                                    <p className="pl-3 text-amber">
                                      geen PR aangemaakt: {r.pullRequestError} (de commit staat er wel)
                                    </p>
                                  )}
                                </div>
                              )
                            })}
                          </div>
                        )}
                      </li>
                    )}
                  </ul>
                )}
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}
