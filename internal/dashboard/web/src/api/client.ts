import type {
  HealthUpdate,
  AISettingsResponse,
  AISettingsRequest,
  HealthSnapshot,
  CausalContext,
  ExplainResponse,
  PredictionResult,
  PredictionResponse,
  ModelCatalog,
  FleetSummary,
  CreateTroubleshootRequest,
  TroubleshootRequestSummary,
} from '../types'

const BASE = '/api'

const authToken = document.querySelector('meta[name="dashboard-auth-token"]')?.getAttribute('content') ?? '';

function getAuthHeaders(): Record<string, string> {
  if (!authToken) return {}
  return { Authorization: `Bearer ${authToken}` }
}

/** Normalize Go nil slices (JSON null) to empty arrays */
export function normalizeHealth(data: HealthUpdate): HealthUpdate {
  if (!data.results) {
    data.results = {}
  }
  for (const key of Object.keys(data.results)) {
    if (!data.results[key].issues) {
      data.results[key].issues = []
    }
  }
  if (!data.namespaces) {
    data.namespaces = []
  }
  if (!data.summary) {
    data.summary = { totalHealthy: 0, totalIssues: 0, criticalCount: 0, warningCount: 0, infoCount: 0 }
  }
  return data
}

async function json<T>(url: string, init?: RequestInit): Promise<T> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 30_000)
  try {
    const headers = { ...getAuthHeaders(), ...init?.headers }
    const resp = await fetch(url, { ...init, headers, signal: controller.signal })
    if (!resp.ok) throw new Error(`${resp.status} ${resp.statusText}`)
    return resp.json() as Promise<T>
  } finally {
    clearTimeout(timeout)
  }
}

/** GET /api/health — current health data */
export async function fetchHealth(clusterId?: string): Promise<HealthUpdate> {
  const url = clusterId
    ? `${BASE}/health?clusterId=${encodeURIComponent(clusterId)}`
    : `${BASE}/health`
  const data = await json<HealthUpdate>(url)
  return normalizeHealth(data)
}

/** GET /api/fleet/summary — aggregate fleet health */
export function fetchFleetSummary(): Promise<FleetSummary> {
  return json<FleetSummary>(`${BASE}/fleet/summary`)
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

/** GET /api/settings/ai/catalog — model catalog */
export function fetchModelCatalog(provider?: string): Promise<ModelCatalog> {
  const url = new URL(`${BASE}/settings/ai/catalog`, window.location.origin)
  if (provider) url.searchParams.set('provider', provider)
  return json<ModelCatalog>(url.toString())
}

/** GET /api/health/history — health score history */
export function fetchHealthHistory(params?: { last?: number; since?: string; clusterId?: string }): Promise<HealthSnapshot[]> {
  const url = new URL(`${BASE}/health/history`, window.location.origin)
  if (params?.last) url.searchParams.set('last', String(params.last))
  if (params?.since) url.searchParams.set('since', params.since)
  if (params?.clusterId) url.searchParams.set('clusterId', params.clusterId)
  return json<HealthSnapshot[]>(url.toString())
}

/** GET /api/causal/groups — causal correlation analysis */
export function fetchCausalGroups(clusterId?: string): Promise<CausalContext> {
  const url = clusterId
    ? `${BASE}/causal/groups?clusterId=${encodeURIComponent(clusterId)}`
    : `${BASE}/causal/groups`
  return json<CausalContext>(url)
}

/** GET /api/explain — AI-generated cluster health explanation */
export function fetchExplain(clusterId?: string): Promise<ExplainResponse> {
  const url = clusterId
    ? `${BASE}/explain?clusterId=${encodeURIComponent(clusterId)}`
    : `${BASE}/explain`
  return json<ExplainResponse>(url)
}

/** GET /api/prediction/trend — predictive health trend analysis */
export async function fetchPrediction(clusterId?: string): Promise<PredictionResult | null> {
  const url = clusterId
    ? `${BASE}/prediction/trend?clusterId=${encodeURIComponent(clusterId)}`
    : `${BASE}/prediction/trend`
  const data = await json<PredictionResult & PredictionResponse>(url)
  if (data.status === 'insufficient_data') return null
  return data as PredictionResult
}

/** GET /api/clusters — available clusters */
export async function fetchClusters(): Promise<string[]> {
  const data = await json<{ clusters: string[] }>(`${BASE}/clusters`)
  return data.clusters ?? []
}

/** POST /api/troubleshoot — create a TroubleshootRequest CR */
export function createTroubleshootRequest(
  body: CreateTroubleshootRequest,
): Promise<{ name: string; namespace: string; phase: string }> {
  return json(`${BASE}/troubleshoot`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...getAuthHeaders() },
    body: JSON.stringify(body),
  })
}

/** GET /api/troubleshoot — list TroubleshootRequest CRs */
export function listTroubleshootRequests(namespace?: string): Promise<TroubleshootRequestSummary[]> {
  const url = new URL(`${BASE}/troubleshoot`, window.location.origin)
  if (namespace) url.searchParams.set('namespace', namespace)
  return json<TroubleshootRequestSummary[]>(url.toString())
}

/** GET /api/capabilities — feature flags */
export interface Capabilities {
  troubleshootCreate: boolean
}
export function fetchCapabilities(): Promise<Capabilities> {
  return json<Capabilities>(`${BASE}/capabilities`)
}

/** GET /api/events — SSE stream (returns EventSource, caller manages lifecycle) */
export function createSSEConnection(clusterId?: string): EventSource {
  const url = new URL(
    clusterId ? `${BASE}/events?clusterId=${encodeURIComponent(clusterId)}` : `${BASE}/events`,
    window.location.origin,
  )
  // Auth cookie (__dashboard_session) is sent automatically by the browser.
  // No need to pass token as query parameter.
  return new EventSource(url.toString())
}
