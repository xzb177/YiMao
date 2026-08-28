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
	for _, want := range []string{
		`"type":"buttons"`, `"callback_data":"search:menu"`,
		"搜索求片", "查看进度", "申请洗版", "进入许愿", "帮助说明", "更多功能",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("welcome missing %q in %s", want, body)
		}
	}
	if strings.Count(body, `"style":"success"`) != 1 {
		t.Fatalf("want exactly one success button, got %s", body)
	}
	// 方案1 is two rows of three: one success plus five primary controls.
	if strings.Count(body, `"style":"primary"`) != 5 {
		t.Fatalf("want five primary nav buttons, got %s", body)
	}
	if strings.Count(body, `"type":"buttons"`) != 2 {
		t.Fatalf("welcome must emit exactly two button rows, got %s", body)
	}
	for _, legacy := range []string{"今天", "Today", "立即求片", "云海求片助手", "🔍", "🎬", "🏠"} {
		if strings.Contains(body, legacy) {
			t.Fatalf("welcome legacy label %q: %s", legacy, body)
		}
	}
	// Administration, the game drawer and any Mini App button stay off screen one.
	for _, leak := range []string{"管理后台", "游戏中心", "系统设置", "问题反馈", "打开云海小程序", "web_app"} {
		if strings.Contains(body, leak) {
			t.Fatalf("first screen leaked %q: %s", leak, body)
		}
	}
}

func TestWelcomeWithoutHeroKeepsTheSixButtonGrid(t *testing.T) {
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
	for i, row := range kb.InlineKeyboard {
		if len(row) != 3 {
			t.Fatalf("fallback row %d has %d buttons, want a 3-column grid", i, len(row))
		}
	}
	if kb.InlineKeyboard[0][0].Style != types.ButtonStyleSuccess || kb.InlineKeyboard[0][0].Text != "搜索求片" {
		t.Fatalf("search button=%#v", kb.InlineKeyboard[0][0])
	}
}

func TestWelcomeMoreHidesSecondaryUntilTapped(t *testing.T) {
	card := BuildWelcomeMoreCard(WelcomeOptions{IsAdmin: true, MiniAppURL: "https://example.com/miniapp"})
	body := string(mustJSON(t, card.Input()))
	for _, want := range []string{"系统设置", "问题反馈", "游戏中心", "返回首页", copyMoreTag, copyMoreBody} {
		if !strings.Contains(body, want) {
			t.Fatalf("more missing %q in %s", want, body)
		}
	}
	for _, leak := range []string{"打开云海小程序", "云海求片助手", "立即求片", "申请洗版", "进入许愿", "查看进度"} {
		if strings.Contains(body, leak) {
			t.Fatalf("more leaked %q", leak)
		}
	}
}
