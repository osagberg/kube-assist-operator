import type { Summary } from '../types'

type Severity = 'all' | 'Critical' | 'Warning' | 'Info'

interface Props {
  active: Severity
  onChange: (s: Severity) => void
  summary: Summary
}

const tabs: { key: Severity; label: string; color: string }[] = [
  { key: 'all', label: 'All', color: 'bg-indigo-600' },
  { key: 'Critical', label: 'Critical', color: 'bg-red-500' },
  { key: 'Warning', label: 'Warning', color: 'bg-yellow-500' },
  { key: 'Info', label: 'Info', color: 'bg-blue-500' },
]

function getCount(summary: Summary, key: Severity): number {
  if (key === 'all') return summary.totalIssues
  if (key === 'Critical') return summary.criticalCount
  if (key === 'Warning') return summary.warningCount
  return summary.infoCount
}

export function SeverityTabs({ active, onChange, summary }: Props) {
  return (
    <div className="flex gap-1 bg-gray-100 dark:bg-gray-800 rounded-lg p-1">
      {tabs.map((tab) => {
        const count = getCount(summary, tab.key)
        const isActive = active === tab.key
        return (
          <button
            key={tab.key}
            onClick={() => onChange(tab.key)}
            className={`flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm font-medium transition-colors ${
              isActive
                ? 'bg-white dark:bg-gray-700 shadow-sm text-gray-900 dark:text-gray-100'
                : 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'
            }`}
          >
            {tab.label}
            <span className={`text-xs px-1.5 py-0.5 rounded-full text-white ${isActive ? tab.color : 'bg-gray-300 dark:bg-gray-600'}`}>
              {count}
            </span>
          </button>
        )
      })}
    </div>
  )
}

export type { Severity }
