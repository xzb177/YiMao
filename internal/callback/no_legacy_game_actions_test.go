package callback

import "testing"

func TestLegacyChallengeActionsAreRejected(t *testing.T) {
	legacy := []string{
		"adven" + "ture_start",
		"adven" + "ture_choice",
		"adven" + "ture_hint",
		"adven" + "ture_retry",
		"adven" + "ture_quit",
		"adven" + "ture_share",
		"adven" + "ture_revive",
		"adven" + "ture_gamble",
		"adven" + "ture_gamble_safe",
		"adven" + "ture_gamble_triple",
		"game_adven" + "ture_stats",
		"game_adven" + "ture_rank",
		"game_daily_" + "challenge",
		// 游戏中心与 AI 能力已整体下线
		"game_" + "menu",
		"game_" + "narrator",
		"game_" + "narrate",
		"game_" + "blindbox",
		"game_" + "blindbox_open",
		"game_" + "blindbox_horror",
		"game_" + "blindbox_personality",
		"game_" + "roulette",
		"game_" + "roulette_spin",
		"game_" + "social",
		"game_" + "review",
		"game_" + "review_rate",
		"game_" + "emotion",
		"game_" + "achievements",
		"game_" + "compare",
		"game_" + "contract",
		"game_" + "personality",
		"game_" + "rank",
		"game_" + "time_machine",
		"game_" + "prescription",
		"ai",
		"start_" + "ai",
	}
	parser := NewParser()
	for _, action := range legacy {
		if isValidAction(Action(action)) {
			t.Errorf("legacy callback action %q remains whitelisted", action)
		}
		if _, err := parser.Parse(action); err == nil {
			t.Errorf("legacy callback action %q remains parseable", action)
		}
	}
}
