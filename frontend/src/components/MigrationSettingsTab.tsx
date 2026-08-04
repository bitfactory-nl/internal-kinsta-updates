import { useState, useEffect } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { MigrationCfg, DomainPair } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import Foutvak from './Foutvak'

interface Props { projectId: string }

function foutTekst(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

const veldClass = 'w-full bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg font-mono'

export default function MigrationSettingsTab({ projectId }: Props) {
  const [multisite, setMultisite] = useState(false)
  const [prodURL, setProdURL] = useState('')
  const [localURL, setLocalURL] = useState('')
  const [prodDomain, setProdDomain] = useState('')
  const [localDomain, setLocalDomain] = useState('')
  const [extra, setExtra] = useState<DomainPair[]>([])

  const [laden, setLaden] = useState(true)
  const [bezig, setBezig] = useState(false)
  const [opgeslagen, setOpgeslagen] = useState(false)
  const [fout, setFout] = useState<string | null>(null)

  useEffect(() => {
    setLaden(true); setFout(null); setOpgeslagen(false)
    Services.MigrationService.GetSettings(projectId)
      .then(cfg => {
        setMultisite(cfg.multisite ?? false)
        setProdURL(cfg.prodUrl ?? '')
        setLocalURL(cfg.localUrl ?? '')
        setProdDomain(cfg.prodDomain ?? '')
        setLocalDomain(cfg.localDomain ?? '')
        setExtra(cfg.extraDomains ?? [])
      })
      .catch(e => setFout(foutTekst(e)))
      .finally(() => setLaden(false))
  }, [projectId])

  const opslaan = async () => {
    setBezig(true); setFout(null); setOpgeslagen(false)
    try {
      const cfg: MigrationCfg = {
        multisite,
        prodUrl: prodURL,
        localUrl: localURL,
        prodDomain,
        localDomain,
        extraDomains: extra,
      }
      await Services.MigrationService.SaveSettings(projectId, cfg)
      setOpgeslagen(true)
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setBezig(false)
    }
  }

  const wijzigExtra = (i: number, veld: 'prod' | 'local', waarde: string) => {
    setExtra(huidig => huidig.map((p, idx) => (idx === i ? { ...p, [veld]: waarde } : p)))
    setOpgeslagen(false)
  }

  if (laden) {
    return <div className="flex-1 px-6 py-4 text-[13px] text-fg-faint italic">Instellingen laden…</div>
  }

  return (
    <div className="flex-1 min-h-0 overflow-y-auto px-6 py-4">
      {fout && <Foutvak fout={fout} className="mb-4" />}

      <p className="text-[11.5px] text-fg-muted mb-4 max-w-[620px]">
        Deze URL's worden gebruikt bij het klonen van de database: ze bepalen wat er van productie naar
        lokaal wordt herschreven. Eenmalig invullen betekent dat je ze niet bij elke migratie opnieuw
        hoeft te typen — en dat een collega dezelfde mapping krijgt zodra je het opslagen bestand commit.
      </p>

      <div className="bg-panel border border-border rounded-xl p-4 mb-4">
        <label className="flex items-center gap-2 text-[12px] font-semibold text-fg mb-3 cursor-pointer">
          <input type="checkbox" checked={multisite}
            onChange={e => { setMultisite(e.target.checked); setOpgeslagen(false) }} />
          Dit is een multisite
        </label>
        <p className="text-[10.5px] text-fg-faint">
          {multisite
            ? 'Bij multisite worden de kale domeinen gebruikt als zoek/vervang-paar: een subsite-URL bevat de volledige URL van de hoofdsite niet als substring, dus zou anders worden overgeslagen.'
            : 'Bij een enkele site worden de volledige URL\'s gebruikt als zoek/vervang-paar.'}
        </p>
      </div>

      <div className="bg-panel border border-border rounded-xl p-4 mb-4">
        <div className="text-[12px] font-semibold text-fg mb-3">Volledige URL's</div>
        <label className="block text-[11px] text-fg-muted mb-1">Productie</label>
        <input value={prodURL} onChange={e => { setProdURL(e.target.value); setOpgeslagen(false) }}
          placeholder="https://vanluyken.nl" className={`${veldClass} mb-2`} />
        <label className="block text-[11px] text-fg-muted mb-1">Lokaal</label>
        <input value={localURL} onChange={e => { setLocalURL(e.target.value); setOpgeslagen(false) }}
          placeholder="https://vanluykennl.test" className={veldClass} />
      </div>

      <div className={`bg-panel border rounded-xl p-4 mb-4 ${multisite ? 'border-border' : 'border-border opacity-50'}`}>
        <div className="text-[12px] font-semibold text-fg mb-1">Netwerkdomeinen (multisite)</div>
        <p className="text-[10.5px] text-fg-faint mb-3">
          Kale hostnamen, zonder https://. Dit is wat wp_blogs en wp_site opslaan.
        </p>
        <label className="block text-[11px] text-fg-muted mb-1">Productie-netwerkdomein</label>
        <input value={prodDomain} onChange={e => { setProdDomain(e.target.value); setOpgeslagen(false) }}
          placeholder="vanluyken.nl" disabled={!multisite} className={`${veldClass} mb-2`} />
        <label className="block text-[11px] text-fg-muted mb-1">Lokaal netwerkdomein</label>
        <input value={localDomain} onChange={e => { setLocalDomain(e.target.value); setOpgeslagen(false) }}
          placeholder="vanluykennl.test" disabled={!multisite} className={veldClass} />
      </div>

      {multisite && (
        <div className="bg-panel border border-border rounded-xl p-4 mb-4">
          <div className="text-[12px] font-semibold text-fg mb-1">Subsites met een eigen domein</div>
          <p className="text-[10.5px] text-fg-faint mb-3 max-w-[560px]">
            Alleen nodig voor subsites die via domain mapping een compleet eigen domein hebben. Zulke
            domeinen delen geen wortel met het netwerk, dus er is geen manier om ze automatisch af te
            leiden — zonder een paar hier blijven ze na de kloon naar productie wijzen.
          </p>
          {extra.length === 0 && (
            <p className="text-[11.5px] text-fg-faint italic mb-3">Nog geen extra domeinen.</p>
          )}
          {extra.map((p, i) => (
            <div key={i} className="flex items-center gap-2 mb-2">
              <input value={p.prod} onChange={e => wijzigExtra(i, 'prod', e.target.value)}
                placeholder="productiedomein.nl" className={veldClass} />
              <span className="text-fg-faint shrink-0">→</span>
              <input value={p.local} onChange={e => wijzigExtra(i, 'local', e.target.value)}
                placeholder="lokaaldomein.test" className={veldClass} />
              <button
                onClick={() => { setExtra(huidig => huidig.filter((_, idx) => idx !== i)); setOpgeslagen(false) }}
                title="Rij verwijderen"
                className="shrink-0 text-[11px] text-fg-muted border border-border rounded-md px-2 py-1 hover:bg-hover hover:text-fg transition">
                ✕
              </button>
            </div>
          ))}
          <button
            onClick={() => { setExtra(huidig => [...huidig, { prod: '', local: '' }]); setOpgeslagen(false) }}
            className="text-[11.5px] text-fg-muted border border-border rounded-lg px-3 py-1.5 hover:bg-hover transition">
            + Domein toevoegen
          </button>
        </div>
      )}

      <div className="flex items-center gap-2.5">
        <button onClick={opslaan} disabled={bezig}
          className="bg-accent text-white text-[12.5px] font-semibold px-4 py-1.5 rounded-lg hover:brightness-110 disabled:opacity-50 transition">
          {bezig ? <span className="animate-spin inline-block">↻</span> : 'Opslaan'}
        </button>
        {opgeslagen && <span className="text-[11.5px] text-green">✓ opgeslagen in .rdm.yml</span>}
      </div>
      <p className="text-[10.5px] text-fg-faint mt-2">
        opgeslagen in .rdm.yml van dit project — committen doe je zelf wanneer je wilt
      </p>
    </div>
  )
}
