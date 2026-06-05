package ui

import (
	"fmt"
	"github.com/xzb177/yimao/internal/services"
	"strings"
)

// UI 风格类型
type UIStyle string

const (
	StyleNeon   UIStyle = "neon"   // 暗黑霓虹风
	StyleFilm   UIStyle = "film"   // 文艺胶片风
	StylePop    UIStyle = "pop"    // 波普艺术风
	StyleCard   UIStyle = "card"   // 极简卡片风
	StyleCinema UIStyle = "cinema" // 沉浸电影风
)

// MessageBuilder 消息构建器接口
type MessageBuilder interface {
	BuildMenu(title, subtitle string) string
	BuildSearchResults(query string, results []services.SearchResult, page, total int) string
	BuildMediaDetail(result *services.SearchResult) string
	BuildRecommendation(title string, results []services.SearchResult, mood string) string
	BuildRequestList(requests []services.SubscribeItem, page, totalPages, total int) string
}

// NewBuilder 根据风格类型创建对应的构建器
func NewBuilder(style UIStyle) MessageBuilder {
	switch style {
	case StyleNeon:
		return &NeonBuilder{}
	case StyleFilm:
		return &FilmBuilder{}
	case StylePop:
		return &PopBuilder{}
	case StyleCard:
		return &DashBuilder{}
	case StyleCinema:
		return &CinemaBuilder{}
	default:
		return &NeonBuilder{} // 默认使用暗黑霓虹风
	}
}

// BuildMenu 构建主菜单（使用极简卡片风格）
func BuildMenu(title, subtitle string) string {
	builder := NewBuilder(StyleCard)
	return builder.BuildMenu(title, subtitle)
}

// BuildSearchResults 构建搜索结果（使用极简卡片风格）
func BuildSearchResults(query string, results []services.SearchResult, page, total int) string {
	builder := NewBuilder(StyleCard)
	return builder.BuildSearchResults(query, results, page, total)
}

// BuildMediaDetail 构建媒体详情（使用极简卡片风格）
func BuildMediaDetail(result *services.SearchResult) string {
	builder := NewBuilder(StyleCard)
	return builder.BuildMediaDetail(result)
}

// BuildRecommendation 构建推荐内容（使用极简卡片风格）
func BuildRecommendation(title string, results []services.SearchResult, mood string) string {
	builder := NewBuilder(StyleCard)
	return builder.BuildRecommendation(title, results, mood)
}

// BuildRequestList 构建请求列表（使用极简卡片风格）
func BuildRequestList(requests []services.SubscribeItem, page, totalPages, total int) string {
	builder := NewBuilder(StyleCard)
	return builder.BuildRequestList(requests, page, totalPages, total)
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

// 辅助函数：获取评分显示
func getRatingDisplay(rating float64) string {
	if rating > 0 {
		return strings.Repeat("★", int(rating/2)) + strings.Repeat("☆", 5-int(rating/2)) + fmt.Sprintf(" %.1f", rating)
	}
	return "暂无评分"
}

// 辅助函数：构建进度条
func buildProgressBar(current, total int, length int) string {
	if total <= 0 {
		return strings.Repeat("░", length)
	}
	filled := float64(current) / float64(total) * float64(length)
	filledLen := int(filled)
	if filledLen > length {
		filledLen = length
	}
	return strings.Repeat("█", filledLen) + strings.Repeat("░", length-filledLen)
}
