package services

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
)

// AdminService manages admin users and notifications
type AdminService struct {
	adminsFile string
	admins     map[string]string // userID -> name
	mu         sync.RWMutex
}

// Admin represents an admin user
type Admin struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
}

// NewAdminService creates a new admin service
func NewAdminService(dataDir string) *AdminService {
	adminsFile := fmt.Sprintf("%s/admins.json", dataDir)

	service := &AdminService{
		adminsFile: adminsFile,
		admins:     make(map[string]string),
	}

	service.load()

	return service
}

// load loads admins from file
func (s *AdminService) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.adminsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var fileData struct {
		Admins map[string]string `json:"admins"`
	}

	if err := json.Unmarshal(data, &fileData); err != nil {
		// Try legacy format from .env
		return s.loadFromEnv()
	}

	s.admins = fileData.Admins

	log.Printf("[AdminService] Loaded %d admins", len(s.admins))
	return nil
}

// loadFromEnv loads admins from environment variable (legacy support)
func (s *AdminService) loadFromEnv() error {
	// This is a fallback for migrations from .env
	// In production, admins should be in admins.json
	return nil
}

// save saves admins to file
func (s *AdminService) save() error {
	data, err := json.MarshalIndent(map[string]interface{}{
		"admins": s.admins,
	}, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.adminsFile, data, 0644)
}

// IsAdmin checks if a user is an admin
func (s *AdminService) IsAdmin(userID int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.admins[strconv.FormatInt(userID, 10)]
	return exists
}

// AddAdmin adds an admin
func (s *AdminService) AddAdmin(userID int64, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	userKey := strconv.FormatInt(userID, 10)
	s.admins[userKey] = name

	log.Printf("[AdminService] Added admin: %s (%s)", name, userKey)
	return s.save()
}

// RemoveAdmin removes an admin
func (s *AdminService) RemoveAdmin(userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	userKey := strconv.FormatInt(userID, 10)
	if _, exists := s.admins[userKey]; exists {
		delete(s.admins, userKey)
		log.Printf("[AdminService] Removed admin: %s", userKey)
		return s.save()
	}

	return fmt.Errorf("user %d is not an admin", userID)
}

// GetAllAdmins returns all admins
func (s *AdminService) GetAllAdmins() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]string)
	for k, v := range s.admins {
		result[k] = v
	}

	return result
}

// GetAdminIDs returns all admin user IDs as int64
func (s *AdminService) GetAdminIDs() []int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var ids []int64
	for idStr := range s.admins {
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}

	return ids
}

// GetAdminCount returns the number of admins
func (s *AdminService) GetAdminCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.admins)
}

// HasAdmins checks if there are any admins configured
func (s *AdminService) HasAdmins() bool {
	return s.GetAdminCount() > 0
}
