package richmessage

import (
	"fmt"
	"strings"
)

// ============================================================
//  段位系统卡片
// ============================================================

// RankCardData 段位卡片数据
type RankCardData struct {
	UserName     string
	TierName     string
	TierIcon     string
	Score        int
	TotalMovies  int
	TotalSeries  int
	GenreCount   int
	AvgRating    float64
	Badges       []string
	NextTier     string
	NextTierDiff int
	TopGenre     string
}

// BuildRankCard 构建段位卡片
func BuildRankCard(data RankCardData) RichMessage {
	builder := NewBuilder()

	builder.Heading(fmt.Sprintf("%s %s 的影坛段位", data.TierIcon, data.TierName), 2)
	builder.BoldParagraph(fmt.Sprintf("👤 %s", data.UserName))
	builder.Divider()

	// 段位进度
	scoreBar := fmt.Sprintf("🏆 总分: **%d**", data.Score)
	if data.NextTier != "" {
		scoreBar += fmt.Sprintf("  →  距离 **%s** 还差 %d 分", data.NextTier, data.NextTierDiff)
	}
	builder.Paragraph(scoreBar)
	builder.Divider()

	// 数据统计
	stats := fmt.Sprintf("🎬 电影: %d  |  📺 剧集: %d  |  🎭 类型: %d 种\n⭐ 平均评分: %.1f  |  ❤️ 最爱: %s",
		data.TotalMovies, data.TotalSeries, data.GenreCount, data.AvgRating, data.TopGenre)
	builder.Paragraph(stats)
	builder.Divider()

	// 成就徽章
	if len(data.Badges) > 0 {
		builder.Heading("🏅 成就徽章", 3)
		for _, b := range data.Badges {
			builder.Paragraph(fmt.Sprintf("• %s", b))
		}
	}

	return builder.Build()
}

// ============================================================
//  性格测试卡片
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
func BuildPersonalityCard(data PersonalityCardData) RichMessage {
	builder := NewBuilder()

	builder.Heading(fmt.Sprintf("🧠 %s 的电影人格", data.UserName), 2)
	builder.BoldParagraph(fmt.Sprintf("%s %s", data.Type, data.TypeName))
	builder.Divider()

	// 四个维度
	builder.Heading("📊 人格维度", 3)
	for _, d := range data.Dimensions {
		bar := buildDimensionBar(d.Score)
		builder.Paragraph(fmt.Sprintf("%s **%s**\n%s %s %s\n结果: **%s**",
			d.Icon, d.Name, d.Left, bar, d.Right, d.Result))
	}
	builder.Divider()

	// 核心特质
	builder.Heading("🎯 核心特质", 3)
	builder.Paragraph(fmt.Sprintf("• %s", data.TopTrait))
	builder.Divider()

	// 总结
	builder.Italic(data.Description)

	return builder.Build()
}

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

	// 标题行：电影名 + 年份
	builder.Heading(fmt.Sprintf("🎬 %s (%d)", data.Title, data.Year), 2)

	// 模式标签 + 评分 + 类型 一行搞定
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

	// 剧情概要
	if data.Summary != "" {
		if len([]rune(data.Summary)) > 800 {
			data.Summary = string([]rune(data.Summary)[:800]) + "..."
		}
		builder.Paragraph(data.Summary)
	}

	// 关键看点
	if len(data.KeyPoints) > 0 {
		builder.BoldParagraph("💡 看点")
		for _, p := range data.KeyPoints {
			builder.Paragraph(fmt.Sprintf("  • %s", p))
		}
	}

	// 适合心情
	if data.Mood != "" {
		builder.Paragraph(fmt.Sprintf("🎭 **适合心情：** %s", data.Mood))
	}

	// 类似推荐
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
	Title     string
	Year      int
	Rating    float64
	Rarity    string
	Genres    string
	Overview  string
	Revealed  bool
}

// BuildBlindBoxCard 构建盲盒卡片
func BuildBlindBoxCard(data BlindBoxCardData) RichMessage {
	builder := NewBuilder()

	builder.Heading("🎰 电影盲盒", 2)
	builder.BoldParagraph("惊喜从这里开始...")
	builder.Divider()

	for i, item := range data.Items {
		rarityIcon := map[string]string{
			"N": "⚪", "R": "🔵", "SR": "🟣", "SSR": "🟡",
		}[item.Rarity]
		if rarityIcon == "" {
			rarityIcon = "⚪"
		}

		if item.Revealed {
			builder.Paragraph(fmt.Sprintf("%s **盲盒 #%d** [%s %s]\n🎬 %s (%d)\n⭐ %.1f · %s",
				rarityIcon, i+1, item.Rarity, rarityIcon, item.Title, item.Year, item.Rating, item.Genres))
			if item.Overview != "" {
				overview := item.Overview
				if len([]rune(overview)) > 150 {
					overview = string([]rune(overview)[:150]) + "..."
				}
				builder.Details("📖 简介", overview, false)
			}
		} else {
			builder.Paragraph(fmt.Sprintf("%s **盲盒 #%d** [%s %s]\n❓ 未揭晓 - 点击揭晓按钮看看是什么！",
				rarityIcon, i+1, item.Rarity, rarityIcon))
		}
		if i < len(data.Items)-1 {
			builder.Divider()
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

	builder.Heading("📢 影友圈动态", 2)
	builder.Divider()

	if len(data.Events) == 0 {
		builder.Italic("还没有动态，快来写第一条影评吧！")
		return builder.Build()
	}

	for _, e := range data.Events {
		icon := "📝"
		switch e.EventType {
		case "review":
			icon = "⭐"
		case "achievement":
			icon = "🏆"
		case "watch":
			icon = "🎬"
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

	builder.Heading(fmt.Sprintf("⭐ %s 的影评", data.UserName), 2)
	builder.BoldParagraph(fmt.Sprintf("《%s》", data.MovieName))
	builder.Divider()

	builder.Paragraph(fmt.Sprintf("评分: %s", strings.Repeat("⭐", data.Rating)))
	builder.Paragraph(data.Content)
	builder.Divider()

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

	builder.Heading("🎡 命运轮盘", 2)
	builder.BoldParagraph(fmt.Sprintf("🎰 轮盘转动！今日第 %d/%d 次", data.SpinCount, data.MaxSpins))
	builder.Divider()

	builder.Heading(fmt.Sprintf("🎬 %s (%d)", data.Title, data.Year), 3)
	if data.Rating > 0 {
		builder.Paragraph(fmt.Sprintf("⭐ TMDB 评分: %.1f", data.Rating))
	}
	builder.Divider()

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
	builder := NewBuilder()

	builder.Heading("🎮 云海游戏中心", 2)
	builder.BoldParagraph("观影不止于看，更在于玩")
	builder.Divider()

	builder.Paragraph("🏆 **电影段位** — 你在影坛是什么级别？")
	builder.Paragraph("🧠 **灵魂画像** — 你的观影人格是什么？")
	builder.Paragraph("🎬 **AI 解说** — 3分钟了解一部电影")
	builder.Paragraph("🎰 **电影盲盒** — 今天开什么惊喜？")
	builder.Paragraph("📢 **影友圈** — 和朋友分享观影心得")
	builder.Paragraph("🎡 **命运轮盘** — 让命运决定今晚看什么")

	builder.Divider()
	builder.Italic("选择一个功能开始探索吧！")

	return builder.Build()
}
