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

func TestCache_GetPut(t *testing.T) {
	c := NewCache(10, 5*time.Minute, true)

	req := AnalysisRequest{
		Issues: []IssueContext{
			{Type: "CrashLoopBackOff", Severity: "critical", Resource: "deployment/test", Message: "container crashing"},
		},
	}
	resp := &AnalysisResponse{
		Summary:    "test response",
		TokensUsed: 100,
	}

	c.Put(req, resp)

	got, ok := c.Get(req)
	if !ok {
		t.Fatal("Get() returned false, want true")
	}
	if got.Summary != "test response" {
		t.Errorf("Get().Summary = %q, want %q", got.Summary, "test response")
	}
	if c.Size() != 1 {
		t.Errorf("Size() = %d, want 1", c.Size())
	}
}

func TestCache_Miss(t *testing.T) {
	c := NewCache(10, 5*time.Minute, true)

	req := AnalysisRequest{
		Issues: []IssueContext{
			{Type: "OOMKilled", Severity: "critical", Resource: "pod/test", Message: "out of memory"},
		},
	}

	got, ok := c.Get(req)
	if ok {
		t.Error("Get() on empty cache returned true, want false")
	}
	if got != nil {
		t.Errorf("Get() on miss returned non-nil response: %v", got)
	}
}

func TestCache_LRUEviction(t *testing.T) {
	c := NewCache(2, 5*time.Minute, true)

	req1 := AnalysisRequest{Issues: []IssueContext{{Type: "type1", Message: "msg1"}}}
	req2 := AnalysisRequest{Issues: []IssueContext{{Type: "type2", Message: "msg2"}}}
	req3 := AnalysisRequest{Issues: []IssueContext{{Type: "type3", Message: "msg3"}}}

	resp := &AnalysisResponse{Summary: "resp", TokensUsed: 50}

	c.Put(req1, resp)
	c.Put(req2, resp)

	if c.Size() != 2 {
		t.Fatalf("Size() = %d, want 2", c.Size())
	}

	// Adding a third should evict req1 (oldest/LRU)
	c.Put(req3, resp)

	if c.Size() != 2 {
		t.Fatalf("Size() after eviction = %d, want 2", c.Size())
	}

	// req1 should be evicted
	if _, ok := c.Get(req1); ok {
		t.Error("req1 should have been evicted")
	}

	// req2 and req3 should still be present
	if _, ok := c.Get(req2); !ok {
		t.Error("req2 should still be cached")
	}
	if _, ok := c.Get(req3); !ok {
		t.Error("req3 should still be cached")
	}
}

func TestCache_TTLExpiration(t *testing.T) {
	c := NewCache(10, 1*time.Millisecond, true)

	req := AnalysisRequest{Issues: []IssueContext{{Type: "test", Message: "ttl test"}}}
	resp := &AnalysisResponse{Summary: "will expire", TokensUsed: 10}

	c.Put(req, resp)

	// Should be present immediately
	if _, ok := c.Get(req); !ok {
		t.Fatal("Get() immediately after Put() returned false")
	}

	// Wait for TTL to expire
	time.Sleep(2 * time.Millisecond)

	// Should be expired
	if _, ok := c.Get(req); ok {
		t.Error("Get() after TTL expiry should return false")
	}
}

func TestCache_Disabled(t *testing.T) {
	c := NewCache(10, 5*time.Minute, false)

	req := AnalysisRequest{Issues: []IssueContext{{Type: "test", Message: "disabled"}}}
	resp := &AnalysisResponse{Summary: "should not cache", TokensUsed: 10}

	c.Put(req, resp)

	if _, ok := c.Get(req); ok {
		t.Error("disabled cache Get() should return false")
	}
	if c.Size() != 0 {
		t.Errorf("disabled cache Size() = %d, want 0", c.Size())
	}
}

func TestCache_Clear(t *testing.T) {
	c := NewCache(10, 5*time.Minute, true)

	req1 := AnalysisRequest{Issues: []IssueContext{{Type: "type1", Message: "msg1"}}}
	req2 := AnalysisRequest{Issues: []IssueContext{{Type: "type2", Message: "msg2"}}}
	resp := &AnalysisResponse{Summary: "resp", TokensUsed: 50}

	c.Put(req1, resp)
	c.Put(req2, resp)

	if c.Size() != 2 {
		t.Fatalf("Size() before Clear() = %d, want 2", c.Size())
	}

	c.Clear()

	if c.Size() != 0 {
		t.Errorf("Size() after Clear() = %d, want 0", c.Size())
	}
	if _, ok := c.Get(req1); ok {
		t.Error("Get(req1) after Clear() should return false")
	}
	if _, ok := c.Get(req2); ok {
		t.Error("Get(req2) after Clear() should return false")
	}
}
