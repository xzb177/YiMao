package ai

import (
	"strings"
	"sync"
	"time"
)

// ResponseCache caches AI responses to reduce API calls
type ResponseCache struct {
	data      map[string]*CacheEntry
	mu        sync.RWMutex
	ttl       time.Duration
	stopChan  chan struct{}
	stopped   sync.Once
}

// CacheEntry represents a cached response
type CacheEntry struct {
	Response  string
	Timestamp time.Time
	HitCount  int
}

// NewResponseCache creates a new response cache
func NewResponseCache(ttl time.Duration) *ResponseCache {
	cache := &ResponseCache{
		data:     make(map[string]*CacheEntry),
		ttl:      ttl,
		stopChan: make(chan struct{}),
	}
	// Start cleanup goroutine
	go cache.cleanup()
	return cache
}

// Get retrieves a cached response
func (c *ResponseCache) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, exists := c.data[key]
	if !exists || time.Since(entry.Timestamp) > c.ttl {
		return "", false
	}
	entry.HitCount++
	return entry.Response, true
}

// Set stores a response in cache
func (c *ResponseCache) Set(key, response string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = &CacheEntry{
		Response:  response,
		Timestamp: time.Now(),
		HitCount:  0,
	}
}

// Delete removes a specific entry from cache
func (c *ResponseCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
}

// Clear empties the cache
func (c *ResponseCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[string]*CacheEntry)
}

// Size returns the number of cached entries
func (c *ResponseCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}

// GetStats returns cache statistics
func (c *ResponseCache) GetStats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	totalHits := 0
	for _, entry := range c.data {
		totalHits += entry.HitCount
	}

	return CacheStats{
		Entries:   len(c.data),
		TotalHits: totalHits,
		TTLHours:  c.ttl.Hours(),
	}
}

// cleanup removes expired entries periodically
func (c *ResponseCache) cleanup() {
	ticker := time.NewTicker(time.Minute * 10)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now()
			for key, entry := range c.data {
				if now.Sub(entry.Timestamp) > c.ttl {
					delete(c.data, key)
				}
			}
			c.mu.Unlock()
		case <-c.stopChan:
			return
		}
	}
}

// Stop stops the cleanup goroutine
func (c *ResponseCache) Stop() {
	c.stopped.Do(func() {
		close(c.stopChan)
	})
}

// BuildCacheKey creates a cache key from messages and system prompt
func BuildCacheKey(systemPrompt string, messages []Message) string {
	var parts []string
	if systemPrompt != "" {
		parts = append(parts, "system:"+systemPrompt)
	}
	for _, m := range messages {
		parts = append(parts, m.Role+":"+m.Content)
	}
	return strings.Join(parts, "|||")
}

// CacheStats represents cache statistics
type CacheStats struct {
	Entries   int
	TotalHits int
	TTLHours  float64
}
