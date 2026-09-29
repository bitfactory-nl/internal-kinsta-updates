import { useEffect, useState } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { WPSite } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import type { WPSiteView, ConnectionResult } from '../../bindings/github.com/rdm/sites-tool/internal/services/models'
import Foutvak from './Foutvak'

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

export default function WPSiteForm({ initial, isNew, hasPassword, projectMissing, onSaved, onUnlink, onDelete }: Props) {
  const [site, setSite] = useState<WPSite>(initial)
  const [tags, setTags] = useState((initial.tags ?? []).join(', '))
  const [wachtwoord, setWachtwoord] = useState('')
  const [bewaard, setBewaard] = useState(hasPassword)
  const [nieuw, setNieuw] = useState(isNew)
  const [bezig, setBezig] = useState(false)
  const [fout, setFout] = useState<string | null>(null)
  const [verbinding, setVerbinding] = useState<ConnectionResult | null>(null)

  useEffect(() => {
    setSite(initial); setTags((initial.tags ?? []).join(', '))
    setWachtwoord(''); setBewaard(hasPassword); setNieuw(isNew)
    setFout(null); setVerbinding(null)
  }, [initial.id, initial.projectPath, isNew, hasPassword])

  const zet = (veld: keyof WPSite, waarde: string | boolean) => setSite(s => ({ ...s, [veld]: waarde } as WPSite))

  const opslaan = async () => {
    setBezig(true); setFout(null)
    const volledig = { ...site, tags: tags.split(',').map(t => t.trim()).filter(Boolean) } as WPSite
    try {
      const v = nieuw
        ? await Services.WPSiteService.Create(volledig, wachtwoord)
        : await Services.WPSiteService.Update(volledig, wachtwoord)
      setWachtwoord(''); setBewaard(v.hasPassword); setNieuw(false)
      onSaved(v)
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setBezig(false)
    }
  }

  const testen = async () => {
    setBezig(true); setFout(null); setVerbinding(null)
    try {
      setVerbinding(await Services.WPSiteService.TestConnection(site.id))
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setBezig(false)
    }
  }

  const verwijderen = async () => {
    if (!onDelete) return
    if (!window.confirm(`Site "${site.name}" en het opgeslagen Application Password verwijderen?`)) return
    setBezig(true); setFout(null)
    try {
      await Services.WPSiteService.Delete(site.id)
      onDelete()
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setBezig(false)
    }
  }

  const ontkoppelen = async () => {
    if (!onUnlink) return
    setBezig(true); setFout(null)
    try {
      await Services.WPSiteService.Unlink(site.id)
      onUnlink()
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setBezig(false)
    }
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

      <label className="block text-[11px] text-fg-muted mb-1">Tags (komma-gescheiden)</label>
      <input value={tags} onChange={e => setTags(e.target.value)} placeholder="kinsta, production" className={invoer} />

      <label className="flex items-center gap-2 text-[12px] text-fg-muted mb-3">
        <input type="checkbox" checked={site.readOnly} onChange={e => zet('readOnly', e.target.checked)} />
        Alleen lezen (wp-fleet weigert schrijfacties voor deze site)
      </label>

      <div className="flex flex-wrap items-center gap-2">
        <button onClick={opslaan} disabled={bezig || !site.id || !site.url || (nieuw && !wachtwoord)} className={knop}>
          {bezig ? <span className="animate-spin inline-block">↻</span> : 'Opslaan'}
        </button>
        {!nieuw && <button onClick={testen} disabled={bezig} className={knopStil}>Test verbinding</button>}
        {!nieuw && onUnlink && <button onClick={ontkoppelen} disabled={bezig} className={knopStil}>Ontkoppelen</button>}
        {!nieuw && onDelete && <button onClick={verwijderen} disabled={bezig} className={`${knopStil} text-red`}>Verwijderen</button>}
        {verbinding && (
          <span className={`text-[11.5px] ${verbindingKleur[verbinding.status] ?? 'text-fg-muted'}`}>
            {verbinding.message}
          </span>
        )}
      </div>
    </div>
  )
}
