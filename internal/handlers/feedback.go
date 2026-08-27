package handlers

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
)

// FeedbackHandler handles user feedback callbacks
type FeedbackHandler struct {
	sessMgr      *session.Manager
	telegram     *services.TelegramClient
	adminService *services.AdminService
	issueService *services.IssueService
	tmdbClient   *services.TMDBClient
	quotaService *services.QuotaService
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

// SetTMDBClient sets the TMDB client
func (h *FeedbackHandler) SetTMDBClient(tmdb *services.TMDBClient) {
	h.tmdbClient = tmdb
}

// SetQuotaService sets the quota service
func (h *FeedbackHandler) SetQuotaService(quota *services.QuotaService) {
	h.quotaService = quota
}

// getMediaTitle retrieves the media title, using TMDB API if not provided
func (h *FeedbackHandler) getMediaTitle(tmdbIDStr, mediaType, providedTitle string) string {
	// If title is already provided, use it
	if providedTitle != "" {
		return providedTitle
	}

	// Parse TMDB ID
	var tmdbID int
	fmt.Sscanf(tmdbIDStr, "%d", &tmdbID)
	if tmdbID == 0 || h.tmdbClient == nil {
		return ""
	}

	// Fetch from TMDB
	mediaInfo, err := h.tmdbClient.GetMediaByType(tmdbID, mediaType)
	if err != nil {
		logger.Info("[FeedbackHandler] Failed to get media title from TMDB: %v", err)
		return ""
	}

	return mediaInfo.GetTitle()
}

// Handle handles feedback callbacks
func (h *FeedbackHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	logger.Info("[FeedbackHandler] Handle called: action=%s, params=%+v, h=%v, h.sessMgr=%v, h.telegram=%v",
		ctx.Callback.Action, ctx.Callback.Params, h != nil, h.sessMgr != nil, h.telegram != nil)

	if ctx.Callback.Action == "issue" {
		return &callback.Response{
			Text:     "📝 遇到什么问题？\n\n如果是某部电影或剧集的画质、声音、字幕、播放或缺集问题，请选「影视内容问题」。\n\n账号、Bot 功能、求片流程等使用问题或建议，不需要选择影片。",
			Edit:     true,
			Keyboard: &callback.Keyboard{InlineKeyboard: [][]callback.Button{{{Text: "🎬 影视内容问题", CallbackData: "feedback:scope:media"}}, {{Text: "⚙️ 使用问题或建议", CallbackData: "feedback:scope:general"}}, {{Text: "📋 我的问题", CallbackData: "feedback:view"}}, {{Text: "🏠 主菜单", CallbackData: "start"}}}},
		}, nil
	}
	if scope := ctx.Callback.Params["scope"]; scope != "" {
		ctx.Callback.Params = map[string]string{"id": "0", "type": "other"}
		sess := h.sessMgr.GetOrCreate(ctx.UserID)
		if scope == "general" {
			sess.Delete("feedback_require_media")
			ctx.Callback.Params["issue_type"] = "other"
			return h.handleTypeSelect(ctx)
		}
		sess.Set("feedback_require_media", true)
		return h.handleStart(ctx)
	}

	if _, confirm := ctx.Callback.Params["confirm"]; confirm {
		return h.handleConfirm(ctx)
	}

	// New callbacks carry only a compact option index; legacy callbacks with
	// URL-escaped text remain supported for messages sent before this release.
	if quickIndex, hasQuickIndex := ctx.Callback.Params["quick_idx"]; hasQuickIndex {
		return h.handleQuickIndexSelect(ctx, quickIndex)
	}
	if quickText, hasQuick := ctx.Callback.Params["quick"]; hasQuick {
		return h.handleQuickSelect(ctx, quickText)
	}

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

	// Check if user wants to stop follow-up mode
	if _, hasStopFollow := ctx.Callback.Params["stop_follow"]; hasStopFollow {
		return h.handleStopFollowUp(ctx)
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
	// id=0 is the homepage generic issue flow; media detail feedback remains bound
	// to its real TMDB id.

	// Store feedback context in session. Detail pages already cache titles by
	// TMDB ID, so removing the title from callback_data does not lose context.
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	if mediaTitle == "" {
		mediaTitle, _ = sess.GetString("media_title_" + tmdbID)
	}
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

	logger.Info("[FeedbackHandler] handleTypeSelect: issueType=%s, tmdbID=%s, mediaType=%s", issueType, tmdbID, mediaType)

	if issueType == "" || tmdbID == "" {
		return &callback.Response{
			CallbackMsg: "参数错误",
			ShowAlert:   true,
		}, nil
	}

	// Store type in session
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	sess.Set("feedback_issue_type", issueType)
	sess.Set("feedback_tmdb_id", tmdbID)
	sess.Set("feedback_media_type", mediaType)
	requireMedia, _ := sess.Get("feedback_require_media")
	if tmdbID == "0" && (issueType != "other" || requireMedia == true) {
		sess.Set("feedback_step", "media_title")
		sess.Set("feedback_media_type", "media")
		msg := services.NewMessageBuilder()
		msg.Bold("🎬 先告诉我是哪部片").Newline()
		msg.Newline()
		msg.Text("请直接发送片名或剧名，可带季集信息。").Newline()
		msg.Newline()
		msg.Text("例如：纸牌屋 S05E03").Newline()
		msg.Italic("记录媒体后，再选择或输入具体问题。")
		kb := services.NewKeyboardBuilder()
		kb.AddButton("❌ 取消反馈", "cancel")
		return &callback.Response{Text: msg.Build(), CallbackMsg: "请先发送片名", Edit: false, Keyboard: convertKeyboard(kb.Build()), ParseMode: "HTML"}, nil
	}
	sess.Set("feedback_step", "description")

	// Get type label and quick options
	typeInfo := getTypeInfo(issueType)
	typeLabel := typeInfo.label
	if typeLabel == "" {
		typeLabel = "问题反馈"
	}

	// Build enhanced message with quick options
	msg := services.NewMessageBuilder()
	msg.Bold(fmt.Sprintf("🐛 %s", typeLabel)).Newline()
	msg.Newline()

	// Show quick options if available
	if len(typeInfo.quickOptions) > 0 {
		msg.Italic("📋 快捷选择 (点击快速填写):").Newline()
		msg.Newline()
		for i, opt := range typeInfo.quickOptions {
			msg.Textf("%d. %s", i+1, opt).Newline()
		}
		msg.Newline()
	}

	msg.Bold("💬 请描述您遇到的问题").Newline()
	msg.Newline()
	msg.Text("您可以：").Newline()
	msg.Text("• 点击上方快捷选项").Newline()
	msg.Text("• 自由输入问题描述").Newline()
	msg.Text("• 发送图片截图辅助说明").Newline()
	msg.Newline()
	msg.Italic("📝 详细描述有助于快速定位问题").Newline()
	msg.Newline()
	msg.Italic("⏰ 请在 10 分钟内完成描述").Newline()

	// Build keyboard with quick options and cancel
	kb := services.NewKeyboardBuilder()

	// Add quick option buttons (2 per row)
	if len(typeInfo.quickOptions) > 0 {
		for i, opt := range typeInfo.quickOptions {
			// Truncate long options for button text
			buttonText := opt
			if len([]rune(buttonText)) > 15 {
				buttonText = string([]rune(buttonText)[:12]) + "..."
			}
			// Only carry the option index. The option text and media context are
			// already in this user's session; embedding URL-escaped Chinese text
			// easily exceeds Telegram's 64-byte callback_data limit.
			callbackData := fmt.Sprintf("feedback:quick_idx:%d", i)
			kb.AddButton(buttonText, callbackData)
			if i%2 == 1 {
				kb.NewRow()
			}
		}
		if len(typeInfo.quickOptions)%2 != 0 {
			kb.NewRow()
		}
	}

	kb.AddButton("❌ 取消反馈", "cancel")

	return &callback.Response{
		Text:        msg.Build(),
		CallbackMsg: "请发送问题描述或选择快捷选项",
		Edit:        false,
		Keyboard:    convertKeyboard(kb.Build()),
		ParseMode:   "HTML",
	}, nil
}

// getTypeInfo returns type label and quick options for each issue type
func getTypeInfo(issueType string) struct {
	label        string
	quickOptions []string
} {
	typeMap := map[string]struct {
		label        string
		quickOptions []string
	}{
		"quality": {
			label: "画质问题",
			quickOptions: []string{
				"画面模糊/分辨率低",
				"画面卡顿/掉帧",
				"色彩异常/偏色",
				"有水印/广告",
				"画质与标注不符",
			},
		},
		"audio": {
			label: "音频问题",
			quickOptions: []string{
				"没有声音",
				"声音不同步",
				"音质差/有杂音",
				"缺少音轨",
				"没有中文字幕配音",
			},
		},
		"subtitle": {
			label: "字幕问题",
			quickOptions: []string{
				"没有字幕",
				"字幕不同步",
				"字幕翻译错误",
				"缺少中文字幕",
				"字幕显示乱码",
			},
		},
		"not_found": {
			label: "搜索不到",
			quickOptions: []string{
				"搜索结果为空",
				"剧集不完整",
				"版本不对(导演剪辑版等)",
				"缺少特定季/集",
			},
		},
		"playback": {
			label: "播放问题",
			quickOptions: []string{
				"无法播放",
				"播放中断/自动停止",
				"加载缓慢/一直转圈",
				"进度条无法拖动",
				"无法快进/快退",
			},
		},
		"other": {
			label: "其他问题",
			quickOptions: []string{
				"下载失败",
				"订阅问题",
				"账号问题",
				"建议/改进意见",
			},
		},
	}

	info, ok := typeMap[issueType]
	if !ok {
		return struct {
			label        string
			quickOptions []string
		}{label: "问题反馈", quickOptions: []string{}}
	}
	return info
}

// handleQuickIndexSelect resolves compact callback data from the user's
// feedback session. It keeps every generated callback well under 64 bytes.
func (h *FeedbackHandler) handleQuickIndexSelect(ctx *callback.Context, indexText string) (*callback.Response, error) {
	var index int
	if _, err := fmt.Sscanf(indexText, "%d", &index); err != nil || index < 0 {
		return &callback.Response{CallbackMsg: "快捷选项已失效，请重新选择", ShowAlert: true}, nil
	}

	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	step, _ := sess.GetString("feedback_step")
	if step != "description" {
		return &callback.Response{CallbackMsg: "请先发送片名或剧名", ShowAlert: true}, nil
	}
	issueType, _ := sess.GetString("feedback_issue_type")
	options := getTypeInfo(issueType).quickOptions
	if index >= len(options) {
		return &callback.Response{CallbackMsg: "快捷选项已失效，请重新选择", ShowAlert: true}, nil
	}
	return h.handleQuickSelect(ctx, options[index])
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
	if step == "confirm" {
		return fmt.Errorf("feedback draft awaiting confirmation")
	}
	if step != "description" && step != "media_title" {
		return fmt.Errorf("not in feedback process")
	}
	if step == "media_title" {
		mediaTitle := strings.TrimSpace(text)
		if mediaTitle == "" {
			_, err := h.telegram.SendMessage(chatID, "请先发送片名或剧名，例如：纸牌屋 S05E03", "", nil)
			return err
		}
		if len([]rune(mediaTitle)) > 100 {
			_, err := h.telegram.SendMessage(chatID, "片名太长了，请只保留片名和季集信息", "", nil)
			return err
		}
		sess.Set("feedback_media_title", mediaTitle)
		sess.Set("feedback_step", "description")
		issueType, _ := sess.GetString("feedback_issue_type")
		info := getTypeInfo(issueType)
		msg := services.NewMessageBuilder()
		msg.Bold(fmt.Sprintf("✅ 已记录：《%s》", mediaTitle)).Newline()
		msg.Newline()
		msg.Text("现在请选择常见问题，或直接发送详细描述/截图。")
		kb := services.NewKeyboardBuilder()
		for i, option := range info.quickOptions {
			buttonText := option
			if len([]rune(buttonText)) > 15 {
				buttonText = string([]rune(buttonText)[:12]) + "..."
			}
			kb.AddButton(buttonText, fmt.Sprintf("feedback:quick_idx:%d", i))
			if i%2 == 1 {
				kb.NewRow()
			}
		}
		kb.NewRow()
		kb.AddButton("❌ 取消反馈", "cancel")
		_, err := h.telegram.SendMessage(chatID, msg.Build(), "HTML", kb.Build())
		return err
	}

	// Store the description/photo as a draft. Every input path must pass through
	// the same explicit confirmation before an issue is persisted.
	return h.storeDraftAndSendConfirmation(userID, chatID, text, photoFileID)
}

// feedbackTypeLabel returns the user-facing label for a stored issue type.
func feedbackTypeLabel(issueType string) string {
	if label := getTypeInfo(issueType).label; label != "" {
		return label
	}
	return "问题反馈"
}

func clearFeedbackDraft(sess *session.Session) {
	for _, key := range []string{
		"feedback_step", "feedback_tmdb_id", "feedback_media_type",
		"feedback_media_title", "feedback_issue_type", "feedback_require_media",
		"feedback_draft_description", "feedback_draft_photo_file_id",
	} {
		sess.Delete(key)
	}
}

func (h *FeedbackHandler) buildDraftConfirmation(userID int64, description, photoFileID string) (*callback.Response, error) {
	sess := h.sessMgr.GetOrCreate(userID)
	tmdbID, ok := sess.GetString("feedback_tmdb_id")
	if !ok || tmdbID == "" {
		return nil, fmt.Errorf("missing feedback context: tmdb_id")
	}
	issueType, _ := sess.GetString("feedback_issue_type")
	mediaTitle, _ := sess.GetString("feedback_media_title")
	requireMedia, _ := sess.Get("feedback_require_media")
	if requireMedia == true && strings.TrimSpace(mediaTitle) == "" {
		return &callback.Response{CallbackMsg: "请先填写片名或剧名", ShowAlert: true}, nil
	}
	description = strings.TrimSpace(description)
	if description == "" && photoFileID == "" {
		return &callback.Response{CallbackMsg: "请填写问题描述或发送截图", ShowAlert: true}, nil
	}
	if description == "" {
		description = "用户发送了问题截图"
	}

	sess.Set("feedback_draft_description", description)
	sess.Set("feedback_draft_photo_file_id", photoFileID)
	sess.Set("feedback_step", "confirm")

	msg := services.NewMessageBuilder()
	msg.Bold("🧾 确认提交问题").Newline().Newline()
	if mediaTitle != "" {
		msg.Textf("🎬 媒体：%s", mediaTitle).Newline()
	}
	msg.Textf("🏷️ 类型：%s", feedbackTypeLabel(issueType)).Newline()
	msg.Textf("📝 描述：%s", description).Newline()
	if photoFileID != "" {
		msg.Text("📷 已附带截图").Newline()
	}
	msg.Newline().Italic("确认后才会提交给管理员。")
	kb := services.NewKeyboardBuilder()
	kb.AddButton("✅ 确认提交", "feedback:confirm:1")
	kb.AddButton("❌ 取消", "cancel")
	return &callback.Response{Text: msg.Build(), Edit: false, Keyboard: convertKeyboard(kb.Build()), ParseMode: "HTML"}, nil
}

func (h *FeedbackHandler) storeDraftAndSendConfirmation(userID, chatID int64, description, photoFileID string) error {
	resp, err := h.buildDraftConfirmation(userID, description, photoFileID)
	if err != nil {
		return err
	}
	if h.telegram == nil {
		return fmt.Errorf("telegram client not available")
	}
	_, err = h.telegram.SendMessage(chatID, resp.Text, resp.ParseMode, callbackKeyboardToTelegram(resp.Keyboard))
	return err
}

func callbackKeyboardToTelegram(kb *callback.Keyboard) *types.TelegramInlineKeyboard {
	if kb == nil {
		return nil
	}
	rows := make([][]types.TelegramInlineKeyboardButton, len(kb.InlineKeyboard))
	for i, row := range kb.InlineKeyboard {
		rows[i] = make([]types.TelegramInlineKeyboardButton, len(row))
		for j, button := range row {
			btn := types.TelegramInlineKeyboardButton{Text: button.Text, CallbackData: button.CallbackData, URL: button.URL, Style: button.Style}
			if button.Disabled {
				btn.Disabled = types.DisabledButtonValue()
				btn.CallbackData = ""
				btn.URL = ""
			}
			rows[i][j] = btn
		}
	}
	return &types.TelegramInlineKeyboard{InlineKeyboard: rows}
}

func (h *FeedbackHandler) handleConfirm(ctx *callback.Context) (*callback.Response, error) {
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	step, _ := sess.GetString("feedback_step")
	if step != "confirm" {
		// A completed or stale callback is acknowledged without creating another issue.
		return &callback.Response{CallbackMsg: "该反馈已处理，请勿重复提交", ShowAlert: true}, nil
	}
	if h.issueService == nil {
		return &callback.Response{CallbackMsg: "功能暂不可用", ShowAlert: true}, nil
	}
	description, ok := sess.GetString("feedback_draft_description")
	if !ok || description == "" {
		return &callback.Response{CallbackMsg: "反馈草稿已失效，请重新填写", ShowAlert: true}, nil
	}
	photoFileID, _ := sess.GetString("feedback_draft_photo_file_id")
	tmdbID, ok := sess.GetString("feedback_tmdb_id")
	if !ok || tmdbID == "" {
		return &callback.Response{CallbackMsg: "反馈状态已过期，请重新打开反馈", ShowAlert: true}, nil
	}
	mediaType, _ := sess.GetString("feedback_media_type")
	mediaTitle, _ := sess.GetString("feedback_media_title")
	issueType, _ := sess.GetString("feedback_issue_type")
	mediaTitle = h.getMediaTitle(tmdbID, mediaType, mediaTitle)
	userName := "用户"
	if name, ok := sess.GetString("name"); ok && name != "" {
		userName = name
	}

	issue, err := h.issueService.CreateIssueWithPhoto(ctx.UserID, userName, feedbackTypeLabel(issueType), description, mediaType, tmdbID, mediaTitle, photoFileID)
	if err != nil {
		logger.Info("[FeedbackHandler] Failed to create confirmed issue: %v", err)
		return &callback.Response{CallbackMsg: "提交失败，草稿已保留，请稍后重试", ShowAlert: true}, nil
	}
	clearFeedbackDraft(sess)
	go h.notifyAdmins(issue, feedbackTypeLabel(issueType))

	msg := services.NewMessageBuilder()
	msg.Bold("✅ 反馈已提交").Newline().Newline()
	msg.Textf("问题编号: <code>#%d</code>", issue.ID).Newline()
	msg.Textf("问题类型: %s", feedbackTypeLabel(issueType)).Newline().Newline()
	msg.Italic("💡 管理员已收到通知，会尽快处理")
	kb := services.NewKeyboardBuilder()
	kb.AddButton("🏠 主菜单", "start")
	return &callback.Response{Text: msg.Build(), CallbackMsg: "反馈已提交", Edit: false, Keyboard: convertKeyboard(kb.Build()), ParseMode: "HTML"}, nil
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

	// Build detailed message for admins
	msg := services.NewMessageBuilder()

	// Header with priority indicator
	priorityIcon := "🟡"
	if issue.Priority == services.PriorityHigh || issue.Priority == services.PriorityUrgent {
		priorityIcon = "🔴"
	} else if issue.Priority == services.PriorityLow {
		priorityIcon = "🟢"
	}

	msg.Bold(fmt.Sprintf("%s 🐛 新问题反馈", priorityIcon)).Newline()
	msg.Newline()
	msg.Textf("📋 问题编号: <code>#%d</code>", issue.ID).Newline()
	msg.Textf("👤 用户: <code>%s</code> (<b>ID: %d</b>)", issue.UserName, issue.UserID).Newline()
	msg.Textf("🏷️ 类型: %s", typeLabel).Newline()

	// Media info with TMDB ID
	if issue.MediaTitle != "" {
		mediaType := "电影"
		if issue.MediaType == "tv" {
			mediaType = "剧集"
		}
		msg.Textf("🎬 媒体: %s (%s)", issue.MediaTitle, mediaType).Newline()
	}
	if issue.TmdbID > 0 {
		msg.Textf("🆔 TMDB ID: <code>%d</code>", issue.TmdbID).Newline()
	}
	if issue.MediaID != "" && issue.MediaID != "0" {
		msg.Textf("🆔 Media ID: <code>%s</code>", issue.MediaID).Newline()
	}

	msg.Newline()
	msg.Bold("📝 问题描述:").Newline()
	msg.Text(issue.Description).Newline()
	msg.Newline()
	msg.Italic(fmt.Sprintf("🕐 %s", issue.CreatedAt.Format("2006-01-02 15:04:05"))).Newline()

	// Build keyboard for admin actions
	kb := services.NewKeyboardBuilder()
	kb.AddButton("🔍 查看详情", fmt.Sprintf("admin_feedback_detail:id:%d", issue.ID))
	kb.AddButton("💬 回复", fmt.Sprintf("admin_feedback_reply:id:%d", issue.ID))
	kb.NewRow()
	kb.AddButton("🔧 处理中", fmt.Sprintf("admin_issue_processing:id:%d", issue.ID))
	kb.AddButton("✅ 已解决", fmt.Sprintf("admin_issue_fixed:id:%d", issue.ID))
	kb.NewRow()
	kb.AddButton("🚫 关闭", fmt.Sprintf("admin_issue_close:id:%d", issue.ID))

	message := msg.Build()
	keyboard := kb.Build()

	// Send to all admins with error handling
	for _, adminID := range adminIDs {
		// If issue has photo, send photo with caption
		if issue.PhotoFileID != "" {
			// Send photo with the message as caption
			// Use SendPhotoByFileIDWithParseMode to send using Telegram's file_id with HTML parsing
			if _, err := h.telegram.SendPhotoByFileIDWithParseMode(adminID, issue.PhotoFileID, message, "HTML", keyboard); err != nil {
				logger.Info("[FeedbackHandler] Failed to send photo to admin %d: %v", adminID, err)
				// Fallback to text message
				if _, err2 := h.telegram.SendMessage(adminID, message, "HTML", keyboard); err2 != nil {
					logger.Info("[FeedbackHandler] Failed to send fallback message to admin %d: %v", adminID, err2)
				}
			}
		} else {
			if _, err := h.telegram.SendMessage(adminID, message, "HTML", keyboard); err != nil {
				logger.Info("[FeedbackHandler] Failed to notify admin %d: %v", adminID, err)
			}
		}
	}
}

// IsInFeedbackProcess checks if user is in feedback process
func (h *FeedbackHandler) IsInFeedbackProcess(userID int64) bool {
	sess := h.sessMgr.GetOrCreate(userID)
	step, ok := sess.Get("feedback_step")
	logger.Info("[FeedbackHandler] IsInFeedbackProcess for user %d: step=%v, ok=%v", userID, step, ok)
	return ok && (step == "description" || step == "media_title" || step == "confirm")
}

// handleViewList handles viewing user's feedback list
func (h *FeedbackHandler) handleViewList(ctx *callback.Context) (*callback.Response, error) {
	if h.issueService == nil {
		return &callback.Response{
			CallbackMsg: "功能暂不可用",
			ShowAlert:   true,
		}, nil
	}

	// Clear follow-up session when returning to list
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	sess.Delete("feedback_conversation_issue_id")

	issues := h.issueService.GetUserIssues(ctx.UserID)

	msg := services.NewMessageBuilder()
	msg.Bold("🐛 我的反馈").Newline()
	msg.Newline()

	if len(issues) == 0 {
		msg.Text("暂无反馈记录").Newline()
		msg.Newline()
		msg.Italic("💡 在影片详情页点击「🐛 反馈」按钮提交问题")

		kb := services.NewKeyboardBuilder()
		kb.AddButton("🏠 主菜单", "start")

		return &callback.Response{
			Text:     msg.Build(),
			Edit:     true,
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	// Sort by created date (newest first)
	sort.Slice(issues, func(i, j int) bool { return issues[i].CreatedAt.After(issues[j].CreatedAt) })

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
	kb.AddButton("🏠 主菜单", "start")

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

	// Check if user can follow-up (only when issue has admin reply and is not closed)
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	canFollowUp := false
	if issue.Status != services.IssueStatusClosed {
		for _, reply := range issue.Replies {
			if reply.Type == "admin" {
				canFollowUp = true
				break
			}
		}
	}

	// Only set follow-up session if admin has replied
	if canFollowUp {
		sess.Set("feedback_conversation_issue_id", float64(issueID))
	} else {
		// Clear any existing follow-up session
		sess.Delete("feedback_conversation_issue_id")
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

	// 追加回复功能提示（仅当用户未禁用追问时显示）
	followupDisabled := h.quotaService != nil && h.quotaService.IsFollowupDisabled(ctx.UserID)
	if issue.Status != services.IssueStatusClosed && !followupDisabled {
		kb.NewRow()
		kb.AddButton("⏹️ 停止追问", fmt.Sprintf("feedback:stop_follow:%d", issue.ID))

		msg.Newline()
		msg.Text("──────────────────").Newline()

		// 如果有管理员回复，显示追问提示
		hasAdminReply := false
		for _, reply := range issue.Replies {
			if reply.Type == "admin" {
				hasAdminReply = true
				break
			}
		}

		if hasAdminReply {
			msg.Bold("💬 回复管理员").Newline()
			msg.Italic("👆 直接在下方输入框发送消息即可回复").Newline()
			msg.Italic("👆 点击「停止追问」退出对话模式").Newline()
		} else {
			msg.Italic("💡 管理员回复后，可直接在此回复").Newline()
		}
		msg.Newline()
	}

	kb.NewRow()
	kb.AddButton("⬅️ 返回列表", "feedback:view")
	kb.AddButton("🏠 主菜单", "start")

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
	// Check if follow-up is disabled for this user
	if h.quotaService != nil && h.quotaService.IsFollowupDisabled(userID) {
		// Don't process follow-up, just ignore it
		logger.Info("[FeedbackHandler] Follow-up disabled for user %d, ignoring message", userID)
		return nil
	}

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
		logger.Info("[FeedbackHandler] Failed to add follow-up: %v", err)
		return err
	}

	// Confirm to user
	confirmMsg := services.NewMessageBuilder()
	confirmMsg.Bold("💬 追问已发送").Newline()
	confirmMsg.Newline()
	confirmMsg.Textf("问题编号: #%d", issueID).Newline()
	confirmMsg.Italic("管理员已收到您的追问，会尽快回复").Newline()

	if _, sendErr := h.telegram.SendMessage(chatID, confirmMsg.Build(), "HTML", nil); sendErr != nil {
		logger.Warn("[FeedbackHandler] 追问确认发送失败 chat=%d issue=%d: %v", chatID, issueID, sendErr)
	}

	// Notify admins even if the user confirmation could not be delivered.
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
			logger.Info("[FeedbackHandler] Failed to notify admin %d: %v", adminID, err)
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
		logger.Info("[反馈] 用户关闭反馈失败: issue=%d user=%d err=%v", issueID, ctx.UserID, err)
		return &callback.Response{
			CallbackMsg: "这条反馈暂时关不了，请稍后再试",
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
	if err := h.issueService.RateSatisfaction(issueID, rating, ctx.UserID); err != nil {
		logger.Info("[FeedbackHandler] Failed to save rating: %v", err)
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
	kb.AddButton("🏠 主菜单", "start")

	return &callback.Response{
		Text:        msg.Build(),
		Edit:        true,
		Keyboard:    convertKeyboard(kb.Build()),
		CallbackMsg: "感谢评价",
	}, nil
}

// handleQuickSelect handles quick option selection from user
func (h *FeedbackHandler) handleQuickSelect(ctx *callback.Context, encodedText string) (*callback.Response, error) {
	// Decode the URL-encoded text
	decodedText, err := url.QueryUnescape(encodedText)
	if err != nil {
		// Fallback to simple replacement decoding
		decodedText = strings.ReplaceAll(encodedText, "_", " ")
		decodedText = strings.ReplaceAll(decodedText, "\\c", ":")
		decodedText = strings.ReplaceAll(decodedText, "\\s", ";")
	}

	// New compact callbacks recover media context from the session. Legacy
	// callbacks still provide id directly and continue to work.
	tmdbID := ctx.Callback.Params["id"]
	if tmdbID == "" {
		if sess := h.sessMgr.GetOrCreate(ctx.UserID); sess != nil {
			tmdbID, _ = sess.GetString("feedback_tmdb_id")
		}
	}
	if tmdbID == "" {
		return &callback.Response{
			CallbackMsg: "反馈状态已过期，请重新打开反馈",
			ShowAlert:   true,
		}, nil
	}

	// Quick choices are drafts too; old URL-escaped callbacks safely enter the
	// same confirmation path instead of creating an issue immediately.
	return h.buildDraftConfirmation(ctx.UserID, decodedText, "")
}

// handleStopFollowUp handles user request to stop follow-up mode
func (h *FeedbackHandler) handleStopFollowUp(ctx *callback.Context) (*callback.Response, error) {
	// Parse issue ID from params
	issueIDStr := ctx.Callback.Params["stop_follow"]
	if issueIDStr == "" {
		return &callback.Response{
			CallbackMsg: "无效的反馈ID",
			ShowAlert:   true,
		}, nil
	}

	var issueID int64
	fmt.Sscanf(issueIDStr, "%d", &issueID)

	// Clear the follow-up session
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	sess.Delete("feedback_conversation_issue_id")

	// Get issue for display
	issue, exists := h.issueService.GetIssue(issueID)
	if !exists || issue.UserID != ctx.UserID {
		return &callback.Response{
			CallbackMsg: "反馈不存在",
			ShowAlert:   true,
		}, nil
	}

	// Build confirmation message
	msg := services.NewMessageBuilder()
	msg.Bold("⏹️ 已退出追问模式").Newline()
	msg.Newline()
	msg.Textf("反馈编号: #%d", issue.ID).Newline()
	msg.Newline()
	msg.Italic("💡 如需继续反馈，请重新进入此反馈详情页").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🔄 继续追问", fmt.Sprintf("feedback:detail_id:%d", issue.ID))
	kb.NewRow()
	kb.AddButton("⬅️ 返回列表", "feedback:view")
	kb.AddButton("🏠 主菜单", "start")

	return &callback.Response{
		Text:        msg.Build(),
		Edit:        true,
		Keyboard:    convertKeyboard(kb.Build()),
		CallbackMsg: "已退出追问模式",
	}, nil
}
