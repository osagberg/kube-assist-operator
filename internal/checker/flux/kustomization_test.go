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

	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/osagberg/kube-assist-operator/internal/checker"
)

// Test issue types
const (
	testIssueTypeSuspended           = "Suspended"
	testIssueTypeStaleReconciliation = "StaleReconciliation"
)

func TestKustomizationChecker_Name(t *testing.T) {
	c := NewKustomizationChecker()
	if c.Name() != KustomizationCheckerName {
		t.Errorf("Name() = %v, want %v", c.Name(), KustomizationCheckerName)
	}
}

func TestKustomizationChecker_CheckHealthyKustomization(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kustomizev1.AddToScheme(scheme)

	ks := &kustomizev1.Kustomization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "healthy-ks",
			Namespace: "flux-system",
		},
		Spec: kustomizev1.KustomizationSpec{
			Suspend: false,
		},
		Status: kustomizev1.KustomizationStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					Reason:             "ReconciliationSucceeded",
					Message:            "Applied successfully",
					LastTransitionTime: metav1.Now(),
				},
			},
			LastAttemptedRevision: "main@sha1:abc123",
			LastAppliedRevision:   "main@sha1:abc123",
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ks).
		Build()

	c := NewKustomizationChecker()
	checkCtx := &checker.CheckContext{
		Client:     client,
		Namespaces: []string{"flux-system"},
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

func TestKustomizationChecker_ChecktestIssueTypeSuspendedKustomization(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kustomizev1.AddToScheme(scheme)

	ks := &kustomizev1.Kustomization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "suspended-ks",
			Namespace: "flux-system",
		},
		Spec: kustomizev1.KustomizationSpec{
			Suspend: true,
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ks).
		Build()

	c := NewKustomizationChecker()
	checkCtx := &checker.CheckContext{
		Client:     client,
		Namespaces: []string{"flux-system"},
	}

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == testIssueTypeSuspended && issue.Severity == checker.SeverityWarning {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Check() did not find expected testIssueTypeSuspended issue")
	}
}

func TestKustomizationChecker_CheckBuildFailed(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kustomizev1.AddToScheme(scheme)

	ks := &kustomizev1.Kustomization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "failed-ks",
			Namespace: "flux-system",
		},
		Spec: kustomizev1.KustomizationSpec{
			Suspend: false,
		},
		Status: kustomizev1.KustomizationStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionFalse,
					Reason:             "BuildFailed",
					Message:            "kustomization.yaml not found",
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ks).
		Build()

	c := NewKustomizationChecker()
	checkCtx := &checker.CheckContext{
		Client:     client,
		Namespaces: []string{"flux-system"},
	}

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "BuildFailed" && issue.Severity == checker.SeverityCritical {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Check() did not find expected BuildFailed critical issue")
	}
}

func TestKustomizationChecker_CheckHealthCheckFailed(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kustomizev1.AddToScheme(scheme)

	ks := &kustomizev1.Kustomization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unhealthy-ks",
			Namespace: "flux-system",
		},
		Spec: kustomizev1.KustomizationSpec{
			Suspend: false,
		},
		Status: kustomizev1.KustomizationStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionFalse,
					Reason:             "HealthCheckFailed",
					Message:            "Deployment not ready",
					LastTransitionTime: metav1.Now(),
				},
				{
					Type:               "Healthy",
					Status:             metav1.ConditionFalse,
					Reason:             "HealthCheckFailed",
					Message:            "Deployment my-app not ready",
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ks).
		Build()

	c := NewKustomizationChecker()
	checkCtx := &checker.CheckContext{
		Client:     client,
		Namespaces: []string{"flux-system"},
	}

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	// Should have both HealthCheckFailed and UnhealthyResources issues
	foundHealthCheckFailed := false
	foundUnhealthy := false
	for _, issue := range result.Issues {
		if issue.Type == "HealthCheckFailed" && issue.Severity == checker.SeverityCritical {
			foundHealthCheckFailed = true
		}
		if issue.Type == "UnhealthyResources" && issue.Severity == checker.SeverityWarning {
			foundUnhealthy = true
		}
	}
	if !foundHealthCheckFailed {
		t.Errorf("Check() did not find expected HealthCheckFailed issue")
	}
	if !foundUnhealthy {
		t.Errorf("Check() did not find expected UnhealthyResources issue")
	}
}

func TestKustomizationChecker_CheckPendingChanges(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kustomizev1.AddToScheme(scheme)

	ks := &kustomizev1.Kustomization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pending-ks",
			Namespace: "flux-system",
		},
		Spec: kustomizev1.KustomizationSpec{
			Suspend: false,
		},
		Status: kustomizev1.KustomizationStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					Reason:             "ReconciliationSucceeded",
					LastTransitionTime: metav1.Now(),
				},
			},
			LastAttemptedRevision: "main@sha1:def456",
			LastAppliedRevision:   "main@sha1:abc123",
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ks).
		Build()

	c := NewKustomizationChecker()
	checkCtx := &checker.CheckContext{
		Client:     client,
		Namespaces: []string{"flux-system"},
	}

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "PendingChanges" && issue.Severity == checker.SeverityInfo {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Check() did not find expected PendingChanges issue")
	}
}

func TestKustomizationChecker_ChecktestIssueTypeStaleReconciliation(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kustomizev1.AddToScheme(scheme)

	staleTime := metav1.NewTime(time.Now().Add(-2 * time.Hour))

	ks := &kustomizev1.Kustomization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stale-ks",
			Namespace: "flux-system",
		},
		Spec: kustomizev1.KustomizationSpec{
			Suspend: false,
		},
		Status: kustomizev1.KustomizationStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					Reason:             "ReconciliationSucceeded",
					LastTransitionTime: staleTime,
				},
			},
			LastAttemptedRevision: "main@sha1:abc123",
			LastAppliedRevision:   "main@sha1:abc123",
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ks).
		Build()

	c := NewKustomizationChecker()
	checkCtx := &checker.CheckContext{
		Client:     client,
		Namespaces: []string{"flux-system"},
	}

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == testIssueTypeStaleReconciliation && issue.Severity == checker.SeverityWarning {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Check() did not find expected testIssueTypeStaleReconciliation issue")
	}
}

func TestGetSuggestionForKustomizationReason(t *testing.T) {
	tests := []struct {
		reason   string
		wantPart string
	}{
		{"BuildFailed", "build output"},
		{"ValidationFailed", "Validate YAML"},
		{"HealthCheckFailed", "health of deployed"},
		{"ArtifactFailed", "source is accessible"},
		{"ReconciliationFailed", "status conditions"},
		{"DependencyNotReady", "dependent"},
		{"PruneFailed", "pruned"},
		{"UnknownReason", "status and events"},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			got := getSuggestionForKustomizationReason(tt.reason)
			if got == "" {
				t.Error("getSuggestionForKustomizationReason() returned empty string")
			}
			if len(got) < 10 {
				t.Errorf("getSuggestionForKustomizationReason() returned too short suggestion: %s", got)
			}
		})
	}
}
