import { useState, useCallback } from 'react'
import type { ExplainResponse } from '../types'
import { fetchExplain } from '../api/client'

export function useExplain(clusterId?: string) {
  const [data, setData] = useState<ExplainResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const result = await fetchExplain(clusterId)
      setData(result)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch explanation')
    } finally {
      setLoading(false)
    }
  }, [clusterId])

  return { data, loading, error, refresh }
}
