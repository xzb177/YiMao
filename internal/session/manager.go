package session

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// Manager manages user sessions
type Manager struct {
	sessions map[int64]*Session
	mu       sync.RWMutex
	maxAge   time.Duration
	maxSize  int
}

// Session represents a user session
type Session struct {
	UserID       int64
	CreatedAt    time.Time
	LastActivity time.Time
	Data         map[string]interface{}
	navStack     []NavEntry
	mu           sync.RWMutex
}

// NavEntry represents a navigation history entry
type NavEntry struct {
	Source    string
	Query     string
	Timestamp time.Time
}

// SearchItem represents a search result item
type SearchItem struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Year     int       `json:"year"`
	Type     string    `json:"type"`
	Poster   string    `json:"poster,omitempty"`
	Rating   float64   `json:"rating,omitempty"`
	Overview string    `json:"overview,omitempty"`
	Seasons  []Season  `json:"seasons,omitempty"`
}

// Season represents a TV season
type Season struct {
	SeasonNumber int    `json:"season_number"`
	EpisodeCount int    `json:"episode_count"`
	Name         string `json:"name,omitempty"`
}

// AIRecommendationItem represents a cached AI recommendation
type AIRecommendationItem struct {
	TmdbID   int    `json:"tmdb_id"`
	Title    string `json:"title"`
	Overview string `json:"overview"`
	Reason   string `json:"reason"`
	Year     int    `json:"year"`
	Rating   float64 `json:"rating"`
	MediaType string `json:"media_type"`
	CachedAt time.Time `json:"cached_at"`
}

// NewManager creates a new session manager
func NewManager(maxAge time.Duration, maxSize int) *Manager {
	m := &Manager{
		sessions: make(map[int64]*Session),
		maxAge:   maxAge,
		maxSize:  maxSize,
	}

	// Start cleanup goroutine
	go m.cleanupLoop()

	return m
}

// GetOrCreate gets or creates a session for a user.
//
// If the session already exists, it updates the LastActivity time and returns it.
// If the session doesn't exist, it creates a new one.
//
// When the session limit (maxSize) is reached:
// 1. First tries to clean up expired sessions
// 2. If still at limit, evicts the oldest inactive session
//
// This method is thread-safe and can be called concurrently.
func (m *Manager) GetOrCreate(userID int64) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	if sess, exists := m.sessions[userID]; exists {
		sess.LastActivity = time.Now()
		return sess
	}

	// Check if we've hit the session limit
	// All operations on m.sessions are atomic within the lock
	if len(m.sessions) >= m.maxSize {
		// Try to clean up expired sessions first
		removed := m.cleanupLocked()
		// If still at limit after cleanup, evict the oldest inactive session
		if len(m.sessions) >= m.maxSize {
			m.evictOldestSession()
		}
		if removed > 0 || len(m.sessions) >= m.maxSize {
			log.Printf("[Session] Session limit reached (%d/%d), cleaned %d, evicted oldest",
				len(m.sessions), m.maxSize, removed)
		}
	}

	sess := &Session{
		UserID:       userID,
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
		Data:         make(map[string]interface{}),
		navStack:     make([]NavEntry, 0, 10),
	}

	m.sessions[userID] = sess
	log.Printf("[Session] Created new session for user %d (total: %d/%d)", userID, len(m.sessions), m.maxSize)

	return sess
}

// GetOrCreateWithCleanup creates a session and triggers cleanup if threshold is reached.
//
// This method checks if the session count is at 80% capacity and triggers
// a proactive cleanup in a separate goroutine if needed. This helps prevent
// memory buildup during high traffic periods before the hard limit is reached.
func (m *Manager) GetOrCreateWithCleanup(userID int64) *Session {
	// Check if we should proactively cleanup (at 80% capacity)
	m.mu.RLock()
	threshold := m.maxSize * 4 / 5
	needsCleanup := len(m.sessions) >= threshold
	m.mu.RUnlock()

	if needsCleanup {
		go func() {
			removed := m.Cleanup()
			if removed > 0 {
				log.Printf("[Session] Proactive cleanup: removed %d expired sessions", removed)
			}
		}()
	}

	return m.GetOrCreate(userID)
}

// Get gets a session if it exists
func (m *Manager) Get(userID int64) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if sess, exists := m.sessions[userID]; exists {
		sess.LastActivity = time.Now()
		return sess
	}

	return nil
}

// Delete deletes a session
func (m *Manager) Delete(userID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if sess, exists := m.sessions[userID]; exists {
		log.Printf("[Session] Deleting session for user %d", userID)
		delete(m.sessions, userID)
		// Note: sess is removed from map, will be garbage collected (no explicit cleanup needed)
		_ = sess
	}
}

// IsValid checks if a user has a valid session
func (m *Manager) IsValid(userID int64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sess, exists := m.sessions[userID]
	if !exists {
		return false
	}

	return time.Since(sess.LastActivity) < m.maxAge
}

// Cleanup removes expired sessions
func (m *Manager) Cleanup() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cleanupLocked()
}

// cleanupLocked removes expired sessions (must be called with lock held)
func (m *Manager) cleanupLocked() int {
	cutoff := time.Now().Add(-m.maxAge)
	removed := 0

	for userID, sess := range m.sessions {
		if sess.LastActivity.Before(cutoff) {
			delete(m.sessions, userID)
			removed++
		}
	}

	if removed > 0 {
		log.Printf("[Session] Cleaned up %d expired sessions", removed)
	}

	return removed
}

// evictOldestSession removes the least recently used session (must be called with lock held)
func (m *Manager) evictOldestSession() {
	if len(m.sessions) == 0 {
		return
	}

	var oldestID int64
	var oldestTime time.Time
	first := true

	for userID, sess := range m.sessions {
		if first || sess.LastActivity.Before(oldestTime) {
			oldestID = userID
			oldestTime = sess.LastActivity
			first = false
		}
	}

	if oldestID != 0 {
		delete(m.sessions, oldestID)
		log.Printf("[Session] Evicted oldest session for user %d (inactive since %v)", oldestID, oldestTime.Format("15:04:05"))
	}
}

// cleanupLoop runs periodic cleanup
func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute) // Reduced from 10 to 5 minutes
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[Session] Panic in cleanup: %v, recovering...", r)
					}
				}()
				m.Cleanup()
			}()
		}
	}
}

// Session methods

// Set sets a value in the session
func (s *Session) Set(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Data[key] = value
}

// Get gets a value from the session
func (s *Session) Get(key string) (interface{}, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	val, exists := s.Data[key]
	return val, exists
}

// GetInt gets an int value from the session
func (s *Session) GetInt(key string) (int, bool) {
	if val, exists := s.Get(key); exists {
		if i, ok := val.(int); ok {
			return i, true
		}
	}
	return 0, false
}

// GetString gets a string value from the session
func (s *Session) GetString(key string) (string, bool) {
	if val, exists := s.Get(key); exists {
		if str, ok := val.(string); ok {
			return str, true
		}
	}
	return "", false
}

// Delete deletes a value from the session
func (s *Session) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.Data, key)
}

// PushNavEntry pushes a navigation entry to the stack
func (s *Session) PushNavEntry(source, query, extra string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := NavEntry{
		Source:    source,
		Query:     query,
		Timestamp: time.Now(),
	}

	s.navStack = append(s.navStack, entry)

	// Limit stack size
	if len(s.navStack) > 20 {
		s.navStack = s.navStack[len(s.navStack)-20:]
	}
}

// PopNavEntry pops a navigation entry from the stack
func (s *Session) PopNavEntry() (NavEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.navStack) == 0 {
		return NavEntry{}, false
	}

	idx := len(s.navStack) - 1
	entry := s.navStack[idx]
	s.navStack = s.navStack[:idx]

	return entry, true
}

// PeekNavEntry peeks at the last navigation entry without removing it
func (s *Session) PeekNavEntry() (NavEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.navStack) == 0 {
		return NavEntry{}, false
	}

	return s.navStack[len(s.navStack)-1], true
}

// ClearNavStack clears the navigation stack
func (s *Session) ClearNavStack() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.navStack = make([]NavEntry, 0, 10)
}

// SetSearchResults stores search results in the session
func (s *Session) SetSearchResults(results []SearchItem, page int, query string) {
	data := map[string]interface{}{
		"items":  results,
		"page":   page,
		"query":  query,
		"cached": time.Now().Unix(),
	}
	s.Set("search_results", data)
}

// GetSearchResults retrieves search results from the session
func (s *Session) GetSearchResults() ([]SearchItem, int, string, bool) {
	val, exists := s.Get("search_results")
	if !exists {
		return nil, 0, "", false
	}

	data, ok := val.(map[string]interface{})
	if !ok {
		return nil, 0, "", false
	}

	items := make([]SearchItem, 0)

	// Try to get items - handle both []interface{} (from JSON) and []SearchItem (direct storage)
	if itemsData, ok := data["items"].([]interface{}); ok {
		// JSON deserialized format
		for _, item := range itemsData {
			if itemMap, ok := item.(map[string]interface{}); ok {
				item := SearchItem{}
				if id, ok := itemMap["id"].(string); ok {
					item.ID = id
				}
				if title, ok := itemMap["title"].(string); ok {
					item.Title = title
				}
				if year, ok := itemMap["year"].(float64); ok {
					item.Year = int(year)
				}
				if typ, ok := itemMap["type"].(string); ok {
					item.Type = typ
				}
				if poster, ok := itemMap["poster"].(string); ok {
					item.Poster = poster
				}
				if rating, ok := itemMap["rating"].(float64); ok {
					item.Rating = rating
				}
				if overview, ok := itemMap["overview"].(string); ok {
					item.Overview = overview
				}
				items = append(items, item)
			}
		}
	} else if itemsData, ok := data["items"].([]SearchItem); ok {
		// Direct slice format (not JSON deserialized)
		items = itemsData
	}

	page := 1
	if p, ok := data["page"].(float64); ok {
		page = int(p)
	}

	query := ""
	if q, ok := data["query"].(string); ok {
		query = q
	}

	return items, page, query, len(items) > 0
}

// CacheAIItem caches an AI recommendation item
func (s *Session) CacheAIItem(item *AIRecommendationItem) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Initialize AI cache if needed
	if s.Data["ai_cache"] == nil {
		s.Data["ai_cache"] = make(map[int]*AIRecommendationItem)
	}

	cache, ok := s.Data["ai_cache"].(map[int]*AIRecommendationItem)
	if !ok {
		return
	}

	item.CachedAt = time.Now()
	cache[item.TmdbID] = item

	// LRU eviction: clean old cache entries when exceeding limit
	const maxAICacheSize = 50
	if len(cache) > maxAICacheSize {
		// Find and remove the oldest entry (true LRU)
		var oldestKey int
		var oldestTime time.Time
		for k, v := range cache {
			if oldestTime.IsZero() || v.CachedAt.Before(oldestTime) {
				oldestTime = v.CachedAt
				oldestKey = k
			}
		}
		if oldestKey != 0 {
			delete(cache, oldestKey)
		}
	}
}

// GetCachedAIItem retrieves a cached AI recommendation item
func (s *Session) GetCachedAIItem(tmdbID int) *AIRecommendationItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cache, ok := s.Data["ai_cache"].(map[int]*AIRecommendationItem)
	if !ok {
		return nil
	}

	return cache[tmdbID]
}

// CacheAIResults caches multiple AI recommendation items
func (s *Session) CacheAIResults(items []*AIRecommendationItem) {
	for _, item := range items {
		s.CacheAIItem(item)
	}
}

// SetJellyseerrUserID stores the Jellyseerr user ID mapping
func (s *Session) SetJellyseerrUserID(jellyseerrID int64) {
	s.Set("jellyseerr_id", jellyseerrID)
}

// GetJellyseerrUserID retrieves the Jellyseerr user ID
// Deprecated: Use GetMoviePilotUserID instead
func (s *Session) GetJellyseerrUserID() int64 {
	return s.GetMoviePilotUserID()
}

// GetMoviePilotUserID retrieves the MoviePilot user ID
func (s *Session) GetMoviePilotUserID() int64 {
	if id, ok := s.GetInt("moviepilot_id"); ok {
		return int64(id)
	}
	// Fallback to legacy field
	if id, ok := s.GetInt("jellyseerr_id"); ok {
		return int64(id)
	}
	return 0
}

// ToJSON exports the session to JSON (for debugging)
func (s *Session) ToJSON() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data := map[string]interface{}{
		"user_id":        s.UserID,
		"created_at":     s.CreatedAt,
		"last_activity":  s.LastActivity,
		"data":           s.Data,
		"nav_stack_size": len(s.navStack),
	}

	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}

	return string(bytes), nil
}

// Size returns the approximate size of the session in bytes
func (s *Session) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Base overhead for Session struct
	size := 200 // Base struct overhead (userID, timestamps, maps, etc.)

	// Calculate Data map size
	for k, v := range s.Data {
		size += len(k) + 16 // Key + pointer overhead
		switch val := v.(type) {
		case string:
			size += len(val)
		case int:
			size += 8
		case int64:
			size += 8
		case bool:
			size += 1
		case map[int]*AIRecommendationItem:
			size += len(val) * 200 // Approximate per AI item
		case map[string]interface{}:
			size += len(val) * 100 // Approximate per entry
		case []SearchItem:
			size += len(val) * 250 // Approximate per search item
		default:
			size += 50 // Fallback for unknown types
		}
	}

	// NavStack size
	size += len(s.navStack) * 100 // Approximate per nav entry

	return size
}

// Stats returns session statistics
func (m *Manager) Stats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	totalSize := 0
	for _, sess := range m.sessions {
		totalSize += sess.Size()
	}

	return map[string]interface{}{
		"total_sessions": len(m.sessions),
		"total_size":     totalSize,
		"max_size":       m.maxSize,
		"max_age_hours":  m.maxAge.Hours(),
	}
}

func (s *Session) String() string {
	return fmt.Sprintf("Session{UserID=%d, CreatedAt=%v, LastActivity=%v}",
		s.UserID, s.CreatedAt, s.LastActivity)
}
