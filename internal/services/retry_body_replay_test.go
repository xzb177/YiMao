package services

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// 审计第 11 条行为变更：非幂等方法（POST/PUT/PATCH/DELETE）不再自动重试。
// 服务端可能已经执行了请求（订阅已创建、用户已注册），重试会把一次操作
// 变成两次。即使 body 可重放，POST 也只执行一次。
func TestRetryHTTPDoesNotRetryPost(t *testing.T) {
	var attempts atomic.Int32
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(raw))
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	payload := `{"name":"test-title","tmdbid":1433321}`
	req, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewBufferString(payload))
	if err != nil {
		t.Fatal(err)
	}

	cfg := DefaultRetryConfig()
	cfg.BaseDelay = 0
	resp, err := RetryHTTP(context.Background(), server.Client(), req, cfg)
	if err == nil {
		t.Fatal("RetryHTTP must return the 500 error for a non-retried POST")
	}
	if resp != nil {
		resp.Body.Close()
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts=%d, want exactly 1 (no retry for POST)", got)
	}
	if len(bodies) != 1 || bodies[0] != payload {
		t.Fatalf("bodies=%q, want the single original payload", bodies)
	}
}

// unrewindableBody has no GetBody counterpart, so the request may only be sent
// once; a second attempt would send an incomplete payload.
type unrewindableBody struct{ r io.Reader }

func (u *unrewindableBody) Read(p []byte) (int, error) { return u.r.Read(p) }
func (u *unrewindableBody) Close() error               { return nil }

func TestRetryHTTPDoesNotRetryUnrewindableBody(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	const payload = `{"tmdbid":1}`
	req.Body = &unrewindableBody{r: strings.NewReader(payload)}
	req.ContentLength = int64(len(payload))
	req.GetBody = nil

	cfg := DefaultRetryConfig()
	cfg.BaseDelay = 0
	resp, err := RetryHTTP(context.Background(), server.Client(), req, cfg)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("RetryHTTP must return the 500 error for a non-retried POST")
	}
	if !strings.Contains(err.Error(), "no retry for POST") {
		t.Fatalf("error = %v, want the explicit no-retry error", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts=%d, want exactly 1 (no retry)", got)
	}
}

// Bodyless GET requests stay retryable.
func TestRetryHTTPStillRetriesBodylessRequests(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultRetryConfig()
	cfg.BaseDelay = 0
	resp, err := RetryHTTP(context.Background(), server.Client(), req, cfg)
	if err != nil {
		t.Fatalf("RetryHTTP returned error: %v", err)
	}
	defer resp.Body.Close()
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts=%d, want 3", got)
	}
}
