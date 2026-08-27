package richmessage

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPageTemplateWelcomeCopy(t *testing.T) {
	card := BuildWelcomeCard("", WelcomeOptions{})
	md := card.Markdown
	for _, want := range []string{"云海求片", "想看的，交给云海", "直接发片名，或点搜索。提交后可在进度里查到。", "在线 · 可求片"} {
		if !strings.Contains(md, want) {
			t.Fatalf("welcome missing %q in %q", want, md)
		}
	}
	raw, _ := json.Marshal(card.Input())
	body := string(raw)
	if strings.Count(body, `"callback_data":"search:menu"`) != 1 {
		t.Fatalf("search button count: %s", body)
	}
	if strings.Contains(body, "云海求片助手") || strings.Contains(md, "首次加载约需") {
		t.Fatalf("legacy copy: %q", md)
	}
	if strings.Count(body, `"style":"success"`) != 1 {
		t.Fatalf("want one success: %s", body)
	}
}

func TestHelpAndSettingsUsePageTemplate(t *testing.T) {
	help := BuildHelpCard().Markdown
	if !strings.Contains(help, "帮助") || strings.Contains(help, "云海求片助手") {
		t.Fatalf("help=%q", help)
	}
	settings := BuildSettingsCard(false).Markdown
	if !strings.Contains(settings, "设置") || !strings.Contains(settings, "绑定") {
		t.Fatalf("settings=%q", settings)
	}
	search := BuildSearchPromptCard().Markdown
	if !strings.Contains(search, "发中文或英文片名") {
		t.Fatalf("search=%q", search)
	}
	progress := BuildProgressEmptyCard(false).Markdown
	if !strings.Contains(progress, "提交过的片在这里") {
		t.Fatalf("progress=%q", progress)
	}
}
