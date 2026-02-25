package bot

import (
	"fmt"
	"log"
	"strings"
	"time"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/config"
	"emby-telegram-bot/internal/handlers"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
	"emby-telegram-bot/pkg/types"
	"emby-telegram-bot/pkg/validation"
)

// Dependencies holds bot dependencies
type Dependencies struct {
	Telegram        *services.TelegramClient
	MoviePilot      *services.MoviePilotClient
	SessionMgr      *session.Manager
	UserMapping     *services.UserMappingService
	BindingRequest  *services.BindingRequestService
	AdminService    *services.AdminService
	QuotaService    *services.QuotaService
	SearchHistory   *services.SearchHistoryService
	TMDB            *services.TMDBClient
	IssueService    *services.IssueService
	FeedbackHandler *handlers.FeedbackHandler
}

// PollDeps holds dependencies for polling (reduced set)
type PollDeps struct {
	Telegram        *services.TelegramClient
	MoviePilot      *services.MoviePilotClient
	SessionMgr      *session.Manager
	UserMapping     *services.UserMappingService
	BindingRequest  *services.BindingRequestService
	AdminService    *services.AdminService
	QuotaService    *services.QuotaService
	SearchHistory   *services.SearchHistoryService
	TMDB            *services.TMDBClient
	IssueService    *services.IssueService
	FeedbackHandler *handlers.FeedbackHandler
}

// StartPolling starts the Telegram update polling
func StartPolling(deps *Dependencies, cfg *config.Config, registry *callback.Registry) {
	if cfg.WebhookURL != "" {
		return // Don't poll if webhook is configured
	}

	log.Println("🔄 Starting Telegram updates polling...")

	offset := 0
	pollInterval := 1 * time.Second

	// Convert to PollDeps
	pollDeps := &PollDeps{
		Telegram:       deps.Telegram,
		MoviePilot:     deps.MoviePilot,
		SessionMgr:     deps.SessionMgr,
		UserMapping:    deps.UserMapping,
		BindingRequest: deps.BindingRequest,
		AdminService:   deps.AdminService,
		QuotaService:   deps.QuotaService,
		SearchHistory:  deps.SearchHistory,
		TMDB:           deps.TMDB,
		IssueService:   deps.IssueService,
		FeedbackHandler: deps.FeedbackHandler,
	}

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
			// Update offset immediately to avoid reprocessing
			if update.UpdateID > 0 {
				offset = int(update.UpdateID + 1)
			}

			// Process update in goroutine to avoid blocking
			update := update // Capture loop variable
			go func() {
				// Debug: log update type
				if update.Message != nil {
					log.Printf("[Poll] Update %d: Message from %d: %s", update.UpdateID, update.Message.From.ID, update.Message.Text)
					HandlePollMessage(update.Message, pollDeps, cfg)
				} else if update.CallbackQuery != nil {
					log.Printf("[Poll] Update %d: Callback from %d: %s", update.UpdateID, update.CallbackQuery.From.ID, update.CallbackQuery.Data)
					HandleCallbackQuery(update.CallbackQuery, registry, deps.Telegram)
				} else {
					log.Printf("[Poll] Update %d: Empty update (no message or callback)", update.UpdateID)
				}
			}()
		}
	}
}

// HandlePollMessage processes a message update (for polling)
func HandlePollMessage(msg *types.TelegramMessage, deps *PollDeps, cfg *config.Config) {
	// Sanitize input text
	sanitizedText := validation.SanitizeMessageText(msg.Text)
	log.Printf("[Poll] Message from %d: %s", msg.From.ID, sanitizedText)

	// Group chat: handle search queries only
	if msg.Chat.Type != "private" {
		HandleGroupChatMessage(msg, deps.Telegram, deps.MoviePilot, deps.SessionMgr, deps.SearchHistory, deps.TMDB)
		return
	}

	// Check if user is in feedback process
	log.Printf("[Poll] Checking feedback process for user %d, FeedbackHandler=%v", msg.From.ID, deps.FeedbackHandler != nil)
	if deps.FeedbackHandler != nil {
		inFeedback := deps.FeedbackHandler.IsInFeedbackProcess(msg.From.ID)
		log.Printf("[Poll] User %d in feedback process: %v", msg.From.ID, inFeedback)
		if inFeedback {
			if err := deps.FeedbackHandler.HandleFeedbackText(msg.From.ID, msg.Chat.ID, sanitizedText); err != nil {
				log.Printf("[Poll] Failed to handle feedback: %v", err)
			}
			return
		}
	}

	// Private chat: Handle commands and search query
	if strings.HasPrefix(sanitizedText, "/") {
		msg.Text = sanitizedText // Update with sanitized text
		HandleCommand(deps.Telegram, msg, cfg, deps.AdminService, deps.BindingRequest, deps.QuotaService, deps.UserMapping)
		return
	}

	// Handle search query (non-command text)
	if sanitizedText != "" && len(sanitizedText) > 1 {
		msg.Text = sanitizedText // Update with sanitized text
		HandlePollSearchQuery(msg, deps.Telegram, deps.MoviePilot, deps.SessionMgr, deps.SearchHistory, deps.TMDB)
	}
}

// HandleGroupChatMessage handles messages in group chats
func HandleGroupChatMessage(msg *types.TelegramMessage, telegram *services.TelegramClient, moviepilot *services.MoviePilotClient, sessMgr *session.Manager, searchHistory *services.SearchHistoryService, tmdb *services.TMDBClient) {
	query := validation.SanitizeMessageText(msg.Text)
	isMention := strings.Contains(strings.ToLower(query), "@oceancloudying_bot") ||
		strings.Contains(strings.ToLower(query), "@云海看板娘")

	log.Printf("[PollGroupChat] ChatID=%d, Title=%s, Message from %s: isMention=%v", msg.Chat.ID, msg.Chat.Title, msg.From.FirstName, isMention)

	// Only respond to mentions
	if !isMention {
		return
	}

	// Remove mention from query
	query = strings.ReplaceAll(query, "@oceancloudying_bot", "")
	query = strings.ReplaceAll(query, "@云海看板娘", "")
	query = strings.TrimSpace(query)

	// Sanitize the query after removing mentions
	query = validation.SanitizeSearchQuery(query)

	if query == "" {
		telegram.SendMessage(msg.Chat.ID, "你好！请问有什么我可以帮助你的？", "", nil)
		return
	}

	// Handle /ai command - send recommendation menu
	if query == "/ai" {
		sendRecommendationMenu(telegram, msg.Chat.ID)
		return
	}

	// Treat as search query
	msg.Text = query
	HandlePollSearchQuery(msg, telegram, moviepilot, sessMgr, searchHistory, tmdb)
}

// sendRecommendationMenu sends the recommendation menu
func sendRecommendationMenu(telegram *services.TelegramClient, chatID int64) {
	msg := services.NewMessageBuilder()
	msg.Bold("🎬 精选推荐").Newline()
	msg.Newline()
	msg.Text("发现你喜欢的精彩内容").Newline()
	msg.Newline()
	msg.Italic("💡 选择推荐类型开始探索")

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🔥 本周热门", "search:type:trending")
	kb.AddButton("📺 热门剧集", "search:type:hot")
	kb.NewRow()
	kb.AddButton("⭐ 必看神作", "search:type:toprated")
	kb.AddButton("🆕 最新上映", "search:type:new")
	kb.NewRow()
	kb.AddButton("🎲 随机探索", "search:type:random")
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	telegram.SendMessage(chatID, msg.Build(), "", kb.Build())
}

// HandlePollSearchQuery handles search queries (for polling)
func HandlePollSearchQuery(msg *types.TelegramMessage, telegram *services.TelegramClient, moviepilot *services.MoviePilotClient, sessMgr *session.Manager, searchHistory *services.SearchHistoryService, tmdb *services.TMDBClient) {
	// Sanitize search query
	query := validation.SanitizeSearchQuery(msg.Text)

	// Add to search history
	if searchHistory != nil && query != "" {
		searchHistory.AddSearch(msg.From.ID, query)
	}

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

		searchItem := session.SearchItem{
			ID:       fmt.Sprintf("%d", item.ID),
			Title:    item.Title,
			Year:     item.Year.Int(),
			Type:     mediaType,
			Poster:   item.Poster,
			Rating:   item.Rating,
			Overview: item.Overview,
		}

		// For TV shows, try to fetch season info from TMDB (with timeout)
		if mediaType == "tv" && item.ID > 0 && tmdb != nil {
			// Fetch seasons with timeout
			type seasonResult struct {
				seasons []session.Season
				err     error
			}
			resultChan := make(chan seasonResult, 1)

			go func(tmdbID int) {
				tvDetails, err := tmdb.GetTVDetailsWithSeasons(tmdbID)
				if err != nil {
					log.Printf("[PollSearch] Failed to fetch seasons from TMDB for %s: %v", item.Title, err)
					resultChan <- seasonResult{err: err}
					return
				}
				seasons := make([]session.Season, 0, len(tvDetails.Seasons))
				for _, s := range tvDetails.Seasons {
					// Skip season 0 (specials) if desired, or include it
					seasons = append(seasons, session.Season{
						SeasonNumber: s.SeasonNumber,
						EpisodeCount: s.EpisodeCount,
						Name:         s.Name,
					})
				}
				resultChan <- seasonResult{seasons: seasons}
			}(item.ID)

			select {
			case result := <-resultChan:
				if result.err == nil && len(result.seasons) > 0 {
					searchItem.Seasons = result.seasons
				}
			case <-time.After(3 * time.Second):
				// Timeout, silently skip season info
			}
		}

		searchItems[i] = searchItem
	}

	// 统计获取到的季数信息
	seasonCount := 0
	for _, item := range searchItems {
		if len(item.Seasons) > 0 {
			seasonCount++
		}
	}
	if seasonCount > 0 {
		log.Printf("[搜索] 获取到 %d/%d 部剧集的季数信息", seasonCount, len(searchItems))
	}

	sess.SetSearchResults(searchItems, 1, query)
	log.Printf("[搜索] 查询 \"%s\": %d 条结果", query, len(results.Results))

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
	// Sanitize callback data
	sanitizedData := validation.SanitizeCallbackData(cb.Data)
	log.Printf("[Poll] Callback from user %d: %s", cb.From.ID, sanitizedData)

	// Parse callback
	parsed, err := registry.Parser().Parse(sanitizedData)
	if err != nil {
		log.Printf("Failed to parse callback: %v", err)
		if ansErr := telegram.AnswerCallback(cb.ID, "无效的请求", true); ansErr != nil {
			log.Printf("[Callback] Failed to answer callback (parse error): %v", ansErr)
		}
		return
	}

	// Build context
	ctx := &callback.Context{
		UserID:     cb.From.ID,
		ChatID:     cb.Message.Chat.ID,
		ChatType:   cb.Message.Chat.Type,
		MessageID:  cb.Message.MessageID,
		CallbackID: cb.ID,
		Callback:   parsed,
	}

	// Get handler
	handler, exists := registry.Get(parsed.Action)
	if !exists {
		log.Printf("No handler for action: %s", parsed.Action)
		if ansErr := telegram.AnswerCallback(cb.ID, "未知操作", true); ansErr != nil {
			log.Printf("[Callback] Failed to answer callback (no handler): %v", ansErr)
		}
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
		// Log but don't fail - callback may have expired (common with slow handlers)
		// Telegram callbacks expire after a few seconds
		log.Printf("[Callback] AnswerCallback error (callback may have expired): %v", err)
	}

	// Send or edit message
	if resp != nil {
		keyboard := ConvertKeyboard(resp.Keyboard)

		// Check if we need to send a photo
		if resp.Photo != "" {
			// Delete the original message first
			if delErr := telegram.DeleteMessage(ctx.ChatID, ctx.MessageID); delErr != nil {
				log.Printf("[Callback] DeleteMessage error: %v", delErr)
			}
			// Send photo with caption and keyboard
			caption := resp.PhotoCaption
			if caption == "" {
				caption = resp.Text
			}
			if _, sendErr := telegram.SendPhoto(ctx.ChatID, resp.Photo, caption, keyboard); sendErr != nil {
				log.Printf("[Callback] SendPhoto error: %v", sendErr)
			}
		} else if resp.Text != "" {
			if resp.DeleteMessage {
				// Delete current message and send new one
				if delErr := telegram.DeleteMessage(ctx.ChatID, ctx.MessageID); delErr != nil {
					log.Printf("[Callback] DeleteMessage error: %v", delErr)
				}
				if _, sendErr := telegram.SendMessage(ctx.ChatID, resp.Text, "", keyboard); sendErr != nil {
					log.Printf("[Callback] SendMessage error: %v", sendErr)
				}
			} else if resp.Edit {
				// Edit existing message
				if _, editErr := telegram.EditMessage(ctx.ChatID, ctx.MessageID, resp.Text, "Markdown", keyboard); editErr != nil {
					log.Printf("[Callback] EditMessage error: %v", editErr)
				}
			} else {
				// Send new message
				if _, sendErr := telegram.SendMessage(ctx.ChatID, resp.Text, "", keyboard); sendErr != nil {
					log.Printf("[Callback] SendMessage error: %v", sendErr)
				}
			}
		}
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
