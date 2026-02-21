package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// UserPreferences stores user notification preferences
type UserPreferences struct {
	UserID             string        `json:"userId"`
	Username           string        `json:"username"`
	NotifyMovies       bool          `json:"notifyMovies"`
	NotifySeries       bool          `json:"notifySeries"`
	NotifyIssues       bool          `json:"notifyIssues"`
	NotifyApproved     bool          `json:"notifyApproved"`
	NotifyAvailable    bool          `json:"notifyAvailable"`
	MinVoteAverage     float64       `json:"minVoteAverage"`
	QuietHoursEnabled  bool          `json:"quietHoursEnabled"`
	QuietHoursStart    string        `json:"quietHoursStart"` // HH:MM format
	QuietHoursEnd      string        `json:"quietHoursEnd"`   // HH:MM format
	KeywordsWhitelist  []string      `json:"keywordsWhitelist"`
	KeywordsBlacklist  []string      `json:"keywordsBlacklist"`
}

// PreferenceManager manages user preferences
type PreferenceManager struct {
	preferences map[string]*UserPreferences
	mutex       sync.RWMutex
}

var prefManager *PreferenceManager

// InitPreferenceManager initializes the preference manager
func InitPreferenceManager() {
	prefManager = &PreferenceManager{
		preferences: make(map[string]*UserPreferences),
	}

	// Load from file
	loadPreferencesFromFile()

	log.Println("Preference manager initialized")
}

// GetPreferences returns user preferences, creating default if needed
func (pm *PreferenceManager) GetPreferences(userID, username string) *UserPreferences {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if prefs, exists := pm.preferences[userID]; exists {
		return prefs
	}

	// Create default preferences
	prefs := &UserPreferences{
		UserID:            userID,
		Username:          username,
		NotifyMovies:      true,
		NotifySeries:      true,
		NotifyIssues:      true,
		NotifyApproved:    true,
		NotifyAvailable:   true,
		MinVoteAverage:    0,
		QuietHoursEnabled: false,
		QuietHoursStart:   "22:00",
		QuietHoursEnd:     "08:00",
		KeywordsWhitelist: []string{},
		KeywordsBlacklist: []string{},
	}

	pm.preferences[userID] = prefs
	return prefs
}

// SetPreferences updates user preferences
func (pm *PreferenceManager) SetPreferences(userID string, prefs *UserPreferences) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	prefs.UserID = userID
	pm.preferences[userID] = prefs

	// Save to file - use internal method that assumes lock is held
	pm.savePreferencesToFileLocked()
}

// ShouldNotify determines if a user should receive a notification
func (pm *PreferenceManager) ShouldNotify(userID string, eventType, mediaType string, title string) bool {
	pm.mutex.RLock()
	prefs, exists := pm.preferences[userID]
	pm.mutex.RUnlock()

	if !exists {
		return true // No preferences set, notify by default
	}

	// Check quiet hours
	if prefs.QuietHoursEnabled && isInQuietHours(prefs.QuietHoursStart, prefs.QuietHoursEnd) {
		// Only urgent issues bypass quiet hours
		if eventType != "ISSUE_CREATED" || !isUrgentIssue(title) {
			return false
		}
	}

	// Check media type preferences
	switch eventType {
	case "REQUEST_CREATED":
		if mediaType == "movie" && !prefs.NotifyMovies {
			return false
		}
		if mediaType == "tv" && !prefs.NotifySeries {
			return false
		}
	case "REQUEST_APPROVED":
		if !prefs.NotifyApproved {
			return false
		}
		if mediaType == "movie" && !prefs.NotifyMovies {
			return false
		}
		if mediaType == "tv" && !prefs.NotifySeries {
			return false
		}
	case "REQUEST_AVAILABLE":
		if !prefs.NotifyAvailable {
			return false
		}
		if mediaType == "movie" && !prefs.NotifyMovies {
			return false
		}
		if mediaType == "tv" && !prefs.NotifySeries {
			return false
		}
	case "ISSUE_CREATED", "ISSUE_COMMENT", "ISSUE_RESOLVED":
		if !prefs.NotifyIssues {
			return false
		}
	}

	// Check keywords
	if len(prefs.KeywordsWhitelist) > 0 {
		found := false
		for _, keyword := range prefs.KeywordsWhitelist {
			if contains(title, keyword) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(prefs.KeywordsBlacklist) > 0 {
		for _, keyword := range prefs.KeywordsBlacklist {
			if contains(title, keyword) {
				return false
			}
		}
	}

	return true
}

// isInQuietHours checks if current time is in quiet hours
func isInQuietHours(start, end string) bool {
	now := time.Now()
	currentTime := now.Hour()*60 + now.Minute()

	// Parse start time (HH:MM)
	startHour, startMin := parseTime(start)
	startMinutes := startHour*60 + startMin

	// Parse end time (HH:MM)
	endHour, endMin := parseTime(end)
	endMinutes := endHour*60 + endMin

	// Handle case where quiet hours span midnight (e.g., 22:00 - 08:00)
	if startMinutes > endMinutes {
		// Current time is after start OR before end
		return currentTime >= startMinutes || currentTime < endMinutes
	}

	// Normal case (e.g., 01:00 - 06:00)
	return currentTime >= startMinutes && currentTime < endMinutes
}

// parseTime parses HH:MM format
func parseTime(timeStr string) (hour, min int) {
	fmt.Sscanf(timeStr, "%d:%d", &hour, &min)
	return
}

// isUrgentIssue checks if an issue is urgent
func isUrgentIssue(title string) bool {
	return contains(title, "Video") || contains(title, "Audio") || contains(title, "视频") || contains(title, "音频")
}

// contains checks if a string contains a substring (case insensitive for ASCII)
func contains(s, substr string) bool {
	// Use strings.Contains which properly handles UTF-8
	return strings.Contains(s, substr)
}

// savePreferencesToFile saves preferences to disk
// Note: This function is designed to be called when the caller does NOT hold the mutex
func savePreferencesToFile() {
	if prefManager == nil {
		return
	}

	prefManager.mutex.Lock()
	defer prefManager.mutex.Unlock()

	prefManager.savePreferencesToFileLocked()
}

// savePreferencesToFileLocked saves preferences to disk
// Note: This function assumes the caller already holds the mutex
func (pm *PreferenceManager) savePreferencesToFileLocked() {
	data, err := json.MarshalIndent(pm.preferences, "", "  ")
	if err != nil {
		log.Printf("Error marshaling preferences: %v", err)
		return
	}

	err = os.WriteFile("/root/emby-telegram-bot/preferences.json", data, 0644)
	if err != nil {
		log.Printf("Error saving preferences: %v", err)
	}
}

// loadPreferencesFromFile loads preferences from disk
// Note: This function should only be called during initialization when no other goroutines are running
func loadPreferencesFromFile() {
	data, err := os.ReadFile("/root/emby-telegram-bot/preferences.json")
	if err != nil {
		log.Println("No existing preferences found, starting fresh")
		return
	}

	// Parse into a temporary map first to avoid concurrent access issues
	var tempPrefs map[string]*UserPreferences
	if err := json.Unmarshal(data, &tempPrefs); err != nil {
		log.Printf("Error loading preferences: %v", err)
		return
	}

	// Now assign under lock if prefManager exists
	if prefManager != nil {
		prefManager.mutex.Lock()
		prefManager.preferences = tempPrefs
		prefManager.mutex.Unlock()
		log.Printf("Loaded preferences for %d users", len(tempPrefs))
	}
}

// FormatPreferences formats user preferences for display
func FormatPreferences(prefs *UserPreferences) string {
	msg := "⚙️ *我的通知设置*\n\n"

	msg += "*通知类型:*\n"
	if prefs.NotifyMovies {
		msg += "✅ 电影通知\n"
	} else {
		msg += "❌ 电影通知\n"
	}
	if prefs.NotifySeries {
		msg += "✅ 剧集通知\n"
	} else {
		msg += "❌ 剧集通知\n"
	}
	if prefs.NotifyIssues {
		msg += "✅ 问题报告\n"
	} else {
		msg += "❌ 问题报告\n"
	}
	if prefs.NotifyApproved {
		msg += "✅ 批准通知\n"
	} else {
		msg += "❌ 批准通知\n"
	}
	if prefs.NotifyAvailable {
		msg += "✅ 可用通知\n"
	} else {
		msg += "❌ 可用通知\n"
	}

	msg += "\n*勿扰模式:*\n"
	if prefs.QuietHoursEnabled {
		msg += fmt.Sprintf("✅ 已启用 (%s - %s)\n", prefs.QuietHoursStart, prefs.QuietHoursEnd)
	} else {
		msg += "❌ 已禁用\n"
	}

	if len(prefs.KeywordsWhitelist) > 0 {
		msg += fmt.Sprintf("\n*白名单关键词:*\n%s\n", formatList(prefs.KeywordsWhitelist))
	}

	if len(prefs.KeywordsBlacklist) > 0 {
		msg += fmt.Sprintf("\n*黑名单关键词:*\n%s\n", formatList(prefs.KeywordsBlacklist))
	}

	return msg
}

func formatList(items []string) string {
	result := ""
	for i, item := range items {
		result += fmt.Sprintf("• %s", item)
		if i < len(items)-1 {
			result += "\n"
		}
	}
	return result
}

// FormatPreferencesWithKeyboard formats preferences with interactive buttons
func FormatPreferencesWithKeyboard(prefs *UserPreferences) (string, *TelegramInlineKeyboard) {
	msg := "⚙️ *我的通知设置*\n\n"

	// Status indicators
	onIndicator := "✅"
	offIndicator := "❌"

	msg += "*📢 通知类型*\n"
	if prefs.NotifyMovies {
		msg += fmt.Sprintf("%s 电影通知\n", onIndicator)
	} else {
		msg += fmt.Sprintf("%s 电影通知\n", offIndicator)
	}
	if prefs.NotifySeries {
		msg += fmt.Sprintf("%s 剧集通知\n", onIndicator)
	} else {
		msg += fmt.Sprintf("%s 剧集通知\n", offIndicator)
	}
	if prefs.NotifyIssues {
		msg += fmt.Sprintf("%s 问题报告\n", onIndicator)
	} else {
		msg += fmt.Sprintf("%s 问题报告\n", offIndicator)
	}
	if prefs.NotifyApproved {
		msg += fmt.Sprintf("%s 批准通知\n", onIndicator)
	} else {
		msg += fmt.Sprintf("%s 批准通知\n", offIndicator)
	}
	if prefs.NotifyAvailable {
		msg += fmt.Sprintf("%s 可用通知\n", onIndicator)
	} else {
		msg += fmt.Sprintf("%s 可用通知\n", offIndicator)
	}

	msg += "\n*🌙 勿扰模式*\n"
	if prefs.QuietHoursEnabled {
		msg += fmt.Sprintf("%s 已启用 (%s - %s)\n", onIndicator, prefs.QuietHoursStart, prefs.QuietHoursEnd)
	} else {
		msg += fmt.Sprintf("%s 已禁用\n", offIndicator)
	}

	// Create interactive keyboard
	keyboard := &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{
			{
				{"text": "🎬 电影", "callback_data": "prefs_toggle_movies"},
				{"text": "📺 剧集", "callback_data": "prefs_toggle_series"},
			},
			{
				{"text": "🐛 问题", "callback_data": "prefs_toggle_issues"},
				{"text": "✅ 批准", "callback_data": "prefs_toggle_approved"},
			},
			{
				{"text": "🎉 可用", "callback_data": "prefs_toggle_available"},
				{"text": "🌙 勿扰", "callback_data": "prefs_toggle_quiet"},
			},
			{
				{"text": "🔕 白名单", "callback_data": "prefs_whitelist"},
				{"text": "🚫 黑名单", "callback_data": "prefs_blacklist"},
			},
			{
				{"text": "🔄 重置所有", "callback_data": "prefs_reset"},
				{"text": "❓ 帮助", "callback_data": "prefs_help"},
			},
		},
	}

	return msg, keyboard
}

// GetActiveUserCount returns the number of users with preferences
func GetActiveUserCount() int {
	if prefManager == nil {
		return 0
	}
	prefManager.mutex.RLock()
	defer prefManager.mutex.RUnlock()
	return len(prefManager.preferences)
}
