import { useState, useEffect, useCallback, useRef } from 'react'
import type { HealthUpdate } from '../types'
import { fetchHealth } from '../api/client'

export function useHealth(clusterId?: string) {
  const [health, setHealth] = useState<HealthUpdate | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const requestIdRef = useRef(0)

  const refresh = useCallback(() => {
    const id = ++requestIdRef.current
    setLoading(true)
    fetchHealth(clusterId)
      .then((data) => {
        if (id !== requestIdRef.current) return // stale response
        setHealth(data)
        setError(null)
      })
      .catch((e) => {
        if (id !== requestIdRef.current) return
        setError(e.message)
      })
      .finally(() => {
        if (id !== requestIdRef.current) return
        setLoading(false)
      })
  }, [clusterId])

  useEffect(() => {
    refresh()
  }, [refresh])

  return { health, error, loading, refresh, setHealth }
}
