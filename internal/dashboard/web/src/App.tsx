import { useState, useEffect } from 'react'
import type { HealthUpdate } from './types'
import { fetchHealth } from './api/client'

function App() {
  const [health, setHealth] = useState<HealthUpdate | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [dark, setDark] = useState(() =>
    window.matchMedia('(prefers-color-scheme: dark)').matches
  )

  useEffect(() => {
    document.documentElement.classList.toggle('dark', dark)
  }, [dark])

  useEffect(() => {
    fetchHealth()
      .then(setHealth)
      .catch((e) => setError(e.message))
  }, [])

  return (
    <div className="min-h-screen bg-gray-50 dark:bg-gray-900 text-gray-900 dark:text-gray-100">
      <header className="bg-indigo-600 text-white px-6 py-4 flex items-center justify-between">
        <h1 className="text-xl font-bold">KubeAssist Dashboard</h1>
        <button
          onClick={() => setDark(!dark)}
          className="px-3 py-1 rounded bg-indigo-700 hover:bg-indigo-800 text-sm"
        >
          {dark ? 'Light' : 'Dark'}
        </button>
      </header>

      <main className="max-w-7xl mx-auto px-6 py-8">
        {error && (
          <div className="bg-red-100 dark:bg-red-900 text-red-700 dark:text-red-200 px-4 py-3 rounded mb-6">
            {error}
          </div>
        )}

        {!health && !error && (
          <div className="text-center py-12 text-gray-500">Loading health data...</div>
        )}

        {health && (
          <div className="space-y-6">
            <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
              <SummaryCard label="Health Score" value={`${Math.round(health.summary.totalHealthy / Math.max(health.summary.totalHealthy + health.summary.totalIssues, 1) * 100)}%`} color="indigo" />
              <SummaryCard label="Critical" value={String(health.summary.criticalCount)} color="red" />
              <SummaryCard label="Warnings" value={String(health.summary.warningCount)} color="yellow" />
              <SummaryCard label="Info" value={String(health.summary.infoCount)} color="blue" />
            </div>

            <div className="space-y-4">
              {Object.entries(health.results).map(([name, result]) => (
                <div key={name} className="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
                  <div className="flex items-center justify-between mb-2">
                    <h3 className="font-semibold text-lg capitalize">{name}</h3>
                    <span className="text-sm text-gray-500">
                      {result.healthy} healthy, {result.issues.length} issues
                    </span>
                  </div>
                  {result.error && (
                    <div className="text-red-500 text-sm">{result.error}</div>
                  )}
                  {result.issues.length > 0 && (
                    <ul className="space-y-2 mt-2">
                      {result.issues.map((issue, i) => (
                        <li key={i} className="text-sm border-l-4 pl-3 py-1" style={{
                          borderColor: issue.severity === 'Critical' ? '#EF4444' : issue.severity === 'Warning' ? '#F59E0B' : '#3B82F6'
                        }}>
                          <div className="font-medium">{issue.resource} — {issue.message}</div>
                          {issue.suggestion && (
                            <div className="text-gray-500 dark:text-gray-400 mt-1">{issue.suggestion}</div>
                          )}
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
              ))}
            </div>
          </div>
        )}
      </main>
    </div>
  )
}

function SummaryCard({ label, value, color }: { label: string; value: string; color: string }) {
  const colorMap: Record<string, string> = {
    indigo: 'bg-indigo-100 dark:bg-indigo-900 text-indigo-700 dark:text-indigo-200',
    red: 'bg-red-100 dark:bg-red-900 text-red-700 dark:text-red-200',
    yellow: 'bg-yellow-100 dark:bg-yellow-900 text-yellow-700 dark:text-yellow-200',
    blue: 'bg-blue-100 dark:bg-blue-900 text-blue-700 dark:text-blue-200',
  }
  return (
    <div className={`rounded-lg p-4 ${colorMap[color] ?? ''}`}>
      <div className="text-sm font-medium">{label}</div>
      <div className="text-2xl font-bold">{value}</div>
    </div>
  )
}

export default App
