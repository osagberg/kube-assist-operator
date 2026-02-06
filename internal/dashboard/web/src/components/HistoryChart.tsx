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
    <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
      <h3 className="text-sm font-semibold mb-3 text-gray-600 dark:text-gray-300">Health Score History</h3>
      <ResponsiveContainer width="100%" height={180}>
        <LineChart data={chartData}>
          <CartesianGrid strokeDasharray="3 3" stroke="currentColor" className="text-gray-200 dark:text-gray-700" />
          <XAxis dataKey="time" tick={{ fontSize: 11 }} stroke="#9CA3AF" />
          <YAxis domain={[0, 100]} tick={{ fontSize: 11 }} stroke="#9CA3AF" />
          <Tooltip
            contentStyle={{
              backgroundColor: 'var(--tw-bg-opacity, #1F2937)',
              border: 'none',
              borderRadius: '8px',
              fontSize: '12px',
            }}
          />
          <Line
            type="monotone"
            dataKey="score"
            stroke="#4F46E5"
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
