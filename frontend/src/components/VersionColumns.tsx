interface Props {
  /** Versie in de werkmap van de gebruiker. */
  local: string
  /** Versie op de default release-branch van GitHub. */
  github: string
  /** Laatste beschikbare versie (wp.org). */
  latest: string
  /** GitHub-kolom loopt achter op de laatste versie. */
  outdated: boolean
  /** Werkmap loopt achter op GitHub — een pull-hint, geen verouderde site. */
  localBehind: boolean
}

/** Vaste kolombreedte, zodat de cellen in alle rijen uitlijnen. */
const cell = 'font-mono w-[72px] text-right shrink-0'

/**
 * VersionColumns toont lokaal / GitHub / laatste naast elkaar. De
 * verouderd-markering hangt aan de GitHub-kolom (dat is de stand die naar
 * productie gaat); de lokale kolom krijgt alleen een pull-hint.
 */
export default function VersionColumns({ local, github, latest, outdated, localBehind }: Props) {
  return (
    <>
      <span className={`${cell} ${localBehind ? 'text-amber/70' : 'text-fg-faint'}`}
            title={localBehind
              ? 'Lokaal (werkmap) — loopt achter op GitHub, pull om bij te werken'
              : 'Lokaal (werkmap)'}>
        {local || '–'}{localBehind && ' ↓'}
      </span>
      <span className={`${cell} ${outdated ? 'text-amber' : 'text-fg-muted'}`}
            title="GitHub (default release-branch)">
        {github || '–'}
      </span>
      <span className={`${cell} ${outdated ? 'text-fg' : 'text-fg-faint'}`}
            title="Laatste beschikbare versie">
        {latest || '–'}
      </span>
    </>
  )
}

/** VersionColumnsHeader is de bijbehorende kolomkop. */
export function VersionColumnsHeader() {
  return (
    <>
      <span className={`${cell} text-[10px] uppercase tracking-[.08em] text-fg-faint`}>Lokaal</span>
      <span className={`${cell} text-[10px] uppercase tracking-[.08em] text-fg-faint`}>GitHub</span>
      <span className={`${cell} text-[10px] uppercase tracking-[.08em] text-fg-faint`}>Laatste</span>
    </>
  )
}
