import { useState, useCallback } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type {
  GitStatus,
  FileDiff,
} from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import DiffViewer from './DiffViewer'

export interface ChangesTabProps {
  projectId: string
  status: GitStatus | null
  onRefreshStatus: () => void
}

type SelectedEntry = {
  path: string
  staged: boolean
}

const kindLabel: Record<string, { label: string; cls: string }> = {
  added:    { label: 'A', cls: 'text-emerald-600' },
  modified: { label: 'M', cls: 'text-amber-500' },
  deleted:  { label: 'D', cls: 'text-red-600' },
  renamed:  { label: 'R', cls: 'text-sky-600' },
}

export default function ChangesTab({ projectId, status, onRefreshStatus }: ChangesTabProps) {
  const [selectedEntry, setSelectedEntry] = useState<SelectedEntry | null>(null)
  const [diff, setDiff] = useState<FileDiff[] | null>(null)
  const [loadingDiff, setLoadingDiff] = useState(false)
  const [diffError, setDiffError] = useState<string | null>(null)
  const [commitMessage, setCommitMessage] = useState('')
  const [actionError, setActionError] = useState<string | null>(null)
  const [loadingAction, setLoadingAction] = useState<string | null>(null)

  const loadDiff = useCallback(async (path: string, staged: boolean) => {
    setSelectedEntry({ path, staged })
    setLoadingDiff(true)
    setDiffError(null)
    try {
      const diffs = await Services.GitService.GetWorkingDiff(projectId, staged)
      const filtered = (diffs ?? []).filter(d => d.path === path)
      setDiff(filtered)
    } catch (err: unknown) {
      setDiffError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoadingDiff(false)
    }
  }, [projectId])

  const withAction = useCallback(async (key: string, fn: () => Promise<void>) => {
    setActionError(null)
    setLoadingAction(key)
    try {
      await fn()
      onRefreshStatus()
    } catch (err: unknown) {
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoadingAction(null)
    }
  }, [onRefreshStatus])

  const stageFile = useCallback((path: string) =>
    withAction(`stage-${path}`, () => Services.GitService.StageFiles(projectId, [path])),
  [projectId, withAction])

  const unstageFile = useCallback((path: string) =>
    withAction(`unstage-${path}`, () => Services.GitService.UnstageFiles(projectId, [path])),
  [projectId, withAction])

  const stageAll = useCallback(() =>
    withAction('stage-all', () => Services.GitService.StageAll(projectId)),
  [projectId, withAction])

  const discardFile = useCallback((path: string) =>
    withAction(`discard-${path}`, () => Services.GitService.DiscardFile(projectId, path)),
  [projectId, withAction])

  const doCommit = useCallback((amend: boolean) =>
    withAction('commit', async () => {
      await Services.GitService.Commit(projectId, commitMessage, amend)
      setCommitMessage('')
    }),
  [projectId, commitMessage, withAction])

  const doFetch = useCallback(() =>
    withAction('fetch', () => Services.GitService.Fetch(projectId)),
  [projectId, withAction])

  const doPull = useCallback(() =>
    withAction('pull', () => Services.GitService.Pull(projectId)),
  [projectId, withAction])

  const doPush = useCallback((force = false) =>
    withAction('push', () => Services.GitService.Push(projectId, force)),
  [projectId, withAction])

  const loading = (key: string) => loadingAction === key

  if (!status) {
    return (
      <div className="flex items-center justify-center py-8 text-gray-600 text-sm italic">
        Loading status…
      </div>
    )
  }

  const staged = status.staged ?? []
  const unstaged = status.unstaged ?? []
  const untracked = status.untracked ?? []
  const totalChanges = staged.length + unstaged.length + untracked.length

  return (
    <div className="flex flex-1 min-h-0 overflow-hidden">
      {/* Left panel */}
      <div className="w-[40%] shrink-0 flex flex-col border-r border-black/[0.08] overflow-hidden">
        <div className="flex-1 overflow-y-auto">
          {actionError && (
            <div className="m-2 bg-red-100 text-red-600 px-3 py-2 rounded text-xs">
              {actionError}
            </div>
          )}

          {totalChanges === 0 && (
            <div className="px-4 py-3 text-sm text-gray-600 italic">
              Working tree clean
            </div>
          )}

          {/* Staged files */}
          <div className="px-3 pt-3">
            <div className="flex items-center justify-between mb-1.5">
              <span className="text-[11px] font-semibold text-gray-600 uppercase tracking-wider">
                Staged ({staged.length})
              </span>
              {staged.length > 0 && (
                <button
                  onClick={() => withAction('unstage-all', () => Services.GitService.UnstageFiles(projectId, staged.map(f => f.path)))}
                  className="text-[11px] text-gray-600 hover:text-gray-800 transition-colors"
                >
                  Unstage all
                </button>
              )}
            </div>
            {staged.map(f => {
              const k = kindLabel[f.kind] ?? { label: '?', cls: 'text-gray-600' }
              const isSelected = selectedEntry?.path === f.path && selectedEntry?.staged
              return (
                <div
                  key={f.path}
                  onClick={() => loadDiff(f.path, true)}
                  className={`flex items-center gap-2 px-2 py-1 rounded cursor-pointer transition-colors
                    ${isSelected ? 'bg-black/[0.08]' : 'hover:bg-black/[0.04]'}`}
                >
                  <span className={`text-[11px] font-mono font-bold w-3 shrink-0 ${k.cls}`}>{k.label}</span>
                  <span className="text-xs text-gray-700 flex-1 truncate font-mono" title={f.path}>
                    {f.path.split('/').pop()}
                  </span>
                  <button
                    onClick={e => { e.stopPropagation(); unstageFile(f.path) }}
                    disabled={loadingAction !== null}
                    title="Unstage"
                    className="text-[11px] text-gray-600 hover:text-amber-500 transition-colors shrink-0 px-1"
                  >
                    {loading(`unstage-${f.path}`) ? '↻' : '−'}
                  </button>
                </div>
              )
            })}
          </div>

          {/* Unstaged files */}
          <div className="px-3 pt-3">
            <div className="flex items-center justify-between mb-1.5">
              <span className="text-[11px] font-semibold text-gray-600 uppercase tracking-wider">
                Unstaged ({unstaged.length})
              </span>
              {(unstaged.length > 0 || untracked.length > 0) && (
                <button
                  onClick={stageAll}
                  disabled={loadingAction !== null}
                  className="text-[11px] text-gray-600 hover:text-gray-800 transition-colors"
                >
                  {loading('stage-all') ? <span className="animate-spin inline-block">↻</span> : 'Stage all'}
                </button>
              )}
            </div>
            {unstaged.map(f => {
              const k = kindLabel[f.kind] ?? { label: '?', cls: 'text-gray-600' }
              const isSelected = selectedEntry?.path === f.path && !selectedEntry?.staged
              return (
                <div
                  key={f.path}
                  onClick={() => loadDiff(f.path, false)}
                  className={`flex items-center gap-2 px-2 py-1 rounded cursor-pointer transition-colors
                    ${isSelected ? 'bg-black/[0.08]' : 'hover:bg-black/[0.04]'}`}
                >
                  <span className={`text-[11px] font-mono font-bold w-3 shrink-0 ${k.cls}`}>{k.label}</span>
                  <span className="text-xs text-gray-700 flex-1 truncate font-mono" title={f.path}>
                    {f.path.split('/').pop()}
                  </span>
                  <button
                    onClick={e => { e.stopPropagation(); stageFile(f.path) }}
                    disabled={loadingAction !== null}
                    title="Stage"
                    className="text-[11px] text-gray-600 hover:text-emerald-600 transition-colors shrink-0 px-1"
                  >
                    {loading(`stage-${f.path}`) ? <span className="animate-spin inline-block text-[11px]">↻</span> : '+'}
                  </button>
                </div>
              )
            })}
          </div>

          {/* Untracked files */}
          {untracked.length > 0 && (
            <div className="px-3 pt-3">
              <div className="flex items-center justify-between mb-1.5">
                <span className="text-[11px] font-semibold text-gray-600 uppercase tracking-wider">
                  Untracked ({untracked.length})
                </span>
              </div>
              {untracked.map(path => {
                const isSelected = selectedEntry?.path === path && !selectedEntry?.staged
                return (
                  <div
                    key={path}
                    onClick={() => loadDiff(path, false)}
                    className={`flex items-center gap-2 px-2 py-1 rounded cursor-pointer transition-colors
                      ${isSelected ? 'bg-black/[0.08]' : 'hover:bg-black/[0.04]'}`}
                  >
                    <span className="text-[11px] font-mono font-bold w-3 shrink-0 text-gray-600">?</span>
                    <span className="text-xs text-gray-600 flex-1 truncate font-mono" title={path}>
                      {path.split('/').pop()}
                    </span>
                    <div className="flex items-center gap-0.5 shrink-0">
                      <button
                        onClick={e => { e.stopPropagation(); stageFile(path) }}
                        disabled={loadingAction !== null}
                        title="Stage"
                        className="text-[11px] text-gray-600 hover:text-emerald-600 transition-colors px-1"
                      >
                        {loading(`stage-${path}`) ? <span className="animate-spin inline-block text-[11px]">↻</span> : '+'}
                      </button>
                      <button
                        onClick={e => { e.stopPropagation(); discardFile(path) }}
                        disabled={loadingAction !== null}
                        title="Discard"
                        className="text-[11px] text-gray-600 hover:text-red-600 transition-colors px-1"
                      >
                        ×
                      </button>
                    </div>
                  </div>
                )
              })}
            </div>
          )}

          {/* Conflicted */}
          {(status.conflicted ?? []).length > 0 && (
            <div className="px-3 pt-3">
              <div className="text-[11px] font-semibold text-red-500 uppercase tracking-wider mb-1.5">
                Conflicts ({status.conflicted.length})
              </div>
              {status.conflicted.map(path => (
                <div key={path} className="flex items-center gap-2 px-2 py-1">
                  <span className="text-[11px] font-mono font-bold text-red-500 w-3">!</span>
                  <span className="text-xs text-red-600 font-mono truncate">{path.split('/').pop()}</span>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Commit form */}
        <div className="border-t border-black/[0.08] p-3 shrink-0">
          <textarea
            value={commitMessage}
            onChange={e => setCommitMessage(e.target.value)}
            placeholder="Commit message…"
            rows={3}
            className="w-full bg-black/[0.05] text-sm text-gray-800 placeholder-gray-400
                       rounded-lg px-3 py-2 outline-none focus:ring-1 focus:ring-indigo-500/40
                       border border-transparent focus:border-indigo-400 resize-none font-mono text-xs mb-2"
          />
          <div className="flex items-center gap-2">
            <button
              onClick={() => doCommit(false)}
              disabled={!commitMessage.trim() || staged.length === 0 || loadingAction !== null}
              className="flex-1 py-1.5 bg-indigo-600 hover:bg-indigo-500 disabled:opacity-40
                         disabled:cursor-not-allowed text-gray-900 text-xs font-medium rounded-lg
                         transition-colors"
            >
              {loading('commit') ? <span className="animate-spin inline-block">↻</span> : 'Commit'}
            </button>
            <button
              onClick={() => doCommit(true)}
              disabled={!commitMessage.trim() || loadingAction !== null}
              className="px-3 py-1.5 bg-black/[0.08] hover:bg-black/[0.10] disabled:opacity-40
                         disabled:cursor-not-allowed text-gray-700 text-xs rounded-lg
                         transition-colors"
            >
              Amend
            </button>
          </div>

          {/* Sync strip */}
          <div className="flex items-center gap-1.5 mt-2 pt-2 border-t border-black/[0.08]">
            <div className="flex items-center gap-1 flex-1">
              {status.ahead > 0 && (
                <span className="text-[11px] text-emerald-600 font-mono">↑{status.ahead}</span>
              )}
              {status.behind > 0 && (
                <span className="text-[11px] text-red-600 font-mono">↓{status.behind}</span>
              )}
              {status.upstream && (
                <span className="text-[10px] text-gray-600 font-mono truncate">{status.upstream}</span>
              )}
            </div>
            <button
              onClick={doFetch}
              disabled={loadingAction !== null}
              title="Fetch"
              className="text-xs text-gray-600 hover:text-gray-800 hover:bg-black/[0.08]
                         px-2 py-0.5 rounded transition-colors"
            >
              {loading('fetch') ? <span className="animate-spin inline-block">↻</span> : '⟳'}
            </button>
            <button
              onClick={doPull}
              disabled={loadingAction !== null}
              title="Pull"
              className="text-xs text-gray-600 hover:text-gray-800 hover:bg-black/[0.08]
                         px-2 py-0.5 rounded transition-colors"
            >
              {loading('pull') ? <span className="animate-spin inline-block">↻</span> : '↓ Pull'}
            </button>
            <button
              onClick={() => doPush(false)}
              disabled={loadingAction !== null}
              title="Push"
              className="text-xs text-gray-600 hover:text-gray-800 hover:bg-black/[0.08]
                         px-2 py-0.5 rounded transition-colors"
            >
              {loading('push') ? <span className="animate-spin inline-block">↻</span> : '↑ Push'}
            </button>
          </div>
        </div>
      </div>

      {/* Right panel: diff */}
      <div className="flex-1 min-w-0 flex flex-col overflow-hidden">
        {!selectedEntry ? (
          <div className="flex-1 flex items-center justify-center text-gray-600 text-sm italic">
            Selecteer een bestand
          </div>
        ) : (
          <div className="flex-1 overflow-y-auto p-3">
            {diffError && (
              <div className="mb-2 bg-red-100 text-red-600 px-3 py-2 rounded text-xs">
                {diffError}
              </div>
            )}
            <DiffViewer diffs={diff} loading={loadingDiff} />
          </div>
        )}
      </div>
    </div>
  )
}
