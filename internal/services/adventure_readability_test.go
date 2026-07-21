package services

import (
	"strings"
	"testing"

	"golang.org/x/image/font"
)

func TestValidateGeneratedCinematicSceneRequiresAllFourBeats(t *testing.T) {
	scene := &AdventureScene{Description: "旧格式描述仍可用于存档兼容。", Choices: []AdventureChoice{{Text: "一", Correct: true}, {Text: "二"}, {Text: "三"}, {Text: "四"}}}
	if err := ValidateAdventureScene(scene); err != nil {
		t.Fatalf("general legacy validation should remain compatible: %v", err)
	}
	if err := validateGeneratedCinematicScene(scene); err == nil || !strings.Contains(err.Error(), "cinematic") {
		t.Fatalf("new AI response without beats must be rejected: %v", err)
	}
}

func TestValidateGeneratedCinematicSceneRejectsEllipsis(t *testing.T) {
	scene := &AdventureScene{Setting: "浓雾笼罩着已经断电的海港…", Role: "你是守在灯塔顶层的值守员", Situation: "失控货轮正朝防波堤快速撞来", Conflict: "你要关闭灯塔，还是继续引导平民船", Choices: []AdventureChoice{{Text: "一", Correct: true}, {Text: "二"}, {Text: "三"}, {Text: "四"}}}
	if err := validateGeneratedCinematicScene(scene); err == nil {
		t.Fatal("ellipsis must be rejected")
	}
}

func TestCompactCompleteSentencesNeverReturnsHalfSentence(t *testing.T) {
	text := strings.Repeat("这段没有句号所以不能安全裁切", 12)
	if got := compactCompleteSentences(text, 90); got != text {
		t.Fatalf("unsafe fallback should preserve full legacy text, got=%q", got)
	}
}

func TestCinematicDescriptionComposesDecisionCriticalBeatsWithinNinetyRunes(t *testing.T) {
	scene := &AdventureScene{
		Setting:   "投降钟响彻烟尘笼罩的君临",
		Role:      "你是丹妮莉丝，骑卓耿停在城墙",
		Situation: "弥桑黛已死，敌军却已弃剑投降",
		Conflict:  "你要接受投降，还是让复仇吞没君临",
	}
	got := scene.CinematicDescription()
	if len([]rune(got)) < 60 || len([]rune(got)) > 90 {
		t.Fatalf("cinematic description length=%d text=%q", len([]rune(got)), got)
	}
	for _, want := range []string{"君临", "你是丹妮莉丝", "敌军却已弃剑投降", "接受投降", "复仇"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing decision beat %q in %q", want, got)
		}
	}
	if !strings.HasSuffix(got, "。") || strings.Contains(got, "…") {
		t.Fatalf("description must end cleanly: %q", got)
	}
}

func TestValidateAdventureSceneRejectsOversizedCinematicBeats(t *testing.T) {
	scene := &AdventureScene{
		Setting:   "暴雨和浓雾同时笼罩着已经彻底断电的偏远海港码头",
		Role:      "你是唯一留在灯塔顶层并掌握无线电密码的值守员",
		Situation: "失控货轮正朝防波堤撞来而岸上所有撤离通道都已封死",
		Conflict:  "救援队要求你关闭灯塔但海面仍有一艘载满平民的小艇",
		Choices: []AdventureChoice{
			{Text: "选项一", Correct: true}, {Text: "选项二"}, {Text: "选项三"}, {Text: "选项四"},
		},
	}
	if err := validateGeneratedCinematicScene(scene); err == nil || !strings.Contains(err.Error(), "cinematic") {
		t.Fatalf("oversized beats must trigger cinematic regeneration: %v", err)
	}
}

func TestCinematicDescriptionFallsBackToCompleteSentencesForLegacyScene(t *testing.T) {
	scene := &AdventureScene{Description: "浓雾压住整条街。你听见门后传来急促脚步。远处警报突然响起。后面这句非常冗长而且不应以半句话进入手机卡片造成阅读负担。"}
	got := scene.CinematicDescription()
	if len([]rune(got)) > 90 || strings.Contains(got, "…") || !strings.HasSuffix(got, "。") {
		t.Fatalf("legacy fallback is not a complete compact shot: %q", got)
	}
}

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
