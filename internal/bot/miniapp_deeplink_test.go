package bot

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xzb177/yimao/internal/services"
)

func TestParseMiniAppStartPayload(t *testing.T) {
	tests := []struct {
		payload string
		ok      bool
		typeVal string
		id      int
		season  int
	}{
		{"yh_m_1273472_0", true, "movie", 1273472, 0},
		{"yh_t_302051_1", true, "tv", 302051, 1},
		{"yh_t_302051_0", false, "", 0, 0},
		{"yh_m_12_1", false, "", 0, 0},
		{"yh_x_12_0", false, "", 0, 0},
		{"yh_m_-1_0", false, "", 0, 0},
		{"bad", false, "", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.payload, func(t *testing.T) {
			got, ok := parseMiniAppStartPayload(tt.payload)
			if ok != tt.ok {
				t.Fatalf("ok=%v want %v", ok, tt.ok)
			}
			if ok && (got.Type != tt.typeVal || got.TMDBID != tt.id || got.Season != tt.season) {
				t.Fatalf("got=%+v", got)
			}
		})
	}
}

func TestSendMiniAppDeepLinkUsesDocumentedEnvironmentVariable(t *testing.T) {
	t.Setenv("MINI_APP_URL", "https://example.com/miniapp")
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":1,"type":"private"}}}`))
	}))
	defer server.Close()

	telegram := services.NewTelegramClient("test")
	telegram.SetBaseURLForTest(server.URL, server.Client())
	SendMiniAppDeepLink(telegram, 101, miniAppDeepLink{TMDBID: 550, Type: "movie"})

	replyMarkup, ok := payload["reply_markup"].(map[string]any)
	if !ok {
		t.Fatalf("reply_markup=%#v", payload["reply_markup"])
	}
	rows, ok := replyMarkup["inline_keyboard"].([]any)
	if !ok || len(rows) == 0 {
		t.Fatalf("inline_keyboard=%#v", replyMarkup["inline_keyboard"])
	}
	firstRow := rows[0].([]any)
	button := firstRow[0].(map[string]any)
	webApp := button["web_app"].(map[string]any)
	url, _ := webApp["url"].(string)
	if !strings.HasPrefix(url, "https://example.com/miniapp?") || !strings.Contains(url, "tmdb_id=550") || !strings.Contains(url, "type=movie") {
		t.Fatalf("deep-link URL=%q", url)
	}
}

func TestSendMiniAppDeepLinkFallsBackForInvalidConfiguration(t *testing.T) {
	t.Setenv("MINI_APP_URL", "https://")
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk, _ := io.ReadAll(r.Body)
		bodies = append(bodies, chunk)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":1,"type":"private"}}}`))
	}))
	defer server.Close()

	telegram := services.NewTelegramClient("test")
	telegram.SetBaseURLForTest(server.URL, server.Client())
	SendMiniAppDeepLink(telegram, 101, miniAppDeepLink{TMDBID: 550, Type: "movie"})

	joined := ""
	for _, chunk := range bodies {
		joined += string(chunk)
	}
	if strings.Contains(joined, `"web_app"`) || strings.Contains(joined, "tmdb_id=550") {
		t.Fatalf("invalid Mini App URL leaked into fallback payload: %s", joined)
	}
}
