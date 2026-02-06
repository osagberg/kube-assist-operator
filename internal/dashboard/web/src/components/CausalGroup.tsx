import { useState } from 'react'
import type { CausalGroup as CausalGroupType } from '../types'

const severityPills: Record<string, string> = {
  Critical: 'severity-pill-critical',
  Warning: 'severity-pill-warning',
  Info: 'severity-pill-info',
}

const severityLabels: Record<string, string> = {
  Critical: 'CR',
  Warning: 'WR',
  Info: 'IN',
}

const ruleLabels: Record<string, string> = {
  'oom-quota': 'OOM + Quota Pressure',
  'crash-imagepull': 'Crash + Image Pull',
  'flux-chain': 'Flux Pipeline Failure',
  'pvc-workload': 'PVC + Workload Scheduling',
  'resource-graph': 'Ownership Chain',
  temporal: 'Temporal Correlation',
}

function stripIssueRefs(text: string): string {
  return text
    .replace(/\s*\((?:see\s+)?(?:issue|group)_\d+\)/gi, '')
    .replace(/\b(?:issue|group)_\d+\b/gi, '')
    .trim()
}

interface Props {
  group: CausalGroupType
}

export function CausalGroupCard({ group }: Props) {
  const [expanded, setExpanded] = useState(false)
  const pillClass = severityPills[group.severity] ?? severityPills.Info
  const pillText = severityLabels[group.severity] ?? 'IN'
  const ruleLabel = ruleLabels[group.rule] ?? group.rule

  return (
    <div className="glass-panel rounded-xl p-4">
      <button
        className="w-full text-left flex items-center justify-between"
        onClick={() => setExpanded((e) => !e)}
        aria-expanded={expanded}
      >
        <div className="flex items-center gap-2 flex-1 min-w-0">
          <span className={pillClass} aria-hidden>{pillText}</span>
          <div className="min-w-0">
            <div className="font-semibold text-sm truncate" style={{ color: 'var(--text-primary)' }}>{group.title}</div>
            <div className="flex items-center gap-2 mt-0.5">
              <span className="text-xs px-1.5 py-0.5 glass-inset font-mono rounded-md" style={{ color: 'var(--text-secondary)' }}>
                {ruleLabel}
              </span>
              {group.aiEnhanced && (
                <span className="text-xs px-1.5 py-0.5 rounded bg-ai-bg text-ai border border-ai-border font-semibold">
                  AI
                </span>
              )}
              <span className="text-xs" style={{ color: 'var(--text-tertiary)' }}>
                {Math.round(group.confidence * 100)}% confidence
              </span>
              <span className="text-xs" style={{ color: 'var(--text-tertiary)' }}>
                {group.events.length} issues
              </span>
            </div>
          </div>
        </div>
        <svg
          className={`ml-2 w-4 h-4 transition-transform duration-200 ${expanded ? 'rotate-180' : ''}`}
          style={{ color: 'var(--text-tertiary)' }}
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
        </svg>
      </button>

      {group.rootCause && (
        <div className="mt-2 text-sm glass-inset rounded-lg px-3 py-2">
          <span className="font-medium" style={{ color: 'var(--text-secondary)' }}>Root Cause:</span>{' '}
          <span style={{ color: 'var(--text-secondary)' }}>{group.rootCause}</span>
        </div>
      )}

      {group.aiRootCause && (
        <div className="mt-2 text-sm bg-ai-bg border border-ai-border rounded-lg px-3 py-2">
          <div className="flex items-center gap-1.5 mb-1">
            <span className="font-semibold text-ai text-xs">AI Analysis</span>
          </div>
          <div className="break-words" style={{ color: 'var(--text-secondary)' }}>
            <span className="font-medium">Root Cause:</span> {stripIssueRefs(group.aiRootCause)}
          </div>
          {group.aiSuggestion && (
            <div className="mt-1 break-words" style={{ color: 'var(--text-secondary)' }}>
              <span className="font-medium">Recommendation:</span> {stripIssueRefs(group.aiSuggestion)}
            </div>
          )}
        </div>
      )}

      {expanded && (
        <div className="mt-3 space-y-2">
          {group.aiSteps && group.aiSteps.length > 0 && (
            <div className="mb-2 bg-ai-bg border border-ai-border rounded-lg px-3 py-2">
              <div className="text-xs font-semibold text-ai mb-1">Remediation Steps</div>
              <ol className="list-decimal list-inside text-xs space-y-0.5" style={{ color: 'var(--text-secondary)' }}>
                {group.aiSteps.map((step, i) => (
                  <li key={i}>{stripIssueRefs(step)}</li>
                ))}
              </ol>
            </div>
          )}
          {group.events.map((event, i) => (
            <div
              key={`${event.checker}-${event.issue.resource}-${i}`}
              className="text-xs glass-inset rounded-lg px-3 py-2 flex items-start gap-2"
            >
              <span className="font-mono shrink-0" style={{ color: 'var(--text-tertiary)' }}>
                {event.checker}
              </span>
              <span className="font-medium shrink-0" style={{ color: 'var(--text-primary)' }}>{event.issue.namespace}/{event.issue.resource}</span>
              <span className="truncate" style={{ color: 'var(--text-secondary)' }}>{event.issue.message}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
