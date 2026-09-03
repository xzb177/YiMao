package services

import (
	"strings"
	"testing"
	"time"
)

// A legacy wash work order written before the verification cap existed keeps
// retry_count far above the cap while sitting in approved state. Startup must
// terminate it so no automatic path retries it again, while the record stays
// visible for manual handling and is never deleted.
func TestLegacyExhaustedWashOrdersAreTerminatedAtStartup(t *testing.T) {
	dir := t.TempDir()
	svc := NewReviewService(dir, false)
	review := &ReviewRequest{
		RequestID:    "legacy-wash-loop",
		BusinessType: BusinessTypeWash,
		TelegramID:   1,
		TmdbID:       229192,
		MediaTitle:   "沧元图",
		MediaType:    MediaTypeTV,
		Season:       1,
		Episode:      1,
	}
	if err := svc.CreateRequest(review); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(review.RequestID, 99, review.ApproveToken); err != nil {
		t.Fatal(err)
	}
	// Reproduce the stuck legacy shape: 226 MoviePilot retries, stuck marker set.
	for i := 0; i < 226; i++ {
		svc.mu.Lock()
		svc.reviews[review.RequestID].RetryCount = 226
		svc.reviews[review.RequestID].Stuck = true
		svc.reviews[review.RequestID].LastError = "MoviePilot subscription 21754 is not a wash subscription"
		svc.mu.Unlock()
		break
	}
	svc.mu.Lock()
	if err := svc.saveLocked(); err != nil {
		svc.mu.Unlock()
		t.Fatal(err)
	}
	svc.mu.Unlock()

	// Restart: a fresh service loads the same file and must terminate the order.
	reloaded := NewReviewService(dir, false)
	stored, ok := reloaded.GetRequest(review.RequestID)
	if !ok {
		t.Fatal("terminating a stuck wash order must never delete the record")
	}
	if stored.Status != WashStatusFailed {
		t.Fatalf("status=%q, want %q so no automatic path retries it", stored.Status, WashStatusFailed)
	}
	if stored.Stuck || stored.LastError != "" {
		t.Fatalf("stuck markers survived termination: stuck=%v last_error=%q", stored.Stuck, stored.LastError)
	}
	if !strings.Contains(stored.WashLastError, "终止自动重试") || !strings.Contains(stored.WashLastError, "21754") {
		t.Fatalf("wash_last_error=%q, want the original cause plus a stop marker", stored.WashLastError)
	}
	// Still surfaced to administrators for manual handling.
	found := false
	for _, item := range reloaded.GetWashRequests() {
		if item.RequestID == review.RequestID {
			found = true
		}
	}
	if !found {
		t.Fatal("terminated order disappeared from the wash workbench queue")
	}
	failed := reloaded.GetFailedWashRequests()
	if len(failed) != 1 || failed[0].RequestID != review.RequestID {
		t.Fatalf("GetFailedWashRequests()=%+v, want the terminated order", failed)
	}
	// No automatic completion path may touch it again.
	if _, err := reloaded.CompleteWashAutomatically(review.RequestID, []string{"/media/new.mkv"}); err == nil {
		t.Fatal("automatic completion must refuse a terminated order")
	}
	// It is also gone from the MoviePilot stuck retry candidate set.
	for _, candidate := range reloaded.getSubscriptionRecoveryCandidates() {
		if candidate.RequestID == review.RequestID {
			t.Fatal("terminated wash order is still a MoviePilot retry candidate")
		}
	}
	if len(reloaded.GetStuckRequests()) != 0 {
		t.Fatalf("terminated order still listed as stuck: %+v", reloaded.GetStuckRequests())
	}
	// Idempotent: another restart does not change anything.
	again := NewReviewService(dir, false)
	second, _ := again.GetRequest(review.RequestID)
	if second.Status != WashStatusFailed || second.WashLastError != stored.WashLastError {
		t.Fatalf("second startup mutated the terminal record: %+v", second)
	}
}

// Reopening is an explicit administrator decision and restores the automatic
// verification budget.
func TestReopenWashRequiresBaselineAndResetsRetries(t *testing.T) {
	dir := t.TempDir()
	svc := NewReviewService(dir, false)
	review := &ReviewRequest{
		RequestID:    "reopen-wash",
		BusinessType: BusinessTypeWash,
		TelegramID:   1,
		TmdbID:       42,
		MediaTitle:   "Reopen Target",
		MediaType:    MediaTypeMovie,
		WashBaseline: []string{"/media/old.mkv"},
	}
	if err := svc.CreateRequest(review); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(review.RequestID, 99, review.ApproveToken); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.FailWashPermanently(review.RequestID, "核验持续失败"); err != nil {
		t.Fatal(err)
	}
	reopened, err := svc.ReopenWash(review.RequestID, 99)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	if reopened.Status != "approved" || reopened.RetryCount != 0 || reopened.WashLastError != "" {
		t.Fatalf("reopened order=%+v, want approved with a fresh verification budget", reopened)
	}
	// A baseline-less order can never verify, so reopening must be refused.
	svc.mu.Lock()
	svc.reviews[review.RequestID].WashBaseline = nil
	svc.reviews[review.RequestID].Status = WashStatusFailed
	svc.mu.Unlock()
	if _, err := svc.ReopenWash(review.RequestID, 99); err == nil {
		t.Fatal("reopening a baseline-less order must be refused")
	}
	_ = time.Now
}
