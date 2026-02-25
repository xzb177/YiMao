package handlers

import (
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
	"emby-telegram-bot/pkg/metrics"
	"emby-telegram-bot/pkg/types"
)

// Recommendation cache entry
type recommendationCacheEntry struct {
	results    []services.SearchResult
	expiredAt  time.Time
}

// Recommendation cache
type recommendationCache struct {
	sync.RWMutex
	data map[string]*recommendationCacheEntry
}

var recCache = &recommendationCache{
	data: make(map[string]*recommendationCacheEntry),
}

const cacheTTL = 5 * time.Minute

// getFromCache retrieves cached recommendations if available and not expired
func (c *recommendationCache) get(key string) ([]services.SearchResult, bool) {
	c.RLock()
	defer c.RUnlock()

	entry, exists := c.data[key]
	if !exists {
		metrics.RecordCacheMiss("recommendation")
		return nil, false
	}

	if time.Now().After(entry.expiredAt) {
		// Expired, remove it
		delete(c.data, key)
		metrics.RecordCacheMiss("recommendation")
		return nil, false
	}

	metrics.RecordCacheHit("recommendation")
	return entry.results, true
}

// set stores recommendations in cache with expiration
func (c *recommendationCache) set(key string, results []services.SearchResult) {
	c.Lock()
	defer c.Unlock()

	c.data[key] = &recommendationCacheEntry{
		results:   results,
		expiredAt: time.Now().Add(cacheTTL),
	}
}

// SearchHandler handles search callbacks and queries
type SearchHandler struct {
	sessMgr         *session.Manager
	telegram        *services.TelegramClient
	moviepilot      *services.MoviePilotClient
	tmdb            *services.TMDBClient
	searchService   *services.SearchService
	searchHistory   *services.SearchHistoryService
}

func NewSearchHandler(
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	moviepilot *services.MoviePilotClient,
	tmdb *services.TMDBClient,
) *SearchHandler {
	searchSvc := services.NewSearchService(moviepilot, sessMgr)
	return &SearchHandler{
		sessMgr:        sessMgr,
		telegram:       telegram,
		moviepilot:     moviepilot,
		tmdb:           tmdb,
		searchService:  searchSvc,
	}
}

// SetSearchHistory sets the search history service
func (h *SearchHandler) SetSearchHistory(sh *services.SearchHistoryService) {
	h.searchHistory = sh
}

// shuffleStrings randomly selects n items from slice and shuffles them
func shuffleStrings(items []string, n int) []string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Create a copy and shuffle
	shuffled := make([]string, len(items))
	copy(shuffled, items)

	// Fisher-Yates shuffle
	for i := len(shuffled) - 1; i > 0; i-- {
		j := r.Intn(i + 1)
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}

	// Return first n items
	if n >= len(shuffled) {
		return shuffled
	}
	return shuffled[:n]
}

func (h *SearchHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	log.Printf("[SearchHandler] Handle: action=%s, params=%v", ctx.Callback.Action, ctx.Callback.Params)

	// Check if this is a search result selection
	if tmdbIDStr, hasID := ctx.Callback.Params["id"]; hasID {
		return h.handleSelect(ctx, tmdbIDStr)
	}

	// Check if this is a page navigation
	if pageStr, hasPage := ctx.Callback.Params["page"]; hasPage {
		return h.handlePage(ctx, pageStr)
	}

	// Check if this is a trending type selection
	if tType, hasType := ctx.Callback.Params["type"]; hasType {
		return h.handleTrending(ctx, tType)
	}

	// Check if this is a search history query
	if query, hasQuery := ctx.Callback.Params["query"]; hasQuery {
		// Execute search from history
		h.HandleSearchQuery(ctx.UserID, ctx.ChatID, query)
		return &callback.Response{
			CallbackMsg: "搜索中...",
			ShowAlert:   false,
		}, nil
	}

	// Check if clearing history
	if _, hasClear := ctx.Callback.Params["clear_history"]; hasClear {
		log.Printf("[SearchHandler] Clearing search history for user %d", ctx.UserID)
		if h.searchHistory != nil {
			if err := h.searchHistory.ClearHistory(ctx.UserID); err != nil {
				log.Printf("[SearchHandler] Failed to clear history: %v", err)
			}
		}
		kb := services.NewKeyboardBuilder()
		kb.AddButton("⬅️ 返回主菜单", "start")
		log.Printf("[SearchHandler] Returning 'history cleared' message with keyboard")
		return &callback.Response{
			Text:     "🗑️ 搜索历史已清空",
			Edit:     true,
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	// Show search history or prompt
	return h.showSearchHistoryOrPrompt(ctx)
}

// handleSearchQuery handles a text search query
func (h *SearchHandler) HandleSearchQuery(userID int64, chatID int64, query string) error {
	log.Printf("[SearchHandler] Search query: %s", query)

	// Trim whitespace
	query = strings.TrimSpace(query)
	if query == "" {
		// Show search history
		return h.showSearchHistory(userID, chatID)
	}

	// Add to search history
	if h.searchHistory != nil {
		h.searchHistory.AddSearch(userID, query)
	}

	// Perform search
	results, err := h.moviepilot.SearchMedia(query, 1)
	if err != nil {
		log.Printf("[SearchHandler] Search failed: %v", err)
		h.telegram.SendMessage(chatID, fmt.Sprintf("❌ 搜索失败: %v", err), "", nil)
		return err
	}

	// Check for empty results
	if results == nil || len(results.Results) == 0 {
		log.Printf("[SearchHandler] No results found for query: %s", query)
		h.sendNoResultsMessage(chatID, query)
		return nil
	}

	// Send results
	h.sendSearchResults(userID, chatID, query, results)
	return nil
}

// sendNoResultsMessage sends a message when no search results are found
func (h *SearchHandler) sendNoResultsMessage(chatID int64, query string) {
	msg := fmt.Sprintf("🔍 搜索结果「%s」\n\n😔 未搜索到需要的资源\n\n💡 建议：\n• 检查影片名称是否正确\n• 尝试使用更简短的关键词\n• 或使用英文片名搜索", query)
	kb := services.NewKeyboardBuilder()
	kb.AddButton("⬅️ 返回主菜单", "start")
	h.telegram.SendMessage(chatID, msg, "", kb.Build())
}

func (h *SearchHandler) showSearchHistoryOrPrompt(ctx *callback.Context) (*callback.Response, error) {
	msg := services.NewMessageBuilder()
	msg.Bold("🔍 搜索影片").Newline()
	msg.Newline()

	// Show search history if available
	if h.searchHistory != nil {
		history := h.searchHistory.GetHistory(ctx.UserID)
		if len(history) > 0 {
			msg.Text("📜 最近搜索：").Newline()
			msg.Newline()

			// Show up to 5 recent searches
			displayCount := 5
			if len(history) < displayCount {
				displayCount = len(history)
			}

			kb := services.NewKeyboardBuilder()

			for i := 0; i < displayCount; i++ {
				entry := history[i]
				msg.Textf("%d. %s", i+1, entry.Query).Newline()
				// Add button for quick search
				kb.AddButton(fmt.Sprintf("🔎 %s", truncateString(entry.Query, 15)), fmt.Sprintf("search:query:%s", entry.Query))
				// 2 buttons per row
				if (i+1)%2 == 0 {
					kb.NewRow()
				}
			}

			// Add clear history button
			if len(history) > 0 {
				kb.NewRow()
				kb.AddButton("🗑️ 清空历史", "search:clear_history:1")
			}
			kb.NewRow()
			kb.AddButton("⬅️ 返回主菜单", "start")

			msg.Newline()
			msg.Italic("💡 点击按钮快速搜索，或输入新影片名称")

			return &callback.Response{
				Text:     msg.Build(),
				Edit:     true,
				Keyboard: convertKeyboard(kb.Build()),
			}, nil
		}
	}

	// No history, show prompt
	msg.Text("请输入影片名称，支持中文/英文").Newline()
	msg.Newline()
	msg.Italic("💡 输入影片名称后自动搜索")

	kb := services.NewKeyboardBuilder()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// truncateString truncates a string to max length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Simple truncation (for proper Chinese handling, use rune count)
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

func (h *SearchHandler) handleSelect(ctx *callback.Context, tmdbIDStr string) (*callback.Response, error) {
	// Redirect to detail handler - build detail callback data
	detailCallback := fmt.Sprintf("detail:id:%s:type:movie", tmdbIDStr)

	// Parse and delegate to detail handler
	parser := callback.NewParser()
	cb, _ := parser.Parse(detailCallback)

	detailHandler := NewDetailHandler(h.sessMgr, h.telegram, h.moviepilot, h.tmdb)

	// Update the context with the parsed callback
	ctx.Callback = cb
	ctx.Callback.Action = callback.ActionDetail

	return detailHandler.Handle(ctx)
}

func (h *SearchHandler) handlePage(ctx *callback.Context, pageStr string) (*callback.Response, error) {
	page := 1
	fmt.Sscanf(pageStr, "%d", &page)

	msg := services.NewMessageBuilder()
	msg.Bold("📄 分页功能").Newline()
	msg.Newline()
	msg.Textf("第 %d 页功能开发中...", page)

	kb := services.NewKeyboardBuilder()
	kb.AddButton("⬅️ 返回", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

func (h *SearchHandler) handleTrending(ctx *callback.Context, tType string) (*callback.Response, error) {
	log.Printf("[SearchHandler] AI trending request: %s", tType)

	// AI recommendation is only available in private chats
	if ctx.ChatType != "private" {
		return &callback.Response{
			Text:        "⚠️ 推荐功能仅在私聊中可用",
			CallbackMsg: "请私聊使用",
			ShowAlert:   true,
		}, nil
	}

	msg := services.NewMessageBuilder()

	// Get trending recommendations from MoviePilot
	results, err := h.getTrendingResults(tType)
	if err != nil {
		msg.Bold("🎬 精选推荐").Newline()
		msg.Newline()
		msg.Text("😓 推荐服务暂时不可用").Newline()
		msg.Newline()
		msg.Italic("💡 稍后再试或使用搜索功能")

		kb := services.NewKeyboardBuilder()
		kb.AddButton("⬅️ 返回主菜单", "start")

		return &callback.Response{
			Text:     msg.Build(),
			Edit:     true,
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	// Build response with results
	msg.Bold("🎬 精选推荐").Newline()
	msg.Newline()

	title := ""
	subtitle := ""
	switch tType {
	case "trending":
		title = "🔥 本周热门"
		subtitle = "大家都在看的好片"
	case "hot":
		title = "📺 热门剧集"
		subtitle = "追剧必看热门番"
	case "toprated":
		title = "⭐ 必看神作"
		subtitle = "高分经典，不容错过"
	case "new":
		title = "🆕 最新上映"
		subtitle = "刚上线的新鲜内容"
	case "random":
		title = "🎲 随机探索"
		subtitle = "发现未知的精彩"
	default:
		title = "🎬 精选推荐"
		subtitle = "为您推荐优质内容"
	}
	msg.Italic(title).Newline()
	msg.Text(subtitle).Newline()
	msg.Newline()

	if len(results) == 0 {
		msg.Italic("💫 暂时没有找到相关内容").Newline()
		msg.Newline()
		msg.Text("试试其他分类，或许有惊喜哦")

		kb := services.NewKeyboardBuilder()
		kb.AddButton("🔥 热门电影", "search:type:trending")
		kb.AddButton("📺 热播剧集", "search:type:hot")
		kb.NewRow()
		kb.AddButton("⭐ 高分佳作", "search:type:toprated")
		kb.AddButton("🆕 最新上线", "search:type:new")
		kb.NewRow()
		kb.AddButton("🎲 随机发现", "search:type:random")
		kb.NewRow()
		kb.AddButton("⬅️ 返回主菜单", "start")

		return &callback.Response{
			Text:     msg.Build(),
			Edit:     true,
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	// Display results
	displayCount := len(results)
	if displayCount > 8 {
		displayCount = 8
	}

	// Save results to session for detail view
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	searchItems := make([]session.SearchItem, 0, len(results))
	for _, item := range results {
		// Convert Chinese type to English for callback data
		mediaType := "movie"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaType = "tv"
		}

		searchItem := session.SearchItem{
			ID:       fmt.Sprintf("%d", item.ID),
			Title:    item.Title,
			Year:     item.Year.Int(),
			Type:     mediaType, // Use English type for callbacks
			Rating:   item.Rating,
			Poster:   item.Poster,
			Overview: item.Overview,
		}
		searchItems = append(searchItems, searchItem)
	}
	sess.SetSearchResults(searchItems, 1, tType)

	kb := services.NewKeyboardBuilder()
	for i, item := range results[:displayCount] {
		year := ""
		if item.Year > 0 {
			year = fmt.Sprintf(" (%d)", item.Year)
		}

		rating := ""
		if item.Rating > 0 {
			rating = fmt.Sprintf(" ⭐%.1f", item.Rating)
		}

		mediaType := "🎬"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaType = "📺"
		}

		msg.Textf("%d. %s%s%s%s", i+1, item.Title, year, mediaType, rating).Newline()

		// Add button for each item - use English type for callback data
		mediaTypeForCallback := "movie"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaTypeForCallback = "tv"
		}
		kb.AddButton(fmt.Sprintf("%d", i+1), fmt.Sprintf("detail:id:%d:type:%s", item.ID, mediaTypeForCallback))

		// New row every 4 items
		if (i+1)%4 == 0 || i == displayCount-1 {
			kb.NewRow()
		}
	}

	// Add navigation row
	kb.AddButton("🔄 换一批", fmt.Sprintf("search:type:%s", tType))
	kb.AddButton("⬅️ 返回主菜单", "start")
	kb.NewRow()
	kb.AddButton("🤖 其他推荐", "ai")

	// Check if we're returning from detail view (the message is likely a photo)
	// In that case, we need to delete and resend instead of editing
	isReturningFromDetail := ctx.Callback != nil && ctx.Callback.Action == "search" && ctx.Callback.Params["type"] != ""

	if isReturningFromDetail {
		return &callback.Response{
			Text:         msg.Build(),
			Edit:         false,
			DeleteMessage: true,
			Keyboard:     convertKeyboard(kb.Build()),
		}, nil
	}

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// getTrendingResults gets trending results based on type
func (h *SearchHandler) getTrendingResults(tType string) ([]services.SearchResult, error) {
	switch tType {
	case "trending":
		// 热门电影 - 使用 TMDB trending API + MoviePilot 验证
		return h.getTrendingMoviesHybrid()
	case "hot":
		// 热播剧集 - 使用 TMDB trending TV API + MoviePilot 验证
		return h.getTrendingTVHybrid()
	case "toprated":
		return h.getTopRatedMediaHybrid()
	case "new":
		return h.getNewMediaHybrid()
	case "random":
		return h.getRandomMedia()
	default:
		return h.getTrendingMoviesHybrid()
	}
}

// getTrendingMoviesHybrid gets trending movies from TMDB
func (h *SearchHandler) getTrendingMoviesHybrid() ([]services.SearchResult, error) {
	// Check cache first (but use random key to avoid always returning same results)
	cacheKey := fmt.Sprintf("popular_movies_%d", time.Now().Unix()/60) // Cache key changes every minute
	if cached, found := recCache.get(cacheKey); found {
		log.Printf("[SearchHandler] Using cached popular movies")
		return cached, nil
	}

	if h.tmdb == nil {
		return h.getFallbackMedia()
	}

	// Fetch multiple pages and shuffle for variety
	var allItems []services.TMDBTrendingMediaInfo
	pages := []int{1, 2, 3}

	for _, page := range pages {
		tmdbResults, err := h.tmdb.GetPopularMovies(page)
		if err != nil {
			log.Printf("[SearchHandler] TMDB GetPopularMovies page %d failed: %v", page, err)
			continue
		}
		allItems = append(allItems, tmdbResults.Results...)
	}

	if len(allItems) == 0 {
		return h.getFallbackMedia()
	}

	// Shuffle results
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := len(allItems) - 1; i > 0; i-- {
		j := r.Intn(i + 1)
		allItems[i], allItems[j] = allItems[j], allItems[i]
	}

	results := h.convertTMDBToSearchResults(allItems, "movie")

	// Cache the results
	recCache.set(cacheKey, results)

	return results, nil
}

// getTrendingTVHybrid gets trending TV shows from TMDB
func (h *SearchHandler) getTrendingTVHybrid() ([]services.SearchResult, error) {
	// Check cache first
	cacheKey := fmt.Sprintf("popular_tv_%d", time.Now().Unix()/60)
	if cached, found := recCache.get(cacheKey); found {
		log.Printf("[SearchHandler] Using cached popular TV")
		return cached, nil
	}

	if h.tmdb == nil {
		return h.getFallbackTVMedia()
	}

	// Fetch multiple pages and shuffle for variety
	var allItems []services.TMDBTrendingMediaInfo
	pages := []int{1, 2, 3}

	for _, page := range pages {
		tmdbResults, err := h.tmdb.GetPopularTV(page)
		if err != nil {
			log.Printf("[SearchHandler] TMDB GetPopularTV page %d failed: %v", page, err)
			continue
		}
		allItems = append(allItems, tmdbResults.Results...)
	}

	if len(allItems) == 0 {
		return h.getFallbackTVMedia()
	}

	// Shuffle results
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := len(allItems) - 1; i > 0; i-- {
		j := r.Intn(i + 1)
		allItems[i], allItems[j] = allItems[j], allItems[i]
	}

	results := h.convertTMDBToSearchResults(allItems, "tv")

	// Cache the results
	recCache.set(cacheKey, results)

	return results, nil
}

// getTopRatedMediaHybrid gets top-rated media from TMDB
func (h *SearchHandler) getTopRatedMediaHybrid() ([]services.SearchResult, error) {
	// Check cache first
	cacheKey := fmt.Sprintf("toprated_movies_%d", time.Now().Unix()/60)
	if cached, found := recCache.get(cacheKey); found {
		log.Printf("[SearchHandler] Using cached top-rated movies")
		return cached, nil
	}

	if h.tmdb == nil {
		return h.getFallbackMedia()
	}

	// Fetch multiple pages and shuffle for variety
	var allItems []services.TMDBTrendingMediaInfo
	pages := []int{1, 2, 3}

	for _, page := range pages {
		tmdbResults, err := h.tmdb.GetTopRatedMovies(page)
		if err != nil {
			log.Printf("[SearchHandler] TMDB GetTopRatedMovies page %d failed: %v", page, err)
			continue
		}
		allItems = append(allItems, tmdbResults.Results...)
	}

	if len(allItems) == 0 {
		return h.getFallbackMedia()
	}

	// Shuffle results
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := len(allItems) - 1; i > 0; i-- {
		j := r.Intn(i + 1)
		allItems[i], allItems[j] = allItems[j], allItems[i]
	}

	results := h.convertTMDBToSearchResults(allItems, "movie")

	// Cache the results
	recCache.set(cacheKey, results)

	return results, nil
}

// getNewMediaHybrid gets new releases from TMDB
func (h *SearchHandler) getNewMediaHybrid() ([]services.SearchResult, error) {
	// Check cache first
	cacheKey := fmt.Sprintf("new_movies_%d", time.Now().Unix()/60)
	if cached, found := recCache.get(cacheKey); found {
		log.Printf("[SearchHandler] Using cached new movies")
		return cached, nil
	}

	if h.tmdb == nil {
		return h.getNewMedia()
	}

	// Use now playing for new releases
	tmdbResults, err := h.tmdb.GetNowPlayingMovies(1)
	if err != nil {
		log.Printf("[SearchHandler] TMDB GetNowPlayingMovies failed: %v", err)
	} else if len(tmdbResults.Results) >= 8 {
		// Shuffle now playing results
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		shuffled := make([]services.TMDBTrendingMediaInfo, len(tmdbResults.Results))
		copy(shuffled, tmdbResults.Results)
		for i := len(shuffled) - 1; i > 0; i-- {
			j := r.Intn(i + 1)
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		}
		results := h.convertTMDBToSearchResults(shuffled, "movie")
		recCache.set(cacheKey, results)
		return results, nil
	}

	// Fallback to popular with year filter
	var allRecent []services.TMDBTrendingMediaInfo
	currentYear := time.Now().Year()

	// Fetch multiple pages
	for page := 1; page <= 3; page++ {
		tmdbResults, err := h.tmdb.GetPopularMovies(page)
		if err != nil {
			log.Printf("[SearchHandler] TMDB GetPopularMovies page %d failed: %v", page, err)
			continue
		}

		// Filter for recent content (last 3 years)
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

	if len(allRecent) >= 8 {
		// Shuffle recent results
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		for i := len(allRecent) - 1; i > 0; i-- {
			j := r.Intn(i + 1)
			allRecent[i], allRecent[j] = allRecent[j], allRecent[i]
		}
		results := h.convertTMDBToSearchResults(allRecent, "movie")
		recCache.set(cacheKey, results)
		return results, nil
	}

	// Last resort - use fallback
	return h.getNewMedia()
}

// convertTMDBToSearchResults converts TMDB trending results to search results directly
func (h *SearchHandler) convertTMDBToSearchResults(items []services.TMDBTrendingMediaInfo, mediaType string) []services.SearchResult {
	var results []services.SearchResult
	seen := make(map[int]bool)

	for _, item := range items {
		if seen[item.ID] {
			continue
		}
		seen[item.ID] = true

		// Extract year from release date
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

		if len(results) >= 8 {
			break
		}
	}

	return results
}

// filterTMDBResultsByMoviePilot filters TMDB results by checking if they exist in MoviePilot
func (h *SearchHandler) filterTMDBResultsByMoviePilot(items []services.TMDBTrendingMediaInfo, mediaType string) ([]services.SearchResult, error) {
	var validResults []services.SearchResult
	seen := make(map[int]bool)

	for _, item := range items {
		tmdbID := item.ID
		if seen[tmdbID] {
			continue
		}
		seen[tmdbID] = true

		// Check if MoviePilot has this media
		mediaInfo, err := h.moviepilot.GetMediaInfo(tmdbID, services.MediaType(mediaType))
		if err != nil {
			// MoviePilot doesn't have this media, skip it
			log.Printf("[SearchHandler] MoviePilot doesn't have TMDB ID %d (%s), skipping", tmdbID, getItemTitle(item))
			continue
		}

		// Use the original mediaType parameter (English) instead of mediaInfo.Type (Chinese)
		result := services.SearchResult{
			ID:       tmdbID,
			Title:    mediaInfo.Title,
			Year:     mediaInfo.Year,
			Type:     mediaType, // Use English type for callback data
			Poster:   mediaInfo.Poster,
			Rating:   mediaInfo.Rating,
			Overview: mediaInfo.Overview,
		}
		validResults = append(validResults, result)

		if len(validResults) >= 8 {
			break
		}
	}

	// If we got results, return them
	if len(validResults) > 0 {
		log.Printf("[SearchHandler] Filtered %d valid results from TMDB", len(validResults))
		return validResults, nil
	}

	// Fallback if no valid results found
	log.Printf("[SearchHandler] No valid results after filtering, using fallback")
	if mediaType == "tv" {
		return h.getFallbackTVMedia()
	}
	return h.getFallbackMedia()
}

// getItemTitle gets title from TMDBTrendingMediaInfo
func getItemTitle(item services.TMDBTrendingMediaInfo) string {
	if item.Title != "" {
		return item.Title
	}
	return item.Name
}

// getNewMediaFromAPI gets new media using better keywords
func (h *SearchHandler) getNewMediaFromAPI() ([]services.SearchResult, error) {
	// Use year-based search for newer content
	currentYear := time.Now().Year()

	// Try multiple recent year searches
	years := []int{currentYear, currentYear - 1, currentYear - 2}

	var allResults []services.SearchResult
	seen := make(map[int]bool)

	for _, year := range years {
		// Search for content from this year
		yearStr := fmt.Sprintf("%d", year)
		results, err := h.moviepilot.SearchMedia(yearStr, 1)
		if err != nil {
			continue
		}

		for _, item := range results.Results {
			if !seen[item.ID] && item.Year.Int() >= year {
				seen[item.ID] = true
				allResults = append(allResults, item)
				if len(allResults) >= 8 {
					return allResults, nil
				}
			}
		}
	}

	if len(allResults) == 0 {
		return h.getNewMedia()
	}

	return allResults, nil
}

// getTopRatedMedia returns high-rated media
func (h *SearchHandler) getTopRatedMedia() ([]services.SearchResult, error) {
	// High-quality classic and modern films
	allKeywords := []string{
		"肖申克的救赎", "教父", "这个杀手不太冷", "泰坦尼克号", "阿甘正传",
		"楚门的世界", "星际穿越", "千与千寻", "狮子王", "辛德勒的名单",
		"美丽人生", "钢琴家", "触不可及", "三傻大闹宝莱坞", "放牛班的春天",
		"疯狂动物城", "寻梦环游记", "机器人总动员", "飞屋环游记", "玩具总动员",
		"超能陆战队", "头脑特工队", " coco", "你的名字", "天气之子",
	}

	selected := shuffleStrings(allKeywords, 10)

	var allResults []services.SearchResult
	seen := make(map[int]bool)
	seenPrefix := make(map[string]bool) // Track title prefixes to avoid series clustering

	for _, kw := range selected {
		results, err := h.moviepilot.SearchMedia(kw, 1)
		if err != nil {
			continue
		}

		items := results.Results
		for _, item := range items {
			if !seen[item.ID] && item.Rating >= 7.5 {
				// Extract title prefix to detect series
				prefix := ""
				runes := []rune(item.Title)
				if len(runes) >= 2 {
					prefix = string(runes[:2])
				}

				// Skip if we already have something with this prefix
				if prefix != "" && seenPrefix[prefix] {
					continue
				}

				seen[item.ID] = true
				if prefix != "" {
					seenPrefix[prefix] = true
				}
				allResults = append(allResults, item)
				if len(allResults) >= 8 {
					return allResults, nil
				}
			}
		}
	}

	// Fallback to lower rating if not enough results
	if len(allResults) < 4 {
		for _, kw := range selected {
			results, err := h.moviepilot.SearchMedia(kw, 1)
			if err != nil {
				continue
			}

			items := results.Results
			for _, item := range items {
				if !seen[item.ID] && item.Rating >= 6.5 {
					seen[item.ID] = true
					allResults = append(allResults, item)
					if len(allResults) >= 8 {
						return allResults, nil
					}
				}
			}
		}
	}

	return allResults, nil
}

// getNewMedia returns recently added media (fallback)
func (h *SearchHandler) getNewMedia() ([]services.SearchResult, error) {
	currentYear := time.Now().Year()

	// Use specific recent movie titles instead of years to avoid series clustering
	// Grouped by genre to ensure variety
	categories := [][]string{
		// Sci-Fi / Action
		{"沙丘2", "奥本海默", "银河护卫队3", "闪电侠", "蚁人与黄蜂女"},
		// Superhero
		{"惊奇队长2", "海王2", "蓝甲虫", "闪电侠", "雷霆沙赞"},
		// Action/Adventure
		{"碟中谍7", "速度与激情10", "约翰 Wick4", "夺宝奇兵5", "鬼玩人崛起"},
		// Animation
		{"蜘蛛侠纵横宇宙", "超级马力欧", "元素方城市", "忍者神龟", "星愿"},
		// Asian cinema
		{"流浪地球2", "满江红", "无名", "深海", "熊出没"},
		// Horror/Thriller
		{"邪恶 Nun", "欢迎来到 rifle", "梅根", "恐惧", "微笑"},
	}

	// Pick one from each category to ensure variety
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	var selectedKeywords []string

	for _, category := range categories {
		if len(category) > 0 {
			idx := r.Intn(len(category))
			selectedKeywords = append(selectedKeywords, category[idx])
		}
	}

	// Shuffle and pick up to 8
	shuffled := shuffleStrings(selectedKeywords, len(selectedKeywords))
	if len(shuffled) > 8 {
		shuffled = shuffled[:8]
	}

	var allResults []services.SearchResult
	seen := make(map[int]bool)
	seenPrefix := make(map[string]bool) // Track title prefixes to avoid series clustering

	for _, kw := range shuffled {
		results, err := h.moviepilot.SearchMedia(kw, 1)
		if err != nil {
			continue
		}

		items := results.Results
		for _, item := range items {
			// Skip if we already have this TMDB ID
			if seen[item.ID] {
				continue
			}

			// Extract title prefix (first 2-3 chars) to detect series
			// This helps avoid getting 沙丘, 沙丘2, 沙丘3 together
			prefix := ""
			if len(item.Title) >= 2 {
				// For Chinese titles, use first 2 characters
				runes := []rune(item.Title)
				if len(runes) >= 2 {
					prefix = string(runes[:2])
				}
			}

			// Skip if we already have something with this prefix (series clustering)
			if prefix != "" && seenPrefix[prefix] {
				continue
			}

			// Filter by year
			if item.Year.Int() >= currentYear-3 {
				seen[item.ID] = true
				if prefix != "" {
					seenPrefix[prefix] = true
				}
				allResults = append(allResults, item)
				if len(allResults) >= 8 {
					return allResults, nil
				}
			}
		}
	}

	// If we still don't have enough, relax the prefix restriction
	if len(allResults) < 4 {
		for _, kw := range shuffled {
			results, err := h.moviepilot.SearchMedia(kw, 1)
			if err != nil {
				continue
			}

			items := results.Results
			for _, item := range items {
				if !seen[item.ID] && item.Year.Int() >= currentYear-5 {
					seen[item.ID] = true
					allResults = append(allResults, item)
					if len(allResults) >= 8 {
						return allResults, nil
					}
				}
			}
		}
	}

	return allResults, nil
}

// getRandomMedia returns random media recommendations
func (h *SearchHandler) getRandomMedia() ([]services.SearchResult, error) {
	// More diverse random categories with better keywords
	categories := [][]string{
		{"科幻", "星际", "未来", "太空", "机器人", "末日"},
		{"动作", "冒险", "特工", "警匪", "战争", "格斗"},
		{"喜剧", "搞笑", "爱情", "浪漫", "家庭", "温馨"},
		{"动画", "动漫", "卡通", "皮克斯", "吉卜力", "迪士尼"},
		{"悬疑", "惊悚", "恐怖", "犯罪", "推理", "侦探"},
		{"奇幻", "魔法", "神话", "传说", "超能", "异能"},
	}

	// Pick 2 random categories
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	selectedCategories := make([]string, 0)
	indices := r.Perm(len(categories))[:2]
	for _, idx := range indices {
		selectedCategories = append(selectedCategories, categories[idx]...)
	}

	// Shuffle and pick keywords
	selectedKeywords := shuffleStrings(selectedCategories, 6)

	var allResults []services.SearchResult
	seen := make(map[int]bool)
	seenPrefix := make(map[string]bool) // Track title prefixes to avoid series clustering

	for _, kw := range selectedKeywords {
		results, err := h.moviepilot.SearchMedia(kw, 1)
		if err != nil {
			continue
		}

		items := results.Results
		for _, item := range items {
			if !seen[item.ID] && item.Rating >= 5.0 {
				// Extract title prefix to detect series
				prefix := ""
				runes := []rune(item.Title)
				if len(runes) >= 2 {
					prefix = string(runes[:2])
				}

				// Skip if we already have something with this prefix
				if prefix != "" && seenPrefix[prefix] {
					continue
				}

				seen[item.ID] = true
				if prefix != "" {
					seenPrefix[prefix] = true
				}
				allResults = append(allResults, item)
				if len(allResults) >= 8 {
					return allResults, nil
				}
			}
		}
	}

	// Fallback if no results
	if len(allResults) == 0 {
		results, _ := h.getFallbackMedia()
		return results, nil
	}

	return allResults, nil
}

// getFallbackMedia returns fallback popular media
func (h *SearchHandler) getFallbackMedia() ([]services.SearchResult, error) {
	fallbackKeywords := []string{
		"复仇者联盟", "沙丘", "奥本海默", "流浪地球", "阿凡达",
		"泰坦尼克", "黑客帝国", "星际穿越", "盗梦空间", "蝙蝠侠",
	}

	selected := shuffleStrings(fallbackKeywords, 6)
	var allResults []services.SearchResult
	seen := make(map[int]bool)
	seenPrefix := make(map[string]bool) // Track title prefixes to avoid series clustering

	for _, kw := range selected {
		results, err := h.moviepilot.SearchMedia(kw, 1)
		if err != nil {
			continue
		}

		items := results.Results
		for _, item := range items {
			// Skip non-movie items
			if item.Type != "电影" && item.Type != "MOV" {
				continue
			}
			if !seen[item.ID] {
				// Extract title prefix to detect series
				prefix := ""
				runes := []rune(item.Title)
				if len(runes) >= 2 {
					prefix = string(runes[:2])
				}

				// Skip if we already have something with this prefix
				if prefix != "" && seenPrefix[prefix] {
					continue
				}

				seen[item.ID] = true
				if prefix != "" {
					seenPrefix[prefix] = true
				}
				allResults = append(allResults, item)
				if len(allResults) >= 8 {
					return allResults, nil
				}
			}
		}
	}

	return allResults, nil
}

// getFallbackTVMedia returns fallback TV media
func (h *SearchHandler) getFallbackTVMedia() ([]services.SearchResult, error) {
	fallbackKeywords := []string{
		"权力的游戏", "行尸走肉", "绝命毒师", "怪奇物语", "黑镜",
		"纸钞屋", "鱿鱼游戏", "王国", "黑暗", "使女的故事",
	}

	selected := shuffleStrings(fallbackKeywords, 6)
	var allResults []services.SearchResult
	seen := make(map[int]bool)
	seenPrefix := make(map[string]bool) // Track title prefixes to avoid series clustering

	for _, kw := range selected {
		results, err := h.moviepilot.SearchMedia(kw, 1)
		if err != nil {
			continue
		}

		items := results.Results
		for _, item := range items {
			// Skip non-TV items
			if item.Type != "电视剧" && item.Type != "TV" && item.Type != "剧集" {
				continue
			}
			if !seen[item.ID] {
				// Extract title prefix to detect series
				prefix := ""
				runes := []rune(item.Title)
				if len(runes) >= 2 {
					prefix = string(runes[:2])
				}

				// Skip if we already have something with this prefix
				if prefix != "" && seenPrefix[prefix] {
					continue
				}

				seen[item.ID] = true
				if prefix != "" {
					seenPrefix[prefix] = true
				}
				allResults = append(allResults, item)
				if len(allResults) >= 8 {
					return allResults, nil
				}
			}
		}
	}

	return allResults, nil
}

// sendSearchResults sends search results to user
func (h *SearchHandler) sendSearchResults(userID int64, chatID int64, query string, results *services.SearchResponse) {
	text := fmt.Sprintf("🔍 搜索结果「%s」\n\n找到 %d 条结果\n\n",
		query, len(results.Results))

	// Build keyboard with results
	var keyboardRows [][]types.TelegramInlineKeyboardButton
	var row []types.TelegramInlineKeyboardButton

	for i, item := range results.Results {
		if i >= 8 {
			break
		}

		year := ""
		if item.Year > 0 {
			year = fmt.Sprintf("%d", item.Year)
		}

		rating := ""
		if item.Rating > 0 {
			rating = fmt.Sprintf(" ⭐%.1f", item.Rating)
		}

		mediaType := "🎬 电影"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaType = "📺 剧集"
		}
		text += fmt.Sprintf("%d. %s (%s) %s%s\n", i+1, item.Title, year, mediaType, rating)

		// Convert Chinese type to English for callback data
		mediaTypeForCallback := "movie"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaTypeForCallback = "tv"
		}

		row = append(row, types.TelegramInlineKeyboardButton{
			Text:         fmt.Sprintf("%d", i+1),
			CallbackData: fmt.Sprintf("select:id:%d:type:%s", item.ID, mediaTypeForCallback),
		})

		if len(row) == 4 {
			keyboardRows = append(keyboardRows, row)
			row = []types.TelegramInlineKeyboardButton{}
		}
	}

	if len(row) > 0 {
		keyboardRows = append(keyboardRows, row)
	}

	// Save search results to session
	sess := h.sessMgr.GetOrCreate(userID)
	searchItems := make([]session.SearchItem, 0, len(results.Results))
	if len(results.Results) > 8 {
		searchItems = make([]session.SearchItem, 8)
	}
	for i, item := range results.Results {
		if i >= 8 {
			break
		}
		// Convert Chinese type to English for callback data
		mediaType := "movie"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaType = "tv"
		}
		searchItems[i] = session.SearchItem{
			ID:       fmt.Sprintf("%d", item.ID),
			Title:    item.Title,
			Year:     item.Year.Int(),
			Type:     mediaType, // Use English type for callbacks
			Rating:   item.Rating,
			Poster:   item.Poster,
			Overview: item.Overview,
		}
	}
	sess.SetSearchResults(searchItems, 1, query)

	navRow := []types.TelegramInlineKeyboardButton{
		{Text: "⬅️ 返回主菜单", CallbackData: "start"},
	}
	if len(results.Results) >= 20 {
		navRow = append(navRow, types.TelegramInlineKeyboardButton{
			Text:         "➡️ 下一页",
			CallbackData: fmt.Sprintf("search:query:%s:page:2", query),
		})
	}
	keyboardRows = append(keyboardRows, navRow)

	keyboard := &types.TelegramInlineKeyboard{
		InlineKeyboard: keyboardRows,
	}

	h.telegram.SendMessage(chatID, text, "", keyboard)
}

func (h *SearchHandler) showSearchHistory(userID int64, chatID int64) error {
	if h.searchHistory == nil {
		return nil
	}

	history := h.searchHistory.GetHistory(userID)
	if len(history) == 0 {
		return nil
	}

	msg := services.NewMessageBuilder()
	msg.Bold("📜 搜索历史").Newline()
	msg.Newline()

	for i, item := range history {
		if i >= 10 {
			break
		}
		msg.Textf("%d. %s", i+1, item).Newline()
	}

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🗑️ 清空历史", "search:clear_history:1")
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	h.telegram.SendMessage(chatID, msg.Build(), "", kb.Build())
	return nil
}
