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

	// Save to file
	savePreferencesToFile()
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
func savePreferencesToFile() {
	if prefManager == nil {
		return
	}

	prefManager.mutex.Lock()
	defer prefManager.mutex.Unlock()

	data, err := json.MarshalIndent(prefManager.preferences, "", "  ")
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
func loadPreferencesFromFile() {
	data, err := os.ReadFile("/root/emby-telegram-bot/preferences.json")
	if err != nil {
		log.Println("No existing preferences found, starting fresh")
		return
	}

	err = json.Unmarshal(data, &prefManager.preferences)
	if err != nil {
		log.Printf("Error loading preferences: %v", err)
		return
	}

	log.Printf("Loaded preferences for %d users", len(prefManager.preferences))
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

// GetActiveUserCount returns the number of users with preferences
func GetActiveUserCount() int {
	if prefManager == nil {
		return 0
	}
	prefManager.mutex.RLock()
	defer prefManager.mutex.RUnlock()
	return len(prefManager.preferences)
}
