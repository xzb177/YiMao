package richmessage

import (
	"strings"
	"testing"
)

// 玩法与 AI 能力已下线：欢迎页文案不得再出现历史闯关/游戏用语。
func TestWelcomeCopyDoesNotMentionRetiredEntertainment(t *testing.T) {
	assertNoLegacyCopy(t, BuildWelcomeMessage("").Markdown)
	assertNoLegacyCopy(t, BuildPage(morePage()).Markdown)
}

func assertNoLegacyCopy(t *testing.T, text string) {
	t.Helper()
	for _, forbidden := range []string{
		"adven" + "ture", "Adven" + "ture", "电影" + "冒险", "趣味" + "闯关", "冒险" + "记录",
		"游戏" + "中心", "盲盒", "轮盘", "情报站", "AI " + "解说",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("retired entertainment copy %q found in %q", forbidden, text)
		}
	}
}
