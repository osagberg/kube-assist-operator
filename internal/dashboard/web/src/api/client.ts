import type {
  HealthUpdate,
  AISettingsResponse,
  AISettingsRequest,
  HealthSnapshot,
  CausalContext,
} from '../types'

const BASE = '/api'

async function json<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init)
  if (!resp.ok) {
    throw new Error(`${resp.status} ${resp.statusText}`)
  }
  return resp.json() as Promise<T>
}

/** GET /api/health — current health data */
export function fetchHealth(): Promise<HealthUpdate> {
  return json<HealthUpdate>(`${BASE}/health`)
}

/** POST /api/check — trigger immediate health check */
export async function triggerCheck(): Promise<void> {
  await fetch(`${BASE}/check`, { method: 'POST' })
}

/** GET /api/settings/ai — current AI configuration */
export function fetchAISettings(): Promise<AISettingsResponse> {
  return json<AISettingsResponse>(`${BASE}/settings/ai`)
}

/** POST /api/settings/ai — update AI configuration */
export function updateAISettings(settings: AISettingsRequest): Promise<AISettingsResponse> {
  return json<AISettingsResponse>(`${BASE}/settings/ai`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(settings),
  })
}

/** GET /api/health/history — health score history */
export function fetchHealthHistory(params?: { last?: number; since?: string }): Promise<HealthSnapshot[]> {
  const url = new URL(`${BASE}/health/history`, window.location.origin)
  if (params?.last) url.searchParams.set('last', String(params.last))
  if (params?.since) url.searchParams.set('since', params.since)
  return json<HealthSnapshot[]>(url.toString())
}

/** GET /api/causal/groups — causal correlation analysis */
export function fetchCausalGroups(): Promise<CausalContext> {
  return json<CausalContext>(`${BASE}/causal/groups`)
}

/** GET /api/events — SSE stream (returns EventSource, caller manages lifecycle) */
export function createSSEConnection(): EventSource {
  return new EventSource(`${BASE}/events`)
}
