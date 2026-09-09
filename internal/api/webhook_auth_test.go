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
)

func webhookSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyWebhookAuthHeaderOnly(t *testing.T) {
	body := []byte(`{"event":"test"}`)
	const secret = "webhook-secret"
	cases := []struct {
		name, target, signature, token string
		want                           bool
	}{
		{"HMAC", "/webhook/emby", webhookSignature(body, secret), "", true},
		{"migration header", "/webhook/emby", "", secret, true},
		{"query rejected", "/webhook/emby?token=" + secret, "", "", false},
		{"wrong signature", "/webhook/emby", "sha256=bad", "", false},
		{"wrong header", "/webhook/emby", "", "wrong", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.target, strings.NewReader(string(body)))
			req.Header.Set("X-Webhook-Signature", tc.signature)
			req.Header.Set("X-Webhook-Token", tc.token)
			if got := verifyWebhookAuth(req, body, secret); got != tc.want {
				t.Fatalf("verifyWebhookAuth()=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestHandleWebhookAuthenticationBoundary(t *testing.T) {
	const secret = "webhook-secret"
	body := []byte(`{}`)
	router := &Router{cfg: &config.Config{WebhookSecret: secret}}
	cases := []struct {
		name, target, signature, token string
		wantUnauthorized               bool
	}{
		{"HMAC passes auth", "/webhook/emby", webhookSignature(body, secret), "", false},
		{"header passes auth", "/webhook/emby", "", secret, false},
		{"query rejected", "/webhook/emby?token=" + secret, "", "", true},
		{"wrong rejected", "/webhook/emby", "", "wrong", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.target, strings.NewReader(string(body)))
			req.Header.Set("X-Webhook-Signature", tc.signature)
			req.Header.Set("X-Webhook-Token", tc.token)
			rr := httptest.NewRecorder()
			router.HandleWebhook(rr, req)
			if got := rr.Code == http.StatusUnauthorized; got != tc.wantUnauthorized {
				t.Fatalf("status=%d unauthorized=%v want=%v", rr.Code, got, tc.wantUnauthorized)
			}
		})
	}
}
