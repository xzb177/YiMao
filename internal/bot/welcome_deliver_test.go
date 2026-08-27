package bot

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xzb177/yimao/internal/services"
)

func TestDeliverWelcomeFallbackOmitsLegacyHTML(t *testing.T) {
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk, _ := io.ReadAll(r.Body)
		bodies = append(bodies, chunk)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "sendRichMessage") || strings.Contains(r.URL.Path, "deleteMessage") {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"ok":false,"description":"no hero"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":1,"type":"private"}}}`))
	}))
	defer server.Close()
	telegram := services.NewTelegramClient("test")
	telegram.SetBaseURLForTest(server.URL, server.Client())
	DeliverWelcome(telegram, 101, "", false)
	joined := ""
	for _, chunk := range bodies {
		joined += string(chunk)
	}
	if strings.Contains(joined, "云海求片助手") || strings.Contains(joined, "<b>") {
		t.Fatalf("legacy HTML leaked: %s", joined)
	}
	if !strings.Contains(joined, "搜索求片") {
		t.Fatalf("fallback missing 搜索求片: %s", joined)
	}
	if !strings.Contains(joined, "welcome_hero.png") && !strings.Contains(joined, "attach://welcome_hero") {
		t.Fatalf("hero not sent: %s", joined[:500])
	}
}
