package richmessage

import (
	"strings"
	"testing"
)

func TestBuildRequestProgressCardMapsLocalWorkOrderStates(t *testing.T) {
	card := BuildRequestProgressCard(RequestCardData{
		Total:      2,
		Page:       1,
		TotalPages: 1,
		Running:    2,
		Items: []RequestCardItem{
			{Title: "[洗版] 权力的游戏", State: "WORK_ORDER", Type: "电视剧", Season: 3},
			{Title: "[问题 #1] 字幕异常", State: "ISSUE_OPEN", Type: "问题"},
		},
	})

	for _, want := range []string{"🔧 处理中", "📝 待处理"} {
		if !strings.Contains(card.Markdown, want) {
			t.Fatalf("progress card missing %q: %s", want, card.Markdown)
		}
	}
	if strings.Contains(card.Markdown, "❓ 未知") {
		t.Fatalf("local work order rendered as unknown: %s", card.Markdown)
	}
}
