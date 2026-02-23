package handlers

import (
	"fmt"
	"log"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/config"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
	"emby-telegram-bot/pkg/errors"
	"emby-telegram-bot/pkg/types"
)

// StartHandler handles start menu callbacks
type StartHandler struct {
	cfg         *config.Config
	sessMgr     *session.Manager
	telegram    *services.TelegramClient
	moviepilot  *services.MoviePilotClient
	adminService *services.AdminService
}

func NewStartHandler(
	cfg *config.Config,
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	moviepilot *services.MoviePilotClient,
) *StartHandler {
	return &StartHandler{
		cfg:        cfg,
		sessMgr:    sessMgr,
		telegram:   telegram,
		moviepilot: moviepilot,
	}
}

// SetAdminService sets the admin service
func (h *StartHandler) SetAdminService(adminSvc *services.AdminService) {
	h.adminService = adminSvc
}

func (h *StartHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	action := ctx.Callback.Action

	switch action {
	case callback.ActionStart:
		return h.HandleStart(ctx)
	case callback.ActionSearch:
		return h.HandleSearch(ctx)
	case callback.ActionAI:
		return h.HandleAI(ctx)
	case callback.ActionHot:
		return h.HandleHot(ctx)
	case callback.ActionNew:
		return h.HandleNew(ctx)
	default:
		return nil, errors.CallbackInvalid(fmt.Sprintf("unknown start action: %s", action))
	}
}

func (h *StartHandler) HandleStart(ctx *callback.Context) (*callback.Response, error) {
	msg := services.NewMessageBuilder()
	msg.Bold("👋 欢迎使用云海影视助手").Newline()
	msg.Newline()
	msg.Text("🔍 搜索影片 · 快速查找心仪内容").Newline()
	msg.Text("🤖 AI 推荐 · 发现热门好片").Newline()
	msg.Text("📋 我的请求 · 跟踪求片进度").Newline()
	msg.Text("🔗 账号绑定 · 同步观影记录").Newline()
	msg.Newline()
	msg.Italic("💡 点击下方按钮开始探索").Newline()

	// Check if user is admin to add admin menu button
	isAdmin := false
	if h.adminService != nil {
		isAdmin = h.adminService.IsAdmin(ctx.UserID)
	}

	keyboard := services.BuildStartKeyboard(isAdmin)

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(keyboard),
	}, nil
}

func (h *StartHandler) HandleSearch(ctx *callback.Context) (*callback.Response, error) {
	msg := services.NewMessageBuilder()
	msg.Bold("🔍 搜索影片").Newline()
	msg.Newline()
	msg.Text("请输入影片名称，支持中文/英文").Newline()
	msg.Newline()
	msg.Italic("💡 输入影片名称后自动搜索")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: &callback.Keyboard{},
	}, nil
}

func (h *StartHandler) HandleAI(ctx *callback.Context) (*callback.Response, error) {
	if !h.cfg.EnableAI {
		return &callback.Response{
			Text:        "⚠️ AI 推荐暂未开放",
			CallbackMsg: "功能未启用",
			ShowAlert:   true,
		}, nil
	}

	msg := services.NewMessageBuilder()
	msg.Bold("🤖 AI 智能推荐").Newline()
	msg.Newline()
	msg.Italic("✨ 为您精选优质内容").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🔥 热门电影", "ai:trending")
	kb.AddButton("📺 热播剧集", "ai:hot")
	kb.NewRow()
	kb.AddButton("⭐ 高分佳作", "ai:toprated")
	kb.AddButton("🆕 最新上线", "ai:new")
	kb.NewRow()
	kb.AddButton("🎲 随机发现", "ai:random")
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

func (h *StartHandler) HandleHot(ctx *callback.Context) (*callback.Response, error) {
	return &callback.Response{
		Text:        "📺 加载中...",
		CallbackMsg: "加载中",
		ShowAlert:   true,
	}, nil
}

func (h *StartHandler) HandleNew(ctx *callback.Context) (*callback.Response, error) {
	return &callback.Response{
		Text:        "🆕 加载中...",
		CallbackMsg: "加载中",
		ShowAlert:   true,
	}, nil
}

// DetailHandler handles media detail callbacks
type DetailHandler struct {
	sessMgr    *session.Manager
	telegram   *services.TelegramClient
	moviepilot *services.MoviePilotClient
	tmdb       *services.TMDBClient
}

func NewDetailHandler(
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	moviepilot *services.MoviePilotClient,
	tmdb *services.TMDBClient,
) *DetailHandler {
	return &DetailHandler{
		sessMgr:    sessMgr,
		telegram:   telegram,
		moviepilot: moviepilot,
		tmdb:       tmdb,
	}
}

func (h *DetailHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	// Check if this is a detail_seasons action
	if ctx.Callback.Action == callback.ActionDetailSeasons {
		return h.HandleSeasons(ctx)
	}

	// Get media ID and type from params
	mediaID, hasID := ctx.Callback.Params["id"]
	mediaType, hasType := ctx.Callback.Params["type"]

	if !hasID || !hasType {
		return nil, errors.InvalidInput("media ID and type are required")
	}

	// Parse TMDB ID
	tmdbID := 0
	fmt.Sscanf(mediaID, "%d", &tmdbID)
	if tmdbID == 0 {
		return nil, errors.InvalidInput("invalid media ID")
	}

	sess := h.sessMgr.GetOrCreate(ctx.UserID)

	// Check if we have cached data from search results first
	items, _, _, hasSearch := sess.GetSearchResults()
	if hasSearch {
		for _, item := range items {
			if item.ID == mediaID {
				// Use search result data - it already has all we need
				log.Printf("[DetailHandler] Using search result info for: %s", item.Title)
				return h.buildDetailFromSearch(item, mediaType, sess), nil
			}
		}
	}

	// Check if we have cached data from AI
	if cachedItem := sess.GetCachedAIItem(tmdbID); cachedItem != nil {
		return h.buildDetailFromCache(cachedItem, sess), nil
	}

	// Try TMDB API for details
	if h.tmdb != nil {
		tmdbMedia, err := h.tmdb.GetMediaByType(tmdbID, mediaType)
		if err == nil && tmdbMedia != nil {
			log.Printf("[DetailHandler] Got media info from TMDB: %s", tmdbMedia.GetTitle())
			return h.buildDetailFromTMDB(tmdbMedia, sess), nil
		}
		log.Printf("[DetailHandler] TMDB API failed: %v", err)
	}

	// If all else fails, build a simple detail page
	return h.buildSimpleDetail(tmdbID, mediaType, sess), nil
}

func (h *DetailHandler) buildDetailFromCache(item *session.AIRecommendationItem, sess *session.Session) *callback.Response {
	msg := services.NewMessageBuilder()

	// Type icon and label
	typeIcon := "🎬"
	typeLabel := "电影"
	if item.MediaType == "tv" {
		typeIcon = "📺"
		typeLabel = "剧集"
	}

	// Header with title
	msg.Bold(fmt.Sprintf("%s %s", typeIcon, item.Title)).Newline()

	// Year and rating on same line
	infoLine := ""
	if item.Year > 0 {
		infoLine = fmt.Sprintf("📅 %d年", item.Year)
	}
	if item.Rating > 0 {
		if infoLine != "" {
			infoLine += "  •  "
		}
		infoLine += fmt.Sprintf("⭐ %.1f分", item.Rating)
	}
	if infoLine != "" {
		msg.Text(infoLine).Newline()
	}

	// Media type badge
	msg.Text(fmt.Sprintf("🏷️ %s", typeLabel)).Newline()
	msg.Newline()

	// AI Reason
	if item.Reason != "" {
		msg.Bold("💭 推荐理由").Newline()
		msg.Text(item.Reason).Newline()
		msg.Newline()
	}

	// Overview (truncate if too long)
	if item.Overview != "" {
		overview := item.Overview
		if len(overview) > 150 {
			overview = overview[:150] + "..."
		}
		msg.Text(overview).Newline()
		msg.Newline()
	}

	// Request button label
	buttonLabel := "✅ 立即求片"
	if item.MediaType == "tv" {
		buttonLabel = "✅ 求剧集"
	}

	// Keyboard
	kb := services.NewKeyboardBuilder()
	kb.AddButton(buttonLabel, fmt.Sprintf("request:id:%d:type:%s", item.TmdbID, item.MediaType))
	kb.NewRow()
	kb.AddButton("⬅️ 返回列表", "back")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}
}

func (h *DetailHandler) buildDetailFromTMDB(media *services.TMDBMediaInfo, sess *session.Session) *callback.Response {
	msg := services.NewMessageBuilder()

	// Type icon
	typeIcon := "🎬"
	if media.MediaType == "tv" {
		typeIcon = "📺"
	}

	// Header
	msg.Bold(fmt.Sprintf("%s %s", typeIcon, media.GetTitle())).Newline()

	// Year and rating
	infoLine := ""
	year := media.GetYear()
	if year > 0 {
		infoLine = fmt.Sprintf("📅 %d年", year)
	}
	if media.VoteAverage > 0 {
		if infoLine != "" {
			infoLine += "  •  "
		}
		infoLine += fmt.Sprintf("⭐ %.1f分", media.VoteAverage)
	}
	if infoLine != "" {
		msg.Text(infoLine).Newline()
	}

	// Runtime
	runtime := media.GetRuntime()
	if runtime > 0 {
		hours := runtime / 60
		mins := runtime % 60
		if hours > 0 {
			msg.Text(fmt.Sprintf("⏱️ 时长: %d小时%d分钟", hours, mins))
		} else {
			msg.Text(fmt.Sprintf("⏱️ 时长: %d分钟", runtime))
		}
		msg.Newline()
	}

	// Genres
	genres := media.GetGenres()
	if genres != "" {
		msg.Text(fmt.Sprintf("🎭 %s", genres)).Newline()
	}

	msg.Newline()

	// Overview (truncate if too long)
	if media.Overview != "" {
		overview := media.Overview
		if len(overview) > 200 {
			overview = overview[:200] + "..."
		}
		msg.Text(overview).Newline()
		msg.Newline()
	}

	// TMDB ID
	msg.Text(fmt.Sprintf("🆔 TMDB ID: %d", media.ID)).Newline()

	// Request button label
	buttonLabel := "✅ 立即求片"
	if media.MediaType == "tv" {
		buttonLabel = "✅ 求剧集"
	}

	// Keyboard
	kb := services.NewKeyboardBuilder()
	kb.AddButton(buttonLabel, fmt.Sprintf("request:id:%d:type:%s", media.ID, media.MediaType))
	kb.NewRow()
	kb.AddButton("⬅️ 返回列表", "back")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}
}

func (h *DetailHandler) buildDetailFromMedia(media *services.MediaInfo, sess *session.Session) *callback.Response {
	msg := services.NewMessageBuilder()

	// Get title
	title := media.Title

	// Type icon
	typeIcon := "🎬"
	if media.Type == services.MediaTypeTV {
		typeIcon = "📺"
	}

	// Header
	msg.Bold(fmt.Sprintf("%s %s", typeIcon, title)).Newline()

	// Year and rating
	infoLine := ""
	if media.Year > 0 {
		infoLine = fmt.Sprintf("📅 %d年", media.Year)
	}
	if media.Rating > 0 {
		if infoLine != "" {
			infoLine += "  •  "
		}
		infoLine += fmt.Sprintf("⭐ %.1f分", media.Rating)
	}
	if infoLine != "" {
		msg.Text(infoLine).Newline()
	}

	// Genres - not available in MoviePilot MediaInfo
	// Skip genre display for now

	msg.Newline()

	// Overview (truncate if too long)
	if media.Overview != "" {
		overview := media.Overview
		if len(overview) > 200 {
			overview = overview[:200] + "..."
		}
		msg.Text(overview).Newline()
		msg.Newline()
	}

	// TMDB ID
	msg.Text(fmt.Sprintf("🆔 TMDB ID: %d", media.ID)).Newline()

	// Request button label
	buttonLabel := "✅ 立即求片"
	if media.Type == services.MediaTypeTV {
		buttonLabel = "✅ 求剧集"
	}

	// Keyboard
	mediaTypeStr := "movie"
	if media.Type == services.MediaTypeTV {
		mediaTypeStr = "tv"
	}
	kb := services.NewKeyboardBuilder()
	kb.AddButton(buttonLabel, fmt.Sprintf("request:id:%d:type:%s", media.ID, mediaTypeStr))
	kb.NewRow()
	kb.AddButton("⬅️ 返回列表", "back")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}
}

// buildDetailFromSearch builds detail page from search result item
func (h *DetailHandler) buildDetailFromSearch(item session.SearchItem, mediaType string, sess *session.Session) *callback.Response {
	msg := services.NewMessageBuilder()

	// Type icon and label
	typeIcon := "🎬"
	typeLabel := "电影"
	isTV := mediaType == "tv" || item.Type == "tv"
	if isTV {
		typeIcon = "📺"
		typeLabel = "剧集"
	}

	// Header with title
	msg.Bold(fmt.Sprintf("%s %s", typeIcon, item.Title)).Newline()

	// Year and rating on same line
	infoLine := ""
	if item.Year > 0 {
		infoLine = fmt.Sprintf("📅 %d年", item.Year)
	}
	if item.Rating > 0 {
		if infoLine != "" {
			infoLine += "  •  "
		}
		infoLine += fmt.Sprintf("⭐ %.1f分", item.Rating)
	}
	if infoLine != "" {
		msg.Text(infoLine).Newline()
	}

	// Media type badge
	msg.Text(fmt.Sprintf("🏷️ %s", typeLabel)).Newline()
	msg.Newline()

	// TV show specific info
	if isTV && len(item.Seasons) > 0 {
		msg.Bold(fmt.Sprintf("📺 共 %d 季", len(item.Seasons))).Newline()
		// Show season info
		for i, s := range item.Seasons {
			if i >= 3 {
				msg.Textf("   ... 还有 %d 季", len(item.Seasons)-3)
				break
			}
			seasonName := fmt.Sprintf("第%d季", s.SeasonNumber)
			if s.Name != "" {
				seasonName = s.Name
			}
			msg.Text(fmt.Sprintf("   • %s (%d集)", seasonName, s.EpisodeCount)).Newline()
		}
		msg.Newline()
	}

	// Overview (truncate if too long)
	if item.Overview != "" {
		overview := item.Overview
		if len(overview) > 200 {
			overview = overview[:200] + "..."
		}
		msg.Text(overview).Newline()
		msg.Newline()
	}

	// TMDB info
	msg.Text(fmt.Sprintf("🆔 TMDB ID: %s", item.ID)).Newline()

	// Build keyboard
	kb := services.NewKeyboardBuilder()

	if isTV && len(item.Seasons) > 0 {
		// TV show with seasons - show season selection
		kb.AddButton("✅ 订阅全季", fmt.Sprintf("request:id:%s:type:tv:season:0", item.ID))
		kb.NewRow()

		// Show first few seasons as individual buttons
		for i, s := range item.Seasons {
			if i >= 4 {
				break // Show max 4 seasons
			}
			seasonName := fmt.Sprintf("S%d", s.SeasonNumber)
			if s.SeasonNumber == 0 {
				seasonName = "特别篇"
			}
			kb.AddButton(fmt.Sprintf("📺 %s", seasonName), fmt.Sprintf("request:id:%s:type:tv:season:%d", item.ID, s.SeasonNumber))
			if (i+1)%2 == 0 {
				kb.NewRow()
			}
		}
		if len(item.Seasons) > 4 {
			kb.AddButton(fmt.Sprintf("更多... (%d季)", len(item.Seasons)), fmt.Sprintf("detail_seasons:id:%s", item.ID))
		}
		kb.NewRow()
	} else {
		// Movie or TV show without season info
		buttonLabel := "✅ 立即求片"
		if isTV {
			buttonLabel = "✅ 订阅剧集"
		}
		kb.AddButton(buttonLabel, fmt.Sprintf("request:id:%s:type:%s", item.ID, item.Type))
		kb.NewRow()
	}

	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}
}

// buildSimpleDetail builds a simple detail page when API fails
func (h *DetailHandler) buildSimpleDetail(tmdbID int, mediaType string, sess *session.Session) *callback.Response {
	msg := services.NewMessageBuilder()

	// Type icon and label
	typeIcon := "🎬"
	typeLabel := "电影"
	if mediaType == "tv" {
		typeIcon = "📺"
		typeLabel = "剧集"
	}

	// Header
	msg.Bold(fmt.Sprintf("%s 影片详情", typeIcon)).Newline().Newline()

	// TMDB ID
	msg.Text(fmt.Sprintf("🆔 TMDB ID: %d", tmdbID)).Newline()
	msg.Text(fmt.Sprintf("🏷️ 类型: %s", typeLabel)).Newline()
	msg.Newline()

	// Note
	msg.Italic("💡 信息暂不可用，但仍可发起请求").Newline().Newline()

	// Request button label
	buttonLabel := "✅ 立即求片"
	if mediaType == "tv" {
		buttonLabel = "✅ 求剧集"
	}

	// Keyboard
	kb := services.NewKeyboardBuilder()
	kb.AddButton(buttonLabel, fmt.Sprintf("request:id:%d:type:%s", tmdbID, mediaType))
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}
}

// HandleSeasons handles the detail_seasons action - shows all seasons for a TV show
func (h *DetailHandler) HandleSeasons(ctx *callback.Context) (*callback.Response, error) {
	// Get media ID from params
	mediaID, hasID := ctx.Callback.Params["id"]
	if !hasID {
		return &callback.Response{
			Text:        "❌ 请求无效",
			CallbackMsg: "参数错误",
			ShowAlert:   true,
		}, nil
	}

	sess := h.sessMgr.GetOrCreate(ctx.UserID)

	// Try to find the item in search results
	items, _, _, hasSearch := sess.GetSearchResults()
	if !hasSearch {
		return &callback.Response{
			Text:        "⏰ 搜索结果已过期，请重新搜索",
			CallbackMsg: "结果已过期",
			ShowAlert:   true,
		}, nil
	}

	var targetItem *session.SearchItem
	for i := range items {
		if items[i].ID == mediaID {
			targetItem = &items[i]
			break
		}
	}

	if targetItem == nil {
		return &callback.Response{
			Text:        "⏰ 未找到该媒体信息",
			CallbackMsg: "未找到",
			ShowAlert:   true,
		}, nil
	}

	// Check if it's a TV show with seasons
	if targetItem.Type != "tv" || len(targetItem.Seasons) == 0 {
		return &callback.Response{
			Text:        "⏰ 该媒体没有季信息",
			CallbackMsg: "无季信息",
			ShowAlert:   true,
		}, nil
	}

	// Build seasons list page
	msg := services.NewMessageBuilder()
	msg.Bold(fmt.Sprintf("📺 %s - 全部季", targetItem.Title)).Newline()
	msg.Newline()
	msg.Text(fmt.Sprintf("共 %d 季", len(targetItem.Seasons))).Newline()
	msg.Newline()

	kb := services.NewKeyboardBuilder()

	// List all seasons with buttons
	for i, season := range targetItem.Seasons {
		seasonName := fmt.Sprintf("第%d季", season.SeasonNumber)
		if season.SeasonNumber == 0 {
			seasonName = "特别篇"
		}
		if season.Name != "" && season.Name != seasonName {
			seasonName = fmt.Sprintf("%s - %s", seasonName, season.Name)
		}

		msg.Text(fmt.Sprintf("%d. %s (%d集)", i+1, seasonName, season.EpisodeCount)).Newline()

		// Add button for each season
		buttonLabel := fmt.Sprintf("📺 S%d", season.SeasonNumber)
		if season.SeasonNumber == 0 {
			buttonLabel = "📺 特别篇"
		}
		kb.AddButton(buttonLabel, fmt.Sprintf("request:id:%s:type:tv:season:%d", targetItem.ID, season.SeasonNumber))

		// Two buttons per row
		if (i+1)%2 == 0 {
			kb.NewRow()
		}
	}

	if len(targetItem.Seasons)%2 != 0 {
		kb.NewRow()
	}

	// Add "Subscribe All" button
	kb.AddButton("✅ 订阅全季", fmt.Sprintf("request:id:%s:type:tv:season:0", targetItem.ID))
	kb.NewRow()

	// Back to detail button
	kb.AddButton("⬅️ 返回详情", fmt.Sprintf("detail:id:%s:type:tv", targetItem.ID))
	kb.NewRow()
	kb.AddButton("🏠 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// BackHandler handles back navigation
type BackHandler struct {
	sessMgr      *session.Manager
	adminService *services.AdminService
}

func NewBackHandler(sessMgr *session.Manager) *BackHandler {
	return &BackHandler{sessMgr: sessMgr}
}

// SetAdminService sets the admin service
func (h *BackHandler) SetAdminService(adminSvc *services.AdminService) {
	h.adminService = adminSvc
}

func (h *BackHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	sess := h.sessMgr.GetOrCreate(ctx.UserID)

	entry, hasHistory := sess.PopNavEntry()
	if !hasHistory {
		// No history, show start menu
		isAdmin := h.adminService != nil && h.adminService.IsAdmin(ctx.UserID)
		return &callback.Response{
			Text:     "🌟 欢迎回来",
			Edit:     true,
			Keyboard: convertKeyboard(services.BuildStartKeyboard(isAdmin)),
		}, nil
	}

	// Restore previous view
	log.Printf("[BackHandler] Restoring view: source=%s", entry.Source)

	// Based on source, restore appropriate view
	switch entry.Source {
	case "ai_trending", "trending":
		return &callback.Response{
			Text:        "🔄 加载中...",
			CallbackMsg: "加载中",
			ShowAlert:   true,
		}, nil
	case "ai_hot", "hot":
		return &callback.Response{
			Text:        "🔄 加载中...",
			CallbackMsg: "加载中",
			ShowAlert:   true,
		}, nil
	case "ai_new", "new":
		return &callback.Response{
			Text:        "🔄 加载中...",
			CallbackMsg: "加载中",
			ShowAlert:   true,
		}, nil
	default:
		isAdmin := h.adminService != nil && h.adminService.IsAdmin(ctx.UserID)
		return &callback.Response{
			Text:     "🌟 欢迎回来",
			Edit:     true,
			Keyboard: convertKeyboard(services.BuildStartKeyboard(isAdmin)),
		}, nil
	}
}

// Helper function to convert keyboard types
func convertKeyboard(tk *types.TelegramInlineKeyboard) *callback.Keyboard {
	if tk == nil {
		return &callback.Keyboard{InlineKeyboard: [][]callback.Button{}}
	}

	buttons := make([][]callback.Button, len(tk.InlineKeyboard))
	for i, row := range tk.InlineKeyboard {
		buttons[i] = make([]callback.Button, len(row))
		for j, btn := range row {
			buttons[i][j] = callback.Button{
				Text:         btn.Text,
				CallbackData: btn.CallbackData,
				URL:          btn.URL,
			}
		}
	}

	return &callback.Keyboard{InlineKeyboard: buttons}
}

// CancelHandler handles cancel action
type CancelHandler struct{}

func NewCancelHandler() *CancelHandler {
	return &CancelHandler{}
}

func (h *CancelHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	return &callback.Response{
		Text:     "✖️ 已取消",
		Edit:     true,
		Keyboard: &callback.Keyboard{},
	}, nil
}
