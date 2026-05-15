import { useState } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { BatchResult } from '../../bindings/github.com/rdm/sites-tool/internal/services'

export default function BatchTab() {
  const [results, setResults] = useState<BatchResult[] | null>(null)
  const [loading, setLoading] = useState<'fetch' | 'pull' | null>(null)

  const run = async (op: 'fetch' | 'pull') => {
    setLoading(op)
    setResults(null)
    try {
      const r = op === 'fetch'
        ? await Services.BatchService.FetchAll()
        : await Services.BatchService.PullAll()
      setResults(r ?? [])
    } catch (e) {
      setResults([])
    } finally {
      setLoading(null)
    }
  }

  const successCount = results?.filter(r => r.success).length ?? 0
  const failCount = results?.filter(r => !r.success).length ?? 0

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-hidden">
      {/* Actions bar */}
      <div className="px-4 py-3 border-b border-black/[0.08] shrink-0 flex items-center gap-3">
        <p className="text-xs text-gray-600 flex-1">
          Voer een operatie uit op alle gekoppelde git-repos tegelijk.
        </p>
        <button
          onClick={() => run('fetch')}
          disabled={loading !== null}
          className="px-3 py-1.5 bg-black/[0.08] hover:bg-black/[0.10] text-gray-900 text-xs rounded-lg transition-colors disabled:opacity-50 flex items-center gap-1.5"
        >
          {loading === 'fetch' ? <span className="animate-spin inline-block text-sm">↻</span> : '⟳'}
          Fetch all
        </button>
        <button
          onClick={() => run('pull')}
          disabled={loading !== null}
          className="px-3 py-1.5 bg-indigo-100 hover:bg-indigo-500/30 text-indigo-800 text-xs rounded-lg transition-colors disabled:opacity-50 flex items-center gap-1.5"
        >
          {loading === 'pull' ? <span className="animate-spin inline-block text-sm">↻</span> : '↓'}
          Pull all
        </button>
      </div>

      {/* Results */}
      <div className="flex-1 overflow-y-auto">
        {loading !== null && results === null && (
          <div className="flex items-center justify-center py-12 gap-2 text-gray-600 text-sm">
            <span className="animate-spin inline-block">↻</span>
            {loading === 'fetch' ? 'Fetching alle repos…' : 'Pulling alle repos…'}
          </div>
        )}

        {results !== null && (
          <>
            <div className="px-4 py-2 border-b border-black/[0.06] flex items-center gap-3 text-xs shrink-0">
              <span className="text-emerald-600">{successCount} geslaagd</span>
              {failCount > 0 && <span className="text-red-600">{failCount} mislukt</span>}
            </div>
            <div className="divide-y divide-black/[0.06]">
              {results.map(r => (
                <div key={r.projectId} className="flex items-center gap-3 px-4 py-2">
                  <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${r.success ? 'bg-emerald-400' : 'bg-red-400'}`} />
                  <span className="text-sm text-gray-800 flex-1 truncate">{r.displayName}</span>
                  {r.error && (
                    <span className="text-[10px] text-red-600 truncate max-w-[200px]" title={r.error}>
                      {r.error}
                    </span>
                  )}
                  {r.success && (
                    <span className="text-[10px] text-emerald-600 shrink-0">✓</span>
                  )}
                </div>
              ))}
            </div>
          </>
        )}

        {results === null && loading === null && (
          <div className="flex-1 flex items-center justify-center text-gray-600 text-sm italic py-12">
            Klik Fetch all of Pull all om te beginnen
          </div>
        )}
      </div>
    </div>
  )
}
