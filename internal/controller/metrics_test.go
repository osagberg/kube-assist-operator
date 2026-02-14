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

package controller

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestMetrics_ReconcileTotal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		namespace string
		result    string
	}{
		{name: "success", namespace: "default", result: "success"},
		{name: "error", namespace: "default", result: "error"},
		{name: "requeue", namespace: "kube-system", result: "requeue"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			counter, err := reconcileTotal.GetMetricWith(prometheus.Labels{
				"namespace": tt.namespace,
				"result":    tt.result,
			})
			if err != nil {
				t.Fatalf("failed to get metric: %v", err)
			}

			var m dto.Metric
			before := getCounterValue(t, counter)
			counter.Inc()
			after := getCounterValue(t, counter)

			if after-before != 1 {
				t.Errorf("expected counter to increment by 1, got delta %f (metric: %+v)", after-before, &m)
			}
		})
	}
}

func TestMetrics_ReconcileDuration(t *testing.T) {
	t.Parallel()

	observer, err := reconcileDuration.GetMetricWith(prometheus.Labels{
		"namespace": "metrics-test",
	})
	if err != nil {
		t.Fatalf("failed to get metric: %v", err)
	}

	observer.Observe(0.5)
	observer.Observe(1.2)

	var m dto.Metric
	if err := observer.(prometheus.Metric).Write(&m); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	if m.Histogram == nil {
		t.Fatal("expected histogram metric")
	}
	if got := m.Histogram.GetSampleCount(); got != 2 {
		t.Errorf("sample count = %d, want 2", got)
	}
}

func TestMetrics_IssuesTotal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		namespace string
		severity  string
		value     float64
	}{
		{name: "critical", namespace: "metrics-ns", severity: "Critical", value: 3},
		{name: "warning", namespace: "metrics-ns", severity: "Warning", value: 5},
		{name: "info", namespace: "metrics-ns", severity: "Info", value: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gauge, err := issuesTotal.GetMetricWith(prometheus.Labels{
				"namespace": tt.namespace,
				"severity":  tt.severity,
			})
			if err != nil {
				t.Fatalf("failed to get metric: %v", err)
			}

			gauge.Set(tt.value)

			var m dto.Metric
			if err := gauge.Write(&m); err != nil {
				t.Fatalf("failed to write metric: %v", err)
			}
			if got := m.Gauge.GetValue(); got != tt.value {
				t.Errorf("gauge value = %f, want %f", got, tt.value)
			}
		})
	}
}

func TestMetrics_TeamHealthIssues(t *testing.T) {
	t.Parallel()

	gauge, err := teamHealthIssues.GetMetricWith(prometheus.Labels{
		"checker":  "workloads",
		"severity": "Critical",
	})
	if err != nil {
		t.Fatalf("failed to get metric: %v", err)
	}

	gauge.Set(7)

	var m dto.Metric
	if err := gauge.Write(&m); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	if got := m.Gauge.GetValue(); got != 7 {
		t.Errorf("gauge value = %f, want 7", got)
	}
}

func TestMetrics_TeamHealthResourcesChecked(t *testing.T) {
	t.Parallel()

	gauge, err := teamHealthResourcesChecked.GetMetricWith(prometheus.Labels{
		"checker": "workloads",
	})
	if err != nil {
		t.Fatalf("failed to get metric: %v", err)
	}

	gauge.Set(42)

	var m dto.Metric
	if err := gauge.Write(&m); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	if got := m.Gauge.GetValue(); got != 42 {
		t.Errorf("gauge value = %f, want 42", got)
	}
}

func getCounterValue(t *testing.T, counter prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	if err := counter.Write(&m); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	return m.Counter.GetValue()
}
