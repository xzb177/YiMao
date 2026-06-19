package services

import (
	"encoding/json"
	"fmt"
	"github.com/xzb177/yimao/pkg/logger"
	"os"
	"strconv"
	"sync"
	"time"
)

// UserMappingService handles user mappings between Telegram and MoviePilot
type UserMappingService struct {
	mappingsFile string
	mappings     map[string]int64  // telegramID -> moviepilotID
	usernames    map[string]string // telegramID -> moviepilotUsername
	reverseMap   map[int64]string  // moviepilotID -> telegramID
	mu           sync.RWMutex
	dirty        bool        // Track if data needs saving
	savePending  bool        // Prevent multiple concurrent saves
	lastSave     time.Time   // Last save time
	saveTimer    *time.Timer // Single timer for delayed saves
	saveMu       sync.Mutex  // Protects saveTimer creation
}

// BindingRequest represents a pending binding request
type BindingRequest struct {
	RequestID          string `json:"request_id"`
	TelegramID         int64  `json:"telegram_id"`
	TelegramName       string `json:"telegram_name"`
	TelegramUsername   string `json:"telegram_username"`
	MoviePilotID       int64  `json:"moviepilot_id"`
	MoviePilotName     string `json:"moviepilot_name"`
	MoviePilotUsername string `json:"moviepilot_username"`
	// Legacy fields for compatibility
	JellyseerrID       int64  `json:"jellyseerr_id,omitempty"`
	JellyseerrName     string `json:"jellyseerr_name,omitempty"`
	JellyseerrUsername string `json:"jellyseerr_username,omitempty"`
	CreatedAt          string `json:"created_at"`
	ExpiresAt          string `json:"expires_at"`
	Status             string `json:"status"` // pending, approved, rejected
}

const saveDelay = 5 * time.Second // Delay before saving dirty data

// NewUserMappingService creates a new user mapping service
func NewUserMappingService(dataDir string) *UserMappingService {
	mappingsFile := fmt.Sprintf("%s/user_mappings.json", dataDir)

	service := &UserMappingService{
		mappingsFile: mappingsFile,
		mappings:     make(map[string]int64),
		usernames:    make(map[string]string),
		reverseMap:   make(map[int64]string),
	}

	service.load()

	return service
}

// load loads user mappings from file
func (s *UserMappingService) load() error {
	logger.Info("[UserMapping] load: acquiring lock...")
	s.mu.Lock()
	logger.Info("[UserMapping] load: lock acquired, reading file: %s", s.mappingsFile)

	data, err := os.ReadFile(s.mappingsFile)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Info("[UserMapping] File not exist, creating empty file")
			// Create empty file - use saveLocked since we already hold the lock
			if saveErr := s.saveLocked(); saveErr != nil {
				logger.Info("[UserMapping] ERROR creating file: %v", saveErr)
				s.mu.Unlock()
				return saveErr
			}
			s.mu.Unlock()
			return nil
		}
		logger.Info("[UserMapping] ERROR reading file: %v", err)
		s.mu.Unlock()
		return err
	}
	logger.Info("[UserMapping] File read: %d bytes", len(data))

	// Try new format first
	var fileData struct {
		UserMappings    map[string]int64  `json:"user_mappings"`
		Usernames       map[string]string `json:"usernames"`
		ReverseMappings map[int64]string  `json:"reverse_mappings"`
	}
	if err := json.Unmarshal(data, &fileData); err == nil {
		s.mappings = fileData.UserMappings
		s.usernames = fileData.Usernames
		s.reverseMap = fileData.ReverseMappings
		logger.Info("[UserMapping] Loaded %d user mappings (new format)", len(s.mappings))
		s.mu.Unlock()
		return nil
	}

	// Try alternative format with "mappings" key
	var altData struct {
		Mappings map[string]int64 `json:"mappings"`
	}
	if err := json.Unmarshal(data, &altData); err == nil && altData.Mappings != nil {
		s.mappings = altData.Mappings
		logger.Info("[UserMapping] Loaded %d user mappings (alt format with 'mappings' key)", len(s.mappings))
		s.mu.Unlock()
		return nil
	}

	// Try legacy format (direct mappings)
	var legacyData map[string]int64
	if err := json.Unmarshal(data, &legacyData); err == nil {
		s.mappings = legacyData
		logger.Info("[UserMapping] Loaded %d user mappings (legacy format)", len(s.mappings))
		s.mu.Unlock()
		return nil
	}

	logger.Info("[UserMapping] Failed to load user mappings: %v", err)
	s.mu.Unlock()
	return nil
}

// save saves user mappings to file
func (s *UserMappingService) save() error {
	s.mu.Lock()
	// Copy data under lock
	data := map[string]interface{}{
		"user_mappings":    s.mappings,
		"usernames":        s.usernames,
		"reverse_mappings": s.reverseMap,
	}
	s.dirty = false
	s.lastSave = time.Now()
	s.mu.Unlock()

	// Marshal and write file without holding lock
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	logger.Info("[UserMapping] Writing to file: %s (%d bytes)", s.mappingsFile, len(jsonData))
	writeErr := atomicWriteFile(s.mappingsFile, jsonData, 0644)
	if writeErr != nil {
		logger.Info("[UserMapping] ERROR writing file: %v", writeErr)
	} else {
		logger.Info("[UserMapping] File saved successfully")
	}

	return writeErr
}

// saveLocked saves user mappings to file (must be called with lock held)
// Deprecated: Use save() instead
func (s *UserMappingService) saveLocked() error {
	// This method is kept for compatibility but delegates to save()
	// We need to release the lock first since save() will acquire it
	s.mu.Unlock()
	defer s.mu.Lock()
	return s.save()
}

// scheduleSave schedules a delayed save if data is dirty
func (s *UserMappingService) scheduleSave() {
	s.mu.Lock()
	s.dirty = true
	s.mu.Unlock()

	// Use a mutex-protected timer to prevent goroutine proliferation
	s.saveMu.Lock()
	defer s.saveMu.Unlock()

	// Stop existing timer if any (this is safe even if timer already fired)
	if s.saveTimer != nil {
		s.saveTimer.Stop()
	}

	// Create new timer for delayed save
	s.saveTimer = time.AfterFunc(saveDelay, func() {
		s.mu.Lock()
		if s.dirty && !s.savePending {
			s.savePending = true
			s.mu.Unlock()
			if err := s.save(); err != nil {
				logger.Info("[UserMapping] Failed to save: %v", err)
			}
			s.mu.Lock()
			s.savePending = false
		}
		s.mu.Unlock()
	})
}

// ForceSave immediately saves the data to disk
func (s *UserMappingService) ForceSave() error {
	return s.save()
}

// GetJellyseerrUserID gets Jellyseerr user ID for a Telegram user
// Deprecated: Use GetMoviePilotUserID instead
func (s *UserMappingService) GetJellyseerrUserID(telegramID int64) (int64, bool) {
	return s.GetMoviePilotUserID(telegramID)
}

// GetMoviePilotUserID gets MoviePilot user ID for a Telegram user
func (s *UserMappingService) GetMoviePilotUserID(telegramID int64) (int64, bool) {
	logger.Info("[UserMapping] GetMoviePilotUserID: acquiring lock for telegramID=%d", telegramID)
	s.mu.RLock()
	logger.Info("[UserMapping] GetMoviePilotUserID: lock acquired for telegramID=%d", telegramID)
	defer func() {
		s.mu.RUnlock()
		logger.Info("[UserMapping] GetMoviePilotUserID: lock released for telegramID=%d", telegramID)
	}()

	moviepilotID, exists := s.mappings[fmt.Sprintf("%d", telegramID)]
	// Treat ID 0 as invalid/non-existent
	if exists && moviepilotID == 0 {
		return 0, false
	}
	return moviepilotID, exists
}

// GetTelegramUsername gets Telegram username for a user
func (s *UserMappingService) GetTelegramUsername(telegramID int64) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.usernames[fmt.Sprintf("%d", telegramID)]
}

// GetMoviePilotUsername gets the MoviePilot username for a Telegram user
func (s *UserMappingService) GetMoviePilotUsername(telegramID int64) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	name, ok := s.usernames[fmt.Sprintf("%d", telegramID)]
	if !ok || name == "" {
		return "", fmt.Errorf("用户未绑定 MoviePilot 账号")
	}
	return name, nil
}

// SetTelegramUsername sets the Telegram username for a user
func (s *UserMappingService) SetTelegramUsername(telegramID int64, username string) {
	s.mu.Lock()

	s.usernames[fmt.Sprintf("%d", telegramID)] = username

	// Mark as dirty before releasing lock
	s.dirty = true

	// Release lock before calling scheduleSave to avoid deadlock
	s.mu.Unlock()

	// Trigger async save without holding lock
	s.scheduleSave()
}

// AddMapping adds a user mapping
func (s *UserMappingService) AddMapping(telegramID int64, jellyseerrID int64, jellyseerrUsername string) error {
	s.mu.Lock()

	telegramKey := fmt.Sprintf("%d", telegramID)
	if owner, exists := s.reverseMap[jellyseerrID]; exists && owner != telegramKey {
		s.mu.Unlock()
		return fmt.Errorf("MoviePilot 用户 ID %d 已绑定其他 Telegram 用户", jellyseerrID)
	}
	if jellyseerrUsername != "" {
		for existingTelegram, existingUsername := range s.usernames {
			if existingUsername == jellyseerrUsername && existingTelegram != telegramKey {
				s.mu.Unlock()
				return fmt.Errorf("MoviePilot 用户名 %s 已绑定其他 Telegram 用户", jellyseerrUsername)
			}
		}
	}
	s.mappings[telegramKey] = jellyseerrID

	// Update reverse mapping
	s.reverseMap[jellyseerrID] = telegramKey

	if jellyseerrUsername != "" {
		s.usernames[telegramKey] = jellyseerrUsername
	}

	// Mark as dirty before releasing lock
	s.dirty = true

	// Release lock before calling scheduleSave to avoid deadlock
	s.mu.Unlock()

	// Trigger async save without holding lock
	s.scheduleSave()

	return nil
}

// RemoveMapping removes a user mapping
func (s *UserMappingService) RemoveMapping(telegramID int64) error {
	s.mu.Lock()

	telegramKey := fmt.Sprintf("%d", telegramID)
	moviePilotID, exists := s.mappings[telegramKey]
	if !exists {
		s.mu.Unlock()
		return fmt.Errorf("no mapping found for telegram ID %d", telegramID)
	}

	// Remove from reverse map
	delete(s.reverseMap, moviePilotID)

	delete(s.mappings, telegramKey)
	delete(s.usernames, telegramKey)

	// Mark as dirty before releasing lock
	s.dirty = true

	// Release lock before calling scheduleSave to avoid deadlock
	s.mu.Unlock()

	// Trigger async save without holding lock
	s.scheduleSave()

	return nil
}

// GetTelegramIDByJellyseerrID gets Telegram ID by Jellyseerr ID
func (s *UserMappingService) GetTelegramIDByJellyseerrID(jellyseerrID int64) (int64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	telegramKey, exists := s.reverseMap[jellyseerrID]
	if !exists {
		return 0, false
	}

	var telegramID int64
	fmt.Sscanf(telegramKey, "%d", &telegramID)
	return telegramID, true
}

// GetTelegramIDByMoviePilotUsername gets Telegram ID by MoviePilot username
func (s *UserMappingService) GetTelegramIDByMoviePilotUsername(username string) (int64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Search through usernames map to find matching MoviePilot username
	for telegramKey, mpUsername := range s.usernames {
		if mpUsername == username {
			var telegramID int64
			fmt.Sscanf(telegramKey, "%d", &telegramID)
			return telegramID, true
		}
	}
	return 0, false
}

// BindingRequestService handles binding requests
type BindingRequestService struct {
	requestsFile string
	requests     map[string]*BindingRequest
	mu           sync.RWMutex
}

// NewBindingRequestService creates a new binding request service
func NewBindingRequestService(dataDir string) *BindingRequestService {
	requestsFile := fmt.Sprintf("%s/binding_requests.json", dataDir)

	service := &BindingRequestService{
		requestsFile: requestsFile,
		requests:     make(map[string]*BindingRequest),
	}

	service.load()

	return service
}

// load loads binding requests from file
func (s *BindingRequestService) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.requestsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if err := json.Unmarshal(data, &s.requests); err != nil {
		// Try legacy format
		var legacyRequests map[string]*BindingRequest
		if err := json.Unmarshal(data, &legacyRequests); err == nil {
			s.requests = legacyRequests
		}
	}

	logger.Info("[BindingRequest] Loaded %d binding requests", len(s.requests))
	return nil
}

// save saves binding requests to file
func (s *BindingRequestService) save() error {
	data, err := json.MarshalIndent(s.requests, "", "  ")
	if err != nil {
		return err
	}

	return atomicWriteFile(s.requestsFile, data, 0644)
}

// CreateRequest creates a new binding request
func (s *BindingRequestService) CreateRequest(req *BindingRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	req.Status = "pending"
	req.CreatedAt = fmt.Sprintf("%d", time.Now().Unix())

	s.requests[req.RequestID] = req

	return s.save()
}

// GetRequest gets a binding request by ID
func (s *BindingRequestService) GetRequest(requestID string) (*BindingRequest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	req, exists := s.requests[requestID]
	return req, exists
}

// GetAllPendingRequests gets all pending requests
func (s *BindingRequestService) GetAllPendingRequests() []*BindingRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var pending []*BindingRequest
	for _, req := range s.requests {
		if req.Status == "pending" {
			pending = append(pending, req)
		}
	}

	return pending
}

// UpdateRequestStatus updates the status of a binding request
func (s *BindingRequestService) UpdateRequestStatus(requestID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, exists := s.requests[requestID]; exists {
		req.Status = status
		return s.save()
	}

	return fmt.Errorf("request not found: %s", requestID)
}

// ApproveRequest approves a binding request
func (s *BindingRequestService) ApproveRequest(requestID string, userMappingService UserMappingStore) error {
	s.mu.RLock()
	req, exists := s.requests[requestID]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("request not found: %s", requestID)
	}

	// Use MoviePilotID preferentially, fallback to JellyseerrID for compatibility
	moviepilotID := req.MoviePilotID
	if moviepilotID == 0 {
		moviepilotID = req.JellyseerrID
	}
	moviepilotUsername := req.MoviePilotUsername
	if moviepilotUsername == "" {
		moviepilotUsername = req.JellyseerrUsername
	}

	// Add user mapping
	if err := userMappingService.AddMapping(req.TelegramID, moviepilotID, moviepilotUsername); err != nil {
		return err
	}

	// Update request status
	return s.UpdateRequestStatus(requestID, "approved")
}

// RejectRequest rejects a binding request
func (s *BindingRequestService) RejectRequest(requestID string) error {
	return s.UpdateRequestStatus(requestID, "rejected")
}

// CleanupExpiredRequests removes requests older than 24 hours
func (s *BindingRequestService) CleanupExpiredRequests() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	expiry := 24 * 60 * 60 // 24 hours in seconds
	now := int64(time.Now().Unix())

	removed := 0
	for id, req := range s.requests {
		if req.Status == "pending" {
			createdAt, err := strconv.ParseInt(req.CreatedAt, 10, 64)
			if err != nil {
				logger.Info("[UserMapping] Failed to parse CreatedAt for request %s: %v", id, err)
				continue // Skip this request if timestamp is invalid
			}
			age := now - createdAt
			if int(age) > expiry {
				delete(s.requests, id)
				removed++
			}
		}
	}

	if removed > 0 {
		s.save()
	}

	return removed
}

// GetAllTelegramUsers returns all Telegram user IDs that have mappings
func (s *UserMappingService) GetAllTelegramUsers() []int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var users []int64
	for telegramKey := range s.mappings {
		var telegramID int64
		fmt.Sscanf(telegramKey, "%d", &telegramID)
		users = append(users, telegramID)
	}
	return users
}

// GetAllMappings returns all user mappings
func (s *UserMappingService) GetAllMappings() map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return a copy to avoid race conditions
	result := make(map[string]int64, len(s.mappings))
	for k, v := range s.mappings {
		result[k] = v
	}
	return result
}
