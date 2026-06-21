package services

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/xzb177/yimao/pkg/logger"
)

// AdminRole represents the role of an admin user
type AdminRole string

const (
	// AdminRoleRoot is the super admin who can manage other admins
	AdminRoleRoot AdminRole = "root"
	// AdminRoleNormal is a regular admin who can approve requests
	AdminRoleNormal AdminRole = "normal"
)

// AdminInfo represents an admin user with role
type AdminInfo struct {
	UserID int64     `json:"user_id"`
	Name   string    `json:"name"`
	Role   AdminRole `json:"role"`
}

// AdminService manages admin users and notifications
type AdminService struct {
	adminsFile string
	admins     map[int64]*AdminInfo // userID -> AdminInfo
	rootUserID int64                // The first/root admin
	mu         sync.RWMutex
}

// Admin represents an admin user (legacy format for backward compatibility)
type Admin struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
}

// NewAdminService creates a new admin service
func NewAdminService(dataDir string) *AdminService {
	adminsFile := fmt.Sprintf("%s/admins.json", dataDir)

	service := &AdminService{
		adminsFile: adminsFile,
		admins:     make(map[int64]*AdminInfo),
	}

	service.load()
	service.loadFromEnv() // Also load from ADMIN_USER_IDS env var

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

	// Try new format first with AdminInfo
	var fileData struct {
		Admins map[int64]*AdminInfo `json:"admins"`
		RootID int64                `json:"root_id"`
	}

	if err := json.Unmarshal(data, &fileData); err == nil && len(fileData.Admins) > 0 {
		s.admins = fileData.Admins
		s.rootUserID = fileData.RootID
		logger.Info("[AdminService] Loaded %d admins (new format), root=%d", len(s.admins), s.rootUserID)
		return nil
	}

	// Try legacy format: {"admins": {"123": "Name"}}
	var legacyData struct {
		Admins map[string]string `json:"admins"`
	}

	if err := json.Unmarshal(data, &legacyData); err == nil && len(legacyData.Admins) > 0 {
		s.admins = make(map[int64]*AdminInfo)
		firstID := int64(0)
		for idStr, name := range legacyData.Admins {
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				logger.Info("[AdminService] Invalid admin ID '%s' in legacy data, skipping", idStr)
				continue
			}
			s.admins[id] = &AdminInfo{
				UserID: id,
				Name:   name,
				Role:   AdminRoleNormal, // Default to normal
			}
			if firstID == 0 {
				firstID = id
			}
		}
		// First admin becomes root
		if firstID > 0 {
			s.rootUserID = firstID
			s.admins[firstID].Role = AdminRoleRoot
		}
		s.save() // Save in new format
		logger.Info("[AdminService] Migrated %d admins from legacy format, root=%d", len(s.admins), s.rootUserID)
		return nil
	}

	return nil
}

// loadFromEnv loads admins from ADMIN_USER_IDS environment variable.
// Supports comma-separated IDs. First ID becomes root admin.
func (s *AdminService) loadFromEnv() error {
	envVal := os.Getenv("ADMIN_USER_IDS")
	if envVal == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dirty := false
	for _, idStr := range strings.Split(envVal, ",") {
		idStr = strings.TrimSpace(idStr)
		if idStr == "" {
			continue
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		if _, exists := s.admins[id]; !exists {
			role := AdminRoleNormal
			if len(s.admins) == 0 {
				role = AdminRoleRoot
				s.rootUserID = id
			}
			s.admins[id] = &AdminInfo{
				UserID: id,
				Name:   "Admin",
				Role:   role,
			}
			logger.Info("[AdminService] Loaded admin from env: %d (role=%s)", id, role)
			dirty = true
		}
	}

	// Save to file for persistence (within lock scope)
	if dirty && len(s.admins) > 0 {
		s.save()
	}

	return nil
}

// save saves admins to file
func (s *AdminService) save() error {
	data, err := json.MarshalIndent(map[string]interface{}{
		"admins":  s.admins,
		"root_id": s.rootUserID,
	}, "", "  ")
	if err != nil {
		return err
	}

	return atomicWriteFile(s.adminsFile, data, 0600)
}

// IsAdmin checks if a user is an admin
func (s *AdminService) IsAdmin(userID int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.admins[userID]
	return exists
}

// IsRootAdmin checks if a user is the root/super admin
func (s *AdminService) IsRootAdmin(userID int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if admin, exists := s.admins[userID]; exists {
		return admin.Role == AdminRoleRoot
	}
	return false
}

// GetRootAdminID returns the root admin's user ID
func (s *AdminService) GetRootAdminID() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.rootUserID
}

// SetRootAdmin sets a user as the root admin
func (s *AdminService) SetRootAdmin(userID int64, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, exists := s.admins[userID]; exists {
		existing.Role = AdminRoleRoot
	} else {
		s.admins[userID] = &AdminInfo{
			UserID: userID,
			Name:   name,
			Role:   AdminRoleRoot,
		}
	}
	s.rootUserID = userID

	logger.Info("[AdminService] Set root admin: %s (%d)", name, userID)
	return s.save()
}

// AddAdmin adds an admin
func (s *AdminService) AddAdmin(userID int64, name string) error {
	return s.AddAdminWithRole(userID, name, AdminRoleNormal)
}

// AddAdminWithRole adds an admin with a specific role
func (s *AdminService) AddAdminWithRole(userID int64, name string, role AdminRole) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, exists := s.admins[userID]; exists {
		// Don't change role if already exists
		existing.Name = name
	} else {
		s.admins[userID] = &AdminInfo{
			UserID: userID,
			Name:   name,
			Role:   role,
		}
		// Only auto-promote to root when it's the very first admin in the entire
		// system AND the caller did not explicitly request a specific role.
		// This ensures ADMIN_USER_IDS / existing file admins keep root control.
		if role == AdminRoleNormal && len(s.admins) == 1 && s.rootUserID == 0 {
			s.admins[userID].Role = AdminRoleRoot
			s.rootUserID = userID
		}
	}

	logger.Info("[AdminService] Added admin: %s (%d, role=%s)", name, userID, role)
	return s.save()
}

// RemoveAdmin removes an admin (cannot remove root admin)
func (s *AdminService) RemoveAdmin(userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if admin, exists := s.admins[userID]; exists {
		if admin.Role == AdminRoleRoot {
			return fmt.Errorf("cannot remove root admin")
		}
		delete(s.admins, userID)
		logger.Info("[AdminService] Removed admin: %d", userID)
		return s.save()
	}

	return fmt.Errorf("user %d is not an admin", userID)
}

// GetAllAdmins returns all admins
func (s *AdminService) GetAllAdmins() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]string)
	for _, admin := range s.admins {
		roleMark := ""
		if admin.Role == AdminRoleRoot {
			roleMark = "👑 "
		}
		result[strconv.FormatInt(admin.UserID, 10)] = roleMark + admin.Name
	}

	return result
}

// GetAllAdminInfo returns all admin info with details
func (s *AdminService) GetAllAdminInfo() []*AdminInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*AdminInfo, 0, len(s.admins))
	for _, admin := range s.admins {
		// Return a copy to avoid race conditions
		adminCopy := *admin
		result = append(result, &adminCopy)
	}

	return result
}

// GetAdminName returns the admin's name
func (s *AdminService) GetAdminName(userID int64) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if admin, exists := s.admins[userID]; exists {
		return admin.Name
	}
	return ""
}

// GetAdminIDs returns all admin user IDs as int64
func (s *AdminService) GetAdminIDs() []int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var ids []int64
	for id := range s.admins {
		ids = append(ids, id)
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
