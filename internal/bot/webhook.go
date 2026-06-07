package bot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/config"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
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
		logger.Info("Failed to decode update: %v", err)
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
	logger.Info("[Webhook] Callback from user %d: %s", cb.From.ID, cb.Data)

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
		UserID:     cb.From.ID,
		ChatID:     cb.Message.Chat.ID,
		MessageID:  cb.Message.MessageID,
		CallbackID: cb.ID,
		Callback:   parsed,
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
		logger.Info("Handler error: %v", err)
		if callbackMsg == "" {
			callbackMsg = "操作失败"
		}
		showAlert = true
	}

	if showAlert && len(callbackMsg) > 200 {
		callbackMsg = callbackMsg[:197] + "..."
	}

	if err := telegram.AnswerCallback(cb.ID, callbackMsg, showAlert); err != nil {
		logger.Info("[Callback] AnswerCallback error: %v", err)
	}

	// Send or edit message
	if resp != nil {
		keyboard := ConvertKeyboard(resp.Keyboard)

		// Use HTML as default parse mode if not specified
		parseMode := resp.ParseMode
		if parseMode == "" {
			parseMode = "HTML"
		}

		// Check if we need to send a photo
		if resp.Photo != "" {
			// Delete the original message first
			telegram.DeleteMessage(ctx.ChatID, ctx.MessageID)
			// Send photo with caption and keyboard
			// Use SendPhotoWithAuth for reliable delivery (downloads and uploads the image)
			caption := resp.PhotoCaption
			if caption == "" {
				caption = resp.Text
			}
			logger.Info("[Webhook] Sending photo via proxy upload: %s", resp.Photo)
			if _, sendErr := telegram.SendPhotoWithAuth(ctx.ChatID, resp.Photo, caption, nil, keyboard); sendErr != nil {
				logger.Info("[Webhook] SendPhotoWithAuth error: %v, trying URL method", sendErr)
				// Fallback to URL method if proxy upload fails
				telegram.SendPhoto(ctx.ChatID, resp.Photo, caption, keyboard)
			}
		} else if resp.Text != "" {
			if resp.Edit {
				// Edit existing message
				telegram.EditMessage(ctx.ChatID, ctx.MessageID, resp.Text, parseMode, keyboard)
			} else {
				// Send new message
				telegram.SendMessage(ctx.ChatID, resp.Text, parseMode, keyboard)
			}
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
	logger.Info("[Webhook] Message from user %d (chat: %d, type: %s): %s", msg.From.ID, msg.Chat.ID, msg.Chat.Type, msg.Text)

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

			// 用户正在反馈流程中，处理反馈文本/图片并返回
			if err := deps.FeedbackHandler.HandleFeedbackWithPhoto(msg.From.ID, msg.Chat.ID, msg.Text, photoFileID); err != nil {
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
					if resp.Text != "" {
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

	// Handle commands
	if strings.HasPrefix(msg.Text, "/") {
		HandleCommand(deps.Telegram, msg, cfg, deps.AdminService, deps.BindingRequest, deps.QuotaService, deps.UserMapping, deps.SessionMgr, deps.WishHandler, deps.MyRequests)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
		return
	}

	// Check if user is in explicit AI chat mode.
	if isAIChatMode(deps.SessionMgr, msg.From.ID, msg.Chat.ID) {
		logger.Info("[Webhook] User %d is in AI chat mode, handling with AI", msg.From.ID)
		handleAIChatMessage(msg.From.ID, msg.Chat.ID, msg.Text, deps.Telegram, deps.SessionMgr)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
		return
	}

	// Handle search queries (non-command text)
	// 只有在非反馈状态下才执行搜索
	if len(msg.Text) > 1 {
		HandleWebhookTextQuery(deps.Telegram, msg, deps.SessionMgr, cfg, registry, deps.MoviePilot, deps.SearchHistory, deps.TMDB)
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "OK")
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
		deps.Telegram.SendMessage(msg.Chat.ID, fmt.Sprintf("❌ 回复失败: %v", err), "", nil)
		return
	}

	if issue, exists := deps.IssueService.GetIssue(issueID); exists && issue.UserID != msg.From.ID {
		notifyMsg := fmt.Sprintf("💬 管理员回复了您的%s\n\n问题 #%d: %s\n\n📝 回复: %s", label, issue.ID, issue.Title, msg.Text)
		deps.Telegram.SendMessage(issue.UserID, notifyMsg, "", nil)
	}
	deps.Telegram.SendMessage(msg.Chat.ID, fmt.Sprintf("✅ %s回复已发送\n\n问题 #%d", label, issueID), "", nil)
}

// HandleWebhookGroupChat handles group chat messages from webhook
// 群组中完全禁用交互功能，仅用于接收入库通知推送
func HandleWebhookGroupChat(
	telegram *services.TelegramClient,
	msg *types.TelegramMessage,
	moviepilot *services.MoviePilotClient,
	sessMgr *session.Manager,
	searchHistory *services.SearchHistoryService,
	tmdb *services.TMDBClient,
) {
	// 群组中不响应任何消息，只用于接收入库通知
	logger.Info("[WebhookGroupChat] ChatID=%d: Message ignored (groups are notifications only)", msg.Chat.ID)
	return
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
	// Treat as search query
	PerformSearch(telegram, msg, sessMgr, moviepilot, tmdb, searchHistory)
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
