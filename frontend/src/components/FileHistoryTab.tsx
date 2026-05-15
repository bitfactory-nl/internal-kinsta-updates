import { useState, useEffect } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { Commit, FileDiff } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import FilePicker from './FilePicker'
import DiffViewer from './DiffViewer'

interface Props { projectId: string }

function timeAgo(dateStr: string): string {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  if (isNaN(d.getTime())) return ''
  const diff = Math.floor((Date.now() - d.getTime()) / 1000)
  if (diff < 60) return `${diff}s ago`
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
  if (diff < 604800) return `${Math.floor(diff / 86400)}d ago`
  return d.toLocaleDateString('nl-NL', { day: 'numeric', month: 'short', year: 'numeric' })
}

export default function FileHistoryTab({ projectId }: Props) {
  const [files, setFiles] = useState<string[]>([])
  const [loadingFiles, setLoadingFiles] = useState(false)
  const [selectedFile, setSelectedFile] = useState<string | null>(null)

  const [commits, setCommits] = useState<Commit[]>([])
  const [loadingHistory, setLoadingHistory] = useState(false)

  const [selectedHash, setSelectedHash] = useState<string | null>(null)
  const [diffs, setDiffs] = useState<FileDiff[] | null>(null)
  const [loadingDiff, setLoadingDiff] = useState(false)

  // Load file list on project change
  useEffect(() => {
    setFiles([])
    setSelectedFile(null)
    setCommits([])
    setSelectedHash(null)
    setDiffs(null)
    setLoadingFiles(true)
    Services.GitService.GetFiles(projectId)
      .then((result: string[]) => setFiles(result ?? []))
      .catch(() => setFiles([]))
      .finally(() => setLoadingFiles(false))
  }, [projectId])

  const selectFile = async (path: string) => {
    setSelectedFile(path)
    setCommits([])
    setSelectedHash(null)
    setDiffs(null)
    setLoadingHistory(true)
    try {
      const result = await Services.GitService.GetFileHistory(projectId, path)
      setCommits(result ?? [])
    } catch {
      setCommits([])
    } finally {
      setLoadingHistory(false)
    }
  }

  const selectCommit = async (hash: string) => {
    setSelectedHash(hash)
    setLoadingDiff(true)
    setDiffs(null)
    try {
      const result = await Services.GitService.GetCommitDiff(projectId, hash)
      const filtered = (result ?? []).filter(d => d.path === selectedFile || d.oldPath === selectedFile)
      setDiffs(filtered.length > 0 ? filtered : result ?? [])
    } catch {
      setDiffs([])
    } finally {
      setLoadingDiff(false)
    }
  }

  return (
    <div className="flex flex-1 min-h-0 overflow-hidden">
      {/* Col 1: file picker */}
      <div className="w-[220px] shrink-0 border-r border-black/[0.08] flex flex-col overflow-hidden">
        {loadingFiles ? (
          <div className="flex items-center justify-center py-4 text-gray-600 text-xs gap-1">
            <span className="animate-spin inline-block">↻</span> Laden…
          </div>
        ) : (
          <FilePicker files={files} selected={selectedFile} onSelect={selectFile} />
        )}
      </div>

      {/* Col 2: commit list for selected file */}
      <div className="w-[240px] shrink-0 border-r border-black/[0.08] flex flex-col overflow-hidden">
        {!selectedFile ? (
          <div className="flex-1 flex items-center justify-center text-gray-600 text-sm italic p-4 text-center">
            Selecteer een bestand
          </div>
        ) : loadingHistory ? (
          <div className="flex-1 flex items-center justify-center gap-2 text-gray-600 text-sm">
            <span className="animate-spin inline-block">↻</span>
          </div>
        ) : commits.length === 0 ? (
          <div className="flex-1 flex items-center justify-center text-gray-600 text-sm italic">
            Geen geschiedenis
          </div>
        ) : (
          <>
            <div className="px-3 py-1.5 text-[11px] text-gray-600 border-b border-black/[0.06] shrink-0 truncate">
              {commits.length} commit{commits.length !== 1 ? 's' : ''} — {selectedFile}
            </div>
            <div className="flex-1 overflow-y-auto">
              {commits.map(c => (
                <button
                  key={c.hash}
                  onClick={() => selectCommit(c.hash)}
                  className={`w-full text-left px-3 py-2 border-b border-black/[0.06] transition-colors
                    ${selectedHash === c.hash ? 'bg-indigo-100' : 'hover:bg-black/[0.04]'}`}
                >
                  <div className="flex items-center gap-2 mb-0.5">
                    <span className="font-mono text-[10px] text-indigo-600 shrink-0">
                      {c.shortHash ?? c.hash?.slice(0, 7)}
                    </span>
                    <span className="text-[10px] text-gray-600 ml-auto shrink-0">
                      {timeAgo(String(c.authoredAt ?? ''))}
                    </span>
                  </div>
                  <p className="text-xs text-gray-800 truncate leading-snug">{c.subject}</p>
                  <p className="text-[10px] text-gray-600 truncate mt-0.5">{c.author?.name ?? ''}</p>
                </button>
              ))}
            </div>
          </>
        )}
      </div>

      {/* Col 3: diff for selected commit */}
      <div className="flex-1 min-w-0 flex flex-col overflow-hidden">
        {!selectedHash ? (
          <div className="flex-1 flex items-center justify-center text-gray-600 text-sm italic">
            Selecteer een commit
          </div>
        ) : loadingDiff ? (
          <div className="flex-1 flex items-center justify-center gap-2 text-gray-600 text-sm">
            <span className="animate-spin inline-block">↻</span>
          </div>
        ) : (
          <div className="flex-1 overflow-y-auto p-3">
            <DiffViewer diffs={diffs} loading={false} />
          </div>
        )}
      </div>
    </div>
  )
}
