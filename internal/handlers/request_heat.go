package handlers

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
)

const (
	requestHeatWindow   = 7 * 24 * time.Hour
	requestHeatLimit    = 8
	requestHeatMinUsers = 1
)

// RequestHeatHandler displays anonymous aggregates of real request activity.
type RequestHeatHandler struct {
	heat *services.RequestHeatService
}

func NewRequestHeatHandler(heat *services.RequestHeatService) *RequestHeatHandler {
	return &RequestHeatHandler{heat: heat}
}

func (h *RequestHeatHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	if h == nil || h.heat == nil {
		return &callback.Response{CallbackMsg: "服务暂时未就绪", ShowAlert: true}, nil
	}

	items := h.heat.Recent(requestHeatWindow, requestHeatLimit)
	visible := items[:0]
	for _, item := range items {
		if item.Count >= requestHeatMinUsers {
			visible = append(visible, item)
		}
	}
	items = visible
	keyboard := &callback.Keyboard{}
	var text strings.Builder
	text.WriteString("🔥 <b>大家最近在求</b>\n\n")

	if len(items) == 0 {
		text.WriteString("最近 7 天还没有正在等待的求片。\n\n搜到想看的影片后，可以直接求片或加入想看。")
		keyboard.InlineKeyboard = [][]callback.Button{
			{{Text: "🔍 搜索求片", CallbackData: "start_search"}},
			{{Text: "🏠 主菜单", CallbackData: "start"}},
		}
		return &callback.Response{Text: text.String(), Edit: true, ParseMode: "HTML", Keyboard: keyboard}, nil
	}

	text.WriteString("最近 7 天真实提交过、目前仍在等待的内容：\n\n")
	for index, item := range items {
		typeLabel := "电影"
		if item.MediaType == "tv" {
			typeLabel = "剧集"
		}
		year := ""
		if item.Year > 0 {
			year = fmt.Sprintf(" (%d)", item.Year)
		}
		text.WriteString(fmt.Sprintf("%d. 《%s》%s · %s · %d 人想看\n", index+1, html.EscapeString(item.Title), year, typeLabel, item.Count))
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []callback.Button{{
			Text:         fmt.Sprintf("%d · %s", index+1, truncateRequestHeatTitle(item.Title, 22)),
			CallbackData: fmt.Sprintf("detail:id:%d:type:%s:source:request_heat", item.TMDBID, item.MediaType),
		}})
	}
	text.WriteString("\n点影片查看详情，也可以直接求片或加入想看。")
	keyboard.InlineKeyboard = append(keyboard.InlineKeyboard,
		[]callback.Button{{Text: "🔄 刷新", CallbackData: "request_heat"}},
		[]callback.Button{{Text: "🏠 主菜单", CallbackData: "start"}},
	)
	return &callback.Response{Text: text.String(), Edit: true, ParseMode: "HTML", Keyboard: keyboard}, nil
}

func truncateRequestHeatTitle(title string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(title))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes-1]) + "…"
}
