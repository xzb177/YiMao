package services

import (
	"fmt"
	"strings"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
)

func (s *WebhookService) sendAggregatedEpisodeToAdmins(seriesName string, year int, season int, episodes []int, epRange string, quality, imageURL string, fileSize int64, enhancedInfo *EmbyEnhancedInfo, libraryName string) {
	// 检查单集通知开关（任何管理员启用即发送）
	if s.mediaNotificationSvc != nil && !s.mediaNotificationSvc.IsSingleEnabled() {
		logger.Info("[入库] 单集群组通知已关闭，跳过发送")
		return
	}

	// 只发送到群组
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

		// #3 拼车 +1：入库后 @ 想看该剧的用户，并清空拼车记录（#2 三层通知）。
		if enhancedInfo != nil {
			s.notifyCarpoolMembers(enhancedInfo.TMDBID, "tv", seriesName)
		}
	}
}

func (s *WebhookService) sendNotificationWithPhotoToAdmin(adminID int64, caption string, imageURL string, enhancedInfo *EmbyEnhancedInfo) {
	// Try to send as photo using Telegram's built-in URL download
	_, err := s.telegram.SendPhotoFromURL(adminID, imageURL, caption, nil)
	if err != nil {
		logger.Info("[入库] Failed to send photo to admin %d: %v, falling back to text", adminID, err)
		// Fallback to text-only message
		s.telegram.SendMessage(adminID, caption, "", nil)
	}
}

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
	builder.WriteString("──────────────────\n")

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

	// #2 群追剧「集数进度条」：追加一行更新进度。
	// current 用本次聚合到的最大集号；total 从 TMDB 取（取不到则只显示「已更到 EXX」）。
	if line := s.buildEpisodeProgressLine(agg); line != "" {
		builder.WriteString("\n")
		builder.WriteString(line)
	}

	return builder.String()
}

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

	// File size：仅展示 Emby 返回的有效大小。
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
	if strings.TrimSpace(title) == "" {
		title = "新入库媒体"
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
	if strings.TrimSpace(title) == "" {
		title = "新入库媒体"
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
	builder.WriteString("──────────────────\n")

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

	// 仅展示有效大小；不向用户暴露 0B 或底层“引用文件”实现。
	if enhanced != nil && enhanced.FileSize > 0 {
		builder.WriteString(fmt.Sprintf("📦 总大小：%s\n", s.formatFileSizeDecimal(enhanced.FileSize)))
	}

	// File count line - only display an actual positive count
	if enhanced != nil && enhanced.FileCount > 0 {
		builder.WriteString(fmt.Sprintf("📁 文件数量：%d个\n", enhanced.FileCount))
	}

	return builder.String()
}

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
		logger.Info("[Webhook] Caption truncated from %d to %d characters", len(message), len(caption))
	}
	logger.Info("[Webhook] Sending photo caption (%d chars):\n%s", len(caption), caption)

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
		logger.Info("[Webhook] Failed to send photo: %v", err)

		// 【重要】Emby 图片失败时，尝试 TMDB fallback
		if strings.Contains(photoURL, s.embyURL) && enhancedInfo != nil && enhancedInfo.TMDBID != "" {
			logger.Info("[Webhook] Emby image failed, trying TMDB fallback with ID: %s", enhancedInfo.TMDBID)
			// 【关键】根据 enhancedInfo.Type 映射到 TMDB mediaType
			mediaType := "movie" // 默认
			if enhancedInfo.Type == "Series" || enhancedInfo.Type == "Episode" {
				mediaType = "tv"
			}
			if backdropURL := s.getTMDBBackdrop(enhancedInfo.TMDBID, mediaType); backdropURL != "" {
				logger.Info("[Webhook] Using TMDB backdrop (代理上传): %s", backdropURL)
				// 【关键】TMDB 备胎也使用代理上传
				_, tmdbErr := s.telegram.SendPhotoWithAuth(s.chatID, backdropURL, caption, nil, nil)
				if tmdbErr == nil {
					// TMDB 成功，添加到缓存并返回
					if s.messageCache != nil {
						s.messageCache.Add(s.chatID, message)
					}
					return
				}
				logger.Info("[Webhook] TMDB fallback also failed: %v", tmdbErr)
			} else {
				logger.Info("[Webhook] TMDB backdrop is empty for ID: %s", enhancedInfo.TMDBID)
			}
		}

		// 最后回退到纯文本消息
		logger.Info("[Webhook] All image attempts failed, falling back to text message")
		s.sendWithCache(s.chatID, message)
		return
	}

	// Add to cache
	if s.messageCache != nil {
		s.messageCache.Add(s.chatID, message)
	}
}

func (s *WebhookService) formatPhotoCaption(payload EmbyWebhookPayload, enhanced *EmbyEnhancedInfo) string {
	var builder strings.Builder

	// Debug logging for enhanced info
	if enhanced != nil {
		logger.Info("[Debug] formatPhotoCaption - Quality: %s, FileSize: %d bytes, FileCount: %d",
			enhanced.Quality, enhanced.FileSize, enhanced.FileCount)
	} else {
		logger.Info("[Debug] formatPhotoCaption - enhanced is nil")
	}

	// Get title
	title := payload.ItemName
	if title == "" && payload.Item != nil {
		title = payload.Item.Name
	}
	if enhanced != nil && enhanced.Title != "" {
		title = enhanced.Title
	}
	if strings.TrimSpace(title) == "" {
		title = "新入库媒体"
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
	builder.WriteString("──────────────────\n")

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

func (s *WebhookService) formatEpisodePhotoCaption(agg *EpisodeAggregation, epRange string) string {
	var builder strings.Builder

	// Debug logging
	logger.Info("[入库聚合] formatEpisodePhotoCaption: quality=%q, fileSize=%d, imageURL=%s",
		agg.Quality, agg.FileSize, agg.ImageURL)

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
	builder.WriteString("──────────────────\n")

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

	// #2 群追剧「集数进度条」：图片版通知同样追加进度行。
	if line := s.buildEpisodeProgressLine(agg); line != "" {
		builder.WriteString("\n")
		builder.WriteString(line)
	}

	return builder.String()
}

func resolveSummaryTitle(payload EmbyWebhookPayload, enhanced *EmbyEnhancedInfo) string {
	if title := strings.TrimSpace(payload.ItemName); title != "" {
		return title
	}
	if payload.Item != nil {
		if title := strings.TrimSpace(payload.Item.Name); title != "" {
			return title
		}
	}
	if enhanced != nil {
		if title := strings.TrimSpace(enhanced.Title); title != "" {
			return title
		}
	}
	if title := strings.TrimSpace(payload.SeriesName); title != "" {
		return title
	}
	return ""
}

func (s *WebhookService) addMediaItemToSummary(payload EmbyWebhookPayload, enhanced *EmbyEnhancedInfo) {
	if s.mediaNotificationSvc == nil {
		return
	}

	// Get library name (path fallback is unreliable, use empty string instead)
	libraryName := payload.Library
	if libraryName == "" && payload.Item != nil && payload.Item.Path != "" {
		// Try to extract library name from path instead of using full path
		// Path format: /library_name/...
		path := payload.Item.Path
		if strings.HasPrefix(path, "/") {
			parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
			if len(parts) > 0 && parts[0] != "" {
				libraryName = parts[0]
			}
		}
	}

	// Get media type - enhanced detection with multiple fallbacks
	mediaType := MediaTypeMovie
	itemType := payload.ItemType
	if itemType == "" && payload.Item != nil {
		itemType = payload.Item.Type
	}

	// Check if anime first (before other detection)
	isAnime := false
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

	// Detect media type with multiple signals
	// 1. Check ItemType field
	if itemType == "Episode" || itemType == "Series" || itemType == "Season" {
		mediaType = MediaTypeSeries
	}

	// 2. Check SeriesName - strong indicator of series/anime
	if payload.SeriesName != "" {
		if isAnime {
			mediaType = MediaTypeAnime
		} else {
			mediaType = MediaTypeSeries
		}
	}

	// 3. Override with anime library detection
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

	// Get filename for better movie title extraction
	fileName := ""
	if payload.Item != nil {
		// Try FileName field first, then extract from Path
		if payload.Item.FileName != "" {
			fileName = payload.Item.FileName
		} else if payload.Item.Path != "" {
			// Extract filename from path
			path := payload.Item.Path
			if idx := strings.LastIndex(path, "/"); idx != -1 {
				fileName = path[idx+1:]
			} else if idx := strings.LastIndex(path, "\\"); idx != -1 {
				fileName = path[idx+1:]
			} else {
				fileName = path
			}
		}
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

	// Create media item. Empty titles are not useful in a summary.
	title := resolveSummaryTitle(payload, enhanced)
	if title == "" {
		return
	}
	item := &MediaItem{
		Title:        title,
		Year:         year,
		LibraryName:  libraryName,
		MediaType:    mediaType,
		SeriesName:   payload.SeriesName,
		SeasonNumber: season,
		EpisodeStart: episode,
		EpisodeEnd:   episode,
		EpisodeCount: 1,
		IsCompleted:  false,
		FileName:     fileName,
	}

	s.mediaNotificationSvc.AddItem(item)
	logger.Info("[汇总] 已添加到每日汇总: %s", item.Title)
}

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
		Title:        agg.SeriesName,
		Year:         agg.Year,
		LibraryName:  agg.LibraryName,
		MediaType:    mediaType,
		SeriesName:   agg.SeriesName,
		SeasonNumber: agg.Season,
		EpisodeStart: episodeStart,
		EpisodeEnd:   episodeEnd,
		EpisodeCount: episodeCount,
		IsCompleted:  false,
	}

	s.mediaNotificationSvc.AddItem(item)
	logger.Info("[汇总] 已添加到每日汇总: %s S%02d %s", agg.SeriesName, agg.Season, epRange)
}

func (s *WebhookService) handleTestNotification() error {
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
