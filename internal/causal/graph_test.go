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

func TestResourceGraphCorrelator_Correlate(t *testing.T) {
	now := time.Date(2026, 2, 6, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		results    map[string]*checker.CheckResult
		wantGroups int
	}{
		{
			name:       "empty input",
			results:    map[string]*checker.CheckResult{},
			wantGroups: 0,
		},
		{
			name: "single issue no group",
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "Crash", Severity: "Critical", Resource: "pod/api-abc", Namespace: "ns1", Message: "crash"},
					},
				},
			},
			wantGroups: 0,
		},
		{
			name: "deployment and pod with name prefix",
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "NoLimits", Severity: "Warning", Resource: "deployment/api-server", Namespace: "prod", Message: "limits"},
						{Type: "CrashLoop", Severity: "Critical", Resource: "pod/api-server-5d4f8b7c9-abc", Namespace: "prod", Message: "crash"},
					},
				},
			},
			wantGroups: 1,
		},
		{
			name: "deployment and replicaset with name prefix",
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "NoLimits", Severity: "Warning", Resource: "deployment/web", Namespace: "prod", Message: "limits"},
						{Type: "Unhealthy", Severity: "Warning", Resource: "replicaset/web-abc", Namespace: "prod", Message: "unhealthy"},
					},
				},
			},
			wantGroups: 1,
		},
		{
			name: "three-level chain: deployment -> replicaset -> pod",
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "NoLimits", Severity: "Warning", Resource: "deployment/app", Namespace: "prod", Message: "limits"},
						{Type: "Scaling", Severity: "Info", Resource: "replicaset/app-abc", Namespace: "prod", Message: "scaling"},
						{Type: "Crash", Severity: "Critical", Resource: "pod/app-abc-xyz", Namespace: "prod", Message: "crash"},
					},
				},
			},
			wantGroups: 1,
		},
		{
			name: "unrelated pods not grouped",
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "Crash", Severity: "Critical", Resource: "pod/api-abc", Namespace: "prod", Message: "crash"},
						{Type: "Crash", Severity: "Critical", Resource: "pod/web-xyz", Namespace: "prod", Message: "crash"},
					},
				},
			},
			wantGroups: 0,
		},
		{
			name: "different namespaces not grouped",
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "NoLimits", Severity: "Warning", Resource: "deployment/api", Namespace: "prod", Message: "limits"},
						{Type: "Crash", Severity: "Critical", Resource: "pod/api-abc", Namespace: "staging", Message: "crash"},
					},
				},
			},
			wantGroups: 0,
		},
		{
			name: "non-hierarchy kinds not grouped",
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "Crash", Severity: "Critical", Resource: "pod/api-abc", Namespace: "prod", Message: "crash"},
					},
				},
				"secrets": {
					CheckerName: "secrets",
					Issues: []checker.Issue{
						{Type: "Expired", Severity: "Critical", Resource: "secret/api-tls", Namespace: "prod", Message: "expired"},
					},
				},
			},
			wantGroups: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rg := NewResourceGraphCorrelator()
			input := CorrelationInput{
				Results:   tt.results,
				Timestamp: now,
			}
			groups := rg.Correlate(input)
			if len(groups) != tt.wantGroups {
				t.Errorf("groups = %d, want %d", len(groups), tt.wantGroups)
				for _, g := range groups {
					t.Logf("  group: %s (events=%d, confidence=%.2f)", g.Title, len(g.Events), g.Confidence)
				}
			}
		})
	}
}

func TestParseResource(t *testing.T) {
	tests := []struct {
		resource string
		wantKind string
		wantName string
	}{
		{"deployment/api-server", "deployment", "api-server"},
		{"pod/api-server-abc", "pod", "api-server-abc"},
		{"replicaset/api-server-5d4f8b7c9", "replicaset", "api-server-5d4f8b7c9"},
		{"standalone", "", "standalone"},
	}

	for _, tt := range tests {
		t.Run(tt.resource, func(t *testing.T) {
			kind, name := parseResource(tt.resource)
			if kind != tt.wantKind {
				t.Errorf("kind = %q, want %q", kind, tt.wantKind)
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
		})
	}
}

func TestRelatedByOwnership(t *testing.T) {
	tests := []struct {
		name     string
		kindA    string
		nameA    string
		kindB    string
		nameB    string
		expected bool
	}{
		{"deployment owns pod", "deployment", "api", "pod", "api-abc-xyz", true},
		{"deployment owns replicaset", "deployment", "api", "replicaset", "api-abc", true},
		{"pod owned by deployment", "pod", "api-abc", "deployment", "api", true},
		{"same kind same name", "pod", "api", "pod", "api", false},
		{"same level different names", "deployment", "api", "deployment", "web", false},
		{"non-hierarchy kind", "deployment", "api", "service", "api-svc", false},
		{"no prefix match", "deployment", "api", "pod", "web-xyz", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := relatedByOwnership(tt.kindA, tt.nameA, tt.kindB, tt.nameB)
			if got != tt.expected {
				t.Errorf("relatedByOwnership(%s/%s, %s/%s) = %v, want %v",
					tt.kindA, tt.nameA, tt.kindB, tt.nameB, got, tt.expected)
			}
		})
	}
}

func TestGraphConfidence(t *testing.T) {
	tests := []struct {
		name       string
		eventCount int
		wantMin    float64
	}{
		{"two events", 2, 0.7},
		{"three events", 3, 0.9},
		{"five events", 5, 0.9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := make([]TimelineEvent, tt.eventCount)
			got := graphConfidence(events)
			if got < tt.wantMin {
				t.Errorf("graphConfidence(%d events) = %f, want >= %f", tt.eventCount, got, tt.wantMin)
			}
		})
	}
}
