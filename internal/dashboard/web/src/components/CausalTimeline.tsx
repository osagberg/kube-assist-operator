import { useState } from 'react'
import { useCausal } from '../hooks/useCausal'
import { CausalGroupCard } from './CausalGroup'

export function CausalTimeline({ clusterId }: { clusterId?: string }) {
  const { data, loading, error } = useCausal(clusterId)
  const [collapsed, setCollapsed] = useState(false)
  const groups = data?.groups ?? []

  if (loading) {
    return (
      <div className="glass-panel rounded-xl p-6">
        <h2 className="text-sm font-semibold mb-4" style={{ color: 'var(--text-secondary)' }}>Causal Analysis</h2>
        <div className="flex items-center justify-center py-8">
          <div className="w-5 h-5 border-2 border-accent-muted border-t-accent rounded-full animate-spin" />
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="glass-panel rounded-xl p-6">
        <h2 className="text-sm font-semibold mb-4" style={{ color: 'var(--text-secondary)' }}>Causal Analysis</h2>
        <div className="text-xs text-severity-critical">{error}</div>
      </div>
    )
  }

  if (!data || groups.length === 0) {
    return (
      <div className="glass-panel rounded-xl p-6">
        <h2 className="text-sm font-semibold mb-4" style={{ color: 'var(--text-secondary)' }}>Causal Analysis</h2>
        <div className="text-xs text-center py-4" style={{ color: 'var(--text-tertiary)' }}>
          No correlated issue groups detected. Issues are independent.
        </div>
      </div>
    )
  }

  const aiGroupCount = groups.filter((g) => g.aiEnhanced).length

  return (
    <div className="glass-panel rounded-xl p-6">
      <button
        className="w-full flex items-center justify-between"
        onClick={() => setCollapsed((c) => !c)}
        aria-expanded={!collapsed}
      >
        <h2 className="text-sm font-semibold" style={{ color: 'var(--text-secondary)' }}>Causal Analysis</h2>
        <div className="flex items-center gap-3 text-xs" style={{ color: 'var(--text-tertiary)' }}>
          <span>{groups.length} group{groups.length !== 1 ? 's' : ''}</span>
          <span>{data.totalIssues} total issues</span>
          {data.uncorrelatedCount > 0 && (
            <span>{data.uncorrelatedCount} uncorrelated</span>
          )}
          {aiGroupCount > 0 && (
            <span className="text-ai font-medium">
              {aiGroupCount} AI-enhanced
            </span>
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
        <div className="space-y-3 mt-4">
          {groups.map((group) => (
            <CausalGroupCard key={group.id} group={group} />
          ))}
        </div>
      )}
    </div>
  )
}
