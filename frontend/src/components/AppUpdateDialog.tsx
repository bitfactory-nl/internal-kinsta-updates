import { useEffect, useState } from 'react'
import { Events } from '@wailsio/runtime'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { AvailableUpdate } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'

/** Payload van het `updates:progress`-event; spiegelt domain.UpdateProgress in Go. */
type UpdateProgress = { phase: string; done: number; total: number }

interface Props {
  /** 'available' vraagt om te installeren; 'whatsnew' toont wat er net is bijgewerkt. */
  mode: 'available' | 'whatsnew'
  currentVersion: string
  update: AvailableUpdate
  /** Wegklikken zonder installeren; alleen relevant in de 'available'-modus. */
  onLater: () => void
  /** Sluiten na een geslaagde installatie of in de 'whatsnew'-modus. */
  onKlaar: () => void
}

const KOP_PER_SOORT: Record<string, string> = {
  nieuw: 'Nieuw',
  opgelost: 'Opgelost',
  overig: 'Overig',
}
const SOORT_ORDE = ['nieuw', 'opgelost', 'overig']

function mb(bytes: number): string {
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

/** Wijzigingen per soort, in een vaste volgorde en zonder lege groepen. */
function Wijzigingen({ update }: { update: AvailableUpdate }) {
  const regels = update.changes ?? []
  if (regels.length === 0) {
    return (
      <p className="text-[12.5px] text-fg-faint">
        Geen details beschikbaar voor deze versie.
      </p>
    )
  }
  return (
    <div className="space-y-3">
      {SOORT_ORDE.map(soort => {
        const vanSoort = regels.filter(r => r.kind === soort)
        if (vanSoort.length === 0) return null
        return (
          <div key={soort}>
            <h4 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-1.5">
              {KOP_PER_SOORT[soort] ?? soort}
            </h4>
            <ul className="space-y-1">
              {vanSoort.map((r, i) => (
                <li key={`${soort}-${i}`} className="text-[12.5px] text-fg-muted flex gap-2">
                  <span className="text-accent shrink-0">•</span>
                  <span>{r.text}</span>
                </li>
              ))}
            </ul>
          </div>
        )
      })}
    </div>
  )
}

export default function AppUpdateDialog({ mode, currentVersion, update, onLater, onKlaar }: Props) {
  const [installeren, setInstalleren] = useState(false)
  const [voortgang, setVoortgang] = useState<UpdateProgress | null>(null)
  const [fout, setFout] = useState<string | null>(null)

  // Voortgang komt van de backend; de app sluit zichzelf zodra het
  // helper-script is gestart, dus de laatste fase blijft kort in beeld.
  useEffect(() => {
    const stop = Events.On('updates:progress', ev => {
      const data = ev.data
      const p: UpdateProgress | null = typeof data === 'string'
        ? (() => { try { return JSON.parse(data) } catch { return null } })()
        : (Array.isArray(data) ? data[0] : (data as UpdateProgress | undefined)) ?? null
      if (p) setVoortgang(p)
    })
    return () => stop()
  }, [])

  const installeer = async () => {
    setInstalleren(true)
    setFout(null)
    try {
      await Services.UpdateService.Install()
      // Lukt dit, dan sluit de app zichzelf en is deze regel niet meer zichtbaar.
      onKlaar()
    } catch (e) {
      setFout(String(e))
      setInstalleren(false)
    }
  }

  const later = async () => {
    try {
      await Services.UpdateService.Skip(update.version)
    } catch {
      // Niet erg: dan komt de popup bij een volgende check nog één keer.
    }
    onLater()
  }

  const percentage = voortgang && voortgang.total > 0
    ? Math.round((voortgang.done / voortgang.total) * 100)
    : null

  const faseTekst = (() => {
    if (!voortgang) return 'Voorbereiden…'
    if (voortgang.phase === 'download') {
      return percentage !== null ? `Downloaden… ${percentage}%` : 'Downloaden…'
    }
    if (voortgang.phase === 'uitpakken') return 'Uitpakken en controleren…'
    if (voortgang.phase === 'vervangen') return 'App wordt vervangen, hij start straks zelf opnieuw…'
    return 'Bezig…'
  })()

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-6"
      role="dialog"
      aria-modal="true"
      aria-label={mode === 'available' ? 'Update beschikbaar' : 'Bijgewerkt'}
    >
      <div className="w-full max-w-[520px] max-h-full flex flex-col bg-panel border border-border rounded-[14px] shadow-2xl overflow-hidden">
        <div className="px-5 py-4 border-b border-border">
          <h3 className="text-[15px] font-bold text-fg">
            {mode === 'available' ? 'Update beschikbaar' : `Bijgewerkt naar ${update.version}`}
          </h3>
          <p className="text-[12px] text-fg-faint mt-0.5 font-mono">
            {mode === 'available'
              ? `${currentVersion} → ${update.version}${update.sizeBytes > 0 ? ` · ${mb(update.sizeBytes)}` : ''}`
              : currentVersion}
          </p>
        </div>

        <div className="px-5 py-4 overflow-y-auto flex-1 min-h-0">
          <Wijzigingen update={update} />
          {mode === 'whatsnew' && (
            <p className="text-[11.5px] text-fg-faint mt-4">
              De macOS-permissies van deze app zijn opnieuw gevraagd doordat de
              app-identiteit is gewijzigd. Je API-keys zijn automatisch
              overgezet.
            </p>
          )}
        </div>

        {installeren && (
          <div className="px-5 py-3 border-t border-border">
            <p className="text-[12.5px] text-fg-muted mb-2">{faseTekst}</p>
            <div className="h-1.5 rounded-full bg-panel-2 overflow-hidden">
              <div
                className={`h-full bg-accent transition-[width] duration-200 ${percentage === null ? 'animate-pulse w-1/3' : ''}`}
                style={percentage !== null ? { width: `${percentage}%` } : undefined}
              />
            </div>
          </div>
        )}

        {fout && (
          <div className="px-5 py-3 border-t border-border">
            <p className="text-[12.5px] text-red">{fout}</p>
          </div>
        )}

        <div className="px-5 py-3 bg-panel border-t border-border flex items-center gap-3 justify-end">
          {mode === 'available' ? (
            <>
              <button
                onClick={later}
                disabled={installeren}
                className="bg-panel-2 border border-border text-fg-muted text-[12.5px] font-semibold px-[15px] py-[9px]
                           rounded-[9px] hover:bg-hover disabled:opacity-50 transition-colors"
              >
                Later
              </button>
              <button
                onClick={installeer}
                disabled={installeren}
                className="bg-accent text-white text-[12.5px] font-semibold px-[18px] py-[9px] rounded-[9px]
                           hover:bg-accent-2 disabled:opacity-50 transition-colors flex items-center gap-2"
              >
                {installeren && <span className="animate-spin inline-block text-xs">↻</span>}
                Nu installeren
              </button>
            </>
          ) : (
            <button
              onClick={onKlaar}
              className="bg-accent text-white text-[12.5px] font-semibold px-[18px] py-[9px] rounded-[9px]
                         hover:bg-accent-2 transition-colors"
            >
              Aan de slag
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
