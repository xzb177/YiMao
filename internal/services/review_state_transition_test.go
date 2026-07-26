package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRejectOnlyTransitionsPendingRequest(t *testing.T) {
	dir := t.TempDir()
	s := NewReviewService(dir, false)
	r := &ReviewRequest{RequestID: "approved", TelegramID: 1, MediaType: MediaTypeMovie}
	if err := s.CreateRequest(r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approve("approved", 99, r.ApproveToken); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reject("approved", 100, "late reject"); err == nil {
		t.Fatal("Reject must not overwrite an approved request")
	}
	got, _ := s.GetRequest("approved")
	if got.Status != "approved" {
		t.Fatalf("status=%q, want approved", got.Status)
	}
}

func TestRejectIsNotRepeatable(t *testing.T) {
	dir := t.TempDir()
	s := NewReviewService(dir, false)
	if err := s.CreateRequest(&ReviewRequest{RequestID: "pending", TelegramID: 1, MediaType: MediaTypeMovie}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reject("pending", 99, "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reject("pending", 100, "second"); err == nil {
		t.Fatal("second Reject must fail")
	}
}

func TestCreateRequestRollsBackMemoryWhenPersistenceFails(t *testing.T) {
	parent := t.TempDir()
	dataPath := filepath.Join(parent, "not-a-directory")
	if err := os.WriteFile(dataPath, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewReviewService(dataPath, false)
	if err := s.CreateRequest(&ReviewRequest{RequestID: "ghost", TelegramID: 1, MediaType: MediaTypeMovie}); err == nil {
		t.Fatal("CreateRequest must fail when persistence path is invalid")
	}
	if _, ok := s.GetRequest("ghost"); ok {
		t.Fatal("failed request remained in memory")
	}
}

func TestReviewReadAPIsReturnDetachedSnapshots(t *testing.T) {
	s := NewReviewService(t.TempDir(), false)
	review := &ReviewRequest{RequestID: "detached", TelegramID: 1, TmdbID: 550, MediaType: MediaTypeMovie, MediaTitle: "原名", EmbyInfo: &EmbySearchResult{Title: "媒体库原名"}}
	if err := s.CreateRequest(review); err != nil {
		t.Fatal(err)
	}

	assertDetached := func(name string, got *ReviewRequest, ok bool) {
		t.Helper()
		if !ok || got == nil {
			t.Fatalf("%s did not return request", name)
		}
		got.MediaTitle = "被修改"
		got.EmbyInfo.Title = "被修改"
		stored := s.reviews[review.RequestID]
		if stored.MediaTitle != "原名" || stored.EmbyInfo.Title != "媒体库原名" {
			t.Fatalf("%s exposed internal state: %#v", name, stored)
		}
	}

	got, ok := s.GetRequest(review.RequestID)
	assertDetached("GetRequest", got, ok)
	got, ok = s.GetRequestByToken(review.ApproveToken)
	assertDetached("GetRequestByToken", got, ok)
	assertDetached("GetPendingRequests", s.GetPendingRequests()[0], true)
	assertDetached("GetUserRequests", s.GetUserRequests(review.TelegramID)[0], true)
	got, ok = s.HasActiveSimilarRequest(review.TelegramID, review.TmdbID, review.MediaType, review.Season)
	assertDetached("HasActiveSimilarRequest", got, ok)
	got, ok = s.HasActiveSimilarContent(review.TmdbID, review.MediaType, review.Season)
	assertDetached("HasActiveSimilarContent", got, ok)

	if _, err := s.Approve(review.RequestID, 99, review.ApproveToken); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.reviews[review.RequestID].Stuck = true
	s.mu.Unlock()
	assertDetached("GetStuckRequests", s.GetStuckRequests()[0], true)
}

func TestRejectRollsBackWhenPersistenceFails(t *testing.T) {
	s := NewReviewService(t.TempDir(), false)
	review := &ReviewRequest{RequestID: "reject-rollback", TelegramID: 7, MediaType: MediaTypeMovie}
	if err := s.CreateRequest(review); err != nil {
		t.Fatal(err)
	}
	s.reviewsFile = filepath.Join(t.TempDir(), "missing", "review_requests.json")
	if _, err := s.Reject(review.RequestID, 99, "bad"); err == nil {
		t.Fatal("expected persistence error")
	}
	stored := s.GetUserRequests(review.TelegramID)[0]
	if stored.Status != "pending" || stored.ReviewedBy != 0 || !stored.ReviewedAt.IsZero() || stored.RejectionReason != "" {
		t.Fatalf("rejection state was not rolled back: %#v", stored)
	}
}

func TestCancelByUserRollsBackWhenPersistenceFails(t *testing.T) {
	s := NewReviewService(t.TempDir(), false)
	review := &ReviewRequest{RequestID: "cancel-rollback", TelegramID: 7, MediaType: MediaTypeMovie}
	if err := s.CreateRequest(review); err != nil {
		t.Fatal(err)
	}
	s.reviewsFile = filepath.Join(t.TempDir(), "missing", "review_requests.json")
	if err := s.CancelByUser(review.RequestID, review.TelegramID); err == nil {
		t.Fatal("expected persistence error")
	}
	stored := s.GetUserRequests(review.TelegramID)[0]
	if stored.Status != "pending" || !stored.ReviewedAt.IsZero() || stored.RejectionReason != "" {
		t.Fatalf("cancellation state was not rolled back: %#v", stored)
	}
}
