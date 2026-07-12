package handlers

import (
	"testing"

	"github.com/xzb177/yimao/internal/services"
)

func TestValidateAdventureCallback(t *testing.T) {
	state := &AdventureState{RunID: "run-new", Turn: 4, Phase: AdventurePhasePlaying}
	tests := []struct {
		name   string
		params map[string]string
		phase  AdventurePhase
		want   bool
	}{
		{name: "current run turn and phase", params: map[string]string{"run": "run-new", "turn": "4"}, phase: AdventurePhasePlaying, want: true},
		{name: "old run", params: map[string]string{"run": "run-old", "turn": "4"}, phase: AdventurePhasePlaying},
		{name: "old turn", params: map[string]string{"run": "run-new", "turn": "3"}, phase: AdventurePhasePlaying},
		{name: "missing run", params: map[string]string{"turn": "4"}, phase: AdventurePhasePlaying},
		{name: "missing turn", params: map[string]string{"run": "run-new"}, phase: AdventurePhasePlaying},
		{name: "malformed turn", params: map[string]string{"run": "run-new", "turn": "four"}, phase: AdventurePhasePlaying},
		{name: "generating rejects playing callback", params: map[string]string{"run": "run-new", "turn": "4"}, phase: AdventurePhaseGenerating},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateAdventureCallback(state, tc.params, tc.phase); got != tc.want {
				t.Fatalf("validateAdventureCallback() = %v, want %v", got, tc.want)
			}
		})
	}

	state.Phase = AdventurePhaseGenerating
	if validateAdventureCallback(state, map[string]string{"run": "run-new", "turn": "4"}, AdventurePhasePlaying) {
		t.Fatal("callback must be rejected while state is generating")
	}
}

func TestClaimAdventureFinishOnlyOnce(t *testing.T) {
	state := &AdventureState{Phase: AdventurePhasePlaying, InProgress: true}
	if !claimAdventureFinish(state, true) {
		t.Fatal("first finish claim must succeed")
	}
	if !state.FinishClaimed || !state.Success || state.Phase != AdventurePhaseFinishing || state.InProgress {
		t.Fatalf("unexpected claimed state: %+v", state)
	}
	if claimAdventureFinish(state, false) {
		t.Fatal("second finish claim must fail")
	}
	if !state.Success {
		t.Fatal("losing duplicate claim must not overwrite first result")
	}
	if claimAdventureFinish(nil, true) {
		t.Fatal("nil state must not be claimable")
	}
}

func TestFinalizeAdventureResultOverridesAIFields(t *testing.T) {
	state := &AdventureState{
		HP: 73, TotalLevels: 5, PerfectRun: false, Mistakes: 2,
		HintsUsed: 1, ReviveCount: 1, MaxCombo: 3,
	}
	for _, tc := range []struct {
		name    string
		success bool
		ai      *services.AdventureResult
	}{
		{name: "AI forges win and maximum score", success: false, ai: &services.AdventureResult{Success: true, Score: 100, Grade: "SSS", FinalScene: "kept"}},
		{name: "AI forges loss and low score", success: true, ai: &services.AdventureResult{Success: false, Score: -999, Grade: "D", FinalScene: "kept"}},
		{name: "nil AI result", success: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := finalizeAdventureResult(state, tc.success, tc.ai)
			wantScore := AdventureScore(tc.success, state.PerfectRun, state.HP, state.TotalLevels, state.Mistakes, state.HintsUsed, state.ReviveCount, state.MaxCombo)
			if got.Success != tc.success || got.Score != wantScore || got.Grade != AdventureGrade(wantScore) {
				t.Fatalf("server result = {success:%v score:%d grade:%q}, want {%v %d %q}", got.Success, got.Score, got.Grade, tc.success, wantScore, AdventureGrade(wantScore))
			}
			if state.Score != wantScore {
				t.Fatalf("state score = %d, want %d", state.Score, wantScore)
			}
			if tc.ai != nil && got.FinalScene != "kept" {
				t.Fatal("non-authoritative narrative fields should be retained")
			}
		})
	}
}

func TestNormalizeAdventureStateLegacy(t *testing.T) {
	tests := []struct {
		name      string
		state     *AdventureState
		wantPhase AdventurePhase
		wantLive  bool
	}{
		{name: "active legacy run", state: &AdventureState{Level: 2, HP: 50, InProgress: true}, wantPhase: AdventurePhasePlaying, wantLive: true},
		{name: "dead legacy run", state: &AdventureState{Level: 3, HP: 0, InProgress: true}, wantPhase: AdventurePhaseRevive, wantLive: true},
		{name: "completed legacy run", state: &AdventureState{Level: 5, HP: 50}, wantPhase: AdventurePhaseFinished},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			normalizeAdventureState(tc.state)
			if tc.state.Phase != tc.wantPhase || tc.state.InProgress != tc.wantLive {
				t.Fatalf("normalized phase/live = %q/%v, want %q/%v", tc.state.Phase, tc.state.InProgress, tc.wantPhase, tc.wantLive)
			}
			if tc.state.Turn != tc.state.Level {
				t.Fatalf("legacy turn = %d, want level %d", tc.state.Turn, tc.state.Level)
			}
			if tc.state.TriedChoices == nil {
				t.Fatal("legacy tried choices map was not initialized")
			}
		})
	}

	current := &AdventureState{Level: 4, Turn: 9, HP: 80, Phase: AdventurePhaseGenerating}
	normalizeAdventureState(current)
	if current.Turn != 9 || current.Phase != AdventurePhaseGenerating || !current.InProgress {
		t.Fatalf("current state was incorrectly migrated: %+v", current)
	}
	normalizeAdventureState(nil)
}
