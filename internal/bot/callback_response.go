package bot

import (
	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/pkg/logger"
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

	if resp.RichMessage != "" {
		logger.Info("%s Sending Rich Message to chat %d, len=%d", logPrefix, ctx.ChatID, len(resp.RichMessage))
		deletedOriginal := false
		if resp.Edit || resp.DeleteMessage {
			if delErr := telegram.DeleteMessage(ctx.ChatID, ctx.MessageID); delErr != nil {
				logger.Info("%s DeleteMessage before Rich Message error: %v", logPrefix, delErr)
			} else {
				deletedOriginal = true
			}
		}
		if _, sendErr := telegram.SendRichMessage(ctx.ChatID, resp.RichMessage, keyboard); sendErr != nil {
			logger.Info("%s Rich Message failed: %v, falling back to plain text", logPrefix, sendErr)
			if resp.Text != "" {
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

func defaultParseMode(parseMode string) string {
	if parseMode == "" {
		return "HTML"
	}
	return parseMode
}
