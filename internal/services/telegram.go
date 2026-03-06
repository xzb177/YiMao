package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net"
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
	imageCache *ImageCache // 图片缓存服务
}

// NewTelegramClient creates a new Telegram client
func NewTelegramClient(botToken string) *TelegramClient {
	return &TelegramClient{
		botToken: botToken,
		baseURL:  fmt.Sprintf("https://api.telegram.org/bot%s", botToken),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
				// Set connection timeouts
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				// Force HTTP/2
				ForceAttemptHTTP2: true,
				// TLS handshake timeout
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
	}
}

// SetImageCache 设置图片缓存服务
func (c *TelegramClient) SetImageCache(cache *ImageCache) {
	c.imageCache = cache
	log.Printf("[Telegram] ImageCache attached")
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

	// DeleteMessage returns bool result, not Message
	return c.makeSimpleRequest(apiURL, payload)
}

// EditMessageReplyMarkup edits only the reply markup of an existing message
// 用于状态融合按钮的原地刷新 - 不修改消息文本，只更新按钮
func (c *TelegramClient) EditMessageReplyMarkup(chatID int64, messageID int64, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	apiURL := fmt.Sprintf("%s/editMessageReplyMarkup", c.baseURL)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
	}

	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}

	return c.makeRequest(apiURL, payload)
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
	// Use URL method first to avoid multipart encoding issues with Chinese characters
	// Telegram will download the image directly
	msg, err := c.SendPhotoByURL(chatID, photoURL, caption, keyboard)
	if err == nil {
		return msg, nil
	}

	// If URL method fails, try downloading and sending as file
	log.Printf("[Telegram] URL method failed: %v, trying file upload", err)
	return c.SendPhotoFromURL(chatID, photoURL, caption, keyboard)
}

// SendPhotoWithAuth sends a photo with caption, using custom headers for image download
// This is needed for Emby images that require X-Emby-Token authentication
// Also used for TMDB images to ensure reliable delivery (avoids Telegram URL fetch issues)
// 实现图片代理上传：机器人下载图片后，通过 multipart/form-data 上传到 Telegram
// 支持本地缓存：相同图片优先从缓存读取，减少带宽消耗
func (c *TelegramClient) SendPhotoWithAuth(chatID int64, photoURL, caption string, headers map[string]string, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	// 优先使用缓存（如果有）
	var imageData []byte
	var fromCache bool

	if c.imageCache != nil {
		if cached := c.imageCache.Get(photoURL); cached != nil {
			imageData = cached
			fromCache = true
			log.Printf("[Telegram] [缓存命中] 使用本地缓存图片: %d bytes", len(imageData))
		}
	}

	// 缓存未命中，下载图片
	if imageData == nil {
		imageType := "外部"
		if strings.Contains(photoURL, "emby") || strings.Contains(photoURL, "Emby") {
			imageType = "Emby"
		} else if strings.Contains(photoURL, "tmdb") || strings.Contains(photoURL, "themoviedb") {
			imageType = "TMDB"
		}
		log.Printf("[Telegram] [代理上传] 正在下载 %s 图片: %s", imageType, photoURL)

		req, err := http.NewRequest("GET", photoURL, nil)
		if err != nil {
			log.Printf("[Telegram] Failed to create request: %v", err)
			return c.SendPhotoByURL(chatID, photoURL, caption, keyboard)
		}

		// Add User-Agent to avoid being blocked
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")

		// Add custom headers (e.g., X-Emby-Token)
		for key, value := range headers {
			req.Header.Set(key, value)
		}

		// Download the image
		resp, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[Telegram] [代理上传] 下载失败: %v", err)
			return c.SendPhotoByURL(chatID, photoURL, caption, keyboard)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Printf("[Telegram] [代理上传] 下载状态码异常: %d", resp.StatusCode)
			return c.SendPhotoByURL(chatID, photoURL, caption, keyboard)
		}

		// 读取图片数据
		imageData, err = io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("[Telegram] [代理上传] 读取图片数据失败: %v", err)
			return nil, err
		}

		log.Printf("[Telegram] [代理上传] 下载成功，大小: %d bytes", len(imageData))

		// 保存到缓存（异步）
		if c.imageCache != nil {
			go func() {
				if err := c.imageCache.Set(photoURL, imageData); err != nil {
					log.Printf("[Telegram] [缓存保存] 失败: %v", err)
				}
			}()
		}
	}

	// Create multipart form for Telegram upload
	apiURL := fmt.Sprintf("%s/sendPhoto", c.baseURL)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add chat_id
	writer.WriteField("chat_id", fmt.Sprintf("%d", chatID))

	// Add caption
	if caption != "" {
		writer.WriteField("caption", caption)
	}

	// Add photo file (从内存字节流上传)
	part, err := writer.CreateFormFile("photo", "photo.jpg")
	if err != nil {
		return nil, err
	}

	if _, err := part.Write(imageData); err != nil {
		return nil, err
	}

	// Add keyboard if provided
	if keyboard != nil && len(keyboard.InlineKeyboard) > 0 {
		keyboardJSON, _ := json.Marshal(keyboard)
		writer.WriteField("reply_markup", string(keyboardJSON))
	}

	writer.Close()

	// Create POST request to Telegram
	req2, err := http.NewRequest("POST", apiURL, &buf)
	if err != nil {
		return nil, err
	}

	req2.Header.Set("Content-Type", writer.FormDataContentType())

	resp2, err := c.httpClient.Do(req2)
	if err != nil {
		log.Printf("[Telegram] [代理上传] 上传到 Telegram 失败: %v", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp2.Body.Close()

	body, _ := io.ReadAll(resp2.Body)

	var result struct {
		OK      bool                     `json:"ok"`
		Result  *types.TelegramMessage  `json:"result"`
		Error   *types.TelegramError    `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[Telegram] Failed to decode response: %v", err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.OK {
		if result.Error != nil {
			return nil, result.Error
		}
		return nil, fmt.Errorf("telegram API error: %s", string(body))
	}

	logPrefix := "[代理上传]"
	if fromCache {
		logPrefix = "[缓存上传]"
	}
	log.Printf("[Telegram] %s 成功发送图片到 Telegram", logPrefix)
	return result.Result, nil
}

// SendPhotoFromURL downloads photo from URL and sends it to Telegram
func (c *TelegramClient) SendPhotoFromURL(chatID int64, photoURL, caption string, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	log.Printf("[Telegram] Downloading photo from: %s", photoURL)

	// Download the image
	resp, err := c.httpClient.Get(photoURL)
	if err != nil {
		log.Printf("[Telegram] Failed to download photo: %v", err)
		// Fallback to URL method if download fails
		return c.SendPhotoByURL(chatID, photoURL, caption, keyboard)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[Telegram] Photo download status: %d", resp.StatusCode)
		// Fallback to URL method
		return c.SendPhotoByURL(chatID, photoURL, caption, keyboard)
	}

	log.Printf("[Telegram] Photo downloaded, size: %d bytes", resp.ContentLength)

	// Create multipart form
	apiURL := fmt.Sprintf("%s/sendPhoto", c.baseURL)

	// Read image data
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add chat_id
	writer.WriteField("chat_id", fmt.Sprintf("%d", chatID))

	// Add caption
	if caption != "" {
		writer.WriteField("caption", caption)
	}

	// Add photo file
	part, err := writer.CreateFormFile("photo", "photo.jpg")
	if err != nil {
		return c.SendPhotoByURL(chatID, photoURL, caption, keyboard)
	}

	_, err = io.Copy(part, resp.Body)
	if err != nil {
		return c.SendPhotoByURL(chatID, photoURL, caption, keyboard)
	}

	// Add keyboard if provided
	if keyboard != nil && len(keyboard.InlineKeyboard) > 0 {
		keyboardJSON, _ := json.Marshal(keyboard)
		writer.WriteField("reply_markup", string(keyboardJSON))
	}

	writer.Close()

	// Create request
	req, err := http.NewRequest("POST", apiURL, &buf)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp2, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("[Telegram] Multipart request failed: %v", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp2.Body.Close()

	body, _ := io.ReadAll(resp2.Body)

	var result struct {
		OK      bool                  `json:"ok"`
		Result  *types.TelegramMessage `json:"result"`
		Error   *types.TelegramError  `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[Telegram] Failed to decode multipart response: %v", err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.OK {
		if result.Error != nil {
			log.Printf("[Telegram] Multipart API error: %s", result.Error.Message)
			return nil, result.Error
		}
		log.Printf("[Telegram] Multipart API error (no error details): %s", string(body))
		return nil, fmt.Errorf("telegram API error: %s", string(body))
	}

	log.Printf("[Telegram] Multipart sendPhoto successful")
	return result.Result, nil
}

// SendPhotoByURL sends photo by URL (Telegram will download it)
func (c *TelegramClient) SendPhotoByURL(chatID int64, photoURL, caption string, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	apiURL := fmt.Sprintf("%s/sendPhoto", c.baseURL)

	payload := map[string]interface{}{
		"chat_id": chatID,
		"photo":   photoURL,
		"caption": caption,
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

	// 只打印 API 端点，不打印完整 payload
	method := extractMethod(apiURL)
	log.Printf("[Telegram] API: %s", method)

	resp, err := c.httpClient.Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("[Telegram] 请求失败: %s", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		OK      bool                  `json:"ok"`
		Result  *types.TelegramMessage `json:"result"`
		Error   *types.TelegramError  `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[Telegram] 解析响应失败: %v", err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.OK {
		if result.Error != nil {
			// 特殊处理：消息未被修改不是真正的错误，只是说明消息已经是目标状态
			// 这是一个常见的 Telegram API 行为，可以安全忽略
			if result.Error.Code == 400 &&
				(strings.Contains(result.Error.Message, "message not modified") ||
				 strings.Contains(result.Error.Message, "消息未修改") ||
				 strings.Contains(result.Error.Message, "message is not modified")) {
				// 静默忽略，返回 nil 表示操作成功（消息已经正确）
				return nil, nil
			}
			log.Printf("[Telegram] API 错误: %s", result.Error.Message)
			return nil, result.Error
		}
		log.Printf("[Telegram] API 未知错误: %s", string(body))
		return nil, fmt.Errorf("telegram API error: %s", string(body))
	}

	return result.Result, nil
}

// extractMethod 提取 API 方法名称（用于日志）
func extractMethod(apiURL string) string {
	parts := strings.Split(apiURL, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return apiURL
}

// makeSimpleRequest makes an API request that returns a boolean result (like answerCallbackQuery)
func (c *TelegramClient) makeSimpleRequest(apiURL string, payload map[string]interface{}) error {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	method := extractMethod(apiURL)
	log.Printf("[Telegram] API: %s", method)

	resp, err := c.httpClient.Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("[Telegram] 请求失败: %s", err)
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		OK     bool                 `json:"ok"`
		Result bool                 `json:"result"`
		Error  *types.TelegramError `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[Telegram] 解析响应失败: %v", err)
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.OK {
		if result.Error != nil {
			// 特殊处理：消息未被修改不是真正的错误
			if result.Error.Code == 400 &&
				(strings.Contains(result.Error.Message, "message not modified") ||
				 strings.Contains(result.Error.Message, "消息未修改") ||
				 strings.Contains(result.Error.Message, "message is not modified")) {
				return nil // 静默忽略
			}
			log.Printf("[Telegram] API 错误: %s", result.Error.Message)
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
		parseMode: "HTML", // Use HTML for more reliable formatting
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

	kb.AddButton("🔍 搜索", "start_search")
	if showAI {
		kb.AddButton("🎬 推荐", "start_ai")
	}

	kb.NewRow()
	kb.AddButton("💫 情绪选片", "start_mood")

	kb.NewRow()
	kb.AddButton("📋 我的请求", "start_requests")
	kb.AddButton("🐞 我的反馈", "my_feedback")

	kb.NewRow()
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
