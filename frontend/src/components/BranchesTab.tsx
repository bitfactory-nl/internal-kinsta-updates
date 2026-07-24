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
    <div className="flex-1 overflow-y-auto">
      <div className="px-[26px] pt-[22px] pb-[50px] max-w-[1000px]">
        {/* New branch form */}
        <div className="flex items-stretch gap-2.5 mb-[26px]">
          <input
            type="text"
            value={newName}
            onChange={e => setNewName(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && createBranch()}
            placeholder="New branch name…"
            className="flex-1 bg-panel border border-border rounded-[9px] px-[13px] py-[9px] text-[13px]
                       text-fg placeholder-fg-faint outline-none focus:border-accent
                       focus:ring-1 focus:ring-accent/30"
          />
          <select
            value={fromBranch}
            onChange={e => setFromBranch(e.target.value)}
            className="bg-panel border border-border rounded-[9px] px-[13px] py-[9px] text-[12.5px]
                       font-mono font-[450] text-fg-muted outline-none focus:border-accent
                       focus:ring-1 focus:ring-accent/30 max-w-[200px]"
          >
            {localBranches.map(b => (
              <option key={b.name} value={b.name}>{b.name}</option>
            ))}
          </select>
          <button
            onClick={createBranch}
            disabled={!newName.trim() || loadingAction !== null}
            className="bg-accent text-white text-[12.5px] font-semibold px-[20px] rounded-[9px]
                       hover:bg-accent-2 transition-colors disabled:opacity-40
                       disabled:cursor-not-allowed shrink-0"
          >
            {isLoading('create') ? <span className="animate-spin inline-block">↻</span> : 'Create'}
          </button>
        </div>

        {(error || actionError) && (
          <div className="bg-red-soft text-red px-3 py-2 rounded-[9px] text-xs mb-4">
            {error || actionError}
          </div>
        )}

        {loading ? (
          <div className="flex items-center justify-center py-8 gap-2 text-fg-muted text-sm">
            <span className="animate-spin inline-block">↻</span>
            <span>Loading branches…</span>
          </div>
        ) : (
          <>
            {/* Local branches */}
            <div className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">
              Local · {localBranches.length}
            </div>
            <div className="bg-panel border border-border rounded-[11px] overflow-hidden mb-[26px]">
              {localBranches.length === 0 ? (
                <p className="text-[13px] text-fg-faint italic px-4 py-3">No local branches</p>
              ) : (
                <div className="divide-y divide-border">
                  {localBranches.map(branch => (
                    <div
                      key={branch.name}
                      className="flex items-center gap-[11px] px-4 py-3 hover:bg-hover transition-colors"
                    >
                      {/* Current dot */}
                      <span
                        className={`w-[7px] h-[7px] rounded-full shrink-0 ${
                          branch.isCurrent ? 'bg-accent' : 'bg-fg-faint opacity-50'
                        }`}
                      />

                      {/* Name */}
                      <span
                        className={`flex-1 min-w-0 text-[13px] font-mono truncate ${
                          branch.isCurrent ? 'font-semibold text-fg' : 'font-[450] text-fg-muted'
                        }`}
                      >
                        {branch.name}
                      </span>

                      {/* Upstream */}
                      {branch.upstream && (
                        <span className="text-[11px] font-[450] text-fg-faint font-mono shrink-0 hidden lg:block">
                          {branch.upstream}
                        </span>
                      )}

                      {/* Actions */}
                      {!branch.isCurrent && (
                        <div className="flex items-center gap-4 shrink-0 text-[12px] font-medium">
                          <button
                            onClick={() => checkout(branch.name)}
                            disabled={loadingAction !== null}
                            className="text-accent hover:text-accent-2 disabled:opacity-40 transition-colors"
                          >
                            {isLoading(`checkout-${branch.name}`) ? <span className="animate-spin inline-block">↻</span> : 'Checkout'}
                          </button>
                          <button
                            onClick={() => merge(branch.name)}
                            disabled={loadingAction !== null}
                            className="text-fg-muted hover:text-fg disabled:opacity-40 transition-colors"
                          >
                            {isLoading(`merge-${branch.name}`) ? <span className="animate-spin inline-block">↻</span> : 'Merge'}
                          </button>
                          <button
                            onClick={() => deleteBranch(branch.name)}
                            disabled={loadingAction !== null}
                            className="text-red hover:opacity-80 disabled:opacity-40 transition-opacity"
                          >
                            {isLoading(`delete-${branch.name}`) ? <span className="animate-spin inline-block">↻</span> : 'Delete'}
                          </button>
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>

            {/* Remote branches */}
            <div className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">
              Remote · {remoteBranches.length}
            </div>
            <div className="bg-panel border border-border rounded-[11px] overflow-hidden">
              {remoteBranches.length === 0 ? (
                <p className="text-[13px] text-fg-faint italic px-4 py-3">No remote branches</p>
              ) : (
                <div className="divide-y divide-border">
                  {remoteBranches.map(branch => (
                    <div
                      key={branch.fullRef}
                      className="flex items-center gap-[11px] px-4 py-3 hover:bg-hover transition-colors"
                    >
                      <span className="w-[7px] h-[7px] rounded-full shrink-0 bg-fg-faint opacity-50" />
                      <span className="flex-1 min-w-0 text-[13px] font-mono font-[450] truncate text-fg-muted">
                        {branch.name}
                      </span>
                      <button
                        onClick={() => {
                          const localName = branch.name.replace(/^[^/]+\//, '')
                          withAction(`checkout-remote-${branch.name}`, () =>
                            Services.GitService.CheckoutBranch(projectId, localName)
                          )
                        }}
                        disabled={loadingAction !== null}
                        className="text-[12px] font-medium text-accent hover:text-accent-2
                                   disabled:opacity-40 transition-colors shrink-0"
                      >
                        {isLoading(`checkout-remote-${branch.name}`) ? <span className="animate-spin inline-block">↻</span> : 'Checkout as local'}
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  )
}
