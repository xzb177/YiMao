package services

import (
	"fmt"
	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
	"regexp"
	"strconv"
	"strings"
)

func (s *WebhookService) HandleJellyseerrWebhook(payload JellyseerrWebhookPayload) error {
	logger.Info("[Webhook] Jellyseerr event: %s", payload.Event)

	switch payload.Event {
	case "request_created":
		return s.handleRequestCreated(payload)
	case "request_approved":
		return s.handleRequestApproved(payload)
	case "request_declined":
		return s.handleRequestDeclined(payload)
	case "request_available":
		return s.handleRequestAvailable(payload)
	case "issue_created":
		return s.handleIssueCreated(payload)
	case "issue_comment":
		return s.handleIssueComment(payload)
	case "issue_resolved":
		return s.handleIssueResolved(payload)
	case "test":
		return s.handleJellyseerrTest(payload)
	default:
		logger.Info("[Webhook] Unknown event: %s", payload.Event)
		return nil
	}
}

// handleRequestCreated handles new request event
func (s *WebhookService) handleRequestCreated(payload JellyseerrWebhookPayload) error {
	if payload.Media == nil || payload.User == nil {
		return nil
	}

	// Find user's Telegram ID
	telegramID, exists := s.userMapping.GetTelegramIDByJellyseerrID(payload.User.ID)
	if !exists {
		logger.Info("[Webhook] No Telegram mapping for Jellyseerr user %d", payload.User.ID)
		// Still notify admins
		return s.notifyAdminsAboutRequest(payload)
	}

	// Check user preferences
	if !s.preferences.ShouldNotify(telegramID, PrefApproveNotification, payload.Media.Title) {
		return nil
	}

	// Send confirmation to user
	message := s.formatRequestCreatedMessage(payload)
	if _, err := s.telegram.SendMessage(telegramID, message, "", nil); err != nil {
		logger.Info("[Webhook] Failed to send request created message: %v", err)
	}

	// Notify admins
	return s.notifyAdminsAboutRequest(payload)
}

// formatRequestCreatedMessage formats a request created message
func (s *WebhookService) formatRequestCreatedMessage(payload JellyseerrWebhookPayload) string {
	mediaType := "电影"
	if payload.Media.MediaType == "tv" {
		mediaType = "剧集"
	}

	return fmt.Sprintf("🎬 新求片请求\n\n%s\n%s\n\n✅ 请求已提交，等待管理员处理",
		payload.Media.Title,
		mediaType)
}

// notifyAdminsAboutRequest notifies all admins about a new request
func (s *WebhookService) notifyAdminsAboutRequest(payload JellyseerrWebhookPayload) error {
	adminIDs := s.adminService.GetAdminIDs()

	if len(adminIDs) == 0 {
		return nil
	}

	mediaType := "电影"
	if payload.Media != nil && payload.Media.MediaType == "tv" {
		mediaType = "剧集"
	}

	title := payload.Subject
	if payload.Media != nil {
		title = payload.Media.Title
	}

	message := fmt.Sprintf("🎬 新求片请求\n\n%s\n%s", title, mediaType)

	username := "未知用户"
	if payload.User != nil {
		username = payload.User.Username
	}

	requestID := ""
	if payload.Request != nil {
		requestID = strconv.Itoa(payload.Request.ID)
	}

	// Add action buttons for each admin
	for _, adminID := range adminIDs {
		keyboard := [][]map[string]string{
			{
				{"text": "✅ 批准", "callback_data": fmt.Sprintf("admin_approve:id:%s", requestID)},
				{"text": "❌ 拒绝", "callback_data": fmt.Sprintf("admin_decline:id:%s", requestID)},
			},
		}

		fullMessage := fmt.Sprintf("%s\n\n👤 用户: %s", message, username)
		if _, err := s.telegram.SendMessage(adminID, fullMessage, "", convertToInlineKeyboard(keyboard)); err != nil {
			logger.Info("[Webhook] Failed to notify admin %d: %v", adminID, err)
		}
	}

	return nil
}

// handleRequestApproved handles request approved event
func (s *WebhookService) handleRequestApproved(payload JellyseerrWebhookPayload) error {
	if payload.Media == nil || payload.User == nil {
		return nil
	}

	telegramID, exists := s.userMapping.GetTelegramIDByJellyseerrID(payload.User.ID)
	if !exists {
		return nil
	}

	if !s.preferences.ShouldNotify(telegramID, PrefApproveNotification, payload.Media.Title) {
		return nil
	}

	message := fmt.Sprintf("✅ 请求已批准\n\n%s\n\n🎬 正在处理中，完成后会通知你", payload.Media.Title)
	_, err := s.telegram.SendMessage(telegramID, message, "", nil)
	return err
}

// handleRequestDeclined handles request declined event
func (s *WebhookService) handleRequestDeclined(payload JellyseerrWebhookPayload) error {
	if payload.Media == nil || payload.User == nil {
		return nil
	}

	telegramID, exists := s.userMapping.GetTelegramIDByJellyseerrID(payload.User.ID)
	if !exists {
		return nil
	}

	if !s.preferences.ShouldNotify(telegramID, PrefAvailableNotification, payload.Media.Title) {
		return nil
	}

	// Check if media already exists
	existsMsg := ""
	if payload.Media.Status == "available" {
		existsMsg = "\n\n💡 这部电影已经在库中了，可以直接观看 🎬"
	} else if payload.Media.Status == "processing" {
		existsMsg = "\n\n💡 这部电影正在处理中，请耐心等待"
	}

	message := fmt.Sprintf("❌ 请求已拒绝\n\n%s%s", payload.Media.Title, existsMsg)
	_, err := s.telegram.SendMessage(telegramID, message, "", nil)
	return err
}

// handleRequestAvailable handles request available event
func (s *WebhookService) handleRequestAvailable(payload JellyseerrWebhookPayload) error {
	if payload.Media == nil || payload.User == nil {
		return nil
	}

	telegramID, exists := s.userMapping.GetTelegramIDByJellyseerrID(payload.User.ID)
	if !exists {
		return nil
	}

	if !s.preferences.ShouldNotify(telegramID, PrefAvailableNotification, payload.Media.Title) {
		return nil
	}

	// Mention user to get their attention
	username := s.userMapping.GetTelegramUsername(telegramID)
	if username == "" {
		username = "用户"
	}

	message := fmt.Sprintf("🎉 内容已可用！\n\n%s\n\n@%s 快来观看吧！", payload.Media.Title, username)
	_, err := s.telegram.SendMessage(telegramID, message, "", nil)
	return err
}

// handleIssueCreated handles issue created event
func (s *WebhookService) handleIssueCreated(payload JellyseerrWebhookPayload) error {
	if payload.Issue == nil {
		return nil
	}

	issueID := payload.Issue.ID
	if issueID == 0 {
		// Try to get issue ID from subject/message
		re := regexp.MustCompile(`Issue #(\d+)`)
		matches := re.FindStringSubmatch(payload.Subject + " " + payload.Message)
		if len(matches) > 1 {
			if id, err := strconv.ParseInt(matches[1], 10, 64); err == nil {
				issueID = id
			}
		}
	}

	// Get user info
	var userID int64
	var username string
	if payload.User != nil {
		userID = payload.User.ID
		username = payload.User.Username
	}

	// Get Telegram ID
	telegramID, exists := s.userMapping.GetTelegramIDByJellyseerrID(userID)
	if !exists {
		logger.Info("[Webhook] No Telegram mapping for user %d", userID)
	}

	// Notify admins
	return s.notifyAdminsAboutIssue(issueID, payload, telegramID, username)
}

// notifyAdminsAboutIssue notifies admins about a new issue
func (s *WebhookService) notifyAdminsAboutIssue(issueID int64, payload JellyseerrWebhookPayload, telegramID int64, username string) error {
	adminIDs := s.adminService.GetAdminIDs()

	if len(adminIDs) == 0 {
		return nil
	}

	// Determine priority emoji
	priorityEmoji := "🐛"
	if strings.Contains(strings.ToLower(payload.Subject), "audio") {
		priorityEmoji = "🔊"
	} else if strings.Contains(strings.ToLower(payload.Subject), "subtitle") {
		priorityEmoji = "💬"
	} else if strings.Contains(strings.ToLower(payload.Subject), "video") {
		priorityEmoji = "🎬"
	}

	message := fmt.Sprintf("%s 问题报告\n\n%s\n\n👉 用户: %s", priorityEmoji, payload.Subject, username)

	if telegramID != 0 {
		tgUsername := s.userMapping.GetTelegramUsername(telegramID)
		if tgUsername != "" {
			message += fmt.Sprintf(" (@%s)", tgUsername)
		}
	}

	if payload.Issue != nil && payload.Issue.Problem != "" {
		message += fmt.Sprintf("\n\n📝 问题: %s", payload.Issue.Problem)
	}

	// Add action buttons
	keyboard := [][]map[string]string{
		{
			{"text": "💬 回复", "callback_data": fmt.Sprintf("admin_issue_reply:id:%d", issueID)},
			{"text": "✅ 已修复", "callback_data": fmt.Sprintf("admin_issue_fixed:id:%d", issueID)},
		},
		{
			{"text": "ℹ️ 处理中", "callback_data": fmt.Sprintf("admin_issue_processing:id:%d", issueID)},
			{"text": "❌ 关闭", "callback_data": fmt.Sprintf("admin_issue_close:id:%d", issueID)},
		},
	}

	for _, adminID := range adminIDs {
		if _, err := s.telegram.SendMessage(adminID, message, "", convertToInlineKeyboard(keyboard)); err != nil {
			logger.Info("[Webhook] Failed to notify admin %d about issue: %v", adminID, err)
		}
	}

	return nil
}

// handleIssueComment handles new comment on issue
func (s *WebhookService) handleIssueComment(payload JellyseerrWebhookPayload) error {
	issueID := int64(0)
	if payload.Issue != nil {
		issueID = payload.Issue.ID
	}
	message := fmt.Sprintf("💬 Jellyseerr 问题有新评论\n\n%s", payload.Subject)
	if payload.Message != "" {
		message += fmt.Sprintf("\n\n📝 %s", payload.Message)
	}
	if payload.User != nil && payload.User.Username != "" {
		message += fmt.Sprintf("\n\n👤 评论人: %s", payload.User.Username)
	}

	var keyboard *types.TelegramInlineKeyboard
	if issueID != 0 {
		keyboard = convertToInlineKeyboard([][]map[string]string{{
			{"text": "💬 回复", "callback_data": fmt.Sprintf("admin_issue_reply:id:%d", issueID)},
			{"text": "✅ 已修复", "callback_data": fmt.Sprintf("admin_issue_fixed:id:%d", issueID)},
		}})
	}

	for _, adminID := range s.adminService.GetAdminIDs() {
		if _, err := s.telegram.SendMessage(adminID, message, "", keyboard); err != nil {
			logger.Info("[Webhook] Failed to notify admin %d about issue comment: %v", adminID, err)
		}
	}
	return nil
}

// handleIssueResolved handles issue resolved event
func (s *WebhookService) handleIssueResolved(payload JellyseerrWebhookPayload) error {
	message := fmt.Sprintf("✅ Jellyseerr 问题已解决\n\n%s", payload.Subject)
	if payload.Media != nil && payload.Media.Title != "" {
		message += fmt.Sprintf("\n\n🎬 %s", payload.Media.Title)
	}
	if payload.User != nil && payload.User.Username != "" {
		message += fmt.Sprintf("\n👤 %s", payload.User.Username)
	}

	if payload.User != nil {
		if telegramID, ok := s.userMapping.GetTelegramIDByJellyseerrID(payload.User.ID); ok {
			if _, err := s.telegram.SendMessage(telegramID, message, "", nil); err != nil {
				logger.Info("[Webhook] Failed to notify user %d about issue resolved: %v", telegramID, err)
			}
		}
	}

	for _, adminID := range s.adminService.GetAdminIDs() {
		if _, err := s.telegram.SendMessage(adminID, message, "", nil); err != nil {
			logger.Info("[Webhook] Failed to notify admin %d about issue resolved: %v", adminID, err)
		}
	}
	return nil
}

// handleJellyseerrTest handles test webhook from Jellyseerr
func (s *WebhookService) handleJellyseerrTest(payload JellyseerrWebhookPayload) error {
	message := "🔔 测试通知\n\nJellyseerr 连接正常！"

	if s.chatID != 0 {
		_, err := s.telegram.SendMessage(s.chatID, message, "", nil)
		return err
	}

	return fmt.Errorf("no chat ID configured")
}
