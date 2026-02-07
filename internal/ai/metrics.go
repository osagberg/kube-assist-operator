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
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	aiCallsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubeassist_ai_calls_total",
			Help: "Total number of AI provider calls",
		},
		[]string{"provider", "mode", "result"},
	)

	aiTokensUsedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubeassist_ai_tokens_used_total",
			Help: "Total tokens consumed by AI calls",
		},
		[]string{"provider", "mode"},
	)

	aiCacheHitsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "kubeassist_ai_cache_hits_total",
			Help: "Total AI response cache hits",
		},
	)

	aiCacheMissesTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "kubeassist_ai_cache_misses_total",
			Help: "Total AI response cache misses",
		},
	)

	aiBudgetExceededTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubeassist_ai_budget_exceeded_total",
			Help: "Total times token budget was exceeded",
		},
		[]string{"window"},
	)

	aiIssuesFilteredTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubeassist_ai_issues_filtered_total",
			Help: "Total issues filtered before AI analysis",
		},
		[]string{"reason"},
	)

	aiCallDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kubeassist_ai_call_duration_seconds",
			Help:    "Duration of AI provider calls",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"provider", "mode"},
	)

	aiCacheSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "kubeassist_ai_cache_size",
			Help: "Current number of entries in the AI response cache",
		},
	)

	aiBudgetTokensUsed = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kubeassist_ai_budget_tokens_used",
			Help: "Current token usage within budget window",
		},
		[]string{"window"},
	)

	aiBudgetTokensLimit = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kubeassist_ai_budget_tokens_limit",
			Help: "Token limit for budget window",
		},
		[]string{"window"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		aiCallsTotal,
		aiTokensUsedTotal,
		aiCacheHitsTotal,
		aiCacheMissesTotal,
		aiBudgetExceededTotal,
		aiIssuesFilteredTotal,
		aiCallDuration,
		aiCacheSize,
		aiBudgetTokensUsed,
		aiBudgetTokensLimit,
	)
}

// RecordAICall records metrics for an AI provider call.
func RecordAICall(provider, mode, result string, tokens int, duration time.Duration) {
	aiCallsTotal.WithLabelValues(provider, mode, result).Inc()
	if tokens > 0 {
		aiTokensUsedTotal.WithLabelValues(provider, mode).Add(float64(tokens))
	}
	aiCallDuration.WithLabelValues(provider, mode).Observe(duration.Seconds())
}

// RecordCacheHit records an AI response cache hit.
func RecordCacheHit() {
	aiCacheHitsTotal.Inc()
}

// RecordCacheMiss records an AI response cache miss.
func RecordCacheMiss() {
	aiCacheMissesTotal.Inc()
}

// RecordBudgetExceeded records that a budget window was exceeded.
func RecordBudgetExceeded(window string) {
	aiBudgetExceededTotal.WithLabelValues(window).Inc()
}

// RecordIssuesFiltered records issues filtered before AI analysis.
func RecordIssuesFiltered(reason string, count int) {
	aiIssuesFilteredTotal.WithLabelValues(reason).Add(float64(count))
}

// UpdateCacheSize updates the cache size gauge.
func UpdateCacheSize(size int) {
	aiCacheSize.Set(float64(size))
}

// UpdateBudgetMetrics updates budget usage gauges from current Budget state.
func UpdateBudgetMetrics(b *Budget) {
	if b == nil {
		return
	}
	for _, wu := range b.GetUsage() {
		aiBudgetTokensUsed.WithLabelValues(wu.Name).Set(float64(wu.Used))
		aiBudgetTokensLimit.WithLabelValues(wu.Name).Set(float64(wu.Limit))
	}
}
