package handlers

import "testing"

func TestCanFreeRevive_DeadEarlyRunIsReachable(t *testing.T) {
	state := &AdventureState{Level: 3, HP: 0, InProgress: true}
	if !canFreeRevive(state, "2026-07-12") {
		t.Fatal("dead run in levels 1-3 must expose free revive")
	}
	state.LastFreeReviveDate = "2026-07-12"
	if canFreeRevive(state, "2026-07-12") {
		t.Fatal("free revive must only be available once per day")
	}
}

func TestCanShareAdventure_OnlyMatchingSuccessfulRun(t *testing.T) {
	state := &AdventureState{RunID: "run-1", Success: false, InProgress: false, HP: 0}
	if canShareAdventure(state, "run-1") {
		t.Fatal("failed adventure must not be shareable")
	}
	state.Success = true
	state.HP = 20
	if !canShareAdventure(state, "run-1") {
		t.Fatal("successful matching run must be shareable")
	}
	if canShareAdventure(state, "run-old") || canShareAdventure(state, "") {
		t.Fatal("share callback must identify the exact run/request")
	}
}

func TestAdventureBroadcastThresholds(t *testing.T) {
	tests := []struct {
		name               string
		success            bool
		grade              string
		perfect            bool
		combo              int
		failedLevel, total int
		want               bool
	}{
		{"SSS", true, "SSS", false, 0, 0, 5, true},
		{"SS", true, "SS", false, 0, 0, 5, true},
		{"perfect", true, "A", true, 0, 0, 5, true},
		{"x4", true, "A", false, 4, 0, 5, true},
		{"ordinary success", true, "A", false, 3, 0, 5, false},
		{"last level near miss", false, "D", false, 0, 5, 5, true},
		{"fourth level is not last", false, "D", false, 0, 4, 5, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldBroadcastAdventure(tt.success, tt.grade, tt.perfect, tt.combo, tt.failedLevel, tt.total); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestClaimAdventureShare_IdempotentByRunAndRequestID(t *testing.T) {
	h := &AdventureHandler{sharedRuns: make(map[string]struct{})}
	if !h.claimAdventureShare("run-1", "request-1") {
		t.Fatal("first share should be claimed")
	}
	if h.claimAdventureShare("run-1", "request-2") {
		t.Fatal("same run must not be shared twice")
	}
	if h.claimAdventureShare("run-2", "request-1") {
		t.Fatal("same request must not be processed twice")
	}
}
