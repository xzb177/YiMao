package handlers

import (
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
)

// ─── mergePendingReviews 测试 ───

func TestMergePendingReviews_NilReviewSvc(t *testing.T) {
	h := &MyRequestsHandler{reviewSvc: nil}
	mpItems := []services.SubscribeItem{
		{ID: 1, Name: "流浪地球", TMDBID: 100, State: "S"},
	}
	result := h.mergePendingReviews(123, mpItems)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if result[0].Name != "流浪地球" {
		t.Errorf("expected 流浪地球, got %s", result[0].Name)
	}
}

func TestMergePendingReviews_MergesPendingReview(t *testing.T) {
	rs := services.NewReviewService(t.TempDir(), false)
	// 创建一条 pending 审核单
	rs.CreateRequest(&services.ReviewRequest{
		RequestID:  "req-1",
		TelegramID: 123,
		TmdbID:     200,
		MediaTitle: "三体",
		MediaYear:  2023,
		MediaType:  services.MediaTypeTV,
		Season:     1,
		CreatedAt:  time.Now(),
	})

	h := &MyRequestsHandler{reviewSvc: rs}
	mpItems := []services.SubscribeItem{
		{ID: 1, Name: "流浪地球", TMDBID: 100, State: "S"},
	}
	result := h.mergePendingReviews(123, mpItems)

	// 应该有 2 条：pending 在前，MP 在后
	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}
	if result[0].Name != "三体" {
		t.Errorf("expected 三体 first (pending), got %s", result[0].Name)
	}
	if result[0].State != stateReviewing {
		t.Errorf("expected state %s, got %s", stateReviewing, result[0].State)
	}
	if result[0].ID != 0 {
		t.Errorf("synthetic item should have ID 0, got %d", result[0].ID)
	}
	if result[1].Name != "流浪地球" {
		t.Errorf("expected 流浪地球 second (MP), got %s", result[1].Name)
	}
}

func TestMergePendingReviews_DeduplicatesByTMDBAndSeason(t *testing.T) {
	rs := services.NewReviewService(t.TempDir(), false)
	// pending 审核单 TMDBID=100, Season=0
	rs.CreateRequest(&services.ReviewRequest{
		RequestID:  "req-1",
		TelegramID: 123,
		TmdbID:     100,
		MediaTitle: "流浪地球",
		MediaYear:  2019,
		MediaType:  services.MediaTypeMovie,
		Season:     0,
		CreatedAt:  time.Now(),
	})

	h := &MyRequestsHandler{reviewSvc: rs}
	// MP 里已经有 TMDBID=100 的条目
	mpItems := []services.SubscribeItem{
		{ID: 1, Name: "流浪地球", TMDBID: 100, State: "S"},
	}
	result := h.mergePendingReviews(123, mpItems)

	// 应该只有 1 条（MP 为准，pending 被去重）
	if len(result) != 1 {
		t.Fatalf("expected 1 item (deduplicated), got %d", len(result))
	}
	if result[0].ID != 1 {
		t.Errorf("should keep MP item (ID=1), got ID=%d", result[0].ID)
	}
}

func TestMergePendingReviews_SkipsRejected(t *testing.T) {
	rs := services.NewReviewService(t.TempDir(), false)
	rs.CreateRequest(&services.ReviewRequest{
		RequestID:  "req-rejected",
		TelegramID: 123,
		TmdbID:     300,
		MediaTitle: "被拒的片",
		CreatedAt:  time.Now(),
	})
	// CreateRequest 强制设为 pending，需要手动改 status
	req, _ := rs.GetRequest("req-rejected")
	req.Status = "rejected"

	h := &MyRequestsHandler{reviewSvc: rs}
	result := h.mergePendingReviews(123, nil)

	// rejected 不出现在列表里
	if len(result) != 0 {
		t.Fatalf("expected 0 items (rejected skipped), got %d", len(result))
	}
}

func TestMergePendingReviews_StuckApproved(t *testing.T) {
	rs := services.NewReviewService(t.TempDir(), false)
	rs.CreateRequest(&services.ReviewRequest{
		RequestID:  "req-stuck",
		TelegramID: 123,
		TmdbID:     400,
		MediaTitle: "卡住的片",
		MediaYear:  2024,
		MediaType:  services.MediaTypeMovie,
		CreatedAt:  time.Now(),
	})
	// CreateRequest 强制设为 pending，需要手动改 status + stuck
	req, _ := rs.GetRequest("req-stuck")
	req.Status = "approved"
	req.Stuck = true

	h := &MyRequestsHandler{reviewSvc: rs}
	result := h.mergePendingReviews(123, nil)

	if len(result) != 1 {
		t.Fatalf("expected 1 stuck item, got %d", len(result))
	}
	if result[0].State != stateStuck {
		t.Errorf("expected state %s, got %s", stateStuck, result[0].State)
	}
}

// ─── HandleCancelReview 测试 ───

func TestHandleCancelReview_NoPending(t *testing.T) {
	rs := services.NewReviewService(t.TempDir(), false)
	h := &MyRequestsHandler{reviewSvc: rs}
	ctx := &callback.Context{
		UserID: 123,
		Callback: &callback.Callback{
			Action: "myreq_cancel",
			Params: map[string]string{},
		},
	}
	resp, err := h.HandleCancelReview(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.CallbackMsg != "没有可撤回的申请" {
		t.Errorf("expected '没有可撤回的申请', got %q", resp.CallbackMsg)
	}
}

func TestHandleCancelReview_NilReviewSvc(t *testing.T) {
	h := &MyRequestsHandler{reviewSvc: nil}
	ctx := &callback.Context{
		UserID: 123,
		Callback: &callback.Callback{
			Action: "myreq_cancel",
			Params: map[string]string{},
		},
	}
	resp, err := h.HandleCancelReview(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.CallbackMsg != "服务未就绪" {
		t.Errorf("expected '服务未就绪', got %q", resp.CallbackMsg)
	}
}

// ─── buildRequestsMessage 测试 ───

func TestBuildRequestsMessage_Empty(t *testing.T) {
	h := &MyRequestsHandler{}
	msg, kb := h.buildRequestsMessage(nil, 1, 1, 0)

	if msg == "" {
		t.Fatal("expected non-empty message")
	}
	if kb == nil {
		t.Fatal("expected non-nil keyboard")
	}
	// 空列表应提示"暂无记录"
	if len(msg) < 10 {
		t.Errorf("message too short for empty state: %q", msg)
	}
}

func TestBuildRequestsMessage_GroupsByStatus(t *testing.T) {
	h := &MyRequestsHandler{}
	items := []services.SubscribeItem{
		{ID: 1, Name: "进行中", State: "S", Type: "movie"},
		{ID: 2, Name: "已完成", State: "C", Type: "movie"},
		{ID: 3, Name: "异常", State: "F", Type: "movie"},
	}
	msg, _ := h.buildRequestsMessage(items, 1, 1, 3)

	// 应该包含三个分组
	if !containsStr(msg, "进行中") {
		t.Error("missing '进行中' group")
	}
	if !containsStr(msg, "已完成") {
		t.Error("missing '已完成' group")
	}
}

// ─── NotifyEnabled 守门测试 ───

func TestNotifyEnabled_DefaultOn(t *testing.T) {
	ps := services.NewPreferencesService(t.TempDir())
	// 没有任何设置，应该全部默认开
	if !ps.IsNotifyEnabled(123, services.NotifyDownload) {
		t.Error("NotifyDownload should default to true")
	}
	if !ps.IsNotifyEnabled(123, services.NotifyRecommend) {
		t.Error("NotifyRecommend should default to true")
	}
	if !ps.IsNotifyEnabled(123, services.NotifyWeekly) {
		t.Error("NotifyWeekly should default to true")
	}
	if !ps.IsNotifyEnabled(123, services.NotifyAnnounce) {
		t.Error("NotifyAnnounce should default to true")
	}
}

func TestNotifyEnabled_ToggleOff(t *testing.T) {
	ps := services.NewPreferencesService(t.TempDir())
	ps.SetNotify(123, services.NotifyDownload, false)
	if ps.IsNotifyEnabled(123, services.NotifyDownload) {
		t.Error("NotifyDownload should be false after SetNotify(false)")
	}
	// 其他类型不受影响
	if !ps.IsNotifyEnabled(123, services.NotifyRecommend) {
		t.Error("NotifyRecommend should still be true")
	}
}

func TestNotifyEnabled_ToggleOnAgain(t *testing.T) {
	ps := services.NewPreferencesService(t.TempDir())
	ps.SetNotify(123, services.NotifyWeekly, false)
	ps.SetNotify(123, services.NotifyWeekly, true)
	if !ps.IsNotifyEnabled(123, services.NotifyWeekly) {
		t.Error("NotifyWeekly should be true after SetNotify(true)")
	}
}

// helper
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstr(s, substr))
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
