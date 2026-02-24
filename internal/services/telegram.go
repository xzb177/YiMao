package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"emby-telegram-bot/pkg/logger"
	"emby-telegram-bot/pkg/types"
)

// TelegramClient provides access to Telegram Bot API
type TelegramClient struct {
	botToken   string
	httpClient *http.Client
	baseURL    string
}

// NewTelegramClient creates a new Telegram client
func NewTelegramClient(botToken string) *TelegramClient {
	return &TelegramClient{
		botToken: botToken,
		baseURL:  fmt.Sprintf("https://api.telegram.org/bot%s", botToken),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SendMessage sends a message to a chat
func (c *TelegramClient) SendMessage(chatID int64, text string, parseMode string, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	apiURL := fmt.Sprintf("%s/sendMessage", c.baseURL)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": parseMode,
	}

	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}

	return c.makeRequest(apiURL, payload)
}

// EditMessage edits an existing message
func (c *TelegramClient) EditMessage(chatID int64, messageID int64, text string, parseMode string, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	apiURL := fmt.Sprintf("%s/editMessageText", c.baseURL)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
		"parse_mode": parseMode,
	}

	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}

	return c.makeRequest(apiURL, payload)
}

// DeleteMessage deletes a message
func (c *TelegramClient) DeleteMessage(chatID int64, messageID int64) error {
	apiURL := fmt.Sprintf("%s/deleteMessage", c.baseURL)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
	}

	_, err := c.makeRequest(apiURL, payload)
	return err
}

// AnswerCallback answers a callback query
func (c *TelegramClient) AnswerCallback(callbackID string, text string, showAlert bool) error {
	apiURL := fmt.Sprintf("%s/answerCallbackQuery", c.baseURL)

	payload := map[string]interface{}{
		"callback_query_id": callbackID,
		"show_alert":       showAlert,
	}

	// Only include text if it's not empty
	if text != "" {
		payload["text"] = text
	}

	return c.makeSimpleRequest(apiURL, payload)
}

// SendPhoto sends a photo with caption to a chat
func (c *TelegramClient) SendPhoto(chatID int64, photoURL, caption string, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	apiURL := fmt.Sprintf("%s/sendPhoto", c.baseURL)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"photo":      photoURL,
		"caption":    caption,
		"parse_mode": "Markdown",
	}

	// Add keyboard if provided
	if keyboard != nil && len(keyboard.InlineKeyboard) > 0 {
		payload["reply_markup"] = keyboard
	}

	return c.makeRequest(apiURL, payload)
}

// SetWebhook sets the webhook URL
func (c *TelegramClient) SetWebhook(webhookURL string) error {
	apiURL := fmt.Sprintf("%s/setWebhook", c.baseURL)

	payload := map[string]interface{}{
		"url": webhookURL,
	}

	_, err := c.makeRequest(apiURL, payload)
	return err
}

// DeleteWebhook deletes the webhook
func (c *TelegramClient) DeleteWebhook() error {
	apiURL := fmt.Sprintf("%s/deleteWebhook", c.baseURL)

	return c.makeSimpleRequest(apiURL, nil)
}

// GetUpdates fetches updates from Telegram (for polling)
func (c *TelegramClient) GetUpdates(offset int, limit int) ([]types.TelegramUpdate, error) {
	apiURL := fmt.Sprintf("%s/getUpdates?offset=%d&limit=%d&timeout=10", c.baseURL, offset, limit)

	resp, err := c.httpClient.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var response struct {
		OK     bool                     `json:"ok"`
		Result []types.TelegramUpdate `json:"result"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	if !response.OK {
		return nil, fmt.Errorf("Telegram API error: %s", string(body))
	}

	return response.Result, nil
}

// GetWebhookInfo gets webhook info
func (c *TelegramClient) GetWebhookInfo() (map[string]interface{}, error) {
	apiURL := fmt.Sprintf("%s/getWebhookInfo", c.baseURL)

	resp, err := c.httpClient.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		OK     bool                      `json:"ok"`
		Result map[string]interface{}    `json:"result"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if !result.OK {
		return nil, fmt.Errorf("telegram API error")
	}

	return result.Result, nil
}

// makeRequest makes a generic API request
func (c *TelegramClient) makeRequest(apiURL string, payload map[string]interface{}) (*types.TelegramMessage, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	log.Printf("[Telegram] Request: %s, payload: %s", logger.Sanitize(apiURL), logger.SanitizePayload(jsonData))

	resp, err := c.httpClient.Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	log.Printf("[Telegram] Response: %s", logger.Sanitize(string(body)))

	var result struct {
		OK      bool                  `json:"ok"`
		Result  *types.TelegramMessage `json:"result"`
		Error   *types.TelegramError  `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.OK {
		if result.Error != nil {
			return nil, result.Error
		}
		return nil, fmt.Errorf("telegram API error: %s", string(body))
	}

	return result.Result, nil
}

// makeSimpleRequest makes an API request that returns a boolean result (like answerCallbackQuery)
func (c *TelegramClient) makeSimpleRequest(apiURL string, payload map[string]interface{}) error {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	log.Printf("[Telegram] Request: %s, payload: %s", logger.Sanitize(apiURL), logger.SanitizePayload(jsonData))

	resp, err := c.httpClient.Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	log.Printf("[Telegram] Response: %s", logger.Sanitize(string(body)))

	var result struct {
		OK     bool                 `json:"ok"`
		Result bool                 `json:"result"`
		Error  *types.TelegramError `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.OK {
		if result.Error != nil {
			return result.Error
		}
		return fmt.Errorf("telegram API error: %s", string(body))
	}

	return nil
}

// BotCommand represents a bot command in the menu
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// SetMyCommands sets the bot's command menu
func (c *TelegramClient) SetMyCommands(commands []BotCommand, languageCode string) error {
	apiURL := fmt.Sprintf("%s/setMyCommands", c.baseURL)

	payload := map[string]interface{}{
		"commands": commands,
	}

	if languageCode != "" {
		payload["language_code"] = languageCode
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	log.Printf("[Telegram] Setting bot commands")

	resp, err := c.httpClient.Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	log.Printf("[Telegram] Response: %s", logger.Sanitize(string(body)))

	var result struct {
		OK     bool                 `json:"ok"`
		Result bool                 `json:"result"`
		Error  *types.TelegramError `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.OK {
		if result.Error != nil {
			return result.Error
		}
		return fmt.Errorf("telegram API error: %s", string(body))
	}

	return nil
}

// KeyboardBuilder helps build inline keyboards
type KeyboardBuilder struct {
	buttons [][]types.TelegramInlineKeyboardButton
	currentRow []types.TelegramInlineKeyboardButton
}

// NewKeyboardBuilder creates a new keyboard builder
func NewKeyboardBuilder() *KeyboardBuilder {
	return &KeyboardBuilder{
		buttons: make([][]types.TelegramInlineKeyboardButton, 0),
		currentRow: make([]types.TelegramInlineKeyboardButton, 0),
	}
}

// AddButton adds a button to the current row
func (kb *KeyboardBuilder) AddButton(text string, callbackData string) *KeyboardBuilder {
	kb.currentRow = append(kb.currentRow, types.TelegramInlineKeyboardButton{
		Text:         text,
		CallbackData: callbackData,
	})
	return kb
}

// AddURLButton adds a URL button to the current row
func (kb *KeyboardBuilder) AddURLButton(text, url string) *KeyboardBuilder {
	kb.currentRow = append(kb.currentRow, types.TelegramInlineKeyboardButton{
		Text: text,
		URL:  url,
	})
	return kb
}

// NewRow starts a new row
func (kb *KeyboardBuilder) NewRow() *KeyboardBuilder {
	if len(kb.currentRow) > 0 {
		kb.buttons = append(kb.buttons, kb.currentRow)
		kb.currentRow = make([]types.TelegramInlineKeyboardButton, 0)
	}
	return kb
}

// Build builds the keyboard
func (kb *KeyboardBuilder) Build() *types.TelegramInlineKeyboard {
	if len(kb.currentRow) > 0 {
		kb.buttons = append(kb.buttons, kb.currentRow)
		kb.currentRow = nil
	}

	return &types.TelegramInlineKeyboard{
		InlineKeyboard: kb.buttons,
	}
}

// FormatMarkdown formats text for MarkdownV2
// Escapes special characters as needed
func FormatMarkdown(text string) string {
	// MarkdownV2 special characters that need escaping
	specialChars := []string{"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!"}

	result := text
	for _, char := range specialChars {
		// Only escape if it's not already part of a parse mode entity
		// This is a simple implementation; a full one would parse entities
		if !strings.Contains(result, "\\") {
			result = strings.ReplaceAll(result, char, "\\"+char)
		}
	}

	return result
}

// FormatHTML formats text for HTML parse mode
func FormatHTML(text string) string {
	// Escape HTML special characters
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	)
	return replacer.Replace(text)
}

// MessageBuilder helps build messages
type MessageBuilder struct {
	buffer strings.Builder
	parseMode string
}

// NewMessageBuilder creates a new message builder
func NewMessageBuilder() *MessageBuilder {
	return &MessageBuilder{
		parseMode: "Markdown", // Default
	}
}

// Text adds text to the message
func (mb *MessageBuilder) Text(text string) *MessageBuilder {
	mb.buffer.WriteString(text)
	return mb
}

// Textf adds formatted text to the message
func (mb *MessageBuilder) Textf(format string, args ...interface{}) *MessageBuilder {
	mb.buffer.WriteString(fmt.Sprintf(format, args...))
	return mb
}

// Newline adds a newline
func (mb *MessageBuilder) Newline() *MessageBuilder {
	mb.buffer.WriteString("\n")
	return mb
}

// Bold adds bold text
func (mb *MessageBuilder) Bold(text string) *MessageBuilder {
	if mb.parseMode == "Markdown" || mb.parseMode == "MarkdownV2" {
		mb.buffer.WriteString(fmt.Sprintf("*%s*", text))
	} else {
		mb.buffer.WriteString(fmt.Sprintf("<b>%s</b>", text))
	}
	return mb
}

// Italic adds italic text
func (mb *MessageBuilder) Italic(text string) *MessageBuilder {
	if mb.parseMode == "Markdown" || mb.parseMode == "MarkdownV2" {
		mb.buffer.WriteString(fmt.Sprintf("_%s_", text))
	} else {
		mb.buffer.WriteString(fmt.Sprintf("<i>%s</i>", text))
	}
	return mb
}

// Code adds code text
func (mb *MessageBuilder) Code(text string) *MessageBuilder {
	if mb.parseMode == "Markdown" || mb.parseMode == "MarkdownV2" {
		mb.buffer.WriteString(fmt.Sprintf("`%s`", text))
	} else {
		mb.buffer.WriteString(fmt.Sprintf("<code>%s</code>", text))
	}
	return mb
}

// Link adds a link
func (mb *MessageBuilder) Link(text, url string) *MessageBuilder {
	if mb.parseMode == "Markdown" || mb.parseMode == "MarkdownV2" {
		mb.buffer.WriteString(fmt.Sprintf("[%s](%s)", text, url))
	} else {
		mb.buffer.WriteString(fmt.Sprintf("<a href=\"%s\">%s</a>", url, text))
	}
	return mb
}

// SetParseMode sets the parse mode
func (mb *MessageBuilder) SetParseMode(mode string) *MessageBuilder {
	mb.parseMode = mode
	return mb
}

// Build builds the message
func (mb *MessageBuilder) Build() string {
	return mb.buffer.String()
}

// ParseMode returns the parse mode
func (mb *MessageBuilder) ParseMode() string {
	return mb.parseMode
}

// Helpers for building common message formats

// BuildDetailKeyboard builds a detail view keyboard
func BuildDetailKeyboard(mediaID, mediaType string, hasQuota bool) *types.TelegramInlineKeyboard {
	kb := NewKeyboardBuilder()

	if hasQuota {
		kb.AddButton("✅ 求片", fmt.Sprintf("request:id:%s:type:%s", mediaID, mediaType))
	} else {
		kb.AddButton("📊 配额已用完", "quota_exceeded")
	}

	kb.NewRow()
	kb.AddButton("🔍 搜索", "search:menu")
	kb.AddButton("📋 我的请求", "requests:list")

	kb.NewRow()
	kb.AddButton("⬅️ 返回列表", "back")

	return kb.Build()
}

// BuildSearchKeyboard builds a search results keyboard
func BuildSearchKeyboard(items []SearchItemButton, page int, totalPages int, query string) *types.TelegramInlineKeyboard {
	kb := NewKeyboardBuilder()

	// Add item buttons
	for _, item := range items {
		kb.AddButton(item.Text, item.CallbackData)
		kb.NewRow()
	}

	// Add navigation buttons
	if totalPages > 1 {
		if page > 1 {
			kb.AddButton("⬅️ 上一页", fmt.Sprintf("search:query:%s:page:%d", url.QueryEscape(query), page-1))
		}
		if page < totalPages {
			kb.AddButton("➡️ 下一页", fmt.Sprintf("search:query:%s:page:%d", url.QueryEscape(query), page+1))
		}
		kb.NewRow()
	}

	kb.AddButton("❌ 取消", "cancel")

	return kb.Build()
}

// SearchItemButton represents a search result button
type SearchItemButton struct {
	Text         string
	CallbackData string
}

// BuildStartKeyboard builds the start menu keyboard
func BuildStartKeyboard(isAdmin bool) *types.TelegramInlineKeyboard {
	return BuildStartKeyboardWithOptions(isAdmin, true)
}

// BuildStartKeyboardWithOptions builds start keyboard with options
// showAI: whether to show AI recommendation button (only in private chats)
func BuildStartKeyboardWithOptions(isAdmin, showAI bool) *types.TelegramInlineKeyboard {
	kb := NewKeyboardBuilder()

	kb.AddButton("🔍 搜索影片", "start_search")
	if showAI {
		kb.AddButton("🤖 AI 推荐", "start_ai")
	}

	kb.NewRow()
	kb.AddButton("📋 我的请求", "start_requests")
	kb.AddButton("🔗 绑定账号", "start_link")

	kb.NewRow()
	kb.AddButton("❓ 帮助", "start_help")

	// Add admin button for admin users
	if isAdmin {
		kb.NewRow()
		kb.AddButton("⚙️ 管理菜单", "admin_menu")
	}

	return kb.Build()
}

// Int64ToString converts int64 to string
func Int64ToString(i int64) string {
	return strconv.FormatInt(i, 10)
}

// StringToInt64 converts string to int64
func StringToInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
