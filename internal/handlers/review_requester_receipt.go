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
	mediaType, season, year, title := "", 0, 0, ""
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
	return &callback.Response{
		Text: card.Markdown, RichMessage: card.Markdown, StructuredRichMessage: card.Input(), CallbackMsg: status, OnDelivered: delivered,
	}
}

func requesterReceiptTitle(review *services.ReviewRequest) string {
	title := review.MediaTitle
	if review.MediaYear > 0 {
		title += fmt.Sprintf(" (%d)", review.MediaYear)
	}
	if review.MediaType == services.MediaTypeTV && review.Season > 0 {
		title += fmt.Sprintf(" S%d", review.Season)
	}
	return title
}

func buildRequesterReceiptText(review *services.ReviewRequest, headline, statusLine, footer string) string {
	text := fmt.Sprintf("%s\n\n%s", headline, requesterReceiptTitle(review))
	if statusLine != "" {
		text += "\n状态：" + statusLine
	}
	if footer != "" {
		text += "\n\n" + footer
	}
	return text
}

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
	input := card.Input()
	var keyboard *types.TelegramInlineKeyboard
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
