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
	case callback.ActionTrending:
		return h.HandleTrending(ctx)
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
	msg.Bold("🌟 欢迎使用 Emby Telegram Bot").Newline()
	msg.Newline()
	msg.Text("请选择操作：").Newline()

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
	msg.Text("请输入影片名称进行搜索").Newline()
	msg.Newline()
	msg.Italic("💡 提示：直接输入影片名称即可开始搜索")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: &callback.Keyboard{},
	}, nil
}

func (h *StartHandler) HandleAI(ctx *callback.Context) (*callback.Response, error) {
	if !h.cfg.EnableAI {
		return &callback.Response{
			Text:        "❌ AI 推荐功能未启用",
			CallbackMsg: "功能未启用",
			ShowAlert:   true,
		}, nil
	}

	msg := services.NewMessageBuilder()
	msg.Bold("🤖 AI 智能推荐").Newline()
	msg.Newline()
	msg.Text("请选择推荐类型：").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🔥 热门推荐", "ai:trending")
	kb.AddButton("📺 热门剧集", "ai:hot")
	kb.NewRow()
	kb.AddButton("🆕 新片上线", "ai:new")
	kb.AddButton("🎲 随机推荐", "ai:random")
	kb.NewRow()
	kb.AddButton("⬅️ 返回", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

func (h *StartHandler) HandleTrending(ctx *callback.Context) (*callback.Response, error) {
	return &callback.Response{
		Text:        "🔥 正在加载热门榜单...",
		CallbackMsg: "加载中",
		ShowAlert:   true,
	}, nil
}

func (h *StartHandler) HandleHot(ctx *callback.Context) (*callback.Response, error) {
	return &callback.Response{
		Text:        "📺 正在加载热门剧集...",
		CallbackMsg: "加载中",
		ShowAlert:   true,
	}, nil
}

func (h *StartHandler) HandleNew(ctx *callback.Context) (*callback.Response, error) {
	return &callback.Response{
		Text:        "🆕 正在加载新片...",
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
		msg.Bold("💭 AI推荐理由").Newline()
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
	kb.AddButton("⬅️ 返回", "back")

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
	kb.AddButton("⬅️ 返回", "back")

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
	if mediaType == "tv" || item.Type == "tv" {
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

	// Request button label
	buttonLabel := "✅ 立即求片"
	if mediaType == "tv" || item.Type == "tv" {
		buttonLabel = "✅ 求剧集"
	}

	// Keyboard
	kb := services.NewKeyboardBuilder()
	kb.AddButton(buttonLabel, fmt.Sprintf("request:id:%s:type:%s", item.ID, item.Type))
	kb.NewRow()
	kb.AddButton("⬅️ 返回搜索", "start")

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
	msg.Italic("💡 完整信息暂时无法获取，但您仍然可以发起请求").Newline().Newline()

	// Request button label
	buttonLabel := "✅ 立即求片"
	if mediaType == "tv" {
		buttonLabel = "✅ 求剧集"
	}

	// Keyboard
	kb := services.NewKeyboardBuilder()
	kb.AddButton(buttonLabel, fmt.Sprintf("request:id:%d:type:%s", tmdbID, mediaType))
	kb.NewRow()
	kb.AddButton("⬅️ 返回菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}
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
			Text:        "🔥 正在重新加载...",
			CallbackMsg: "加载中",
			ShowAlert:   true,
		}, nil
	case "ai_hot", "hot":
		return &callback.Response{
			Text:        "📺 正在重新加载...",
			CallbackMsg: "加载中",
			ShowAlert:   true,
		}, nil
	case "ai_new", "new":
		return &callback.Response{
			Text:        "🆕 正在重新加载...",
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
		Text:     "❌ 已取消",
		Edit:     true,
		Keyboard: &callback.Keyboard{},
	}, nil
}
