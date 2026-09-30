import { useState, useEffect, useRef } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { Project } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import Foutvak from './Foutvak'
import KnopTekst from './KnopTekst'
import { bevestig } from '../lib/bevestig'
import WPSiteForm from './WPSiteForm'
import type { WPSite } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import type { WPSiteView } from '../../bindings/github.com/rdm/sites-tool/internal/services/models'

interface Props {
  project: Project
  // Wijziging die het project-ID niet raakt: parent herlaadt het register.
  onLocalUrlSaved: () => void
  // Wijziging die het project-ID verandert (map gekoppeld/ontkoppeld, site
  // aangemaakt) of laat verdwijnen (uit de lijst verwijderd). Met siteId opent
  // de parent de rij van die site opnieuw; zonder valt de selectie weg.
  onRegisterChanged: (siteId?: string) => void
}

const knopStil = 'border border-border text-fg-muted text-[12.5px] px-3 py-1.5 rounded-lg hover:bg-hover disabled:opacity-50 transition'

function foutTekst(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

export default function ProjectSettingsTab({ project, onLocalUrlSaved, onRegisterChanged }: Props) {
  const heeftMap = project.local && !project.missing
  const [localURL, setLocalURL] = useState('')
  const [opslaanBezig, setOpslaanBezig] = useState(false)
  const [opgeslagen, setOpgeslagen] = useState(false)

  const [sshUser, setSshUser] = useState('')
  const [sshPad, setSshPad] = useState('')
  const [sshWachtwoord, setSshWachtwoord] = useState('')
  const [heeftWachtwoord, setHeeftWachtwoord] = useState(false)
  const [sshOpslaanBezig, setSshOpslaanBezig] = useState(false)

  const [fout, setFout] = useState<string | null>(null)
  const [wpKoppeling, setWpKoppeling] = useState<WPSiteView | null>(null)
  const [wpVoorstel, setWpVoorstel] = useState<WPSite | null>(null)
  const [wpBridge, setWpBridge] = useState(false)
  const [wpLos, setWpLos] = useState<WPSiteView | null>(null)
  const [mapActie, setMapActie] = useState<'kiezen' | 'ontkoppelen' | 'verwijderen' | 'omzetten' | null>(null)
  const [omzetVoorstel, setOmzetVoorstel] = useState<WPSite | null>(null)

  useEffect(() => {
    setFout(null); setOpgeslagen(false); setOmzetVoorstel(null)
    if (!heeftMap) {
      // zonder map zijn er geen lokale URL en geen SSH-pad te lezen
      setLocalURL(''); setSshUser(''); setSshPad(''); setHeeftWachtwoord(false); setSshWachtwoord('')
      return
    }

    const bestaandeLocal = project.deploy?.link?.local
    if (bestaandeLocal) {
      setLocalURL(bestaandeLocal)
    } else {
      Services.DBCloneService.LocalDefaults(project.id)
        .then(def => setLocalURL(def.url ?? ''))
        .catch(() => {})
    }

    Services.MediaService.GetSSHAccess(project.id)
      .then(a => { setSshUser(a.user ?? ''); setSshPad(a.path ?? ''); setHeeftWachtwoord(a.hasPassword) })
      .catch(() => {})
    setSshWachtwoord('')
  }, [project.id, project.deploy?.link?.local, heeftMap])

  // Elke laadronde krijgt een nummer; antwoorden van een oudere ronde (ander
  // project of eerdere herlaadactie) worden genegeerd.
  const wpRonde = useRef(0)
  const laadWp = async () => {
    const ronde = ++wpRonde.current
    const actueel = () => wpRonde.current === ronde
    setWpKoppeling(null); setWpVoorstel(null); setWpLos(null); setWpBridge(false)
    try {
      const [koppeling, voorstel, bridge] = await Promise.all([
        Services.WPSiteService.ForProject(project.id),
        Services.WPSiteService.SuggestForProject(project.id).catch(() => null),
        heeftMap ? Services.WPSiteService.DetectBridge(project.id).catch(() => false) : Promise.resolve(false),
      ])
      if (!actueel()) return
      let los: WPSiteView | null = null
      if (!koppeling && voorstel) {
        // een eerder ontkoppelde site bestaat nog los: opnieuw koppelen i.p.v. aanmaken
        const alle = await Services.WPSiteService.List().catch(() => [] as WPSiteView[])
        if (!actueel()) return
        los = alle.find(v => !v.site.projectPath && v.site.id === voorstel.id) ?? null
      }
      setWpKoppeling(koppeling ?? null); setWpVoorstel(voorstel); setWpLos(los); setWpBridge(bridge)
    } catch (e) {
      if (actueel()) setFout(foutTekst(e))
    }
  }
  useEffect(() => {
    laadWp()
    return () => { wpRonde.current++ }
  }, [project.id, heeftMap])

  const voerMapActie = async (actie: NonNullable<typeof mapActie>, werk: () => Promise<void>) => {
    setMapActie(actie); setFout(null)
    try {
      await werk()
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setMapActie(null)
    }
  }

  const mapOntkoppelen = async () => {
    if (!(await bevestig('Ontkoppelen?', `De map van "${project.displayName}" ontkoppelen? De site blijft in de lijst staan, zonder map.`))) return
    await voerMapActie('ontkoppelen', async () => {
      await Services.WPSiteService.Unlink(project.siteId!)
      onRegisterChanged(project.siteId)
    })
  }

  const uitLijstVerwijderen = async () => {
    if (!(await bevestig('Uit de lijst verwijderen?', `"${project.displayName}" uit de lijst verwijderen? De map zelf blijft staan.`))) return
    await voerMapActie('verwijderen', async () => {
      await Services.RegisterService.RemoveLocalProject(project.id)
      onRegisterChanged()
    })
  }

  const alsSiteKoppelen = () => voerMapActie('omzetten', async () => {
    setOmzetVoorstel(await Services.RegisterService.ConvertLocalToSite(project.id))
  })

  const mapKiezen = () => voerMapActie('kiezen', async () => {
    const pad = await Services.RegisterService.ChooseFolder()
    if (!pad) return
    await Services.RegisterService.LinkSiteFolder(project.siteId!, pad)
    onRegisterChanged(project.siteId)
  })

  const localURLOpslaan = async () => {
    setOpslaanBezig(true); setFout(null); setOpgeslagen(false)
    try {
      await Services.DBCloneService.SaveLocalURL(project.id, localURL)
      setOpgeslagen(true)
      onLocalUrlSaved()
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setOpslaanBezig(false)
    }
  }

  const sshToegangOpslaan = async () => {
    setSshOpslaanBezig(true); setFout(null)
    try {
      await Services.MediaService.SaveSSHAccess(project.id, sshUser, sshPad, sshWachtwoord)
      if (sshWachtwoord) {
        setHeeftWachtwoord(true)
        setSshWachtwoord('')
      }
    } catch (e) {
      setFout(foutTekst(e))
    } finally {
      setSshOpslaanBezig(false)
    }
  }

  return (
    <div className="flex-1 min-h-0 overflow-y-auto px-6 py-4">
      {fout && <Foutvak fout={fout} className="mb-4" />}

      <div className="bg-panel border border-border rounded-xl p-4 mb-4">
        <div className="text-[12px] font-semibold text-fg mb-3">Lokale map</div>
        <div className="text-[12.5px] font-mono text-fg mb-1 break-all">
          {project.local && project.path ? project.path : <span className="text-fg-muted">niet gekoppeld</span>}
        </div>
        {project.local && project.missing && (
          <p className="text-[11.5px] text-amber mb-2">Map niet gevonden op deze plek.</p>
        )}
        <div className="flex flex-wrap items-center gap-2 mt-2">
          {project.siteId ? (
            <>
              <button onClick={mapKiezen} disabled={mapActie !== null} className={knopStil}>
                <KnopTekst tekst="Map kiezen" bezig={mapActie === 'kiezen'} />
              </button>
              {project.local && (
                <button onClick={mapOntkoppelen} disabled={mapActie !== null} className={knopStil}>
                  <KnopTekst tekst="Ontkoppelen" bezig={mapActie === 'ontkoppelen'} />
                </button>
              )}
            </>
          ) : (
            <>
              <button onClick={alsSiteKoppelen} disabled={mapActie !== null || omzetVoorstel !== null} className={knopStil}>
                <KnopTekst tekst="Als WordPress-site koppelen" bezig={mapActie === 'omzetten'} />
              </button>
              <button onClick={uitLijstVerwijderen} disabled={mapActie !== null} className={`${knopStil} text-red`}>
                <KnopTekst tekst="Uit de lijst verwijderen" bezig={mapActie === 'verwijderen'} />
              </button>
            </>
          )}
        </div>
        {!project.siteId && (
          <p className="text-[10.5px] text-fg-faint mt-2">Uit de lijst verwijderen laat de map zelf staan.</p>
        )}
      </div>

      {omzetVoorstel && (
        <div className="bg-panel border border-border rounded-xl p-4 mb-4">
          <div className="text-[12px] font-semibold text-fg mb-1">Als WordPress-site koppelen</div>
          <p className="text-[10.5px] text-fg-faint mb-3">
            Controleer de gegevens en sla op om dit project als site aan wp-fleet toe te voegen.
          </p>
          <WPSiteForm
            initial={omzetVoorstel}
            isNew
            hasPassword={false}
            onSaved={v => onRegisterChanged(v.site.id)}
          />
          <button onClick={() => setOmzetVoorstel(null)} className={`${knopStil} mt-2`}>Annuleren</button>
        </div>
      )}

      {heeftMap && <>
      <div className="bg-panel border border-border rounded-xl p-4 mb-4">
        <div className="text-[12px] font-semibold text-fg mb-3">Lokale URL</div>
        <input value={localURL} onChange={e => { setLocalURL(e.target.value); setOpgeslagen(false) }}
          className="w-full bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg mb-2 font-mono" />
        <div className="flex items-center gap-2.5">
          <button onClick={localURLOpslaan} disabled={opslaanBezig || !localURL}
            className="bg-accent text-white text-[12.5px] font-semibold px-4 py-1.5 rounded-lg hover:brightness-110 disabled:opacity-50 transition">
            {opslaanBezig ? <span className="animate-spin inline-block">↻</span> : 'Opslaan'}
          </button>
          {opgeslagen && <span className="text-[11.5px] text-green">✓ opgeslagen</span>}
        </div>
        <p className="text-[10.5px] text-fg-faint mt-2">
          opgeslagen in deploy_conf.json (link.local) — committen doe je zelf wanneer je wilt
        </p>
      </div>

      <div className="bg-panel border border-border rounded-xl p-4 mb-4">
        <div className="text-[12px] font-semibold text-fg mb-3">Acceptatie &amp; productie</div>
        <div className="text-[11.5px] text-fg-muted space-y-1.5">
          <div>Acceptatie: <span className="font-mono text-fg">{project.deploy?.link?.acc || '—'}</span></div>
          <div>Productie: <span className="font-mono text-fg">{project.deploy?.link?.prod || '—'}</span></div>
        </div>
        <p className="text-[10.5px] text-fg-faint mt-2">komen uit deploy_conf.json in git — hier niet aan te passen</p>
      </div>

      <div className="bg-panel border border-border rounded-xl p-4 mb-4">
        <div className="text-[12px] font-semibold text-fg mb-3">SSH-toegang</div>
        <label className="block text-[11px] text-fg-muted mb-1">Gebruiker</label>
        <input value={sshUser} onChange={e => setSshUser(e.target.value)} placeholder="SSH-gebruiker"
          className="w-full bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg mb-2" />
        <label className="block text-[11px] text-fg-muted mb-1">Pad (leeg = zelf zoeken)</label>
        <input value={sshPad} onChange={e => setSshPad(e.target.value)} placeholder="/www/site/public"
          className="w-full bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg mb-2 font-mono" />
        <label className="block text-[11px] text-fg-muted mb-1">Wachtwoord</label>
        <input type="password" value={sshWachtwoord} onChange={e => setSshWachtwoord(e.target.value)}
          placeholder={heeftWachtwoord ? 'wachtwoord bewaard' : 'wachtwoord'}
          title="Wordt in de macOS-keychain bewaard; .rdm.yml krijgt alleen een verwijzing. Leeg laten houdt het bestaande wachtwoord."
          className="w-full bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg mb-3" />
        <button onClick={sshToegangOpslaan} disabled={sshOpslaanBezig || !sshUser}
          className="bg-accent text-white text-[12.5px] font-semibold px-4 py-1.5 rounded-lg hover:brightness-110 disabled:opacity-50 transition">
          {sshOpslaanBezig ? <span className="animate-spin inline-block">↻</span> : 'Opslaan'}
        </button>
      </div>
      </>}
      {(project.siteId ? !!wpKoppeling : (wpBridge && !!wpVoorstel)) && (
        <div className="bg-panel border border-border rounded-xl p-4">
          <div className="text-[12px] font-semibold text-fg mb-1">WordPress MCP</div>
          <p className="text-[10.5px] text-fg-faint mb-3">
            {wpKoppeling
              ? 'Gekoppeld aan wp-fleet. Gegevens staan in ~/.config/rdm/wp-sites.yml, het wachtwoord in de Keychain — niet in deze repo.'
              : wpLos
                ? 'Er bestaat al een losse site met dit id — Opslaan koppelt die opnieuw aan dit project.'
                : 'nb-mcp-bridge gevonden in deze checkout. Vul de gegevens in om deze site aan wp-fleet toe te voegen.'}
          </p>
          <WPSiteForm
            initial={wpKoppeling ? wpKoppeling.site : wpLos ? { ...wpLos.site, projectPath: wpVoorstel?.projectPath } as WPSite : wpVoorstel!}
            isNew={!wpKoppeling && !wpLos}
            hasPassword={(wpKoppeling ?? wpLos)?.hasPassword ?? false}
            projectMissing={wpKoppeling?.projectMissing}
            onSaved={v => { setWpKoppeling(v); onLocalUrlSaved() }}
            onUnlink={() => onRegisterChanged(project.siteId)}
          />
        </div>
      )}
    </div>
  )
}
