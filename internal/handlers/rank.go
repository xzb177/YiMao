package handlers

import (
	"fmt"
	"strings"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
)

// RankHandler 冒险者公会排行处理器
type RankHandler struct {
	socialDB    *services.SocialDB
	telegram    *services.TelegramClient
	userMapping services.UserMappingStore
	groupChatID int64
}

// NewRankHandler 创建排行处理器
func NewRankHandler(
	socialDB *services.SocialDB,
	telegram *services.TelegramClient,
	userMapping services.UserMappingStore,
	groupChatID int64,
) *RankHandler {
	return &RankHandler{
		socialDB:    socialDB,
		telegram:    telegram,
		userMapping: userMapping,
		groupChatID: groupChatID,
	}
}

// HandleCommand 处理 /rank 命令
func (h *RankHandler) HandleCommand(chatID, userID int64) {
	if h.socialDB == nil {
		h.telegram.SendMessage(chatID, "❌ 排行服务未就绪", "", nil)
		return
	}

	sssEntries, _ := h.socialDB.GetWeeklyLeaderboard("sss", 5)
	scoreEntries, _ := h.socialDB.GetWeeklyLeaderboard("score", 5)
	vengeanceEntries, _ := h.socialDB.GetWeeklyLeaderboard("vengeance", 3)

	var sb strings.Builder
	sb.WriteString("🏛️ 冒险者公会 · 本周排行\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n\n")

	sb.WriteString("🏆 SSS猎人榜\n")
	if len(sssEntries) == 0 {
		sb.WriteString("  暂无记录\n")
	} else {
		for _, e := range sssEntries {
			name := h.getUserName(e.UserID)
			medal := map[int]string{1: "🥇", 2: "🥈", 3: "🥉"}[e.Rank]
			if medal == "" {
				medal = fmt.Sprintf("%d.", e.Rank)
			}
			sb.WriteString(fmt.Sprintf("  %s %s · %d次SSS\n", medal, name, e.Value))
		}
	}

	sb.WriteString("\n⚔️ 本周积分榜\n")
	if len(scoreEntries) == 0 {
		sb.WriteString("  暂无记录\n")
	} else {
		for _, e := range scoreEntries {
			name := h.getUserName(e.UserID)
			medal := map[int]string{1: "🥇", 2: "🥈", 3: "🥉"}[e.Rank]
			if medal == "" {
				medal = fmt.Sprintf("%d.", e.Rank)
			}
			sb.WriteString(fmt.Sprintf("  %s %s · %d分\n", medal, name, e.Value))
		}
	}

	sb.WriteString("\n💀 宿敌终结者\n")
	if len(vengeanceEntries) == 0 {
		sb.WriteString("  暂无记录\n")
	} else {
		for _, e := range vengeanceEntries {
			name := h.getUserName(e.UserID)
			sb.WriteString(fmt.Sprintf("  ⚔️ %s · 复仇%d次\n", name, e.Value))
		}
	}

	sb.WriteString("\n━━━━━━━━━━━━━━━━━━━━\n")
	sb.WriteString("📅 每周一凌晨重置 · 下一局见")

	h.telegram.SendMessage(chatID, sb.String(), "", nil)
}

func (h *RankHandler) getUserName(userID int64) string {
	if h.userMapping != nil {
		if name, err := h.userMapping.GetMoviePilotUsername(userID); err == nil && name != "" {
			return name
		}
	}
	return fmt.Sprintf("用户%d", userID)
}

// Handle 处理排行回调（从游戏中心进入）
func (h *RankHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	go h.HandleCommand(ctx.ChatID, ctx.UserID)
	return &callback.Response{CallbackMsg: "📊 正在生成排行榜...", ShowAlert: false}, nil
}
