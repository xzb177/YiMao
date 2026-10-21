package services

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 审计 P1-3：stuck 自动重试耗尽后必须落终态——Status=failed、配额退
// 还一次（幂等）、管理员收到终态通知，用户侧不再永远显示「同步中」。

type alertRecord struct {
	requestID string
	title     string
	retry     int
	lastError string
}

func seedStuckRequest(s *ReviewService, id string, telegramID int64, retryCount int) {
	s.reviews[id] = &ReviewRequest{
		RequestID:  id,
		TelegramID: telegramID,
		MediaTitle: "终态测试片",
		MediaType:  MediaTypeMovie,
		Status:     "approved",
		Stuck:      true,
		RetryCount: retryCount,
		QuotaCost:  1,
		LastError:  "moviepilot unavailable",
		CreatedAt:  time.Now(),
		ReviewedAt: time.Now(),
	}
}

func TestFailExhaustedStuckRequestTerminalRefundAndAlert(t *testing.T) {
	dir := t.TempDir()
	quota := NewQuotaService(dir, nil)
	if err := quota.UseMovieQuota(42); err != nil {
		t.Fatal(err)
	}
	s := NewReviewService(dir, false)
	s.SetQuotaService(quota)
	seedStuckRequest(s, "r-terminal", 42, MaxApproveRetry)

	alerts := make(chan alertRecord, 2)
	s.Alert = func(requestID, title string, retryCount int, lastError string) {
		alerts <- alertRecord{requestID, title, retryCount, lastError}
	}

	s.failExhaustedStuckRequest("r-terminal")

	got, ok := s.GetRequest("r-terminal")
	if !ok {
		t.Fatal("request missing after terminal transition")
	}
	if got.Status != reviewStatusFailed {
		t.Fatalf("Status = %q, want %q", got.Status, reviewStatusFailed)
	}
	if got.Stuck {
		t.Fatal("Stuck still true after terminal transition")
	}
	if !got.QuotaRestored {
		t.Fatal("QuotaRestored = false, want true")
	}
	if got.RefundPending {
		t.Fatal("RefundPending still true after successful refund")
	}
	if used := quota.GetQuotaInfo(42).MovieUsed; used != 0 {
		t.Fatalf("MovieUsed = %d, want 0 after refund", used)
	}

	select {
	case a := <-alerts:
		if a.requestID != "r-terminal" || !strings.Contains(a.lastError, "终态失败") {
			t.Fatalf("unexpected alert: %+v", a)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("admin alert not sent for terminal failure")
	}

	// 幂等：再次调用不得重复退配额、不得重复告警、不得改动终态。
	s.failExhaustedStuckRequest("r-terminal")
	if used := quota.GetQuotaInfo(42).MovieUsed; used != 0 {
		t.Fatalf("MovieUsed after repeat = %d, want 0", used)
	}
	select {
	case a := <-alerts:
		t.Fatalf("unexpected second alert: %+v", a)
	case <-time.After(200 * time.Millisecond):
	}

	// 终态与退款标记必须落盘，重启后保持。
	reloaded := NewReviewService(dir, false)
	again, ok := reloaded.GetRequest("r-terminal")
	if !ok || again.Status != reviewStatusFailed || !again.QuotaRestored {
		t.Fatalf("terminal state not persisted: %#v", again)
	}

	// 终态请求不再是订阅恢复候选。
	for _, c := range s.getSubscriptionRecoveryCandidates() {
		if c.RequestID == "r-terminal" {
			t.Fatal("terminal request still in recovery candidates")
		}
	}
}

func TestFailExhaustedStuckRequestGuards(t *testing.T) {
	dir := t.TempDir()
	quota := NewQuotaService(dir, nil)
	if err := quota.UseMovieQuota(7); err != nil {
		t.Fatal(err)
	}
	if err := quota.UseMovieQuota(8); err != nil {
		t.Fatal(err)
	}
	if err := quota.UseMovieQuota(9); err != nil {
		t.Fatal(err)
	}
	s := NewReviewService(dir, false)
	s.SetQuotaService(quota)

	// 重试未耗尽：不动。
	seedStuckRequest(s, "r-early", 7, MaxApproveRetry-1)
	// 已有关联订阅：不动（防与 LinkSubscription 竞态误杀）。
	seedStuckRequest(s, "r-linked", 8, MaxApproveRetry)
	s.reviews["r-linked"].SubscriptionID = 55
	// 洗版工单：不走求片终态路径。
	seedStuckRequest(s, "r-wash", 9, MaxApproveRetry)
	s.reviews["r-wash"].BusinessType = BusinessTypeWash

	for _, id := range []string{"r-early", "r-linked", "r-wash"} {
		s.failExhaustedStuckRequest(id)
		got, _ := s.GetRequest(id)
		if got.Status != "approved" || !got.Stuck {
			t.Fatalf("%s was terminalized unexpectedly: status=%q stuck=%v", id, got.Status, got.Stuck)
		}
		if got.QuotaRestored {
			t.Fatalf("%s quota restored unexpectedly", id)
		}
	}
}

func TestFailExhaustedStuckRequestWithoutQuotaServiceKeepsRefundPending(t *testing.T) {
	dir := t.TempDir()
	s := NewReviewService(dir, false) // 未注入配额服务
	seedStuckRequest(s, "r-noquota", 11, MaxApproveRetry)

	s.failExhaustedStuckRequest("r-noquota")

	got, _ := s.GetRequest("r-noquota")
	if got.Status != reviewStatusFailed {
		t.Fatalf("Status = %q, want %q", got.Status, reviewStatusFailed)
	}
	if !got.RefundPending {
		t.Fatal("RefundPending = false, want true so startup reconciliation refunds later")
	}

	// 启动补偿路径能接住这笔待退配额。
	quota := NewQuotaService(dir, nil)
	if err := quota.UseMovieQuota(11); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RetryPendingRefunds(quota); err != nil {
		t.Fatal(err)
	}
	after, _ := s.GetRequest("r-noquota")
	if after.RefundPending || !after.QuotaRestored {
		t.Fatalf("pending refund not reconciled: %#v", after)
	}
	if used := quota.GetQuotaInfo(11).MovieUsed; used != 0 {
		t.Fatalf("MovieUsed = %d, want 0", used)
	}
}

func TestRetryStuckRequestsTerminalizesExhaustedOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "mp down", http.StatusInternalServerError)
	}))
	defer server.Close()

	dir := t.TempDir()
	quota := NewQuotaService(dir, nil)
	if err := quota.UseMovieQuota(21); err != nil {
		t.Fatal(err)
	}
	if err := quota.UseMovieQuota(22); err != nil {
		t.Fatal(err)
	}
	s := NewReviewService(dir, false)
	s.SetQuotaService(quota)
	s.moviepilot = NewMoviePilotClient(server.URL, "test-key", "")
	seedStuckRequest(s, "r-exhausted", 21, MaxApproveRetry)
	seedStuckRequest(s, "r-fresh", 22, 1)

	s.retryStuckRequests()

	exhausted, _ := s.GetRequest("r-exhausted")
	if exhausted.Status != reviewStatusFailed || !exhausted.QuotaRestored {
		t.Fatalf("exhausted request not terminalized: status=%q restored=%v", exhausted.Status, exhausted.QuotaRestored)
	}
	if used := quota.GetQuotaInfo(21).MovieUsed; used != 0 {
		t.Fatalf("MovieUsed(21) = %d, want 0", used)
	}

	fresh, _ := s.GetRequest("r-fresh")
	if fresh.Status != "approved" || !fresh.Stuck {
		t.Fatalf("fresh stuck request should stay approved/stuck, got status=%q stuck=%v", fresh.Status, fresh.Stuck)
	}
	if fresh.QuotaRestored {
		t.Fatal("fresh stuck request quota restored unexpectedly")
	}
}
