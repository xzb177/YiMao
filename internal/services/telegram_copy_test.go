package services

import "testing"

func TestStartKeyboardKeepsRequestFirstHierarchy(t *testing.T) {
	keyboard := BuildStartKeyboardWithOptions(false, true)
	if keyboard == nil || len(keyboard.InlineKeyboard) != 2 {
		t.Fatalf("unexpected start keyboard: %#v", keyboard)
	}
	first := keyboard.InlineKeyboard[0]
	if len(first) != 2 || first[0].Text != "搜索求片" || first[0].CallbackData != "start_search" || first[1].CallbackData != "requests" {
		t.Fatalf("primary row = %#v", first)
	}
	second := keyboard.InlineKeyboard[1]
	if len(second) != 2 || second[0].CallbackData != "help" || second[1].CallbackData != "start_more" {
		t.Fatalf("second row = %#v", second)
	}
	for _, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == "wash" || button.CallbackData == "admin_menu" || button.CallbackData == "game_menu" {
				t.Fatalf("secondary entry leaked onto home: %#v", button)
			}
		}
	}
}

func TestGameCenterUsesCanonicalCopyAndCallbacks(t *testing.T) {
	keyboard := BuildGameCenterKeyboard()
	want := map[string]string{
		"game_narrator": "电影情报站",
		"game_blindbox": "盲盒",
		"game_roulette": "命运轮盘",
		"portrait":      "观影画像",
		"start":         "主菜单",
	}
	for _, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			expected, ok := want[button.CallbackData]
			if !ok {
				t.Fatalf("unexpected game-center callback: %q", button.CallbackData)
			}
			if button.Text != expected {
				t.Errorf("%s text = %q, want %q", button.CallbackData, button.Text, expected)
			}
			delete(want, button.CallbackData)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing callbacks: %#v", want)
	}
}
