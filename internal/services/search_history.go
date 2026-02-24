package services

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// SearchHistoryService manages search history for users
type SearchHistoryService struct {
	historyFile string
	history     map[int64][]SearchEntry // telegramID -> search entries
	mu          sync.RWMutex
	maxPerUser  int
}

// SearchEntry represents a search history entry
type SearchEntry struct {
	Query     string    `json:"query"`
	Timestamp time.Time `json:"timestamp"`
	Count     int       `json:"count"` // Number of times searched
}

// NewSearchHistoryService creates a new search history service
func NewSearchHistoryService(dataDir string) *SearchHistoryService {
	historyFile := fmt.Sprintf("%s/search_history.json", dataDir)

	service := &SearchHistoryService{
		historyFile: historyFile,
		history:     make(map[int64][]SearchEntry),
		maxPerUser:  20, // Keep last 20 searches per user
	}

	service.load()

	return service
}

// load loads search history from file
func (s *SearchHistoryService) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.historyFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if err := json.Unmarshal(data, &s.history); err != nil {
		return err
	}

	log.Printf("[SearchHistory] Loaded history for %d users", len(s.history))
	return nil
}

// save saves search history to file
func (s *SearchHistoryService) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(s.history, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.historyFile, data, 0644)
}

// AddSearch adds a search query to history
func (s *SearchHistoryService) AddSearch(telegramID int64, query string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries := s.history[telegramID]

	// Check if this query already exists
	found := false
	for i, entry := range entries {
		if entry.Query == query {
			entries[i].Count++
			entries[i].Timestamp = time.Now()
			found = true
			break
		}
	}

	if !found {
		// Add new entry at the beginning
		entries = append([]SearchEntry{{
			Query:     query,
			Timestamp: time.Now(),
			Count:     1,
		}}, entries...)
	}

	// Limit to max entries
	if len(entries) > s.maxPerUser {
		entries = entries[:s.maxPerUser]
	}

	s.history[telegramID] = entries

	// Save after releasing lock - copy data to avoid race
	historyCopy := make(map[int64][]SearchEntry)
	for k, v := range s.history {
		historyCopy[k] = v
	}
	go func() {
		s.saveAsync(historyCopy)
	}()
}

// saveAsync saves history without holding the lock
func (s *SearchHistoryService) saveAsync(history map[int64][]SearchEntry) {
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		log.Printf("[SearchHistory] Failed to marshal: %v", err)
		return
	}

	if err := os.WriteFile(s.historyFile, data, 0644); err != nil {
		log.Printf("[SearchHistory] Failed to save: %v", err)
	}
}

// GetHistory gets search history for a user
func (s *SearchHistoryService) GetHistory(telegramID int64) []SearchEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, exists := s.history[telegramID]
	if !exists {
		return []SearchEntry{}
	}

	// Return a copy
	result := make([]SearchEntry, len(entries))
	copy(result, entries)
	return result
}

// GetSuggestions gets search suggestions based on query
func (s *SearchHistoryService) GetSuggestions(telegramID int64, query string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if query == "" {
		// Return recent searches
		entries := s.history[telegramID]
		if len(entries) > 5 {
			entries = entries[:5]
		}
		suggestions := make([]string, len(entries))
		for i, entry := range entries {
			suggestions[i] = entry.Query
		}
		return suggestions
	}

	// Return matching searches
	entries := s.history[telegramID]
	var suggestions []string

	queryLower := searchLower(query)
	for _, entry := range entries {
		if searchContains(searchLower(entry.Query), queryLower) {
			suggestions = append(suggestions, entry.Query)
		}
		if len(suggestions) >= 5 {
			break
		}
	}

	return suggestions
}

// ClearHistory clears search history for a user
func (s *SearchHistoryService) ClearHistory(telegramID int64) error {
	s.mu.Lock()
	delete(s.history, telegramID)

	// Copy data for saving after releasing lock
	historyCopy := make(map[int64][]SearchEntry)
	for k, v := range s.history {
		historyCopy[k] = v
	}
	s.mu.Unlock()

	// Save without holding the lock
	s.saveAsync(historyCopy)
	return nil
}

// Helper functions for case-insensitive search
func searchLower(s string) string {
	// Simple lowercase conversion
	var result []rune
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			result = append(result, r+32)
		} else {
			result = append(result, r)
		}
	}
	return string(result)
}

func searchContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && len(substr) > 0 && findSearchSubstring(s, substr))
}

func findSearchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
