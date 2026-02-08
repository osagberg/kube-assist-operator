import { useState, useEffect, useCallback } from 'react'
import type { FleetSummary } from '../types'
import { fetchFleetSummary } from '../api/client'

export function useFleet(enabled: boolean) {
  const [fleet, setFleet] = useState<FleetSummary | null>(null)
  const [loading, setLoading] = useState(false)

  const refresh = useCallback(() => {
    if (!enabled) return
    setLoading(true)
    fetchFleetSummary()
      .then(setFleet)
      .catch(() => setFleet(null))
      .finally(() => setLoading(false))
  }, [enabled])

  useEffect(() => {
    refresh()
    if (!enabled) return
    const interval = setInterval(refresh, 30_000)
    return () => clearInterval(interval)
  }, [refresh, enabled])

  return { fleet, loading, refresh }
}
