package services

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// 审计 A4：doWithRetry 必须读响应体里的 parameters.retry_after（Telegram 官方机制）。

func TestParseTelegramRetryAfter(t *testing.T) {
	cases := []struct {
		body string
		want int
	}{
		{`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":7}}`, 7},
		{`{"ok":false,"error_code":429}`, 0},
		{`not json`, 0},
		{`{"parameters":{"retry_after":0}}`, 0},
	}
	for _, c := range cases {
		if got := parseTelegramRetryAfter([]byte(c.body)); got != c.want {
			t.Errorf("body=%q: got %d want %d", c.body, got, c.want)
		}
	}
}

// 审计 A4：发图路径（multipart）必须走 429 重试——第一次 429（体里带 retry_after），第二次成功。
func TestSendPhotoBytesRetriesOn429(t *testing.T) {
	var calls int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"ok":false,"error_code":429,"parameters":{"retry_after":0}}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":1,"chat":{"id":1,"type":"private"},"date":1}}`)
	}))
	defer server.Close()

	client := NewTelegramClient("test")
	client.SetBaseURLForTest(server.URL, server.Client())

	_, err := client.SendPhotoBytes(123, "photo.jpg", []byte("fake"), "cap", nil)
	if err != nil {
		t.Fatalf("429 后应重试成功，实际 err=%v", err)
	}
	if got := atomic.LoadInt64(&calls); got != 2 {
		t.Fatalf("期望打 2 次上游，实际 %d 次", got)
	}
}

// doWithRetry 在多次 429 后仍成功，且等待语义由 retry_after 驱动（retry_after=0 时回退 header/默认）。
func TestDoWithRetryHonorsBodyRetryAfter(t *testing.T) {
	var calls int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"ok":false,"error_code":429,"parameters":{"retry_after":0}}`)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	client := NewTelegramClient("test")
	client.SetBaseURLForTest(server.URL, server.Client())
	req, _ := http.NewRequest("POST", server.URL+"/sendMessage", nil)
	resp, err := client.doWithRetry(req)
	if err != nil {
		t.Fatalf("应重试成功，err=%v", err)
	}
	resp.Body.Close()
	if got := atomic.LoadInt64(&calls); got != 3 {
		t.Fatalf("期望 3 次调用，实际 %d", got)
	}
}
