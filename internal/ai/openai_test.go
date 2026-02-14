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
	testSchemaAnalysis = "analysis_response"
	testSchemaExplain  = "explain_response"
)

func TestOpenAI_SchemaMode_Success(t *testing.T) {
	// Server returns valid schema-compliant JSON
	respPayload := `{
		"suggestions": {
			"issue_0": {
				"suggestion": "Fix the deployment",
				"rootCause": "Image not found",
				"steps": ["Pull the correct image"],
				"references": ["https://kubernetes.io/docs/"],
				"confidence": 0.9
			}
		},
		"causalInsights": [],
		"summary": "One issue found"
	}`

	var receivedReq openAIRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedReq)

		resp := openAIResponse{
			ID: "test-id",
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Message: struct {
						Content string `json:"content"`
					}{Content: respPayload},
					FinishReason: "stop",
				},
			},
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 500},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(Config{
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

	// Verify the request included response_format
	if receivedReq.ResponseFormat == nil {
		t.Fatal("request should include response_format")
	}
	if receivedReq.ResponseFormat.Type != "json_schema" {
		t.Errorf("response_format.type = %q, want json_schema", receivedReq.ResponseFormat.Type)
	}
	if receivedReq.ResponseFormat.JSONSchema == nil {
		t.Fatal("response_format.json_schema should not be nil")
	}
	if receivedReq.ResponseFormat.JSONSchema.Name != testSchemaAnalysis {
		t.Errorf("json_schema.name = %q, want analysis_response", receivedReq.ResponseFormat.JSONSchema.Name)
	}
	if !receivedReq.ResponseFormat.JSONSchema.Strict {
		t.Error("json_schema.strict should be true")
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

func TestOpenAI_SchemaMode_Fallback(t *testing.T) {
	// First call with schema returns 400, second call without schema succeeds
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)

		var req openAIRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		if count == 1 {
			// Schema mode — return 400 to simulate unsupported
			if req.ResponseFormat == nil {
				t.Error("first request should have response_format")
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"json_schema not supported"}}`))
			return
		}

		// Fallback mode — no response_format
		if req.ResponseFormat != nil {
			t.Error("fallback request should not have response_format")
		}

		respPayload := `{"suggestions":{"issue_0":{"suggestion":"Fix it","rootCause":"Bad config","steps":[],"references":[],"confidence":0.8}},"causalInsights":[],"summary":"Fixed"}`

		resp := openAIResponse{
			ID: "test-id",
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Message: struct {
						Content string `json:"content"`
					}{Content: respPayload},
					FinishReason: "stop",
				},
			},
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 300},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(Config{
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
		t.Errorf("expected 2 API calls (schema + fallback), got %d", callCount.Load())
	}

	if result.ParseFailed {
		t.Error("ParseFailed should be false after fallback")
	}
	if len(result.EnhancedSuggestions) != 1 {
		t.Errorf("EnhancedSuggestions length = %d, want 1", len(result.EnhancedSuggestions))
	}
}

func TestOpenAI_SchemaMode_ExplainMode(t *testing.T) {
	var receivedReq openAIRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedReq)

		respPayload := `{"narrative":"Cluster is healthy","riskLevel":"low","topIssues":[],"trendDirection":"stable","confidence":0.9}`

		resp := openAIResponse{
			ID: "test-id",
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Message: struct {
						Content string `json:"content"`
					}{Content: respPayload},
					FinishReason: "stop",
				},
			},
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 200},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(Config{
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

	if receivedReq.ResponseFormat == nil || receivedReq.ResponseFormat.JSONSchema == nil {
		t.Fatal("request should include json_schema response_format")
	}
	if receivedReq.ResponseFormat.JSONSchema.Name != testSchemaExplain {
		t.Errorf("json_schema.name = %q, want explain_response", receivedReq.ResponseFormat.JSONSchema.Name)
	}
}

func TestOpenAI_DoRequest_StatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    string
	}{
		{
			name:       "400 Bad Request",
			statusCode: http.StatusBadRequest,
			body:       `{"error":{"message":"bad request"}}`,
			wantErr:    "API error (status 400)",
		},
		{
			name:       "429 Rate Limited",
			statusCode: http.StatusTooManyRequests,
			body:       `{"error":{"message":"rate limited"}}`,
			wantErr:    "rate limited (status 429)",
		},
		{
			name:       "500 Internal Error",
			statusCode: http.StatusInternalServerError,
			body:       `{"error":{"message":"internal error"}}`,
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

			provider := NewOpenAIProvider(Config{
				APIKey:   "test-key",
				Endpoint: server.URL,
			})

			_, err := provider.doOpenAIRequest(context.Background(), openAIRequest{
				Model:       "test",
				Messages:    []openAIMessage{{Role: "user", Content: "test"}},
				Temperature: 0.3,
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

func TestOpenAI_DoRequest_Truncated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openAIResponse{
			ID: "test-id",
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Message: struct {
						Content string `json:"content"`
					}{Content: `{"suggestions":{},"causalInsights":[],"summary":"truncated"}`},
					FinishReason: "length",
				},
			},
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 16384},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(Config{
		APIKey:   "test-key",
		Endpoint: server.URL,
	})

	result, err := provider.doOpenAIRequest(context.Background(), openAIRequest{
		Model:       "test",
		Messages:    []openAIMessage{{Role: "user", Content: "test"}},
		Temperature: 0.3,
	})

	if err != nil {
		t.Fatalf("doOpenAIRequest() error = %v", err)
	}
	if !result.Truncated {
		t.Error("Truncated should be true for finish_reason=length")
	}
}

func TestOpenAI_SchemaMode_RequestSerialization(t *testing.T) {
	// Verify the full request body is valid JSON with the expected structure
	req := openAIRequest{
		Model: "gpt-4o-mini",
		Messages: []openAIMessage{
			{Role: "system", Content: "test system prompt"},
			{Role: "user", Content: "test user prompt"},
		},
		MaxTokens:   1000,
		Temperature: 0.3,
		ResponseFormat: &responseFormat{
			Type: "json_schema",
			JSONSchema: &jsonSchema{
				Name:   testSchemaAnalysis,
				Strict: true,
				Schema: AnalysisResponseSchema(),
			},
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

	rf, ok := parsed["response_format"].(map[string]any)
	if !ok {
		t.Fatal("response_format missing or wrong type")
	}
	if rf["type"] != "json_schema" {
		t.Errorf("response_format.type = %v, want json_schema", rf["type"])
	}

	js, ok := rf["json_schema"].(map[string]any)
	if !ok {
		t.Fatal("json_schema missing or wrong type")
	}
	if js["name"] != testSchemaAnalysis {
		t.Errorf("json_schema.name = %v, want analysis_response", js["name"])
	}
	if js["strict"] != true {
		t.Errorf("json_schema.strict = %v, want true", js["strict"])
	}
}
