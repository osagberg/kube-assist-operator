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
	"context"
	"strings"
	"testing"
)

func TestNoOpProvider_Name(t *testing.T) {
	p := NewNoOpProvider()
	if p.Name() != "noop" {
		t.Errorf("Name() = %s, want noop", p.Name())
	}
}

func TestNoOpProvider_Available(t *testing.T) {
	p := NewNoOpProvider()
	if !p.Available() {
		t.Error("Available() = false, want true")
	}
}

func TestNoOpProvider_Analyze(t *testing.T) {
	p := NewNoOpProvider()

	request := AnalysisRequest{
		Issues: []IssueContext{
			{
				Type:             "CrashLoopBackOff",
				Severity:         "critical",
				Resource:         "deployment/test",
				Namespace:        "default",
				Message:          "Container crashing",
				StaticSuggestion: "Check logs",
			},
		},
	}

	resp, err := p.Analyze(context.Background(), request)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if len(resp.EnhancedSuggestions) != 1 {
		t.Errorf("EnhancedSuggestions length = %d, want 1", len(resp.EnhancedSuggestions))
	}

	key := "issue_0"
	suggestion, ok := resp.EnhancedSuggestions[key]
	if !ok {
		t.Errorf("EnhancedSuggestions missing key %s", key)
		return
	}

	wantSuggestion := "[NoOp AI] Check logs"
	if suggestion.Suggestion != wantSuggestion {
		t.Errorf("Suggestion = %s, want %q", suggestion.Suggestion, wantSuggestion)
	}
	if suggestion.Confidence != 1.0 {
		t.Errorf("Confidence = %f, want 1.0", suggestion.Confidence)
	}
}

func TestOpenAIProvider_Name(t *testing.T) {
	p := NewOpenAIProvider(Config{})
	if p.Name() != "openai" {
		t.Errorf("Name() = %s, want openai", p.Name())
	}
}

func TestOpenAIProvider_Available(t *testing.T) {
	tests := []struct {
		name   string
		apiKey string
		want   bool
	}{
		{"empty key", "", false},
		{"with key", "test-key", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewOpenAIProvider(Config{APIKey: tt.apiKey})
			if p.Available() != tt.want {
				t.Errorf("Available() = %v, want %v", p.Available(), tt.want)
			}
		})
	}
}

func TestOpenAIProvider_Analyze_NotConfigured(t *testing.T) {
	p := NewOpenAIProvider(Config{})
	_, err := p.Analyze(context.Background(), AnalysisRequest{})
	if err != ErrNotConfigured {
		t.Errorf("Analyze() error = %v, want ErrNotConfigured", err)
	}
}

func TestAnthropicProvider_Name(t *testing.T) {
	p := NewAnthropicProvider(Config{})
	if p.Name() != "anthropic" {
		t.Errorf("Name() = %s, want anthropic", p.Name())
	}
}

func TestAnthropicProvider_Available(t *testing.T) {
	tests := []struct {
		name   string
		apiKey string
		want   bool
	}{
		{"empty key", "", false},
		{"with key", "test-key", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewAnthropicProvider(Config{APIKey: tt.apiKey})
			if p.Available() != tt.want {
				t.Errorf("Available() = %v, want %v", p.Available(), tt.want)
			}
		})
	}
}

func TestAnthropicProvider_Analyze_NotConfigured(t *testing.T) {
	p := NewAnthropicProvider(Config{})
	_, err := p.Analyze(context.Background(), AnalysisRequest{})
	if err != ErrNotConfigured {
		t.Errorf("Analyze() error = %v, want ErrNotConfigured", err)
	}
}

func TestNewProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		wantName string
		wantErr  bool
	}{
		{"noop", "noop", "noop", false},
		{"empty", "", "noop", false},
		{"openai", "openai", "openai", false},
		{"anthropic", "anthropic", "anthropic", false},
		{"unknown", "unknown", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _, _, err := NewProvider(Config{Provider: tt.provider})
			if (err != nil) != tt.wantErr {
				t.Errorf("NewProvider() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil && p.Name() != tt.wantName {
				t.Errorf("NewProvider() name = %s, want %s", p.Name(), tt.wantName)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Provider != "noop" {
		t.Errorf("Provider = %s, want noop", config.Provider)
	}
	if config.MaxTokens != 16384 {
		t.Errorf("MaxTokens = %d, want 16384", config.MaxTokens)
	}
	if config.Timeout != 60 {
		t.Errorf("Timeout = %d, want 60", config.Timeout)
	}
	if config.MaxIssues != 15 {
		t.Errorf("MaxIssues = %d, want 15", config.MaxIssues)
	}
	if config.MinConfidence != 0.3 {
		t.Errorf("MinConfidence = %f, want 0.3", config.MinConfidence)
	}
}

func TestParseResponse_ValidJSON(t *testing.T) {
	content := `{"suggestions":{"issue_0":{"suggestion":"Fix the deployment","rootCause":"Image not found","confidence":0.9,"severity":"critical","category":"ImagePull","steps":["Pull the correct image","Update deployment"]}},"summary":"One issue found"}`
	resp := ParseResponse(content, 500, "test")

	if resp.ParseFailed {
		t.Error("ParseFailed should be false for valid JSON")
	}
	if resp.TokensUsed != 500 {
		t.Errorf("TokensUsed = %d, want 500", resp.TokensUsed)
	}
	if len(resp.EnhancedSuggestions) != 1 {
		t.Fatalf("EnhancedSuggestions length = %d, want 1", len(resp.EnhancedSuggestions))
	}
	s := resp.EnhancedSuggestions["issue_0"]
	if s.Suggestion != "Fix the deployment" {
		t.Errorf("Suggestion = %q, want %q", s.Suggestion, "Fix the deployment")
	}
	if s.Confidence != 0.9 {
		t.Errorf("Confidence = %f, want 0.9", s.Confidence)
	}
}

func TestParseResponse_TruncatedJSON(t *testing.T) {
	content := `{"suggestions":{"issue_0":{"suggestion":"Fix`
	resp := ParseResponse(content, 100, "test")

	if !resp.ParseFailed {
		t.Error("ParseFailed should be true for truncated JSON")
	}
	if len(resp.EnhancedSuggestions) != 0 {
		t.Errorf("EnhancedSuggestions length = %d, want 0", len(resp.EnhancedSuggestions))
	}
}

func TestParseResponse_EmptySuggestions(t *testing.T) {
	content := `{"suggestions":{},"summary":"No issues to enhance"}`
	resp := ParseResponse(content, 200, "test")

	if resp.ParseFailed {
		t.Error("ParseFailed should be false for valid JSON with empty suggestions")
	}
	if len(resp.EnhancedSuggestions) != 0 {
		t.Errorf("EnhancedSuggestions length = %d, want 0", len(resp.EnhancedSuggestions))
	}
	if resp.Summary != "No issues to enhance" {
		t.Errorf("Summary = %q, want %q", resp.Summary, "No issues to enhance")
	}
}

func TestParseResponse_CodeFenced(t *testing.T) {
	content := "```json\n{\"suggestions\":{\"issue_0\":{\"suggestion\":\"Fix it\",\"confidence\":0.8}},\"summary\":\"ok\"}\n```"
	resp := ParseResponse(content, 300, "test")

	if resp.ParseFailed {
		t.Error("ParseFailed should be false for code-fenced JSON")
	}
	if len(resp.EnhancedSuggestions) != 1 {
		t.Fatalf("EnhancedSuggestions length = %d, want 1", len(resp.EnhancedSuggestions))
	}
}

func TestParseResponse_TruncatedCodeFence(t *testing.T) {
	// Missing closing ``` — extractJSON should handle this
	content := "```json\n{\"suggestions\":{\"issue_0\":{\"suggestion\":\"Fix it\",\"confidence\":0.8}},\"summary\":\"ok\"}"
	resp := ParseResponse(content, 300, "test")

	if resp.ParseFailed {
		t.Error("ParseFailed should be false for truncated fence with valid JSON")
	}
	if len(resp.EnhancedSuggestions) != 1 {
		t.Fatalf("EnhancedSuggestions length = %d, want 1", len(resp.EnhancedSuggestions))
	}
}

func TestParseResponse_InvalidEscapes(t *testing.T) {
	// Simulate Anthropic returning \| and \. in JSON strings
	content := `{"suggestions":{"issue_0":{"suggestion":"Check logs \| grep for errors","rootCause":"Config file path is \/etc\/app\.conf","confidence":0.85}},"summary":"Found issue"}`
	resp := ParseResponse(content, 400, "test")

	if resp.ParseFailed {
		t.Error("ParseFailed should be false — invalid escapes should be fixed")
	}
	s, ok := resp.EnhancedSuggestions["issue_0"]
	if !ok {
		t.Fatal("Missing issue_0 suggestion")
	}
	if !strings.Contains(s.Suggestion, "grep for errors") {
		t.Errorf("Suggestion should contain 'grep for errors', got %q", s.Suggestion)
	}
}

func TestParseResponse_ConfidenceClamping(t *testing.T) {
	content := `{"suggestions":{"issue_0":{"suggestion":"Fix","confidence":1.5},"issue_1":{"suggestion":"Fix2","confidence":-0.5}},"summary":"test"}`
	resp := ParseResponse(content, 100, "test")

	if resp.ParseFailed {
		t.Error("ParseFailed should be false")
	}
	if s, ok := resp.EnhancedSuggestions["issue_0"]; ok {
		if s.Confidence != 1.0 {
			t.Errorf("Confidence should be clamped to 1.0, got %f", s.Confidence)
		}
	}
	if s, ok := resp.EnhancedSuggestions["issue_1"]; ok {
		if s.Confidence != 0.0 {
			t.Errorf("Confidence should be clamped to 0.0, got %f", s.Confidence)
		}
	}
}

func TestParseResponse_StripIssueReferences(t *testing.T) {
	content := `{"suggestions":{"issue_0":{"suggestion":"Fix the pod (see issue_0) and check issue_1","confidence":0.8}},"summary":"test"}`
	resp := ParseResponse(content, 100, "test")

	s := resp.EnhancedSuggestions["issue_0"]
	if strings.Contains(s.Suggestion, "issue_0") || strings.Contains(s.Suggestion, "issue_1") {
		t.Errorf("Issue references should be stripped, got %q", s.Suggestion)
	}
}

func TestExtractJSON(t *testing.T) {
	const wantJSON = `{"key": "value"}`

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"code fenced", "Here is the analysis:\n```json\n{\"key\": \"value\"}\n```\nDone.", wantJSON},
		{"bare fenced", "```\n{\"key\": \"value\"}\n```", wantJSON},
		{"truncated fence", "```json\n{\"key\": \"value\"}", wantJSON},
		{"no fence", wantJSON, wantJSON},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.input)
			if got != tt.want {
				t.Errorf("extractJSON = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFixInvalidJSONEscapes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"backslash pipe", `hello \| world`, `hello | world`},
		{"backslash dot", `path\.conf`, `path.conf`},
		{"backslash colon", `time\: 12\:00`, `time: 12:00`},
		{"backslash semicolon", `a\;b`, `a;b`},
		{"preserves newline", `hello\nworld`, `hello\nworld`},
		{"preserves tab", `hello\tworld`, `hello\tworld`},
		{"preserves quote", `hello\"world`, `hello\"world`},
		{"preserves valid slash", `hello\/world`, `hello\/world`},
		{"preserves unicode", `hello\u0041world`, `hello\u0041world`},
		{"double backslash then char", `hello\\world`, `hello\world`},
		{"mixed valid and invalid", `path\: app\.conf`, `path: app.conf`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fixInvalidJSONEscapes(tt.input)
			if got != tt.want {
				t.Errorf("fixInvalidJSONEscapes(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
