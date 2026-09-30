import { useState, useEffect, useCallback } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type {
  Project,
  GitStatus,
} from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import InfoTab from './InfoTab'
import HistoryTab from './HistoryTab'
import ChangesTab from './ChangesTab'
import BranchesTab from './BranchesTab'
import StashTagsTab from './StashTagsTab'
import BlameTab from './BlameTab'
import FileHistoryTab from './FileHistoryTab'
import PluginsTab from './PluginsTab'
import SshTerminalTab from './SshTerminalTab'
import UpdatesTab from './UpdatesTab'
import SecurityTab from './SecurityTab'
import TestsTab from './TestsTab'
import MediaTab from './MediaTab'
import LogsTab from './LogsTab'
import DBEditorTab from './DBEditorTab'
import DatabaseTab from './DatabaseTab'
import MigrationSettingsTab from './MigrationSettingsTab'
import MigrationMediaTab from './MigrationMediaTab'
import ProjectSettingsTab from './ProjectSettingsTab'
import ReportTab from './ReportTab'
import Tooltip from './Tooltip'
import { ChevronIcon } from './icons'

export interface ProjectDetailProps {
  project: Project
  onRefresh: () => void
  onRegisterChanged: (siteId?: string) => void
}

type TabId = 'info' | 'history' | 'changes' | 'branches' | 'stash' | 'blame' | 'filehistory' | 'plugins' | 'media' | 'logs' | 'database' | 'dbeditor' | 'migrationsettings' | 'migrationmedia' | 'terminal' | 'updates' | 'security' | 'tests' | 'report' | 'projectsettings'

// Elke menugroep is inklapbaar; de vouwstand staat per groepstitel in
// localStorage, zodat het menu er na een herstart uitziet zoals je het liet.
const COLLAPSE_KEY = 'rdm.nav.collapsed'

interface NavItem {
  id: TabId
  label: string
  badge?: number
  badgeClass?: string
  locked?: string // tooltip-tekst als een vereiste ontbreekt
}

interface NavGroup {
  title: string
  items: NavItem[]
}

const deployTypeLabel: Record<string, string> = {
  wordpress_kinsta:  'WordPress / Kinsta',
  wordpress_transip: 'WordPress / TransIP',
  wordpress_5_2:     'WordPress 5.2',
}

function HeaderIconBtn({ onClick, title, disabled, children }: {
  onClick: () => void; title: string; disabled?: boolean; children: React.ReactNode
}) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      title={title}
      className="w-7 h-7 rounded-[7px] border border-border flex items-center justify-center
                 text-fg-muted hover:text-fg hover:bg-hover transition-colors text-[13px] font-mono"
    >
      {children}
    </button>
  )
}

export default function ProjectDetail({ project, onRefresh, onRegisterChanged }: ProjectDetailProps) {
  const [activeTab, setActiveTab] = useState<TabId>('info')
  const [status, setStatus] = useState<GitStatus | null>(project.git ?? null)
  const [loadingOp, setLoadingOp] = useState<string | null>(null)
  const [opError, setOpError] = useState<string | null>(null)
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>(() => {
    try {
      const raw = localStorage.getItem(COLLAPSE_KEY)
      return raw ? JSON.parse(raw) : {}
    } catch {
      return {}
    }
  })

  const heeftMap = project.local && !project.missing
  const heeftKinsta = !!project.config?.kinsta?.siteId
  const [bridgeSite, setBridgeSite] = useState<boolean>(false)
  const isWordPress = !!project.siteId || !!project.deploy?.type?.startsWith('wordpress')
  // Opnieuw ophalen bij tabwissel en bij een nieuw project-object: de koppeling
  // kan in Projectinstellingen zijn aangepast.
  useEffect(() => {
    let actueel = true
    if (!isWordPress) {
      setBridgeSite(false)
      return
    }
    Services.WPSiteService.ForProject(project.id)
      .then(v => { if (actueel) setBridgeSite(!!v?.hasPassword && !!v?.site.bridge) })
      .catch(() => { if (actueel) setBridgeSite(false) })
    return () => { actueel = false }
  }, [project, activeTab, isWordPress])

  // De DB editor verschijnt alleen als er echt een lokale database is; dat weet
  // alleen de backend (uit .env), dus dat wordt opgevraagd.
  const [dbEditorBeschikbaar, setDbEditorBeschikbaar] = useState(false)
  useEffect(() => {
    setDbEditorBeschikbaar(false)
    if (!heeftMap) return
    Services.DBEditorService.Info(project.id)
      .then(i => setDbEditorBeschikbaar(i.beschikbaar))
      .catch(() => setDbEditorBeschikbaar(false))
  }, [project.id, heeftMap])

  const refreshStatus = useCallback(async () => {
    if (!heeftMap) return
    try {
      const s = await Services.GitService.GetStatus(project.id)
      setStatus(s)
    } catch {
      // non-critical
    }
  }, [project.id, heeftMap])

  useEffect(() => {
    setActiveTab('info')
    setStatus(project.git ?? null)
    setOpError(null)
    refreshStatus()
  }, [project.id, project.git, refreshStatus])

  const withOp = useCallback(async (key: string, fn: () => Promise<void>) => {
    setOpError(null)
    setLoadingOp(key)
    try {
      await fn()
      await refreshStatus()
      onRefresh()
    } catch (err: unknown) {
      setOpError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoadingOp(null)
    }
  }, [refreshStatus, onRefresh])

  const doFetch = useCallback(() =>
    withOp('fetch', () => Services.GitService.Fetch(project.id)),
  [project.id, withOp])

  const doPull = useCallback(() =>
    withOp('pull', () => Services.GitService.Pull(project.id)),
  [project.id, withOp])

  const doPush = useCallback(() =>
    withOp('push', () => Services.GitService.Push(project.id, false)),
  [project.id, withOp])

  const dirtyCount = status
    ? (status.staged?.length ?? 0) + (status.unstaged?.length ?? 0) + (status.untracked?.length ?? 0)
    : 0

  const isLoading = (key: string) => loadingOp === key

  const geenMap = heeftMap ? undefined : 'vereist een lokale map'
  const geenKinsta = heeftKinsta ? undefined : 'vereist een Kinsta-koppeling'
  const geenMapOfKinsta = geenMap ?? geenKinsta

  const navGroups: NavGroup[] = [
    { title: 'OVERVIEW', items: [{ id: 'info' as TabId, label: 'Info' }] },
    {
      title: 'SOURCE CONTROL',
      items: [
        { id: 'history' as TabId, label: 'History', locked: geenMap },
        { id: 'changes' as TabId, label: 'Changes', locked: geenMap, badge: dirtyCount > 0 ? dirtyCount : undefined, badgeClass: 'bg-amber-soft text-amber' },
        { id: 'branches' as TabId, label: 'Branches', locked: geenMap },
        { id: 'stash' as TabId, label: 'Stash & Tags', locked: geenMap },
        { id: 'blame' as TabId, label: 'Blame', locked: geenMap },
        { id: 'filehistory' as TabId, label: 'File History', locked: geenMap },
      ],
    },
    ...(isWordPress ? [{
      title: 'WORDPRESS',
      items: [
        { id: 'updates' as TabId, label: 'Updates', locked: !bridgeSite && !heeftMap ? 'vereist een bridge-koppeling' : undefined },
        { id: 'plugins' as TabId, label: 'Plugins', locked: geenMap },
        { id: 'security' as TabId, label: 'Security', locked: geenMap },
        { id: 'tests' as TabId, label: 'Tests', locked: geenMap },
        { id: 'logs' as TabId, label: 'Logs', locked: geenKinsta },
        { id: 'media' as TabId, label: 'Media-analyse', locked: geenKinsta ?? geenMap },
      ],
    }, {
      title: 'MIGRATIE',
      items: [
        { id: 'database' as TabId, label: 'Database', locked: geenMapOfKinsta },
        ...(dbEditorBeschikbaar ? [{ id: 'dbeditor' as TabId, label: 'DB editor', locked: geenMapOfKinsta }] : []),
        { id: 'migrationmedia' as TabId, label: 'Media', locked: geenMapOfKinsta },
        { id: 'migrationsettings' as TabId, label: 'Instellingen', locked: geenMapOfKinsta },
      ],
    }] : []),
    ...(heeftKinsta || project.siteId ? [{
      title: 'TOOLS',
      items: [{ id: 'terminal' as TabId, label: 'Terminal', locked: geenKinsta }],
    }] : []),
    {
      title: 'KLANT',
      items: [{ id: 'report' as TabId, label: 'Rapportage', locked: geenMap }],
    },
    {
      title: 'INSTELLINGEN',
      items: [{ id: 'projectsettings' as TabId, label: 'Projectinstellingen' }],
    },
  ].filter(g => g.items.length > 0)

  useEffect(() => {
    try {
      localStorage.setItem(COLLAPSE_KEY, JSON.stringify(collapsed))
    } catch {
      // localStorage niet beschikbaar: het onthouden van de vouwstand is dan niet erg
    }
  }, [collapsed])

  // Navigeren mag nooit op een verborgen tab eindigen: klap de groep van de
  // actieve tab automatisch open als die was ingeklapt.
  useEffect(() => {
    const groep = navGroups.find(g => g.items.some(it => it.id === activeTab))
    if (groep && collapsed[groep.title]) {
      setCollapsed(huidig => ({ ...huidig, [groep.title]: false }))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeTab])

  const toggleGroep = (titel: string) => {
    setCollapsed(huidig => ({ ...huidig, [titel]: !huidig[titel] }))
  }

  const typeLabel = project.deploy?.type ? (deployTypeLabel[project.deploy.type] ?? project.deploy.type) : null

  return (
    <div className="flex-1 flex flex-col min-h-0 overflow-hidden bg-bg">
      {/* Header */}
      <header className="h-16 shrink-0 flex items-center gap-3 px-[22px] border-b border-border bg-panel">
        <h2 className="font-display text-[20px] font-extrabold tracking-[-.02em] text-fg truncate">
          {project.displayName}
        </h2>
        {heeftMap && status?.isRepo && status.branch && (
          <span className="inline-flex items-center gap-1.5 text-[11px] font-medium font-mono text-fg-muted
                           bg-panel-2 border border-border px-2 py-[3px] rounded-md max-w-[260px]">
            <span className="truncate">⑂ {status.branch}</span>
          </span>
        )}
        {typeLabel && (
          <span className="inline-flex items-center text-[10.5px] font-semibold tracking-[.02em]
                           text-purple bg-accent-soft px-2 py-[3px] rounded-md whitespace-nowrap">
            {typeLabel}
          </span>
        )}

        <div className="ml-auto flex items-center gap-1.5 shrink-0">
          {heeftMap && (
            <Tooltip label="Open in editor">
              <HeaderIconBtn
                onClick={() => Services.EditorService.OpenInEditor(project.id, project.path)}
                title="Open in editor"
              >
                ✎
              </HeaderIconBtn>
            </Tooltip>
          )}
          <Tooltip label="Projectinstellingen">
            <HeaderIconBtn onClick={() => setActiveTab('projectsettings')} title="Projectinstellingen">
              ⚙
            </HeaderIconBtn>
          </Tooltip>
          {heeftMap && status?.isRepo && (
            <>
              <Tooltip label="Fetch">
                <HeaderIconBtn onClick={doFetch} disabled={loadingOp !== null} title="Fetch">
                  {isLoading('fetch') ? <span className="animate-spin inline-block text-xs">↻</span> : '↺'}
                </HeaderIconBtn>
              </Tooltip>
              <Tooltip label="Pull">
                <HeaderIconBtn onClick={doPull} disabled={loadingOp !== null} title="Pull">
                  {isLoading('pull') ? <span className="animate-spin inline-block text-xs">↻</span> : '↓'}
                </HeaderIconBtn>
              </Tooltip>
              <Tooltip label="Push">
                <HeaderIconBtn onClick={doPush} disabled={loadingOp !== null} title="Push">
                  {isLoading('push') ? <span className="animate-spin inline-block text-xs">↻</span> : '↑'}
                </HeaderIconBtn>
              </Tooltip>
            </>
          )}
        </div>
      </header>

      {opError && (
        <div className="shrink-0 mx-[22px] mt-3 bg-red-soft text-red px-3 py-2 rounded-lg text-xs">
          {opError}
        </div>
      )}

      {/* Nav rail + content */}
      <div className="flex-1 min-h-0 flex">
        <nav className="w-[206px] shrink-0 bg-panel border-r border-border py-4 px-1.5 overflow-y-auto">
          {navGroups.map(grp => {
            const dicht = !!collapsed[grp.title]
            return (
              <div key={grp.title} className="mb-[15px]">
                <button
                  onClick={() => toggleGroep(grp.title)}
                  className="w-full flex items-center justify-between px-3 pb-1.5 text-[10px] font-semibold
                             tracking-[.09em] text-fg-faint hover:text-fg-muted transition-colors"
                >
                  <span>{grp.title}</span>
                  <ChevronIcon open={!dicht} className="w-2.5 h-2.5" />
                </button>
                {!dicht && grp.items.map(it => {
                  const on = activeTab === it.id
                  if (it.locked) {
                    return (
                      <button
                        key={it.id}
                        onClick={() => setActiveTab('projectsettings')}
                        title={it.locked}
                        aria-disabled="true"
                        aria-label={`${it.label} (vergrendeld: ${it.locked})`}
                        className="w-full flex items-center justify-between gap-2 mx-0 my-px px-[11px] py-[7px]
                                   rounded-nav text-[13px] transition-colors text-left opacity-50
                                   font-[450] text-fg-muted hover:bg-hover"
                      >
                        <span>{it.label}</span>
                        <span className="text-[10px]">🔒</span>
                      </button>
                    )
                  }
                  return (
                    <button
                      key={it.id}
                      onClick={() => setActiveTab(it.id)}
                      className={`w-full flex items-center justify-between gap-2 mx-0 my-px px-[11px] py-[7px]
                                  rounded-nav text-[13px] transition-colors text-left
                                  ${on
                                    ? 'font-semibold text-fg bg-sel shadow-[inset_2px_0_0_var(--accent)]'
                                    : 'font-[450] text-fg-muted hover:bg-hover'}`}
                    >
                      <span>{it.label}</span>
                      {it.badge !== undefined && (
                        <span className={`text-[10px] font-semibold font-mono px-1.5 py-px rounded-full ${it.badgeClass ?? 'bg-panel-2 text-fg-muted'}`}>
                          {it.badge}
                        </span>
                      )}
                    </button>
                  )
                })}
              </div>
            )
          })}
        </nav>

        {/* Tab content */}
        <div className="flex-1 min-w-0 min-h-0 flex flex-col overflow-hidden bg-bg">
          {activeTab === 'info' && <InfoTab project={project} />}
          {activeTab === 'history' && <HistoryTab projectId={project.id} />}
          {activeTab === 'changes' && (
            <ChangesTab projectId={project.id} status={status} onRefreshStatus={refreshStatus} />
          )}
          {activeTab === 'branches' && (
            <BranchesTab projectId={project.id} currentBranch={status?.branch ?? ''} onBranchChange={refreshStatus} />
          )}
          {activeTab === 'stash' && <StashTagsTab projectId={project.id} />}
          {activeTab === 'blame' && <BlameTab projectId={project.id} />}
          {activeTab === 'filehistory' && <FileHistoryTab projectId={project.id} />}
          {activeTab === 'plugins' && <PluginsTab projectId={project.id} />}
          {activeTab === 'terminal' && <SshTerminalTab projectId={project.id} />}
          {activeTab === 'updates' && <UpdatesTab projectId={project.id} currentBranch={status?.branch ?? ''} onBranchCheckedOut={refreshStatus} heeftMap={heeftMap} />}
          {activeTab === 'security' && <SecurityTab projectId={project.id} />}
          {activeTab === 'tests' && <TestsTab projectId={project.id} />}
          {activeTab === 'media' && <MediaTab projectId={project.id} />}
          {activeTab === 'logs' && <LogsTab projectId={project.id} />}
          {activeTab === 'dbeditor' && <DBEditorTab projectId={project.id} />}
          {activeTab === 'database' && <DatabaseTab projectId={project.id} />}
          {activeTab === 'migrationmedia' && <MigrationMediaTab projectId={project.id} />}
          {activeTab === 'migrationsettings' && <MigrationSettingsTab projectId={project.id} />}
          {activeTab === 'report' && <ReportTab projectId={project.id} />}
          {activeTab === 'projectsettings' && <ProjectSettingsTab project={project} onLocalUrlSaved={onRefresh} onRegisterChanged={onRegisterChanged} />}
        </div>
      </div>
    </div>
  )
}