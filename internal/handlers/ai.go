package handlers

import (
	"fmt"
	"log"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/config"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
	"emby-telegram-bot/pkg/errors"
)

// AIHandler handles AI recommendation callbacks
type AIHandler struct {
	cfg        *config.Config
	sessMgr    *session.Manager
	telegram   *services.TelegramClient
	moviepilot *services.MoviePilotClient
	aiService  *services.AIService
}

func NewAIHandler(
	cfg *config.Config,
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	moviepilot *services.MoviePilotClient,
) *AIHandler {
	aiSvc := services.NewAIService(moviepilot, sessMgr)
	return &AIHandler{
		cfg:        cfg,
		sessMgr:    sessMgr,
		telegram:   telegram,
		moviepilot: moviepilot,
		aiService:  aiSvc,
	}
}

// SetTMDBClient sets the TMDB client
func (h *AIHandler) SetTMDBClient(tmdb *services.TMDBClient) {
	h.aiService.SetTMDBClient(tmdb)
}

func (h *AIHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	if !h.cfg.EnableAI {
		return &callback.Response{
			Text:        "❌ AI 推荐功能未启用",
			CallbackMsg: "功能未启用",
			ShowAlert:   true,
		}, nil
	}

	// Get AI sub-action from params
	subAction, hasAction := ctx.Callback.Params["type"]

	if !hasAction {
		// Show AI menu
		return h.ShowAIMenu()
	}

	switch subAction {
	case "trending":
		return h.HandleTrending(ctx)
	case "hot":
		return h.HandleHot(ctx)
	case "new":
		return h.HandleNew(ctx)
	case "random":
		return h.HandleRandom(ctx)
	case "toprated":
		return h.HandleTopRated(ctx)
	default:
		return &callback.Response{
			Text:        "❌ 未知的 AI 推荐类型",
			CallbackMsg: "未知类型",
			ShowAlert:   true,
		}, nil
	}
}

func (h *AIHandler) HandleSearchQuery(userID int64, chatID int64, query string) error {
	// Check if this is an AI recommendation query
	if isAIQuery(query) {
		return h.handleAIQuery(userID, chatID, query)
	}

	// Otherwise delegate to search handler
	return nil
}

func (h *AIHandler) ShowAIMenu() (*callback.Response, error) {
	msg := services.NewMessageBuilder()
	msg.Bold("🤖 AI 智能推荐").Newline()
	msg.Newline()
	msg.Italic("💡 基于大数据分析，为您精选优质内容").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🔥 热门电影", "ai:type:trending")
	kb.AddButton("📺 热播剧集", "ai:type:hot")
	kb.NewRow()
	kb.AddButton("⭐ 高分佳作", "ai:type:toprated")
	kb.AddButton("🆕 最新上线", "ai:type:new")
	kb.NewRow()
	kb.AddButton("🎲 随机发现", "ai:type:random")
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

func (h *AIHandler) HandleTrending(ctx *callback.Context) (*callback.Response, error) {
	result, err := h.aiService.GetTrendingMovies(ctx.UserID, 1)
	if err != nil {
		return nil, errors.AIErr("获取热门推荐失败", err)
	}

	return h.buildResultsMessage(ctx.UserID, result, "🔥 热门电影推荐")
}

func (h *AIHandler) HandleHot(ctx *callback.Context) (*callback.Response, error) {
	result, err := h.aiService.GetHotTV(ctx.UserID, 1)
	if err != nil {
		return nil, errors.AIErr("获取热门剧集失败", err)
	}

	return h.buildResultsMessage(ctx.UserID, result, "📺 热门剧集推荐")
}

func (h *AIHandler) HandleNew(ctx *callback.Context) (*callback.Response, error) {
	result, err := h.aiService.GetNewMovies(ctx.UserID, 1)
	if err != nil {
		return nil, errors.AIErr("获取新片失败", err)
	}

	return h.buildResultsMessage(ctx.UserID, result, "🆕 新片上线")
}

func (h *AIHandler) HandleTopRated(ctx *callback.Context) (*callback.Response, error) {
	result, err := h.aiService.GetTopRated(ctx.UserID, 1)
	if err != nil {
		return nil, errors.AIErr("获取高分佳作失败", err)
	}

	return h.buildResultsMessage(ctx.UserID, result, "⭐ 高分佳作")
}

func (h *AIHandler) HandleRandom(ctx *callback.Context) (*callback.Response, error) {
	result, err := h.aiService.GetRandom(ctx.UserID, 5)
	if err != nil {
		return nil, errors.AIErr("随机推荐失败", err)
	}

	return h.buildResultsMessage(ctx.UserID, result, "🎲 随机推荐")
}

// Internal methods

func (h *AIHandler) buildResultsMessage(userID int64, result *services.TrendingResult, title string) (*callback.Response, error) {
	msg := services.NewMessageBuilder()
	msg.Bold(title).Newline()
	msg.Newline()

	if len(result.Items) == 0 {
		msg.Text("暂无推荐")
	} else {
		msg.Textf("为您找到 %d 个推荐", len(result.Items)).Newline()
		msg.Newline()

		displayCount := len(result.Items)
		if displayCount > 10 {
			displayCount = 10
		}

		for i, item := range result.Items {
			if i >= displayCount {
				break
			}

			yearStr := ""
			if item.Year > 0 {
				yearStr = fmt.Sprintf("%d", item.Year)
			}
			ratingStr := ""
			if item.Rating > 0 {
				ratingStr = fmt.Sprintf(" ⭐%.1f", item.Rating)
			}

			msg.Textf("%d. %s", i+1, item.Title)
			if yearStr != "" {
				msg.Textf(" (%s)", yearStr)
			}
			if ratingStr != "" {
				msg.Text(ratingStr)
			}
			msg.Newline()
		}
	}

	kb := services.NewKeyboardBuilder()

	displayCount := len(result.Items)
	if displayCount > 10 {
		displayCount = 10
	}

	// Build buttons in 2 columns
	for i, item := range result.Items {
		if i >= displayCount {
			break
		}

		buttonText := fmt.Sprintf("%d", i+1)
		callbackData := fmt.Sprintf("detail:id:%s:type:%s", item.ID, item.Type)

		kb.AddButton(buttonText, callbackData)
		// Add new row after every 2 buttons (except the last one)
		if i%2 == 1 || i == displayCount-1 {
			kb.NewRow()
		}
	}

	kb.AddButton("🔄 换一批", fmt.Sprintf("ai:type:%s", result.Source))
	kb.AddButton("⬅️ 返回主菜单", "start")

	// Cache all AI recommendation items for later use in request handler
	sess := h.sessMgr.GetOrCreate(userID)
	if sess != nil && len(result.Items) > 0 {
		cacheItems := make([]*session.AIRecommendationItem, 0, len(result.Items))
		for _, item := range result.Items {
			tmdbID := 0
			fmt.Sscanf(item.ID, "%d", &tmdbID)
			if tmdbID > 0 {
				cacheItems = append(cacheItems, &session.AIRecommendationItem{
					TmdbID:    tmdbID,
					Title:     item.Title,
					Year:      item.Year,
					Rating:    item.Rating,
					MediaType: item.Type,
					Overview:  item.Overview,
					Reason:    h.generateReason(result.Source),
				})
			}
		}
		sess.CacheAIResults(cacheItems)
		log.Printf("[AIHandler] Cached %d AI recommendation items", len(cacheItems))
	}

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// generateReason generates an AI recommendation reason
func (h *AIHandler) generateReason(source string) string {
	switch source {
	case "trending":
		return "🔥 当前热门影片，观众评分很高"
	case "hot":
		return "📺 热播剧集，追剧达人推荐"
	case "new":
		return "🆕 最新上映，值得一看"
	case "random":
		return "🎲 为你随机挑选的佳作"
	default:
		return "💡 精选推荐"
	}
}

func (h *AIHandler) handleAIQuery(userID int64, chatID int64, query string) error {
	// Parse intent from natural language query
	intent := parseAIIntent(query)

	var result *services.TrendingResult
	var err error

	switch intent {
	case "trending":
		result, err = h.aiService.GetTrendingMovies(userID, 1)
	case "hot":
		result, err = h.aiService.GetHotTV(userID, 1)
	case "new":
		result, err = h.aiService.GetNewMovies(userID, 1)
	case "random":
		result, err = h.aiService.GetRandom(userID, 5)
	default:
		// Default to trending
		result, err = h.aiService.GetTrendingMovies(userID, 1)
	}

	if err != nil {
		h.telegram.SendMessage(chatID, fmt.Sprintf("❌ 获取推荐失败: %v", err), "Markdown", nil)
		return err
	}

	// Build and send message
	msg := services.NewMessageBuilder()
	msg.Bold("🤖 AI 推荐").Newline()
	msg.Newline()
	msg.Textf("为您找到 %d 个推荐\n\n", len(result.Items))

	for i, item := range result.Items {
		if i >= 5 {
			break
		}

		yearStr := ""
		if item.Year > 0 {
			yearStr = fmt.Sprintf("%d", item.Year)
		}
		ratingStr := ""
		if item.Rating > 0 {
			ratingStr = fmt.Sprintf(" ⭐%.1f", item.Rating)
		}

		msg.Textf("%d. %s", i+1, item.Title)
		if yearStr != "" {
			msg.Textf(" (%s)", yearStr)
		}
		if ratingStr != "" {
			msg.Text(ratingStr)
		}
		msg.Newline()
	}

	kb := services.NewKeyboardBuilder()

	for i, item := range result.Items {
		if i >= 5 {
			break
		}

		buttonText := fmt.Sprintf("📖 %d", i+1)
		callbackData := fmt.Sprintf("detail:id:%s:type:%s", item.ID, item.Type)

		kb.AddButton(buttonText, callbackData)
		kb.NewRow()
	}

	kb.AddButton("⬅️ 返回主菜单", "start")

	h.telegram.SendMessage(chatID, msg.Build(), "Markdown", kb.Build())

	return nil
}

// isAIQuery checks if a query is an AI recommendation request
func isAIQuery(query string) bool {
	aiKeywords := []string{"推荐", "有什么", "好看的", "想看", "来点", "给我"}
	for _, keyword := range aiKeywords {
		if contains(query, keyword) {
			return true
		}
	}
	return false
}

// parseAIIntent parses the intent from an AI query
func parseAIIntent(query string) string {
	if contains(query, "热门") || contains(query, "trending") {
		return "trending"
	}
	if contains(query, "剧集") || contains(query, "电视剧") || contains(query, "tv") {
		return "hot"
	}
	if contains(query, "新片") || contains(query, "最新") || contains(query, "new") {
		return "new"
	}
	if contains(query, "随机") || contains(query, "随便") {
		return "random"
	}
	return "trending" // Default
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (
		s[:len(substr)] == substr ||
		s[len(s)-len(substr):] == substr ||
		indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
