package handlers

import (
	"fmt"

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
	carpool *services.CarpoolService
}

// NewCarpoolHandler 创建拼车回调处理器。
func NewCarpoolHandler(carpool *services.CarpoolService) *CarpoolHandler {
	return &CarpoolHandler{carpool: carpool}
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

	// 只回 toast，不改动原消息（用户可重复点击，去重后人数不变）。
	return &callback.Response{
		CallbackMsg: fmt.Sprintf("✅ 已加入，共 %d 人想看", count),
		ShowAlert:   false,
	}, nil
}
