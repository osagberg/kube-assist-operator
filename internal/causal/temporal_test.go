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
	"testing"
	"time"

	"github.com/osagberg/kube-assist-operator/internal/checker"
)

func TestTemporalCorrelator_Correlate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 2, 6, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		window     time.Duration
		results    map[string]*checker.CheckResult
		wantGroups int
	}{
		{
			name:   "empty input",
			window: DefaultTimeWindow,
			results: map[string]*checker.CheckResult{
				"workloads": {CheckerName: "workloads"},
			},
			wantGroups: 0,
		},
		{
			name:   "single issue no group",
			window: DefaultTimeWindow,
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "CrashLoop", Severity: "Critical", Resource: "pod/a", Namespace: "ns1", Message: "crash"},
					},
				},
			},
			wantGroups: 0,
		},
		{
			name:   "two issues same namespace grouped",
			window: DefaultTimeWindow,
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "CrashLoop", Severity: "Critical", Resource: "pod/a", Namespace: "ns1", Message: "crash"},
						{Type: "NoLimits", Severity: "Warning", Resource: "deployment/b", Namespace: "ns1", Message: "limits"},
					},
				},
			},
			wantGroups: 1,
		},
		{
			name:   "issues in different namespaces not grouped",
			window: DefaultTimeWindow,
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "CrashLoop", Severity: "Critical", Resource: "pod/a", Namespace: "ns1", Message: "crash"},
						{Type: "NoLimits", Severity: "Warning", Resource: "deployment/b", Namespace: "ns2", Message: "limits"},
					},
				},
			},
			wantGroups: 0,
		},
		{
			name:   "multiple checkers same namespace",
			window: DefaultTimeWindow,
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "CrashLoop", Severity: "Critical", Resource: "pod/a", Namespace: "ns1", Message: "crash"},
					},
				},
				"secrets": {
					CheckerName: "secrets",
					Issues: []checker.Issue{
						{Type: "Expired", Severity: "Critical", Resource: "secret/tls", Namespace: "ns1", Message: "expired"},
					},
				},
			},
			wantGroups: 1,
		},
		{
			name:   "issues without namespace use _cluster",
			window: DefaultTimeWindow,
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "A", Severity: "Warning", Resource: "pod/a", Namespace: "", Message: "a"},
						{Type: "B", Severity: "Warning", Resource: "pod/b", Namespace: "", Message: "b"},
					},
				},
			},
			wantGroups: 1,
		},
		{
			name:   "default window for zero duration",
			window: 0,
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "A", Severity: "Info", Resource: "pod/a", Namespace: "ns1", Message: "a"},
						{Type: "B", Severity: "Info", Resource: "pod/b", Namespace: "ns1", Message: "b"},
					},
				},
			},
			wantGroups: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewTemporalCorrelator(tt.window)
			input := CorrelationInput{
				Results:   tt.results,
				Timestamp: now,
			}
			groups := tc.Correlate(input)
			if len(groups) != tt.wantGroups {
				t.Errorf("groups = %d, want %d", len(groups), tt.wantGroups)
				for _, g := range groups {
					t.Logf("  group: %s (rule=%s, events=%d)", g.Title, g.Rule, len(g.Events))
				}
			}
		})
	}
}

func TestTemporalCorrelator_GroupMetadata(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 2, 6, 12, 0, 0, 0, time.UTC)
	tc := NewTemporalCorrelator(DefaultTimeWindow)

	input := CorrelationInput{
		Results: map[string]*checker.CheckResult{
			"workloads": {
				CheckerName: "workloads",
				Issues: []checker.Issue{
					{Type: "CrashLoop", Severity: "Critical", Resource: "pod/api", Namespace: "prod", Message: "crash"},
					{Type: "NoLimits", Severity: "Info", Resource: "deployment/api", Namespace: "prod", Message: "limits"},
				},
			},
		},
		Timestamp: now,
	}

	groups := tc.Correlate(input)
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(groups))
	}

	g := groups[0]
	if g.Rule != "temporal" {
		t.Errorf("Rule = %q, want temporal", g.Rule)
	}
	if g.Severity != checker.SeverityCritical {
		t.Errorf("Severity = %q, want %q", g.Severity, checker.SeverityCritical)
	}
	if g.ID == "" {
		t.Error("ID should not be empty")
	}
	if g.Title == "" {
		t.Error("Title should not be empty")
	}
	if len(g.Events) != 2 {
		t.Errorf("Events = %d, want 2", len(g.Events))
	}
}

func TestHighestSeverity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		events  []TimelineEvent
		wantSev string
	}{
		{
			name: "critical wins",
			events: []TimelineEvent{
				{Issue: checker.Issue{Severity: "Warning"}},
				{Issue: checker.Issue{Severity: "Critical"}},
				{Issue: checker.Issue{Severity: "Info"}},
			},
			wantSev: "Critical",
		},
		{
			name: "warning wins over info",
			events: []TimelineEvent{
				{Issue: checker.Issue{Severity: "Info"}},
				{Issue: checker.Issue{Severity: "Warning"}},
			},
			wantSev: "Warning",
		},
		{
			name: "single event",
			events: []TimelineEvent{
				{Issue: checker.Issue{Severity: "Info"}},
			},
			wantSev: "Info",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := highestSeverity(tt.events)
			if got != tt.wantSev {
				t.Errorf("highestSeverity() = %q, want %q", got, tt.wantSev)
			}
		})
	}
}
