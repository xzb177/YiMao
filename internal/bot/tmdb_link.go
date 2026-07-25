package bot

import (
	"regexp"
	"strconv"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/pkg/types"
)

// TMDB links are intentionally strict: HTTPS, the canonical host, movie/tv,
// and a positive numeric id. Boundary checks prevent matching lookalike hosts
// or paths embedded in a larger token while still allowing surrounding prose.
var strictTMDBLink = regexp.MustCompile(`(?:^|[\s<({，。！？：；])https://(?:www\.)?themoviedb\.org/(movie|tv)/([1-9][0-9]*)(?:-[^\s/?#<]*)?(?:[/?#][^\s<]*)?(?:$|[\s>)}，。！？：；])`)

func ParseStrictTMDBLink(text string) (mediaType string, id int, ok bool) {
	m := strictTMDBLink.FindStringSubmatch(text)
	if len(m) != 3 {
		return "", 0, false
	}
	id, err := strconv.Atoi(m[2])
	return m[1], id, err == nil && id > 0
}

// HandlePrivateTMDBLink routes a pasted link through the existing detail
// callback, preserving one detail implementation for polling and webhook.
func HandlePrivateTMDBLink(msg *types.TelegramMessage, registry *callback.Registry, telegram *services.TelegramClient) bool {
	if msg == nil || msg.From == nil || msg.Chat == nil || msg.Chat.Type != "private" {
		return false
	}
	mediaType, id, ok := ParseStrictTMDBLink(msg.Text)
	if !ok || registry == nil {
		return false
	}
	parsed, err := registry.Parser().Parse(callback.BuildDetailCallback(strconv.Itoa(id), mediaType))
	if err != nil {
		return false
	}
	handler, exists := registry.Get(parsed.Action)
	if !exists {
		return false
	}
	ctx := &callback.Context{UserID: msg.From.ID, ChatID: msg.Chat.ID, ChatType: msg.Chat.Type, MessageID: msg.MessageID, Callback: parsed}
	resp, err := handler.Handle(ctx)
	if err != nil || resp == nil {
		return false
	}
	RenderCallbackResponse("[TMDBLink]", ctx, resp, telegram)
	return true
}
