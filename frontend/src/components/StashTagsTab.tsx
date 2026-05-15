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
      <div className="flex items-center justify-center py-8 gap-2 text-gray-600 text-sm">
        <span className="animate-spin inline-block">↻</span>
        <span>Loading…</span>
      </div>
    )
  }

  return (
    <div className="flex flex-1 min-h-0 overflow-hidden">
      {/* Stash section */}
      <div className="w-1/2 border-r border-black/[0.08] flex flex-col overflow-hidden">
        <div className="flex items-center justify-between px-4 py-3 border-b border-black/[0.08] shrink-0">
          <h3 className="text-sm font-semibold text-gray-900">Stash</h3>
          <button
            onClick={() => setShowStashForm(f => !f)}
            className="text-xs text-gray-600 hover:text-gray-800 hover:bg-black/[0.08]
                       px-2 py-1 rounded transition-colors"
          >
            {showStashForm ? 'Cancel' : '+ New stash'}
          </button>
        </div>

        {/* New stash form */}
        {showStashForm && (
          <div className="px-4 py-3 border-b border-black/[0.08] shrink-0">
            <input
              type="text"
              value={stashMessage}
              onChange={e => setStashMessage(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && saveStash()}
              placeholder="Stash message (optional)…"
              className="w-full bg-black/[0.05] text-sm text-gray-800 placeholder-gray-400
                         rounded-lg px-3 py-1.5 outline-none focus:ring-1 focus:ring-indigo-500/40
                         border border-transparent focus:border-indigo-400 mb-2"
            />
            <button
              onClick={saveStash}
              disabled={loadingAction !== null}
              className="w-full py-1.5 bg-indigo-600 hover:bg-indigo-500 disabled:opacity-40
                         text-gray-900 text-xs font-medium rounded-lg transition-colors"
            >
              {isLoading('stash-save') ? <span className="animate-spin inline-block">↻</span> : 'Save stash'}
            </button>
          </div>
        )}

        {(error || actionError) && (
          <div className="mx-4 mt-2 bg-red-100 text-red-600 px-3 py-2 rounded text-xs">
            {error || actionError}
          </div>
        )}

        <div className="flex-1 overflow-y-auto">
          {stashes.length === 0 ? (
            <p className="text-xs text-gray-600 italic px-4 py-3">No stashes</p>
          ) : (
            <div className="divide-y divide-black/[0.06]">
              {stashes.map(stash => (
                <div key={stash.index} className="px-4 py-3 hover:bg-black/[0.04] transition-colors">
                  <div className="flex items-start gap-2">
                    <span className="text-[10px] font-mono bg-black/[0.08] text-gray-600 px-1.5 py-px rounded shrink-0 mt-0.5">
                      {stash.index}
                    </span>
                    <div className="flex-1 min-w-0">
                      <p className="text-xs text-gray-800 font-medium leading-snug truncate">
                        {stash.message || 'WIP'}
                      </p>
                      <p className="text-[11px] text-gray-600 mt-0.5 font-mono">{stash.branch}</p>
                      <p className="text-[11px] text-gray-600 mt-0.5">{formatDate(stash.stashedAt)}</p>
                    </div>
                  </div>
                  <div className="flex items-center gap-1 mt-2">
                    <button
                      onClick={() => popStash(stash.index)}
                      disabled={loadingAction !== null}
                      className="text-xs text-gray-600 hover:text-gray-900 hover:bg-black/[0.08]
                                 px-2 py-0.5 rounded transition-colors"
                    >
                      {isLoading(`stash-pop-${stash.index}`) ? <span className="animate-spin inline-block">↻</span> : 'Pop'}
                    </button>
                    <button
                      onClick={() => dropStash(stash.index)}
                      disabled={loadingAction !== null}
                      className="text-xs text-gray-600 hover:text-red-600 hover:bg-red-500/10
                                 px-2 py-0.5 rounded transition-colors"
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
      <div className="w-1/2 flex flex-col overflow-hidden">
        <div className="flex items-center px-4 py-3 border-b border-black/[0.08] shrink-0">
          <h3 className="text-sm font-semibold text-gray-900">Tags</h3>
          <span className="ml-2 text-[11px] text-gray-600">({tags.length})</span>
        </div>

        <div className="flex-1 overflow-y-auto">
          {tags.length === 0 ? (
            <p className="text-xs text-gray-600 italic px-4 py-3">No tags</p>
          ) : (
            <div className="divide-y divide-black/[0.06]">
              {tags.map(tag => (
                <div key={tag.name} className="px-4 py-3 hover:bg-black/[0.04] transition-colors">
                  <div className="flex items-start gap-2">
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-1.5">
                        <span className="text-xs text-gray-800 font-medium font-mono">{tag.name}</span>
                        {tag.annotated && (
                          <span className="text-[10px] px-1.5 py-px rounded ring-1 text-emerald-700 ring-emerald-500/30">
                            annotated
                          </span>
                        )}
                      </div>
                      <p className="text-[11px] text-gray-600 font-mono mt-0.5">{tag.commit.slice(0, 7)}</p>
                      {tag.message && (
                        <p className="text-[11px] text-gray-600 mt-0.5 truncate">{tag.message}</p>
                      )}
                      <p className="text-[11px] text-gray-600 mt-0.5">{formatDate(tag.taggedAt)}</p>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
