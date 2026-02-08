interface Props {
  clusters: string[]
  selected: string
  onChange: (cluster: string) => void
}

export function ClusterSelector({ clusters, selected, onChange }: Props) {
  if (clusters.length === 0) return null

  return (
    <select
      value={selected}
      onChange={(e) => onChange(e.target.value)}
      aria-label="Select cluster"
      className="px-3 py-2 glass-inset rounded-xl text-sm focus:outline-none focus:ring-2 focus:ring-accent/60 transition-all duration-200"
      style={{ color: 'var(--text-primary)' }}
    >
      {clusters.length > 1 && (
        <option value="">Fleet Overview</option>
      )}
      {clusters.map((c) => (
        <option key={c} value={c}>{c}</option>
      ))}
    </select>
  )
}
