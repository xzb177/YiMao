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
	IsBoss      bool
	LastResult  string // 上一关的结果反馈（内嵌，不单独发卡片）
	// 心理学数据
	DeathRate   string // "73% 的人死在这一关"
	OptionStats string // "📊 A 35% | B 15% | C 42% | D 8%"
	TimeUrgency string // "⚡ 思考时间：30秒"
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

	// 上一关结果反馈（内嵌，不单独发卡片）
	if data.LastResult != "" {
		b.BoldParagraph(data.LastResult)
	}

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

	// 心理学数据：社交证明 + 时间压力
	if data.DeathRate != "" || data.OptionStats != "" || data.TimeUrgency != "" {
		b.Divider()
		if data.DeathRate != "" {
			b.BoldParagraph(data.DeathRate)
		}
		if data.TimeUrgency != "" {
			b.Paragraph(data.TimeUrgency)
		}
		if data.OptionStats != "" {
			b.Italic(data.OptionStats)
		}
	}

	// 选项全文（显示在卡片里，按钮只放编号）
	if len(data.Choices) > 0 {
		b.Divider()
		numbers := []string{"1️⃣", "2️⃣", "3️⃣", "4️⃣"}
		for i, c := range data.Choices {
			num := "?"
			if i < len(numbers) {
				num = numbers[i]
			}
			b.Paragraph(fmt.Sprintf("%s %s", num, c.Text))
		}
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
//  炫耀卡（分享到群）
// ============================================================

// AdventureShareCardData 炫耀卡数据
type AdventureShareCardData struct {
	UserName    string
	MovieTitle  string
	MovieYear   int
	Score       int
	HP          int
	MaxCombo    int
	PerfectRun  bool
	Success     bool
	Level       int
	TotalLevels int
}

// BuildAdventureShareCard 构建炫耀卡（群内分享用，精简有冲击力）
func BuildAdventureShareCard(data AdventureShareCardData) RichMessage {
	b := NewBuilder()

	if data.Success {
		// 通关炫耀
		gradeIcon := "🏆"
		if data.Score >= 90 {
			gradeIcon = "👑"
		} else if data.Score >= 80 {
			gradeIcon = "💎"
		} else if data.Score >= 70 {
			gradeIcon = "⭐"
		}

		b.BoldParagraph(fmt.Sprintf("%s %s 通关了《%s》的求片大冒险！", gradeIcon, data.UserName, data.MovieTitle))

		statsLine := fmt.Sprintf("🎯 %d分  ❤️ %d%%  🔥 x%d", data.Score, data.HP, data.MaxCombo)
		b.Paragraph(statsLine)

		if data.PerfectRun {
			b.BoldParagraph("🛡️ 全程无伤 — 无人能及！")
		}

		if data.Score >= 90 {
			b.Italic("这不是挑战，这是碾压")
		} else if data.Score >= 70 {
			b.Italic("这部电影他不只是看过——他活过")
		} else {
			b.Italic("险象环生，但他挺过来了")
		}
	} else {
		// 失败炫耀（惜败也有面子）
		b.BoldParagraph(fmt.Sprintf("💀 %s 在《%s》的求片大冒险中惜败", data.UserName, data.MovieTitle))
		b.Paragraph(fmt.Sprintf("⚔️ 倒在第 %d/%d 关  🎯 %d分", data.Level, data.TotalLevels, data.Score))

		if data.Level >= 4 {
			b.Italic("差一步就通关了... 这个人有实力")
		} else if data.Level >= 3 {
			b.Italic("走到了深渊边缘，差一点就能看到真相")
		} else {
			b.Italic("这部电影比想象中要难...")
		}
	}

	return b.Build()
}

// ============================================================
//  入口卡片
// ============================================================

type AdventureEntryCardData struct {
	MovieTitle   string
	MovieYear    int
	Genres       []string
	Overview     string
	Rating       float64
	NemesisCount int // ☠️ 被这部电影击败的次数
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

	// ☠️ 宿敌标记
	if data.NemesisCount > 0 {
		if data.NemesisCount == 1 {
			b.BoldParagraph("☠️ 宿敌！这部电影击败过你 1 次")
		} else {
			b.BoldParagraph(fmt.Sprintf("☠️ 宿敌！这部电影已经击败你 %d 次", data.NemesisCount))
		}
		b.Divider()
	}

	// 游戏规则 — 带上心理学钩子
	b.BoldParagraph("📜 冒险规则")
	b.Paragraph("  • 你将化身这部电影的主角")
	b.Paragraph("  • 5道关卡，难度逐级递增")
	b.Paragraph("  • 每关4个选项，每个都看起来非常合理")
	b.Paragraph("  • ❤️ 生命值100，选错扣45-60HP")
	b.Paragraph("  • ⚠️ 两次失误即死，没有犯错空间")
	b.Paragraph("  • 陷阱选项看起来最正确——这正是它的危险之处")
	b.Divider()

	b.BoldParagraph("🎬 你对这部电影记得多深？")
	b.Italic("  只有通关才能提交求片请求")

	return b.Build()
}

// ============================================================
//  选错反馈卡片（受伤后）
// ============================================================

type AdventureDamageCardData struct {
	ChoiceResult     string
	Damage           int
	HP               int
	Level            int
	TotalLevels      int
	Combo            int
	Score            int
	IsDead           bool
	RemainingChoices []AdventureChoiceView // 剩余选项
	TriedChoices     map[int]bool          // 已试过的选项
	CorrectAnswer    string                // 正确答案（死亡时展示）
	CorrectReason    string                // 正确原因（死亡时展示）
}

func BuildAdventureDamageCard(data AdventureDamageCardData) RichMessage {
	b := NewBuilder()

	if data.IsDead {
		b.Heading("💀 生命耗尽", 2)
		b.BoldParagraph(fmt.Sprintf("💥 %s", data.ChoiceResult))
		b.Divider()
		b.Paragraph(fmt.Sprintf("❤️ 0%% — 你倒在了第 %d/%d 关", data.Level, data.TotalLevels))

		// 真相时刻：展示正确答案
		if data.CorrectAnswer != "" {
			b.Divider()
			b.BoldParagraph("🔍 真相时刻")
			b.Paragraph(fmt.Sprintf("✅ 正确答案：%s", data.CorrectAnswer))
			if data.CorrectReason != "" {
				b.Italic(fmt.Sprintf("💡 %s", data.CorrectReason))
			}
		}

		b.Divider()
		b.Italic("这不是结束... 还是想再来一次？")
	} else {
		b.Heading("💥 受伤！", 2)
		b.BoldParagraph(data.ChoiceResult)
		b.Divider()

		// HP条
		hpBar := buildAdventureHPBar(data.HP)
		damageText := fmt.Sprintf("❤️ -%d HP → %s %d%%", data.Damage, hpBar, data.HP)
		b.Paragraph(damageText)

		// 连击归零提示
		if data.Combo == 0 {
			b.Italic("🔥 连击中断，下一题重新起势")
		}

		// 状态
		statusLine := fmt.Sprintf("🎯 %d分", data.Score)
		if data.Combo > 0 {
			statusLine += fmt.Sprintf("  🔥 连击 x%d", data.Combo)
		}
		b.Paragraph(statusLine)

		// 低血量提示
		if data.HP <= 30 {
			b.BoldParagraph("💡 生命值较低，先看清线索再选择")
		} else if data.HP <= 60 {
			b.Italic("💡 仔细想想，别急着选")
		}

		// 显示剩余选项（标记已试过的）
		if len(data.RemainingChoices) > 0 {
			b.Divider()
			b.BoldParagraph("🎯 选项：")
			numbers := []string{"1️⃣", "2️⃣", "3️⃣", "4️⃣"}
			for i, c := range data.RemainingChoices {
				num := "?"
				if i < len(numbers) {
					num = numbers[i]
				}
				// 已试过的选项加标记
				if data.TriedChoices != nil && data.TriedChoices[c.Index] {
					b.Paragraph(fmt.Sprintf("~~%s %s~~ ❌ 已试过", num, c.Text))
				} else {
					b.Paragraph(fmt.Sprintf("%s %s", num, c.Text))
				}
			}
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
	MovieTitle     string
	MovieYear      int
	Genres         []string
	Score          int
	Grade          string
	FinalScene     string
	EasterEgg      string
	Stats          string
	HP             int
	MaxCombo       int
	BonusEffect    string // 随机彩蛋效果
	Recommendation string // 基于观影历史的推荐
	GlobalPassRate string // 🌍 全球通关率
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

	// 随机彩蛋奖励
	if data.BonusEffect != "" {
		b.BoldParagraph(fmt.Sprintf("🎰 %s", data.BonusEffect))
		b.Divider()
	}

	// 🌍 全球通关率
	if data.GlobalPassRate != "" {
		b.BoldParagraph("🌍 全球通关率")
		b.Paragraph(data.GlobalPassRate)
		b.Divider()
	}

	// 个性化推荐（基于观影历史）
	if data.Recommendation != "" {
		b.BoldParagraph("🎬 推荐下一部")
		b.Paragraph(data.Recommendation)
		b.Divider()
	}

	b.Italic("🌟 你证明了自己是真正的主角。求片请求已提交！")

	return b.Build()
}

// ============================================================
//  失败卡片
// ============================================================

type AdventureFailCardData struct {
	MovieTitle     string
	MovieYear      int
	Genres         []string
	Level          int
	TotalLevels    int
	FinalScene     string
	DeathReason    string
	Tips           string
	Score          int
	Grade          string
	Stats          string
	MaxCombo       int
	HP             int
	GlobalPassRate string // 🌍 全球通关率
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

	// 🌍 全球通关率
	if data.GlobalPassRate != "" {
		b.BoldParagraph("🌍 全球通关率")
		b.Paragraph(data.GlobalPassRate)
		b.Divider()
	}

	// 心理学钩子：近因效应 + 损失厌恶
	nearMiss := ""
	switch {
	case data.Level >= 4:
		nearMiss = "你已经看到了终点的光... 就差这一步"
	case data.Level >= 3:
		nearMiss = "已经走进后半场，线索也更清楚了"
	default:
		nearMiss = "这部电影比你想象的要复杂，但你已经学到了关键线索"
	}
	b.BoldParagraph(fmt.Sprintf("🔄 %s", nearMiss))
	b.Italic("求片请求未提交——只有通关才能求片")

	// 惜败安慰：第4-5关失败给安慰奖
	if data.Level >= 4 {
		b.Divider()
		b.BoldParagraph("🎁 惜败安慰奖")
		b.Paragraph("  虽然没能通关，但你的勇气值得奖励")
		b.Italic("  下次挑战时，第1关直接跳过，从第2关开始")
	}

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

// ============================================================
//  🩸 每日免费复活卡片
// ============================================================

type AdventureReviveCardData struct {
	MovieTitle    string
	Level         int
	TotalLevels   int
	HP            int
	Damage        int
	CorrectAnswer string
}

func BuildAdventureReviveCard(data AdventureReviveCardData) RichMessage {
	b := NewBuilder()

	b.Heading("🩸 命悬一线", 2)
	b.BoldParagraph(fmt.Sprintf("🎬 %s · 第 %d/%d 关", data.MovieTitle, data.Level, data.TotalLevels))
	b.Divider()

	b.Paragraph(fmt.Sprintf("💀 你受到了 %d 点伤害，生命值归零", data.Damage))
	if data.CorrectAnswer != "" {
		b.Italic(fmt.Sprintf("正确答案是：%s", data.CorrectAnswer))
	}
	b.Divider()

	b.BoldParagraph("🩸 今日免费复活可用")
	b.Paragraph("  每天第1次在第1-3关死亡时")
	b.Paragraph("  可以免费复活 · HP恢复到 30")
	b.Paragraph("  点击后恢复 30 HP，并重新挑战当前关卡")
	b.Divider()

	b.Italic("今天可使用一次免费复活")

	return b.Build()
}

// ============================================================
//  🎰 双倍或归零 — 下注卡片
// ============================================================

type GambleOfferCardData struct {
	Grade      string
	ItemCount  int
	MovieTitle string
}

func BuildGambleOfferCard(data GambleOfferCardData) RichMessage {
	b := NewBuilder()

	b.Heading("🎰 战利品时间", 2)
	b.BoldParagraph(fmt.Sprintf("🏆 %s 评级通关 · 《%s》", data.Grade, data.MovieTitle))
	b.Divider()

	b.Paragraph(fmt.Sprintf("🎁 你获得了 %d 个通关盲盒", data.ItemCount))
	b.Divider()

	b.BoldParagraph("你怎么选？")
	b.Paragraph("  📦 稳妥收下 — 落袋为安，零风险")
	b.Paragraph("  🎰 双倍或归零 — 50% 翻倍 / 50% 归零")
	b.Paragraph("  💀 三倍豪赌 — 30% 三倍 / 70% 腰斩")
	b.Divider()

	b.Italic("要么稳稳地走，要么玩命地赌")

	return b.Build()
}

// ============================================================
//  🎰 双倍或归零 — 结果卡片
// ============================================================

type GambleResultCardData struct {
	Grade      string
	Items      []BlindBoxItemView
	Won        bool
	MovieTitle string
}

func BuildGambleResultCard(data GambleResultCardData) RichMessage {
	b := NewBuilder()

	if data.Won {
		b.Heading("🎉 赌赢了！", 2)
		b.BoldParagraph(fmt.Sprintf("🏆 %s · 《%s》", data.Grade, data.MovieTitle))
		b.Divider()
		b.BoldParagraph(fmt.Sprintf("🎁 奖励翻倍！获得 %d 个盲盒", len(data.Items)))
		b.Divider()

		for _, item := range data.Items {
			rarityEmoji := map[string]string{
				"SSR": "🌈", "SR": "💎", "R": "⭐", "N": "📀",
			}[item.Rarity]
			if rarityEmoji == "" {
				rarityEmoji = "📀"
			}
			b.BoldParagraph(fmt.Sprintf("%s %s · %s (%d)", rarityEmoji, item.Rarity, item.Title, item.Year))
			if item.Genres != "" && item.Genres != "/" {
				b.Italic(fmt.Sprintf("  %s  ⭐ %.1f", item.Genres, item.Rating))
			}
		}

		b.Divider()
		b.Italic("命运眷顾勇者——你敢赌，它就敢给你")
	} else {
		b.Heading("💸 归零！", 2)
		b.BoldParagraph(fmt.Sprintf("🏆 %s · 《%s》", data.Grade, data.MovieTitle))
		b.Divider()
		b.Paragraph("🎰 赌局结果：归零")
		b.Paragraph("  所有的盲盒化为乌有...")
		b.Divider()
		b.Italic("本局赌注已结算")
	}

	return b.Build()
}
