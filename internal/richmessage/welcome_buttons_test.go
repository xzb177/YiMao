package richmessage

import (
	"strings"
	"testing"
)

func TestWelcomeSearchButtonIsAPI103RichButtonNotWebApp(t *testing.T) {
	card := BuildWelcomeCard("春暖花开", WelcomeOptions{})
	body := string(mustJSON(t, card.Input()))
	if strings.Contains(body, "web_app") {
		t.Fatalf("welcome 搜索求片 must not be web_app: %s", body)
	}
	for _, want := range []string{`"type":"photo"`, `attach://welcome_hero`, `"type":"buttons"`, `"style":"primary"`, `"callback_data":"search:menu"`, "搜索求片", "求片进度", "帮助", "更多", "在线 · 可求片"} {
		if !strings.Contains(body, want) {
			t.Fatalf("welcome missing %q in %s", want, body)
		}
	}
	if strings.Count(body, `"style":"primary"`) != 1 {
		t.Fatalf("want one primary, got %s", body)
	}
	for _, leak := range []string{"洗版", "管理", "游戏中心", "许愿池", "打开云海小程序"} {
		if strings.Contains(body, leak) {
			t.Fatalf("first screen leaked %q: %s", leak, body)
		}
	}
}

func TestWelcomeMoreHidesSecondaryUntilTapped(t *testing.T) {
	card := BuildWelcomeMoreCard(WelcomeOptions{IsAdmin: true, MiniAppURL: "https://example.com/miniapp"})
	body := string(mustJSON(t, card.Input()))
	for _, want := range []string{"洗版", "游戏中心", "许愿池", "管理", "打开云海小程序", "web_app"} {
		if !strings.Contains(body, want) {
			t.Fatalf("more missing %q in %s", want, body)
		}
	}
	if strings.Contains(body, `"style":"primary"`) {
		t.Fatalf("more must not use primary: %s", body)
	}
}
