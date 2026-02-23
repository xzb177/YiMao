package handlers

import (
	"fmt"
	"log"
	"strconv"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/config"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
)

// AdminHandler handles admin-related callbacks
type AdminHandler struct {
	cfg                  *config.Config
	sessMgr              *session.Manager
	telegram             *services.TelegramClient
	moviepilot           *services.MoviePilotClient
	adminService         *services.AdminService
	quotaService         *services.QuotaService
	mediaNotificationSvc *services.MediaNotificationService
}

// NewAdminHandler creates a new admin handler
func NewAdminHandler(
	cfg *config.Config,
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	moviepilot *services.MoviePilotClient,
	adminService *services.AdminService,
	quotaService *services.QuotaService,
) *AdminHandler {
	return &AdminHandler{
		cfg:          cfg,
		sessMgr:      sessMgr,
		telegram:     telegram,
		moviepilot:   moviepilot,
		adminService: adminService,
		quotaService: quotaService,
	}
}

// SetMediaNotificationService sets the media notification service
func (h *AdminHandler) SetMediaNotificationService(svc *services.MediaNotificationService) {
	h.mediaNotificationSvc = svc
}

// Handle handles admin callbacks
func (h *AdminHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	switch ctx.Callback.Action {
	case "admin_approve":
		return h.handleApprove(ctx)
	case "admin_decline":
		return h.handleDecline(ctx)
	case "admin_pending":
		return h.handlePending(ctx)
	case "admin_issue_reply":
		return h.handleIssueReply(ctx)
	case "admin_issue_fixed":
		return h.handleIssueFixed(ctx)
	case "admin_issue_processing":
		return h.handleIssueProcessing(ctx)
	case "admin_issue_close":
		return h.handleIssueClose(ctx)
	case "admin_menu":
		return h.handleAdminMenu(ctx)
	case "admin_notif_settings":
		return h.handleNotifSettings(ctx)
	case "admin_notif_mode_instant":
		return h.handleNotifModeInstant(ctx)
	case "admin_notif_mode_daily":
		return h.handleNotifModeDaily(ctx)
	case "admin_notif_toggle":
		return h.handleNotifToggle(ctx)
	case "admin_notif_settime":
		return h.handleNotifSetTime(ctx)
	case "admin_notif_format_simple":
		return h.handleNotifFormatSimple(ctx)
	case "admin_notif_format_detailed":
		return h.handleNotifFormatDetailed(ctx)
	default:
		return nil, nil
	}
}

// handleApprove handles approve request callback
func (h *AdminHandler) handleApprove(ctx *callback.Context) (*callback.Response, error) {
	// Check if user is admin
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	// Parse request ID from params
	requestIDStr := ctx.Callback.Params["id"]
	if requestIDStr == "" {
		return &callback.Response{
			CallbackMsg: "无效的请求ID",
			ShowAlert:   true,
		}, nil
	}

	requestID, err := strconv.Atoi(requestIDStr)
	if err != nil {
		return &callback.Response{
			CallbackMsg: "无效的请求ID",
			ShowAlert:   true,
		}, nil
	}

	// Approve request
	if h.moviepilot != nil {
		// MoviePilot API for approving requests will be implemented
		log.Printf("[AdminHandler] Approve request %d (MoviePilot)", requestID)
	}

	message := fmt.Sprintf("✅ 请求已批准\n\n请求ID: %d", requestID)

	return &callback.Response{
		Text:   message,
		CallbackMsg: "已批准",
		Edit:   true,
	}, nil
}

// handleDecline handles decline request callback
func (h *AdminHandler) handleDecline(ctx *callback.Context) (*callback.Response, error) {
	// Check if user is admin
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	// Parse request ID from params
	requestIDStr := ctx.Callback.Params["id"]
	if requestIDStr == "" {
		return &callback.Response{
			CallbackMsg: "无效的请求ID",
			ShowAlert:   true,
		}, nil
	}

	requestID, err := strconv.Atoi(requestIDStr)
	if err != nil {
		return &callback.Response{
			CallbackMsg: "无效的请求ID",
			ShowAlert:   true,
		}, nil
	}

	// Decline request
	if h.moviepilot != nil {
		// MoviePilot API for declining requests will be implemented
		log.Printf("[AdminHandler] Decline request %d (MoviePilot)", requestID)
	}

	message := fmt.Sprintf("❌ 请求已拒绝\n\n请求ID: %d", requestID)

	return &callback.Response{
		Text:   message,
		CallbackMsg: "已拒绝",
		Edit:   true,
	}, nil
}

// handlePending handles show pending requests callback
func (h *AdminHandler) handlePending(ctx *callback.Context) (*callback.Response, error) {
	// Check if user is admin
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	if h.moviepilot == nil {
		return &callback.Response{
			Text:   "❌ MoviePilot API 未配置",
			Edit:   true,
		}, nil
	}

	// Get pending requests - placeholder for MoviePilot API
	message := "📋 待处理请求\n\n💡 MoviePilot API 集成中，请直接访问 MoviePilot 网页管理请求"

	return &callback.Response{
		Text:   message,
		Edit:   true,
	}, nil
}

// formatPendingRequests formats pending requests for display
func (h *AdminHandler) formatPendingRequests(requests []services.Request) string {
	if len(requests) == 0 {
		return "📋 待处理请求\n\n✅ 暂无待处理请求"
	}

	message := "📋 待处理请求\n\n"
	for i, req := range requests {
		if i >= 10 { // Limit to 10 requests
			break
		}

		mediaType := "电影"
		if req.MediaType == services.MediaTypeTV {
			mediaType = "剧集"
		}

		title := fmt.Sprintf("媒体 #%d", req.MediaID)
		if req.Media != nil && req.Media.Title != "" {
			title = req.Media.Title
		}

		message += fmt.Sprintf("%d. %s (%s) - ID:%d\n", i+1, title, mediaType, req.ID)
	}

	if len(requests) > 10 {
		message += fmt.Sprintf("\n... 还有 %d 个请求", len(requests)-10)
	}

	return message
}

// handleIssueReply handles issue reply callback
func (h *AdminHandler) handleIssueReply(ctx *callback.Context) (*callback.Response, error) {
	// Check if user is admin
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	// Parse issue ID
	issueIDStr := ctx.Callback.Params["id"]
	if issueIDStr == "" {
		return &callback.Response{
			CallbackMsg: "无效的问题ID",
			ShowAlert:   true,
		}, nil
	}

	issueID, err := strconv.ParseInt(issueIDStr, 10, 64)
	if err != nil {
		return &callback.Response{
			CallbackMsg: "无效的问题ID",
			ShowAlert:   true,
		}, nil
	}

	// Store pending reply in session for next message
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	sess.Set("pending_issue_reply", issueID)

	message := fmt.Sprintf("💬 请输入回复内容\n\n问题ID: %d\n\n发送下一条消息将作为回复内容", issueID)

	return &callback.Response{
		Text:   message,
		Edit:   true,
	}, nil
}

// handleIssueFixed handles issue fixed callback
func (h *AdminHandler) handleIssueFixed(ctx *callback.Context) (*callback.Response, error) {
	// Check if user is admin
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	// Parse issue ID
	issueIDStr := ctx.Callback.Params["id"]
	if issueIDStr == "" {
		return &callback.Response{
			CallbackMsg: "无效的问题ID",
			ShowAlert:   true,
		}, nil
	}

	issueID, err := strconv.ParseInt(issueIDStr, 10, 64)
	if err != nil {
		return &callback.Response{
			CallbackMsg: "无效的问题ID",
			ShowAlert:   true,
		}, nil
	}

	// Send "fixed" comment via webhook service
	// This would be handled by the WebhookService

	message := fmt.Sprintf("✅ 问题已标记为修复\n\n问题ID: %d", issueID)

	return &callback.Response{
		Text:   message,
		Edit:   true,
	}, nil
}

// handleIssueProcessing handles issue processing callback
func (h *AdminHandler) handleIssueProcessing(ctx *callback.Context) (*callback.Response, error) {
	// Check if user is admin
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	// Parse issue ID
	issueIDStr := ctx.Callback.Params["id"]
	if issueIDStr == "" {
		return &callback.Response{
			CallbackMsg: "无效的问题ID",
			ShowAlert:   true,
		}, nil
	}

	issueID, err := strconv.ParseInt(issueIDStr, 10, 64)
	if err != nil {
		return &callback.Response{
			CallbackMsg: "无效的问题ID",
			ShowAlert:   true,
		}, nil
	}

	message := fmt.Sprintf("ℹ️ 问题已标记为处理中\n\n问题ID: %d", issueID)

	return &callback.Response{
		Text:   message,
		Edit:   true,
	}, nil
}

// handleIssueClose handles issue close callback
func (h *AdminHandler) handleIssueClose(ctx *callback.Context) (*callback.Response, error) {
	// Check if user is admin
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	// Parse issue ID
	issueIDStr := ctx.Callback.Params["id"]
	if issueIDStr == "" {
		return &callback.Response{
			CallbackMsg: "无效的问题ID",
			ShowAlert:   true,
		}, nil
	}

	issueID, err := strconv.ParseInt(issueIDStr, 10, 64)
	if err != nil {
		return &callback.Response{
			CallbackMsg: "无效的问题ID",
			ShowAlert:   true,
		}, nil
	}

	message := fmt.Sprintf("❌ 问题已关闭\n\n问题ID: %d", issueID)

	return &callback.Response{
		Text:   message,
		Edit:   true,
	}, nil
}

// handleAdminMenu handles admin menu callback
func (h *AdminHandler) handleAdminMenu(ctx *callback.Context) (*callback.Response, error) {
	// Check if user is admin
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	msg := services.NewMessageBuilder()
	msg.Bold("🔧 管理员菜单").Newline()
	msg.Newline()
	msg.Text("请选择操作：").Newline()

	kb := services.NewKeyboardBuilder()

	// Media notification settings
	if h.mediaNotificationSvc != nil {
		settings := h.mediaNotificationSvc.GetSettings(ctx.UserID)
		modeIcon := "⚡"
		if settings.Mode == services.ModeDaily {
			modeIcon = "📅"
		}
		statusIcon := "✅"
		if !settings.Enabled {
			statusIcon = "❌"
		}

		msg.Bold(fmt.Sprintf("%s 媒体库通知", statusIcon)).Newline()
		msg.Textf("   模式: %s %s", modeIcon, h.getModeText(settings.Mode)).Newline()
		if settings.Mode == services.ModeDaily {
			msg.Textf("   汇总时间: %s", settings.DailyTime).Newline()
		}
		msg.Newline()

		kb.AddButton("🔔 通知设置", "admin_notif_settings")
		kb.NewRow()
	}

	// Return button
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleNotifSettings handles notification settings callback
func (h *AdminHandler) handleNotifSettings(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	if h.mediaNotificationSvc == nil {
		return &callback.Response{
			Text:   "❌ 通知服务未启用",
			Edit:   true,
		}, nil
	}

	settings := h.mediaNotificationSvc.GetSettings(ctx.UserID)
	pendingCount := h.mediaNotificationSvc.GetPendingItemsCount(ctx.UserID)

	msg := services.NewMessageBuilder()
	msg.Bold("🔔 媒体库通知设置").Newline()
	msg.Newline()

	// Current mode
	modeIcon := "⚡"
	if settings.Mode == services.ModeDaily {
		modeIcon = "📅"
	}
	msg.Text(fmt.Sprintf("📱 当前模式: %s %s", modeIcon, h.getModeText(settings.Mode))).Newline()

	// Current format
	formatIcon := "📝"
	formatText := "简洁"
	if settings.Format == services.FormatDetailed {
		formatIcon = "📋"
		formatText = "详细"
	}
	msg.Text(fmt.Sprintf("%s 通知格式: %s", formatIcon, formatText)).Newline()

	// Status
	statusIcon := "✅"
	statusText := "已启用"
	if !settings.Enabled {
		statusIcon = "❌"
		statusText = "已禁用"
	}
	msg.Text(fmt.Sprintf("%s 状态: %s", statusIcon, statusText)).Newline()

	// Daily time (if applicable)
	if settings.Mode == services.ModeDaily {
		msg.Text(fmt.Sprintf("⏰ 汇总时间: %s", settings.DailyTime)).Newline()
	}

	// Pending items
	if settings.Mode == services.ModeDaily && pendingCount > 0 {
		msg.Text(fmt.Sprintf("📦 今日待汇总: %d 项", pendingCount)).Newline()
	}

	msg.Newline()

	kb := services.NewKeyboardBuilder()

	// Mode selection
	kb.AddButton("⚡ 单集推送", "admin_notif_mode_instant")
	kb.AddButton("📅 每日汇总", "admin_notif_mode_daily")
	kb.NewRow()

	// Format selection
	formatSimpleIcon := "⚪"
	formatDetailedIcon := "⚪"
	if settings.Format == services.FormatSimple {
		formatSimpleIcon = "🔵"
	} else {
		formatDetailedIcon = "🔵"
	}
	kb.AddButton(fmt.Sprintf("%s 简洁格式", formatSimpleIcon), "admin_notif_format_simple")
	kb.AddButton(fmt.Sprintf("%s 详细格式", formatDetailedIcon), "admin_notif_format_detailed")
	kb.NewRow()

	// Time selection (for daily mode)
	if settings.Mode == services.ModeDaily {
		kb.AddButton("⏰ 设置时间", "admin_notif_settime")
		kb.NewRow()
	}

	// Toggle enable/disable
	toggleText := "❌ 禁用通知"
	if !settings.Enabled {
		toggleText = "✅ 启用通知"
	}
	kb.AddButton(toggleText, "admin_notif_toggle")
	kb.NewRow()

	// Return buttons
	kb.AddButton("⬅️ 返回管理员菜单", "admin_menu")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleNotifModeInstant handles switching to instant notification mode
func (h *AdminHandler) handleNotifModeInstant(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	if h.mediaNotificationSvc == nil {
		return &callback.Response{
			CallbackMsg: "通知服务未启用",
			ShowAlert:   true,
		}, nil
	}

	h.mediaNotificationSvc.SetMode(ctx.UserID, services.ModeInstant)

	return &callback.Response{
		Text:        "✅ 已切换到单集推送模式",
		CallbackMsg: "模式已切换",
		Edit:        true,
	}, nil
}

// handleNotifModeDaily handles switching to daily summary mode
func (h *AdminHandler) handleNotifModeDaily(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	if h.mediaNotificationSvc == nil {
		return &callback.Response{
			CallbackMsg: "通知服务未启用",
			ShowAlert:   true,
		}, nil
	}

	h.mediaNotificationSvc.SetMode(ctx.UserID, services.ModeDaily)

	return &callback.Response{
		Text:        "✅ 已切换到每日汇总模式",
		CallbackMsg: "模式已切换",
		Edit:        true,
	}, nil
}

// handleNotifToggle handles toggling notifications on/off
func (h *AdminHandler) handleNotifToggle(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	if h.mediaNotificationSvc == nil {
		return &callback.Response{
			CallbackMsg: "通知服务未启用",
			ShowAlert:   true,
		}, nil
	}

	enabled := h.mediaNotificationSvc.ToggleEnabled(ctx.UserID)

	statusText := "已启用"
	if !enabled {
		statusText = "已禁用"
	}

	return &callback.Response{
		Text:        fmt.Sprintf("✅ 通知%s", statusText),
		CallbackMsg: statusText,
		Edit:        true,
	}, nil
}

// handleNotifSetTime handles setting daily summary time
func (h *AdminHandler) handleNotifSetTime(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	if h.mediaNotificationSvc == nil {
		return &callback.Response{
			CallbackMsg: "通知服务未启用",
			ShowAlert:   true,
		}, nil
	}

	// Get time from params or show time selection
	timeStr := ctx.Callback.Params["time"]
	if timeStr != "" {
		// Validate time format (HH:MM)
		if len(timeStr) == 5 && timeStr[2] == ':' {
			h.mediaNotificationSvc.SetDailyTime(ctx.UserID, timeStr)
			return &callback.Response{
				Text:        fmt.Sprintf("✅ 汇总时间已设为 %s", timeStr),
				CallbackMsg: "时间已设置",
				Edit:        true,
			}, nil
		}
	}

	// Show time selection keyboard
	msg := services.NewMessageBuilder()
	msg.Bold("⏰ 设置每日汇总时间").Newline()
	msg.Newline()
	msg.Text("请选择汇总发送时间：").Newline()

	kb := services.NewKeyboardBuilder()

	// Common time options
	times := []string{"08:00", "12:00", "18:00", "20:00", "21:00", "22:00", "23:00"}
	for i, t := range times {
		kb.AddButton(t, fmt.Sprintf("admin_notif_settime:time:%s", t))
		if i%2 == 1 && i < len(times)-1 {
			kb.NewRow()
		}
	}
	kb.NewRow()
	kb.AddButton("⬅️ 返回设置", "admin_notif_settings")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleNotifFormatSimple handles switching to simple notification format
func (h *AdminHandler) handleNotifFormatSimple(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	if h.mediaNotificationSvc == nil {
		return &callback.Response{
			CallbackMsg: "通知服务未启用",
			ShowAlert:   true,
		}, nil
	}

	h.mediaNotificationSvc.SetFormat(ctx.UserID, services.FormatSimple)

	return &callback.Response{
		Text:        "✅ 已切换到简洁格式",
		CallbackMsg: "格式已切换",
		Edit:        true,
	}, nil
}

// handleNotifFormatDetailed handles switching to detailed notification format
func (h *AdminHandler) handleNotifFormatDetailed(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	if h.mediaNotificationSvc == nil {
		return &callback.Response{
			CallbackMsg: "通知服务未启用",
			ShowAlert:   true,
		}, nil
	}

	h.mediaNotificationSvc.SetFormat(ctx.UserID, services.FormatDetailed)

	return &callback.Response{
		Text:        "✅ 已切换到详细格式",
		CallbackMsg: "格式已切换",
		Edit:        true,
	}, nil
}

// getModeText returns the display text for a notification mode
func (h *AdminHandler) getModeText(mode services.NotificationMode) string {
	switch mode {
	case services.ModeInstant:
		return "单集推送"
	case services.ModeDaily:
		return "每日汇总"
	default:
		return "未知"
	}
}
