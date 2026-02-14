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
	"strings"
	"testing"
	"time"

	"github.com/osagberg/kube-assist-operator/internal/checker"
)

func TestOOMQuotaRule(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 2, 6, 12, 0, 0, 0, time.UTC)
	rules := []CrossCheckerRule{oomQuotaRule()}

	tests := []struct {
		name       string
		results    map[string]*checker.CheckResult
		wantGroups int
		wantRoot   string
	}{
		{
			name: "OOM + quota matches",
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues:      []checker.Issue{{Type: "OOMKilled", Severity: "Critical", Namespace: "prod", Resource: "pod/a", Message: "oom"}},
				},
				"quotas": {
					CheckerName: "quotas",
					Issues:      []checker.Issue{{Type: "QuotaNearLimit", Severity: "Warning", Namespace: "prod", Resource: "resourcequota/q", Message: "85%"}},
				},
			},
			wantGroups: 1,
			wantRoot:   "OOM kills",
		},
		{
			name: "OOM without quota does not match",
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues:      []checker.Issue{{Type: "OOMKilled", Severity: "Critical", Namespace: "prod", Resource: "pod/a", Message: "oom"}},
				},
			},
			wantGroups: 0,
		},
		{
			name: "different namespaces do not match",
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues:      []checker.Issue{{Type: "OOMKilled", Severity: "Critical", Namespace: "prod", Resource: "pod/a", Message: "oom"}},
				},
				"quotas": {
					CheckerName: "quotas",
					Issues:      []checker.Issue{{Type: "QuotaNearLimit", Severity: "Warning", Namespace: "staging", Resource: "resourcequota/q", Message: "85%"}},
				},
			},
			wantGroups: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups := applyCrossCheckerRules(rules, CorrelationInput{Results: tt.results, Timestamp: now})
			if len(groups) != tt.wantGroups {
				t.Errorf("groups = %d, want %d", len(groups), tt.wantGroups)
			}
			if tt.wantRoot != "" && len(groups) > 0 {
				if !strings.Contains(groups[0].RootCause, tt.wantRoot) {
					t.Errorf("RootCause = %q, want to contain %q", groups[0].RootCause, tt.wantRoot)
				}
			}
		})
	}
}

func TestCrashImagePullRule(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 2, 6, 12, 0, 0, 0, time.UTC)
	rules := []CrossCheckerRule{crashImagePullRule()}

	tests := []struct {
		name       string
		results    map[string]*checker.CheckResult
		wantGroups int
	}{
		{
			name: "crash + imagepull matches",
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "CrashLoopBackOff", Severity: "Critical", Namespace: "ns1", Resource: "pod/a", Message: "crash"},
						{Type: "ImagePullBackOff", Severity: "Critical", Namespace: "ns1", Resource: "pod/b", Message: "pull fail"},
					},
				},
			},
			wantGroups: 1,
		},
		{
			name: "crash only does not match",
			results: map[string]*checker.CheckResult{
				"workloads": {
					CheckerName: "workloads",
					Issues: []checker.Issue{
						{Type: "CrashLoopBackOff", Severity: "Critical", Namespace: "ns1", Resource: "pod/a", Message: "crash"},
					},
				},
			},
			wantGroups: 0,
		},
		{
			name: "non-workloads checker ignored",
			results: map[string]*checker.CheckResult{
				"secrets": {
					CheckerName: "secrets",
					Issues: []checker.Issue{
						{Type: "CrashLoopBackOff", Severity: "Critical", Namespace: "ns1", Resource: "secret/a", Message: "crash"},
					},
				},
			},
			wantGroups: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups := applyCrossCheckerRules(rules, CorrelationInput{Results: tt.results, Timestamp: now})
			if len(groups) != tt.wantGroups {
				t.Errorf("groups = %d, want %d", len(groups), tt.wantGroups)
			}
		})
	}
}

func TestFluxChainRule(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 2, 6, 12, 0, 0, 0, time.UTC)
	rules := []CrossCheckerRule{fluxChainRule()}

	tests := []struct {
		name       string
		results    map[string]*checker.CheckResult
		wantGroups int
	}{
		{
			name: "git source + helmrelease matches",
			results: map[string]*checker.CheckResult{
				"gitrepositories": {
					CheckerName: "gitrepositories",
					Issues:      []checker.Issue{{Type: "CloneFailed", Severity: "Critical", Namespace: "flux", Resource: "gitrepository/app", Message: "fail"}},
				},
				"helmreleases": {
					CheckerName: "helmreleases",
					Issues:      []checker.Issue{{Type: "UpgradeFailed", Severity: "Critical", Namespace: "flux", Resource: "helmrelease/app", Message: "fail"}},
				},
			},
			wantGroups: 1,
		},
		{
			name: "git source + kustomization matches",
			results: map[string]*checker.CheckResult{
				"gitrepositories": {
					CheckerName: "gitrepositories",
					Issues:      []checker.Issue{{Type: "AuthFailed", Severity: "Critical", Namespace: "flux", Resource: "gitrepository/infra", Message: "auth"}},
				},
				"kustomizations": {
					CheckerName: "kustomizations",
					Issues:      []checker.Issue{{Type: "BuildFailed", Severity: "Critical", Namespace: "flux", Resource: "kustomization/infra", Message: "build"}},
				},
			},
			wantGroups: 1,
		},
		{
			name: "info severity not matched",
			results: map[string]*checker.CheckResult{
				"gitrepositories": {
					CheckerName: "gitrepositories",
					Issues:      []checker.Issue{{Type: "Suspended", Severity: "Info", Namespace: "flux", Resource: "gitrepository/app", Message: "suspended"}},
				},
				"helmreleases": {
					CheckerName: "helmreleases",
					Issues:      []checker.Issue{{Type: "Suspended", Severity: "Info", Namespace: "flux", Resource: "helmrelease/app", Message: "suspended"}},
				},
			},
			wantGroups: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups := applyCrossCheckerRules(rules, CorrelationInput{Results: tt.results, Timestamp: now})
			if len(groups) != tt.wantGroups {
				t.Errorf("groups = %d, want %d", len(groups), tt.wantGroups)
			}
		})
	}
}

func TestPVCWorkloadRule(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 2, 6, 12, 0, 0, 0, time.UTC)
	rules := []CrossCheckerRule{pvcWorkloadRule()}

	tests := []struct {
		name       string
		results    map[string]*checker.CheckResult
		wantGroups int
	}{
		{
			name: "PVC pending + workload pending matches",
			results: map[string]*checker.CheckResult{
				"pvcs": {
					CheckerName: "pvcs",
					Issues:      []checker.Issue{{Type: "PVCPending", Severity: "Warning", Namespace: "data", Resource: "pvc/data-vol", Message: "pending"}},
				},
				"workloads": {
					CheckerName: "workloads",
					Issues:      []checker.Issue{{Type: "Pending (Unschedulable)", Severity: "Critical", Namespace: "data", Resource: "pod/db-0", Message: "pending"}},
				},
			},
			wantGroups: 1,
		},
		{
			name: "PVC pending but no workload issue does not match",
			results: map[string]*checker.CheckResult{
				"pvcs": {
					CheckerName: "pvcs",
					Issues:      []checker.Issue{{Type: "PVCPending", Severity: "Warning", Namespace: "data", Resource: "pvc/data-vol", Message: "pending"}},
				},
			},
			wantGroups: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups := applyCrossCheckerRules(rules, CorrelationInput{Results: tt.results, Timestamp: now})
			if len(groups) != tt.wantGroups {
				t.Errorf("groups = %d, want %d", len(groups), tt.wantGroups)
			}
		})
	}
}

func TestDefaultRules_Count(t *testing.T) {
	t.Parallel()
	rules := DefaultRules()
	if len(rules) != 4 {
		t.Errorf("DefaultRules() = %d rules, want 4", len(rules))
	}

	names := make(map[string]bool)
	for _, r := range rules {
		if names[r.Name] {
			t.Errorf("duplicate rule name: %s", r.Name)
		}
		names[r.Name] = true
		if r.Confidence <= 0 || r.Confidence > 1 {
			t.Errorf("rule %s confidence = %f, want (0, 1]", r.Name, r.Confidence)
		}
		if len(r.RequiredTags) < 2 {
			t.Errorf("rule %s has %d required tags, want >= 2", r.Name, len(r.RequiredTags))
		}
	}
}
