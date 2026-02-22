package main

import (
	"fmt"
)

// MessageLevel defines the detail level of messages
type MessageLevel int

const (
	LevelSimple MessageLevel = iota // Newbie friendly: minimal info
	LevelNormal                     // Balanced: key info + suggestions
	LevelDetail                     // Detailed: complete information
)

// MessageType represents different types of messages
type MessageType string

const (
	MsgWelcome          MessageType = "welcome"
	MsgSearch           MessageType = "search"
	MsgRequestCreated   MessageType = "request_created"
	MsgRequestApproved  MessageType = "request_approved"
	MsgRequestAvailable MessageType = "request_available"
	MsgRequestDeclined  MessageType = "request_declined"
	MsgError            MessageType = "error"
	MsgHelp             MessageType = "help"
	MsgStatus           MessageType = "status"
	MsgAdminPanel       MessageType = "admin_panel"
	MsgNoResults        MessageType = "no_results"
)

// MessageBuilder builds user-friendly messages
type MessageBuilder struct {
	level MessageLevel
}

// NewMessageBuilder creates a new message builder
func NewMessageBuilder(level MessageLevel) *MessageBuilder {
	return &MessageBuilder{level: level}
}

// Build creates a message based on type and data
func (mb *MessageBuilder) Build(msgType MessageType, data map[string]interface{}) string {
	switch msgType {
	case MsgWelcome:
		return mb.buildWelcome(data)
	case MsgSearch:
		return mb.buildSearch(data)
	case MsgRequestCreated:
		return mb.buildRequestCreated(data)
	case MsgRequestApproved:
		return mb.buildRequestApproved(data)
	case MsgRequestAvailable:
		return mb.buildRequestAvailable(data)
	case MsgRequestDeclined:
		return mb.buildRequestDeclined(data)
	case MsgError:
		return mb.buildError(data)
	case MsgHelp:
		return mb.buildHelp(data)
	case MsgStatus:
		return mb.buildStatus(data)
	case MsgAdminPanel:
		return mb.buildAdminPanel(data)
	case MsgNoResults:
		return mb.buildNoResults(data)
	default:
		return mb.buildGeneric(data)
	}
}

// buildWelcome creates a welcome message for new users
func (mb *MessageBuilder) buildWelcome(data map[string]interface{}) string {
	username := ""
	if name, ok := data["username"].(string); ok {
		username = name
	}

	msg := "👋 *欢迎使用云海看板娘！*\n\n"

	if mb.level == LevelSimple {
		msg += "我是你的智能媒体助手，帮你搜索和请求影视内容\n\n"
		msg += "🎬 点击下方按钮快速开始"
	} else {
		if username != "" {
			msg += fmt.Sprintf("你好，%s！\n\n", username)
		}
		msg += "我能帮你：\n"
		msg += "• 🔍 搜索电影和剧集\n"
		msg += "• 📋 创建求片请求\n"
		msg += "• 📊 查看请求状态\n"
		msg += "• ⚙️ 管理通知偏好\n\n"
		msg += "💡 直接输入你想看的内容即可开始搜索！"
	}

	return msg
}

// buildSearch creates a search results message
func (mb *MessageBuilder) buildSearch(data map[string]interface{}) string {
	query := ""
	if q, ok := data["query"].(string); ok {
		query = q
	}

	count := 0
	if c, ok := data["count"].(int); ok {
		count = c
	}

	msg := fmt.Sprintf("🔍 *搜索结果: \"%s\"*\n\n", query)

	if mb.level == LevelSimple {
		if count == 0 {
			msg += "未找到相关内容\n\n"
			msg += "💡 尝试使用更简短的关键词"
		} else {
			msg += fmt.Sprintf("找到 %d 个结果\n\n", count)
			msg += "💡 点击下方按钮快速请求"
		}
	} else {
		if count == 0 {
			msg += "未找到相关内容\n\n"
			msg += "💡 建议：\n"
			msg += "• 使用更简短的关键词\n"
			msg += "• 检查拼写是否正确\n"
			msg += "• 尝试使用英文原名"
		} else {
			msg += fmt.Sprintf("找到 %d 个结果\n\n", count)
		}
	}

	return msg
}

// buildRequestCreated creates a request created message
func (mb *MessageBuilder) buildRequestCreated(data map[string]interface{}) string {
	title := ""
	if t, ok := data["title"].(string); ok {
		title = t
	}

	mediaType := "movie"
	if mt, ok := data["media_type"].(string); ok {
		mediaType = mt
	}

	msg := "✅ *请求已创建*\n\n"

	emoji := "🎬"
	if mediaType == "tv" {
		emoji = "📺"
	}

	msg += fmt.Sprintf("%s %s\n", emoji, title)

	if mb.level == LevelSimple {
		msg += "\n等待管理员批准"
	} else {
		msg += "\n\n📋 预计流程：\n"
		msg += "1. ⏳ 等待管理员审核\n"
		msg += "2. 🎬 批准后自动添加下载\n"
		msg += "3. 🎉 完成后通知你\n\n"
		msg += "💡 可用 /status 查看进度"
	}

	return msg
}

// buildRequestApproved creates a request approved message
func (mb *MessageBuilder) buildRequestApproved(data map[string]interface{}) string {
	title := ""
	if t, ok := data["title"].(string); ok {
		title = t
	}

	msg := "✅ *请求已批准*\n\n"
	msg += fmt.Sprintf("📦 %s\n", title)

	if mb.level == LevelSimple {
		msg += "\n正在准备下载..."
	} else {
		msg += "\n\n🎬 Jellyseerr 正在处理"
		msg += "\n📥 下载完成后会通知你"
	}

	return msg
}

// buildRequestAvailable creates a request available message
func (mb *MessageBuilder) buildRequestAvailable(data map[string]interface{}) string {
	title := ""
	if t, ok := data["title"].(string); ok {
		title = t
	}

	msg := "🎉 *内容已可用！*\n\n"
	msg += fmt.Sprintf("📦 %s\n", title)

	if mb.level == LevelSimple {
		msg += "\n🎬 快去观看吧！"
	} else {
		msg += "\n\n🎬 现在可以在媒体库中观看"
		msg += "\n\n💡 享受观影时光！"
	}

	return msg
}

// buildRequestDeclined creates a request declined message
func (mb *MessageBuilder) buildRequestDeclined(data map[string]interface{}) string {
	title := ""
	if t, ok := data["title"].(string); ok {
		title = t
	}

	reason := ""
	if r, ok := data["reason"].(string); ok {
		reason = r
	}

	msg := "❌ *请求已拒绝*\n\n"
	msg += fmt.Sprintf("📦 %s\n", title)

	if reason != "" {
		msg += fmt.Sprintf("\n📝 原因: %s", reason)
	}

	if mb.level == LevelNormal || mb.level == LevelDetail {
		msg += "\n\n💡 如有疑问，请联系管理员"
	}

	return msg
}

// buildError creates an error message with helpful suggestions
func (mb *MessageBuilder) buildError(data map[string]interface{}) string {
	errorType := "general"
	if et, ok := data["type"].(string); ok {
		errorType = et
	}

	msg := "❌ "

	switch errorType {
	case "search_failed":
		msg += "搜索失败\n\n"
		msg += "💡 请稍后再试或联系管理员"
	case "request_failed":
		msg += "请求创建失败\n\n"
		msg += "💡 请检查网络连接"
	case "not_found":
		msg += "未找到内容\n\n"
		msg += "💡 尝试其他关键词"
	case "unauthorized":
		msg += "权限不足\n\n"
		msg += "💡 请联系管理员获取权限"
	case "rate_limit":
		msg += "请求过于频繁\n\n"
		msg += "💡 请稍等片刻再试"
	default:
		msg += "操作失败\n\n"
		if mb.level == LevelSimple {
			msg += "💡 请稍后再试"
		} else {
			msg += "💡 建议：\n"
			msg += "• 检查网络连接\n"
			msg += "• 稍后再试\n"
			msg += "• 联系管理员"
		}
	}

	return msg
}

// buildHelp creates a help message
func (mb *MessageBuilder) buildHelp(data map[string]interface{}) string {
	msg := "📖 *使用帮助*\n\n"

	if mb.level == LevelSimple {
		msg += "*快速开始：*\n"
		msg += "• 输入电影/剧集名称搜索\n"
		msg += "• 点击按钮快速请求\n\n"
		msg += "*常用命令：*\n"
		msg += "/搜索 <关键词> - 搜索\n"
		msg += "/我的 - 查看我的请求\n"
		msg += "/菜单 - 显示所有功能"
	} else {
		msg += "*🔍 搜索功能：*\n"
		msg += "• 直接输入内容名称\n"
		msg += "• 使用 \"搜索 xxx\" 明确搜索\n"
		msg += "• 添加年份: \"xxx 2024\"\n\n"

		msg += "*📋 请求功能：*\n"
		msg += "• 搜索结果点按钮请求\n"
		msg += "• 查看进度: /我的\n\n"

		msg += "*⚙️ 设置：*\n"
		msg += "/prefs - 通知设置\n"
		msg += "/status - 查看状态\n\n"

		msg += "*👨‍💼 管理员：*\n"
		msg += "/pending - 待处理请求\n"
		msg += "/approve <ID> - 批准\n"
		msg += "/decline <ID> - 拒绝"
	}

	return msg
}

// buildStatus creates a status message
func (mb *MessageBuilder) buildStatus(data map[string]interface{}) string {
	msg := "📊 *我的状态*\n\n"

	pending := 0
	approved := 0
	available := 0
	declined := 0

	if p, ok := data["pending"].(int); ok {
		pending = p
	}
	if a, ok := data["approved"].(int); ok {
		approved = a
	}
	if av, ok := data["available"].(int); ok {
		available = av
	}
	if d, ok := data["declined"].(int); ok {
		declined = d
	}

	msg += fmt.Sprintf("⏳ 待处理: %d\n", pending)
	msg += fmt.Sprintf("✅ 已批准: %d\n", approved)
	msg += fmt.Sprintf("🎉 已可用: %d\n", available)
	msg += fmt.Sprintf("❌ 已拒绝: %d\n", declined)

	total := pending + approved + available + declined
	msg += fmt.Sprintf("\n📊 总计: %d 个请求", total)

	if mb.level == LevelNormal || mb.level == LevelDetail {
		if pending > 0 {
			msg += fmt.Sprintf("\n\n💡 有 %d 个请求等待处理", pending)
		}
		if available > 0 {
			msg += fmt.Sprintf("\n🎬 有 %d 个内容可以观看", available)
		}
	}

	return msg
}

// buildAdminPanel creates an admin panel message
func (mb *MessageBuilder) buildAdminPanel(data map[string]interface{}) string {
	pendingCount := 0
	if pc, ok := data["pending_count"].(int); ok {
		pendingCount = pc
	}

	msg := "👨‍💼 *管理员面板*\n\n"

	if mb.level == LevelSimple {
		msg += fmt.Sprintf("⏳ 待处理: %d 个请求\n", pendingCount)
		msg += "\n点击下方按钮操作"
	} else {
		msg += fmt.Sprintf("⏳ 待处理请求: %d\n", pendingCount)

		todayRequests := 0
		if tr, ok := data["today_requests"].(int); ok {
			todayRequests = tr
		}
		msg += fmt.Sprintf("📊 今日请求: %d\n", todayRequests)

		activeUsers := 0
		if au, ok := data["active_users"].(int); ok {
			activeUsers = au
		}
		msg += fmt.Sprintf("👥 活跃用户: %d\n", activeUsers)
	}

	return msg
}

// buildNoResults creates a no results message
func (mb *MessageBuilder) buildNoResults(data map[string]interface{}) string {
	query := ""
	if q, ok := data["query"].(string); ok {
		query = q
	}

	msg := fmt.Sprintf("🔍 *未找到结果*\n\n")
	msg += fmt.Sprintf("未找到 \"%s\" 相关内容\n\n", query)

	if mb.level == LevelSimple {
		msg += "💡 尝试其他关键词"
	} else {
		msg += "💡 建议：\n"
		msg += "• 使用更简短的关键词\n"
		msg += "• 尝试其他表达方式\n"
		msg += "• 使用英文原名搜索\n"
		msg += "• 检查是否有错别字"
	}

	return msg
}

// buildGeneric creates a generic message
func (mb *MessageBuilder) buildGeneric(data map[string]interface{}) string {
	if text, ok := data["text"].(string); ok {
		return text
	}
	return "操作完成"
}

// Global convenience functions

// GetWelcomeMessage returns a welcome message
func GetWelcomeMessage(username string, level MessageLevel) string {
	mb := NewMessageBuilder(level)
	return mb.Build(MsgWelcome, map[string]interface{}{
		"username": username,
	})
}

// GetSearchMessage returns a search message
func GetSearchMessage(query string, count int, level MessageLevel) string {
	mb := NewMessageBuilder(level)
	return mb.Build(MsgSearch, map[string]interface{}{
		"query": query,
		"count": count,
	})
}

// GetNoResultsMessage returns a no results message
func GetNoResultsMessage(query string, level MessageLevel) string {
	mb := NewMessageBuilder(level)
	return mb.Build(MsgNoResults, map[string]interface{}{
		"query": query,
	})
}

// GetErrorMessage returns an error message
func GetErrorMessage(errorType string, level MessageLevel) string {
	mb := NewMessageBuilder(level)
	return mb.Build(MsgError, map[string]interface{}{
		"type": errorType,
	})
}

// GetStatusMessage returns a status message
func GetStatusMessage(pending, approved, available, declined int, level MessageLevel) string {
	mb := NewMessageBuilder(level)
	return mb.Build(MsgStatus, map[string]interface{}{
		"pending":   pending,
		"approved":  approved,
		"available": available,
		"declined":  declined,
	})
}

// GetHelpMessage returns a help message
func GetHelpMessage(level MessageLevel) string {
	mb := NewMessageBuilder(level)
	return mb.Build(MsgHelp, map[string]interface{}{})
}

// FormatUserMessage formats a message with emoji icons
func FormatUserMessage(icon, title, text string) string {
	msg := fmt.Sprintf("%s *%s*\n\n", icon, title)
	if text != "" {
		msg += text
	}
	return msg
}

// FormatSuccessMessage formats a success message
func FormatSuccessMessage(text string) string {
	return fmt.Sprintf("✅ %s", text)
}

// FormatErrorMessage formats an error message
func FormatErrorMessage(text string) string {
	return fmt.Sprintf("❌ %s", text)
}

// FormatInfoMessage formats an info message
func FormatInfoMessage(text string) string {
	return fmt.Sprintf("ℹ️ %s", text)
}

// FormatWaitingMessage formats a waiting message
func FormatWaitingMessage(text string) string {
	return fmt.Sprintf("⏳ %s", text)
}

// GetUserLevel returns the appropriate message level for a user
func GetUserLevel(userID int64) MessageLevel {
	// For now, always return Normal level
	// TODO: Implement user preference detection
	return LevelNormal
}

// SuggestNextActions suggests next actions based on context
func SuggestNextActions(context string) []string {
	switch context {
	case "search_empty":
		return []string{"尝试其他关键词", "浏览热门内容", "查看帮助"}
	case "search_results":
		return []string{"点击按钮请求", "修改搜索条件", "查看我的请求"}
	case "request_created":
		return []string{"查看请求状态", "继续搜索", "修改通知设置"}
	case "request_available":
		return []string{"去观看", "查看我的请求", "继续搜索"}
	case "no_requests":
		return []string{"搜索内容", "查看帮助", "浏览热门"}
	case "admin_pending":
		return []string{"批准请求", "拒绝请求", "查看详情"}
	default:
		return []string{"发送 /help 查看帮助"}
	}
}

// FormatSuggestions formats action suggestions as buttons
func FormatSuggestions(suggestions []string) *TelegramInlineKeyboard {
	keyboard := &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{},
	}

	// Create buttons (2 per row)
	for i := 0; i < len(suggestions); i += 2 {
		row := []map[string]string{}

		btn1 := map[string]string{
			"text":          suggestions[i],
			"callback_data": fmt.Sprintf("suggest_%d", i),
		}
		row = append(row, btn1)

		if i+1 < len(suggestions) {
			btn2 := map[string]string{
				"text":          suggestions[i+1],
				"callback_data": fmt.Sprintf("suggest_%d", i+1),
			}
			row = append(row, btn2)
		}

		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, row)
	}

	return keyboard
}
