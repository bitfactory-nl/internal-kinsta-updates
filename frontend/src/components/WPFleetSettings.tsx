import { useEffect, useState } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { FleetStatus } from '../../bindings/github.com/rdm/sites-tool/internal/services/models'
import Foutvak from './Foutvak'

function foutTekst(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

export default function WPFleetSettings() {
  const [status, setStatus] = useState<FleetStatus | null>(null)
  const [image, setImage] = useState('')
  const [readOnly, setReadOnly] = useState(false)
  const [bezig, setBezig] = useState(false)
  const [fout, setFout] = useState<string | null>(null)
  const [opgeslagen, setOpgeslagen] = useState(false)

  const laad = () => {
    Services.WPFleetService.Status()
      .then(s => { setStatus(s); setImage(s.image); setReadOnly(s.readOnly) })
      .catch(e => setFout(foutTekst(e)))
  }
  useEffect(laad, [])

  const opslaan = async () => {
    setBezig(true); setFout(null); setOpgeslagen(false)
    try {
      await Services.WPFleetService.SaveSettings(image, readOnly)
      setOpgeslagen(true)
      laad()
    } catch (e) { setFout(foutTekst(e)) } finally { setBezig(false) }
  }

  const registreren = async () => {
    if (status && !status.stableLocation && !window.confirm(
      `Deze app draait niet vanuit /Applications (${status.executable}). ` +
      'Claude Code verwijst dan naar een pad dat kan verdwijnen. Toch registreren?')) return
    setBezig(true); setFout(null)
    try { setStatus(await Services.WPFleetService.Register()) }
    catch (e) { setFout(foutTekst(e)) } finally { setBezig(false) }
  }

  const afmelden = async () => {
    setBezig(true); setFout(null)
    try { await Services.WPFleetService.Unregister(); laad() }
    catch (e) { setFout(foutTekst(e)) } finally { setBezig(false) }
  }

  const registratieTekst = !status ? '…'
    : !status.registered ? 'niet geregistreerd'
    : status.pointsToThisApp ? '✓ geregistreerd, wijst naar deze app'
    : 'geregistreerd, maar niet via deze app (oude configuratie)'

  return (
    <section>
      <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">wp-fleet</h3>
      {fout && <Foutvak fout={fout} className="mb-2.5" />}
      <div className="bg-panel border border-border rounded-[11px] divide-y divide-border">
        <div className="flex items-center gap-4 px-4 py-3">
          <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Image</label>
          <input value={image} onChange={e => { setImage(e.target.value); setOpgeslagen(false) }}
            className="flex-1 bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg font-mono" />
        </div>
        <div className="flex items-center gap-4 px-4 py-3">
          <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Alleen lezen</label>
          <input type="checkbox" checked={readOnly} onChange={e => { setReadOnly(e.target.checked); setOpgeslagen(false) }} />
          <span className="text-[11.5px] text-fg-faint flex-1">wp-fleet registreert dan geen schrijf-tools</span>
          <button onClick={opslaan} disabled={bezig || !image}
            className="bg-accent text-white text-[12.5px] font-semibold px-4 py-1.5 rounded-lg hover:brightness-110 disabled:opacity-50 transition">
            Opslaan
          </button>
          {opgeslagen && <span className="text-[11.5px] text-green">✓</span>}
        </div>
        <div className="flex items-center gap-4 px-4 py-3">
          <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Sites</label>
          <span className="text-[12.5px] text-fg flex-1">
            {status ? `${status.sites} gekoppeld${status.sitesWithoutPassword ? ` · ${status.sitesWithoutPassword} zonder wachtwoord (worden overgeslagen)` : ''}` : '…'}
          </span>
        </div>
        <div className="flex items-center gap-4 px-4 py-3">
          <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Claude Code</label>
          <span className="text-[12.5px] text-fg flex-1">{registratieTekst}</span>
          <button onClick={registreren} disabled={bezig}
            className="bg-accent text-white text-[12.5px] font-semibold px-4 py-1.5 rounded-lg hover:brightness-110 disabled:opacity-50 transition">
            Registreer bij Claude Code
          </button>
          {status?.registered && (
            <button onClick={afmelden} disabled={bezig}
              className="border border-border text-fg-muted text-[12.5px] px-3 py-1.5 rounded-lg hover:bg-hover disabled:opacity-50 transition">
              Verwijder registratie
            </button>
          )}
        </div>
      </div>
    </section>
  )
}
