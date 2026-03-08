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
	sb.WriteString(fmt.Sprintf("🎬 %s\n", title))
	sb.WriteString(cardSeparator + "\n\n")

	sb.WriteString("直接发片名给我，就能找到想看的电影和剧集\n\n")

	sb.WriteString("👇 选一个开始\n")

	return sb.String()
}

// BuildSearchResults 构建搜索结果
func (b *CardBuilder) BuildSearchResults(query string, results []services.SearchResult, page, total int) string {
	var sb strings.Builder

	sb.WriteString(cardSeparator + "\n")
	sb.WriteString(fmt.Sprintf("🔍 \"%s\"\n\n", query))

	if len(results) == 0 {
		sb.WriteString("没找到，换个说法试试？\n\n")
		sb.WriteString("💡 建议：\n")
		sb.WriteString("• 用简短的关键词\n")
		sb.WriteString("• 换个中文名或英文名\n")
		sb.WriteString("• 点下方看看别人在搜什么\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("找到 %d 个结果\n\n", total))
	sb.WriteString(cardSeparator + "\n\n")

	displayCount := len(results)
	if displayCount > 8 {
		displayCount = 8
	}

	for i := 0; i < displayCount; i++ {
		item := results[i]
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
	sb.WriteString("点数字看详情，或继续搜索其他片名\n")

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
		sb.WriteString(fmt.Sprintf("😊 %s\n\n", mood))
		sb.WriteString(cardSeparator + "\n\n")
	}

	if len(results) == 0 {
		sb.WriteString("暂时没有找到，换个心情试试？\n\n")
		sb.WriteString("💡 点「🔄 换一批」刷新\n")
		return sb.String()
	}

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
	sb.WriteString("点数字看详情\n")

	return sb.String()
}

// BuildRequestList 构建请求列表
func (b *CardBuilder) BuildRequestList(requests []services.SubscribeItem, page, totalPages, total int) string {
	var sb strings.Builder

	sb.WriteString(cardSeparator + "\n")
	sb.WriteString("📋 我的请求\n")
	sb.WriteString(cardSeparator + "\n\n")

	if total == 0 {
		sb.WriteString("还没有提交过求片\n\n")
		sb.WriteString("💡 搜影片 → 点数字 → 求片\n")
		return sb.String()
	}

	// 统计状态
	pending := 0
	completed := 0
	failed := 0
	for _, req := range requests {
		switch req.State {
		case "PENDING", "RECYCLED", "SEARCHING", "DOWNLOADING":
			pending++
		case "COMPLETED":
			completed++
		case "FAILED", "CANCELLED":
			failed++
		}
	}

	sb.WriteString(fmt.Sprintf("共 %d 条，第 %d/%d 页\n", total, page, totalPages))
	sb.WriteString(fmt.Sprintf("进行中 %d · 已完成 %d · 异常 %d\n", pending, completed, failed))
	sb.WriteString("────────\n\n")

	// 计算分页
	requestsPerPage := 10
	startIdx := (page - 1) * requestsPerPage
	endIdx := startIdx + requestsPerPage
	if endIdx > total {
		endIdx = total
	}

	// 按状态分组显示
	pendingItems := []services.SubscribeItem{}
	doneItems := []services.SubscribeItem{}
	failedItems := []services.SubscribeItem{}

	for i := startIdx; i < endIdx; i++ {
		req := requests[i]
		switch req.State {
		case "PENDING", "RECYCLED", "SEARCHING", "DOWNLOADING":
			pendingItems = append(pendingItems, req)
		case "COMPLETED":
			doneItems = append(doneItems, req)
		default:
			failedItems = append(failedItems, req)
		}
	}

	if len(pendingItems) > 0 {
		sb.WriteString("【进行中】\n")
		for _, req := range pendingItems {
			statusEmoji := getRequestStatusEmoji(req.State)
			typeIcon := getMediaTypeIcon(req.Type)
			title := req.Name
			if req.Year != "" && req.Year != "0" {
				title = fmt.Sprintf("%s (%s)", title, req.Year)
			}
			line := fmt.Sprintf("%s %s %s", statusEmoji, title, typeIcon)
			if req.Date != "" && len(req.Date) >= 10 {
				line += fmt.Sprintf(" · %s", req.Date[:10])
			}
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}

	if len(doneItems) > 0 {
		sb.WriteString("【已完成】\n")
		for _, req := range doneItems {
			statusEmoji := getRequestStatusEmoji(req.State)
			typeIcon := getMediaTypeIcon(req.Type)
			title := req.Name
			if req.Year != "" && req.Year != "0" {
				title = fmt.Sprintf("%s (%s)", title, req.Year)
			}
			line := fmt.Sprintf("%s %s %s", statusEmoji, title, typeIcon)
			if req.Date != "" && len(req.Date) >= 10 {
				line += fmt.Sprintf(" · %s", req.Date[:10])
			}
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}

	if len(failedItems) > 0 {
		sb.WriteString("【异常】\n")
		for _, req := range failedItems {
			statusEmoji := getRequestStatusEmoji(req.State)
			typeIcon := getMediaTypeIcon(req.Type)
			title := req.Name
			if req.Year != "" && req.Year != "0" {
				title = fmt.Sprintf("%s (%s)", title, req.Year)
			}
			line := fmt.Sprintf("%s %s %s", statusEmoji, title, typeIcon)
			if req.Date != "" && len(req.Date) >= 10 {
				line += fmt.Sprintf(" · %s", req.Date[:10])
			}
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
