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
import KinstaTab from './KinstaTab'
import UpdatesTab from './UpdatesTab'

export interface ProjectDetailProps {
  project: Project
  onRefresh: () => void
}

type TabId = 'info' | 'history' | 'changes' | 'branches' | 'stash' | 'blame' | 'filehistory' | 'kinsta' | 'updates'

export default function ProjectDetail({ project, onRefresh }: ProjectDetailProps) {
  const [activeTab, setActiveTab] = useState<TabId>('info')
  const [status, setStatus] = useState<GitStatus | null>(project.git ?? null)
  const [loadingOp, setLoadingOp] = useState<string | null>(null)
  const [opError, setOpError] = useState<string | null>(null)

  const isKinsta = project.deploy?.type === 'wordpress_kinsta'

  const refreshStatus = useCallback(async () => {
    try {
      const s = await Services.GitService.GetStatus(project.id)
      setStatus(s)
    } catch {
      // non-critical
    }
  }, [project.id])

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

  const tabs: { id: TabId; label: string; badge?: number }[] = [
    { id: 'info',        label: 'Info' },
    { id: 'history',     label: 'History' },
    { id: 'changes',     label: 'Changes', badge: dirtyCount > 0 ? dirtyCount : undefined },
    { id: 'branches',    label: 'Branches' },
    { id: 'stash',       label: 'Stash & Tags' },
    { id: 'blame',       label: 'Blame' },
    { id: 'filehistory', label: 'File History' },
    ...(isKinsta ? [{ id: 'kinsta' as TabId, label: 'Kinsta' }] : []),
    ...(status?.isRepo ? [{ id: 'updates' as TabId, label: 'Updates' }] : []),
  ]

  return (
    <div className="flex-1 flex flex-col min-h-0 overflow-hidden">
      {/* Header */}
      <div className="px-4 py-3 border-b border-black/[0.08] shrink-0">
        <div className="flex items-center gap-2 mb-2">
          <h2 className="text-base font-semibold text-gray-900 truncate flex-1">
            {project.displayName}
          </h2>

          {/* Action buttons */}
          <div className="flex items-center gap-1 shrink-0">
            <button
              onClick={() => Services.EditorService.OpenInEditor(project.id, project.path)}
              title="Open in editor"
              className="w-7 h-7 flex items-center justify-center rounded-md text-gray-600
                         hover:text-gray-900 hover:bg-black/[0.08] transition-colors text-sm"
            >
              ✎
            </button>

            {status?.isRepo && (
              <>
                <button onClick={doFetch} disabled={loadingOp !== null} title="Fetch"
                  className="w-7 h-7 flex items-center justify-center rounded-md text-gray-600
                             hover:text-gray-900 hover:bg-black/[0.08] transition-colors text-sm">
                  {isLoading('fetch') ? <span className="animate-spin inline-block text-xs">↻</span> : '⟳'}
                </button>
                <button onClick={doPull} disabled={loadingOp !== null} title="Pull"
                  className="w-7 h-7 flex items-center justify-center rounded-md text-gray-600
                             hover:text-gray-900 hover:bg-black/[0.08] transition-colors text-sm">
                  {isLoading('pull') ? <span className="animate-spin inline-block text-xs">↻</span> : '↓'}
                </button>
                <button onClick={doPush} disabled={loadingOp !== null} title="Push"
                  className="w-7 h-7 flex items-center justify-center rounded-md text-gray-600
                             hover:text-gray-900 hover:bg-black/[0.08] transition-colors text-sm">
                  {isLoading('push') ? <span className="animate-spin inline-block text-xs">↻</span> : '↑'}
                </button>
              </>
            )}
          </div>
        </div>

        {opError && (
          <div className="mb-2 bg-red-100 text-red-600 px-3 py-2 rounded text-xs">{opError}</div>
        )}

        {/* Tabs — scrollable for small windows */}
        <div className="flex items-center gap-0.5 overflow-x-auto scrollbar-none">
          {tabs.map(tab => (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id)}
              className={`flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-colors whitespace-nowrap
                ${activeTab === tab.id
                  ? 'bg-black/[0.08] text-gray-900'
                  : 'text-gray-600 hover:text-gray-800 hover:bg-black/[0.04]'}`}
            >
              {tab.label}
              {tab.badge !== undefined && (
                <span className="bg-amber-500/30 text-amber-700 text-[10px] px-1.5 py-px rounded-full font-mono">
                  {tab.badge}
                </span>
              )}
            </button>
          ))}
        </div>
      </div>

      {/* Tab content */}
      <div className="flex-1 min-h-0 flex flex-col overflow-hidden">
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
        {activeTab === 'kinsta' && <KinstaTab projectId={project.id} />}
        {activeTab === 'updates' && <UpdatesTab projectId={project.id} currentBranch={status?.branch ?? ''} onBranchCheckedOut={refreshStatus} />}
      </div>
    </div>
  )
}
