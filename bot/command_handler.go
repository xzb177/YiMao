package bot

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
)

// CommandHandler handles bot commands with enterprise-grade error handling
type CommandHandler struct {
	// Dependencies
	quotaManager   *QuotaManager
	feedbackManager *FeedbackManager
	searchHandlers map[string]SearchHandler
	subscribeHandlers map[string]SubscribeHandler

	// Quick action handlers for callback buttons
	quickActionHandlers map[string]QuickActionHandler

	mu sync.RWMutex
}

// QuickActionHandler handles quick action button callbacks
type QuickActionHandler func(userID int64, params map[string]string) *MessageResponse

// QuickActionConfig defines a quick action configuration
type QuickActionConfig struct {
	CallbackData string
	Text         string
	Handler      QuickActionHandler
	Description  string
	RequiresAuth bool // Requires user to be linked
}

// NewCommandHandler creates a new command handler
func NewCommandHandler() *CommandHandler {
	h := &CommandHandler{
		searchHandlers:      make(map[string]SearchHandler),
		subscribeHandlers:   make(map[string]SubscribeHandler),
		quickActionHandlers: make(map[string]QuickActionHandler),
	}

	// Register default quick actions
	h.registerDefaultQuickActions()

	return h
}

// registerDefaultQuickActions registers built-in quick actions
func (h *CommandHandler) registerDefaultQuickActions() {
	// Search action
	h.RegisterQuickAction(QuickActionConfig{
		CallbackData: "action_search",
		Text:         "🔍 搜索内容",
		Handler:      h.handleQuickSearch,
		Description:  "Search for content",
	})

	// My requests
	h.RegisterQuickAction(QuickActionConfig{
		CallbackData: "action_myrequests",
		Text:         "📋 我的请求",
		Handler:      h.handleQuickMyRequests,
		Description:  "View my requests",
	})

	// Settings
	h.RegisterQuickAction(QuickActionConfig{
		CallbackData: "action_settings",
		Text:         "⚙️ 设置",
		Handler:      h.handleQuickSettings,
		Description:  "Open settings",
	})

	// Help
	h.RegisterQuickAction(QuickActionConfig{
		CallbackData: "action_help",
		Text:         "❓ 帮助",
		Handler:      h.handleQuickHelp,
		Description:  "Show help",
	})

	// Trending search
	h.RegisterQuickAction(QuickActionConfig{
		CallbackData: "search_trending",
		Text:         "🔥 热门推荐",
		Handler:      h.handleTrendingSearch,
		Description:  "Search trending content",
	})

	// Hot TV shows
	h.RegisterQuickAction(QuickActionConfig{
		CallbackData: "search_tv_hot",
		Text:         "📺 热播剧集",
		Handler:      h.handleHotTVSearch,
		Description:  "Search hot TV shows",
	})

	// New movies
	h.RegisterQuickAction(QuickActionConfig{
		CallbackData: "search_movie_new",
		Text:         "🎬 最新电影",
		Handler:      h.handleNewMoviesSearch,
		Description:  "Search new movies",
	})
}

// RegisterQuickAction registers a quick action
func (h *CommandHandler) RegisterQuickAction(config QuickActionConfig) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.quickActionHandlers[config.CallbackData] = config.Handler
	log.Printf("[CommandHandler] Registered quick action: %s (%s)", config.CallbackData, config.Description)
}

// RegisterSearchHandler registers a search handler
func (h *CommandHandler) RegisterSearchHandler(key string, handler SearchHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.searchHandlers[key] = handler
}

// RegisterSubscribeHandler registers a subscribe handler
func (h *CommandHandler) RegisterSubscribeHandler(key string, handler SubscribeHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subscribeHandlers[key] = handler
}

// SetQuotaManager sets the quota manager
func (h *CommandHandler) SetQuotaManager(qm *QuotaManager) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.quotaManager = qm
}

// SetFeedbackManager sets the feedback manager
func (h *CommandHandler) SetFeedbackManager(fm *FeedbackManager) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.feedbackManager = fm
}

// HandleQuickAction handles a quick action callback
func (h *CommandHandler) HandleQuickAction(callbackData string, userID int64, params map[string]string) *MessageResponse {
	h.mu.RLock()
	handler, exists := h.quickActionHandlers[callbackData]
	h.mu.RUnlock()

	if !exists {
		log.Printf("[CommandHandler] Unknown quick action: %s", callbackData)
		return h.buildErrorResponse("未知操作", "该操作暂不可用，请稍后再试")
	}

	log.Printf("[CommandHandler] Handling quick action: %s for user %d", callbackData, userID)

	// Execute handler with panic recovery
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[CommandHandler] Panic in quick action handler %s: %v", callbackData, r)
		}
	}()

	return handler(userID, params)
}

// Quick action handlers

func (h *CommandHandler) handleQuickSearch(userID int64, params map[string]string) *MessageResponse {
	return &MessageResponse{
		Text: "🔍 *搜索内容*\n\n" +
			"直接输入影片名称即可搜索\n\n" +
			"💡 **搜索技巧**\n" +
			"• 输入中文名：复仇者联盟\n" +
			"• 输入英文名：Avatar\n" +
			"• 输入年份：2024年电影\n" +
			"• 输入类型：科幻剧",
	}
}

func (h *CommandHandler) handleQuickMyRequests(userID int64, params map[string]string) *MessageResponse {
	// This will be implemented to show user's requests
	return &MessageResponse{
		Text: "📋 *我的请求*\n\n" +
			"正在获取您的请求列表...\n\n" +
			"💡 使用 /link 绑定账号后可查看详细信息",
	}
}

func (h *CommandHandler) handleQuickSettings(userID int64, params map[string]string) *MessageResponse {
	quotaText := "未绑定账号"
	if h.quotaManager != nil {
		quota := h.quotaManager.GetUserQuota(userID)
		if quota != nil {
			movieRemaining := quota.MovieLimit - quota.MovieUsed
			tvRemaining := quota.TVLimit - quota.TVUsed
			quotaText = fmt.Sprintf("🎬 电影：%d/%d\n📺 剧集：%d/%d",
				movieRemaining, quota.MovieLimit,
				tvRemaining, quota.TVLimit)
		}
	}

	return &MessageResponse{
		Text: "⚙️ *设置*\n\n" +
			"📊 **今日配额**\n" +
			quotaText + "\n\n" +
			"💡 **其他设置**\n" +
			"/prefs - 通知设置\n" +
			"/link - 绑定账号\n" +
			"/quota - 配额详情",
	}
}

func (h *CommandHandler) handleQuickHelp(userID int64, params map[string]string) *MessageResponse {
	return &MessageResponse{
		Text: "❓ *帮助中心*\n\n" +
			"📱 **常用命令**\n" +
			"/start - 开始使用\n" +
			"/search - 搜索内容\n" +
			"/my - 我的请求\n" +
			"/link - 绑定账号\n" +
			"/help - 显示此帮助\n\n" +
			"💡 点击左下角菜单快速访问所有功能",
	}
}

func (h *CommandHandler) handleTrendingSearch(userID int64, params map[string]string) *MessageResponse {
	// Try to use search handler if available
	h.mu.RLock()
	searchHandler, hasSearch := h.searchHandlers["default"]
	h.mu.RUnlock()

	if !hasSearch {
		return h.buildSearchUnavailableMessage("🔥 热门推荐")
	}

	// Search for trending content (use a generic term that returns popular results)
	result, err := searchHandler(userID, "2024", 0)
	if err != nil {
		log.Printf("[CommandHandler] Trending search error: %v", err)
		return h.buildSearchFallbackMessage("🔥 热门推荐",
			"搜索服务暂时不可用，请尝试手动搜索",
			[]string{"输入 '复仇者联盟' 搜索", "输入 '权力的游戏' 搜索", "输入 '2024' 搜索今年内容"})
	}

	if result == nil || len(result.Items) == 0 {
		return h.buildSearchFallbackMessage("🔥 热门推荐",
			"暂无热门内容推荐",
			[]string{"尝试搜索其他关键词", "查看最新电影", "查看热播剧集"})
	}

	return h.buildSearchResultsMessage("🔥 热门推荐", "2024", result)
}

func (h *CommandHandler) handleHotTVSearch(userID int64, params map[string]string) *MessageResponse {
	h.mu.RLock()
	searchHandler, hasSearch := h.searchHandlers["default"]
	h.mu.RUnlock()

	if !hasSearch {
		return h.buildSearchUnavailableMessage("📺 热播剧集")
	}

	// Search for popular TV shows
	result, err := searchHandler(userID, "2024", 0)
	if err != nil {
		log.Printf("[CommandHandler] Hot TV search error: %v", err)
		return h.buildSearchFallbackMessage("📺 热播剧集",
			"搜索服务暂时不可用",
			[]string{"输入 '繁花' 搜索", "输入 '三体' 搜索", "输入 '狂飙' 搜索"})
	}

	if result == nil || len(result.Items) == 0 {
		return h.buildSearchFallbackMessage("📺 热播剧集",
			"暂无热播剧集",
			[]string{"尝试搜索具体剧名", "查看热门推荐", "搜索其他年份"})
	}

	// Filter for TV shows only
	filteredItems := make([]SearchItem, 0)
	for _, item := range result.Items {
		if item.Type == "tv" {
			filteredItems = append(filteredItems, item)
		}
	}

	if len(filteredItems) == 0 {
		return h.buildSearchFallbackMessage("📺 热播剧集",
			"未找到热播剧集",
			[]string{"尝试搜索具体剧名", "输入 '美剧' 搜索", "输入 '韩剧' 搜索"})
	}

	result.Items = filteredItems
	result.Total = len(filteredItems)

	return h.buildSearchResultsMessage("📺 热播剧集", "2024 剧集", result)
}

func (h *CommandHandler) handleNewMoviesSearch(userID int64, params map[string]string) *MessageResponse {
	h.mu.RLock()
	searchHandler, hasSearch := h.searchHandlers["default"]
	h.mu.RUnlock()

	if !hasSearch {
		return h.buildSearchUnavailableMessage("🎬 最新电影")
	}

	// Search for new movies
	result, err := searchHandler(userID, "2024", 0)
	if err != nil {
		log.Printf("[CommandHandler] New movies search error: %v", err)
		return h.buildSearchFallbackMessage("🎬 最新电影",
			"搜索服务暂时不可用",
			[]string{"输入 '沙丘2' 搜索", "输入 '奥本海默' 搜索", "输入 ' Barbie' 搜索"})
	}

	if result == nil || len(result.Items) == 0 {
		return h.buildSearchFallbackMessage("🎬 最新电影",
			"暂无最新电影",
			[]string{"尝试搜索具体电影名", "查看热门推荐", "搜索其他年份"})
	}

	// Filter for movies only
	filteredItems := make([]SearchItem, 0)
	for _, item := range result.Items {
		if item.Type == "movie" {
			filteredItems = append(filteredItems, item)
		}
	}

	if len(filteredItems) == 0 {
		return h.buildSearchFallbackMessage("🎬 最新电影",
			"未找到最新电影",
			[]string{"尝试搜索具体电影名", "输入 '动作片' 搜索", "搜索其他年份"})
	}

	result.Items = filteredItems
	result.Total = len(filteredItems)

	return h.buildSearchResultsMessage("🎬 最新电影", "2024 电影", result)
}

// Helper methods for building responses

func (h *CommandHandler) buildSearchUnavailableMessage(title string) *MessageResponse {
	return &MessageResponse{
		Text: fmt.Sprintf("%s *暂时不可用*\n\n"+
			"搜索功能正在维护中\n\n"+
			"💡 **替代方案**\n"+
			"• 直接输入内容名搜索\n"+
			"• 稍后再试\n"+
			"• 联系管理员", title),
	}
}

func (h *CommandHandler) buildSearchFallbackMessage(title, message string, suggestions []string) *MessageResponse {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n\n%s\n\n", title, message))

	if len(suggestions) > 0 {
		sb.WriteString("💡 **你可以试试**\n")
		for i, s := range suggestions {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, s))
		}
	}

	return &MessageResponse{
		Text: sb.String(),
	}
}

func (h *CommandHandler) buildSearchResultsMessage(title, query string, result *SearchResult) *MessageResponse {
	var sb strings.Builder

	// Header
	sb.WriteString(fmt.Sprintf("┌─── %s ────┐\n\n", title))
	sb.WriteString(fmt.Sprintf("  关键词: 「%s」\n", query))
	sb.WriteString(fmt.Sprintf("  📄 共 %d 条结果\n\n", result.Total))
	sb.WriteString("  ━━━━━━━━━━━━━━━  \n\n")

	// Results
	for i, item := range result.Items {
		if i >= 8 {
			break
		}

		emoji := "🎬"
		if item.Type == "tv" {
			emoji = "📺"
		}

		sb.WriteString(fmt.Sprintf("  %s %d. %s", emoji, i+1, item.Title))
		if item.Year > 0 {
			sb.WriteString(fmt.Sprintf(" (%d)", item.Year))
		}
		if item.Rating > 0 {
			sb.WriteString(fmt.Sprintf("  ⭐%.1f", item.Rating))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n└──────────────────────┘")
	sb.WriteString(fmt.Sprintf("\n\n💡 输入数字 1-%d 查看详情", len(result.Items)))

	return &MessageResponse{
		Text: sb.String(),
	}
}

func (h *CommandHandler) buildErrorResponse(title, message string) *MessageResponse {
	return &MessageResponse{
		Text: fmt.Sprintf("❌ %s\n\n%s", title, message),
	}
}

// GetQuickActionsKeyboard returns the quick actions keyboard for /start
func (h *CommandHandler) GetQuickActionsKeyboard() [][]map[string]string {
	return [][]map[string]string{
		{{"text": "🔍 搜索内容", "callback_data": "action_search"}},
		{
			{"text": "🔥 热门推荐", "callback_data": "search_trending"},
			{"text": "📺 热播剧集", "callback_data": "search_tv_hot"},
		},
		{
			{"text": "🎬 最新电影", "callback_data": "search_movie_new"},
			{"text": "📋 我的请求", "callback_data": "action_myrequests"},
		},
		{
			{"text": "⚙️ 设置", "callback_data": "action_settings"},
			{"text": "❓ 帮助", "callback_data": "action_help"},
		},
	}
}

// CallbackDataWithPage creates callback data with pagination
func CallbackDataWithPage(action, page string) string {
	return fmt.Sprintf("%s:page=%s", action, page)
}

// ParseCallbackData parses callback data into action and params
func ParseCallbackData(data string) (action string, params map[string]string) {
	params = make(map[string]string)

	parts := strings.Split(data, ":")
	action = parts[0]

	for _, part := range parts[1:] {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			params[kv[0]] = kv[1]
		}
	}

	return
}

// GetWelcomeKeyboard returns the welcome keyboard for new users
func GetWelcomeKeyboard() [][]map[string]string {
	return [][]map[string]string{
		{{"text": "🔍 开始搜索", "callback_data": "action_search"}},
		{{"text": "❓ 查看帮助", "callback_data": "action_help"}},
		{{"text": "🔗 绑定账号", "callback_data": "action_link"}},
	}
}

// Enhanced response builder with error recovery
func (h *CommandHandler) SafeExecute(fn func() *MessageResponse, fallback string) *MessageResponse {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[CommandHandler] Panic recovered: %v", r)
		}
	}()

	result := fn()
	if result == nil {
		return &MessageResponse{Text: fallback}
	}
	return result
}

// FormatMessageWithIcon formats a message with an icon prefix
func FormatMessageWithIcon(icon, message string) string {
	return fmt.Sprintf("%s %s", icon, message)
}

// BuildProgressMessage builds a progress/loading message
func BuildProgressMessage(operation string) string {
	return fmt.Sprintf("⏳ *正在处理*\n\n%s\n\n请稍候...", operation)
}

// BuildSuccessMessage builds a success message
func BuildSuccessMessage(message string) string {
	return fmt.Sprintf("✅ *成功*\n\n%s", message)
}

// BuildErrorMessage builds an error message with suggestions
func BuildErrorMessage(title, message string, suggestions ...string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("❌ *%s*\n\n%s", title, message))

	if len(suggestions) > 0 {
		sb.WriteString("\n\n💡 **建议**\n")
		for i, s := range suggestions {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, s))
		}
	}

	return sb.String()
}

// ParsePositiveInt parses a positive integer from string
func ParsePositiveInt(s string) (int, error) {
	val, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid number: %s", s)
	}
	if val < 0 {
		return 0, fmt.Errorf("negative number not allowed: %d", val)
	}
	return val, nil
}

// ValidateMediaType validates if media type is valid
func ValidateMediaType(mediaType string) bool {
	switch mediaType {
	case "movie", "tv", "both":
		return true
	default:
		return false
	}
}

// SanitizeSearchQuery sanitizes search query
func SanitizeSearchQuery(query string) string {
	// Remove extra whitespace
	query = strings.TrimSpace(query)
	// Limit length
	if len(query) > 100 {
		query = query[:100]
	}
	return query
}
