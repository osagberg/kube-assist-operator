import { useState, useCallback, useEffect } from 'react'

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

  const addToast = useCallback((text: string, type: 'success' | 'error' | 'info' = 'info') => {
    const id = nextId++
    setToasts((prev) => [...prev, { id, text, type }])
    setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id))
    }, 3000)
  }, [])

  useEffect(() => {
    addToastFn = addToast
    return () => { addToastFn = null }
  }, [addToast])

  if (toasts.length === 0) return null

  const colors = {
    success: 'bg-green-500',
    error: 'bg-red-500',
    info: 'bg-indigo-500',
  }

  return (
    <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2">
      {toasts.map((t) => (
        <div
          key={t.id}
          className={`${colors[t.type]} text-white px-4 py-2 rounded-lg shadow-lg text-sm animate-slide-up`}
        >
          {t.text}
        </div>
      ))}
    </div>
  )
}
