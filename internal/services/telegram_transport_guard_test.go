package services

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/xzb177/yimao/pkg/types"
)

func TestSanitizeInlineKeyboardDropsOversizedCallbacks(t *testing.T) {
	keyboard := &types.TelegramInlineKeyboard{InlineKeyboard: [][]types.TelegramInlineKeyboardButton{
		{
			{Text: "有效", CallbackData: "feedback:quick_idx:1"},
			{Text: "过长", CallbackData: strings.Repeat("片", 22)},
			{Text: "链接", URL: "https://example.com"},
			{Text: "无动作"},
		},
	}}

	got := sanitizeInlineKeyboard(keyboard)
	if got == nil || len(got.InlineKeyboard) != 1 {
		t.Fatalf("unexpected keyboard rows: %#v", got)
	}
	row := got.InlineKeyboard[0]
	if len(row) != 2 {
		t.Fatalf("button count = %d, want 2", len(row))
	}
	if row[0].CallbackData != "feedback:quick_idx:1" || row[1].URL != "https://example.com" {
		t.Fatalf("unexpected sanitized buttons: %#v", row)
	}
}

func TestNormalizeTelegramPayloadCapsTextAndKeyboard(t *testing.T) {
	payload := map[string]interface{}{
		"text":       "<b>" + strings.Repeat("界", 4100) + "</b>",
		"parse_mode": "HTML",
		"reply_markup": &types.TelegramInlineKeyboard{InlineKeyboard: [][]types.TelegramInlineKeyboardButton{{
			{Text: "保留", CallbackData: "start"},
			{Text: "移除", CallbackData: strings.Repeat("x", telegramCallbackDataMaxBytes+1)},
		}}},
	}

	normalizeTelegramPayload(payload)
	text, ok := payload["text"].(string)
	if !ok {
		t.Fatalf("text type = %T", payload["text"])
	}
	if utf8.RuneCountInString(text) > 4096 {
		t.Fatalf("text length = %d runes", utf8.RuneCountInString(text))
	}
	if strings.Contains(text, "<b>") || payload["parse_mode"] != "" {
		t.Fatalf("truncated HTML was not downgraded: mode=%q text=%q", payload["parse_mode"], text[:8])
	}
	keyboard := payload["reply_markup"].(*types.TelegramInlineKeyboard)
	if len(keyboard.InlineKeyboard) != 1 || len(keyboard.InlineKeyboard[0]) != 1 {
		t.Fatalf("unexpected normalized keyboard: %#v", keyboard)
	}
}

func TestNormalizeTelegramPayloadCapsMediaCaption(t *testing.T) {
	media := map[string]interface{}{
		"caption":    "<b>" + strings.Repeat("片", 1100) + "</b>",
		"parse_mode": "HTML",
	}
	payload := map[string]interface{}{"media": media}

	normalizeTelegramPayload(payload)
	caption := media["caption"].(string)
	if utf8.RuneCountInString(caption) > 1024 || media["parse_mode"] != "" {
		t.Fatalf("media caption was not safely capped: mode=%q runes=%d", media["parse_mode"], utf8.RuneCountInString(caption))
	}
}

func TestSafeURLForLogRemovesCredentialsAndQuery(t *testing.T) {
	got := safeURLForLog("https://user:pass@example.com/poster.jpg?api_key=secret#fragment")
	if got != "https://example.com/poster.jpg" {
		t.Fatalf("safeURLForLog = %q", got)
	}
}

func TestSanitizeUTF8RemovesNullAndInvalidBytes(t *testing.T) {
	got := sanitizeUTF8("正常\x00\xff文本")
	if !utf8.ValidString(got) || strings.ContainsRune(got, 0) {
		t.Fatalf("sanitizeUTF8 returned invalid text: %q", got)
	}
	if got != "正常文本" {
		t.Fatalf("sanitizeUTF8 = %q", got)
	}
}

func TestSanitizeInlineKeyboardAppliesSemanticStyles(t *testing.T) {
	keyboard := &types.TelegramInlineKeyboard{InlineKeyboard: [][]types.TelegramInlineKeyboardButton{{
		{Text: "🎬 求片", CallbackData: "request:id:1:type:movie"},
		{Text: "✅ 确认提交", CallbackData: "feedback:confirm:1"},
		{Text: "❌ 取消", CallbackData: "cancel"},
		{Text: "🏠 主菜单", CallbackData: "start"},
	}}}

	got := sanitizeInlineKeyboard(keyboard)
	row := got.InlineKeyboard[0]
	want := []string{telegramButtonStylePrimary, telegramButtonStyleSuccess, telegramButtonStyleDanger, ""}
	for i, button := range row {
		if button.Style != want[i] {
			t.Fatalf("button %q style = %q, want %q", button.Text, button.Style, want[i])
		}
	}
}

func TestSanitizeInlineKeyboardKeepsOnlyValidExplicitStyle(t *testing.T) {
	keyboard := &types.TelegramInlineKeyboard{InlineKeyboard: [][]types.TelegramInlineKeyboardButton{{
		{Text: "普通操作", CallbackData: "custom_action", Style: "purple"},
		{Text: "主操作", CallbackData: "custom_action", Style: " PRIMARY "},
	}}}

	got := sanitizeInlineKeyboard(keyboard)
	row := got.InlineKeyboard[0]
	if row[0].Style != "" {
		t.Fatalf("invalid explicit style = %q, want empty", row[0].Style)
	}
	if row[1].Style != telegramButtonStylePrimary {
		t.Fatalf("valid explicit style = %q, want %q", row[1].Style, telegramButtonStylePrimary)
	}
}
