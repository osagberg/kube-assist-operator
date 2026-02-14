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
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestAnalysisResponseSchema_ValidJSON(t *testing.T) {
	schema := AnalysisResponseSchema()
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("failed to marshal AnalysisResponseSchema: %v", err)
	}

	var roundTrip map[string]any
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("failed to unmarshal AnalysisResponseSchema: %v", err)
	}

	// Verify top-level structure
	if roundTrip["type"] != "object" {
		t.Errorf("type = %v, want object", roundTrip["type"])
	}
	props, ok := roundTrip["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties is not a map")
	}
	for _, key := range []string{"suggestions", "causalInsights", "summary"} {
		if _, ok := props[key]; !ok {
			t.Errorf("missing property %q", key)
		}
	}

	// Verify additionalProperties: false at top level
	if roundTrip["additionalProperties"] != false {
		t.Error("top-level additionalProperties should be false")
	}
}

func TestExplainResponseSchema_ValidJSON(t *testing.T) {
	schema := ExplainResponseSchema()
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("failed to marshal ExplainResponseSchema: %v", err)
	}

	var roundTrip map[string]any
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("failed to unmarshal ExplainResponseSchema: %v", err)
	}

	props, ok := roundTrip["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties is not a map")
	}
	for _, key := range []string{"narrative", "riskLevel", "topIssues", "trendDirection", "confidence"} {
		if _, ok := props[key]; !ok {
			t.Errorf("missing property %q", key)
		}
	}

	if roundTrip["additionalProperties"] != false {
		t.Error("top-level additionalProperties should be false")
	}
}

func TestAnalysisResponseSchema_MatchesParseResponse(t *testing.T) {
	// A sample response that matches the schema should parse without failure.
	sample := `{
		"suggestions": {
			"issue_0": {
				"suggestion": "Fix the deployment",
				"rootCause": "Image not found",
				"steps": ["Pull the correct image", "Update deployment"],
				"references": ["https://kubernetes.io/docs/"],
				"confidence": 0.9
			}
		},
		"causalInsights": [],
		"summary": "One issue found"
	}`

	resp := ParseResponse(sample, 500, "test")
	if resp.ParseFailed {
		t.Error("ParseFailed should be false for schema-compliant JSON")
	}
	if len(resp.EnhancedSuggestions) != 1 {
		t.Errorf("EnhancedSuggestions length = %d, want 1", len(resp.EnhancedSuggestions))
	}
	s := resp.EnhancedSuggestions["issue_0"]
	if s.Suggestion != "Fix the deployment" {
		t.Errorf("Suggestion = %q, want %q", s.Suggestion, "Fix the deployment")
	}
}

func TestAnalysisResponseSchema_WithCausalInsights(t *testing.T) {
	sample := `{
		"suggestions": {
			"issue_0": {
				"suggestion": "Fix OOM",
				"rootCause": "Memory limit too low",
				"steps": ["Increase memory limit"],
				"references": [],
				"confidence": 0.85
			}
		},
		"causalInsights": [
			{
				"groupID": "group_0",
				"aiRootCause": "OOM due to memory leak",
				"aiSuggestion": "Increase limits and fix leak",
				"aiSteps": ["Profile memory", "Fix leak"],
				"confidence": 0.9
			}
		],
		"summary": "OOM issues detected"
	}`

	resp := ParseResponse(sample, 600, "test")
	if resp.ParseFailed {
		t.Error("ParseFailed should be false")
	}
	if len(resp.CausalInsights) != 1 {
		t.Fatalf("CausalInsights length = %d, want 1", len(resp.CausalInsights))
	}
	if resp.CausalInsights[0].GroupID != "group_0" {
		t.Errorf("GroupID = %q, want group_0", resp.CausalInsights[0].GroupID)
	}
}

func TestExplainResponseSchema_MatchesParseExplainResponse(t *testing.T) {
	sample := `{
		"narrative": "The cluster is healthy.",
		"riskLevel": "low",
		"topIssues": [
			{"title": "Pod restart", "severity": "warning", "impact": "Minimal"}
		],
		"trendDirection": "stable",
		"confidence": 0.9
	}`

	resp := ParseExplainResponse(sample, 300)
	if resp.Narrative != "The cluster is healthy." {
		t.Errorf("Narrative = %q, want expected", resp.Narrative)
	}
	if resp.RiskLevel != testRiskLow {
		t.Errorf("RiskLevel = %q, want low", resp.RiskLevel)
	}
	if resp.TrendDirection != "stable" {
		t.Errorf("TrendDirection = %q, want stable", resp.TrendDirection)
	}
	if len(resp.TopIssues) != 1 {
		t.Errorf("TopIssues length = %d, want 1", len(resp.TopIssues))
	}
}

func TestAnalysisResponseSchema_StrictModeRequirements(t *testing.T) {
	schema := AnalysisResponseSchema()

	// Verify required includes causalInsights (needed for OpenAI strict mode)
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("required is not []string")
	}
	if !slices.Contains(required, "causalInsights") {
		t.Error("causalInsights must be in required for OpenAI strict mode")
	}
}

// jsonTags extracts the set of JSON field names from a struct type.
func jsonTags(t reflect.Type) map[string]bool {
	tags := make(map[string]bool)
	for i := range t.NumField() {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name != "" {
			tags[name] = true
		}
	}
	return tags
}

// schemaProperties extracts property names from a JSON Schema map.
func schemaProperties(schema map[string]any) map[string]bool {
	props := make(map[string]bool)
	if p, ok := schema["properties"].(map[string]any); ok {
		for name := range p {
			props[name] = true
		}
	}
	return props
}

func TestAnalysisResponseSchema_MatchesStruct(t *testing.T) {
	schema := AnalysisResponseSchema()

	// Top-level schema should have "suggestions", "causalInsights", "summary"
	topProps := schemaProperties(schema)
	for _, required := range []string{"suggestions", "causalInsights", "summary"} {
		if !topProps[required] {
			t.Errorf("AnalysisResponseSchema missing top-level property %q", required)
		}
	}

	// Suggestion properties should match EnhancedSuggestion struct
	suggestionsSchema := schema["properties"].(map[string]any)["suggestions"].(map[string]any)
	suggestionItemSchema := suggestionsSchema["additionalProperties"].(map[string]any)
	schemaFields := schemaProperties(suggestionItemSchema)
	structFields := jsonTags(reflect.TypeFor[EnhancedSuggestion]())

	for field := range structFields {
		if !schemaFields[field] {
			t.Errorf("EnhancedSuggestion JSON tag %q missing from AnalysisResponseSchema", field)
		}
	}
	for field := range schemaFields {
		if !structFields[field] {
			t.Errorf("AnalysisResponseSchema has property %q not in EnhancedSuggestion struct", field)
		}
	}
}

func TestExplainResponseSchema_MatchesStruct(t *testing.T) {
	schema := ExplainResponseSchema()

	// Every schema property must exist in the struct
	schemaFields := schemaProperties(schema)
	structFields := jsonTags(reflect.TypeFor[ExplainResponse]())

	for field := range schemaFields {
		if !structFields[field] {
			t.Errorf("ExplainResponseSchema has property %q not in ExplainResponse struct", field)
		}
	}
	// Note: the struct may have extra internal fields (e.g. tokensUsed) that
	// are intentionally absent from the AI response schema.
	for field := range structFields {
		if !schemaFields[field] {
			t.Logf("ExplainResponse JSON tag %q not in schema (expected for internal fields)", field)
		}
	}
}

func TestAnalysisResponseSchema_CausalInsightFields(t *testing.T) {
	schema := AnalysisResponseSchema()
	causalSchema := schema["properties"].(map[string]any)["causalInsights"].(map[string]any)
	itemSchema := causalSchema["items"].(map[string]any)
	schemaFields := schemaProperties(itemSchema)
	structFields := jsonTags(reflect.TypeFor[CausalGroupInsight]())

	for field := range structFields {
		if !schemaFields[field] {
			t.Errorf("CausalGroupInsight JSON tag %q missing from schema", field)
		}
	}
	for field := range schemaFields {
		if !structFields[field] {
			t.Errorf("Schema causalInsight has property %q not in CausalGroupInsight struct", field)
		}
	}
}
