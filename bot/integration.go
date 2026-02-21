package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"emby-telegram-bot/chain"
	"emby-telegram-bot/session"
)

// Global mutex for user_mappings.json file access (deprecated - using userSyncMgr instead)
var userMappingsMutex sync.RWMutex

// UserSyncGetter is an interface for getting user mappings
// This allows BotModule to access the global userSyncMgr without direct coupling
type UserSyncGetter interface {
	GetJellyseerrUserID(telegramID int64) (int64, bool)
	GetTelegramUsername(telegramID int64) string
}

// globalUserSyncGetter holds the reference to the user sync manager
var globalUserSyncGetter UserSyncGetter

// SetUserSyncGetter sets the global user sync getter
func SetUserSyncGetter(getter UserSyncGetter) {
	globalUserSyncGetter = getter
	log.Printf("[BotModule] UserSyncGetter set")
}

// AdminNotifier is a function type for sending admin notifications
type AdminNotifier func(mediaTitle, mediaType, username, requestID string)

// httpClient for sending Telegram messages
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

// BotModule integrates all bot functionality
type BotModule struct {
	handler        *Handler
	sessionManager *session.SessionManager
	messageEditor  *MessageEditor
	quotaManager   *QuotaManager
	chatSystem     *ChatSystem // 添加聊天系统
	feedbackManager *FeedbackManager // 反馈管理器

	searchChain    *chain.SearchChain
	subscribeChain *chain.SubscribeChain
	downloadChain  *chain.DownloadChain

	// Configuration
	botToken  string
	chatID    string
	jellyseerrURL string
	jellyseerrAPIKey string

	// Admin notification
	adminNotifier AdminNotifier
	admins        map[string]string // adminID -> adminName
	adminsMutex   sync.RWMutex

	mu sync.RWMutex
}

// NewBotModule creates a new bot module
func NewBotModule() *BotModule {
	module := &BotModule{
		handler:        NewHandler(),
		sessionManager: session.NewSessionManager(),
		messageEditor:  NewMessageEditor(),
		quotaManager:   NewQuotaManager("user_quotas.json"),
	}

	// 初始化特权管理器
	privilegeMgr := NewPrivilegeManager("/app/data/admin_profiles.json")
	module.handler.SetPrivilegeManager(privilegeMgr)

	// FeedbackManager and ChatSystem will be initialized in Init()

	// Register handlers
	module.handler.RegisterSearchHandler("default", module.handleSearchRequest)
	module.handler.RegisterSubscribeHandler("default", module.handleSubscribeRequest)
	module.handler.RegisterDownloadHandler("default", module.handleDownloadRequest)

	// Inject dependencies
	module.handler.SetSessionManager(module.sessionManager)
	module.handler.SetMessageEditor(module.messageEditor)
	module.handler.SetQuotaManager(module.quotaManager)

	return module
}

// Init initializes the bot module with configuration
func (m *BotModule) Init(botToken, chatID, jellyseerrURL, jellyseerrAPIKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.botToken = botToken
	m.chatID = chatID
	m.jellyseerrURL = jellyseerrURL
	m.jellyseerrAPIKey = jellyseerrAPIKey

	// Initialize chains
	m.searchChain = chain.NewSearchChain(jellyseerrURL, jellyseerrAPIKey)
	m.subscribeChain = chain.NewSubscribeChain(jellyseerrURL, jellyseerrAPIKey)
	m.downloadChain = chain.NewDownloadChain(jellyseerrURL, jellyseerrAPIKey, "", "", "")

	// Set bot token for message editor
	m.messageEditor.SetBotToken(botToken)

	// Initialize chat system for private chat responses
	kb := NewKnowledgeBase(".")
	m.chatSystem = NewChatSystem(kb)
	m.handler.SetChatSystem(m.chatSystem)

	// Initialize feedback manager
	m.feedbackManager = NewFeedbackManager(jellyseerrURL, jellyseerrAPIKey)
	m.handler.SetFeedbackManager(m.feedbackManager)

	// Note: Admin checker should be set via SetAdminChecker() from main.go

	log.Printf("[BotModule] Initialized with Jellyseerr: %s", jellyseerrURL)

	return nil
}

// HandleMessage processes an incoming message (exported for main.go)
func (m *BotModule) HandleMessage(update *TelegramUpdate) {
	response := m.handler.HandleMessage(update)

	if response != nil && update.Message != nil {
		chatID := update.Message.Chat.ID
		messageID := update.Message.MessageID

		log.Printf("[BotModule] Response received: edit=%v, text_len=%d", response.EditMode, len(response.Text))

		// Store message ID for editing
		if response.EditMode && messageID > 0 {
			m.messageEditor.EditMessage(chatID, messageID, response.Text, response.Keyboard)
		} else {
			m.messageEditor.SendMessage(chatID, response.Text, response.Keyboard)
		}
	} else {
		log.Printf("[BotModule] No response or message: response=%v, message=%v", response != nil, update.Message != nil)
	}
}

// HandleCallback processes a callback query (exported for main.go)
func (m *BotModule) HandleCallback(update *TelegramUpdate) {
	if update.CallbackQuery == nil {
		return
	}

	log.Printf("[BotModule] Received callback: %s", update.CallbackQuery.Data)

	response := m.handler.HandleCallback(update)

	if response == nil {
		log.Printf("[BotModule] No response from callback handler")
		m.messageEditor.AnswerCallback(update.CallbackQuery.ID, "", false)
		return
	}

	log.Printf("[BotModule] Handler response: ShowAlert=%v, Text=%s", response.ShowAlert, response.Text)

	// Answer the callback query
	// If there's alert text, show it; otherwise just acknowledge
	answerText := response.Text
	if response.ShowAlert && len(answerText) > 200 {
		// Telegram alert text limit is 200 characters
		answerText = answerText[:197] + "..."
	}
	if answerText == "" {
		answerText = ""
	}
	err := m.messageEditor.AnswerCallback(update.CallbackQuery.ID, answerText, response.ShowAlert)
	if err != nil {
		log.Printf("[BotModule] AnswerCallback error: %v", err)
	}

	log.Printf("[BotModule] Callback response: EditMode=%v, TextLen=%d, KeyboardLen=%d",
		response.EditMode,
		len(response.Text),
		len(response.Keyboard))

	// Edit message if needed
	if response.EditMode {
		chatID := update.CallbackQuery.Message.Chat.ID
		messageID := update.CallbackQuery.Message.MessageID
		log.Printf("[BotModule] Editing message: chatID=%d, messageID=%d", chatID, messageID)
		m.messageEditor.EditMessage(chatID, messageID, response.Text, response.Keyboard)
	}
}

// Chain handlers

func (m *BotModule) handleSearchRequest(userID int64, query string, page int) (*SearchResult, error) {
	result, err := m.searchChain.SearchByTitle(query, page)
	if err != nil {
		return nil, err
	}

	// Convert to bot format - use chain.SearchItem directly
	items := make([]session.SearchItem, len(result.Items))
	for i, item := range result.Items {
		year := 0
		if item.ReleaseDate != "" && len(item.ReleaseDate) >= 4 {
			fmt.Sscanf(item.ReleaseDate[:4], "%d", &year)
		}

		title := item.Title
		if title == "" {
			title = item.Name
		}

		// Debug logging
		log.Printf("[BotModule] Converting item: ID=%d, Title='%s', Name='%s', MediaType='%s', ReleaseDate='%s'",
			item.ID, item.Title, item.Name, item.MediaType, item.ReleaseDate)

		// Use media type from search results
		mediaType := item.MediaType
		// If media type is empty, try to guess from other data
		if mediaType == "" && item.Name != "" && item.Title == "" {
			// TV shows often have 'name' but no 'title'
			mediaType = "tv"
		} else if mediaType == "" && item.Title != "" {
			// Movies typically have 'title'
			mediaType = "movie"
		}

		items[i] = session.SearchItem{
			ID:       strconv.Itoa(item.ID),
			Title:    title,
			Year:     year,
			Type:     mediaType,
			Poster:   item.PosterPath,
			Rating:   item.VoteAverage,
		}
	}

	return &SearchResult{
		Query:    result.Query,
		Page:     result.Page,
		PageSize: result.PageSize,
		Total:    result.Total,
		Items:    items,
	}, nil
}

func (m *BotModule) handleSubscribeRequest(userID int64, mediaID string, season int) error {
	// Parse mediaID (format: "type:id")
	var mediaType string
	var id int
	var err error

	// Find colon separator
	colonIdx := -1
	for i, c := range mediaID {
		if c == ':' {
			colonIdx = i
			break
		}
	}

	if colonIdx > 0 {
		mediaType = mediaID[:colonIdx]
		id, err = strconv.Atoi(mediaID[colonIdx+1:])
		if err != nil {
			return fmt.Errorf("invalid media ID format: %s", mediaID)
		}
	} else {
		// Try parsing as int directly
		id, err = strconv.Atoi(mediaID)
		if err != nil {
			return fmt.Errorf("invalid media ID: %s", mediaID)
		}
		mediaType = "movie" // Default
	}

	log.Printf("[BotModule] Subscribe: userID=%d, mediaType=%s, id=%d", userID, mediaType, id)

	// Get Jellyseerr user ID from mapping
	// This is needed for Jellyseerr API to create the request
	jellyseerrUserID := m.getJellyseerrUserID(userID)
	if jellyseerrUserID == 0 {
		return fmt.Errorf("请先使用 /link 命令绑定账号")
	}

	// Create the subscription request
	result, err := m.subscribeChain.SubscribeWithUser(id, mediaType, nil, jellyseerrUserID)
	if err != nil {
		log.Printf("[BotModule] Subscribe failed: %v", err)
		return err
	}

	// Notify admins about the new request
	if result != nil && result.ID > 0 {
		// Get user info for notification
		username := m.getTelegramUsername(userID)
		requestID := strconv.Itoa(result.ID)

		// 🎯 获取媒体标题（从 session）
		mediaTitle := ""
		userSession := m.GetSession(userID)
		if userSession != nil {
			if selectedItem := userSession.GetSelectedItem(); selectedItem != nil {
				mediaTitle = selectedItem.Title
			}
		}
		// 降级：使用 ID 作为标题
		if mediaTitle == "" {
			mediaTitle = fmt.Sprintf("ID:%d", id)
		}

		log.Printf("[BotModule] Notifying admins: requestID=%s, title=%s, media=%s, user=%s", requestID, mediaTitle, mediaType, username)
		m.notifyAdmins(mediaTitle, mediaType, username, requestID)
	} else {
		log.Printf("[BotModule] Request created but no valid ID result, skipping admin notification")
	}

	return nil
}

// getTelegramUsername gets the Telegram username for a user ID
// Uses the global UserSyncGetter instead of reading file directly
func (m *BotModule) getTelegramUsername(userID int64) string {
	if globalUserSyncGetter != nil {
		if username := globalUserSyncGetter.GetTelegramUsername(userID); username != "" {
			return username
		}
	}
	return fmt.Sprintf("User_%d", userID)
}

func (m *BotModule) handleDownloadRequest(userID int64, torrentID string) error {
	_, err := m.downloadChain.DownloadFromURL(torrentID)
	return err
}

// GetSession returns a user session
func (m *BotModule) GetSession(userID int64) *session.UserSession {
	return m.sessionManager.GetSession(userID)
}

// GetActiveSessionCount returns the number of active sessions
func (m *BotModule) GetActiveSessionCount() int {
	return m.sessionManager.GetActiveSessionCount()
}

// SetAdminChecker sets the admin checker function for the handler and chat system
func (m *BotModule) SetAdminChecker(fn func(int64) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handler.SetAdminChecker(fn)
	if m.chatSystem != nil {
		m.chatSystem.SetAdminChecker(fn)
		log.Printf("[BotModule] Admin checker set for both handler and chatSystem")
	}
}

// SetAdminNotifier sets the admin notification function
func (m *BotModule) SetAdminNotifier(notifier AdminNotifier) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.adminNotifier = notifier
	log.Printf("[BotModule] Admin notifier set")
}

// SetAdmins sets the admin list
func (m *BotModule) SetAdmins(admins map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.admins = admins
	log.Printf("[BotModule] Admin list updated: %d admins", len(admins))
}

// notifyAdmins sends a notification to all admins about a new request
// 优先使用全局 adminNotifier（它使用令牌系统），如果没有则使用旧方法
func (m *BotModule) notifyAdmins(mediaTitle, mediaType, username, requestID string) {
	m.mu.RLock()
	notifier := m.adminNotifier
	m.mu.RUnlock()

	// 优先使用全局 adminNotifier（它使用令牌系统）
	if notifier != nil {
		log.Printf("[BotModule] Using global adminNotifier with token system: requestID=%s", requestID)
		notifier(mediaTitle, mediaType, username, requestID)
		return
	}

	// 降级到旧方法
	m.mu.RLock()
	botToken := m.botToken
	admins := m.admins
	m.mu.RUnlock()

	if botToken == "" {
		log.Printf("[BotModule] No bot token configured, skipping notification")
		return
	}

	if len(admins) == 0 {
		log.Printf("[BotModule] No admins configured, skipping notification")
		return
	}

	log.Printf("[BotModule] Sending admin notification (legacy): requestID=%s, mediaType=%s, user=%s", requestID, mediaType, username)

	// Determine emoji
	mediaEmoji := "📀"
	if mediaType == "movie" {
		mediaEmoji = "🎬"
	} else if mediaType == "tv" {
		mediaEmoji = "📺"
	}

	// Create message
	msg := fmt.Sprintf("%s *新求片请求*\n\n", mediaEmoji)
	if mediaTitle != "" {
		msg += fmt.Sprintf("📦 %s\n", mediaTitle)
	}
	msg += fmt.Sprintf("👤 %s 请求\n", username)
	if requestID != "" {
		msg += fmt.Sprintf("🆔 ID: %s", requestID)
	}
	msg += fmt.Sprintf("\n\n请选择操作：")

	// Create inline keyboard (旧格式)
	keyboard := map[string]interface{}{
		"inline_keyboard": [][]map[string]string{
			{
				{"text": "✅ 批准", "callback_data": fmt.Sprintf("approve_%s", requestID)},
				{"text": "❌ 拒绝", "callback_data": fmt.Sprintf("decline_%s", requestID)},
			},
		},
	}

	// Send to each admin
	for adminID := range admins {
		go m.sendToAdmin(adminID, msg, keyboard)
	}
}

// sendToAdmin sends a message to a specific admin
func (m *BotModule) sendToAdmin(adminID, msg string, keyboard map[string]interface{}) {
	m.mu.RLock()
	botToken := m.botToken
	m.mu.RUnlock()

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	payload := map[string]interface{}{
		"chat_id":      adminID,
		"text":         msg,
		"reply_markup": keyboard,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[BotModule] Failed to marshal admin message: %v", err)
		return
	}

	resp, err := httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("[BotModule] Failed to send admin notification to %s: %v", adminID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[BotModule] Telegram API error for admin %s: status=%d, response=%s", adminID, resp.StatusCode, string(body))
	} else {
		log.Printf("[BotModule] Admin notification sent successfully to %s", adminID)
	}
}

// GinRoute returns the Gin route handler for webhook
func (m *BotModule) GinRoute() gin.HandlerFunc {
	return func(c *gin.Context) {
		var data []byte
		if c.Request.Body != nil {
			data, _ = c.GetRawData()
		}

		update, err := JSONToUpdate(data)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid JSON"})
			return
		}

		if update.Message != nil {
			go m.HandleMessage(update)
		} else if update.CallbackQuery != nil {
			go m.HandleCallback(update)
		}

		c.JSON(200, gin.H{"status": "ok"})
	}
}

// Stop stops the bot module
func (m *BotModule) Stop() {
	m.handler.Stop()
	m.sessionManager.Stop()
}

// getJellyseerrUserID gets the Jellyseerr user ID for a Telegram user
// Uses the global UserSyncGetter instead of reading file directly
func (m *BotModule) getJellyseerrUserID(telegramID int64) int {
	if globalUserSyncGetter == nil {
		log.Printf("[BotModule] UserSyncGetter not initialized, falling back to file read")
		return m.getJellyseerrUserIDFromFile(telegramID)
	}

	jid, exists := globalUserSyncGetter.GetJellyseerrUserID(telegramID)
	if exists {
		log.Printf("[BotModule] Found mapping via UserSyncGetter: telegramID=%d -> jellyseerrID=%d", telegramID, jid)
		return int(jid)
	}

	log.Printf("[BotModule] No mapping found via UserSyncGetter: telegramID=%d", telegramID)
	return 0
}

// getJellyseerrUserIDFromFile is a fallback method that reads directly from file
// This should only be used when UserSyncGetter is not available
func (m *BotModule) getJellyseerrUserIDFromFile(telegramID int64) int {
	// Try to read from user_mappings.json
	type MappingData struct {
		TelegramToJellyseerr map[int64]int64 `json:"telegramToJellyseerr"`
		JellyseerrToTelegram map[int64]int64 `json:"jellyseerrToTelegram"`
		TelegramUsernames    map[int64]string `json:"telegramUsernames"`
	}

	data, err := os.ReadFile("user_mappings.json")
	if err != nil {
		log.Printf("[BotModule] Failed to read user mappings: %v", err)
		return 0
	}

	var mappings MappingData
	if err := json.Unmarshal(data, &mappings); err != nil {
		log.Printf("[BotModule] Failed to parse user mappings: %v", err)
		return 0
	}

	if jid, exists := mappings.TelegramToJellyseerr[telegramID]; exists {
		log.Printf("[BotModule] Found mapping from file: telegramID=%d -> jellyseerrID=%d", telegramID, jid)
		return int(jid)
	}

	log.Printf("[BotModule] No mapping found in file: telegramID=%d", telegramID)
	return 0
}
