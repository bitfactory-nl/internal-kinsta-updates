import { useState, useEffect, useCallback } from 'react'
import { Events } from '@wailsio/runtime'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { SiteDetails } from '../../bindings/github.com/rdm/sites-tool/internal/adapters/kinsta/models'
import type { DBProbe, DBCloneRequest, DBCloneResult } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import Foutvak from './Foutvak'
import { bevestig } from '../lib/bevestig'

interface Props { projectId: string }

interface DBCloneProgress {
  phase: string
  detail: string
  bytes?: number
  total?: number
}

const FASE_LABEL: Record<string, string> = {
  backup: 'Backup van de huidige lokale database',
  export: 'Dump maken op de server',
  download: 'Dump ophalen',
  import: 'Lokaal importeren',
  'multisite-fix': 'Multisite-domeinen bijwerken (bèta)',
  verify: 'Resultaat controleren',
  done: 'Klaar',
  error: 'Mislukt',
}

function foutTekst(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

function bytesLeesbaar(n: number): string {
  if (!n) return '0 B'
  const eenheden = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), eenheden.length - 1)
  return `${(n / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1)} ${eenheden[i]}`
}

export default function DatabaseTab({ projectId }: Props) {
  const [site, setSite] = useState<SiteDetails | null>(null)
  const [envId, setEnvId] = useState('')
  const [probe, setProbe] = useState<DBProbe | null>(null)
  const [dbApp, setDbApp] = useState('Sequel Ace')

  const [prodSiteURL, setProdSiteURL] = useState('')
  const [localURL, setLocalURL] = useState('')
  const [localDBName, setLocalDBName] = useState('')
  const [localDBHost, setLocalDBHost] = useState('')
  const [multisite, setMultisite] = useState(false)
  const [multisiteUitEnv, setMultisiteUitEnv] = useState(false)
  const [tablePrefix, setTablePrefix] = useState('wp_')
  const [prodNetworkDomain, setProdNetworkDomain] = useState('')
  const [localNetworkDomain, setLocalNetworkDomain] = useState('')

  const [probeBezig, setProbeBezig] = useState(false)
  const [kloonBezig, setKloonBezig] = useState(false)
  const [progress, setProgress] = useState<DBCloneProgress | null>(null)
  const [result, setResult] = useState<DBCloneResult | null>(null)
  const [fout, setFout] = useState<string | null>(null)

  useEffect(() => {
    setSite(null); setProbe(null); setResult(null); setFout(null); setEnvId(''); setProgress(null)
    setMultisiteUitEnv(false)

    Services.KinstaService.GetLinkedSiteID(projectId)
      .then(id => (id ? Services.KinstaService.GetSiteDetails(id).then(setSite) : undefined))
      .catch(e => setFout(foutTekst(e)))

    // MULTISITE/DOMAIN_CURRENT_SITE uit .env is de duidelijkste bron voor of
    // (en hoe) een project multisite draait — vooraf invullen voordat er
    // ook maar geprobeerd is, zodat het klopt zonder een SSH-verbinding.
    Services.DBCloneService.LocalDefaults(projectId)
      .then(def => {
        setLocalDBName(def.dbName ?? '')
        setLocalDBHost(def.dbHost ?? '')
        setLocalURL(def.url ?? '')
        setMultisite(def.isMultisite ?? false)
        setMultisiteUitEnv(def.isMultisite ?? false)
        setLocalNetworkDomain(def.domainCurrentSite ?? '')
      })
      .catch(() => {})

    Services.SettingsService.Get()
      .then(s => setDbApp(s.dbApp || 'Sequel Ace'))
      .catch(() => {})
  }, [projectId])

  // Zonder expliciete keuze de live-omgeving: dat is waar productie draait.
  useEffect(() => {
    if (!envId && site?.environments?.length) {
      const live = site.environments.find(e => e.name === 'live') ?? site.environments[0]
      setEnvId(live.id)
    }
  }, [site, envId])

  const verbindingControleren = async () => {
    setProbeBezig(true); setFout(null)
    try {
      const p = await Services.DBCloneService.Probe(projectId, envId)
      setProbe(p)
      setProdSiteURL(p.siteUrl)
      setTablePrefix(p.tablePrefix || 'wp_')
      if (p.networkDomain) setProdNetworkDomain(p.networkDomain)
      // .env (MULTISITE=true) is de duidelijkste bron; de probe is er alleen
      // om dat te bevestigen of, bij ontbrekende .env-info, als terugval.
      if (!multisiteUitEnv) setMultisite(p.isMultisite)
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setProbeBezig(false)
    }
  }

  const multisiteConflict = probe !== null && multisiteUitEnv && !probe.isMultisite

  const kloonNaarLokaal = async () => {
    const bevestigd = await bevestig(
      'Database klonen naar lokaal',
      `De lokale database "${localDBName}" wordt overschreven (er wordt eerst automatisch een backup gemaakt als hij al bestaat).\n\n` +
      'De productie-database wordt nergens gewijzigd — er wordt alleen een dump geëxporteerd, nooit teruggeschreven.',
    )
    if (!bevestigd) return

    setKloonBezig(true); setFout(null); setResult(null); setProgress(null)
    const eventNaam = `db:${projectId}:progress`
    const stopListening = Events.On(eventNaam, ev => {
      const data = ev.data
      const parsed: DBCloneProgress | null = typeof data === 'string'
        ? (() => { try { return JSON.parse(data) } catch { return null } })()
        : (data as DBCloneProgress | null)
      if (parsed) setProgress(parsed)
    })

    try {
      const req: DBCloneRequest = {
        prodSiteUrl: prodSiteURL,
        localUrl: localURL,
        localDbName: localDBName,
        localDbHost: localDBHost,
        tablePrefix,
        multisite,
        prodNetworkDomain,
        localNetworkDomain,
      }
      const res = await Services.DBCloneService.Clone(projectId, envId, req)
      setResult(res)
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      stopListening()
      setKloonBezig(false)
    }
  }

  const openInDbApp = useCallback(async () => {
    setFout(null)
    try {
      await Services.DBCloneService.OpenInApp(projectId, localDBHost, localDBName, dbApp)
    } catch (e) {
      setFout(foutTekst(e))
    }
  }, [projectId, localDBHost, localDBName, dbApp])

  if (!site) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center gap-3 p-10 text-center">
        <p className="text-[15px] font-semibold text-fg">Geen Kinsta-site gekoppeld</p>
        <p className="text-[13px] text-fg-muted max-w-[380px]">
          Koppel dit project eerst aan een Kinsta-site via het Kinsta-tabblad; het klonen gebruikt het SSH-adres van die site.
        </p>
        {fout && <p className="text-[12px] text-red">{fout}</p>}
      </div>
    )
  }

  const grootteWaarschuwing = probe && (
    (probe.tmpFreeBytes !== undefined && probe.tmpFreeBytes > 0 && probe.tmpFreeBytes < probe.dbSizeBytes * 2) ||
    probe.dbSizeBytes > 2 * 1024 * 1024 * 1024
  )

  const voortgangPct = progress?.total ? Math.min(100, Math.round(((progress.bytes ?? 0) / progress.total) * 100)) : null

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-hidden">
      <div className="shrink-0 flex items-center gap-2 px-6 py-3 border-b border-border bg-panel flex-wrap">
        <select value={envId} onChange={e => setEnvId(e.target.value)}
          className="bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg">
          {(site.environments ?? []).map(env => (
            <option key={env.id} value={env.id}>{env.display_name || env.name}</option>
          ))}
        </select>
        <button onClick={verbindingControleren} disabled={probeBezig || kloonBezig}
          className="text-[12.5px] text-fg-muted border border-border rounded-lg px-3 py-1.5 hover:bg-hover transition disabled:opacity-50">
          {probeBezig ? <span className="animate-spin inline-block">↻</span> : 'Verbinding controleren'}
        </button>
      </div>

      {fout && <Foutvak fout={fout} className="shrink-0 mx-6 mt-3" />}

      <div className="flex-1 min-h-0 overflow-y-auto px-6 py-4">
        {probe && (
          <div className="flex gap-2.5 flex-wrap mb-4">
            <div className="bg-panel border border-border rounded-xl px-4 py-3 min-w-[130px]">
              <div className="text-[10px] uppercase tracking-wide text-fg-faint mb-1">Databasegrootte</div>
              <div className="text-[17px] font-semibold text-fg leading-tight">{bytesLeesbaar(probe.dbSizeBytes)}</div>
              {probe.isMultisite && <div className="text-[11px] text-amber mt-0.5">multisite</div>}
            </div>
            <div className="bg-panel border border-border rounded-xl px-4 py-3 min-w-[130px]">
              <div className="text-[10px] uppercase tracking-wide text-fg-faint mb-1">Tabelprefix</div>
              <div className="text-[17px] font-semibold text-fg leading-tight font-mono">{probe.tablePrefix || '—'}</div>
            </div>
          </div>
        )}

        {grootteWaarschuwing && (
          <div className="mb-4 bg-amber-soft text-amber px-3 py-2 rounded-lg text-[11.5px]">
            Mogelijk onvoldoende schijfruimte op de server voor de tijdelijke dump — controleer /tmp op de omgeving.
          </div>
        )}

        <div className="bg-panel border border-border rounded-xl p-4 mb-4">
          <div className="text-[12px] font-semibold text-fg mb-3">Bron (productie)</div>
          <label className="block text-[11px] text-fg-muted mb-1">Site-URL (vervangbron)</label>
          <input value={prodSiteURL} onChange={e => setProdSiteURL(e.target.value)}
            className="w-full bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg mb-2 font-mono" />
        </div>

        <div className="bg-panel border border-border rounded-xl p-4 mb-4">
          <div className="text-[12px] font-semibold text-fg mb-3">Doel (lokaal)</div>
          <label className="block text-[11px] text-fg-muted mb-1">Lokale databasenaam</label>
          <input value={localDBName} onChange={e => setLocalDBName(e.target.value)}
            className="w-full bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg mb-2 font-mono" />
          <label className="block text-[11px] text-fg-muted mb-1">Lokale URL</label>
          <input value={localURL} onChange={e => setLocalURL(e.target.value)}
            className="w-full bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg mb-1 font-mono" />
          <p className="text-[10.5px] text-fg-faint mb-2">
            wordt opgeslagen in deploy_conf.json (link.local) — committen doe je zelf
          </p>
          <span className="inline-flex items-center text-[10.5px] font-semibold text-fg-muted bg-panel-2 border border-border px-2 py-[3px] rounded-md">
            host: {localDBHost || 'onbekend'}
          </span>
        </div>

        <div className="bg-panel border border-border rounded-xl p-4 mb-4">
          <label className="flex items-center gap-2 text-[12px] font-semibold text-fg mb-3 cursor-pointer">
            <input type="checkbox" checked={multisite} onChange={e => setMultisite(e.target.checked)} />
            Multisite
            {multisiteUitEnv && <span className="text-[10.5px] font-normal text-fg-faint">(uit .env: MULTISITE=true)</span>}
          </label>
          {multisiteConflict && (
            <div className="mb-3 bg-amber-soft text-amber px-3 py-2 rounded-lg text-[11.5px]">
              .env zegt multisite, maar de server meldt is_multisite() = nee — controleer dit handmatig voor je verdergaat.
            </div>
          )}
          {multisite && (
            <>
              <label className="block text-[11px] text-fg-muted mb-1">Productie-netwerkdomein (kaal, zonder https://)</label>
              <input value={prodNetworkDomain} onChange={e => setProdNetworkDomain(e.target.value)}
                placeholder="bijv. vanluyken.nl"
                className="w-full bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg mb-2 font-mono" />
              <label className="block text-[11px] text-fg-muted mb-1">Lokaal netwerkdomein (uit .env DOMAIN_CURRENT_SITE)</label>
              <input value={localNetworkDomain} onChange={e => setLocalNetworkDomain(e.target.value)}
                placeholder="bijv. vanluykennl.test"
                className="w-full bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg mb-1 font-mono" />
              <p className="text-[10.5px] text-fg-faint">
                bepaalt wat wp_blogs/wp_site na het klonen als domein krijgen — leeg laten valt terug op de kale host van de URL's hierboven
              </p>
            </>
          )}
        </div>

        <div className="flex items-center gap-2.5 mb-4">
          <button onClick={kloonNaarLokaal} disabled={kloonBezig || !localDBName || !localDBHost || !prodSiteURL || !localURL}
            className="bg-accent text-white text-[12.5px] font-semibold px-4 py-1.5 rounded-lg hover:brightness-110 disabled:opacity-50 transition">
            {kloonBezig ? <span className="animate-spin inline-block">↻</span> : 'Klonen naar lokaal'}
          </button>
        </div>

        {progress && (
          <div className="bg-panel border border-border rounded-xl p-4 mb-4">
            <div className="text-[12px] font-semibold text-fg mb-1">{FASE_LABEL[progress.phase] ?? progress.phase}</div>
            <div className="text-[11.5px] text-fg-muted mb-2">{progress.detail}</div>
            {voortgangPct !== null && (
              <div className="w-full h-1.5 bg-panel-2 rounded-full overflow-hidden">
                <div className="h-full bg-accent transition-all" style={{ width: `${voortgangPct}%` }} />
              </div>
            )}
          </div>
        )}

        {result && (
          <div className="bg-panel border border-border rounded-xl p-4">
            <div className="text-[12px] font-semibold text-fg mb-3">Resultaat</div>
            <div className="text-[11.5px] text-fg-muted space-y-1 mb-3">
              <div>Site-URL: <span className="font-mono">{result.siteUrlBefore}</span> → <span className="font-mono">{result.siteUrlAfter}</span></div>
              <div>{result.tablesImported} tabellen geïmporteerd · dump {bytesLeesbaar(result.dumpBytes)}</div>
              {result.backupPath && <div>Backup: <span className="font-mono">{result.backupPath}</span></div>}
            </div>
            {(result.warnings ?? []).map((w, i) => (
              <div key={i} className="bg-amber-soft text-amber px-3 py-2 rounded-lg text-[11.5px] mb-2">{w}</div>
            ))}
            <button onClick={openInDbApp}
              className="text-[12.5px] text-fg-muted border border-border rounded-lg px-3 py-1.5 hover:bg-hover transition">
              Open in {dbApp}
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
