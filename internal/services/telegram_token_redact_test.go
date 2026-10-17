package services

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// 审计第 1 条后半：TelegramClient 返回错误前先剥掉 URL。
// http.Client 超时返回 *url.Error，Error() 里带完整请求 URL（含 bot token）。
func TestStripTokenFromError(t *testing.T) {
	token := "123456789:AAECa1b2C3d4E5f6G7h8I9j0K1l2M3n4O5p6"
	raw := &url.Error{
		Op:  "Get",
		URL: "https://api.telegram.org/bot" + token + "/getUpdates?offset=0&limit=100&timeout=10",
		Err: &net.DNSError{Err: "i/o timeout", Name: "api.telegram.org", IsTimeout: true},
	}

	got := stripTokenFromError(raw)
	if got == nil {
		t.Fatal("expected non-nil error")
	}
	if strings.Contains(got.Error(), token) || strings.Contains(got.Error(), "AAECa1b2") {
		t.Fatalf("bot token leaked in returned error: %q", got.Error())
	}
	// 原始超时信息应保留
	if !strings.Contains(got.Error(), "timeout") {
		t.Fatalf("underlying error info lost: %q", got.Error())
	}
}

// 无 token 的错误原样返回（不破坏错误链/内容）。
func TestStripTokenFromErrorPassthrough(t *testing.T) {
	raw := &url.Error{Op: "Get", URL: "https://example.com/x", Err: context.DeadlineExceeded}
	got := stripTokenFromError(raw)
	if got != raw {
		t.Fatalf("expected passthrough, got %v", got)
	}
	if stripTokenFromError(nil) != nil {
		t.Fatal("nil should stay nil")
	}
}

// 端到端：GetUpdates 超时返回的 error 里不能带 token。
func TestGetUpdatesErrorHasNoToken(t *testing.T) {
	token := "999999:ZZZZy1x2W3v4U5t6S7r8Q9p0O1n2M3l4K5j6"
	c := NewTelegramClient(token)
	c.SetBaseURLForTest("http://127.0.0.1:1", &http.Client{Timeout: 50}) // 必然连接失败/超时

	_, err := c.GetUpdates(0, 100)
	if err == nil {
		t.Skip("expected a network error in this environment")
	}
	if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "ZZZZy1x2") {
		t.Fatalf("bot token leaked from GetUpdates error: %q", err.Error())
	}
}
