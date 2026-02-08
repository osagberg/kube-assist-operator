import { useState, useEffect } from 'react'
import type { ModelCatalog } from '../types'
import { fetchModelCatalog } from '../api/client'

export function useCatalog(provider?: string) {
  const [catalog, setCatalog] = useState<ModelCatalog | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!provider || provider === 'noop') {
      setCatalog(null)
      return
    }

    setLoading(true)
    fetchModelCatalog(provider)
      .then(setCatalog)
      .catch(() => setCatalog(null))
      .finally(() => setLoading(false))
  }, [provider])

  return { catalog, loading }
}
