import { useCallback, useEffect, useMemo, useState } from 'react'
import { Events } from '@wailsio/runtime'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { OrgSyncResult, OrgSyncRepo, OrgSyncLocalOnly, OrgCloneResult } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import Foutvak from './Foutvak'
import ExternalLink from './ExternalLink'
import Tooltip from './Tooltip'
import { CloudDownloadIcon, CloseIcon, FolderIcon } from './icons'

interface Props { onClose: () => void }

interface OrgSyncProgress {
  phase: string
  repo?: string
  done: number
  total: number
}

interface OrgCloneProgress {
  phase: string
  repo?: string
  done: number
  total: number
}

// parseEvent pelt de Wails-eventpayload af: die komt als object binnen, maar bij
// sommige transportvormen als JSON-string.
function parseEvent<T>(data: unknown): T | null {
  if (typeof data === 'string') {
    try { return JSON.parse(data) as T } catch { return null }
  }
  return (data as T | null) ?? null
}

type Filter = 'wp-missing' | 'wp-all' | 'all' | 'local-only'

const NOOIT_GESYNCHRONISEERD = 'nog nooit gesynchroniseerd'
const WARNING_LIMIT = 10

// leesbaar pelt de JSON-envelop van een Wails-fout af, net als Foutvak dat doet
// (dupliceren i.p.v. exporteren: Foutvak's helper is niet exposed).
function leesbareFout(fout: unknown): string {
  const tekst = fout instanceof Error ? fout.message : String(fout)
  const trimmed = tekst.trim()
  if (trimmed.startsWith('{')) {
    try {
      const j = JSON.parse(trimmed)
      if (j && typeof j.message === 'string') return j.message
    } catch {
      // geen JSON: dan is het al een gewone boodschap
    }
  }
  return trimmed
}

function isNooitGesynchroniseerd(fout: unknown): boolean {
  return leesbareFout(fout).toLowerCase().includes(NOOIT_GESYNCHRONISEERD)
}

// deployType -> badge klasse. wordpress* krijgt de accentkleur (dit is de
// feature waar het om draait), lege waarde ("geen deploy_conf.json") is
// faint, de rest is neutraal.
function deployTypeBadge(deployType: string, hasDeployConf: boolean): { label: string; klasse: string } {
  if (!hasDeployConf) return { label: 'geen deploy_conf', klasse: 'bg-panel-2 text-fg-faint' }
  if (deployType.toLowerCase().startsWith('wordpress')) return { label: deployType, klasse: 'bg-accent-soft text-accent' }
  if (!deployType) return { label: '—', klasse: 'bg-panel-2 text-fg-faint' }
  return { label: deployType, klasse: 'bg-panel-2 text-fg-muted' }
}

function FilterBtn({ active, onClick, children }: {
  active: boolean; onClick: () => void; children: React.ReactNode
}) {
  return (
    <button
      onClick={onClick}
      className={`px-2.5 py-1 rounded-[6px] text-[11.5px] font-semibold transition-colors
        ${active ? 'bg-panel text-fg shadow-sm' : 'text-fg-muted hover:text-fg'}`}
    >
      {children}
    </button>
  )
}

function StatCard({ label, value, accent = false }: { label: string; value: number; accent?: boolean }) {
  return (
    <div className={`flex-1 min-w-[120px] rounded-xl border p-3.5 ${
      accent ? 'bg-accent-soft border-accent/40' : 'bg-panel border-border'
    }`}>
      <p className={`text-[22px] font-bold tracking-[-.01em] ${accent ? 'text-accent' : 'text-fg'}`}>{value}</p>
      <p className={`text-[11.5px] mt-0.5 ${accent ? 'text-accent' : 'text-fg-muted'}`}>{label}</p>
    </div>
  )
}

// CloneSamenvatting toont per clone-run wat er gelukt is en, belangrijker, wat
// niet: een overgeslagen of mislukte repo moet je kunnen lezen zonder in de
// tabel te gaan zoeken.
function CloneSamenvatting({ res, onSluit }: { res: OrgCloneResult; onSluit: () => void }) {
  const problemen = (res.outcomes ?? []).filter(o => o.status !== 'cloned')
  const kleur = res.failed > 0
    ? 'bg-amber-soft border-amber/40 text-amber'
    : 'bg-panel border-border text-fg-muted'
  return (
    <div className={`rounded-xl border p-3.5 ${kleur}`}>
      <div className="flex items-start gap-3">
        <p className="flex-1 text-[12.5px] font-semibold">
          {res.cloned} gecloned
          {res.skipped > 0 && `, ${res.skipped} overgeslagen`}
          {res.failed > 0 && `, ${res.failed} mislukt`}
          {res.root && <span className="font-normal"> — in {res.root}</span>}
        </p>
        <button onClick={onSluit} className="shrink-0 opacity-60 hover:opacity-100 transition-opacity" title="Sluiten">
          <CloseIcon size={13} />
        </button>
      </div>
      {problemen.length > 0 && (
        <ul className="mt-2 space-y-1">
          {problemen.map((o, i) => (
            <li key={`${o.repo}-${i}`} className="text-[11px] font-mono">
              {o.repo || 'run'}: {o.message}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

export default function OrgSyncPage({ onClose }: Props) {
  const [result, setResult] = useState<OrgSyncResult | null>(null)
  const [org, setOrg] = useState('')
  const [busy, setBusy] = useState(false)
  const [leegState, setLeegState] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [progress, setProgress] = useState<OrgSyncProgress | null>(null)
  const [filter, setFilter] = useState<Filter>('wp-missing')
  const [zoek, setZoek] = useState('')
  const [selectie, setSelectie] = useState<Set<string>>(new Set())
  const [cloneBezig, setCloneBezig] = useState(false)
  const [cloneProgress, setCloneProgress] = useState<OrgCloneProgress | null>(null)
  const [cloneResult, setCloneResult] = useState<OrgCloneResult | null>(null)

  const laadLaatste = useCallback(async () => {
    setError(null)
    try {
      const r = await Services.OrgSyncService.Last()
      setResult(r)
      setLeegState(false)
    } catch (e) {
      if (isNooitGesynchroniseerd(e)) {
        setLeegState(true)
      } else {
        setError(leesbareFout(e))
      }
    }
  }, [])

  useEffect(() => {
    Services.OrgSyncService.Org().then(setOrg).catch(() => {})
    laadLaatste()
  }, [laadLaatste])

  const sync = async (force: boolean) => {
    setBusy(true); setError(null); setProgress(null)
    const stopListening = Events.On('orgsync:progress', ev => {
      const parsed = parseEvent<OrgSyncProgress>(ev.data)
      if (parsed) setProgress(parsed)
    })
    try {
      const r = await Services.OrgSyncService.Sync(force)
      setResult(r)
      setLeegState(false)
      // De repolijst is vernieuwd, dus een selectie van vóór de sync kan naar
      // repo's wijzen die er niet meer zijn.
      setSelectie(new Set())
    } catch (e) {
      setError(leesbareFout(e))
    } finally {
      stopListening()
      setBusy(false)
      setProgress(null)
    }
  }

  const clone = async (namen: string[]) => {
    if (namen.length === 0 || cloneBezig) return
    setCloneBezig(true); setError(null); setCloneProgress(null); setCloneResult(null)
    const stopListening = Events.On('orgclone:progress', ev => {
      const parsed = parseEvent<OrgCloneProgress>(ev.data)
      if (parsed) setCloneProgress(parsed)
    })
    try {
      const res = await Services.OrgSyncService.Clone(namen)
      setCloneResult(res)
      // Alleen de gevraagde repo's uit de selectie halen: wat er tijdens de run
      // bij is aangevinkt blijft staan.
      setSelectie(prev => {
        const next = new Set(prev)
        namen.forEach(n => next.delete(n))
        return next
      })
      // Herlaad zodat de kolom "Lokaal" het nieuwe pad toont; Last() rematcht
      // lokaal en kost geen netwerkverkeer.
      await laadLaatste()
    } catch (e) {
      setError(leesbareFout(e))
    } finally {
      stopListening()
      setCloneBezig(false)
      setCloneProgress(null)
    }
  }

  const repos = useMemo<OrgSyncRepo[]>(() => result?.repos ?? [], [result])
  const localOnly = useMemo<OrgSyncLocalOnly[]>(() => result?.localOnly ?? [], [result])

  const gefilterdeRepos = useMemo(() => {
    let lijst = repos
    // !archived hoort erbij: de kaart "WordPress zonder lokale checkout" telt in
    // de backend exclusief gearchiveerde repos, dus de default-lijst moet
    // dezelfde definitie gebruiken — anders tonen kaart en tabel andere getallen.
    if (filter === 'wp-missing') lijst = lijst.filter(r => r.isWordPress && !r.localPath && !r.archived)
    else if (filter === 'wp-all') lijst = lijst.filter(r => r.isWordPress)
    // 'all' en 'local-only' filteren hier niet verder — 'local-only' gebruikt localOnly, niet repos.
    const q = zoek.trim().toLowerCase()
    if (q) lijst = lijst.filter(r => r.name.toLowerCase().includes(q) || r.fullName.toLowerCase().includes(q))
    return lijst
  }, [repos, filter, zoek])

  const gefilterdeLocalOnly = useMemo(() => {
    const q = zoek.trim().toLowerCase()
    if (!q) return localOnly
    return localOnly.filter(l => l.displayName.toLowerCase().includes(q) || l.path.toLowerCase().includes(q))
  }, [localOnly, zoek])

  // Alleen repo's zonder lokale checkout zijn te clonen; de rest heeft de map al.
  const teClonen = useMemo(() => gefilterdeRepos.filter(r => !r.localPath), [gefilterdeRepos])
  const zichtbaarGeselecteerd = useMemo(
    () => teClonen.filter(r => selectie.has(r.name)).length,
    [teClonen, selectie],
  )
  const allesGeselecteerd = teClonen.length > 0 && zichtbaarGeselecteerd === teClonen.length

  const wisselSelectie = (naam: string) => {
    setSelectie(prev => {
      const next = new Set(prev)
      if (next.has(naam)) next.delete(naam)
      else next.add(naam)
      return next
    })
  }

  const wisselAlles = () => {
    setSelectie(prev => {
      const next = new Set(prev)
      if (allesGeselecteerd) teClonen.forEach(r => next.delete(r.name))
      else teClonen.forEach(r => next.add(r.name))
      return next
    })
  }

  const warnings = result?.warnings ?? []
  const zichtbareWarnings = warnings.slice(0, WARNING_LIMIT)
  const restWarnings = warnings.length - zichtbareWarnings.length

  const laatstOpgehaald = result?.fetchedAt ? new Date(result.fetchedAt).toLocaleString('nl-NL') : null

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-hidden bg-bg">
      {/* ── toolbar ── */}
      <div className="h-14 px-[22px] bg-panel border-b border-border shrink-0 flex items-center gap-3">
        <span className="w-8 h-8 flex items-center justify-center rounded-lg bg-hover text-fg-muted shrink-0">
          <CloudDownloadIcon size={16} />
        </span>
        <div className="flex-1 min-w-0">
          <h2 className="text-[15px] font-bold tracking-[-.01em] text-fg leading-tight truncate">
            Org-sync{org ? ` — ${org}` : ''}
          </h2>
          <p className="text-[11px] text-fg-faint leading-tight truncate">
            {laatstOpgehaald ? `laatst opgehaald ${laatstOpgehaald}` : 'nog niet opgehaald'}
          </p>
        </div>
        {selectie.size > 0 && (
          <Tooltip label={`Cloont de geselecteerde repo's naar de ingestelde projectmap (${selectie.size} stuks, één voor één).`}>
            <button onClick={() => clone([...selectie])} disabled={busy || cloneBezig}
              className="px-3 py-1.5 text-[12.5px] font-semibold text-fg border border-border rounded-lg
                         hover:bg-hover disabled:opacity-50 transition-colors shrink-0 flex items-center gap-1.5">
              {cloneBezig
                ? <span className="animate-spin inline-block">↻</span>
                : <FolderIcon size={13} />}
              Clone geselecteerde ({selectie.size})
            </button>
          </Tooltip>
        )}
        <Tooltip label="Leest alle ±590 repo's van de organisatie opnieuw — dit duurt een paar minuten.">
          <button onClick={() => sync(true)} disabled={busy}
            className="px-2.5 py-1.5 text-[11.5px] font-medium text-fg-muted border border-border rounded-lg
                       hover:bg-hover hover:text-fg disabled:opacity-50 transition-colors shrink-0">
            Alles opnieuw ophalen
          </button>
        </Tooltip>
        <button onClick={() => sync(false)} disabled={busy}
          className="px-3 py-1.5 bg-accent hover:bg-accent-2 text-white text-[12.5px] font-semibold
                     rounded-lg disabled:opacity-50 transition-colors shrink-0 flex items-center gap-1.5">
          {busy
            ? <span className="animate-spin inline-block">↻</span>
            : <CloudDownloadIcon size={13} />}
          {busy ? 'Bezig…' : 'Synchroniseren'}
        </button>
        <button onClick={onClose}
          className="w-7 h-7 flex items-center justify-center rounded-md text-fg-muted
                     hover:text-fg hover:bg-hover transition-colors shrink-0"
          title="Sluiten">
          <CloseIcon size={15} />
        </button>
      </div>

      {busy && (
        <div className="px-6 py-2 bg-panel-2 border-b border-border shrink-0 text-[11.5px] text-fg-muted flex items-center gap-2">
          <span className="animate-spin inline-block">↻</span>
          {progress
            ? (progress.phase === 'bezig' && progress.total > 0
                ? `${progress.done}/${progress.total} repo's verwerkt${progress.repo ? ` — ${progress.repo}` : ''}`
                : progress.phase === 'lijst'
                  ? 'Repolijst ophalen…'
                  : 'Bezig…')
            : 'Bezig…'}
          <span className="text-fg-faint">· de eerste keer kan dit een paar minuten duren</span>
        </div>
      )}

      {cloneBezig && (
        <div className="px-6 py-2 bg-panel-2 border-b border-border shrink-0 text-[11.5px] text-fg-muted flex items-center gap-2">
          <span className="animate-spin inline-block">↻</span>
          {cloneProgress && cloneProgress.total > 0
            ? `Clonen: ${cloneProgress.done}/${cloneProgress.total}${cloneProgress.repo ? ` — ${cloneProgress.repo}` : ''}`
            : 'Clonen…'}
          <span className="text-fg-faint">· een grote repo kan minuten duren</span>
        </div>
      )}

      <div className="flex-1 overflow-y-auto px-6 py-5 space-y-5">
        {error && <Foutvak fout={error} />}

        {cloneResult && <CloneSamenvatting res={cloneResult} onSluit={() => setCloneResult(null)} />}

        {leegState && !result && !busy && (
          <div className="flex-1 flex flex-col items-center justify-center gap-3 py-16 text-center">
            <p className="text-[15px] font-semibold text-fg">Nog nooit gesynchroniseerd</p>
            <p className="text-[13px] text-fg-muted max-w-[420px]">
              Haal de repolijst van de organisatie op om te zien welke WordPress-sites nog geen lokale checkout hebben.
              De eerste synchronisatie leest alle ±590 repo&apos;s en kan een paar minuten duren.
            </p>
            <button onClick={() => sync(false)} disabled={busy}
              className="mt-1 px-4 py-2 bg-accent hover:bg-accent-2 text-white text-[13px] font-semibold
                         rounded-lg disabled:opacity-50 transition-colors flex items-center gap-1.5">
              {busy ? <span className="animate-spin inline-block">↻</span> : <CloudDownloadIcon size={14} />}
              {busy ? 'Bezig…' : 'Synchroniseren'}
            </button>
          </div>
        )}

        {result && (
          <>
            {/* ── samenvattingskaarten ── */}
            <div className="flex flex-wrap gap-2.5">
              <StatCard label="Repo's totaal" value={result.totals.repos} />
              <StatCard label="WordPress" value={result.totals.wordpress} />
              <StatCard label="WordPress met lokale checkout" value={result.totals.wordpressLocal} />
              <StatCard label="WordPress zonder lokale checkout" value={result.totals.wordpressMissing} accent />
              <StatCard label="Archived" value={result.totals.archived} />
            </div>

            {/* ── warnings ── */}
            {warnings.length > 0 && (
              <div className="space-y-1.5">
                {zichtbareWarnings.map((w, i) => (
                  <div key={i} className="bg-amber-soft text-amber px-3 py-1.5 rounded-lg text-[11px]">{w}</div>
                ))}
                {restWarnings > 0 && (
                  <p className="text-[11px] text-fg-faint">…en {restWarnings} meer</p>
                )}
              </div>
            )}

            {/* ── filters ── */}
            <div className="flex items-center gap-2 flex-wrap">
              <div className="flex items-center gap-0.5 bg-bg border border-border rounded-lg p-0.5">
                <FilterBtn active={filter === 'wp-missing'} onClick={() => setFilter('wp-missing')}>
                  WordPress zonder lokaal
                </FilterBtn>
                <FilterBtn active={filter === 'wp-all'} onClick={() => setFilter('wp-all')}>
                  Alle WordPress
                </FilterBtn>
                <FilterBtn active={filter === 'all'} onClick={() => setFilter('all')}>
                  Alles
                </FilterBtn>
                <FilterBtn active={filter === 'local-only'} onClick={() => setFilter('local-only')}>
                  Lokaal zonder org-repo
                </FilterBtn>
              </div>
              <input
                type="search"
                placeholder="Zoek op naam…"
                value={zoek}
                onChange={e => setZoek(e.target.value)}
                className="w-[220px] bg-bg text-[12.5px] text-fg placeholder-fg-faint rounded-lg px-3 py-[6px]
                           outline-none border border-border focus:border-accent focus:ring-1 focus:ring-accent/30"
              />
            </div>

            {/* ── tabel ── */}
            {filter === 'local-only' ? (
              gefilterdeLocalOnly.length === 0 ? (
                <p className="text-[12.5px] text-fg-faint">Geen lokale checkouts zonder org-repo gevonden.</p>
              ) : (
                <div className="bg-panel border border-border rounded-xl overflow-hidden">
                  <table className="w-full text-[12.5px]">
                    <thead>
                      <tr className="border-b border-border text-left text-fg-faint text-[11px]">
                        <th className="px-3 py-2 font-medium">Naam</th>
                        <th className="px-3 py-2 font-medium">Pad</th>
                        <th className="px-3 py-2 font-medium">Remote</th>
                      </tr>
                    </thead>
                    <tbody>
                      {gefilterdeLocalOnly.map(l => (
                        <tr key={l.projectId} className="border-b border-border/50 last:border-0">
                          <td className="px-3 py-2 text-fg">{l.displayName}</td>
                          <td className="px-3 py-2 font-mono text-fg-muted truncate max-w-[320px]" title={l.path}>{l.path}</td>
                          <td className="px-3 py-2 font-mono text-fg-faint truncate max-w-[320px]" title={l.remote}>{l.remote}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )
            ) : gefilterdeRepos.length === 0 ? (
              <p className="text-[12.5px] text-fg-faint">Geen repo&apos;s gevonden voor dit filter.</p>
            ) : (
              <div className="bg-panel border border-border rounded-xl overflow-hidden">
                <table className="w-full text-[12.5px]">
                  <thead>
                    <tr className="border-b border-border text-left text-fg-faint text-[11px]">
                      <th className="pl-3 pr-1 py-2 font-medium w-8">
                        <input
                          type="checkbox"
                          checked={allesGeselecteerd}
                          onChange={wisselAlles}
                          disabled={teClonen.length === 0 || cloneBezig}
                          className="accent-accent align-middle disabled:opacity-40"
                          title={teClonen.length === 0
                            ? 'Niets te clonen in dit filter'
                            : `Selecteer alle ${teClonen.length} repo's zonder lokale checkout`}
                        />
                      </th>
                      <th className="px-3 py-2 font-medium">Repo</th>
                      <th className="px-3 py-2 font-medium">Deploy type</th>
                      <th className="px-3 py-2 font-medium">Vlaggen</th>
                      <th className="px-3 py-2 font-medium">Lokaal</th>
                    </tr>
                  </thead>
                  <tbody>
                    {gefilterdeRepos.map(r => {
                      const badge = deployTypeBadge(r.deployType, r.hasDeployConf)
                      return (
                        <tr key={r.fullName} className="border-b border-border/50 last:border-0">
                          <td className="pl-3 pr-1 py-2">
                            {!r.localPath && (
                              <input
                                type="checkbox"
                                checked={selectie.has(r.name)}
                                onChange={() => wisselSelectie(r.name)}
                                disabled={cloneBezig}
                                className="accent-accent align-middle disabled:opacity-40"
                              />
                            )}
                          </td>
                          <td className="px-3 py-2">
                            <ExternalLink href={r.htmlUrl} className="text-accent hover:text-accent-2 transition-colors">
                              {r.name}
                            </ExternalLink>
                          </td>
                          <td className="px-3 py-2">
                            <span className={`px-2 py-[3px] rounded-[5px] text-[11px] font-medium ${badge.klasse}`}>
                              {badge.label}
                            </span>
                          </td>
                          <td className="px-3 py-2 text-fg-faint text-[11px]">
                            {r.archived && <span className="mr-2">archived</span>}
                            {r.fork && <span>fork</span>}
                          </td>
                          <td className="px-3 py-2 font-mono text-fg-muted truncate max-w-[280px]" title={r.localPath || undefined}>
                            {r.localPath || (
                              <button onClick={() => clone([r.name])} disabled={busy || cloneBezig}
                                className="font-sans text-[11px] font-semibold text-accent hover:text-accent-2
                                           disabled:opacity-40 disabled:hover:text-accent transition-colors">
                                Clone
                              </button>
                            )}
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
}
