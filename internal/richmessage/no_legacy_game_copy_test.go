package richmessage

import (
	"strings"
	"testing"
)

func TestWelcomeAndGameCenterCopyDoNotMentionLegacyChallenge(t *testing.T) {
	for _, markdown := range []string{
		BuildWelcomeMessage("").Markdown,
		BuildGameCenterCard().Markdown,
	} {
		assertNoLegacyCopy(t, markdown)
	}
}

func assertNoLegacyCopy(t *testing.T, text string) {
	t.Helper()
	for _, forbidden := range []string{"adven" + "ture", "Adven" + "ture", "电影" + "冒险", "趣味" + "闯关", "冒险" + "记录"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("legacy challenge copy %q found in %q", forbidden, text)
		}
	}
}
