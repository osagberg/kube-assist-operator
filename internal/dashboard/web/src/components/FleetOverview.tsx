import type { FleetClusterEntry } from '../types'
import { HealthScoreRing } from './HealthScoreRing'

interface Props {
  clusters: FleetClusterEntry[]
  onDrillDown: (clusterId: string) => void
}

export function FleetOverview({ clusters, onDrillDown }: Props) {
  if (clusters.length === 0) {
    return (
      <div className="glass-panel rounded-xl p-8 text-center">
        <p className="text-sm" style={{ color: 'var(--text-tertiary)' }}>
          No cluster data available yet. Waiting for health checks...
        </p>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <h2 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>Fleet Overview</h2>
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {clusters.map((entry) => (
          <FleetCard key={entry.clusterId} entry={entry} onClick={() => onDrillDown(entry.clusterId)} />
        ))}
      </div>
    </div>
  )
}

function FleetCard({ entry, onClick }: { entry: FleetClusterEntry; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className="glass-panel rounded-xl p-5 text-left w-full transition-all duration-200 hover:ring-1 hover:ring-accent/30 cursor-pointer"
    >
      <div className="flex items-center gap-4">
        <HealthScoreRing score={entry.healthScore} size="sm" />
        <div className="flex-1 min-w-0">
          <div className="text-sm font-semibold truncate" style={{ color: 'var(--text-primary)' }}>
            {entry.clusterId}
          </div>
          <div className="flex items-center gap-2 mt-1.5 flex-wrap">
            {entry.criticalCount > 0 && (
              <span className="severity-pill-critical">{entry.criticalCount} CR</span>
            )}
            {entry.warningCount > 0 && (
              <span className="severity-pill-warning">{entry.warningCount} WR</span>
            )}
            {entry.infoCount > 0 && (
              <span className="severity-pill-info">{entry.infoCount} IN</span>
            )}
            {entry.totalIssues === 0 && (
              <span className="severity-pill-healthy">OK</span>
            )}
          </div>
          <div className="text-[10px] mt-1.5" style={{ color: 'var(--text-tertiary)' }}>
            {new Date(entry.lastUpdated).toLocaleTimeString()}
          </div>
        </div>
      </div>
    </button>
  )
}
