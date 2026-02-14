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
	"errors"
	"sync"
	"time"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // healthy, calls pass through
	CircuitOpen                         // tripped, calls rejected immediately
	CircuitHalfOpen                     // probing, one call allowed
)

// String returns the string representation of a CircuitState.
func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// ErrCircuitOpen is returned when the circuit breaker is open.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// CircuitBreaker implements the circuit breaker pattern for AI provider calls.
// When consecutive failures reach the threshold, the circuit opens and rejects
// calls immediately. After resetTimeout, it transitions to half-open and allows
// one probe call to determine if the provider has recovered.
type CircuitBreaker struct {
	mu                  sync.Mutex
	state               CircuitState
	consecutiveFailures int
	failureThreshold    int
	resetTimeout        time.Duration
	lastFailureTime     time.Time
	nowFunc             func() time.Time
	onStateChange       func(from, to CircuitState)
}

// CircuitBreakerOption configures a CircuitBreaker.
type CircuitBreakerOption func(*CircuitBreaker)

// WithNowFunc injects a clock function for testing.
func WithNowFunc(f func() time.Time) CircuitBreakerOption {
	return func(cb *CircuitBreaker) {
		cb.nowFunc = f
	}
}

// WithOnStateChange sets a callback for state transitions.
func WithOnStateChange(f func(from, to CircuitState)) CircuitBreakerOption {
	return func(cb *CircuitBreaker) {
		cb.onStateChange = f
	}
}

// NewCircuitBreaker creates a circuit breaker with the given failure threshold
// and reset timeout. Use functional options to inject a clock or state change callback.
func NewCircuitBreaker(threshold int, timeout time.Duration, opts ...CircuitBreakerOption) *CircuitBreaker {
	cb := &CircuitBreaker{
		state:            CircuitClosed,
		failureThreshold: threshold,
		resetTimeout:     timeout,
		nowFunc:          time.Now,
	}
	for _, opt := range opts {
		opt(cb)
	}
	return cb
}

// Allow returns true if a call should proceed. In Open state, it checks whether
// resetTimeout has elapsed and transitions to HalfOpen if so, allowing one probe
// call. In HalfOpen, subsequent calls are rejected until the probe result is recorded.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	var transition *stateTransition
	switch cb.state {
	case CircuitClosed:
		cb.mu.Unlock()
		return true
	case CircuitOpen:
		if cb.nowFunc().Sub(cb.lastFailureTime) >= cb.resetTimeout {
			transition = cb.setStateLocked(CircuitHalfOpen)
			cb.mu.Unlock()
			if transition != nil {
				transition.callback(transition.from, transition.to)
			}
			return true
		}
		cb.mu.Unlock()
		return false
	case CircuitHalfOpen:
		cb.mu.Unlock()
		return false
	}
	cb.mu.Unlock()
	return false
}

// RecordSuccess records a successful call. Resets the consecutive failure counter
// and closes the circuit if it was half-open.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	cb.consecutiveFailures = 0
	var transition *stateTransition
	if cb.state == CircuitHalfOpen {
		transition = cb.setStateLocked(CircuitClosed)
	}
	cb.mu.Unlock()
	if transition != nil {
		transition.callback(transition.from, transition.to)
	}
}

// RecordFailure records a failed call. Increments the failure counter and opens
// the circuit when the threshold is reached, or re-opens it if half-open.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	cb.consecutiveFailures++
	cb.lastFailureTime = cb.nowFunc()
	var transition *stateTransition
	if cb.state == CircuitHalfOpen || cb.consecutiveFailures >= cb.failureThreshold {
		transition = cb.setStateLocked(CircuitOpen)
	}
	cb.mu.Unlock()
	if transition != nil {
		transition.callback(transition.from, transition.to)
	}
}

// RecordRateLimit records a 429 rate-limit response. It resets the half-open
// timer so the provider gets another chance after resetTimeout, but does NOT
// increment the failure counter.
func (cb *CircuitBreaker) RecordRateLimit() {
	cb.mu.Lock()
	cb.lastFailureTime = cb.nowFunc()
	cb.mu.Unlock()
}

// State returns the current circuit state (thread-safe).
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// stateTransition captures a pending callback to invoke outside the lock.
type stateTransition struct {
	from, to CircuitState
	callback func(from, to CircuitState)
}

// setStateLocked records the state change under lock and returns a transition
// to call after the lock is released. Returns nil if no callback is needed.
// Caller MUST hold cb.mu.
func (cb *CircuitBreaker) setStateLocked(newState CircuitState) *stateTransition {
	old := cb.state
	cb.state = newState
	if cb.onStateChange != nil && old != newState {
		return &stateTransition{from: old, to: newState, callback: cb.onStateChange}
	}
	return nil
}
