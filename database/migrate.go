package database

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// Migrator handles migration from JSON files to SQLite database
type Migrator struct {
	userDB *UserDB
}

// NewMigrator creates a new migrator
func NewMigrator(userDB *UserDB) *Migrator {
	return &Migrator{userDB: userDB}
}

// MigrateFromJSON migrates data from JSON files to the database
func (m *Migrator) MigrateFromJSON(dataDir string) error {
	log.Printf("[Migration] Starting migration from %s", dataDir)

	// Migrate user quotas
	if err := m.migrateQuotas(filepath.Join(dataDir, "user_quotas.json")); err != nil {
		log.Printf("[Migration] Error migrating quotas: %v", err)
	}

	// Migrate user mappings
	if err := m.migrateUserMappings(filepath.Join(dataDir, "user_mapping.json")); err != nil {
		log.Printf("[Migration] Error migrating user mappings: %v", err)
	}

	// Migrate UserSyncManager format
	if err := m.migrateUserSyncMappings(filepath.Join(dataDir, "user_mappings.json")); err != nil {
		log.Printf("[Migration] Error migrating user sync mappings: %v", err)
	}

	// Migrate admins
	if err := m.migrateAdmins(filepath.Join(dataDir, "admins.json")); err != nil {
		log.Printf("[Migration] Error migrating admins: %v", err)
	}

	// Migrate analytics (requests)
	if err := m.migrateAnalytics(filepath.Join(dataDir, "analytics.json")); err != nil {
		log.Printf("[Migration] Error migrating analytics: %v", err)
	}

	log.Printf("[Migration] Migration completed")
	return nil
}

// migrateQuotas migrates user quota data
func (m *Migrator) migrateQuotas(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist, skip
		}
		return err
	}

	var quotas map[string]struct {
		MovieLimit int `json:"movieLimit"`
		MovieUsed  int `json:"movieUsed"`
		TVLimit    int `json:"tvLimit"`
		TVUsed     int `json:"tvUsed"`
	}

	if err := json.Unmarshal(data, &quotas); err != nil {
		return err
	}

	count := 0
	for userIDStr, quota := range quotas {
		var telegramID int64
		if _, err := fmt.Sscanf(userIDStr, "%d", &telegramID); err != nil {
			continue
		}

		user, err := m.userDB.GetUser(telegramID)
		if err != nil {
			// Create new user
			user = &UserData{
				TelegramID:      telegramID,
				MovieQuotaLimit: quota.MovieLimit,
				MovieQuotaUsed:  quota.MovieUsed,
				TVQuotaLimit:    quota.TVLimit,
				TVQuotaUsed:     quota.TVUsed,
				LastActiveAt:    time.Now(),
			}
		} else {
			// Update existing user
			user.MovieQuotaLimit = quota.MovieLimit
			user.MovieQuotaUsed = quota.MovieUsed
			user.TVQuotaLimit = quota.TVLimit
			user.TVQuotaUsed = quota.TVUsed
		}

		if err := m.userDB.UpsertUser(user); err != nil {
			log.Printf("[Migration] Error upserting user %d: %v", telegramID, err)
			continue
		}
		count++
	}

	log.Printf("[Migration] Migrated %d quota records", count)
	return nil
}

// migrateUserMappings migrates legacy user mapping format
func (m *Migrator) migrateUserMappings(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var mappings map[string]struct {
		JellyseerrID   int    `json:"jellyseerr_id"`
		JellyseerrName string `json:"jellyseerr_name"`
	}

	if err := json.Unmarshal(data, &mappings); err != nil {
		return err
	}

	count := 0
	for userIDStr, mapping := range mappings {
		var telegramID int64
		if _, err := fmt.Sscanf(userIDStr, "%d", &telegramID); err != nil {
			continue
		}

		user, err := m.userDB.GetUser(telegramID)
		if err != nil {
			user = &UserData{
				TelegramID:   telegramID,
				LastActiveAt: time.Now(),
			}
		}

		user.JellyseerrID = mapping.JellyseerrID
		user.JellyseerrName = mapping.JellyseerrName

		if err := m.userDB.UpsertUser(user); err != nil {
			log.Printf("[Migration] Error upserting user mapping %d: %v", telegramID, err)
			continue
		}
		count++
	}

	log.Printf("[Migration] Migrated %d user mapping records", count)
	return nil
}

// migrateUserSyncMappings migrates UserSyncManager format
func (m *Migrator) migrateUserSyncMappings(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var mappings struct {
		TelegramToJellyseerr map[string]int `json:"telegramToJellyseerr"`
		JellyseerrToTelegram map[string]int `json:"jellyseerrToTelegram"`
	}

	if err := json.Unmarshal(data, &mappings); err != nil {
		return err
	}

	count := 0
	for telegramIDStr, jellyseerrID := range mappings.TelegramToJellyseerr {
		var telegramID int64
		if _, err := fmt.Sscanf(telegramIDStr, "%d", &telegramID); err != nil {
			continue
		}

		user, err := m.userDB.GetUser(telegramID)
		if err != nil {
			user = &UserData{
				TelegramID:   telegramID,
				LastActiveAt: time.Now(),
			}
		}

		user.JellyseerrID = jellyseerrID

		if err := m.userDB.UpsertUser(user); err != nil {
			log.Printf("[Migration] Error upserting user sync mapping %d: %v", telegramID, err)
			continue
		}
		count++
	}

	log.Printf("[Migration] Migrated %d user sync mapping records", count)
	return nil
}

// migrateAdmins migrates admin data
func (m *Migrator) migrateAdmins(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var admins map[string]string // userID -> name

	if err := json.Unmarshal(data, &admins); err != nil {
		return err
	}

	count := 0
	for userIDStr, name := range admins {
		var telegramID int64
		if _, err := fmt.Sscanf(userIDStr, "%d", &telegramID); err != nil {
			continue
		}

		user, err := m.userDB.GetUser(telegramID)
		if err != nil {
			user = &UserData{
				TelegramID:   telegramID,
				UserName:     name,
				IsAdmin:      true,
				LastActiveAt: time.Now(),
			}
		} else {
			user.IsAdmin = true
			if user.UserName == "" {
				user.UserName = name
			}
		}

		if err := m.userDB.UpsertUser(user); err != nil {
			log.Printf("[Migration] Error upserting admin %d: %v", telegramID, err)
			continue
		}
		count++
	}

	log.Printf("[Migration] Migrated %d admin records", count)
	return nil
}

// migrateAnalytics migrates analytics request data
func (m *Migrator) migrateAnalytics(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var analytics struct {
		Requests []struct {
			UserID     string    `json:"userId"`
			MediaTitle string    `json:"mediaTitle"`
			MediaID    int       `json:"mediaId"`
			MediaType  string    `json:"mediaType"`
			Status     string    `json:"status"`
			Timestamp  time.Time `json:"timestamp"`
		} `json:"requests"`
	}

	if err := json.Unmarshal(data, &analytics); err != nil {
		return err
	}

	count := 0
	for _, req := range analytics.Requests {
		var telegramID int64
		if _, err := fmt.Sscanf(req.UserID, "%d", &telegramID); err != nil {
			continue
		}

		// Check if request already exists
		existingReqs, _ := m.userDB.GetRequests(telegramID)
		exists := false
		for _, e := range existingReqs {
			if e.MediaID == req.MediaID && e.CreatedAt.Format("2006-01-02") == req.Timestamp.Format("2006-01-02") {
				exists = true
				break
			}
		}

		if exists {
			continue
		}

		request := &UserRequest{
			TelegramID: telegramID,
			MediaID:    req.MediaID,
			MediaTitle: req.MediaTitle,
			MediaType:  req.MediaType,
			TmdbID:     req.MediaID,
			Status:     req.Status,
			CreatedAt:  req.Timestamp,
		}

		if err := m.userDB.CreateRequest(request); err != nil {
			log.Printf("[Migration] Error creating request: %v", err)
			continue
		}
		count++
	}

	log.Printf("[Migration] Migrated %d request records", count)
	return nil
}
