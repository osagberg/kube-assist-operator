import { useState, useCallback, useEffect, useRef } from 'react'

interface ToastMessage {
  id: number
  text: string
  type: 'success' | 'error' | 'info'
}

let nextId = 0
let addToastFn: ((text: string, type?: 'success' | 'error' | 'info') => void) | null = null

export function showToast(text: string, type: 'success' | 'error' | 'info' = 'info') {
  addToastFn?.(text, type)
}

export function ToastContainer() {
  const [toasts, setToasts] = useState<ToastMessage[]>([])
  const timersRef = useRef<Map<number, ReturnType<typeof setTimeout>>>(new Map())

  const addToast = useCallback((text: string, type: 'success' | 'error' | 'info' = 'info') => {
    const id = nextId++
    setToasts((prev) => [...prev, { id, text, type }])
    const timer = setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id))
      timersRef.current.delete(id)
    }, 3000)
    timersRef.current.set(id, timer)
  }, [])

  useEffect(() => {
    addToastFn = addToast
    const timers = timersRef.current
    return () => {
      addToastFn = null
      timers.forEach((t) => clearTimeout(t))
      timers.clear()
    }
  }, [addToast])

  if (toasts.length === 0) return null

  const styles: Record<string, { borderColor: string; textClass: string }> = {
    success: {
      borderColor: 'rgba(34, 197, 94, 0.3)',
      textClass: 'text-severity-healthy',
    },
    error: {
      borderColor: 'rgba(239, 68, 68, 0.3)',
      textClass: 'text-severity-critical',
    },
    info: {
      borderColor: 'rgba(99, 102, 241, 0.3)',
      textClass: 'text-accent',
    },
  }

  return (
    <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2" role="status" aria-live="polite" aria-atomic="true">
      {toasts.map((t) => (
        <div
          key={t.id}
          className={`glass-panel rounded-xl px-4 py-2 text-sm animate-slide-up border ${styles[t.type].textClass}`}
          style={{ borderColor: styles[t.type].borderColor }}
        >
          {t.text}
        </div>
      ))}
    </div>
  )
}
