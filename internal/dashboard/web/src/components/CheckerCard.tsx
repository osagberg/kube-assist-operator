import { useState } from 'react'
import type { CheckResult } from '../types'
import { IssueRow } from './IssueRow'
import type { Severity } from './SeverityTabs'

interface Props {
  name: string
  result: CheckResult
  search: string
  severity: Severity
  namespace: string
}

export function CheckerCard({ name, result, search, severity, namespace }: Props) {
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

  return (
    <div className={`bg-white dark:bg-gray-800 rounded-lg shadow border ${
      hasCritical ? 'border-red-300 dark:border-red-800' :
      hasWarning ? 'border-yellow-300 dark:border-yellow-800' :
      'border-gray-200 dark:border-gray-700'
    }`}>
      <button
        onClick={() => setCollapsed(!collapsed)}
        aria-expanded={!collapsed}
        className="w-full flex items-center justify-between p-4 text-left hover:bg-gray-50 dark:hover:bg-gray-750 rounded-t-lg"
      >
        <div className="flex items-center gap-3">
          <svg className="w-4 h-4 text-indigo-500 transition-transform" style={{ transform: collapsed ? 'rotate(-90deg)' : '' }} fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
          </svg>
          <h3 className="font-semibold text-lg capitalize">{name}</h3>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-sm text-green-600 dark:text-green-400 bg-green-50 dark:bg-green-900/30 px-2 py-0.5 rounded">
            {result.healthy} healthy
          </span>
          {result.issues.length > 0 && (
            <span className={`text-sm px-2 py-0.5 rounded ${
              hasCritical ? 'text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/30' :
              hasWarning ? 'text-yellow-600 dark:text-yellow-400 bg-yellow-50 dark:bg-yellow-900/30' :
              'text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-900/30'
            }`}>
              {result.issues.length} issues
            </span>
          )}
        </div>
      </button>

      {result.error && (
        <div className="px-4 pb-2 text-red-500 text-sm">{result.error}</div>
      )}

      {!collapsed && filteredIssues.length > 0 && (
        <div className="border-t border-gray-100 dark:border-gray-700">
          {filteredIssues.map((issue) => (
            <IssueRow key={`${issue.namespace}-${issue.resource}-${issue.type}`} issue={issue} />
          ))}
        </div>
      )}

      {!collapsed && filteredIssues.length === 0 && result.issues.length > 0 && (
        <div className="px-4 py-3 text-sm text-gray-400 border-t border-gray-100 dark:border-gray-700">
          No issues match filters
        </div>
      )}
    </div>
  )
}
