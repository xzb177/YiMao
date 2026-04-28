package ui

import (
	"fmt"
	"strings"

	"github.com/xzb177/yimao/internal/services"
)

// FilmBuilder 文艺胶片风格构建器
type FilmBuilder struct{}

const (
	filmSeparator = "· · · · · · · · · · · · · · · · · ·"
	filmLine      = "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
)

// 伤感文案库
var filmQuotes = []string{
	"有些故事，像深夜里的一杯热茶，不惊艳，但温暖人心。",
	"我们终其一生，都在寻找一个愿意停下来的人。",
	"有些电影，不是为了让你哭，而是为了让你在泪光里，看清那个倔强的自己。",
	"生活不是电影，没有那么多意外和惊喜，但你可以选择，在平凡中，发现不平凡的美好。",
	"每一帧画面，都是时光的标本，收藏着那些无法重来的瞬间。",
	"有些情绪，只有在你独自一人的时候，才会悄悄浮现，像春天的雨，不张扬，却很真实。",
}

// BuildMenu 构建主菜单（文艺胶片风格）
func (b *FilmBuilder) BuildMenu(title, subtitle string) string {
	var sb strings.Builder

	sb.WriteString(filmSeparator + "\n")
	sb.WriteString(fmt.Sprintf("「%s」\n", title))
	sb.WriteString(filmSeparator + "\n\n")

	// 添加一段伤感文案
	quote := getRandomQuote()
	sb.WriteString(fmt.Sprintf("✨ %s\n\n", quote))

	sb.WriteString(fmt.Sprintf("%s\n\n", subtitle))

	return sb.String()
}

// BuildSearchResults 构建搜索结果
func (b *FilmBuilder) BuildSearchResults(query string, results []services.SearchResult, page, total int) string {
	var sb strings.Builder

	sb.WriteString(filmSeparator + "\n")
	sb.WriteString(fmt.Sprintf("「搜索 · %s」\n", query))
	sb.WriteString(filmSeparator + "\n\n")

	if len(results) == 0 {
		sb.WriteString("💫 未找到匹配的影片\n\n")
		sb.WriteString("也许有些故事，\n")
		sb.WriteString("注定只存在于想象之中。\n\n")
		sb.WriteString("💡 建议：\n")
		sb.WriteString("换个关键词，或许就能找到\n")
		sb.WriteString("那部一直在等你发现的电影。\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("✦ 找到 %d 部相关影片", total))
	if total > len(results) {
		sb.WriteString(fmt.Sprintf(" · 第 %d 页", page))
	}
	sb.WriteString("\n\n")
	sb.WriteString(filmSeparator + "\n\n")

	for i, item := range results {
		icon := getMediaTypeIcon(item.Type)
		year := ""
		if item.Year > 0 {
			year = fmt.Sprintf(" (%d)", item.Year)
		}
		rating := ""
		if item.Rating > 0 {
			rating = fmt.Sprintf(" ⭐ %.1f", item.Rating)
		}

		sb.WriteString(fmt.Sprintf("%d. %s%s%s%s\n", i+1, item.Title, year, icon, rating))

		if item.Overview != "" {
			overview := truncateText(item.Overview, 45)
			sb.WriteString(fmt.Sprintf("   %s\n", overview))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(filmSeparator + "\n")
	sb.WriteString("选择数字 · 或继续探索\n")

	return sb.String()
}

// BuildMediaDetail 构建媒体详情
func (b *FilmBuilder) BuildMediaDetail(result *services.SearchResult) string {
	var sb strings.Builder

	sb.WriteString(filmSeparator + "\n")

	// 标题
	sb.WriteString(fmt.Sprintf("🎬 %s", result.Title))
	if int(result.Year) > 0 {
		sb.WriteString(fmt.Sprintf(" (%d)", result.Year))
	}
	sb.WriteString("\n")

	sb.WriteString(filmSeparator + "\n\n")

	// 元信息
	if result.Rating > 0 {
		sb.WriteString(fmt.Sprintf("⭐ %.1f 分  ·  📅 %s\n", result.Rating, getMediaTypeLabel(result.Type)))
	} else {
		sb.WriteString(fmt.Sprintf("📅 %s\n", getMediaTypeLabel(result.Type)))
	}

	sb.WriteString("\n")
	sb.WriteString(filmLine + "\n\n")

	// 概要（用文艺风格呈现）
	if result.Overview != "" {
		sb.WriteString("「剧情简介」\n\n")
		paragraphs := strings.Split(result.Overview, "\n\n")
		for _, para := range paragraphs {
			if strings.TrimSpace(para) != "" {
				sb.WriteString(fmt.Sprintf("   %s\n\n", wrapText(para, 28)))
			}
		}
	}

	sb.WriteString(filmLine + "\n\n")

	// 添加一句相关的感悟
	sb.WriteString("\n")
	sb.WriteString("♪ 推荐理由\n")
	sb.WriteString(fmt.Sprintf("   %s\n\n", getRecommendationReason(result)))

	// TMDB ID
	sb.WriteString(fmt.Sprintf("🆔 TMDB ID: %d\n", result.ID))

	return sb.String()
}

// BuildRecommendation 构建推荐内容（文艺胶片风格）
func (b *FilmBuilder) BuildRecommendation(title string, results []services.SearchResult, mood string) string {
	var sb strings.Builder

	sb.WriteString(filmSeparator + "\n")
	sb.WriteString(fmt.Sprintf("「%s」\n", title))
	sb.WriteString(filmSeparator + "\n\n")

	if mood != "" {
		sb.WriteString(fmt.Sprintf("🎭 此时此刻，你或许需要...\n"))
		sb.WriteString(fmt.Sprintf("   %s\n\n", getMoodDescription(mood)))
		sb.WriteString(filmSeparator + "\n\n")
	}

	if len(results) == 0 {
		sb.WriteString("💫 暂时没有找到合适的影片\n\n")
		sb.WriteString("有时候，\n")
		sb.WriteString("最好的选择，就是什么都不选，\n")
		sb.WriteString("静静等待下一个灵感的到来。\n")
		return sb.String()
	}

	sb.WriteString("✦ 为你推荐的影片\n\n")

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
			rating = fmt.Sprintf(" ⭐ %.1f", item.Rating)
		}

		sb.WriteString(fmt.Sprintf("%d. %s%s%s%s\n", i+1, item.Title, year, icon, rating))

		// 添加一句简短的推荐语
		sb.WriteString(fmt.Sprintf("   %s\n\n", getFilmQuoteForMood(mood)))
	}

	sb.WriteString(filmSeparator + "\n")
	sb.WriteString("选择一部 · 或获取更多推荐\n")

	return sb.String()
}

// BuildRequestList 构建请求列表
func (b *FilmBuilder) BuildRequestList(requests []services.SubscribeItem, page, totalPages, total int) string {
	var sb strings.Builder

	sb.WriteString(filmSeparator + "\n")
	sb.WriteString("「我的请求」\n")
	sb.WriteString(filmSeparator + "\n\n")

	if total == 0 {
		sb.WriteString("✦ 暂无请求记录\n\n")
		sb.WriteString("每一部影片，\n")
		sb.WriteString("都是一次期待的开始。\n\n")
		sb.WriteString("搜索并添加，\n")
		sb.WriteString("让等待变成一种美好。\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("✦ 共 %d 个请求 · 第 %d/%d 页\n\n", total, page, totalPages))
	sb.WriteString(filmSeparator + "\n\n")

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

		// 状态描述
		statusDesc := getStatusDescription(req.State)

		sb.WriteString(fmt.Sprintf("%d. %s %s %s\n", i+1, statusEmoji, title, typeEmoji))
		sb.WriteString(fmt.Sprintf("   %s\n\n", statusDesc))
	}

	sb.WriteString(filmSeparator + "\n")
	sb.WriteString("查看详情 · 或继续等待\n")

	return sb.String()
}

// 辅助函数：获取随机文案
func getRandomQuote() string {
	if len(filmQuotes) == 0 {
		return ""
	}
	index := len(filmQuotes) / 2 // 简单选择中间的文案
	if index >= len(filmQuotes) {
		index = 0
	}
	return filmQuotes[index]
}

// 辅助函数：获取心情描述
func getMoodDescription(mood string) string {
	descriptions := map[string]string{
		"happy":   "一些快乐，能够治愈整个世界。",
		"sad":     "有时候，眼泪是最好的释放。",
		"relax":   "轻松的时光，是生活最珍贵的馈赠。",
		"excited": "激情和兴奋，让我们感受到活着的意义。",
		"calm":    "平静的日子里，藏着最真实的自己。",
	}
	if desc, ok := descriptions[mood]; ok {
		return desc
	}
	return "一部好片，总能找到共鸣。"
}

// 辅助函数：获取推荐理由
func getRecommendationReason(result *services.SearchResult) string {
	reasons := []string{
		"这部电影，值得你花两个小时去感受。",
		"有些故事，会悄悄改变你看待世界的方式。",
		"评分不错，值得一看。",
		"一个值得收藏的故事。",
	}

	if result.Rating >= 8.0 {
		return "高分作品，值得你细细品味。"
	}
	if len(reasons) > 0 {
		return reasons[0]
	}
	return "一部值得一看的影片。"
}

// 辅助函数：获取心情相关的电影语录
func getFilmQuoteForMood(mood string) string {
	quotes := map[string]string{
		"happy":   "快乐的时候，看什么都像阳光。",
		"sad":     "悲伤的时候，一部电影，一个拥抱。",
		"relax":   "放松下来，享受这个时刻。",
		"excited": "期待感，比结果更让人心动。",
		"calm":    "平静的心，能听懂更多。",
	}
	if quote, ok := quotes[mood]; ok {
		return quote
	}
	return "每个人都有自己的观影方式。"
}

// 辅助函数：获取状态描述
func getStatusDescription(state string) string {
	descriptions := map[string]string{
		"pending":    "等待中...",
		"recycled":   "重新搜索中...",
		"searching":  "正在寻找...",
		"downloading": "下载中...",
		"completed":  "已完成，可以观看了。",
		"failed":     "遇到了一些问题。",
		"cancelled":  "已取消。",
	}
	if desc, ok := descriptions[state]; ok {
		return desc
	}
	return "未知状态"
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
