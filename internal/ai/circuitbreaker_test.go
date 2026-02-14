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
	"sync"
	"testing"
	"time"
)

func TestNewCircuitBreaker_Defaults(t *testing.T) {
	cb := NewCircuitBreaker(3, 5*time.Minute)

	if cb.State() != CircuitClosed {
		t.Errorf("initial state = %s, want closed", cb.State())
	}
	if cb.failureThreshold != 3 {
		t.Errorf("failureThreshold = %d, want 3", cb.failureThreshold)
	}
	if cb.resetTimeout != 5*time.Minute {
		t.Errorf("resetTimeout = %v, want 5m", cb.resetTimeout)
	}
}

func TestCircuitBreaker_AllowWhenClosed(t *testing.T) {
	cb := NewCircuitBreaker(3, 5*time.Minute)

	if !cb.Allow() {
		t.Error("Allow() should return true when circuit is closed")
	}
}

func TestCircuitBreaker_PartialFailures(t *testing.T) {
	cb := NewCircuitBreaker(3, 5*time.Minute)

	// 1 and 2 failures should keep the circuit closed
	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Errorf("after 1 failure: state = %s, want closed", cb.State())
	}
	if !cb.Allow() {
		t.Error("Allow() should return true after 1 failure")
	}

	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Errorf("after 2 failures: state = %s, want closed", cb.State())
	}
	if !cb.Allow() {
		t.Error("Allow() should return true after 2 failures")
	}
}

func TestCircuitBreaker_ThresholdOpens(t *testing.T) {
	cb := NewCircuitBreaker(3, 5*time.Minute)

	for range 3 {
		cb.RecordFailure()
	}

	if cb.State() != CircuitOpen {
		t.Errorf("after 3 failures: state = %s, want open", cb.State())
	}
}

func TestCircuitBreaker_OpenRejects(t *testing.T) {
	cb := NewCircuitBreaker(3, 5*time.Minute)

	for range 3 {
		cb.RecordFailure()
	}

	if cb.Allow() {
		t.Error("Allow() should return false when circuit is open")
	}
}

func TestCircuitBreaker_HalfOpenAfterTimeout(t *testing.T) {
	now := time.Now()
	cb := NewCircuitBreaker(3, 5*time.Minute, WithNowFunc(func() time.Time {
		return now
	}))

	// Trip the circuit
	for range 3 {
		cb.RecordFailure()
	}
	if cb.State() != CircuitOpen {
		t.Fatalf("expected open, got %s", cb.State())
	}

	// Advance past timeout
	now = now.Add(6 * time.Minute)

	// Allow should transition to half-open and return true
	if !cb.Allow() {
		t.Error("Allow() should return true after timeout (half-open probe)")
	}
	if cb.State() != CircuitHalfOpen {
		t.Errorf("state = %s, want half-open", cb.State())
	}

	// Second Allow in half-open should return false
	if cb.Allow() {
		t.Error("Allow() should return false for second call in half-open")
	}
}

func TestCircuitBreaker_HalfOpenSuccess(t *testing.T) {
	now := time.Now()
	cb := NewCircuitBreaker(3, 5*time.Minute, WithNowFunc(func() time.Time {
		return now
	}))

	// Trip the circuit
	for range 3 {
		cb.RecordFailure()
	}

	// Advance past timeout and allow probe
	now = now.Add(6 * time.Minute)
	cb.Allow()

	// Probe succeeds → should close
	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Errorf("state = %s, want closed after half-open success", cb.State())
	}

	// Should allow calls again
	if !cb.Allow() {
		t.Error("Allow() should return true after circuit closed")
	}
}

func TestCircuitBreaker_HalfOpenFailure(t *testing.T) {
	now := time.Now()
	cb := NewCircuitBreaker(3, 5*time.Minute, WithNowFunc(func() time.Time {
		return now
	}))

	// Trip the circuit
	for range 3 {
		cb.RecordFailure()
	}

	// Advance past timeout and allow probe
	now = now.Add(6 * time.Minute)
	cb.Allow()

	// Probe fails → should re-open with new timer
	failTime := now.Add(1 * time.Second)
	now = failTime
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Errorf("state = %s, want open after half-open failure", cb.State())
	}

	// Should still reject (timer was reset)
	now = failTime.Add(4 * time.Minute) // less than 5m timeout
	if cb.Allow() {
		t.Error("Allow() should return false before new timeout expires")
	}

	// Advance past new timeout
	now = failTime.Add(6 * time.Minute)
	if !cb.Allow() {
		t.Error("Allow() should return true after new timeout expires")
	}
}

func TestCircuitBreaker_SuccessResetsCounter(t *testing.T) {
	cb := NewCircuitBreaker(3, 5*time.Minute)

	// 2 failures, then a success
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess()

	// 2 more failures should not trip (counter was reset)
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != CircuitClosed {
		t.Errorf("state = %s, want closed (success should have reset counter)", cb.State())
	}

	// 3rd failure after reset should trip
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Errorf("state = %s, want open after 3 consecutive failures", cb.State())
	}
}

func TestCircuitBreaker_StateChangeCallback(t *testing.T) {
	type transition struct {
		from, to CircuitState
	}
	var transitions []transition

	now := time.Now()
	cb := NewCircuitBreaker(3, 5*time.Minute,
		WithNowFunc(func() time.Time { return now }),
		WithOnStateChange(func(from, to CircuitState) {
			transitions = append(transitions, transition{from, to})
		}),
	)

	// Trip the circuit: Closed → Open
	for range 3 {
		cb.RecordFailure()
	}

	// Advance past timeout, allow probe: Open → HalfOpen
	now = now.Add(6 * time.Minute)
	cb.Allow()

	// Probe succeeds: HalfOpen → Closed
	cb.RecordSuccess()

	expected := []transition{
		{CircuitClosed, CircuitOpen},
		{CircuitOpen, CircuitHalfOpen},
		{CircuitHalfOpen, CircuitClosed},
	}

	if len(transitions) != len(expected) {
		t.Fatalf("got %d transitions, want %d", len(transitions), len(expected))
	}
	for i, got := range transitions {
		if got != expected[i] {
			t.Errorf("transition[%d] = %s→%s, want %s→%s",
				i, got.from, got.to, expected[i].from, expected[i].to)
		}
	}
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	cb := NewCircuitBreaker(100, 5*time.Minute)

	var wg sync.WaitGroup
	const goroutines = 100

	// Concurrent Allow + RecordFailure + RecordSuccess + State
	for range goroutines {
		wg.Go(func() {
			cb.Allow()
		})
		wg.Go(func() {
			cb.RecordFailure()
		})
		wg.Go(func() {
			cb.RecordSuccess()
		})
		wg.Go(func() {
			_ = cb.State()
		})
	}

	wg.Wait()
	// If we get here without a race condition, the test passes
}

func TestCircuitBreaker_ThresholdOne(t *testing.T) {
	cb := NewCircuitBreaker(1, 5*time.Minute)

	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Errorf("state = %s, want open after 1 failure with threshold=1", cb.State())
	}
	if cb.Allow() {
		t.Error("Allow() should return false when circuit is open")
	}
}

func TestCircuitState_String(t *testing.T) {
	tests := []struct {
		state CircuitState
		want  string
	}{
		{CircuitClosed, "closed"},
		{CircuitOpen, "open"},
		{CircuitHalfOpen, "half-open"},
		{CircuitState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("CircuitState(%d).String() = %s, want %s", tt.state, got, tt.want)
		}
	}
}
