package services

import (
	"github.com/xzb177/yimao/internal/richmessage"
	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
)

// RichMessageService provides rich message functionality using TelegramClient
type RichMessageService struct {
	telegram *TelegramClient
}

// NewRichMessageService creates a new rich message service
func NewRichMessageService(telegram *TelegramClient) *RichMessageService {
	return &RichMessageService{
		telegram: telegram,
	}
}

// SendMediaInfoCard sends a media info card using Rich Message
func (s *RichMessageService) SendMediaInfoCard(chatID int64, info richmessage.MediaInfo, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	msg := richmessage.BuildMediaInfoCard(info)
	
	logger.Info("[RichMessage] Sending media info card: %s", info.Title)
	return s.telegram.SendRichMessage(chatID, msg.Markdown, keyboard)
}

// SendSubscriptionDashboard sends a subscription dashboard using Rich Message
func (s *RichMessageService) SendSubscriptionDashboard(chatID int64, subs []richmessage.SubscriptionStatus, todayAdded, weekDownload int, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	msg := richmessage.BuildSubscriptionDashboard(subs, todayAdded, weekDownload)
	
	logger.Info("[RichMessage] Sending subscription dashboard")
	return s.telegram.SendRichMessage(chatID, msg.Markdown, keyboard)
}

// SendCustomRichMessage sends a custom rich message
func (s *RichMessageService) SendCustomRichMessage(chatID int64, builder *richmessage.Builder, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	msg := builder.Build()
	
	logger.Info("[RichMessage] Sending custom rich message")
	return s.telegram.SendRichMessage(chatID, msg.Markdown, keyboard)
}
