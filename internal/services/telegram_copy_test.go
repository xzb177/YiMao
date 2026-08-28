package services

import "testing"

func TestStartKeyboardKeepsRequestFirstHierarchy(t *testing.T) {
	keyboard := BuildStartKeyboardWithOptions(false, true)
	if keyboard == nil || len(keyboard.InlineKeyboard) != 2 {
		t.Fatalf("unexpected start keyboard: %#v", keyboard)
	}
	// 方案1: two rows of three, request-first, 搜索求片 the only green control.
	first := keyboard.InlineKeyboard[0]
	if len(first) != 3 || first[0].Text != "搜索求片" || first[0].CallbackData != "start_search" ||
		first[1].CallbackData != "requests" || first[2].CallbackData != "wash" {
		t.Fatalf("primary row = %#v", first)
	}
	second := keyboard.InlineKeyboard[1]
	if len(second) != 3 || second[0].CallbackData != "start_wish" ||
		second[1].CallbackData != "help" || second[2].CallbackData != "start_more" {
		t.Fatalf("second row = %#v", second)
	}
	success := 0
	for _, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			if len([]rune(button.Text)) != 4 {
				t.Fatalf("label %q is not 4 CJK characters", button.Text)
			}
			if button.Style == "success" {
				success++
				if button.Text != "搜索求片" {
					t.Fatalf("unexpected success button: %#v", button)
				}
			}
			// Administration and the game drawer stay behind 更多功能.
			if button.CallbackData == "admin_menu" || button.CallbackData == "game_menu" || button.CallbackData == "start_settings" {
				t.Fatalf("secondary entry leaked onto home: %#v", button)
			}
		}
	}
	if success != 1 {
		t.Fatalf("start keyboard must have exactly one success button, got %d", success)
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
