// Types matching Go structs in internal/dashboard/server.go

export interface HealthUpdate {
  timestamp: string
  namespaces: string[]
  results: Record<string, CheckResult>
  summary: Summary
}

export interface CheckResult {
  name: string
  healthy: number
  issues: Issue[]
  error?: string
}

export interface Issue {
  type: string
  severity: 'Critical' | 'Warning' | 'Info'
  resource: string
  namespace: string
  message: string
  suggestion: string
}

export interface Summary {
  totalHealthy: number
  totalIssues: number
  criticalCount: number
  warningCount: number
  infoCount: number
}

// AI settings types matching Go structs
export interface AISettingsResponse {
  enabled: boolean
  provider: string
  model?: string
  hasApiKey: boolean
  providerReady: boolean
}

export interface AISettingsRequest {
  enabled: boolean
  provider: string
  apiKey?: string
  model?: string
}

// Health history types matching internal/history/ringbuffer.go
export interface HealthSnapshot {
  timestamp: string
  totalHealthy: number
  totalIssues: number
  bySeverity: Record<string, number>
  byChecker: Record<string, number>
  healthScore: number
}
