import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  CHAT_SESSION_STORAGE_KEY,
  clearChatSessionId,
  getChatAvailability,
  getOrCreateChatSessionId,
  loadChatSessionId,
  resolveSessionIdEvent,
  saveChatSessionId,
} from './chatSession'

const EXISTING_SESSION_ID = '11111111-1111-4111-8111-111111111111'
const GENERATED_SESSION_ID = '22222222-2222-4222-8222-222222222222'

describe('chatSession storage helpers', () => {
  beforeEach(() => {
    vi.unstubAllGlobals()
    vi.stubGlobal('sessionStorage', createMockStorage())
  })

  it('loads a valid persisted session ID', () => {
    sessionStorage.setItem(CHAT_SESSION_STORAGE_KEY, EXISTING_SESSION_ID)

    expect(loadChatSessionId()).toBe(EXISTING_SESSION_ID)
  })

  it('ignores invalid persisted session IDs', () => {
    sessionStorage.setItem(CHAT_SESSION_STORAGE_KEY, 'not-a-uuid')

    expect(loadChatSessionId()).toBeNull()
  })

  it('reuses existing persisted session when available', () => {
    sessionStorage.setItem(CHAT_SESSION_STORAGE_KEY, EXISTING_SESSION_ID)
    vi.stubGlobal('crypto', { randomUUID: vi.fn(() => GENERATED_SESSION_ID) })

    expect(getOrCreateChatSessionId()).toBe(EXISTING_SESSION_ID)
  })

  it('creates and persists a session ID when none exists', () => {
    vi.stubGlobal('crypto', { randomUUID: vi.fn(() => GENERATED_SESSION_ID) })

    expect(getOrCreateChatSessionId()).toBe(GENERATED_SESSION_ID)
    expect(sessionStorage.getItem(CHAT_SESSION_STORAGE_KEY)).toBe(GENERATED_SESSION_ID)
  })

  it('writes only valid session IDs', () => {
    saveChatSessionId('bad')
    expect(sessionStorage.getItem(CHAT_SESSION_STORAGE_KEY)).toBeNull()

    saveChatSessionId(EXISTING_SESSION_ID)
    expect(sessionStorage.getItem(CHAT_SESSION_STORAGE_KEY)).toBe(EXISTING_SESSION_ID)
  })

  it('clears persisted session ID', () => {
    sessionStorage.setItem(CHAT_SESSION_STORAGE_KEY, EXISTING_SESSION_ID)
    clearChatSessionId()
    expect(sessionStorage.getItem(CHAT_SESSION_STORAGE_KEY)).toBeNull()
  })
})

describe('resolveSessionIdEvent', () => {
  it('returns canonical session ID for session_id events', () => {
    expect(resolveSessionIdEvent({ type: 'session_id', sessionId: EXISTING_SESSION_ID })).toBe(EXISTING_SESSION_ID)
  })

  it('returns null for non-session events or invalid IDs', () => {
    expect(resolveSessionIdEvent({ type: 'content', sessionId: EXISTING_SESSION_ID })).toBeNull()
    expect(resolveSessionIdEvent({ type: 'session_id', sessionId: 'bad' })).toBeNull()
  })
})

describe('getChatAvailability', () => {
  it('enables chat only with capability and effective cluster', () => {
    expect(getChatAvailability(true, false, 'cluster-a')).toEqual({ enabled: true })
  })

  it('disables chat in fleet mode with clear reason', () => {
    expect(getChatAvailability(true, true)).toEqual({
      enabled: false,
      reason: 'Chat is disabled in fleet view. Select a cluster first.',
    })
  })

  it('disables chat when no cluster is selected outside fleet mode', () => {
    expect(getChatAvailability(true, false)).toEqual({
      enabled: false,
      reason: 'Select a cluster to use chat.',
    })
  })

  it('disables chat when capability is unavailable', () => {
    expect(getChatAvailability(false, false, 'cluster-a')).toEqual({
      enabled: false,
      reason: 'Chat is unavailable for this dashboard.',
    })
  })
})

function createMockStorage(): Storage {
  const entries = new Map<string, string>()

  return {
    get length() {
      return entries.size
    },
    clear() {
      entries.clear()
    },
    getItem(key: string) {
      return entries.has(key) ? entries.get(key)! : null
    },
    key(index: number) {
      return Array.from(entries.keys())[index] ?? null
    },
    removeItem(key: string) {
      entries.delete(key)
    },
    setItem(key: string, value: string) {
      entries.set(key, String(value))
    },
  }
}
