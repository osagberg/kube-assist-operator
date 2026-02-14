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

// Package console provides a multi-cluster aggregation backend that serves
// Kubernetes resource data over HTTP. It manages multiple kubeconfig-based
// clients and exposes them via a unified REST API.
package console

import (
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/osagberg/kube-assist-operator/internal/datasource"
)

var log = logf.Log.WithName("console-aggregator")

// Aggregator manages multiple Kubernetes client.Reader instances,
// one per cluster, keyed by cluster ID.
type Aggregator struct {
	// clusters is set during construction and MUST NOT be modified after NewAggregator returns.
	// All reads are safe without synchronization because the map is immutable after init.
	clusters map[string]client.Reader
	scheme   *runtime.Scheme
}

// ClusterConfig maps a cluster ID to its kubeconfig path.
type ClusterConfig struct {
	ID             string
	KubeconfigPath string
}

// NewAggregator creates an Aggregator from a set of cluster configurations.
// Each cluster gets its own client.Reader backed by a direct (uncached) client.
func NewAggregator(configs []ClusterConfig) (*Aggregator, error) {
	s := datasource.NewConsoleScheme()

	clusters := make(map[string]client.Reader, len(configs))
	for _, cfg := range configs {
		restConfig, err := clientcmd.BuildConfigFromFlags("", cfg.KubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("cluster %s: failed to load kubeconfig %s: %w", cfg.ID, cfg.KubeconfigPath, err)
		}

		c, err := client.New(restConfig, client.Options{Scheme: s})
		if err != nil {
			return nil, fmt.Errorf("cluster %s: failed to create client: %w", cfg.ID, err)
		}

		clusters[cfg.ID] = c
		log.Info("Registered cluster", "id", cfg.ID, "kubeconfig", cfg.KubeconfigPath)
	}

	return &Aggregator{clusters: clusters, scheme: s}, nil
}

// GetReader returns the client.Reader for the given cluster ID.
func (a *Aggregator) GetReader(clusterID string) (client.Reader, bool) {
	r, ok := a.clusters[clusterID]
	return r, ok
}

// ClusterIDs returns the list of registered cluster IDs.
func (a *Aggregator) ClusterIDs() []string {
	ids := make([]string, 0, len(a.clusters))
	for id := range a.clusters {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Scheme returns the shared scheme used by all cluster clients.
func (a *Aggregator) Scheme() *runtime.Scheme {
	return a.scheme
}
