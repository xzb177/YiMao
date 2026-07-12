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
