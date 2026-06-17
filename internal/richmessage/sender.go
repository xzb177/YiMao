package richmessage

// RichMessageSender sends rich messages via Telegram Bot API
type RichMessageSender struct {
	BotToken string
	ChatID   int64
}

// NewRichMessageSender creates a new sender
func NewRichMessageSender(botToken string, chatID int64) *RichMessageSender {
	return &RichMessageSender{
		BotToken: botToken,
		ChatID:   chatID,
	}
}

// SendMediaInfoCard sends a media info card
func (s *RichMessageSender) SendMediaInfoCard(info MediaInfo) error {
	msg := BuildMediaInfoCard(info)
	return SendRichMessage(s.BotToken, s.ChatID, msg)
}

// SendSubscriptionDashboard sends a subscription dashboard
func (s *RichMessageSender) SendSubscriptionDashboard(subs []SubscriptionStatus, todayAdded, weekDownload int) error {
	msg := BuildSubscriptionDashboard(subs, todayAdded, weekDownload)
	return SendRichMessage(s.BotToken, s.ChatID, msg)
}

// SendCustomRichMessage sends a custom rich message
func (s *RichMessageSender) SendCustomRichMessage(msg RichMessage) error {
	return SendRichMessage(s.BotToken, s.ChatID, msg)
}
