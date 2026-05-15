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
        <p className="text-gray-600 text-sm font-medium">Kinsta niet geconfigureerd</p>
        <p className="text-gray-600 text-xs">Voeg je API key toe via ⚙ Instellingen.</p>
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
      <div className="flex flex-col flex-1 min-h-0 overflow-hidden">
        <div className="px-4 py-3 border-b border-black/[0.08] shrink-0">
          <p className="text-xs text-gray-600 mb-2">
            Koppel dit project aan een Kinsta site om de dashboard te activeren.
            De keuze wordt opgeslagen in <code className="text-gray-600 font-mono">.rdm.yml</code>.
          </p>
          <input
            type="search"
            placeholder="Filter sites…"
            value={siteFilter}
            onChange={e => setSiteFilter(e.target.value)}
            className="w-full bg-black/[0.05] text-xs text-gray-800 placeholder-gray-400
                       rounded-lg px-3 py-1.5 outline-none focus:ring-1 focus:ring-indigo-500/40"
          />
        </div>

        {loadingSites && <Spinner />}
        {siteError && <div className="m-4 bg-red-100 text-red-600 px-3 py-2 rounded text-xs">{siteError}</div>}

        <div className="flex-1 overflow-y-auto divide-y divide-black/[0.06]">
          {filtered.map(s => (
            <button
              key={s.id}
              onClick={() => linkSite(s.id)}
              disabled={linking}
              className="w-full text-left px-4 py-2.5 hover:bg-black/[0.05] transition-colors flex items-center gap-3"
            >
              <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${s.status === 'live' ? 'bg-emerald-400' : 'bg-gray-500'}`} />
              <span className="text-sm text-gray-800 flex-1 truncate">{s.display_name || s.name}</span>
              <span className="text-[10px] text-gray-600 font-mono shrink-0">{s.name}</span>
            </button>
          ))}
          {!loadingSites && filtered.length === 0 && (
            <p className="text-xs text-gray-600 text-center py-8">Geen sites gevonden</p>
          )}
        </div>
      </div>
    )
  }

  // ── Loading site details ─────────────────────────────────────────────────────
  if (loadingSite) return <Spinner />
  if (siteError) return <div className="m-4 bg-red-100 text-red-600 px-3 py-2 rounded text-xs">{siteError}</div>
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
      <div className="w-[200px] shrink-0 border-r border-black/[0.08] flex flex-col overflow-hidden">
        <div className="px-3 py-2 border-b border-black/[0.06] shrink-0">
          <p className="text-xs font-medium text-gray-900 truncate">{site.site.display_name || site.site.name}</p>
          <div className="flex items-center gap-1.5 mt-0.5">
            <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${site.site.status === 'live' ? 'bg-emerald-400' : 'bg-gray-500'}`} />
            <span className="text-[10px] text-gray-600">{site.site.status}</span>
            <button
              onClick={unlink}
              className="ml-auto text-[10px] text-gray-700 hover:text-gray-800 transition-colors"
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
              className={`w-full text-left px-3 py-2 border-b border-black/[0.06] transition-colors
                ${selectedEnvId === env.id ? 'bg-indigo-100' : 'hover:bg-black/[0.04]'}`}
            >
              <div className="flex items-center gap-1.5">
                <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${env.is_blocked ? 'bg-red-400' : 'bg-emerald-400'}`} />
                <span className="text-xs text-gray-800 truncate">{env.display_name || env.name}</span>
                {env.name === 'live' && <span className="ml-auto text-[9px] text-indigo-700 shrink-0">live</span>}
              </div>
              {env.container_info?.php_engine_version && (
                <p className="text-[10px] text-gray-600 mt-0.5 pl-3">
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
          <div className="flex-1 flex items-center justify-center text-gray-600 text-sm italic">
            Selecteer een omgeving
          </div>
        ) : loadingEnv ? (
          <Spinner />
        ) : envError ? (
          <div className="m-4 bg-red-100 text-red-600 px-3 py-2 rounded text-xs">{envError}</div>
        ) : envDetails ? (
          <div className="flex-1 overflow-y-auto p-3 space-y-4">
            {/* Environment info — from already-loaded site.environments */}
            {(() => {
              const env = (site.environments ?? []).find(e => e.id === selectedEnvId)
              if (!env) return null
              const phpVersion = env.container_info?.php_engine_version?.replace('php', '') || '—'
              return (
                <div className="bg-black/[0.04] rounded-lg p-3">
                  <h3 className="text-xs font-semibold text-gray-900 mb-2">Omgeving</h3>
                  <div className="grid grid-cols-2 gap-1 text-[11px]">
                    <span className="text-gray-600">PHP</span>
                    <span className="text-gray-700">{phpVersion}</span>
                    <span className="text-gray-600">WordPress</span>
                    <span className="text-gray-700">{env.wordpress_version || '—'}</span>
                    <span className="text-gray-600">Status</span>
                    <span className={env.is_blocked ? 'text-red-600' : 'text-emerald-600'}>
                      {env.is_blocked ? 'Geblokkeerd' : 'Actief'}
                    </span>
                    {env.ssh_connection?.ssh_ip?.external_ip && (
                      <>
                        <span className="text-gray-600">SSH IP</span>
                        <span className="text-gray-700 font-mono">{env.ssh_connection.ssh_ip.external_ip}:{env.ssh_connection.ssh_port}</span>
                      </>
                    )}
                  </div>
                </div>
              )
            })()}

            {vulnerableCount > 0 && (
              <div className="bg-red-500/10 border border-red-500/20 rounded-lg px-3 py-2 text-xs text-red-700">
                ⚠ {vulnerableCount} kwetsbare plugin{vulnerableCount !== 1 ? 's/thema\'s' : '/thema'}
              </div>
            )}
            {updateCount > 0 && (
              <div className="bg-amber-500/10 border border-amber-500/20 rounded-lg px-3 py-2 text-xs text-amber-700">
                ↑ {updateCount} update{updateCount !== 1 ? 's' : ''} beschikbaar
              </div>
            )}

            {/* Plugins */}
            <div>
              <h3 className="text-xs font-semibold text-gray-900 mb-2">
                Plugins <span className="text-gray-600 font-normal">{envDetails.plugins.length}</span>
                {envDetails.plugins.filter(p => p.update === 'available').length > 0 && (
                  <span className="ml-2 text-amber-500 font-normal">
                    {envDetails.plugins.filter(p => p.update === 'available').length} update{envDetails.plugins.filter(p => p.update === 'available').length !== 1 ? 's' : ''}
                  </span>
                )}
              </h3>
              <div className="space-y-px">
                {envDetails.plugins.map(p => (
                  <div key={p.name} className="flex items-center gap-2 py-1 px-2 rounded hover:bg-black/[0.03]">
                    <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${p.status === 'active' ? 'bg-emerald-400' : 'bg-gray-600'}`} />
                    <span className="text-[11px] text-gray-700 flex-1 truncate">{p.title || p.name}</span>
                    {p.is_version_vulnerable && <span className="text-[10px] text-red-600 shrink-0">⚠ kwetsbaar</span>}
                    <span className="text-[10px] font-mono text-gray-600 shrink-0">{p.version}</span>
                    {p.update === 'available' && (
                      <span className="text-[10px] text-amber-500 shrink-0">→ {p.update_version}</span>
                    )}
                  </div>
                ))}
                {envDetails.plugins.length === 0 && <p className="text-xs text-gray-600 italic px-2">Geen plugins</p>}
              </div>
            </div>

            {/* Themes */}
            <div>
              <h3 className="text-xs font-semibold text-gray-900 mb-2">
                Thema's <span className="text-gray-600 font-normal">{envDetails.themes.length}</span>
                {envDetails.themes.filter(t => t.update === 'available').length > 0 && (
                  <span className="ml-2 text-amber-500 font-normal">
                    {envDetails.themes.filter(t => t.update === 'available').length} update{envDetails.themes.filter(t => t.update === 'available').length !== 1 ? 's' : ''}
                  </span>
                )}
              </h3>
              <div className="space-y-px">
                {envDetails.themes.map(t => (
                  <div key={t.name} className="flex items-center gap-2 py-1 px-2 rounded hover:bg-black/[0.03]">
                    <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${t.status === 'active' ? 'bg-indigo-400' : 'bg-gray-600'}`} />
                    <span className="text-[11px] text-gray-700 flex-1 truncate">{t.title || t.name}</span>
                    {t.is_version_vulnerable && <span className="text-[10px] text-red-600 shrink-0">⚠ kwetsbaar</span>}
                    <span className="text-[10px] font-mono text-gray-600 shrink-0">{t.version}</span>
                    {t.update === 'available' && (
                      <span className="text-[10px] text-amber-500 shrink-0">→ {t.update_version}</span>
                    )}
                  </div>
                ))}
                {envDetails.themes.length === 0 && <p className="text-xs text-gray-600 italic px-2">Geen thema's</p>}
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
    <div className="flex-1 flex items-center justify-center gap-2 text-gray-600 text-sm">
      <span className="animate-spin inline-block">↻</span>
    </div>
  )
}
