import { useState, useEffect, useRef, useCallback } from 'react'
import type { HealthUpdate } from '../types'

export function useSSE(paused: boolean) {
  const [data, setData] = useState<HealthUpdate | null>(null)
  const [connected, setConnected] = useState(false)
  const esRef = useRef<EventSource | null>(null)

  const connect = useCallback(() => {
    if (esRef.current) {
      esRef.current.close()
    }

    const es = new EventSource('/api/events')
    esRef.current = es

    es.onopen = () => setConnected(true)

    es.onmessage = (event) => {
      try {
        const update = JSON.parse(event.data) as HealthUpdate
        setData(update)
      } catch {
        // ignore parse errors
      }
    }

    es.onerror = () => {
      setConnected(false)
      es.close()
      // Auto-reconnect after 5s
      setTimeout(connect, 5000)
    }
  }, [])

  useEffect(() => {
    if (paused) {
      esRef.current?.close()
      setConnected(false)
      return
    }

    connect()
    return () => {
      esRef.current?.close()
    }
  }, [paused, connect])

  return { data, connected }
}
