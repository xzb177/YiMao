package handlers

import (
	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/config"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
	"emby-telegram-bot/pkg/errors"
)

// MyRequestsHandler handles "my requests" callbacks
type MyRequestsHandler struct {
	sessMgr    *session.Manager
	telegram   *services.TelegramClient
	jellyseerr *services.JellyseerrClient
}

func NewMyRequestsHandler(
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	jellyseerr *services.JellyseerrClient,
) *MyRequestsHandler {
	return &MyRequestsHandler{
		sessMgr:    sessMgr,
		telegram:   telegram,
		jellyseerr: jellyseerr,
	}
}

func (h *MyRequestsHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	// Get Jellyseerr user ID from session
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	jellyseerrID := sess.GetJellyseerrUserID()
	if jellyseerrID == 0 {
		return &callback.Response{
			Text: "❌ 请先使用 /link 命令绑定 Jellyseerr 账号",
			Edit: true,
			Keyboard: &callback.Keyboard{
				InlineKeyboard: [][]callback.Button{
					{{Text: "🔗 绑定账号", CallbackData: "link"}},
					{{Text: "⬅️ 返回", CallbackData: "start"}},
				},
			},
		}, nil
	}

	// Fetch user requests
	requests, err := h.jellyseerr.GetUserRequests(jellyseerrID, 1)
	if err != nil {
		return nil, errors.JellyseerrErr("failed to get requests", err)
	}

	// Build response message
	msg := services.NewMessageBuilder()
	msg.Bold("📋 我的请求").Newline()
	msg.Newline()

	if len(requests) == 0 {
		msg.Text("暂无请求记录")
	} else {
		msg.Textf("共有 %d 条请求", len(requests)).Newline()
		msg.Newline()

		for i, req := range requests {
			if i >= 10 {
				msg.Textf("... 还有 %d 条请求", len(requests)-10)
				break
			}

			// Status icon
			statusIcon := "⏳"
			switch req.Status {
			case "pending":
				statusIcon = "⏳"
			case "approved":
				statusIcon = "✅"
			case "available":
				statusIcon = "🎉"
			case "declined":
				statusIcon = "❌"
			}

			// Media title
			title := req.Media.Title
			if title == "" {
				title = req.Media.Name
			}

			msg.Textf("%s %s", statusIcon, title)
			if req.Media.MediaType == "tv" {
				msg.Text(" (剧集)")
			}
			msg.Newline()
		}
	}

	// Build keyboard
	kb := services.NewKeyboardBuilder()
	kb.AddButton("🔄 刷新", "requests:refresh")
	kb.NewRow()
	kb.AddButton("⬅️ 返回", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// LinkHandler handles account linking callbacks
type LinkHandler struct {
	cfg       *config.Config
	sessMgr   *session.Manager
	telegram  *services.TelegramClient
}

func NewLinkHandler(
	cfg *config.Config,
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
) *LinkHandler {
	return &LinkHandler{
		cfg:      cfg,
		sessMgr:  sessMgr,
		telegram: telegram,
	}
}

func (h *LinkHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	msg := services.NewMessageBuilder()
	msg.Bold("🔗 绑定 Jellyseerr 账号").Newline()
	msg.Newline()
	msg.Text("请发送您的 Jellyseerr 用户名进行绑定").Newline()
	msg.Newline()
	msg.Italic("💡 您的 Jellyseerr 用户名可以在设置页面查看")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: &callback.Keyboard{
			InlineKeyboard: [][]callback.Button{
				{{Text: "⬅️ 返回", CallbackData: "start"}},
			},
		},
	}, nil
}

// HelpHandler handles help callbacks
type HelpHandler struct{}

func NewHelpHandler() *HelpHandler {
	return &HelpHandler{}
}

func (h *HelpHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	msg := services.NewMessageBuilder()
	msg.Bold("❓ 帮助中心").Newline()
	msg.Newline()

	msg.Bold("🔍 搜索影片").Newline()
	msg.Text("  直接输入影片名称或使用 /search 命令").Newline()
	msg.Newline()

	msg.Bold("🤖 AI 推荐").Newline()
	msg.Text("  使用智能推荐发现好片").Newline()
	msg.Newline()

	msg.Bold("📋 我的请求").Newline()
	msg.Text("  查看您的媒体请求状态").Newline()
	msg.Newline()

	msg.Bold("🔗 绑定账号").Newline()
	msg.Text("  绑定您的 Jellyseerr 账号以使用请求功能").Newline()
	msg.Newline()

	msg.Bold("⌨️ 命令列表").Newline()
	msg.Text("  /start - 开始使用").Newline()
	msg.Text("  /search - 搜索影片").Newline()
	msg.Text("  /ai - AI 推荐").Newline()
	msg.Text("  /trending - 热门榜单").Newline()
	msg.Text("  /requests - 我的请求").Newline()
	msg.Text("  /link - 绑定账号").Newline()
	msg.Text("  /help - 帮助信息").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("⬅️ 返回", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}
