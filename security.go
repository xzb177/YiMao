package main

import (
	"log"
	"sync"
	"time"
)

// SecurityManager handles rate limiting and security
type SecurityManager struct {
	// Rate limiting: userID -> []requestTime
	requests map[int64][]time.Time
	mutex    sync.RWMutex

	// IP ban list
	bannedIPs   map[string]time.Time
	banMutex    sync.RWMutex

	// Failed login attempts
	failedAttempts map[string]int
	failMutex      sync.RWMutex
}

var securityMgr *SecurityManager

// RateLimitConfig defines rate limit rules
type RateLimitConfig struct {
	RequestsPerMinute int
	BanDuration       time.Duration
}

const (
	// Rate limits
	maxRequestsPerMinute = 30
	banDurationMinutes    = 30
)

// InitSecurityManager initializes the security manager
func InitSecurityManager() {
	securityMgr = &SecurityManager{
		requests:       make(map[int64][]time.Time),
		bannedIPs:      make(map[string]time.Time),
		failedAttempts: make(map[string]int),
	}

	// Start cleanup routine
	go securityMgr.cleanup()

	log.Println("SecurityManager initialized")
}

// CheckRateLimit checks if user is rate limited
func (s *SecurityManager) CheckRateLimit(userID int64) (*RateLimitConfig, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	now := time.Now()
	cutoff := now.Add(-1 * time.Minute)

	// Clean old requests
	if requests, ok := s.requests[userID]; ok {
		valid := []time.Time{}
		for _, t := range requests {
			if t.After(cutoff) {
				valid = append(valid, t)
			}
		}
		s.requests[userID] = valid

		// Check if exceeded
		if len(valid) >= maxRequestsPerMinute {
			return &RateLimitConfig{
				RequestsPerMinute: maxRequestsPerMinute,
				BanDuration:       banDurationMinutes * time.Minute,
			}, true
		}
	} else {
		s.requests[userID] = []time.Time{}
	}

	return nil, false
}

// RecordRequest records a request for rate limiting
func (s *SecurityManager) RecordRequest(userID int64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.requests[userID] = append(s.requests[userID], time.Now())
}

// IsBanned checks if an IP is banned
func (s *SecurityManager) IsBanned(ip string) bool {
	s.banMutex.RLock()
	defer s.banMutex.RUnlock()

	if banTime, banned := s.bannedIPs[ip]; banned {
		if time.Now().Before(banTime) {
			return true
		}
	}
	return false
}

// BanIP bans an IP address
func (s *SecurityManager) BanIP(ip string, duration time.Duration) {
	s.banMutex.Lock()
	defer s.banMutex.Unlock()

	s.bannedIPs[ip] = time.Now().Add(duration)

	log.Printf("SecurityManager: Banned IP %s until %v", ip, time.Now().Add(duration))
}

// RecordFailedAttempt records a failed login attempt
func (s *SecurityManager) RecordFailedAttempt(identifier string) {
	s.failMutex.Lock()
	defer s.failMutex.Unlock()

	s.failedAttempts[identifier]++

	// Auto-ban after too many attempts
	if s.failedAttempts[identifier] >= 5 {
		s.BanIP(identifier, time.Hour)
		delete(s.failedAttempts, identifier)
		log.Printf("SecurityManager: Auto-banned %s due to too many failed attempts", identifier)
	}
}

// cleanup periodically cleans up old data
func (s *SecurityManager) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.mutex.Lock()
		for userID, requests := range s.requests {
			cutoff := time.Now().Add(-1 * time.Minute)
			valid := []time.Time{}
			for _, t := range requests {
				if t.After(cutoff) {
					valid = append(valid, t)
				}
			}
			if len(valid) == 0 {
				delete(s.requests, userID)
			} else {
				s.requests[userID] = valid
			}
		}
		s.mutex.Unlock()

		// Clean up expired bans
		s.banMutex.Lock()
		now := time.Now()
		for ip, banTime := range s.bannedIPs {
			if now.After(banTime) {
				delete(s.bannedIPs, ip)
			}
		}
		s.banMutex.Unlock()
	}
}

// GetSecurityStatus returns security status for debugging
func (s *SecurityManager) GetSecurityStatus() map[string]interface{} {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	s.banMutex.RLock()
	defer s.banMutex.RUnlock()

	status := make(map[string]interface{})
	status["active_users"] = len(s.requests)
	status["banned_ips"] = len(s.bannedIPs)
	status["failed_attempts"] = len(s.failedAttempts)

	return status
}
