import { useEffect, useState, useCallback } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { InventoryItem } from '../../bindings/github.com/rdm/sites-tool/internal/services'
import { PackageIcon, PaletteIcon, RefreshIcon, ChevronIcon } from './icons'

interface Props {
  kind: 'plugins' | 'themes'
}

export default function InventoryPage({ kind }: Props) {
  const [items, setItems] = useState<InventoryItem[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [filter, setFilter] = useState('')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())

  const title = kind === 'plugins' ? 'Plugins' : "Thema's"
  const Icon = kind === 'plugins' ? PackageIcon : PaletteIcon

  const load = useCallback(async () => {
    setBusy(true); setError(null)
    try {
      const result = kind === 'plugins'
        ? await Services.InventoryService.Plugins()
        : await Services.InventoryService.Themes()
      setItems(result ?? [])
    } catch (e) {
      setError(String(e))
    } finally {
      setBusy(false)
    }
  }, [kind])

  useEffect(() => { void load() }, [load])

  const toggle = (slug: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      next.has(slug) ? next.delete(slug) : next.add(slug)
      return next
    })
  }

  const q = filter.trim().toLowerCase()
  const shown = (items ?? []).filter(it =>
    !q ||
    it.slug.toLowerCase().includes(q) ||
    it.projects.some(p => p.projectName.toLowerCase().includes(q))
  )
  const outdatedTotal = (items ?? []).reduce((n, it) => n + it.outdatedCount, 0)

  return (
    <div className="flex-1 flex flex-col min-h-0 overflow-hidden bg-bg">
      {/* ── toolbar ── */}
      <div className="h-14 px-[22px] bg-panel border-b border-border shrink-0 flex items-center gap-3">
        <span className="w-8 h-8 flex items-center justify-center rounded-lg bg-hover text-fg-muted shrink-0">
          <Icon size={16} />
        </span>
        <div className="flex-1 min-w-0">
          <h2 className="text-[15px] font-bold tracking-[-.01em] text-fg leading-tight truncate">{title}</h2>
          {items && (
            <p className="text-[11px] text-fg-faint leading-tight truncate">
              {items.length} {kind === 'plugins' ? 'plugins' : "thema's"}
              {outdatedTotal > 0 && <> · <span className="text-amber">{outdatedTotal} verouderd</span></>}
              {' · vergeleken met de default branch per project'}
            </p>
          )}
        </div>
        <input
          type="search"
          placeholder={kind === 'plugins' ? 'Zoek plugin of project…' : 'Zoek thema of project…'}
          value={filter}
          onChange={e => setFilter(e.target.value)}
          className="w-[200px] bg-bg text-[12.5px] text-fg placeholder-fg-faint rounded-lg px-3 py-[6px]
                     outline-none border border-border focus:border-accent focus:ring-1 focus:ring-accent/30"
        />
        <button onClick={() => void load()} disabled={busy}
          className="px-3 py-1.5 bg-accent hover:bg-accent-2 text-white text-[12.5px] font-semibold
                     rounded-lg disabled:opacity-50 transition-colors shrink-0 flex items-center gap-1.5">
          <span className={`inline-flex ${busy ? 'animate-spin' : ''}`}>
            <RefreshIcon size={13} />
          </span>
          {busy ? 'Bezig…' : 'Vernieuwen'}
        </button>
      </div>

      {/* ── list ── */}
      <div className="flex-1 overflow-y-auto px-6 py-4">
        {error && <p className="text-[12.5px] text-red mb-3">{error}</p>}

        {!items && busy && (
          <p className="text-[12.5px] text-fg-faint">Verzamelen uit alle projecten…</p>
        )}
        {items && shown.length === 0 && (
          <p className="text-[12.5px] text-fg-faint">
            {filter ? 'Geen resultaten voor dit filter.' : `Geen ${title.toLowerCase()} gevonden in de projecten.`}
          </p>
        )}

        <div className="space-y-1">
          {shown.map(it => {
            const open = expanded.has(it.slug)
            return (
              <div key={it.slug} className="border border-border rounded-lg overflow-hidden">
                {/* item row */}
                <button
                  onClick={() => toggle(it.slug)}
                  className="w-full flex items-center gap-2.5 px-3 py-2 text-left hover:bg-hover transition-colors"
                >
                  <span className="text-fg-faint shrink-0"><ChevronIcon size={13} open={open} /></span>
                  <span className="font-mono text-[12.5px] text-fg truncate">{it.slug}</span>

                  {it.outdatedCount > 0 && (
                    <span className="text-[10.5px] font-semibold text-amber bg-amber/10 border border-amber/30
                                     rounded-full px-2 py-px shrink-0">
                      {it.outdatedCount} verouderd
                    </span>
                  )}

                  <span className="ml-auto text-[11px] text-fg-faint shrink-0">
                    {it.projects.length} project{it.projects.length !== 1 ? 'en' : ''}
                  </span>
                  <span className="text-[12px] font-mono shrink-0 w-[90px] text-right">
                    {it.latestVersion
                      ? <span className="text-fg">{it.latestVersion}</span>
                      : <span className="text-fg-faint" title="Niet op wp.org — handmatig bijhouden">—</span>}
                  </span>
                </button>

                {/* per-project versions */}
                {open && (
                  <ul className="border-t border-border bg-panel/40">
                    {it.projects.map(p => (
                      <li key={p.projectId + p.version}
                          className="flex items-center gap-2 px-3 py-1.5 pl-9 text-[12px] border-b border-border/40 last:border-b-0">
                        <span className="text-fg truncate">{p.projectName}</span>
                        <span className="font-mono text-[10.5px] text-fg-faint truncate flex-1"
                              title={`Versie gelezen van ${p.ref}`}>
                          ⑂ {p.ref}
                        </span>
                        <span className={`font-mono ${p.outdated ? 'text-amber' : 'text-fg-muted'}`}>
                          {p.version || '?'}
                        </span>
                        {p.outdated && it.latestVersion && (
                          <span className="font-mono text-fg-faint">→ {it.latestVersion}</span>
                        )}
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}
