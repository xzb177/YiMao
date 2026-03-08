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

	// Check if user is closing feedback
	if _, hasClose := ctx.Callback.Params["close"]; hasClose {
		return h.handleCloseByUser(ctx)
	}

	// Check if user is rating satisfaction
	if ratingStr, hasRating := ctx.Callback.Params["rate"]; hasRating {
		return h.handleRateSatisfaction(ctx, ratingStr)
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
	sess.Set("feedback_media_type", mediaType)

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
	msg.Italic("💡 支持发送图片截图").Newline()
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
	return h.HandleFeedbackWithPhoto(userID, chatID, text, "")
}

// HandleFeedbackWithPhoto handles user's feedback with photo attachment
func (h *FeedbackHandler) HandleFeedbackWithPhoto(userID int64, chatID int64, text, photoFileID string) error {
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

	// Create issue with photo if provided
	var issue *services.Issue
	var err error
	if photoFileID != "" {
		issue, err = h.issueService.CreateIssueWithPhoto(
			userID,
			userName,
			typeLabel,
			text,
			mediaType,
			tmdbID,
			mediaTitle,
			photoFileID,
		)
	} else {
		issue, err = h.issueService.CreateIssue(
			userID,
			userName,
			typeLabel,
			text,
			mediaType,
			tmdbID,
			mediaTitle,
		)
	}
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
	keyboard := kb.Build()

	// Send to all admins with error handling
	for _, adminID := range adminIDs {
		// If issue has photo, send photo with caption
		if issue.PhotoFileID != "" {
			// Send photo with the message as caption
			// Use SendPhotoByFileID to send using Telegram's file_id
			if _, err := h.telegram.SendPhotoByFileID(adminID, issue.PhotoFileID, message, keyboard); err != nil {
				log.Printf("[FeedbackHandler] Failed to send photo to admin %d: %v", adminID, err)
				// Fallback to text message
				if _, err2 := h.telegram.SendMessage(adminID, message, "HTML", keyboard); err2 != nil {
					log.Printf("[FeedbackHandler] Failed to send fallback message to admin %d: %v", adminID, err2)
				}
			}
		} else {
			if _, err := h.telegram.SendMessage(adminID, message, "HTML", keyboard); err != nil {
				log.Printf("[FeedbackHandler] Failed to notify admin %d: %v", adminID, err)
			}
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

	// Set session for follow-up
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	sess.Set("feedback_conversation_issue_id", float64(issueID))

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
		msg.Bold("💬 回复记录:").Newline()
		for _, reply := range issue.Replies {
			replyType := ""
			if reply.Type == "admin" {
				replyType = "[管理员] "
			} else if reply.Type == "user" {
				replyType = "[您] "
			}
			msg.Textf("  %s%s: %s", replyType, reply.AuthorName, reply.Content).Newline()
		}
		msg.Newline()
	}

	msg.Italic(fmt.Sprintf("🕐 提交时间: %s", issue.CreatedAt.Format("2006-01-02 15:04"))).Newline()

	kb := services.NewKeyboardBuilder()

	// 根据状态显示不同操作
	if issue.Status == services.IssueStatusFixed {
		// 已解决状态 - 显示评分（如果未评分）
		if issue.Satisfaction == 0 {
			msg.Newline()
			msg.Bold("⭐ 请为本次处理评分").Newline()
			// 评分按钮：一行两个，更紧凑
			kb.AddButton("⭐⭐⭐⭐⭐", fmt.Sprintf("feedback:rate:5:id:%d", issue.ID))
			kb.AddButton("⭐⭐⭐", fmt.Sprintf("feedback:rate:3:id:%d", issue.ID))
			kb.NewRow()
			kb.AddButton("⭐⭐", fmt.Sprintf("feedback:rate:2:id:%d", issue.ID))
			kb.AddButton("⭐", fmt.Sprintf("feedback:rate:1:id:%d", issue.ID))
			kb.NewRow()
			kb.AddButton("🚫 关闭反馈", fmt.Sprintf("feedback:close:%d", issue.ID))
		} else {
			// 已评分 - 显示评分和关闭按钮
			stars := ""
			for i := 0; i < 5; i++ {
				if i < issue.Satisfaction {
					stars += "⭐"
				} else {
					stars += "☆"
				}
			}
			msg.Newline()
			msg.Textf("您的评分: %s (%d/5)", stars, issue.Satisfaction).Newline()
			kb.AddButton("🚫 关闭反馈", fmt.Sprintf("feedback:close:%d", issue.ID))
		}
	} else if issue.Status != services.IssueStatusClosed {
		// 未关闭状态 - 显示关闭按钮
		kb.AddButton("🚫 关闭反馈", fmt.Sprintf("feedback:close:%d", issue.ID))
	}

	// 最后一行：返回按钮
	if len(issue.Replies) > 0 && issue.Status != services.IssueStatusClosed {
		kb.NewRow()
		msg.Italic("💬 您可以回复此消息进行追问").Newline()
	}
	kb.AddButton("⬅️ 返回列表", "feedback:view")
	if len(issue.Replies) > 0 && issue.Status != services.IssueStatusClosed {
		msg.Newline()
		msg.Italic("💬 您可以直接回复此消息进行追问").Newline()
	}

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

// ============================================================
// 用户交互增强模块
// ============================================================

// handleUserFollowUp handles user follow-up messages to an existing feedback
func (h *FeedbackHandler) HandleUserFollowUp(userID int64, chatID int64, text string) error {
	sess := h.sessMgr.GetOrCreate(userID)

	// Check if user has an active feedback conversation
	issueIDVal, exists := sess.Get("feedback_conversation_issue_id")
	if !exists {
		return nil // Not in a follow-up conversation
	}

	var issueID int64
	switch v := issueIDVal.(type) {
	case float64:
		issueID = int64(v)
	case int64:
		issueID = v
	case string:
		fmt.Sscanf(v, "%d", &issueID)
	default:
		return fmt.Errorf("invalid issue ID type")
	}

	if issueID == 0 {
		return nil
	}

	// Get issue to verify ownership
	issue, exists := h.issueService.GetIssue(issueID)
	if !exists || issue.UserID != userID {
		// Clear the invalid session
		sess.Delete("feedback_conversation_issue_id")
		return nil
	}

	// Add user follow-up as a reply
	userName := "用户"
	if nameVal, ok := sess.Get("name"); ok && nameVal != "" {
		if name, ok := nameVal.(string); ok {
			userName = name
		}
	}

	_, err := h.issueService.AddReply(issueID, userID, userName, text, "user")
	if err != nil {
		log.Printf("[FeedbackHandler] Failed to add follow-up: %v", err)
		return err
	}

	// Confirm to user
	confirmMsg := services.NewMessageBuilder()
	confirmMsg.Bold("💬 追问已发送").Newline()
	confirmMsg.Newline()
	confirmMsg.Textf("问题编号: #%d", issueID).Newline()
	confirmMsg.Italic("管理员已收到您的追问，会尽快回复").Newline()

	h.telegram.SendMessage(chatID, confirmMsg.Build(), "HTML", nil)

	// Notify admins
	go h.notifyAdminFollowUp(issueID, text, userName)

	return nil
}

// notifyAdminFollowUp sends notification to admins about user follow-up
func (h *FeedbackHandler) notifyAdminFollowUp(issueID int64, text, userName string) {
	if h.adminService == nil {
		return
	}

	issue, exists := h.issueService.GetIssue(issueID)
	if !exists {
		return
	}

	adminIDs := h.adminService.GetAdminIDs()
	if len(adminIDs) == 0 {
		return
	}

	msg := services.NewMessageBuilder()
	msg.Bold("💬 用户追问").Newline()
	msg.Newline()
	msg.Textf("🐛 问题 #%d", issue.ID).Newline()
	msg.Textf("👤 用户: %s", userName).Newline()
	msg.Newline()
	msg.Bold("📝 追问内容:").Newline()
	msg.Text(text).Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("💬 回复", fmt.Sprintf("admin_feedback_reply:id:%d", issue.ID))
	kb.AddButton("✅ 已解决", fmt.Sprintf("admin_issue_fixed:id:%d", issue.ID))

	message := msg.Build()
	keyboard := kb.Build()

	// Send to all admins
	for _, adminID := range adminIDs {
		if _, err := h.telegram.SendMessage(adminID, message, "HTML", keyboard); err != nil {
			log.Printf("[FeedbackHandler] Failed to notify admin %d: %v", adminID, err)
		}
	}
}

// handleCloseByUser handles user closing their own feedback
func (h *FeedbackHandler) handleCloseByUser(ctx *callback.Context) (*callback.Response, error) {
	if h.issueService == nil {
		return &callback.Response{
			CallbackMsg: "功能暂不可用",
			ShowAlert:   true,
		}, nil
	}

	issueIDStr := ctx.Callback.Params["close"]
	if issueIDStr == "" {
		return &callback.Response{
			CallbackMsg: "无效的反馈ID",
			ShowAlert:   true,
		}, nil
	}

	var issueID int64
	fmt.Sscanf(issueIDStr, "%d", &issueID)

	// Close the issue (will verify ownership)
	err := h.issueService.CloseByUser(issueID, ctx.UserID)
	if err != nil {
		return &callback.Response{
			CallbackMsg: err.Error(),
			ShowAlert:   true,
		}, nil
	}

	// Clear follow-up session if exists
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	sess.Delete("feedback_conversation_issue_id")

	return h.handleViewList(ctx)
}

// handleRateSatisfaction handles user satisfaction rating
func (h *FeedbackHandler) handleRateSatisfaction(ctx *callback.Context, ratingStr string) (*callback.Response, error) {
	if h.issueService == nil {
		return &callback.Response{
			CallbackMsg: "功能暂不可用",
			ShowAlert:   true,
		}, nil
	}

	rating := 0
	fmt.Sscanf(ratingStr, "%d", &rating)

	if rating < 1 || rating > 5 {
		return &callback.Response{
			CallbackMsg: "无效的评分",
			ShowAlert:   true,
		}, nil
	}

	// Get issue ID from params
	issueIDStr := ctx.Callback.Params["id"]
	if issueIDStr == "" {
		return &callback.Response{
			CallbackMsg: "无效的反馈ID",
			ShowAlert:   true,
		}, nil
	}

	var issueID int64
	fmt.Sscanf(issueIDStr, "%d", &issueID)

	// Verify ownership and get issue
	issue, exists := h.issueService.GetIssue(issueID)
	if !exists || issue.UserID != ctx.UserID {
		return &callback.Response{
			CallbackMsg: "反馈不存在",
			ShowAlert:   true,
		}, nil
	}

	// Save rating
	if err := h.issueService.RateSatisfaction(issueID, rating); err != nil {
		log.Printf("[FeedbackHandler] Failed to save rating: %v", err)
		return &callback.Response{
			CallbackMsg: "评分失败",
			ShowAlert:   true,
		}, nil
	}

	// Show success message
	stars := ""
	for i := 0; i < 5; i++ {
		if i < rating {
			stars += "⭐"
		} else {
			stars += "☆"
		}
	}

	msg := services.NewMessageBuilder()
	msg.Bold("✅ 感谢您的评价").Newline()
	msg.Newline()
	msg.Textf("问题编号: #%d", issueID).Newline()
	msg.Textf("您的评分: %s", stars).Newline()
	msg.Newline()
	msg.Italic("💡 您的评价将帮助我们改进服务").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("⬅️ 返回列表", "feedback:view")
	kb.AddButton("🏠 返回主菜单", "start")

	return &callback.Response{
		Text:      msg.Build(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
		CallbackMsg: "感谢评价",
	}, nil
}
