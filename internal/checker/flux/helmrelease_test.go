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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/osagberg/kube-assist-operator/internal/checker"
)

func TestHelmReleaseChecker_Name(t *testing.T) {
	c := NewHelmReleaseChecker()
	if c.Name() != HelmReleaseCheckerName {
		t.Errorf("Name() = %v, want %v", c.Name(), HelmReleaseCheckerName)
	}
}

func TestHelmReleaseChecker_CheckHealthyRelease(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = helmv2.AddToScheme(scheme)

	hr := &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "healthy-release",
			Namespace: "default",
		},
		Spec: helmv2.HelmReleaseSpec{
			Suspend: false,
		},
		Status: helmv2.HelmReleaseStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					Reason:             "ReconciliationSucceeded",
					Message:            "Release reconciliation succeeded",
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(hr).
		Build()

	c := NewHelmReleaseChecker()
	checkCtx := &checker.CheckContext{
		Client:     client,
		Namespaces: []string{"default"},
	}

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

func TestHelmReleaseChecker_CheckSuspendedRelease(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = helmv2.AddToScheme(scheme)

	hr := &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "suspended-release",
			Namespace: "default",
		},
		Spec: helmv2.HelmReleaseSpec{
			Suspend: true,
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(hr).
		Build()

	c := NewHelmReleaseChecker()
	checkCtx := &checker.CheckContext{
		Client:     client,
		Namespaces: []string{"default"},
	}

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if len(result.Issues) == 0 {
		t.Errorf("Check() should have found issues for suspended release")
		return
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

func TestHelmReleaseChecker_CheckFailedRelease(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = helmv2.AddToScheme(scheme)

	hr := &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "failed-release",
			Namespace: "default",
		},
		Spec: helmv2.HelmReleaseSpec{
			Suspend: false,
		},
		Status: helmv2.HelmReleaseStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionFalse,
					Reason:             "UpgradeFailed",
					Message:            "Helm upgrade failed: timeout waiting for condition",
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(hr).
		Build()

	c := NewHelmReleaseChecker()
	checkCtx := &checker.CheckContext{
		Client:     client,
		Namespaces: []string{"default"},
	}

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if len(result.Issues) == 0 {
		t.Errorf("Check() should have found issues for failed release")
		return
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "UpgradeFailed" && issue.Severity == checker.SeverityCritical {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Check() did not find expected UpgradeFailed critical issue")
	}
}

func TestHelmReleaseChecker_CheckStaleReconciliation(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = helmv2.AddToScheme(scheme)

	// Create a release with stale reconciliation (over 1 hour old)
	staleTime := metav1.NewTime(time.Now().Add(-2 * time.Hour))

	hr := &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stale-release",
			Namespace: "default",
		},
		Spec: helmv2.HelmReleaseSpec{
			Suspend: false,
		},
		Status: helmv2.HelmReleaseStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					Reason:             "ReconciliationSucceeded",
					Message:            "Release reconciliation succeeded",
					LastTransitionTime: staleTime,
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(hr).
		Build()

	c := NewHelmReleaseChecker()
	checkCtx := &checker.CheckContext{
		Client:     client,
		Namespaces: []string{"default"},
	}

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

func TestHelmReleaseChecker_MultipleNamespaces(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = helmv2.AddToScheme(scheme)

	hr1 := &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "release1",
			Namespace: "ns1",
		},
		Spec: helmv2.HelmReleaseSpec{
			Suspend: false,
		},
		Status: helmv2.HelmReleaseStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}

	hr2 := &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "release2",
			Namespace: "ns2",
		},
		Spec: helmv2.HelmReleaseSpec{
			Suspend: true, // This one is suspended
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(hr1, hr2).
		Build()

	c := NewHelmReleaseChecker()
	checkCtx := &checker.CheckContext{
		Client:     client,
		Namespaces: []string{"ns1", "ns2"},
	}

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if result.Healthy != 1 {
		t.Errorf("Check() healthy = %d, want 1", result.Healthy)
	}
	if len(result.Issues) != 1 {
		t.Errorf("Check() issues = %d, want 1", len(result.Issues))
	}
}

func TestGetSuggestionForHelmReason(t *testing.T) {
	tests := []struct {
		reason   string
		wantPart string
	}{
		{"UpgradeFailed", "Helm controller logs"},
		{"InstallFailed", "chart exists"},
		{"UninstallFailed", "finalizers"},
		{"ArtifactFailed", "source is accessible"},
		{"ChartPullFailed", "authentication"},
		{"ReconciliationFailed", "status conditions"},
		{"DependencyNotReady", "dependent HelmReleases"},
		{"ValuesValidationFailed", "chart schema"},
		{"UnknownReason", "status and events"},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			got := getSuggestionForHelmReason(tt.reason)
			if got == "" {
				t.Error("getSuggestionForHelmReason() returned empty string")
			}
			// Just verify we get a non-empty suggestion
			if len(got) < 10 {
				t.Errorf("getSuggestionForHelmReason() returned too short suggestion: %s", got)
			}
		})
	}
}

func TestFindCondition(t *testing.T) {
	conditions := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue},
		{Type: "Reconciling", Status: metav1.ConditionFalse},
	}

	// Test found
	cond := findCondition(conditions, "Ready")
	if cond == nil {
		t.Error("findCondition() returned nil for existing condition")
	}
	if cond.Type != "Ready" {
		t.Errorf("findCondition() returned wrong condition type: %s", cond.Type)
	}

	// Test not found
	cond = findCondition(conditions, "NonExistent")
	if cond != nil {
		t.Error("findCondition() should return nil for non-existent condition")
	}
}

func TestTruncateMessage(t *testing.T) {
	tests := []struct {
		msg    string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a long message that should be truncated", 20, "this is a long me..."},
		{"", 10, ""},
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			got := truncateMessage(tt.msg, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateMessage(%q, %d) = %q, want %q", tt.msg, tt.maxLen, got, tt.want)
			}
		})
	}
}
