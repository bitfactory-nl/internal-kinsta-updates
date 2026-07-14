import { useState, useEffect } from 'react'
import ExternalLink from './ExternalLink'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { SecurityScanResult, SecurityFinding } from '../../bindings/github.com/rdm/sites-tool/internal/services'

interface Props {
  projectId: string
}

const severityStyles: Record<string, string> = {
  critical: 'text-red bg-red-soft',
  high: 'text-orange bg-orange-soft',
  moderate: 'text-amber bg-amber-soft',
  low: 'text-fg-muted bg-hover',
  unknown: 'text-fg-muted bg-hover',
}

function formatDate(dateStr: string): string {
  const d = new Date(dateStr)
  if (isNaN(d.getTime())) return dateStr
  return d.toLocaleDateString('nl-NL', {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function SeverityBadge({ severity }: { severity: string }) {
  return (
    <span className={`w-[76px] shrink-0 text-center text-[9.5px] font-semibold tracking-[.04em] uppercase py-1 rounded-[5px] h-fit ${severityStyles[severity] ?? severityStyles.unknown}`}>
      {severity}
    </span>
  )
}

function FindingRow({ finding }: { finding: SecurityFinding }) {
  return (
    <div className="px-4 py-[15px] flex items-start gap-3.5 hover:bg-hover transition-colors">
      <SeverityBadge severity={finding.severity} />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="font-mono font-semibold text-[13.5px] text-fg truncate">{finding.package}</span>
          <span className="shrink-0 text-[9.5px] font-medium tracking-[.04em] uppercase text-fg-faint border border-border px-1.5 py-px rounded-[5px]">{finding.source}</span>
        </div>
        <p className="text-[12.5px] font-[450] text-fg-muted mt-1">{finding.title}</p>
        <p className="text-xs mt-1.5 flex items-center gap-2.5">
          {finding.cve && <span className="font-mono text-fg-faint">{finding.cve}</span>}
          {finding.link && (
            <ExternalLink
              href={finding.link}
              className="font-medium text-accent hover:text-accent-2 transition-colors"
            >
              details ↗
            </ExternalLink>
          )}
        </p>
      </div>
    </div>
  )
}

export default function SecurityTab({ projectId }: Props) {
  const [result, setResult] = useState<SecurityScanResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const fetchResults = () => {
    setError(null)
    setLoading(true)
    Services.SecurityService.GetScanResults(projectId)
      .then(r => setResult(r))
      .catch(e => setError(String(e)))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    setResult(null)
    fetchResults()
  }, [projectId])

  if (loading) {
    return (
      <div className="flex-1 flex items-center justify-center gap-2 text-fg-faint text-sm">
        <span className="animate-spin inline-block">↻</span>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex-1 flex flex-col p-4 gap-3">
        <div className="bg-red-soft text-red px-3 py-2 rounded-lg text-xs">{error}</div>
        <button
          onClick={fetchResults}
          className="self-start bg-panel-2 border border-border text-fg-muted text-[12.5px] font-semibold px-[15px] py-[9px] rounded-[9px] hover:bg-hover transition-colors"
        >
          ⟳ Opnieuw proberen
        </button>
      </div>
    )
  }

  if (!result) {
    return (
      <div className="flex-1 flex items-center justify-center text-fg-faint text-[13px] italic py-12">
        Geen scanresultaten beschikbaar
      </div>
    )
  }

  const findings = result.findings ?? []
  const counts = findings.reduce<Record<string, number>>((acc, f) => {
    acc[f.severity] = (acc[f.severity] ?? 0) + 1
    return acc
  }, {})

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-hidden">
      <div className="flex-1 overflow-y-auto">
        <div className="px-6 pt-5 pb-[50px]">
          {/* Header row */}
          <div className="flex items-center gap-3 mb-4">
            <span className="text-xs font-[450] text-fg-muted">
              Laatste scan: {formatDate(result.scannedAt)}
            </span>
            {(result.hasComposerReport || result.hasNpmReport) && (
              <span className="font-mono text-[11px] font-medium text-fg-faint bg-panel-2 border border-border px-2 py-px rounded-[5px]">
                {result.hasComposerReport && 'composer'}
                {result.hasComposerReport && result.hasNpmReport && ' + '}
                {result.hasNpmReport && 'npm'}
              </span>
            )}
            <button
              onClick={fetchResults}
              className="ml-auto text-xs font-semibold text-accent hover:text-accent-2 transition-colors flex items-center gap-1"
              title="Ververs scanresultaten"
            >
              ↻ Ververs
            </button>
          </div>

          {findings.length === 0 ? (
            <div className="flex flex-col items-center justify-center gap-2 text-sm py-12">
              <span className="text-2xl">✅</span>
              <p className="text-fg font-medium">Geen bekende kwetsbaarheden</p>
              <p className="text-xs text-fg-muted">
                composer audit en npm audit vonden niets in de laatste scan
              </p>
            </div>
          ) : (
            <>
              {/* Severity count chips */}
              <div className="flex flex-wrap gap-[9px] mb-[18px]">
                {(['critical', 'high', 'moderate', 'low', 'unknown'] as const)
                  .filter(sev => counts[sev])
                  .map(sev => (
                    <div key={sev} className="flex items-center gap-2 bg-panel border border-border rounded-[9px] px-[13px] py-2">
                      <span className={`text-[9.5px] font-semibold tracking-[.05em] uppercase px-2 py-[3px] rounded-[5px] ${severityStyles[sev] ?? severityStyles.unknown}`}>
                        {sev}
                      </span>
                      <span className="font-mono font-semibold text-[15px] text-fg">{counts[sev]}</span>
                    </div>
                  ))}
              </div>

              {/* Findings card */}
              <div className="bg-panel border border-border rounded-[11px] overflow-hidden divide-y divide-border">
                {findings.map((f, i) => (
                  <FindingRow key={`${f.source}-${f.package}-${i}`} finding={f} />
                ))}
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
