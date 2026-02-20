package response

import (
	"fmt"
	"strings"
	"time"
)

// TemplateType represents different message templates
type TemplateType string

const (
	// Search templates
	TemplateSearchInProgress TemplateType = "search_in_progress"
	TemplateSearchNoResults  TemplateType = "search_no_results"
	TemplateSearchError      TemplateType = "search_error"

	// Request/Subscribe templates
	TemplateRequestSuccess      TemplateType = "request_success"
	TemplateRequestPending      TemplateType = "request_pending"
	TemplateRequestApproved     TemplateType = "request_approved"
	TemplateRequestAvailable    TemplateType = "request_available"
	TemplateRequestDeclined     TemplateType = "request_declined"
	TemplateRequestError        TemplateType = "request_error"
	TemplateRequestQuotaExhausted TemplateType = "request_quota_exhausted"

	// Account templates
	TemplateAccountLinked      TemplateType = "account_linked"
	TemplateAccountLinkError   TemplateType = "account_link_error"
	TemplateAccountNotLinked   TemplateType = "account_not_linked"

	// System templates
	TemplateSystemError        TemplateType = "system_error"
	TemplateNetworkError       TemplateType = "network_error"
	TemplateRateLimited        TemplateType = "rate_limited"
	TemplateOperationTimeout   TemplateType = "operation_timeout"
	TemplateInvalidInput       TemplateType = "invalid_input"

	// Rating templates
	TemplateRatingSuccess      TemplateType = "rating_success"
	TemplateRatingUpdated      TemplateType = "rating_updated"

	// Progress templates
	TemplateProgressProcessing  TemplateType = "progress_processing"
	TemplateProgressCompleted   TemplateType = "progress_completed"
	TemplateProgressFailed      TemplateType = "progress_failed"
)

// TemplateData contains data for template rendering
type TemplateData struct {
	Title       string
	MediaTitle  string
	MediaType   string
	MediaID     string
	QuotaUsed   int
	QuotaLimit  int
	QuotaRemaining int
	QuotaType   string
	Error       string
	Suggestions []string
	EstimateTime time.Duration
	RetryAfter  time.Duration
	CurrentStep string
	TotalSteps  int
	Progress    int
	RequestID   string
	UserName    string
	UserID      int64
	AdminName   string
	Seasons     []int
	Episodes    int
}

// RenderTemplate renders a template with the given data
func RenderTemplate(templateType TemplateType, data TemplateData) *Response {
	builder := NewBuilder()

	switch templateType {
	case TemplateSearchInProgress:
		return builder.
			WithType(ResponseTypeLoading).
			WithTitle("🔍 搜索中").
			WithMessage(fmt.Sprintf("正在搜索「%s」...", data.MediaTitle)).
			WithDetails("请稍候，这可能需要几秒钟").
			Build()

	case TemplateSearchNoResults:
		suggestions := []string{
			"检查拼写是否正确",
			"尝试使用更简单的关键词",
			"尝试使用影片的英文名",
		}
		return builder.
			WithType(ResponseTypeInfo).
			WithTitle("🔍 未找到相关内容").
			WithMessage(fmt.Sprintf("关键词：%s", data.MediaTitle)).
			WithSuggestions(suggestions...).
			Build()

	case TemplateSearchError:
		return builder.
			WithType(ResponseTypeError).
			WithSeverity(SeverityMedium).
			WithTitle("❌ 搜索失败").
			WithMessage("无法完成搜索操作").
			WithDetails(data.Error).
			WithSuggestions("请稍后再试", "如果问题持续，请联系管理员").
			Build()

	case TemplateRequestSuccess:
		var msg strings.Builder
		msg.WriteString(fmt.Sprintf("✅ 请求已发送！\n\n🎬 %s\n\n", data.MediaTitle))
		msg.WriteString("📋 状态：等待管理员处理\n")
		msg.WriteString("🔔 完成后会自动通知你")

		details := ""
		if data.QuotaRemaining > 0 {
			details = fmt.Sprintf("💡 今日还可请求 %d 部%s", data.QuotaRemaining, data.QuotaType)
		} else if data.QuotaRemaining == 0 && data.QuotaLimit > 0 {
			details = "🎊 今日配额已用完！明天会自动重置"
		}

		return builder.
			WithType(ResponseTypeSuccess).
			WithTitle("请求已提交").
			WithMessage(msg.String()).
			WithDetails(details).
			WithAlert(true).
			Build()

	case TemplateRequestPending:
		return builder.
			WithType(ResponseTypeInfo).
			WithTitle("⏳ 请求处理中").
			WithMessage(fmt.Sprintf("🎬 %s", data.MediaTitle)).
			WithDetails("管理员正在处理您的请求，请耐心等待").
			Build()

	case TemplateRequestApproved:
		return builder.
			WithType(ResponseTypeSuccess).
			WithTitle("✅ 请求已批准").
			WithMessage(fmt.Sprintf("🎬 %s", data.MediaTitle)).
			WithDetails("正在下载中，完成后会自动通知您").
			Build()

	case TemplateRequestAvailable:
		return builder.
			WithType(ResponseTypeSuccess).
			WithTitle("🎉 内容已可用").
			WithMessage(fmt.Sprintf("🎬 %s", data.MediaTitle)).
			WithDetails("现在可以观看了！去媒体库搜索一下吧").
			Build()

	case TemplateRequestDeclined:
		return builder.
			WithType(ResponseTypeWarning).
			WithTitle("❌ 请求已拒绝").
			WithMessage(fmt.Sprintf("🎬 %s", data.MediaTitle)).
			WithDetails("如有疑问，请联系管理员").
			Build()

	case TemplateRequestQuotaExhausted:
		quotaType := "电影"
		if data.QuotaType == "tv" {
			quotaType = "剧集"
		}
		message := fmt.Sprintf("🚫 今日%s配额已用完\n\n今日已请求 %d 部%s，达到每日限额 %d 部",
			quotaType, data.QuotaUsed, quotaType, data.QuotaLimit)
		return builder.
			WithType(ResponseTypeError).
			WithSeverity(SeverityMedium).
			WithTitle("配额已达上限").
			WithMessage(message).
			WithDetails("💡 明天配额会自动重置，请明天再试").
			WithAlert(true).
			Build()

	case TemplateRequestError:
		// Check if it's an account linking issue
		if strings.Contains(data.Error, "绑定") || strings.Contains(data.Error, "link") {
			return RenderTemplate(TemplateAccountNotLinked, data)
		}

		return builder.
			WithType(ResponseTypeError).
			WithSeverity(SeverityHigh).
			WithTitle("❌ 请求失败").
			WithMessage("无法完成您的请求").
			WithDetails(data.Error).
			WithSuggestions("请稍后再试", "如果问题持续，请联系管理员").
			WithAlert(true).
			Build()

	case TemplateAccountLinked:
		return builder.
			WithType(ResponseTypeSuccess).
			WithTitle("✅ 账号已绑定").
			WithMessage(fmt.Sprintf("欢迎，%s！", data.UserName)).
			WithDetails("您现在可以发起请求了").
			Build()

	case TemplateAccountLinkError:
		return builder.
			WithType(ResponseTypeError).
			WithSeverity(SeverityMedium).
			WithTitle("❌ 绑定失败").
			WithMessage("无法绑定您的账号").
			WithDetails(data.Error).
			WithSuggestions("请检查账号密码是否正确", "确保账号在 Jellyfin 中存在").
			Build()

	case TemplateAccountNotLinked:
		return builder.
			WithType(ResponseTypeWarning).
			WithTitle("⚠️ 需要绑定账号").
			WithMessage("使用此功能前需要先绑定您的 Jellyfin 账号").
			WithDetails("使用 /link 账号 密码 命令绑定").
			WithAction("link_now", "🔗 立即绑定", "link").
			Build()

	case TemplateSystemError:
		return builder.
			WithType(ResponseTypeError).
			WithSeverity(SeverityCritical).
			WithTitle("🚨 系统错误").
			WithMessage("服务暂时不可用").
			WithDetails(data.Error).
			WithSuggestions("我们已收到此错误", "请稍后再试", "如需帮助，请联系管理员").
			WithRequestID(data.RequestID).
			Build()

	case TemplateNetworkError:
		return builder.
			WithType(ResponseTypeError).
			WithSeverity(SeverityHigh).
			WithTitle("🌐 网络错误").
			WithMessage("无法连接到服务器").
			WithDetails("请检查您的网络连接").
			WithSuggestions("稍后重试", "如果问题持续，可能是服务暂时不可用").
			Build()

	case TemplateRateLimited:
		retryText := ""
		if data.RetryAfter > 0 {
			if data.RetryAfter < time.Minute {
				retryText = fmt.Sprintf("请在 %d 秒后重试", int(data.RetryAfter.Seconds()))
			} else {
				retryText = fmt.Sprintf("请在 %d 分钟后重试", int(data.RetryAfter.Minutes()))
			}
		}
		return builder.
			WithType(ResponseTypeWarning).
			WithTitle("⏱️ 操作太频繁").
			WithMessage("您的操作过于频繁，请稍后再试").
			WithDetails(retryText).
			Build()

	case TemplateOperationTimeout:
		return builder.
			WithType(ResponseTypeError).
			WithSeverity(SeverityMedium).
			WithTitle("⏰ 操作超时").
			WithMessage("操作耗时过长，已自动取消").
			WithDetails("这可能是由于网络延迟或服务器负载过高").
			WithSuggestions("请稍后重试", "检查网络连接").
			Build()

	case TemplateInvalidInput:
		return builder.
			WithType(ResponseTypeError).
			WithSeverity(SeverityLow).
			WithTitle("❌ 输入无效").
			WithMessage(data.Error).
			WithDetails("请检查您的输入格式").
			Build()

	case TemplateRatingSuccess:
		return builder.
			WithType(ResponseTypeSuccess).
			WithTitle("✅ 评分成功").
			WithMessage(fmt.Sprintf("⭐ 您的评分: %.1f/10", float64(data.QuotaUsed))). // Reuse QuotaUsed as rating
			WithDetails(fmt.Sprintf("📊 平均评分: %.1f/10 (%d人评分)", float64(data.QuotaLimit), data.QuotaRemaining)). // Reuse fields
			WithAlert(true).
			Build()

	case TemplateRatingUpdated:
		return builder.
			WithType(ResponseTypeSuccess).
			WithTitle("✅ 评分已更新").
			WithMessage(fmt.Sprintf("⭐ 新评分: %.1f/10", float64(data.QuotaUsed))).
			WithDetails("您的评分已成功更新").
			WithAlert(true).
			Build()

	case TemplateProgressProcessing:
		message := data.CurrentStep
		if data.TotalSteps > 0 {
			message = fmt.Sprintf("[%d/%d] %s", data.Progress, data.TotalSteps, data.CurrentStep)
		}
		return builder.
			WithType(ResponseTypeProgress).
			WithTitle("⏳ 处理中").
			WithMessage(message).
			WithDismissable(false).
			Build()

	case TemplateProgressCompleted:
		return builder.
			WithType(ResponseTypeSuccess).
			WithTitle("✅ 操作完成").
			WithMessage(data.MediaTitle).
			Build()

	case TemplateProgressFailed:
		return builder.
			WithType(ResponseTypeError).
			WithSeverity(SeverityHigh).
			WithTitle("❌ 操作失败").
			WithMessage(data.MediaTitle).
			WithDetails(data.Error).
			Build()

	default:
		return builder.
			WithType(ResponseTypeInfo).
			WithMessage("未知消息类型").
			Build()
	}
}

// Helper methods for common responses

// Success creates a simple success response
func Success(message string) *Response {
	return NewBuilder().
		WithType(ResponseTypeSuccess).
		WithMessage(message).
		Build()
}

// SuccessWithTitle creates a success response with title
func SuccessWithTitle(title, message string) *Response {
	return NewBuilder().
		WithType(ResponseTypeSuccess).
		WithTitle(title).
		WithMessage(message).
		Build()
}

// Error creates a simple error response
func Error(message string) *Response {
	return NewBuilder().
		WithType(ResponseTypeError).
		WithSeverity(SeverityMedium).
		WithMessage(message).
		Build()
}

// ErrorWithDetails creates an error response with details
func ErrorWithDetails(title, message, details string) *Response {
	return NewBuilder().
		WithType(ResponseTypeError).
		WithSeverity(SeverityMedium).
		WithTitle(title).
		WithMessage(message).
		WithDetails(details).
		Build()
}

// Info creates an info response
func Info(message string) *Response {
	return NewBuilder().
		WithType(ResponseTypeInfo).
		WithMessage(message).
		Build()
}

// Loading creates a loading response
func Loading(message string) *Response {
	return NewBuilder().
		WithType(ResponseTypeLoading).
		WithMessage(message).
		WithDismissable(false).
		Build()
}

// Warning creates a warning response
func Warning(message string) *Response {
	return NewBuilder().
		WithType(ResponseTypeWarning).
		WithMessage(message).
		Build()
}

// Progress creates a progress response
func Progress(current, total int, message string) *Response {
	return NewBuilder().
		WithType(ResponseTypeProgress).
		WithMessage(fmt.Sprintf("[%d/%d] %s", current, total, message)).
		WithDismissable(false).
		Build()
}
