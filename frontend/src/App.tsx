import { useEffect, useState, useCallback } from 'react'
import * as ProjectService from '../bindings/github.com/rdm/sites-tool/internal/services'
import type { ProjectStatusSummary, Project } from '../bindings/github.com/rdm/sites-tool/internal/domain/models'
import ProjectDetail from './components/ProjectDetail'
import BatchTab from './components/BatchTab'
import SearchPanel from './components/SearchPanel'
import SettingsPage from './components/SettingsPage'
import ErrorBoundary from './components/ErrorBoundary'

// ─── tiny icon button ──────────────────────────────────────────────────────
function IconBtn({ onClick, title, children, drag = false }: {
  onClick: () => void; title: string; children: React.ReactNode; drag?: boolean
}) {
  return (
    <button
      onClick={onClick}
      title={title}
      style={drag ? { WebkitAppRegion: 'no-drag' } as React.CSSProperties : undefined}
      className="w-7 h-7 flex items-center justify-center rounded-md text-fg-muted
                 hover:text-fg hover:bg-hover transition-colors text-sm select-none"
    >
      {children}
    </button>
  )
}

// ─── project row ───────────────────────────────────────────────────────────
function ProjectRow({ s, active, onClick }: {
  s: ProjectStatusSummary; active: boolean; onClick: () => void
}) {
  return (
    <button
      onClick={onClick}
      className={`w-full text-left px-2.5 py-2 rounded-lg flex items-start gap-2 transition-colors
        ${active ? 'bg-sel shadow-[inset_0_0_0_1px_var(--border-strong)]' : 'hover:bg-hover'}`}
    >
      {/* status dot column */}
      <span
        className={`w-1.5 h-1.5 rounded-full shrink-0 mt-[7px] ${s.dirty ? 'bg-amber' : 'bg-transparent'}`}
        title={s.dirty ? 'Uncommitted changes' : undefined}
      />

      {/* name + branch */}
      <div className="flex-1 min-w-0">
        <p className="text-[13px] font-semibold text-fg truncate leading-snug">{s.displayName}</p>
        <p className="text-[11px] truncate leading-snug mt-px font-mono text-fg-faint">
          {s.isRepo ? `⑂ ${s.branch}` : 'no git'}
        </p>
      </div>

      {/* ahead/behind */}
      {s.isRepo && (s.ahead > 0 || s.behind > 0) && (
        <div className="flex items-center gap-1 shrink-0 text-xs pt-0.5 font-mono">
          {s.ahead  > 0 && <span className="text-green">↑{s.ahead}</span>}
          {s.behind > 0 && <span className="text-red">↓{s.behind}</span>}
        </div>
      )}
    </button>
  )
}

// ─── empty state ───────────────────────────────────────────────────────────
function EmptyState({ onAdd }: { onAdd: () => void }) {
  return (
    <div className="flex-1 flex flex-col items-center justify-center gap-3 px-6 text-center py-12">
      <span className="text-3xl opacity-20 select-none">📁</span>
      <p className="text-sm text-fg-muted font-medium">Geen projecten</p>
      <p className="text-xs text-fg-faint">Voeg een folder toe om te beginnen</p>
      <button
        onClick={onAdd}
        className="mt-1 px-4 py-1.5 bg-accent hover:bg-accent-2 text-white text-sm
                   font-semibold rounded-lg transition-colors"
      >
        Folder toevoegen
      </button>
    </div>
  )
}

// ─── root app ──────────────────────────────────────────────────────────────
export default function App() {
  const [summaries, setSummaries] = useState<ProjectStatusSummary[]>([])
  const [selected, setSelected] = useState<Project | null>(null)
  const [scanning, setScanning] = useState(false)
  const [search, setSearch] = useState('')
  const [roots, setRoots] = useState<string[]>([])
  const [showBatch, setShowBatch] = useState(false)
  const [showSearch, setShowSearch] = useState(false)
  const [showSettings, setShowSettings] = useState(false)

  const doScan = useCallback(async (currentRoots: string[]) => {
    if (currentRoots.length === 0) return
    setScanning(true)
    try {
      await ProjectService.ProjectService.Scan()
      const results = await ProjectService.ProjectService.BatchStatus()
      setSummaries(results ?? [])
    } finally {
      setScanning(false)
    }
  }, [])

  const refresh = useCallback(async () => {
    const r = await ProjectService.ProjectService.GetRoots()
    const safeRoots = r ?? []
    setRoots(safeRoots)
    await doScan(safeRoots)
  }, [doScan])

  const addFolder = useCallback(async () => {
    const newRoots = await ProjectService.ProjectService.AddRoot()
    const safeRoots = newRoots ?? []
    setRoots(safeRoots)
    await doScan(safeRoots)
  }, [doScan])

  const removeFolder = useCallback(async (path: string) => {
    const newRoots = await ProjectService.ProjectService.RemoveRoot(path)
    setRoots(newRoots ?? [])
    setSelected(null)
    await doScan(newRoots ?? [])
  }, [doScan])

  const selectProject = useCallback(async (id: string) => {
    const p = await ProjectService.ProjectService.RefreshOne(id)
    setSelected(p)
  }, [])

  useEffect(() => { refresh() }, [refresh])

  const filtered = search
    ? summaries.filter(s =>
        s.displayName.toLowerCase().includes(search.toLowerCase()) ||
        (s.branch ?? '').toLowerCase().includes(search.toLowerCase())
      )
    : summaries

  const homePath = (p: string) => p.replace(/^\/Users\/[^/]+/, '~')

  return (
    <div className="flex w-full h-screen overflow-hidden bg-bg text-fg font-sans">

      {/* ── Sidebar ── */}
      <div className="w-[262px] shrink-0 flex flex-col bg-sidebar border-r border-border">

        {/* title bar — draggable */}
        <div
          className="h-[52px] flex items-center px-3 gap-1 shrink-0 pl-[76px] border-b border-border"
          style={{ WebkitAppRegion: 'drag' } as React.CSSProperties}
        >
          <span
            className="flex-1 text-[13px] font-bold tracking-[-.01em] text-fg pl-1"
            style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}
          >
            RDM Sites
          </span>
          <IconBtn onClick={refresh} title="Opnieuw scannen" drag>
            {scanning ? (
              <span className="animate-spin inline-block">↻</span>
            ) : '↻'}
          </IconBtn>
          <IconBtn onClick={() => { setShowSearch(s => !s); setShowBatch(false) }} title="Zoeken in alle repos" drag>
            ⌕
          </IconBtn>
          <IconBtn onClick={() => { setShowBatch(b => !b); setShowSearch(false) }} title="Batch operaties" drag>
            ⊞
          </IconBtn>
          <IconBtn onClick={addFolder} title="Folder toevoegen" drag>
            +
          </IconBtn>
        </div>

        {/* configured roots */}
        <div className="px-3.5 pt-3.5 pb-2">
          {roots.map(r => (
            <div key={r} className="flex items-center gap-1 group">
              <span
                className="text-[10px] font-semibold tracking-[.08em] text-fg-faint uppercase truncate flex-1"
                title={r}
              >
                {homePath(r)}
              </span>
              <button
                onClick={() => removeFolder(r)}
                className="text-[10px] text-transparent group-hover:text-fg-muted
                           hover:!text-red transition-colors shrink-0"
                title="Verwijder folder"
              >✕</button>
            </div>
          ))}
          <button
            onClick={addFolder}
            className="mt-1 text-xs font-medium text-accent hover:text-accent-2 transition-colors whitespace-nowrap"
          >
            + Folder toevoegen
          </button>
        </div>

        {/* search */}
        {summaries.length > 0 && (
          <div className="px-3 pb-2.5">
            <input
              type="search"
              placeholder="Zoeken…"
              value={search}
              onChange={e => setSearch(e.target.value)}
              className="w-full bg-panel text-[12.5px] text-fg placeholder-fg-faint
                         rounded-lg px-3 py-[7px] outline-none border border-border
                         focus:border-accent focus:ring-1 focus:ring-accent/30"
            />
          </div>
        )}

        {/* project list */}
        <div className="flex-1 overflow-y-auto px-2 space-y-0.5 py-0.5">
          {roots.length === 0 ? (
            <EmptyState onAdd={addFolder} />
          ) : scanning && summaries.length === 0 ? (
            <div className="flex items-center justify-center py-12 gap-2 text-fg-muted text-sm">
              <span className="animate-spin">↻</span> Scannen…
            </div>
          ) : filtered.length === 0 ? (
            <p className="text-xs text-fg-faint text-center py-8">
              {search ? 'Geen resultaten' : 'Geen projecten gevonden'}
            </p>
          ) : (
            filtered.map(s => (
              <ProjectRow
                key={s.projectId}
                s={s}
                active={selected?.id === s.projectId}
                onClick={() => selectProject(s.projectId)}
              />
            ))
          )}
        </div>

        {/* footer */}
        <div className="h-[38px] px-3.5 border-t border-border shrink-0 flex items-center gap-2">
          <p className="text-[11px] text-fg-faint flex-1">
            {summaries.length > 0
              ? `${summaries.length} project${summaries.length !== 1 ? 'en' : ''}${search && filtered.length !== summaries.length ? ` · ${filtered.length} zichtbaar` : ''}`
              : ''}
          </p>
          <button
            onClick={() => { setShowSettings(s => !s); setShowSearch(false); setShowBatch(false) }}
            title="Instellingen"
            className={`w-7 h-7 flex items-center justify-center rounded-md transition-colors text-sm
              ${showSettings ? 'bg-hover text-fg' : 'text-fg-muted hover:text-fg hover:bg-hover'}`}
          >
            ⚙
          </button>
        </div>
      </div>

      {/* ── Detail panel ── */}
      {showSettings ? (
        <ErrorBoundary label="Settings error">
          <SettingsPage onClose={() => setShowSettings(false)} />
        </ErrorBoundary>
      ) : showSearch ? (
        <div className="flex-1 flex flex-col min-h-0 overflow-hidden">
          <div className="h-[56px] px-[22px] border-b border-border bg-panel shrink-0 flex items-center gap-2">
            <h2 className="text-[15px] font-bold tracking-[-.01em] text-fg flex-1">Zoeken</h2>
            <button
              onClick={() => setShowSearch(false)}
              className="text-fg-muted hover:text-fg text-sm transition-colors"
              title="Sluiten"
            >✕</button>
          </div>
          <SearchPanel onSelectProject={(id) => {
            setShowSearch(false)
            selectProject(id)
          }} />
        </div>
      ) : showBatch ? (
        <div className="flex-1 flex flex-col min-h-0 overflow-hidden">
          <div className="h-[56px] px-[22px] border-b border-border bg-panel shrink-0 flex items-center gap-2">
            <h2 className="text-[15px] font-bold tracking-[-.01em] text-fg flex-1">Batch Operaties</h2>
            <button
              onClick={() => setShowBatch(false)}
              className="text-fg-muted hover:text-fg text-sm transition-colors"
              title="Sluiten"
            >✕</button>
          </div>
          <BatchTab />
        </div>
      ) : selected ? (
        <ErrorBoundary key={selected.id} label="Project detail error">
          <ProjectDetail project={selected} onRefresh={refresh} />
        </ErrorBoundary>
      ) : (
        <div className="flex-1 flex items-center justify-center text-fg-faint text-sm select-none">
          Selecteer een project
        </div>
      )}
    </div>
  )
}
