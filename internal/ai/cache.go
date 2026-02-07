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
	"container/list"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

// Cache provides an LRU cache for AI analysis responses.
type Cache struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	items    map[string]*list.Element
	order    *list.List
	enabled  bool
}

type cacheEntry struct {
	key       string
	response  *AnalysisResponse
	createdAt time.Time
}

// NewCache creates an LRU cache. Pass enabled=false to create a no-op cache.
func NewCache(capacity int, ttl time.Duration, enabled bool) *Cache {
	if !enabled || capacity <= 0 {
		return &Cache{enabled: false}
	}
	return &Cache{
		capacity: capacity,
		ttl:      ttl,
		items:    make(map[string]*list.Element),
		order:    list.New(),
		enabled:  enabled,
	}
}

// Get retrieves a cached response. Returns (nil, false) on miss or expired entry.
func (c *Cache) Get(request AnalysisRequest) (*AnalysisResponse, bool) {
	if c == nil || !c.enabled {
		return nil, false
	}
	key := computeRequestKey(request)
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		return nil, false
	}
	entry := elem.Value.(*cacheEntry)
	if time.Since(entry.createdAt) > c.ttl {
		c.order.Remove(elem)
		delete(c.items, key)
		return nil, false
	}
	c.order.MoveToFront(elem)
	return entry.response, true
}

// Put stores a response in the cache, evicting LRU entries if at capacity.
func (c *Cache) Put(request AnalysisRequest, response *AnalysisResponse) {
	if c == nil || !c.enabled || response == nil {
		return
	}
	key := computeRequestKey(request)
	c.mu.Lock()
	defer c.mu.Unlock()

	// Update existing
	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		elem.Value.(*cacheEntry).response = response
		elem.Value.(*cacheEntry).createdAt = time.Now()
		return
	}

	// Evict if at capacity
	if c.order.Len() >= c.capacity {
		oldest := c.order.Back()
		if oldest != nil {
			c.order.Remove(oldest)
			delete(c.items, oldest.Value.(*cacheEntry).key)
		}
	}

	entry := &cacheEntry{key: key, response: response, createdAt: time.Now()}
	elem := c.order.PushFront(entry)
	c.items[key] = elem
	UpdateCacheSize(c.order.Len())
}

// Clear removes all entries from the cache.
func (c *Cache) Clear() {
	if c == nil || !c.enabled {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*list.Element)
	c.order.Init()
	UpdateCacheSize(0)
}

// Size returns the current number of cached entries.
func (c *Cache) Size() int {
	if c == nil || !c.enabled {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// computeRequestKey generates a deterministic cache key from an AnalysisRequest.
// It hashes the normalized issue signatures (type+severity+resource+message).
func computeRequestKey(request AnalysisRequest) string {
	h := sha256.New()
	for _, issue := range request.Issues {
		_, _ = fmt.Fprintf(h, "%s|%s|%s|%s\n", issue.Type, issue.Severity, issue.Resource, issue.Message)
	}
	// Include explain mode in key
	if request.ExplainMode {
		_, _ = fmt.Fprintf(h, "explain:%s\n", request.ExplainContext)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
