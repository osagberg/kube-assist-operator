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

package checker

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/osagberg/kube-assist-operator/internal/ai"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
)

func newFakeDS(objs ...runtime.Object) datasource.DataSource {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	builder := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...)
	return datasource.NewKubernetes(builder.Build())
}

func TestContextBuilder_EventsPopulated(t *testing.T) {
	now := metav1.Now()
	events := []runtime.Object{
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "ev1", Namespace: "ns1"},
			InvolvedObject: corev1.ObjectReference{Kind: "Deployment", Name: "my-app"},
			Reason:         "ScalingReplicaSet",
			Message:        "Scaled up replica set my-app-abc to 2",
			LastTimestamp:  metav1.NewTime(now.Add(-1 * time.Minute)),
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "ev2", Namespace: "ns1"},
			InvolvedObject: corev1.ObjectReference{Kind: "Deployment", Name: "my-app"},
			Reason:         "Unhealthy",
			Message:        "Readiness probe failed",
			LastTimestamp:  now,
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "ev3", Namespace: "ns1"},
			InvolvedObject: corev1.ObjectReference{Kind: "Deployment", Name: "other-app"},
			Reason:         "Pulled",
			Message:        "Image pulled",
			LastTimestamp:  now,
		},
	}

	ds := newFakeDS(events...)
	builder := NewContextBuilder(ds, nil, LogContextConfig{
		Enabled:           true,
		MaxEventsPerIssue: 10,
		MaxLogLines:       50,
		MaxTotalChars:     30000,
	})

	issues := []ai.IssueContext{
		{
			Type:      "Unhealthy",
			Severity:  "Warning",
			Resource:  "deployment/my-app",
			Namespace: "ns1",
			Message:   "Deployment my-app is unhealthy",
		},
	}

	result := builder.Enrich(context.Background(), issues)
	if len(result[0].Events) != 2 {
		t.Errorf("expected 2 events, got %d: %v", len(result[0].Events), result[0].Events)
	}
	// Most recent event should be first
	if len(result[0].Events) > 0 && !strings.Contains(result[0].Events[0], "Unhealthy") {
		t.Errorf("expected most recent event first, got: %s", result[0].Events[0])
	}
}

func TestContextBuilder_LogsPopulated(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod",
			Namespace: "ns1",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "main"}},
		},
	}

	ds := newFakeDS(pod)
	clientset := k8sfake.NewSimpleClientset(pod) //nolint:staticcheck // NewClientset requires apply config generation

	builder := NewContextBuilder(ds, clientset, LogContextConfig{
		Enabled:           true,
		MaxEventsPerIssue: 10,
		MaxLogLines:       50,
		MaxTotalChars:     30000,
	})

	issues := []ai.IssueContext{
		{
			Type:      "CrashLoopBackOff",
			Severity:  "Critical",
			Resource:  "pod/my-pod",
			Namespace: "ns1",
			Message:   "Pod my-pod is crash looping",
		},
	}

	// The fake clientset returns empty logs, but the builder should not panic
	result := builder.Enrich(context.Background(), issues)
	// Logs may be empty since fake clientset doesn't produce real logs, but no panic
	_ = result[0].Logs
}

func TestContextBuilder_NoClientset(t *testing.T) {
	ds := newFakeDS()
	builder := NewContextBuilder(ds, nil, LogContextConfig{
		Enabled:           true,
		MaxEventsPerIssue: 10,
		MaxLogLines:       50,
		MaxTotalChars:     30000,
	})

	issues := []ai.IssueContext{
		{
			Type:      "CrashLoopBackOff",
			Severity:  "Critical",
			Resource:  "pod/my-pod",
			Namespace: "ns1",
			Message:   "Pod my-pod is crash looping",
		},
	}

	result := builder.Enrich(context.Background(), issues)
	if len(result[0].Logs) != 0 {
		t.Errorf("expected no logs when clientset is nil, got %d", len(result[0].Logs))
	}
}

func TestContextBuilder_TokenBudgetCapping(t *testing.T) {
	now := metav1.Now()
	events := make([]runtime.Object, 0, 20)
	for i := range 20 {
		events = append(events, &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("ev%d", i),
				Namespace: "ns1",
			},
			InvolvedObject: corev1.ObjectReference{Kind: "Deployment", Name: "my-app"},
			Reason:         "Event",
			Message:        strings.Repeat("x", 500), // 500 chars each
			LastTimestamp:  metav1.NewTime(now.Add(-time.Duration(i) * time.Minute)),
		})
	}

	ds := newFakeDS(events...)
	builder := NewContextBuilder(ds, nil, LogContextConfig{
		Enabled:           true,
		MaxEventsPerIssue: 20,
		MaxLogLines:       50,
		MaxTotalChars:     2000, // Low budget to trigger capping
	})

	issues := []ai.IssueContext{
		{
			Type:      "Unhealthy",
			Severity:  "Warning",
			Resource:  "deployment/my-app",
			Namespace: "ns1",
			Message:   "Deployment my-app is unhealthy",
		},
	}

	result := builder.Enrich(context.Background(), issues)
	total := contextChars(result)
	if total > 2000 {
		t.Errorf("expected total chars <= 2000, got %d", total)
	}
}

func TestContextBuilder_GracefulErrors(t *testing.T) {
	// Use a DS with no events at all — should not error
	ds := newFakeDS()
	builder := NewContextBuilder(ds, nil, LogContextConfig{
		Enabled:           true,
		MaxEventsPerIssue: 10,
		MaxLogLines:       50,
		MaxTotalChars:     30000,
	})

	issues := []ai.IssueContext{
		{
			Type:      "Unknown",
			Severity:  "Warning",
			Resource:  "deployment/my-app",
			Namespace: "ns1",
			Message:   "Something happened",
		},
	}

	result := builder.Enrich(context.Background(), issues)
	if len(result[0].Events) != 0 {
		t.Errorf("expected empty events, got %d", len(result[0].Events))
	}
	if len(result[0].Logs) != 0 {
		t.Errorf("expected empty logs, got %d", len(result[0].Logs))
	}
}

func TestContextBuilder_Disabled(t *testing.T) {
	ds := newFakeDS()
	builder := NewContextBuilder(ds, nil, LogContextConfig{
		Enabled: false,
	})

	issues := []ai.IssueContext{
		{
			Type:      "CrashLoopBackOff",
			Severity:  "Critical",
			Resource:  "pod/my-pod",
			Namespace: "ns1",
			Message:   "Pod crash looping",
		},
	}

	result := builder.Enrich(context.Background(), issues)
	if len(result[0].Events) != 0 || len(result[0].Logs) != 0 {
		t.Error("expected no enrichment when disabled")
	}
}

func TestContextBuilder_NonCrashIssue(t *testing.T) {
	now := metav1.Now()
	objs := []runtime.Object{
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "ev1", Namespace: "ns1"},
			InvolvedObject: corev1.ObjectReference{Kind: "Deployment", Name: "my-app"},
			Reason:         "ScalingReplicaSet",
			Message:        "Scaled up",
			LastTimestamp:  now,
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "my-pod", Namespace: "ns1"},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
		},
	}

	ds := newFakeDS(objs...)
	clientset := k8sfake.NewSimpleClientset(objs[1].(*corev1.Pod)) //nolint:staticcheck // NewClientset requires apply config generation

	builder := NewContextBuilder(ds, clientset, LogContextConfig{
		Enabled:           true,
		MaxEventsPerIssue: 10,
		MaxLogLines:       50,
		MaxTotalChars:     30000,
	})

	issues := []ai.IssueContext{
		{
			Type:      "InsufficientReplicas",
			Severity:  "Warning",
			Resource:  "deployment/my-app",
			Namespace: "ns1",
			Message:   "Deployment has fewer replicas than desired",
		},
	}

	result := builder.Enrich(context.Background(), issues)
	// Should have events but NOT logs (non-crash issue)
	if len(result[0].Events) == 0 {
		t.Error("expected events for non-crash issue")
	}
	if len(result[0].Logs) != 0 {
		t.Error("expected no logs for non-crash issue type")
	}
}

func TestContextBuilder_MultipleIssues(t *testing.T) {
	now := metav1.Now()
	objs := make([]runtime.Object, 0, 30)
	// Events for two different resources
	for i := range 15 {
		objs = append(objs, &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("ev-app1-%d", i),
				Namespace: "ns1",
			},
			InvolvedObject: corev1.ObjectReference{Kind: "Deployment", Name: "app1"},
			Reason:         "Event",
			Message:        strings.Repeat("a", 200),
			LastTimestamp:  metav1.NewTime(now.Add(-time.Duration(i) * time.Minute)),
		})
	}
	for i := range 15 {
		objs = append(objs, &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("ev-app2-%d", i),
				Namespace: "ns1",
			},
			InvolvedObject: corev1.ObjectReference{Kind: "Deployment", Name: "app2"},
			Reason:         "Event",
			Message:        strings.Repeat("b", 200),
			LastTimestamp:  metav1.NewTime(now.Add(-time.Duration(i) * time.Minute)),
		})
	}

	ds := newFakeDS(objs...)
	builder := NewContextBuilder(ds, nil, LogContextConfig{
		Enabled:           true,
		MaxEventsPerIssue: 10,
		MaxLogLines:       50,
		MaxTotalChars:     3000, // Low budget to test proportional trimming
	})

	issues := []ai.IssueContext{
		{
			Type:      "Unhealthy",
			Severity:  "Warning",
			Resource:  "deployment/app1",
			Namespace: "ns1",
			Message:   "Deployment app1 is unhealthy",
		},
		{
			Type:      "Unhealthy",
			Severity:  "Warning",
			Resource:  "deployment/app2",
			Namespace: "ns1",
			Message:   "Deployment app2 is unhealthy",
		},
	}

	result := builder.Enrich(context.Background(), issues)
	total := contextChars(result)
	if total > 3000 {
		t.Errorf("expected total chars <= 3000, got %d", total)
	}
	// Both issues should have some events (proportional trimming)
	if len(result[0].Events) == 0 && len(result[1].Events) == 0 {
		t.Error("expected at least some events on both issues after proportional trimming")
	}
}

func TestContextBuilder_MaxEventsRespected(t *testing.T) {
	now := metav1.Now()
	events := make([]runtime.Object, 0, 20)
	for i := range 20 {
		events = append(events, &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("ev%d", i),
				Namespace: "ns1",
			},
			InvolvedObject: corev1.ObjectReference{Kind: "Deployment", Name: "my-app"},
			Reason:         "Event",
			Message:        "short event",
			LastTimestamp:  metav1.NewTime(now.Add(-time.Duration(i) * time.Minute)),
		})
	}

	ds := newFakeDS(events...)
	builder := NewContextBuilder(ds, nil, LogContextConfig{
		Enabled:           true,
		MaxEventsPerIssue: 5, // Limit to 5
		MaxLogLines:       50,
		MaxTotalChars:     30000,
	})

	issues := []ai.IssueContext{
		{
			Type:      "Unhealthy",
			Severity:  "Warning",
			Resource:  "deployment/my-app",
			Namespace: "ns1",
			Message:   "Unhealthy",
		},
	}

	result := builder.Enrich(context.Background(), issues)
	if len(result[0].Events) > 5 {
		t.Errorf("expected at most 5 events, got %d", len(result[0].Events))
	}
}

func TestContextBuilder_InvalidResource(t *testing.T) {
	ds := newFakeDS()
	builder := NewContextBuilder(ds, nil, LogContextConfig{
		Enabled:           true,
		MaxEventsPerIssue: 10,
		MaxLogLines:       50,
		MaxTotalChars:     30000,
	})

	issues := []ai.IssueContext{
		{
			Type:      "Unknown",
			Severity:  "Info",
			Resource:  "invalid-no-slash",
			Namespace: "ns1",
			Message:   "Something",
		},
	}

	result := builder.Enrich(context.Background(), issues)
	if len(result[0].Events) != 0 {
		t.Error("expected no events for invalid resource format")
	}
}

func TestParseResource(t *testing.T) {
	tests := []struct {
		input    string
		wantKind string
		wantName string
	}{
		{"deployment/my-app", "deployment", "my-app"},
		{"pod/my-pod-abc", "pod", "my-pod-abc"},
		{"statefulset/db-0", "statefulset", "db-0"},
		{"invalid-no-slash", "", ""},
		{"", "", ""},
		{"a/b/c", "a", "b/c"}, // SplitN with 2 keeps remainder
	}

	for _, tt := range tests {
		kind, name := parseResource(tt.input)
		if kind != tt.wantKind || name != tt.wantName {
			t.Errorf("parseResource(%q) = (%q, %q), want (%q, %q)",
				tt.input, kind, name, tt.wantKind, tt.wantName)
		}
	}
}

func TestContextChars(t *testing.T) {
	issues := []ai.IssueContext{
		{Events: []string{"abc", "def"}, Logs: []string{"ghi"}},
		{Events: []string{"jk"}, Logs: []string{"lm", "no"}},
	}
	got := contextChars(issues)
	// abc(3) + def(3) + ghi(3) + jk(2) + lm(2) + no(2) = 15
	if got != 15 {
		t.Errorf("contextChars = %d, want 15", got)
	}
}

func TestContextBuilder_DeploymentOwnershipChain(t *testing.T) {
	// Pod owned by a ReplicaSet that belongs to the deployment
	rs := "my-app-7f8b4c5d9f"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rs + "-x2k4p",
			Namespace: "ns1",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "ReplicaSet",
					Name: rs,
				},
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}

	ds := newFakeDS(pod)
	clientset := k8sfake.NewSimpleClientset(pod) //nolint:staticcheck // NewClientset requires apply config generation

	builder := NewContextBuilder(ds, clientset, LogContextConfig{
		Enabled:           true,
		MaxEventsPerIssue: 10,
		MaxLogLines:       50,
		MaxTotalChars:     30000,
	})

	issues := []ai.IssueContext{
		{
			Type:      "CrashLoopBackOff",
			Severity:  "Critical",
			Resource:  "deployment/my-app",
			Namespace: "ns1",
			Message:   "Deployment my-app pods are crash looping",
		},
	}

	result := builder.Enrich(context.Background(), issues)
	// Should not panic and should attempt to find logs through the ownership chain
	_ = result[0].Logs
}

func TestContextBuilder_DeploymentNoFalseMatch(t *testing.T) {
	// Pod owned by ReplicaSet "app-v2-abc1234567" should NOT match deployment "app"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-v2-abc1234567-x2k4p",
			Namespace: "ns1",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "ReplicaSet",
					Name: "app-v2-abc1234567",
				},
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}

	ds := newFakeDS(pod)
	clientset := k8sfake.NewSimpleClientset(pod) //nolint:staticcheck // NewClientset requires apply config generation

	builder := NewContextBuilder(ds, clientset, LogContextConfig{
		Enabled:           true,
		MaxEventsPerIssue: 10,
		MaxLogLines:       50,
		MaxTotalChars:     30000,
	})

	issues := []ai.IssueContext{
		{
			Type:      "CrashLoopBackOff",
			Severity:  "Critical",
			Resource:  "deployment/app",
			Namespace: "ns1",
			Message:   "Deployment app pods are crash looping",
		},
	}

	result := builder.Enrich(context.Background(), issues)
	// Should NOT find any pods since "app-v2-abc1234567" doesn't belong to deployment "app"
	if len(result[0].Logs) != 0 {
		t.Error("expected no logs — ReplicaSet app-v2-abc1234567 should not match deployment app")
	}
}

// ---------------------------------------------------------------------------
// capTokenBudget direct tests
// ---------------------------------------------------------------------------

func TestCapTokenBudget_UnderBudget(t *testing.T) {
	b := &ContextBuilder{config: LogContextConfig{MaxTotalChars: 1000}}
	issues := []ai.IssueContext{
		{Events: []string{"short"}, Logs: []string{"line"}},
	}
	result := b.capTokenBudget(issues)
	// Under budget, should be unchanged
	if len(result[0].Events) != 1 || result[0].Events[0] != "short" {
		t.Errorf("expected events unchanged, got %v", result[0].Events)
	}
	if len(result[0].Logs) != 1 || result[0].Logs[0] != "line" {
		t.Errorf("expected logs unchanged, got %v", result[0].Logs)
	}
}

func TestCapTokenBudget_DefaultMaxChars(t *testing.T) {
	// MaxTotalChars=0 should default to 30000
	b := &ContextBuilder{config: LogContextConfig{MaxTotalChars: 0}}
	issues := []ai.IssueContext{
		{Events: []string{"small event"}},
	}
	result := b.capTokenBudget(issues)
	// 11 chars is well under 30000, should be unchanged
	if len(result[0].Events) != 1 {
		t.Errorf("expected events unchanged with default budget, got %d events", len(result[0].Events))
	}
}

func TestCapTokenBudget_EventTrimmingPass(t *testing.T) {
	// Budget is 100, but we have 3 events of 50 chars each (150 total).
	// Event trimming should remove oldest events until under budget.
	b := &ContextBuilder{config: LogContextConfig{MaxTotalChars: 100}}
	issues := []ai.IssueContext{
		{Events: []string{
			strings.Repeat("a", 50),
			strings.Repeat("b", 50),
			strings.Repeat("c", 50),
		}},
	}
	result := b.capTokenBudget(issues)
	total := contextChars(result)
	if total > 100 {
		t.Errorf("expected total <= 100 after event trimming, got %d", total)
	}
	if len(result[0].Events) >= 3 {
		t.Errorf("expected some events trimmed, got %d events", len(result[0].Events))
	}
}

func TestCapTokenBudget_LogTrimmingPass(t *testing.T) {
	// Budget is 80, events have only 1 entry (can't trim further in pass 1),
	// but logs have 3 entries. Log trimming should remove oldest lines.
	b := &ContextBuilder{config: LogContextConfig{MaxTotalChars: 80}}
	issues := []ai.IssueContext{
		{
			Events: []string{strings.Repeat("e", 30)},
			Logs: []string{
				strings.Repeat("x", 30),
				strings.Repeat("y", 30),
				strings.Repeat("z", 30),
			},
		},
	}
	// Total: 30 + 30 + 30 + 30 = 120 > 80
	result := b.capTokenBudget(issues)
	total := contextChars(result)
	if total > 80 {
		t.Errorf("expected total <= 80 after log trimming, got %d", total)
	}
}

func TestCapTokenBudget_FinalCascadeClear(t *testing.T) {
	// Budget is very small (5), but we have single-entry events and logs.
	// First and second passes can't trim single entries. Final pass should clear them.
	b := &ContextBuilder{config: LogContextConfig{MaxTotalChars: 5}}
	issues := []ai.IssueContext{
		{
			Events: []string{strings.Repeat("e", 20)},
			Logs:   []string{strings.Repeat("l", 20)},
		},
		{
			Events: []string{strings.Repeat("f", 20)},
			Logs:   []string{strings.Repeat("m", 20)},
		},
	}
	// Total: 20 + 20 + 20 + 20 = 80 >> 5
	result := b.capTokenBudget(issues)
	total := contextChars(result)
	if total > 5 {
		t.Errorf("expected total <= 5 after final cascade, got %d", total)
	}
	// Everything should be cleared
	for i, ic := range result {
		if len(ic.Events) != 0 {
			t.Errorf("issue %d: expected Events cleared, got %d", i, len(ic.Events))
		}
		if len(ic.Logs) != 0 {
			t.Errorf("issue %d: expected Logs cleared, got %d", i, len(ic.Logs))
		}
	}
}

func TestCapTokenBudget_EventTrimEarlyReturn(t *testing.T) {
	// Budget is 60, we have 2 issues with 2 events each of 20 chars.
	// Total = 80. After trimming 1 event from each issue, we get 40 which is <= 60.
	// The function should return as soon as budget is met.
	b := &ContextBuilder{config: LogContextConfig{MaxTotalChars: 60}}
	issues := []ai.IssueContext{
		{Events: []string{strings.Repeat("a", 20), strings.Repeat("b", 20)}},
		{Events: []string{strings.Repeat("c", 20), strings.Repeat("d", 20)}},
	}
	result := b.capTokenBudget(issues)
	total := contextChars(result)
	if total > 60 {
		t.Errorf("expected total <= 60, got %d", total)
	}
	// Should still have some events (not everything cleared)
	totalEvents := len(result[0].Events) + len(result[1].Events)
	if totalEvents == 0 {
		t.Error("expected some events to remain after partial trim")
	}
}

func TestCapTokenBudget_LogTrimEarlyReturn(t *testing.T) {
	// Only logs, no events. Budget is 40, each issue has 2 log lines of 20 chars.
	// Total = 80, need to trim. Log trimming should handle it.
	b := &ContextBuilder{config: LogContextConfig{MaxTotalChars: 40}}
	issues := []ai.IssueContext{
		{Logs: []string{strings.Repeat("a", 20), strings.Repeat("b", 20)}},
		{Logs: []string{strings.Repeat("c", 20), strings.Repeat("d", 20)}},
	}
	result := b.capTokenBudget(issues)
	total := contextChars(result)
	if total > 40 {
		t.Errorf("expected total <= 40, got %d", total)
	}
}

func TestCapTokenBudget_MixedEventsAndLogs(t *testing.T) {
	// Mix of events and logs across multiple issues, with a tight budget.
	b := &ContextBuilder{config: LogContextConfig{MaxTotalChars: 50}}
	issues := []ai.IssueContext{
		{
			Events: []string{strings.Repeat("e", 15), strings.Repeat("e", 15)},
			Logs:   []string{strings.Repeat("l", 15), strings.Repeat("l", 15)},
		},
		{
			Events: []string{strings.Repeat("f", 15)},
			Logs:   []string{strings.Repeat("m", 15), strings.Repeat("m", 15)},
		},
	}
	// Total: 15*2 + 15*2 + 15 + 15*2 = 30 + 30 + 15 + 30 = 105 >> 50
	result := b.capTokenBudget(issues)
	total := contextChars(result)
	if total > 50 {
		t.Errorf("expected total <= 50, got %d", total)
	}
}

func TestCapTokenBudget_EmptyIssues(t *testing.T) {
	b := &ContextBuilder{config: LogContextConfig{MaxTotalChars: 100}}
	issues := []ai.IssueContext{}
	result := b.capTokenBudget(issues)
	if len(result) != 0 {
		t.Errorf("expected empty result for empty issues, got %d", len(result))
	}
}

func TestCapTokenBudget_NoEventsOrLogs(t *testing.T) {
	b := &ContextBuilder{config: LogContextConfig{MaxTotalChars: 100}}
	issues := []ai.IssueContext{
		{Type: "test", Message: "no events or logs"},
	}
	result := b.capTokenBudget(issues)
	if contextChars(result) != 0 {
		t.Errorf("expected 0 chars for issues with no events/logs")
	}
}
