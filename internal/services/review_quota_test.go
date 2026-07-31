package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReviewRequestQuotaFieldsPersist(t *testing.T) {
	dir := t.TempDir()
	s := NewReviewService(dir, false)
	if err := s.CreateRequest(&ReviewRequest{RequestID: "tv-1", TelegramID: 42, MediaType: MediaTypeTV, QuotaCost: 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RestoreQuotaOnce("tv-1", NewQuotaService(dir, nil)); err != nil {
		t.Fatal(err)
	}

	reloaded := NewReviewService(dir, false)
	r, ok := reloaded.GetRequest("tv-1")
	if !ok || r.QuotaCost != 3 || !r.QuotaRestored {
		t.Fatalf("quota fields not persisted: %#v", r)
	}
}

func TestRestoreQuotaOnceIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	quota := NewQuotaService(dir, nil)
	if err := quota.UseTVQuotaN(7, 3); err != nil {
		t.Fatal(err)
	}
	s := NewReviewService(dir, false)
	if err := s.CreateRequest(&ReviewRequest{RequestID: "tv", TelegramID: 7, MediaType: MediaTypeTV, QuotaCost: 3}); err != nil {
		t.Fatal(err)
	}

	first, err := s.RestoreQuotaOnce("tv", quota)
	if err != nil || !first {
		t.Fatalf("first restore = %v, %v; want true, nil", first, err)
	}
	second, err := s.RestoreQuotaOnce("tv", quota)
	if err != nil || second {
		t.Fatalf("second restore = %v, %v; want false, nil", second, err)
	}
	if got := quota.GetQuotaInfo(7).TVUsed; got != 0 {
		t.Fatalf("TVUsed = %d, want 0", got)
	}
}

func TestDelayedAsyncSaveCannotOverwriteRestoreLedger(t *testing.T) {
	dir := t.TempDir()
	quota := NewQuotaService(dir, nil)
	if err := quota.UseMovieQuota(17); err != nil {
		t.Fatal(err)
	}

	quota.mu.RLock()
	stale := quota.snapshotLocked()
	quota.mu.RUnlock()
	if restored, err := quota.RestoreQuotaForRequest("delayed", 17, string(MediaTypeMovie), 1); err != nil || !restored {
		t.Fatalf("restore = %v, %v; want true, nil", restored, err)
	}

	quota.saveAsync(stale)
	reloaded := NewQuotaService(dir, nil)
	if restored, err := reloaded.RestoreQuotaForRequest("delayed", 17, string(MediaTypeMovie), 1); err != nil || restored {
		t.Fatalf("second restore after delayed save = %v, %v; want false, nil", restored, err)
	}
}

func TestUpdateSubscriptionInfoRollsBackOnWriteFailure(t *testing.T) {
	dir := t.TempDir()
	s := NewReviewService(dir, false)
	if err := s.CreateRequest(&ReviewRequest{RequestID: "link", SubscriptionID: 17, SubscriptionState: "R"}); err != nil {
		t.Fatal(err)
	}
	s.reviewsFile = filepath.Join(dir, "missing", "review_requests.json")
	if err := s.UpdateSubscriptionInfo("link", 99, "N"); err == nil {
		t.Fatal("UpdateSubscriptionInfo succeeded with an invalid target path")
	}
	r, _ := s.GetRequest("link")
	if r.SubscriptionID != 17 || r.SubscriptionState != "R" {
		t.Fatalf("subscription state was not rolled back: %#v", r)
	}
}

func TestLinkSubscriptionPersistsLinkAndClearsStuck(t *testing.T) {
	dir := t.TempDir()
	s := NewReviewService(dir, false)
	if err := s.CreateRequest(&ReviewRequest{RequestID: "stuck", Stuck: true, LastError: "temporary"}); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkSubscription("stuck", 8428, "S"); err != nil {
		t.Fatal(err)
	}
	r, _ := NewReviewService(dir, false).GetRequest("stuck")
	if r.SubscriptionID != 8428 || r.SubscriptionState != "S" || r.Stuck || r.LastError != "" {
		t.Fatalf("linked state not persisted: %#v", r)
	}
}

func TestRestoreQuotaOnceLegacyJSONInfersCost(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"old":{"request_id":"old","telegram_id":9,"media_type":"tv","status":"pending"}}`
	if err := os.WriteFile(filepath.Join(dir, "review_requests.json"), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	quota := NewQuotaService(dir, nil)
	if err := quota.UseTVQuotaN(9, 3); err != nil {
		t.Fatal(err)
	}
	s := NewReviewService(dir, false)
	restored, err := s.RestoreQuotaOnce("old", quota)
	if err != nil || !restored {
		t.Fatalf("legacy restore = %v, %v", restored, err)
	}
	r, _ := s.GetRequest("old")
	if r.QuotaCost != 3 || !r.QuotaRestored {
		t.Fatalf("legacy fields = cost %d restored %v", r.QuotaCost, r.QuotaRestored)
	}
	if got := quota.GetQuotaInfo(9).TVUsed; got != 0 {
		t.Fatalf("TVUsed = %d, want 0", got)
	}
}

func TestRestoreQuotaOnceDoesNotDoubleRestoreAfterReviewWriteFailure(t *testing.T) {
	dir := t.TempDir()
	quota := NewQuotaService(dir, nil)
	if err := quota.UseMovieQuota(11); err != nil {
		t.Fatal(err)
	}
	s := NewReviewService(dir, false)
	if err := s.CreateRequest(&ReviewRequest{RequestID: "movie", TelegramID: 11, MediaType: MediaTypeMovie, QuotaCost: 1}); err != nil {
		t.Fatal(err)
	}

	originalReviewFile := s.reviewsFile
	s.reviewsFile = filepath.Join(dir, "missing", "review_requests.json")
	if restored, err := s.RestoreQuotaOnce("movie", quota); err == nil || restored {
		t.Fatalf("first restore = %v, %v; want false, write error", restored, err)
	}
	if got := quota.GetQuotaInfo(11).MovieUsed; got != 0 {
		t.Fatalf("MovieUsed after partial failure = %d, want 0", got)
	}

	s.reviewsFile = originalReviewFile
	if restored, err := s.RestoreQuotaOnce("movie", quota); err != nil || !restored {
		t.Fatalf("reconcile restore = %v, %v; want true, nil", restored, err)
	}
	if got := quota.GetQuotaInfo(11).MovieUsed; got != 0 {
		t.Fatalf("MovieUsed after reconcile = %d, want 0", got)
	}

	reloadedQuota := NewQuotaService(dir, nil)
	if restored, err := reloadedQuota.RestoreQuotaForRequest("movie", 11, "movie", 1); err != nil || restored {
		t.Fatalf("ledger after restart = %v, %v; want false, nil", restored, err)
	}
}
