package services

import (
	"testing"

	"github.com/xzb177/yimao/pkg/types"
)

func TestBuildStartKeyboardAddsMiniAppOnlyForHTTPSURL(t *testing.T) {
	t.Setenv("MINI_APP_URL", "https://example.com/miniapp")
	keyboard := BuildStartKeyboardWithOptions(false, false)
	var found bool
	for _, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			if button.WebApp != nil {
				found = button.WebApp.URL == "https://example.com/miniapp" && button.CallbackData == "" && button.URL == ""
			}
		}
	}
	if !found {
		t.Fatal("valid HTTPS Mini App button was not added")
	}
}

func TestBuildStartKeyboardRejectsNonHTTPSMiniAppURL(t *testing.T) {
	t.Setenv("MINI_APP_URL", "http://example.test/miniapp")
	keyboard := BuildStartKeyboardWithOptions(false, false)
	for _, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			if button.WebApp != nil {
				t.Fatal("non-HTTPS Mini App URL was exposed")
			}
		}
	}
}

func TestBuildStartKeyboardRejectsMalformedOrCredentialedMiniAppURL(t *testing.T) {
	for _, raw := range []string{
		"https://",
		"https://user:password@example.test/miniapp",
		"https://example.test/miniapp?token=secret",
		"https://example.test/miniapp#fragment",
		"not a URL",
	} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("MINI_APP_URL", raw)
			keyboard := BuildStartKeyboardWithOptions(false, false)
			for _, row := range keyboard.InlineKeyboard {
				for _, button := range row {
					if button.WebApp != nil {
						t.Fatalf("invalid Mini App URL %q was exposed", raw)
					}
				}
			}
		})
	}
}

func TestSanitizeInlineKeyboardPreservesWebAppButton(t *testing.T) {
	t.Setenv("MINI_APP_URL", "https://example.com/miniapp")
	keyboard := sanitizeInlineKeyboard(BuildStartKeyboardWithOptions(false, false))
	for _, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			if button.WebApp != nil {
				if button.WebApp.URL != "https://example.com/miniapp" {
					t.Fatalf("web app URL=%q", button.WebApp.URL)
				}
				return
			}
		}
	}
	t.Fatal("transport sanitizer dropped the Mini App web_app button")
}

func TestSanitizeInlineKeyboardRejectsInvalidWebAppURL(t *testing.T) {
	for _, raw := range []string{
		"",
		"http://example.test/miniapp",
		"https://",
		"https://user:password@example.test/miniapp",
		"https://example.test/miniapp?token=secret",
		"https://example.test/miniapp#fragment",
	} {
		t.Run(raw, func(t *testing.T) {
			keyboard := &types.TelegramInlineKeyboard{InlineKeyboard: [][]types.TelegramInlineKeyboardButton{{{
				Text:   "Mini App",
				WebApp: &types.TelegramWebAppInfo{URL: raw},
			}}}}
			got := sanitizeInlineKeyboard(keyboard)
			if got != nil && len(got.InlineKeyboard) != 0 {
				t.Fatalf("sanitizeInlineKeyboard() = %#v, want no buttons", got)
			}
		})
	}
}

func TestSanitizeInlineKeyboardRejectsMultipleActions(t *testing.T) {
	validWebApp := &types.TelegramWebAppInfo{URL: "https://example.test/miniapp"}
	buttons := []types.TelegramInlineKeyboardButton{
		{Text: "callback+url", CallbackData: "search", URL: "https://example.test"},
		{Text: "callback+webapp", CallbackData: "search", WebApp: validWebApp},
		{Text: "url+webapp", URL: "https://example.test", WebApp: validWebApp},
	}
	for _, button := range buttons {
		t.Run(button.Text, func(t *testing.T) {
			keyboard := &types.TelegramInlineKeyboard{InlineKeyboard: [][]types.TelegramInlineKeyboardButton{{button}}}
			got := sanitizeInlineKeyboard(keyboard)
			if got != nil && len(got.InlineKeyboard) != 0 {
				t.Fatalf("sanitizeInlineKeyboard() = %#v, want no buttons", got)
			}
		})
	}
}

func TestSanitizeInlineKeyboardPreservesAllowedDeepLinkURLs(t *testing.T) {
	for _, raw := range []string{
		"https://example.test/miniapp?tmdb_id=550&type=movie",
		"https://example.test/miniapp?season=2&tmdb_id=1399&type=tv",
	} {
		t.Run(raw, func(t *testing.T) {
			keyboard := &types.TelegramInlineKeyboard{InlineKeyboard: [][]types.TelegramInlineKeyboardButton{{{
				Text:   "打开影视详情",
				WebApp: &types.TelegramWebAppInfo{URL: raw},
			}}}}
			got := sanitizeInlineKeyboard(keyboard)
			if got == nil || len(got.InlineKeyboard) != 1 || len(got.InlineKeyboard[0]) != 1 {
				t.Fatalf("sanitizeInlineKeyboard() = %#v, want one deep-link button", got)
			}
			if got.InlineKeyboard[0][0].WebApp == nil || got.InlineKeyboard[0][0].WebApp.URL != raw {
				t.Fatalf("deep-link Web App URL was changed or dropped: %#v", got.InlineKeyboard[0][0].WebApp)
			}
		})
	}
}
