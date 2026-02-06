import { useState, useEffect, useRef, useCallback } from 'react'
import type { HealthUpdate } from '../types'

const MAX_RETRIES = 10
const BASE_DELAY = 1000
const MAX_DELAY = 60000

export function useSSE(paused: boolean) {
  const [data, setData] = useState<HealthUpdate | null>(null)
  const [connected, setConnected] = useState(false)
  const esRef = useRef<EventSource | null>(null)
  const retriesRef = useRef(0)

  const connect = useCallback(() => {
    if (esRef.current) {
      esRef.current.close()
    }

    const es = new EventSource('/api/events')
    esRef.current = es

    es.onopen = () => {
      setConnected(true)
      retriesRef.current = 0
    }

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
      if (retriesRef.current >= MAX_RETRIES) return
      const delay = Math.min(BASE_DELAY * 2 ** retriesRef.current, MAX_DELAY)
      const jitter = delay * 0.1 * Math.random()
      retriesRef.current++
      setTimeout(connect, delay + jitter)
    }
  }, [])

  useEffect(() => {
    if (paused) {
      esRef.current?.close()
      setConnected(false)
      return
    }

    retriesRef.current = 0
    connect()
    return () => {
      esRef.current?.close()
    }
  }, [paused, connect])

  return { data, connected }
}
