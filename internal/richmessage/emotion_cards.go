package richmessage

import (
	"fmt"
	"strings"
)

// ============================================================
//  叙事型卡片模板
//  从"展示数据"升级为"讲故事"
// ============================================================

// GenreCountView 类型计数视图
type GenreCountView struct {
	Genre string
	Count int
}

// EmotionProfileCardData 情绪画像卡片数据
type EmotionProfileCardData struct {
	UserName         string
	PersonalityTag   string
	SignatureGenre   string
	EmotionalIntensity float64
	EmotionTrend     string
	CurrentMood      string
	LifePhase        string
	TopGenres        []GenreCountView
	MovieCount       int
	SeriesCount      int
	WatchDays        int
	WatchStreak      int
	Pattern          ViewingPatternView
	Transitions      []GenreTransitionView
}

// ViewingPatternView 观影模式视图
type ViewingPatternView struct {
	PeakHour    int
	PeakPeriod  string
	WeekdayAvg  float64
	WeekendAvg  float64
	IsNightOwl  bool
}

// GenreTransitionView 类型转变视图
type GenreTransitionView struct {
	From      string
	To        string
	Direction string
}

// BuildEmotionProfileCard 构建情绪画像卡片
func BuildEmotionProfileCard(data EmotionProfileCardData) RichMessage {
	builder := NewBuilder()

	// 标题：用性格标签而非段位
	builder.Heading(fmt.Sprintf("🪞 %s", data.PersonalityTag), 2)
	builder.BoldParagraph(fmt.Sprintf("👤 %s 的观影人格", data.UserName))
	builder.Divider()

	// 当前情绪状态 — 用叙事方式
	builder.Paragraph(data.CurrentMood)
	builder.Divider()

	// 人生阶段 — 核心叙事
	builder.Paragraph(fmt.Sprintf("📍 %s", data.LifePhase))
	builder.Divider()

	// 情绪强度可视化
	bar := buildEmotionBar(data.EmotionalIntensity)
	trendIcon := map[string]string{"上升": "📈", "下降": "📉", "平稳": "📊"}[data.EmotionTrend]
	builder.Paragraph(fmt.Sprintf("%s 情绪强度 %s %.1f/10", trendIcon, bar, data.EmotionalIntensity))
	builder.Divider()

	// 观影模式
	if data.Pattern.IsNightOwl {
		builder.Paragraph(fmt.Sprintf("🌙 夜猫子 · %s最活跃", data.Pattern.PeakPeriod))
	} else {
		builder.Paragraph(fmt.Sprintf("☀️ 规律作息 · %s观影", data.Pattern.PeakPeriod))
	}
	if data.WatchStreak >= 3 {
		builder.Paragraph(fmt.Sprintf("🔥 连续观影 %d 天", data.WatchStreak))
	}
	builder.Divider()

	// 类型偏好 — 只显示top3
	if len(data.TopGenres) > 0 {
		top3 := data.TopGenres
		if len(top3) > 3 {
			top3 = top3[:3]
		}
		var parts []string
		for _, g := range top3 {
			parts = append(parts, fmt.Sprintf("%s(%d)", g.Genre, g.Count))
		}
		builder.Paragraph(fmt.Sprintf("🎭 %s", strings.Join(parts, " · ")))
	}

	// 类型转变
	if len(data.Transitions) > 0 {
		builder.Divider()
		for _, t := range data.Transitions {
			builder.Paragraph(fmt.Sprintf("🔄 %s → %s（%s）", t.From, t.To, t.Direction))
		}
	}

	return builder.Build()
}

// buildEmotionBar 构建情绪强度进度条
func buildEmotionBar(intensity float64) string {
	filled := int(intensity)
	if filled > 10 {
		filled = 10
	}
	if filled < 0 {
		filled = 0
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)
}

// TimeMachineCardData 时光放映机卡片数据
type TimeMachineCardData struct {
	UserName  string
	Narrative string
	WeekRange string
}

// BuildTimeMachineCard 构建时光放映机卡片
func BuildTimeMachineCard(data TimeMachineCardData) RichMessage {
	builder := NewBuilder()

	builder.Heading(fmt.Sprintf("📽️ %s 的观影故事", data.UserName), 2)
	builder.Italic(data.WeekRange)
	builder.Divider()

	// 叙事内容按段落拆分
	paragraphs := strings.Split(data.Narrative, "\n")
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// 标题行（以 emoji 开头的短行）
		if len([]rune(p)) < 3 && len(paragraphs) > 1 {
			continue
		}
		builder.Paragraph(p)
	}

	builder.Divider()
	builder.Italic("🎬 每周日晚更新 · 你的观影故事，由你的选择书写")

	return builder.Build()
}

type PrescriptionItem struct {
	Title    string
	Year     int
	Rating   float64
	Genres   string
	Overview string
	Rarity   string
}

// PrescriptionCardDataV2 处方卡片数据（v2，无循环依赖）
type PrescriptionCardDataV2 struct {
	UserName     string
	Diagnosis    string
	Intensity    float64
	Trend        string
	Items        []PrescriptionItem
}

// BuildPrescriptionCard 构建情绪处方卡片
func BuildPrescriptionCard(data PrescriptionCardDataV2) RichMessage {
	builder := NewBuilder()

	builder.Heading("💊 情绪处方", 2)
	if data.UserName != "" {
		builder.BoldParagraph(fmt.Sprintf("👤 %s", data.UserName))
		builder.Divider()
	}

	// 诊断
	builder.Paragraph(fmt.Sprintf("📋 %s", data.Diagnosis))
	builder.Divider()

	// 情绪状态
	bar := buildEmotionBar(data.Intensity)
	builder.Paragraph(fmt.Sprintf("情绪强度: %s %.1f/10 (%s)", bar, data.Intensity, data.Trend))
	builder.Divider()

	// 处方药品（电影）
	builder.BoldParagraph("💊 今日处方")
	for i, item := range data.Items {
		rarityIcon := map[string]string{
			"N": "⚪", "R": "🔵", "SR": "🟣", "SSR": "🟡",
		}[item.Rarity]
		if rarityIcon == "" {
			rarityIcon = "⚪"
		}

		builder.Paragraph(fmt.Sprintf("%s **%s** (%d) %s\n⭐ %.1f · %s",
			rarityIcon, item.Title, item.Year, item.Rarity, item.Rating, item.Genres))
		if item.Overview != "" {
			overview := item.Overview
			if len([]rune(overview)) > 100 {
				overview = string([]rune(overview)[:100]) + "..."
			}
			builder.Paragraph(fmt.Sprintf("  📖 %s", overview))
		}
		if i < len(data.Items)-1 {
			builder.Divider()
		}
	}

	builder.Divider()
	builder.Italic("💡 建议今晚服用，一次一剂")

	return builder.Build()
}

// ContractCardData 命运契约卡片数据
type ContractCardData struct {
	MovieName  string
	Year       int
	Rating     float64
	Genres     string
	Overview   string
	Challenge  string  // 挑战内容
	Deadline   string  // 截止时间
	Reward     string  // 奖励描述
	SpinCount  int
	MaxSpins   int
}

// BuildContractCard 构建命运契约卡片
func BuildContractCard(data ContractCardData) RichMessage {
	builder := NewBuilder()

	builder.Heading("📜 命运契约", 2)
	builder.BoldParagraph(fmt.Sprintf("🎰 今日第 %d/%d 次转盘", data.SpinCount, data.MaxSpins))
	builder.Divider()

	// 命运选中的电影
	builder.Heading(fmt.Sprintf("🎬 %s (%d)", data.MovieName, data.Year), 3)
	if data.Rating > 0 {
		ratingLine := fmt.Sprintf("⭐ TMDB %.1f", data.Rating)
		if data.Genres != "" {
			ratingLine += " · " + data.Genres
		}
		builder.Paragraph(ratingLine)
	}

	// 简介
	if data.Overview != "" {
		overview := data.Overview
		if len([]rune(overview)) > 300 {
			overview = string([]rune(overview)[:300]) + "..."
		}
		builder.Paragraph(overview)
	}
	builder.Divider()

	// 契约条款
	builder.BoldParagraph("📜 契约条款")
	builder.Paragraph(fmt.Sprintf("🎯 挑战：%s", data.Challenge))
	builder.Paragraph(fmt.Sprintf("⏰ 截止：%s", data.Deadline))
	builder.Paragraph(fmt.Sprintf("🎁 奖励：%s", data.Reward))
	builder.Divider()

	remaining := data.MaxSpins - data.SpinCount
	if remaining > 0 {
		builder.Italic(fmt.Sprintf("🎡 今日剩余 %d 次签约机会", remaining))
	} else {
		builder.Italic("🎡 今日签约次数已用完，明天再来")
	}

	return builder.Build()
}

// RelationshipCardData 观影关系卡片数据
type RelationshipCardData struct {
	User1Name     string
	User2Name     string
	Overlap       int      // 共同观看数量
	OverlapRate   float64  // 重合度
	User1Top      []string // 用户1的top类型
	User2Top      []string // 用户2的top类型
	Divergence    string   // "正在趋同" / "正在分化" / "稳定"
	Relationship  string   // 关系描述
}

// BuildRelationshipCard 构建观影关系卡片
func BuildRelationshipCard(data RelationshipCardData) RichMessage {
	builder := NewBuilder()

	builder.Heading("🤝 观影关系", 2)
	builder.BoldParagraph(fmt.Sprintf("%s × %s", data.User1Name, data.User2Name))
	builder.Divider()

	// 重合度
	builder.Paragraph(fmt.Sprintf("🎯 共同观看：%d 部", data.Overlap))
	builder.Paragraph(fmt.Sprintf("📊 重合度：%.0f%%", data.OverlapRate))
	builder.Divider()

	// 类型对比
	builder.BoldParagraph("🎭 类型偏好")
	builder.Paragraph(fmt.Sprintf("👤 %s：%s", data.User1Name, strings.Join(data.User1Top, " · ")))
	builder.Paragraph(fmt.Sprintf("👤 %s：%s", data.User2Name, strings.Join(data.User2Top, " · ")))
	builder.Divider()

	// 趋势
	trendIcon := map[string]string{
		"正在趋同": "🧲", "正在分化": "🔀", "稳定": "⚖️",
	}[data.Divergence]
	builder.Paragraph(fmt.Sprintf("%s %s", trendIcon, data.Divergence))
	builder.Divider()

	// 关系描述
	builder.Italic(data.Relationship)

	return builder.Build()
}
