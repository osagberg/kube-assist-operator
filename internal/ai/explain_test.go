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
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildExplainPrompt_Basic(t *testing.T) {
	healthResults := map[string]any{
		"workloads": map[string]any{
			"healthy": 5,
			"issues":  2,
			"error":   "",
		},
		"summary": map[string]any{
			"totalHealthy": 5,
			"totalIssues":  2,
		},
	}

	prompt := BuildExplainPrompt(healthResults, nil, nil)

	if !strings.Contains(prompt, "Kubernetes cluster health advisor") {
		t.Error("prompt should contain system instruction")
	}
	if !strings.Contains(prompt, "Current Health Results") {
		t.Error("prompt should contain health results section")
	}
	if !strings.Contains(prompt, "narrative") {
		t.Error("prompt should request narrative field")
	}
	if !strings.Contains(prompt, "riskLevel") {
		t.Error("prompt should request riskLevel field")
	}
	if strings.Contains(prompt, "Causal Analysis") {
		t.Error("prompt should not contain causal section when nil")
	}
	if strings.Contains(prompt, "Recent Health Score History") {
		t.Error("prompt should not contain history section when nil")
	}
}

func TestBuildExplainPrompt_WithCausal(t *testing.T) {
	healthResults := map[string]any{
		"summary": map[string]any{"totalIssues": 3},
	}
	causalCtx := &CausalAnalysisContext{
		Groups: []CausalGroupSummary{
			{
				Rule:       "oom-correlation",
				Title:      "OOM in namespace default",
				Severity:   "critical",
				Confidence: 0.9,
				Resources:  []string{"default/my-pod"},
			},
		},
		UncorrelatedCount: 1,
		TotalIssues:       3,
	}

	prompt := BuildExplainPrompt(healthResults, causalCtx, nil)

	if !strings.Contains(prompt, "Causal Analysis") {
		t.Error("prompt should contain causal section")
	}
	if !strings.Contains(prompt, "oom-correlation") {
		t.Error("prompt should contain causal group data")
	}
}

func TestBuildExplainPrompt_WithHistory(t *testing.T) {
	healthResults := map[string]any{
		"summary": map[string]any{"totalIssues": 0},
	}
	history := []any{
		map[string]any{
			"timestamp":   "2026-02-07T12:00:00Z",
			"healthScore": 95.0,
			"totalIssues": 1,
		},
		map[string]any{
			"timestamp":   "2026-02-07T12:05:00Z",
			"healthScore": 100.0,
			"totalIssues": 0,
		},
	}

	prompt := BuildExplainPrompt(healthResults, nil, history)

	if !strings.Contains(prompt, "Recent Health Score History") {
		t.Error("prompt should contain history section")
	}
	if !strings.Contains(prompt, "2026-02-07T12:00:00Z") {
		t.Error("prompt should contain history timestamps")
	}
}

func TestParseExplainResponse_ValidJSON(t *testing.T) {
	content := `{
		"narrative": "The cluster is healthy with no critical issues.",
		"riskLevel": "low",
		"topIssues": [
			{"title": "Minor pod restart", "severity": "warning", "impact": "Minimal"}
		],
		"trendDirection": "improving",
		"confidence": 0.85
	}`

	resp := ParseExplainResponse(content, 500)

	if resp.Narrative != "The cluster is healthy with no critical issues." {
		t.Errorf("Narrative = %q, want expected value", resp.Narrative)
	}
	if resp.RiskLevel != "low" {
		t.Errorf("RiskLevel = %q, want low", resp.RiskLevel)
	}
	if len(resp.TopIssues) != 1 {
		t.Errorf("TopIssues length = %d, want 1", len(resp.TopIssues))
	}
	if resp.TrendDirection != "improving" {
		t.Errorf("TrendDirection = %q, want improving", resp.TrendDirection)
	}
	if resp.Confidence != 0.85 {
		t.Errorf("Confidence = %f, want 0.85", resp.Confidence)
	}
	if resp.TokensUsed != 500 {
		t.Errorf("TokensUsed = %d, want 500", resp.TokensUsed)
	}
}

func TestParseExplainResponse_WrappedInCodeBlock(t *testing.T) {
	content := "```json\n" + `{
		"narrative": "Cluster is degrading.",
		"riskLevel": "high",
		"topIssues": [],
		"trendDirection": "degrading",
		"confidence": 0.7
	}` + "\n```"

	resp := ParseExplainResponse(content, 300)

	if resp.RiskLevel != "high" {
		t.Errorf("RiskLevel = %q, want high", resp.RiskLevel)
	}
	if resp.TrendDirection != "degrading" {
		t.Errorf("TrendDirection = %q, want degrading", resp.TrendDirection)
	}
}

func TestParseExplainResponse_InvalidJSON(t *testing.T) {
	content := "This is not JSON, just a narrative about the cluster."

	resp := ParseExplainResponse(content, 100)

	if resp.Narrative != content {
		t.Errorf("Narrative = %q, want raw content", resp.Narrative)
	}
	if resp.RiskLevel != explainUnknown {
		t.Errorf("RiskLevel = %q, want unknown", resp.RiskLevel)
	}
	if resp.TrendDirection != explainUnknown {
		t.Errorf("TrendDirection = %q, want unknown", resp.TrendDirection)
	}
	if resp.TokensUsed != 100 {
		t.Errorf("TokensUsed = %d, want 100", resp.TokensUsed)
	}
}

func TestParseExplainResponse_ConfidenceClamping(t *testing.T) {
	tests := []struct {
		name       string
		confidence float64
		want       float64
	}{
		{"negative", -0.5, 0},
		{"over one", 1.5, 1},
		{"normal", 0.8, 0.8},
		{"zero", 0, 0},
		{"one", 1.0, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, _ := json.Marshal(map[string]any{
				"narrative":      "test",
				"riskLevel":      "low",
				"trendDirection": "stable",
				"confidence":     tt.confidence,
			})
			resp := ParseExplainResponse(string(data), 0)
			if resp.Confidence != tt.want {
				t.Errorf("Confidence = %f, want %f", resp.Confidence, tt.want)
			}
		})
	}
}

func TestExplainResponse_JSONRoundTrip(t *testing.T) {
	resp := ExplainResponse{
		Narrative: "Cluster is healthy.",
		RiskLevel: "low",
		TopIssues: []ExplainIssue{
			{Title: "Pod restart", Severity: "warning", Impact: "Minimal"},
		},
		TrendDirection: "stable",
		Confidence:     0.9,
		TokensUsed:     200,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}

	var decoded ExplainResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}

	if decoded.Narrative != resp.Narrative {
		t.Errorf("Narrative = %q, want %q", decoded.Narrative, resp.Narrative)
	}
	if decoded.RiskLevel != resp.RiskLevel {
		t.Errorf("RiskLevel = %q, want %q", decoded.RiskLevel, resp.RiskLevel)
	}
	if len(decoded.TopIssues) != 1 {
		t.Errorf("TopIssues length = %d, want 1", len(decoded.TopIssues))
	}
	if decoded.TrendDirection != resp.TrendDirection {
		t.Errorf("TrendDirection = %q, want %q", decoded.TrendDirection, resp.TrendDirection)
	}
}

func TestBuildPrompt_ExplainMode(t *testing.T) {
	explainPrompt := "This is an explain prompt."
	request := AnalysisRequest{
		ExplainMode:    true,
		ExplainContext: explainPrompt,
	}

	result := BuildPrompt(request)
	if result != explainPrompt {
		t.Errorf("BuildPrompt with ExplainMode = %q, want %q", result, explainPrompt)
	}
}

func TestParseExplainResponse_NormalizesRiskLevel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"uppercase HIGH", "HIGH", "high"},
		{"mixed Low", "Low", "low"},
		{"invalid foo", "foo", "unknown"},
		{"empty string", "", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, _ := json.Marshal(map[string]any{
				"narrative":      "test narrative",
				"riskLevel":      tt.input,
				"trendDirection": "stable",
				"confidence":     0.5,
			})
			resp := ParseExplainResponse(string(data), 0)
			if resp.RiskLevel != tt.want {
				t.Errorf("RiskLevel = %q, want %q", resp.RiskLevel, tt.want)
			}
		})
	}
}

func TestParseExplainResponse_NormalizesTrendDirection(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"uppercase STABLE", "STABLE", "stable"},
		{"mixed Improving", "Improving", "improving"},
		{"invalid bar", "bar", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, _ := json.Marshal(map[string]any{
				"narrative":      "test narrative",
				"riskLevel":      "low",
				"trendDirection": tt.input,
				"confidence":     0.5,
			})
			resp := ParseExplainResponse(string(data), 0)
			if resp.TrendDirection != tt.want {
				t.Errorf("TrendDirection = %q, want %q", resp.TrendDirection, tt.want)
			}
		})
	}
}

func TestParseExplainResponse_EmptyNarrative(t *testing.T) {
	data, _ := json.Marshal(map[string]any{
		"narrative":      "",
		"riskLevel":      "low",
		"trendDirection": "stable",
		"confidence":     0.5,
	})
	raw := string(data)
	resp := ParseExplainResponse(raw, 0)
	if resp.Narrative != raw {
		t.Errorf("Narrative = %q, want raw content %q", resp.Narrative, raw)
	}
}

func TestBuildExplainPrompt_Truncation(t *testing.T) {
	// Create health results with a large field that exceeds 8192 bytes when serialized
	largeValue := strings.Repeat("x", 10000)
	healthResults := map[string]any{
		"bigField": largeValue,
		"summary": map[string]any{
			"totalHealthy": 5,
			"totalIssues":  2,
		},
	}

	prompt := BuildExplainPrompt(healthResults, nil, nil)

	// The full healthJSON would be >10000 bytes, but it should be truncated to the summary
	if strings.Contains(prompt, largeValue) {
		t.Error("prompt should not contain the large value after truncation")
	}
	if !strings.Contains(prompt, "totalHealthy") {
		t.Error("prompt should contain summary data after truncation")
	}
	// Verify the prompt is reasonably sized
	if len(prompt) > 8192+2000 {
		t.Errorf("prompt length = %d, expected to be bounded after truncation", len(prompt))
	}
}

func TestParseExplainResponse_NormalizesAllValidValues(t *testing.T) {
	riskLevels := []string{"low", "medium", "high", "critical"}
	for _, rl := range riskLevels {
		data, _ := json.Marshal(map[string]any{
			"narrative":      "test",
			"riskLevel":      rl,
			"trendDirection": "stable",
			"confidence":     0.5,
		})
		resp := ParseExplainResponse(string(data), 0)
		if resp.RiskLevel != rl {
			t.Errorf("RiskLevel = %q, want %q", resp.RiskLevel, rl)
		}
	}

	trendDirs := []string{"improving", "stable", "degrading"}
	for _, td := range trendDirs {
		data, _ := json.Marshal(map[string]any{
			"narrative":      "test",
			"riskLevel":      "low",
			"trendDirection": td,
			"confidence":     0.5,
		})
		resp := ParseExplainResponse(string(data), 0)
		if resp.TrendDirection != td {
			t.Errorf("TrendDirection = %q, want %q", resp.TrendDirection, td)
		}
	}
}

func TestBuildPrompt_ExplainModeEmpty(t *testing.T) {
	// ExplainMode true but empty context should fall through to normal prompt
	request := AnalysisRequest{
		ExplainMode:    true,
		ExplainContext: "",
		Issues: []IssueContext{
			{Type: "test", Message: "test issue"},
		},
	}

	result := BuildPrompt(request)
	if !strings.Contains(result, "Analyze these Kubernetes") {
		t.Error("BuildPrompt with empty ExplainContext should fall through to normal prompt")
	}
}
