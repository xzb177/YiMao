package richmessage

import (
	"strings"
	"testing"
)

func TestWelcomeCopyKeepsRequestFirstProductOrder(t *testing.T) {
	markdown := BuildWelcomeMessage("").Markdown
	search := strings.Index(markdown, "搜索求片")
	progress := strings.Index(markdown, "求片进度")
	more := strings.Index(markdown, "更多")
	if search < 0 || progress < 0 || more < 0 {
		t.Fatalf("welcome copy misses canonical labels: %q", markdown)
	}
	if !(search < progress && progress < more) {
		t.Fatalf("welcome hierarchy is not request-first: search=%d progress=%d more=%d", search, progress, more)
	}
	for _, hidden := range []string{"洗版", "管理", "游戏中心", "许愿池"} {
		if strings.Contains(markdown, hidden) {
			t.Fatalf("first screen leaked %q: %q", hidden, markdown)
		}
	}
	for _, legacy := range []string{"普通求片", "趣味求片", "通关才给下载"} {
		if strings.Contains(markdown, legacy) {
			t.Errorf("welcome copy contains legacy phrase %q", legacy)
		}
	}
}
