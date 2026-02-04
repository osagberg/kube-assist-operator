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
)

func TestServer_HandleDashboard(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(client, registry, ":8080")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	server.handleDashboard(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleDashboard() status = %d, want %d", rr.Code, http.StatusOK)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "text/html" {
		t.Errorf("handleDashboard() Content-Type = %s, want text/html", contentType)
	}

	// Check that HTML contains expected content
	body := rr.Body.String()
	if len(body) < 100 {
		t.Error("handleDashboard() returned too short response")
	}
}

func TestServer_HandleHealth_NoData(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(client, registry, ":8080")

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()

	server.handleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleHealth() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var response map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("handleHealth() returned invalid JSON: %v", err)
	}

	if response["status"] != "initializing" {
		t.Errorf("handleHealth() status = %s, want initializing", response["status"])
	}
}

func TestServer_HandleHealth_WithData(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(client, registry, ":8080")

	// Set some data
	server.latest = &HealthUpdate{
		Timestamp:  time.Now(),
		Namespaces: []string{"default"},
		Results: map[string]CheckResult{
			"workloads": {
				Name:    "workloads",
				Healthy: 5,
				Issues:  []Issue{},
			},
		},
		Summary: Summary{
			TotalHealthy: 5,
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()

	server.handleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleHealth() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var response HealthUpdate
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("handleHealth() returned invalid JSON: %v", err)
	}

	if response.Summary.TotalHealthy != 5 {
		t.Errorf("handleHealth() totalHealthy = %d, want 5", response.Summary.TotalHealthy)
	}
}

func TestServer_HandleTriggerCheck_WrongMethod(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(client, registry, ":8080")

	req := httptest.NewRequest(http.MethodGet, "/api/check", nil)
	rr := httptest.NewRecorder()

	server.handleTriggerCheck(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("handleTriggerCheck() with GET status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestServer_HandleTriggerCheck_POST(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(client, registry, ":8080")

	req := httptest.NewRequest(http.MethodPost, "/api/check", nil)
	rr := httptest.NewRecorder()

	server.handleTriggerCheck(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleTriggerCheck() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var response map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("handleTriggerCheck() returned invalid JSON: %v", err)
	}

	if response["status"] != "check triggered" {
		t.Errorf("handleTriggerCheck() status = %s, want 'check triggered'", response["status"])
	}
}

func TestNewServer(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(client, registry, ":9090")

	if server.addr != ":9090" {
		t.Errorf("NewServer() addr = %s, want :9090", server.addr)
	}
	if server.client != client {
		t.Error("NewServer() client not set correctly")
	}
	if server.registry != registry {
		t.Error("NewServer() registry not set correctly")
	}
	if server.clients == nil {
		t.Error("NewServer() clients map is nil")
	}
}

func TestHealthUpdate_JSON(t *testing.T) {
	update := HealthUpdate{
		Timestamp:  time.Now(),
		Namespaces: []string{"ns1", "ns2"},
		Results: map[string]CheckResult{
			"workloads": {
				Name:    "workloads",
				Healthy: 3,
				Issues: []Issue{
					{
						Type:       "CrashLoopBackOff",
						Severity:   "critical",
						Resource:   "deployment/test",
						Namespace:  "ns1",
						Message:    "Container crashing",
						Suggestion: "Check logs",
					},
				},
			},
		},
		Summary: Summary{
			TotalHealthy:  3,
			TotalIssues:   1,
			CriticalCount: 1,
		},
	}

	data, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded HealthUpdate
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if len(decoded.Namespaces) != 2 {
		t.Errorf("Namespaces length = %d, want 2", len(decoded.Namespaces))
	}
	if decoded.Summary.CriticalCount != 1 {
		t.Errorf("CriticalCount = %d, want 1", decoded.Summary.CriticalCount)
	}
}
