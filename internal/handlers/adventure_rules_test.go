package handlers

import "testing"

func TestAdventureDamageIgnoresAI(t *testing.T) {
	cases := []struct {
		level, total int
		trap         bool
		want         int
	}{{1, 5, false, 46}, {1, 5, true, 58}, {5, 5, false, 65}, {5, 5, true, 78}}
	for _, tc := range cases {
		if got := AdventureDamage(tc.level, tc.total, tc.trap); got != tc.want {
			t.Fatalf("damage=%d want %d", got, tc.want)
		}
	}
}

func TestAdventureHintCannotKill(t *testing.T) {
	if AdventureHintCost(3) != 14 {
		t.Fatal("wrong cost")
	}
	if hp, ok := ApplyAdventureHint(14, 3); ok || hp != 14 {
		t.Fatalf("hint killed or charged: %d %v", hp, ok)
	}
	if hp, ok := ApplyAdventureHint(15, 3); !ok || hp != 1 {
		t.Fatalf("hint=%d %v", hp, ok)
	}
}

func TestAdventureScoreGradeDeterministic(t *testing.T) {
	a := AdventureScore(true, true, 100, 5, 0, 0, 0, 5)
	b := AdventureScore(true, true, 100, 5, 0, 0, 0, 5)
	if a != b || AdventureGrade(a) != "SSS" {
		t.Fatalf("score=%d/%d grade=%s", a, b, AdventureGrade(a))
	}
}
