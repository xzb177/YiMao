package handlers

import (
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
	"emby-telegram-bot/pkg/types"
)

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

	// Send results
	h.sendSearchResults(userID, chatID, query, results)
	return nil
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

// getTrendingMoviesHybrid gets trending movies from TMDB and filters by MoviePilot availability
func (h *SearchHandler) getTrendingMoviesHybrid() ([]services.SearchResult, error) {
	if h.tmdb == nil {
		return h.getFallbackMedia()
	}

	tmdbResults, err := h.tmdb.GetTrendingMovies("week")
	if err != nil {
		log.Printf("[SearchHandler] TMDB GetTrendingMovies failed: %v", err)
		return h.getFallbackMedia()
	}

	// Filter through MoviePilot to check availability
	return h.filterTMDBResultsByMoviePilot(tmdbResults.Results, "movie")
}

// getTrendingTVHybrid gets trending TV shows from TMDB and filters by MoviePilot availability
func (h *SearchHandler) getTrendingTVHybrid() ([]services.SearchResult, error) {
	if h.tmdb == nil {
		return h.getFallbackTVMedia()
	}

	tmdbResults, err := h.tmdb.GetTrendingTV("week")
	if err != nil {
		log.Printf("[SearchHandler] TMDB GetTrendingTV failed: %v", err)
		return h.getFallbackTVMedia()
	}

	// Filter through MoviePilot to check availability
	return h.filterTMDBResultsByMoviePilot(tmdbResults.Results, "tv")
}

// getTopRatedMediaHybrid gets top-rated media from TMDB and filters by MoviePilot
func (h *SearchHandler) getTopRatedMediaHybrid() ([]services.SearchResult, error) {
	if h.tmdb == nil {
		return h.getFallbackMedia()
	}

	tmdbResults, err := h.tmdb.GetPopularMovies(1)
	if err != nil {
		log.Printf("[SearchHandler] TMDB GetPopularMovies failed: %v", err)
		return h.getFallbackMedia()
	}

	// Filter and only return high-rated content
	var highRated []services.TMDBTrendingMediaInfo
	for _, item := range tmdbResults.Results {
		if item.VoteAverage >= 7.5 {
			highRated = append(highRated, item)
		}
	}

	return h.filterTMDBResultsByMoviePilot(highRated, "movie")
}

// getNewMediaHybrid gets new releases from TMDB and filters by MoviePilot
func (h *SearchHandler) getNewMediaHybrid() ([]services.SearchResult, error) {
	if h.tmdb == nil {
		return h.getNewMedia()
	}

	tmdbResults, err := h.tmdb.GetTrendingMovies("week")
	if err != nil {
		log.Printf("[SearchHandler] TMDB GetTrendingMovies failed: %v", err)
		return h.getNewMedia()
	}

	// Filter for recent content (last 2 years)
	currentYear := time.Now().Year()
	var recentResults []services.TMDBTrendingMediaInfo

	for _, item := range tmdbResults.Results {
		if item.ReleaseDate != "" {
			year := 0
			fmt.Sscanf(item.ReleaseDate, "%d-", &year)
			if year >= currentYear-2 {
				recentResults = append(recentResults, item)
			}
		}
	}

	return h.filterTMDBResultsByMoviePilot(recentResults, "movie")
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

	for _, kw := range selected {
		results, err := h.moviepilot.SearchMedia(kw, 1)
		if err != nil {
			continue
		}

		items := results.Results
		for _, item := range items {
			if !seen[item.ID] && item.Rating >= 7.5 {
				seen[item.ID] = true
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

	// Recent popular movies
	allKeywords := []string{
		fmt.Sprintf("%d", currentYear),
		fmt.Sprintf("%d", currentYear-1),
		"沙丘2", "奥本海默", "盟约", "惊奇队长", "蜘蛛侠", "蝙蝠侠",
		"黑豹", "奇异博士", "雷神", "黑寡妇", "永恒族", "尚气",
		"侏罗纪世界", "哥斯拉", "金刚", "速度与激情", "碟中谍",
	}

	selected := shuffleStrings(allKeywords, 8)

	var allResults []services.SearchResult
	seen := make(map[int]bool)

	for _, kw := range selected {
		results, err := h.moviepilot.SearchMedia(kw, 1)
		if err != nil {
			continue
		}

		items := results.Results
		for _, item := range items {
			if !seen[item.ID] && item.Year.Int() >= currentYear-2 {
				seen[item.ID] = true
				allResults = append(allResults, item)
				if len(allResults) >= 8 {
					return allResults, nil
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
	seen := make(map[int]bool) // Use TMDB ID for deduplication

	for _, kw := range selectedKeywords {
		results, err := h.moviepilot.SearchMedia(kw, 1)
		if err != nil {
			continue
		}

		items := results.Results
		for _, item := range items {
			if !seen[item.ID] && item.Rating >= 5.0 {
				seen[item.ID] = true
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
	seen := make(map[int]bool) // Use TMDB ID for deduplication

	for _, kw := range selected {
		results, err := h.moviepilot.SearchMedia(kw, 1)
		if err != nil {
			continue
		}

		items := results.Results
		for _, item := range items {
			// Skip non-movie items
			if item.Type != "电影" && item.Type != "MOV" && item.Type != "电影" {
				continue
			}
			if !seen[item.ID] {
				seen[item.ID] = true
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
	seen := make(map[int]bool) // Use TMDB ID for deduplication

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
				seen[item.ID] = true
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
