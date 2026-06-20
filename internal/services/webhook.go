package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
)

// NewWebhookService creates a new webhook service
func NewWebhookService(telegram *TelegramClient, moviepilot *MoviePilotClient, userMapping UserMappingStore, adminService *AdminService, preferences *PreferencesService, chatID int64, embyURL, embyAPIKey, embyUserID string, embySkipTLSVerify bool, mediaNotificationSvc *MediaNotificationService, notificationFormat string, tmdbAPIKey string) *WebhookService {
	svc := &WebhookService{
		telegram:             telegram,
		moviepilot:           moviepilot,
		userMapping:          userMapping,
		adminService:         adminService,
		preferences:          preferences,
		chatID:               chatID,
		embyURL:              embyURL,
		embyAPIKey:           embyAPIKey,
		embyUserID:           embyUserID,
		embySkipTLSVerify:    embySkipTLSVerify,
		mediaNotificationSvc: mediaNotificationSvc,
		messageCache:         NewMessageCache(5 * time.Minute),
		notificationFormat:   notificationFormat,
		tmdbAPIKey:           tmdbAPIKey,
		tmdbClient:           &http.Client{Timeout: 10 * time.Second},
		epAggregation:        make(map[string]*EpisodeAggregation),
		aggregationDelay:     60 * time.Second, // 默认60秒聚合延迟
		fileInfoCache:        make(map[string]*cachedFileInfo),
		fileInfoCacheTTL:     1 * time.Hour, // 缓存1小时
	}

	// Auto-discover Emby user ID if not configured
	if svc.embyUserID == "" && svc.embyURL != "" && svc.embyAPIKey != "" {
		if uid, err := svc.discoverEmbyUserID(); err == nil {
			svc.embyUserID = uid
			logger.Info("[Webhook] Auto-discovered Emby user ID: %s", uid)
		} else {
			logger.Info("[Webhook] Warning: Could not auto-discover Emby user ID: %v. Set EMBY_USER_ID env var.", err)
		}
	}

	// 启动缓存清理协程
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("[Webhook] cleanupFileInfoCache panic: %v", r)
			}
		}()
		svc.cleanupFileInfoCache()
	}()

	return svc
}

// discoverEmbyUserID fetches the first admin user ID from Emby
func (s *WebhookService) discoverEmbyUserID() (string, error) {
	usersURL := s.embyURL + "/Users?IsDisabled=false"
	req, err := http.NewRequest("GET", usersURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Emby-Token", s.embyAPIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := s.tmdbClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("Emby users API returned status %d", resp.StatusCode)
	}

	var users []struct {
		ID      string `json:"Id"`
		Name    string `json:"Name"`
		IsAdmin bool   `json:"Policy.IsAdministrator"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return "", fmt.Errorf("failed to decode Emby users: %w", err)
	}

	for _, u := range users {
		if u.IsAdmin {
			return u.ID, nil
		}
	}
	if len(users) > 0 {
		return users[0].ID, nil
	}

	return "", fmt.Errorf("no users found in Emby")
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
				logger.Info("[EmbyAPI] Cleaned expired cache for %s", itemID)
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
		logger.Info("[Webhook] Emby event: %s, item: %s", eventType, itemName)
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
	// 检查单集/即时通知开关（关闭后跳过群组即时推送）
	if s.mediaNotificationSvc != nil && !s.mediaNotificationSvc.IsSingleEnabled() {
		logger.Info("[入库] 即时群组通知已关闭，跳过发送: type=%s, name=%s", itemType, itemName)
		// 仍保留每日汇总入列能力
		s.addMediaItemToSummary(payload, nil)
		return nil
	}

	// Debug: check if payload.Item is available
	if payload.Item != nil {
		logger.Info("[Debug] payload.Item exists: Name=%s, Path=%s", payload.Item.Name, payload.Item.Path)
		if payload.Item.ProviderIds != nil {
			logger.Info("[Debug] payload.Item.ProviderIds: %v", payload.Item.ProviderIds)
		} else {
			logger.Info("[Debug] payload.Item.ProviderIds is nil")
		}
	} else {
		logger.Info("[Debug] payload.Item is nil")
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
			logger.Info("[Webhook] Failed to get enhanced info for %s: %v", itemName, err)
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
			logger.Info("[Webhook] Parsed quality from webhook path: %s, format: %s (WEB-DL: %v)", quality, format, isWEBDL)

			if enhancedPayload == nil {
				// Create minimal enhanced info from webhook payload
				enhancedPayload = &EmbyEnhancedInfo{
					Quality: quality,
					Format:  format,
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
					logger.Info("[Webhook] Getting TMDB backdrop from webhook ProviderIds: %s (mediaType: %s)", tmdbID, mediaType)
					if backdropURL := s.getTMDBBackdrop(tmdbID, mediaType); backdropURL != "" {
						enhancedPayload.ImageURL = backdropURL
						logger.Info("[Webhook] Got TMDB backdrop: %s", backdropURL)
					}
				}
			}
		} else {
			// No path available in webhook payload - check MediaSources directly
			logger.Info("[Webhook] No path available, checking MediaSources (%d items)", len(payload.Item.MediaSources))
			if enhancedPayload == nil {
				enhancedPayload = &EmbyEnhancedInfo{}
			}
			// Get file info from MediaSources
			for _, ms := range payload.Item.MediaSources {
				if ms.Size > 0 {
					enhancedPayload.FileSize = ms.Size
					enhancedPayload.FileCount = 1
					logger.Info("[Webhook] Got file size from MediaSources: %d bytes", ms.Size)
					// Try to parse quality from MediaSource path
					if ms.Path != "" {
						enhancedPayload.Quality = s.parseQualityFromPath(ms.Path)
						enhancedPayload.Format = s.parseReleaseFormat(ms.Path)
						enhancedPayload.IsWEBDL = s.detectWEBDL(ms.Path)
						logger.Info("[Webhook] Parsed quality from MediaSource path: %s, format: %s", enhancedPayload.Quality, enhancedPayload.Format)
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
					logger.Info("[Webhook] Enhanced file size from Emby API: %d bytes", apiSize)
				}
				if apiCount > 0 && enhancedPayload.FileCount == 0 {
					enhancedPayload.FileCount = apiCount
					logger.Info("[Webhook] Enhanced file count from Emby API: %d", apiCount)
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
		logger.Info("[入库] %s", itemName)
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

		// #3 拼车 +1：电影/单条入库后 @ 想看该片的用户并清空记录。
		// itemType 为剧集类（Series/Episode/Season）时按 tv，否则按 movie。
		carpoolType := "movie"
		if itemType == "Series" || itemType == "Episode" || itemType == "Season" {
			carpoolType = "tv"
		}
		carpoolTMDBID := s.extractTMDBID(enhancedPayload, payload)
		// #2 群内公示用片名：优先 enhanced.Title，退回 webhook itemName。
		carpoolTitle := itemName
		if enhancedPayload != nil && strings.TrimSpace(enhancedPayload.Title) != "" {
			carpoolTitle = enhancedPayload.Title
		}
		s.notifyCarpoolMembers(carpoolTMDBID, carpoolType, carpoolTitle)

		// 入库后私聊通知求片用户：查找匹配的审核请求，通知求片人
		s.notifyRequesterOnLibraryAdd(carpoolTMDBID, carpoolType, carpoolTitle)
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
		logger.Info("[入库聚合] 跳过无效集数: %s", payload.SeriesName)
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
		logger.Info("[入库聚合] 无法获取剧集名称，跳过")
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
				logger.Info("[Webhook] Parsed quality from webhook path (episode): %s, format: %s (WEB-DL: %v)", quality, format, isWEBDL)

				if enhancedInfo == nil {
					// Create minimal enhanced info from webhook payload
					enhancedInfo = &EmbyEnhancedInfo{
						Quality: quality,
						Format:  format,
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
					// 复制文件大小和数量
					for _, ms := range payload.Item.MediaSources {
						enhancedInfo.FileSize = ms.Size
						enhancedInfo.FileCount = 1
						logger.Info("[Webhook] Copied file size from MediaSources: %d bytes", ms.Size)
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
						logger.Info("[Webhook] Getting TMDB backdrop from webhook ProviderIds (episode): %s", tmdbID)
						// 【关键】Episode 必须使用 "tv"
						if backdropURL := s.getTMDBBackdrop(tmdbID, "tv"); backdropURL != "" {
							enhancedInfo.ImageURL = backdropURL
							logger.Info("[Webhook] Got TMDB backdrop (episode): %s", backdropURL)
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
			SeriesName:   seriesName, // 使用已处理的 seriesName (带 fallback)
			SeriesID:     seriesID,   // 保存 SeriesID 用于后续获取图片
			Year:         year,
			Season:       season,
			Episodes:     []int{},
			FirstAdded:   time.Now(),
			EnhancedInfo: enhancedInfo,
			LibraryName:  payload.Library,
			FileSize:     0, // 初始化为0，后续通过累加获取
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

		logger.Info("[入库聚合] 创建新聚合: %s S%02d, 延迟 %v 后发送", seriesName, season, s.aggregationDelay)
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
		logger.Info("[Debug] Payload.Item has %d MediaSources", len(payload.Item.MediaSources))
		for i, ms := range payload.Item.MediaSources {
			logger.Info("[Debug] MediaSource[%d]: Size=%d, Path=%s", i, ms.Size, ms.Path)
			// 即使 Size 为 0 或很小（如 strm 文件），也要记录
			thisFileSize = ms.Size
			thisFileCount = 1
			break // 使用第一个 MediaSource
		}

		// 【黑科技】如果文件大小为 0 或很小（strm 文件），调用 Emby API 获取真实大小
		if thisFileSize == 0 {
			if itemID := payload.getItemID(); itemID != "" {
				if apiSize, apiCount, err := s.fetchMediaSourcesFromEmby(itemID); err == nil && apiSize > 0 {
					thisFileSize = apiSize
					thisFileCount = apiCount
					logger.Info("[入库聚合] 从 Emby API 获取到真实文件大小: %d bytes, %d files", apiSize, apiCount)
				}
			}
		}
	}
	logger.Info("[Debug] Episode file size: %d bytes, count: %d", thisFileSize, thisFileCount)

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

		logger.Info("[入库聚合] 添加集数: %s S%02dE%02d (当前共%d集, 总大小:%s), 重置定时器 %v",
			seriesName, season, episode, len(agg.Episodes),
			s.formatFileSizeDecimal(agg.FileSize), s.aggregationDelay)
	} else {
		logger.Info("[入库聚合] 集数已存在: %s S%02dE%02d, 跳过", seriesName, season, episode)
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
		logger.Info("[入库聚合] 错误：SeriesName 为空，跳过发送通知，key=%s", key)
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
			logger.Info("[入库聚合] 从 TMDB 获取到 backdrop: %s", imageURL)
		}
	}

	// Build episode range string
	epRange := buildEpisodeRangeString(episodes)

	// Debug logging before sending
	logger.Info("[入库聚合] 准备发送通知: series=%s, quality=%q, fileSize=%d, imageURL=%s",
		seriesName, quality, fileSize, imageURL)

	// Send notification to admins based on their individual settings
	s.sendAggregatedEpisodeToAdmins(seriesName, year, season, episodes, epRange, quality, imageURL, fileSize, enhancedInfo, libraryName)

	logger.Info("[入库] %s S%02d %s (%d集, 总大小:%s)", seriesName, season, epRange, len(episodes), s.formatFileSizeDecimal(fileSize))

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
				logger.Info("[入库聚合] 使用已有图片: %s", photoURL)
			}

			// 【优先级2】如果没有，从 TMDB 获取 backdrop
			if photoURL == "" && agg.EnhancedInfo != nil && agg.EnhancedInfo.TMDBID != "" {
				// 【关键】剧集聚合必须使用 "tv"
				if backdropURL := s.getTMDBBackdrop(agg.EnhancedInfo.TMDBID, "tv"); backdropURL != "" {
					photoURL = backdropURL
					logger.Info("[入库聚合] 从 TMDB 获取到 backdrop: %s", photoURL)
				}
			}

			// 使用现有的多行排版文本作为 caption 发送图片
			if photoURL != "" {
				s.sendNotificationWithPhoto(message, photoURL, agg.EnhancedInfo)
			} else {
				// 实在获取不到图片，回退到纯文本
				logger.Info("[入库聚合] 无法获取图片，使用纯文本发送")
				s.sendWithCache(s.chatID, message)
			}
		}

		logger.Info("[入库] %s S%02d %s (%d集)", agg.SeriesName, agg.Season, epRange, len(agg.Episodes))

		// Add to daily summary list
		s.addAggregatedEpisodeToSummary(agg, epRange)

		delete(s.epAggregation, key)
	}
}

// sendAggregatedEpisodeToAdmins sends aggregated episode notification to group chat only
// 入库通知推送到群组，检查管理员单集开关设置

// sendNotificationWithPhotoToAdmin sends a photo notification to a specific admin

// buildEpisodeRangeString builds episode range string with forced head-tail merge
// 单集显示 "E02"，多集强制首尾合并显示 "E02-E18"（忽略中间断层）

// formatAggregatedEpisodeMessage formats aggregated episode notification (极简呼吸感排版)

// formatAggregatedEpisodeSimple formats aggregated episode notification in simple format

// formatEmbyNotificationSimple formats an Emby notification in simple format

// getEmbyEnhancedInfoForEpisode fetches enhanced information from Emby API for episodes
// It queries both the episode and its parent series to get genres and backdrop
// 图片优先级：Emby（代理上传） → TMDB（备胎）

// getSeriesInfo fetches series information from Emby API (including backdrop image)

// getEmbyEnhancedInfo fetches enhanced information from Emby API

// getTMDBBackdrop fetches backdrop URL from TMDB API (横屏图片)
// 【关键修复】必须传入 mediaType 避免跨库撞车！
// mediaType: "movie" 或 "tv" (剧集/动漫统一使用 tv)

// getTMDBPoster fetches poster URL from TMDB API (竖版海报)
// 添加中文语言参数以获取中文海报

// detectWEBDL detects if the source is a WEB-DL from the path

// detectQuality detects video quality from width (从MediaSources的Width字段获取)

// parseQualityFromPath parses quality from file path

// parseReleaseFormat parses release format from file path
// 支持格式: BluRay, WEB-DL, WEBRip, HDTV, DVDRip, HDRip, REMUX, etc.

// getFullQuality returns full quality string including format info
// 支持格式: "BluRay 1080p", "WEB-DL 2160p", "HDTV 720p", "BluRay.REMUX 2160p" 等

// inferFileCount 从路径中智能推断文件数量
// 支持检测：CD1/CD2, Part1/Part2, Disc1/Disc2, x264/x265 多文件等

// fetchMediaSourcesFromEmby 直接从 Emby API 获取完整的 MediaSources 信息
// 带缓存机制，避免频繁调用对 Emby 服务器造成负担

// formatEmbyNotificationEnhanced formats an enhanced Emby notification (new detailed format)

// getDetailedCategory returns detailed category name based on genres and item type

// getBasicCategory returns basic category name based on item type

// getImageURL gets the image URL from payload or enhanced info

// getItemID gets the ItemID from payload with fallback to Item.Id

// formatFileSizeDecimal formats file size in decimal (GB not GiB) for consistency

// formatFileSize formats file size in human readable format

// truncateCaption truncates caption to fit Telegram's 1024 character limit

// sendNotificationWithPhoto sends notification with photo
// enhancedInfo is used for TMDB fallback when Emby image download fails

// formatPhotoCaption formats a compact caption for photo notifications (Telegram limit: 1024 chars)
// 【更新】统一使用极简呼吸感排版，与 formatEpisodePhotoCaption 保持一致

// formatEpisodePhotoCaption formats a caption for episode photo notifications (极简呼吸感排版)

// addMediaItemToSummary adds media item to daily summary list

// addAggregatedEpisodeToSummary adds aggregated episode to daily summary list

// handleTestNotification handles test notification

// GetEmbyMediaInfo fetches media info from Emby API

// SearchEmbyMedia searches for media in Emby library using fuzzy search
// Emby's SearchTerm parameter natively supports fuzzy matching - we trust its results
// CRITICAL: We filter by mediaType to prevent false matches between movies and series with the same name

// convertToSearchResult converts Emby item to search result

// SendJellyseerrIssueComment sends a comment to Jellyseerr issue
// Deprecated: Use MoviePilot API instead

// CloseJellyseerrIssue closes an issue in Jellyseerr
// Deprecated: Use MoviePilot API instead

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
