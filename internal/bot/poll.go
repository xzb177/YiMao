package bot

import (
	"fmt"
	"strings"
	"time"

	"github.com/xzb177/yimao/ai"
	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/config"
	"github.com/xzb177/yimao/internal/handlers"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
	"github.com/xzb177/yimao/pkg/validation"
)

// Dependencies holds bot dependencies
type Dependencies struct {
	Telegram        *services.TelegramClient
	MoviePilot      *services.MoviePilotClient
	SessionMgr      *session.Manager
	UserMapping     *services.UserMappingService
	BindingRequest  *services.BindingRequestService
	AdminService    *services.AdminService
	AdminHandler    *handlers.AdminHandler
	QuotaService    *services.QuotaService
	SearchHistory   *services.SearchHistoryService
	SearchHistoryDB *services.SearchHistoryDB
	TMDB            *services.TMDBClient
	IssueService    *services.IssueService
	FeedbackHandler *handlers.FeedbackHandler
	FallbackService *services.SearchFallbackService
	WishHandler     *handlers.WishHandler       // #6 许愿池
	MyRequests      *handlers.MyRequestsHandler // 我的请求（/requests 命令复用）
}

// PollDeps holds dependencies for polling (reduced set)
type PollDeps struct {
	Telegram        *services.TelegramClient
	MoviePilot      *services.MoviePilotClient
	SessionMgr      *session.Manager
	UserMapping     *services.UserMappingService
	BindingRequest  *services.BindingRequestService
	AdminService    *services.AdminService
	AdminHandler    *handlers.AdminHandler // For admin management flows
	QuotaService    *services.QuotaService
	SearchHistory   *services.SearchHistoryService // Legacy, for backward compatibility
	SearchHistoryDB *services.SearchHistoryDB      // New, advanced features
	TMDB            *services.TMDBClient
	IssueService    *services.IssueService
	FeedbackHandler *handlers.FeedbackHandler
	FallbackService *services.SearchFallbackService
	WishHandler     *handlers.WishHandler       // #6 许愿池
	MyRequests      *handlers.MyRequestsHandler // 我的请求（/requests 命令复用）
}

// StartPolling starts the Telegram update polling
func StartPolling(deps *Dependencies, cfg *config.Config, registry *callback.Registry) {
	if cfg.WebhookURL != "" {
		return // Don't poll if webhook is configured
	}

	logger.Info("🔄 Starting Telegram updates polling...")

	offset := 0
	pollInterval := 1 * time.Second

	// Convert to PollDeps
	pollDeps := &PollDeps{
		Telegram:        deps.Telegram,
		MoviePilot:      deps.MoviePilot,
		SessionMgr:      deps.SessionMgr,
		UserMapping:     deps.UserMapping,
		BindingRequest:  deps.BindingRequest,
		AdminService:    deps.AdminService,
		AdminHandler:    deps.AdminHandler,
		QuotaService:    deps.QuotaService,
		SearchHistory:   deps.SearchHistory,
		SearchHistoryDB: deps.SearchHistoryDB,
		TMDB:            deps.TMDB,
		IssueService:    deps.IssueService,
		FeedbackHandler: deps.FeedbackHandler,
		FallbackService: services.NewSearchFallbackService(deps.MoviePilot),
		WishHandler:     deps.WishHandler,
		MyRequests:      deps.MyRequests,
	}

	for {
		time.Sleep(pollInterval)

		updates, err := deps.Telegram.GetUpdates(offset, 100)
		if err != nil {
			logger.Info("[Poll] Failed to get updates: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		if len(updates) == 0 {
			continue
		}

		logger.Info("[Poll] Received %d updates", len(updates))

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
					logger.Info("[Poll] Update %d: Message from %d: %s", update.UpdateID, update.Message.From.ID, update.Message.Text)
					HandlePollMessage(update.Message, pollDeps, cfg)
				} else if update.CallbackQuery != nil {
					logger.Info("[Poll] Update %d: Callback from %d: %s", update.UpdateID, update.CallbackQuery.From.ID, update.CallbackQuery.Data)
					HandleCallbackQuery(update.CallbackQuery, registry, deps.Telegram)
				} else {
					logger.Info("[Poll] Update %d: Empty update (no message or callback)", update.UpdateID)
				}
			}()
		}
	}
}

// HandlePollMessage processes a message update (for polling)
func HandlePollMessage(msg *types.TelegramMessage, deps *PollDeps, cfg *config.Config) {
	// Sanitize input text
	sanitizedText := validation.SanitizeMessageText(msg.Text)
	logger.Info("[Poll] Message from %d: %s", msg.From.ID, sanitizedText)

	// Group chat: handle search queries only
	if msg.Chat.Type != "private" {
		HandleGroupChatMessage(msg, deps.Telegram, deps.MoviePilot, deps.SessionMgr, deps.SearchHistory, deps.TMDB)
		return
	}

	// Check if user is feedback process
	logger.Info("[Poll] Checking feedback process for user %d, FeedbackHandler=%v", msg.From.ID, deps.FeedbackHandler != nil)
	if deps.FeedbackHandler != nil {
		inFeedback := deps.FeedbackHandler.IsInFeedbackProcess(msg.From.ID)
		logger.Info("[Poll] User %d in feedback process: %v", msg.From.ID, inFeedback)
		if inFeedback {
			// Check if user sent a photo with feedback
			var photoFileID string
			if msg.Photo != nil && len(msg.Photo) > 0 {
				// Get the largest photo (last element in array)
				photoFileID = msg.Photo[len(msg.Photo)-1].FileID
				logger.Info("[Poll] User %d sent photo with feedback: file_id=%s", msg.From.ID, photoFileID)
			}

			if err := deps.FeedbackHandler.HandleFeedbackWithPhoto(msg.From.ID, msg.Chat.ID, sanitizedText, photoFileID); err != nil {
				logger.Info("[Poll] Failed to handle feedback: %v", err)
			}
			return
		}

		// Check if user is in a feedback follow-up conversation
		sess := deps.SessionMgr.GetOrCreate(msg.From.ID)
		if sess != nil && deps.SessionMgr.IsValid(msg.From.ID) {
			if _, exists := sess.Get("feedback_conversation_issue_id"); exists {
				logger.Info("[Poll] User %d is in feedback follow-up conversation", msg.From.ID)
				if err := deps.FeedbackHandler.HandleUserFollowUp(msg.From.ID, msg.Chat.ID, sanitizedText); err != nil {
					logger.Info("[Poll] Failed to handle follow-up: %v", err)
				}
				return
			}
		}
	}

	// Check if user is in "waiting for add admin" state
	if deps.AdminHandler != nil {
		sess := deps.SessionMgr.GetOrCreate(msg.From.ID)
		if sess != nil && deps.SessionMgr.IsValid(msg.From.ID) {
			if _, exists := sess.Get("waiting_for_add_admin"); exists {
				logger.Info("[Poll] User %d is in add admin state", msg.From.ID)
				msg.Text = sanitizedText // Update with sanitized text
				if resp, err := deps.AdminHandler.HandleAdminAddMessage(msg.From.ID, msg.Chat.ID, msg); resp != nil {
					if err != nil {
						logger.Info("[Poll] HandleAdminAddMessage error: %v", err)
					}
					// Send the response as a new message
					keyboard := ConvertKeyboard(resp.Keyboard)
					if resp.Text != "" {
						deps.Telegram.SendMessage(msg.Chat.ID, resp.Text, "", keyboard)
					}
				}
				return
			}

			// Check if user is in "waiting for custom time input" state
			if _, exists := sess.Get("waiting_for_time_input"); exists {
				logger.Info("[Poll] User %d is in custom time input state", msg.From.ID)
				msg.Text = sanitizedText // Update with sanitized text
				if resp, err := deps.AdminHandler.HandleNotifCustomTimeInput(msg.From.ID, msg.Chat.ID, msg.Text); resp != nil {
					if err != nil {
						logger.Info("[Poll] HandleNotifCustomTimeInput error: %v", err)
					}
					// Send the response as a new message
					keyboard := ConvertKeyboard(resp.Keyboard)
					if resp.Text != "" {
						deps.Telegram.SendMessage(msg.Chat.ID, resp.Text, "", keyboard)
					}
					// Clear the waiting state on success
					sess.Delete("waiting_for_time_input")
					return
				}
			}

			// Check if admin is in "pending feedback reply" state
			if pendingFeedbackIDVal, exists := sess.Get("pending_feedback_reply"); exists {
				logger.Info("[Poll] Admin %d is in feedback reply state", msg.From.ID)
				var issueID int64
				switch v := pendingFeedbackIDVal.(type) {
				case int64:
					issueID = v
				case int:
					issueID = int64(v)
				case float64:
					issueID = int64(v)
				case string:
					fmt.Sscanf(v, "%d", &issueID)
				}

				if issueID > 0 {
					sess.Delete("pending_feedback_reply")
					adminName := "管理员"
					if name, ok := sess.GetString("name"); ok && name != "" {
						adminName = name
					}

					if deps.IssueService != nil {
						_, err := deps.IssueService.AddReply(issueID, msg.From.ID, adminName, sanitizedText, "admin")
						if err != nil {
							deps.Telegram.SendMessage(msg.Chat.ID, fmt.Sprintf("❌ 回复失败: %v", err), "", nil)
						} else {
							if issue, exists := deps.IssueService.GetIssue(issueID); exists && issue.UserID != msg.From.ID {
								notifyMsg := fmt.Sprintf("💬 管理员回复了您的反馈\n\n问题 #%d: %s\n\n📝 回复: %s", issue.ID, issue.Title, sanitizedText)
								deps.Telegram.SendMessage(issue.UserID, notifyMsg, "", nil)
							}
							deps.Telegram.SendMessage(msg.Chat.ID, fmt.Sprintf("✅ 反馈回复已发送\n\n问题 #%d", issueID), "", nil)
						}
					}
				}
				return
			}

			// Check if admin is in "pending issue reply" state
			if pendingIssueIDVal, exists := sess.Get("pending_issue_reply"); exists {
				logger.Info("[Poll] Admin %d is in issue reply state", msg.From.ID)
				// Parse issue ID
				var issueID int64
				if issueIDInt, ok := pendingIssueIDVal.(int64); ok {
					issueID = issueIDInt
				} else if issueIDStr, ok := pendingIssueIDVal.(string); ok {
					fmt.Sscanf(issueIDStr, "%d", &issueID)
				}

				if issueID > 0 {
					// Clear the pending state
					sess.Delete("pending_issue_reply")

					// Get admin name
					adminName := "管理员"
					if name, ok := sess.GetString("name"); ok && name != "" {
						adminName = name
					}

					// Handle issue reply through IssueService
					if deps.IssueService != nil {
						_, err := deps.IssueService.AddReply(issueID, msg.From.ID, adminName, sanitizedText, "admin")
						if err != nil {
							deps.Telegram.SendMessage(msg.Chat.ID, fmt.Sprintf("❌ 回复失败: %v", err), "", nil)
						} else {
							// Get issue details to notify user
							issue, exists := deps.IssueService.GetIssue(issueID)
							if exists && issue.UserID != msg.From.ID {
								// Notify the user who reported the issue
								notifyMsg := fmt.Sprintf("💬 管理员回复了您的问题\n\n🐛 问题 #%d: %s\n\n📝 回复: %s", issue.ID, issue.Title, sanitizedText)
								deps.Telegram.SendMessage(issue.UserID, notifyMsg, "", nil)
							}
							deps.Telegram.SendMessage(msg.Chat.ID, fmt.Sprintf("✅ 回复已发送\n\n问题 #%d", issueID), "", nil)
						}
					}
				}
				return
			}
		}
	}

	// Private chat: Handle commands and search query
	if strings.HasPrefix(sanitizedText, "/") {
		msg.Text = sanitizedText // Update with sanitized text
		HandleCommand(deps.Telegram, msg, cfg, deps.AdminService, deps.BindingRequest, deps.QuotaService, deps.UserMapping, deps.SessionMgr, deps.WishHandler, deps.MyRequests)
		return
	}

	// Check if user is in AI chat mode
	sess := deps.SessionMgr.GetOrCreate(msg.From.ID)
	if sess != nil && deps.SessionMgr.IsValid(msg.From.ID) {
		if aiChatMode, exists := sess.Get("ai_chat_mode"); exists {
			if aiEnabled, ok := aiChatMode.(bool); ok && aiEnabled {
				logger.Info("[Poll] User %d is in AI chat mode, handling with AI", msg.From.ID)
				handleAIChatMessage(msg.From.ID, msg.Chat.ID, sanitizedText, deps.Telegram, deps.SessionMgr)
				return
			}
		}
	}

	// Check if user is not linked and input looks like credentials (userID + password)
	// Format: "number password" or "username password"
	if deps.UserMapping != nil {
		_, isLinked := deps.UserMapping.GetMoviePilotUserID(msg.From.ID)
		if !isLinked && sanitizedText != "" && len(sanitizedText) > 1 {
			parts := strings.Fields(sanitizedText)
			// Check if format matches: ID (number) + password, or username + password
			if len(parts) >= 2 {
				// First part is a number (MoviePilot user ID) or username
				firstPart := parts[0]

				// Detect if this looks like credentials (not a typical search query)
				// Criteria: first part is all digits, or total parts is exactly 2
				isCredentialFormat := false
				if len(parts) == 2 {
					// Exactly 2 parts: likely username/id + password
					isCredentialFormat = true
				} else if len(firstPart) >= 4 && allDigits(firstPart) {
					// First part is a long number (likely user ID)
					isCredentialFormat = true
				}

				if isCredentialFormat {
					logger.Info("[Poll] User %d not linked, detected credential format, attempting auto-link", msg.From.ID)
					msg.Text = sanitizedText
					HandleLinkCommand(deps.Telegram, msg, deps.BindingRequest, cfg, deps.UserMapping)
					return
				}
			}
		}
	}

	// Handle search query (non-command text)
	if sanitizedText != "" && len(sanitizedText) > 1 {
		msg.Text = sanitizedText // Update with sanitized text
		HandlePollSearchQuery(msg, deps.Telegram, deps.MoviePilot, deps.SessionMgr, deps.SearchHistory, deps.SearchHistoryDB, deps.TMDB, deps.FallbackService)
	}
}

// allDigits checks if a string contains only digits
func allDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// HandleGroupChatMessage handles messages in group chats
// 群组只做轻量引导和通知，不在群里展开搜索结果/求片进度，避免暴露观影隐私。
func HandleGroupChatMessage(msg *types.TelegramMessage, telegram *services.TelegramClient, moviepilot *services.MoviePilotClient, sessMgr *session.Manager, searchHistory *services.SearchHistoryService, tmdb *services.TMDBClient) {
	text := strings.TrimSpace(msg.Text)
	logger.Info("[PollGroupChat] ChatID=%d, Title=%s, Text=%q", msg.Chat.ID, msg.Chat.Title, text)

	if !strings.HasPrefix(text, "/") {
		// 普通群聊消息不响应，避免刷屏。
		return
	}

	cmd := strings.Fields(text)[0]
	if idx := strings.Index(cmd, "@"); idx >= 0 {
		cmd = cmd[:idx]
	}

	switch cmd {
	case "/start", "/search", "/wish", "/requests", "/watchlist", "/quota", "/ai":
		telegram.SendMessage(msg.Chat.ID, "🔒 为了保护观影隐私，搜片、求片、进度和配额请私聊机器人使用。\n\n群组会用于接收入库通知、拼车到货提醒和公告～", "", nil)
	case "/id":
		text := fmt.Sprintf("📋 当前聊天信息\n\n聊天 ID: <code>%d</code>\n聊天类型: %s\n用户 ID: <code>%d</code>", msg.Chat.ID, msg.Chat.Type, msg.From.ID)
		telegram.SendMessage(msg.Chat.ID, text, "HTML", nil)
	default:
		// 其他命令不响应，交给 Telegram/群管理习惯。
		return
	}
}

// sendRecommendationMenu sends the recommendation menu and enters one-shot AI chat mode.
func sendRecommendationMenu(telegram *services.TelegramClient, chatID int64, userID int64, sessMgr *session.Manager) {
	msg := services.NewMessageBuilder()
	msg.Bold("🎬 今晚看什么").Newline()
	msg.Newline()
	if ai.GetManager().IsEnabled() {
		if sessMgr != nil {
			sessMgr.GetOrCreate(userID).Set("ai_chat_mode", true)
		}
		msg.Text("直接告诉我你的口味/心情，我帮你挑一部。").Newline()
		msg.Newline()
		msg.Text("比如：").Newline()
		msg.Text("• 想看一部轻松治愈的").Newline()
		msg.Text("• 来点烧脑悬疑，不要太长").Newline()
		msg.Text("• 适合今晚和朋友一起看的电影").Newline()
		msg.Newline()
		msg.Italic("💡 下一条文字会走 AI 推荐；想搜片请点“搜影片”或返回主菜单")
	} else {
		msg.Text("AI 推荐暂未启用，但你可以先用下面的精选入口。").Newline()
		msg.Newline()
		msg.Italic("💡 管理员配置 AI 后，这里会支持直接发口味描述")
	}

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

	telegram.SendMessage(chatID, msg.Build(), msg.ParseMode(), kb.Build())
}

// HandlePollSearchQuery handles search queries (for polling)
func HandlePollSearchQuery(msg *types.TelegramMessage, telegram *services.TelegramClient, moviepilot *services.MoviePilotClient, sessMgr *session.Manager, searchHistory *services.SearchHistoryService, searchHistoryDB *services.SearchHistoryDB, tmdb *services.TMDBClient, fallbackSvc *services.SearchFallbackService) {
	// Sanitize search query
	query := validation.SanitizeSearchQuery(msg.Text)

	// Add to search history - prefer DB version (new), fallback to legacy
	if searchHistoryDB != nil && query != "" {
		searchHistoryDB.AddSearch(msg.From.ID, query)
		logger.Info("[Poll] Search added to SearchHistoryDB: userID=%d, query=%s", msg.From.ID, query)
	} else if searchHistory != nil && query != "" {
		searchHistory.AddSearch(msg.From.ID, query)
		logger.Info("[Poll] Search added to SearchHistory (legacy): userID=%d, query=%s", msg.From.ID, query)
	} else if query != "" {
		logger.Info("[Poll] WARNING: No search history service available, query not saved: %s", query)
	}

	// Search in MoviePilot
	results, err := moviepilot.SearchMedia(query, 1)
	if err != nil {
		// Log full error for admin debugging
		logger.Info("[Poll] Search failed for query '%s': %v", query, err)
		// Send user-friendly message without technical details
		telegram.SendMessage(msg.Chat.ID, "❌ 搜索失败：服务器暂时开小差了，请稍后再试。\n\n💡 如果持续失败，请联系管理员。", "", nil)
		return
	}

	if len(results.Results) == 0 {
		// Try fallback search strategies
		if fallbackSvc != nil {
			fallbackResults, fallbackQuery, fbErr := fallbackSvc.TryFallback(query)
			if fbErr != nil {
				logger.Info("[Poll] Fallback search failed: %v", fbErr)
			}
			if len(fallbackResults) > 0 {
				logger.Info("[Poll] Fallback hit: query=%s -> fallback=%s, count=%d", query, fallbackQuery, len(fallbackResults))
				// Update query to the successful fallback query
				query = fallbackQuery
				results = &services.SearchResponse{Results: fallbackResults}
				telegram.SendMessage(msg.Chat.ID, fmt.Sprintf("💡 已为你启用兜底搜索：%s", fallbackQuery), "", nil)
			} else {
				telegram.SendMessage(msg.Chat.ID, "😕 未找到相关内容\n\n💡 建议：\n• 检查拼写是否正确\n• 尝试使用更简短的关键词\n• 尝试使用英文搜索", "", nil)
				return
			}
		} else {
			telegram.SendMessage(msg.Chat.ID, "😕 未找到相关内容\n\n💡 建议：\n• 检查拼写是否正确\n• 尝试使用更简短的关键词\n• 尝试使用英文搜索", "", nil)
			return
		}
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
					logger.Info("[PollSearch] Failed to fetch seasons from TMDB for %s: %v", item.Title, err)
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
		logger.Info("[搜索] 获取到 %d/%d 部剧集的季数信息", seasonCount, len(searchItems))
	}

	sess.SetSearchResults(searchItems, 1, query)
	logger.Info("[搜索] 查询 \"%s\": %d 条结果", query, len(results.Results))

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
			CallbackData: "search:page:2",
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
	logger.Info("[Poll] Callback from user %d: %s", cb.From.ID, sanitizedData)

	// Parse callback
	parsed, err := registry.Parser().Parse(sanitizedData)
	if err != nil {
		logger.Info("Failed to parse callback: %v", err)
		if ansErr := telegram.AnswerCallback(cb.ID, "无效的请求", true); ansErr != nil {
			logger.Info("[Callback] Failed to answer callback (parse error): %v", ansErr)
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
		logger.Info("No handler for action: %s", parsed.Action)
		if ansErr := telegram.AnswerCallback(cb.ID, "未知操作", true); ansErr != nil {
			logger.Info("[Callback] Failed to answer callback (no handler): %v", ansErr)
		}
		return
	}

	// Handle callback with timeout protection (10 seconds)
	type handleResult struct {
		resp *callback.Response
		err  error
	}
	resultChan := make(chan handleResult, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Info("[Callback] Panic recovered in handler: %v", r)
				resultChan <- handleResult{
					resp: &callback.Response{
						Text:        "❌ 处理请求时发生错误",
						CallbackMsg: "处理错误",
						ShowAlert:   true,
					},
					err: fmt.Errorf("panic: %v", r),
				}
			}
		}()
		resp, err := handler.Handle(ctx)
		resultChan <- handleResult{resp: resp, err: err}
	}()

	select {
	case result := <-resultChan:
		// Use the result from handler
		if result.err != nil {
			logger.Info("Handler error: %v", result.err)
			callbackMsg := "操作失败"
			if result.resp != nil && result.resp.CallbackMsg != "" {
				callbackMsg = result.resp.CallbackMsg
			}
			if ansErr := telegram.AnswerCallback(cb.ID, callbackMsg, true); ansErr != nil {
				logger.Info("[Callback] Failed to answer callback (error): %v", ansErr)
			}
			// Try to show error message if response exists
			if result.resp != nil && result.resp.Text != "" {
				keyboard := ConvertKeyboard(result.resp.Keyboard)
				telegram.EditMessage(ctx.ChatID, ctx.MessageID, result.resp.Text, "", keyboard)
			}
			return
		}
		resp := result.resp
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

		if showAlert && len(callbackMsg) > 200 {
			callbackMsg = callbackMsg[:197] + "..."
		}

		if ansErr := telegram.AnswerCallback(cb.ID, callbackMsg, showAlert); ansErr != nil {
			logger.Info("[Callback] AnswerCallback error (callback may have expired): %v", ansErr)
		}

		// Send or edit message
		if resp != nil {
			logger.Info("[Callback] Response: Text=%d chars, Edit=%v, Photo=%v, Keyboard=%v",
				len(resp.Text), resp.Edit, resp.Photo != "", resp.Keyboard != nil)
			handleCallbackResponse(ctx, resp, telegram)
		} else {
			logger.Info("[Callback] Response is nil!")
		}
	case <-time.After(25 * time.Second):
		logger.Info("[Callback] Handler timeout for action=%s, userID=%d", parsed.Action, cb.From.ID)
		if ansErr := telegram.AnswerCallback(cb.ID, "处理超时，请重试", true); ansErr != nil {
			logger.Info("[Callback] Failed to answer callback (timeout): %v", ansErr)
		}
		return
	}
}

// handleCallbackResponse sends or edits message based on response
func handleCallbackResponse(ctx *callback.Context, resp *callback.Response, telegram *services.TelegramClient) {
	keyboard := ConvertKeyboard(resp.Keyboard)

	// Use HTML as default parse mode if not specified
	parseMode := resp.ParseMode
	if parseMode == "" {
		parseMode = "HTML"
	}

	// Log parse mode for debugging
	logger.Info("[Callback] Using parse_mode=%s for chat %d, text preview=%.50s",
		parseMode, ctx.ChatID, resp.Text)

	// Check if we need to send a photo
	if resp.Photo != "" {
		// Delete the original message first
		if delErr := telegram.DeleteMessage(ctx.ChatID, ctx.MessageID); delErr != nil {
			logger.Info("[Callback] DeleteMessage error: %v", delErr)
		}
		// Send photo with caption and keyboard
		// First try URL method (most reliable)
		caption := resp.PhotoCaption
		if caption == "" {
			caption = resp.Text
		}
		logger.Info("[Callback] Sending photo via URL: %s with parse_mode=%s", resp.Photo, parseMode)
		if _, sendErr := telegram.SendPhotoWithParseMode(ctx.ChatID, resp.Photo, caption, parseMode, keyboard); sendErr != nil {
			logger.Info("[Callback] SendPhoto URL method error: %v, trying proxy upload", sendErr)
			// Fallback to proxy upload if URL method fails
			if _, fallbackErr := telegram.SendPhotoWithAuthAndParseMode(ctx.ChatID, resp.Photo, caption, parseMode, nil, keyboard); fallbackErr != nil {
				logger.Info("[Callback] SendPhoto proxy upload also failed: %v", fallbackErr)
			}
		}
	} else if resp.Text != "" {
		if resp.DeleteMessage {
			// Delete current message and send new one
			if delErr := telegram.DeleteMessage(ctx.ChatID, ctx.MessageID); delErr != nil {
				logger.Info("[Callback] DeleteMessage error: %v", delErr)
			}
			if _, sendErr := telegram.SendMessage(ctx.ChatID, resp.Text, parseMode, keyboard); sendErr != nil {
				logger.Info("[Callback] SendMessage error: %v", sendErr)
			}
		} else if resp.Edit {
			// Edit existing message
			if _, editErr := telegram.EditMessage(ctx.ChatID, ctx.MessageID, resp.Text, parseMode, keyboard); editErr != nil {
				logger.Info("[Callback] EditMessage error: %v", editErr)
			}
		} else {
			// Send new message
			if _, sendErr := telegram.SendMessage(ctx.ChatID, resp.Text, parseMode, keyboard); sendErr != nil {
				logger.Info("[Callback] SendMessage error: %v", sendErr)
			}
		}
	} else if resp.Keyboard != nil && resp.Text == "" {
		// 仅更新键盘按钮，不修改文本 (状态融合按钮的原地刷新)
		if _, editErr := telegram.EditMessageReplyMarkup(ctx.ChatID, ctx.MessageID, keyboard); editErr != nil {
			logger.Info("[Callback] EditMessageReplyMarkup error: %v", editErr)
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

// handleAIChatMessage handles messages in AI chat mode
func handleAIChatMessage(userID, chatID int64, text string, telegram *services.TelegramClient, sessMgr *session.Manager) {
	// Check if AI is enabled
	if !ai.GetManager().IsEnabled() {
		telegram.SendMessage(chatID, "🤖 AI 功能暂未启用\n\n💡 请联系管理员配置 AI_ENABLED=true，并设置 ZHIPU_API_KEY / CLAUDE_API_KEY / ANTHROPIC_API_KEY 之一", "", nil)
		return
	}

	// Get AI recommendations based on user input
	response, err := ai.GetAIRecommendations(text, 5)
	if err != nil {
		logger.Info("[AIChat] Failed to get AI recommendations: %v", err)
		telegram.SendMessage(chatID, fmt.Sprintf("😓 AI 推荐暂时不可用: %v\n\n💡 请稍后再试或使用普通搜索", err), "", nil)
		return
	}

	// Build response message
	msg := services.NewMessageBuilder()
	msg.Text(response).Newline()
	msg.Newline()
	msg.Italic("💡 推荐结束啦，继续发片名将直接搜索影片")

	// Build keyboard
	kb := services.NewKeyboardBuilder()
	kb.AddButton("🎬 继续推荐", "start_ai")
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	telegram.SendMessage(chatID, msg.Build(), "HTML", kb.Build())

	// AI 单次交互后自动退出，下一条文本回归搜索
	sessMgr.GetOrCreate(userID).Delete("ai_chat_mode")
}
