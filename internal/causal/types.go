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

// Package causal provides correlation analysis for health check results.
// It groups related issues by time window, resource ownership, and cross-checker
// rules to surface root causes rather than individual symptoms.
package causal

import (
	"time"

	"github.com/osagberg/kube-assist-operator/internal/checker"
)

// CausalGroup is a set of related issues that likely share a root cause.
type CausalGroup struct {
	// ID is a stable identifier derived from the group's primary issue.
	ID string `json:"id"`

	// Title is a human-readable summary (e.g., "OOM + quota pressure in namespace X").
	Title string `json:"title"`

	// RootCause is the inferred root cause, if one can be determined.
	RootCause string `json:"rootCause,omitempty"`

	// Severity is the highest severity among the grouped issues.
	Severity string `json:"severity"`

	// Events are the individual issues in chronological order.
	Events []TimelineEvent `json:"events"`

	// Rule identifies which correlation rule produced this group.
	Rule string `json:"rule"`

	// Confidence is a 0–1 score indicating how confident the correlation is.
	Confidence float64 `json:"confidence"`

	// FirstSeen is the earliest timestamp among the events.
	FirstSeen time.Time `json:"firstSeen"`

	// LastSeen is the latest timestamp among the events.
	LastSeen time.Time `json:"lastSeen"`
}

// TimelineEvent wraps a checker issue with timing metadata.
type TimelineEvent struct {
	// Timestamp is when this issue was observed.
	Timestamp time.Time `json:"timestamp"`

	// Checker is the name of the checker that found the issue.
	Checker string `json:"checker"`

	// Issue is the underlying checker issue.
	Issue checker.Issue `json:"issue"`
}

// CausalContext is the enriched context that can be passed to AI providers
// for deeper root-cause analysis.
type CausalContext struct {
	// Groups is the set of correlated issue groups.
	Groups []CausalGroup `json:"groups"`

	// UncorrelatedCount is the number of issues that didn't match any rule.
	UncorrelatedCount int `json:"uncorrelatedCount"`

	// TotalIssues is the total number of issues analyzed.
	TotalIssues int `json:"totalIssues"`
}

// CorrelationInput bundles everything the correlator needs.
type CorrelationInput struct {
	// Results are the checker results keyed by checker name.
	Results map[string]*checker.CheckResult

	// Timestamp is when the health check was performed.
	Timestamp time.Time
}
