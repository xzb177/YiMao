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
		if h.searchHistory != nil {
			h.searchHistory.ClearHistory(ctx.UserID)
		}
		kb := services.NewKeyboardBuilder()
		kb.AddButton("⬅️ 返回主菜单", "start")
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
				kb.AddButton("🗑️ 清空历史", "search:clear_history")
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
			Text:        "⚠️ AI 推荐功能仅在私聊中可用",
			CallbackMsg: "请私聊使用",
			ShowAlert:   true,
		}, nil
	}

	msg := services.NewMessageBuilder()

	// Get trending recommendations from MoviePilot
	results, err := h.getTrendingResults(tType)
	if err != nil {
		msg.Bold("🤖 AI 推荐").Newline()
		msg.Newline()
		msg.Text("抱歉，暂时无法获取推荐内容。").Newline()
		msg.Newline()
		msg.Italic("💡 请稍后再试或使用搜索功能")

		kb := services.NewKeyboardBuilder()
		kb.AddButton("⬅️ 返回主菜单", "start")

		return &callback.Response{
			Text:     msg.Build(),
			Edit:     true,
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	// Build response with results
	msg.Bold("🤖 AI 推荐").Newline()
	msg.Newline()

	title := ""
	switch tType {
	case "trending":
		title = "🔥 热门电影"
	case "hot":
		title = "📺 热播剧集"
	case "toprated":
		title = "⭐ 高分佳作"
	case "new":
		title = "🆕 最新上线"
	case "random":
		title = "🎲 随机发现"
	default:
		title = "🤖 AI 推荐"
	}
	msg.Italic(title).Newline()
	msg.Newline()

	if len(results) == 0 {
		msg.Text("暂无推荐内容").Newline()
		msg.Newline()
		msg.Italic("💡 试试其他推荐类型")

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
		searchItem := session.SearchItem{
			ID:     fmt.Sprintf("%d", item.ID),
			Title:  item.Title,
			Year:   item.Year.Int(),
			Type:   string(item.Type),
			Rating: item.Rating,
			Poster: item.Poster,
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

		// Add button for each item - use detail callback instead of select
		kb.AddButton(fmt.Sprintf("%d", i+1), fmt.Sprintf("detail:id:%d:type:%s", item.ID, item.Type))

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
	case "trending", "hot":
		// Get recent requests and find popular ones
		return h.getPopularMedia()
	case "toprated":
		return h.getTopRatedMedia()
	case "new":
		return h.getNewMedia()
	case "random":
		return h.getRandomMedia()
	default:
		return h.getPopularMedia()
	}
}

// getPopularMedia returns popular media from recent requests
func (h *SearchHandler) getPopularMedia() ([]services.SearchResult, error) {
	// Expanded keywords list for variety
	allKeywords := []string{
		"复仇者联盟", "沙丘", "奥本海默", "流浪地球", "阿凡达", "泰坦尼克", "黑客帝国", "教父", "权力的游戏",
		"星际穿越", "盗梦空间", "蝙蝠侠", "蜘蛛侠", "钢铁侠", "雷神", "美国队长", "黑寡妇",
		"神奇女侠", "海王", "闪电侠", "绿灯侠", "金刚狼", "死侍", "银河护卫队",
	}

	// Randomly select and shuffle keywords
	selected := shuffleStrings(allKeywords, 6)

	var allResults []services.SearchResult
	seen := make(map[string]bool)

	for _, kw := range selected {
		results, err := h.moviepilot.SearchMedia(kw, 1)
		if err != nil {
			log.Printf("[SearchHandler] Search for '%s' failed: %v", kw, err)
			continue
		}

		items := results.Results
		for _, item := range items {
			if !seen[item.Title] {
				seen[item.Title] = true
				allResults = append(allResults, item)
				if len(allResults) >= 8 {
					break
				}
			}
		}
		if len(allResults) >= 8 {
			break
		}
	}

	if len(allResults) == 0 {
		// Fallback to empty
		return []services.SearchResult{}, nil
	}

	return allResults, nil
}

// getTopRatedMedia returns high-rated media
func (h *SearchHandler) getTopRatedMedia() ([]services.SearchResult, error) {
	// Expanded keywords list
	allKeywords := []string{
		"肖申克的救赎", "教父", "这个杀手不太冷", "泰坦尼克号", "阿甘正传", "楚门的世界", "星际穿越", "千与千寻",
		"狮子王", "辛德勒的名单", "美丽人生", "钢琴家", "触不可及", "三傻大闹宝莱坞", "放牛班的春天",
		"疯狂动物城", "寻梦环游记", "机器人总动员", "飞屋环游记", "玩具总动员", "超能陆战队",
	}

	selected := shuffleStrings(allKeywords, 8)

	var allResults []services.SearchResult
	seen := make(map[string]bool)

	for _, kw := range selected {
		results, err := h.moviepilot.SearchMedia(kw, 1)
		if err != nil {
			continue
		}

		items := results.Results
		for _, item := range items {
			if !seen[item.Title] && item.Rating >= 7.0 {
				seen[item.Title] = true
				allResults = append(allResults, item)
				if len(allResults) >= 8 {
					break
				}
			}
		}
		if len(allResults) >= 8 {
			break
		}
	}

	return allResults, nil
}

// getNewMedia returns recently added media
func (h *SearchHandler) getNewMedia() ([]services.SearchResult, error) {
	// Expanded keywords list
	allKeywords := []string{
		"沙丘2", "奥本海默", "盟约", "惊奇队长", "蜘蛛侠", "蝙蝠侠", "黑豹", "奇异博士2",
		"雷神4", "黑寡妇", "永恒族", "尚气", "蜘蛛侠：英雄无归", "奇异博士2", "雷神4：爱与雷霆",
		"侏罗纪世界3", "侏罗纪世界：统治", "哥斯拉大战金刚", "金刚大战哥斯拉",
	}

	selected := shuffleStrings(allKeywords, 6)

	var allResults []services.SearchResult
	seen := make(map[string]bool)

	for _, kw := range selected {
		results, err := h.moviepilot.SearchMedia(kw, 1)
		if err != nil {
			continue
		}

		items := results.Results
		for _, item := range items {
			if !seen[item.Title] {
				seen[item.Title] = true
				allResults = append(allResults, item)
				if len(allResults) >= 8 {
					break
				}
			}
		}
		if len(allResults) >= 8 {
			break
		}
	}

	return allResults, nil
}

// getRandomMedia returns random media recommendations
func (h *SearchHandler) getRandomMedia() ([]services.SearchResult, error) {
	// Random keywords for variety
	keywords := []string{"科幻", "动作", "喜剧", "动画", "悬疑", "恐怖", "爱情", "冒险"}

	// Pick 3 random keywords
	selected := make([]string, 0)
	for i := 0; i < 3; i++ {
		idx := len(keywords) * (i + 1) / 3
		selected = append(selected, keywords[idx])
	}

	var allResults []services.SearchResult
	seen := make(map[string]bool)

	for _, kw := range selected {
		results, err := h.moviepilot.SearchMedia(kw, 1)
		if err != nil {
			continue
		}

		items := results.Results
		for _, item := range items {
			if !seen[item.Title] {
				seen[item.Title] = true
				allResults = append(allResults, item)
				if len(allResults) >= 8 {
					break
				}
			}
		}
		if len(allResults) >= 8 {
			break
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

		row = append(row, types.TelegramInlineKeyboardButton{
			Text:         fmt.Sprintf("%d", i+1),
			CallbackData: fmt.Sprintf("select:id:%d:type:%s", item.ID, item.Type),
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
		searchItems[i] = session.SearchItem{
			ID:     fmt.Sprintf("%d", item.ID),
			Title:  item.Title,
			Year:   item.Year.Int(),
			Type:   string(item.Type),
			Rating: item.Rating,
			Poster: item.Poster,
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
	kb.AddButton("🗑️ 清空历史", "search:clear_history")
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	h.telegram.SendMessage(chatID, msg.Build(), "", kb.Build())
	return nil
}
