package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// SearchHistoryManager manages user search history
type SearchHistoryManager struct {
	histories  map[int64]*UserSearchHistory // telegramID -> history
	historyMap map[string][]int64            // query -> telegramIDs (reverse index for trending)
	mutex      sync.RWMutex
	storageFile string
	maxEntries int // Max entries per user
}

// UserSearchHistory represents a user's search history
type UserSearchHistory struct {
	TelegramID int64          `json:"telegramId"`
	Entries    []SearchEntry `json:"entries"`
	UpdatedAt  time.Time     `json:"updatedAt"`
}

// SearchEntry represents a single search entry
type SearchEntry struct {
	Query     string    `json:"query"`
	Timestamp time.Time `json:"timestamp"`
	MediaType string    `json:"mediaType,omitempty"` // "movie", "tv", or ""
	Count     int       `json:"count"`                // Search frequency
}

// SearchHistoryData stores all search histories
type SearchHistoryData struct {
	Histories  map[int64]*UserSearchHistory `json:"histories"`
	LastSync   string                       `json:"lastSync"`
}

var searchHistoryMgr *SearchHistoryManager

// InitSearchHistoryManager initializes the search history manager
func InitSearchHistoryManager() {
	searchHistoryMgr = &SearchHistoryManager{
		histories:   make(map[int64]*UserSearchHistory),
		historyMap:  make(map[string][]int64),
		storageFile: "search_history.json",
		maxEntries:  50, // Keep last 50 searches per user
	}

	// Load existing data
	searchHistoryMgr.load()

	// Start cleanup routine
	go searchHistoryMgr.cleanupOld()

	log.Println("SearchHistory manager initialized")
}

// AddSearch adds a search to history
func (m *SearchHistoryManager) AddSearch(telegramID int64, query, mediaType string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Get or create user history
	history, exists := m.histories[telegramID]
	if !exists {
		history = &UserSearchHistory{
			TelegramID: telegramID,
			Entries:    []SearchEntry{},
		}
		m.histories[telegramID] = history
	}

	// Check if query already exists
	found := false
	now := time.Now()
	for i, entry := range history.Entries {
		if entry.Query == query {
			// Update existing entry
			history.Entries[i].Timestamp = now
			history.Entries[i].Count++
			if mediaType != "" {
				history.Entries[i].MediaType = mediaType
			}
			found = true

			// Move to front (most recent)
			if i > 0 {
				history.Entries = append([]SearchEntry{history.Entries[i]}, append(history.Entries[:i], history.Entries[i+1:]...)...)
			}
			break
		}
	}

	// Add new entry if not found
	if !found {
		newEntry := SearchEntry{
			Query:     query,
			Timestamp: now,
			MediaType: mediaType,
			Count:     1,
		}
		history.Entries = append([]SearchEntry{newEntry}, history.Entries...)

		// Update reverse index
		m.historyMap[query] = append(m.historyMap[query], telegramID)
	}

	// Trim to max entries
	if len(history.Entries) > m.maxEntries {
		history.Entries = history.Entries[:m.maxEntries]
	}

	history.UpdatedAt = now
	m.save()

	log.Printf("SearchHistory: Added search for user %d: %s", telegramID, query)
}

// GetUserHistory gets a user's search history
func (m *SearchHistoryManager) GetUserHistory(telegramID int64, limit int) []SearchEntry {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	history, exists := m.histories[telegramID]
	if !exists || len(history.Entries) == 0 {
		return nil
	}

	if limit <= 0 || limit > len(history.Entries) {
		limit = len(history.Entries)
	}

	return history.Entries[:limit]
}

// GetRecentSearches gets recent searches across all users (for trending)
func (m *SearchHistoryManager) GetRecentSearches(limit int) []SearchEntry {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// Collect all recent searches
	allEntries := []SearchEntry{}
	cutoff := time.Now().Add(-7 * 24 * time.Hour) // Last 7 days

	for _, history := range m.histories {
		for _, entry := range history.Entries {
			if entry.Timestamp.After(cutoff) {
				allEntries = append(allEntries, entry)
			}
		}
	}

	// Sort by frequency (count) and recency
	// For simplicity, just return top by count
	// A more sophisticated implementation would use a proper ranking algorithm

	return allEntries
}

// GetTrendingSearches returns trending searches based on frequency
func (m *SearchHistoryManager) GetTrendingSearches(limit int) []TrendingSearch {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// Aggregate searches by query
	queryCounts := make(map[string]int)
	queryTypes := make(map[string]string) // Store media type

	for _, history := range m.histories {
		for _, entry := range history.Entries {
			// Only count searches from last 7 days
			if time.Since(entry.Timestamp) < 7*24*time.Hour {
				queryCounts[entry.Query] += entry.Count
				if queryTypes[entry.Query] == "" && entry.MediaType != "" {
					queryTypes[entry.Query] = entry.MediaType
				}
			}
		}
	}

	// Convert to slice and sort
	trending := make([]TrendingSearch, 0, len(queryCounts))
	for query, count := range queryCounts {
		trending = append(trending, TrendingSearch{
			Query:     query,
			Count:     count,
			MediaType: queryTypes[query],
		})
	}

	// Sort by count (descending)
	for i := 0; i < len(trending)-1; i++ {
		for j := i + 1; j < len(trending); j++ {
			if trending[j].Count > trending[i].Count {
				trending[i], trending[j] = trending[j], trending[i]
			}
		}
	}

	if limit > 0 && limit < len(trending) {
		trending = trending[:limit]
	}

	return trending
}

// TrendingSearch represents a trending search query
type TrendingSearch struct {
	Query     string `json:"query"`
	Count     int    `json:"count"`
	MediaType string `json:"mediaType"`
}

// ClearUserHistory clears a user's search history
func (m *SearchHistoryManager) ClearUserHistory(telegramID int64) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, exists := m.histories[telegramID]; exists {
		delete(m.histories, telegramID)
		m.save()
		log.Printf("SearchHistory: Cleared history for user %d", telegramID)
		return true
	}
	return false
}

// RemoveEntry removes a specific search entry
func (m *SearchHistoryManager) RemoveEntry(telegramID int64, query string) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	history, exists := m.histories[telegramID]
	if !exists {
		return false
	}

	for i, entry := range history.Entries {
		if entry.Query == query {
			history.Entries = append(history.Entries[:i], history.Entries[i+1:]...)
			m.save()
			return true
		}
	}

	return false
}

// cleanupOld removes old search entries periodically
func (m *SearchHistoryManager) cleanupOld() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		m.mutex.Lock()
		func() {
			defer m.mutex.Unlock() // Ensure unlock even if panic occurs
			cutoff := time.Now().Add(-30 * 24 * time.Hour) // 30 days ago

			for _, history := range m.histories {
				newEntries := []SearchEntry{}
				for _, entry := range history.Entries {
					if entry.Timestamp.After(cutoff) {
						newEntries = append(newEntries, entry)
					}
				}
				history.Entries = newEntries
			}

			m.save()
		}()

		log.Println("SearchHistory: Cleaned up old entries")
	}
}

// save saves search history to file
func (m *SearchHistoryManager) save() {
	data := SearchHistoryData{
		Histories: m.histories,
		LastSync:  time.Now().Format(time.RFC3339),
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Printf("SearchHistory: Failed to marshal data: %v", err)
		return
	}

	if err := os.WriteFile(m.storageFile, jsonData, 0644); err != nil {
		log.Printf("SearchHistory: Failed to save data: %v", err)
	}
}

// load loads search history from file
func (m *SearchHistoryManager) load() {
	data, err := os.ReadFile(m.storageFile)
	if err != nil {
		log.Printf("SearchHistory: Data file not found, starting fresh: %v", err)
		return
	}

	var loaded SearchHistoryData
	if err := json.Unmarshal(data, &loaded); err != nil {
		log.Printf("SearchHistory: Failed to load data: %v", err)
		return
	}

	// Fix nil map issue
	if loaded.Histories != nil {
		m.histories = loaded.Histories
	} else {
		m.histories = make(map[int64]*UserSearchHistory)
	}
	log.Printf("SearchHistory: Loaded %d user histories", len(m.histories))
}

// FormatUserHistory formats user search history for display
func FormatUserHistory(telegramID int64) string {
	if searchHistoryMgr == nil {
		return "搜索历史功能未初始化"
	}

	entries := searchHistoryMgr.GetUserHistory(telegramID, 15)
	if len(entries) == 0 {
		return "📜 *搜索历史*\n\n暂无搜索记录"
	}

	msg := "📜 *最近搜索*\n\n"
	msg += fmt.Sprintf("共 %d 条记录\n\n", len(entries))

	for i, entry := range entries {
		emoji := "🔍"
		if entry.MediaType == "movie" {
			emoji = "🎬"
		} else if entry.MediaType == "tv" {
			emoji = "📺"
		}

		timeStr := formatTimeAgo(entry.Timestamp)
		msg += fmt.Sprintf("%d. %s %s", i+1, emoji, entry.Query)
		if entry.Count > 1 {
			msg += fmt.Sprintf(" (×%d)", entry.Count)
		}
		msg += fmt.Sprintf("\n   _%s_\n", timeStr)
	}

	msg += "\n💡 点击任意条目可重新搜索"

	return msg
}

// FormatTrending formats trending searches for display
func FormatTrendingSearches(limit int) string {
	if searchHistoryMgr == nil {
		return "搜索历史功能未初始化"
	}

	trending := searchHistoryMgr.GetTrendingSearches(limit)
	if len(trending) == 0 {
		return "🔥 *热门搜索*\n\n暂无数据"
	}

	msg := "🔥 *热门搜索*\n\n"
	msg += "基于大家最近 7 天的搜索\n\n"

	for i, item := range trending {
		emoji := "🔍"
		if item.MediaType == "movie" {
			emoji = "🎬"
		} else if item.MediaType == "tv" {
			emoji = "📺"
		}

		msg += fmt.Sprintf("%d. %s %s", i+1, emoji, item.Query)
		msg += fmt.Sprintf(" _(%d次)_\n", item.Count)
	}

	return msg
}

// formatTimeAgo formats a timestamp as relative time
func formatTimeAgo(t time.Time) string {
	duration := time.Since(t)

	if duration < time.Minute {
		return "刚刚"
	} else if duration < time.Hour {
		return fmt.Sprintf("%d分钟前", int(duration.Minutes()))
	} else if duration < 24*time.Hour {
		hours := int(duration.Hours())
		if hours == 1 {
			return "1小时前"
		}
		return fmt.Sprintf("%d小时前", hours)
	} else if duration < 7*24*time.Hour {
		days := int(duration.Hours()) / 24
		if days == 1 {
			return "昨天"
		}
		return fmt.Sprintf("%d天前", days)
	} else {
		return t.Format("01-02")
	}
}
