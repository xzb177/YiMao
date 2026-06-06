package ui

import (
	"fmt"
	"strings"

	"github.com/xzb177/yimao/internal/services"
)

// NeonBuilder 暗黑霓虹风格构建器
type NeonBuilder struct{}

const (
	NeonSeparator = "▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰"
	NeonLine      = "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	NeonPrimary   = "00f5d4" // 霓虹青
	NeonSecondary = "ff006e" // 霓虹粉
	NeonAccent    = "8338ec" // 霓虹紫

	// 保留小写别名用于内部使用
	neonSeparator = NeonSeparator
	neonLine      = NeonLine
	neonPrimary   = NeonPrimary
	neonSecondary = NeonSecondary
	neonAccent    = NeonAccent
)

// BuildMenu 构建主菜单
func (b *NeonBuilder) BuildMenu(title, subtitle string) string {
	var sb strings.Builder

	sb.WriteString(neonLine + "\n")
	sb.WriteString(fmt.Sprintf("%s %s %s\n", neonPrimary, title, neonPrimary))
	sb.WriteString(neonLine + "\n")
	sb.WriteString(fmt.Sprintf("✦ %s\n\n", subtitle))

	return sb.String()
}

// BuildSearchResults 构建搜索结果
func (b *NeonBuilder) BuildSearchResults(query string, results []services.SearchResult, page, total int) string {
	var sb strings.Builder

	sb.WriteString(neonLine + "\n")
	sb.WriteString(fmt.Sprintf("🔮 搜索结果 · %s\n", query))
	sb.WriteString(neonLine + "\n\n")

	if len(results) == 0 {
		sb.WriteString("😕 未找到相关内容\n\n")
		sb.WriteString("💡 建议：\n")
		sb.WriteString("• 检查拼写是否正确\n")
		sb.WriteString("• 尝试使用更简短的关键词\n")
		sb.WriteString("• 尝试使用英文搜索\n")
		return sb.String()
	}

	// 显示结果数量信息
	sb.WriteString(fmt.Sprintf("📊 找到 %d 个结果", total))
	if total > len(results) {
		sb.WriteString(fmt.Sprintf(" (第 %d 页)", page))
	}
	sb.WriteString("\n\n")
	sb.WriteString(neonSeparator + "\n\n")

	// 显示结果列表
	for i, item := range results {
		icon := getMediaTypeIcon(item.Type)
		year := ""
		if item.Year > 0 {
			year = fmt.Sprintf(" (%d)", item.Year)
		}
		rating := ""
		if item.Rating > 0 {
			rating = fmt.Sprintf(" ⏺️ %.1f", item.Rating)
		}

		sb.WriteString(fmt.Sprintf("▸ %d. %s%s%s%s\n", i+1, escapeText(item.Title), year, icon, rating))

		// 如果有概要，显示简短描述
		if item.Overview != "" {
			overview := truncateAndEscape(item.Overview, 50)
			sb.WriteString(fmt.Sprintf("   %s\n", overview))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(neonSeparator + "\n")
	sb.WriteString("输入数字选择 · 或翻页 ↓\n")

	return sb.String()
}

// BuildMediaDetail 构建媒体详情
func (b *NeonBuilder) BuildMediaDetail(result *services.SearchResult) string {
	var sb strings.Builder

	sb.WriteString(neonLine + "\n")

	// 标题
	sb.WriteString(fmt.Sprintf("🎬 %s", escapeText(result.Title)))
	if int(result.Year) > 0 {
		sb.WriteString(fmt.Sprintf(" (%d)", result.Year))
	}
	sb.WriteString("\n")

	sb.WriteString(neonLine + "\n\n")

	// 元信息
	if result.Rating > 0 {
		sb.WriteString(fmt.Sprintf("⭐ 评分 %.1f  ·  🎬 %s\n",
			result.Rating, getMediaTypeLabel(result.Type)))
	} else {
		sb.WriteString(fmt.Sprintf("🎬 %s\n", getMediaTypeLabel(result.Type)))
	}

	sb.WriteString("\n")
	sb.WriteString(neonLine + "\n")

	// 概要
	if result.Overview != "" {
		sb.WriteString(fmt.Sprintf("  %s\n", formatOverview(escapeText(result.Overview))))
	}

	sb.WriteString(neonLine + "\n")

	// TMDB ID
	sb.WriteString(fmt.Sprintf("🆔 TMDB ID: %d\n", result.ID))

	return sb.String()
}

// BuildRecommendation 构建推荐内容（暗黑霓虹风格）
func (b *NeonBuilder) BuildRecommendation(title string, results []services.SearchResult, mood string) string {
	var sb strings.Builder

	sb.WriteString(neonLine + "\n")
	sb.WriteString(fmt.Sprintf("⚡ %s\n", title))
	sb.WriteString(neonLine + "\n\n")

	if mood != "" {
		sb.WriteString(fmt.Sprintf("🎭 当前心情: %s\n\n", mood))
		sb.WriteString(neonSeparator + "\n\n")
	}

	if len(results) == 0 {
		sb.WriteString("💫 暂时没有找到相关内容\n\n")
		return sb.String()
	}

	sb.WriteString("✨ 为你精选：\n\n")

	displayCount := len(results)
	if displayCount > 8 {
		displayCount = 8
	}

	for i, item := range results[:displayCount] {
		icon := getMediaTypeIcon(item.Type)
		year := ""
		if item.Year > 0 {
			year = fmt.Sprintf(" (%d)", item.Year)
		}
		rating := ""
		if item.Rating > 0 {
			rating = fmt.Sprintf(" ⏺️ %.1f", item.Rating)
		}

		sb.WriteString(fmt.Sprintf("%d. %s%s%s%s\n", i+1, escapeText(item.Title), year, icon, rating))

		if item.Overview != "" {
			overview := truncateAndEscape(item.Overview, 40)
			sb.WriteString(fmt.Sprintf("   %s\n", overview))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(neonSeparator + "\n")
	sb.WriteString("点击数字选择 · 换一批获取更多\n")

	return sb.String()
}

// BuildRequestList 构建请求列表
func (b *NeonBuilder) BuildRequestList(requests []services.SubscribeItem, page, totalPages, total int) string {
	var sb strings.Builder

	sb.WriteString(neonLine + "\n")
	sb.WriteString(fmt.Sprintf("📋 我的请求\n"))
	sb.WriteString(neonLine + "\n\n")

	if total == 0 {
		sb.WriteString("暂无记录\n\n")
		sb.WriteString("搜索后点击「求片」即可添加\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("共 %d 条，第 %d/%d 页\n\n", total, page, totalPages))
	sb.WriteString(neonSeparator + "\n\n")

	// 计算分页
	requestsPerPage := 10
	startIdx := (page - 1) * requestsPerPage
	endIdx := startIdx + requestsPerPage
	if endIdx > total {
		endIdx = total
	}

	for i := startIdx; i < endIdx; i++ {
		req := requests[i]

		// 状态图标
		statusEmoji := getRequestStatusEmoji(req.State)

		// 类型图标
		typeEmoji := getMediaTypeIcon(req.Type)

		// 标题
		title := req.Name
		if req.Year != "" && req.Year != "0" {
			title = fmt.Sprintf("%s (%s)", title, req.Year)
		}

		// 集数
		extra := ""
		if req.Season > 0 {
			extra = fmt.Sprintf(" S%d", req.Season)
		}
		if req.TotalEpisode > 0 {
			extra = fmt.Sprintf(" 共%d集", req.TotalEpisode)
		}

		sb.WriteString(fmt.Sprintf("%d. %s %s%s%s · %s\n",
			i+1, statusEmoji, title, extra, typeEmoji, req.State))

		if req.Date != "" {
			sb.WriteString(fmt.Sprintf("   📅 %s\n", req.Date[:10]))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(neonSeparator + "\n")
	sb.WriteString("点击编号查看详情 · 翻页浏览\n")

	return sb.String()
}

// 辅助函数：格式化概要
func formatOverview(text string) string {
	lines := strings.Split(text, "\n")
	var formatted strings.Builder
	for i, line := range lines {
		if i > 0 && len(line) > 0 {
			formatted.WriteString("  ")
		}
		formatted.WriteString(line)
		if i < len(lines)-1 {
			formatted.WriteString("\n")
		}
	}
	return formatted.String()
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
