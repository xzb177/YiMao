package services

import "testing"

func TestStartKeyboardKeepsRequestFirstHierarchy(t *testing.T) {
	keyboard := BuildStartKeyboardWithOptions(false, true)
	if keyboard == nil || len(keyboard.InlineKeyboard) != 5 {
		t.Fatalf("unexpected start keyboard: %#v", keyboard)
	}

	first := keyboard.InlineKeyboard[0]
	if len(first) != 2 || first[0].Text != "🎬 求片" || first[0].CallbackData != "start_search" || first[1].CallbackData != "wash" {
		t.Fatalf("primary row = %#v", first)
	}

	wantRows := [][]string{
		{"start_search", "wash"},
		{"issue", "start_requests"},
		{"start_wish", "request_heat"},
		{"game_menu"},
		{"start_settings", "help"},
	}
	for i, want := range wantRows {
		row := keyboard.InlineKeyboard[i]
		if len(row) != len(want) {
			t.Fatalf("row %d = %#v, want callbacks %v", i, row, want)
		}
		for j, callback := range want {
			if row[j].CallbackData != callback {
				t.Fatalf("row %d button %d callback=%q want=%q", i, j, row[j].CallbackData, callback)
			}
		}
	}

	for _, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == "adventure_start" || button.CallbackData == "start_portrait" || button.CallbackData == "start_ai" {
				t.Fatalf("duplicate/low-frequency entry still exposed on home: %#v", button)
			}
		}
	}
}

func TestGameCenterUsesCanonicalCopyWithoutChangingCallbacks(t *testing.T) {
	keyboard := BuildGameCenterKeyboard()
	want := map[string]string{
		"adventure_start":      "⚔️ 电影冒险",
		"game_daily_challenge": "🎯 今日挑战",
		"game_adventure_rank":  "📊 冒险排行",
		"game_adventure_stats": "📈 我的战绩",
		"game_narrator":        "📖 电影情报站",
		"start":                "🏠 主菜单",
	}
	for _, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			if expected, ok := want[button.CallbackData]; ok {
				if button.Text != expected {
					t.Errorf("%s text = %q, want %q", button.CallbackData, button.Text, expected)
				}
				delete(want, button.CallbackData)
			}
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing callbacks: %#v", want)
	}
}
