import { useEffect, useState } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import { ImportChoice } from '../../bindings/github.com/rdm/sites-tool/internal/services/models'
import type { ImportCandidate } from '../../bindings/github.com/rdm/sites-tool/internal/services/models'
import KnopTekst from './KnopTekst'

interface Props {
  /** Sluiten na importeren of overslaan; de aanroeper laadt de lijst opnieuw. */
  onKlaar: () => void
}

interface Keuze { aan: boolean; alsSite: boolean }

const knopStil = 'bg-panel-2 border border-border text-fg-muted text-[12.5px] font-semibold px-[15px] py-[9px] rounded-[9px] hover:bg-hover disabled:opacity-50 transition-colors'
const knopHoofd = 'bg-accent text-white text-[12.5px] font-semibold px-[18px] py-[9px] rounded-[9px] hover:bg-accent-2 disabled:opacity-50 transition-colors'

function foutTekst(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

export default function ImportDialog({ onKlaar }: Props) {
  const [kandidaten, setKandidaten] = useState<ImportCandidate[] | null>(null)
  const [keuzes, setKeuzes] = useState<Record<string, Keuze>>({})
  const [actie, setActie] = useState<'importeren' | 'overslaan' | null>(null)
  const [fout, setFout] = useState<string | null>(null)
  // Na het toepassen blijft het venster staan tot de gebruiker het resultaat gezien heeft.
  const [resultaat, setResultaat] = useState<{ aantal: number; notities: string[]; fout: string | null } | null>(null)

  useEffect(() => {
    Services.ImportService.Candidates()
      .then(lijst => {
        const l = lijst ?? []
        setKandidaten(l)
        // Zonder URL kan een WordPress-map geen site worden: standaard lokaal.
        setKeuzes(Object.fromEntries(l.map(c => [c.path, { aan: c.wordPress, alsSite: !!c.url }])))
      })
      .catch(e => { setKandidaten([]); setFout(foutTekst(e)) })
  }, [])

  const zet = (pad: string, delta: Partial<Keuze>) =>
    setKeuzes(k => ({ ...k, [pad]: { ...k[pad], ...delta } }))

  const pasToe = async (a: 'importeren' | 'overslaan') => {
    setActie(a); setFout(null)
    const gekozen = a === 'overslaan' ? [] : (kandidaten ?? [])
      .filter(c => keuzes[c.path]?.aan)
      .map(c => new ImportChoice({ path: c.path, asSite: c.wordPress && keuzes[c.path].alsSite }))
    try {
      const r = await Services.ImportService.Apply(gekozen)
      if (a === 'overslaan') { onKlaar(); return }
      setResultaat({ aantal: r.added, notities: r.notes ?? [], fout: null })
    } catch (e) {
      // De Go-kant past de rest wel toe; toon de fout, sluiten doet de gebruiker.
      if (a === 'overslaan') { onKlaar(); return }
      setResultaat({ aantal: 0, notities: [], fout: foutTekst(e) })
    } finally {
      setActie(null)
    }
  }

  const bezig = actie !== null

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-6"
      role="dialog"
      aria-modal="true"
      aria-label="Projecten importeren"
    >
      <div className="w-full max-w-[720px] max-h-full flex flex-col bg-panel border border-border rounded-[14px] shadow-2xl overflow-hidden">
        <div className="px-5 py-4 border-b border-border">
          <h3 className="text-[15px] font-bold text-fg">Projecten importeren</h3>
          <p className="text-[12px] text-fg-muted mt-1">
            De tool scant geen mappen meer. Kies welke projecten je wilt houden; Application Passwords vul je daarna per site in.
          </p>
        </div>

        <div className="px-5 py-4 overflow-y-auto flex-1 min-h-0">
          {resultaat ? (
            <div className="space-y-2">
              {resultaat.fout ? (
                <>
                  <p className="text-[13px] text-fg">Import gedeeltelijk mislukt:</p>
                  <p className="text-[12.5px] text-red whitespace-pre-wrap">{resultaat.fout}</p>
                </>
              ) : (
                <p className="text-[13px] text-fg">
                  {resultaat.aantal} {resultaat.aantal === 1 ? 'project' : 'projecten'} toegevoegd.
                </p>
              )}
              {resultaat.notities.map(n => (
                <p key={n} className="text-[12px] text-amber">{n}</p>
              ))}
            </div>
          ) : kandidaten === null ? (
            <p className="text-[12.5px] text-fg-faint">Laden…</p>
          ) : kandidaten.length === 0 ? (
            <p className="text-[12.5px] text-fg-faint">Geen projecten gevonden om te importeren.</p>
          ) : (
            <table className="w-full text-[12.5px]">
              <thead>
                <tr className="text-left text-[11px] text-fg-faint uppercase tracking-[.06em]">
                  <th className="w-6" />
                  <th className="font-semibold py-1 pr-2">Naam</th>
                  <th className="font-semibold py-1 pr-2">Type</th>
                  <th className="font-semibold py-1 pr-2">URL</th>
                  <th className="font-semibold py-1">Importeren als</th>
                </tr>
              </thead>
              <tbody>
                {kandidaten.map(c => {
                  const k = keuzes[c.path] ?? { aan: false, alsSite: true }
                  return (
                    <tr key={c.path} className="border-t border-border">
                      <td className="py-1.5">
                        <input type="checkbox" checked={k.aan} disabled={bezig}
                          onChange={e => zet(c.path, { aan: e.target.checked })}
                          aria-label={`${c.name} importeren`} />
                      </td>
                      <td className="py-1.5 pr-2 text-fg" title={c.path}>{c.name}</td>
                      <td className="py-1.5 pr-2 font-mono text-fg-muted">{c.deployType || 'overig'}</td>
                      <td className="py-1.5 pr-2 font-mono text-fg-muted truncate max-w-[180px]" title={c.url}>
                        {c.url || (c.wordPress && <span className="font-sans text-amber">geen URL in deploy_conf.json</span>)}
                      </td>
                      <td className="py-1.5">
                        {k.aan && c.wordPress && (
                          <select value={k.alsSite ? 'site' : 'lokaal'} disabled={bezig}
                            onChange={e => zet(c.path, { alsSite: e.target.value === 'site' })}
                            className="bg-panel-2 border border-border rounded-md px-1.5 py-0.5 text-[12px] text-fg">
                            <option value="site">als site</option>
                            <option value="lokaal">als lokaal project</option>
                          </select>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
          {fout && !resultaat && <p className="text-[12.5px] text-red mt-3">{fout}</p>}
        </div>

        <div className="px-5 py-3 bg-panel border-t border-border flex items-center gap-3 justify-end">
          {resultaat ? (
            <button onClick={onKlaar} className={knopHoofd}>Sluiten</button>
          ) : (
            <>
              <button onClick={() => pasToe('overslaan')} disabled={bezig} className={knopStil}>
                <KnopTekst tekst="Overslaan" bezig={actie === 'overslaan'} />
              </button>
              <button onClick={() => pasToe('importeren')} disabled={bezig || kandidaten === null} className={knopHoofd}>
                <KnopTekst tekst="Importeren" bezig={actie === 'importeren'} />
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
