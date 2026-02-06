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
        <circle
          cx="70" cy="70" r={radius}
          fill="none"
          stroke="currentColor"
          strokeWidth="10"
          className="text-gray-200 dark:text-gray-700"
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
        />
        <text
          x="70" y="70"
          textAnchor="middle"
          dominantBaseline="central"
          className="fill-gray-900 dark:fill-gray-100 text-2xl font-bold"
          fontSize="28"
          fontWeight="bold"
        >
          {Math.round(score)}%
        </text>
      </svg>
      <span className="text-sm text-gray-500 dark:text-gray-400 mt-1">Health Score</span>
    </div>
  )
}
