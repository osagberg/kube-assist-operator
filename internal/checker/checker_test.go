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
	"testing"
)

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
