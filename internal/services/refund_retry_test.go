package services

import (
	"path/filepath"
	"testing"
)

func TestRejectedRefundPendingRetriesIdempotently(t *testing.T) {
	dir := t.TempDir()
	reviews := newReviewService(dir, false)
	quotaPath := filepath.Join(dir, "quota.json")
	quota := newQuotaService(quotaPath, nil)
	if err := quota.UseMovieQuota(55); err != nil {
		t.Fatal(err)
	}
	review := &ReviewRequest{
		RequestID:  "refund-1",
		TelegramID: 55,
		MediaType:  MediaTypeMovie,
		QuotaCost:  1,
	}
	if err := reviews.CreateRequest(review); err != nil {
		t.Fatal(err)
	}
	if _, err := reviews.Reject("refund-1", 99, "no"); err != nil {
		t.Fatal(err)
	}

	quota.quotasFile = filepath.Join(quotaPath, "child")
	if _, err := reviews.RestoreQuotaOnce("refund-1", quota); err == nil {
		t.Fatal("expected injected quota persistence failure")
	}
	stored, _ := reviews.GetRequest("refund-1")
	if !stored.RefundPending || stored.QuotaRestored {
		t.Fatalf("failed refund lost compensation intent: %#v", stored)
	}
	if got := quota.GetQuotaInfo(55).MovieUsed; got != 1 {
		t.Fatalf("failed refund changed in-memory usage: %d", got)
	}

	quota.quotasFile = quotaPath
	if done, err := reviews.RetryPendingRefunds(quota); err != nil || done != 1 {
		t.Fatalf("retry: done=%d err=%v", done, err)
	}
	if done, err := reviews.RetryPendingRefunds(quota); err != nil || done != 0 {
		t.Fatalf("duplicate retry: done=%d err=%v", done, err)
	}
	stored, _ = reviews.GetRequest("refund-1")
	if stored.RefundPending || !stored.QuotaRestored {
		t.Fatalf("successful retry did not settle marker: %#v", stored)
	}
	if got := quota.GetQuotaInfo(55).MovieUsed; got != 0 {
		t.Fatalf("successful retry usage=%d, want 0", got)
	}

	reloaded := newQuotaService(quotaPath, nil)
	if err := reloaded.load(); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.GetQuotaInfo(55).MovieUsed; got != 0 {
		t.Fatalf("reloaded usage=%d, want 0", got)
	}
}
