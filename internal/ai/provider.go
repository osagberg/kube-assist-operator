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
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
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

	// ExplainMode, when true, uses ExplainContext as the prompt instead of building from Issues.
	ExplainMode bool `json:"explainMode,omitempty"`

	// ExplainContext is the raw prompt for explain mode.
	ExplainContext string `json:"explainContext,omitempty"`
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

	// CausalInsights contains AI analysis for causal correlation groups.
	CausalInsights []CausalGroupInsight `json:"causalInsights,omitempty"`

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

// CausalGroupInsight contains AI-generated analysis for a causal correlation group.
type CausalGroupInsight struct {
	// GroupID identifies which correlation group produced this insight (e.g., "group_0").
	GroupID string `json:"groupID"`

	// AIRootCause is the AI-generated deep root cause analysis.
	AIRootCause string `json:"aiRootCause"`

	// AISuggestion is the AI-generated actionable recommendation.
	AISuggestion string `json:"aiSuggestion"`

	// AISteps is an ordered list of remediation steps.
	AISteps []string `json:"aiSteps,omitempty"`

	// Confidence is the AI's confidence level (0-1).
	Confidence float64 `json:"confidence"`
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

	// MaxIssues caps the number of issues sent to AI per batch (0 = use default 15)
	MaxIssues int `json:"maxIssues,omitempty"`

	// MinConfidence is the minimum confidence threshold for AI suggestions (0 = use default 0.3)
	MinConfidence float64 `json:"minConfidence,omitempty"`
}

// DefaultConfig returns a default configuration with NoOp provider
func DefaultConfig() Config {
	return Config{
		Provider:      "noop",
		MaxTokens:     16384,
		Timeout:       60,
		MaxIssues:     15,
		MinConfidence: 0.3,
	}
}

// BuildPrompt constructs the user prompt for AI analysis from an AnalysisRequest.
// When ExplainMode is true, it returns the ExplainContext directly.
func BuildPrompt(request AnalysisRequest) string {
	if request.ExplainMode && request.ExplainContext != "" {
		return request.ExplainContext
	}

	contextJSON, _ := json.MarshalIndent(request.ClusterContext, "", "  ")

	var issueLines strings.Builder
	for i, issue := range request.Issues {
		issueJSON, _ := json.Marshal(issue)
		fmt.Fprintf(&issueLines, "issue_%d: %s\n", i, issueJSON)
	}

	prompt := fmt.Sprintf(`Analyze these Kubernetes health check issues and provide enhanced suggestions.
Use the exact issue key (issue_0, issue_1, ...) in your response.

Cluster Context:
%s

Issues:
%s`, contextJSON, issueLines.String())

	if request.CausalContext != nil && len(request.CausalContext.Groups) > 0 {
		var groupLines strings.Builder
		for i, group := range request.CausalContext.Groups {
			groupJSON, _ := json.Marshal(group)
			fmt.Fprintf(&groupLines, "group_%d: %s\n", i, groupJSON)
		}
		prompt += fmt.Sprintf(`

Causal Correlation Analysis (issues have been automatically grouped by likely root cause):
Total issues: %d, Uncorrelated: %d

Groups:
%s
Use the correlation data above to provide deeper root cause analysis. Correlated issues should be analyzed together.
For each group, use the exact group key (group_0, group_1, ...) as the "groupID" value in your causalInsights response.`,
			request.CausalContext.TotalIssues,
			request.CausalContext.UncorrelatedCount,
			groupLines.String())
	}

	prompt += "\n\nProvide a JSON response with enhanced suggestions for each issue."
	return prompt
}

var issueRefPattern = regexp.MustCompile(`\s*\((?:see\s+)?(?:issue|group)_\d+\)`)
var bareIssueRefPattern = regexp.MustCompile(`\b(?:issue|group)_\d+\b`)

// StripIssueReferences removes AI internal references (e.g., "issue_0", "group_1") from user-facing text.
func StripIssueReferences(text string) string {
	text = issueRefPattern.ReplaceAllString(text, "")
	text = bareIssueRefPattern.ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}

// ParseResponse parses raw AI response content into an AnalysisResponse.
func ParseResponse(content string, tokensUsed int, providerName string) *AnalysisResponse {
	var result struct {
		Suggestions    map[string]EnhancedSuggestion `json:"suggestions"`
		CausalInsights []CausalGroupInsight          `json:"causalInsights"`
		Summary        string                        `json:"summary"`
	}

	cleaned := extractJSON(content)
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		preview := content
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		log.Error(err, "Failed to parse AI response as JSON", "provider", providerName, "preview", preview)
		return &AnalysisResponse{
			EnhancedSuggestions: make(map[string]EnhancedSuggestion),
			Summary:             content,
			TokensUsed:          tokensUsed,
		}
	}

	// Strip issue/group references from user-facing text fields
	for k, s := range result.Suggestions {
		s.Suggestion = StripIssueReferences(s.Suggestion)
		s.RootCause = StripIssueReferences(s.RootCause)
		for j, step := range s.Steps {
			s.Steps[j] = StripIssueReferences(step)
		}
		result.Suggestions[k] = s
	}
	for i := range result.CausalInsights {
		result.CausalInsights[i].AIRootCause = StripIssueReferences(result.CausalInsights[i].AIRootCause)
		result.CausalInsights[i].AISuggestion = StripIssueReferences(result.CausalInsights[i].AISuggestion)
		for j, step := range result.CausalInsights[i].AISteps {
			result.CausalInsights[i].AISteps[j] = StripIssueReferences(step)
		}
	}

	// Clamp confidence values to 0.0-1.0
	for k, s := range result.Suggestions {
		if s.Confidence < 0 {
			s.Confidence = 0
		}
		if s.Confidence > 1 {
			s.Confidence = 1
		}
		result.Suggestions[k] = s
	}
	for i := range result.CausalInsights {
		if result.CausalInsights[i].Confidence < 0 {
			result.CausalInsights[i].Confidence = 0
		}
		if result.CausalInsights[i].Confidence > 1 {
			result.CausalInsights[i].Confidence = 1
		}
	}

	return &AnalysisResponse{
		EnhancedSuggestions: result.Suggestions,
		CausalInsights:      result.CausalInsights,
		Summary:             result.Summary,
		TokensUsed:          tokensUsed,
	}
}

// ExplainResponse contains an AI-generated narrative cluster health summary.
type ExplainResponse struct {
	// Narrative is a human-readable explanation of the cluster's current state
	Narrative string `json:"narrative"`

	// RiskLevel indicates the overall cluster risk: "low", "medium", "high", "critical"
	RiskLevel string `json:"riskLevel"`

	// TopIssues summarizes the most important issues
	TopIssues []ExplainIssue `json:"topIssues,omitempty"`

	// TrendDirection is "improving", "stable", or "degrading"
	TrendDirection string `json:"trendDirection"`

	// Confidence is the AI's confidence in this assessment (0-1)
	Confidence float64 `json:"confidence"`

	// TokensUsed tracks token consumption
	TokensUsed int `json:"tokensUsed"`
}

// ExplainIssue is a summarized issue for the explain response.
type ExplainIssue struct {
	Title    string `json:"title"`
	Severity string `json:"severity"`
	Impact   string `json:"impact"`
}

// BuildExplainPrompt constructs a prompt for the "Explain This Cluster" AI mode.
// It takes the current health data, causal analysis, and optionally health history snapshots.
func BuildExplainPrompt(healthResults map[string]any, causalCtx *CausalAnalysisContext, historySnapshots []any) string {
	var b strings.Builder

	b.WriteString("You are a Kubernetes cluster health advisor. Analyze the following cluster health data and provide a concise narrative explanation.\n\n")

	b.WriteString("Current Health Results:\n")
	healthJSON, _ := json.MarshalIndent(healthResults, "", "  ")
	b.Write(healthJSON)
	b.WriteString("\n")

	if causalCtx != nil && len(causalCtx.Groups) > 0 {
		b.WriteString("\nCausal Analysis:\n")
		causalJSON, _ := json.MarshalIndent(causalCtx.Groups, "", "  ")
		b.Write(causalJSON)
		b.WriteString("\n")
	}

	if len(historySnapshots) > 0 {
		b.WriteString("\nRecent Health Score History:\n")
		historyJSON, _ := json.MarshalIndent(historySnapshots, "", "  ")
		b.Write(historyJSON)
		b.WriteString("\n")
	}

	b.WriteString(`
Respond with a JSON object containing:
- "narrative": A 2-4 sentence human-readable summary of cluster health
- "riskLevel": one of "low", "medium", "high", "critical"
- "topIssues": array of {"title", "severity", "impact"} for top 3 issues
- "trendDirection": one of "improving", "stable", "degrading"
- "confidence": your confidence 0-1
`)

	return b.String()
}

// ParseExplainResponse parses raw AI response content into an ExplainResponse.
func ParseExplainResponse(content string, tokensUsed int) *ExplainResponse {
	var result ExplainResponse

	cleaned := extractJSON(content)
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		// On parse failure, return a response with just the narrative set to the raw content
		return &ExplainResponse{
			Narrative:      content,
			RiskLevel:      "unknown",
			TrendDirection: "unknown",
			TokensUsed:     tokensUsed,
		}
	}

	// Clamp confidence to 0-1
	if result.Confidence < 0 {
		result.Confidence = 0
	}
	if result.Confidence > 1 {
		result.Confidence = 1
	}

	result.TokensUsed = tokensUsed
	return &result
}
