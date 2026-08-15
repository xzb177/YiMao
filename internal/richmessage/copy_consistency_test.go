package richmessage

import (
	"strings"
	"testing"
)

func TestWelcomeCopyKeepsRequestFirstProductOrder(t *testing.T) {
	markdown := BuildWelcomeMessage("").Markdown
	search := strings.Index(markdown, "搜索求片")
	progress := strings.Index(markdown, "求片进度")
	gameCenter := strings.Index(markdown, "游戏中心")
	if search < 0 || progress < 0 || gameCenter < 0 {
		t.Fatalf("welcome copy misses canonical labels: %q", markdown)
	}
	if !(search < progress && progress < gameCenter) {
		t.Fatalf("welcome hierarchy is not request-first: search=%d progress=%d game=%d", search, progress, gameCenter)
	}
	for _, legacy := range []string{"普通求片", "趣味求片", "通关才给下载"} {
		if strings.Contains(markdown, legacy) {
			t.Errorf("welcome copy contains legacy phrase %q", legacy)
		}
	}
}
