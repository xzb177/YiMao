package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
)

func TestHandleSearchQueryPageRequestsAndStoresRequestedPage(t *testing.T) {
	var mu sync.Mutex
	requestedPages := make([]string, 0, 1)
	requestedCounts := make([]string, 0, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/media/search" {
			mu.Lock()
			requestedPages = append(requestedPages, r.URL.Query().Get("page"))
			requestedCounts = append(requestedCounts, r.URL.Query().Get("count"))
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"tmdb_id": 77, "title": "第二页影片", "year": 2026,
				"type": "电影", "vote_average": 8.2,
			}})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/sendChatAction") {
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":42,"type":"private"},"date":1,"text":"ok"}}`))
	}))
	defer server.Close()

	telegram := services.NewTelegramClient("test")
	telegram.SetBaseURLForTest(server.URL, server.Client())
	manager := session.NewManager(time.Hour, 10)
	handler := NewSearchHandler(manager, telegram, services.NewMoviePilotClient(server.URL, "test", ""), nil)

	if err := handler.handleSearchQueryPage(9, 42, "测试", 2, false); err != nil {
		t.Fatalf("page search failed: %v", err)
	}

	mu.Lock()
	pages := append([]string(nil), requestedPages...)
	counts := append([]string(nil), requestedCounts...)
	mu.Unlock()
	if len(pages) != 1 || pages[0] != "2" || len(counts) != 1 || counts[0] != "8" {
		t.Fatalf("requested pages/counts = %v/%v, want [2]/[8]", pages, counts)
	}

	items, page, query, ok := manager.GetOrCreate(9).GetSearchResults()
	if !ok || page != 2 || query != "测试" || len(items) != 1 || items[0].Title != "第二页影片" {
		t.Fatalf("stored search state: ok=%v page=%d query=%q items=%+v", ok, page, query, items)
	}
}

func TestSearchResultsKeyboardUsesReadableTitlesAndStableCallbacks(t *testing.T) {
	results := []services.SearchResult{
		{ID: 101, Title: "这是一部名字非常非常长需要截断但仍然能辨认的电影", Year: services.FlexibleYear(2026), Type: "电影"},
		{ID: 202, Title: "短剧名", Year: services.FlexibleYear(2025), Type: "电视剧"},
	}
	keyboard := buildSearchResultsKeyboard(results, 2, true)

	if len(keyboard.InlineKeyboard) < 5 {
		t.Fatalf("keyboard rows = %d, want result, navigation and recovery rows", len(keyboard.InlineKeyboard))
	}
	first := keyboard.InlineKeyboard[0][0]
	if !strings.HasPrefix(first.Text, "1 · 这是一部") || !strings.HasSuffix(first.Text, "…") {
		t.Fatalf("first button text = %q", first.Text)
	}
	if first.CallbackData != "select:id:101:type:movie" {
		t.Fatalf("first callback = %q", first.CallbackData)
	}
	second := keyboard.InlineKeyboard[1][0]
	if second.CallbackData != "select:id:202:type:tv" {
		t.Fatalf("second callback = %q", second.CallbackData)
	}

	callbacks := map[string]bool{}
	for _, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			callbacks[button.CallbackData] = true
			if len([]byte(button.CallbackData)) > 64 {
				t.Fatalf("callback exceeds Telegram 64-byte limit: %q", button.CallbackData)
			}
		}
	}
	for _, want := range []string{"search:page:1", "search:page:3", "search", "search_history_menu", "start"} {
		if !callbacks[want] {
			t.Errorf("missing callback %q", want)
		}
	}
}

func TestBackToSearchPreservesPageAndReadableKeyboard(t *testing.T) {
	manager := session.NewManager(time.Hour, 10)
	sess := manager.GetOrCreate(9)
	items := []session.SearchItem{
		{ID: "101", Title: "第二页电影", Year: 2026, Type: "movie", Rating: 8.1},
		{ID: "202", Title: "第二页剧集", Year: 2025, Type: "tv", Rating: 7.9},
	}
	sess.SetSearchResults(items, 2, "返回测试")
	sess.PushNavEntry("search", "返回测试", "返回测试")

	resp, err := NewBackHandler(manager).Handle(&callback.Context{UserID: 9, ChatID: 42})
	if err != nil {
		t.Fatalf("back handler failed: %v", err)
	}
	if !strings.Contains(resp.Text, "第 2 页") || resp.Keyboard == nil {
		t.Fatalf("response did not preserve page: text=%q keyboard=%v", resp.Text, resp.Keyboard)
	}
	callbacks := map[string]bool{}
	readable := false
	for _, row := range resp.Keyboard.InlineKeyboard {
		for _, button := range row {
			callbacks[button.CallbackData] = true
			if strings.Contains(button.Text, "第二页电影") {
				readable = true
			}
		}
	}
	if !readable || !callbacks["search:page:1"] || callbacks["search:page:3"] {
		t.Fatalf("restored keyboard missing readable result/previous page or added a false next page: %+v", resp.Keyboard)
	}
	if !resp.DeleteMessage || resp.Edit {
		t.Fatalf("detail return must replace the detail message: delete=%v edit=%v", resp.DeleteMessage, resp.Edit)
	}
}
