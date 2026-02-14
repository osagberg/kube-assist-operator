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
	"errors"
	"testing"
	"time"

	"github.com/osagberg/kube-assist-operator/internal/checker"
)

func TestCorrelator_Analyze(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 2, 6, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name               string
		results            map[string]*checker.CheckResult
		wantGroups         int
		wantUncorrelated   int
		wantTotalIssues    int
		checkGroupContains func(t *testing.T, ctx *CausalContext)
	}{
		{
			name:             "no issues produces empty context",
			results:          map[string]*checker.CheckResult{},
			wantGroups:       0,
			wantUncorrelated: 0,
			wantTotalIssues:  0,
		},
		{
			name: "single issue produces no groups",
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Healthy:     5,
					Issues: []checker.Issue{
						{Type: "CrashLoopBackOff", Severity: "Critical", Resource: "deployment/api", Namespace: "prod", Message: "crash"},
					},
				},
			},
			wantGroups:       0,
			wantUncorrelated: 1,
			wantTotalIssues:  1,
		},
		{
			name: "OOM + quota rule fires",
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "OOMKilled", Severity: "Critical", Resource: "pod/api-abc", Namespace: "prod", Message: "container killed"},
					},
				},
				"quotas": {
					CheckerName: "quotas",
					Issues: []checker.Issue{
						{Type: "QuotaNearLimit", Severity: "Warning", Resource: "resourcequota/default", Namespace: "prod", Message: "memory at 85%"},
					},
				},
			},
			wantGroups:      1,
			wantTotalIssues: 2,
			checkGroupContains: func(t *testing.T, ctx *CausalContext) {
				found := false
				for _, g := range ctx.Groups {
					if g.Rule == "oom-quota" {
						found = true
						if g.Severity != "Critical" {
							t.Errorf("oom-quota group severity = %q, want Critical", g.Severity)
						}
						if g.Confidence < 0.8 {
							t.Errorf("oom-quota confidence = %f, want >= 0.8", g.Confidence)
						}
						if g.RootCause == "" {
							t.Error("oom-quota group has no RootCause")
						}
					}
				}
				if !found {
					t.Error("expected oom-quota group not found")
				}
			},
		},
		{
			name: "flux chain rule fires",
			results: map[string]*checker.CheckResult{
				"gitrepositories": {
					CheckerName: "gitrepositories",
					Issues: []checker.Issue{
						{Type: "CloneFailed", Severity: "Critical", Resource: "gitrepository/app-repo", Namespace: "flux-system", Message: "auth failed"},
					},
				},
				"helmreleases": {
					CheckerName: "helmreleases",
					Issues: []checker.Issue{
						{Type: "UpgradeFailed", Severity: "Critical", Resource: "helmrelease/app", Namespace: "flux-system", Message: "source not ready"},
					},
				},
			},
			wantGroups:      1,
			wantTotalIssues: 2,
			checkGroupContains: func(t *testing.T, ctx *CausalContext) {
				found := false
				for _, g := range ctx.Groups {
					if g.Rule == "flux-chain" {
						found = true
						if len(g.Events) != 2 {
							t.Errorf("flux-chain events = %d, want 2", len(g.Events))
						}
					}
				}
				if !found {
					t.Error("expected flux-chain group not found")
				}
			},
		},
		{
			name: "crash + imagepull rule fires",
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "CrashLoopBackOff", Severity: "Critical", Resource: "pod/web-abc", Namespace: "staging", Message: "container restarting"},
						{Type: "ImagePullBackOff", Severity: "Critical", Resource: "pod/web-def", Namespace: "staging", Message: "image not found"},
					},
				},
			},
			wantGroups:      1,
			wantTotalIssues: 2,
			checkGroupContains: func(t *testing.T, ctx *CausalContext) {
				for _, g := range ctx.Groups {
					if g.Rule == "crash-imagepull" {
						return
					}
				}
				t.Error("expected crash-imagepull group not found")
			},
		},
		{
			name: "issues in different namespaces are not grouped by rules",
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "OOMKilled", Severity: "Critical", Resource: "pod/api-abc", Namespace: "prod", Message: "oom"},
					},
				},
				"quotas": {
					CheckerName: "quotas",
					Issues: []checker.Issue{
						{Type: "QuotaNearLimit", Severity: "Warning", Resource: "resourcequota/default", Namespace: "staging", Message: "quota"},
					},
				},
			},
			wantGroups:       0,
			wantUncorrelated: 2,
			wantTotalIssues:  2,
		},
		{
			name: "multiple issues same namespace get temporal grouping",
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "NoLimits", Severity: "Info", Resource: "deployment/a", Namespace: "dev", Message: "no limits"},
						{Type: "NoProbe", Severity: "Info", Resource: "deployment/b", Namespace: "dev", Message: "no probe"},
						{Type: "NoProbe", Severity: "Info", Resource: "deployment/c", Namespace: "dev", Message: "no probe"},
					},
				},
			},
			wantTotalIssues: 3,
			checkGroupContains: func(t *testing.T, ctx *CausalContext) {
				if len(ctx.Groups) == 0 {
					t.Error("expected at least one temporal group")
				}
				for _, g := range ctx.Groups {
					if g.Rule == "temporal" {
						return
					}
				}
				t.Error("expected temporal group not found")
			},
		},
		{
			name: "resource graph groups deployment and pod issues",
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "CrashLoopBackOff", Severity: "Critical", Resource: "pod/api-server-abc123", Namespace: "prod", Message: "crash"},
						{Type: "NoLimits", Severity: "Warning", Resource: "deployment/api-server", Namespace: "prod", Message: "no limits"},
					},
				},
			},
			wantTotalIssues: 2,
			checkGroupContains: func(t *testing.T, ctx *CausalContext) {
				for _, g := range ctx.Groups {
					if g.Rule == "resource-graph" {
						return
					}
				}
				t.Error("expected resource-graph group not found")
			},
		},
		{
			name: "error results are skipped",
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Error:       errors.New("checker not supported"),
				},
			},
			wantGroups:      0,
			wantTotalIssues: 0,
		},
		{
			name: "nil results are skipped",
			results: map[string]*checker.CheckResult{
				"workloads": nil,
			},
			wantGroups:      0,
			wantTotalIssues: 0,
		},
		{
			name: "groups sorted by severity then confidence",
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "OOMKilled", Severity: "Critical", Resource: "pod/a", Namespace: "ns1", Message: "oom"},
						{Type: "NoProbe", Severity: "Info", Resource: "deployment/x", Namespace: "ns2", Message: "probe"},
						{Type: "NoLimits", Severity: "Info", Resource: "deployment/y", Namespace: "ns2", Message: "limits"},
					},
				},
				"quotas": {
					CheckerName: "quotas",
					Issues: []checker.Issue{
						{Type: "QuotaNearLimit", Severity: "Warning", Resource: "resourcequota/q", Namespace: "ns1", Message: "quota"},
					},
				},
			},
			wantTotalIssues: 4,
			checkGroupContains: func(t *testing.T, ctx *CausalContext) {
				if len(ctx.Groups) == 0 {
					t.Fatal("expected at least one group")
				}
				// First group should be the highest severity.
				if ctx.Groups[0].Severity != "Critical" {
					t.Errorf("first group severity = %q, want Critical", ctx.Groups[0].Severity)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCorrelator()
			input := CorrelationInput{
				Results:   tt.results,
				Timestamp: now,
			}
			ctx := c.Analyze(input)

			if ctx.TotalIssues != tt.wantTotalIssues {
				t.Errorf("TotalIssues = %d, want %d", ctx.TotalIssues, tt.wantTotalIssues)
			}

			if tt.wantGroups > 0 && len(ctx.Groups) != tt.wantGroups {
				t.Errorf("Groups = %d, want %d", len(ctx.Groups), tt.wantGroups)
			}

			if tt.wantUncorrelated > 0 && ctx.UncorrelatedCount != tt.wantUncorrelated {
				t.Errorf("UncorrelatedCount = %d, want %d", ctx.UncorrelatedCount, tt.wantUncorrelated)
			}

			if tt.checkGroupContains != nil {
				tt.checkGroupContains(t, ctx)
			}
		})
	}
}
