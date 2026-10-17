package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/config"
)

// 审计 A6：同一 Emby webhook body 在 10 分钟内重复投递，第二次必须 409。
func TestEmbyWebhookReplayRejected(t *testing.T) {
	const secret = "replay-test-secret"
	router := NewRouter(&config.Config{WebhookSecret: secret}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	body := `{"Event":"library.new","Item":{"Name":"测试剧集","Type":"Episode"}}`
	sig := "sha256=" + hmacHex(secret, body)

	post := func() int {
		req := httptest.NewRequest(http.MethodPost, "/webhook/emby", strings.NewReader(body))
		req.Header.Set("X-Webhook-Signature", sig)
		rr := httptest.NewRecorder()
		router.HandleWebhook(rr, req)
		return rr.Code
	}

	if code := post(); code != http.StatusAccepted {
		t.Fatalf("第一次投递应 202，实际 %d", code)
	}
	if code := post(); code != http.StatusConflict {
		t.Fatalf("重复投递应 409，实际 %d", code)
	}
	// 不同 body 不受影响。
	other := `{"Event":"library.new","Item":{"Name":"另一部"}}`
	req := httptest.NewRequest(http.MethodPost, "/webhook/emby", strings.NewReader(other))
	req.Header.Set("X-Webhook-Signature", "sha256="+hmacHex(secret, other))
	rr := httptest.NewRecorder()
	router.HandleWebhook(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("不同事件应 202，实际 %d", rr.Code)
	}
}

// 审计 A6：webhookDedup 指纹语义——相同 body 命中、不同 body 不命中。
func TestWebhookDedupMark(t *testing.T) {
	d := &webhookDedup{seen: make(map[string]time.Time)}
	if !d.mark([]byte("a")) {
		t.Fatal("首次应通过")
	}
	if d.mark([]byte("a")) {
		t.Fatal("重复应被拦")
	}
	if !d.mark([]byte("b")) {
		t.Fatal("不同 body 应通过")
	}
}

func hmacHex(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}
