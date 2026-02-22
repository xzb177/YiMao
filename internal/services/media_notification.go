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

// NotificationMode represents the notification mode
type NotificationMode string

const (
	// ModeInstant sends notifications immediately when items are added
	ModeInstant NotificationMode = "instant"
	// ModeDaily sends a daily summary at a specified time
	ModeDaily NotificationMode = "daily"
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
	// Timestamp
	AddedAt time.Time `json:"added_at"`
}

// AdminNotificationSettings stores notification preferences for an admin
type AdminNotificationSettings struct {
	AdminID     int64             `json:"admin_id"`
	Mode        NotificationMode `json:"mode"`
	DailyTime   string           `json:"daily_time"` // Format: "HH:MM"
	Enabled     bool             `json:"enabled"`
	Libraries   []string         `json:"libraries"`   // Specific libraries to monitor, empty = all
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
		AdminID:   adminID,
		Mode:      ModeInstant,
		DailyTime: "20:00",
		Enabled:   true,
		Libraries: []string{},
	}
}

// SetSettings sets notification settings for an admin
func (s *MediaNotificationService) SetSettings(settings *AdminNotificationSettings) error {
	s.mu.Lock()
	s.settings[settings.AdminID] = settings
	s.mu.Unlock()

	return s.save()
}

// SetMode sets the notification mode for an admin
func (s *MediaNotificationService) SetMode(adminID int64, mode NotificationMode) error {
	settings := s.GetSettings(adminID)
	settings.Mode = mode
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
	s.mu.Lock()

	adminIDs := s.adminService.GetAdminIDs()
	today := time.Now().Format("2006-01-02")

	for _, adminID := range adminIDs {
		settings := s.GetSettings(adminID)

		// Skip if disabled
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

		switch settings.Mode {
		case ModeInstant:
			// Send immediately (unlock before sending to avoid deadlock)
			s.mu.Unlock()
			s.sendInstantNotification(adminID, item)
			s.mu.Lock()

		case ModeDaily:
			// Add to pending items
			adminKey := strconv.FormatInt(adminID, 10)
			if s.pendingItems[adminKey] == nil {
				s.pendingItems[adminKey] = make(map[string][]*MediaItem)
			}
			s.pendingItems[adminKey][today] = append(s.pendingItems[adminKey][today], item)
		}
	}

	s.mu.Unlock()
}

// sendInstantNotification sends an instant notification for a single item
func (s *MediaNotificationService) sendInstantNotification(adminID int64, item *MediaItem) {
	message := s.formatInstantMessage(item)

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

// formatInstantMessage formats an instant notification message
func (s *MediaNotificationService) formatInstantMessage(item *MediaItem) string {
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

	s.mu.Lock()
	defer s.mu.Unlock()

	adminIDs := s.adminService.GetAdminIDs()

	for _, adminID := range adminIDs {
		settings := s.GetSettings(adminID)

		// Only process admins with daily mode
		if settings.Mode != ModeDaily || !settings.Enabled {
			continue
		}

		// Check if it's time to send (within the same minute)
		if settings.DailyTime == currentTime {
			adminKey := strconv.FormatInt(adminID, 10)

			// Get pending items for today
			if items, exists := s.pendingItems[adminKey][today]; exists && len(items) > 0 {
				// Send summary
				s.sendDailySummary(adminID, items)

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
