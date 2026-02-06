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

package datasource

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestKubernetesDataSourceImplementsInterface(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	// Compile-time assertion: *KubernetesDataSource implements DataSource
	var _ DataSource = NewKubernetes(cl)
	_ = t // test passes if it compiles
}

func TestKubernetesDataSourceGet(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	ds := NewKubernetes(cl)

	got := &corev1.Pod{}
	err := ds.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test-pod"}, got)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "test-pod" {
		t.Errorf("expected name test-pod, got %s", got.Name)
	}
}

func TestKubernetesDataSourceGetNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	ds := NewKubernetes(cl)

	got := &corev1.Pod{}
	err := ds.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "missing"}, got)
	if err == nil {
		t.Fatal("expected error for missing object")
	}
}

func TestKubernetesDataSourceList(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	pods := []client.Object{
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-2", Namespace: "default"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-3", Namespace: "other"}},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pods...).Build()
	ds := NewKubernetes(cl)

	var podList corev1.PodList
	err := ds.List(context.Background(), &podList, client.InNamespace("default"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(podList.Items) != 2 {
		t.Errorf("expected 2 pods in default namespace, got %d", len(podList.Items))
	}
}

func TestKubernetesDataSourceListAll(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	pods := []client.Object{
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-2", Namespace: "other"}},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pods...).Build()
	ds := NewKubernetes(cl)

	var podList corev1.PodList
	err := ds.List(context.Background(), &podList)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(podList.Items) != 2 {
		t.Errorf("expected 2 pods total, got %d", len(podList.Items))
	}
}
