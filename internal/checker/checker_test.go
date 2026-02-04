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
