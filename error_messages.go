package main

import (
	"fmt"
)

// ErrorMessage represents a user-friendly error message
type ErrorMessage struct {
	Title     string
	Message   string
	Suggestion string
	Emoji     string
}

// errorMessages maps error codes to user-friendly messages
var errorMessages = map[string]ErrorMessage{
	// Account binding errors
	"ERR_ALREADY_LINKED": {
		Title:   "已经绑定过了",
		Message: "你已经绑定过 Jellyseerr 账号了",
		Suggestion: "如果想更换账号，请先发送 /unlink 解绑",
		Emoji:   "✅",
	},
	"ERR_INVALID_CREDENTIALS": {
		Title:   "账号或密码错误",
		Message: "无法验证你的 Jellyfin 账号信息",
		Suggestion: "请检查账号和密码是否正确，或尝试使用其他绑定方式",
		Emoji:   "🔐",
	},
	"ERR_VERIFICATION_FAILED": {
		Title:   "验证失败",
		Message: "验证码不正确或已过期",
		Suggestion: "请重新获取验证码后再试",
		Emoji:   "⚠️",
	},
	"ERR_RATE_LIMITED": {
		Title:   "操作太频繁",
		Message: "你的请求过于频繁，请稍后再试",
		Suggestion: "等待一分钟后重新尝试",
		Emoji:   "⏱️",
	},
	"ERR_CODE_EXPIRED": {
		Title:   "验证码已过期",
		Message: "验证码已超过有效期",
		Suggestion: "请重新获取验证码",
		Emoji:   "⏰",
	},

	// Request errors
	"ERR_QUOTA_EXCEEDED": {
		Title:   "配额已用完",
		Message: "你今天的请求配额已经用完了",
		Suggestion: "配额每天 00:00 重置，明天再来吧！\n发送 /quota 查看配额详情",
		Emoji:   "📊",
	},
	"ERR_ALREADY_REQUESTED": {
		Title:   "已经请求过了",
		Message: "你已经请求过这个内容了",
		Suggestion: "可以在 /myrequests 中查看请求状态",
		Emoji:   "📋",
	},
	"ERR_REQUEST_FAILED": {
		Title:   "请求失败",
		Message: "创建请求时遇到问题",
		Suggestion: "请稍后重试，或联系管理员",
		Emoji:   "❌",
	},
	"ERR_MEDIA_NOT_FOUND": {
		Title:   "找不到内容",
		Message: "没有找到匹配的媒体信息",
		Suggestion: "尝试使用不同的关键词搜索",
		Emoji:   "🔍",
	},

	// Search errors
	"ERR_SEARCH_EMPTY": {
		Title:   "搜索内容为空",
		Message: "请输入你想搜索的内容",
		Suggestion: "例如: 复仇者联盟、权力的游戏",
		Emoji:   "🔍",
	},
	"ERR_NO_RESULTS": {
		Title:   "没有找到结果",
		Message: "搜索没有找到匹配的内容",
		Suggestion: "尝试:\n• 检查拼写\n• 使用更简单的关键词\n• 尝试英文原名",
		Emoji:   "🤷",
	},

	// System errors
	"ERR_API_UNAVAILABLE": {
		Title:   "服务暂时不可用",
		Message: "系统正在维护或响应缓慢",
		Suggestion: "请稍后重试，如果问题持续请联系管理员",
		Emoji:   "🔧",
	},
	"ERR_NETWORK_ERROR": {
		Title:   "网络连接失败",
		Message: "无法连接到服务器",
		Suggestion: "请检查网络连接后重试",
		Emoji:   "🌐",
	},
	"ERR_PERMISSION_DENIED": {
		Title:   "权限不足",
		Message: "你没有权限执行此操作",
		Suggestion: "这个功能需要管理员权限",
		Emoji:   "🔒",
	},
	"ERR_NOT_BOUND": {
		Title:   "未绑定账号",
		Message: "你需要先绑定 Jellyseerr 账号",
		Suggestion: "发送 /link 开始绑定，或 /quicklink 快速绑定",
		Emoji:   "🔗",
	},

	// Admin errors
	"ERR_NO_PENDING_REQUESTS": {
		Title:   "没有待处理请求",
		Message: "目前没有需要处理的请求",
		Suggestion: "当有新请求时你会收到通知",
		Emoji:   "✅",
	},
	"ERR_REQUEST_NOT_FOUND": {
		Title:   "请求不存在",
		Message: "找不到指定的请求",
		Suggestion: "请求可能已被处理，请刷新列表",
		Emoji:   "📭",
	},

	// General errors
	"ERR_UNKNOWN": {
		Title:   "遇到问题",
		Message: "系统遇到未知问题",
		Suggestion: "请稍后重试，或联系管理员",
		Emoji:   "❓",
	},
}

// UserFriendlyError converts an error to a user-friendly message
func UserFriendlyError(err error, context ...string) string {
	if err == nil {
		return ""
	}

	errMsg := err.Error()

	// Try to match error message to a known error type
	errorCode := "ERR_UNKNOWN"
	for code := range errorMessages {
		if containsErrorCode(errMsg, code) {
			errorCode = code
			break
		}
	}

	// Get the friendly message
	msg, ok := errorMessages[errorCode]
	if !ok {
		msg = errorMessages["ERR_UNKNOWN"]
	}

	// Format the message
	result := fmt.Sprintf("%s *%s*\n\n", msg.Emoji, msg.Title)
	result += fmt.Sprintf("%s\n\n", msg.Message)

	if msg.Suggestion != "" {
		result += fmt.Sprintf("💡 %s", msg.Suggestion)
	}

	// Add context if provided
	if len(context) > 0 && context[0] != "" {
		result += fmt.Sprintf("\n\n_操作: %s_", context[0])
	}

	return result
}

// containsErrorCode checks if error message contains keywords related to error code
func containsErrorCode(errMsg, code string) bool {
	keywordMap := map[string][]string{
		"ERR_ALREADY_LINKED":      {"already linked", "已绑定", "already bound"},
		"ERR_INVALID_CREDENTIALS": {"invalid credentials", "认证失败", "authentication failed", "401"},
		"ERR_VERIFICATION_FAILED": {"invalid code", "验证码错误", "code expired"},
		"ERR_RATE_LIMITED":        {"rate limit", "过于频繁", "too many requests", "429"},
		"ERR_CODE_EXPIRED":        {"expired", "过期"},
		"ERR_QUOTA_EXCEEDED":      {"quota", "配额", "limit exceeded"},
		"ERR_ALREADY_REQUESTED":   {"already requested", "已请求"},
		"ERR_REQUEST_FAILED":      {"request failed", "请求失败"},
		"ERR_MEDIA_NOT_FOUND":     {"not found", "找不到", "404"},
		"ERR_SEARCH_EMPTY":        {"empty search", "搜索内容为空"},
		"ERR_NO_RESULTS":          {"no results", "没有结果"},
		"ERR_API_UNAVAILABLE":     {"unavailable", "不可用", "503", "502"},
		"ERR_NETWORK_ERROR":       {"network", "连接失败", "connection", "timeout"},
		"ERR_PERMISSION_DENIED":   {"permission", "权限不足", "unauthorized", "403"},
		"ERR_NOT_BOUND":           {"not bound", "未绑定", "not linked"},
	}

	keywords, ok := keywordMap[code]
	if !ok {
		return false
	}

	errLower := toLower(errMsg)
	for _, kw := range keywords {
		if containsErr(errLower, toLower(kw)) {
			return true
		}
	}

	return false
}

// Simple string helpers (use strings package functions)
func toLower(s string) string {
	// Simple lowercase conversion for ASCII
	result := []rune(s)
	for i, r := range result {
		if r >= 'A' && r <= 'Z' {
			result[i] = r + ('a' - 'A')
		}
	}
	return string(result)
}

func containsErr(s, substr string) bool {
	return len(s) >= len(substr) && indexOfErr(s, substr) >= 0
}

func indexOfErr(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// FormatSuccess formats a success message
func FormatSuccess(title, message string, emoji string) string {
	if emoji == "" {
		emoji = "✅"
	}

	result := fmt.Sprintf("%s *%s*\n\n", emoji, title)
	if message != "" {
		result += message
	}
	return result
}

// FormatLoading formats a loading/processing message
func FormatLoading(message string) string {
	return fmt.Sprintf("⏳ *处理中*\n\n%s", message)
}

// FormatInfo formats an informational message
func FormatInfo(title, message string, emoji string) string {
	if emoji == "" {
		emoji = "ℹ️"
	}

	result := fmt.Sprintf("%s *%s*\n\n", emoji, title)
	if message != "" {
		result += message
	}
	return result
}

// FormatWarning formats a warning message
func FormatWarning(title, message string) string {
	return fmt.Sprintf("⚠️ *%s*\n\n%s", title, message)
}

// Common error formatters for specific scenarios

// FormatBindError formats account binding errors
func FormatBindError(err error) string {
	if err == nil {
		return ""
	}

	errMsg := err.Error()

	if containsErr(errMsg, "already") || containsErr(errMsg, "已绑定") {
		return UserFriendlyError(fmt.Errorf("already linked"), "绑定账号")
	}
	if containsErr(errMsg, "credentials") || containsErr(errMsg, "401") {
		return UserFriendlyError(fmt.Errorf("invalid credentials"), "绑定账号")
	}
	if containsErr(errMsg, "rate") || containsErr(errMsg, "频繁") {
		return UserFriendlyError(fmt.Errorf("rate limited"), "绑定账号")
	}

	return UserFriendlyError(err, "绑定账号")
}

// FormatRequestError formats request creation errors
func FormatRequestError(err error) string {
	if err == nil {
		return ""
	}

	errMsg := err.Error()

	if containsErr(errMsg, "quota") || containsErr(errMsg, "配额") {
		return UserFriendlyError(fmt.Errorf("quota exceeded"), "发起请求")
	}
	if containsErr(errMsg, "already") || containsErr(errMsg, "已请求") {
		return UserFriendlyError(fmt.Errorf("already requested"), "发起请求")
	}
	if containsErr(errMsg, "not bound") || containsErr(errMsg, "未绑定") {
		return UserFriendlyError(fmt.Errorf("not bound"), "发起请求")
	}

	return UserFriendlyError(err, "发起请求")
}

// FormatSearchError formats search errors
func FormatSearchError(err error) string {
	if err == nil {
		return ""
	}

	return UserFriendlyError(err, "搜索")
}

// GetQuickActionButtons returns common action buttons for better UX
func GetQuickActionButtons(actions ...string) *TelegramInlineKeyboard {
	keyboard := &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{},
	}

	buttonMap := map[string]map[string]string{
		"search":  {"text": "🔍 搜索", "callback_data": "action_search"},
		"my":      {"text": "📋 我的", "callback_data": "action_myrequests"},
		"help":    {"text": "❓ 帮助", "callback_data": "action_help"},
		"link":    {"text": "🔗 绑定", "callback_data": "action_link"},
		"retry":   {"text": "🔄 重试", "callback_data": "action_retry"},
		"cancel":  {"text": "❌ 取消", "callback_data": "action_cancel"},
		"confirm": {"text": "✅ 确认", "callback_data": "action_confirm"},
		"back":    {"text": "🔙 返回", "callback_data": "action_back"},
		"home":    {"text": "🏠 首页", "callback_data": "action_home"},
	}

	for _, action := range actions {
		if btn, ok := buttonMap[action]; ok {
			keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []map[string]string{btn})
		}
	}

	return keyboard
}

// GetProgressIndicator returns a visual progress indicator
func GetProgressIndicator(current, total int, style string) string {
	if total == 0 {
		return ""
	}

	percent := (current * 100) / total

	switch style {
	case "bar":
		barWidth := 10
		if total == 0 {
			return "[░░░░░░░░░░] 0%"
		}
		filled := (current * barWidth) / total
		bar := ""
		for i := 0; i < barWidth; i++ {
			if i < filled {
				bar += "█"
			} else {
				bar += "░"
			}
		}
		return fmt.Sprintf("%s %d%%", bar, percent)

	case "dots":
		dots := "●●●●●"
		filled := current % 5
		if filled > 5 {
			filled = 5
		}
		return fmt.Sprintf("%s%s %d/%d", dots[:filled], "○○○○○"[filled:], current, total)

	case "percent":
		return fmt.Sprintf("%d%%", percent)

	default:
		return fmt.Sprintf("%d/%d", current, total)
	}
}
