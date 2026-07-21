package handlers

import (
	"strings"

	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/pkg/types"
)

// userScopedSender keeps personal output private when a workflow runs in a
// group. Telegram private chat IDs equal the user's ID; every other chat is
// treated as a group scope and receives an ephemeral sendMessage target.
type userScopedSender struct {
	telegram *services.TelegramClient
	chatID   int64
	userID   int64
	group    bool
}

func newUserScopedSender(telegram *services.TelegramClient, chatID, userID int64) *userScopedSender {
	return &userScopedSender{
		telegram: telegram,
		chatID:   chatID,
		userID:   userID,
		group:    chatID < 0,
	}
}

func (s *userScopedSender) SendMessage(text, parseMode string, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	if !s.group {
		return s.telegram.SendMessage(s.chatID, text, parseMode, keyboard)
	}
	return s.telegram.SendMessage(s.chatID, text, parseMode, keyboard, &types.TelegramSendOptions{ReceiverUserID: s.userID})
}

// SendRichMessage preserves Rich Message in private chats. Bot API 10.2 does
// not document ephemeral sendRichMessage, so groups receive a Markdown
// sendMessage fallback, then plain text if Telegram rejects the Markdown.
func (s *userScopedSender) SendRichMessage(markdown string, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	if !s.group {
		return s.telegram.SendRichMessage(s.chatID, markdown, keyboard)
	}
	msg, err := s.SendMessage(markdown, "Markdown", keyboard)
	if err == nil {
		return msg, nil
	}
	return s.SendMessage(stripMarkdown(markdown), "", keyboard)
}

func (s *userScopedSender) SendStructuredRichMessage(rich *types.TelegramInputRichMessage, fallbackText string, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	if !s.group {
		return s.telegram.SendStructuredRichMessage(s.chatID, rich, keyboard)
	}
	if strings.TrimSpace(fallbackText) == "" {
		fallbackText = "当前内容请在私聊中查看"
	}
	return s.SendMessage(fallbackText, "Markdown", keyboard)
}

func (s *userScopedSender) DeleteMessage(msg *types.TelegramMessage) error {
	if msg == nil {
		return nil
	}
	if s.group && msg.EphemeralMessageID != 0 {
		return s.telegram.DeleteEphemeralMessage(s.chatID, s.userID, msg.EphemeralMessageID)
	}
	if msg.MessageID == 0 {
		return nil
	}
	return s.telegram.DeleteMessage(s.chatID, msg.MessageID)
}

func stripMarkdown(text string) string {
	replacer := strings.NewReplacer("**", "", "__", "", "`", "", "~~", "")
	return replacer.Replace(text)
}
