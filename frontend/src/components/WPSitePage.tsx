import { useState } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { WPSite } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import type { WPSiteView } from '../../bindings/github.com/rdm/sites-tool/internal/services/models'
import WPSiteForm from './WPSiteForm'
import Foutvak from './Foutvak'

// Alleen het formulier voor een nieuwe site; bestaande sites lopen via ProjectDetail.
interface Props {
  onSaved: (v: WPSiteView) => void
}

const leeg = { id: '', name: '', url: 'https://', username: 'mcp-bot', tags: [], readOnly: false, bridge: true } as unknown as WPSite

const knopStil = 'border border-border text-fg-muted text-[12.5px] px-3 py-1.5 rounded-lg hover:bg-hover disabled:opacity-50 transition'

export default function WPSitePage({ onSaved }: Props) {
  const [pad, setPad] = useState('')
  const [fout, setFout] = useState<string | null>(null)

  const kiesMap = async () => {
    setFout(null)
    try {
      const gekozen = await Services.RegisterService.ChooseFolder()
      if (gekozen) setPad(gekozen)
    } catch (e) {
      setFout(e instanceof Error ? e.message : String(e))
    }
  }

  // De site is al aangemaakt zodra het formulier opslaat; de map koppelen we daarna.
  const naOpslaan = async (v: WPSiteView) => {
    if (pad) {
      try {
        await Services.RegisterService.LinkSiteFolder(v.site.id, pad)
      } catch (e) {
        setFout(`Site opgeslagen, maar de map koppelen mislukte: ${e instanceof Error ? e.message : String(e)}`)
      }
    }
    onSaved(v)
  }

  return (
    <div className="flex-1 flex flex-col min-h-0 overflow-hidden">
      <div className="h-[56px] px-[22px] border-b border-border bg-panel shrink-0 flex items-center gap-2">
        <h2 className="text-[15px] font-bold tracking-[-.01em] text-fg flex-1">Nieuwe WordPress-site</h2>
      </div>
      <div className="flex-1 min-h-0 overflow-y-auto px-6 py-4">
        <div className="bg-panel border border-border rounded-xl p-4 max-w-[560px]">
          <p className="text-[10.5px] text-fg-faint mb-3">
            Gegevens staan in ~/.config/rdm/wp-sites.yml, het wachtwoord in de Keychain.
          </p>
          {fout && <Foutvak fout={fout} className="mb-3" />}
          <div className="flex items-center gap-2 mb-3">
            <button onClick={kiesMap} className={knopStil}>Map kiezen (optioneel)</button>
            <span className="text-[11.5px] text-fg-faint font-mono truncate" title={pad}>
              {pad || 'geen map gekozen'}
            </span>
          </div>
          <WPSiteForm initial={leeg} isNew hasPassword={false} onSaved={naOpslaan} />
        </div>
      </div>
    </div>
  )
}
