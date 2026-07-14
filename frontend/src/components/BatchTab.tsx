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
      <div className="px-6 py-4 border-b border-border shrink-0 flex items-center gap-3">
        <p className="text-[12.5px] text-fg-muted flex-1">
          Voer een operatie uit op alle gekoppelde git-repos tegelijk.
        </p>
        <button
          onClick={() => run('fetch')}
          disabled={loading !== null}
          className="bg-panel-2 border border-border text-fg-muted text-[12.5px] font-semibold px-[15px] py-[9px]
                     rounded-[9px] hover:bg-hover transition-colors disabled:opacity-50 flex items-center gap-1.5"
        >
          {loading === 'fetch' ? <span className="animate-spin inline-block text-sm">↻</span> : '⟳'}
          Fetch all
        </button>
        <button
          onClick={() => run('pull')}
          disabled={loading !== null}
          className="bg-accent text-white text-[12.5px] font-semibold px-[18px] py-[9px] rounded-[9px]
                     hover:bg-accent-2 transition-colors disabled:opacity-50 flex items-center gap-1.5"
        >
          {loading === 'pull' ? <span className="animate-spin inline-block text-sm">↻</span> : '↓'}
          Pull all
        </button>
      </div>

      {/* Results */}
      <div className="flex-1 overflow-y-auto px-6 py-5">
        {loading !== null && results === null && (
          <div className="flex items-center justify-center py-12 gap-2 text-fg-faint text-[13px]">
            <span className="animate-spin inline-block">↻</span>
            {loading === 'fetch' ? 'Fetching alle repos…' : 'Pulling alle repos…'}
          </div>
        )}

        {results !== null && (
          <>
            <div className="mb-2.5 flex items-center gap-3 text-[11px] font-semibold tracking-[.08em] uppercase">
              <span className="text-green">{successCount} geslaagd</span>
              {failCount > 0 && <span className="text-red">{failCount} mislukt</span>}
            </div>
            <div className="bg-panel border border-border rounded-[11px] overflow-hidden divide-y divide-border">
              {results.map(r => (
                <div key={r.projectId} className="flex items-center gap-3 px-4 py-3 hover:bg-hover transition-colors">
                  <span className={`w-2 h-2 rounded-full shrink-0 ${r.success ? 'bg-green' : 'bg-red'}`} />
                  <span className="text-[13px] font-medium text-fg flex-1 truncate">{r.displayName}</span>
                  {r.error && (
                    <span className="text-[11px] text-red truncate max-w-[200px]" title={r.error}>
                      {r.error}
                    </span>
                  )}
                  {r.success && (
                    <span className="text-[11px] text-green shrink-0">✓</span>
                  )}
                </div>
              ))}
            </div>
          </>
        )}

        {results === null && loading === null && (
          <div className="flex items-center justify-center text-fg-faint text-[13px] italic py-12">
            Klik Fetch all of Pull all om te beginnen
          </div>
        )}
      </div>
    </div>
  )
}
