package bot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/types"
)

func TestHandlePollSearchQueryUsesUnifiedReadableSearch(t *testing.T) {
	var mu sync.Mutex
	count := ""
	var sentKeyboard map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/media/search" {
			mu.Lock()
			count = r.URL.Query().Get("count")
			mu.Unlock()
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"tmdb_id": 88, "title": "统一搜索片名", "year": 2026, "type": "movie",
			}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if kb, ok := body["reply_markup"].(map[string]any); ok {
				mu.Lock()
				sentKeyboard = kb
				mu.Unlock()
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":42,"type":"private"},"date":1,"text":"ok"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	telegram := services.NewTelegramClient("test")
	telegram.SetBaseURLForTest(server.URL, server.Client())
	manager := session.NewManager(time.Hour, 10)
	msg := &types.TelegramMessage{Text: "统一搜索", From: &types.TelegramUser{ID: 9}, Chat: &types.TelegramChat{ID: 42, Type: "private"}}
	HandlePollSearchQuery(msg, telegram, services.NewMoviePilotClient(server.URL, "test", ""), manager, nil, nil, nil, nil)

	mu.Lock()
	gotCount := count
	keyboardJSON, _ := json.Marshal(sentKeyboard)
	mu.Unlock()
	if gotCount != "8" {
		t.Fatalf("search count=%q, want 8", gotCount)
	}
	keyboardText := string(keyboardJSON)
	if !strings.Contains(keyboardText, "1 · 统一搜索片名") || !strings.Contains(keyboardText, "search_history_menu") {
		t.Fatalf("polling did not use unified readable keyboard: %s", keyboardText)
	}
}
