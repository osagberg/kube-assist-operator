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

package ai

import (
	"testing"
	"time"
)

func TestRecordAICall(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		mode     string
		result   string
		tokens   int
		duration time.Duration
	}{
		{
			name:     "successful call with tokens",
			provider: "openai",
			mode:     "analyze",
			result:   "success",
			tokens:   500,
			duration: 2 * time.Second,
		},
		{
			name:     "failed call with zero tokens",
			provider: "anthropic",
			mode:     "explain",
			result:   "error",
			tokens:   0,
			duration: 100 * time.Millisecond,
		},
		{
			name:     "noop provider",
			provider: "noop",
			mode:     "analyze",
			result:   "success",
			tokens:   0,
			duration: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			RecordAICall(tt.provider, tt.mode, tt.result, tt.tokens, tt.duration)
		})
	}
}

func TestRecordCacheHit(t *testing.T) {
	// Should not panic
	RecordCacheHit()
	RecordCacheHit()
}

func TestRecordCacheMiss(t *testing.T) {
	// Should not panic
	RecordCacheMiss()
	RecordCacheMiss()
}

func TestRecordBudgetExceeded(t *testing.T) {
	tests := []struct {
		name   string
		window string
	}{
		{"daily window", "daily"},
		{"monthly window", "monthly"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			RecordBudgetExceeded(tt.window)
		})
	}
}

func TestRecordIssuesFiltered(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		count  int
	}{
		{"duplicate issues", "duplicate", 3},
		{"below threshold", "below-threshold", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			RecordIssuesFiltered(tt.reason, tt.count)
		})
	}
}

func TestUpdateCacheSize(t *testing.T) {
	tests := []struct {
		name string
		size int
	}{
		{"zero entries", 0},
		{"positive entries", 42},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			UpdateCacheSize(tt.size)
		})
	}
}

func TestUpdateBudgetMetrics(t *testing.T) {
	t.Run("nil budget does not panic", func(t *testing.T) {
		UpdateBudgetMetrics(nil)
	})

	t.Run("real budget with recorded usage", func(t *testing.T) {
		budget := NewBudget([]BudgetWindow{
			{Name: "daily", Duration: 24 * time.Hour, Limit: 10000},
			{Name: "monthly", Duration: 30 * 24 * time.Hour, Limit: 300000},
		})

		budget.RecordUsage(1500)
		budget.RecordUsage(2500)

		// Should not panic and should update gauges
		UpdateBudgetMetrics(budget)
	})
}
