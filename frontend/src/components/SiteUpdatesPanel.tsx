import { useCallback, useEffect, useState } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { WPSiteView, SiteUpdates, RunResult, DepotCoreStatus } from '../../bindings/github.com/rdm/sites-tool/internal/services/models'
import Foutvak from './Foutvak'
import KnopTekst from './KnopTekst'
import { bevestig } from '../lib/bevestig'
import ExternalLink from './ExternalLink'
import { hoogsteVersie, vergelijkVersie } from '../lib/versie'

type Actie = 'verversen' | 'plugins' | 'alles' | 'core' | 'vertalingen' | 'composerpr'

const knop = 'bg-accent text-white text-[12px] font-semibold px-3 py-1.5 rounded-lg hover:brightness-110 disabled:opacity-50 transition'
const knopStil = 'border border-border text-fg-muted text-[12px] px-3 py-1.5 rounded-lg hover:bg-hover disabled:opacity-50 transition'

function foutTekst(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

export default function SiteUpdatesPanel({ view }: { view: WPSiteView }) {
  const site = view.site
  const [data, setData] = useState<SiteUpdates | null>(null)
  const [actie, setActie] = useState<Actie | null>(null)
  const [fout, setFout] = useState<string | null>(null)
  const [gekozen, setGekozen] = useState<Set<string>>(new Set())
  const [major, setMajor] = useState(false)
  const [resultaat, setResultaat] = useState<RunResult | null>(null)
  const [depot, setDepot] = useState<DepotCoreStatus | null>(null)
  const [depotFout, setDepotFout] = useState<string | null>(null)
  const bezig = actie !== null

  const laad = useCallback(async (refresh: boolean) => {
    setFout(null)
    try {
      const u = await Services.WPSiteService.AvailableUpdates(site.id, refresh)
      setData(u); setGekozen(new Set())
    } catch (e) {
      setFout(foutTekst(e))
    }
  }, [site.id])

  // Alleen op de depotsite: loopt composer.lock in de repo achter op live?
  // Een fout hier mag het blok niet blokkeren.
  const laadDepot = useCallback(async () => {
    if (!site.depot) return
    try {
      setDepot(await Services.WPSiteService.DepotCoreStatus(site.id)); setDepotFout(null)
    } catch (e) {
      setDepot(null); setDepotFout(foutTekst(e))
    }
  }, [site.id, site.depot])

  useEffect(() => { setData(null); setResultaat(null); setMajor(false); void laad(false) }, [laad])
  useEffect(() => { setDepot(null); setDepotFout(null); void laadDepot() }, [laadDepot])

  const waarschuwing = site.depot
    ? null
    : 'Deze wijziging komt niet in git; de volgende deploy draait hem terug.'

  const voerUit = async (a: Actie, titel: string, uitleg: string, werk: () => Promise<RunResult>) => {
    if (!(await bevestig(titel, [uitleg, waarschuwing].filter(Boolean).join('\n\n')))) return
    setActie(a); setFout(null); setResultaat(null)
    try {
      setResultaat(await werk())
      if (a !== 'composerpr') await laad(true)
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      if (a === 'core' || a === 'composerpr') await laadDepot()
      setMajor(false)
      setActie(null)
    }
  }

  const verversen = async () => {
    setActie('verversen')
    try {
      await laad(true)
    } finally {
      setActie(null)
    }
  }

  const kanUitvoeren = !site.readOnly && !bezig
  const core = data?.core ?? []
  const plugins = data?.plugins ?? []
  const themes = data?.themes ?? []
  // De bridge kiest de hoogste nieuwere versie, zonder major-vinkje alleen
  // binnen dezelfde major; de bevestiging noemt diezelfde keuze.
  const coreDoel = () => {
    const huidig = hoogsteVersie(core.map(c => c.current)) ?? '?'
    const toegestaan = core.filter(c => vergelijkVersie(c.version, c.current) > 0 && (major || c.type !== 'major'))
    const doel = hoogsteVersie((toegestaan.length > 0 ? toegestaan : core).map(c => c.version)) ?? '?'
    return `WordPress ${huidig} → ${doel}${major ? ' (major toegestaan)' : ''}`
  }
  const alleGekozen = plugins.length > 0 && plugins.every(p => gekozen.has(p.id))
  const niksTeDoen = data && core.length === 0 && plugins.length === 0 && themes.length === 0 && data.translations === 0

  return (
    <div className="bg-panel border border-border rounded-xl p-4 mb-6">
      <div className="flex items-center gap-3 mb-3">
        <div className="text-[12px] font-semibold text-fg flex-1">Beschikbaar op de site</div>
        {data?.checkedAt && <span className="text-[11px] text-fg-faint">gecontroleerd {new Date(data.checkedAt).toLocaleString('nl-NL')}</span>}
        <button onClick={verversen} disabled={bezig} className={knopStil}><KnopTekst tekst="Verversen" bezig={actie === 'verversen'} /></button>
      </div>

      {fout && <Foutvak fout={fout} className="mb-3" />}
      {site.readOnly && <p className="text-[11.5px] text-fg-faint mb-3">Deze site staat op alleen-lezen: bijwerken vanuit de tool staat uit.</p>}
      {!data && !fout && <p className="text-[12px] text-fg-faint">Laden…</p>}
      {depot?.needsPr && (
        <div className="flex items-center gap-3 mb-3">
          <span className="text-[12px] text-amber flex-1">Live draait WordPress {depot.live}, composer.lock staat op {depot.locked}.</span>
          {/* Raakt de live site niet, dus ook bij alleen-lezen beschikbaar. */}
          <button disabled={bezig} className={knop}
            onClick={() => voerUit('composerpr', 'Composer-PR maken?',
              `composer.lock in de repo naar WordPress ${depot.live} zetten (nu ${depot.locked}). De live site verandert niet.`,
              () => Services.WPSiteService.OpenDepotCorePR(site.id))}>
            <KnopTekst tekst="Composer-PR maken" bezig={actie === 'composerpr'} />
          </button>
        </div>
      )}
      {depotFout && <p className="text-[11px] text-fg-faint mb-3">Composer-status niet op te halen: {depotFout}</p>}
      {niksTeDoen && <p className="text-[12px] text-green">Alles is bijgewerkt.</p>}

      {core.length > 0 && (
        <div className="mb-4">
          <p className="text-[11px] font-semibold uppercase tracking-wide text-fg-faint mb-1.5">WordPress</p>
          {core.map(c => (
            <div key={c.version} className="flex items-center gap-2 text-[12px] font-mono text-fg mb-1">
              <span>{c.current} → {c.version}</span>
              <span className={`text-[10px] px-1.5 py-0.5 rounded ${c.type === 'major' ? 'bg-amber-soft text-amber' : 'bg-panel-2 text-fg-muted'}`}>{c.type}</span>
            </div>
          ))}
          <div className="flex items-center gap-3 mt-2">
            <button disabled={!kanUitvoeren} className={knop}
              onClick={() => voerUit('core', 'WordPress bijwerken?',
                `${coreDoel()} op ${site.name}.` + (site.depot ? '\n\nDaarna maakt de tool een Composer-PR in de gekoppelde repo.' : ''),
                () => Services.WPSiteService.UpdateCore(site.id, major))}>
              <KnopTekst tekst="WordPress bijwerken" bezig={actie === 'core'} />
            </button>
            <label className="flex items-center gap-1.5 text-[11.5px] text-fg-muted">
              <input type="checkbox" checked={major} onChange={e => setMajor(e.target.checked)} /> ook major toestaan
            </label>
          </div>
        </div>
      )}

      {plugins.length > 0 && (
        <div className="mb-4">
          <p className="text-[11px] font-semibold uppercase tracking-wide text-fg-faint mb-1.5">Plugins ({plugins.length})</p>
          <div className="flex items-center gap-2 mb-2">
            <button disabled={!kanUitvoeren || gekozen.size === 0} className={knop}
              onClick={() => voerUit('plugins', 'Plugins bijwerken?',
                `${gekozen.size} plugin(s) bijwerken.${site.depot ? ' De PR komt via de SatisPress-workflow op GitHub.' : ''}`,
                () => Services.WPSiteService.UpdatePlugins(site.id, [...gekozen], false))}>
              <KnopTekst tekst="Geselecteerde bijwerken" bezig={actie === 'plugins'} />
            </button>
            <button disabled={!kanUitvoeren} className={knopStil}
              onClick={() => voerUit('alles', 'Alle plugins bijwerken?',
                `Alle ${plugins.length} plugins bijwerken.${site.depot ? ' De PR komt via de SatisPress-workflow op GitHub.' : ''}`,
                () => Services.WPSiteService.UpdatePlugins(site.id, [], true))}>
              <KnopTekst tekst="Alles bijwerken" bezig={actie === 'alles'} />
            </button>
          </div>
          <table className="w-full text-[12px]">
            <thead>
              <tr>
                <th className="py-1 w-6 text-left font-normal">
                  <input type="checkbox" aria-label="Alles selecteren" title="Alles selecteren" checked={alleGekozen} disabled={!kanUitvoeren}
                    onChange={e => setGekozen(e.target.checked ? new Set(plugins.map(p => p.id)) : new Set())} />
                </th>
                <th className="py-1 text-left font-normal text-[11px] text-fg-faint">Alles selecteren</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {plugins.map(p => (
                <tr key={p.id} className="border-t border-border">
                  <td className="py-1 w-6">
                    <input type="checkbox" checked={gekozen.has(p.id)} disabled={!kanUitvoeren}
                      onChange={e => setGekozen(prev => {
                        const next = new Set(prev)
                        if (e.target.checked) { next.add(p.id) } else { next.delete(p.id) }
                        return next
                      })} />
                  </td>
                  <td className="py-1 text-fg">{p.name}</td>
                  <td className="py-1 font-mono text-fg-muted text-right">{p.currentVersion} → {p.newVersion}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {themes.length > 0 && (
        <div className="mb-4">
          <p className="text-[11px] font-semibold uppercase tracking-wide text-fg-faint mb-1.5">Thema's ({themes.length}) — alleen ter info</p>
          {themes.map(t => (
            <div key={t.id} className="flex text-[12px] py-0.5">
              <span className="text-fg flex-1">{t.name}</span>
              <span className="font-mono text-fg-muted">{t.currentVersion} → {t.newVersion}</span>
            </div>
          ))}
        </div>
      )}

      {data && data.translations > 0 && (
        <div className="flex items-center gap-3 mb-2">
          <span className="text-[12px] text-fg flex-1">{data.translations} vertaling(en) klaar</span>
          <button disabled={!kanUitvoeren} className={knopStil}
            onClick={() => voerUit('vertalingen', 'Vertalingen bijwerken?', 'Alle klaarstaande vertalingen bijwerken.',
              () => Services.WPSiteService.UpdateTranslations(site.id))}>
            <KnopTekst tekst="Vertalingen bijwerken" bezig={actie === 'vertalingen'} />
          </button>
        </div>
      )}

      {resultaat && (
        <div className="mt-3 border-t border-border pt-3 text-[12px]">
          {resultaat.items.map((it, i) => (
            <div key={i} className={it.success ? 'text-green' : 'text-red'}>
              {it.success ? '✓' : '✗'} {it.name} {it.from && `${it.from} → ${it.to}`} {it.error && `— ${it.error}`}
            </div>
          ))}
          {resultaat.pullRequest && <div className="mt-1.5">PR: <ExternalLink href={resultaat.pullRequest}>{resultaat.pullRequest}</ExternalLink></div>}
          {resultaat.actionsUrl && <div className="mt-1.5">De PR wordt gemaakt door de workflow: <ExternalLink href={resultaat.actionsUrl}>bekijk de run</ExternalLink></div>}
          {resultaat.warning && <p className="mt-1.5 text-amber">{resultaat.warning}</p>}
        </div>
      )}
    </div>
  )
}
