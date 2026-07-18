package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
)

// sanitizeUTF8 ensures the string contains only valid UTF-8 characters
// Invalid UTF-8 sequences are replaced with the replacement character
func sanitizeUTF8(s string) string {
	// Fast path: check if string is already valid with no null bytes
	if utf8.ValidString(s) && !strings.ContainsRune(s, 0) {
		return s
	}
	// Rebuild with only valid runes, skipping RuneError and null bytes
	var result strings.Builder
	result.Grow(len(s))
	for _, r := range s {
		if r == 0 || r == utf8.RuneError {
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}

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

// SetBaseURLForTest replaces the API endpoint and HTTP client for isolated tests.
// It is intentionally narrow and should not be used by runtime wiring.
func (c *TelegramClient) SetBaseURLForTest(baseURL string, httpClient *http.Client) {
	c.baseURL = baseURL
	c.httpClient = httpClient
}

// SetImageCache 设置图片缓存服务
func (c *TelegramClient) SetImageCache(cache *ImageCache) {
	c.imageCache = cache
	logger.Info("[Telegram] ImageCache attached")
}

// SendMessage sends a message to a chat. Optional send options preserve the
// existing call shape while allowing ephemeral message targeting.
func (c *TelegramClient) SendMessage(chatID int64, text string, parseMode string, keyboard *types.TelegramInlineKeyboard, options ...*types.TelegramSendOptions) (*types.TelegramMessage, error) {
	apiURL := fmt.Sprintf("%s/sendMessage", c.baseURL)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": parseMode,
	}

	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}
	applyTelegramSendOptions(payload, options)

	return c.makeRequest(apiURL, payload)
}

// richMessageEnabled returns whether Telegram Rich Message should be used.
// Default is enabled; set ENABLE_RICH_MESSAGE=false to force plain-message fallback.
func richMessageEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("ENABLE_RICH_MESSAGE")))
	return v == "" || !(v == "false" || v == "0" || v == "no" || v == "off")
}

// SendRichMessage sends a rich message via Telegram Bot API 10.1.
// Bot API 10.2 does not document ephemeral targeting for sendRichMessage;
// callers needing private group output must use SendMessage with send options.
func (c *TelegramClient) SendRichMessage(chatID int64, markdown string, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	return c.sendRichMessage(chatID, markdown, nil, keyboard)
}

// SendRichMessageWithPhoto embeds a photo in the same Rich Message card. The
// media is referenced by an internal tg:// ID, so users see one unified card
// instead of a separate photo message followed by duplicated text.
func (c *TelegramClient) SendRichMessageWithPhoto(chatID int64, markdown, photo string, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	media := []map[string]interface{}{{
		"id": "poster",
		"media": map[string]interface{}{
			"type":  "photo",
			"media": photo,
		},
	}}
	return c.sendRichMessage(chatID, "![](tg://photo?id=poster)\n\n"+markdown, media, keyboard)
}

func (c *TelegramClient) sendRichMessage(chatID int64, markdown string, media []map[string]interface{}, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	if !richMessageEnabled() {
		return nil, fmt.Errorf("rich message disabled by ENABLE_RICH_MESSAGE")
	}
	apiURL := fmt.Sprintf("%s/sendRichMessage", c.baseURL)

	richMessage := map[string]interface{}{
		"markdown": markdown,
	}
	if len(media) > 0 {
		richMessage["media"] = media
	}

	payload := map[string]interface{}{
		"chat_id":      chatID,
		"rich_message": richMessage,
	}

	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}

	return c.makeRequest(apiURL, payload)
}

func applyTelegramSendOptions(payload map[string]interface{}, options []*types.TelegramSendOptions) {
	if len(options) == 0 || options[0] == nil {
		return
	}
	option := options[0]
	if option.ReceiverUserID != 0 {
		payload["receiver_user_id"] = option.ReceiverUserID
	}
	if option.CallbackQueryID != "" {
		payload["callback_query_id"] = option.CallbackQueryID
	}
	if option.MessageThreadID != 0 {
		payload["message_thread_id"] = option.MessageThreadID
	}
	if option.ReplyParameters != nil {
		payload["reply_parameters"] = option.ReplyParameters
	}
}

func applyTelegramSendOptionsToMultipart(writer *multipart.Writer, options []*types.TelegramSendOptions) {
	if len(options) == 0 || options[0] == nil {
		return
	}
	option := options[0]
	if option.ReceiverUserID != 0 {
		_ = writer.WriteField("receiver_user_id", fmt.Sprintf("%d", option.ReceiverUserID))
	}
	if option.CallbackQueryID != "" {
		_ = writer.WriteField("callback_query_id", option.CallbackQueryID)
	}
	if option.MessageThreadID != 0 {
		_ = writer.WriteField("message_thread_id", fmt.Sprintf("%d", option.MessageThreadID))
	}
	if option.ReplyParameters != nil {
		if raw, err := json.Marshal(option.ReplyParameters); err == nil {
			_ = writer.WriteField("reply_parameters", string(raw))
		}
	}
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

// DeleteEphemeralMessage deletes a message visible only to receiverUserID.
func (c *TelegramClient) DeleteEphemeralMessage(chatID int64, receiverUserID int64, ephemeralMessageID int64) error {
	apiURL := fmt.Sprintf("%s/deleteEphemeralMessage", c.baseURL)
	payload := map[string]interface{}{
		"chat_id":              chatID,
		"receiver_user_id":     receiverUserID,
		"ephemeral_message_id": ephemeralMessageID,
	}
	return c.makeSimpleRequest(apiURL, payload)
}

// EditEphemeralMessageText edits an established user-scoped message. It no
// longer depends on the callback's 15-second send window.
func (c *TelegramClient) EditEphemeralMessageText(chatID, receiverUserID, ephemeralMessageID int64, text, parseMode string, keyboard *types.TelegramInlineKeyboard) error {
	apiURL := fmt.Sprintf("%s/editEphemeralMessageText", c.baseURL)
	payload := map[string]interface{}{
		"chat_id":              chatID,
		"receiver_user_id":     receiverUserID,
		"ephemeral_message_id": ephemeralMessageID,
		"text":                 text,
		"parse_mode":           parseMode,
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}
	return c.makeSimpleRequest(apiURL, payload)
}

// EditEphemeralMessageReplyMarkup updates only the buttons of an established
// ephemeral message.
func (c *TelegramClient) EditEphemeralMessageReplyMarkup(chatID, receiverUserID, ephemeralMessageID int64, keyboard *types.TelegramInlineKeyboard) error {
	apiURL := fmt.Sprintf("%s/editEphemeralMessageReplyMarkup", c.baseURL)
	payload := map[string]interface{}{
		"chat_id":              chatID,
		"receiver_user_id":     receiverUserID,
		"ephemeral_message_id": ephemeralMessageID,
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}
	return c.makeSimpleRequest(apiURL, payload)
}

// EditEphemeralMessageCaption edits the caption and keyboard of an established
// ephemeral media message. Telegram returns a Boolean, not a Message.
func (c *TelegramClient) EditEphemeralMessageCaption(chatID, receiverUserID, ephemeralMessageID int64, caption, parseMode string, keyboard *types.TelegramInlineKeyboard) error {
	apiURL := fmt.Sprintf("%s/editEphemeralMessageCaption", c.baseURL)
	payload := map[string]interface{}{
		"chat_id":              chatID,
		"receiver_user_id":     receiverUserID,
		"ephemeral_message_id": ephemeralMessageID,
		"caption":              caption,
	}
	if parseMode != "" {
		payload["parse_mode"] = parseMode
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}
	return c.makeSimpleRequest(apiURL, payload)
}

// EditEphemeralMessageMedia replaces an ephemeral message with URL/file_id
// media. Telegram doesn't allow uploading a new file through this edit method.
func (c *TelegramClient) EditEphemeralMessageMedia(chatID, receiverUserID, ephemeralMessageID int64, media map[string]interface{}, keyboard *types.TelegramInlineKeyboard) error {
	apiURL := fmt.Sprintf("%s/editEphemeralMessageMedia", c.baseURL)
	payload := map[string]interface{}{
		"chat_id":              chatID,
		"receiver_user_id":     receiverUserID,
		"ephemeral_message_id": ephemeralMessageID,
		"media":                media,
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}
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
		"show_alert":        showAlert,
	}

	// Only include text if it's not empty
	if text != "" {
		payload["text"] = text
	}

	return c.makeSimpleRequest(apiURL, payload)
}

// SendChatAction sends a chat action (typing, upload_photo, etc.) to show the bot is working
func (c *TelegramClient) SendChatAction(chatID int64, action string) error {
	apiURL := fmt.Sprintf("%s/sendChatAction", c.baseURL)
	payload := map[string]interface{}{
		"chat_id": chatID,
		"action":  action,
	}
	return c.makeSimpleRequest(apiURL, payload)
}

// SendPhoto sends a photo with caption to a chat
func (c *TelegramClient) SendPhoto(chatID int64, photoURL, caption string, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	return c.SendPhotoWithParseMode(chatID, photoURL, caption, "HTML", keyboard)
}

// SendPhotoWithParseMode sends a photo with caption and specified parse mode.
// Options carry ephemeral targeting and Forum topic context.
func (c *TelegramClient) SendPhotoWithParseMode(chatID int64, photoURL, caption, parseMode string, keyboard *types.TelegramInlineKeyboard, options ...*types.TelegramSendOptions) (*types.TelegramMessage, error) {
	// Use URL method first to avoid multipart encoding issues with Chinese characters
	// Telegram will download the image directly
	msg, err := c.SendPhotoByURLWithParseMode(chatID, photoURL, caption, parseMode, keyboard, options...)
	if err == nil {
		return msg, nil
	}

	// If URL method fails, try downloading and sending as file
	logger.Info("[Telegram] URL method failed: %v, trying file upload with parse_mode=%s", err, parseMode)
	return c.SendPhotoFromURLWithParseMode(chatID, photoURL, caption, parseMode, nil, keyboard, options...)
}

// SendPhotoWithAuth sends a photo with caption, using custom headers for image download
// This is needed for Emby images that require X-Emby-Token authentication
// Also used for TMDB images to ensure reliable delivery (avoids Telegram URL fetch issues)
// 实现图片代理上传：机器人下载图片后，通过 multipart/form-data 上传到 Telegram
// 支持本地缓存：相同图片优先从缓存读取，减少带宽消耗
func (c *TelegramClient) SendPhotoWithAuth(chatID int64, photoURL, caption string, headers map[string]string, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	return c.SendPhotoWithAuthAndParseMode(chatID, photoURL, caption, "HTML", headers, keyboard)
}

// SendPhotoWithAuthAndParseMode sends a photo with caption, parse mode, and custom headers
func (c *TelegramClient) SendPhotoWithAuthAndParseMode(chatID int64, photoURL, caption, parseMode string, headers map[string]string, keyboard *types.TelegramInlineKeyboard, options ...*types.TelegramSendOptions) (*types.TelegramMessage, error) {
	// 优先使用缓存（如果有）
	var imageData []byte
	var fromCache bool

	if c.imageCache != nil {
		if cached := c.imageCache.Get(photoURL); cached != nil {
			imageData = cached
			fromCache = true
			logger.Info("[Telegram] [缓存命中] 使用本地缓存图片: %d bytes", len(imageData))
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
		logger.Info("[Telegram] [代理上传] 正在下载 %s 图片: %s", imageType, photoURL)

		req, err := http.NewRequest("GET", photoURL, nil)
		if err != nil {
			logger.Info("[Telegram] Failed to create request: %v", err)
			return c.SendPhotoByURLWithParseMode(chatID, photoURL, caption, parseMode, keyboard, options...)
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
			logger.Info("[Telegram] [代理上传] 下载失败: %v", err)
			return c.SendPhotoByURLWithParseMode(chatID, photoURL, caption, parseMode, keyboard, options...)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			logger.Info("[Telegram] [代理上传] 下载状态码异常: %d", resp.StatusCode)
			return c.SendPhotoByURLWithParseMode(chatID, photoURL, caption, parseMode, keyboard, options...)
		}

		// 读取图片数据
		imageData, err = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			logger.Info("[Telegram] [代理上传] 读取图片数据失败: %v", err)
			return nil, err
		}

		logger.Info("[Telegram] [代理上传] 下载成功，大小: %d bytes", len(imageData))

		// 保存到缓存（异步）
		if c.imageCache != nil {
			go func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Info("[Telegram] Panic in cache save: %v", r)
					}
				}()
				if err := c.imageCache.Set(photoURL, imageData); err != nil {
					logger.Info("[Telegram] [缓存保存] 失败: %v", err)
				}
			}()
		}
	}

	// Create multipart form for Telegram upload
	apiURL := fmt.Sprintf("%s/sendPhoto", c.baseURL)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add chat_id and optional ephemeral/thread targeting.
	writer.WriteField("chat_id", fmt.Sprintf("%d", chatID))
	applyTelegramSendOptionsToMultipart(writer, options)

	// Add parse_mode
	writer.WriteField("parse_mode", parseMode)

	// Add caption (sanitize to ensure valid UTF-8)
	if caption != "" {
		sanitized := sanitizeUTF8(caption)
		writer.WriteField("caption", sanitized)
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
		keyboardJSON, err := json.Marshal(keyboard)
		if err != nil {
			logger.Info("[Telegram] Failed to marshal keyboard: %v", err)
			// Continue without keyboard rather than failing the entire send
		} else {
			logger.Debug("[Telegram] Adding keyboard: %d buttons", len(keyboard.InlineKeyboard))
			writer.WriteField("reply_markup", string(keyboardJSON))
		}
	} else {
		logger.Info("[Telegram] No keyboard to add to photo")
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
		logger.Info("[Telegram] [代理上传] 上传到 Telegram 失败: %v", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp2.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp2.Body, 1<<20))
	if err != nil {
		logger.Info("[Telegram] Failed to read response body: %v", err)
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	previewLen := len(body)
	if previewLen > 500 {
		previewLen = 500
	}
	logger.Info("[Telegram] sendPhoto response status: %d, body preview: %s", resp2.StatusCode, string(body[:previewLen]))

	var result struct {
		OK     bool                   `json:"ok"`
		Result *types.TelegramMessage `json:"result"`
		Error  *types.TelegramError   `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		logger.Info("[Telegram] Failed to decode response: %v, body: %s", err, string(body))
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.OK {
		if result.Error != nil {
			logger.Info("[Telegram] sendPhoto API error: %s", result.Error.Message)
			return nil, result.Error
		}
		logger.Info("[Telegram] sendPhoto API error (no error details): %s", string(body))
		return nil, fmt.Errorf("telegram API error: %s", string(body))
	}

	logPrefix := "[代理上传]"
	if fromCache {
		logPrefix = "[缓存上传]"
	}
	if result.Result != nil {
		logger.Info("[Telegram] %s 成功发送图片, message_id=%d", logPrefix, result.Result.MessageID)
	} else {
		logger.Info("[Telegram] %s 成功发送图片 (no message id)", logPrefix)
	}
	return result.Result, nil
}

// SendPhotoFromURL downloads photo from URL and sends it to Telegram
func (c *TelegramClient) SendPhotoFromURL(chatID int64, photoURL, caption string, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	return c.SendPhotoFromURLWithParseMode(chatID, photoURL, caption, "HTML", nil, keyboard)
}

// SendPhotoFromURLWithParseMode downloads photo from URL and sends it to Telegram with specified parse mode
func (c *TelegramClient) SendPhotoFromURLWithParseMode(chatID int64, photoURL, caption, parseMode string, headers map[string]string, keyboard *types.TelegramInlineKeyboard, options ...*types.TelegramSendOptions) (*types.TelegramMessage, error) {
	logger.Info("[Telegram] Downloading photo from: %s with parse_mode=%s", photoURL, parseMode)

	// Download the image
	var resp *http.Response
	var err error

	if headers != nil {
		// Create request with custom headers (for Emby auth)
		req, _ := http.NewRequest("GET", photoURL, nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err = c.httpClient.Do(req)
	} else {
		resp, err = c.httpClient.Get(photoURL)
	}

	if err != nil {
		logger.Info("[Telegram] Failed to download photo: %v", err)
		// Fallback to URL method if download fails
		return c.SendPhotoByURLWithParseMode(chatID, photoURL, caption, parseMode, keyboard, options...)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Info("[Telegram] Photo download status: %d", resp.StatusCode)
		// Fallback to URL method
		return c.SendPhotoByURLWithParseMode(chatID, photoURL, caption, parseMode, keyboard, options...)
	}

	logger.Info("[Telegram] Photo downloaded, size: %d bytes", resp.ContentLength)

	// Create multipart form
	apiURL := fmt.Sprintf("%s/sendPhoto", c.baseURL)

	// Read image data
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add chat_id and optional ephemeral/thread targeting.
	writer.WriteField("chat_id", fmt.Sprintf("%d", chatID))
	applyTelegramSendOptionsToMultipart(writer, options)

	// Add parse_mode
	writer.WriteField("parse_mode", parseMode)

	// Add caption (sanitize to ensure valid UTF-8)
	if caption != "" {
		sanitized := sanitizeUTF8(caption)
		writer.WriteField("caption", sanitized)
	}

	// Add photo file
	part, err := writer.CreateFormFile("photo", "photo.jpg")
	if err != nil {
		return c.SendPhotoByURLWithParseMode(chatID, photoURL, caption, parseMode, keyboard, options...)
	}

	_, err = io.Copy(part, resp.Body)
	if err != nil {
		return c.SendPhotoByURLWithParseMode(chatID, photoURL, caption, parseMode, keyboard, options...)
	}

	// Add keyboard if provided
	if keyboard != nil && len(keyboard.InlineKeyboard) > 0 {
		keyboardJSON, err := json.Marshal(keyboard)
		if err != nil {
			logger.Info("[Telegram] Failed to marshal keyboard: %v", err)
			// Continue without keyboard rather than failing the entire send
		} else {
			logger.Debug("[Telegram] Adding keyboard: %d buttons", len(keyboard.InlineKeyboard))
			writer.WriteField("reply_markup", string(keyboardJSON))
		}
	} else {
		logger.Info("[Telegram] No keyboard to add to photo")
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
		logger.Info("[Telegram] Multipart request failed: %v", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp2.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp2.Body, 1<<20))
	if err != nil {
		logger.Info("[Telegram] Failed to read response body: %v", err)
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		OK     bool                   `json:"ok"`
		Result *types.TelegramMessage `json:"result"`
		Error  *types.TelegramError   `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		logger.Info("[Telegram] Failed to decode multipart response: %v", err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.OK {
		if result.Error != nil {
			logger.Info("[Telegram] Multipart API error: %s", result.Error.Message)
			return nil, result.Error
		}
		logger.Info("[Telegram] Multipart API error (no error details): %s", string(body))
		return nil, fmt.Errorf("telegram API error: %s", string(body))
	}

	logger.Info("[Telegram] Multipart sendPhoto successful with parse_mode=%s", parseMode)
	return result.Result, nil
}

// SendPhotoByURL sends photo by URL (Telegram will download it)
func (c *TelegramClient) SendPhotoByURL(chatID int64, photoURL, caption string, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	return c.SendPhotoByURLWithParseMode(chatID, photoURL, caption, "HTML", keyboard)
}

// SendPhotoByURLWithParseMode sends photo by URL with specified parse mode
func (c *TelegramClient) SendPhotoByURLWithParseMode(chatID int64, photoURL, caption, parseMode string, keyboard *types.TelegramInlineKeyboard, options ...*types.TelegramSendOptions) (*types.TelegramMessage, error) {
	apiURL := fmt.Sprintf("%s/sendPhoto", c.baseURL)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"photo":      photoURL,
		"caption":    caption,
		"parse_mode": parseMode,
	}
	applyTelegramSendOptions(payload, options)

	// Add keyboard if provided
	if keyboard != nil && len(keyboard.InlineKeyboard) > 0 {
		payload["reply_markup"] = keyboard
		if kbJSON, err := json.Marshal(keyboard); err == nil {
			logger.Info("[Telegram] sendPhoto payload: chat_id=%d, photo=%s, caption=%d chars, parse_mode=%s, keyboard=%s",
				chatID, photoURL, len(caption), parseMode, string(kbJSON))
		}
	}

	logger.Info("[Telegram] Calling sendPhoto API with parse_mode=%s...", parseMode)
	return c.makeRequest(apiURL, payload)
}

// SendPhotoByFileID sends a photo using Telegram's file_id
// This is the most efficient way to resend a photo that was previously uploaded to Telegram
func (c *TelegramClient) SendPhotoByFileID(chatID int64, fileID, caption string, keyboard *types.TelegramInlineKeyboard) (*types.TelegramMessage, error) {
	return c.SendPhotoByFileIDWithParseMode(chatID, fileID, caption, "HTML", keyboard)
}

// SendPhotoByFileIDWithParseMode sends a photo using Telegram's file_id with specified parse mode
func (c *TelegramClient) SendPhotoByFileIDWithParseMode(chatID int64, fileID, caption, parseMode string, keyboard *types.TelegramInlineKeyboard, options ...*types.TelegramSendOptions) (*types.TelegramMessage, error) {
	apiURL := fmt.Sprintf("%s/sendPhoto", c.baseURL)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"photo":      fileID,
		"caption":    caption,
		"parse_mode": parseMode,
	}
	applyTelegramSendOptions(payload, options)

	// Add keyboard if provided
	if keyboard != nil && len(keyboard.InlineKeyboard) > 0 {
		payload["reply_markup"] = keyboard
	}

	logger.Info("[Telegram] Sending photo by file_id to chat %d with parse_mode=%s", chatID, parseMode)
	return c.makeRequest(apiURL, payload)
}

// SetWebhook sets the webhook URL without a secret (legacy callers).
func (c *TelegramClient) SetWebhook(webhookURL string) error {
	return c.SetWebhookWithSecret(webhookURL, "")
}

// SetWebhookWithSecret sets the webhook URL and Telegram secret_token.
func (c *TelegramClient) SetWebhookWithSecret(webhookURL, secret string) error {
	apiURL := fmt.Sprintf("%s/setWebhook", c.baseURL)

	payload := map[string]interface{}{
		"url": webhookURL,
	}
	if secret != "" {
		payload["secret_token"] = secret
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	var response struct {
		OK     bool                   `json:"ok"`
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	var result struct {
		OK     bool                   `json:"ok"`
		Result map[string]interface{} `json:"result"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if !result.OK {
		return nil, fmt.Errorf("telegram API error")
	}

	return result.Result, nil
}

// doWithRetry 执行 HTTP 请求，遇到 429 限流时自动重试（最多 3 次）
func (c *TelegramClient) doWithRetry(req *http.Request) (*http.Response, error) {
	const maxRetries = 3
	// 保存原始 body 用于重试
	var bodyBytes []byte
	if req.Body != nil {
		bodyBytes, _ = io.ReadAll(req.Body)
		req.Body.Close()
	}
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// 每次重试重新设置 body
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != 429 {
			return resp, nil
		}
		// 429 Too Many Requests — 读取 Retry-After 并等待
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		resp.Body.Close()
		retryAfter := 1 // 默认 1 秒
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			fmt.Sscanf(ra, "%d", &retryAfter)
		}
		if retryAfter > 30 {
			retryAfter = 30 // 最多等 30 秒
		}
		logger.Warn("[Telegram] 429 限流，%d 秒后重试 (attempt %d/%d): %s", retryAfter, attempt+1, maxRetries, string(respBody))
		time.Sleep(time.Duration(retryAfter) * time.Second)
	}
	return nil, fmt.Errorf("telegram API rate limited after %d retries", maxRetries)
}

// makeRequest makes a generic API request
func (c *TelegramClient) makeRequest(apiURL string, payload map[string]interface{}) (*types.TelegramMessage, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	// 只打印 API 端点，不打印完整 payload
	method := extractMethod(apiURL)
	logger.Info("[Telegram] API: %s", method)

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.doWithRetry(req)
	if err != nil {
		logger.Info("[Telegram] 请求失败: %s", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		OK        bool                   `json:"ok"`
		Result    *types.TelegramMessage `json:"result"`
		ErrorCode int                    `json:"error_code,omitempty"`
		ErrorDesc string                 `json:"description,omitempty"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		logger.Info("[Telegram] 解析响应失败: %v", err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.OK {
		errorMsg := result.ErrorDesc
		// 特殊处理：消息未被修改不是真正的错误，只是说明消息已经是目标状态
		// 这是一个常见的 Telegram API 行为，可以安全忽略
		if result.ErrorCode == 400 &&
			(strings.Contains(errorMsg, "message not modified") ||
				strings.Contains(errorMsg, "消息未修改") ||
				strings.Contains(errorMsg, "message is not modified")) {
			// 静默忽略，返回 nil 表示操作成功（消息已经正确）
			return nil, nil
		}
		if errorMsg != "" {
			logger.Info("[Telegram] API 错误: %s", errorMsg)
			return nil, &types.TelegramError{Code: result.ErrorCode, Message: errorMsg}
		}
		logger.Info("[Telegram] API 未知错误: %s", string(body))
		return nil, fmt.Errorf("telegram API error: %s", string(body))
	}

	// Log success for sendPhoto
	if result.Result != nil {
		logger.Info("[Telegram] %s 成功, message_id=%d, has_photo=%v", method, result.Result.MessageID, result.Result.Photo != nil)
		if len(result.Result.Photo) > 0 {
			logger.Info("[Telegram]   返回 %d 张图片", len(result.Result.Photo))
		}
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
	logger.Info("[Telegram] API: %s", method)

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.doWithRetry(req)
	if err != nil {
		logger.Info("[Telegram] 请求失败: %s", err)
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		OK        bool   `json:"ok"`
		Result    bool   `json:"result"`
		ErrorCode int    `json:"error_code,omitempty"`
		ErrorDesc string `json:"description,omitempty"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		logger.Info("[Telegram] 解析响应失败: %v", err)
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.OK {
		errorMsg := result.ErrorDesc
		// 特殊处理：消息未被修改不是真正的错误
		if result.ErrorCode == 400 &&
			(strings.Contains(errorMsg, "message not modified") ||
				strings.Contains(errorMsg, "消息未修改") ||
				strings.Contains(errorMsg, "message is not modified")) {
			return nil // 静默忽略
		}
		if errorMsg != "" {
			logger.Info("[Telegram] API 错误: %s", errorMsg)
			return &types.TelegramError{Code: result.ErrorCode, Message: errorMsg}
		}
		return fmt.Errorf("telegram API error: %s", string(body))
	}

	return nil
}

// BotCommand represents a bot command in the menu
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
	IsEphemeral bool   `json:"is_ephemeral,omitempty"`
}

// SetMyCommands sets the bot's default command menu.
func (c *TelegramClient) SetMyCommands(commands []BotCommand, languageCode string) error {
	return c.SetMyCommandsForScope(commands, languageCode, nil)
}

// SetMyCommandsForScope sets commands for a Bot API command scope. Passing nil
// keeps the historical default scope. Group commands can therefore be marked
// ephemeral without changing the private-chat command menu.
func (c *TelegramClient) SetMyCommandsForScope(commands []BotCommand, languageCode string, scope map[string]interface{}) error {
	apiURL := fmt.Sprintf("%s/setMyCommands", c.baseURL)

	payload := map[string]interface{}{
		"commands": commands,
	}
	if scope != nil {
		payload["scope"] = scope
	}

	if languageCode != "" {
		payload["language_code"] = languageCode
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	logger.Info("[Telegram] Setting bot commands")

	resp, err := c.httpClient.Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	logger.Info("[Telegram] Response: %s", logger.Sanitize(string(body)))

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
	buttons    [][]types.TelegramInlineKeyboardButton
	currentRow []types.TelegramInlineKeyboardButton
}

// NewKeyboardBuilder creates a new keyboard builder
func NewKeyboardBuilder() *KeyboardBuilder {
	return &KeyboardBuilder{
		buttons:    make([][]types.TelegramInlineKeyboardButton, 0),
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
	buffer    strings.Builder
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
			kb.AddButton("⬅️ 上一页", fmt.Sprintf("search:page:%d", page-1))
		}
		if page < totalPages {
			kb.AddButton("➡️ 下一页", fmt.Sprintf("search:page:%d", page+1))
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
// isAdmin: show admin menu button
// showWish: show wish pool button
func BuildStartKeyboardWithOptions(isAdmin, showWish bool) *types.TelegramInlineKeyboard {
	kb := NewKeyboardBuilder()

	kb.AddButton("🔍 搜索求片", "start_search")
	kb.AddButton("📊 求片进度", "start_requests")
	kb.NewRow()
	if showWish {
		kb.AddButton("✨ 许愿池", "start_wish")
	}
	kb.AddButton("🧠 观影画像", "start_portrait")
	kb.NewRow()
	kb.AddButton("⚔️ 趣味求片", "adventure_start")
	kb.AddButton("🎮 游戏中心", "game_menu")
	kb.NewRow()
	kb.AddButton("⚙️ 设置", "start_settings")
	kb.AddButton("❓ 帮助", "help")

	if isAdmin {
		kb.NewRow()
		kb.AddButton("🛠️ 管理", "admin_menu")
	}

	return kb.Build()
}

// BuildGameCenterKeyboard builds the single canonical game-center menu.
// Viewing portraits stay in the main menu and are intentionally not duplicated here.
func BuildGameCenterKeyboard() *types.TelegramInlineKeyboard {
	kb := NewKeyboardBuilder()
	kb.AddButton("⚔️ 电影冒险", "adventure_start")
	kb.AddButton("🎯 今日挑战", "game_daily_challenge")
	kb.NewRow()
	kb.AddButton("📊 冒险排行", "game_adventure_rank")
	kb.AddButton("📈 我的战绩", "game_adventure_stats")
	kb.NewRow()
	kb.AddButton("📖 电影情报站", "game_narrator")
	kb.AddButton("⬅️ 返回", "start")
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

// #6 许愿池坑5：发通知前用 getChatMember 检查用户是否可达。
// GetChatMemberStatus 返回用户在指定聊天里的成员状态（"member"/"administrator"/
// "creator"/"restricted"/"left"/"kicked"）。
// 出错（包括用户从未与 bot 交互导致的 400/403）时返回空串 + error，调用方据此判定为不可达。
func (c *TelegramClient) GetChatMemberStatus(chatID int64, userID int64) (string, error) {
	apiURL := fmt.Sprintf("%s/getChatMember", c.baseURL)
	payload := map[string]interface{}{
		"chat_id": chatID,
		"user_id": userID,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("getChatMember request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			Status string `json:"status"`
		} `json:"result"`
		ErrorCode int    `json:"error_code,omitempty"`
		ErrorDesc string `json:"description,omitempty"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to decode getChatMember response: %w", err)
	}

	if !result.OK {
		return "", &types.TelegramError{Code: result.ErrorCode, Message: result.ErrorDesc}
	}
	return result.Result.Status, nil
}

// GetUserDisplayName 通过 getChat API 获取用户的显示名称。
// 返回格式：FirstName + LastName（如有），失败时返回空串。
func (c *TelegramClient) GetUserDisplayName(userID int64) (string, error) {
	apiURL := fmt.Sprintf("%s/getChat", c.baseURL)
	payload := map[string]interface{}{
		"chat_id": userID,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("getChat request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("failed to read getChat response: %w", err)
	}

	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
		} `json:"result"`
		ErrorCode int    `json:"error_code,omitempty"`
		ErrorDesc string `json:"description,omitempty"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to decode getChat response: %w", err)
	}

	if !result.OK {
		return "", &types.TelegramError{Code: result.ErrorCode, Message: result.ErrorDesc}
	}

	name := strings.TrimSpace(result.Result.FirstName + " " + result.Result.LastName)
	return name, nil
}
