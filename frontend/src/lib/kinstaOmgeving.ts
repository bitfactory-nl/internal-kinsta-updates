import { useEffect, useState } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'

// standaardOmgeving kiest de omgeving als de gebruiker nog niets koos: de
// omgeving die bij de site is ingesteld, anders live, anders de eerste.
export function standaardOmgeving<T extends { id: string; name: string }>(envs: T[], voorkeur?: string | null): T | undefined {
  return (voorkeur ? envs.find(e => e.id === voorkeur) : undefined)
    ?? envs.find(e => e.name === 'live')
    ?? envs[0]
}

// useVoorkeurOmgeving haalt de standaardomgeving van de site op. null zolang
// die nog laadt, zodat een tab niet eerst live kiest en daarna niet meer wisselt.
export function useVoorkeurOmgeving(projectId: string): string | null {
  const [voorkeur, setVoorkeur] = useState<string | null>(null)
  useEffect(() => {
    let actueel = true
    setVoorkeur(null)
    Services.KinstaService.LinkedEnvID(projectId)
      .then(id => { if (actueel) setVoorkeur(id ?? '') })
      .catch(() => { if (actueel) setVoorkeur('') })
    return () => { actueel = false }
  }, [projectId])
  return voorkeur
}
