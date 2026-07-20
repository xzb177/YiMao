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

	return &SearchHandler{
		fallbackService: fallbackSvc,
		sessMgr:         sessMgr,
		telegram:        telegram,
		moviepilot:      moviepilot,
		tmdb:            tmdb,
		searchService:   searchSvc,
		recommender:     recommender,
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

	// Check if this is a popular search query (by index)
	if idxStr, hasPop := ctx.Callback.Params["pop"]; hasPop {
		var idx int
		fmt.Sscanf(idxStr, "%d", &idx)
		query := h.getPopularQuery(idx)
		if query != "" {
			logger.Info("[SearchHandler] Popular search: idx=%d query=%s", idx, query)
			h.HandleSearchQuery(ctx.UserID, ctx.ChatID, query)
			return &callback.Response{CallbackMsg: "搜索中...", ShowAlert: false}, nil
		}
		return &callback.Response{Text: "⚠️ 热门数据已过期", ShowAlert: true}, nil
	}

	// Check if this is a search history query (by index, avoiding 64-byte callback limit)
	if idxStr, hasHist := ctx.Callback.Params["hist"]; hasHist {
		var idx int
		fmt.Sscanf(idxStr, "%d", &idx)
		query := h.getHistoryQuery(ctx.UserID, idx)
		if query != "" {
			logger.Info("[SearchHandler] History search: idx=%d query=%s", idx, query)
			h.HandleSearchQuery(ctx.UserID, ctx.ChatID, query)
			return &callback.Response{CallbackMsg: "搜索中...", ShowAlert: false}, nil
		}
		return &callback.Response{Text: "⚠️ 历史记录已过期，请重新搜索", ShowAlert: true}, nil
	}

	// Check if this is a search history query (by legacy format with query in callback data)
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
		kb.AddButton("🏠 主菜单", "start")
		return &callback.Response{
			Text:     "🗑️ 搜索历史已清空",
			Edit:     true,
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	return h.showSearchHistoryOrPrompt(ctx)
}

// getPopularQuery 根据索引从热门搜索中取回 query。
func (h *SearchHandler) getPopularQuery(idx int) string {
	if h.searchHistoryDB != nil {
		pops, err := h.searchHistoryDB.GetPopularSearches(20)
		if err == nil && idx >= 0 && idx < len(pops) {
			return pops[idx].Query
		}
	}
	return ""
}

// getHistoryQuery 根据索引从搜索历史中取回 query。
func (h *SearchHandler) getHistoryQuery(userID int64, idx int) string {
	if h.searchHistoryDB != nil {
		entries, err := h.searchHistoryDB.GetHistory(userID, 20)
		if err == nil && idx >= 0 && idx < len(entries) {
			return entries[idx].Query
		}
	}
	if h.searchHistory != nil {
		entries := h.searchHistory.GetHistory(userID)
		if idx >= 0 && idx < len(entries) {
			return entries[idx].Query
		}
	}
	return ""
}

// HandleSearchQuery handles a text search query.
func (h *SearchHandler) HandleSearchQuery(userID int64, chatID int64, query string) error {
	return h.handleSearchQueryPage(userID, chatID, query, 1, true)
}

// handleSearchQueryPage keeps the requested page in the API call, session and
// keyboard. recordHistory is false for pagination so navigation does not pollute
// search frequency and trending statistics.
func (h *SearchHandler) handleSearchQueryPage(userID int64, chatID int64, query string, page int, recordHistory bool) error {
	logger.Info("[SearchHandler] Search query: %s page=%d", query, page)

	query = strings.TrimSpace(query)
	if query == "" {
		return h.showSearchHistory(userID, chatID)
	}
	if page < 1 {
		page = 1
	}

	// 发送 typing 指示器，让用户知道 Bot 在处理
	h.telegram.SendChatAction(chatID, "typing")

	// Add to search history only for a user-initiated query. Pagination is navigation.
	if recordHistory {
		if h.searchHistoryDB != nil {
			h.searchHistoryDB.AddSearch(userID, query)
			logger.Info("[SearchHandler] Search added to SearchHistoryDB: userID=%d, query=%s", userID, query)
		} else if h.searchHistory != nil {
			h.searchHistory.AddSearch(userID, query)
			logger.Info("[SearchHandler] Search added to SearchHistory (legacy): userID=%d, query=%s", userID, query)
		} else {
			logger.Info("[SearchHandler] WARNING: No search history service available, query not saved: %s", query)
		}
	}

	// Perform an 8-item interactive search so every API result remains reachable.
	results, err := h.moviepilot.SearchMediaWithCount(query, page, 8)
	if err != nil {
		logger.Info("[SearchHandler] Search failed: %v", err)
		h.sendUserScopedText(userID, chatID, "❌ 搜索服务暂时不可用，请稍后再试", nil)
		return err
	}

	if results == nil || results.Results == nil {
		h.sendNoResultsMessage(userID, chatID, query)
		return nil
	}

	filtered := results.Results[:0]
	for _, item := range results.Results {
		if item.ID > 0 {
			filtered = append(filtered, item)
		}
	}
	results.Results = filtered

	if len(results.Results) == 0 {
		if page > 1 {
			h.sendUserScopedText(userID, chatID, fmt.Sprintf("🔍 「%s」没有更多结果了", query), buildSearchRecoveryKeyboard(page-1))
			return nil
		}
		fallbackResults, fallbackQuery, fbErr := h.trySearchFallback(query)
		if fbErr != nil {
			logger.Info("[SearchHandler] Fallback search failed: %v", fbErr)
		}
		if fallbackResults != nil && len(fallbackResults) > 0 {
			h.sendSearchResults(userID, chatID, fallbackQuery, &services.SearchResponse{Results: fallbackResults}, 1)
			return nil
		}
		h.sendNoResultsMessage(userID, chatID, query)
		return nil
	}

	h.sendSearchResults(userID, chatID, query, results, page)
	return nil
}

func (h *SearchHandler) showSearchHistoryOrPrompt(ctx *callback.Context) (*callback.Response, error) {
	msg := services.NewMessageBuilder()
	msg.Bold("🔍 搜索求片").Newline()
	msg.Newline()
	msg.Text("把片名发给我就行").Newline()
	msg.Newline()
	msg.Text("中英文、电影剧集都能搜").Newline()
	msg.Newline()
	msg.Italic("💡 直接发片名，不用加命令")

	kb := services.NewKeyboardBuilder()
	kb.AddButton("📊 历史记录", "search_history_menu")
	kb.NewRow()
	kb.AddButton("🏠 主菜单", "start")

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

	// Parse TMDB ID for validation
	tmdbID := 0
	fmt.Sscanf(tmdbIDStr, "%d", &tmdbID)
	if tmdbID == 0 {
		return &callback.Response{
			Text:        "⚠️ 这条结果暂时打不开，请换一条试试",
			CallbackMsg: "条目无效",
			ShowAlert:   true,
			Edit:        true,
		}, nil
	}

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
			Text:        "⚠️ 搜索会话已过期，点下面重新搜索",
			CallbackMsg: "会话过期",
			ShowAlert:   true,
			Keyboard:    convertKeyboard(buildSearchRecoveryKeyboard(0)),
		}, nil
	}

	// Callback pagination returns a view to the central renderer instead of
	// sending directly. This lets group callbacks update their existing
	// ephemeral placeholder and keeps private/public routing fail-closed.
	results, err := h.moviepilot.SearchMediaWithCount(query, page, 8)
	if err != nil {
		logger.Info("[SearchHandler] Page search failed: %v", err)
		return &callback.Response{Text: "❌ 搜索服务暂时不可用，请稍后再试", CallbackMsg: "搜索失败"}, nil
	}
	if results == nil {
		results = &services.SearchResponse{}
	}
	filtered := results.Results[:0]
	for _, item := range results.Results {
		if item.ID > 0 {
			filtered = append(filtered, item)
		}
	}
	results.Results = filtered
	if len(results.Results) == 0 {
		return &callback.Response{
			Text:        fmt.Sprintf("🔍 「%s」没有更多结果了", query),
			CallbackMsg: "已经到底了",
			Keyboard:    convertKeyboard(buildSearchRecoveryKeyboard(page - 1)),
		}, nil
	}
	h.storeSearchResults(ctx.UserID, query, results.Results, page)
	var rich *types.TelegramInputRichMessage
	if ctx.ChatType == "private" {
		rich = h.buildVisualSearchSlideshow(query, page, results.Results)
	}
	return &callback.Response{
		Text:                  buildSearchResultsText(query, page, results.Results),
		StructuredRichMessage: rich,
		Keyboard:              convertKeyboard(buildSearchResultsKeyboard(results.Results, page, len(results.Results) >= 8)),
		CallbackMsg:           fmt.Sprintf("第 %d 页", page),
	}, nil
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
		kb.AddButton("🏠 主菜单", "start")

		return &callback.Response{
			Text:     msg.Build(),
			Edit:     true,
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	return h.buildRecommendationResponse(results, tType, ctx.Callback, ctx.UserID), nil
}

func (h *SearchHandler) handleMoodRecommendation(ctx *callback.Context, recType, mood string) (*callback.Response, error) {
	logger.Info("[SearchHandler] Mood recommendation (fallback to trending): type=%s, mood=%s", recType, mood)

	// AI module removed, fallback to regular trending
	return h.handleTrending(ctx, recType)
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
		kb.AddButton("🏠 主菜单", "start")

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
	kb.AddButton("🏠 主菜单", "start")

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
	msg := fmt.Sprintf("🔍 没找到「%s」\n\n可以检查片名，或换个更短的名字再搜。\n\n也可以点「许愿」：片源以后出现时，会继续帮你留意。", query)
	kb := services.NewKeyboardBuilder()
	// #1 搜索无结果 → 弹「🌟 加入许愿池」按钮。
	// 片名可能超长 / 含特殊字符，直接塞 callback_data 会撞 TG 64 字节上限，
	// 因此把片名暂存到 session（按 userID 存），回调串只用固定的 "wish_add"，
	// 由 WishHandler 用 ctx.UserID 从同一 session 取词。
	// 仅当 sessMgr 可用且片名非空时提供按钮（否则取不到词，避免点了报错的死按钮）。
	if h.sessMgr != nil && strings.TrimSpace(query) != "" {
		sess := h.sessMgr.GetOrCreate(userID)
		sess.Set("pending_wish_query", query)
		kb.AddButton("✨ 许愿", "wish_add")
		kb.NewRow()
	}
	kb.AddButton("🔍 重新搜索", "search")
	kb.AddButton("📜 搜索记录", "search_history_menu")
	kb.NewRow()
	kb.AddButton("🏠 主菜单", "start")
	h.sendUserScopedText(userID, chatID, msg, kb.Build())
}

func (h *SearchHandler) sendUserScopedText(userID, chatID int64, text string, keyboard *types.TelegramInlineKeyboard) {
	if chatID < 0 {
		if _, err := h.telegram.SendMessage(chatID, text, "", keyboard, &types.TelegramSendOptions{ReceiverUserID: userID}); err != nil {
			logger.Info("[SearchHandler] Ephemeral text send failed; no public fallback: %v", err)
		}
		return
	}
	if _, err := h.telegram.SendMessage(chatID, text, "", keyboard); err != nil {
		logger.Info("[SearchHandler] Text send failed: %v", err)
	}
}

func (h *SearchHandler) trySearchFallback(query string) ([]services.SearchResult, string, error) {
	return h.fallbackService.TryFallback(query)
}

func (h *SearchHandler) sendSearchResults(userID int64, chatID int64, query string, results *services.SearchResponse, page int) {
	text := buildSearchResultsText(query, page, results.Results)
	keyboard := buildSearchResultsKeyboard(results.Results, page, len(results.Results) >= 8)
	h.storeSearchResults(userID, query, results.Results, page)

	// Telegram chat IDs for groups/channels are negative. Avoid both rich-message
	// composition and transport there; community searches remain ephemeral text.
	if chatID > 0 {
		if rich := h.buildVisualSearchSlideshow(query, page, results.Results); rich != nil {
			if _, err := h.telegram.SendStructuredRichMessage(chatID, rich, keyboard); err == nil {
				return
			} else {
				logger.Info("[SearchHandler] Rich slideshow failed, falling back to text: %v", err)
			}
		}
	}
	h.sendUserScopedText(userID, chatID, text, keyboard)
}

func (h *SearchHandler) storeSearchResults(userID int64, query string, results []services.SearchResult, page int) {
	// Save before delivery so callback buttons remain usable after a transport
	// fallback or a Telegram retry.
	sess := h.sessMgr.GetOrCreate(userID)
	displayCount := len(results)
	if displayCount > 8 {
		displayCount = 8
	}
	searchItems := make([]session.SearchItem, 0, displayCount)
	for i, item := range results {
		if i >= 8 {
			break
		}
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
	sess.SetSearchResults(searchItems, page, query)
}

func buildSearchResultsText(query string, page int, results []services.SearchResult) string {
	text := fmt.Sprintf("🔍 搜索结果「%s」\n\n第 %d 页 · 本页最多展示 8 条\n\n", query, page)
	for i, item := range results {
		if i >= 8 {
			break
		}
		text += searchResultLine(i, item) + "\n"
	}
	return text
}

func searchResultLine(index int, item services.SearchResult) string {
	year := ""
	if item.Year > 0 {
		year = fmt.Sprintf(" (%d)", item.Year)
	}
	mediaType := "🎬 电影"
	if item.Type == "tv" || item.Type == "电视剧" {
		mediaType = "📺 剧集"
	}
	rating := ""
	if item.Rating > 0 {
		rating = fmt.Sprintf(" ⭐%.1f", item.Rating)
	}
	return fmt.Sprintf("%d. %s%s · %s%s", index+1, item.Title, year, mediaType, rating)
}

func searchSlideCaption(index int, item services.SearchResult) string {
	metadata := make([]string, 0, 3)
	if item.Year > 0 {
		metadata = append(metadata, fmt.Sprintf("%d", item.Year))
	}
	if item.Type == "tv" || item.Type == "电视剧" {
		metadata = append(metadata, "剧集")
	} else {
		metadata = append(metadata, "电影")
	}
	if item.Rating > 0 {
		metadata = append(metadata, fmt.Sprintf("⭐ %.1f", item.Rating))
	}

	caption := fmt.Sprintf("%d · %s\n%s", index+1, strings.TrimSpace(item.Title), strings.Join(metadata, " · "))
	if overview := compactOverview(item.Overview, 90); overview != "" {
		caption += "\n" + overview
	}
	return caption
}

func compactOverview(overview string, maxRunes int) string {
	overview = strings.Join(strings.Fields(overview), " ")
	if overview == "" || maxRunes <= 0 {
		return ""
	}
	runes := []rune(overview)
	if len(runes) <= maxRunes {
		return overview
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + "…"
}

// buildSearchSlideshow uses poster URLs already returned by the single search
// request; it performs no detail lookups and therefore introduces no N+1.
func buildSearchSlideshow(query string, page int, results []services.SearchResult) *types.TelegramInputRichMessage {
	blocks := make([]types.TelegramInputRichBlock, 0, 8)
	for i, item := range results {
		if i >= 8 {
			break
		}
		poster := getPosterURL(strings.TrimSpace(item.Poster))
		if poster == "" {
			continue
		}
		blocks = append(blocks, types.TelegramInputRichBlock{
			Type:    "photo",
			Photo:   &types.TelegramRichPhoto{Type: "photo", Media: poster},
			Caption: &types.TelegramRichText{Text: searchSlideCaption(i, item)},
		})
	}
	if len(blocks) < 2 {
		return nil
	}
	caption := fmt.Sprintf("🔍 搜索结果「%s」 · 第 %d 页\n左右滑动看海报，点下方片名看详情。", query, page)
	return &types.TelegramInputRichMessage{Blocks: []types.TelegramInputRichBlock{{
		Type: "slideshow", Blocks: blocks, Caption: &types.TelegramRichText{Text: caption},
	}}}
}

// buildVisualSearchSlideshow uses one fresh subscription-cache snapshot and
// composites all slides concurrently. Captions remain as semantic metadata,
// but the JPEG itself is authoritative because some Telegram clients omit
// slideshow photo captions.
func (h *SearchHandler) buildVisualSearchSlideshow(query string, page int, results []services.SearchResult) *types.TelegramInputRichMessage {
	subscribed, _ := h.moviepilot.CachedSubscriptionTMDBIDs()
	cards := services.BuildSearchVisualCards(results, subscribed)
	if len(cards) < 2 {
		return nil
	}
	blocks := make([]types.TelegramInputRichBlock, 0, len(cards))
	media := make([]types.TelegramInputRichMessageMedia, 0, len(cards))
	for _, card := range cards {
		item := results[card.ResultIndex]
		id := fmt.Sprintf("search_card_%d", card.ResultIndex+1)
		blocks = append(blocks, types.TelegramInputRichBlock{
			Type:    "photo",
			Photo:   &types.TelegramRichPhoto{Type: "photo", Media: "attach://" + id},
			Caption: &types.TelegramRichText{Text: searchSlideCaption(card.ResultIndex, item)},
		})
		media = append(media, types.TelegramInputRichMessageMedia{
			ID:       id,
			Media:    types.TelegramRichPhoto{Type: "photo", Media: "attach://" + id},
			Upload:   card.JPEG,
			Filename: id + ".jpg",
		})
	}
	caption := fmt.Sprintf("🔍 搜索结果「%s」 · 第 %d 页\n左右滑动看视觉卡，点下方片名看详情。", query, page)
	return &types.TelegramInputRichMessage{
		Blocks: []types.TelegramInputRichBlock{{Type: "slideshow", Blocks: blocks, Caption: &types.TelegramRichText{Text: caption}}},
		Media:  media,
	}
}

func buildSearchResultsKeyboard(results []services.SearchResult, page int, hasNext bool) *types.TelegramInlineKeyboard {
	rows := make([][]types.TelegramInlineKeyboardButton, 0, 12)
	for i, item := range results {
		if i >= 8 {
			break
		}
		mediaType := "movie"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaType = "tv"
		}
		rows = append(rows, []types.TelegramInlineKeyboardButton{{
			Text:         fmt.Sprintf("%d · %s", i+1, truncateSearchTitle(item.Title, 20)),
			CallbackData: fmt.Sprintf("select:id:%d:type:%s", item.ID, mediaType),
		}})
	}

	navRow := make([]types.TelegramInlineKeyboardButton, 0, 2)
	if page > 1 {
		navRow = append(navRow, types.TelegramInlineKeyboardButton{
			Text: "⬅️ 上一页", CallbackData: fmt.Sprintf("search:page:%d", page-1),
		})
	}
	if hasNext {
		navRow = append(navRow, types.TelegramInlineKeyboardButton{
			Text: "➡️ 下一页", CallbackData: fmt.Sprintf("search:page:%d", page+1),
		})
	}
	if len(navRow) > 0 {
		rows = append(rows, navRow)
	}
	rows = append(rows,
		[]types.TelegramInlineKeyboardButton{
			{Text: "🔍 换个片名", CallbackData: "search"},
			{Text: "📜 搜索记录", CallbackData: "search_history_menu"},
		},
		[]types.TelegramInlineKeyboardButton{{Text: "🏠 主菜单", CallbackData: "start"}},
	)
	return &types.TelegramInlineKeyboard{InlineKeyboard: rows}
}

func truncateSearchTitle(title string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(title))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	if maxRunes <= 1 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-1]) + "…"
}

func buildSearchRecoveryKeyboard(previousPage int) *types.TelegramInlineKeyboard {
	rows := make([][]types.TelegramInlineKeyboardButton, 0, 3)
	if previousPage > 0 {
		rows = append(rows, []types.TelegramInlineKeyboardButton{{
			Text:         "⬅️ 返回上一页",
			CallbackData: fmt.Sprintf("search:page:%d", previousPage),
		}})
	}
	rows = append(rows,
		[]types.TelegramInlineKeyboardButton{
			{Text: "🔍 换个片名", CallbackData: "search"},
			{Text: "📜 搜索记录", CallbackData: "search_history_menu"},
		},
		[]types.TelegramInlineKeyboardButton{{Text: "🏠 主菜单", CallbackData: "start"}},
	)
	return &types.TelegramInlineKeyboard{InlineKeyboard: rows}
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
	kb.AddButton("🏠 主菜单", "start")

	h.telegram.SendMessage(chatID, msg.Build(), "", kb.Build())
	return nil
}
