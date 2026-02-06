import type { Summary } from '../types'

type Severity = 'all' | 'Critical' | 'Warning' | 'Info'

interface Props {
  active: Severity
  onChange: (s: Severity) => void
  summary: Summary
}

const tabs: { key: Severity; label: string; activeBadge: string; }[] = [
  { key: 'all', label: 'All', activeBadge: 'bg-accent-muted text-accent' },
  { key: 'Critical', label: 'Critical', activeBadge: 'bg-severity-critical-bg text-severity-critical' },
  { key: 'Warning', label: 'Warning', activeBadge: 'bg-severity-warning-bg text-severity-warning' },
  { key: 'Info', label: 'Info', activeBadge: 'bg-severity-info-bg text-severity-info' },
]

function getCount(summary: Summary, key: Severity): number {
  if (key === 'all') return summary.totalIssues
  if (key === 'Critical') return summary.criticalCount
  if (key === 'Warning') return summary.warningCount
  return summary.infoCount
}

export function SeverityTabs({ active, onChange, summary }: Props) {
  return (
    <div className="flex gap-1 glass-inset rounded-xl p-1">
      {tabs.map((tab) => {
        const count = getCount(summary, tab.key)
        const isActive = active === tab.key
        return (
          <button
            key={tab.key}
            onClick={() => onChange(tab.key)}
            className={`flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm font-medium transition-all duration-200 ${
              isActive
                ? 'glass-panel shadow-glass-sm'
                : 'hover:bg-glass-200'
            }`}
            style={{ color: isActive ? 'var(--text-primary)' : 'var(--text-tertiary)' }}
          >
            {tab.label}
            <span className={`text-xs px-1.5 py-0.5 rounded-full ${isActive ? tab.activeBadge : 'bg-glass-200'}`}
              style={isActive ? undefined : { color: 'var(--text-tertiary)' }}
            >
              {count}
            </span>
          </button>
        )
      })}
    </div>
  )
}

export type { Severity }
