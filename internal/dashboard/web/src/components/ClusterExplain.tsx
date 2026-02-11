import { useState } from 'react'
import { useExplain } from '../hooks/useExplain'

const riskColors: Record<string, string> = {
  low: 'severity-pill-healthy',
  medium: 'severity-pill-warning',
  high: 'severity-pill-critical',
  critical: 'severity-pill-critical',
  unknown: 'severity-pill-info',
}

const riskLabels: Record<string, string> = {
  low: 'Low Risk',
  medium: 'Medium Risk',
  high: 'High Risk',
  critical: 'Critical Risk',
  unknown: 'Unknown',
}

const trendArrows: Record<string, string> = {
  improving: '\u2197',
  stable: '\u2192',
  degrading: '\u2198',
  unknown: '?',
}

export function ClusterExplain({ clusterId }: { clusterId?: string }) {
  const { data, loading, error, refresh } = useExplain(clusterId)
  const [collapsed, setCollapsed] = useState(false)

  // Don't render until user explicitly opens it
  const hasData = data && data.riskLevel !== 'unknown'

  return (
    <div className="glass-panel rounded-xl p-6">
      <button
        className="w-full flex items-center justify-between"
        onClick={() => {
          setCollapsed((c) => !c)
        }}
        aria-expanded={!collapsed}
      >
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-semibold" style={{ color: 'var(--text-secondary)' }}>
            Explain This Cluster
          </h2>
          <span className="text-[10px] font-medium px-2 py-0.5 rounded-full bg-ai-bg text-ai border border-ai-border">
            AI
          </span>
        </div>
        <div className="flex items-center gap-3 text-xs" style={{ color: 'var(--text-tertiary)' }}>
          {hasData && (
            <>
              <span className={riskColors[data.riskLevel] ?? 'severity-pill-info'}>
                {riskLabels[data.riskLevel] ?? data.riskLevel}
              </span>
              <span>{trendArrows[data.trendDirection] ?? '?'} {data.trendDirection}</span>
              {data.confidence > 0 && (
                <span>{Math.round(data.confidence * 100)}%</span>
              )}
            </>
          )}
          <svg
            className={`w-4 h-4 transition-transform duration-200 ${collapsed ? '' : 'rotate-180'}`}
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
          >
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
          </svg>
        </div>
      </button>

      {!collapsed && (
        <div className="mt-4 space-y-4 animate-fade-in">
          {loading && !data && (
            <div className="flex items-center justify-center py-6">
              <div className="w-5 h-5 border-2 border-accent-muted border-t-accent rounded-full animate-spin" />
            </div>
          )}

          {error && (
            <div className="text-xs text-severity-critical">{error}</div>
          )}

          {hasData && (
            <>
              {/* Narrative */}
              <p className="text-sm leading-relaxed" style={{ color: 'var(--text-primary)' }}>
                {data.narrative}
              </p>

              {/* Top Issues */}
              {data.topIssues && data.topIssues.length > 0 && (
                <div className="space-y-2">
                  <h3 className="text-xs font-semibold" style={{ color: 'var(--text-tertiary)' }}>
                    Top Issues
                  </h3>
                  <div className="space-y-1.5">
                    {data.topIssues.map((issue, i) => (
                      <div key={i} className="glass-inset rounded-lg px-3 py-2 flex items-start gap-2">
                        <span className={`mt-0.5 ${
                          issue.severity.toLowerCase() === 'critical' ? 'severity-dot-critical' :
                          issue.severity.toLowerCase() === 'warning' ? 'severity-dot-warning' :
                          'severity-dot-info'
                        }`} />
                        <div className="min-w-0">
                          <div className="text-xs font-medium" style={{ color: 'var(--text-primary)' }}>
                            {issue.title}
                          </div>
                          <div className="text-[11px]" style={{ color: 'var(--text-tertiary)' }}>
                            {issue.impact}
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* Footer: tokens + refresh */}
              <div className="flex items-center justify-between text-[11px] pt-1" style={{ color: 'var(--text-tertiary)' }}>
                <span>
                  {data.tokensUsed > 0 && (
                    <>{data.tokensUsed >= 1000 ? `${(data.tokensUsed / 1000).toFixed(0)}K` : data.tokensUsed} tokens</>
                  )}
                </span>
                <button
                  onClick={(e) => { e.stopPropagation(); refresh() }}
                  className="glass-button px-2.5 py-1 rounded-md text-[11px]"
                  style={{ color: 'var(--text-secondary)' }}
                  disabled={loading}
                >
                  {loading ? 'Refreshing...' : 'Refresh'}
                </button>
              </div>
            </>
          )}

          {data && !hasData && data.narrative && (
            <p className="text-xs" style={{ color: 'var(--text-tertiary)' }}>
              {data.narrative}
            </p>
          )}
        </div>
      )}
    </div>
  )
}
