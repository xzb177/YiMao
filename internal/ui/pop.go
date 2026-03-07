package ui

import (
	"fmt"
	"strings"

	"emby-telegram-bot/internal/services"
)

// PopBuilder 波普艺术风格构建器
type PopBuilder struct{}

const (
	popSeparator = "🎨 🎨 🎨 🎨 🎨 🎨 🎨 🎨 🎨 🎨"
	popLine      = "━━━━━━━━━━━━━━━━━━━━━━━━━━"
)

// BuildMenu 构建主菜单（波普艺术风格）
func (b *PopBuilder) BuildMenu(title, subtitle string) string {
	var sb strings.Builder

	sb.WriteString(popSeparator + "\n")
	sb.WriteString(fmt.Sprintf("💥 %s 💥\n", title))
	sb.WriteString(popSeparator + "\n\n")

	sb.WriteString(fmt.Sprintf("🎯 %s\n\n", subtitle))
	sb.WriteString(popLine + "\n\n")

	return sb.String()
}

// BuildSearchResults 构建搜索结果
func (b *PopBuilder) BuildSearchResults(query string, results []services.SearchResult, page, total int) string {
	var sb strings.Builder

	sb.WriteString(popSeparator + "\n")
	sb.WriteString(fmt.Sprintf("🔍 搜索结果 · %s\n", query))
	sb.WriteString(popSeparator + "\n\n")

	if len(results) == 0 {
		sb.WriteString("😕 哎呀，没有找到！\n\n")
		sb.WriteString("💡 试试这些：\n")
		sb.WriteString("• 换个关键词\n")
		sb.WriteString("• 用英文搜索\n")
		sb.WriteString("• 少打几个字\n\n")
		sb.WriteString("🎲 要不要随机推荐一个？\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("🎉 找到了 %d 个！", total))
	if total > len(results) {
		sb.WriteString(fmt.Sprintf(" (第 %d 页)", page))
	}
	sb.WriteString("\n\n")
	sb.WriteString(popLine + "\n\n")

	for i, item := range results {
		icon := getMediaTypeIcon(item.Type)
		year := ""
		if item.Year > 0 {
			year = fmt.Sprintf(" (%d)", item.Year)
		}
		rating := ""
		if item.Rating > 0 {
			rating = fmt.Sprintf(" ★%.1f", item.Rating)
		}

		sb.WriteString(fmt.Sprintf("%d. %s%s%s%s\n", i+1, item.Title, year, icon, rating))

		if item.Overview != "" {
			overview := truncateText(item.Overview, 45)
			sb.WriteString(fmt.Sprintf("   %s\n", overview))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(popLine + "\n")
	sb.WriteString("💥 选一个看看！💥\n")

	return sb.String()
}

// BuildMediaDetail 构建媒体详情
func (b *PopBuilder) BuildMediaDetail(result *services.SearchResult) string {
	var sb strings.Builder

	sb.WriteString(popSeparator + "\n")

	// 标题
	sb.WriteString(fmt.Sprintf("🎬 %s", result.Title))
	if int(result.Year) > 0 {
		sb.WriteString(fmt.Sprintf(" (%d)", result.Year))
	}
	sb.WriteString("\n")

	sb.WriteString(popSeparator + "\n\n")

	// 元信息
	if result.Rating > 0 {
		sb.WriteString(fmt.Sprintf("📊 评分 %.1f  ·  🎬 %s\n",
			result.Rating, getMediaTypeLabel(result.Type)))
	} else {
		sb.WriteString(fmt.Sprintf("🎬 %s\n", getMediaTypeLabel(result.Type)))
	}

	sb.WriteString("\n")
	sb.WriteString(popLine + "\n\n")

	// 概要
	if result.Overview != "" {
		sb.WriteString("「剧情」\n\n")
		paragraphs := strings.Split(result.Overview, "\n\n")
		for _, para := range paragraphs {
			if strings.TrimSpace(para) != "" {
				sb.WriteString(fmt.Sprintf("   %s\n\n", wrapText(para, 30)))
			}
		}
	}

	sb.WriteString(popLine + "\n\n")

	sb.WriteString(fmt.Sprintf("🆔 TMDB ID: %d\n", result.ID))

	return sb.String()
}

// BuildRecommendation 构建推荐内容（波普艺术风格）
func (b *PopBuilder) BuildRecommendation(title string, results []services.SearchResult, mood string) string {
	var sb strings.Builder

	sb.WriteString(popSeparator + "\n")
	sb.WriteString(fmt.Sprintf("🎲 %s 🎲\n", title))
	sb.WriteString(popSeparator + "\n\n")

	if mood != "" {
		sb.WriteString(fmt.Sprintf("🎭 心情: %s\n\n", mood))
		sb.WriteString(popLine + "\n\n")
	}

	if len(results) == 0 {
		sb.WriteString("😅 暂时没有推荐！\n\n")
		sb.WriteString("💡 试试其他分类，\n")
		sb.WriteString("说不定有惊喜哦！\n")
		return sb.String()
	}

	sb.WriteString("🌟 精选推荐：\n\n")

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
			rating = fmt.Sprintf(" ★%.1f", item.Rating)
		}

		sb.WriteString(fmt.Sprintf("%d. %s%s%s%s\n", i+1, item.Title, year, icon, rating))

		if item.Overview != "" {
			overview := truncateText(item.Overview, 40)
			sb.WriteString(fmt.Sprintf("   %s\n", overview))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(popLine + "\n")
	sb.WriteString("💥 点击选择！💥\n")

	return sb.String()
}

// BuildRequestList 构建请求列表
func (b *PopBuilder) BuildRequestList(requests []services.SubscribeItem, page, totalPages, total int) string {
	var sb strings.Builder

	sb.WriteString(popSeparator + "\n")
	sb.WriteString("📋 我的请求\n")
	sb.WriteString(popSeparator + "\n\n")

	if total == 0 {
		sb.WriteString("😅 暂无请求！\n\n")
		sb.WriteString("💡 去搜索添加吧，\n")
		sb.WriteString("有好片子在等你！\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("🎉 共 %d 个 · 第 %d/%d 页\n\n", total, page, totalPages))
	sb.WriteString(popLine + "\n\n")

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

		sb.WriteString(fmt.Sprintf("%d. %s %s%s%s\n",
			i+1, statusEmoji, title, extra, typeEmoji))
		sb.WriteString(fmt.Sprintf("   %s\n", req.State))

		if req.Date != "" {
			sb.WriteString(fmt.Sprintf("   📅 %s\n", req.Date[:10]))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(popLine + "\n")
	sb.WriteString("💥 查看详情！💥\n")

	return sb.String()
}
