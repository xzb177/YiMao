package services

import (
	"testing"
	"time"
)

func newPendingReview(t *testing.T, svc *ReviewService, id string, age time.Duration) *ReviewRequest {
	t.Helper()
	review := &ReviewRequest{
		RequestID:    id,
		BusinessType: BusinessTypeRequest,
		TelegramID:   42,
		TelegramName: "requester",
		TmdbID:       1,
		MediaTitle:   id,
		MediaType:    MediaTypeMovie,
	}
	if err := svc.CreateRequest(review); err != nil {
		t.Fatal(err)
	}
	svc.mu.Lock()
	svc.reviews[id].CreatedAt = time.Now().Add(-age)
	err := svc.saveLocked()
	svc.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	return review
}

// Requests pending past the threshold must be reported once, oldest first, and
// never reported again once the marker is persisted.
func TestOverduePendingRequestsAreReportedOnceOldestFirst(t *testing.T) {
	svc := NewReviewService(t.TempDir(), false)
	newPendingReview(t, svc, "old-49h", 49*time.Hour)
	newPendingReview(t, svc, "oldest-30d", 30*24*time.Hour)
	newPendingReview(t, svc, "fresh-2h", 2*time.Hour)

	overdue := svc.OverduePendingRequests(48 * time.Hour)
	if len(overdue) != 2 {
		t.Fatalf("overdue=%d, want 2 (the 49h and 30d requests)", len(overdue))
	}
	if overdue[0].RequestID != "oldest-30d" {
		t.Fatalf("first overdue=%q, want the oldest request first", overdue[0].RequestID)
	}

	var reported [][]string
	svc.OnOverdueReviews = func(items []*ReviewRequest, threshold time.Duration) bool {
		ids := make([]string, 0, len(items))
		for _, item := range items {
			ids = append(ids, item.RequestID)
		}
		reported = append(reported, ids)
		return true
	}
	svc.remindOverduePendingReviews()
	svc.remindOverduePendingReviews()
	if len(reported) != 1 {
		t.Fatalf("reminder fired %d times, want exactly 1 (no spam)", len(reported))
	}
	if len(reported[0]) != 2 {
		t.Fatalf("reported ids=%v, want both overdue requests", reported[0])
	}
	if len(svc.OverduePendingRequests(48*time.Hour)) != 0 {
		t.Fatal("already reported requests must not be reported again")
	}

	// The reminder never changes the review decision: status stays pending.
	for _, id := range []string{"old-49h", "oldest-30d"} {
		stored, ok := svc.GetRequest(id)
		if !ok || stored.Status != "pending" {
			t.Fatalf("%s status=%v, want it left pending for the admin decision", id, stored)
		}
		if stored.OverdueRemindedAt == nil || stored.OverdueRemindedAt.IsZero() {
			t.Fatalf("%s missing persisted overdue marker", id)
		}
	}
}

// A failed delivery must not consume the one-shot marker.
func TestOverdueReminderRetriesWhenDeliveryFails(t *testing.T) {
	svc := NewReviewService(t.TempDir(), false)
	newPendingReview(t, svc, "undeliverable", 72*time.Hour)
	attempts := 0
	svc.OnOverdueReviews = func([]*ReviewRequest, time.Duration) bool {
		attempts++
		return false
	}
	svc.remindOverduePendingReviews()
	svc.remindOverduePendingReviews()
	if attempts != 2 {
		t.Fatalf("attempts=%d, want the reminder retried after a failed delivery", attempts)
	}
	stored, _ := svc.GetRequest("undeliverable")
	if stored.OverdueRemindedAt != nil {
		t.Fatal("failed delivery must not persist the reminded marker")
	}
}

// Threshold is configurable and falls back to the 48h default on bad input.
func TestReviewOverdueThresholdConfiguration(t *testing.T) {
	if got := reviewOverdueThreshold(); got != defaultReviewOverdueThreshold {
		t.Fatalf("default threshold=%v, want %v", got, defaultReviewOverdueThreshold)
	}
	t.Setenv("REVIEW_OVERDUE_HOURS", "6")
	if got := reviewOverdueThreshold(); got != 6*time.Hour {
		t.Fatalf("configured threshold=%v, want 6h", got)
	}
	t.Setenv("REVIEW_OVERDUE_HOURS", "not-a-number")
	if got := reviewOverdueThreshold(); got != defaultReviewOverdueThreshold {
		t.Fatalf("invalid threshold=%v, want the 48h default", got)
	}
}
