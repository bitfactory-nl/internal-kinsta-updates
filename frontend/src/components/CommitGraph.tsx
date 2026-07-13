import { useMemo } from 'react'
import type { GraphCommit } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'

export interface CommitGraphProps {
  commits: GraphCommit[]
  selectedHash: string | null
  onSelect: (hash: string) => void
}

const LANE_W = 16
const ROW_H = 52
const R = 5

const LANE_COLORS = [
  'var(--accent)',
  'var(--green)',
  'var(--orange)',
  'var(--purple)',
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
  const label = refName.startsWith('tag:') ? refName.slice(4) : refName

  if (refName === 'HEAD') {
    return (
      <span className="font-mono text-[9.5px] font-semibold text-green bg-green-soft px-1.5 py-[2px] rounded-[5px] shrink-0">
        {label}
      </span>
    )
  }

  return (
    <span className="font-mono text-[9.5px] font-medium text-fg-muted bg-panel-2 border border-border px-1.5 py-[2px] rounded-[5px] shrink-0">
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
      className={`w-full flex items-center gap-0 text-left transition-colors border-b border-border
        ${selected ? 'bg-sel' : 'hover:bg-hover'}`}
      style={{ minHeight: ROW_H }}
    >
      {/* SVG graph strip */}
      <div className="shrink-0 pl-2" style={{ width: svgWidth + 8 }}>
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
            return (
              <line
                key={`in-${i}`}
                x1={x1} y1={y1}
                x2={x2} y2={y2}
                stroke="var(--border)"
                strokeWidth={2}
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
            return (
              <line
                key={`out-${i}`}
                x1={x1} y1={y1}
                x2={x2} y2={y2}
                stroke="var(--border)"
                strokeWidth={2}
                strokeLinecap="round"
              />
            )
          })}

          {/* Commit dot */}
          <circle
            cx={commit.lane * LANE_W + LANE_W / 2}
            cy={ROW_H / 2}
            r={R}
            fill={laneColor(commit.lane)}
            stroke="var(--panel)"
            strokeWidth={3}
          />
        </svg>
      </div>

      {/* Commit info */}
      <div className="flex-1 min-w-0 px-2.5 py-2">
        {/* Subject + refs */}
        <div className="flex items-center gap-[7px] min-w-0">
          <span className="text-[13px] font-medium text-fg truncate flex-1">
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

        {/* Author + time + hash */}
        <div className="flex items-center gap-2.5 mt-[3px] text-[11.5px] font-[450] text-fg-faint">
          <span className="truncate">{commit.author}</span>
          <span>·</span>
          <span className="shrink-0">{timeAgo(commit.authorDate)}</span>
          <span className="font-mono text-[11px] font-medium shrink-0 ml-auto pr-2">{commit.shortHash}</span>
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
      <div className="flex items-center justify-center py-8 text-fg-faint text-[13px] italic">
        No commits
      </div>
    )
  }

  return (
    <div className="flex flex-col">
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
