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
