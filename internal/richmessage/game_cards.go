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

	// 主标题 - 更有冲击力
	builder.Heading(fmt.Sprintf("%s %s", data.TierIcon, data.TierName), 2)
	builder.BoldParagraph(fmt.Sprintf("👤 %s 的影坛战绩", data.UserName))
	builder.Divider()

	// 核心数据 - 用更直观的方式展示
	builder.Heading("📊 核心数据", 3)
	
	// 分数和进度
	scoreText := fmt.Sprintf("🏆 **%d** 分", data.Score)
	if data.NextTier != "" {
		progress := 100 - (data.NextTierDiff * 100 / (data.Score + data.NextTierDiff))
		if progress < 0 {
			progress = 0
		}
		if progress > 100 {
			progress = 100
		}
		bar := buildProgressBar(progress)
		scoreText += fmt.Sprintf("\n%s %d%% → %s", bar, progress, data.NextTier)
	}
	builder.Paragraph(scoreText)
	builder.Divider()

	// 观影统计 - 用图标和数字
	builder.Heading("🎬 观影档案", 3)
	stats := fmt.Sprintf("🎬 **%d** 部电影\n📺 **%d** 部剧集\n🎭 **%d** 种类型\n⭐ **%.1f** 平均评分\n❤️ 最爱：**%s**",
		data.TotalMovies, data.TotalSeries, data.GenreCount, data.AvgRating, data.TopGenre)
	builder.Paragraph(stats)
	builder.Divider()

	// 成就徽章 - 更有仪式感
	if len(data.Badges) > 0 {
		builder.Heading("🏅 荣誉勋章", 3)
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

	// 主标题 - 更有个性
	builder.Heading(fmt.Sprintf("🧠 %s 的电影人格", data.UserName), 2)
	builder.BoldParagraph(fmt.Sprintf("%s %s", data.Type, data.TypeName))
	builder.Divider()

	// 四个维度 - 用更直观的进度条
	builder.Heading("📊 人格维度", 3)
	for _, d := range data.Dimensions {
		bar := buildDimensionBar(d.Score)
		builder.Paragraph(fmt.Sprintf("%s **%s**\n%s %s %s\n结果: **%s**",
			d.Icon, d.Name, d.Left, bar, d.Right, d.Result))
	}
	builder.Divider()

	// 核心特质 - 更有洞察
	builder.Heading("🎯 核心特质", 3)
	builder.Paragraph(fmt.Sprintf("• %s", data.TopTrait))
	builder.Divider()

	// 总结 - 更有温度
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
		// 未揭晓 - 更有悬念
		builder.Italic("三个神秘盒子摆在你面前...")
		builder.Divider()
		for i, item := range data.Items {
			rarityIcon := map[string]string{
				"N": "⚪", "R": "🔵", "SR": "🟣", "SSR": "🟡",
			}[item.Rarity]
			if rarityIcon == "" {
				rarityIcon = "⚪"
			}
			builder.Paragraph(fmt.Sprintf("%s **盲盒 #%d** [%s %s]", rarityIcon, i+1, item.Rarity, rarityIcon))
			builder.Paragraph("❓ 未揭晓 — 点击揭晓按钮看看是什么！")
			if i < len(data.Items)-1 {
				builder.Divider()
			}
		}
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
	builder := NewBuilder()

	builder.Heading("🎮 云海游戏中心", 2)
	builder.Italic("你的观影数据，藏着一个你不知道的自己")
	builder.Divider()

	// 核心体验
	builder.BoldParagraph("🪞 看见自己")
	builder.Paragraph("  情绪画像 — 你的观影习惯暴露了什么？")
	builder.Paragraph("  时光放映机 — AI讲一个关于你的故事")
	builder.Divider()

	// 互动玩法
	builder.BoldParagraph("🎲 玩起来")
	builder.Paragraph("  情绪处方 — 根据心情开药方")
	builder.Paragraph("  命运契约 — 签一份观影挑战")
	builder.Paragraph("  电影盲盒 — 开一个未知的惊喜")
	builder.Paragraph("  👥 关系对比 — 看看谁是你的观影灵魂伴侣")
	builder.Paragraph("  🎯 今日挑战 — 每天一个观影小任务")
	builder.Paragraph("  🏆 成就系统 — 解锁成就徽章，收集经验值")
	builder.Paragraph("  📊 观影周报 — 每周观影数据回顾")
	builder.Divider()

	builder.Italic("每个功能都通向下一个，像一个环，走进去就出不来")

	return builder.Build()
}
