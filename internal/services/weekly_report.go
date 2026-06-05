package services

import (
	"encoding/json"
	"fmt"
	"github.com/xzb177/yimao/pkg/logger"
	"os"
	"sync"
	"time"
)

// WeeklyReport 用户每周观影报告
type WeeklyReport struct {
	UserID    int64     `json:"user_id"`
	UserName  string    `json:"user_name"`
	WeekStart time.Time `json:"week_start"`
	WeekEnd   time.Time `json:"week_end"`

	// 统计数据
	SearchCount   int `json:"search_count"`
	RequestCount  int `json:"request_count"`
	ApprovedCount int `json:"approved_count"`

	// 搜索内容分析
	TopSearches []string       `json:"top_searches"`
	GenrePrefs  map[string]int `json:"genre_prefs"` // 类型偏好
	YearPrefs   []int          `json:"year_prefs"`  // 年份偏好

	// 行为标签
	BehaviorTags []string `json:"behavior_tags"` // "追剧达人", "电影爱好者" 等

	// 推荐
	Recommendations []string `json:"recommendations"` // 基于本周行为推荐

	LastSentAt time.Time `json:"last_sent_at"`
	IsSent     bool      `json:"is_sent"`
}

// MediaReminder 媒体提醒（用户搜索的影片上线时通知）
type MediaReminder struct {
	ID         string    `json:"id"`
	UserID     int64     `json:"user_id"`
	UserName   string    `json:"user_name"`
	TmdbID     int       `json:"tmdb_id"`
	MediaTitle string    `json:"media_title"`
	MediaType  string    `json:"media_type"`
	SearchDate time.Time `json:"search_date"`
	IsNotified bool      `json:"is_notified"`
	NotifiedAt time.Time `json:"notified_at,omitempty"`
}

// WeeklyReportService manages weekly reports and media reminders
type WeeklyReportService struct {
	dataFile      string
	reports       map[string]*WeeklyReport  // "userID_weekStart" -> report
	reminders     map[string]*MediaReminder // id -> reminder
	reminderIndex map[int64][]string        // userID -> reminder IDs
	mu            sync.RWMutex
	searchHistory *SearchHistoryDB
	quotaService  *QuotaService
	reviewService *ReviewService
	telegram      *TelegramClient
	tmdbClient    *TMDBClient

	// 定时任务
	ticker   *time.Ticker
	stopChan chan bool

	// NotifyEnabled 检查用户是否开启了某类通知（由 main 注入）。
	NotifyEnabled func(userID int64, notifyKey string) bool
}

// NewWeeklyReportService creates a new weekly report service
func NewWeeklyReportService(dataDir string, searchHistory *SearchHistoryDB, quota *QuotaService, review *ReviewService, telegram *TelegramClient, tmdb *TMDBClient) *WeeklyReportService {
	dataFile := fmt.Sprintf("%s/weekly_reports.json", dataDir)

	service := &WeeklyReportService{
		dataFile:      dataFile,
		reports:       make(map[string]*WeeklyReport),
		reminders:     make(map[string]*MediaReminder),
		reminderIndex: make(map[int64][]string),
		searchHistory: searchHistory,
		quotaService:  quota,
		reviewService: review,
		telegram:      telegram,
		tmdbClient:    tmdb,
		stopChan:      make(chan bool),
	}

	service.load()

	// Start weekly report routine (every Monday 9 AM)
	go service.weeklyRoutine()

	// Start reminder check routine (every hour)
	go service.reminderRoutine()

	return service
}

// load loads data from file
func (s *WeeklyReportService) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.dataFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var fileData struct {
		Reports       map[string]*WeeklyReport  `json:"reports"`
		Reminders     map[string]*MediaReminder `json:"reminders"`
		ReminderIndex map[int64][]string        `json:"reminder_index"`
	}

	if err := json.Unmarshal(data, &fileData); err != nil {
		return err
	}

	s.reports = fileData.Reports
	s.reminders = fileData.Reminders
	s.reminderIndex = fileData.ReminderIndex

	logger.Info("[WeeklyReport] Loaded %d reports, %d reminders", len(s.reports), len(s.reminders))
	return nil
}

// save saves data to file
func (s *WeeklyReportService) save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.MarshalIndent(map[string]interface{}{
		"reports":        s.reports,
		"reminders":      s.reminders,
		"reminder_index": s.reminderIndex,
	}, "", "  ")
	if err != nil {
		return err
	}

	return atomicWriteFile(s.dataFile, data, 0644)
}

// getWeekRange returns the start and end of the current week (Monday-Sunday)
func getWeekRange(t time.Time) (time.Time, time.Time) {
	// Get Monday of the week
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	daysSinceMonday := weekday - 1
	monday := t.AddDate(0, 0, -daysSinceMonday)
	weekStart := time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, time.Local)
	weekEnd := weekStart.AddDate(0, 0, 6).Add(23*time.Hour + 59*time.Minute + 59*time.Second)

	return weekStart, weekEnd
}

// getReportKey generates a unique key for a weekly report
func getReportKey(userID int64, weekStart time.Time) string {
	return fmt.Sprintf("%d_%s", userID, weekStart.Format("2006-01-02"))
}

// GenerateReport generates a weekly report for a user
func (s *WeeklyReportService) GenerateReport(userID int64, userName string) (*WeeklyReport, error) {
	weekStart, weekEnd := getWeekRange(time.Now())
	reportKey := getReportKey(userID, weekStart)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if report already exists
	if report, exists := s.reports[reportKey]; exists {
		return report, nil
	}

	report := &WeeklyReport{
		UserID:     userID,
		UserName:   userName,
		WeekStart:  weekStart,
		WeekEnd:    weekEnd,
		GenrePrefs: make(map[string]int),
	}

	// Get search history from database
	if s.searchHistory != nil {
		history, err := s.searchHistory.GetHistory(userID, 100) // Get recent history
		if err == nil {
			// Filter by week range and count
			searchFreq := make(map[string]int)
			for _, entry := range history {
				// Filter entries within the week range
				if entry.Timestamp.Before(weekStart) || entry.Timestamp.After(weekEnd) {
					continue
				}
				report.SearchCount += entry.Count
				searchFreq[entry.Query] += entry.Count
			}

			// Get top searches
			for query, count := range searchFreq {
				if count > 1 {
					report.TopSearches = append(report.TopSearches, fmt.Sprintf("%s(%d次)", query, count))
				}
			}
		}
	}

	// Get request count from review service
	if s.reviewService != nil {
		allRequests := s.reviewService.GetUserRequests(userID)
		for _, req := range allRequests {
			// Filter by week range
			if req.CreatedAt.Before(weekStart) || req.CreatedAt.After(weekEnd) {
				continue
			}
			report.RequestCount++
			if req.Status == "approved" {
				report.ApprovedCount++
			}
		}
	}

	// Generate behavior tags
	report.BehaviorTags = s.generateBehaviorTags(report)

	// Generate recommendations
	report.Recommendations = s.generateRecommendations(report)

	s.reports[reportKey] = report
	go s.save()

	logger.Info("[WeeklyReport] Generated report for user %d: %d searches, %d requests",
		userID, report.SearchCount, report.RequestCount)

	return report, nil
}

// generateBehaviorTags generates behavior tags based on user activity
func (s *WeeklyReportService) generateBehaviorTags(report *WeeklyReport) []string {
	var tags []string

	if report.SearchCount >= 20 {
		tags = append(tags, "🔍 搜索达人")
	}
	if report.RequestCount >= 5 {
		tags = append(tags, "📋 求片狂魔")
	}
	if report.ApprovedCount >= 3 {
		tags = append(tags, "✅ 成功率高")
	}

	// Genre preference
	if len(report.GenrePrefs) > 0 {
		maxGenre := ""
		maxCount := 0
		for genre, count := range report.GenrePrefs {
			if count > maxCount {
				maxCount = count
				maxGenre = genre
			}
		}
		if maxGenre != "" {
			tags = append(tags, fmt.Sprintf("🎭 %s迷", maxGenre))
		}
	}

	if len(tags) == 0 {
		tags = append(tags, "👶 新手入门")
	}

	return tags
}

// generateRecommendations generates recommendations based on user activity
func (s *WeeklyReportService) generateRecommendations(report *WeeklyReport) []string {
	var recs []string

	if report.SearchCount == 0 {
		recs = append(recs, "💡 试试搜索你喜欢的电影或剧集吧！")
	}

	if report.RequestCount == 0 && report.SearchCount > 3 {
		recs = append(recs, "💡 看到喜欢的就点点「求片」，管理员会帮你找资源！")
	}

	if report.SearchCount > 10 && report.RequestCount == 0 {
		recs = append(recs, "💡 只看不求？勇敢点，把想要的都求一遍！")
	}

	// Genre-based recommendations
	for genre := range report.GenrePrefs {
		switch genre {
		case "动作":
			recs = append(recs, "🎬 动作片爱好者，推荐试试今年热门动作片！")
		case "喜剧":
			recs = append(recs, "😄 喜剧片时间到了，来部轻松的吧！")
		case "悬疑":
			recs = append(recs, "🧠 悬疑迷？试试高分悬疑剧！")
		case "爱情":
			recs = append(recs, "💕 爱情片来一部，温暖一下！")
		case "科幻":
			recs = append(recs, "🚀 科幻迷，推荐试试最新硬科幻！")
		}
		break // Only recommend for top genre
	}

	if len(recs) == 0 {
		recs = append(recs, "🌟 继续探索更多精彩内容吧！")
	}

	return recs
}

// FormatReport formats a weekly report as message text
func (s *WeeklyReportService) FormatReport(report *WeeklyReport) string {
	weekStr := report.WeekStart.Format("01月02日")
	toStr := report.WeekEnd.Format("01月02日")

	var msg string
	msg += "━━━━━━━━━━━━━━━━━━━━━━━━\n"
	msg += fmt.Sprintf("📊 %s 的观影周报\n", report.UserName)
	msg += fmt.Sprintf("📅 %s - %s\n\n", weekStr, toStr)

	// Stats
	msg += "📈 本周数据\n"
	msg += fmt.Sprintf("   🔍 搜索 %d 次\n", report.SearchCount)
	msg += fmt.Sprintf("   📋 求片 %d 次\n", report.RequestCount)
	if report.ApprovedCount > 0 {
		msg += fmt.Sprintf("   ✅ 通过 %d 个\n", report.ApprovedCount)
	}
	msg += "\n"

	// Behavior tags
	if len(report.BehaviorTags) > 0 {
		msg += "🏷️ 本周标签\n"
		for _, tag := range report.BehaviorTags {
			msg += fmt.Sprintf("   %s\n", tag)
		}
		msg += "\n"
	}

	// Top searches
	if len(report.TopSearches) > 0 {
		msg += "🔥 热搜关键词\n"
		for _, search := range report.TopSearches {
			msg += fmt.Sprintf("   • %s\n", search)
		}
		msg += "\n"
	}

	// Genre preferences
	if len(report.GenrePrefs) > 0 {
		msg += "🎭 类型偏好\n"
		for genre, count := range report.GenrePrefs {
			msg += fmt.Sprintf("   %s: %d次\n", genre, count)
		}
		msg += "\n"
	}

	// Recommendations
	if len(report.Recommendations) > 0 {
		msg += "💡 专属建议\n"
		for _, rec := range report.Recommendations {
			msg += fmt.Sprintf("   %s\n", rec)
		}
		msg += "\n"
	}

	msg += "━━━━━━━━━━━━━━━━━━━━━━━━\n"
	msg += "👇 下周继续探索精彩内容！"

	return msg
}

// SendReport sends a weekly report to a user
func (s *WeeklyReportService) SendReport(userID int64, userName string) error {
	report, err := s.GenerateReport(userID, userName)
	if err != nil {
		return err
	}

	if s.telegram == nil {
		return fmt.Errorf("telegram client not configured")
	}

	// 检查用户是否开启了周报通知
	if s.NotifyEnabled != nil && !s.NotifyEnabled(userID, NotifyWeekly) {
		return nil
	}

	msg := s.FormatReport(report)
	_, err = s.telegram.SendMessage(userID, msg, "", nil)
	if err != nil {
		return err
	}

	// Mark as sent
	reportKey := getReportKey(userID, report.WeekStart)
	s.mu.Lock()
	if r, exists := s.reports[reportKey]; exists {
		r.IsSent = true
		r.LastSentAt = time.Now()
	}
	s.mu.Unlock()

	go s.save()

	logger.Info("[WeeklyReport] Sent report to user %d", userID)
	return nil
}

// weeklyRoutine runs weekly to send reports (every Monday 9 AM)
func (s *WeeklyReportService) weeklyRoutine() {
	// Calculate next Monday 9 AM
	now := time.Now()
	nextMonday := nextWeekday(now, time.Monday).Add(9 * time.Hour)

	if now.After(nextMonday) {
		nextMonday = nextMonday.Add(7 * 24 * time.Hour)
	}

	firstDelay := time.Until(nextMonday)
	if firstDelay < 0 {
		firstDelay = 24 * time.Hour
	}

	logger.Info("[WeeklyReport] First report scheduled in %v", firstDelay)

	time.Sleep(firstDelay)

	ticker := time.NewTicker(7 * 24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.sendWeeklyReports()
		case <-s.stopChan:
			return
		}
	}
}

// sendWeeklyReports sends reports to all active users
func (s *WeeklyReportService) sendWeeklyReports() {
	logger.Info("[WeeklyReport] Starting weekly report sending - using existing report data")

	// Send reports for users who already have generated reports
	s.mu.RLock()
	var userIDs []int64
	for _, report := range s.reports {
		if !report.IsSent {
			userIDs = append(userIDs, report.UserID)
		}
	}
	s.mu.RUnlock()

	for _, userID := range userIDs {
		userName := "用户" // Can be fetched from session
		if err := s.SendReport(userID, userName); err != nil {
			logger.Info("[WeeklyReport] Failed to send report to %d: %v", userID, err)
		}
		// Add delay to avoid rate limiting
		time.Sleep(500 * time.Millisecond)
	}

	logger.Info("[WeeklyReport] Sent %d weekly reports", len(userIDs))
}

// reminderRoutine checks for media availability reminders hourly
func (s *WeeklyReportService) reminderRoutine() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.checkReminders()
		case <-s.stopChan:
			return
		}
	}
}

// checkReminders checks and sends media availability reminders
func (s *WeeklyReportService) checkReminders() {
	if s.tmdbClient == nil || s.telegram == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for id, reminder := range s.reminders {
		if reminder.IsNotified {
			continue
		}

		// Check if media is available (simplified check)
		// In real implementation, check Emby/MoviePilot availability
		// For now, just check if enough time has passed (3 days)
		if time.Since(reminder.SearchDate) > 3*24*time.Hour {
			// 检查用户是否开启了推荐通知
			if s.NotifyEnabled != nil && !s.NotifyEnabled(reminder.UserID, NotifyRecommend) {
				reminder.IsNotified = true
				continue
			}

			// Send reminder
			msg := fmt.Sprintf("🔔 你之前搜索的「%s」可能已经更新啦！快去搜索看看吧 👉 /search", reminder.MediaTitle)
			s.telegram.SendMessage(reminder.UserID, msg, "", nil)

			reminder.IsNotified = true
			reminder.NotifiedAt = time.Now()
			s.reminders[id] = reminder

			logger.Info("[WeeklyReport] Sent reminder for %s to user %d", reminder.MediaTitle, reminder.UserID)
		}
	}

	go s.save()
}

// AddReminder adds a media reminder
func (s *WeeklyReportService) AddReminder(userID int64, userName string, tmdbID int, mediaTitle, mediaType string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	reminderID := fmt.Sprintf("rem_%d_%d", userID, tmdbID)

	// Check if already exists
	if _, exists := s.reminders[reminderID]; exists {
		return
	}

	reminder := &MediaReminder{
		ID:         reminderID,
		UserID:     userID,
		UserName:   userName,
		TmdbID:     tmdbID,
		MediaTitle: mediaTitle,
		MediaType:  mediaType,
		SearchDate: time.Now(),
	}

	s.reminders[reminderID] = reminder
	s.reminderIndex[userID] = append(s.reminderIndex[userID], reminderID)

	go s.save()

	logger.Info("[WeeklyReport] Added reminder for %s (user %d)", mediaTitle, userID)
}

// GetReport gets the latest report for a user
func (s *WeeklyReportService) GetReport(userID int64) (*WeeklyReport, bool) {
	weekStart, _ := getWeekRange(time.Now())
	reportKey := getReportKey(userID, weekStart)

	s.mu.RLock()
	defer s.mu.RUnlock()

	report, exists := s.reports[reportKey]
	return report, exists
}

// Stop stops the service
func (s *WeeklyReportService) Stop() {
	close(s.stopChan)
	if s.ticker != nil {
		s.ticker.Stop()
	}
}

// Helper functions

func nextWeekday(t time.Time, weekday time.Weekday) time.Time {
	daysUntil := int(weekday) - int(t.Weekday())
	if daysUntil <= 0 {
		daysUntil += 7
	}
	return t.AddDate(0, 0, daysUntil)
}

func containsGenre(s, genre string) bool {
	// Simple genre matching
	return len(s) >= len(genre) && (s == genre ||
		fmt.Sprintf("%s", s) == genre)
}
