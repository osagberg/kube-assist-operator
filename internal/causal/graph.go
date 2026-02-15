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
)

// resourceHierarchy maps Kubernetes resource kinds to their ownership level.
// Higher values indicate higher-level resources (Deployment > ReplicaSet > Pod).
var resourceHierarchy = map[string]int{
	"deployment":  3,
	"statefulset": 3,
	"daemonset":   3,
	"replicaset":  2,
	"pod":         1,
}

// ResourceGraphCorrelator groups issues whose resources belong to the same
// ownership chain. For example, a Deployment owns a ReplicaSet which owns Pods;
// issues across these resources share a root cause.
//
// This correlator uses simple heuristic name-prefix matching rather than
// querying the API server for ownerReferences, keeping it lightweight and
// test-friendly.
type ResourceGraphCorrelator struct{}

// NewResourceGraphCorrelator returns a new resource-graph correlator.
func NewResourceGraphCorrelator() *ResourceGraphCorrelator {
	return &ResourceGraphCorrelator{}
}

// Correlate groups issues by inferred resource ownership.
func (rg *ResourceGraphCorrelator) Correlate(input CorrelationInput) []CausalGroup {
	events := flattenEvents(input)
	if len(events) < 2 {
		return nil
	}

	// Group events by namespace first.
	byNS := make(map[string][]TimelineEvent)
	for _, e := range events {
		ns := e.Issue.Namespace
		if ns == "" {
			ns = clusterScope
		}
		byNS[ns] = append(byNS[ns], e)
	}

	var groups []CausalGroup
	for ns, nsEvents := range byNS {
		if len(nsEvents) < 2 {
			continue
		}

		// Parse resource references.
		type parsed struct {
			kind string
			name string
			idx  int
		}
		var resources []parsed
		for i, e := range nsEvents {
			kind, name := parseResource(e.Issue.Resource)
			resources = append(resources, parsed{kind: kind, name: name, idx: i})
		}

		// Find ownership chains: if resource A's name is a prefix of resource B's
		// name and A is a higher-level kind, they're related.
		// O(n²) pairwise scan — acceptable because issues rarely exceed ~50 per
		// namespace, and the inner loop short-circuits via the used[j] guard.
		used := make(map[int]bool)
		for i := range resources {
			if used[i] {
				continue
			}
			chain := []int{i}
			used[i] = true
			for j := range resources {
				if i == j || used[j] {
					continue
				}
				if relatedByOwnership(resources[i].kind, resources[i].name, resources[j].kind, resources[j].name) {
					chain = append(chain, j)
					used[j] = true
				}
			}
			if len(chain) < 2 {
				used[i] = false // release single-item so other chains can pick it up
				continue
			}

			var groupEvents []TimelineEvent
			for _, idx := range chain {
				groupEvents = append(groupEvents, nsEvents[idx])
			}
			groups = append(groups, buildGraphGroup(ns, groupEvents, input))
		}
	}
	return groups
}

func flattenEvents(input CorrelationInput) []TimelineEvent {
	var events []TimelineEvent
	for checkerName, result := range input.Results {
		if result == nil || result.Error != nil {
			continue
		}
		for _, issue := range result.Issues {
			events = append(events, TimelineEvent{
				Timestamp: input.Timestamp,
				Checker:   checkerName,
				Issue:     issue,
			})
		}
	}
	return events
}

// parseResource extracts kind and name from "kind/name" format.
func parseResource(resource string) (string, string) {
	parts := strings.SplitN(resource, "/", 2)
	if len(parts) == 2 {
		return strings.ToLower(parts[0]), parts[1]
	}
	return "", resource
}

// relatedByOwnership checks if two resources are in the same ownership chain
// using Kubernetes naming conventions and kind hierarchy.
func relatedByOwnership(kindA, nameA, kindB, nameB string) bool {
	// Same resource
	if kindA == kindB && nameA == nameB {
		return false
	}

	levelA := resourceHierarchy[kindA]
	levelB := resourceHierarchy[kindB]

	// Both must be in the hierarchy.
	if levelA == 0 || levelB == 0 {
		return false
	}

	// The higher-level resource's name should be a prefix of the lower-level.
	if levelA > levelB {
		return strings.HasPrefix(nameB, nameA+"-") || nameB == nameA
	}
	if levelB > levelA {
		return strings.HasPrefix(nameA, nameB+"-") || nameA == nameB
	}
	return false
}

func buildGraphGroup(namespace string, events []TimelineEvent, input CorrelationInput) CausalGroup {
	sev := highestSeverity(events)
	checkers := uniqueCheckers(events)

	// Find the highest-level resource to use in the title.
	var rootResource string
	rootLevel := 0
	for _, e := range events {
		kind, _ := parseResource(e.Issue.Resource)
		if l := resourceHierarchy[kind]; l > rootLevel {
			rootLevel = l
			rootResource = e.Issue.Resource
		}
	}
	if rootResource == "" && len(events) > 0 {
		rootResource = events[0].Issue.Resource
	}

	// Sort events for stable output.
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Issue.Resource < events[j].Issue.Resource
	})

	title := fmt.Sprintf("ownership chain: %s in %s (%s)", rootResource, namespace, strings.Join(checkers, ", "))

	return CausalGroup{
		ID:         fmt.Sprintf("graph-%s-%s-%d", namespace, rootResource, input.Timestamp.UnixMilli()),
		Title:      title,
		Severity:   sev,
		Events:     events,
		Rule:       "resource-graph",
		Confidence: graphConfidence(events),
		FirstSeen:  input.Timestamp,
		LastSeen:   input.Timestamp,
	}
}

func graphConfidence(events []TimelineEvent) float64 {
	// Longer chains = higher confidence.
	switch {
	case len(events) >= 3:
		return 0.9
	case len(events) == 2:
		return 0.7
	default:
		return 0.5
	}
}
