package services

import (
	"strings"
	"testing"

	"github.com/xzb177/yimao/pkg/types"
)

func TestNativeMenusDoNotExposeLegacyChallenge(t *testing.T) {
	for _, button := range flattenKeyboard(BuildStartKeyboardWithOptions(false, true)) {
		assertNoLegacySurface(t, button.Text, button.CallbackData)
	}
	for _, button := range flattenKeyboard(BuildGameCenterKeyboard()) {
		assertNoLegacySurface(t, button.Text, button.CallbackData)
	}
}

func flattenKeyboard(keyboard *types.TelegramInlineKeyboard) []types.TelegramInlineKeyboardButton {
	var buttons []types.TelegramInlineKeyboardButton
	if keyboard == nil {
		return buttons
	}
	for _, row := range keyboard.InlineKeyboard {
		buttons = append(buttons, row...)
	}
	return buttons
}

func assertNoLegacySurface(t *testing.T, text, callback string) {
	t.Helper()
	legacyText := []string{"adven" + "ture", "Adven" + "ture", "电影" + "冒险", "趣味" + "闯关", "冒险" + "记录"}
	for _, value := range legacyText {
		if strings.Contains(text, value) || strings.Contains(callback, value) {
			t.Errorf("legacy challenge surface %q found in text=%q callback=%q", value, text, callback)
		}
	}
}
