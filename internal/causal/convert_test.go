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

func TestToAIContext(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 14, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name              string
		input             *CausalContext
		wantNil           bool
		wantGroups        int
		wantUncorrelated  int
		wantTotalIssues   int
		checkResources    func(t *testing.T, groups []string)
		checkGroupDetails func(t *testing.T, result any)
	}{
		{
			name:    "nil input returns nil",
			input:   nil,
			wantNil: true,
		},
		{
			name: "empty groups returns nil",
			input: &CausalContext{
				Groups:            []CausalGroup{},
				UncorrelatedCount: 3,
				TotalIssues:       3,
			},
			wantNil: true,
		},
		{
			name: "single group with single event",
			input: &CausalContext{
				Groups: []CausalGroup{
					{
						Rule:       "oom-cascade",
						Title:      "OOM cascade in default",
						RootCause:  "Memory limit too low",
						Severity:   "Critical",
						Confidence: 0.9,
						Events: []TimelineEvent{
							{
								Timestamp: now,
								Checker:   "workloads",
								Issue: checker.Issue{
									Namespace: "default",
									Resource:  "deployment/api-server",
								},
							},
						},
					},
				},
				UncorrelatedCount: 1,
				TotalIssues:       2,
			},
			wantNil:          false,
			wantGroups:       1,
			wantUncorrelated: 1,
			wantTotalIssues:  2,
		},
		{
			name: "single group with multiple events",
			input: &CausalContext{
				Groups: []CausalGroup{
					{
						Rule:       "cascade-failure",
						Title:      "Cascade in prod",
						RootCause:  "Node pressure",
						Severity:   "Critical",
						Confidence: 0.85,
						Events: []TimelineEvent{
							{
								Timestamp: now,
								Checker:   "workloads",
								Issue: checker.Issue{
									Namespace: "prod",
									Resource:  "deployment/web",
								},
							},
							{
								Timestamp: now.Add(time.Second),
								Checker:   "workloads",
								Issue: checker.Issue{
									Namespace: "prod",
									Resource:  "deployment/api",
								},
							},
						},
					},
				},
				UncorrelatedCount: 0,
				TotalIssues:       2,
			},
			wantNil:          false,
			wantGroups:       1,
			wantUncorrelated: 0,
			wantTotalIssues:  2,
		},
		{
			name: "multiple groups",
			input: &CausalContext{
				Groups: []CausalGroup{
					{
						Rule:       "oom-cascade",
						Title:      "OOM cascade",
						Severity:   "Critical",
						Confidence: 0.9,
						Events: []TimelineEvent{
							{
								Timestamp: now,
								Checker:   "workloads",
								Issue: checker.Issue{
									Namespace: "ns1",
									Resource:  "deployment/app1",
								},
							},
						},
					},
					{
						Rule:       "network-issue",
						Title:      "Network problems",
						Severity:   "Warning",
						Confidence: 0.7,
						Events: []TimelineEvent{
							{
								Timestamp: now,
								Checker:   "resources",
								Issue: checker.Issue{
									Namespace: "ns2",
									Resource:  "service/svc1",
								},
							},
						},
					},
				},
				UncorrelatedCount: 5,
				TotalIssues:       7,
			},
			wantNil:          false,
			wantGroups:       2,
			wantUncorrelated: 5,
			wantTotalIssues:  7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ToAIContext(tt.input)

			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Fatal("expected non-nil result")
			}

			if got := len(result.Groups); got != tt.wantGroups {
				t.Errorf("groups count = %d, want %d", got, tt.wantGroups)
			}
			if result.UncorrelatedCount != tt.wantUncorrelated {
				t.Errorf("UncorrelatedCount = %d, want %d", result.UncorrelatedCount, tt.wantUncorrelated)
			}
			if result.TotalIssues != tt.wantTotalIssues {
				t.Errorf("TotalIssues = %d, want %d", result.TotalIssues, tt.wantTotalIssues)
			}

			// Verify group fields are correctly mapped
			for i, group := range result.Groups {
				srcGroup := tt.input.Groups[i]
				if group.Rule != srcGroup.Rule {
					t.Errorf("group[%d].Rule = %q, want %q", i, group.Rule, srcGroup.Rule)
				}
				if group.Title != srcGroup.Title {
					t.Errorf("group[%d].Title = %q, want %q", i, group.Title, srcGroup.Title)
				}
				if group.RootCause != srcGroup.RootCause {
					t.Errorf("group[%d].RootCause = %q, want %q", i, group.RootCause, srcGroup.RootCause)
				}
				if group.Severity != srcGroup.Severity {
					t.Errorf("group[%d].Severity = %q, want %q", i, group.Severity, srcGroup.Severity)
				}
				if group.Confidence != srcGroup.Confidence {
					t.Errorf("group[%d].Confidence = %f, want %f", i, group.Confidence, srcGroup.Confidence)
				}

				// Verify resources are namespace/resource format
				wantResources := len(srcGroup.Events)
				if len(group.Resources) != wantResources {
					t.Errorf("group[%d].Resources count = %d, want %d", i, len(group.Resources), wantResources)
				}
				for j, event := range srcGroup.Events {
					wantResource := event.Issue.Namespace + "/" + event.Issue.Resource
					if j < len(group.Resources) && group.Resources[j] != wantResource {
						t.Errorf("group[%d].Resources[%d] = %q, want %q", i, j, group.Resources[j], wantResource)
					}
				}
			}
		})
	}
}
