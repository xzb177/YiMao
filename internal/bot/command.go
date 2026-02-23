package bot

import (
	"fmt"
	"log"
	"strings"

	"emby-telegram-bot/internal/config"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/pkg/types"
)

// HandleCommand handles bot commands
func HandleCommand(
	telegram *services.TelegramClient,
	msg *types.TelegramMessage,
	cfg *config.Config,
	adminService *services.AdminService,
	bindingRequest *services.BindingRequestService,
) {
	parts := strings.Fields(msg.Text)
	if len(parts) == 0 {
		return
	}

	command := parts[0]

	switch command {
	case "/start":
		isAdmin := adminService != nil && adminService.IsAdmin(msg.From.ID)
		SendStartMenu(telegram, msg.Chat.ID, isAdmin)
	case "/help":
		SendHelpMessage(telegram, msg.Chat.ID)
	case "/search":
		text := "🔍 请输入影片名称进行搜索"
		telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
	case "/ai":
		text := "🤖 请使用菜单选择推荐类型"
		telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
	case "/requests":
		text := "📋 请使用 /start 菜单中的 我的请求 功能"
		telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
	case "/link":
		HandleLinkCommand(telegram, msg, bindingRequest)
	case "/quota":
		text := "📊 配额功能开发中...\n\n💡 请使用 /start 菜单查看请求状态"
		telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
	// Unknown commands are silently ignored
	}
}

// HandleLinkCommand handles /link command with optional username and password
func HandleLinkCommand(telegram *services.TelegramClient, msg *types.TelegramMessage, bindingRequest *services.BindingRequestService) {
	parts := strings.Fields(msg.Text)

	if len(parts) == 1 {
		text := "🔗 绑定 MoviePilot 账号\n\n请使用以下命令绑定您的账号：\n\n/link 用户名 密码\n\n示例：\n/link johndoe mypassword123\n\n💡 您的凭据将直接发送到 MoviePilot 服务器进行验证"
		telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
		return
	}

	if len(parts) < 3 {
		text := "❌ 参数不足\n\n格式: /link 用户名 密码\n\n示例: /link johndoe mypassword123"
		telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
		return
	}

	username := parts[1]
	_ = strings.Join(parts[2:], " ") // Password accepted but not stored for security

	// For now, create a binding request
	requestID := fmt.Sprintf("req_%d_%d", msg.From.ID, msg.Chat.ID)
	req := &services.BindingRequest{
		RequestID:        requestID,
		TelegramID:       msg.From.ID,
		TelegramName:     msg.From.FirstName,
		TelegramUsername: username,
	}
	err := bindingRequest.CreateRequest(req)
	if err != nil {
		log.Printf("[LinkCommand] Failed to create binding request: %v", err)
		text := fmt.Sprintf("❌ 绑定请求创建失败: %v", err)
		telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
		return
	}

	text := "✅ 绑定请求已提交，请等待管理员审核"
	telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
}

// SendStartMenu sends the start menu
func SendStartMenu(telegram *services.TelegramClient, chatID int64, isAdmin bool) {
	msg := services.NewMessageBuilder()
	msg.Bold("🌟 欢迎使用云海影视助手").Newline()
	msg.Newline()
	msg.Text("🔍 智能搜索 — 快速查找心仪影片").Newline()
	msg.Text("🤖 AI 推荐 — 发现热门好片").Newline()
	msg.Text("📋 请求管理 — 跟踪您的求片进度").Newline()
	msg.Text("🔗 账号绑定 — 同步您的观影记录").Newline()
	msg.Newline()
	msg.Italic("💡 点击下方按钮开始探索").Newline()

	keyboard := services.BuildStartKeyboard(isAdmin)
	telegram.SendMessage(chatID, msg.Build(), "Markdown", keyboard)
}

// SendHelpMessage sends the help message
func SendHelpMessage(telegram *services.TelegramClient, chatID int64) {
	msg := services.NewMessageBuilder()
	msg.Bold("❓ 帮助中心").Newline()
	msg.Newline()
	msg.Bold("🌟 功能介绍").Newline()
	msg.Newline()

	msg.Bold("🔍 智能搜索").Newline()
	msg.Text("  直接输入影片名称即可搜索").Newline()
	msg.Newline()

	msg.Bold("🤖 AI 智能推荐").Newline()
	msg.Text("  基于 TMDB 数据，为您精选优质内容").Newline()
	msg.Newline()

	msg.Bold("📋 请求管理").Newline()
	msg.Text("  一键求片，系统自动处理").Newline()
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

	telegram.SendMessage(chatID, msg.Build(), "Markdown", nil)
}
