package handlers

import (
	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/pkg/logger"
)

// WatchFollowupHandler 处理入库回访「看了吗」按钮的回调。
// 回调格式：watch_fb:id:<requestID>:a:<w|l|d>
//   - w = 看完了  l = 还没看  d = 不想看了
type WatchFollowupHandler struct {
	stats  *services.FulfillmentStatsService
	review *services.ReviewService
}

// NewWatchFollowupHandler 创建入库回访处理器。
func NewWatchFollowupHandler(stats *services.FulfillmentStatsService, review *services.ReviewService) *WatchFollowupHandler {
	return &WatchFollowupHandler{stats: stats, review: review}
}

// Handle 记录回答并给一句人味的收尾；按钮消失（编辑原消息）。
func (h *WatchFollowupHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	if h == nil || h.review == nil {
		return &callback.Response{CallbackMsg: "回访服务未就绪", ShowAlert: true}, nil
	}
	requestID := ctx.Callback.Params["id"]
	answer := ctx.Callback.Params["a"]

	valid := map[string]string{
		"w": "🎉 太好了！口味数据记下了，之后推荐会更懂你",
		"l": "🍿 不急，片子在库里随时等你",
		"d": "👌 收到，这类内容以后会少打扰你",
	}
	reply, ok := valid[answer]
	if !ok || requestID == "" {
		return &callback.Response{CallbackMsg: "这个按钮已失效", ShowAlert: true}, nil
	}

	title := ""
	if h.review != nil {
		rv, exists := h.review.GetRequest(requestID)
		if !exists || rv == nil || rv.TelegramID != ctx.UserID {
			// 消息转发/按钮伪造：只有原求片人能回答，防污染偏好数据。
			return &callback.Response{CallbackMsg: "这不是你的回访", ShowAlert: true}, nil
		}
		title = rv.MediaTitle
	}
	if h.stats != nil {
		h.stats.AddWatchFeedbackTitled(requestID, title, answer)
	}
	logger.Info("[WatchFollowup] 用户 %d 回访回答: request=%s answer=%s", ctx.UserID, requestID, answer)

	return &callback.Response{
		Text:     reply,
		Edit:     true,
		Keyboard: &callback.Keyboard{},
	}, nil
}
