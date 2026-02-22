package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// AnalyticsData stores detailed analytics
type AnalyticsData struct {
	Requests   []RequestRecord        `json:"requests"`
	MediaStats map[string]*MediaStats `json:"mediaStats"`
	UserStats  map[string]*UserStats  `json:"userStats"`
	DailyStats map[string]*DailyCount `json:"dailyStats"`
	mutex      sync.RWMutex
}

// RequestRecord records each request
type RequestRecord struct {
	RequestID   string     `json:"requestId"`
	MediaTitle  string     `json:"mediaTitle"`
	MediaType   string     `json:"mediaType"`
	TmdbID      int        `json:"tmdbId"`
	UserID      string     `json:"userId"`
	Username    string     `json:"username"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"createdAt"`
	ApprovedAt  *time.Time `json:"approvedAt,omitempty"`
	AvailableAt *time.Time `json:"availableAt,omitempty"`
}

// MediaStats tracks per-media statistics
type MediaStats struct {
	MediaID      string    `json:"mediaId"`
	MediaTitle   string    `json:"mediaTitle"`
	MediaType    string    `json:"mediaType"`
	RequestCount int       `json:"requestCount"`
	FirstRequest time.Time `json:"firstRequest"`
	LastRequest  time.Time `json:"lastRequest"`
	TmdbID       int       `json:"tmdbId"`
}

// UserStats tracks per-user statistics
type UserStats struct {
	UserID        string    `json:"userId"`
	Username      string    `json:"username"`
	RequestCount  int       `json:"requestCount"`
	ApprovedCount int       `json:"approvedCount"`
	DeclinedCount int       `json:"declinedCount"`
	FirstRequest  time.Time `json:"firstRequest"`
	LastRequest   time.Time `json:"lastRequest"`
	TopGenres     []string  `json:"topGenres"`
}

// DailyCount tracks daily statistics
type DailyCount struct {
	Date           string `json:"date"`
	RequestCount   int    `json:"requestCount"`
	ApprovedCount  int    `json:"approvedCount"`
	DeclinedCount  int    `json:"declinedCount"`
	AvailableCount int    `json:"availableCount"`
}

var analytics *AnalyticsData

// InitAnalytics initializes the analytics system
func InitAnalytics() {
	analytics = &AnalyticsData{
		Requests:   make([]RequestRecord, 0),
		MediaStats: make(map[string]*MediaStats),
		UserStats:  make(map[string]*UserStats),
		DailyStats: make(map[string]*DailyCount),
	}

	// Load existing data from file
	loadAnalyticsFromFile()

	// Start periodic save
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			saveAnalyticsToFile()
		}
	}()

	log.Println("Analytics system initialized")
}

// RecordRequest records a new request
func RecordRequest(requestID, mediaTitle, mediaType string, tmdbID int, userID, username string) {
	analytics.mutex.Lock()
	defer analytics.mutex.Unlock()

	now := time.Now()
	today := now.Format("2006-01-02")

	// Add to request records
	record := RequestRecord{
		RequestID:  requestID,
		MediaTitle: mediaTitle,
		MediaType:  mediaType,
		TmdbID:     tmdbID,
		UserID:     userID,
		Username:   username,
		Status:     "pending",
		CreatedAt:  now,
	}
	analytics.Requests = append(analytics.Requests, record)

	// Update media stats
	mediaKey := fmt.Sprintf("%s_%d", mediaType, tmdbID)
	if stats, exists := analytics.MediaStats[mediaKey]; exists {
		stats.RequestCount++
		stats.LastRequest = now
	} else {
		analytics.MediaStats[mediaKey] = &MediaStats{
			MediaID:      mediaKey,
			MediaTitle:   mediaTitle,
			MediaType:    mediaType,
			RequestCount: 1,
			FirstRequest: now,
			LastRequest:  now,
			TmdbID:       tmdbID,
		}
	}

	// Update user stats
	if stats, exists := analytics.UserStats[userID]; exists {
		stats.RequestCount++
		stats.LastRequest = now
	} else {
		analytics.UserStats[userID] = &UserStats{
			UserID:       userID,
			Username:     username,
			RequestCount: 1,
			FirstRequest: now,
			LastRequest:  now,
		}
	}

	// Update daily stats
	if daily, exists := analytics.DailyStats[today]; exists {
		daily.RequestCount++
	} else {
		analytics.DailyStats[today] = &DailyCount{
			Date:         today,
			RequestCount: 1,
		}
	}

	// Keep only last 1000 requests
	if len(analytics.Requests) > 1000 {
		analytics.Requests = analytics.Requests[len(analytics.Requests)-1000:]
	}
}

// UpdateRequestStatus updates request status
func UpdateRequestStatus(requestID, status string) {
	analytics.mutex.Lock()
	defer analytics.mutex.Unlock()

	now := time.Now()
	today := now.Format("2006-01-02")

	for i, req := range analytics.Requests {
		if req.RequestID == requestID {
			analytics.Requests[i].Status = status

			if status == "approved" {
				analytics.Requests[i].ApprovedAt = &now

				// Update user stats
				if userStats, exists := analytics.UserStats[req.UserID]; exists {
					userStats.ApprovedCount++
				}

				// Update daily stats
				if daily, exists := analytics.DailyStats[today]; exists {
					daily.ApprovedCount++
				} else {
					analytics.DailyStats[today] = &DailyCount{
						Date:          today,
						RequestCount:  0,
						ApprovedCount: 1,
					}
				}

			} else if status == "declined" {
				// Update user stats
				if userStats, exists := analytics.UserStats[req.UserID]; exists {
					userStats.DeclinedCount++
				}

				// Update daily stats
				if daily, exists := analytics.DailyStats[today]; exists {
					daily.DeclinedCount++
				} else {
					analytics.DailyStats[today] = &DailyCount{
						Date:          today,
						RequestCount:  0,
						DeclinedCount: 1,
					}
				}

			} else if status == "available" {
				analytics.Requests[i].AvailableAt = &now

				// Update daily stats
				if daily, exists := analytics.DailyStats[today]; exists {
					daily.AvailableCount++
				} else {
					analytics.DailyStats[today] = &DailyCount{
						Date:           today,
						RequestCount:   0,
						AvailableCount: 1,
					}
				}
			}
			break
		}
	}
}

// GetTopMedia returns top requested media
func GetTopMedia(limit int) []*MediaStats {
	analytics.mutex.RLock()
	defer analytics.mutex.RUnlock()

	var list []*MediaStats
	for _, stats := range analytics.MediaStats {
		list = append(list, stats)
	}

	// Simple sort by request count
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].RequestCount > list[i].RequestCount {
				list[i], list[j] = list[j], list[i]
			}
		}
	}

	if len(list) > limit {
		list = list[:limit]
	}

	return list
}

// GetTopUsers returns top users by request count
func GetTopUsers(limit int) []*UserStats {
	analytics.mutex.RLock()
	defer analytics.mutex.RUnlock()

	var list []*UserStats
	for _, stats := range analytics.UserStats {
		list = append(list, stats)
	}

	// Simple sort by request count
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].RequestCount > list[i].RequestCount {
				list[i], list[j] = list[j], list[i]
			}
		}
	}

	if len(list) > limit {
		list = list[:limit]
	}

	return list
}

// GetTrends returns daily trends for the last N days
func GetTrends(days int) []*DailyCount {
	analytics.mutex.RLock()
	defer analytics.mutex.RUnlock()

	var list []*DailyCount
	cutoff := time.Now().AddDate(0, 0, -days)

	for _, stats := range analytics.DailyStats {
		date, _ := time.Parse("2006-01-02", stats.Date)
		if date.After(cutoff) {
			list = append(list, stats)
		}
	}

	// Sort by date
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].Date > list[i].Date {
				list[i], list[j] = list[j], list[i]
			}
		}
	}

	return list
}

// FormatTopMedia formats top media for display
func FormatTopMedia(mediaList []*MediaStats) string {
	if len(mediaList) == 0 {
		return "┌────────────────────┐\n│  🔥 热门媒体排行  │\n└────────────────────┘\n\n暂无数据"
	}

	msg := "┌────────────────────┐\n"
	msg += "│  🔥 热门媒体排行  │\n"
	msg += "└────────────────────┘\n\n"

	for i, media := range mediaList {
		emoji := "🎬"
		if media.MediaType == "tv" {
			emoji = "📺"
		}

		rank := i + 1
		switch rank {
		case 1:
			emoji = "🥇"
		case 2:
			emoji = "🥈"
		case 3:
			emoji = "🥉"
		}

		mediaType := "电影"
		if media.MediaType == "tv" {
			mediaType = "剧集"
		}

		msg += fmt.Sprintf("%s %s\n", emoji, media.MediaTitle)
		msg += fmt.Sprintf("   👜 请求 %d 次 · %s\n\n", media.RequestCount, mediaType)
	}

	return msg
}

// FormatTopUsers formats top users for display
func FormatTopUsers(userList []*UserStats) string {
	if len(userList) == 0 {
		return "┌────────────────────┐\n│  👥 活跃用户排行  │\n└────────────────────┘\n\n暂无数据"
	}

	msg := "┌────────────────────┐\n"
	msg += "│  👥 活跃用户排行  │\n"
	msg += "└────────────────────┘\n\n"

	for i, user := range userList {
		rank := i + 1
		emoji := "👤"
		switch rank {
		case 1:
			emoji = "🥇"
		case 2:
			emoji = "🥈"
		case 3:
			emoji = "🥉"
		}

		msg += fmt.Sprintf("%s %s\n", emoji, user.Username)
		msg += fmt.Sprintf("   📋 请求 %d 次", user.RequestCount)
		if user.ApprovedCount > 0 {
			msg += fmt.Sprintf(" · ✅ %d", user.ApprovedCount)
		}
		if user.DeclinedCount > 0 {
			msg += fmt.Sprintf(" · ❌ %d", user.DeclinedCount)
		}
		msg += "\n\n"
	}

	return msg
}

// FormatTrends formats trends for display with ASCII chart
func FormatTrends(trends []*DailyCount) string {
	if len(trends) == 0 {
		return "┌────────────────────┐\n│  📈 请求趋势统计  │\n└────────────────────┘\n\n暂无数据"
	}

	msg := "┌────────────────────┐\n"
	msg += "│  📈 请求趋势统计  │\n"
	msg += "└────────────────────┘\n\n"

	// Calculate max for scaling
	maxCount := 1
	for _, t := range trends {
		if t.RequestCount > maxCount {
			maxCount = t.RequestCount
		}
	}

	msg += "┌─ 每日请求 ────────┐\n"
	for _, t := range trends {
		date := t.Date[5:] // Remove year
		bar := ""
		count := t.RequestCount
		if count > 0 {
			barLength := (count * 15) / maxCount
			for i := 0; i < barLength; i++ {
				bar += "▓"
			}
		}
		msg += fmt.Sprintf("│ %s │%s%d │\n", date, bar+repeatSpaces(15-len(bar)-len(fmt.Sprintf("%d", count))), count)
	}
	msg += "└───────────────────┘\n\n"

	// Summary stats
	totalRequests := 0
	totalApproved := 0
	totalDeclined := 0
	for _, t := range trends {
		totalRequests += t.RequestCount
		totalApproved += t.ApprovedCount
		totalDeclined += t.DeclinedCount
	}

	msg += "┌─ 统计汇总 ────────┐\n"
	msg += fmt.Sprintf("│ 📊 总请求 │%10d │\n", totalRequests)
	msg += fmt.Sprintf("│ ✅ 已批准 │%10d │\n", totalApproved)
	msg += fmt.Sprintf("│ ❌ 已拒绝 │%10d │\n", totalDeclined)
	msg += "└───────────────────┘"

	return msg
}

// Helper function to repeat spaces
func repeatSpaces(count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += " "
	}
	return result
}

// saveAnalyticsToFile saves analytics to disk
func saveAnalyticsToFile() {
	analytics.mutex.Lock()
	defer analytics.mutex.Unlock()

	data, err := json.MarshalIndent(analytics, "", "  ")
	if err != nil {
		log.Printf("Error marshaling analytics: %v", err)
		return
	}

	err = os.WriteFile("/root/emby-telegram-bot/analytics.json", data, 0644)
	if err != nil {
		log.Printf("Error saving analytics: %v", err)
	}
}

// loadAnalyticsFromFile loads analytics from disk
func loadAnalyticsFromFile() {
	data, err := os.ReadFile("/root/emby-telegram-bot/analytics.json")
	if err != nil {
		log.Println("No existing analytics data found, starting fresh")
		return
	}

	err = json.Unmarshal(data, &analytics)
	if err != nil {
		log.Printf("Error loading analytics: %v", err)
		return
	}

	log.Printf("Loaded analytics data: %d requests, %d media, %d users",
		len(analytics.Requests), len(analytics.MediaStats), len(analytics.UserStats))
}

// GetAnalyticsSummary returns overall statistics
func GetAnalyticsSummary() map[string]interface{} {
	analytics.mutex.RLock()
	defer analytics.mutex.RUnlock()

	return map[string]interface{}{
		"totalRequests": len(analytics.Requests),
		"totalMedia":    len(analytics.MediaStats),
		"totalUsers":    len(analytics.UserStats),
		"daysTracked":   len(analytics.DailyStats),
	}
}
