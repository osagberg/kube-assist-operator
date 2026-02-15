import { useId } from 'react'

interface Props {
  score: number
  size?: 'sm' | 'md'
}

export function HealthScoreRing({ score, size = 'md' }: Props) {
  const isSmall = size === 'sm'
  const svgSize = isSmall ? 64 : 140
  const center = svgSize / 2
  const radius = isSmall ? 24 : 54
  const strokeWidth = isSmall ? 5 : 10
  const fontSize = isSmall ? 14 : 28
  const circumference = 2 * Math.PI * radius
  const offset = circumference - (score / 100) * circumference
  const color = score >= 80 ? '#10B981' : score >= 50 ? '#F59E0B' : '#EF4444'
  const filterId = useId()

  return (
    <div className="flex flex-col items-center">
      <svg width={svgSize} height={svgSize} viewBox={`0 0 ${svgSize} ${svgSize}`}>
        <defs>
          <filter id={filterId}>
            <feGaussianBlur stdDeviation={isSmall ? 1.5 : 3} result="blur" />
            <feMerge>
              <feMergeNode in="blur" />
              <feMergeNode in="SourceGraphic" />
            </feMerge>
          </filter>
        </defs>
        <circle
          cx={center} cy={center} r={radius}
          fill="none"
          stroke="var(--ring-track)"
          strokeWidth={strokeWidth}
        />
        <circle
          cx={center} cy={center} r={radius}
          fill="none"
          stroke={color}
          strokeWidth={strokeWidth}
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={offset}
          className="transition-all duration-700 ease-out"
          transform={`rotate(-90 ${center} ${center})`}
          filter={`url(#${filterId})`}
        />
        <text
          x={center} y={center}
          textAnchor="middle"
          dominantBaseline="central"
          fill="var(--text-primary)"
          fontSize={fontSize}
          fontWeight="bold"
        >
          {Math.round(score)}%
        </text>
      </svg>
      {!isSmall && (
        <span className="text-sm mt-1" style={{ color: 'var(--text-secondary)' }}>Health Score</span>
      )}
    </div>
  )
}
