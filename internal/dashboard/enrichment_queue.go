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
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/osagberg/kube-assist-operator/internal/ai"
	"github.com/osagberg/kube-assist-operator/internal/causal"
	"github.com/osagberg/kube-assist-operator/internal/checker"
)

// Prometheus metrics for the enrichment queue.
var (
	enrichmentQueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "kubeassist_ai_enrichment_queue_depth",
		Help: "Current depth of the AI enrichment queue",
	})
	enrichmentDroppedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "kubeassist_ai_enrichment_dropped_total",
		Help: "Total enrichment requests dropped due to full queue",
	})
	enrichmentProcessed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "kubeassist_ai_enrichment_processed_total",
		Help: "Total enrichment requests processed",
	}, []string{"result"}) // result: success, error, stale
	enrichmentDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "kubeassist_ai_enrichment_duration_seconds",
		Help:    "Duration of async AI enrichment calls",
		Buckets: prometheus.DefBuckets,
	})
)

func init() {
	metrics.Registry.MustRegister(
		enrichmentQueueDepth,
		enrichmentDroppedTotal,
		enrichmentProcessed,
		enrichmentDuration,
	)
}

// enrichmentRequest carries an immutable snapshot for async AI enrichment.
type enrichmentRequest struct {
	clusterID     string
	generation    uint64                          // cs.checkCounter at enqueue time
	issueHash     string                          // hash of issues at enqueue time
	configVersion string                          // hash(provider.Name()+model) at enqueue time
	results       map[string]*checker.CheckResult // deep copy — owned by worker
	causalCtx     *causal.CausalContext           // deep copy — owned by worker
	checkCtx      *checker.CheckContext           // minimal copy (no DataSource/Clientset)
	provider      ai.Provider
	aiModel       string // snapshot of model name at enqueue time
	enqueuedAt    time.Time
}

// enrichmentQueue is an in-process async queue with single-writer semantics
// per cluster key and newest-wins coalescing.
type enrichmentQueue struct {
	ch       chan enrichmentRequest
	mu       sync.Mutex
	pending  map[string]*enrichmentRequest // clusterID → newest pending (coalescing)
	inflight map[string]struct{}           // clusterID → currently processing
	capacity int
	wg       sync.WaitGroup
}

// newEnrichmentQueue creates a queue with the given channel capacity.
func newEnrichmentQueue(capacity int) *enrichmentQueue {
	if capacity <= 0 {
		capacity = 16
	}
	return &enrichmentQueue{
		ch:       make(chan enrichmentRequest, capacity),
		pending:  make(map[string]*enrichmentRequest),
		inflight: make(map[string]struct{}),
		capacity: capacity,
	}
}

// Enqueue submits a request with newest-wins coalescing per cluster.
// Returns true if the request was accepted (enqueued or stored as pending).
// Returns false only if the channel is full with all different clusters.
func (q *enrichmentQueue) Enqueue(req enrichmentRequest) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	// If this cluster is already in-flight, store/replace in pending (newest wins)
	if _, ok := q.inflight[req.clusterID]; ok {
		q.pending[req.clusterID] = &req
		q.updateDepthGauge()
		return true
	}

	// Try to dispatch directly via channel
	select {
	case q.ch <- req:
		q.inflight[req.clusterID] = struct{}{}
		q.updateDepthGauge()
		return true
	default:
		// Channel full — all slots are different clusters
		enrichmentDroppedTotal.Inc()
		return false
	}
}

// markDone is called by the worker after processing a request. It promotes
// a pending request for the same cluster if one exists.
func (q *enrichmentQueue) markDone(clusterID string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	delete(q.inflight, clusterID)

	// Check if there's a pending request for this cluster
	if pending, ok := q.pending[clusterID]; ok {
		delete(q.pending, clusterID)
		// Try to dispatch the pending request
		select {
		case q.ch <- *pending:
			q.inflight[clusterID] = struct{}{}
		default:
			// Channel full — put back in pending
			q.pending[clusterID] = pending
		}
	}

	q.updateDepthGauge()
}

// Run starts the single worker loop. It processes requests from the channel
// and calls handler for each. The ctx is passed through to the handler for
// AI call cancellation on shutdown.
func (q *enrichmentQueue) Run(ctx context.Context, handler func(context.Context, enrichmentRequest)) {
	q.wg.Go(func() {
		for {
			select {
			case <-ctx.Done():
				return
			case req, ok := <-q.ch:
				if !ok {
					return
				}
				handler(ctx, req)
			}
		}
	})
}

// Shutdown waits for the worker to finish (graceful drain).
func (q *enrichmentQueue) Shutdown() {
	q.wg.Wait()
}

// Depth returns the current number of pending + in-flight items.
func (q *enrichmentQueue) Depth() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending) + len(q.inflight)
}

// updateDepthGauge updates the Prometheus gauge. Must be called with q.mu held.
func (q *enrichmentQueue) updateDepthGauge() {
	enrichmentQueueDepth.Set(float64(len(q.pending) + len(q.inflight)))
}
