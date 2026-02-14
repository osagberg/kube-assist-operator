// Types matching Go structs in internal/dashboard/server.go

export interface AIStatus {
  enabled: boolean
  provider: string
  lastError?: string
  issuesEnhanced: number
  tokensUsed: number
  estimatedCostUsd?: number
  cacheHit?: boolean
  issuesCapped?: boolean
  totalIssueCount?: number
  pending?: boolean
  checkPhase?: string // checkers, causal, ai, done
}

export interface HealthUpdate {
  timestamp: string
  namespaces: string[]
  results: Record<string, CheckResult>
  summary: Summary
  aiStatus?: AIStatus
  clusterId?: string
  issueStates?: Record<string, IssueState>
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
  aiEnhanced?: boolean
  rootCause?: string
  metadata?: Record<string, string>
}

export interface Summary {
  totalHealthy: number
  totalIssues: number
  criticalCount: number
  warningCount: number
  infoCount: number
  healthScore: number
  deploymentReady: number
  deploymentDesired: number
  deploymentReadinessScore: number
}

// AI settings types matching Go structs
export interface AISettingsResponse {
  enabled: boolean
  provider: string
  model?: string
  explainModel?: string
  hasApiKey: boolean
  providerReady: boolean
}

export interface AISettingsRequest {
  enabled: boolean
  provider: string
  apiKey?: string
  clearApiKey?: boolean
  model?: string
  explainModel?: string
}

// Model catalog types matching internal/ai/catalog.go
export interface ModelEntry {
  id: string
  label: string
  status: 'active' | 'deprecated'
  pricingHint?: string
  verifiedAt: string
  tier: 'primary' | 'explain' | 'both'
}

export type ModelCatalog = Record<string, ModelEntry[]>

// Fleet summary types matching internal/dashboard/server.go
export interface FleetSummary {
  clusters: FleetClusterEntry[]
}

export interface FleetClusterEntry {
  clusterId: string
  healthScore: number
  totalIssues: number
  criticalCount: number
  warningCount: number
  infoCount: number
  deploymentReady: number
  deploymentDesired: number
  deploymentReadinessScore: number
  lastUpdated: string
}

// Causal analysis types matching internal/causal/types.go
export interface CausalContext {
  groups: CausalGroup[]
  uncorrelatedCount: number
  totalIssues: number
}

export interface CausalGroup {
  id: string
  title: string
  rootCause?: string
  severity: 'Critical' | 'Warning' | 'Info'
  events: TimelineEvent[]
  rule: string
  confidence: number
  firstSeen: string
  lastSeen: string
  aiRootCause?: string
  aiSuggestion?: string
  aiSteps?: string[]
  aiEnhanced?: boolean
}

export interface TimelineEvent {
  timestamp: string
  checker: string
  issue: Issue
}

// Explain response types matching internal/ai/provider.go
export interface ExplainResponse {
  narrative: string
  riskLevel: string
  topIssues?: ExplainIssue[]
  trendDirection: string
  confidence: number
  tokensUsed: number
}

export interface ExplainIssue {
  title: string
  severity: string
  impact: string
}

// Prediction types matching internal/prediction/analyzer.go
export interface PredictionResult {
  trendDirection: 'improving' | 'stable' | 'degrading'
  velocity: number
  projectedScore: number
  confidenceInterval: [number, number]
  rSquared: number
  dataPoints: number
  riskyCheckers?: string[]
  severityTrajectories?: Record<string, string>
}

export interface PredictionResponse {
  status?: string
  message?: string
}

// Issue state types matching internal/dashboard/issuestate.go
export interface IssueState {
  key: string
  action: 'acknowledged' | 'snoozed'
  reason?: string
  snoozedUntil?: string
  createdAt: string
}

// TroubleshootRequest types matching internal/dashboard/server.go
export type TroubleshootAction = 'diagnose' | 'logs' | 'events' | 'describe' | 'all'
export type TargetKind = 'Deployment' | 'StatefulSet' | 'DaemonSet' | 'Pod' | 'ReplicaSet'

export interface CreateTroubleshootRequest {
  namespace: string
  target: { kind: TargetKind; name: string }
  actions?: TroubleshootAction[]
  tailLines?: number
  ttlSecondsAfterFinished?: number
}

export interface TroubleshootRequestSummary {
  name: string
  namespace: string
  phase: string
  target: { kind: string; name: string }
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

// Chat types for NLQ chat interface
export interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
  toolCalls?: { name: string; args: Record<string, unknown> }[]
  toolResults?: { name: string; summary: string }[]
  streaming?: boolean
}

export interface ChatEvent {
  type: 'thinking' | 'tool_call' | 'tool_result' | 'content' | 'done' | 'error' | 'session_id'
  content?: string
  tool?: string
  args?: Record<string, unknown>
  tokens?: number
  sessionId?: string
}
