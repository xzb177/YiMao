package services

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// PreferenceType represents a user preference type
type PreferenceType string

const (
	PrefMovieNotification    PreferenceType = "movies"
	PrefTVNotification       PreferenceType = "tv"
	PrefIssueNotification   PreferenceType = "issues"
	PrefApproveNotification PreferenceType = "approved"
	PrefAvailableNotification PreferenceType = "available"
	PrefQuietMode           PreferenceType = "quiet"
	PrefQuietStart          PreferenceType = "quiet_start"
	PrefWhitelist           PreferenceType = "whitelist"
	PrefBlacklist           PreferenceType = "blacklist"
)

// UserPreferences represents user notification preferences
type UserPreferences struct {
	TelegramID int64                  `json:"telegram_id"`
	Movies     bool                   `json:"movies"`
	TV         bool                   `json:"tv"`
	Issues     bool                   `json:"issues"`
	Approved   bool                   `json:"approved"`
	Available  bool                   `json:"available"`
	QuietMode  bool                   `json:"quiet_mode"`
	QuietStart string                 `json:"quiet_start"` // HH:MM format
	Whitelist  []string               `json:"whitelist_keywords"`
	Blacklist   []string               `json:"blacklist_keywords"`
}

// PreferencesService manages user preferences
type PreferencesService struct {
	prefsFile string
	prefs     map[int64]*UserPreferences
	mu        sync.RWMutex
}

// NewPreferencesService creates a new preferences service
func NewPreferencesService(dataDir string) *PreferencesService {
	prefsFile := fmt.Sprintf("%s/preferences.json", dataDir)

	service := &PreferencesService{
		prefsFile: prefsFile,
		prefs:     make(map[int64]*UserPreferences),
	}

	service.load()

	return service
}

// load loads preferences from file
func (s *PreferencesService) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.prefsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var filePrefs struct {
		Preferences map[string]*UserPreferences `json:"preferences"`
	}

	if err := json.Unmarshal(data, &filePrefs); err != nil {
		return err
	}

	// Convert string keys to int64
	for key, pref := range filePrefs.Preferences {
		var id int64
		fmt.Sscanf(key, "%d", &id)
		pref.TelegramID = id
		s.prefs[id] = pref
	}

	log.Printf("[Preferences] Loaded %d user preferences", len(s.prefs))
	return nil
}

// save saves preferences to file
func (s *PreferencesService) save() error {
	// Convert int64 keys to strings for JSON
	filePrefs := make(map[string]*UserPreferences)
	for id, pref := range s.prefs {
		filePrefs[fmt.Sprintf("%d", id)] = pref
	}

	data, err := json.MarshalIndent(filePrefs, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.prefsFile, data, 0644)
}

// GetPreferences gets preferences for a user
func (s *PreferencesService) GetPreferences(telegramID int64) *UserPreferences {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if pref, exists := s.prefs[telegramID]; exists {
		return pref
	}

	// Return default preferences
	return &UserPreferences{
		TelegramID: telegramID,
		Movies:    true,
		TV:        true,
		Issues:    true,
		Approved:  true,
		Available: true,
		QuietMode: false,
		Whitelist: []string{},
		Blacklist: []string{},
	}
}

// SetPreference sets a preference for a user
func (s *PreferencesService) SetPreference(telegramID int64, prefType PreferenceType, value interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pref := s.GetPreferences(telegramID)

	switch prefType {
	case PrefMovieNotification:
		if v, ok := value.(bool); ok {
			pref.Movies = v
		}
	case PrefTVNotification:
		if v, ok := value.(bool); ok {
			pref.TV = v
		}
	case PrefIssueNotification:
		if v, ok := value.(bool); ok {
			pref.Issues = v
		}
	case PrefApproveNotification:
		if v, ok := value.(bool); ok {
			pref.Approved = v
		}
	case PrefAvailableNotification:
		if v, ok := value.(bool); ok {
			pref.Available = v
		}
	case PrefQuietMode:
		if v, ok := value.(bool); ok {
			pref.QuietMode = v
		}
	case PrefQuietStart:
		if v, ok := value.(string); ok {
			pref.QuietStart = v
		}
	case PrefWhitelist:
		if v, ok := value.([]string); ok {
			pref.Whitelist = v
		}
	case PrefBlacklist:
		if v, ok := value.([]string); ok {
			pref.Blacklist = v
		}
	}

	s.prefs[telegramID] = pref
	return s.save()
}

// TogglePreference toggles a boolean preference
func (s *PreferencesService) TogglePreference(telegramID int64, prefType PreferenceType) (bool, error) {
	pref := s.GetPreferences(telegramID)

	var newValue bool
	switch prefType {
	case PrefMovieNotification:
		newValue = !pref.Movies
		pref.Movies = newValue
	case PrefTVNotification:
		newValue = !pref.TV
		pref.TV = newValue
	case PrefIssueNotification:
		newValue = !pref.Issues
		pref.Issues = newValue
	case PrefApproveNotification:
		newValue = !pref.Approved
		pref.Approved = newValue
	case PrefAvailableNotification:
		newValue = !pref.Available
		pref.Available = newValue
	case PrefQuietMode:
		newValue = !pref.QuietMode
		pref.QuietMode = newValue
	default:
		return false, fmt.Errorf("unknown preference type: %s", prefType)
	}

	s.prefs[telegramID] = pref
	return newValue, s.save()
}

// ShouldNotify checks if a notification should be sent based on preferences
func (s *PreferencesService) ShouldNotify(telegramID int64, prefType PreferenceType, title string) bool {
	pref := s.GetPreferences(telegramID)

	// Check if quiet mode is enabled
	if pref.QuietMode {
		return false
	}

	// Check quiet start time
	if pref.QuietStart != "" {
		currentTime := time.Now()
		quietStart, err := time.Parse("15:04", pref.QuietStart)
		if err == nil && currentTime.Hour() < quietStart.Hour() {
			return false
		}
		// If time parsing fails, log and continue without quiet time check
		if err != nil {
			log.Printf("[Preferences] Failed to parse quiet start time '%s': %v", pref.QuietStart, err)
		}
	}

	// Check specific preference
	switch prefType {
	case PrefMovieNotification:
		if !pref.Movies {
			return false
		}
	case PrefTVNotification:
		if !pref.TV {
			return false
		}
	case PrefIssueNotification:
		if !pref.Issues {
			return false
		}
	case PrefApproveNotification:
		if !pref.Approved {
			return false
		}
	case PrefAvailableNotification:
		if !pref.Available {
			return false
		}
	}

	// Check blacklist
	for _, keyword := range pref.Blacklist {
		if contains(title, keyword) {
			return false
		}
	}

	// Check whitelist (if not empty, only allow matching)
	if len(pref.Whitelist) > 0 {
		for _, keyword := range pref.Whitelist {
			if contains(title, keyword) {
				return true
			}
		}
		return false
	}

	return true
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
