package ui

import (
	"fmt"
	"strings"

	"github.com/xzb177/yimao/internal/services"
)

// ─────────────────────────────────────
// 云海求片 · 文本卡片 UI
// 设计理念：管家迎门、信息有层次、文案有人味
//
// 排版约定（全局唯一一套，勿再引入新的画线风格）：
//   - 不使用 ASCII 边框：中文与 emoji 是双宽字符，固定宽度的框在
//     手机窄屏上必定错位、换行，观感廉价。层次改由 HTML 粗体 +
//     缩进 + 空行表达。
//   - 分隔线统一使用 sep 一种规格，其余长度/字符一律不再新增。
// ─────────────────────────────────────

const (
	// sep 是全局唯一的主分隔线（章节之间）。
	sep = "──────────────────"
	dot = "·"
	// indent 用于正文相对标题的层次缩进。
	indent = "  "
)

// DashBuilder 仪表盘风格构建器
type DashBuilder struct{}

// BuildMenu 首页仪表盘
func (b *DashBuilder) BuildMenu(title, subtitle string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🌊 <b>%s</b>\n", escapeText(title)))
	if subtitle != "" {
		sb.WriteString(fmt.Sprintf("%s\n", escapeText(subtitle)))
	}
	sb.WriteString("\n")
	sb.WriteString("想看的，交给云海\n")
	return sb.String()
}

// BuildMenuWithName 带用户名的首页。
func (b *DashBuilder) BuildMenuWithName(name string) string {
	var sb strings.Builder
	sb.WriteString("<b>云海求片</b>\n")
	if name != "" {
		sb.WriteString(fmt.Sprintf("你好，%s 👋\n", escapeText(name)))
	} else {
		sb.WriteString("你的私人选片师\n")
	}
	sb.WriteString("\n")
	sb.WriteString("想看的，交给云海\n")
	return sb.String()
}

// BuildSearchResults 搜索结果（卡片流）
func (b *DashBuilder) BuildSearchResults(query string, results []services.SearchResult, page, total int) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("🔍 <b>%s</b>\n", escapeText(query)))
	sb.WriteString(sep + "\n")

	if len(results) == 0 {
		sb.WriteString("\n" + emptySearchCopy + "\n")
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

// buildResultCard 单条搜索结果卡片。
// 采用「序号 + 粗体标题」起头、简介缩进一层的层次结构，
// 不再绘制固定宽度边框（中文/emoji 双宽会错位）。
func (b *DashBuilder) buildResultCard(index int, item services.SearchResult) string {
	var sb strings.Builder

	icon := getMediaTypeIcon(item.Type)
	year := ""
	if item.Year > 0 {
		year = fmt.Sprintf(" %s %d", dot, item.Year)
	}
	rating := ""
	if item.Rating > 0 {
		rating = fmt.Sprintf("  ⭐%.1f", item.Rating)
	}

	sb.WriteString(fmt.Sprintf("<b>%d.</b> %s <b>%s</b>%s%s\n",
		index, icon, escapeText(item.Title), year, rating))

	if item.Overview != "" {
		overview := truncateAndEscape(item.Overview, 48)
		sb.WriteString(fmt.Sprintf("%s%s\n", indent, overview))
	}
	sb.WriteString("\n")

	return sb.String()
}

// BuildMediaDetail 媒体详情页
func (b *DashBuilder) BuildMediaDetail(result *services.SearchResult) string {
	var sb strings.Builder

	icon := getMediaTypeIcon(result.Type)
	year := ""
	if result.Year > 0 {
		year = fmt.Sprintf(" %s %d", dot, result.Year)
	}

	sb.WriteString(fmt.Sprintf("%s <b>%s</b>%s\n", icon, escapeText(result.Title), year))

	// 评分行
	if result.Rating > 0 {
		stars := getStarDisplay(result.Rating)
		sb.WriteString(fmt.Sprintf("%s %.1f/10\n", stars, result.Rating))
	}

	sb.WriteString(fmt.Sprintf("%s\n", getMediaTypeLabel(result.Type)))
	sb.WriteString(sep + "\n")

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

	sb.WriteString(fmt.Sprintf("🎬 <b>%s</b>\n", escapeText(title)))
	sb.WriteString(sep + "\n\n")

	if mood != "" {
		sb.WriteString(fmt.Sprintf("%s\n\n", escapeText(mood)))
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

	sb.WriteString("📊 <b>求片进度</b>\n")
	sb.WriteString(sep + "\n")

	if total == 0 {
		sb.WriteString("\n" + emptyRequestCopy + "\n")
		return sb.String()
	}

	// 统计
	pending, completed, failed := countRequestStates(requests)
	sb.WriteString(fmt.Sprintf("共 %d 条  %s  ", total, dot))
	sb.WriteString(fmt.Sprintf("进行中 %d %s 完成 %d %s 异常 %d", pending, dot, completed, dot, failed))
	sb.WriteString("\n" + sep + "\n\n")

	requestsPerPage := 10
	startIdx := (page - 1) * requestsPerPage
	endIdx := startIdx + requestsPerPage
	if endIdx > total {
		endIdx = total
	}

	// 分组
	pendingItems, doneItems, failedItems := groupRequests(requests, startIdx, endIdx)

	if len(pendingItems) > 0 {
		sb.WriteString("⏳ <b>进行中</b>\n")
		for _, req := range pendingItems {
			sb.WriteString(fmt.Sprintf("%s%s\n", indent, formatRequestLine(req)))
		}
		sb.WriteString("\n")
	}

	if len(doneItems) > 0 {
		sb.WriteString("✅ <b>已完成</b>\n")
		for _, req := range doneItems {
			sb.WriteString(fmt.Sprintf("%s%s\n", indent, formatRequestLine(req)))
		}
		sb.WriteString("\n")
	}

	if len(failedItems) > 0 {
		sb.WriteString("⚠️ <b>异常</b>\n")
		for _, req := range failedItems {
			sb.WriteString(fmt.Sprintf("%s%s\n", indent, formatRequestLine(req)))
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
	title := escapeText(req.Name)
	if req.Year != "" && req.Year != "0" {
		title = fmt.Sprintf("%s (%s)", title, escapeText(req.Year))
	}
	line := fmt.Sprintf("%s %s %s", emoji, title, icon)
	if req.Date != "" && len(req.Date) >= 10 {
		line += fmt.Sprintf(" %s %s", dot, escapeText(req.Date[:10]))
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
