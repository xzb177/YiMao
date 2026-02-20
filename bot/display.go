package bot

import (
	"fmt"
	"strings"

	"emby-telegram-bot/callback"
	"emby-telegram-bot/session"
)

// DisplayBuilder handles building beautiful message displays
type DisplayBuilder struct {
	callbackParser *callback.CallbackParser
}

// NewDisplayBuilder creates a new display builder
func NewDisplayBuilder(cp *callback.CallbackParser) *DisplayBuilder {
	return &DisplayBuilder{
		callbackParser: cp,
	}
}

// Emoji constants for consistent theming
const (
	EmojiSearch     = "🔍"
	EmojiList       = "📋"
	EmojiSuccess    = "✅"
	EmojiError      = "❌"
	EmojiInfo       = "ℹ️"
	EmojiWarning    = "⚠️"
	EmojiStar       = "⭐"
	EmojiTV         = "📺"
	EmojiFilm       = "🎬"
	EmojiCalendar   = "📅"
	EmojiClock      = "⏱️"
	EmojiChart      = "📊"
	EmojiSparkles   = "✨"
	EmojiFire       = "🔥"
	EmojiArrowRight = "▸"
	EmojiBox        = "▢"
)

// BuildSearchResultsMessage creates a beautiful search results display
func (d *DisplayBuilder) BuildSearchResultsMessage(sess *session.UserSession, result *SearchResult) *MessageResponse {
	var text strings.Builder

	// ========== Header Section ==========
	totalPages := (result.Total + 7) / 8

	// Top border with title
	text.WriteString("┌─── 🔍 搜索结果 ────┐\n\n")

	// Search query
	text.WriteString(fmt.Sprintf("  关键词: 「%s」\n", sess.SearchQuery))

	// Page info with emoji
	text.WriteString(fmt.Sprintf("  📄 第 %d/%d 页  ·  共 %d 条结果\n",
		sess.CurrentPage+1, totalPages, result.Total))

	// ========== Separator ==========
	text.WriteString("\n  ━━━━━━━━━━━━━━━  \n\n")

	// ========== Results List ==========
	startIdx := sess.CurrentPage * 8
	for i, item := range result.Items {
		num := startIdx + i + 1

		// Type emoji
		typeEmoji := EmojiFilm
		if item.Type == "tv" {
			typeEmoji = EmojiTV
		}

		// Number emoji
		numEmoji := d.getNumberEmoji(i)

		// Build each result - no HTML tags
		text.WriteString(fmt.Sprintf("  %s %d. %s", numEmoji, num, item.Title))

		// Year in parentheses
		if item.Year > 0 {
			text.WriteString(fmt.Sprintf(" (%d)", item.Year))
		}

		// Rating
		if item.Rating > 0 {
			text.WriteString(fmt.Sprintf("  %s%.1f", EmojiStar, item.Rating))
		}

		// Type indicator
		text.WriteString(fmt.Sprintf(" %s", typeEmoji))

		text.WriteString("\n")
	}

	// Bottom border
	text.WriteString("\n└──────────────────────┘")

	// ========== Build Keyboard ==========
	keyboard := d.buildSearchKeyboard(sess, result, totalPages)

	return &MessageResponse{
		Text:     text.String(),
		Keyboard: keyboard,
		EditMode: sess.LastMessageID > 0,
	}
}

// getNumberEmoji returns decorative emoji for list numbering
func (d *DisplayBuilder) getNumberEmoji(index int) string {
	emojis := []string{"🔸", "🔹", "🌟", "✨", "💫", "⭐", "🌙", "☀️"}
	return emojis[index%len(emojis)]
}

// BuildItemDetailsMessage creates a beautiful item details display
func (d *DisplayBuilder) BuildItemDetailsMessage(
	sess *session.UserSession,
	item *session.SearchItem,
	quota *QuotaInfo,
) *MessageResponse {
	var text strings.Builder

	// ========== Title Section ==========
	displayTitle := item.Title
	if displayTitle == "" {
		displayTitle = fmt.Sprintf("媒体 #%s", item.ID)
	}

	// Type emoji for header
	headerEmoji := EmojiFilm
	if item.Type == "tv" {
		headerEmoji = EmojiTV
	}

	// Top border
	text.WriteString("┌─ ✨ 详情 ─────────┐\n\n")

	// Title with year - no HTML tags
	text.WriteString(fmt.Sprintf("  %s %s", headerEmoji, displayTitle))
	if item.Year > 0 {
		text.WriteString(fmt.Sprintf(" · %d年", item.Year))
	}
	text.WriteString("\n\n")

	// ========== Rating & Status Bar ==========
	text.WriteString("  ━━━━━━━━━━━━━━━  \n\n")

	// First row: Rating with stars
	if item.Rating > 0 {
		ratingStars := d.getRatingStars(item.Rating)
		text.WriteString(fmt.Sprintf("  %s 评分 %.1f %s\n", EmojiStar, item.Rating, ratingStars))
	} else {
		text.WriteString(fmt.Sprintf("  %s 暂无评分 ☆☆☆☆☆\n", EmojiStar))
	}

	// Second row: Media Type
	mediaType := d.getMediaTypeLabel(item.Type)
	text.WriteString(fmt.Sprintf("  %s %s", EmojiBox, mediaType))

	// Add media ID for reference
	text.WriteString(fmt.Sprintf(" · ID: %s\n", item.ID))

	// Third row: Year and other info
	infoParts := []string{}
	if item.Year > 0 {
		infoParts = append(infoParts, fmt.Sprintf("%d年", item.Year))
	}
	if item.Runtime > 0 {
		hours := item.Runtime / 60
		mins := item.Runtime % 60
		if hours > 0 {
			infoParts = append(infoParts, fmt.Sprintf("%d时%d分", hours, mins))
		} else {
			infoParts = append(infoParts, fmt.Sprintf("%d分钟", mins))
		}
	}
	// Add release date if available
	if item.ReleaseDate != "" && item.Year == 0 {
		infoParts = append(infoParts, item.ReleaseDate)
	}

	if len(infoParts) > 0 {
		text.WriteString(fmt.Sprintf("  %s %s\n", EmojiCalendar, strings.Join(infoParts, " · ")))
	} else {
		text.WriteString("\n")
	}

	// ========== Genres ==========
	if len(item.Genres) > 0 {
		text.WriteString("\n  🎬 ")
		for i, genre := range item.Genres {
			if i > 0 {
				text.WriteString(" · ")
			}
			text.WriteString(genre)
			if i >= 2 {
				text.WriteString(" ...")
				break
			}
		}
		text.WriteString("\n")
	}

	// ========== Seasons (for TV shows) ==========
	if len(item.Seasons) > 0 {
		text.WriteString(fmt.Sprintf("\n  📺 季数: %v\n", item.Seasons))
	}

	// ========== Status if available ==========
	if item.Status != "" {
		text.WriteString(fmt.Sprintf("\n  📡 状态: %s\n", item.Status))
	}

	// ========== Quota Status ==========
	if quota != nil {
		text.WriteString(d.formatQuotaStatusCompact(item.Type, quota))
	}

	// ========== Overview ==========
	if item.Overview != "" {
		text.WriteString("\n\n  ┌─ 剧情简介 ─────┐\n")
		text.WriteString(d.formatOverviewCompact(item.Overview))
		text.WriteString("\n  └─────────────────┘")
	}

	// Bottom border
	text.WriteString("\n\n└────────────────────┘")

	// ========== Build Keyboard ==========
	keyboard := d.buildDetailsKeyboard(sess, item, quota)

	return &MessageResponse{
		Text:     text.String(),
		Keyboard: keyboard,
		EditMode: true,
	}
}

// getRatingStars converts rating to star display
func (d *DisplayBuilder) getRatingStars(rating float64) string {
	stars := rating / 2.0 // Convert 10-point to 5-point scale
	fullStars := int(stars)
	result := strings.Repeat("★", fullStars)
	if stars-float64(fullStars) >= 0.5 {
		result += "☆"
	}
	emptyStars := 5 - len([]rune(result))
	result += strings.Repeat("☆", emptyStars)
	return result
}

// formatQuotaStatusCompact formats quota in compact style
func (d *DisplayBuilder) formatQuotaStatusCompact(mediaType string, quota *QuotaInfo) string {
	if quota == nil {
		return ""
	}

	var remaining, limit int
	if mediaType == "tv" {
		remaining = quota.TVLimit - quota.TVUsed
		limit = quota.TVLimit
	} else {
		remaining = quota.MovieLimit - quota.MovieUsed
		limit = quota.MovieLimit
	}

	// Build progress bar
	barWidth := 10
	filled := (limit - remaining) * barWidth / limit
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	status := fmt.Sprintf("\n\n  📊 今日配额\n  %s %d/%d", bar, remaining, limit)

	if remaining == 0 {
		status += " 已用完"
	} else if remaining == 1 {
		status += " 最后1个"
	}

	return status
}

// formatOverviewCompact formats overview with word wrap
func (d *DisplayBuilder) formatOverviewCompact(overview string) string {
	maxWidth := 28 // Characters per line
	words := strings.Fields(overview)
	var lines []string
	currentLine := ""

	for _, word := range words {
		testLine := currentLine
		if testLine != "" {
			testLine += " "
		}
		testLine += word

		if len(testLine) <= maxWidth {
			currentLine = testLine
		} else {
			if currentLine != "" {
				lines = append(lines, "  │ "+currentLine+strings.Repeat(" ", maxWidth-len(currentLine))+" │")
			}
			currentLine = word
		}
	}
	if currentLine != "" {
		lines = append(lines, "  │ "+currentLine+strings.Repeat(" ", maxWidth-len(currentLine))+" │")
	}

	if len(lines) > 4 {
		lines = lines[:4]
		lines[3] = "  │ "+strings.Repeat(" ", 26)+"... │"
	}

	return strings.Join(lines, "\n")
}

// getMediaTypeEmoji returns emoji for media type
func (d *DisplayBuilder) getMediaTypeEmoji(mediaType string) string {
	switch mediaType {
	case "movie":
		return EmojiFilm
	case "tv":
		return EmojiTV
	default:
		return ""
	}
}

// getMediaTypeLabel returns Chinese label for media type
func (d *DisplayBuilder) getMediaTypeLabel(mediaType string) string {
	switch mediaType {
	case "movie":
		return "电影"
	case "tv":
		return "剧集"
	default:
		return "电影/剧集"
	}
}

// QuotaInfo represents quota information for display
type QuotaInfo struct {
	MovieLimit int
	MovieUsed  int
	TVLimit    int
	TVUsed     int
}

// buildSearchKeyboard builds the keyboard for search results
func (d *DisplayBuilder) buildSearchKeyboard(sess *session.UserSession, result *SearchResult, totalPages int) [][]map[string]string {
	keyboard := [][]map[string]string{}

	// ========== Item Number Buttons (4 per row) ==========
	row := []string{}
	for i := range result.Items {
		num := i + 1
		row = append(row, fmt.Sprintf("%d", num))
		if len(row) == 4 {
			keyboard = append(keyboard, d.buildNumberButtonRow(row, sess))
			row = []string{}
		}
	}
	if len(row) > 0 {
		keyboard = append(keyboard, d.buildNumberButtonRow(row, sess))
	}

	// ========== Navigation Buttons ==========
	navRow := []map[string]string{}

	if sess.CurrentPage > 0 {
		navRow = append(navRow, map[string]string{
			"text":         "⬅️ 上一页",
			"callback_data": d.callbackParser.Format("page", fmt.Sprintf("%d", sess.CurrentPage)),
		})
	}

	// Page indicator
	pageIndicator := fmt.Sprintf("· %d/%d ·", sess.CurrentPage+1, totalPages)
	navRow = append(navRow, map[string]string{
		"text":         pageIndicator,
		"callback_data": "noop",
	})

	if sess.CurrentPage < totalPages-1 {
		navRow = append(navRow, map[string]string{
			"text":         "下一页➡️",
			"callback_data": d.callbackParser.Format("page", fmt.Sprintf("%d", sess.CurrentPage+2)),
		})
	}

	if len(navRow) > 0 {
		keyboard = append(keyboard, navRow)
	}

	// ========== Cancel Button ==========
	keyboard = append(keyboard, []map[string]string{{
		"text":         "❌ 关闭",
		"callback_data": d.callbackParser.Format("cancel", ""),
	}})

	return keyboard
}

// buildDetailsKeyboard builds the keyboard for item details
func (d *DisplayBuilder) buildDetailsKeyboard(sess *session.UserSession, item *session.SearchItem, quota *QuotaInfo) [][]map[string]string {
	keyboard := [][]map[string]string{}

	// Check quota availability
	hasQuota := true
	buttonText := "📋 发起请求"

	if quota != nil {
		if item.Type == "tv" {
			if quota.TVUsed >= quota.TVLimit {
				hasQuota = false
				buttonText = "🚫 配额已用完"
			}
		} else {
			if quota.MovieUsed >= quota.MovieLimit {
				hasQuota = false
				buttonText = "🚫 配额已用完"
			}
		}
	}

	displayTitle := item.Title
	if displayTitle == "" {
		displayTitle = item.ID
	}

	// ========== Action Buttons ==========
	if hasQuota {
		// Primary action button (full width)
		keyboard = append(keyboard, []map[string]string{{
			"text":         buttonText,
			"callback_data": d.callbackParser.FormatWithData("subscribe", map[string]string{
				"id":    item.ID,
				"title": displayTitle,
				"type":  item.Type,
			}),
		}})
	}

	// ========== Secondary Buttons ==========
	secondaryRow := []map[string]string{
		{"text": "⬅️ 返回列表", "callback_data": d.callbackParser.Format("back", "results")},
	}

	if !hasQuota {
		// When no quota, show info button
		secondaryRow = append(secondaryRow, map[string]string{
			"text":         "ℹ️ 配额说明",
			"callback_data": "quota_info",
		})
	}

	secondaryRow = append(secondaryRow, map[string]string{
		"text":         "❌ 关闭",
		"callback_data": d.callbackParser.Format("cancel", ""),
	})

	keyboard = append(keyboard, secondaryRow)

	return keyboard
}

// buildNumberButtonRow builds a row of number buttons
func (d *DisplayBuilder) buildNumberButtonRow(labels []string, sess *session.UserSession) []map[string]string {
	row := []map[string]string{}
	for _, label := range labels {
		row = append(row, map[string]string{
			"text":         label,
			"callback_data": d.callbackParser.FormatWithData("select", map[string]string{
				"index": label,
				"page":  fmt.Sprintf("%d", sess.CurrentPage+1),
			}),
		})
	}
	return row
}

// BuildNoResultsMessage creates a friendly "no results" message
func (d *DisplayBuilder) BuildNoResultsMessage(query string) *MessageResponse {
	var text strings.Builder

	text.WriteString("┌─── 🔍 搜索结果 ────┐\n\n")
	text.WriteString(fmt.Sprintf("  关键词: 「%s」\n\n", query))
	text.WriteString("  ━━━━━━━━━━━━━━━  \n\n")
	text.WriteString("  😕 未找到相关内容\n\n")
	text.WriteString("  💡 搜索建议：\n\n")
	text.WriteString("  • 检查拼写是否正确\n")
	text.WriteString("  • 尝试使用更简单的关键词\n")
	text.WriteString("  • 尝试使用影片的英文名\n")
	text.WriteString("  • 确认是否为较新上映的内容\n")
	text.WriteString("\n└──────────────────────┘")

	return &MessageResponse{
		Text: text.String(),
	}
}

// BuildErrorMessage creates a formatted error message
func (d *DisplayBuilder) BuildErrorMessage(title, message string) *MessageResponse {
	var text strings.Builder

	text.WriteString(fmt.Sprintf("%s %s\n\n", EmojiError, title))
	text.WriteString(message)

	return &MessageResponse{
		Text: text.String(),
	}
}

// BuildSuccessMessage creates a formatted success message
func (d *DisplayBuilder) BuildSuccessMessage(title, message string) *MessageResponse {
	var text strings.Builder

	text.WriteString(fmt.Sprintf("%s %s\n\n", EmojiSuccess, title))
	text.WriteString(message)

	return &MessageResponse{
		Text: text.String(),
	}
}

// BuildInfoMessage creates a formatted info message
func (d *DisplayBuilder) BuildInfoMessage(title, message string) *MessageResponse {
	var text strings.Builder

	text.WriteString(fmt.Sprintf("%s %s\n\n", EmojiInfo, title))
	text.WriteString(message)

	return &MessageResponse{
		Text: text.String(),
	}
}
