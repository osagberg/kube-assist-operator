import { useState, useCallback, useRef, useEffect } from 'react'
import type { ChatMessage, ChatEvent } from '../types'

export function useChat() {
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [streaming, setStreaming] = useState(false)
  const sessionIdRef = useRef(crypto.randomUUID())
  const abortRef = useRef<AbortController | null>(null)

  // Abort any in-flight request on unmount
  useEffect(() => {
    return () => { abortRef.current?.abort() }
  }, [])

  const send = useCallback(async (message: string, clusterId?: string) => {
    if (!message.trim() || streaming) return

    // Add user message
    const userMsg: ChatMessage = { role: 'user', content: message }
    setMessages(prev => [...prev, userMsg])
    setStreaming(true)

    // Create assistant message placeholder
    const assistantMsg: ChatMessage = { role: 'assistant', content: '', streaming: true, toolCalls: [], toolResults: [] }
    setMessages(prev => [...prev, assistantMsg])

    const controller = new AbortController()
    abortRef.current = controller

    try {
      const response = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sessionId: sessionIdRef.current, message, clusterId }),
        credentials: 'same-origin',
        signal: controller.signal,
      })

      if (!response.ok) {
        const text = await response.text()
        throw new Error(text || `${response.status} ${response.statusText}`)
      }

      if (!response.body) {
        throw new Error('Response body is not readable')
      }
      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''

      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })

        const parts = buffer.split('\n\n')
        buffer = parts.pop() ?? ''

        for (const part of parts) {
          const line = part.trim()
          if (!line.startsWith('data: ')) continue
          try {
            const event: ChatEvent = JSON.parse(line.slice(6))
            handleEvent(event, setMessages)
          } catch { /* skip malformed events */ }
        }
      }
    } catch (err) {
      if ((err as Error).name !== 'AbortError') {
        setMessages(prev => {
          const updated = [...prev]
          const last = updated[updated.length - 1]
          if (last?.role === 'assistant') {
            updated[updated.length - 1] = { ...last, content: last.content || `Error: ${(err as Error).message}`, streaming: false }
          }
          return updated
        })
      }
    } finally {
      setStreaming(false)
      abortRef.current = null
      // Ensure streaming flag cleared on final message
      setMessages(prev => {
        const updated = [...prev]
        const last = updated[updated.length - 1]
        if (last?.role === 'assistant' && last.streaming) {
          updated[updated.length - 1] = { ...last, streaming: false }
        }
        return updated
      })
    }
  }, [streaming])

  const reset = useCallback(() => {
    if (abortRef.current) abortRef.current.abort()
    setMessages([])
    setStreaming(false)
    sessionIdRef.current = crypto.randomUUID()
  }, [])

  const stop = useCallback(() => {
    if (abortRef.current) abortRef.current.abort()
  }, [])

  return { messages, streaming, send, reset, stop }
}

function handleEvent(event: ChatEvent, setMessages: React.Dispatch<React.SetStateAction<ChatMessage[]>>) {
  setMessages(prev => {
    const updated = [...prev]
    const lastIdx = updated.length - 1
    if (lastIdx < 0 || updated[lastIdx].role !== 'assistant') return updated
    const last = { ...updated[lastIdx] }

    switch (event.type) {
      case 'tool_call':
        last.toolCalls = [...(last.toolCalls ?? []), { name: event.tool ?? '', args: event.args ?? {} }]
        break
      case 'tool_result':
        last.toolResults = [...(last.toolResults ?? []), { name: event.tool ?? '', summary: event.content ?? '' }]
        break
      case 'content':
        last.content = (last.content ?? '') + (event.content ?? '')
        break
      case 'done':
        last.streaming = false
        break
      case 'error':
        last.content = last.content || `Error: ${event.content}`
        last.streaming = false
        break
    }

    updated[lastIdx] = last
    return updated
  })
}
