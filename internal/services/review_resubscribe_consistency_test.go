package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestCleanupReplacedSubscriptionsRetriesUntilDeleteSucceeds(t *testing.T) {
	var failDelete atomic.Bool
	failDelete.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/subscription/41" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if failDelete.Load() {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	dataDir := t.TempDir()
	reviewsFile := filepath.Join(dataDir, "review_requests.json")
	service := &ReviewService{
		reviewsFile: reviewsFile,
		reviews: map[string]*ReviewRequest{
			"request-1": {RequestID: "request-1", SubscriptionID: 42, PendingDeleteSubscriptionID: 41},
		},
		moviepilot: NewMoviePilotClient(server.URL, "unused", ""),
	}

	service.cleanupReplacedSubscriptions()
	if got := service.reviews["request-1"].PendingDeleteSubscriptionID; got != 41 {
		t.Fatalf("cleanup marker cleared after failed delete: got %d", got)
	}

	failDelete.Store(false)
	service.cleanupReplacedSubscriptions()
	if got := service.reviews["request-1"].PendingDeleteSubscriptionID; got != 0 {
		t.Fatalf("cleanup marker not cleared after successful retry: got %d", got)
	}

	reloaded := NewReviewService(dataDir, false)
	reloadedReview, ok := reloaded.reviews["request-1"]
	if !ok {
		t.Fatal("persisted review was not reloaded")
	}
	if got := reloadedReview.PendingDeleteSubscriptionID; got != 0 {
		t.Fatalf("cleared marker was not persisted: got %d", got)
	}
}

func TestResubscribeDeletesNewSubscriptionWhenLinkPersistenceFails(t *testing.T) {
	deleted := make(chan int, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/subscribe":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data":    map[string]any{"id": 42},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/subscription/42":
			deleted <- 42
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/subscribe/search":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	service := &ReviewService{
		reviewsFile: filepath.Join(t.TempDir(), "missing", "review_requests.json"),
		reviews: map[string]*ReviewRequest{
			"request-1": {
				RequestID:         "request-1",
				MediaTitle:        "Example",
				MediaType:         MediaTypeMovie,
				TmdbID:            123,
				SubscriptionID:    41,
				SubscriptionState: "R",
			},
		},
		moviepilot: NewMoviePilotClient(server.URL, "unused", ""),
	}

	service.resubscribeRecycledRequests([]string{"request-1"})
	select {
	case got := <-deleted:
		if got != 42 {
			t.Fatalf("deleted subscription = %d, want 42", got)
		}
	default:
		t.Fatal("new subscription was not deleted after persistence failure")
	}
	review := service.reviews["request-1"]
	if review.SubscriptionID != 41 || review.SubscriptionState != "R" || review.PendingDeleteSubscriptionID != 0 {
		t.Fatalf("old subscription link changed after persistence failure: %+v", review)
	}
}

func TestResubscribeWashKeepsBestVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/subscribe":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["best_version"] != float64(1) {
				t.Fatalf("best_version = %#v", payload["best_version"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": 42}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/subscribe/42":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "tmdbid": 123, "type": "电影", "best_version": 1})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/subscribe/search/42":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/subscription/41":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	service := &ReviewService{
		reviewsFile: filepath.Join(t.TempDir(), "review_requests.json"),
		reviews: map[string]*ReviewRequest{
			"wash": {RequestID: "wash", BusinessType: BusinessTypeWash, Status: "approved", MediaTitle: "Example", TmdbID: 123, MediaType: MediaTypeMovie, SubscriptionID: 41, SubscriptionState: "R"},
		},
		moviepilot: NewMoviePilotClient(server.URL, "unused", ""),
	}
	service.resubscribeRecycledRequests([]string{"wash"})
	if service.reviews["wash"].SubscriptionID != 42 {
		t.Fatalf("wash was not relinked: %+v", service.reviews["wash"])
	}
}
