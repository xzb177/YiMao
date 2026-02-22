package services

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// UserQuota represents a user's quota usage
type UserQuota struct {
	TelegramID      int64     `json:"telegram_id"`
	MoviePilotID    int64     `json:"moviepilot_id"`
	JellyseerrID    int64     `json:"jellyseerr_id,omitempty"` // Legacy field for compatibility
	MovieUsed       int       `json:"movie_used"`
	MovieLimit      int       `json:"movie_limit"`
	TVUsed          int       `json:"tv_used"`
	TVLimit         int       `json:"tv_limit"`
	LastSync        time.Time `json:"last_sync"`
	LastResetDate   string    `json:"last_reset_date"` // YYYY-MM-DD format
}

// QuotaService manages user quotas
type QuotaService struct {
	quotasFile     string
	quotas         map[int64]*UserQuota // telegramID -> quota
	moviepilot     *MoviePilotClient
	mu             sync.RWMutex
	adminIDs       map[int64]bool       // admin users with unlimited quota
}

// NewQuotaService creates a new quota service
func NewQuotaService(dataDir string, moviepilot *MoviePilotClient) *QuotaService {
	quotasFile := fmt.Sprintf("%s/user_quotas.json", dataDir)

	service := &QuotaService{
		quotasFile: quotasFile,
		quotas:     make(map[int64]*UserQuota),
		moviepilot: moviepilot,
		adminIDs:   make(map[int64]bool),
	}

	service.load()

	// Start daily sync routine
	go service.syncRoutine()

	return service
}

// load loads quotas from file
func (s *QuotaService) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.quotasFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var fileData struct {
		Quotas map[int64]*UserQuota `json:"quotas"`
	}

	if err := json.Unmarshal(data, &fileData); err != nil {
		// Try legacy format (string keys)
		var legacyData map[string]*UserQuota
		if err := json.Unmarshal(data, &legacyData); err == nil {
			s.quotas = make(map[int64]*UserQuota)
			for key, quota := range legacyData {
				var id int64
				fmt.Sscanf(key, "%d", &id)
				s.quotas[id] = quota
			}
		}
	} else {
		s.quotas = fileData.Quotas
	}

	// Reset daily usage if needed
	s.resetDailyUsage()

	log.Printf("[QuotaService] Loaded %d user quotas", len(s.quotas))
	return nil
}

// save saves quotas to file
func (s *QuotaService) save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.MarshalIndent(map[string]interface{}{
		"quotas": s.quotas,
	}, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.quotasFile, data, 0644)
}

// GetOrCreateQuota gets or creates a quota for a user
func (s *QuotaService) GetOrCreateQuota(telegramID int64) *UserQuota {
	s.mu.Lock()
	defer s.mu.Unlock()

	if quota, exists := s.quotas[telegramID]; exists {
		return quota
	}

	quota := &UserQuota{
		TelegramID:    telegramID,
		MovieUsed:     0,
		MovieLimit:    2, // Default limit
		TVUsed:        0,
		TVLimit:       2, // Default limit
		LastResetDate: getCurrentDate(),
	}

	s.quotas[telegramID] = quota
	s.save()

	return quota
}

// SyncFromJellyseerr syncs quota from Jellyseerr server
// Deprecated: Use MoviePilot API instead
func (s *QuotaService) SyncFromJellyseerr(telegramID, jellyseerrID int64) error {
	if s.moviepilot == nil {
		return fmt.Errorf("moviepilot client not configured")
	}

	// MoviePilot doesn't have quota API, so we just track locally
	s.mu.Lock()
	defer s.mu.Unlock()

	userQuota := s.GetOrCreateQuota(telegramID)
	userQuota.MoviePilotID = jellyseerrID
	userQuota.JellyseerrID = jellyseerrID // For backwards compatibility
	userQuota.LastSync = time.Now()

	return s.save()
}

// SyncFromMoviePilot syncs quota from MoviePilot server
func (s *QuotaService) SyncFromMoviePilot(telegramID, moviepilotID int64) error {
	if s.moviepilot == nil {
		return fmt.Errorf("moviepilot client not configured")
	}

	// MoviePilot doesn't have quota API, so we just track locally
	s.mu.Lock()
	defer s.mu.Unlock()

	userQuota := s.GetOrCreateQuota(telegramID)
	userQuota.MoviePilotID = moviepilotID
	userQuota.LastSync = time.Now()

	return s.save()
}

// CheckMovieQuota checks if user can make a movie request
func (s *QuotaService) CheckMovieQuota(telegramID int64) bool {
	// Admins have unlimited quota
	if s.isAdmin(telegramID) {
		return true
	}

	quota := s.GetOrCreateQuota(telegramID)

	// Check if we need to reset
	s.checkAndReset(telegramID)

	// -1 means unlimited
	if quota.MovieLimit == -1 {
		return true
	}

	return quota.MovieUsed < quota.MovieLimit
}

// CheckTVQuota checks if user can make a TV request
func (s *QuotaService) CheckTVQuota(telegramID int64) bool {
	// Admins have unlimited quota
	if s.isAdmin(telegramID) {
		return true
	}

	quota := s.GetOrCreateQuota(telegramID)

	// Check if we need to reset
	s.checkAndReset(telegramID)

	// -1 means unlimited
	if quota.TVLimit == -1 {
		return true
	}

	return quota.TVUsed < quota.TVLimit
}

// UseMovieQuota uses one movie quota
func (s *QuotaService) UseMovieQuota(telegramID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	quota := s.GetOrCreateQuota(telegramID)
	s.checkAndResetUnsafe(telegramID)

	if quota.MovieLimit != -1 && quota.MovieUsed >= quota.MovieLimit {
		return fmt.Errorf("movie quota exceeded")
	}

	quota.MovieUsed++
	return s.save()
}

// UseTVQuota uses one TV quota
func (s *QuotaService) UseTVQuota(telegramID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	quota := s.GetOrCreateQuota(telegramID)
	s.checkAndResetUnsafe(telegramID)

	if quota.TVLimit != -1 && quota.TVUsed >= quota.TVLimit {
		return fmt.Errorf("TV quota exceeded")
	}

	quota.TVUsed++
	return s.save()
}

// RestoreQuota restores quota (e.g., when request is declined)
func (s *QuotaService) RestoreQuota(telegramID int64, mediaType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	quota := s.GetOrCreateQuota(telegramID)

	switch mediaType {
	case "movie":
		if quota.MovieUsed > 0 {
			quota.MovieUsed--
		}
	case "tv":
		if quota.TVUsed > 0 {
			quota.TVUsed--
		}
	}

	return s.save()
}

// GetQuotaText returns formatted quota text for a user
func (s *QuotaService) GetQuotaText(telegramID int64) string {
	quota := s.GetOrCreateQuota(telegramID)
	s.checkAndReset(telegramID)

	movieRemaining := quota.MovieLimit - quota.MovieUsed
	tvRemaining := quota.TVLimit - quota.TVUsed

	if quota.MovieLimit == -1 {
		movieRemaining = -1
	}
	if quota.TVLimit == -1 {
		tvRemaining = -1
	}

	var movieText, tvText string

	if movieRemaining == -1 {
		movieText = "无限制"
	} else {
		movieText = fmt.Sprintf("%d/%d", quota.MovieUsed, quota.MovieLimit)
	}

	if tvRemaining == -1 {
		tvText = "无限制"
	} else {
		tvText = fmt.Sprintf("%d/%d", quota.TVUsed, quota.TVLimit)
	}

	return fmt.Sprintf("📊 今日配额\n\n🎬 电影: %s\n📺 剧集: %s", movieText, tvText)
}

// GetQuotaInfo returns detailed quota information
func (s *QuotaService) GetQuotaInfo(telegramID int64) *UserQuota {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.checkAndResetUnsafe(telegramID)
	return s.GetOrCreateQuota(telegramID)
}

// checkAndReset checks if we need to reset daily usage
func (s *QuotaService) checkAndReset(telegramID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkAndResetUnsafe(telegramID)
}

// checkAndResetUnsafe checks and resets without locking (caller must hold lock)
func (s *QuotaService) checkAndResetUnsafe(telegramID int64) {
	quota := s.GetOrCreateQuota(telegramID)
	currentDate := getCurrentDate()

	if quota.LastResetDate != currentDate {
		// Reset daily usage
		quota.MovieUsed = 0
		quota.TVUsed = 0
		quota.LastResetDate = currentDate
		log.Printf("[QuotaService] Reset daily quota for user %d", telegramID)
	}
}

// resetDailyUsage resets all users' daily usage if needed
func (s *QuotaService) resetDailyUsage() {
	currentDate := getCurrentDate()

	for _, quota := range s.quotas {
		if quota.LastResetDate != currentDate {
			quota.MovieUsed = 0
			quota.TVUsed = 0
			quota.LastResetDate = currentDate
		}
	}
}

// syncRoutine periodically syncs quotas from Jellyseerr
func (s *QuotaService) syncRoutine() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		s.syncAllQuotas()
	}
}

// syncAllQuotas syncs all user quotas from MoviePilot
func (s *QuotaService) syncAllQuotas() {
	if s.moviepilot == nil {
		return
	}

	s.mu.RLock()
	users := make([]int64, 0, len(s.quotas))
	for telegramID, quota := range s.quotas {
		if quota.MoviePilotID > 0 {
			users = append(users, telegramID)
		}
	}
	s.mu.RUnlock()

	for _, telegramID := range users {
		s.mu.RLock()
		quota := s.quotas[telegramID]
		moviepilotID := quota.MoviePilotID
		s.mu.RUnlock()

		if moviepilotID > 0 {
			if err := s.SyncFromMoviePilot(telegramID, moviepilotID); err != nil {
				log.Printf("[QuotaService] Failed to sync quota for user %d: %v", telegramID, err)
			}
		}
	}

	log.Printf("[QuotaService] Synced %d user quotas", len(users))
}

// getCurrentDate returns current date in YYYY-MM-DD format
func getCurrentDate() string {
	return time.Now().Format("2006-01-02")
}

// SetAdminIDs sets the admin user IDs who have unlimited quota
func (s *QuotaService) SetAdminIDs(adminIDs []int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.adminIDs = make(map[int64]bool)
	for _, id := range adminIDs {
		s.adminIDs[id] = true
	}

	log.Printf("[QuotaService] Set %d admin IDs for unlimited quota", len(adminIDs))
}

// isAdmin checks if a user is an admin
func (s *QuotaService) isAdmin(telegramID int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.adminIDs[telegramID]
}

// CanRequest checks if user can make a request based on media type
func (s *QuotaService) CanRequest(telegramID int64, mediaType string) bool {
	switch mediaType {
	case "movie":
		return s.CheckMovieQuota(telegramID)
	case "tv":
		return s.CheckTVQuota(telegramID)
	default:
		return true
	}
}

// UseQuota uses quota for a media type
func (s *QuotaService) UseQuota(telegramID int64, mediaType string) error {
	switch mediaType {
	case "movie":
		return s.UseMovieQuota(telegramID)
	case "tv":
		return s.UseTVQuota(telegramID)
	default:
		return nil
	}
}

// FormatQuotaStatus returns a formatted status string
func (s *QuotaService) FormatQuotaStatus(telegramID int64) string {
	quota := s.GetQuotaInfo(telegramID)

	movieRemaining := quota.MovieLimit - quota.MovieUsed
	tvRemaining := quota.TVLimit - quota.TVUsed

	if quota.MovieLimit == -1 {
		movieRemaining = 999 // Effectively unlimited
	}
	if quota.TVLimit == -1 {
		tvRemaining = 999
	}

	// Determine overall status
	var status string
	var emoji string

	if movieRemaining == 0 && tvRemaining == 0 {
		status = "已用完"
		emoji = "🚫"
	} else if movieRemaining == 0 || tvRemaining == 0 {
		status = "部分用完"
		emoji = "⚠️"
	} else if movieRemaining == 1 || tvRemaining == 1 {
		status = "最后1个"
		emoji = "⚡"
	} else {
		status = "充足"
		emoji = "✅"
	}

	return fmt.Sprintf("%s 今日配额: %s", emoji, status)
}
