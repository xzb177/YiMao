package handlers

import (
	"io"
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

type capturedSearchSend struct {
	path        string
	contentType string
	body        string
}

// newSearchSendRecorder records every Telegram send so a test can assert which
// transport and payload a search surface actually used.
func newSearchSendRecorder() (*httptest.Server, func() []capturedSearchSend) {
	var mu sync.Mutex
	sends := make([]capturedSearchSend, 0, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(r.URL.Path, "/send") && !strings.Contains(r.URL.Path, "sendChatAction") {
			mu.Lock()
			sends = append(sends, capturedSearchSend{path: r.URL.Path, contentType: r.Header.Get("Content-Type"), body: string(body)})
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":42,"type":"private"},"date":1,"text":"ok"}}`))
	}))
	return server, func() []capturedSearchSend {
		mu.Lock()
		defer mu.Unlock()
		return append([]capturedSearchSend(nil), sends...)
	}
}

func stubSearchVisualCards(t *testing.T) {
	t.Helper()
	original := searchVisualCards
	searchVisualCards = func(results []services.SearchResult, statuses map[string]string) []services.SearchVisualCard {
		cards := make([]services.SearchVisualCard, 0, len(results))
		for i := range results {
			cards = append(cards, services.SearchVisualCard{ResultIndex: i, JPEG: []byte("jpeg")})
		}
		return cards
	}
	t.Cleanup(func() { searchVisualCards = original })
}

// TestWashSearchResultsShareStructuredRichRenderingWithLeadingNotice pins the
// contract: a wash title picker renders through the same structured rich card as
// a request search and only prepends one explanation block. A request search
// never mentions 洗版.
func TestWashSearchResultsShareStructuredRichRenderingWithLeadingNotice(t *testing.T) {
	server, sends := newSearchSendRecorder()
	defer server.Close()
	stubSearchVisualCards(t)

	telegram := services.NewTelegramClient("test")
	telegram.SetBaseURLForTest(server.URL, server.Client())
	manager := session.NewManager(time.Hour, 10)
	handler := NewSearchHandler(manager, telegram, services.NewMoviePilotClient(server.URL, "test", ""), nil)

	results := &services.SearchResponse{Results: []services.SearchResult{
		{ID: 11, Title: "候选一号", Year: 2026, Type: "电影", Rating: 8.4, Overview: "简介一。"},
		{ID: 22, Title: "候选二号", Year: 2025, Type: "电视剧", Rating: 7.7, Overview: "简介二。"},
	}}

	manager.GetOrCreate(9).Set("media_search_intent", "wash")
	handler.sendSearchResults(9, 42, "候选", results, 1)

	got := sends()
	if len(got) != 1 {
		t.Fatalf("wash search sends = %d, want 1: %+v", len(got), got)
	}
	wash := got[0]
	if wash.path != "/sendRichMessage" || !strings.HasPrefix(wash.contentType, "multipart/form-data;") {
		t.Fatalf("wash search must use the shared structured rich transport: path=%s type=%s", wash.path, wash.contentType)
	}
	noticeAt := strings.Index(wash.body, "为洗版选择影片")
	slideshowAt := strings.Index(wash.body, "slideshow")
	if noticeAt < 0 || slideshowAt < 0 || noticeAt > slideshowAt {
		t.Fatalf("wash explanation must be the first段 before the shared cards: notice=%d slideshow=%d", noticeAt, slideshowAt)
	}
	if !strings.Contains(wash.body, `"type":"paragraph"`) {
		t.Fatalf("wash explanation must be a paragraph block: %s", wash.body)
	}
	for _, want := range []string{"attach://search_card_1", "attach://search_card_2", "select:id:11:type:movie", "select:id:22:type:tv", "search_history_menu"} {
		if !strings.Contains(wash.body, want) {
			t.Fatalf("wash card lost shared search field %q", want)
		}
	}

	manager.GetOrCreate(9).Delete("media_search_intent")
	handler.sendSearchResults(9, 42, "候选", results, 1)
	got = sends()
	if len(got) != 2 {
		t.Fatalf("request search sends = %d, want 2: %+v", len(got), got)
	}
	plain := got[1]
	if plain.path != "/sendRichMessage" {
		t.Fatalf("request search transport changed: %s", plain.path)
	}
	if strings.Contains(plain.body, "洗版") {
		t.Fatalf("request search leaked wash copy: %s", plain.body)
	}
}

// TestWashSearchPaginationKeepsLeadingNotice pins that page 2 of a wash title
// picker still renders the shared search card with exactly one leading
// explanation paragraph.
func TestWashSearchPaginationKeepsLeadingNotice(t *testing.T) {
	stubSearchVisualCards(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/api/v1/media/search") {
			_, _ = w.Write([]byte(`[{"tmdb_id":33,"title":"第二页一号","year":"2026","type":"电影"},{"tmdb_id":44,"title":"第二页二号","year":"2025","type":"电视剧"}]`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer server.Close()

	telegram := services.NewTelegramClient("test")
	telegram.SetBaseURLForTest(server.URL, server.Client())
	manager := session.NewManager(time.Hour, 10)
	handler := NewSearchHandler(manager, telegram, services.NewMoviePilotClient(server.URL, "test", ""), nil)

	sess := manager.GetOrCreate(7)
	sess.SetSearchResults([]session.SearchItem{{ID: "11", Title: "第一页", Type: "movie"}}, 1, "候选")
	sess.Set("media_search_intent", "wash")

	ctx := &callback.Context{UserID: 7, ChatID: 42, ChatType: "private", Callback: &callback.Callback{Params: map[string]string{"page": "2"}}}
	resp, err := handler.handlePage(ctx, "2")
	if err != nil {
		t.Fatalf("handlePage error: %v", err)
	}
	if resp == nil || resp.StructuredRichMessage == nil {
		t.Fatalf("wash pagination lost the shared structured card: %+v", resp)
	}
	blocks := resp.StructuredRichMessage.Blocks
	if len(blocks) == 0 || blocks[0].Type != "paragraph" || blocks[0].Text != washSearchNotice {
		t.Fatalf("wash pagination must keep one leading explanation paragraph, got %+v", blocks)
	}
	for _, block := range blocks[1:] {
		if block.Type == "paragraph" && block.Text == washSearchNotice {
			t.Fatalf("wash explanation duplicated: %+v", blocks)
		}
	}

	sess.Delete("media_search_intent")
	plain, err := handler.handlePage(ctx, "2")
	if err != nil {
		t.Fatalf("request handlePage error: %v", err)
	}
	if plain == nil || plain.StructuredRichMessage == nil {
		t.Fatalf("request pagination lost the shared card: %+v", plain)
	}
	for _, block := range plain.StructuredRichMessage.Blocks {
		if text, ok := block.Text.(string); ok && strings.Contains(text, "洗版") {
			t.Fatalf("request pagination leaked wash copy: %q", text)
		}
	}
	if strings.Contains(plain.Text, "洗版") {
		t.Fatalf("request pagination text leaked wash copy: %q", plain.Text)
	}
}

// TestWashSearchBackRestoreKeepsLeadingNotice pins that returning from a detail
// view to the wash title picker keeps the same shared card plus exactly one
// leading explanation paragraph, and that a request search stays clean.
func TestWashSearchBackRestoreKeepsLeadingNotice(t *testing.T) {
	stubSearchVisualCards(t)
	manager := session.NewManager(time.Hour, 10)
	back := NewBackHandler(manager)

	sess := manager.GetOrCreate(5)
	sess.SetSearchResults([]session.SearchItem{
		{ID: "11", Title: "返回候选一", Year: 2026, Type: "movie", Overview: "简介一。", Status: "已在库"},
		{ID: "22", Title: "返回候选二", Year: 2025, Type: "tv", Overview: "简介二。", Status: "已在库"},
	}, 1, "候选")
	sess.PushNavEntry("search", "候选", "")
	sess.Set("media_search_intent", "wash")

	resp, err := back.Handle(&callback.Context{UserID: 5, ChatID: 42, ChatType: "private", Callback: &callback.Callback{}})
	if err != nil {
		t.Fatalf("back handle error: %v", err)
	}
	if resp == nil || resp.StructuredRichMessage == nil {
		t.Fatalf("wash back restore lost the shared structured card: %+v", resp)
	}
	blocks := resp.StructuredRichMessage.Blocks
	if len(blocks) == 0 || blocks[0].Type != "paragraph" || blocks[0].Text != washSearchNotice {
		t.Fatalf("wash back restore must keep one leading explanation paragraph, got %+v", blocks)
	}
	for _, block := range blocks[1:] {
		if block.Type == "paragraph" && block.Text == washSearchNotice {
			t.Fatalf("wash explanation duplicated on back restore: %+v", blocks)
		}
	}
	if !strings.Contains(resp.Text, washSearchNotice) {
		t.Fatalf("wash back restore text lost the notice: %q", resp.Text)
	}

	sess.Delete("media_search_intent")
	sess.PushNavEntry("search", "候选", "")
	plain, err := back.Handle(&callback.Context{UserID: 5, ChatID: 42, ChatType: "private", Callback: &callback.Callback{}})
	if err != nil {
		t.Fatalf("request back handle error: %v", err)
	}
	if plain == nil || plain.StructuredRichMessage == nil {
		t.Fatalf("request back restore lost the shared card: %+v", plain)
	}
	if strings.Contains(plain.Text, "洗版") {
		t.Fatalf("request back restore leaked wash copy: %q", plain.Text)
	}
	for _, block := range plain.StructuredRichMessage.Blocks {
		if text, ok := block.Text.(string); ok && strings.Contains(text, "洗版") {
			t.Fatalf("request back restore leaked wash copy in blocks: %q", text)
		}
	}
}
