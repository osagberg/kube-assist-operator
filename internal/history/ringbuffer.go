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

package history

import (
	"sync"
	"time"
)

// HealthSnapshot stores a point-in-time health check result
type HealthSnapshot struct {
	Timestamp    time.Time      `json:"timestamp"`
	TotalHealthy int            `json:"totalHealthy"`
	TotalIssues  int            `json:"totalIssues"`
	BySeverity   map[string]int `json:"bySeverity"`
	ByChecker    map[string]int `json:"byChecker"`
	HealthScore  float64        `json:"healthScore"`
}

// RingBuffer is a fixed-size circular buffer for health snapshots
type RingBuffer struct {
	mu       sync.RWMutex
	buf      []HealthSnapshot
	capacity int
	head     int
	count    int
}

// New creates a RingBuffer with the given capacity
func New(capacity int) *RingBuffer {
	return &RingBuffer{
		buf:      make([]HealthSnapshot, capacity),
		capacity: capacity,
	}
}

// Add inserts a snapshot into the buffer, overwriting the oldest if full
func (rb *RingBuffer) Add(s HealthSnapshot) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.buf[rb.head] = s
	rb.head = (rb.head + 1) % rb.capacity
	if rb.count < rb.capacity {
		rb.count++
	}
}

// Latest returns the most recently added snapshot
func (rb *RingBuffer) Latest() (HealthSnapshot, bool) {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.count == 0 {
		return HealthSnapshot{}, false
	}

	idx := (rb.head - 1 + rb.capacity) % rb.capacity
	return rb.buf[idx], true
}

// Last returns the last n snapshots in chronological order
func (rb *RingBuffer) Last(n int) []HealthSnapshot {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if n > rb.count {
		n = rb.count
	}
	if n == 0 {
		return nil
	}

	result := make([]HealthSnapshot, n)
	start := (rb.head - n + rb.capacity) % rb.capacity
	for i := 0; i < n; i++ {
		result[i] = rb.buf[(start+i)%rb.capacity]
	}
	return result
}

// Since returns all snapshots with a timestamp at or after t, in chronological order
func (rb *RingBuffer) Since(t time.Time) []HealthSnapshot {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.count == 0 {
		return nil
	}

	var result []HealthSnapshot
	start := (rb.head - rb.count + rb.capacity) % rb.capacity
	for i := 0; i < rb.count; i++ {
		s := rb.buf[(start+i)%rb.capacity]
		if !s.Timestamp.Before(t) {
			result = append(result, s)
		}
	}
	return result
}

// Len returns the current number of snapshots in the buffer
func (rb *RingBuffer) Len() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.count
}

// ComputeScore returns healthy/(healthy+issues)*100, or 100 if both are 0
func ComputeScore(healthy, issues int) float64 {
	total := healthy + issues
	if total == 0 {
		return 100
	}
	return float64(healthy) / float64(total) * 100
}
