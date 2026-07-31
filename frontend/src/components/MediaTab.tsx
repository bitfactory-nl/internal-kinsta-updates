import { useState, useEffect, useCallback, useMemo } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { SiteDetails } from '../../bindings/github.com/rdm/sites-tool/internal/adapters/kinsta/models'
import type { MediaScanSummary, MediaCategoryResult, MediaFileRow, MediaPeriodBucket } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import { MediaCategory } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'

interface Props { projectId: string }

function foutTekst(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

// CATEGORIE_UITLEG zegt per categorie wat de claim is én hoe hard die is. De derde
// is een heuristiek: het ontbreken van een referentie is geen bewijs dat iets
// ongebruikt is, en die nuance moet in de UI staan, niet in een handleiding.
const CATEGORIE_UITLEG: Record<string, { titel: string; uitleg: string }> = {
  [MediaCategory.MediaInUse]: {
    titel: 'Wordt gebruikt',
    uitleg: 'Voor deze media is een concrete verwijzing gevonden. Achter elk bestand staat waar: in de content, in meta, in een ACF-veld, in de instellingen of in themacode.',
  },
  [MediaCategory.MediaOrphanFile]: {
    titel: 'Staat niet in de mediabibliotheek',
    uitleg: 'Bestanden op de server die WordPress niet kent. Hard feit; vaak restanten van migraties of handmatige uploads.',
  },
  [MediaCategory.MediaMissingFile]: {
    titel: 'Bestand ontbreekt op de server',
    uitleg: 'De mediabibliotheek verwijst naar een bestand dat er niet is. Hard feit; dit zijn kapotte afbeeldingen op de site.',
  },
  [MediaCategory.MediaUnreferenced]: {
    titel: 'Geen referentie gevonden',
    uitleg: 'Nergens in de content, meta, opties of themacode een verwijzing gevonden. Dit is een aanwijzing, geen bewijs: sliders, nieuwsbrieven, externe systemen en directe links vallen buiten de scan.',
  },
}

// BEWIJS_LABEL maakt van een bewijs-bit een leesbare plek.
const BEWIJS_LABEL: Record<string, string> = {
  content: 'content',
  meta: 'meta',
  acf: 'ACF-veld',
  options: 'instellingen',
  termmeta: 'categorie',
  usermeta: 'gebruiker',
  theme: 'themacode',
  extra_table: 'plugin-tabel',
  revision_only: 'alleen revisie',
  filename_only: 'alleen bestandsnaam',
}

function bytes(n: number): string {
  if (!n) return '0 B'
  const eenheden = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), eenheden.length - 1)
  return `${(n / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1)} ${eenheden[i]}`
}

function datum(unix: number): string {
  if (!unix) return '—'
  return new Date(unix * 1000).toLocaleDateString('nl-NL', { year: 'numeric', month: 'short', day: 'numeric' })
}

function Stat({ label, waarde, sub }: { label: string; waarde: string; sub?: string }) {
  return (
    <div className="bg-panel border border-border rounded-xl px-4 py-3 min-w-[130px]">
      <div className="text-[10px] uppercase tracking-wide text-fg-faint mb-1">{label}</div>
      <div className="text-[17px] font-semibold text-fg leading-tight">{waarde}</div>
      {sub && <div className="text-[11px] text-fg-muted mt-0.5">{sub}</div>}
    </div>
  )
}

interface MappenPaneelProps {
  rijen: MediaPeriodBucket[]
  herkomst: string
  selectie: Set<string>
  onToggle: (period: string) => void
  onScan: () => void
  bezig: boolean
}

// MappenPaneel laat in pure CSS zien waar de ruimte zit (een chartlibrary zit niet
// in dit project) en dient tegelijk als selectie: een gerichte scan doorloopt alleen
// de aangevinkte mappen, wat het duurste onderdeel — de bestandsdoorloop — klein
// houdt.
function MappenPaneel({ rijen, herkomst, selectie, onToggle, onScan, bezig }: MappenPaneelProps) {
  const [alles, setAlles] = useState(false)
  const gesorteerd = rijen.slice().sort((a, b) => b.bytes - a.bytes)
  const zichtbaar = alles ? gesorteerd : gesorteerd.slice(0, 12)
  const max = gesorteerd.reduce((m, r) => Math.max(m, r.bytes), 0)
  const gekozenBytes = gesorteerd.filter(r => selectie.has(r.period)).reduce((n, r) => n + r.bytes, 0)
  if (!gesorteerd.length) return null

  return (
    <div className="bg-panel border border-border rounded-xl p-4">
      <div className="flex items-center gap-2 mb-2.5">
        <div className="text-[10px] font-semibold tracking-wide text-fg-faint">MAPPEN</div>
        <div className="text-[10px] text-fg-faint">{herkomst}</div>
        {selectie.size > 0 && (
          <button onClick={onScan} disabled={bezig}
            className="ml-auto bg-accent text-white text-[11.5px] font-semibold px-3 py-1 rounded-lg hover:brightness-110 disabled:opacity-50 transition">
            {bezig ? 'Bezig…' : `Scan ${selectie.size} map${selectie.size === 1 ? '' : 'pen'} (${bytes(gekozenBytes)})`}
          </button>
        )}
      </div>

      {zichtbaar.map(r => {
        const aan = selectie.has(r.period)
        return (
          <label key={r.period} className="flex items-center gap-2 py-[3px] cursor-pointer hover:bg-hover rounded px-1 -mx-1">
            <input type="checkbox" checked={aan} onChange={() => onToggle(r.period)} className="shrink-0 accent-accent" />
            <span className={`font-mono text-[11px] w-[110px] shrink-0 truncate ${aan ? 'text-fg' : 'text-fg-muted'}`}>
              {r.period === '.' ? '(hoofdmap)' : r.period}
            </span>
            <div className="flex-1 h-2 bg-panel-2 rounded-full overflow-hidden">
              <div className="h-full bg-accent rounded-full" style={{ width: `${max ? (r.bytes / max) * 100 : 0}%` }} />
            </div>
            <span className="font-mono text-[11px] text-fg w-[70px] text-right shrink-0">{bytes(r.bytes)}</span>
            <span className="font-mono text-[10.5px] text-fg-faint w-[60px] text-right shrink-0">{r.files}×</span>
          </label>
        )
      })}

      {gesorteerd.length > 12 && (
        <button onClick={() => setAlles(a => !a)} className="mt-2 text-[11.5px] text-fg-muted hover:text-fg transition">
          {alles ? 'minder tonen' : `alle ${gesorteerd.length} mappen tonen`}
        </button>
      )}
    </div>
  )
}

function ScopeBlok({ scan }: { scan: MediaScanSummary }) {
  const s = scan.scope
  return (
    <div className="bg-panel-2 border border-border rounded-xl p-4 text-[11.5px] text-fg-muted leading-relaxed">
      <div className="text-[10px] font-semibold tracking-wide text-fg-faint mb-1.5">WAT IS GESCAND</div>
      <div>
        Uploads-map <span className="font-mono text-fg">{s.uploadsPath}</span>
        {s.multisite && ' · multisite'}
      </div>
      <div>
        Gescand: {(s.tablesScanned ?? []).length} databasetabellen ({(s.tablesScanned ?? []).join(', ') || '—'})
        {s.themeFilesScanned > 0 && `, ${s.themeFilesScanned} bestanden in thema en mu-plugins`}.
        Revisies gelden niet als bewijs.
      </div>
      {s.rowsScanned && Object.keys(s.rowsScanned).length > 0 && (
        <div className="mt-1">
          Doorzochte rijen:{' '}
          {Object.entries(s.rowsScanned).map(([bron, n], i) => (
            <span key={bron}>
              {i > 0 && ' · '}
              <span className="font-mono text-fg">{(n ?? 0).toLocaleString('nl-NL')}</span> {bron}
            </span>
          ))}
        </div>
      )}
      <div className="mt-1">
        Niet gescand: externe systemen, nieuwsbrieven, CDN-caches, code buiten thema en mu-plugins,
        en plugins die media in eigen tabellen bewaren.
      </div>
      {s.offloadDetected && (
        <div className="mt-2 text-amber">Let op: er is een offload-plugin actief. Ontbrekende bestanden staan waarschijnlijk in externe opslag.</div>
      )}
      {s.truncated && (
        <div className="mt-2 text-amber">Let op: de scan is op tijd afgekapt; de cijfers zijn onvolledig.</div>
      )}
      {(s.notes ?? []).map((n, i) => (
        <div key={i} className="mt-1.5 text-fg-faint">· {n}</div>
      ))}
    </div>
  )
}

function CategorieBlok({ projectId, scanId, blok }: { projectId: string; scanId: string; blok: MediaCategoryResult }) {
  const [open, setOpen] = useState(false)
  const [rijen, setRijen] = useState<MediaFileRow[]>(blok.samples ?? [])
  const [meerBezig, setMeerBezig] = useState(false)
  const [filter, setFilter] = useState('')

  const uitleg = CATEGORIE_UITLEG[blok.category] ?? { titel: blok.category, uitleg: '' }

  const zichtbaar = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (!q) return rijen
    return rijen.filter(r => r.path.toLowerCase().includes(q) || (r.title ?? '').toLowerCase().includes(q))
  }, [rijen, filter])

  // De offset loopt binnen deze categorie; het detailbestand bevat alle
  // categorieën, dus de backend filtert mee.
  const meerLaden = async () => {
    setMeerBezig(true)
    try {
      const volgende = await Services.MediaService.ScanDetail(projectId, scanId, blok.category, rijen.length, 500)
      setRijen(huidig => [...huidig, ...(volgende ?? [])])
    } finally {
      setMeerBezig(false)
    }
  }

  return (
    <div className="bg-panel border border-border rounded-xl overflow-hidden">
      <button onClick={() => setOpen(o => !o)} className="w-full flex items-center gap-2.5 px-4 py-3 text-left hover:bg-hover transition">
        <span className="text-fg-faint text-[11px] w-3">{open ? '▾' : '▸'}</span>
        <span className="text-[13px] font-semibold text-fg">{uitleg.titel}</span>
        <span className={`text-[9.5px] font-bold px-2 py-px rounded ${blok.hard ? 'bg-green-soft text-green' : 'bg-amber-soft text-amber'}`}>
          {blok.hard ? 'HARD FEIT' : 'HEURISTIEK'}
        </span>
        <span className="ml-auto text-[11px] text-fg-faint">{open ? '' : 'bekijk bestanden'}</span>
        <span className="font-mono text-[12px] text-fg-muted">{blok.files}×</span>
        <span className="font-mono text-[12px] text-fg w-[80px] text-right">{bytes(blok.bytes)}</span>
      </button>

      {open && (
        <div className="border-t border-border px-4 py-3">
          <p className="text-[11.5px] text-fg-muted mb-3 leading-relaxed">{uitleg.uitleg}</p>

          {blok.files > 0 && (
            <input type="search" value={filter} onChange={e => setFilter(e.target.value)}
              placeholder="Zoek in bestandsnaam of titel"
              className="w-full bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12px] text-fg mb-2" />
          )}

          <div className="divide-y divide-border/40">
            {zichtbaar.map((r, i) => (
              <div key={`${r.path}-${i}`} className="flex items-center gap-2 py-1.5">
                <span className="font-mono text-[11.5px] text-fg truncate flex-1">{r.path}</span>
                {(r.evidence ?? []).map(e => (
                  <span key={e} className={`text-[9.5px] font-semibold px-1.5 py-px rounded shrink-0 ${
                    e === 'revision_only' || e === 'filename_only' ? 'bg-amber-soft text-amber' : 'bg-green-soft text-green'
                  }`}>
                    {BEWIJS_LABEL[e] ?? e}
                  </span>
                ))}
                {r.title && <span className="text-[11px] text-fg-faint truncate max-w-[140px]">{r.title}</span>}
                <span className="font-mono text-[11px] text-fg-muted w-[70px] text-right shrink-0">{bytes(r.bytes)}</span>
                <span className="font-mono text-[11px] text-fg-faint w-[90px] text-right shrink-0">{datum(r.modifiedAt)}</span>
              </div>
            ))}
            {zichtbaar.length === 0 && (
              <div className="text-[12px] text-fg-faint italic py-2">Niets in deze categorie.</div>
            )}
          </div>

          {blok.truncated && rijen.length < blok.files && (
            <button onClick={meerLaden} disabled={meerBezig}
              className="mt-3 text-[12px] text-fg-muted border border-border rounded-lg px-3 py-1.5 hover:bg-hover transition disabled:opacity-50">
              {meerBezig ? 'Laden…' : `Meer laden (${rijen.length} van ${blok.files})`}
            </button>
          )}
        </div>
      )}
    </div>
  )
}

export default function MediaTab({ projectId }: Props) {
  const [site, setSite] = useState<SiteDetails | null>(null)
  const [envId, setEnvId] = useState('')
  const [user, setUser] = useState('')
  const [pad, setPad] = useState('')
  const [wachtwoord, setWachtwoord] = useState('')
  const [heeftWachtwoord, setHeeftWachtwoord] = useState(false)
  const [scan, setScan] = useState<MediaScanSummary | null>(null)
  const [scans, setScans] = useState<MediaScanSummary[]>([])
  const [selectie, setSelectie] = useState<Set<string>>(new Set())
  const [bezig, setBezig] = useState(false)
  const [probeTekst, setProbeTekst] = useState<string | null>(null)
  const [fout, setFout] = useState<string | null>(null)

  useEffect(() => {
    setSite(null); setScan(null); setFout(null); setProbeTekst(null); setEnvId('')

    Services.KinstaService.GetLinkedSiteID(projectId)
      .then(id => (id ? Services.KinstaService.GetSiteDetails(id).then(setSite) : undefined))
      .catch(e => setFout(foutTekst(e)))

    setWachtwoord(''); setScans([]); setSelectie(new Set())
    Services.MediaService.GetSSHAccess(projectId)
      .then(a => { setUser(a.user ?? ''); setPad(a.path ?? ''); setHeeftWachtwoord(a.hasPassword) })
      .catch(() => {})

    Services.MediaService.LatestScan(projectId)
      .then(s => setScan(s ?? null))
      .catch(e => setFout(foutTekst(e)))

    Services.MediaService.ListScans(projectId)
      .then(l => setScans(l ?? []))
      .catch(() => {})
  }, [projectId])

  // Zonder expliciete keuze de live-omgeving: dat is waar de klant naar kijkt.
  useEffect(() => {
    if (!envId && site?.environments?.length) {
      const live = site.environments.find(e => e.name === 'live') ?? site.environments[0]
      setEnvId(live.id)
    }
  }, [site, envId])

  // Het wachtwoord gaat naar de macOS-keychain; .rdm.yml krijgt alleen een
  // verwijzing, want dat bestand staat in de repo van de klant.
  const bewaarToegang = useCallback(async () => {
    await Services.MediaService.SaveSSHAccess(projectId, user, pad, wachtwoord)
    if (wachtwoord) {
      setHeeftWachtwoord(true)
      setWachtwoord(''); setScans([]); setSelectie(new Set())
    }
  }, [projectId, user, pad, wachtwoord])

  const testVerbinding = async () => {
    setBezig(true); setFout(null); setProbeTekst(null)
    try {
      await bewaarToegang()
      const p = await Services.MediaService.ProbeEnvironment(projectId, envId)
      setProbeTekst(`ingelogd als ${p.user} · site in ${p.webroot} · ${p.wpCli || 'geen WP-CLI-antwoord'} · uploads ${bytes(p.uploadsKb * 1024)}`)
      if (p.webroot && !pad) setPad(p.webroot)
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setBezig(false)
    }
  }

  const scanNu = async (mappen: string[] = []) => {
    setBezig(true); setFout(null); setProbeTekst(null)
    try {
      await bewaarToegang()
      const verse = await Services.MediaService.ScanEnvironment(projectId, envId, mappen)
      setScan(verse)
      setScans(await Services.MediaService.ListScans(projectId) ?? [])
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setBezig(false)
    }
  }

  const toggleMap = (period: string) => {
    setSelectie(huidig => {
      const volgende = new Set(huidig)
      if (volgende.has(period)) {
        volgende.delete(period)
      } else {
        volgende.add(period)
      }
      return volgende
    })
  }

  if (!site) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center gap-3 p-10 text-center">
        <p className="text-[15px] font-semibold text-fg">Geen Kinsta-site gekoppeld</p>
        <p className="text-[13px] text-fg-muted max-w-[380px]">
          Koppel dit project eerst aan een Kinsta-site via het Kinsta-tabblad; de scan gebruikt het SSH-adres van die site.
        </p>
        {fout && <p className="text-[12px] text-red">{fout}</p>}
      </div>
    )
  }

  // De mappenlijst komt uit de laatste vólledige scan. Na een gerichte scan zou je
  // anders alleen de mappen zien die je net had gekozen, en niets meer kunnen kiezen.
  const volledige = scans.find(s => !(s.scope.folders ?? []).length) ?? null
  const mappenlijst: MediaPeriodBucket[] = (volledige ?? scan)?.byPeriod ?? []
  const mappenHerkomst = volledige && scan && volledige.id !== scan.id
    ? `uit de volledige scan van ${new Date(volledige.scannedAt).toLocaleDateString('nl-NL')}`
    : ''

  const gegenereerd = (scan?.byClass ?? []).find(c => c.class === 'generated')
  const systeem = (scan?.byClass ?? []).find(c => c.class === 'system')

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-hidden">
      {/* Instellingen en acties */}
      <div className="shrink-0 flex items-center gap-2 px-6 py-3 border-b border-border bg-panel flex-wrap">
        <select value={envId} onChange={e => setEnvId(e.target.value)}
          className="bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg">
          {(site.environments ?? []).map(env => (
            <option key={env.id} value={env.id}>{env.display_name || env.name}</option>
          ))}
        </select>
        <input value={user} onChange={e => setUser(e.target.value)} placeholder="SSH-gebruiker"
          className="bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg w-[150px]" />
        <input type="password" value={wachtwoord} onChange={e => setWachtwoord(e.target.value)}
          placeholder={heeftWachtwoord ? 'wachtwoord bewaard' : 'wachtwoord'}
          title="Wordt in de macOS-keychain bewaard; .rdm.yml krijgt alleen een verwijzing. Leeg laten houdt het bestaande wachtwoord."
          className="bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg w-[140px]" />
        <input value={pad} onChange={e => setPad(e.target.value)} placeholder="pad (leeg = zelf zoeken)"
          className="bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg w-[200px] font-mono" />
        <button onClick={testVerbinding} disabled={bezig || !user}
          className="text-[12.5px] text-fg-muted border border-border rounded-lg px-3 py-1.5 hover:bg-hover transition disabled:opacity-50">
          Verbinding testen
        </button>
        <button onClick={() => scanNu()} disabled={bezig || !user || !envId}
          className="ml-auto bg-accent text-white text-[12.5px] font-semibold px-4 py-1.5 rounded-lg hover:brightness-110 disabled:opacity-50 transition">
          {bezig ? <span className="animate-spin inline-block">↻</span> : '▶ Alles scannen'}
        </button>
      </div>

      {probeTekst && (
        <div className="shrink-0 mx-6 mt-3 bg-green-soft text-green px-3 py-2 rounded-lg text-[11.5px]">{probeTekst}</div>
      )}
      {fout && (
        <div className="shrink-0 mx-6 mt-3 bg-red-soft text-red px-3 py-2 rounded-lg text-[11.5px]">{fout}</div>
      )}

      <div className="flex-1 min-h-0 overflow-y-auto px-6 py-4">
        {!scan ? (
          <div className="text-fg-faint text-[13px] italic py-10 text-center">
            {bezig
              ? 'De scan loopt op de server; bij een grote mediabibliotheek duurt dat enkele minuten.'
              : 'Nog geen scan voor dit project. Vul de SSH-gebruiker in en druk op Scan uitvoeren.'}
          </div>
        ) : (
          <>
            <div className="text-[11.5px] text-fg-muted mb-3">
              Stand van {new Date(scan.scannedAt).toLocaleString('nl-NL')} · omgeving {scan.environment} ·
              {' '}{Math.round(scan.durationMs / 1000)} s
              {(scan.scope.folders ?? []).length > 0 && (
                <span className="ml-1.5 text-amber">
                  · alleen {(scan.scope.folders ?? []).join(', ')} — over de overige mappen zegt deze scan niets
                </span>
              )}
            </div>

            <div className="flex gap-2.5 flex-wrap mb-4">
              <Stat label="Uploads totaal" waarde={bytes(scan.totalBytes)} sub={`${scan.totalFiles.toLocaleString('nl-NL')} bestanden`} />
              <Stat label="Mediabibliotheek" waarde={scan.attachmentCount.toLocaleString('nl-NL')} sub={`${scan.referencedCount.toLocaleString('nl-NL')} met referentie`} />
              {gegenereerd && (
                <Stat label="Gegenereerde formaten" waarde={bytes(gegenereerd.bytes)}
                  sub={scan.totalBytes ? `${Math.round((gegenereerd.bytes / scan.totalBytes) * 100)}% van de map` : undefined} />
              )}
              {systeem && systeem.bytes > 0 && (
                <Stat label="Caches en archieven" waarde={bytes(systeem.bytes)} sub="geen media" />
              )}
              {scan.diskUsageBytes > 0 && (
                <Stat label="Volgens du" waarde={bytes(scan.diskUsageBytes)} sub="blokken op schijf" />
              )}
            </div>

            <div className="flex flex-col gap-2.5 mb-4">
              {(scan.categories ?? []).map(blok => (
                <CategorieBlok key={blok.category} projectId={projectId} scanId={scan.id} blok={blok} />
              ))}
            </div>

            <div className="flex flex-col gap-2.5">
              <MappenPaneel
                rijen={mappenlijst}
                herkomst={mappenHerkomst}
                selectie={selectie}
                onToggle={toggleMap}
                onScan={() => scanNu(Array.from(selectie))}
                bezig={bezig}
              />
              <ScopeBlok scan={scan} />
            </div>
          </>
        )}
      </div>
    </div>
  )
}
