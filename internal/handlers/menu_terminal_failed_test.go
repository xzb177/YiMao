package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/services"
)

// seedReviewLedger 直接写审核账本文件，构造 CreateRequest 到不了的
// 终态（如 Status=failed），再由服务加载。
func seedReviewLedger(t *testing.T, req *services.ReviewRequest) *services.ReviewService {
	t.Helper()
	dir := t.TempDir()
	data, err := json.Marshal(map[string]*services.ReviewRequest{req.RequestID: req})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "review_requests.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return services.NewReviewService(dir, false)
}

// 审计 P1-3（用户侧文案）：终态失败的求片在「求片进度」里必须显示
// 失败（StateFailed，进「异常/失败」分组），不再永远停在「同步中」。
func TestMergePendingReviews_TerminalFailedShowsFailed(t *testing.T) {
	rs := seedReviewLedger(t, &services.ReviewRequest{
		RequestID:  "req-failed",
		TelegramID: 123,
		TmdbID:     300,
		MediaTitle: "提交失败的片",
		MediaYear:  2024,
		MediaType:  services.MediaTypeMovie,
		Status:     "failed",
		CreatedAt:  time.Now(),
	})

	h := &MyRequestsHandler{reviewSvc: rs}
	result := h.mergePendingReviews(123, nil)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if result[0].State != services.StateFailed {
		t.Errorf("state = %q, want %q (StateFailed)", result[0].State, services.StateFailed)
	}
}

// 洗版工单的 failed 是另一套既有口径，本次映射不得影响它。
func TestMergePendingReviews_WashFailedUnchanged(t *testing.T) {
	rs := seedReviewLedger(t, &services.ReviewRequest{
		RequestID:    "req-wash-failed",
		TelegramID:   123,
		TmdbID:       301,
		MediaTitle:   "洗版失败的片",
		MediaYear:    2024,
		MediaType:    services.MediaTypeMovie,
		BusinessType: services.BusinessTypeWash,
		Status:       "failed",
		CreatedAt:    time.Now(),
	})

	h := &MyRequestsHandler{reviewSvc: rs}
	result := h.mergePendingReviews(123, nil)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if result[0].State == services.StateFailed {
		t.Errorf("wash failed must not map to StateFailed, got %q", result[0].State)
	}
}
