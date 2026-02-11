import { useState, useCallback, useEffect, useRef } from 'react'
import type { ExplainResponse } from '../types'
import { fetchExplain } from '../api/client'

export function useExplain(clusterId?: string) {
  const [data, setData] = useState<ExplainResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const requestVersion = useRef(0)

  useEffect(() => {
    // Cluster changed: clear stale explanation and invalidate in-flight requests.
    requestVersion.current++
    setData(null)
    setError(null)
    setLoading(false)
  }, [clusterId])

  const refresh = useCallback(async () => {
    const currentVersion = ++requestVersion.current
    setLoading(true)
    try {
      const result = await fetchExplain(clusterId)
      if (currentVersion !== requestVersion.current) return
      setData(result)
      setError(null)
    } catch (err) {
      if (currentVersion !== requestVersion.current) return
      setError(err instanceof Error ? err.message : 'Failed to fetch explanation')
    } finally {
      if (currentVersion !== requestVersion.current) return
      setLoading(false)
    }
  }, [clusterId])

  // Load explanation automatically like the other data panels.
  useEffect(() => {
    void refresh()
  }, [refresh])

  return { data, loading, error, refresh }
}
