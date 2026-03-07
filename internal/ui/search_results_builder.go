package ui

import (
	"fmt"
	"strings"

	"emby-telegram-bot/internal/services"
)

// SearchResultsBuilder 优化后的搜索结果构建器
type SearchResultsBuilder struct {
	style UIStyle
}

// NewSearchResultsBuilder 创建搜索结果构建器
func NewSearchResultsBuilder(style UIStyle) *SearchResultsBuilder {
	return &SearchResultsBuilder{style: style}
}

// BuildSearchResultsMessage 构建搜索结果消息
func (b *SearchResultsBuilder) BuildSearchResultsMessage(query string, results []services.SearchResult, page, total int) string {
	var sb strings.Builder

	// 根据风格选择构建方式
	switch b.style {
	case StyleNeon:
		return b.buildNeonSearchResults(query, results, page, total)
	case StyleFilm:
		return b.buildFilmSearchResults(query, results, page, total)
	case StylePop:
		return b.buildPopSearchResults(query, results, page, total)
	case StyleCard:
		return b.buildCardSearchResults(query, results, page, total)
	default:
		return b.buildCardSearchResults(query, results, page, total) // 默认使用极简卡片风
	}
}

// buildNeonSearchResults 暗黑霓虹风格搜索结果
func (b *SearchResultsBuilder) buildNeonSearchResults(query string, results []services.SearchResult, page, total int) string {
	var sb strings.Builder

	// 标题分隔线
	sb.WriteString(neonSeparator + "\n")
	sb.WriteString(fmt.Sprintf("🔮 搜索结果 · %s\n", query))
	sb.WriteString(neonSeparator + "\n\n")

	// 装饰线
	sb.WriteString(neonLine + "\n\n")

	// 统计信息
	sb.WriteString(fmt.Sprintf("📊 找到 %d 个结果", total))
	if total > len(results) {
		sb.WriteString(fmt.Sprintf(" (第 %d 页)", page))
	}
	sb.WriteString("\n\n")

	// 装饰线
	sb.WriteString(neonLine + "\n\n")

	// 结果列表
	displayCount := len(results)
	for i, item := range results {
		// 类型图标
		icon := getMediaTypeIcon(item.Type)

		// 年份
		year := ""
		if item.Year > 0 {
			year = fmt.Sprintf(" (%d)", item.Year)
		}

		// 评分
		rating := ""
		if item.Rating > 0 {
			rating = fmt.Sprintf(" ⭐%.1f", item.Rating)
		}

		// 结果项
		sb.WriteString(fmt.Sprintf("▸ %d. %s%s%s%s\n", i+1, item.Title, year, icon, rating))

		// 概要（如果有）
		if item.Overview != "" {
			overview := truncateText(item.Overview, 45)
			sb.WriteString(fmt.Sprintf("   %s\n", overview))
		}
		sb.WriteString("\n")
	}

	// 装饰线
	sb.WriteString(neonLine + "\n")
	sb.WriteString("输入数字选择 · 或翻页 ↓\n")

	return sb.String()
}

// buildFilmSearchResults 文艺胶片风格搜索结果
func (b *SearchResultsBuilder) buildFilmSearchResults(query string, results []services.SearchResult, page, total int) string {
	var sb strings.Builder

	// 标题分隔线
	sb.WriteString(filmSeparator + "\n")
	sb.WriteString(fmt.Sprintf("「搜索 · %s」\n", query))
	sb.WriteString(filmSeparator + "\n\n")

	// 统计信息
	sb.WriteString(fmt.Sprintf("✦ 找到 %d 部相关影片", total))
	if total > len(results) {
		sb.WriteString(fmt.Sprintf(" · 第 %d 页", page))
	}
	sb.WriteString("\n\n")

	// 分隔线
	sb.WriteString(filmSeparator + "\n\n")

	// 结果列表
	displayCount := len(results)
	for i, item := range results {
		// 类型图标
		icon := getMediaTypeIcon(item.Type)

		// 年份
		year := ""
		if item.Year > 0 {
			year = fmt.Sprintf(" (%d)", item.Year)
		}

		// 评分
		rating := ""
		if item.Rating > 0 {
			rating = fmt.Sprintf(" ⭐%.1f", item.Rating)
		}

		// 结果项
		sb.WriteString(fmt.Sprintf("%d. %s%s%s%s\n", i+1, item.Title, year, icon, rating))

		// 概要
		if item.Overview != "" {
			overview := truncateText(item.Overview, 40)
			sb.WriteString(fmt.Sprintf("   %s\n", overview))
		}
		sb.WriteString("\n")
	}

	// 分隔线
	sb.WriteString(filmSeparator + "\n")
	sb.WriteString("选择数字 · 或继续探索\n")

	return sb.String()
}

// buildPopSearchResults 波普艺术风格搜索结果
func (b *SearchResultsBuilder) buildPopSearchResults(query string, results []services.SearchResult, page, total int) string {
	var sb strings.Builder

	// 标题分隔线
	sb.WriteString(popSeparator + "\n")
	sb.WriteString(fmt.Sprintf("🔍 搜索结果 · %s\n", query))
	sb.WriteString(popSeparator + "\n\n")

	// 统计信息
	sb.WriteString(fmt.Sprintf("🎉 找到了 %d 个！", total))
	if total > len(results) {
		sb.WriteString(fmt.Sprintf(" (第 %d 页)", page))
	}
	sb.WriteString("\n\n")

	// 分隔线
	sb.WriteString(popLine + "\n\n")

	// 结果列表
	displayCount := len(results)
	for i, item := range results {
		// 类型图标
		icon := getMediaTypeIcon(item.Type)

		// 年份
		year := ""
		if item.Year > 0 {
			year = fmt.Sprintf(" (%d)", item.Year)
		}

		// 评分
		rating := ""
		if item.Rating > 0 {
			rating = fmt.Sprintf(" ★%.1f", item.Rating)
		}

		// 结果项
		sb.WriteString(fmt.Sprintf("%d. %s%s%s%s\n", i+1, item.Title, year, icon, rating))

		// 概要
		if item.Overview != "" {
			overview := truncateText(item.Overview, 45)
			sb.WriteString(fmt.Sprintf("   %s\n", overview))
		}
		sb.WriteString("\n")
	}

	// 分隔线
	sb.WriteString(popLine + "\n")
	sb.WriteString("💥 选一个看看！💥\n")

	return sb.String()
}

// buildCardSearchResults 极简卡片风格搜索结果
func (b *SearchResultsBuilder) buildCardSearchResults(query string, results []services.SearchResult, page, total int) string {
	var sb strings.Builder

	// 标题分隔线
	sb.WriteString(cardSeparator + "\n")
	sb.WriteString(fmt.Sprintf("🔍 搜索: %s\n", query))
	sb.WriteString(cardSeparator + "\n\n")

	// 统计信息
	sb.WriteString(fmt.Sprintf("结果: %d", total))
	if total > len(results) {
		sb.WriteString(fmt.Sprintf(" (第 %d 页)", page))
	}
	sb.WriteString("\n\n")
	sb.WriteString(cardSeparator + "\n\n")

	// 结果列表
	displayCount := len(results)
	for i, item := range results {
		// 类型图标
		icon := getMediaTypeIcon(item.Type)

		// 年份
		year := ""
		if item.Year > 0 {
			year = fmt.Sprintf(" (%d)", item.Year)
		}

		// 评分
		rating := ""
		if item.Rating > 0 {
			rating = fmt.Sprintf(" [%.1f]", item.Rating)
		}

		// 结果项（卡片式）
		sb.WriteString(fmt.Sprintf("%d. %s%s%s%s\n", i+1, item.Title, year, icon, rating))

		// 概要（如果有）
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

// BuildSearchKeyboard 构建搜索结果键盘
func (b *SearchResultsBuilder) BuildSearchKeyboard(results []services.SearchResult, page, totalPages int) *SearchKeyboard {
	kb := &SearchKeyboard{
		Buttons: make([][]SearchButton, 0),
	}

	// 数字按钮（每行4个）
	const buttonsPerRow = 4
	displayCount := len(results)
	if displayCount > 8 {
		displayCount = 8
	}

	for i := 0; i < displayCount; i++ {
		if i%buttonsPerRow == 0 {
			kb.Buttons = append(kb.Buttons, []SearchButton{})
		}

		item := results[i]
		kb.Buttons[len(kb.Buttons)-1] = append(kb.Buttons[len(kb.Buttons)-1], SearchButton{
			Text:         fmt.Sprintf("%d", i+1),
			CallbackData: fmt.Sprintf("select:id:%d:type:%s", item.ID, item.Type),
		})
	}

	// 分页按钮
	paginationRow := []SearchButton{}
	if page > 1 {
		paginationRow = append(paginationRow, SearchButton{
			Text:         "⬅️ 上一页",
			CallbackData: fmt.Sprintf("search:query:%s:page:%d", "", page-1),
		})
	}
	paginationRow = append(paginationRow, SearchButton{
		Text:         fmt.Sprintf("PAGE %d/%d", page, totalPages),
		CallbackData: fmt.Sprintf("search:query:%s:page:%d", "", page),
	})
	if page < totalPages {
		paginationRow = append(paginationRow, SearchButton{
			Text:         "下一页 ➡️",
			CallbackData: fmt.Sprintf("search:query:%s:page:%d", "", page+1),
		})
	}
	kb.Buttons = append(kb.Buttons, paginationRow)

	// 返回按钮
	kb.Buttons = append(kb.Buttons, []SearchButton{
		{Text: "⬅️ 返回主菜单", CallbackData: "start"},
	})

	return kb
}

// SearchKeyboard 搜索结果键盘
type SearchKeyboard struct {
	Buttons [][]SearchButton
}

// SearchKeyboard 搜索按钮
type SearchButton struct {
	Text         string
	CallbackData string
}
