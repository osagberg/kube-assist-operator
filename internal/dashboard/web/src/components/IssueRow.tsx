import { useState, useRef, useEffect } from 'react'
import type { Issue, IssueState, TargetKind } from '../types'

interface Props {
  issue: Issue
  issueKey: string
  issueState?: IssueState
  onAcknowledge?: (key: string) => void
  onSnooze?: (key: string, duration: string) => void
  onDismissState?: (key: string) => void
  onDiagnose?: (prefill?: { namespace?: string; targetKind?: TargetKind; targetName?: string }) => void
}

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

function stripIssueRefs(text: string): string {
  return text
    .replace(/\s*\((?:see\s+)?(?:issue|group)_\d+\)/gi, '')
    .replace(/\b(?:issue|group)_\d+\b/gi, '')
    .trim()
}

const RESOURCE_KIND_MAP: Record<string, TargetKind> = {
  deployment: 'Deployment',
  deployments: 'Deployment',
  statefulset: 'StatefulSet',
  statefulsets: 'StatefulSet',
  daemonset: 'DaemonSet',
  daemonsets: 'DaemonSet',
  pod: 'Pod',
  pods: 'Pod',
  replicaset: 'ReplicaSet',
  replicasets: 'ReplicaSet',
}

function parseResourceKindAndName(resource: string): { kind?: TargetKind; name: string } {
  const slashIdx = resource.indexOf('/')
  if (slashIdx === -1) return { name: resource }
  const rawKind = resource.slice(0, slashIdx).toLowerCase()
  const name = resource.slice(slashIdx + 1)
  return { kind: RESOURCE_KIND_MAP[rawKind], name }
}

const SNOOZE_OPTIONS = [
  { label: '30m', value: '30m' },
  { label: '1h', value: '1h' },
  { label: '4h', value: '4h' },
  { label: '24h', value: '24h' },
]

export function IssueRow({ issue, issueKey, issueState, onAcknowledge, onSnooze, onDismissState, onDiagnose }: Props) {
  const [copied, setCopied] = useState(false)
  const [showFull, setShowFull] = useState(false)
  const [showSnooze, setShowSnooze] = useState(false)
  const snoozeRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!showSnooze) return
    const handler = (e: MouseEvent) => {
      if (!snoozeRef.current?.contains(e.target as Node)) setShowSnooze(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [showSnooze])
  const pillClass = severityPills[issue.severity] ?? severityPills.Info
  const pillText = severityLabels[issue.severity] ?? 'IN'

  const copyCommand = () => {
    const cmd = extractCommand(issue.suggestion)
    if (cmd) {
      navigator.clipboard.writeText(cmd).then(() => {
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
      })
    }
  }

  const cmd = extractCommand(issue.suggestion)

  const isMuted = !!issueState

  return (
    <div className={`px-4 py-2 transition-all duration-200 hover:bg-glass-200 ${isMuted ? 'opacity-50' : ''}`}>
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-start gap-2.5 min-w-0 flex-1">
          <span className={`${pillClass} mt-0.5`}>{pillText}</span>
          <div className="min-w-0">
            <div className="font-medium text-sm flex items-center gap-1.5">
              <span>
                <span style={{ color: 'var(--text-tertiary)' }}>{issue.namespace}/</span>
                <span style={{ color: 'var(--text-primary)' }}>{issue.resource}</span>
              </span>
              {issue.aiEnhanced && (
                <span className="inline-flex items-center px-1.5 py-0.5 rounded-full text-[10px] font-semibold bg-ai-bg text-ai border border-ai-border">
                  AI
                </span>
              )}
            </div>
            <div className="text-sm mt-0.5" style={{ color: 'var(--text-secondary)' }}>{issue.message}</div>
            {issue.rootCause && (
              <div className="text-xs mt-0.5 italic break-words" style={{ color: 'var(--text-tertiary)' }}>
                Root cause: {stripIssueRefs(issue.rootCause)}
              </div>
            )}
            {issue.suggestion && (
              <div
                className={`text-xs mt-0.5 break-words cursor-pointer ${showFull ? '' : 'line-clamp-1'}`}
                style={{ color: 'var(--text-tertiary)' }}
                onClick={() => setShowFull(!showFull)}
                title={showFull ? 'Click to collapse' : 'Click to expand'}
              >
                {stripIssueRefs(issue.suggestion)}
              </div>
            )}
            {cmd && (
              <div className="mt-1.5 flex items-center gap-2">
                <code className="text-xs glass-inset px-2 py-1 rounded-md font-mono break-all" style={{ color: 'var(--text-secondary)' }}>
                  {cmd}
                </code>
                <button
                  onClick={copyCommand}
                  className="text-xs text-accent hover:underline flex-shrink-0 transition-all duration-200"
                >
                  {copied ? 'Copied!' : 'Copy'}
                </button>
              </div>
            )}
          </div>
        </div>
        <div className="flex items-center gap-2 flex-shrink-0 mt-0.5">
          {isMuted ? (
            <div className="flex items-center gap-1.5">
              <span className="text-[10px] px-1.5 py-0.5 rounded-full glass-inset font-medium" style={{ color: 'var(--text-tertiary)' }}>
                {issueState.action === 'acknowledged' ? 'Acked' : `Snoozed${issueState.snoozedUntil ? ` until ${new Date(issueState.snoozedUntil).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}` : ''}`}
              </span>
              {onDismissState && (
                <button
                  onClick={() => onDismissState(issueKey)}
                  className="text-[10px] text-accent hover:underline transition-all duration-200"
                >
                  {issueState.action === 'acknowledged' ? 'Undo' : 'Wake'}
                </button>
              )}
            </div>
          ) : (
            <>
              {onAcknowledge && (
                <button
                  onClick={() => onAcknowledge(issueKey)}
                  className="text-xs text-accent hover:underline transition-all duration-200"
                  aria-label={`Acknowledge ${issue.resource}`}
                >
                  Ack
                </button>
              )}
              {onSnooze && (
                <div className="relative" ref={snoozeRef}>
                  <button
                    onClick={() => setShowSnooze(!showSnooze)}
                    className="text-xs text-accent hover:underline transition-all duration-200"
                    aria-label={`Snooze ${issue.resource}`}
                  >
                    Snooze
                  </button>
                  {showSnooze && (
                    <div className="absolute right-0 top-6 z-50 glass-elevated rounded-lg p-1 flex flex-col gap-0.5 min-w-[80px]">
                      {SNOOZE_OPTIONS.map((opt) => (
                        <button
                          key={opt.value}
                          onClick={() => {
                            onSnooze(issueKey, opt.value)
                            setShowSnooze(false)
                          }}
                          className="text-xs px-3 py-1.5 rounded-md text-left transition-all duration-200 hover:bg-glass-200"
                          style={{ color: 'var(--text-secondary)' }}
                        >
                          {opt.label}
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </>
          )}
          {onDiagnose && (
            <button
              onClick={() => {
                const parsed = parseResourceKindAndName(issue.resource)
                onDiagnose({
                  namespace: issue.namespace,
                  targetKind: parsed.kind,
                  targetName: parsed.name,
                })
              }}
              className="text-xs text-accent hover:underline flex-shrink-0 transition-all duration-200"
              aria-label={`Diagnose ${issue.resource}`}
            >
              Diagnose
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

function extractCommand(suggestion: string): string | null {
  const match = suggestion.match(/`(kubectl[^`]+)`/)
  return match ? match[1] : null
}
