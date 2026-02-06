import { useState, useEffect, useCallback } from 'react'
import type { HealthUpdate } from '../types'
import { fetchHealth } from '../api/client'

export function useHealth() {
  const [health, setHealth] = useState<HealthUpdate | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(() => {
    setLoading(true)
    fetchHealth()
      .then((data) => {
        setHealth(data)
        setError(null)
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

  return { health, error, loading, refresh, setHealth }
}
