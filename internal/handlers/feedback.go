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
		Edit:     true,
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
	h.telegram.SendMessage(ctx.ChatID, msg.Build(), "Markdown", kb.Build())

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
	tmdbIDVal, _ := sess.Get("feedback_tmdb_id")
	mediaTypeVal, _ := sess.Get("feedback_media_type")
	mediaTitleVal, _ := sess.Get("feedback_media_title")
	issueTypeVal, _ := sess.Get("feedback_issue_type")

	tmdbID, _ := tmdbIDVal.(string)
	mediaType, _ := mediaTypeVal.(string)
	mediaTitle, _ := mediaTitleVal.(string)
	issueType, _ := issueTypeVal.(string)

	if tmdbID == "" {
		return fmt.Errorf("missing feedback context")
	}

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
		h.telegram.SendMessage(chatID, "❌ 提交失败，请稍后重试", "Markdown", nil)
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

	h.telegram.SendMessage(chatID, confirmMsg.Build(), "Markdown", kb.Build())

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

	// Send to all admins
	for _, adminID := range adminIDs {
		h.telegram.SendMessage(adminID, message, "Markdown", kb.Build())
	}
}

// IsInFeedbackProcess checks if user is in feedback process
func (h *FeedbackHandler) IsInFeedbackProcess(userID int64) bool {
	sess := h.sessMgr.GetOrCreate(userID)
	step, ok := sess.Get("feedback_step")
	log.Printf("[FeedbackHandler] IsInFeedbackProcess for user %d: step=%v, ok=%v", userID, step, ok)
	return ok && step == "description"
}
