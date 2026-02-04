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

package ai

import (
	"strings"
	"testing"
)

func TestSanitizer_SanitizeString(t *testing.T) {
	s := NewSanitizer()

	tests := []struct {
		name     string
		input    string
		contains string
		notContains string
	}{
		{
			name:        "API key",
			input:       "Error: api_key=sk-12345abcdef",
			notContains: "sk-12345abcdef",
		},
		{
			name:        "Bearer token",
			input:       "Authorization: Bearer abc123xyz",
			notContains: "abc123xyz",
		},
		{
			name:        "Password",
			input:       "password: mysecretpassword",
			notContains: "mysecretpassword",
		},
		{
			name:        "AWS access key",
			input:       "AWS_ACCESS_KEY=AKIAIOSFODNN7EXAMPLE",
			notContains: "AKIAIOSFODNN7EXAMPLE",
		},
		{
			name:        "Email",
			input:       "Contact: user@example.com",
			notContains: "user@example.com",
		},
		{
			name:        "Internal IP",
			input:       "Connecting to 192.168.1.100",
			notContains: "192.168.1.100",
		},
		{
			name:        "JWT token",
			input:       "Token: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
			notContains: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
		},
		{
			name:     "Safe text",
			input:    "Deployment my-app is not ready",
			contains: "Deployment my-app is not ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.SanitizeString(tt.input)

			if tt.notContains != "" && strings.Contains(result, tt.notContains) {
				t.Errorf("SanitizeString() should not contain %q, got %q", tt.notContains, result)
			}

			if tt.contains != "" && !strings.Contains(result, tt.contains) {
				t.Errorf("SanitizeString() should contain %q, got %q", tt.contains, result)
			}
		})
	}
}

func TestSanitizer_SanitizeStrings(t *testing.T) {
	s := NewSanitizer()

	inputs := []string{
		"Normal log message",
		"Secret: api_key=abc123",
		"Another normal message",
	}

	results := s.SanitizeStrings(inputs)

	if len(results) != 3 {
		t.Fatalf("SanitizeStrings() returned %d results, want 3", len(results))
	}

	if results[0] != "Normal log message" {
		t.Errorf("First message changed: %s", results[0])
	}

	if strings.Contains(results[1], "abc123") {
		t.Errorf("Second message should be sanitized: %s", results[1])
	}
}

func TestSanitizer_SanitizeResourceName(t *testing.T) {
	s := NewSanitizer()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "normal deployment",
			input: "deployment/my-app",
			want:  "deployment/my-app",
		},
		{
			name:  "secret resource",
			input: "secret/my-database-credentials",
			want:  "secret/secret-[REDACTED]",
		},
		{
			name:  "configmap with token",
			input: "configmap/api-token-config",
			want:  "configmap/configmap-[REDACTED]",
		},
		{
			name:  "no slash",
			input: "my-deployment",
			want:  "my-deployment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.SanitizeResourceName(tt.input)
			if got != tt.want {
				t.Errorf("SanitizeResourceName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizer_SanitizeIssueContext(t *testing.T) {
	s := NewSanitizer()

	issue := IssueContext{
		Type:             "CrashLoopBackOff",
		Severity:         "critical",
		Resource:         "deployment/my-app",
		Namespace:        "default",
		Message:          "Container crashed with api_key=secret123",
		StaticSuggestion: "Check logs",
		Events:           []string{"Normal event", "Secret: token=abc"},
		Logs:             []string{"Normal log", "password: mypass"},
	}

	sanitized := s.SanitizeIssueContext(issue)

	if strings.Contains(sanitized.Message, "secret123") {
		t.Error("Message should be sanitized")
	}

	if sanitized.StaticSuggestion != "Check logs" {
		t.Error("StaticSuggestion should not be modified")
	}

	for _, event := range sanitized.Events {
		if strings.Contains(event, "abc") {
			t.Errorf("Event should be sanitized: %s", event)
		}
	}

	for _, log := range sanitized.Logs {
		if strings.Contains(log, "mypass") {
			t.Errorf("Log should be sanitized: %s", log)
		}
	}
}

func TestSanitizer_SanitizeAnalysisRequest(t *testing.T) {
	s := NewSanitizer()

	request := AnalysisRequest{
		Issues: []IssueContext{
			{
				Type:    "Error",
				Message: "password=secret",
			},
		},
		ClusterContext: ClusterContext{
			KubernetesVersion: "1.28",
			Namespaces:        []string{"default"},
		},
		MaxTokens: 1000,
	}

	sanitized := s.SanitizeAnalysisRequest(request)

	if strings.Contains(sanitized.Issues[0].Message, "secret") {
		t.Error("Issue message should be sanitized")
	}

	if sanitized.ClusterContext.KubernetesVersion != "1.28" {
		t.Error("ClusterContext should be preserved")
	}

	if sanitized.MaxTokens != 1000 {
		t.Error("MaxTokens should be preserved")
	}
}

func TestSanitizer_AddPattern(t *testing.T) {
	s := NewSanitizer()

	// Add custom pattern for order IDs
	err := s.AddPattern(`ORDER-\d+`)
	if err != nil {
		t.Fatalf("AddPattern() error = %v", err)
	}

	input := "Processing ORDER-12345"
	result := s.SanitizeString(input)

	if strings.Contains(result, "ORDER-12345") {
		t.Errorf("Custom pattern should sanitize ORDER-12345, got %s", result)
	}
}

func TestSanitizer_AddPattern_Invalid(t *testing.T) {
	s := NewSanitizer()

	err := s.AddPattern(`[invalid`)
	if err == nil {
		t.Error("AddPattern() should return error for invalid regex")
	}
}
