import { useState } from 'react'
import type { CausalGroup as CausalGroupType } from '../types'

const severityColors: Record<string, string> = {
  Critical: 'border-l-red-500 bg-red-50 dark:bg-red-900/20',
  Warning: 'border-l-yellow-500 bg-yellow-50 dark:bg-yellow-900/20',
  Info: 'border-l-blue-500 bg-blue-50 dark:bg-blue-900/20',
}

const severityIcons: Record<string, string> = {
  Critical: '\u26A0',
  Warning: '\u25B2',
  Info: '\u2139',
}

const ruleLabels: Record<string, string> = {
  'oom-quota': 'OOM + Quota Pressure',
  'crash-imagepull': 'Crash + Image Pull',
  'flux-chain': 'Flux Pipeline Failure',
  'pvc-workload': 'PVC + Workload Scheduling',
  'resource-graph': 'Ownership Chain',
  temporal: 'Temporal Correlation',
}

interface Props {
  group: CausalGroupType
}

export function CausalGroupCard({ group }: Props) {
  const [expanded, setExpanded] = useState(false)
  const colorClass = severityColors[group.severity] ?? severityColors.Info
  const icon = severityIcons[group.severity] ?? ''
  const ruleLabel = ruleLabels[group.rule] ?? group.rule

  return (
    <div className={`border-l-4 rounded-lg p-4 ${colorClass}`}>
      <button
        className="w-full text-left flex items-center justify-between"
        onClick={() => setExpanded((e) => !e)}
        aria-expanded={expanded}
      >
        <div className="flex items-center gap-2 flex-1 min-w-0">
          <span className="text-lg" aria-hidden>{icon}</span>
          <div className="min-w-0">
            <div className="font-semibold text-sm truncate">{group.title}</div>
            <div className="flex items-center gap-2 mt-0.5">
              <span className="text-xs px-1.5 py-0.5 rounded bg-gray-200 dark:bg-gray-700 font-mono">
                {ruleLabel}
              </span>
              <span className="text-xs text-gray-500 dark:text-gray-400">
                {Math.round(group.confidence * 100)}% confidence
              </span>
              <span className="text-xs text-gray-500 dark:text-gray-400">
                {group.events.length} issues
              </span>
            </div>
          </div>
        </div>
        <span className="text-gray-400 ml-2">{expanded ? '\u25B2' : '\u25BC'}</span>
      </button>

      {group.rootCause && (
        <div className="mt-2 text-sm text-gray-700 dark:text-gray-300 bg-white/50 dark:bg-gray-800/50 rounded px-3 py-2">
          <span className="font-medium">Root Cause:</span> {group.rootCause}
        </div>
      )}

      {expanded && (
        <div className="mt-3 space-y-2">
          {group.events.map((event, i) => (
            <div
              key={`${event.checker}-${event.issue.resource}-${i}`}
              className="text-xs bg-white dark:bg-gray-800 rounded px-3 py-2 flex items-start gap-2"
            >
              <span className="font-mono text-gray-500 dark:text-gray-400 shrink-0">
                {event.checker}
              </span>
              <span className="font-medium shrink-0">{event.issue.namespace}/{event.issue.resource}</span>
              <span className="text-gray-600 dark:text-gray-400 truncate">{event.issue.message}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
