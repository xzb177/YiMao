package services

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 审计第 14 条(a)：FOUND 通知失败重试接线。
// 通知临时失败（500）后条目必须保留 FOUND（不标终态），updated_at 被刷新做节流；
// retryStaleFoundNotifications 能把 updated_at 超重试间隔的 FOUND 条目捡回来再试。
func TestRetryStaleFoundNotifications(t *testing.T) {
	ws := newTestWishService(t)
	wish := makeFoundItem(t, ws, 1, 730)
	backdateUpdatedAt(t, ws, wish.ID, 2*time.Hour)

	// Telegram 桩：全部 500，模拟网络抖动（临时错误，可重试）。
	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"ok":false,"error_code":500,"description":"Internal Server Error"}`)
	}))
	defer tgServer.Close()
	telegram := NewTelegramClient("test")
	telegram.SetBaseURLForTest(tgServer.URL, tgServer.Client())

	s := NewWishScheduler(ws, nil, telegram, 0, 24, 30, 1, 60)
	s.SetFoundNotifyRetryInterval(30 * time.Minute)

	// 条目已 stale（updated_at 2 小时前）：重试扫瞄应捡回它。
	stale, err := ws.ListStaleFoundItems(30 * time.Minute)
	if err != nil || len(stale) != 1 || stale[0].ID != wish.ID {
		t.Fatalf("precondition: stale FOUND item not listed: %v %v", stale, err)
	}

	before := time.Now()
	s.retryStaleFoundNotifications()

	// 通知失败（500 临时错误）：条目保留 FOUND，不进终态。
	got, err := ws.GetByID(wish.ID)
	if err != nil || got == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.State != WishStateFound {
		t.Fatalf("transient notify failure should keep FOUND, got %s", got.State)
	}
	// updated_at 被刷新（节流戳）：重试不会立刻再打。
	if got.UpdatedAt.Before(before) {
		t.Fatalf("updated_at should be touched after failed notify attempt, got %v", got.UpdatedAt)
	}
	staleAfter, err := ws.ListStaleFoundItems(30 * time.Minute)
	if err != nil {
		t.Fatalf("ListStaleFoundItems: %v", err)
	}
	if len(staleAfter) != 0 {
		t.Fatalf("item should be throttled right after a failed attempt, still stale: %d", len(staleAfter))
	}
}
