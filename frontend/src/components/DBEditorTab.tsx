import { useState, useEffect, useCallback } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { DBEditorInfo, TabelWeergave, QueryUitkomst, AIQueryVoorstel } from '../../bindings/github.com/rdm/sites-tool/internal/services/models'
import type { Tabel, Cel, SleutelWaarde, NieuweWaarde } from '../../bindings/github.com/rdm/sites-tool/internal/adapters/mysqldb/models'
import Foutvak from './Foutvak'
import Tooltip from './Tooltip'
import { bevestig } from '../lib/bevestig'

interface Props { projectId: string }

const PAGINA = 50

function foutTekst(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

function bytesLeesbaar(n: number): string {
  if (!n) return '0 B'
  const eenheden = ['B', 'KB', 'MB', 'GB']
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), eenheden.length - 1)
  return `${(n / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1)} ${eenheden[i]}`
}

// celTekst maakt van een cel iets leesbaars, met NULL zichtbaar anders dan leeg.
function celTekst(c: Cel): string {
  if (c.null) return 'NULL'
  if (c.binair) return c.waarde
  return c.waarde
}

export default function DBEditorTab({ projectId }: Props) {
  const [info, setInfo] = useState<DBEditorInfo | null>(null)
  const [databases, setDatabases] = useState<string[]>([])
  const [dbNaam, setDbNaam] = useState('')
  const [tabellen, setTabellen] = useState<Tabel[]>([])
  const [tabelFilter, setTabelFilter] = useState('')
  const [actieveTabel, setActieveTabel] = useState('')

  const [weergave, setWeergave] = useState<TabelWeergave | null>(null)
  const [offset, setOffset] = useState(0)
  const [sorteer, setSorteer] = useState('')
  const [aflopend, setAflopend] = useState(false)
  const [zoekKolom, setZoekKolom] = useState('')
  const [zoek, setZoek] = useState('')

  const [bezig, setBezig] = useState(false)
  const [fout, setFout] = useState<string | null>(null)
  const [melding, setMelding] = useState<string | null>(null)

  // Bewerken: welke cel staat open, en met welke waarde.
  const [bewerkt, setBewerkt] = useState<{ rij: number; kolom: string; waarde: string } | null>(null)
  const [nieuweRij, setNieuweRij] = useState<Record<string, string> | null>(null)

  const [vraag, setVraag] = useState('')
  const [voorstel, setVoorstel] = useState<AIQueryVoorstel | null>(null)
  const [bouwBezig, setBouwBezig] = useState(false)

  const [sql, setSql] = useState('')
  const [queryUit, setQueryUit] = useState<QueryUitkomst | null>(null)
  const [queryBezig, setQueryBezig] = useState(false)
  const [modus, setModus] = useState<'tabellen' | 'query'>('tabellen')

  useEffect(() => {
    setInfo(null); setDatabases([]); setDbNaam(''); setTabellen([])
    setActieveTabel(''); setWeergave(null); setFout(null); setMelding(null)
    setQueryUit(null); setSql(''); setBewerkt(null); setNieuweRij(null)
    setVraag(''); setVoorstel(null)

    Services.DBEditorService.Info(projectId)
      .then(i => {
        setInfo(i)
        if (i.beschikbaar) setDbNaam(i.database)
      })
      .catch(e => setFout(foutTekst(e)))
  }, [projectId])

  useEffect(() => {
    if (!info?.beschikbaar) return
    Services.DBEditorService.Databases(projectId)
      .then(l => setDatabases(l ?? []))
      .catch(e => setFout(foutTekst(e)))
  }, [projectId, info])

  const tabellenLaden = useCallback(async (db: string) => {
    if (!db) return
    setBezig(true); setFout(null)
    try {
      setTabellen((await Services.DBEditorService.Tabellen(projectId, db)) ?? [])
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setBezig(false)
    }
  }, [projectId])

  useEffect(() => {
    if (dbNaam) void tabellenLaden(dbNaam)
  }, [dbNaam, tabellenLaden])

  const tabelLaden = useCallback(async (tabel: string, nieuweOffset: number) => {
    if (!tabel) return
    setBezig(true); setFout(null); setBewerkt(null); setNieuweRij(null)
    try {
      const w = await Services.DBEditorService.Tabel(projectId, {
        database: dbNaam, tabel, sorteer, aflopend,
        zoekKolom, zoek, limiet: PAGINA, offset: nieuweOffset,
      })
      setWeergave(w)
      setOffset(nieuweOffset)
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setBezig(false)
    }
  }, [projectId, dbNaam, sorteer, aflopend, zoekKolom, zoek])

  useEffect(() => {
    if (actieveTabel) void tabelLaden(actieveTabel, 0)
  }, [actieveTabel, sorteer, aflopend, zoek, zoekKolom, tabelLaden])

  const sleutelVan = (rijIndex: number): SleutelWaarde[] => {
    if (!weergave) return []
    return weergave.tabel.primaryKeys.map(k => {
      const i = weergave.rijen.kolommen.indexOf(k)
      const cel = weergave.rijen.rijen[rijIndex][i]
      return { kolom: k, waarde: cel.waarde, null: cel.null }
    })
  }

  const celOpslaan = async () => {
    if (!bewerkt || !weergave) return
    setBezig(true); setFout(null); setMelding(null)
    try {
      const uit = await Services.DBEditorService.ZetCel(projectId, {
        database: dbNaam, tabel: actieveTabel,
        sleutel: sleutelVan(bewerkt.rij),
        kolom: bewerkt.kolom, waarde: bewerkt.waarde, naarNull: false,
      })
      if (uit.dumpPad) setMelding(`Veiligheidsdump gemaakt: ${uit.dumpPad}`)
      setBewerkt(null)
      await tabelLaden(actieveTabel, offset)
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setBezig(false)
    }
  }

  const celNull = async (rijIndex: number, kolom: string) => {
    setBezig(true); setFout(null)
    try {
      const uit = await Services.DBEditorService.ZetCel(projectId, {
        database: dbNaam, tabel: actieveTabel,
        sleutel: sleutelVan(rijIndex), kolom, waarde: '', naarNull: true,
      })
      if (uit.dumpPad) setMelding(`Veiligheidsdump gemaakt: ${uit.dumpPad}`)
      setBewerkt(null)
      await tabelLaden(actieveTabel, offset)
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setBezig(false)
    }
  }

  const rijVerwijderen = async (rijIndex: number) => {
    const sleutel = sleutelVan(rijIndex)
    const beschrijving = sleutel.map(s => `${s.kolom} = ${s.waarde}`).join(', ')
    if (!(await bevestig('Rij verwijderen', `Deze rij uit ${actieveTabel} verwijderen?\n\n${beschrijving}\n\nEr wordt eerst een dump van de database gemaakt als dat in deze sessie nog niet gebeurd is.`))) return

    setBezig(true); setFout(null)
    try {
      const uit = await Services.DBEditorService.VerwijderRij(projectId, {
        database: dbNaam, tabel: actieveTabel, sleutel, waarden: [],
      })
      if (uit.dumpPad) setMelding(`Veiligheidsdump gemaakt: ${uit.dumpPad}`)
      await tabelLaden(actieveTabel, offset)
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setBezig(false)
    }
  }

  const rijToevoegen = async () => {
    if (!nieuweRij || !weergave) return
    const waarden: NieuweWaarde[] = Object.entries(nieuweRij)
      .filter(([, v]) => v !== '')
      .map(([kolom, waarde]) => ({ kolom, waarde, null: false }))

    setBezig(true); setFout(null)
    try {
      const uit = await Services.DBEditorService.VoegRijToe(projectId, {
        database: dbNaam, tabel: actieveTabel, sleutel: [], waarden,
      })
      if (uit.dumpPad) setMelding(`Veiligheidsdump gemaakt: ${uit.dumpPad}`)
      setNieuweRij(null)
      await tabelLaden(actieveTabel, offset)
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setBezig(false)
    }
  }

  const queryBouwen = async () => {
    if (!vraag.trim()) return
    setBouwBezig(true); setFout(null); setVoorstel(null); setQueryUit(null)
    try {
      const v = await Services.DBEditorService.BouwQuery(projectId, dbNaam, vraag)
      setVoorstel(v)
      // De query komt in de editor te staan, zodat je hem kunt bijschaven
      // voordat je uitvoert.
      if (v.sql) setSql(v.sql)
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setBouwBezig(false)
    }
  }

  const queryUitvoeren = async (bevestigd: boolean) => {
    if (!sql.trim()) return
    setQueryBezig(true); setFout(null); setMelding(null)
    try {
      const uit = await Services.DBEditorService.VoerQueryUit(projectId, dbNaam, sql, bevestigd)
      setQueryUit(uit)

      if (uit.bevestigingNodig) {
        const akkoord = await bevestig(
          'Deze query eerst bevestigen',
          `${uit.beoordeling.reden}\n\n${sql.trim()}\n\nEr wordt eerst een dump van ${dbNaam} gemaakt als dat in deze sessie nog niet gebeurd is.`,
        )
        if (akkoord) {
          const opnieuw = await Services.DBEditorService.VoerQueryUit(projectId, dbNaam, sql, true)
          setQueryUit(opnieuw)
          if (actieveTabel) await tabelLaden(actieveTabel, offset)
          await tabellenLaden(dbNaam)
        }
        return
      }
      if (uit.beoordeling.soort !== 'lezen') {
        if (actieveTabel) await tabelLaden(actieveTabel, offset)
        await tabellenLaden(dbNaam)
      }
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setQueryBezig(false)
    }
  }

  const dumpNu = async () => {
    setBezig(true); setFout(null)
    try {
      const uit = await Services.DBEditorService.MaakDump(projectId, dbNaam)
      setMelding(uit.dumpPad ? `Dump gemaakt: ${uit.dumpPad}` : (uit.melding || 'Er was niets te dumpen.'))
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setBezig(false)
    }
  }

  if (info && !info.beschikbaar) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center gap-3 p-10 text-center">
        <p className="text-[15px] font-semibold text-fg">Geen lokale database</p>
        <p className="text-[13px] text-fg-muted max-w-[420px]">{info.reden}</p>
      </div>
    )
  }
  if (!info) {
    return <div className="flex-1 flex items-center justify-center text-fg-faint text-[13px] italic">Bezig…</div>
  }

  const zichtbareTabellen = tabelFilter
    ? tabellen.filter(t => t.naam.toLowerCase().includes(tabelFilter.toLowerCase()))
    : tabellen

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-hidden">
      <div className="shrink-0 flex items-center gap-2 px-6 py-3 border-b border-border bg-panel flex-wrap">
        <select value={dbNaam} onChange={e => { setDbNaam(e.target.value); setActieveTabel(''); setWeergave(null) }}
          className="bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg">
          {(databases.length ? databases : [info.database]).map(d => (
            <option key={d} value={d}>{d}{d === info.database ? ' (uit .env)' : ''}</option>
          ))}
        </select>

        <div className="flex items-center gap-0.5 bg-panel-2 border border-border rounded-lg p-0.5">
          {(['tabellen', 'query'] as const).map(m => (
            <button key={m} onClick={() => setModus(m)}
              className={`text-[12px] px-2.5 py-1 rounded-md transition ${
                modus === m ? 'bg-accent text-white' : 'text-fg-muted hover:text-fg'}`}>
              {m === 'tabellen' ? 'Tabellen' : 'Query'}
            </button>
          ))}
        </div>

        <span className="text-[11px] text-fg-faint">
          {info.container} · {info.host}:{info.poort} · {info.gebruiker}
        </span>

        <Tooltip label="Maak nu een dump, los van de automatische dump bij de eerste wijziging">
          <button onClick={dumpNu} disabled={bezig}
            className="ml-auto text-[12px] text-fg-muted border border-border rounded-lg px-3 py-1.5 hover:bg-hover transition disabled:opacity-50">
            Dump maken
          </button>
        </Tooltip>
      </div>

      {fout && <Foutvak fout={fout} className="shrink-0 mx-6 mt-3" />}
      {melding && (
        <div className="shrink-0 mx-6 mt-3 bg-green-soft text-green px-3 py-2 rounded-lg text-[11.5px] flex items-start gap-2">
          <span className="flex-1">{melding}</span>
          <button onClick={() => setMelding(null)} className="text-[11px] opacity-70 hover:opacity-100">×</button>
        </div>
      )}

      {modus === 'query' ? (
        <div className="flex-1 min-h-0 overflow-y-auto px-6 py-4">
          {/* AI-querybouwer: een vraag wordt een voorstel, uitvoeren is een
              tweede, aparte klik. */}
          <div className="bg-panel border border-border rounded-xl p-3.5 mb-4">
            <div className="flex items-center gap-2 mb-2">
              <span className="text-[12px] font-semibold text-fg">✨ Vraag het in gewone taal</span>
              <Tooltip label="Alleen het schema gaat naar de AI: tabelnamen, kolomnamen en types. Er wordt geen enkele rij meegestuurd.">
                <span className="text-[10.5px] text-fg-faint cursor-help">wat wordt er verstuurd?</span>
              </Tooltip>
            </div>
            <div className="flex items-start gap-2">
              <input value={vraag} onChange={e => setVraag(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter' && !bouwBezig) void queryBouwen() }}
                placeholder="bijv. welke gebruikers hebben de rol administrator?"
                className="flex-1 bg-panel-2 border border-border rounded-lg px-3 py-2 text-[12.5px] text-fg" />
              <button onClick={queryBouwen} disabled={bouwBezig || !vraag.trim()}
                className="bg-accent text-white text-[12.5px] font-semibold px-4 py-2 rounded-lg hover:brightness-110 disabled:opacity-50 transition whitespace-nowrap">
                {bouwBezig ? <span className="animate-spin inline-block">↻</span> : 'Bouw query'}
              </button>
            </div>

            {voorstel && (
              <div className="mt-3 space-y-2">
                {voorstel.uitleg && (
                  <p className="text-[11.5px] text-fg-muted">{voorstel.uitleg}</p>
                )}
                {(voorstel.aannames ?? []).length > 0 && (
                  <div>
                    <div className="text-[10.5px] font-semibold text-fg-muted mb-1">Aannames van de AI</div>
                    <ul className="space-y-0.5">
                      {(voorstel.aannames ?? []).map((a, i) => (
                        <li key={i} className="text-[11px] text-fg-muted">· {a}</li>
                      ))}
                    </ul>
                  </div>
                )}
                {voorstel.waarschuwing && (
                  <div className="bg-amber-soft text-amber px-3 py-1.5 rounded-lg text-[11px]">{voorstel.waarschuwing}</div>
                )}
                {(voorstel.waarschuwingen ?? []).map((w, i) => (
                  <div key={i} className="bg-amber-soft text-amber px-3 py-1.5 rounded-lg text-[11px]">{w}</div>
                ))}
                <div className="text-[10.5px] text-fg-faint">
                  {voorstel.beoordeling.sleutelwoord && <>oordeel: <span className="font-mono">{voorstel.beoordeling.soort}</span> · </>}
                  {(voorstel.tabellen ?? []).length > 0
                    ? <>tabellen: <span className="font-mono">{(voorstel.tabellen ?? []).join(', ')}</span></>
                    : 'geen bekende tabellen herkend'}
                  {voorstel.sql && ' · de query staat hieronder klaar; nakijken en dan uitvoeren'}
                </div>
              </div>
            )}
          </div>

          <textarea value={sql} onChange={e => setSql(e.target.value)}
            spellCheck={false} rows={7}
            placeholder={`SELECT * FROM wp_options WHERE option_name LIKE 'home%'`}
            className="w-full bg-panel-2 border border-border rounded-xl px-3 py-2.5 text-[12px] font-mono text-fg resize-y" />
          <div className="flex items-center gap-2 mt-2">
            <button onClick={() => void queryUitvoeren(false)} disabled={queryBezig || !sql.trim()}
              className="bg-accent text-white text-[12.5px] font-semibold px-4 py-1.5 rounded-lg hover:brightness-110 disabled:opacity-50 transition">
              {queryBezig ? <span className="animate-spin inline-block">↻</span> : 'Uitvoeren'}
            </button>
            <span className="text-[11px] text-fg-faint">
              Destructieve statements vragen eerst om bevestiging. Eén statement per keer.
            </span>
          </div>

          {queryUit && (
            <div className="mt-4">
              <div className="text-[11.5px] text-fg-muted mb-2">
                {queryUit.beoordeling.sleutelwoord && <span className="font-mono">{queryUit.beoordeling.sleutelwoord}</span>}
                {queryUit.resultaat
                  ? ` · ${queryUit.resultaat.rijen.length} rijen · ${queryUit.duurMs} ms`
                  : queryUit.bevestigingNodig
                    ? ' · wacht op bevestiging'
                    : ` · ${queryUit.geraakt} rijen geraakt · ${queryUit.duurMs} ms`}
              </div>
              {(queryUit.waarschuwingen ?? []).map((w, i) => (
                <div key={i} className="bg-amber-soft text-amber px-3 py-1.5 rounded-lg text-[11px] mb-1.5">{w}</div>
              ))}
              {queryUit.resultaat && <Resultaatgrid kolommen={queryUit.resultaat.kolommen} rijen={queryUit.resultaat.rijen} />}
            </div>
          )}
        </div>
      ) : (
        <div className="flex-1 min-h-0 flex overflow-hidden">
          <div className="w-[260px] shrink-0 border-r border-border flex flex-col min-h-0">
            <div className="shrink-0 px-3 py-2 border-b border-border">
              <input value={tabelFilter} onChange={e => setTabelFilter(e.target.value)}
                placeholder="tabel zoeken…"
                className="w-full bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12px] text-fg" />
            </div>
            <div className="flex-1 min-h-0 overflow-y-auto">
              {zichtbareTabellen.length === 0 ? (
                <div className="text-fg-faint text-[12px] italic p-4 text-center">
                  {bezig ? 'Bezig…' : 'Geen tabellen'}
                </div>
              ) : zichtbareTabellen.map(t => (
                <button key={t.naam} onClick={() => setActieveTabel(t.naam)}
                  className={`w-full text-left px-3 py-2 border-b border-border hover:bg-hover transition ${
                    actieveTabel === t.naam ? 'bg-hover' : ''}`}>
                  <div className="text-[12px] font-mono text-fg truncate">{t.naam}</div>
                  <div className="text-[10.5px] text-fg-faint">
                    ±{t.rijen.toLocaleString('nl-NL')} rijen · {bytesLeesbaar(t.bytes)}
                    {t.primaryKeys.length === 0 && ' · geen PK'}
                  </div>
                </button>
              ))}
            </div>
          </div>

          <div className="flex-1 min-w-0 flex flex-col min-h-0">
            {!weergave ? (
              <div className="flex-1 flex items-center justify-center text-fg-faint text-[13px] italic">
                Kies links een tabel.
              </div>
            ) : (
              <>
                <div className="shrink-0 flex items-center gap-2 px-4 py-2 border-b border-border flex-wrap">
                  <span className="text-[12px] font-mono text-fg">{actieveTabel}</span>
                  <span className="text-[11px] text-fg-muted">
                    {weergave.totaal.toLocaleString('nl-NL')} rijen
                    {weergave.rijen.duurMs ? ` · ${weergave.rijen.duurMs} ms` : ''}
                  </span>

                  <select value={zoekKolom} onChange={e => setZoekKolom(e.target.value)}
                    className="bg-panel-2 border border-border rounded-lg px-2 py-1 text-[11.5px] text-fg">
                    <option value="">zoek in kolom…</option>
                    {weergave.kolommen.map(k => <option key={k.naam} value={k.naam}>{k.naam}</option>)}
                  </select>
                  <input value={zoek} onChange={e => setZoek(e.target.value)} placeholder="waarde"
                    disabled={!zoekKolom}
                    className="w-[150px] bg-panel-2 border border-border rounded-lg px-2 py-1 text-[11.5px] text-fg disabled:opacity-50" />

                  {weergave.bewerkbaar ? (
                    <button onClick={() => setNieuweRij(nieuweRij ? null : {})}
                      className="ml-auto text-[11.5px] text-fg-muted border border-border rounded-lg px-2.5 py-1 hover:bg-hover transition">
                      {nieuweRij ? 'Annuleer' : '+ rij'}
                    </button>
                  ) : (
                    <Tooltip label={weergave.reden}>
                      <span className="ml-auto text-[11px] text-amber cursor-help">alleen-lezen</span>
                    </Tooltip>
                  )}
                </div>

                {!weergave.bewerkbaar && (
                  <div className="shrink-0 mx-4 mt-2 bg-amber-soft text-amber px-3 py-1.5 rounded-lg text-[11px]">
                    {weergave.reden}
                  </div>
                )}

                <div className="flex-1 min-h-0 overflow-auto">
                  <table className="text-[11.5px] border-collapse">
                    <thead className="sticky top-0 bg-panel">
                      <tr>
                        {weergave.bewerkbaar && <th className="border-b border-border px-2 py-1.5 w-8" />}
                        {weergave.rijen.kolommen.map(k => {
                          const kol = weergave.kolommen.find(c => c.naam === k)
                          return (
                            <th key={k} className="border-b border-border px-2 py-1.5 text-left whitespace-nowrap">
                              <button onClick={() => {
                                if (sorteer === k) setAflopend(!aflopend)
                                else { setSorteer(k); setAflopend(false) }
                              }} className="font-semibold text-fg hover:text-accent transition">
                                {k}{sorteer === k ? (aflopend ? ' ↓' : ' ↑') : ''}
                              </button>
                              <div className="text-[10px] text-fg-faint font-normal">
                                {kol?.type}{kol?.isPk ? ' · PK' : ''}
                              </div>
                            </th>
                          )
                        })}
                      </tr>
                    </thead>
                    <tbody>
                      {nieuweRij && (
                        <tr className="bg-accent-soft">
                          <td className="border-b border-border px-2 py-1">
                            <button onClick={rijToevoegen} disabled={bezig}
                              className="text-accent text-[11px] font-semibold">✓</button>
                          </td>
                          {weergave.rijen.kolommen.map(k => {
                            const kol = weergave.kolommen.find(c => c.naam === k)
                            return (
                              <td key={k} className="border-b border-border px-1 py-1">
                                <input value={nieuweRij[k] ?? ''}
                                  onChange={e => setNieuweRij({ ...nieuweRij, [k]: e.target.value })}
                                  placeholder={kol?.autoIncr ? 'auto' : (kol?.nullable ? 'NULL' : '')}
                                  className="w-full min-w-[90px] bg-panel border border-border rounded px-1.5 py-0.5 text-[11px] font-mono text-fg" />
                              </td>
                            )
                          })}
                        </tr>
                      )}

                      {weergave.rijen.rijen.map((rij, ri) => (
                        <tr key={ri} className="hover:bg-hover">
                          {weergave.bewerkbaar && (
                            <td className="border-b border-border px-2 py-1 align-top">
                              <button onClick={() => rijVerwijderen(ri)} disabled={bezig}
                                title="rij verwijderen"
                                className="text-fg-faint hover:text-red transition text-[11px]">×</button>
                            </td>
                          )}
                          {rij.map((cel, ci) => {
                            const kolom = weergave.rijen.kolommen[ci]
                            const inBewerking = bewerkt?.rij === ri && bewerkt?.kolom === kolom
                            return (
                              <td key={ci} className="border-b border-border px-2 py-1 align-top max-w-[380px]">
                                {inBewerking ? (
                                  <div className="flex items-center gap-1">
                                    <input autoFocus value={bewerkt.waarde}
                                      onChange={e => setBewerkt({ ...bewerkt, waarde: e.target.value })}
                                      onKeyDown={e => {
                                        if (e.key === 'Enter') void celOpslaan()
                                        if (e.key === 'Escape') setBewerkt(null)
                                      }}
                                      className="w-full min-w-[120px] bg-panel border border-accent rounded px-1.5 py-0.5 text-[11px] font-mono text-fg" />
                                    <button onClick={celOpslaan} className="text-accent text-[11px]">✓</button>
                                    <button onClick={() => setBewerkt(null)} className="text-fg-faint text-[11px]">×</button>
                                    <Tooltip label="Zet deze cel op NULL">
                                      <button onClick={() => celNull(ri, kolom)} className="text-fg-faint text-[10px]">∅</button>
                                    </Tooltip>
                                  </div>
                                ) : (
                                  <button
                                    onClick={() => {
                                      if (!weergave.bewerkbaar) return
                                      setBewerkt({ rij: ri, kolom, waarde: cel.null ? '' : cel.waarde })
                                    }}
                                    className={`text-left font-mono block truncate max-w-[360px] ${
                                      cel.null ? 'text-fg-faint italic' : cel.binair ? 'text-amber' : 'text-fg-muted'
                                    } ${weergave.bewerkbaar ? 'hover:text-fg cursor-text' : 'cursor-default'}`}>
                                    {celTekst(cel) || ' '}
                                    {cel.afgekapt && <span className="text-fg-faint"> …</span>}
                                  </button>
                                )}
                              </td>
                            )
                          })}
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>

                <div className="shrink-0 flex items-center gap-2 px-4 py-2 border-t border-border">
                  <button onClick={() => tabelLaden(actieveTabel, Math.max(0, offset - PAGINA))}
                    disabled={offset === 0 || bezig}
                    className="text-[11.5px] text-fg-muted border border-border rounded-lg px-2.5 py-1 hover:bg-hover transition disabled:opacity-40">
                    ← vorige
                  </button>
                  <span className="text-[11px] text-fg-muted">
                    {weergave.totaal === 0 ? '0' : `${offset + 1}–${Math.min(offset + PAGINA, weergave.totaal)}`} van {weergave.totaal.toLocaleString('nl-NL')}
                  </span>
                  <button onClick={() => tabelLaden(actieveTabel, offset + PAGINA)}
                    disabled={offset + PAGINA >= weergave.totaal || bezig}
                    className="text-[11.5px] text-fg-muted border border-border rounded-lg px-2.5 py-1 hover:bg-hover transition disabled:opacity-40">
                    volgende →
                  </button>
                  {weergave.bewerkbaar && (
                    <span className="ml-auto text-[10.5px] text-fg-faint">
                      klik op een cel om te bewerken · Enter opslaan · Esc annuleren
                    </span>
                  )}
                </div>
              </>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function Resultaatgrid({ kolommen, rijen }: { kolommen: string[]; rijen: Cel[][] }) {
  if (rijen.length === 0) {
    return <div className="text-fg-faint text-[12px] italic py-6 text-center">Geen rijen.</div>
  }
  return (
    <div className="overflow-auto border border-border rounded-xl max-h-[480px]">
      <table className="text-[11.5px] border-collapse">
        <thead className="sticky top-0 bg-panel">
          <tr>
            {kolommen.map(k => (
              <th key={k} className="border-b border-border px-2 py-1.5 text-left whitespace-nowrap font-semibold text-fg">{k}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rijen.map((rij, ri) => (
            <tr key={ri} className="hover:bg-hover">
              {rij.map((cel, ci) => (
                <td key={ci} className={`border-b border-border px-2 py-1 align-top font-mono max-w-[380px] truncate ${
                  cel.null ? 'text-fg-faint italic' : cel.binair ? 'text-amber' : 'text-fg-muted'}`}>
                  {celTekst(cel) || ' '}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
