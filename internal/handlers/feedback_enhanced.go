package handlers

import (
	"encoding/json"
	"fmt"
	"log"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
	"emby-telegram-bot/internal/ui"
)

// FeedbackHandler 反馈处理器
type FeedbackHandler struct {
	sessMgr            *session.SessionManager
	telegram           *services.TelegramService
	feedbackDB         *services.FeedbackDB
	templateService    *services.FeedbackTemplateService
	similarityChecker  *services.SimilarityChecker
	notifyService      *services.NotificationService
}

// NewFeedbackHandler 创建反馈处理器
func NewFeedbackHandler(
	sessMgr *session.SessionManager,
	telegram *services.TelegramService,
	feedbackDB *services.FeedbackDB,
	notifyService *services.NotificationService,
) *FeedbackHandler {
	return &FeedbackHandler{
		sessMgr:           sessMgr,
		telegram:          telegram,
		feedbackDB:        feedbackDB,
		templateService:   services.NewFeedbackTemplateService(),
		similarityChecker: services.NewSimilarityChecker(0.5), // 50% 相似度阈值
		notifyService:     notifyService,
	}
}

// Handle 处理反馈回调
func (h *FeedbackHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	switch ctx.Callback.Action {
	case callback.ActionFeedbackMenu:
		return h.handleMenu(ctx)
	case callback.ActionFeedbackType:
		return h.handleTypeSelect(ctx)
	case callback.ActionFeedbackTemplate:
		return h.handleTemplateSelect(ctx)
	case callback.ActionFeedbackSubmit:
		return h.handleSubmit(ctx)
	case callback.ActionFeedbackList:
		return h.handleList(ctx)
	case callback.ActionFeedbackDetail:
		return h.handleDetail(ctx)
	case callback.ActionFeedbackReply:
		return h.handleReply(ctx)
	case callback.ActionFeedbackStats:
		return h.handleStats(ctx)
	default:
		return h.handleMenu(ctx)
	}
}

// handleMenu 显示反馈菜单
func (h *FeedbackHandler) handleMenu(ctx *callback.Context) (*callback.Response, error) {
	msg := services.NewMessageBuilder()
	msg.Bold("🐛 问题反馈").Newline()
	msg.Newline()
	msg.Text("发现影片播放问题？遇到功能使用障碍？").Newline()
	msg.Text("欢迎通过以下方式向我们反馈：").Newline()
	msg.Newline()
	msg.Bold("📝 反馈方式").Newline()
	msg.Newline()
	msg.Text("1️⃣ 选择问题类型").Newline()
	msg.Text("2️⃣ 填写问题描述").Newline()
	msg.Text("3️⃣ 上传相关截图").Newline()
	msg.Text("4️⃣ 提交反馈").Newline()
	msg.Newline()
	msg.Italic("💡 我们会尽快处理您的反馈！").Newline()

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
		kb.AddButton(it.label, fmt.Sprintf("%s:media_type:%s:media_id:%s", it.callback, "", ""))
	}
	kb.NewRow()

	// 查看我的反馈
	kb.AddButton("📋 我的反馈", "feedback_list")
	kb.NewRow()

	// 返回
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     ctx.Callback.Edit != "",
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleTypeSelect 处理类型选择
func (h *FeedbackHandler) handleTypeSelect(ctx *callback.Context) (*callback.Response, error) {
	// 获取问题类型
	issueType := ctx.Callback.Params["type"]

	// 检查问题类型
	if issueType == "" {
		return h.handleMenu(ctx)
	}

	// 获取该类型的模板
	templates := h.templateService.GetTemplatesByType(issueType)

	if len(templates) == 0 {
		msg := services.NewMessageBuilder()
		msg.Bold("❌ 暂无可用模板").Newline()
		msg.Newline()
		msg.Text("该问题类型暂无可用模板，请手动描述问题。").Newline()
		msg.Newline()
		msg.Italic("您也可以稍后再试。").Newline()

		kb := services.NewKeyboardBuilder()
		kb.AddButton("⬅️ 返回", "feedback_menu")

		return &callback.Response{
			Text:     msg.Build(),
			Edit:     ctx.Callback.Edit != "",
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	msg := services.NewMessageBuilder()
	typeLabel := getIssueTypeLabel(issueType)
	msg.Bold(fmt.Sprintf("📋 %s反馈", typeLabel)).Newline()
	msg.Newline()
	msg.Text("请选择适合您的问题模板：").Newline()
	msg.Newline()

	kb := services.NewKeyboardBuilder()

	// 添加模板按钮
	for i, template := range templates {
		kb.AddButton(fmt.Sprintf("%d. %s", i+1, template.Title), fmt.Sprintf("feedback_template:%s:%s", issueType, template.ID))
		if (i+1)%2 == 0 {
			kb.NewRow()
		}
	}

	// 添加手动描述按钮
	if len(templates)%2 != 0 {
		kb.NewRow()
	}
	kb.AddButton("✏️ 手动描述", fmt.Sprintf("feedback_manual:%s", issueType))
	kb.NewRow()

	// 返回
	kb.AddButton("⬅️ 返回", "feedback_menu")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     ctx.Callback.Edit != "",
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleTemplateSelect 处理模板选择
func (h *FeedbackHandler) handleTemplateSelect(ctx *callback.Context) (*callback.Response, error) {
	// 获取问题类型和模板ID
	issueType := ctx.Callback.Params["type"]
	templateID := ctx.Callback.Params["template"]

	// 检查参数
	if issueType == "" || templateID == "" {
		return h.handleTypeSelect(ctx)
	}

	// 获取模板
	template, exists := h.templateService.GetTemplate(templateID)
	if !exists {
		msg := services.NewMessageBuilder()
		msg.Bold("❌ 模板不存在").Newline()
		msg.Newline()
		msg.Text("请选择其他模板或手动描述问题。").Newline()

		kb := services.NewKeyboardBuilder()
		kb.AddButton("⬅️ 返回", fmt.Sprintf("feedback_type:%s", issueType))

		return &callback.Response{
			Text:     msg.Build(),
			Edit:     ctx.Callback.Edit != "",
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	// 保存模板到会话
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	sess.Set("feedback_template", templateID)
	sess.Set("feedback_type", issueType)
	sess.Set("feedback_fields", template.Fields)

	// 构建模板填写界面
	msg := services.NewMessageBuilder()
	msg.Bold(fmt.Sprintf("📋 %s", template.Title)).Newline()
	msg.Newline()

	for _, field := range template.Fields {
		msg.Text(fmt.Sprintf("• %s", field.Label))
		if field.Required {
			msg.Text(" *必填")
		}
		if field.Type == "select" {
			msg.Text(fmt.Sprintf(" (%s)", formatOptions(field.Options)))
		}
		msg.Newline()
	}

	msg.Newline()
	msg.Bold("请按照以上字段填写问题描述，每行一个字段：").Newline()
	msg.Newline()
	msg.Italic("💡 例如：").Newline()
	msg.Italic(template.Example).Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("✅ 提交反馈", "feedback_submit")
	kb.NewRow()
	kb.AddButton("🖼️ 添加截图", "feedback_image")
	kb.NewRow()
	kb.AddButton("⬅️ 返回", fmt.Sprintf("feedback_type:%s", issueType))

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     ctx.Callback.Edit != "",
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleSubmit 处理反馈提交
func (h *FeedbackHandler) handleSubmit(ctx *callback.Context) (*callback.Response, error) {
	// 获取会话中的反馈信息
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	templateID := sess.GetString("feedback_template")
	issueType := sess.GetString("feedback_type")

	if templateID == "" || issueType == "" {
		return h.handleMenu(ctx)
	}

	// 获取用户输入
	description := ctx.Callback.Params["description"]
	if description == "" {
		description = ctx.Callback.Params["text"] // 支持文本消息
	}

	if description == "" {
		msg := services.NewMessageBuilder()
		msg.Bold("❌ 请先填写问题描述").Newline()
		msg.Newline()
		msg.Text("请按照模板格式填写问题描述。").Newline()

		kb := services.NewKeyboardBuilder()
		kb.AddButton("⬅️ 返回", "feedback_menu")

		return &callback.Response{
			Text:     msg.Build(),
			Edit:     false,
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	// 获取模板
	template, _ := h.templateService.GetTemplate(templateID)

	// 解析字段
	fieldAnswers := parseFieldAnswers(description, template.Fields)

	// 构建格式化描述
	formattedDesc := h.formatDescription(template, fieldAnswers)

	// 检查相似反馈
	similarFeedbacks, _ := h.feedbackDB.FindSimilarFeedbacks(0, issueType, ctx.UserID)
	if len(similarFeedbacks) > 0 {
		// 构建相似反馈提示
		msg := services.NewMessageBuilder()
		msg.Bold("⚠️ 检测到相似问题").Newline()
		msg.Newline()
		msg.Text("发现以下类似的问题，请确认是否重复：").Newline()
		msg.Newline()

		for i, fb := range similarFeedbacks {
			if i >= 3 {
				break
			}
			msg.Text(fmt.Sprintf("%d. %s", i+1, fb.Title))
			msg.Newline()
			msg.Italic(fmt.Sprintf("   %s", truncateString(fb.Description, 50)))
			msg.Newline()
			msg.Newline()
		}

		msg.Italic("如果问题不同，请继续提交。").Newline()

		kb := services.NewKeyboardBuilder()
		kb.AddButton("✅ 继续提交", "feedback_confirm")
		kb.NewRow()
		kb.AddButton("⬅️ 重新填写", fmt.Sprintf("feedback_template:%s:%s", issueType, templateID))

		// 临时保存反馈数据
		sess.Set("pending_feedback", map[string]interface{}{
			"title":          template.Title,
			"description":    formattedDesc,
			"issue_type":     issueType,
			"template_used":  templateID,
			"field_answers":  fieldAnswers,
		})

		return &callback.Response{
			Text:     msg.Build(),
			Edit:     ctx.Callback.Edit != "",
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	// 创建反馈
	feedback := &services.Feedback{
		UserID:       ctx.UserID,
		UserName:     ctx.UserName,
		Title:        template.Title,
		Description:  formattedDesc,
		IssueType:    issueType,
		Priority:     "medium", // 默认中等
		Status:       "open",
		TemplateUsed: templateID,
	}

	// 获取关联的媒体信息
	if mediaID := ctx.Callback.Params["media_id"]; mediaID != "" {
		feedback.MediaID = mediaID
	}
	if mediaTitle := ctx.Callback.Params["media_title"]; mediaTitle != "" {
		feedback.MediaTitle = mediaTitle
	}
	if tmdbIDStr := ctx.Callback.Params["tmdb_id"]; tmdbIDStr != "" {
		var tmdbID int
		fmt.Sscanf(tmdbIDStr, "%d", &tmdbID)
		feedback.TmdbID = tmdbID
	}

	// 添加标签
	if tags, exists := ctx.Callback.Params["tags"]; exists {
		var tagList []string
		json.Unmarshal([]byte(tags), &tagList)
		feedback.Tags = tagList
	}

	// 添加图片
	if images, exists := ctx.Callback.Params["images"]; exists {
		var imageList []string
		json.Unmarshal([]byte(images), &imageList)
		feedback.Images = imageList
	}

	// 保存到数据库
	feedbackID, err := h.feedbackDB.CreateFeedback(feedback)
	if err != nil {
		log.Printf("[FeedbackHandler] Failed to create feedback: %v", err)
		return &callback.Response{
			Text:     "❌ 提交失败，请稍后重试",
			Edit:     false,
			Keyboard: nil,
		}, err
	}

	// 清理会话数据
	sess.Delete("feedback_template")
	sess.Delete("feedback_type")
	sess.Delete("feedback_fields")

	// 构建成功消息
	msg := services.NewMessageBuilder()
	msg.Bold("✅ 反馈提交成功").Newline()
	msg.Newline()
	msg.Text(fmt.Sprintf("反馈编号：#%d", feedbackID)).Newline()
	msg.Text(fmt.Sprintf("问题类型：%s", getIssueTypeLabel(issueType))).Newline()
	msg.Newline()
	msg.Italic("感谢您的反馈！我们会尽快处理。").Newline()
	msg.Italic("如有回复，我们会第一时间通知您。").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("📋 查看反馈", fmt.Sprintf("feedback_detail:%d", feedbackID))
	kb.NewRow()
	kb.AddButton("⬅️ 返回", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     ctx.Callback.Edit != "",
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// formatDescription 格式化描述
func (h *FeedbackHandler) formatDescription(template *services.FeedbackTemplate, answers map[string]string) string {
	var desc strings.Builder

	desc.WriteString(fmt.Sprintf("【%s】%s\n", getIssueTypeLabel(template.Type), template.Title))
	desc.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	for _, field := range template.Fields {
		answer := answers[field.ID]
		if answer == "" {
			answer = "未填写"
		}
		desc.WriteString(fmt.Sprintf("• %s：%s\n", field.Label, answer))
	}

	return desc.String()
}

// parseFieldAnswers 解析字段答案
func parseFieldAnswers(description string, fields []services.Field) map[string]string {
	answers := make(map[string]string)

	lines := strings.Split(description, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// 尝试匹配字段格式：• label ： answer
		for _, field := range fields {
			if strings.HasPrefix(line, "• "+field.Label+"：") || strings.HasPrefix(line, "• "+field.Label+":") {
				answer := strings.TrimPrefix(line, "• "+field.Label+"：")
				answer = strings.TrimPrefix(answer, "• "+field.Label+":")
				answers[field.ID] = strings.TrimSpace(answer)
				break
			}
		}
	}

	return answers
}

// 其他处理方法...（handleList, handleDetail, handleReply, handleStats）
