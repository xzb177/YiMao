package bot

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xzb177/yimao/ai"
	"github.com/xzb177/yimao/internal/config"
	"github.com/xzb177/yimao/internal/handlers"
	"github.com/xzb177/yimao/internal/richmessage"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
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
	userMapping services.UserMappingStore,
	sessMgr *session.Manager,
	wishHandler *handlers.WishHandler,
	myRequestsHandler *handlers.MyRequestsHandler,
) {
	logger.Info("[Command] Received command: %s from user %d", msg.Text, msg.From.ID)
	parts := strings.Fields(msg.Text)
	if len(parts) == 0 {
		return
	}

	rawCommand := parts[0]
	command := strings.SplitN(rawCommand, "@", 2)[0]
	inlineArgs := ""
	if strings.HasPrefix(command, "/ai") && command != "/ai" {
		inlineArgs = strings.TrimPrefix(command, "/ai")
		command = "/ai"
	}
	logger.Info("[Command] Parsed command: %s", command)

	switch command {
	case "/start":
		isAdmin := adminService != nil && adminService.IsAdmin(msg.From.ID)
		SendStartMenu(telegram, msg.Chat.ID, isAdmin)
	case "/status":
		isAdminStatus := adminService != nil && adminService.IsAdmin(msg.From.ID)
		var sb strings.Builder
		sb.WriteString("🤖 <b>云海影视 Bot</b>\n\n")
		sb.WriteString(fmt.Sprintf("📊 版本: <code>%s</code>\n", "v1.0"))
		sb.WriteString(fmt.Sprintf("⏰ 服务端时间: <code>%s</code>\n", time.Now().Format("2006-01-02 15:04:05")))
		sb.WriteString(fmt.Sprintf("👤 当前用户: <code>%d</code>\n", msg.From.ID))
		sb.WriteString(fmt.Sprintf("💬 聊天类型: <code>%s</code>\n", msg.Chat.Type))
		if isAdminStatus {
			sb.WriteString("\n🛡️ 身份: <b>管理员</b>")
		}
		telegram.SendMessage(msg.Chat.ID, sb.String(), "HTML", nil)
	case "/help":
		SendHelpMessage(telegram, msg.Chat.ID)
	case "/id":
		text := fmt.Sprintf("📋 当前聊天信息\n\n聊天 ID: <code>%d</code>\n聊天类型: %s\n用户 ID: <code>%d</code>", msg.Chat.ID, msg.Chat.Type, msg.From.ID)
		telegram.SendMessage(msg.Chat.ID, text, "HTML", nil)
	case "/search":
		text := "🔍 请输入影片名称进行搜索"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
	case "/ai":
		args := strings.TrimSpace(strings.TrimPrefix(msg.Text, rawCommand))
		if inlineArgs != "" {
			args = strings.TrimSpace(inlineArgs + " " + args)
		}
		if args == "" {
			telegram.SendMessage(msg.Chat.ID, "🎬 今晚看什么\n\n直接把你的口味/心情告诉我，比如：\n• 想看一部烧脑悬疑\n• 推荐治愈系的\n• 适合今晚轻松看的电影", "", nil)
			return
		}
		handleAIChatMessage(msg.From.ID, msg.Chat.ID, args, telegram, sessMgr)
	case "/requests":
		// 群组隐私保护：群内不发长卡片，引导去私聊
		if msg.Chat.Type == "group" || msg.Chat.Type == "supergroup" {
			telegram.SendMessage(msg.Chat.ID, "🔒 为了保护您的观影隐私，请私聊查看完整进度", "", nil)
			return
		}
		if myRequestsHandler != nil {
			text, kb := myRequestsHandler.BuildForCommand(msg.From.ID)
			telegram.SendMessage(msg.Chat.ID, text, "HTML", ConvertKeyboard(kb))
		} else {
			telegram.SendMessage(msg.Chat.ID, "📋 服务未就绪，请稍后再试", "", nil)
		}
	case "/watchlist":
		// 群组隐私保护：群内不暴露片单/进度
		if msg.Chat.Type == "group" || msg.Chat.Type == "supergroup" {
			telegram.SendMessage(msg.Chat.ID, "🔒 为了保护您的观影隐私，请私聊查看完整片单", "", nil)
			return
		}
		// 片单 == 我的请求（已订阅/进行中），与 /requests 同源，不再踢皮球。
		if myRequestsHandler != nil {
			text, kb := myRequestsHandler.BuildForCommand(msg.From.ID)
			telegram.SendMessage(msg.Chat.ID, text, "HTML", ConvertKeyboard(kb))
		} else {
			telegram.SendMessage(msg.Chat.ID, "📎 服务未就绪，请稍后再试", "", nil)
		}
	case "/link":
		HandleLinkCommand(telegram, msg, bindingRequest, cfg, userMapping)
	case "/resetpw":
		HandleResetPasswordCommand(telegram, msg, cfg, userMapping)
	case "/unlink":
		if userMapping == nil {
			telegram.SendMessage(msg.Chat.ID, "⚠️ 服务未就绪", "", nil)
			return
		}
		if err := userMapping.RemoveMapping(msg.From.ID); err != nil {
			telegram.SendMessage(msg.Chat.ID, "❌ 解绑失败："+err.Error(), "", nil)
		} else {
			telegram.SendMessage(msg.Chat.ID, "✅ 已解绑 MoviePilot 账号", "", nil)
		}
	case "/quota":
		// 群组隐私保护：群内不暴露配额
		if msg.Chat.Type == "group" || msg.Chat.Type == "supergroup" {
			telegram.SendMessage(msg.Chat.ID, "🔒 为了保护您的隐私，请私聊查看配额详情", "", nil)
			return
		}
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

// linkAttempts 记录 /link 命令的失败尝试次数（防暴力破解）
var (
	linkAttempts   = make(map[int64]int)
	linkBlocked    = make(map[int64]time.Time)
	linkAttemptsMu sync.Mutex
)

const (
	maxLinkAttempts = 5                // 最多 5 次失败
	linkBlockTime   = 15 * time.Minute // 锁定 15 分钟
)

func checkLinkRateLimit(userID int64) (blocked bool, remaining time.Duration) {
	linkAttemptsMu.Lock()
	defer linkAttemptsMu.Unlock()

	if until, ok := linkBlocked[userID]; ok {
		if time.Now().Before(until) {
			return true, time.Until(until)
		}
		delete(linkBlocked, userID)
		delete(linkAttempts, userID)
	}
	return false, 0
}

func recordLinkFailure(userID int64) {
	linkAttemptsMu.Lock()
	defer linkAttemptsMu.Unlock()

	linkAttempts[userID]++
	if linkAttempts[userID] >= maxLinkAttempts {
		linkBlocked[userID] = time.Now().Add(linkBlockTime)
		delete(linkAttempts, userID)
	}
}

func resetLinkAttempts(userID int64) {
	linkAttemptsMu.Lock()
	defer linkAttemptsMu.Unlock()
	delete(linkAttempts, userID)
	delete(linkBlocked, userID)
}

// HandleLinkCommand handles /link command with optional username and password
// Also supports direct credential input: "username password" (without /link prefix)
func HandleLinkCommand(telegram *services.TelegramClient, msg *types.TelegramMessage, bindingRequest *services.BindingRequestService, cfg *config.Config, userMapping services.UserMappingStore) {
	logger.Info("[LinkCommand] Processing /link command from user %d", msg.From.ID)

	// 防暴力破解：检查是否被锁定
	if blocked, remaining := checkLinkRateLimit(msg.From.ID); blocked {
		minutes := int(remaining.Minutes()) + 1
		telegram.SendMessage(msg.Chat.ID, fmt.Sprintf("⏱️ 绑定尝试次数过多，请 %d 分钟后再试", minutes), "", nil)
		return
	}

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
		text := "🔗 绑定 MoviePilot 账号\n\n/link 用户名\n\n示例：/link cabbeenpoom\n\n💡 首次使用会自动创建账号，不需要密码"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	username := parts[startIndex]
	password := ""
	if len(parts) > startIndex+1 {
		password = strings.Join(parts[startIndex+1:], " ")
	}
	_ = password // password no longer required for binding
	logger.Info("[LinkCommand] Processing link request for user")

	// Validate and sanitize inputs
	sanitizedUsername, err := validation.SanitizeUsername(username)
	if err != nil {
		logger.Info("[LinkCommand] Invalid username: %v", err)
		text := "❌ 用户名格式无效"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	logger.Info("[LinkCommand] Sanitized inputs: %s", sanitizedUsername)

	// Find user in MoviePilot (no password verification)
	mpClient := services.NewMoviePilotClient(cfg.MoviePilotURL, cfg.MoviePilotAPIKey, cfg.DownloadSavePath)
	userID, err := mpClient.EnsureUser(sanitizedUsername)
	if err != nil {
		logger.Info("[LinkCommand] EnsureUser failed for %s: %v", sanitizedUsername, err)
		text := "❌ 绑定失败：" + err.Error()
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
	resetLinkAttempts(msg.From.ID)
	text := "✅ 绑定成功！现在可以求片了～"
	if _, err := telegram.SendMessage(msg.Chat.ID, text, "", nil); err != nil {
		logger.Info("[LinkCommand] Failed to send success message: %v", err)
	}
}

// HandleResetPasswordCommand handles /resetpw command
// Resets the MoviePilot user's password to a random one and sends it to the user.
func HandleResetPasswordCommand(telegram *services.TelegramClient, msg *types.TelegramMessage, cfg *config.Config, userMapping services.UserMappingStore) {
	logger.Info("[ResetPW] Processing /resetpw from user %d", msg.From.ID)

	// Rate limit check (reuse link rate limiter)
	if blocked, remaining := checkLinkRateLimit(msg.From.ID); blocked {
		minutes := int(remaining.Minutes()) + 1
		telegram.SendMessage(msg.Chat.ID, fmt.Sprintf("⏱️ 操作过于频繁，请 %d 分钟后再试", minutes), "", nil)
		return
	}

	// Determine the MoviePilot username
	var mpUsername string

	parts := strings.Fields(msg.Text)
	if len(parts) > 1 {
		// User provided a username: /resetpw username
		sanitized, err := validation.SanitizeUsername(parts[1])
		if err != nil {
			telegram.SendMessage(msg.Chat.ID, "❌ 用户名格式无效", "", nil)
			return
		}
		mpUsername = sanitized
	} else {
		// No username provided — try to get from linked account
		if userMapping == nil {
			telegram.SendMessage(msg.Chat.ID, "⚠️ 服务未就绪", "", nil)
			return
		}
		mpID, exists := userMapping.GetMoviePilotUserID(msg.From.ID)
		if !exists || mpID == 0 {
			text := "🔗 请先绑定账号，或指定用户名：\n\n/resetpw 用户名\n\n示例：/resetpw cabbeenpoom"
			telegram.SendMessage(msg.Chat.ID, text, "", nil)
			return
		}
		// Get username from mapping
		name, err := userMapping.GetMoviePilotUsername(msg.From.ID)
		if err != nil || name == "" {
			telegram.SendMessage(msg.Chat.ID, "❌ 无法获取绑定的用户名，请用：/resetpw 用户名", "", nil)
			return
		}
		mpUsername = name
	}

	// Check if DB path is configured
	if cfg.MoviePilotDBPath == "" {
		telegram.SendMessage(msg.Chat.ID, "❌ 密码重置功能未配置，请联系管理员", "", nil)
		return
	}

	// Reset password
	mpClient := services.NewMoviePilotClient(cfg.MoviePilotURL, cfg.MoviePilotAPIKey, cfg.DownloadSavePath)
	newPassword, err := mpClient.ResetUserPassword(cfg.MoviePilotDBPath, mpUsername)
	if err != nil {
		logger.Info("[ResetPW] Failed to reset password for %s: %v", mpUsername, err)
		recordLinkFailure(msg.From.ID)
		telegram.SendMessage(msg.Chat.ID, "❌ 密码重置失败："+err.Error(), "", nil)
		return
	}

	resetLinkAttempts(msg.From.ID)

	// Send new password to user (private message for security)
	text := fmt.Sprintf("🔑 密码重置成功！\n\n"+
		"👤 用户名：<code>%s</code>\n"+
		"🔐 新密码：<code>%s</code>\n\n"+
		"请用新密码绑定：\n<code>/link %s %s</code>\n\n"+
		"⚠️ 请妥善保管，此消息不会重复发送",
		mpUsername, newPassword, mpUsername, newPassword)

	// Try private chat first, fall back to current chat
	if msg.Chat.Type == "group" || msg.Chat.Type == "supergroup" {
		// Security: send password in private chat, not group
		privMsg, err := telegram.SendMessage(msg.From.ID, text, "HTML", nil)
		if err != nil || privMsg == nil {
			telegram.SendMessage(msg.Chat.ID, "🔒 请先私聊发送任意消息，然后再次执行 /resetpw", "", nil)
			return
		}
		telegram.SendMessage(msg.Chat.ID, "✅ 密码已重置，请查看私聊消息", "", nil)
	} else {
		telegram.SendMessage(msg.Chat.ID, text, "HTML", nil)
	}
}

// SendStartMenu sends the start menu
func SendStartMenu(telegram *services.TelegramClient, chatID int64, isAdmin bool) {
	// 使用 Rich Message 发送欢迎页（Bot API 10.1）
	hasAI := ai.GetManager().IsEnabled()
	richMsg := richmessage.BuildWelcomeMessage("", hasAI)
	keyboard := services.BuildStartKeyboardWithOptions(isAdmin, hasAI, true)

	if _, err := telegram.SendRichMessage(chatID, richMsg.Markdown, keyboard); err != nil {
		logger.Info("[Command] Rich Message failed: %v, falling back to plain text", err)
		menuText := ui.BuildMenuWith(ui.StyleCard, "云海影视", "你的私人选片师")
		telegram.SendMessage(chatID, menuText, "", keyboard)
	}
}

// SendHelpMessage sends the help message
// #5 /help 导览：列全部用户功能，文案口语化，并强调「想被通知出源先和我私聊」。
func SendHelpMessage(telegram *services.TelegramClient, chatID int64) {
	msg := services.NewMessageBuilder()
	msg.Bold("❓ 我能帮你做什么").Newline()
	msg.Newline()
	msg.Bold("🔍 搜片").Newline()
	msg.Text("直接把片名发给我，中英文都行，点结果看详情再求片").Newline()
	msg.Newline()
	msg.Bold("🎬 求片").Newline()
	msg.Text("详情页点「发起求片」，审核通过后自动下载，完成后通知你").Newline()
	msg.Newline()
	msg.Bold("✨ 许愿池").Newline()
	msg.Text("搜不到的片？用 /wish 片名 许个愿，找到源第一时间通知你").Newline()
	msg.Newline()
	msg.Bold("🙋 拼车 +1").Newline()
	msg.Text("详情页点「我也想看」，到货时群里 @ 你").Newline()
	msg.Newline()
	msg.Bold("🔔 通知设置").Newline()
	msg.Text("设置 → 通知设置，可单独开关入库通知/每日推荐/周报/公告").Newline()
	msg.Newline()
	msg.Bold("📊 配额").Newline()
	msg.Text("电影扣 1 配额，剧集扣 3（保护服务器资源），次日 0 点刷新").Newline()
	msg.Newline()
	msg.Bold("🔑 账号").Newline()
	msg.Text("首次使用自动绑定，无需注册。/link 换绑，/resetpw 重置密码").Newline()
	msg.Newline()
	msg.Bold("⌨️ 命令速查").Newline()
	msg.Text("/start 主菜单  /search 搜片  /ai 今晚看什么").Newline()
	msg.Text("/wish 许愿  /requests 求片进度  /quota 配额").Newline()
	msg.Text("/link 绑定  /resetpw 重置密码  /help 帮助").Newline()
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
