package ai

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// TrendingResult represents a trending media item with details
type TrendingResult struct {
	Title       string   `json:"title"`
	Year        int      `json:"year,omitempty"`
	MediaType   string   `json:"mediaType"` // movie or tv
	Genre       string   `json:"genre,omitempty"`
	Rating      float64  `json:"rating,omitempty"`
	Reason      string   `json:"reason"`       // AI-generated reason
	TmdbID      int      `json:"tmdbId,omitempty"`
	ReleaseDate string   `json:"releaseDate,omitempty"`
	Country     string   `json:"country,omitempty"`
}

// TrendingAIManager handles AI-powered trending recommendations
type TrendingAIManager struct {
	zhipu       *ZhipuClient
	jailed      map[string]*TrendingCacheItem // Cache for AI results
	cacheMutex  sync.RWMutex
	lastUpdated time.Time
	enabled     bool
}

// TrendingCacheItem represents a cached trending result
type TrendingCacheItem struct {
	Results    []*TrendingResult
	ExpiresAt  time.Time
}

// NewTrendingAIManager creates a new trending AI manager
func NewTrendingAIManager(zhipu *ZhipuClient) *TrendingAIManager {
	mgr := &TrendingAIManager{
		zhipu:       zhipu,
		jailed:      make(map[string]*TrendingCacheItem),
		lastUpdated: time.Time{},
		enabled:     zhipu != nil && zhipu.IsEnabled(),
	}
	// Load from file on startup
	mgr.loadFromFile()
	return mgr
}

// IsEnabled returns whether AI trending is enabled
func (m *TrendingAIManager) IsEnabled() bool {
	return m.enabled && m.zhipu != nil && m.zhipu.IsEnabled()
}

// GetTrendingMovies gets AI-recommended trending movies
func (m *TrendingAIManager) GetTrendingMovies(count int) ([]*TrendingResult, error) {
	if !m.IsEnabled() {
		return nil, fmt.Errorf("AI trending not enabled")
	}

	if count > 10 {
		count = 10
	}
	if count < 3 {
		count = 3
	}

	// Check cache
	cacheKey := "trending_movies"
	m.cacheMutex.RLock()
	if cached, exists := m.jailed[cacheKey]; exists && time.Now().Before(cached.ExpiresAt) {
		m.cacheMutex.RUnlock()
		log.Printf("[TrendingAI] Cache hit for trending_movies")
		return cached.Results, nil
	}
	m.cacheMutex.RUnlock()

	log.Printf("[TrendingAI] Fetching AI trending movies...")

	currentTime := time.Now().Format("2006-01-02 15:04")

	systemPrompt := fmt.Sprintf(`你是一位专业的影视推荐专家，精通全球热门影视作品。

请推荐 %d 部当前最受欢迎的电影（包括近期上映和高评分作品）。

返回格式必须是纯 JSON 数组，不要包含任何其他文字或格式标记：
[
  {"title": "电影名", "year": 2024, "genre": "类型", "rating": 8.5, "reason": "推荐理由", "tmdbId": 123, "releaseDate": "2024-01-01", "country": "国家"},
  {"title": "电影名", "year": 2024, "genre": "类型", "rating": 8.0, "reason": "推荐理由", "tmdbId": 456, "releaseDate": "2024-02-01", "country": "国家"}
]

要求：
1. 优先推荐 2023-2025 年的作品
2. 推荐评分 7.0 以上的作品
3. 包含中美、韩、日等地区的热门作品
4. 推荐理由要简洁有吸引力（不超过30字）
5. 每部作品给出准确的 TMDB ID（如果知道）
6. 返回纯JSON格式，不要有代码块标记

当前时间：%s`, count, currentTime)

	userMessage := fmt.Sprintf("推荐 %d 部当前最热门的电影", count)

	response, err := m.zhipu.Send(userMessage, systemPrompt)
	if err != nil {
		log.Printf("[TrendingAI] Error getting trending movies: %v", err)
		return nil, err
	}

	results, err := m.parseTrendingResults(response, "movie")
	if err != nil {
		log.Printf("[TrendingAI] Error parsing trending movies: %v", err)
		return nil, err
	}

	// Cache for 1 hour
	m.cacheMutex.Lock()
	m.jailed[cacheKey] = &TrendingCacheItem{
		Results:   results,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	m.lastUpdated = time.Now()
	m.cacheMutex.Unlock()

	// Save to file for persistence
	m.saveToFile()

	log.Printf("[TrendingAI] Got %d trending movies from AI", len(results))
	return results, nil
}

// GetHotTVShows gets AI-recommended hot TV shows
func (m *TrendingAIManager) GetHotTVShows(count int) ([]*TrendingResult, error) {
	if !m.IsEnabled() {
		return nil, fmt.Errorf("AI trending not enabled")
	}

	if count > 10 {
		count = 10
	}
	if count < 3 {
		count = 3
	}

	// Check cache
	cacheKey := "hot_tv_shows"
	m.cacheMutex.RLock()
	if cached, exists := m.jailed[cacheKey]; exists && time.Now().Before(cached.ExpiresAt) {
		m.cacheMutex.RUnlock()
		log.Printf("[TrendingAI] Cache hit for hot_tv_shows")
		return cached.Results, nil
	}
	m.cacheMutex.RUnlock()

	log.Printf("[TrendingAI] Fetching AI hot TV shows...")

	currentTime := time.Now().Format("2006-01-02 15:04")

	systemPrompt := fmt.Sprintf(`你是一位专业的影视推荐专家，精通全球热门剧集。

请推荐 %d 部当前最受欢迎的电视剧（包括近期播出和高评分作品）。

返回格式必须是纯 JSON 数组，不要包含任何其他文字或格式标记：
[
  {"title": "剧集名", "year": 2024, "genre": "类型", "rating": 8.5, "reason": "推荐理由", "tmdbId": 123, "releaseDate": "2024-01-01", "country": "国家"},
  {"title": "剧集名", "year": 2024, "genre": "类型", "rating": 8.0, "reason": "推荐理由", "tmdbId": 456, "releaseDate": "2024-02-01", "country": "国家"}
]

要求：
1. 优先推荐 2023-2025 年播出的剧集
2. 推荐评分 7.0 以上的作品
3. 包含中美、韩、日、英等地区的热门剧集
4. 推荐理由要简洁有吸引力（不超过30字）
5. 每部作品给出准确的 TMDB ID（如果知道）
6. 返回纯JSON格式，不要有代码块标记

当前时间：%s`, count, currentTime)

	userMessage := fmt.Sprintf("推荐 %d 部当前最热门的电视剧", count)

	response, err := m.zhipu.Send(userMessage, systemPrompt)
	if err != nil {
		log.Printf("[TrendingAI] Error getting hot TV shows: %v", err)
		return nil, err
	}

	results, err := m.parseTrendingResults(response, "tv")
	if err != nil {
		log.Printf("[TrendingAI] Error parsing hot TV shows: %v", err)
		return nil, err
	}

	// Cache for 1 hour
	m.cacheMutex.Lock()
	m.jailed[cacheKey] = &TrendingCacheItem{
		Results:   results,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	m.lastUpdated = time.Now()
	m.cacheMutex.Unlock()

	// Save to file for persistence
	m.saveToFile()

	log.Printf("[TrendingAI] Got %d hot TV shows from AI", len(results))
	return results, nil
}

// GetNewReleases gets AI-recommended new releases
func (m *TrendingAIManager) GetNewReleases(count int) ([]*TrendingResult, error) {
	if !m.IsEnabled() {
		return nil, fmt.Errorf("AI trending not enabled")
	}

	if count > 10 {
		count = 10
	}
	if count < 3 {
		count = 3
	}

	// Check cache
	cacheKey := "new_releases"
	m.cacheMutex.RLock()
	if cached, exists := m.jailed[cacheKey]; exists && time.Now().Before(cached.ExpiresAt) {
		m.cacheMutex.RUnlock()
		log.Printf("[TrendingAI] Cache hit for new_releases")
		return cached.Results, nil
	}
	m.cacheMutex.RUnlock()

	log.Printf("[TrendingAI] Fetching AI new releases...")

	currentTime := time.Now().Format("2006-01-02 15:04")

	systemPrompt := fmt.Sprintf(`你是一位专业的影视推荐专家，精通最新影视作品。

请推荐 %d 部最新上映的优质电影（最近3个月内上映或公布）。

返回格式必须是纯 JSON 数组，不要包含任何其他文字或格式标记：
[
  {"title": "电影名", "year": 2025, "genre": "类型", "rating": 8.5, "reason": "推荐理由", "tmdbId": 123, "releaseDate": "2025-01-01", "country": "国家"},
  {"title": "电影名", "year": 2025, "genre": "类型", "rating": 8.0, "reason": "推荐理由", "tmdbId": 456, "releaseDate": "2025-02-01", "country": "国家"}
]

要求：
1. 优先推荐最近上映的高期待作品
2. 推荐评分 6.5 以上的作品（新片评分可能较低）
3. 包含中美、韩、日等地区的最新作品
4. 推荐理由要突出"新"、"期待"等关键词
5. 每部作品给出准确的 TMDB ID（如果知道）
6. 返回纯JSON格式，不要有代码块标记

当前时间：%s

注意：今天是 2026 年 2 月 20 日，请推荐这个时间段前后上映的作品。`, count, currentTime)

	userMessage := fmt.Sprintf("推荐 %d 部最新上映的热门电影", count)

	response, err := m.zhipu.Send(userMessage, systemPrompt)
	if err != nil {
		log.Printf("[TrendingAI] Error getting new releases: %v", err)
		return nil, err
	}

	results, err := m.parseTrendingResults(response, "movie")
	if err != nil {
		log.Printf("[TrendingAI] Error parsing new releases: %v", err)
		return nil, err
	}

	// Cache for 30 minutes (new releases update more frequently)
	m.cacheMutex.Lock()
	m.jailed[cacheKey] = &TrendingCacheItem{
		Results:   results,
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}
	m.lastUpdated = time.Now()
	m.cacheMutex.Unlock()

	// Save to file for persistence
	m.saveToFile()

	log.Printf("[TrendingAI] Got %d new releases from AI", len(results))
	return results, nil
}

// parseTrendingResults parses AI response into trending results
func (m *TrendingAIManager) parseTrendingResults(response string, expectedType string) ([]*TrendingResult, error) {
	// Clean up the response
	response = cleanAIResponse(response)

	var results []*TrendingResult
	if err := json.Unmarshal([]byte(response), &results); err != nil {
		// Try to fix common JSON issues
		fixed := fixTrendingJSON(response)
		if err := json.Unmarshal([]byte(fixed), &results); err != nil {
			return nil, fmt.Errorf("failed to parse AI response: %w", err)
		}
	}

	// Ensure media type is set correctly
	for _, result := range results {
		if result.MediaType == "" {
			result.MediaType = expectedType
		}
	}

	return results, nil
}

// cleanAIResponse removes markdown code blocks and trims whitespace
func cleanAIResponse(response string) string {
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)
	return response
}

// fixTrendingJSON attempts to fix common JSON formatting issues
func fixTrendingJSON(input string) string {
	// Remove trailing commas
	input = strings.ReplaceAll(input, ",\n}", "\n}")
	input = strings.ReplaceAll(input, ",\n]", "\n]")
	input = strings.ReplaceAll(input, ", }", "}")
	input = strings.ReplaceAll(input, ", ]", "]")

	// Fix common AI model issues
	input = strings.ReplaceAll(input, "，", ",") // Chinese comma
	input = strings.ReplaceAll(input, "：", ":") // Chinese colon

	return input
}

// ClearCache clears all cached trending results
func (m *TrendingAIManager) ClearCache() {
	m.cacheMutex.Lock()
	defer m.cacheMutex.Unlock()
	m.jailed = make(map[string]*TrendingCacheItem)
	log.Printf("[TrendingAI] Cache cleared")
}

// RefreshCache refreshes all trending caches
func (m *TrendingAIManager) RefreshCache() error {
	if !m.IsEnabled() {
		return fmt.Errorf("AI trending not enabled")
	}

	log.Printf("[TrendingAI] Refreshing all caches...")

	// Refresh all three categories in parallel
	var wg sync.WaitGroup
	errChan := make(chan error, 3)

	wg.Add(3)

	go func() {
		defer wg.Done()
		if _, err := m.GetTrendingMovies(5); err != nil {
			log.Printf("[TrendingAI] Failed to refresh trending movies: %v", err)
			errChan <- err
		}
	}()

	go func() {
		defer wg.Done()
		if _, err := m.GetHotTVShows(5); err != nil {
			log.Printf("[TrendingAI] Failed to refresh hot TV shows: %v", err)
			errChan <- err
		}
	}()

	go func() {
		defer wg.Done()
		if _, err := m.GetNewReleases(5); err != nil {
			log.Printf("[TrendingAI] Failed to refresh new releases: %v", err)
			errChan <- err
		}
	}()

	wg.Wait()

	// Collect errors
	var errors []string
	close(errChan)
	for err := range errChan {
		if err != nil {
			errors = append(errors, err.Error())
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("some caches failed to refresh: %s", strings.Join(errors, "; "))
	}

	log.Printf("[TrendingAI] All caches refreshed successfully")
	return nil
}

// FormatTrendingResults formats trending results for display
func FormatTrendingResults(results []*TrendingResult, title string) string {
	if len(results) == 0 {
		return fmt.Sprintf("%s\n\n暂无推荐内容", title)
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("┌─── %s ────┐\n\n", title))
	sb.WriteString(fmt.Sprintf("  📅 更新时间: %s\n\n", time.Now().Format("2006-01-02 15:04")))
	sb.WriteString("  ━━━━━━━━━━━━━━━  \n\n")

	for i, item := range results {
		if i >= 8 {
			break
		}

		emoji := "🎬"
		if item.MediaType == "tv" {
			emoji = "📺"
		}

		// Format: 🔸 1. 电影名 (年份) ⭐x.x 🎬
		sb.WriteString(fmt.Sprintf("  %s %d. %s", emoji, i+1, item.Title))

		if item.Year > 0 {
			sb.WriteString(fmt.Sprintf(" (%d)", item.Year))
		}

		if item.Rating > 0 {
			sb.WriteString(fmt.Sprintf("  ⭐%.1f", item.Rating))
		}

		sb.WriteString(fmt.Sprintf(" %s\n", emoji))

		// Add reason in next line
		if item.Reason != "" {
			// Truncate reason to fit
			reason := item.Reason
			if len(reason) > 30 {
				reason = reason[:27] + "..."
			}
			sb.WriteString(fmt.Sprintf("     💡 %s\n", reason))
		}

		sb.WriteString("\n")
	}

	sb.WriteString("└──────────────────────┘")
	sb.WriteString(fmt.Sprintf("\n\n💡 输入数字 1-%d 查看详情", len(results)))

	return sb.String()
}

// GetCachedStatus returns the status of cached results
func (m *TrendingAIManager) GetCachedStatus() map[string]interface{} {
	status := make(map[string]interface{})

	m.cacheMutex.RLock()
	defer m.cacheMutex.RUnlock()

	now := time.Now()
	for key, cached := range m.jailed {
		status[key] = map[string]interface{}{
			"count":      len(cached.Results),
			"expires_at": cached.ExpiresAt.Format(time.RFC3339),
			"is_fresh":   now.Before(cached.ExpiresAt),
		}
	}

	status["last_updated"] = m.lastUpdated.Format(time.RFC3339)
	status["enabled"] = m.IsEnabled()

	return status
}

// GetLastUpdate returns the last update time
func (m *TrendingAIManager) GetLastUpdate() time.Time {
	m.cacheMutex.RLock()
	defer m.cacheMutex.RUnlock()
	return m.lastUpdated
}

// GetCacheUpdateTime returns the update time for a specific cache key
func (m *TrendingAIManager) GetCacheUpdateTime(cacheKey string) time.Time {
	m.cacheMutex.RLock()
	defer m.cacheMutex.RUnlock()
	if cached, exists := m.jailed[cacheKey]; exists {
		// Calculate when this cache was created (expires - cache duration)
		var cacheDuration time.Duration
		switch cacheKey {
		case "trending_movies", "hot_tv_shows":
			cacheDuration = time.Hour
		case "new_releases":
			cacheDuration = 30 * time.Minute
		default:
			cacheDuration = time.Hour
		}
		return cached.ExpiresAt.Add(-cacheDuration)
	}
	return time.Time{}
}

// FormatUpdateTime formats the update time for display
func (m *TrendingAIManager) FormatUpdateTime(cacheKey string) string {
	updateTime := m.GetCacheUpdateTime(cacheKey)
	if updateTime.IsZero() {
		return "未更新"
	}
	now := time.Now()
	diff := now.Sub(updateTime)

	if diff < time.Minute {
		return "刚刚更新"
	} else if diff < time.Hour {
		return fmt.Sprintf("%d分钟前更新", int(diff.Minutes()))
	} else if diff < 24*time.Hour {
		hours := int(diff.Hours())
		return fmt.Sprintf("%d小时前更新", hours)
	} else {
		days := int(diff.Hours() / 24)
		return fmt.Sprintf("%d天前更新", days)
	}
}

// saveToFile saves cache to file for persistence
func (m *TrendingAIManager) saveToFile() error {
	m.cacheMutex.RLock()
	defer m.cacheMutex.RUnlock()

	data := map[string]interface{}{
		"last_updated": m.lastUpdated.Format(time.RFC3339),
		"caches":       m.jailed,
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	err = os.WriteFile("ai_trending_cache.json", jsonData, 0644)
	if err != nil {
		return err
	}

	log.Printf("[TrendingAI] Cache saved to file")
	return nil
}

// loadFromFile loads cache from file
func (m *TrendingAIManager) loadFromFile() error {
	data, err := os.ReadFile("ai_trending_cache.json")
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("[TrendingAI] No cache file found, starting fresh")
			return nil
		}
		return err
	}

	var savedData struct {
		LastUpdated string                       `json:"last_updated"`
		Caches      map[string]*TrendingCacheItem `json:"caches"`
	}

	if err := json.Unmarshal(data, &savedData); err != nil {
		return err
	}

	// Parse last updated time
	if savedData.LastUpdated != "" {
		if t, err := time.Parse(time.RFC3339, savedData.LastUpdated); err == nil {
			m.lastUpdated = t
		}
	}

	// Restore caches, but only if they haven't expired yet
	now := time.Now()
	for key, cached := range savedData.Caches {
		if now.Before(cached.ExpiresAt) {
			m.jailed[key] = cached
			log.Printf("[TrendingAI] Restored cache: %s (%d items)", key, len(cached.Results))
		}
	}

	log.Printf("[TrendingAI] Cache loaded from file")
	return nil
}

// GetRandomRecommendation gets random recommendations (bypasses cache)
func (m *TrendingAIManager) GetRandomRecommendation(count int, mediaType string) ([]*TrendingResult, error) {
	if !m.IsEnabled() {
		return nil, fmt.Errorf("AI trending not enabled")
	}

	if count > 10 {
		count = 10
	}
	if count < 3 {
		count = 3
	}

	log.Printf("[TrendingAI] Fetching random %s recommendations...", mediaType)

	currentTime := time.Now().Format("2006-01-02 15:04")

	var systemPrompt string
	var userMessage string

	if mediaType == "tv" {
		systemPrompt = fmt.Sprintf(`你是一位专业的影视推荐专家，精通全球剧集。

请推荐 %d 部不同类型的精彩剧集（包括经典作品和冷门佳作）。

返回格式必须是纯 JSON 数组：
[
  {"title": "剧集名", "year": 2024, "genre": "类型", "rating": 8.5, "reason": "推荐理由", "tmdbId": 123, "releaseDate": "2024-01-01", "country": "国家"}
]

要求：
1. 推荐不同类型：悬疑、爱情、科幻、历史、犯罪等
2. 包含不同国家的作品
3. 推荐理由要独特有吸引力
4. 返回纯JSON格式

当前时间：%s`, count, currentTime)
		userMessage = fmt.Sprintf("推荐 %d 部不同类型的精彩剧集", count)
	} else {
		systemPrompt = fmt.Sprintf(`你是一位专业的影视推荐专家，精通全球电影。

请推荐 %d 部不同类型的精彩电影（包括经典作品和冷门佳作）。

返回格式必须是纯 JSON 数组：
[
  {"title": "电影名", "year": 2024, "genre": "类型", "rating": 8.5, "reason": "推荐理由", "tmdbId": 123, "releaseDate": "2024-01-01", "country": "国家"}
]

要求：
1. 推荐不同类型：动作、喜剧、剧情、科幻、恐怖、动画等
2. 包含不同国家和时代的作品
3. 推荐理由要独特有吸引力
4. 返回纯JSON格式

当前时间：%s`, count, currentTime)
		userMessage = fmt.Sprintf("推荐 %d 部不同类型的精彩电影", count)
	}

	response, err := m.zhipu.Send(userMessage, systemPrompt)
	if err != nil {
		log.Printf("[TrendingAI] Error getting random recommendations: %v", err)
		return nil, err
	}

	results, err := m.parseTrendingResults(response, mediaType)
	if err != nil {
		log.Printf("[TrendingAI] Error parsing random recommendations: %v", err)
		return nil, err
	}

	log.Printf("[TrendingAI] Got %d random recommendations from AI", len(results))
	return results, nil
}

// GetCachedResults returns cached results for a specific key if fresh
func (m *TrendingAIManager) GetCachedResults(cacheKey string) []*TrendingResult {
	m.cacheMutex.RLock()
	defer m.cacheMutex.RUnlock()

	if cached, exists := m.jailed[cacheKey]; exists && time.Now().Before(cached.ExpiresAt) {
		return cached.Results
	}
	return nil
}

// MustGetCachedResults returns cached results (even if expired) or nil
func (m *TrendingAIManager) MustGetCachedResults(cacheKey string) []*TrendingResult {
	m.cacheMutex.RLock()
	defer m.cacheMutex.RUnlock()

	if cached, exists := m.jailed[cacheKey]; exists {
		return cached.Results
	}
	return nil
}
