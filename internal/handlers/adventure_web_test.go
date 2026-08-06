package handlers

import (
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
)

func TestAdventureWebViewIncludesTurnButHidesCorrectChoice(t *testing.T) {
	state := &AdventureState{
		RunID: "run-1", Turn: 3, Level: 2, HP: 70, MaxHP: 100,
		InProgress: true, Phase: AdventurePhasePlaying,
		MovieInfo: &services.MovieInfo{Title: "测试电影", Backdrops: []string{"/scene.jpg"}},
		Scene:     &services.AdventureScene{Choices: []services.AdventureChoice{{Text: "左边", Correct: true}, {Text: "右边"}}},
	}
	view := adventureWebView(state)
	if view.Turn != 3 || view.RunID != "run-1" || len(view.Choices) != 2 || len(view.Backdrops) != 1 || view.Backdrops[0] != "/scene.jpg" {
		t.Fatalf("missing public state: %+v", view)
	}
	state.MovieInfo.Backdrops[0] = "/mutated.jpg"
	if view.Backdrops[0] != "/scene.jpg" {
		t.Fatalf("web view must own backdrop data: %+v", view.Backdrops)
	}
}

func TestWebCurrentWaitsForAdventureStateLock(t *testing.T) {
	manager := session.NewManager(time.Hour, 10)
	state := &AdventureState{RunID: "run-1", Turn: 1, InProgress: true, Phase: AdventurePhasePlaying}
	manager.GetOrCreate(7).Set("adventure_state", state)
	h := &AdventureHandler{sessionMgr: manager}

	state.ChoiceLock.Lock()
	done := make(chan *AdventureWebView, 1)
	go func() { done <- h.WebCurrent(7) }()
	select {
	case <-done:
		state.ChoiceLock.Unlock()
		t.Fatal("WebCurrent read state without acquiring ChoiceLock")
	case <-time.After(20 * time.Millisecond):
	}
	state.ChoiceLock.Unlock()

	select {
	case view := <-done:
		if view == nil || view.RunID != "run-1" {
			t.Fatalf("unexpected view: %+v", view)
		}
	case <-time.After(time.Second):
		t.Fatal("WebCurrent did not finish after ChoiceLock was released")
	}
}

func TestWebQuitWaitsForStateLockAndExpiresRun(t *testing.T) {
	manager := session.NewManager(time.Hour, 10)
	state := &AdventureState{RunID: "run-1", Turn: 1, InProgress: true, Phase: AdventurePhasePlaying}
	sess := manager.GetOrCreate(7)
	sess.Set("adventure_state", state)
	h := &AdventureHandler{sessionMgr: manager}

	state.ChoiceLock.Lock()
	done := make(chan struct{})
	go func() {
		h.WebQuit(7)
		close(done)
	}()
	select {
	case <-done:
		state.ChoiceLock.Unlock()
		t.Fatal("WebQuit deleted state without acquiring ChoiceLock")
	case <-time.After(20 * time.Millisecond):
	}
	state.ChoiceLock.Unlock()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("WebQuit did not finish after ChoiceLock was released")
	}
	if state.InProgress || state.Phase == AdventurePhasePlaying {
		t.Fatalf("quit state remains active: %+v", state)
	}
	if _, ok := sess.Get("adventure_state"); ok {
		t.Fatal("quit state remains in session")
	}
	if validateAdventureCallback(state, map[string]string{"run": "run-1", "turn": "1"}, AdventurePhasePlaying) {
		t.Fatal("callback must expire after quit")
	}
}
