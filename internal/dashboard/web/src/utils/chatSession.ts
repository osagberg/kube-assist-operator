import type { ChatEvent } from '../types'

export const CHAT_SESSION_STORAGE_KEY = 'kubeassist.chat.session_id'

const UUID_V4_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i

export interface ChatAvailability {
  enabled: boolean
  reason?: string
}

export function getOrCreateChatSessionId(): string {
  const persisted = loadChatSessionId()
  if (persisted) return persisted

  const next = createChatSessionId()
  saveChatSessionId(next)
  return next
}

export function saveChatSessionId(sessionId: string): void {
  const storage = getSessionStorage()
  if (!storage) return

  const normalized = normalizeSessionId(sessionId)
  if (!normalized) return

  try {
    storage.setItem(CHAT_SESSION_STORAGE_KEY, normalized)
  } catch {
    // Ignore storage write errors (e.g. private mode quota restrictions).
  }
}

export function loadChatSessionId(): string | null {
  const storage = getSessionStorage()
  if (!storage) return null

  try {
    const value = storage.getItem(CHAT_SESSION_STORAGE_KEY)
    return normalizeSessionId(value)
  } catch {
    return null
  }
}

export function clearChatSessionId(): void {
  const storage = getSessionStorage()
  if (!storage) return

  try {
    storage.removeItem(CHAT_SESSION_STORAGE_KEY)
  } catch {
    // Ignore storage removal errors.
  }
}

export function resolveSessionIdEvent(event: Pick<ChatEvent, 'type' | 'sessionId'>): string | null {
  if (event.type !== 'session_id') return null
  return normalizeSessionId(event.sessionId)
}

export function getChatAvailability(
  canChat: boolean,
  showFleet: boolean,
  effectiveCluster?: string,
): ChatAvailability {
  if (!canChat) {
    return { enabled: false, reason: 'Chat is unavailable for this dashboard.' }
  }
  if (showFleet) {
    return { enabled: false, reason: 'Chat is disabled in fleet view. Select a cluster first.' }
  }
  if (!effectiveCluster) {
    return { enabled: false, reason: 'Select a cluster to use chat.' }
  }
  return { enabled: true }
}

function createChatSessionId(): string {
  return crypto.randomUUID()
}

function normalizeSessionId(value: string | null | undefined): string | null {
  if (!value) return null
  const trimmed = value.trim()
  return UUID_V4_PATTERN.test(trimmed) ? trimmed : null
}

function getSessionStorage(): Storage | null {
  try {
    return globalThis.sessionStorage ?? null
  } catch {
    return null
  }
}
