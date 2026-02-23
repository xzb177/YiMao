package services

import (
	"encoding/json"
	"fmt"
	"log"
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
}

// BindingRequest represents a pending binding request
type BindingRequest struct {
	RequestID        string    `json:"request_id"`
	TelegramID       int64     `json:"telegram_id"`
	TelegramName     string    `json:"telegram_name"`
	TelegramUsername string    `json:"telegram_username"`
	MoviePilotID     int64     `json:"moviepilot_id"`
	MoviePilotName   string    `json:"moviepilot_name"`
	MoviePilotUsername string `json:"moviepilot_username"`
	// Legacy fields for compatibility
	JellyseerrID        int64     `json:"jellyseerr_id,omitempty"`
	JellyseerrName      string    `json:"jellyseerr_name,omitempty"`
	JellyseerrUsername  string    `json:"jellyseerr_username,omitempty"`
	CreatedAt           string    `json:"created_at"`
	ExpiresAt           string    `json:"expires_at"`
	Status              string    `json:"status"` // pending, approved, rejected
}

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
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.mappingsFile)
	if err != nil {
		if os.IsNotExist(err) {
			// Create empty file
			s.save()
			return nil
		}
		return err
	}

	// Try new format first
	var fileData struct {
		UserMappings     map[string]int64  `json:"user_mappings"`
		Usernames        map[string]string `json:"usernames"`
		ReverseMappings map[int64]string  `json:"reverse_mappings"`
	}
	if err := json.Unmarshal(data, &fileData); err == nil {
		s.mappings = fileData.UserMappings
		s.usernames = fileData.Usernames
		s.reverseMap = fileData.ReverseMappings
		log.Printf("[UserMapping] Loaded %d user mappings", len(s.mappings))
		return nil
	}

	// Try legacy format (direct mappings)
	var legacyData map[string]int64
	if err := json.Unmarshal(data, &legacyData); err == nil {
		s.mappings = legacyData
		log.Printf("[UserMapping] Loaded %d user mappings (legacy format)", len(s.mappings))
		return nil
	}

	log.Printf("[UserMapping] Failed to load user mappings: %v", err)
	return nil
}

// save saves user mappings to file
func (s *UserMappingService) save() error {
	data := map[string]interface{}{
		"user_mappings":  s.mappings,
		"usernames":      s.usernames,
		"reverse_mappings": s.reverseMap,
	}
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.mappingsFile, jsonData, 0644)
}

// GetJellyseerrUserID gets Jellyseerr user ID for a Telegram user
// Deprecated: Use GetMoviePilotUserID instead
func (s *UserMappingService) GetJellyseerrUserID(telegramID int64) (int64, bool) {
	return s.GetMoviePilotUserID(telegramID)
}

// GetMoviePilotUserID gets MoviePilot user ID for a Telegram user
func (s *UserMappingService) GetMoviePilotUserID(telegramID int64) (int64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	moviepilotID, exists := s.mappings[fmt.Sprintf("%d", telegramID)]
	return moviepilotID, exists
}

// GetTelegramUsername gets Telegram username for a user
func (s *UserMappingService) GetTelegramUsername(telegramID int64) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.usernames[fmt.Sprintf("%d", telegramID)]
}

// SetTelegramUsername sets the Telegram username for a user
func (s *UserMappingService) SetTelegramUsername(telegramID int64, username string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.usernames[fmt.Sprintf("%d", telegramID)] = username
	s.save()
}

// AddMapping adds a user mapping
func (s *UserMappingService) AddMapping(telegramID int64, jellyseerrID int64, jellyseerrUsername string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	telegramKey := fmt.Sprintf("%d", telegramID)
	s.mappings[telegramKey] = jellyseerrID

	// Update reverse mapping
	s.reverseMap[jellyseerrID] = telegramKey

	if jellyseerrUsername != "" {
		s.usernames[telegramKey] = jellyseerrUsername
	}

	return s.save()
}

// RemoveMapping removes a user mapping
func (s *UserMappingService) RemoveMapping(telegramID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	telegramKey := fmt.Sprintf("%d", telegramID)
	moviePilotID, exists := s.mappings[telegramKey]
	if !exists {
		return fmt.Errorf("no mapping found for telegram ID %d", telegramID)
	}

	// Remove from reverse map
	delete(s.reverseMap, moviePilotID)

	delete(s.mappings, telegramKey)
	delete(s.usernames, telegramKey)

	return s.save()
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

	log.Printf("[BindingRequest] Loaded %d binding requests", len(s.requests))
	return nil
}

// save saves binding requests to file
func (s *BindingRequestService) save() error {
	data, err := json.MarshalIndent(s.requests, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.requestsFile, data, 0644)
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
func (s *BindingRequestService) ApproveRequest(requestID string, userMappingService *UserMappingService) error {
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
			createdAt, _ := strconv.ParseInt(req.CreatedAt, 10, 64)
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
