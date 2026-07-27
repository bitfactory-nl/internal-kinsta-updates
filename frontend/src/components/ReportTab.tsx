import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import { Report } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'

interface Props {
  projectId: string
}

const QUARTERS = ['Q1', 'Q2', 'Q3', 'Q4'] as const

function currentPeriod(): { quarter: string; year: number } {
  const now = new Date()
  const quarter = QUARTERS[Math.floor(now.getMonth() / 3)]
  return { quarter, year: now.getFullYear() }
}

function yearOptions(current: number): number[] {
  const years: number[] = []
  for (let y = current - 3; y <= current + 1; y++) years.push(y)
  return years
}

// ---- Generic editable table -------------------------------------------------

interface ColumnConfig<T> {
  key: keyof T
  label: string
  width?: string
}

interface EditableTableProps<T extends Record<string, unknown>> {
  rows: T[]
  columns: ColumnConfig<T>[]
  empty: T
  onChange: (rows: T[]) => void
}

function EditableTable<T extends Record<string, unknown>>({ rows, columns, empty, onChange }: EditableTableProps<T>) {
  const updateCell = (idx: number, key: keyof T, value: string) => {
    const next = rows.map((row, i) => (i === idx ? { ...row, [key]: value } : row))
    onChange(next)
  }

  const removeRow = (idx: number) => {
    onChange(rows.filter((_, i) => i !== idx))
  }

  const addRow = () => {
    onChange([...rows, { ...empty }])
  }

  return (
    <div>
      <div className="overflow-x-auto rounded-[9px] border border-border">
        <table className="w-full border-collapse text-[12.5px]">
          <thead>
            <tr className="bg-panel-2">
              {columns.map(col => (
                <th
                  key={String(col.key)}
                  style={col.width ? { width: col.width } : undefined}
                  className="text-left text-[10.5px] font-semibold tracking-[.04em] text-fg-faint uppercase px-2.5 py-2 border-b border-border"
                >
                  {col.label}
                </th>
              ))}
              <th className="w-8 border-b border-border" />
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={columns.length + 1} className="px-2.5 py-3 text-fg-faint italic text-[12px] text-center">
                  geen rijen
                </td>
              </tr>
            )}
            {rows.map((row, idx) => (
              <tr key={idx} className="border-b border-border last:border-b-0 hover:bg-hover/40">
                {columns.map(col => (
                  <td key={String(col.key)} className="px-1 py-1 align-top">
                    <input
                      value={(row[col.key] as string) ?? ''}
                      onChange={e => updateCell(idx, col.key, e.target.value)}
                      className="w-full bg-transparent px-1.5 py-1 text-[12.5px] text-fg outline-none rounded-[6px] focus:bg-panel-2 focus:ring-1 focus:ring-accent/30"
                    />
                  </td>
                ))}
                <td className="px-1 py-1 text-center align-top">
                  <button
                    onClick={() => removeRow(idx)}
                    title="Verwijder rij"
                    className="text-fg-faint hover:text-red text-[12px] px-1 transition-colors"
                  >
                    ✕
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <button
        onClick={addRow}
        className="mt-2 text-[11.5px] text-fg-muted border border-border rounded-[7px] px-2.5 py-1 hover:bg-hover transition-colors"
      >
        + rij
      </button>
    </div>
  )
}

function SectionCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="bg-panel border border-border rounded-[11px] p-4 mb-4">
      <div className="text-[10.5px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">
        {title}
      </div>
      {children}
    </div>
  )
}

// ---- Main tab ---------------------------------------------------------------

export default function ReportTab({ projectId }: Props) {
  const initialPeriod = useMemo(currentPeriod, [])
  const [quarter, setQuarter] = useState<string>(initialPeriod.quarter)
  const [year, setYear] = useState<number>(initialPeriod.year)

  const [report, setReport] = useState<Report | null>(null)
  const [savedSnapshot, setSavedSnapshot] = useState<string>('')
  const [loading, setLoading] = useState(false)
  const [prefilling, setPrefilling] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [exporting, setExporting] = useState(false)
  const [exportedPath, setExportedPath] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const period = `${quarter} ${year}`
  const reportKey = `${projectId}::${period}`

  // Kept in sync every render so async callbacks can detect a project/period
  // switch that happened after they were kicked off, and bail out instead of
  // clobbering the now-current selection's state with a stale response.
  const keyRef = useRef(reportKey)
  keyRef.current = reportKey

  const savedTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const load = useCallback(() => {
    const key = reportKey
    setLoading(true)
    setError(null)
    setExportedPath(null)
    Services.ReportService.GetReport(projectId, period)
      .then(r => {
        if (keyRef.current !== key) return
        setReport(r)
        setSavedSnapshot(JSON.stringify(r))
      })
      .catch(e => {
        if (keyRef.current !== key) return
        setError(e instanceof Error ? e.message : String(e))
      })
      .finally(() => {
        if (keyRef.current !== key) return
        setLoading(false)
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId, period, reportKey])

  useEffect(() => {
    if (savedTimerRef.current) {
      clearTimeout(savedTimerRef.current)
      savedTimerRef.current = null
    }
    setSaved(false)
    load()
  }, [load])

  useEffect(() => {
    return () => {
      if (savedTimerRef.current) clearTimeout(savedTimerRef.current)
    }
  }, [])

  const dirty = report !== null && JSON.stringify(report) !== savedSnapshot

  const doPrefill = async () => {
    const key = reportKey
    setPrefilling(true)
    setError(null)
    try {
      const enriched = await Services.ReportService.Prefill(projectId, period)
      if (keyRef.current !== key) return
      setReport(enriched)
    } catch (e) {
      if (keyRef.current === key) setError(e instanceof Error ? e.message : String(e))
    } finally {
      setPrefilling(false)
    }
  }

  const doSave = async () => {
    if (!report) return
    const key = reportKey
    setSaving(true)
    setError(null)
    setSaved(false)
    try {
      await Services.ReportService.SaveReport(report)
      if (keyRef.current !== key) return
      setSavedSnapshot(JSON.stringify(report))
      setSaved(true)
      savedTimerRef.current = setTimeout(() => {
        savedTimerRef.current = null
        if (keyRef.current === key) setSaved(false)
      }, 2000)
    } catch (e) {
      if (keyRef.current === key) setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  const doExport = async () => {
    const key = reportKey
    setExporting(true)
    setError(null)
    setExportedPath(null)
    try {
      if (report) {
        await Services.ReportService.SaveReport(report)
        if (keyRef.current === key) setSavedSnapshot(JSON.stringify(report))
      }
      const path = await Services.ReportService.ExportPDF(projectId, period)
      if (keyRef.current !== key) return
      if (path) setExportedPath(path)
    } catch (e) {
      if (keyRef.current === key) setError(e instanceof Error ? e.message : String(e))
    } finally {
      setExporting(false)
    }
  }

  const updateField = (field: 'clientName' | 'websiteName' | 'opmerkingen', value: string) => {
    if (!report) return
    setReport(new Report({ ...report, [field]: value }))
  }

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-hidden">
      {/* Top bar */}
      <div className="shrink-0 flex items-center gap-2 px-6 py-3 border-b border-border bg-panel flex-wrap">
        <select
          value={quarter}
          onChange={e => setQuarter(e.target.value)}
          className="bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg"
        >
          {QUARTERS.map(q => <option key={q} value={q}>{q}</option>)}
        </select>
        <select
          value={year}
          onChange={e => setYear(Number(e.target.value))}
          className="bg-panel-2 border border-border rounded-lg px-2.5 py-1.5 text-[12.5px] text-fg"
        >
          {yearOptions(initialPeriod.year).map(y => <option key={y} value={y}>{y}</option>)}
        </select>

        <button
          onClick={doPrefill}
          disabled={prefilling || loading}
          className="text-[12.5px] text-fg-muted border border-border rounded-lg px-3 py-1.5 hover:bg-hover transition disabled:opacity-50"
        >
          {prefilling ? <span className="animate-spin inline-block">↻</span> : '⟳ Vul automatisch'}
        </button>

        <button
          onClick={doSave}
          disabled={saving || loading || !report}
          className="relative ml-auto bg-accent text-white text-[12.5px] font-semibold px-4 py-1.5 rounded-lg hover:brightness-110 disabled:opacity-50 transition"
        >
          {saving ? <span className="animate-spin inline-block">↻</span> : saved ? '✓ Opgeslagen' : 'Opslaan'}
          {dirty && !saving && (
            <span className="absolute -top-1 -right-1 w-2 h-2 rounded-full bg-amber" />
          )}
        </button>

        <button
          onClick={doExport}
          disabled={exporting || loading || !report}
          className="bg-panel-2 border border-border text-fg-muted text-[12.5px] font-semibold px-3 py-1.5 rounded-lg hover:bg-hover transition disabled:opacity-50"
        >
          {exporting ? <span className="animate-spin inline-block">↻</span> : '⬇ Exporteer PDF'}
        </button>
      </div>

      {error && (
        <div className="shrink-0 mx-6 mt-3 bg-red-soft text-red px-3 py-2 rounded-lg text-xs">
          {error}
        </div>
      )}

      {exportedPath && (
        <div className="shrink-0 mx-6 mt-3 bg-green-soft text-green px-3 py-2 rounded-lg text-xs font-mono">
          Opgeslagen: {exportedPath}
        </div>
      )}

      <div className="flex-1 min-h-0 overflow-y-auto px-6 py-4">
        {loading || !report ? (
          <div className="flex-1 flex items-center justify-center text-fg-faint text-[13px] py-10 text-center">
            {loading ? <span className="animate-spin inline-block mr-2">↻</span> : null}
            {loading ? 'Laden…' : 'Geen data.'}
          </div>
        ) : (
          <>
            {/* Header fields */}
            <div className="flex gap-3 mb-4">
              <div className="flex-1">
                <div className="text-[10.5px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-1">Klantnaam</div>
                <input
                  value={report.clientName}
                  onChange={e => updateField('clientName', e.target.value)}
                  className="w-full bg-panel border border-border rounded-[9px] px-3 py-2 text-[13px] text-fg outline-none focus:border-accent focus:ring-1 focus:ring-accent/30"
                />
              </div>
              <div className="flex-1">
                <div className="text-[10.5px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-1">Website-naam</div>
                <input
                  value={report.websiteName}
                  onChange={e => updateField('websiteName', e.target.value)}
                  className="w-full bg-panel border border-border rounded-[9px] px-3 py-2 text-[13px] text-fg outline-none focus:border-accent focus:ring-1 focus:ring-accent/30"
                />
              </div>
            </div>

            <SectionCard title="Acties & aandachtspunten">
              <EditableTable
                rows={report.acties}
                columns={[
                  { key: 'actie', label: 'Actie' },
                  { key: 'wie', label: 'Wie', width: '160px' },
                ]}
                empty={{ actie: '', wie: '' }}
                onChange={rows => setReport(new Report({ ...report, acties: rows }))}
              />
            </SectionCard>

            <SectionCard title="Server / Uptime / TLS-monitoring">
              <EditableTable
                rows={report.monitoring}
                columns={[
                  { key: 'onderdeel', label: 'Onderdeel', width: '200px' },
                  { key: 'status', label: 'Status', width: '140px' },
                  { key: 'opmerking', label: 'Opmerking' },
                ]}
                empty={{ onderdeel: '', status: '', opmerking: '' }}
                onChange={rows => setReport(new Report({ ...report, monitoring: rows }))}
              />
            </SectionCard>

            <SectionCard title="Server software & frameworks">
              <EditableTable
                rows={report.software}
                columns={[
                  { key: 'component', label: 'Component', width: '160px' },
                  { key: 'huidig', label: 'Huidig', width: '110px' },
                  { key: 'ondersteundTot', label: 'Ondersteund tot', width: '140px' },
                  { key: 'laatste', label: 'Laatste', width: '110px' },
                  { key: 'opmerking', label: 'Opmerkingen' },
                ]}
                empty={{ component: '', huidig: '', ondersteundTot: '', laatste: '', opmerking: '' }}
                onChange={rows => setReport(new Report({ ...report, software: rows }))}
              />
            </SectionCard>

            <SectionCard title="Managed software-updates">
              <div className="text-[11.5px] font-semibold text-fg-muted mb-1.5">Dependency managers</div>
              <EditableTable
                rows={report.dependencyUpdates}
                columns={[
                  { key: 'naam', label: 'Naam', width: '200px' },
                  { key: 'uitgevoerd', label: 'Uitgevoerd', width: '140px' },
                  { key: 'opmerking', label: 'Opmerking' },
                ]}
                empty={{ naam: '', uitgevoerd: '', opmerking: '' }}
                onChange={rows => setReport(new Report({ ...report, dependencyUpdates: rows }))}
              />
              <div className="text-[11.5px] font-semibold text-fg-muted mb-1.5 mt-4">WordPress</div>
              <EditableTable
                rows={report.wpUpdates}
                columns={[
                  { key: 'naam', label: 'Naam', width: '200px' },
                  { key: 'uitgevoerd', label: 'Uitgevoerd', width: '140px' },
                  { key: 'opmerking', label: 'Opmerking' },
                ]}
                empty={{ naam: '', uitgevoerd: '', opmerking: '' }}
                onChange={rows => setReport(new Report({ ...report, wpUpdates: rows }))}
              />
            </SectionCard>

            <SectionCard title="AVG-check">
              <EditableTable
                rows={report.avg}
                columns={[
                  { key: 'onderwerp', label: 'Onderwerp', width: '220px' },
                  { key: 'opmerking', label: 'Opmerking' },
                ]}
                empty={{ onderwerp: '', opmerking: '' }}
                onChange={rows => setReport(new Report({ ...report, avg: rows }))}
              />
            </SectionCard>

            <SectionCard title="Overige opmerkingen">
              <textarea
                value={report.opmerkingen}
                onChange={e => updateField('opmerkingen', e.target.value)}
                rows={5}
                placeholder="Vrije ruimte voor extra informatie van de developer/updater…"
                className="w-full bg-panel border border-border rounded-[9px] px-3 py-2 text-[13px] text-fg outline-none focus:border-accent focus:ring-1 focus:ring-accent/30 resize-y"
              />
            </SectionCard>
          </>
        )}
      </div>
    </div>
  )
}
