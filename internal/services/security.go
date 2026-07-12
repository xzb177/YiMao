package services

import (
	"crypto/subtle"
	"github.com/xzb177/yimao/pkg/logger"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Default Security Config
const (
	defaultRateLimitRequests = 60               // requests per minute
	defaultRateLimitWindow   = time.Minute      // sliding window
	defaultRateLimitCleanup  = 5 * time.Minute  // cleanup interval
	defaultMaxFailedAttempts = 5                // failed attempts before block
	defaultBlockDuration     = 30 * time.Minute // how long to block
)

// APIKeyConfig holds API key configuration
type APIKeyConfig struct {
	Keys    map[string]string // key -> description
	Enabled bool
	mu      sync.RWMutex
}

// RateLimitEntry tracks requests for rate limiting
type RateLimitEntry struct {
	Count     int
	LastSeen  time.Time
	FirstSeen time.Time
}

// SecurityContext holds security state
type SecurityContext struct {
	apiKeys        APIKeyConfig
	rateLimits     map[string]*RateLimitEntry // IP -> entry
	failedAttempts map[string]int             // IP -> count
	blockedIPs     map[string]time.Time       // IP -> unblock time
	mu             sync.RWMutex
	cleanupDone    chan bool

	// Configurable limits
	rateLimitRequests int
	rateLimitWindow   time.Duration
	maxFailedAttempts int
	blockDuration     time.Duration
}

// SecurityService manages API security
type SecurityService struct {
	ctx *SecurityContext
}

// NewSecurityService creates a new security service
func NewSecurityService() *SecurityService {
	return &SecurityService{
		ctx: &SecurityContext{
			apiKeys: APIKeyConfig{
				Keys:    make(map[string]string),
				Enabled: false,
			},
			rateLimits:        make(map[string]*RateLimitEntry),
			failedAttempts:    make(map[string]int),
			blockedIPs:        make(map[string]time.Time),
			cleanupDone:       make(chan bool),
			rateLimitRequests: defaultRateLimitRequests,
			rateLimitWindow:   defaultRateLimitWindow,
			maxFailedAttempts: defaultMaxFailedAttempts,
			blockDuration:     defaultBlockDuration,
		},
	}
}

// SetConfig sets configurable security limits
func (s *SecurityService) SetConfig(requests int, windowSecs int, maxAttempts int, blockMins int) {
	s.ctx.mu.Lock()
	defer s.ctx.mu.Unlock()

	s.ctx.rateLimitRequests = requests
	if requests > 0 {
		s.ctx.rateLimitWindow = time.Duration(windowSecs) * time.Second
	}
	s.ctx.maxFailedAttempts = maxAttempts
	s.ctx.blockDuration = time.Duration(blockMins) * time.Minute

	logger.Info("[Security] Config updated: requests=%d/%ds, maxAttempts=%d, block=%dm",
		requests, windowSecs, maxAttempts, blockMins)
}

// Start initializes the security service
func (s *SecurityService) Start() {
	go s.ctx.cleanupLoop()
	logger.Info("[Security] Service initialized")
}

// Stop stops the security service
func (s *SecurityService) Stop() {
	close(s.ctx.cleanupDone)
	logger.Info("[Security] Service stopped")
}

// SetAPIKeys sets the API keys for authentication
func (s *SecurityService) SetAPIKeys(keys map[string]string) {
	s.ctx.apiKeys.mu.Lock()
	defer s.ctx.apiKeys.mu.Unlock()
	s.ctx.apiKeys.Keys = keys
	logger.Info("[Security] API keys configured: %d keys", len(keys))
}

// EnableAPIAuth enables/disables API key authentication
func (s *SecurityService) EnableAPIAuth(enabled bool) {
	s.ctx.apiKeys.mu.Lock()
	defer s.ctx.apiKeys.mu.Unlock()
	s.ctx.apiKeys.Enabled = enabled
	logger.Info("[Security] API auth %v", map[bool]string{true: "enabled", false: "disabled"}[enabled])
}

// getClientIP extracts the real client IP from request
func (s *SecurityService) getClientIP(r *http.Request) string {
	// Only trust proxy headers when request comes from a trusted proxy
	remoteIP := r.RemoteAddr
	if idx := strings.LastIndex(remoteIP, ":"); idx != -1 {
		remoteIP = remoteIP[:idx]
	}

	if s.isTrustedProxy(remoteIP) {
		// Request is from a trusted proxy, honor X-Forwarded-For / X-Real-IP
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			ips := strings.Split(xff, ",")
			if len(ips) > 0 {
				ip := strings.TrimSpace(ips[0])
				if ip != "" {
					return ip
				}
			}
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return xri
		}
	}

	// Direct connection or untrusted proxy — use RemoteAddr
	return remoteIP
}

// isTrustedProxy checks if an IP is a trusted proxy
// Uses strict allowlist to prevent IP spoofing
func (s *SecurityService) isTrustedProxy(ip string) bool {
	// Only trust localhost and exact local loopback
	trusted := []string{"127.0.0.1", "::1", "localhost"}
	for _, t := range trusted {
		if ip == t {
			return true
		}
	}
	return false
}

// isIPBlocked checks if an IP is currently blocked
func (s *SecurityService) IsIPBlocked(ip string) bool {
	s.ctx.mu.RLock()
	blocked, exists := s.ctx.blockedIPs[ip]
	s.ctx.mu.RUnlock()

	if exists {
		if time.Now().Before(blocked) {
			return true
		}
		// Block expired, remove it (need write lock)
		s.ctx.mu.Lock()
		delete(s.ctx.blockedIPs, ip)
		s.ctx.mu.Unlock()
	}
	return false
}

// recordFailedAttempt records a failed authentication attempt
func (s *SecurityService) RecordFailedAttempt(ip string) {
	s.ctx.mu.Lock()
	defer s.ctx.mu.Unlock()

	s.ctx.failedAttempts[ip]++
	if s.ctx.failedAttempts[ip] >= s.ctx.maxFailedAttempts {
		// Block this IP
		s.ctx.blockedIPs[ip] = time.Now().Add(s.ctx.blockDuration)
		logger.Info("[Security] IP %s blocked after %d failed attempts", ip, s.ctx.failedAttempts[ip])
		// Reset counter
		delete(s.ctx.failedAttempts, ip)
	}
}

// clearFailedAttempts clears failed attempts after successful auth
func (s *SecurityService) clearFailedAttempts(ip string) {
	s.ctx.mu.Lock()
	defer s.ctx.mu.Unlock()
	delete(s.ctx.failedAttempts, ip)
}

// checkRateLimit checks if a request should be rate limited
func (s *SecurityService) CheckRateLimit(ip string) bool {
	s.ctx.mu.Lock()
	defer s.ctx.mu.Unlock()

	now := time.Now()
	entry, exists := s.ctx.rateLimits[ip]

	if !exists {
		s.ctx.rateLimits[ip] = &RateLimitEntry{
			Count:     1,
			FirstSeen: now,
			LastSeen:  now,
		}
		return false
	}

	// Reset if window expired
	if now.Sub(entry.FirstSeen) >= s.ctx.rateLimitWindow {
		entry.Count = 1
		entry.FirstSeen = now
		entry.LastSeen = now
		return false
	}

	// Check limit
	if entry.Count >= s.ctx.rateLimitRequests {
		entry.LastSeen = now
		return true
	}

	entry.Count++
	entry.LastSeen = now
	return false
}

// cleanupLoop periodically cleans up old rate limit entries
func (s *SecurityContext) cleanupLoop() {
	ticker := time.NewTicker(defaultRateLimitCleanup)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Info("[Security] Panic in cleanup: %v", r)
					}
				}()
				s.cleanup()
			}()
		case <-s.cleanupDone:
			return
		}
	}
}

// cleanup removes stale entries
func (s *SecurityContext) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	window := s.rateLimitWindow
	if window == 0 {
		window = defaultRateLimitWindow
	}
	for ip, entry := range s.rateLimits {
		if now.Sub(entry.LastSeen) > window*2 {
			delete(s.rateLimits, ip)
		}
	}

	// Clean up expired blocks
	for ip, unblockTime := range s.blockedIPs {
		if now.After(unblockTime) {
			delete(s.blockedIPs, ip)
		}
	}
}

// validateAPIKey validates an API key using constant-time comparison
func (s *SecurityService) validateAPIKey(key string) bool {
	s.ctx.apiKeys.mu.RLock()
	defer s.ctx.apiKeys.mu.RUnlock()

	if !s.ctx.apiKeys.Enabled {
		return true // Authentication explicitly disabled
	}
	if len(s.ctx.apiKeys.Keys) == 0 {
		return false // enabled without keys must fail closed
	}

	// Use a constant-time approach: always compare against all keys
	keyMatch := false
	for validKey := range s.ctx.apiKeys.Keys {
		if subtle.ConstantTimeCompare([]byte(key), []byte(validKey)) == 1 {
			keyMatch = true
		}
	}
	return keyMatch
}

// Middleware returns an HTTP middleware with security checks
func (s *SecurityService) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := s.getClientIP(r)

		// Check if IP is blocked
		if s.IsIPBlocked(ip) {
			logger.Info("[Security] Blocked request from %s", ip)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"too_many_requests","message":"IP address temporarily blocked due to repeated failed attempts"}`))
			return
		}

		// Check API key authentication (only from header, not query string for security)
		apiKey := r.Header.Get("X-API-Key")

		if !s.validateAPIKey(apiKey) {
			logger.Info("[Security] Invalid API key from %s", ip)
			s.RecordFailedAttempt(ip)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized","message":"Invalid or missing API key"}`))
			return
		}

		// Clear failed attempts on successful auth
		s.clearFailedAttempts(ip)

		// Check rate limit
		if s.CheckRateLimit(ip) {
			logger.Info("[Security] Rate limit exceeded for %s", ip)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate_limit_exceeded","message":"Too many requests. Please try again later."}`))
			return
		}

		// Add security headers
		s.addSecurityHeaders(w)

		next(w, r)
	}
}

// PublicMiddleware returns middleware for public endpoints (no API key required)
func (s *SecurityService) PublicMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := s.getClientIP(r)

		// Check if IP is blocked (even for public endpoints)
		if s.IsIPBlocked(ip) {
			logger.Info("[Security] Blocked request from %s (public endpoint)", ip)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"too_many_requests","message":"IP address temporarily blocked"}`))
			return
		}

		// Apply rate limit to public endpoints
		if s.CheckRateLimit(ip) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate_limit_exceeded"}`))
			return
		}

		// Add security headers
		s.addSecurityHeaders(w)

		next(w, r)
	}
}

// addSecurityHeaders adds security headers to response
func (s *SecurityService) addSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-XSS-Protection", "1; mode=block")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
}

// GetStats returns security statistics
func (s *SecurityService) GetStats() map[string]interface{} {
	s.ctx.mu.RLock()
	defer s.ctx.mu.RUnlock()

	stats := map[string]interface{}{
		"tracked_ips":     len(s.ctx.rateLimits),
		"blocked_ips":     len(s.ctx.blockedIPs),
		"failed_attempts": len(s.ctx.failedAttempts),
		"auth_enabled":    s.ctx.apiKeys.Enabled,
		"api_keys_count":  len(s.ctx.apiKeys.Keys),
	}

	return stats
}
