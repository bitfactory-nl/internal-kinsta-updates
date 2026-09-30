import WPSiteForm from './WPSiteForm'
import SiteUpdatesPanel from './SiteUpdatesPanel'
import type { WPSite } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import type { WPSiteView } from '../../bindings/github.com/rdm/sites-tool/internal/services/models'

interface Props {
  view: WPSiteView | null
  onSaved: (v: WPSiteView) => void
  onDeleted: () => void
  onUnlinked: () => void
}

const leeg = { id: '', name: '', url: 'https://', username: 'mcp-bot', tags: [], readOnly: false, bridge: true } as unknown as WPSite

export default function WPSitePage({ view, onSaved, onDeleted, onUnlinked }: Props) {
  return (
    <div className="flex-1 flex flex-col min-h-0 overflow-hidden">
      <div className="h-[56px] px-[22px] border-b border-border bg-panel shrink-0 flex items-center gap-2">
        <h2 className="text-[15px] font-bold tracking-[-.01em] text-fg flex-1">
          {view ? view.site.name : 'Nieuwe WordPress-site'}
        </h2>
      </div>
      <div className="flex-1 min-h-0 overflow-y-auto px-6 py-4">
        <div className="bg-panel border border-border rounded-xl p-4 max-w-[560px]">
          {view?.projectMissing ? (
            <p className="text-[11.5px] text-amber mb-3">
              De gekoppelde map is niet gevonden. Ontkoppel de site om hem als losse site te houden, of koppel hem opnieuw vanuit de projectinstellingen.
            </p>
          ) : (
            <p className="text-[10.5px] text-fg-faint mb-3">
              Losse site zonder projectmap. Gegevens staan in ~/.config/rdm/wp-sites.yml, het wachtwoord in de Keychain.
            </p>
          )}
          <WPSiteForm
            initial={view ? view.site : leeg}
            isNew={!view}
            hasPassword={view?.hasPassword ?? false}
            onSaved={onSaved}
            projectMissing={view?.projectMissing}
            onUnlink={view?.projectMissing ? onUnlinked : undefined}
            onDelete={onDeleted}
          />
        </div>
        {view && view.hasPassword && (
          <div className="max-w-[760px] mt-4">
            <SiteUpdatesPanel key={view.site.id} view={view} />
          </div>
        )}
      </div>
    </div>
  )
}
