package bot

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xzb177/yimao/internal/callback"
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
	rankHandler *handlers.RankHandler,
	statsHandler *handlers.StatsHandler,
	dreamHandler *handlers.DreamHandler,
	adventureHandler *handlers.AdventureHandler,
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
	logger.Info("[Command] Parsed command: %s", command)

	switch command {
	case "/start":
		isAdmin := adminService != nil && adminService.IsAdmin(msg.From.ID)
		SendStartMenu(telegram, msg.Chat.ID, isAdmin)
	case "/adventure":
		if adventureHandler != nil {
			adventureHandler.HandleGoCommand(telegram, msg)
		}
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
		telegram.SendMessage(msg.Chat.ID, "🎬 AI 推荐功能已下线\n\n请使用主菜单的推荐功能", "", nil)
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
		// 片单与“求片进度”同源，统一展示已订阅和进行中的内容。
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
			logger.Info("[Command] Unlink failed for user %d: %v", msg.From.ID, err)
			_, _ = telegram.SendMessage(msg.Chat.ID, "❌ 解绑失败，请稍后再试或联系管理员", "", nil)
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
	case "/portrait":
		HandlePortraitCommand(telegram, msg, cfg, userMapping)
	case "/game":
		HandleGameCommand(telegram, msg)
	case "/narrate":
		HandleNarrateCommand(telegram, msg, cfg, sessMgr)
	case "/review":
		HandleReviewCommand(telegram, msg, cfg, userMapping)
		// New adventure commands
	case "/go":
		if adventureHandler != nil {
			adventureHandler.HandleGoCommand(telegram, msg)
		}
	case "/rank", "/排行":
		if rankHandler != nil {
			rankHandler.HandleCommand(msg.Chat.ID, msg.From.ID)
		}
	case "/mystats":
		if statsHandler != nil {
			statsHandler.HandleCommand(msg.Chat.ID, msg.From.ID)
		}
	case "/dream":
		if dreamHandler != nil {
			dreamHandler.HandleCommand(msg.Chat.ID, msg.From.ID)
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

// portraitSvc 单例复用 http.Client 连接池
var (
	portraitOnce sync.Once
	portraitSvc  *services.PortraitService
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
			_, _ = telegram.SendMessage(msg.Chat.ID, "❌ 绑定失败，请稍后再试或联系管理员", "", nil)
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
		_, _ = telegram.SendMessage(msg.Chat.ID, "❌ 绑定失败，请稍后再试或联系管理员", "", nil)
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
		_, _ = telegram.SendMessage(msg.Chat.ID, "❌ 密码重置失败，请稍后再试或联系管理员", "", nil)
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
	richMsg := richmessage.BuildWelcomeMessage("")
	keyboard := services.BuildStartKeyboardWithOptions(isAdmin, true)

	if _, err := telegram.SendRichMessage(chatID, richMsg.Markdown, keyboard); err != nil {
		logger.Info("[Command] Rich Message failed: %v, falling back to plain text", err)
		menuText := ui.BuildMenuWith(ui.StyleCard, "云海求片助手", "想看的，交给云海")
		telegram.SendMessage(chatID, menuText, "", keyboard)
	}
}

// SendHelpMessage sends the help message
// #5 /help 导览：列全部用户功能，文案口语化，并强调「想被通知出源先和我私聊」。
func SendHelpMessage(telegram *services.TelegramClient, chatID int64) {
	msg := services.NewMessageBuilder()
	msg.Bold("❓ 我能帮你做什么").Newline()
	msg.Newline()
	msg.Bold("🔍 搜索求片").Newline()
	msg.Text("直接把片名发给我，中英文都行，点结果看详情再求片").Newline()
	msg.Newline()
	msg.Bold("🎬 求片").Newline()
	msg.Text("电影详情点「立即求片」；剧集先选季度，求全部季度会再次确认").Newline()
	msg.Newline()
	msg.Bold("✨ 许愿池").Newline()
	msg.Text("搜不到的片？用 /wish 片名 许个愿，找到源第一时间通知你").Newline()
	msg.Newline()
	msg.Bold("🙋 加入想看").Newline()
	msg.Text("详情页点「加入想看」，到货时群里 @ 你").Newline()
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
	msg.Text("/start 主菜单  /search 搜索求片  /ai 今晚看什么").Newline()
	msg.Text("/wish 许愿  /requests 求片进度  /quota 配额").Newline()
	msg.Text("/portrait 观影画像  /link 绑定  /resetpw 重置密码  /help 帮助").Newline()
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
	sb.WriteString("🎬 <b>云海求片助手</b>\n\n")
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

// HandlePortraitCommand handles /portrait command — 观影画像
func HandlePortraitCommand(telegram *services.TelegramClient, msg *types.TelegramMessage, cfg *config.Config, userMapping services.UserMappingStore) {
	logger.Info("[Portrait] Processing /portrait from user %d", msg.From.ID)

	if cfg.EmbyURL == "" || cfg.EmbyAPIKey == "" {
		telegram.SendMessage(msg.Chat.ID, "❌ Emby 未配置，无法生成画像", "", nil)
		return
	}

	// 群组隐私保护：群内引导去私聊
	if msg.Chat.Type == "group" || msg.Chat.Type == "supergroup" {
		_, _ = telegram.SendMessage(msg.Chat.ID, "🔒 观影画像包含私人观影记录，请私聊机器人查看。", "", nil)
		return
	}

	// 查找 MoviePilot 用户名
	if userMapping == nil {
		telegram.SendMessage(msg.Chat.ID, "⚠️ 服务未就绪", "", nil)
		return
	}
	mpUsername, err := userMapping.GetMoviePilotUsername(msg.From.ID)
	if err != nil || mpUsername == "" {
		_, _ = telegram.SendMessage(msg.Chat.ID, "🔗 请先绑定账号（/link），再生成观影画像", "", nil)
		return
	}

	// 发送"生成中"提示
	sent, _ := telegram.SendMessage(msg.Chat.ID, "🧠 正在整理你的观影记录，请稍候...", "", nil)

	// 创建画像服务（单例复用 http.Client）
	portraitOnce.Do(func() {
		portraitSvc = services.NewPortraitService(cfg.EmbyURL, cfg.EmbyAPIKey)
	})

	// 查找 Emby 用户
	embyUserID, err := portraitSvc.FindEmbyUserByName(mpUsername)
	if err != nil {
		logger.Info("[Portrait] Emby user not found for %s: %v", mpUsername, err)
		telegram.SendMessage(msg.Chat.ID, "❌ 未找到你的 Emby 观影记录\n\n需要你的 Emby 用户名与 MoviePilot 用户名一致", "", nil)
		// 清理"生成中"提示
		if sent != nil {
			go telegram.DeleteMessage(msg.Chat.ID, sent.MessageID)
		}
		return
	}

	// 生成画像
	result, err := portraitSvc.GeneratePortrait(embyUserID, mpUsername)
	if err != nil {
		logger.Info("[Portrait] Generate failed: %v", err)
		_, _ = telegram.SendMessage(msg.Chat.ID, "❌ 画像生成失败，请稍后再试", "", nil)
		// 清理"生成中"提示
		if sent != nil {
			go telegram.DeleteMessage(msg.Chat.ID, sent.MessageID)
		}
		return
	}

	// 转换为卡片数据
	cardData := richmessage.PortraitCardData{
		UserName:   result.UserName,
		TotalItems: result.TotalItems,
		TopGenres:  strings.Join(result.TopGenres, " · "),
		AvgRating:  result.AvgRating,
		TasteLevel: result.TasteLevel,
		TasteDesc:  result.TasteDesc,
		RhythmType: result.RhythmType,
		RhythmDesc: result.RhythmDesc,
		Surprises:  result.Surprises,
		BlindSpots: result.BlindSpots,
	}
	for _, bar := range result.GenreBar {
		cardData.GenreBar = append(cardData.GenreBar, richmessage.GenreBarData{
			Genre: bar.Genre,
			Pct:   fmt.Sprintf("%.1f", bar.Pct),
			Bar:   bar.Bar,
		})
	}
	for _, pt := range result.PsychTraits {
		cardData.PsychTraits = append(cardData.PsychTraits, richmessage.PsychTraitData{
			Genre: pt.Genre,
			Trait: pt.Trait,
			Desc:  pt.Desc,
		})
	}

	// 发送画像卡片
	card := richmessage.BuildPortraitCard(cardData)
	if _, err := telegram.SendRichMessage(msg.Chat.ID, card.Markdown, nil); err != nil {
		logger.Info("[Portrait] SendRichMessage failed: %v, falling back to text", err)
		// 降级为纯文本
		ratingText := "暂无评分"
		if result.AvgRating >= 0 {
			ratingText = fmt.Sprintf("⭐ %.1f", result.AvgRating)
		}
		text := fmt.Sprintf("🧠 观影画像 — %s\n\n👤 %d 部作品\n🎭 %s\n%s\n%s — %s\n%s — %s",
			result.UserName, result.TotalItems, cardData.TopGenres, ratingText,
			result.TasteLevel, result.TasteDesc, result.RhythmType, result.RhythmDesc)
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
	}

	// 删除"生成中"提示
	if sent != nil {
		go telegram.DeleteMessage(msg.Chat.ID, sent.MessageID)
	}

	logger.Info("[Portrait] Portrait generated for %s (%d items)", mpUsername, result.TotalItems)
}

// ==================== 游戏化命令 ====================

// gameNarratorSvc 单例
var (
	gameNarratorOnce sync.Once
	gameNarratorSvc  *services.NarratorService
)

// gameSocialDB 单例
var (
	gameSocialOnce sync.Once
	gameSocialDB   *services.SocialDB
)

// HandleGameCommand 处理 /game 命令 — 游戏中心入口
func HandleGameCommand(telegram *services.TelegramClient, msg *types.TelegramMessage) {
	card := richmessage.BuildGameCenterCard(0, 0) // /game 命令无法获取 userID，默认无连胜
	telegram.SendRichMessage(msg.Chat.ID, card.Markdown, services.BuildGameCenterKeyboard())
}

// HandleNarrateCommand 处理 /narrate 命令 — AI 电影解说
func HandleNarrateCommand(telegram *services.TelegramClient, msg *types.TelegramMessage, cfg *config.Config, sessMgr *session.Manager) {
	parts := strings.Fields(msg.Text)
	if len(parts) < 2 {
		telegram.SendMessage(msg.Chat.ID, "🎬 用法: /narrate 电影名\n\n例如: /narrate 流浪地球", "", nil)
		return
	}
	movieName := strings.Join(parts[1:], " ")

	// 初始化服务
	gameNarratorOnce.Do(func() {
		gameNarratorSvc = services.NewNarratorService(cfg.EmbyURL, cfg.EmbyAPIKey, cfg.OpenAIAPIKey, "", "")
	})

	if gameNarratorSvc == nil {
		telegram.SendMessage(msg.Chat.ID, "❌ AI 服务未就绪", "", nil)
		return
	}

	// 发送"生成中"提示
	sent, _ := telegram.SendMessage(msg.Chat.ID, "🎬 正在为你解说《"+movieName+"》...", "", nil)

	// 搜索电影信息
	title, year, genres, rating, _ := gameNarratorSvc.SearchMovie(movieName)
	if title == "" {
		title = movieName
	}

	// 生成解说
	result, err := gameNarratorSvc.GenerateNarration(title, year, false)
	if err != nil {
		logger.Info("[NarrateCommand] Generation failed for user %d: %v", msg.From.ID, err)
		_, _ = telegram.SendMessage(msg.Chat.ID, "❌ 解说生成失败，请稍后再试", "", nil)
		if sent != nil {
			go telegram.DeleteMessage(msg.Chat.ID, sent.MessageID)
		}
		return
	}
	result.Rating = rating
	result.Genres = genres

	// 构建卡片
	card := richmessage.BuildNarratorCard(richmessage.NarratorCardData{
		Title:     result.Title,
		Year:      result.Year,
		Summary:   result.Summary,
		KeyPoints: result.KeyPoints,
		Mood:      result.Mood,
		Similar:   result.Similar,
		Rating:    result.Rating,
		Genres:    result.Genres,
	})

	kb := services.NewKeyboardBuilder()
	if sessMgr != nil {
		sess := sessMgr.GetOrCreate(msg.From.ID)
		ref := callback.ShortRef(movieName)
		sess.Set("narrate_movie_name", movieName)
		sess.Set("narrate_movie_"+ref, movieName)
		kb.AddButton("🔥 剧透版", "game_narrate:ref:"+ref+":spoiler:1")
	}
	kb.AddButton("🎮 游戏中心", "game_menu")

	// 删除"生成中"提示
	if sent != nil {
		go telegram.DeleteMessage(msg.Chat.ID, sent.MessageID)
	}

	telegram.SendRichMessage(msg.Chat.ID, card.Markdown, kb.Build())
}

// HandleReviewCommand 处理 /review 命令 — 写影评
func HandleReviewCommand(telegram *services.TelegramClient, msg *types.TelegramMessage, cfg *config.Config, userMapping services.UserMappingStore) {
	parts := strings.Fields(msg.Text)
	if len(parts) < 3 {
		telegram.SendMessage(msg.Chat.ID, "✍️ 用法: /review 电影名 评分(1-5) 评语\n\n例如: /review 流浪地球 5 特效炸裂", "", nil)
		return
	}

	movieName := parts[1]
	ratingStr := parts[2]
	rating, err := strconv.Atoi(ratingStr)
	if err != nil || rating < 1 || rating > 5 {
		telegram.SendMessage(msg.Chat.ID, "❌ 评分必须是 1-5 的数字", "", nil)
		return
	}

	content := ""
	if len(parts) > 3 {
		content = strings.Join(parts[3:], " ")
	}

	// 初始化社交DB
	gameSocialOnce.Do(func() {
		gameSocialDB, _ = services.NewSocialDB(cfg.DataDir)
	})

	if gameSocialDB == nil {
		telegram.SendMessage(msg.Chat.ID, "❌ 社交服务未就绪", "", nil)
		return
	}

	// 获取用户名
	mpUsername := fmt.Sprintf("用户%d", msg.From.ID)
	if userMapping != nil {
		if name, err := userMapping.GetMoviePilotUsername(msg.From.ID); err == nil && name != "" {
			mpUsername = name
		}
	}

	err = gameSocialDB.AddReview(msg.From.ID, mpUsername, movieName, 0, rating, content)
	if err != nil {
		logger.Info("[ReviewCommand] AddReview failed for user %d: %v", msg.From.ID, err)
		_, _ = telegram.SendMessage(msg.Chat.ID, "❌ 发表影评失败，请稍后再试", "", nil)
		return
	}

	stars := strings.Repeat("⭐", rating)
	text := fmt.Sprintf("✅ 影评发表成功！\n\n🎬 《%s》\n%s\n%s", movieName, stars, content)
	telegram.SendMessage(msg.Chat.ID, text, "", nil)
}
