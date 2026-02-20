package session

import (
	"log"
	"sync"
	"time"
)

// SessionManager manages user sessions
type SessionManager struct {
	sessions map[int64]*UserSession
	mu       sync.RWMutex
	stopChan chan struct{}
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

	mu sync.RWMutex
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
	close(sm.stopChan)
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
