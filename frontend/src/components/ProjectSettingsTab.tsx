import { useState, useEffect } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { Project } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import Foutvak from './Foutvak'

interface Props {
  project: Project
  onLocalUrlSaved: () => void
}

function foutTekst(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

export default function ProjectSettingsTab({ project, onLocalUrlSaved }: Props) {
  const [localURL, setLocalURL] = useState('')
  const [opslaanBezig, setOpslaanBezig] = useState(false)
  const [opgeslagen, setOpgeslagen] = useState(false)

  const [sshUser, setSshUser] = useState('')
  const [sshPad, setSshPad] = useState('')
  const [sshWachtwoord, setSshWachtwoord] = useState('')
  const [heeftWachtwoord, setHeeftWachtwoord] = useState(false)
  const [sshOpslaanBezig, setSshOpslaanBezig] = useState(false)

  const [fout, setFout] = useState<string | null>(null)

  useEffect(() => {
    setFout(null); setOpgeslagen(false)

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
  }, [project.id, project.deploy?.link?.local])

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

      <div className="bg-panel border border-border rounded-xl p-4">
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
    </div>
  )
}
