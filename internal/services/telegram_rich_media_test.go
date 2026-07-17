package services

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestSendRichMessageWithPhotoUsesSingleInlineMediaCard(t *testing.T) {
	client := newTelegramTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sendRichMessage" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		rich := payload["rich_message"].(map[string]any)
		markdown := rich["markdown"].(string)
		if !strings.HasPrefix(markdown, "![](tg://photo?id=poster)\n\n## Title") {
			t.Fatalf("markdown=%q", markdown)
		}
		media := rich["media"].([]any)
		if len(media) != 1 {
			t.Fatalf("media=%v", media)
		}
		item := media[0].(map[string]any)
		if item["id"] != "poster" {
			t.Fatalf("id=%v", item["id"])
		}
		photo := item["media"].(map[string]any)
		if photo["type"] != "photo" || photo["media"] != "https://example.com/poster.jpg" {
			t.Fatalf("photo=%v", photo)
		}
		writeMessageOK(t, w)
	})
	if _, err := client.SendRichMessageWithPhoto(42, "## Title", "https://example.com/poster.jpg", nil); err != nil {
		t.Fatal(err)
	}
}
