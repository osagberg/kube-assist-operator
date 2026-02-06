import { useCausal } from '../hooks/useCausal'
import { CausalGroupCard } from './CausalGroup'

export function CausalTimeline() {
  const { data, loading, error } = useCausal()

  if (loading) {
    return (
      <div className="bg-white dark:bg-gray-800 rounded-xl p-6 shadow-sm">
        <h2 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-4">Causal Analysis</h2>
        <div className="flex items-center justify-center py-8">
          <div className="w-5 h-5 border-2 border-indigo-200 border-t-indigo-600 rounded-full animate-spin" />
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="bg-white dark:bg-gray-800 rounded-xl p-6 shadow-sm">
        <h2 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-4">Causal Analysis</h2>
        <div className="text-xs text-red-500">{error}</div>
      </div>
    )
  }

  if (!data || data.groups.length === 0) {
    return (
      <div className="bg-white dark:bg-gray-800 rounded-xl p-6 shadow-sm">
        <h2 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-4">Causal Analysis</h2>
        <div className="text-xs text-gray-400 text-center py-4">
          No correlated issue groups detected. Issues are independent.
        </div>
      </div>
    )
  }

  return (
    <div className="bg-white dark:bg-gray-800 rounded-xl p-6 shadow-sm">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-sm font-semibold text-gray-700 dark:text-gray-300">Causal Analysis</h2>
        <div className="flex items-center gap-3 text-xs text-gray-500 dark:text-gray-400">
          <span>{data.groups.length} group{data.groups.length !== 1 ? 's' : ''}</span>
          <span>{data.totalIssues} total issues</span>
          {data.uncorrelatedCount > 0 && (
            <span>{data.uncorrelatedCount} uncorrelated</span>
          )}
        </div>
      </div>
      <div className="space-y-3">
        {data.groups.map((group) => (
          <CausalGroupCard key={group.id} group={group} />
        ))}
      </div>
    </div>
  )
}
