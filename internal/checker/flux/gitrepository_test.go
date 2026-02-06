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

	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/testutil"
)

func TestGitRepositoryChecker_Name(t *testing.T) {
	c := NewGitRepositoryChecker()
	if c.Name() != GitRepositoryCheckerName {
		t.Errorf("Name() = %v, want %v", c.Name(), GitRepositoryCheckerName)
	}
}

func TestGitRepositoryChecker_CheckHealthyRepository(t *testing.T) {
	gr := &sourcev1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "healthy-repo",
			Namespace: "flux-system",
		},
		Spec: sourcev1.GitRepositorySpec{
			URL:     "https://github.com/example/repo",
			Suspend: false,
		},
		Status: sourcev1.GitRepositoryStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					Reason:             "Succeeded",
					Message:            "stored artifact",
					LastTransitionTime: metav1.Now(),
				},
			},
			Artifact: &fluxmeta.Artifact{
				Revision: "main@sha1:abc123",
			},
		},
	}

	c := NewGitRepositoryChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"flux-system"}, gr)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if result.Healthy != 1 {
		t.Errorf("Check() healthy = %d, want 1", result.Healthy)
	}
	if len(result.Issues) != 0 {
		t.Errorf("Check() issues = %d, want 0", len(result.Issues))
	}
}

func TestGitRepositoryChecker_CheckSuspendedRepository(t *testing.T) {
	gr := &sourcev1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "suspended-repo",
			Namespace: "flux-system",
		},
		Spec: sourcev1.GitRepositorySpec{
			URL:     "https://github.com/example/repo",
			Suspend: true,
		},
	}

	c := NewGitRepositoryChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"flux-system"}, gr)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "Suspended" && issue.Severity == checker.SeverityWarning {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Check() did not find expected Suspended issue")
	}
}

func TestGitRepositoryChecker_CheckAuthenticationFailed(t *testing.T) {
	gr := &sourcev1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "auth-failed-repo",
			Namespace: "flux-system",
		},
		Spec: sourcev1.GitRepositorySpec{
			URL:     "https://github.com/example/private-repo",
			Suspend: false,
		},
		Status: sourcev1.GitRepositoryStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionFalse,
					Reason:             "AuthenticationFailed",
					Message:            "authentication required",
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}

	c := NewGitRepositoryChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"flux-system"}, gr)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "AuthenticationFailed" && issue.Severity == checker.SeverityCritical {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Check() did not find expected AuthenticationFailed critical issue")
	}
}

func TestGitRepositoryChecker_CheckCloneFailed(t *testing.T) {
	gr := &sourcev1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clone-failed-repo",
			Namespace: "flux-system",
		},
		Spec: sourcev1.GitRepositorySpec{
			URL:     "https://github.com/example/nonexistent",
			Suspend: false,
		},
		Status: sourcev1.GitRepositoryStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionFalse,
					Reason:             "CloneFailed",
					Message:            "repository not found",
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}

	c := NewGitRepositoryChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"flux-system"}, gr)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "CloneFailed" && issue.Severity == checker.SeverityCritical {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Check() did not find expected CloneFailed critical issue")
	}
}

func TestGitRepositoryChecker_CheckStaleReconciliation(t *testing.T) {
	staleTime := metav1.NewTime(time.Now().Add(-2 * time.Hour))

	gr := &sourcev1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stale-repo",
			Namespace: "flux-system",
		},
		Spec: sourcev1.GitRepositorySpec{
			URL:     "https://github.com/example/repo",
			Suspend: false,
		},
		Status: sourcev1.GitRepositoryStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					Reason:             "Succeeded",
					LastTransitionTime: staleTime,
				},
			},
			Artifact: &fluxmeta.Artifact{
				Revision: "main@sha1:abc123",
			},
		},
	}

	c := NewGitRepositoryChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"flux-system"}, gr)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "StaleReconciliation" && issue.Severity == checker.SeverityWarning {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Check() did not find expected StaleReconciliation issue")
	}
}

func TestGitRepositoryChecker_CheckNoAuthForPrivateRepo(t *testing.T) {
	gr := &sourcev1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "private-no-auth",
			Namespace: "flux-system",
		},
		Spec: sourcev1.GitRepositorySpec{
			URL:       "git@github.com:example/private-repo.git",
			Suspend:   false,
			SecretRef: nil,
		},
		Status: sourcev1.GitRepositoryStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					Reason:             "Succeeded",
					LastTransitionTime: metav1.Now(),
				},
			},
			Artifact: &fluxmeta.Artifact{
				Revision: "main@sha1:abc123",
			},
		},
	}

	c := NewGitRepositoryChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"flux-system"}, gr)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "NoAuthentication" && issue.Severity == checker.SeverityWarning {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Check() did not find expected NoAuthentication issue")
	}
}

func TestIsPrivateRepo(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"git@github.com:user/repo.git", true},
		{"ssh://git@github.com/user/repo.git", true},
		{"https://github.com/user/repo.git", false},
		{"http://gitlab.com/user/repo.git", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := isPrivateRepo(tt.url)
			if got != tt.want {
				t.Errorf("isPrivateRepo(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestGetSuggestionForGitRepoReason(t *testing.T) {
	tests := []struct {
		reason string
	}{
		{"GitOperationFailed"},
		{"AuthenticationFailed"},
		{"CheckoutFailed"},
		{"CloneFailed"},
		{"StorageOperationFailed"},
		{"ReconciliationFailed"},
		{"UnknownReason"},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			got := getSuggestionForGitRepoReason(tt.reason)
			if got == "" {
				t.Error("getSuggestionForGitRepoReason() returned empty string")
			}
			if len(got) < 10 {
				t.Errorf("getSuggestionForGitRepoReason() returned too short suggestion: %s", got)
			}
		})
	}
}
