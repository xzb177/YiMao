package bot

import (
	"fmt"
	"strings"
	"time"

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
	UserMapping     services.UserMappingStore
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
	MyRequests      *handlers.MyRequestsHandler // 求片进度（/requests 命令复用）
}

// PollDeps holds dependencies for polling (reduced set)
type PollDeps struct {
	Registry        *callback.Registry
	Telegram        *services.TelegramClient
	MoviePilot      *services.MoviePilotClient
	SessionMgr      *session.Manager
	UserMapping     services.UserMappingStore
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
	MyRequests      *handlers.MyRequestsHandler // 求片进度（/requests 命令复用）
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
		Registry:        registry,
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
	// 去重 + 并发上限：Telegram 会在 offset 确认前重复投递同一条 update，
	// 处理两次会造成重复回复/重复求片/重复扣配额；同时限制并发处理数，
	// 避免一波更新打满下游 MoviePilot/TMDB/Emby 调用。
	dedup := newUpdateDeduper(processedUpdateTTL)
	updateSem := make(chan struct{}, maxUpdateWorkers)
	go dedup.pruneProcessedUpdates(processedUpdatePruneInterval)

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

		// offset 在整批 dispatch 完成后再推进：先推进会让处理中的 update 在
		// 崩溃/重启后彻底丢失，而去重保证了重复投递不会被处理两次。
		maxUpdateID := dispatchUpdates(updates, dedup, updateSem, nil, func(update types.TelegramUpdate) {
			handlePollUpdate(update, pollDeps, registry, deps, cfg)
		})
		if maxUpdateID > 0 {
			offset = int(maxUpdateID + 1)
		}
	}
}

// handlePollUpdate routes one Telegram update to the message or callback path.
func handlePollUpdate(
	update types.TelegramUpdate,
	pollDeps *PollDeps,
	registry *callback.Registry,
	deps *Dependencies,
	cfg *config.Config,
) {
	if update.Message != nil {
		if update.Message.Chat == nil {
			logger.Info("[Poll] Update %d ignored: message has no chat", update.UpdateID)
			return
		}
		if update.Message.CommunityChatAdded != nil {
			community := update.Message.CommunityChatAdded.Community
			logger.Info("[Community] Chat %d joined community id=%d name=%q", update.Message.Chat.ID, community.ID, community.Name)
			return
		}
		if update.Message.CommunityChatRemoved != nil {
			logger.Info("[Community] Chat %d left its community", update.Message.Chat.ID)
			return
		}
		if update.Message.From == nil {
			logger.Info("[Poll] Update %d ignored: message has no sender", update.UpdateID)
			return
		}
		logger.Info("[Poll] Update %d: Message from %d: %s", update.UpdateID, update.Message.From.ID, validation.RedactSensitiveText(update.Message.Text))
		HandlePollMessage(update.Message, pollDeps, cfg)
		return
	}
	if update.CallbackQuery != nil {
		logger.Info("[Poll] Update %d: Callback from %d", update.UpdateID, update.CallbackQuery.From.ID)
		HandleCallbackQuery(update.CallbackQuery, registry, deps.Telegram)
		return
	}
	logger.Info("[Poll] Update %d: Empty update (no message or callback)", update.UpdateID)
}

// HandlePollMessage processes a message update (for polling)
func HandlePollMessage(msg *types.TelegramMessage, deps *PollDeps, cfg *config.Config) {
	// Sanitize input text (photo descriptions use caption when text is empty).
	rawText := msg.Text
	if strings.TrimSpace(rawText) == "" {
		rawText = msg.Caption
	}
	sanitizedText := validation.SanitizeMessageText(rawText)
	logger.Info("[Poll] Message from %d: %s", msg.From.ID, validation.RedactSensitiveText(sanitizedText))

	// Group chat: handle search queries only
	if msg.Chat.Type != "private" {
		HandleGroupChatMessage(msg, deps.Telegram, deps.MoviePilot, deps.SessionMgr, deps.SearchHistory, deps.TMDB)
		return
	}

	// 【P0 dead-end 逃逸修复】用户处于任意“等待文本输入”的 pending 态时，命令(/cancel /start 等)
	// 必须能逃出，而不是被当成输入内容吞掉。统一处理：当文本以 "/" 开头时，先清除所有 pending
	// 输入态键，再放行到下方命令处理；若原本处于 pending 态且命令为 /cancel，则给出明确反馈并返回。
	if strings.HasPrefix(sanitizedText, "/") {
		if clearPendingInputStatesPoll(deps, msg.From.ID) {
			cmd := strings.ToLower(strings.TrimSpace(sanitizedText))
			if cmd == "/cancel" || cmd == "/取消" {
				deps.Telegram.SendMessage(msg.Chat.ID, "✅ 已退出当前输入", "", nil)
				return
			}
		}
		// 其它命令(如 /start /requests)：已清除 pending 态，直接执行命令。
		msg.Text = sanitizedText // Update with sanitized text
		HandleCommand(deps.Telegram, msg, cfg, deps.AdminService, deps.BindingRequest, deps.QuotaService, deps.UserMapping, deps.SessionMgr, deps.WishHandler, deps.MyRequests)
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
					keyboard := ConvertKeyboard(resp.Keyboard)
					if resp.RichMessage != "" {
						deps.Telegram.SendRichMessage(msg.Chat.ID, resp.RichMessage, keyboard)
					} else if resp.Text != "" {
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
							logger.Info("[Poll] Issue reply failed for issue %d by user %d: %v", issueID, msg.From.ID, err)
							_, _ = deps.Telegram.SendMessage(msg.Chat.ID, "❌ 回复失败，请稍后再试", "", nil)
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
							logger.Info("[Poll] Issue reply failed for issue %d by user %d: %v", issueID, msg.From.ID, err)
							_, _ = deps.Telegram.SendMessage(msg.Chat.ID, "❌ 回复失败，请稍后再试", "", nil)
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

	// Private chat: command handling —— 已在前面统一处理(含 pending 逃逸)，此处无需再判命令。

	// Check if user is not linked and input looks like credentials (userID + password)
	// Format: "number password" or "username password"
	if deps.UserMapping != nil {
		sess := deps.SessionMgr.GetOrCreate(msg.From.ID)
		intent, _ := sess.GetString("media_search_intent")
		_, isLinked := deps.UserMapping.GetMoviePilotUserID(msg.From.ID)
		if intent != "wash" && !isLinked && sanitizedText != "" && len(sanitizedText) > 1 {
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
		if HandlePrivateTMDBLink(msg, deps.Registry, deps.Telegram) {
			return
		}

		HandlePollSearchQuery(msg, deps.Telegram, deps.MoviePilot, deps.SessionMgr, deps.SearchHistory, deps.SearchHistoryDB, deps.TMDB, deps.FallbackService)
	}
}

// clearPendingInputStatesPoll 清除用户所有“等待文本输入”的 pending 态键，用于命令逃逸(poll 入口)。
// 【P0 dead-end 逃逸修复】与 webhook 入口逻辑一致。返回 true 表示用户原本确实处于某个 pending 态。
func clearPendingInputStatesPoll(deps *PollDeps, userID int64) bool {
	if deps == nil || deps.SessionMgr == nil {
		return false
	}
	sess := deps.SessionMgr.GetOrCreate(userID)
	if sess == nil || !deps.SessionMgr.IsValid(userID) {
		return false
	}

	hadPending := false
	pendingKeys := []string{
		// 反馈流程（描述输入步骤）
		"feedback_step",
		"feedback_tmdb_id",
		"feedback_media_type",
		"feedback_media_title",
		"feedback_issue_type",
		"feedback_require_media",
		"feedback_draft_description",
		"feedback_draft_photo_file_id",
		// 反馈追问会话
		"feedback_conversation_issue_id",
		// 管理员添加管理员 / 自定义时间 / 回复反馈 / 回复问题
		"waiting_for_add_admin",
		"waiting_for_time_input",
		"pending_feedback_reply",
		"pending_issue_reply",
	}
	for _, key := range pendingKeys {
		if _, exists := sess.Get(key); exists {
			hadPending = true
			sess.Delete(key)
		}
	}
	return hadPending
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
	case "/start", "/search", "/wish", "/requests", "/watchlist", "/quota", "/portrait":
		// Ephemeral command response: visible only to the invoking member. Fail
		// closed instead of posting private details when delivery is unavailable.
		sendCommunityCommandMessage(telegram, msg, "🔒 这是你的私密操作入口。请点下方菜单继续；群里只保留入库喜报、拼车到货等高光通知。", "", services.BuildStartKeyboardWithOptions(false, true))
	case "/id":
		text := fmt.Sprintf("📋 当前聊天信息\n\n聊天 ID: <code>%d</code>\n聊天类型: %s\n用户 ID: <code>%d</code>", msg.Chat.ID, msg.Chat.Type, msg.From.ID)
		telegram.SendMessage(msg.Chat.ID, text, "HTML", nil)
	default:
		// 其他命令不响应，交给 Telegram/群管理习惯。
		return
	}
}

func sendCommunityCommandMessage(telegram *services.TelegramClient, msg *types.TelegramMessage, text, parseMode string, keyboard *types.TelegramInlineKeyboard) {
	if telegram == nil || msg == nil || msg.Chat == nil || msg.From == nil || !isCommunityChat(msg.Chat.Type) {
		return
	}
	opt := &types.TelegramSendOptions{ReceiverUserID: msg.From.ID}
	if msg.EphemeralMessageID != 0 {
		opt.ReplyParameters = &types.TelegramReplyParameters{EphemeralMessageID: msg.EphemeralMessageID}
	}
	if _, err := telegram.SendMessage(msg.Chat.ID, text, parseMode, keyboard, opt); err != nil {
		logger.Info("[Community] Ephemeral command response unavailable for user=%d: %v", msg.From.ID, err)
	}
}

// sendRecommendationMenu sends the recommendation menu.
func sendRecommendationMenu(telegram *services.TelegramClient, chatID int64, userID int64, sessMgr *session.Manager) {
	msg := services.NewMessageBuilder()
	msg.Bold("🎬 今晚看什么").Newline()
	msg.Newline()
	msg.Text("选一个感兴趣的分类：").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🔥 本周热门", "search:type:trending")
	kb.AddButton("📺 热门剧集", "search:type:hot")
	kb.NewRow()
	kb.AddButton("⭐ 必看神作", "search:type:toprated")
	kb.AddButton("🆕 最新上映", "search:type:new")
	kb.NewRow()
	kb.AddButton("🎲 随机探索", "search:type:random")
	kb.NewRow()
	kb.AddButton("🏠 主菜单", "start")

	telegram.SendMessage(chatID, msg.Build(), msg.ParseMode(), kb.Build())
}

// HandlePollSearchQuery handles search queries (for polling) through the same
// SearchHandler used by callbacks and pagination.
func HandlePollSearchQuery(msg *types.TelegramMessage, telegram *services.TelegramClient, moviepilot *services.MoviePilotClient, sessMgr *session.Manager, searchHistory *services.SearchHistoryService, searchHistoryDB *services.SearchHistoryDB, tmdb *services.TMDBClient, fallbackSvc *services.SearchFallbackService) {
	query := validation.SanitizeSearchQuery(msg.Text)
	handler := handlers.NewSearchHandler(sessMgr, telegram, moviepilot, tmdb)
	handler.SetSearchHistory(searchHistory)
	handler.SetSearchHistoryDB(searchHistoryDB)
	if err := handler.HandleSearchQuery(msg.From.ID, msg.Chat.ID, query); err != nil {
		logger.Info("[Poll] Search failed for query %q: %v", query, err)
	}
}

// HandleCallbackQuery processes a callback query (for polling)
func HandleCallbackQuery(cb *types.TelegramCallbackQuery, registry *callback.Registry, telegram *services.TelegramClient) {
	// Sanitize callback data
	sanitizedData := validation.SanitizeCallbackData(cb.Data)
	logger.Info("[Poll] Callback from user %d", cb.From.ID)

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
		UserID:             cb.From.ID,
		ChatID:             cb.Message.Chat.ID,
		ChatType:           cb.Message.Chat.Type,
		MessageID:          cb.Message.MessageID,
		MessageThreadID:    cb.Message.MessageThreadID,
		EphemeralMessageID: cb.Message.EphemeralMessageID,
		CallbackID:         cb.ID,
		Callback:           parsed,
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

	// Establish a private response target before invoking potentially slow business
	// handlers. This consumes the callback within Telegram's 15-second window;
	// subsequent updates edit the ephemeral placeholder by ID.
	answered := false
	if isCommunityChat(ctx.ChatType) {
		answered = true
		if err := telegram.AnswerCallback(cb.ID, "", false); err != nil {
			logger.Info("[Callback] Immediate answer failed: %v", err)
		}
		if ctx.EphemeralMessageID == 0 {
			placeholder, sendErr := telegram.SendMessage(ctx.ChatID, "⏳ 正在处理…", "", nil, &types.TelegramSendOptions{
				ReceiverUserID:              ctx.UserID,
				CallbackQueryID:             ctx.CallbackID,
				ReplaceCallbackQueryMessage: true,
				MessageThreadID:             ctx.MessageThreadID,
			})
			if sendErr != nil || placeholder == nil || placeholder.EphemeralMessageID == 0 {
				logger.Info("[Callback] Cannot establish ephemeral response target: %v", sendErr)
				return
			}
			ctx.EphemeralMessageID = placeholder.EphemeralMessageID
		}
	} else if callbackNeedsImmediateAck(parsed.Action) {
		// Slow, authoritative actions are acknowledged up front so the client
		// never spins while preflight checks run.
		answered = true
		if err := telegram.AnswerCallback(cb.ID, "", false); err != nil {
			logger.Info("[Callback] Immediate answer failed: %v", err)
		}
	}

	// Handle callback with timeout protection
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
			if !answered {
				if ansErr := telegram.AnswerCallback(cb.ID, callbackMsg, true); ansErr != nil {
					logger.Info("[Callback] Failed to answer callback (error): %v", ansErr)
				}
			}
			// Errors must stay private in Community chats. Render through the
			// central P1 router instead of editing the public source message.
			if result.resp != nil && result.resp.Text != "" {
				RenderCallbackResponse("[Callback]", ctx, result.resp, telegram)
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
		if isCommunityChat(ctx.ChatType) && callbackMsg == "" {
			callbackMsg = "私密响应仅你可见；若未显示请稍后再试"
		}

		if showAlert && len(callbackMsg) > 200 {
			callbackMsg = callbackMsg[:197] + "..."
		}

		if !answered {
			if ansErr := telegram.AnswerCallback(cb.ID, callbackMsg, showAlert); ansErr != nil {
				logger.Info("[Callback] AnswerCallback error (callback may have expired): %v", ansErr)
			}
		}

		// Send or edit message
		if resp != nil {
			logger.Info("[Callback] Response: Text=%d chars, Edit=%v, Photo=%v, Keyboard=%v",
				len(resp.Text), resp.Edit, resp.Photo != "", resp.Keyboard != nil)
			handleCallbackResponse(ctx, resp, telegram)
		} else {
			logger.Info("[Callback] Response is nil!")
		}
	}
}

// handleCallbackResponse sends or edits message based on response
func handleCallbackResponse(ctx *callback.Context, resp *callback.Response, telegram *services.TelegramClient) {
	RenderCallbackResponse("[Callback]", ctx, resp, telegram)
}

// ConvertKeyboard converts callback Keyboard to TelegramInlineKeyboard
func ConvertKeyboard(kb *callback.Keyboard) *types.TelegramInlineKeyboard {
	if kb == nil {
		return nil
	}

	result := &types.TelegramInlineKeyboard{
		ForceReply:     kb.ForceReply,
		RemoveKeyboard: kb.RemoveKeyboard,
	}
	if len(kb.InlineKeyboard) == 0 {
		return result
	}
	result.InlineKeyboard = make([][]types.TelegramInlineKeyboardButton, len(kb.InlineKeyboard))

	for i, row := range kb.InlineKeyboard {
		result.InlineKeyboard[i] = make([]types.TelegramInlineKeyboardButton, len(row))
		for j, btn := range row {
			converted := types.TelegramInlineKeyboardButton{
				Text:         types.CleanButtonText(btn.Text),
				CallbackData: btn.CallbackData,
				URL:          btn.URL,
				WebApp:       btn.WebApp,
				Style:        btn.Style,
			}
			if converted.Style == "" {
				converted.Style = types.ButtonStyleFor(converted.Text, converted.CallbackData)
			}
			if btn.Disabled {
				converted.Disabled = types.DisabledButtonValue()
				converted.CallbackData = ""
				converted.URL = ""
				converted.WebApp = nil
			}
			result.InlineKeyboard[i][j] = converted
		}
	}

	return result
}
