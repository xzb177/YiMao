package services

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

// SearchHistoryDB handles search history with SQLite backend
type SearchHistoryDB struct {
	db *sql.DB
}

// NewSearchHistoryDB creates a new SQLite-based search history service
func NewSearchHistoryDB(dataDir string) (*SearchHistoryDB, error) {
	dbPath := fmt.Sprintf("%s/search_history.db", dataDir)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Create tables
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS search_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			query TEXT NOT NULL,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			count INTEGER DEFAULT 1,
			tags TEXT,
			media_id INTEGER,
			media_type TEXT
		);

		CREATE INDEX IF NOT EXISTS idx_user_id ON search_history(user_id);
		CREATE INDEX IF NOT EXISTS idx_timestamp ON search_history(timestamp DESC);
		CREATE INDEX IF NOT EXISTS idx_query ON search_history(query);
		CREATE INDEX IF NOT EXISTS idx_user_timestamp ON search_history(user_id, timestamp DESC);
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	log.Printf("[SearchHistoryDB] Initialized database at %s", dbPath)

	return &SearchHistoryDB{db: db}, nil
}

// AddSearch adds a search query to history
func (s *SearchHistoryDB) AddSearch(telegramID int64, query string) error {
	now := time.Now()

	// Check if query exists for this user
	var id int
	err := s.db.QueryRow(
		"SELECT id FROM search_history WHERE user_id = ? AND query = ? ORDER BY timestamp DESC LIMIT 1",
		telegramID, query,
	).Scan(&id)

	if err == sql.ErrNoRows {
		// Insert new entry
		_, err = s.db.Exec(
			"INSERT INTO search_history (user_id, query, timestamp, count) VALUES (?, ?, ?, 1)",
			telegramID, query, now,
		)
	} else if err == nil {
		// Update existing entry
		_, err = s.db.Exec(
			"UPDATE search_history SET count = count + 1, timestamp = ? WHERE id = ?",
			now, id,
		)
	}

	if err != nil {
		return fmt.Errorf("failed to add search: %w", err)
	}

	// Clean up old entries (keep last 20 per user)
	go s.cleanupOldEntries(telegramID, 20)

	return nil
}

// cleanupOldEntries removes old search entries beyond the limit
func (s *SearchHistoryDB) cleanupOldEntries(userID int64, limit int) {
	_, err := s.db.Exec(`
		DELETE FROM search_history
		WHERE id NOT IN (
			SELECT id FROM search_history
			WHERE user_id = ?
			ORDER BY timestamp DESC
			LIMIT ?
		)
	`, userID, limit)

	if err != nil {
		log.Printf("[SearchHistoryDB] Failed to cleanup old entries: %v", err)
	}
}

// GetHistory gets search history for a user
func (s *SearchHistoryDB) GetHistory(userID int64, limit int) ([]SearchEntry, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.Query(
		"SELECT query, timestamp, count FROM search_history WHERE user_id = ? ORDER BY timestamp DESC LIMIT ?",
		userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get history: %w", err)
	}
	defer rows.Close()

	var entries []SearchEntry
	for rows.Next() {
		var entry SearchEntry
		if err := rows.Scan(&entry.Query, &entry.Timestamp, &entry.Count); err != nil {
			return nil, fmt.Errorf("failed to scan entry: %w", err)
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// GetHistoryGrouped gets search history grouped by time periods
func (s *SearchHistoryDB) GetHistoryGrouped(userID int64) (map[string][]SearchEntry, error) {
	entries, err := s.GetHistory(userID, 0)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	grouped := make(map[string][]SearchEntry)

	for _, entry := range entries {
		diff := now.Sub(entry.Timestamp)

		var group string
		if diff.Hours() < 24 {
			group = "今天"
		} else if diff.Hours() < 24*7 {
			group = "本周"
		} else if diff.Hours() < 24*30 {
			group = "本月"
		} else {
			group = "更早"
		}

		grouped[group] = append(grouped[group], entry)
	}

	return grouped, nil
}

// GetEntry gets a specific search entry by index
func (s *SearchHistoryDB) GetEntry(userID int64, index int) (*SearchEntry, error) {
	entries, err := s.GetHistory(userID, 0)
	if err != nil {
		return nil, err
	}

	if index < 0 || index >= len(entries) {
		return nil, fmt.Errorf("invalid index: %d", index)
	}

	return &entries[index], nil
}

// DeleteEntry removes a specific search entry
func (s *SearchHistoryDB) DeleteEntry(userID int64, index int) error {
	entries, err := s.GetHistory(userID, 0)
	if err != nil {
		return err
	}

	if index < 0 || index >= len(entries) {
		return fmt.Errorf("invalid index: %d", index)
	}

	target := entries[index]

	_, err = s.db.Exec(
		"DELETE FROM search_history WHERE user_id = ? AND query = ? AND timestamp = ?",
		userID, target.Query, target.Timestamp,
	)

	return err
}

// ClearHistory clears all search history for a user
func (s *SearchHistoryDB) ClearHistory(userID int64) error {
	_, err := s.db.Exec(
		"DELETE FROM search_history WHERE user_id = ?",
		userID,
	)

	if err != nil {
		return fmt.Errorf("failed to clear history: %w", err)
	}

	log.Printf("[SearchHistoryDB] Cleared history for user %d", userID)
	return nil
}

// GetStats gets search statistics for a user
func (s *SearchHistoryDB) GetStats(userID int64) (*SearchStats, error) {
	entries, err := s.GetHistory(userID, 0)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	stats := &SearchStats{
		Total: 0,
		Week:  0,
		Month: 0,
		Top5:  make([]string, 0, 5),
	}

	// Count by time period
	for _, entry := range entries {
		stats.Total += entry.Count
		diff := now.Sub(entry.Timestamp)

		if diff.Hours() < 24*7 {
			stats.Week += entry.Count
		}
		if diff.Hours() < 24*30 {
			stats.Month += entry.Count
		}
	}

	// Get top 5 searches
	searchCounts := make(map[string]int)
	for _, entry := range entries {
		searchCounts[entry.Query] += entry.Count
	}

	type QueryCount struct {
		Query string
		Count int
	}

	var sorted []QueryCount
	for query, count := range searchCounts {
		sorted = append(sorted, QueryCount{query, count})
	}

	// Sort by count (descending)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i].Count < sorted[j].Count {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Get top 5
	for i := 0; i < len(sorted) && i < 5; i++ {
		stats.Top5 = append(stats.Top5, sorted[i].Query)
	}

	return stats, nil
}

// SearchStats represents search statistics
type SearchStats struct {
	Total int
	Week  int
	Month int
	Top5  []string
}

// PopularSearch represents a popular search across all users
type PopularSearch struct {
	Query string
	Count int
}

// GetPopularSearches gets the most popular searches across all users
func (s *SearchHistoryDB) GetPopularSearches(limit int) ([]PopularSearch, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := s.db.Query(`
		SELECT query, SUM(count) as total
		FROM search_history
		WHERE timestamp > datetime('now', '-30 days')
		GROUP BY query
		ORDER BY total DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get popular searches: %w", err)
	}
	defer rows.Close()

	var results []PopularSearch
	for rows.Next() {
		var result PopularSearch
		if err := rows.Scan(&result.Query, &result.Count); err != nil {
			return nil, fmt.Errorf("failed to scan popular search: %w", err)
		}
		results = append(results, result)
	}

	return results, nil
}

// TrendItem represents a search trend
type TrendItem struct {
	Query    string
	Count     int
	Yesterday int
	Growth    float64
}

// GetSearchTrends gets trending searches
func (s *SearchHistoryDB) GetSearchTrends(days int) ([]TrendItem, error) {
	if days <= 0 {
		days = 7
	}

	cutoff := time.Now().AddDate(0, 0, -days)
	yesterday := cutoff.AddDate(0, 0, -1)

	// Get recent searches
	recentRows, err := s.db.Query(`
		SELECT query, SUM(count) as total
		FROM search_history
		WHERE timestamp > ?
		GROUP BY query
	`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to get recent searches: %w", err)
	}
	defer recentRows.Close()

	recentCounts := make(map[string]int)
	var queries []string
	for recentRows.Next() {
		var query string
		var count int
		if err := recentRows.Scan(&query, &count); err != nil {
			return nil, fmt.Errorf("failed to scan recent search: %w", err)
		}
		recentCounts[query] = count
		queries = append(queries, query)
	}

	// Get yesterday counts for comparison
	yesterdayCounts := make(map[string]int)
	for _, query := range queries {
		var count int
		err := s.db.QueryRow(`
			SELECT COALESCE(SUM(count), 0)
			FROM search_history
			WHERE query = ? AND timestamp > ? AND timestamp < ?
		`, query, yesterday, cutoff).Scan(&count)

		if err == nil {
			yesterdayCounts[query] = count
		}
	}

	// Calculate trends
	var trends []TrendItem
	for _, query := range queries {
		recentCount := recentCounts[query]
		yesterdayCount := yesterdayCounts[query]

		growth := 0.0
		if yesterdayCount > 0 {
			growth = float64(recentCount-yesterdayCount) / float64(yesterdayCount) * 100
		}

		trends = append(trends, TrendItem{
			Query:    query,
			Count:     recentCount,
			Yesterday: yesterdayCount,
			Growth:    growth,
		})
	}

	// Sort by growth (descending)
	for i := 0; i < len(trends); i++ {
		for j := i + 1; j < len(trends); j++ {
			if trends[i].Growth < trends[j].Growth {
				trends[i], trends[j] = trends[j], trends[i]
			}
		}
	}

	return trends, nil
}

// GetSuggestions gets search suggestions based on query
func (s *SearchHistoryDB) GetSuggestions(userID int64, query string) ([]string, error) {
	if query == "" {
		// Return recent searches
		entries, err := s.GetHistory(userID, 5)
		if err != nil {
			return nil, err
		}

		suggestions := make([]string, len(entries))
		for i, entry := range entries {
			suggestions[i] = entry.Query
		}
		return suggestions, nil
	}

	// Return matching searches
	rows, err := s.db.Query(`
		SELECT DISTINCT query
		FROM search_history
		WHERE user_id = ? AND query LIKE ?
		ORDER BY timestamp DESC
		LIMIT 5
	`, userID, "%"+query+"%")

	if err != nil {
		return nil, fmt.Errorf("failed to get suggestions: %w", err)
	}
	defer rows.Close()

	var suggestions []string
	for rows.Next() {
		var query string
		if err := rows.Scan(&query); err != nil {
			return nil, fmt.Errorf("failed to scan suggestion: %w", err)
		}
		suggestions = append(suggestions, query)
	}

	return suggestions, nil
}

// UpdateEntryTags updates tags for a search entry
func (s *SearchHistoryDB) UpdateEntryTags(userID int64, index int, tags []string) error {
	entry, err := s.GetEntry(userID, index)
	if err != nil {
		return err
	}

	// Convert tags to string (comma-separated)
	tagsStr := ""
	for i, tag := range tags {
		if i > 0 {
			tagsStr += ","
		}
		tagsStr += tag
	}

	_, err = s.db.Exec(
		"UPDATE search_history SET tags = ? WHERE user_id = ? AND query = ? AND timestamp = ?",
		tagsStr, userID, entry.Query, entry.Timestamp,
	)

	return err
}

// UpdateEntryMedia updates media association for a search entry
func (s *SearchHistoryDB) UpdateEntryMedia(userID int64, index int, mediaID int, mediaType string) error {
	entry, err := s.GetEntry(userID, index)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(
		"UPDATE search_history SET media_id = ?, media_type = ? WHERE user_id = ? AND query = ? AND timestamp = ?",
		mediaID, mediaType, userID, entry.Query, entry.Timestamp,
	)

	return err
}

// Close closes the database connection
func (s *SearchHistoryDB) Close() error {
	return s.db.Close()
}
