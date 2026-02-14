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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const (
	testToolAnalyze = "analyze_issues"
	testToolExplain = "explain_cluster"
)

func TestAnthropic_ToolUse_Success(t *testing.T) {
	toolInput := `{"suggestions":{"issue_0":{"suggestion":"Fix the deployment","rootCause":"Image not found","steps":["Pull the correct image"],"references":["https://kubernetes.io/docs/"],"confidence":0.9}},"causalInsights":[],"summary":"One issue found"}`

	var receivedReq anthropicRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedReq)

		resp := anthropicResponse{
			ID:         "msg-test",
			Type:       "message",
			StopReason: "tool_use",
			Content: []anthropicContentBlock{
				{
					Type:  "tool_use",
					ID:    "toolu_test",
					Name:  testToolAnalyze,
					Input: json.RawMessage(toolInput),
				},
			},
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{InputTokens: 300, OutputTokens: 200},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider(Config{
		APIKey:   "test-key",
		Endpoint: server.URL,
	})

	result, err := provider.Analyze(context.Background(), AnalysisRequest{
		Issues: []IssueContext{
			{Type: "CrashLoopBackOff", Severity: "critical", Resource: "deployment/test", Message: "Container crashing"},
		},
	})

	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	// Verify the request included tools
	if len(receivedReq.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(receivedReq.Tools))
	}
	if receivedReq.Tools[0].Name != testToolAnalyze {
		t.Errorf("tool name = %q, want analyze_issues", receivedReq.Tools[0].Name)
	}
	if receivedReq.ToolChoice == nil {
		t.Fatal("tool_choice should not be nil")
	}
	if receivedReq.ToolChoice.Type != "tool" { //nolint:goconst // API value
		t.Errorf("tool_choice.type = %q, want tool", receivedReq.ToolChoice.Type)
	}
	if receivedReq.ToolChoice.Name != testToolAnalyze {
		t.Errorf("tool_choice.name = %q, want analyze_issues", receivedReq.ToolChoice.Name)
	}

	// Verify the response was parsed correctly
	if result.ParseFailed {
		t.Error("ParseFailed should be false")
	}
	if len(result.EnhancedSuggestions) != 1 {
		t.Errorf("EnhancedSuggestions length = %d, want 1", len(result.EnhancedSuggestions))
	}
	if result.TokensUsed != 500 {
		t.Errorf("TokensUsed = %d, want 500", result.TokensUsed)
	}
}

func TestAnthropic_ToolUse_Fallback(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)

		var req anthropicRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		if count == 1 {
			// Tool use mode — return 400 to simulate unsupported
			if len(req.Tools) == 0 {
				t.Error("first request should have tools")
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"tools not supported"}}`))
			return
		}

		// Fallback mode — no tools
		if len(req.Tools) > 0 {
			t.Error("fallback request should not have tools")
		}
		if req.ToolChoice != nil {
			t.Error("fallback request should not have tool_choice")
		}

		respPayload := `{"suggestions":{"issue_0":{"suggestion":"Fix it","rootCause":"Bad config","steps":[],"references":[],"confidence":0.8}},"causalInsights":[],"summary":"Fixed"}`

		resp := anthropicResponse{
			ID:         "msg-test",
			Type:       "message",
			StopReason: "end_turn",
			Content: []anthropicContentBlock{
				{Type: "text", Text: respPayload},
			},
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{InputTokens: 200, OutputTokens: 100},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider(Config{
		APIKey:   "test-key",
		Endpoint: server.URL,
	})

	result, err := provider.Analyze(context.Background(), AnalysisRequest{
		Issues: []IssueContext{
			{Type: "CrashLoopBackOff", Severity: "critical", Resource: "deployment/test", Message: "Container crashing"},
		},
	})

	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if callCount.Load() != 2 {
		t.Errorf("expected 2 API calls (tool_use + fallback), got %d", callCount.Load())
	}

	if result.ParseFailed {
		t.Error("ParseFailed should be false after fallback")
	}
	if len(result.EnhancedSuggestions) != 1 {
		t.Errorf("EnhancedSuggestions length = %d, want 1", len(result.EnhancedSuggestions))
	}
}

func TestAnthropic_ToolUse_ExplainMode(t *testing.T) {
	var receivedReq anthropicRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedReq)

		toolInput := `{"narrative":"Cluster is healthy","riskLevel":"low","topIssues":[],"trendDirection":"stable","confidence":0.9}`

		resp := anthropicResponse{
			ID:         "msg-test",
			Type:       "message",
			StopReason: "tool_use",
			Content: []anthropicContentBlock{
				{
					Type:  "tool_use",
					ID:    "toolu_test",
					Name:  testToolExplain,
					Input: json.RawMessage(toolInput),
				},
			},
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{InputTokens: 200, OutputTokens: 100},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider(Config{
		APIKey:   "test-key",
		Endpoint: server.URL,
	})

	_, err := provider.Analyze(context.Background(), AnalysisRequest{
		ExplainMode:    true,
		ExplainContext: "Explain the cluster health",
	})

	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if len(receivedReq.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(receivedReq.Tools))
	}
	if receivedReq.Tools[0].Name != testToolExplain {
		t.Errorf("tool name = %q, want explain_cluster", receivedReq.Tools[0].Name)
	}
	if receivedReq.ToolChoice == nil || receivedReq.ToolChoice.Name != testToolExplain {
		t.Error("tool_choice should force explain_cluster")
	}

	// Verify the tool has the explain schema
	schema := receivedReq.Tools[0].InputSchema
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("input_schema.properties is not a map")
	}
	if _, ok := props["narrative"]; !ok {
		t.Error("explain schema should contain narrative property")
	}
}

func TestAnthropic_DoRequest_StatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    string
	}{
		{
			name:       "400 Bad Request",
			statusCode: http.StatusBadRequest,
			body:       `{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`,
			wantErr:    "API error (status 400)",
		},
		{
			name:       "429 Rate Limited",
			statusCode: http.StatusTooManyRequests,
			body:       `{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`,
			wantErr:    "API error (status 429)",
		},
		{
			name:       "500 Internal Error",
			statusCode: http.StatusInternalServerError,
			body:       `{"type":"error","error":{"type":"api_error","message":"internal error"}}`,
			wantErr:    "API error (status 500)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			provider := NewAnthropicProvider(Config{
				APIKey:   "test-key",
				Endpoint: server.URL,
			})

			_, err := provider.doAnthropicRequest(context.Background(), anthropicRequest{
				Model:     "test",
				MaxTokens: 1000,
				Messages:  []anthropicMessage{{Role: "user", Content: "test"}},
			})

			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestAnthropic_DoRequest_TextContentExtraction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := anthropicResponse{
			ID:         "msg-test",
			Type:       "message",
			StopReason: "end_turn",
			Content: []anthropicContentBlock{
				{Type: "text", Text: `{"suggestions":{},"causalInsights":[],"summary":"test"}`},
			},
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{InputTokens: 100, OutputTokens: 50},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider(Config{
		APIKey:   "test-key",
		Endpoint: server.URL,
	})

	result, err := provider.doAnthropicRequest(context.Background(), anthropicRequest{
		Model:     "test",
		MaxTokens: 1000,
		Messages:  []anthropicMessage{{Role: "user", Content: "test"}},
	})

	if err != nil {
		t.Fatalf("doAnthropicRequest() error = %v", err)
	}
	if result.TokensUsed != 150 {
		t.Errorf("TokensUsed = %d, want 150", result.TokensUsed)
	}
	if result.Truncated {
		t.Error("Truncated should be false for end_turn")
	}
}

func TestAnthropic_DoRequest_Truncated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := anthropicResponse{
			ID:         "msg-test",
			Type:       "message",
			StopReason: "max_tokens",
			Content: []anthropicContentBlock{
				{Type: "text", Text: `{"suggestions":{},"summary":"truncated`},
			},
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{InputTokens: 100, OutputTokens: 16384},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider(Config{
		APIKey:   "test-key",
		Endpoint: server.URL,
	})

	result, err := provider.doAnthropicRequest(context.Background(), anthropicRequest{
		Model:     "test",
		MaxTokens: 16384,
		Messages:  []anthropicMessage{{Role: "user", Content: "test"}},
	})

	if err != nil {
		t.Fatalf("doAnthropicRequest() error = %v", err)
	}
	if !result.Truncated {
		t.Error("Truncated should be true for stop_reason=max_tokens")
	}
}

func TestAnthropic_ToolUse_ToolUsePrioritizedOverText(t *testing.T) {
	// When both text and tool_use blocks are present, tool_use should be preferred
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		toolInput := `{"suggestions":{"issue_0":{"suggestion":"From tool","rootCause":"Tool root","steps":[],"references":[],"confidence":0.95}},"causalInsights":[],"summary":"From tool use"}`

		resp := anthropicResponse{
			ID:         "msg-test",
			Type:       "message",
			StopReason: "tool_use",
			Content: []anthropicContentBlock{
				{Type: "text", Text: "Some preamble text"},
				{
					Type:  "tool_use",
					ID:    "toolu_test",
					Name:  testToolAnalyze,
					Input: json.RawMessage(toolInput),
				},
			},
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{InputTokens: 200, OutputTokens: 150},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider(Config{
		APIKey:   "test-key",
		Endpoint: server.URL,
	})

	result, err := provider.Analyze(context.Background(), AnalysisRequest{
		Issues: []IssueContext{
			{Type: "CrashLoopBackOff", Severity: "critical", Resource: "deployment/test", Message: "test"},
		},
	})

	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if result.ParseFailed {
		t.Error("ParseFailed should be false")
	}
	if result.Summary != "From tool use" {
		t.Errorf("Summary = %q, want 'From tool use' (from tool_use block, not text)", result.Summary)
	}
}

func TestAnthropic_RequestSerialization_WithTools(t *testing.T) {
	req := anthropicRequest{
		Model:     "claude-haiku-4-5-20251001",
		MaxTokens: 16384,
		System: []systemBlock{
			{Type: "text", Text: "test system prompt"},
		},
		Messages: []anthropicMessage{
			{Role: "user", Content: "test user prompt"},
		},
		Tools: []anthropicTool{
			{
				Name:        testToolAnalyze,
				Description: "Provide structured analysis",
				InputSchema: AnalysisResponseSchema(),
			},
		},
		ToolChoice: &anthropicToolChoice{
			Type: "tool",
			Name: testToolAnalyze,
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	tools, ok := parsed["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatal("tools should be array with 1 element")
	}

	tool := tools[0].(map[string]any)
	if tool["name"] != testToolAnalyze {
		t.Errorf("tool.name = %v, want analyze_issues", tool["name"])
	}

	toolChoice, ok := parsed["tool_choice"].(map[string]any)
	if !ok {
		t.Fatal("tool_choice missing")
	}
	if toolChoice["type"] != "tool" {
		t.Errorf("tool_choice.type = %v, want tool", toolChoice["type"])
	}
	if toolChoice["name"] != testToolAnalyze {
		t.Errorf("tool_choice.name = %v, want analyze_issues", toolChoice["name"])
	}
}
