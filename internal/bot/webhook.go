package bot

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/config"
	"github.com/xzb177/yimao/internal/handlers"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
	"github.com/xzb177/yimao/pkg/validation"
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
	if cfg.TelegramWebhookSecret == "" {
		http.Error(w, "Telegram webhook disabled", http.StatusServiceUnavailable)
		return
	}
	provided := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
	if subtle.ConstantTimeCompare([]byte(provided), []byte(cfg.TelegramWebhookSecret)) != 1 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Body == nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	// Parse update
	var update types.TelegramUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		logger.Info("Failed to decode update: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Route update
	if update.CallbackQuery != nil {
		if update.CallbackQuery.From == nil || update.CallbackQuery.Message == nil || update.CallbackQuery.Message.Chat == nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		HandleWebhookCallback(w, &update, registry, deps.Telegram, deps.SessionMgr, cfg, deps.AdminService)
	} else if update.Message != nil {
		if update.Message.Chat == nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		// Community membership changes are service messages and may not carry a
		// sender. Acknowledge them without routing into user-command handlers.
		if update.Message.CommunityChatAdded != nil {
			community := update.Message.CommunityChatAdded.Community
			logger.Info("[Community] Chat %d joined community id=%d name=%q", update.Message.Chat.ID, community.ID, community.Name)
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "OK")
			return
		}
		if update.Message.CommunityChatRemoved != nil {
			logger.Info("[Community] Chat %d left its community", update.Message.Chat.ID)
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "OK")
			return
		}
		if update.Message.From == nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
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
	logger.Info("[Webhook] Callback from user %d", cb.From.ID)

	// Parse callback
	parsed, err := registry.Parser().Parse(cb.Data)
	if err != nil {
		logger.Info("Failed to parse callback: %v", err)
		telegram.AnswerCallback(cb.ID, "无效的请求", true)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
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
		telegram.AnswerCallback(cb.ID, "未知操作", true)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
		return
	}

	// Establish the private target before potentially slow business work. Later
	// rendering edits this message by ephemeral_message_id.
	answered := false
	if isCommunityChat(ctx.ChatType) {
		answered = true
		if err := telegram.AnswerCallback(cb.ID, "", false); err != nil {
			logger.Info("[Webhook] Immediate callback answer failed: %v", err)
		}
		if ctx.EphemeralMessageID == 0 {
			placeholder, sendErr := telegram.SendMessage(ctx.ChatID, "⏳ 正在处理…", "", nil, &types.TelegramSendOptions{
				ReceiverUserID:              ctx.UserID,
				CallbackQueryID:             ctx.CallbackID,
				ReplaceCallbackQueryMessage: true,
				MessageThreadID:             ctx.MessageThreadID,
			})
			if sendErr != nil || placeholder == nil || placeholder.EphemeralMessageID == 0 {
				logger.Info("[Webhook] Cannot establish ephemeral response target: %v", sendErr)
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, "OK")
				return
			}
			ctx.EphemeralMessageID = placeholder.EphemeralMessageID
		}
	} else if callbackNeedsImmediateAck(parsed.Action) {
		answered = true
		if err := telegram.AnswerCallback(cb.ID, "", false); err != nil {
			logger.Info("[Webhook] Immediate callback answer failed: %v", err)
		}
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
	if isCommunityChat(ctx.ChatType) && callbackMsg == "" {
		callbackMsg = "私密响应仅你可见；若未显示请稍后再试"
	}

	if err != nil {
		logger.Info("Handler error: %v", err)
		if callbackMsg == "" {
			callbackMsg = "操作失败"
		}
		showAlert = true
	}

	if showAlert && len(callbackMsg) > 200 {
		callbackMsg = callbackMsg[:197] + "..."
	}

	if !answered {
		if err := telegram.AnswerCallback(cb.ID, callbackMsg, showAlert); err != nil {
			logger.Info("[Callback] AnswerCallback error: %v", err)
		}
	}

	// Send or edit message
	if resp != nil {
		RenderCallbackResponse("[Webhook]", ctx, resp, telegram)
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
	logger.Info("[Webhook] Message from user %d (chat: %d, type: %s): %s", msg.From.ID, msg.Chat.ID, msg.Chat.Type, validation.RedactSensitiveText(msg.Text))

	// 群聊处理 @mention 搜索
	if msg.Chat.Type != "private" {
		if len(msg.Text) > 1 {
			HandleWebhookGroupChat(deps.Telegram, msg, deps.MoviePilot, deps.SessionMgr, deps.SearchHistory, deps.TMDB)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
		return
	}

	// 私聊中处理所有功能

	// 【P0 dead-end 逃逸修复】用户处于任意“等待文本输入”的 pending 态时，命令(/cancel /start 等)
	// 必须能逃出，而不是被当成输入内容吞掉。统一处理：当 msg.Text 以 "/" 开头时，先清除所有
	// pending 输入态键，再放行到下方命令处理；如果原本处于 pending 态且命令为 /cancel，则给出
	// 明确反馈并直接返回（/cancel 不是真命令）。
	if strings.HasPrefix(msg.Text, "/") {
		if clearPendingInputStates(deps, msg.From.ID) {
			cmd := strings.ToLower(strings.TrimSpace(msg.Text))
			if cmd == "/cancel" || cmd == "/取消" {
				deps.Telegram.SendMessage(msg.Chat.ID, "✅ 已退出当前输入", "", nil)
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, "OK")
				return
			}
		}
		// 其它命令(如 /start /requests)：已清除 pending 态，直接落到下方命令处理。
		HandleCommand(deps.Telegram, msg, cfg, deps.AdminService, deps.BindingRequest, deps.QuotaService, deps.UserMapping, deps.SessionMgr, deps.WishHandler, deps.MyRequests)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
		return
	}

	// 【关键修复】首先检查用户是否处于反馈流程中
	logger.Info("[Webhook] Checking feedback process for user %d, FeedbackHandler=%v", msg.From.ID, deps.FeedbackHandler != nil)
	if deps.FeedbackHandler != nil {
		inFeedback := deps.FeedbackHandler.IsInFeedbackProcess(msg.From.ID)
		logger.Info("[Webhook] User %d in feedback process: %v", msg.From.ID, inFeedback)
		if inFeedback {
			// Check if user sent a photo with feedback
			var photoFileID string
			if msg.Photo != nil && len(msg.Photo) > 0 {
				// Get the largest photo (last element in array)
				photoFileID = msg.Photo[len(msg.Photo)-1].FileID
				logger.Info("[Webhook] User %d sent photo with feedback: file_id=%s", msg.From.ID, photoFileID)
			}

			feedbackText := msg.Text
			if strings.TrimSpace(feedbackText) == "" {
				feedbackText = msg.Caption
			}
			// 用户正在反馈流程中，处理反馈文本/图片并返回
			if err := deps.FeedbackHandler.HandleFeedbackWithPhoto(msg.From.ID, msg.Chat.ID, feedbackText, photoFileID); err != nil {
				logger.Info("[Webhook] Failed to handle feedback: %v", err)
			}
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "OK")
			return
		}

		// Check if user is in a feedback follow-up conversation
		sess := deps.SessionMgr.GetOrCreate(msg.From.ID)
		if sess != nil && deps.SessionMgr.IsValid(msg.From.ID) {
			if _, exists := sess.Get("feedback_conversation_issue_id"); exists {
				logger.Info("[Webhook] User %d is in feedback follow-up conversation", msg.From.ID)
				if err := deps.FeedbackHandler.HandleUserFollowUp(msg.From.ID, msg.Chat.ID, msg.Text); err != nil {
					logger.Info("[Webhook] Failed to handle follow-up: %v", err)
				}
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, "OK")
				return
			}
		}
	}

	// Check admin pending text-input states before generic commands/search.
	if deps.AdminHandler != nil {
		sess := deps.SessionMgr.GetOrCreate(msg.From.ID)
		if sess != nil && deps.SessionMgr.IsValid(msg.From.ID) {
			if _, exists := sess.Get("waiting_for_add_admin"); exists {
				logger.Info("[Webhook] User %d is in add admin state", msg.From.ID)
				if resp, err := deps.AdminHandler.HandleAdminAddMessage(msg.From.ID, msg.Chat.ID, msg); resp != nil {
					if err != nil {
						logger.Info("[Webhook] HandleAdminAddMessage error: %v", err)
					}
					if resp.RichMessage != "" {
						deps.Telegram.SendRichMessage(msg.Chat.ID, resp.RichMessage, ConvertKeyboard(resp.Keyboard))
					} else if resp.Text != "" {
						deps.Telegram.SendMessage(msg.Chat.ID, resp.Text, "", ConvertKeyboard(resp.Keyboard))
					}
				}
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, "OK")
				return
			}

			if _, exists := sess.Get("waiting_for_time_input"); exists {
				logger.Info("[Webhook] User %d is in custom time input state", msg.From.ID)
				if resp, err := deps.AdminHandler.HandleNotifCustomTimeInput(msg.From.ID, msg.Chat.ID, msg.Text); resp != nil {
					if err != nil {
						logger.Info("[Webhook] HandleNotifCustomTimeInput error: %v", err)
					}
					if resp.Text != "" {
						deps.Telegram.SendMessage(msg.Chat.ID, resp.Text, "", ConvertKeyboard(resp.Keyboard))
					}
					sess.Delete("waiting_for_time_input")
					w.WriteHeader(http.StatusOK)
					fmt.Fprint(w, "OK")
					return
				}
			}

			if pendingFeedbackIDVal, exists := sess.Get("pending_feedback_reply"); exists {
				logger.Info("[Webhook] Admin %d is in feedback reply state", msg.From.ID)
				handleAdminPendingReplyText(deps, sess, msg, pendingFeedbackIDVal, "pending_feedback_reply", "反馈")
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, "OK")
				return
			}

			if pendingIssueIDVal, exists := sess.Get("pending_issue_reply"); exists {
				logger.Info("[Webhook] Admin %d is in issue reply state", msg.From.ID)
				handleAdminPendingReplyText(deps, sess, msg, pendingIssueIDVal, "pending_issue_reply", "问题")
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, "OK")
				return
			}
		}
	}

	// Handle commands —— 已在前面统一处理(含 pending 逃逸)，此处无需再判命令。

	// Handle search queries (non-command text)
	// 只有在非反馈状态下才执行搜索
	if len(msg.Text) > 1 {
		if HandlePrivateTMDBLink(msg, registry, deps.Telegram) {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "OK")
			return
		}
		HandleWebhookTextQuery(deps.Telegram, msg, deps.SessionMgr, cfg, registry, deps.MoviePilot, deps.SearchHistory, deps.TMDB)
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "OK")
}

// clearPendingInputStates 清除用户所有“等待文本输入”的 pending 态键，用于命令逃逸。
// 【P0 dead-end 逃逸修复】当用户在任意 pending 输入态打命令时调用本函数清状态，
// 让命令得以正常执行而不被吞掉。返回 true 表示用户原本确实处于某个 pending 态。
func clearPendingInputStates(deps *Dependencies, userID int64) bool {
	if deps == nil || deps.SessionMgr == nil {
		return false
	}
	sess := deps.SessionMgr.GetOrCreate(userID)
	if sess == nil || !deps.SessionMgr.IsValid(userID) {
		return false
	}

	hadPending := false
	// 反馈流程 + 管理员各类等待输入态
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

func handleAdminPendingReplyText(deps *Dependencies, sess interface{ Delete(string) }, msg *types.TelegramMessage, rawIssueID interface{}, stateKey string, label string) {
	var issueID int64
	switch v := rawIssueID.(type) {
	case int64:
		issueID = v
	case int:
		issueID = int64(v)
	case float64:
		issueID = int64(v)
	case string:
		fmt.Sscanf(v, "%d", &issueID)
	}
	if issueID <= 0 {
		sess.Delete(stateKey)
		deps.Telegram.SendMessage(msg.Chat.ID, "❌ 回复状态异常，请重新打开面板再试", "", nil)
		return
	}

	sess.Delete(stateKey)
	adminName := "管理员"
	if userSess := deps.SessionMgr.GetOrCreate(msg.From.ID); userSess != nil {
		if name, ok := userSess.GetString("name"); ok && name != "" {
			adminName = name
		}
	}

	if deps.IssueService == nil {
		deps.Telegram.SendMessage(msg.Chat.ID, "❌ 反馈服务未就绪", "", nil)
		return
	}

	_, err := deps.IssueService.AddReply(issueID, msg.From.ID, adminName, msg.Text, "admin")
	if err != nil {
		logger.Info("[Webhook] Issue reply failed for issue %d by user %d: %v", issueID, msg.From.ID, err)
		_, _ = deps.Telegram.SendMessage(msg.Chat.ID, "❌ 回复失败，请稍后再试", "", nil)
		return
	}

	if issue, exists := deps.IssueService.GetIssue(issueID); exists && issue.UserID != msg.From.ID {
		notifyMsg := fmt.Sprintf("💬 管理员回复了您的%s\n\n问题 #%d: %s\n\n📝 回复: %s", label, issue.ID, issue.Title, msg.Text)
		deps.Telegram.SendMessage(issue.UserID, notifyMsg, "", nil)
	}
	deps.Telegram.SendMessage(msg.Chat.ID, fmt.Sprintf("✅ %s回复已发送\n\n问题 #%d", label, issueID), "", nil)
}

// HandleWebhookGroupChat handles group chat messages from webhook.
// 群组只做轻量引导和通知，不在群里展开搜索结果/求片进度，避免暴露观影隐私。
func HandleWebhookGroupChat(
	telegram *services.TelegramClient,
	msg *types.TelegramMessage,
	moviepilot *services.MoviePilotClient,
	sessMgr *session.Manager,
	searchHistory *services.SearchHistoryService,
	tmdb *services.TMDBClient,
) {
	text := strings.TrimSpace(msg.Text)
	logger.Info("[WebhookGroupChat] ChatID=%d, Text=%q", msg.Chat.ID, text)

	if !strings.HasPrefix(text, "/") {
		return
	}

	cmd := strings.Fields(text)[0]
	if idx := strings.Index(cmd, "@"); idx >= 0 {
		cmd = cmd[:idx]
	}

	switch cmd {
	case "/start", "/search", "/wish", "/requests", "/watchlist", "/quota", "/portrait":
		sendCommunityCommandMessage(telegram, msg, "🔒 这是你的私密操作入口。请点下方菜单继续；群里只保留入库喜报、拼车到货等高光通知。", "", services.BuildStartKeyboardWithOptions(false, true))
	case "/id":
		text := fmt.Sprintf("📋 当前聊天信息\n\n聊天 ID: <code>%d</code>\n聊天类型: %s\n用户 ID: <code>%d</code>", msg.Chat.ID, msg.Chat.Type, msg.From.ID)
		telegram.SendMessage(msg.Chat.ID, text, "HTML", nil)
	default:
		return
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
	searchHistory *services.SearchHistoryService,
	tmdb *services.TMDBClient,
) {
	// Treat webhook text exactly like polling text so both transports share the
	// same page size, result keyboard, fallback and session behavior.
	query := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(msg.Text, "@oceancloudying_bot", ""), "@云海看板娘", ""))
	handler := handlers.NewSearchHandler(sessMgr, telegram, moviepilot, tmdb)
	handler.SetSearchHistory(searchHistory)
	if err := handler.HandleSearchQuery(msg.From.ID, msg.Chat.ID, query); err != nil {
		logger.Info("[Webhook] Search failed for query %q: %v", query, err)
	}
}

// PerformSearch performs the actual search in background
func PerformSearch(
	telegram *services.TelegramClient,
	msg *types.TelegramMessage,
	sessMgr *session.Manager,
	moviepilot *services.MoviePilotClient,
	tmdb *services.TMDBClient,
	searchHistory *services.SearchHistoryService,
) {
	query := msg.Text

	// Remove mention if present
	query = strings.ReplaceAll(query, "@oceancloudying_bot", "")
	query = strings.ReplaceAll(query, "@云海看板娘", "")
	query = strings.TrimSpace(query)

	// Add to search history
	if searchHistory != nil && query != "" {
		searchHistory.AddSearch(msg.From.ID, query)
	}

	results, err := moviepilot.SearchMedia(query, 1)
	if err != nil {
		// Log full error for admin debugging (already done above)
		logger.Info("[Search] Failed to search: %v", err)
		// Send user-friendly message without technical details
		text := "❌ 搜索失败：服务器暂时开小差了，请稍后再试。\n\n💡 如果持续失败，请联系管理员。"
		telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	if len(results.Results) == 0 {
		fallbackResults, fallbackQuery, _ := tryWebhookSearchFallback(moviepilot, query)
		if len(fallbackResults) > 0 {
			results.Results = fallbackResults
			query = fallbackQuery
			telegram.SendMessage(msg.Chat.ID, fmt.Sprintf("💡 已启用兜底搜索：%s", fallbackQuery), "", nil)
		} else {
			text := "😕 未找到相关内容\n\n💡 建议：\n• 检查拼写是否正确\n• 尝试使用更简短的关键词\n• 尝试使用英文搜索"
			telegram.SendMessage(msg.Chat.ID, text, "", nil)
			return
		}
	}

	// Filter out items with TMDB ID = 0 (invalid, can't create subscriptions)
	filtered := make([]services.SearchResult, 0, len(results.Results))
	for _, item := range results.Results {
		if item.ID > 0 {
			filtered = append(filtered, item)
		} else {
			logger.Info("[Search] Skipping result with ID=0: %s", item.Title)
		}
	}
	results.Results = filtered
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
		{Text: "🏠 主菜单", CallbackData: "start"},
	}

	if len(results.Results) >= 20 {
		navRow = append(navRow, types.TelegramInlineKeyboardButton{
			Text:         "➡️ 下一页",
			CallbackData: "search:page:2",
		})
	}

	keyboardRows = append(keyboardRows, navRow)

	keyboard := &types.TelegramInlineKeyboard{InlineKeyboard: keyboardRows}
	telegram.SendMessage(msg.Chat.ID, text, "", keyboard)
}

func tryWebhookSearchFallback(moviepilot *services.MoviePilotClient, query string) ([]services.SearchResult, string, error) {
	candidates := buildWebhookFallbackQueries(query)
	for _, q := range candidates {
		if q == "" || q == query {
			continue
		}
		results, err := moviepilot.SearchMedia(q, 1)
		if err != nil || results == nil {
			continue
		}
		if len(results.Results) > 0 {
			return results.Results, q, nil
		}
	}
	return nil, "", nil
}

func buildWebhookFallbackQueries(query string) []string {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}

	seen := map[string]bool{q: true}
	add := func(list *[]string, s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		*list = append(*list, s)
	}

	var out []string
	suffixes := []string{"电影", "电视剧", "剧", "动画", "动漫", "第1季", "第一季", "第2季", "第二季", "国语", "中字", "完整版"}
	trimmed := q
	for _, s := range suffixes {
		trimmed = strings.ReplaceAll(trimmed, s, "")
	}
	add(&out, trimmed)
	add(&out, extractWebhookCoreKeyword(q))
	for _, r := range []string{"（", "("} {
		if idx := strings.Index(q, r); idx > 0 {
			add(&out, strings.TrimSpace(q[:idx]))
		}
	}
	if y := extractWebhookYear(q); y != "" {
		add(&out, y)
	}
	return out
}

func extractWebhookCoreKeyword(s string) string {
	runes := []rune(strings.TrimSpace(s))
	keep := make([]rune, 0, len(runes))
	for _, r := range runes {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= 0x4e00 && r <= 0x9fff) {
			keep = append(keep, r)
		}
	}
	return strings.TrimSpace(string(keep))
}

func extractWebhookYear(s string) string {
	runes := []rune(s)
	for i := 0; i+3 < len(runes); i++ {
		chunk := string(runes[i : i+4])
		y, err := strconv.Atoi(chunk)
		if err == nil && y >= 1900 && y <= 2099 {
			return chunk
		}
	}
	return ""
}
