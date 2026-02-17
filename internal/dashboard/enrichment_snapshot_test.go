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

package dashboard

import (
	"testing"
	"time"

	"github.com/osagberg/kube-assist-operator/internal/ai"
	"github.com/osagberg/kube-assist-operator/internal/checker"
)

const testMutated = "MUTATED"

func TestDeepCopyResults_Independence(t *testing.T) {
	src := map[string]*checker.CheckResult{
		"workloads": {
			CheckerName: "workloads",
			Healthy:     3,
			Issues: []checker.Issue{
				{
					Type:       "CrashLoop",
					Severity:   "critical",
					Namespace:  "default",
					Resource:   "deploy/app",
					Message:    "Pod crashing",
					Suggestion: "Check logs",
					Metadata:   map[string]string{"key": "value"},
				},
			},
		},
	}

	dst := deepCopyResults(src)

	// Mutate the copy
	dst["workloads"].Issues[0].Suggestion = testMutated
	dst["workloads"].Issues[0].Metadata["key"] = testMutated
	dst["workloads"].Healthy = 999

	// Original should be unchanged
	if src["workloads"].Issues[0].Suggestion != "Check logs" {
		t.Errorf("original Suggestion mutated: %q", src["workloads"].Issues[0].Suggestion)
	}
	if src["workloads"].Issues[0].Metadata["key"] != "value" {
		t.Errorf("original Metadata mutated: %q", src["workloads"].Issues[0].Metadata["key"])
	}
	if src["workloads"].Healthy != 3 {
		t.Errorf("original Healthy mutated: %d", src["workloads"].Healthy)
	}
}

func TestDeepCopyResults_NilSafe(t *testing.T) {
	dst := deepCopyResults(nil)
	if dst != nil {
		t.Errorf("expected nil for nil input, got %v", dst)
	}
}

func TestDeepCopyResults_MetadataCloned(t *testing.T) {
	src := map[string]*checker.CheckResult{
		"test": {
			Issues: []checker.Issue{
				{
					Metadata: map[string]string{"a": "1", "b": "2"},
				},
			},
		},
	}

	dst := deepCopyResults(src)

	// Add to copy's metadata
	dst["test"].Issues[0].Metadata["c"] = "3"

	// Original should not have the new key
	if _, ok := src["test"].Issues[0].Metadata["c"]; ok {
		t.Error("original Metadata should not have key 'c'")
	}
}

func TestDeepCopyResults_NilMetadata(t *testing.T) {
	src := map[string]*checker.CheckResult{
		"test": {
			Issues: []checker.Issue{
				{Type: "test", Metadata: nil},
			},
		},
	}

	dst := deepCopyResults(src)
	if dst["test"].Issues[0].Metadata != nil {
		t.Error("nil Metadata should remain nil in copy")
	}
}

func TestMinimalCheckContext_NoDataSource(t *testing.T) {
	provider := ai.NewNoOpProvider()
	src := &checker.CheckContext{
		DataSource: nil, // would be set in real use
		Namespaces: []string{"default", "prod"},
		AIEnabled:  true,
		AIProvider: provider,
		MaxIssues:  15,
		ClusterContext: ai.ClusterContext{
			Namespaces: []string{"default", "prod"},
		},
	}

	dst := minimalCheckContext(src, provider)

	if dst.DataSource != nil {
		t.Error("DataSource should be nil in minimal copy")
	}
	if dst.Clientset != nil {
		t.Error("Clientset should be nil in minimal copy")
	}
	if !dst.AIEnabled {
		t.Error("AIEnabled should be true")
	}
	if dst.AIProvider != provider {
		t.Error("AIProvider should match")
	}
	if dst.MaxIssues != 15 {
		t.Errorf("MaxIssues = %d, want 15", dst.MaxIssues)
	}
}

func TestMinimalCheckContext_NilSafe(t *testing.T) {
	dst := minimalCheckContext(nil, nil)
	if dst != nil {
		t.Error("expected nil for nil input")
	}
}

func TestCloneHealthUpdate_Independence(t *testing.T) {
	src := &HealthUpdate{
		Timestamp:  time.Now(),
		ClusterID:  "test",
		Namespaces: []string{"default", "prod"},
		Results: map[string]CheckResult{
			"workloads": {
				Name:    "workloads",
				Healthy: 5,
				Issues: []Issue{
					{
						Type:       "CrashLoop",
						Severity:   "critical",
						Suggestion: "Check logs",
						Metadata:   map[string]string{"key": "val"},
					},
				},
			},
		},
		Summary: Summary{TotalHealthy: 5, HealthScore: 80},
		AIStatus: &AIStatus{
			Enabled:  true,
			Provider: "noop",
			Pending:  true,
		},
		IssueStates: map[string]*IssueState{
			"ns/deploy/type": {
				Key:       "ns/deploy/type",
				Action:    ActionAcknowledged,
				CreatedAt: time.Now(),
			},
		},
	}

	dst := cloneHealthUpdate(src)

	// Mutate the clone
	dst.Results["workloads"] = CheckResult{Name: testMutated}
	dst.AIStatus.Provider = testMutated
	dst.Namespaces[0] = testMutated
	dst.IssueStates["ns/deploy/type"].Action = testMutated

	// Original should be unchanged
	if src.Results["workloads"].Name != "workloads" {
		t.Errorf("original Results mutated: %q", src.Results["workloads"].Name)
	}
	if src.AIStatus.Provider != "noop" {
		t.Errorf("original AIStatus mutated: %q", src.AIStatus.Provider)
	}
	if src.Namespaces[0] != "default" {
		t.Errorf("original Namespaces mutated: %q", src.Namespaces[0])
	}
	if src.IssueStates["ns/deploy/type"].Action != ActionAcknowledged {
		t.Errorf("original IssueStates mutated: %q", src.IssueStates["ns/deploy/type"].Action)
	}
}

func TestCloneHealthUpdate_NilSafe(t *testing.T) {
	dst := cloneHealthUpdate(nil)
	if dst.Results != nil {
		t.Error("expected nil Results for nil input")
	}
	if dst.AIStatus != nil {
		t.Error("expected nil AIStatus for nil input")
	}
}

func TestCloneHealthUpdate_IssueMetadataCloned(t *testing.T) {
	src := &HealthUpdate{
		Results: map[string]CheckResult{
			"test": {
				Issues: []Issue{
					{Metadata: map[string]string{"a": "1"}},
				},
			},
		},
	}

	dst := cloneHealthUpdate(src)

	// Mutate clone's metadata
	cr := dst.Results["test"]
	cr.Issues[0].Metadata["b"] = "2"
	dst.Results["test"] = cr

	// Original should not have new key
	if _, ok := src.Results["test"].Issues[0].Metadata["b"]; ok {
		t.Error("original Issue Metadata should not have key 'b'")
	}
}

func TestConfigVersionHash_Deterministic(t *testing.T) {
	h1 := configVersionHash("openai", "gpt-4")
	h2 := configVersionHash("openai", "gpt-4")
	if h1 != h2 {
		t.Errorf("same input should produce same hash: %q vs %q", h1, h2)
	}
}

func TestConfigVersionHash_ProviderSensitive(t *testing.T) {
	h1 := configVersionHash("openai", "gpt-4")
	h2 := configVersionHash("anthropic", "gpt-4")
	if h1 == h2 {
		t.Error("different providers should produce different hashes")
	}
}

func TestConfigVersionHash_ModelSensitive(t *testing.T) {
	h1 := configVersionHash("openai", "gpt-4")
	h2 := configVersionHash("openai", "gpt-3.5")
	if h1 == h2 {
		t.Error("different models should produce different hashes")
	}
}

func TestConfigVersionHash_EmptyInputs(t *testing.T) {
	h := configVersionHash("", "")
	if h == "" {
		t.Error("hash should not be empty even for empty inputs")
	}
}
