package richmessage

import (
	"strings"
	"testing"
)

func TestWelcomeCopyKeepsRequestFirstProductOrder(t *testing.T) {
	markdown := BuildWelcomeMessage("").Markdown
	// 方案1 promotes 申请洗版 and 进入许愿 onto the first screen; the order must
	// still read request-first, with the secondary drawer last.
	order := []string{"搜索求片", "查看进度", "申请洗版", "进入许愿", "帮助说明", "更多功能"}
	last := -1
	for _, label := range order {
		at := strings.Index(markdown, label)
		if at < 0 {
			t.Fatalf("welcome copy misses canonical label %q: %q", label, markdown)
		}
		if at <= last {
			t.Fatalf("welcome hierarchy is out of order at %q: %q", label, markdown)
		}
		last = at
	}
	// Administration and secondary tools stay behind 更多功能.
	for _, hidden := range []string{"管理后台", "观影画像", "系统设置", "问题反馈"} {
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
