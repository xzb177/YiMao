package bot

import (
	"fmt"
	"os"
	"encoding/json"
	"log"
	"sync"
)

// QuotaManager manages user request quotas
type QuotaManager struct {
	quotaFile string
	quotas    map[int64]*UserQuota
	mu        sync.RWMutex
}

// UserQuota represents a user's quota information
type UserQuota struct {
	UserID            int64  `json:"userId"`
	MovieLimit        int    `json:"movieLimit"`
	MovieUsed         int    `json:"movieUsed"`
	MovieResetDate    string `json:"movieResetDate"`
	TVLimit           int    `json:"tvLimit"`
	TVUsed            int    `json:"tvUsed"`
	TVResetDate       string `json:"tvResetDate"`
	ServerRequestCount int   `json:"serverRequestCount,omitempty"`
	LastSyncDate      string `json:"lastSyncDate,omitempty"`
}

// NewQuotaManager creates a new quota manager
func NewQuotaManager(quotaFile string) *QuotaManager {
	qm := &QuotaManager{
		quotaFile: quotaFile,
		quotas:    make(map[int64]*UserQuota),
	}
	qm.loadQuotas()
	return qm
}

// loadQuotas loads quotas from file
func (qm *QuotaManager) loadQuotas() {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	data, err := os.ReadFile(qm.quotaFile)
	if err != nil {
		log.Printf("[QuotaManager] Quota file not found, starting fresh: %v", err)
		return
	}

	var quotas map[int64]*UserQuota
	if err := json.Unmarshal(data, &quotas); err != nil {
		log.Printf("[QuotaManager] Failed to load quotas: %v", err)
		return
	}

	qm.quotas = quotas
	log.Printf("[QuotaManager] Loaded %d user quotas", len(quotas))
}

// saveQuotas saves quotas to file
func (qm *QuotaManager) saveQuotas() {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	data, err := json.MarshalIndent(qm.quotas, "", "  ")
	if err != nil {
		log.Printf("[QuotaManager] Failed to marshal quotas: %v", err)
		return
	}

	if err := os.WriteFile(qm.quotaFile, data, 0644); err != nil {
		log.Printf("[QuotaManager] Failed to save quotas: %v", err)
	}
}

// GetUserQuota gets user's quota info
func (qm *QuotaManager) GetUserQuota(userID int64) *UserQuota {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	quota, exists := qm.quotas[userID]
	if !exists {
		// Create default quota
		quota = &UserQuota{
			UserID:         userID,
			MovieLimit:     2,
			MovieUsed:      0,
			MovieResetDate: "",
			TVLimit:        2,
			TVUsed:         0,
			TVResetDate:    "",
		}
		qm.quotas[userID] = quota
	}

	return quota
}

// SetUserQuota sets user's quota info
func (qm *QuotaManager) SetUserQuota(userID int64, quota *UserQuota) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	qm.quotas[userID] = quota
	qm.saveQuotasUnsafe()
}

// IncrementUsage increments the usage counter for a media type
func (qm *QuotaManager) IncrementUsage(userID int64, mediaType string) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	quota := qm.quotas[userID]
	if quota == nil {
		quota = &UserQuota{
			UserID:      userID,
			MovieLimit:  2,
			TVLimit:     2,
		}
		qm.quotas[userID] = quota
	}

	if mediaType == "movie" {
		quota.MovieUsed++
	} else if mediaType == "tv" {
		quota.TVUsed++
	}

	qm.saveQuotasUnsafe()
}

// saveQuotasUnsafe saves without locking (caller must hold lock)
func (qm *QuotaManager) saveQuotasUnsafe() {
	data, err := json.MarshalIndent(qm.quotas, "", "  ")
	if err != nil {
		log.Printf("[QuotaManager] Failed to marshal quotas: %v", err)
		return
	}

	if err := os.WriteFile(qm.quotaFile, data, 0644); err != nil {
		log.Printf("[QuotaManager] Failed to save quotas: %v", err)
	}
}

// FormatQuotaInfo formats quota info as a string
func (qm *QuotaManager) FormatQuotaInfo(userID int64) string {
	quota := qm.GetUserQuota(userID)

	msg := "📊 我的请求配额\n\n"
	msg += fmt.Sprintf("🎬 电影: %d/%d (每天)\n", quota.MovieUsed, quota.MovieLimit)
	msg += fmt.Sprintf("📺 剧集: %d/%d (每天)\n\n", quota.TVUsed, quota.TVLimit)

	remaining := ""
	if quota.MovieUsed < quota.MovieLimit {
		remaining += fmt.Sprintf("还可请求 %d 部电影", quota.MovieLimit-quota.MovieUsed)
	}
	if quota.TVUsed < quota.TVLimit {
		if remaining != "" {
			remaining += "，"
		}
		remaining += fmt.Sprintf("%d 部剧集", quota.TVLimit-quota.TVUsed)
	}
	if remaining == "" {
		remaining = "今日配额已用完"
	}
	msg += "💡 " + remaining

	return msg
}

// CheckQuota checks if user has quota left for the media type
func (qm *QuotaManager) CheckQuota(userID int64, mediaType string) (bool, string) {
	quota := qm.GetUserQuota(userID)

	if mediaType == "movie" {
		if quota.MovieUsed >= quota.MovieLimit {
			return false, "电影配额已用完"
		}
	} else if mediaType == "tv" {
		if quota.TVUsed >= quota.TVLimit {
			return false, "剧集配额已用完"
		}
	}

	return true, ""
}
