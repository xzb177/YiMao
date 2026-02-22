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
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Year     int     `json:"year"`
	Type     string  `json:"type"`
	Poster   string  `json:"poster,omitempty"`
	Rating   float64 `json:"rating,omitempty"`
	Overview string  `json:"overview,omitempty"`
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

// GetOrCreate gets or creates a session for a user
func (m *Manager) GetOrCreate(userID int64) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	if sess, exists := m.sessions[userID]; exists {
		sess.LastActivity = time.Now()
		return sess
	}

	sess := &Session{
		UserID:       userID,
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
		Data:         make(map[string]interface{}),
		navStack:     make([]NavEntry, 0, 10),
	}

	m.sessions[userID] = sess
	log.Printf("[Session] Created new session for user %d", userID)

	return sess
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

// cleanupLoop runs periodic cleanup
func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		m.Cleanup()
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

	// Clean old cache entries (keep only last 50)
	if len(cache) > 50 {
		// Remove oldest entries
		var oldestKey int
		var oldestTime time.Time
		for k, v := range cache {
			if oldestTime.IsZero() || v.CachedAt.Before(oldestTime) {
				oldestTime = v.CachedAt
				oldestKey = k
			}
		}
		delete(cache, oldestKey)
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

	size := 100 // Base overhead
	for k, v := range s.Data {
		size += len(k) + 16 // Key + pointer overhead
		if str, ok := v.(string); ok {
			size += len(str)
		} else if items, ok := v.([]SearchItem); ok {
			size += len(items) * 200 // Approximate per item
		}
	}

	size += len(s.navStack) * 100

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
