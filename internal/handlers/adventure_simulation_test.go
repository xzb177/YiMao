package handlers

import (
	"math/rand"
	"testing"
)

const (
	simulationRuns    = 20000
	simulationLevels  = 5
	simulationChoices = 4
)

type visibleAdventureObservation struct {
	recommended int
	remaining   []int
}

type simulationStrategy struct {
	name        string
	signalSkill float64
	useHint     bool
}

// visibleObservation models what a player can see: a fallible recommendation
// plus choices not yet eliminated by earlier feedback. The policy never receives
// the hidden correct choice.
func visibleObservation(rng *rand.Rand, correct int, skill float64, remaining []int) visibleAdventureObservation {
	recommended := correct
	if rng.Float64() >= skill {
		wrong := make([]int, 0, len(remaining)-1)
		for _, choice := range remaining {
			if choice != correct {
				wrong = append(wrong, choice)
			}
		}
		recommended = wrong[rng.Intn(len(wrong))]
	}
	return visibleAdventureObservation{recommended: recommended, remaining: append([]int(nil), remaining...)}
}

func chooseFromVisible(rng *rand.Rand, observation visibleAdventureObservation, firstAttempt bool) int {
	if firstAttempt {
		return observation.recommended
	}
	return observation.remaining[rng.Intn(len(observation.remaining))]
}

func removeVisibleChoice(choices []int, tried int) []int {
	for i, choice := range choices {
		if choice == tried {
			return append(choices[:i:i], choices[i+1:]...)
		}
	}
	return choices
}

func simulateAdventure(rng *rand.Rand, strategy simulationStrategy) bool {
	hp := 100
	revivedToday := false
	const today = "2026-07-12"

	for level := 1; level <= simulationLevels; level++ {
		if strategy.useHint {
			var ok bool
			hp, ok = ApplyAdventureHint(hp, level)
			if !ok {
				// Calling the cost helper as well makes the refusal condition explicit.
				if hp > AdventureHintCost(level) {
					panic("ApplyAdventureHint refused affordable hint")
				}
			}
		}

		correct := rng.Intn(simulationChoices) // environment-only hidden answer
		remaining := []int{0, 1, 2, 3}
		observation := visibleObservation(rng, correct, strategy.signalSkill, remaining)
		firstAttempt := true
		for {
			choice := chooseFromVisible(rng, observation, firstAttempt)
			firstAttempt = false
			if choice == correct { // environment judges; policy does not inspect this
				break
			}

			remaining = removeVisibleChoice(remaining, choice)
			observation.remaining = remaining // wrong-answer feedback is visible
			trap := choice == 0               // visible option property, independent of correctness
			hp -= AdventureDamage(level, simulationLevels, trap)
			if hp > 0 {
				continue
			}

			state := &AdventureState{Level: level, HP: 0}
			if revivedToday {
				state.LastFreeReviveDate = today
			}
			if !canFreeRevive(state, today) {
				return false
			}
			hp = adventureReviveHP
			revivedToday = true
		}
	}
	return true
}

func TestAdventureStrategyMonteCarlo(t *testing.T) {
	strategies := []simulationStrategy{
		{name: "blind", signalSkill: 0.34},
		{name: "clue", signalSkill: 0.53, useHint: true},
		{name: "fan", signalSkill: 0.82},
		{name: "oracle", signalSkill: 1},
	}
	bounds := [][2]float64{{0.03, 0.08}, {0.10, 0.20}, {0.50, 0.70}, {1, 1}}
	rates := make([]float64, len(strategies))

	for i, strategy := range strategies {
		// Separate fixed streams make failures exactly reproducible.
		rng := rand.New(rand.NewSource(2026071200 + int64(i)))
		wins := 0
		for run := 0; run < simulationRuns; run++ {
			if simulateAdventure(rng, strategy) {
				wins++
			}
		}
		rates[i] = float64(wins) / simulationRuns
		t.Logf("strategy=%s seed=%d runs=%d wins=%d success=%.3f%%", strategy.name, 2026071200+int64(i), simulationRuns, wins, rates[i]*100)
		if rates[i] < bounds[i][0] || rates[i] > bounds[i][1] {
			t.Errorf("%s success %.3f%% outside [%.1f%%, %.1f%%]", strategy.name, rates[i]*100, bounds[i][0]*100, bounds[i][1]*100)
		}
	}
	for i := 1; i < len(rates); i++ {
		if rates[i] <= rates[i-1] {
			t.Errorf("success rates not strictly ordered: %s %.3f%% <= %s %.3f%%", strategies[i].name, rates[i]*100, strategies[i-1].name, rates[i-1]*100)
		}
	}
}
