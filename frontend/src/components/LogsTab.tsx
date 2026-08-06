import { useState, useEffect } from 'react'
import { Events } from '@wailsio/runtime'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { SiteDetails } from '../../bindings/github.com/rdm/sites-tool/internal/adapters/kinsta/models'
import type { LogFetchResult, LogGroup, AIFixResult, AIFixPreview } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import Foutvak from './Foutvak'
import Tooltip from './Tooltip'
import ExternalLink from './ExternalLink'
import { bevestig } from '../lib/bevestig'

interface Props { projectId: string }

interface FixProgress {
  groupId: string
  phase: string
  detail: string
}

const LOGBESTANDEN: { id: string; label: string; uitleg: string }[] = [
  { id: 'error', label: 'Errors', uitleg: 'error.log — PHP-fouten en meldingen van de webserver' },
  { id: 'access', label: 'Access', uitleg: 'access.log — elk verzoek aan de site' },
  { id: 'kinsta-cache-perf', label: 'Cache', uitleg: 'kinsta-cache-perf.log — cache-hits en responstijden' },
]

function foutTekst(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

function tijd(waarde: unknown): string {
  if (!waarde) return '—'
  const d = new Date(waarde as string)
  if (isNaN(d.getTime())) return '—'
  return d.toLocaleString('nl-NL', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' })
}

// soortStijl geeft per soort melding een badge-kleur. Botruis krijgt bewust een
// vlakke kleur: het is geen probleem, alleen achtergrondgeluid.
function soortStijl(kind: string): { label: string; klasse: string } {
  switch (kind) {
    case 'php_fatal': return { label: 'FATAL', klasse: 'bg-red-soft text-red' }
    case 'php_warning': return { label: 'WARNING', klasse: 'bg-amber-soft text-amber' }
    case 'php_deprecated': return { label: 'DEPRECATED', klasse: 'bg-amber-soft text-amber' }
    case 'php_notice': return { label: 'NOTICE', klasse: 'bg-panel-2 text-fg-muted' }
    case 'php_other': return { label: 'PHP', klasse: 'bg-amber-soft text-amber' }
    case 'nginx': return { label: 'NGINX', klasse: 'bg-panel-2 text-fg-muted' }
    case 'bot_probe': return { label: 'BOT', klasse: 'bg-panel-2 text-fg-faint' }
    case 'access': return { label: 'ACCESS', klasse: 'bg-panel-2 text-fg-faint' }
    default: return { label: 'ONBEKEND', klasse: 'bg-panel-2 text-fg-faint' }
  }
}

export default function LogsTab({ projectId }: Props) {
  const [site, setSite] = useState<SiteDetails | null>(null)
  const [envId, setEnvId] = useState('')
  const [bestand, setBestand] = useState('error')
  const [regels, setRegels] = useState(1000)
  const [verbergRuis, setVerbergRuis] = useState(true)

  const [result, setResult] = useState<LogFetchResult | null>(null)
  const [bezig, setBezig] = useState(false)
  const [fout, setFout] = useState<string | null>(null)
  const [open, setOpen] = useState<Set<string>>(new Set())

  const [preview, setPreview] = useState<AIFixPreview | null>(null)
  const [previewBezig, setPreviewBezig] = useState<string | null>(null)
  const [fixBezig, setFixBezig] = useState<string | null>(null)
  const [progress, setProgress] = useState<FixProgress | null>(null)
  const [fixResult, setFixResult] = useState<AIFixResult | null>(null)

  useEffect(() => {
    setSite(null); setResult(null); setFout(null); setEnvId('')
    setPreview(null); setFixResult(null); setProgress(null); setOpen(new Set())

    Services.KinstaService.GetLinkedSiteID(projectId)
      .then(id => (id ? Services.KinstaService.GetSiteDetails(id).then(setSite) : undefined))
      .catch(e => setFout(foutTekst(e)))
  }, [projectId])

  useEffect(() => {
    if (!envId && site?.environments?.length) {
      const live = site.environments.find(e => e.name === 'live') ?? site.environments[0]
      setEnvId(live.id)
    }
  }, [site, envId])

  const ophalen = async () => {
    setBezig(true); setFout(null)
    try {
      setResult(await Services.LogService.Fetch(projectId, envId, bestand, regels))
      setOpen(new Set())
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setBezig(false)
    }
  }

  const toggle = (id: string) => {
    setOpen(huidig => {
      const volgende = new Set(huidig)
      if (volgende.has(id)) volgende.delete(id)
      else volgende.add(id)
      return volgende
    })
  }

  const previewOpenen = async (groep: LogGroup) => {
    setFout(null); setFixResult(null)
    // De regel meteen openklappen: het paneel komt daarbinnen te staan, dus
    // zonder dit lijkt de knop niets te doen.
    setOpen(huidig => new Set(huidig).add(groep.id))
    setPreviewBezig(groep.id)
    try {
      setPreview(await Services.LogService.FixPreview(projectId, groep.id))
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setPreviewBezig(null)
    }
  }

  const fixStarten = async (groep: LogGroup) => {
    const branch = preview?.branch ?? `fix/log-${groep.id}`
    const bevestigd = await bevestig(
      'AI laten oplossen en een pull request openen',
      `Er wordt een branch ${branch} gemaakt vanaf de default branch, een AI-agent past de code aan in een aparte worktree, ` +
      `en bij een geslaagde syntaxcontrole wordt er gecommit, gepusht en een draft pull request geopend op GitHub.\n\n` +
      `Je eigen checkout blijft ongemoeid. Op productie wordt niets gewijzigd.\n\n` +
      `Faalt een controle — een wijziging in WordPress core, een kapotte php -l, of geen wijziging — dan stopt het daar ` +
      `en wordt er niets gepusht.`,
    )
    if (!bevestigd) return

    setFixBezig(groep.id); setProgress(null); setFixResult(null); setFout(null); setPreview(null)
    const stopListening = Events.On(`logs:${projectId}:fix`, ev => {
      const data = ev.data
      const parsed: FixProgress | null = typeof data === 'string'
        ? (() => { try { return JSON.parse(data) } catch { return null } })()
        : (data as FixProgress | null)
      if (parsed) setProgress(parsed)
    })

    try {
      setFixResult(await Services.LogFixService.Fix(projectId, groep.id))
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      stopListening()
      setFixBezig(null)
    }
  }

  if (!site) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center gap-3 p-10 text-center">
        <p className="text-[15px] font-semibold text-fg">Geen Kinsta-site gekoppeld</p>
        <p className="text-[13px] text-fg-muted max-w-[380px]">
          Koppel dit project eerst aan een Kinsta-site via het Kinsta-tabblad; de logs komen van de Kinsta-API.
        </p>
        {fout && <p className="text-[12px] text-red">{fout}</p>}
      </div>
    )
  }

  const groepen = result?.groups ?? []
  const zichtbaar = verbergRuis ? groepen.filter(g => g.kind !== 'bot_probe' && g.kind !== 'access') : groepen
  const verborgen = groepen.length - zichtbaar.length
  const kandidaten = groepen.filter(g => g.aiEligible).length

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-hidden">
      <div className="shrink-0 flex items-center gap-2 px-6 py-3 border-b border-border bg-panel flex-wrap">
        <select value={envId} onChange={e => setEnvId(e.target.value)}
          className="bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg">
          {(site.environments ?? []).map(env => (
            <option key={env.id} value={env.id}>{env.display_name || env.name}</option>
          ))}
        </select>

        <div className="flex items-center gap-0.5 bg-panel-2 border border-border rounded-lg p-0.5">
          {LOGBESTANDEN.map(b => (
            <Tooltip key={b.id} label={b.uitleg}>
              <button onClick={() => setBestand(b.id)}
                className={`text-[12px] px-2.5 py-1 rounded-md transition ${
                  bestand === b.id ? 'bg-accent text-white' : 'text-fg-muted hover:text-fg'}`}>
                {b.label}
              </button>
            </Tooltip>
          ))}
        </div>

        <Tooltip label="Aantal regels dat Kinsta teruggeeft (maximaal 20.000)">
          <input type="number" min={1} max={20000} value={regels}
            onChange={e => setRegels(Math.max(1, Math.min(20000, Number(e.target.value) || 1000)))}
            className="w-[86px] bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg" />
        </Tooltip>

        <button onClick={ophalen} disabled={bezig || !envId || fixBezig !== null}
          className="text-[12.5px] text-fg-muted border border-border rounded-lg px-3 py-1.5 hover:bg-hover transition disabled:opacity-50">
          {bezig ? <span className="animate-spin inline-block">↻</span> : 'Logs ophalen'}
        </button>

        {groepen.length > 0 && (
          <label className="ml-auto flex items-center gap-1.5 text-[11.5px] text-fg-muted cursor-pointer">
            <input type="checkbox" checked={verbergRuis} onChange={e => setVerbergRuis(e.target.checked)} />
            botverkeer verbergen{verborgen > 0 ? ` (${verborgen})` : ''}
          </label>
        )}
      </div>

      {fout && <Foutvak fout={fout} className="shrink-0 mx-6 mt-3" />}

      <div className="flex-1 min-h-0 overflow-y-auto px-6 py-4">
        {result === null ? (
          <div className="text-fg-faint text-[13px] italic py-10 text-center">
            Druk op “Logs ophalen” om het gekozen logbestand van deze omgeving te bekijken.
          </div>
        ) : (
          <>
            <p className="text-[11.5px] text-fg-muted mb-3">
              {result.linesReceived.toLocaleString('nl-NL')} regels · {groepen.length} meldingen na groeperen ·{' '}
              {kandidaten > 0
                ? <span className="text-fg">{kandidaten} met code in deze checkout</span>
                : <span>geen enkele melding wijst naar code in deze checkout</span>}
              <span className="text-fg-faint"> · opgehaald {tijd(result.fetchedAt)}</span>
            </p>

            {(result.warnings ?? []).length > 0 && (
              <div className="space-y-1.5 mb-4">
                {(result.warnings ?? []).map((w, i) => (
                  <div key={i} className="bg-amber-soft text-amber px-3 py-1.5 rounded-lg text-[11px]">{w}</div>
                ))}
              </div>
            )}

            {zichtbaar.length === 0 ? (
              <div className="text-fg-faint text-[13px] italic py-10 text-center">
                {groepen.length === 0
                  ? 'Dit logbestand is leeg. Bij een rustige site is dat normaal.'
                  : 'Alleen botverkeer gevonden. Haal het vinkje weg om het te zien.'}
              </div>
            ) : (
              <div className="bg-panel border border-border rounded-xl divide-y divide-border overflow-hidden">
                {zichtbaar.map(g => {
                  const stijl = soortStijl(g.kind)
                  const uit = open.has(g.id)
                  return (
                    <div key={g.id}>
                      <div className="flex items-start gap-3 px-4 py-3 hover:bg-hover transition">
                        <button onClick={() => toggle(g.id)} className="text-fg-faint hover:text-fg text-[11px] mt-0.5 w-3 shrink-0">
                          {uit ? '▾' : '▸'}
                        </button>
                        <span className={`shrink-0 text-[10px] font-semibold px-1.5 py-0.5 rounded ${stijl.klasse}`}>
                          {stijl.label}
                        </span>
                        <div className="flex-1 min-w-0">
                          <button onClick={() => toggle(g.id)} className="block text-left w-full">
                            <span className="text-[12.5px] text-fg break-words">{g.title}</span>
                          </button>
                          <div className="text-[11px] text-fg-muted mt-1 flex flex-wrap gap-x-3 gap-y-0.5">
                            <span>{g.count}×</span>
                            <span>{tijd(g.first)} – {tijd(g.last)}</span>
                            {g.repoPath
                              ? <span className="font-mono text-fg-muted">{g.repoPath}{g.line > 0 ? `:${g.line}` : ''}</span>
                              : g.file
                                ? <span className="font-mono text-fg-faint">{g.file}{g.line > 0 ? `:${g.line}` : ''}</span>
                                : null}
                            {g.isCore && <span className="text-fg-faint">core</span>}
                            {g.hasPii && (
                              <Tooltip label="Deze regels bevatten persoonsgegevens; die worden gemaskeerd voordat er iets naar de AI gaat">
                                <span className="text-amber">persoonsgegevens</span>
                              </Tooltip>
                            )}
                          </div>
                        </div>

                        <div className="shrink-0">
                          {g.aiEligible ? (
                            <button onClick={() => previewOpenen(g)} disabled={fixBezig !== null || previewBezig !== null}
                              className="bg-accent text-white text-[11.5px] font-semibold px-3 py-1.5 rounded-lg hover:brightness-110 disabled:opacity-50 transition">
                              {fixBezig === g.id || previewBezig === g.id
                                ? <span className="animate-spin inline-block">↻</span>
                                : '✨ Oplossen met AI'}
                            </button>
                          ) : (
                            // Klikbaar, niet alleen een tooltip: een element dat bij
                            // klikken niets doet leest als een kapotte knop.
                            <Tooltip label={g.aiReason}>
                              <button onClick={() => toggle(g.id)}
                                className="text-[11px] text-fg-faint border border-border rounded-lg px-2.5 py-1.5 hover:bg-hover hover:text-fg-muted transition">
                                geen AI-fix · waarom?
                              </button>
                            </Tooltip>
                          )}
                        </div>
                      </div>

                      {uit && (
                        <div className="px-4 pb-3 pl-[42px] space-y-2">
                          <p className="text-[11px] text-fg-muted">{g.aiReason}</p>
                          {(g.samples ?? []).map((s, i) => (
                            <pre key={i} className="bg-panel-2 border border-border rounded-lg p-2.5 text-[10.5px] font-mono text-fg-muted whitespace-pre-wrap break-all">
                              {s.raw}
                            </pre>
                          ))}
                          {(g.samples ?? [])[0]?.stack && (
                            <div>
                              <div className="text-[11px] font-semibold text-fg mb-1">Stacktrace</div>
                              <pre className="bg-panel-2 border border-border rounded-lg p-2.5 text-[10.5px] font-mono text-fg-muted whitespace-pre-wrap break-all">
                                {(g.samples ?? [])[0].stack}
                              </pre>
                            </div>
                          )}

                          {previewBezig === g.id && (
                            <p className="text-[11.5px] text-fg-muted">
                              <span className="animate-spin inline-block mr-1.5">↻</span>
                              opdracht voor de AI samenstellen…
                            </p>
                          )}

                          {/* Het paneel staat hier, binnen de aangeklikte regel, en niet
                              onderaan de lijst: daar viel het buiten beeld en leek de knop
                              niets te doen. */}
                          {preview?.groupId === g.id && (
                            <div className="bg-panel-2 border border-accent rounded-xl p-3.5">
                              <div className="flex items-center gap-2 mb-2">
                                <div className="text-[12px] font-semibold text-fg">Dit gaat er naar de AI</div>
                                <button onClick={() => setPreview(null)}
                                  className="ml-auto text-[11px] text-fg-muted hover:text-fg">sluiten</button>
                              </div>
                              <p className="text-[11px] text-fg-muted mb-2">
                                Branch <span className="font-mono text-fg">{preview.branch}</span>
                                {(preview.masked ?? []).length > 0 && (
                                  <> · gemaskeerd: <span className="text-amber">{(preview.masked ?? []).join(', ')}</span></>
                                )}
                              </p>
                              <pre className="bg-panel border border-border rounded-lg p-2.5 max-h-[260px] overflow-y-auto text-[10.5px] font-mono text-fg-muted whitespace-pre-wrap break-words">
                                {preview.prompt}
                              </pre>
                              <button onClick={() => void fixStarten(g)}
                                className="mt-3 bg-accent text-white text-[12.5px] font-semibold px-4 py-1.5 rounded-lg hover:brightness-110 transition">
                                Doorgaan: branch, fix, controle en draft PR
                              </button>
                            </div>
                          )}

                          {progress && fixBezig === g.id && (
                            <div className="bg-panel-2 border border-border rounded-xl p-3.5">
                              <div className="text-[12px] font-semibold text-fg mb-1">
                                <span className="animate-spin inline-block mr-1.5">↻</span>{progress.phase}
                              </div>
                              <div className="text-[11.5px] text-fg-muted whitespace-pre-wrap break-words">{progress.detail}</div>
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            )}
          </>
        )}


        {fixResult && (
          <div className={`bg-panel border rounded-xl p-4 mt-4 ${fixResult.blocked ? 'border-amber' : 'border-green'}`}>
            <div className="text-[12px] font-semibold text-fg mb-2">
              {fixResult.blocked ? 'Gestopt door een controle — er is niets gepusht' : 'Klaar'}
            </div>

            {fixResult.blocked && (
              <div className="bg-amber-soft text-amber px-3 py-2 rounded-lg text-[11.5px] mb-3">
                {fixResult.blockReason}
              </div>
            )}

            <div className="text-[11.5px] text-fg-muted space-y-1">
              <div>Branch: <span className="font-mono text-fg">{fixResult.branch}</span> (vanaf {fixResult.baseRef})</div>
              {fixResult.commitHash && <div>Commit: <span className="font-mono">{fixResult.commitHash}</span></div>}
              {(fixResult.changedFiles ?? []).length > 0 && (
                <div>
                  Gewijzigd:{' '}
                  <span className="font-mono">{(fixResult.changedFiles ?? []).join(', ')}</span>
                </div>
              )}
              {fixResult.pullRequestUrl && (
                <div>
                  Pull request:{' '}
                  <ExternalLink href={fixResult.pullRequestUrl} className="text-accent hover:underline">
                    {fixResult.pullRequestUrl}
                  </ExternalLink>
                </div>
              )}
            </div>

            {fixResult.aiSummary && (
              <div className="mt-3">
                <div className="text-[11px] font-semibold text-fg mb-1">Wat de AI zegt</div>
                <div className="text-[11.5px] text-fg-muted whitespace-pre-wrap break-words">{fixResult.aiSummary}</div>
              </div>
            )}

            {fixResult.lintOutput && (
              <div className="mt-3">
                <div className="text-[11px] font-semibold text-fg mb-1">Syntaxcontrole</div>
                <pre className="bg-panel-2 border border-border rounded-lg p-2.5 text-[10.5px] font-mono text-fg-muted whitespace-pre-wrap break-all">
                  {fixResult.lintOutput}
                </pre>
              </div>
            )}

            {(fixResult.warnings ?? []).length > 0 && (
              <div className="mt-3 space-y-1.5">
                {(fixResult.warnings ?? []).map((w, i) => (
                  <div key={i} className="bg-amber-soft text-amber px-3 py-1.5 rounded-lg text-[11px]">{w}</div>
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
