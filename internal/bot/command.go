package bot

import (
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
	quotaService *services.QuotaService,
	userMapping *services.UserMappingService,
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
		HandleLinkCommand(telegram, msg, bindingRequest, cfg, userMapping)
	case "/quota":
		HandleQuotaCommand(telegram, msg, quotaService)
	// Unknown commands are silently ignored
	}
}

// HandleLinkCommand handles /link command with optional username and password
func HandleLinkCommand(telegram *services.TelegramClient, msg *types.TelegramMessage, bindingRequest *services.BindingRequestService, cfg *config.Config, userMapping *services.UserMappingService) {
	parts := strings.Fields(msg.Text)

	if len(parts) == 1 {
		text := "🔗 绑定 MoviePilot 账号\n\n请使用以下命令绑定您的账号：\n\n/link 用户名 密码\n\n示例：\n/link johndoe mypassword123\n\n💡 您的凭据将直接发送到 MoviePilot 服务器进行验证，新用户将自动注册"
		telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
		return
	}

	if len(parts) < 3 {
		text := "❌ 参数不足\n\n格式: /link 用户名 密码\n\n示例: /link johndoe mypassword123"
		telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
		return
	}

	username := parts[1]
	password := strings.Join(parts[2:], " ")

	// Verify credentials with MoviePilot
	mpClient := services.NewMoviePilotClient(cfg.MoviePilotURL, cfg.MoviePilotAPIKey)
	userID, err := mpClient.Authenticate(username, password)
	if err != nil {
		log.Printf("[LinkCommand] Authentication failed for %s: %v", username, err)
		text := "❌ 绑定失败：用户名或密码错误"
		telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
		return
	}

	// Check if userID is valid (not 0)
	if userID == 0 {
		log.Printf("[LinkCommand] Authentication returned invalid userID 0 for %s", username)
		text := "❌ 绑定失败：无法获取用户ID，请稍后重试"
		telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
		return
	}

	// Save mapping using the provided userMapping service
	if err := userMapping.AddMapping(msg.From.ID, userID, username); err != nil {
		log.Printf("[LinkCommand] Failed to save mapping: %v", err)
		text := "❌ 绑定失败：无法保存映射"
		telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
		return
	}

	log.Printf("[LinkCommand] User %d bound to MoviePilot ID %d", msg.From.ID, userID)
	text := "✅ 绑定成功\n\n您现在可以使用求片功能了"
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

// HandleQuotaCommand handles /quota command
func HandleQuotaCommand(telegram *services.TelegramClient, msg *types.TelegramMessage, quotaService *services.QuotaService) {
	log.Printf("[QuotaCommand] Handling /quota for user %d, quotaService=%v", msg.From.ID, quotaService != nil)

	if quotaService == nil {
		log.Printf("[QuotaCommand] QuotaService is nil!")
		telegram.SendMessage(msg.Chat.ID, "❌ 配额服务未启用", "", nil)
		return
	}

	userID := msg.From.ID
	quotaText := quotaService.GetQuotaText(userID)
	log.Printf("[QuotaCommand] Sending quota text to user %d", userID)
	telegram.SendMessage(msg.Chat.ID, quotaText, "Markdown", nil)
}
