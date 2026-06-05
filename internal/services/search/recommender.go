// Package search provides recommendation services for movies and TV shows.
package search

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/pkg/logger"
)

// RecommendationCacheEntry represents a cached recommendation result.
type RecommendationCacheEntry struct {
	results   []services.SearchResult
	expiredAt time.Time
}

// RecommendationCache manages recommendation result caching.
type RecommendationCache struct {
	sync.RWMutex
	data map[string]*RecommendationCacheEntry
}

// NewRecommendationCache creates a new recommendation cache.
func NewRecommendationCache() *RecommendationCache {
	return &RecommendationCache{
		data: make(map[string]*RecommendationCacheEntry),
	}
}

// Get retrieves cached recommendations if available and not expired.
func (c *RecommendationCache) Get(key string) ([]services.SearchResult, bool) {
	c.RLock()
	defer c.RUnlock()

	entry, exists := c.data[key]
	if !exists {
		return nil, false
	}

	if time.Now().After(entry.expiredAt) {
		delete(c.data, key)
		return nil, false
	}

	return entry.results, true
}

// Set stores recommendations in cache with expiration.
func (c *RecommendationCache) Set(key string, results []services.SearchResult, ttl time.Duration) {
	c.Lock()
	defer c.Unlock()

	c.data[key] = &RecommendationCacheEntry{
		results:   results,
		expiredAt: time.Now().Add(ttl),
	}
}

const (
	// CacheTTL is the time-to-live for cached recommendations.
	CacheTTL = 5 * time.Minute
	// MaxResults is the maximum number of results to return.
	MaxResults = 8
)

// Recommender provides media recommendation services.
type Recommender struct {
	tmdb  *services.TMDBClient
	mp    *services.MoviePilotClient
	cache *RecommendationCache
}

// NewRecommender creates a new recommender service.
func NewRecommender(tmdb *services.TMDBClient, mp *services.MoviePilotClient) *Recommender {
	return &Recommender{
		tmdb:  tmdb,
		mp:    mp,
		cache: NewRecommendationCache(),
	}
}

// GetRecommendations returns recommendations based on the specified type.
func (r *Recommender) GetRecommendations(recType string) ([]services.SearchResult, error) {
	switch recType {
	case "trending":
		return r.getTrendingMovies()
	case "hot":
		return r.getTrendingTV()
	case "toprated":
		return r.getTopRatedMedia()
	case "new":
		return r.getNewMedia()
	case "random":
		return r.getRandomMedia()
	default:
		return r.getTrendingMovies()
	}
}

// getTrendingMovies gets trending movies from TMDB.
func (r *Recommender) getTrendingMovies() ([]services.SearchResult, error) {
	cacheKey := fmt.Sprintf("popular_movies_%d", time.Now().Unix()/60)
	if cached, found := r.cache.Get(cacheKey); found {
		logger.Info("[Recommender] Using cached popular movies")
		return cached, nil
	}

	if r.tmdb == nil {
		return r.getFallbackMedia()
	}

	var allItems []services.TMDBTrendingMediaInfo
	pages := []int{1, 2, 3}

	for _, page := range pages {
		tmdbResults, err := r.tmdb.GetPopularMovies(page)
		if err != nil {
			logger.Info("[Recommender] TMDB GetPopularMovies page %d failed: %v", page, err)
			continue
		}
		allItems = append(allItems, tmdbResults.Results...)
	}

	if len(allItems) == 0 {
		return r.getFallbackMedia()
	}

	results := r.shuffleAndConvert(allItems, "movie")
	r.cache.Set(cacheKey, results, CacheTTL)
	return results, nil
}

// getTrendingTV gets trending TV shows from TMDB.
func (r *Recommender) getTrendingTV() ([]services.SearchResult, error) {
	cacheKey := fmt.Sprintf("popular_tv_%d", time.Now().Unix()/60)
	if cached, found := r.cache.Get(cacheKey); found {
		logger.Info("[Recommender] Using cached popular TV")
		return cached, nil
	}

	if r.tmdb == nil {
		return r.getFallbackTVMedia()
	}

	var allItems []services.TMDBTrendingMediaInfo
	pages := []int{1, 2, 3}

	for _, page := range pages {
		tmdbResults, err := r.tmdb.GetPopularTV(page)
		if err != nil {
			logger.Info("[Recommender] TMDB GetPopularTV page %d failed: %v", page, err)
			continue
		}
		allItems = append(allItems, tmdbResults.Results...)
	}

	if len(allItems) == 0 {
		return r.getFallbackTVMedia()
	}

	results := r.shuffleAndConvert(allItems, "tv")
	r.cache.Set(cacheKey, results, CacheTTL)
	return results, nil
}

// getTopRatedMedia gets top-rated media from TMDB.
func (r *Recommender) getTopRatedMedia() ([]services.SearchResult, error) {
	cacheKey := fmt.Sprintf("toprated_movies_%d", time.Now().Unix()/60)
	if cached, found := r.cache.Get(cacheKey); found {
		logger.Info("[Recommender] Using cached top-rated movies")
		return cached, nil
	}

	if r.tmdb == nil {
		return r.getFallbackMedia()
	}

	var allItems []services.TMDBTrendingMediaInfo
	pages := []int{1, 2, 3}

	for _, page := range pages {
		tmdbResults, err := r.tmdb.GetTopRatedMovies(page)
		if err != nil {
			logger.Info("[Recommender] TMDB GetTopRatedMovies page %d failed: %v", page, err)
			continue
		}
		allItems = append(allItems, tmdbResults.Results...)
	}

	if len(allItems) == 0 {
		return r.getFallbackMedia()
	}

	results := r.shuffleAndConvert(allItems, "movie")
	r.cache.Set(cacheKey, results, CacheTTL)
	return results, nil
}

// getNewMedia gets new releases from TMDB.
func (r *Recommender) getNewMedia() ([]services.SearchResult, error) {
	cacheKey := fmt.Sprintf("new_movies_%d", time.Now().Unix()/60)
	if cached, found := r.cache.Get(cacheKey); found {
		logger.Info("[Recommender] Using cached new movies")
		return cached, nil
	}

	if r.tmdb == nil {
		return r.getNewMediaFallback()
	}

	tmdbResults, err := r.tmdb.GetNowPlayingMovies(1)
	if err != nil {
		logger.Info("[Recommender] TMDB GetNowPlayingMovies failed: %v", err)
	} else if len(tmdbResults.Results) >= MaxResults {
		shuffled := r.shuffleItems(tmdbResults.Results)
		results := r.convertTMDBToSearchResults(shuffled, "movie")
		r.cache.Set(cacheKey, results, CacheTTL)
		return results, nil
	}

	// Fallback to popular with year filter
	return r.getNewMediaFromYear()
}

// getNewMediaFromYear gets recent media using year-based search.
func (r *Recommender) getNewMediaFromYear() ([]services.SearchResult, error) {
	if r.tmdb == nil {
		return r.getNewMediaFallback()
	}

	var allRecent []services.TMDBTrendingMediaInfo
	currentYear := time.Now().Year()

	for page := 1; page <= 3; page++ {
		tmdbResults, err := r.tmdb.GetPopularMovies(page)
		if err != nil {
			continue
		}

		for _, item := range tmdbResults.Results {
			if item.ReleaseDate != "" {
				year := 0
				fmt.Sscanf(item.ReleaseDate, "%d-", &year)
				if year >= currentYear-3 {
					allRecent = append(allRecent, item)
				}
			}
		}
	}

	if len(allRecent) >= MaxResults {
		results := r.shuffleAndConvert(allRecent, "movie")
		cacheKey := fmt.Sprintf("new_movies_%d", time.Now().Unix()/60)
		r.cache.Set(cacheKey, results, CacheTTL)
		return results, nil
	}

	return r.getNewMediaFallback()
}

// getRandomMedia returns random media recommendations.
func (r *Recommender) getRandomMedia() ([]services.SearchResult, error) {
	categories := [][]string{
		{"科幻", "星际", "未来", "太空", "机器人", "末日"},
		{"动作", "冒险", "特工", "警匪", "战争", "格斗"},
		{"喜剧", "搞笑", "爱情", "浪漫", "家庭", "温馨"},
		{"动画", "动漫", "卡通", "皮克斯", "吉卜力", "迪士尼"},
		{"悬疑", "惊悚", "恐怖", "犯罪", "推理", "侦探"},
		{"奇幻", "魔法", "神话", "传说", "超能", "异能"},
	}

	// Pick 2 random categories
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	selectedCategories := make([]string, 0)
	indices := rnd.Perm(len(categories))[:2]
	for _, idx := range indices {
		selectedCategories = append(selectedCategories, categories[idx]...)
	}

	// Shuffle and pick keywords
	selectedKeywords := shuffleStrings(selectedCategories, 6)

	var allResults []services.SearchResult
	seen := make(map[int]bool)
	seenPrefix := make(map[string]bool)

	for _, kw := range selectedKeywords {
		results, err := r.mp.SearchMedia(kw, 1)
		if err != nil {
			continue
		}

		items := results.Results
		for _, item := range items {
			if item.Rating < 5.0 {
				continue
			}
			if seen[item.ID] {
				continue
			}

			// Extract title prefix to detect series
			prefix := ""
			runes := []rune(item.Title)
			if len(runes) >= 2 {
				prefix = string(runes[:2])
			}

			if prefix != "" && seenPrefix[prefix] {
				continue
			}

			seen[item.ID] = true
			if prefix != "" {
				seenPrefix[prefix] = true
			}
			allResults = append(allResults, item)
			if len(allResults) >= MaxResults {
				return allResults, nil
			}
		}
	}

	if len(allResults) == 0 {
		return r.getFallbackMedia()
	}

	return allResults, nil
}

// getFallbackMedia returns fallback popular media.
func (r *Recommender) getFallbackMedia() ([]services.SearchResult, error) {
	fallbackKeywords := []string{
		"复仇者联盟", "沙丘", "奥本海默", "流浪地球", "阿凡达",
		"泰坦尼克", "黑客帝国", "星际穿越", "盗梦空间", "蝙蝠侠",
	}

	selected := shuffleStrings(fallbackKeywords, 6)
	var allResults []services.SearchResult
	seen := make(map[int]bool)
	seenPrefix := make(map[string]bool)

	for _, kw := range selected {
		results, err := r.mp.SearchMedia(kw, 1)
		if err != nil {
			continue
		}

		items := results.Results
		for _, item := range items {
			// Skip non-movie items
			if item.Type != "电影" && item.Type != "MOV" {
				continue
			}
			if seen[item.ID] {
				continue
			}

			prefix := ""
			runes := []rune(item.Title)
			if len(runes) >= 2 {
				prefix = string(runes[:2])
			}

			if prefix != "" && seenPrefix[prefix] {
				continue
			}

			seen[item.ID] = true
			if prefix != "" {
				seenPrefix[prefix] = true
			}
			allResults = append(allResults, item)
			if len(allResults) >= MaxResults {
				return allResults, nil
			}
		}
	}

	return allResults, nil
}

// getFallbackTVMedia returns fallback TV media.
func (r *Recommender) getFallbackTVMedia() ([]services.SearchResult, error) {
	fallbackKeywords := []string{
		"权力的游戏", "行尸走肉", "绝命毒师", "怪奇物语", "黑镜",
		"纸钞屋", "鱿鱼游戏", "王国", "黑暗", "使女的故事",
	}

	selected := shuffleStrings(fallbackKeywords, 6)
	var allResults []services.SearchResult
	seen := make(map[int]bool)
	seenPrefix := make(map[string]bool)

	for _, kw := range selected {
		results, err := r.mp.SearchMedia(kw, 1)
		if err != nil {
			continue
		}

		items := results.Results
		for _, item := range items {
			if item.Type != "电视剧" && item.Type != "TV" && item.Type != "剧集" {
				continue
			}
			if seen[item.ID] {
				continue
			}

			prefix := ""
			runes := []rune(item.Title)
			if len(runes) >= 2 {
				prefix = string(runes[:2])
			}

			if prefix != "" && seenPrefix[prefix] {
				continue
			}

			seen[item.ID] = true
			if prefix != "" {
				seenPrefix[prefix] = true
			}
			allResults = append(allResults, item)
			if len(allResults) >= MaxResults {
				return allResults, nil
			}
		}
	}

	return allResults, nil
}

// getNewMediaFallback returns new media fallback using keyword search.
func (r *Recommender) getNewMediaFallback() ([]services.SearchResult, error) {
	currentYear := time.Now().Year()

	categories := [][]string{
		{"沙丘2", "奥本海默", "银河护卫队3", "闪电侠", "蚁人与黄蜂女"},
		{"惊奇队长2", "海王2", "蓝甲虫", "闪电侠", "雷霆沙赞"},
		{"碟中谍7", "速度与激情10", "约翰 Wick4", "夺宝奇兵5", "鬼玩人崛起"},
		{"蜘蛛侠纵横宇宙", "超级马力欧", "元素方城市", "忍者神龟", "星愿"},
		{"流浪地球2", "满江红", "无名", "深海", "熊出没"},
		{"邪恶 Nun", "欢迎来到 rifle", "梅根", "恐惧", "微笑"},
	}

	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	var selectedKeywords []string

	for _, category := range categories {
		if len(category) > 0 {
			idx := rnd.Intn(len(category))
			selectedKeywords = append(selectedKeywords, category[idx])
		}
	}

	shuffled := shuffleStrings(selectedKeywords, len(selectedKeywords))
	if len(shuffled) > MaxResults {
		shuffled = shuffled[:MaxResults]
	}

	var allResults []services.SearchResult
	seen := make(map[int]bool)
	seenPrefix := make(map[string]bool)

	for _, kw := range shuffled {
		results, err := r.mp.SearchMedia(kw, 1)
		if err != nil {
			continue
		}

		items := results.Results
		for _, item := range items {
			if seen[item.ID] {
				continue
			}

			prefix := ""
			if len(item.Title) >= 2 {
				runes := []rune(item.Title)
				if len(runes) >= 2 {
					prefix = string(runes[:2])
				}
			}

			if prefix != "" && seenPrefix[prefix] {
				continue
			}

			if item.Year.Int() >= currentYear-3 {
				seen[item.ID] = true
				if prefix != "" {
					seenPrefix[prefix] = true
				}
				allResults = append(allResults, item)
				if len(allResults) >= MaxResults {
					return allResults, nil
				}
			}
		}
	}

	if len(allResults) < 4 {
		for _, kw := range shuffled {
			results, err := r.mp.SearchMedia(kw, 1)
			if err != nil {
				continue
			}

			items := results.Results
			for _, item := range items {
				if !seen[item.ID] && item.Year.Int() >= currentYear-5 {
					seen[item.ID] = true
					allResults = append(allResults, item)
					if len(allResults) >= MaxResults {
						return allResults, nil
					}
				}
			}
		}
	}

	return allResults, nil
}

// shuffleAndConvert shuffles items and converts to search results.
func (r *Recommender) shuffleAndConvert(items []services.TMDBTrendingMediaInfo, mediaType string) []services.SearchResult {
	shuffled := r.shuffleItems(items)
	return r.convertTMDBToSearchResults(shuffled, mediaType)
}

// shuffleItems shuffles a slice of TMDBTrendingMediaInfo.
func (r *Recommender) shuffleItems(items []services.TMDBTrendingMediaInfo) []services.TMDBTrendingMediaInfo {
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	shuffled := make([]services.TMDBTrendingMediaInfo, len(items))
	copy(shuffled, items)
	for i := len(shuffled) - 1; i > 0; i-- {
		j := rnd.Intn(i + 1)
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}
	return shuffled
}

// convertTMDBToSearchResults converts TMDB trending results to search results.
func (r *Recommender) convertTMDBToSearchResults(items []services.TMDBTrendingMediaInfo, mediaType string) []services.SearchResult {
	var results []services.SearchResult
	seen := make(map[int]bool)

	for _, item := range items {
		if seen[item.ID] {
			continue
		}
		seen[item.ID] = true

		year := 0
		if item.ReleaseDate != "" {
			fmt.Sscanf(item.ReleaseDate, "%d-", &year)
		}

		result := services.SearchResult{
			ID:       item.ID,
			Title:    getItemTitle(item),
			Year:     services.FlexibleYear(year),
			Type:     mediaType,
			Poster:   item.PosterPath,
			Rating:   item.VoteAverage,
			Overview: item.Overview,
		}
		results = append(results, result)

		if len(results) >= MaxResults {
			break
		}
	}

	return results
}

// getItemTitle gets title from TMDBTrendingMediaInfo.
func getItemTitle(item services.TMDBTrendingMediaInfo) string {
	if item.Title != "" {
		return item.Title
	}
	return item.Name
}

// shuffleStrings randomly selects n items from slice and shuffles them.
func shuffleStrings(items []string, n int) []string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	shuffled := make([]string, len(items))
	copy(shuffled, items)

	for i := len(shuffled) - 1; i > 0; i-- {
		j := r.Intn(i + 1)
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}

	if n >= len(shuffled) {
		return shuffled
	}
	return shuffled[:n]
}
