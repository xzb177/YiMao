package bot

import (
	"fmt"
	"strings"

	"github.com/xzb177/yimao/ai"
	"github.com/xzb177/yimao/internal/config"
	"github.com/xzb177/yimao/internal/handlers"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/ui"
	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
	"github.com/xzb177/yimao/pkg/validation"
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
	wishHandler *handlers.WishHandler,
	myRequestsHandler *handlers.MyRequestsHandler,
) {
	logger.Info("[Command] Received command: %s from user %d", msg.Text, msg.From.ID)
	parts := strings.Fields(msg.Text)
	if len(parts) == 0 {
		return
	}

	command := parts[0]
	logger.Info("[Command] Parsed command: %s", command)

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
		if myRequestsHandler != nil {
			text, kb := myRequestsHandler.BuildForCommand(msg.From.ID)
			telegram.SendMessage(msg.Chat.ID, text, "HTML", ConvertKeyboard(kb))
		} else {
			telegram.SendMessage(msg.Chat.ID, "📋 服务未就绪，请稍后再试", "", nil)
		}
	case "/watchlist":
		// 片单 == 我的请求（已订阅/进行中），与 /requests 同源，不再踢皮球。
		if myRequestsHandler != nil {
			text, kb := myRequestsHandler.BuildForCommand(msg.From.ID)
			telegram.SendMessage(msg.Chat.ID, text, "HTML", ConvertKeyboard(kb))
		} else {
			telegram.SendMessage(msg.Chat.ID, "📎 服务未就绪，请稍后再试", "", nil)
		}
	case "/link":
		HandleLinkCommand(telegram, msg, bindingRequest, cfg, userMapping)
	case "/quota":
		HandleQuotaCommand(telegram, msg, quotaService)
	case "/wish":
		// #6 许愿池入口：/wish <片名> 入池；/wish 无参列出我的许愿。
		// 仅在 WishService 就绪（wishHandler != nil）时启用，否则静默忽略（不接入半成品）。
		if wishHandler != nil {
			wishHandler.HandleCommand(msg.Chat.ID, msg.From.ID, msg.Text)
		}
		// Unknown commands are silently ignored
	}
}

// HandleLinkCommand handles /link command with optional username and password
// Also supports direct credential input: "username password" (without /link prefix)
func HandleLinkCommand(telegram *services.TelegramClient, msg *types.TelegramMessage, bindingRequest *services.BindingRequestService, cfg *config.Config, userMapping *services.UserMappingService) {
	logger.Info("[LinkCommand] Processing /link command from user %d: %s", msg.From.ID, msg.Text)
	parts := strings.Fields(msg.Text)
	logger.Info("[LinkCommand] Parsed parts: %v (len=%d)", parts, len(parts))

	// Check if user is already linked
	if mpID, exists := userMapping.GetMoviePilotUserID(msg.From.ID); exists {
		logger.Info("[LinkCommand] User %d already linked to MoviePilot ID %d", msg.From.ID, mpID)
		text := "✅ 账号已绑定\n\n如需更换账号，请先联系管理员解绑"
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
		logger.Info("[LinkCommand] No credentials provided, showing help")
		text := "🔗 绑定 MoviePilot 账号\n\n请使用以下格式绑定：\n\n/link 用户名 密码\n\n或直接输入：\n用户名 密码\n\n示例：\n/link 2879681674 mypassword\n或：\n2879681674 mypassword"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	username := parts[startIndex]
	password := ""
	if len(parts) > startIndex+1 {
		password = strings.Join(parts[startIndex+1:], " ")
	}
	// 不记录密码和敏感信息，只记录操作类型
	logger.Info("[LinkCommand] Processing link request for user")

	// Validate and sanitize inputs
	sanitizedUsername, err := validation.SanitizeUsername(username)
	if err != nil {
		logger.Info("[LinkCommand] Invalid username: %v", err)
		text := "❌ 用户名格式无效"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	sanitizedPassword, err := validation.SanitizePassword(password)
	if err != nil {
		logger.Info("[LinkCommand] Invalid password: %v", err)
		text := "❌ 密码格式无效"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	logger.Info("[LinkCommand] Sanitized inputs: %s / ***", sanitizedUsername)

	// Verify credentials with MoviePilot
	mpClient := services.NewMoviePilotClient(cfg.MoviePilotURL, cfg.MoviePilotAPIKey, cfg.DownloadSavePath)
	logger.Info("[LinkCommand] Calling Authenticate with MoviePilot URL: %s", cfg.MoviePilotURL)
	userID, err := mpClient.Authenticate(sanitizedUsername, sanitizedPassword)
	if err != nil {
		logger.Info("[LinkCommand] Authentication failed for %s: %v", sanitizedUsername, err)
		text := "❌ 绑定失败：用户名或密码错误"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	// Check if userID is valid (not 0)
	if userID == 0 {
		logger.Info("[LinkCommand] Authentication returned invalid userID 0 for %s", sanitizedUsername)
		text := "❌ 绑定失败：无法获取用户ID，请稍后重试"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	logger.Info("[LinkCommand] Authentication successful, userID=%d", userID)

	// Save mapping using the provided userMapping service
	logger.Info("[LinkCommand] Calling AddMapping...")
	if err := userMapping.AddMapping(msg.From.ID, userID, sanitizedUsername); err != nil {
		logger.Info("[LinkCommand] Failed to save mapping: %v", err)
		text := "❌ 绑定失败：无法保存映射"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	logger.Info("[LinkCommand] AddMapping completed, sending success message")
	text := "✅ 绑定成功！现在可以求片了～"
	if _, err := telegram.SendMessage(msg.Chat.ID, text, "", nil); err != nil {
		logger.Info("[LinkCommand] Failed to send success message: %v", err)
	}
}

// SendStartMenu sends the start menu
func SendStartMenu(telegram *services.TelegramClient, chatID int64, isAdmin bool) {
	menuText := ui.BuildMenu("云海影视", "你的私人选片师")
	// AI 按钮只在 AI 功能开启时显示
	keyboard := services.BuildStartKeyboardWithOptions(isAdmin, ai.GetManager().IsEnabled(), true)
	telegram.SendMessage(chatID, menuText, "", keyboard)
}

// SendHelpMessage sends the help message
// #5 /help 导览：列全部用户功能，文案口语化，并强调「想被通知出源先和我私聊」。
func SendHelpMessage(telegram *services.TelegramClient, chatID int64) {
	msg := services.NewMessageBuilder()
	msg.Bold("❓ 我能帮你做这些").Newline()
	msg.Newline()
	msg.Bold("🔍 搜片").Newline()
	msg.Text("直接把片名发给我（中英文、电影剧集都行），点结果看详情、求片或订阅。").Newline()
	msg.Newline()
	msg.Bold("🎬 求片").Newline()
	msg.Text("详情页点「求片」，提交后能在「我的请求」里看进度。").Newline()
	msg.Newline()
	msg.Bold("🚦 预产期灯牌").Newline()
	msg.Text("看候选资源时会有个状态灯：").Newline()
	msg.Text("⚡ 资源充足，很快到货 → 等着就好").Newline()
	msg.Text("🔄 已有源，需要等做种 → 可去站点顶一下种").Newline()
	msg.Text("🐢 暂无站点出源 → 可去求助群问问谁有").Newline()
	msg.Text("❓ 数据不足，系统还在找来源 → 稍等").Newline()
	msg.Newline()
	msg.Bold("📈 剧集进度条").Newline()
	msg.Text("剧集入库通知会带本季/全剧的更新进度，一眼看出追到哪了。").Newline()
	msg.Newline()
	msg.Bold("🙋 拼车 +1").Newline()
	msg.Text("详情页点「我也想看 +1」，到货时群里会 @ 你一起追。").Newline()
	msg.Newline()
	msg.Bold("🌟 许愿池").Newline()
	msg.Text("没源的片用 /wish 片名 加入，找到源第一时间通知你；/wish 不带参数看我的许愿。").Newline()
	msg.Newline()
	msg.Bold("⌨️ 常用命令").Newline()
	msg.Text("/start 主菜单  /search 搜片  /ai 推荐").Newline()
	msg.Text("/wish 许愿求片  /requests 我的请求").Newline()
	msg.Text("/link 绑定账号  /quota 查看配额  /help 帮助").Newline()
	msg.Newline()
	msg.Italic("💬 想被通知出源？记得先和我私聊过一句哦，不然我发不出私信～").Newline()

	telegram.SendMessage(chatID, msg.Build(), msg.ParseMode(), nil)
}

// HandleQuotaCommand handles /quota command
func HandleQuotaCommand(telegram *services.TelegramClient, msg *types.TelegramMessage, quotaService *services.QuotaService) {
	logger.Info("[QuotaCommand] Handling /quota for user %d, quotaService=%v", msg.From.ID, quotaService != nil)

	if quotaService == nil {
		logger.Info("[QuotaCommand] QuotaService is nil!")
		telegram.SendMessage(msg.Chat.ID, "❌ 配额服务未启用", "", nil)
		return
	}

	userID := msg.From.ID
	quotaText := quotaService.GetQuotaText(userID)
	logger.Info("[QuotaCommand] Sending quota text to user %d", userID)
	telegram.SendMessage(msg.Chat.ID, quotaText, "", nil)
}
