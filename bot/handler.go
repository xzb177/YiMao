package bot

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"emby-telegram-bot/callback"
	"emby-telegram-bot/session"
)

// Handler handles incoming bot messages
type Handler struct {
	sessionManager *session.SessionManager
	callbackParser *callback.CallbackParser
	messageEditor  *MessageEditor
	displayBuilder *DisplayBuilder
	quotaManager   *QuotaManager
	feedbackManager *FeedbackManager
	chatSystem     *ChatSystem // 添加聊天系统

	// Event handlers
	searchHandlers   map[string]SearchHandler
	subscribeHandlers map[string]SubscribeHandler
	downloadHandlers map[string]DownloadHandler

	mu sync.RWMutex
}

// SearchHandler handles search requests
type SearchHandler func(userID int64, query string, page int) (*SearchResult, error)

// SubscribeHandler handles subscribe requests
type SubscribeHandler func(userID int64, mediaID string, season int) error

// DownloadHandler handles download requests
type DownloadHandler func(userID int64, torrentID string) error

// SearchResult contains search results
type SearchResult struct {
	Query    string
	Page     int
	PageSize int
	Total    int
	Items    []session.SearchItem
}

// Import SearchItem from session package
type SearchItem = session.SearchItem

// NewHandler creates a new bot handler
func NewHandler() *Handler {
	h := &Handler{
		sessionManager: session.NewSessionManager(),
		callbackParser: callback.NewCallbackParser(),
		messageEditor:  NewMessageEditor(),
		displayBuilder: NewDisplayBuilder(callback.NewCallbackParser()),
		searchHandlers: make(map[string]SearchHandler),
		subscribeHandlers: make(map[string]SubscribeHandler),
		downloadHandlers: make(map[string]DownloadHandler),
	}

	// Start session cleanup goroutine
	go h.sessionManager.StartCleanup()

	return h
}

// SetChatSystem sets the chat system
func (h *Handler) SetChatSystem(cs *ChatSystem) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.chatSystem = cs
}

// RegisterSearchHandler registers a search handler
func (h *Handler) RegisterSearchHandler(key string, handler SearchHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.searchHandlers[key] = handler
}

// RegisterSubscribeHandler registers a subscribe handler
func (h *Handler) RegisterSubscribeHandler(key string, handler SubscribeHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subscribeHandlers[key] = handler
}

// RegisterDownloadHandler registers a download handler
func (h *Handler) RegisterDownloadHandler(key string, handler DownloadHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.downloadHandlers[key] = handler
}

// HandleMessage processes an incoming message
func (h *Handler) HandleMessage(update *TelegramUpdate) *MessageResponse {
	if update.Message == nil {
		return nil
	}

	message := update.Message
	userID := message.From.ID
	chatID := message.Chat.ID
	chatType := message.Chat.Type
	text := message.Text

	if text == "" {
		return nil
	}

	// 检查是否是回复机器人的消息
	isReplyToBot := false
	if message.ReplyToMessage != nil {
		log.Printf("[Handler] ReplyToMessage detected: MessageID=%d, FromID=%d, FromUsername=%q, IsBot=%v",
			message.ReplyToMessage.MessageID,
			message.ReplyToMessage.From.ID,
			message.ReplyToMessage.From.Username,
			message.ReplyToMessage.From.IsBot)
		if message.ReplyToMessage.From.IsBot {
			isReplyToBot = true
		}
	}

	log.Printf("[Handler] User %d (chat type: %s, replyToBot: %v): %s", userID, chatType, isReplyToBot, text)

	// 处理聊天系统的回复（回复消息或@机器人）
	if h.chatSystem != nil {
		// 检查是否应该触发聊天（回复机器人或@机器人）
		if isReplyToBot || IsMentioningBot(text) {
			userName := message.From.FirstName
			if message.From.Username != "" {
				userName = message.From.Username
			}
			response := h.chatSystem.GetChatResponse(text, userName, userID)
			return &MessageResponse{Text: response}
		}
	}

	// 限制：搜索功能仅在私聊中使用
	if chatType != "private" && !strings.HasPrefix(text, "/") {
		// 非私聊且不是命令，忽略
		log.Printf("[Handler] Ignoring non-private chat message")
		return nil
	}

	// Get or create user session
	session := h.sessionManager.GetOrCreateSession(userID, chatID)

	// Update last active time
	session.UpdateActivity()

	// Handle different message types
	switch {
	case strings.HasPrefix(text, "/"):
		return h.handleCommand(session, text)

	case strings.HasPrefix(text, "CALLBACK:"):
		// This should be handled by callback handler
		return nil

	case h.isNumericInput(text):
		return h.handleNumericInput(session, text)

	case strings.HasPrefix(text, "搜索"), strings.HasPrefix(text, "订阅"), strings.HasPrefix(text, "洗版"):
		return h.handleActionKeyword(session, text)

	case text == "p" || text == "P":
		return h.handlePagination(session, -1)

	case text == "n" || text == "N":
		return h.handlePagination(session, 1)

	default:
		// Treat as search query
		return h.handleSearch(session, text)
	}
}

// HandleCallback processes a callback query
func (h *Handler) HandleCallback(update *TelegramUpdate) *CallbackResponse {
	if update.CallbackQuery == nil {
		return nil
	}

	callback := update.CallbackQuery
	userID := callback.From.ID

	log.Printf("[Handler] Callback from user %d: %s", userID, callback.Data)

	// Parse callback data
	cb, err := h.callbackParser.Parse(callback.Data)
	if err != nil {
		log.Printf("[Handler] Failed to parse callback: %v", err)
		return &CallbackResponse{
			ShowAlert: true,
			Text:     "无效的操作",
		}
	}

	log.Printf("[Handler] Parsed callback: action=%s, data=%v", cb.Action, cb.Data)

	// Get user session
	session := h.sessionManager.GetSession(userID)
	if session == nil {
		log.Printf("[Handler] No session found for user %d", userID)
		return &CallbackResponse{
			ShowAlert: true,
			Text:     "会话已过期，请重新开始",
		}
	}

	session.UpdateActivity()

	// Handle different callback actions
	switch cb.Action {
	case "select":
		return h.handleSelectCallback(session, cb)

	case "search":
		return h.handleSearchCallback(session, cb)

	case "subscribe":
		return h.handleSubscribeCallback(session, cb)

	case "download":
		return h.handleDownloadCallback(session, cb)

	case "page":
		return h.handlePageCallback(session, cb)

	case "cancel":
		return h.handleCancelCallback(session, cb)

	case "back":
		return h.handleBackCallback(session, cb)

	default:
		log.Printf("[Handler] Unknown callback action: %s", cb.Action)
		return &CallbackResponse{
			ShowAlert: false,
			Text:     "未知操作",
		}
	}
}

// handleCommand handles slash commands
func (h *Handler) handleCommand(session *session.UserSession, command string) *MessageResponse {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil
	}

	cmd := parts[0]
	args := ""
	if len(parts) > 1 {
		args = strings.Join(parts[1:], " ")
	}

	switch cmd {
	case "/start":
		return h.handleStartCommand(session)

	case "/help":
		return h.sendHelpMessage(session)

	case "/search":
		if args == "" {
			return &MessageResponse{
				Text: "请输入搜索内容\n格式: /search 电影名称",
			}
		}
		return h.handleSearch(session, args)

	case "/subscribe":
		if args == "" {
			return &MessageResponse{
				Text: "请输入订阅内容\n格式: /subscribe 电影名称",
			}
		}
		return h.handleSubscribeCommand(session, args)

	case "/my":
		return h.handleMyRequests(session)

	case "/status":
		return h.handleStatus(session)

	case "/pending":
		return h.handlePending(session)

	case "/link":
		if args == "" {
			return &MessageResponse{
				Text: "请输入账号信息\n格式: /link 账号 密码",
			}
		}
		return h.handleLinkCommand(session, args)

	case "/quota":
		return h.handleQuotaCommand(session)

	case "/feedback", "/fb":
		return h.handleFeedback(session, args)

	case "/issues":
		return h.handleMyIssues(session)

	case "/allissues":
		return h.handleAllIssues(session)

	case "/ai":
		return h.handleAICommand(session, args)

	case "/recommend", "/rec", "/suggest":
		return h.handleRecommendCommand(session, args)

	default:
		return &MessageResponse{
			Text: fmt.Sprintf("未知命令: %s\n使用 /help 查看帮助", cmd),
		}
	}
}

// handleFeedback handles feedback command
func (h *Handler) handleFeedback(session *session.UserSession, args string) *MessageResponse {
	if h.feedbackManager == nil {
		return &MessageResponse{
			Text: "反馈功能暂不可用",
		}
	}

	if args == "" {
		return &MessageResponse{
			Text: "📝 报告问题\n\n" +
				"格式: /feedback <问题类型> <媒体ID> <描述>\n\n" +
				"问题类型:\n" +
				"• audio - 🔊 音频问题\n" +
				"• subtitle - 📝 字幕问题\n" +
				"• video - 🎬 视频问题\n" +
				"• other - 💬 其他问题\n\n" +
				"示例: /feedback video 12345 这部电影画面模糊",
		}
	}

	// Parse args: <type> [mediaID] <message>
	parts := strings.Fields(args)
	if len(parts) < 2 {
		return &MessageResponse{
			Text: "格式错误，请使用: /feedback <类型> <描述>\n" +
				"或者: /feedback <类型> <媒体ID> <描述>",
		}
	}

	problemType := parts[0]
	var message string
	var mediaID int

	// Check if second part is a number (media ID)
	if len(parts) >= 2 {
		if id, err := strconv.Atoi(parts[1]); err == nil && len(parts) >= 3 {
			mediaID = id
			message = strings.Join(parts[2:], " ")
		} else {
			message = strings.Join(parts[1:], " ")
		}
	}

	if message == "" {
		return &MessageResponse{
			Text: "请提供问题描述",
		}
	}

	// Default media type if no media ID provided
	mediaType := "movie"
	if mediaID == 0 {
		// No media ID, create a general issue
		mediaID = 1 // Use a dummy ID for general issues
	}

	// Create issue
	issue, err := h.feedbackManager.CreateIssue(session.UserID, fmt.Sprintf("用户%d", session.UserID), problemType, message, mediaID, mediaType)
	if err != nil {
		return &MessageResponse{
			Text: fmt.Sprintf("❌ 创建失败: %s", err.Error()),
		}
	}

	return &MessageResponse{
		Text: fmt.Sprintf("✅ 问题已报告！\n\n%s", FormatIssue(issue)),
	}
}

// handleMyIssues shows user's issues
func (h *Handler) handleMyIssues(session *session.UserSession) *MessageResponse {
	if h.feedbackManager == nil {
		return &MessageResponse{
			Text: "反馈功能暂不可用",
		}
	}

	issues, err := h.feedbackManager.GetMyIssues(session.UserID)
	if err != nil {
		return &MessageResponse{
			Text: fmt.Sprintf("❌ 获取失败: %s", err.Error()),
		}
	}

	if len(issues) == 0 {
		return &MessageResponse{
			Text: "📝 您还没有报告过问题",
		}
	}

	msg := fmt.Sprintf("📝 您的问题 (%d 条)\n\n", len(issues))

	for i, issue := range issues {
		if i >= 10 {
			msg += fmt.Sprintf("... 还有 %d 条\n", len(issues)-10)
			break
		}
		msg += FormatIssue(&issue)
		msg += "\n\n"
	}

	return &MessageResponse{
		Text: msg,
	}
}

// handleAllIssues shows all issues (admin)
func (h *Handler) handleAllIssues(session *session.UserSession) *MessageResponse {
	if h.feedbackManager == nil {
		return &MessageResponse{
			Text: "反馈功能暂不可用",
		}
	}

	issues, err := h.feedbackManager.GetAllIssues(20)
	if err != nil {
		return &MessageResponse{
			Text: fmt.Sprintf("❌ 获取失败: %s", err.Error()),
		}
	}

	if len(issues) == 0 {
		return &MessageResponse{
			Text: "📝 暂无问题报告",
		}
	}

	msg := fmt.Sprintf("📝 所有问题 (%d 条)\n\n", len(issues))

	for i, issue := range issues {
		if i >= 15 {
			msg += fmt.Sprintf("... 还有 %d 条\n", len(issues)-15)
			break
		}
		msg += FormatIssue(&issue)
		msg += fmt.Sprintf(" 👤 %s\n\n", issue.CreatedBy.DisplayName)
	}

	return &MessageResponse{
		Text: msg,
	}
}

// handleLinkCommand handles link command
func (h *Handler) handleLinkCommand(session *session.UserSession, args string) *MessageResponse {
	parts := strings.Fields(args)
	if len(parts) < 2 {
		return &MessageResponse{
			Text: "🔗 绑定账号\n\n格式: /link 账号 密码\n\n示例: /link myusername mypassword",
		}
	}

	// This would typically be handled by the legacy system
	// For now, return a message directing to use legacy handler
	return &MessageResponse{
		Text: "🔗 绑定账号\n\n请使用旧版本命令进行绑定",
	}
}

// handleQuotaCommand handles quota command
func (h *Handler) handleQuotaCommand(session *session.UserSession) *MessageResponse {
	if h.quotaManager == nil {
		return &MessageResponse{
			Text: "配额功能暂不可用",
		}
	}

	quota := h.quotaManager.GetUserQuota(session.UserID)
	if quota == nil {
		return &MessageResponse{
			Text: "❌ 未找到配额信息\n\n请先使用 /link 绑定账号",
		}
	}

	// Build quota status message
	var text strings.Builder
	text.WriteString("📊 今日配额使用情况\n\n")

	text.WriteString(fmt.Sprintf("🎬 电影: %d/%d", quota.MovieUsed, quota.MovieLimit))
	if quota.MovieUsed < quota.MovieLimit {
		remaining := quota.MovieLimit - quota.MovieUsed
		text.WriteString(fmt.Sprintf(" (剩余 %d)", remaining))
	} else {
		text.WriteString(" (已用完)")
	}
	text.WriteString("\n")

	text.WriteString(fmt.Sprintf("📺 剧集: %d/%d", quota.TVUsed, quota.TVLimit))
	if quota.TVUsed < quota.TVLimit {
		remaining := quota.TVLimit - quota.TVUsed
		text.WriteString(fmt.Sprintf(" (剩余 %d)", remaining))
	} else {
		text.WriteString(" (已用完)")
	}

	text.WriteString("\n\n💡 配额每天 00:00 自动重置")

	return &MessageResponse{
		Text: text.String(),
	}
}

// containsAny checks if the text contains any of the keywords
func containsAny(text string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// handleSearch handles search queries
func (h *Handler) handleSearch(session *session.UserSession, query string) *MessageResponse {
	h.mu.RLock()
	handler, exists := h.searchHandlers["default"]
	h.mu.RUnlock()

	if !exists {
		return h.displayBuilder.BuildErrorMessage("搜索功能暂不可用", "请稍后再试或联系管理员")
	}

	// Reset to first page for new search
	if session.SearchQuery != query {
		session.SearchQuery = query
		session.CurrentPage = 0
	}

	result, err := handler(session.UserID, query, session.CurrentPage)
	if err != nil {
		return h.displayBuilder.BuildErrorMessage("搜索失败", err.Error())
	}

	if result == nil || len(result.Items) == 0 {
		return h.displayBuilder.BuildNoResultsMessage(query)
	}

	// Store results in session
	session.SearchResults = result.Items
	session.TotalResults = result.Total

	// Build response with inline keyboard using display builder
	return h.displayBuilder.BuildSearchResultsMessage(session, result)
}

// handleNumericInput handles numeric selections (1-8 for items, 0 for auto)
func (h *Handler) handleNumericInput(session *session.UserSession, input string) *MessageResponse {
	if len(session.SearchResults) == 0 {
		return &MessageResponse{
			Text: "🔍 请先搜索内容\n\n直接输入影片名称即可搜索",
		}
	}

	// Parse input as number
	var num int
	_, err := fmt.Sscanf(input, "%d", &num)
	if err != nil || num <= 0 {
		return &MessageResponse{
			Text: "❌ 无效的输入\n\n请输入数字选择结果，或输入新的关键词搜索",
		}
	}

	// Calculate actual index (num is 1-based, so subtract 1 first)
	index := num - 1 + session.CurrentPage*8

	if index < 0 || index >= len(session.SearchResults) {
		maxNum := (len(session.SearchResults) - session.CurrentPage*8)
		if maxNum > 8 {
			maxNum = 8
		}
		return &MessageResponse{
			Text: fmt.Sprintf("❌ 请输入 1-%d 之间的数字", maxNum),
		}
	}

	// Get selected item
	item := session.SearchResults[index]
	session.SelectedItem = &item

	// Show item details with action buttons
	return h.buildItemDetailsMessage(session, &item)
}

// handlePagination handles page navigation
func (h *Handler) handlePagination(session *session.UserSession, direction int) *MessageResponse {
	if len(session.SearchResults) == 0 {
		return &MessageResponse{
			Text: "📄 没有可翻页的内容\n\n请先搜索内容",
		}
	}

	newPage := session.CurrentPage + direction

	if newPage < 0 {
		return &MessageResponse{
			Text: "⬅️ 已经是第一页了",
		}

	}

	maxPage := (session.TotalResults - 1) / 8
	if newPage > maxPage {
		return &MessageResponse{
			Text: "➡️ 已经是最后一页了",
		}
	}

	session.CurrentPage = newPage

	// Re-send search for new page
	return h.handleSearch(session, session.SearchQuery)
}

// handleActionKeyword handles action keywords (搜索/订阅/洗版)
func (h *Handler) handleActionKeyword(session *session.UserSession, text string) *MessageResponse {
	if strings.HasPrefix(text, "订阅") {
		query := strings.TrimPrefix(text, "订阅")
		query = strings.TrimPrefix(query, ":")
		query = strings.TrimPrefix(query, "：")
		query = strings.TrimSpace(query)
		return h.handleSubscribeCommand(session, query)
	}

	if strings.HasPrefix(text, "洗版") {
		query := strings.TrimPrefix(text, "洗版")
		query = strings.TrimPrefix(query, ":")
		query = strings.TrimPrefix(query, "：")
		query = strings.TrimSpace(query)
		return h.handleBestVersion(session, query)
	}

	// Default to search
	query := strings.TrimPrefix(text, "搜索")
	query = strings.TrimPrefix(query, ":")
	query = strings.TrimPrefix(query, "：")
	query = strings.TrimSpace(query)
	return h.handleSearch(session, query)
}

// handleSubscribeCommand handles subscribe command
func (h *Handler) handleSubscribeCommand(session *session.UserSession, query string) *MessageResponse {
	// First search for the media
	searchResult, err := h.performSearch(session, query)
	if err != nil {
		return &MessageResponse{
			Text: fmt.Sprintf("搜索失败: %v", err),
		}
	}

	if len(searchResult.Items) == 0 {
		return &MessageResponse{
			Text: fmt.Sprintf("未找到: %s", query),
		}
	}

	// If only one result, subscribe directly
	if len(searchResult.Items) == 1 {
		item := searchResult.Items[0]
		return h.performSubscribe(session, &item)
	}

	// Store results and show selection
	session.SearchResults = searchResult.Items
	session.TotalResults = searchResult.Total
	session.PendingAction = "subscribe"

	return h.buildSearchResultsMessage(session, searchResult)
}

// handleBestVersion handles best version (洗版) command
func (h *Handler) handleBestVersion(session *session.UserSession, query string) *MessageResponse {
	// Similar to subscribe but with best_version flag
	return h.handleSubscribeCommand(session, query)
}

// Helper methods

func (h *Handler) isNumericInput(text string) bool {
	text = strings.TrimSpace(text)
	for _, c := range text {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(text) > 0
}

func (h *Handler) performSearch(session *session.UserSession, query string) (*SearchResult, error) {
	h.mu.RLock()
	handler, exists := h.searchHandlers["default"]
	h.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("搜索功能不可用")
	}

	return handler(session.UserID, query, 0)
}

func (h *Handler) performSubscribe(session *session.UserSession, item *SearchItem) *MessageResponse {
	h.mu.RLock()
	handler, exists := h.subscribeHandlers["default"]
	h.mu.RUnlock()

	if !exists {
		return &MessageResponse{
			Text: "订阅功能暂不可用",
		}
	}

	mediaID := fmt.Sprintf("%s:%d", item.Type, item.ID)
	err := handler(session.UserID, mediaID, 1)
	if err != nil {
		return &MessageResponse{
			Text: fmt.Sprintf("订阅失败: %v", err),
		}
	}

	return &MessageResponse{
		Text:    fmt.Sprintf("✅ 已订阅: %s", item.Title),
		EditMode: true,
	}
}

// Message builders

func (h *Handler) sendHelpMessage(session *session.UserSession) *MessageResponse {
	// 检查用户是否已绑定账号
	isBound := false
	if h.quotaManager != nil {
		quota := h.quotaManager.GetUserQuota(session.UserID)
		isBound = quota != nil && (quota.MovieLimit > 0 || quota.TVLimit > 0)
	}

	helpText := `📖 *云海看板娘 - 使用指南*

━━━━━━━━━━━━━━━━━━━━━

📱 *快速开始*

1️⃣ 直接输入影片名称搜索
   示例：「阿凡达」「沙丘2」「繁花」

2️⃣ 点击结果查看详情

3️⃣ 点击「📋 发起请求」按钮

4️⃣ 坐等完成，自动通知你 🎉

━━━━━━━━━━━━━━━━━━━━━`

	if !isBound {
		helpText += `
⚠️ *使用前需要先绑定账号*

请使用 /link 命令绑定你的 Jellyfin 账号

绑定格式：` + "`" + `/link 账号 密码` + "`" + `

━━━━━━━━━━━━━━━━━━━━━
`
	}

	helpText += `
🔍 *搜索与发现*
` + "`" + `/search` + "`" + ` <关键词> - 搜索内容
` + "`" + `/ai` + "`" + ` <问题> - AI 智能助手
` + "`" + `/recommend` + "`" + ` <心情> - 智能推荐
` + "`" + `/trending` + "`" + ` - 热门搜索
` + "`" + `/history` + "`" + ` - 搜索历史

👤 *个人中心*
` + "`" + `/profile` + "`" + ` - 我的资料卡片
` + "`" + `/daily` + "`" + ` - 每日签到领奖励
` + "`" + `/my` + "`" + ` - 我的请求状态
` + "`" + `/quota` + "`" + ` - 配额查询
` + "`" + `/prefs` + "`" + ` - 通知设置

🏆 *社交竞技*
` + "`" + `/leaderboard` + "`" + ` - 用户排行榜
` + "`" + `/challenges` + "`" + ` - 每日挑战任务
` + "`" + `/badges` + "`" + ` - 我的成就徽章
` + "`" + `/top` + "`" + ` - 热门内容榜

🔗 *账号管理*
` + "`" + `/link` + "`" + ` <账号> <密码> - 绑定账号
` + "`" + `/quicklink` + "`" + ` <账号> <密码> - 快速绑定
` + "`" + `/unlink` + "`" + ` - 解绑账号

💬 *反馈与帮助*
` + "`" + `/feedback` + "`" + ` <内容> - 提交反馈
` + "`" + `/issues` + "`" + ` - 我的问题

━━━━━━━━━━━━━━━━━━━━━

💡 *小贴士*

• 别名: ` + "`" + `/h` + "` = `/help`, `/s` = `/search`" + `
• 点击左下角菜单快速访问
• 完成后自动通知你

祝你观影愉快！🎬`

	return &MessageResponse{
		Text: helpText,
	}
}

func (h *Handler) buildSearchResultsMessage(session *session.UserSession, result *SearchResult) *MessageResponse {
	// Use the new display builder for beautiful formatting
	return h.displayBuilder.BuildSearchResultsMessage(session, result)
}

func (h *Handler) buildItemDetailsMessage(session *session.UserSession, item *SearchItem) *MessageResponse {
	// Get quota info
	var quota *QuotaInfo
	if h.quotaManager != nil {
		q := h.quotaManager.GetUserQuota(session.UserID)
		if q != nil {
			quota = &QuotaInfo{
				MovieLimit: q.MovieLimit,
				MovieUsed:  q.MovieUsed,
				TVLimit:    q.TVLimit,
				TVUsed:     q.TVUsed,
			}
		}
	}

	// Use the new display builder for beautiful formatting
	return h.displayBuilder.BuildItemDetailsMessage(session, item, quota)
}

// Placeholder handlers for callbacks
func (h *Handler) handleSelectCallback(session *session.UserSession, cb *callback.Callback) *CallbackResponse {
	// Get the index from callback data
	indexStr := cb.Data["index"]
	if indexStr == "" {
		return &CallbackResponse{Text: "无效的选择"}
	}

	// Parse index (1-based)
	index, err := strconv.Atoi(indexStr)
	if err != nil || index < 1 {
		return &CallbackResponse{Text: "无效的编号"}
	}

	// Calculate actual index in results
	actualIndex := (session.CurrentPage * 8) + index - 1

	if actualIndex < 0 || actualIndex >= len(session.SearchResults) {
		return &CallbackResponse{Text: fmt.Sprintf("请输入 1-%d 之间的数字", len(session.SearchResults))}
	}

	// Get selected item
	item := session.SearchResults[actualIndex]
	session.SelectedItem = &item

	// Show item details with action buttons
	return h.buildItemDetailsCallbackResponse(session, &item)
}

func (h *Handler) buildItemDetailsCallbackResponse(session *session.UserSession, item *SearchItem) *CallbackResponse {
	// Debug logging
	log.Printf("[Handler] Item details: Title='%s', Year=%d, Type='%s', Rating=%.1f, ID='%s'",
		item.Title, item.Year, item.Type, item.Rating, item.ID)

	// Get quota info
	var quota *QuotaInfo
	if h.quotaManager != nil {
		q := h.quotaManager.GetUserQuota(session.UserID)
		if q != nil {
			quota = &QuotaInfo{
				MovieLimit: q.MovieLimit,
				MovieUsed:  q.MovieUsed,
				TVLimit:    q.TVLimit,
				TVUsed:     q.TVUsed,
			}
		}
	}

	// Use the new display builder
	msgResp := h.displayBuilder.BuildItemDetailsMessage(session, item, quota)

	// Convert to CallbackResponse
	return &CallbackResponse{
		Text:     msgResp.Text,
		Keyboard: msgResp.Keyboard,
		EditMode: true,
	}
}

func (h *Handler) handleSearchCallback(session *session.UserSession, cb *callback.Callback) *CallbackResponse {
	return &CallbackResponse{Text: "搜索中..."}
}

func (h *Handler) handleSubscribeCallback(session *session.UserSession, cb *callback.Callback) *CallbackResponse {
	// Get media ID and title from callback data
	mediaID := cb.Data["id"]
	title := cb.Data["title"]
	mediaType := cb.Data["type"]

	if mediaID == "" {
		return &CallbackResponse{
			ShowAlert: true,
			Text:     "❌ 操作失败：缺少媒体信息\n\n请重新搜索后重试",
		}
	}

	log.Printf("[Handler] Subscribe request: mediaID=%s, title=%s, userID=%d, type=%s", mediaID, title, session.UserID, mediaType)

	// Determine media type for quota check
	quotaMediaType := "movie"
	if mediaType == "tv" {
		quotaMediaType = "tv"
	}

	// Check quota if available - with better message
	if h.quotaManager != nil {
		quota := h.quotaManager.GetUserQuota(session.UserID)
		canRequest := false
		var quotaMsg string

		if quotaMediaType == "movie" {
			canRequest = quota.MovieUsed < quota.MovieLimit
			if !canRequest {
				quotaMsg = fmt.Sprintf("🚫 今日电影配额已用完\n\n今日已请求 %d 部电影，达到每日限额 %d 部\n\n💡 明天配额会自动重置，请明天再试", quota.MovieUsed, quota.MovieLimit)
			}
		} else {
			canRequest = quota.TVUsed < quota.TVLimit
			if !canRequest {
				quotaMsg = fmt.Sprintf("🚫 今日剧集配额已用完\n\n今日已请求 %d 部剧集，达到每日限额 %d 部\n\n💡 明天配额会自动重置，请明天再试", quota.TVUsed, quota.TVLimit)
			}
		}

		if !canRequest {
			return &CallbackResponse{
				ShowAlert: true,
				Text:     quotaMsg,
			}
		}
	}

	// Convert string ID to int
	id, err := strconv.Atoi(mediaID)
	if err != nil {
		return &CallbackResponse{
			ShowAlert: true,
			Text:     "❌ 操作失败：媒体信息格式错误\n\n请重新搜索后重试",
		}
	}

	// Call subscribe handler
	h.mu.RLock()
	handler, exists := h.subscribeHandlers["default"]
	h.mu.RUnlock()

	if !exists {
		return &CallbackResponse{
			ShowAlert: true,
			Text:     "❌ 订阅功能暂时不可用\n\n请稍后再试或联系管理员",
		}
	}

	// Use movie as default type (Jellyseerr will auto-detect)
	requestMediaType := "movie"
	if mediaType == "tv" {
		requestMediaType = "tv"
	}
	fullMediaID := fmt.Sprintf("%s:%d", requestMediaType, id)

	log.Printf("[Handler] Calling subscribe handler with: %s", fullMediaID)

	err = handler(session.UserID, fullMediaID, 0)
	if err != nil {
		log.Printf("[Handler] Subscribe error: %v", err)

		// Check for common errors and provide helpful messages
		errMsg := err.Error()
		if strings.Contains(errMsg, "请先使用") || strings.Contains(errMsg, "绑定账号") {
			return &CallbackResponse{
				ShowAlert: true,
				Text:     "❌ 需要先绑定账号\n\n请使用 /link 命令绑定你的 Jellyfin 账号\n\n绑定格式：/link 账号 密码",
			}
		}

		// Check if media already exists in library
		if strings.Contains(errMsg, "已存在") || strings.Contains(errMsg, "already exists") || strings.Contains(errMsg, "available") {
			displayTitle := title
			if displayTitle == "" {
				displayTitle = "这部内容"
			}
			return &CallbackResponse{
				ShowAlert: true,
				Text:     fmt.Sprintf("✨ %s 已经在库中了！\n\n🎬 可以直接观看，无需请求\n\n去媒体库搜索一下吧", displayTitle),
			}
		}

		// Generic error with helpful message
		displayTitle := title
		if displayTitle == "" {
			displayTitle = "这部内容"
		}
		return &CallbackResponse{
			ShowAlert: true,
			Text:     fmt.Sprintf("❌ 请求失败：\n\n%s\n\n可能的原因：\n• 媒体已存在于库中\n• 网络连接问题\n• 服务器暂时不可用\n\n请稍后再试或联系管理员", errMsg),
		}
	}

	// Increment quota usage
	if h.quotaManager != nil {
		h.quotaManager.IncrementUsage(session.UserID, quotaMediaType)
	}

	// Return success message with more details
	displayTitle := title
	if displayTitle == "" {
		displayTitle = "这部内容"
	}

	// Build success message
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("✅ 请求已发送！\n\n🎬 %s\n\n", displayTitle))
	msg.WriteString("📋 状态：等待管理员处理\n")
	msg.WriteString("🔔 完成后会自动通知你\n")

	// Show remaining quota if available
	if h.quotaManager != nil {
		quota := h.quotaManager.GetUserQuota(session.UserID)
		if quotaMediaType == "movie" {
			remaining := quota.MovieLimit - quota.MovieUsed
			if remaining > 0 {
				msg.WriteString(fmt.Sprintf("\n💡 今日还可请求 %d 部电影", remaining))
			} else {
				msg.WriteString("\n🎊 今日电影配额已用完！")
			}
		} else {
			remaining := quota.TVLimit - quota.TVUsed
			if remaining > 0 {
				msg.WriteString(fmt.Sprintf("\n💡 今日还可请求 %d 部剧集", remaining))
			} else {
				msg.WriteString("\n🎊 今日剧集配额已用完！")
			}
		}
	}

	return &CallbackResponse{
		Text:     msg.String(),
		ShowAlert: true,
	}
}

func (h *Handler) handleDownloadCallback(session *session.UserSession, cb *callback.Callback) *CallbackResponse {
	return &CallbackResponse{
		ShowAlert: true,
		Text:     "下载功能开发中...",
	}
}

func (h *Handler) handlePageCallback(session *session.UserSession, cb *callback.Callback) *CallbackResponse {
	// Get page from callback data
	pageStr := cb.Data["page"]
	if pageStr == "" {
		// If no page specified, use "back" as indicator to return to results
		return h.buildSearchResultsCallbackResponse(session)
	}

	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}

	// Update session page
	session.CurrentPage = page - 1

	// Re-build search results message
	return h.buildSearchResultsCallbackResponse(session)
}

func (h *Handler) handleCancelCallback(session *session.UserSession, cb *callback.Callback) *CallbackResponse {
	// Clear session state
	session.SearchResults = nil
	session.SearchQuery = ""
	session.SelectedItem = nil
	session.CurrentPage = 0

	return &CallbackResponse{
		Text:     "❌ 已取消",
		EditMode: true,
	}
}

// handleBackCallback handles returning to previous view
func (h *Handler) handleBackCallback(session *session.UserSession, cb *callback.Callback) *CallbackResponse {
	target := cb.Data["target"]
	log.Printf("[Handler] handleBackCallback: target=%s, resultsCount=%d", target, len(session.SearchResults))
	if target == "results" || target == "" {
		return h.buildSearchResultsCallbackResponse(session)
	}
	return &CallbackResponse{Text: "未知返回目标"}
}

// buildSearchResultsCallbackResponse builds the search results response for callback
func (h *Handler) buildSearchResultsCallbackResponse(session *session.UserSession) *CallbackResponse {
	if len(session.SearchResults) == 0 {
		return &CallbackResponse{
			Text:     "🔍 搜索结果已过期\n\n请重新输入关键词搜索",
			EditMode: true,
		}
	}

	// Calculate pagination
	pageSize := 8
	startIdx := session.CurrentPage * pageSize
	endIdx := startIdx + pageSize
	if endIdx > len(session.SearchResults) {
		endIdx = len(session.SearchResults)
	}

	// Build a result object for display
	result := &SearchResult{
		Query:    session.SearchQuery,
		Page:     session.CurrentPage,
		PageSize: pageSize,
		Total:    session.TotalResults,
		Items:    session.SearchResults[startIdx:endIdx],
	}

	// Use the display builder
	msgResp := h.displayBuilder.BuildSearchResultsMessage(session, result)

	return &CallbackResponse{
		Text:     msgResp.Text,
		Keyboard: msgResp.Keyboard,
		EditMode: true,
	}
}

func (h *Handler) handleMyRequests(session *session.UserSession) *MessageResponse {
	return &MessageResponse{Text: "我的请求功能开发中..."}
}

func (h *Handler) handleStatus(session *session.UserSession) *MessageResponse {
	return &MessageResponse{Text: "状态功能开发中..."}
}

func (h *Handler) handlePending(session *session.UserSession) *MessageResponse {
	return &MessageResponse{Text: "待处理功能开发中..."}
}

// MessageResponse represents a response to be sent
type MessageResponse struct {
	Text     string
	Keyboard [][]map[string]string
	EditMode bool
}

// CallbackResponse represents a callback query response
type CallbackResponse struct {
	Text     string
	Keyboard [][]map[string]string
	ShowAlert bool
	EditMode  bool
}

// TelegramUpdate represents an incoming update from Telegram
type TelegramUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		MessageID int64 `json:"message_id"`
		From      struct {
			ID        int64  `json:"id"`
			IsBot     bool   `json:"is_bot"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			Username  string `json:"username"`
		} `json:"from"`
		Chat struct {
			ID int64 `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
		Date            int64 `json:"date"`
		Text            string `json:"text"`
		ReplyToMessage  *struct {
			MessageID int64 `json:"message_id"`
			From      struct {
				ID        int64 `json:"id"`
				IsBot     bool   `json:"is_bot"`
				FirstName string `json:"first_name"`
				Username  string `json:"username"`
			} `json:"from"`
		} `json:"reply_to_message"`
	} `json:"message"`
	CallbackQuery *struct {
		ID      string `json:"id"`
		From    struct {
			ID        int64  `json:"id"`
			IsBot     bool   `json:"is_bot"`
			FirstName string `json:"first_name"`
			Username  string `json:"username"`
		} `json:"from"`
		Message struct {
			MessageID int64 `json:"message_id"`
			Chat      struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
		Data string `json:"callback_data"`
	} `json:"callback_query"`
}

// JSONMessage is used for sending messages to Telegram
type JSONMessage struct {
	ChatID      int64                    `json:"chat_id"`
	Text        string                   `json:"text"`
	ParseMode   string                   `json:"parse_mode,omitempty"`
	ReplyMarkup *TelegramInlineKeyboard  `json:"reply_markup,omitempty"`
}

// TelegramInlineKeyboard represents inline keyboard markup
type TelegramInlineKeyboard struct {
	InlineKeyboard [][]map[string]string `json:"inline_keyboard"`
}

// JSONToUpdate converts JSON to TelegramUpdate
func JSONToUpdate(data []byte) (*TelegramUpdate, error) {
	var update TelegramUpdate
	err := json.Unmarshal(data, &update)
	if err != nil {
		return nil, err
	}
	return &update, nil
}

// HTTP handlers for Gin framework
func (h *Handler) GinHandler() gin.HandlerFunc {
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

		response := h.HandleMessage(update)
		if response != nil {
			// Send response via message editor
			if update.Message != nil {
				chatID := update.Message.Chat.ID
				messageID := update.Message.MessageID

				if response.EditMode && messageID > 0 {
					h.messageEditor.EditMessage(chatID, messageID, response.Text, response.Keyboard)
				} else {
					h.messageEditor.SendMessage(chatID, response.Text, response.Keyboard)
				}
			}
		}

		c.JSON(200, gin.H{"status": "ok"})
	}
}

func (h *Handler) GinCallbackHandler() gin.HandlerFunc {
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

		response := h.HandleCallback(update)
		if response != nil {
			// Answer callback query
			if update.CallbackQuery != nil {
				h.messageEditor.AnswerCallback(update.CallbackQuery.ID, response.Text, response.ShowAlert)

				// Edit message if needed
				if response.EditMode {
					chatID := update.CallbackQuery.Message.Chat.ID
					messageID := update.CallbackQuery.Message.MessageID
					// Edit message with keyboard
					h.messageEditor.EditMessage(chatID, messageID, response.Text, response.Keyboard)
				}
			}
		}

		c.JSON(200, gin.H{"status": "ok"})
	}
}

// SetSessionManager sets the session manager dependency
func (h *Handler) SetSessionManager(sm *session.SessionManager) {
	h.sessionManager = sm
}

// SetMessageEditor sets the message editor dependency
func (h *Handler) SetMessageEditor(me *MessageEditor) {
	h.messageEditor = me
}

// SetQuotaManager sets the quota manager dependency
func (h *Handler) SetQuotaManager(qm *QuotaManager) {
	h.quotaManager = qm
}

// SetFeedbackManager sets the feedback manager dependency
func (h *Handler) SetFeedbackManager(fm *FeedbackManager) {
	h.feedbackManager = fm
}

// Cleanup
func (h *Handler) Stop() {
	h.sessionManager.Stop()
}

// handleStartCommand handles /start command
func (h *Handler) handleStartCommand(session *session.UserSession) *MessageResponse {
	// Get display name from context if available
	displayName := "朋友"
	if session.Context != nil {
		if name, ok := session.Context["first_name"].(string); ok && name != "" {
			displayName = name
		} else if name, ok := session.Context["username"].(string); ok && name != "" {
			displayName = "@" + name
		}
	}

	msg := fmt.Sprintf("👋 *欢迎回来，%s！*\n\n", displayName)
	msg += "我可以帮你搜索和请求影视内容\n\n"
	msg += "🔍 *快速搜索*\n"
	msg += "直接输入电影或剧集名称\n\n"
	msg += "📋 *其他功能*\n"
	msg += "`/help` - 查看完整帮助\n"
	msg += "`/recommend` - 智能推荐\n"
	msg += "`/ai` - AI 助手\n"
	msg += "`/profile` - 我的资料"

	return &MessageResponse{
		Text: msg,
	}
}

// handleAICommand handles /ai command
func (h *Handler) handleAICommand(session *session.UserSession, args string) *MessageResponse {
	// Import ai package to use AI functionality
	response, err := getAIResponse(session.UserID, args)
	if err != nil {
		return &MessageResponse{
			Text: fmt.Sprintf("🤖 *AI 助手错误*\n\n%s", formatAIError(err)),
		}
	}
	return &MessageResponse{
		Text: response,
	}
}

// handleRecommendCommand handles /recommend command
func (h *Handler) handleRecommendCommand(session *session.UserSession, mood string) *MessageResponse {
	if mood == "" {
		msg := "🎯 *智能推荐*\n\n"
		msg += "请告诉我你的心情或偏好：\n\n"
		msg += "• 开心/放松 - 轻松喜剧\n"
		msg += "• 紧张/刺激 - 悬疑惊悚\n"
		msg += "• 感动/温情 - 爱情剧情\n"
		msg += "• 好奇/探索 - 科幻纪录片\n\n"
		msg += "用法: /recommend 心情"
		return &MessageResponse{
			Text: msg,
		}
	}

	// Get AI recommendations
	response, err := getAIRecommendations(mood, 5)
	if err != nil {
		msg := "🤖 *推荐失败*\n\n"
		if strings.Contains(err.Error(), "not enabled") || strings.Contains(err.Error(), "enabled") {
			msg += "AI 功能暂未启用\n\n"
			msg += "💡 请联系管理员配置 ZHIPU_API_KEY"
		} else {
			msg += "抱歉，AI 服务暂时不可用\n\n"
			msg += "💡 请稍后再试"
		}
		return &MessageResponse{
			Text: msg,
		}
	}
	return &MessageResponse{
		Text: response,
	}
}

// getAIResponse is a bridge to the AI package
func getAIResponse(userID int64, args string) (string, error) {
	// This function will be implemented by calling the actual AI package
	// For now, return a basic response
	if args == "" {
		return `🤖 **AI 智能助手**

我可以帮你：
• **推荐** - "我想看悬疑片"
• **搜索** - "帮我找泰坦尼克号"
• **解释** - "星际穿越讲什么"
• **心情推荐** - "心情不好想看喜剧"

直接和我说你想要什么！`, nil
	}
	return fmt.Sprintf("🤖 你说: %s\n\n💡 AI 功能正在开发中，敬请期待！", args), nil
}

// getAIRecommendations gets AI recommendations
func getAIRecommendations(mood string, count int) (string, error) {
	// This is a placeholder - in production, call the actual AI package
	results := []struct {
		Title string
		Genre string
		Reason string
	}{
		{"肖申克的救赎", "剧情", "经典励志片，适合各种心情"},
		{"阿甘正传", "剧情/喜剧", "温暖人心的故事"},
		{"当幸福来敲门", "剧情", "积极向上的励志电影"},
		{"疯狂动物城", "动画/喜剧", "轻松有趣，老少皆宜"},
		{"寻梦环游记", "动画/奇幻", "温馨感人的家庭故事"},
	}

	msg := fmt.Sprintf("🤖 **AI 智能推荐** - %s的心情\n\n", mood)
	for i, r := range results {
		if i >= count {
			break
		}
		msg += fmt.Sprintf("%d. 🎬 **%s**\n", i+1, r.Title)
		msg += fmt.Sprintf("   🎭 %s\n", r.Genre)
		msg += fmt.Sprintf("   💡 %s\n\n", r.Reason)
	}
	return msg, nil
}

// formatAIError formats AI errors
func formatAIError(err error) string {
	if strings.Contains(err.Error(), "not enabled") {
		return "AI 功能暂未启用\n\n💡 请联系管理员配置 ZHIPU_API_KEY"
	}
	return "抱歉，AI 服务暂时不可用\n\n💡 请稍后再试"
}

// SetAdminChecker sets the admin checker function (for integration)
func (h *Handler) SetAdminChecker(fn func(int64) bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Store the checker for use in security checks
	log.Printf("[Handler] Admin checker set")
}
