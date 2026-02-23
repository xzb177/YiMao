package services

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"emby-telegram-bot/pkg/types"
)

// EmbyWebhookPayload represents an Emby webhook payload
// Emby uses camelCase starting with lowercase
type EmbyWebhookPayload struct {
	Event      string `json:"NotificationType"` // Emby uses NotificationType
	EventField string `json:"Event"`             // Alternative event field
	ItemID     string `json:"ItemId"`
	ItemName   string `json:"ItemName"`
	ItemType   string `json:"ItemType"`
	Library    string `json:"LibraryName"`
	SeriesName string `json:"SeriesName"`
	Season     int    `json:"SeasonNumber"`
	Episode    int    `json:"IndexNumber"`
	Overview   string `json:"Overview"`
	Timestamp  string `json:"Timestamp"`
	UserID     string `json:"UserId"`
	UserName   string `json:"UserName"`
	Year       *int   `json:"Year"` // ProductionYear
	// Nested Item object (some Emby versions use this)
	Item     *EmbyItem `json:"Item"`
}

// EmbyItem represents a nested item in Emby webhook
type EmbyItem struct {
	Name        string `json:"Name"`
	Type        string `json:"Type"`
	Year        *int   `json:"Year"`
	Overview    string `json:"Overview"`
	Genres      []string `json:"Genres"`
	CommunityRating float64 `json:"CommunityRating"`
}

// JellyseerrWebhookPayload represents a Jellyseerr webhook payload
type JellyseerrWebhookPayload struct {
	Event     string                 `json:"event"`
	Subject   string                 `json:"subject"`
	Message   string                 `json:"message"`
	Issue     *JellyseerrIssue       `json:"issue,omitempty"`
	Media     *JellyseerrMedia       `json:"media,omitempty"`
	Request   *JellyseerrRequest     `json:"request,omitempty"`
	User      *JellyseerrUserWebhook `json:"user,omitempty"`
	CreatedAt string                 `json:"created_at"`
}

// JellyseerrIssue represents an issue in Jellyseerr
type JellyseerrIssue struct {
	ID       int64  `json:"id"`
	Status   string `json:"status"`
	Problem  string `json:"problem"`
	MediaID  int    `json:"mediaId"`
	Provider string `json:"provider"`
}

// JellyseerrMedia represents media in Jellyseerr
type JellyseerrMedia struct {
	MediaType string `json:"mediaType"`
	TmdbID    int    `json:"tmdbId"`
	Title     string `json:"title"`
	Status    string `json:"status"`
}

// JellyseerrRequest represents a request in Jellyseerr
type JellyseerrRequest struct {
	ID          int    `json:"id"`
	Status      string `json:"status"`
	MediaID     int    `json:"mediaId"`
	MediaType   string `json:"mediaType"`
	CreatedAt   string `json:"createdAt"`
}

// JellyseerrUserWebhook represents a user in webhook
type JellyseerrUserWebhook struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// MoviePilotWebhookPayload represents a MoviePilot webhook payload
type MoviePilotWebhookPayload struct {
	Event string `json:"event"` // subscribe, download, complete
	Data  struct {
		ID             int    `json:"id"`
		Name           string `json:"name"`
		Year           string `json:"year"`
		Type           string `json:"type"` // 电影, 电视剧
		Season         int    `json:"season"`
		TotalEpisode   int    `json:"total_episode"`
		State          string `json:"state"` // P, S, D, C, F, X
		StatusText     string `json:"status_text"`
		Username       string `json:"username"`
		MediaID        int    `json:"media_id"`
		Poster         string `json:"poster"`
		Overview       string `json:"overview"`
	} `json:"data"`
}

// WebhookService handles webhook processing
type WebhookService struct {
	telegram             *TelegramClient
	moviepilot           *MoviePilotClient
	userMapping          *UserMappingService
	adminService         *AdminService
	preferences          *PreferencesService
	chatID               int64
	embyURL              string
	embyAPIKey           string
	mediaNotificationSvc *MediaNotificationService
	messageCache         *MessageCache
}

// NewWebhookService creates a new webhook service
func NewWebhookService(telegram *TelegramClient, moviepilot *MoviePilotClient, userMapping *UserMappingService, adminService *AdminService, preferences *PreferencesService, chatID int64, embyURL, embyAPIKey string, mediaNotificationSvc *MediaNotificationService) *WebhookService {
	return &WebhookService{
		telegram:             telegram,
		moviepilot:           moviepilot,
		userMapping:          userMapping,
		adminService:         adminService,
		preferences:          preferences,
		chatID:               chatID,
		embyURL:              embyURL,
		embyAPIKey:           embyAPIKey,
		mediaNotificationSvc: mediaNotificationSvc,
		messageCache:         NewMessageCache(5 * time.Minute), // Cache for 5 minutes
	}
}

// HandleEmbyWebhook handles an incoming Emby webhook
func (s *WebhookService) HandleEmbyWebhook(payload EmbyWebhookPayload) error {
	// Use Event field if NotificationType is empty
	eventType := payload.Event
	if eventType == "" && payload.EventField != "" {
		eventType = payload.EventField
	}

	// Extract item name from nested Item if not set at top level
	itemName := payload.ItemName
	if itemName == "" && payload.Item != nil {
		itemName = payload.Item.Name
	}

	log.Printf("[Webhook] Emby event: %s, item: %s", eventType, itemName)

	// Normalize event name (Emby sends ItemAdded)
	event := strings.ToLower(eventType)

	switch event {
	case "item.added", "itemadded":
		return s.handleItemAdded(payload)
	case "item.updated", "itemupdated":
		// Skip update events to reduce noise
		return nil
	case "system.notificationtest", "system.test", "test", "playback.start", "playback.stop", "playback.pause", "playback.resume":
		// For test/playback events, send a simple notification
		if itemName != "" {
			message := fmt.Sprintf("🎬 Emby 通知\n\n事件: %s\n内容: %s", eventType, itemName)
			if s.chatID != 0 {
				s.telegram.SendMessage(s.chatID, message, "", nil)
			}
		}
		return s.handleTestNotification(payload)
	default:
		return nil
	}
}

// handleItemAdded handles new item added event
func (s *WebhookService) handleItemAdded(payload EmbyWebhookPayload) error {
	// Get item type from various sources
	itemType := payload.ItemType
	if itemType == "" && payload.Item != nil {
		itemType = payload.Item.Type
	}

	// Skip single episode additions to reduce noise
	// But track episode count for season completion notification
	if itemType == "Episode" {
		// Could track episode count here for later "Season complete" notification
		return nil
	}

	// Try to get enhanced info from Emby API (with retry)
	var enhancedPayload *EmbyEnhancedInfo
	var err error

	for i := 0; i < 5; i++ {
		enhancedPayload, err = s.getEmbyEnhancedInfo(payload.ItemID)
		if err == nil {
			break
		}
		log.Printf("[Webhook] Attempt %d: failed to get enhanced info: %v", i+1, err)
		time.Sleep(time.Second)
	}

	// Format message with enhanced info if available
	message := s.formatEmbyNotificationEnhanced(payload, enhancedPayload)

	// Check if we should send with photo
	if enhancedPayload != nil && enhancedPayload.ImageURL != "" {
		s.sendNotificationWithPhoto(message, enhancedPayload.ImageURL)
	} else {
		// Send to main chat with cache check
		if s.chatID != 0 {
			if !s.sendWithCache(s.chatID, message) {
				log.Printf("[Webhook] Duplicate notification skipped for chat %d", s.chatID)
			}
		}
	}

	// Also send to media notification service for admin notifications
	if s.mediaNotificationSvc != nil {
		mediaItem := s.convertToMediaItem(payload, enhancedPayload)
		s.mediaNotificationSvc.AddItem(mediaItem)
	}

	return nil
}

// EmbyEnhancedInfo holds enhanced media information from Emby API
type EmbyEnhancedInfo struct {
	Title        string
	Year         int
	Rating       float64
	Genres       []string
	Overview     string
	RunTimeTicks int64
	ImageURL     string
	Quality      string
	FileSize     int64
	FileCount    int
}

// getEmbyEnhancedInfo fetches enhanced information from Emby API
func (s *WebhookService) getEmbyEnhancedInfo(itemID string) (*EmbyEnhancedInfo, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return nil, fmt.Errorf("Emby not configured")
	}

	url := fmt.Sprintf("%s/Users/%s/Items/%s?Fields=MediaSources,MediaStreams,Genres,Overview,ProductionYear,CommunityRating,RunTimeTicks,ImageTags,BackdropImageTags",
		s.embyURL, s.embyAPIKey, itemID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Emby-Token", s.embyAPIKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Emby API returned status %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	info := &EmbyEnhancedInfo{}

	// Extract basic info
	if name, ok := result["Name"].(string); ok {
		info.Title = name
	}
	if year, ok := result["ProductionYear"].(float64); ok {
		info.Year = int(year)
	}
	if rating, ok := result["CommunityRating"].(float64); ok {
		info.Rating = rating
	}
	if overview, ok := result["Overview"].(string); ok {
		info.Overview = overview
	}
	if runtime, ok := result["RunTimeTicks"].(float64); ok {
		info.RunTimeTicks = int64(runtime)
	}

	// Extract genres
	if genres, ok := result["Genres"].([]interface{}); ok {
		for _, g := range genres {
			if genreStr, ok := g.(string); ok {
				info.Genres = append(info.Genres, genreStr)
			}
		}
	}

	// Extract image URL
	if itemID, ok := result["Id"].(string); ok {
		if tagName, ok := result["BackdropImageTags"].([]interface{}); ok && len(tagName) > 0 {
			if tag, ok := tagName[0].(string); ok {
				info.ImageURL = fmt.Sprintf("%s/Items/%s/Images/Backdrop/%s?fillWidth=800&quality=90",
					s.embyURL, itemID, tag)
			}
		}
		// Fallback to primary image
		if info.ImageURL == "" {
			if tagName, ok := result["ImageTags"].([]interface{}); ok && len(tagName) > 0 {
				if tag, ok := tagName[0].(string); ok {
					info.ImageURL = fmt.Sprintf("%s/Items/%s/Images/Primary/%s?fillWidth=800&quality=90",
						s.embyURL, itemID, tag)
				}
			}
		}
	}

	// Extract media info for quality and file count
	if mediaSources, ok := result["MediaSources"].([]interface{}); ok && len(mediaSources) > 0 {
		for _, ms := range mediaSources {
			if source, ok := ms.(map[string]interface{}); ok {
				// Get quality from media type
				if width, ok := source["Width"].(float64); ok {
					info.Quality = s.detectQuality(int(width))
				}

				// Get file size
				if size, ok := source["Size"].(float64); ok {
					info.FileSize = int64(size)
				}

				// Count media streams (video files)
				if streams, ok := source["MediaStreams"].([]interface{}); ok {
					videoCount := 0
					for _, stream := range streams {
						if s, ok := stream.(map[string]interface{}); ok {
							if st, ok := s["Type"].(string); ok && st == "Video" {
								videoCount++
							}
						}
					}
					info.FileCount = videoCount
				}
				break // Use first media source
			}
		}
	}

	return info, nil
}

// detectQuality detects video quality from width
func (s *WebhookService) detectQuality(width int) string {
	switch {
	case width >= 3800:
		return "4K"
	case width >= 1900:
		return "1080p"
	case width >= 1200:
		return "720p"
	default:
		return "SD"
	}
}

// formatEmbyNotificationEnhanced formats an enhanced Emby notification
func (s *WebhookService) formatEmbyNotificationEnhanced(payload EmbyWebhookPayload, enhanced *EmbyEnhancedInfo) string {
	var builder strings.Builder

	// Header
	builder.WriteString("✅ 入库成功")

	// Get title from various sources
	title := payload.ItemName
	if title == "" && payload.Item != nil {
		title = payload.Item.Name
	}
	if enhanced != nil && enhanced.Title != "" {
		title = enhanced.Title
	}

	// Get item type
	itemType := payload.ItemType
	if itemType == "" && payload.Item != nil {
		itemType = payload.Item.Type
	}

	// Add year if available
	year := 0
	if payload.Year != nil {
		year = *payload.Year
	}
	if year == 0 && payload.Item != nil && payload.Item.Year != nil {
		year = *payload.Item.Year
	}
	if year == 0 && enhanced != nil {
		year = enhanced.Year
	}

	// Get series name
	seriesName := payload.SeriesName

	if itemType == "Episode" && seriesName != "" {
		season := payload.Season
		episode := payload.Episode
		builder.WriteString(fmt.Sprintf("：%s S%02dE%02d", seriesName, season, episode))
	} else {
		builder.WriteString(fmt.Sprintf("：%s", title))
		if year > 1900 && year < 2100 {
			builder.WriteString(fmt.Sprintf(" (%d)", year))
		}
	}

	builder.WriteString("\n")
	builder.WriteString(strings.Repeat("─", 30))
	builder.WriteString("\n")

	// Media info line
	builder.WriteString(fmt.Sprintf("🎬 名称：%s", title))
	if seriesName != "" {
		builder.WriteString(fmt.Sprintf(" (%s)", seriesName))
	}
	builder.WriteString("\n")

	// Type and quality
	typeLabel := "电影/剧集"
	switch itemType {
	case "Movie":
		typeLabel = "电影"
	case "Episode", "Series":
		typeLabel = "剧集"
	case "Season":
		typeLabel = "剧集（整季）"
	}

	builder.WriteString(fmt.Sprintf("🏷️ 类别：%s", typeLabel))

	// Add quality if available
	if enhanced != nil && enhanced.Quality != "" {
		builder.WriteString(fmt.Sprintf(" · 💎 质量：%s", enhanced.Quality))
	}
	builder.WriteString("\n")

	// Rating
	if enhanced != nil && enhanced.Rating > 0 {
		// Convert to 10-point scale and show stars
		rating := enhanced.Rating
		if rating > 10 {
			rating = rating / 2 // Convert from 20-point to 10-point scale
		}
		stars := "★"
		if rating >= 2 {
			stars = "★★"
		}
		if rating >= 4 {
			stars = "★★★"
		}
		if rating >= 6 {
			stars = "★★★★"
		}
		if rating >= 8 {
			stars = "★★★★★"
		}
		builder.WriteString(fmt.Sprintf("⭐ 评分：%.1f %s", rating, stars))
		builder.WriteString("\n")
	}

	// Runtime
	if enhanced != nil && enhanced.RunTimeTicks > 0 {
		minutes := enhanced.RunTimeTicks / 600000000
		if minutes > 0 {
			hours := minutes / 60
			mins := minutes % 60
			if hours > 0 {
				builder.WriteString(fmt.Sprintf("⏳ 时长：%d时%d分\n", hours, mins))
			} else {
				builder.WriteString(fmt.Sprintf("⏳ 时长：%d分钟\n", mins))
			}
		}
	}

	// Genres (max 3)
	if enhanced != nil && len(enhanced.Genres) > 0 {
		genreCount := len(enhanced.Genres)
		if genreCount > 3 {
			genreCount = 3
		}
		builder.WriteString("🎭 类型：")
		for i := 0; i < genreCount; i++ {
			if i > 0 {
				builder.WriteString("、")
			}
			builder.WriteString(enhanced.Genres[i])
		}
		if len(enhanced.Genres) > 3 {
			builder.WriteString("...")
		}
		builder.WriteString("\n")
	}

	// File info
	if enhanced != nil && enhanced.FileSize > 0 {
		builder.WriteString(fmt.Sprintf("📦 大小：%s", s.formatFileSize(enhanced.FileSize)))
		if enhanced.FileCount > 0 {
			builder.WriteString(fmt.Sprintf(" · 📁 文件：%d个", enhanced.FileCount))
		}
		builder.WriteString("\n")
	}

	return builder.String()
}

// formatFileSize formats file size in human readable format
func (s *WebhookService) formatFileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// sendNotificationWithPhoto sends notification with photo
func (s *WebhookService) sendNotificationWithPhoto(message, photoURL string) {
	if s.chatID == 0 {
		return
	}

	// Check cache using message content as key
	if s.messageCache != nil && s.messageCache.Check(s.chatID, message) {
		log.Printf("[Webhook] Duplicate notification skipped (photo) for chat %d", s.chatID)
		return
	}

	// Send photo with caption
	if _, err := s.telegram.SendPhoto(s.chatID, photoURL, message); err != nil {
		log.Printf("[Webhook] Failed to send photo, falling back to text message: %v", err)
		// Fallback to text message
		s.sendWithCache(s.chatID, message)
		return
	}

	// Add to cache
	if s.messageCache != nil {
		s.messageCache.Add(s.chatID, message)
	}
}

// handleTestNotification handles test notification
func (s *WebhookService) handleTestNotification(payload EmbyWebhookPayload) error {
	message := "🔔 测试通知\n\nEmby 连接正常！"

	if s.chatID != 0 {
		// Use cache to avoid spamming test notifications
		if !s.sendWithCache(s.chatID, message) {
			return nil
		}
		return nil
	}

	return fmt.Errorf("no chat ID configured")
}

// HandleJellyseerrWebhook handles an incoming Jellyseerr webhook
func (s *WebhookService) HandleJellyseerrWebhook(payload JellyseerrWebhookPayload) error {
	log.Printf("[Webhook] Jellyseerr event: %s", payload.Event)

	switch payload.Event {
	case "request_created":
		return s.handleRequestCreated(payload)
	case "request_approved":
		return s.handleRequestApproved(payload)
	case "request_declined":
		return s.handleRequestDeclined(payload)
	case "request_available":
		return s.handleRequestAvailable(payload)
	case "issue_created":
		return s.handleIssueCreated(payload)
	case "issue_comment":
		return s.handleIssueComment(payload)
	case "issue_resolved":
		return s.handleIssueResolved(payload)
	case "test":
		return s.handleJellyseerrTest(payload)
	default:
		log.Printf("[Webhook] Unknown event: %s", payload.Event)
		return nil
	}
}

// handleRequestCreated handles new request event
func (s *WebhookService) handleRequestCreated(payload JellyseerrWebhookPayload) error {
	if payload.Media == nil || payload.User == nil {
		return nil
	}

	// Find user's Telegram ID
	telegramID, exists := s.userMapping.GetTelegramIDByJellyseerrID(payload.User.ID)
	if !exists {
		log.Printf("[Webhook] No Telegram mapping for Jellyseerr user %d", payload.User.ID)
		// Still notify admins
		return s.notifyAdminsAboutRequest(payload)
	}

	// Check user preferences
	if !s.preferences.ShouldNotify(telegramID, PrefApproveNotification, payload.Media.Title) {
		return nil
	}

	// Send confirmation to user
	message := s.formatRequestCreatedMessage(payload)
	if _, err := s.telegram.SendMessage(telegramID, message, "", nil); err != nil {
		log.Printf("[Webhook] Failed to send request created message: %v", err)
	}

	// Notify admins
	return s.notifyAdminsAboutRequest(payload)
}

// formatRequestCreatedMessage formats a request created message
func (s *WebhookService) formatRequestCreatedMessage(payload JellyseerrWebhookPayload) string {
	mediaType := "电影"
	if payload.Media.MediaType == "tv" {
		mediaType = "剧集"
	}

	return fmt.Sprintf("🎬 新求片请求\n\n%s\n%s\n\n✅ 请求已提交，等待管理员处理",
		payload.Media.Title,
		mediaType)
}

// notifyAdminsAboutRequest notifies all admins about a new request
func (s *WebhookService) notifyAdminsAboutRequest(payload JellyseerrWebhookPayload) error {
	adminIDs := s.adminService.GetAdminIDs()

	if len(adminIDs) == 0 {
		return nil
	}

	mediaType := "电影"
	if payload.Media != nil && payload.Media.MediaType == "tv" {
		mediaType = "剧集"
	}

	title := payload.Subject
	if payload.Media != nil {
		title = payload.Media.Title
	}

	message := fmt.Sprintf("🎬 新求片请求\n\n%s\n%s", title, mediaType)

	username := "未知用户"
	if payload.User != nil {
		username = payload.User.Username
	}

	requestID := ""
	if payload.Request != nil {
		requestID = strconv.Itoa(payload.Request.ID)
	}

	// Add action buttons for each admin
	for _, adminID := range adminIDs {
		keyboard := [][]map[string]string{
			{
				{"text": "✅ 批准", "callback_data": fmt.Sprintf("admin_approve_%s", requestID)},
				{"text": "❌ 拒绝", "callback_data": fmt.Sprintf("admin_decline_%s", requestID)},
			},
		}

		fullMessage := fmt.Sprintf("%s\n\n👤 用户: %s", message, username)
		if _, err := s.telegram.SendMessage(adminID, fullMessage, "", convertToInlineKeyboard(keyboard)); err != nil {
			log.Printf("[Webhook] Failed to notify admin %d: %v", adminID, err)
		}
	}

	return nil
}

// handleRequestApproved handles request approved event
func (s *WebhookService) handleRequestApproved(payload JellyseerrWebhookPayload) error {
	if payload.Media == nil || payload.User == nil {
		return nil
	}

	telegramID, exists := s.userMapping.GetTelegramIDByJellyseerrID(payload.User.ID)
	if !exists {
		return nil
	}

	if !s.preferences.ShouldNotify(telegramID, PrefApproveNotification, payload.Media.Title) {
		return nil
	}

	message := fmt.Sprintf("✅ 请求已批准\n\n%s\n\n🎬 正在处理中，完成后会通知你", payload.Media.Title)
	_, err := s.telegram.SendMessage(telegramID, message, "", nil)
	return err
}

// handleRequestDeclined handles request declined event
func (s *WebhookService) handleRequestDeclined(payload JellyseerrWebhookPayload) error {
	if payload.Media == nil || payload.User == nil {
		return nil
	}

	telegramID, exists := s.userMapping.GetTelegramIDByJellyseerrID(payload.User.ID)
	if !exists {
		return nil
	}

	if !s.preferences.ShouldNotify(telegramID, PrefAvailableNotification, payload.Media.Title) {
		return nil
	}

	// Check if media already exists
	existsMsg := ""
	if payload.Media.Status == "available" {
		existsMsg = "\n\n💡 这部电影已经在库中了，可以直接观看 🎬"
	} else if payload.Media.Status == "processing" {
		existsMsg = "\n\n💡 这部电影正在处理中，请耐心等待"
	}

	message := fmt.Sprintf("❌ 请求已拒绝\n\n%s%s", payload.Media.Title, existsMsg)
	_, err := s.telegram.SendMessage(telegramID, message, "", nil)
	return err
}

// handleRequestAvailable handles request available event
func (s *WebhookService) handleRequestAvailable(payload JellyseerrWebhookPayload) error {
	if payload.Media == nil || payload.User == nil {
		return nil
	}

	telegramID, exists := s.userMapping.GetTelegramIDByJellyseerrID(payload.User.ID)
	if !exists {
		return nil
	}

	if !s.preferences.ShouldNotify(telegramID, PrefAvailableNotification, payload.Media.Title) {
		return nil
	}

	// Mention user to get their attention
	username := s.userMapping.GetTelegramUsername(telegramID)
	if username == "" {
		username = "用户"
	}

	message := fmt.Sprintf("🎉 内容已可用！\n\n%s\n\n@%s 快来观看吧！", payload.Media.Title, username)
	_, err := s.telegram.SendMessage(telegramID, message, "", nil)
	return err
}

// handleIssueCreated handles issue created event
func (s *WebhookService) handleIssueCreated(payload JellyseerrWebhookPayload) error {
	if payload.Issue == nil {
		return nil
	}

	issueID := payload.Issue.ID
	if issueID == 0 {
		// Try to get issue ID from subject/message
		re := regexp.MustCompile(`Issue #(\d+)`)
		matches := re.FindStringSubmatch(payload.Subject + " " + payload.Message)
		if len(matches) > 1 {
			if id, err := strconv.ParseInt(matches[1], 10, 64); err == nil {
				issueID = id
			}
		}
	}

	// Get user info
	var userID int64
	var username string
	if payload.User != nil {
		userID = payload.User.ID
		username = payload.User.Username
	}

	// Get Telegram ID
	telegramID, exists := s.userMapping.GetTelegramIDByJellyseerrID(userID)
	if !exists {
		log.Printf("[Webhook] No Telegram mapping for user %d", userID)
	}

	// Notify admins
	return s.notifyAdminsAboutIssue(issueID, payload, telegramID, username)
}

// notifyAdminsAboutIssue notifies admins about a new issue
func (s *WebhookService) notifyAdminsAboutIssue(issueID int64, payload JellyseerrWebhookPayload, telegramID int64, username string) error {
	adminIDs := s.adminService.GetAdminIDs()

	if len(adminIDs) == 0 {
		return nil
	}

	// Determine priority emoji
	priorityEmoji := "🐛"
	if strings.Contains(strings.ToLower(payload.Subject), "audio") {
		priorityEmoji = "🔊"
	} else if strings.Contains(strings.ToLower(payload.Subject), "subtitle") {
		priorityEmoji = "💬"
	} else if strings.Contains(strings.ToLower(payload.Subject), "video") {
		priorityEmoji = "🎬"
	}

	message := fmt.Sprintf("%s 问题报告\n\n%s\n\n👉 用户: %s", priorityEmoji, payload.Subject, username)

	if telegramID != 0 {
		tgUsername := s.userMapping.GetTelegramUsername(telegramID)
		message += fmt.Sprintf(" (@%s, tg_id:%d)", tgUsername, telegramID)
	}

	if payload.Issue != nil && payload.Issue.Problem != "" {
		message += fmt.Sprintf("\n\n📝 问题: %s", payload.Issue.Problem)
	}

	// Add action buttons
	keyboard := [][]map[string]string{
		{
			{"text": "💬 回复", "callback_data": fmt.Sprintf("issue_reply_%d", issueID)},
			{"text": "✅ 已修复", "callback_data": fmt.Sprintf("issue_fixed_%d", issueID)},
		},
		{
			{"text": "ℹ️ 处理中", "callback_data": fmt.Sprintf("issue_processing_%d", issueID)},
			{"text": "❌ 关闭", "callback_data": fmt.Sprintf("issue_close_%d", issueID)},
		},
	}

	for _, adminID := range adminIDs {
		if _, err := s.telegram.SendMessage(adminID, message, "", convertToInlineKeyboard(keyboard)); err != nil {
			log.Printf("[Webhook] Failed to notify admin %d about issue: %v", adminID, err)
		}
	}

	return nil
}

// handleIssueComment handles new comment on issue
func (s *WebhookService) handleIssueComment(payload JellyseerrWebhookPayload) error {
	// For now, just log it
	log.Printf("[Webhook] Issue comment: %s", payload.Message)
	return nil
}

// handleIssueResolved handles issue resolved event
func (s *WebhookService) handleIssueResolved(payload JellyseerrWebhookPayload) error {
	// For now, just log it
	log.Printf("[Webhook] Issue resolved: %s", payload.Subject)
	return nil
}

// handleJellyseerrTest handles test webhook from Jellyseerr
func (s *WebhookService) handleJellyseerrTest(payload JellyseerrWebhookPayload) error {
	message := "🔔 测试通知\n\nJellyseerr 连接正常！"

	if s.chatID != 0 {
		_, err := s.telegram.SendMessage(s.chatID, message, "", nil)
		return err
	}

	return fmt.Errorf("no chat ID configured")
}

// GetEmbyMediaInfo fetches media info from Emby API
func (s *WebhookService) GetEmbyMediaInfo(itemID string) (map[string]interface{}, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return nil, fmt.Errorf("Emby URL or API key not configured")
	}

	url := fmt.Sprintf("%s/Users/%s/Items/%s", s.embyURL, s.embyAPIKey, itemID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Emby-Token", s.embyAPIKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Emby API returned status %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// SendJellyseerrIssueComment sends a comment to Jellyseerr issue
// Deprecated: Use MoviePilot API instead
func (s *WebhookService) SendJellyseerrIssueComment(issueID int64, comment string) error {
	if s.moviepilot == nil {
		return fmt.Errorf("MoviePilot client not configured")
	}

	// Note: MoviePilot API structure may differ, this is a placeholder for compatibility
	log.Printf("[Webhook] Issue comment functionality needs MoviePilot API implementation")
	return nil
}

// CloseJellyseerrIssue closes an issue in Jellyseerr
// Deprecated: Use MoviePilot API instead
func (s *WebhookService) CloseJellyseerrIssue(issueID int64) error {
	if s.moviepilot == nil {
		return fmt.Errorf("MoviePilot client not configured")
	}

	// Note: MoviePilot API structure may differ, this is a placeholder for compatibility
	log.Printf("[Webhook] Issue close functionality needs MoviePilot API implementation")
	return nil
}

// convertToInlineKeyboard converts a simple keyboard format to TelegramInlineKeyboard
func convertToInlineKeyboard(keyboard [][]map[string]string) *types.TelegramInlineKeyboard {
	if keyboard == nil {
		return nil
	}

	result := &types.TelegramInlineKeyboard{
		InlineKeyboard: make([][]types.TelegramInlineKeyboardButton, len(keyboard)),
	}

	for i, row := range keyboard {
		result.InlineKeyboard[i] = make([]types.TelegramInlineKeyboardButton, len(row))
		for j, btn := range row {
			result.InlineKeyboard[i][j] = types.TelegramInlineKeyboardButton{
				Text:         btn["text"],
				CallbackData: btn["callback_data"],
			}
		}
	}

	return result
}

// convertToMediaItem converts Emby webhook payload to MediaItem for notification service
func (s *WebhookService) convertToMediaItem(payload EmbyWebhookPayload, enhanced *EmbyEnhancedInfo) *MediaItem {
	item := &MediaItem{
		ID:          payload.ItemID,
		Title:       payload.ItemName,
		LibraryName: payload.Library,
		AddedAt:     time.Now(),
	}

	// Determine media type
	switch payload.ItemType {
	case "Movie":
		item.MediaType = MediaTypeMovie
	case "Episode":
		item.MediaType = MediaTypeSeries
		item.SeriesName = payload.SeriesName
		item.SeasonNumber = payload.Season
		item.EpisodeStart = payload.Episode
		item.EpisodeEnd = payload.Episode
		item.EpisodeCount = 1
	case "Season":
		item.MediaType = MediaTypeSeries
		item.SeriesName = payload.SeriesName
		item.SeasonNumber = payload.Season
		item.IsCompleted = true
	case "Series":
		item.MediaType = MediaTypeSeries
		item.SeriesName = payload.ItemName
	default:
		item.MediaType = MediaTypeMovie
	}

	// Enhanced info
	if enhanced != nil {
		if enhanced.Title != "" {
			item.Title = enhanced.Title
		}
		item.Year = enhanced.Year
		item.Rating = enhanced.Rating
		item.Overview = enhanced.Overview
		item.Quality = enhanced.Quality
		item.ImageURL = enhanced.ImageURL
		item.Genres = enhanced.Genres
	}

	return item
}

// sendWithCache sends a message with duplicate detection
// Returns true if message was sent, false if it was a duplicate
func (s *WebhookService) sendWithCache(chatID int64, message string) bool {
	if s.messageCache == nil {
		s.telegram.SendMessage(chatID, message, "", nil)
		return true
	}

	// Check if this message was recently sent
	if s.messageCache.Check(chatID, message) {
		return false
	}

	// Send the message
	s.telegram.SendMessage(chatID, message, "", nil)

	// Add to cache
	s.messageCache.Add(chatID, message)
	return true
}

// sendWithCacheAndKeyboard sends a message with keyboard and duplicate detection
func (s *WebhookService) sendWithCacheAndKeyboard(chatID int64, message string, keyboard *types.TelegramInlineKeyboard) {
	if s.messageCache != nil && s.messageCache.Check(chatID, message) {
		return
	}

	s.telegram.SendMessage(chatID, message, "", keyboard)

	if s.messageCache != nil {
		s.messageCache.Add(chatID, message)
	}
}

// HandleMoviePilotWebhook handles a MoviePilot webhook
func (s *WebhookService) HandleMoviePilotWebhook(payload MoviePilotWebhookPayload) error {
	log.Printf("[Webhook] MoviePilot event: %s, item: %s (user: %s)", payload.Event, payload.Data.Name, payload.Data.Username)

	switch payload.Event {
	case "subscribe":
		return s.handleMoviePilotSubscribe(payload)
	case "download":
		return s.handleMoviePilotDownload(payload)
	case "complete":
		return s.handleMoviePilotComplete(payload)
	default:
		log.Printf("[Webhook] Unknown MoviePilot event: %s", payload.Event)
		return nil
	}
}

// handleMoviePilotSubscribe handles new subscription event
func (s *WebhookService) handleMoviePilotSubscribe(payload MoviePilotWebhookPayload) error {
	// Format media type
	mediaType := "电影"
	if payload.Data.Type == "电视剧" {
		mediaType = "剧集"
	}

	// Build message
	message := fmt.Sprintf("🎬 新求片请求\n\n%s", payload.Data.Name)

	// Add year if available
	if payload.Data.Year != "" && payload.Data.Year != "0" {
		message += fmt.Sprintf(" (%s)", payload.Data.Year)
	}

	// Add season for TV shows
	if payload.Data.Type == "电视剧" && payload.Data.Season > 0 {
		message += fmt.Sprintf("\n📺 季数: 第%d季", payload.Data.Season)
	}

	// Add status
	statusText := GetStateText(payload.Data.State)
	message += fmt.Sprintf("\n\n%s\n%s", mediaType, statusText)

	// Try to find user's Telegram ID by MoviePilot username
	var userTelegramID int64
	if payload.Data.Username != "" && s.userMapping != nil {
		userTelegramID, _ = s.userMapping.GetTelegramIDByMoviePilotUsername(payload.Data.Username)
	}

	// Send confirmation to the requesting user
	if userTelegramID != 0 {
		userMessage := message
		userMessage += fmt.Sprintf("\n\n✅ 您的请求已提交，等待管理员处理")
		s.telegram.SendMessage(userTelegramID, userMessage, "", nil)
	}

	// Notify admins (without the username since they know who requested)
	adminIDs := s.adminService.GetAdminIDs()
	if len(adminIDs) == 0 {
		return nil
	}

	adminMessage := message
	if payload.Data.Username != "" {
		adminMessage += fmt.Sprintf("\n👤 用户: %s", payload.Data.Username)
	}

	for _, adminID := range adminIDs {
		// Add action buttons for subscription management
		keyboard := [][]map[string]string{
			{
				{"text": "✅ 已处理", "callback_data": fmt.Sprintf("mp_done_%d", payload.Data.ID)},
			},
		}
		s.sendWithCacheAndKeyboard(adminID, adminMessage, convertToInlineKeyboard(keyboard))
	}

	return nil
}

// handleMoviePilotDownload handles download started event
func (s *WebhookService) handleMoviePilotDownload(payload MoviePilotWebhookPayload) error {
	mediaType := "电影"
	if payload.Data.Type == "电视剧" {
		mediaType = "剧集"
	}

	message := fmt.Sprintf("📥 开始下载\n\n%s", payload.Data.Name)

	if payload.Data.Year != "" && payload.Data.Year != "0" {
		message += fmt.Sprintf(" (%s)", payload.Data.Year)
	}

	if payload.Data.Type == "电视剧" && payload.Data.Season > 0 {
		message += fmt.Sprintf("\n📺 第%d季", payload.Data.Season)
	}

	message += fmt.Sprintf("\n\n%s", mediaType)

	// Try to find user's Telegram ID by MoviePilot username
	var userTelegramID int64
	if payload.Data.Username != "" && s.userMapping != nil {
		userTelegramID, _ = s.userMapping.GetTelegramIDByMoviePilotUsername(payload.Data.Username)
	}

	// Send to the requesting user
	if userTelegramID != 0 {
		s.sendWithCache(userTelegramID, message)
	}

	return nil
}

// handleMoviePilotComplete handles download complete event
func (s *WebhookService) handleMoviePilotComplete(payload MoviePilotWebhookPayload) error {
	mediaType := "电影"
	if payload.Data.Type == "电视剧" {
		mediaType = "剧集"
	}

	message := fmt.Sprintf("✅ 下载完成\n\n%s", payload.Data.Name)

	if payload.Data.Year != "" && payload.Data.Year != "0" {
		message += fmt.Sprintf(" (%s)", payload.Data.Year)
	}

	if payload.Data.Type == "电视剧" {
		if payload.Data.TotalEpisode > 0 {
			message += fmt.Sprintf("\n📺 共 %d 集", payload.Data.TotalEpisode)
		}
		if payload.Data.Season > 0 {
			message += fmt.Sprintf(" (第%d季)", payload.Data.Season)
		}
	}

	message += fmt.Sprintf("\n\n%s", mediaType)

	// Try to find user's Telegram ID by MoviePilot username
	var userTelegramID int64
	if payload.Data.Username != "" && s.userMapping != nil {
		userTelegramID, _ = s.userMapping.GetTelegramIDByMoviePilotUsername(payload.Data.Username)
	}

	// Send to the requesting user
	if userTelegramID != 0 {
		s.sendWithCache(userTelegramID, message)
	}

	return nil
}

