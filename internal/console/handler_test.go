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

package console

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	"github.com/osagberg/kube-assist-operator/internal/datasource"
)

// newTestHandler builds a Handler with fake clients for the given clusters.
// Each cluster gets a fake client seeded with the provided objects.
func newTestHandler(t *testing.T, clusters map[string][]client.Object) *http.ServeMux {
	t.Helper()
	s := datasource.NewConsoleScheme()
	readers := make(map[string]client.Reader, len(clusters))
	for id, objs := range clusters {
		builder := fake.NewClientBuilder().WithScheme(s)
		if len(objs) > 0 {
			builder = builder.WithObjects(objs...)
		}
		readers[id] = builder.Build()
	}
	agg := &Aggregator{clusters: readers, scheme: s}
	h := NewHandler(agg)
	mux := http.NewServeMux()
	RegisterRoutes(mux, h)
	return mux
}

// ---- handleListClusters ----

func TestHandleListClusters(t *testing.T) {
	mux := newTestHandler(t, map[string][]client.Object{
		"cluster-a": nil,
		"cluster-b": nil,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var body struct {
		Clusters []string `json:"clusters"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if len(body.Clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(body.Clusters))
	}
	if body.Clusters[0] != "cluster-a" || body.Clusters[1] != "cluster-b" {
		t.Errorf("unexpected clusters: %v", body.Clusters)
	}
}

// ---- handleResource path parsing ----

func TestHandleResourceInvalidPath(t *testing.T) {
	mux := newTestHandler(t, map[string][]client.Object{"c1": nil})

	tests := []struct {
		name string
		path string
		code int
	}{
		{
			name: "too few segments",
			path: "/api/v1/clusters/c1",
			code: http.StatusBadRequest,
		},
		{
			name: "only cluster ID",
			path: "/api/v1/clusters/c1/",
			code: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.code {
				t.Errorf("path %s: expected %d, got %d", tt.path, tt.code, rec.Code)
			}
		})
	}
}

func TestHandleResourceUnknownCluster(t *testing.T) {
	mux := newTestHandler(t, map[string][]client.Object{"c1": nil})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/unknown/pods", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body["error"] == "" {
		t.Error("expected error message in body")
	}
}

func TestHandleResourceUnknownResource(t *testing.T) {
	mux := newTestHandler(t, map[string][]client.Object{"c1": nil})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/c1/foobar", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown resource, got %d", rec.Code)
	}
}

// ---- handleGet with actual K8s objects ----

func TestHandleGetPodNamespaced(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pod", Namespace: "default"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}

	mux := newTestHandler(t, map[string][]client.Object{
		"c1": {pod},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/c1/namespaces/default/pods/my-pod", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got corev1.Pod
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if got.Name != "my-pod" {
		t.Errorf("expected name my-pod, got %s", got.Name)
	}
	if got.Status.Phase != corev1.PodRunning {
		t.Errorf("expected phase Running, got %s", got.Status.Phase)
	}
}

func TestHandleGetDeploymentNamespaced(t *testing.T) {
	replicas := int32(3)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
	}

	mux := newTestHandler(t, map[string][]client.Object{
		"c1": {dep},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/c1/namespaces/prod/deployments/web", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got appsv1.Deployment
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if got.Name != "web" {
		t.Errorf("expected name web, got %s", got.Name)
	}
}

func TestHandleGetSecret_RedactsSensitiveData(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "api-key", Namespace: "default"},
		Data: map[string][]byte{
			"token": []byte("super-secret"),
		},
	}

	mux := newTestHandler(t, map[string][]client.Object{
		"c1": {secret},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/c1/namespaces/default/secrets/api-key", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got corev1.Secret
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if got.Data != nil {
		t.Errorf("expected secret data to be redacted, got keys: %v", got.Data)
	}
}

func TestHandleGetNotFound(t *testing.T) {
	mux := newTestHandler(t, map[string][]client.Object{"c1": nil})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/c1/namespaces/default/pods/nonexistent", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestHandleGetClusterScoped(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "kube-system"},
	}

	mux := newTestHandler(t, map[string][]client.Object{
		"c1": {ns},
	})

	// Path /api/v1/clusters/c1/namespaces/kube-system
	// After stripping prefix: "c1/namespaces/kube-system"
	// parts = ["c1", "namespaces", "kube-system"], rest = ["namespaces", "kube-system"]
	// Since len(rest) < 3 || rest[0] == "namespaces" but len(rest) == 2,
	// falls to cluster-scoped branch: resource="namespaces", name="kube-system"
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/c1/namespaces/kube-system", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---- handleList with actual K8s objects ----

func TestHandleListPodsNamespaced(t *testing.T) {
	pods := []client.Object{
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-2", Namespace: "default"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-3", Namespace: "other"}},
	}

	mux := newTestHandler(t, map[string][]client.Object{
		"c1": pods,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/c1/namespaces/default/pods", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got corev1.PodList
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(got.Items) != 2 {
		t.Errorf("expected 2 pods in default ns, got %d", len(got.Items))
	}
}

func TestHandleListDeployments(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "staging"},
	}

	mux := newTestHandler(t, map[string][]client.Object{
		"c1": {dep},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/c1/namespaces/staging/deployments", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var got appsv1.DeploymentList
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "api" {
		t.Errorf("expected 1 deployment named api, got %v", got.Items)
	}
}

func TestHandleListSecrets_RedactsSensitiveData(t *testing.T) {
	secrets := []client.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "one", Namespace: "default"},
			Data: map[string][]byte{
				"password": []byte("secret-1"),
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "two", Namespace: "default"},
			Data: map[string][]byte{
				"password": []byte("secret-2"),
			},
		},
	}

	mux := newTestHandler(t, map[string][]client.Object{
		"c1": secrets,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/c1/namespaces/default/secrets", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got corev1.SecretList
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(got.Items))
	}
	for _, item := range got.Items {
		if item.Data != nil {
			t.Errorf("expected secret %q data to be redacted", item.Name)
		}
	}
}

// ---- buildListOptions ----

func TestBuildListOptions(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		namespace string
		wantCount int
		wantErr   bool
	}{
		{
			name:      "no params no namespace",
			path:      "/test",
			namespace: "",
			wantCount: 0,
		},
		{
			name:      "namespace only",
			path:      "/test",
			namespace: "default",
			wantCount: 1,
		},
		{
			name:      "labelSelector only",
			path:      "/test?labelSelector=app%3Dweb",
			namespace: "",
			wantCount: 1,
		},
		{
			name:      "limit only",
			path:      "/test?limit=10",
			namespace: "",
			wantCount: 1,
		},
		{
			name:      "namespace and labelSelector and limit",
			path:      "/test?labelSelector=app%3Dweb&limit=5",
			namespace: "prod",
			wantCount: 3,
		},
		{
			name:      "invalid labelSelector returns error",
			path:      "/test?labelSelector=!!!invalid",
			namespace: "",
			wantErr:   true,
		},
		{
			name:      "invalid limit is ignored",
			path:      "/test?limit=notanumber",
			namespace: "",
			wantCount: 0,
		},
		{
			name:      "zero limit is ignored",
			path:      "/test?limit=0",
			namespace: "",
			wantCount: 0,
		},
		{
			name:      "negative limit is ignored",
			path:      "/test?limit=-5",
			namespace: "",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			opts, err := buildListOptions(req, tt.namespace)
			if tt.wantErr {
				if err == nil {
					t.Error("buildListOptions() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("buildListOptions() unexpected error: %v", err)
			}
			if len(opts) != tt.wantCount {
				t.Errorf("buildListOptions() returned %d opts, want %d", len(opts), tt.wantCount)
			}
		})
	}
}

// ---- newObjectForResource / newListForResource ----

func TestNewObjectForResource(t *testing.T) {
	tests := []struct {
		resource string
		wantErr  bool
	}{
		{"pods", false},
		{"services", false},
		{"secrets", false},
		{"persistentvolumeclaims", false},
		{"resourcequotas", false},
		{"namespaces", false},
		{"events", false},
		{"deployments", false},
		{"statefulsets", false},
		{"daemonsets", false},
		{"replicasets", false},
		{"networkpolicies", false},
		{"helmreleases", false},
		{"kustomizations", false},
		{"gitrepositories", false},
		{"unknown", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.resource, func(t *testing.T) {
			obj, err := newObjectForResource(tt.resource)
			if (err != nil) != tt.wantErr {
				t.Errorf("newObjectForResource(%q) error = %v, wantErr %v", tt.resource, err, tt.wantErr)
				return
			}
			if !tt.wantErr && obj == nil {
				t.Errorf("newObjectForResource(%q) returned nil object", tt.resource)
			}
		})
	}
}

func TestNewListForResource(t *testing.T) {
	tests := []struct {
		resource string
		wantErr  bool
	}{
		{"pods", false},
		{"services", false},
		{"secrets", false},
		{"persistentvolumeclaims", false},
		{"resourcequotas", false},
		{"namespaces", false},
		{"events", false},
		{"deployments", false},
		{"statefulsets", false},
		{"daemonsets", false},
		{"replicasets", false},
		{"networkpolicies", false},
		{"helmreleases", false},
		{"kustomizations", false},
		{"gitrepositories", false},
		{"unknown", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.resource, func(t *testing.T) {
			list, err := newListForResource(tt.resource)
			if (err != nil) != tt.wantErr {
				t.Errorf("newListForResource(%q) error = %v, wantErr %v", tt.resource, err, tt.wantErr)
				return
			}
			if !tt.wantErr && list == nil {
				t.Errorf("newListForResource(%q) returned nil list", tt.resource)
			}
		})
	}
}

// ---- writeJSON / writeJSONError / writeK8sError ----

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]string{"msg": "hello"})

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body["msg"] != "hello" {
		t.Errorf("expected msg=hello, got %s", body["msg"])
	}
}

func TestWriteJSONError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSONError(rec, http.StatusBadRequest, "bad input")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body["error"] != "bad input" {
		t.Errorf("expected error=bad input, got %s", body["error"])
	}
}

func TestWriteK8sErrorNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	err := apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "pods"}, "test-pod")
	writeK8sError(rec, err)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestWriteK8sErrorForbidden(t *testing.T) {
	rec := httptest.NewRecorder()
	err := apierrors.NewForbidden(schema.GroupResource{Group: "", Resource: "pods"}, "test-pod", fmt.Errorf("access denied"))
	writeK8sError(rec, err)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestWriteK8sErrorGeneric(t *testing.T) {
	rec := httptest.NewRecorder()
	err := fmt.Errorf("something went wrong")
	writeK8sError(rec, err)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}

	var body map[string]string
	if decErr := json.NewDecoder(rec.Body).Decode(&body); decErr != nil {
		t.Fatalf("decode error: %v", decErr)
	}
	if body["error"] != "internal server error" {
		t.Errorf("expected 'internal server error', got %s", body["error"])
	}
}

// ---- handleList with labelSelector query param ----

func TestHandleListWithLabelSelector(t *testing.T) {
	pods := []client.Object{
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name: "matching", Namespace: "default",
			Labels: map[string]string{"app": "web"},
		}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name: "not-matching", Namespace: "default",
			Labels: map[string]string{"app": "api"},
		}},
	}

	mux := newTestHandler(t, map[string][]client.Object{
		"c1": pods,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/c1/namespaces/default/pods?labelSelector=app%3Dweb", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got corev1.PodList
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("expected 1 pod matching label, got %d", len(got.Items))
	}
	if got.Items[0].Name != "matching" {
		t.Errorf("expected matching pod, got %s", got.Items[0].Name)
	}
}

// ---- handleList with limit ----

func TestHandleListWithLimit(t *testing.T) {
	pods := []client.Object{
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-2", Namespace: "default"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-3", Namespace: "default"}},
	}

	mux := newTestHandler(t, map[string][]client.Object{
		"c1": pods,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/c1/namespaces/default/pods?limit=2", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got corev1.PodList
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	// The fake client may or may not honor limit, but the handler should not error.
	if len(got.Items) == 0 {
		t.Error("expected at least some pods returned")
	}
}

// ---- healthz endpoint ----

func TestHealthz(t *testing.T) {
	mux := newTestHandler(t, map[string][]client.Object{"c1": nil})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("expected body ok, got %s", rec.Body.String())
	}
}

// ---- handleGet / handleList for multiple resource types ----

func TestHandleListServices(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "my-svc", Namespace: "default"},
	}
	mux := newTestHandler(t, map[string][]client.Object{"c1": {svc}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/c1/namespaces/default/services", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var got corev1.ServiceList
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "my-svc" {
		t.Errorf("expected 1 service named my-svc, got %v", got.Items)
	}
}

func TestHandleGetService(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "my-svc", Namespace: "default"},
	}
	mux := newTestHandler(t, map[string][]client.Object{"c1": {svc}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/c1/namespaces/default/services/my-svc", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got corev1.Service
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if got.Name != "my-svc" {
		t.Errorf("expected my-svc, got %s", got.Name)
	}
}

func TestHandleListStatefulSets(t *testing.T) {
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "redis", Namespace: "cache"},
	}
	mux := newTestHandler(t, map[string][]client.Object{"c1": {sts}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/c1/namespaces/cache/statefulsets", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var got appsv1.StatefulSetList
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "redis" {
		t.Errorf("expected 1 statefulset named redis, got %v", got.Items)
	}
}

func TestHandleListDaemonSets(t *testing.T) {
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "node-exporter", Namespace: "monitoring"},
	}
	mux := newTestHandler(t, map[string][]client.Object{"c1": {ds}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/c1/namespaces/monitoring/daemonsets", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var got appsv1.DaemonSetList
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "node-exporter" {
		t.Errorf("expected 1 daemonset named node-exporter, got %v", got.Items)
	}
}

// ---- multi-cluster test ----

func TestHandleResourceMultiCluster(t *testing.T) {
	mux := newTestHandler(t, map[string][]client.Object{
		"cluster-a": {
			&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "default"}},
		},
		"cluster-b": {
			&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-b", Namespace: "default"}},
		},
	})

	// Get from cluster-a
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/cluster-a/namespaces/default/pods/pod-a", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from cluster-a, got %d", rec.Code)
	}

	var gotA corev1.Pod
	if err := json.NewDecoder(rec.Body).Decode(&gotA); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if gotA.Name != "pod-a" {
		t.Errorf("expected pod-a, got %s", gotA.Name)
	}

	// Get from cluster-b
	req = httptest.NewRequest(http.MethodGet, "/api/v1/clusters/cluster-b/namespaces/default/pods/pod-b", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from cluster-b, got %d", rec.Code)
	}

	var gotB corev1.Pod
	if err := json.NewDecoder(rec.Body).Decode(&gotB); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if gotB.Name != "pod-b" {
		t.Errorf("expected pod-b, got %s", gotB.Name)
	}

	// pod-a should NOT exist in cluster-b
	req = httptest.NewRequest(http.MethodGet, "/api/v1/clusters/cluster-b/namespaces/default/pods/pod-a", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for pod-a in cluster-b, got %d", rec.Code)
	}
}

// ---- Flux resource types via handler ----

func TestHandleListHelmReleases(t *testing.T) {
	hr := &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{Name: "nginx", Namespace: "flux-system"},
	}
	mux := newTestHandler(t, map[string][]client.Object{"c1": {hr}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/c1/namespaces/flux-system/helmreleases", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got helmv2.HelmReleaseList
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "nginx" {
		t.Errorf("expected 1 helmrelease named nginx, got %v", got.Items)
	}
}

func TestHandleListKustomizations(t *testing.T) {
	ks := &kustomizev1.Kustomization{
		ObjectMeta: metav1.ObjectMeta{Name: "infra", Namespace: "flux-system"},
	}
	mux := newTestHandler(t, map[string][]client.Object{"c1": {ks}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/c1/namespaces/flux-system/kustomizations", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got kustomizev1.KustomizationList
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "infra" {
		t.Errorf("expected 1 kustomization named infra, got %v", got.Items)
	}
}

func TestHandleListGitRepositories(t *testing.T) {
	repo := &sourcev1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-repo", Namespace: "flux-system"},
	}
	mux := newTestHandler(t, map[string][]client.Object{"c1": {repo}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/c1/namespaces/flux-system/gitrepositories", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got sourcev1.GitRepositoryList
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "cluster-repo" {
		t.Errorf("expected 1 gitrepository named cluster-repo, got %v", got.Items)
	}
}

// ---- AuthMiddleware ----

func TestAuthMiddleware_SkipsWhenNoToken(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := AuthMiddleware("", inner)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if !called {
		t.Error("expected inner handler to be called when no token configured")
	}
}

func TestAuthMiddleware_RejectsUnauthenticated(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("inner handler should not be called")
	})
	handler := AuthMiddleware("my-secret", inner)

	tests := []struct {
		name       string
		authHeader string
		wantCode   int
	}{
		{"no header", "", http.StatusUnauthorized},
		{"empty bearer", "Bearer ", http.StatusForbidden},
		{"wrong token", "Bearer wrong-token", http.StatusForbidden},
		{"basic auth", "Basic dXNlcjpwYXNz", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Errorf("expected %d, got %d", tt.wantCode, rec.Code)
			}
		})
	}
}

func TestAuthMiddleware_AcceptsValidToken(t *testing.T) {
	const token = "super-secret-token-123"
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := AuthMiddleware(token, inner)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if !called {
		t.Error("expected inner handler to be called for valid token")
	}
}

func TestSecurityHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := SecurityHeaders(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	checks := map[string]string{
		"Content-Security-Policy": "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
		"Permissions-Policy":      "camera=(), microphone=(), geolocation=()",
	}
	for header, want := range checks {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}
