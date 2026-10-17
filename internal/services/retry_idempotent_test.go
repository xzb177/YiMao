package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 审计第 11 条：非幂等的 POST 被自动重试——网络抖动/5xx 时服务端可能已经
// 执行了（订阅已创建、用户已注册），重试会把一次操作变成两次。
// 复现：一个永远 500 的上游，POST 必须只打一次；GET 仍可按配置重试。
func TestRetryHTTPDoesNotRetryNonIdempotentMethods(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	newReq := func(method string) *http.Request {
		req, err := http.NewRequest(method, server.URL+"/x", strings.NewReader(`{"a":1}`))
		if err != nil {
			t.Fatal(err)
		}
		return req
	}

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		hits.Store(0)
		resp, err := RetryHTTP(context.Background(), server.Client(), newReq(method), DefaultRetryConfig())
		if resp != nil {
			resp.Body.Close()
		}
		if err == nil {
			t.Fatalf("%s: want error from 500 upstream, got nil", method)
		}
		if got := hits.Load(); got != 1 {
			t.Fatalf("%s: upstream hits=%d, want exactly 1 (no retry for non-idempotent method)", method, got)
		}
	}

	// GET 仍按配置重试（3 次）。
	hits.Store(0)
	resp, err := RetryHTTP(context.Background(), server.Client(), newReq(http.MethodGet), DefaultRetryConfig())
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("GET: want error from 500 upstream, got nil")
	}
	if got := hits.Load(); got != 3 {
		t.Fatalf("GET: upstream hits=%d, want 3 (DefaultRetryConfig MaxAttempts)", got)
	}
}

// 审计第 11 条：triggerSubscriptionSearch 以前是外层 Retry(3) 包着内层
// RetryHTTP(3)，一次触发最多打 9 次；而且每个订阅各触发一次全局搜索，
// 批量审批时直接压垮 MoviePilot 和站点。
func TestTriggerSubscriptionSearchSingleLevelRetry(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/subscribe/search") {
			hits.Add(1)
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := NewMoviePilotClient(server.URL, "testkey", "")
	c.triggerSubscriptionSearch(42)
	if got := hits.Load(); got > 3 {
		t.Fatalf("search endpoint hits=%d, want <=3 (single-level retry, was 9 with nested retry)", got)
	}
	if got := hits.Load(); got != 3 {
		t.Fatalf("search endpoint hits=%d, want 3 (inner RetryHTTP attempts)", got)
	}
}

func TestTriggerSubscriptionSearchDebounced(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/subscribe/search") {
			hits.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	c := NewMoviePilotClient(server.URL, "testkey", "")
	start := time.Now()
	c.triggerSubscriptionSearch(1) // 第一次：实际触发（含 500ms 等待）
	c.triggerSubscriptionSearch(2) // 冷却窗口内：合并，不再打
	c.triggerSubscriptionSearch(3) // 冷却窗口内：合并，不再打
	if got := hits.Load(); got != 1 {
		t.Fatalf("batch of 3 triggers produced %d upstream searches, want 1 (coalesced)", got)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("took %v, debounce path should be fast after the first trigger", elapsed)
	}

	// 非法 subID 直接忽略。
	c.triggerSubscriptionSearch(0)
	if got := hits.Load(); got != 1 {
		t.Fatalf("hits=%d after subID=0, want still 1", got)
	}
}
