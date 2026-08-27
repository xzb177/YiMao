package bot

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xzb177/yimao/internal/richmessage"
	"github.com/xzb177/yimao/internal/services"
)

func TestDeliverWelcomeFallbackOmitsLegacyHTML(t *testing.T) {
	var bodies [][]byte
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk, _ := io.ReadAll(r.Body)
		bodies = append(bodies, chunk)
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "sendRichMessage") || strings.Contains(r.URL.Path, "sendPhoto") || strings.Contains(r.URL.Path, "deleteMessage") {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"ok":false,"description":"IMAGE_PROCESS_FAILED"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":1,"type":"private"}}}`))
	}))
	defer server.Close()
	telegram := services.NewTelegramClient("test")
	telegram.SetBaseURLForTest(server.URL, server.Client())
	DeliverWelcome(telegram, 101, "", false)
	joined := strings.Join(paths, " ")
	payload := ""
	for _, chunk := range bodies {
		payload += string(chunk)
	}
	if strings.Contains(payload, "云海求片助手") || strings.Contains(payload, "<b>") {
		t.Fatalf("legacy HTML leaked: %s", payload)
	}
	if !strings.Contains(payload, "搜索求片") {
		t.Fatalf("fallback missing 搜索求片: %s", payload)
	}
	if !strings.Contains(payload, "welcome_hero.png") && !strings.Contains(payload, "attach://welcome_hero") {
		t.Fatalf("hero not attempted: %s", payload[:min(500, len(payload))])
	}
	if !strings.Contains(joined, "sendMessage") {
		t.Fatalf("photo failure must still send welcome text, paths=%s", joined)
	}
}

func TestDeliverWelcomeSendsCopyWhenHeroCacheFails(t *testing.T) {
	richmessage.SetLiveWelcomeHero(func() ([]byte, string) {
		return nil, ""
	})
	t.Cleanup(func() { richmessage.SetLiveWelcomeHero(nil) })
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "deleteMessage") {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"ok":false,"description":"skip"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":1,"type":"private"}}}`))
	}))
	defer server.Close()
	telegram := services.NewTelegramClient("test")
	telegram.SetBaseURLForTest(server.URL, server.Client())
	DeliverWelcome(telegram, 101, "", false)
	joined := strings.Join(paths, " ")
	if !strings.Contains(joined, "sendRichMessage") && !strings.Contains(joined, "sendPhoto") && !strings.Contains(joined, "sendMessage") {
		t.Fatalf("welcome silent: %s", joined)
	}
}
