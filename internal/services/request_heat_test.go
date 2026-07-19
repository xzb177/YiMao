package services

import (
	"testing"
	"time"
)

func TestRequestHeatRecentUsesRealRequestsAndUniqueUsers(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	reviews := NewReviewService(t.TempDir(), false)
	carpool := NewCarpoolService(t.TempDir())

	reviews.mu.Lock()
	reviews.reviews = map[string]*ReviewRequest{
		"movie-a": {RequestID: "movie-a", TelegramID: 11, TmdbID: 100, MediaTitle: "热门电影", MediaYear: 2026, MediaType: MediaTypeMovie, Status: "pending", CreatedAt: now.Add(-time.Hour)},
		"movie-b": {RequestID: "movie-b", TelegramID: 12, TmdbID: 100, MediaTitle: "热门电影", MediaYear: 2026, MediaType: MediaTypeMovie, Status: "approved", CreatedAt: now.Add(-2 * time.Hour)},
		"tv-a":    {RequestID: "tv-a", TelegramID: 21, TmdbID: 100, MediaTitle: "同号剧集", MediaYear: 2025, MediaType: MediaTypeTV, Status: "pending", Season: 2, CreatedAt: now.Add(-3 * time.Hour)},
	}
	reviews.mu.Unlock()
	carpool.Add(100, "movie", 11) // duplicate requester must not double count
	carpool.Add(100, "movie", 13)

	heat := NewRequestHeatService(reviews, carpool)
	heat.now = func() time.Time { return now }
	items := heat.Recent(7*24*time.Hour, 8)
	if len(items) != 2 {
		t.Fatalf("items=%#v, want two media-type aggregates", items)
	}
	if items[0].MediaType != "movie" || items[0].Count != 3 || items[0].TMDBID != 100 {
		t.Fatalf("movie aggregate=%#v", items[0])
	}
	if items[1].MediaType != "tv" || items[1].Count != 1 {
		t.Fatalf("tv aggregate=%#v", items[1])
	}
}

func TestRequestHeatRecentFiltersOldAndTerminalRequests(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	reviews := NewReviewService(t.TempDir(), false)
	reviews.mu.Lock()
	reviews.reviews = map[string]*ReviewRequest{
		"old":       {RequestID: "old", TelegramID: 1, TmdbID: 1, MediaTitle: "旧请求", MediaType: MediaTypeMovie, Status: "pending", CreatedAt: now.Add(-8 * 24 * time.Hour)},
		"rejected":  {RequestID: "rejected", TelegramID: 2, TmdbID: 2, MediaTitle: "已拒绝", MediaType: MediaTypeMovie, Status: "rejected", CreatedAt: now.Add(-time.Hour)},
		"completed": {RequestID: "completed", TelegramID: 3, TmdbID: 3, MediaTitle: "已完成", MediaType: MediaTypeMovie, Status: "approved", SubscriptionState: StateCompleted, CreatedAt: now.Add(-time.Hour)},
		"invalid":   {RequestID: "invalid", TelegramID: 4, TmdbID: 0, MediaTitle: "无效", MediaType: MediaTypeMovie, Status: "pending", CreatedAt: now.Add(-time.Hour)},
		"active":    {RequestID: "active", TelegramID: 5, TmdbID: 5, MediaTitle: "仍在求", MediaType: MediaTypeMovie, Status: "approved", SubscriptionState: StateSearching, CreatedAt: now.Add(-time.Hour)},
	}
	reviews.mu.Unlock()
	heat := NewRequestHeatService(reviews, nil)
	heat.now = func() time.Time { return now }
	items := heat.Recent(7*24*time.Hour, 8)
	if len(items) != 1 || items[0].Title != "仍在求" {
		t.Fatalf("items=%#v", items)
	}
}

func TestGetAllRequestsReturnsDetachedSnapshots(t *testing.T) {
	reviews := NewReviewService(t.TempDir(), false)
	reviews.mu.Lock()
	reviews.reviews = map[string]*ReviewRequest{"a": {RequestID: "a", MediaTitle: "原名"}}
	reviews.mu.Unlock()

	snapshot := reviews.GetAllRequests()
	snapshot[0].MediaTitle = "被修改"
	got, ok := reviews.GetRequest("a")
	if !ok || got.MediaTitle != "原名" {
		t.Fatalf("internal request was mutated: %#v", got)
	}
}
