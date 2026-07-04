package handlers

import (
	"fmt"
	"strings"

	"github.com/xzb177/yimao/internal/services"
)

// StatsHandler 个人面板处理器
type StatsHandler struct {
	socialDB    *services.SocialDB
	telegram    *services.TelegramClient
	userMapping services.UserMappingStore
}

// NewStatsHandler 创建个人面板处理器
func NewStatsHandler(
	socialDB *services.SocialDB,
	telegram *services.TelegramClient,
	userMapping services.UserMappingStore,
) *StatsHandler {
	return &StatsHandler{
		socialDB:    socialDB,
		telegram:    telegram,
		userMapping: userMapping,
	}
}

// HandleCommand 处理 /mystats 命令
func (h *StatsHandler) HandleCommand(chatID, userID int64) {
	if h.socialDB == nil {
		h.telegram.SendMessage(chatID, "❌ 统计服务未就绪", "", nil)
		return
	}

	total, wins, sss, maxScore, maxCombo, streakDays, bestStreak := h.socialDB.GetUserAdventureStats(userID)
	userName := h.getUserName(userID)

	if total == 0 {
		h.telegram.SendMessage(chatID, "📜 你还没有开始冒险\n\n发送 /adventure 开始你的第一次冒险！", "", nil)
		return
	}

	// 连胜称号
	streakRewards := services.GetStreakRewards(streakDays)
	title := streakRewards.FlameLevel + "行者"

	// 周排名
	sssRank := h.socialDB.GetUserWeeklyRank(userID, "sss")

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📜 冒险者档案：@%s\n", userName))
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n")
	sb.WriteString(fmt.Sprintf("🏆 称号：%s（%d天连胜中）\n", title, streakDays))
	sb.WriteString(fmt.Sprintf("⚔️ 总冒险：%d次 | 通关：%d次\n", total, wins))
	sb.WriteString(fmt.Sprintf("🎯 SSS次数：%d | 最高分：%d\n", sss, maxScore))
	if maxCombo > 0 {
		sb.WriteString(fmt.Sprintf("🔥 最大连击：x%d\n", maxCombo))
	}
	sb.WriteString(fmt.Sprintf("📅 连胜：%d天 | 最佳：%d天\n", streakDays, bestStreak))
	if sssRank > 0 {
		sb.WriteString(fmt.Sprintf("📊 本周排名：#%d SSS猎人\n", sssRank))
	}
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n")

	// 宿敌墙
	nemeses, err := h.socialDB.GetAllNemeses(userID)
	if err == nil && len(nemeses) > 0 {
		sb.WriteString("宿敌墙：\n")
		for _, n := range nemeses {
			revengedIcon := "☠️"
			if n.Revenged {
				revengedIcon = "💀已复仇"
			}
			sb.WriteString(fmt.Sprintf("  %s %s — %d次\n", revengedIcon, n.MovieName, n.FailCount))
		}
	}

	h.telegram.SendMessage(chatID, sb.String(), "", nil)
}

func (h *StatsHandler) getUserName(userID int64) string {
	if h.userMapping != nil {
		if name, err := h.userMapping.GetMoviePilotUsername(userID); err == nil && name != "" {
			return name
		}
	}
	return fmt.Sprintf("用户%d", userID)
}
