package services

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	Season     int    `json:"SeasonNumber"`       // Deprecated: use ParentIndexNumber
	Episode    int    `json:"IndexNumber"`        // Deprecated: use IndexNumber in Item
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
	Path        string `json:"Path"`         // File path
	FileName    string `json:"FileName"`     // File name
	ProviderIds map[string]string `json:"ProviderIds"` // TMDB, IMDb, TVDB IDs
	MediaSources []EmbyMediaSource `json:"MediaSources"` // Media sources with file size
	// Parent/ Series info for episodes
	SeriesId             string   `json:"SeriesId"`
	SeriesName           string   `json:"SeriesName"`           // Series name for episodes
	SeasonName           string   `json:"SeasonName"`           // Season name
	ParentIndexNumber    *int     `json:"ParentIndexNumber"`    // Season number (correct field for episodes)
	IndexNumber          *int     `json:"IndexNumber"`          // Episode number (correct field for episodes)
	ParentBackdropItemId string   `json:"ParentBackdropItemId"`
	ParentBackdropImageTags []string `json:"ParentBackdropImageTags"`
	SeriesPrimaryImageTag string   `json:"SeriesPrimaryImageTag"`
	ParentThumbItemId     string   `json:"ParentThumbItemId"`
	ParentThumbImageTag   string   `json:"ParentThumbImageTag"`
	PrimaryImageAspectRatio float64 `json:"PrimaryImageAspectRatio"`
	ImageTags            map[string]string `json:"ImageTags"`
	BackdropImageTags    []string `json:"BackdropImageTags"`
}

// EmbyMediaSource represents a media source with file information
type EmbyMediaSource struct {
	Size int64  `json:"Size"`    // File size in bytes
	Path string `json:"Path"`    // File path
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
	notificationFormat   string // "simple" or "detailed"
	tmdbAPIKey           string // TMDB API key for fetching images
	// Episode aggregation - 每个剧集独立的防抖动机制
	epAggregation        map[string]*EpisodeAggregation  // key: seriesName_season
	epAggregationMu      sync.RWMutex
	aggregationDelay     time.Duration                  // 聚合延迟时间 (默认60秒)
}

// EpisodeAggregation holds aggregated episode info
type EpisodeAggregation struct {
	SeriesName   string
	Year         int
	Season       int
	Episodes     []int                   // episode numbers
	FirstAdded   time.Time
	Quality      string
	FileSize     int64
	FileCount    int
	ImageURL     string
	EnhancedInfo *EmbyEnhancedInfo
	LibraryName  string                  // Library name for category detection
	timer        *time.Timer             // Independent timer for this aggregation
	mu           sync.Mutex              // Mutex for this specific aggregation
}

// NewWebhookService creates a new webhook service
func NewWebhookService(telegram *TelegramClient, moviepilot *MoviePilotClient, userMapping *UserMappingService, adminService *AdminService, preferences *PreferencesService, chatID int64, embyURL, embyAPIKey string, mediaNotificationSvc *MediaNotificationService, notificationFormat string, tmdbAPIKey string) *WebhookService {
	svc := &WebhookService{
		telegram:           telegram,
		moviepilot:         moviepilot,
		userMapping:        userMapping,
		adminService:       adminService,
		preferences:        preferences,
		chatID:             chatID,
		embyURL:            embyURL,
		embyAPIKey:         embyAPIKey,
		mediaNotificationSvc: mediaNotificationSvc,
		messageCache:       NewMessageCache(5 * time.Minute),
		notificationFormat: notificationFormat,
		tmdbAPIKey:         tmdbAPIKey,
		epAggregation:      make(map[string]*EpisodeAggregation),
		aggregationDelay:   60 * time.Second,  // 默认60秒聚合延迟
	}

	return svc
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

	// 只对非入库事件打印日志
	event := strings.ToLower(eventType)
	isItemAdded := event == "item.added" || event == "itemadded" || event == "library.new" || event == "librarynew"
	if !isItemAdded {
		log.Printf("[Webhook] Emby event: %s, item: %s", eventType, itemName)
	}

	switch event {
	case "item.added", "itemadded", "library.new", "librarynew":
		// Handle both legacy item.added and Emby's library.new events
		return s.handleItemAdded(payload)
	case "item.updated", "itemupdated", "library.updated", "libraryupdated":
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
	// Get item type and name
	itemType := payload.ItemType
	if itemType == "" && payload.Item != nil {
		itemType = payload.Item.Type
	}

	itemName := payload.ItemName
	if itemName == "" && payload.Item != nil {
		itemName = payload.Item.Name
	}

	// For episodes, add to aggregation instead of sending immediately
	if itemType == "Episode" && payload.SeriesName != "" {
		return s.aggregateEpisode(payload)
	}

	// For movies and other content, send immediately
	return s.sendImmediateNotification(payload, itemType, itemName)
}

// sendImmediateNotification sends notification immediately for non-episode content
func (s *WebhookService) sendImmediateNotification(payload EmbyWebhookPayload, itemType, itemName string) error {
	// Debug: check if payload.Item is available
	if payload.Item != nil {
		log.Printf("[Debug] payload.Item exists: Name=%s, Path=%s", payload.Item.Name, payload.Item.Path)
		if payload.Item.ProviderIds != nil {
			log.Printf("[Debug] payload.Item.ProviderIds: %v", payload.Item.ProviderIds)
		} else {
			log.Printf("[Debug] payload.Item.ProviderIds is nil")
		}
	} else {
		log.Printf("[Debug] payload.Item is nil")
	}

	// Try to get enhanced info from Emby API (with retry)
	var enhancedPayload *EmbyEnhancedInfo
	var err error

	for i := 0; i < 5; i++ {
		enhancedPayload, err = s.getEmbyEnhancedInfo(payload.ItemID)
		if err == nil {
			break
		}
		// 只在最后一次尝试失败时打印日志
		if i == 4 {
			log.Printf("[Webhook] Failed to get enhanced info for %s: %v", itemName, err)
		}
		time.Sleep(time.Second)
	}

	// If enhanced info is missing or quality is empty, try to parse from webhook payload
	if (enhancedPayload == nil || enhancedPayload.Quality == "") && payload.Item != nil {
		path := payload.Item.Path
		if path == "" {
			path = payload.Item.FileName
		}
		if path != "" {
			quality := s.parseQualityFromPath(path)
			isWEBDL := s.detectWEBDL(path)
			log.Printf("[Webhook] Parsed quality from webhook path: %s (WEB-DL: %v)", quality, isWEBDL)

			if enhancedPayload == nil {
				// Create minimal enhanced info from webhook payload
				enhancedPayload = &EmbyEnhancedInfo{
					Quality: quality,
					IsWEBDL: isWEBDL,
				}
				// Copy available fields
				if payload.Item.Name != "" {
					enhancedPayload.Title = payload.Item.Name
				}
				if payload.Item.Year != nil {
					enhancedPayload.Year = *payload.Item.Year
				}
				if len(payload.Item.Genres) > 0 {
					enhancedPayload.Genres = payload.Item.Genres
				}
				if payload.Item.Overview != "" {
					enhancedPayload.Overview = payload.Item.Overview
				}
				if payload.Item.CommunityRating > 0 {
					enhancedPayload.Rating = payload.Item.CommunityRating
				}
				// Get file size from MediaSources
				for _, ms := range payload.Item.MediaSources {
					if ms.Size > 0 {
						enhancedPayload.FileSize = ms.Size
						enhancedPayload.FileCount = 1
						break
					}
				}
			} else {
				// Update existing enhanced info with parsed quality
				enhancedPayload.Quality = quality
				enhancedPayload.IsWEBDL = isWEBDL
				// If genres is empty, try to copy from webhook payload
				if len(enhancedPayload.Genres) == 0 && len(payload.Item.Genres) > 0 {
					enhancedPayload.Genres = payload.Item.Genres
				}
				// If file size is 0, try to get from MediaSources
				if enhancedPayload.FileSize == 0 {
					for _, ms := range payload.Item.MediaSources {
						if ms.Size > 0 {
							enhancedPayload.FileSize = ms.Size
							if enhancedPayload.FileCount == 0 {
								enhancedPayload.FileCount = 1
							}
							break
						}
					}
				}
			}

			// Try to get image from TMDB using ProviderIds (both for new and existing enhanced info)
			if enhancedPayload.ImageURL == "" && payload.Item.ProviderIds != nil {
				if tmdbID, ok := payload.Item.ProviderIds["tmdb"]; ok && tmdbID != "" {
					log.Printf("[Webhook] Getting TMDB backdrop from webhook ProviderIds: %s", tmdbID)
					if backdropURL := s.getTMDBBackdrop(tmdbID); backdropURL != "" {
						enhancedPayload.ImageURL = backdropURL
						log.Printf("[Webhook] Got TMDB backdrop: %s", backdropURL)
					}
				}
			}
		} else {
			// No path available in webhook payload - check MediaSources directly
			log.Printf("[Webhook] No path available, checking MediaSources (%d items)", len(payload.Item.MediaSources))
			if enhancedPayload == nil {
				enhancedPayload = &EmbyEnhancedInfo{}
			}
			// Get file info from MediaSources
			for _, ms := range payload.Item.MediaSources {
				if ms.Size > 0 {
					enhancedPayload.FileSize = ms.Size
					enhancedPayload.FileCount = 1
					log.Printf("[Webhook] Got file size from MediaSources: %d bytes", ms.Size)
					// Try to parse quality from MediaSource path
					if ms.Path != "" {
						enhancedPayload.Quality = s.parseQualityFromPath(ms.Path)
						enhancedPayload.IsWEBDL = s.detectWEBDL(ms.Path)
						log.Printf("[Webhook] Parsed quality from MediaSource path: %s", enhancedPayload.Quality)
					}
					break
				}
			}
			// Copy other available fields
			if payload.Item.Name != "" && enhancedPayload.Title == "" {
				enhancedPayload.Title = payload.Item.Name
			}
			if payload.Item.Year != nil && enhancedPayload.Year == 0 {
				enhancedPayload.Year = *payload.Item.Year
			}
			if len(payload.Item.Genres) > 0 && len(enhancedPayload.Genres) == 0 {
				enhancedPayload.Genres = payload.Item.Genres
			}
			if payload.Item.Overview != "" && enhancedPayload.Overview == "" {
				enhancedPayload.Overview = payload.Item.Overview
			}
			if payload.Item.CommunityRating > 0 && enhancedPayload.Rating == 0 {
				enhancedPayload.Rating = payload.Item.CommunityRating
			}
		}
	}

	// Format message with enhanced info if available, based on notification format
	var message string
	if s.notificationFormat == "simple" {
		message = s.formatEmbyNotificationSimple(payload, enhancedPayload)
	} else {
		message = s.formatEmbyNotificationEnhanced(payload, enhancedPayload)
	}

	// 简洁的入库日志
	if itemName != "" {
		log.Printf("[入库] %s", itemName)
	}

	// Add to daily summary list
	s.addMediaItemToSummary(payload, enhancedPayload)

	// Only send to main chat (group chat, not private)
	// 私聊不推送入库通知，只推送到群组
	if s.chatID != 0 && s.chatID < -100 { // Only send to group chats (chatID < -100)
		// Check if we should send with photo
		if enhancedPayload != nil && enhancedPayload.ImageURL != "" {
			// For photo notifications, use a more compact format to fit within 1024 char limit
			photoCaption := s.formatPhotoCaption(payload, enhancedPayload)
			s.sendNotificationWithPhoto(photoCaption, enhancedPayload.ImageURL)
		} else {
			s.sendWithCache(s.chatID, message)
		}
	}

	return nil
}

// aggregateEpisode adds episode to aggregation buffer with per-key debounce mechanism
func (s *WebhookService) aggregateEpisode(payload EmbyWebhookPayload) error {
	// Get season number from correct field (ParentIndexNumber in Item)
	season := 0
	if payload.Item != nil && payload.Item.ParentIndexNumber != nil {
		season = *payload.Item.ParentIndexNumber
	}
	if season == 0 && payload.Season != 0 {
		season = payload.Season
	}

	// Get episode number from correct field (IndexNumber in Item)
	episode := 0
	if payload.Item != nil && payload.Item.IndexNumber != nil {
		episode = *payload.Item.IndexNumber
	}
	if episode == 0 && payload.Episode != 0 {
		episode = payload.Episode
	}

	if episode == 0 {
		log.Printf("[入库聚合] 跳过无效集数: %s", payload.SeriesName)
		return nil
	}

	// Create aggregation key: seriesName_season
	aggregationKey := fmt.Sprintf("%s_S%02d", payload.SeriesName, season)

	s.epAggregationMu.Lock()

	// Get existing or create new aggregation
	agg, exists := s.epAggregation[aggregationKey]
	if !exists {
		// Extract parent series info from webhook payload for better image and genre lookup
		var seriesID string
		var parentBackdropItemID string
		var parentBackdropImageTags []string
		var seriesPrimaryImageTag string

		if payload.Item != nil {
			seriesID = payload.Item.SeriesId
			parentBackdropItemID = payload.Item.ParentBackdropItemId
			parentBackdropImageTags = payload.Item.ParentBackdropImageTags
			seriesPrimaryImageTag = payload.Item.SeriesPrimaryImageTag
		}

		// Try to get enhanced info using the new episode-aware function
		var enhancedInfo *EmbyEnhancedInfo
		for i := 0; i < 3; i++ {
			var err error
			enhancedInfo, err = s.getEmbyEnhancedInfoForEpisode(
				payload.ItemID,
				seriesID,
				parentBackdropItemID,
				parentBackdropImageTags,
				seriesPrimaryImageTag,
			)
			if err == nil {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}

		// If enhanced info is missing or quality is empty, try to parse from webhook payload
		if (enhancedInfo == nil || enhancedInfo.Quality == "") && payload.Item != nil {
			path := payload.Item.Path
			if path == "" {
				path = payload.Item.FileName
			}
			if path != "" {
				quality := s.parseQualityFromPath(path)
				isWEBDL := s.detectWEBDL(path)
				log.Printf("[Webhook] Parsed quality from webhook path (episode): %s (WEB-DL: %v)", quality, isWEBDL)

				if enhancedInfo == nil {
					// Create minimal enhanced info from webhook payload
					enhancedInfo = &EmbyEnhancedInfo{
						Quality: quality,
						IsWEBDL: isWEBDL,
					}
					// Copy available fields
					if payload.Item.Name != "" {
						enhancedInfo.Title = payload.Item.Name
					}
					if payload.Item.Year != nil {
						enhancedInfo.Year = *payload.Item.Year
					}
					if len(payload.Item.Genres) > 0 {
						enhancedInfo.Genres = payload.Item.Genres
					}
				}

				// Try to get image from TMDB using ProviderIds (both for new and existing enhanced info)
				if enhancedInfo.ImageURL == "" && payload.Item.ProviderIds != nil {
					if tmdbID, ok := payload.Item.ProviderIds["tmdb"]; ok && tmdbID != "" {
						log.Printf("[Webhook] Getting TMDB backdrop from webhook ProviderIds (episode): %s", tmdbID)
						if backdropURL := s.getTMDBBackdrop(tmdbID); backdropURL != "" {
							enhancedInfo.ImageURL = backdropURL
							log.Printf("[Webhook] Got TMDB backdrop (episode): %s", backdropURL)
						}
					}
				}
			}
		}

		year := 0
		if payload.Year != nil {
			year = *payload.Year
		}
		if year == 0 && enhancedInfo != nil {
			year = enhancedInfo.Year
		}

		// Create new aggregation with independent timer
		agg = &EpisodeAggregation{
			SeriesName:   payload.SeriesName,
			Year:         year,
			Season:       season,
			Episodes:     []int{},
			FirstAdded:   time.Now(),
			EnhancedInfo: enhancedInfo,
			LibraryName:  payload.Library,
		}
		if enhancedInfo != nil {
			agg.ImageURL = enhancedInfo.ImageURL
			agg.Quality = enhancedInfo.Quality
			if enhancedInfo.IsWEBDL {
				agg.Quality = fmt.Sprintf("WEB-DL %s", enhancedInfo.Quality)
			}
		}
		s.epAggregation[aggregationKey] = agg

		// Create independent timer for this aggregation key
		key := aggregationKey // Capture for closure
		agg.timer = time.AfterFunc(s.aggregationDelay, func() {
			s.flushSingleAggregation(key)
		})

		log.Printf("[入库聚合] 创建新聚合: %s S%02d, 延迟 %v 后发送", payload.SeriesName, season, s.aggregationDelay)
	}

	// Lock this specific aggregation for episode addition
	agg.mu.Lock()
	defer agg.mu.Unlock()

	// Check if episode already exists
	alreadyAdded := false
	for _, ep := range agg.Episodes {
		if ep == episode {
			alreadyAdded = true
			break
		}
	}

	// Extract file size and count from this episode
	var thisFileSize int64
	var thisFileCount int
	if payload.Item != nil {
		for _, ms := range payload.Item.MediaSources {
			if ms.Size > 0 {
				thisFileSize = ms.Size
				thisFileCount = 1
				break
			}
		}
	}

	if !alreadyAdded {
		agg.Episodes = append(agg.Episodes, episode)
		agg.FileSize += thisFileSize
		agg.FileCount += thisFileCount

		// Reset timer when new episode arrives (debounce)
		if agg.timer != nil {
			agg.timer.Stop()
		}
		key := aggregationKey // Capture for closure
		agg.timer = time.AfterFunc(s.aggregationDelay, func() {
			s.flushSingleAggregation(key)
		})

		log.Printf("[入库聚合] 添加集数: %s S%02dE%02d (当前共%d集, 总大小:%s), 重置定时器 %v",
			payload.SeriesName, season, episode, len(agg.Episodes),
			s.formatFileSizeDecimal(agg.FileSize), s.aggregationDelay)
	} else {
		log.Printf("[入库聚合] 集数已存在: %s S%02dE%02d, 跳过", payload.SeriesName, season, episode)
	}

	s.epAggregationMu.Unlock()

	return nil
}

// flushSingleAggregation sends a single aggregated episode notification (called by per-key timer)
func (s *WebhookService) flushSingleAggregation(key string) {
	s.epAggregationMu.Lock()
	agg, exists := s.epAggregation[key]
	if !exists {
		s.epAggregationMu.Unlock()
		return
	}

	// Lock this specific aggregation during flush
	agg.mu.Lock()

	// Stop and cleanup timer
	if agg.timer != nil {
		agg.timer.Stop()
		agg.timer = nil
	}

	// Make a copy of the data we need
	seriesName := agg.SeriesName
	season := agg.Season
	episodes := make([]int, len(agg.Episodes))
	copy(episodes, agg.Episodes)
	year := agg.Year
	quality := agg.Quality
	imageURL := agg.ImageURL
	fileSize := agg.FileSize
	fileCount := agg.FileCount
	enhancedInfo := agg.EnhancedInfo
	libraryName := agg.LibraryName

	// Remove from map
	delete(s.epAggregation, key)

	agg.mu.Unlock()
	s.epAggregationMu.Unlock()

	if len(episodes) == 0 {
		return
	}

	// Sort episodes
	sort.Ints(episodes)

	// Build episode range string
	epRange := buildEpisodeRangeString(episodes)

	// Build message based on notification format
	var message string
	if s.notificationFormat == "simple" {
		message = s.formatAggregatedEpisodeSimple(&EpisodeAggregation{
			SeriesName:   seriesName,
			Year:         year,
			Season:       season,
			Episodes:     episodes,
			Quality:      quality,
			ImageURL:     imageURL,
			FileSize:     fileSize,
			FileCount:    fileCount,
			EnhancedInfo: enhancedInfo,
			LibraryName:  libraryName,
		}, epRange)
	} else {
		message = s.formatAggregatedEpisodeMessage(&EpisodeAggregation{
			SeriesName:   seriesName,
			Year:         year,
			Season:       season,
			Episodes:     episodes,
			Quality:      quality,
			ImageURL:     imageURL,
			FileSize:     fileSize,
			FileCount:    fileCount,
			EnhancedInfo: enhancedInfo,
			LibraryName:  libraryName,
		}, epRange)
	}

	// Send notification only to group chats (chatID < -100)
	if s.chatID != 0 && s.chatID < -100 {
		if imageURL != "" {
			// For photo notifications, use a more compact format to fit within 1024 char limit
			photoCaption := s.formatEpisodePhotoCaption(&EpisodeAggregation{
				SeriesName:   seriesName,
				Year:         year,
				Season:       season,
				Episodes:     episodes,
				Quality:      quality,
				ImageURL:     imageURL,
				FileSize:     fileSize,
				FileCount:    fileCount,
				EnhancedInfo: enhancedInfo,
				LibraryName:  libraryName,
			}, epRange)
			s.sendNotificationWithPhoto(photoCaption, imageURL)
		} else {
			s.sendWithCache(s.chatID, message)
		}
	}

	log.Printf("[入库] %s S%02d %s (%d集, 总大小:%s)", seriesName, season, epRange, len(episodes), s.formatFileSizeDecimal(fileSize))

	// Add to daily summary list
	s.addAggregatedEpisodeToSummary(&EpisodeAggregation{
		SeriesName:   seriesName,
		Year:         year,
		Season:       season,
		Episodes:     episodes,
		Quality:      quality,
		FileSize:     fileSize,
		FileCount:    fileCount,
		EnhancedInfo: enhancedInfo,
		LibraryName:  libraryName,
	}, epRange)
}

// flushEpisodeAggregation sends all aggregated episode notifications (legacy support)
func (s *WebhookService) flushEpisodeAggregation() {
	s.epAggregationMu.Lock()
	defer s.epAggregationMu.Unlock()

	if len(s.epAggregation) == 0 {
		return
	}

	for key, agg := range s.epAggregation {
		if len(agg.Episodes) == 0 {
			continue
		}

		// Stop timer if exists
		agg.mu.Lock()
		if agg.timer != nil {
			agg.timer.Stop()
			agg.timer = nil
		}
		agg.mu.Unlock()

		// Sort episodes
		sort.Ints(agg.Episodes)

		// Build episode range string
		epRange := buildEpisodeRangeString(agg.Episodes)

		// Build message based on notification format
		var message string
		if s.notificationFormat == "simple" {
			message = s.formatAggregatedEpisodeSimple(agg, epRange)
		} else {
			message = s.formatAggregatedEpisodeMessage(agg, epRange)
		}

		// Send notification only to group chats (chatID < -100)
		if s.chatID != 0 && s.chatID < -100 {
			if agg.ImageURL != "" {
				// For photo notifications, use a more compact format to fit within 1024 char limit
				photoCaption := s.formatEpisodePhotoCaption(agg, epRange)
				s.sendNotificationWithPhoto(photoCaption, agg.ImageURL)
			} else {
				s.sendWithCache(s.chatID, message)
			}
		}

		log.Printf("[入库] %s S%02d %s (%d集)", agg.SeriesName, agg.Season, epRange, len(agg.Episodes))

		// Add to daily summary list
		s.addAggregatedEpisodeToSummary(agg, epRange)

		delete(s.epAggregation, key)
	}
}

// buildEpisodeRangeString builds episode range string like "E01-E05, E07, E09-E12"
func buildEpisodeRangeString(episodes []int) string {
	if len(episodes) == 0 {
		return ""
	}
	if len(episodes) == 1 {
		return fmt.Sprintf("E%02d", episodes[0])
	}

	var ranges []string
	start := episodes[0]
	end := episodes[0]

	for i := 1; i < len(episodes); i++ {
		if episodes[i] == end+1 {
			end = episodes[i]
		} else {
			// Flush current range
			if start == end {
				ranges = append(ranges, fmt.Sprintf("E%02d", start))
			} else {
				ranges = append(ranges, fmt.Sprintf("E%02d-E%02d", start, end))
			}
			start = episodes[i]
			end = episodes[i]
		}
	}
	// Flush last range
	if start == end {
		ranges = append(ranges, fmt.Sprintf("E%02d", start))
	} else {
		ranges = append(ranges, fmt.Sprintf("E%02d-E%02d", start, end))
	}

	return strings.Join(ranges, ", ")
}

// formatAggregatedEpisodeMessage formats aggregated episode notification
func (s *WebhookService) formatAggregatedEpisodeMessage(agg *EpisodeAggregation, epRange string) string {
	var builder strings.Builder

	// Build title
	var title string
	if agg.Year > 1900 && agg.Year < 2100 {
		title = fmt.Sprintf("%s (%d) S%02d %s", agg.SeriesName, agg.Year, agg.Season, epRange)
	} else {
		title = fmt.Sprintf("%s S%02d %s", agg.SeriesName, agg.Season, epRange)
	}

	// Image URL as first line (for Telegram auto-render)
	if agg.EnhancedInfo != nil && agg.EnhancedInfo.ImageURL != "" {
		builder.WriteString(agg.EnhancedInfo.ImageURL)
		builder.WriteString("\n")
	}

	// Header
	builder.WriteString(fmt.Sprintf("✅ 入库成功：%s\n", title))
	builder.WriteString("───────────────────\n\n")

	// Name line
	builder.WriteString(fmt.Sprintf("🎬 名称：%s\n", title))

	// Category - use enhanced info or default to 国产剧
	category := "国产剧"
	if agg.EnhancedInfo != nil {
		category = s.getDetailedCategory("Episode", agg.EnhancedInfo)
	}
	builder.WriteString(fmt.Sprintf("🏷️ 类别：%s\n", category))

	// Quality
	if agg.Quality != "" {
		builder.WriteString(fmt.Sprintf("💎 质量：%s\n", agg.Quality))
	}

	// File size
	if agg.FileSize > 0 {
		builder.WriteString(fmt.Sprintf("📦 总大小：%s\n", s.formatFileSizeDecimal(agg.FileSize)))
	}

	// File count
	if agg.FileCount > 0 {
		builder.WriteString(fmt.Sprintf("📁 文件数量：%d个", agg.FileCount))
	}

	return builder.String()
}

// formatAggregatedEpisodeSimple formats aggregated episode notification in simple format
func (s *WebhookService) formatAggregatedEpisodeSimple(agg *EpisodeAggregation, epRange string) string {
	var builder strings.Builder

	// Library emoji based on type
	emoji := "📺"

	builder.WriteString(fmt.Sprintf("%s 入库通知\n\n", emoji))

	// Title
	builder.WriteString(fmt.Sprintf("《%s》", agg.SeriesName))
	if agg.Year > 1900 && agg.Year < 2100 {
		builder.WriteString(fmt.Sprintf(" (%d)", agg.Year))
	}
	if agg.Season > 0 {
		builder.WriteString(fmt.Sprintf(" 第%d季", agg.Season))
	}
	// Episode range
	builder.WriteString(fmt.Sprintf(" %s", epRange))
	builder.WriteString("\n")

	// Quality
	if agg.Quality != "" {
		builder.WriteString(fmt.Sprintf("💎 %s", agg.Quality))
	}

	// File size (using decimal GB)
	if agg.FileSize > 0 {
		if agg.Quality != "" {
			builder.WriteString(" · ")
		}
		builder.WriteString(fmt.Sprintf("📦 %s", s.formatFileSizeDecimal(agg.FileSize)))
	}

	builder.WriteString("\n")

	// Time
	builder.WriteString(fmt.Sprintf("🕒 %s", time.Now().Format("15:04")))

	return builder.String()
}

// formatEmbyNotificationSimple formats an Emby notification in simple format
func (s *WebhookService) formatEmbyNotificationSimple(payload EmbyWebhookPayload, enhanced *EmbyEnhancedInfo) string {
	var builder strings.Builder

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

	// Library emoji based on type
	emoji := "🎬"
	switch itemType {
	case "Episode":
		emoji = "📺"
	case "Season":
		emoji = "📺"
	case "Series":
		emoji = "📺"
	}

	builder.WriteString(fmt.Sprintf("%s 入库通知\n\n", emoji))

	// Title
	if itemType == "Episode" && payload.SeriesName != "" {
		builder.WriteString(fmt.Sprintf("《%s》", payload.SeriesName))
		if year > 1900 && year < 2100 {
			builder.WriteString(fmt.Sprintf(" (%d)", year))
		}
		// Get season/episode from correct fields
		season := 0
		episode := 0
		if payload.Item != nil {
			if payload.Item.ParentIndexNumber != nil {
				season = *payload.Item.ParentIndexNumber
			}
			if payload.Item.IndexNumber != nil {
				episode = *payload.Item.IndexNumber
			}
		}
		if season == 0 && payload.Season != 0 {
			season = payload.Season
		}
		if episode == 0 && payload.Episode != 0 {
			episode = payload.Episode
		}
		// Only show season/episode if at least one is non-zero
		if season > 0 {
			builder.WriteString(fmt.Sprintf(" 第%d季", season))
		}
		if episode > 0 {
			builder.WriteString(fmt.Sprintf(" EP%02d", episode))
		}
	} else if itemType == "Season" && payload.SeriesName != "" {
		builder.WriteString(fmt.Sprintf("《%s》", payload.SeriesName))
		if year > 1900 && year < 2100 {
			builder.WriteString(fmt.Sprintf(" (%d)", year))
		}
		// Get season from correct field
		season := 0
		if payload.Item != nil && payload.Item.ParentIndexNumber != nil {
			season = *payload.Item.ParentIndexNumber
		}
		if season == 0 && payload.Season != 0 {
			season = payload.Season
		}
		if season > 0 {
			builder.WriteString(fmt.Sprintf(" 第%d季", season))
		}
	} else if itemType == "Series" {
		builder.WriteString(fmt.Sprintf("《%s》", title))
		if year > 1900 && year < 2100 {
			builder.WriteString(fmt.Sprintf(" (%d)", year))
		}
	} else {
		builder.WriteString(fmt.Sprintf("《%s》", title))
		if year > 1900 && year < 2100 {
			builder.WriteString(fmt.Sprintf(" (%d)", year))
		}
	}

	builder.WriteString("\n")

	// Library
	if payload.Library != "" {
		builder.WriteString(fmt.Sprintf("📁 库: %s", payload.Library))
	}

	// Quality
	quality := ""
	if enhanced != nil && enhanced.Quality != "" {
		quality = enhanced.Quality
		if enhanced.IsWEBDL && !strings.Contains(strings.ToLower(enhanced.Quality), "web-dl") {
			quality = fmt.Sprintf("WEB-DL %s", enhanced.Quality)
		}
	}
	if quality != "" {
		if payload.Library != "" {
			builder.WriteString(" · ")
		}
		builder.WriteString(fmt.Sprintf("💎 %s", quality))
	}

	// Rating
	rating := 0.0
	if payload.Item != nil && payload.Item.CommunityRating > 0 {
		rating = payload.Item.CommunityRating
	}
	if enhanced != nil && enhanced.Rating > 0 {
		rating = enhanced.Rating
	}
	if rating > 0 {
		if payload.Library != "" || quality != "" {
			builder.WriteString(" · ")
		}
		builder.WriteString(fmt.Sprintf("⭐ %.1f", rating))
	}

	builder.WriteString("\n")

	// Time
	builder.WriteString(fmt.Sprintf("🕒 %s", time.Now().Format("15:04")))

	return builder.String()
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
	IsWEBDL      bool   // Whether the source is WEB-DL
	Container    string // Container format (mkv, mp4, etc.)
	TMDBID       string // TMDB ID for fetching images
}

// getEmbyEnhancedInfoForEpisode fetches enhanced information from Emby API for episodes
// It queries both the episode and its parent series to get genres and backdrop
func (s *WebhookService) getEmbyEnhancedInfoForEpisode(itemID string, seriesID string, parentBackdropItemID string, parentBackdropImageTags []string, seriesPrimaryImageTag string) (*EmbyEnhancedInfo, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return nil, fmt.Errorf("Emby not configured")
	}

	info := &EmbyEnhancedInfo{}

	// First, query the Series to get Genres (episodes don't have genres)
	if seriesID != "" {
		seriesInfo, err := s.getSeriesInfo(seriesID)
		if err == nil && seriesInfo != nil {
			// Copy genres from series
			info.Genres = seriesInfo.Genres
			log.Printf("[Debug] Got %d genres from series %s", len(info.Genres), seriesID)
			// Copy TMDB ID from series for image lookup
			if seriesInfo.TMDBID != "" {
				info.TMDBID = seriesInfo.TMDBID
			}
		} else {
			log.Printf("[Debug] Failed to get series info: %v", err)
		}
	}

	// Query the episode for media info (quality, file size, etc.)
	episodeInfo, err := s.getEmbyEnhancedInfo(itemID)
	if err == nil && episodeInfo != nil {
		info.Quality = episodeInfo.Quality
		info.FileSize = episodeInfo.FileSize
		info.FileCount = episodeInfo.FileCount
		info.IsWEBDL = episodeInfo.IsWEBDL
		info.Container = episodeInfo.Container
		info.RunTimeTicks = episodeInfo.RunTimeTicks
		log.Printf("[Debug] Episode info - Quality: %s, FileSize: %d, FileCount: %d",
			info.Quality, info.FileSize, info.FileCount)
		// Use episode's title, year, rating, overview if series didn't provide them
		if info.Title == "" {
			info.Title = episodeInfo.Title
		}
		if info.Year == 0 {
			info.Year = episodeInfo.Year
		}
		if info.Rating == 0 {
			info.Rating = episodeInfo.Rating
		}
		if info.Overview == "" {
			info.Overview = episodeInfo.Overview
		}
		// Copy TMDB ID from episode if series didn't have it
		if info.TMDBID == "" && episodeInfo.TMDBID != "" {
			info.TMDBID = episodeInfo.TMDBID
		}
	} else {
		log.Printf("[Debug] Failed to get episode info: %v", err)
	}

	// Try to get image using parent backdrop info from webhook payload
	if parentBackdropItemID != "" && len(parentBackdropImageTags) > 0 {
		info.ImageURL = fmt.Sprintf("%s/Items/%s/Images/Backdrop/%s?fillWidth=800&quality=90",
			s.embyURL, parentBackdropItemID, parentBackdropImageTags[0])
		log.Printf("[Debug] Using parent backdrop from webhook: %s", info.ImageURL)
	} else if seriesPrimaryImageTag != "" && seriesID != "" {
		// Fallback to series primary image
		info.ImageURL = fmt.Sprintf("%s/Items/%s/Images/Primary/%s?maxWidth=600&quality=95",
			s.embyURL, seriesID, seriesPrimaryImageTag)
		log.Printf("[Debug] Using series primary image: %s", info.ImageURL)
	}

	// If no image yet, try TMDB using the TMDB ID we collected
	if info.ImageURL == "" && info.TMDBID != "" {
		if backdropURL := s.getTMDBBackdrop(info.TMDBID); backdropURL != "" {
			info.ImageURL = backdropURL
			log.Printf("[Debug] Using TMDB backdrop for episode: %s", backdropURL)
		}
	}

	return info, nil
}

// getSeriesInfo fetches series information from Emby API
func (s *WebhookService) getSeriesInfo(seriesID string) (*EmbyEnhancedInfo, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return nil, fmt.Errorf("Emby not configured")
	}

	url := fmt.Sprintf("%s/Users/%s/Items/%s?Fields=Genres,ProviderIds,Overview,ProductionYear,CommunityRating",
		s.embyURL, s.embyAPIKey, seriesID)

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

	// Extract genres
	if genres, ok := result["Genres"].([]interface{}); ok {
		for _, g := range genres {
			if genreStr, ok := g.(string); ok {
				info.Genres = append(info.Genres, genreStr)
			}
		}
	}

	// Extract TMDB ID
	if providerIds, ok := result["ProviderIds"].(map[string]interface{}); ok {
		if tid, ok := providerIds["tmdb"].(string); ok {
			info.TMDBID = tid
		}
	}

	return info, nil
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

	// Extract image URL - try to get from TMDB first (publicly accessible)
	// Check for TMDB ID in ProviderIds
	var tmdbID string
	if providerIds, ok := result["ProviderIds"].(map[string]interface{}); ok {
		if tid, ok := providerIds["tmdb"].(string); ok {
			tmdbID = tid
		}
	}

	log.Printf("[Debug] TMDB ID: %s", tmdbID)

	// Get backdrop from TMDB if we have the ID (横屏图片)
	if tmdbID != "" {
		if backdropURL := s.getTMDBBackdrop(tmdbID); backdropURL != "" {
			info.ImageURL = backdropURL
			log.Printf("[Debug] Using TMDB backdrop: %s", backdropURL)
		} else {
			log.Printf("[Debug] TMDB backdrop not found for ID: %s", tmdbID)
		}
	}

	// Fallback to Emby images if TMDB failed
	if info.ImageURL == "" {
		if itemID, ok := result["Id"].(string); ok {
			// Try backdrop first (横屏) - BackdropImageTags is an array
			if backdropImageTags, ok := result["BackdropImageTags"].([]interface{}); ok && len(backdropImageTags) > 0 {
				if tag, ok := backdropImageTags[0].(string); ok {
					info.ImageURL = fmt.Sprintf("%s/Items/%s/Images/Backdrop/%s?fillWidth=800&quality=90",
						s.embyURL, itemID, tag)
					log.Printf("[Debug] Using Emby backdrop: %s", info.ImageURL)
				}
			} else {
				log.Printf("[Debug] No Emby BackdropImageTags found")
			}
			// Fallback to primary image
			if info.ImageURL == "" {
				if imageTags, ok := result["ImageTags"].(map[string]interface{}); ok {
					if tag, ok := imageTags["Primary"].(string); ok {
						info.ImageURL = fmt.Sprintf("%s/Items/%s/Images/Primary/%s?maxWidth=600&quality=95",
							s.embyURL, itemID, tag)
						log.Printf("[Debug] Using Emby primary image: %s", info.ImageURL)
					}
				}
			}
		}
	} else {
		log.Printf("[Debug] ImageURL set: %s", info.ImageURL)
	}

	// Extract media info for quality and file count
	if mediaSources, ok := result["MediaSources"].([]interface{}); ok && len(mediaSources) > 0 {
		log.Printf("[Debug] MediaSources found: %d sources", len(mediaSources))
		for _, ms := range mediaSources {
			if source, ok := ms.(map[string]interface{}); ok {
				// Get quality from media type or parse from path
				if width, ok := source["Width"].(float64); ok {
					info.Quality = s.detectQuality(int(width))
					log.Printf("[Debug] Quality from Width: %s (width=%d)", info.Quality, int(width))
				} else {
					log.Printf("[Debug] No Width field in MediaSource")
					// Try to parse quality from file path
					if path, ok := source["Path"].(string); ok {
						info.Quality = s.parseQualityFromPath(path)
						log.Printf("[Debug] Quality from Path: %s (path=%s)", info.Quality, path)
					} else {
						log.Printf("[Debug] No Path field in MediaSource")
					}
				}

				// Get file size
				if size, ok := source["Size"].(float64); ok {
					info.FileSize = int64(size)
				}

				// Get container format
				if container, ok := source["Container"].(string); ok {
					info.Container = container
				}

				// Detect WEB-DL from path or name
				if path, ok := source["Path"].(string); ok {
					info.IsWEBDL = s.detectWEBDL(path)
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

// getTMDBBackdrop fetches backdrop URL from TMDB API (横屏图片)
func (s *WebhookService) getTMDBBackdrop(tmdbID string) string {
	apiKey := s.tmdbAPIKey
	if apiKey == "" {
		apiKey = "a62307d3a16cd0a605de3857d9ed614e" // fallback default key
	}

	client := &http.Client{Timeout: 5 * time.Second}

	// Try TMDB 3 API for movie details
	url := fmt.Sprintf("https://api.themoviedb.org/3/movie/%s?api_key=%s", tmdbID, apiKey)
	resp, err := client.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		// Try TV API
		tvURL := fmt.Sprintf("https://api.themoviedb.org/3/tv/%s?api_key=%s", tmdbID, apiKey)
		resp, err = client.Get(tvURL)
		if err != nil {
			return ""
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return ""
		}
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}

	// Get backdrop path (横屏图片)
	if backdropPath, ok := result["backdrop_path"].(string); ok && backdropPath != "" {
		return fmt.Sprintf("https://image.tmdb.org/t/p/original%s", backdropPath)
	}

	return ""
}

// getTMDBPoster fetches poster URL from TMDB API (竖版海报)
func (s *WebhookService) getTMDBPoster(tmdbID string) string {
	apiKey := s.tmdbAPIKey
	if apiKey == "" {
		apiKey = "a62307d3a16cd0a605de3857d9ed614e" // fallback default key
	}

	client := &http.Client{Timeout: 5 * time.Second}

	// Try TMDB 3 API for movie details (use English for consistent poster)
	url := fmt.Sprintf("https://api.themoviedb.org/3/movie/%s?api_key=%s", tmdbID, apiKey)
	resp, err := client.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		// Try TV API
		tvURL := fmt.Sprintf("https://api.themoviedb.org/3/tv/%s?api_key=%s", tmdbID, apiKey)
		resp, err = client.Get(tvURL)
		if err != nil {
			return ""
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return ""
		}
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}

	// Get poster path
	if posterPath, ok := result["poster_path"].(string); ok && posterPath != "" {
		return fmt.Sprintf("https://image.tmdb.org/t/p/w500%s", posterPath)
	}

	return ""
}

// detectWEBDL detects if the source is a WEB-DL from the path
func (s *WebhookService) detectWEBDL(path string) bool {
	pathLower := strings.ToLower(path)
	// Common WEB-DL indicators in file/folder names
	webdlIndicators := []string{"webdl", "web-dl", "webdl.", "web.", "webrip", "nf."}
	for _, indicator := range webdlIndicators {
		if strings.Contains(pathLower, indicator) {
			return true
		}
	}
	return false
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

// parseQualityFromPath parses quality from file path
func (s *WebhookService) parseQualityFromPath(path string) string {
	path = strings.ToLower(path)
	log.Printf("[Quality] Parsing quality from path: %s", path)

	// Check for resolution indicators in path
	if strings.Contains(path, "2160p") {
		return "2160p"
	}
	if strings.Contains(path, "1080p") {
		return "1080p"
	}
	if strings.Contains(path, "720p") {
		return "720p"
	}
	if strings.Contains(path, "480p") {
		return "480p"
	}

	// Fallback to detecting from container or default
	log.Printf("[Quality] Could not parse quality from path: %s", path)
	return "1080p"
}

// getFullQuality returns full quality string including WEB-DL info
func (s *WebhookService) getFullQuality(info *EmbyEnhancedInfo) string {
	if info == nil {
		return ""
	}

	quality := info.Quality
	if quality == "" {
		return ""
	}

	// Add WEB-DL prefix if applicable
	if info.IsWEBDL {
		return fmt.Sprintf("WEB-DL %s", quality)
	}

	return quality
}

// formatEmbyNotificationEnhanced formats an enhanced Emby notification (new detailed format)
func (s *WebhookService) formatEmbyNotificationEnhanced(payload EmbyWebhookPayload, enhanced *EmbyEnhancedInfo) string {
	var builder strings.Builder

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

	// Get series name - check both top-level and nested Item
	seriesName := payload.SeriesName
	if seriesName == "" && payload.Item != nil {
		seriesName = payload.Item.SeriesName
	}

	// Header line - "✅ 入库成功：名称"
	builder.WriteString("✅ 入库成功：")
	if itemType == "Episode" && seriesName != "" {
		builder.WriteString(seriesName)
	} else if itemType == "Season" && seriesName != "" {
		builder.WriteString(seriesName)
	} else if itemType == "Series" {
		builder.WriteString(title)
	} else {
		builder.WriteString(title)
	}
	builder.WriteString("\n")
	builder.WriteString("───────────────────\n")

	// 3. Empty line after separator
	builder.WriteString("\n")

	// 4. Content lines with backticks
	// Name line
	displayName := title
	if itemType == "Episode" && seriesName != "" {
		// Get season/episode from correct fields
		season := 0
		episode := 0
		if payload.Item != nil {
			if payload.Item.ParentIndexNumber != nil {
				season = *payload.Item.ParentIndexNumber
			}
			if payload.Item.IndexNumber != nil {
				episode = *payload.Item.IndexNumber
			}
		}
		if season == 0 && payload.Season != 0 {
			season = payload.Season
		}
		if episode == 0 && payload.Episode != 0 {
			episode = payload.Episode
		}
		// If both season and episode are 0, only show series name
		if season == 0 && episode == 0 {
			displayName = seriesName
		} else {
			displayName = fmt.Sprintf("%s S%02d E%02d", seriesName, season, episode)
		}
	} else if itemType == "Season" && seriesName != "" {
		season := 0
		if payload.Item != nil && payload.Item.ParentIndexNumber != nil {
			season = *payload.Item.ParentIndexNumber
		}
		if season == 0 && payload.Season != 0 {
			season = payload.Season
		}
		if season > 0 {
			displayName = fmt.Sprintf("%s S%02d", seriesName, season)
		} else {
			displayName = seriesName
		}
	}
	builder.WriteString(fmt.Sprintf("🎬 名称：%s\n", displayName))

	// Category line
	category := s.getDetailedCategory(itemType, enhanced)
	builder.WriteString(fmt.Sprintf("🏷️ 类别：%s\n", category))

	// Quality line
	quality := ""
	if enhanced != nil {
		quality = s.getFullQuality(enhanced)
	}
	if quality != "" {
		builder.WriteString(fmt.Sprintf("💎 质量：%s\n", quality))
	}

	// File size line - UNCONDITIONALLY display, no if conditions
	if enhanced != nil {
		if enhanced.FileSize > 1024*1024 {
			builder.WriteString(fmt.Sprintf("📦 总大小：%s\n", s.formatFileSizeDecimal(enhanced.FileSize)))
		} else {
			// Small file size or .strm files - always show
			builder.WriteString(fmt.Sprintf("📄 引用文件：%s\n", s.formatFileSizeDecimal(enhanced.FileSize)))
		}
	}

	// File count line - UNCONDITIONALLY display, no if conditions
	if enhanced != nil {
		builder.WriteString(fmt.Sprintf("📁 文件数量：%d个\n", enhanced.FileCount))
	}

	return builder.String()
}

// getDetailedCategory returns detailed category name based on genres and item type
func (s *WebhookService) getDetailedCategory(itemType string, enhanced *EmbyEnhancedInfo) string {
	if enhanced == nil {
		return s.getBasicCategory(itemType)
	}

	// Check genres for region/category info
	hasAnimation := false
	hasChinese := false
	hasJapanese := false
	hasKorean := false
	hasWestern := false
	hasCostume := false
	hasFantasy := false
	hasSciFi := false
	hasRomance := false
	hasAction := false

	for _, genre := range enhanced.Genres {
		g := strings.ToLower(genre)
		switch g {
		case "动漫", "动画", "anime", "animation", "cartoon":
			hasAnimation = true
		case "华语", "台湾", "香港", "中国", "chinese", "taiwanese", "hong kong", "国产", "大陆", "古装", "武侠":
			if g == "古装" || g == "武侠" {
				hasCostume = true
			}
			hasChinese = true
		case "日剧", "日本", "japanese", "japan":
			hasJapanese = true
		case "韩剧", "韩国", "korean", "korea":
			hasKorean = true
		case "欧美", "美国", "american", "western", "us", "uk", "british":
			hasWestern = true
		case "奇幻", "仙侠", " fantasy":
			hasFantasy = true
		case "科幻", "sci-fi", "scifi":
			hasSciFi = true
		case "言情", "爱情", "romance":
			hasRomance = true
		case "动作", "action":
			hasAction = true
		}
	}

	// Determine category based on item type and flags
	isEpisode := itemType == "Episode" || itemType == "Series" || itemType == "Season"

	if hasAnimation {
		if isEpisode {
			if hasChinese {
				return "国漫"
			}
			if hasJapanese {
				return "日漫"
			}
			return "动漫"
		}
		return "动画电影"
	}

	if hasCostume && isEpisode {
		return "古装剧"
	}

	if hasFantasy && hasCostume && isEpisode {
		return "古装奇幻"
	}

	if hasSciFi && isEpisode {
		return "科幻剧"
	}

	if hasRomance && isEpisode {
		return "言情剧"
	}

	if hasAction && hasCostume && isEpisode {
		return "武侠剧"
	}

	if hasChinese {
		if isEpisode {
			return "国产剧"
		}
		return "华语电影"
	}

	if hasJapanese {
		if isEpisode {
			return "日剧"
		}
		return "日本电影"
	}

	if hasKorean {
		if isEpisode {
			return "韩剧"
		}
		return "韩国电影"
	}

	if hasWestern {
		if isEpisode {
			return "美剧"
		}
		return "欧美电影"
	}

	// Fallback to basic category
	return s.getBasicCategory(itemType)
}

// getBasicCategory returns basic category name based on item type
func (s *WebhookService) getBasicCategory(itemType string) string {
	switch itemType {
	case "Movie":
		return "电影"
	case "Episode", "Series", "Season":
		return "剧集"
	default:
		return "电影/剧集"
	}
}

// getImageURL gets the image URL from payload or enhanced info
func (s *WebhookService) getImageURL(payload EmbyWebhookPayload, enhanced *EmbyEnhancedInfo) string {
	if enhanced != nil && enhanced.ImageURL != "" {
		return enhanced.ImageURL
	}
	return ""
}

// formatFileSizeDecimal formats file size in decimal (GB not GiB) for consistency
func (s *WebhookService) formatFileSizeDecimal(bytes int64) string {
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

// truncateCaption truncates caption to fit Telegram's 1024 character limit
func (s *WebhookService) truncateCaption(caption string) string {
	const maxCaptionLength = 1024
	if len(caption) <= maxCaptionLength {
		return caption
	}
	// Truncate and add ellipsis
	runes := []rune(caption)
	if len(runes) > maxCaptionLength {
		return string(runes[:maxCaptionLength-3]) + "..."
	}
	return caption
}

// sendNotificationWithPhoto sends notification with photo
func (s *WebhookService) sendNotificationWithPhoto(message, photoURL string) {
	if s.chatID == 0 {
		return
	}

	// Check cache using message content as key
	if s.messageCache != nil && s.messageCache.Check(s.chatID, message) {
		// 静默跳过重复通知
		return
	}

	// Truncate caption to fit Telegram's 1024 character limit
	caption := s.truncateCaption(message)
	if len(caption) < len(message) {
		log.Printf("[Webhook] Caption truncated from %d to %d characters", len(message), len(caption))
	}
	log.Printf("[Webhook] Sending photo caption (%d chars):\n%s", len(caption), caption)

	// Send photo with caption
	if _, err := s.telegram.SendPhoto(s.chatID, photoURL, caption, nil); err != nil {
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

// formatPhotoCaption formats a compact caption for photo notifications (Telegram limit: 1024 chars)
func (s *WebhookService) formatPhotoCaption(payload EmbyWebhookPayload, enhanced *EmbyEnhancedInfo) string {
	var builder strings.Builder

	// Debug logging for enhanced info
	if enhanced != nil {
		log.Printf("[Debug] formatPhotoCaption - Quality: %s, FileSize: %d bytes, FileCount: %d",
			enhanced.Quality, enhanced.FileSize, enhanced.FileCount)
	} else {
		log.Printf("[Debug] formatPhotoCaption - enhanced is nil")
	}

	// Get title
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

	// Get series name - check both top-level and nested Item
	seriesName := payload.SeriesName
	if seriesName == "" && payload.Item != nil {
		seriesName = payload.Item.SeriesName
	}

	// Build display name (for ✅ 入库成功 line)
	successName := title
	if itemType == "Episode" && seriesName != "" {
		successName = seriesName
	} else if itemType == "Season" && seriesName != "" {
		successName = seriesName
	} else if itemType == "Series" {
		successName = title
	}

	// Build titleStr (for 🎬 名称 line - with full details)
	// Get season and episode from correct fields (Item.ParentIndexNumber and Item.IndexNumber)
	var titleStr string
	if itemType == "Episode" && seriesName != "" {
		season := 0
		episode := 0

		// Try to get season/episode from Item object first (correct fields)
		if payload.Item != nil {
			if payload.Item.ParentIndexNumber != nil {
				season = *payload.Item.ParentIndexNumber
			}
			if payload.Item.IndexNumber != nil {
				episode = *payload.Item.IndexNumber
			}
		}

		// Fallback to top-level fields if Item doesn't have the data
		if season == 0 && payload.Season != 0 {
			season = payload.Season
		}
		if episode == 0 && payload.Episode != 0 {
			episode = payload.Episode
		}

		// If both season and episode are 0, this might be a series entry, not a single episode
		if season == 0 && episode == 0 {
			titleStr = seriesName  // Only show series name, don't show S00 E00
		} else {
			titleStr = fmt.Sprintf("%s S%02d E%02d", seriesName, season, episode)
		}
	} else if itemType == "Season" && seriesName != "" {
		season := 0
		// Try to get season from Item object first
		if payload.Item != nil && payload.Item.ParentIndexNumber != nil {
			season = *payload.Item.ParentIndexNumber
		}
		if season == 0 {
			season = payload.Season
		}
		if season > 0 {
			titleStr = fmt.Sprintf("%s S%02d", seriesName, season)
		} else {
			titleStr = seriesName
		}
	} else {
		titleStr = title
	}

	// Header line - only name, no year/season info
	builder.WriteString("✅ 入库成功：")
	builder.WriteString(successName)
	builder.WriteString("\n")
	builder.WriteString("───────────────────\n\n")

	// Name line
	builder.WriteString(fmt.Sprintf("🎬 名称：%s\n", titleStr))

	// Category line
	builder.WriteString(fmt.Sprintf("🏷️ 类别：%s\n", s.getDetailedCategory(itemType, enhanced)))

	// Quality line - use getFullQuality for proper WEB-DL format
	quality := ""
	if enhanced != nil && enhanced.Quality != "" {
		quality = s.getFullQuality(enhanced)
	}
	if quality != "" {
		builder.WriteString(fmt.Sprintf("💎 质量：%s\n", quality))
	}

	// File size - UNCONDITIONALLY display, no if conditions
	if enhanced != nil {
		if enhanced.FileSize > 1024*1024 {
			builder.WriteString(fmt.Sprintf("📦 总大小：%s\n", s.formatFileSizeDecimal(enhanced.FileSize)))
		} else {
			// Small file size or .strm files - always show
			builder.WriteString(fmt.Sprintf("📄 引用文件：%s\n", s.formatFileSizeDecimal(enhanced.FileSize)))
		}
	}

	// File count - UNCONDITIONALLY display, no if conditions
	if enhanced != nil {
		builder.WriteString(fmt.Sprintf("📁 文件数量：%d个", enhanced.FileCount))
	}

	return builder.String()
}

// formatEpisodePhotoCaption formats a caption for episode photo notifications
func (s *WebhookService) formatEpisodePhotoCaption(agg *EpisodeAggregation, epRange string) string {
	var builder strings.Builder

	// Build title string for 🎬 名称 line (with full details)
	var title string
	if agg.Year > 1900 && agg.Year < 2100 {
		title = fmt.Sprintf("%s (%d) S%02d %s", agg.SeriesName, agg.Year, agg.Season, epRange)
	} else {
		title = fmt.Sprintf("%s S%02d %s", agg.SeriesName, agg.Season, epRange)
	}

	// Header line - only series name, no year/season/episode info
	builder.WriteString("✅ 入库成功：")
	builder.WriteString(agg.SeriesName)
	builder.WriteString("\n")
	builder.WriteString("───────────────────\n\n")

	// Name line
	builder.WriteString(fmt.Sprintf("🎬 名称：%s\n", title))

	// Category line - use detailed category from enhanced info
	category := "剧集"
	if agg.EnhancedInfo != nil {
		category = s.getDetailedCategory("Episode", agg.EnhancedInfo)
	}
	builder.WriteString(fmt.Sprintf("🏷️ 类别：%s\n", category))

	// Quality line
	if agg.Quality != "" {
		builder.WriteString(fmt.Sprintf("💎 质量：%s\n", agg.Quality))
	}

	// File size - UNCONDITIONALLY display, no if conditions
	if agg.FileSize > 1024*1024 {
		builder.WriteString(fmt.Sprintf("📦 总大小：%s\n", s.formatFileSizeDecimal(agg.FileSize)))
	} else {
		// Small file size or .strm files - always show
		builder.WriteString(fmt.Sprintf("📄 引用文件：%s\n", s.formatFileSizeDecimal(agg.FileSize)))
	}

	// File count - UNCONDITIONALLY display, no if conditions
	builder.WriteString(fmt.Sprintf("📁 文件数量：%d个", agg.FileCount))

	return builder.String()
}

// addMediaItemToSummary adds media item to daily summary list
func (s *WebhookService) addMediaItemToSummary(payload EmbyWebhookPayload, enhanced *EmbyEnhancedInfo) {
	if s.mediaNotificationSvc == nil {
		return
	}

	// Get media type
	mediaType := MediaTypeMovie
	itemType := payload.ItemType
	if itemType == "" && payload.Item != nil {
		itemType = payload.Item.Type
	}

	// Detect media type
	if itemType == "Episode" || itemType == "Series" || itemType == "Season" {
		mediaType = MediaTypeSeries
	}

	// Check if anime
	isAnime := false
	libraryName := payload.Library
	if libraryName == "" && payload.Item != nil {
		libraryName = payload.Item.Path
	}
	if libraryName != "" {
		lowerLib := strings.ToLower(libraryName)
		animeKeywords := []string{"anim", "动画", "anime", "卡通", "漫画"}
		for _, kw := range animeKeywords {
			if strings.Contains(lowerLib, kw) {
				isAnime = true
				break
			}
		}
	}

	if isAnime {
		mediaType = MediaTypeAnime
	}

	// Get year
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

	// Get season and episode from correct fields
	season := 0
	episode := 0
	if payload.Item != nil {
		if payload.Item.ParentIndexNumber != nil {
			season = *payload.Item.ParentIndexNumber
		}
		if payload.Item.IndexNumber != nil {
			episode = *payload.Item.IndexNumber
		}
	}
	if season == 0 && payload.Season != 0 {
		season = payload.Season
	}
	if episode == 0 && payload.Episode != 0 {
		episode = payload.Episode
	}

	// Create media item
	item := &MediaItem{
		Title:         payload.ItemName,
		Year:          year,
		LibraryName:   libraryName,
		MediaType:     mediaType,
		SeriesName:    payload.SeriesName,
		SeasonNumber:  season,
		EpisodeStart:  episode,
		EpisodeEnd:    episode,
		EpisodeCount:  1,
		IsCompleted:   false,
	}

	s.mediaNotificationSvc.AddItem(item)
	log.Printf("[汇总] 已添加到每日汇总: %s", item.Title)
}

// addAggregatedEpisodeToSummary adds aggregated episode to daily summary list
func (s *WebhookService) addAggregatedEpisodeToSummary(agg *EpisodeAggregation, epRange string) {
	if s.mediaNotificationSvc == nil {
		return
	}

	// Detect media type from library name
	mediaType := MediaTypeSeries
	isAnime := false
	if agg.LibraryName != "" {
		lowerLib := strings.ToLower(agg.LibraryName)
		animeKeywords := []string{"anim", "动画", "anime", "卡通", "漫画"}
		for _, kw := range animeKeywords {
			if strings.Contains(lowerLib, kw) {
				isAnime = true
				break
			}
		}
	}

	if isAnime {
		mediaType = MediaTypeAnime
	}

	// Parse episode range to get episode count
	episodeCount := len(agg.Episodes)
	episodeStart := agg.Episodes[0]
	episodeEnd := agg.Episodes[len(agg.Episodes)-1]

	// Create media item
	item := &MediaItem{
		Title:         agg.SeriesName,
		Year:          agg.Year,
		LibraryName:   agg.LibraryName,
		MediaType:     mediaType,
		SeriesName:    agg.SeriesName,
		SeasonNumber:  agg.Season,
		EpisodeStart:  episodeStart,
		EpisodeEnd:    episodeEnd,
		EpisodeCount:  episodeCount,
		IsCompleted:   false,
	}

	s.mediaNotificationSvc.AddItem(item)
	log.Printf("[汇总] 已添加到每日汇总: %s S%02d %s", agg.SeriesName, agg.Season, epRange)
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

// SearchEmbyMedia searches for media in Emby library
func (s *WebhookService) SearchEmbyMedia(title string, year int, mediaType MediaType) (*EmbySearchResult, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return nil, fmt.Errorf("Emby URL or API key not configured")
	}

	// Build search URL with proper URL encoding
	searchParams := fmt.Sprintf("?SearchTerm=%s&IncludeItemTypes=Movie,Series&Recursive=true&Limit=10", url.QueryEscape(title))
	fullURL := fmt.Sprintf("%s/Users/%s/Items%s", s.embyURL, s.embyAPIKey, searchParams)

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Emby-Token", s.embyAPIKey)
	req.Header.Set("Accept", "application/json")

	// Skip TLS verification for self-signed/origin certificates
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: tr,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Emby API returned status %d", resp.StatusCode)
	}

	// Emby API returns an object with Items array
	var response struct {
		Items []map[string]interface{} `json:"Items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	// Find best match
	for _, item := range response.Items {
		itemTitle := ""
		if name, ok := item["Name"].(string); ok {
			itemTitle = name
		}
		itemYear := 0
		if y, ok := item["ProductionYear"].(float64); ok {
			itemYear = int(y)
		}

		// Match by title and year
		if strings.Contains(strings.ToLower(itemTitle), strings.ToLower(title)) || strings.Contains(strings.ToLower(title), strings.ToLower(itemTitle)) {
			// Check year if provided
			if year > 0 && itemYear > 0 {
				if itemYear-year >= -1 && itemYear-year <= 1 { // Allow 1 year difference
					result, err := s.convertToSearchResult(item)
					if err != nil {
						return nil, err
					}
					return result, nil
				}
			} else {
				result, err := s.convertToSearchResult(item)
				if err != nil {
					return nil, err
				}
				return result, nil
			}
		}
	}

	return nil, nil // No match found
}

// EmbySearchResult represents a search result from Emby
type EmbySearchResult struct {
	ID       string
	Title    string
	Year     int
	Type     string
	PosterURL string
	Overview string
	RunTime  int64 // in ticks
}

// convertToSearchResult converts Emby item to search result
func (s *WebhookService) convertToSearchResult(item map[string]interface{}) (*EmbySearchResult, error) {
	result := &EmbySearchResult{}

	// Extract ID
	if id, ok := item["Id"].(string); ok {
		result.ID = id
	} else {
		return nil, fmt.Errorf("missing Id field")
	}

	// Extract Title
	if name, ok := item["Name"].(string); ok {
		result.Title = name
	}

	// Extract Year
	if year, ok := item["ProductionYear"].(float64); ok {
		result.Year = int(year)
	}

	// Extract Type
	if itemType, ok := item["Type"].(string); ok {
		result.Type = itemType
	}

	// Extract Overview
	if overview, ok := item["Overview"].(string); ok {
		result.Overview = overview
	}

	// Extract Runtime
	if runtime, ok := item["RunTimeTicks"].(float64); ok {
		result.RunTime = int64(runtime)
	}

	// Extract Poster/Image
	if itemID, ok := item["Id"].(string); ok {
		if tags, ok := item["ImageTags"].([]interface{}); ok && len(tags) > 0 {
			if tag, ok := tags[0].(string); ok {
				result.PosterURL = fmt.Sprintf("%s/Items/%s/Images/Primary/%s?fillWidth=400&quality=90", s.embyURL, itemID, tag)
			}
		}
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
	// Extract title from various sources
	title := payload.ItemName
	if title == "" && payload.Item != nil {
		title = payload.Item.Name
	}

	item := &MediaItem{
		ID:          payload.ItemID,
		Title:       title,
		LibraryName: payload.Library,
		AddedAt:     time.Now(),
	}

	// Determine media type
	itemType := payload.ItemType
	if itemType == "" && payload.Item != nil {
		itemType = payload.Item.Type
	}

	// Get season and episode from correct fields
	season := 0
	episode := 0
	if payload.Item != nil {
		if payload.Item.ParentIndexNumber != nil {
			season = *payload.Item.ParentIndexNumber
		}
		if payload.Item.IndexNumber != nil {
			episode = *payload.Item.IndexNumber
		}
	}
	if season == 0 && payload.Season != 0 {
		season = payload.Season
	}
	if episode == 0 && payload.Episode != 0 {
		episode = payload.Episode
	}

	switch itemType {
	case "Movie":
		item.MediaType = MediaTypeMovie
	case "Episode":
		item.MediaType = MediaTypeSeries
		item.SeriesName = payload.SeriesName
		item.SeasonNumber = season
		item.EpisodeStart = episode
		item.EpisodeEnd = episode
		item.EpisodeCount = 1
	case "Season":
		item.MediaType = MediaTypeSeries
		item.SeriesName = payload.SeriesName
		item.SeasonNumber = season
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
		item.FileSize = enhanced.FileSize
		item.FileCount = enhanced.FileCount
		item.IsWEBDL = enhanced.IsWEBDL
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

