package bot

import (
	"fmt"
	"log"
	"strings"

	"emby-telegram-bot/internal/config"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/ui"
	"emby-telegram-bot/pkg/types"
	"emby-telegram-bot/pkg/validation"
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
	log.Printf("[Command] Received command: %s from user %d", msg.Text, msg.From.ID)
	parts := strings.Fields(msg.Text)
	if len(parts) == 0 {
		return
	}

	command := parts[0]
	log.Printf("[Command] Parsed command: %s", command)

	switch command {
	case "/start":
		isAdmin := adminService != nil && adminService.IsAdmin(msg.From.ID)
		SendStartMenu(telegram, msg.Chat.ID, isAdmin)
	case "/help":
		SendHelpMessage(telegram, msg.Chat.ID)
	case "/id":
		text := fmt.Sprintf("📋 当前聊天信息\n\n聊天 ID: <code>%d</code>\n聊天类型: %s\n用户 ID: <code>%d</code>", msg.Chat.ID, msg.Chat.Type, msg.From.ID)
		telegram.SendMessage(msg.Chat.ID, text, "HTML", nil)
	case "/search":
		text := "🔍 请输入影片名称进行搜索"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
	case "/ai":
		sendRecommendationMenu(telegram, msg.Chat.ID)
	case "/requests":
		text := "📋 请使用 /start 菜单中的 我的请求 功能"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
	case "/watchlist":
		text := "📎 请使用 /start 菜单中的 我的片单 功能"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
	case "/link":
		HandleLinkCommand(telegram, msg, bindingRequest, cfg, userMapping)
	case "/quota":
		HandleQuotaCommand(telegram, msg, quotaService)
	// Unknown commands are silently ignored
	}
}

// HandleLinkCommand handles /link command with optional username and password
// Also supports direct credential input: "username password" (without /link prefix)
func HandleLinkCommand(telegram *services.TelegramClient, msg *types.TelegramMessage, bindingRequest *services.BindingRequestService, cfg *config.Config, userMapping *services.UserMappingService) {
	log.Printf("[LinkCommand] Processing /link command from user %d: %s", msg.From.ID, msg.Text)
	parts := strings.Fields(msg.Text)
	log.Printf("[LinkCommand] Parsed parts: %v (len=%d)", parts, len(parts))

	// Check if user is already linked
	if mpID, exists := userMapping.GetMoviePilotUserID(msg.From.ID); exists {
		log.Printf("[LinkCommand] User %d already linked to MoviePilot ID %d", msg.From.ID, mpID)
		text := fmt.Sprintf("✅ 账号已绑定\n\n您已经绑定了 MoviePilot 账号 (ID: %d)\n\n如需更换账号，请先联系管理员解绑", mpID)
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	// Determine if this is a /link command or direct credentials
 startIndex := 0
	if len(parts) > 0 && strings.HasPrefix(parts[0], "/link") {
		startIndex = 1 // Skip "/link" prefix
	}

	// Extract username and password
	if len(parts) <= startIndex {
		log.Printf("[LinkCommand] No credentials provided, showing help")
		text := "🔗 绑定 MoviePilot 账号\n\n请使用以下格式绑定：\n\n/link 用户名 密码\n\n或直接输入：\n用户名 密码\n\n示例：\n/link 2879681674 mypassword\n或：\n2879681674 mypassword"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	username := parts[startIndex]
	password := ""
	if len(parts) > startIndex+1 {
		password = strings.Join(parts[startIndex+1:], " ")
	}
	log.Printf("[LinkCommand] Username=%s, Password length=%d", username, len(password))

	// Validate and sanitize inputs
	sanitizedUsername, err := validation.SanitizeUsername(username)
	if err != nil {
		log.Printf("[LinkCommand] Invalid username: %v", err)
		text := "❌ 用户名格式无效"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	sanitizedPassword, err := validation.SanitizePassword(password)
	if err != nil {
		log.Printf("[LinkCommand] Invalid password: %v", err)
		text := "❌ 密码格式无效"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	log.Printf("[LinkCommand] Sanitized inputs: %s / ***", sanitizedUsername)

	// Verify credentials with MoviePilot
	mpClient := services.NewMoviePilotClient(cfg.MoviePilotURL, cfg.MoviePilotAPIKey)
	log.Printf("[LinkCommand] Calling Authenticate with MoviePilot URL: %s", cfg.MoviePilotURL)
	userID, err := mpClient.Authenticate(sanitizedUsername, sanitizedPassword)
	if err != nil {
		log.Printf("[LinkCommand] Authentication failed for %s: %v", sanitizedUsername, err)
		text := "❌ 绑定失败：用户名或密码错误"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	// Check if userID is valid (not 0)
	if userID == 0 {
		log.Printf("[LinkCommand] Authentication returned invalid userID 0 for %s", sanitizedUsername)
		text := "❌ 绑定失败：无法获取用户ID，请稍后重试"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	log.Printf("[LinkCommand] Authentication successful, userID=%d", userID)

	// Save mapping using the provided userMapping service
	log.Printf("[LinkCommand] Calling AddMapping...")
	if err := userMapping.AddMapping(msg.From.ID, userID, sanitizedUsername); err != nil {
		log.Printf("[LinkCommand] Failed to save mapping: %v", err)
		text := "❌ 绑定失败：无法保存映射"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	log.Printf("[LinkCommand] AddMapping completed, sending success message")
	text := "✅ 绑定成功\n\n您现在可以使用求片功能了"
	if _, err := telegram.SendMessage(msg.Chat.ID, text, "", nil); err != nil {
		log.Printf("[LinkCommand] Failed to send success message: %v", err)
	}
}

// SendStartMenu sends the start menu
func SendStartMenu(telegram *services.TelegramClient, chatID int64, isAdmin bool) {
	// 使用 UI 包构建主菜单（极简卡片风格）
	menuText := ui.BuildMenu("云海影视助手", "你的私人选片师")

	// 构建键盘
	keyboard := services.BuildStartKeyboardWithOptions(isAdmin, true)

	// 发送消息（纯文本，不需要特殊解析模式）
	telegram.SendMessage(chatID, menuText, "", keyboard)
}

// SendHelpMessage sends the help message
func SendHelpMessage(telegram *services.TelegramClient, chatID int64) {
	msg := services.NewMessageBuilder()
	msg.Bold("❓ 帮助中心").Newline()
	msg.Newline()
	msg.Text("🔍 搜索：输入片名即可查询").Newline()
	msg.Text("🎬 推荐：浏览热门与高分内容").Newline()
	msg.Text("📋 请求：提交后在“我的请求”查看进度").Newline()
	msg.Text("🔗 绑定：使用 /link 用户名 密码").Newline()
	msg.Newline()
	msg.Bold("⌨️ 常用命令").Newline()
	msg.Text("/start  主菜单").Newline()
	msg.Text("/search 搜索影片").Newline()
	msg.Text("/ai     精选推荐").Newline()
	msg.Text("/requests 我的请求").Newline()
	msg.Text("/help   帮助").Newline()
	msg.Newline()
	msg.Italic("💬 如有问题请联系管理员").Newline()

	telegram.SendMessage(chatID, msg.Build(), msg.ParseMode(), nil)
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
	telegram.SendMessage(msg.Chat.ID, quotaText, "", nil)
}
