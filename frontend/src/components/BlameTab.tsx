import { useState, useEffect } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { BlameLine } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import FilePicker from './FilePicker'

interface Props { projectId: string }

const LINE_H = 20

function timeAgo(dateStr: string): string {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  if (isNaN(d.getTime())) return ''
  const diff = Math.floor((Date.now() - d.getTime()) / 1000)
  if (diff < 60) return `${diff}s`
  if (diff < 3600) return `${Math.floor(diff / 60)}m`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h`
  if (diff < 604800) return `${Math.floor(diff / 86400)}d`
  return d.toLocaleDateString('nl-NL', { day: 'numeric', month: 'short' })
}

export default function BlameTab({ projectId }: Props) {
  const [files, setFiles] = useState<string[]>([])
  const [selectedFile, setSelectedFile] = useState<string | null>(null)
  const [lines, setLines] = useState<BlameLine[]>([])
  const [loading, setLoading] = useState(false)
  const [loadingFiles, setLoadingFiles] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [hoveredCommit, setHoveredCommit] = useState<string | null>(null)

  // Load file list on mount / project change
  useEffect(() => {
    setFiles([])
    setSelectedFile(null)
    setLines([])
    setError(null)
    setLoadingFiles(true)
    Services.GitService.GetFiles(projectId)
      .then((result: string[]) => setFiles(result ?? []))
      .catch(() => setFiles([]))
      .finally(() => setLoadingFiles(false))
  }, [projectId])

  const selectFile = async (path: string) => {
    setSelectedFile(path)
    setLoading(true)
    setError(null)
    setLines([])
    try {
      const result = await Services.GitService.GetBlame(projectId, path)
      setLines(result ?? [])
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }

  const commitBlocks = lines.reduce<{ hash: string; count: number }[]>((acc, line) => {
    const last = acc[acc.length - 1]
    if (last && last.hash === line.commit) { last.count++; return acc }
    return [...acc, { hash: line.commit, count: 1 }]
  }, [])

  const commitMeta: Record<string, BlameLine> = {}
  for (const line of lines) {
    if (!commitMeta[line.commit]) commitMeta[line.commit] = line
  }

  return (
    <div className="flex flex-1 min-h-0 overflow-hidden">
      {/* Left: file picker */}
      <div className="w-[220px] shrink-0 border-r border-black/[0.08] flex flex-col overflow-hidden">
        {loadingFiles ? (
          <div className="flex items-center justify-center py-4 text-gray-600 text-xs gap-1">
            <span className="animate-spin inline-block">↻</span> Laden…
          </div>
        ) : (
          <FilePicker files={files} selected={selectedFile} onSelect={selectFile} />
        )}
      </div>

      {/* Right: blame view */}
      <div className="flex-1 min-w-0 flex flex-col overflow-hidden">
        {!selectedFile ? (
          <div className="flex-1 flex items-center justify-center text-gray-600 text-sm italic">
            Selecteer een bestand
          </div>
        ) : loading ? (
          <div className="flex-1 flex items-center justify-center gap-2 text-gray-600 text-sm">
            <span className="animate-spin inline-block">↻</span> Blame laden…
          </div>
        ) : error ? (
          <div className="m-3 bg-red-100 text-red-600 px-3 py-2 rounded text-xs">{error}</div>
        ) : lines.length === 0 ? (
          <div className="flex-1 flex items-center justify-center text-gray-600 text-sm italic">
            Geen blame data
          </div>
        ) : (
          <div className="flex flex-1 overflow-hidden">
            {/* Commit gutter */}
            <div className="w-[180px] shrink-0 border-r border-black/[0.08] overflow-y-auto select-none">
              {commitBlocks.map((block, i) => {
                const meta = commitMeta[block.hash]
                const isHovered = hoveredCommit === block.hash
                return (
                  <div
                    key={i}
                    style={{ height: block.count * LINE_H }}
                    onMouseEnter={() => setHoveredCommit(block.hash)}
                    onMouseLeave={() => setHoveredCommit(null)}
                    className={`px-2 overflow-hidden border-b border-white/[0.03] transition-colors
                      ${isHovered ? 'bg-indigo-500/10' : ''}`}
                  >
                    <div className="flex items-start gap-1.5 pt-px">
                      <span className="text-[10px] font-mono text-indigo-600 shrink-0 leading-[20px]">
                        {block.hash.slice(0, 7)}
                      </span>
                      {block.count >= 2 && meta && (
                        <span className="text-[10px] text-gray-600 truncate leading-[20px]">
                          {meta.author?.name ?? ''}{block.count >= 3 ? ` · ${timeAgo(String(meta.date ?? ''))}` : ''}
                        </span>
                      )}
                    </div>
                  </div>
                )
              })}
            </div>
            {/* Code */}
            <div className="flex-1 overflow-auto">
              <table className="w-full border-collapse">
                <tbody>
                  {lines.map(line => (
                    <tr
                      key={line.lineNo}
                      onMouseEnter={() => setHoveredCommit(line.commit)}
                      onMouseLeave={() => setHoveredCommit(null)}
                      className={`transition-colors ${hoveredCommit === line.commit ? 'bg-indigo-500/10' : ''}`}
                      style={{ height: LINE_H }}
                    >
                      <td className="text-right text-[11px] text-gray-700 font-mono px-2 select-none w-10 shrink-0 leading-[20px]">
                        {line.lineNo}
                      </td>
                      <td className="text-[11px] text-gray-700 font-mono whitespace-pre pl-1 pr-4 leading-[20px]">
                        {line.content}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
