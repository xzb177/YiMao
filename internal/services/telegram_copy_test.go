package services

import "testing"

func TestStartKeyboardKeepsRequestFirstHierarchy(t *testing.T) {
	keyboard := BuildStartKeyboardWithOptions(false, true)
	if keyboard == nil || len(keyboard.InlineKeyboard) < 3 {
		t.Fatalf("unexpected start keyboard: %#v", keyboard)
	}

	first := keyboard.InlineKeyboard[0]
	if len(first) != 2 {
		t.Fatalf("first row buttons = %d, want 2", len(first))
	}
	if first[0].Text != "🔍 搜索求片" || first[0].CallbackData != "start_search" {
		t.Fatalf("first button = %#v", first[0])
	}
	if first[1].Text != "📊 求片进度" || first[1].CallbackData != "start_requests" {
		t.Fatalf("second button = %#v", first[1])
	}

	positions := map[string]int{}
	for rowIndex, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			positions[button.CallbackData] = rowIndex
		}
	}
	if positions["adventure_start"] <= positions["start_search"] {
		t.Fatalf("optional adventure row %d must follow request row %d", positions["adventure_start"], positions["start_search"])
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
