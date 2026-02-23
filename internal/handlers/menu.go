package handlers

import (
	"log"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
	"emby-telegram-bot/pkg/errors"
)

// MyRequestsHandler handles "my requests" callbacks
type MyRequestsHandler struct {
	sessMgr     *session.Manager
	telegram    *services.TelegramClient
	moviepilot  *services.MoviePilotClient
	userMapping *services.UserMappingService
}

func NewMyRequestsHandler(
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	moviepilot *services.MoviePilotClient,
) *MyRequestsHandler {
	return &MyRequestsHandler{
		sessMgr:    sessMgr,
		telegram:   telegram,
		moviepilot: moviepilot,
	}
}

// SetUserMapping sets the user mapping service
func (h *MyRequestsHandler) SetUserMapping(um *services.UserMappingService) {
	h.userMapping = um
}

func (h *MyRequestsHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	// Try to get MoviePilot user ID from session first
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	moviepilotID := sess.GetMoviePilotUserID()

	// If not in session, try to get from user mapping (persistent storage)
	if moviepilotID == 0 && h.userMapping != nil {
		if id, exists := h.userMapping.GetMoviePilotUserID(ctx.UserID); exists {
			moviepilotID = id
			// Update session for future requests
			sess.Set("moviepilot_id", int(id))
			log.Printf("[MyRequestsHandler] Loaded moviepilot_id=%d from userMapping for user %d", id, ctx.UserID)
		} else {
			log.Printf("[MyRequestsHandler] No moviepilot_id found in userMapping for user %d", ctx.UserID)
		}
	}

	log.Printf("[MyRequestsHandler] User %d: moviepilotID=%d", ctx.UserID, moviepilotID)

	if moviepilotID == 0 {
		return &callback.Response{
			Text: "❌ 请先使用 /link 命令绑定 MoviePilot 账号",
			Edit: true,
			Keyboard: &callback.Keyboard{
				InlineKeyboard: [][]callback.Button{
					{{Text: "🔗 绑定账号", CallbackData: "link"}},
					{{Text: "⬅️ 返回主菜单", CallbackData: "start"}},
				},
			},
		}, nil
	}

	// Fetch user requests
	requests, err := h.moviepilot.GetUserRequests(moviepilotID)
	if err != nil {
		return nil, errors.MoviePilotErr("failed to get requests", err)
	}

	// Build response message
	msg := services.NewMessageBuilder()
	msg.Bold("📋 我的请求").Newline()
	msg.Newline()

	if len(requests) == 0 {
		msg.Text("暂无请求记录").Newline()
		msg.Newline()
		msg.Italic("💡 搜索影片后点击「请求」即可添加求片")
	} else {
		msg.Textf("共有 %d 条请求记录", len(requests)).Newline()
		msg.Newline()

		for i, req := range requests {
			if i >= 10 {
				msg.Textf("... 还有 %d 条请求", len(requests)-10)
				break
			}

			// Status icon
			statusIcon := "⏳"
			statusText := "等待处理"
			switch req.Status {
			case "pending":
				statusIcon = "⏳"
				statusText = "等待处理"
			case "approved":
				statusIcon = "✅"
				statusText = "已批准"
			case "available":
				statusIcon = "🎉"
				statusText = "已就绪"
			case "declined":
				statusIcon = "❌"
				statusText = "已拒绝"
			}

			// Media title
			title := req.Media.Title

			msg.Textf("%s %s — %s", statusIcon, title, statusText)
			if req.Media.Type == services.MediaTypeTV {
				msg.Text(" (剧集)")
			}
			msg.Newline()
		}
	}

	// Build keyboard
	kb := services.NewKeyboardBuilder()
	kb.AddButton("🔄 刷新状态", "requests:refresh")
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
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
	msg.Bold("✨ 功能介绍").Newline()
	msg.Newline()

	msg.Bold("🔍 智能搜索").Newline()
	msg.Text("  支持片名、演员、导演等多种搜索方式").Newline()
	msg.Newline()

	msg.Bold("🤖 AI 推荐").Newline()
	msg.Text("  大数据分析为您推荐热门好片").Newline()
	msg.Newline()

	msg.Bold("📋 请求管理").Newline()
	msg.Text("  一键求片，实时跟踪处理进度").Newline()
	msg.Newline()

	msg.Bold("🔗 账号绑定").Newline()
	msg.Text("  绑定 MoviePilot 同步观影记录").Newline()
	msg.Newline()

	msg.Bold("⌨️ 快捷命令").Newline()
	msg.Text("  /start — 打开主菜单").Newline()
	msg.Text("  /search — 搜索影片").Newline()
	msg.Text("  /ai — AI 推荐菜单").Newline()
	msg.Text("  /requests — 我的请求").Newline()
	msg.Text("  /link — 绑定账号").Newline()
	msg.Text("  /help — 显示此帮助").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}
