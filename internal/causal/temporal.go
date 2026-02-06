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
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/osagberg/kube-assist-operator/internal/checker"
)

const (
	// DefaultTimeWindow is the default window for temporal correlation.
	DefaultTimeWindow = 2 * time.Minute

	// clusterScope is the placeholder namespace for cluster-scoped issues.
	clusterScope = "_cluster"
)

// TemporalCorrelator groups issues that appeared within a configurable time
// window inside the same namespace. When multiple issues share a namespace
// and fall within the window they are likely related.
type TemporalCorrelator struct {
	Window time.Duration
}

// NewTemporalCorrelator returns a correlator with the given window.
func NewTemporalCorrelator(window time.Duration) *TemporalCorrelator {
	if window <= 0 {
		window = DefaultTimeWindow
	}
	return &TemporalCorrelator{Window: window}
}

// Correlate finds groups of issues in the same namespace that occurred within
// the time window. Issues are treated as having the same timestamp (the health
// check run time) when the input provides a single Timestamp. If multiple
// results come from different points in time, the window is applied across them.
func (tc *TemporalCorrelator) Correlate(input CorrelationInput) []CausalGroup {
	// Build flat list of events grouped by namespace.
	byNS := make(map[string][]TimelineEvent)
	for checkerName, result := range input.Results {
		if result == nil || result.Error != nil {
			continue
		}
		for _, issue := range result.Issues {
			ns := issue.Namespace
			if ns == "" {
				ns = clusterScope
			}
			byNS[ns] = append(byNS[ns], TimelineEvent{
				Timestamp: input.Timestamp,
				Checker:   checkerName,
				Issue:     issue,
			})
		}
	}

	var groups []CausalGroup
	for ns, events := range byNS {
		if len(events) < 2 {
			continue // single issue in a namespace isn't a temporal group
		}

		// Sort events by timestamp (stable for same-timestamp events).
		sort.SliceStable(events, func(i, j int) bool {
			return events[i].Timestamp.Before(events[j].Timestamp)
		})

		// Sliding-window grouping.
		used := make([]bool, len(events))
		for i := range events {
			if used[i] {
				continue
			}
			group := []TimelineEvent{events[i]}
			used[i] = true
			for j := i + 1; j < len(events); j++ {
				if used[j] {
					continue
				}
				if events[j].Timestamp.Sub(events[i].Timestamp) <= tc.Window {
					group = append(group, events[j])
					used[j] = true
				}
			}
			if len(group) < 2 {
				continue
			}
			groups = append(groups, buildTemporalGroup(ns, group))
		}
	}
	return groups
}

func buildTemporalGroup(namespace string, events []TimelineEvent) CausalGroup {
	// Determine highest severity.
	sev := highestSeverity(events)

	// Build a title from checker names involved.
	checkers := uniqueCheckers(events)
	title := fmt.Sprintf("%d related issues in %s (%s)", len(events), namespace, strings.Join(checkers, ", "))

	first := events[0].Timestamp
	last := events[len(events)-1].Timestamp

	return CausalGroup{
		ID:         fmt.Sprintf("temporal-%s-%d", namespace, first.UnixMilli()),
		Title:      title,
		Severity:   sev,
		Events:     events,
		Rule:       "temporal",
		Confidence: temporalConfidence(events),
		FirstSeen:  first,
		LastSeen:   last,
	}
}

func temporalConfidence(events []TimelineEvent) float64 {
	// More events and more distinct checkers = higher confidence.
	checkers := uniqueCheckers(events)
	base := 0.5
	if len(checkers) > 1 {
		base += 0.2
	}
	if len(events) >= 4 {
		base += 0.1
	}
	if base > 1.0 {
		base = 1.0
	}
	return base
}

func uniqueCheckers(events []TimelineEvent) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, e := range events {
		if _, ok := seen[e.Checker]; !ok {
			seen[e.Checker] = struct{}{}
			result = append(result, e.Checker)
		}
	}
	sort.Strings(result)
	return result
}

func highestSeverity(events []TimelineEvent) string {
	prio := map[string]int{
		checker.SeverityCritical: 3,
		checker.SeverityWarning:  2,
		checker.SeverityInfo:     1,
	}
	best := ""
	bestPrio := 0
	for _, e := range events {
		if p := prio[e.Issue.Severity]; p > bestPrio {
			bestPrio = p
			best = e.Issue.Severity
		}
	}
	return best
}
