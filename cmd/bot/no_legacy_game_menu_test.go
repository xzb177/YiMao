package main

import (
	"os"
	"strings"
	"testing"
)

func TestPrivateAndGroupCommandMenusDoNotContainLegacyChallenge(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, command := range []string{"adven" + "ture", "go", "rank", "mystats", "dream"} {
		if strings.Contains(text, `Command: "`+command+`"`) {
			t.Errorf("legacy command %q remains in command menus", command)
		}
	}
}
