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

package plugin

import (
	"context"
	"maps"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
)

// fakeDataSource implements datasource.DataSource for testing.
type fakeDataSource struct {
	items []unstructured.Unstructured
	err   error
}

func (f *fakeDataSource) Get(_ context.Context, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
	return f.err
}

func (f *fakeDataSource) List(_ context.Context, list client.ObjectList, _ ...client.ListOption) error {
	if f.err != nil {
		return f.err
	}
	ul, ok := list.(*unstructured.UnstructuredList)
	if !ok {
		return nil
	}
	ul.Items = f.items
	return nil
}

// Ensure fakeDataSource implements the interface
var _ datasource.DataSource = &fakeDataSource{}

func TestNewPluginChecker(t *testing.T) {
	tests := []struct {
		name    string
		spec    assistv1alpha1.CheckPluginSpec
		wantErr bool
	}{
		{
			name: "valid spec with one rule",
			spec: assistv1alpha1.CheckPluginSpec{
				DisplayName: "test-plugin",
				TargetResource: assistv1alpha1.TargetGVR{
					Group:    "apps",
					Version:  "v1",
					Resource: "deployments",
					Kind:     "Deployment",
				},
				Rules: []assistv1alpha1.CheckRule{
					{
						Name:      "replicas-check",
						Condition: "object.spec.replicas > 5",
						Severity:  "Warning",
						Message:   "Too many replicas",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid spec with CEL message",
			spec: assistv1alpha1.CheckPluginSpec{
				DisplayName: "cel-message-plugin",
				TargetResource: assistv1alpha1.TargetGVR{
					Group:    "apps",
					Version:  "v1",
					Resource: "deployments",
					Kind:     "Deployment",
				},
				Rules: []assistv1alpha1.CheckRule{
					{
						Name:      "name-check",
						Condition: `object.metadata.name == "bad"`,
						Severity:  "Critical",
						Message:   `="Deployment " + object.metadata.name + " is bad"`,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid condition expression",
			spec: assistv1alpha1.CheckPluginSpec{
				DisplayName: "bad-plugin",
				TargetResource: assistv1alpha1.TargetGVR{
					Version:  "v1",
					Resource: "pods",
					Kind:     "Pod",
				},
				Rules: []assistv1alpha1.CheckRule{
					{
						Name:      "bad-rule",
						Condition: "invalid CEL ===",
						Severity:  "Warning",
						Message:   "test",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid message CEL expression",
			spec: assistv1alpha1.CheckPluginSpec{
				DisplayName: "bad-msg-plugin",
				TargetResource: assistv1alpha1.TargetGVR{
					Version:  "v1",
					Resource: "pods",
					Kind:     "Pod",
				},
				Rules: []assistv1alpha1.CheckRule{
					{
						Name:      "bad-msg-rule",
						Condition: "true",
						Severity:  "Warning",
						Message:   "=invalid CEL ===",
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc, err := NewPluginChecker(tt.spec.DisplayName, tt.spec)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewPluginChecker() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && pc == nil {
				t.Error("NewPluginChecker() returned nil for valid spec")
			}
		})
	}
}

func TestPluginChecker_Name(t *testing.T) {
	spec := assistv1alpha1.CheckPluginSpec{
		DisplayName: "my-plugin",
		TargetResource: assistv1alpha1.TargetGVR{
			Version:  "v1",
			Resource: "pods",
			Kind:     "Pod",
		},
		Rules: []assistv1alpha1.CheckRule{
			{
				Name:      "test",
				Condition: "true",
				Message:   "test",
			},
		},
	}

	pc, err := NewPluginChecker("my-plugin", spec)
	if err != nil {
		t.Fatalf("NewPluginChecker() error = %v", err)
	}

	if got := pc.Name(); got != "plugin:my-plugin" {
		t.Errorf("Name() = %q, want %q", got, "plugin:my-plugin")
	}
}

func TestPluginChecker_Supports(t *testing.T) {
	spec := assistv1alpha1.CheckPluginSpec{
		DisplayName: "test",
		TargetResource: assistv1alpha1.TargetGVR{
			Version:  "v1",
			Resource: "pods",
			Kind:     "Pod",
		},
		Rules: []assistv1alpha1.CheckRule{
			{Name: "r", Condition: "true", Message: "m"},
		},
	}

	pc, err := NewPluginChecker("test", spec)
	if err != nil {
		t.Fatalf("NewPluginChecker() error = %v", err)
	}

	if !pc.Supports(context.Background(), nil) {
		t.Error("Supports() = false, want true")
	}
}

func TestPluginChecker_Check(t *testing.T) {
	tests := []struct {
		name       string
		spec       assistv1alpha1.CheckPluginSpec
		items      []unstructured.Unstructured
		namespaces []string
		wantIssues int
		wantHealth int
	}{
		{
			name: "detects issue with matching condition",
			spec: assistv1alpha1.CheckPluginSpec{
				DisplayName: "replica-check",
				TargetResource: assistv1alpha1.TargetGVR{
					Group:    "apps",
					Version:  "v1",
					Resource: "deployments",
					Kind:     "Deployment",
				},
				Rules: []assistv1alpha1.CheckRule{
					{
						Name:      "too-many-replicas",
						Condition: "object.spec.replicas > 3",
						Severity:  "Warning",
						Message:   "Too many replicas configured",
					},
				},
			},
			items: []unstructured.Unstructured{
				makeUnstructured("apps", "Deployment", "big-deploy", map[string]any{
					"spec": map[string]any{"replicas": int64(5)},
				}),
				makeUnstructured("apps", "Deployment", "small-deploy", map[string]any{
					"spec": map[string]any{"replicas": int64(2)},
				}),
			},
			namespaces: []string{"default"},
			wantIssues: 1,
			wantHealth: 1,
		},
		{
			name: "all resources healthy",
			spec: assistv1alpha1.CheckPluginSpec{
				DisplayName: "healthy-check",
				TargetResource: assistv1alpha1.TargetGVR{
					Version:  "v1",
					Resource: "pods",
					Kind:     "Pod",
				},
				Rules: []assistv1alpha1.CheckRule{
					{
						Name:      "not-running",
						Condition: `object.status.phase != "Running"`,
						Severity:  "Critical",
						Message:   "Pod is not running",
					},
				},
			},
			items: []unstructured.Unstructured{
				makeUnstructured("", "Pod", "pod-1", map[string]any{
					"status": map[string]any{"phase": "Running"},
				}),
				makeUnstructured("", "Pod", "pod-2", map[string]any{
					"status": map[string]any{"phase": "Running"},
				}),
			},
			namespaces: []string{"default"},
			wantIssues: 0,
			wantHealth: 2,
		},
		{
			name: "CEL message expression works",
			spec: assistv1alpha1.CheckPluginSpec{
				DisplayName: "cel-msg",
				TargetResource: assistv1alpha1.TargetGVR{
					Version:  "v1",
					Resource: "pods",
					Kind:     "Pod",
				},
				Rules: []assistv1alpha1.CheckRule{
					{
						Name:      "bad-pod",
						Condition: `object.metadata.name == "bad-pod"`,
						Severity:  "Warning",
						Message:   `="Pod " + object.metadata.name + " is problematic"`,
					},
				},
			},
			items: []unstructured.Unstructured{
				makeUnstructured("", "Pod", "bad-pod", map[string]any{}),
			},
			namespaces: []string{"default"},
			wantIssues: 1,
			wantHealth: 0,
		},
		{
			name: "empty resource list",
			spec: assistv1alpha1.CheckPluginSpec{
				DisplayName: "empty-check",
				TargetResource: assistv1alpha1.TargetGVR{
					Version:  "v1",
					Resource: "pods",
					Kind:     "Pod",
				},
				Rules: []assistv1alpha1.CheckRule{
					{
						Name:      "always-true",
						Condition: "true",
						Message:   "should not fire",
					},
				},
			},
			items:      []unstructured.Unstructured{},
			namespaces: []string{"default"},
			wantIssues: 0,
			wantHealth: 0,
		},
		{
			name: "multiple rules evaluated per resource",
			spec: assistv1alpha1.CheckPluginSpec{
				DisplayName: "multi-rule",
				TargetResource: assistv1alpha1.TargetGVR{
					Group:    "apps",
					Version:  "v1",
					Resource: "deployments",
					Kind:     "Deployment",
				},
				Rules: []assistv1alpha1.CheckRule{
					{
						Name:      "high-replicas",
						Condition: "object.spec.replicas > 3",
						Severity:  "Warning",
						Message:   "Too many replicas",
					},
					{
						Name:      "missing-labels",
						Condition: `!has(object.metadata.labels)`,
						Severity:  "Info",
						Message:   "No labels set",
					},
				},
			},
			items: []unstructured.Unstructured{
				makeUnstructured("apps", "Deployment", "unlabeled-big", map[string]any{
					"spec": map[string]any{"replicas": int64(5)},
				}),
			},
			namespaces: []string{"default"},
			wantIssues: 2,
			wantHealth: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc, err := NewPluginChecker(tt.spec.DisplayName, tt.spec)
			if err != nil {
				t.Fatalf("NewPluginChecker() error = %v", err)
			}

			ds := &fakeDataSource{items: tt.items}
			checkCtx := &checker.CheckContext{
				DataSource: ds,
				Namespaces: tt.namespaces,
			}

			result, err := pc.Check(context.Background(), checkCtx)
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}

			if len(result.Issues) != tt.wantIssues {
				t.Errorf("Check() issues = %d, want %d", len(result.Issues), tt.wantIssues)
				for _, issue := range result.Issues {
					t.Logf("  Issue: %s - %s", issue.Type, issue.Message)
				}
			}

			if result.Healthy != tt.wantHealth {
				t.Errorf("Check() healthy = %d, want %d", result.Healthy, tt.wantHealth)
			}
		})
	}
}

func TestPluginChecker_Check_IssueSeverity(t *testing.T) {
	spec := assistv1alpha1.CheckPluginSpec{
		DisplayName: "severity-test",
		TargetResource: assistv1alpha1.TargetGVR{
			Version:  "v1",
			Resource: "pods",
			Kind:     "Pod",
		},
		Rules: []assistv1alpha1.CheckRule{
			{
				Name:      "critical-rule",
				Condition: "true",
				Severity:  "Critical",
				Message:   "critical issue",
			},
		},
	}

	pc, err := NewPluginChecker("severity-test", spec)
	if err != nil {
		t.Fatalf("NewPluginChecker() error = %v", err)
	}

	ds := &fakeDataSource{
		items: []unstructured.Unstructured{
			makeUnstructured("", "Pod", "test-pod", map[string]any{}),
		},
	}

	result, err := pc.Check(context.Background(), &checker.CheckContext{
		DataSource: ds,
		Namespaces: []string{"default"},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if len(result.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(result.Issues))
	}

	if result.Issues[0].Severity != "Critical" {
		t.Errorf("issue severity = %q, want %q", result.Issues[0].Severity, "Critical")
	}

	if result.Issues[0].Namespace != "default" {
		t.Errorf("issue namespace = %q, want %q", result.Issues[0].Namespace, "default")
	}
}

// makeUnstructured creates an unstructured object for testing.
func makeUnstructured(group, kind, name string, extra map[string]any) unstructured.Unstructured {
	obj := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": schema.GroupVersion{Group: group, Version: "v1"}.String(),
			"kind":       kind,
			"metadata": map[string]any{
				"name":      name,
				"namespace": "default",
			},
		},
	}

	// Merge extra fields into the object
	maps.Copy(obj.Object, extra)

	return obj
}
