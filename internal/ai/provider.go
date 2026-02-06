/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package ai provides AI-powered analysis for health check results.
package ai

import (
	"context"
	"errors"
)

// ErrNotConfigured is returned when AI provider is not configured
var ErrNotConfigured = errors.New("AI provider not configured")

// Provider defines the interface for AI analysis providers
type Provider interface {
	// Name returns the provider identifier (e.g., "openai", "anthropic", "noop")
	Name() string

	// Analyze takes health check context and returns enhanced suggestions
	Analyze(ctx context.Context, request AnalysisRequest) (*AnalysisResponse, error)

	// Available returns true if the provider is properly configured and ready
	Available() bool
}

// AnalysisRequest contains the context for AI analysis
type AnalysisRequest struct {
	// Issues is the list of issues to analyze
	Issues []IssueContext `json:"issues"`

	// ClusterContext provides additional cluster information
	ClusterContext ClusterContext `json:"clusterContext"`

	// CausalContext provides correlation data for root cause analysis.
	// When present, the AI provider should use it to give deeper analysis.
	CausalContext *CausalAnalysisContext `json:"causalContext,omitempty"`

	// MaxTokens limits the response length (provider-specific)
	MaxTokens int `json:"maxTokens,omitempty"`
}

// CausalAnalysisContext is a simplified representation of causal correlation
// data that can be included in AI analysis requests.
type CausalAnalysisContext struct {
	// Groups are the correlated issue groups with inferred root causes.
	Groups []CausalGroupSummary `json:"groups"`

	// UncorrelatedCount is the number of issues not matching any rule.
	UncorrelatedCount int `json:"uncorrelatedCount"`

	// TotalIssues is the total number of issues analyzed.
	TotalIssues int `json:"totalIssues"`
}

// CausalGroupSummary is a condensed view of a CausalGroup for AI consumption.
type CausalGroupSummary struct {
	Rule       string   `json:"rule"`
	Title      string   `json:"title"`
	RootCause  string   `json:"rootCause,omitempty"`
	Severity   string   `json:"severity"`
	Confidence float64  `json:"confidence"`
	Resources  []string `json:"resources"`
}

// IssueContext contains sanitized issue information for AI analysis
type IssueContext struct {
	// Type is the issue type (e.g., "CrashLoopBackOff", "UpgradeFailed")
	Type string `json:"type"`

	// Severity is the issue severity level
	Severity string `json:"severity"`

	// Resource is the resource identifier (e.g., "deployment/my-app")
	Resource string `json:"resource"`

	// Namespace is the resource namespace
	Namespace string `json:"namespace"`

	// Message is the issue description
	Message string `json:"message"`

	// StaticSuggestion is the built-in suggestion
	StaticSuggestion string `json:"staticSuggestion"`

	// Events contains recent related events (sanitized)
	Events []string `json:"events,omitempty"`

	// Logs contains recent relevant logs (sanitized)
	Logs []string `json:"logs,omitempty"`
}

// ClusterContext provides sanitized cluster information
type ClusterContext struct {
	// KubernetesVersion is the cluster version
	KubernetesVersion string `json:"kubernetesVersion,omitempty"`

	// Provider is the cluster provider (e.g., "kind", "eks", "gke", "orbstack")
	Provider string `json:"provider,omitempty"`

	// FluxVersion is the Flux CD version if installed
	FluxVersion string `json:"fluxVersion,omitempty"`

	// Namespaces being analyzed
	Namespaces []string `json:"namespaces"`
}

// AnalysisResponse contains the AI analysis results
type AnalysisResponse struct {
	// EnhancedSuggestions maps issue identifiers to enhanced suggestions
	EnhancedSuggestions map[string]EnhancedSuggestion `json:"enhancedSuggestions"`

	// Summary provides an overall analysis summary
	Summary string `json:"summary,omitempty"`

	// TokensUsed tracks token consumption (if available)
	TokensUsed int `json:"tokensUsed,omitempty"`
}

// EnhancedSuggestion contains AI-generated analysis for an issue
type EnhancedSuggestion struct {
	// Suggestion is the AI-generated fix suggestion
	Suggestion string `json:"suggestion"`

	// RootCause is the likely root cause analysis
	RootCause string `json:"rootCause,omitempty"`

	// Steps is an ordered list of remediation steps
	Steps []string `json:"steps,omitempty"`

	// References is a list of relevant documentation links
	References []string `json:"references,omitempty"`

	// Confidence is the AI's confidence level (0-1)
	Confidence float64 `json:"confidence,omitempty"`
}

// Config holds AI provider configuration
type Config struct {
	// Provider is the AI provider to use ("openai", "anthropic", "noop")
	Provider string `json:"provider"`

	// APIKey is the API key for the provider (can be from env var)
	APIKey string `json:"apiKey,omitempty"`

	// Endpoint is an optional custom API endpoint
	Endpoint string `json:"endpoint,omitempty"`

	// Model is the model to use (e.g., "gpt-4", "claude-3-opus")
	Model string `json:"model,omitempty"`

	// MaxTokens is the maximum tokens for responses
	MaxTokens int `json:"maxTokens,omitempty"`

	// Timeout is the request timeout in seconds
	Timeout int `json:"timeout,omitempty"`
}

// DefaultConfig returns a default configuration with NoOp provider
func DefaultConfig() Config {
	return Config{
		Provider:  "noop",
		MaxTokens: 4096,
		Timeout:   30,
	}
}
