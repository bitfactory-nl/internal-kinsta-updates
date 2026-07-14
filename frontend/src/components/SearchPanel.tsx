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
      <div className="px-4 py-3 border-b border-border shrink-0">
        <input
          type="search"
          placeholder="Zoek in alle bestanden…"
          value={query}
          onChange={onChange}
          autoFocus
          className="w-full max-w-[520px] bg-panel border border-border rounded-[9px] px-3 py-2
                     text-[13px] text-fg placeholder-fg-faint outline-none
                     focus:border-accent focus:ring-1 focus:ring-accent/30"
        />
      </div>

      {/* Results */}
      <div className="flex-1 overflow-y-auto">
        {loading && (
          <div className="flex items-center justify-center py-8 gap-2 text-fg-faint text-[13px]">
            <span className="animate-spin inline-block">↻</span> Zoeken…
          </div>
        )}

        {!loading && results !== null && totalHits === 0 && (
          <p className="text-[13px] text-fg-faint italic text-center py-8">Geen resultaten voor "{query}"</p>
        )}

        {!loading && totalHits > 0 && (
          <>
            <div className="px-4 py-2 text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase border-b border-border shrink-0">
              {totalHits} treffer{totalHits !== 1 ? 's' : ''} in {Object.keys(grouped).length} project{Object.keys(grouped).length !== 1 ? 'en' : ''}
            </div>
            {Object.entries(grouped).map(([projectId, hits]) => (
              <div key={projectId}>
                <button
                  onClick={() => onSelectProject(projectId)}
                  className="w-full text-left px-4 py-2 text-[12px] font-semibold text-accent
                             bg-panel hover:bg-hover transition-colors border-b border-border"
                >
                  {hits[0].displayName}
                  <span className="ml-2 text-fg-faint font-normal font-mono">{hits.length}</span>
                </button>
                {hits.slice(0, 50).map((h, i) => (
                  <div
                    key={i}
                    className="flex items-start gap-2 px-4 py-1 border-b border-border hover:bg-hover transition-colors"
                  >
                    <span className="text-[11px] text-fg-faint font-mono shrink-0 w-8 text-right pt-px">
                      {h.line}
                    </span>
                    <span className="text-[11px] text-accent-2 font-mono shrink-0 truncate max-w-[140px]">
                      {h.file}
                    </span>
                    <span className="text-[12px] text-fg-muted font-mono truncate flex-1">
                      {h.content.trim()}
                    </span>
                  </div>
                ))}
                {hits.length > 50 && (
                  <p className="text-[11px] text-fg-faint px-4 py-1 italic">
                    + {hits.length - 50} meer…
                  </p>
                )}
              </div>
            ))}
          </>
        )}

        {!loading && results === null && query.length < 2 && query.length > 0 && (
          <p className="text-[13px] text-fg-faint italic text-center py-8">Typ minimaal 2 tekens…</p>
        )}
      </div>
    </div>
  )
}
