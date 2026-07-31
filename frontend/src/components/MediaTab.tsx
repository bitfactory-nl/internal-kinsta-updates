import { useState, useEffect, useCallback, useMemo } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { SiteDetails } from '../../bindings/github.com/rdm/sites-tool/internal/adapters/kinsta/models'
import type { MediaScanSummary, MediaCategoryResult, MediaFileRow, MediaPeriodBucket, MediaExtTotals } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import type { QuarantineBatch, MediaCrawlResult } from '../../bindings/github.com/rdm/sites-tool/internal/services'
import { MediaCategory } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import ExternalLink from './ExternalLink'

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
  rendered: 'op de site gezien',
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
  // Voor het uitklappen van een map: de bestanden komen uit de opgeslagen scan.
  projectId: string
  scanId: string
  uploadsUrl: string
  gekozen: Set<string>
  onToggleRij: (pad: string) => void
  onToggleZichtbaar: (paden: string[], aan: boolean) => void
}

type MapSortering = 'grootte' | 'datum' | 'aantal' | 'gemiddeld'

// SORTEER_LABEL benoemt ook wát je aan een sortering ziet. "Gemiddeld" is de
// interessantste: een map met een gemiddelde van tientallen MB's is geen
// mediabibliotheek maar een parkeerplaats voor grote bestanden, terwijl een map vol
// thumbnails juist een laag gemiddelde heeft.
const SORTEER_LABEL: Record<MapSortering, string> = {
  grootte: 'grootte',
  datum: 'datum',
  aantal: 'aantal',
  gemiddeld: 'gem. per bestand',
}

// datumWaarde maakt van "2024/05" een sorteerbaar getal. Mappen die geen jaar/maand
// zijn (cache, WPL, de hoofdmap) horen niet in een datumreeks en gaan altijd naar
// achteren, ongeacht de richting.
function datumWaarde(period: string): number | null {
  const m = /^(\d{4})\/(\d{2})$/.exec(period)
  if (!m) return null
  return Number(m[1]) * 12 + Number(m[2])
}

function sorteerMappen(rijen: MediaPeriodBucket[], op: MapSortering, omgekeerd: boolean): MediaPeriodBucket[] {
  const richting = omgekeerd ? -1 : 1
  const gemiddeld = (r: MediaPeriodBucket) => (r.files > 0 ? r.bytes / r.files : 0)

  return rijen.slice().sort((a, b) => {
    if (op === 'datum') {
      const da = datumWaarde(a.period)
      const db = datumWaarde(b.period)
      if (da === null && db === null) return a.period.localeCompare(b.period)
      if (da === null) return 1
      if (db === null) return -1
      return (db - da) * richting
    }
    const waarde = op === 'aantal' ? (r: MediaPeriodBucket) => r.files
      : op === 'gemiddeld' ? gemiddeld
      : (r: MediaPeriodBucket) => r.bytes
    return (waarde(b) - waarde(a)) * richting
  })
}

function SorteerKnop({ op, actief, omgekeerd, onKies }: {
  op: MapSortering
  actief: boolean
  omgekeerd: boolean
  onKies: (op: MapSortering) => void
}) {
  return (
    <button onClick={() => onKies(op)}
      className={`text-[10.5px] px-2 py-0.5 rounded-md transition ${
        actief ? 'bg-sel text-fg font-semibold' : 'text-fg-muted hover:text-fg hover:bg-hover'
      }`}>
      {SORTEER_LABEL[op]}
      {actief && <span className="ml-1 text-fg-faint">{omgekeerd ? '↑' : '↓'}</span>}
    </button>
  )
}

interface VoorbeeldProps {
  projectId: string
  scanId: string
  uploadsUrl: string
  rij: MediaFileRow
}

const PREVIEW_TYPES = /\.(jpe?g|png|gif|webp|avif|svg|ico)$/i

// Voorbeeld toont het bestand zoals de bezoeker het zou zien: rechtstreeks van de
// publieke uploads-URL. Lukt dat niet, dan is dat zelf informatie — een bestand dat
// niet op te vragen is, wordt ook door niemand gebruikt.
function Voorbeeld({ projectId, scanId, uploadsUrl, rij }: VoorbeeldProps) {
  const [mislukt, setMislukt] = useState(false)
  const [paginas, setPaginas] = useState<string[] | null>(null)
  const url = uploadsUrl ? `${uploadsUrl.replace(/\/$/, '')}/${rij.path}` : ''
  const toonbaar = PREVIEW_TYPES.test(rij.path)

  useEffect(() => {
    let levend = true
    setPaginas(null)
    Services.MediaService.FileUsage(projectId, scanId, rij.path)
      .then(p => { if (levend) setPaginas(p ?? []) })
      .catch(() => { if (levend) setPaginas([]) })
    return () => { levend = false }
  }, [projectId, scanId, rij.path])

  return (
    <div className="ml-6 my-1.5 flex gap-3 bg-panel-2 border border-border rounded-lg p-2.5">
      <div className="w-[120px] h-[90px] shrink-0 rounded-md border border-border bg-panel overflow-hidden flex items-center justify-center">
        {toonbaar && url && !mislukt ? (
          <img src={url} alt={rij.path} onError={() => setMislukt(true)}
            className="max-w-full max-h-full object-contain" />
        ) : (
          <span className="text-[10px] text-fg-faint text-center px-1">
            {mislukt ? 'niet op te vragen' : (rij.path.split('.').pop() ?? '').toUpperCase()}
          </span>
        )}
      </div>

      <div className="min-w-0 flex-1 text-[11px] leading-relaxed">
        <div className="font-mono text-fg truncate">{rij.path}</div>
        {rij.title && <div className="text-fg-muted truncate">{rij.title}</div>}
        <div className="text-fg-faint">
          {bytes(rij.bytes)} · {datum(rij.modifiedAt)}
          {rij.attachmentId ? ` · id ${rij.attachmentId}` : ''}
          {rij.mimeType ? ` · ${rij.mimeType}` : ''}
        </div>
        {url && (
          <ExternalLink href={url} className="text-accent hover:underline break-all">{url}</ExternalLink>
        )}
        <div className="mt-1">
          {paginas === null ? (
            <span className="text-fg-faint">vindplaatsen ophalen…</span>
          ) : paginas.length === 0 ? (
            <span className="text-fg-faint">
              Geen crawl-resultaat voor dit bestand. Draai "Site doorzoeken" om te zien of een
              pagina het echt opvraagt.
            </span>
          ) : (
            <>
              <span className="text-green font-semibold">Geladen op {paginas.length} pagina{paginas.length === 1 ? '' : "'s"}:</span>
              {paginas.map(p => (
                <div key={p} className="truncate">
                  <ExternalLink href={p} className="text-accent hover:underline">{p}</ExternalLink>
                </div>
              ))}
            </>
          )}
        </div>
      </div>
    </div>
  )
}

interface MapInhoudProps {
  projectId: string
  scanId: string
  map: string
  uploadsUrl: string
  gekozen: Set<string>
  onToggleRij: (pad: string) => void
  onToggleZichtbaar: (paden: string[], aan: boolean) => void
}

// MapInhoud haalt de bestanden van één map op uit de opgeslagen scan. Alleen wat de
// scan in een veilige categorie zette is aan te vinken; wat in gebruik is staat er
// bewust wél bij — je wil kunnen zien waarom een map niet leeg kan.
function MapInhoud({ projectId, scanId, map, uploadsUrl, gekozen, onToggleRij, onToggleZichtbaar }: MapInhoudProps) {
  const [rijen, setRijen] = useState<MediaFileRow[] | null>(null)
  const [fout, setFout] = useState<string | null>(null)
  const [meer, setMeer] = useState(false)
  const [openRij, setOpenRij] = useState<string | null>(null)

  useEffect(() => {
    let levend = true
    setRijen(null); setFout(null)
    Services.MediaService.ScanDetail(projectId, scanId, '' as MediaCategory, map, 0, 500)
      .then(r => { if (levend) setRijen(r ?? []) })
      .catch(e => { if (levend) setFout(foutTekst(e)) })
    return () => { levend = false }
  }, [projectId, scanId, map])

  if (fout) return <div className="pl-7 py-1.5 text-[11.5px] text-red">{fout}</div>
  if (rijen === null) return <div className="pl-7 py-1.5 text-[11.5px] text-fg-faint">Bestanden laden…</div>
  if (!rijen.length) {
    return (
      <div className="pl-7 py-1.5 text-[11.5px] text-fg-faint">
        Geen bevindingen in deze map — alles hier is in gebruik of niet beoordeeld.
      </div>
    )
  }

  const kanWeg = (r: MediaFileRow) =>
    r.category === MediaCategory.MediaUnreferenced || r.category === MediaCategory.MediaOrphanFile
  const selecteerbaar = rijen.filter(kanWeg)
  const zichtbaar = meer ? rijen : rijen.slice(0, 25)
  const allesAan = selecteerbaar.length > 0 && selecteerbaar.every(r => gekozen.has(r.path))

  return (
    <div className="pl-7 pr-1 pb-2">
      <div className="flex items-center gap-2 py-1.5">
        <span className="text-[11px] text-fg-faint">
          {rijen.length} bevinding{rijen.length === 1 ? '' : 'en'} · {selecteerbaar.length} kan naar quarantaine
        </span>
        {selecteerbaar.length > 0 && (
          <button onClick={() => onToggleZichtbaar(selecteerbaar.map(r => r.path), !allesAan)}
            className="text-[11px] text-fg-muted border border-border rounded-lg px-2 py-0.5 hover:bg-hover transition">
            {allesAan ? 'selectie wissen' : `selecteer ${selecteerbaar.length}`}
          </button>
        )}
      </div>

      <div className="divide-y divide-border/30">
        {zichtbaar.map((r, i) => {
          const mag = kanWeg(r)
          const open = openRij === r.path
          return (
            <div key={`${r.path}-${i}`}>
            <div className="flex items-center gap-2 py-1">
              <input type="checkbox" disabled={!mag} checked={gekozen.has(r.path)}
                onChange={() => onToggleRij(r.path)}
                title={mag ? '' : 'deze media wordt gebruikt of het bestand bestaat niet meer'}
                className="shrink-0 accent-accent disabled:opacity-30" />
              <button onClick={() => setOpenRij(open ? null : r.path)}
                title="voorbeeld en vindplaatsen"
                className={`font-mono text-[11px] truncate flex-1 text-left hover:underline ${
                  open ? 'text-fg font-semibold' : mag ? 'text-fg' : 'text-fg-faint'
                }`}>
                {r.path.startsWith(map + '/') ? r.path.slice(map.length + 1) : r.path}
              </button>
              {(r.evidence ?? []).slice(0, 2).map(e => (
                <span key={e} className="text-[9px] font-semibold px-1.5 py-px rounded bg-green-soft text-green shrink-0">
                  {BEWIJS_LABEL[e] ?? e}
                </span>
              ))}
              {!mag && !(r.evidence ?? []).length && (
                <span className="text-[9px] font-semibold px-1.5 py-px rounded bg-panel-2 text-fg-faint shrink-0">
                  {r.category === MediaCategory.MediaMissingFile ? 'bestand mist' : 'in gebruik'}
                </span>
              )}
              <span className="font-mono text-[10.5px] text-fg-muted w-[65px] text-right shrink-0">{bytes(r.bytes)}</span>
              <span className="font-mono text-[10.5px] text-fg-faint w-[85px] text-right shrink-0">{datum(r.modifiedAt)}</span>
            </div>
            {open && <Voorbeeld projectId={projectId} scanId={scanId} uploadsUrl={uploadsUrl} rij={r} />}
            </div>
          )
        })}
      </div>

      {rijen.length > 25 && (
        <button onClick={() => setMeer(m => !m)} className="mt-1.5 text-[11px] text-fg-muted hover:text-fg transition">
          {meer ? 'minder tonen' : `alle ${rijen.length} tonen`}
        </button>
      )}
    </div>
  )
}

// MappenPaneel laat in pure CSS zien waar de ruimte zit (een chartlibrary zit niet
// in dit project) en dient tegelijk als selectie: een gerichte scan doorloopt alleen
// de aangevinkte mappen, wat het duurste onderdeel — de bestandsdoorloop — klein
// houdt.
function MappenPaneel({
  rijen, herkomst, selectie, onToggle, onScan, bezig,
  projectId, scanId, uploadsUrl, gekozen, onToggleRij, onToggleZichtbaar,
}: MappenPaneelProps) {
  const [alles, setAlles] = useState(false)
  const [openMap, setOpenMap] = useState<string | null>(null)
  const [sortering, setSortering] = useState<MapSortering>('grootte')
  const [omgekeerd, setOmgekeerd] = useState(false)

  const kiesSortering = (op: MapSortering) => {
    if (op === sortering) {
      setOmgekeerd(o => !o)
    } else {
      setSortering(op)
      setOmgekeerd(false)
    }
  }

  const gesorteerd = useMemo(() => sorteerMappen(rijen, sortering, omgekeerd), [rijen, sortering, omgekeerd])
  const zichtbaar = alles ? gesorteerd : gesorteerd.slice(0, 12)
  // De balk blijft altijd op grootte geschaald, ook als er op iets anders gesorteerd
  // wordt: anders verandert de betekenis van de balk per sortering.
  const max = rijen.reduce((m, r) => Math.max(m, r.bytes), 0)
  const gekozenBytes = rijen.filter(r => selectie.has(r.period)).reduce((n, r) => n + r.bytes, 0)
  if (!gesorteerd.length) return null

  return (
    <div className="bg-panel border border-border rounded-xl p-4">
      <div className="flex items-center gap-2 mb-1.5 flex-wrap">
        <div className="text-[10px] font-semibold tracking-wide text-fg-faint">MAPPEN</div>
        <div className="text-[10px] text-fg-faint">{herkomst}</div>
        {selectie.size > 0 && (
          <button onClick={onScan} disabled={bezig}
            className="ml-auto bg-accent text-white text-[11.5px] font-semibold px-3 py-1 rounded-lg hover:brightness-110 disabled:opacity-50 transition">
            {bezig ? 'Bezig…' : `Scan ${selectie.size} map${selectie.size === 1 ? '' : 'pen'} (${bytes(gekozenBytes)})`}
          </button>
        )}
      </div>

      <div className="flex items-center gap-1 mb-2 pb-2 border-b border-border/60">
        <span className="text-[10px] text-fg-faint mr-1">sorteer op</span>
        {(['grootte', 'datum', 'aantal', 'gemiddeld'] as MapSortering[]).map(op => (
          <SorteerKnop key={op} op={op} actief={sortering === op} omgekeerd={omgekeerd} onKies={kiesSortering} />
        ))}
      </div>

      {zichtbaar.map(r => {
        const aan = selectie.has(r.period)
        const open = openMap === r.period
        const gem = r.files > 0 ? r.bytes / r.files : 0
        return (
          <div key={r.period}>
            <div className="flex items-center gap-2 py-[3px] hover:bg-hover rounded px-1 -mx-1">
              {/* Het vinkje kiest de map voor een scan; de naam klapt hem open. Twee
                  verschillende acties, dus geen label om de hele rij. */}
              <label className="flex items-center shrink-0 cursor-pointer" title="kies deze map voor een gerichte scan">
                <input type="checkbox" checked={aan} onChange={() => onToggle(r.period)} className="accent-accent" />
              </label>
              <button onClick={() => setOpenMap(open ? null : r.period)}
                title="bestanden in deze map bekijken en selecteren"
                className={`font-mono text-[11px] w-[110px] shrink-0 truncate text-left hover:underline ${
                  open ? 'text-fg font-semibold' : aan ? 'text-fg' : 'text-fg-muted'
                }`}>
                <span className="text-fg-faint mr-1">{open ? '▾' : '▸'}</span>
                {r.period === '.' ? '(hoofdmap)' : r.period}
              </button>
              <div className="flex-1 h-2 bg-panel-2 rounded-full overflow-hidden">
                <div className="h-full bg-accent rounded-full" style={{ width: `${max ? (r.bytes / max) * 100 : 0}%` }} />
              </div>
              <span className="font-mono text-[11px] text-fg w-[70px] text-right shrink-0">{bytes(r.bytes)}</span>
              <span className="font-mono text-[10.5px] text-fg-faint w-[60px] text-right shrink-0">{r.files}×</span>
              <span className={`font-mono text-[10.5px] w-[70px] text-right shrink-0 ${
                sortering === 'gemiddeld' ? 'text-fg' : 'text-fg-faint'
              }`} title="gemiddelde grootte per bestand">
                {bytes(gem)}
              </span>
            </div>
            {open && (
              <MapInhoud projectId={projectId} scanId={scanId} map={r.period} uploadsUrl={uploadsUrl}
                gekozen={gekozen} onToggleRij={onToggleRij} onToggleZichtbaar={onToggleZichtbaar} />
            )}
          </div>
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

// TypePaneel scheidt webformaten van de rest. Dat onderscheid is wat een
// fair-use-gesprek nodig heeft: niet "ongebruikt", maar "dit hoort niet op een
// website" — archieven, presentaties, video, RAW-beeld.
function TypePaneel({ rijen, totaal }: { rijen: MediaExtTotals[]; totaal: number }) {
  if (!rijen.length) return null
  const geenWeb = rijen.filter(r => !r.web)
  const geenWebBytes = geenWeb.reduce((n, r) => n + r.bytes, 0)

  return (
    <div className="bg-panel border border-border rounded-xl p-4">
      <div className="flex items-baseline gap-2 mb-2.5">
        <div className="text-[10px] font-semibold tracking-wide text-fg-faint">BESTANDSTYPEN</div>
        {geenWebBytes > 0 && (
          <div className="text-[10.5px] text-amber">
            {bytes(geenWebBytes)} in formaten die een website niet gebruikt
            {totaal > 0 && ` — ${Math.round((geenWebBytes / totaal) * 100)}% van de map`}
          </div>
        )}
      </div>
      {rijen.slice(0, 14).map(r => (
        <div key={r.ext} className="flex items-center gap-2 py-[3px]">
          <span className={`font-mono text-[11px] w-[70px] shrink-0 ${r.web ? 'text-fg-muted' : 'text-amber'}`}>
            .{r.ext}
          </span>
          {!r.web && <span className="text-[9.5px] font-semibold px-1.5 py-px rounded bg-amber-soft text-amber shrink-0">geen web</span>}
          <div className="flex-1 h-2 bg-panel-2 rounded-full overflow-hidden">
            <div className={`h-full rounded-full ${r.web ? 'bg-accent' : 'bg-amber'}`}
              style={{ width: `${totaal ? (r.bytes / totaal) * 100 : 0}%` }} />
          </div>
          <span className="font-mono text-[11px] text-fg w-[70px] text-right shrink-0">{bytes(r.bytes)}</span>
          <span className="font-mono text-[10.5px] text-fg-faint w-[60px] text-right shrink-0">{r.files}×</span>
        </div>
      ))}
    </div>
  )
}

interface CrawlPaneelProps {
  crawl: MediaCrawlResult | null
  maxPaginas: number
  onMax: (n: number) => void
  onStart: () => void
  bezig: boolean
}

// CrawlPaneel draait de Playwright-crawl. Dit is het enige onderdeel dat kan zien wat
// een pagina écht opvraagt — sliders, pagebuilder-CSS en lazy loading zitten nergens
// in de database. Wat de crawl ziet, gaat daarom boven de databasescan.
function CrawlPaneel({ crawl, maxPaginas, onMax, onStart, bezig }: CrawlPaneelProps) {
  return (
    <div className="bg-panel border border-border rounded-xl p-4">
      <div className="flex items-center gap-2 mb-1.5 flex-wrap">
        <div className="text-[10px] font-semibold tracking-wide text-fg-faint">SITE DOORZOEKEN</div>
        <label className="flex items-center gap-1.5 text-[11px] text-fg-muted ml-auto">
          max
          <input type="number" min={1} max={500} value={maxPaginas}
            onChange={e => onMax(Math.max(1, Math.min(500, Number(e.target.value) || 1)))}
            className="w-[60px] bg-panel-2 border border-border rounded-lg px-2 py-1 text-[12px] text-fg text-right" />
          pagina's
        </label>
        <button onClick={onStart} disabled={bezig}
          className="bg-panel-2 border border-border text-[11.5px] font-semibold text-fg px-3 py-1 rounded-lg hover:bg-hover disabled:opacity-50 transition">
          {bezig ? 'Bezig…' : crawl ? 'Opnieuw doorzoeken' : 'Start met Playwright'}
        </button>
      </div>

      <p className="text-[11.5px] text-fg-muted leading-relaxed">
        Playwright opent de pagina's uit de sitemap, scrolt naar beneden zodat lazy-loaded
        beelden ook laden, en legt vast welk bestand echt wordt opgevraagd. Dat is de enige
        manier om media te vinden die via een slider of pagebuilder wordt ingeladen.
      </p>

      {crawl && (
        <div className="mt-2 text-[11.5px] text-fg-muted">
          <div>
            Stand van {new Date(crawl.crawledAt).toLocaleString('nl-NL')} ·{' '}
            <span className="font-mono text-fg">{crawl.pagesVisited}</span> van {crawl.pagesPlanned} pagina's ·{' '}
            <span className="font-mono text-fg">{crawl.uploadsSeen}</span> bestanden gezien
          </div>
          {crawl.unreferencedSeen > 0 && (
            <div className="mt-1 text-amber">
              {crawl.unreferencedSeen} bestanden die de databasescan "ongebruikt" noemde, worden
              wél door de site opgevraagd. Die zijn uitgesloten van quarantaine.
            </div>
          )}
          {(crawl.errors ?? []).length > 0 && (
            <div className="mt-1 text-fg-faint">
              {(crawl.errors ?? []).length} pagina{(crawl.errors ?? []).length === 1 ? '' : "'s"} gaf een fout,
              bijv. {(crawl.errors ?? [])[0]}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

interface QuarantainePaneelProps {
  batches: QuarantineBatch[] | null
  bezig: boolean
  onOphalen: () => void
  onHerstel: (batch: string) => void
}

// QuarantainePaneel toont wat er van de server af is gehaald en biedt per batch een
// herstelknop. Dat terugzetten één handeling is, is de reden dat verplaatsen
// verantwoord is waar verwijderen dat niet zou zijn.
function QuarantainePaneel({ batches, bezig, onOphalen, onHerstel }: QuarantainePaneelProps) {
  return (
    <div className="bg-panel border border-border rounded-xl p-4">
      <div className="flex items-center gap-2 mb-2">
        <div className="text-[10px] font-semibold tracking-wide text-fg-faint">QUARANTAINE</div>
        <button onClick={onOphalen} disabled={bezig}
          className="ml-auto text-[11.5px] text-fg-muted border border-border rounded-lg px-2.5 py-1 hover:bg-hover transition disabled:opacity-50">
          {bezig ? 'Bezig…' : batches === null ? 'Ophalen' : 'Vernieuwen'}
        </button>
      </div>

      {batches === null ? (
        <p className="text-[11.5px] text-fg-faint">
          Nog niet opgehaald. De lijst komt van de server, dus dat kost één verbinding.
        </p>
      ) : batches.length === 0 ? (
        <p className="text-[11.5px] text-fg-faint">Niets in quarantaine.</p>
      ) : (
        <div className="divide-y divide-border/40">
          {batches.map(b => (
            <div key={b.batch} className="flex items-center gap-2 py-2">
              <span className="font-mono text-[11.5px] text-fg">{b.batch}</span>
              <span className="text-[11px] text-fg-faint">
                {b.created ? new Date(b.created).toLocaleString('nl-NL') : ''}
              </span>
              <span className="ml-auto font-mono text-[11.5px] text-fg-muted">{b.files}×</span>
              <span className="font-mono text-[11.5px] text-fg w-[70px] text-right">{bytes(b.bytes)}</span>
              <button onClick={() => onHerstel(b.batch)} disabled={bezig}
                className="text-[11.5px] text-fg-muted border border-border rounded-lg px-2.5 py-1 hover:bg-hover transition disabled:opacity-50">
                Terugzetten
              </button>
            </div>
          ))}
        </div>
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

interface CategorieBlokProps {
  projectId: string
  scanId: string
  blok: MediaCategoryResult
  gekozen: Set<string>
  onToggleRij: (pad: string) => void
  onToggleZichtbaar: (paden: string[], aan: boolean) => void
}

function CategorieBlok({ projectId, scanId, blok, gekozen, onToggleRij, onToggleZichtbaar }: CategorieBlokProps) {
  const [open, setOpen] = useState(false)
  const [rijen, setRijen] = useState<MediaFileRow[]>(blok.samples ?? [])
  const [meerBezig, setMeerBezig] = useState(false)
  const [filter, setFilter] = useState('')

  const uitleg = CATEGORIE_UITLEG[blok.category] ?? { titel: blok.category, uitleg: '' }
  // Alleen categorieën waarvan de backend verplaatsen toestaat krijgen vinkjes; de
  // UI en de poort in de service moeten hetzelfde zeggen.
  const selecteerbaar = blok.category === MediaCategory.MediaUnreferenced || blok.category === MediaCategory.MediaOrphanFile

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
      const volgende = await Services.MediaService.ScanDetail(projectId, scanId, blok.category, '', rijen.length, 500)
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
            <div className="flex items-center gap-2 mb-2">
              <input type="search" value={filter} onChange={e => setFilter(e.target.value)}
                placeholder="Zoek of filter op map, bijv. 2020/01"
                className="flex-1 bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12px] text-fg" />
              {selecteerbaar && zichtbaar.length > 0 && (
                <button
                  onClick={() => onToggleZichtbaar(zichtbaar.map(r => r.path), !zichtbaar.every(r => gekozen.has(r.path)))}
                  className="text-[11.5px] text-fg-muted border border-border rounded-lg px-2.5 py-1.5 hover:bg-hover transition whitespace-nowrap">
                  {zichtbaar.every(r => gekozen.has(r.path)) ? 'selectie wissen' : `selecteer ${zichtbaar.length} zichtbare`}
                </button>
              )}
            </div>
          )}

          <div className="divide-y divide-border/40">
            {zichtbaar.map((r, i) => (
              <div key={`${r.path}-${i}`} className="flex items-center gap-2 py-1.5">
                {selecteerbaar && (
                  <input type="checkbox" checked={gekozen.has(r.path)} onChange={() => onToggleRij(r.path)}
                    className="shrink-0 accent-accent" />
                )}
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
  const [gekozen, setGekozen] = useState<Set<string>>(new Set())
  const [minLeeftijd, setMinLeeftijd] = useState(90)
  const [batches, setBatches] = useState<QuarantineBatch[] | null>(null)
  const [qBezig, setQBezig] = useState(false)
  const [qMelding, setQMelding] = useState<string | null>(null)
  const [crawl, setCrawl] = useState<MediaCrawlResult | null>(null)
  const [maxPaginas, setMaxPaginas] = useState(60)
  const [bezig, setBezig] = useState(false)
  const [probeTekst, setProbeTekst] = useState<string | null>(null)
  const [fout, setFout] = useState<string | null>(null)

  useEffect(() => {
    setSite(null); setScan(null); setFout(null); setProbeTekst(null); setEnvId('')

    Services.KinstaService.GetLinkedSiteID(projectId)
      .then(id => (id ? Services.KinstaService.GetSiteDetails(id).then(setSite) : undefined))
      .catch(e => setFout(foutTekst(e)))

    setWachtwoord(''); setScans([]); setSelectie(new Set())
    setGekozen(new Set()); setBatches(null); setQMelding(null); setCrawl(null)
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
    setGekozen(new Set()); setBatches(null); setQMelding(null); setCrawl(null)
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

  // De crawl-samenvatting hoort bij de scan die je bekijkt, dus die volgt de scan.
  useEffect(() => {
    if (!scan) { setCrawl(null); return }
    let levend = true
    Services.MediaService.CrawlSummary(projectId, scan.id)
      .then(c => { if (levend) setCrawl(c ?? null) })
      .catch(() => { if (levend) setCrawl(null) })
    return () => { levend = false }
  }, [projectId, scan])

  const doorzoekSite = async () => {
    if (!scan) return
    setQBezig(true); setFout(null); setQMelding(null)
    try {
      const res = await Services.MediaService.CrawlSite(projectId, envId, scan.id, maxPaginas)
      setCrawl(res)
      setQMelding(
        `${res.pagesVisited} pagina's bezocht · ${res.uploadsSeen} bestanden echt opgevraagd` +
        (res.unreferencedSeen > 0
          ? ` · ${res.unreferencedSeen} daarvan stonden als "geen referentie gevonden" — die zijn nu uitgesloten van quarantaine`
          : ''),
      )
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setQBezig(false)
    }
  }

  const toggleRij = (pad: string) => {
    setGekozen(huidig => {
      const volgende = new Set(huidig)
      if (volgende.has(pad)) {
        volgende.delete(pad)
      } else {
        volgende.add(pad)
      }
      return volgende
    })
  }

  const toggleZichtbaar = (paden: string[], aan: boolean) => {
    setGekozen(huidig => {
      const volgende = new Set(huidig)
      paden.forEach(p => (aan ? volgende.add(p) : volgende.delete(p)))
      return volgende
    })
  }

  const haalBatches = useCallback(async () => {
    setQBezig(true); setFout(null)
    try {
      setBatches(await Services.MediaService.ListQuarantine(projectId, envId) ?? [])
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setQBezig(false)
    }
  }, [projectId, envId])

  const naarQuarantaine = async () => {
    if (!scan) return
    const paden = Array.from(gekozen)
    const bevestigd = window.confirm(
      `${paden.length} bestand(en) worden verplaatst naar een quarantainemap buiten de webroot.\n\n` +
      `De site kan ze daarna niet meer opvragen — dat is de bedoeling, zo zie je wat er stuk gaat. ` +
      `Terugzetten kan met één knop zolang de batch in quarantaine staat.\n\nDoorgaan?`,
    )
    if (!bevestigd) return

    setQBezig(true); setFout(null); setQMelding(null)
    try {
      const res = await Services.MediaService.QuarantineFiles(projectId, envId, scan.id, paden, minLeeftijd)
      const overgeslagen = (res.skipped ?? []).length
      setQMelding(
        `${(res.moved ?? []).length} bestand(en) verplaatst (${bytes(res.bytes)}) naar batch ${res.batch}` +
        (overgeslagen > 0 ? ` · ${overgeslagen} overgeslagen: ${(res.skipped ?? [])[0]?.reason ?? ''}` : ''),
      )
      setGekozen(new Set())
      await haalBatches()
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setQBezig(false)
    }
  }

  const herstelBatch = async (batch: string) => {
    if (!window.confirm(`Batch ${batch} terugzetten op de oorspronkelijke plek?`)) return
    setQBezig(true); setFout(null); setQMelding(null)
    try {
      const res = await Services.MediaService.RestoreQuarantine(projectId, envId, batch)
      setQMelding(`${(res.moved ?? []).length} bestand(en) teruggezet uit batch ${batch}`)
      await haalBatches()
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setQBezig(false)
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

  const inGebruikBlok = (scan?.categories ?? []).find(c => c.category === MediaCategory.MediaInUse)
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
              {inGebruikBlok && (
                <Stat label="Site gebruikt hiervan" waarde={bytes(inGebruikBlok.bytes)}
                  sub={scan.totalBytes ? `${(inGebruikBlok.bytes / scan.totalBytes * 100).toFixed(1)}% van de map` : undefined} />
              )}
              {scan.diskUsageBytes > 0 && (
                <Stat label="Volgens du" waarde={bytes(scan.diskUsageBytes)} sub="blokken op schijf" />
              )}
            </div>

            {gekozen.size > 0 && (
              <div className="sticky top-0 z-10 mb-3 flex items-center gap-2.5 bg-panel-2 border border-amber/40 rounded-xl px-4 py-2.5">
                <span className="text-[12.5px] font-semibold text-fg">{gekozen.size} bestand{gekozen.size === 1 ? '' : 'en'} gekozen</span>
                <button onClick={() => setGekozen(new Set())} className="text-[11.5px] text-fg-muted hover:text-fg transition">
                  selectie wissen
                </button>
                <label className="ml-auto flex items-center gap-1.5 text-[11.5px] text-fg-muted"
                  title="Bestanden jonger dan dit aantal dagen worden geweigerd. Verse uploads zijn het meest kansrijk om ergens vandaan te worden opgevraagd.">
                  ouder dan
                  <input type="number" min={0} value={minLeeftijd}
                    onChange={e => setMinLeeftijd(Math.max(0, Number(e.target.value) || 0))}
                    className="w-[60px] bg-panel border border-border rounded-lg px-2 py-1 text-[12px] text-fg text-right" />
                  dagen
                </label>
                <button onClick={naarQuarantaine} disabled={qBezig}
                  className="bg-amber text-white text-[12.5px] font-semibold px-4 py-1.5 rounded-lg hover:brightness-110 disabled:opacity-50 transition">
                  {qBezig ? 'Bezig…' : 'In quarantaine plaatsen'}
                </button>
              </div>
            )}

            {qMelding && (
              <div className="mb-3 bg-green-soft text-green px-3 py-2 rounded-lg text-[11.5px]">{qMelding}</div>
            )}

            <div className="flex flex-col gap-2.5 mb-4">
              {(scan.categories ?? []).map(blok => (
                <CategorieBlok key={blok.category} projectId={projectId} scanId={scan.id} blok={blok}
                  gekozen={gekozen} onToggleRij={toggleRij} onToggleZichtbaar={toggleZichtbaar} />
              ))}
            </div>

            <div className="flex flex-col gap-2.5">
              <CrawlPaneel crawl={crawl} maxPaginas={maxPaginas} onMax={setMaxPaginas}
                onStart={doorzoekSite} bezig={qBezig} />
              <QuarantainePaneel batches={batches} bezig={qBezig} onOphalen={haalBatches} onHerstel={herstelBatch} />
              <TypePaneel rijen={scan.byExtension ?? []} totaal={scan.totalBytes} />
              <MappenPaneel
                rijen={mappenlijst}
                herkomst={mappenHerkomst}
                selectie={selectie}
                onToggle={toggleMap}
                onScan={() => scanNu(Array.from(selectie))}
                bezig={bezig}
                projectId={projectId}
                scanId={scan.id}
                uploadsUrl={scan.scope.uploadsUrl}
                gekozen={gekozen}
                onToggleRij={toggleRij}
                onToggleZichtbaar={toggleZichtbaar}
              />
              <ScopeBlok scan={scan} />
            </div>
          </>
        )}
      </div>
    </div>
  )
}
