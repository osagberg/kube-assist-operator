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
import { ClusterExplain } from './components/ClusterExplain'
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
  const [menuOpen, setMenuOpen] = useState(false)
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
    document.documentElement.classList.toggle('light', !dark)
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
    <div className="min-h-screen" style={{ color: 'var(--text-primary)' }}>
      {/* Header */}
      <header className="glass-panel sticky top-0 z-40 px-6 py-3">
        <div className="max-w-7xl mx-auto flex items-center justify-between">
          <div className="flex items-center gap-3">
            <h1 className="text-lg font-semibold tracking-tight" style={{ color: 'var(--text-primary)' }}>KubeAssist</h1>
            <span className="text-[11px] font-medium px-2.5 py-0.5 rounded-full bg-accent-muted text-accent">Dashboard</span>
            <span className={`w-2 h-2 rounded-full ${connected ? 'severity-dot-healthy' : paused ? 'severity-dot-warning' : 'severity-dot-critical'}`} title={connected ? 'Connected' : paused ? 'Paused' : 'Disconnected'} role="status" aria-label={connected ? 'Connected to server' : paused ? 'Updates paused' : 'Disconnected from server'} />
          </div>
          {/* Desktop buttons */}
          <div className="hidden md:flex items-center gap-2">
            <button onClick={togglePause} className="glass-button px-3 py-1.5 rounded-lg text-sm" style={{ color: 'var(--text-secondary)' }} aria-label={paused ? 'Resume live updates' : 'Pause live updates'}>
              {paused ? 'Resume' : 'Pause'}
            </button>
            <button onClick={handleTriggerCheck} className="glass-button px-3 py-1.5 rounded-lg text-sm" style={{ color: 'var(--text-secondary)' }} aria-label="Refresh health data">
              Refresh
            </button>
            <button onClick={() => setShowSettings(true)} className="glass-button px-3 py-1.5 rounded-lg text-sm" style={{ color: 'var(--text-secondary)' }} aria-label="Open AI settings">
              AI Settings
            </button>
            <button onClick={toggleTheme} className="glass-button px-3 py-1.5 rounded-lg text-sm" style={{ color: 'var(--text-secondary)' }} aria-label={dark ? 'Switch to light theme' : 'Switch to dark theme'}>
              {dark ? 'Light' : 'Dark'}
            </button>
            <button onClick={() => setShowHelp(true)} className="glass-button px-2.5 py-1.5 rounded-lg text-sm font-mono" style={{ color: 'var(--text-secondary)' }} aria-label="Show keyboard shortcuts">
              ?
            </button>
          </div>
          {/* Mobile buttons */}
          <div className="flex md:hidden items-center gap-2">
            <button onClick={toggleTheme} className="glass-button px-3 py-1.5 rounded-lg text-sm" style={{ color: 'var(--text-secondary)' }} aria-label={dark ? 'Switch to light theme' : 'Switch to dark theme'}>
              {dark ? 'Light' : 'Dark'}
            </button>
            <button onClick={() => setMenuOpen((o) => !o)} className="glass-button px-2.5 py-1.5 rounded-lg text-sm font-mono" style={{ color: 'var(--text-secondary)' }} aria-label="Open menu">
              ...
            </button>
          </div>
          {menuOpen && (
            <>
              <div className="fixed inset-0 z-40" onClick={() => setMenuOpen(false)} />
              <div className="glass-elevated absolute right-4 top-14 rounded-xl p-2 flex flex-col gap-1 z-50">
                <button onClick={() => { togglePause(); setMenuOpen(false) }} className="glass-button px-3 py-1.5 rounded-lg text-sm w-full text-left" style={{ color: 'var(--text-secondary)' }} aria-label={paused ? 'Resume live updates' : 'Pause live updates'}>
                  {paused ? 'Resume' : 'Pause'}
                </button>
                <button onClick={() => { handleTriggerCheck(); setMenuOpen(false) }} className="glass-button px-3 py-1.5 rounded-lg text-sm w-full text-left" style={{ color: 'var(--text-secondary)' }} aria-label="Refresh health data">
                  Refresh
                </button>
                <button onClick={() => { setShowSettings(true); setMenuOpen(false) }} className="glass-button px-3 py-1.5 rounded-lg text-sm w-full text-left" style={{ color: 'var(--text-secondary)' }} aria-label="Open AI settings">
                  AI Settings
                </button>
                <button onClick={() => { setShowHelp(true); setMenuOpen(false) }} className="glass-button px-2.5 py-1.5 rounded-lg text-sm font-mono w-full text-left" style={{ color: 'var(--text-secondary)' }} aria-label="Show keyboard shortcuts">
                  ?
                </button>
              </div>
            </>
          )}
        </div>
      </header>

      {/* AI Status Bar */}
      {health?.aiStatus && (
        <div className="glass-panel mx-6 mt-3 rounded-xl px-4 py-2 max-w-7xl lg:mx-auto">
          <div className="flex items-center gap-2 text-xs font-medium">
            <span className={
              health.aiStatus.lastError ? 'severity-pill-critical' :
              health.aiStatus.pending ? 'severity-pill-warning' :
              health.aiStatus.issuesEnhanced > 0 ? 'severity-pill-healthy' :
              'severity-pill-info'
            }>
              {health.aiStatus.lastError ? 'CR' :
               health.aiStatus.pending ? 'WR' :
               health.aiStatus.issuesEnhanced > 0 ? 'OK' :
               'IN'}
            </span>
            <span style={{ color: 'var(--text-secondary)' }}>AI ({health.aiStatus.provider}):</span>
            {health.aiStatus.lastError ? (
              <span className="text-severity-critical">Error: {health.aiStatus.lastError}</span>
            ) : health.aiStatus.cacheHit ? (
              <span style={{ color: 'var(--text-secondary)' }}>Using cached results (no API call) — {health.aiStatus.issuesEnhanced} issues enhanced</span>
            ) : health.aiStatus.issuesEnhanced > 0 ? (
              <span style={{ color: 'var(--text-secondary)' }}>
                {health.aiStatus.issuesEnhanced} issues enhanced
                {health.aiStatus.issuesCapped && health.aiStatus.totalIssueCount
                  ? ` of ${health.aiStatus.totalIssueCount}`
                  : ''}
                {' '}({health.aiStatus.tokensUsed >= 1000 ? `${(health.aiStatus.tokensUsed / 1000).toFixed(0)}K` : health.aiStatus.tokensUsed} tokens
                {health.aiStatus.estimatedCostUsd ? `, ~$${health.aiStatus.estimatedCostUsd.toFixed(4)}` : ''})
                {health.aiStatus.issuesCapped ? ' (capped)' : ''}
              </span>
            ) : (
              <span style={{ color: 'var(--text-secondary)' }}>Enabled — waiting for results</span>
            )}
          </div>
          {health.aiStatus.checkPhase && health.aiStatus.checkPhase !== 'done' && (
            <PipelineIndicator phase={health.aiStatus.checkPhase} />
          )}
        </div>
      )}

      <main className="max-w-7xl mx-auto px-6 py-8">
        {error && (
          <div className="glass-panel rounded-xl mb-6 px-4 py-3 border-severity-critical-border" style={{ background: 'rgba(239, 68, 68, 0.08)' }}>
            <span className="text-severity-critical text-sm">{error}</span>
          </div>
        )}

        {loading && !health && (
          <div className="flex items-center justify-center py-20">
            <div className="w-8 h-8 border-2 border-accent-muted border-t-accent rounded-full animate-spin" />
          </div>
        )}

        {health && (
          <div className="space-y-6">
            {/* Summary Row */}
            <div className="flex flex-col md:flex-row items-start gap-6">
              <HealthScoreRing score={healthScore} />
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4 flex-1 w-full">
                <MetricCard label="Healthy" value={health.summary.totalHealthy} severity="healthy" />
                <MetricCard label="Critical" value={health.summary.criticalCount} severity="critical" />
                <MetricCard label="Warnings" value={health.summary.warningCount} severity="warning" />
                <MetricCard label="Info" value={health.summary.infoCount} severity="info" />
              </div>
            </div>

            {/* Explain This Cluster */}
            <ClusterExplain />

            {/* History Chart */}
            <HistoryChart />

            {/* Causal Analysis */}
            <CausalTimeline />

            {/* Filters */}
            <div className="sticky top-14 z-30 glass-panel -mx-6 px-6 py-3 rounded-none">
            <div className="flex flex-col md:flex-row gap-3 items-start md:items-center justify-between">
              <SeverityTabs active={severity} onChange={setSeverity} summary={health.summary} />
              <div className="flex gap-2 items-center flex-wrap">
                <SearchBar value={search} onChange={setSearch} inputRef={searchRef} />
                <NamespaceFilter namespaces={namespaces} selected={namespace} onChange={setNamespace} selectRef={nsRef} />
                <ExportButton health={health} />
              </div>
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
            <div className="text-center text-xs pt-4" style={{ color: 'var(--text-tertiary)' }}>
              Last updated: {new Date(health.timestamp).toLocaleString()}
              {paused && <span className="ml-2 text-severity-warning">(updates paused)</span>}
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

const pipelineStages = ['checkers', 'causal', 'ai'] as const
const stageLabels: Record<string, string> = { checkers: 'Checkers', causal: 'Causal', ai: 'AI' }

function PipelineIndicator({ phase }: { phase: string }) {
  const activeIdx = pipelineStages.indexOf(phase as typeof pipelineStages[number])
  return (
    <div className="flex items-center gap-1.5 mt-2">
      {pipelineStages.map((stage, i) => {
        const done = i < activeIdx
        const active = i === activeIdx
        return (
          <div key={stage} className="flex items-center gap-1.5">
            {i > 0 && (
              <svg className="w-3 h-3" style={{ color: done ? 'var(--text-secondary)' : 'var(--text-tertiary)' }} fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
              </svg>
            )}
            <span
              className={`px-2 py-0.5 rounded-md text-[10px] font-semibold transition-all duration-300 ${
                done ? 'glass-inset text-accent' :
                active ? 'bg-accent/20 text-accent border border-accent/30' :
                'glass-inset'
              }`}
              style={!done && !active ? { color: 'var(--text-tertiary)' } : undefined}
            >
              {done ? '✓ ' : active ? '⏳ ' : ''}{stageLabels[stage]}
            </span>
          </div>
        )
      })}
    </div>
  )
}

function MetricCard({ label, value, severity }: { label: string; value: number; severity: string }) {
  const pillClass = `severity-pill-${severity}`
  const pillLabels: Record<string, string> = { healthy: 'OK', critical: 'CR', warning: 'WR', info: 'IN' }
  const pillText = pillLabels[severity] ?? '??'
  return (
    <div className="glass-panel rounded-xl p-4 transition-all duration-200">
      <div className="flex items-center gap-2 mb-2">
        <span className={pillClass}>{pillText}</span>
        <span className="text-sm" style={{ color: 'var(--text-secondary)' }}>{label}</span>
      </div>
      <div className="text-2xl font-bold" style={{ color: 'var(--text-primary)' }}>{value}</div>
    </div>
  )
}

export default App
