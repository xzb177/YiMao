package handlers

import (
	"fmt"
	"strings"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
	searchsvc "github.com/xzb177/yimao/internal/services/search"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
)

// SearchHandler handles search callbacks and queries
type SearchHandler struct {
	fallbackService *services.SearchFallbackService
	sessMgr         *session.Manager
	telegram        *services.TelegramClient
	moviepilot      *services.MoviePilotClient
	tmdb            *services.TMDBClient
	searchService   *services.SearchService
	searchHistory   *services.SearchHistoryService
	searchHistoryDB *services.SearchHistoryDB
	recommender     *searchsvc.Recommender
	aiRecommender   *searchsvc.AIRecommender
}

func NewSearchHandler(
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	moviepilot *services.MoviePilotClient,
	tmdb *services.TMDBClient,
) *SearchHandler {
	searchSvc := services.NewSearchService(moviepilot, sessMgr)
	searchSvc.SetTMDBClient(tmdb)
	fallbackSvc := services.NewSearchFallbackService(moviepilot)
	recommender := searchsvc.NewRecommender(tmdb, moviepilot)
	aiRecommender := searchsvc.NewAIRecommender(tmdb, moviepilot, recommender)

	return &SearchHandler{
		fallbackService: fallbackSvc,
		sessMgr:         sessMgr,
		telegram:        telegram,
		moviepilot:      moviepilot,
		tmdb:            tmdb,
		searchService:   searchSvc,
		recommender:     recommender,
		aiRecommender:   aiRecommender,
	}
}

// SetSearchHistory sets the search history service
func (h *SearchHandler) SetSearchHistory(sh *services.SearchHistoryService) {
	h.searchHistory = sh
}

// SetSearchHistoryDB sets the search history database service
func (h *SearchHandler) SetSearchHistoryDB(db *services.SearchHistoryDB) {
	h.searchHistoryDB = db
}

func (h *SearchHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	logger.Info("[SearchHandler] Handle: action=%s, params=%v", ctx.Callback.Action, ctx.Callback.Params)

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
		// Check if this is a mood-based recommendation
		if mood, hasMood := ctx.Callback.Params["mood"]; hasMood {
			return h.handleMoodRecommendation(ctx, tType, mood)
		}
		return h.handleTrending(ctx, tType)
	}

	// Check if this is a search history query
	if query, hasQuery := ctx.Callback.Params["query"]; hasQuery {
		logger.Info("[SearchHandler] Quick search from history: query=%s", query)
		h.HandleSearchQuery(ctx.UserID, ctx.ChatID, query)
		return &callback.Response{
			CallbackMsg: "搜索中...",
			ShowAlert:   false,
		}, nil
	}

	// Check if clearing history
	if _, hasClear := ctx.Callback.Params["clear_history"]; hasClear {
		logger.Info("[SearchHandler] Clearing search history for user %d", ctx.UserID)
		if h.searchHistory != nil {
			if err := h.searchHistory.ClearHistory(ctx.UserID); err != nil {
				logger.Info("[SearchHandler] Failed to clear history: %v", err)
			}
		}
		kb := services.NewKeyboardBuilder()
		kb.AddButton("⬅️ 返回主菜单", "start")
		return &callback.Response{
			Text:     "🗑️ 搜索历史已清空",
			Edit:     true,
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	return h.showSearchHistoryOrPrompt(ctx)
}

// HandleSearchQuery handles a text search query
func (h *SearchHandler) HandleSearchQuery(userID int64, chatID int64, query string) error {
	logger.Info("[SearchHandler] Search query: %s", query)

	query = strings.TrimSpace(query)
	if query == "" {
		return h.showSearchHistory(userID, chatID)
	}

	// Add to search history - prefer DB version (new), fallback to legacy
	if h.searchHistoryDB != nil {
		h.searchHistoryDB.AddSearch(userID, query)
		logger.Info("[SearchHandler] Search added to SearchHistoryDB: userID=%d, query=%s", userID, query)
	} else if h.searchHistory != nil {
		h.searchHistory.AddSearch(userID, query)
		logger.Info("[SearchHandler] Search added to SearchHistory (legacy): userID=%d, query=%s", userID, query)
	} else {
		logger.Info("[SearchHandler] WARNING: No search history service available, query not saved: %s", query)
	}

	// Perform search
	results, err := h.moviepilot.SearchMedia(query, 1)
	if err != nil {
		logger.Info("[SearchHandler] Search failed: %v", err)
		h.telegram.SendMessage(chatID, "❌ 搜索服务暂时不可用，请稍后再试", "", nil)
		return err
	}

	if results == nil || results.Results == nil {
		h.sendNoResultsMessage(userID, chatID, query)
		return nil
	}

	if len(results.Results) == 0 {
		fallbackResults, fallbackQuery, fbErr := h.trySearchFallback(query)
		if fbErr != nil {
			logger.Info("[SearchHandler] Fallback search failed: %v", fbErr)
		}
		if fallbackResults != nil && len(fallbackResults) > 0 {
			h.sendSearchResults(userID, chatID, fallbackQuery, &services.SearchResponse{Results: fallbackResults})
			h.telegram.SendMessage(chatID, fmt.Sprintf("💡 已为你启用兜底搜索：%s", fallbackQuery), "", nil)
			return nil
		}
		h.sendNoResultsMessage(userID, chatID, query)
		return nil
	}

	h.sendSearchResults(userID, chatID, query, results)
	return nil
}

func (h *SearchHandler) showSearchHistoryOrPrompt(ctx *callback.Context) (*callback.Response, error) {
	msg := services.NewMessageBuilder()
	msg.Bold("🔍 搜影片").Newline()
	msg.Newline()
	msg.Text("把片名发给我就行").Newline()
	msg.Newline()
	msg.Text("中英文、电影剧集都能搜").Newline()
	msg.Newline()
	msg.Italic("💡 直接发片名，不用加命令")

	kb := services.NewKeyboardBuilder()
	kb.AddButton("📊 历史记录", "search_history_menu")
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:      msg.Build(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
		ParseMode: "HTML",
	}, nil
}

func (h *SearchHandler) handleSelect(ctx *callback.Context, tmdbIDStr string) (*callback.Response, error) {
	mediaType := "movie"
	if typeStr, hasType := ctx.Callback.Params["type"]; hasType {
		mediaType = typeStr
	}

	logger.Info("[SearchHandler] handleSelect: id=%s, type=%s", tmdbIDStr, mediaType)

	detailCallback := fmt.Sprintf("detail:id:%s:type:%s", tmdbIDStr, mediaType)
	parser := callback.NewParser()
	cb, err := parser.Parse(detailCallback)
	if err != nil {
		logger.Info("[SearchHandler] Failed to parse detail callback: %v", err)
		return &callback.Response{
			Text: "❌ 操作失败",
			Edit: true,
		}, nil
	}

	detailHandler := NewDetailHandler(h.sessMgr, h.telegram, h.moviepilot, h.tmdb)
	ctx.Callback = cb
	ctx.Callback.Action = callback.ActionDetail

	return detailHandler.Handle(ctx)
}

func (h *SearchHandler) handlePage(ctx *callback.Context, pageStr string) (*callback.Response, error) {
	page := 1
	fmt.Sscanf(pageStr, "%d", &page)

	// 从 session 读取之前的搜索上下文（query 存在 session 里，不走 callback data，避免 64 字节超限）
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	_, _, query, ok := sess.GetSearchResults()
	if !ok || query == "" {
		return &callback.Response{
			Text:        "⚠️ 搜索会话已过期，请重新输入片名",
			CallbackMsg: "会话过期",
			ShowAlert:   true,
		}, nil
	}

	// 重新搜索并展示指定页
	h.HandleSearchQuery(ctx.UserID, ctx.ChatID, query)
	return &callback.Response{CallbackMsg: fmt.Sprintf("第 %d 页", page)}, nil
}

func (h *SearchHandler) handleTrending(ctx *callback.Context, tType string) (*callback.Response, error) {
	logger.Info("[SearchHandler] Recommendation request: %s", tType)

	// 给用户即时反馈，避免连续狂点触发限流
	_ = h.telegram.AnswerCallback(ctx.CallbackID, "✨ 正在为你精选...", false)

	if ctx.ChatType != "private" {
		return &callback.Response{
			Text:        "⚠️ 推荐功能仅在私聊中可用",
			CallbackMsg: "请私聊使用",
			ShowAlert:   true,
		}, nil
	}

	msg := services.NewMessageBuilder()

	results, err := h.recommender.GetRecommendations(tType)
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

	return h.buildRecommendationResponse(results, tType, ctx.Callback, ctx.UserID), nil
}

func (h *SearchHandler) handleMoodRecommendation(ctx *callback.Context, recType, mood string) (*callback.Response, error) {
	logger.Info("[SearchHandler] Mood recommendation: type=%s, mood=%s", recType, mood)

	if ctx.ChatType != "private" {
		return &callback.Response{
			Text:        "⚠️ AI 推荐功能仅在私聊中可用",
			CallbackMsg: "请私聊使用",
			ShowAlert:   true,
		}, nil
	}

	msg := services.NewMessageBuilder()

	moodKeyword := searchsvc.MapMoodKeyword(mood)
	results, err := h.aiRecommender.GetMoodRecommendations(moodKeyword, 6)
	if err != nil {
		logger.Info("[SearchHandler] AI recommendation failed: %v", err)
		msg.Bold("🤖 AI 心情推荐").Newline()
		msg.Newline()
		msg.Text("😓 AI 推荐服务暂时不可用").Newline()
		msg.Newline()
		msg.Textf("💡 已为你切换到普通推荐").Newline()

		// Fallback to regular trending
		return h.handleTrending(ctx, recType)
	}

	msg.Bold("🤖 AI 心情推荐").Newline()
	msg.Newline()

	moodLabel := searchsvc.GetMoodLabel(moodKeyword)
	msg.Italic(moodLabel).Newline()
	msg.Text("根据你的心情智能推荐").Newline()
	msg.Newline()

	if len(results) == 0 {
		msg.Italic("💫 暂时没有找到相关内容").Newline()
		msg.Newline()
		msg.Text("试试其他心情，或许有惊喜哦")

		kb := services.NewKeyboardBuilder()
		kb.AddButton("😌 解压轻松", "search:type:hot:mood:relax")
		kb.AddButton("🤯 烧脑刺激", "search:type:toprated:mood:mindblow")
		kb.NewRow()
		kb.AddButton("😭 情绪共鸣", "search:type:trending:mood:emotional")
		kb.AddButton("🧘 治愈慢节奏", "search:type:new:mood:healing")
		kb.NewRow()
		kb.AddButton("⬅️ 返回主菜单", "start")

		return &callback.Response{
			Text:     msg.Build(),
			Edit:     true,
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	return h.buildMoodRecommendationResponse(results, recType, mood, moodLabel, ctx.UserID), nil
}

func (h *SearchHandler) buildRecommendationResponse(results []services.SearchResult, recType string, cb *callback.Callback, userID int64) *callback.Response {
	msg := services.NewMessageBuilder()
	msg.Bold("🎬 精选推荐").Newline()
	msg.Newline()

	title, subtitle := h.getRecommendationTitle(recType)
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
		}
	}

	displayCount := len(results)
	if displayCount > 8 {
		displayCount = 8
	}

	// Store recommendation results in session for detail view
	sess := h.sessMgr.GetOrCreate(userID)
	searchItems := make([]session.SearchItem, 0, displayCount)
	for _, item := range results[:displayCount] {
		mediaType := "movie"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaType = "tv"
		}
		searchItems = append(searchItems, session.SearchItem{
			ID:       fmt.Sprintf("%d", item.ID),
			Title:    item.Title,
			Year:     item.Year.Int(),
			Type:     mediaType,
			Rating:   item.Rating,
			Poster:   item.Poster,
			Overview: item.Overview,
		})
	}
	sess.SetSearchResults(searchItems, 1, recType)
	logger.Info("[SearchHandler] Stored %d recommendation results in session for user %d", len(searchItems), userID)

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

		mediaTypeForCallback := "movie"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaTypeForCallback = "tv"
		}
		kb.AddButton(fmt.Sprintf("%d", i+1), fmt.Sprintf("detail:id:%d:type:%s", item.ID, mediaTypeForCallback))

		if (i+1)%4 == 0 || i == displayCount-1 {
			kb.NewRow()
		}
	}

	kb.AddButton("🔄 换一批", fmt.Sprintf("search:type:%s", recType))
	kb.AddButton("⬅️ 返回主菜单", "start")
	kb.NewRow()
	kb.AddButton("🤖 其他推荐", "ai")

	isReturningFromDetail := cb != nil && cb.Action == "search" && cb.Params["type"] != ""

	if isReturningFromDetail {
		return &callback.Response{
			Text:          msg.Build(),
			Edit:          false,
			DeleteMessage: true,
			Keyboard:      convertKeyboard(kb.Build()),
		}
	}

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}
}

func (h *SearchHandler) buildMoodRecommendationResponse(results []services.SearchResult, recType, mood, moodLabel string, userID int64) *callback.Response {
	msg := services.NewMessageBuilder()
	msg.Bold("🤖 AI 心情推荐").Newline()
	msg.Newline()
	msg.Italic(moodLabel).Newline()
	msg.Text("根据你的心情智能推荐").Newline()
	msg.Newline()

	displayCount := len(results)
	if displayCount > 6 {
		displayCount = 6
	}

	// Store recommendation results in session for detail view
	sess := h.sessMgr.GetOrCreate(userID)
	searchItems := make([]session.SearchItem, 0, displayCount)
	for _, item := range results[:displayCount] {
		mediaType := "movie"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaType = "tv"
		}
		searchItems = append(searchItems, session.SearchItem{
			ID:       fmt.Sprintf("%d", item.ID),
			Title:    item.Title,
			Year:     item.Year.Int(),
			Type:     mediaType,
			Rating:   item.Rating,
			Poster:   item.Poster,
			Overview: item.Overview,
		})
	}
	sess.SetSearchResults(searchItems, 1, fmt.Sprintf("mood_%s", mood))
	logger.Info("[SearchHandler] Stored %d mood recommendation results in session for user %d", len(searchItems), userID)

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

		mediaTypeForCallback := "movie"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaTypeForCallback = "tv"
		}
		kb.AddButton(fmt.Sprintf("%d", i+1), fmt.Sprintf("detail:id:%d:type:%s", item.ID, mediaTypeForCallback))

		if (i+1)%3 == 0 || i == displayCount-1 {
			kb.NewRow()
		}
	}

	kb.AddButton("🔄 换一批", fmt.Sprintf("search:type:%s:mood:%s", recType, mood))
	kb.AddButton("💫 换个心情", "mood")
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}
}

func (h *SearchHandler) getRecommendationTitle(recType string) (title, subtitle string) {
	switch recType {
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
	return
}

func (h *SearchHandler) sendNoResultsMessage(userID int64, chatID int64, query string) {
	msg := fmt.Sprintf("🔍 搜索结果「%s」\n\n😕 未找到相关内容\n\n💡 建议：\n• 检查拼写是否正确\n• 尝试使用更简短的关键词\n• 尝试使用英文搜索", query)
	kb := services.NewKeyboardBuilder()
	// #1 搜索无结果 → 弹「🌟 加入许愿池」按钮。
	// 片名可能超长 / 含特殊字符，直接塞 callback_data 会撞 TG 64 字节上限，
	// 因此把片名暂存到 session（按 userID 存），回调串只用固定的 "wish_add"，
	// 由 WishHandler 用 ctx.UserID 从同一 session 取词。
	// 仅当 sessMgr 可用且片名非空时提供按钮（否则取不到词，避免点了报错的死按钮）。
	if h.sessMgr != nil && strings.TrimSpace(query) != "" {
		sess := h.sessMgr.GetOrCreate(userID)
		sess.Set("pending_wish_query", query)
		kb.AddButton("🌟 加入许愿池", "wish_add")
		kb.NewRow()
	}
	kb.AddButton("⬅️ 返回主菜单", "start")
	h.telegram.SendMessage(chatID, msg, "", kb.Build())
}

func (h *SearchHandler) trySearchFallback(query string) ([]services.SearchResult, string, error) {
	return h.fallbackService.TryFallback(query)
}

func (h *SearchHandler) sendSearchResults(userID int64, chatID int64, query string, results *services.SearchResponse) {
	text := fmt.Sprintf("🔍 搜索结果「%s」\n\n找到 %d 条结果\n\n",
		query, len(results.Results))

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
		mediaType := "movie"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaType = "tv"
		}
		searchItems[i] = session.SearchItem{
			ID:       fmt.Sprintf("%d", item.ID),
			Title:    item.Title,
			Year:     item.Year.Int(),
			Type:     mediaType,
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
			CallbackData: "search:page:2",
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
		msg.Textf("%d. %s", i+1, item.Query).Newline()
	}

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🗑️ 清空历史", "search:clear_history:1")
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	h.telegram.SendMessage(chatID, msg.Build(), "", kb.Build())
	return nil
}
