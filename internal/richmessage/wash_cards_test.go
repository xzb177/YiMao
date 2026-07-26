package richmessage

import (
	"strings"
	"testing"
)

func TestBuildWashStatusCardPublicVariantDoesNotLeakRequester(t *testing.T) {
	card := BuildWashStatusCard(WashStatusData{Title: "Fight Club", Year: 1999, Status: "approved", Requester: "private-user", Public: true})
	if !strings.Contains(card.Markdown, "洗版") || !strings.Contains(card.Markdown, "已批准") {
		t.Fatalf("card=%q", card.Markdown)
	}
	if strings.Contains(card.Markdown, "private-user") || strings.Contains(card.Markdown, "用户") {
		t.Fatalf("public card leaked requester: %q", card.Markdown)
	}
}
