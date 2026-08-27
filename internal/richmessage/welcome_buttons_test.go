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
	for _, want := range []string{`"type":"buttons"`, `"type":"button"`, `"style":"primary"`, `"callback_data":"search:menu"`, "搜索求片"} {
		if !strings.Contains(body, want) {
			t.Fatalf("welcome missing %q", want)
		}
	}
}

func TestWelcomeMiniAppRowStaysSeparateWebApp(t *testing.T) {
	card := BuildWelcomeCard("", WelcomeOptions{MiniAppURL: "https://example.com/miniapp"})
	body := string(mustJSON(t, card.Input()))
	if !strings.Contains(body, "打开云海小程序") || !strings.Contains(body, "web_app") {
		t.Fatalf("mini app row missing: %s", body)
	}
}
