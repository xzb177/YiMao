package handlers

import (
	"fmt"

	"github.com/xzb177/yimao/internal/services"
)

// DreamHandler 本周梦魇处理器
type DreamHandler struct {
	socialDB     *services.SocialDB
	telegram     *services.TelegramClient
	adventureHdl *AdventureHandler
	userMapping  services.UserMappingStore
}

// NewDreamHandler 创建梦魇处理器
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
		h.telegram.SendMessage(chatID, "❌ 梦魇服务未就绪", "", nil)
		return
	}

	wb, err := h.socialDB.GetWeeklyBoss()
	if err != nil || wb == nil {
		h.telegram.SendMessage(chatID, "🎪 本周暂无梦魇\n\n梦魇会在每周一自动生成\n敬请期待", "", nil)
		return
	}

	text := fmt.Sprintf("🎪 本周梦魇：你逃不掉的\n━━━━━━━━━━━━━━━━━━━\n这周，有部片子等着你\n《%s》 (%d)\n\n难度 +30%% · 奖励翻倍\n敢来？通关双倍，失败……你知道后果\n━━━━━━━━━━━━━━━━━━━\n/dream 开始挑战", wb.MovieName, wb.MovieYear)
	h.telegram.SendMessage(chatID, text, "", nil)

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
