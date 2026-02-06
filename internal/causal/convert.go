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

package causal

import (
	"github.com/osagberg/kube-assist-operator/internal/ai"
)

// ToAIContext converts a CausalContext to an ai.CausalAnalysisContext
// suitable for inclusion in AI analysis requests.
func ToAIContext(cc *CausalContext) *ai.CausalAnalysisContext {
	if cc == nil || len(cc.Groups) == 0 {
		return nil
	}

	groups := make([]ai.CausalGroupSummary, 0, len(cc.Groups))
	for _, g := range cc.Groups {
		var resources []string
		for _, e := range g.Events {
			resources = append(resources, e.Issue.Namespace+"/"+e.Issue.Resource)
		}
		groups = append(groups, ai.CausalGroupSummary{
			Rule:       g.Rule,
			Title:      g.Title,
			RootCause:  g.RootCause,
			Severity:   g.Severity,
			Confidence: g.Confidence,
			Resources:  resources,
		})
	}

	return &ai.CausalAnalysisContext{
		Groups:            groups,
		UncorrelatedCount: cc.UncorrelatedCount,
		TotalIssues:       cc.TotalIssues,
	}
}
