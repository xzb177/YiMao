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
	Id          string `json:"Id"`                 // Item ID
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
	// 文件信息缓存 - 避免频繁调用 Emby API
	fileInfoCache        map[string]*cachedFileInfo      // key: itemID
	fileInfoCacheMu      sync.RWMutex
	fileInfoCacheTTL     time.Duration                  // 缓存过期时间 (默认1小时)
}

// cachedFileInfo 缓存的文件信息
type cachedFileInfo struct {
	fileSize   int64
	fileCount  int
	cachedAt   time.Time
}

// EpisodeAggregation holds aggregated episode info
type EpisodeAggregation struct {
	SeriesName   string
	SeriesID     string                  // Series ID for fetching images
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
		fileInfoCache:      make(map[string]*cachedFileInfo),
		fileInfoCacheTTL:   1 * time.Hour,     // 缓存1小时
	}

	// 启动缓存清理协程
	go svc.cleanupFileInfoCache()

	return svc
}

// cleanupFileInfoCache 定期清理过期的文件信息缓存
func (s *WebhookService) cleanupFileInfoCache() {
	ticker := time.NewTicker(30 * time.Minute) // 每30分钟清理一次
	defer ticker.Stop()

	for range ticker.C {
		s.fileInfoCacheMu.Lock()
		now := time.Now()
		for itemID, cached := range s.fileInfoCache {
			if now.Sub(cached.cachedAt) > s.fileInfoCacheTTL {
				delete(s.fileInfoCache, itemID)
				log.Printf("[EmbyAPI] Cleaned expired cache for %s", itemID)
			}
		}
		s.fileInfoCacheMu.Unlock()
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

	// 彻底切断单集直发通道：所有剧集类型必须进入聚合队列
	if itemType == "Episode" {
		// 如果没有SeriesName，尝试从Item中获取
		seriesName := payload.SeriesName
		if seriesName == "" && payload.Item != nil {
			seriesName = payload.Item.SeriesName
		}
		// 强制进入聚合，即使SeriesName为空也聚合
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
		enhancedPayload, err = s.getEmbyEnhancedInfo(payload.getItemID())
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
			format := s.parseReleaseFormat(path)
			isWEBDL := s.detectWEBDL(path)
			log.Printf("[Webhook] Parsed quality from webhook path: %s, format: %s (WEB-DL: %v)", quality, format, isWEBDL)

			if enhancedPayload == nil {
				// Create minimal enhanced info from webhook payload
				enhancedPayload = &EmbyEnhancedInfo{
					Quality: quality,
					Format:   format,
					IsWEBDL:  isWEBDL,
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
				// Try lowercase "tmdb" first
				var tmdbID string
				if tid, ok := payload.Item.ProviderIds["tmdb"]; ok && tid != "" {
					tmdbID = tid
				} else if tid, ok := payload.Item.ProviderIds["Tmdb"]; ok && tid != "" {
					tmdbID = tid
				} else if tid, ok := payload.Item.ProviderIds["Tvdb"]; ok && tid != "" {
					tmdbID = tid
				}
				if tmdbID != "" {
					// 【关键】根据 itemType 映射到 TMDB mediaType
					mediaType := "movie" // 默认为 movie
					if itemType == "Series" || itemType == "Episode" {
						mediaType = "tv"
					}
					log.Printf("[Webhook] Getting TMDB backdrop from webhook ProviderIds: %s (mediaType: %s)", tmdbID, mediaType)
					if backdropURL := s.getTMDBBackdrop(tmdbID, mediaType); backdropURL != "" {
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
						enhancedPayload.Format = s.parseReleaseFormat(ms.Path)
						enhancedPayload.IsWEBDL = s.detectWEBDL(ms.Path)
						log.Printf("[Webhook] Parsed quality from MediaSource path: %s, format: %s", enhancedPayload.Quality, enhancedPayload.Format)
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

	// 【黑科技】增强文件信息获取：如果文件大小或数量缺失，直接调用 Emby API
	if enhancedPayload != nil && (enhancedPayload.FileSize == 0 || enhancedPayload.FileCount == 0) {
		if itemID := payload.getItemID(); itemID != "" {
			if apiSize, apiCount, err := s.fetchMediaSourcesFromEmby(itemID); err == nil {
				if apiSize > 0 && enhancedPayload.FileSize == 0 {
					enhancedPayload.FileSize = apiSize
					log.Printf("[Webhook] Enhanced file size from Emby API: %d bytes", apiSize)
				}
				if apiCount > 0 && enhancedPayload.FileCount == 0 {
					enhancedPayload.FileCount = apiCount
					log.Printf("[Webhook] Enhanced file count from Emby API: %d", apiCount)
				}
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
			s.sendNotificationWithPhoto(photoCaption, enhancedPayload.ImageURL, enhancedPayload)
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

	// Get series name with fallback: payload.SeriesName -> Item.SeriesName -> Item.Name
	seriesName := payload.SeriesName
	if seriesName == "" && payload.Item != nil {
		seriesName = payload.Item.SeriesName
	}
	if seriesName == "" && payload.Item != nil {
		seriesName = payload.Item.Name
	}

	if seriesName == "" {
		log.Printf("[入库聚合] 无法获取剧集名称，跳过")
		return nil
	}

	// Create aggregation key: seriesName_season
	aggregationKey := fmt.Sprintf("%s_S%02d", seriesName, season)

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
				payload.getItemID(),
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
				format := s.parseReleaseFormat(path)
				isWEBDL := s.detectWEBDL(path)
				log.Printf("[Webhook] Parsed quality from webhook path (episode): %s, format: %s (WEB-DL: %v)", quality, format, isWEBDL)

				if enhancedInfo == nil {
					// Create minimal enhanced info from webhook payload
					enhancedInfo = &EmbyEnhancedInfo{
						Quality: quality,
						Format:   format,
						IsWEBDL:  isWEBDL,
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
					// 复制文件大小和数量
					for _, ms := range payload.Item.MediaSources {
						enhancedInfo.FileSize = ms.Size
						enhancedInfo.FileCount = 1
						log.Printf("[Webhook] Copied file size from MediaSources: %d bytes", ms.Size)
						break
					}
				} else {
					// 更新现有 enhancedInfo 的质量信息
					enhancedInfo.Quality = quality
					enhancedInfo.IsWEBDL = isWEBDL
				}

				// Try to get image from TMDB using ProviderIds
				if enhancedInfo.ImageURL == "" && payload.Item.ProviderIds != nil {
					// Try lowercase "tmdb" first, then "Tmdb", then "Tvdb"
					var tmdbID string
					if tid, ok := payload.Item.ProviderIds["tmdb"]; ok && tid != "" {
						tmdbID = tid
					} else if tid, ok := payload.Item.ProviderIds["Tmdb"]; ok && tid != "" {
						tmdbID = tid
					} else if tid, ok := payload.Item.ProviderIds["Tvdb"]; ok && tid != "" {
						tmdbID = tid
					}
					if tmdbID != "" {
						log.Printf("[Webhook] Getting TMDB backdrop from webhook ProviderIds (episode): %s", tmdbID)
						// 【关键】Episode 必须使用 "tv"
						if backdropURL := s.getTMDBBackdrop(tmdbID, "tv"); backdropURL != "" {
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
			SeriesName:   seriesName,  // 使用已处理的 seriesName (带 fallback)
			SeriesID:     seriesID,    // 保存 SeriesID 用于后续获取图片
			Year:         year,
			Season:       season,
			Episodes:     []int{},
			FirstAdded:   time.Now(),
			EnhancedInfo: enhancedInfo,
			LibraryName:  payload.Library,
			FileSize:     0,  // 初始化为0，后续通过累加获取
		}
		if enhancedInfo != nil {
			agg.ImageURL = enhancedInfo.ImageURL
			agg.Quality = enhancedInfo.Quality
			// FileSize 初始化为0，后续在添加集数时累加，避免重复计算
			if enhancedInfo.IsWEBDL && enhancedInfo.Quality != "" {
				agg.Quality = fmt.Sprintf("WEB-DL %s", enhancedInfo.Quality)
			}
		}
		s.epAggregation[aggregationKey] = agg

		// Create independent timer for this aggregation key
		key := aggregationKey // Capture for closure
		agg.timer = time.AfterFunc(s.aggregationDelay, func() {
			s.flushSingleAggregation(key)
		})

		log.Printf("[入库聚合] 创建新聚合: %s S%02d, 延迟 %v 后发送", seriesName, season, s.aggregationDelay)
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
		log.Printf("[Debug] Payload.Item has %d MediaSources", len(payload.Item.MediaSources))
		for i, ms := range payload.Item.MediaSources {
			log.Printf("[Debug] MediaSource[%d]: Size=%d, Path=%s", i, ms.Size, ms.Path)
			// 即使 Size 为 0 或很小（如 strm 文件），也要记录
			thisFileSize = ms.Size
			thisFileCount = 1
			break  // 使用第一个 MediaSource
		}

		// 【黑科技】如果文件大小为 0 或很小（strm 文件），调用 Emby API 获取真实大小
		if thisFileSize == 0 {
			if itemID := payload.getItemID(); itemID != "" {
				if apiSize, apiCount, err := s.fetchMediaSourcesFromEmby(itemID); err == nil && apiSize > 0 {
					thisFileSize = apiSize
					thisFileCount = apiCount
					log.Printf("[入库聚合] 从 Emby API 获取到真实文件大小: %d bytes, %d files", apiSize, apiCount)
				}
			}
		}
	}
	log.Printf("[Debug] Episode file size: %d bytes, count: %d", thisFileSize, thisFileCount)

	if !alreadyAdded {
		agg.Episodes = append(agg.Episodes, episode)
		agg.FileSize += thisFileSize
		// FileCount 不再累加，将在发送时使用 len(Episodes) 计算

		// Reset timer when new episode arrives (debounce)
		if agg.timer != nil {
			agg.timer.Stop()
		}
		key := aggregationKey // Capture for closure
		agg.timer = time.AfterFunc(s.aggregationDelay, func() {
			s.flushSingleAggregation(key)
		})

		log.Printf("[入库聚合] 添加集数: %s S%02dE%02d (当前共%d集, 总大小:%s), 重置定时器 %v",
			seriesName, season, episode, len(agg.Episodes),
			s.formatFileSizeDecimal(agg.FileSize), s.aggregationDelay)
	} else {
		log.Printf("[入库聚合] 集数已存在: %s S%02dE%02d, 跳过", seriesName, season, episode)
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
	// Fallback: if seriesName is empty, try to get from EnhancedInfo
	if seriesName == "" && agg.EnhancedInfo != nil && agg.EnhancedInfo.Title != "" {
		seriesName = agg.EnhancedInfo.Title
	}
	season := agg.Season
	episodes := make([]int, len(agg.Episodes))
	copy(episodes, agg.Episodes)
	year := agg.Year
	quality := agg.Quality
	imageURL := agg.ImageURL
	fileSize := agg.FileSize
	enhancedInfo := agg.EnhancedInfo
	libraryName := agg.LibraryName

	// Remove from map
	delete(s.epAggregation, key)

	agg.mu.Unlock()
	s.epAggregationMu.Unlock()

	if len(episodes) == 0 {
		return
	}

	// Final safety check: if still no series name, log and skip
	if seriesName == "" {
		log.Printf("[入库聚合] 错误：SeriesName 为空，跳过发送通知，key=%s", key)
		return
	}

	// Sort episodes
	sort.Ints(episodes)

	// 图片获取：TMDB 为主
	// 【优先级1】从 TMDB 获取 backdrop（横屏图片）
	if imageURL == "" && enhancedInfo != nil && enhancedInfo.TMDBID != "" {
		// 【关键】剧集聚合必须使用 "tv"
		if backdropURL := s.getTMDBBackdrop(enhancedInfo.TMDBID, "tv"); backdropURL != "" {
			imageURL = backdropURL
			log.Printf("[入库聚合] 从 TMDB 获取到 backdrop: %s", imageURL)
		}
	}

	// Build episode range string
	epRange := buildEpisodeRangeString(episodes)

	// Send notification to admins based on their individual settings
	s.sendAggregatedEpisodeToAdmins(seriesName, year, season, episodes, epRange, quality, imageURL, fileSize, enhancedInfo, libraryName)

	log.Printf("[入库] %s S%02d %s (%d集, 总大小:%s)", seriesName, season, epRange, len(episodes), s.formatFileSizeDecimal(fileSize))

	// Add to daily summary list
	s.addAggregatedEpisodeToSummary(&EpisodeAggregation{
		SeriesName:   seriesName,
		Year:         year,
		Season:       season,
		Episodes:     episodes,
		Quality:      quality,
		FileSize:     fileSize,
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
		// 图片优先级：Emby（代理上传）→ TMDB（备胎）
		if s.chatID != 0 && s.chatID < -100 {
			photoURL := ""

			// 【优先级1】先尝试使用已有的图片
			if agg.ImageURL != "" {
				photoURL = agg.ImageURL
				log.Printf("[入库聚合] 使用已有图片: %s", photoURL)
			}

			// 【优先级2】如果没有，从 TMDB 获取 backdrop
			if photoURL == "" && agg.EnhancedInfo != nil && agg.EnhancedInfo.TMDBID != "" {
				// 【关键】剧集聚合必须使用 "tv"
				if backdropURL := s.getTMDBBackdrop(agg.EnhancedInfo.TMDBID, "tv"); backdropURL != "" {
					photoURL = backdropURL
					log.Printf("[入库聚合] 从 TMDB 获取到 backdrop: %s", photoURL)
				}
			}

			// 使用现有的多行排版文本作为 caption 发送图片
			if photoURL != "" {
				s.sendNotificationWithPhoto(message, photoURL, agg.EnhancedInfo)
			} else {
				// 实在获取不到图片，回退到纯文本
				log.Printf("[入库聚合] 无法获取图片，使用纯文本发送")
				s.sendWithCache(s.chatID, message)
			}
		}

		log.Printf("[入库] %s S%02d %s (%d集)", agg.SeriesName, agg.Season, epRange, len(agg.Episodes))

		// Add to daily summary list
		s.addAggregatedEpisodeToSummary(agg, epRange)

		delete(s.epAggregation, key)
	}
}

// sendAggregatedEpisodeToAdmins sends aggregated episode notification to group chat only
// 入库通知只推送到群组，不推送给管理员个人
func (s *WebhookService) sendAggregatedEpisodeToAdmins(seriesName string, year int, season int, episodes []int, epRange string, quality, imageURL string, fileSize int64, enhancedInfo *EmbyEnhancedInfo, libraryName string) {
	// 只发送到群组，不发送给管理员个人
	if s.chatID != 0 && s.chatID < -100 {
		message := s.formatAggregatedEpisodeMessage(&EpisodeAggregation{
			SeriesName:   seriesName,
			Year:         year,
			Season:       season,
			Episodes:     episodes,
			Quality:      quality,
			ImageURL:     imageURL,
			FileSize:     fileSize,
			EnhancedInfo: enhancedInfo,
			LibraryName:  libraryName,
		}, epRange)

		if imageURL != "" {
			photoCaption := s.formatEpisodePhotoCaption(&EpisodeAggregation{
				SeriesName:   seriesName,
				Year:         year,
				Season:       season,
				Episodes:     episodes,
				Quality:      quality,
				ImageURL:     imageURL,
				FileSize:     fileSize,
				EnhancedInfo: enhancedInfo,
				LibraryName:  libraryName,
			}, epRange)
			s.sendNotificationWithPhoto(photoCaption, imageURL, enhancedInfo)
		} else {
			s.sendWithCache(s.chatID, message)
		}
	}
}

// sendNotificationWithPhotoToAdmin sends a photo notification to a specific admin
func (s *WebhookService) sendNotificationWithPhotoToAdmin(adminID int64, caption string, imageURL string, enhancedInfo *EmbyEnhancedInfo) {
	// Try to send as photo using Telegram's built-in URL download
	_, err := s.telegram.SendPhotoFromURL(adminID, imageURL, caption, nil)
	if err != nil {
		log.Printf("[入库] Failed to send photo to admin %d: %v, falling back to text", adminID, err)
		// Fallback to text-only message
		s.telegram.SendMessage(adminID, caption, "", nil)
	}
}

// buildEpisodeRangeString builds episode range string with forced head-tail merge
// 单集显示 "E02"，多集强制首尾合并显示 "E02-E18"（忽略中间断层）
func buildEpisodeRangeString(episodes []int) string {
	if len(episodes) == 0 {
		return ""
	}
	if len(episodes) == 1 {
		return fmt.Sprintf("E%02d", episodes[0])
	}

	// 多集：强制使用最小集数-最大集数，忽略中间是否连续
	return fmt.Sprintf("E%02d-E%02d", episodes[0], episodes[len(episodes)-1])
}

// formatAggregatedEpisodeMessage formats aggregated episode notification (极简呼吸感排版)
func (s *WebhookService) formatAggregatedEpisodeMessage(agg *EpisodeAggregation, epRange string) string {
	var builder strings.Builder

	// Build title with year, season and episode range (用于顶部"✅ 入库成功"行)
	var title string
	if agg.Year > 1900 && agg.Year < 2100 {
		title = fmt.Sprintf("%s (%d) S%02d %s", agg.SeriesName, agg.Year, agg.Season, epRange)
	} else {
		title = fmt.Sprintf("%s S%02d %s", agg.SeriesName, agg.Season, epRange)
	}

	// Build name only (年份+剧集名，不包含季集数，用于"🎬 名称"行)
	var nameOnly string
	if agg.Year > 1900 && agg.Year < 2100 {
		nameOnly = fmt.Sprintf("%s (%d)", agg.SeriesName, agg.Year)
	} else {
		nameOnly = agg.SeriesName
	}

	// Header line - complete title with year/season/episode
	builder.WriteString("✅ 入库成功：")
	builder.WriteString(title)
	builder.WriteString("\n")
	builder.WriteString("──────\n")

	// Name line - only series name (with year), no season/episode
	builder.WriteString("\n")
	builder.WriteString("🎬 名称：")
	builder.WriteString(nameOnly)
	builder.WriteString("\n")

	// Category line
	category := "剧集"
	if agg.EnhancedInfo != nil {
		category = s.getDetailedCategory("Episode", agg.EnhancedInfo)
	}
	builder.WriteString("\n")
	builder.WriteString("🏷️ 类别：")
	builder.WriteString(category)
	builder.WriteString("\n")

	// Quality line
	if agg.Quality != "" {
		builder.WriteString("\n")
		builder.WriteString("💎 质量：")
		builder.WriteString(agg.Quality)
		builder.WriteString("\n")
	}

	// File size line - 只在有实际大小时显示
	if agg.FileSize > 0 {
		builder.WriteString("\n")
		builder.WriteString("📦 总大小：")
		builder.WriteString(s.formatFileSizeDecimal(agg.FileSize))
		builder.WriteString("\n")
	}

	// File count line - 使用 Episodes 列表长度而非 FileCount
	fileCount := len(agg.Episodes)
	builder.WriteString("\n")
	builder.WriteString("📁 文件数量：")
	builder.WriteString(fmt.Sprintf("%d", fileCount))
	builder.WriteString(" 个")

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

	// File size (using decimal GB, show "未知" if 0)
	if agg.FileSize > 0 {
		if agg.Quality != "" {
			builder.WriteString(" · ")
		}
		builder.WriteString(fmt.Sprintf("📦 %s", s.formatFileSizeDecimal(agg.FileSize)))
	} else {
		if agg.Quality != "" {
			builder.WriteString(" · ")
		}
		builder.WriteString("📦 未知")
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
	Quality      string    // Resolution (1080p, 2160p, etc.)
	Format       string    // Release format (BluRay, WEB-DL, WEBRip, HDTV, etc.)
	FileSize     int64
	FileCount    int
	IsWEBDL      bool      // Deprecated: kept for compatibility, use Format instead
	Container    string    // Container format (mkv, mp4, etc.)
	TMDBID       string    // TMDB ID for fetching images
	Type         string    // Item type (Movie, Series, Episode) for TMDB API
}

// getEmbyEnhancedInfoForEpisode fetches enhanced information from Emby API for episodes
// It queries both the episode and its parent series to get genres and backdrop
// 图片优先级：Emby（代理上传） → TMDB（备胎）
func (s *WebhookService) getEmbyEnhancedInfoForEpisode(itemID string, seriesID string, parentBackdropItemID string, parentBackdropImageTags []string, seriesPrimaryImageTag string) (*EmbyEnhancedInfo, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return nil, fmt.Errorf("Emby not configured")
	}

	info := &EmbyEnhancedInfo{}
	var seriesInfo *EmbyEnhancedInfo

	// First, query the Series to get Genres (episodes don't have genres)
	if seriesID != "" {
		var err error
		seriesInfo, err = s.getSeriesInfo(seriesID)
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

	// 【优先级1】从 TMDB 获取 backdrop（横屏图片）
	if info.ImageURL == "" && info.TMDBID != "" {
		// 【关键】Episode 必须使用 "tv"
		if backdropURL := s.getTMDBBackdrop(info.TMDBID, "tv"); backdropURL != "" {
			info.ImageURL = backdropURL
			log.Printf("[Debug] Using TMDB backdrop: %s", backdropURL)
		}
	}

	return info, nil
}

// getSeriesInfo fetches series information from Emby API (including backdrop image)
func (s *WebhookService) getSeriesInfo(seriesID string) (*EmbyEnhancedInfo, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return nil, fmt.Errorf("Emby not configured")
	}

	// 请求包含 BackdropImageTags 以获取横幅图
	url := fmt.Sprintf("%s/Users/%s/Items/%s?Fields=Genres,ProviderIds,Overview,ProductionYear,CommunityRating,BackdropImageTags,ImageTags",
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

	// Extract TMDB ID - try both lowercase and uppercase variants (Emby uses Tvdb/Tmdb)
	if providerIds, ok := result["ProviderIds"].(map[string]interface{}); ok {
		// Try lowercase "tmdb" first
		if tid, ok := providerIds["tmdb"].(string); ok && tid != "" {
			info.TMDBID = tid
			log.Printf("[Debug] Found TMDB ID from tmdb: %s", tid)
		} else if tid, ok := providerIds["Tmdb"].(string); ok && tid != "" {
			// Try capitalized "Tmdb"
			info.TMDBID = tid
			log.Printf("[Debug] Found TMDB ID from Tmdb: %s", tid)
		} else if tid, ok := providerIds["Tvdb"].(string); ok && tid != "" {
			// Use Tvdb as fallback (TMDB API can sometimes use TVDB ID)
			info.TMDBID = tid
			log.Printf("[Debug] Using Tvdb ID as TMDB ID: %s", tid)
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

	// 【关键】提取 Type 字段以确定 TMDB mediaType
	itemType := ""
	if typeVal, ok := result["Type"].(string); ok {
		itemType = typeVal
		info.Type = itemType // 保存到结构体中供后续使用
		log.Printf("[Debug] Item Type: %s", itemType)
	}

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

	// Extract image URL - 优先级：Emby（代理上传） → TMDB（备胎）
	// Check for TMDB ID in ProviderIds (Emby uses Tvdb/Tmdb) - 作为备胎使用
	var tmdbID string
	if providerIds, ok := result["ProviderIds"].(map[string]interface{}); ok {
		// Try lowercase "tmdb" first
		if tid, ok := providerIds["tmdb"].(string); ok && tid != "" {
			tmdbID = tid
		} else if tid, ok := providerIds["Tmdb"].(string); ok && tid != "" {
			// Try capitalized "Tmdb"
			tmdbID = tid
		} else if tid, ok := providerIds["Tvdb"].(string); ok && tid != "" {
			// Use Tvdb as fallback (TMDB API can sometimes use TVDB ID)
			tmdbID = tid
		}
	}
	info.TMDBID = tmdbID // 保存 TMDB ID
	log.Printf("[Debug] TMDB ID: %s", tmdbID)

	// 【优先级1】从 TMDB 获取 backdrop（横屏图片）
	if info.ImageURL == "" && tmdbID != "" {
		// 【关键】根据 itemType 映射到 TMDB mediaType
		mediaType := "movie" // 默认
		if itemType == "Series" || itemType == "Episode" {
			mediaType = "tv"
		}
		if backdropURL := s.getTMDBBackdrop(tmdbID, mediaType); backdropURL != "" {
			info.ImageURL = backdropURL
			log.Printf("[Debug] Using TMDB backdrop: %s", backdropURL)
		} else {
			log.Printf("[Debug] TMDB backdrop not found for ID: %s", tmdbID)
		}
	}

	if info.ImageURL != "" {
		log.Printf("[Debug] Final ImageURL: %s", info.ImageURL)
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

				// Detect WEB-DL and Format from path or name
				if path, ok := source["Path"].(string); ok {
					info.IsWEBDL = s.detectWEBDL(path)
					info.Format = s.parseReleaseFormat(path)
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
// 【关键修复】必须传入 mediaType 避免跨库撞车！
// mediaType: "movie" 或 "tv" (剧集/动漫统一使用 tv)
func (s *WebhookService) getTMDBBackdrop(tmdbID string, mediaType string) string {
	apiKey := s.tmdbAPIKey
	if apiKey == "" {
		apiKey = "a62307d3a16cd0a605de3857d9ed614e" // fallback default key
	}

	client := &http.Client{Timeout: 10 * time.Second}

	// 【关键】使用 /images 端点，根据 mediaType 动态拼接路径
	// 【关键】添加 include_image_language=zh,null 优先中文图片
	url := fmt.Sprintf("https://api.themoviedb.org/3/%s/%s/images?api_key=%s&include_image_language=zh,null", mediaType, tmdbID, apiKey)

	log.Printf("[TMDB] Fetching images from %s", url)
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("[TMDB] API request failed for %s ID %s: %v", mediaType, tmdbID, err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[TMDB] API returned status %d for %s ID %s", resp.StatusCode, mediaType, tmdbID)
		return ""
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[TMDB] Failed to decode response for %s ID %s: %v", mediaType, tmdbID, err)
		return ""
	}

	// 【绝对锁定】只读取 backdrops 数组，绝不读取 posters！
	if backdrops, ok := result["backdrops"].([]interface{}); ok && len(backdrops) > 0 {
		if firstBackdrop, ok := backdrops[0].(map[string]interface{}); ok {
			if filePath, ok := firstBackdrop["file_path"].(string); ok && filePath != "" {
				url := fmt.Sprintf("https://image.tmdb.org/t/p/original%s", filePath)
				log.Printf("[TMDB] Got backdrop for %s ID %s: %s", mediaType, tmdbID, filePath)
				return url
			}
		}
	}

	// 【严禁】没有 backdrop 就返回空，宁可让 Telegram 走纯文本 fallback，也不发难看的竖屏海报！
	log.Printf("[TMDB] No backdrop found for %s ID %s, returning empty (will use text fallback)", mediaType, tmdbID)
	return ""
}

// getTMDBPoster fetches poster URL from TMDB API (竖版海报)
// 添加中文语言参数以获取中文海报
func (s *WebhookService) getTMDBPoster(tmdbID string) string {
	apiKey := s.tmdbAPIKey
	if apiKey == "" {
		apiKey = "a62307d3a16cd0a605de3857d9ed614e" // fallback default key
	}

	client := &http.Client{Timeout: 5 * time.Second}

	// 添加中文语言参数，优先获取中文海报
	// language=zh-CN: 返回中文内容
	// include_image_language=zh,null: 优先中文图片，无中文时返回默认图片
	url := fmt.Sprintf("https://api.themoviedb.org/3/movie/%s?api_key=%s&language=zh-CN&include_image_language=zh,null", tmdbID, apiKey)
	resp, err := client.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		// Try TV API with same language parameters
		tvURL := fmt.Sprintf("https://api.themoviedb.org/3/tv/%s?api_key=%s&language=zh-CN&include_image_language=zh,null", tmdbID, apiKey)
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

// detectQuality detects video quality from width (从MediaSources的Width字段获取)
func (s *WebhookService) detectQuality(width int) string {
	switch {
	case width >= 3800:
		return "2160p"  // 4K统一显示为2160p
	case width >= 1900:
		return "1080p"
	case width >= 1200:
		return "720p"
	default:
		return ""  // 无法确定时返回空字符串，严禁伪造数据
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

	// 无法解析时返回空字符串，严禁伪造数据
	log.Printf("[Quality] Could not parse quality from path: %s", path)
	return ""
}

// parseReleaseFormat parses release format from file path
// 支持格式: BluRay, WEB-DL, WEBRip, HDTV, DVDRip, HDRip, REMUX, etc.
func (s *WebhookService) parseReleaseFormat(path string) string {
	if path == "" {
		return ""
	}

	pathLower := strings.ToLower(path)
	log.Printf("[Format] Parsing format from path: %s", path)

	// 格式检测顺序很重要，更具体的格式应该优先检查

	// REMUX / BluRay.REMUX
	if strings.Contains(pathLower, "remux") {
		return "BluRay.REMUX"
	}

	// WEB-DL (check before web-dl to avoid false positive)
	if strings.Contains(pathLower, "web-dl") || strings.Contains(pathLower, "webdl") {
		return "WEB-DL"
	}

	// WEBRip
	if strings.Contains(pathLower, "webrip") || strings.Contains(pathLower, "web.rip") {
		return "WEBRip"
	}

	// BluRay / BRrip / BDRip / BD
	if strings.Contains(pathLower, "blu-ray") || strings.Contains(pathLower, "bluray") || strings.Contains(pathLower, "brrip") || strings.Contains(pathLower, "bdrip") {
		return "BluRay"
	}

	// HDTV
	if strings.Contains(pathLower, "hdtv") {
		return "HDTV"
	}

	// DVDRip
	if strings.Contains(pathLower, "dvdrip") {
		return "DVDRip"
	}

	// HDRip
	if strings.Contains(pathLower, "hdrip") {
		return "HDRip"
	}

	// WEB (generic web source)
	if strings.Contains(pathLower, "web.") || strings.Contains(pathLower, "[web]") {
		return "WEB"
	}

	log.Printf("[Format] No known format found in path")
	return ""
}

// getFullQuality returns full quality string including format info
// 支持格式: "BluRay 1080p", "WEB-DL 2160p", "HDTV 720p", "BluRay.REMUX 2160p" 等
func (s *WebhookService) getFullQuality(info *EmbyEnhancedInfo) string {
	if info == nil {
		return ""
	}

	quality := info.Quality
	if quality == "" {
		return ""
	}

	// 优先使用新的 Format 字段
	if info.Format != "" {
		return fmt.Sprintf("%s %s", info.Format, quality)
	}

	// 兼容旧代码：如果没有 Format 但有 IsWEBDL 标记
	if info.IsWEBDL {
		return fmt.Sprintf("WEB-DL %s", quality)
	}

	return quality
}

// inferFileCount 从路径中智能推断文件数量
// 支持检测：CD1/CD2, Part1/Part2, Disc1/Disc2, x264/x265 多文件等
func (s *WebhookService) inferFileCount(path string) int {
	if path == "" {
		return 1
	}

	pathLower := strings.ToLower(path)
	count := 1

	// 检测多CD/Part/Disc标记
	multiFilePatterns := []struct {
		pattern string
		multiplier int
	}{
		{"cd1", 2}, {"cd2", 2}, {"cd3", 3}, {"cd4", 4},
		{"part1", 2}, {"part2", 2}, {"part3", 3},
		{"disc1", 2}, {"disc2", 2}, {"disc3", 3},
		{"disk1", 2}, {"disk2", 2},
	}

	for _, p := range multiFilePatterns {
		if strings.Contains(pathLower, p.pattern) {
			if p.multiplier > count {
				count = p.multiplier
			}
		}
	}

	// 检测双音轨/双语言标记 (通常表示双版本)
	if strings.Contains(pathLower, "dual") || strings.Contains(pathLower, "diy") {
		// 不增加文件数，但可以标记
	}

	return count
}

// fetchMediaSourcesFromEmby 直接从 Emby API 获取完整的 MediaSources 信息
// 带缓存机制，避免频繁调用对 Emby 服务器造成负担
func (s *WebhookService) fetchMediaSourcesFromEmby(itemID string) (fileSize int64, fileCount int, err error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return 0, 0, fmt.Errorf("Emby URL or API key not configured")
	}

	// 【缓存检查】先查看缓存
	s.fileInfoCacheMu.RLock()
	if cached, exists := s.fileInfoCache[itemID]; exists {
		// 检查缓存是否过期
		if time.Since(cached.cachedAt) < s.fileInfoCacheTTL {
			s.fileInfoCacheMu.RUnlock()
			log.Printf("[EmbyAPI] Cache hit for %s: size=%d, files=%d", itemID, cached.fileSize, cached.fileCount)
			return cached.fileSize, cached.fileCount, nil
		}
	}
	s.fileInfoCacheMu.RUnlock()

	// 【API调用】缓存未命中，调用 Emby API
	log.Printf("[EmbyAPI] Cache miss, fetching from API for %s", itemID)

	// 调用 Emby API 获取完整的 Items 信息
	url := fmt.Sprintf("%s/Users/%s/Items/%s", s.embyURL, "e56c0bc56c984ba6a95c67222d5c69f1", itemID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, 0, err
	}

	req.Header.Set("X-Emby-Token", s.embyAPIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("Emby API returned status %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, 0, err
	}

	// 提取 MediaSources 信息
	totalSize := int64(0)
	totalFiles := 0

	if mediaSources, ok := result["MediaSources"].([]interface{}); ok {
		for _, ms := range mediaSources {
			if source, ok := ms.(map[string]interface{}); ok {
				// 获取文件大小
				if size, ok := source["Size"].(float64); ok {
					totalSize += int64(size)
				}

				// 获取路径用于推断文件数量
				if path, ok := source["Path"].(string); ok {
					totalFiles += s.inferFileCount(path)
				}
			}
		}
	}

	log.Printf("[EmbyAPI] Fetched media info for %s: size=%d, files=%d", itemID, totalSize, totalFiles)

	// 【缓存存储】将结果存入缓存
	s.fileInfoCacheMu.Lock()
	s.fileInfoCache[itemID] = &cachedFileInfo{
		fileSize:  totalSize,
		fileCount: totalFiles,
		cachedAt:  time.Now(),
	}
	s.fileInfoCacheMu.Unlock()

	return totalSize, totalFiles, nil
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

// getItemID gets the ItemID from payload with fallback to Item.Id
func (p *EmbyWebhookPayload) getItemID() string {
	if p.ItemID != "" {
		return p.ItemID
	}
	if p.Item != nil && p.Item.Id != "" {
		return p.Item.Id
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
// enhancedInfo is used for TMDB fallback when Emby image download fails
func (s *WebhookService) sendNotificationWithPhoto(message, photoURL string, enhancedInfo *EmbyEnhancedInfo) {
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

	var err error
	// 【图片发送策略】所有图片都使用代理上传，确保稳定性
	// 原因：Telegram 有时无法直接获取 TMDB 图片（failed to get HTTP URL content）
	if strings.Contains(photoURL, s.embyURL) && s.embyAPIKey != "" {
		// Emby 图片 - 使用认证下载
		headers := map[string]string{
			"X-Emby-Token": s.embyAPIKey,
		}
		_, err = s.telegram.SendPhotoWithAuth(s.chatID, photoURL, caption, headers, nil)
	} else if strings.Contains(photoURL, "tmdb.org") || strings.Contains(photoURL, "themoviedb.org") {
		// 【重要】TMDB 图片也使用代理上传，避免 Telegram 无法获取
		// 不需要认证 headers，但使用相同的下载上传机制
		_, err = s.telegram.SendPhotoWithAuth(s.chatID, photoURL, caption, nil, nil)
	} else {
		// 其他图片源使用常规方法
		_, err = s.telegram.SendPhoto(s.chatID, photoURL, caption, nil)
	}

	if err != nil {
		log.Printf("[Webhook] Failed to send photo: %v", err)

		// 【重要】Emby 图片失败时，尝试 TMDB fallback
		if strings.Contains(photoURL, s.embyURL) && enhancedInfo != nil && enhancedInfo.TMDBID != "" {
			log.Printf("[Webhook] Emby image failed, trying TMDB fallback with ID: %s", enhancedInfo.TMDBID)
			// 【关键】根据 enhancedInfo.Type 映射到 TMDB mediaType
			mediaType := "movie" // 默认
			if enhancedInfo.Type == "Series" || enhancedInfo.Type == "Episode" {
				mediaType = "tv"
			}
			if backdropURL := s.getTMDBBackdrop(enhancedInfo.TMDBID, mediaType); backdropURL != "" {
				log.Printf("[Webhook] Using TMDB backdrop (代理上传): %s", backdropURL)
				// 【关键】TMDB 备胎也使用代理上传
				_, tmdbErr := s.telegram.SendPhotoWithAuth(s.chatID, backdropURL, caption, nil, nil)
				if tmdbErr == nil {
					// TMDB 成功，添加到缓存并返回
					if s.messageCache != nil {
						s.messageCache.Add(s.chatID, message)
					}
					return
				}
				log.Printf("[Webhook] TMDB fallback also failed: %v", tmdbErr)
			} else {
				log.Printf("[Webhook] TMDB backdrop is empty for ID: %s", enhancedInfo.TMDBID)
			}
		}

		// 最后回退到纯文本消息
		log.Printf("[Webhook] All image attempts failed, falling back to text message")
		s.sendWithCache(s.chatID, message)
		return
	}

	// Add to cache
	if s.messageCache != nil {
		s.messageCache.Add(s.chatID, message)
	}
}

// formatPhotoCaption formats a compact caption for photo notifications (Telegram limit: 1024 chars)
// 【更新】统一使用极简呼吸感排版，与 formatEpisodePhotoCaption 保持一致
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

	// Get year for nameOnly display
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

	// Build nameOnly (年份+名称，用于"🎬 名称"行)
	var nameOnly string
	if year > 1900 && year < 2100 {
		nameOnly = fmt.Sprintf("%s (%d)", title, year)
	} else {
		nameOnly = title
	}

	// Header line
	builder.WriteString("✅ 入库成功：")
	builder.WriteString(title)
	builder.WriteString("\n")
	builder.WriteString("──────\n")

	// Name line
	builder.WriteString("\n")
	builder.WriteString("🎬 名称：")
	builder.WriteString(nameOnly)
	builder.WriteString("\n")

	// Category line
	builder.WriteString("\n")
	builder.WriteString("🏷️ 类别：")
	builder.WriteString(s.getDetailedCategory(itemType, enhanced))
	builder.WriteString("\n")

	// Quality line
	quality := ""
	if enhanced != nil && enhanced.Quality != "" {
		quality = enhanced.Quality
	}
	if quality != "" {
		builder.WriteString("\n")
		builder.WriteString("💎 质量：")
		builder.WriteString(quality)
		builder.WriteString("\n")
	}

	// File size line - 只在有实际大小时显示
	if enhanced != nil && enhanced.FileSize > 0 {
		builder.WriteString("\n")
		builder.WriteString("📦 总大小：")
		builder.WriteString(s.formatFileSizeDecimal(enhanced.FileSize))
		builder.WriteString("\n")
	}

	// File count line - 只在有实际数量时显示
	if enhanced != nil && enhanced.FileCount > 0 {
		builder.WriteString("\n")
		builder.WriteString("📁 文件数量：")
		builder.WriteString(fmt.Sprintf("%d", enhanced.FileCount))
		builder.WriteString(" 个")
	}

	return builder.String()
}

// formatEpisodePhotoCaption formats a caption for episode photo notifications (极简呼吸感排版)
func (s *WebhookService) formatEpisodePhotoCaption(agg *EpisodeAggregation, epRange string) string {
	var builder strings.Builder

	// Build title string with year, season and episode range
	var title string
	if agg.Year > 1900 && agg.Year < 2100 {
		title = fmt.Sprintf("%s (%d) S%02d %s", agg.SeriesName, agg.Year, agg.Season, epRange)
	} else {
		title = fmt.Sprintf("%s S%02d %s", agg.SeriesName, agg.Season, epRange)
	}

	// Header line - complete title with year/season/episode
	builder.WriteString("✅ 入库成功：")
	builder.WriteString(title)
	builder.WriteString("\n")
	builder.WriteString("──────\n")

	// Name line
	builder.WriteString("\n")
	builder.WriteString("🎬 名称：")
	builder.WriteString(title)
	builder.WriteString("\n")

	// Category line
	category := "剧集"
	if agg.EnhancedInfo != nil {
		category = s.getDetailedCategory("Episode", agg.EnhancedInfo)
	}
	builder.WriteString("\n")
	builder.WriteString("🏷️ 类别：")
	builder.WriteString(category)
	builder.WriteString("\n")

	// Quality line
	if agg.Quality != "" {
		builder.WriteString("\n")
		builder.WriteString("💎 质量：")
		builder.WriteString(agg.Quality)
		builder.WriteString("\n")
	}

	// File size line - 只在有实际大小时显示
	if agg.FileSize > 0 {
		builder.WriteString("\n")
		builder.WriteString("📦 总大小：")
		builder.WriteString(s.formatFileSizeDecimal(agg.FileSize))
		builder.WriteString("\n")
	}

	// File count line - 使用 Episodes 列表长度而非 FileCount
	fileCount := len(agg.Episodes)
	builder.WriteString("\n")
	builder.WriteString("📁 文件数量：")
	builder.WriteString(fmt.Sprintf("%d", fileCount))
	builder.WriteString(" 个")

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

// SearchEmbyMedia searches for media in Emby library using fuzzy search
// Emby's SearchTerm parameter natively supports fuzzy matching - we trust its results
// CRITICAL: We filter by mediaType to prevent false matches between movies and series with the same name
func (s *WebhookService) SearchEmbyMedia(title string, year int, mediaType MediaType) (*EmbySearchResult, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return nil, fmt.Errorf("Emby URL or API key not configured")
	}

	// Build IncludeItemTypes based on mediaType to avoid false positives
	// e.g., user requests TV series "X", but movie "X" exists - should NOT block the request
	var includeItemTypes string
	switch mediaType {
	case MediaTypeMovie:
		includeItemTypes = "Movie"
	case MediaTypeTV:
		includeItemTypes = "Series"
	default:
		// Fallback: search both if mediaType is unknown (should not happen in normal flow)
		includeItemTypes = "Movie,Series"
		log.Printf("[SearchEmby] WARNING: Unknown mediaType %q, searching both Movie and Series", mediaType)
	}

	// Build search URL with fuzzy search parameters
	// SearchTerm: Emby's native fuzzy search - finds partial matches
	// IncludeItemTypes: Filter by media type (Movie or Series) to prevent false matches
	// Recursive: Search all library folders
	// Limit: Get up to 20 results to find the best match
	searchParams := fmt.Sprintf("?SearchTerm=%s&IncludeItemTypes=%s&Recursive=true&Limit=20",
		url.QueryEscape(title), includeItemTypes)
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
		TotalRecordCount int            `json:"TotalRecordCount"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	log.Printf("[SearchEmby] Query: %s, Found: %d results", title, len(response.Items))

	// Find best match using scoring system
	// We trust Emby's fuzzy search and score the results to find the best match
	type scoredResult struct {
		score  float64
		result *EmbySearchResult
		year   int
	}
	var candidates []scoredResult

	for _, item := range response.Items {
		// Convert to search result
		result, err := s.convertToSearchResult(item)
		if err != nil {
			log.Printf("[SearchEmby] Failed to convert item: %v", err)
			continue
		}

		itemTitle := result.Title
		itemYear := result.Year

		// Calculate match score
		score := 0.0

		// 1. Title similarity score (most important)
		titleLower := strings.ToLower(title)
		itemTitleLower := strings.ToLower(itemTitle)

		// Exact match gets highest score
		if itemTitleLower == titleLower {
			score += 100
		} else if strings.Contains(itemTitleLower, titleLower) {
			// Item title contains search term (e.g., "怪奇迷案限时破" contains "怪奇迷案")
			// Score based on how much of the search term is covered
			ratio := float64(len(title)) / float64(len(itemTitle))
			score += 50 + ratio*30
		} else if strings.Contains(titleLower, itemTitleLower) {
			// Search term contains item title (reverse match)
			score += 40
		} else {
			// Fuzzy match from Emby - give base score
			score += 10
		}

		// 2. Year matching (if year provided)
		if year > 0 && itemYear > 0 {
			yearDiff := itemYear - year
			if yearDiff == 0 {
				score += 30 // Exact year match
			} else if yearDiff >= -2 && yearDiff <= 2 {
				score += 15 // Close year (±2 years)
			} else if yearDiff >= -5 && yearDiff <= 5 {
				score += 5  // Somewhat close
			}
		}

		candidates = append(candidates, scoredResult{
			score:  score,
			result: result,
			year:   itemYear,
		})

		log.Printf("[SearchEmby] - %s (%d) score=%.1f", itemTitle, itemYear, score)
	}

	// Return highest scored result
	if len(candidates) == 0 {
		return nil, nil // No match found
	}

	// Sort by score descending
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].score > candidates[i].score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	best := candidates[0]
	log.Printf("[SearchEmby] Best match: %s (%d) score=%.1f", best.result.Title, best.year, best.score)

	return best.result, nil
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
				result.PosterURL = fmt.Sprintf("%s/Items/%s/Images/Primary/%s?fillWidth=400&quality=90&api_key=%s", s.embyURL, itemID, tag, s.embyAPIKey)
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
		ID:          payload.getItemID(),
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

