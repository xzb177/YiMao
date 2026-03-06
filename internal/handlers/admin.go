package handlers

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/config"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
	"emby-telegram-bot/pkg/types"
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
	issueService         *services.IssueService
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

// SetIssueService sets the issue service
func (h *AdminHandler) SetIssueService(svc *services.IssueService) {
	h.issueService = svc
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
	case "admin_notif_toggle_daily_v2":
		return h.handleNotifToggleDailyV2(ctx)
	case "admin_notif_settime":
		return h.handleNotifSetTime(ctx)
	case "admin_notif_custom_time":
		return h.handleNotifCustomTime(ctx)
	// 管理员管理回调 - 仅 Root 可用
	case "admin_mgmt":
		return h.handleAdminMgmt(ctx)
	case "admin_list":
		return h.handleAdminList(ctx)
	case "admin_add_start":
		return h.handleAdminAddStart(ctx)
	case "admin_remove_list":
		return h.handleAdminRemoveList(ctx)
	case "admin_remove_confirm":
		return h.handleAdminRemoveConfirm(ctx)
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

	// Update issue status
	if h.issueService != nil {
		if err := h.issueService.UpdateStatus(issueID, services.IssueStatusFixed); err != nil {
			log.Printf("[AdminHandler] Failed to update issue status: %v", err)
		}
		// Get issue and notify user
		if issue, exists := h.issueService.GetIssue(issueID); exists {
			// Notify the user who created the issue
			msg := services.NewMessageBuilder()
			msg.Bold("✅ 您的问题已解决").Newline()
			msg.Newline()
			msg.Textf("问题编号: #%d", issue.ID).Newline()
			msg.Textf("问题类型: %s", issue.Title).Newline()
			if issue.MediaTitle != "" {
				msg.Textf("相关媒体: %s", issue.MediaTitle).Newline()
			}
			msg.Newline()
			msg.Italic("💡 感谢您的反馈，帮助我们改进服务").Newline()

			kb := services.NewKeyboardBuilder()
			kb.AddButton("⬅️ 返回主菜单", "start")

			h.telegram.SendMessage(issue.UserID, msg.Build(), "HTML", kb.Build())
			log.Printf("[AdminHandler] Notified user %d about issue #%d being fixed", issue.UserID, issue.ID)
		}
	}

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

	// Update issue status
	if h.issueService != nil {
		if err := h.issueService.UpdateStatus(issueID, services.IssueStatusProcessing); err != nil {
			log.Printf("[AdminHandler] Failed to update issue status: %v", err)
		}
		// Get issue and notify user
		if issue, exists := h.issueService.GetIssue(issueID); exists {
			// Notify the user who created the issue
			msg := services.NewMessageBuilder()
			msg.Bold("🔧 您的问题正在处理中").Newline()
			msg.Newline()
			msg.Textf("问题编号: #%d", issue.ID).Newline()
			msg.Textf("问题类型: %s", issue.Title).Newline()
			if issue.MediaTitle != "" {
				msg.Textf("相关媒体: %s", issue.MediaTitle).Newline()
			}
			msg.Newline()
			msg.Italic("💡 管理员正在处理您的问题，请耐心等待").Newline()

			kb := services.NewKeyboardBuilder()
			kb.AddButton("⬅️ 返回主菜单", "start")

			h.telegram.SendMessage(issue.UserID, msg.Build(), "HTML", kb.Build())
			log.Printf("[AdminHandler] Notified user %d about issue #%d being processed", issue.UserID, issue.ID)
		}
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

	// Update issue status
	if h.issueService != nil {
		if err := h.issueService.UpdateStatus(issueID, services.IssueStatusClosed); err != nil {
			log.Printf("[AdminHandler] Failed to update issue status: %v", err)
		}
		// Get issue and notify user
		if issue, exists := h.issueService.GetIssue(issueID); exists {
			// Notify the user who created the issue
			msg := services.NewMessageBuilder()
			msg.Bold("🚫 您的问题已关闭").Newline()
			msg.Newline()
			msg.Textf("问题编号: #%d", issue.ID).Newline()
			msg.Textf("问题类型: %s", issue.Title).Newline()
			if issue.MediaTitle != "" {
				msg.Textf("相关媒体: %s", issue.MediaTitle).Newline()
			}
			msg.Newline()
			msg.Italic("💡 如仍有问题，请重新提交反馈").Newline()

			kb := services.NewKeyboardBuilder()
			kb.AddButton("⬅️ 返回主菜单", "start")

			h.telegram.SendMessage(issue.UserID, msg.Build(), "HTML", kb.Build())
			log.Printf("[AdminHandler] Notified user %d about issue #%d being closed", issue.UserID, issue.ID)
		}
	}

	message := fmt.Sprintf("❌ 问题已关闭\n\n问题ID: %d", issueID)

	return &callback.Response{
		Text:   message,
		Edit:   true,
	}, nil
}

// handleAdminMenu handles admin menu callback
func (h *AdminHandler) handleAdminMenu(ctx *callback.Context) (*callback.Response, error) {
	log.Printf("[AdminHandler] handleAdminMenu called by user %d", ctx.UserID)

	// Check if user is admin
	if !h.adminService.IsAdmin(ctx.UserID) {
		log.Printf("[AdminHandler] User %d is not admin", ctx.UserID)
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	// Check if user is root admin for special menu
	isRoot := h.adminService.IsRootAdmin(ctx.UserID)

	msg := services.NewMessageBuilder()
	msg.Bold("🔧 管理员菜单").Newline()
	msg.Newline()
	msg.Text("请选择操作：").Newline()

	kb := services.NewKeyboardBuilder()

	// Media notification settings
	log.Printf("[AdminHandler] mediaNotificationSvc is nil: %v", h.mediaNotificationSvc == nil)
	if h.mediaNotificationSvc != nil {
		log.Printf("[AdminHandler] Getting settings for user %d", ctx.UserID)
		settings := h.mediaNotificationSvc.GetSettings(ctx.UserID)
		log.Printf("[AdminHandler] Got settings: daily=%v", settings.DailySummaryEnabled)

		// 群组通知状态（全局开启）
		msg.Bold("✅ 媒体库通知").Newline()
		
		// 群组通知
		msg.Textf("   📺 群组通知: %s", "已开启").Newline()
		
		// 每日汇总状态
		dailyIcon := "📅"
		if !settings.DailySummaryEnabled {
			dailyIcon = "📅"
		}
		msg.Textf("   %s 每日汇总: %s", dailyIcon, h.getBoolText(settings.DailySummaryEnabled)).Newline()
		msg.Textf("   ⏰ 汇总时间: %s", settings.DailyTime).Newline()
		msg.Newline()

		kb.AddButton("🔔 通知设置", "admin_notif_settings")
		kb.NewRow()
		log.Printf("[AdminHandler] Added notification settings button")
	}

	// Admin management - only for root admin
	if isRoot {
		adminCount := h.adminService.GetAdminCount()
		roleText := "普通管理员"
		if isRoot {
			roleText = "👑 超级管理员"
		}
		msg.Bold(fmt.Sprintf("🛡️ 管理员管理 (%s)", roleText)).Newline()
		msg.Textf("   当前共有 %d 位管理员", adminCount).Newline()
		msg.Newline()

		kb.AddButton("🛡️ 管理员设置", "admin_mgmt")
		kb.NewRow()
	}

	// Return button
	kb.AddButton("⬅️ 返回主菜单", "start")

	resultText := msg.Build()
	log.Printf("[AdminHandler] Returning admin menu with %d chars text, isRoot=%v", len(resultText), isRoot)

	return &callback.Response{
		Text:     resultText,
		ParseMode: msg.ParseMode(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleNotifSettings handles notification settings callback
// 重构后：群组通知全局开启，管理员只能控制每日汇总（私聊）
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

	// 获取群组ID显示
	groupStatus := "未配置"
	if h.cfg.TelegramChatID != "" {
		groupStatus = h.cfg.TelegramChatID
	}

	msg := services.NewMessageBuilder()
	msg.Bold("⚙️ 入库通知设置").Newline()
	msg.Newline()
	
	// 群组通知状态（全局开启）
	msg.Bold("📺 群组通知").Newline()
	msg.Text("   状态: ✅ 全局开启").Newline()
	msg.Textf("   群组 ID: %s", groupStatus).Newline()
	msg.Newline()
	
	// 每日汇总状态（可配置）
	msg.Bold("📰 每日汇总").Newline()
	dailyStatus := "关闭"
	dailyIcon := "❌"
	if settings.DailySummaryEnabled {
		dailyStatus = "开启"
		dailyIcon = "✅"
	}
	msg.Textf("   状态: %s %s", dailyIcon, dailyStatus).Newline()
	msg.Textf("   时间: %s", settings.DailyTime).Newline()
	msg.Newline()
	
	msg.Italic("💡 群组通知直接发送到配置的群组，每日汇总发送到私聊").Newline()

	kb := services.NewKeyboardBuilder()

	// 每日汇总开关
	kb.AddButton(fmt.Sprintf("📰 每日汇总: %s %s", dailyIcon, dailyStatus), "admin_notif_toggle_daily_v2")
	kb.NewRow()

	// 汇总时间设置
	kb.AddButton(fmt.Sprintf("⏰ 汇总时间: %s ✏️", settings.DailyTime), "admin_notif_settime")
	kb.NewRow()

	// 返回按钮
	kb.AddButton("⬅️ 返回管理员菜单", "admin_menu")

	return &callback.Response{
		Text:      msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
	}, nil
}

// handleNotifToggleSingleV2 处理单集推送切换 - 仅更新按钮，不发新消息

// handleNotifToggleDailyV2 处理每日汇总切换
func (h *AdminHandler) handleNotifToggleDailyV2(ctx *callback.Context) (*callback.Response, error) {
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

	settings := h.mediaNotificationSvc.GetSettings(ctx.UserID)
	newState := !settings.DailySummaryEnabled
	h.mediaNotificationSvc.SetDailySummaryEnabled(ctx.UserID, newState)

	// 防抖提示
	callbackMsg := "每日汇总已关闭"
	if newState {
		callbackMsg = "每日汇总已开启"
	}

	// 重新构建界面
	updatedSettings := h.mediaNotificationSvc.GetSettings(ctx.UserID)

	// 获取群组ID显示
	groupStatus := "未配置"
	if h.cfg.TelegramChatID != "" {
		groupStatus = h.cfg.TelegramChatID
	}

	msg := services.NewMessageBuilder()
	msg.Bold("⚙️ 入库通知设置").Newline()
	msg.Newline()
	
	// 群组通知状态（全局开启）
	msg.Bold("📺 群组通知").Newline()
	msg.Text("   状态: ✅ 全局开启").Newline()
	msg.Textf("   群组 ID: %s", groupStatus).Newline()
	msg.Newline()
	
	// 每日汇总状态（可配置）
	msg.Bold("📰 每日汇总").Newline()
	dailyStatus := "关闭"
	dailyIcon := "❌"
	if updatedSettings.DailySummaryEnabled {
		dailyStatus = "开启"
		dailyIcon = "✅"
	}
	msg.Textf("   状态: %s %s", dailyIcon, dailyStatus).Newline()
	msg.Textf("   时间: %s", updatedSettings.DailyTime).Newline()
	msg.Newline()
	
	msg.Italic("💡 群组通知直接发送到配置的群组，每日汇总发送到私聊").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton(fmt.Sprintf("📰 每日汇总: %s %s", dailyIcon, dailyStatus), "admin_notif_toggle_daily_v2")
	kb.NewRow()
	kb.AddButton(fmt.Sprintf("⏰ 汇总时间: %s ✏️", updatedSettings.DailyTime), "admin_notif_settime")
	kb.NewRow()
	kb.AddButton("⬅️ 返回管理员菜单", "admin_menu")

	return &callback.Response{
		Text:      msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
		CallbackMsg: callbackMsg,
	}, nil
}

// handleNotifToggleFormat 处理格式切换 - 在"详细"和"简洁"之间循环

// handleNotifDisableAll 停用所有通知

// buildNotifSettingsKeyboard 构建通知设置的按钮键盘（用于原地刷新）

// handleNotifToggleInstant handles toggling instant notifications

// handleNotifToggleDaily handles toggling daily summary

// handleNotifToggle handles toggling notifications on/off (overall)

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

			// Build response with keyboard to continue
			msg := services.NewMessageBuilder()
			msg.Bold("✅ 汇总时间已设为 ").Text(timeStr).Newline()
			msg.Newline()
			msg.Italic("💡 您可以继续调整其他设置").Newline()

			kb := services.NewKeyboardBuilder()
			kb.AddButton("⬅️ 返回设置", "admin_notif_settings")

			return &callback.Response{
				Text:     msg.Build(),
				ParseMode: msg.ParseMode(),
				CallbackMsg: "时间已设置",
				Edit:     true,
				Keyboard: convertKeyboard(kb.Build()),
			}, nil
		}
	}

	// Show time selection keyboard with custom input option
	msg := services.NewMessageBuilder()
	msg.Bold("⏰ 设置每日汇总时间").Newline()
	msg.Newline()
	msg.Text("请选择预设时间或输入自定义时间（格式：HH:MM）").Newline()
	msg.Newline()
	msg.Italic("💡 例如：23:00、08:30").Newline()

	kb := services.NewKeyboardBuilder()

	// Common time options
	times := []string{"08:00", "12:00", "18:00", "20:00", "21:00", "22:00", "23:00", "23:59"}
	for i, t := range times {
		kb.AddButton(t, fmt.Sprintf("admin_notif_settime:time:%s", t))
		if i%2 == 1 && i < len(times)-1 {
			kb.NewRow()
		}
	}
	kb.NewRow()
	kb.AddButton("✏️ 自定义时间", "admin_notif_custom_time")
	kb.NewRow()
	kb.AddButton("⬅️ 返回设置", "admin_notif_settings")

	return &callback.Response{
		Text:     msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleNotifCustomTime handles custom time input
func (h *AdminHandler) handleNotifCustomTime(ctx *callback.Context) (*callback.Response, error) {
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

	// Set session state for custom time input
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	sess.Set("waiting_for_time_input", true)
	sess.Set("previous_menu", "admin_notif_settings")

	msg := services.NewMessageBuilder()
	msg.Bold("⏰ 输入自定义时间").Newline()
	msg.Newline()
	msg.Text("请输入汇总发送时间").Newline()
	msg.Newline()
	msg.Italic("💡 格式：HH:MM，例如：23:00、08:30").Newline()
	msg.Italic("💡 范围：00:00 - 23:59").Newline()
	msg.Newline()
	msg.Italic("❌ 输入 /cancel 或「取消」可退出").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("⬅️ 取消", "admin_notif_settings")

	return &callback.Response{
		Text:     msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleNotifFormatSimple handles switching to simple notification format

// handleNotifFormatDetailed handles switching to detailed notification format

// getBoolText returns the display text for a boolean
func (h *AdminHandler) getBoolText(v bool) string {
	if v {
		return "开启"
	}
	return "关闭"
}

// HandleNotifCustomTimeInput handles custom time input for daily summary
func (h *AdminHandler) HandleNotifCustomTimeInput(userID int64, chatID int64, text string) (*callback.Response, error) {
	// Trim and validate input
	trimmed := strings.TrimSpace(text)

	// Check for cancel commands
	if strings.ToLower(trimmed) == "/cancel" || trimmed == "取消" {
		kb := services.NewKeyboardBuilder()
		kb.AddButton("⬅️ 返回设置", "admin_notif_settings")
		msg := services.NewMessageBuilder()
		msg.Bold("⏰ 设置每日汇总时间").Newline()
		msg.Newline()
		msg.Text("已取消").Newline()

		return &callback.Response{
			Text:     msg.Build(),
			ParseMode: msg.ParseMode(),
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	// Validate time format (HH:MM)
	if len(trimmed) != 5 || trimmed[2] != ':' {
		msg := services.NewMessageBuilder()
		msg.Bold("❌ 无效的时间格式").Newline()
		msg.Newline()
		msg.Text("请使用 HH:MM 格式，例如：23:00、08:30").Newline()
		msg.Newline()
		msg.Italic("💡 范围：00:00 - 23:59").Newline()

		kb := services.NewKeyboardBuilder()
		kb.AddButton("⬅️ 返回设置", "admin_notif_settings")

		return &callback.Response{
			Text:     msg.Build(),
			ParseMode: msg.ParseMode(),
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	// Validate hour and minute
	hourStr := trimmed[:2]
	minuteStr := trimmed[3:]
	hour, err1 := strconv.Atoi(hourStr)
	minute, err2 := strconv.Atoi(minuteStr)

	if err1 != nil || err2 != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		msg := services.NewMessageBuilder()
		msg.Bold("❌ 无效的时间").Newline()
		msg.Newline()
		msg.Textf("小时：00-23，分钟：00-59").Newline()
		msg.Newline()
		msg.Italic(fmt.Sprintf("您的输入：%s", trimmed)).Newline()
		msg.Newline()
		msg.Italic("💡 正确格式：23:00、08:30").Newline()

		kb := services.NewKeyboardBuilder()
		kb.AddButton("⬅️ 返回设置", "admin_notif_settings")

		return &callback.Response{
			Text:     msg.Build(),
			ParseMode: msg.ParseMode(),
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	// Set the time
	timeStr := fmt.Sprintf("%02d:%02d", hour, minute)
	h.mediaNotificationSvc.SetDailyTime(userID, timeStr)

	// Show success and return to settings menu
	kb := services.NewKeyboardBuilder()
	kb.AddButton("⬅️ 返回设置", "admin_notif_settings")
	msg := services.NewMessageBuilder()
	msg.Bold("✅ 时间设置成功").Newline()
	msg.Newline()
	msg.Textf("每日汇总将在 %s 发送", timeStr).Newline()

	return &callback.Response{
		Text:     msg.Build(),
		ParseMode: msg.ParseMode(),
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// ============================================================
// 管理员管理模块 (仅超级管理员可用)
// ============================================================

// handleAdminMgmt handles the admin management submenu
func (h *AdminHandler) handleAdminMgmt(ctx *callback.Context) (*callback.Response, error) {
	// Only root admin can access this
	if !h.adminService.IsRootAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "此功能仅限超级管理员使用",
			ShowAlert:   true,
		}, nil
	}

	msg := services.NewMessageBuilder()
	msg.Bold("🛡️ 管理员设置").Newline()
	msg.Newline()
	msg.Text("管理机器人管理员权限").Newline()
	msg.Newline()
	msg.Italic("💡 提示：超级管理员（👑）无法被移除").Newline()

	kb := services.NewKeyboardBuilder()

	// 第一行：查看列表
	kb.AddButton("📋 查看管理员列表", "admin_list")
	kb.NewRow()

	// 第二行：添加和移除
	kb.AddButton("➕ 添加管理员", "admin_add_start")
	kb.AddButton("➖ 移除管理员", "admin_remove_list")
	kb.NewRow()

	// 第三行：返回
	kb.AddButton("⬅️ 返回管理员菜单", "admin_menu")

	return &callback.Response{
		Text:     msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleAdminList displays all admins
func (h *AdminHandler) handleAdminList(ctx *callback.Context) (*callback.Response, error) {
	// Only root admin can access this
	if !h.adminService.IsRootAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "此功能仅限超级管理员使用",
			ShowAlert:   true,
		}, nil
	}

	admins := h.adminService.GetAllAdminInfo()

	msg := services.NewMessageBuilder()
	msg.Bold("📋 管理员列表").Newline()
	msg.Newline()

	if len(admins) == 0 {
		msg.Text("暂无管理员").Newline()
	} else {
		for i, admin := range admins {
			roleMark := "👑 "
			if admin.Role != services.AdminRoleRoot {
				roleMark = "  "
			}
			name := admin.Name
			if name == "" {
				name = "未命名"
			}
			msg.Code(fmt.Sprintf("%d. %s%s (%d)", i+1, roleMark, name, admin.UserID)).Newline()
		}
		msg.Newline()
		msg.Italic(fmt.Sprintf("共 %d 位管理员", len(admins))).Newline()
	}

	kb := services.NewKeyboardBuilder()
	kb.AddButton("⬅️ 返回管理员设置", "admin_mgmt")

	return &callback.Response{
		Text:     msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleAdminAddStart starts the add admin flow
func (h *AdminHandler) handleAdminAddStart(ctx *callback.Context) (*callback.Response, error) {
	// Only root admin can access this
	if !h.adminService.IsRootAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "此功能仅限超级管理员使用",
			ShowAlert:   true,
		}, nil
	}

	// Set session state
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	sess.Set("waiting_for_add_admin", true)
	sess.Set("previous_menu", "admin_mgmt")

	msg := services.NewMessageBuilder()
	msg.Bold("➕ 添加管理员").Newline()
	msg.Newline()
	msg.Text("请通过以下任一方式添加新管理员：").Newline()
	msg.Newline()
	msg.Text("1️⃣ 直接输入新管理员的 Telegram 数字 ID").Newline()
	msg.Text("2️⃣ 将他的一条消息转发给我").Newline()
	msg.Newline()
	msg.Italic("💡 转发消息时，我会自动提取发送者的 ID").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("⬅️ 取消", "admin_mgmt")

	return &callback.Response{
		Text:     msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleAdminRemoveList displays list of admins that can be removed
func (h *AdminHandler) handleAdminRemoveList(ctx *callback.Context) (*callback.Response, error) {
	// Only root admin can access this
	if !h.adminService.IsRootAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "此功能仅限超级管理员使用",
			ShowAlert:   true,
		}, nil
	}

	admins := h.adminService.GetAllAdminInfo()

	msg := services.NewMessageBuilder()
	msg.Bold("➖ 移除管理员").Newline()
	msg.Newline()

	kb := services.NewKeyboardBuilder()

	removableCount := 0
	for _, admin := range admins {
		// Skip root admin
		if admin.Role == services.AdminRoleRoot {
			continue
		}

		removableCount++
		name := admin.Name
		if name == "" {
			name = "未命名"
		}

		// Create remove button for each admin
		btnText := fmt.Sprintf("❌ %s (%d)", name, admin.UserID)
		callbackData := fmt.Sprintf("admin_remove_confirm:id:%d", admin.UserID)
		kb.AddButton(btnText, callbackData)
		kb.NewRow()
	}

	if removableCount == 0 {
		msg.Text("当前没有可移除的管理员").Newline()
		msg.Newline()
		kb.AddButton("⬅️ 返回管理员设置", "admin_mgmt")
	} else {
		msg.Textf("选择要移除的管理员（共 %d 位）:", removableCount).Newline()
		msg.Newline()
		kb.AddButton("⬅️ 返回管理员设置", "admin_mgmt")
	}

	return &callback.Response{
		Text:     msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleAdminRemoveConfirm confirms and removes an admin
func (h *AdminHandler) handleAdminRemoveConfirm(ctx *callback.Context) (*callback.Response, error) {
	// Only root admin can access this
	if !h.adminService.IsRootAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "此功能仅限超级管理员使用",
			ShowAlert:   true,
		}, nil
	}

	// Parse admin ID from params
	idStr := ctx.Callback.Params["id"]
	if idStr == "" {
		return &callback.Response{
			CallbackMsg: "无效的管理员 ID",
			ShowAlert:   true,
		}, nil
	}

	adminID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return &callback.Response{
			CallbackMsg: "无效的管理员 ID",
			ShowAlert:   true,
		}, nil
	}

	// Check if trying to remove root admin
	if h.adminService.IsRootAdmin(adminID) {
		return &callback.Response{
			CallbackMsg: "无法移除超级管理员",
			ShowAlert:   true,
		}, nil
	}

	// Get admin name before removing
	name := h.adminService.GetAdminName(adminID)

	// Remove admin
	if err := h.adminService.RemoveAdmin(adminID); err != nil {
		log.Printf("[AdminHandler] Failed to remove admin %d: %v", adminID, err)
		return &callback.Response{
			CallbackMsg: "移除失败，请稍后再试",
			ShowAlert:   true,
		}, nil
	}

	// Refresh the remove list
	admins := h.adminService.GetAllAdminInfo()

	msg := services.NewMessageBuilder()
	msg.Bold("➕ 移除成功").Newline()
	msg.Newline()
	msg.Textf("已移除管理员: %s (%d)", name, adminID).Newline()
	msg.Newline()

	kb := services.NewKeyboardBuilder()

	// Show remaining removable admins
	removableCount := 0
	for _, admin := range admins {
		if admin.Role == services.AdminRoleRoot {
			continue
		}
		removableCount++
		adminName := admin.Name
		if adminName == "" {
			adminName = "未命名"
		}
		btnText := fmt.Sprintf("❌ %s (%d)", adminName, admin.UserID)
		callbackData := fmt.Sprintf("admin_remove_confirm:id:%d", admin.UserID)
		kb.AddButton(btnText, callbackData)
		kb.NewRow()
	}

	if removableCount == 0 {
		msg.Text("当前没有可移除的管理员").Newline()
		kb.AddButton("⬅️ 返回管理员设置", "admin_mgmt")
	} else {
		msg.Textf("剩余可移除: %d 位", removableCount).Newline()
		kb.AddButton("⬅️ 返回管理员设置", "admin_mgmt")
	}

	return &callback.Response{
		Text:        msg.Build(),
		ParseMode:   msg.ParseMode(),
		Edit:        true,
		Keyboard:    convertKeyboard(kb.Build()),
		CallbackMsg: fmt.Sprintf("已移除 %s", name),
	}, nil
}

// HandleAdminAddMessage handles incoming message when waiting for admin ID
// This is called from the poll handler when in "waiting_for_add_admin" state
func (h *AdminHandler) HandleAdminAddMessage(userID int64, chatID int64, message *types.TelegramMessage) (*callback.Response, error) {
	// Verify user is in the correct state
	sess := h.sessMgr.GetOrCreate(userID)
	if sess == nil || !h.sessMgr.IsValid(userID) {
		return nil, nil // Invalid session
	}

	// Check if we have the state flag
	if _, exists := sess.Get("waiting_for_add_admin"); !exists {
		return nil, nil // Not in add admin state
	}

	var targetID int64
	var targetName string
	var source string

	// Currently only support manual ID input
	// (Forward message support requires extending TelegramMessage structure)
	trimmed := strings.TrimSpace(message.Text)
	parsedID, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		msg := services.NewMessageBuilder()
		msg.Bold("❌ 无效的 ID").Newline()
		msg.Newline()
		msg.Text("请输入有效的 Telegram 数字 ID").Newline()
		msg.Newline()
		msg.Italic("💡 提示：ID 是一串纯数字").Newline()

		kb := services.NewKeyboardBuilder()
		kb.AddButton("⬅️ 取消", "admin_mgmt")

		return &callback.Response{
			Text:     msg.Build(),
			ParseMode: msg.ParseMode(),
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	targetID = parsedID
	source = "手动输入"

	// Clear the waiting state
	sess.Delete("waiting_for_add_admin")

	// Check if already an admin
	if h.adminService.IsAdmin(targetID) {
		role := "普通管理员"
		if h.adminService.IsRootAdmin(targetID) {
			role = "超级管理员"
		}

		msg := services.NewMessageBuilder()
		msg.Bold("⚠️ 已是管理员").Newline()
		msg.Newline()
		msg.Textf("用户 ").Code(fmt.Sprintf("%d", targetID)).Textf(" 已经是 %s", role).Newline()
		msg.Newline()
		msg.Italic("如需修改权限，请联系开发者").Newline()

		kb := services.NewKeyboardBuilder()
		kb.AddButton("⬅️ 返回管理员设置", "admin_mgmt")

		return &callback.Response{
			Text:     msg.Build(),
			ParseMode: msg.ParseMode(),
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	// Add the admin
	if targetName == "" {
		targetName = fmt.Sprintf("Admin_%d", targetID)
	}
	if err := h.adminService.AddAdmin(targetID, targetName); err != nil {
		log.Printf("[AdminHandler] Failed to add admin: %v", err)
		return &callback.Response{
			Text: "❌ 添加失败，请稍后再试",
		}, nil
	}

	// Success message
	msg := services.NewMessageBuilder()
	msg.Bold("✅ 添加成功").Newline()
	msg.Newline()
	msg.Textf("来源: %s", source).Newline()
	msg.Textf("ID: ").Code(fmt.Sprintf("%d", targetID)).Newline()
	msg.Textf("昵称: %s", targetName).Newline()
	msg.Newline()
	msg.Italic("新管理员可以访问管理员菜单进行审批操作").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("⬅️ 返回管理员设置", "admin_mgmt")

	return &callback.Response{
		Text:     msg.Build(),
		ParseMode: msg.ParseMode(),
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}
