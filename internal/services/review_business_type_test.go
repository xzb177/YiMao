package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestReviewBusinessTypeBackwardCompatibilityAndDuplicateKey(t *testing.T) {
	dir := t.TempDir()
	legacy := map[string]*ReviewRequest{"old": {RequestID: "old", TelegramID: 1, TmdbID: 550, MediaType: MediaTypeMovie, Status: "pending"}}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(dir+"/review_requests.json", data, 0o600); err != nil {
		t.Fatal(err)
	}
	svc := NewReviewService(dir, false)
	old, ok := svc.GetRequest("old")
	if !ok || old.NormalizedBusinessType() != BusinessTypeRequest {
		t.Fatalf("legacy business type not normalized: %#v", old)
	}
	if _, duplicate := svc.HasActiveSimilarContentForBusiness(550, MediaTypeMovie, 0, BusinessTypeWash); duplicate {
		t.Fatal("ordinary request must not collide with wash")
	}
	wash := &ReviewRequest{RequestID: "wash", BusinessType: BusinessTypeWash, TelegramID: 1, TmdbID: 550, MediaType: MediaTypeMovie, MediaTitle: "Fight Club"}
	if err := svc.CreateRequest(wash); err != nil {
		t.Fatal(err)
	}
	if _, duplicate := svc.HasActiveSimilarContentForBusiness(550, MediaTypeMovie, 0, BusinessTypeWash); !duplicate {
		t.Fatal("wash duplicate not found")
	}
	if wash.QuotaCost != 0 {
		t.Fatalf("wash consumed quota cost: %d", wash.QuotaCost)
	}
	restored, err := svc.RestoreQuotaOnce(wash.RequestID, nil)
	if err != nil || restored {
		t.Fatalf("wash quota restore must be a persisted no-op: restored=%v err=%v", restored, err)
	}
}

func TestCreateWashIfNoActiveSimilarAndComplete(t *testing.T) {
	svc := NewReviewService(t.TempDir(), false)
	first := &ReviewRequest{RequestID: "wash-1", BusinessType: BusinessTypeWash, TelegramID: 7, TmdbID: 1425, MediaType: MediaTypeTV, Season: 2, MediaTitle: "House of Cards"}
	if _, created, err := svc.CreateRequestIfNoActiveSimilar(first); err != nil || !created {
		t.Fatalf("first create: created=%v err=%v", created, err)
	}
	second := &ReviewRequest{RequestID: "wash-2", BusinessType: BusinessTypeWash, TelegramID: 8, TmdbID: 1425, MediaType: MediaTypeTV, Season: 2, MediaTitle: "House of Cards"}
	if existing, created, err := svc.CreateRequestIfNoActiveSimilar(second); err != nil || created || existing.RequestID != first.RequestID {
		t.Fatalf("duplicate create: existing=%v created=%v err=%v", existing, created, err)
	}
	stored, _ := svc.GetRequest(first.RequestID)
	stored.Status = "approved"
	if _, err := svc.CompleteWash(first.RequestID, 99); err != nil {
		t.Fatal(err)
	}
	completed, _ := svc.GetRequest(first.RequestID)
	if completed.Status != "completed" {
		t.Fatalf("status=%q", completed.Status)
	}
}

func TestCreateWashIfNoActiveSimilarConcurrentAcrossUsers(t *testing.T) {
	svc := NewReviewService(t.TempDir(), false)
	var wg sync.WaitGroup
	created := make(chan bool, 2)
	for i, userID := range []int64{7, 8} {
		wg.Add(1)
		go func(i int, userID int64) {
			defer wg.Done()
			review := &ReviewRequest{RequestID: fmt.Sprintf("wash-concurrent-%d", i), BusinessType: BusinessTypeWash, TelegramID: userID, TmdbID: 1425, MediaType: MediaTypeTV, Season: 2, MediaTitle: "House of Cards"}
			_, wasCreated, err := svc.CreateRequestIfNoActiveSimilar(review)
			if err != nil {
				t.Errorf("create: %v", err)
				return
			}
			created <- wasCreated
		}(i, userID)
	}
	wg.Wait()
	close(created)
	count := 0
	for value := range created {
		if value {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("created=%d, want exactly one global wash work order", count)
	}
}

func TestApproveRollsBackWhenPersistenceFails(t *testing.T) {
	svc := NewReviewService(t.TempDir(), false)
	review := &ReviewRequest{RequestID: "approve-rollback", TelegramID: 7, TmdbID: 550, MediaType: MediaTypeMovie, MediaTitle: "Fight Club"}
	if err := svc.CreateRequest(review); err != nil {
		t.Fatal(err)
	}
	svc.reviewsFile = filepath.Join(t.TempDir(), "missing", "review_requests.json")
	if _, err := svc.Approve(review.RequestID, 99, review.ApproveToken); err == nil {
		t.Fatal("expected persistence error")
	}
	stored, _ := svc.GetRequest(review.RequestID)
	if stored.Status != "pending" || stored.ReviewedBy != 0 || !stored.ReviewedAt.IsZero() {
		t.Fatalf("approval state was not rolled back: %#v", stored)
	}
}
