package bot

import (
	"fmt"
	"strings"

	"emby-telegram-bot/callback"
	"emby-telegram-bot/session"
)

// ============================================================
// UNIFIED DETAIL DISPLAY SYSTEM
// ============================================================
// A clean, standard media detail display system that follows
// universal design principles for readability and usability.
//
// Layout Rules (Strict):
// 1. Standard Header: 🎬 影片名称 (年份) + 评分：⭐ 8.5
// 2. Information List: Emoji-prefixed, one per line
// 3. Clean Description: Paragraph format, no over-decoration
// 4. Quota Display: Clear status text at bottom
// 5. Button Alignment: Standard 3-button layout
// ============================================================

// UnifiedDetailBuilder creates clean, standard media detail displays
type UnifiedDetailBuilder struct {
	callbackParser *callback.CallbackParser
}

// NewUnifiedDetailBuilder creates a new unified detail builder
func NewUnifiedDetailBuilder(cp *callback.CallbackParser) *UnifiedDetailBuilder {
	return &UnifiedDetailBuilder{
		callbackParser: cp,
	}
}

// UnifiedMediaDetail represents complete media information for display
type UnifiedMediaDetail struct {
	ID       string
	Title    string
	Year     int
	Type     string // "movie" or "tv"
	Rating   float64
	Genres   []string
	Overview string
	Runtime  int
}

// UnifiedQuotaInfo represents quota information for unified display
type UnifiedQuotaInfo struct {
	MovieRemaining int
	MovieLimit     int
	TVRemaining    int
	TVLimit        int
}

// UnifiedMessageResponse represents a response to be sent
type UnifiedMessageResponse struct {
	Text     string
	Keyboard [][]map[string]string
	EditMode bool
}

// BuildDetail creates a standard, clean media detail display
func (b *UnifiedDetailBuilder) BuildDetail(
	detail *UnifiedMediaDetail,
	quota *UnifiedQuotaInfo,
	sess *session.UserSession,
) *UnifiedMessageResponse {

	var text strings.Builder

	// ========== Standard Header ==========
	// 🎬 影片名称 (年份)
	text.WriteString(fmt.Sprintf("🎬 %s", detail.Title))
	if detail.Year > 0 {
		text.WriteString(fmt.Sprintf(" (%d)", detail.Year))
	}
	text.WriteString("\n")

	// 评分：⭐ 8.5
	if detail.Rating > 0 {
		text.WriteString(fmt.Sprintf("评分：⭐ %.1f", detail.Rating))
	}
	text.WriteString("\n\n")

	// ========== Information List ==========
	// • 📅 上映：2024
	// • ⏳ 时长：120分钟
	// • 🎭 类型：科幻 / 动作

	if detail.Year > 0 {
		text.WriteString(fmt.Sprintf("• 📅 上映：%d\n", detail.Year))
	}

	if detail.Runtime > 0 {
		hours := detail.Runtime / 60
		mins := detail.Runtime % 60
		if hours > 0 {
			text.WriteString(fmt.Sprintf("• ⏳ 时长：%d时%d分\n", hours, mins))
		} else {
			text.WriteString(fmt.Sprintf("• ⏳ 时长：%d分钟\n", mins))
		}
	}

	if len(detail.Genres) > 0 {
		genreList := detail.Genres
		if len(genreList) > 3 {
			genreList = genreList[:3]
		}
		text.WriteString(fmt.Sprintf("• 🎭 类型：%s\n", strings.Join(genreList, " / ")))
	}

	// ========== Clean Description ==========
	// 剧情简介直接作为段落显示
	if detail.Overview != "" {
		text.WriteString("\n")
		overview := detail.Overview
		if len(overview) > 200 {
			overview = overview[:197] + "..."
		}
		text.WriteString(overview)
		text.WriteString("\n")
	}

	// ========== Quota Display ==========
	// 今日配额：充足 / 余量：1/2
	text.WriteString("\n")
	if quota != nil {
		text.WriteString(b.formatQuotaText(detail.Type, quota))
	}

	// ========== Build Keyboard ==========
	// [ 确认请求 ] [ 返回列表 ]
	// [ 🐛 反馈 ]
	keyboard := b.buildKeyboard(detail, quota)

	return &UnifiedMessageResponse{
		Text:     text.String(),
		Keyboard: keyboard,
		EditMode: true,
	}
}

// formatQuotaText formats quota status in standard clear text
func (b *UnifiedDetailBuilder) formatQuotaText(mediaType string, quota *UnifiedQuotaInfo) string {
	if mediaType == "tv" {
		if quota.TVRemaining >= quota.TVLimit/2 {
			return "今日配额：充足"
		} else if quota.TVRemaining > 0 {
			return fmt.Sprintf("余量：%d/%d", quota.TVRemaining, quota.TVLimit)
		}
		return "今日配额：已用完"
	}

	// movie
	if quota.MovieRemaining >= quota.MovieLimit/2 {
		return "今日配额：充足"
	} else if quota.MovieRemaining > 0 {
		return fmt.Sprintf("余量：%d/%d", quota.MovieRemaining, quota.MovieLimit)
	}
	return "今日配额：已用完"
}

// buildKeyboard creates standard keyboard with aligned buttons
func (b *UnifiedDetailBuilder) buildKeyboard(
	detail *UnifiedMediaDetail,
	quota *UnifiedQuotaInfo,
) [][]map[string]string {

	keyboard := [][]map[string]string{}

	// Determine button state based on quota
	requestBtnText := "✅ 确认请求"
	requestBtnData := b.callbackParser.FormatWithData("request", map[string]string{
		"id":    detail.ID,
		"title": detail.Title,
		"type":  detail.Type,
	})

	// Check if quota is available
	quotaAvailable := true
	if detail.Type == "tv" {
		if quota != nil && quota.TVRemaining <= 0 {
			quotaAvailable = false
		}
	} else {
		if quota != nil && quota.MovieRemaining <= 0 {
			quotaAvailable = false
		}
	}

	if !quotaAvailable {
		requestBtnText = "🚫 配额已用完"
		requestBtnData = "quota_exhausted"
	}

	// First row: Main actions
	// [ 确认请求 ] [ 返回列表 ]
	mainRow := []map[string]string{
		{"text": requestBtnText, "callback_data": requestBtnData},
		{"text": "⬅️ 返回列表", "callback_data": "back_to_list"},
	}
	keyboard = append(keyboard, mainRow)

	// Second row: Feedback button
	feedbackRow := []map[string]string{
		{"text": "🐛 反馈", "callback_data": b.callbackParser.FormatWithData("feedback", map[string]string{
			"id":    detail.ID,
			"title": detail.Title,
			"type":  detail.Type,
		})},
	}
	keyboard = append(keyboard, feedbackRow)

	return keyboard
}

// BuildLoading creates a loading message
func (b *UnifiedDetailBuilder) BuildLoading(title string) *UnifiedMessageResponse {
	var text strings.Builder
	text.WriteString(fmt.Sprintf("⏳ 正在获取%s...\n\n请稍候", title))

	return &UnifiedMessageResponse{
		Text:     text.String(),
		EditMode: false,
	}
}

// BuildSuccess creates a success message after request
func (b *UnifiedDetailBuilder) BuildSuccess(title string, requestID string) *UnifiedMessageResponse {
	var text strings.Builder
	text.WriteString("✅ 请求成功！\n\n")
	text.WriteString(fmt.Sprintf("📋 《%s》", title))
	text.WriteString("\n\n")
	text.WriteString(fmt.Sprintf("请求 ID: %s", requestID))
	text.WriteString("\n\n")
	text.WriteString("管理员批准后会通知你")

	keyboard := [][]map[string]string{
		{
			{"text": "⬅️ 返回列表", "callback_data": "back_to_list"},
		},
	}

	return &UnifiedMessageResponse{
		Text:     text.String(),
		Keyboard: keyboard,
		EditMode: true,
	}
}

// BuildError creates an error message
func (b *UnifiedDetailBuilder) BuildError(title, errorMsg string) *UnifiedMessageResponse {
	var text strings.Builder
	text.WriteString("❌ 请求失败\n\n")
	if title != "" {
		text.WriteString(fmt.Sprintf("📋 《%s》\n\n", title))
	}
	text.WriteString(errorMsg)

	keyboard := [][]map[string]string{
		{
			{"text": "⬅️ 返回列表", "callback_data": "back_to_list"},
			{"text": "❌ 关闭", "callback_data": "close_detail"},
		},
	}

	return &UnifiedMessageResponse{
		Text:     text.String(),
		Keyboard: keyboard,
		EditMode: true,
	}
}

// BuildNoResults creates a no results message
func (b *UnifiedDetailBuilder) BuildNoResults(query string) *UnifiedMessageResponse {
	var text strings.Builder
	text.WriteString("🔍 未找到相关内容\n\n")
	text.WriteString(fmt.Sprintf("搜索关键词：%s", query))
	text.WriteString("\n\n")
	text.WriteString("建议：")
	text.WriteString("\n• 检查输入是否正确")
	text.WriteString("\n• 尝试使用更简短的关键词")
	text.WriteString("\n• 尝试使用英文原名")

	return &UnifiedMessageResponse{
		Text:     text.String(),
		EditMode: true,
	}
}
