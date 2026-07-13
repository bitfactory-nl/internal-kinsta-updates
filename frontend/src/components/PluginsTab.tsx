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
      <div className="w-[200px] shrink-0 border-r border-border flex flex-col overflow-hidden">
        <div className="px-3 py-2.5 border-b border-border shrink-0 flex items-center gap-2">
          <p className="text-xs font-semibold text-fg truncate flex-1">
            {site.site.display_name || site.site.name}
          </p>
          <button
            onClick={refreshManifest}
            disabled={loading}
            title="Manifest verversen"
            className="text-[11px] text-fg-muted hover:text-fg transition-colors"
          >
            {loading ? <span className="animate-spin inline-block">↻</span> : '⟳'}
          </button>
        </div>
        <div className="flex-1 overflow-y-auto py-1">
          {(site.environments ?? []).map(env => (
            <button
              key={env.id}
              onClick={() => loadDiff(env.id)}
              className={`w-full text-left px-3 py-2 transition-colors
                ${selectedEnvId === env.id ? 'bg-sel' : 'hover:bg-hover'}`}
            >
              <div className="flex items-center gap-1.5">
                <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${env.is_blocked ? 'bg-red' : 'bg-green'}`} />
                <span className={`text-xs truncate ${selectedEnvId === env.id ? 'text-fg font-medium' : 'text-fg-muted'}`}>{env.display_name || env.name}</span>
                {env.name === 'live' && (
                  <span className="ml-auto shrink-0 text-[9px] font-semibold font-mono text-green bg-green-soft px-1.5 py-px rounded-full">live</span>
                )}
              </div>
            </button>
          ))}
        </div>
      </div>

      {/* Diff detail */}
      <div className="flex-1 min-w-0 flex flex-col overflow-hidden">
        {!selectedEnvId ? (
          <div className="flex-1 flex items-center justify-center text-fg-faint text-[13px] italic">
            Selecteer een omgeving
          </div>
        ) : loading ? (
          <Spinner />
        ) : error ? (
          <div className="m-4 bg-red-soft text-red px-3 py-2 rounded-lg text-xs">{error}</div>
        ) : diffs ? (
          <div className="flex-1 overflow-y-auto px-6 py-5 space-y-6">
            <section>
              <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">
                Betaalde plugins <span className="font-mono normal-case tracking-normal text-fg-faint">{paid.length}</span>
              </h3>
              {paid.length > 0 ? (
                <div className="bg-panel border border-border rounded-[11px] overflow-hidden divide-y divide-border">
                  {paid.map(d => <DiffRow key={d.slug} diff={d} />)}
                </div>
              ) : (
                <p className="text-[13px] font-[450] text-fg-faint italic">
                  Geen betaalde plugins uit het manifest geïnstalleerd op deze omgeving.
                </p>
              )}
            </section>

            {vulnerableOther.length > 0 && (
              <section>
                <h3 className="text-[11px] font-semibold tracking-[.08em] text-red uppercase mb-2.5">
                  Kwetsbaar (overig) <span className="font-mono normal-case tracking-normal">{vulnerableOther.length}</span>
                </h3>
                <div className="bg-panel border border-border rounded-[11px] overflow-hidden divide-y divide-border">
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
    <div className="flex items-center gap-2.5 px-4 py-2.5 hover:bg-hover transition-colors">
      <StatusBadge status={diff.status} />
      <span className="text-xs font-medium text-fg flex-1 truncate font-mono">{diff.slug}</span>
      <span className="text-[11px] font-mono text-fg-faint shrink-0">{diff.installedVersion || '—'}</span>
      {diff.status === DiffStatus.DiffUpdate && diff.availableVersion && (
        <span className="text-[11px] text-green font-mono font-semibold shrink-0">→ {diff.availableVersion}</span>
      )}
    </div>
  )
}

function StatusBadge({ status }: { status: DiffStatus }) {
  const map: Record<DiffStatus, { label: string; cls: string }> = {
    [DiffStatus.$zero]: { label: '—', cls: 'text-fg-muted bg-hover' },
    [DiffStatus.DiffVulnerable]: { label: '⚠ kwetsbaar', cls: 'text-red bg-red-soft' },
    [DiffStatus.DiffUpdate]: { label: '↑ update', cls: 'text-amber bg-amber-soft' },
    [DiffStatus.DiffUpToDate]: { label: 'actueel', cls: 'text-green bg-green-soft' },
    [DiffStatus.DiffNotFound]: { label: 'wp.org', cls: 'text-fg-muted bg-hover' },
  }
  const { label, cls } = map[status] ?? map[DiffStatus.$zero]
  return (
    <span className={`text-[10px] font-semibold font-mono px-1.5 py-px rounded-full shrink-0 whitespace-nowrap ${cls}`}>
      {label}
    </span>
  )
}

function Empty({ title, hint }: { title: string; hint: string }) {
  return (
    <div className="flex-1 flex flex-col items-center justify-center gap-3.5 p-10 text-center">
      <div className="w-[52px] h-[52px] rounded-[14px] border-[1.5px] border-dashed border-border-strong flex items-center justify-center font-mono font-medium text-[22px] text-fg-faint">
        {'{ }'}
      </div>
      <p className="text-[15px] font-semibold text-fg">{title}</p>
      <p className="text-[13px] font-[450] text-fg-muted max-w-[380px]">{hint}</p>
    </div>
  )
}

function Spinner() {
  return (
    <div className="flex-1 flex items-center justify-center gap-2 text-fg-faint text-sm">
      <span className="animate-spin inline-block">↻</span>
    </div>
  )
}
