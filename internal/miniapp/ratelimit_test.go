package miniapp

import (
	"encoding/json"
	"mime"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testLimiter(now *time.Time, readLimit, writeLimit int) *userRateLimiter {
	return newUserRateLimiter(RateLimitConfig{
		ReadLimit: readLimit, WriteLimit: writeLimit, Window: time.Minute,
		BucketTTL: 2 * time.Minute, MaxBuckets: 100, Now: func() time.Time { return *now },
	})
}

func TestUserRateLimiterBucketsAndWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	limiter := testLimiter(&now, 2, 1)
	if ok, _ := limiter.allow(101, rateRead); !ok {
		t.Fatal("first read rejected")
	}
	if ok, _ := limiter.allow(101, rateRead); !ok {
		t.Fatal("second read rejected")
	}
	if ok, retry := limiter.allow(101, rateRead); ok || retry != time.Minute {
		t.Fatalf("read threshold: ok=%v retry=%v", ok, retry)
	}
	if ok, _ := limiter.allow(101, rateWrite); !ok {
		t.Fatal("write must use a separate bucket")
	}
	if ok, _ := limiter.allow(101, rateWrite); ok {
		t.Fatal("write threshold exceeded")
	}
	if ok, _ := limiter.allow(202, rateRead); !ok {
		t.Fatal("another user was affected")
	}
	now = now.Add(time.Minute)
	if ok, _ := limiter.allow(101, rateRead); !ok {
		t.Fatal("read window did not reset")
	}
	if ok, _ := limiter.allow(101, rateWrite); !ok {
		t.Fatal("write window did not reset")
	}
}

func TestUserRateLimiterConcurrentDoesNotOverIssue(t *testing.T) {
	now := time.Unix(2000, 0)
	limiter := testLimiter(&now, 25, 1)
	var admitted int32
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := limiter.allow(101, rateRead); ok {
				atomic.AddInt32(&admitted, 1)
			}
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&admitted); got != 25 {
		t.Fatalf("admitted %d requests, want 25", got)
	}
}

func TestUserRateLimiterHasBoundedBucketsAndTTL(t *testing.T) {
	now := time.Unix(3000, 0)
	limiter := newUserRateLimiter(RateLimitConfig{
		ReadLimit: 1, WriteLimit: 1, Window: time.Minute, BucketTTL: time.Minute,
		MaxBuckets: 3, Now: func() time.Time { return now },
	})
	for userID := int64(1); userID <= 10; userID++ {
		limiter.allow(userID, rateRead)
	}
	if len(limiter.buckets) > 3 {
		t.Fatalf("bucket count %d exceeds cap", len(limiter.buckets))
	}
	now = now.Add(2 * time.Minute)
	limiter.allow(99, rateRead)
	if len(limiter.buckets) != 1 {
		t.Fatalf("expired buckets retained: %d", len(limiter.buckets))
	}
}

func TestMiniAppRateLimitHTTPResponse(t *testing.T) {
	now := time.Now()
	server := NewServer(Deps{BotToken: miniAppTestToken, RateLimit: RateLimitConfig{
		ReadLimit: 1, WriteLimit: 1, Window: 30 * time.Second,
		BucketTTL: time.Minute, MaxBuckets: 10, Now: func() time.Time { return now },
	}})
	handler := server.Handler()
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, signedRequest(t, http.MethodGet, "/api/miniapp/v1/search?q=test", "", 101))
	limited := httptest.NewRecorder()
	handler.ServeHTTP(limited, signedRequest(t, http.MethodGet, "/api/miniapp/v1/detail?id=1&type=movie", "", 101))
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", limited.Code, limited.Body.String())
	}
	if got := limited.Header().Get("Retry-After"); got != strconv.Itoa(30) {
		t.Fatalf("Retry-After=%q", got)
	}
	mediaType, _, err := mime.ParseMediaType(limited.Header().Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		t.Fatalf("Content-Type=%q err=%v", limited.Header().Get("Content-Type"), err)
	}
	var body struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(limited.Body.Bytes(), &body); err != nil || body.OK || body.Error != "rate_limited" {
		t.Fatalf("invalid body %q: %+v, %v", limited.Body.String(), body, err)
	}
	write := httptest.NewRecorder()
	handler.ServeHTTP(write, signedRequest(t, http.MethodPost, "/api/miniapp/v1/watchlist", `{}`, 101))
	if write.Code == http.StatusTooManyRequests {
		t.Fatal("read traffic consumed write quota")
	}
	other := httptest.NewRecorder()
	handler.ServeHTTP(other, signedRequest(t, http.MethodGet, "/api/miniapp/v1/search?q=test", "", 202))
	if other.Code == http.StatusTooManyRequests {
		t.Fatal("one user consumed another user's quota")
	}
}

func TestMiniAppAuthFailuresAreLimitedByPeerWithoutBlockingValidUsers(t *testing.T) {
	now := time.Now()
	server := NewServer(Deps{BotToken: miniAppTestToken, RateLimit: RateLimitConfig{
		ReadLimit: 10, WriteLimit: 10, AuthFailureLimit: 1, Window: 30 * time.Second,
		BucketTTL: time.Minute, MaxBuckets: 10, Now: func() time.Time { return now },
	}})
	handler := server.Handler()

	invalid := func(remote string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/miniapp/v1/me", nil)
		req.RemoteAddr = remote
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr
	}
	if got := invalid("203.0.113.7:1001").Code; got != http.StatusUnauthorized {
		t.Fatalf("first invalid request status=%d", got)
	}
	limited := invalid("203.0.113.7:2002")
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") != "30" {
		t.Fatalf("limited status=%d retry=%q", limited.Code, limited.Header().Get("Retry-After"))
	}
	if got := invalid("203.0.113.8:1001").Code; got != http.StatusUnauthorized {
		t.Fatalf("another peer was affected: status=%d", got)
	}

	valid := signedRequest(t, http.MethodGet, "/api/miniapp/v1/me", "", 101)
	valid.RemoteAddr = "203.0.113.7:3003"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, valid)
	if response.Code == http.StatusTooManyRequests || response.Code == http.StatusUnauthorized {
		t.Fatalf("valid user was blocked by unauthenticated peer bucket: status=%d", response.Code)
	}
}

func TestAuthFailureSubjectCanonicalizesPeerAddress(t *testing.T) {
	cases := map[string]string{
		"203.0.113.7:1234":   "peer:203.0.113.7",
		"[2001:db8::7]:4321": "peer:2001:db8::7",
		"2001:db8::8":        "peer:2001:db8::8",
		"":                   "peer:unknown",
	}
	for remote, want := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = remote
		if got := authFailureSubject(req); got != want {
			t.Errorf("remote=%q subject=%q want=%q", remote, got, want)
		}
	}
}
