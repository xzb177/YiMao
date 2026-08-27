package handlers

import (
	"fmt"

	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/pkg/logger"
)

// requesterReceiptTitle renders the media title line used on the requester's
// submission receipt card.
func requesterReceiptTitle(review *services.ReviewRequest) string {
	icon := "🎬"
	if review.MediaType == services.MediaTypeTV {
		icon = "📺"
	}
	title := fmt.Sprintf("%s 《%s》", icon, review.MediaTitle)
	if review.MediaYear > 0 {
		title += fmt.Sprintf(" (%d)", review.MediaYear)
	}
	if review.MediaType == services.MediaTypeTV && review.Season > 0 {
		title += fmt.Sprintf(" · 第 %d 季", review.Season)
	}
	return title
}

// buildRequesterReceiptText renders the requester receipt card for a concrete
// review outcome. The status line replaces「等待管理员审核」so the requester's
// original card can never keep claiming the request is still pending.
func buildRequesterReceiptText(review *services.ReviewRequest, headline, statusLine, footer string) string {
	text := fmt.Sprintf("%s\n\n%s", headline, requesterReceiptTitle(review))
	if statusLine != "" {
		text += "\n📋 状态：" + statusLine
	}
	if footer != "" {
		text += "\n\n" + footer
	}
	return text
}

// updateRequesterReceipt edits the requester's original submission receipt in
// place. Approving an admin message must never be the only visible outcome: the
// requester card is the message the user actually keeps looking at.
func (h *ReviewHandler) updateRequesterReceipt(review *services.ReviewRequest, headline, statusLine, footer string) {
	if h == nil || h.telegram == nil || review == nil {
		return
	}
	if review.RequesterChatID == 0 || (review.RequesterReceiptMsgID == 0 && review.RequesterReceiptEphemeralID == 0) {
		logger.Info("[ReviewHandler] 无申请人回执坐标，跳过原消息更新: request=%s user=%d", review.RequestID, review.TelegramID)
		return
	}
	kb := services.NewKeyboardBuilder()
	kb.AddButton("📊 求片进度", "requests")
	kb.AddButton("🏠 主菜单", "start")
	text := buildRequesterReceiptText(review, headline, statusLine, footer)
	if review.RequesterReceiptEphemeralID != 0 {
		if err := h.telegram.EditEphemeralMessageText(review.RequesterChatID, review.TelegramID, review.RequesterReceiptEphemeralID, text, "", kb.Build()); err != nil {
			logger.Warn("[ReviewHandler] 申请人回执更新失败 chat=%d ephemeral=%d: %v", review.RequesterChatID, review.RequesterReceiptEphemeralID, err)
			return
		}
		logger.Info("[ReviewHandler] 已更新申请人回执: request=%s chat=%d ephemeral=%d", review.RequestID, review.RequesterChatID, review.RequesterReceiptEphemeralID)
		return
	}
	if _, err := h.telegram.EditMessage(review.RequesterChatID, review.RequesterReceiptMsgID, text, "", kb.Build()); err != nil {
		logger.Warn("[ReviewHandler] 申请人回执更新失败 chat=%d message=%d: %v", review.RequesterChatID, review.RequesterReceiptMsgID, err)
		return
	}
	logger.Info("[ReviewHandler] 已更新申请人回执: request=%s chat=%d message=%d", review.RequestID, review.RequesterChatID, review.RequesterReceiptMsgID)
}
