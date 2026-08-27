package richmessage

import (
	"strings"
	"testing"

	"github.com/xzb177/yimao/pkg/types"
)

func TestWelcomeSearchButtonIsAPI103RichButtonNotWebApp(t *testing.T) {
	card := BuildWelcomeCard("春暖花开", WelcomeOptions{})
	body := string(mustJSON(t, card.Input()))
	if strings.Contains(body, "web_app") {
		t.Fatalf("welcome 搜索求片 must not be web_app: %s", body)
	}
	for _, want := range []string{`"type":"buttons"`, `"callback_data":"search:menu"`, "搜索求片", "求片进度", "帮助", "更多"} {
		if !strings.Contains(body, want) {
			t.Fatalf("welcome missing %q in %s", want, body)
		}
	}
	if strings.Count(body, `"style":"success"`) != 1 {
		t.Fatalf("want one success button, got %s", body)
	}
	if strings.Count(body, `"style":"primary"`) != 3 {
		t.Fatalf("want three primary nav buttons, got %s", body)
	}
	for _, leak := range []string{"洗版", "管理", "游戏中心", "许愿池", "打开云海小程序", "云海求片助手"} {
		if strings.Contains(body, leak) {
			t.Fatalf("first screen leaked %q: %s", leak, body)
		}
	}
}

func TestWelcomeWithoutHeroKeepsFourButtons(t *testing.T) {
	card := BuildWelcomeCard("", WelcomeOptions{})
	stripped := WithoutHero(card.Input())
	body := string(mustJSON(t, stripped))
	if strings.Contains(body, "attach://welcome_hero") || strings.Contains(body, `"type":"photo"`) {
		t.Fatalf("hero still present: %s", body)
	}
	kb := InlineKeyboardFromBlocks(stripped)
	if kb == nil || len(kb.InlineKeyboard) != 2 {
		t.Fatalf("fallback keyboard rows=%v", kb)
	}
	if kb.InlineKeyboard[0][0].Style != types.ButtonStyleSuccess || kb.InlineKeyboard[0][0].Text != "搜索求片" {
		t.Fatalf("search button=%#v", kb.InlineKeyboard[0][0])
	}
}

func TestWelcomeMoreHidesSecondaryUntilTapped(t *testing.T) {
	card := BuildWelcomeMoreCard(WelcomeOptions{IsAdmin: true, MiniAppURL: "https://example.com/miniapp"})
	body := string(mustJSON(t, card.Input()))
	for _, want := range []string{"洗版", "许愿池", "设置", "遇到问题", "我的进度", "游戏中心", "返回", copyMoreTag, copyMoreBody} {
		if !strings.Contains(body, want) {
			t.Fatalf("more missing %q in %s", want, body)
		}
	}
	for _, leak := range []string{"打开云海小程序", "云海求片助手", "立即求片"} {
		if strings.Contains(body, leak) {
			t.Fatalf("more leaked %q", leak)
		}
	}
}
