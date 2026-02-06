interface Props {
  score: number
}

export function HealthScoreRing({ score }: Props) {
  const radius = 54
  const circumference = 2 * Math.PI * radius
  const offset = circumference - (score / 100) * circumference
  const color = score >= 80 ? '#10B981' : score >= 50 ? '#F59E0B' : '#EF4444'

  return (
    <div className="flex flex-col items-center">
      <svg width="140" height="140" viewBox="0 0 140 140">
        <defs>
          <filter id="score-glow">
            <feGaussianBlur stdDeviation="3" result="blur" />
            <feMerge>
              <feMergeNode in="blur" />
              <feMergeNode in="SourceGraphic" />
            </feMerge>
          </filter>
        </defs>
        <circle
          cx="70" cy="70" r={radius}
          fill="none"
          stroke="var(--ring-track)"
          strokeWidth="10"
        />
        <circle
          cx="70" cy="70" r={radius}
          fill="none"
          stroke={color}
          strokeWidth="10"
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={offset}
          className="transition-all duration-700 ease-out"
          transform="rotate(-90 70 70)"
          filter="url(#score-glow)"
        />
        <text
          x="70" y="70"
          textAnchor="middle"
          dominantBaseline="central"
          fill="var(--text-primary)"
          className="text-2xl font-bold"
          fontSize="28"
          fontWeight="bold"
        >
          {Math.round(score)}%
        </text>
      </svg>
      <span className="text-sm mt-1" style={{ color: 'var(--text-secondary)' }}>Health Score</span>
    </div>
  )
}
