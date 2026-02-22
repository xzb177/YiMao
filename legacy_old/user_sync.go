package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// UserSyncManager handles user synchronization between Telegram and Jellyseerr
type UserSyncManager struct {
	jellyseerrURL string
	apiKey        string
	httpClient    *http.Client

	// User mappings
	telegramToJellyseerr map[int64]int64  // telegramUserID -> jellyseerrUserID
	jellyseerrToTelegram map[int64]int64  // jellyseerrUserID -> telegramUserID
	jellyfinToTelegram   map[string]int64 // jellyfinUserID -> telegramUserID
	telegramUsernames    map[int64]string // telegramUserID -> username (for displaying in notifications)
	mappingMutex         sync.RWMutex

	// Verification codes for user linking (deprecated)
	verificationCodes map[string]*VerificationCode
	verifyMutex       sync.RWMutex

	// Binding requests (new system)
	bindingRequests map[string]*BindingRequest
	bindingMutex    sync.RWMutex

	// User profile cache
	userProfiles map[int64]*JellyseerrUserProfile
	profileMutex sync.RWMutex

	// Storage files
	mappingFile string
	verifyFile  string
	bindingFile string
}

// JellyseerrUserProfile represents extended user information from Jellyseerr
type JellyseerrUserProfile struct {
	ID             int    `json:"id"`
	Email          string `json:"email"`
	Username       string `json:"username"`
	DisplayName    string `json:"displayName"`
	Avatar         string `json:"avatar"`
	JellyfinUserID string `json:"jellyfinUserId"`
	TelegramID     string `json:"telegramId,omitempty"`
	// Request quotas
	MovieQuota int `json:"movieQuota"`
	TVQuota    int `json:"tvQuota"`
	// Request counts
	MovieRequests int `json:"movieRequests"`
	TVRequests    int `json:"tvRequests"`
	// Account status
	IsActive  bool   `json:"isActive"`
	IsAdmin   bool   `json:"isAdmin"`
	CreatedAt string `json:"createdAt"`
	LastLogin string `json:"lastLogin,omitempty"`
}

// VerificationCode represents a pending user linking verification (deprecated - using BindingRequest instead)
type VerificationCode struct {
	Code       string     `json:"code"`
	TelegramID int64      `json:"telegramId"`
	Username   string     `json:"username"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	IsUsed     bool       `json:"isUsed"`
	LinkedAt   *time.Time `json:"linkedAt,omitempty"`
}

// BindingRequest represents a pending account binding request awaiting admin approval
type BindingRequest struct {
	RequestID          string     `json:"requestId"`          // Unique request ID
	TelegramID         int64      `json:"telegramId"`         // Telegram user ID
	TelegramName       string     `json:"telegramName"`       // Telegram user name
	JellyseerrID       int64      `json:"jellyseerrId"`       // Jellyseerr user ID
	JellyseerrName     string     `json:"jellyseerrName"`     // Jellyseerr display name
	JellyseerrUsername string     `json:"jellyseerrUsername"` // Jellyseerr username
	CreatedAt          time.Time  `json:"createdAt"`
	ExpiresAt          time.Time  `json:"expiresAt"`
	Status             string     `json:"status"`               // "pending", "approved", "rejected"
	ReviewedBy         int64      `json:"reviewedBy,omitempty"` // Admin Telegram ID who reviewed
	ReviewedAt         *time.Time `json:"reviewedAt,omitempty"`
}

// BindingRequestData stores all binding requests
type BindingRequestData struct {
	Requests map[string]*BindingRequest `json:"requests"`
	LastSync string                     `json:"lastSync"`
}

// UserMappingData stores the complete user mapping data
type UserMappingData struct {
	TelegramToJellyseerr map[int64]int64  `json:"telegramToJellyseerr"`
	JellyseerrToTelegram map[int64]int64  `json:"jellyseerrToTelegram"`
	JellyfinToTelegram   map[string]int64 `json:"jellyfinToTelegram"`
	TelegramUsernames    map[int64]string `json:"telegramUsernames,omitempty"`
	LastSync             string           `json:"lastSync"`
}

var userSyncMgr *UserSyncManager

// InitUserSyncManager initializes the user sync manager
func InitUserSyncManager() {
	userSyncMgr = &UserSyncManager{
		jellyseerrURL:        jellyseerrURL,
		apiKey:               os.Getenv("JELLYSEERR_API_KEY"),
		httpClient:           &http.Client{Timeout: 30 * time.Second},
		telegramToJellyseerr: make(map[int64]int64),
		jellyseerrToTelegram: make(map[int64]int64),
		jellyfinToTelegram:   make(map[string]int64),
		telegramUsernames:    make(map[int64]string),
		verificationCodes:    make(map[string]*VerificationCode),
		bindingRequests:      make(map[string]*BindingRequest),
		userProfiles:         make(map[int64]*JellyseerrUserProfile),
		mappingFile:          "user_mappings.json",
		verifyFile:           "verification_codes.json",
		bindingFile:          "binding_requests.json",
	}

	// Load existing data
	userSyncMgr.loadMappings()
	userSyncMgr.loadVerificationCodes()
	userSyncMgr.loadBindingRequests()

	// Sync users from Jellyseerr
	go userSyncMgr.syncUsersFromJellyseerr()

	// Start background tasks
	go userSyncMgr.periodicSync()
	go userSyncMgr.cleanupExpiredCodes()
	go userSyncMgr.periodicCleanupBindingRequests()

	log.Println("UserSync manager initialized")
}

// syncUsersFromJellyseerr syncs all users from Jellyseerr
func (m *UserSyncManager) syncUsersFromJellyseerr() error {
	if m.apiKey == "" {
		log.Println("User sync: Jellyseerr API not configured")
		return fmt.Errorf("API not configured")
	}

	log.Println("User sync: Starting full sync...")

	// Fetch all users from Jellyseerr (paginated using skip/take)
	skip := 0
	take := 50
	totalSynced := 0
	totalUsers := 0

	for {
		users, pageInfo, err := m.fetchUsersPage(skip, take)
		if err != nil {
			log.Printf("User sync: Error fetching users at skip %d: %v", skip, err)
			// Don't return error on first batch, just log it
			if skip == 0 {
				return err
			}
			break
		}

		totalUsers += len(users)
		if len(users) == 0 {
			break
		}

		// Process each user
		for _, user := range users {
			// Cache user profile
			m.profileMutex.Lock()
			m.userProfiles[int64(user.ID)] = &user
			m.profileMutex.Unlock()

			// Check for existing Telegram ID mapping
			if user.TelegramID != "" {
				var telegramID int64
				fmt.Sscanf(user.TelegramID, "%d", &telegramID)
				if telegramID > 0 {
					m.mappingMutex.Lock()
					m.telegramToJellyseerr[telegramID] = int64(user.ID)
					m.jellyseerrToTelegram[int64(user.ID)] = telegramID
					m.mappingMutex.Unlock()
					totalSynced++
					log.Printf("User sync: Mapped Telegram user %d to Jellyseerr user %d (%s)",
						telegramID, user.ID, user.DisplayName)
				}
			}

			// Check for Jellyfin user ID mapping
			if user.JellyfinUserID != "" {
				m.mappingMutex.Lock()
				if telegramID, exists := m.jellyfinToTelegram[user.JellyfinUserID]; exists {
					// Cross-reference: link Jellyfin user to Telegram user
					m.telegramToJellyseerr[telegramID] = int64(user.ID)
					m.jellyseerrToTelegram[int64(user.ID)] = telegramID
					log.Printf("User sync: Cross-mapped Jellyfin user %s to Telegram %d via Jellyseerr %d",
						user.JellyfinUserID, telegramID, user.ID)
					totalSynced++
				}
				m.mappingMutex.Unlock()
			}
		}

		// Check if we've fetched all users
		if skip+len(users) >= pageInfo.Total {
			break
		}
		skip += len(users)
	}

	// Save mappings
	m.saveMappings()

	log.Printf("User sync: Completed. Total users: %d, Mapped: %d", totalUsers, totalSynced)
	return nil
}

// PageInfo represents pagination information
type PageInfo struct {
	Page     int `json:"page"`
	Pages    int `json:"pages"`
	PageSize int `json:"pageSize"`
	Total    int `json:"total"`
}

// fetchUsersPage fetches a page of users from Jellyseerr using skip/take
func (m *UserSyncManager) fetchUsersPage(skip int, take int) ([]JellyseerrUserProfile, *PageInfo, error) {
	url := fmt.Sprintf("%s/api/v1/user?skip=%d&take=%d&sort=created", m.jellyseerrURL, skip, take)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, nil, err
	}

	req.Header.Set("X-Api-Key", m.apiKey)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Jellyseerr returns paginated response with pageInfo
	var response struct {
		PageInfo PageInfo                `json:"pageInfo"`
		Results  []JellyseerrUserProfile `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, nil, err
	}

	return response.Results, &response.PageInfo, nil
}

// periodicSync performs periodic user synchronization
func (m *UserSyncManager) periodicSync() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	// Initial sync after a short delay
	time.Sleep(1 * time.Minute)
	m.syncUsersFromJellyseerr()

	for range ticker.C {
		m.syncUsersFromJellyseerr()
	}
}

// periodicCleanupBindingRequests periodically cleans up expired/old binding requests
func (m *UserSyncManager) periodicCleanupBindingRequests() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	// Initial cleanup after 5 minutes
	time.Sleep(5 * time.Minute)
	m.CleanupExpiredBindingRequests()

	for range ticker.C {
		m.CleanupExpiredBindingRequests()
	}
}

// GetJellyseerrUserID gets the Jellyseerr user ID for a Telegram user

// GetJellyseerrUserID gets the Jellyseerr user ID for a Telegram user
func (m *UserSyncManager) GetJellyseerrUserID(telegramID int64) (int64, bool) {
	m.mappingMutex.RLock()
	defer m.mappingMutex.RUnlock()

	jellyseerrID, exists := m.telegramToJellyseerr[telegramID]
	return jellyseerrID, exists
}

// GetTelegramUserID gets the Telegram user ID for a Jellyseerr user
func (m *UserSyncManager) GetTelegramUserID(jellyseerrID int64) (int64, bool) {
	m.mappingMutex.RLock()
	defer m.mappingMutex.RUnlock()

	telegramID, exists := m.jellyseerrToTelegram[jellyseerrID]
	return telegramID, exists
}

// GetUserProfile gets a user's profile from Jellyseerr
func (m *UserSyncManager) GetUserProfile(jellyseerrID int64) (*JellyseerrUserProfile, error) {
	m.profileMutex.RLock()
	if profile, exists := m.userProfiles[jellyseerrID]; exists {
		m.profileMutex.RUnlock()
		return profile, nil
	}
	m.profileMutex.RUnlock()

	// Fetch from API
	url := fmt.Sprintf("%s/api/v1/user/%d", m.jellyseerrURL, jellyseerrID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Api-Key", m.apiKey)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var profile JellyseerrUserProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, err
	}

	// Cache the profile
	m.profileMutex.Lock()
	m.userProfiles[jellyseerrID] = &profile
	m.profileMutex.Unlock()

	return &profile, nil
}

// SetUserMapping sets a mapping between Telegram and Jellyseerr users
func (m *UserSyncManager) SetUserMapping(telegramID int64, jellyseerrID int64) error {
	m.mappingMutex.Lock()
	defer m.mappingMutex.Unlock()

	m.telegramToJellyseerr[telegramID] = jellyseerrID
	m.jellyseerrToTelegram[jellyseerrID] = telegramID

	if err := m.saveMappingsUnsafe(); err != nil {
		return err
	}

	log.Printf("User sync: Set mapping Telegram %d <-> Jellyseerr %d", telegramID, jellyseerrID)
	return nil
}

// RemoveUserMapping removes a user mapping
func (m *UserSyncManager) RemoveUserMapping(telegramID int64) {
	m.mappingMutex.Lock()
	defer m.mappingMutex.Unlock()

	if jellyseerrID, exists := m.telegramToJellyseerr[telegramID]; exists {
		delete(m.telegramToJellyseerr, telegramID)
		delete(m.jellyseerrToTelegram, jellyseerrID)
		m.saveMappingsUnsafe()
		log.Printf("User sync: Removed mapping for Telegram %d", telegramID)
	}
}

// GenerateVerificationCode generates a verification code for user linking
func (m *UserSyncManager) GenerateVerificationCode(telegramID int64, username string) string {
	// Generate a 6-character code
	code := generateRandomCode(6)

	m.verifyMutex.Lock()
	defer m.verifyMutex.Unlock()

	// Remove any existing codes for this user
	for k, v := range m.verificationCodes {
		if v.TelegramID == telegramID {
			delete(m.verificationCodes, k)
		}
	}

	// Create new verification code
	verifyCode := &VerificationCode{
		Code:       code,
		TelegramID: telegramID,
		Username:   username,
		CreatedAt:  time.Now(),
		ExpiresAt:  time.Now().Add(10 * time.Minute),
		IsUsed:     false,
	}

	m.verificationCodes[code] = verifyCode
	m.saveVerificationCodesUnsafe()

	log.Printf("User sync: Generated verification code %s for Telegram %d (%s)",
		code, telegramID, username)

	return code
}

// GenerateLinkConfirmationCode generates a simple confirmation code for account linking
// Returns a 4-digit code that user must confirm to link their account
func (m *UserSyncManager) GenerateLinkConfirmationCode(telegramID int64, jellyseerrID int64) string {
	// Generate a 4-digit numeric code
	code := fmt.Sprintf("%04d", time.Now().Unix()%10000)

	m.verifyMutex.Lock()
	defer m.verifyMutex.Unlock()

	// Store with special key format
	key := fmt.Sprintf("link_confirm_%d_%d", telegramID, jellyseerrID)
	m.verificationCodes[key] = &VerificationCode{
		Code:       code,
		TelegramID: telegramID,
		Username:   fmt.Sprintf("pending_%d", jellyseerrID),
		CreatedAt:  time.Now(),
		ExpiresAt:  time.Now().Add(5 * time.Minute),
		IsUsed:     false,
	}
	m.saveVerificationCodesUnsafe()

	log.Printf("User sync: Generated link confirmation code %s for Telegram %d -> Jellyseerr %d",
		code, telegramID, jellyseerrID)

	return code
}

// ValidateLinkConfirmationCode validates a link confirmation code
func (m *UserSyncManager) ValidateLinkConfirmationCode(telegramID int64, jellyseerrID int64, code string) bool {
	m.verifyMutex.Lock()
	defer m.verifyMutex.Unlock()

	key := fmt.Sprintf("link_confirm_%d_%d", telegramID, jellyseerrID)
	vc, exists := m.verificationCodes[key]
	if !exists {
		return false
	}

	if vc.IsUsed || time.Now().After(vc.ExpiresAt) {
		delete(m.verificationCodes, key)
		return false
	}

	if vc.Code == code {
		vc.IsUsed = true
		// Set up the mapping
		m.telegramToJellyseerr[telegramID] = jellyseerrID
		m.jellyseerrToTelegram[jellyseerrID] = telegramID
		m.saveMappingsUnsafe()
		m.saveVerificationCodesUnsafe()

		log.Printf("User sync: Link confirmed via code, Telegram %d -> Jellyseerr %d", telegramID, jellyseerrID)
		return true
	}

	return false
}

// ValidateVerificationCode validates and uses a verification code
func (m *UserSyncManager) ValidateVerificationCode(code string) (*VerificationCode, error) {
	m.verifyMutex.Lock()
	defer m.verifyMutex.Unlock()

	verifyCode, exists := m.verificationCodes[code]
	if !exists {
		return nil, fmt.Errorf("验证码不存在")
	}

	if verifyCode.IsUsed {
		return nil, fmt.Errorf("验证码已被使用")
	}

	if time.Now().After(verifyCode.ExpiresAt) {
		delete(m.verificationCodes, code)
		m.saveVerificationCodesUnsafe()
		return nil, fmt.Errorf("验证码已过期")
	}

	return verifyCode, nil
}

// MarkVerificationCodeUsed marks a verification code as used
func (m *UserSyncManager) MarkVerificationCodeUsed(code string, jellyseerrID int64) error {
	m.verifyMutex.Lock()
	defer m.verifyMutex.Unlock()

	verifyCode, exists := m.verificationCodes[code]
	if !exists {
		return fmt.Errorf("验证码不存在")
	}

	now := time.Now()
	verifyCode.IsUsed = true
	verifyCode.LinkedAt = &now

	// Set up the mapping
	m.telegramToJellyseerr[verifyCode.TelegramID] = jellyseerrID
	m.jellyseerrToTelegram[jellyseerrID] = verifyCode.TelegramID

	// Save both files
	m.saveVerificationCodesUnsafe()
	m.saveMappingsUnsafe()

	log.Printf("User sync: Verification code %s used, linked Telegram %d to Jellyseerr %d",
		code, verifyCode.TelegramID, jellyseerrID)

	return nil
}

// cleanupExpiredCodes removes expired verification codes
func (m *UserSyncManager) cleanupExpiredCodes() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		m.verifyMutex.Lock()
		now := time.Now()
		cleaned := 0

		for code, vc := range m.verificationCodes {
			if now.After(vc.ExpiresAt) || vc.IsUsed {
				delete(m.verificationCodes, code)
				cleaned++
			}
		}

		if cleaned > 0 {
			m.saveVerificationCodesUnsafe()
			log.Printf("User sync: Cleaned up %d expired/used verification codes", cleaned)
		}

		m.verifyMutex.Unlock()
	}
}

// GetAllMappings returns all user mappings
func (m *UserSyncManager) GetAllMappings() map[int64]int64 {
	m.mappingMutex.RLock()
	defer m.mappingMutex.RUnlock()

	// Return a copy
	result := make(map[int64]int64)
	for k, v := range m.telegramToJellyseerr {
		result[k] = v
	}
	return result
}

// FormatUserMappings formats user mappings for display
func (m *UserSyncManager) FormatUserMappings() string {
	mappings := m.GetAllMappings()

	if len(mappings) == 0 {
		return "📋 *用户映射*\n\n暂无已绑定用户"
	}

	msg := "📋 *用户映射列表*\n\n"
	msg += fmt.Sprintf("共 %d 位已绑定用户\n\n", len(mappings))

	count := 1
	for telegramID, jellyseerrID := range mappings {
		// Get user profile
		profile, err := m.GetUserProfile(jellyseerrID)
		if err == nil {
			displayName := profile.DisplayName
			if displayName == "" {
				displayName = profile.Username
			}
			msg += fmt.Sprintf("%d. %s (Telegram: %d, Jellyseerr: %d)\n", count, displayName, telegramID, jellyseerrID)
		} else {
			msg += fmt.Sprintf("%d. Telegram: %d, Jellyseerr: %d\n", count, telegramID, jellyseerrID)
		}
		count++

		if count > 20 {
			msg += fmt.Sprintf("\n... 还有 %d 位用户", len(mappings)-20)
			break
		}
	}

	return msg
}

// loadMappings loads user mappings from file
func (m *UserSyncManager) loadMappings() {
	data, err := os.ReadFile(m.mappingFile)
	if err != nil {
		log.Printf("User sync: Mapping file not found, starting fresh: %v", err)
		return
	}

	var mappingData UserMappingData
	if err := json.Unmarshal(data, &mappingData); err != nil {
		log.Printf("User sync: Failed to load mappings: %v", err)
		// Try old format with string keys
		m.loadStringKeyMappings(data)
		return
	}

	// Check if we got any data (might be empty if JSON had string keys)
	log.Printf("User sync: Parsed mappings - telegramToJellyseerr: %d, jellyseerrToTelegram: %d, usernames: %d",
		len(mappingData.TelegramToJellyseerr), len(mappingData.JellyseerrToTelegram), len(mappingData.TelegramUsernames))

	// If all maps are empty, try string key format
	if len(mappingData.TelegramToJellyseerr) == 0 && len(mappingData.JellyseerrToTelegram) == 0 {
		log.Printf("User sync: Empty mappings loaded, trying string key format...")
		m.loadStringKeyMappings(data)
		return
	}

	m.mappingMutex.Lock()
	m.telegramToJellyseerr = mappingData.TelegramToJellyseerr
	m.jellyseerrToTelegram = mappingData.JellyseerrToTelegram
	m.jellyfinToTelegram = mappingData.JellyfinToTelegram
	if mappingData.TelegramUsernames != nil {
		m.telegramUsernames = mappingData.TelegramUsernames
	} else {
		m.telegramUsernames = make(map[int64]string)
	}
	m.mappingMutex.Unlock()

	log.Printf("User sync: Loaded %d user mappings", len(m.telegramToJellyseerr))
}

// loadStringKeyMappings loads mappings with string keys (old format)
func (m *UserSyncManager) loadStringKeyMappings(data []byte) {
	// Try to parse as UserMappingData but with string keys
	type OldMappingData struct {
		TelegramToJellyseerr map[string]int    `json:"telegramToJellyseerr"`
		JellyseerrToTelegram map[string]int    `json:"jellyseerrToTelegram"`
		JellyfinToTelegram   map[string]int64  `json:"jellyfinToTelegram"`
		TelegramUsernames    map[string]string `json:"telegramUsernames"`
	}

	var oldData OldMappingData
	if err := json.Unmarshal(data, &oldData); err != nil {
		log.Printf("User sync: Failed to load string key mappings: %v", err)
		// Try even older format (simple map)
		m.loadVeryOldMappings(data)
		return
	}

	// Convert string keys to int64 keys
	m.mappingMutex.Lock()
	m.telegramToJellyseerr = make(map[int64]int64)
	m.jellyseerrToTelegram = make(map[int64]int64)
	m.jellyfinToTelegram = make(map[string]int64)
	m.telegramUsernames = make(map[int64]string)

	for keyStr, val := range oldData.TelegramToJellyseerr {
		if keyInt, err := strconv.ParseInt(keyStr, 10, 64); err == nil {
			m.telegramToJellyseerr[keyInt] = int64(val)
		}
	}

	for keyStr, val := range oldData.JellyseerrToTelegram {
		if keyInt, err := strconv.ParseInt(keyStr, 10, 64); err == nil {
			m.jellyseerrToTelegram[keyInt] = int64(val)
		}
	}

	m.jellyfinToTelegram = oldData.JellyfinToTelegram

	for keyStr, val := range oldData.TelegramUsernames {
		if keyInt, err := strconv.ParseInt(keyStr, 10, 64); err == nil {
			m.telegramUsernames[keyInt] = val
		}
	}

	m.mappingMutex.Unlock()

	log.Printf("User sync: Loaded %d user mappings from string-key format", len(m.telegramToJellyseerr))

	// Save in new format immediately
	m.saveMappings()
}

// loadVeryOldMappings loads very old format (simple map)
func (m *UserSyncManager) loadVeryOldMappings(data []byte) {
	var oldFormat map[int64]int64
	if err := json.Unmarshal(data, &oldFormat); err != nil {
		log.Printf("User sync: Failed to load legacy mappings: %v", err)
		return
	}

	m.mappingMutex.Lock()
	m.telegramToJellyseerr = oldFormat
	m.jellyseerrToTelegram = make(map[int64]int64)
	// Reverse mapping
	for telegramID, jellyseerrID := range oldFormat {
		m.jellyseerrToTelegram[jellyseerrID] = telegramID
	}
	m.mappingMutex.Unlock()

	log.Printf("User sync: Loaded %d legacy user mappings", len(oldFormat))
}

// saveMappings saves user mappings to file
func (m *UserSyncManager) saveMappings() {
	m.mappingMutex.Lock()
	defer m.mappingMutex.Unlock()
	m.saveMappingsUnsafe()
}

// saveMappingsUnsafe saves mappings without locking (caller must hold lock)
// Uses atomic write when possible, falls back to direct write
func (m *UserSyncManager) saveMappingsUnsafe() error {
	mappingData := UserMappingData{
		TelegramToJellyseerr: m.telegramToJellyseerr,
		JellyseerrToTelegram: m.jellyseerrToTelegram,
		JellyfinToTelegram:   m.jellyfinToTelegram,
		TelegramUsernames:    m.telegramUsernames,
		LastSync:             time.Now().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(mappingData, "", "  ")
	if err != nil {
		log.Printf("User sync: Failed to marshal mappings: %v", err)
		return err
	}

	// Try atomic write with temp file first
	tmpFile := m.mappingFile + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		log.Printf("User sync: Failed to write temp file: %v", err)
		// Fall back to direct write
		return m.writeMappingsDirect(data)
	}

	// Try sync to ensure data is written
	if err := syncFile(tmpFile); err != nil {
		log.Printf("User sync: Failed to sync temp file: %v", err)
	}

	// Try atomic rename
	if err := os.Rename(tmpFile, m.mappingFile); err != nil {
		log.Printf("User sync: Failed to rename temp file: %v", err)
		os.Remove(tmpFile)
		// Fall back to direct write
		return m.writeMappingsDirect(data)
	}

	log.Printf("User sync: Mappings saved atomically to %s", m.mappingFile)
	return nil
}

// writeMappingsDirect writes mappings directly to file (fallback)
func (m *UserSyncManager) writeMappingsDirect(data []byte) error {
	if err := os.WriteFile(m.mappingFile, data, 0644); err != nil {
		log.Printf("User sync: Failed to write mappings directly: %v", err)
		return err
	}
	log.Printf("User sync: Mappings saved directly to %s", m.mappingFile)
	return nil
}

// syncFile ensures data is written to disk
func syncFile(filename string) error {
	f, err := os.OpenFile(filename, os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// loadVerificationCodes loads verification codes from file
func (m *UserSyncManager) loadVerificationCodes() {
	data, err := os.ReadFile(m.verifyFile)
	if err != nil {
		log.Printf("User sync: Verification codes file not found, starting fresh: %v", err)
		return
	}

	if err := json.Unmarshal(data, &m.verificationCodes); err != nil {
		log.Printf("User sync: Failed to load verification codes: %v", err)
		return
	}

	log.Printf("User sync: Loaded %d verification codes", len(m.verificationCodes))
}

// saveVerificationCodes saves verification codes to file
func (m *UserSyncManager) saveVerificationCodes() {
	m.verifyMutex.Lock()
	defer m.verifyMutex.Unlock()
	m.saveVerificationCodesUnsafe()
}

// saveVerificationCodesUnsafe saves verification codes without locking
func (m *UserSyncManager) saveVerificationCodesUnsafe() error {
	data, err := json.MarshalIndent(m.verificationCodes, "", "  ")
	if err != nil {
		log.Printf("User sync: Failed to marshal verification codes: %v", err)
		return err
	}

	if err := os.WriteFile(m.verifyFile, data, 0644); err != nil {
		log.Printf("User sync: Failed to save verification codes: %v", err)
		return err
	}

	return nil
}

// generateRandomCode generates a random alphanumeric code using crypto/rand
func generateRandomCode(length int) string {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // Removed confusing chars
	b := make([]byte, length)
	for i := range b {
		// Use crypto/rand for better randomness
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			// Fallback to time-based if crypto fails (shouldn't happen)
			b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
		} else {
			b[i] = charset[n.Int64()]
		}
	}
	return string(b)
}

// GetLinkedTelegramIDForJellyfinUser finds Telegram ID by Jellyfin user ID
func (m *UserSyncManager) GetLinkedTelegramIDForJellyfinUser(jellyfinID string) (int64, bool) {
	m.mappingMutex.RLock()
	defer m.mappingMutex.RUnlock()

	telegramID, exists := m.jellyfinToTelegram[jellyfinID]
	return telegramID, exists
}

// SetJellyfinToTelegramMapping sets a mapping from Jellyfin user ID to Telegram user ID
func (m *UserSyncManager) SetJellyfinToTelegramMapping(jellyfinID string, telegramID int64) {
	m.mappingMutex.Lock()
	defer m.mappingMutex.Unlock()

	m.jellyfinToTelegram[jellyfinID] = telegramID
	m.saveMappingsUnsafe()

	log.Printf("User sync: Set Jellyfin %s -> Telegram %d mapping", jellyfinID, telegramID)
}

// SetTelegramUsername sets the Telegram username for a user
func (m *UserSyncManager) SetTelegramUsername(telegramID int64, username string) {
	m.mappingMutex.Lock()
	defer m.mappingMutex.Unlock()

	m.telegramUsernames[telegramID] = username
	m.saveMappingsUnsafe()

	log.Printf("User sync: Set Telegram username for %d: %s", telegramID, username)
}

// GetTelegramUsername gets the Telegram username for a user
func (m *UserSyncManager) GetTelegramUsername(telegramID int64) string {
	m.mappingMutex.RLock()
	defer m.mappingMutex.RUnlock()

	return m.telegramUsernames[telegramID]
}

// GetTelegramUserInfo gets the Telegram user info for a Jellyseerr user
func (m *UserSyncManager) GetTelegramUserInfo(jellyseerrID int64) (telegramID int64, username string, ok bool) {
	m.mappingMutex.RLock()
	defer m.mappingMutex.RUnlock()

	telegramID, ok = m.jellyseerrToTelegram[jellyseerrID]
	if !ok {
		return 0, "", false
	}

	username = m.telegramUsernames[telegramID]
	return telegramID, username, true
}

// ============================================
// Binding Request System (Admin Approval)
// ============================================

// VerifyJellyfinCredentials verifies Jellyfin username and password
// Returns the Jellyseerr user ID if credentials are valid
func (m *UserSyncManager) VerifyJellyfinCredentials(username, password string) (int64, string, error) {
	if m.apiKey == "" {
		return 0, "", fmt.Errorf("Jellyseerr API not configured")
	}

	// Call Jellyseerr's Jellyfin auth endpoint
	url := fmt.Sprintf("%s/api/v1/auth/jellyfin", m.jellyseerrURL)

	payload := map[string]string{
		"username": username,
		"password": password,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return 0, "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return 0, "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", m.apiKey)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", fmt.Errorf("failed to read response: %w", err)
	}

	log.Printf("Jellyfin auth response: status=%d, body=%s", resp.StatusCode, string(body))

	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("authentication failed: %s", string(body))
	}

	// Parse response to get user info
	// Jellyseerr may return different formats, try multiple possibilities
	var result struct {
		UserID      int    `json:"user_id"`
		ApiKey      string `json:"apiKey"`
		AppUsername string `json:"appUsername"`
		Email       string `json:"email"`
		Id          int    `json:"id"` // Alternative field name
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, "", fmt.Errorf("failed to decode response: %w, body: %s", err, string(body))
	}

	// Check both possible field names for user ID
	userID := result.UserID
	if userID == 0 {
		userID = result.Id
	}

	if userID == 0 {
		return 0, "", fmt.Errorf("invalid user ID in response: %s", string(body))
	}

	// Get user profile to get display name
	profile, err := m.GetUserProfile(int64(userID))
	displayName := username
	if err == nil && profile.DisplayName != "" {
		displayName = profile.DisplayName
	}

	log.Printf("VerifyJellyfinCredentials: userID=%d, displayName=%s", userID, displayName)

	return int64(userID), displayName, nil
}

// CreateBindingRequest creates a new binding request awaiting admin approval
func (m *UserSyncManager) CreateBindingRequest(telegramID int64, telegramName string, jellyseerrID int64, jellyseerrName string, jellyseerrUsername string) *BindingRequest {
	m.bindingMutex.Lock()
	defer m.bindingMutex.Unlock()

	// Check for existing pending request for this user
	for _, req := range m.bindingRequests {
		if req.TelegramID == telegramID && req.Status == "pending" {
			// Update existing request
			req.JellyseerrID = jellyseerrID
			req.JellyseerrName = jellyseerrName
			req.JellyseerrUsername = jellyseerrUsername
			req.ExpiresAt = time.Now().Add(24 * time.Hour)
			m.saveBindingRequestsUnsafe()
			log.Printf("User sync: Updated existing binding request %s for Telegram %d", req.RequestID, telegramID)
			return req
		}
	}

	// Create new request
	requestID := fmt.Sprintf("bind_%d_%d", telegramID, time.Now().Unix())
	request := &BindingRequest{
		RequestID:          requestID,
		TelegramID:         telegramID,
		TelegramName:       telegramName,
		JellyseerrID:       jellyseerrID,
		JellyseerrName:     jellyseerrName,
		JellyseerrUsername: jellyseerrUsername,
		CreatedAt:          time.Now(),
		ExpiresAt:          time.Now().Add(24 * time.Hour), // 24 hours validity
		Status:             "pending",
	}

	m.bindingRequests[requestID] = request
	m.saveBindingRequestsUnsafe()

	log.Printf("User sync: Created binding request %s for Telegram %d -> Jellyseerr %d", requestID, telegramID, jellyseerrID)
	return request
}

// GetPendingBindingRequests returns all pending binding requests
func (m *UserSyncManager) GetPendingBindingRequests() []*BindingRequest {
	m.bindingMutex.RLock()
	defer m.bindingMutex.RUnlock()

	var pending []*BindingRequest
	now := time.Now()

	for _, req := range m.bindingRequests {
		if req.Status == "pending" && now.Before(req.ExpiresAt) {
			pending = append(pending, req)
		}
	}

	return pending
}

// GetBindingRequestByID retrieves a binding request by ID
func (m *UserSyncManager) GetBindingRequestByID(requestID string) *BindingRequest {
	m.bindingMutex.RLock()
	defer m.bindingMutex.RUnlock()

	return m.bindingRequests[requestID]
}

// GetBindingRequestByTelegramID retrieves pending binding request for a Telegram user
func (m *UserSyncManager) GetBindingRequestByTelegramID(telegramID int64) *BindingRequest {
	m.bindingMutex.RLock()
	defer m.bindingMutex.RUnlock()

	now := time.Now()
	for _, req := range m.bindingRequests {
		if req.TelegramID == telegramID && req.Status == "pending" && now.Before(req.ExpiresAt) {
			return req
		}
	}
	return nil
}

// ApproveBindingRequest approves a binding request and creates the mapping
func (m *UserSyncManager) ApproveBindingRequest(requestID string, adminID int64) error {
	m.bindingMutex.Lock()
	req, exists := m.bindingRequests[requestID]
	if !exists {
		m.bindingMutex.Unlock()
		return fmt.Errorf("绑定请求不存在")
	}

	if req.Status != "pending" {
		m.bindingMutex.Unlock()
		return fmt.Errorf("绑定请求已被处理")
	}

	if time.Now().After(req.ExpiresAt) {
		req.Status = "expired"
		m.saveBindingRequestsUnsafe()
		m.bindingMutex.Unlock()
		return fmt.Errorf("绑定请求已过期")
	}

	// Update request status
	now := time.Now()
	req.Status = "approved"
	req.ReviewedBy = adminID
	req.ReviewedAt = &now
	m.saveBindingRequestsUnsafe()
	m.bindingMutex.Unlock()

	// Create the mapping
	if err := m.SetUserMapping(req.TelegramID, req.JellyseerrID); err != nil {
		log.Printf("User sync: Failed to create mapping for approved request: %v", err)
		return fmt.Errorf("创建映射失败: %v", err)
	}

	log.Printf("User sync: Approved binding request %s by admin %d", requestID, adminID)
	return nil
}

// RejectBindingRequest rejects a binding request
func (m *UserSyncManager) RejectBindingRequest(requestID string, adminID int64) error {
	m.bindingMutex.Lock()
	defer m.bindingMutex.Unlock()

	req, exists := m.bindingRequests[requestID]
	if !exists {
		return fmt.Errorf("绑定请求不存在")
	}

	if req.Status != "pending" {
		return fmt.Errorf("绑定请求已被处理")
	}

	now := time.Now()
	req.Status = "rejected"
	req.ReviewedBy = adminID
	req.ReviewedAt = &now
	m.saveBindingRequestsUnsafe()

	log.Printf("User sync: Rejected binding request %s by admin %d", requestID, adminID)
	return nil
}

// CancelBindingRequest cancels a binding request (by the user themselves)
func (m *UserSyncManager) CancelBindingRequest(telegramID int64) bool {
	m.bindingMutex.Lock()
	defer m.bindingMutex.Unlock()

	now := time.Now()
	for id, req := range m.bindingRequests {
		if req.TelegramID == telegramID && req.Status == "pending" && now.Before(req.ExpiresAt) {
			req.Status = "cancelled"
			m.saveBindingRequestsUnsafe()
			log.Printf("User sync: Cancelled binding request %s by user %d", id, telegramID)
			return true
		}
	}
	return false
}

// FormatBindingRequests formats pending binding requests for display
func (m *UserSyncManager) FormatBindingRequests() string {
	pending := m.GetPendingBindingRequests()

	if len(pending) == 0 {
		return "📋 *待审核绑定请求*\n\n暂无待处理的绑定请求"
	}

	msg := "📋 *待审核绑定请求*\n\n"
	msg += fmt.Sprintf("共有 %d 个待处理请求\n\n", len(pending))

	for i, req := range pending {
		msg += fmt.Sprintf("*请求 %d*\n", i+1)
		msg += fmt.Sprintf("👤 Telegram: %s (ID: `%d`)\n", req.TelegramName, req.TelegramID)
		msg += fmt.Sprintf("🎬 Jellyseerr: %s (@%s, ID: `%d`)\n", req.JellyseerrName, req.JellyseerrUsername, req.JellyseerrID)
		msg += fmt.Sprintf("⏰ 创建时间: %s\n", req.CreatedAt.Format("2006-01-02 15:04"))
		msg += fmt.Sprintf("📝 请求ID: `%s`\n\n", req.RequestID)
	}

	msg += "💡 使用 /approvebind <请求ID> 批准绑定"
	msg += "\n💡 使用 /rejectbind <请求ID> 拒绝绑定"

	return msg
}

// FormatBindingRequestsWithButtons formats pending binding requests with inline buttons
// This is used for callback responses where buttons are needed
func (m *UserSyncManager) FormatBindingRequestsWithButtons() string {
	pending := m.GetPendingBindingRequests()

	if len(pending) == 0 {
		return "📋 *待审核绑定请求*\n\n✅ 暂无待处理的绑定请求"
	}

	msg := "📋 *待审核绑定请求*\n\n"
	msg += fmt.Sprintf("共有 %d 个待处理请求\n\n", len(pending))

	// Show up to 5 requests with buttons
	displayCount := len(pending)
	if displayCount > 5 {
		displayCount = 5
	}

	for i := 0; i < displayCount; i++ {
		req := pending[i]
		msg += fmt.Sprintf("*%d. %s*\n", i+1, req.TelegramName)
		msg += fmt.Sprintf("👤 ID: `%d`\n", req.TelegramID)
		msg += fmt.Sprintf("🎬 绑定: %s (@%s)\n", req.JellyseerrName, req.JellyseerrUsername)
		msg += fmt.Sprintf("⏰ %s\n\n", req.CreatedAt.Format("01-02 15:04"))
	}

	if len(pending) > 5 {
		msg += fmt.Sprintf("... 还有 %d 个请求\n\n", len(pending)-5)
	}

	return msg
}

// CleanupExpiredBindingRequests removes expired binding requests
func (m *UserSyncManager) CleanupExpiredBindingRequests() {
	m.bindingMutex.Lock()
	defer m.bindingMutex.Unlock()

	now := time.Now()
	cleaned := 0

	for id, req := range m.bindingRequests {
		if (req.Status == "pending" && now.After(req.ExpiresAt)) ||
			(req.Status == "approved" || req.Status == "rejected" || req.Status == "cancelled") {
			// Keep approved/rejected for 7 days for history
			if req.ReviewedAt != nil && now.Sub(*req.ReviewedAt) > 7*24*time.Hour {
				delete(m.bindingRequests, id)
				cleaned++
			} else if req.Status == "pending" && now.After(req.ExpiresAt) {
				req.Status = "expired"
				cleaned++
			}
		}
	}

	if cleaned > 0 {
		m.saveBindingRequestsUnsafe()
		log.Printf("User sync: Cleaned up %d binding requests", cleaned)
	}
}

// loadBindingRequests loads binding requests from file
func (m *UserSyncManager) loadBindingRequests() {
	data, err := os.ReadFile(m.bindingFile)
	if err != nil {
		log.Printf("User sync: Binding requests file not found, starting fresh: %v", err)
		return
	}

	var requestData BindingRequestData
	if err := json.Unmarshal(data, &requestData); err != nil {
		log.Printf("User sync: Failed to load binding requests: %v", err)
		return
	}

	m.bindingMutex.Lock()
	m.bindingRequests = requestData.Requests
	m.bindingMutex.Unlock()

	log.Printf("User sync: Loaded %d binding requests", len(m.bindingRequests))
}

// saveBindingRequests saves binding requests to file
func (m *UserSyncManager) saveBindingRequests() {
	m.bindingMutex.Lock()
	defer m.bindingMutex.Unlock()
	m.saveBindingRequestsUnsafe()
}

// saveBindingRequestsUnsafe saves binding requests without locking
func (m *UserSyncManager) saveBindingRequestsUnsafe() error {
	requestData := BindingRequestData{
		Requests: m.bindingRequests,
		LastSync: time.Now().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(requestData, "", "  ")
	if err != nil {
		log.Printf("User sync: Failed to marshal binding requests: %v", err)
		return err
	}

	if err := os.WriteFile(m.bindingFile, data, 0644); err != nil {
		log.Printf("User sync: Failed to save binding requests: %v", err)
		return err
	}

	return nil
}
