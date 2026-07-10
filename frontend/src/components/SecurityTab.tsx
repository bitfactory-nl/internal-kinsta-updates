import { useState, useEffect } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { SecurityScanResult, SecurityFinding } from '../../bindings/github.com/rdm/sites-tool/internal/services'

interface Props {
  projectId: string
}

const severityStyles: Record<string, string> = {
  critical: 'bg-red-500/15 text-red-700',
  high: 'bg-orange-500/15 text-orange-700',
  moderate: 'bg-amber-500/15 text-amber-700',
  low: 'bg-gray-500/10 text-gray-600',
  unknown: 'bg-gray-500/10 text-gray-600',
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
    <span className={`text-[10px] px-1.5 py-px rounded font-medium uppercase shrink-0 ${severityStyles[severity] ?? severityStyles.unknown}`}>
      {severity}
    </span>
  )
}

function FindingRow({ finding }: { finding: SecurityFinding }) {
  return (
    <div className="px-4 py-3 flex items-start gap-3">
      <SeverityBadge severity={finding.severity} />
      <div className="flex-1 min-w-0">
        <p className="text-sm text-gray-900 truncate">
          <span className="font-mono">{finding.package}</span>
          <span className="ml-2 text-[10px] text-gray-600 uppercase">{finding.source}</span>
        </p>
        <p className="text-xs text-gray-700 mt-0.5">{finding.title}</p>
        <p className="text-[11px] text-gray-600 mt-0.5 flex items-center gap-2">
          {finding.cve && <span className="font-mono">{finding.cve}</span>}
          {finding.link && (
            <a
              href={finding.link}
              target="_blank"
              rel="noreferrer"
              className="text-indigo-600 hover:text-indigo-800 transition-colors"
            >
              details ↗
            </a>
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
      <div className="flex-1 flex items-center justify-center gap-2 text-gray-600 text-sm">
        <span className="animate-spin inline-block">↻</span>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex-1 flex flex-col p-4 gap-3">
        <div className="bg-red-100 text-red-600 px-3 py-2 rounded text-xs">{error}</div>
        <button
          onClick={fetchResults}
          className="self-start px-3 py-1.5 text-xs bg-black/[0.06] hover:bg-black/[0.1] text-gray-800 rounded-lg transition-colors"
        >
          ⟳ Opnieuw proberen
        </button>
      </div>
    )
  }

  if (!result) {
    return (
      <div className="flex-1 flex items-center justify-center text-gray-600 text-sm italic py-12">
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
      <div className="px-4 py-2 border-b border-black/[0.06] shrink-0 flex items-center gap-3">
        <span className="text-[11px] text-gray-600">
          Laatste scan: {formatDate(result.scannedAt)}
        </span>
        <span className="text-[11px] text-gray-600">
          {result.hasComposerReport && 'composer'}
          {result.hasComposerReport && result.hasNpmReport && ' + '}
          {result.hasNpmReport && 'npm'}
        </span>
        <button
          onClick={fetchResults}
          className="ml-auto text-gray-600 hover:text-gray-800 text-xs transition-colors flex items-center gap-1"
          title="Ververs scanresultaten"
        >
          <span className="text-xs">⟳</span> Ververs
        </button>
      </div>

      {findings.length === 0 ? (
        <div className="flex-1 flex flex-col items-center justify-center gap-2 text-sm py-12">
          <span className="text-2xl">✅</span>
          <p className="text-gray-800">Geen bekende kwetsbaarheden</p>
          <p className="text-xs text-gray-600">
            composer audit en npm audit vonden niets in de laatste scan
          </p>
        </div>
      ) : (
        <>
          <div className="px-4 py-2 border-b border-black/[0.06] shrink-0 flex items-center gap-2">
            {(['critical', 'high', 'moderate', 'low', 'unknown'] as const)
              .filter(sev => counts[sev])
              .map(sev => (
                <span key={sev} className="flex items-center gap-1.5">
                  <SeverityBadge severity={sev} />
                  <span className="text-xs text-gray-700">{counts[sev]}</span>
                </span>
              ))}
          </div>
          <div className="flex-1 overflow-y-auto divide-y divide-black/[0.06]">
            {findings.map((f, i) => (
              <FindingRow key={`${f.source}-${f.package}-${i}`} finding={f} />
            ))}
          </div>
        </>
      )}
    </div>
  )
}
