import { useState } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { SiteDetails } from '../../bindings/github.com/rdm/sites-tool/internal/adapters/kinsta/models'
import type { AnonymiseCfg, SensitiveDataReport, SensitiveTable } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import Foutvak from './Foutvak'

interface Props {
  projectId: string
  cfg: AnonymiseCfg
  onChange: (cfg: AnonymiseCfg) => void
}

const CATEGORIE_LABEL: Record<string, string> = {
  formulieren: 'Formulierinzendingen',
  webshop: 'Webshop (klant- en orderdata)',
  nieuwsbrief: 'Nieuwsbrief en abonnees',
  logboek: 'Logboeken met IP-adressen',
  overig: 'Overig',
}

function foutTekst(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

const veldClass = 'w-full bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg font-mono'

export default function AvgPaneel({ projectId, cfg, onChange }: Props) {
  const [site, setSite] = useState<SiteDetails | null>(null)
  const [rapport, setRapport] = useState<SensitiveDataReport | null>(null)
  const [bezig, setBezig] = useState(false)
  const [fout, setFout] = useState<string | null>(null)

  const zet = <K extends keyof AnonymiseCfg>(veld: K, waarde: AnonymiseCfg[K]) => {
    onChange({ ...cfg, [veld]: waarde })
  }

  const inspecteer = async () => {
    setBezig(true); setFout(null)
    try {
      // De omgeving pakken we hier zelf op: dit paneel staat in de instellingen,
      // waar geen omgevingskeuze staat, en de live-omgeving is wat je wilt weten.
      let s = site
      if (!s) {
        const id = await Services.KinstaService.GetLinkedSiteID(projectId)
        if (!id) throw new Error('Dit project is nog niet aan een Kinsta-site gekoppeld.')
        s = await Services.KinstaService.GetSiteDetails(id)
        setSite(s)
      }
      const envs = s?.environments ?? []
      const live = envs.find(e => e.name === 'live') ?? envs[0]
      if (!live) throw new Error('Geen omgevingen gevonden voor deze Kinsta-site.')

      const r = await Services.DBCloneService.InspectSensitiveData(projectId, live.id)
      setRapport(r)

      // Bij een eerste inspectie alles aanvinken: veilig is de default, en je
      // kunt daarna gericht uitvinken wat je wél nodig hebt.
      if ((cfg.emptyTables ?? []).length === 0) {
        onChange({ ...cfg, emptyTables: (r.tables ?? []).map(t => t.name) })
      }
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setBezig(false)
    }
  }

  const gekozenTabellen = new Set(cfg.emptyTables ?? [])
  const toggleTabel = (naam: string) => {
    const volgende = new Set(gekozenTabellen)
    if (volgende.has(naam)) volgende.delete(naam)
    else volgende.add(naam)
    zet('emptyTables', Array.from(volgende))
  }

  const gekozenRollen = new Set(cfg.keepRoles ?? [])
  const toggleRol = (rol: string) => {
    const volgende = new Set(gekozenRollen)
    if (volgende.has(rol)) volgende.delete(rol)
    else volgende.add(rol)
    zet('keepRoles', Array.from(volgende))
  }

  // Tabellen gegroepeerd per categorie, zodat de lijst leesbaar blijft.
  const perCategorie = new Map<string, SensitiveTable[]>()
  for (const t of rapport?.tables ?? []) {
    const lijst = perCategorie.get(t.category) ?? []
    lijst.push(t)
    perCategorie.set(t.category, lijst)
  }

  return (
    <div className="bg-panel border border-border rounded-xl p-4 mb-4">
      <label className="flex items-center gap-2 text-[12px] font-semibold text-fg mb-1 cursor-pointer">
        <input type="checkbox" checked={cfg.enabled} onChange={e => zet('enabled', e.target.checked)} />
        Anonimiseren volgens de AVG
      </label>
      <p className="text-[10.5px] text-fg-faint mb-3 max-w-[620px]">
        Staat dit uit, dan komt de kloon met álle persoonsgegevens uit productie op je machine te
        staan. De kloon meldt dat dan expliciet in het resultaat, zodat het nooit stil gebeurt.
      </p>

      {!cfg.enabled ? (
        <div className="bg-amber-soft text-amber px-3 py-2 rounded-lg text-[11.5px]">
          Anonimisatie staat uit voor dit project.
        </div>
      ) : (
        <>
          <div className="bg-panel-2 border border-border rounded-lg px-3 py-2 mb-4 text-[10.5px] text-fg-muted">
            De anonimisatie gebeurt lokaal, direct na de import. De persoonsgegevens staan dus even in
            de gedownloade dump en in de database voordat ze worden verwijderd — dat is inherent aan
            deze aanpak. Mislukt het opschonen, dan faalt de kloon met een expliciete waarschuwing in
            plaats van stil door te gaan.
          </div>

          {fout && <Foutvak fout={fout} className="mb-3" />}

          {/* ── Gebruikers ─────────────────────────────────────────────── */}
          <div className="mb-4">
            <label className="flex items-center gap-2 text-[11.5px] font-semibold text-fg mb-2 cursor-pointer">
              <input type="checkbox" checked={cfg.anonymiseUsers}
                onChange={e => zet('anonymiseUsers', e.target.checked)} />
              Gebruikers anonimiseren
            </label>
            {cfg.anonymiseUsers && (
              <div className="pl-5">
                <p className="text-[10.5px] text-fg-faint mb-2 max-w-[600px]">
                  E-mail, login, naam en wachtwoordhash worden vervangen door onschadelijke waarden.
                  Accounts blijven bestaan, dus auteurschap en relaties blijven intact. Kies hieronder
                  wie zijn echte gegevens houdt — zonder uitzondering kun je lokaal niet inloggen.
                </p>

                <div className="text-[11px] text-fg-muted mb-1">Rollen die hun gegevens houden</div>
                {rapport?.roles?.length ? (
                  <div className="flex flex-wrap gap-1.5 mb-2">
                    {rapport.roles.map(rol => (
                      <button key={rol} onClick={() => toggleRol(rol)}
                        className={`text-[11px] px-2 py-1 rounded-md border transition ${
                          gekozenRollen.has(rol)
                            ? 'bg-accent-soft border-accent text-accent font-semibold'
                            : 'bg-panel-2 border-border text-fg-muted hover:bg-hover'
                        }`}>
                        {rol}
                      </button>
                    ))}
                  </div>
                ) : (
                  <p className="text-[11px] text-fg-faint italic mb-2">
                    Inspecteer de site om de rollen van deze installatie te zien.
                  </p>
                )}

                <div className="text-[11px] text-fg-muted mb-1">
                  Losse accounts die hun gegevens houden (logins, komma-gescheiden)
                </div>
                <input
                  value={(cfg.keepUserLogins ?? []).join(', ')}
                  onChange={e => zet('keepUserLogins', e.target.value.split(',').map(s => s.trim()).filter(Boolean))}
                  placeholder="jeffrey, beheerder"
                  className={veldClass} />
              </div>
            )}
          </div>

          {/* ── Reacties ───────────────────────────────────────────────── */}
          <label className="flex items-center gap-2 text-[11.5px] font-semibold text-fg mb-1 cursor-pointer">
            <input type="checkbox" checked={cfg.anonymiseComments}
              onChange={e => zet('anonymiseComments', e.target.checked)} />
            Reacties anonimiseren
          </label>
          <p className="text-[10.5px] text-fg-faint mb-4 pl-5">
            Naam, e-mailadres, website, IP-adres en browserinformatie van reageerders worden gewist.
          </p>

          {/* ── Gevoelige tabellen ─────────────────────────────────────── */}
          <div className="flex items-center gap-2.5 mb-2">
            <div className="text-[11.5px] font-semibold text-fg">Tabellen die worden geleegd</div>
            <button onClick={inspecteer} disabled={bezig}
              className="text-[11px] text-fg-muted border border-border rounded-lg px-2.5 py-1 hover:bg-hover transition disabled:opacity-50">
              {bezig ? <span className="animate-spin inline-block">↻</span> : rapport ? 'Opnieuw inspecteren' : 'Site inspecteren'}
            </button>
          </div>

          {!rapport ? (
            <p className="text-[11.5px] text-fg-faint italic">
              Inspecteer de productiedatabase om te zien welke tabellen met persoonsgegevens deze site
              werkelijk heeft — dat is per site anders, afhankelijk van de plugins.
            </p>
          ) : (rapport.tables ?? []).length === 0 ? (
            <p className="text-[11.5px] text-fg-muted">
              Geen bekende tabellen met persoonsgegevens gevonden in {rapport.allTables?.length ?? 0} tabellen.
            </p>
          ) : (
            <>
              <p className="text-[10.5px] text-fg-faint mb-2">
                Het schema blijft staan, alleen de inhoud gaat eruit — zo valt geen plugin om over een
                ontbrekende tabel. Aantallen zijn schattingen uit de database.
              </p>
              {Array.from(perCategorie.entries()).map(([cat, tabellen]) => (
                <div key={cat} className="mb-3">
                  <div className="text-[10px] uppercase tracking-wide text-fg-faint mb-1">
                    {CATEGORIE_LABEL[cat] ?? cat}
                  </div>
                  <div className="bg-panel-2 border border-border rounded-lg divide-y divide-border overflow-hidden">
                    {(tabellen ?? []).map(t => (
                      <label key={t.name}
                        className="flex items-start gap-2.5 px-3 py-2 cursor-pointer hover:bg-hover transition">
                        <input type="checkbox" className="mt-0.5"
                          checked={gekozenTabellen.has(t.name)}
                          onChange={() => toggleTabel(t.name)} />
                        <span className="flex-1 min-w-0">
                          <span className="font-mono text-[11.5px] text-fg">{t.name}</span>
                          <span className="block text-[10.5px] text-fg-muted">{t.reason}</span>
                        </span>
                        <span className="text-[10.5px] text-fg-faint shrink-0 tabular-nums">
                          ± {t.rows.toLocaleString('nl-NL')} rijen
                        </span>
                      </label>
                    ))}
                  </div>
                </div>
              ))}
            </>
          )}
        </>
      )}
    </div>
  )
}
