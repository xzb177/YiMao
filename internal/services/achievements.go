package services

import (
	"fmt"
	"strings"
)

// ============================================================
//  成就系统 (Achievement System)
// ============================================================

// Achievement 成就定义
type Achievement struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Category    string `json:"category"` // watch, social, explore, challenge
	Condition   string `json:"condition"`
	XP          int    `json:"xp"`
}

// UserAchievement 用户成就
type UserAchievement struct {
	Achievement
	UnlockedAt string `json:"unlocked_at"`
}

// 所有成就定义
var AllAchievements = []Achievement{
	// 观影成就
	{
		ID:          "first_movie",
		Name:        "初次观影",
		Description: "观看你的第一部电影",
		Icon:        "🎬",
		Category:    "watch",
		Condition:   "观看1部电影",
		XP:          5,
	},
	{
		ID:          "movie_10",
		Name:        "小有成就",
		Description: "观看10部电影",
		Icon:        "🎥",
		Category:    "watch",
		Condition:   "观看10部电影",
		XP:          10,
	},
	{
		ID:          "movie_50",
		Name:        "资深影迷",
		Description: "观看50部电影",
		Icon:        "📽️",
		Category:    "watch",
		Condition:   "观看50部电影",
		XP:          20,
	},
	{
		ID:          "movie_100",
		Name:        "百片斩",
		Description: "观看100部电影",
		Icon:        "🎭",
		Category:    "watch",
		Condition:   "观看100部电影",
		XP:          30,
	},
	{
		ID:          "series_10",
		Name:        "剧集达人",
		Description: "观看10部剧集",
		Icon:        "📺",
		Category:    "watch",
		Condition:   "观看10部剧集",
		XP:          15,
	},
	{
		ID:          "genre_explorer",
		Name:        "类型探索者",
		Description: "涉猎5种以上类型",
		Icon:        "🌈",
		Category:    "watch",
		Condition:   "涉猎5种以上类型",
		XP:          15,
	},
	{
		ID:          "genre_master",
		Name:        "全类型大师",
		Description: "涉猎10种以上类型",
		Icon:        "🌍",
		Category:    "watch",
		Condition:   "涉猎10种以上类型",
		XP:          25,
	},
	{
		ID:          "rating_9",
		Name:        "品质鉴赏家",
		Description: "平均评分9.0以上",
		Icon:        "⭐",
		Category:    "watch",
		Condition:   "平均评分9.0以上",
		XP:          20,
	},

	// 社交成就
	{
		ID:          "first_review",
		Name:        "初次点评",
		Description: "写下第一篇影评",
		Icon:        "✍️",
		Category:    "social",
		Condition:   "写1篇影评",
		XP:          5,
	},
	{
		ID:          "review_10",
		Name:        "影评达人",
		Description: "写下10篇影评",
		Icon:        "📝",
		Category:    "social",
		Condition:   "写10篇影评",
		XP:          15,
	},
	{
		ID:          "first_contract",
		Name:        "契约新手",
		Description: "签定第一份命运契约",
		Icon:        "📜",
		Category:    "social",
		Condition:   "签定1份契约",
		XP:          5,
	},
	{
		ID:          "contract_5",
		Name:        "契约达人",
		Description: "完成5份命运契约",
		Icon:        "📋",
		Category:    "social",
		Condition:   "完成5份契约",
		XP:          20,
	},

	// 探索成就
	{
		ID:          "first_blindbox",
		Name:        "开盒新手",
		Description: "第一次开盲盒",
		Icon:        "📦",
		Category:    "explore",
		Condition:   "开1次盲盒",
		XP:          5,
	},
	{
		ID:          "blindbox_10",
		Name:        "开盒达人",
		Description: "开10次盲盒",
		Icon:        "🎰",
		Category:    "explore",
		Condition:   "开10次盲盒",
		XP:          15,
	},
	{
		ID:          "ssr_hunter",
		Name:        "SSR猎人",
		Description: "开出SSR级电影",
		Icon:        "💎",
		Category:    "explore",
		Condition:   "开出SSR",
		XP:          25,
	},
	{
		ID:          "first_roulette",
		Name:        "轮盘新手",
		Description: "第一次转命运轮盘",
		Icon:        "🎡",
		Category:    "explore",
		Condition:   "转1次轮盘",
		XP:          5,
	},
	{
		ID:          "roulette_10",
		Name:        "轮盘达人",
		Description: "转10次命运轮盘",
		Icon:        "🎰",
		Category:    "explore",
		Condition:   "转10次轮盘",
		XP:          15,
	},

	// 特殊成就
	{
		ID:          "night_owl",
		Name:        "夜猫子",
		Description: "凌晨2点后观影",
		Icon:        "🦉",
		Category:    "special",
		Condition:   "凌晨观影",
		XP:          10,
	},
	{
		ID:          "early_bird",
		Name:        "早起鸟",
		Description: "早上6点前观影",
		Icon:        "🐦",
		Category:    "special",
		Condition:   "清晨观影",
		XP:          10,
	},
	{
		ID:          "marathon",
		Name:        "马拉松观影",
		Description: "一天看3部以上电影",
		Icon:        "🏃",
		Category:    "special",
		Condition:   "单日3部+",
		XP:          15,
	},
	{
		ID:          "emotion_explorer",
		Name:        "情绪探索者",
		Description: "使用情绪画像功能",
		Icon:        "🪞",
		Category:    "special",
		Condition:   "使用情绪画像",
		XP:          5,
	},
	{
		ID:          "time_traveler",
		Name:        "时光旅人",
		Description: "使用时光放映机功能",
		Icon:        "🕰️",
		Category:    "special",
		Condition:   "使用时光机",
		XP:          5,
	},
	{
		ID:          "relationship_explorer",
		Name:        "关系探索者",
		Description: "使用观影关系对比功能",
		Icon:        "👥",
		Category:    "special",
		Condition:   "使用关系对比",
		XP:          5,
	},
}

// GetAchievementsByCategory 按分类获取成就
func GetAchievementsByCategory(category string) []Achievement {
	var result []Achievement
	for _, a := range AllAchievements {
		if category == "" || a.Category == category {
			result = append(result, a)
		}
	}
	return result
}

// GetAchievementByID 根据ID获取成就
func GetAchievementByID(id string) *Achievement {
	for _, a := range AllAchievements {
		if a.ID == id {
			return &a
		}
	}
	return nil
}

// BuildAchievementCard 构建成就卡片
func BuildAchievementCard(achievements []UserAchievement, totalXP int) string {
	var sb strings.Builder

	sb.WriteString("🏆 **成就系统**\n\n")
	sb.WriteString(fmt.Sprintf("总经验值: **%d** XP\n", totalXP))
	sb.WriteString(fmt.Sprintf("已解锁: **%d** / %d\n\n", len(achievements), len(AllAchievements)))

	// 按分类组织
	categories := map[string]string{
		"watch":     "🎬 观影成就",
		"social":    "👥 社交成就",
		"explore":   "🔍 探索成就",
		"challenge": "🎯 挑战成就",
		"special":   "✨ 特殊成就",
	}

	unlockedMap := make(map[string]bool)
	for _, a := range achievements {
		unlockedMap[a.ID] = true
	}

	for cat, catName := range categories {
		catAchievements := GetAchievementsByCategory(cat)
		if len(catAchievements) == 0 {
			continue
		}

		sb.WriteString(fmt.Sprintf("**%s**\n", catName))
		for _, a := range catAchievements {
			if unlockedMap[a.ID] {
				sb.WriteString(fmt.Sprintf("%s %s ✅\n", a.Icon, a.Name))
			} else {
				sb.WriteString(fmt.Sprintf("🔒 %s\n", a.Name))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
