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
	"sort"
	"time"

	"github.com/osagberg/kube-assist-operator/internal/checker"
)

// Correlator runs all correlation strategies and merges the results into a
// deduplicated set of CausalGroups, ordered by severity and confidence.
type Correlator struct {
	temporal *TemporalCorrelator
	graph    *ResourceGraphCorrelator
	rules    []CrossCheckerRule
}

// NewCorrelator creates a Correlator with default settings.
func NewCorrelator() *Correlator {
	return &Correlator{
		temporal: NewTemporalCorrelator(DefaultTimeWindow),
		graph:    NewResourceGraphCorrelator(),
		rules:    DefaultRules(),
	}
}

// NewCorrelatorWithWindow creates a Correlator with a custom time window.
func NewCorrelatorWithWindow(window time.Duration) *Correlator {
	return &Correlator{
		temporal: NewTemporalCorrelator(window),
		graph:    NewResourceGraphCorrelator(),
		rules:    DefaultRules(),
	}
}

// Analyze runs all correlation strategies on the input and returns a CausalContext.
func (c *Correlator) Analyze(input CorrelationInput) *CausalContext {
	// Count total issues for the uncorrelated metric.
	totalIssues := 0
	for _, result := range input.Results {
		if result != nil && result.Error == nil {
			totalIssues += len(result.Issues)
		}
	}

	if totalIssues == 0 {
		return &CausalContext{TotalIssues: 0}
	}

	// Run each strategy.
	var allGroups []CausalGroup
	allGroups = append(allGroups, applyCrossCheckerRules(c.rules, input)...)
	allGroups = append(allGroups, c.graph.Correlate(input)...)
	allGroups = append(allGroups, c.temporal.Correlate(input)...)

	// Deduplicate: an issue (identified by checker+resource+namespace) should only
	// appear in the highest-confidence group that contains it.
	groups := deduplicateGroups(allGroups)

	// Count correlated issue keys.
	correlated := make(map[string]bool)
	for _, g := range groups {
		for _, e := range g.Events {
			key := e.Checker + ":" + e.Issue.Namespace + "/" + e.Issue.Resource + ":" + e.Issue.Type
			correlated[key] = true
		}
	}

	// Sort groups: highest severity first, then highest confidence.
	sortGroups(groups)

	return &CausalContext{
		Groups:            groups,
		UncorrelatedCount: totalIssues - len(correlated),
		TotalIssues:       totalIssues,
	}
}

// deduplicateGroups removes issues from lower-confidence groups when the same
// issue already appears in a higher-confidence group. Groups with no remaining
// events are dropped entirely.
func deduplicateGroups(groups []CausalGroup) []CausalGroup {
	// Sort by confidence descending so higher-confidence groups claim issues first.
	sort.SliceStable(groups, func(i, j int) bool {
		return groups[i].Confidence > groups[j].Confidence
	})

	claimed := make(map[string]bool)
	result := make([]CausalGroup, 0, len(groups))
	for _, g := range groups {
		var kept []TimelineEvent
		for _, e := range g.Events {
			key := e.Checker + ":" + e.Issue.Namespace + "/" + e.Issue.Resource + ":" + e.Issue.Type
			if !claimed[key] {
				claimed[key] = true
				kept = append(kept, e)
			}
		}
		if len(kept) < 2 {
			// A group with fewer than 2 events isn't meaningful; release claims.
			for _, e := range kept {
				key := e.Checker + ":" + e.Issue.Namespace + "/" + e.Issue.Resource + ":" + e.Issue.Type
				delete(claimed, key)
			}
			continue
		}
		g.Events = kept
		result = append(result, g)
	}
	return result
}

func sortGroups(groups []CausalGroup) {
	sevPrio := map[string]int{
		checker.SeverityCritical: 3,
		checker.SeverityWarning:  2,
		checker.SeverityInfo:     1,
	}
	sort.SliceStable(groups, func(i, j int) bool {
		pi := sevPrio[groups[i].Severity]
		pj := sevPrio[groups[j].Severity]
		if pi != pj {
			return pi > pj
		}
		return groups[i].Confidence > groups[j].Confidence
	})
}
