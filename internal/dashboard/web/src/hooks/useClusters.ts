import { useState, useEffect } from 'react'
import { fetchClusters } from '../api/client'

export function useClusters() {
  const [clusters, setClusters] = useState<string[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetchClusters()
      .then(setClusters)
      .catch(() => setClusters([]))
      .finally(() => setLoading(false))
  }, [])

  return { clusters, loading }
}
