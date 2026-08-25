package services

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// MoviePilot ignores page/count on /api/v1/subscribe/ and always returns the
// full list. One successful response is therefore a complete snapshot: the
// client must not request a second page nor fail with an "incomplete snapshot"
// pagination error.
func TestGetAllSubscriptionsTreatsSingleResponseAsComplete(t *testing.T) {
	var requests atomic.Int32
	var pages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		pages = append(pages, r.URL.Query().Get("page"))
		w.Header().Set("Content-Type", "application/json")
		// Same payload for every page, exactly like the real MoviePilot.
		_, _ = w.Write([]byte(`[{"id":22946,"name":"a","state":"R","media_id":"1477712"},{"id":22947,"name":"b","state":"R","media_id":"1232569"}]`))
	}))
	defer server.Close()

	client := NewMoviePilotClient(server.URL, "test-key", "")
	items, err := client.GetAllSubscriptions()
	if err != nil {
		t.Fatalf("GetAllSubscriptions returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items=%d, want 2", len(items))
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("HTTP requests=%d (pages=%v), want exactly 1 full snapshot request", got, pages)
	}
}

// Repeated calls inside the TTL must reuse the cached snapshot instead of
// re-downloading several MB per refresh step.
func TestGetAllSubscriptionsCachesSnapshotWithinTTL(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":1,"name":"cached","state":"R","media_id":"550"}]`))
	}))
	defer server.Close()

	client := NewMoviePilotClient(server.URL, "test-key", "")
	for i := 0; i < 4; i++ {
		if _, err := client.GetAllSubscriptions(); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("HTTP requests=%d, want 1 (cached within TTL)", got)
	}

	// Creating or changing a subscription must invalidate the snapshot.
	client.InvalidateSubscriptionsCache()
	if _, err := client.GetAllSubscriptions(); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("HTTP requests=%d after invalidation, want 2", got)
	}
}

func TestSubscriptionSnapshotCacheExpires(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewMoviePilotClient(server.URL, "test-key", "")
	client.statusCacheTTL = 10 * time.Millisecond
	if _, err := client.GetAllSubscriptions(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := client.GetAllSubscriptions(); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("HTTP requests=%d, want 2 after TTL expiry", got)
	}
}

// The snapshot client must allow far more than the generic 30s API budget,
// because a large library needs a long time to serialize.
func TestSubscriptionSnapshotUsesLongTimeout(t *testing.T) {
	client := NewMoviePilotClient("http://127.0.0.1:1", "test-key", "")
	if client.snapshotClient == nil {
		t.Fatal("snapshotClient must be configured")
	}
	if client.snapshotClient.Timeout < 60*time.Second {
		t.Fatalf("snapshot timeout=%v, want >= 60s", client.snapshotClient.Timeout)
	}
	if client.statusCacheTTL <= 0 {
		t.Fatal("subscription status cache TTL must be positive")
	}
}

// A slow-but-successful snapshot (longer than the generic 30s client timeout)
// must still succeed.
func TestGetAllSubscriptionsSurvivesSlowResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[{"id":7,"name":"slow","state":"R","media_id":"1"}]`)
	}))
	defer server.Close()

	client := NewMoviePilotClient(server.URL, "test-key", "")
	client.httpClient.Timeout = 50 * time.Millisecond // generic budget is too small
	items, err := client.GetAllSubscriptions()
	if err != nil {
		t.Fatalf("slow snapshot failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items=%d, want 1", len(items))
	}
}
