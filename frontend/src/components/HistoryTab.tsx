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
  const display = label.startsWith('tag:') ? label.slice(4) : label

  if (label === 'HEAD') {
    return (
      <span className="font-mono text-[9.5px] font-semibold text-green bg-green-soft px-1.5 py-[2px] rounded-[5px] shrink-0">
        {display}
      </span>
    )
  }

  return (
    <span className="font-mono text-[9.5px] font-medium text-fg-muted bg-panel-2 border border-border px-1.5 py-[2px] rounded-[5px] shrink-0">
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
      <div className="flex-1 min-w-0 max-w-[660px] shrink-0 flex flex-col border-r border-border overflow-hidden">
        {historyError && (
          <div className="m-2 bg-red-soft text-red px-3 py-2 rounded-[7px] text-xs">
            {historyError}
          </div>
        )}
        {loadingHistory ? (
          <div className="flex items-center justify-center py-8 gap-2 text-fg-faint text-[13px]">
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
          <div className="flex-1 flex items-center justify-center text-fg-faint text-[13px] font-[450] italic">
            Select a commit to view changes
          </div>
        ) : (
          <>
            {/* Commit header */}
            {selectedCommit && (
              <div className="px-4 py-3 border-b border-border shrink-0">
                <div className="flex items-start gap-2 mb-1 flex-wrap">
                  <span className="font-mono text-[11px] font-medium bg-panel-2 border border-border text-fg-muted px-1.5 py-px rounded-[5px] shrink-0">
                    {selectedCommit.shortHash}
                  </span>
                  {(selectedCommit.refs ?? []).map((r, i) => (
                    <RefPill key={i} label={r} />
                  ))}
                </div>
                <h3 className="text-[13px] font-medium text-fg mt-1 leading-snug">
                  {selectedCommit.subject}
                </h3>
                <div className="flex items-center gap-2 mt-1 text-[11.5px] font-[450] text-fg-faint">
                  <span>{selectedCommit.author}</span>
                  <span>·</span>
                  <span>{timeAgo(selectedCommit.authorDate)}</span>
                  {new Date(selectedCommit.authorDate).toLocaleDateString('nl-NL', {
                    day: 'numeric', month: 'short', year: 'numeric'
                  }) && (
                    <span className="text-fg-faint">
                      {new Date(selectedCommit.authorDate).toLocaleDateString('nl-NL', {
                        day: 'numeric', month: 'short', year: 'numeric'
                      })}
                    </span>
                  )}
                </div>
                {(selectedCommit.parents ?? []).length > 0 && (
                  <div className="flex items-center gap-1 mt-1.5 flex-wrap">
                    <span className="text-[11px] text-fg-faint">Parents:</span>
                    {selectedCommit.parents.map(p => (
                      <button
                        key={p}
                        onClick={() => selectCommit(p)}
                        className="font-mono text-[11px] text-accent hover:text-accent-2 transition-colors"
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
              <div className="border-b border-border shrink-0 max-h-[160px] overflow-y-auto">
                <div className="px-3 py-1.5">
                  <button
                    onClick={() => setSelectedFile(null)}
                    className={`w-full text-left text-[11px] px-2 py-1 rounded-[5px] transition-colors
                      ${!selectedFile ? 'bg-sel text-fg' : 'text-fg-muted hover:bg-hover'}`}
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
                        className={`w-full text-left flex items-center gap-2 px-2 py-0.5 rounded-[5px] transition-colors
                          ${selectedFile === d.path ? 'bg-sel text-fg' : 'text-fg-muted hover:bg-hover'}`}
                      >
                        <span className="font-mono text-[11px] flex-1 truncate">{d.path}</span>
                        <span className="text-[10px] font-mono shrink-0 flex gap-1">
                          {adds > 0 && <span className="text-green">+{adds}</span>}
                          {dels > 0 && <span className="text-red">-{dels}</span>}
                        </span>
                      </button>
                    )
                  })}
                </div>
              </div>
            )}

            {/* Diff error */}
            {diffError && (
              <div className="m-2 bg-red-soft text-red px-3 py-2 rounded-[7px] text-xs">
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
