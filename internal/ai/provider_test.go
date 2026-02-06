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
			p, err := NewProvider(Config{Provider: tt.provider})
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
	if config.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096", config.MaxTokens)
	}
	if config.Timeout != 30 {
		t.Errorf("Timeout = %d, want 30", config.Timeout)
	}
}
