package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xzb177/yimao/internal/services"
)

func TestUserScopedSenderPrivateRichMessageUsesNativeAPI(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		payload["_path"] = r.URL.Path
		requests <- payload
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":88,"chat":{"id":42,"type":"private"}}}`))
	}))
	defer server.Close()

	client := services.NewTelegramClient("test-token")
	client.SetBaseURLForTest(server.URL, server.Client())
	sender := newUserScopedSender(client, 42, 42)

	if _, err := sender.SendRichMessage("## 富文本标题\n\n**正文**", nil); err != nil {
		t.Fatalf("SendRichMessage: %v", err)
	}
	payload := <-requests
	if payload["_path"] != "/sendRichMessage" {
		t.Fatalf("path=%v, want /sendRichMessage", payload["_path"])
	}
	rich, ok := payload["rich_message"].(map[string]any)
	if !ok || rich["markdown"] != "## 富文本标题\n\n**正文**" {
		t.Fatalf("rich_message=%v", payload["rich_message"])
	}
}
