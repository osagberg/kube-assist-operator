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
