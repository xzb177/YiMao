package bot

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/config"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
	"emby-telegram-bot/pkg/types"
)

// HandleWebhook handles incoming Telegram webhook
func HandleWebhook(
	w http.ResponseWriter,
	r *http.Request,
	registry *callback.Registry,
	deps *Dependencies,
	cfg *config.Config,
) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse update
	var update types.TelegramUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		log.Printf("Failed to decode update: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Route update
	if update.CallbackQuery != nil {
		HandleWebhookCallback(w, &update, registry, deps.Telegram, deps.SessionMgr, cfg, deps.AdminService)
	} else if update.Message != nil {
		HandleWebhookMessage(w, &update, deps, cfg, registry)
	} else {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	}
}

// HandleWebhookCallback handles callback queries from webhook
func HandleWebhookCallback(
	w http.ResponseWriter,
	update *types.TelegramUpdate,
	registry *callback.Registry,
	telegram *services.TelegramClient,
	sessMgr *session.Manager,
	cfg *config.Config,
	adminService *services.AdminService,
) {
	cb := update.CallbackQuery
	log.Printf("[Webhook] Callback from user %d: %s", cb.From.ID, cb.Data)

	// Parse callback
	parsed, err := registry.Parser().Parse(cb.Data)
	if err != nil {
		log.Printf("Failed to parse callback: %v", err)
		telegram.AnswerCallback(cb.ID, "无效的请求", true)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
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
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
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

	// Send or edit message
	if resp != nil && resp.Text != "" {
		keyboard := ConvertKeyboard(resp.Keyboard)
		if resp.Edit {
			// Edit existing message
			telegram.EditMessage(ctx.ChatID, ctx.MessageID, resp.Text, "Markdown", keyboard)
		} else {
			// Send new message
			telegram.SendMessage(ctx.ChatID, resp.Text, "", keyboard)
		}
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "OK")
}

// HandleWebhookMessage handles messages from webhook
func HandleWebhookMessage(
	w http.ResponseWriter,
	update *types.TelegramUpdate,
	deps *Dependencies,
	cfg *config.Config,
	registry *callback.Registry,
) {
	msg := update.Message
	if msg == nil {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
		return
	}
	log.Printf("[Webhook] Message from user %d (chat: %s, type: %s): %s", msg.From.ID, msg.Chat.ID, msg.Chat.Type, msg.Text)

	// 群聊中只处理 AI 聊天
	if msg.Chat.Type != "private" {
		if len(msg.Text) > 1 {
			HandleWebhookGroupChat(deps.Telegram, msg, deps.ChatService)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
		return
	}

	// 私聊中处理所有功能

	// Handle commands
	if strings.HasPrefix(msg.Text, "/") {
		HandleCommand(deps.Telegram, msg, cfg, deps.AdminService, deps.BindingRequest, deps.QuotaService, deps.UserMapping)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
		return
	}

	// Handle search queries (non-command text)
	if len(msg.Text) > 1 {
		HandleWebhookTextQuery(deps.Telegram, msg, deps.SessionMgr, cfg, registry, deps.MoviePilot, deps.ChatService)
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "OK")
}

// HandleWebhookGroupChat handles group chat messages from webhook
func HandleWebhookGroupChat(telegram *services.TelegramClient, msg *types.TelegramMessage, chatService *services.ChatService) {
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

	if chatService.ShouldReply(chatMsg) {
		response := chatService.GetResponse(chatMsg)
		if response.ShouldReply && response.Text != "" {
			telegram.SendMessage(msg.Chat.ID, response.Text, "", nil)
		}
	}
}

// HandleWebhookTextQuery handles text queries from webhook
func HandleWebhookTextQuery(
	telegram *services.TelegramClient,
	msg *types.TelegramMessage,
	sessMgr *session.Manager,
	cfg *config.Config,
	registry *callback.Registry,
	moviepilot *services.MoviePilotClient,
	chatService *services.ChatService,
) {
	query := msg.Text

	// Check for reply_to_bot or mention
	isReplyToBot := msg.ReplyToMessage != nil && msg.ReplyToMessage.From.IsBot
	isMention := strings.Contains(strings.ToLower(query), "@oceancloudying_bot") ||
		strings.Contains(strings.ToLower(query), "@云海看板娘")

	chatType := services.ChatTypePrivate

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

	// Private chat: AI chat check
	if chatService.ShouldReply(chatMsg) {
		response := chatService.GetResponse(chatMsg)
		if response.ShouldReply && response.Text != "" {
			telegram.SendMessage(msg.Chat.ID, response.Text, "", nil)
		}
		return
	}

	// Check if it's an AI recommendation query
	if IsAIQuery(query) {
		SendAIMenu(telegram, msg.Chat.ID)
		return
	}

	// Otherwise, treat as search query
	go PerformSearch(telegram, msg, sessMgr, moviepilot)
}

// IsAIQuery checks if the query is an AI recommendation request
func IsAIQuery(query string) bool {
	aiKeywords := []string{"推荐", "有什么", "好看的", "想看", "来点", "给我", "热门", "trending"}
	for _, keyword := range aiKeywords {
		if strings.Contains(query, keyword) {
			return true
		}
	}
	return false
}

// PerformSearch performs the actual search in background
func PerformSearch(
	telegram *services.TelegramClient,
	msg *types.TelegramMessage,
	sessMgr *session.Manager,
	moviepilot *services.MoviePilotClient,
) {
	query := msg.Text

	results, err := moviepilot.SearchMedia(query, 1)
	if err != nil {
		log.Printf("[Search] Failed to search: %v", err)
		text := fmt.Sprintf("❌ 搜索失败\n\n错误: %v", err)
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	if len(results.Results) == 0 {
		text := "😕 未找到相关内容\n\n💡 建议：\n• 检查拼写是否正确\n• 尝试使用更简短的关键词\n• 尝试使用英文搜索"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	// Build search results message
	text := fmt.Sprintf("🔍 搜索结果「%s」\n\n找到 %d 条结果\n\n",
		query, len(results.Results))

	// Build keyboard with results
	var keyboardRows [][]types.TelegramInlineKeyboardButton
	var row []types.TelegramInlineKeyboardButton

	for i, item := range results.Results {
		if i >= 8 {
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

	// Save search results to session
	sess := sessMgr.GetOrCreate(msg.Chat.ID)
	searchItems := make([]session.SearchItem, 0)
	for i, item := range results.Results {
		if i >= 8 {
			break
		}
		searchItems = append(searchItems, session.SearchItem{
			ID:     fmt.Sprintf("%d", item.ID),
			Title:  item.Title,
			Year:   item.Year.Int(),
			Type:   string(item.Type),
			Rating: item.Rating,
		})
	}
	sess.SetSearchResults(searchItems, 1, query)

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

	keyboard := &types.TelegramInlineKeyboard{InlineKeyboard: keyboardRows}
	telegram.SendMessage(msg.Chat.ID, text, "", keyboard)
}

// SendAIMenu sends AI recommendation menu
func SendAIMenu(telegram *services.TelegramClient, chatID int64) {
	text := "🤖 AI 智能推荐\n\n请选择推荐类型："

	keyboard := &types.TelegramInlineKeyboard{
		InlineKeyboard: [][]types.TelegramInlineKeyboardButton{
			{
				{Text: "🔥 热门推荐", CallbackData: "ai:trending"},
				{Text: "📺 热播剧集", CallbackData: "ai:hot_tv"},
			},
			{
				{Text: "🎬 最新电影", CallbackData: "ai:new_movies"},
				{Text: "🎲 随机推荐", CallbackData: "ai:random"},
			},
			{
				{Text: "⬅️ 返回主菜单", CallbackData: "start"},
			},
		},
	}

	telegram.SendMessage(chatID, text, "", keyboard)
}
