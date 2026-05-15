import { useState } from 'react'
import type { FileDiff } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'

export interface DiffViewerProps {
  diffs: FileDiff[] | null
  loading?: boolean
}

function countChanges(diff: FileDiff): { additions: number; deletions: number } {
  let additions = 0
  let deletions = 0
  for (const hunk of diff.hunks ?? []) {
    for (const line of hunk.lines ?? []) {
      if (line.kind === 'add') additions++
      else if (line.kind === 'del') deletions++
    }
  }
  return { additions, deletions }
}

function FileDiffBlock({ diff }: { diff: FileDiff }) {
  const [collapsed, setCollapsed] = useState(false)
  const { additions, deletions } = countChanges(diff)
  const displayPath = diff.oldPath && diff.oldPath !== diff.path
    ? `${diff.oldPath} → ${diff.path}`
    : diff.path

  return (
    <div className="border border-black/[0.08] rounded-lg overflow-hidden">
      {/* File header */}
      <button
        onClick={() => setCollapsed(c => !c)}
        className="w-full flex items-center gap-2 px-3 py-2 bg-black/[0.04] hover:bg-black/[0.07]
                   transition-colors text-left"
      >
        <span className="text-gray-600 text-xs select-none w-3 shrink-0">
          {collapsed ? '▶' : '▼'}
        </span>
        <span className="font-mono text-xs text-gray-800 flex-1 truncate">{displayPath}</span>
        {!diff.binary && (
          <span className="text-[11px] font-mono shrink-0 flex items-center gap-1.5">
            {additions > 0 && (
              <span className="text-emerald-600">+{additions}</span>
            )}
            {deletions > 0 && (
              <span className="text-red-600">-{deletions}</span>
            )}
          </span>
        )}
      </button>

      {/* Diff content */}
      {!collapsed && (
        <div className="overflow-x-auto">
          {diff.binary ? (
            <div className="px-4 py-3 text-xs text-gray-600 font-mono italic">
              Binary file — not shown
            </div>
          ) : (diff.hunks ?? []).length === 0 ? (
            <div className="px-4 py-3 text-xs text-gray-600 italic">
              No changes
            </div>
          ) : (
            <table className="w-full border-collapse text-xs font-mono">
              <tbody>
                {(diff.hunks ?? []).map((hunk, hi) => (
                  <>
                    {/* Hunk header */}
                    <tr key={`hunk-${hi}`} className="bg-black/[0.03]">
                      <td className="w-8 text-right text-gray-600 text-xs font-mono select-none pr-2 shrink-0 py-0.5" />
                      <td className="w-8 text-right text-gray-600 text-xs font-mono select-none pr-2 shrink-0 py-0.5" />
                      <td className="pl-2 text-gray-600 py-0.5 whitespace-pre">{hunk.header}</td>
                    </tr>
                    {/* Diff lines */}
                    {(hunk.lines ?? []).map((line, li) => {
                      const isAdd = line.kind === 'add'
                      const isDel = line.kind === 'del'
                      const rowClass = isAdd
                        ? 'bg-emerald-100'
                        : isDel
                        ? 'bg-red-100'
                        : ''
                      const textClass = isAdd
                        ? 'text-emerald-800'
                        : isDel
                        ? 'text-red-700'
                        : 'text-gray-600'
                      const prefix = isAdd ? '+' : isDel ? '-' : ' '

                      return (
                        <tr key={`line-${hi}-${li}`} className={rowClass}>
                          <td className="w-8 text-right text-gray-600 text-xs font-mono select-none pr-2 shrink-0 align-top py-px">
                            {isDel || line.kind === 'context' ? line.oldNum || '' : ''}
                          </td>
                          <td className="w-8 text-right text-gray-600 text-xs font-mono select-none pr-2 shrink-0 align-top py-px">
                            {isAdd || line.kind === 'context' ? line.newNum || '' : ''}
                          </td>
                          <td className={`font-mono text-xs pl-2 whitespace-pre overflow-x-auto py-px ${textClass}`}>
                            {prefix}{line.content}
                          </td>
                        </tr>
                      )
                    })}
                  </>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}
    </div>
  )
}

export default function DiffViewer({ diffs, loading }: DiffViewerProps) {
  if (loading) {
    return (
      <div className="flex items-center justify-center py-8 gap-2 text-gray-600 text-sm">
        <span className="animate-spin inline-block">↻</span>
        <span>Loading diff…</span>
      </div>
    )
  }

  if (!diffs || diffs.length === 0) {
    return (
      <div className="flex items-center justify-center py-8 text-gray-600 text-sm italic">
        No changes
      </div>
    )
  }

  return (
    <div className="space-y-2">
      {diffs.map((diff, i) => (
        <FileDiffBlock key={`${diff.path}-${i}`} diff={diff} />
      ))}
    </div>
  )
}
