import { useState, useEffect } from 'react'
import { LineChart, Line, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid } from 'recharts'
import type { HealthSnapshot } from '../types'
import { fetchHealthHistory } from '../api/client'

export function HistoryChart() {
  const [data, setData] = useState<HealthSnapshot[]>([])
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetchHealthHistory({ last: 50 })
      .then((d) => { setData(d); setError(null) })
      .catch((e) => setError(e instanceof Error ? e.message : 'Failed to load history'))
  }, [])

  if (error || data.length === 0) return null

  const chartData = data.map((s) => ({
    time: new Date(s.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
    score: Math.round(s.healthScore),
    issues: s.totalIssues,
  }))

  return (
    <div className="glass-panel rounded-xl p-4 transition-all duration-200">
      <h3 className="text-sm font-semibold mb-3" style={{ color: 'var(--text-secondary)' }}>Health Score History</h3>
      <ResponsiveContainer width="100%" height={180}>
        <LineChart data={chartData}>
          <CartesianGrid strokeDasharray="3 3" stroke="var(--chart-grid)" />
          <XAxis dataKey="time" tick={{ fontSize: 11, fill: 'var(--chart-axis)' }} stroke="var(--chart-grid)" />
          <YAxis domain={[0, 100]} tick={{ fontSize: 11, fill: 'var(--chart-axis)' }} stroke="var(--chart-grid)" />
          <Tooltip
            contentStyle={{
              backgroundColor: 'var(--tooltip-bg)',
              border: '1px solid var(--glass-border)',
              borderRadius: '12px',
              fontSize: '12px',
              backdropFilter: 'blur(20px)',
              WebkitBackdropFilter: 'blur(20px)',
              color: 'var(--text-primary)',
            }}
          />
          <Line
            type="monotone"
            dataKey="score"
            stroke="#6366F1"
            strokeWidth={2}
            dot={false}
            name="Health %"
          />
          <Line
            type="monotone"
            dataKey="issues"
            stroke="#EF4444"
            strokeWidth={1.5}
            dot={false}
            name="Issues"
            strokeDasharray="4 2"
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  )
}
