// Versienummers als 7.2.1 vergelijken per numeriek deel; een ontbrekend deel
// telt als 0, dus 7.2 == 7.2.0. Negatief als a < b, 0 bij gelijk, positief als a > b.
export function vergelijkVersie(a: string, b: string): number {
  const pa = a.split('.').map(d => parseInt(d, 10) || 0)
  const pb = b.split('.').map(d => parseInt(d, 10) || 0)
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const verschil = (pa[i] ?? 0) - (pb[i] ?? 0)
    if (verschil !== 0) return verschil
  }
  return 0
}

// De hoogste versie uit de lijst, of undefined bij een lege lijst.
export function hoogsteVersie(versies: string[]): string | undefined {
  return versies.reduce<string | undefined>((max, v) => (max === undefined || vergelijkVersie(v, max) > 0 ? v : max), undefined)
}
