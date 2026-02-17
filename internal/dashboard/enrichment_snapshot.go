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

package dashboard

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"

	"github.com/osagberg/kube-assist-operator/internal/ai"
	"github.com/osagberg/kube-assist-operator/internal/checker"
)

// deepCopyResults returns a new map with deep-copied CheckResult values.
// Each Issue.Metadata map is cloned so the worker cannot alias caller data.
func deepCopyResults(src map[string]*checker.CheckResult) map[string]*checker.CheckResult {
	if src == nil {
		return nil
	}
	dst := make(map[string]*checker.CheckResult, len(src))
	for k, v := range src {
		if v == nil {
			dst[k] = nil
			continue
		}
		cr := &checker.CheckResult{
			CheckerName: v.CheckerName,
			Healthy:     v.Healthy,
			Error:       v.Error,
		}
		if len(v.Issues) > 0 {
			cr.Issues = make([]checker.Issue, len(v.Issues))
			for i, issue := range v.Issues {
				cr.Issues[i] = checker.Issue{
					Type:       issue.Type,
					Severity:   issue.Severity,
					Resource:   issue.Resource,
					Namespace:  issue.Namespace,
					Message:    issue.Message,
					Suggestion: issue.Suggestion,
				}
				if issue.Metadata != nil {
					cr.Issues[i].Metadata = make(map[string]string, len(issue.Metadata))
					maps.Copy(cr.Issues[i].Metadata, issue.Metadata)
				}
			}
		}
		dst[k] = cr
	}
	return dst
}

// NOTE: deepCopyCausalContext is defined in handlers_causal.go (used by both
// the causal groups handler and the enrichment pipeline).

// minimalCheckContext builds a CheckContext with only the fields needed for AI enrichment.
// DataSource and Clientset are nil (not needed by EnhanceAllWithAI).
func minimalCheckContext(src *checker.CheckContext, provider ai.Provider) *checker.CheckContext {
	if src == nil {
		return nil
	}
	return &checker.CheckContext{
		AIEnabled:      src.AIEnabled,
		AIProvider:     provider,
		ClusterContext: src.ClusterContext,
		CausalContext:  src.CausalContext,
		MaxIssues:      src.MaxIssues,
		Namespaces:     src.Namespaces,
	}
}

// cloneHealthUpdate returns a deep clone of a HealthUpdate suitable for broadcast.
// Clones Results map (each CheckResult with cloned Issues slice), IssueStates map,
// and Namespaces slice. AIStatus is cloned by value (pointer to new copy).
func cloneHealthUpdate(src *HealthUpdate) HealthUpdate {
	if src == nil {
		return HealthUpdate{}
	}
	dst := HealthUpdate{
		Timestamp: src.Timestamp,
		ClusterID: src.ClusterID,
		Summary:   src.Summary,
	}
	// Clone Namespaces
	if len(src.Namespaces) > 0 {
		dst.Namespaces = make([]string, len(src.Namespaces))
		copy(dst.Namespaces, src.Namespaces)
	}
	// Clone AIStatus by value
	if src.AIStatus != nil {
		aiCopy := *src.AIStatus
		dst.AIStatus = &aiCopy
	}
	// Clone Results map with deep-copied Issues slices
	if src.Results != nil {
		dst.Results = make(map[string]CheckResult, len(src.Results))
		for k, cr := range src.Results {
			clonedCR := CheckResult{
				Name:    cr.Name,
				Healthy: cr.Healthy,
				Error:   cr.Error,
			}
			if len(cr.Issues) > 0 {
				clonedCR.Issues = make([]Issue, len(cr.Issues))
				for i, issue := range cr.Issues {
					clonedCR.Issues[i] = Issue{
						Type:       issue.Type,
						Severity:   issue.Severity,
						Resource:   issue.Resource,
						Namespace:  issue.Namespace,
						Message:    issue.Message,
						Suggestion: issue.Suggestion,
						AIEnhanced: issue.AIEnhanced,
						RootCause:  issue.RootCause,
					}
					if issue.Metadata != nil {
						clonedCR.Issues[i].Metadata = make(map[string]string, len(issue.Metadata))
						maps.Copy(clonedCR.Issues[i].Metadata, issue.Metadata)
					}
				}
			}
			dst.Results[k] = clonedCR
		}
	}
	// Clone IssueStates map
	if src.IssueStates != nil {
		dst.IssueStates = make(map[string]*IssueState, len(src.IssueStates))
		for k, st := range src.IssueStates {
			if st == nil {
				dst.IssueStates[k] = nil
				continue
			}
			stCopy := *st
			dst.IssueStates[k] = &stCopy
		}
	}
	return dst
}

// configVersionHash returns a short hash of provider+model for config staleness checks.
func configVersionHash(providerName, model string) string {
	h := sha256.Sum256([]byte(providerName + "\x00" + model))
	return hex.EncodeToString(h[:8])
}
