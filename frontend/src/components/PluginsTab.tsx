import { useState, useEffect, useCallback } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { SiteDetails } from '../../bindings/github.com/rdm/sites-tool/internal/adapters/kinsta/models'
import type { PluginDiff } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import { DiffStatus, PluginSource } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'

interface Props { projectId: string }

function getErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

export default function PluginsTab({ projectId }: Props) {
  const [configured, setConfigured] = useState<boolean | null>(null)
  const [site, setSite] = useState<SiteDetails | null>(null)
  const [selectedEnvId, setSelectedEnvId] = useState<string | null>(null)
  const [diffs, setDiffs] = useState<PluginDiff[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setSite(null)
    setSelectedEnvId(null)
    setDiffs(null)
    setError(null)

    Services.PluginService.IsConfigured()
      .then(cfg => {
        setConfigured(cfg)
        if (!cfg) return
        return Services.KinstaService.GetLinkedSiteID(projectId).then(id => {
          if (id) return Services.KinstaService.GetSiteDetails(id).then(setSite)
        })
      })
      .catch(e => {
        setConfigured(false)
        setError(getErrorMessage(e))
      })
  }, [projectId])

  const loadDiff = useCallback(async (envId: string) => {
    setSelectedEnvId(envId)
    setDiffs(null)
    setError(null)
    setLoading(true)
    try {
      const result = await Services.PluginService.Diff(envId)
      setDiffs(result ?? [])
    } catch (e: unknown) {
      setError(getErrorMessage(e))
    } finally {
      setLoading(false)
    }
  }, [])

  const refreshManifest = useCallback(async () => {
    setError(null)
    setLoading(true)
    try {
      await Services.PluginService.RefreshIndex()
      if (selectedEnvId) {
        const result = await Services.PluginService.Diff(selectedEnvId)
        setDiffs(result ?? [])
      }
    } catch (e: unknown) {
      setError(getErrorMessage(e))
    } finally {
      setLoading(false)
    }
  }, [selectedEnvId])

  if (configured === null) return <Spinner />

  if (configured === false) {
    return (
      <Empty
        title="Plugin-repo niet geconfigureerd"
        hint="Stel plugin_repo (github_token + repo) in via ⚙ Instellingen."
      />
    )
  }

  if (!site) {
    return (
      <Empty
        title="Geen Kinsta site gekoppeld"
        hint="Koppel dit project eerst aan een Kinsta site via het Kinsta-tabblad."
      />
    )
  }

  const paid = (diffs ?? []).filter(d => d.source === PluginSource.SourcePrivateRepo)
  const vulnerableOther = (diffs ?? []).filter(
    d => d.source !== PluginSource.SourcePrivateRepo && d.isVulnerable,
  )

  return (
    <div className="flex flex-1 min-h-0 overflow-hidden">
      {/* Environment list */}
      <div className="w-[200px] shrink-0 border-r border-black/[0.08] flex flex-col overflow-hidden">
        <div className="px-3 py-2 border-b border-black/[0.06] shrink-0 flex items-center gap-2">
          <p className="text-xs font-medium text-gray-900 truncate flex-1">
            {site.site.display_name || site.site.name}
          </p>
          <button
            onClick={refreshManifest}
            disabled={loading}
            title="Manifest verversen"
            className="text-[11px] text-gray-700 hover:text-gray-900 transition-colors"
          >
            {loading ? <span className="animate-spin inline-block">↻</span> : '⟳'}
          </button>
        </div>
        <div className="flex-1 overflow-y-auto py-1">
          {(site.environments ?? []).map(env => (
            <button
              key={env.id}
              onClick={() => loadDiff(env.id)}
              className={`w-full text-left px-3 py-2 border-b border-black/[0.06] transition-colors
                ${selectedEnvId === env.id ? 'bg-indigo-100' : 'hover:bg-black/[0.04]'}`}
            >
              <div className="flex items-center gap-1.5">
                <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${env.is_blocked ? 'bg-red-400' : 'bg-emerald-400'}`} />
                <span className="text-xs text-gray-800 truncate">{env.display_name || env.name}</span>
                {env.name === 'live' && <span className="ml-auto text-[9px] text-indigo-700 shrink-0">live</span>}
              </div>
            </button>
          ))}
        </div>
      </div>

      {/* Diff detail */}
      <div className="flex-1 min-w-0 flex flex-col overflow-hidden">
        {!selectedEnvId ? (
          <div className="flex-1 flex items-center justify-center text-gray-600 text-sm italic">
            Selecteer een omgeving
          </div>
        ) : loading ? (
          <Spinner />
        ) : error ? (
          <div className="m-4 bg-red-100 text-red-600 px-3 py-2 rounded text-xs">{error}</div>
        ) : diffs ? (
          <div className="flex-1 overflow-y-auto p-3 space-y-4">
            <section>
              <h3 className="text-xs font-semibold text-gray-900 mb-2">
                Betaalde plugins <span className="text-gray-600 font-normal">{paid.length}</span>
              </h3>
              <div className="space-y-px">
                {paid.map(d => <DiffRow key={d.slug} diff={d} />)}
                {paid.length === 0 && (
                  <p className="text-xs text-gray-600 italic px-2">
                    Geen betaalde plugins uit het manifest geïnstalleerd op deze omgeving.
                  </p>
                )}
              </div>
            </section>

            {vulnerableOther.length > 0 && (
              <section>
                <h3 className="text-xs font-semibold text-red-700 mb-2">
                  Kwetsbaar (overig) <span className="font-normal">{vulnerableOther.length}</span>
                </h3>
                <div className="space-y-px">
                  {vulnerableOther.map(d => <DiffRow key={d.slug} diff={d} />)}
                </div>
              </section>
            )}
          </div>
        ) : null}
      </div>
    </div>
  )
}

function DiffRow({ diff }: { diff: PluginDiff }) {
  return (
    <div className="flex items-center gap-2 py-1 px-2 rounded hover:bg-black/[0.03]">
      <StatusBadge status={diff.status} />
      <span className="text-[11px] text-gray-700 flex-1 truncate font-mono">{diff.slug}</span>
      <span className="text-[10px] font-mono text-gray-600 shrink-0">{diff.installedVersion || '—'}</span>
      {diff.status === DiffStatus.DiffUpdate && diff.availableVersion && (
        <span className="text-[10px] text-amber-500 font-mono shrink-0">→ {diff.availableVersion}</span>
      )}
    </div>
  )
}

function StatusBadge({ status }: { status: DiffStatus }) {
  const map: Record<DiffStatus, { label: string; cls: string }> = {
    [DiffStatus.$zero]: { label: '—', cls: 'bg-gray-500/20 text-gray-600' },
    [DiffStatus.DiffVulnerable]: { label: '⚠ kwetsbaar', cls: 'bg-red-500/20 text-red-700' },
    [DiffStatus.DiffUpdate]: { label: '↑ update', cls: 'bg-amber-500/20 text-amber-700' },
    [DiffStatus.DiffUpToDate]: { label: 'actueel', cls: 'bg-emerald-500/20 text-emerald-700' },
    [DiffStatus.DiffNotFound]: { label: 'wp.org', cls: 'bg-gray-500/20 text-gray-600' },
  }
  const { label, cls } = map[status] ?? map[DiffStatus.$zero]
  return (
    <span className={`text-[10px] px-1.5 py-px rounded-full shrink-0 whitespace-nowrap ${cls}`}>
      {label}
    </span>
  )
}

function Empty({ title, hint }: { title: string; hint: string }) {
  return (
    <div className="flex-1 flex flex-col items-center justify-center gap-2 p-8 text-center">
      <p className="text-gray-600 text-sm font-medium">{title}</p>
      <p className="text-gray-600 text-xs">{hint}</p>
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
