package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// QuotaReminderManager manages user quota reminders
type QuotaReminderManager struct {
	// Reminders sent (telegramID -> reminder info)
	reminders     map[int64]*QuotaReminder
	reminderMutex sync.RWMutex

	// Storage file
	storageFile string

	// Reminder thresholds (percentage)
	thresholds []int

	// Check interval
	checkInterval time.Duration
}

// QuotaReminder represents a reminder sent to a user
type QuotaReminder struct {
	TelegramID    int64     `json:"telegramId"`
	QuotaType     string    `json:"quotaType"`     // "movie" or "tv"
	Threshold     int       `json:"threshold"`     // Percentage that triggered reminder
	SentAt        time.Time `json:"sentAt"`
	Acknowledged  bool      `json:"acknowledged"`
	OriginalQuota int       `json:"originalQuota"` // For reference
	Remaining     int       `json:"remaining"`
}

// QuotaReminderData stores all quota reminders
type QuotaReminderData struct {
	Reminders map[int64]*QuotaReminder `json:"reminders"`
	LastSync  string                   `json:"lastSync"`
}

var quotaReminderMgr *QuotaReminderManager

// InitQuotaReminderManager initializes the quota reminder manager
func InitQuotaReminderManager() {
	quotaReminderMgr = &QuotaReminderManager{
		reminders:     make(map[int64]*QuotaReminder),
		storageFile:   "quota_reminders.json",
		thresholds:    []int{75, 50, 25}, // Remind at 75%, 50%, 25% remaining
		checkInterval: 6 * time.Hour,      // Check every 6 hours
	}

	// Load existing data
	quotaReminderMgr.load()

	// Start periodic check
	go quotaReminderMgr.periodicCheck()

	log.Println("QuotaReminder manager initialized")
}

// CheckAndRemind checks user quota and sends reminder if needed
// Returns true if a reminder was sent
func (m *QuotaReminderManager) CheckAndRemind(telegramID int64) bool {
	if smartSearchMgr == nil {
		return false
	}

	// Get user quota info using internal method
	quota := smartSearchMgr.getUserQuota(telegramID)
	if quota == nil {
		return false
	}

	reminderSent := false

	// Check movie quota
	if quota.MovieLimit > 0 {
		movieRemaining := quota.MovieLimit - quota.MovieUsed
		moviePercent := (movieRemaining * 100) / quota.MovieLimit

		for _, threshold := range m.thresholds {
			if moviePercent <= threshold && moviePercent > (threshold-10) {
				if m.shouldSendReminder(telegramID, "movie", threshold) {
					m.sendReminder(telegramID, "movie", movieRemaining, quota.MovieLimit, threshold)
					reminderSent = true
				}
			}
		}

		// Emergency: only 1 left
		if movieRemaining == 1 && m.shouldSendReminder(telegramID, "movie", 5) {
			m.sendReminder(telegramID, "movie", movieRemaining, quota.MovieLimit, 5)
			reminderSent = true
		}
	}

	// Check TV quota
	if quota.TVLimit > 0 {
		tvRemaining := quota.TVLimit - quota.TVUsed
		tvPercent := (tvRemaining * 100) / quota.TVLimit

		for _, threshold := range m.thresholds {
			if tvPercent <= threshold && tvPercent > (threshold-10) {
				if m.shouldSendReminder(telegramID, "tv", threshold) {
					m.sendReminder(telegramID, "tv", tvRemaining, quota.TVLimit, threshold)
					reminderSent = true
				}
			}
		}

		// Emergency: only 1 left
		if tvRemaining == 1 && m.shouldSendReminder(telegramID, "tv", 5) {
			m.sendReminder(telegramID, "tv", tvRemaining, quota.TVLimit, 5)
			reminderSent = true
		}
	}

	return reminderSent
}

// shouldSendReminder checks if a reminder should be sent
func (m *QuotaReminderManager) shouldSendReminder(telegramID int64, quotaType string, threshold int) bool {
	m.reminderMutex.RLock()
	defer m.reminderMutex.RUnlock()

	// Check if similar reminder was sent recently
	if reminder, exists := m.reminders[telegramID]; exists {
		if reminder.QuotaType == quotaType && reminder.Threshold == threshold {
			// Don't send same reminder again within 24 hours
			if time.Since(reminder.SentAt) < 24*time.Hour {
				return false
			}
		}
	}

	return true
}

// sendReminder sends a quota reminder to user
func (m *QuotaReminderManager) sendReminder(telegramID int64, quotaType string, remaining, limit, threshold int) {
	typeName := "电影"
	emoji := "🎬"
	if quotaType == "tv" {
		typeName = "剧集"
		emoji = "📺"
	}

	msg := fmt.Sprintf("⚠️ *配额提醒*\n\n")
	msg += fmt.Sprintf("%s 你的%s配额即将用完！\n\n", emoji, typeName)
	msg += fmt.Sprintf("📊 剩余: %d / %d\n", remaining, limit)
	msg += fmt.Sprintf("💡 使用率: %d%%\n\n", 100-(remaining*100/limit))

	if remaining <= 1 {
		msg += "⚠️ 这是今天的最后一个配额了！\n\n"
	} else if remaining <= 3 {
		msg += "⚠️ 配额不多了，请谨慎使用~\n\n"
	}

	msg += "💡 提示:\n"
	msg += "• 配额每天 00:00 重置\n"
	msg += "• 发送 /quota 查看详细配额"

	// Send message
	sendPrivateMessage(telegramID, msg, nil)

	// Record reminder
	m.reminderMutex.Lock()
	m.reminders[telegramID] = &QuotaReminder{
		TelegramID:    telegramID,
		QuotaType:     quotaType,
		Threshold:     threshold,
		SentAt:        time.Now(),
		Acknowledged:  false,
		OriginalQuota: limit,
		Remaining:     remaining,
	}
	m.save()
	m.reminderMutex.Unlock()

	log.Printf("QuotaReminder: Sent %s reminder to user %d (remaining: %d/%d)", quotaType, telegramID, remaining, limit)
}

// periodicCheck checks all active users for quota reminders
func (m *QuotaReminderManager) periodicCheck() {
	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()

	// Initial check after startup
	time.Sleep(5 * time.Minute)
	m.checkAllUsers()

	for range ticker.C {
		m.checkAllUsers()
	}
}

// checkAllUsers checks quota for all active users
func (m *QuotaReminderManager) checkAllUsers() {
	if userSyncMgr == nil || smartSearchMgr == nil {
		return
	}

	// Get all mapped users
	mappings := userSyncMgr.GetAllMappings()
	count := 0

	for telegramID := range mappings {
		if m.CheckAndRemind(telegramID) {
			count++
		}
	}

	if count > 0 {
		log.Printf("QuotaReminder: Checked %d users, sent %d reminders", len(mappings), count)
	}
}

// ClearReminder clears a reminder for a user
func (m *QuotaReminderManager) ClearReminder(telegramID int64) {
	m.reminderMutex.Lock()
	defer m.reminderMutex.Unlock()

	if _, exists := m.reminders[telegramID]; exists {
		delete(m.reminders, telegramID)
		m.save()
	}
}

// save saves quota reminder data
func (m *QuotaReminderManager) save() {
	data := QuotaReminderData{
		Reminders: m.reminders,
		LastSync:  time.Now().Format(time.RFC3339),
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Printf("QuotaReminder: Failed to marshal data: %v", err)
		return
	}

	if err := os.WriteFile(m.storageFile, jsonData, 0644); err != nil {
		log.Printf("QuotaReminder: Failed to save data: %v", err)
	}
}

// load loads quota reminder data
func (m *QuotaReminderManager) load() {
	data, err := os.ReadFile(m.storageFile)
	if err != nil {
		log.Printf("QuotaReminder: Data file not found, starting fresh: %v", err)
		return
	}

	var loaded QuotaReminderData
	if err := json.Unmarshal(data, &loaded); err != nil {
		log.Printf("QuotaReminder: Failed to load data: %v", err)
		return
	}

	m.reminders = loaded.Reminders
	log.Printf("QuotaReminder: Loaded %d reminders", len(m.reminders))
}

// GetUserReminderStatus gets a user's reminder status
func (m *QuotaReminderManager) GetUserReminderStatus(telegramID int64) *QuotaReminder {
	m.reminderMutex.RLock()
	defer m.reminderMutex.RUnlock()

	return m.reminders[telegramID]
}

// FormatQuotaReminderStatus formats the reminder status for display
func FormatQuotaReminderStatus(telegramID int64) string {
	if quotaReminderMgr == nil {
		return ""
	}

	reminder := quotaReminderMgr.GetUserReminderStatus(telegramID)
	if reminder == nil {
		return ""
	}

	typeName := "电影"
	if reminder.QuotaType == "tv" {
		typeName = "剧集"
	}

	msg := fmt.Sprintf("📋 *最近提醒*\n\n")
	msg += fmt.Sprintf("类型: %s\n", typeName)
	msg += fmt.Sprintf("剩余: %d / %d\n", reminder.Remaining, reminder.OriginalQuota)
	msg += fmt.Sprintf("时间: %s", reminder.SentAt.Format("15:04"))

	return msg
}

// DisableRemindersForUser disables reminders for a user (opt-out)
func (m *QuotaReminderManager) DisableRemindersForUser(telegramID int64) {
	// This could be extended to support per-user opt-out
	// For now, just mark reminder as acknowledged
	m.reminderMutex.Lock()
	defer m.reminderMutex.Unlock()

	if reminder, exists := m.reminders[telegramID]; exists {
		reminder.Acknowledged = true
		m.save()
	}
}
