package richmessage

import (
	"fmt"
	"strings"
)

// ============================================================
//  求片大冒险 — 卡片构建器
// ============================================================

// AdventureSceneCardData 冒险场景卡片数据
type AdventureSceneCardData struct {
	MovieTitle  string
	MovieYear   int
	Genres      []string
	Level       int
	TotalLevels int
	SceneTitle  string
	Description string
	Choices     []AdventureChoiceView
	Hint        string
	HP          int // 生命值 0-100
}

// AdventureChoiceView 选项视图
type AdventureChoiceView struct {
	Index int
	Text  string
}

// BuildAdventureSceneCard 构建冒险场景卡片
func BuildAdventureSceneCard(data AdventureSceneCardData) RichMessage {
	builder := NewBuilder()

	// 主标题 — 电影名 + 关卡
	builder.Heading(fmt.Sprintf("🎬 %s (%d)", data.MovieTitle, data.MovieYear), 2)
	builder.BoldParagraph(fmt.Sprintf("⚔️ 第 %d/%d 关 · %s", data.Level, data.TotalLevels, data.SceneTitle))

	// 生命值进度条
	hpBar := buildAdventureHP(data.HP)
	builder.Paragraph(fmt.Sprintf("❤️ 生命值 %s %d%%", hpBar, data.HP))

	// 类型标签
	if len(data.Genres) > 0 {
		builder.Italic(fmt.Sprintf("🎭 %s", strings.Join(data.Genres, " · ")))
	}
	builder.Divider()

	// 场景描述 — 核心沉浸体验
	builder.Paragraph(data.Description)

	// 提示（如果有）
	if data.Hint != "" {
		builder.Divider()
		builder.Italic(fmt.Sprintf("💡 线索：%s", data.Hint))
	}

	return builder.Build()
}

// buildAdventureHP 构建生命值进度条
func buildAdventureHP(hp int) string {
	if hp < 0 {
		hp = 0
	}
	if hp > 100 {
		hp = 100
	}
	filled := hp / 10
	empty := 10 - filled
	bar := strings.Repeat("❤️", filled) + strings.Repeat("🖤", empty)
	return bar
}

// AdventureSuccessCardData 通关卡片数据
type AdventureSuccessCardData struct {
	MovieTitle string
	MovieYear  int
	Genres     []string
	Score      int
	FinalScene string
	EasterEgg  string
	TotalHP    int
}

// BuildAdventureSuccessCard 构建通关卡片
func BuildAdventureSuccessCard(data AdventureSuccessCardData) RichMessage {
	builder := NewBuilder()

	// 胜利标题
	builder.Heading("🏆 通关成功！", 2)
	builder.BoldParagraph(fmt.Sprintf("🎬 %s (%d)", data.MovieTitle, data.MovieYear))

	// 分数展示
	grade := getScoreGrade(data.Score)
	builder.Paragraph(fmt.Sprintf("🎯 得分：%d/100 %s", data.Score, grade))
	builder.Paragraph(fmt.Sprintf("❤️ 剩余生命：%d%%", data.TotalHP))
	builder.Divider()

	// 最终场景
	builder.Heading("🎬 最终一幕", 3)
	builder.Paragraph(data.FinalScene)

	// 通关彩蛋
	if data.EasterEgg != "" {
		builder.Divider()
		builder.Heading("🥚 通关彩蛋", 3)
		builder.Paragraph(data.EasterEgg)
	}

	builder.Divider()
	builder.Italic("🌟 你是真正的主角！这部电影等你来征服～")

	return builder.Build()
}

// AdventureFailCardData 失败卡片数据
type AdventureFailCardData struct {
	MovieTitle  string
	MovieYear   int
	Genres      []string
	Level       int
	TotalLevels int
	FinalScene  string
	DeathReason string
	Tips        string
	Score       int
}

// BuildAdventureFailCard 构建失败卡片
func BuildAdventureFailCard(data AdventureFailCardData) RichMessage {
	builder := NewBuilder()

	// 失败标题
	builder.Heading("💀 冒险失败", 2)
	builder.BoldParagraph(fmt.Sprintf("🎬 %s (%d)", data.MovieTitle, data.MovieYear))

	// 失败信息
	builder.Paragraph(fmt.Sprintf("⚔️ 倒在第 %d/%d 关", data.Level, data.TotalLevels))
	builder.Paragraph(fmt.Sprintf("🎯 得分：%d/100", data.Score))
	builder.Divider()

	// 死因
	builder.Heading("☠️ 死因", 3)
	builder.Paragraph(data.DeathReason)

	// 最终场景
	builder.Heading("🎬 最后一刻", 3)
	builder.Paragraph(data.FinalScene)

	// 提示
	if data.Tips != "" {
		builder.Divider()
		builder.Italic(fmt.Sprintf("💡 %s", data.Tips))
	}

	builder.Divider()
	builder.Italic("🔄 再来一次？也许这次你能成为真正的主角。")

	return builder.Build()
}

// AdventureEntryCardData 冒险入口卡片数据
type AdventureEntryCardData struct {
	MovieTitle string
	MovieYear  int
	Genres     []string
	Overview   string
	Rating     float64
}

// BuildAdventureEntryCard 构建冒险入口卡片
func BuildAdventureEntryCard(data AdventureEntryCardData) RichMessage {
	builder := NewBuilder()

	// 标题
	builder.Heading("⚔️ 求片大冒险", 2)
	builder.BoldParagraph(fmt.Sprintf("🎬 %s (%d)", data.MovieTitle, data.MovieYear))

	// 元数据
	var meta []string
	if data.Rating > 0 {
		meta = append(meta, fmt.Sprintf("⭐ %.1f", data.Rating))
	}
	if len(data.Genres) > 0 {
		meta = append(meta, strings.Join(data.Genres, " · "))
	}
	if len(meta) > 0 {
		builder.Italic(strings.Join(meta, "  |  "))
	}
	builder.Divider()

	// 简介
	if data.Overview != "" {
		overview := data.Overview
		if len([]rune(overview)) > 200 {
			overview = string([]rune(overview)[:200]) + "..."
		}
		builder.Paragraph(overview)
	}
	builder.Divider()

	// 游戏说明
	builder.BoldParagraph("📜 冒险规则")
	builder.Paragraph("  • 你将成为这部电影的主角")
	builder.Paragraph("  • 每关一个场景，3个选项")
	builder.Paragraph("  • 只有1个是正确的——需要真正了解这部电影")
	builder.Paragraph("  • ❤️ 生命值100，选错扣30-50")
	builder.Paragraph("  • 通关才能提交求片！")
	builder.Divider()

	builder.Italic("🎬 准备好了吗？你的冒险即将开始...")

	return builder.Build()
}

// getScoreGrade 根据分数返回等级
func getScoreGrade(score int) string {
	switch {
	case score >= 95:
		return "SSS · 传奇"
	case score >= 85:
		return "SS · 完美"
	case score >= 75:
		return "S · 优秀"
	case score >= 60:
		return "A · 不错"
	default:
		return "B · 及格"
	}
}
