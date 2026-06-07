package ui

import (
	"fmt"
	"strings"

	"github.com/xzb177/yimao/internal/services"
)

// ─────────────────────────────────────
// 云海影视 · 全新 UI
// 设计理念：管家迎门、信息有层次、文案有人味
// ─────────────────────────────────────

const (
	sep     = "─────────────────────────"
	boxTop  = "╭──────────────────────────╮"
	boxMid  = "├──────────────────────────┤"
	boxBot  = "╰──────────────────────────╯"
	boxLine = "│"
	dot     = "·"
)

// DashBuilder 仪表盘风格构建器
type DashBuilder struct{}

// BuildMenu 首页仪表盘
func (b *DashBuilder) BuildMenu(title, subtitle string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n", boxTop))
	sb.WriteString(fmt.Sprintf("%s  🌊 %s\n", boxLine, title))
	if subtitle != "" {
		sb.WriteString(fmt.Sprintf("%s  %s\n", boxLine, subtitle))
	}
	sb.WriteString(fmt.Sprintf("%s\n", boxBot))
	sb.WriteString("\n")
	sb.WriteString("把片名发给我就行，中英文都可以 🎬\n")
	return sb.String()
}

// BuildMenuWithName 带用户名的首页。
func (b *DashBuilder) BuildMenuWithName(name string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n", boxTop))
	sb.WriteString(fmt.Sprintf("%s  🌊 云海影视\n", boxLine))
	if name != "" {
		sb.WriteString(fmt.Sprintf("%s  你好，%s 👋\n", boxLine, name))
	} else {
		sb.WriteString(fmt.Sprintf("%s  你的私人选片师\n", boxLine))
	}
	sb.WriteString(fmt.Sprintf("%s\n", boxBot))
	sb.WriteString("\n")
	sb.WriteString("把片名发给我就行，中英文都可以 🎬\n")
	return sb.String()
}

// BuildSearchResults 搜索结果（卡片流）
func (b *DashBuilder) BuildSearchResults(query string, results []services.SearchResult, page, total int) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("🔍 %s\n", query))
	sb.WriteString(fmt.Sprintf("%s\n", sep))

	if len(results) == 0 {
		sb.WriteString("\n😢 暂时没有找到可用源\n\n")
		sb.WriteString("💡 可以试试：\n")
		sb.WriteString("  换个片名搜搜\n")
		sb.WriteString("  去许愿池许愿，有源了第一时间通知你\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("找到 %d 个结果", total))
	if total > 8 {
		sb.WriteString(fmt.Sprintf("，显示前 %d 个", len(results)))
	}
	sb.WriteString("\n\n")

	displayCount := len(results)
	if displayCount > 8 {
		displayCount = 8
	}

	for i := 0; i < displayCount; i++ {
		item := results[i]
		sb.WriteString(b.buildResultCard(i+1, item))
	}

	sb.WriteString(sep + "\n")
	sb.WriteString("点编号看详情，或继续搜其他片名\n")

	return sb.String()
}

// buildResultCard 单条搜索结果卡片
func (b *DashBuilder) buildResultCard(index int, item services.SearchResult) string {
	var sb strings.Builder

	icon := getMediaTypeIcon(item.Type)
	year := ""
	if item.Year > 0 {
		year = fmt.Sprintf(" · %d", item.Year)
	}
	rating := ""
	if item.Rating > 0 {
		rating = fmt.Sprintf("  ⭐%.1f", item.Rating)
	}

	sb.WriteString(fmt.Sprintf("╭─ %d ──────────────────────╮\n", index))
	sb.WriteString(fmt.Sprintf("│ %s %s%s%s\n", icon, escapeText(item.Title), year, rating))

	if item.Overview != "" {
		overview := truncateAndEscape(item.Overview, 48)
		sb.WriteString(fmt.Sprintf("│ %s\n", overview))
	}
	sb.WriteString(fmt.Sprintf("╰──────────────────────────╯\n"))

	return sb.String()
}

// BuildMediaDetail 媒体详情页
func (b *DashBuilder) BuildMediaDetail(result *services.SearchResult) string {
	var sb strings.Builder

	icon := getMediaTypeIcon(result.Type)
	year := ""
	if result.Year > 0 {
		year = fmt.Sprintf(" · %d", result.Year)
	}

	sb.WriteString(fmt.Sprintf("╭──────────────────────────╮\n"))
	sb.WriteString(fmt.Sprintf("│ %s %s%s\n", icon, escapeText(result.Title), year))

	// 评分行
	if result.Rating > 0 {
		stars := getStarDisplay(result.Rating)
		sb.WriteString(fmt.Sprintf("│ %s %.1f/10\n", stars, result.Rating))
	}

	sb.WriteString(fmt.Sprintf("│ %s · %s\n", getMediaTypeLabel(result.Type), fmt.Sprintf("TMDB #%d", result.ID)))
	sb.WriteString(fmt.Sprintf("╰──────────────────────────╯\n"))

	// 简介
	if result.Overview != "" {
		sb.WriteString("\n📖 ")
		sb.WriteString(wrapText(escapeText(result.Overview), 28))
		sb.WriteString("\n")
	}

	return sb.String()
}

// BuildRecommendation 推荐内容
func (b *DashBuilder) BuildRecommendation(title string, results []services.SearchResult, mood string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("🎬 %s\n", title))
	sb.WriteString(fmt.Sprintf("%s\n\n", sep))

	if mood != "" {
		sb.WriteString(fmt.Sprintf("%s\n\n", mood))
	}

	if len(results) == 0 {
		sb.WriteString("暂时没找到合适的，换一批试试？\n")
		return sb.String()
	}

	displayCount := len(results)
	if displayCount > 8 {
		displayCount = 8
	}

	for i, item := range results[:displayCount] {
		sb.WriteString(b.buildResultCard(i+1, item))
	}

	sb.WriteString(sep + "\n")
	sb.WriteString("点编号看详情\n")

	return sb.String()
}

// BuildRequestList 请求列表
func (b *DashBuilder) BuildRequestList(requests []services.SubscribeItem, page, totalPages, total int) string {
	var sb strings.Builder

	sb.WriteString("📊 求片进度\n")
	sb.WriteString(fmt.Sprintf("%s\n", sep))

	if total == 0 {
		sb.WriteString("\n还没有求过片～\n\n")
		sb.WriteString("💡 搜片名 → 选一部 → 求片\n")
		return sb.String()
	}

	// 统计
	pending, completed, failed := countRequestStates(requests)
	sb.WriteString(fmt.Sprintf("共 %d 条  ·  ", total))
	sb.WriteString(fmt.Sprintf("进行中 %d · 完成 %d · 异常 %d", pending, completed, failed))
	sb.WriteString(fmt.Sprintf("\n%s\n\n", sep))

	requestsPerPage := 10
	startIdx := (page - 1) * requestsPerPage
	endIdx := startIdx + requestsPerPage
	if endIdx > total {
		endIdx = total
	}

	// 分组
	pendingItems, doneItems, failedItems := groupRequests(requests, startIdx, endIdx)

	if len(pendingItems) > 0 {
		sb.WriteString("⏳ 进行中\n")
		for _, req := range pendingItems {
			sb.WriteString(fmt.Sprintf("  %s\n", formatRequestLine(req)))
		}
		sb.WriteString("\n")
	}

	if len(doneItems) > 0 {
		sb.WriteString("✅ 已完成\n")
		for _, req := range doneItems {
			sb.WriteString(fmt.Sprintf("  %s\n", formatRequestLine(req)))
		}
		sb.WriteString("\n")
	}

	if len(failedItems) > 0 {
		sb.WriteString("⚠️ 异常\n")
		for _, req := range failedItems {
			sb.WriteString(fmt.Sprintf("  %s\n", formatRequestLine(req)))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// ─── 共享辅助函数（其他 builder 也会用到）───

func countRequestStates(requests []services.SubscribeItem) (pending, completed, failed int) {
	for _, req := range requests {
		switch req.State {
		case "P", "R", "S", "D", "WISH", "REVIEWING", "STUCK":
			pending++
		case "C":
			completed++
		default:
			failed++
		}
	}
	return
}

func groupRequests(requests []services.SubscribeItem, startIdx, endIdx int) (pending, done, failed []services.SubscribeItem) {
	for i := startIdx; i < endIdx && i < len(requests); i++ {
		req := requests[i]
		switch req.State {
		case "P", "R", "S", "D":
			pending = append(pending, req)
		case "C":
			done = append(done, req)
		default:
			failed = append(failed, req)
		}
	}
	return
}

func formatRequestLine(req services.SubscribeItem) string {
	emoji := getRequestStatusEmoji(req.State)
	icon := getMediaTypeIcon(req.Type)
	title := req.Name
	if req.Year != "" && req.Year != "0" {
		title = fmt.Sprintf("%s (%s)", title, req.Year)
	}
	line := fmt.Sprintf("%s %s %s", emoji, title, icon)
	if req.Date != "" && len(req.Date) >= 10 {
		line += fmt.Sprintf(" · %s", req.Date[:10])
	}
	return line
}

func getStarDisplay(rating float64) string {
	if rating <= 0 {
		return ""
	}
	full := int(rating / 2)
	if full > 5 {
		full = 5
	}
	return strings.Repeat("★", full) + strings.Repeat("☆", 5-full)
}
