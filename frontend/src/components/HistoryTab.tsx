import { useState, useEffect, useCallback } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type {
  GraphCommit,
  FileDiff,
} from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import CommitGraph from './CommitGraph'
import DiffViewer from './DiffViewer'

export interface HistoryTabProps {
  projectId: string
}

function timeAgo(dateStr: string): string {
  if (!dateStr) return ''
  const now = Date.now()
  const then = new Date(dateStr).getTime()
  if (isNaN(then)) return ''
  const diff = Math.floor((now - then) / 1000)
  if (diff < 60) return `${diff}s ago`
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
  if (diff < 604800) return `${Math.floor(diff / 86400)}d ago`
  if (diff < 2592000) return `${Math.floor(diff / 604800)}w ago`
  if (diff < 31536000) return `${Math.floor(diff / 2592000)}mo ago`
  return `${Math.floor(diff / 31536000)}y ago`
}

function RefPill({ label }: { label: string }) {
  let cls = 'text-gray-600 ring-gray-400/50'
  if (label === 'HEAD') cls = 'text-yellow-700 ring-yellow-400/40'
  else if (label.startsWith('tag:')) cls = 'text-emerald-700 ring-emerald-500/30'
  else if (label.startsWith('origin/') || label.includes('remote')) cls = 'text-gray-600 ring-gray-400/50'
  else cls = 'text-indigo-700 ring-indigo-500/30'

  const display = label.startsWith('tag:') ? label.slice(4) : label
  return (
    <span className={`text-[10px] px-1.5 py-px rounded ring-1 ${cls} font-mono`}>
      {display}
    </span>
  )
}

export default function HistoryTab({ projectId }: HistoryTabProps) {
  const [commits, setCommits] = useState<GraphCommit[]>([])
  const [loadingHistory, setLoadingHistory] = useState(false)
  const [historyError, setHistoryError] = useState<string | null>(null)

  const [selectedHash, setSelectedHash] = useState<string | null>(null)
  const [selectedCommit, setSelectedCommit] = useState<GraphCommit | null>(null)

  const [commitDiffs, setCommitDiffs] = useState<FileDiff[] | null>(null)
  const [loadingDiff, setLoadingDiff] = useState(false)
  const [diffError, setDiffError] = useState<string | null>(null)
  const [selectedFile, setSelectedFile] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setCommits([])
    setSelectedHash(null)
    setSelectedCommit(null)
    setCommitDiffs(null)
    setLoadingHistory(true)
    setHistoryError(null)

    Services.GitService.GetHistory(projectId, 200)
      .then((result: import('../../bindings/github.com/rdm/sites-tool/internal/domain/models').GraphCommit[]) => {
        if (!cancelled) setCommits(result ?? [])
      })
      .catch((err: unknown) => {
        if (!cancelled) setHistoryError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => {
        if (!cancelled) setLoadingHistory(false)
      })

    return () => { cancelled = true }
  }, [projectId])

  // Load diff when commit is selected
  const selectCommit = useCallback(async (hash: string) => {
    setSelectedHash(hash)
    setSelectedFile(null)
    const found = commits.find(c => c.hash === hash) ?? null
    setSelectedCommit(found)
    setLoadingDiff(true)
    setDiffError(null)
    try {
      const diffs = await Services.GitService.GetCommitDiff(projectId, hash)
      setCommitDiffs(diffs ?? [])
    } catch (err: unknown) {
      setDiffError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoadingDiff(false)
    }
  }, [projectId, commits])

  const visibleDiffs = selectedFile
    ? (commitDiffs ?? []).filter(d => d.path === selectedFile)
    : commitDiffs

  return (
    <div className="flex flex-1 min-h-0 overflow-hidden">
      {/* Left: Commit graph */}
      <div className="w-[40%] shrink-0 flex flex-col border-r border-black/[0.08] overflow-hidden">
        {historyError && (
          <div className="m-2 bg-red-100 text-red-600 px-3 py-2 rounded text-xs">
            {historyError}
          </div>
        )}
        {loadingHistory ? (
          <div className="flex items-center justify-center py-8 gap-2 text-gray-600 text-sm">
            <span className="animate-spin inline-block">↻</span>
            <span>Loading history…</span>
          </div>
        ) : (
          <div className="flex-1 overflow-y-auto">
            <CommitGraph
              commits={commits}
              selectedHash={selectedHash}
              onSelect={selectCommit}
            />
          </div>
        )}
      </div>

      {/* Right: Diff panel */}
      <div className="flex-1 min-w-0 flex flex-col overflow-hidden">
        {!selectedHash ? (
          <div className="flex-1 flex items-center justify-center text-gray-600 text-sm italic">
            Select a commit to view changes
          </div>
        ) : (
          <>
            {/* Commit header */}
            {selectedCommit && (
              <div className="px-4 py-3 border-b border-black/[0.08] shrink-0">
                <div className="flex items-start gap-2 mb-1 flex-wrap">
                  <span className="font-mono text-[11px] bg-black/[0.08] text-gray-700 px-1.5 py-px rounded shrink-0">
                    {selectedCommit.shortHash}
                  </span>
                  {(selectedCommit.refs ?? []).map((r, i) => (
                    <RefPill key={i} label={r} />
                  ))}
                </div>
                <h3 className="text-sm font-medium text-gray-900 mt-1 leading-snug">
                  {selectedCommit.subject}
                </h3>
                <div className="flex items-center gap-2 mt-1 text-xs text-gray-600">
                  <span>{selectedCommit.author}</span>
                  <span>·</span>
                  <span>{timeAgo(selectedCommit.authorDate)}</span>
                  {new Date(selectedCommit.authorDate).toLocaleDateString('nl-NL', {
                    day: 'numeric', month: 'short', year: 'numeric'
                  }) && (
                    <span className="text-gray-600">
                      {new Date(selectedCommit.authorDate).toLocaleDateString('nl-NL', {
                        day: 'numeric', month: 'short', year: 'numeric'
                      })}
                    </span>
                  )}
                </div>
                {(selectedCommit.parents ?? []).length > 0 && (
                  <div className="flex items-center gap-1 mt-1.5 flex-wrap">
                    <span className="text-[11px] text-gray-600">Parents:</span>
                    {selectedCommit.parents.map(p => (
                      <button
                        key={p}
                        onClick={() => selectCommit(p)}
                        className="font-mono text-[11px] text-indigo-600 hover:text-indigo-700 transition-colors"
                      >
                        {p.slice(0, 7)}
                      </button>
                    ))}
                  </div>
                )}
              </div>
            )}

            {/* File list */}
            {!loadingDiff && (commitDiffs ?? []).length > 0 && (
              <div className="border-b border-black/[0.08] shrink-0 max-h-[160px] overflow-y-auto">
                <div className="px-3 py-1.5">
                  <button
                    onClick={() => setSelectedFile(null)}
                    className={`w-full text-left text-[11px] px-2 py-1 rounded transition-colors
                      ${!selectedFile ? 'bg-black/[0.08] text-gray-900' : 'text-gray-600 hover:bg-black/[0.04]'}`}
                  >
                    All files ({commitDiffs!.length})
                  </button>
                  {commitDiffs!.map((d, i) => {
                    const adds = (d.hunks ?? []).reduce((s, h) =>
                      s + (h.lines ?? []).filter(l => l.kind === 'add').length, 0)
                    const dels = (d.hunks ?? []).reduce((s, h) =>
                      s + (h.lines ?? []).filter(l => l.kind === 'del').length, 0)
                    return (
                      <button
                        key={`${d.path}-${i}`}
                        onClick={() => setSelectedFile(d.path === selectedFile ? null : d.path)}
                        className={`w-full text-left flex items-center gap-2 px-2 py-0.5 rounded transition-colors
                          ${selectedFile === d.path ? 'bg-black/[0.08] text-gray-900' : 'text-gray-600 hover:bg-black/[0.04]'}`}
                      >
                        <span className="font-mono text-[11px] flex-1 truncate">{d.path}</span>
                        <span className="text-[10px] font-mono shrink-0 flex gap-1">
                          {adds > 0 && <span className="text-emerald-600">+{adds}</span>}
                          {dels > 0 && <span className="text-red-600">-{dels}</span>}
                        </span>
                      </button>
                    )
                  })}
                </div>
              </div>
            )}

            {/* Diff error */}
            {diffError && (
              <div className="m-2 bg-red-100 text-red-600 px-3 py-2 rounded text-xs">
                {diffError}
              </div>
            )}

            {/* Diff content */}
            <div className="flex-1 overflow-y-auto p-3">
              <DiffViewer diffs={visibleDiffs} loading={loadingDiff} />
            </div>
          </>
        )}
      </div>
    </div>
  )
}
