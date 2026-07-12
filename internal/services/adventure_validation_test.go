package services

import "testing"

func validAdventureScene() *AdventureScene {
	return &AdventureScene{Choices: []AdventureChoice{{Text: "a", Correct: true}, {Text: "b"}, {Text: "c"}, {Text: "d", HPChange: -999}}}
}

func TestValidateAdventureScene(t *testing.T) {
	if err := ValidateAdventureScene(validAdventureScene()); err != nil {
		t.Fatal(err)
	}
	tests := []func(*AdventureScene){
		func(s *AdventureScene) { s.Choices = s.Choices[:3] },
		func(s *AdventureScene) { s.Choices[0].Correct = false },
		func(s *AdventureScene) { s.Choices[1].Correct = true },
		func(s *AdventureScene) { s.Choices[0].IsTrap = true },
		func(s *AdventureScene) { s.Choices[1].Text = "a" },
		func(s *AdventureScene) { s.Choices[1].Text = " " },
	}
	for i, mutate := range tests {
		s := validAdventureScene()
		mutate(s)
		if ValidateAdventureScene(s) == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}
