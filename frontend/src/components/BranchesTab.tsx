import { useState, useEffect, useCallback } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { Branch } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'

export interface BranchesTabProps {
  projectId: string
  currentBranch: string
  onBranchChange: () => void
}

export default function BranchesTab({ projectId, currentBranch, onBranchChange }: BranchesTabProps) {
  const [branches, setBranches] = useState<Branch[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [loadingAction, setLoadingAction] = useState<string | null>(null)

  // New branch form
  const [newName, setNewName] = useState('')
  const [fromBranch, setFromBranch] = useState('')

  const loadBranches = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const result = await Services.GitService.GetBranches(projectId)
      setBranches(result ?? [])
      const cur = (result ?? []).find(b => b.isCurrent)
      if (cur && !fromBranch) {
        setFromBranch(cur.name)
      }
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [projectId, fromBranch])

  useEffect(() => {
    setBranches([])
    loadBranches()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId])

  const withAction = useCallback(async (key: string, fn: () => Promise<void>) => {
    setActionError(null)
    setLoadingAction(key)
    try {
      await fn()
      await loadBranches()
      onBranchChange()
    } catch (err: unknown) {
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoadingAction(null)
    }
  }, [loadBranches, onBranchChange])

  const checkout = useCallback((name: string) =>
    withAction(`checkout-${name}`, () => Services.GitService.CheckoutBranch(projectId, name)),
  [projectId, withAction])

  const merge = useCallback((name: string) =>
    withAction(`merge-${name}`, () => Services.GitService.MergeBranch(projectId, name)),
  [projectId, withAction])

  const deleteBranch = useCallback((name: string, force = false) =>
    withAction(`delete-${name}`, () => Services.GitService.DeleteBranch(projectId, name, force)),
  [projectId, withAction])

  const createBranch = useCallback(async () => {
    if (!newName.trim()) return
    await withAction('create', async () => {
      await Services.GitService.CreateBranch(projectId, newName.trim(), fromBranch || currentBranch)
      setNewName('')
    })
  }, [projectId, newName, fromBranch, currentBranch, withAction])

  const localBranches = branches.filter(b => !b.isRemote)
  const remoteBranches = branches.filter(b => b.isRemote)

  const isLoading = (key: string) => loadingAction === key

  return (
    <div className="flex flex-col flex-1 overflow-y-auto p-4 gap-4">
      {/* New branch form */}
      <div className="bg-black/[0.04] rounded-xl p-3 flex items-center gap-2">
        <input
          type="text"
          value={newName}
          onChange={e => setNewName(e.target.value)}
          onKeyDown={e => e.key === 'Enter' && createBranch()}
          placeholder="New branch name…"
          className="flex-1 bg-black/[0.05] text-sm text-gray-800 placeholder-gray-400
                     rounded-lg px-3 py-1.5 outline-none focus:ring-1 focus:ring-indigo-500/40
                     border border-transparent focus:border-indigo-400"
        />
        <select
          value={fromBranch}
          onChange={e => setFromBranch(e.target.value)}
          className="bg-black/[0.05] text-xs text-gray-600 rounded-lg px-2 py-1.5 outline-none
                     border border-black/[0.08] focus:ring-1 focus:ring-indigo-400 max-w-[140px]"
        >
          {localBranches.map(b => (
            <option key={b.name} value={b.name}>{b.name}</option>
          ))}
        </select>
        <button
          onClick={createBranch}
          disabled={!newName.trim() || loadingAction !== null}
          className="px-3 py-1.5 bg-indigo-600 hover:bg-indigo-500 disabled:opacity-40
                     disabled:cursor-not-allowed text-gray-900 text-xs font-medium rounded-lg
                     transition-colors shrink-0"
        >
          {isLoading('create') ? <span className="animate-spin inline-block">↻</span> : 'Create'}
        </button>
      </div>

      {(error || actionError) && (
        <div className="bg-red-100 text-red-600 px-3 py-2 rounded text-xs">
          {error || actionError}
        </div>
      )}

      {loading ? (
        <div className="flex items-center justify-center py-8 gap-2 text-gray-600 text-sm">
          <span className="animate-spin inline-block">↻</span>
          <span>Loading branches…</span>
        </div>
      ) : (
        <>
          {/* Local branches */}
          <div>
            <h3 className="text-[11px] font-semibold text-gray-600 uppercase tracking-wider mb-2">
              Local ({localBranches.length})
            </h3>
            <div className="space-y-0.5">
              {localBranches.map(branch => (
                <div
                  key={branch.name}
                  className="flex items-center gap-2 px-3 py-2 rounded-lg hover:bg-black/[0.04] transition-colors"
                >
                  {/* Current dot */}
                  <div className="w-3 shrink-0 flex items-center justify-center">
                    {branch.isCurrent && (
                      <span className="w-2 h-2 rounded-full bg-indigo-400 block" />
                    )}
                  </div>

                  {/* Name */}
                  <span className={`text-sm flex-1 font-mono truncate ${branch.isCurrent ? 'text-gray-900 font-medium' : 'text-gray-700'}`}>
                    {branch.name}
                  </span>

                  {/* Upstream */}
                  {branch.upstream && (
                    <span className="text-[10px] text-gray-600 font-mono shrink-0 hidden lg:block">
                      {branch.upstream}
                    </span>
                  )}

                  {/* Actions */}
                  {!branch.isCurrent && (
                    <div className="flex items-center gap-1 shrink-0">
                      <button
                        onClick={() => checkout(branch.name)}
                        disabled={loadingAction !== null}
                        className="text-xs text-gray-600 hover:text-gray-900 hover:bg-black/[0.08]
                                   px-2 py-0.5 rounded transition-colors"
                      >
                        {isLoading(`checkout-${branch.name}`) ? <span className="animate-spin inline-block">↻</span> : 'Checkout'}
                      </button>
                      <button
                        onClick={() => merge(branch.name)}
                        disabled={loadingAction !== null}
                        className="text-xs text-gray-600 hover:text-gray-900 hover:bg-black/[0.08]
                                   px-2 py-0.5 rounded transition-colors"
                      >
                        {isLoading(`merge-${branch.name}`) ? <span className="animate-spin inline-block">↻</span> : 'Merge'}
                      </button>
                      <button
                        onClick={() => deleteBranch(branch.name)}
                        disabled={loadingAction !== null}
                        className="text-xs text-gray-600 hover:text-red-600 hover:bg-red-500/10
                                   px-2 py-0.5 rounded transition-colors"
                      >
                        {isLoading(`delete-${branch.name}`) ? <span className="animate-spin inline-block">↻</span> : 'Delete'}
                      </button>
                    </div>
                  )}
                </div>
              ))}
              {localBranches.length === 0 && (
                <p className="text-xs text-gray-600 italic px-3">No local branches</p>
              )}
            </div>
          </div>

          {/* Divider */}
          <div className="border-t border-black/[0.08]" />

          {/* Remote branches */}
          <div>
            <h3 className="text-[11px] font-semibold text-gray-600 uppercase tracking-wider mb-2">
              Remote ({remoteBranches.length})
            </h3>
            <div className="space-y-0.5">
              {remoteBranches.map(branch => (
                <div
                  key={branch.fullRef}
                  className="flex items-center gap-2 px-3 py-2 rounded-lg hover:bg-black/[0.04] transition-colors"
                >
                  <div className="w-3 shrink-0" />
                  <span className="text-sm flex-1 font-mono truncate text-gray-600">
                    {branch.name}
                  </span>
                  <div className="flex items-center gap-1 shrink-0">
                    <button
                      onClick={() => {
                        const localName = branch.name.replace(/^[^/]+\//, '')
                        withAction(`checkout-remote-${branch.name}`, () =>
                          Services.GitService.CheckoutBranch(projectId, localName)
                        )
                      }}
                      disabled={loadingAction !== null}
                      className="text-xs text-gray-600 hover:text-gray-900 hover:bg-black/[0.08]
                                 px-2 py-0.5 rounded transition-colors"
                    >
                      {isLoading(`checkout-remote-${branch.name}`) ? <span className="animate-spin inline-block">↻</span> : 'Checkout as local'}
                    </button>
                  </div>
                </div>
              ))}
              {remoteBranches.length === 0 && (
                <p className="text-xs text-gray-600 italic px-3">No remote branches</p>
              )}
            </div>
          </div>
        </>
      )}
    </div>
  )
}
