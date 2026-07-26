package ui

import (
	"html"
	"strings"

	"github.com/xzb177/yimao/internal/services"
)

// escapeText 转义 HTML 特殊字符（< > & "），
// 防止外部文本（Overview/Title）里的 < > 破坏 Telegram HTML 解析。
func escapeText(text string) string {
	return html.EscapeString(text)
}

// truncateAndEscape 截断 + 转义，用于外部文本的安全展示。
func truncateAndEscape(text string, maxLen int) string {
	return escapeText(truncateText(text, maxLen))
}

// UI 风格类型。
// 历史上曾有 neon/film/pop/cinema 多套主题，均从未在生产路径启用，
// 已于 2026-07 清理；现仅保留唯一的 StyleCard。类型与常量保留，
// 以兼容配置层可能残留的字符串值（未知值一律回退 StyleCard）。
type UIStyle string

const (
	StyleCard UIStyle = "card" // 极简卡片风（唯一生效主题）
)

// MessageBuilder 消息构建器接口
type MessageBuilder interface {
	BuildMenu(title, subtitle string) string
	BuildSearchResults(query string, results []services.SearchResult, page, total int) string
	BuildMediaDetail(result *services.SearchResult) string
	BuildRecommendation(title string, results []services.SearchResult, mood string) string
	BuildRequestList(requests []services.SubscribeItem, page, totalPages, total int) string
}

const (
	emptySearchCopy  = "没搜到相关内容。试试简称、英文名或年份；还没有可加入许愿。"
	emptyRequestCopy = "还没有请求。搜索影片后点击「求片」即可添加。"
)

func getRequestStatusLabel(state string) string {
	switch strings.ToLower(state) {
	case "p", "pending", "wish", "reviewing":
		return "待审核"
	case "r", "s", "recycled", "searching", "stuck":
		return "正在找资源"
	case "d", "downloading":
		return "下载中"
	case "c", "completed":
		return "已入库"
	case "failed", "f", "error":
		return "处理失败"
	case "cancelled", "canceled", "cancel": //nolint:misspell // cancelled is a persisted legacy state
		return "已取消"
	default:
		return "处理失败"
	}
}

// NewBuilder 根据风格类型创建对应的构建器。
// 未知/历史遗留风格值一律回退到唯一的卡片主题。
func NewBuilder(_ UIStyle) MessageBuilder {
	return &DashBuilder{}
}

// BuildMenuWith 构建主菜单（指定风格）
func BuildMenuWith(style UIStyle, title, subtitle string) string {
	return NewBuilder(style).BuildMenu(title, subtitle)
}

// BuildSearchResultsWith 构建搜索结果（指定风格）
func BuildSearchResultsWith(style UIStyle, query string, results []services.SearchResult, page, total int) string {
	return NewBuilder(style).BuildSearchResults(query, results, page, total)
}

// BuildMediaDetailWith 构建媒体详情（指定风格）
func BuildMediaDetailWith(style UIStyle, result *services.SearchResult) string {
	return NewBuilder(style).BuildMediaDetail(result)
}

// BuildRecommendationWith 构建推荐内容（指定风格）
func BuildRecommendationWith(style UIStyle, title string, results []services.SearchResult, mood string) string {
	return NewBuilder(style).BuildRecommendation(title, results, mood)
}

// BuildRequestListWith 构建请求列表（指定风格）
func BuildRequestListWith(style UIStyle, requests []services.SubscribeItem, page, totalPages, total int) string {
	return NewBuilder(style).BuildRequestList(requests, page, totalPages, total)
}

// 辅助函数：截断文本
func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen]) + "..."
}

// 辅助函数：获取媒体类型图标
func getMediaTypeIcon(mediaType string) string {
	switch strings.ToLower(mediaType) {
	case "movie", "mov", "电影":
		return "🎬"
	case "tv", "电视剧", "剧集":
		return "📺"
	default:
		return "🎬"
	}
}

// 辅助函数：获取媒体类型标签
func getMediaTypeLabel(mediaType string) string {
	switch strings.ToLower(mediaType) {
	case "movie", "mov", "电影":
		return "电影"
	case "tv", "电视剧", "剧集":
		return "剧集"
	default:
		return mediaType
	}
}

// 辅助函数：获取请求状态图标
func getRequestStatusEmoji(state string) string {
	switch state {
	case "pending":
		return "⏳"
	case "recycled":
		return "🔄"
	case "searching":
		return "🔍"
	case "downloading":
		return "⬇️"
	case "completed":
		return "✅"
	case "failed":
		return "❌"
	case "cancelled":
		return "🚫"
	default:
		return "❓"
	}
}

// 辅助函数：文本换行
func wrapText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}

	runes := []rune(text)
	var result []string
	current := strings.Builder{}

	for _, r := range runes {
		if current.Len()+1 > maxLen {
			result = append(result, current.String())
			current.Reset()
		}
		current.WriteRune(r)
	}

	if current.Len() > 0 {
		result = append(result, current.String())
	}

	return strings.Join(result, "\n   ")
}
