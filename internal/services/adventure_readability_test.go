package services

import (
	"strings"
	"testing"

	"golang.org/x/image/font"
)

func TestAdventureSceneDescriptionFitsSevenLargeLinesWithoutEllipsis(t *testing.T) {
	t.Setenv("YIMAO_CJK_FONT", "/usr/share/fonts/noto/NotoSansCJK-Regular.ttc")
	faces, err := loadAdventureCardFaces()
	if err != nil {
		t.Fatal(err)
	}
	defer faces.close()
	text := "君临午后的灰光被烟尘压暗，焦油与血腥味漫过街巷。你是丹妮莉丝，骑卓耿停在城墙上；黄金团已溃，兰尼斯特士兵纷纷弃剑。投降钟声响起，红堡就在前方。弥桑黛最后的呼喊仍在耳边。"
	lines := wrapCardText(faces.body, text, 772, 7)
	if len(lines) > 7 {
		t.Fatalf("lines=%d", len(lines))
	}
	for _, line := range lines {
		if font.MeasureString(faces.body, line).Ceil() > 772 {
			t.Fatalf("line exceeds width: %q", line)
		}
	}
	if got := joinCardLines(lines); got != text {
		t.Fatalf("description was truncated:\n got=%q\nwant=%q", got, text)
	}
}

func TestNormalizeAdventureSceneCopyLimitsFutureDescriptionsAndChoices(t *testing.T) {
	scene := &AdventureScene{Description: "一二三四五六七八九十。这里是需要保留语义的后续句子。", Choices: []AdventureChoice{{Text: "这是一个非常非常长而且不适合手机阅读的选择，需要被控制在合理长度以内"}}}
	normalizeAdventureSceneCopy(scene, 18, 16)
	if len([]rune(scene.Description)) > 18 || len([]rune(scene.Choices[0].Text)) > 16 {
		t.Fatalf("normalization failed: %+v", scene)
	}
	if !strings.HasSuffix(scene.Description, "。") {
		t.Fatalf("description should end at sentence boundary: %q", scene.Description)
	}
}
