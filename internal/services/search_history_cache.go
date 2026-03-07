package services

import (
	"sync"
	"time"
)

// SearchHistoryCache wraps SearchHistoryDB with caching
type SearchHistoryCache struct {
	db       *SearchHistoryDB
	cache    map[int64]*cacheEntry
	cacheTTL time.Duration
	mu       sync.RWMutex
}

type cacheEntry struct {
	data      interface{}
	timestamp time.Time
}

// NewSearchHistoryCache creates a cached search history service
func NewSearchHistoryCache(db *SearchHistoryDB, ttl time.Duration) *SearchHistoryCache {
	return &SearchHistoryCache{
		db:       db,
		cache:    make(map[int64]*cacheEntry),
		cacheTTL: ttl,
	}
}

// AddSearch adds a search query to history
func (s *SearchHistoryCache) AddSearch(telegramID int64, query string) error {
	// Invalidate cache for this user
	s.invalidate(telegramID)

	return s.db.AddSearch(telegramID, query)
}

// GetHistory gets search history for a user
func (s *SearchHistoryCache) GetHistory(telegramID int64, limit int) ([]SearchEntry, error) {
	// Try cache first
	s.mu.RLock()
	cached, exists := s.cache[telegramID]
	s.mu.RUnlock()

	if exists && time.Since(cached.timestamp) < s.cacheTTL {
		return cached.data.([]SearchEntry), nil
	}

	// Load from database
	entries, err := s.db.GetHistory(telegramID, limit)
	if err != nil {
		return nil, err
	}

	// Cache the result
	s.mu.Lock()
	s.cache[telegramID] = &cacheEntry{
		data:      entries,
		timestamp: time.Now(),
	}
	s.mu.Unlock()

	return entries, nil
}

// GetHistoryGrouped gets search history grouped by time periods
func (s *SearchHistoryCache) GetHistoryGrouped(telegramID int64) (map[string][]SearchEntry, error) {
	// Try cache first
	cacheKey := telegramID + 1000000 // Offset for grouped cache

	s.mu.RLock()
	cached, exists := s.cache[cacheKey]
	s.mu.RUnlock()

	if exists && time.Since(cached.timestamp) < s.cacheTTL {
		return cached.data.(map[string][]SearchEntry), nil
	}

	// Load from database
	grouped, err := s.db.GetHistoryGrouped(telegramID)
	if err != nil {
		return nil, err
	}

	// Cache the result
	s.mu.Lock()
	s.cache[cacheKey] = &cacheEntry{
		data:      grouped,
		timestamp: time.Now(),
	}
	s.mu.Unlock()

	return grouped, nil
}

// GetEntry gets a specific search entry by index
func (s *SearchHistoryCache) GetEntry(telegramID int64, index int) (*SearchEntry, error) {
	return s.db.GetEntry(telegramID, index)
}

// DeleteEntry removes a specific search entry
func (s *SearchHistoryCache) DeleteEntry(telegramID int64, index int) error {
	// Invalidate cache
	s.invalidate(telegramID)

	return s.db.DeleteEntry(telegramID, index)
}

// ClearHistory clears all search history for a user
func (s *SearchHistoryCache) ClearHistory(telegramID int64) error {
	// Invalidate cache
	s.invalidate(telegramID)

	return s.db.ClearHistory(telegramID)
}

// GetStats gets search statistics for a user
func (s *SearchHistoryCache) GetStats(telegramID int64) (*SearchStats, error) {
	// Try cache first
	cacheKey := telegramID + 2000000 // Offset for stats cache

	s.mu.RLock()
	cached, exists := s.cache[cacheKey]
	s.mu.RUnlock()

	if exists && time.Since(cached.timestamp) < s.cacheTTL {
		return cached.data.(*SearchStats), nil
	}

	// Load from database
	stats, err := s.db.GetStats(telegramID)
	if err != nil {
		return nil, err
	}

	// Cache the result
	s.mu.Lock()
	s.cache[cacheKey] = &cacheEntry{
		data:      stats,
		timestamp: time.Now(),
	}
	s.mu.Unlock()

	return stats, nil
}

// GetPopularSearches gets popular searches across all users
func (s *SearchHistoryCache) GetPopularSearches(limit int) ([]PopularSearch, error) {
	// Use a shared cache key for popular searches
	cacheKey := int64(3000000)

	s.mu.RLock()
	cached, exists := s.cache[cacheKey]
	s.mu.RUnlock()

	if exists && time.Since(cached.timestamp) < s.cacheTTL {
		return cached.data.([]PopularSearch), nil
	}

	// Load from database
	popular, err := s.db.GetPopularSearches(limit)
	if err != nil {
		return nil, err
	}

	// Cache the result
	s.mu.Lock()
	s.cache[cacheKey] = &cacheEntry{
		data:      popular,
		timestamp: time.Now(),
	}
	s.mu.Unlock()

	return popular, nil
}

// GetSearchTrends gets trending searches
func (s *SearchHistoryCache) GetSearchTrends(days int) ([]TrendItem, error) {
	// Use a shared cache key for trends
	cacheKey := int64(4000000)

	s.mu.RLock()
	cached, exists := s.cache[cacheKey]
	s.mu.RUnlock()

	if exists && time.Since(cached.timestamp) < s.cacheTTL {
		return cached.data.([]TrendItem), nil
	}

	// Load from database
	trends, err := s.db.GetSearchTrends(days)
	if err != nil {
		return nil, err
	}

	// Cache the result
	s.mu.Lock()
	s.cache[cacheKey] = &cacheEntry{
		data:      trends,
		timestamp: time.Now(),
	}
	s.mu.Unlock()

	return trends, nil
}

// GetSuggestions gets search suggestions based on query
func (s *SearchHistoryCache) GetSuggestions(telegramID int64, query string) ([]string, error) {
	// Suggestions are user-specific and query-specific, don't cache
	return s.db.GetSuggestions(telegramID, query)
}

// UpdateEntryTags updates tags for a search entry
func (s *SearchHistoryCache) UpdateEntryTags(telegramID int64, index int, tags []string) error {
	// Invalidate cache
	s.invalidate(telegramID)

	return s.db.UpdateEntryTags(telegramID, index, tags)
}

// UpdateEntryMedia updates media association for a search entry
func (s *SearchHistoryCache) UpdateEntryMedia(telegramID int64, index int, mediaID int, mediaType string) error {
	// Invalidate cache
	s.invalidate(telegramID)

	return s.db.UpdateEntryMedia(telegramID, index, mediaID, mediaType)
}

// invalidate removes cache entries for a user
func (s *SearchHistoryCache) invalidate(telegramID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Invalidate user-specific caches
	delete(s.cache, telegramID)
	delete(s.cache, telegramID+1000000) // grouped
	delete(s.cache, telegramID+2000000) // stats
}

// Cleanup removes expired cache entries
func (s *SearchHistoryCache) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	for key, entry := range s.cache {
		if now.Sub(entry.timestamp) > s.cacheTTL {
			delete(s.cache, key)
		}
	}
}
