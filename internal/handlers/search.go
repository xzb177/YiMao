package handlers

import (
	"fmt"
	"log"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
)

// SearchHandler handles search callbacks and queries
type SearchHandler struct {
	sessMgr        *session.Manager
	telegram       *services.TelegramClient
	moviepilot     *services.MoviePilotClient
	tmdb           *services.TMDBClient
	searchService  *services.SearchService
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

	// Otherwise, show search prompt
	return &callback.Response{
		Text:     "🔍 请输入影片名称进行搜索",
		Edit:     true,
		Keyboard: &callback.Keyboard{},
	}, nil
}

// handleSearchQuery handles a text search query
func (h *SearchHandler) HandleSearchQuery(userID int64, chatID int64, query string) error {
	log.Printf("[SearchHandler] Search query: %s", query)

	// Perform search
	result, err := h.searchService.Search(userID, query, 1)
	if err != nil {
		h.telegram.SendMessage(chatID, fmt.Sprintf("❌ 搜索失败: %v", err), "Markdown", nil)
		return err
	}

	if len(result.Results) == 0 {
		h.telegram.SendMessage(chatID, "❌ 未找到匹配结果", "Markdown", nil)
		return nil
	}

	// Build results message
	msg := services.NewMessageBuilder()
	msg.Bold(fmt.Sprintf("🔍 搜索结果: %s", query)).Newline()
	msg.Textf("找到 %d 个结果", len(result.Results)).Newline()
	msg.Newline()

	// Build keyboard with results
	kb := services.NewKeyboardBuilder()

	displayCount := len(result.Results)
	if displayCount > 10 {
		displayCount = 10
	}

	for i, item := range result.Results {
		if i >= displayCount {
			break
		}

		yearStr := ""
		if item.Year > 0 {
			yearStr = fmt.Sprintf(" (%d)", item.Year)
		}
		ratingStr := ""
		if item.Rating > 0 {
			ratingStr = fmt.Sprintf(" ⭐%.1f", item.Rating)
		}

		buttonText := fmt.Sprintf("%d. %s%s%s", i+1, item.Title, yearStr, ratingStr)
		callbackData := fmt.Sprintf("select:id:%s:type:%s", item.ID, item.Type)

		kb.AddButton(buttonText, callbackData)
		kb.NewRow()
	}

	// Add pagination if needed
	if len(result.Results) >= 20 {
		kb.AddButton("➡️ 下一页", fmt.Sprintf("search:query:%s:page:%d", query, 2))
		kb.NewRow()
	}

	kb.AddButton("❌ 取消", "cancel")

	h.telegram.SendMessage(chatID, msg.Build(), "Markdown", kb.Build())

	return nil
}

func (h *SearchHandler) handleSelect(ctx *callback.Context, tmdbIDStr string) (*callback.Response, error) {
	// Redirect to detail handler - build detail callback data
	detailCallback := fmt.Sprintf("detail:id:%s:type:movie", tmdbIDStr)

	// Try to detect type from search results
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	_, _, _, hasSearch := sess.GetSearchResults()
	if hasSearch {
		items, _, _, _ := sess.GetSearchResults()
		for _, item := range items {
			if item.ID == tmdbIDStr {
				detailCallback = fmt.Sprintf("detail:id:%s:type:%s", item.ID, item.Type)
				break
			}
		}
	}

	// Parse and delegate to detail handler
	parser := callback.NewParser()
	cb, _ := parser.Parse(detailCallback)
	ctx.Callback = cb

	detailHandler := NewDetailHandler(h.sessMgr, h.telegram, h.moviepilot, h.tmdb)
	return detailHandler.Handle(ctx)
}

func (h *SearchHandler) handlePage(ctx *callback.Context, pageStr string) (*callback.Response, error) {
	// Get last search query from session
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	_, page, query, hasResults := sess.GetSearchResults()

	if !hasResults {
		return &callback.Response{
			Text:        "搜索结果已过期，请重新搜索",
			CallbackMsg: "结果已过期",
			ShowAlert:   true,
		}, nil
	}

	// Fetch new page from Jellyseerr
	newPage := page + 1
	result, err := h.searchService.Search(ctx.UserID, query, newPage)
	if err != nil {
		return &callback.Response{
			Text:        fmt.Sprintf("加载失败: %v", err),
			CallbackMsg: "加载失败",
			ShowAlert:   true,
		}, nil
	}

	// Build results message
	msg := services.NewMessageBuilder()
	msg.Bold(fmt.Sprintf("🔍 搜索结果: %s", query)).Newline()
	msg.Textf("找到 %d 个结果 (第 %d 页)", len(result.Results), newPage).Newline()
	msg.Newline()

	kb := services.NewKeyboardBuilder()

	displayCount := len(result.Results)
	if displayCount > 10 {
		displayCount = 10
	}

	for i, item := range result.Results {
		if i >= displayCount {
			break
		}

		yearStr := ""
		if item.Year > 0 {
			yearStr = fmt.Sprintf(" (%d)", item.Year)
		}
		ratingStr := ""
		if item.Rating > 0 {
			ratingStr = fmt.Sprintf(" ⭐%.1f", item.Rating)
		}

		buttonText := fmt.Sprintf("%d. %s%s%s", ((newPage-1)*10)+i+1, item.Title, yearStr, ratingStr)
		callbackData := fmt.Sprintf("select:id:%s:type:%s", item.ID, item.Type)

		kb.AddButton(buttonText, callbackData)
		kb.NewRow()
	}

	// Add pagination buttons
	if newPage > 1 {
		kb.AddButton("⬅️ 上一页", fmt.Sprintf("search:page:%d", newPage-1))
	}
	if result.Total > newPage*10 {
		kb.AddButton("➡️ 下一页", fmt.Sprintf("search:page:%d", newPage+1))
	}
	kb.NewRow()

	kb.AddButton("⬅️ 返回", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

func (h *SearchHandler) handleTrending(ctx *callback.Context, tType string) (*callback.Response, error) {
	// This will be handled by AIHandler for trending results
	return &callback.Response{
		Text:        "正在加载热门推荐...",
		CallbackMsg: "加载中",
		ShowAlert:   true,
	}, nil
}
