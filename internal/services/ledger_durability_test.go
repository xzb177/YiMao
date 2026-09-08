package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckedConstructorsRejectCorruptCriticalLedgers(t *testing.T) {
	t.Run("review corrupt JSON", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "review_requests.json"), []byte("{"), 0644); err != nil {
			t.Fatal(err)
		}
		if service, err := NewReviewServiceChecked(dir, false); err == nil || service != nil {
			t.Fatalf("expected nil service and parse error, got service=%v err=%v", service, err)
		}
	})
	t.Run("quota neither current nor valid legacy", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "quota.json")
		if err := os.WriteFile(path, []byte(`{"not-an-id":{"movie_used":1}}`), 0644); err != nil {
			t.Fatal(err)
		}
		if service, err := NewQuotaServiceChecked(path, nil); err == nil || service != nil {
			t.Fatalf("expected nil service and legacy parse error, got service=%v err=%v", service, err)
		}
	})
	t.Run("current quota format requires quota object", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "quota.json")
		if err := os.WriteFile(path, []byte(`{"quotas":null,"restored_requests":{}}`), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := NewQuotaServiceChecked(path, nil); err == nil {
			t.Fatal("null quota object must fail instead of silently clearing the ledger")
		}
	})
	t.Run("wrong file type", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "review_requests.json"), 0755); err != nil {
			t.Fatal(err)
		}
		if _, err := NewReviewServiceChecked(dir, false); err == nil {
			t.Fatal("directory in place of review ledger must fail")
		}
	})
	t.Run("missing files are first start", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := NewReviewServiceChecked(dir, false); err != nil {
			t.Fatalf("review first start: %v", err)
		}
		if _, err := NewQuotaServiceChecked(filepath.Join(dir, "missing.json"), nil); err != nil {
			t.Fatalf("quota first start: %v", err)
		}
	})
}

func TestQuotaUseRequiresDurableWriteAndRollsBack(t *testing.T) {
	target := filepath.Join(t.TempDir(), "ledger.json")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}
	s := newQuotaService(target, nil)
	if err := s.UseMovieQuota(11); err == nil {
		t.Fatal("movie debit unexpectedly succeeded")
	}
	if q := s.GetQuotaInfo(11); q == nil || q.MovieUsed != 0 {
		t.Fatalf("failed movie persistence changed memory: %#v", q)
	}
	if err := s.UseTVQuotaN(11, 3); err == nil {
		t.Fatal("TV debit unexpectedly succeeded")
	}
	if q := s.GetQuotaInfo(11); q.TVUsed != 0 {
		t.Fatalf("failed TV persistence changed memory: %#v", q)
	}
}

func TestQuotaUseReloadMatchesCommittedUsage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota.json")
	s := newQuotaService(path, nil)
	if err := s.UseMovieQuota(22); err != nil {
		t.Fatal(err)
	}
	if err := s.UseTVQuotaN(22, 3); err != nil {
		t.Fatal(err)
	}
	reloaded := newQuotaService(path, nil)
	if err := reloaded.load(); err != nil {
		t.Fatal(err)
	}
	q := reloaded.GetQuotaInfo(22)
	if q.MovieUsed != 1 || q.TVUsed != 3 {
		t.Fatalf("reloaded usage mismatch: %#v", q)
	}
}

func TestCleanupUsesBusinessTerminalState(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	old := now.Add(-8 * 24 * time.Hour)
	completedFresh := now.Add(-13 * 24 * time.Hour)
	completedOld := now.Add(-15 * 24 * time.Hour)
	s := newReviewService(t.TempDir(), false)
	for _, state := range []string{"N", "S", "D", "R", ""} {
		id := "active-" + state
		s.reviews[id] = &ReviewRequest{RequestID: id, Status: "approved", SubscriptionID: 7, SubscriptionState: state, ReviewedAt: old}
	}
	s.reviews["completed-fresh"] = &ReviewRequest{RequestID: "completed-fresh", Status: "approved", SubscriptionID: 8, SubscriptionState: "C", ReviewedAt: old, CompletedNoticeAt: &completedFresh}
	s.reviews["completed-old"] = &ReviewRequest{RequestID: "completed-old", Status: "approved", SubscriptionID: 9, SubscriptionState: "C", ReviewedAt: old, CompletedNoticeAt: &completedOld}
	s.reviews["rejected"] = &ReviewRequest{RequestID: "rejected", Status: "rejected", ReviewedAt: old}
	s.reviews["legacy-approved"] = &ReviewRequest{RequestID: "legacy-approved", Status: "approved", ReviewedAt: old, Stuck: true}
	s.reviews["tv-season"] = &ReviewRequest{RequestID: "tv-season", Status: "approved", MediaType: MediaTypeTV, Season: 3, SubscriptionID: 10, SubscriptionState: "S", ReviewedAt: old}
	s.reviews["wash"] = &ReviewRequest{RequestID: "wash", BusinessType: BusinessTypeWash, Status: "approved", ReviewedAt: old}

	s.cleanupAt(now)
	for _, id := range []string{"active-N", "active-S", "active-D", "active-R", "active-", "completed-fresh", "legacy-approved", "tv-season", "wash"} {
		if _, ok := s.reviews[id]; !ok {
			t.Errorf("active/recoverable record %q was deleted", id)
		}
	}
	for _, id := range []string{"completed-old", "rejected"} {
		if _, ok := s.reviews[id]; ok {
			t.Errorf("terminal expired record %q was retained", id)
		}
	}
}

func TestCleanupSaveFailureRestoresMemory(t *testing.T) {
	dir := t.TempDir()
	s := newReviewService(dir, false)
	s.reviews["rejected"] = &ReviewRequest{RequestID: "rejected", Status: "rejected", ReviewedAt: time.Now().Add(-8 * 24 * time.Hour)}
	if err := os.Mkdir(s.reviewsFile, 0755); err != nil {
		t.Fatal(err)
	}
	s.cleanupAt(time.Now())
	if _, ok := s.reviews["rejected"]; !ok {
		t.Fatal("cleanup write failure did not restore in-memory record")
	}
}
