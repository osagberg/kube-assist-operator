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

package workload

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/osagberg/kube-assist-operator/internal/checker"
)

// Test issue types
const (
	testIssueTypeContainerNotReady = "ContainerNotReady"
	testIssueTypeHighRestartCount  = "HighRestartCount"
)

func TestChecker_Name(t *testing.T) {
	c := New()
	if c.Name() != CheckerName {
		t.Errorf("Name() = %s, want %s", c.Name(), CheckerName)
	}
}

func TestChecker_Supports(t *testing.T) {
	c := New()
	// Workload checker always supports
	if !c.Supports(context.TODO(), nil) {
		t.Error("Supports() = false, want true")
	}
}

func TestGetSuggestionForWaitingReason(t *testing.T) {
	tests := []struct {
		reason   string
		contains string
	}{
		{"ImagePullBackOff", "image"},
		{"ErrImagePull", "image"},
		{"CrashLoopBackOff", "crashing"},
		{"CreateContainerConfigError", "ConfigMaps"},
		{"ContainerCreating", "being created"},
		{"UnknownReason", "events"},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			got := GetSuggestionForWaitingReason(tt.reason)
			if got == "" {
				t.Errorf("GetSuggestionForWaitingReason(%s) returned empty", tt.reason)
			}
		})
	}
}

func TestGetSuggestionForTerminatedReason(t *testing.T) {
	tests := []struct {
		reason   string
		exitCode int32
		contains string
	}{
		{"OOMKilled", 137, "memory"},
		{"", 137, "killed"},
		{"", 143, "SIGTERM"},
		{"", 1, "error"},
		{"", 126, "permissions"},
		{"", 127, "not found"},
		{"", 42, "42"},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			got := GetSuggestionForTerminatedReason(tt.reason, tt.exitCode)
			if got == "" {
				t.Errorf("GetSuggestionForTerminatedReason(%s, %d) returned empty", tt.reason, tt.exitCode)
			}
		})
	}
}

func TestDiagnosePods_HealthyPod(t *testing.T) {
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "healthy-pod"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "app",
						Ready: true,
					},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "app",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("256Mi"),
								corev1.ResourceCPU:    resource.MustParse("500m"),
							},
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("128Mi"),
								corev1.ResourceCPU:    resource.MustParse("100m"),
							},
						},
						LivenessProbe:  &corev1.Probe{},
						ReadinessProbe: &corev1.Probe{},
					},
				},
			},
		},
	}

	issues := DiagnosePods(pods, "default", "deployment/test", DefaultRestartThreshold)

	if len(issues) != 0 {
		t.Errorf("DiagnosePods() found %d issues for healthy pod, want 0", len(issues))
		for _, issue := range issues {
			t.Logf("Issue: %s - %s", issue.Type, issue.Message)
		}
	}
}

func TestDiagnosePods_CrashLoopBackOff(t *testing.T) {
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "crash-pod"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "app",
						Ready: false,
						State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{
								Reason:  "CrashLoopBackOff",
								Message: "back-off 5m0s restarting failed container",
							},
						},
						RestartCount: 10,
					},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "app"},
				},
			},
		},
	}

	issues := DiagnosePods(pods, "default", "deployment/test", DefaultRestartThreshold)

	// Should have testIssueTypeContainerNotReady and testIssueTypeHighRestartCount
	if len(issues) < 2 {
		t.Errorf("DiagnosePods() found %d issues, want at least 2", len(issues))
	}

	hasNotReady := false
	hasHighRestart := false
	for _, issue := range issues {
		if issue.Type == testIssueTypeContainerNotReady {
			hasNotReady = true
			if issue.Severity != checker.SeverityCritical {
				t.Errorf("testIssueTypeContainerNotReady severity = %s, want Critical", issue.Severity)
			}
		}
		if issue.Type == testIssueTypeHighRestartCount {
			hasHighRestart = true
			if issue.Severity != checker.SeverityWarning {
				t.Errorf("testIssueTypeHighRestartCount severity = %s, want Warning", issue.Severity)
			}
		}
	}

	if !hasNotReady {
		t.Error("Expected testIssueTypeContainerNotReady issue")
	}
	if !hasHighRestart {
		t.Error("Expected testIssueTypeHighRestartCount issue")
	}
}

func TestDiagnosePods_ImagePullBackOff(t *testing.T) {
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "image-pull-pod"},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "app",
						Ready: false,
						State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{
								Reason:  "ImagePullBackOff",
								Message: "Back-off pulling image",
							},
						},
					},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "app"},
				},
			},
		},
	}

	issues := DiagnosePods(pods, "default", "deployment/test", DefaultRestartThreshold)

	// Should have PodNotRunning and testIssueTypeContainerNotReady
	hasPodNotRunning := false
	hasContainerNotReady := false
	for _, issue := range issues {
		if issue.Type == "PodNotRunning" {
			hasPodNotRunning = true
		}
		if issue.Type == testIssueTypeContainerNotReady {
			hasContainerNotReady = true
			if issue.Metadata["reason"] != "ImagePullBackOff" {
				t.Errorf("Expected reason=ImagePullBackOff in metadata, got %s", issue.Metadata["reason"])
			}
		}
	}

	if !hasPodNotRunning {
		t.Error("Expected PodNotRunning issue")
	}
	if !hasContainerNotReady {
		t.Error("Expected testIssueTypeContainerNotReady issue")
	}
}

func TestDiagnosePods_OOMKilled(t *testing.T) {
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "oom-pod"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "app",
						Ready: false,
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								Reason:   "OOMKilled",
								ExitCode: 137,
							},
						},
					},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "app"},
				},
			},
		},
	}

	issues := DiagnosePods(pods, "default", "deployment/test", DefaultRestartThreshold)

	hasOOMIssue := false
	for _, issue := range issues {
		if issue.Type == testIssueTypeContainerNotReady && issue.Metadata["reason"] == "OOMKilled" {
			hasOOMIssue = true
			if issue.Suggestion == "" {
				t.Error("Expected suggestion for OOMKilled")
			}
		}
	}

	if !hasOOMIssue {
		t.Error("Expected testIssueTypeContainerNotReady issue with OOMKilled reason")
	}
}

func TestDiagnosePods_SchedulingFailed(t *testing.T) {
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "unscheduled-pod"},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				Conditions: []corev1.PodCondition{
					{
						Type:    corev1.PodScheduled,
						Status:  corev1.ConditionFalse,
						Reason:  "Unschedulable",
						Message: "0/3 nodes are available: 3 Insufficient memory.",
					},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "app"},
				},
			},
		},
	}

	issues := DiagnosePods(pods, "default", "deployment/test", DefaultRestartThreshold)

	hasSchedulingFailed := false
	for _, issue := range issues {
		if issue.Type == "SchedulingFailed" {
			hasSchedulingFailed = true
			if issue.Severity != checker.SeverityCritical {
				t.Errorf("SchedulingFailed severity = %s, want Critical", issue.Severity)
			}
		}
	}

	if !hasSchedulingFailed {
		t.Error("Expected SchedulingFailed issue")
	}
}

func TestDiagnosePods_MissingResources(t *testing.T) {
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "no-resources-pod"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "app", Ready: true},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:      "app",
						Resources: corev1.ResourceRequirements{}, // No resources
					},
				},
			},
		},
	}

	issues := DiagnosePods(pods, "default", "deployment/test", DefaultRestartThreshold)

	issueTypes := make(map[string]bool)
	for _, issue := range issues {
		issueTypes[issue.Type] = true
	}

	expected := []string{"NoMemoryLimit", "NoCPULimit", "NoResourceRequests", "NoLivenessProbe", "NoReadinessProbe"}
	for _, et := range expected {
		if !issueTypes[et] {
			t.Errorf("Expected issue type %s not found", et)
		}
	}
}

func TestDiagnosePods_CustomRestartThreshold(t *testing.T) {
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "restart-pod"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:         "app",
						Ready:        true,
						RestartCount: 5,
					},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "app"},
				},
			},
		},
	}

	// With default threshold (3), should have testIssueTypeHighRestartCount
	issues := DiagnosePods(pods, "default", "deployment/test", 3)
	hasHighRestart := false
	for _, issue := range issues {
		if issue.Type == testIssueTypeHighRestartCount {
			hasHighRestart = true
		}
	}
	if !hasHighRestart {
		t.Error("Expected testIssueTypeHighRestartCount with threshold 3")
	}

	// With higher threshold (10), should not have testIssueTypeHighRestartCount
	issues = DiagnosePods(pods, "default", "deployment/test", 10)
	hasHighRestart = false
	for _, issue := range issues {
		if issue.Type == testIssueTypeHighRestartCount {
			hasHighRestart = true
		}
	}
	if hasHighRestart {
		t.Error("Did not expect testIssueTypeHighRestartCount with threshold 10")
	}
}

func TestDiagnosePods_EmptyPodList(t *testing.T) {
	issues := DiagnosePods([]corev1.Pod{}, "default", "deployment/test", DefaultRestartThreshold)

	if len(issues) != 0 {
		t.Errorf("DiagnosePods() with empty list returned %d issues, want 0", len(issues))
	}
}

func TestDiagnosePods_Metadata(t *testing.T) {
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
			Status: corev1.PodStatus{
				Phase: corev1.PodFailed,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "app"},
				},
			},
		},
	}

	issues := DiagnosePods(pods, "myns", "deployment/myapp", DefaultRestartThreshold)

	for _, issue := range issues {
		if issue.Namespace != "myns" {
			t.Errorf("Issue namespace = %s, want myns", issue.Namespace)
		}
		if issue.Resource != "deployment/myapp" {
			t.Errorf("Issue resource = %s, want deployment/myapp", issue.Resource)
		}
		if issue.Type == "PodNotRunning" {
			if issue.Metadata["pod"] != "test-pod" {
				t.Errorf("Issue metadata[pod] = %s, want test-pod", issue.Metadata["pod"])
			}
		}
	}
}
