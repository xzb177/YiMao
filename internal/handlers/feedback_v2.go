package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
	"emby-telegram-bot/internal/ui"
)

// FeedbackHandlerV2 替换版的反馈处理器（直接使用 SQLite）
type FeedbackHandlerV2 struct {
	sessMgr            *session.SessionManager
	telegram           *services.TelegramClient
	adminService       *services.AdminService
	feedbackDB         *services.FeedbackDB
	templateService     *services.FeedbackTemplateService
	similarityChecker  *services.SimilarityChecker
	notifyService      *services.NotificationService
	migrated           bool
}

// NewFeedbackHandlerV2 创建新的反馈处理器
func NewFeedbackHandlerV2(
	sessMgr *session.SessionManager,
	telegram *services.TelegramClient,
	adminService *services.AdminService,
	feedbackDB *services.FeedbackDB,
	notifyService *services.NotificationService,
) *FeedbackHandlerV2 {
	return &FeedbackHandlerV2{
		sessMgr:           sessMgr,
		telegram:          telegram,
		adminService:      adminService,
		feedbackDB:        feedbackDB,
		templateService:    services.NewFeedbackTemplateService(),
		similarityChecker: services.NewSimilarityChecker(0.5),
		notifyService:      notifyService,
		migrated:          false,
	}
}

// MigrateOldData 迁移旧 JSON 数据到 SQLite
func (h *FeedbackHandlerV2) MigrateOldData(jsonFile string) error {
	if h.migrated {
		return nil
	}

	if _, err := os.Stat(jsonFile); os.IsNotExist(err) {
		log.Printf("[FeedbackHandlerV2] No old data to migrate: %s not found", jsonFile)
		return nil
	}

	log.Printf("[FeedbackHandlerV2] Starting data migration from %s...", jsonFile)

	data, err := os.ReadFile(jsonFile)
	if err != nil {
		return fmt.Errorf("failed to read JSON file: %w", err)
	}

	// 解析旧格式
	type OldIssue struct {
		ID          int64                      `json:"id"`
		UserID      int64                      `json:"user_id"`
		UserName    string                     `json:"user_name"`
		Title       string                     `json:"title"`
		Description string                     `json:"description"`
		Status      string                     `json:"status"`
		Priority    string                     `json:"priority"`
		MediaType   string                     `json:"media_type"`
		MediaID     string                     `json:"media_id"`
		MediaTitle  string                     `json:"media_title"`
		TmdbID      int                        `json:"tmdb_id"`
		CreatedAt   time.Time                  `json:"created_at"`
		UpdatedAt   time.Time                  `json:"updated_at"`
		Replies     []services.IssueReply      `json:"replies"`
	}

	var oldIssues map[int64]*OldIssue
	if err := json.Unmarshal(data, &oldIssues); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	migrated := 0
	for _, oldIssue := range oldIssues {
		// 检查是否已存在
		if _, exists := h.feedbackDB.GetFeedback(oldIssue.ID); exists {
			continue
		}

		// 转换为新格式
		feedback := &services.Feedback{
			ID:          oldIssue.ID,
			UserID:      oldIssue.UserID,
			UserName:    oldIssue.UserName,
			Title:       oldIssue.Title,
			Description: oldIssue.Description,
			IssueType:   h.inferIssueType(oldIssue.Title),
			Priority:    oldIssue.Priority,
			Status:      oldIssue.Status,
			MediaType:   oldIssue.MediaType,
			MediaID:     oldIssue.MediaID,
			MediaTitle:  oldIssue.MediaTitle,
			TmdbID:      oldIssue.TmdbID,
			CreatedAt:   oldIssue.CreatedAt,
			UpdatedAt:   oldIssue.UpdatedAt,
			Tags:        []string{},
			Images:      []string{},
		}

		// 创建反馈
		_, err := h.feedbackDB.CreateFeedback(feedback)
		if err != nil {
			log.Printf("[FeedbackHandlerV2] Failed to migrate issue #%d: %v", oldIssue.ID, err)
			continue
		}

		// 迁移回复
		for _, oldReply := range oldIssue.Replies {
			_, err := h.feedbackDB.AddReply(
				oldIssue.ID,
				oldReply.AuthorID,
				oldReply.AuthorName,
				oldReply.Content,
				oldReply.Type,
			)
			if err != nil {
				log.Printf("[FeedbackHandlerV2] Failed to migrate reply for issue #%d: %v", oldIssue.ID, err)
			}
		}

		migrated++
	}

	log.Printf("[FeedbackHandlerV2] Migration completed: %d/%d issues migrated", migrated, len(oldIssues))
	h.migrated = true

	return nil
}

// Handle 处理反馈回调
func (h *FeedbackHandlerV2) Handle(ctx *callback.Context) (*callback.Response, error) {
	// 自动迁移旧数据
	if !h.migrated {
		go h.MigrateOldData("./data/feedback.json")
	}

	// 使用 UI 构建器（极简卡片风）
	uiBuilder := ui.NewHistoryBuilder(ui.StyleCard)

	switch ctx.Callback.Action {
	case "feedback":
		return h.handleMenu(ctx)
	case "feedback_type":
		return h.handleTypeSelect(ctx)
	case "feedback_template":
		return h.handleTemplateSelect(ctx)
	case "feedback_submit":
		return h.handleSubmit(ctx)
	case "feedback_list", "my_feedback":
		return h.handleList(ctx)
	case "feedback_detail":
		return h.handleDetail(ctx)
	case "feedback_reply":
		return h.handleReply(ctx)
	case "feedback_stats":
		return h.handleStats(ctx)
	case "feedback_export_csv":
		return h.handleExportCSV(ctx)
	case "feedback_export_excel":
		return h.handleExportExcel(ctx)
	default:
		return h.handleMenu(ctx)
	}
}

// handleMenu 显示反馈菜单
func (h *FeedbackHandlerV2) handleMenu(ctx *callback.Context) (*callback.Response, error) {
	msg := services.NewMessageBuilder()
	msg.Bold("🐛 问题反馈").Newline()
	msg.Newline()
	msg.Text("发现影片播放问题？遇到功能使用障碍？").Newline()
	msg.Text("欢迎通过以下方式向我们反馈：").Newline()
	msg.Newline()
	msg.Bold("📝 反馈方式").Newline()
	msg.Newline()
	msg.Text("1️⃣ 选择问题类型").Newline()
	msg.Text("2️⃣ 使用模板描述（可选）").Newline()
	msg.Text("3️⃣ 上传相关截图（可选）").Newline()
	msg.Text("4️⃣ 提交反馈").Newline()
	msg.Newline()
	msg.Italic("💡 使用模板可以更准确地描述问题").Newline()

	kb := services.NewKeyboardBuilder()

	// 问题类型按钮
	issueTypes := []struct {
		label       string
		callback    string
		issueType   string
	}{
		{"📺 画质问题", "feedback_type:video_quality", "video_quality"},
		{"🔊 音频问题", "feedback_type:audio_quality", "audio_quality"},
		{"📝 字幕问题", "feedback_type:subtitle", "subtitle"},
		{"🔍 搜索问题", "feedback_type:search", "search"},
		{"▶️ 播放问题", "feedback_type:playback", "playback"},
		{"🤔 其他问题", "feedback_type:other", "other"},
	}

	for _, it := range issueTypes {
		callbackData := fmt.Sprintf("%s:media_type:%s:media_id:%s", it.callback, "", "")
		kb.AddButton(it.label, callbackData)
		if callbackData == "feedback_type:other" {
			kb.NewRow()
		}
	}

	kb.AddButton("📊 查看统计", "feedback_stats")
	kb.AddButton("📤 导出数据", "feedback_export_csv")
	kb.NewRow()
	kb.AddButton("📋 我的反馈", "feedback_list")
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     false,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleTypeSelect 处理类型选择
func (h *FeedbackHandlerV2) handleTypeSelect(ctx *callback.Context) (*callback.Response, error) {
	issueType := ctx.Callback.Params["issue_type"]
	mediaType := ctx.Callback.Params["media_type"]
	mediaID := ctx.Callback.Params["id"]

	log.Printf("[FeedbackHandlerV2] Type selected: %s, mediaType: %s, mediaID: %s", issueType, mediaType, mediaID)

	// 存储到 session
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	sess.Set("feedback_issue_type", issueType)
	sess.Set("feedback_media_type", mediaType)
	sess.Set("feedback_media_id", mediaID)
	sess.Set("feedback_step", "template")

	// 获取该类型的模板
	templates := h.templateService.GetTemplatesByType(issueType)

	msg := services.NewMessageBuilder()
	msg.Bold("📝 选择描述模板").Newline()
	msg.Newline()

	if len(templates) > 0 {
		msg.Text("💡 选择一个模板可以更准确地描述问题").Newline()
		msg.Newline()

		kb := services.NewKeyboardBuilder()

		// 显示模板按钮
		for _, tmpl := range templates {
			callbackData := fmt.Sprintf("feedback_template:id:%s", tmpl.ID)
			kb.AddButton(fmt.Sprintf("📋 %s", tmpl.Name), callbackData)
			if callbackData == fmt.Sprintf("feedback_template:id:%s", tmpl.ID) {
				kb.NewRow()
			}
		}

		// 添加"自定义"选项
		kb.AddButton("✏️ 自定义描述", "feedback_template:custom")
		kb.NewRow()
		kb.AddButton("❌ 取消", "cancel")

		return &callback.Response{
			Text:     msg.Build(),
			Edit:     true,
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	// 如果没有模板，直接要求描述
	sess.Set("feedback_step", "description")
	return h.askDescription(ctx, issueType)
}

// handleTemplateSelect 处理模板选择
func (h *FeedbackHandlerV2) handleTemplateSelect(ctx *callback.Context) (*callback.Response, error) {
	templateID := ctx.Callback.Params["id"]

	sess := h.sessMgr.GetOrCreate(ctx.UserID)

	if templateID == "custom" {
		// 自定义描述
		sess.Set("feedback_template_id", "")
		sess.Set("feedback_step", "description")

		issueTypeVal, _ := sess.Get("feedback_issue_type")
		issueType, _ := issueTypeVal.(string)
		return h.askDescription(ctx, issueType)
	}

	// 选择模板
	template, exists := h.templateService.GetTemplate(templateID)
	if !exists {
		return &callback.Response{
			CallbackMsg: "模板不存在",
			ShowAlert:   true,
		}, nil
	}

	// 存储模板 ID
	sess.Set("feedback_template_id", template.ID)
	sess.Set("feedback_template_fields", template.Fields)

	// 显示模板字段
	msg := services.NewMessageBuilder()
	msg.Bold(fmt.Sprintf("📋 %s", template.Name)).Newline()
	msg.Newline()
	msg.Italic("💬 请根据以下模板填写问题描述：").Newline()
	msg.Newline()

	// 显示模板内容
	for _, field := range template.Fields {
		msg.Textf("• %s：%s", field.Label, field.Example).Newline()
	}

	msg.Newline()
	msg.Italic("⏰ 请直接发送问题描述").Newline()

	// 更新状态为等待描述
	sess.Set("feedback_step", "description")

	kb := services.NewKeyboardBuilder()
	kb.AddButton("❌ 取消", "cancel")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// askDescription 询问描述
func (h *FeedbackHandlerV2) askDescription(ctx *callback.Context, issueType string) (*callback.Response, error) {
	msg := services.NewMessageBuilder()
	msg.Bold("💬 请描述问题").Newline()
	msg.Newline()
	msg.Italic("💡 请详细描述您遇到的问题").Newline()
	msg.Newline()
	msg.Text("建议包含：").Newline()
	msg.Text("• 问题的具体表现").Newline()
	msg.Text("• 发生的时间或场景").Newline()
	msg.Text("• 任何有助于解决问题的信息").Newline()
	msg.Newline()
	msg.Italic("⏰ 请在 5 分钟内完成描述").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("❌ 取消", "cancel")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// HandleFeedbackText 处理用户输入的描述
func (h *FeedbackHandlerV2) HandleFeedbackText(userID int64, chatID int64, text string) error {
	sess := h.sessMgr.GetOrCreate(userID)

	// 检查是否在反馈流程中
	stepVal, _ := sess.Get("feedback_step")
	step, _ := stepVal.(string)
	if step != "description" {
		return fmt.Errorf("not in feedback process")
	}

	// 获取反馈上下文
	issueTypeVal, _ := sess.Get("feedback_issue_type")
	issueType, _ := issueTypeVal.(string)

	mediaTypeVal, _ := sess.Get("feedback_media_type")
	mediaType, _ := mediaTypeVal.(string)

	mediaIDVal, _ := sess.Get("feedback_media_id")
	mediaID, _ := mediaIDVal.(string)

	templateIDVal, _ := sess.Get("feedback_template_id")
	templateID, _ := templateIDVal.(string)

	// 清除 session
	sess.Delete("feedback_step")
	sess.Delete("feedback_issue_type")
	sess.Delete("feedback_media_type")
	sess.Delete("feedback_media_id")
	sess.Delete("feedback_template_id")

	// 获取用户名
	userName := "用户"
	if nameVal, ok := sess.Get("name"); ok && nameVal != "" {
		if name, ok := nameVal.(string); ok {
			userName = name
		}
	}

	// 获取类型标签
	typeLabels := map[string]string{
		"video_quality": "画质问题",
		"audio_quality": "音频问题",
		"subtitle":     "字幕问题",
		"search":       "搜索问题",
		"playback":     "播放问题",
		"other":        "其他问题",
	}
	typeLabel := typeLabels[issueType]
	if typeLabel == "" {
		typeLabel = "问题反馈"
	}

	// 检查相似反馈
	similar, _ := h.feedbackDB.FindSimilarFeedbacks(0, issueType, userID)
	if len(similar) > 0 {
		// 显示相似反馈
		msg := services.NewMessageBuilder()
		msg.Bold("💡 发现相似反馈").Newline()
		msg.Newline()
		msg.Text("以下反馈可能与您的问题相关：").Newline()
		msg.Newline()

		for i, fb := range similar {
			if i >= 3 {
				break
			}
			msg.Textf("%d. #%d - %s", i+1, fb.ID, truncateText(fb.Description, 50)).Newline()
		}

		msg.Newline()
		msg.Text("如果这是重复问题，请等待管理员处理。").Newline()
		msg.Text("如果是新问题，请继续提交。").Newline()

		// 添加按钮：继续提交或取消
		kb := services.NewKeyboardBuilder()
		kb.AddButton("✅ 继续提交", fmt.Sprintf("feedback_confirm:%d", userID))
		kb.AddButton("❌ 取消", "cancel")

		// 存储临时数据
		sess.Set("pending_feedback", map[string]interface{}{
			"issue_type": issueType,
			"text":       text,
			"user_name":  userName,
			"media_type": mediaType,
			"media_id":   mediaID,
			"template_id": templateID,
		})

		h.telegram.SendMessage(chatID, msg.Build(), "HTML", kb.Build())
		return nil
	}

	// 创建反馈
	return h.createFeedback(userID, chatID, userName, typeLabel, issueType, text, mediaType, mediaID, templateID)
}

// handleSubmit 确认提交
func (h *FeedbackHandlerV2) handleSubmit(ctx *callback.Context) (*callback.Response, error) {
	sess := h.sessMgr.GetOrCreate(ctx.UserID)

	// 获取临时数据
	pendingVal, exists := sess.Get("pending_feedback")
	if !exists {
		return &callback.Response{
			CallbackMsg: "未找到待提交的反馈",
			ShowAlert:   true,
		}, nil
	}

	pending := pendingVal.(map[string]interface{})

	issueType, _ := pending["issue_type"].(string)
	text, _ := pending["text"].(string)
	userName, _ := pending["user_name"].(string)
	mediaType, _ := pending["media_type"].(string)
	mediaID, _ := pending["media_id"].(string)
	templateID, _ := pending["template_id"].(string)

	typeLabels := map[string]string{
		"video_quality": "画质问题",
		"audio_quality": "音频问题",
		"subtitle":     "字幕问题",
		"search":       "搜索问题",
		"playback":     "播放问题",
		"other":        "其他问题",
	}
	typeLabel := typeLabels[issueType]

	// 清除临时数据
	sess.Delete("pending_feedback")

	// 创建反馈
	err := h.createFeedback(ctx.UserID, ctx.ChatID, userName, typeLabel, issueType, text, mediaType, mediaID, templateID)
	if err != nil {
		return nil, err
	}

	return &callback.Response{
		CallbackMsg: "✅ 反馈已提交",
		ShowAlert:   true,
	}, nil
}

// createFeedback 创建反馈
func (h *FeedbackHandlerV2) createFeedback(userID, chatID int64, userName, typeLabel, issueType, description, mediaType, mediaID, templateID string) error {
	// 智能推断优先级
	priority := "medium"
	if h.similarityChecker != nil {
		// 检查是否频繁出现
		similar, _ := h.feedbackDB.FindSimilarFeedbacks(0, issueType, 0)
		if len(similar) >= 3 {
			priority = "high"
		}
		if len(similar) >= 5 {
			priority = "urgent"
		}
	}

	// 创建反馈
	feedback := &services.Feedback{
		UserID:      userID,
		UserName:    userName,
		Title:       typeLabel,
		Description: description,
		IssueType:   issueType,
		Priority:    priority,
		Status:      "open",
		MediaType:   mediaType,
		MediaID:     mediaID,
		Tags:        []string{issueType},
		Images:      []string{},
	}

	id, err := h.feedbackDB.CreateFeedback(feedback)
	if err != nil {
		log.Printf("[FeedbackHandlerV2] Failed to create feedback: %v", err)
		h.telegram.SendMessage(chatID, "❌ 提交失败，请稍后重试", "", nil)
		return err
	}

	feedback.ID = id

	// 确认消息
	msg := services.NewMessageBuilder()
	msg.Bold("✅ 反馈已提交").Newline()
	msg.Newline()
	msg.Textf("问题编号: #%d", id).Newline()
	msg.Textf("问题类型: %s", typeLabel).Newline()
	msg.Textf("优先级: %s", priority).Newline()
	msg.Newline()
	msg.Italic("💡 管理员已收到通知，会尽快处理").Newline()
	msg.Newline()
	msg.Italic("📬 收到管理员回复时会通知您").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("📋 我的反馈", "feedback_list")
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	h.telegram.SendMessage(chatID, msg.Build(), "HTML", kb.Build())

	// 通知管理员
	go h.notifyAdmins(feedback)

	return nil
}

// handleList 处理列表查看
func (h *FeedbackHandlerV2) handleList(ctx *callback.Context) (*callback.Response, error) {
	feedbacks, err := h.feedbackDB.GetUserFeedbacks(ctx.UserID)
	if err != nil {
		log.Printf("[FeedbackHandlerV2] Failed to get feedbacks: %v", err)
		return &callback.Response{
			CallbackMsg: "获取失败",
			ShowAlert:   true,
		}, nil
	}

	msg := services.NewMessageBuilder()
	msg.Bold("🐛 我的反馈").Newline()
	msg.Newline()

	if len(feedbacks) == 0 {
		msg.Text("暂无反馈记录").Newline()
		msg.Newline()
		msg.Italic("💡 点击「问题反馈」提交第一个问题").Newline()

		kb := services.NewKeyboardBuilder()
		kb.AddButton("⬅️ 返回主菜单", "start")

		return &callback.Response{
			Text:     msg.Build(),
			Edit:     true,
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	msg.Textf("共 %d 条反馈记录", len(feedbacks)).Newline()
	msg.Newline()

	kb := services.NewKeyboardBuilder()

	// 显示最近 10 条
	displayCount := 10
	if len(feedbacks) < displayCount {
		displayCount = len(feedbacks)
	}

	for i := 0; i < displayCount; i++ {
		fb := feedbacks[i]
		statusIcon := h.getStatusIcon(fb.Status)
		mediaText := ""
		if fb.MediaTitle != "" {
			mediaText = fmt.Sprintf(" - %s", fb.MediaTitle)
		}

		msg.Textf("%d. %s #%d%s", i+1, statusIcon, fb.ID, mediaText).Newline()
		msg.Textf("   %s", truncateText(fb.Description, 40)).Newline()
		msg.Newline()

		// 按钮
		buttonText := fmt.Sprintf("#%d %s", fb.ID, fb.Status)
		kb.AddButton(buttonText, fmt.Sprintf("feedback_detail:id:%d", fb.ID))
		if (i+1)%2 == 0 {
			kb.NewRow()
		}
	}

	if len(feedbacks) > displayCount {
		kb.NewRow()
		msg.Textf("... 还有 %d 条记录", len(feedbacks)-displayCount).Newline()
	}

	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleDetail 处理详情查看
func (h *FeedbackHandlerV2) handleDetail(ctx *callback.Context) (*callback.Response, error) {
	idStr := ctx.Callback.Params["id"]
	id := parseInt64(idStr)

	feedback, exists := h.feedbackDB.GetFeedback(id)
	if !exists {
		return &callback.Response{
			CallbackMsg: "反馈不存在",
			ShowAlert:   true,
		}, nil
	}

	// 获取回复
	replies, _ := h.feedbackDB.GetFeedbackReplies(id)

	msg := services.NewMessageBuilder()
	msg.Bold(fmt.Sprintf("#%d %s", feedback.ID, feedback.Title)).Newline()
	msg.Newline()

	// 状态和优先级
	msg.Textf("%s %s · %s %s",
		h.getStatusIcon(feedback.Status),
		feedback.Status,
		h.getPriorityIcon(feedback.Priority),
		feedback.Priority,
	).Newline()

	// 媒体信息
	if feedback.MediaTitle != "" {
		msg.Textf("🎬 %s (%s)", feedback.MediaTitle, feedback.MediaType).Newline()
	}

	msg.Newline()

	// 描述
	msg.Bold("📝 问题描述:").Newline()
	msg.Text(feedback.Description).Newline()
	msg.Newline()

	// 标签
	if len(feedback.Tags) > 0 {
		msg.Bold("🏷️ 标签: ").Newline()
		msg.Text(strings.Join(feedback.Tags, "、")).Newline()
		msg.Newline()
	}

	// 图片
	if len(feedback.Images) > 0 {
		msg.Bold("📸 相关截图:").Newline()
		for _, img := range feedback.Images {
			msg.Textf("• %s", img).Newline()
		}
		msg.Newline()
	}

	// 回复
	if len(replies) > 0 {
		msg.Bold("💬 管理员回复:").Newline()
		for _, reply := range replies {
			msg.Textf("  %s: %s", reply.AuthorName, reply.Content).Newline()
		}
		msg.Newline()
	}

	msg.Italic(fmt.Sprintf("🕐 %s", feedback.CreatedAt.Format("2006-01-02 15:04"))).Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("⬅️ 返回列表", "feedback_list")
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleReply 处理回复
func (h *FeedbackHandlerV2) handleReply(ctx *callback.Context) (*callback.Response, error) {
	// 管理员回复功能
	idStr := ctx.Callback.Params["id"]
	id := parseInt64(idStr)

	// 检查是否是管理员
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "仅管理员可回复",
			ShowAlert:   true,
		}, nil
	}

	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	sess.Set("admin_reply_feedback_id", id)

	msg := services.NewMessageBuilder()
	msg.Bold("💬 回复反馈").Newline()
	msg.Newline()
	msg.Italic("💬 请输入回复内容").Newline()

	// 更新键盘
	kb := services.NewKeyboardBuilder()
	kb.AddButton("❌ 取消", "cancel")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleStats 处理统计查看
func (h *FeedbackHandlerV2) handleStats(ctx *callback.Context) (*callback.Response, error) {
	// 检查是否是管理员
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "仅管理员可查看统计",
			ShowAlert:   true,
		}, nil
	}

	stats, err := h.feedbackDB.GetFeedbackStatistics()
	if err != nil {
		return &callback.Response{
			CallbackMsg: "获取统计失败",
			ShowAlert:   true,
		}, nil
	}

	msg := services.NewMessageBuilder()
	msg.Bold("📊 反馈统计").Newline()
	msg.Newline()

	// 总体统计
	msg.Bold("📈 总体统计").Newline()
	msg.Textf("• 总反馈数: %d", stats.TotalCount).Newline()
	msg.Textf("• 待处理: %d", stats.OpenCount).Newline()
	msg.Textf("• 处理中: %d", stats.ProcessingCount).Newline()
	msg.Textf("• 已解决: %d", stats.FixedCount).Newline()
	msg.Textf("• 已关闭: %d", stats.ClosedCount).Newline()
	msg.Newline()

	// 类型分布
	if len(stats.ByType) > 0 {
		msg.Bold("🏷️ 类型分布").Newline()
		for typeName, count := range stats.ByType {
			msg.Textf("• %s: %d", typeName, count).Newline()
		}
		msg.Newline()
	}

	// 优先级分布
	if len(stats.ByPriority) > 0 {
		msg.Bold("⚖️ 优先级分布").Newline()
		for priority, count := range stats.ByPriority {
			msg.Textf("• %s: %d", priority, count).Newline()
		}
		msg.Newline()
	}

	// 平均解决时间
	if stats.AvgResolutionTime > 0 {
		msg.Bold("⏱️ 平均解决时间").Newline()
		msg.Textf("• %v", stats.AvgResolutionTime).Newline()
		msg.Newline()
	}

	kb := services.NewKeyboardBuilder()
	kb.AddButton("⬅️ 返回菜单", "feedback")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleExportCSV 处理 CSV 导出
func (h *FeedbackHandlerV2) handleExportCSV(ctx *callback.Context) (*callback.Response, error) {
	// 检查是否是管理员
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "仅管理员可导出",
			ShowAlert:   true,
		}, nil
	}

	data, err := h.feedbackDB.ExportToCSV(nil)
	if err != nil {
		return &callback.Response{
			CallbackMsg: "导出失败",
			ShowAlert:   true,
		}, nil
	}

	msg := services.NewMessageBuilder()
	msg.Bold("📤 CSV 数据导出").Newline()
	msg.Newline()
	msg.Text("数据已准备，文件将发送到您的私聊").Newline()

	// TODO: 发送文件
	// h.telegram.SendDocument(ctx.UserID, "feedback.csv", data)

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: nil,
	}, nil
}

// handleExportExcel 处理 Excel 导出
func (h *FeedbackHandlerV2) handleExportExcel(ctx *callback.Context) (*callback.Response, error) {
	// 检查是否是管理员
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			CallbackMsg: "仅管理员可导出",
			ShowAlert:   true,
		}, nil
	}

	data, err := h.feedbackDB.ExportToExcel(nil)
	if err != nil {
		return &callback.Response{
			CallbackMsg: "导出失败",
			ShowAlert:   true,
		}, nil
	}

	msg := services.NewMessageBuilder()
	msg.Bold("📤 Excel 数据导出").Newline()
	msg.Newline()
	msg.Text("数据已准备，文件将发送到您的私聊").Newline()

	// TODO: 发送文件
	// h.telegram.SendDocument(ctx.UserID, "feedback.xlsx", data)

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: nil,
	}, nil
}

// notifyAdmins 通知管理员
func (h *FeedbackHandlerV2) notifyAdmins(feedback *services.Feedback) {
	if h.adminService == nil {
		return
	}

	adminIDs := h.adminService.GetAdminIDs()
	if len(adminIDs) == 0 {
		return
	}

	// 构建消息
	msg := services.NewMessageBuilder()
	msg.Bold("🐛 新问题反馈").Newline()
	msg.Newline()
	msg.Textf("📋 问题编号: #%d", feedback.ID).Newline()
	msg.Textf("👤 用户: %s", feedback.UserName).Newline()
	msg.Textf("🏷️ 类型: %s", feedback.IssueType).Newline()

	if feedback.MediaTitle != "" {
		msg.Textf("🎬 媒体: %s (%s)", feedback.MediaTitle, feedback.MediaType).Newline()
	}

	msg.Newline()
	msg.Bold("📝 问题描述:").Newline()
	msg.Text(feedback.Description).Newline()
	msg.Newline()
	msg.Italic(fmt.Sprintf("🕐 %s", feedback.CreatedAt.Format("2006-01-02 15:04"))).Newline()

	// 构建管理员操作键盘
	kb := services.NewKeyboardBuilder()
	kb.AddButton("💬 回复", fmt.Sprintf("feedback_reply:id:%d", feedback.ID))
	kb.AddButton("🔧 处理中", fmt.Sprintf("feedback_update:id:%d:status:processing", feedback.ID))
	kb.NewRow()
	kb.AddButton("✅ 已解决", fmt.Sprintf("feedback_update:id:%d:status:fixed", feedback.ID))
	kb.AddButton("🚫 关闭", fmt.Sprintf("feedback_update:id:%d:status:closed", feedback.ID))

	message := msg.Build()

	// 发送给所有管理员
	for _, adminID := range adminIDs {
		if _, err := h.telegram.SendMessage(adminID, message, "HTML", kb.Build()); err != nil {
			log.Printf("[FeedbackHandlerV2] Failed to notify admin %d: %v", adminID, err)
		}
	}
}

// IsInFeedbackProcess 检查是否在反馈流程中
func (h *FeedbackHandlerV2) IsInFeedbackProcess(userID int64) bool {
	sess := h.sessMgr.GetOrCreate(userID)
	step, ok := sess.Get("feedback_step")
	return ok && step == "description"
}

// getStatusIcon 获取状态图标
func (h *FeedbackHandlerV2) getStatusIcon(status string) string {
	switch status {
	case "open":
		return "🔵"
	case "reply":
		return "💬"
	case "processing":
		return "🔧"
	case "fixed":
		return "✅"
	case "closed":
		return "🚫"
	default:
		return "⚪"
	}
}

// getPriorityIcon 获取优先级图标
func (h *FeedbackHandlerV2) getPriorityIcon(priority string) string {
	switch priority {
	case "low":
		return "🟢"
	case "medium":
		return "🟡"
	case "high":
		return "🟠"
	case "urgent":
		return "🔴"
	default:
		return "⚪"
	}
}

// inferIssueType 从标题推断类型
func (h *FeedbackHandlerV2) inferIssueType(title string) string {
	if containsKeyword(title, "画质", "模糊", "马赛克", "分辨率") {
		return "video_quality"
	}
	if containsKeyword(title, "音频", "音质", "音量", "音画不同步") {
		return "audio_quality"
	}
	if containsKeyword(title, "字幕", "翻译") {
		return "subtitle"
	}
	if containsKeyword(title, "搜索", "找不到") {
		return "search"
	}
	if containsKeyword(title, "播放", "卡顿", "无法播放") {
		return "playback"
	}
	return "other"
}

// containsKeyword 检查是否包含关键词
func containsKeyword(text string, keywords ...string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// truncateText 截断文本
func truncateText(text string, maxLen int) string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen]) + "..."
}

// parseInt64 解析 int64
func parseInt64(s string) int64 {
	var i int64
	fmt.Sscanf(s, "%d", &i)
	return i
}
