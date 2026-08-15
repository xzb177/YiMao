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
