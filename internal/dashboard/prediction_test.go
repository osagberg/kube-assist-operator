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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
	"github.com/osagberg/kube-assist-operator/internal/history"
	"github.com/osagberg/kube-assist-operator/internal/prediction"
)

func TestServer_HandlePrediction_InsufficientData(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	server.mu.Lock()
	cs := server.getOrCreateClusterState("")
	server.mu.Unlock()

	// Add only 3 snapshots (less than MinDataPoints)
	for i := range 3 {
		cs.history.Add(history.HealthSnapshot{
			Timestamp:    time.Now().Add(time.Duration(i) * 30 * time.Second),
			TotalHealthy: 10,
			TotalIssues:  0,
			HealthScore:  100,
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/prediction/trend", nil)
	rr := httptest.NewRecorder()

	server.handlePrediction(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handlePrediction() insufficient data status = %d, want %d", rr.Code, http.StatusOK)
	}

	var response map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("handlePrediction() returned invalid JSON: %v", err)
	}

	if response["status"] != "insufficient_data" {
		t.Errorf("handlePrediction() status = %q, want %q", response["status"], "insufficient_data")
	}
}

func TestServer_HandlePrediction_WithData(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	server.mu.Lock()
	cs := server.getOrCreateClusterState("")
	server.mu.Unlock()

	// Add 10 snapshots with a degrading trend
	base := time.Now().Add(-5 * time.Minute)
	for i := range 10 {
		score := 95.0 - float64(i)*2
		healthy := int(score)
		issues := 100 - healthy
		cs.history.Add(history.HealthSnapshot{
			Timestamp:    base.Add(time.Duration(i) * 30 * time.Second),
			TotalHealthy: healthy,
			TotalIssues:  issues,
			BySeverity:   map[string]int{"Critical": issues / 3, "Warning": issues / 3, "Info": issues / 3},
			ByChecker:    map[string]int{"workloads": issues / 2, "helmreleases": issues / 2},
			HealthScore:  score,
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/prediction/trend", nil)
	rr := httptest.NewRecorder()

	server.handlePrediction(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handlePrediction() with data status = %d, want %d", rr.Code, http.StatusOK)
	}

	var result prediction.PredictionResult
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("handlePrediction() returned invalid JSON: %v", err)
	}

	if result.TrendDirection != "degrading" {
		t.Errorf("handlePrediction() trend = %q, want %q", result.TrendDirection, "degrading")
	}
	if result.DataPoints != 10 {
		t.Errorf("handlePrediction() dataPoints = %d, want 10", result.DataPoints)
	}
	if result.ProjectedScore < 0 || result.ProjectedScore > 100 {
		t.Errorf("handlePrediction() projected score out of range: %.1f", result.ProjectedScore)
	}
}

func TestServer_HandlePrediction_MethodNotAllowed(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	req := httptest.NewRequest(http.MethodPost, "/api/prediction/trend", nil)
	rr := httptest.NewRecorder()

	server.handlePrediction(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("handlePrediction() POST status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestServer_HandlePrediction_EmptyHistory(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	req := httptest.NewRequest(http.MethodGet, "/api/prediction/trend", nil)
	rr := httptest.NewRecorder()

	server.handlePrediction(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handlePrediction() empty history status = %d, want %d", rr.Code, http.StatusOK)
	}

	var response map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("handlePrediction() returned invalid JSON: %v", err)
	}

	if response["status"] != "insufficient_data" {
		t.Errorf("handlePrediction() status = %q, want %q", response["status"], "insufficient_data")
	}
}
