package miniapp

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultReadRateLimit        = 120
	DefaultWriteRateLimit       = 20
	DefaultAuthFailureRateLimit = 30
	DefaultRateWindow           = time.Minute
	DefaultRateBucketTTL        = 10 * time.Minute
	DefaultRateMaxBuckets       = 10000
)

type RateLimitConfig struct {
	ReadLimit        int
	WriteLimit       int
	AuthFailureLimit int
	Window           time.Duration
	BucketTTL        time.Duration
	MaxBuckets       int
	Now              func() time.Time
}

type rateClass uint8

const (
	rateNone rateClass = iota
	rateRead
	rateWrite
	rateAuthFailure
)

type rateBucketKey struct {
	subject string
	class   rateClass
}

type rateBucket struct {
	windowStart time.Time
	lastSeen    time.Time
	count       int
}

type userRateLimiter struct {
	mu               sync.Mutex
	readLimit        int
	writeLimit       int
	authFailureLimit int
	window           time.Duration
	bucketTTL        time.Duration
	maxBuckets       int
	now              func() time.Time
	buckets          map[rateBucketKey]rateBucket
	lastCleanup      time.Time
}

func newUserRateLimiter(config RateLimitConfig) *userRateLimiter {
	if config.ReadLimit <= 0 {
		config.ReadLimit = DefaultReadRateLimit
	}
	if config.WriteLimit <= 0 {
		config.WriteLimit = DefaultWriteRateLimit
	}
	if config.AuthFailureLimit <= 0 {
		config.AuthFailureLimit = DefaultAuthFailureRateLimit
	}
	if config.Window <= 0 {
		config.Window = DefaultRateWindow
	}
	if config.BucketTTL <= 0 {
		config.BucketTTL = DefaultRateBucketTTL
	}
	if config.MaxBuckets <= 0 {
		config.MaxBuckets = DefaultRateMaxBuckets
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &userRateLimiter{
		readLimit: config.ReadLimit, writeLimit: config.WriteLimit,
		authFailureLimit: config.AuthFailureLimit, window: config.Window,
		bucketTTL: config.BucketTTL, maxBuckets: config.MaxBuckets, now: config.Now,
		buckets: make(map[rateBucketKey]rateBucket),
	}
}

func (l *userRateLimiter) allow(userID int64, class rateClass) (bool, time.Duration) {
	return l.allowSubject("user:"+strconv.FormatInt(userID, 10), class)
}

func (l *userRateLimiter) allowAuthFailure(r *http.Request) (bool, time.Duration) {
	return l.allowSubject(authFailureSubject(r), rateAuthFailure)
}

func (l *userRateLimiter) allowSubject(subject string, class rateClass) (bool, time.Duration) {
	if class == rateNone {
		return true, 0
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lastCleanup.IsZero() || now.Sub(l.lastCleanup) >= l.bucketTTL || len(l.buckets) >= l.maxBuckets {
		l.cleanup(now)
	}
	key := rateBucketKey{subject: subject, class: class}
	bucket, exists := l.buckets[key]
	if !exists && len(l.buckets) >= l.maxBuckets {
		l.evictOldest()
	}
	if !exists || now.Sub(bucket.windowStart) >= l.window || now.Before(bucket.windowStart) {
		bucket = rateBucket{windowStart: now}
	}
	bucket.lastSeen = now
	limit := l.readLimit
	switch class {
	case rateWrite:
		limit = l.writeLimit
	case rateAuthFailure:
		limit = l.authFailureLimit
	}
	if bucket.count >= limit {
		l.buckets[key] = bucket
		retry := l.window - now.Sub(bucket.windowStart)
		if retry <= 0 {
			retry = time.Second
		}
		return false, retry
	}
	bucket.count++
	l.buckets[key] = bucket
	return true, 0
}

func authFailureSubject(r *http.Request) string {
	if r == nil {
		return "peer:unknown"
	}
	remote := strings.TrimSpace(r.RemoteAddr)
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	if ip := net.ParseIP(strings.TrimSpace(host)); ip != nil {
		host = ip.String()
	}
	if host == "" {
		host = "unknown"
	}
	return "peer:" + host
}

func (l *userRateLimiter) cleanup(now time.Time) {
	for key, bucket := range l.buckets {
		if now.Sub(bucket.lastSeen) >= l.bucketTTL || now.Before(bucket.lastSeen) {
			delete(l.buckets, key)
		}
	}
	l.lastCleanup = now
}

func (l *userRateLimiter) evictOldest() {
	var oldestKey rateBucketKey
	var oldest time.Time
	found := false
	for key, bucket := range l.buckets {
		if !found || bucket.lastSeen.Before(oldest) {
			oldestKey, oldest, found = key, bucket.lastSeen, true
		}
	}
	if found {
		delete(l.buckets, oldestKey)
	}
}

func miniAppRateClass(r *http.Request) rateClass {
	if r == nil || len(r.URL.Path) < len("/api/miniapp/v1/") || r.URL.Path[:len("/api/miniapp/v1/")] != "/api/miniapp/v1/" {
		return rateNone
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		return rateRead
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return rateWrite
	default:
		return rateNone
	}
}

func writeRateLimit(w http.ResponseWriter, retry time.Duration) {
	seconds := int((retry + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{
		"ok": false, "error": "rate_limited", "message": "请求过于频繁，请稍后再试",
	})
}
