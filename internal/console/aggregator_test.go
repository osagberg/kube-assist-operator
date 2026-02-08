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
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/osagberg/kube-assist-operator/internal/datasource"
)

func TestAggregatorGetReader(t *testing.T) {
	s := datasource.NewConsoleScheme()
	fc := fake.NewClientBuilder().WithScheme(s).Build()

	agg := &Aggregator{
		clusters: map[string]client.Reader{
			"cluster-1": fc,
		},
		scheme: s,
	}

	tests := []struct {
		name      string
		clusterID string
		wantOK    bool
	}{
		{
			name:      "existing cluster returns reader",
			clusterID: "cluster-1",
			wantOK:    true,
		},
		{
			name:      "missing cluster returns false",
			clusterID: "no-such-cluster",
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, ok := agg.GetReader(tt.clusterID)
			if ok != tt.wantOK {
				t.Errorf("GetReader(%q) ok = %v, want %v", tt.clusterID, ok, tt.wantOK)
			}
			if tt.wantOK && reader == nil {
				t.Errorf("GetReader(%q) returned nil reader when ok=true", tt.clusterID)
			}
			if !tt.wantOK && reader != nil {
				t.Errorf("GetReader(%q) returned non-nil reader when ok=false", tt.clusterID)
			}
		})
	}
}

func TestAggregatorClusterIDs(t *testing.T) {
	s := datasource.NewConsoleScheme()
	fc1 := fake.NewClientBuilder().WithScheme(s).Build()
	fc2 := fake.NewClientBuilder().WithScheme(s).Build()
	fc3 := fake.NewClientBuilder().WithScheme(s).Build()

	agg := &Aggregator{
		clusters: map[string]client.Reader{
			"alpha":      fc1,
			"beta":       fc2,
			"gamma-prod": fc3,
		},
		scheme: s,
	}

	got := agg.ClusterIDs()

	want := []string{"alpha", "beta", "gamma-prod"}
	if len(got) != len(want) {
		t.Fatalf("ClusterIDs() returned %d IDs, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i] != id {
			t.Errorf("ClusterIDs()[%d] = %q, want %q", i, got[i], id)
		}
	}
}

func TestAggregatorClusterIDsEmpty(t *testing.T) {
	agg := &Aggregator{
		clusters: map[string]client.Reader{},
		scheme:   datasource.NewConsoleScheme(),
	}

	got := agg.ClusterIDs()
	if len(got) != 0 {
		t.Errorf("ClusterIDs() on empty aggregator returned %v, want empty", got)
	}
}

func TestAggregatorScheme(t *testing.T) {
	s := datasource.NewConsoleScheme()
	agg := &Aggregator{
		clusters: map[string]client.Reader{},
		scheme:   s,
	}

	if agg.Scheme() != s {
		t.Error("Scheme() did not return the same scheme instance")
	}
}
