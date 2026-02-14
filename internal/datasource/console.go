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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
)

// ConsoleDataSource implements DataSource by querying a console backend HTTP API.
// This enables cross-cluster health monitoring by fetching Kubernetes resource
// data from a centralized console service instead of the local K8s API.
type ConsoleDataSource struct {
	baseURL     string
	clusterID   string
	httpClient  *http.Client
	scheme      *runtime.Scheme
	bearerToken string
}

// ConsoleOption configures a ConsoleDataSource.
type ConsoleOption func(*ConsoleDataSource)

// WithBearerToken sets the bearer token for authenticating with the console backend.
func WithBearerToken(token string) ConsoleOption {
	return func(c *ConsoleDataSource) {
		c.bearerToken = token
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) ConsoleOption {
	return func(c *ConsoleDataSource) {
		c.httpClient = hc
	}
}

// validClusterID matches alphanumeric characters, hyphens, underscores, and dots.
var validClusterID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// ValidateConsoleURL checks that baseURL has a valid scheme and host.
func ValidateConsoleURL(baseURL string) error {
	u, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid console URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("console URL must have scheme and host, got %q", baseURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("console URL scheme must be http or https, got %q", u.Scheme)
	}
	return nil
}

// ValidateClusterID checks that clusterID contains only safe characters.
func ValidateClusterID(clusterID string) error {
	if !validClusterID.MatchString(clusterID) {
		return fmt.Errorf("cluster ID must match %s, got %q", validClusterID.String(), clusterID)
	}
	return nil
}

// NewConsole creates a DataSource backed by a console backend HTTP API.
func NewConsole(baseURL, clusterID string, opts ...ConsoleOption) (*ConsoleDataSource, error) {
	if err := ValidateConsoleURL(baseURL); err != nil {
		return nil, err
	}
	if err := ValidateClusterID(clusterID); err != nil {
		return nil, err
	}
	s := NewConsoleScheme()

	ds := &ConsoleDataSource{
		baseURL:   strings.TrimRight(baseURL, "/"),
		clusterID: clusterID,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		scheme: s,
	}
	for _, o := range opts {
		o(ds)
	}
	return ds, nil
}

// NewConsoleScheme builds the runtime.Scheme used by both ConsoleDataSource
// and the console-backend Aggregator, ensuring type registration stays in sync.
func NewConsoleScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	_ = networkingv1.AddToScheme(s)
	_ = helmv2.AddToScheme(s)
	_ = kustomizev1.AddToScheme(s)
	_ = sourcev1.AddToScheme(s)
	return s
}

// gvkToResource maps GVK to the REST resource name. This covers the ~15 types
// used by kube-assist checkers. Extending this map is the only step needed to
// support additional resource types.
var gvkToResource = map[schema.GroupVersionKind]string{
	corev1.SchemeGroupVersion.WithKind("Pod"):                       "pods",
	corev1.SchemeGroupVersion.WithKind("PodList"):                   "pods",
	corev1.SchemeGroupVersion.WithKind("Service"):                   "services",
	corev1.SchemeGroupVersion.WithKind("ServiceList"):               "services",
	corev1.SchemeGroupVersion.WithKind("Secret"):                    "secrets",
	corev1.SchemeGroupVersion.WithKind("SecretList"):                "secrets",
	corev1.SchemeGroupVersion.WithKind("PersistentVolumeClaim"):     "persistentvolumeclaims",
	corev1.SchemeGroupVersion.WithKind("PersistentVolumeClaimList"): "persistentvolumeclaims",
	corev1.SchemeGroupVersion.WithKind("ResourceQuota"):             "resourcequotas",
	corev1.SchemeGroupVersion.WithKind("ResourceQuotaList"):         "resourcequotas",
	corev1.SchemeGroupVersion.WithKind("Namespace"):                 "namespaces",
	corev1.SchemeGroupVersion.WithKind("NamespaceList"):             "namespaces",
	corev1.SchemeGroupVersion.WithKind("Event"):                     "events",
	corev1.SchemeGroupVersion.WithKind("EventList"):                 "events",

	appsv1.SchemeGroupVersion.WithKind("Deployment"):      "deployments",
	appsv1.SchemeGroupVersion.WithKind("DeploymentList"):  "deployments",
	appsv1.SchemeGroupVersion.WithKind("StatefulSet"):     "statefulsets",
	appsv1.SchemeGroupVersion.WithKind("StatefulSetList"): "statefulsets",
	appsv1.SchemeGroupVersion.WithKind("DaemonSet"):       "daemonsets",
	appsv1.SchemeGroupVersion.WithKind("DaemonSetList"):   "daemonsets",
	appsv1.SchemeGroupVersion.WithKind("ReplicaSet"):      "replicasets",
	appsv1.SchemeGroupVersion.WithKind("ReplicaSetList"):  "replicasets",

	networkingv1.SchemeGroupVersion.WithKind("NetworkPolicy"):     "networkpolicies",
	networkingv1.SchemeGroupVersion.WithKind("NetworkPolicyList"): "networkpolicies",

	{Group: "helm.toolkit.fluxcd.io", Version: "v2", Kind: "HelmRelease"}:     "helmreleases",
	{Group: "helm.toolkit.fluxcd.io", Version: "v2", Kind: "HelmReleaseList"}: "helmreleases",

	{Group: "kustomize.toolkit.fluxcd.io", Version: "v1", Kind: "Kustomization"}:     "kustomizations",
	{Group: "kustomize.toolkit.fluxcd.io", Version: "v1", Kind: "KustomizationList"}: "kustomizations",

	{Group: "source.toolkit.fluxcd.io", Version: "v1", Kind: "GitRepository"}:     "gitrepositories",
	{Group: "source.toolkit.fluxcd.io", Version: "v1", Kind: "GitRepositoryList"}: "gitrepositories",
}

// clusterScopedResources is the set of resources that don't live in a namespace.
var clusterScopedResources = map[string]bool{
	"namespaces": true,
}

func (c *ConsoleDataSource) Get(ctx context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	resource, err := c.resourceFor(obj)
	if err != nil {
		return err
	}

	reqURL := c.buildGetURL(resource, key.Namespace, key.Name)

	body, err := c.doRequest(ctx, reqURL)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()

	return json.NewDecoder(body).Decode(obj)
}

func (c *ConsoleDataSource) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	resource, err := c.resourceFor(list)
	if err != nil {
		return err
	}

	listOpts := &client.ListOptions{}
	for _, o := range opts {
		o.ApplyToList(listOpts)
	}

	reqURL := c.buildListURL(resource, listOpts)

	body, err := c.doRequest(ctx, reqURL)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()

	return json.NewDecoder(body).Decode(list)
}

// resourceFor resolves a runtime.Object to its REST resource name.
func (c *ConsoleDataSource) resourceFor(obj runtime.Object) (string, error) {
	gvks, _, err := c.scheme.ObjectKinds(obj)
	if err != nil {
		return "", fmt.Errorf("consoledatasource: unknown type %T: %w", obj, err)
	}
	for _, gvk := range gvks {
		if r, ok := gvkToResource[gvk]; ok {
			return r, nil
		}
	}
	return "", fmt.Errorf("consoledatasource: no resource mapping for GVK %v", gvks)
}

// buildGetURL constructs the URL for a single-resource GET.
func (c *ConsoleDataSource) buildGetURL(resource, namespace, name string) string {
	if clusterScopedResources[resource] || namespace == "" {
		return fmt.Sprintf("%s/api/v1/clusters/%s/%s/%s",
			c.baseURL, c.clusterID, resource, name)
	}
	return fmt.Sprintf("%s/api/v1/clusters/%s/namespaces/%s/%s/%s",
		c.baseURL, c.clusterID, namespace, resource, name)
}

// buildListURL constructs the URL for a List request with query parameters.
func (c *ConsoleDataSource) buildListURL(resource string, opts *client.ListOptions) string {
	var base string
	if clusterScopedResources[resource] || opts.Namespace == "" {
		base = fmt.Sprintf("%s/api/v1/clusters/%s/%s",
			c.baseURL, c.clusterID, resource)
	} else {
		base = fmt.Sprintf("%s/api/v1/clusters/%s/namespaces/%s/%s",
			c.baseURL, c.clusterID, opts.Namespace, resource)
	}

	q := url.Values{}
	if opts.LabelSelector != nil {
		q.Set("labelSelector", opts.LabelSelector.String())
	}
	if opts.FieldSelector != nil {
		q.Set("fieldSelector", opts.FieldSelector.String())
	}
	if opts.Limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}

	if len(q) > 0 {
		return base + "?" + q.Encode()
	}
	return base
}

// doRequest executes an HTTP GET and handles error responses.
func (c *ConsoleDataSource) doRequest(ctx context.Context, reqURL string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("consoledatasource: failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	if c.bearerToken != "" {
		if req.URL.Scheme != "https" {
			host := req.URL.Hostname()
			if host != "localhost" && host != "127.0.0.1" && host != "::1" {
				return nil, fmt.Errorf("refusing to send bearer token over insecure connection to %s", host)
			}
			logf.Log.WithName("console-datasource").Info("WARNING: sending bearer token over insecure HTTP connection",
				"host", host, "url", reqURL)
		}
		req.Header.Set("Authorization", "Bearer "+c.bearerToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("consoledatasource: request failed: %w", err)
	}

	if resp.StatusCode == http.StatusOK {
		return resp.Body, nil
	}

	// Read the body for error details, then close it.
	defer func() { _ = resp.Body.Close() }()
	errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	gr := schema.GroupResource{} // generic; callers check via apierrors helpers
	switch resp.StatusCode {
	case http.StatusNotFound:
		return nil, apierrors.NewNotFound(gr, reqURL)
	case http.StatusForbidden:
		return nil, apierrors.NewForbidden(gr, reqURL, fmt.Errorf("%s", errBody))
	default:
		return nil, fmt.Errorf("consoledatasource: HTTP %d: %s", resp.StatusCode, errBody)
	}
}

// ClusterID returns the cluster identifier for this console data source.
func (c *ConsoleDataSource) ClusterID() string { return c.clusterID }

// ForCluster returns a shallow copy of this ConsoleDataSource scoped to a specific cluster.
// The returned copy shares the HTTP client, scheme, and auth token.
// Returns an error if clusterID fails validation.
func (c *ConsoleDataSource) ForCluster(clusterID string) (*ConsoleDataSource, error) {
	if err := ValidateClusterID(clusterID); err != nil {
		return nil, err
	}
	return &ConsoleDataSource{
		baseURL:     c.baseURL,
		clusterID:   clusterID,
		httpClient:  c.httpClient,
		scheme:      c.scheme,
		bearerToken: c.bearerToken,
	}, nil
}

// GetClusters fetches the list of known cluster IDs from the console backend.
func (c *ConsoleDataSource) GetClusters(ctx context.Context) ([]string, error) {
	reqURL := c.baseURL + "/api/v1/clusters"
	body, err := c.doRequest(ctx, reqURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()

	var resp struct {
		Clusters []string `json:"clusters"`
	}
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("consoledatasource: failed to decode clusters: %w", err)
	}
	return resp.Clusters, nil
}
