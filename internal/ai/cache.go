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
	"encoding/json"
	"fmt"
	"slices"
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
// Optional providerModel args: [provider, model] are included in the cache key
// to avoid cross-provider collisions.
func (c *Cache) Get(request AnalysisRequest, providerModel ...string) (*AnalysisResponse, bool) {
	if c == nil || !c.enabled {
		return nil, false
	}
	provider, model := extractProviderModel(providerModel)
	key := computeRequestKey(request, provider, model)
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
// Optional providerModel args: [provider, model] are included in the cache key.
func (c *Cache) Put(request AnalysisRequest, response *AnalysisResponse, providerModel ...string) {
	if c == nil || !c.enabled || response == nil {
		return
	}
	provider, model := extractProviderModel(providerModel)
	key := computeRequestKey(request, provider, model)
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

// extractProviderModel extracts provider and model from variadic args.
func extractProviderModel(args []string) (string, string) {
	var provider, model string
	if len(args) > 0 {
		provider = args[0]
	}
	if len(args) > 1 {
		model = args[1]
	}
	return provider, model
}

// normalizeRequestForKey returns a copy of request with deterministic ordering
// for semantically order-insensitive fields.
func normalizeRequestForKey(request AnalysisRequest) AnalysisRequest {
	normalized := request

	// ExplainContext is ignored unless ExplainMode is true.
	if !normalized.ExplainMode {
		normalized.ExplainContext = ""
	}

	// Causal context is only used by BuildPrompt when groups exist.
	if normalized.CausalContext != nil && len(normalized.CausalContext.Groups) == 0 {
		normalized.CausalContext = nil
	}

	// Namespace order is not semantically meaningful for cluster context.
	sorted := make([]string, len(normalized.ClusterContext.Namespaces))
	copy(sorted, normalized.ClusterContext.Namespaces)
	slices.Sort(sorted)
	normalized.ClusterContext.Namespaces = sorted

	return normalized
}

// computeRequestKey generates a deterministic cache key from an AnalysisRequest.
//
// Key dimensions (all contribute to the hash):
//   - provider, model: avoid cross-provider/model cache collisions
//   - full AnalysisRequest payload after normalization (issue content, cluster context, causal context)
//   - ExplainMode + ExplainContext: separates explain from analyze requests
//   - MaxTokens: different token limits yield different responses
//   - ClusterContext.Namespaces: sorted namespace list
//
// NOTE: SHA-256 is used here; xxhash could be substituted if profiling shows this is a bottleneck.
func computeRequestKey(request AnalysisRequest, provider, model string) string {
	payload := struct {
		Provider string          `json:"provider"`
		Model    string          `json:"model"`
		Request  AnalysisRequest `json:"request"`
	}{
		Provider: provider,
		Model:    model,
		Request:  normalizeRequestForKey(request),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		// Fallback keeps cache operational if marshaling ever fails.
		sum := sha256.Sum256([]byte(fmt.Sprintf("%+v", payload)))
		return fmt.Sprintf("%x", sum[:])
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}
