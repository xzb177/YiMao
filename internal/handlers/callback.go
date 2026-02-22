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
	jellyseerr  *services.JellyseerrClient
}

func NewStartHandler(
	cfg *config.Config,
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	jellyseerr *services.JellyseerrClient,
) *StartHandler {
	return &StartHandler{
		cfg:        cfg,
		sessMgr:    sessMgr,
		telegram:   telegram,
		jellyseerr: jellyseerr,
	}
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

	keyboard := services.BuildStartKeyboard()

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
	jellyseerr *services.JellyseerrClient
}

func NewDetailHandler(
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	jellyseerr *services.JellyseerrClient,
) *DetailHandler {
	return &DetailHandler{
		sessMgr:    sessMgr,
		telegram:   telegram,
		jellyseerr: jellyseerr,
	}
}

func (h *DetailHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	// Get media ID and type from params
	mediaID, hasID := ctx.Callback.Params["id"]
	_, hasType := ctx.Callback.Params["type"]

	if !hasID || !hasType {
		return nil, errors.InvalidInput("media ID and type are required")
	}

	// Parse TMDB ID
	tmdbID := 0
	fmt.Sscanf(mediaID, "%d", &tmdbID)
	if tmdbID == 0 {
		return nil, errors.InvalidInput("invalid media ID")
	}

	// Check if we have cached data from AI
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	if cachedItem := sess.GetCachedAIItem(tmdbID); cachedItem != nil {
		return h.buildDetailFromCache(cachedItem, sess), nil
	}

	// Fetch from Jellyseerr
	media, err := h.jellyseerr.GetMediaInfo(tmdbID)
	if err != nil {
		return nil, errors.MediaNotFound(fmt.Sprintf("failed to get media info: %v", err))
	}

	return h.buildDetailFromMedia(media, sess), nil
}

func (h *DetailHandler) buildDetailFromCache(item *session.AIRecommendationItem, sess *session.Session) *callback.Response {
	msg := services.NewMessageBuilder()

	// Header
	msg.Bold(fmt.Sprintf("%s (%d)", item.Title, item.Year))
	if item.Rating > 0 {
		msg.Textf(" ⭐ %.1f", item.Rating)
	}
	msg.Newline()
	msg.Newline()

	// AI Reason
	if item.Reason != "" {
		msg.Italic("💭 " + item.Reason).Newline()
		msg.Newline()
	}

	// Overview
	if item.Overview != "" {
		msg.Text(item.Overview).Newline()
	}

	// Quota info
	quotaText := "📊 今日配额：充足"
	if q, _ := sess.GetString("quota_status"); q != "" {
		quotaText = q
	}
	msg.Newline().Newline()
	msg.Text(quotaText)

	// Keyboard
	kb := services.NewKeyboardBuilder()
	kb.AddButton("✅ 确认请求", fmt.Sprintf("request:id:%d:type:%s", item.TmdbID, item.MediaType))
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

	// Header
	title := media.Title
	if title == "" {
		title = media.Name
	}
	year := ""
	if media.ReleaseDate != "" && len(media.ReleaseDate) >= 4 {
		year = media.ReleaseDate[:4]
	}

	msg.Bold(fmt.Sprintf("%s", title))
	if year != "" {
		msg.Textf(" (%s)", year)
	}
	if media.VoteAverage > 0 {
		msg.Textf(" ⭐ %.1f", media.VoteAverage)
	}
	msg.Newline()
	msg.Newline()

	// Details
	if media.Runtime > 0 {
		msg.Text(fmt.Sprintf("⏱️ 时长：%d分钟", media.Runtime))
	}
	if len(media.Genres) > 0 {
		msg.Text("  🎭 类型：")
		for i, g := range media.Genres {
			if i > 0 {
				msg.Text(" / ")
			}
			msg.Text(g.Name)
		}
	}
	msg.Newline()

	// Overview
	if media.Overview != "" {
		msg.Newline()
		msg.Text(media.Overview)
	}

	// Keyboard
	kb := services.NewKeyboardBuilder()
	kb.AddButton("✅ 请求", fmt.Sprintf("request:id:%d:type:%s", media.ID, media.MediaType))
	kb.NewRow()
	kb.AddButton("⬅️ 返回", "back")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}
}

// BackHandler handles back navigation
type BackHandler struct {
	sessMgr *session.Manager
}

func NewBackHandler(sessMgr *session.Manager) *BackHandler {
	return &BackHandler{sessMgr: sessMgr}
}

func (h *BackHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	sess := h.sessMgr.GetOrCreate(ctx.UserID)

	entry, hasHistory := sess.PopNavEntry()
	if !hasHistory {
		// No history, show start menu
		return &callback.Response{
			Text:     "🌟 欢迎回来",
			Edit:     true,
			Keyboard: convertKeyboard(services.BuildStartKeyboard()),
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
		return &callback.Response{
			Text:     "🌟 欢迎回来",
			Edit:     true,
			Keyboard: convertKeyboard(services.BuildStartKeyboard()),
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
