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
//  通关奖励盲盒卡片
// ============================================================

// BlindBoxRewardCardData 通关奖励盲盒数据
type BlindBoxRewardCardData struct {
	Grade string
	Items []BlindBoxItemView
}

// BuildBlindBoxRewardCard 构建通关奖励盲盒卡片
func BuildBlindBoxRewardCard(data BlindBoxRewardCardData) RichMessage {
	b := NewBuilder()

	gradeLabel := map[string]string{
		"SSS": "👑 SSS · 传说宝箱",
		"SS":  "💎 SS · 王者宝箱",
		"S":   "⭐ S · 精英宝箱",
		"A":   "🏆 A · 勇者宝箱",
	}[data.Grade]
	if gradeLabel == "" {
		gradeLabel = "🎁 通关宝箱"
	}

	b.Heading("🎁 通关奖励", 2)
	b.BoldParagraph(fmt.Sprintf("%s", gradeLabel))
	b.Italic(fmt.Sprintf("你在《冒险》中获得了 %s 评级，这是你的奖励", data.Grade))
	b.Divider()

	for _, item := range data.Items {
		rarityIcon := map[string]string{
			"N": "⚪", "R": "🔵", "SR": "🟣", "SSR": "🟡",
		}[item.Rarity]
		if rarityIcon == "" {
			rarityIcon = "⚪"
		}

		yearStr := ""
		if item.Year > 0 {
			yearStr = fmt.Sprintf(" (%d)", item.Year)
		}

		ratingStr := ""
		if item.Rating > 0 {
			ratingStr = fmt.Sprintf(" ⭐ %.1f", item.Rating)
		}

		b.BoldParagraph(fmt.Sprintf("%s [%s] 《%s》%s%s", rarityIcon, item.Rarity, item.Title, yearStr, ratingStr))
		if item.Genres != "" {
			b.Paragraph(fmt.Sprintf("  📂 %s", item.Genres))
		}
		if item.Overview != "" {
			overview := item.Overview
			if len([]rune(overview)) > 80 {
				overview = string([]rune(overview)[:80]) + "..."
			}
			b.Italic(fmt.Sprintf("  %s", overview))
		}
	}

	b.Divider()
	b.Italic("评级越高，宝箱越稀有。继续挑战解锁更好的奖励！")

	return b.Build()
}

// ============================================================
//  游戏中心入口卡片
// ============================================================

// BuildGameCenterCard 构建游戏中心入口卡片
func BuildGameCenterCard(streakCurrent int, streakBest int) RichMessage {
	builder := NewBuilder()

	builder.Heading("🎮 云海游戏中心", 2)

	// 🔥 连胜火焰
	if streakCurrent > 0 {
		fireEmoji := "🔥"
		if streakCurrent >= 30 {
			fireEmoji = "🌈🔥" // 彩虹焰
		} else if streakCurrent >= 7 {
			fireEmoji = "🔱🔥" // 金焰
		} else if streakCurrent >= 3 {
			fireEmoji = "⚡🔥" // 银焰
		}
		builder.BoldParagraph(fmt.Sprintf("%s 连续通关 %d 天 （最佳：%d 天）", fireEmoji, streakCurrent, streakBest))
	} else {
		builder.Italic("今天还没有通关，来打破沉默？")
	}
	builder.Divider()

	builder.BoldParagraph("⚔️ 今晚就玩真的")
	builder.Paragraph("  求片大冒险 — 选一部片，闯完 5 关直接提交求片")
	builder.Paragraph("  🎯 今日挑战 — 每天同一道擂台题，赢了计入战绩")
	builder.Divider()

	builder.BoldParagraph("🏆 有记录，才有输赢")
	builder.Paragraph("  📊 冒险排行 — 只统计真实通关成绩")
	builder.Paragraph("  📈 我的战绩 — 胜场、最高分、连胜和最近对局")
	builder.Divider()

	builder.BoldParagraph("📖 卡关再看")
	builder.Paragraph("  电影情报站 — 输入片名，先补背景再继续挑战")
	builder.Divider()

	builder.Italic("游戏中心只保留能产生真实战绩的玩法")

	return builder.Build()
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
