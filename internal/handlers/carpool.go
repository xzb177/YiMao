package handlers

import (
	"fmt"
	"sync"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/pkg/logger"
)

// 本文件实现「拼车 +1」回调处理（Batch A #3）。
//
// 回调格式（沿用项目现有 colon 编码：action:key:value:...）：
//   carpool:id:<tmdbID>:type:<movie|tv>
// 点击后把用户加入该片的拼车列表，并回一个 toast「已加入，共 N 人想看」。

// CarpoolHandler 处理「我也想看 +1」按钮点击。
type CarpoolHandler struct {
	carpool  *services.CarpoolService
	telegram *services.TelegramClient // #1 用于「打招呼」打通私聊通道（可选注入，允许为 nil）

	// #1 已打招呼用户去重（进程内）：避免每次 +1 都给同一用户发问候，造成骚扰。
	// 仅作「尽力而为」用途，进程重启后重置可接受（最多多发一次问候，不影响主流程）。
	greeted   map[int64]bool
	greetedMu sync.Mutex
}

// NewCarpoolHandler 创建拼车回调处理器。
func NewCarpoolHandler(carpool *services.CarpoolService) *CarpoolHandler {
	return &CarpoolHandler{carpool: carpool, greeted: make(map[int64]bool)}
}

// SetTelegram 注入 TelegramClient（#1 拼车 +1 时尝试打通私聊通道）。setter 注入不改构造签名。
func (h *CarpoolHandler) SetTelegram(t *services.TelegramClient) {
	h.telegram = t
}

// Handle 处理 carpool 回调。
func (h *CarpoolHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	if h.carpool == nil {
		logger.Info("[CarpoolHandler] ERROR: carpool service is nil!")
		return &callback.Response{
			CallbackMsg: "服务未就绪",
			ShowAlert:   true,
		}, nil
	}

	idStr, hasID := ctx.Callback.Params["id"]
	mediaType := ctx.Callback.Params["type"]
	if !hasID {
		return &callback.Response{
			CallbackMsg: "参数无效",
			ShowAlert:   true,
		}, nil
	}

	tmdbID := 0
	fmt.Sscanf(idStr, "%d", &tmdbID)
	if tmdbID == 0 {
		return &callback.Response{
			CallbackMsg: "无效的影片ID",
			ShowAlert:   true,
		}, nil
	}
	if mediaType == "" {
		mediaType = "movie"
	}

	count := h.carpool.Add(tmdbID, mediaType, ctx.UserID)
	logger.Info("[Carpool] 用户 %d 加入拼车: tmdbID=%d type=%s, 当前共 %d 人", ctx.UserID, tmdbID, mediaType, count)

	// #1 通路缺口修复：拼车 +1 时尽力打通用户与 bot 的私聊通道（失败不阻断 +1，仅记日志）。
	// 这样到货时除群内 @ 外，还能给已通私聊的用户发 direct link 兜底。
	h.tryOpenDMChannel(ctx.UserID)

	// 只回 toast，不改动原消息（用户可重复点击，去重后人数不变）。
	return &callback.Response{
		CallbackMsg: fmt.Sprintf("✅ 已加入，共 %d 人想看", count),
		ShowAlert:   false,
	}, nil
}

// tryOpenDMChannel 尽力给拼车用户发一条「打招呼」私信，打通后续通知通道（#1）。
// 进程内对同一用户只发一次（greeted 去重），避免反复 +1 时骚扰。
// 失败（未私聊过 / 封禁 → 403 等）仅记日志，绝不影响 +1 主流程。
func (h *CarpoolHandler) tryOpenDMChannel(userID int64) {
	if h.telegram == nil || userID == 0 {
		return
	}
	// 去重：本进程已问候过则跳过。
	h.greetedMu.Lock()
	if h.greeted[userID] {
		h.greetedMu.Unlock()
		return
	}
	h.greeted[userID] = true
	h.greetedMu.Unlock()

	greeting := "你好，这里是云海求片。\n你已加入这部影片的求片记录；到货时会通知你。"
	if _, err := h.telegram.SendMessage(userID, greeting, "", nil); err != nil {
		// 发失败说明还没建立私聊（或被封禁）：撤销 greeted 标记，下次 +1 可再试一次。
		h.greetedMu.Lock()
		delete(h.greeted, userID)
		h.greetedMu.Unlock()
		logger.Info("[Carpool] 打招呼私信失败（用户可能未私聊过 bot，不影响 +1）user=%d: %v", userID, err)
		return
	}
	logger.Info("[Carpool] 已向 user=%d 发送打招呼私信，私聊通道已打通", userID)
}
