package richmessage

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCinemaWelcomeMatchesMockupA(t *testing.T) {
	card := BuildWelcomeCard("", WelcomeOptions{})
	raw, _ := json.Marshal(card.Input())
	body := string(raw)
	md := card.Markdown
	for _, want := range []string{copyKickerCinema, copyWelcomeH1, copyWelcomeTag, copyWelcomeBody, copyWelcomeStat, "搜索求片", "求片进度", "帮助", "更多"} {
		if !strings.Contains(md, want) && !strings.Contains(body, want) {
			t.Fatalf("welcome missing %q md=%q body=%s", want, md, body)
		}
	}
	if strings.Contains(md, "云海求片助手") || strings.Contains(md, "首次加载") || strings.Contains(body, "web_app") {
		t.Fatalf("legacy welcome: %q %s", md, body)
	}
	if strings.Count(body, `"style":"success"`) != 1 {
		t.Fatalf("success count %s", body)
	}
	if strings.Count(body, `"callback_data":"search:menu"`) != 1 {
		t.Fatalf("search:menu %s", body)
	}
}

func TestCinemaSearchHelpMoreCopy(t *testing.T) {
	search := BuildSearchPromptCard().Markdown
	for _, want := range []string{copySearchH1, copySearchTag, copySearchBody, "发片名", "返回"} {
		if !strings.Contains(search, want) {
			t.Fatalf("search missing %q in %q", want, search)
		}
	}
	help := BuildHelpCard().Markdown
	for _, want := range []string{copyHelpH1, copyHelpTag, copyHelpBody} {
		if !strings.Contains(help, want) {
			t.Fatalf("help missing %q in %q", want, help)
		}
	}
	more := BuildWelcomeMoreCard(WelcomeOptions{IsAdmin: true, MiniAppURL: "https://x"}).Markdown
	for _, want := range []string{copyMoreH1, copyMoreTag, copyMoreBody, "洗版", "许愿池", "设置", "返回"} {
		if !strings.Contains(more, want) {
			t.Fatalf("more missing %q in %q", want, more)
		}
	}
	if strings.Contains(more, "游戏中心") {
		t.Fatalf("more extra: %q", more)
	}
}

func TestPlaybillProgressMatchesMockupB(t *testing.T) {
	card := BuildPlaybillCard(PlaybillCard{Title: "醉玲珑", Tagline: copyPlaybillTag, Body: copyPlaybillBody, Year: "2017", Kind: "剧集", Next: "入库确认", Refresh: "requests"})
	md := card.Markdown
	for _, want := range []string{copyKickerNow, "醉玲珑", copyPlaybillTag, copyPlaybillBody, "年份", "2017", "类型", "剧集", "下一步", "入库确认", "主菜单", "刷新"} {
		if !strings.Contains(md, want) {
			t.Fatalf("playbill missing %q in %q", want, md)
		}
	}
	if strings.Contains(md, "求片进度") {
		t.Fatalf("single title must not use 求片进度 as H1: %q", md)
	}
}
