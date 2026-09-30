// KnopTekst houdt de knop even breed als hij bezig is: de tekst blijft
// (onzichtbaar) staan en de spinner ligt eroverheen. Anders verspringen de
// knoppen ernaast midden in de opacity-overgang, en WebKit laat dan
// spookkopieën van de tekst achter.
export default function KnopTekst({ tekst, bezig }: { tekst: string; bezig: boolean }) {
  return (
    <span className="relative inline-flex items-center justify-center">
      <span className={bezig ? 'invisible' : undefined}>{tekst}</span>
      {bezig && (
        <span className="absolute inset-0 flex items-center justify-center" aria-label="bezig">
          <span className="animate-spin inline-block">↻</span>
        </span>
      )}
    </span>
  )
}
