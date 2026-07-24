import { useState, useEffect, useCallback } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type {
  Stash,
  Tag,
} from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'

export interface StashTagsTabProps {
  projectId: string
}

function formatDate(dateStr: string): string {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  if (isNaN(d.getTime())) return ''
  return d.toLocaleDateString('nl-NL', { day: 'numeric', month: 'short', year: 'numeric' })
}

export default function StashTagsTab({ projectId }: StashTagsTabProps) {
  const [stashes, setStashes] = useState<Stash[]>([])
  const [tags, setTags] = useState<Tag[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [loadingAction, setLoadingAction] = useState<string | null>(null)

  // New stash form
  const [showStashForm, setShowStashForm] = useState(false)
  const [stashMessage, setStashMessage] = useState('')

  const loadData = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [stashResult, tagResult] = await Promise.all([
        Services.GitService.GetStashes(projectId),
        Services.GitService.GetTags(projectId),
      ])
      setStashes(stashResult ?? [])
      // Sort tags newest first
      const sorted = (tagResult ?? []).sort((a, b) => {
        const ta = new Date(a.taggedAt).getTime()
        const tb = new Date(b.taggedAt).getTime()
        return tb - ta
      })
      setTags(sorted)
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [projectId])

  useEffect(() => {
    setStashes([])
    setTags([])
    loadData()
  }, [loadData, projectId])

  const withAction = useCallback(async (key: string, fn: () => Promise<void>) => {
    setActionError(null)
    setLoadingAction(key)
    try {
      await fn()
      await loadData()
    } catch (err: unknown) {
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoadingAction(null)
    }
  }, [loadData])

  const saveStash = useCallback(async () => {
    await withAction('stash-save', async () => {
      await Services.GitService.StashSave(projectId, stashMessage)
      setStashMessage('')
      setShowStashForm(false)
    })
  }, [projectId, stashMessage, withAction])

  const popStash = useCallback((index: number) =>
    withAction(`stash-pop-${index}`, () => Services.GitService.StashPop(projectId, index)),
  [projectId, withAction])

  const dropStash = useCallback((index: number) =>
    withAction(`stash-drop-${index}`, () => Services.GitService.StashDrop(projectId, index)),
  [projectId, withAction])

  const isLoading = (key: string) => loadingAction === key

  if (loading) {
    return (
      <div className="flex items-center justify-center py-8 gap-2 text-fg-muted text-sm">
        <span className="animate-spin inline-block">↻</span>
        <span>Loading…</span>
      </div>
    )
  }

  return (
    <div className="flex flex-1 min-h-0 overflow-hidden">
      {/* Stash section */}
      <div className="w-[360px] shrink-0 border-r border-border flex flex-col overflow-hidden">
        <div className="flex items-center justify-between px-[22px] pt-5 pb-4 shrink-0">
          <h3 className="text-[13px] font-semibold text-fg">Stash</h3>
          <button
            onClick={() => setShowStashForm(f => !f)}
            className="text-[12px] font-medium text-accent hover:text-accent-2 transition-colors"
          >
            {showStashForm ? 'Cancel' : '+ New stash'}
          </button>
        </div>

        {/* New stash form */}
        {showStashForm && (
          <div className="px-[22px] pb-4 shrink-0">
            <input
              type="text"
              value={stashMessage}
              onChange={e => setStashMessage(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && saveStash()}
              placeholder="Stash message (optional)…"
              className="w-full bg-panel border border-border rounded-[9px] px-3 py-2 text-[13px]
                         text-fg placeholder-fg-faint outline-none focus:border-accent
                         focus:ring-1 focus:ring-accent/30 mb-2"
            />
            <button
              onClick={saveStash}
              disabled={loadingAction !== null}
              className="w-full bg-accent text-white text-[12.5px] font-semibold py-[9px] rounded-[9px]
                         hover:bg-accent-2 transition-colors disabled:opacity-40"
            >
              {isLoading('stash-save') ? <span className="animate-spin inline-block">↻</span> : 'Save stash'}
            </button>
          </div>
        )}

        {(error || actionError) && (
          <div className="mx-[22px] mb-3 bg-red-soft text-red px-3 py-2 rounded-[9px] text-xs">
            {error || actionError}
          </div>
        )}

        <div className="flex-1 overflow-y-auto px-[22px] pb-5">
          {stashes.length === 0 ? (
            <p className="text-[13px] text-fg-faint italic">No stashes</p>
          ) : (
            <div className="bg-panel border border-border rounded-[11px] overflow-hidden divide-y divide-border">
              {stashes.map(stash => (
                <div key={stash.index} className="px-4 py-3 hover:bg-hover transition-colors">
                  <div className="flex items-start gap-2.5">
                    <span className="text-[10px] font-semibold font-mono bg-panel-2 border border-border text-fg-muted px-1.5 py-px rounded-[5px] shrink-0 mt-0.5">
                      {stash.index}
                    </span>
                    <div className="flex-1 min-w-0">
                      <p className="text-[13px] text-fg font-medium leading-snug truncate">
                        {stash.message || 'WIP'}
                      </p>
                      <p className="text-[11px] font-[450] text-fg-faint mt-0.5 font-mono">{stash.branch}</p>
                      <p className="text-[11px] font-[450] text-fg-faint mt-0.5">{formatDate(stash.stashedAt)}</p>
                    </div>
                  </div>
                  <div className="flex items-center gap-4 mt-2 text-[12px] font-medium">
                    <button
                      onClick={() => popStash(stash.index)}
                      disabled={loadingAction !== null}
                      className="text-accent hover:text-accent-2 disabled:opacity-40 transition-colors"
                    >
                      {isLoading(`stash-pop-${stash.index}`) ? <span className="animate-spin inline-block">↻</span> : 'Pop'}
                    </button>
                    <button
                      onClick={() => dropStash(stash.index)}
                      disabled={loadingAction !== null}
                      className="text-red hover:opacity-80 disabled:opacity-40 transition-opacity"
                    >
                      {isLoading(`stash-drop-${stash.index}`) ? <span className="animate-spin inline-block">↻</span> : 'Drop'}
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Tags section */}
      <div className="flex-1 min-w-0 flex flex-col overflow-hidden">
        <div className="flex-1 overflow-y-auto px-6 pt-5 pb-10">
          <div className="flex items-baseline gap-2 mb-3.5">
            <h3 className="text-[13px] font-semibold text-fg">Tags</h3>
            <span className="text-[12px] font-medium font-mono text-fg-faint">{tags.length}</span>
          </div>

          {tags.length === 0 ? (
            <p className="text-[13px] text-fg-faint italic">No tags</p>
          ) : (
            <div className="bg-panel border border-border rounded-[11px] overflow-hidden divide-y divide-border">
              {tags.map(tag => (
                <div key={tag.name} className="px-4 py-[13px] hover:bg-hover transition-colors">
                  <div className="flex items-center gap-2.5">
                    <span className="text-[13px] font-semibold font-mono text-fg">{tag.name}</span>
                    <span className="text-[11px] font-[450] font-mono text-fg-faint">{tag.commit.slice(0, 7)}</span>
                    {tag.annotated && (
                      <span className="text-[10px] font-semibold font-mono px-1.5 py-px rounded-full text-green bg-green-soft">
                        annotated
                      </span>
                    )}
                    <span className="ml-auto text-[11.5px] font-[450] text-fg-faint">
                      {formatDate(tag.taggedAt)}
                    </span>
                  </div>
                  {tag.message && (
                    <p className="text-[12px] font-[450] text-fg-muted mt-1 truncate">{tag.message}</p>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
