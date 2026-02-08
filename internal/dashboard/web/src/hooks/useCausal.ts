import { useState, useEffect, useCallback } from 'react'
import type { CausalContext } from '../types'
import { fetchCausalGroups } from '../api/client'

export function useCausal(clusterId?: string, refreshInterval = 30000) {
  const [data, setData] = useState<CausalContext | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      const result = await fetchCausalGroups(clusterId)
      setData(result)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch causal data')
    } finally {
      setLoading(false)
    }
  }, [clusterId])

  useEffect(() => {
    refresh()
    const interval = setInterval(refresh, refreshInterval)
    return () => clearInterval(interval)
  }, [refresh, refreshInterval])

  return { data, loading, error, refresh }
}
