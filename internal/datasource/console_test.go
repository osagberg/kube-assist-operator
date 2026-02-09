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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// mustEncode writes v as JSON to w, failing the test on error.
func mustEncode(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("failed to encode response: %v", err)
	}
}

func TestConsoleDataSourceImplementsInterface(t *testing.T) {
	ds, err := NewConsole("http://localhost", "test-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	var _ DataSource = ds
}

func TestConsoleDataSourceGet(t *testing.T) {
	pod := &corev1.Pod{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/clusters/my-cluster/namespaces/default/pods/test-pod" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, pod)
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &corev1.Pod{}
	err = ds.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test-pod"}, got)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "test-pod" {
		t.Errorf("expected name test-pod, got %s", got.Name)
	}
	if got.Status.Phase != corev1.PodRunning {
		t.Errorf("expected phase Running, got %s", got.Status.Phase)
	}
}

func TestConsoleDataSourceGetNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"not found"}`))
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &corev1.Pod{}
	err = ds.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "missing"}, got)
	if err == nil {
		t.Fatal("expected error for missing object")
	}
	if !apierrors.IsNotFound(err) {
		t.Errorf("expected IsNotFound error, got: %v", err)
	}
}

func TestConsoleDataSourceGetForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"access denied"}`))
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &corev1.Pod{}
	err = ds.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "forbidden"}, got)
	if err == nil {
		t.Fatal("expected error for forbidden object")
	}
	if !apierrors.IsForbidden(err) {
		t.Errorf("expected IsForbidden error, got: %v", err)
	}
}

func TestConsoleDataSourceGetServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &corev1.Pod{}
	err = ds.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test"}, got)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestConsoleDataSourceList(t *testing.T) {
	podList := &corev1.PodList{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"},
		Items: []corev1.Pod{
			{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "pod-2", Namespace: "default"}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/clusters/my-cluster/namespaces/default/pods" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, podList)
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &corev1.PodList{}
	err = ds.List(context.Background(), got, client.InNamespace("default"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Items) != 2 {
		t.Errorf("expected 2 pods, got %d", len(got.Items))
	}
}

func TestConsoleDataSourceListClusterScoped(t *testing.T) {
	nsList := &corev1.NamespaceList{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "NamespaceList"},
		Items: []corev1.Namespace{
			{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/clusters/my-cluster/namespaces" {
			t.Errorf("unexpected path: %s, expected cluster-scoped path", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, nsList)
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &corev1.NamespaceList{}
	err = ds.List(context.Background(), got)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Items) != 2 {
		t.Errorf("expected 2 namespaces, got %d", len(got.Items))
	}
}

func TestConsoleDataSourceListWithLabelSelector(t *testing.T) {
	var capturedQuery string
	podList := &corev1.PodList{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"},
		Items: []corev1.Pod{
			{ObjectMeta: metav1.ObjectMeta{Name: "matching-pod", Namespace: "default"}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, podList)
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}

	selector, _ := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{"app": "web"},
	})

	got := &corev1.PodList{}
	err = ds.List(context.Background(), got,
		client.InNamespace("default"),
		client.MatchingLabelsSelector{Selector: selector})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedQuery == "" {
		t.Error("expected labelSelector query parameter")
	}
	// The query should contain labelSelector=app%3Dweb (URL-encoded "app=web")
	if capturedQuery != "labelSelector=app%3Dweb" {
		t.Errorf("unexpected query: %s", capturedQuery)
	}
}

func TestConsoleDataSourceListWithLimit(t *testing.T) {
	var capturedQuery string
	podList := &corev1.PodList{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, podList)
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &corev1.PodList{}
	err = ds.List(context.Background(), got, client.InNamespace("default"), client.Limit(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedQuery != "limit=1" {
		t.Errorf("expected limit=1 query param, got: %s", capturedQuery)
	}
}

func TestConsoleDataSourceBearerToken(t *testing.T) {
	var capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, &corev1.Pod{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
			ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		})
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster", WithBearerToken("secret-token"))
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &corev1.Pod{}
	err = ds.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test-pod"}, got)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedAuth != "Bearer secret-token" {
		t.Errorf("expected Bearer auth header, got: %s", capturedAuth)
	}
}

func TestConsoleDataSourceNoBearerToken(t *testing.T) {
	var capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, &corev1.Pod{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
			ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		})
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &corev1.Pod{}
	_ = ds.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test-pod"}, got)
	if capturedAuth != "" {
		t.Errorf("expected no auth header, got: %s", capturedAuth)
	}
}

func TestConsoleDataSourceTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster", WithHTTPClient(&http.Client{
		Timeout: 50 * time.Millisecond,
	}))
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &corev1.Pod{}
	err = ds.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test"}, got)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestConsoleDataSourceListDeployments(t *testing.T) {
	replicas := int32(3)
	deployList := &appsv1.DeploymentList{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "DeploymentList"},
		Items: []appsv1.Deployment{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "api-server", Namespace: "production"},
				Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
				Status:     appsv1.DeploymentStatus{ReadyReplicas: 3, Replicas: 3},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/clusters/prod/namespaces/production/deployments" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, deployList)
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "prod")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &appsv1.DeploymentList{}
	err = ds.List(context.Background(), got, client.InNamespace("production"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("expected 1 deployment, got %d", len(got.Items))
	}
	if got.Items[0].Name != "api-server" {
		t.Errorf("expected api-server, got %s", got.Items[0].Name)
	}
	if got.Items[0].Status.ReadyReplicas != 3 {
		t.Errorf("expected 3 ready replicas, got %d", got.Items[0].Status.ReadyReplicas)
	}
}

func TestConsoleDataSourceListStatefulSets(t *testing.T) {
	stsList := &appsv1.StatefulSetList{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSetList"},
		Items: []appsv1.StatefulSet{
			{ObjectMeta: metav1.ObjectMeta{Name: "redis", Namespace: "cache"}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, stsList)
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &appsv1.StatefulSetList{}
	err = ds.List(context.Background(), got, client.InNamespace("cache"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "redis" {
		t.Errorf("expected 1 statefulset named redis, got %v", got.Items)
	}
}

func TestConsoleDataSourceListDaemonSets(t *testing.T) {
	dsList := &appsv1.DaemonSetList{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "DaemonSetList"},
		Items: []appsv1.DaemonSet{
			{ObjectMeta: metav1.ObjectMeta{Name: "node-exporter", Namespace: "monitoring"}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, dsList)
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &appsv1.DaemonSetList{}
	err = ds.List(context.Background(), got, client.InNamespace("monitoring"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "node-exporter" {
		t.Errorf("expected 1 daemonset named node-exporter, got %v", got.Items)
	}
}

func TestConsoleDataSourceListSecrets(t *testing.T) {
	secretList := &corev1.SecretList{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "SecretList"},
		Items: []corev1.Secret{
			{ObjectMeta: metav1.ObjectMeta{Name: "tls-cert", Namespace: "default"}, Type: corev1.SecretTypeTLS},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, secretList)
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &corev1.SecretList{}
	err = ds.List(context.Background(), got, client.InNamespace("default"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Type != corev1.SecretTypeTLS {
		t.Errorf("expected 1 TLS secret, got %v", got.Items)
	}
}

func TestConsoleDataSourceListPVCs(t *testing.T) {
	pvcList := &corev1.PersistentVolumeClaimList{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolumeClaimList"},
		Items: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "data-vol", Namespace: "default"},
				Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, pvcList)
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &corev1.PersistentVolumeClaimList{}
	err = ds.List(context.Background(), got, client.InNamespace("default"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Status.Phase != corev1.ClaimBound {
		t.Errorf("expected 1 bound PVC, got %v", got.Items)
	}
}

func TestConsoleDataSourceListNetworkPolicies(t *testing.T) {
	npList := &networkingv1.NetworkPolicyList{
		TypeMeta: metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicyList"},
		Items: []networkingv1.NetworkPolicy{
			{ObjectMeta: metav1.ObjectMeta{Name: "deny-all", Namespace: "secure"}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, npList)
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &networkingv1.NetworkPolicyList{}
	err = ds.List(context.Background(), got, client.InNamespace("secure"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "deny-all" {
		t.Errorf("expected 1 network policy named deny-all, got %v", got.Items)
	}
}

func TestConsoleDataSourceListResourceQuotas(t *testing.T) {
	rqList := &corev1.ResourceQuotaList{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ResourceQuotaList"},
		Items: []corev1.ResourceQuota{
			{ObjectMeta: metav1.ObjectMeta{Name: "team-quota", Namespace: "team-a"}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, rqList)
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &corev1.ResourceQuotaList{}
	err = ds.List(context.Background(), got, client.InNamespace("team-a"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "team-quota" {
		t.Errorf("expected 1 resource quota, got %v", got.Items)
	}
}

func TestConsoleDataSourceListAllNamespaces(t *testing.T) {
	podList := &corev1.PodList{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"},
		Items: []corev1.Pod{
			{ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "ns-1"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "pod-b", Namespace: "ns-2"}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// When no namespace is specified, the path should be cluster-scoped for pods
		if r.URL.Path != "/api/v1/clusters/my-cluster/pods" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, podList)
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &corev1.PodList{}
	err = ds.List(context.Background(), got)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Items) != 2 {
		t.Errorf("expected 2 pods across all namespaces, got %d", len(got.Items))
	}
}

func TestConsoleDataSourceGetClusterScoped(t *testing.T) {
	ns := &corev1.Namespace{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
		ObjectMeta: metav1.ObjectMeta{Name: "production"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Namespace is cluster-scoped; path should NOT include /namespaces/
		if r.URL.Path != "/api/v1/clusters/my-cluster/namespaces/production" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, ns)
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &corev1.Namespace{}
	err = ds.Get(context.Background(), client.ObjectKey{Name: "production"}, got)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "production" {
		t.Errorf("expected namespace production, got %s", got.Name)
	}
}

func TestConsoleDataSourceAcceptHeader(t *testing.T) {
	var capturedAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, &corev1.PodList{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"},
		})
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	_ = ds.List(context.Background(), &corev1.PodList{}, client.InNamespace("default"))
	if capturedAccept != "application/json" {
		t.Errorf("expected Accept: application/json, got: %s", capturedAccept)
	}
}

func TestConsoleDataSourceCustomHTTPClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, &corev1.Pod{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		})
	}))
	defer srv.Close()

	customClient := &http.Client{Timeout: 5 * time.Second}
	ds, err := NewConsole(srv.URL, "my-cluster", WithHTTPClient(customClient))
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}

	got := &corev1.Pod{}
	err = ds.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test"}, got)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConsoleDataSourceBaseURLTrailingSlash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should not have double slashes
		if r.URL.Path[:2] == "//" {
			t.Errorf("double slash in path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, &corev1.PodList{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"},
		})
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL+"/", "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	_ = ds.List(context.Background(), &corev1.PodList{}, client.InNamespace("default"))
}

// TestConsoleDataSourceMultiResourceScenario simulates a realistic checker
// workflow: listing deployments, then pods by label selector for each.
func TestConsoleDataSourceMultiResourceScenario(t *testing.T) {
	replicas := int32(2)
	responses := map[string]any{
		"/api/v1/clusters/prod/namespaces/app/deployments": &appsv1.DeploymentList{
			TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "DeploymentList"},
			Items: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "app"},
					Spec: appsv1.DeploymentSpec{
						Replicas: &replicas,
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "web"},
						},
					},
					Status: appsv1.DeploymentStatus{ReadyReplicas: 1, Replicas: 2},
				},
			},
		},
		"/api/v1/clusters/prod/namespaces/app/pods": &corev1.PodList{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"},
			Items: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "web-abc-123", Namespace: "app", Labels: map[string]string{"app": "web"}},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{Name: "web", Ready: true, RestartCount: 0},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "web-abc-456", Namespace: "app", Labels: map[string]string{"app": "web"}},
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:  "web",
								Ready: false,
								State: corev1.ContainerState{
									Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
								},
								RestartCount: 5,
							},
						},
					},
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, ok := responses[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, resp)
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "prod")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}

	// Step 1: List deployments (like workload checker does)
	deploys := &appsv1.DeploymentList{}
	err = ds.List(context.Background(), deploys, client.InNamespace("app"))
	if err != nil {
		t.Fatalf("list deployments: %v", err)
	}
	if len(deploys.Items) != 1 {
		t.Fatalf("expected 1 deployment, got %d", len(deploys.Items))
	}

	// Step 2: List pods with label selector (like workload checker does)
	selector, _ := metav1.LabelSelectorAsSelector(deploys.Items[0].Spec.Selector)
	pods := &corev1.PodList{}
	err = ds.List(context.Background(), pods,
		client.InNamespace("app"),
		client.MatchingLabelsSelector{Selector: selector})
	if err != nil {
		t.Fatalf("list pods: %v", err)
	}
	if len(pods.Items) != 2 {
		t.Fatalf("expected 2 pods, got %d", len(pods.Items))
	}

	// Verify the problematic pod data came through correctly
	crashingPod := pods.Items[1]
	if crashingPod.Status.ContainerStatuses[0].State.Waiting == nil {
		t.Error("expected waiting state on crashing pod")
	}
	if crashingPod.Status.ContainerStatuses[0].State.Waiting.Reason != "CrashLoopBackOff" {
		t.Errorf("expected CrashLoopBackOff, got %s", crashingPod.Status.ContainerStatuses[0].State.Waiting.Reason)
	}
	if crashingPod.Status.ContainerStatuses[0].RestartCount != 5 {
		t.Errorf("expected restart count 5, got %d", crashingPod.Status.ContainerStatuses[0].RestartCount)
	}
}

func TestConsoleDataSourceMalformedJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Return truncated/malformed JSON — 200 OK but body can't be decoded.
		_, _ = w.Write([]byte(`{"metadata":{"name":"broken`))
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &corev1.Pod{}
	err = ds.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "broken"}, got)
	if err == nil {
		t.Fatal("expected decode error for malformed JSON, got nil")
	}
	// The error should come from json.Decoder, not from HTTP status handling.
	if strings.Contains(err.Error(), "HTTP") {
		t.Errorf("expected JSON decode error, got HTTP error: %v", err)
	}
}

func TestConsoleDataSourceContextCancellation(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		// Block until the test ends; context cancellation should abort this.
		time.Sleep(10 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	ds, err := NewConsole(srv.URL, "my-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- ds.Get(ctx, client.ObjectKey{Namespace: "default", Name: "test"}, &corev1.Pod{})
	}()

	<-started
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error after context cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cancelled request to return")
	}
}

func TestValidateConsoleURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"valid https", "https://console.example.com", false},
		{"valid http", "http://localhost:8085", false},
		{"missing scheme", "console.example.com", true},
		{"missing host", "https://", true},
		{"not a url", "not a url", true},
		{"empty", "", true},
		{"ftp scheme", "ftp://example.com", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConsoleURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConsoleURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestValidateClusterID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"simple", "my-cluster", false},
		{"alphanumeric", "cluster123", false},
		{"with dots", "prod.us-east-1", false},
		{"with underscores", "my_cluster", false},
		{"spaces", "my cluster", true},
		{"slashes", "my/cluster", true},
		{"empty", "", true},
		{"starts with hyphen", "-cluster", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateClusterID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateClusterID(%q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
			}
		})
	}
}

func TestNewConsoleValidation(t *testing.T) {
	tests := []struct {
		name      string
		baseURL   string
		clusterID string
		wantErr   bool
	}{
		{"valid", "http://localhost:8080", "my-cluster", false},
		{"empty URL", "", "", true},
		{"invalid URL scheme", "ftp://example.com", "my-cluster", true},
		{"empty cluster ID", "http://localhost", "", true},
		{"invalid cluster ID", "http://localhost", "bad cluster!!", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewConsole(tt.baseURL, tt.clusterID)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewConsole() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConsoleDataSourceDefaultTimeout(t *testing.T) {
	// Verify that the default client has a non-zero timeout.
	ds, err := NewConsole("http://localhost", "test-cluster")
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	if ds.httpClient.Timeout == 0 {
		t.Error("expected default HTTP client to have non-zero timeout")
	}
	if ds.httpClient.Timeout != 30*time.Second {
		t.Errorf("expected 30s default timeout, got %v", ds.httpClient.Timeout)
	}
}

func TestDoRequest_RefusesBearerOverHTTP(t *testing.T) {
	ds, err := NewConsole("http://remote-host.example.com:8085", "test-cluster", WithBearerToken("secret"))
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &corev1.Pod{}
	err = ds.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test-pod"}, got)
	if err == nil {
		t.Fatal("expected error when sending bearer token over HTTP to non-localhost")
	}
	if !strings.Contains(err.Error(), "refusing to send bearer token over insecure connection") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDoRequest_AllowsBearerOverHTTPS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("expected bearer token, got: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, &corev1.Pod{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
			ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		})
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "test-cluster", WithBearerToken("secret"), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &corev1.Pod{}
	err = ds.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test-pod"}, got)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDoRequest_AllowsBearerToLocalhost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("expected bearer token, got: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, &corev1.Pod{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
			ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		})
	}))
	defer srv.Close()

	ds, err := NewConsole(srv.URL, "test-cluster", WithBearerToken("secret"))
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	got := &corev1.Pod{}
	err = ds.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test-pod"}, got)
	if err != nil {
		t.Fatalf("unexpected error with localhost: %v", err)
	}
}
