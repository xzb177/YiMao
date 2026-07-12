package richmessage

import (
	"fmt"
	"time"
)

// ============================================================
//  冒险统计卡片
// ============================================================

// AdventureStatsCardData 冒险统计卡片数据
type AdventureStatsCardData struct {
	UserName        string
	TotalChallenges int
	TotalSuccess    int
	BestScore       int
	BestGrade       string
	BestCombo       int
	PerfectRuns     int
	RecentRecords   []AdventureRecordView
	CurrentStreak   int // 当前连胜天数
	BestStreak      int // 最佳连胜天数
}

// AdventureRecordView 冒险记录视图
type AdventureRecordView struct {
	MovieName string
	MovieYear int
	Score     int
	Grade     string
	Success   bool
	MaxCombo  int
	TimeAgo   string
}

// BuildAdventureStatsCard 构建冒险统计卡片
func BuildAdventureStatsCard(data AdventureStatsCardData) RichMessage {
	b := NewBuilder()

	b.Heading("⚔️ 冒险档案", 2)
	b.BoldParagraph(fmt.Sprintf("👤 %s 的闯关记录", data.UserName))
	b.Divider()

	// 核心数据
	if data.TotalChallenges == 0 {
		b.Italic("还没有挑战记录，去试试吧！")
		return b.Build()
	}

	passRate := 0
	if data.TotalChallenges > 0 {
		passRate = data.TotalSuccess * 100 / data.TotalChallenges
	}

	b.Heading("📊 战绩总览", 3)
	b.Paragraph(fmt.Sprintf("🎯 总挑战：%d 次", data.TotalChallenges))
	b.Paragraph(fmt.Sprintf("✅ 通关：%d 次（%d%%）", data.TotalSuccess, passRate))
	b.Paragraph(fmt.Sprintf("🏆 最高分：%d  评级：%s", data.BestScore, data.BestGrade))
	b.Paragraph(fmt.Sprintf("🔥 最高连击：x%d", data.BestCombo))
	if data.PerfectRuns > 0 {
		b.Paragraph(fmt.Sprintf("🛡️ 全程无伤：%d 次", data.PerfectRuns))
	}

	// 连胜数据
	if data.CurrentStreak > 0 {
		b.Divider()
		streakEmoji := "🔥"
		if data.CurrentStreak >= 7 {
			streakEmoji = "🔥🔥🔥"
		} else if data.CurrentStreak >= 3 {
			streakEmoji = "🔥🔥"
		}
		b.BoldParagraph(fmt.Sprintf("%s 连胜 %d 天！", streakEmoji, data.CurrentStreak))
		if data.BestStreak > data.CurrentStreak {
			b.Italic(fmt.Sprintf("最佳纪录：%d 天", data.BestStreak))
		}
		b.Paragraph("  每天挑战，连胜越长奖励越丰厚")
		b.Paragraph("  断签一次，连胜归零")
	}

	// 最近记录
	if len(data.RecentRecords) > 0 {
		b.Divider()
		b.Heading("📜 最近挑战", 3)
		for _, r := range data.RecentRecords {
			icon := "💀"
			if r.Success {
				icon = gradeToIcon(r.Grade)
			}
			yearStr := ""
			if r.MovieYear > 0 {
				yearStr = fmt.Sprintf(" (%d)", r.MovieYear)
			}
			b.Paragraph(fmt.Sprintf("%s 《%s》%s — %d分 %s  %s",
				icon, r.MovieName, yearStr, r.Score, r.Grade, r.TimeAgo))
		}
	}

	return b.Build()
}

func gradeToIcon(grade string) string {
	switch grade {
	case "SSS":
		return "👑"
	case "SS":
		return "💎"
	case "S":
		return "⭐"
	case "A":
		return "🏆"
	case "B":
		return "🥈"
	default:
		return "✅"
	}
}

// FormatTimeAgo 格式化时间差
func FormatTimeAgo(t time.Time) string {
	diff := time.Since(t)
	switch {
	case diff < time.Minute:
		return "刚刚"
	case diff < time.Hour:
		return fmt.Sprintf("%d分钟前", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%d小时前", int(diff.Hours()))
	case diff < 7*24*time.Hour:
		return fmt.Sprintf("%d天前", int(diff.Hours()/24))
	default:
		return t.Format("01-02")
	}
}

// ============================================================
//  求片路径统计卡片（双核心总览）
// ============================================================

// UserRequestStatsData 求片统计卡片
type UserRequestStatsData struct {
	UserName         string
	NormalTotal      int
	NormalApproved   int
	AdventureTotal   int
	AdventureSuccess int
	BestGrade        string
	BestCombo        int
}

// BuildUserRequestStatsCard 构建求片统计卡片
func BuildUserRequestStatsCard(data UserRequestStatsData) RichMessage {
	b := NewBuilder()

	b.Heading("📊 求片档案", 2)
	b.BoldParagraph(fmt.Sprintf("👤 %s", data.UserName))
	b.Divider()

	// 普通求片
	b.Heading("🔍 普通求片", 3)
	if data.NormalTotal > 0 {
		approveRate := data.NormalApproved * 100 / data.NormalTotal
		b.Paragraph(fmt.Sprintf("  总请求：%d 次", data.NormalTotal))
		b.Paragraph(fmt.Sprintf("  通过率：%d%%", approveRate))
	} else {
		b.Paragraph("  暂无记录")
	}

	// 趣味求片
	b.Heading("⚔️ 趣味求片", 3)
	if data.AdventureTotal > 0 {
		passRate := data.AdventureSuccess * 100 / data.AdventureTotal
		b.Paragraph(fmt.Sprintf("  总挑战：%d 次", data.AdventureTotal))
		b.Paragraph(fmt.Sprintf("  通关率：%d%%", passRate))
		if data.BestGrade != "" {
			b.Paragraph(fmt.Sprintf("  最高评级：%s", data.BestGrade))
		}
		if data.BestCombo > 0 {
			b.Paragraph(fmt.Sprintf("  最高连击：x%d", data.BestCombo))
		}
	} else {
		b.Paragraph("  暂无记录")
	}

	b.Divider()

	// 总览
	total := data.NormalTotal + data.AdventureTotal
	b.Paragraph(fmt.Sprintf("🎬 总求片：%d 次", total))

	return b.Build()
}

// ============================================================
//  冒险排行榜卡片
// ============================================================

// AdventureRankPlayer 排行榜玩家数据
type AdventureRankPlayer struct {
	Rank         int
	UserName     string
	BestScore    int
	BestGrade    string
	BestCombo    int
	TotalSuccess int
	PerfectRuns  int
}

// AdventureRankCardData 排行榜卡片数据
type AdventureRankCardData struct {
	UserName   string
	TopPlayers []AdventureRankPlayer
}

// BuildAdventureRankCard 构建冒险排行榜卡片
func BuildAdventureRankCard(data AdventureRankCardData) RichMessage {
	b := NewBuilder()

	b.Heading("📊 冒险排行榜", 2)
	b.Italic("谁是最强影迷？用实力说话")
	b.Divider()

	if len(data.TopPlayers) == 0 {
		b.Italic("还没有人通关过... 你要做第一个吗？")
		return b.Build()
	}

	boldLine := "🏆 殿堂"
	b.BoldParagraph(boldLine)

	rankIcons := []string{"🥇", "🥈", "🥉"}
	for _, p := range data.TopPlayers {
		icon := fmt.Sprintf("#%d", p.Rank)
		if p.Rank <= 3 {
			icon = rankIcons[p.Rank-1]
		}

		suffix := ""
		if p.PerfectRuns > 0 {
			suffix = fmt.Sprintf(" 🛡️x%d", p.PerfectRuns)
		}

		b.Paragraph(fmt.Sprintf("%s %s — %d分 %s 🔥x%d%s",
			icon, p.UserName, p.BestScore, p.BestGrade, p.BestCombo, suffix))
	}

	b.Divider()
	b.Italic(fmt.Sprintf("👤 %s — 向排行榜发起冲击！", data.UserName))

	return b.Build()
}

// ============================================================
//  每日挑战卡片
// ============================================================

// DailyChallengeCardData 每日挑战卡片数据
type DailyChallengeCardData struct {
	MovieTitle     string
	MovieYear      int
	Genre          string
	Hint           string
	Completed      bool
	DayStreak      int
	SocialProof    string // 社交证明文本（如"昨天 张三 通关了这部电影"）
	ChallengerName string // 挑战者名字
}

// BuildDailyChallengeCard 构建每日挑战卡片
func BuildDailyChallengeCard(data DailyChallengeCardData) RichMessage {
	b := NewBuilder()

	b.Heading("🎯 今日挑战", 2)
	b.Italic("每天一部电影，通关有额外奖励")
	b.Divider()

	// 社交攀比提示
	if data.SocialProof != "" {
		b.Italic(fmt.Sprintf("🔥 %s", data.SocialProof))
		b.Divider()
	}

	b.BoldParagraph(fmt.Sprintf("🎬 《%s》(%d)", data.MovieTitle, data.MovieYear))
	b.Paragraph(fmt.Sprintf("📂 类型：%s", data.Genre))
	b.Italic(fmt.Sprintf("💡 线索：%s", data.Hint))
	b.Divider()

	if data.Completed {
		b.BoldParagraph("✅ 今日挑战已完成！")
		if data.DayStreak > 0 {
			b.Paragraph(fmt.Sprintf("🔥 连续挑战 %d 天", data.DayStreak))
		}
		b.Italic("明天还有一部新电影等你")
	} else {
		b.BoldParagraph("⚔️ 你敢接受挑战吗？")
		b.Paragraph("  • 通关双倍冒险积分")
		b.Paragraph("  • SSS评级额外奖励盲盒")
		b.Italic("  大多数人会在第3关倒下")
	}

	return b.Build()
}
