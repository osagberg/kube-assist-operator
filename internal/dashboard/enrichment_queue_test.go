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
	"sync/atomic"
	"testing"
	"time"
)

func TestEnrichmentQueue_EnqueueDequeue(t *testing.T) {
	q := newEnrichmentQueue(4)
	ctx := t.Context()

	var received []string
	var mu sync.Mutex

	q.Run(ctx, func(_ context.Context, req enrichmentRequest) {
		mu.Lock()
		received = append(received, req.clusterID)
		mu.Unlock()
		q.markDone(req.clusterID)
	})

	ok := q.Enqueue(enrichmentRequest{clusterID: "a", generation: 1})
	if !ok {
		t.Fatal("Enqueue should succeed")
	}

	// Wait for processing
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 || received[0] != "a" {
		t.Errorf("received = %v, want [a]", received)
	}
}

func TestEnrichmentQueue_BoundedCapacity(t *testing.T) {
	// Channel capacity 2 — fill with different clusters
	q := newEnrichmentQueue(2)
	// Don't start the worker so channel stays full

	ok1 := q.Enqueue(enrichmentRequest{clusterID: "a", generation: 1})
	ok2 := q.Enqueue(enrichmentRequest{clusterID: "b", generation: 1})
	ok3 := q.Enqueue(enrichmentRequest{clusterID: "c", generation: 1})

	if !ok1 || !ok2 {
		t.Fatal("first two enqueues should succeed")
	}
	if ok3 {
		t.Error("third enqueue for different cluster should fail when channel is full")
	}
}

func TestEnrichmentQueue_DroppedMetric(t *testing.T) {
	q := newEnrichmentQueue(1)
	// Don't start worker

	q.Enqueue(enrichmentRequest{clusterID: "a", generation: 1})
	// Channel full, different cluster should be dropped
	dropped := q.Enqueue(enrichmentRequest{clusterID: "b", generation: 1})
	if dropped {
		t.Error("should return false when dropped")
	}
}

func TestEnrichmentQueue_DepthGauge(t *testing.T) {
	q := newEnrichmentQueue(4)
	// Don't start worker

	if q.Depth() != 0 {
		t.Errorf("initial depth = %d, want 0", q.Depth())
	}

	q.Enqueue(enrichmentRequest{clusterID: "a", generation: 1})
	if q.Depth() != 1 {
		t.Errorf("depth after first enqueue = %d, want 1", q.Depth())
	}

	q.Enqueue(enrichmentRequest{clusterID: "b", generation: 1})
	if q.Depth() != 2 {
		t.Errorf("depth after second enqueue = %d, want 2", q.Depth())
	}
}

func TestEnrichmentQueue_PerClusterCoalescing(t *testing.T) {
	q := newEnrichmentQueue(4)
	ctx := t.Context()

	// Block the worker to force coalescing
	workerStarted := make(chan struct{})
	workerContinue := make(chan struct{})
	var startOnce sync.Once

	q.Run(ctx, func(_ context.Context, req enrichmentRequest) {
		startOnce.Do(func() {
			close(workerStarted)
			<-workerContinue
		})
		q.markDone(req.clusterID)
	})

	// First enqueue goes to worker
	q.Enqueue(enrichmentRequest{clusterID: "a", generation: 1})
	<-workerStarted

	// Second enqueue for same cluster goes to pending
	ok := q.Enqueue(enrichmentRequest{clusterID: "a", generation: 2})
	if !ok {
		t.Fatal("coalesced enqueue should succeed")
	}

	// Third enqueue for same cluster replaces pending (newest wins)
	ok = q.Enqueue(enrichmentRequest{clusterID: "a", generation: 3})
	if !ok {
		t.Fatal("newest-wins enqueue should succeed")
	}

	// Depth should be 1 inflight + 1 pending = 2
	if q.Depth() != 2 {
		t.Errorf("depth = %d, want 2 (1 inflight + 1 pending)", q.Depth())
	}

	close(workerContinue)
}

func TestEnrichmentQueue_MarkDone_DrainsPending(t *testing.T) {
	q := newEnrichmentQueue(4)
	ctx := t.Context()

	var processed []uint64
	var mu sync.Mutex
	done := make(chan struct{}, 4)

	firstDone := make(chan struct{})
	var firstOnce sync.Once

	q.Run(ctx, func(_ context.Context, req enrichmentRequest) {
		// Small delay on first request to allow pending to accumulate
		firstOnce.Do(func() {
			time.Sleep(20 * time.Millisecond)
			close(firstDone)
		})
		mu.Lock()
		processed = append(processed, req.generation)
		mu.Unlock()
		q.markDone(req.clusterID)
		done <- struct{}{}
	})

	q.Enqueue(enrichmentRequest{clusterID: "a", generation: 1})
	// Wait for first to start processing, then enqueue pending
	<-firstDone
	q.Enqueue(enrichmentRequest{clusterID: "a", generation: 2})

	// Wait for both to process
	<-done
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(processed) != 2 {
		t.Fatalf("expected 2 processed, got %d: %v", len(processed), processed)
	}
	if processed[0] != 1 || processed[1] != 2 {
		t.Errorf("processed = %v, want [1, 2]", processed)
	}
}

func TestEnrichmentQueue_NewestWins(t *testing.T) {
	q := newEnrichmentQueue(4)
	ctx := t.Context()

	workerStarted := make(chan struct{})
	workerContinue := make(chan struct{})
	var received []uint64
	var mu sync.Mutex
	done := make(chan struct{}, 4)

	q.Run(ctx, func(_ context.Context, req enrichmentRequest) {
		select {
		case <-workerStarted:
		default:
			close(workerStarted)
			<-workerContinue
		}
		mu.Lock()
		received = append(received, req.generation)
		mu.Unlock()
		q.markDone(req.clusterID)
		done <- struct{}{}
	})

	// First request goes to worker
	q.Enqueue(enrichmentRequest{clusterID: "a", generation: 1})
	<-workerStarted

	// Three rapid enqueues for same cluster — only latest should survive
	q.Enqueue(enrichmentRequest{clusterID: "a", generation: 2})
	q.Enqueue(enrichmentRequest{clusterID: "a", generation: 3})
	q.Enqueue(enrichmentRequest{clusterID: "a", generation: 4})

	// Release first worker
	close(workerContinue)

	// Wait for first + the surviving pending
	<-done
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("expected 2 processed, got %d: %v", len(received), received)
	}
	if received[0] != 1 {
		t.Errorf("first processed should be gen 1, got %d", received[0])
	}
	if received[1] != 4 {
		t.Errorf("second processed should be gen 4 (newest wins), got %d", received[1])
	}
}

func TestEnrichmentQueue_Shutdown(t *testing.T) {
	q := newEnrichmentQueue(4)
	ctx, cancel := context.WithCancel(context.Background())

	q.Run(ctx, func(_ context.Context, req enrichmentRequest) {
		q.markDone(req.clusterID)
	})

	q.Enqueue(enrichmentRequest{clusterID: "a", generation: 1})

	// Cancel context and verify shutdown completes without hanging
	cancel()

	shutdownDone := make(chan struct{})
	go func() {
		q.Shutdown()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown timed out — possible goroutine leak")
	}
}

func TestEnrichmentQueue_ConcurrentEnqueue(t *testing.T) {
	q := newEnrichmentQueue(16)
	ctx := t.Context()

	var processCount atomic.Int64
	q.Run(ctx, func(_ context.Context, req enrichmentRequest) {
		processCount.Add(1)
		q.markDone(req.clusterID)
	})

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			q.Enqueue(enrichmentRequest{
				clusterID:  "cluster-" + string(rune('a'+i%10)),
				generation: uint64(i),
			})
		}(i)
	}
	wg.Wait()

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	if processCount.Load() == 0 {
		t.Error("expected some requests to be processed")
	}
}
