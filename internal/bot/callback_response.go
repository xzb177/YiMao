package bot

import (
	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
)

// RenderCallbackResponse renders callback responses for both Poll and Webhook modes.
// Keep this as the single place for Edit/Delete/RichMessage/Photo fallback semantics.
func RenderCallbackResponse(source string, ctx *callback.Context, resp *callback.Response, telegram *services.TelegramClient) {
	if ctx == nil || resp == nil || telegram == nil {
		return
	}

	keyboard := ConvertKeyboard(resp.Keyboard)
	logPrefix := "[Callback]"
	if source != "" {
		logPrefix = source
	}

	// P1/P3: a callback made on a Community timeline belongs to the clicking
	// user. Never edit, delete, or append personal details to the public message.
	// callback_query_id is included so ordinary (non-admin) bots can deliver
	// within Telegram's 15-second eligibility window.
	if isCommunityChat(ctx.ChatType) {
		renderCommunityCallbackResponse(logPrefix, ctx, resp, telegram, keyboard)
		return
	}

	if resp.StructuredRichMessage != nil || resp.RichMessage != "" {
		logger.Info("%s Sending Rich Message to chat %d", logPrefix, ctx.ChatID)
		deletedOriginal := false
		if resp.Edit || resp.DeleteMessage {
			if delErr := telegram.DeleteMessage(ctx.ChatID, ctx.MessageID); delErr != nil {
				logger.Info("%s DeleteMessage before Rich Message error: %v", logPrefix, delErr)
			} else {
				deletedOriginal = true
			}
		}
		// Photo + Rich Message is one native rich card. Never send a standalone
		// photo first; that duplicates metadata and leaves buttons on a second card.
		var sendErr error
		if resp.StructuredRichMessage != nil {
			_, sendErr = telegram.SendStructuredRichMessage(ctx.ChatID, resp.StructuredRichMessage, keyboard)
		} else if resp.Photo != "" {
			_, sendErr = telegram.SendRichMessageWithPhoto(ctx.ChatID, resp.RichMessage, resp.Photo, keyboard)
		} else {
			_, sendErr = telegram.SendRichMessage(ctx.ChatID, resp.RichMessage, keyboard)
		}
		if sendErr != nil {
			logger.Info("%s Rich Message failed: %v, falling back to one media/text card", logPrefix, sendErr)
			if resp.Photo != "" {
				caption := resp.PhotoCaption
				if caption == "" {
					caption = resp.Text
				}
				if _, photoErr := telegram.SendPhotoWithParseMode(ctx.ChatID, resp.Photo, caption, defaultParseMode(resp.ParseMode), keyboard); photoErr != nil {
					logger.Info("%s Rich media fallback failed: %v", logPrefix, photoErr)
				}
			} else if resp.Text != "" {
				if resp.Edit && !deletedOriginal {
					if _, editErr := telegram.EditMessage(ctx.ChatID, ctx.MessageID, resp.Text, defaultParseMode(resp.ParseMode), keyboard); editErr != nil {
						logger.Info("%s Rich fallback EditMessage error: %v", logPrefix, editErr)
					}
				} else if _, msgErr := telegram.SendMessage(ctx.ChatID, resp.Text, defaultParseMode(resp.ParseMode), keyboard); msgErr != nil {
					logger.Info("%s Rich fallback SendMessage error: %v", logPrefix, msgErr)
				}
			}
		}
		return
	}

	parseMode := defaultParseMode(resp.ParseMode)
	logger.Info("%s Using parse_mode=%s for chat %d, text preview=%.50s", logPrefix, parseMode, ctx.ChatID, resp.Text)

	if resp.Photo != "" {
		if delErr := telegram.DeleteMessage(ctx.ChatID, ctx.MessageID); delErr != nil {
			logger.Info("%s DeleteMessage before photo error: %v", logPrefix, delErr)
		}
		caption := resp.PhotoCaption
		if caption == "" {
			caption = resp.Text
		}
		logger.Info("%s Sending photo: %s with parse_mode=%s", logPrefix, resp.Photo, parseMode)
		if _, sendErr := telegram.SendPhotoWithParseMode(ctx.ChatID, resp.Photo, caption, parseMode, keyboard); sendErr != nil {
			logger.Info("%s SendPhoto URL method error: %v, trying proxy upload", logPrefix, sendErr)
			if _, fallbackErr := telegram.SendPhotoWithAuthAndParseMode(ctx.ChatID, resp.Photo, caption, parseMode, nil, keyboard); fallbackErr != nil {
				logger.Info("%s SendPhoto proxy upload also failed: %v", logPrefix, fallbackErr)
			}
		}
		return
	}

	if resp.Text != "" {
		if resp.DeleteMessage {
			if delErr := telegram.DeleteMessage(ctx.ChatID, ctx.MessageID); delErr != nil {
				logger.Info("%s DeleteMessage error: %v", logPrefix, delErr)
			}
			if _, sendErr := telegram.SendMessage(ctx.ChatID, resp.Text, parseMode, keyboard); sendErr != nil {
				logger.Info("%s SendMessage error: %v", logPrefix, sendErr)
			}
		} else if resp.Edit {
			if _, editErr := telegram.EditMessage(ctx.ChatID, ctx.MessageID, resp.Text, parseMode, keyboard); editErr != nil {
				logger.Info("%s EditMessage error: %v", logPrefix, editErr)
			}
		} else {
			if _, sendErr := telegram.SendMessage(ctx.ChatID, resp.Text, parseMode, keyboard); sendErr != nil {
				logger.Info("%s SendMessage error: %v", logPrefix, sendErr)
			}
		}
		return
	}

	if resp.Keyboard != nil {
		if _, editErr := telegram.EditMessageReplyMarkup(ctx.ChatID, ctx.MessageID, keyboard); editErr != nil {
			logger.Info("%s EditMessageReplyMarkup error: %v", logPrefix, editErr)
		}
	}
}

func isCommunityChat(chatType string) bool {
	return chatType == "group" || chatType == "supergroup"
}

// callbackNeedsImmediateAck reports whether a callback must be acknowledged
// before its business handler runs. Review approval performs authoritative
// preflight work (Emby availability, MoviePilot duplicate lookup, subscription
// creation) that can take longer than Telegram's callback window, so the ack is
// sent first and the outcome is delivered by the rendered message instead of a
// toast. The preflight itself keeps running in the handler goroutine and is
// idempotent, so a repeated click can never double-create a subscription.
func callbackNeedsImmediateAck(action callback.Action) bool {
	switch action {
	case "review_approve", "rv_a", "review_complete_wash", "review_retry_wash":
		return true
	default:
		return false
	}
}

func renderCommunityCallbackResponse(logPrefix string, ctx *callback.Context, resp *callback.Response, telegram *services.TelegramClient, keyboard *types.TelegramInlineKeyboard) {
	parseMode := defaultParseMode(resp.ParseMode)
	plain := resp.Text
	if plain == "" {
		plain = resp.RichMessage
		if plain != "" {
			parseMode = "Markdown"
		}
	}

	// Bot API 10.2 supports native ephemeral photos. Upgrade the existing text
	// placeholder in place so the user sees one card rather than a text message
	// followed by a second image. editEphemeralMessageMedia accepts URL/file_id.
	if resp.Photo != "" {
		caption := resp.PhotoCaption
		if caption == "" {
			caption = plain
		}
		if len([]rune(caption)) > 1024 {
			caption = string([]rune(caption)[:1021]) + "..."
		}
		media := map[string]interface{}{
			"type":       "photo",
			"media":      resp.Photo,
			"caption":    caption,
			"parse_mode": parseMode,
		}
		if ctx.EphemeralMessageID != 0 {
			if err := telegram.EditEphemeralMessageMedia(ctx.ChatID, ctx.UserID, ctx.EphemeralMessageID, media, keyboard); err == nil {
				return
			} else {
				logger.Info("%s Ephemeral media edit failed; trying private photo send: %v", logPrefix, err)
			}
		}
		options := &types.TelegramSendOptions{ReceiverUserID: ctx.UserID, CallbackQueryID: ctx.CallbackID, MessageThreadID: ctx.MessageThreadID}
		if _, err := telegram.SendPhotoWithParseMode(ctx.ChatID, resp.Photo, caption, parseMode, keyboard, options); err == nil {
			if ctx.EphemeralMessageID != 0 {
				_ = telegram.DeleteEphemeralMessage(ctx.ChatID, ctx.UserID, ctx.EphemeralMessageID)
			}
			return
		} else {
			logger.Info("%s Ephemeral photo send failed; using private text fallback: %v", logPrefix, err)
		}
	}

	if plain == "" {
		if ctx.EphemeralMessageID != 0 && resp.Keyboard != nil {
			if err := telegram.EditEphemeralMessageReplyMarkup(ctx.ChatID, ctx.UserID, ctx.EphemeralMessageID, keyboard); err != nil {
				logger.Info("%s Ephemeral markup update failed: %v", logPrefix, err)
			}
		}
		return
	}
	if ctx.EphemeralMessageID != 0 {
		if err := telegram.EditEphemeralMessageText(ctx.ChatID, ctx.UserID, ctx.EphemeralMessageID, plain, parseMode, keyboard); err != nil {
			logger.Info("%s Ephemeral edit failed; no public fallback: %v", logPrefix, err)
		}
		return
	}
	options := &types.TelegramSendOptions{
		ReceiverUserID:  ctx.UserID,
		CallbackQueryID: ctx.CallbackID,
		MessageThreadID: ctx.MessageThreadID,
	}
	if _, err := telegram.SendMessage(ctx.ChatID, plain, parseMode, keyboard, options); err != nil {
		logger.Info("%s Ephemeral send failed; no public fallback: %v", logPrefix, err)
	}
}

func defaultParseMode(parseMode string) string {
	if parseMode == "" {
		return "HTML"
	}
	return parseMode
}
