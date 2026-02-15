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

package flux

import (
	"context"
	"testing"
	"time"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/testutil"
)

// ---------------------------------------------------------------------------
// GitRepositoryChecker tests
// ---------------------------------------------------------------------------

func TestGitRepositoryChecker_Name(t *testing.T) {
	c := NewGitRepositoryChecker()
	if c.Name() != GitRepositoryCheckerName {
		t.Errorf("Name() = %v, want %v", c.Name(), GitRepositoryCheckerName)
	}
}

func TestGitRepositoryChecker_Check(t *testing.T) {
	tests := []struct {
		name        string
		gr          *sourcev1.GitRepository
		wantHealthy int
		wantTypes   map[string]string // issue type -> expected severity
	}{
		{
			name: "healthy git repository",
			gr: &sourcev1.GitRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "healthy-repo",
					Namespace: "flux-system",
				},
				Spec: sourcev1.GitRepositorySpec{
					URL: "https://github.com/example/repo",
				},
				Status: sourcev1.GitRepositoryStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							Reason:             "Succeeded",
						},
					},
					Artifact: &fluxmeta.Artifact{
						Revision: "main@sha1:abc123",
					},
				},
			},
			wantHealthy: 1,
			wantTypes:   map[string]string{},
		},
		{
			name: "suspended git repository",
			gr: &sourcev1.GitRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "suspended-repo",
					Namespace: "flux-system",
				},
				Spec: sourcev1.GitRepositorySpec{
					URL:     "https://github.com/example/repo",
					Suspend: true,
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"Suspended": checker.SeverityWarning},
		},
		{
			name: "not ready - GitOperationFailed",
			gr: &sourcev1.GitRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "failed-repo",
					Namespace: "flux-system",
				},
				Spec: sourcev1.GitRepositorySpec{
					URL: "https://github.com/example/repo",
				},
				Status: sourcev1.GitRepositoryStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "GitOperationFailed",
							Message:            "git operation failed",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"GitOperationFailed": checker.SeverityCritical},
		},
		{
			name: "not ready - AuthenticationFailed",
			gr: &sourcev1.GitRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auth-failed-repo",
					Namespace: "flux-system",
				},
				Spec: sourcev1.GitRepositorySpec{
					URL: "https://github.com/example/repo",
				},
				Status: sourcev1.GitRepositoryStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "AuthenticationFailed",
							Message:            "auth failed",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"AuthenticationFailed": checker.SeverityCritical},
		},
		{
			name: "not ready - CheckoutFailed",
			gr: &sourcev1.GitRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "checkout-failed-repo",
					Namespace: "flux-system",
				},
				Spec: sourcev1.GitRepositorySpec{
					URL: "https://github.com/example/repo",
				},
				Status: sourcev1.GitRepositoryStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "CheckoutFailed",
							Message:            "checkout failed",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"CheckoutFailed": checker.SeverityCritical},
		},
		{
			name: "not ready - CloneFailed",
			gr: &sourcev1.GitRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "clone-failed-repo",
					Namespace: "flux-system",
				},
				Spec: sourcev1.GitRepositorySpec{
					URL: "https://github.com/example/repo",
				},
				Status: sourcev1.GitRepositoryStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "CloneFailed",
							Message:            "clone failed",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"CloneFailed": checker.SeverityCritical},
		},
		{
			name: "not ready - StorageOperationFailed",
			gr: &sourcev1.GitRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storage-failed-repo",
					Namespace: "flux-system",
				},
				Spec: sourcev1.GitRepositorySpec{
					URL: "https://github.com/example/repo",
				},
				Status: sourcev1.GitRepositoryStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "StorageOperationFailed",
							Message:            "storage error",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"StorageOperationFailed": checker.SeverityCritical},
		},
		{
			name: "not ready - warning severity reason",
			gr: &sourcev1.GitRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "warn-repo",
					Namespace: "flux-system",
				},
				Spec: sourcev1.GitRepositorySpec{
					URL: "https://github.com/example/repo",
				},
				Status: sourcev1.GitRepositoryStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "SomeOtherReason",
							Message:            "something happened",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"SomeOtherReason": checker.SeverityWarning},
		},
		{
			name: "stale reconciliation",
			gr: &sourcev1.GitRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "stale-repo",
					Namespace: "flux-system",
				},
				Spec: sourcev1.GitRepositorySpec{
					URL: "https://github.com/example/repo",
				},
				Status: sourcev1.GitRepositoryStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							Reason:             "Succeeded",
						},
					},
					Artifact: &fluxmeta.Artifact{
						Revision:       "main@sha1:abc123",
						LastUpdateTime: metav1.NewTime(time.Now().Add(-3 * time.Hour)),
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"StaleReconciliation": checker.SeverityWarning},
		},
		{
			name: "ready but no artifact",
			gr: &sourcev1.GitRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-artifact-repo",
					Namespace: "flux-system",
				},
				Spec: sourcev1.GitRepositorySpec{
					URL: "https://github.com/example/repo",
				},
				Status: sourcev1.GitRepositoryStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							Reason:             "Succeeded",
						},
					},
					// No Artifact set
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"NoArtifact": checker.SeverityWarning},
		},
		{
			name: "private SSH URL without secretRef",
			gr: &sourcev1.GitRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "private-repo",
					Namespace: "flux-system",
				},
				Spec: sourcev1.GitRepositorySpec{
					URL: "git@github.com:example/private.git",
					// No SecretRef
				},
				Status: sourcev1.GitRepositoryStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							Reason:             "Succeeded",
						},
					},
					Artifact: &fluxmeta.Artifact{
						Revision: "main@sha1:abc123",
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"NoAuthentication": checker.SeverityWarning},
		},
		{
			name: "private SSH URL with secretRef is healthy",
			gr: &sourcev1.GitRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "private-with-secret",
					Namespace: "flux-system",
				},
				Spec: sourcev1.GitRepositorySpec{
					URL: "git@github.com:example/private.git",
					SecretRef: &fluxmeta.LocalObjectReference{
						Name: "git-creds",
					},
				},
				Status: sourcev1.GitRepositoryStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							Reason:             "Succeeded",
						},
					},
					Artifact: &fluxmeta.Artifact{
						Revision: "main@sha1:abc123",
					},
				},
			},
			wantHealthy: 1,
			wantTypes:   map[string]string{},
		},
		{
			name: "SSH URL prefix triggers NoAuthentication",
			gr: &sourcev1.GitRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ssh-repo",
					Namespace: "flux-system",
				},
				Spec: sourcev1.GitRepositorySpec{
					URL: "ssh://git@github.com/example/repo",
				},
				Status: sourcev1.GitRepositoryStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							Reason:             "Succeeded",
						},
					},
					Artifact: &fluxmeta.Artifact{
						Revision: "main@sha1:abc123",
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"NoAuthentication": checker.SeverityWarning},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewGitRepositoryChecker()
			// Use a very short stale threshold for testing
			c.staleThreshold = time.Hour
			checkCtx := testutil.NewCheckContext(t, []string{tt.gr.Namespace}, tt.gr)

			result, err := c.Check(context.Background(), checkCtx)
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}

			if result.Healthy != tt.wantHealthy {
				t.Errorf("Check() healthy = %d, want %d", result.Healthy, tt.wantHealthy)
			}

			// Check that all expected issue types are present with correct severity
			for wantType, wantSeverity := range tt.wantTypes {
				found := false
				for _, issue := range result.Issues {
					if issue.Type == wantType {
						found = true
						if issue.Severity != wantSeverity {
							t.Errorf("Issue type %s: severity = %s, want %s", wantType, issue.Severity, wantSeverity)
						}
						break
					}
				}
				if !found {
					t.Errorf("Check() did not find expected issue type %s", wantType)
				}
			}
		})
	}
}

func TestGitRepositoryChecker_EmptyList(t *testing.T) {
	c := NewGitRepositoryChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"})

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if result.Healthy != 0 {
		t.Errorf("Check() healthy = %d, want 0", result.Healthy)
	}
	if len(result.Issues) != 0 {
		t.Errorf("Check() issues = %d, want 0", len(result.Issues))
	}
}

func TestGetSuggestionForGitRepoReason(t *testing.T) {
	tests := []struct {
		name     string
		reason   string
		contains string
	}{
		{
			name:     "GitOperationFailed",
			reason:   "GitOperationFailed",
			contains: "Git operation failed",
		},
		{
			name:     "AuthenticationFailed",
			reason:   "AuthenticationFailed",
			contains: "Authentication to the Git repository failed",
		},
		{
			name:     "CheckoutFailed",
			reason:   "CheckoutFailed",
			contains: "Git checkout failed",
		},
		{
			name:     "CloneFailed",
			reason:   "CloneFailed",
			contains: "Git clone failed",
		},
		{
			name:     "StorageOperationFailed",
			reason:   "StorageOperationFailed",
			contains: "Source controller storage error",
		},
		{
			name:     "ReconciliationFailed",
			reason:   "ReconciliationFailed",
			contains: "Reconciliation failed",
		},
		{
			name:     "unknown reason",
			reason:   "SomethingUnknown",
			contains: "Check GitRepository status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getSuggestionForGitRepoReason(tt.reason)
			if got == "" {
				t.Error("getSuggestionForGitRepoReason() returned empty string")
			}
			if len(got) < len(tt.contains) {
				t.Errorf("getSuggestionForGitRepoReason() too short: %q", got)
			}
			// Verify suggestion contains expected substring
			found := false
			for i := 0; i <= len(got)-len(tt.contains); i++ {
				if got[i:i+len(tt.contains)] == tt.contains {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("getSuggestionForGitRepoReason(%s) = %q, want substring %q", tt.reason, got, tt.contains)
			}
		})
	}
}

func TestIsPrivateRepo(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{
			name: "SSH URL with git@ prefix",
			url:  "git@github.com:example/repo.git",
			want: true,
		},
		{
			name: "SSH URL with ssh:// prefix",
			url:  "ssh://git@github.com/example/repo",
			want: true,
		},
		{
			name: "HTTPS URL",
			url:  "https://github.com/example/repo",
			want: false,
		},
		{
			name: "HTTP URL",
			url:  "http://github.com/example/repo",
			want: false,
		},
		{
			name: "empty URL",
			url:  "",
			want: false,
		},
		{
			name: "short URL",
			url:  "abc",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPrivateRepo(tt.url); got != tt.want {
				t.Errorf("isPrivateRepo(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// HelmReleaseChecker tests
// ---------------------------------------------------------------------------

func TestHelmReleaseChecker_Name(t *testing.T) {
	c := NewHelmReleaseChecker()
	if c.Name() != HelmReleaseCheckerName {
		t.Errorf("Name() = %v, want %v", c.Name(), HelmReleaseCheckerName)
	}
}

func TestHelmReleaseChecker_Check(t *testing.T) {
	tests := []struct {
		name        string
		hr          *helmv2.HelmRelease
		wantHealthy int
		wantTypes   map[string]string
	}{
		{
			name: "healthy helm release",
			hr: &helmv2.HelmRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "healthy-release",
					Namespace: "flux-system",
				},
				Spec: helmv2.HelmReleaseSpec{
					Interval: metav1.Duration{Duration: 5 * time.Minute},
				},
				Status: helmv2.HelmReleaseStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							Reason:             "Succeeded",
						},
					},
				},
			},
			wantHealthy: 1,
			wantTypes:   map[string]string{},
		},
		{
			name: "suspended helm release",
			hr: &helmv2.HelmRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "suspended-release",
					Namespace: "flux-system",
				},
				Spec: helmv2.HelmReleaseSpec{
					Suspend:  true,
					Interval: metav1.Duration{Duration: 5 * time.Minute},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"Suspended": checker.SeverityWarning},
		},
		{
			name: "not ready - UpgradeFailed",
			hr: &helmv2.HelmRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "upgrade-failed",
					Namespace: "flux-system",
				},
				Spec: helmv2.HelmReleaseSpec{
					Interval: metav1.Duration{Duration: 5 * time.Minute},
				},
				Status: helmv2.HelmReleaseStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "UpgradeFailed",
							Message:            "upgrade failed",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"UpgradeFailed": checker.SeverityCritical},
		},
		{
			name: "not ready - InstallFailed",
			hr: &helmv2.HelmRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "install-failed",
					Namespace: "flux-system",
				},
				Spec: helmv2.HelmReleaseSpec{
					Interval: metav1.Duration{Duration: 5 * time.Minute},
				},
				Status: helmv2.HelmReleaseStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "InstallFailed",
							Message:            "install failed",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"InstallFailed": checker.SeverityCritical},
		},
		{
			name: "not ready - UninstallFailed",
			hr: &helmv2.HelmRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "uninstall-failed",
					Namespace: "flux-system",
				},
				Spec: helmv2.HelmReleaseSpec{
					Interval: metav1.Duration{Duration: 5 * time.Minute},
				},
				Status: helmv2.HelmReleaseStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "UninstallFailed",
							Message:            "uninstall failed",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"UninstallFailed": checker.SeverityCritical},
		},
		{
			name: "not ready - ArtifactFailed",
			hr: &helmv2.HelmRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "artifact-failed",
					Namespace: "flux-system",
				},
				Spec: helmv2.HelmReleaseSpec{
					Interval: metav1.Duration{Duration: 5 * time.Minute},
				},
				Status: helmv2.HelmReleaseStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "ArtifactFailed",
							Message:            "artifact failed",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"ArtifactFailed": checker.SeverityCritical},
		},
		{
			name: "not ready - ChartPullFailed",
			hr: &helmv2.HelmRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "chartpull-failed",
					Namespace: "flux-system",
				},
				Spec: helmv2.HelmReleaseSpec{
					Interval: metav1.Duration{Duration: 5 * time.Minute},
				},
				Status: helmv2.HelmReleaseStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "ChartPullFailed",
							Message:            "chart pull failed",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"ChartPullFailed": checker.SeverityCritical},
		},
		{
			name: "not ready - ReconciliationFailed",
			hr: &helmv2.HelmRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "reconciliation-failed",
					Namespace: "flux-system",
				},
				Spec: helmv2.HelmReleaseSpec{
					Interval: metav1.Duration{Duration: 5 * time.Minute},
				},
				Status: helmv2.HelmReleaseStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "ReconciliationFailed",
							Message:            "reconciliation failed",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"ReconciliationFailed": checker.SeverityCritical},
		},
		{
			name: "not ready - warning severity reason",
			hr: &helmv2.HelmRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-reason",
					Namespace: "flux-system",
				},
				Spec: helmv2.HelmReleaseSpec{
					Interval: metav1.Duration{Duration: 5 * time.Minute},
				},
				Status: helmv2.HelmReleaseStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "DependencyNotReady",
							Message:            "dependency not ready",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"DependencyNotReady": checker.SeverityWarning},
		},
		{
			name: "stale reconciliation",
			hr: &helmv2.HelmRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "stale-release",
					Namespace: "flux-system",
				},
				Spec: helmv2.HelmReleaseSpec{
					Interval: metav1.Duration{Duration: 5 * time.Minute},
				},
				Status: helmv2.HelmReleaseStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							Reason:             "Succeeded",
						},
					},
					ReconcileRequestStatus: fluxmeta.ReconcileRequestStatus{
						LastHandledReconcileAt: time.Now().Add(-3 * time.Hour).Format(time.RFC3339Nano),
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"StaleReconciliation": checker.SeverityWarning},
		},
		{
			name: "ready with old transition but no reconcile timestamp",
			hr: &helmv2.HelmRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "old-transition-no-reconcile-token",
					Namespace: "flux-system",
				},
				Spec: helmv2.HelmReleaseSpec{
					Interval: metav1.Duration{Duration: 5 * time.Minute},
				},
				Status: helmv2.HelmReleaseStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.NewTime(time.Now().Add(-3 * time.Hour)),
							Reason:             "Succeeded",
						},
					},
				},
			},
			wantHealthy: 1,
			wantTypes:   map[string]string{},
		},
		{
			name: "failed release in history",
			hr: &helmv2.HelmRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "failed-history",
					Namespace: "flux-system",
				},
				Spec: helmv2.HelmReleaseSpec{
					Interval: metav1.Duration{Duration: 5 * time.Minute},
				},
				Status: helmv2.HelmReleaseStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							Reason:             "Succeeded",
						},
					},
					History: helmv2.Snapshots{
						{
							Name:         "failed-history",
							Namespace:    "flux-system",
							Version:      2,
							Status:       "failed",
							ChartName:    "my-chart",
							ChartVersion: "1.0.0",
							Digest:       "sha256:abc",
							ConfigDigest: "sha256:def",
						},
					},
				},
			},
			wantHealthy: 0,
			wantTypes: map[string]string{
				"ReleaseFailed": checker.SeverityCritical,
			},
		},
		{
			name: "deployed release in history is not an issue",
			hr: &helmv2.HelmRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "deployed-history",
					Namespace: "flux-system",
				},
				Spec: helmv2.HelmReleaseSpec{
					Interval: metav1.Duration{Duration: 5 * time.Minute},
				},
				Status: helmv2.HelmReleaseStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							Reason:             "Succeeded",
						},
					},
					History: helmv2.Snapshots{
						{
							Name:         "deployed-history",
							Namespace:    "flux-system",
							Version:      1,
							Status:       "deployed",
							ChartName:    "my-chart",
							ChartVersion: "1.0.0",
							Digest:       "sha256:abc",
							ConfigDigest: "sha256:def",
						},
					},
				},
			},
			wantHealthy: 1,
			wantTypes:   map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewHelmReleaseChecker()
			c.staleThreshold = time.Hour
			checkCtx := testutil.NewCheckContext(t, []string{tt.hr.Namespace}, tt.hr)

			result, err := c.Check(context.Background(), checkCtx)
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}

			if result.Healthy != tt.wantHealthy {
				t.Errorf("Check() healthy = %d, want %d", result.Healthy, tt.wantHealthy)
			}

			for wantType, wantSeverity := range tt.wantTypes {
				found := false
				for _, issue := range result.Issues {
					if issue.Type == wantType {
						found = true
						if issue.Severity != wantSeverity {
							t.Errorf("Issue type %s: severity = %s, want %s", wantType, issue.Severity, wantSeverity)
						}
						break
					}
				}
				if !found {
					t.Errorf("Check() did not find expected issue type %s (got %d issues: %v)", wantType, len(result.Issues), issueTypes(result.Issues))
				}
			}
		})
	}
}

func TestHelmReleaseChecker_EmptyList(t *testing.T) {
	c := NewHelmReleaseChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"})

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if result.Healthy != 0 {
		t.Errorf("Check() healthy = %d, want 0", result.Healthy)
	}
	if len(result.Issues) != 0 {
		t.Errorf("Check() issues = %d, want 0", len(result.Issues))
	}
}

func TestGetSuggestionForHelmReason(t *testing.T) {
	tests := []struct {
		name     string
		reason   string
		contains string
	}{
		{"UpgradeFailed", "UpgradeFailed", "Helm upgrade failed"},
		{"InstallFailed", "InstallFailed", "Helm install failed"},
		{"UninstallFailed", "UninstallFailed", "Helm uninstall blocked"},
		{"ArtifactFailed", "ArtifactFailed", "Chart artifact unavailable"},
		{"ChartPullFailed", "ChartPullFailed", "Chart pull failed"},
		{"ReconciliationFailed", "ReconciliationFailed", "Reconciliation failed"},
		{"DependencyNotReady", "DependencyNotReady", "dependency is not ready"},
		{"ValuesValidationFailed", "ValuesValidationFailed", "Values do not match"},
		{"unknown", "SomethingUnknown", "Check HelmRelease status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getSuggestionForHelmReason(tt.reason)
			if got == "" {
				t.Error("getSuggestionForHelmReason() returned empty string")
			}
			assertContains(t, got, tt.contains)
		})
	}
}

// ---------------------------------------------------------------------------
// KustomizationChecker tests
// ---------------------------------------------------------------------------

func TestKustomizationChecker_Name(t *testing.T) {
	c := NewKustomizationChecker()
	if c.Name() != KustomizationCheckerName {
		t.Errorf("Name() = %v, want %v", c.Name(), KustomizationCheckerName)
	}
}

func TestKustomizationChecker_Check(t *testing.T) {
	tests := []struct {
		name        string
		ks          *kustomizev1.Kustomization
		wantHealthy int
		wantTypes   map[string]string
	}{
		{
			name: "healthy kustomization",
			ks: &kustomizev1.Kustomization{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "healthy-ks",
					Namespace: "flux-system",
				},
				Spec: kustomizev1.KustomizationSpec{
					Interval: metav1.Duration{Duration: 10 * time.Minute},
					SourceRef: kustomizev1.CrossNamespaceSourceReference{
						Kind: "GitRepository",
						Name: "my-repo",
					},
				},
				Status: kustomizev1.KustomizationStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							Reason:             "Succeeded",
						},
					},
					LastAppliedRevision:   "main@sha1:abc",
					LastAttemptedRevision: "main@sha1:abc",
				},
			},
			wantHealthy: 1,
			wantTypes:   map[string]string{},
		},
		{
			name: "suspended kustomization",
			ks: &kustomizev1.Kustomization{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "suspended-ks",
					Namespace: "flux-system",
				},
				Spec: kustomizev1.KustomizationSpec{
					Suspend:  true,
					Interval: metav1.Duration{Duration: 10 * time.Minute},
					SourceRef: kustomizev1.CrossNamespaceSourceReference{
						Kind: "GitRepository",
						Name: "my-repo",
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"Suspended": checker.SeverityWarning},
		},
		{
			name: "not ready - BuildFailed",
			ks: &kustomizev1.Kustomization{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "build-failed-ks",
					Namespace: "flux-system",
				},
				Spec: kustomizev1.KustomizationSpec{
					Interval: metav1.Duration{Duration: 10 * time.Minute},
					SourceRef: kustomizev1.CrossNamespaceSourceReference{
						Kind: "GitRepository",
						Name: "my-repo",
					},
				},
				Status: kustomizev1.KustomizationStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "BuildFailed",
							Message:            "build failed",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"BuildFailed": checker.SeverityCritical},
		},
		{
			name: "not ready - ValidationFailed",
			ks: &kustomizev1.Kustomization{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "validation-failed-ks",
					Namespace: "flux-system",
				},
				Spec: kustomizev1.KustomizationSpec{
					Interval: metav1.Duration{Duration: 10 * time.Minute},
					SourceRef: kustomizev1.CrossNamespaceSourceReference{
						Kind: "GitRepository",
						Name: "my-repo",
					},
				},
				Status: kustomizev1.KustomizationStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "ValidationFailed",
							Message:            "validation failed",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"ValidationFailed": checker.SeverityCritical},
		},
		{
			name: "not ready - HealthCheckFailed",
			ks: &kustomizev1.Kustomization{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "healthcheck-failed-ks",
					Namespace: "flux-system",
				},
				Spec: kustomizev1.KustomizationSpec{
					Interval: metav1.Duration{Duration: 10 * time.Minute},
					SourceRef: kustomizev1.CrossNamespaceSourceReference{
						Kind: "GitRepository",
						Name: "my-repo",
					},
				},
				Status: kustomizev1.KustomizationStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "HealthCheckFailed",
							Message:            "health check failed",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"HealthCheckFailed": checker.SeverityCritical},
		},
		{
			name: "not ready - ArtifactFailed",
			ks: &kustomizev1.Kustomization{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "artifact-failed-ks",
					Namespace: "flux-system",
				},
				Spec: kustomizev1.KustomizationSpec{
					Interval: metav1.Duration{Duration: 10 * time.Minute},
					SourceRef: kustomizev1.CrossNamespaceSourceReference{
						Kind: "GitRepository",
						Name: "my-repo",
					},
				},
				Status: kustomizev1.KustomizationStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "ArtifactFailed",
							Message:            "artifact failed",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"ArtifactFailed": checker.SeverityCritical},
		},
		{
			name: "not ready - ReconciliationFailed",
			ks: &kustomizev1.Kustomization{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "reconciliation-failed-ks",
					Namespace: "flux-system",
				},
				Spec: kustomizev1.KustomizationSpec{
					Interval: metav1.Duration{Duration: 10 * time.Minute},
					SourceRef: kustomizev1.CrossNamespaceSourceReference{
						Kind: "GitRepository",
						Name: "my-repo",
					},
				},
				Status: kustomizev1.KustomizationStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "ReconciliationFailed",
							Message:            "reconciliation failed",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"ReconciliationFailed": checker.SeverityCritical},
		},
		{
			name: "not ready - DependencyNotReady (warning severity)",
			ks: &kustomizev1.Kustomization{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dep-not-ready-ks",
					Namespace: "flux-system",
				},
				Spec: kustomizev1.KustomizationSpec{
					Interval: metav1.Duration{Duration: 10 * time.Minute},
					SourceRef: kustomizev1.CrossNamespaceSourceReference{
						Kind: "GitRepository",
						Name: "my-repo",
					},
				},
				Status: kustomizev1.KustomizationStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "DependencyNotReady",
							Message:            "dependency not ready",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"DependencyNotReady": checker.SeverityWarning},
		},
		{
			name: "stale reconciliation",
			ks: &kustomizev1.Kustomization{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "stale-ks",
					Namespace: "flux-system",
				},
				Spec: kustomizev1.KustomizationSpec{
					Interval: metav1.Duration{Duration: 10 * time.Minute},
					SourceRef: kustomizev1.CrossNamespaceSourceReference{
						Kind: "GitRepository",
						Name: "my-repo",
					},
				},
				Status: kustomizev1.KustomizationStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							Reason:             "Succeeded",
						},
					},
					ReconcileRequestStatus: fluxmeta.ReconcileRequestStatus{
						LastHandledReconcileAt: time.Now().Add(-3 * time.Hour).Format(time.RFC3339Nano),
					},
					LastAttemptedRevision: "main@sha1:abc",
					LastAppliedRevision:   "main@sha1:abc",
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"StaleReconciliation": checker.SeverityWarning},
		},
		{
			name: "ready with old transition but no reconcile timestamp",
			ks: &kustomizev1.Kustomization{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "old-transition-no-reconcile-token",
					Namespace: "flux-system",
				},
				Spec: kustomizev1.KustomizationSpec{
					Interval: metav1.Duration{Duration: 10 * time.Minute},
					SourceRef: kustomizev1.CrossNamespaceSourceReference{
						Kind: "GitRepository",
						Name: "my-repo",
					},
				},
				Status: kustomizev1.KustomizationStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.NewTime(time.Now().Add(-3 * time.Hour)),
							Reason:             "Succeeded",
						},
					},
					LastAttemptedRevision: "main@sha1:abc",
					LastAppliedRevision:   "main@sha1:abc",
				},
			},
			wantHealthy: 1,
			wantTypes:   map[string]string{},
		},
		{
			name: "pending changes - different revisions",
			ks: &kustomizev1.Kustomization{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pending-ks",
					Namespace: "flux-system",
				},
				Spec: kustomizev1.KustomizationSpec{
					Interval: metav1.Duration{Duration: 10 * time.Minute},
					SourceRef: kustomizev1.CrossNamespaceSourceReference{
						Kind: "GitRepository",
						Name: "my-repo",
					},
				},
				Status: kustomizev1.KustomizationStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							Reason:             "Succeeded",
						},
					},
					LastAppliedRevision:   "main@sha1:old",
					LastAttemptedRevision: "main@sha1:new",
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"PendingChanges": checker.SeverityInfo},
		},
		{
			name: "unhealthy resources - Healthy condition False",
			ks: &kustomizev1.Kustomization{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unhealthy-ks",
					Namespace: "flux-system",
				},
				Spec: kustomizev1.KustomizationSpec{
					Interval: metav1.Duration{Duration: 10 * time.Minute},
					SourceRef: kustomizev1.CrossNamespaceSourceReference{
						Kind: "GitRepository",
						Name: "my-repo",
					},
				},
				Status: kustomizev1.KustomizationStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							Reason:             "Succeeded",
						},
						{
							Type:               "Healthy",
							Status:             metav1.ConditionFalse,
							Reason:             "HealthCheckFailed",
							Message:            "deployment/my-app not ready",
							LastTransitionTime: metav1.Now(),
						},
					},
					LastAppliedRevision:   "main@sha1:abc",
					LastAttemptedRevision: "main@sha1:abc",
				},
			},
			wantHealthy: 0,
			wantTypes:   map[string]string{"UnhealthyResources": checker.SeverityWarning},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewKustomizationChecker()
			c.staleThreshold = time.Hour
			checkCtx := testutil.NewCheckContext(t, []string{tt.ks.Namespace}, tt.ks)

			result, err := c.Check(context.Background(), checkCtx)
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}

			if result.Healthy != tt.wantHealthy {
				t.Errorf("Check() healthy = %d, want %d", result.Healthy, tt.wantHealthy)
			}

			for wantType, wantSeverity := range tt.wantTypes {
				found := false
				for _, issue := range result.Issues {
					if issue.Type == wantType {
						found = true
						if issue.Severity != wantSeverity {
							t.Errorf("Issue type %s: severity = %s, want %s", wantType, issue.Severity, wantSeverity)
						}
						break
					}
				}
				if !found {
					t.Errorf("Check() did not find expected issue type %s (got %d issues: %v)", wantType, len(result.Issues), issueTypes(result.Issues))
				}
			}
		})
	}
}

func TestKustomizationChecker_EmptyList(t *testing.T) {
	c := NewKustomizationChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"})

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if result.Healthy != 0 {
		t.Errorf("Check() healthy = %d, want 0", result.Healthy)
	}
	if len(result.Issues) != 0 {
		t.Errorf("Check() issues = %d, want 0", len(result.Issues))
	}
}

func TestGetSuggestionForKustomizationReason(t *testing.T) {
	tests := []struct {
		name     string
		reason   string
		contains string
	}{
		{"BuildFailed", "BuildFailed", "Kustomize build failed"},
		{"ValidationFailed", "ValidationFailed", "Manifest validation failed"},
		{"HealthCheckFailed", "HealthCheckFailed", "Deployed resources failed health checks"},
		{"ArtifactFailed", "ArtifactFailed", "Source artifact is unavailable"},
		{"ReconciliationFailed", "ReconciliationFailed", "Reconciliation failed"},
		{"DependencyNotReady", "DependencyNotReady", "dependency is not ready"},
		{"PruneFailed", "PruneFailed", "Resource pruning failed"},
		{"unknown", "SomethingUnknown", "Check Kustomization status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getSuggestionForKustomizationReason(tt.reason)
			if got == "" {
				t.Error("getSuggestionForKustomizationReason() returned empty string")
			}
			assertContains(t, got, tt.contains)
		})
	}
}

// ---------------------------------------------------------------------------
// Shared helper tests
// ---------------------------------------------------------------------------

func TestFindCondition(t *testing.T) {
	conditions := []metav1.Condition{
		{
			Type:   "Ready",
			Status: metav1.ConditionTrue,
			Reason: "Succeeded",
		},
		{
			Type:   "Healthy",
			Status: metav1.ConditionFalse,
			Reason: "HealthCheckFailed",
		},
	}

	tests := []struct {
		name     string
		condType string
		wantNil  bool
		wantType string
	}{
		{"find Ready", "Ready", false, "Ready"},
		{"find Healthy", "Healthy", false, "Healthy"},
		{"missing condition", "Missing", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findCondition(conditions, tt.condType)
			if tt.wantNil {
				if got != nil {
					t.Errorf("findCondition() = %v, want nil", got)
				}
			} else {
				if got == nil {
					t.Fatal("findCondition() = nil, want non-nil")
				}
				if got.Type != tt.wantType {
					t.Errorf("findCondition().Type = %s, want %s", got.Type, tt.wantType)
				}
			}
		})
	}
}

func TestTruncateMessage(t *testing.T) {
	tests := []struct {
		name   string
		msg    string
		maxLen int
		want   string
	}{
		{
			name:   "short message",
			msg:    "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exactly at limit",
			msg:    "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "long message truncated",
			msg:    "this is a very long message that should be truncated",
			maxLen: 20,
			want:   "this is a very lo...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateMessage(tt.msg, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateMessage(%q, %d) = %q, want %q", tt.msg, tt.maxLen, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func issueTypes(issues []checker.Issue) []string {
	types := make([]string, len(issues))
	for i, issue := range issues {
		types[i] = issue.Type
	}
	return types
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return
		}
	}
	t.Errorf("string %q does not contain %q", s, substr)
}
