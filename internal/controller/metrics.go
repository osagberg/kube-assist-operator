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
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// reconcileTotal counts the total number of reconciliations per controller
	reconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubeassist_reconcile_total",
			Help: "Total number of reconciliations per TroubleshootRequest",
		},
		[]string{"name", "namespace", "result"},
	)

	// reconcileDuration tracks the duration of reconciliations
	reconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kubeassist_reconcile_duration_seconds",
			Help:    "Duration of reconciliation in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"name", "namespace"},
	)

	// issuesTotal tracks the total number of issues found per namespace and severity
	issuesTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kubeassist_issues_total",
			Help: "Total number of diagnostic issues found",
		},
		[]string{"namespace", "severity"},
	)
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(reconcileTotal)
	metrics.Registry.MustRegister(reconcileDuration)
	metrics.Registry.MustRegister(issuesTotal)
}
