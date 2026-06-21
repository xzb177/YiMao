package bot

import (
	"fmt"
	"os"
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

	// 全局限流：超过每分钟 60 条命令则忽略
	if checkCommandRateLimit(msg.From.ID) {
		logger.Warn("[Command] Rate limit exceeded for user %d, dropping command: %s", msg.From.ID, msg.Text)
		return
	}

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
		telegram.SendMessage(msg.Chat.ID, BuildStatusMessage(msg, cfg, adminService, userMapping), "HTML", nil)
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
		HandleResetPasswordCommand(telegram, msg, cfg, userMapping, adminService)
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

// Genral command rate limiter: per-user command throttle to prevent abuse.
var (
	cmdTimestamps   = make(map[int64][]time.Time)
	cmdTimestampsMu sync.Mutex
)

const (
	maxCommandsPerMinute = 60 // max commands per rolling window
	cmdWindowDuration    = 60 * time.Second
)

func checkCommandRateLimit(userID int64) bool {
	cmdTimestampsMu.Lock()
	defer cmdTimestampsMu.Unlock()
	now := time.Now()
	windowStart := now.Add(-cmdWindowDuration)
	// Clean old entries
	recent := cmdTimestamps[userID][:0]
	for _, t := range cmdTimestamps[userID] {
		if t.After(windowStart) {
			recent = append(recent, t)
		}
	}
	if len(recent) >= maxCommandsPerMinute {
		cmdTimestamps[userID] = recent
		return true
	}
	recent = append(recent, now)
	cmdTimestamps[userID] = recent
	return false
}

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

// HandleLinkCommand handles /link command with optional username and password.
// New MoviePilot users can be auto-created; existing MoviePilot users must verify password.
func HandleLinkCommand(telegram *services.TelegramClient, msg *types.TelegramMessage, bindingRequest *services.BindingRequestService, cfg *config.Config, userMapping services.UserMappingStore) {
	logger.Info("[LinkCommand] Processing /link command from user %d", msg.From.ID)

	if msg.Chat.Type == "group" || msg.Chat.Type == "supergroup" {
		telegram.SendMessage(msg.Chat.ID, "🔒 为了保护账号安全，请私聊机器人绑定 MoviePilot 账号", "", nil)
		return
	}

	if blocked, remaining := checkLinkRateLimit(msg.From.ID); blocked {
		minutes := int(remaining.Minutes()) + 1
		telegram.SendMessage(msg.Chat.ID, fmt.Sprintf("⏱️ 绑定尝试次数过多，请 %d 分钟后再试", minutes), "", nil)
		return
	}

	parts := strings.Fields(msg.Text)
	logger.Info("[LinkCommand] Parsed parts: len=%d", len(parts))

	if userMapping == nil {
		telegram.SendMessage(msg.Chat.ID, "⚠️ 服务未就绪", "", nil)
		return
	}

	if mpID, exists := userMapping.GetMoviePilotUserID(msg.From.ID); exists {
		logger.Info("[LinkCommand] User %d already linked to MoviePilot ID %d", msg.From.ID, mpID)
		telegram.SendMessage(msg.Chat.ID, "✅ 账号已绑定\n\n如需更换账号，请先联系管理员解绑", "", nil)
		return
	}

	startIndex := 0
	if len(parts) > 0 && strings.HasPrefix(parts[0], "/link") {
		startIndex = 1
	}

	if len(parts) <= startIndex {
		text := "🔗 绑定 MoviePilot 账号\n\n新用户：\n/link 用户名\n\n已有 MoviePilot 用户：\n/link 用户名 密码\n\n💡 新用户会自动创建账号；已有用户必须校验密码，防止被冒用绑定。"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	username := parts[startIndex]
	password := ""
	if len(parts) > startIndex+1 {
		password = strings.Join(parts[startIndex+1:], " ")
	}

	sanitizedUsername, err := validation.SanitizeUsername(username)
	if err != nil {
		logger.Info("[LinkCommand] Invalid username: %v", err)
		recordLinkFailure(msg.From.ID)
		telegram.SendMessage(msg.Chat.ID, "❌ 用户名格式无效", "", nil)
		return
	}

	mpClient := services.NewMoviePilotClient(cfg.MoviePilotURL, cfg.MoviePilotAPIKey, cfg.DownloadSavePath)
	var userID int64
	autoCreated := false
	createdPassword := ""

	user, err := mpClient.GetUserByUsername(sanitizedUsername)
	if err == nil && user != nil {
		if strings.TrimSpace(password) == "" {
			recordLinkFailure(msg.From.ID)
			telegram.SendMessage(msg.Chat.ID, "🔐 该 MoviePilot 用户已存在。为了防止冒用绑定，请使用：\n\n/link 用户名 密码", "", nil)
			return
		}
		if _, err := mpClient.LoginUser(sanitizedUsername, password); err != nil {
			logger.Info("[LinkCommand] Password verification failed for %s: %v", sanitizedUsername, err)
			recordLinkFailure(msg.From.ID)
			telegram.SendMessage(msg.Chat.ID, "❌ 账号或密码验证失败", "", nil)
			return
		}
		userID = user.ID
	} else {
		createdPassword = strings.TrimSpace(password)
		if createdPassword == "" {
			createdPassword, err = services.GenerateRandomPassword(16)
			if err != nil {
				telegram.SendMessage(msg.Chat.ID, "❌ 生成初始密码失败，请稍后重试", "", nil)
				return
			}
		}
		newUser, err := mpClient.RegisterUser(sanitizedUsername, createdPassword, sanitizedUsername+"@auto.local")
		if err != nil {
			logger.Info("[LinkCommand] RegisterUser failed for %s: %v", sanitizedUsername, err)
			recordLinkFailure(msg.From.ID)
			telegram.SendMessage(msg.Chat.ID, "❌ 绑定失败："+err.Error(), "", nil)
			return
		}
		userID = newUser.ID
		autoCreated = true
	}

	if userID == 0 {
		telegram.SendMessage(msg.Chat.ID, "❌ 绑定失败：无法获取用户ID，请稍后重试", "", nil)
		return
	}

	if owner, exists := userMapping.GetTelegramIDByJellyseerrID(userID); exists && owner != msg.From.ID {
		logger.Info("[LinkCommand] MoviePilot user ID %d already linked to Telegram %d", userID, owner)
		telegram.SendMessage(msg.Chat.ID, "❌ 该 MoviePilot 账号已绑定其他 Telegram 用户，请联系管理员处理", "", nil)
		return
	}
	if owner, exists := userMapping.GetTelegramIDByMoviePilotUsername(sanitizedUsername); exists && owner != msg.From.ID {
		logger.Info("[LinkCommand] MoviePilot username %s already linked to Telegram %d", sanitizedUsername, owner)
		telegram.SendMessage(msg.Chat.ID, "❌ 该 MoviePilot 账号已绑定其他 Telegram 用户，请联系管理员处理", "", nil)
		return
	}

	if err := userMapping.AddMapping(msg.From.ID, userID, sanitizedUsername); err != nil {
		logger.Info("[LinkCommand] Failed to save mapping: %v", err)
		telegram.SendMessage(msg.Chat.ID, "❌ 绑定失败："+err.Error(), "", nil)
		return
	}

	resetLinkAttempts(msg.From.ID)
	if autoCreated {
		text := fmt.Sprintf("✅ 绑定成功，已为你自动创建 MoviePilot 账号！\n\n👤 用户名：<code>%s</code>\n🔐 初始密码：<code>%s</code>\n\n⚠️ 请妥善保存，建议登录后自行修改密码。", sanitizedUsername, createdPassword)
		telegram.SendMessage(msg.Chat.ID, text, "HTML", nil)
		return
	}
	telegram.SendMessage(msg.Chat.ID, "✅ 绑定成功！现在可以求片了～", "", nil)
}

// HandleResetPasswordCommand handles /resetpw command
// Resets the MoviePilot user's password to a random one and sends it to the user.
func HandleResetPasswordCommand(telegram *services.TelegramClient, msg *types.TelegramMessage, cfg *config.Config, userMapping services.UserMappingStore, adminService *services.AdminService) {
	logger.Info("[ResetPW] Processing /resetpw from user %d", msg.From.ID)
	isAdmin := adminService != nil && adminService.IsAdmin(msg.From.ID)

	if blocked, remaining := checkLinkRateLimit(msg.From.ID); blocked {
		minutes := int(remaining.Minutes()) + 1
		telegram.SendMessage(msg.Chat.ID, fmt.Sprintf("⏱️ 操作过于频繁，请 %d 分钟后再试", minutes), "", nil)
		return
	}

	var mpUsername string
	parts := strings.Fields(msg.Text)
	if len(parts) > 1 {
		if !isAdmin {
			telegram.SendMessage(msg.Chat.ID, "🔒 普通用户只能重置自己已绑定的 MoviePilot 账号。请使用 /resetpw", "", nil)
			return
		}
		sanitized, err := validation.SanitizeUsername(parts[1])
		if err != nil {
			telegram.SendMessage(msg.Chat.ID, "❌ 用户名格式无效", "", nil)
			return
		}
		mpUsername = sanitized
	} else {
		if userMapping == nil {
			telegram.SendMessage(msg.Chat.ID, "⚠️ 服务未就绪", "", nil)
			return
		}
		mpID, exists := userMapping.GetMoviePilotUserID(msg.From.ID)
		if !exists || mpID == 0 {
			telegram.SendMessage(msg.Chat.ID, "🔗 请先绑定账号，然后使用 /resetpw 重置自己的密码", "", nil)
			return
		}
		name, err := userMapping.GetMoviePilotUsername(msg.From.ID)
		if err != nil || name == "" {
			telegram.SendMessage(msg.Chat.ID, "❌ 无法获取绑定的用户名，请联系管理员", "", nil)
			return
		}
		mpUsername = name
	}

	if cfg.MoviePilotDBPath == "" {
		telegram.SendMessage(msg.Chat.ID, "❌ 密码重置功能未配置，请联系管理员设置 MOVIEPILOT_DB_PATH", "", nil)
		return
	}

	mpClient := services.NewMoviePilotClient(cfg.MoviePilotURL, cfg.MoviePilotAPIKey, cfg.DownloadSavePath)
	newPassword, err := mpClient.ResetUserPassword(cfg.MoviePilotDBPath, mpUsername)
	if err != nil {
		logger.Info("[ResetPW] Failed to reset password for %s by tg=%d admin=%v: %v", mpUsername, msg.From.ID, isAdmin, err)
		recordLinkFailure(msg.From.ID)
		telegram.SendMessage(msg.Chat.ID, "❌ 密码重置失败："+err.Error(), "", nil)
		return
	}
	logger.Info("[ResetPW] Password reset completed for %s by tg=%d admin=%v", mpUsername, msg.From.ID, isAdmin)
	resetLinkAttempts(msg.From.ID)

	text := fmt.Sprintf("🔑 密码重置成功！\n\n"+
		"👤 用户名：<code>%s</code>\n"+
		"🔐 新密码：<code>%s</code>\n\n"+
		"请用新密码绑定：\n<code>/link %s %s</code>\n\n"+
		"⚠️ 请妥善保管，此消息不会重复发送",
		mpUsername, newPassword, mpUsername, newPassword)

	if msg.Chat.Type == "group" || msg.Chat.Type == "supergroup" {
		privMsg, err := telegram.SendMessage(msg.From.ID, text, "HTML", nil)
		if err != nil || privMsg == nil {
			telegram.SendMessage(msg.Chat.ID, "🔒 密码已重置，但私聊发送失败。请先私聊机器人发送任意消息。", "", nil)
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
	keyboard := services.BuildStartKeyboardWithOptions(isAdmin, true, hasAI, true)

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
	msg.Text("用 /link 创建或绑定 MoviePilot 账号；/resetpw 重置自己的密码").Newline()
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

// BuildStatusMessage builds a lightweight diagnostics panel for /status.
func BuildStatusMessage(msg *types.TelegramMessage, cfg *config.Config, adminService *services.AdminService, userMapping services.UserMappingStore) string {
	isAdmin := adminService != nil && msg != nil && adminService.IsAdmin(msg.From.ID)
	var sb strings.Builder
	sb.WriteString("🤖 <b>云海影视 Bot</b>\n\n")
	sb.WriteString("📊 版本: <code>v1.0</code>\n")
	sb.WriteString(fmt.Sprintf("⏰ 服务端时间: <code>%s</code>\n", time.Now().Format("2006-01-02 15:04:05")))
	if msg != nil {
		sb.WriteString(fmt.Sprintf("👤 当前用户: <code>%d</code>\n", msg.From.ID))
		sb.WriteString(fmt.Sprintf("💬 聊天类型: <code>%s</code>\n", msg.Chat.Type))
	}
	if isAdmin {
		sb.WriteString("🛡️ 身份: <b>管理员</b>\n")
	}

	if !isAdmin {
		return sb.String()
	}

	sb.WriteString("\n<b>部署诊断</b>\n")
	sb.WriteString(statusLine("MoviePilot URL", cfg != nil && cfg.MoviePilotURL != ""))
	sb.WriteString(statusLine("MoviePilot API Key", cfg != nil && cfg.MoviePilotAPIKey != ""))
	sb.WriteString(statusLine("Emby/Jellyfin", cfg != nil && cfg.EmbyURL != "" && cfg.EmbyAPIKey != ""))
	sb.WriteString(statusLine("AI 开关", os.Getenv("AI_ENABLED") == "true" || os.Getenv("ENABLE_AI") == "true"))
	sb.WriteString(statusLine("OpenAI 兼容配置", os.Getenv("OPENAI_API_KEY") != "" && os.Getenv("OPENAI_BASE_URL") != ""))
	sb.WriteString(statusLine("Rich Message", richMessageStatusEnabled()))
	sb.WriteString(statusLine("Webhook Secret", cfg != nil && cfg.WebhookSecret != ""))
	sb.WriteString(statusLine("密码重置配置", cfg != nil && cfg.MoviePilotDBPath != "" && os.Getenv("MOVIEPILOT_CONTAINER") != ""))
	sb.WriteString(statusLine("用户映射存储", userMapping != nil))
	return sb.String()
}

func statusLine(name string, ok bool) string {
	icon := "⚠️"
	value := "未配置"
	if ok {
		icon = "✅"
		value = "已配置"
	}
	return fmt.Sprintf("%s %s：<code>%s</code>\n", icon, name, value)
}

func richMessageStatusEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("ENABLE_RICH_MESSAGE")))
	return v == "" || !(v == "false" || v == "0" || v == "no" || v == "off")
}
