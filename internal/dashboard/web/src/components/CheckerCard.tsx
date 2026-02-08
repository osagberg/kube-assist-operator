import { useState } from 'react'
import type { CheckResult, TargetKind } from '../types'
import { IssueRow } from './IssueRow'
import type { Severity } from './SeverityTabs'

interface Props {
  name: string
  result: CheckResult
  search: string
  severity: Severity
  namespace: string
  onDiagnose?: (prefill?: { namespace?: string; targetKind?: TargetKind; targetName?: string }) => void
}

export function CheckerCard({ name, result, search, severity, namespace, onDiagnose }: Props) {
  const [collapsed, setCollapsed] = useState(false)

  const filteredIssues = result.issues.filter((issue) => {
    if (severity !== 'all' && issue.severity !== severity) return false
    if (namespace && issue.namespace !== namespace) return false
    if (search) {
      const q = search.toLowerCase()
      return (
        issue.resource.toLowerCase().includes(q) ||
        issue.message.toLowerCase().includes(q) ||
        issue.namespace.toLowerCase().includes(q)
      )
    }
    return true
  })

  const hasCritical = result.issues.some((i) => i.severity === 'Critical')
  const hasWarning = result.issues.some((i) => i.severity === 'Warning')
  const aiCount = result.issues.filter((i) => i.aiEnhanced).length

  return (
    <div className="glass-panel rounded-xl">
      <button
        onClick={() => setCollapsed(!collapsed)}
        aria-expanded={!collapsed}
        className="w-full flex items-center justify-between p-4 text-left rounded-xl transition-all duration-200 hover:bg-glass-200"
      >
        <div className="flex items-center gap-3">
          <svg className="w-4 h-4 text-accent transition-transform duration-200" style={{ transform: collapsed ? 'rotate(-90deg)' : '' }} fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
          </svg>
          <h3 className="font-semibold text-lg capitalize" style={{ color: 'var(--text-primary)' }}>{name}</h3>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-sm px-2.5 py-0.5 rounded-lg bg-severity-healthy-bg text-severity-healthy">
            {result.healthy} healthy
          </span>
          {result.issues.length > 0 && (
            <span className={`text-sm px-2.5 py-0.5 rounded-lg ${
              hasCritical ? 'bg-severity-critical-bg text-severity-critical' :
              hasWarning ? 'bg-severity-warning-bg text-severity-warning' :
              'bg-severity-info-bg text-severity-info'
            }`}>
              {result.issues.length} issues
            </span>
          )}
          {aiCount > 0 && (
            <span className="text-sm px-2.5 py-0.5 rounded-lg bg-ai-bg text-ai border border-ai-border">
              {aiCount} AI
            </span>
          )}
        </div>
      </button>

      {result.error && (
        <div className="px-4 pb-2 text-severity-critical text-sm">{result.error}</div>
      )}

      {!collapsed && filteredIssues.length > 0 && (
        <div className="border-t border-edge">
          {filteredIssues.map((issue) => (
            <IssueRow key={`${issue.namespace}-${issue.resource}-${issue.type}`} issue={issue} onDiagnose={onDiagnose} />
          ))}
        </div>
      )}

      {!collapsed && filteredIssues.length === 0 && result.issues.length > 0 && (
        <div className="px-4 py-3 text-sm border-t border-edge" style={{ color: 'var(--text-tertiary)' }}>
          No issues match filters
        </div>
      )}
    </div>
  )
}
