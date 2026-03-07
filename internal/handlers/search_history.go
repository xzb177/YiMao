package handlers

import (
	"fmt"
	"log"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/ui"
	"emby-telegram-bot/pkg/errors"
)

// SearchHistoryHandler handles search history operations
type SearchHistoryHandler struct {
	telegram       *services.TelegramClient
	searchHistory *services.SearchHistoryDB // 使用数据库版本
	uiBuilder     *ui.HistoryBuilder
}

// NewSearchHistoryHandler creates a new search history handler
func NewSearchHistoryHandler(
	telegram *services.TelegramClient,
	searchHistory *services.SearchHistoryDB,
) *SearchHistoryHandler {
	return &SearchHistoryHandler{
		telegram:       telegram,
		searchHistory: searchHistory,
		uiBuilder:     ui.NewHistoryBuilder(ui.StyleCard), // 使用极简卡片风
	}
}

// Handle handles search history callbacks
func (h *SearchHistoryHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	log.Printf("[SearchHistoryHandler] Handle: action=%s, params=%v", ctx.Callback.Action, ctx.Callback.Params)

	// 根据不同的 action 处理
	switch ctx.Callback.Action {
	case callback.ActionSearch:
		return h.handleSearchAction(ctx)
	case "search_history_menu":
		return h.showHistoryMenu(ctx)
	case "search_stats":
		return h.showStats(ctx)
	case "search_popular":
		return h.showPopularSearches(ctx)
	case "search_trends":
		return h.showTrends(ctx)
	case "search_manage":
		return h.showManageHistory(ctx)
	case "search_delete":
		return h.deleteEntry(ctx)
	case "search_clear_all":
		return h.clearHistory(ctx)
	default:
		return h.showHistoryMenu(ctx)
	}
}

// showHistoryMenu 显示搜索历史主菜单
func (h *SearchHistoryHandler) showHistoryMenu(ctx *callback.Context) (*callback.Response, error) {
	// 获取分组历史记录
	groupedHistory, err := h.searchHistory.GetHistoryGrouped(ctx.UserID)
	if err != nil {
		log.Printf("[SearchHistoryHandler] Failed to get grouped history: %v", err)
		return nil, errors.ServiceErr("failed to get history", err)
	}

	// 获取统计数据
	stats, err := h.searchHistory.GetStats(ctx.UserID)
	if err != nil {
		log.Printf("[SearchHistoryHandler] Failed to get stats: %v", err)
		stats = &services.SearchStats{Total: 0, Week: 0, Month: 0, Top5: []string{}}
	}

	// 获取热门搜索（可选）
	popular, err := h.searchHistory.GetPopularSearches(5)
	if err != nil {
		log.Printf("[SearchHistoryHandler] Failed to get popular: %v", err)
		popular = []services.PopularSearch{}
	}

	// 获取趋势（可选）
	trends, err := h.searchHistory.GetSearchTrends(7)
	if err != nil {
		log.Printf("[SearchHistoryHandler] Failed to get trends: %v", err)
		trends = []services.TrendItem{}
	}

	// 构建界面
	message := h.uiBuilder.BuildHistoryUI(ctx.UserID, stats, groupedHistory, popular, trends)

	// 构建键盘
	history, err := h.searchHistory.GetHistory(ctx.UserID, 10)
	if err != nil {
		log.Printf("[SearchHistoryHandler] Failed to get history for keyboard: %v", err)
		history = []services.SearchEntry{}
	}

	keyboard := h.uiBuilder.BuildHistoryKeyboard(history, ctx.UserID)

	return &callback.Response{
		Text:     message,
		Edit:     true,
		Keyboard: keyboard,
	}, nil
}

// showStats 显示统计信息
func (h *SearchHistoryHandler) showStats(ctx *callback.Context) (*callback.Response, error) {
	stats, err := h.searchHistory.GetStats(ctx.UserID)
	if err != nil {
		log.Printf("[SearchHistoryHandler] Failed to get stats: %v", err)
		return &callback.Response{
			Text:        "❌ 获取统计失败",
			CallbackMsg: "获取失败",
			ShowAlert:   true,
		}, nil
	}

	message := h.uiBuilder.BuildStatsUI(stats, ctx.UserID)
	keyboard := h.uiBuilder.BuildStatsKeyboard()

	return &callback.Response{
		Text:     message,
		Edit:     true,
		Keyboard: keyboard,
	}, nil
}

// showPopularSearches 显示热门搜索
func (h *SearchHistoryHandler) showPopularSearches(ctx *callback.Context) (*callback.Response, error) {
	// 检查是本周热门还是历史热门
	period := "week"
	if periodParam, hasPeriod := ctx.Callback.Params["period"]; hasPeriod {
		period = periodParam
	}

	limit := 10
	popular, err := h.searchHistory.GetPopularSearches(limit)
	if err != nil {
		log.Printf("[SearchHistoryHandler] Failed to get popular: %v", err)
		return &callback.Response{
			Text:        "❌ 获取热门搜索失败",
			CallbackMsg: "获取失败",
			ShowAlert:   true,
		}, nil
	}

	message := h.uiBuilder.BuildPopularSearchesUI(popular, period == "all")
	keyboard := h.uiBuilder.BuildPopularSearchesKeyboard(popular)

	return &callback.Response{
		Text:     message,
		Edit:     true,
		Keyboard: keyboard,
	}, nil
}

// showTrends 显示搜索趋势
func (h *SearchHistoryHandler) showTrends(ctx *callback.Context) (*callback.Response, error) {
	// 获取天数参数
	days := 7
	if daysParam, hasDays := ctx.Callback.Params["days"]; hasDays {
		fmt.Sscanf(daysParam, "%d", &days)
	}

	trends, err := h.searchHistory.GetSearchTrends(days)
	if err != nil {
		log.Printf("[SearchHistoryHandler] Failed to get trends: %v", err)
		return &callback.Response{
			Text:        "❌ 获取搜索趋势失败",
			CallbackMsg: "获取失败",
			ShowAlert:   true,
		}, nil
	}

	message := h.uiBuilder.BuildTrendsUI(trends, days)
	keyboard := h.uiBuilder.BuildTrendsKeyboard(days)

	return &callback.Response{
		Text:     message,
		Edit:     true,
		Keyboard: keyboard,
	}, nil
}

// showManageHistory 显示管理历史界面
func (h *SearchHistoryHandler) showManageHistory(ctx *callback.Context) (*callback.Response, error) {
	// 获取页码
	page := 1
	if pageParam, hasPage := ctx.Callback.Params["page"]; hasPage {
		fmt.Sscanf(pageParam, "%d", &page)
	}

	// 获取历史记录
	history, err := h.searchHistory.GetHistory(ctx.UserID, 0)
	if err != nil {
		log.Printf("[SearchHistoryHandler] Failed to get history: %v", err)
		return &callback.Response{
			Text:        "❌ 获取历史失败",
			CallbackMsg: "获取失败",
			ShowAlert:   true,
		}, nil
	}

	// 计算分页
	itemsPerPage := 10
	totalPages := (len(history) + itemsPerPage - 1) / itemsPerPage
	if totalPages == 0 {
		totalPages = 1
	}

	// 获取当前页数据
	startIdx := (page - 1) * itemsPerPage
	endIdx := startIdx + itemsPerPage
	if endIdx > len(history) {
		endIdx = len(history)
	}

	pageHistory := history[startIdx:endIdx]

	message := h.uiBuilder.BuildManageHistoryUI(pageHistory)
	keyboard := h.uiBuilder.BuildManageHistoryKeyboard(pageHistory, page, totalPages)

	return &callback.Response{
		Text:     message,
		Edit:     true,
		Keyboard: keyboard,
	}, nil
}

// deleteEntry 删除单条历史记录
func (h *SearchHistoryHandler) deleteEntry(ctx *callback.Context) (*callback.Response, error) {
	// 获取索引
	indexStr, hasIndex := ctx.Callback.Params["index"]
	if !hasIndex {
		return &callback.Response{
			Text:        "❌ 参数无效",
			CallbackMsg: "参数错误",
			ShowAlert:   true,
		}, nil
	}

	var index int
	fmt.Sscanf(indexStr, "%d", &index)

	// 删除记录
	err := h.searchHistory.DeleteEntry(ctx.UserID, index)
	if err != nil {
		log.Printf("[SearchHistoryHandler] Failed to delete entry: %v", err)
		return &callback.Response{
			Text:        "❌ 删除失败",
			CallbackMsg: "删除失败",
			ShowAlert:   true,
		}, nil
	}

	// 返回到管理界面
	return h.showManageHistory(ctx)
}

// clearHistory 清空所有历史记录
func (h *SearchHistoryHandler) clearHistory(ctx *callback.Context) (*callback.Response, error) {
	err := h.searchHistory.ClearHistory(ctx.UserID)
	if err != nil {
		log.Printf("[SearchHistoryHandler] Failed to clear history: %v", err)
		return &callback.Response{
			Text:        "❌ 清空失败",
			CallbackMsg: "清空失败",
			ShowAlert:   true,
		}, nil
	}

	return &callback.Response{
		Text:        "✅ 搜索历史已清空",
		CallbackMsg: "已清空",
		ShowAlert:   true,
	}, nil
}

// handleSearchAction 处理搜索相关的 action
func (h *SearchHistoryHandler) handleSearchAction(ctx *callback.Context) (*callback.Response, error) {
	// 检查是否是快速搜索
	if query, hasQuery := ctx.Callback.Params["query"]; hasQuery {
		// 转义处理
		query = unescapeString(query)

		log.Printf("[SearchHistoryHandler] Quick search: %s", query)

		// 发送搜索结果（这里需要调用 SearchHandler）
		// 由于 SearchHandler 是独立的，我们需要一个方法来触发搜索
		return &callback.Response{
			Text:        fmt.Sprintf("🔍 正在搜索：%s", query),
			CallbackMsg: "搜索中...",
			ShowAlert:   false,
		}, nil
	}

	return h.showHistoryMenu(ctx)
}

// unescapeString 反转义字符串
func unescapeString(s string) string {
	s = s // Placeholder - implement unescaping if needed
	return s
}
