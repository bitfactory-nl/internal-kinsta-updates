import { useState, useEffect } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { UpdateBranch } from '../../bindings/github.com/rdm/sites-tool/internal/services'

interface Props {
  projectId: string
  currentBranch: string
  onBranchCheckedOut: () => void
}

function formatDate(dateStr: string): string {
  // branch date format: 2026-04-30T11-57-29 (colons replaced by dashes by git)
  // Normalize to ISO: replace last two dashes in time part back to colons
  const normalized = dateStr.replace(/(\d{4}-\d{2}-\d{2}T\d{2})-(\d{2})-(\d{2})/, '$1:$2:$3')
  const d = new Date(normalized)
  if (isNaN(d.getTime())) return dateStr
  return d.toLocaleDateString('nl-NL', {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function timeAgo(dateStr: string): string {
  const normalized = dateStr.replace(/(\d{4}-\d{2}-\d{2}T\d{2})-(\d{2})-(\d{2})/, '$1:$2:$3')
  const d = new Date(normalized)
  if (isNaN(d.getTime())) return ''
  const diff = Math.floor((Date.now() - d.getTime()) / 1000)
  if (diff < 60) return `${diff}s geleden`
  if (diff < 3600) return `${Math.floor(diff / 60)}m geleden`
  if (diff < 86400) return `${Math.floor(diff / 3600)}u geleden`
  return `${Math.floor(diff / 86400)}d geleden`
}

export default function UpdatesTab({ projectId, currentBranch, onBranchCheckedOut }: Props) {
  const [branches, setBranches] = useState<UpdateBranch[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [checkingOut, setCheckingOut] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const fetchBranches = () => {
    setError(null)
    setLoading(true)
    Services.GitService.GetUpdateBranches(projectId)
      .then(b => setBranches(b ?? []))
      .catch(e => setError(String(e)))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    setBranches(null)
    fetchBranches()
  }, [projectId])

  const checkout = async (branch: UpdateBranch) => {
    setCheckingOut(branch.shortName)
    try {
      await Services.GitService.CheckoutBranch(projectId, branch.shortName)
      onBranchCheckedOut()
      fetchBranches()
    } catch (e) {
      setError(String(e))
    } finally {
      setCheckingOut(null)
    }
  }

  if (loading) {
    return (
      <div className="flex-1 flex items-center justify-center gap-2 text-gray-600 text-sm">
        <span className="animate-spin inline-block">↻</span>
      </div>
    )
  }

  if (error) {
    return <div className="m-4 bg-red-100 text-red-600 px-3 py-2 rounded text-xs">{error}</div>
  }

  if (!branches || branches.length === 0) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center gap-2 text-gray-600 text-sm italic py-12">
        <p>Geen update branches gevonden</p>
        <p className="text-xs text-gray-700">
          Verwacht: <code className="font-mono">automated/wp-updates-*</code>, <code className="font-mono">automated/updates-*</code> of <code className="font-mono">Updates - *</code>
        </p>
      </div>
    )
  }

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-hidden">
      <div className="px-4 py-2 border-b border-black/[0.06] shrink-0 flex items-center gap-2">
        <span className="text-[11px] text-gray-600">
          {branches.length} update branch{branches.length !== 1 ? 'es' : ''}
        </span>
        <button
          onClick={() => {
            setLoading(true)
            Services.GitService.Fetch(projectId)
              .then(() => fetchBranches())
              .catch(e => { setError(String(e)); setLoading(false) })
          }}
          className="ml-auto text-gray-600 hover:text-gray-800 text-xs transition-colors flex items-center gap-1"
          title="Fetch en ververs"
        >
          <span className="text-xs">⟳</span> Ververs
        </button>
      </div>

      <div className="flex-1 overflow-y-auto divide-y divide-black/[0.06]">
        {branches.map(branch => {
          const isActive = branch.shortName === currentBranch
          return (
            <div key={branch.shortName} className={`px-4 py-3 flex items-center gap-3 ${isActive ? 'bg-black/[0.03]' : ''}`}>
              {/* Status dot */}
              <span className={`w-2 h-2 rounded-full shrink-0 ${isActive ? 'bg-indigo-400' : branch.isLocal ? 'bg-emerald-400' : 'bg-gray-600'}`}
                title={isActive ? 'Actieve branch' : branch.isLocal ? 'Lokaal aanwezig' : 'Alleen remote'} />

              {/* Branch info */}
              <div className="flex-1 min-w-0">
                <p className={`text-sm font-mono truncate ${isActive ? 'text-gray-900' : 'text-gray-800'}`}>{branch.shortName}</p>
                <p className="text-[11px] text-gray-600 mt-0.5">
                  {formatDate(branch.dateStr)}
                  <span className="ml-2 text-gray-700">{timeAgo(branch.dateStr)}</span>
                </p>
              </div>

              {/* Action */}
              {isActive ? (
                <span className="text-[11px] text-indigo-600 shrink-0 px-2 py-0.5 bg-indigo-500/10 rounded">
                  ● actief
                </span>
              ) : branch.isLocal ? (
                <button
                  onClick={() => checkout(branch)}
                  disabled={checkingOut !== null}
                  className="shrink-0 px-3 py-1.5 text-xs bg-emerald-500/15 hover:bg-emerald-500/25
                             text-emerald-700 rounded-lg transition-colors disabled:opacity-50 flex items-center gap-1.5"
                >
                  {checkingOut === branch.shortName
                    ? <><span className="animate-spin inline-block text-xs">↻</span> Schakelen…</>
                    : '⇄ Schakel'}
                </button>
              ) : (
                <button
                  onClick={() => checkout(branch)}
                  disabled={checkingOut !== null}
                  className="shrink-0 px-3 py-1.5 text-xs bg-indigo-100 hover:bg-indigo-500/30
                             text-indigo-800 rounded-lg transition-colors disabled:opacity-50 flex items-center gap-1.5"
                >
                  {checkingOut === branch.shortName
                    ? <><span className="animate-spin inline-block text-xs">↻</span> Checken…</>
                    : '↓ Checkout'}
                </button>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
