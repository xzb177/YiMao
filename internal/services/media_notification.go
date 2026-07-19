package services

import (
	"encoding/json"
	"fmt"
	"github.com/xzb177/yimao/internal/richmessage"
	"github.com/xzb177/yimao/pkg/logger"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NotificationFormat represents the notification format style
type NotificationFormat string

const (
	// FormatSimple uses simple, concise format
	FormatSimple NotificationFormat = "simple"
	// FormatDetailed uses detailed format with file info
	FormatDetailed NotificationFormat = "detailed"
)

// MediaType categories for notification (extending the base MediaType from moviepilot.go)
const (
	MediaTypeSeries MediaType = "series"
	MediaTypeAnime  MediaType = "anime"
)

// MediaItem represents a media item that was added to the library
type MediaItem struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	LibraryName string    `json:"library_name"`
	MediaType   MediaType `json:"media_type"`
	Year        int       `json:"year"`
	// For series/anime
	SeriesName   string `json:"series_name,omitempty"`
	SeasonNumber int    `json:"season_number,omitempty"`
	EpisodeCount int    `json:"episode_count,omitempty"`
	EpisodeStart int    `json:"episode_start,omitempty"`
	EpisodeEnd   int    `json:"episode_end,omitempty"`
	IsCompleted  bool   `json:"is_completed,omitempty"`
	// Metadata
	Quality  string   `json:"quality,omitempty"`
	Rating   float64  `json:"rating,omitempty"`
	Genres   []string `json:"genres,omitempty"`
	Overview string   `json:"overview,omitempty"`
	ImageURL string   `json:"image_url,omitempty"`
	// File info
	FileSize  int64  `json:"file_size,omitempty"`
	FileCount int    `json:"file_count,omitempty"`
	IsWEBDL   bool   `json:"is_webdl,omitempty"`
	FileName  string `json:"file_name,omitempty"` // Original filename for title extraction
	// Timestamp
	AddedAt time.Time `json:"added_at"`
}

// AdminNotificationSettings stores notification preferences for an admin
type AdminNotificationSettings struct {
	AdminID             int64              `json:"admin_id"`
	SingleEnabled       bool               `json:"single_enabled"`        // Enable instant notification to group (入库群组通知)
	DailyTime           string             `json:"daily_time"`            // Format: "HH:MM", default "00:10"
	DailySummaryEnabled bool               `json:"daily_summary_enabled"` // Enable daily summary notification (private message)
	Libraries           []string           `json:"libraries"`             // Specific libraries to monitor for daily summary, empty = all
	Format              NotificationFormat `json:"format"`                // Notification format: simple or detailed
}

// MediaNotificationService handles media library notifications
type MediaNotificationService struct {
	dataFile     string
	telegram     *TelegramClient
	adminService *AdminService
	moviepilot   *MoviePilotClient
	groupChatID  int64 // 群组 ChatID，用于发送每日汇总

	// Settings per admin
	settings map[int64]*AdminNotificationSettings

	// Daily pending items (key: adminID -> date -> items)
	pendingItems     map[string]map[string][]*MediaItem // "adminID" -> "YYYY-MM-DD" -> items
	lastSummarySent  map[int64]string
	groupSummarySent string

	// Title resolver
	titleResolver *TitleResolver

	mu sync.RWMutex

	// Channels
	itemChan chan *MediaItem
	doneChan chan struct{}
}

// NewMediaNotificationService creates a new media notification service
func NewMediaNotificationService(dataDir string, telegram *TelegramClient, adminService *AdminService, groupChatID int64, moviepilot *MoviePilotClient) *MediaNotificationService {
	dataFile := fmt.Sprintf("%s/media_notifications.json", dataDir)

	service := &MediaNotificationService{
		dataFile:        dataFile,
		telegram:        telegram,
		adminService:    adminService,
		moviepilot:      moviepilot,
		groupChatID:     groupChatID,
		settings:        make(map[int64]*AdminNotificationSettings),
		pendingItems:    make(map[string]map[string][]*MediaItem),
		lastSummarySent: make(map[int64]string),
		titleResolver:   NewTitleResolver(),
		itemChan:        make(chan *MediaItem, 100),
		doneChan:        make(chan struct{}),
	}

	service.load()

	logger.Info("[MediaNotification] Service initialized")

	// Start processing items
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("[MediaNotification] processItems panic: %v", r)
			}
		}()
		service.processItems()
	}()

	// Start daily summary scheduler
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("[MediaNotification] scheduleDailySummaries panic: %v", r)
			}
		}()
		service.scheduleDailySummaries()
	}()

	return service
}

// load loads notification settings from file
func (s *MediaNotificationService) load() error {
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
		Settings         map[int64]*AdminNotificationSettings `json:"settings"`
		LastSummarySent  map[int64]string                     `json:"last_summary_sent"`
		GroupSummarySent string                               `json:"group_summary_sent"`
	}

	if err := json.Unmarshal(data, &fileData); err != nil {
		return err
	}

	s.settings = fileData.Settings
	for _, settings := range s.settings {
		if settings != nil && settings.DailyTime == "23:50" {
			settings.DailyTime = "00:10"
			logger.Info("[MediaNotification] Migrated legacy daily summary time from 23:50 to 00:10")
		}
	}
	if fileData.LastSummarySent != nil {
		s.lastSummarySent = fileData.LastSummarySent
	}
	s.groupSummarySent = fileData.GroupSummarySent

	logger.Info("[MediaNotification] Loaded settings for %d admins", len(s.settings))
	return nil
}

// save saves notification settings to file
func (s *MediaNotificationService) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(map[string]interface{}{
		"settings":           s.settings,
		"last_summary_sent":  s.lastSummarySent,
		"group_summary_sent": s.groupSummarySent,
	}, "", "  ")
	if err != nil {
		logger.Info("[MediaNotification] Failed to marshal settings: %v", err)
		return err
	}

	if err := atomicWriteFile(s.dataFile, data, 0644); err != nil {
		logger.Info("[MediaNotification] Failed to save settings to %s: %v", s.dataFile, err)
		return err
	}

	logger.Info("[MediaNotification] Saved settings for %d admins to %s", len(s.settings), s.dataFile)
	return nil
}

// GetSettings returns notification settings for an admin
func (s *MediaNotificationService) GetSettings(adminID int64) *AdminNotificationSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if settings, exists := s.settings[adminID]; exists {
		return settings
	}

	// Return default settings
	return &AdminNotificationSettings{
		AdminID:             adminID,
		SingleEnabled:       true, // Default to enabled
		DailyTime:           "00:10",
		DailySummaryEnabled: false, // Default to disabled
		Libraries:           []string{},
		Format:              FormatDetailed,
	}
}

// SetSettings sets notification settings for an admin
func (s *MediaNotificationService) SetSettings(settings *AdminNotificationSettings) error {
	s.mu.Lock()
	s.settings[settings.AdminID] = settings
	s.mu.Unlock()

	return s.save()
}

// SetDailySummaryEnabled sets whether daily summary is enabled
func (s *MediaNotificationService) SetDailySummaryEnabled(adminID int64, enabled bool) error {
	settings := s.GetSettings(adminID)
	settings.DailySummaryEnabled = enabled
	logger.Info("[MediaNotification] SetDailySummaryEnabled: adminID=%d, enabled=%v", adminID, enabled)
	return s.SetSettings(settings)
}

// SetSingleEnabled sets whether instant group notification is enabled
func (s *MediaNotificationService) SetSingleEnabled(adminID int64, enabled bool) error {
	settings := s.GetSettings(adminID)
	settings.SingleEnabled = enabled
	logger.Info("[MediaNotification] SetSingleEnabled: adminID=%d, enabled=%v", adminID, enabled)
	return s.SetSettings(settings)
}

// IsSingleEnabled checks if instant group notification is enabled (any admin has it enabled)
func (s *MediaNotificationService) IsSingleEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 没有任何设置记录时，保持默认开启（兼容旧行为）
	if len(s.settings) == 0 {
		return true
	}

	// 只要有任一管理员开启，就发送
	for _, settings := range s.settings {
		if settings != nil && settings.SingleEnabled {
			return true
		}
	}

	// 存在设置但无人开启 => 关闭
	return false
}

// SetDailyTime sets the daily summary time for an admin
func (s *MediaNotificationService) SetDailyTime(adminID int64, timeStr string) error {
	settings := s.GetSettings(adminID)
	settings.DailyTime = timeStr
	return s.SetSettings(settings)
}

// SetFormat sets the notification format for an admin
func (s *MediaNotificationService) SetFormat(adminID int64, format NotificationFormat) error {
	settings := s.GetSettings(adminID)
	settings.Format = format
	return s.SetSettings(settings)
}

// AddItem adds a media item for notification processing
func (s *MediaNotificationService) AddItem(item *MediaItem) {
	s.itemChan <- item
}

// processItems processes incoming media items
func (s *MediaNotificationService) processItems() {
	for {
		select {
		case item := <-s.itemChan:
			s.handleItem(item)
		case <-s.doneChan:
			return
		}
	}
}

// handleItem handles a single media item
// 群组通知由 webhook.go 统一处理
// 此函数仅用于将项目添加到每日汇总列表（管理员私聊通知）
func (s *MediaNotificationService) handleItem(item *MediaItem) {
	adminIDs := s.adminService.GetAdminIDs()
	today := time.Now().Format("2006-01-02")

	// Get all settings first WITHOUT holding any lock
	// Copy settings to avoid holding lock while processing
	adminSettings := make(map[int64]*AdminNotificationSettings)
	s.mu.RLock()
	for _, adminID := range adminIDs {
		if settings, exists := s.settings[adminID]; exists {
			// Copy the settings to avoid holding reference to map data
			settingsCopy := *settings
			adminSettings[adminID] = &settingsCopy
		} else {
			// Return default settings without calling GetSettings (which would try to acquire lock again)
			adminSettings[adminID] = &AdminNotificationSettings{
				AdminID:             adminID,
				SingleEnabled:       true,
				DailyTime:           "00:10",
				DailySummaryEnabled: false,
				Libraries:           []string{},
				Format:              FormatDetailed,
			}
		}
	}
	s.mu.RUnlock()

	// Now process with write lock for pendingItems
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, adminID := range adminIDs {
		settings := adminSettings[adminID]
		if settings == nil {
			continue
		}

		// Check library filter for daily summary
		if len(settings.Libraries) > 0 {
			libraryMatch := false
			for _, lib := range settings.Libraries {
				if strings.EqualFold(item.LibraryName, lib) {
					libraryMatch = true
					break
				}
			}
			if !libraryMatch {
				continue
			}
		}

		// Add to pending items for daily summary (if enabled)
		if settings.DailySummaryEnabled {
			adminKey := strconv.FormatInt(adminID, 10)
			if s.pendingItems[adminKey] == nil {
				s.pendingItems[adminKey] = make(map[string][]*MediaItem)
			}
			s.pendingItems[adminKey][today] = append(s.pendingItems[adminKey][today], item)
		}
	}
}

// sendInstantNotification sends an instant notification for a single item
func (s *MediaNotificationService) sendInstantNotification(adminID int64, item *MediaItem, format NotificationFormat) {
	// Build Rich Message data
	rmData := richmessage.InstantNotifyData{
		Title:        item.Title,
		Year:         item.Year,
		SeriesName:   item.SeriesName,
		SeasonNum:    item.SeasonNumber,
		EpisodeStart: item.EpisodeStart,
		EpisodeEnd:   item.EpisodeEnd,
		EpisodeCount: item.EpisodeCount,
		MediaType:    string(item.MediaType),
		Quality:      item.Quality,
		Rating:       item.Rating,
		Category:     s.getDetailedCategory(item),
		FileSize:     s.formatFileSizeDecimal(item.FileSize),
		FileCount:    item.FileCount,
		Time:         item.AddedAt.Format("15:04"),
		ImageURL:     item.ImageURL,
	}
	richMsg := richmessage.BuildInstantNotifyCard(rmData)

	// Plain text fallback
	var plainText string
	if format == FormatDetailed {
		plainText = s.formatDetailedMessage(item)
	} else {
		plainText = s.formatSimpleMessage(item)
	}

	// Try Rich Message, fall back to plain text
	if _, err := s.telegram.SendRichMessage(adminID, richMsg.Markdown, nil); err != nil {
		logger.Info("[MediaNotification] Rich Message failed for instant notify: %v, falling back", err)
		s.telegram.SendMessage(adminID, plainText, "", nil)
	}
}

// formatSimpleMessage formats a simple instant notification message
// 简洁格式：适合快速浏览，单屏显示
func (s *MediaNotificationService) formatSimpleMessage(item *MediaItem) string {
	var builder strings.Builder

	// Library emoji based on type
	emoji := "🎬"
	switch item.MediaType {
	case MediaTypeAnime:
		emoji = "🎨"
	case MediaTypeSeries:
		emoji = "📺"
	case MediaTypeMovie:
		emoji = "🎥"
	}

	// Build title
	var title string
	if item.SeriesName != "" {
		title = item.SeriesName
		if item.SeasonNumber > 0 {
			title += fmt.Sprintf(" S%d", item.SeasonNumber)
		}
		if item.EpisodeCount > 0 {
			if item.EpisodeStart > 0 && item.EpisodeEnd > 0 {
				title += fmt.Sprintf(" E%d-E%d", item.EpisodeStart, item.EpisodeEnd)
			} else if item.EpisodeCount == 1 {
				title += fmt.Sprintf(" E%d", item.EpisodeStart)
			} else {
				title += fmt.Sprintf(" +%d集", item.EpisodeCount)
			}
		}
	} else {
		title = item.Title
		if item.Year > 1900 && item.Year < 2100 {
			title += fmt.Sprintf(" (%d)", item.Year)
		}
	}

	// Header
	builder.WriteString(fmt.Sprintf("%s 新入库", emoji))

	// Info line
	builder.WriteString("\n\n")
	builder.WriteString(fmt.Sprintf("《 %s 》", title))

	// Meta info line
	metaInfo := []string{}
	if item.Quality != "" {
		metaInfo = append(metaInfo, item.Quality)
	}
	if item.Rating > 0 {
		metaInfo = append(metaInfo, fmt.Sprintf("⭐%.1f", item.Rating))
	}
	if len(metaInfo) > 0 {
		builder.WriteString("\n")
		builder.WriteString(strings.Join(metaInfo, " · "))
	}

	// Time
	builder.WriteString(fmt.Sprintf("\n🕒 %s", item.AddedAt.Format("15:04")))

	return builder.String()
}

// formatDetailedMessage formats a detailed notification message
func (s *MediaNotificationService) formatDetailedMessage(item *MediaItem) string {
	// Build title with year
	var title string
	if item.SeriesName != "" {
		title = item.SeriesName
		if item.Year > 1900 && item.Year < 2100 {
			title += fmt.Sprintf(" (%d)", item.Year)
		}
		if item.SeasonNumber > 0 {
			title += fmt.Sprintf(" S%02d", item.SeasonNumber)
		}
		if item.EpisodeCount > 0 {
			if item.EpisodeStart > 0 && item.EpisodeEnd > 0 {
				title += fmt.Sprintf(" E%02d-E%02d", item.EpisodeStart, item.EpisodeEnd)
			} else if item.EpisodeCount == 1 {
				title += fmt.Sprintf(" E%02d", item.EpisodeStart)
			}
		}
	} else {
		title = item.Title
		if item.Year > 1900 && item.Year < 2100 {
			title += fmt.Sprintf(" (%d)", item.Year)
		}
	}

	// Build quality string with better formatting
	quality := ""
	if item.Quality != "" {
		quality = item.Quality
		if item.IsWEBDL && !strings.Contains(strings.ToLower(quality), "web-dl") {
			quality = fmt.Sprintf("WEB-DL %s", quality)
		}
	}

	var builder strings.Builder

	// Image URL as first line (for Telegram auto-render)
	if item.ImageURL != "" {
		builder.WriteString(item.ImageURL)
		builder.WriteString("\n")
	}

	// Header line
	builder.WriteString(fmt.Sprintf("✅ 入库成功：%s\n", title))
	builder.WriteString("───────────────────\n\n")

	// Name line
	builder.WriteString(fmt.Sprintf("🎬 名称：`%s`\n", title))

	// Category line
	builder.WriteString(fmt.Sprintf("🏷️ 类别：`%s`\n", s.getDetailedCategory(item)))

	// Quality line
	if quality != "" {
		builder.WriteString(fmt.Sprintf("💎 质量：`%s`\n", quality))
	}

	// File size (ignore files smaller than 1MB)
	if item.FileSize > 1024*1024 {
		builder.WriteString(fmt.Sprintf("📦 总大小：`%s`\n", s.formatFileSizeDecimal(item.FileSize)))
	}

	// File count
	if item.FileCount > 0 {
		builder.WriteString(fmt.Sprintf("📁 文件数量：`%d` 个", item.FileCount))
	}

	return builder.String()
}

// formatFileSizeDecimal formats file size in decimal (GB not GiB) for consistency
func (s *MediaNotificationService) formatFileSizeDecimal(bytes int64) string {
	const unit = 1000 // Use decimal for GB display
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	// Convert to decimal GB for display
	gb := float64(bytes) / (1000 * 1000 * 1000)
	if gb >= 1 {
		return fmt.Sprintf("%.2fG", gb)
	}

	// For smaller sizes, use MB
	mb := float64(bytes) / (1000 * 1000)
	return fmt.Sprintf("%.2fM", mb)
}

// getDetailedCategory returns detailed category name based on genres and media type
func (s *MediaNotificationService) getDetailedCategory(item *MediaItem) string {
	// Check genres for region/category info
	if len(item.Genres) > 0 {
		for _, genre := range item.Genres {
			switch strings.ToLower(genre) {
			case "韩剧", "韩国", "korean":
				return "韩剧"
			case "日剧", "日本", "japanese":
				return "日剧"
			case "华语", "台湾", "香港", "中国", "chinese", "taiwanese", "hong kong":
				if item.MediaType == MediaTypeSeries {
					return "华语剧集"
				}
				return "华语电影"
			case "动漫", "动画", "anime", "animation":
				if item.MediaType == MediaTypeSeries {
					return "日本动漫"
				}
				return "动画电影"
			case "欧美", "美国", " british", "american", "western":
				if item.MediaType == MediaTypeSeries {
					return "美剧"
				}
				return "欧美电影"
			}
		}
	}

	// Fallback to media type
	switch item.MediaType {
	case MediaTypeSeries:
		return "剧集"
	case MediaTypeAnime:
		return "动画"
	case MediaTypeMovie:
		return "电影"
	default:
		return "电影/剧集"
	}
}

// scheduleDailySummaries schedules daily summary notifications
func (s *MediaNotificationService) scheduleDailySummaries() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.checkAndSendDailySummaries()
		case <-s.doneChan:
			return
		}
	}
}

// checkAndSendDailySummaries checks if it's time to send daily summaries
func (s *MediaNotificationService) checkAndSendDailySummaries() {
	now := time.Now()
	reportDay := now.AddDate(0, 0, -1)
	reportDayKey := reportDay.Format("2006-01-02")

	adminIDs := s.adminService.GetAdminIDs()

	// Get all settings first WITHOUT holding any lock
	adminSettings := make(map[int64]*AdminNotificationSettings)
	s.mu.RLock()
	for _, adminID := range adminIDs {
		if settings, exists := s.settings[adminID]; exists {
			settingsCopy := *settings
			adminSettings[adminID] = &settingsCopy
		} else {
			adminSettings[adminID] = &AdminNotificationSettings{
				AdminID:             adminID,
				SingleEnabled:       true,
				DailyTime:           "00:10",
				DailySummaryEnabled: false,
				Libraries:           []string{},
				Format:              FormatDetailed,
			}
		}
	}
	s.mu.RUnlock()

	// Now acquire write lock for pendingItems manipulation
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, adminID := range adminIDs {
		settings := adminSettings[adminID]
		if settings == nil {
			continue
		}

		// Only process admins with daily summary enabled and overall enabled
		if !settings.DailySummaryEnabled {
			continue
		}

		// Keep retrying after the configured time until this report day is
		// marked sent. This catches up after restarts and transient failures.
		if dailySummaryDue(now, settings.DailyTime, s.lastSummarySent[adminID]) {
			if s.lastSummarySent[adminID] == reportDayKey {
				continue
			}
			sendGroup := s.groupSummarySent != reportDayKey
			s.mu.Unlock()
			groupSent, err := s.sendTransferHistorySummary(adminID, reportDay, sendGroup)
			s.mu.Lock()
			if groupSent {
				s.groupSummarySent = reportDayKey
			}
			if err != nil {
				logger.Error("[MediaNotification] 每日汇总生成失败 admin=%d day=%s: %v", adminID, reportDayKey, err)
				if groupSent {
					s.mu.Unlock()
					_ = s.save()
					s.mu.Lock()
				}
				continue
			}
			s.lastSummarySent[adminID] = reportDayKey
			adminKey := strconv.FormatInt(adminID, 10)
			delete(s.pendingItems[adminKey], reportDayKey)
			s.mu.Unlock()
			_ = s.save()
			s.mu.Lock()
		}
	}
}

func dailySummaryDue(now time.Time, dailyTime, lastSentDay string) bool {
	reportDay := now.AddDate(0, 0, -1).Format("2006-01-02")
	if lastSentDay == reportDay {
		return false
	}
	scheduled, err := time.Parse("15:04", dailyTime)
	if err != nil {
		return false
	}
	return now.Hour()*60+now.Minute() >= scheduled.Hour()*60+scheduled.Minute()
}

func (s *MediaNotificationService) sendTransferHistorySummary(adminID int64, day time.Time, sendGroup bool) (bool, error) {
	if s.moviepilot == nil {
		return false, fmt.Errorf("MoviePilot client is not configured")
	}
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	rows, err := s.moviepilot.GetSuccessfulTransferHistory(start, start.AddDate(0, 0, 1))
	if err != nil {
		return false, err
	}
	summary := SummarizeTransferHistory(rows, start)
	if summary.FileCount == 0 {
		logger.Info("[MediaNotification] %s 无成功入库记录，跳过汇总", start.Format("2006-01-02"))
		return false, nil
	}
	movies := make([]richmessage.TransferDailySummarySeries, 0, len(summary.Movies))
	for _, item := range summary.Movies {
		movies = append(movies, richmessage.TransferDailySummarySeries{Title: item.DisplayTitle(), Files: item.Files})
	}
	series := make([]richmessage.TransferDailySummarySeries, 0, len(summary.Series))
	for _, item := range summary.Series {
		series = append(series, richmessage.TransferDailySummarySeries{Title: item.DisplayTitle(), Files: item.Files})
	}
	richMsg := richmessage.BuildTransferDailySummaryCard(
		start.Format("2006年1月2日"), movies, series, summary.FileCount,
		summary.FirstAt.Format("15:04:05"), summary.LastAt.Format("15:04:05"),
	)
	sendTo := func(chatID int64, label string) error {
		if _, err := s.telegram.SendRichMessage(chatID, richMsg.Markdown, nil); err != nil {
			return fmt.Errorf("send rich summary to %s %d: %w", label, chatID, err)
		}
		logger.Info("[MediaNotification] 已发送完整自然日汇总到 %s %d: day=%s movies=%d series=%d files=%d range=%s-%s",
			label, chatID, start.Format("2006-01-02"), summary.MovieCount, summary.SeriesCount, summary.FileCount,
			summary.FirstAt.Format("15:04:05"), summary.LastAt.Format("15:04:05"))
		return nil
	}
	if sendGroup && s.groupChatID != 0 && s.groupChatID < -100 {
		if err := sendTo(s.groupChatID, "群组"); err != nil {
			return false, err
		}
		if err := sendTo(adminID, "管理员"); err != nil {
			return true, err
		}
		return true, nil
	}
	return false, sendTo(adminID, "管理员")
}

// sendDailySummary sends a daily summary notification
// 同时发送到群组和管理员私聊
func (s *MediaNotificationService) sendDailySummary(adminID int64, items []*MediaItem) {
	now := time.Now()
	dateStr := now.Format("2006年1月2日")

	// Aggregate items for Rich Message
	movies := make([]*AggregatedMovie, 0)
	seriesMap := make(map[SeriesAggregationKey]*AggregatedSeries)

	for _, item := range items {
		if item.SeriesName != "" {
			key := SeriesAggregationKey{SeriesName: item.SeriesName, SeasonNumber: item.SeasonNumber}
			if existing, exists := seriesMap[key]; exists {
				if item.EpisodeStart > 0 {
					existing.Episodes = append(existing.Episodes, item.EpisodeStart)
				}
				if item.EpisodeEnd > 0 {
					for ep := item.EpisodeStart; ep <= item.EpisodeEnd; ep++ {
						existing.Episodes = append(existing.Episodes, ep)
					}
				}
				existing.Count = len(uniqueSortedInts(existing.Episodes))
				if len(existing.Episodes) > 0 {
					existing.MinEpisode = existing.Episodes[0]
					existing.MaxEpisode = existing.Episodes[len(existing.Episodes)-1]
				}
			} else {
				seriesMap[key] = NewAggregatedSeries([]*MediaItem{item})
			}
		} else {
			displayTitle := s.titleResolver.ResolveMovieTitle(item, item.FileName)
			movies = append(movies, &AggregatedMovie{Title: displayTitle, Year: item.Year, LibraryName: item.LibraryName, Count: 1})
		}
	}

	// Build Rich Message data
	rmMovies := make([]richmessage.DailySummaryMovie, 0, len(movies))
	for _, m := range movies {
		rmMovies = append(rmMovies, richmessage.DailySummaryMovie{Title: m.Title, Year: m.Year})
	}

	rmSeries := make([]richmessage.DailySummarySeries, 0, len(seriesMap))
	for _, agg := range seriesMap {
		rmSeries = append(rmSeries, richmessage.DailySummarySeries{Title: agg.FormatForSummary()})
	}

	totalCount := len(rmMovies) + len(rmSeries)
	richMsg := richmessage.BuildDailySummaryCard(dateStr, rmMovies, rmSeries, totalCount)
	plainText := s.formatDailySummary(now, items)

	// sendTo is a helper that tries Rich Message first, falls back to plain text
	sendTo := func(chatID int64, label string) {
		if _, err := s.telegram.SendRichMessage(chatID, richMsg.Markdown, nil); err != nil {
			logger.Info("[MediaNotification] Rich Message failed for %s %d: %v, falling back to plain text", label, chatID, err)
			if _, err2 := s.telegram.SendMessage(chatID, plainText, "", nil); err2 != nil {
				logger.Info("[MediaNotification] Failed to send daily summary to %s %d: %v", label, chatID, err2)
			}
		} else {
			logger.Info("[MediaNotification] 已发送每日汇总到 %s %d (Rich Message)", label, chatID)
		}
	}

	// 1. 发送到群组
	if s.groupChatID != 0 && s.groupChatID < -100 {
		settings := s.GetSettings(adminID)
		if settings.DailySummaryEnabled {
			sendTo(s.groupChatID, "群组")
		}
	}

	// 2. 发送到管理员私聊
	sendTo(adminID, "管理员")
}

// formatDailySummary formats a daily summary message
func (s *MediaNotificationService) formatDailySummary(date time.Time, items []*MediaItem) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("📅 %s 入库汇总\n\n", date.Format("2006-01-02")))

	// Separate movies and series
	movies := make([]*MediaItem, 0)
	series := make([]*MediaItem, 0)

	for _, item := range items {
		if item.SeriesName != "" {
			series = append(series, item)
		} else {
			movies = append(movies, item)
		}
	}

	// Aggregate series by (SeriesName, SeasonNumber)
	seriesAggregations := make(map[SeriesAggregationKey]*AggregatedSeries)
	for _, item := range series {
		key := SeriesAggregationKey{
			SeriesName:   item.SeriesName,
			SeasonNumber: item.SeasonNumber,
		}

		if existing, exists := seriesAggregations[key]; exists {
			// Add to existing aggregation
			if item.EpisodeStart > 0 {
				existing.Episodes = append(existing.Episodes, item.EpisodeStart)
			}
			if item.EpisodeEnd > 0 {
				for ep := item.EpisodeStart; ep <= item.EpisodeEnd; ep++ {
					existing.Episodes = append(existing.Episodes, ep)
				}
			}
			existing.Count = len(uniqueSortedInts(existing.Episodes))
			if len(existing.Episodes) > 0 {
				existing.MinEpisode = existing.Episodes[0]
				existing.MaxEpisode = existing.Episodes[len(existing.Episodes)-1]
			}
		} else {
			seriesAggregations[key] = NewAggregatedSeries([]*MediaItem{item})
		}
	}

	// Aggregate movies by title
	uniqueMovies := make(map[MovieAggregationKey]*AggregatedMovie)
	for _, item := range movies {
		// Resolve title with filename fallback (now uses item.FileName)
		displayTitle := s.titleResolver.ResolveMovieTitle(item, item.FileName)

		key := MovieAggregationKey{
			Title: displayTitle,
			Year:  item.Year,
		}

		if existing, exists := uniqueMovies[key]; exists {
			existing.Count++
		} else {
			uniqueMovies[key] = &AggregatedMovie{
				Title:       displayTitle,
				Year:        item.Year,
				LibraryName: item.LibraryName,
				Count:       1,
			}
		}
	}

	// Sort series by title
	sortedSeriesKeys := make([]SeriesAggregationKey, 0, len(seriesAggregations))
	for key := range seriesAggregations {
		sortedSeriesKeys = append(sortedSeriesKeys, key)
	}
	sortSeriesKeys(sortedSeriesKeys, seriesAggregations)

	// Sort movies by title
	sortedMovieKeys := make([]MovieAggregationKey, 0, len(uniqueMovies))
	for key := range uniqueMovies {
		sortedMovieKeys = append(sortedMovieKeys, key)
	}
	sortMovieKeys(sortedMovieKeys, uniqueMovies)

	// Build message
	if len(sortedSeriesKeys) > 0 {
		builder.WriteString(fmt.Sprintf("📺 剧集更新（%d 部）\n", len(sortedSeriesKeys)))
		for _, key := range sortedSeriesKeys {
			agg := seriesAggregations[key]
			builder.WriteString(fmt.Sprintf("• %s\n", agg.FormatForSummary()))
		}
		builder.WriteString("\n")
	}

	if len(sortedMovieKeys) > 0 {
		builder.WriteString(fmt.Sprintf("🎥 新增电影（%d 部）\n", len(sortedMovieKeys)))
		for _, key := range sortedMovieKeys {
			movie := uniqueMovies[key]
			builder.WriteString(fmt.Sprintf("• %s\n", movie.FormatForSummary()))
		}
		builder.WriteString("\n")
	}

	// Summary statistics
	builder.WriteString("📌 今日总览\n")
	totalWorks := len(sortedSeriesKeys) + len(sortedMovieKeys)
	builder.WriteString(fmt.Sprintf("合计：%d 部作品\n", totalWorks))
	if len(sortedSeriesKeys) > 0 {
		builder.WriteString(fmt.Sprintf("剧集更新：%d 部作品\n", len(sortedSeriesKeys)))
	}
	if len(sortedMovieKeys) > 0 {
		builder.WriteString(fmt.Sprintf("新增电影：%d 部作品\n", len(sortedMovieKeys)))
	}

	// Add record count in detailed mode (optional)
	totalRecords := len(items)
	if totalRecords != totalWorks {
		builder.WriteString(fmt.Sprintf("\n（处理记录：%d 条）", totalRecords))
	}

	return builder.String()
}

// formatItemForSummary formats a single item for the daily summary
// Note: This is now mainly used for instant notifications and sorting
func (s *MediaNotificationService) formatItemForSummary(item *MediaItem) string {
	var title string

	if item.SeriesName != "" {
		title = item.SeriesName

		// Add season
		if item.SeasonNumber > 0 {
			title += fmt.Sprintf(" 第%d季", item.SeasonNumber)
		}

		// Add episode range
		if item.EpisodeCount > 0 {
			if item.EpisodeStart > 0 && item.EpisodeEnd > 0 {
				title += fmt.Sprintf(" EP%02d-E%02d", item.EpisodeStart, item.EpisodeEnd)
			} else if item.EpisodeCount == 1 {
				// Single episode - show EP number
				title += fmt.Sprintf(" EP%02d", item.EpisodeStart)
			} else {
				// Multiple episodes without specific range
				title += fmt.Sprintf(" 共%d集", item.EpisodeCount)
			}
		}

		if item.IsCompleted {
			title += "（完结）"
		}
	} else {
		// Movie or standalone item - use title resolver
		title = s.titleResolver.ResolveMovieTitle(item, "")
	}

	return title
}

// sortSeriesKeys sorts series aggregation keys by their display names
func sortSeriesKeys(keys []SeriesAggregationKey, aggregations map[SeriesAggregationKey]*AggregatedSeries) {
	sort.Slice(keys, func(i, j int) bool {
		return aggregations[keys[i]].SeriesName < aggregations[keys[j]].SeriesName
	})
}

// sortMovieKeys sorts movie aggregation keys by their display titles
func sortMovieKeys(keys []MovieAggregationKey, aggregations map[MovieAggregationKey]*AggregatedMovie) {
	sort.Slice(keys, func(i, j int) bool {
		return aggregations[keys[i]].Title < aggregations[keys[j]].Title
	})
}

// detectLibraryCategory detects the category of a library
func (s *MediaNotificationService) detectLibraryCategory(libraryName string) string {
	name := strings.ToLower(libraryName)

	// Anime keywords
	animeKeywords := []string{"anim", "动画", "anime", "卡通", "漫画"}
	for _, kw := range animeKeywords {
		if strings.Contains(name, kw) {
			return "动画库"
		}
	}

	// TV/Series keywords
	tvKeywords := []string{"tv", "剧", "series", "show", "电视剧"}
	for _, kw := range tvKeywords {
		if strings.Contains(name, kw) {
			return "剧集库"
		}
	}

	// Movie keywords (default)
	return "电影库"
}

// calculateStats calculates statistics by media type
func (s *MediaNotificationService) calculateStats(items []*MediaItem) map[MediaType]int {
	stats := make(map[MediaType]int)

	for _, item := range items {
		stats[item.MediaType]++
	}

	return stats
}

// SendManualSummary sends a daily summary manually (for testing or admin request)
func (s *MediaNotificationService) SendManualSummary(adminID int64, date time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	adminKey := strconv.FormatInt(adminID, 10)
	dateStr := date.Format("2006-01-02")

	if items, exists := s.pendingItems[adminKey][dateStr]; exists && len(items) > 0 {
		s.sendDailySummary(adminID, items)
		// Clear sent items
		delete(s.pendingItems[adminKey], dateStr)
		return nil
	}

	return fmt.Errorf("no pending items for %s", dateStr)
}

// GetPendingItemsCount returns the count of pending items for an admin today
func (s *MediaNotificationService) GetPendingItemsCount(adminID int64) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	adminKey := strconv.FormatInt(adminID, 10)
	today := time.Now().Format("2006-01-02")

	if items, exists := s.pendingItems[adminKey][today]; exists {
		return len(items)
	}

	return 0
}

// Stop stops the service
func (s *MediaNotificationService) Stop() {
	close(s.doneChan)
}
