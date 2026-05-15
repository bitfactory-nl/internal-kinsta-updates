import { useMemo } from 'react'
import type { GraphCommit } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'

export interface CommitGraphProps {
  commits: GraphCommit[]
  selectedHash: string | null
  onSelect: (hash: string) => void
}

const LANE_W = 14
const ROW_H = 32
const R = 4

const LANE_COLORS = [
  '#6366f1', '#10b981', '#f59e0b', '#ef4444',
  '#8b5cf6', '#06b6d4', '#ec4899', '#84cc16',
]

function laneColor(lane: number): string {
  return LANE_COLORS[lane % LANE_COLORS.length]
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

function RefPill({ label: refName }: { label: string }) {
  let cls = 'text-gray-600 ring-gray-400/50'
  if (refName === 'HEAD') {
    cls = 'text-yellow-700 ring-yellow-400/40'
  } else if (refName.startsWith('tag:')) {
    cls = 'text-emerald-700 ring-emerald-500/30'
  } else if (refName.startsWith('origin/') || refName.includes('remote')) {
    cls = 'text-gray-600 ring-gray-400/50'
  } else {
    cls = 'text-indigo-700 ring-indigo-500/30'
  }

  const label = refName.startsWith('tag:') ? refName.slice(4) : refName

  return (
    <span className={`text-[10px] px-1.5 py-px rounded ring-1 ${cls} font-mono shrink-0`}>
      {label}
    </span>
  )
}

interface CommitRowProps {
  commit: GraphCommit
  prevEdges: GraphCommit['edges']
  prevColumns: number
  selected: boolean
  onSelect: (hash: string) => void
}

function CommitRow({ commit, prevEdges, prevColumns, selected, onSelect }: CommitRowProps) {
  const totalColumns = Math.max(commit.columns ?? 1, prevColumns, commit.lane + 1)
  const svgWidth = totalColumns * LANE_W + 8

  return (
    <button
      onClick={() => onSelect(commit.hash)}
      className={`w-full flex items-center gap-0 min-h-[${ROW_H}px] text-left transition-colors
        ${selected ? 'bg-indigo-100' : 'hover:bg-black/[0.04]'}`}
      style={{ minHeight: ROW_H }}
    >
      {/* SVG graph strip */}
      <div className="shrink-0" style={{ width: svgWidth }}>
        <svg
          width={svgWidth}
          height={ROW_H}
          viewBox={`0 0 ${svgWidth} ${ROW_H}`}
          className="overflow-visible"
        >
          {/* Incoming lines from prev commit's edges */}
          {(prevEdges ?? []).map((edge, i) => {
            const x1 = edge.fromLane * LANE_W + LANE_W / 2
            const y1 = 0
            const x2 = edge.toLane * LANE_W + LANE_W / 2
            const y2 = ROW_H / 2
            const color = laneColor(edge.colorLane ?? edge.fromLane)
            return (
              <line
                key={`in-${i}`}
                x1={x1} y1={y1}
                x2={x2} y2={y2}
                stroke={color}
                strokeWidth={1.5}
                strokeLinecap="round"
              />
            )
          })}

          {/* Outgoing lines from this commit's edges */}
          {(commit.edges ?? []).map((edge, i) => {
            const x1 = edge.fromLane * LANE_W + LANE_W / 2
            const y1 = ROW_H / 2
            const x2 = edge.toLane * LANE_W + LANE_W / 2
            const y2 = ROW_H
            const color = laneColor(edge.colorLane ?? edge.fromLane)
            return (
              <line
                key={`out-${i}`}
                x1={x1} y1={y1}
                x2={x2} y2={y2}
                stroke={color}
                strokeWidth={1.5}
                strokeLinecap="round"
              />
            )
          })}

          {/* Commit circle */}
          <circle
            cx={commit.lane * LANE_W + LANE_W / 2}
            cy={ROW_H / 2}
            r={R}
            fill={laneColor(commit.lane)}
            stroke="rgba(255,255,255,0.2)"
            strokeWidth={1}
          />
        </svg>
      </div>

      {/* Commit info */}
      <div className="flex-1 min-w-0 px-2 py-1">
        {/* Subject + refs */}
        <div className="flex items-center gap-1.5 min-w-0">
          <span className={`text-xs truncate flex-1 ${selected ? 'text-gray-900' : 'text-gray-800'}`}>
            {commit.subject}
          </span>
          {(commit.refs ?? []).length > 0 && (
            <div className="flex items-center gap-1 shrink-0 flex-wrap justify-end max-w-[140px]">
              {commit.refs.slice(0, 3).map((r, i) => (
                <RefPill key={i} label={r} />
              ))}
            </div>
          )}
        </div>

        {/* Author + time */}
        <div className="flex items-center gap-1.5 mt-0.5">
          <span className="text-[11px] text-gray-600 truncate">{commit.author}</span>
          <span className="text-[11px] text-gray-600 shrink-0">{timeAgo(commit.authorDate)}</span>
          <span className="text-[10px] text-gray-700 font-mono shrink-0 ml-auto">{commit.shortHash}</span>
        </div>
      </div>
    </button>
  )
}

export default function CommitGraph({ commits, selectedHash, onSelect }: CommitGraphProps) {
  const rows = useMemo(() => {
    return commits.map((commit, idx) => ({
      commit,
      prevEdges: idx > 0 ? commits[idx - 1].edges ?? [] : [],
      prevColumns: idx > 0 ? commits[idx - 1].columns ?? 1 : 1,
    }))
  }, [commits])

  if (commits.length === 0) {
    return (
      <div className="flex items-center justify-center py-8 text-gray-600 text-sm italic">
        No commits
      </div>
    )
  }

  return (
    <div className="flex flex-col divide-y divide-black/[0.06]">
      {rows.map(({ commit, prevEdges, prevColumns }) => (
        <CommitRow
          key={commit.hash}
          commit={commit}
          prevEdges={prevEdges}
          prevColumns={prevColumns}
          selected={selectedHash === commit.hash}
          onSelect={onSelect}
        />
      ))}
    </div>
  )
}
