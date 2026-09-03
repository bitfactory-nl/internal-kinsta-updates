import { useEffect, useState, useCallback } from 'react'
import { Events } from '@wailsio/runtime'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type {
  BulkUpdatePlan,
  BulkUpdateResult,
  BulkUpdateProjectResult,
} from '../../bindings/github.com/rdm/sites-tool/internal/services'
import { RefreshIcon, CloudDownloadIcon } from './icons'
import ExternalLink from './ExternalLink'
import { bevestig } from '../lib/bevestig'

function foutTekst(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

interface Voortgang {
  index: number
  total: number
  projectName: string
  phase: string
}

// BulkUpdatePage werkt alle WordPress-projecten in één run bij vanuit de
// referentie-installatie: plugins én core. Eerst een voorbeschouwing (wat zou
// er gebeuren), dan een bewuste keuze welke projecten meedoen, en pas daarna
// de run. Mergen blijft per project een losse knop.
export default function BulkUpdatePage() {
  const [plan, setPlan] = useState<BulkUpdatePlan | null>(null)
  const [bezig, setBezig] = useState(false)
  const [fout, setFout] = useState<string | null>(null)
  const [keuze, setKeuze] = useState<Set<string>>(new Set())

  const [draait, setDraait] = useState(false)
  const [voortgang, setVoortgang] = useState<Voortgang | null>(null)
  const [resultaat, setResultaat] = useState<BulkUpdateResult | null>(null)

  const [mergeBezig, setMergeBezig] = useState<string | null>(null)
  const [mergeUitkomst, setMergeUitkomst] = useState<Record<string, string>>({})

  const laden = useCallback(async () => {
    setBezig(true); setFout(null)
    try {
      const p = await Services.BulkUpdateService.Plan()
      setPlan(p)
      // Voorselectie: alles waar daadwerkelijk iets te doen is.
      setKeuze(new Set((p.projects ?? []).filter(r => !r.skip).map(r => r.projectId)))
    } catch (e) {
      setPlan(null); setFout(foutTekst(e))
    } finally {
      setBezig(false)
    }
  }, [])

  useEffect(() => { void laden() }, [laden])

  useEffect(() => {
    // ev.data komt soms als JSON-string binnen in plaats van als object — zelfde
    // behandeling als in OrgSyncPage, dat hier al tegenaan liep.
    const stop = Events.On('bulkupdate:progress', ev => {
      const data = ev.data
      const ruw = Array.isArray(data) ? data[0] : data
      const parsed: Voortgang | null = typeof ruw === 'string'
        ? (() => { try { return JSON.parse(ruw) as Voortgang } catch { return null } })()
        : (ruw as Voortgang | null)
      if (parsed) setVoortgang(parsed)
    })
    return () => { stop() }
  }, [])

  const rijen = plan?.projects ?? []
  const teDoen = rijen.filter(r => !r.skip)
  const gekozenRijen = teDoen.filter(r => keuze.has(r.projectId))
  const totaalPlugins = gekozenRijen.reduce((n, r) => n + (r.plugins ?? []).length, 0)
  const totaalCore = gekozenRijen.filter(r => r.coreOutdated).length

  const toggle = (projectId: string) => {
    setKeuze(prev => {
      const next = new Set(prev)
      next.has(projectId) ? next.delete(projectId) : next.add(projectId)
      return next
    })
  }

  const allesAan = () => setKeuze(new Set(teDoen.map(r => r.projectId)))
  const allesUit = () => setKeuze(new Set())

  const starten = async () => {
    const ids = gekozenRijen.map(r => r.projectId)
    if (ids.length === 0) return
    const ok = await bevestig(
      `${ids.length} project(en) bijwerken`,
      `Per project gebeurt dit:\n\n` +
      `• openstaand werk gaat automatisch in een stash (je krijgt te zien welke)\n` +
      `• een branch chore/wp-updates-<datum>, afgetakt van origin/<default branch> na een fetch\n` +
      `• een commit per plugin (${totaalPlugins} in totaal) en één voor WordPress core (${totaalCore} project(en))\n` +
      `• de branch wordt gepusht en er komt een pull request op\n` +
      `• daarna gaat de checkout terug naar de branch waar hij op stond, met het geparkeerde werk erop\n\n` +
      `Er wordt niets gemerged — dat blijft per project een aparte knop.`,
    )
    if (!ok) return

    setDraait(true); setFout(null); setResultaat(null); setVoortgang(null); setMergeUitkomst({})
    try {
      const res = await Services.BulkUpdateService.Run(ids)
      setResultaat(res)
      await laden()
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setDraait(false); setVoortgang(null)
    }
  }

  const mergen = async (r: BulkUpdateProjectResult) => {
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
      setMergeUitkomst(h => ({ ...h, [r.projectId]: `✗ ${foutTekst(e)}` }))
    } finally {
      setMergeBezig(null)
    }
  }

  return (
    <div className="flex-1 flex flex-col min-h-0 overflow-hidden bg-bg">
      {/* ── toolbar ── */}
      <div className="h-14 px-[22px] bg-panel border-b border-border shrink-0 flex items-center gap-3">
        <span className="w-8 h-8 flex items-center justify-center rounded-lg bg-hover text-fg-muted shrink-0">
          <CloudDownloadIcon size={16} />
        </span>
        <div className="flex-1 min-w-0">
          <h2 className="text-[15px] font-bold tracking-[-.01em] text-fg leading-tight truncate">
            Alles bijwerken
          </h2>
          <p className="text-[11px] text-fg-faint leading-tight truncate">
            {plan
              ? `${teDoen.length} van ${rijen.length} project(en) achter · referentie-installatie: WordPress ${plan.referenceCore}`
              : 'plugins en WordPress core uit de referentie-installatie'}
          </p>
        </div>
        <button onClick={() => void laden()} disabled={bezig || draait}
          className="px-3 py-1.5 bg-panel border border-border rounded-lg text-[12.5px] font-medium
                     text-fg hover:bg-hover disabled:opacity-50 transition-colors shrink-0 flex items-center gap-1.5">
          <span className={`inline-flex ${bezig ? 'animate-spin' : ''}`}><RefreshIcon size={13} /></span>
          {bezig ? 'Bezig…' : 'Opnieuw bekijken'}
        </button>
        <button onClick={() => void starten()} disabled={draait || bezig || gekozenRijen.length === 0}
          className="px-3 py-1.5 bg-accent hover:bg-accent-2 text-white text-[12.5px] font-semibold
                     rounded-lg disabled:opacity-50 transition-colors shrink-0">
          {draait ? 'Bijwerken…' : `Bijwerken (${gekozenRijen.length})`}
        </button>
      </div>

      <div className="flex-1 overflow-y-auto px-6 py-4">
        {fout && (
          <p className="text-[12.5px] text-red mb-3 whitespace-pre-line bg-red-soft px-3 py-2 rounded-lg">{fout}</p>
        )}

        {draait && (
          <div className="mb-3 bg-panel border border-border rounded-lg px-3 py-2 text-[12px]">
            <div className="flex items-center gap-2">
              <span className="animate-spin inline-block text-fg-faint">↻</span>
              <span className="text-fg">
                {voortgang
                  ? `${voortgang.index}/${voortgang.total} — ${voortgang.projectName || ''} ${voortgang.phase ? `· ${voortgang.phase}` : ''}`
                  : 'starten…'}
              </span>
            </div>
            {voortgang && voortgang.total > 0 && (
              <div className="mt-1.5 h-1 bg-hover rounded-full overflow-hidden">
                <div className="h-full bg-accent transition-all"
                     style={{ width: `${Math.round((voortgang.index / voortgang.total) * 100)}%` }} />
              </div>
            )}
          </div>
        )}

        {/* ── resultaat na een run ── */}
        {resultaat && (
          <div className="mb-5">
            <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2">Resultaat</h3>
            <div className="border border-border rounded-lg divide-y divide-border/60">
              {(resultaat.projects ?? []).map(r => (
                <div key={r.projectId} className="px-3 py-2 text-[12px]">
                  <div className="flex items-center gap-2">
                    <span className={
                      r.status === 'updated' ? 'text-green' : r.status === 'nothing' ? 'text-fg-muted' : 'text-red'
                    }>
                      {r.status === 'updated' ? '✓' : r.status === 'nothing' ? '=' : '✗'}
                    </span>
                    <span className="text-fg font-medium">{r.projectName}</span>
                    {r.branch && <span className="font-mono text-[10.5px] text-fg-faint">{r.branch}</span>}
                    <span className="ml-auto flex items-center gap-2">
                      {r.pullRequestUrl && (
                        <ExternalLink href={r.pullRequestUrl}
                          className="text-accent hover:underline cursor-pointer text-[11.5px]">↗ PR</ExternalLink>
                      )}
                      {r.canMerge && !!r.pullRequestNumber && !mergeUitkomst[r.projectId] && (
                        <button onClick={() => void mergen(r)} disabled={mergeBezig === r.projectId}
                          className="text-[10.5px] font-semibold text-green bg-green-soft border border-green/30
                                     rounded-full px-2 py-px hover:brightness-95 disabled:opacity-50 transition">
                          {mergeBezig === r.projectId ? 'Mergen…' : 'Mergen'}
                        </button>
                      )}
                      {mergeUitkomst[r.projectId] && (
                        <span className={mergeUitkomst[r.projectId].startsWith('✓') ? 'text-green' : 'text-red'}>
                          {mergeUitkomst[r.projectId]}
                        </span>
                      )}
                    </span>
                  </div>

                  <div className="pl-5 mt-0.5 space-y-0.5 text-[11px]">
                    {r.coreStatus === 'updated' && (
                      <p className="text-fg-muted">WordPress {r.coreFrom || '?'} → {r.coreTo}</p>
                    )}
                    {r.coreError && <p className="text-red">core: {r.coreError}</p>}
                    {(r.plugins ?? []).filter(p => p.status === 'updated').length > 0 && (
                      <p className="text-fg-muted">
                        {(r.plugins ?? []).filter(p => p.status === 'updated').length} plugin(s):{' '}
                        {(r.plugins ?? []).filter(p => p.status === 'updated').map(p => p.slug).join(', ')}
                      </p>
                    )}
                    {(r.plugins ?? []).filter(p => p.status === 'error').map(p => (
                      <p key={p.slug} className="text-red">{p.slug}: {p.error}</p>
                    ))}
                    {r.status === 'error' && r.error && <p className="text-red">{r.error}</p>}
                    {r.status === 'nothing' && r.error && <p className="text-fg-faint">{r.error}</p>}
                    {r.stash && (
                      <p className={r.stashRestored ? 'text-fg-faint' : 'text-amber'}>
                        {r.stashRestored
                          ? `werk was gestasht en is teruggezet op ${r.restoredBranch}`
                          : `werk staat nog in de stash: ${r.stash}`}
                      </p>
                    )}
                    {r.restoreError && <p className="text-amber">{r.restoreError}</p>}
                    {r.pullRequestError && (
                      <p className="text-amber">geen PR: {r.pullRequestError} (de commit staat er wel)</p>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* ── voorbeschouwing ── */}
        {!plan && bezig && <p className="text-[12.5px] text-fg-faint">Projecten vergelijken met de referentie-installatie…</p>}

        {plan && (
          <>
            <div className="flex items-center gap-2 mb-2">
              <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase">
                Te doen
              </h3>
              {teDoen.length > 0 && (
                <>
                  <span className="text-[11px] text-fg-faint">
                    · {totaalCore} × core, {totaalPlugins} plugin-update(s) geselecteerd
                  </span>
                  <button onClick={allesAan} className="ml-auto text-[11px] text-accent hover:underline">alles aan</button>
                  <button onClick={allesUit} className="text-[11px] text-fg-muted hover:text-fg">alles uit</button>
                </>
              )}
            </div>

            {teDoen.length === 0 ? (
              <p className="text-[12.5px] text-fg-faint mb-5">
                Alle WordPress-projecten staan op de versies van de referentie-installatie.
              </p>
            ) : (
              <div className="border border-border rounded-lg divide-y divide-border/60 mb-5">
                {teDoen.map(r => (
                  <label key={r.projectId}
                         className="flex items-start gap-2.5 px-3 py-2 hover:bg-hover cursor-pointer transition-colors">
                    <input type="checkbox" checked={keuze.has(r.projectId)} onChange={() => toggle(r.projectId)}
                           disabled={draait} className="mt-[3px] shrink-0 accent-accent" />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="text-[12.5px] text-fg font-medium truncate">{r.projectName}</span>
                        <span className="font-mono text-[10.5px] text-fg-faint truncate">⑂ {r.branch}</span>
                      </div>
                      <div className="mt-0.5 flex flex-wrap items-center gap-1.5 text-[11px]">
                        {r.coreOutdated && (
                          <span className="text-amber bg-amber/10 border border-amber/30 rounded-full px-2 py-px">
                            WordPress {r.coreFrom} → {r.coreTo}
                          </span>
                        )}
                        {(r.plugins ?? []).map(p => (
                          <span key={p.slug} className="font-mono text-fg-muted bg-hover rounded-full px-2 py-px">
                            {p.slug} {p.from}→{p.to}
                          </span>
                        ))}
                      </div>
                    </div>
                  </label>
                ))}
              </div>
            )}

            {/* Overgeslagen projecten blijven zichtbaar met de reden erbij. */}
            {rijen.some(r => r.skip) && (
              <>
                <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2">
                  Overgeslagen
                </h3>
                <div className="border border-border rounded-lg divide-y divide-border/60">
                  {rijen.filter(r => r.skip).map(r => (
                    <div key={r.projectId} className="flex items-center gap-2 px-3 py-1.5 text-[12px]">
                      <span className="text-fg-muted truncate">{r.projectName}</span>
                      <span className="text-[11px] text-fg-faint ml-auto shrink-0">{r.skip}</span>
                    </div>
                  ))}
                </div>
              </>
            )}
          </>
        )}
      </div>
    </div>
  )
}
