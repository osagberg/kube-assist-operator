import { useState, useEffect, useRef, useMemo, useCallback } from 'react'
import { useHealth } from './hooks/useHealth'
import { useSSE } from './hooks/useSSE'
import { useSettings } from './hooks/useSettings'
import { triggerCheck } from './api/client'
import { HealthScoreRing } from './components/HealthScoreRing'
import { CheckerCard } from './components/CheckerCard'
import { SeverityTabs } from './components/SeverityTabs'
import type { Severity } from './components/SeverityTabs'
import { SearchBar } from './components/SearchBar'
import { NamespaceFilter } from './components/NamespaceFilter'
import { ExportButton } from './components/ExportButton'
import { SettingsModal } from './components/SettingsModal'
import { HistoryChart } from './components/HistoryChart'
import { CausalTimeline } from './components/CausalTimeline'
import { useKeyboardShortcuts, KeyboardShortcutsHelp } from './components/KeyboardShortcuts'
import { ToastContainer, showToast } from './components/Toast'
import { ErrorBoundary } from './components/ErrorBoundary'

const severityKeys: Severity[] = ['all', 'Critical', 'Warning', 'Info']

function App() {
  const { health, error, loading, refresh, setHealth } = useHealth()
  const [dark, setDark] = useState(() =>
    window.matchMedia('(prefers-color-scheme: dark)').matches
  )
  const [search, setSearch] = useState('')
  const [severity, setSeverity] = useState<Severity>('all')
  const [namespace, setNamespace] = useState('')
  const [paused, setPaused] = useState(false)
  const [showSettings, setShowSettings] = useState(false)
  const [showHelp, setShowHelp] = useState(false)
  const searchRef = useRef<HTMLInputElement>(null)
  const nsRef = useRef<HTMLSelectElement>(null)

  const { settings, save: saveSettings } = useSettings()
  const { data: sseData, connected } = useSSE(paused)

  // Update health from SSE
  useEffect(() => {
    if (sseData) setHealth(sseData)
  }, [sseData, setHealth])

  useEffect(() => {
    document.documentElement.classList.toggle('dark', dark)
  }, [dark])

  const namespaces = useMemo(() => {
    if (!health) return []
    return health.namespaces ?? []
  }, [health])

  const healthScore = useMemo(() => {
    if (!health) return 100
    const total = health.summary.totalHealthy + health.summary.totalIssues
    return total === 0 ? 100 : (health.summary.totalHealthy / total) * 100
  }, [health])

  const handleTriggerCheck = async () => {
    try {
      await triggerCheck()
      showToast('Health check triggered', 'success')
      setTimeout(refresh, 2000)
    } catch {
      showToast('Failed to trigger check', 'error')
    }
  }

  const toggleTheme = useCallback(() => setDark((d) => !d), [])
  const togglePause = useCallback(() => {
    setPaused((p) => {
      showToast(p ? 'Live updates resumed' : 'Live updates paused', 'info')
      return !p
    })
  }, [])

  useKeyboardShortcuts({
    onToggleTheme: toggleTheme,
    onRefresh: handleTriggerCheck,
    onFocusSearch: () => searchRef.current?.focus(),
    onFocusNamespace: () => nsRef.current?.focus(),
    onTogglePause: togglePause,
    onSeverity: (i) => setSeverity(severityKeys[i] ?? 'all'),
    onShowHelp: () => setShowHelp(true),
  })

  return (
    <ErrorBoundary>
    <div className="min-h-screen bg-gray-50 dark:bg-gray-900 text-gray-900 dark:text-gray-100">
      {/* Header */}
      <header className="bg-gradient-to-r from-indigo-600 to-purple-500 text-white px-6 py-4 shadow-lg">
        <div className="max-w-7xl mx-auto flex items-center justify-between">
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-bold">KubeAssist</h1>
            <span className="text-xs bg-white/20 px-2 py-0.5 rounded-full">Dashboard</span>
            <span className={`w-2 h-2 rounded-full ${connected ? 'bg-green-400 animate-pulse' : paused ? 'bg-yellow-400' : 'bg-red-400'}`} title={connected ? 'Connected' : paused ? 'Paused' : 'Disconnected'} role="status" aria-label={connected ? 'Connected to server' : paused ? 'Updates paused' : 'Disconnected from server'} />
          </div>
          <div className="flex items-center gap-2">
            <button onClick={togglePause} className="px-3 py-1.5 rounded-md bg-white/20 hover:bg-white/30 text-sm transition-colors" aria-label={paused ? 'Resume live updates' : 'Pause live updates'}>
              {paused ? 'Resume' : 'Pause'}
            </button>
            <button onClick={handleTriggerCheck} className="px-3 py-1.5 rounded-md bg-white/20 hover:bg-white/30 text-sm transition-colors" aria-label="Refresh health data">
              Refresh
            </button>
            <button onClick={() => setShowSettings(true)} className="px-3 py-1.5 rounded-md bg-white/20 hover:bg-white/30 text-sm transition-colors" aria-label="Open AI settings">
              AI Settings
            </button>
            <button onClick={toggleTheme} className="px-3 py-1.5 rounded-md bg-white/20 hover:bg-white/30 text-sm transition-colors" aria-label={dark ? 'Switch to light theme' : 'Switch to dark theme'}>
              {dark ? 'Light' : 'Dark'}
            </button>
            <button onClick={() => setShowHelp(true)} className="px-2 py-1.5 rounded-md bg-white/20 hover:bg-white/30 text-sm transition-colors font-mono" aria-label="Show keyboard shortcuts">
              ?
            </button>
          </div>
        </div>
      </header>

      <main className="max-w-7xl mx-auto px-6 py-8">
        {error && (
          <div className="bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300 px-4 py-3 rounded-lg mb-6 border border-red-200 dark:border-red-800">
            {error}
          </div>
        )}

        {loading && !health && (
          <div className="flex items-center justify-center py-20">
            <div className="w-8 h-8 border-4 border-indigo-200 border-t-indigo-600 rounded-full animate-spin" />
          </div>
        )}

        {health && (
          <div className="space-y-6">
            {/* Summary Row */}
            <div className="flex flex-col md:flex-row items-start gap-6">
              <HealthScoreRing score={healthScore} />
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4 flex-1 w-full">
                <MetricCard label="Healthy" value={health.summary.totalHealthy} color="green" />
                <MetricCard label="Critical" value={health.summary.criticalCount} color="red" />
                <MetricCard label="Warnings" value={health.summary.warningCount} color="yellow" />
                <MetricCard label="Info" value={health.summary.infoCount} color="blue" />
              </div>
            </div>

            {/* History Chart */}
            <HistoryChart />

            {/* Causal Analysis */}
            <CausalTimeline />

            {/* Filters */}
            <div className="flex flex-col md:flex-row gap-3 items-start md:items-center justify-between">
              <SeverityTabs active={severity} onChange={setSeverity} summary={health.summary} />
              <div className="flex gap-2 items-center flex-wrap">
                <SearchBar value={search} onChange={setSearch} inputRef={searchRef} />
                <NamespaceFilter namespaces={namespaces} selected={namespace} onChange={setNamespace} selectRef={nsRef} />
                <ExportButton health={health} />
              </div>
            </div>

            {/* Checker Cards */}
            <div className="space-y-4">
              {Object.entries(health.results)
                .sort(([, a], [, b]) => b.issues.length - a.issues.length)
                .map(([name, result]) => (
                  <CheckerCard
                    key={name}
                    name={name}
                    result={result}
                    search={search}
                    severity={severity}
                    namespace={namespace}
                  />
                ))}
            </div>

            {/* Timestamp */}
            <div className="text-center text-xs text-gray-400 pt-4">
              Last updated: {new Date(health.timestamp).toLocaleString()}
              {paused && <span className="ml-2 text-yellow-500">(updates paused)</span>}
            </div>
          </div>
        )}
      </main>

      {/* Modals */}
      <SettingsModal
        open={showSettings}
        onClose={() => setShowSettings(false)}
        settings={settings}
        onSave={async (req) => {
          const result = await saveSettings(req)
          showToast('AI settings saved', 'success')
          return result
        }}
      />
      <KeyboardShortcutsHelp open={showHelp} onClose={() => setShowHelp(false)} />
      <ToastContainer />
    </div>
    </ErrorBoundary>
  )
}

function MetricCard({ label, value, color }: { label: string; value: number; color: string }) {
  const styles: Record<string, string> = {
    green: 'border-l-green-500 text-green-600 dark:text-green-400',
    red: 'border-l-red-500 text-red-600 dark:text-red-400',
    yellow: 'border-l-yellow-500 text-yellow-600 dark:text-yellow-400',
    blue: 'border-l-blue-500 text-blue-600 dark:text-blue-400',
  }
  return (
    <div className={`bg-white dark:bg-gray-800 rounded-lg p-4 border-l-4 shadow-sm ${styles[color]}`}>
      <div className="text-sm text-gray-500 dark:text-gray-400">{label}</div>
      <div className="text-2xl font-bold">{value}</div>
    </div>
  )
}

export default App
