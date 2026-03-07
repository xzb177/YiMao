package ui

import (
	"fmt"
	"strings"

	"emby-telegram-bot/internal/services"
)

// CardBuilder 极简卡片风格构建器
type CardBuilder struct{}

const (
	cardSeparator = "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	cardBoxStart  = "┌─────────────────────────────┐"
	cardBoxEnd    = "└─────────────────────────────┘"
)

// BuildMenu 构建主菜单
func (b *CardBuilder) BuildMenu(title, subtitle string) string {
	var sb strings.Builder

	sb.WriteString(cardSeparator + "\n")
	sb.WriteString(fmt.Sprintf("📋 %s\n", title))
	sb.WriteString(cardSeparator + "\n\n")

	sb.WriteString(fmt.Sprintf("%s\n\n", subtitle))

	// 功能列表
	sb.WriteString("🔍 搜索影片 · 快速查找想看的内容\n")
	sb.WriteString("💫 情绪选片 · 按心情一键找片\n")
	sb.WriteString("🎯 不纠结 · 直接给你三种风格候选\n")
	sb.WriteString("📋 我的请求 · 查看求片进度\n")
	sb.WriteString("🐞 我的反馈 · 查看处理结果\n")
	sb.WriteString("🔗 绑定账号 · 同步账号信息\n")

	sb.WriteString("\n")
	sb.WriteString("👇 选择下方功能开始探索\n")

	return sb.String()
}

// BuildSearchResults 构建搜索结果
func (b *CardBuilder) BuildSearchResults(query string, results []services.SearchResult, page, total int) string {
	var sb strings.Builder

	sb.WriteString(cardSeparator + "\n")
	sb.WriteString(fmt.Sprintf("🔍 搜索: %s\n", query))
	sb.WriteString(cardSeparator + "\n\n")

	if len(results) == 0 {
		sb.WriteString("❌ 未找到结果\n\n")
		sb.WriteString("建议：\n")
		sb.WriteString("• 检查拼写\n")
		sb.WriteString("• 尝试简短关键词\n")
		sb.WriteString("• 使用英文搜索\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("结果: %d", total))
	if total > len(results) {
		sb.WriteString(fmt.Sprintf(" (第 %d 页)", page))
	}
	sb.WriteString("\n\n")
	sb.WriteString(cardSeparator + "\n\n")

	for i, item := range results {
		icon := getMediaTypeIcon(item.Type)
		year := ""
		if item.Year > 0 {
			year = fmt.Sprintf(" (%d)", item.Year)
		}
		rating := ""
		if item.Rating > 0 {
			rating = fmt.Sprintf(" [%.1f]", item.Rating)
		}

		sb.WriteString(fmt.Sprintf("%d. %s%s%s%s\n", i+1, item.Title, year, icon, rating))

		if item.Overview != "" {
			overview := truncateText(item.Overview, 50)
			sb.WriteString(fmt.Sprintf("   %s\n", overview))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(cardSeparator + "\n")
	sb.WriteString("输入数字选择或翻页\n")

	return sb.String()
}

// BuildMediaDetail 构建媒体详情
func (b *CardBuilder) BuildMediaDetail(result *services.SearchResult) string {
	var sb strings.Builder

	sb.WriteString(cardSeparator + "\n")
	sb.WriteString(fmt.Sprintf("🎬 %s", result.Title))
	if int(result.Year) > 0 {
		sb.WriteString(fmt.Sprintf(" (%d)", result.Year))
	}
	sb.WriteString("\n")

	sb.WriteString(cardSeparator + "\n\n")

	// 信息卡片
	sb.WriteString(cardBoxStart + "\n")
	if result.Rating > 0 {
		sb.WriteString(fmt.Sprintf("  📊 评分: %.1f\n", result.Rating))
	}
	sb.WriteString(fmt.Sprintf("  🎬 类型: %s\n", getMediaTypeLabel(result.Type)))
	sb.WriteString(cardBoxEnd + "\n\n")

	// 概要卡片
	if result.Overview != "" {
		sb.WriteString(cardBoxStart + "\n")
		sb.WriteString("  📖 剧情简介\n")
		sb.WriteString(cardSeparator + "\n")
		sb.WriteString(fmt.Sprintf("  %s\n", wrapText(result.Overview, 26)))
		sb.WriteString(cardBoxEnd + "\n\n")
	}

	sb.WriteString(fmt.Sprintf("🆔 TMDB ID: %d\n", result.ID))

	return sb.String()
}

// BuildRecommendation 构建推荐内容
func (b *CardBuilder) BuildRecommendation(title string, results []services.SearchResult, mood string) string {
	var sb strings.Builder

	sb.WriteString(cardSeparator + "\n")
	sb.WriteString(fmt.Sprintf("🎬 %s\n", title))
	sb.WriteString(cardSeparator + "\n\n")

	if mood != "" {
		sb.WriteString(fmt.Sprintf("🎭 心情: %s\n\n", mood))
		sb.WriteString(cardSeparator + "\n\n")
	}

	if len(results) == 0 {
		sb.WriteString("❌ 暂无推荐\n\n")
		sb.WriteString("建议：\n")
		sb.WriteString("• 更换心情类型\n")
		sb.WriteString("• 刷新获取新推荐\n")
		return sb.String()
	}

	sb.WriteString("✨ 为你推荐\n\n")

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
			rating = fmt.Sprintf(" [%.1f]", item.Rating)
		}

		sb.WriteString(fmt.Sprintf("%d. %s%s%s%s\n", i+1, item.Title, year, icon, rating))

		if item.Overview != "" {
			overview := truncateText(item.Overview, 40)
			sb.WriteString(fmt.Sprintf("   %s\n", overview))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(cardSeparator + "\n")
	sb.WriteString("点击数字选择或刷新\n")

	return sb.String()
}

// BuildRequestList 构建请求列表
func (b *CardBuilder) BuildRequestList(requests []services.SubscribeItem, page, totalPages, total int) string {
	var sb strings.Builder

	sb.WriteString(cardSeparator + "\n")
	sb.WriteString("📋 我的请求\n")
	sb.WriteString(cardSeparator + "\n\n")

	if total == 0 {
		sb.WriteString("❌ 暂无请求\n\n")
		sb.WriteString("💡 搜索并求片即可添加\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("总计: %d 条 · 第 %d/%d 页\n\n", total, page, totalPages))
	sb.WriteString(cardSeparator + "\n\n")

	// 计算分页
	requestsPerPage := 10
	startIdx := (page - 1) * requestsPerPage
	endIdx := startIdx + requestsPerPage
	if endIdx > total {
		endIdx = total
	}

	for i := startIdx; i < endIdx; i++ {
		req := requests[i]

		statusEmoji := getRequestStatusEmoji(req.State)
		typeIcon := getMediaTypeIcon(req.Type)

		title := req.Name
		if req.Year != "" && req.Year != "0" {
			title = fmt.Sprintf("%s (%s)", title, req.Year)
		}

		extra := ""
		if req.Season > 0 {
			extra = fmt.Sprintf(" S%d", req.Season)
		}
		if req.TotalEpisode > 0 {
			extra = fmt.Sprintf(" %d集", req.TotalEpisode)
		}

		sb.WriteString(fmt.Sprintf("%d. %s %s%s%s [%s]\n",
			i+1, statusEmoji, title, extra, typeIcon, req.State))

		if req.Date != "" {
			sb.WriteString(fmt.Sprintf("   📅 %s\n", req.Date[:10]))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(cardSeparator + "\n")
	sb.WriteString("输入数字查看详情或翻页\n")

	return sb.String()
}
