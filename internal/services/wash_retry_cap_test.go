package services

import (
	"errors"
	"strings"
	"testing"
)

func newWashWorkOrder(t *testing.T, dir string) (*ReviewService, *ReviewRequest) {
	t.Helper()
	svc := NewReviewService(dir, false)
	review := &ReviewRequest{
		RequestID:    "wash-retry-cap",
		BusinessType: BusinessTypeWash,
		TelegramID:   1,
		TmdbID:       229192,
		MediaTitle:   "Wash Target",
		MediaYear:    2023,
		MediaType:    MediaTypeTV,
		Season:       1,
		WashBaseline: []string{"/media/old.mkv"},
	}
	if err := svc.CreateRequest(review); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(review.RequestID, 99, review.ApproveToken); err != nil {
		t.Fatal(err)
	}
	return svc, review
}

// A wash work order that can never verify (old baseline source missing) must
// stop being retried automatically once the cap is reached, instead of failing
// forever on every Emby library event.
func TestWashAutomaticVerificationStopsAtRetryCap(t *testing.T) {
	svc, review := newWashWorkOrder(t, t.TempDir())

	// Baseline source is gone, so verification can never succeed.
	broken := []string{"/media/new-only.mkv"}

	attempts := 0
	for i := 0; i < MaxWashVerifyRetry+5; i++ {
		_, err := svc.CompleteWashAutomatically(review.RequestID, broken)
		if err == nil {
			t.Fatalf("attempt %d unexpectedly completed an unverifiable wash order", i+1)
		}
		if errors.Is(err, ErrWashVerificationExhausted) {
			break
		}
		attempts++
	}

	if attempts > MaxWashVerifyRetry {
		t.Fatalf("verification attempted %d times, want at most %d before giving up", attempts, MaxWashVerifyRetry)
	}
	if !svc.WashVerificationExhausted(review.RequestID) {
		t.Fatal("work order must be marked exhausted after the retry cap")
	}

	// Every further attempt is refused without touching verification again.
	_, err := svc.CompleteWashAutomatically(review.RequestID, broken)
	if !errors.Is(err, ErrWashVerificationExhausted) {
		t.Fatalf("post-cap error = %v, want ErrWashVerificationExhausted", err)
	}

	stored, ok := svc.GetRequest(review.RequestID)
	if !ok {
		t.Fatal("work order disappeared")
	}
	if stored.RetryCount != MaxWashVerifyRetry {
		t.Fatalf("retry_count=%d, want capped at %d", stored.RetryCount, MaxWashVerifyRetry)
	}
	if !strings.Contains(stored.WashLastError, "停止自动重试") {
		t.Fatalf("wash_last_error=%q, want an explicit stop marker", stored.WashLastError)
	}
	if stored.Status != "approved" {
		t.Fatalf("status=%q, want the order to remain visible for manual handling", stored.Status)
	}
}

// A verifiable order still completes normally and is never blocked by the cap.
func TestWashVerificationSucceedsBeforeCap(t *testing.T) {
	svc, review := newWashWorkOrder(t, t.TempDir())

	completed, err := svc.CompleteWashAutomatically(review.RequestID, []string{"/media/old.mkv", "/media/new-2160p.mkv"})
	if err != nil {
		t.Fatalf("verified wash completion failed: %v", err)
	}
	if completed.Status != "completed" {
		t.Fatalf("status=%q, want completed", completed.Status)
	}
	if svc.WashVerificationExhausted(review.RequestID) {
		t.Fatal("successful completion must not be marked exhausted")
	}
}
