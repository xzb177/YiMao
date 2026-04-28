package services

import (
	"emby-telegram-bot/pkg/logger"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// MessageCache prevents sending duplicate messages within a time window
type MessageCache struct {
	cache      map[string]messageCacheEntry
	mu         sync.RWMutex
	ttl        time.Duration
	cleanupInt time.Duration
	stopChan   chan struct{}
}

type messageCacheEntry struct {
	content   string
	timestamp time.Time
}

// NewMessageCache creates a new message cache
func NewMessageCache(ttl time.Duration) *MessageCache {
	mc := &MessageCache{
		cache:      make(map[string]messageCacheEntry),
		ttl:        ttl,
		cleanupInt: ttl * 2,
		stopChan:   make(chan struct{}),
	}
	go mc.cleanupLoop()
	return mc
}

// Stop stops the cache cleanup routine
func (mc *MessageCache) Stop() {
	close(mc.stopChan)
}

// Check checks if a message was recently sent
// Returns true if the message was sent within TTL (should be skipped)
func (mc *MessageCache) Check(chatID int64, content string) bool {
	if mc == nil {
		return false
	}

	key := mc.cacheKey(chatID, content)

	mc.mu.RLock()
	entry, exists := mc.cache[key]
	mc.mu.RUnlock()

	if !exists {
		return false
	}

	// Check if entry is still valid
	if time.Since(entry.timestamp) < mc.ttl {
		logger.Info("[MessageCache] Duplicate message blocked for chat %d", chatID)
		return true
	}

	// Entry expired, remove it
	mc.mu.Lock()
	delete(mc.cache, key)
	mc.mu.Unlock()
	return false
}

// Add adds a message to the cache
func (mc *MessageCache) Add(chatID int64, content string) {
	if mc == nil {
		return
	}

	key := mc.cacheKey(chatID, content)

	mc.mu.Lock()
	mc.cache[key] = messageCacheEntry{
		content:   content,
		timestamp: time.Now(),
	}
	mc.mu.Unlock()
}

// cacheKey generates a cache key for a message
func (mc *MessageCache) cacheKey(chatID int64, content string) string {
	// Use MD5 of content to limit key size
	h := md5.New()
	h.Write([]byte(content))
	hash := hex.EncodeToString(h.Sum(nil))
	return fmt.Sprintf("%d:%s", chatID, hash)
}

// cleanupLoop periodically cleans up expired entries
func (mc *MessageCache) cleanupLoop() {
	ticker := time.NewTicker(mc.cleanupInt)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Info("[MessageCache] Panic in cleanup: %v", r)
					}
				}()
				mc.cleanup()
			}()
		case <-mc.stopChan:
			return
		}
	}
}

// cleanup removes expired entries
func (mc *MessageCache) cleanup() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	now := time.Now()
	for key, entry := range mc.cache {
		if now.Sub(entry.timestamp) > mc.ttl {
			delete(mc.cache, key)
		}
	}
}

// Stats returns cache statistics
func (mc *MessageCache) Stats() map[string]interface{} {
	if mc == nil {
		return nil
	}

	mc.mu.RLock()
	defer mc.mu.RUnlock()

	return map[string]interface{}{
		"size": len(mc.cache),
		"ttl":  mc.ttl.String(),
	}
}
