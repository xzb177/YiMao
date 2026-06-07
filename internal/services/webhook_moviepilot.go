package services

import (
	"fmt"
	"github.com/xzb177/yimao/pkg/logger"
)

func (s *WebhookService) HandleMoviePilotWebhook(payload MoviePilotWebhookPayload) error {
	logger.Info("[Webhook] MoviePilot event: %s, item: %s (user: %s)", payload.Event, payload.Data.Name, payload.Data.Username)

	switch payload.Event {
	case "subscribe":
		return s.handleMoviePilotSubscribe(payload)
	case "download":
		return s.handleMoviePilotDownload(payload)
	case "complete":
		return s.handleMoviePilotComplete(payload)
	default:
		logger.Info("[Webhook] Unknown MoviePilot event: %s", payload.Event)
		return nil
	}
}

// handleMoviePilotSubscribe handles new subscription event
func (s *WebhookService) handleMoviePilotSubscribe(payload MoviePilotWebhookPayload) error {
	// Format media type
	mediaType := "电影"
	if payload.Data.Type == "电视剧" {
		mediaType = "剧集"
	}

	// Build message
	message := fmt.Sprintf("🎬 新求片请求\n\n%s", payload.Data.Name)

	// Add year if available
	if payload.Data.Year != "" && payload.Data.Year != "0" {
		message += fmt.Sprintf(" (%s)", payload.Data.Year)
	}

	// Add season for TV shows
	if payload.Data.Type == "电视剧" && payload.Data.Season > 0 {
		message += fmt.Sprintf("\n📺 季数: 第%d季", payload.Data.Season)
	}

	// Add status
	statusText := GetStateText(payload.Data.State)
	message += fmt.Sprintf("\n\n%s\n%s", mediaType, statusText)

	// Try to find user's Telegram ID by MoviePilot username
	var userTelegramID int64
	if payload.Data.Username != "" && s.userMapping != nil {
		// Note: If lookup fails, userTelegramID stays 0, and message won't be sent (acceptable)
		userTelegramID, _ = s.userMapping.GetTelegramIDByMoviePilotUsername(payload.Data.Username)
	}

	// Send confirmation to the requesting user
	if userTelegramID != 0 {
		userMessage := message
		userMessage += fmt.Sprintf("\n\n✅ 您的请求已提交，等待管理员处理")
		s.telegram.SendMessage(userTelegramID, userMessage, "", nil)
	}

	// Notify admins (without the username since they know who requested)
	adminIDs := s.adminService.GetAdminIDs()
	if len(adminIDs) == 0 {
		return nil
	}

	adminMessage := message
	if payload.Data.Username != "" {
		adminMessage += fmt.Sprintf("\n👤 用户: %s", payload.Data.Username)
	}

	for _, adminID := range adminIDs {
		s.sendWithCache(adminID, adminMessage)
	}

	return nil
}

// handleMoviePilotDownload handles download started event
func (s *WebhookService) handleMoviePilotDownload(payload MoviePilotWebhookPayload) error {
	mediaType := "电影"
	if payload.Data.Type == "电视剧" {
		mediaType = "剧集"
	}

	message := fmt.Sprintf("📥 开始下载\n\n%s", payload.Data.Name)

	if payload.Data.Year != "" && payload.Data.Year != "0" {
		message += fmt.Sprintf(" (%s)", payload.Data.Year)
	}

	if payload.Data.Type == "电视剧" && payload.Data.Season > 0 {
		message += fmt.Sprintf("\n📺 第%d季", payload.Data.Season)
	}

	message += fmt.Sprintf("\n\n%s", mediaType)

	// Try to find user's Telegram ID by MoviePilot username
	var userTelegramID int64
	if payload.Data.Username != "" && s.userMapping != nil {
		// Note: If lookup fails, userTelegramID stays 0, and message won't be sent (acceptable)
		userTelegramID, _ = s.userMapping.GetTelegramIDByMoviePilotUsername(payload.Data.Username)
	}

	// Send to the requesting user
	if userTelegramID != 0 {
		s.sendWithCache(userTelegramID, message)
	}

	return nil
}

// handleMoviePilotComplete handles download complete event
func (s *WebhookService) handleMoviePilotComplete(payload MoviePilotWebhookPayload) error {
	mediaType := "电影"
	if payload.Data.Type == "电视剧" {
		mediaType = "剧集"
	}

	message := fmt.Sprintf("✅ 下载完成\n\n%s", payload.Data.Name)

	if payload.Data.Year != "" && payload.Data.Year != "0" {
		message += fmt.Sprintf(" (%s)", payload.Data.Year)
	}

	if payload.Data.Type == "电视剧" {
		if payload.Data.TotalEpisode > 0 {
			message += fmt.Sprintf("\n📺 共 %d 集", payload.Data.TotalEpisode)
		}
		if payload.Data.Season > 0 {
			message += fmt.Sprintf(" (第%d季)", payload.Data.Season)
		}
	}

	message += fmt.Sprintf("\n\n%s", mediaType)

	// Try to find user's Telegram ID by MoviePilot username
	var userTelegramID int64
	if payload.Data.Username != "" && s.userMapping != nil {
		// Note: If lookup fails, userTelegramID stays 0, and message won't be sent (acceptable)
		userTelegramID, _ = s.userMapping.GetTelegramIDByMoviePilotUsername(payload.Data.Username)
	}

	// Send to the requesting user
	if userTelegramID != 0 {
		s.sendWithCache(userTelegramID, message)
	}

	return nil
}
