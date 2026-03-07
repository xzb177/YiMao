package ui

import (
	"fmt"
	"strings"

	"emby-telegram-bot/internal/services"
)

// MediaDetailBuilder 优化后的媒体详情页构建器
type MediaDetailBuilder struct {
	style UIStyle
}

// NewMediaDetailBuilder 创建媒体详情构建器
func NewMediaDetailBuilder(style UIStyle) *MediaDetailBuilder {
	return &MediaDetailBuilder{style: style}
}

// BuildMediaDetailMessage 构建媒体详情消息
func (b *MediaDetailBuilder) BuildMediaDetailMessage(info *services.MediaInfo) string {
	switch b.style {
	case StyleNeon:
		return b.buildNeonMediaDetail(info)
	case StyleFilm:
		return b.buildFilmMediaDetail(info)
	case StylePop:
		return b.buildPopMediaDetail(info)
	case StyleCard:
		return b.buildCardMediaDetail(info)
	default:
		return b.buildCardMediaDetail(info) // 默认使用极简卡片风
	}
}

// buildNeonMediaDetail 暗黑霓虹风格媒体详情
func (b *MediaDetailBuilder) buildNeonMediaDetail(info *services.MediaInfo) string {
	var sb strings.Builder

	// 确定媒体类型
	isTV := info.Type == services.MediaTypeTV
	typeIcon := "🎬"
	typeLabel := "电影"
	if isTV {
		typeIcon = "📺"
		typeLabel = "剧集"
	}

	// 标题分隔线
	sb.WriteString(neonSeparator + "\n")
	sb.WriteString(fmt.Sprintf("%s %s\n", typeIcon, info.Title))
	if info.OriginalTitle != "" && info.OriginalTitle != info.Title {
		sb.WriteString(fmt.Sprintf("   %s\n", info.OriginalTitle))
	}
	sb.WriteString(neonSeparator + "\n\n")

	// 元信息行
	sb.WriteString(fmt.Sprintf("📊 热度 %d  ·  ⭐ 评分 %.1f  ·  🎬 类型 %s\n\n",
		info.Popularity, info.Rating, typeLabel))

	// 时长
	if info.Runtime > 0 {
		sb.WriteString(fmt.Sprintf("⏱️ 时长: %d 分钟\n\n", info.Runtime))
	}

	// 分隔线
	sb.WriteString(neonSeparator + "\n\n")

	// 概要
	if info.Overview != "" {
		sb.WriteString("📖 剧情简介\n\n")
		overview := formatOverview(info.Overview)
		sb.WriteString(fmt.Sprintf("  %s\n\n", overview))
	}

	// 分隔线
	sb.WriteString(neonSeparator + "\n")

	// 类型标签
	if len(info.Genres) > 0 {
		sb.WriteString(fmt.Sprintf("🏷️ %s\n", strings.Join(info.Genres, " · ")))
	}

	// 发布日期
	if info.ReleaseDate != "" {
		sb.WriteString(fmt.Sprintf("\n📅 发布: %s", info.ReleaseDate))
	}

	// TMDB ID
	sb.WriteString(fmt.Sprintf("\n🆔 TMDB ID: %d", info.ID))

	return sb.String()
}

// buildFilmMediaDetail 文艺胶片风格媒体详情
func (b *MediaDetailBuilder) buildFilmMediaDetail(info *services.MediaInfo) string {
	var sb strings.Builder

	// 确定媒体类型
	isTV := info.Type == services.MediaTypeTV
	typeIcon := "🎬"
	if isTV {
		typeIcon = "📺"
	}

	// 标题分隔线
	sb.WriteString(filmSeparator + "\n")
	sb.WriteString(fmt.Sprintf("%s %s\n", typeIcon, info.Title))
	if info.OriginalTitle != "" && info.OriginalTitle != info.Title {
		sb.WriteString(fmt.Sprintf("%s\n", info.OriginalTitle))
	}
	sb.WriteString(filmSeparator + "\n\n")

	// 元信息行
	sb.WriteString(fmt.Sprintf("⭐ %.1f 分  ·  📅 %s\n\n", info.Rating, getMediaTypeLabel(info.Type)))

	// 发布日期
	if info.ReleaseDate != "" {
		sb.WriteString(fmt.Sprintf("📅 上映日期: %s\n", info.ReleaseDate))
	}

	// 时长
	if info.Runtime > 0 {
		sb.WriteString(fmt.Sprintf("⏱️ 时长: %d 分钟\n", info.Runtime))
	}

	sb.WriteString("\n")
	sb.WriteString(filmSeparator + "\n\n")

	// 概要（文艺风格）
	if info.Overview != "" {
		sb.WriteString("「剧情简介」\n\n")
		paragraphs := strings.Split(info.Overview, "\n\n")
		for _, para := range paragraphs {
			if strings.TrimSpace(para) != "" {
				sb.WriteString(fmt.Sprintf("   %s\n\n", wrapText(para, 28)))
			}
		}
	}

	sb.WriteString(filmSeparator + "\n\n")

	// 类型标签
	if len(info.Genres) > 0 {
		sb.WriteString(fmt.Sprintf("🏷️ %s\n", strings.Join(info.Genres, " / ")))
	}

	// 添加一句推荐理由
	sb.WriteString("\n")
	sb.WriteString("♪ 推荐理由\n")
	sb.WriteString(fmt.Sprintf("   %s\n\n", getRecommendationReason(info.Rating)))

	// TMDB ID
	sb.WriteString(fmt.Sprintf("🆔 TMDB ID: %d", info.ID))

	return sb.String()
}

// buildPopMediaDetail 波普艺术风格媒体详情
func (b *MediaDetailBuilder) buildPopMediaDetail(info *services.MediaInfo) string {
	var sb strings.Builder

	// 确定媒体类型
	isTV := info.Type == services.MediaTypeTV
	typeIcon := "🎬"
	if isTV {
		typeIcon = "📺"
	}

	// 标题分隔线
	sb.WriteString(popSeparator + "\n")
	sb.WriteString(fmt.Sprintf("%s %s\n", typeIcon, info.Title))
	if info.OriginalTitle != "" && info.OriginalTitle != info.Title {
		sb.WriteString(fmt.Sprintf("%s\n", info.OriginalTitle))
	}
	sb.WriteString(popSeparator + "\n\n")

	// 元信息行
	sb.WriteString(fmt.Sprintf("📊 评分 %.1f  ·  🎬 %s\n", info.Rating, getMediaTypeLabel(info.Type)))

	// 发布日期
	if info.ReleaseDate != "" {
		sb.WriteString(fmt.Sprintf("📅 日期 %s\n", info.ReleaseDate))
	}

	// 时长
	if info.Runtime > 0 {
		sb.WriteString(fmt.Sprintf("⏱️ 时长 %d 分钟\n", info.Runtime))
	}

	sb.WriteString("\n")
	sb.WriteString(popLine + "\n\n")

	// 概要
	if info.Overview != "" {
		sb.WriteString("「剧情」\n\n")
		paragraphs := strings.Split(info.Overview, "\n\n")
		for _, para := range paragraphs {
			if strings.TrimSpace(para) != "" {
				sb.WriteString(fmt.Sprintf("   %s\n\n", wrapText(para, 30)))
			}
		}
	}

	sb.WriteString(popLine + "\n\n")

	// 类型标签
	if len(info.Genres) > 0 {
		sb.WriteString(fmt.Sprintf("🏷️ %s\n", strings.Join(info.Genres, " · ")))
	}

	// TMDB ID
	sb.WriteString(fmt.Sprintf("\n🆔 TMDB ID: %d", info.ID))

	return sb.String()
}

// buildCardMediaDetail 极简卡片风格媒体详情
func (b *MediaDetailBuilder) buildCardMediaDetail(info *services.MediaInfo) string {
	var sb strings.Builder

	// 确定媒体类型
	isTV := info.Type == services.MediaTypeTV
	typeIcon := "🎬"
	typeLabel := "电影"
	if isTV {
		typeIcon = "📺"
		typeLabel = "剧集"
	}

	// 标题分隔线
	sb.WriteString(cardSeparator + "\n")
	sb.WriteString(fmt.Sprintf("%s %s\n", typeIcon, info.Title))
	if info.OriginalTitle != "" && info.OriginalTitle != info.Title {
		sb.WriteString(fmt.Sprintf("%s\n", info.OriginalTitle))
	}
	sb.WriteString(cardSeparator + "\n\n")

	// 信息卡片
	sb.WriteString(cardBoxStart + "\n")
	sb.WriteString(fmt.Sprintf("  📊 评分: %.1f\n", info.Rating))
	sb.WriteString(fmt.Sprintf("  🎬 类型: %s\n", getMediaTypeLabel(info.Type)))

	if info.ReleaseDate != "" {
		sb.WriteString(fmt.Sprintf("  📅 日期: %s\n", info.ReleaseDate))
	}

	if info.Runtime > 0 {
		sb.WriteString(fmt.Sprintf("  ⏱️ 时长: %d 分钟\n", info.Runtime))
	}

	sb.WriteString(cardBoxEnd + "\n\n")

	// 概要卡片
	if info.Overview != "" {
		sb.WriteString(cardBoxStart + "\n")
		sb.WriteString("  📖 剧情简介\n")
		sb.WriteString(cardSeparator + "\n")
		sb.WriteString(fmt.Sprintf("  %s\n", wrapText(info.Overview, 26)))
		sb.WriteString(cardBoxEnd + "\n\n")
	}

	// 类型标签
	if len(info.Genres) > 0 {
		sb.WriteString(fmt.Sprintf("🏷️ %s\n", strings.Join(info.Genres, " / ")))
	}

	sb.WriteString(fmt.Sprintf("🆔 TMDB ID: %d", info.ID))

	return sb.String()
}

// BuildMediaDetailKeyboard 构建媒体详情键盘
func (b *MediaDetailBuilder) BuildMediaDetailKeyboard(info *services.MediaInfo, hasSeasons bool, hasRequests bool) *DetailKeyboard {
	kb := &DetailKeyboard{
		Buttons: make([][]DetailButton, 0),
	}

	// 确定媒体类型
	isTV := info.Type == services.MediaTypeTV

	// 操作按钮行
	actionRow := []DetailButton{}

	// 求片/订阅按钮
	if isTV {
		actionRow = append(actionRow, DetailButton{
			Text:         "✅ 立即求片",
			CallbackData: fmt.Sprintf("request:id:%d:type:tv:season:0", info.ID),
		})
		actionRow = append(actionRow, DetailButton{
			Text:         "📺 添加订阅",
			CallbackData: fmt.Sprintf("subscribe:id:%d:type:tv", info.ID),
		})
	} else {
		actionRow = append(actionRow, DetailButton{
			Text:         "✅ 立即求片",
			CallbackData: fmt.Sprintf("request:id:%d:type:movie", info.ID),
		})
	}

	kb.Buttons = append(kb.Buttons, actionRow)

	// 导航按钮行
	navRow := []DetailButton{
		{Text: "⬅️ 返回", CallbackData: "back"},
		{Text: "🏠 主菜单", CallbackData: "start"},
	}

	// 如果是剧集且有更多季，添加查看更多按钮
	if isTV && hasSeasons {
		navRow = append(navRow, DetailButton{
			Text:         "📺 查看所有季",
			CallbackData: fmt.Sprintf("detail_seasons:id:%d", info.ID),
		})
	}

	kb.Buttons = append(kb.Buttons, navRow)

	return kb
}

// DetailKeyboard 详情键盘
type DetailKeyboard struct {
	Buttons [][]DetailButton
}

// DetailButton 详情按钮
type DetailButton struct {
	Text         string
	CallbackData string
}

// 辅助函数：格式化概要
func formatOverview(text string) string {
	// 移除多余空行
	lines := strings.Split(text, "\n")
	var formatted strings.Builder
	emptyCount := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			emptyCount++
			if emptyCount > 1 {
				continue // 跳过多余的空行
			}
		} else {
			emptyCount = 0
		}
		formatted.WriteString(trimmed)
		if len(lines) > 1 {
			formatted.WriteString("\n  ")
		}
	}

	return strings.TrimRight(formatted.String(), "  \n")
}

// 辅助函数：获取推荐理由
func getRecommendationReason(rating float64) string {
	reasons := []string{
		"这部电影，值得你花两个小时去感受。",
		"有些故事，会悄悄改变你看待世界的方式。",
		"评分不错，值得一看。",
		"一个值得收藏的故事。",
	}

	if rating >= 8.0 {
		return "高分作品，值得你细细品味。"
	}
	if rating >= 7.0 {
		return "值得一看的作品。"
	}

	if len(reasons) > 0 {
		return reasons[0]
	}
	return "一部值得一看的影片。"
}
