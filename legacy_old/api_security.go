package main

import (
	"crypto/subtle"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// API Security Config
const (
	// Rate limiting
	rateLimitRequests = 60              // requests per minute
	rateLimitWindow   = time.Minute     // sliding window
	rateLimitCleanup  = 5 * time.Minute // cleanup interval

	// IP-based blocking
	maxFailedAttempts = 5                // failed attempts before block
	blockDuration     = 30 * time.Minute // how long to block

	// Trusted proxies (for X-Forwarded-For)
	trustedProxies = "127.0.0.1,::1"
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
}

var securityCtx *SecurityContext

// InitAPISecurity initializes the API security system
func InitAPISecurity() {
	securityCtx = &SecurityContext{
		apiKeys: APIKeyConfig{
			Keys:    make(map[string]string),
			Enabled: false,
		},
		rateLimits:     make(map[string]*RateLimitEntry),
		failedAttempts: make(map[string]int),
		blockedIPs:     make(map[string]time.Time),
		cleanupDone:    make(chan bool),
	}

	// Start background cleanup
	go securityCtx.cleanupLoop()

	log.Printf("API Security initialized")
}

// SetAPIKeys sets the API keys for authentication
func SetAPIKeys(keys map[string]string) {
	if securityCtx == nil {
		return
	}
	securityCtx.apiKeys.mu.Lock()
	defer securityCtx.apiKeys.mu.Unlock()
	securityCtx.apiKeys.Keys = keys
	securityCtx.apiKeys.Enabled = len(keys) > 0
}

// EnableAPIAuth enables/disables API key authentication
func EnableAPIAuth(enabled bool) {
	if securityCtx == nil {
		return
	}
	securityCtx.apiKeys.mu.Lock()
	defer securityCtx.apiKeys.mu.Unlock()
	securityCtx.apiKeys.Enabled = enabled
}

// getClientIP extracts the real client IP from request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (for proxies/load balancers)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// Get the first IP (original client)
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			ip := strings.TrimSpace(ips[0])
			// Validate it's not from a trusted proxy
			if !isTrustedProxy(ip) {
				return ip
			}
		}
	}

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

// isTrustedProxy checks if an IP is a trusted proxy
func isTrustedProxy(ip string) bool {
	trusted := []string{"127.0.0.1", "::1", "localhost"}
	for _, t := range trusted {
		if ip == t || strings.HasPrefix(ip, "127.") || strings.HasPrefix(ip, "::1") || strings.HasPrefix(ip, "10.") || strings.HasPrefix(ip, "192.168.") || strings.HasPrefix(ip, "172.16.") {
			return true
		}
	}
	return false
}

// isIPBlocked checks if an IP is currently blocked
func (s *SecurityContext) isIPBlocked(ip string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if unblockTime, blocked := s.blockedIPs[ip]; blocked {
		if time.Now().Before(unblockTime) {
			return true
		}
		// Block expired, remove it
		delete(s.blockedIPs, ip)
	}
	return false
}

// recordFailedAttempt records a failed authentication attempt
func (s *SecurityContext) recordFailedAttempt(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.failedAttempts[ip]++
	if s.failedAttempts[ip] >= maxFailedAttempts {
		// Block this IP
		s.blockedIPs[ip] = time.Now().Add(blockDuration)
		log.Printf("[SECURITY] IP %s blocked after %d failed attempts", ip, s.failedAttempts[ip])
		// Reset counter
		delete(s.failedAttempts, ip)
	}
}

// clearFailedAttempts clears failed attempts after successful auth
func (s *SecurityContext) clearFailedAttempts(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failedAttempts, ip)
}

// checkRateLimit checks if a request should be rate limited
func (s *SecurityContext) checkRateLimit(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	entry, exists := s.rateLimits[ip]

	if !exists {
		s.rateLimits[ip] = &RateLimitEntry{
			Count:     1,
			FirstSeen: now,
			LastSeen:  now,
		}
		return false
	}

	// Reset if window expired
	if now.Sub(entry.FirstSeen) >= rateLimitWindow {
		entry.Count = 1
		entry.FirstSeen = now
		entry.LastSeen = now
		return false
	}

	// Check limit
	if entry.Count >= rateLimitRequests {
		entry.LastSeen = now
		return true
	}

	entry.Count++
	entry.LastSeen = now
	return false
}

// cleanupLoop periodically cleans up old rate limit entries
func (s *SecurityContext) cleanupLoop() {
	ticker := time.NewTicker(rateLimitCleanup)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanup()
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
	for ip, entry := range s.rateLimits {
		if now.Sub(entry.LastSeen) > rateLimitWindow*2 {
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
// to prevent timing attacks. Always compares against all keys.
func (s *SecurityContext) validateAPIKey(key string) bool {
	s.apiKeys.mu.RLock()
	defer s.apiKeys.mu.RUnlock()

	if !s.apiKeys.Enabled {
		return true // Authentication disabled
	}

	// Use a constant-time approach: always compare against all keys
	// to prevent timing attacks that could leak key length info
	keyMatch := false
	for validKey := range s.apiKeys.Keys {
		// ConstantTimeCompare returns 1 if equal, 0 if not
		// We OR the results to ensure we check all keys
		if subtle.ConstantTimeCompare([]byte(key), []byte(validKey)) == 1 {
			keyMatch = true
		}
	}
	return keyMatch
}

// SecurityMiddleware wraps an http.Handler with security checks
func SecurityMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if securityCtx == nil {
			next(w, r)
			return
		}

		ip := getClientIP(r)

		// Check if IP is blocked
		if securityCtx.isIPBlocked(ip) {
			log.Printf("[SECURITY] Blocked request from %s", ip)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"too_many_requests","message":"IP address temporarily blocked due to repeated failed attempts"}`))
			return
		}

		// Check API key authentication
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			apiKey = r.URL.Query().Get("api_key")
		}

		if !securityCtx.validateAPIKey(apiKey) {
			log.Printf("[SECURITY] Invalid API key from %s", ip)
			securityCtx.recordFailedAttempt(ip)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized","message":"Invalid or missing API key"}`))
			return
		}

		// Clear failed attempts on successful auth
		securityCtx.clearFailedAttempts(ip)

		// Check rate limit
		if securityCtx.checkRateLimit(ip) {
			log.Printf("[SECURITY] Rate limit exceeded for %s", ip)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate_limit_exceeded","message":"Too many requests. Please try again later."}`))
			return
		}

		// Add security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		next(w, r)
	}
}

// PublicAPIMiddleware applies security but doesn't require API key (for public endpoints)
func PublicAPIMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if securityCtx == nil {
			next(w, r)
			return
		}

		ip := getClientIP(r)

		// Check if IP is blocked (even for public endpoints)
		if securityCtx.isIPBlocked(ip) {
			log.Printf("[SECURITY] Blocked request from %s (public endpoint)", ip)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"too_many_requests","message":"IP address temporarily blocked"}`))
			return
		}

		// Apply rate limit to public endpoints (stricter)
		if securityCtx.checkRateLimit(ip) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate_limit_exceeded"}`))
			return
		}

		// Add security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		next(w, r)
	}
}

// GetSecurityStats returns security statistics
func GetSecurityStats() map[string]interface{} {
	if securityCtx == nil {
		return nil
	}

	securityCtx.mu.RLock()
	defer securityCtx.mu.RUnlock()

	stats := map[string]interface{}{
		"tracked_ips":     len(securityCtx.rateLimits),
		"blocked_ips":     len(securityCtx.blockedIPs),
		"failed_attempts": len(securityCtx.failedAttempts),
		"auth_enabled":    securityCtx.apiKeys.Enabled,
		"api_keys_count":  len(securityCtx.apiKeys.Keys),
	}

	return stats
}
