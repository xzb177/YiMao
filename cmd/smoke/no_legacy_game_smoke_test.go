package main

import (
	"os"
	"strings"
	"testing"
)

func TestSmokeDoesNotRequireLegacyChallengeCommand(t *testing.T) {
	source, err := os.ReadFile("checks.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if strings.Contains(text, `"adven`+`ture"`) || strings.Contains(text, `"game_daily_`+`challenge"`) {
		t.Fatal("smoke command requirements still include legacy challenge")
	}
}
