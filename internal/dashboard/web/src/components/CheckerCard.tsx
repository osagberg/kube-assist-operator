import { useState } from 'react'
import type { CheckResult, IssueState, TargetKind } from '../types'
import { IssueRow } from './IssueRow'
import type { Severity } from './SeverityTabs'

interface Props {
  name: string
  result: CheckResult
  search: string
  severity: Severity
  namespace: string
  issueStates?: Record<string, IssueState>
  onAcknowledge?: (key: string) => void
  onSnooze?: (key: string, duration: string) => void
  onDismissState?: (key: string) => void
  onDiagnose?: (prefill?: { namespace?: string; targetKind?: TargetKind; targetName?: string }) => void
}

function makeIssueKey(issue: { namespace: string; resource: string; type: string }): string {
  return `${issue.namespace}/${issue.resource}/${issue.type}`
}

function isStateMuted(state: IssueState | undefined): boolean {
  if (!state) return false
  if (state.action === 'acknowledged') return true
  if (state.action === 'snoozed') {
    if (!state.snoozedUntil) return true
    return new Date(state.snoozedUntil).getTime() > Date.now()
  }
  return false
}

export function CheckerCard({ name, result, search, severity, namespace, issueStates, onAcknowledge, onSnooze, onDismissState, onDiagnose }: Props) {
  const [collapsed, setCollapsed] = useState(false)
  const [showMuted, setShowMuted] = useState(false)
  const issues = Array.isArray(result.issues) ? result.issues : []

  const filteredIssues = issues.filter((issue) => {
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

  const activeIssues = filteredIssues.filter((issue) => !isStateMuted(issueStates?.[makeIssueKey(issue)]))
  const mutedIssues = filteredIssues.filter((issue) => isStateMuted(issueStates?.[makeIssueKey(issue)]))

  const hasCritical = activeIssues.some((i) => i.severity === 'Critical')
  const hasWarning = activeIssues.some((i) => i.severity === 'Warning')
  const aiCount = filteredIssues.filter((i) => i.aiEnhanced).length

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
          {activeIssues.length > 0 && (
            <span className={`text-sm px-2.5 py-0.5 rounded-lg ${
              hasCritical ? 'bg-severity-critical-bg text-severity-critical' :
              hasWarning ? 'bg-severity-warning-bg text-severity-warning' :
              'bg-severity-info-bg text-severity-info'
            }`}>
              {activeIssues.length} issue{activeIssues.length !== 1 ? 's' : ''}
            </span>
          )}
          {mutedIssues.length > 0 && (
            <span className="text-sm px-2.5 py-0.5 rounded-lg glass-inset" style={{ color: 'var(--text-tertiary)' }}>
              {mutedIssues.length} muted
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

      {!collapsed && activeIssues.length > 0 && (
        <div className="border-t border-edge">
          {activeIssues.map((issue) => {
            const key = makeIssueKey(issue)
            return (
              <IssueRow
                key={key}
                issue={issue}
                issueKey={key}
                issueState={issueStates?.[key]}
                onAcknowledge={onAcknowledge}
                onSnooze={onSnooze}
                onDismissState={onDismissState}
                onDiagnose={onDiagnose}
              />
            )
          })}
        </div>
      )}

      {!collapsed && mutedIssues.length > 0 && (
        <div className="border-t border-edge">
          <button
            onClick={() => setShowMuted(!showMuted)}
            className="w-full px-4 py-2 text-xs text-left transition-all duration-200 hover:bg-glass-200 flex items-center gap-1.5"
            style={{ color: 'var(--text-tertiary)' }}
          >
            <svg className="w-3 h-3 transition-transform duration-200" style={{ transform: showMuted ? '' : 'rotate(-90deg)' }} fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
            </svg>
            {mutedIssues.length} acknowledged/snoozed
          </button>
          {showMuted && mutedIssues.map((issue) => {
            const key = makeIssueKey(issue)
            return (
              <IssueRow
                key={key}
                issue={issue}
                issueKey={key}
                issueState={issueStates?.[key]}
                onAcknowledge={onAcknowledge}
                onSnooze={onSnooze}
                onDismissState={onDismissState}
                onDiagnose={onDiagnose}
              />
            )
          })}
        </div>
      )}

      {!collapsed && activeIssues.length === 0 && mutedIssues.length === 0 && issues.length > 0 && (
        <div className="px-4 py-3 text-sm border-t border-edge" style={{ color: 'var(--text-tertiary)' }}>
          No issues match filters
        </div>
      )}
    </div>
  )
}
