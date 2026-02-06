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
	"fmt"
)

// NoOpProvider is a no-operation provider that returns static suggestions
type NoOpProvider struct{}

// NewNoOpProvider creates a new NoOp provider
func NewNoOpProvider() *NoOpProvider {
	return &NoOpProvider{}
}

// Name returns the provider identifier
func (p *NoOpProvider) Name() string {
	return ProviderNameNoop
}

// Available always returns true for NoOp
func (p *NoOpProvider) Available() bool {
	return true
}

// Analyze returns the static suggestions without AI enhancement
func (p *NoOpProvider) Analyze(_ context.Context, request AnalysisRequest) (*AnalysisResponse, error) {
	response := &AnalysisResponse{
		EnhancedSuggestions: make(map[string]EnhancedSuggestion),
	}

	for _, issue := range request.Issues {
		key := issue.Namespace + "/" + issue.Resource
		rootCause := "This is a test suggestion from the NoOp AI provider."
		if request.CausalContext != nil && len(request.CausalContext.Groups) > 0 {
			rootCause = fmt.Sprintf("[NoOp AI] Causal context: %d groups, %d total issues",
				len(request.CausalContext.Groups), request.CausalContext.TotalIssues)
		}
		response.EnhancedSuggestions[key] = EnhancedSuggestion{
			Suggestion: "[NoOp AI] " + issue.StaticSuggestion,
			RootCause:  rootCause,
			Confidence: 1.0, // Full confidence for testing
		}
	}

	return response, nil
}
