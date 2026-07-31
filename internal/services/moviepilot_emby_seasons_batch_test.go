package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbyAvailableSeasonsByTMDBReturnsAllSeasonNumbersInTwoCalls(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/Users/user-1/Items":
			if r.URL.Query().Get("ParentId") == "series-1" {
				_ = json.NewEncoder(w).Encode(map[string]any{"Items": []any{map[string]any{"IndexNumber": 1}, map[string]any{"IndexNumber": 3}, map[string]any{"IndexNumber": 0}}})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{"Items": []any{map[string]any{"Id": "series-1"}}})
			}
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := NewMoviePilotClient("http://unused", "unused", "")
	client.SetEmbyConfig(server.URL, "emby-key")
	client.SetEmbyUserID("user-1")
	seasons, err := client.EmbyAvailableSeasonsByTMDB(123)
	if err != nil || calls != 2 || !seasons[1] || !seasons[3] || seasons[0] {
		t.Fatalf("seasons=%v calls=%d err=%v", seasons, calls, err)
	}
}

func TestEmbyAvailableSeasonsByTMDBFailsClosedOnTruncatedResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ParentId") == "series-1" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Items":            []any{map[string]any{"IndexNumber": 1}},
				"TotalRecordCount": 2,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Items": []any{map[string]any{"Id": "series-1"}}})
	}))
	defer server.Close()
	client := NewMoviePilotClient("http://unused", "unused", "")
	client.SetEmbyConfig(server.URL, "emby-key")
	client.SetEmbyUserID("user-1")
	if seasons, err := client.EmbyAvailableSeasonsByTMDB(123); err == nil || seasons != nil {
		t.Fatalf("expected truncated result error, seasons=%v err=%v", seasons, err)
	}
}
