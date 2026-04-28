package services

import (
	"fmt"
	"strings"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
)

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

func (s *WebhookService) detectQuality(width int) string {
	switch {
	case width >= 3800:
		return "2160p" // 4K统一显示为2160p
	case width >= 1900:
		return "1080p"
	case width >= 1200:
		return "720p"
	default:
		return "" // 无法确定时返回空字符串，严禁伪造数据
	}
}

func (s *WebhookService) parseQualityFromPath(path string) string {
	path = strings.ToLower(path)
	logger.Info("[Quality] Parsing quality from path: %s", path)

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
	logger.Info("[Quality] Could not parse quality from path: %s", path)
	return ""
}

func (s *WebhookService) parseReleaseFormat(path string) string {
	if path == "" {
		return ""
	}

	pathLower := strings.ToLower(path)
	logger.Info("[Format] Parsing format from path: %s", path)

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

	logger.Info("[Format] No known format found in path")
	return ""
}

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

func (s *WebhookService) inferFileCount(path string) int {
	if path == "" {
		return 1
	}

	pathLower := strings.ToLower(path)
	count := 1

	// 检测多CD/Part/Disc标记
	multiFilePatterns := []struct {
		pattern    string
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

func (s *WebhookService) getImageURL(payload EmbyWebhookPayload, enhanced *EmbyEnhancedInfo) string {
	if enhanced != nil && enhanced.ImageURL != "" {
		return enhanced.ImageURL
	}
	return ""
}

func (p *EmbyWebhookPayload) getItemID() string {
	if p.ItemID != "" {
		return p.ItemID
	}
	if p.Item != nil && p.Item.Id != "" {
		return p.Item.Id
	}
	return ""
}

func (s *WebhookService) formatFileSizeDecimal(bytes int64) string {
	// Handle unknown size (e.g., strm files, missing info)
	if bytes == 0 {
		return "未知大小"
	}

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

func (s *WebhookService) formatFileSize(bytes int64) string {
	// Handle unknown size (e.g., strm files, missing info)
	if bytes == 0 {
		return "未知"
	}

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
