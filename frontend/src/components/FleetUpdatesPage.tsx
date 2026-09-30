import { useCallback, useEffect, useState } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { FleetSiteUpdates, WPSiteView } from '../../bindings/github.com/rdm/sites-tool/internal/services/models'
import Foutvak from './Foutvak'
import KnopTekst from './KnopTekst'
import { hoogsteVersie } from '../lib/versie'

interface Props {
  onOpenProject: (projectId: string) => void
  onOpenSite: (v: WPSiteView) => void
}

function aantal(r: FleetSiteUpdates): number {
  const u = r.updates
  // Vertalingen tellen samen als één update.
  return u ? u.core.length + u.plugins.length + u.themes.length + (u.translations > 0 ? 1 : 0) : 0
}

// Sites met updates bovenaan, dan fouten, dan bijgewerkte sites.
function rang(r: FleetSiteUpdates): number {
  if (aantal(r) > 0) return 0
  if (r.error) return 1
  return 2
}

export default function FleetUpdatesPage({ onOpenProject, onOpenSite }: Props) {
  const [rijen, setRijen] = useState<FleetSiteUpdates[] | null>(null)
  const [bezig, setBezig] = useState(false)
  const [fout, setFout] = useState<string | null>(null)

  const laad = useCallback(async (refresh: boolean) => {
    setBezig(true); setFout(null)
    try {
      const r = await Services.WPSiteService.FleetUpdates(refresh)
      setRijen([...r].sort((a, b) => rang(a) - rang(b) || a.site.site.name.localeCompare(b.site.site.name)))
    } catch (e) {
      setFout(e instanceof Error ? e.message : String(e))
    } finally {
      setBezig(false)
    }
  }, [])

  useEffect(() => { void laad(false) }, [laad])

  const open = (r: FleetSiteUpdates) => (r.projectId ? onOpenProject(r.projectId) : onOpenSite(r.site))

  return (
    <div className="flex-1 flex flex-col min-h-0 overflow-hidden">
      <div className="h-[56px] px-[22px] border-b border-border bg-panel shrink-0 flex items-center gap-2">
        <h2 className="text-[15px] font-bold tracking-[-.01em] text-fg flex-1">WordPress-vloot</h2>
        <button onClick={() => laad(true)} disabled={bezig}
          className="border border-border text-fg-muted text-[12px] px-3 py-1.5 rounded-lg hover:bg-hover disabled:opacity-50 transition">
          <KnopTekst tekst="Alles verversen" bezig={bezig} />
        </button>
      </div>
      <div className="flex-1 min-h-0 overflow-y-auto px-6 py-4">
        {fout && <Foutvak fout={fout} className="mb-3" />}
        {rijen && rijen.length === 0 && <p className="text-[12px] text-fg-faint">Nog geen gekoppelde WordPress-sites.</p>}
        {rijen && rijen.length > 0 && (
          <table className="w-full text-[12.5px] bg-panel border border-border rounded-xl overflow-hidden">
            <thead>
              <tr className="text-left text-[11px] uppercase tracking-wide text-fg-faint">
                <th className="px-3 py-2">Site</th>
                <th className="px-3 py-2">Core</th>
                <th className="px-3 py-2">Plugins</th>
                <th className="px-3 py-2">Thema's</th>
                <th className="px-3 py-2">Gecontroleerd</th>
                <th className="px-3 py-2">Status</th>
              </tr>
            </thead>
            <tbody>
              {rijen.map(r => (
                <tr key={r.site.site.id} onClick={() => open(r)} className="border-t border-border hover:bg-hover cursor-pointer">
                  <td className="px-3 py-2 text-fg">{r.site.site.name}{r.site.site.depot && <span className="ml-1.5 text-[10px] text-accent">depot</span>}</td>
                  <td className="px-3 py-2 font-mono">{r.updates ? (hoogsteVersie(r.updates.core.map(c => c.version)) ?? '—') : ''}</td>
                  <td className="px-3 py-2 font-mono">{r.updates ? r.updates.plugins.length : ''}</td>
                  <td className="px-3 py-2 font-mono">{r.updates ? r.updates.themes.length : ''}</td>
                  <td className="px-3 py-2 text-fg-faint">{r.updates?.checkedAt ? new Date(r.updates.checkedAt).toLocaleString('nl-NL') : ''}</td>
                  <td className={`px-3 py-2 ${r.error ? 'text-red' : aantal(r) > 0 ? 'text-amber' : 'text-green'}`}>
                    {r.error || (aantal(r) > 0 ? `${aantal(r)} update(s)` : 'bijgewerkt')}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
