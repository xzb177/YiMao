package bot

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"
	"emby-telegram-bot/chain"
	"emby-telegram-bot/session"
)

// Global mutex for user_mappings.json file access
var userMappingsMutex sync.RWMutex

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

	_, err = m.subscribeChain.SubscribeWithUser(id, mediaType, nil, jellyseerrUserID)
	return err
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
func (m *BotModule) getJellyseerrUserID(telegramID int64) int {
	// Use read lock for concurrent safe file reading
	userMappingsMutex.RLock()
	defer userMappingsMutex.RUnlock()

	// Try to read from user_mappings.json
	type MappingData struct {
		TelegramToJellyseerr map[string]int `json:"telegramToJellyseerr"`
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

	// Convert telegramID to string for lookup
	telegramIDStr := fmt.Sprintf("%d", telegramID)
	if jid, exists := mappings.TelegramToJellyseerr[telegramIDStr]; exists {
		log.Printf("[BotModule] Found mapping: telegramID=%d -> jellyseerrID=%d", telegramID, jid)
		return jid
	}

	log.Printf("[BotModule] No mapping found for telegramID=%d", telegramID)
	return 0
}
