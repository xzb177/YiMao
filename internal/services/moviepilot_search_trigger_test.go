package services

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// 审计 P1-2：合并窗口内的订阅搜索触发不许再被静默吞掉——窗口结束时
// 必须补发一次全局搜索；补发失败要有限退避重试，最终失败要停下来
// （不再无限打），而不是悄悄消失或无限循环。

type searchTriggerHarness struct {
	client      *MoviePilotClient
	hits        atomic.Int32 // 全部 /subscribe/search 请求数
	successHits atomic.Int32 // 返回 200 的请求数
	failAll     atomic.Bool
}

func newSearchTriggerHarness(t *testing.T, cooldown time.Duration, backoffs []time.Duration) *searchTriggerHarness {
	t.Helper()
	h := &searchTriggerHarness{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/subscribe/search" {
			http.NotFound(w, r)
			return
		}
		h.hits.Add(1)
		if h.failAll.Load() {
			http.Error(w, "mp unavailable", http.StatusInternalServerError)
			return
		}
		h.successHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"success":true}`)
	}))
	t.Cleanup(server.Close)
	h.client = NewMoviePilotClient(server.URL, "test-key", "")
	// 合并窗口与退避间隔都在构造时注入 client 字段，触发之后不再改动
	// 任何状态——旧写法运行中改写包级 var 会与仍在飞的补发定时器相撞。
	h.client.searchTriggerCooldown = cooldown
	h.client.searchTriggerBackoffs = backoffs
	return h
}

func waitForHits(t *testing.T, counter *atomic.Int32, want int32, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if counter.Load() >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d hits, got %d", want, counter.Load())
}

func TestTriggerSubscriptionSearchCoalescedFlush(t *testing.T) {
	// 窗口取 2s：首次触发内部有 500ms 落库等待，窗口必须明显大于它，
	// 模拟生产 30s 窗口内秒级连点批准的情形。
	h := newSearchTriggerHarness(t, 2*time.Second, nil)

	// 第一次触发：不在窗口内，立即发出一次全局搜索。
	h.client.triggerSubscriptionSearch(101)
	if got := h.hits.Load(); got != 1 {
		t.Fatalf("first trigger hits = %d, want 1", got)
	}

	// 窗口内的两次触发：只记账，不立即发，也不丢弃。
	h.client.triggerSubscriptionSearch(102)
	h.client.triggerSubscriptionSearch(103)
	if got := h.hits.Load(); got != 1 {
		t.Fatalf("coalesced triggers fired early, hits = %d, want 1", got)
	}

	// 窗口结束（2s）+ 补发内部等待（500ms）后，恰好补发一次。
	waitForHits(t, &h.hits, 2, 5*time.Second)
	time.Sleep(300 * time.Millisecond)
	if got := h.hits.Load(); got != 2 {
		t.Fatalf("flush hits = %d, want exactly 2 (one coalesced flush)", got)
	}

	h.client.searchTriggerMu.Lock()
	pending, timer := h.client.searchTriggerPending, h.client.searchTriggerTimer
	h.client.searchTriggerMu.Unlock()
	if pending || timer != nil {
		t.Fatalf("flush left pending=%v timer=%v, want cleared", pending, timer != nil)
	}
}

func TestFlushSubscriptionSearchRetriesAndRecovers(t *testing.T) {
	h := newSearchTriggerHarness(t, 150*time.Millisecond, []time.Duration{600 * time.Millisecond, 600 * time.Millisecond})
	h.failAll.Store(true)
	// 模拟窗口刚被上一次触发打开：本次触发只能走合并补发路径。
	h.client.lastSearchTriggerTime = time.Now()
	h.client.triggerSubscriptionSearch(202)

	// 补发首轮（含内部重试）失败后，在退避重试期间恢复 MP。
	time.AfterFunc(1200*time.Millisecond, func() { h.failAll.Store(false) })

	waitForHits(t, &h.successHits, 1, 6*time.Second)
	h.client.searchTriggerMu.Lock()
	pending := h.client.searchTriggerPending
	h.client.searchTriggerMu.Unlock()
	if pending {
		t.Fatal("pending flag still set after successful flush")
	}
}

func TestFlushSubscriptionSearchExhaustsRetriesAndStops(t *testing.T) {
	h := newSearchTriggerHarness(t, 100*time.Millisecond, []time.Duration{10 * time.Millisecond, 10 * time.Millisecond})
	h.failAll.Store(true)
	h.client.lastSearchTriggerTime = time.Now()
	h.client.triggerSubscriptionSearch(303)

	// 补发 = 1 次首发 + 2 次退避重试；每次 makeRequest 对 GET 内部重试
	// 3 次（DefaultRetryConfig），共 9 个 HTTP 请求，之后必须停。
	waitForHits(t, &h.hits, 9, 8*time.Second)
	time.Sleep(400 * time.Millisecond)
	if got := h.hits.Load(); got != 9 {
		t.Fatalf("hits = %d, want 9 (flush gave up after bounded retries)", got)
	}
	h.client.searchTriggerMu.Lock()
	pending, timer := h.client.searchTriggerPending, h.client.searchTriggerTimer
	h.client.searchTriggerMu.Unlock()
	if pending || timer != nil {
		t.Fatalf("exhausted flush left pending=%v timer=%v, want cleared", pending, timer != nil)
	}
}

func TestTriggerSubscriptionSearchIgnoresInvalidID(t *testing.T) {
	h := newSearchTriggerHarness(t, 100*time.Millisecond, nil)
	h.client.triggerSubscriptionSearch(0)
	h.client.triggerSubscriptionSearch(-5)
	time.Sleep(300 * time.Millisecond)
	if got := h.hits.Load(); got != 0 {
		t.Fatalf("invalid subID fired %d searches, want 0", got)
	}
}

func TestRequestMediaTriggersSubscriptionSearch(t *testing.T) {
	var searchHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/subscribe":
			fmt.Fprint(w, `{"success":true,"message":"","data":{"id":77}}`)
		case "/api/v1/subscribe/search":
			searchHits.Add(1)
			fmt.Fprint(w, `{"success":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewMoviePilotClient(server.URL, "test-key", "")
	req, err := client.RequestMedia("测试片", 2026, 999, MediaTypeMovie, 0)
	if err != nil {
		t.Fatalf("RequestMedia: %v", err)
	}
	if req.ID != 77 {
		t.Fatalf("request ID = %d, want 77", req.ID)
	}
	// RequestMedia 以 goroutine 触发搜索（内部先等 500ms 确保订阅落库）。
	waitForHits(t, &searchHits, 1, 3*time.Second)
}
