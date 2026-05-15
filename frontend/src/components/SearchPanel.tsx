import { useState, useRef } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { SearchHit } from '../../bindings/github.com/rdm/sites-tool/internal/services'

interface Props {
  onSelectProject: (projectId: string) => void
}

export default function SearchPanel({ onSelectProject }: Props) {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchHit[] | null>(null)
  const [loading, setLoading] = useState(false)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const search = async (q: string) => {
    if (q.trim().length < 2) {
      setResults(null)
      return
    }
    setLoading(true)
    try {
      const hits = await Services.SearchService.GrepAll(q)
      setResults(hits ?? [])
    } catch {
      setResults([])
    } finally {
      setLoading(false)
    }
  }

  const onChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const q = e.target.value
    setQuery(q)
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => search(q), 400)
  }

  const grouped = results?.reduce<Record<string, SearchHit[]>>((acc, h) => {
    if (!acc[h.projectId]) acc[h.projectId] = []
    acc[h.projectId].push(h)
    return acc
  }, {}) ?? {}

  const totalHits = results?.length ?? 0

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-hidden">
      {/* Search input */}
      <div className="px-4 py-3 border-b border-black/[0.08] shrink-0">
        <input
          type="search"
          placeholder="Zoek in alle bestanden…"
          value={query}
          onChange={onChange}
          autoFocus
          className="w-full bg-black/[0.05] text-sm text-gray-800 placeholder-gray-400
                     rounded-lg px-3 py-1.5 outline-none focus:ring-1 focus:ring-indigo-500/40
                     border border-transparent focus:border-indigo-400"
        />
      </div>

      {/* Results */}
      <div className="flex-1 overflow-y-auto">
        {loading && (
          <div className="flex items-center justify-center py-8 gap-2 text-gray-600 text-sm">
            <span className="animate-spin inline-block">↻</span> Zoeken…
          </div>
        )}

        {!loading && results !== null && totalHits === 0 && (
          <p className="text-xs text-gray-600 text-center py-8">Geen resultaten voor "{query}"</p>
        )}

        {!loading && totalHits > 0 && (
          <>
            <div className="px-4 py-1.5 text-[11px] text-gray-600 border-b border-black/[0.06] shrink-0">
              {totalHits} treffer{totalHits !== 1 ? 's' : ''} in {Object.keys(grouped).length} project{Object.keys(grouped).length !== 1 ? 'en' : ''}
            </div>
            {Object.entries(grouped).map(([projectId, hits]) => (
              <div key={projectId}>
                <button
                  onClick={() => onSelectProject(projectId)}
                  className="w-full text-left px-4 py-1.5 text-[11px] font-semibold text-indigo-700
                             bg-black/[0.03] hover:bg-black/[0.05] transition-colors border-b border-black/[0.06]"
                >
                  {hits[0].displayName}
                  <span className="ml-2 text-gray-600 font-normal">{hits.length}</span>
                </button>
                {hits.slice(0, 50).map((h, i) => (
                  <div
                    key={i}
                    className="flex items-start gap-2 px-4 py-1 border-b border-black/[0.05] hover:bg-black/[0.03]"
                  >
                    <span className="text-[10px] text-gray-600 font-mono shrink-0 w-8 text-right pt-px">
                      {h.line}
                    </span>
                    <span className="text-[10px] text-indigo-600 font-mono shrink-0 truncate max-w-[140px]">
                      {h.file}
                    </span>
                    <span className="text-[11px] text-gray-700 font-mono truncate flex-1">
                      {h.content.trim()}
                    </span>
                  </div>
                ))}
                {hits.length > 50 && (
                  <p className="text-[10px] text-gray-600 px-4 py-1 italic">
                    + {hits.length - 50} meer…
                  </p>
                )}
              </div>
            ))}
          </>
        )}

        {!loading && results === null && query.length < 2 && query.length > 0 && (
          <p className="text-xs text-gray-600 text-center py-8">Typ minimaal 2 tekens…</p>
        )}
      </div>
    </div>
  )
}
