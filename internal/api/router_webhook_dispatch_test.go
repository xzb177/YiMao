package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xzb177/yimao/internal/config"
	"github.com/xzb177/yimao/internal/services"
)

// 审计第 10 条：外部 webhook 以前不看 URL 路径。/webhook/jellyseerr 上没带
// type 的 Jellyseerr 事件掉进 auto-detect，被误判成 Emby（它的 "event" 字段
// 恰好命中 Emby payload 的 Event 备选字段），返回 200 却什么都没做。
func TestResolveWebhookTypePrefersPath(t *testing.T) {
	cases := []struct {
		name, path, query, header, want string
	}{
		{"emby path", "/webhook/emby", "", "", "emby"},
		{"jellyseerr path", "/webhook/jellyseerr", "", "", "jellyseerr"},
		{"moviepilot path", "/webhook/moviepilot", "", "", "moviepilot"},
		{"mp alias path", "/webhook/mp", "", "", "moviepilot"},
		{"trailing slash", "/webhook/jellyseerr/", "", "", "jellyseerr"},
		{"query type", "/webhook", "jellyseerr", "", "jellyseerr"},
		{"header type", "/webhook", "", "moviepilot", "moviepilot"},
		{"path beats query", "/webhook/emby", "jellyseerr", "", "emby"},
		{"unknown path", "/webhook", "", "", ""},
		{"unknown explicit type", "/webhook", "weird", "", "weird"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := tc.path
			if tc.query != "" {
				target += "?type=" + tc.query
			}
			req := httptest.NewRequest(http.MethodPost, target, nil)
			if tc.header != "" {
				req.Header.Set("X-Webhook-Type", tc.header)
			}
			if got := resolveWebhookType(req); got != tc.want {
				t.Fatalf("resolveWebhookType(%q)=%q, want %q", target, got, tc.want)
			}
		})
	}
}

// 回归测试：Jellyseerr 形状的事件必须判成 jellyseerr，不能再误判成 emby。
func TestClassifyAutoDetectWebhook(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{
			"jellyseerr request_created",
			`{"event":"request_created","subject":"有人求片","message":"x","media":{"media_type":"movie"},"user":{"id":7}}`,
			"jellyseerr",
		},
		{
			"jellyseerr test event",
			`{"event":"test","subject":"测试","message":"ok"}`,
			"jellyseerr",
		},
		{
			"emby playback event",
			`{"NotificationType":"playback.start","ItemId":"123","ItemName":"示例电影","ItemType":"Movie"}`,
			"emby",
		},
		{
			"bare event field is unknown, not emby",
			`{"event":"request_created"}`,
			"unknown",
		},
		{
			"garbage is unknown",
			`{"foo":"bar"}`,
			"unknown",
		},
		{
			"invalid json is unknown",
			`not json`,
			"unknown",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyAutoDetectWebhook([]byte(tc.body)); got != tc.want {
				t.Fatalf("classify(%s)=%q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// 无法识别的事件必须返回明确的 400，而不是假 200。
func TestHandleWebhookUnknownTypeReturns400(t *testing.T) {
	cfg := &config.Config{WebhookSecret: "s3cr3t"}
	r := NewRouter(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	body := `{"some":"unrecognized","event":"whatever"}`
	mac := hmac.New(sha256.New, []byte("s3cr3t"))
	mac.Write([]byte(body))
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	req.Header.Set("X-Webhook-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	rr := httptest.NewRecorder()
	r.HandleWebhook(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 for unclassifiable webhook", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Unknown webhook type") {
		t.Fatalf("body=%q, want explicit unknown-type message", rr.Body.String())
	}
	_ = services.EmbyWebhookPayload{}
}
