package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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
	first := &ReviewRequest{RequestID: "wash-1", BusinessType: BusinessTypeWash, TelegramID: 7, TmdbID: 1425, MediaType: MediaTypeTV, Season: 2, MediaTitle: "House of Cards", WashBaseline: []string{"/old/s02e01.mkv"}}
	if _, created, err := svc.CreateRequestIfNoActiveSimilar(first); err != nil || !created {
		t.Fatalf("first create: created=%v err=%v", created, err)
	}
	second := &ReviewRequest{RequestID: "wash-2", BusinessType: BusinessTypeWash, TelegramID: 8, TmdbID: 1425, MediaType: MediaTypeTV, Season: 2, MediaTitle: "House of Cards"}
	if existing, created, err := svc.CreateRequestIfNoActiveSimilar(second); err != nil || created || existing.RequestID != first.RequestID {
		t.Fatalf("duplicate create: existing=%v created=%v err=%v", existing, created, err)
	}
	if _, err := svc.Approve(first.RequestID, 99, first.ApproveToken); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ClaimWash(first.RequestID, 99); err != nil {
		t.Fatal(err)
	}
	third := &ReviewRequest{RequestID: "wash-3", BusinessType: BusinessTypeWash, TelegramID: 9, TmdbID: 1425, MediaType: MediaTypeTV, Season: 2, MediaTitle: "House of Cards"}
	if existing, created, err := svc.CreateRequestIfNoActiveSimilar(third); err != nil || created || existing.RequestID != first.RequestID || existing.Status != "claimed" {
		t.Fatalf("claimed duplicate create: existing=%v created=%v err=%v", existing, created, err)
	}
	if _, err := svc.CompleteWash(first.RequestID, 99, []string{"/old/s02e01.mkv", "/new/s02e01.mkv"}); err != nil {
		t.Fatal(err)
	}
	completed, _ := svc.GetRequest(first.RequestID)
	if completed.Status != "completed" {
		t.Fatalf("status=%q", completed.Status)
	}
}

func TestCompleteWashRequiresBaselineNewSourceAndPreservedOldSource(t *testing.T) {
	svc := NewReviewService(t.TempDir(), false)
	for _, tt := range []struct {
		name     string
		baseline []string
		current  []string
		wantErr  string
	}{
		{name: "legacy work order", current: []string{"/new/movie.mkv"}, wantErr: "缺少创建时基线"},
		{name: "no new source", baseline: []string{"/old/movie.mkv"}, current: []string{"/old/movie.mkv"}, wantErr: "未发现新增"},
		{name: "old source removed", baseline: []string{"/old/movie.mkv"}, current: []string{"/new/movie.mkv"}, wantErr: "旧版未保留"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := &ReviewRequest{RequestID: tt.name, BusinessType: BusinessTypeWash, TelegramID: 7, TmdbID: 550, MediaType: MediaTypeMovie, MediaTitle: "Fight Club", WashBaseline: tt.baseline}
			if err := svc.CreateRequest(r); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Approve(r.RequestID, 99, r.ApproveToken); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.ClaimWash(r.RequestID, 99); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.CompleteWash(r.RequestID, 99, tt.current); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err=%v, want %q", err, tt.wantErr)
			}
			if got, _ := svc.GetRequest(r.RequestID); got.Status != "claimed" || got.WashLastError == "" {
				t.Fatalf("failed verification state=%q error=%q", got.Status, got.WashLastError)
			}
		})
	}
}

func TestCompleteWashIsIdempotentAfterVerifiedCompletion(t *testing.T) {
	svc := NewReviewService(t.TempDir(), false)
	r := &ReviewRequest{RequestID: "wash-idempotent", BusinessType: BusinessTypeWash, TelegramID: 7, TmdbID: 550, MediaType: MediaTypeMovie, WashBaseline: []string{"old"}}
	if err := svc.CreateRequest(r); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(r.RequestID, 99, r.ApproveToken); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ClaimWash(r.RequestID, 99); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		got, err := svc.CompleteWash(r.RequestID, 99, []string{"old", "new"})
		if err != nil || got.Status != "completed" {
			t.Fatalf("call %d: got=%v err=%v", i+1, got, err)
		}
	}
}

func TestWashClaimReleaseAndFailureOwnership(t *testing.T) {
	svc := NewReviewService(t.TempDir(), false)
	r := &ReviewRequest{RequestID: "wash-claim", BusinessType: BusinessTypeWash, TmdbID: 550, MediaType: MediaTypeMovie, WashBaseline: []string{"old"}}
	if err := svc.CreateRequest(r); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(r.RequestID, 99, r.ApproveToken); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ClaimWash(r.RequestID, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ClaimWash(r.RequestID, 101); err == nil {
		t.Fatal("second administrator claimed the work order")
	}
	if err := svc.RecordWashVerificationFailure(r.RequestID, 101, "wrong admin"); err == nil {
		t.Fatal("unowned failure was recorded")
	}
	if err := svc.RecordWashVerificationFailure(r.RequestID, 100, "等待 Emby 扫描"); err != nil {
		t.Fatal(err)
	}
	stored, _ := svc.GetRequest(r.RequestID)
	if stored.Status != "claimed" || stored.WashLastError != "等待 Emby 扫描" {
		t.Fatalf("stored=%#v", stored)
	}
	if err := svc.ReleaseWash(r.RequestID, 101); err == nil {
		t.Fatal("other administrator released the work order")
	}
	if err := svc.ReleaseWash(r.RequestID, 100); err != nil {
		t.Fatal(err)
	}
	stored, _ = svc.GetRequest(r.RequestID)
	if stored.Status != "approved" || stored.WashClaimedBy != 0 {
		t.Fatalf("released=%#v", stored)
	}
}

func TestCleanupKeepsApprovedWashWorkOrders(t *testing.T) {
	svc := NewReviewService(t.TempDir(), false)
	r := &ReviewRequest{RequestID: "old-approved-wash", BusinessType: BusinessTypeWash, Status: "approved", ReviewedAt: time.Now().Add(-8 * 24 * time.Hour)}
	svc.reviews[r.RequestID] = r
	svc.cleanup()
	if _, ok := svc.GetRequest(r.RequestID); !ok {
		t.Fatal("approved wash was removed by seven-day cleanup")
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
