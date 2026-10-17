package services

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newEmbyUserIDTestService(t *testing.T, handler http.Handler) *WebhookService {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	return &WebhookService{
		embyURL:    server.URL,
		embyAPIKey: "SECRET-API-KEY",
		embyUserID: "user-1",
		tmdbClient: server.Client(),
	}
}

// 审计第 3 条：Emby API Key 被当成 UserID 拼进 URL 路径。
// 路径里必须是 embyUserID，密钥只能走 X-Emby-Token 头。
func TestGetSeriesInfoUsesUserIDNotAPIKey(t *testing.T) {
	var gotPath string
	var gotTokenHeader string
	svc := newEmbyUserIDTestService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		gotTokenHeader = r.Header.Get("X-Emby-Token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Name":"测试剧","ProductionYear":2024}`))
	}))

	if _, err := svc.getSeriesInfo("series-9"); err != nil {
		t.Fatalf("getSeriesInfo: %v", err)
	}
	if want := "/Users/user-1/Items/series-9"; gotPath != want {
		t.Fatalf("path = %q, want %q", gotPath, want)
	}
	if strings.Contains(gotPath, "SECRET-API-KEY") {
		t.Fatalf("API key leaked into URL path: %q", gotPath)
	}
	if gotTokenHeader != "SECRET-API-KEY" {
		t.Fatalf("X-Emby-Token header = %q, want API key", gotTokenHeader)
	}
}

func TestGetEmbyEnhancedInfoUsesUserIDNotAPIKey(t *testing.T) {
	var gotPath string
	svc := newEmbyUserIDTestService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Name":"测试电影","Type":"Movie","ProductionYear":2024}`))
	}))

	if _, err := svc.getEmbyEnhancedInfo("item-7"); err != nil {
		t.Fatalf("getEmbyEnhancedInfo: %v", err)
	}
	if want := "/Users/user-1/Items/item-7"; gotPath != want {
		t.Fatalf("path = %q, want %q", gotPath, want)
	}
	if strings.Contains(gotPath, "SECRET-API-KEY") {
		t.Fatalf("API key leaked into URL path: %q", gotPath)
	}
}

// 审计第 3 条后半：4xx 是请求本身的问题，不应重试。
// 调用方（webhook.go 的重试循环）用 errors.Is 识别这个信号。
func TestGetEmbyEnhancedInfo4xxIsNoRetry(t *testing.T) {
	svc := newEmbyUserIDTestService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))

	_, err := svc.getEmbyEnhancedInfo("item-7")
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if !errors.Is(err, ErrEmbyClientError) {
		t.Fatalf("4xx error should be ErrEmbyClientError, got %v", err)
	}

	_, err = svc.getSeriesInfo("series-9")
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if !errors.Is(err, ErrEmbyClientError) {
		t.Fatalf("4xx error should be ErrEmbyClientError, got %v", err)
	}
}

// 5xx 仍然是可重试的信号，不能被误判为 ErrEmbyClientError。
func TestGetEmbyEnhancedInfo5xxStillRetryable(t *testing.T) {
	svc := newEmbyUserIDTestService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))

	_, err := svc.getEmbyEnhancedInfo("item-7")
	if err == nil {
		t.Fatal("expected error for 502")
	}
	if errors.Is(err, ErrEmbyClientError) {
		t.Fatalf("5xx must NOT be ErrEmbyClientError, got %v", err)
	}
}
