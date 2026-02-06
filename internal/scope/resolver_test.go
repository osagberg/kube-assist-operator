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

package scope

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
)

func TestResolver_ResolveNamespaces_ExplicitList(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	// Create test namespaces
	ns1 := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1"}}
	ns2 := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns2"}}
	ns3 := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns3"}}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ns1, ns2, ns3).
		Build()

	resolver := NewResolver(datasource.NewKubernetes(client), "default")

	scope := assistv1alpha1.ScopeConfig{
		Namespaces: []string{"ns1", "ns2", "nonexistent"},
	}

	namespaces, err := resolver.ResolveNamespaces(context.Background(), scope)
	if err != nil {
		t.Fatalf("ResolveNamespaces() error = %v", err)
	}

	// Should have ns1 and ns2, not nonexistent
	if len(namespaces) != 2 {
		t.Errorf("ResolveNamespaces() = %d namespaces, want 2", len(namespaces))
	}

	// Should be sorted
	if namespaces[0] != "ns1" || namespaces[1] != "ns2" {
		t.Errorf("ResolveNamespaces() = %v, want [ns1, ns2]", namespaces)
	}
}

func TestResolver_ResolveNamespaces_LabelSelector(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	// Create test namespaces with labels
	ns1 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "team-a",
			Labels: map[string]string{"team": "a"},
		},
	}
	ns2 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "team-b",
			Labels: map[string]string{"team": "b"},
		},
	}
	ns3 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "team-a-staging",
			Labels: map[string]string{"team": "a"},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ns1, ns2, ns3).
		Build()

	resolver := NewResolver(datasource.NewKubernetes(client), "default")

	scope := assistv1alpha1.ScopeConfig{
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"team": "a"},
		},
	}

	namespaces, err := resolver.ResolveNamespaces(context.Background(), scope)
	if err != nil {
		t.Fatalf("ResolveNamespaces() error = %v", err)
	}

	// Should have team-a and team-a-staging
	if len(namespaces) != 2 {
		t.Errorf("ResolveNamespaces() = %d namespaces, want 2", len(namespaces))
	}
}

func TestResolver_ResolveNamespaces_CurrentNamespaceOnly(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	resolver := NewResolver(datasource.NewKubernetes(client), "my-namespace")

	scope := assistv1alpha1.ScopeConfig{
		CurrentNamespaceOnly: true,
	}

	namespaces, err := resolver.ResolveNamespaces(context.Background(), scope)
	if err != nil {
		t.Fatalf("ResolveNamespaces() error = %v", err)
	}

	if len(namespaces) != 1 || namespaces[0] != "my-namespace" {
		t.Errorf("ResolveNamespaces() = %v, want [my-namespace]", namespaces)
	}
}

func TestResolver_ResolveNamespaces_EmptyScope(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	resolver := NewResolver(datasource.NewKubernetes(client), "default-ns")

	scope := assistv1alpha1.ScopeConfig{}

	namespaces, err := resolver.ResolveNamespaces(context.Background(), scope)
	if err != nil {
		t.Fatalf("ResolveNamespaces() error = %v", err)
	}

	// Should default to current namespace
	if len(namespaces) != 1 || namespaces[0] != "default-ns" {
		t.Errorf("ResolveNamespaces() = %v, want [default-ns]", namespaces)
	}
}

func TestResolver_ResolveNamespaces_SkipsTerminating(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	ns1 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "active",
			Labels: map[string]string{"include": "true"},
		},
		Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
	}
	ns2 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "terminating",
			Labels: map[string]string{"include": "true"},
		},
		Status: corev1.NamespaceStatus{Phase: corev1.NamespaceTerminating},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ns1, ns2).
		Build()

	resolver := NewResolver(datasource.NewKubernetes(client), "default")

	scope := assistv1alpha1.ScopeConfig{
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"include": "true"},
		},
	}

	namespaces, err := resolver.ResolveNamespaces(context.Background(), scope)
	if err != nil {
		t.Fatalf("ResolveNamespaces() error = %v", err)
	}

	// Should only have active, not terminating
	if len(namespaces) != 1 || namespaces[0] != "active" {
		t.Errorf("ResolveNamespaces() = %v, want [active]", namespaces)
	}
}

func TestFilterSystemNamespaces(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "filters system namespaces",
			input: []string{"kube-system", "default", "kube-public", "my-app"},
			want:  []string{"default", "my-app"},
		},
		{
			name:  "empty input",
			input: []string{},
			want:  []string{},
		},
		{
			name:  "all system",
			input: []string{"kube-system", "flux-system"},
			want:  []string{},
		},
		{
			name:  "no system",
			input: []string{"app1", "app2"},
			want:  []string{"app1", "app2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterSystemNamespaces(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("FilterSystemNamespaces() = %v, want %v", got, tt.want)
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("FilterSystemNamespaces()[%d] = %v, want %v", i, v, tt.want[i])
				}
			}
		})
	}
}

func TestResolver_Priority(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	ns1 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "ns1",
			Labels: map[string]string{"env": "prod"},
		},
	}
	ns2 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "ns2",
			Labels: map[string]string{"env": "prod"},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ns1, ns2).
		Build()

	resolver := NewResolver(datasource.NewKubernetes(client), "default")

	// Test that explicit list takes priority over selector
	scope := assistv1alpha1.ScopeConfig{
		Namespaces: []string{"ns1"}, // Should take priority
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"env": "prod"},
		},
	}

	namespaces, err := resolver.ResolveNamespaces(context.Background(), scope)
	if err != nil {
		t.Fatalf("ResolveNamespaces() error = %v", err)
	}

	// Should only have ns1 from explicit list, not ns2 from selector
	if len(namespaces) != 1 || namespaces[0] != "ns1" {
		t.Errorf("ResolveNamespaces() = %v, want [ns1]", namespaces)
	}
}
