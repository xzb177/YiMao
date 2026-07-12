package ui

import (
	"fmt"
	"strings"

	"github.com/xzb177/yimao/internal/services"
)

// CinemaBuilder 沉浸电影风格构建器
type CinemaBuilder struct{}

const (
	cinemaSeparator = "▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔"
	cinemaLine      = "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
)

// 电影台词库
var movieQuotes = []string{
	"恐惧让你沦为囚犯。希望让你重获自由。",
	"生命中真正重要的不是你遭遇了什么，而是你记住了哪些事，又是如何铭记的。",
	"有些鸟儿是永远关不住的，每一片羽毛都闪耀着自由的光辉。",
	"人生就像一盒巧克力，你永远不知道下一颗是什么味道。",
	"我们要相信，有些东西是值得为之奋斗、为之牺牲的。",
	"即使世界让你失望，也不要对自己失望。",
}

// BuildMenu 构建主菜单
func (b *CinemaBuilder) BuildMenu(title, subtitle string) string {
	var sb strings.Builder

	sb.WriteString(cinemaSeparator + "\n\n")

	sb.WriteString(fmt.Sprintf("              🎬 %s\n", title))
	sb.WriteString(fmt.Sprintf("           %s\n\n", subtitle))

	sb.WriteString(cinemaSeparator + "\n\n")

	// 添加一句电影台词
	sb.WriteString(fmt.Sprintf("%s\n\n", getRandomQuote()))

	return sb.String()
}

// BuildSearchResults 构建搜索结果
func (b *CinemaBuilder) BuildSearchResults(query string, results []services.SearchResult, page, total int) string {
	var sb strings.Builder

	sb.WriteString(cinemaSeparator + "\n\n")

	sb.WriteString(fmt.Sprintf("🔍 搜索: %s\n\n", query))

	sb.WriteString(cinemaSeparator + "\n\n")

	if len(results) == 0 {
		sb.WriteString(emptySearchCopy + "\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("✦ 找到 %d 部影片", total))
	if total > len(results) {
		sb.WriteString(fmt.Sprintf(" · 第 %d 页", page))
	}
	sb.WriteString("\n\n")
	sb.WriteString(cinemaLine + "\n\n")

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

		sb.WriteString(fmt.Sprintf("%d. %s%s%s%s\n", i+1, escapeText(item.Title), year, icon, rating))

		if item.Overview != "" {
			overview := truncateAndEscape(item.Overview, 50)
			sb.WriteString(fmt.Sprintf("   %s\n", overview))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(cinemaLine + "\n\n")
	sb.WriteString("点击编号查看详情 · 或继续探索\n\n")

	return sb.String()
}

// BuildMediaDetail 构建媒体详情
func (b *CinemaBuilder) BuildMediaDetail(result *services.SearchResult) string {
	var sb strings.Builder

	sb.WriteString(cinemaSeparator + "\n\n")

	// 标题
	sb.WriteString(fmt.Sprintf("              🎬 %s\n", escapeText(result.Title)))
	if int(result.Year) > 0 {
		sb.WriteString(fmt.Sprintf("              (%d)\n", result.Year))
	}

	sb.WriteString("\n")
	if result.Rating > 0 {
		sb.WriteString(fmt.Sprintf("                ★★★★★  %.1f\n\n", result.Rating))
	} else {
		sb.WriteString("\n\n")
	}

	sb.WriteString(cinemaSeparator + "\n\n")

	// 元信息
	sb.WriteString(fmt.Sprintf("✦ %s", getMediaTypeLabel(result.Type)))
	sb.WriteString(" ✦\n\n")

	sb.WriteString(cinemaLine + "\n\n")

	// 概要（电影式呈现）
	if result.Overview != "" {
		sb.WriteString("「剧情简介」\n\n")
		paragraphs := strings.Split(result.Overview, "\n\n")
		for _, para := range paragraphs {
			if strings.TrimSpace(para) != "" {
				sb.WriteString(fmt.Sprintf("    %s\n\n", wrapText(para, 28)))
			}
		}
	}

	sb.WriteString(cinemaLine + "\n\n")

	return sb.String()
}

// BuildRecommendation 构建推荐内容（沉浸电影风格）
func (b *CinemaBuilder) BuildRecommendation(title string, results []services.SearchResult, mood string) string {
	var sb strings.Builder

	sb.WriteString(cinemaSeparator + "\n\n")

	sb.WriteString(fmt.Sprintf("              🎬 %s\n", title))
	sb.WriteString("           今日精选\n\n")

	sb.WriteString(cinemaSeparator + "\n\n")

	if mood != "" {
		sb.WriteString(fmt.Sprintf("🎭 此刻心情: %s\n\n", mood))
		sb.WriteString(cinemaLine + "\n\n")
	}

	if len(results) == 0 {
		sb.WriteString("💫 暂无推荐\n\n")
		sb.WriteString(cinemaLine + "\n\n")
		sb.WriteString("有时候，\n")
		sb.WriteString("最好的选择，就是什么都不选，\n")
		sb.WriteString("静静等待下一个灵感的到来。\n\n")
		return sb.String()
	}

	sb.WriteString("✦ 为你精选的影片\n\n")

	displayCount := len(results)
	if displayCount > 6 {
		displayCount = 6
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

		sb.WriteString(fmt.Sprintf("%d. %s%s%s%s\n", i+1, escapeText(item.Title), year, icon, rating))

		if item.Overview != "" {
			overview := truncateAndEscape(item.Overview, 45)
			sb.WriteString(fmt.Sprintf("   %s\n", overview))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(cinemaLine + "\n\n")
	sb.WriteString("选择一部 · 或获取更多推荐\n\n")

	return sb.String()
}

// BuildRequestList 构建请求列表
func (b *CinemaBuilder) BuildRequestList(requests []services.SubscribeItem, page, totalPages, total int) string {
	var sb strings.Builder

	sb.WriteString(cinemaSeparator + "\n\n")

	sb.WriteString(fmt.Sprintf("              📋 我的请求\n\n"))

	sb.WriteString(cinemaSeparator + "\n\n")

	if total == 0 {
		sb.WriteString(emptyRequestCopy + "\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("✦ 共 %d 个请求 · 第 %d/%d 页\n\n", total, page, totalPages))
	sb.WriteString(cinemaLine + "\n\n")

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
			extra = fmt.Sprintf(" 第%d季", req.Season)
		}
		if req.TotalEpisode > 0 {
			extra = fmt.Sprintf(" %d集", req.TotalEpisode)
		}

		sb.WriteString(fmt.Sprintf("%d. %s %s%s%s\n", i+1, statusEmoji, title, extra, typeIcon))
		sb.WriteString(fmt.Sprintf("   %s\n", getRequestStatusLabel(req.State)))

		if req.Date != "" {
			sb.WriteString(fmt.Sprintf("   📅 %s\n", req.Date[:10]))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(cinemaLine + "\n\n")
	sb.WriteString("查看详情 · 或继续等待\n\n")

	return sb.String()
}
