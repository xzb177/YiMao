package richmessage

import (
	"fmt"
	"strings"
)

// ============================================================

// PersonalityCardData 性格卡片数据
type PersonalityCardData struct {
	UserName    string
	Type        string
	TypeName    string
	Description string
	Dimensions  []PDimensionView
	TopTrait    string
}

// PDimensionView 性格维度视图
type PDimensionView struct {
	Name   string
	Left   string
	Right  string
	Score  float64
	Result string
	Icon   string
}

// BuildPersonalityCard 构建性格卡片

func buildDimensionBar(score float64) string {
	// 用 10 格进度条表示
	filled := int(score / 10)
	if filled > 10 {
		filled = 10
	}
	if filled < 0 {
		filled = 0
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)
}

// ============================================================
//  AI 解说员卡片
// ============================================================

// NarratorCardData 解说卡片数据
type NarratorCardData struct {
	Title       string
	Year        int
	Summary     string
	KeyPoints   []string
	Mood        string
	Similar     []string
	Rating      float64
	Genres      []string
	SpoilerMode bool
}

// BuildNarratorCard 构建解说卡片
func BuildNarratorCard(data NarratorCardData) RichMessage {
	builder := NewBuilder()

	// 标题行：电影名 + 年份 - 更有电影感
	builder.Heading(fmt.Sprintf("🎬 %s (%d)", data.Title, data.Year), 2)

	// 模式标签 + 评分 + 类型 - 更紧凑
	var meta []string
	if data.SpoilerMode {
		meta = append(meta, "🔥 剧透模式")
	} else {
		meta = append(meta, "📖 安全观看")
	}
	if data.Rating > 0 {
		meta = append(meta, fmt.Sprintf("⭐ %.1f", data.Rating))
	}
	if len(data.Genres) > 0 {
		meta = append(meta, strings.Join(data.Genres, " · "))
	}
	builder.BoldParagraph(strings.Join(meta, "  |  "))
	builder.Divider()

	// 剧情概要 - 更有故事感
	if data.Summary != "" {
		if len([]rune(data.Summary)) > 800 {
			data.Summary = string([]rune(data.Summary)[:800]) + "..."
		}
		builder.Paragraph(data.Summary)
	}

	// 关键看点 - 更有洞察
	if len(data.KeyPoints) > 0 {
		builder.BoldParagraph("💡 看点")
		for _, p := range data.KeyPoints {
			builder.Paragraph(fmt.Sprintf("  • %s", p))
		}
	}
	builder.Divider()

	// 适合心情 - 更有温度
	if data.Mood != "" {
		builder.Paragraph(fmt.Sprintf("🎭 **适合心情：** %s", data.Mood))
	}

	// 类似推荐 - 更有引导
	if len(data.Similar) > 0 {
		builder.BoldParagraph("🔗 类似推荐")
		for _, s := range data.Similar {
			builder.Paragraph(fmt.Sprintf("  • %s", s))
		}
	}

	return builder.Build()
}

// ============================================================
//  盲盒卡片
// ============================================================

// BlindBoxCardData 盲盒卡片数据
type BlindBoxCardData struct {
	Items []BlindBoxItemView
}

// BlindBoxItemView 盲盒物品视图
type BlindBoxItemView struct {
	Title    string
	Year     int
	Rating   float64
	Rarity   string
	Genres   string
	Overview string
	Revealed bool
}

// BuildBlindBoxCard 构建盲盒卡片
func BuildBlindBoxCard(data BlindBoxCardData) RichMessage {
	builder := NewBuilder()

	// 主标题 - 更有神秘感
	builder.Heading("🎰 电影盲盒", 2)

	// 判断是否已揭晓
	allRevealed := true
	for _, item := range data.Items {
		if !item.Revealed {
			allRevealed = false
			break
		}
	}

	if !allRevealed {
		builder.Italic(fmt.Sprintf("🎁 %d 个盲盒待揭晓 · 点击开盒", len(data.Items)))
	} else {
		// 已揭晓 - 更有仪式感
		maxRarity := "N"
		for _, item := range data.Items {
			if item.Rarity == "SSR" {
				maxRarity = "SSR"
			} else if item.Rarity == "SR" && maxRarity != "SSR" {
				maxRarity = "SR"
			} else if item.Rarity == "R" && maxRarity == "N" {
				maxRarity = "R"
			}
		}

		switch maxRarity {
		case "SSR":
			builder.BoldParagraph("🟡 恭喜！你开出了传说级SSR！")
		case "SR":
			builder.BoldParagraph("🟣 不错！你开出了稀有SR！")
		default:
			builder.BoldParagraph("揭晓结果：")
		}
		builder.Divider()

		for i, item := range data.Items {
			rarityIcon := map[string]string{
				"N": "⚪", "R": "🔵", "SR": "🟣", "SSR": "🟡",
			}[item.Rarity]
			if rarityIcon == "" {
				rarityIcon = "⚪"
			}

			builder.Paragraph(fmt.Sprintf("%s **盲盒 #%d** [%s %s]\n🎬 %s (%d)\n⭐ %.1f · %s",
				rarityIcon, i+1, item.Rarity, rarityIcon, item.Title, item.Year, item.Rating, item.Genres))
			if item.Overview != "" {
				overview := item.Overview
				if len([]rune(overview)) > 150 {
					overview = string([]rune(overview)[:150]) + "..."
				}
				builder.Details("📖 简介", overview, false)
			}
			if i < len(data.Items)-1 {
				builder.Divider()
			}
		}
	}

	return builder.Build()
}

// ============================================================
//  社交动态卡片
// ============================================================

// SocialFeedCardData 社交动态卡片数据
type SocialFeedCardData struct {
	Events []SocialEventView
}

// SocialEventView 动态事件视图
type SocialEventView struct {
	UserName  string
	EventType string
	Content   string
	TimeAgo   string
}

// BuildSocialFeedCard 构建社交动态卡片
func BuildSocialFeedCard(data SocialFeedCardData) RichMessage {
	builder := NewBuilder()

	// 主标题 - 更有社交感
	builder.Heading("📢 影友圈动态", 2)
	builder.Divider()

	if len(data.Events) == 0 {
		builder.Italic("还没有动态，快来写第一条影评吧！")
		return builder.Build()
	}

	// 动态列表 - 更有节奏感
	for _, e := range data.Events {
		icon := "📝"
		switch e.EventType {
		case "review":
			icon = "⭐"
		case "achievement":
			icon = "🏆"
		case "watch":
			icon = "🎬"
		case "contract":
			icon = "📜"
		case "challenge":
			icon = "🎯"
		}
		builder.Paragraph(fmt.Sprintf("%s **%s** %s\n🕐 %s", icon, e.UserName, e.Content, e.TimeAgo))
	}

	return builder.Build()
}

// ReviewCardData 影评卡片数据
type ReviewCardData struct {
	UserName  string
	MovieName string
	Rating    int
	Content   string
	Likes     int
	TimeAgo   string
}

// BuildReviewCard 构建单条影评卡片
func BuildReviewCard(data ReviewCardData) RichMessage {
	builder := NewBuilder()

	// 主标题 - 更有影评感
	builder.Heading(fmt.Sprintf("⭐ %s 的影评", data.UserName), 2)
	builder.BoldParagraph(fmt.Sprintf("《%s》", data.MovieName))
	builder.Divider()

	// 评分 - 更有视觉冲击
	builder.Paragraph(fmt.Sprintf("评分: %s", strings.Repeat("⭐", data.Rating)))
	builder.Paragraph(data.Content)
	builder.Divider()

	// 互动信息 - 更有社交感
	builder.Italic(fmt.Sprintf("❤️ %d 赞 · %s", data.Likes, data.TimeAgo))

	return builder.Build()
}

// ============================================================
//  轮盘卡片
// ============================================================

// RouletteCardData 轮盘卡片数据
type RouletteCardData struct {
	Title     string
	Year      int
	Rating    float64
	Overview  string
	SpinCount int
	MaxSpins  int
}

// BuildRouletteCard 构建轮盘卡片
func BuildRouletteCard(data RouletteCardData) RichMessage {
	builder := NewBuilder()

	// 主标题 - 更有轮盘感
	builder.Heading("🎡 命运轮盘", 2)
	builder.BoldParagraph(fmt.Sprintf("🎰 轮盘转动！今日第 %d/%d 次", data.SpinCount, data.MaxSpins))
	builder.Divider()

	// 电影信息 - 更有期待感
	builder.Heading(fmt.Sprintf("🎬 %s (%d)", data.Title, data.Year), 3)
	if data.Rating > 0 {
		builder.Paragraph(fmt.Sprintf("⭐ TMDB 评分: %.1f", data.Rating))
	}
	builder.Divider()

	// 简介 - 更有故事感
	if data.Overview != "" {
		overview := data.Overview
		if len([]rune(overview)) > 400 {
			overview = string([]rune(overview)[:400]) + "..."
		}
		builder.Heading("📝 简介", 3)
		builder.Paragraph(overview)
	}

	builder.Divider()
	builder.Italic(fmt.Sprintf("🎡 今日剩余 %d 次转盘机会", data.MaxSpins-data.SpinCount))

	return builder.Build()
}

// ============================================================
//  游戏中心入口卡片
// ============================================================

// BuildGameCenterCard 构建游戏中心入口卡片
func BuildGameCenterCard() RichMessage {
	return BuildPage(Page{
		Heading: "游戏中心",
		Tagline: "求片之外的可选玩法",
		Body:    "情报站看背景，盲盒和轮盘随便发现一部。随时可以返回去搜索。",
	}).Rich()
}

// TasteCardData 品味分析卡片数据
type TasteCardData struct {
	UserName   string
	TotalViews int
	TopGenres  []ViewingGenreCount
}

// ViewingGenreCount 观影类型统计
type ViewingGenreCount struct {
	Genre string
	Count int
}

// BuildTasteCard 构建品味分析卡片
func BuildTasteCard(data TasteCardData) RichMessage {
	builder := NewBuilder()

	builder.Heading(fmt.Sprintf("👅 %s 的观影品味", data.UserName), 2)
	builder.Divider()

	builder.BoldParagraph(fmt.Sprintf("📊 观影统计"))
	builder.Paragraph(fmt.Sprintf("  累计观影：%d 部", data.TotalViews))

	if len(data.TopGenres) > 0 {
		builder.Divider()
		builder.BoldParagraph("🎯 偏好类型")
		for i, g := range data.TopGenres {
			if i >= 6 {
				break
			}
			bar := strings.Repeat("█", g.Count/2+1)
			builder.Paragraph(fmt.Sprintf("  %s %s ×%d", bar, g.Genre, g.Count))
		}
	}

	builder.Divider()
	builder.Italic("你的品味，就是你的影迷身份证")

	return builder.Build()
}
