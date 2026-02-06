import { useState, useEffect } from 'react'
import { LineChart, Line, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid } from 'recharts'
import type { HealthSnapshot, PredictionResult } from '../types'
import { fetchHealthHistory, fetchPrediction } from '../api/client'

const trendArrows: Record<string, string> = {
  improving: '\u2197',
  stable: '\u2192',
  degrading: '\u2198',
}

const trendColors: Record<string, string> = {
  improving: 'severity-pill-healthy',
  stable: 'severity-pill-info',
  degrading: 'severity-pill-critical',
}

export function HistoryChart() {
  const [data, setData] = useState<HealthSnapshot[]>([])
  const [prediction, setPrediction] = useState<PredictionResult | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetchHealthHistory({ last: 50 })
      .then((d) => { setData(d); setError(null) })
      .catch((e) => setError(e instanceof Error ? e.message : 'Failed to load history'))

    fetchPrediction()
      .then(setPrediction)
      .catch(() => setPrediction(null))  // silently fail, prediction is optional
  }, [])

  if (error || data.length === 0) return null

  const chartData = data.map((s) => ({
    time: new Date(s.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
    score: Math.round(s.healthScore),
    issues: s.totalIssues,
    projected: undefined as number | undefined,
  }))

  // Add projected data point if prediction is available
  if (prediction) {
    const now = new Date()
    const projectedTime = new Date(now.getTime() + 60 * 60 * 1000) // 1 hour ahead
    chartData.push({
      time: projectedTime.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
      score: undefined as unknown as number,  // no actual score at this future time
      issues: undefined as unknown as number,
      projected: Math.round(prediction.projectedScore),
    })

    // Also set the projected value on the last real data point so the line connects
    if (chartData.length >= 2) {
      chartData[chartData.length - 2].projected = chartData[chartData.length - 2].score
    }
  }

  return (
    <div className="glass-panel rounded-xl p-4 transition-all duration-200">
      <div className="flex items-center justify-between mb-3">
        <h3 className="text-sm font-semibold" style={{ color: 'var(--text-secondary)' }}>Health Score History</h3>
        {prediction && (
          <div className="flex items-center gap-2">
            <span className={trendColors[prediction.trendDirection] || 'severity-pill-info'}>
              {trendArrows[prediction.trendDirection] || '?'} {prediction.trendDirection}
            </span>
            <span className="text-[10px]" style={{ color: 'var(--text-tertiary)' }}>
              {prediction.velocity > 0 ? '+' : ''}{prediction.velocity}/hr {'\u2192'} {prediction.projectedScore}%
            </span>
          </div>
        )}
      </div>
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
            connectNulls={false}
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
          {prediction && (
            <Line
              type="monotone"
              dataKey="projected"
              stroke="#6366F1"
              strokeWidth={2}
              strokeDasharray="6 4"
              dot={{ r: 4, fill: '#6366F1', strokeWidth: 0 }}
              name="Projected"
              connectNulls={false}
            />
          )}
        </LineChart>
      </ResponsiveContainer>
    </div>
  )
}
