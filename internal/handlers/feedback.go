package handlers

import (
	"fmt"
	"log"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
)

// FeedbackHandler handles user feedback callbacks
type FeedbackHandler struct {
	sessMgr      *session.Manager
	telegram     *services.TelegramClient
	adminService *services.AdminService
	issueService *services.IssueService
}

// NewFeedbackHandler creates a new feedback handler
func NewFeedbackHandler(
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	adminService *services.AdminService,
) *FeedbackHandler {
	return &FeedbackHandler{
		sessMgr:      sessMgr,
		telegram:     telegram,
		adminService: adminService,
	}
}

// SetIssueService sets the issue service
func (h *FeedbackHandler) SetIssueService(issueSvc *services.IssueService) {
	h.issueService = issueSvc
}

// Handle handles feedback callbacks
func (h *FeedbackHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	log.Printf("[FeedbackHandler] Handle called: action=%s, params=%+v, h=%v, h.sessMgr=%v, h.telegram=%v",
		ctx.Callback.Action, ctx.Callback.Params, h != nil, h.sessMgr != nil, h.telegram != nil)

	// Check if this is the "my_feedback" menu button - show list directly
	if ctx.Callback.Action == "my_feedback" {
		return h.handleViewList(ctx)
	}

	// Check if viewing feedback list (feedback:view)
	if _, hasView := ctx.Callback.Params["view"]; hasView {
		return h.handleViewList(ctx)
	}

	// Check if viewing feedback detail
	if issueIDStr, hasDetailID := ctx.Callback.Params["detail_id"]; hasDetailID {
		return h.handleViewDetail(ctx, issueIDStr)
	}

	// Check if this is a type selection
	// When user clicks an issue type button, callback is like: feedback:issue_type:quality:id:xxx
	issueTypeParam, hasIssueType := ctx.Callback.Params["issue_type"]

	// Check if there's also an "id" param - this indicates type selection
	_, hasID := ctx.Callback.Params["id"]

	if hasIssueType && hasID {
		switch issueTypeParam {
		case "quality", "audio", "subtitle", "not_found", "playback", "other":
			return h.handleTypeSelect(ctx)
		}
	}

	// Check if this is "feedback" action without params - could be "feedback:view" case
	// When params are empty but action is feedback, treat as view list
	if ctx.Callback.Action == "feedback" && len(ctx.Callback.Params) == 0 {
		return h.handleViewList(ctx)
	}

	// Otherwise, this is the initial feedback button click
	return h.handleStart(ctx)
}

// handleStart starts the feedback process
func (h *FeedbackHandler) handleStart(ctx *callback.Context) (*callback.Response, error) {
	// Get media info from params
	tmdbID := ctx.Callback.Params["id"]
	mediaType := ctx.Callback.Params["type"]
	mediaTitle := ctx.Callback.Params["title"]

	if tmdbID == "" {
		return &callback.Response{
			CallbackMsg: "参数错误",
			ShowAlert:   true,
		}, nil
	}

	// Store feedback context in session
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	sess.Set("feedback_tmdb_id", tmdbID)
	sess.Set("feedback_media_type", mediaType)
	sess.Set("feedback_media_title", mediaTitle)
	sess.Set("feedback_step", "type")

	// Build type selection message
	msg := services.NewMessageBuilder()
	msg.Bold("🐛 问题反馈").Newline()
	msg.Newline()
	msg.Text("请选择问题类型：").Newline()
	msg.Newline()
	msg.Italic("💡 选择类型后，请详细描述问题").Newline()

	// Build keyboard with issue types
	kb := services.NewKeyboardBuilder()

	// Issue type buttons (2 columns)
	types := []struct {
		label string
		value string
	}{
		{"🎬 画质问题", "quality"},
		{"🔊 音频问题", "audio"},
		{"📝 字幕问题", "subtitle"},
		{"🔍 搜索不到", "not_found"},
		{"⏯️ 播放问题", "playback"},
		{"❓ 其他问题", "other"},
	}

	for i, t := range types {
		// Use "issue_type" parameter to avoid conflict with media "type" parameter
		callbackData := fmt.Sprintf("feedback:issue_type:%s:id:%s:media_type:%s", t.value, tmdbID, mediaType)
		kb.AddButton(t.label, callbackData)
		if i%2 == 1 {
			kb.NewRow()
		}
	}
	if len(types)%2 != 0 {
		kb.NewRow()
	}

	kb.AddButton("❌ 取消", "cancel")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     false,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleTypeSelect handles issue type selection
func (h *FeedbackHandler) handleTypeSelect(ctx *callback.Context) (*callback.Response, error) {
	issueType := ctx.Callback.Params["issue_type"]
	tmdbID := ctx.Callback.Params["id"]
	mediaType := ctx.Callback.Params["media_type"]

	log.Printf("[FeedbackHandler] handleTypeSelect: issueType=%s, tmdbID=%s, mediaType=%s", issueType, tmdbID, mediaType)

	if issueType == "" || tmdbID == "" {
		return &callback.Response{
			CallbackMsg: "参数错误",
			ShowAlert:   true,
		}, nil
	}

	// Store type in session
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	sess.Set("feedback_issue_type", issueType)
	sess.Set("feedback_step", "description")
	sess.Set("feedback_tmdb_id", tmdbID)
	sess.Set("feedback_media_type", mediaType)  // Store media type too

	// Get type label
	typeLabels := map[string]string{
		"quality":   "画质问题",
		"audio":     "音频问题",
		"subtitle":  "字幕问题",
		"not_found": "搜索不到",
		"playback":  "播放问题",
		"other":     "其他问题",
	}
	typeLabel := typeLabels[issueType]
	if typeLabel == "" {
		typeLabel = "问题反馈"
	}

	// Build message asking for description
	msg := services.NewMessageBuilder()
	msg.Bold(fmt.Sprintf("🐛 %s", typeLabel)).Newline()
	msg.Newline()
	msg.Italic("💬 请描述您遇到的问题").Newline()
	msg.Newline()
	msg.Text("您可以直接发送问题描述，包含：").Newline()
	msg.Text("• 问题的具体表现").Newline()
	msg.Text("• 发生的时间或场景").Newline()
	msg.Text("• 任何有助于解决问题的信息").Newline()
	msg.Newline()
	msg.Italic("⏰ 请在 5 分钟内完成描述").Newline()

	// Update keyboard to show cancel button
	kb := services.NewKeyboardBuilder()
	kb.AddButton("❌ 取消反馈", "cancel")

	// Send message (don't edit, send new message for user to reply)
	h.telegram.SendMessage(ctx.ChatID, msg.Build(), "HTML", kb.Build())

	// Update original message to show waiting state
	return &callback.Response{
		Text:        "等待您描述问题...",
		CallbackMsg: "请发送问题描述",
		Edit:        true,
		Keyboard:    &callback.Keyboard{},
	}, nil
}

// HandleFeedbackText handles user's feedback description text
func (h *FeedbackHandler) HandleFeedbackText(userID int64, chatID int64, text string) error {
	sess := h.sessMgr.GetOrCreate(userID)

	// Check if user is in feedback process
	stepVal, _ := sess.Get("feedback_step")
	step, _ := stepVal.(string)
	if step != "description" {
		return fmt.Errorf("not in feedback process")
	}

	// Get feedback context with type assertions
	tmdbIDVal, ok := sess.Get("feedback_tmdb_id")
	if !ok {
		return fmt.Errorf("missing feedback context: tmdb_id")
	}
	tmdbID, ok := tmdbIDVal.(string)
	if !ok || tmdbID == "" {
		return fmt.Errorf("invalid feedback context: tmdb_id")
	}

	mediaTypeVal, _ := sess.Get("feedback_media_type")
	mediaType, _ := mediaTypeVal.(string)

	mediaTitleVal, _ := sess.Get("feedback_media_title")
	mediaTitle, _ := mediaTitleVal.(string)

	issueTypeVal, _ := sess.Get("feedback_issue_type")
	issueType, _ := issueTypeVal.(string)

	// Clear feedback session
	sess.Delete("feedback_step")
	sess.Delete("feedback_tmdb_id")
	sess.Delete("feedback_media_type")
	sess.Delete("feedback_media_title")
	sess.Delete("feedback_issue_type")

	// Create issue
	if h.issueService == nil {
		return fmt.Errorf("issue service not available")
	}

	// Get type label
	typeLabels := map[string]string{
		"quality":   "画质问题",
		"audio":     "音频问题",
		"subtitle":  "字幕问题",
		"not_found": "搜索不到",
		"playback":  "播放问题",
		"other":     "其他问题",
	}
	typeLabel := typeLabels[issueType]
	if typeLabel == "" {
		typeLabel = "问题反馈"
	}

	// Get user name
	userName := "用户"
	if nameVal, ok := sess.Get("name"); ok && nameVal != "" {
		if name, ok := nameVal.(string); ok {
			userName = name
		}
	}

	// Create issue
	issue, err := h.issueService.CreateIssue(
		userID,
		userName,
		typeLabel,
		text,
		mediaType,
		tmdbID,
		mediaTitle,
	)
	if err != nil {
		log.Printf("[FeedbackHandler] Failed to create issue: %v", err)
		h.telegram.SendMessage(chatID, "❌ 提交失败，请稍后重试", "", nil)
		return err
	}

	// Confirm to user
	confirmMsg := services.NewMessageBuilder()
	confirmMsg.Bold("✅ 反馈已提交").Newline()
	confirmMsg.Newline()
	confirmMsg.Textf("问题编号: #%d", issue.ID).Newline()
	confirmMsg.Textf("问题类型: %s", typeLabel).Newline()
	confirmMsg.Newline()
	confirmMsg.Italic("💡 管理员已收到通知，会尽快处理").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("⬅️ 返回主菜单", "start")

	h.telegram.SendMessage(chatID, confirmMsg.Build(), "HTML", kb.Build())

	// Notify admins
	go h.notifyAdmins(issue, typeLabel)

	return nil
}

// notifyAdmins sends notification to admins about new issue
func (h *FeedbackHandler) notifyAdmins(issue *services.Issue, typeLabel string) {
	if h.adminService == nil {
		return
	}

	adminIDs := h.adminService.GetAdminIDs()
	if len(adminIDs) == 0 {
		return
	}

	// Build message
	msg := services.NewMessageBuilder()
	msg.Bold("🐛 新问题反馈").Newline()
	msg.Newline()
	msg.Textf("📋 问题编号: #%d", issue.ID).Newline()
	msg.Textf("👤 用户: %s", issue.UserName).Newline()
	msg.Textf("🏷️ 类型: %s", typeLabel).Newline()

	if issue.MediaTitle != "" {
		mediaType := "电影"
		if issue.MediaType == "tv" {
			mediaType = "剧集"
		}
		msg.Textf("🎬 媒体: %s (%s)", issue.MediaTitle, mediaType).Newline()
	}

	msg.Newline()
	msg.Bold("📝 问题描述:").Newline()
	msg.Text(issue.Description).Newline()
	msg.Newline()
	msg.Italic(fmt.Sprintf("🕐 %s", issue.CreatedAt.Format("2006-01-02 15:04"))).Newline()

	// Build keyboard for admin actions
	kb := services.NewKeyboardBuilder()
	kb.AddButton("💬 回复", fmt.Sprintf("admin_issue_reply:id:%d", issue.ID))
	kb.AddButton("🔧 处理中", fmt.Sprintf("admin_issue_processing:id:%d", issue.ID))
	kb.NewRow()
	kb.AddButton("✅ 已解决", fmt.Sprintf("admin_issue_fixed:id:%d", issue.ID))
	kb.AddButton("🚫 关闭", fmt.Sprintf("admin_issue_close:id:%d", issue.ID))

	message := msg.Build()

	// Send to all admins with error handling
	for _, adminID := range adminIDs {
		if _, err := h.telegram.SendMessage(adminID, message, "HTML", kb.Build()); err != nil {
			log.Printf("[FeedbackHandler] Failed to notify admin %d: %v", adminID, err)
		}
	}
}

// IsInFeedbackProcess checks if user is in feedback process
func (h *FeedbackHandler) IsInFeedbackProcess(userID int64) bool {
	sess := h.sessMgr.GetOrCreate(userID)
	step, ok := sess.Get("feedback_step")
	log.Printf("[FeedbackHandler] IsInFeedbackProcess for user %d: step=%v, ok=%v", userID, step, ok)
	return ok && step == "description"
}

// handleViewList handles viewing user's feedback list
func (h *FeedbackHandler) handleViewList(ctx *callback.Context) (*callback.Response, error) {
	if h.issueService == nil {
		return &callback.Response{
			CallbackMsg: "功能暂不可用",
			ShowAlert:   true,
		}, nil
	}

	issues := h.issueService.GetUserIssues(ctx.UserID)

	msg := services.NewMessageBuilder()
	msg.Bold("🐛 我的反馈").Newline()
	msg.Newline()

	if len(issues) == 0 {
		msg.Text("暂无反馈记录").Newline()
		msg.Newline()
		msg.Italic("💡 在影片详情页点击「🐛 反馈」按钮提交问题")

		kb := services.NewKeyboardBuilder()
		kb.AddButton("⬅️ 返回主菜单", "start")

		return &callback.Response{
			Text:     msg.Build(),
			Edit:     true,
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	// Sort by created date (newest first)
	// Use simple sort
	for i := 0; i < len(issues); i++ {
		for j := i + 1; j < len(issues); j++ {
			if issues[i].CreatedAt.Before(issues[j].CreatedAt) {
				issues[i], issues[j] = issues[j], issues[i]
			}
		}
	}

	msg.Textf("共 %d 条反馈记录", len(issues)).Newline()
	msg.Newline()

	kb := services.NewKeyboardBuilder()

	// Show up to 10 recent issues
	displayCount := 10
	if len(issues) < displayCount {
		displayCount = len(issues)
	}

	for i := 0; i < displayCount; i++ {
		issue := issues[i]
		statusIcon := getStatusIcon(issue.Status)
		mediaText := ""
		if issue.MediaTitle != "" {
			mediaType := "电影"
			if issue.MediaType == "tv" {
				mediaType = "剧集"
			}
			mediaText = fmt.Sprintf(" - %s(%s)", issue.MediaTitle, mediaType)
		}
		msg.Textf("%d. %s #%d%s", i+1, statusIcon, issue.ID, mediaText).Newline()
		msg.Textf("   %s", issue.Title).Newline()
		msg.Newline()

		// Add button for detail view
		buttonText := fmt.Sprintf("#%d %s", issue.ID, getStatusText(issue.Status))
		kb.AddButton(buttonText, fmt.Sprintf("feedback:detail_id:%d", issue.ID))
		if (i+1)%2 == 0 {
			kb.NewRow()
		}
	}

	if len(issues) > displayCount {
		kb.NewRow()
		msg.Textf("... 还有 %d 条记录", len(issues)-displayCount).Newline()
	}

	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleViewDetail handles viewing feedback detail
func (h *FeedbackHandler) handleViewDetail(ctx *callback.Context, issueIDStr string) (*callback.Response, error) {
	if h.issueService == nil {
		return &callback.Response{
			CallbackMsg: "功能暂不可用",
			ShowAlert:   true,
		}, nil
	}

	// Parse issue ID
	var issueID int64
	fmt.Sscanf(issueIDStr, "%d", &issueID)

	issue, exists := h.issueService.GetIssue(issueID)
	if !exists || issue.UserID != ctx.UserID {
		return &callback.Response{
			CallbackMsg: "反馈不存在",
			ShowAlert:   true,
		}, nil
	}

	msg := services.NewMessageBuilder()
	msg.Bold("🐛 反馈详情").Newline()
	msg.Newline()
	msg.Textf("编号: #%d", issue.ID).Newline()
	msg.Textf("状态: %s %s", getStatusIcon(issue.Status), getStatusText(issue.Status)).Newline()
	msg.Textf("类型: %s", issue.Title).Newline()

	if issue.MediaTitle != "" {
		mediaType := "电影"
		if issue.MediaType == "tv" {
			mediaType = "剧集"
		}
		msg.Textf("媒体: %s (%s)", issue.MediaTitle, mediaType).Newline()
	}

	msg.Newline()
	msg.Bold("📝 问题描述:").Newline()
	msg.Text(issue.Description).Newline()
	msg.Newline()

	// Show replies if any
	if len(issue.Replies) > 0 {
		msg.Bold("💬 管理员回复:").Newline()
		for _, reply := range issue.Replies {
			msg.Textf("  %s: %s", reply.AuthorName, reply.Content).Newline()
		}
		msg.Newline()
	}

	msg.Italic(fmt.Sprintf("🕐 提交时间: %s", issue.CreatedAt.Format("2006-01-02 15:04"))).Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("⬅️ 返回列表", "feedback:view")
	kb.NewRow()
	kb.AddButton("🏠 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// getStatusIcon returns status icon
func getStatusIcon(status services.IssueStatus) string {
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

// getStatusText returns status text
func getStatusText(status services.IssueStatus) string {
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
