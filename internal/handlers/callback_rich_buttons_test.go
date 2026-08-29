package handlers

import (
	"testing"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/richmessage"
	"github.com/xzb177/yimao/pkg/types"
)

func TestHandleSearchRichResponseDoesNotUseForceReply(t *testing.T) {
	h := &StartHandler{}
	resp, err := h.HandleSearch(&callback.Context{UserID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil || resp.StructuredRichMessage == nil {
		t.Fatal("missing rich search card")
	}
	if resp.Keyboard != nil && (resp.Keyboard.ForceReply || resp.Keyboard.RemoveKeyboard) {
		t.Fatalf("rich search card has top-level reply keyboard: %+v", resp.Keyboard)
	}
	found := false
	for _, block := range resp.StructuredRichMessage.Blocks {
		if block.Type == "buttons" && len(block.Buttons) >= 3 {
			found = true
		}
	}
	if !found {
		t.Fatal("search rich card has no buttons block")
	}
}

func TestBuildRichDetailResponseMovesButtonsIntoRichCardWithAndWithoutPoster(t *testing.T) {
	h := &DetailHandler{}
	keyboard := &types.TelegramInlineKeyboard{InlineKeyboard: [][]types.TelegramInlineKeyboardButton{{
		{Text: "求片提交", CallbackData: "request:id:42:type:movie", Style: "primary"},
		{Text: "进入许愿", CallbackData: "wish_add:id:42", Style: "primary"},
	}}}
	for _, poster := range []string{"", "https://image.example/poster.jpg"} {
		resp := h.buildRichDetailResponse(richmessage.MediaInfo{Title: "测试电影", TMDBID: 42, MediaType: "movie", PosterURL: poster}, keyboard, poster, true)
		if resp == nil || resp.StructuredRichMessage == nil {
			t.Fatalf("poster=%q missing rich response", poster)
		}
		if resp.Keyboard != nil {
			t.Fatalf("poster=%q retained top-level keyboard: %+v", poster, resp.Keyboard)
		}
		found := false
		for _, block := range resp.StructuredRichMessage.Blocks {
			if block.Type == "buttons" && len(block.Buttons) == 2 && block.Buttons[0].CallbackData == "request:id:42:type:movie" {
				found = true
			}
		}
		if !found {
			t.Fatalf("poster=%q rich buttons missing: %+v", poster, resp.StructuredRichMessage.Blocks)
		}
		if poster != "" && resp.Photo != poster {
			t.Fatalf("poster dropped: got %q", resp.Photo)
		}
	}
}
