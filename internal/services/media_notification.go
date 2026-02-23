package services

import (
	"encoding/json"
	"fmt"
	"log"
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
	Quality      string  `json:"quality,omitempty"`
	Rating       float64 `json:"rating,omitempty"`
	Genres       []string `json:"genres,omitempty"`
	Overview     string  `json:"overview,omitempty"`
	ImageURL     string  `json:"image_url,omitempty"`
	// File info
	FileSize     int64  `json:"file_size,omitempty"`
	FileCount    int    `json:"file_count,omitempty"`
	IsWEBDL      bool   `json:"is_webdl,omitempty"`
	// Timestamp
	AddedAt time.Time `json:"added_at"`
}

// AdminNotificationSettings stores notification preferences for an admin
type AdminNotificationSettings struct {
	AdminID           int64             `json:"admin_id"`
	DailyTime         string           `json:"daily_time"`         // Format: "HH:MM", default "12:59"
	Enabled           bool             `json:"enabled"`             // Overall notification toggle
	InstantEnabled    bool             `json:"instant_enabled"`    // Enable instant notifications
	DailySummaryEnabled bool             `json:"daily_summary_enabled"` // Enable daily summary notification
	Libraries         []string         `json:"libraries"`           // Specific libraries to monitor, empty = all
	Format           NotificationFormat `json:"format"`             // Notification format: simple or detailed
}

// MediaNotificationService handles media library notifications
type MediaNotificationService struct {
	dataFile     string
	telegram     *TelegramClient
	adminService *AdminService

	// Settings per admin
	settings map[int64]*AdminNotificationSettings

	// Daily pending items (key: adminID -> date -> items)
	pendingItems map[string]map[string][]*MediaItem // "adminID" -> "YYYY-MM-DD" -> items

	mu sync.RWMutex

	// Channels
	itemChan    chan *MediaItem
	doneChan    chan struct{}
}

// NewMediaNotificationService creates a new media notification service
func NewMediaNotificationService(dataDir string, telegram *TelegramClient, adminService *AdminService) *MediaNotificationService {
	dataFile := fmt.Sprintf("%s/media_notifications.json", dataDir)

	service := &MediaNotificationService{
		dataFile:     dataFile,
		telegram:     telegram,
		adminService: adminService,
		settings:     make(map[int64]*AdminNotificationSettings),
		pendingItems: make(map[string]map[string][]*MediaItem),
		itemChan:     make(chan *MediaItem, 100),
		doneChan:     make(chan struct{}),
	}

	service.load()

	log.Printf("[MediaNotification] Service initialized")

	// Start processing items
	go service.processItems()

	// Start daily summary scheduler
	go service.scheduleDailySummaries()

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
		Settings map[int64]*AdminNotificationSettings `json:"settings"`
	}

	if err := json.Unmarshal(data, &fileData); err != nil {
		return err
	}

	s.settings = fileData.Settings

	log.Printf("[MediaNotification] Loaded settings for %d admins", len(s.settings))
	return nil
}

// save saves notification settings to file
func (s *MediaNotificationService) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(map[string]interface{}{
		"settings": s.settings,
	}, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.dataFile, data, 0644)
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
		AdminID:           adminID,
		DailyTime:         "12:59",
		Enabled:           true,
		InstantEnabled:    true,  // Default to instant notifications
		DailySummaryEnabled: false, // Default to disabled
		Libraries:         []string{},
		Format:           FormatDetailed,
	}
}

// SetSettings sets notification settings for an admin
func (s *MediaNotificationService) SetSettings(settings *AdminNotificationSettings) error {
	s.mu.Lock()
	s.settings[settings.AdminID] = settings
	s.mu.Unlock()

	return s.save()
}

// SetInstantEnabled sets whether instant notifications are enabled
func (s *MediaNotificationService) SetInstantEnabled(adminID int64, enabled bool) error {
	settings := s.GetSettings(adminID)
	settings.InstantEnabled = enabled
	return s.SetSettings(settings)
}

// SetDailySummaryEnabled sets whether daily summary is enabled
func (s *MediaNotificationService) SetDailySummaryEnabled(adminID int64, enabled bool) error {
	settings := s.GetSettings(adminID)
	settings.DailySummaryEnabled = enabled
	return s.SetSettings(settings)
}

// SetDailyTime sets the daily summary time for an admin
func (s *MediaNotificationService) SetDailyTime(adminID int64, timeStr string) error {
	settings := s.GetSettings(adminID)
	settings.DailyTime = timeStr
	return s.SetSettings(settings)
}

// ToggleEnabled toggles notification on/off for an admin
func (s *MediaNotificationService) ToggleEnabled(adminID int64) bool {
	settings := s.GetSettings(adminID)
	settings.Enabled = !settings.Enabled
	s.SetSettings(settings)
	return settings.Enabled
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
				AdminID:           adminID,
				DailyTime:         "12:59",
				Enabled:           true,
				InstantEnabled:    true,
				DailySummaryEnabled: false,
				Libraries:         []string{},
				Format:           FormatDetailed,
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

		// Skip if completely disabled
		if !settings.Enabled {
			continue
		}

		// Check library filter
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

		// Always add to pending items for daily summary
		adminKey := strconv.FormatInt(adminID, 10)
		if s.pendingItems[adminKey] == nil {
			s.pendingItems[adminKey] = make(map[string][]*MediaItem)
		}
		s.pendingItems[adminKey][today] = append(s.pendingItems[adminKey][today], item)

		// Send instant notification if enabled (unlock before sending to avoid deadlock)
		if settings.InstantEnabled {
			s.mu.Unlock()
			s.sendInstantNotification(adminID, item, settings.Format)
			s.mu.Lock()
		}
	}
}

// sendInstantNotification sends an instant notification for a single item
func (s *MediaNotificationService) sendInstantNotification(adminID int64, item *MediaItem, format NotificationFormat) {
	var message string

	// Choose format based on settings
	if format == FormatDetailed {
		message = s.formatDetailedMessage(item)
	} else {
		message = s.formatSimpleMessage(item)
	}

	// Send with photo if available
	if item.ImageURL != "" {
		if _, err := s.telegram.SendPhoto(adminID, item.ImageURL, message); err != nil {
			log.Printf("[MediaNotification] Failed to send photo: %v", err)
			s.telegram.SendMessage(adminID, message, "", nil)
		}
	} else {
		s.telegram.SendMessage(adminID, message, "", nil)
	}
}

// formatSimpleMessage formats a simple instant notification message
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

	builder.WriteString(fmt.Sprintf("%s 入库通知\n\n", emoji))

	// Title
	if item.SeriesName != "" {
		builder.WriteString(fmt.Sprintf("《%s》", item.SeriesName))
		if item.SeasonNumber > 0 {
			builder.WriteString(fmt.Sprintf(" 第%d季", item.SeasonNumber))
		}
		if item.EpisodeCount > 0 {
			if item.EpisodeStart > 0 && item.EpisodeEnd > 0 {
				builder.WriteString(fmt.Sprintf(" EP%02d-EP%02d", item.EpisodeStart, item.EpisodeEnd))
			} else if item.EpisodeCount == 1 {
				builder.WriteString(fmt.Sprintf(" EP%02d", item.EpisodeStart))
			} else {
				builder.WriteString(fmt.Sprintf(" 共%d集", item.EpisodeCount))
			}
		}
		if item.IsCompleted {
			builder.WriteString("（完结）")
		}
	} else {
		builder.WriteString(fmt.Sprintf("《%s》", item.Title))
		if item.Year > 1900 && item.Year < 2100 {
			builder.WriteString(fmt.Sprintf(" (%d)", item.Year))
		}
	}

	builder.WriteString("\n")

	// Library
	builder.WriteString(fmt.Sprintf("📁 库: %s", item.LibraryName))

	// Quality
	if item.Quality != "" {
		builder.WriteString(fmt.Sprintf(" · 💎 %s", item.Quality))
	}

	// Rating
	if item.Rating > 0 {
		builder.WriteString(fmt.Sprintf(" · ⭐ %.1f", item.Rating))
	}

	builder.WriteString("\n")

	// Genres (max 2)
	if len(item.Genres) > 0 {
		genreCount := 2
		if len(item.Genres) < 2 {
			genreCount = len(item.Genres)
		}
		builder.WriteString("🎭 ")
		for i := 0; i < genreCount; i++ {
			if i > 0 {
				builder.WriteString("、")
			}
			builder.WriteString(item.Genres[i])
		}
		builder.WriteString("\n")
	}

	// Time
	builder.WriteString(fmt.Sprintf("🕒 %s", item.AddedAt.Format("15:04")))

	return builder.String()
}

// formatDetailedMessage formats a detailed notification message (new format)
func (s *MediaNotificationService) formatDetailedMessage(item *MediaItem) string {
	// Build title
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

	// Build quality string
	quality := ""
	if item.Quality != "" {
		quality = item.Quality
		if item.IsWEBDL && !strings.Contains(strings.ToLower(quality), "web-dl") {
			quality = fmt.Sprintf("WEB-DL %s", quality)
		}
	}

	// Build file info
	fileInfo := ""
	if item.FileSize > 0 {
		fileInfo += fmt.Sprintf("\n📦 总大小：%s", s.formatFileSizeDecimal(item.FileSize))
	}
	if item.FileCount > 0 {
		fileInfo += fmt.Sprintf("\n📁 文件数量：%d 个", item.FileCount)
	}

	// Build complete message - separator on same line as title
	message := fmt.Sprintf("✅ 入库成功：%s ───────────\n🎬 名称：%s\n🏷️ 类别：%s\n💎 质量： %s%s",
		title,
		title,
		s.getDetailedCategory(item),
		quality,
		fileInfo)

	return message
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
	currentTime := now.Format("15:04")
	today := now.Format("2006-01-02")

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
				AdminID:           adminID,
				DailyTime:         "12:59",
				Enabled:           true,
				InstantEnabled:    true,
				DailySummaryEnabled: false,
				Libraries:         []string{},
				Format:           FormatDetailed,
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
		if !settings.DailySummaryEnabled || !settings.Enabled {
			continue
		}

		// Check if it's time to send (within the same minute)
		if settings.DailyTime == currentTime {
			adminKey := strconv.FormatInt(adminID, 10)

			// Get pending items for today
			if items, exists := s.pendingItems[adminKey][today]; exists && len(items) > 0 {
				// Send summary (unlock before sending to avoid deadlock)
				s.mu.Unlock()
				s.sendDailySummary(adminID, items)
				s.mu.Lock()

				// Clear sent items
				delete(s.pendingItems[adminKey], today)
			}
		}
	}
}

// sendDailySummary sends a daily summary notification
func (s *MediaNotificationService) sendDailySummary(adminID int64, items []*MediaItem) {
	message := s.formatDailySummary(time.Now(), items)

	if _, err := s.telegram.SendMessage(adminID, message, "", nil); err != nil {
		log.Printf("[MediaNotification] Failed to send daily summary to %d: %v", adminID, err)
	}
}

// formatDailySummary formats a daily summary message
func (s *MediaNotificationService) formatDailySummary(date time.Time, items []*MediaItem) string {
	var builder strings.Builder

	// Header
	builder.WriteString(fmt.Sprintf("📅 %s 总入库目录\n\n", date.Format("2006-01-02")))

	// Group by library category
	// First, detect library categories
	libCategories := make(map[string]string) // libraryName -> category
	for _, item := range items {
		if _, exists := libCategories[item.LibraryName]; !exists {
			libCategories[item.LibraryName] = s.detectLibraryCategory(item.LibraryName)
		}
	}

	// Group items by library category
	categoryGroups := make(map[string][]*MediaItem) // category -> items
	for _, item := range items {
		category := libCategories[item.LibraryName]
		categoryGroups[category] = append(categoryGroups[category], item)
	}

	// Define category order and emoji
	categoryOrder := []string{"动画库", "剧集库", "电影库"}
	categoryEmojis := map[string]string{
		"动画库": "🎬",
		"剧集库": "📺",
		"电影库": "🎥",
	}

	// Build tree structure
	for _, category := range categoryOrder {
		items, exists := categoryGroups[category]
		if !exists || len(items) == 0 {
			continue
		}

		emoji := categoryEmojis[category]
		builder.WriteString(fmt.Sprintf("├─ %s %s\n", emoji, category))

		// Group by library name within category
		libGroups := make(map[string][]*MediaItem)
		for _, item := range items {
			libGroups[item.LibraryName] = append(libGroups[item.LibraryName], item)
		}

		// Sort library names
		libNames := make([]string, 0, len(libGroups))
		for name := range libGroups {
			libNames = append(libNames, name)
		}
		sort.Strings(libNames)

		// Print items per library
		for i, libName := range libNames {
			libItems := libGroups[libName]
			prefix := "│   ├─"
			if i == len(libNames)-1 {
				prefix = "│   └─"
			}

			for j, item := range libItems {
				if j == 0 {
					builder.WriteString(fmt.Sprintf("%s %s", prefix, s.formatItemForSummary(item)))
				} else {
					builder.WriteString(fmt.Sprintf("│   │   └─ %s", s.formatItemForSummary(item)))
				}
				builder.WriteString("\n")
			}
		}
		builder.WriteString("│\n")
	}

	// Summary stats
	stats := s.calculateStats(items)
	builder.WriteString("入库总览：\n")
	if stats[MediaTypeAnime] > 0 {
		builder.WriteString(fmt.Sprintf("动画：%d 部\n", stats[MediaTypeAnime]))
	}
	if stats[MediaTypeSeries] > 0 {
		builder.WriteString(fmt.Sprintf("剧集：%d 部\n", stats[MediaTypeSeries]))
	}
	if stats[MediaTypeMovie] > 0 {
		builder.WriteString(fmt.Sprintf("电影：%d 部\n", stats[MediaTypeMovie]))
	}

	return builder.String()
}

// formatItemForSummary formats a single item for the daily summary
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
		title = item.Title
		if item.Year > 1900 && item.Year < 2100 {
			title += fmt.Sprintf(" (%d)", item.Year)
		}
	}

	return title
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
