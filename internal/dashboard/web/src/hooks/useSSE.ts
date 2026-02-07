import { useState, useEffect, useRef, useCallback } from 'react'
import type { HealthUpdate } from '../types'
import { normalizeHealth, createSSEConnection } from '../api/client'

const MAX_RETRIES = 10
const BASE_DELAY = 1000
const MAX_DELAY = 60000

export function useSSE(paused: boolean) {
  const [data, setData] = useState<HealthUpdate | null>(null)
  const [connected, setConnected] = useState(false)
  const esRef = useRef<EventSource | null>(null)
  const retriesRef = useRef(0)
  const retryTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const mountedRef = useRef(true)

  const connect = useCallback(() => {
    if (esRef.current) {
      esRef.current.close()
    }

    const es = createSSEConnection()
    esRef.current = es

    es.onopen = () => {
      if (!mountedRef.current) return
      setConnected(true)
      retriesRef.current = 0
    }

    es.onmessage = (event) => {
      if (!mountedRef.current) return
      try {
        const update = JSON.parse(event.data) as HealthUpdate
        setData(normalizeHealth(update))
      } catch {
        // ignore parse errors
      }
    }

    es.onerror = () => {
      if (!mountedRef.current) return
      setConnected(false)
      es.close()
      if (retriesRef.current >= MAX_RETRIES) return
      const delay = Math.min(BASE_DELAY * 2 ** retriesRef.current, MAX_DELAY)
      const jitter = delay * 0.1 * Math.random()
      retriesRef.current++
      retryTimeoutRef.current = setTimeout(connect, delay + jitter)
    }
  }, [])

  useEffect(() => {
    mountedRef.current = true

    if (paused) {
      esRef.current?.close()
      if (retryTimeoutRef.current) {
        clearTimeout(retryTimeoutRef.current)
        retryTimeoutRef.current = null
      }
      setConnected(false)
      return
    }

    retriesRef.current = 0
    connect()
    return () => {
      mountedRef.current = false
      esRef.current?.close()
      if (retryTimeoutRef.current) {
        clearTimeout(retryTimeoutRef.current)
        retryTimeoutRef.current = null
      }
    }
  }, [paused, connect])

  return { data, connected }
}
