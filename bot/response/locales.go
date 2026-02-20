package response

import (
	"strings"
)

// Locale represents the language locale
type Locale string

const (
	LocaleZH Locale = "zh" // Chinese (Simplified)
	LocaleEN Locale = "en" // English
)

// LocalizationProvider provides message translations
type LocalizationProvider struct {
	current Locale
	messages map[Locale]map[TemplateType]string
}

// NewLocalizationProvider creates a new localization provider
func NewLocalizationProvider(defaultLocale Locale) *LocalizationProvider {
	p := &LocalizationProvider{
		current:  defaultLocale,
		messages: make(map[Locale]map[TemplateType]string),
	}

	p.loadMessages()
	return p
}

// SetLocale sets the current locale
func (p *LocalizationProvider) SetLocale(locale Locale) {
	p.current = locale
}

// GetLocale returns the current locale
func (p *LocalizationProvider) GetLocale() Locale {
	return p.current
}

// GetTemplate returns the template string for the given type
func (p *LocalizationProvider) GetTemplate(templateType TemplateType) string {
	if msgs, ok := p.messages[p.current]; ok {
		if msg, ok := msgs[templateType]; ok {
			return msg
		}
	}
	// Fallback to English
	if msgs, ok := p.messages[LocaleEN]; ok {
		if msg, ok := msgs[templateType]; ok {
			return msg
		}
	}
	return ""
}

// FormatMessage formats a message with the given data
func (p *LocalizationProvider) FormatMessage(templateType TemplateType, data map[string]interface{}) string {
	template := p.GetTemplate(templateType)
	if template == "" {
		return ""
	}

	result := template
	for key, value := range data {
		placeholder := "{" + key + "}"
		result = strings.ReplaceAll(result, placeholder, toString(value))
	}

	return result
}

// loadMessages loads all message templates
func (p *LocalizationProvider) loadMessages() {
	// Chinese messages
	zhMessages := map[TemplateType]string{
		// Search
		TemplateSearchInProgress: "⏳ 正在搜索 {query}...",
		TemplateSearchNoResults:  "🔍 未找到相关内容\n\n关键词：{query}\n\n💡 建议：\n• 检查拼写是否正确\n• 尝试使用更简单的关键词\n• 尝试使用影片的英文名",
		TemplateSearchError:      "❌ 搜索失败\n\n{error}",

		// Request
		TemplateRequestSuccess:       "✅ 请求已发送！\n\n🎬 {title}\n\n📋 状态：等待管理员处理\n🔔 完成后会自动通知你",
		TemplateRequestPending:       "⏳ 请求处理中\n\n🎬 {title}\n\n管理员正在处理您的请求",
		TemplateRequestApproved:      "✅ 请求已批准\n\n🎬 {title}\n\n正在下载中",
		TemplateRequestAvailable:     "🎉 内容已可用\n\n🎬 {title}\n\n现在可以观看了！",
		TemplateRequestDeclined:      "❌ 请求已拒绝\n\n🎬 {title}\n\n如有疑问，请联系管理员",
		TemplateRequestQuotaExhausted: "🚫 今日{type}配额已用完\n\n今日已请求 {used} 部{type}，达到每日限额 {limit} 部\n\n💡 明天配额会自动重置",
		TemplateRequestError:         "❌ 请求失败\n\n{error}",

		// Account
		TemplateAccountLinked:    "✅ 账号已绑定\n\n欢迎，{username}！",
		TemplateAccountLinkError: "❌ 绑定失败\n\n{error}",
		TemplateAccountNotLinked: "⚠️ 需要绑定账号\n\n使用此功能前需要先绑定您的 Jellyfin 账号\n\n使用 /link 账号 密码 命令绑定",

		// System
		TemplateSystemError:      "🚨 系统错误\n\n{error}",
		TemplateNetworkError:     "🌐 网络错误\n\n无法连接到服务器",
		TemplateRateLimited:      "⏱️ 操作太频繁\n\n您的操作过于频繁，请稍后再试",
		TemplateOperationTimeout: "⏰ 操作超时\n\n操作耗时过长，已自动取消",
		TemplateInvalidInput:     "❌ 输入无效\n\n{error}",

		// Rating
		TemplateRatingSuccess: "✅ 评分成功\n\n⭐ 你的评分: {rating}/10",
		TemplateRatingUpdated: "✅ 评分已更新",

		// Progress
		TemplateProgressProcessing: "⏳ 处理中\n\n{step}",
		TemplateProgressCompleted:  "✅ 操作完成",
		TemplateProgressFailed:     "❌ 操作失败\n\n{error}",
	}

	// English messages
	enMessages := map[TemplateType]string{
		// Search
		TemplateSearchInProgress: "⏳ Searching {query}...",
		TemplateSearchNoResults:  "🔍 No results found\n\nKeyword: {query}\n\n💡 Suggestions:\n• Check your spelling\n• Try simpler keywords\n• Try the English title",
		TemplateSearchError:      "❌ Search failed\n\n{error}",

		// Request
		TemplateRequestSuccess:       "✅ Request sent!\n\n🎬 {title}\n\n📋 Status: Pending approval\n🔔 You'll be notified when ready",
		TemplateRequestPending:       "⏳ Request processing\n\n🎬 {title}\n\nYour request is being processed",
		TemplateRequestApproved:      "✅ Request approved\n\n🎬 {title}\n\nDownloading now",
		TemplateRequestAvailable:     "🎉 Now available\n\n🎬 {title}\n\nReady to watch!",
		TemplateRequestDeclined:      "❌ Request declined\n\n🎬 {title}\n\nContact admin for questions",
		TemplateRequestQuotaExhausted: "🚫 Daily {type} quota exhausted\n\n{used} of {limit} requests used today\n\n💡 Quota resets tomorrow",
		TemplateRequestError:         "❌ Request failed\n\n{error}",

		// Account
		TemplateAccountLinked:    "✅ Account linked\n\nWelcome, {username}!",
		TemplateAccountLinkError: "❌ Link failed\n\n{error}",
		TemplateAccountNotLinked: "⚠️ Account required\n\nPlease link your Jellyfin account first\n\nUse /link username password",

		// System
		TemplateSystemError:      "🚨 System error\n\n{error}",
		TemplateNetworkError:     "🌐 Network error\n\nCannot connect to server",
		TemplateRateLimited:      "⏱️ Too many requests\n\nPlease slow down and try again later",
		TemplateOperationTimeout: "⏰ Operation timeout\n\nOperation took too long",
		TemplateInvalidInput:     "❌ Invalid input\n\n{error}",

		// Rating
		TemplateRatingSuccess: "✅ Rating saved\n\n⭐ Your rating: {rating}/10",
		TemplateRatingUpdated: "✅ Rating updated",

		// Progress
		TemplateProgressProcessing: "⏳ Processing\n\n{step}",
		TemplateProgressCompleted:  "✅ Operation completed",
		TemplateProgressFailed:     "❌ Operation failed\n\n{error}",
	}

	p.messages[LocaleZH] = zhMessages
	p.messages[LocaleEN] = enMessages
}

// toString converts any value to string
func toString(value interface{}) string {
	if value == nil {
		return ""
	}

	switch v := value.(type) {
	case string:
		return v
	case int:
		return formatInt(v)
	case int64:
		return formatInt64(v)
	case float64:
		return formatFloat(v)
	case bool:
		return formatBool(v)
	default:
		return ""
	}
}

func formatInt(v int) string {
	// Simple int to string conversion
	if v == 0 {
		return "0"
	}
	negative := v < 0
	if negative {
		v = -v
	}
	var digits []byte
	for v > 0 {
		digits = append(digits, byte('0'+v%10))
		v /= 10
	}
	if negative {
		digits = append(digits, '-')
	}
	// Reverse
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}

func formatInt64(v int64) string {
	return formatInt(int(v))
}

func formatFloat(v float64) string {
	// Simple float to string conversion (no formatting)
	return formatInt64(int64(v))
}

func formatBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
