package services

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
)

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
			logger.Info("[Debug] Got %d genres from series %s", len(info.Genres), seriesID)
			// Copy TMDB ID from series for image lookup
			if seriesInfo.TMDBID != "" {
				info.TMDBID = seriesInfo.TMDBID
			}
		} else {
			logger.Info("[Debug] Failed to get series info: %v", err)
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
		logger.Info("[Debug] Episode info - Quality: %s, FileSize: %d, FileCount: %d",
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
		logger.Info("[Debug] Failed to get episode info: %v", err)
	}

	// 【优先级1】从 TMDB 获取 backdrop（横屏图片）
	if info.ImageURL == "" && info.TMDBID != "" {
		// 【关键】Episode 必须使用 "tv"
		if backdropURL := s.getTMDBBackdrop(info.TMDBID, "tv"); backdropURL != "" {
			info.ImageURL = backdropURL
			logger.Info("[Debug] Using TMDB backdrop: %s", backdropURL)
		}
	}

	return info, nil
}

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

	client := s.tmdbClient
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
			logger.Info("[Debug] Found TMDB ID from tmdb: %s", tid)
		} else if tid, ok := providerIds["Tmdb"].(string); ok && tid != "" {
			// Try capitalized "Tmdb"
			info.TMDBID = tid
			logger.Info("[Debug] Found TMDB ID from Tmdb: %s", tid)
		} else if tid, ok := providerIds["Tvdb"].(string); ok && tid != "" {
			// Use Tvdb as fallback (TMDB API can sometimes use TVDB ID)
			info.TMDBID = tid
			logger.Info("[Debug] Using Tvdb ID as TMDB ID: %s", tid)
		}
	}

	return info, nil
}

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

	client := s.tmdbClient
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
		logger.Info("[Debug] Item Type: %s", itemType)
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
	logger.Info("[Debug] TMDB ID: %s", tmdbID)

	// 【优先级1】从 TMDB 获取 backdrop（横屏图片）
	if info.ImageURL == "" && tmdbID != "" {
		// 【关键】根据 itemType 映射到 TMDB mediaType
		mediaType := "movie" // 默认
		if itemType == "Series" || itemType == "Episode" {
			mediaType = "tv"
		}
		if backdropURL := s.getTMDBBackdrop(tmdbID, mediaType); backdropURL != "" {
			info.ImageURL = backdropURL
			logger.Info("[Debug] Using TMDB backdrop: %s", backdropURL)
		} else {
			logger.Info("[Debug] TMDB backdrop not found for ID: %s", tmdbID)
		}
	}

	if info.ImageURL != "" {
		logger.Info("[Debug] Final ImageURL: %s", info.ImageURL)
	}

	// Extract media info for quality and file count
	if mediaSources, ok := result["MediaSources"].([]interface{}); ok && len(mediaSources) > 0 {
		logger.Info("[Debug] MediaSources found: %d sources", len(mediaSources))
		for _, ms := range mediaSources {
			if source, ok := ms.(map[string]interface{}); ok {
				// Get quality from media type or parse from path
				if width, ok := source["Width"].(float64); ok {
					info.Quality = s.detectQuality(int(width))
					logger.Info("[Debug] Quality from Width: %s (width=%d)", info.Quality, int(width))
				} else {
					logger.Info("[Debug] No Width field in MediaSource")
					// Try to parse quality from file path
					if path, ok := source["Path"].(string); ok {
						info.Quality = s.parseQualityFromPath(path)
						logger.Info("[Debug] Quality from Path: %s (path=%s)", info.Quality, path)
					} else {
						logger.Info("[Debug] No Path field in MediaSource")
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

func (s *WebhookService) getTMDBBackdrop(tmdbID string, mediaType string) string {
	apiKey := s.tmdbAPIKey
	if apiKey == "" {
		logger.Warn("[TMDB] TMDB_API_KEY 未配置，跳过背景图获取")
		return ""
	}

	client := s.tmdbClient

	// 【关键】使用 /images 端点，根据 mediaType 动态拼接路径
	// 【关键】添加 include_image_language=zh,null 优先中文图片
	url := fmt.Sprintf("https://api.themoviedb.org/3/%s/%s/images?api_key=%s&include_image_language=zh,null", mediaType, tmdbID, apiKey)

	logger.Info("[TMDB] Fetching images for %s ID %s", mediaType, tmdbID)
	resp, err := client.Get(url)
	if err != nil {
		logger.Info("[TMDB] API request failed for %s ID %s: %v", mediaType, tmdbID, err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Info("[TMDB] API returned status %d for %s ID %s", resp.StatusCode, mediaType, tmdbID)
		return ""
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logger.Info("[TMDB] Failed to decode response for %s ID %s: %v", mediaType, tmdbID, err)
		return ""
	}

	// 【绝对锁定】只读取 backdrops 数组，绝不读取 posters！
	if backdrops, ok := result["backdrops"].([]interface{}); ok && len(backdrops) > 0 {
		if firstBackdrop, ok := backdrops[0].(map[string]interface{}); ok {
			if filePath, ok := firstBackdrop["file_path"].(string); ok && filePath != "" {
				url := fmt.Sprintf("https://image.tmdb.org/t/p/original%s", filePath)
				logger.Info("[TMDB] Got backdrop for %s ID %s: %s", mediaType, tmdbID, filePath)
				return url
			}
		}
	}

	// 【严禁】没有 backdrop 就返回空，宁可让 Telegram 走纯文本 fallback，也不发难看的竖屏海报！
	logger.Info("[TMDB] No backdrop found for %s ID %s, returning empty (will use text fallback)", mediaType, tmdbID)
	return ""
}

func (s *WebhookService) getTMDBPoster(tmdbID string) string {
	apiKey := s.tmdbAPIKey
	if apiKey == "" {
		logger.Warn("[TMDB] TMDB_API_KEY 未配置，跳过海报获取")
		return ""
	}

	client := s.tmdbClient

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
			logger.Info("[EmbyAPI] Cache hit for %s: size=%d, files=%d", itemID, cached.fileSize, cached.fileCount)
			return cached.fileSize, cached.fileCount, nil
		}
	}
	s.fileInfoCacheMu.RUnlock()

	// 【API调用】缓存未命中，调用 Emby API
	logger.Info("[EmbyAPI] Cache miss, fetching from API for %s", itemID)

	// 调用 Emby API 获取完整的 Items 信息
	url := fmt.Sprintf("%s/Users/%s/Items/%s", s.embyURL, s.embyUserID, itemID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, 0, err
	}

	req.Header.Set("X-Emby-Token", s.embyAPIKey)

	client := s.tmdbClient
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

	logger.Info("[EmbyAPI] Fetched media info for %s: size=%d, files=%d", itemID, totalSize, totalFiles)

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

func (s *WebhookService) GetEmbyMediaInfo(itemID string) (map[string]interface{}, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return nil, fmt.Errorf("Emby URL or API key not configured")
	}

	url := fmt.Sprintf("%s/Users/%s/Items/%s", s.embyURL, s.embyUserID, itemID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Emby-Token", s.embyAPIKey)
	req.Header.Set("Accept", "application/json")

	client := s.tmdbClient
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
		logger.Info("[SearchEmby] WARNING: Unknown mediaType %q, searching both Movie and Series", mediaType)
	}

	// Build search URL with fuzzy search parameters
	// SearchTerm: Emby's native fuzzy search - finds partial matches
	// IncludeItemTypes: Filter by media type (Movie or Series) to prevent false matches
	// Recursive: Search all library folders
	// Limit: Get up to 20 results to find the best match
	searchParams := fmt.Sprintf("?SearchTerm=%s&IncludeItemTypes=%s&Recursive=true&Limit=20&Fields=ProviderIds",
		url.QueryEscape(title), includeItemTypes)
	fullURL := fmt.Sprintf("%s/Users/%s/Items%s", s.embyURL, s.embyUserID, searchParams)

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Emby-Token", s.embyAPIKey)
	req.Header.Set("Accept", "application/json")

	// Skip TLS verification only when explicitly enabled for trusted
	// self-signed/origin certificates (EMBY_SKIP_TLS_VERIFY). Default: verify.
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: s.embySkipTLSVerify},
	}
	client := &http.Client{
		Timeout:   5 * time.Second, // 降低超时避免阻塞求片流程，5秒足够正常响应
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
		Items            []map[string]interface{} `json:"Items"`
		TotalRecordCount int                      `json:"TotalRecordCount"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	logger.Info("[SearchEmby] Query: %s, Found: %d results", title, len(response.Items))

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
			logger.Info("[SearchEmby] Failed to convert item: %v", err)
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
				score += 5 // Somewhat close
			}
		}

		candidates = append(candidates, scoredResult{
			score:  score,
			result: result,
			year:   itemYear,
		})

		logger.Info("[SearchEmby] - %s (%d) score=%.1f", itemTitle, itemYear, score)
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
	logger.Info("[SearchEmby] Best match: %s (%d) score=%.1f", best.result.Title, best.year, best.score)

	return best.result, nil
}

// SearchEmbyMediaByTMDB checks library identity using the authoritative TMDB
// provider ID. Request and review gates must use this method rather than title
// fuzzy search, otherwise similarly named media can block the wrong request.
func (s *WebhookService) SearchEmbyMediaByTMDB(tmdbID int, mediaType MediaType) (*EmbySearchResult, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return nil, fmt.Errorf("Emby URL or API key not configured")
	}
	if tmdbID <= 0 {
		return nil, fmt.Errorf("invalid TMDB ID")
	}
	includeItemTypes := "Movie"
	if mediaType == MediaTypeTV {
		includeItemTypes = "Series"
	}
	params := url.Values{}
	params.Set("AnyProviderIdEquals", fmt.Sprintf("Tmdb.%d", tmdbID))
	params.Set("IncludeItemTypes", includeItemTypes)
	params.Set("Recursive", "true")
	params.Set("Fields", "ProviderIds,MediaSources")
	params.Set("Limit", "20")
	endpoint := fmt.Sprintf("%s/Users/%s/Items?%s", s.embyURL, url.PathEscape(s.embyUserID), params.Encode())
	req, err := http.NewRequest(http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Emby-Token", s.embyAPIKey)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: s.embySkipTLSVerify}}}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Emby API returned status %d", resp.StatusCode)
	}
	var response struct {
		Items []map[string]interface{} `json:"Items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}
	want := strconv.Itoa(tmdbID)
	for _, item := range response.Items {
		providerIDs, _ := item["ProviderIds"].(map[string]interface{})
		got := ""
		if value, ok := providerIDs["Tmdb"].(string); ok {
			got = value
		} else if value, ok := providerIDs["tmdb"].(string); ok {
			got = value
		}
		if got != want {
			continue
		}
		result, err := s.convertToSearchResult(item)
		if err != nil {
			return nil, err
		}
		return result, nil
	}
	return nil, nil
}

// CaptureEmbyWashBaseline returns the exact MediaSource paths currently bound
// to the requested movie or TV season. The result is suitable for persistent
// comparison when an administrator later marks the wash complete.
func (s *WebhookService) CaptureEmbyWashBaseline(tmdbID int, mediaType MediaType, season int) ([]string, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return nil, fmt.Errorf("Emby URL or API key not configured")
	}
	if mediaType == MediaTypeTV && season <= 0 {
		return nil, fmt.Errorf("TV wash requires a positive season")
	}
	includeItemTypes := "Movie"
	if mediaType == MediaTypeTV {
		includeItemTypes = "Series"
	}
	params := url.Values{}
	params.Set("AnyProviderIdEquals", fmt.Sprintf("Tmdb.%d", tmdbID))
	params.Set("IncludeItemTypes", includeItemTypes)
	params.Set("Recursive", "true")
	params.Set("Fields", "ProviderIds,MediaSources")
	endpoint := fmt.Sprintf("%s/Users/%s/Items?%s", s.embyURL, url.PathEscape(s.embyUserID), params.Encode())
	var search struct {
		Items []struct {
			ID           string            `json:"Id"`
			ProviderIDs  map[string]string `json:"ProviderIds"`
			MediaSources []EmbyMediaSource `json:"MediaSources"`
		} `json:"Items"`
	}
	if err := s.getEmbyJSON(endpoint, &search); err != nil {
		return nil, err
	}
	var targetID string
	var sources []EmbyMediaSource
	wantTMDB := strconv.Itoa(tmdbID)
	for _, item := range search.Items {
		if item.ProviderIDs["Tmdb"] == wantTMDB || item.ProviderIDs["tmdb"] == wantTMDB {
			targetID, sources = item.ID, item.MediaSources
			break
		}
	}
	if targetID == "" {
		return nil, fmt.Errorf("Emby wash target not found")
	}
	if mediaType == MediaTypeTV {
		params = url.Values{}
		params.Set("Season", strconv.Itoa(season))
		params.Set("Fields", "MediaSources")
		if s.embyUserID != "" {
			params.Set("UserId", s.embyUserID)
		}
		episodesEndpoint := fmt.Sprintf("%s/Shows/%s/Episodes?%s", s.embyURL, url.PathEscape(targetID), params.Encode())
		var episodes struct {
			Items []struct {
				ParentIndexNumber int               `json:"ParentIndexNumber"`
				MediaSources      []EmbyMediaSource `json:"MediaSources"`
			} `json:"Items"`
		}
		if err := s.getEmbyJSON(episodesEndpoint, &episodes); err != nil {
			return nil, err
		}
		sources = nil
		for _, episode := range episodes.Items {
			// Do not trust server-side filtering alone: retain exact-season safety.
			if episode.ParentIndexNumber == season {
				sources = append(sources, episode.MediaSources...)
			}
		}
	}
	seen := make(map[string]struct{})
	paths := make([]string, 0, len(sources))
	for _, source := range sources {
		path := strings.TrimSpace(source.Path)
		if path == "" {
			continue
		}
		if _, exists := seen[path]; !exists {
			seen[path] = struct{}{}
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("Emby wash target has no MediaSource paths")
	}
	sort.Strings(paths)
	return paths, nil
}

func (s *WebhookService) getEmbyJSON(endpoint string, dst interface{}) error {
	req, err := http.NewRequest(http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("X-Emby-Token", s.embyAPIKey)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: s.embySkipTLSVerify}}}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Emby API returned status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

// HasEmbyWashTarget verifies that a wash request points at media which already
// exists in Emby. TV requests must also point at an existing regular season.
// The check fails closed: an unavailable Emby API must not create a zero-quota
// substitute for an ordinary request.
func (s *WebhookService) HasEmbyWashTarget(tmdbID int, title string, year int, mediaType MediaType, season int) (bool, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return false, fmt.Errorf("Emby URL or API key not configured")
	}
	includeItemTypes := "Movie"
	if mediaType == MediaTypeTV {
		includeItemTypes = "Series"
	}
	params := url.Values{}
	params.Set("AnyProviderIdEquals", fmt.Sprintf("Tmdb.%d", tmdbID))
	params.Set("IncludeItemTypes", includeItemTypes)
	params.Set("Recursive", "true")
	params.Set("Fields", "ProviderIds,MediaSources")
	endpoint := fmt.Sprintf("%s/Users/%s/Items?%s", s.embyURL, url.PathEscape(s.embyUserID), params.Encode())
	req, err := http.NewRequest(http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return false, err
	}
	req.Header.Set("X-Emby-Token", s.embyAPIKey)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: s.embySkipTLSVerify}}}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("Emby exact provider search returned status %d", resp.StatusCode)
	}
	var search struct {
		Items []map[string]interface{} `json:"Items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&search); err != nil {
		return false, err
	}
	var media *EmbySearchResult
	wantTMDB := strconv.Itoa(tmdbID)
	for _, item := range search.Items {
		candidate, convertErr := s.convertToSearchResult(item)
		if convertErr == nil && candidate.TMDBID == wantTMDB {
			media = candidate
			break
		}
	}
	if media == nil {
		return false, nil
	}
	if mediaType != MediaTypeTV {
		return true, nil
	}
	if season <= 0 {
		return false, fmt.Errorf("TV wash requires a positive season")
	}

	params = url.Values{}
	if s.embyUserID != "" {
		params.Set("UserId", s.embyUserID)
	}
	seasonEndpoint := fmt.Sprintf("%s/Shows/%s/Seasons?%s", s.embyURL, url.PathEscape(media.ID), params.Encode())
	seasonReq, err := http.NewRequest(http.MethodGet, seasonEndpoint, http.NoBody)
	if err != nil {
		return false, err
	}
	seasonReq.Header.Set("X-Emby-Token", s.embyAPIKey)
	seasonReq.Header.Set("Accept", "application/json")
	seasonClient := &http.Client{
		Timeout:   8 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: s.embySkipTLSVerify}},
	}
	seasonResp, err := seasonClient.Do(seasonReq)
	if err != nil {
		return false, err
	}
	defer seasonResp.Body.Close()
	if seasonResp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("Emby seasons API returned status %d", seasonResp.StatusCode)
	}
	var payload struct {
		Items []struct {
			IndexNumber int `json:"IndexNumber"`
		} `json:"Items"`
	}
	if err := json.NewDecoder(seasonResp.Body).Decode(&payload); err != nil {
		return false, err
	}
	for _, item := range payload.Items {
		if item.IndexNumber == season {
			return true, nil
		}
	}
	return false, nil
}

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
	if providerIDs, ok := item["ProviderIds"].(map[string]interface{}); ok {
		if id, ok := providerIDs["Tmdb"].(string); ok {
			result.TMDBID = id
		} else if id, ok := providerIDs["tmdb"].(string); ok {
			result.TMDBID = id
		}
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
				// NOTE: PosterURL uses api_key in query string because Telegram
				// fetches images directly and cannot set X-Emby-Token header.
				// Always use logger.Sanitizef when logging this URL.
				result.PosterURL = fmt.Sprintf("%s/Items/%s/Images/Primary/%s?fillWidth=400&quality=90&api_key=%s", s.embyURL, itemID, tag, s.embyAPIKey)
			}
		}
	}

	return result, nil
}

func (s *WebhookService) SendJellyseerrIssueComment(issueID int64, comment string) error {
	if s.moviepilot == nil {
		return fmt.Errorf("MoviePilot client not configured")
	}

	// Note: MoviePilot API structure may differ, this is a placeholder for compatibility
	logger.Info("[Webhook] Issue comment functionality needs MoviePilot API implementation")
	return nil
}

func (s *WebhookService) CloseJellyseerrIssue(issueID int64) error {
	if s.moviepilot == nil {
		return fmt.Errorf("MoviePilot client not configured")
	}

	// Note: MoviePilot API structure may differ, this is a placeholder for compatibility
	logger.Info("[Webhook] Issue close functionality needs MoviePilot API implementation")
	return nil
}
