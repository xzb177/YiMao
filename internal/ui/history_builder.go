package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
)

// HistoryBuilder 搜索历史 UI 构建器
type HistoryBuilder struct {
	style UIStyle
}

// NewHistoryBuilder 创建历史记录构建器
func NewHistoryBuilder(style UIStyle) *HistoryBuilder {
	return &HistoryBuilder{style: style}
}

// BuildHistoryUI 构建搜索历史界面
func (b *HistoryBuilder) BuildHistoryUI(userID int64, stats *services.SearchStats, groupedHistory map[string][]services.SearchEntry, popularSearches []services.PopularSearch, trends []services.TrendItem) string {
	switch b.style {
	case StyleCard:
		return b.buildCardHistoryUI(userID, stats, groupedHistory, popularSearches, trends)
	case StyleNeon:
		return b.buildNeonHistoryUI(userID, stats, groupedHistory, popularSearches, trends)
	default:
		return b.buildCardHistoryUI(userID, stats, groupedHistory, popularSearches, trends)
	}
}

// buildNeonHistoryUI 暗黑霓虹风格
func (b *HistoryBuilder) buildNeonHistoryUI(userID int64, stats *services.SearchStats, groupedHistory map[string][]services.SearchEntry, popularSearches []services.PopularSearch, trends []services.TrendItem) string {
	var sb strings.Builder

	sb.WriteString(NeonLine + "\n")
	sb.WriteString("🔮 搜索历史 · 你的观影足迹\n")
	sb.WriteString(NeonLine + "\n\n")
	sb.WriteString("✨ 记录每一次搜索，随时重温观影记忆\n\n")

	// 统计数据
	sb.WriteString("📊 统计数据\n")
	sb.WriteString(NeonSeparator + "\n\n")

	// 处理 nil stats
	if stats == nil {
		sb.WriteString("• 暂无统计数据\n")
	} else {
		sb.WriteString(fmt.Sprintf("• 总搜索次数：%d 次\n", stats.Total))
		sb.WriteString(fmt.Sprintf("• 本周搜索：%d 次\n", stats.Week))
		sb.WriteString(fmt.Sprintf("• 本月搜索：%d 次\n", stats.Month))
		if len(stats.Top5) > 0 {
			sb.WriteString(fmt.Sprintf("• 最常搜索：%s (%d次)\n", stats.Top5[0], stats.Total/len(stats.Top5)))
		}
	}

	sb.WriteString("\n")
	sb.WriteString(NeonSeparator + "\n\n")

	// 分组历史记录
	groupOrder := []string{"今天", "本周", "本月", "更早"}
	for _, group := range groupOrder {
		entries, exists := groupedHistory[group]
		if !exists || len(entries) == 0 {
			continue
		}

		sb.WriteString(fmt.Sprintf("📅 %s (%d条)\n\n", group, len(entries)))

		for i, entry := range entries {
			countText := ""
			if entry.Count > 1 {
				countText = fmt.Sprintf(" [%d次]", entry.Count)
			}

			timeText := formatTimeAgo(entry.Timestamp)

			sb.WriteString(fmt.Sprintf("%d. 🔍 %s%s\n", i+1, entry.Query, countText))
			sb.WriteString(fmt.Sprintf("   %s\n", timeText))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(NeonLine + "\n")

	return sb.String()
}

// buildCardHistoryUI 极简卡片风格
func (b *HistoryBuilder) buildCardHistoryUI(userID int64, stats *services.SearchStats, groupedHistory map[string][]services.SearchEntry, popularSearches []services.PopularSearch, trends []services.TrendItem) string {
	var sb strings.Builder

	sb.WriteString("🔍 搜索历史\n\n")
	sb.WriteString("记录你的观影足迹，随时回顾搜索过的影片\n\n")

	// 统计数据
	if stats != nil && stats.Total > 0 {
		sb.WriteString(fmt.Sprintf("📊 总计 %d 次 · 本周 %d 次\n\n", stats.Total, stats.Week))
	} else {
		sb.WriteString("📊 暂无统计数据\n\n")
	}

	// 处理空的 groupedHistory
	if groupedHistory == nil {
		groupedHistory = make(map[string][]services.SearchEntry)
	}

	// 分组历史记录
	groupOrder := []string{"今天", "本周", "本月", "更早"}
	hasHistory := false
	for _, group := range groupOrder {
		entries, exists := groupedHistory[group]
		if !exists || len(entries) == 0 {
			continue
		}

		if !hasHistory {
			hasHistory = true
		} else {
			sb.WriteString("\n")
		}

		sb.WriteString(fmt.Sprintf("📅 %s (%d)\n", group, len(entries)))

		for i, entry := range entries {
			if i >= 5 {
				break
			}
			countText := ""
			if entry.Count > 1 {
				countText = fmt.Sprintf(" ×%d", entry.Count)
			}

			timeText := formatTimeAgo(entry.Timestamp)

			sb.WriteString(fmt.Sprintf("  %d. %s%s · %s\n", i+1, entry.Query, countText, timeText))
		}
		sb.WriteString("\n")
	}

	if !hasHistory {
		sb.WriteString("暂无搜索记录\n\n")
		sb.WriteString("💡 搜索影片后会自动保存到历史记录\n")
		sb.WriteString("📝 点击下方按钮查看更多功能\n")
	}

	return sb.String()
}

// BuildHistoryKeyboard 构建历史记录键盘
func (b *HistoryBuilder) BuildHistoryKeyboard(history []services.SearchEntry, userID int64) *callback.Keyboard {
	switch b.style {
	case StyleCard:
		return b.buildCardHistoryKeyboard(history, userID)
	case StyleNeon:
		return b.buildNeonHistoryKeyboard(history, userID)
	default:
		return b.buildCardHistoryKeyboard(history, userID)
	}
}

// buildCardHistoryKeyboard 极简卡片风格键盘
func (b *HistoryBuilder) buildCardHistoryKeyboard(history []services.SearchEntry, userID int64) *callback.Keyboard {
	var rows [][]callback.Button

	displayCount := len(history)
	if displayCount > 10 {
		displayCount = 10
	}

	// 数字按钮（每行2个）
	const buttonsPerRow = 2
	for i := 0; i < displayCount; i++ {
		if i%buttonsPerRow == 0 {
			rows = append(rows, []callback.Button{})
		}

		query := history[i].Query
		// Limit query length for button text
		buttonText := fmt.Sprintf("%d. %s", i+1, truncateText(query, 12))

		// Build callback with escape
		callbackData := fmt.Sprintf("search:hist:%d", i)

		rows[len(rows)-1] = append(rows[len(rows)-1], callback.Button{
			Text:         buttonText,
			CallbackData: callbackData,
		})
	}

	// 操作按钮行 - 只有统计按钮
	actionRow := []callback.Button{
		{Text: "📊 查看统计", CallbackData: "search_stats"},
	}
	rows = append(rows, actionRow)

	// 管理按钮行
	manageRow := []callback.Button{}
	if len(history) > 0 {
		manageRow = append(manageRow, callback.Button{
			Text:         "🗑️ 清空历史",
			CallbackData: "search_clear_all",
		})
	}
	manageRow = append(manageRow, callback.Button{
		Text:         "⚙️ 管理历史",
		CallbackData: "search_manage",
	})
	rows = append(rows, manageRow)

	// 返回按钮
	rows = append(rows, []callback.Button{
		{Text: "⬅️ 返回", CallbackData: "start"},
	})

	return &callback.Keyboard{InlineKeyboard: rows}
}

// buildNeonHistoryKeyboard 暗黑霓虹风格键盘
func (b *HistoryBuilder) buildNeonHistoryKeyboard(history []services.SearchEntry, userID int64) *callback.Keyboard {
	var rows [][]callback.Button

	displayCount := len(history)
	if displayCount > 10 {
		displayCount = 10
	}

	// 数字按钮（每行2个）
	const buttonsPerRow = 2
	for i := 0; i < displayCount; i++ {
		if i%buttonsPerRow == 0 {
			rows = append(rows, []callback.Button{})
		}

		query := history[i].Query
		// Limit query length for button text
		buttonText := fmt.Sprintf("%d. %s", i+1, truncateText(query, 12))

		// Build callback with escape
		callbackData := fmt.Sprintf("search:hist:%d", i)

		rows[len(rows)-1] = append(rows[len(rows)-1], callback.Button{
			Text:         buttonText,
			CallbackData: callbackData,
		})
	}

	// 操作按钮行 - 只有统计按钮
	actionRow := []callback.Button{
		{Text: "📊 查看统计", CallbackData: "search_stats"},
	}
	rows = append(rows, actionRow)

	// 管理按钮行
	manageRow := []callback.Button{}
	if len(history) > 0 {
		manageRow = append(manageRow, callback.Button{
			Text:         "🗑️ 清空历史",
			CallbackData: "search_clear_all",
		})
	}
	manageRow = append(manageRow, callback.Button{
		Text:         "⚙️ 管理历史",
		CallbackData: "search_manage",
	})
	rows = append(rows, manageRow)

	// 返回按钮
	rows = append(rows, []callback.Button{
		{Text: "⬅️ 返回", CallbackData: "start"},
	})

	return &callback.Keyboard{InlineKeyboard: rows}
}

// BuildPopularSearchesUI 构建热门搜索界面
func (b *HistoryBuilder) BuildPopularSearchesUI(popular []services.PopularSearch, allTime bool) string {
	var sb strings.Builder

	title := "🔥 热门搜索"
	if allTime {
		title = "🏆 历史热门"
	}

	sb.WriteString(fmt.Sprintf("%s\n\n", title))
	sb.WriteString("✨ 大家都在搜\n\n")

	for i, item := range popular {
		countText := fmt.Sprintf("%d次", item.Count)
		sb.WriteString(fmt.Sprintf("%d. %s · %s\n", i+1, item.Query, countText))
	}

	return sb.String()
}

// BuildPopularSearchesKeyboard 构建热门搜索键盘
func (b *HistoryBuilder) BuildPopularSearchesKeyboard(popular []services.PopularSearch) *callback.Keyboard {
	var rows [][]callback.Button

	// 切换按钮
	toggleRow := []callback.Button{
		{Text: "🔥 本周热门", CallbackData: "popular_week"},
		{Text: "🏆 历史热门", CallbackData: "popular_all"},
	}
	rows = append(rows, toggleRow)

	// 搜索按钮（每行2个）
	const buttonsPerRow = 2
	displayCount := len(popular)
	if displayCount > 10 {
		displayCount = 10
	}

	for i := 0; i < displayCount; i++ {
		if i%buttonsPerRow == 0 {
			rows = append(rows, []callback.Button{})
		}

		query := popular[i].Query
		buttonText := fmt.Sprintf("%d. %s", i+1, truncateText(query, 12))
		callbackData := fmt.Sprintf("search:hist:%d", i)

		rows[len(rows)-1] = append(rows[len(rows)-1], callback.Button{
			Text:         buttonText,
			CallbackData: callbackData,
		})
	}

	// 返回按钮
	rows = append(rows, []callback.Button{
		{Text: "🔄 刷新", CallbackData: "search_popular_refresh"},
		{Text: "⬅️ 返回", CallbackData: "search_history_menu"},
	})

	return &callback.Keyboard{InlineKeyboard: rows}
}

// BuildTrendsUI 构建搜索趋势界面
func (b *HistoryBuilder) BuildTrendsUI(trends []services.TrendItem, days int) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("📈 搜索趋势 · %d天\n\n", days))

	if len(trends) == 0 {
		sb.WriteString("💫 暂无趋势数据\n")
		return sb.String()
	}

	sb.WriteString("✨ 增长最快的搜索\n\n")

	for i, item := range trends {
		if i >= 10 {
			break
		}

		growthText := ""
		if item.Growth > 0 {
			growthText = fmt.Sprintf("🔼 +%.0f%%", item.Growth)
		} else if item.Growth < 0 {
			growthText = fmt.Sprintf("🔽 %.0f%%", item.Growth)
		} else {
			growthText = "➡️ 持平"
		}

		sb.WriteString(fmt.Sprintf("%d. %s %s\n", i+1, item.Query, growthText))
		sb.WriteString(fmt.Sprintf("   %d次 (昨日%d)\n\n", item.Count, item.Yesterday))
	}

	return sb.String()
}

// BuildTrendsKeyboard 构建搜索趋势键盘
func (b *HistoryBuilder) BuildTrendsKeyboard(days int) *callback.Keyboard {
	var rows [][]callback.Button

	// 时间范围选择
	timeRow := []callback.Button{
		{Text: "3天", CallbackData: "search_trends:3"},
		{Text: "7天", CallbackData: "search_trends:7"},
		{Text: "30天", CallbackData: "search_trends:30"},
	}
	rows = append(rows, timeRow)

	// 返回按钮
	rows = append(rows, []callback.Button{
		{Text: "🔄 刷新", CallbackData: "search_trends_refresh"},
		{Text: "⬅️ 返回", CallbackData: "search_history_menu"},
	})

	return &callback.Keyboard{InlineKeyboard: rows}
}

// BuildManageHistoryUI 构建管理历史界面
func (b *HistoryBuilder) BuildManageHistoryUI(history []services.SearchEntry) string {
	var sb strings.Builder

	sb.WriteString("⚙️ 管理搜索历史\n\n")
	sb.WriteString("选择要删除的搜索记录\n\n")

	displayCount := len(history)
	if displayCount > 10 {
		displayCount = 10
	}

	if displayCount == 0 {
		sb.WriteString("暂无历史记录\n")
		return sb.String()
	}

	for i := 0; i < displayCount; i++ {
		entry := history[i]

		countText := ""
		if entry.Count > 1 {
			countText = fmt.Sprintf("×%d", entry.Count)
		}

		timeText := formatTimeAgo(entry.Timestamp)

		sb.WriteString(fmt.Sprintf("%d. %s%s · %s\n", i+1, entry.Query, countText, timeText))
	}

	return sb.String()
}

// BuildManageHistoryKeyboard 构建管理历史键盘
func (b *HistoryBuilder) BuildManageHistoryKeyboard(history []services.SearchEntry, page, totalPages int) *callback.Keyboard {
	var rows [][]callback.Button

	// 删除按钮（每行2个）
	const buttonsPerRow = 2
	displayCount := len(history)
	if displayCount > 10 {
		displayCount = 10
	}

	for i := 0; i < displayCount; i++ {
		if i%buttonsPerRow == 0 {
			rows = append(rows, []callback.Button{})
		}

		buttonText := fmt.Sprintf("🗑️ %d", i+1)
		callbackData := fmt.Sprintf("search_delete:%d", i)

		rows[len(rows)-1] = append(rows[len(rows)-1], callback.Button{
			Text:         buttonText,
			CallbackData: callbackData,
		})
	}

	// 分页按钮
	if totalPages > 1 {
		paginationRow := []callback.Button{}

		if page > 1 {
			paginationRow = append(paginationRow, callback.Button{
				Text:         "⬅️ 上一页",
				CallbackData: fmt.Sprintf("search_manage:%d", page-1),
			})
		}

		paginationRow = append(paginationRow, callback.Button{
			Text:         fmt.Sprintf("%d/%d", page, totalPages),
			CallbackData: fmt.Sprintf("search_manage:%d", page),
		})

		if page < totalPages {
			paginationRow = append(paginationRow, callback.Button{
				Text:         "下一页 ➡️",
				CallbackData: fmt.Sprintf("search_manage:%d", page+1),
			})
		}

		rows = append(rows, paginationRow)
	}

	// 全部清空按钮
	clearRow := []callback.Button{
		{Text: "🗑️ 清空全部", CallbackData: "search_clear_all"},
		{Text: "⬅️ 返回", CallbackData: "search_history_menu"},
	}
	rows = append(rows, clearRow)

	return &callback.Keyboard{InlineKeyboard: rows}
}

// BuildStatsUI 构建统计界面
func (b *HistoryBuilder) BuildStatsUI(stats *services.SearchStats, userID int64) string {
	var sb strings.Builder

	sb.WriteString("📊 搜索统计\n\n")

	// 统计数据
	if stats != nil {
		sb.WriteString(fmt.Sprintf("📊 总计 %d次 · 本周 %d次 · 本月 %d次\n\n", stats.Total, stats.Week, stats.Month))
	}

	// 热门搜索
	if len(stats.Top5) > 0 {
		sb.WriteString("🔥 常搜 TOP5\n")
		for i, query := range stats.Top5 {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, query))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// BuildStatsKeyboard 构建统计键盘
func (b *HistoryBuilder) BuildStatsKeyboard() *callback.Keyboard {
	return &callback.Keyboard{
		InlineKeyboard: [][]callback.Button{
			{
				{Text: "🔥 热门搜索", CallbackData: "search_popular"},
				{Text: "📈 搜索趋势", CallbackData: "search_trends:7"},
			},
			{
				{Text: "🔄 刷新统计", CallbackData: "search_stats"},
				{Text: "⬅️ 返回", CallbackData: "search_history_menu"},
			},
		},
	}
}

// 辅助函数

// formatTimeAgo 格式化时间为"多久前"
func formatTimeAgo(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	if diff.Hours() < 1 {
		return "刚刚"
	} else if diff.Hours() < 24 {
		hours := int(diff.Hours())
		if hours == 1 {
			return "1小时前"
		}
		return fmt.Sprintf("%d小时前", hours)
	} else if diff.Hours() < 24*7 {
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1天前"
		}
		return fmt.Sprintf("%d天前", days)
	} else if diff.Hours() < 24*30 {
		weeks := int(diff.Hours() / (24 * 7))
		if weeks == 1 {
			return "1周前"
		}
		return fmt.Sprintf("%d周前", weeks)
	} else {
		months := int(diff.Hours() / (24 * 30))
		if months == 1 {
			return "1个月前"
		}
		return fmt.Sprintf("%d个月前", months)
	}
}

// escapeString 转义字符串用于 callback data
func escapeString(s string) string {
	s = strings.ReplaceAll(s, ":", "\\x3A")
	s = strings.ReplaceAll(s, ",", "\\x2C")
	return s
}
