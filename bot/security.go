package bot

import (
	"log"
	"sync"
)

// SecurityBridge provides bridge functions for the security system
type SecurityBridge struct {
	// Function pointers (will be set by main package)
	checkRateLimitFunc func(userID int64) (bool, string)
	recordRequestFunc  func(userID int64)
	isAdminFunc        func(userID int64) bool

	mu sync.RWMutex
}

var securityBridge *SecurityBridge

// InitSecurityBridge initializes the security bridge
func InitSecurityBridge() {
	securityBridge = &SecurityBridge{}
	log.Println("[SecurityBridge] Initialized")
}

// SetCheckRateLimitFunc sets the check rate limit function
func SetCheckRateLimitFunc(fn func(userID int64) (bool, string)) {
	if securityBridge != nil {
		securityBridge.mu.Lock()
		securityBridge.checkRateLimitFunc = fn
		securityBridge.mu.Unlock()
	}
}

// SetRecordRequestFunc sets the record request function
func SetRecordRequestFunc(fn func(userID int64)) {
	if securityBridge != nil {
		securityBridge.mu.Lock()
		securityBridge.recordRequestFunc = fn
		securityBridge.mu.Unlock()
	}
}

// SetIsAdminFunc sets the is admin function
func SetIsAdminFunc(fn func(userID int64) bool) {
	if securityBridge != nil {
		securityBridge.mu.Lock()
		securityBridge.isAdminFunc = fn
		securityBridge.mu.Unlock()
	}
}

// Bridge methods

// CheckRateLimit checks if user is rate limited
// Returns (isLimited, message)
func CheckRateLimit(userID int64) (bool, string) {
	if securityBridge == nil || securityBridge.checkRateLimitFunc == nil {
		return false, ""
	}
	securityBridge.mu.RLock()
	defer securityBridge.mu.RUnlock()
	return securityBridge.checkRateLimitFunc(userID)
}

// RecordRequest records a request for rate limiting
func RecordRequest(userID int64) {
	if securityBridge == nil || securityBridge.recordRequestFunc == nil {
		return
	}
	securityBridge.mu.RLock()
	defer securityBridge.mu.RUnlock()
	securityBridge.recordRequestFunc(userID)
}

// IsAdmin checks if user is an admin (admins bypass rate limits)
func IsAdmin(userID int64) bool {
	if securityBridge == nil || securityBridge.isAdminFunc == nil {
		return false
	}
	securityBridge.mu.RLock()
	defer securityBridge.mu.RUnlock()
	return securityBridge.isAdminFunc(userID)
}
