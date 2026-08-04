import { useState, useEffect } from 'react'
import { Events } from '@wailsio/runtime'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { SiteDetails } from '../../bindings/github.com/rdm/sites-tool/internal/adapters/kinsta/models'
import type { UploadFolder, MediaPullResult } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import Foutvak from './Foutvak'
import { bevestig } from '../lib/bevestig'

interface Props { projectId: string }

interface MediaPullProgress {
  phase: string
  folder?: string
  detail: string
  bytes?: number
  files?: number
  folderIndex?: number
  folderTotal?: number
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

export default function MigrationMediaTab({ projectId }: Props) {
  const [site, setSite] = useState<SiteDetails | null>(null)
  const [envId, setEnvId] = useState('')
  const [folders, setFolders] = useState<UploadFolder[] | null>(null)
  const [gekozen, setGekozen] = useState<Set<string>>(new Set())
  const [doelPad, setDoelPad] = useState('')

  const [lijstBezig, setLijstBezig] = useState(false)
  const [pullBezig, setPullBezig] = useState(false)
  const [progress, setProgress] = useState<MediaPullProgress | null>(null)
  const [result, setResult] = useState<MediaPullResult | null>(null)
  const [fout, setFout] = useState<string | null>(null)

  useEffect(() => {
    setSite(null); setFolders(null); setGekozen(new Set()); setResult(null)
    setFout(null); setEnvId(''); setProgress(null)

    Services.KinstaService.GetLinkedSiteID(projectId)
      .then(id => (id ? Services.KinstaService.GetSiteDetails(id).then(setSite) : undefined))
      .catch(e => setFout(foutTekst(e)))

    Services.MigrationService.LocalUploadsPath(projectId)
      .then(setDoelPad)
      .catch(() => {})
  }, [projectId])

  useEffect(() => {
    if (!envId && site?.environments?.length) {
      const live = site.environments.find(e => e.name === 'live') ?? site.environments[0]
      setEnvId(live.id)
    }
  }, [site, envId])

  const mappenOphalen = async () => {
    setLijstBezig(true); setFout(null)
    try {
      const lijst = await Services.MigrationService.ListUploadFolders(projectId, envId)
      setFolders(lijst ?? [])
      setGekozen(new Set())
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setLijstBezig(false)
    }
  }

  const toggle = (naam: string) => {
    setGekozen(huidig => {
      const volgende = new Set(huidig)
      if (volgende.has(naam)) volgende.delete(naam)
      else volgende.add(naam)
      return volgende
    })
  }

  const totaalGekozen = (folders ?? [])
    .filter(f => gekozen.has(f.name))
    .reduce((som, f) => som + f.bytes, 0)

  const pullen = async () => {
    const namen = Array.from(gekozen)
    const bevestigd = await bevestig(
      'Media ophalen van productie',
      `${namen.length} map(pen), samen ${bytesLeesbaar(totaalGekozen)}, worden gedownload naar:\n${doelPad}\n\n` +
      'Bestanden die daar al staan worden overschreven, zodat je lokale kopie exact gelijk is aan productie. ' +
      'Op productie wordt niets gewijzigd — er wordt alleen gelezen.',
    )
    if (!bevestigd) return

    setPullBezig(true); setFout(null); setResult(null); setProgress(null)
    const stopListening = Events.On(`migration:${projectId}:media`, ev => {
      const data = ev.data
      const parsed: MediaPullProgress | null = typeof data === 'string'
        ? (() => { try { return JSON.parse(data) } catch { return null } })()
        : (data as MediaPullProgress | null)
      if (parsed) setProgress(parsed)
    })

    try {
      const res = await Services.MigrationService.PullMedia(projectId, envId, namen)
      setResult(res)
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      stopListening()
      setPullBezig(false)
    }
  }

  if (!site) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center gap-3 p-10 text-center">
        <p className="text-[15px] font-semibold text-fg">Geen Kinsta-site gekoppeld</p>
        <p className="text-[13px] text-fg-muted max-w-[380px]">
          Koppel dit project eerst aan een Kinsta-site via het Kinsta-tabblad; het ophalen gebruikt het SSH-adres van die site.
        </p>
        {fout && <p className="text-[12px] text-red">{fout}</p>}
      </div>
    )
  }

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-hidden">
      <div className="shrink-0 flex items-center gap-2 px-6 py-3 border-b border-border bg-panel flex-wrap">
        <select value={envId} onChange={e => setEnvId(e.target.value)}
          className="bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg">
          {(site.environments ?? []).map(env => (
            <option key={env.id} value={env.id}>{env.display_name || env.name}</option>
          ))}
        </select>
        <button onClick={mappenOphalen} disabled={lijstBezig || pullBezig || !envId}
          className="text-[12.5px] text-fg-muted border border-border rounded-lg px-3 py-1.5 hover:bg-hover transition disabled:opacity-50">
          {lijstBezig ? <span className="animate-spin inline-block">↻</span> : 'Mappen ophalen'}
        </button>
        {gekozen.size > 0 && (
          <button onClick={pullen} disabled={pullBezig}
            className="ml-auto bg-accent text-white text-[12.5px] font-semibold px-4 py-1.5 rounded-lg hover:brightness-110 disabled:opacity-50 transition">
            {pullBezig ? <span className="animate-spin inline-block">↻</span> : `▼ ${gekozen.size} map(pen) ophalen · ${bytesLeesbaar(totaalGekozen)}`}
          </button>
        )}
      </div>

      {fout && <Foutvak fout={fout} className="shrink-0 mx-6 mt-3" />}

      <div className="flex-1 min-h-0 overflow-y-auto px-6 py-4">
        {doelPad && (
          <p className="text-[11.5px] text-fg-muted mb-4">
            Doel: <span className="font-mono text-fg">{doelPad}</span>
            <span className="text-fg-faint"> · deze map staat in .gitignore, dus gepulde media komt nooit in een commit</span>
          </p>
        )}

        {folders === null ? (
          <div className="text-fg-faint text-[13px] italic py-10 text-center">
            Druk op “Mappen ophalen” om te zien wat er in wp-content/uploads op de server staat.
          </div>
        ) : folders.length === 0 ? (
          <div className="text-fg-faint text-[13px] italic py-10 text-center">
            Geen mappen gevonden in wp-content/uploads.
          </div>
        ) : (
          <>
            <div className="flex items-center gap-2.5 mb-2">
              <button onClick={() => setGekozen(new Set(folders.map(f => f.name)))}
                className="text-[11.5px] text-fg-muted hover:text-fg transition">alles selecteren</button>
              <button onClick={() => setGekozen(new Set())}
                className="text-[11.5px] text-fg-muted hover:text-fg transition">selectie wissen</button>
            </div>
            <div className="bg-panel border border-border rounded-xl divide-y divide-border overflow-hidden">
              {folders.map(f => {
                const grootste = Math.max(...folders.map(x => x.bytes), 1)
                const breedte = Math.max(2, Math.round((f.bytes / grootste) * 100))
                return (
                  <label key={f.name}
                    className="flex items-center gap-3 px-4 py-2.5 cursor-pointer hover:bg-hover transition">
                    <input type="checkbox" checked={gekozen.has(f.name)} onChange={() => toggle(f.name)} />
                    <span className="text-[12.5px] font-mono text-fg flex-1 truncate">{f.name}</span>
                    <div className="w-[140px] h-1.5 bg-panel-2 rounded-full overflow-hidden shrink-0">
                      <div className="h-full bg-accent" style={{ width: `${breedte}%` }} />
                    </div>
                    <span className="text-[11.5px] text-fg-muted w-[80px] text-right shrink-0">
                      {f.bytes > 0 ? bytesLeesbaar(f.bytes) : '—'}
                    </span>
                  </label>
                )
              })}
            </div>
          </>
        )}

        {progress && (
          <div className="bg-panel border border-border rounded-xl p-4 mt-4">
            <div className="text-[12px] font-semibold text-fg mb-1">
              {progress.folder ? `Map ${progress.folder}` : 'Bezig'}
              {progress.folderTotal ? ` · ${progress.folderIndex}/${progress.folderTotal}` : ''}
            </div>
            <div className="text-[11.5px] text-fg-muted">
              {progress.detail}
              {progress.files ? ` · ${progress.files.toLocaleString('nl-NL')} bestanden` : ''}
              {progress.bytes ? ` · ${bytesLeesbaar(progress.bytes)}` : ''}
            </div>
          </div>
        )}

        {result && (
          <div className="bg-panel border border-border rounded-xl p-4 mt-4">
            <div className="text-[12px] font-semibold text-fg mb-2">Klaar</div>
            <div className="text-[11.5px] text-fg-muted space-y-1">
              <div>{result.filesWritten.toLocaleString('nl-NL')} bestanden · {bytesLeesbaar(result.bytesWritten)}</div>
              <div>Mappen: <span className="font-mono">{(result.folders ?? []).join(', ')}</span></div>
              <div>Locatie: <span className="font-mono">{result.localPath}</span></div>
            </div>
            {(result.warnings ?? []).length > 0 && (
              <div className="mt-3 space-y-1.5">
                {(result.warnings ?? []).slice(0, 20).map((w, i) => (
                  <div key={i} className="bg-amber-soft text-amber px-3 py-1.5 rounded-lg text-[11px]">{w}</div>
                ))}
                {(result.warnings ?? []).length > 20 && (
                  <div className="text-[11px] text-fg-faint">
                    …en {(result.warnings ?? []).length - 20} meer
                  </div>
                )}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
