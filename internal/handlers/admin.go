package handlers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/config"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
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
	reviewService        *services.ReviewService
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

// SetReviewService sets the review service used by admin panels.
func (h *AdminHandler) SetReviewService(svc *services.ReviewService) {
	h.reviewService = svc
}

// Handle handles admin callbacks
func (h *AdminHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	// 统一入口权限检查：非管理员直接拒绝
	if h.adminService == nil || !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			Text:        "❌ 此操作仅限管理员",
			CallbackMsg: "无权限",
			ShowAlert:   true,
		}, nil
	}

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
	case "admin_notif_toggle_single_v2":
		return h.handleNotifToggleSingle(ctx)
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
	// Admin feedback panel callbacks
	case "admin_feedback":
		return h.handleFeedbackPanel(ctx)
	case "admin_feedback_stats":
		return h.handleFeedbackStats(ctx)
	case "admin_feedback_list":
		return h.handleFeedbackList(ctx)
	case "admin_feedback_filter":
		return h.handleFeedbackFilter(ctx)
	case "admin_feedback_detail":
		return h.handleFeedbackDetail(ctx)
	case "admin_feedback_reply":
		return h.handleFeedbackReply(ctx)
	case "admin_feedback_priority":
		return h.handleFeedbackPriority(ctx)
	case "admin_feedback_priority_menu":
		return h.handleFeedbackPriorityMenu(ctx)
	case "admin_feedback_template":
		return h.handleFeedbackTemplate(ctx)
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
		logger.Info("[AdminHandler] Approve request %d (MoviePilot)", requestID)
	}

	message := fmt.Sprintf("✅ 请求已批准\n\n请求ID: %d", requestID)

	return &callback.Response{
		Text:        message,
		CallbackMsg: "已批准",
		Edit:        true,
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
		logger.Info("[AdminHandler] Decline request %d (MoviePilot)", requestID)
	}

	message := fmt.Sprintf("❌ 请求已拒绝\n\n请求ID: %d", requestID)

	return &callback.Response{
		Text:        message,
		CallbackMsg: "已拒绝",
		Edit:        true,
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

	if h.reviewService == nil {
		return &callback.Response{
			Text: "❌ 审核服务未就绪",
			Edit: true,
		}, nil
	}

	pending := h.reviewService.GetPendingRequests()
	if len(pending) == 0 {
		kb := services.NewKeyboardBuilder()
		kb.AddButton("⬅️ 返回管理面板", "admin_menu")
		return &callback.Response{
			Text:     "📋 待处理请求\n\n✅ 暂无待审核请求",
			Edit:     true,
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	msg := services.NewMessageBuilder()
	msg.Bold("📋 待处理请求").Newline()
	msg.Italic(fmt.Sprintf("当前共有 %d 条待审核", len(pending))).Newline()
	msg.Newline()

	limit := len(pending)
	if limit > 10 {
		limit = 10
	}
	for i := 0; i < limit; i++ {
		req := pending[i]
		mediaType := "电影"
		if req.MediaType == services.MediaTypeTV {
			mediaType = "剧集"
		}
		msg.Textf("%d. %s《%s》(%d)", i+1, mediaType, req.MediaTitle, req.MediaYear).Newline()
		msg.Textf("   👤 %s · %s", req.TelegramName, req.CreatedAt.Format("01-02 15:04")).Newline()
	}
	if len(pending) > limit {
		msg.Newline().Italic(fmt.Sprintf("还有 %d 条，请进入审核列表分页处理", len(pending)-limit))
	}

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🧾 进入审核列表", "review_list")
	kb.NewRow()
	kb.AddButton("⬅️ 返回管理面板", "admin_menu")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
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
		Text: message,
		Edit: true,
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
			logger.Info("[AdminHandler] Failed to update issue status: %v", err)
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
			logger.Info("[AdminHandler] Notified user %d about issue #%d being fixed", issue.UserID, issue.ID)
		}
	}

	message := fmt.Sprintf("✅ 问题已标记为修复\n\n问题ID: %d", issueID)

	return &callback.Response{
		Text: message,
		Edit: true,
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
			logger.Info("[AdminHandler] Failed to update issue status: %v", err)
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
			logger.Info("[AdminHandler] Notified user %d about issue #%d being processed", issue.UserID, issue.ID)
		}
	}

	message := fmt.Sprintf("ℹ️ 问题已标记为处理中\n\n问题ID: %d", issueID)

	return &callback.Response{
		Text: message,
		Edit: true,
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
			logger.Info("[AdminHandler] Failed to update issue status: %v", err)
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
			logger.Info("[AdminHandler] Notified user %d about issue #%d being closed", issue.UserID, issue.ID)
		}
	}

	// Delete the message to remove keyboard, then send new confirmation message
	go func() {
		if err := h.telegram.DeleteMessage(ctx.ChatID, ctx.MessageID); err != nil {
			logger.Info("[AdminHandler] Failed to delete message: %v", err)
		}
		msg := services.NewMessageBuilder()
		msg.Bold("🚫 问题已关闭").Newline()
		msg.Newline()
		msg.Textf("问题ID: #%d", issueID).Newline()
		msg.Text("状态: 已关闭").Newline()

		kb := services.NewKeyboardBuilder()
		kb.AddButton("⬅️ 返回", "start")

		h.telegram.SendMessage(ctx.ChatID, msg.Build(), "HTML", kb.Build())
	}()

	return &callback.Response{
		CallbackMsg: "问题已关闭",
	}, nil
}

// handleAdminMenu handles admin menu callback
func (h *AdminHandler) handleAdminMenu(ctx *callback.Context) (*callback.Response, error) {
	logger.Info("[AdminHandler] handleAdminMenu called by user %d", ctx.UserID)

	// Check if user is admin
	if !h.adminService.IsAdmin(ctx.UserID) {
		logger.Info("[AdminHandler] User %d is not admin", ctx.UserID)
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

	// Feedback management panel
	if h.issueService != nil {
		stats := h.issueService.GetStats()
		kb.AddButton("📊 反馈管理", "admin_feedback")
		kb.NewRow()

		msg.Bold("📊 反馈管理").Newline()
		msg.Textf("   总反馈: %d | 待处理: %d", stats.Total, stats.Open).Newline()
		msg.Newline()
	}

	// Media notification settings
	logger.Info("[AdminHandler] mediaNotificationSvc is nil: %v", h.mediaNotificationSvc == nil)
	if h.mediaNotificationSvc != nil {
		logger.Info("[AdminHandler] Getting settings for user %d", ctx.UserID)
		settings := h.mediaNotificationSvc.GetSettings(ctx.UserID)
		logger.Info("[AdminHandler] Got settings: daily=%v", settings.DailySummaryEnabled)

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
		logger.Info("[AdminHandler] Added notification settings button")
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
	logger.Info("[AdminHandler] Returning admin menu with %d chars text, isRoot=%v", len(resultText), isRoot)

	return &callback.Response{
		Text:      resultText,
		ParseMode: msg.ParseMode(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
	}, nil
}

// handleNotifSettings handles notification settings callback
// 两个开关：单集开关（群组）、汇总开关（群组+私聊）
func (h *AdminHandler) handleNotifSettings(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	if h.mediaNotificationSvc == nil {
		return &callback.Response{
			Text: "❌ 通知服务未启用",
			Edit: true,
		}, nil
	}

	settings := h.mediaNotificationSvc.GetSettings(ctx.UserID)

	// 获取群组ID显示
	groupStatus := "未配置"
	if h.cfg.TelegramChatID != "" {
		groupStatus = h.cfg.TelegramChatID
	}

	singleOn := settings.SingleEnabled
	dailyOn := settings.DailySummaryEnabled

	singleText := "关闭"
	if singleOn {
		singleText = "开启"
	}
	dailyText := "关闭"
	if dailyOn {
		dailyText = "开启"
	}

	msg := services.NewMessageBuilder()
	msg.Bold("⚙️ 入库通知设置").Newline()
	msg.Newline()
	msg.Textf("📡 群组：%s", groupStatus).Newline()
	msg.Textf("📦 单集推送：%s", singleText).Newline()
	msg.Textf("📰 每日汇总：%s（%s）", dailyText, settings.DailyTime).Newline()
	msg.Newline()
	msg.Italic("💡 单集发群组，汇总发群组和私聊").Newline()

	kb := services.NewKeyboardBuilder()

	if singleOn {
		kb.AddButton("📦 单集：开", "admin_notif_toggle_single_v2")
	} else {
		kb.AddButton("📦 单集：关", "admin_notif_toggle_single_v2")
	}

	if dailyOn {
		kb.AddButton("📰 汇总：开", "admin_notif_toggle_daily_v2")
	} else {
		kb.AddButton("📰 汇总：关", "admin_notif_toggle_daily_v2")
	}
	kb.NewRow()

	kb.AddButton(fmt.Sprintf("⏰ 时间：%s", settings.DailyTime), "admin_notif_settime")
	kb.NewRow()

	kb.AddButton("⬅️ 返回管理员菜单", "admin_menu")

	return &callback.Response{
		Text:      msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
	}, nil
}

// handleNotifToggleSingle 处理单集推送切换
func (h *AdminHandler) handleNotifToggleSingle(ctx *callback.Context) (*callback.Response, error) {
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
	oldState := settings.SingleEnabled
	h.mediaNotificationSvc.SetSingleEnabled(ctx.UserID, !oldState)

	// 切换后返回设置页面
	return h.handleNotifSettings(ctx)
}

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
	oldState := settings.DailySummaryEnabled
	h.mediaNotificationSvc.SetDailySummaryEnabled(ctx.UserID, !oldState)

	// 切换后返回设置页面
	return h.handleNotifSettings(ctx)
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
	if timeStr == "" {
		if compact := ctx.Callback.Params["t"]; compact != "" && len(compact) == 4 {
			timeStr = compact[:2] + ":" + compact[2:]
		}
	}
	if timeStr != "" {
		// Validate time format (HH:MM)
		if len(timeStr) == 5 && timeStr[2] == ':' {
			h.mediaNotificationSvc.SetDailyTime(ctx.UserID, timeStr)

			// Build response with keyboard to continue
			msg := services.NewMessageBuilder()
			msg.Bold("时间已更新").Newline()
			msg.Textf("当前时间：%s", timeStr).Newline()

			kb := services.NewKeyboardBuilder()
			kb.AddButton("返回设置", "admin_notif_settings")

			return &callback.Response{
				Text:        msg.Build(),
				ParseMode:   msg.ParseMode(),
				CallbackMsg: "时间已设置",
				Edit:        true,
				Keyboard:    convertKeyboard(kb.Build()),
			}, nil
		}
	}

	// Show time selection keyboard with custom input option
	msg := services.NewMessageBuilder()
	msg.Bold("设置每日汇总时间").Newline()
	msg.Newline()
	msg.Text("请选择预设时间，或输入自定义时间（HH:MM）").Newline()

	kb := services.NewKeyboardBuilder()

	// Common time options
	times := []string{"08:00", "12:00", "18:00", "20:00", "21:00", "22:00", "23:00", "23:59"}
	for i, t := range times {
		compact := strings.ReplaceAll(t, ":", "")
		kb.AddButton(t, fmt.Sprintf("admin_notif_settime:t:%s", compact))
		if i%2 == 1 && i < len(times)-1 {
			kb.NewRow()
		}
	}
	kb.NewRow()
	kb.AddButton("自定义时间", "admin_notif_custom_time")
	kb.NewRow()
	kb.AddButton("返回设置", "admin_notif_settings")

	return &callback.Response{
		Text:      msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
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
	msg.Bold("输入自定义时间").Newline()
	msg.Newline()
	msg.Text("请输入汇总发送时间（HH:MM）").Newline()
	msg.Text("例如：23:00 或 08:30").Newline()
	msg.Text("范围：00:00 - 23:59").Newline()
	msg.Newline()
	msg.Italic("输入 /cancel 或“取消”可退出").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("取消", "admin_notif_settings")

	return &callback.Response{
		Text:      msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
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
			Text:      msg.Build(),
			ParseMode: msg.ParseMode(),
			Keyboard:  convertKeyboard(kb.Build()),
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
			Text:      msg.Build(),
			ParseMode: msg.ParseMode(),
			Keyboard:  convertKeyboard(kb.Build()),
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
			Text:      msg.Build(),
			ParseMode: msg.ParseMode(),
			Keyboard:  convertKeyboard(kb.Build()),
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
		Text:      msg.Build(),
		ParseMode: msg.ParseMode(),
		Keyboard:  convertKeyboard(kb.Build()),
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
		Text:      msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
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
		Text:      msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
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
		Text:      msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
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
		Text:      msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
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
		logger.Info("[AdminHandler] Failed to remove admin %d: %v", adminID, err)
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
			Text:      msg.Build(),
			ParseMode: msg.ParseMode(),
			Keyboard:  convertKeyboard(kb.Build()),
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
			Text:      msg.Build(),
			ParseMode: msg.ParseMode(),
			Keyboard:  convertKeyboard(kb.Build()),
		}, nil
	}

	// Add the admin
	if targetName == "" {
		targetName = fmt.Sprintf("Admin_%d", targetID)
	}
	if err := h.adminService.AddAdmin(targetID, targetName); err != nil {
		logger.Info("[AdminHandler] Failed to add admin: %v", err)
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
		Text:      msg.Build(),
		ParseMode: msg.ParseMode(),
		Keyboard:  convertKeyboard(kb.Build()),
	}, nil
}

// ============================================================
// 管理员反馈面板模块
// ============================================================

// handleFeedbackPanel shows the main admin feedback panel with statistics
func (h *AdminHandler) handleFeedbackPanel(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	if h.issueService == nil {
		return &callback.Response{
			Text: "❌ 反馈服务未启用",
			Edit: true,
		}, nil
	}

	stats := h.issueService.GetStats()

	msg := services.NewMessageBuilder()
	msg.Bold("📊 反馈管理面板").Newline()
	msg.Newline()

	// Statistics section
	msg.Bold("━━━━━━━━━━━━━━━━━━━━").Newline()
	msg.Bold("📈 统计数据").Newline()
	msg.Bold("━━━━━━━━━━━━━━━━━━━━").Newline()
	msg.Textf("总反馈: %d  |  待处理: %d", stats.Total, stats.Open).Newline()
	msg.Textf("处理中: %d   |  已解决: %d", stats.Processing, stats.Fixed).Newline()
	msg.Textf("已关闭: %d   |  本周新增: %d", stats.Closed, stats.ThisWeek).Newline()
	msg.Newline()

	// Type distribution
	msg.Bold("━━━━━━━━━━━━━━━━━━━━").Newline()
	msg.Bold("🏷️ 类型分布").Newline()
	msg.Bold("━━━━━━━━━━━━━━━━━━━━").Newline()

	typeIcons := map[string]string{
		"画质问题": "🎬",
		"音频问题": "🔊",
		"字幕问题": "📝",
		"搜索不到": "🔍",
		"播放问题": "⏯️",
		"其他问题": "❓",
	}

	// Display type distribution
	if len(stats.ByType) > 0 {
		types := []string{"画质问题", "音频问题", "字幕问题", "搜索不到", "播放问题", "其他问题"}
		for i, t := range types {
			if count, ok := stats.ByType[t]; ok {
				icon := typeIcons[t]
				if icon == "" {
					icon = "📌"
				}
				msg.Textf("%s %s: %d", icon, t, count)
				if i%2 == 1 {
					msg.Newline()
				} else {
					msg.Text("   | ")
				}
			}
		}
		msg.Newline()
	} else {
		msg.Text("暂无数据").Newline()
	}

	// Average resolve time
	if stats.AvgResolveTime > 0 {
		hours := int(stats.AvgResolveTime)
		msg.Newline()
		msg.Italic(fmt.Sprintf("⏱️ 平均解决时间: %d 小时", hours)).Newline()
	}

	kb := services.NewKeyboardBuilder()

	// 第一行：主要操作（最常用）
	kb.AddButton("🔵 待处理", "admin_feedback_filter:status:open")
	kb.AddButton("📋 全部反馈", "admin_feedback_list")
	kb.AddButton("🔄 刷新", "admin_feedback")
	kb.NewRow()

	// 第二行：查看筛选（合并）
	kb.AddButton("📊 筛选", "admin_feedback_filter:status:all")

	return &callback.Response{
		Text:      msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
	}, nil
}

// handleFeedbackStats shows detailed statistics (redirects to panel for now)
func (h *AdminHandler) handleFeedbackStats(ctx *callback.Context) (*callback.Response, error) {
	return h.handleFeedbackPanel(ctx)
}

// handleFeedbackList shows all feedback with pagination
func (h *AdminHandler) handleFeedbackList(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	if h.issueService == nil {
		return &callback.Response{
			Text: "❌ 反馈服务未启用",
			Edit: true,
		}, nil
	}

	// Get page from params
	page := 1
	if pageStr := ctx.Callback.Params["page"]; pageStr != "" {
		fmt.Sscanf(pageStr, "%d", &page)
	}

	// Get all issues
	issues := h.issueService.GetAllIssues()
	const perPage = 10

	totalPages := (len(issues) + perPage - 1) / perPage
	if page < 1 {
		page = 1
	}
	if page > totalPages && totalPages > 0 {
		page = totalPages
	}

	startIdx := (page - 1) * perPage
	endIdx := startIdx + perPage
	if endIdx > len(issues) {
		endIdx = len(issues)
	}

	msg := services.NewMessageBuilder()
	msg.Bold("📋 反馈列表").Newline()
	msg.Newline()
	msg.Textf("共 %d 条反馈", len(issues)).Newline()
	if totalPages > 1 {
		msg.Italic(fmt.Sprintf("第 %d/%d 页", page, totalPages)).Newline()
	}
	msg.Newline()

	kb := services.NewKeyboardBuilder()

	if startIdx >= endIdx {
		msg.Text("暂无反馈").Newline()
	} else {
		for i := startIdx; i < endIdx; i++ {
			issue := issues[i]
			statusIcon := getStatusIconFromService(issue.Status)
			priorityIcon := getPriorityIcon(issue.Priority)

			mediaText := ""
			if issue.MediaTitle != "" {
				mediaText = fmt.Sprintf(" - %s", issue.MediaTitle)
			}

			msg.Textf("%d. %s [#%d]%s", i+1, statusIcon, issue.ID, mediaText).Newline()
			msg.Textf("   %s %s", priorityIcon, issue.Title).Newline()
			msg.Textf("   👤 %s | 🕐 %s", issue.UserName,
				issue.CreatedAt.Format("01-02 15:04")).Newline()
			msg.Newline()

			// Detail button
			kb.AddButton(fmt.Sprintf("#%d %s", issue.ID, getStatusTextFromService(issue.Status)),
				fmt.Sprintf("admin_feedback_detail:id:%d", issue.ID))
			kb.NewRow()
		}
	}

	// Pagination buttons
	if totalPages > 1 {
		if page > 1 {
			kb.AddButton("⬅️ 上一页", fmt.Sprintf("admin_feedback_list:page:%d", page-1))
		}
		if page < totalPages {
			kb.AddButton("➡️ 下一页", fmt.Sprintf("admin_feedback_list:page:%d", page+1))
		}
		kb.NewRow()
	}

	kb.AddButton("📊 统计面板", "admin_feedback")
	kb.NewRow()
	kb.AddButton("⬅️ 返回管理员菜单", "admin_menu")

	return &callback.Response{
		Text:      msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
	}, nil
}

// handleFeedbackFilter shows filtered feedback by status
func (h *AdminHandler) handleFeedbackFilter(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	if h.issueService == nil {
		return &callback.Response{
			Text: "❌ 反馈服务未启用",
			Edit: true,
		}, nil
	}

	statusStr := ctx.Callback.Params["status"]
	if statusStr == "" || statusStr == "all" {
		// Show filter menu
		stats := h.issueService.GetStats()

		msg := services.NewMessageBuilder()
		msg.Bold("📊 反馈筛选").Newline()
		msg.Newline()
		msg.Text("请选择要查看的反馈状态：").Newline()
		msg.Newline()
		msg.Textf("🔵 待处理: %d 条", stats.Open).Newline()
		msg.Textf("🔧 处理中: %d 条", stats.Processing).Newline()
		msg.Textf("✅ 已解决: %d 条", stats.Fixed).Newline()
		msg.Textf("🚫 已关闭: %d 条", stats.Closed).Newline()

		kb := services.NewKeyboardBuilder()
		kb.AddButton("🔵 待处理", "admin_feedback_filter:status:open")
		kb.AddButton("🔧 处理中", "admin_feedback_filter:status:processing")
		kb.NewRow()
		kb.AddButton("✅ 已解决", "admin_feedback_filter:status:fixed")
		kb.AddButton("🚫 已关闭", "admin_feedback_filter:status:closed")
		kb.NewRow()
		kb.AddButton("📋 全部反馈", "admin_feedback_list")
		kb.NewRow()
		kb.AddButton("⬅️ 返回", "admin_feedback")

		return &callback.Response{
			Text:      msg.Build(),
			ParseMode: msg.ParseMode(),
			Edit:      true,
			Keyboard:  convertKeyboard(kb.Build()),
		}, nil
	}

	var status services.IssueStatus
	var statusTitle string

	switch statusStr {
	case "open":
		status = services.IssueStatusOpen
		statusTitle = "待处理"
	case "processing":
		status = services.IssueStatusProcessing
		statusTitle = "处理中"
	case "fixed":
		status = services.IssueStatusFixed
		statusTitle = "已解决"
	case "closed":
		status = services.IssueStatusClosed
		statusTitle = "已关闭"
	default:
		return h.handleFeedbackPanel(ctx)
	}

	// Get filtered issues
	issues := h.issueService.GetFilteredIssues([]services.IssueStatus{status}, 50)

	msg := services.NewMessageBuilder()
	msg.Bold(fmt.Sprintf("📋 %s反馈", statusTitle)).Newline()
	msg.Newline()
	msg.Textf("共 %d 条", len(issues)).Newline()
	msg.Newline()

	kb := services.NewKeyboardBuilder()

	if len(issues) == 0 {
		msg.Italic("暂无反馈").Newline()
	} else {
		for i, issue := range issues {
			if i >= 20 { // Limit display
				break
			}
			priorityIcon := getPriorityIcon(issue.Priority)
			mediaText := ""
			if issue.MediaTitle != "" {
				mediaText = fmt.Sprintf(" - %s", issue.MediaTitle)
			}

			msg.Textf("%d. %s [#%d]%s", i+1, getStatusIconFromService(issue.Status), issue.ID, mediaText).Newline()
			msg.Textf("   %s %s", priorityIcon, issue.Title).Newline()
			msg.Textf("   👤 %s", issue.UserName).Newline()
			msg.Newline()

			kb.AddButton(fmt.Sprintf("#%d %s", issue.ID, issue.Title),
				fmt.Sprintf("admin_feedback_detail:id:%d", issue.ID))
			kb.NewRow()
		}
	}

	kb.AddButton("📊 统计面板", "admin_feedback")
	kb.NewRow()
	kb.AddButton("⬅️ 返回管理员菜单", "admin_menu")

	return &callback.Response{
		Text:      msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
	}, nil
}

// handleFeedbackDetail shows feedback detail with action buttons
func (h *AdminHandler) handleFeedbackDetail(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	if h.issueService == nil {
		return &callback.Response{
			Text: "❌ 反馈服务未启用",
			Edit: true,
		}, nil
	}

	issueIDStr := ctx.Callback.Params["id"]
	if issueIDStr == "" {
		return &callback.Response{
			CallbackMsg: "无效的反馈ID",
			ShowAlert:   true,
		}, nil
	}

	issueID, err := strconv.ParseInt(issueIDStr, 10, 64)
	if err != nil {
		return &callback.Response{
			CallbackMsg: "无效的反馈ID",
			ShowAlert:   true,
		}, nil
	}

	issue, exists := h.issueService.GetIssue(issueID)
	if !exists {
		return &callback.Response{
			CallbackMsg: "反馈不存在",
			ShowAlert:   true,
		}, nil
	}

	msg := services.NewMessageBuilder()
	msg.Bold("🐛 反馈详情").Newline()
	msg.Newline()
	msg.Textf("编号: #%d", issue.ID).Newline()
	msg.Textf("状态: %s %s", getStatusIconFromService(issue.Status), getStatusTextFromService(issue.Status)).Newline()
	msg.Textf("优先级: %s", getPriorityText(issue.Priority)).Newline()
	msg.Textf("类型: %s", issue.Title).Newline()
	msg.Textf("用户: %s (%d)", issue.UserName, issue.UserID).Newline()

	if issue.MediaTitle != "" {
		mediaType := "电影"
		if issue.MediaType == "tv" {
			mediaType = "剧集"
		}
		msg.Textf("媒体: %s (%s)", issue.MediaTitle, mediaType).Newline()
	}
	if issue.TmdbID > 0 {
		msg.Textf("TMDB ID: %d", issue.TmdbID).Newline()
	}
	if issue.MediaID != "" {
		msg.Textf("Media ID: %s", issue.MediaID).Newline()
	}

	msg.Newline()
	msg.Bold("📝 问题描述:").Newline()
	msg.Text(issue.Description).Newline()
	msg.Newline()

	// Show satisfaction rating if available
	if issue.Satisfaction > 0 {
		stars := ""
		for i := 0; i < 5; i++ {
			if i < issue.Satisfaction {
				stars += "⭐"
			} else {
				stars += "☆"
			}
		}
		msg.Textf("用户评分: %s (%d/5)", stars, issue.Satisfaction).Newline()
		msg.Newline()
	}

	// Show replies if any
	if len(issue.Replies) > 0 {
		msg.Bold("💬 回复记录:").Newline()
		for _, reply := range issue.Replies {
			replyType := ""
			if reply.Type == "admin" {
				replyType = "[管理员] "
			} else if reply.Type == "user" {
				replyType = "[用户] "
			}
			msg.Textf("  %s%s: %s", replyType, reply.AuthorName, reply.Content).Newline()
		}
		msg.Newline()
	}

	msg.Italic(fmt.Sprintf("🕐 提交: %s", issue.CreatedAt.Format("2006-01-02 15:04"))).Newline()
	if issue.ResolvedAt != nil {
		msg.Italic(fmt.Sprintf("✅ 解决: %s", issue.ResolvedAt.Format("2006-01-02 15:04"))).Newline()
	}

	kb := services.NewKeyboardBuilder()

	// 第一行：主要操作（根据状态显示）
	switch issue.Status {
	case services.IssueStatusOpen, services.IssueStatusReply:
		// 待处理状态：回复 + 处理中
		kb.AddButton("💬 回复", fmt.Sprintf("admin_feedback_reply:id:%d", issue.ID))
		kb.AddButton("🔧 处理中", fmt.Sprintf("admin_issue_processing:id:%d", issue.ID))
		kb.NewRow()
	case services.IssueStatusProcessing:
		// 处理中状态：回复 + 已解决
		kb.AddButton("💬 回复", fmt.Sprintf("admin_feedback_reply:id:%d", issue.ID))
		kb.AddButton("✅ 已解决", fmt.Sprintf("admin_issue_fixed:id:%d", issue.ID))
		kb.NewRow()
	case services.IssueStatusFixed:
		// 已解决状态：可以关闭
		kb.AddButton("🚫 关闭", fmt.Sprintf("admin_issue_close:id:%d", issue.ID))
		kb.NewRow()
	}

	// 第二行：更多操作（优先级 + 关闭，已关闭除外）
	if issue.Status != services.IssueStatusClosed && issue.Status != services.IssueStatusFixed {
		kb.AddButton("⚙️ 优先级", fmt.Sprintf("admin_feedback_priority_menu:id:%d", issue.ID))
		kb.AddButton("🚫 关闭", fmt.Sprintf("admin_issue_close:id:%d", issue.ID))
		kb.NewRow()
	}

	// 最后一行：导航
	kb.AddButton("⬅️ 返回", "admin_feedback_list")

	return &callback.Response{
		Text:      msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
	}, nil
}

// handleFeedbackReply handles admin reply to feedback
func (h *AdminHandler) handleFeedbackReply(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	issueIDStr := ctx.Callback.Params["id"]
	if issueIDStr == "" {
		return &callback.Response{
			CallbackMsg: "无效的反馈ID",
			ShowAlert:   true,
		}, nil
	}

	issueID, err := strconv.ParseInt(issueIDStr, 10, 64)
	if err != nil {
		return &callback.Response{
			CallbackMsg: "无效的反馈ID",
			ShowAlert:   true,
		}, nil
	}

	// Store pending reply in session
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	sess.Set("pending_feedback_reply", issueID)

	// Get issue details
	issue, exists := h.issueService.GetIssue(issueID)
	if !exists {
		return &callback.Response{
			CallbackMsg: "反馈不存在",
			ShowAlert:   true,
		}, nil
	}

	msg := services.NewMessageBuilder()
	msg.Bold("💬 回复反馈").Newline()
	msg.Newline()
	msg.Textf("问题编号: #%d", issueID).Newline()
	msg.Textf("问题类型: %s", issue.Title).Newline()
	msg.Newline()
	msg.Bold("请选择快捷回复或直接输入回复内容").Newline()
	msg.Newline()

	kb := services.NewKeyboardBuilder()

	// Add reply template buttons - 两列显示
	templates := services.GetReplyTemplates()
	for i, tmpl := range templates {
		callbackData := fmt.Sprintf("admin_feedback_template:id:%d:template:%d", issueID, i)
		kb.AddButton(tmpl.Name, callbackData)
		if i%2 == 1 {
			kb.NewRow()
		}
	}
	if len(templates)%2 != 0 {
		kb.NewRow()
	}

	kb.AddButton("❌ 取消", "admin_feedback")

	return &callback.Response{
		Text:      msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
	}, nil
}

// handleFeedbackPriority handles priority adjustment
func (h *AdminHandler) handleFeedbackPriority(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	issueIDStr := ctx.Callback.Params["id"]
	priorityStr := ctx.Callback.Params["priority"]

	if issueIDStr == "" || priorityStr == "" {
		return &callback.Response{
			CallbackMsg: "参数错误",
			ShowAlert:   true,
		}, nil
	}

	issueID, err := strconv.ParseInt(issueIDStr, 10, 64)
	if err != nil {
		return &callback.Response{
			CallbackMsg: "无效的反馈ID",
			ShowAlert:   true,
		}, nil
	}

	var priority services.IssuePriority
	switch priorityStr {
	case "urgent":
		priority = services.PriorityUrgent
	case "high":
		priority = services.PriorityHigh
	case "medium":
		priority = services.PriorityMedium
	case "low":
		priority = services.PriorityLow
	default:
		return &callback.Response{
			CallbackMsg: "无效的优先级",
			ShowAlert:   true,
		}, nil
	}

	if err := h.issueService.UpdatePriority(issueID, priority); err != nil {
		logger.Info("[AdminHandler] Failed to update priority: %v", err)
		return &callback.Response{
			CallbackMsg: "更新失败",
			ShowAlert:   true,
		}, nil
	}

	return h.handleFeedbackDetail(ctx)
}

// handleFeedbackTemplate handles quick reply template selection
func (h *AdminHandler) handleFeedbackTemplate(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	issueIDStr := ctx.Callback.Params["id"]
	templateIdxStr := ctx.Callback.Params["template"]

	if issueIDStr == "" || templateIdxStr == "" {
		return &callback.Response{
			CallbackMsg: "参数错误",
			ShowAlert:   true,
		}, nil
	}

	issueID, err := strconv.ParseInt(issueIDStr, 10, 64)
	if err != nil {
		return &callback.Response{
			CallbackMsg: "无效的反馈ID",
			ShowAlert:   true,
		}, nil
	}

	templateIdx := 0
	fmt.Sscanf(templateIdxStr, "%d", &templateIdx)

	templates := services.GetReplyTemplates()
	if templateIdx < 0 || templateIdx >= len(templates) {
		return &callback.Response{
			CallbackMsg: "无效的模板",
			ShowAlert:   true,
		}, nil
	}

	template := templates[templateIdx]

	// Add the reply
	issue, exists := h.issueService.GetIssue(issueID)
	if !exists {
		return &callback.Response{
			CallbackMsg: "反馈不存在",
			ShowAlert:   true,
		}, nil
	}

	// Get admin name
	adminName := "管理员"
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	if name, ok := sess.GetString("name"); ok && name != "" {
		adminName = name
	}

	_, err = h.issueService.AddReply(issueID, ctx.UserID, adminName, template.Content, "admin")
	if err != nil {
		logger.Info("[AdminHandler] Failed to add reply: %v", err)
		return &callback.Response{
			CallbackMsg: "回复失败",
			ShowAlert:   true,
		}, nil
	}

	// Notify the user about the reply
	if issue.UserID != ctx.UserID {
		notifyMsg := services.NewMessageBuilder()
		notifyMsg.Bold("💬 管理员回复了您的反馈").Newline()
		notifyMsg.Newline()
		notifyMsg.Textf("问题编号: #%d", issue.ID).Newline()
		notifyMsg.Textf("问题类型: %s", issue.Title).Newline()
		notifyMsg.Newline()
		notifyMsg.Bold("回复内容:").Newline()
		notifyMsg.Text(template.Content).Newline()
		notifyMsg.Newline()
		notifyMsg.Italic("💬 您可以继续回复此消息进行追问").Newline()

		// Set session for user follow-up
		userSess := h.sessMgr.GetOrCreate(issue.UserID)
		userSess.Set("feedback_conversation_issue_id", float64(issueID))

		kb := services.NewKeyboardBuilder()
		kb.AddButton("⬅️ 返回主菜单", "start")

		h.telegram.SendMessage(issue.UserID, notifyMsg.Build(), "HTML", kb.Build())
	}

	return h.handleFeedbackDetail(ctx)
}

// Helper functions for status and priority display

func getStatusIconFromService(status services.IssueStatus) string {
	switch status {
	case services.IssueStatusOpen:
		return "🔵"
	case services.IssueStatusReply:
		return "💬"
	case services.IssueStatusProcessing:
		return "🔧"
	case services.IssueStatusFixed:
		return "✅"
	case services.IssueStatusClosed:
		return "🚫"
	default:
		return "⚪"
	}
}

func getStatusTextFromService(status services.IssueStatus) string {
	switch status {
	case services.IssueStatusOpen:
		return "待处理"
	case services.IssueStatusReply:
		return "已回复"
	case services.IssueStatusProcessing:
		return "处理中"
	case services.IssueStatusFixed:
		return "已解决"
	case services.IssueStatusClosed:
		return "已关闭"
	default:
		return "未知"
	}
}

func getPriorityIcon(priority services.IssuePriority) string {
	switch priority {
	case services.PriorityUrgent:
		return "🔴"
	case services.PriorityHigh:
		return "🟠"
	case services.PriorityMedium:
		return "🟡"
	case services.PriorityLow:
		return "🟢"
	default:
		return "⚪"
	}
}

func getPriorityText(priority services.IssuePriority) string {
	switch priority {
	case services.PriorityUrgent:
		return "🔴 紧急"
	case services.PriorityHigh:
		return "🟠 高"
	case services.PriorityMedium:
		return "🟡 中"
	case services.PriorityLow:
		return "🟢 低"
	default:
		return "⚪ 未知"
	}
}

// handleFeedbackPriorityMenu shows priority selection menu
func (h *AdminHandler) handleFeedbackPriorityMenu(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "你不是管理员",
			ShowAlert:   true,
		}, nil
	}

	issueIDStr := ctx.Callback.Params["id"]
	if issueIDStr == "" {
		return &callback.Response{
			CallbackMsg: "无效的反馈ID",
			ShowAlert:   true,
		}, nil
	}

	issueID, err := strconv.ParseInt(issueIDStr, 10, 64)
	if err != nil {
		return &callback.Response{
			CallbackMsg: "无效的反馈ID",
			ShowAlert:   true,
		}, nil
	}

	// Get current priority
	issue, exists := h.issueService.GetIssue(issueID)
	if !exists {
		return &callback.Response{
			CallbackMsg: "反馈不存在",
			ShowAlert:   true,
		}, nil
	}

	msg := services.NewMessageBuilder()
	msg.Bold("⚙️ 设置优先级").Newline()
	msg.Newline()
	msg.Textf("问题编号: #%d", issueID).Newline()
	msg.Textf("当前优先级: %s", getPriorityText(issue.Priority)).Newline()
	msg.Newline()
	msg.Text("请选择新的优先级：").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🔴 紧急", fmt.Sprintf("admin_feedback_priority:id:%d:priority:urgent", issueID))
	kb.AddButton("🟠 高", fmt.Sprintf("admin_feedback_priority:id:%d:priority:high", issueID))
	kb.NewRow()
	kb.AddButton("🟡 中", fmt.Sprintf("admin_feedback_priority:id:%d:priority:medium", issueID))
	kb.AddButton("🟢 低", fmt.Sprintf("admin_feedback_priority:id:%d:priority:low", issueID))
	kb.NewRow()
	kb.AddButton("⬅️ 取消", fmt.Sprintf("admin_feedback_detail:id:%d", issueID))

	return &callback.Response{
		Text:      msg.Build(),
		ParseMode: msg.ParseMode(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
	}, nil
}
