package bot

import (
	"fmt"
	"log"
	"strings"
	"time"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/config"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
	"emby-telegram-bot/pkg/types"
)

// Dependencies holds bot dependencies
type Dependencies struct {
	Telegram          *services.TelegramClient
	MoviePilot        *services.MoviePilotClient
	SessionMgr        *session.Manager
	UserMapping       *services.UserMappingService
	BindingRequest    *services.BindingRequestService
	AdminService      *services.AdminService
	QuotaService      *services.QuotaService
	ChatService       *services.ChatService
}

// PollDeps holds dependencies for polling (reduced set)
type PollDeps struct {
	Telegram       *services.TelegramClient
	MoviePilot     *services.MoviePilotClient
	SessionMgr     *session.Manager
	UserMapping    *services.UserMappingService
	BindingRequest *services.BindingRequestService
	AdminService   *services.AdminService
	QuotaService   *services.QuotaService
	ChatService    *services.ChatService
}

// StartPolling starts the Telegram update polling
func StartPolling(deps *Dependencies, cfg *config.Config, registry *callback.Registry) {
	if cfg.WebhookURL != "" {
		return // Don't poll if webhook is configured
	}

	log.Println("🔄 Starting Telegram updates polling...")

	offset := 0
	pollInterval := 1 * time.Second

	for {
		time.Sleep(pollInterval)

		updates, err := deps.Telegram.GetUpdates(offset, 100)
		if err != nil {
			log.Printf("[Poll] Failed to get updates: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		if len(updates) == 0 {
			continue
		}

		log.Printf("[Poll] Received %d updates", len(updates))

		for _, update := range updates {
			// Update offset
			if update.UpdateID > 0 {
				offset = int(update.UpdateID + 1)
			}

			// Process update
			if update.Message != nil {
				HandlePollMessage(update.Message, deps, cfg)
			} else if update.CallbackQuery != nil {
				HandleCallbackQuery(update.CallbackQuery, registry, deps.Telegram)
			}
		}
	}
}

// HandlePollMessage processes a message update (for polling)
func HandlePollMessage(msg *types.TelegramMessage, deps *Dependencies, cfg *config.Config) {
	log.Printf("[Poll] Message from %d: %s", msg.From.ID, msg.Text)

	// Group chat: Only AI chat is allowed
	if msg.Chat.Type != "private" {
		HandleGroupChatMessage(msg, deps.ChatService, deps.Telegram)
		return
	}

	// Private chat: Handle commands and search query
	if strings.HasPrefix(msg.Text, "/") {
		HandleCommand(deps.Telegram, msg, cfg, deps.AdminService, deps.BindingRequest)
		return
	}

	// Handle search query (non-command text)
	if msg.Text != "" && len(msg.Text) > 1 {
		HandlePollSearchQuery(msg, deps.Telegram, deps.MoviePilot, deps.SessionMgr)
	}
}

// HandleGroupChatMessage handles messages in group chats
func HandleGroupChatMessage(msg *types.TelegramMessage, chatService *services.ChatService, telegram *services.TelegramClient) {
	query := msg.Text
	isReplyToBot := msg.ReplyToMessage != nil && msg.ReplyToMessage.From.IsBot
	isMention := strings.Contains(strings.ToLower(query), "@oceancloudying_bot") ||
		strings.Contains(strings.ToLower(query), "@云海看板娘")

	chatType := services.ChatTypeGroup
	if msg.Chat.Type == "supergroup" {
		chatType = services.ChatTypeSupergroup
	}

	userName := msg.From.FirstName
	if msg.From.Username != "" {
		userName = msg.From.Username
	}

	chatMsg := &services.ChatMessage{
		UserID:    msg.From.ID,
		UserName:  userName,
		Content:   query,
		IsReply:   isReplyToBot,
		IsMention: isMention,
		ChatType:  chatType,
		Timestamp: time.Now(),
	}

	log.Printf("[PollGroupChat] Message from %s: isMention=%v, isReply=%v",
		userName, isMention, isReplyToBot)

	// Only respond to mentions or replies
	if chatService.ShouldReply(chatMsg) {
		log.Printf("[PollGroupChat] ShouldReply=true, getting response...")
		response := chatService.GetResponse(chatMsg)
		log.Printf("[PollGroupChat] Got response: ShouldReply=%v, Text=%s",
			response.ShouldReply, response.Text)
		if response.ShouldReply && response.Text != "" {
			telegram.SendMessage(msg.Chat.ID, response.Text, "", nil)
		}
	}
}

// HandlePollSearchQuery handles search queries (for polling)
func HandlePollSearchQuery(msg *types.TelegramMessage, telegram *services.TelegramClient, moviepilot *services.MoviePilotClient, sessMgr *session.Manager) {
	query := msg.Text

	// Search in MoviePilot
	results, err := moviepilot.SearchMedia(query, 1)
	if err != nil {
		telegram.SendMessage(msg.Chat.ID, fmt.Sprintf("❌ 搜索失败: %v", err), "Markdown", nil)
		return
	}

	if len(results.Results) == 0 {
		telegram.SendMessage(msg.Chat.ID, "😕 未找到相关内容\n\n💡 建议：\n• 检查拼写是否正确\n• 尝试使用更简短的关键词\n• 尝试使用英文搜索", "Markdown", nil)
		return
	}

	// Store search results in session
	sess := sessMgr.GetOrCreate(msg.From.ID)
	searchItems := make([]session.SearchItem, len(results.Results))
	for i, item := range results.Results {
		mediaType := "movie"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaType = "tv"
		}
		searchItems[i] = session.SearchItem{
			ID:       fmt.Sprintf("%d", item.ID),
			Title:    item.Title,
			Year:     item.Year.Int(),
			Type:     mediaType,
			Poster:   item.Poster,
			Rating:   item.Rating,
			Overview: item.Overview,
		}
	}
	sess.SetSearchResults(searchItems, 1, query)
	log.Printf("[PollSearch] Stored %d search results in session for user %d", len(searchItems), msg.From.ID)

	// Build search results message
	text := fmt.Sprintf("🔍 搜索结果「%s」\n\n找到 %d 条结果\n\n",
		query, len(results.Results))

	// Build keyboard with results
	var keyboardRows [][]types.TelegramInlineKeyboardButton
	var row []types.TelegramInlineKeyboardButton

	for i, item := range results.Results {
		if i >= 8 { // Limit to 8 results per page
			break
		}

		year := ""
		if item.Year > 0 {
			year = fmt.Sprintf("%d", item.Year)
		}

		rating := ""
		if item.Rating > 0 {
			rating = fmt.Sprintf(" ⭐%.1f", item.Rating)
		}

		mediaType := "🎬 电影"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaType = "📺 剧集"
		}
		text += fmt.Sprintf("%d. %s (%s) %s%s\n", i+1, item.Title, year, mediaType, rating)

		row = append(row, types.TelegramInlineKeyboardButton{
			Text:         fmt.Sprintf("%d", i+1),
			CallbackData: fmt.Sprintf("select:id:%d:type:%s", item.ID, item.Type),
		})

		if len(row) == 4 {
			keyboardRows = append(keyboardRows, row)
			row = []types.TelegramInlineKeyboardButton{}
		}
	}

	if len(row) > 0 {
		keyboardRows = append(keyboardRows, row)
	}

	navRow := []types.TelegramInlineKeyboardButton{
		{Text: "⬅️ 返回主菜单", CallbackData: "start"},
	}
	if len(results.Results) >= 20 {
		navRow = append(navRow, types.TelegramInlineKeyboardButton{
			Text:         "➡️ 下一页",
			CallbackData: fmt.Sprintf("search:query:%s:page:2", query),
		})
	}
	keyboardRows = append(keyboardRows, navRow)

	keyboard := &types.TelegramInlineKeyboard{
		InlineKeyboard: keyboardRows,
	}

	telegram.SendMessage(msg.Chat.ID, text, "", keyboard)
}

// HandleCallbackQuery processes a callback query (for polling)
func HandleCallbackQuery(cb *types.TelegramCallbackQuery, registry *callback.Registry, telegram *services.TelegramClient) {
	log.Printf("[Poll] Callback from user %d: %s", cb.From.ID, cb.Data)

	// Parse callback
	parsed, err := registry.Parser().Parse(cb.Data)
	if err != nil {
		log.Printf("Failed to parse callback: %v", err)
		telegram.AnswerCallback(cb.ID, "无效的请求", true)
		return
	}

	// Build context
	ctx := &callback.Context{
		UserID:     cb.From.ID,
		ChatID:     cb.Message.Chat.ID,
		MessageID:  cb.Message.MessageID,
		CallbackID: cb.ID,
		Callback:   parsed,
	}

	// Get handler
	handler, exists := registry.Get(parsed.Action)
	if !exists {
		log.Printf("No handler for action: %s", parsed.Action)
		telegram.AnswerCallback(cb.ID, "未知操作", true)
		return
	}

	// Handle callback
	resp, err := handler.Handle(ctx)

	// Answer callback query
	callbackMsg := ""
	showAlert := false
	if resp != nil {
		if resp.ShowAlert && resp.Text != "" {
			callbackMsg = resp.Text
		} else {
			callbackMsg = resp.CallbackMsg
		}
		showAlert = resp.ShowAlert
	}

	if err != nil {
		log.Printf("Handler error: %v", err)
		if callbackMsg == "" {
			callbackMsg = "操作失败"
		}
		showAlert = true
	}

	if showAlert && len(callbackMsg) > 200 {
		callbackMsg = callbackMsg[:197] + "..."
	}

	if err := telegram.AnswerCallback(cb.ID, callbackMsg, showAlert); err != nil {
		log.Printf("[Callback] AnswerCallback error: %v", err)
	}

	// Edit message if needed
	if resp != nil && resp.Edit && resp.Text != "" {
		keyboard := ConvertKeyboard(resp.Keyboard)
		telegram.EditMessage(ctx.ChatID, ctx.MessageID, resp.Text, "Markdown", keyboard)
	}
}

// ConvertKeyboard converts callback Keyboard to TelegramInlineKeyboard
func ConvertKeyboard(kb *callback.Keyboard) *types.TelegramInlineKeyboard {
	if kb == nil {
		return nil
	}

	result := &types.TelegramInlineKeyboard{
		InlineKeyboard: make([][]types.TelegramInlineKeyboardButton, len(kb.InlineKeyboard)),
	}

	for i, row := range kb.InlineKeyboard {
		result.InlineKeyboard[i] = make([]types.TelegramInlineKeyboardButton, len(row))
		for j, btn := range row {
			result.InlineKeyboard[i][j] = types.TelegramInlineKeyboardButton{
				Text:         btn.Text,
				CallbackData: btn.CallbackData,
				URL:          btn.URL,
			}
		}
	}

	return result
}
