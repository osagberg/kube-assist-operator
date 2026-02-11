//go:build multicluster

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

package multicluster

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/osagberg/kube-assist-operator/internal/console"
)

const (
	envKubeconfigA = "KUBECONFIG_A"
	envKubeconfigB = "KUBECONFIG_B"
	envClusterA    = "CLUSTER_A"
	envClusterB    = "CLUSTER_B"
)

// newTestServer creates an httptest.Server backed by a real console.Aggregator
// connected to the Kind clusters. It skips the test if kubeconfig files are missing.
func newTestServer(t *testing.T) (*httptest.Server, string, string) {
	t.Helper()

	kcA := os.Getenv(envKubeconfigA)
	kcB := os.Getenv(envKubeconfigB)
	clusterA := os.Getenv(envClusterA)
	clusterB := os.Getenv(envClusterB)

	if kcA == "" || kcB == "" {
		t.Skip("KUBECONFIG_A / KUBECONFIG_B not set; skipping multi-cluster test")
	}
	if _, err := os.Stat(kcA); os.IsNotExist(err) {
		t.Skipf("kubeconfig %s does not exist on disk; skipping", kcA)
	}
	if _, err := os.Stat(kcB); os.IsNotExist(err) {
		t.Skipf("kubeconfig %s does not exist on disk; skipping", kcB)
	}

	if clusterA == "" {
		clusterA = "kube-assist-a"
	}
	if clusterB == "" {
		clusterB = "kube-assist-b"
	}

	agg, err := console.NewAggregator([]console.ClusterConfig{
		{ID: clusterA, KubeconfigPath: kcA},
		{ID: clusterB, KubeconfigPath: kcB},
	})
	if err != nil {
		t.Fatalf("failed to create aggregator: %v", err)
	}

	mux := http.NewServeMux()
	h := console.NewHandler(agg)
	console.RegisterRoutes(mux, h)

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return ts, clusterA, clusterB
}

func TestListClusters(t *testing.T) {
	ts, clusterA, clusterB := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/v1/clusters")
	if err != nil {
		t.Fatalf("GET /api/v1/clusters: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Clusters []string `json:"clusters"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	found := make(map[string]bool)
	for _, c := range body.Clusters {
		found[c] = true
	}
	if !found[clusterA] {
		t.Errorf("cluster %q not found in response: %v", clusterA, body.Clusters)
	}
	if !found[clusterB] {
		t.Errorf("cluster %q not found in response: %v", clusterB, body.Clusters)
	}
}

func TestListDeployments(t *testing.T) {
	ts, clusterA, _ := newTestServer(t)

	url := ts.URL + "/api/v1/clusters/" + clusterA + "/namespaces/demo-app/deployments"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var list struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	names := make(map[string]bool)
	for _, item := range list.Items {
		names[item.Metadata.Name] = true
	}

	if !names["nginx-healthy"] {
		t.Errorf("nginx-healthy not found in deployments: %v", names)
	}
	if !names["nginx-broken"] {
		t.Errorf("nginx-broken not found in deployments: %v", names)
	}
}

func TestUnknownCluster(t *testing.T) {
	ts, _, _ := newTestServer(t)

	url := ts.URL + "/api/v1/clusters/nonexistent/namespaces/default/deployments"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for unknown cluster, got %d", resp.StatusCode)
	}
}
