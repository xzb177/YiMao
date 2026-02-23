package handlers

import (
	"fmt"
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

			// Use state text helper from MoviePilot service
			statusText := services.GetStateText(req.State)

			// Media title and year
			title := req.Name
			if req.Year != "" && req.Year != "0" {
				title = fmt.Sprintf("%s (%s)", title, req.Year)
			}

			// Type emoji
			typeEmoji := "🎬"
			if req.Type == "电视剧" || req.Type == "tv" {
				typeEmoji = "📺"
			}

			// Add season info for TV shows
			if req.Season > 0 {
				title = fmt.Sprintf("%s S%d", title, req.Season)
			}

			msg.Textf("%d. %s %s", i+1, typeEmoji, title)
			msg.Newline()
			msg.Text("   ").Italic(statusText).Newline()

			// Show episode count for TV shows
			if req.TotalEpisode > 0 {
				msg.Textf("   📺 共 %d 集", req.TotalEpisode).Newline()
			}
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
	msg.Bold("🌟 功能介绍").Newline()
	msg.Newline()

	msg.Bold("🔍 智能搜索").Newline()
	msg.Text("  直接输入影片名称即可搜索，支持中文/英文").Newline()
	msg.Text("  可搜索电影、电视剧、演员、导演等").Newline()
	msg.Text("  💡 示例：输入「沙丘」或「Dune」").Newline()
	msg.Newline()

	msg.Bold("🤖 AI 智能推荐").Newline()
	msg.Text("  基于大数据分析，为您精选优质内容").Newline()
	msg.Text("  🔥 热门电影 — 当前热播的高分影片").Newline()
	msg.Text("  📺 热播剧集 — 追剧必备持续更新").Newline()
	msg.Text("  ⭐ 高分佳作 — 经典高分电影必看").Newline()
	msg.Text("  🆕 最新上线 — 最新上映抢先看").Newline()
	msg.Text("  🎲 随机发现 — 为您随机挑选佳作").Newline()
	msg.Newline()

	msg.Bold("📋 请求管理").Newline()
	msg.Text("  一键求片，系统自动处理并入库").Newline()
	msg.Text("  实时跟踪请求状态：等待→处理中→已入库").Newline()
	msg.Text("  支持电影和电视剧订阅，单季全集都能下").Newline()
	msg.Newline()

	msg.Bold("🔗 账号绑定").Newline()
	msg.Text("  绑定 MoviePilot 账号后可：").Newline()
	msg.Text("  • 查看「我的请求」订阅列表").Newline()
	msg.Text("  • 获取更准确的推荐和搜索结果").Newline()
	msg.Text("  💡 绑定格式：/link 用户名 密码").Newline()
	msg.Newline()

	msg.Bold("💎 配额系统").Newline()
	msg.Text("  每日有请求次数限制，管理员无限制").Newline()
	msg.Text("  /quota 查看剩余配额").Newline()
	msg.Newline()

	msg.Bold("⌨️ 快捷命令").Newline()
	msg.Text("  /start — 打开主菜单").Newline()
	msg.Text("  /search — 搜索影片").Newline()
	msg.Text("  /ai — AI 推荐菜单").Newline()
	msg.Text("  /requests — 我的请求").Newline()
	msg.Text("  /link — 绑定账号").Newline()
	msg.Text("  /quota — 查看配额").Newline()
	msg.Text("  /help — 显示此帮助").Newline()
	msg.Newline()

	msg.Italic("💬 遇到问题？联系管理员获取帮助").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
			Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}
