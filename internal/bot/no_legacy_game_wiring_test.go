package bot

import (
	"os"
	"strings"
	"testing"
)

func TestCommandDispatchDoesNotContainLegacyChallengeCommands(t *testing.T) {
	for _, name := range []string{"command.go", "poll.go", "webhook.go"} {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		text := string(source)
		for _, command := range []string{"/" + "adven" + "ture", "/" + "go", "/" + "rank", "/my" + "stats", "/" + "dream"} {
			if strings.Contains(text, command) {
				t.Errorf("legacy command %q remains in %s", command, name)
			}
		}
	}
}
