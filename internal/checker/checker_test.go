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

package checker

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/osagberg/kube-assist-operator/internal/ai"
)

// ---------------------------------------------------------------------------
// Mock AI provider for tests
// ---------------------------------------------------------------------------

// mockProvider is a controllable AI provider for testing.
type mockProvider struct {
	name        string
	available   bool
	response    *ai.AnalysisResponse
	err         error
	lastRequest *ai.AnalysisRequest
}

func (m *mockProvider) Name() string    { return m.name }
func (m *mockProvider) Available() bool { return m.available }
func (m *mockProvider) Analyze(_ context.Context, req ai.AnalysisRequest) (*ai.AnalysisResponse, error) {
	m.lastRequest = &req
	return m.response, m.err
}

// ---------------------------------------------------------------------------
// Existing struct / method tests
// ---------------------------------------------------------------------------

func TestCheckResult_CountBySeverity(t *testing.T) {
	tests := []struct {
		name   string
		result *CheckResult
		want   map[string]int
	}{
		{
			name:   "empty issues",
			result: &CheckResult{Issues: []Issue{}},
			want:   map[string]int{},
		},
		{
			name: "mixed severities",
			result: &CheckResult{
				Issues: []Issue{
					{Severity: SeverityCritical},
					{Severity: SeverityCritical},
					{Severity: SeverityWarning},
					{Severity: SeverityInfo},
					{Severity: SeverityInfo},
					{Severity: SeverityInfo},
				},
			},
			want: map[string]int{
				SeverityCritical: 2,
				SeverityWarning:  1,
				SeverityInfo:     3,
			},
		},
		{
			name: "only critical",
			result: &CheckResult{
				Issues: []Issue{
					{Severity: SeverityCritical},
				},
			},
			want: map[string]int{
				SeverityCritical: 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.result.CountBySeverity()
			for severity, count := range tt.want {
				if got[severity] != count {
					t.Errorf("CountBySeverity()[%s] = %d, want %d", severity, got[severity], count)
				}
			}
		})
	}
}

func TestCheckResult_HasCritical(t *testing.T) {
	tests := []struct {
		name   string
		result *CheckResult
		want   bool
	}{
		{
			name:   "empty issues",
			result: &CheckResult{Issues: []Issue{}},
			want:   false,
		},
		{
			name: "has critical",
			result: &CheckResult{
				Issues: []Issue{
					{Severity: SeverityWarning},
					{Severity: SeverityCritical},
				},
			},
			want: true,
		},
		{
			name: "no critical",
			result: &CheckResult{
				Issues: []Issue{
					{Severity: SeverityWarning},
					{Severity: SeverityInfo},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.HasCritical(); got != tt.want {
				t.Errorf("HasCritical() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckResult_TotalIssues(t *testing.T) {
	tests := []struct {
		name   string
		result *CheckResult
		want   int
	}{
		{
			name:   "empty issues",
			result: &CheckResult{Issues: []Issue{}},
			want:   0,
		},
		{
			name: "multiple issues",
			result: &CheckResult{
				Issues: []Issue{
					{Type: "A"},
					{Type: "B"},
					{Type: "C"},
				},
			},
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.TotalIssues(); got != tt.want {
				t.Errorf("TotalIssues() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIssue_Fields(t *testing.T) {
	issue := Issue{
		Type:       "CrashLoopBackOff",
		Severity:   SeverityCritical,
		Resource:   "deployment/api-server",
		Namespace:  "production",
		Message:    "Container is crashing",
		Suggestion: "Check logs",
		Metadata: map[string]string{
			"container": "app",
			"pod":       "api-server-abc123",
		},
	}

	if issue.Type != "CrashLoopBackOff" {
		t.Errorf("Type = %s, want CrashLoopBackOff", issue.Type)
	}
	if issue.Severity != SeverityCritical {
		t.Errorf("Severity = %s, want %s", issue.Severity, SeverityCritical)
	}
	if issue.Resource != "deployment/api-server" {
		t.Errorf("Resource = %s, want deployment/api-server", issue.Resource)
	}
	if issue.Namespace != "production" {
		t.Errorf("Namespace = %s, want production", issue.Namespace)
	}
	if issue.Metadata["container"] != "app" {
		t.Errorf("Metadata[container] = %s, want app", issue.Metadata["container"])
	}
}

func TestNormalizeMessage(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "pod hash suffix",
			input: "Pod api-server-7f8b4c5d9f-x2k4p is crashing",
			want:  "Pod api-server-<pod> is crashing",
		},
		{
			name:  "no hash",
			input: "Deployment is not ready",
			want:  "Deployment is not ready",
		},
		{
			name:  "multiple spaces",
			input: "Pod  is   crashing",
			want:  "Pod is crashing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeMessage(tt.input); got != tt.want {
				t.Errorf("normalizeMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeIssueSignature(t *testing.T) {
	// Same type+severity+normalized message = same signature
	issue1 := Issue{
		Type:     "ImagePullBackOff",
		Severity: SeverityCritical,
		Message:  "Failed to pull image for pod api-7f8b4c5d9f-x2k4p",
	}
	issue2 := Issue{
		Type:     "ImagePullBackOff",
		Severity: SeverityCritical,
		Message:  "Failed to pull image for pod api-abc12def34-y3m5q",
	}
	sig1 := normalizeIssueSignature(issue1)
	sig2 := normalizeIssueSignature(issue2)
	if sig1 != sig2 {
		t.Errorf("Expected same signature for similar issues, got %q vs %q", sig1, sig2)
	}

	// Different type = different signature
	issue3 := Issue{
		Type:     "CrashLoopBackOff",
		Severity: SeverityCritical,
		Message:  "Failed to pull image for pod api-7f8b4c5d9f-x2k4p",
	}
	sig3 := normalizeIssueSignature(issue3)
	if sig1 == sig3 {
		t.Errorf("Expected different signatures for different types")
	}
}

// ---------------------------------------------------------------------------
// IsValidNamespace tests
// ---------------------------------------------------------------------------

func TestIsValidNamespace(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"simple lowercase", "default", true},
		{"with hyphens", "kube-system", true},
		{"with numbers", "ns1", true},
		{"alphanumeric mix", "my-ns-123", true},
		{"single char", "a", true},
		{"max length 63 chars", strings.Repeat("a", 63), true},
		{"empty string", "", false},
		{"uppercase letters", "Default", false},
		{"contains underscore", "my_namespace", false},
		{"starts with hyphen", "-invalid", false},
		{"ends with hyphen", "invalid-", false},
		{"too long (64 chars)", strings.Repeat("a", 64), false},
		{"contains dot", "my.namespace", false},
		{"contains space", "my namespace", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidNamespace(tt.input); got != tt.want {
				t.Errorf("IsValidNamespace(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// normalizeIssueSignature invalid namespace sanitization
// ---------------------------------------------------------------------------

func TestNormalizeIssueSignature_InvalidNamespace(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		wantNS    string
	}{
		{
			name:      "valid namespace preserved",
			namespace: "default",
			wantNS:    "default",
		},
		{
			name:      "uppercase namespace sanitized",
			namespace: "Default",
			wantNS:    "<invalid>",
		},
		{
			name:      "namespace with special chars sanitized",
			namespace: "ns_with_underscores",
			wantNS:    "<invalid>",
		},
		{
			name:      "empty namespace preserved",
			namespace: "",
			wantNS:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := Issue{
				Type:      "TestType",
				Severity:  SeverityWarning,
				Namespace: tt.namespace,
				Message:   "test message",
			}
			sig := normalizeIssueSignature(issue)
			wantSig := "TestType|Warning|" + tt.wantNS + "|test message"
			if sig != wantSig {
				t.Errorf("normalizeIssueSignature() = %q, want %q", sig, wantSig)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// severityRank tests
// ---------------------------------------------------------------------------

func TestSeverityRank(t *testing.T) {
	tests := []struct {
		severity string
		want     int
	}{
		{SeverityCritical, 0},
		{SeverityWarning, 1},
		{SeverityInfo, 2},
		{"Unknown", 3},
		{"", 3},
	}

	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			if got := severityRank(tt.severity); got != tt.want {
				t.Errorf("severityRank(%q) = %d, want %d", tt.severity, got, tt.want)
			}
		})
	}

	// Verify ordering: Critical < Warning < Info < Unknown
	if severityRank(SeverityCritical) >= severityRank(SeverityWarning) {
		t.Error("Critical should rank lower (higher priority) than Warning")
	}
	if severityRank(SeverityWarning) >= severityRank(SeverityInfo) {
		t.Error("Warning should rank lower (higher priority) than Info")
	}
}

// ---------------------------------------------------------------------------
// EnhanceWithAI tests
// ---------------------------------------------------------------------------

func TestEnhanceWithAI_NilProvider(t *testing.T) {
	result := &CheckResult{
		Issues: []Issue{
			{Type: "CrashLoop", Severity: SeverityCritical, Message: "crashing"},
		},
	}
	checkCtx := &CheckContext{
		AIEnabled:  true,
		AIProvider: nil,
	}

	err := result.EnhanceWithAI(context.Background(), checkCtx)
	if err != nil {
		t.Errorf("EnhanceWithAI() with nil provider should not error, got %v", err)
	}
	// Suggestion should be unchanged
	if result.Issues[0].Suggestion != "" {
		t.Errorf("Suggestion should remain empty, got %q", result.Issues[0].Suggestion)
	}
}

func TestEnhanceWithAI_Disabled(t *testing.T) {
	result := &CheckResult{
		Issues: []Issue{
			{Type: "CrashLoop", Severity: SeverityCritical, Message: "crashing", Suggestion: "check logs"},
		},
	}
	checkCtx := &CheckContext{
		AIEnabled:  false,
		AIProvider: &mockProvider{available: true},
	}

	err := result.EnhanceWithAI(context.Background(), checkCtx)
	if err != nil {
		t.Errorf("EnhanceWithAI() with AI disabled should not error, got %v", err)
	}
	if result.Issues[0].Suggestion != "check logs" {
		t.Errorf("Suggestion should remain unchanged, got %q", result.Issues[0].Suggestion)
	}
}

func TestEnhanceWithAI_ProviderNotAvailable(t *testing.T) {
	result := &CheckResult{
		Issues: []Issue{
			{Type: "CrashLoop", Severity: SeverityCritical, Message: "crashing"},
		},
	}
	checkCtx := &CheckContext{
		AIEnabled:  true,
		AIProvider: &mockProvider{available: false},
	}

	err := result.EnhanceWithAI(context.Background(), checkCtx)
	if err != nil {
		t.Errorf("EnhanceWithAI() with unavailable provider should not error, got %v", err)
	}
}

func TestEnhanceWithAI_EmptyIssues(t *testing.T) {
	result := &CheckResult{
		Issues: []Issue{},
	}
	checkCtx := &CheckContext{
		AIEnabled:  true,
		AIProvider: &mockProvider{available: true},
	}

	err := result.EnhanceWithAI(context.Background(), checkCtx)
	if err != nil {
		t.Errorf("EnhanceWithAI() with empty issues should not error, got %v", err)
	}
}

func TestEnhanceWithAI_HighConfidence(t *testing.T) {
	result := &CheckResult{
		Issues: []Issue{
			{
				Type:       "CrashLoop",
				Severity:   SeverityCritical,
				Resource:   "deployment/app",
				Namespace:  "default",
				Message:    "container crashing",
				Suggestion: "Check logs",
			},
		},
	}

	provider := &mockProvider{
		name:      "test",
		available: true,
		response: &ai.AnalysisResponse{
			EnhancedSuggestions: map[string]ai.EnhancedSuggestion{
				"issue_0": {
					Suggestion: "Increase memory limits",
					RootCause:  "OOM killed",
					Confidence: 0.9,
				},
			},
		},
	}

	checkCtx := &CheckContext{
		AIEnabled:  true,
		AIProvider: provider,
	}

	err := result.EnhanceWithAI(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("EnhanceWithAI() error = %v", err)
	}

	if !strings.Contains(result.Issues[0].Suggestion, "AI Analysis: Increase memory limits") {
		t.Errorf("Suggestion should contain AI analysis, got %q", result.Issues[0].Suggestion)
	}
	if result.Issues[0].Metadata["aiRootCause"] != "OOM killed" {
		t.Errorf("metadata aiRootCause = %q, want OOM killed", result.Issues[0].Metadata["aiRootCause"])
	}
}

func TestEnhanceWithAI_LowConfidenceFiltered(t *testing.T) {
	result := &CheckResult{
		Issues: []Issue{
			{
				Type:       "SomeIssue",
				Severity:   SeverityWarning,
				Resource:   "deployment/app",
				Namespace:  "default",
				Message:    "something wrong",
				Suggestion: "original suggestion",
			},
		},
	}

	provider := &mockProvider{
		name:      "test",
		available: true,
		response: &ai.AnalysisResponse{
			EnhancedSuggestions: map[string]ai.EnhancedSuggestion{
				"issue_0": {
					Suggestion: "low confidence suggestion",
					Confidence: 0.1, // Below default threshold of 0.3
				},
			},
		},
	}

	checkCtx := &CheckContext{
		AIEnabled:  true,
		AIProvider: provider,
	}

	err := result.EnhanceWithAI(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("EnhanceWithAI() error = %v", err)
	}

	// Suggestion should remain unchanged because confidence is below threshold
	if result.Issues[0].Suggestion != "original suggestion" {
		t.Errorf("Suggestion should remain unchanged for low confidence, got %q", result.Issues[0].Suggestion)
	}
}

func TestEnhanceWithAI_CustomConfidenceThreshold(t *testing.T) {
	result := &CheckResult{
		Issues: []Issue{
			{
				Type:       "SomeIssue",
				Severity:   SeverityWarning,
				Resource:   "deployment/app",
				Namespace:  "default",
				Message:    "something wrong",
				Suggestion: "original",
			},
		},
	}

	provider := &mockProvider{
		name:      "test",
		available: true,
		response: &ai.AnalysisResponse{
			EnhancedSuggestions: map[string]ai.EnhancedSuggestion{
				"issue_0": {
					Suggestion: "AI suggestion",
					Confidence: 0.5, // Between default 0.3 and custom 0.8
				},
			},
		},
	}

	checkCtx := &CheckContext{
		AIEnabled:     true,
		AIProvider:    provider,
		MinConfidence: 0.8, // Higher threshold
	}

	err := result.EnhanceWithAI(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("EnhanceWithAI() error = %v", err)
	}

	// Should be filtered by custom threshold
	if result.Issues[0].Suggestion != "original" {
		t.Errorf("Suggestion should remain unchanged with custom threshold, got %q", result.Issues[0].Suggestion)
	}
}

func TestEnhanceWithAI_LegacyKeyFormats(t *testing.T) {
	tests := []struct {
		name   string
		keyMap map[string]ai.EnhancedSuggestion
		wantAI bool
	}{
		{
			name: "legacy namespace/resource key",
			keyMap: map[string]ai.EnhancedSuggestion{
				"default/deployment/app": {
					Suggestion: "ns-resource key",
					Confidence: 0.9,
				},
			},
			wantAI: true,
		},
		{
			name: "legacy resource-only key",
			keyMap: map[string]ai.EnhancedSuggestion{
				"deployment/app": {
					Suggestion: "resource key",
					Confidence: 0.9,
				},
			},
			wantAI: true,
		},
		{
			name: "legacy type/resource key",
			keyMap: map[string]ai.EnhancedSuggestion{
				"CrashLoop/deployment/app": {
					Suggestion: "type-resource key",
					Confidence: 0.9,
				},
			},
			wantAI: true,
		},
		{
			name: "no matching key",
			keyMap: map[string]ai.EnhancedSuggestion{
				"nonexistent_key": {
					Suggestion: "unmatched",
					Confidence: 0.9,
				},
			},
			wantAI: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &CheckResult{
				Issues: []Issue{
					{
						Type:       "CrashLoop",
						Severity:   SeverityCritical,
						Resource:   "deployment/app",
						Namespace:  "default",
						Message:    "crashing",
						Suggestion: "original",
					},
				},
			}

			provider := &mockProvider{
				name:      "test",
				available: true,
				response: &ai.AnalysisResponse{
					EnhancedSuggestions: tt.keyMap,
				},
			}

			checkCtx := &CheckContext{
				AIEnabled:  true,
				AIProvider: provider,
			}

			err := result.EnhanceWithAI(context.Background(), checkCtx)
			if err != nil {
				t.Fatalf("EnhanceWithAI() error = %v", err)
			}

			hasAI := strings.Contains(result.Issues[0].Suggestion, "AI Analysis")
			if hasAI != tt.wantAI {
				t.Errorf("AI enhancement applied = %v, want %v (suggestion: %q)", hasAI, tt.wantAI, result.Issues[0].Suggestion)
			}
		})
	}
}

func TestEnhanceWithAI_ProviderError(t *testing.T) {
	result := &CheckResult{
		Issues: []Issue{
			{Type: "Err", Severity: SeverityCritical, Message: "test"},
		},
	}

	provider := &mockProvider{
		name:      "test",
		available: true,
		err:       fmt.Errorf("API rate limit exceeded"),
	}

	checkCtx := &CheckContext{
		AIEnabled:  true,
		AIProvider: provider,
	}

	err := result.EnhanceWithAI(context.Background(), checkCtx)
	if err == nil {
		t.Error("EnhanceWithAI() should return error when provider fails")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("error should contain provider error, got %v", err)
	}
}

func TestEnhanceWithAI_EmptySuggestionNotApplied(t *testing.T) {
	result := &CheckResult{
		Issues: []Issue{
			{
				Type:       "Issue",
				Severity:   SeverityWarning,
				Resource:   "deployment/app",
				Namespace:  "default",
				Message:    "test",
				Suggestion: "original",
			},
		},
	}

	provider := &mockProvider{
		name:      "test",
		available: true,
		response: &ai.AnalysisResponse{
			EnhancedSuggestions: map[string]ai.EnhancedSuggestion{
				"issue_0": {
					Suggestion: "", // empty suggestion
					Confidence: 0.9,
				},
			},
		},
	}

	checkCtx := &CheckContext{
		AIEnabled:  true,
		AIProvider: provider,
	}

	err := result.EnhanceWithAI(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("EnhanceWithAI() error = %v", err)
	}

	if result.Issues[0].Suggestion != "original" {
		t.Errorf("Empty AI suggestion should not be applied, got %q", result.Issues[0].Suggestion)
	}
}

func TestEnhanceWithAI_NoExistingSuggestion(t *testing.T) {
	result := &CheckResult{
		Issues: []Issue{
			{
				Type:     "Issue",
				Severity: SeverityWarning,
				Resource: "deployment/app",
				Message:  "test",
				// No existing suggestion
			},
		},
	}

	provider := &mockProvider{
		name:      "test",
		available: true,
		response: &ai.AnalysisResponse{
			EnhancedSuggestions: map[string]ai.EnhancedSuggestion{
				"issue_0": {
					Suggestion: "AI-only suggestion",
					Confidence: 0.9,
				},
			},
		},
	}

	checkCtx := &CheckContext{
		AIEnabled:  true,
		AIProvider: provider,
	}

	err := result.EnhanceWithAI(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("EnhanceWithAI() error = %v", err)
	}

	if result.Issues[0].Suggestion != "AI Analysis: AI-only suggestion" {
		t.Errorf("Expected AI-only suggestion when no existing, got %q", result.Issues[0].Suggestion)
	}
}

// ---------------------------------------------------------------------------
// EnhanceAllWithAI tests
// ---------------------------------------------------------------------------

func TestEnhanceAllWithAI_NilProvider(t *testing.T) {
	results := map[string]*CheckResult{
		"test": {Issues: []Issue{{Severity: SeverityCritical, Message: "test"}}},
	}
	checkCtx := &CheckContext{
		AIEnabled:  true,
		AIProvider: nil,
	}

	enhanced, tokens, total, resp, err := EnhanceAllWithAI(context.Background(), results, checkCtx)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if enhanced != 0 || tokens != 0 || total != 0 || resp != nil {
		t.Errorf("expected zeros for nil provider, got enhanced=%d tokens=%d total=%d resp=%v",
			enhanced, tokens, total, resp)
	}
}

func TestEnhanceAllWithAI_Disabled(t *testing.T) {
	results := map[string]*CheckResult{
		"test": {Issues: []Issue{{Severity: SeverityCritical, Message: "test"}}},
	}
	checkCtx := &CheckContext{
		AIEnabled:  false,
		AIProvider: &mockProvider{available: true},
	}

	enhanced, _, _, _, err := EnhanceAllWithAI(context.Background(), results, checkCtx)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if enhanced != 0 {
		t.Errorf("expected 0 enhanced, got %d", enhanced)
	}
}

func TestEnhanceAllWithAI_InfoSeverityFiltered(t *testing.T) {
	results := map[string]*CheckResult{
		"test": {
			Issues: []Issue{
				{Type: "A", Severity: SeverityInfo, Message: "info issue"},
				{Type: "B", Severity: SeverityCritical, Message: "critical issue"},
			},
		},
	}

	provider := &mockProvider{
		name:      "test",
		available: true,
		response: &ai.AnalysisResponse{
			EnhancedSuggestions: map[string]ai.EnhancedSuggestion{
				"issue_0": {
					Suggestion: "AI for critical",
					Confidence: 0.9,
				},
			},
			TokensUsed: 100,
		},
	}

	checkCtx := &CheckContext{
		AIEnabled:  true,
		AIProvider: provider,
	}

	enhanced, _, _, _, err := EnhanceAllWithAI(context.Background(), results, checkCtx)
	if err != nil {
		t.Fatalf("EnhanceAllWithAI() error = %v", err)
	}

	if enhanced != 1 {
		t.Errorf("expected 1 enhanced (only critical), got %d", enhanced)
	}

	// The info issue should NOT have AI enhancement
	if strings.Contains(results["test"].Issues[0].Suggestion, "AI Analysis") {
		t.Error("Info severity issue should not get AI enhancement")
	}
	// The critical issue should have AI enhancement
	if !strings.Contains(results["test"].Issues[1].Suggestion, "AI Analysis") {
		t.Errorf("Critical issue should have AI enhancement, got %q", results["test"].Issues[1].Suggestion)
	}
}

func TestEnhanceAllWithAI_IssueCapping(t *testing.T) {
	// Create more issues than the default cap
	issues := make([]Issue, 20)
	for i := range issues {
		issues[i] = Issue{
			Type:     fmt.Sprintf("Issue_%d", i),
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("issue %d", i),
		}
	}
	// Make the first two critical so they sort first
	issues[0].Severity = SeverityCritical
	issues[1].Severity = SeverityCritical

	results := map[string]*CheckResult{
		"test": {Issues: issues},
	}

	provider := &mockProvider{
		name:      "test",
		available: true,
		response: &ai.AnalysisResponse{
			EnhancedSuggestions: make(map[string]ai.EnhancedSuggestion),
			TokensUsed:          200,
		},
	}

	checkCtx := &CheckContext{
		AIEnabled:  true,
		AIProvider: provider,
		MaxIssues:  5,
	}

	_, _, total, _, err := EnhanceAllWithAI(context.Background(), results, checkCtx)
	if err != nil {
		t.Fatalf("EnhanceAllWithAI() error = %v", err)
	}

	if total != 20 {
		t.Errorf("total issues = %d, want 20", total)
	}

	// Verify only MaxIssues were sent to the provider
	if provider.lastRequest != nil && len(provider.lastRequest.Issues) > 5 {
		t.Errorf("sent %d issues to provider, want <= 5", len(provider.lastRequest.Issues))
	}
}

func TestEnhanceAllWithAI_Dedup(t *testing.T) {
	// Two issues with the same normalized signature
	results := map[string]*CheckResult{
		"test": {
			Issues: []Issue{
				{
					Type:     "CrashLoop",
					Severity: SeverityCritical,
					Message:  "Pod api-7f8b4c5d9f-x2k4p is crashing",
				},
				{
					Type:     "CrashLoop",
					Severity: SeverityCritical,
					Message:  "Pod api-abc12def34-y3m5q is crashing",
				},
			},
		},
	}

	provider := &mockProvider{
		name:      "test",
		available: true,
		response: &ai.AnalysisResponse{
			EnhancedSuggestions: map[string]ai.EnhancedSuggestion{
				"issue_0": {
					Suggestion: "Fix the crashloop",
					RootCause:  "OOM",
					Confidence: 0.9,
				},
			},
			TokensUsed: 50,
		},
	}

	checkCtx := &CheckContext{
		AIEnabled:  true,
		AIProvider: provider,
	}

	enhanced, _, _, _, err := EnhanceAllWithAI(context.Background(), results, checkCtx)
	if err != nil {
		t.Fatalf("EnhanceAllWithAI() error = %v", err)
	}

	// Both issues should be enhanced via fan-out
	if enhanced != 2 {
		t.Errorf("expected 2 enhanced (fan-out from dedup), got %d", enhanced)
	}

	// Only 1 unique issue should be sent to the provider
	if provider.lastRequest != nil && len(provider.lastRequest.Issues) != 1 {
		t.Errorf("dedup should send 1 unique issue, got %d", len(provider.lastRequest.Issues))
	}

	// Both issues should have the same AI suggestion
	for i, issue := range results["test"].Issues {
		if !strings.Contains(issue.Suggestion, "AI Analysis: Fix the crashloop") {
			t.Errorf("Issue %d should have AI suggestion after fan-out, got %q", i, issue.Suggestion)
		}
	}
}

func TestEnhanceAllWithAI_ProviderError(t *testing.T) {
	results := map[string]*CheckResult{
		"test": {
			Issues: []Issue{
				{Type: "Issue", Severity: SeverityCritical, Message: "test"},
			},
		},
	}

	provider := &mockProvider{
		name:      "test",
		available: true,
		err:       fmt.Errorf("connection timeout"),
	}

	checkCtx := &CheckContext{
		AIEnabled:  true,
		AIProvider: provider,
	}

	_, _, total, _, err := EnhanceAllWithAI(context.Background(), results, checkCtx)
	if err == nil {
		t.Error("expected error when provider fails")
	}
	if total != 1 {
		t.Errorf("total should reflect original count, got %d", total)
	}
}

func TestEnhanceAllWithAI_AllInfoFiltered(t *testing.T) {
	// All issues are info severity - should skip AI entirely
	results := map[string]*CheckResult{
		"test": {
			Issues: []Issue{
				{Type: "A", Severity: SeverityInfo, Message: "info 1"},
				{Type: "B", Severity: SeverityInfo, Message: "info 2"},
			},
		},
	}

	provider := &mockProvider{
		name:      "test",
		available: true,
		response: &ai.AnalysisResponse{
			EnhancedSuggestions: make(map[string]ai.EnhancedSuggestion),
		},
	}

	checkCtx := &CheckContext{
		AIEnabled:  true,
		AIProvider: provider,
	}

	enhanced, _, _, _, err := EnhanceAllWithAI(context.Background(), results, checkCtx)
	if err != nil {
		t.Fatalf("EnhanceAllWithAI() error = %v", err)
	}

	if enhanced != 0 {
		t.Errorf("expected 0 enhanced for all-info issues, got %d", enhanced)
	}

	// Provider should not have been called (no request recorded)
	if provider.lastRequest != nil {
		t.Error("provider should not be called when all issues are filtered")
	}
}

func TestEnhanceAllWithAI_ErroredResultsSkipped(t *testing.T) {
	results := map[string]*CheckResult{
		"good": {
			Issues: []Issue{
				{Type: "A", Severity: SeverityCritical, Message: "good issue"},
			},
		},
		"bad": {
			Issues: []Issue{
				{Type: "B", Severity: SeverityCritical, Message: "bad issue"},
			},
			Error: fmt.Errorf("checker failed"),
		},
	}

	provider := &mockProvider{
		name:      "test",
		available: true,
		response: &ai.AnalysisResponse{
			EnhancedSuggestions: map[string]ai.EnhancedSuggestion{
				"issue_0": {
					Suggestion: "AI fix",
					Confidence: 0.9,
				},
			},
			TokensUsed: 50,
		},
	}

	checkCtx := &CheckContext{
		AIEnabled:  true,
		AIProvider: provider,
	}

	enhanced, _, _, _, err := EnhanceAllWithAI(context.Background(), results, checkCtx)
	if err != nil {
		t.Fatalf("EnhanceAllWithAI() error = %v", err)
	}

	// Only the "good" result's issue should be enhanced
	if enhanced != 1 {
		t.Errorf("expected 1 enhanced, got %d", enhanced)
	}
}

func TestEnhanceAllWithAI_ConfidenceFilterInBatch(t *testing.T) {
	results := map[string]*CheckResult{
		"test": {
			Issues: []Issue{
				{Type: "A", Severity: SeverityCritical, Message: "issue A", Suggestion: "orig A"},
				{Type: "B", Severity: SeverityCritical, Message: "issue B", Suggestion: "orig B"},
			},
		},
	}

	provider := &mockProvider{
		name:      "test",
		available: true,
		response: &ai.AnalysisResponse{
			EnhancedSuggestions: map[string]ai.EnhancedSuggestion{
				"issue_0": {
					Suggestion: "high confidence",
					Confidence: 0.9,
				},
				"issue_1": {
					Suggestion: "low confidence",
					Confidence: 0.1,
				},
			},
			TokensUsed: 100,
		},
	}

	checkCtx := &CheckContext{
		AIEnabled:  true,
		AIProvider: provider,
	}

	enhanced, _, _, _, err := EnhanceAllWithAI(context.Background(), results, checkCtx)
	if err != nil {
		t.Fatalf("EnhanceAllWithAI() error = %v", err)
	}

	// Only the high-confidence issue should be enhanced
	if enhanced != 1 {
		t.Errorf("expected 1 enhanced (1 filtered by confidence), got %d", enhanced)
	}

	if !strings.Contains(results["test"].Issues[0].Suggestion, "high confidence") {
		t.Errorf("Issue A should have AI enhancement, got %q", results["test"].Issues[0].Suggestion)
	}
	if strings.Contains(results["test"].Issues[1].Suggestion, "low confidence") {
		t.Errorf("Issue B should NOT have AI enhancement, got %q", results["test"].Issues[1].Suggestion)
	}
}

func TestEnhanceAllWithAI_MultipleCheckers(t *testing.T) {
	results := map[string]*CheckResult{
		"checker1": {
			Issues: []Issue{
				{Type: "A", Severity: SeverityCritical, Message: "checker1 issue", Suggestion: "s1"},
			},
		},
		"checker2": {
			Issues: []Issue{
				{Type: "B", Severity: SeverityWarning, Message: "checker2 issue", Suggestion: "s2"},
			},
		},
	}

	provider := &mockProvider{
		name:      "test",
		available: true,
		response: &ai.AnalysisResponse{
			EnhancedSuggestions: map[string]ai.EnhancedSuggestion{
				"issue_0": {
					Suggestion: "fix for issue 0",
					RootCause:  "root cause 0",
					Confidence: 0.9,
				},
				"issue_1": {
					Suggestion: "fix for issue 1",
					Confidence: 0.8,
				},
			},
			TokensUsed: 150,
		},
	}

	checkCtx := &CheckContext{
		AIEnabled:  true,
		AIProvider: provider,
	}

	enhanced, tokens, _, _, err := EnhanceAllWithAI(context.Background(), results, checkCtx)
	if err != nil {
		t.Fatalf("EnhanceAllWithAI() error = %v", err)
	}

	if enhanced != 2 {
		t.Errorf("expected 2 enhanced, got %d", enhanced)
	}
	if tokens != 150 {
		t.Errorf("expected 150 tokens, got %d", tokens)
	}
}
