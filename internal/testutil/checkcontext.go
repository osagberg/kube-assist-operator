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

// Package testutil provides helpers for checker and dashboard tests.
package testutil

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
)

// NewScheme creates a scheme with all types needed by checkers.
func NewScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		appsv1.AddToScheme,
		networkingv1.AddToScheme,
		helmv2.AddToScheme,
		kustomizev1.AddToScheme,
		sourcev1.AddToScheme,
	} {
		if err := add(s); err != nil {
			t.Fatalf("testutil: failed to register scheme: %v", err)
		}
	}
	return s
}

// NewDataSource creates a DataSource backed by a fake client wrapping the given objects.
func NewDataSource(t *testing.T, objects ...client.Object) datasource.DataSource {
	t.Helper()
	scheme := NewScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	return datasource.NewKubernetes(c)
}

// NewCheckContext creates a CheckContext with a fake client wrapping the given objects.
func NewCheckContext(t *testing.T, namespaces []string, objects ...client.Object) *checker.CheckContext {
	t.Helper()
	return &checker.CheckContext{
		DataSource: NewDataSource(t, objects...),
		Namespaces: namespaces,
	}
}

// NewCheckContextWithConfig creates a CheckContext with checker config.
func NewCheckContextWithConfig(t *testing.T, namespaces []string, config map[string]any, objects ...client.Object) *checker.CheckContext {
	t.Helper()
	return &checker.CheckContext{
		DataSource: NewDataSource(t, objects...),
		Namespaces: namespaces,
		Config:     config,
	}
}
