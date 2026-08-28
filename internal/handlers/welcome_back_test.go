package handlers

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWelcomeAndBackKeepFourButtons(t *testing.T) {
	resp := welcomeCallbackResponse("", false)
	if resp == nil || resp.StructuredRichMessage == nil {
		t.Fatal("missing welcome payload")
	}
	if strings.Contains(resp.Text, "云海求片助手") || strings.Contains(resp.Text, "<b>") {
		t.Fatalf("legacy HTML fallback: %q", resp.Text)
	}
	raw, err := json.Marshal(resp.StructuredRichMessage)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{"搜索求片", "查看进度", "帮助说明", "更多功能", "search:menu"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
	if strings.Contains(body, "web_app") {
		t.Fatal("搜索求片 must not be web_app")
	}
	if resp.Keyboard == nil || !resp.Keyboard.RemoveKeyboard {
		t.Fatal("welcome should remove composer keyboard")
	}
}
