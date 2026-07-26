package handlers

import (
	"fmt"

	"github.com/xzb177/yimao/internal/services"
)

// DreamHandler 本周特别挑战处理器
type DreamHandler struct {
	socialDB     *services.SocialDB
	telegram     *services.TelegramClient
	adventureHdl *AdventureHandler
	userMapping  services.UserMappingStore
}

// NewDreamHandler 创建本周挑战处理器
func NewDreamHandler(
	socialDB *services.SocialDB,
	telegram *services.TelegramClient,
	adventureHdl *AdventureHandler,
	userMapping services.UserMappingStore,
) *DreamHandler {
	return &DreamHandler{
		socialDB:     socialDB,
		telegram:     telegram,
		adventureHdl: adventureHdl,
		userMapping:  userMapping,
	}
}

// HandleCommand 处理 /dream 命令
func (h *DreamHandler) HandleCommand(chatID, userID int64) {
	if h.socialDB == nil || h.adventureHdl == nil {
		_, _ = h.telegram.SendMessage(chatID, "❌ 本周挑战暂不可用，请稍后再试", "", nil)
		return
	}

	wb, err := h.socialDB.GetWeeklyBoss()
	if err != nil || wb == nil {
		_, _ = h.telegram.SendMessage(chatID, "🎯 本周挑战尚未更新\n\n新片单会在每周一生成。", "", nil)
		return
	}

	text := fmt.Sprintf("🎯 本周挑战\n──────────────────\n本周片单：《%s》 (%d)\n\n难度加成 30%% · 通关奖励翻倍\n完成记录会计入本周挑战。\n──────────────────", wb.MovieName, wb.MovieYear)
	_, _ = h.telegram.SendMessage(chatID, text, "", nil)

	// 直接发起冒险（梦魇模式）
	h.adventureHdl.startWeeklyBossAsync(userID, chatID, wb)
}

func (h *DreamHandler) getUserName(userID int64) string {
	if h.userMapping != nil {
		if name, err := h.userMapping.GetMoviePilotUsername(userID); err == nil && name != "" {
			return name
		}
	}
	return fmt.Sprintf("用户%d", userID)
}
