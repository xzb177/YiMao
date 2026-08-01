package handlers

import (
	"testing"

	"github.com/xzb177/yimao/internal/services"
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
