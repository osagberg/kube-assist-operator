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
	"fmt"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/testutil"
)

// Test issue types and common values
const (
	testIssueTypeContainerNotReady = "ContainerNotReady"
	testIssueTypeHighRestartCount  = "HighRestartCount"
	testIssueTypePodNotRunning     = "PodNotRunning"
	testReasonOOMKilled            = "OOMKilled"
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
		{testReasonOOMKilled, 137, "memory"},
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
		if issue.Type == testIssueTypePodNotRunning {
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
								Reason:   testReasonOOMKilled,
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
		if issue.Type == testIssueTypeContainerNotReady && issue.Metadata["reason"] == testReasonOOMKilled {
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
		if issue.Type == testIssueTypePodNotRunning {
			if issue.Metadata["pod"] != "test-pod" {
				t.Errorf("Issue metadata[pod] = %s, want test-pod", issue.Metadata["pod"])
			}
		}
	}
}

// ---------------------------------------------------------------------------
// P1: diagnosePod — pod phase tests
// ---------------------------------------------------------------------------

func TestDiagnosePod_PodPhases(t *testing.T) {
	tests := []struct {
		name          string
		phase         corev1.PodPhase
		wantIssueType string
		wantIssue     bool
	}{
		{
			name:          "Pending pod reports PodNotRunning",
			phase:         corev1.PodPending,
			wantIssueType: testIssueTypePodNotRunning,
			wantIssue:     true,
		},
		{
			name:          "Failed pod reports PodNotRunning",
			phase:         corev1.PodFailed,
			wantIssueType: testIssueTypePodNotRunning,
			wantIssue:     true,
		},
		{
			name:          "Unknown pod reports PodNotRunning",
			phase:         corev1.PodUnknown,
			wantIssueType: testIssueTypePodNotRunning,
			wantIssue:     true,
		},
		{
			name:      "Succeeded pod reports no PodNotRunning issue",
			phase:     corev1.PodSucceeded,
			wantIssue: false,
		},
		{
			name:      "Running pod reports no PodNotRunning issue",
			phase:     corev1.PodRunning,
			wantIssue: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pods := []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "phase-pod"},
					Status: corev1.PodStatus{
						Phase: tt.phase,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:           "app",
								LivenessProbe:  &corev1.Probe{},
								ReadinessProbe: &corev1.Probe{},
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("128Mi"),
										corev1.ResourceCPU:    resource.MustParse("100m"),
									},
									Requests: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("64Mi"),
										corev1.ResourceCPU:    resource.MustParse("50m"),
									},
								},
							},
						},
					},
				},
			}

			issues := DiagnosePods(pods, "default", "deployment/test", DefaultRestartThreshold)

			hasPodNotRunning := false
			for _, issue := range issues {
				if issue.Type == testIssueTypePodNotRunning {
					hasPodNotRunning = true
					if issue.Severity != checker.SeverityCritical {
						t.Errorf("PodNotRunning severity = %s, want Critical", issue.Severity)
					}
					if issue.Metadata["phase"] != string(tt.phase) {
						t.Errorf("PodNotRunning metadata[phase] = %s, want %s", issue.Metadata["phase"], tt.phase)
					}
				}
			}

			if hasPodNotRunning != tt.wantIssue {
				t.Errorf("PodNotRunning present = %v, want %v", hasPodNotRunning, tt.wantIssue)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// P1: diagnosePod — container waiting state tests
// ---------------------------------------------------------------------------

func TestDiagnosePod_ContainerWaitingStates(t *testing.T) {
	tests := []struct {
		name            string
		reason          string
		message         string
		wantSuggestion  string // substring to check in suggestion
		wantMsgContains string // substring to check in issue message
	}{
		{
			name:            "CreateContainerConfigError",
			reason:          "CreateContainerConfigError",
			message:         "secret not found",
			wantSuggestion:  "ConfigMap",
			wantMsgContains: "CreateContainerConfigError - secret not found",
		},
		{
			name:            "ContainerCreating",
			reason:          "ContainerCreating",
			message:         "",
			wantSuggestion:  "being created",
			wantMsgContains: "ContainerCreating",
		},
		{
			name:            "PodInitializing",
			reason:          "PodInitializing",
			message:         "",
			wantSuggestion:  "Init containers",
			wantMsgContains: "PodInitializing",
		},
		{
			name:            "ImagePullBackOff with message",
			reason:          "ImagePullBackOff",
			message:         "Back-off pulling image",
			wantSuggestion:  "image",
			wantMsgContains: "ImagePullBackOff - Back-off pulling image",
		},
		{
			name:            "CrashLoopBackOff with message",
			reason:          "CrashLoopBackOff",
			message:         "back-off 2m restarting",
			wantSuggestion:  "crashing",
			wantMsgContains: "CrashLoopBackOff - back-off 2m restarting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pods := []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "waiting-pod"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:  "app",
								Ready: false,
								State: corev1.ContainerState{
									Waiting: &corev1.ContainerStateWaiting{
										Reason:  tt.reason,
										Message: tt.message,
									},
								},
							},
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:           "app",
								LivenessProbe:  &corev1.Probe{},
								ReadinessProbe: &corev1.Probe{},
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("128Mi"),
										corev1.ResourceCPU:    resource.MustParse("100m"),
									},
									Requests: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("64Mi"),
										corev1.ResourceCPU:    resource.MustParse("50m"),
									},
								},
							},
						},
					},
				},
			}

			issues := DiagnosePods(pods, "default", "deployment/test", DefaultRestartThreshold)

			foundNotReady := false
			for _, issue := range issues {
				if issue.Type == testIssueTypeContainerNotReady {
					foundNotReady = true
					if issue.Metadata["reason"] != tt.reason {
						t.Errorf("metadata[reason] = %s, want %s", issue.Metadata["reason"], tt.reason)
					}
					if !strings.Contains(issue.Suggestion, tt.wantSuggestion) {
						t.Errorf("suggestion = %q, want substring %q", issue.Suggestion, tt.wantSuggestion)
					}
					if !strings.Contains(issue.Message, tt.wantMsgContains) {
						t.Errorf("message = %q, want substring %q", issue.Message, tt.wantMsgContains)
					}
				}
			}
			if !foundNotReady {
				t.Error("Expected ContainerNotReady issue")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// P1: diagnosePod — container terminated state tests
// ---------------------------------------------------------------------------

func TestDiagnosePod_ContainerTerminatedStates(t *testing.T) {
	tests := []struct {
		name     string
		reason   string
		exitCode int32
	}{
		{
			name:     testReasonOOMKilled,
			reason:   testReasonOOMKilled,
			exitCode: 137,
		},
		{
			name:     "exit code 1",
			reason:   "Error",
			exitCode: 1,
		},
		{
			name:     "exit code 126",
			reason:   "Error",
			exitCode: 126,
		},
		{
			name:     "exit code 127",
			reason:   "Error",
			exitCode: 127,
		},
		{
			name:     "exit code 143 SIGTERM",
			reason:   "Completed",
			exitCode: 143,
		},
		{
			name:     "generic exit code 42",
			reason:   "Error",
			exitCode: 42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pods := []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "terminated-pod"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:  "app",
								Ready: false,
								State: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{
										Reason:   tt.reason,
										ExitCode: tt.exitCode,
									},
								},
							},
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:           "app",
								LivenessProbe:  &corev1.Probe{},
								ReadinessProbe: &corev1.Probe{},
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("128Mi"),
										corev1.ResourceCPU:    resource.MustParse("100m"),
									},
									Requests: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("64Mi"),
										corev1.ResourceCPU:    resource.MustParse("50m"),
									},
								},
							},
						},
					},
				},
			}

			issues := DiagnosePods(pods, "default", "deployment/test", DefaultRestartThreshold)

			foundNotReady := false
			for _, issue := range issues {
				if issue.Type == testIssueTypeContainerNotReady {
					foundNotReady = true
					if issue.Metadata["reason"] != tt.reason {
						t.Errorf("metadata[reason] = %s, want %s", issue.Metadata["reason"], tt.reason)
					}
					wantExitCode := fmt.Sprintf("%d", tt.exitCode)
					if issue.Metadata["exitCode"] != wantExitCode {
						t.Errorf("metadata[exitCode] = %s, want %s", issue.Metadata["exitCode"], wantExitCode)
					}
					if issue.Suggestion == "" {
						t.Error("Expected non-empty suggestion for terminated container")
					}
					wantMsg := fmt.Sprintf("Container app terminated: %s (exit code %d)", tt.reason, tt.exitCode)
					if issue.Message != wantMsg {
						t.Errorf("message = %q, want %q", issue.Message, wantMsg)
					}
				}
			}
			if !foundNotReady {
				t.Error("Expected ContainerNotReady issue for terminated container")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// P1: diagnosePod — restart count edge cases
// ---------------------------------------------------------------------------

func TestDiagnosePod_RestartCountEdgeCases(t *testing.T) {
	tests := []struct {
		name             string
		restartCount     int32
		restartThreshold int
		wantHighRestart  bool
	}{
		{
			name:             "at threshold - no warning (needs > threshold)",
			restartCount:     3,
			restartThreshold: 3,
			wantHighRestart:  false,
		},
		{
			name:             "above threshold - warning",
			restartCount:     4,
			restartThreshold: 3,
			wantHighRestart:  true,
		},
		{
			name:             "negative threshold disables restart warnings",
			restartCount:     100,
			restartThreshold: -1,
			wantHighRestart:  false,
		},
		{
			name:             "zero threshold with restarts - warning",
			restartCount:     1,
			restartThreshold: 0,
			wantHighRestart:  true,
		},
		{
			name:             "zero restarts with zero threshold - no warning",
			restartCount:     0,
			restartThreshold: 0,
			wantHighRestart:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pods := []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "restart-pod"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:         "app",
								Ready:        true,
								RestartCount: tt.restartCount,
							},
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:           "app",
								LivenessProbe:  &corev1.Probe{},
								ReadinessProbe: &corev1.Probe{},
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("128Mi"),
										corev1.ResourceCPU:    resource.MustParse("100m"),
									},
									Requests: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("64Mi"),
										corev1.ResourceCPU:    resource.MustParse("50m"),
									},
								},
							},
						},
					},
				},
			}

			issues := DiagnosePods(pods, "default", "deployment/test", tt.restartThreshold)

			hasHighRestart := false
			for _, issue := range issues {
				if issue.Type == testIssueTypeHighRestartCount {
					hasHighRestart = true
				}
			}
			if hasHighRestart != tt.wantHighRestart {
				t.Errorf("HighRestartCount present = %v, want %v", hasHighRestart, tt.wantHighRestart)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// P1: diagnosePod — pod conditions
// ---------------------------------------------------------------------------

func TestDiagnosePod_PodReadyConditionFalse(t *testing.T) {
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "not-ready-pod"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "app", Ready: true},
				},
				Conditions: []corev1.PodCondition{
					{
						Type:    corev1.PodReady,
						Status:  corev1.ConditionFalse,
						Reason:  "ReadinessGatesNotReady",
						Message: "readiness gate not satisfied",
					},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:           "app",
						LivenessProbe:  &corev1.Probe{},
						ReadinessProbe: &corev1.Probe{},
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("128Mi"),
								corev1.ResourceCPU:    resource.MustParse("100m"),
							},
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("64Mi"),
								corev1.ResourceCPU:    resource.MustParse("50m"),
							},
						},
					},
				},
			},
		},
	}

	issues := DiagnosePods(pods, "default", "deployment/test", DefaultRestartThreshold)

	found := false
	for _, issue := range issues {
		if issue.Type == "PodNotReady" {
			found = true
			if issue.Severity != checker.SeverityWarning {
				t.Errorf("PodNotReady severity = %s, want Warning", issue.Severity)
			}
			if issue.Metadata["reason"] != "ReadinessGatesNotReady" {
				t.Errorf("metadata[reason] = %s, want ReadinessGatesNotReady", issue.Metadata["reason"])
			}
		}
	}
	if !found {
		t.Error("Expected PodNotReady issue for readiness gate failure")
	}
}

func TestDiagnosePod_PodReadyConditionContainersNotReady_Skipped(t *testing.T) {
	// When PodReady is false with reason "ContainersNotReady", the condition
	// should be skipped because container status issues are already reported.
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "containers-not-ready-pod"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "app", Ready: true},
				},
				Conditions: []corev1.PodCondition{
					{
						Type:    corev1.PodReady,
						Status:  corev1.ConditionFalse,
						Reason:  "ContainersNotReady",
						Message: "containers with unready status",
					},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:           "app",
						LivenessProbe:  &corev1.Probe{},
						ReadinessProbe: &corev1.Probe{},
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("128Mi"),
								corev1.ResourceCPU:    resource.MustParse("100m"),
							},
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("64Mi"),
								corev1.ResourceCPU:    resource.MustParse("50m"),
							},
						},
					},
				},
			},
		},
	}

	issues := DiagnosePods(pods, "default", "deployment/test", DefaultRestartThreshold)

	for _, issue := range issues {
		if issue.Type == "PodNotReady" {
			t.Error("Did not expect PodNotReady issue when reason is ContainersNotReady (already handled)")
		}
	}
}

// ---------------------------------------------------------------------------
// P1: diagnosePod — multiple containers with mixed states
// ---------------------------------------------------------------------------

func TestDiagnosePod_MultipleContainersMixedStates(t *testing.T) {
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "multi-container-pod"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "healthy-app",
						Ready: true,
					},
					{
						Name:  "failing-sidecar",
						Ready: false,
						State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{
								Reason: "CrashLoopBackOff",
							},
						},
						RestartCount: 5,
					},
					{
						Name:  "oom-container",
						Ready: false,
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								Reason:   testReasonOOMKilled,
								ExitCode: 137,
							},
						},
					},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:           "healthy-app",
						LivenessProbe:  &corev1.Probe{},
						ReadinessProbe: &corev1.Probe{},
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("128Mi"),
								corev1.ResourceCPU:    resource.MustParse("100m"),
							},
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("64Mi"),
								corev1.ResourceCPU:    resource.MustParse("50m"),
							},
						},
					},
					{
						Name:           "failing-sidecar",
						LivenessProbe:  &corev1.Probe{},
						ReadinessProbe: &corev1.Probe{},
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("128Mi"),
								corev1.ResourceCPU:    resource.MustParse("100m"),
							},
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("64Mi"),
								corev1.ResourceCPU:    resource.MustParse("50m"),
							},
						},
					},
					{
						Name:           "oom-container",
						LivenessProbe:  &corev1.Probe{},
						ReadinessProbe: &corev1.Probe{},
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("128Mi"),
								corev1.ResourceCPU:    resource.MustParse("100m"),
							},
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("64Mi"),
								corev1.ResourceCPU:    resource.MustParse("50m"),
							},
						},
					},
				},
			},
		},
	}

	issues := DiagnosePods(pods, "default", "deployment/test", DefaultRestartThreshold)

	issueMap := make(map[string][]checker.Issue)
	for _, issue := range issues {
		key := issue.Type + "/" + issue.Metadata["container"]
		issueMap[key] = append(issueMap[key], issue)
	}

	// Failing sidecar: ContainerNotReady (CrashLoopBackOff) + HighRestartCount
	if _, ok := issueMap["ContainerNotReady/failing-sidecar"]; !ok {
		t.Error("Expected ContainerNotReady for failing-sidecar")
	}
	if _, ok := issueMap["HighRestartCount/failing-sidecar"]; !ok {
		t.Error("Expected HighRestartCount for failing-sidecar")
	}

	// OOM container: ContainerNotReady (OOMKilled)
	if issues, ok := issueMap["ContainerNotReady/oom-container"]; !ok {
		t.Error("Expected ContainerNotReady for oom-container")
	} else if issues[0].Metadata["reason"] != testReasonOOMKilled {
		t.Errorf("oom-container reason = %s, want OOMKilled", issues[0].Metadata["reason"])
	}

	// Healthy container should NOT have ContainerNotReady
	if _, ok := issueMap["ContainerNotReady/healthy-app"]; ok {
		t.Error("Did not expect ContainerNotReady for healthy-app")
	}
}

// ---------------------------------------------------------------------------
// P1: diagnosePod — container not ready without waiting/terminated (plain not-ready)
// ---------------------------------------------------------------------------

func TestDiagnosePod_ContainerNotReadyNoState(t *testing.T) {
	// Container is not ready but has no Waiting or Terminated state set
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "plain-not-ready-pod"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "app",
						Ready: false,
						// No State.Waiting or State.Terminated
					},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:           "app",
						LivenessProbe:  &corev1.Probe{},
						ReadinessProbe: &corev1.Probe{},
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("128Mi"),
								corev1.ResourceCPU:    resource.MustParse("100m"),
							},
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("64Mi"),
								corev1.ResourceCPU:    resource.MustParse("50m"),
							},
						},
					},
				},
			},
		},
	}

	issues := DiagnosePods(pods, "default", "deployment/test", DefaultRestartThreshold)

	found := false
	for _, issue := range issues {
		if issue.Type == testIssueTypeContainerNotReady {
			found = true
			if !strings.Contains(issue.Message, "not ready") {
				t.Errorf("message = %q, want to contain 'not ready'", issue.Message)
			}
			// No reason metadata expected since neither Waiting nor Terminated is set
			if _, hasReason := issue.Metadata["reason"]; hasReason {
				t.Errorf("did not expect 'reason' in metadata for plain not-ready container")
			}
		}
	}
	if !found {
		t.Error("Expected ContainerNotReady issue for plain not-ready container")
	}
}

// ---------------------------------------------------------------------------
// P2: checkContainerBestPractices — individual best practice checks
// ---------------------------------------------------------------------------

func TestCheckContainerBestPractices(t *testing.T) {
	tests := []struct {
		name       string
		container  corev1.Container
		wantIssues map[string]string // type -> expected severity
	}{
		{
			name: "all best practices met - no issues",
			container: corev1.Container{
				Name: "well-configured",
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
			wantIssues: map[string]string{},
		},
		{
			name: "missing memory limit only",
			container: corev1.Container{
				Name: "no-mem-limit",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("500m"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("128Mi"),
						corev1.ResourceCPU:    resource.MustParse("100m"),
					},
				},
				LivenessProbe:  &corev1.Probe{},
				ReadinessProbe: &corev1.Probe{},
			},
			wantIssues: map[string]string{
				"NoMemoryLimit": checker.SeverityWarning,
			},
		},
		{
			name: "missing CPU limit only",
			container: corev1.Container{
				Name: "no-cpu-limit",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("128Mi"),
						corev1.ResourceCPU:    resource.MustParse("100m"),
					},
				},
				LivenessProbe:  &corev1.Probe{},
				ReadinessProbe: &corev1.Probe{},
			},
			wantIssues: map[string]string{
				"NoCPULimit": checker.SeverityInfo,
			},
		},
		{
			name: "no resource requests at all",
			container: corev1.Container{
				Name: "no-requests",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("256Mi"),
						corev1.ResourceCPU:    resource.MustParse("500m"),
					},
				},
				LivenessProbe:  &corev1.Probe{},
				ReadinessProbe: &corev1.Probe{},
			},
			wantIssues: map[string]string{
				"NoResourceRequests": checker.SeverityWarning,
			},
		},
		{
			name: "missing liveness probe",
			container: corev1.Container{
				Name: "no-liveness",
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
				ReadinessProbe: &corev1.Probe{},
			},
			wantIssues: map[string]string{
				"NoLivenessProbe": checker.SeverityInfo,
			},
		},
		{
			name: "missing readiness probe",
			container: corev1.Container{
				Name: "no-readiness",
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
				LivenessProbe: &corev1.Probe{},
			},
			wantIssues: map[string]string{
				"NoReadinessProbe": checker.SeverityInfo,
			},
		},
		{
			name: "everything missing",
			container: corev1.Container{
				Name: "bare-minimum",
			},
			wantIssues: map[string]string{
				"NoMemoryLimit":      checker.SeverityWarning,
				"NoCPULimit":         checker.SeverityInfo,
				"NoResourceRequests": checker.SeverityWarning,
				"NoLivenessProbe":    checker.SeverityInfo,
				"NoReadinessProbe":   checker.SeverityInfo,
			},
		},
		{
			name: "has CPU request only - no NoResourceRequests since memory is missing but CPU is present",
			container: corev1.Container{
				Name: "cpu-request-only",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("256Mi"),
						corev1.ResourceCPU:    resource.MustParse("500m"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("100m"),
					},
				},
				LivenessProbe:  &corev1.Probe{},
				ReadinessProbe: &corev1.Probe{},
			},
			// CPU request is set, so the condition (mem==0 && cpu==0) is false
			wantIssues: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New()
			issues := c.checkContainerBestPractices(tt.container, "test-pod", "default", "deployment/test")

			foundTypes := make(map[string]string)
			for _, issue := range issues {
				foundTypes[issue.Type] = issue.Severity
				// Verify metadata is correct for all best practice issues
				if issue.Metadata["container"] != tt.container.Name {
					t.Errorf("issue %s: metadata[container] = %s, want %s",
						issue.Type, issue.Metadata["container"], tt.container.Name)
				}
				if issue.Metadata["pod"] != "test-pod" {
					t.Errorf("issue %s: metadata[pod] = %s, want test-pod", issue.Type, issue.Metadata["pod"])
				}
				if issue.Namespace != "default" {
					t.Errorf("issue %s: namespace = %s, want default", issue.Type, issue.Namespace)
				}
				if issue.Resource != "deployment/test" {
					t.Errorf("issue %s: resource = %s, want deployment/test", issue.Type, issue.Resource)
				}
				if issue.Suggestion == "" {
					t.Errorf("issue %s: expected non-empty suggestion", issue.Type)
				}
			}

			for wantType, wantSeverity := range tt.wantIssues {
				gotSeverity, ok := foundTypes[wantType]
				if !ok {
					t.Errorf("expected issue type %s not found", wantType)
				} else if gotSeverity != wantSeverity {
					t.Errorf("issue %s: severity = %s, want %s", wantType, gotSeverity, wantSeverity)
				}
			}

			for foundType := range foundTypes {
				if _, expected := tt.wantIssues[foundType]; !expected {
					t.Errorf("unexpected issue type %s found", foundType)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// P2: checkContainerBestPractices — multiple containers, some with issues
// ---------------------------------------------------------------------------

func TestDiagnosePod_MultipleContainersBestPractices(t *testing.T) {
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "multi-bp-pod"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "good", Ready: true},
					{Name: "bad", Ready: true},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "good",
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
					{
						Name: "bad",
						// No resources, no probes
					},
				},
			},
		},
	}

	issues := DiagnosePods(pods, "default", "deployment/test", DefaultRestartThreshold)

	// "good" container should have zero best-practice issues
	// "bad" container should have all best-practice issues
	goodIssues := 0
	badIssues := 0
	for _, issue := range issues {
		if issue.Metadata["container"] == "good" {
			goodIssues++
		}
		if issue.Metadata["container"] == "bad" {
			badIssues++
		}
	}

	if goodIssues != 0 {
		t.Errorf("good container has %d issues, want 0", goodIssues)
	}
	if badIssues != 5 {
		t.Errorf("bad container has %d issues, want 5 (NoMemoryLimit, NoCPULimit, NoResourceRequests, NoLivenessProbe, NoReadinessProbe)", badIssues)
	}
}

// ---------------------------------------------------------------------------
// P3: Check() main flow — using fake K8s client via testutil
// ---------------------------------------------------------------------------

func TestCheck_SingleDeploymentHealthyPods(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-abc12",
			Namespace: "default",
			Labels:    map[string]string{"app": "web"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "app", Ready: true},
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:           "app",
					LivenessProbe:  &corev1.Probe{},
					ReadinessProbe: &corev1.Probe{},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("128Mi"),
							corev1.ResourceCPU:    resource.MustParse("100m"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("64Mi"),
							corev1.ResourceCPU:    resource.MustParse("50m"),
						},
					},
				},
			},
		},
	}

	checkCtx := testutil.NewCheckContext(t, []string{"default"}, deploy, pod)
	c := New()

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if result.CheckerName != CheckerName {
		t.Errorf("CheckerName = %s, want %s", result.CheckerName, CheckerName)
	}
	if result.Healthy != 1 {
		t.Errorf("Healthy = %d, want 1", result.Healthy)
	}
	if len(result.Issues) != 0 {
		t.Errorf("Issues = %d, want 0", len(result.Issues))
		for _, issue := range result.Issues {
			t.Logf("  Issue: %s - %s", issue.Type, issue.Message)
		}
	}
}

func TestCheck_DeploymentWithUnhealthyPods(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api",
			Namespace: "production",
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "api"},
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-xyz99",
			Namespace: "production",
			Labels:    map[string]string{"app": "api"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:  "api",
					Ready: false,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "CrashLoopBackOff",
						},
					},
					RestartCount: 10,
				},
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "api"},
			},
		},
	}

	checkCtx := testutil.NewCheckContext(t, []string{"production"}, deploy, pod)
	c := New()

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if result.Healthy != 0 {
		t.Errorf("Healthy = %d, want 0", result.Healthy)
	}
	if len(result.Issues) == 0 {
		t.Error("Expected issues for unhealthy pod")
	}

	// Check issues reference the deployment
	for _, issue := range result.Issues {
		if issue.Namespace != "production" {
			t.Errorf("issue namespace = %s, want production", issue.Namespace)
		}
		if issue.Resource != "deployment/api" {
			t.Errorf("issue resource = %s, want deployment/api", issue.Resource)
		}
	}
}

func TestCheck_StatefulSet(t *testing.T) {
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "db",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "db"},
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "db-0",
			Namespace: "default",
			Labels:    map[string]string{"app": "db"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "db"},
			},
		},
	}

	checkCtx := testutil.NewCheckContext(t, []string{"default"}, sts, pod)
	c := New()

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if result.Healthy != 0 {
		t.Errorf("Healthy = %d, want 0 (pending pod)", result.Healthy)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Resource == "statefulset/db" {
			found = true
		}
	}
	if !found {
		t.Error("Expected issues with resource = statefulset/db")
	}
}

func TestCheck_DaemonSet(t *testing.T) {
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "log-collector",
			Namespace: "kube-system",
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "log-collector"},
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "log-collector-node1",
			Namespace: "kube-system",
			Labels:    map[string]string{"app": "log-collector"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:  "collector",
					Ready: false,
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:   testReasonOOMKilled,
							ExitCode: 137,
						},
					},
				},
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "collector"},
			},
		},
	}

	checkCtx := testutil.NewCheckContext(t, []string{"kube-system"}, ds, pod)
	c := New()

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Resource == "daemonset/log-collector" {
			found = true
		}
	}
	if !found {
		t.Error("Expected issues with resource = daemonset/log-collector")
	}
}

func TestCheck_EmptyCluster(t *testing.T) {
	// No workloads at all
	checkCtx := testutil.NewCheckContext(t, []string{"default"})
	c := New()

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if result.Healthy != 0 {
		t.Errorf("Healthy = %d, want 0 (no workloads)", result.Healthy)
	}
	if len(result.Issues) != 0 {
		t.Errorf("Issues = %d, want 0 (no workloads)", len(result.Issues))
	}
}

func TestCheck_MultipleNamespaces(t *testing.T) {
	deploy1 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web",
			Namespace: "ns1",
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
		},
	}

	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-pod",
			Namespace: "ns1",
			Labels:    map[string]string{"app": "web"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "app", Ready: true},
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:           "app",
					LivenessProbe:  &corev1.Probe{},
					ReadinessProbe: &corev1.Probe{},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("128Mi"),
							corev1.ResourceCPU:    resource.MustParse("100m"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("64Mi"),
							corev1.ResourceCPU:    resource.MustParse("50m"),
						},
					},
				},
			},
		},
	}

	deploy2 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api",
			Namespace: "ns2",
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "api"},
			},
		},
	}

	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-pod",
			Namespace: "ns2",
			Labels:    map[string]string{"app": "api"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "api"},
			},
		},
	}

	checkCtx := testutil.NewCheckContext(t, []string{"ns1", "ns2"}, deploy1, pod1, deploy2, pod2)
	c := New()

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	// ns1/web is healthy, ns2/api is not
	if result.Healthy != 1 {
		t.Errorf("Healthy = %d, want 1", result.Healthy)
	}

	hasNs2Issue := false
	for _, issue := range result.Issues {
		if issue.Namespace == "ns2" {
			hasNs2Issue = true
		}
	}
	if !hasNs2Issue {
		t.Error("Expected issues in ns2 namespace")
	}
}

// ---------------------------------------------------------------------------
// P3: Check() — deployment with no matching pods (healthy still counted)
// ---------------------------------------------------------------------------

func TestCheck_DeploymentNoMatchingPods(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "orphan",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "orphan"},
			},
		},
	}

	// Pod in same namespace but different labels
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-pod",
			Namespace: "default",
			Labels:    map[string]string{"app": "other"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app"},
			},
		},
	}

	checkCtx := testutil.NewCheckContext(t, []string{"default"}, deploy, pod)
	c := New()

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	// No pods match -> no issues from pod diagnosis -> counts as healthy
	if result.Healthy != 1 {
		t.Errorf("Healthy = %d, want 1 (deployment with no pods = healthy by diagnosePods logic)", result.Healthy)
	}
}

// ---------------------------------------------------------------------------
// P3: Check() with config for restart threshold
// ---------------------------------------------------------------------------

func TestCheck_WithRestartThresholdConfig(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "myapp"},
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-pod",
			Namespace: "default",
			Labels:    map[string]string{"app": "myapp"},
		},
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
				{
					Name:           "app",
					LivenessProbe:  &corev1.Probe{},
					ReadinessProbe: &corev1.Probe{},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("128Mi"),
							corev1.ResourceCPU:    resource.MustParse("100m"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("64Mi"),
							corev1.ResourceCPU:    resource.MustParse("50m"),
						},
					},
				},
			},
		},
	}

	// High threshold so restart count 5 does NOT trigger
	config := map[string]any{ConfigRestartThreshold: 10}
	checkCtx := testutil.NewCheckContextWithConfig(t, []string{"default"}, config, deploy, pod)
	c := New()

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	for _, issue := range result.Issues {
		if issue.Type == testIssueTypeHighRestartCount {
			t.Error("Did not expect HighRestartCount with threshold=10 and restartCount=5")
		}
	}
}

// ---------------------------------------------------------------------------
// P4: getRestartThreshold
// ---------------------------------------------------------------------------

func TestGetRestartThreshold(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]any
		want   int
	}{
		{
			name:   "nil config returns default",
			config: nil,
			want:   DefaultRestartThreshold,
		},
		{
			name:   "empty config returns default",
			config: map[string]any{},
			want:   DefaultRestartThreshold,
		},
		{
			name:   "config with int threshold",
			config: map[string]any{ConfigRestartThreshold: 10},
			want:   10,
		},
		{
			name:   "config with wrong type returns default",
			config: map[string]any{ConfigRestartThreshold: "not-an-int"},
			want:   DefaultRestartThreshold,
		},
		{
			name:   "config with zero threshold",
			config: map[string]any{ConfigRestartThreshold: 0},
			want:   0,
		},
		{
			name:   "config with negative threshold",
			config: map[string]any{ConfigRestartThreshold: -1},
			want:   -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getRestartThreshold(tt.config)
			if got != tt.want {
				t.Errorf("getRestartThreshold() = %d, want %d", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// P3: Check() — mixed workload types in same namespace
// ---------------------------------------------------------------------------

func TestCheck_MixedWorkloadTypes(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
		},
	}
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "cache", Namespace: "default"},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "cache"},
			},
		},
	}
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "monitor", Namespace: "default"},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "monitor"},
			},
		},
	}

	makePod := func(name string, labels map[string]string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
				Labels:    labels,
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "c", Ready: true},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:           "c",
						LivenessProbe:  &corev1.Probe{},
						ReadinessProbe: &corev1.Probe{},
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("128Mi"),
								corev1.ResourceCPU:    resource.MustParse("100m"),
							},
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("64Mi"),
								corev1.ResourceCPU:    resource.MustParse("50m"),
							},
						},
					},
				},
			},
		}
	}

	webPod := makePod("web-pod", map[string]string{"app": "web"})
	cachePod := makePod("cache-0", map[string]string{"app": "cache"})
	monitorPod := makePod("monitor-node1", map[string]string{"app": "monitor"})

	checkCtx := testutil.NewCheckContext(t, []string{"default"}, deploy, sts, ds, webPod, cachePod, monitorPod)
	c := New()

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if result.Healthy != 3 {
		t.Errorf("Healthy = %d, want 3 (one deploy + one sts + one ds, all healthy)", result.Healthy)
	}
	if len(result.Issues) != 0 {
		t.Errorf("Issues = %d, want 0", len(result.Issues))
		for _, issue := range result.Issues {
			t.Logf("  Issue: %s - %s (%s)", issue.Type, issue.Message, issue.Resource)
		}
	}
}
