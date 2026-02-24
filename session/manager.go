package session

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// SessionManager manages user sessions
type SessionManager struct {
	sessions map[int64]*UserSession
	mu       sync.RWMutex
	stopChan chan struct{}
	stopOnce sync.Once
	stopped  bool
}

// UserSession represents a user's session state
type UserSession struct {
	UserID         int64
	ChatID         int64
	LastActive     time.Time
	LastMessageID  int64
	CurrentPage    int
	SearchQuery    string
	SearchResults  []SearchItem
	TotalResults   int
	SelectedItem   *SearchItem
	PendingAction  string
	Context        map[string]interface{}
	// 🎯 导航历史 - 支持从AI推荐详情页返回列表
	NavHistory     []NavEntry

	mu sync.RWMutex
}

// NavEntry represents a navigation history entry
type NavEntry struct {
	Source   string // "trending", "hot_tv", "new_movies", "search", etc.
	Message  string // The cached message to restore
	Keyboard *string // JSON string of the keyboard (cached)
	Timestamp int64
}

// SearchItem represents a search result item
type SearchItem struct {
	ID          string
	Title       string
	Year        int
	Type        string
	Poster      string
	Rating      float64
	Seasons     []int
	Episodes    map[int]int
	Overview    string
	ReleaseDate string
	Genres      []string
	Runtime     int
	Status      string
	Popularity  float64
}

// NewSessionManager creates a new session manager
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[int64]*UserSession),
		stopChan: make(chan struct{}),
	}
}

// GetOrCreateSession gets an existing session or creates a new one
func (sm *SessionManager) GetOrCreateSession(userID, chatID int64) *UserSession {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if session, exists := sm.sessions[userID]; exists {
		// Update chat ID in case it changed
		session.ChatID = chatID
		session.LastActive = time.Now()
		return session
	}

	session := &UserSession{
		UserID:        userID,
		ChatID:        chatID,
		LastActive:    time.Now(),
		CurrentPage:   0,
		SearchResults: []SearchItem{},
		Context:       make(map[string]interface{}),
	}

	sm.sessions[userID] = session
	log.Printf("[Session] Created new session for user %d", userID)

	return session
}

// GetSession gets an existing session
func (sm *SessionManager) GetSession(userID int64) *UserSession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return sm.sessions[userID]
}

// DeleteSession deletes a user session
func (sm *SessionManager) DeleteSession(userID int64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, exists := sm.sessions[userID]; exists {
		delete(sm.sessions, userID)
		log.Printf("[Session] Deleted session for user %d", userID)
	}
}

// UpdateActivity updates the last active time for a session
func (s *UserSession) UpdateActivity() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.LastActive = time.Now()
}

// SetSearchResults stores search results in the session
func (s *UserSession) SetSearchResults(query string, results []SearchItem, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.SearchQuery = query
	s.SearchResults = results
	s.TotalResults = total
	s.CurrentPage = 0
}

// GetSearchResults gets the current search results
func (s *UserSession) GetSearchResults() (query string, results []SearchItem, page, total int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.SearchQuery, s.SearchResults, s.CurrentPage, s.TotalResults
}

// SetSelectedItem sets the selected item
func (s *UserSession) SetSelectedItem(item *SearchItem) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.SelectedItem = item
}

// GetSelectedItem gets the selected item
func (s *UserSession) GetSelectedItem() *SearchItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.SelectedItem
}

// SetContext sets a context value
func (s *UserSession) SetContext(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Context == nil {
		s.Context = make(map[string]interface{})
	}
	s.Context[key] = value
}

// GetContext gets a context value
func (s *UserSession) GetContext(key string) (interface{}, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	val, exists := s.Context[key]
	return val, exists
}

// IsExpired checks if the session is expired
func (s *UserSession) IsExpired(timeout time.Duration) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return time.Since(s.LastActive) > timeout
}

// StartCleanup starts the background cleanup routine
func (sm *SessionManager) StartCleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	timeout := 30 * time.Minute

	log.Printf("[Session] Started cleanup routine (timeout: %v)", timeout)

	for {
		select {
		case <-ticker.C:
			sm.cleanupExpired(timeout)
		case <-sm.stopChan:
			ticker.Stop()
			return
		}
	}
}

// cleanupExpired removes expired sessions
func (sm *SessionManager) cleanupExpired(timeout time.Duration) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	expiredUsers := []int64{}

	for userID, session := range sm.sessions {
		if now.Sub(session.LastActive) > timeout {
			expiredUsers = append(expiredUsers, userID)
		}
	}

	for _, userID := range expiredUsers {
		delete(sm.sessions, userID)
		log.Printf("[Session] Cleaned up expired session for user %d", userID)
	}

	if len(expiredUsers) > 0 {
		log.Printf("[Session] Cleaned up %d expired sessions", len(expiredUsers))
	}
}

// Stop stops the session manager
func (sm *SessionManager) Stop() {
	sm.stopOnce.Do(func() {
		sm.mu.Lock()
		sm.stopped = true
		sm.mu.Unlock()
		close(sm.stopChan)
	})
}

// GetActiveSessionCount returns the number of active sessions
func (sm *SessionManager) GetActiveSessionCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return len(sm.sessions)
}

// GetAllSessions returns all sessions (for debugging/admin)
func (sm *SessionManager) GetAllSessions() []*UserSession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	sessions := make([]*UserSession, 0, len(sm.sessions))
	for _, session := range sm.sessions {
		sessions = append(sessions, session)
	}

	return sessions
}

// PushNavEntry pushes a navigation entry onto the history stack
func (s *UserSession) PushNavEntry(source, message string, keyboardJSON string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.NavHistory == nil {
		s.NavHistory = make([]NavEntry, 0, 5)
	}

	entry := NavEntry{
		Source:    source,
		Message:   message,
		Keyboard:  &keyboardJSON,
		Timestamp: time.Now().Unix(),
	}

	// Keep only last 5 entries
	s.NavHistory = append([]NavEntry{entry}, s.NavHistory...)
	if len(s.NavHistory) > 5 {
		s.NavHistory = s.NavHistory[:5]
	}

	log.Printf("[Session] Pushed nav entry: source=%s, history_len=%d", source, len(s.NavHistory))
}

// PopNavEntry pops the most recent navigation entry
func (s *UserSession) PopNavEntry() (NavEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.NavHistory) == 0 {
		return NavEntry{}, false
	}

	entry := s.NavHistory[0]
	s.NavHistory = s.NavHistory[1:]
	log.Printf("[Session] Popped nav entry: source=%s, remaining=%d", entry.Source, len(s.NavHistory))
	return entry, true
}

// PeekNavEntry peeks at the most recent navigation entry without removing it
func (s *UserSession) PeekNavEntry() (NavEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.NavHistory) == 0 {
		return NavEntry{}, false
	}

	return s.NavHistory[0], true
}

// ClearNavHistory clears the navigation history
func (s *UserSession) ClearNavHistory() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.NavHistory = nil
	log.Printf("[Session] Cleared nav history")
}

// AIRecommendationItem represents a cached AI recommendation item
type AIRecommendationItem struct {
	TmdbID    int
	Title     string
	Overview  string
	Reason    string
	Year      int
	Rating    float64
	MediaType string
}

// CacheAIItem caches an AI recommendation item for detail view
func (s *UserSession) CacheAIItem(item *AIRecommendationItem) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Context == nil {
		s.Context = make(map[string]interface{})
	}

	// Store in context map with TMDB ID as key
	cacheKey := fmt.Sprintf("ai_item_%d", item.TmdbID)
	s.Context[cacheKey] = item

	// Also maintain a list of recently cached items for cleanup
	var cachedList []int
	if val, ok := s.Context["ai_cached_items"].([]int); ok {
		cachedList = val
	}
	cachedList = append(cachedList, item.TmdbID)

	// Keep only last 20 items
	if len(cachedList) > 20 {
		cachedList = cachedList[len(cachedList)-20:]
	}
	s.Context["ai_cached_items"] = cachedList

	log.Printf("[Session] Cached AI item: TMDB=%d, Title=%s", item.TmdbID, item.Title)
}

// GetCachedAIItem retrieves a cached AI recommendation item
func (s *UserSession) GetCachedAIItem(tmdbID int) *AIRecommendationItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.Context == nil {
		return nil
	}

	cacheKey := fmt.Sprintf("ai_item_%d", tmdbID)
	if item, exists := s.Context[cacheKey].(*AIRecommendationItem); exists {
		return item
	}

	return nil
}

// CacheAIResults caches multiple AI recommendation items at once
func (s *UserSession) CacheAIResults(results interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Context == nil {
		s.Context = make(map[string]interface{})
	}

	// Handle different result types
	switch items := results.(type) {
	case []*AIRecommendationItem:
		for _, item := range items {
			cacheKey := fmt.Sprintf("ai_item_%d", item.TmdbID)
			s.Context[cacheKey] = item
		}
		log.Printf("[Session] Cached %d AI items", len(items))
	default:
		log.Printf("[Session] Warning: unknown results type %T in StoreAIRecommendations", results)
	}
}
