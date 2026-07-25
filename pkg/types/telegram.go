package types

// TelegramUpdate represents an incoming update from Telegram
type TelegramUpdate struct {
	UpdateID      int64                  `json:"update_id"`
	Message       *TelegramMessage       `json:"message,omitempty"`
	CallbackQuery *TelegramCallbackQuery `json:"callback_query,omitempty"`
	EditedMessage *TelegramMessage       `json:"edited_message,omitempty"`
}

// TelegramMessage represents a message
type TelegramMessage struct {
	MessageID            int64                         `json:"message_id"`
	MessageThreadID      int64                         `json:"message_thread_id,omitempty"`
	ReceiverUser         *TelegramUser                 `json:"receiver_user,omitempty"`
	EphemeralMessageID   int64                         `json:"ephemeral_message_id,omitempty"`
	From                 *TelegramUser                 `json:"from"`
	Chat                 *TelegramChat                 `json:"chat"`
	Date                 int64                         `json:"date"`
	Text                 string                        `json:"text,omitempty"`
	Caption              string                        `json:"caption,omitempty"`
	Photo                []*TelegramPhotoSize          `json:"photo,omitempty"`
	CommunityChatAdded   *TelegramCommunityChatAdded   `json:"community_chat_added,omitempty"`
	CommunityChatRemoved *TelegramCommunityChatRemoved `json:"community_chat_removed,omitempty"`
	ReplyToMessage       *TelegramMessage              `json:"reply_to_message,omitempty"`
	ForwardFrom          *TelegramUser                 `json:"forward_from,omitempty"`   // 转发自用户
	ForwardOrigin        *TelegramUser                 `json:"forward_origin,omitempty"` // TG API 7.0+ sender_user
}

// TelegramCallbackQuery represents a callback query
type TelegramCallbackQuery struct {
	ID      string           `json:"id"`
	From    *TelegramUser    `json:"from"`
	Message *TelegramMessage `json:"message,omitempty"`
	Data    string           `json:"data,omitempty"`
}

// TelegramUser represents a user
type TelegramUser struct {
	ID           int64  `json:"id"`
	FirstName    string `json:"first_name,omitempty"`
	LastName     string `json:"last_name,omitempty"`
	Username     string `json:"username,omitempty"`
	LanguageCode string `json:"language_code,omitempty"`
	IsBot        bool   `json:"is_bot,omitempty"`
}

// TelegramCommunity represents a Telegram Community (a linked group of chats).
type TelegramCommunity struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// TelegramCommunityChatAdded is emitted when a chat joins a Community.
type TelegramCommunityChatAdded struct {
	Community TelegramCommunity `json:"community"`
}

// TelegramCommunityChatRemoved is emitted when a chat leaves a Community.
type TelegramCommunityChatRemoved struct{}

// TelegramChat represents a chat
type TelegramChat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title,omitempty"`
	Username string `json:"username,omitempty"`
}

// TelegramPhotoSize represents a photo size
type TelegramPhotoSize struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size,omitempty"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

// TelegramInlineKeyboard represents an inline keyboard markup
type TelegramInlineKeyboard struct {
	InlineKeyboard [][]TelegramInlineKeyboardButton `json:"inline_keyboard"`
}

// TelegramInlineKeyboardButton represents a button
type TelegramInlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}

// SendMessageRequest represents a send message request
type SendMessageRequest struct {
	ChatID          int64                    `json:"chat_id"`
	Text            string                   `json:"text"`
	ParseMode       string                   `json:"parse_mode,omitempty"`
	ReplyMarkup     *TelegramInlineKeyboard  `json:"reply_markup,omitempty"`
	ReceiverUserID  int64                    `json:"receiver_user_id,omitempty"`
	CallbackQueryID string                   `json:"callback_query_id,omitempty"`
	ReplyParameters *TelegramReplyParameters `json:"reply_parameters,omitempty"`
}

// TelegramReplyParameters describes the message being replied to. MessageID is
// optional when EphemeralMessageID identifies an incoming ephemeral message.
type TelegramReplyParameters struct {
	MessageID          int64 `json:"message_id,omitempty"`
	EphemeralMessageID int64 `json:"ephemeral_message_id,omitempty"`
}

// TelegramSendOptions contains optional fields shared by plain and rich sends.
// ReceiverUserID targets an ephemeral message; CallbackQueryID or an ephemeral
// reply target is required for non-administrator bots.
type TelegramSendOptions struct {
	ReceiverUserID  int64                    `json:"receiver_user_id,omitempty"`
	CallbackQueryID string                   `json:"callback_query_id,omitempty"`
	MessageThreadID int64                    `json:"message_thread_id,omitempty"`
	ReplyParameters *TelegramReplyParameters `json:"reply_parameters,omitempty"`
}

// EditMessageRequest represents an edit message request
type EditMessageRequest struct {
	ChatID      int64                   `json:"chat_id"`
	MessageID   int64                   `json:"message_id"`
	Text        string                  `json:"text"`
	ParseMode   string                  `json:"parse_mode,omitempty"`
	ReplyMarkup *TelegramInlineKeyboard `json:"reply_markup,omitempty"`
}

// AnswerCallbackRequest represents an answer callback query request
type AnswerCallbackRequest struct {
	CallbackQueryID string `json:"callback_query_id"`
	Text            string `json:"text,omitempty"`
	ShowAlert       bool   `json:"show_alert,omitempty"`
}

// TelegramAPIResponse represents a generic Telegram API response
type TelegramAPIResponse struct {
	OK     bool           `json:"ok"`
	Result any            `json:"result,omitempty"`
	Error  *TelegramError `json:"error,omitempty"`
}

// TelegramError represents a Telegram API error
type TelegramError struct {
	Code    int    `json:"error_code"`
	Message string `json:"description"`
}

func (e *TelegramError) Error() string {
	return e.Message
}
