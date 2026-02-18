package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"sync"
	"time"
)

// QuickLinkManager handles quick linking without admin approval
// Users can link their accounts directly using a verification code sent to their Jellyfin email
type QuickLinkManager struct {
	// Pending quick link requests (code -> request)
	pendingLinks map[string]*QuickLinkRequest
	linkMutex    sync.RWMutex

	// Rate limiting (telegramID -> last request time)
	lastRequestTime map[int64]time.Time
	rateMutex       sync.RWMutex

	// Storage file
	storageFile string

	// Cooldown period between requests
	requestCooldown time.Duration

	// Code expiry time
	codeExpiry time.Duration
}

// QuickLinkRequest represents a pending quick link request
type QuickLinkRequest struct {
	Code         string    `json:"code"`
	TelegramID   int64     `json:"telegramId"`
	TelegramName string    `json:"telegramName"`
	Username     string    `json:"username"`     // Jellyfin username
	CreatedAt    time.Time `json:"createdAt"`
	ExpiresAt    time.Time `json:"expiresAt"`
	UsedAt       *time.Time `json:"usedAt,omitempty"`
	Linked       bool      `json:"linked"`
}

// QuickLinkData stores all quick link requests
type QuickLinkData struct {
	PendingLinks map[string]*QuickLinkRequest `json:"pendingLinks"`
	LastSync     string                       `json:"lastSync"`
}

var quickLinkMgr *QuickLinkManager

// InitQuickLinkManager initializes the quick link manager
func InitQuickLinkManager() {
	quickLinkMgr = &QuickLinkManager{
		pendingLinks:     make(map[string]*QuickLinkRequest),
		lastRequestTime:  make(map[int64]time.Time),
		storageFile:      "quick_links.json",
		requestCooldown:  1 * time.Minute, // 1 minute between requests
		codeExpiry:       10 * time.Minute, // Code valid for 10 minutes
	}

	// Load existing data
	quickLinkMgr.load()

	// Start cleanup routine
	go quickLinkMgr.cleanupExpired()

	log.Println("QuickLink manager initialized")
}

// CreateQuickLinkRequest creates a new quick link request
// Returns the verification code or error
func (m *QuickLinkManager) CreateQuickLinkRequest(telegramID int64, telegramName, username string) (string, error) {
	// Check rate limit
	m.rateMutex.RLock()
	lastTime, exists := m.lastRequestTime[telegramID]
	m.rateMutex.RUnlock()

	if exists && time.Since(lastTime) < m.requestCooldown {
		remaining := m.requestCooldown - time.Since(lastTime)
		return "", fmt.Errorf("请求过于频繁，请 %d 秒后再试", int(remaining.Seconds()))
	}

	// Check if user is already linked
	if userSyncMgr != nil {
		if _, exists := userSyncMgr.GetJellyseerrUserID(telegramID); exists {
			return "", fmt.Errorf("你已经绑定过账号了")
		}
	}

	// Generate verification code
	code := generateQuickLinkCode()

	// Create request
	req := &QuickLinkRequest{
		Code:         code,
		TelegramID:   telegramID,
		TelegramName: telegramName,
		Username:     username,
		CreatedAt:    time.Now(),
		ExpiresAt:    time.Now().Add(m.codeExpiry),
		Linked:       false,
	}

	// Save request
	m.linkMutex.Lock()
	// Remove any existing pending request for this user
	for k, v := range m.pendingLinks {
		if v.TelegramID == telegramID && !v.Linked {
			delete(m.pendingLinks, k)
		}
	}
	m.pendingLinks[code] = req
	m.save()
	m.linkMutex.Unlock()

	// Update rate limit
	m.rateMutex.Lock()
	m.lastRequestTime[telegramID] = time.Now()
	m.rateMutex.Unlock()

	log.Printf("QuickLink: Created request %s for Telegram %d (%s)", code, telegramID, telegramName)

	return code, nil
}

// VerifyQuickLinkCode verifies and processes a quick link code
// Returns (success, jellyseerrID, displayName, error)
func (m *QuickLinkManager) VerifyQuickLinkCode(code string) (bool, int64, string, error) {
	m.linkMutex.Lock()
	defer m.linkMutex.Unlock()

	req, exists := m.pendingLinks[code]
	if !exists {
		return false, 0, "", fmt.Errorf("验证码不存在")
	}

	if req.Linked {
		return false, 0, "", fmt.Errorf("验证码已被使用")
	}

	if time.Now().After(req.ExpiresAt) {
		delete(m.pendingLinks, code)
		m.save()
		return false, 0, "", fmt.Errorf("验证码已过期")
	}

	// Check if userSyncMgr is initialized
	if userSyncMgr == nil {
		return false, 0, "", fmt.Errorf("用户同步管理器未初始化")
	}

	// Verify credentials with Jellyseerr
	jellyseerrID, displayName, err := userSyncMgr.VerifyJellyfinCredentials(req.Username, code)
	if err != nil {
		return false, 0, "", fmt.Errorf("账号验证失败: %w", err)
	}

	// Mark as linked
	now := time.Now()
	req.Linked = true
	req.UsedAt = &now

	// Create user mapping
	if err := userSyncMgr.SetUserMapping(req.TelegramID, jellyseerrID); err != nil {
		log.Printf("QuickLink: Failed to create mapping: %v", err)
		return false, 0, "", fmt.Errorf("创建映射失败: %w", err)
	}

	// Clean up used request
	delete(m.pendingLinks, code)
	m.save()

	log.Printf("QuickLink: Successfully linked Telegram %d to Jellyseerr %d (%s)",
		req.TelegramID, jellyseerrID, displayName)

	return true, jellyseerrID, displayName, nil
}

// GetPendingRequest gets a pending quick link request by code
func (m *QuickLinkManager) GetPendingRequest(code string) *QuickLinkRequest {
	m.linkMutex.RLock()
	defer m.linkMutex.RUnlock()

	return m.pendingLinks[code]
}

// GetUserPendingRequest gets the pending request for a Telegram user
func (m *QuickLinkManager) GetUserPendingRequest(telegramID int64) *QuickLinkRequest {
	m.linkMutex.RLock()
	defer m.linkMutex.RUnlock()

	for _, req := range m.pendingLinks {
		if req.TelegramID == telegramID && !req.Linked && time.Now().Before(req.ExpiresAt) {
			return req
		}
	}
	return nil
}

// CancelRequest cancels a pending quick link request
func (m *QuickLinkManager) CancelRequest(telegramID int64) bool {
	m.linkMutex.Lock()
	defer m.linkMutex.Unlock()

	for code, req := range m.pendingLinks {
		if req.TelegramID == telegramID && !req.Linked {
			delete(m.pendingLinks, code)
			m.save()
			log.Printf("QuickLink: Cancelled request %s for Telegram %d", code, telegramID)
			return true
		}
	}
	return false
}

// cleanupExpired removes expired requests periodically
func (m *QuickLinkManager) cleanupExpired() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		m.linkMutex.Lock()
		now := time.Now()
		cleaned := 0

		for code, req := range m.pendingLinks {
			if now.After(req.ExpiresAt) || req.Linked {
				delete(m.pendingLinks, code)
				cleaned++
			}
		}

		if cleaned > 0 {
			m.save()
			log.Printf("QuickLink: Cleaned up %d expired/used requests", cleaned)
		}

		m.linkMutex.Unlock()
	}
}

// save saves quick link data to file
func (m *QuickLinkManager) save() {
	data := QuickLinkData{
		PendingLinks: m.pendingLinks,
		LastSync:     time.Now().Format(time.RFC3339),
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Printf("QuickLink: Failed to marshal data: %v", err)
		return
	}

	if err := os.WriteFile(m.storageFile, jsonData, 0644); err != nil {
		log.Printf("QuickLink: Failed to save data: %v", err)
	}
}

// load loads quick link data from file
func (m *QuickLinkManager) load() {
	data, err := os.ReadFile(m.storageFile)
	if err != nil {
		log.Printf("QuickLink: Data file not found, starting fresh: %v", err)
		return
	}

	var loaded QuickLinkData
	if err := json.Unmarshal(data, &loaded); err != nil {
		log.Printf("QuickLink: Failed to load data: %v", err)
		return
	}

	m.linkMutex.Lock()
	m.pendingLinks = loaded.PendingLinks
	m.linkMutex.Unlock()

	log.Printf("QuickLink: Loaded %d pending requests", len(m.pendingLinks))
}

// generateQuickLinkCode generates a 6-digit numeric code for quick linking
func generateQuickLinkCode() string {
	const digits = "0123456789"
	code := make([]byte, 6)

	// Use crypto/rand for secure random generation
	for i := 0; i < 6; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			// Fallback
			code[i] = digits[time.Now().UnixNano()%10]
		} else {
			code[i] = digits[n.Int64()]
		}
	}

	return string(code)
}

// GetQuickLinkHelp returns help text for quick link feature
func GetQuickLinkHelp() string {
	return `🔗 *快速绑定账号*

无需管理员审核，直接绑定！

📋 绑定步骤：
1. 发送 /quicklink 账号名 密码
2. 系统发送验证码到你的邮箱
3. 收到验证码后发送 /verify 码

💡 小贴士：
• 验证码10分钟内有效
• 每分钟最多请求一次
• 验证码即你的 Jellyfin 登录密码`
}
