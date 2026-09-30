import { useEffect, useRef, useState } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { WPSite } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import type { Site as KinstaSite, Environment as KinstaEnv } from '../../bindings/github.com/rdm/sites-tool/internal/adapters/kinsta/models'
import type { WPSiteView, ConnectionResult } from '../../bindings/github.com/rdm/sites-tool/internal/services/models'
import Foutvak from './Foutvak'
import KnopTekst from './KnopTekst'
import { bevestig } from '../lib/bevestig'

interface Props {
  initial: WPSite
  isNew: boolean
  hasPassword: boolean
  projectMissing?: boolean
  onSaved: (v: WPSiteView) => void
  onUnlink?: () => void
  onDelete?: () => void
}

const invoer = 'w-full bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg mb-2'
const knop = 'bg-accent text-white text-[12.5px] font-semibold px-4 py-1.5 rounded-lg hover:brightness-110 disabled:opacity-50 transition'
const knopStil = 'border border-border text-fg-muted text-[12.5px] px-3 py-1.5 rounded-lg hover:bg-hover disabled:opacity-50 transition'

const verbindingKleur: Record<string, string> = {
  ok: 'text-green', no_bridge: 'text-amber', auth_failed: 'text-red', unreachable: 'text-red',
}

function foutTekst(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

type Actie = 'opslaan' | 'testen' | 'ontkoppelen' | 'verwijderen'

export default function WPSiteForm({ initial, isNew, hasPassword, projectMissing, onSaved, onUnlink, onDelete }: Props) {
  const [site, setSite] = useState<WPSite>(initial)
  const [tags, setTags] = useState((initial.tags ?? []).join(', '))
  const [wachtwoord, setWachtwoord] = useState('')
  const [bewaard, setBewaard] = useState(hasPassword)
  const [nieuw, setNieuw] = useState(isNew)
  const [actie, setActie] = useState<Actie | null>(null)
  const bezig = actie !== null
  const [fout, setFout] = useState<string | null>(null)
  const [verbinding, setVerbinding] = useState<ConnectionResult | null>(null)

  // Kinsta-koppeling: sitelijst, eventuele laadfout (Kinsta niet ingesteld) en
  // de omgevingen van de gekozen site. Nooit blokkerend voor het formulier.
  const [kinstaSites, setKinstaSites] = useState<KinstaSite[]>([])
  const [kinstaFout, setKinstaFout] = useState<string | null>(null)
  const [kinstaOmgevingen, setKinstaOmgevingen] = useState<KinstaEnv[]>([])
  const [voorgesteld, setVoorgesteld] = useState(false)
  const voorstelRonde = useRef(0)

  useEffect(() => {
    setSite(initial); setTags((initial.tags ?? []).join(', '))
    setWachtwoord(''); setBewaard(hasPassword); setNieuw(isNew)
    setFout(null); setVerbinding(null)
  }, [initial.id, initial.projectPath, isNew, hasPassword])

  // Sitelijst en (bij een nieuwe of lege koppeling) een voorstel op basis van de URL.
  useEffect(() => {
    const ronde = ++voorstelRonde.current
    const actueel = () => voorstelRonde.current === ronde
    setKinstaSites([]); setKinstaFout(null); setVoorgesteld(false)
    ;(async () => {
      try {
        const sites = (await Services.KinstaService.ListSites()) ?? []
        if (!actueel()) return
        setKinstaSites(sites)
        if ((isNew || !initial.kinstaSiteId) && initial.url) {
          const v = await Services.KinstaService.SuggestSiteForURL(initial.url).catch(() => null)
          if (!actueel()) return
          if (v?.found && v.siteId) {
            setSite(s => ({ ...s, kinstaSiteId: v.siteId, kinstaEnvId: '' } as WPSite))
            setVoorgesteld(true)
          }
        }
      } catch (e) {
        if (actueel()) setKinstaFout(foutTekst(e))
      }
    })()
    return () => { voorstelRonde.current++ }
  }, [initial.id, initial.projectPath, initial.kinstaSiteId, initial.url, isNew])

  // Omgevingen van de gekozen site; alleen nodig als er er meer dan één zijn.
  useEffect(() => {
    setKinstaOmgevingen([])
    if (!site.kinstaSiteId) return
    let actueel = true
    Services.KinstaService.GetSiteDetails(site.kinstaSiteId)
      .then(d => { if (actueel) setKinstaOmgevingen(d?.environments ?? []) })
      .catch(() => {})
    return () => { actueel = false }
  }, [site.kinstaSiteId])

  const zet = (veld: keyof WPSite, waarde: string | boolean) => setSite(s => ({ ...s, [veld]: waarde } as WPSite))

  // voerUit zet de actie als "bezig", toont fouten en ruimt altijd op. Eén
  // actie tegelijk: de andere knoppen staan zolang uit.
  const voerUit = async (a: Actie, werk: () => Promise<void>) => {
    setActie(a); setFout(null)
    try {
      await werk()
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setActie(null)
    }
  }

  const opslaan = () => voerUit('opslaan', async () => {
    const volledig = { ...site, tags: tags.split(',').map(t => t.trim()).filter(Boolean) } as WPSite
    const v = nieuw
      ? await Services.WPSiteService.Create(volledig, wachtwoord)
      : await Services.WPSiteService.Update(volledig, wachtwoord)
    setWachtwoord(''); setBewaard(v.hasPassword); setNieuw(false)
    onSaved(v)
  })

  const testen = () => {
    setVerbinding(null)
    return voerUit('testen', async () => {
      setVerbinding(await Services.WPSiteService.TestConnection(site.id))
    })
  }

  const verwijderen = async () => {
    if (!onDelete) return
    if (!(await bevestig('Site verwijderen?', `Site "${site.name}" en het opgeslagen Application Password verwijderen?`))) return
    await voerUit('verwijderen', async () => {
      await Services.WPSiteService.Delete(site.id)
      onDelete()
    })
  }

  const ontkoppelen = async () => {
    if (!onUnlink) return
    if (!(await bevestig('Ontkoppelen?', `Site "${site.name}" ontkoppelen van deze map? De site blijft in de lijst staan, zonder map.`))) return
    await voerUit('ontkoppelen', async () => {
      await Services.WPSiteService.Unlink(site.id)
      onUnlink()
    })
  }

  return (
    <div>
      {fout && <Foutvak fout={fout} className="mb-3" />}
      {projectMissing && (
        <p className="text-[11.5px] text-amber mb-3">Gekoppelde map niet gevonden: {site.projectPath}</p>
      )}

      <label className="block text-[11px] text-fg-muted mb-1">Id (vast na aanmaken)</label>
      <input value={site.id} disabled={!nieuw} onChange={e => zet('id', e.target.value)}
        className={`${invoer} font-mono disabled:opacity-60`} />

      <label className="block text-[11px] text-fg-muted mb-1">Naam</label>
      <input value={site.name} onChange={e => zet('name', e.target.value)} className={invoer} />

      <label className="block text-[11px] text-fg-muted mb-1">URL</label>
      <input value={site.url} onChange={e => zet('url', e.target.value)} placeholder="https://www.klant.nl"
        className={`${invoer} font-mono`} />

      <label className="block text-[11px] text-fg-muted mb-1">WP-gebruiker</label>
      <input value={site.username} onChange={e => zet('username', e.target.value)} className={invoer} />

      <label className="block text-[11px] text-fg-muted mb-1">Application Password</label>
      <input type="password" value={wachtwoord} onChange={e => setWachtwoord(e.target.value)}
        placeholder={bewaard ? 'opgeslagen in de Keychain' : 'xxxx xxxx xxxx xxxx xxxx xxxx'}
        title="Wordt alleen in de macOS-Keychain bewaard. Leeg laten houdt het bestaande wachtwoord."
        className={invoer} />

      <label className="block text-[11px] text-fg-muted mb-1">Kinsta-site</label>
      <select value={site.kinstaSiteId ?? ''} disabled={!!kinstaFout}
        onChange={e => { setVoorgesteld(false); setSite(s => ({ ...s, kinstaSiteId: e.target.value, kinstaEnvId: '' } as WPSite)) }}
        title={kinstaFout ?? undefined}
        className={`${invoer} disabled:opacity-60`}>
        <option value="">— geen —</option>
        {kinstaSites.map(k => <option key={k.id} value={k.id}>{k.display_name || k.name}</option>)}
        {site.kinstaSiteId && !kinstaSites.some(k => k.id === site.kinstaSiteId) &&
          <option value={site.kinstaSiteId}>{site.kinstaSiteId}</option>}
      </select>
      {kinstaFout && <p className="text-[11px] text-amber -mt-1 mb-2">{kinstaFout}</p>}
      {voorgesteld && !kinstaFout && (
        <p className="text-[11px] text-fg-faint -mt-1 mb-2">voorgesteld op basis van de URL</p>
      )}
      {kinstaOmgevingen.length > 1 && (
        <>
          <label className="block text-[11px] text-fg-muted mb-1">Kinsta-omgeving</label>
          <select value={site.kinstaEnvId ?? ''} onChange={e => zet('kinstaEnvId', e.target.value)} className={invoer}>
            <option value="">live (standaard)</option>
            {kinstaOmgevingen.map(o => <option key={o.id} value={o.id}>{o.display_name || o.name}</option>)}
          </select>
        </>
      )}

      <label className="block text-[11px] text-fg-muted mb-1">Tags (komma-gescheiden)</label>
      <input value={tags} onChange={e => setTags(e.target.value)} placeholder="kinsta, production" className={invoer} />

      <label className="flex items-center gap-2 text-[12px] text-fg-muted mb-3">
        <input type="checkbox" checked={site.readOnly} onChange={e => zet('readOnly', e.target.checked)} />
        Alleen lezen (wp-fleet weigert schrijfacties voor deze site)
      </label>

      <label className="flex items-center gap-2 text-[12px] text-fg-muted mb-3">
        <input type="checkbox" checked={!!site.depot} onChange={e => zet('depot', e.target.checked)} />
        Dit is de SatisPress-depotsite (na een core-update maakt de tool een Composer-PR)
      </label>

      <div className="flex flex-wrap items-center gap-2">
        <button onClick={opslaan} disabled={bezig || !site.id || !site.url || (nieuw && !wachtwoord)} className={knop}>
          <KnopTekst tekst="Opslaan" bezig={actie === 'opslaan'} />
        </button>
        {!nieuw && <button onClick={testen} disabled={bezig} className={knopStil}>
          <KnopTekst tekst="Test verbinding" bezig={actie === 'testen'} />
        </button>}
        {!nieuw && onUnlink && <button onClick={ontkoppelen} disabled={bezig} className={knopStil}>
          <KnopTekst tekst="Ontkoppelen" bezig={actie === 'ontkoppelen'} />
        </button>}
        {!nieuw && onDelete && <button onClick={verwijderen} disabled={bezig} className={`${knopStil} text-red`}>
          <KnopTekst tekst="Verwijderen" bezig={actie === 'verwijderen'} />
        </button>}
        {verbinding && (
          <span className={`text-[11.5px] ${verbindingKleur[verbinding.status] ?? 'text-fg-muted'}`}>
            {verbinding.message}
          </span>
        )}
      </div>
    </div>
  )
}
