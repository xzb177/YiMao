package main

import (
	"fmt"
	"log"
	"strings"
)

// EnhancedCallbackHandler handles callback queries with enterprise-grade error handling
type EnhancedCallbackHandler struct {
	// Dependencies can be added here
}

// NewEnhancedCallbackHandler creates a new enhanced callback handler
func NewEnhancedCallbackHandler() *EnhancedCallbackHandler {
	return &EnhancedCallbackHandler{}
}

// HandleStartPageButton handles buttons from /start page with fallback logic
func (h *EnhancedCallbackHandler) HandleStartPageButton(userID int64, callbackData string) (string, *TelegramInlineKeyboard, bool) {
	log.Printf("[CallbackHandler] Handling start page button: %s for user %d", callbackData, userID)

	// Parse the callback data
	parts := strings.Split(callbackData, ":")
	action := parts[0]

	switch action {
	case "action_search", "action_myrequests", "action_settings", "action_help":
		return h.handleQuickAction(action, userID)

	case "engage_recommend", "engage_daily":
		return h.handleTrendingSearch(userID)

	case "search_trending":
		return h.handleTrendingSearch(userID)

	case "search_tv_hot":
		return h.handleHotTVSearch(userID)

	case "search_movie_new":
		return h.handleNewMoviesSearch(userID)

	default:
		// Try to use legacy handler
		return "", nil, false
	}
}

// handleQuickAction handles quick action buttons
func (h *EnhancedCallbackHandler) handleQuickAction(action string, userID int64) (string, *TelegramInlineKeyboard, bool) {
	var message string

	switch action {
	case "search_trending":
		return h.handleTrendingSearch(userID)

	case "action_search":
		message = `🔍 *搜索内容*

直接输入影片名称即可搜索

💡 *搜索技巧*
• 输入中文名：复仇者联盟
• 输入英文名：Avatar
• 输入年份：2024年电影
• 输入类型：科幻剧

现在就可以开始搜索！`

	case "action_myrequests":
		message = `📋 *我的请求*

正在获取您的请求列表...

💡 *提示*
使用 /link 绑定账号后可查看详细信息`

		// Try to fetch actual requests
		go handleMyRequestsPrivate(userID)

	case "action_settings":
		quotaText := "未绑定账号"
		if smartSearchMgr != nil {
			quotaInfo := smartSearchMgr.GetUserQuotaInfo(userID)
			if quotaInfo != "" {
				quotaText = quotaInfo
			}
		}

		message = fmt.Sprintf(`⚙️ *设置*

📊 *今日配额*
%s

💡 *其他设置*
/prefs - 通知设置
/link - 绑定账号
/quota - 配额详情`, quotaText)

	case "action_help":
		message = `❓ *帮助中心*

📱 *常用命令*
/start - 开始使用
/search - 搜索内容
/my - 我的请求
/link - 绑定账号
/help - 显示此帮助

💡 点击左下角菜单快速访问所有功能`

	default:
		return "", nil, false
	}

	return message, nil, true
}

// handleTrendingSearch handles trending search with fallback
func (h *EnhancedCallbackHandler) handleTrendingSearch(userID int64) (string, *TelegramInlineKeyboard, bool) {
	// Check if smartSearchMgr is available
	if smartSearchMgr == nil {
		return h.buildSearchFallbackMessage("🔥 热门推荐",
			"搜索功能正在初始化中",
			[]string{
				"直接输入「复仇者联盟」搜索",
				"直接输入「权力的游戏」搜索",
				"直接输入「2024」搜索今年内容",
			}), nil, true
	}

	// Try trending search
	ctx := &SearchContext{
		UserID: userID,
		Query:  "trending",
		Params: &SearchParams{},
	}

	if err := smartSearchMgr.Search(ctx); err != nil {
		log.Printf("[CallbackHandler] Trending search error: %v", err)
		return h.buildSearchFallbackMessage("🔥 热门推荐",
			"搜索服务暂时不可用",
			[]string{
				"直接输入「漫威」搜索",
				"直接输入「三体」搜索",
				"稍后再试热门推荐",
			}), nil, true
	}

	// Format results
	msg, keyboard := FormatSearchResultsWithKeyboard(ctx)
	return "🔥 *热门推荐*\n\n" + msg, keyboard, true
}

// handleHotTVSearch handles hot TV search with fallback
func (h *EnhancedCallbackHandler) handleHotTVSearch(userID int64) (string, *TelegramInlineKeyboard, bool) {
	if smartSearchMgr == nil {
		return h.buildSearchFallbackMessage("📺 热播剧集",
			"搜索功能正在初始化中",
			[]string{
				"直接输入「繁花」搜索",
				"直接输入「三体」搜索",
				"直接输入「狂飙」搜索",
			}), nil, true
	}

	ctx := &SearchContext{
		UserID: userID,
		Query:  "2024",
		Params: &SearchParams{
			MediaType: "tv",
			Year:      "2024",
		},
	}

	if err := smartSearchMgr.Search(ctx); err != nil {
		log.Printf("[CallbackHandler] Hot TV search error: %v", err)
		return h.buildSearchFallbackMessage("📺 热播剧集",
			"搜索服务暂时不可用",
			[]string{
				"直接输入「美剧」搜索",
				"直接输入「韩剧」搜索",
				"直接输入具体剧名搜索",
			}), nil, true
	}

	msg, keyboard := FormatSearchResultsWithKeyboard(ctx)
	return "📺 *热播剧集 (2024)*\n\n" + msg, keyboard, true
}

// handleNewMoviesSearch handles new movies search with fallback
func (h *EnhancedCallbackHandler) handleNewMoviesSearch(userID int64) (string, *TelegramInlineKeyboard, bool) {
	if smartSearchMgr == nil {
		return h.buildSearchFallbackMessage("🎬 最新电影",
			"搜索功能正在初始化中",
			[]string{
				"直接输入「沙丘2」搜索",
				"直接输入「奥本海默」搜索",
				"直接输入「 Barbie」搜索",
			}), nil, true
	}

	ctx := &SearchContext{
		UserID: userID,
		Query:  "2024",
		Params: &SearchParams{
			MediaType: "movie",
			Year:      "2024",
		},
	}

	if err := smartSearchMgr.Search(ctx); err != nil {
		log.Printf("[CallbackHandler] New movies search error: %v", err)
		return h.buildSearchFallbackMessage("🎬 最新电影",
			"搜索服务暂时不可用",
			[]string{
				"直接输入「动作片」搜索",
				"直接输入「科幻片」搜索",
				"直接输入具体电影名搜索",
			}), nil, true
	}

	msg, keyboard := FormatSearchResultsWithKeyboard(ctx)
	return "🎬 *最新电影 (2024)*\n\n" + msg, keyboard, true
}

// buildSearchFallbackMessage builds a user-friendly fallback message when search is unavailable
func (h *EnhancedCallbackHandler) buildSearchFallbackMessage(title, message string, suggestions []string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n\n%s\n\n", title, message))

	if len(suggestions) > 0 {
		sb.WriteString("💡 *你可以试试*\n")
		for i, s := range suggestions {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, s))
		}
	}

	sb.WriteString("\n💡 或者直接输入任何内容名开始搜索")

	return sb.String()
}

// GetImprovedQuickStartKeyboard returns an improved quick start keyboard
func GetImprovedQuickStartKeyboard() *TelegramInlineKeyboard {
	return &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{
			{
				{"text": "🔍 搜索内容", "callback_data": "action_search"},
			},
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
		},
	}
}

// SafeCallbackWrapper wraps callback execution with panic recovery
func SafeCallbackWrapper(action string, userID int64, fn func() (string, *TelegramInlineKeyboard, bool)) (msg string, keyboard *TelegramInlineKeyboard, edit bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[CallbackHandler] Panic recovered in %s: %v", action, r)
			msg = fmt.Sprintf("❌ 操作失败：系统错误\n\n请重新尝试或联系管理员")
			keyboard = nil
			edit = true
		}
	}()

	return fn()
}

// TryDirectSearch attempts a direct Jellyseerr search as fallback
func TryDirectSearch(userID int64, query string) (string, *TelegramInlineKeyboard, bool) {
	if jellyseerrClient == nil {
		return "", nil, false
	}

	results, err := jellyseerrClient.SearchMedia(query)
	if err != nil {
		log.Printf("[CallbackHandler] Direct search failed: %v", err)
		return "", nil, false
	}

	if len(results) == 0 {
		msg := fmt.Sprintf("🔍 *搜索结果*\n\n关键词: 「%s」\n\n未找到相关内容", query)
		return msg, nil, true
	}

	// Format results
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("┌─── 🔍 搜索结果 ────┐\n\n"))
	sb.WriteString(fmt.Sprintf("  关键词: 「%s」\n\n", query))
	sb.WriteString("  ━━━━━━━━━━━━━━━  \n\n")

	for i, item := range results {
		if i >= 8 {
			break
		}

		emoji := "🎬"
		if item.MediaType == "tv" {
			emoji = "📺"
		}

		title := item.Title
		if title == "" {
			title = item.Name
		}

		sb.WriteString(fmt.Sprintf("  %s %d. %s", emoji, i+1, title))
		if item.ReleaseDate != "" && len(item.ReleaseDate) >= 4 {
			sb.WriteString(fmt.Sprintf(" (%s)", item.ReleaseDate[:4]))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n└──────────────────────┘")
	sb.WriteString(fmt.Sprintf("\n\n💡 输入数字 1-%d 查看详情", len(results)))

	return sb.String(), nil, true
}

// IsSearchAvailable checks if search functionality is available
func IsSearchAvailable() bool {
	return smartSearchMgr != nil || jellyseerrClient != nil
}

// GetSearchStatusMessage returns a message about search status
func GetSearchStatusMessage() string {
	if smartSearchMgr != nil {
		return "✅ 搜索功能正常"
	}
	if jellyseerrClient != nil {
		return "✅ 基础搜索可用"
	}
	return "❌ 搜索功能暂不可用"
}
