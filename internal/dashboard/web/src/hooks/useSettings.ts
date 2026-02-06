import { useState, useEffect, useCallback } from 'react'
import type { AISettingsResponse, AISettingsRequest } from '../types'
import { fetchAISettings, updateAISettings } from '../api/client'

export function useSettings() {
  const [settings, setSettings] = useState<AISettingsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(() => {
    setLoading(true)
    fetchAISettings()
      .then((s) => { setSettings(s); setError(null) })
      .catch((e) => setError(e instanceof Error ? e.message : 'Failed to load settings'))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

  const save = async (req: AISettingsRequest) => {
    const updated = await updateAISettings(req)
    setSettings(updated)
    return updated
  }

  return { settings, loading, error, save, refresh }
}
