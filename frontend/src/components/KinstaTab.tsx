import { useState, useEffect } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { SiteDetails, EnvironmentDetails, Site } from '../../bindings/github.com/rdm/sites-tool/internal/adapters/kinsta/models'

interface Props { projectId: string }

export default function KinstaTab({ projectId }: Props) {
  const [configured, setConfigured] = useState<boolean | null>(null)
  const [linkedSiteId, setLinkedSiteId] = useState<string | null>(null)

  // Site picker state (when no site is linked yet)
  const [allSites, setAllSites] = useState<Site[] | null>(null)
  const [loadingSites, setLoadingSites] = useState(false)
  const [linking, setLinking] = useState(false)
  const [siteFilter, setSiteFilter] = useState('')

  // Site detail state
  const [site, setSite] = useState<SiteDetails | null>(null)
  const [loadingSite, setLoadingSite] = useState(false)
  const [siteError, setSiteError] = useState<string | null>(null)

  const [selectedEnvId, setSelectedEnvId] = useState<string | null>(null)
  const [envDetails, setEnvDetails] = useState<EnvironmentDetails | null>(null)
  const [loadingEnv, setLoadingEnv] = useState(false)
  const [envError, setEnvError] = useState<string | null>(null)

  // On project change: check if API key is configured + whether a site is linked
  useEffect(() => {
    setSite(null)
    setSiteError(null)
    setAllSites(null)
    setSelectedEnvId(null)
    setEnvDetails(null)
    setLinkedSiteId(null)

    Services.KinstaService.IsConfigured().then(cfg => {
      setConfigured(cfg)
      if (!cfg) return

      Services.KinstaService.GetLinkedSiteID(projectId).then(id => {
        if (id) {
          setLinkedSiteId(id)
          loadSite(id)
        } else {
          // No site linked yet — load all sites for the picker
          fetchAllSites()
        }
      }).catch(() => fetchAllSites())
    }).catch(() => setConfigured(false))
  }, [projectId])

  const fetchAllSites = () => {
    setLoadingSites(true)
    Services.KinstaService.ListSites()
      .then(sites => setAllSites(sites ?? []))
      .catch(e => setSiteError(String(e)))
      .finally(() => setLoadingSites(false))
  }

  const loadSite = (siteId: string) => {
    setLoadingSite(true)
    setSiteError(null)
    Services.KinstaService.GetSiteDetails(siteId)
      .then(s => setSite(s))
      .catch(e => setSiteError(String(e)))
      .finally(() => setLoadingSite(false))
  }

  const linkSite = async (siteId: string) => {
    setLinking(true)
    try {
      await Services.KinstaService.LinkSite(projectId, siteId)
      setLinkedSiteId(siteId)
      setAllSites(null)
      loadSite(siteId)
    } catch (e) {
      setSiteError(String(e))
    } finally {
      setLinking(false)
    }
  }

  const unlink = () => {
    setLinkedSiteId(null)
    setSite(null)
    setSiteError(null)
    setSelectedEnvId(null)
    setEnvDetails(null)
    fetchAllSites()
  }

  const loadEnv = async (envId: string) => {
    setSelectedEnvId(envId)
    setEnvDetails(null)
    setEnvError(null)
    setLoadingEnv(true)
    try {
      const d = await Services.KinstaService.GetEnvironmentPluginsAndThemes(envId)
      setEnvDetails(d)
    } catch (e) {
      setEnvError(String(e))
    } finally {
      setLoadingEnv(false)
    }
  }

  // ── Not configured ──────────────────────────────────────────────────────────
  if (configured === false) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center gap-3 p-8 text-center">
        <p className="text-fg text-[15px] font-semibold">Kinsta niet geconfigureerd</p>
        <p className="text-fg-muted text-[13px]">Voeg je API key toe via ⚙ Instellingen.</p>
      </div>
    )
  }

  if (configured === null) {
    return <Spinner />
  }

  // ── Site picker ──────────────────────────────────────────────────────────────
  if (!linkedSiteId && !loadingSite) {
    const filtered = (allSites ?? []).filter(s =>
      !siteFilter || s.name?.toLowerCase().includes(siteFilter.toLowerCase()) ||
      s.display_name?.toLowerCase().includes(siteFilter.toLowerCase())
    )
    return (
      <div className="flex flex-col flex-1 min-h-0 overflow-y-auto">
        <div className="px-6 py-5 pb-10">
          <p className="text-[12.5px] text-fg-muted mb-3.5">
            Koppel dit project aan een Kinsta site om de dashboard te activeren.
            De keuze wordt opgeslagen in <code className="text-fg font-mono">.rdm.yml</code>.
          </p>
          <input
            type="search"
            placeholder="Filter sites…"
            value={siteFilter}
            onChange={e => setSiteFilter(e.target.value)}
            className="w-full max-w-[520px] bg-panel border border-border rounded-[9px] px-3 py-2
                       text-[13px] text-fg placeholder-fg-faint outline-none
                       focus:border-accent focus:ring-1 focus:ring-accent/30 mb-4 block"
          />

          {loadingSites && <Spinner />}
          {siteError && (
            <div className="mb-4 bg-red-soft text-red border border-border px-3 py-2 rounded-[9px] text-[12.5px]">
              {siteError}
            </div>
          )}

          {!loadingSites && (
            <div className="bg-panel border border-border rounded-[11px] overflow-hidden divide-y divide-border">
              {filtered.map(s => (
                <button
                  key={s.id}
                  onClick={() => linkSite(s.id)}
                  disabled={linking}
                  className="w-full text-left px-4 py-3 hover:bg-hover transition-colors flex items-center gap-3"
                >
                  <span className={`w-2 h-2 rounded-full shrink-0 ${s.status === 'live' ? 'bg-green' : 'bg-fg-faint'}`} />
                  <span className="text-[13px] font-medium text-fg flex-1 truncate">{s.display_name || s.name}</span>
                  {s.id === linkedSiteId && (
                    <span className="text-[10px] font-semibold tracking-[.03em] text-accent bg-accent-soft px-2 py-[3px] rounded-[5px] shrink-0">
                      LINKED
                    </span>
                  )}
                  <span className="text-[12px] text-fg-faint font-mono shrink-0">{s.name}</span>
                </button>
              ))}
              {filtered.length === 0 && (
                <p className="text-[13px] text-fg-faint italic text-center py-8">Geen sites gevonden</p>
              )}
            </div>
          )}
        </div>
      </div>
    )
  }

  // ── Loading site details ─────────────────────────────────────────────────────
  if (loadingSite) return <Spinner />
  if (siteError) return (
    <div className="m-4 bg-red-soft text-red border border-border px-3 py-2 rounded-[9px] text-[12.5px]">{siteError}</div>
  )
  if (!site) return null

  // ── Site detail view ─────────────────────────────────────────────────────────
  const updateCount = envDetails
    ? envDetails.plugins.filter(p => p.update === 'available').length +
      envDetails.themes.filter(t => t.update === 'available').length
    : 0

  const vulnerableCount = envDetails
    ? envDetails.plugins.filter(p => p.is_version_vulnerable).length +
      envDetails.themes.filter(t => t.is_version_vulnerable).length
    : 0

  return (
    <div className="flex flex-1 min-h-0 overflow-hidden">
      {/* Left: environment list */}
      <div className="w-[220px] shrink-0 border-r border-border flex flex-col overflow-hidden">
        <div className="px-3.5 py-3 border-b border-border shrink-0">
          <p className="text-[13px] font-medium text-fg truncate">{site.site.display_name || site.site.name}</p>
          <div className="flex items-center gap-1.5 mt-1">
            <span className={`w-2 h-2 rounded-full shrink-0 ${site.site.status === 'live' ? 'bg-green' : 'bg-fg-faint'}`} />
            <span className="text-[11px] text-fg-muted">{site.site.status}</span>
            <button
              onClick={unlink}
              className="ml-auto text-[11px] text-fg-faint hover:text-fg transition-colors"
              title="Andere site kiezen"
            >
              ↩
            </button>
          </div>
        </div>
        <div className="flex-1 overflow-y-auto py-1">
          {(site.environments ?? []).map(env => (
            <button
              key={env.id}
              onClick={() => loadEnv(env.id)}
              className={`w-full text-left px-3.5 py-2.5 border-b border-border transition-colors
                ${selectedEnvId === env.id ? 'bg-sel' : 'hover:bg-hover'}`}
            >
              <div className="flex items-center gap-1.5">
                <span className={`w-2 h-2 rounded-full shrink-0 ${env.is_blocked ? 'bg-red' : 'bg-green'}`} />
                <span className="text-[12.5px] text-fg truncate">{env.display_name || env.name}</span>
                {env.name === 'live' && (
                  <span className="ml-auto text-[10px] font-semibold font-mono text-accent bg-accent-soft px-1.5 py-px rounded-full shrink-0">live</span>
                )}
              </div>
              {env.container_info?.php_engine_version && (
                <p className="text-[11px] text-fg-faint font-mono mt-0.5 pl-3.5">
                  PHP {env.container_info.php_engine_version.replace('php', '')}
                </p>
              )}
            </button>
          ))}
        </div>
      </div>

      {/* Right: environment detail */}
      <div className="flex-1 min-w-0 flex flex-col overflow-hidden">
        {!selectedEnvId ? (
          <div className="flex-1 flex items-center justify-center text-fg-faint text-[13px] italic">
            Selecteer een omgeving
          </div>
        ) : loadingEnv ? (
          <Spinner />
        ) : envError ? (
          <div className="m-4 bg-red-soft text-red border border-border px-3 py-2 rounded-[9px] text-[12.5px]">{envError}</div>
        ) : envDetails ? (
          <div className="flex-1 overflow-y-auto px-6 py-5 space-y-5">
            {/* Environment info — from already-loaded site.environments */}
            {(() => {
              const env = (site.environments ?? []).find(e => e.id === selectedEnvId)
              if (!env) return null
              const phpVersion = env.container_info?.php_engine_version?.replace('php', '') || '—'
              return (
                <div className="bg-panel border border-border rounded-[11px] p-4">
                  <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">Omgeving</h3>
                  <div className="grid grid-cols-2 gap-1.5 text-[12px]">
                    <span className="text-fg-muted">PHP</span>
                    <span className="text-fg font-mono">{phpVersion}</span>
                    <span className="text-fg-muted">WordPress</span>
                    <span className="text-fg font-mono">{env.wordpress_version || '—'}</span>
                    <span className="text-fg-muted">Status</span>
                    <span className={env.is_blocked ? 'text-red' : 'text-green'}>
                      {env.is_blocked ? 'Geblokkeerd' : 'Actief'}
                    </span>
                    {env.ssh_connection?.ssh_ip?.external_ip && (
                      <>
                        <span className="text-fg-muted">SSH IP</span>
                        <span className="text-fg font-mono">{env.ssh_connection.ssh_ip.external_ip}:{env.ssh_connection.ssh_port}</span>
                      </>
                    )}
                  </div>
                </div>
              )
            })()}

            {vulnerableCount > 0 && (
              <div className="bg-red-soft border border-border rounded-[11px] px-4 py-2.5 text-[12.5px] text-red">
                ⚠ {vulnerableCount} kwetsbare plugin{vulnerableCount !== 1 ? 's/thema\'s' : '/thema'}
              </div>
            )}
            {updateCount > 0 && (
              <div className="bg-amber-soft border border-border rounded-[11px] px-4 py-2.5 text-[12.5px] text-amber">
                ↑ {updateCount} update{updateCount !== 1 ? 's' : ''} beschikbaar
              </div>
            )}

            {/* Plugins */}
            <div>
              <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">
                Plugins · {envDetails.plugins.length}
                {envDetails.plugins.filter(p => p.update === 'available').length > 0 && (
                  <span className="ml-2 text-amber normal-case tracking-normal">
                    {envDetails.plugins.filter(p => p.update === 'available').length} update{envDetails.plugins.filter(p => p.update === 'available').length !== 1 ? 's' : ''}
                  </span>
                )}
              </h3>
              <div className="bg-panel border border-border rounded-[11px] overflow-hidden divide-y divide-border">
                {envDetails.plugins.map(p => (
                  <div key={p.name} className="flex items-center gap-3 px-4 py-2.5 hover:bg-hover transition-colors">
                    <span className={`w-2 h-2 rounded-full shrink-0 ${p.status === 'active' ? 'bg-green' : 'bg-fg-faint'}`} />
                    <span className="text-[13px] font-medium text-fg flex-1 truncate">{p.title || p.name}</span>
                    {p.is_version_vulnerable && (
                      <span className="text-[10px] font-semibold text-red bg-red-soft px-2 py-[3px] rounded-[5px] shrink-0">⚠ kwetsbaar</span>
                    )}
                    <span className="text-[12px] font-mono text-fg-faint shrink-0">{p.version}</span>
                    {p.update === 'available' && (
                      <span className="text-[12px] font-mono font-semibold text-green shrink-0">→ {p.update_version}</span>
                    )}
                  </div>
                ))}
                {envDetails.plugins.length === 0 && (
                  <p className="text-[13px] text-fg-faint italic px-4 py-3">Geen plugins</p>
                )}
              </div>
            </div>

            {/* Themes */}
            <div>
              <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">
                Thema's · {envDetails.themes.length}
                {envDetails.themes.filter(t => t.update === 'available').length > 0 && (
                  <span className="ml-2 text-amber normal-case tracking-normal">
                    {envDetails.themes.filter(t => t.update === 'available').length} update{envDetails.themes.filter(t => t.update === 'available').length !== 1 ? 's' : ''}
                  </span>
                )}
              </h3>
              <div className="bg-panel border border-border rounded-[11px] overflow-hidden divide-y divide-border">
                {envDetails.themes.map(t => (
                  <div key={t.name} className="flex items-center gap-3 px-4 py-2.5 hover:bg-hover transition-colors">
                    <span className={`w-2 h-2 rounded-full shrink-0 ${t.status === 'active' ? 'bg-accent' : 'bg-fg-faint'}`} />
                    <span className="text-[13px] font-medium text-fg flex-1 truncate">{t.title || t.name}</span>
                    {t.is_version_vulnerable && (
                      <span className="text-[10px] font-semibold text-red bg-red-soft px-2 py-[3px] rounded-[5px] shrink-0">⚠ kwetsbaar</span>
                    )}
                    <span className="text-[12px] font-mono text-fg-faint shrink-0">{t.version}</span>
                    {t.update === 'available' && (
                      <span className="text-[12px] font-mono font-semibold text-green shrink-0">→ {t.update_version}</span>
                    )}
                  </div>
                ))}
                {envDetails.themes.length === 0 && (
                  <p className="text-[13px] text-fg-faint italic px-4 py-3">Geen thema's</p>
                )}
              </div>
            </div>
          </div>
        ) : null}
      </div>
    </div>
  )
}

function Spinner() {
  return (
    <div className="flex-1 flex items-center justify-center gap-2 text-fg-faint text-[13px]">
      <span className="animate-spin inline-block">↻</span>
    </div>
  )
}
