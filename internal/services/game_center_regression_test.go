package services

import "testing"

func TestGameCenterKeyboardOnlyExposesLiveFeatures(t *testing.T) {
	kb := BuildGameCenterKeyboard()
	if kb == nil {
		t.Fatal("game center keyboard is nil")
	}
	want := map[string]bool{
		"adventure_start":      false,
		"game_daily_challenge": false,
		"game_adventure_rank":  false,
		"game_adventure_stats": false,
		"game_narrator":        false,
		"start":                false,
	}
	blocked := map[string]bool{
		"game_emotion": true, "game_compare": true, "game_achievements": true,
		"game_contract": true, "game_personality": true, "game_blindbox": true,
	}
	for _, row := range kb.InlineKeyboard {
		for _, button := range row {
			if _, ok := want[button.CallbackData]; ok {
				want[button.CallbackData] = true
			}
			if blocked[button.CallbackData] {
				t.Fatalf("retired action %q is still exposed", button.CallbackData)
			}
		}
	}
	for action, found := range want {
		if !found {
			t.Errorf("live action %q missing from menu", action)
		}
	}
}

func TestAdventureLeaderboardKeepsSameNameUsersSeparate(t *testing.T) {
	db, err := NewSocialDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.SaveAdventureRecord(101, "同名玩家", "电影甲", 2026, 90, "S", 3, 50, 5, 5, false, true); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAdventureRecord(202, "同名玩家", "电影乙", 2026, 80, "A", 2, 40, 5, 5, false, true); err != nil {
		t.Fatal(err)
	}
	rows, err := db.GetAdventureLeaderboard(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("same-name users merged: got %d rows, want 2", len(rows))
	}
	if rows[0].UserID != 101 || rows[1].UserID != 202 {
		t.Fatalf("unexpected ranking identities: %+v", rows)
	}
}

func TestDailyChallengeRequiresRecommendedMovie(t *testing.T) {
	db, err := NewSocialDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.SaveAdventureRecord(303, "玩家", "别的电影", 2026, 99, "SSS", 3, 100, 5, 5, true, true); err != nil {
		t.Fatal(err)
	}
	date := ""
	if err := db.db.QueryRow("SELECT date(created_at) FROM adventure_stats WHERE user_id = ?", 303).Scan(&date); err != nil {
		t.Fatal(err)
	}
	completed, err := db.HasDailyChallenge(303, date, "今日推荐")
	if err != nil {
		t.Fatal(err)
	}
	if completed {
		t.Fatal("clearing another movie must not complete today's recommendation")
	}
	if err := db.SaveAdventureRecord(303, "玩家", "今日推荐", 2026, 70, "A", 2, 30, 5, 5, false, true); err != nil {
		t.Fatal(err)
	}
	completed, err = db.HasDailyChallenge(303, date, "今日推荐")
	if err != nil {
		t.Fatal(err)
	}
	if !completed {
		t.Fatal("recommended movie clear was not recognized")
	}
}
