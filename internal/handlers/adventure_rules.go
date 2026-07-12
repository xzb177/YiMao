package handlers

// AdventurePhase is the server-authoritative run state.
type AdventurePhase string

const (
	AdventurePhasePlaying    AdventurePhase = "playing"
	AdventurePhaseGenerating AdventurePhase = "generating"
	AdventurePhaseRevive     AdventurePhase = "revive"
	AdventurePhaseFinishing  AdventurePhase = "finishing"
	AdventurePhaseFinished   AdventurePhase = "finished"
)

const adventureReviveHP = 30

// AdventureDamage ignores all AI supplied HP changes.
func AdventureDamage(level, total int, trap bool) int {
	boss := total > 0 && level == total
	if boss && trap {
		return 78
	}
	if boss {
		return 65
	}
	if trap {
		return 58
	}
	return 46
}

func AdventureHintCost(level int) int {
	if level < 0 {
		level = 0
	}
	return 8 + 2*level
}

// ApplyAdventureHint never kills the player.
func ApplyAdventureHint(hp, level int) (int, bool) {
	cost := AdventureHintCost(level)
	if hp <= cost {
		return hp, false
	}
	return hp - cost, true
}

// AdventureScore is deterministic and independent of AI output.
func AdventureScore(success, perfect bool, hp, total, mistakes, hints, revives, maxCombo int) int {
	completed := total
	if !success {
		completed--
		if completed < 0 {
			completed = 0
		}
	}
	score := completed*14 + hp/4 + maxCombo*3 - mistakes*5 - hints*4 - revives*8
	if success {
		score += 15
	}
	if perfect && success {
		score += 10
	}
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func AdventureGrade(score int) string {
	switch {
	case score >= 95:
		return "SSS"
	case score >= 85:
		return "SS"
	case score >= 75:
		return "S"
	case score >= 60:
		return "A"
	case score >= 45:
		return "B"
	case score >= 25:
		return "C"
	default:
		return "D"
	}
}
