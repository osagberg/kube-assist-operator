import type {
  HealthUpdate,
  AISettingsResponse,
  AISettingsRequest,
  HealthSnapshot,
  CausalContext,
  ExplainResponse,
  PredictionResult,
  PredictionResponse,
} from '../types'

const BASE = '/api'

function getAuthHeaders(): Record<string, string> {
  const token = (window as any).__DASHBOARD_AUTH_TOKEN__ || ''
  if (!token) return {}
  return { Authorization: `Bearer ${token}` }
}

/** Normalize Go nil slices (JSON null) to empty arrays */
export function normalizeHealth(data: HealthUpdate): HealthUpdate {
  for (const key of Object.keys(data.results)) {
    if (!data.results[key].issues) {
      data.results[key].issues = []
    }
  }
  if (!data.namespaces) {
    data.namespaces = []
  }
  return data
}

async function json<T>(url: string, init?: RequestInit): Promise<T> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 30_000)
  try {
    const resp = await fetch(url, { ...init, signal: controller.signal })
    if (!resp.ok) throw new Error(`${resp.status} ${resp.statusText}`)
    return resp.json() as Promise<T>
  } finally {
    clearTimeout(timeout)
  }
}

/** GET /api/health — current health data */
export async function fetchHealth(): Promise<HealthUpdate> {
  const data = await json<HealthUpdate>(`${BASE}/health`)
  return normalizeHealth(data)
}

/** POST /api/check — trigger immediate health check */
export async function triggerCheck(): Promise<void> {
  const resp = await fetch(`${BASE}/check`, {
    method: 'POST',
    headers: { ...getAuthHeaders() },
  })
  if (!resp.ok) {
    throw new Error(`${resp.status} ${resp.statusText}`)
  }
}

/** GET /api/settings/ai — current AI configuration */
export function fetchAISettings(): Promise<AISettingsResponse> {
  return json<AISettingsResponse>(`${BASE}/settings/ai`)
}

/** POST /api/settings/ai — update AI configuration */
export function updateAISettings(settings: AISettingsRequest): Promise<AISettingsResponse> {
  return json<AISettingsResponse>(`${BASE}/settings/ai`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...getAuthHeaders() },
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

/** GET /api/explain — AI-generated cluster health explanation */
export function fetchExplain(): Promise<ExplainResponse> {
  return json<ExplainResponse>(`${BASE}/explain`)
}

/** GET /api/prediction/trend — predictive health trend analysis */
export async function fetchPrediction(): Promise<PredictionResult | null> {
  const data = await json<PredictionResult & PredictionResponse>(`${BASE}/prediction/trend`)
  if (data.status === 'insufficient_data') return null
  return data as PredictionResult
}

/** GET /api/events — SSE stream (returns EventSource, caller manages lifecycle) */
export function createSSEConnection(): EventSource {
  return new EventSource(`${BASE}/events`)
}
