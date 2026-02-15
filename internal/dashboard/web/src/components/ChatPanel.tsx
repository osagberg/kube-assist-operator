import { useState, useRef, useEffect } from 'react'
import { useChat } from '../hooks/useChat'
import type { ChatMessage } from '../types'

export function ChatPanel({ open, onClose, clusterId }: { open: boolean; onClose: () => void; clusterId?: string }) {
  const { messages, streaming, send, reset, stop } = useChat()
  const [input, setInput] = useState('')
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  // Auto-scroll to bottom on new messages
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  // Focus input when panel opens
  useEffect(() => {
    if (open) inputRef.current?.focus()
  }, [open])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!input.trim() || streaming) return
    const sent = await send(input.trim(), clusterId)
    if (sent) setInput('')
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') onClose()
  }

  if (!open) return null

  return (
    <>
      {/* Backdrop */}
      <div className="fixed inset-0 z-40 bg-black/20" onClick={onClose} />

      {/* Panel */}
      <div
        className="fixed right-0 top-0 bottom-0 z-50 w-full md:w-[420px] flex flex-col glass-elevated"
        style={{ borderLeft: '1px solid var(--border)' }}
        onKeyDown={handleKeyDown}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3" style={{ borderBottom: '1px solid var(--border)' }}>
          <div className="flex items-center gap-2">
            <h2 className="text-sm font-semibold" style={{ color: 'var(--text-primary)' }}>KubeAssist Chat</h2>
            <span className="text-[10px] font-medium px-2 py-0.5 rounded-full bg-accent-muted text-accent">AI</span>
          </div>
          <button onClick={onClose} className="glass-button px-2 py-1 rounded-lg text-sm" style={{ color: 'var(--text-secondary)' }} aria-label="Close chat">
            ✕
          </button>
        </div>

        {/* Messages */}
        <div className="flex-1 overflow-y-auto px-4 py-3 space-y-3">
          {messages.length === 0 && (
            <div className="text-center py-8" style={{ color: 'var(--text-tertiary)' }}>
              <p className="text-sm">Ask about your cluster's health</p>
              <p className="text-xs mt-1">e.g., "What pods are crashing?" or "Show me the health score"</p>
            </div>
          )}
          {messages.map((msg, i) => (
            <MessageBubble key={i} message={msg} />
          ))}
          <div ref={messagesEndRef} />
        </div>

        {/* Input */}
        <div className="px-4 py-3" style={{ borderTop: '1px solid var(--border)' }}>
          <form onSubmit={handleSubmit} className="flex gap-2">
            <input
              ref={inputRef}
              type="text"
              value={input}
              onChange={e => setInput(e.target.value)}
              placeholder="Ask about your cluster..."
              className="flex-1 glass-panel rounded-lg px-3 py-2 text-sm outline-none"
              style={{ color: 'var(--text-primary)', background: 'var(--bg-secondary)' }}
              disabled={streaming}
              maxLength={2000}
            />
            {streaming ? (
              <button type="button" onClick={stop} className="glass-button px-3 py-2 rounded-lg text-sm" style={{ color: 'var(--text-secondary)' }}>
                Stop
              </button>
            ) : (
              <button type="submit" className="glass-button px-3 py-2 rounded-lg text-sm font-medium text-accent" disabled={!input.trim()}>
                Send
              </button>
            )}
          </form>
          <button
            onClick={reset}
            className="text-xs mt-2 hover:underline"
            style={{ color: 'var(--text-tertiary)' }}
          >
            New Chat
          </button>
        </div>
      </div>
    </>
  )
}

function MessageBubble({ message }: { message: ChatMessage }) {
  const isUser = message.role === 'user'

  return (
    <div className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}>
      <div
        className={`max-w-[85%] rounded-xl px-3 py-2 text-sm ${isUser ? 'bg-accent/15 text-accent' : 'glass-panel'}`}
        style={!isUser ? { color: 'var(--text-primary)' } : undefined}
      >
        {/* Tool calls indicator */}
        {!isUser && message.toolCalls && message.toolCalls.length > 0 && (
          <div className="space-y-1 mb-2">
            {message.toolCalls.map((tc, i) => (
              <div key={i} className="flex items-center gap-1.5 text-[11px]" style={{ color: 'var(--text-tertiary)' }}>
                <span className="inline-block w-3 h-3">&#9881;</span>
                <span>{formatToolName(tc.name)}</span>
                {message.toolResults?.[i] && (
                  <span style={{ color: 'var(--text-secondary)' }}> — {message.toolResults[i].summary}</span>
                )}
              </div>
            ))}
          </div>
        )}

        {/* Message content */}
        {message.content && (
          <div className="whitespace-pre-wrap break-words">{message.content}</div>
        )}

        {/* Streaming indicator */}
        {message.streaming && !message.content && (
          <div className="flex items-center gap-1" style={{ color: 'var(--text-tertiary)' }}>
            <span className="animate-pulse">Thinking</span>
            <span className="animate-bounce">...</span>
          </div>
        )}
        {message.streaming && message.content && (
          <span className="inline-block w-1.5 h-4 ml-0.5 animate-pulse" style={{ background: 'var(--text-tertiary)' }} />
        )}
      </div>
    </div>
  )
}

function formatToolName(name: string): string {
  return name.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase())
}
