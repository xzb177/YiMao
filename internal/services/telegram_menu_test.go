package services

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestSetChatMenuButtonPayload(t *testing.T) {
	client := newTelegramTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/setChatMenuButton" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		menu := payload["menu_button"].(map[string]any)
		if menu["type"] != "web_app" || menu["text"] != "打开云海" {
			t.Fatalf("menu=%v", menu)
		}
		app := menu["web_app"].(map[string]any)
		if app["url"] != "https://example.com/miniapp" {
			t.Fatalf("app=%v", app)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	})
	if err := client.SetChatMenuButton("打开云海", "https://example.com/miniapp"); err != nil {
		t.Fatal(err)
	}
}
