package richmessage

import (
	"fmt"
	"strings"
)

// ============================================================
//  求片大冒险 v2 — 沉浸式卡片引擎
// ============================================================

// AdventureSceneCardData 场景卡片
type AdventureSceneCardData struct {
	MovieTitle  string
	MovieYear   int
	Genres      []string
	Level       int
	TotalLevels int
	StageName   string
	SceneTitle  string
	Description string
	Atmosphere  string
	Choices     []AdventureChoiceView
	Hint        string
	HP          int
	Combo       int
	Score       int
	IsBoss      bool // 最终关
}

type AdventureChoiceView struct {
	Index int
	Text  string
}

// BuildAdventureSceneCard 场景卡片
func BuildAdventureSceneCard(data AdventureSceneCardData) RichMessage {
	b := NewBuilder()

	// 顶部：电影信息 + 关卡进度
	b.Heading(fmt.Sprintf("🎬 %s (%d)", data.MovieTitle, data.MovieYear), 2)

	// 关卡进度条 + 阶段名
	progressBar := buildLevelProgress(data.Level, data.TotalLevels)
	boldLine := fmt.Sprintf("⚔️ %s %s", data.StageName, progressBar)
	b.BoldParagraph(boldLine)

	// 状态条：HP + 连击 + 分数
	hpEmoji := "❤️"
	if data.HP <= 30 {
		hpEmoji = "💔"
	} else if data.HP <= 60 {
		hpEmoji = "🧡"
	}
	statusLine := fmt.Sprintf("%s %d%%", hpEmoji, data.HP)
	if data.Combo > 0 {
		statusLine += fmt.Sprintf("  🔥 连击 x%d", data.Combo)
	}
	statusLine += fmt.Sprintf("  🎯 %d分", data.Score)
	b.Paragraph(statusLine)

	// 氛围词
	atmoIcon := map[string]string{
		"紧张": "⚡", "诡异": "👁️", "压迫": "⛓️", "绝望": "🌑",
		"史诗": "⚔️", "窒息": "🫁", "癫狂": "🌀",
	}[data.Atmosphere]
	if atmoIcon == "" {
		atmoIcon = "🎭"
	}
	b.Italic(fmt.Sprintf("%s 氛围：%s", atmoIcon, data.Atmosphere))
	b.Divider()

	// 场景描述 — 核心沉浸
	b.Paragraph(data.Description)

	// Boss关特殊提示
	if data.IsBoss {
		b.BoldParagraph("💀 BOSS 关 — 这是最后的考验")
	}

	// 提示（如果有）
	if data.Hint != "" {
		b.Italic(fmt.Sprintf("💡 %s", data.Hint))
	}

	return b.Build()
}

// buildLevelProgress 关卡进度条
func buildLevelProgress(current, total int) string {
	var parts []string
	for i := 1; i <= total; i++ {
		if i < current {
			parts = append(parts, "✅")
		} else if i == current {
			parts = append(parts, "🔴")
		} else {
			parts = append(parts, "⬜")
		}
	}
	return strings.Join(parts, "")
}

// ============================================================
//  入口卡片
// ============================================================

type AdventureEntryCardData struct {
	MovieTitle string
	MovieYear  int
	Genres     []string
	Overview   string
	Rating     float64
}

func BuildAdventureEntryCard(data AdventureEntryCardData) RichMessage {
	b := NewBuilder()

	b.Heading("⚔️ 求片大冒险", 2)
	b.BoldParagraph(fmt.Sprintf("🎬 %s (%d)", data.MovieTitle, data.MovieYear))

	var meta []string
	if data.Rating > 0 {
		meta = append(meta, fmt.Sprintf("⭐ %.1f", data.Rating))
	}
	if len(data.Genres) > 0 {
		meta = append(meta, strings.Join(data.Genres, " · "))
	}
	if len(meta) > 0 {
		b.Italic(strings.Join(meta, "  |  "))
	}
	b.Divider()

	if data.Overview != "" {
		overview := data.Overview
		if len([]rune(overview)) > 200 {
			overview = string([]rune(overview)[:200]) + "..."
		}
		b.Paragraph(overview)
	}
	b.Divider()

	// 游戏规则 — 带上心理学钩子
	b.BoldParagraph("📜 冒险规则")
	b.Paragraph("  • 你将化身这部电影的主角")
	b.Paragraph("  • 5道关卡，难度逐级递增")
	b.Paragraph("  • 每关3-4个选项，只有1个能活")
	b.Paragraph("  • ❤️ 生命值100，选错扣血，归零即死")
	b.Paragraph("  • 陷阱选项看起来非常合理——这正是它的危险之处")
	b.Divider()

	// 心理学钩子
	b.BoldParagraph("⚠️ 你确定你了解这部电影吗？")
	b.Paragraph("  大多数人在第2关就会倒下")
	b.Paragraph("  通关率不到 15%")
	b.Italic("  只有通关才能提交求片请求")

	return b.Build()
}

// ============================================================
//  选错反馈卡片（受伤后）
// ============================================================

type AdventureDamageCardData struct {
	ChoiceResult string
	Damage       int
	HP           int
	Level        int
	TotalLevels  int
	Combo        int
	Score        int
	IsDead       bool
}

func BuildAdventureDamageCard(data AdventureDamageCardData) RichMessage {
	b := NewBuilder()

	if data.IsDead {
		b.Heading("💀 生命耗尽", 2)
		b.BoldParagraph(fmt.Sprintf("💥 %s", data.ChoiceResult))
		b.Divider()
		b.Paragraph(fmt.Sprintf("❤️ 0%% — 你倒在了第 %d/%d 关", data.Level, data.TotalLevels))
		b.Italic("这不是结束... 还是想再来一次？")
	} else {
		b.Heading("💥 受伤！", 2)
		b.BoldParagraph(data.ChoiceResult)
		b.Divider()

		// HP条
		hpBar := buildAdventureHPBar(data.HP)
		damageText := fmt.Sprintf("❤️ -%d HP → %s %d%%", data.Damage, hpBar, data.HP)
		b.Paragraph(damageText)

		// 连击归零提示（心理学：损失厌恶）
		if data.Combo == 0 {
			b.Italic("🔥 连击中断！之前的努力白费了...")
		}

		// 状态
		statusLine := fmt.Sprintf("🎯 %d分", data.Score)
		if data.Combo > 0 {
			statusLine += fmt.Sprintf("  🔥 连击 x%d", data.Combo)
		}
		b.Paragraph(statusLine)

		// 低血量警告（心理学：紧迫感）
		if data.HP <= 30 {
			b.BoldParagraph("⚠️ 生命值危险！下一次失误可能是最后一次...")
		} else if data.HP <= 60 {
			b.Italic("💡 仔细想想，别急着选")
		}

		b.Divider()
		b.Paragraph("🎯 再试一次——这次要认真想")
	}

	return b.Build()
}

// buildAdventureHPBar HP进度条
func buildAdventureHPBar(hp int) string {
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

// ============================================================
//  通关卡片
// ============================================================

type AdventureSuccessCardData struct {
	MovieTitle string
	MovieYear  int
	Genres     []string
	Score      int
	Grade      string
	FinalScene string
	EasterEgg  string
	Stats      string
	HP         int
	MaxCombo   int
}

func BuildAdventureSuccessCard(data AdventureSuccessCardData) RichMessage {
	b := NewBuilder()

	// 胜利标题 — 带等级
	gradeIcon := map[string]string{
		"SSS": "👑", "SS": "💎", "S": "⭐", "A": "🏆", "B": "🥈",
	}[data.Grade]
	if gradeIcon == "" {
		gradeIcon = "🏆"
	}
	b.Heading(fmt.Sprintf("%s 通关成功！%s", gradeIcon, data.Grade), 2)
	b.BoldParagraph(fmt.Sprintf("🎬 %s (%d)", data.MovieTitle, data.MovieYear))
	b.Divider()

	// 最终场景
	b.Heading("🎬 最终一幕", 3)
	b.Paragraph(data.FinalScene)
	b.Divider()

	// 结算数据
	b.Heading("📊 冒险结算", 3)
	b.Paragraph(fmt.Sprintf("🎯 最终得分：%d/100", data.Score))
	b.Paragraph(fmt.Sprintf("❤️ 剩余生命：%d%%", data.HP))
	b.Paragraph(fmt.Sprintf("🔥 最高连击：x%d", data.MaxCombo))
	if data.Stats != "" {
		b.Italic(data.Stats)
	}
	b.Divider()

	// 通关彩蛋
	if data.EasterEgg != "" {
		b.Heading("🥚 通关彩蛋", 3)
		b.Paragraph(data.EasterEgg)
		b.Divider()
	}

	b.Italic("🌟 你证明了自己是真正的主角。求片请求已提交！")

	return b.Build()
}

// ============================================================
//  失败卡片
// ============================================================

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
	Grade       string
	Stats       string
	MaxCombo    int
	HP          int
}

func BuildAdventureFailCard(data AdventureFailCardData) RichMessage {
	b := NewBuilder()

	b.Heading("💀 冒险失败", 2)
	b.BoldParagraph(fmt.Sprintf("🎬 %s (%d)", data.MovieTitle, data.MovieYear))

	// 死因
	b.Paragraph(fmt.Sprintf("☠️ %s", data.DeathReason))
	b.Divider()

	// 最后一刻
	b.Heading("🎬 最后一幕", 3)
	b.Paragraph(data.FinalScene)
	b.Divider()

	// 结算
	b.Heading("📊 冒险结算", 3)
	b.Paragraph(fmt.Sprintf("⚔️ 倒在第 %d/%d 关", data.Level, data.TotalLevels))
	b.Paragraph(fmt.Sprintf("🎯 得分：%d  %s", data.Score, data.Grade))
	b.Paragraph(fmt.Sprintf("🔥 最高连击：x%d", data.MaxCombo))
	if data.Stats != "" {
		b.Italic(data.Stats)
	}
	b.Divider()

	// 提示
	if data.Tips != "" {
		b.Italic(fmt.Sprintf("💡 %s", data.Tips))
		b.Divider()
	}

	// 心理学钩子：近因效应 + 损失厌恶
	nearMiss := ""
	switch {
	case data.Level >= 4:
		nearMiss = "你已经走了这么远... 就差一点点了"
	case data.Level >= 3:
		nearMiss = "你比大多数人走得都远了，真的要放弃吗？"
	default:
		nearMiss = "这部电影比你想象的要复杂，但你已经学到了一些东西"
	}
	b.BoldParagraph(fmt.Sprintf("🔄 %s", nearMiss))
	b.Italic("求片请求未提交——只有通关才能求片")

	return b.Build()
}

// ============================================================
//  连击通知卡片（轻量级，选对时显示）
// ============================================================

type AdventureComboCardData struct {
	ChoiceResult string
	Combo        int
	HP           int
	Score        int
	Level        int
	TotalLevels  int
	IsPerfect    bool // 全程无伤
}

func BuildAdventureComboCard(data AdventureComboCardData) RichMessage {
	b := NewBuilder()

	comboText := ""
	switch {
	case data.Combo >= 5:
		comboText = "🔥🔥🔥 五连绝世！"
	case data.Combo >= 4:
		comboText = "🔥🔥 四连超凡！"
	case data.Combo >= 3:
		comboText = "🔥 三连破敌！"
	case data.Combo >= 2:
		comboText = "⚡ 双连命中！"
	default:
		comboText = "✅ 正确！"
	}

	b.Heading(comboText, 2)
	b.BoldParagraph(data.ChoiceResult)
	b.Divider()

	// 状态
	statusLine := fmt.Sprintf("❤️ %d%%  🎯 %d分", data.HP, data.Score)
	if data.Combo >= 2 {
		statusLine += fmt.Sprintf("  🔥 连击 x%d", data.Combo)
	}
	b.Paragraph(statusLine)

	// 连击奖励提示（心理学：正反馈循环）
	if data.Combo >= 3 {
		b.Italic("💎 连击加成！你的电影知识令人印象深刻")
	}

	// 完美通关提示
	if data.IsPerfect {
		b.BoldParagraph("👑 全程无伤！你真的看过这部电影！")
	}

	// 下一关预告
	if data.Level < data.TotalLevels {
		b.Divider()
		b.Italic(fmt.Sprintf("⚔️ 准备进入第 %d/%d 关...", data.Level+1, data.TotalLevels))
	} else {
		b.Divider()
		b.BoldParagraph("⚔️ 最终决战即将开始...")
	}

	return b.Build()
}
