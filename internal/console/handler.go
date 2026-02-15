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
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
)

const pathSegmentNamespaces = "namespaces"

// Handler serves Kubernetes resource data from the Aggregator over HTTP.
type Handler struct {
	agg *Aggregator
}

// NewHandler creates an HTTP handler backed by the given Aggregator.
func NewHandler(agg *Aggregator) *Handler {
	return &Handler{agg: agg}
}

// RegisterRoutes sets up routes on the given mux.
// API pattern: /api/v1/clusters/{cluster}/namespaces/{ns}/{resource}[/{name}]
//
//	/api/v1/clusters/{cluster}/{resource}[/{name}]
//	/api/v1/clusters
func RegisterRoutes(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("/api/v1/clusters", h.handleListClusters)
	mux.HandleFunc("/api/v1/clusters/", h.handleResource)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func (h *Handler) handleListClusters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"clusters": h.agg.ClusterIDs(),
	})
}

// handleResource dispatches to Get or List based on the URL path segments.
// Path formats:
//
//	/api/v1/clusters/{cluster}/{resource}                         → List (cluster-scoped)
//	/api/v1/clusters/{cluster}/{resource}/{name}                  → Get (cluster-scoped)
//	/api/v1/clusters/{cluster}/namespaces/{ns}/{resource}         → List (namespaced)
//	/api/v1/clusters/{cluster}/namespaces/{ns}/{resource}/{name}  → Get (namespaced)
func (h *Handler) handleResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Strip prefix to get: {cluster}/...
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/clusters/")
	parts := strings.Split(strings.TrimRight(path, "/"), "/")

	if len(parts) < 2 {
		writeJSONError(w, http.StatusBadRequest, "invalid path")
		return
	}

	clusterID := parts[0]
	reader, ok := h.agg.GetReader(clusterID)
	if !ok {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("unknown cluster: %s", clusterID))
		return
	}

	var namespace, resource, name string

	rest := parts[1:]
	if len(rest) >= 3 && rest[0] == pathSegmentNamespaces {
		// /namespaces/{ns}/{resource}[/{name}]
		namespace = rest[1]
		resource = rest[2]
		if len(rest) >= 4 {
			name = rest[3]
		}
	} else {
		// /{resource}[/{name}]
		resource = rest[0]
		if len(rest) >= 2 {
			name = rest[1]
		}
	}

	if name != "" {
		h.handleGet(w, r, reader, namespace, resource, name)
	} else {
		h.handleList(w, r, reader, namespace, resource)
	}
}

func (h *Handler) handleGet(w http.ResponseWriter, r *http.Request, reader client.Reader, namespace, resource, name string) {
	slog.Info("handleGet", "resource", resource, "namespace", namespace, "name", name, "remote", r.RemoteAddr)

	obj, err := newObjectForResource(resource)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	key := client.ObjectKey{Namespace: namespace, Name: name}
	if err := reader.Get(r.Context(), key, obj); err != nil {
		writeK8sError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, obj)
}

func (h *Handler) handleList(w http.ResponseWriter, r *http.Request, reader client.Reader, namespace, resource string) {
	slog.Info("handleList", "resource", resource, "namespace", namespace, "remote", r.RemoteAddr)

	list, err := newListForResource(resource)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	opts, err := buildListOptions(r, namespace)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := reader.List(r.Context(), list, opts...); err != nil {
		writeK8sError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// buildListOptions extracts query parameters into controller-runtime ListOptions.
// Returns an error if the labelSelector is invalid so the caller can return 400.
func buildListOptions(r *http.Request, namespace string) ([]client.ListOption, error) {
	var opts []client.ListOption
	if namespace != "" {
		opts = append(opts, client.InNamespace(namespace))
	}

	if sel := r.URL.Query().Get("labelSelector"); sel != "" {
		parsed, err := labels.Parse(sel)
		if err != nil {
			return nil, fmt.Errorf("invalid labelSelector %q: %w", sel, err)
		}
		opts = append(opts, client.MatchingLabelsSelector{Selector: parsed})
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.ParseInt(limitStr, 10, 64); err == nil && limit > 0 {
			opts = append(opts, client.Limit(limit))
		}
	}

	return opts, nil
}

// newObjectForResource returns a typed K8s object for a given resource name.
func newObjectForResource(resource string) (client.Object, error) {
	switch resource {
	case "pods":
		return &corev1.Pod{}, nil
	case "services":
		return &corev1.Service{}, nil
	case "secrets":
		return &corev1.Secret{}, nil
	case "persistentvolumeclaims":
		return &corev1.PersistentVolumeClaim{}, nil
	case "resourcequotas":
		return &corev1.ResourceQuota{}, nil
	case "namespaces":
		return &corev1.Namespace{}, nil
	case "events":
		return &corev1.Event{}, nil
	case "deployments":
		return &appsv1.Deployment{}, nil
	case "statefulsets":
		return &appsv1.StatefulSet{}, nil
	case "daemonsets":
		return &appsv1.DaemonSet{}, nil
	case "replicasets":
		return &appsv1.ReplicaSet{}, nil
	case "networkpolicies":
		return &networkingv1.NetworkPolicy{}, nil
	case "helmreleases":
		return &helmv2.HelmRelease{}, nil
	case "kustomizations":
		return &kustomizev1.Kustomization{}, nil
	case "gitrepositories":
		return &sourcev1.GitRepository{}, nil
	default:
		return nil, fmt.Errorf("unsupported resource: %s", resource)
	}
}

// newListForResource returns a typed K8s list object for a given resource name.
func newListForResource(resource string) (client.ObjectList, error) {
	switch resource {
	case "pods":
		return &corev1.PodList{}, nil
	case "services":
		return &corev1.ServiceList{}, nil
	case "secrets":
		return &corev1.SecretList{}, nil
	case "persistentvolumeclaims":
		return &corev1.PersistentVolumeClaimList{}, nil
	case "resourcequotas":
		return &corev1.ResourceQuotaList{}, nil
	case "namespaces":
		return &corev1.NamespaceList{}, nil
	case "events":
		return &corev1.EventList{}, nil
	case "deployments":
		return &appsv1.DeploymentList{}, nil
	case "statefulsets":
		return &appsv1.StatefulSetList{}, nil
	case "daemonsets":
		return &appsv1.DaemonSetList{}, nil
	case "replicasets":
		return &appsv1.ReplicaSetList{}, nil
	case "networkpolicies":
		return &networkingv1.NetworkPolicyList{}, nil
	case "helmreleases":
		return &helmv2.HelmReleaseList{}, nil
	case "kustomizations":
		return &kustomizev1.KustomizationList{}, nil
	case "gitrepositories":
		return &sourcev1.GitRepositoryList{}, nil
	default:
		return nil, fmt.Errorf("unsupported resource: %s", resource)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("Failed to encode response", "error", err)
	}
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeK8sError(w http.ResponseWriter, err error) {
	if apierrors.IsNotFound(err) {
		writeJSONError(w, http.StatusNotFound, err.Error())
	} else if apierrors.IsForbidden(err) {
		writeJSONError(w, http.StatusForbidden, err.Error())
	} else {
		slog.Error("K8s API error", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
	}
}

// AuthMiddleware returns middleware that validates a Bearer token using constant-time comparison.
// When token is empty, all requests are allowed through.
func AuthMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		got := strings.TrimPrefix(auth, "Bearer ")
		gotHash := sha256.Sum256([]byte(got))
		wantHash := sha256.Sum256([]byte(token))
		if subtle.ConstantTimeCompare(gotHash[:], wantHash[:]) != 1 {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SecurityHeaders wraps a handler with standard security headers.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}
