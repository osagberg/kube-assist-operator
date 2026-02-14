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

// AnalysisResponseSchema returns the JSON Schema for the analysis response.
// Compatible with OpenAI strict mode (all properties in required, additionalProperties: false at every level).
func AnalysisResponseSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"suggestions": map[string]any{
				"type": "object",
				"additionalProperties": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"suggestion": map[string]any{"type": "string"},
						"rootCause":  map[string]any{"type": "string"},
						"steps":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"references": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"confidence": map[string]any{"type": "number"},
					},
					"required":             []string{"suggestion", "rootCause", "steps", "references", "confidence"},
					"additionalProperties": false,
				},
			},
			"causalInsights": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"groupID":      map[string]any{"type": "string"},
						"aiRootCause":  map[string]any{"type": "string"},
						"aiSuggestion": map[string]any{"type": "string"},
						"aiSteps":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"confidence":   map[string]any{"type": "number"},
					},
					"required":             []string{"groupID", "aiRootCause", "aiSuggestion", "aiSteps", "confidence"},
					"additionalProperties": false,
				},
			},
			"summary": map[string]any{"type": "string"},
		},
		"required":             []string{"suggestions", "causalInsights", "summary"},
		"additionalProperties": false,
	}
}

// ExplainResponseSchema returns the JSON Schema for the explain response.
// Compatible with OpenAI strict mode.
func ExplainResponseSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"narrative": map[string]any{"type": "string"},
			"riskLevel": map[string]any{
				"type": "string",
				"enum": []string{"low", "medium", "high", "critical"},
			},
			"topIssues": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title":    map[string]any{"type": "string"},
						"severity": map[string]any{"type": "string"},
						"impact":   map[string]any{"type": "string"},
					},
					"required":             []string{"title", "severity", "impact"},
					"additionalProperties": false,
				},
			},
			"trendDirection": map[string]any{
				"type": "string",
				"enum": []string{"improving", "stable", "degrading"},
			},
			"confidence": map[string]any{"type": "number"},
		},
		"required":             []string{"narrative", "riskLevel", "topIssues", "trendDirection", "confidence"},
		"additionalProperties": false,
	}
}
