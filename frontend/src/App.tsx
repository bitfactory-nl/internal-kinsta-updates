import { useEffect, useState, useCallback, useSyncExternalStore } from 'react'
import * as ProjectService from '../bindings/github.com/rdm/sites-tool/internal/services'
import type { ProjectStatusSummary, Project } from '../bindings/github.com/rdm/sites-tool/internal/domain/models'
import ProjectDetail from './components/ProjectDetail'
import BatchTab from './components/BatchTab'
import SearchPanel from './components/SearchPanel'
import SettingsPage from './components/SettingsPage'
import WordfencePage from './components/WordfencePage'
import InventoryPage from './components/InventoryPage'
import WordPressPage from './components/WordPressPage'
import ErrorBoundary from './components/ErrorBoundary'
import {
  RefreshIcon, SearchIcon, GridIcon, ShieldIcon, PlusIcon, GearIcon, FolderIcon,
  PackageIcon, PaletteIcon, GlobeIcon, MonitorIcon, SunIcon, MoonIcon,
} from './components/icons'
import { type ThemeMode, getThemeMode, setThemeMode, subscribeTheme } from './lib/thema'

type View = 'projects' | 'search' | 'batch' | 'cve' | 'plugins' | 'wordpress' | 'themes' | 'settings'

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

// ─── thema-schakelaar ──────────────────────────────────────────────────────
const THEMES: { mode: ThemeMode; label: string; icon: React.ReactNode }[] = [
  { mode: 'system', label: 'Systeem', icon: <MonitorIcon size={14} /> },
  { mode: 'light', label: 'Licht', icon: <SunIcon size={14} /> },
  { mode: 'dark', label: 'Donker', icon: <MoonIcon size={14} /> },
]

function ThemeSwitcher() {
  const mode = useSyncExternalStore(subscribeTheme, getThemeMode)
  return (
    <div
      role="radiogroup"
      aria-label="Thema"
      className="flex items-center gap-0.5 p-0.5 rounded-control bg-black/25"
    >
      {THEMES.map(t => (
        <button
          key={t.mode}
          role="radio"
          aria-checked={mode === t.mode}
          onClick={() => setThemeMode(t.mode)}
          title={t.label}
          className={`flex-1 h-7 flex items-center justify-center rounded-[7px]
            transition-colors select-none
            ${mode === t.mode
              ? 'bg-rail-2 text-white'
              : 'text-rail-muted hover:text-rail-fg'}`}
        >
          {t.icon}
        </button>
      ))}
    </div>
  )
}

// ─── nav menu item ─────────────────────────────────────────────────────────
// Staat op de donkerbruine rail: actief = koraal balkje links (GrowthScan).
function NavItem({ icon, label, active, onClick }: {
  icon: React.ReactNode; label: string; active: boolean; onClick: () => void
}) {
  return (
    <button
      onClick={onClick}
      className={`w-full flex items-center gap-3 h-10 px-3 rounded-nav text-[14px]
        font-medium transition-colors select-none border-l-2
        ${active
          ? 'border-accent-2 bg-rail-hover text-white'
          : 'border-transparent text-rail-fg/75 hover:text-rail-fg hover:bg-rail-hover'}`}
    >
      <span className="shrink-0 flex items-center">{icon}</span>
      <span className="truncate">{label}</span>
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
      <span className="text-fg-faint opacity-60 select-none"><FolderIcon size={28} /></span>
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
  const [typeFilter, setTypeFilter] = useState<string | null>(null)
  const [roots, setRoots] = useState<string[]>([])
  const [view, setView] = useState<View>('projects')

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

  // distinct deploy types (from deploy_conf.json), with per-type counts
  const typeCounts = summaries.reduce<Record<string, number>>((acc, s) => {
    const t = s.deployType || 'overig'
    acc[t] = (acc[t] ?? 0) + 1
    return acc
  }, {})
  const deployTypes = Object.keys(typeCounts).sort()

  const filtered = summaries.filter(s => {
    if (typeFilter && (s.deployType || 'overig') !== typeFilter) return false
    if (!search) return true
    return (
      s.displayName.toLowerCase().includes(search.toLowerCase()) ||
      (s.branch ?? '').toLowerCase().includes(search.toLowerCase())
    )
  })

  const homePath = (p: string) => p.replace(/^\/Users\/[^/]+/, '~')

  return (
    <div className="flex w-full h-screen overflow-hidden bg-bg text-fg font-sans">

      {/* ── Nav rail — donkerbruin in beide thema's ── */}
      <div className="w-[224px] shrink-0 flex flex-col bg-rail text-rail-fg">

        {/* title bar — draggable; de stoplichten lopen tot x=80, dus 88px marge */}
        <div
          className="h-16 flex items-center pl-[88px] pr-2 shrink-0 border-b border-rail-border"
          style={{ WebkitAppRegion: 'drag' } as React.CSSProperties}
        >
          <span className="font-display text-[15px] font-extrabold tracking-[-.02em]
                           text-white whitespace-nowrap">
            Kinsta Updater
          </span>
        </div>

        {/* menu — px-2 hier; de knoppen hebben zelf al px-3 */}
        <nav className="px-2 pt-3.5 space-y-0.5">
          <NavItem
            icon={<FolderIcon size={15} />}
            label="Projecten"
            active={view === 'projects'}
            onClick={() => setView('projects')}
          />
          <NavItem
            icon={<SearchIcon size={15} />}
            label="Zoeken"
            active={view === 'search'}
            onClick={() => setView('search')}
          />
          <NavItem
            icon={<GridIcon size={15} />}
            label="Batch operaties"
            active={view === 'batch'}
            onClick={() => setView('batch')}
          />
          <NavItem
            icon={<ShieldIcon size={15} />}
            label="CVE kwetsbaarheden"
            active={view === 'cve'}
            onClick={() => setView('cve')}
          />
          <NavItem
            icon={<PackageIcon size={15} />}
            label="Plugins"
            active={view === 'plugins'}
            onClick={() => setView('plugins')}
          />
          <NavItem
            icon={<PaletteIcon size={15} />}
            label="Thema's"
            active={view === 'themes'}
            onClick={() => setView('themes')}
          />
          <NavItem
            icon={<GlobeIcon size={15} />}
            label="WordPress"
            active={view === 'wordpress'}
            onClick={() => setView('wordpress')}
          />
        </nav>

        <div className="flex-1" />

        {/* footer: settings + thema */}
        <div className="px-2 py-3 border-t border-rail-border space-y-2">
          <NavItem
            icon={<GearIcon size={15} />}
            label="Instellingen"
            active={view === 'settings'}
            onClick={() => setView('settings')}
          />
          <ThemeSwitcher />
        </div>
      </div>

      {/* ── Sub-sidebar: projects ── */}
      {view === 'projects' && (
        <div className="w-[262px] shrink-0 flex flex-col bg-sidebar border-r border-border">

          {/* header — draggable */}
          <div
            className="h-16 flex items-center px-3.5 gap-1 shrink-0 border-b border-border bg-panel"
            style={{ WebkitAppRegion: 'drag' } as React.CSSProperties}
          >
            <span className="flex-1 font-display text-[17px] font-bold tracking-[-.02em] text-fg">
              Projecten
            </span>
            <IconBtn onClick={refresh} title="Opnieuw scannen" drag>
              <span className={`inline-flex ${scanning ? 'animate-spin' : ''}`}>
                <RefreshIcon size={15} />
              </span>
            </IconBtn>
            <IconBtn onClick={addFolder} title="Folder toevoegen" drag>
              <PlusIcon size={15} />
            </IconBtn>
          </div>

          {/* configured roots */}
          <div className="px-3.5 pt-3 pb-2">
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
          </div>

          {/* filter */}
          {summaries.length > 0 && (
            <div className="px-3 pb-2.5">
              <input
                type="search"
                placeholder="Filter projecten…"
                value={search}
                onChange={e => setSearch(e.target.value)}
                className="w-full bg-panel text-[12.5px] text-fg placeholder-fg-faint
                           rounded-lg px-3 py-[7px] outline-none border border-border
                           focus:border-accent focus:ring-1 focus:ring-accent/30"
              />
            </div>
          )}

          {/* type filter chips */}
          {deployTypes.length > 1 && (
            <div className="px-3 pb-2.5 flex flex-wrap gap-1">
              <button
                onClick={() => setTypeFilter(null)}
                className={`px-2 py-[3px] rounded-full text-[10.5px] font-medium border transition-colors
                  ${typeFilter === null
                    ? 'bg-accent-soft border-accent/40 text-accent'
                    : 'bg-panel border-border text-fg-muted hover:text-fg hover:bg-hover'}`}
              >
                Alle ({summaries.length})
              </button>
              {deployTypes.map(t => (
                <button
                  key={t}
                  onClick={() => setTypeFilter(prev => prev === t ? null : t)}
                  title={`Filter op type ${t}`}
                  className={`px-2 py-[3px] rounded-full text-[10.5px] font-medium border transition-colors font-mono
                    ${typeFilter === t
                      ? 'bg-accent-soft border-accent/40 text-accent'
                      : 'bg-panel border-border text-fg-muted hover:text-fg hover:bg-hover'}`}
                >
                  {t} ({typeCounts[t]})
                </button>
              ))}
            </div>
          )}

          {/* project list */}
          <div className="flex-1 overflow-y-auto px-2 space-y-0.5 py-0.5">
            {roots.length === 0 ? (
              <EmptyState onAdd={addFolder} />
            ) : scanning && summaries.length === 0 ? (
              <div className="flex items-center justify-center py-12 gap-2 text-fg-muted text-sm">
                <span className="animate-spin inline-flex"><RefreshIcon size={14} /></span> Scannen…
              </div>
            ) : filtered.length === 0 ? (
              <p className="text-xs text-fg-faint text-center py-8">
                {search || typeFilter ? 'Geen resultaten' : 'Geen projecten gevonden'}
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
          <div className="h-[38px] px-3.5 border-t border-border shrink-0 flex items-center">
            <p className="text-[11px] text-fg-faint flex-1">
              {summaries.length > 0
                ? `${summaries.length} project${summaries.length !== 1 ? 'en' : ''}${(search || typeFilter) && filtered.length !== summaries.length ? ` · ${filtered.length} zichtbaar` : ''}`
                : ''}
            </p>
          </div>
        </div>
      )}

      {/* ── Detail panel ── */}
      {view === 'settings' ? (
        <ErrorBoundary label="Settings error">
          <SettingsPage onClose={() => setView('projects')} />
        </ErrorBoundary>
      ) : view === 'search' ? (
        <div className="flex-1 flex flex-col min-h-0 overflow-hidden">
          <div className="h-[56px] px-[22px] border-b border-border bg-panel shrink-0 flex items-center gap-2">
            <h2 className="text-[15px] font-bold tracking-[-.01em] text-fg flex-1">Zoeken</h2>
          </div>
          <SearchPanel onSelectProject={(id) => {
            setView('projects')
            selectProject(id)
          }} />
        </div>
      ) : view === 'batch' ? (
        <div className="flex-1 flex flex-col min-h-0 overflow-hidden">
          <div className="h-[56px] px-[22px] border-b border-border bg-panel shrink-0 flex items-center gap-2">
            <h2 className="text-[15px] font-bold tracking-[-.01em] text-fg flex-1">Batch Operaties</h2>
          </div>
          <BatchTab />
        </div>
      ) : view === 'cve' ? (
        <ErrorBoundary label="CVE error">
          <WordfencePage onClose={() => setView('projects')} />
        </ErrorBoundary>
      ) : view === 'plugins' ? (
        <ErrorBoundary label="Plugins error">
          <InventoryPage key="plugins" kind="plugins" />
        </ErrorBoundary>
      ) : view === 'themes' ? (
        <ErrorBoundary label="Thema's error">
          <InventoryPage key="themes" kind="themes" />
        </ErrorBoundary>
      ) : view === 'wordpress' ? (
        <ErrorBoundary label="WordPress error">
          <WordPressPage />
        </ErrorBoundary>
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
