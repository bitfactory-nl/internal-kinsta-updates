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
  added:    { label: 'A', cls: 'text-green' },
  modified: { label: 'M', cls: 'text-amber' },
  deleted:  { label: 'D', cls: 'text-red' },
  renamed:  { label: 'R', cls: 'text-accent-2' },
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
      <div className="flex items-center justify-center py-8 text-fg-faint text-[13px] italic">
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
      <div className="w-[400px] shrink-0 flex flex-col border-r border-border overflow-hidden">
        <div className="flex-1 overflow-y-auto py-1.5">
          {actionError && (
            <div className="m-2 bg-red-soft text-red px-3 py-2 rounded-[7px] text-xs">
              {actionError}
            </div>
          )}

          {totalChanges === 0 && (
            <div className="px-[18px] py-3 text-[13px] text-fg-faint italic">
              Working tree clean
            </div>
          )}

          {/* Staged files */}
          <div>
            <div className="flex items-center justify-between px-[18px] pt-[11px] pb-[7px]">
              <span className="text-[10.5px] font-semibold tracking-[.07em] text-fg-faint uppercase">
                Staged · {staged.length}
              </span>
              {staged.length > 0 && (
                <button
                  onClick={() => withAction('unstage-all', () => Services.GitService.UnstageFiles(projectId, staged.map(f => f.path)))}
                  className="text-[11.5px] font-medium text-accent hover:text-accent-2 transition-colors"
                >
                  Unstage all
                </button>
              )}
            </div>
            {staged.map(f => {
              const k = kindLabel[f.kind] ?? { label: '?', cls: 'text-fg-faint' }
              const isSelected = selectedEntry?.path === f.path && selectedEntry?.staged
              return (
                <div
                  key={f.path}
                  onClick={() => loadDiff(f.path, true)}
                  className={`flex items-center gap-2.5 px-[18px] py-2 cursor-pointer transition-colors
                    ${isSelected ? 'bg-sel' : 'hover:bg-hover'}`}
                >
                  <span className={`w-4 text-center text-[12px] font-mono font-semibold shrink-0 ${k.cls}`}>{k.label}</span>
                  <span className="text-[13px] font-[450] text-fg flex-1 truncate font-mono" title={f.path}>
                    {f.path.split('/').pop()}
                  </span>
                  <button
                    onClick={e => { e.stopPropagation(); unstageFile(f.path) }}
                    disabled={loadingAction !== null}
                    title="Unstage"
                    className="text-[15px] font-semibold font-mono text-fg-faint hover:text-fg transition-colors shrink-0 px-1"
                  >
                    {loading(`unstage-${f.path}`) ? '↻' : '−'}
                  </button>
                </div>
              )
            })}
          </div>

          {/* Unstaged files */}
          <div>
            <div className="flex items-center justify-between px-[18px] pt-[15px] pb-[7px]">
              <span className="text-[10.5px] font-semibold tracking-[.07em] text-fg-faint uppercase">
                Unstaged · {unstaged.length}
              </span>
              {(unstaged.length > 0 || untracked.length > 0) && (
                <button
                  onClick={stageAll}
                  disabled={loadingAction !== null}
                  className="text-[11.5px] font-medium text-accent hover:text-accent-2 transition-colors"
                >
                  {loading('stage-all') ? <span className="animate-spin inline-block">↻</span> : 'Stage all'}
                </button>
              )}
            </div>
            {unstaged.map(f => {
              const k = kindLabel[f.kind] ?? { label: '?', cls: 'text-fg-faint' }
              const isSelected = selectedEntry?.path === f.path && !selectedEntry?.staged
              return (
                <div
                  key={f.path}
                  onClick={() => loadDiff(f.path, false)}
                  className={`flex items-center gap-2.5 px-[18px] py-2 cursor-pointer transition-colors
                    ${isSelected ? 'bg-sel' : 'hover:bg-hover'}`}
                >
                  <span className={`w-4 text-center text-[12px] font-mono font-semibold shrink-0 ${k.cls}`}>{k.label}</span>
                  <span className="text-[13px] font-[450] text-fg flex-1 truncate font-mono" title={f.path}>
                    {f.path.split('/').pop()}
                  </span>
                  <button
                    onClick={e => { e.stopPropagation(); stageFile(f.path) }}
                    disabled={loadingAction !== null}
                    title="Stage"
                    className="text-[15px] font-semibold font-mono text-fg-faint hover:text-fg transition-colors shrink-0 px-1"
                  >
                    {loading(`stage-${f.path}`) ? <span className="animate-spin inline-block text-[11px]">↻</span> : '+'}
                  </button>
                </div>
              )
            })}
          </div>

          {/* Untracked files */}
          {untracked.length > 0 && (
            <div>
              <div className="flex items-center justify-between px-[18px] pt-[15px] pb-[7px]">
                <span className="text-[10.5px] font-semibold tracking-[.07em] text-fg-faint uppercase">
                  Untracked · {untracked.length}
                </span>
              </div>
              {untracked.map(path => {
                const isSelected = selectedEntry?.path === path && !selectedEntry?.staged
                return (
                  <div
                    key={path}
                    onClick={() => loadDiff(path, false)}
                    className={`flex items-center gap-2.5 px-[18px] py-2 cursor-pointer transition-colors
                      ${isSelected ? 'bg-sel' : 'hover:bg-hover'}`}
                  >
                    <span className="w-4 text-center text-[12px] font-mono font-semibold shrink-0 text-fg-faint">?</span>
                    <span className="text-[13px] font-[450] text-fg-muted flex-1 truncate font-mono" title={path}>
                      {path.split('/').pop()}
                    </span>
                    <div className="flex items-center gap-0.5 shrink-0">
                      <button
                        onClick={e => { e.stopPropagation(); stageFile(path) }}
                        disabled={loadingAction !== null}
                        title="Stage"
                        className="text-[15px] font-semibold font-mono text-fg-faint hover:text-fg transition-colors px-1"
                      >
                        {loading(`stage-${path}`) ? <span className="animate-spin inline-block text-[11px]">↻</span> : '+'}
                      </button>
                      <button
                        onClick={e => { e.stopPropagation(); discardFile(path) }}
                        disabled={loadingAction !== null}
                        title="Discard"
                        className="text-[15px] font-semibold font-mono text-fg-faint hover:text-red transition-colors px-1"
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
            <div>
              <div className="px-[18px] pt-[15px] pb-[7px] text-[10.5px] font-semibold tracking-[.07em] text-red uppercase">
                Conflicts · {status.conflicted.length}
              </div>
              {status.conflicted.map(path => (
                <div key={path} className="flex items-center gap-2.5 px-[18px] py-2">
                  <span className="w-4 text-center text-[12px] font-mono font-semibold text-red">!</span>
                  <span className="text-[13px] font-[450] text-red font-mono truncate">{path.split('/').pop()}</span>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Commit form */}
        <div className="border-t border-border bg-panel px-4 py-3.5 shrink-0">
          <textarea
            value={commitMessage}
            onChange={e => setCommitMessage(e.target.value)}
            placeholder="Commit message…"
            rows={3}
            className="w-full bg-bg border border-border rounded-[9px] px-[13px] py-[11px]
                       text-[13px] font-[450] text-fg placeholder-fg-faint outline-none
                       focus:border-accent focus:ring-1 focus:ring-accent/30 resize-none"
          />
          <div className="flex items-center gap-[9px] mt-[11px]">
            <button
              onClick={() => doCommit(false)}
              disabled={!commitMessage.trim() || staged.length === 0 || loadingAction !== null}
              className="flex-1 py-2.5 bg-accent hover:bg-accent-2 disabled:opacity-40
                         disabled:cursor-not-allowed text-white text-[13px] font-semibold rounded-[9px]
                         transition-colors"
            >
              {loading('commit') ? <span className="animate-spin inline-block">↻</span> : 'Commit'}
            </button>
            <button
              onClick={() => doCommit(true)}
              disabled={!commitMessage.trim() || loadingAction !== null}
              className="px-[18px] py-2.5 bg-panel-2 border border-border hover:bg-hover disabled:opacity-40
                         disabled:cursor-not-allowed text-fg-muted text-[13px] font-semibold rounded-[9px]
                         transition-colors"
            >
              Amend
            </button>
          </div>

          {/* Sync strip */}
          <div className="flex items-center justify-between mt-3 text-[11px] font-[450] font-mono text-fg-faint">
            <div className="flex items-center gap-1.5 min-w-0">
              {status.ahead > 0 && (
                <span className="text-green shrink-0">↑{status.ahead}</span>
              )}
              {status.behind > 0 && (
                <span className="text-red shrink-0">↓{status.behind}</span>
              )}
              {status.upstream && (
                <span className="truncate">{status.upstream}</span>
              )}
            </div>
            <div className="flex items-center gap-3 shrink-0">
              <button
                onClick={doFetch}
                disabled={loadingAction !== null}
                title="Fetch"
                className="hover:text-fg transition-colors"
              >
                {loading('fetch') ? <span className="animate-spin inline-block">↻</span> : '⟳'}
              </button>
              <button
                onClick={doPull}
                disabled={loadingAction !== null}
                title="Pull"
                className="hover:text-fg transition-colors"
              >
                {loading('pull') ? <span className="animate-spin inline-block">↻</span> : '↓ Pull'}
              </button>
              <button
                onClick={() => doPush(false)}
                disabled={loadingAction !== null}
                title="Push"
                className="hover:text-fg transition-colors"
              >
                {loading('push') ? <span className="animate-spin inline-block">↻</span> : '↑ Push'}
              </button>
            </div>
          </div>
        </div>
      </div>

      {/* Right panel: diff */}
      <div className="flex-1 min-w-0 flex flex-col overflow-hidden">
        {!selectedEntry ? (
          <div className="flex-1 flex items-center justify-center text-fg-faint text-[13px] font-[450] italic">
            Selecteer een bestand
          </div>
        ) : (
          <div className="flex-1 overflow-y-auto p-3">
            {diffError && (
              <div className="mb-2 bg-red-soft text-red px-3 py-2 rounded-[7px] text-xs">
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
