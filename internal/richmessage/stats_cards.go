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
	UserName       string
	TotalChallenges int
	TotalSuccess    int
	BestScore       int
	BestGrade       string
	BestCombo       int
	PerfectRuns     int
	RecentRecords   []AdventureRecordView
}

// AdventureRecordView 冒险记录视图
type AdventureRecordView struct {
	MovieName   string
	MovieYear   int
	Score       int
	Grade       string
	Success     bool
	MaxCombo    int
	TimeAgo     string
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
