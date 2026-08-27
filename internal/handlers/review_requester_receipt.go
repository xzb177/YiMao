package handlers

import (
	"fmt"
	"strings"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/richmessage"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
)

// requesterReceiptTitle renders the media title line used on the requester's
// submission receipt card.

func reviewPosterURL(review *services.ReviewRequest) string {
	if review == nil {
		return ""
	}
	path := strings.TrimSpace(review.PosterPath)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return "https://image.tmdb.org/t/p/w500" + path
}

func requesterReceiptCard(review *services.ReviewRequest, status, footer string) richmessage.Card {
	mediaType := ""
	season := 0
	year := 0
	title := ""
	if review != nil {
		mediaType = string(review.MediaType)
		season = review.Season
		year = review.MediaYear
		title = review.MediaTitle
	}
	return richmessage.BuildRequesterReceiptCard(title, year, mediaType, season, status, footer, reviewPosterURL(review))
}

func requesterReceiptResponse(review *services.ReviewRequest, status, footer string, delivered func(*types.TelegramMessage)) *callback.Response {
	card := requesterReceiptCard(review, status, footer)
	kb := services.NewKeyboardBuilder()
	kb.AddButton("求片进度", "requests")
	kb.AddButton("主菜单", "start")
	return &callback.Response{
		Text:                  card.Markdown,
		RichMessage:           card.Markdown,
		StructuredRichMessage: card.Input(),
		Keyboard:              convertKeyboard(kb.Build()),
		CallbackMsg:           status,
		OnDelivered:           delivered,
	}
}

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

// updateRequesterReceipt edits the requester original submission receipt in place.
func (h *ReviewHandler) updateRequesterReceipt(review *services.ReviewRequest, headline, statusLine, footer string) {
	if h == nil || h.telegram == nil || review == nil {
		return
	}
	if review.RequesterChatID == 0 || (review.RequesterReceiptMsgID == 0 && review.RequesterReceiptEphemeralID == 0) {
		logger.Info("[ReviewHandler] skip receipt update request=%s user=%d", review.RequestID, review.TelegramID)
		return
	}
	status := strings.TrimSpace(statusLine)
	if status == "" {
		status = strings.TrimSpace(headline)
	}
	card := requesterReceiptCard(review, status, footer)
	kb := services.NewKeyboardBuilder()
	kb.AddButton("求片进度", "requests")
	kb.AddButton("主菜单", "start")
	keyboard := kb.Build()
	input := card.Input()
	if review.RequesterReceiptEphemeralID != 0 {
		if err := h.telegram.EditEphemeralRichMessage(review.RequesterChatID, review.TelegramID, review.RequesterReceiptEphemeralID, input, keyboard); err != nil {
			if err2 := h.telegram.EditEphemeralMessageText(review.RequesterChatID, review.TelegramID, review.RequesterReceiptEphemeralID, card.Markdown, "", keyboard); err2 != nil {
				logger.Warn("[ReviewHandler] ephemeral receipt update failed chat=%d ephemeral=%d: %v / %v", review.RequesterChatID, review.RequesterReceiptEphemeralID, err, err2)
				return
			}
		}
		logger.Info("[ReviewHandler] updated ephemeral receipt request=%s", review.RequestID)
		return
	}
	if _, err := h.telegram.EditMessageRich(review.RequesterChatID, review.RequesterReceiptMsgID, input, keyboard); err != nil {
		if _, err2 := h.telegram.EditMessage(review.RequesterChatID, review.RequesterReceiptMsgID, card.Markdown, "", keyboard); err2 != nil {
			logger.Warn("[ReviewHandler] receipt update failed chat=%d message=%d: %v / %v", review.RequesterChatID, review.RequesterReceiptMsgID, err, err2)
			return
		}
	}
	logger.Info("[ReviewHandler] updated receipt request=%s", review.RequestID)
}
