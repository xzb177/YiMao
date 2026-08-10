package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func newWashMoviePilotServer(t *testing.T, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/subscribe":
			calls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": 73}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/subscribe/73":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 73, "tmdbid": 550, "type": "电影", "best_version": 1})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/subscribe/search/73":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			t.Fatalf("unexpected MoviePilot request: %s %s", r.Method, r.URL.Path)
		}
	}))
}

func TestDispatchApprovedWashLinksVerifiedSubscription(t *testing.T) {
	var calls atomic.Int32
	server := newWashMoviePilotServer(t, &calls)
	defer server.Close()

	service := &ReviewService{
		reviewsFile: filepath.Join(t.TempDir(), "review_requests.json"),
		reviews: map[string]*ReviewRequest{
			"wash": {RequestID: "wash", BusinessType: BusinessTypeWash, Status: "approved", MediaTitle: "Fight Club", TmdbID: 550, MediaType: MediaTypeMovie},
		},
		moviepilot: NewMoviePilotClient(server.URL, "unused", ""),
	}

	req, err := service.DispatchApprovedWash("wash")
	if err != nil {
		t.Fatal(err)
	}
	if req.ID != 73 || service.reviews["wash"].SubscriptionID != 73 || service.reviews["wash"].Status != "approved" {
		t.Fatalf("dispatch state = req:%+v review:%+v", req, service.reviews["wash"])
	}
	if calls.Load() != 1 {
		t.Fatalf("create calls = %d, want 1", calls.Load())
	}
}

func TestRecoverApprovedWashesOnlyDispatchesEligibleHistory(t *testing.T) {
	var calls atomic.Int32
	server := newWashMoviePilotServer(t, &calls)
	defer server.Close()

	service := &ReviewService{
		reviewsFile: filepath.Join(t.TempDir(), "review_requests.json"),
		reviews: map[string]*ReviewRequest{
			"eligible": {RequestID: "eligible", BusinessType: BusinessTypeWash, Status: "approved", MediaTitle: "Fight Club", TmdbID: 550, MediaType: MediaTypeMovie},
			"linked":   {RequestID: "linked", BusinessType: BusinessTypeWash, Status: "approved", SubscriptionID: 12},
			"claimed":  {RequestID: "claimed", BusinessType: BusinessTypeWash, Status: "approved", WashClaimedBy: 99},
			"pending":  {RequestID: "pending", BusinessType: BusinessTypeWash, Status: "pending"},
			"ordinary": {RequestID: "ordinary", Status: "approved"},
		},
		moviepilot: NewMoviePilotClient(server.URL, "unused", ""),
	}

	service.recoverApprovedWashes()
	if calls.Load() != 1 {
		t.Fatalf("create calls = %d, want 1", calls.Load())
	}
	if service.reviews["eligible"].SubscriptionID != 73 {
		t.Fatalf("eligible history was not linked: %+v", service.reviews["eligible"])
	}
	if service.reviews["claimed"].SubscriptionID != 0 || service.reviews["linked"].SubscriptionID != 12 {
		t.Fatalf("ineligible history changed: claimed=%+v linked=%+v", service.reviews["claimed"], service.reviews["linked"])
	}
}

func TestRetryStuckRequestsRetriesApprovedWashWithoutRestart(t *testing.T) {
	var calls atomic.Int32
	server := newWashMoviePilotServer(t, &calls)
	defer server.Close()

	service := &ReviewService{
		reviewsFile: filepath.Join(t.TempDir(), "review_requests.json"),
		reviews: map[string]*ReviewRequest{
			"wash": {RequestID: "wash", BusinessType: BusinessTypeWash, Status: "approved", Stuck: true, MediaTitle: "Fight Club", TmdbID: 550, MediaType: MediaTypeMovie},
		},
		moviepilot: NewMoviePilotClient(server.URL, "unused", ""),
	}

	service.retryStuckRequests()
	if calls.Load() != 1 || service.reviews["wash"].SubscriptionID != 73 {
		t.Fatalf("periodic recovery did not link wash: calls=%d review=%+v", calls.Load(), service.reviews["wash"])
	}
}

func TestClaimWashRejectsDispatchInProgress(t *testing.T) {
	postStarted := make(chan struct{})
	releasePost := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/subscribe":
			close(postStarted)
			<-releasePost
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": 73}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/subscribe/73":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 73, "tmdbid": 550, "type": "电影", "best_version": 1})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/subscribe/search/73":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			t.Fatalf("unexpected MoviePilot request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	service := &ReviewService{
		reviewsFile: filepath.Join(t.TempDir(), "review_requests.json"),
		reviews: map[string]*ReviewRequest{
			"wash": {RequestID: "wash", BusinessType: BusinessTypeWash, Status: "approved", MediaTitle: "Fight Club", TmdbID: 550, MediaType: MediaTypeMovie},
		},
		moviepilot: NewMoviePilotClient(server.URL, "unused", ""),
	}
	dispatchDone := make(chan error, 1)
	go func() {
		_, err := service.DispatchApprovedWash("wash")
		dispatchDone <- err
	}()
	<-postStarted
	if _, err := service.ClaimWash("wash", 99); err == nil {
		close(releasePost)
		<-dispatchDone
		t.Fatal("claim succeeded while MoviePilot dispatch was in progress")
	}
	close(releasePost)
	if err := <-dispatchDone; err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if service.reviews["wash"].Status != "approved" || service.reviews["wash"].SubscriptionID != 73 {
		t.Fatalf("unexpected final state: %+v", service.reviews["wash"])
	}
}

func TestDispatchApprovedWashKeepsSubscriptionWhenLinkPersistenceFails(t *testing.T) {
	var deleted atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/subscribe":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": 73}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/subscribe/73":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 73, "tmdbid": 550, "type": "电影", "best_version": 1})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/subscribe/search/73":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/subscription/73":
			deleted.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			t.Fatalf("unexpected MoviePilot request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	service := &ReviewService{
		reviewsFile: filepath.Join(t.TempDir(), "missing", "review_requests.json"),
		reviews: map[string]*ReviewRequest{
			"wash": {RequestID: "wash", BusinessType: BusinessTypeWash, Status: "approved", MediaTitle: "Fight Club", TmdbID: 550, MediaType: MediaTypeMovie},
		},
		moviepilot: NewMoviePilotClient(server.URL, "unused", ""),
	}
	if _, err := service.DispatchApprovedWash("wash"); err == nil {
		t.Fatal("dispatch succeeded despite persistence failure")
	}
	if deleted.Load() != 0 || service.reviews["wash"].SubscriptionID != 0 {
		t.Fatalf("existing subscription was deleted or linked: deletes=%d review=%+v", deleted.Load(), service.reviews["wash"])
	}
}
