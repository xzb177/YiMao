package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SmartSearchManager handles enhanced search functionality
type SmartSearchManager struct {
	jellyseerrURL string
	apiKey        string
	httpClient    *http.Client

	// Search history for each user
	searchHistory map[int64][]SearchHistoryItem
	historyMutex  sync.RWMutex

	// Popular searches cache
	popularSearches []string
	popularMutex    sync.RWMutex
	popularUpdated  time.Time

	// Search results cache for pagination (key: userID_query, value: cached results)
	resultsCache  map[string]*CachedSearchResults
	cacheMutex    sync.RWMutex

	// User request quotas (synced from Jellyseerr)
	userQuotas    map[int64]*UserQuota
	quotaMutex    sync.RWMutex
	quotaFile     string

	// Telegram to Jellyseerr user mapping
	userMapping   map[int64]int64  // telegramUserID -> jellyseerrUserID
	mappingMutex  sync.RWMutex
	mappingFile    string
}

// CachedSearchResults stores search results for pagination
type CachedSearchResults struct {
	Query    string
	Results  []EnhancedSearchResult
	ExpireAt time.Time
}

// UserQuota tracks user's request quota usage
type UserQuota struct {
	UserID          int64
	MovieLimit      int    // Daily movie request limit
	MovieUsed       int    // Movies requested today
	MovieResetDate  string // Last reset date for movies (YYYY-MM-DD)
	TVLimit         int    // Daily TV request limit
	TVUsed          int    // TV shows requested today
	TVResetDate     string // Last reset date for TV shows (YYYY-MM-DD)
	LastSyncDate    string // Last sync date with Jellyseerr
	// Server-side quota info from Jellyseerr
	ServerMovieLimit  *int   // Server's movie quota limit (null = use default)
	ServerTVLimit     *int   // Server's TV quota limit (null = use default)
	ServerRequestCount int   // Total request count from server
	LastServerCheck   string // Last time we checked server quota
}

// SearchHistoryItem represents a search history entry
type SearchHistoryItem struct {
	Query     string
	Timestamp time.Time
	Results   int
}

// EnhancedSearchResult represents an enhanced search result with one-click request
type EnhancedSearchResult struct {
	TmdbID        int
	Title         string
	OriginalTitle string
	ChineseTitle  string
	MediaType     string
	Year          string
	PosterURL     string
	BackdropURL   string
	Rating        float64
	VoteCount     int
	Overview      string
	Genres        []Genre
	Popularity    float64
	ReleaseDate   string
	Status        string // For TV: "Ended", "Returning Series", etc.
}

// SearchContext provides context for search
type SearchContext struct {
	UserID      int64
	Username    string
	Query       string
	Params      *SearchParams
	Results     []EnhancedSearchResult
	TotalResults int
	HasMore     bool
	Page        int // Current page (1-indexed)
	PageSize    int // Results per page
}

var smartSearchMgr *SmartSearchManager

// InitSmartSearchManager initializes the smart search manager
func InitSmartSearchManager() {
	smartSearchMgr = &SmartSearchManager{
		jellyseerrURL:   jellyseerrURL,
		apiKey:          jellyseerrAPIKey,
		httpClient:      &http.Client{Timeout: 30 * time.Second},
		searchHistory:   make(map[int64][]SearchHistoryItem),
		popularSearches: []string{"漫威", "权力的游戏", "复仇者联盟", "三体", "狂飙", "繁花"},
		popularUpdated:  time.Now(),
		resultsCache:    make(map[string]*CachedSearchResults),
		userQuotas:      make(map[int64]*UserQuota),
		quotaFile:       "user_quotas.json",
		userMapping:     make(map[int64]int64),
		mappingFile:     "user_mappings.json", // Use same file as UserSyncManager
	}

	// Load quotas from file
	smartSearchMgr.loadQuotas()

	// Load user mapping from file
	smartSearchMgr.loadUserMapping()

	// Sync quotas from Jellyseerr
	smartSearchMgr.syncQuotasFromJellyseerr()

	// Sync users from Jellyseerr
	smartSearchMgr.syncUsersFromJellyseerr()

	// Start background tasks
	go smartSearchMgr.updatePopularSearches()
	go smartSearchMgr.periodicQuotaSync()

	log.Println("SmartSearch manager initialized")
}

// updatePopularSearches periodically updates popular searches from analytics
func (m *SmartSearchManager) updatePopularSearches() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		m.popularMutex.Lock()
		// Get top searches from analytics
		if analytics != nil {
			topMedia := GetTopMedia(10)
			newPopular := make([]string, 0, 10)
			for _, media := range topMedia {
				if media.MediaTitle != "" && len(newPopular) < 10 {
					newPopular = append(newPopular, media.MediaTitle)
				}
			}
			if len(newPopular) > 0 {
				m.popularSearches = newPopular
				m.popularUpdated = time.Now()
				log.Printf("Updated popular searches: %d items", len(newPopular))
			}
		}
		m.popularMutex.Unlock()
	}
}

// Search performs an enhanced search with auto-detection
func (m *SmartSearchManager) Search(ctx *SearchContext) error {
	if m.apiKey == "" {
		return fmt.Errorf("Jellyseerr API not configured")
	}

	// Build search URL with parameters
	searchURL := fmt.Sprintf("%s/api/v1/search", m.jellyseerrURL)

	// Build query parameters
	params := url.Values{}
	params.Add("query", ctx.Query)

	// Add media type filter if specified
	if ctx.Params != nil && ctx.Params.MediaType != "" {
		params.Add("type", ctx.Params.MediaType)
	}

	fullURL := searchURL + "?" + params.Encode()

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set("X-Api-Key", m.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("search failed: %d", resp.StatusCode)
	}

	// Jellyseerr API returns a wrapped response with "results" array
	var rawResponse struct {
		Results []JellyseerrSearchResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rawResponse); err != nil {
		return fmt.Errorf("failed to decode search response: %w", err)
	}

	rawResults := rawResponse.Results

	// Enhance results
	ctx.Results = m.enhanceResults(rawResults, ctx.Params)
	ctx.TotalResults = len(rawResults)
	ctx.HasMore = len(rawResults) >= 20

	// Initialize page
	if ctx.Page < 1 {
		ctx.Page = 1
	}

	// Save to history
	m.saveSearchHistory(ctx)

	// Cache results for pagination (cache for 30 minutes)
	// Use URL-safe encoding for cache key
	cacheKey := fmt.Sprintf("%d_%s", ctx.UserID, url.QueryEscape(ctx.Query))
	m.cacheMutex.Lock()
	m.resultsCache[cacheKey] = &CachedSearchResults{
		Query:    ctx.Query,
		Results:  ctx.Results,
		ExpireAt: time.Now().Add(30 * time.Minute),
	}
	// Clean expired entries
	m.cleanExpiredCache()
	m.cacheMutex.Unlock()

	return nil
}

// enhanceResults adds additional information to search results
func (m *SmartSearchManager) enhanceResults(raw []JellyseerrSearchResult, params *SearchParams) []EnhancedSearchResult {
	enhanced := make([]EnhancedSearchResult, 0, len(raw))

	for _, raw := range raw {
		result := EnhancedSearchResult{
			TmdbID:        raw.TmdbID,
			Title:         raw.Title,
			OriginalTitle: raw.Title,
			MediaType:     raw.MediaType,
			PosterURL:     raw.PosterPath,
			ReleaseDate:   raw.ReleaseDate,
			Overview:      raw.Overview,
		}

		// Handle TV shows (use Name field)
		if result.Title == "" {
			result.Title = raw.Name
			result.OriginalTitle = raw.Name
		}

		// Extract year
		if len(raw.ReleaseDate) >= 4 {
			result.Year = raw.ReleaseDate[:4]
		}

		// Apply filters from params
		if params != nil {
			// Filter by year
			if params.Year != "" && result.Year != params.Year {
				continue
			}

			// Filter by media type
			if params.MediaType != "" && result.MediaType != params.MediaType {
				continue
			}
		}

		// Try to get additional details (rating, genres, etc.)
		if details, err := m.getMediaDetails(raw.TmdbID, raw.MediaType); err == nil {
			result.Rating = details.VoteAverage
			result.VoteCount = details.VoteCount
			result.Genres = details.Genres
			result.Popularity = details.Popularity
			result.BackdropURL = details.BackdropPath

			if details.Status != "" {
				result.Status = details.Status
			}

			// Try to get Chinese title from details
			if details.Name != "" && raw.MediaType == "tv" {
				result.OriginalTitle = details.Name
			}
			if details.Title != "" && raw.MediaType == "movie" {
				result.OriginalTitle = details.Title
			}
		}

		enhanced = append(enhanced, result)
	}

	return enhanced
}

// getMediaDetails fetches detailed media information
func (m *SmartSearchManager) getMediaDetails(tmdbID int, mediaType string) (*MediaDetails, error) {
	url := fmt.Sprintf("%s/api/v1/media/%d", m.jellyseerrURL, tmdbID)

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

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("media not found")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get media failed: %d", resp.StatusCode)
	}

	var details MediaDetails
	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return nil, err
	}

	return &details, nil
}

// saveSearchHistory saves a search to user's history
func (m *SmartSearchManager) saveSearchHistory(ctx *SearchContext) {
	m.historyMutex.Lock()
	defer m.historyMutex.Unlock()

	userHistory := m.searchHistory[ctx.UserID]

	// Add new entry
	entry := SearchHistoryItem{
		Query:     ctx.Query,
		Timestamp: time.Now(),
		Results:   len(ctx.Results),
	}

	// Keep only last 20 searches
	userHistory = append([]SearchHistoryItem{entry}, userHistory...)
	if len(userHistory) > 20 {
		userHistory = userHistory[:20]
	}

	m.searchHistory[ctx.UserID] = userHistory
}

// cleanExpiredCache removes expired entries from the results cache
func (m *SmartSearchManager) cleanExpiredCache() {
	now := time.Now()
	for key, cached := range m.resultsCache {
		if now.After(cached.ExpireAt) {
			delete(m.resultsCache, key)
		}
	}
}

// FormatPageResults formats a specific page of search results
func (m *SmartSearchManager) FormatPageResults(userID int64, query string, pageNum int) (string, *TelegramInlineKeyboard) {
	// Use URL-safe encoding for cache key (query is already encoded from callback)
	cacheKey := fmt.Sprintf("%d_%s", userID, query)

	m.cacheMutex.RLock()
	cached, exists := m.resultsCache[cacheKey]
	m.cacheMutex.RUnlock()

	if !exists || time.Now().After(cached.ExpireAt) {
		return "", nil
	}

	// Create search context for formatting
	ctx := &SearchContext{
		UserID:   userID,
		Query:    cached.Query, // Use original query from cache
		Results:  cached.Results,
		Page:     pageNum,
		PageSize: 8, // 8 results per page
	}

	return FormatSearchResultsWithKeyboard(ctx)
}

// GetSearchHistory returns user's search history
func (m *SmartSearchManager) GetSearchHistory(userID int64) []SearchHistoryItem {
	m.historyMutex.RLock()
	defer m.historyMutex.RUnlock()

	history := m.searchHistory[userID]
	result := make([]SearchHistoryItem, len(history))
	copy(result, history)

	return result
}

// syncQuotasFromJellyseerr syncs default quotas from Jellyseerr settings
// and syncs individual user quotas from the server
func (m *SmartSearchManager) syncQuotasFromJellyseerr() {
	if m.apiKey == "" {
		log.Println("Quota sync: Jellyseerr API not configured, using defaults")
		return
	}

	// Step 1: Fetch default settings from Jellyseerr
	url := fmt.Sprintf("%s/api/v1/settings/main", m.jellyseerrURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("Quota sync: Failed to create request: %v", err)
		return
	}

	req.Header.Set("X-Api-Key", m.apiKey)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		log.Printf("Quota sync: Request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Quota sync: API returned status %d", resp.StatusCode)
		return
	}

	var settings struct {
		DefaultQuotas struct {
			Movie struct {
				QuotaLimit int `json:"quotaLimit"`
				QuotaDays  int `json:"quotaDays"`
			} `json:"movie"`
			TV struct {
				QuotaLimit int `json:"quotaLimit"`
				QuotaDays  int `json:"quotaDays"`
			} `json:"tv"`
		} `json:"defaultQuotas"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&settings); err != nil {
		log.Printf("Quota sync: Failed to decode response: %v", err)
		return
	}

	// Check if quotas are valid (not zero)
	movieLimit := settings.DefaultQuotas.Movie.QuotaLimit
	tvLimit := settings.DefaultQuotas.TV.QuotaLimit
	if movieLimit <= 0 {
		movieLimit = 2 // Default fallback
	}
	if tvLimit <= 0 {
		tvLimit = 2 // Default fallback
	}

	log.Printf("Quota sync: Server default limits - Movie: %d/day, TV: %d/day", movieLimit, tvLimit)

	// Step 2: Sync individual user quotas from server
	// Get all mapped users and sync their quota from Jellyseerr
	// Use both SmartSearchManager's local mappings and UserSyncManager's mappings
	mappings := make(map[int64]int64)

	// Add local mappings
	m.mappingMutex.RLock()
	for k, v := range m.userMapping {
		mappings[k] = v
	}
	m.mappingMutex.RUnlock()

	// Add UserSyncManager mappings if available
	if userSyncMgr != nil {
		userSyncMappings := userSyncMgr.GetAllMappings()
		for k, v := range userSyncMappings {
			mappings[k] = v
		}
	}

	log.Printf("Quota sync: Found %d user mappings to sync", len(mappings))

	syncedCount := 0
	for telegramID, jellyseerrID := range mappings {
		// Fetch user profile from server
		userURL := fmt.Sprintf("%s/api/v1/user/%d", m.jellyseerrURL, jellyseerrID)
		userReq, err := http.NewRequest("GET", userURL, nil)
		if err != nil {
			log.Printf("Quota sync: Failed to create user request for %d: %v", jellyseerrID, err)
			continue
		}

		userReq.Header.Set("X-Api-Key", m.apiKey)

		userResp, err := m.httpClient.Do(userReq)
		if err != nil {
			log.Printf("Quota sync: Failed to fetch user %d: %v", jellyseerrID, err)
			continue
		}

		if userResp.StatusCode == http.StatusOK {
			var user JellyseerrUser
			if err := json.NewDecoder(userResp.Body).Decode(&user); err == nil {
				m.syncUserQuotaFromJellyseerrUser(telegramID, &user)
				syncedCount++
			} else {
				log.Printf("Quota sync: Failed to decode user %d: %v", jellyseerrID, err)
			}
		} else {
			log.Printf("Quota sync: API returned status %d for user %d", userResp.StatusCode, jellyseerrID)
		}
		userResp.Body.Close()
	}

	// Step 3: Update users with no custom limit to use default limits
	m.quotaMutex.Lock()
	updated := 0
	for userID, quota := range m.userQuotas {
		// If user has no custom limit from server, use default
		if quota.MovieLimit != movieLimit || quota.TVLimit != tvLimit {
			// Check if this user was synced in step 2
			_, wasSynced := mappings[userID]
			if !wasSynced {
				// User not found in mappings, update with defaults
				quota.MovieLimit = movieLimit
				quota.TVLimit = tvLimit
				quota.LastSyncDate = time.Now().Format("2006-01-02")
				m.userQuotas[userID] = quota
				updated++
			}
		}
	}
	m.quotaMutex.Unlock()

	log.Printf("Quota sync: Synced %d users from server, updated %d with defaults",
		syncedCount, updated)

	// Save to file
	m.saveQuotas()
}

// periodicQuotaSync periodically syncs quotas from Jellyseerr
func (m *SmartSearchManager) periodicQuotaSync() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		m.syncQuotasFromJellyseerr()
	}
}

// getUserQuota gets or creates user quota with default limits
func (m *SmartSearchManager) getUserQuota(userID int64) *UserQuota {
	m.quotaMutex.Lock()
	defer m.quotaMutex.Unlock()

	today := time.Now().Format("2006-01-02")

	quota, exists := m.userQuotas[userID]
	if !exists {
		// Get default limits from Jellyseerr
		movieLimit := 2
		tvLimit := 2

		// Try to fetch from Jellyseerr settings
		if m.apiKey != "" {
			url := fmt.Sprintf("%s/api/v1/settings/main", m.jellyseerrURL)
			req, _ := http.NewRequest("GET", url, nil)
			req.Header.Set("X-Api-Key", m.apiKey)

			resp, err := m.httpClient.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				var settings struct {
					DefaultQuotas struct {
						Movie struct {
							QuotaLimit int `json:"quotaLimit"`
						} `json:"movie"`
						TV struct {
							QuotaLimit int `json:"quotaLimit"`
						} `json:"tv"`
					} `json:"defaultQuotas"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&settings); err == nil {
					// Check if values are valid
					if settings.DefaultQuotas.Movie.QuotaLimit > 0 {
						movieLimit = settings.DefaultQuotas.Movie.QuotaLimit
					}
					if settings.DefaultQuotas.TV.QuotaLimit > 0 {
						tvLimit = settings.DefaultQuotas.TV.QuotaLimit
					}
				}
				resp.Body.Close()
			}
		}

		quota = &UserQuota{
			UserID:          userID,
			MovieLimit:      movieLimit,
			MovieUsed:       0,
			MovieResetDate:  today,
			TVLimit:         tvLimit,
			TVUsed:          0,
			TVResetDate:     today,
			LastSyncDate:    time.Now().Format("2006-01-02"),
		}
		m.userQuotas[userID] = quota
		m.saveQuotasUnsafe()  // Use unsafe version since we already hold the lock
	}

	// Reset counters if it's a new day
	if quota.MovieResetDate != today {
		quota.MovieUsed = 0
		quota.MovieResetDate = today
	}
	if quota.TVResetDate != today {
		quota.TVUsed = 0
		quota.TVResetDate = today
	}

	return quota
}

// checkServerQuota checks user's quota on Jellyseerr server
// Returns (canRequest, movieRemaining, tvRemaining, error)
// This helps detect if the user has exceeded quota on the server side
func (m *SmartSearchManager) checkServerQuota(telegramID int64, mediaType string) (bool, int, int, error) {
	jellyseerrID, exists := m.GetJellyseerrUserID(telegramID)
	if !exists {
		return false, 0, 0, fmt.Errorf("user not mapped")
	}

	// Fetch user profile from Jellyseerr API
	url := fmt.Sprintf("%s/api/v1/user/%d", m.jellyseerrURL, jellyseerrID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false, 0, 0, err
	}

	req.Header.Set("X-Api-Key", m.apiKey)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return false, 0, 0, fmt.Errorf("failed to fetch user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, 0, 0, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var user struct {
		ID               int    `json:"id"`
		MovieQuotaLimit  *int   `json:"movieQuotaLimit"`
		TVQuotaLimit     *int   `json:"tvQuotaLimit"`
		MovieQuotaDays   *int   `json:"movieQuotaDays"`
		TVQuotaDays      *int   `json:"tvQuotaDays"`
		RequestCount     int    `json:"requestCount"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return false, 0, 0, fmt.Errorf("failed to decode user: %w", err)
	}

	// Get default quotas from server settings
	defaultMovieLimit := 2
	defaultTVLimit := 2

	settingsURL := fmt.Sprintf("%s/api/v1/settings/main", m.jellyseerrURL)
	settingsReq, _ := http.NewRequest("GET", settingsURL, nil)
	settingsReq.Header.Set("X-Api-Key", m.apiKey)

	settingsResp, err := m.httpClient.Do(settingsReq)
	if err == nil && settingsResp.StatusCode == http.StatusOK {
		defer settingsResp.Body.Close()
		var settings struct {
			DefaultQuotas struct {
				Movie struct {
					QuotaLimit int `json:"quotaLimit"`
				} `json:"movie"`
				TV struct {
					QuotaLimit int `json:"quotaLimit"`
				} `json:"tv"`
			} `json:"defaultQuotas"`
		}
		if json.NewDecoder(settingsResp.Body).Decode(&settings) == nil {
			if settings.DefaultQuotas.Movie.QuotaLimit > 0 {
				defaultMovieLimit = settings.DefaultQuotas.Movie.QuotaLimit
			}
			if settings.DefaultQuotas.TV.QuotaLimit > 0 {
				defaultTVLimit = settings.DefaultQuotas.TV.QuotaLimit
			}
		}
	}

	// Determine user's actual limits
	movieLimit := defaultMovieLimit
	tvLimit := defaultTVLimit

	if user.MovieQuotaLimit != nil && *user.MovieQuotaLimit > 0 {
		movieLimit = *user.MovieQuotaLimit
	}
	if user.TVQuotaLimit != nil && *user.TVQuotaLimit > 0 {
		tvLimit = *user.TVQuotaLimit
	}

	// Update local quota with server info
	m.quotaMutex.Lock()
	quota := m.getUserQuotaUnsafe(telegramID)
	quota.ServerMovieLimit = user.MovieQuotaLimit
	quota.ServerTVLimit = user.TVQuotaLimit
	quota.ServerRequestCount = user.RequestCount
	quota.LastServerCheck = time.Now().Format("2006-01-02T15:04:05Z")
	quota.MovieLimit = movieLimit
	quota.TVLimit = tvLimit
	m.userQuotas[telegramID] = quota

	// Check if user can request based on server quota
	// Note: We cannot reliably determine daily quota from server's requestCount
	// because requestCount is the TOTAL historical count, not daily usage.
	// Therefore, we always return true here and let the actual request API
	// handle quota validation on the server side.
	// We rely on local quota tracking for pre-check.

	movieRemaining := movieLimit
	tvRemaining := tvLimit

	// Reset if it's a new day
	today := time.Now().Format("2006-01-02")
	if mediaType == "movie" && quota.MovieResetDate != today {
		quota.MovieUsed = 0
		quota.MovieResetDate = today
	}
	if mediaType == "tv" && quota.TVResetDate != today {
		quota.TVUsed = 0
		quota.TVResetDate = today
	}

	// Use local tracking for remaining calculation
	if mediaType == "movie" {
		movieRemaining = movieLimit - quota.MovieUsed
		if movieRemaining < 0 {
			movieRemaining = 0
		}
	}
	if mediaType == "tv" {
		tvRemaining = tvLimit - quota.TVUsed
		if tvRemaining < 0 {
			tvRemaining = 0
		}
	}

	// Save updated quota
	m.userQuotas[telegramID] = quota
	m.saveQuotasUnsafe()
	m.quotaMutex.Unlock()

	// Allow request - server will do final validation
	// We only use this for informational display
	return true, movieRemaining, tvRemaining, nil
}

// getUserQuotaUnsafe gets quota without locking (caller must hold lock)
func (m *SmartSearchManager) getUserQuotaUnsafe(userID int64) *UserQuota {
	quota, exists := m.userQuotas[userID]
	if !exists {
		today := time.Now().Format("2006-01-02")
		quota = &UserQuota{
			UserID:         userID,
			MovieLimit:     2,
			MovieUsed:      0,
			MovieResetDate: today,
			TVLimit:        2,
			TVUsed:         0,
			TVResetDate:    today,
			LastSyncDate:   today,
		}
		m.userQuotas[userID] = quota
	}
	return quota
}

// CanUserRequest checks if user can make a request
func (m *SmartSearchManager) CanUserRequest(userID int64, mediaType string) (bool, string) {
	quota := m.getUserQuota(userID)

	today := time.Now().Format("2006-01-02")

	if mediaType == "movie" {
		if quota.MovieResetDate != today {
			quota.MovieUsed = 0
			quota.MovieResetDate = today
		}
		if quota.MovieUsed >= quota.MovieLimit {
			return false, fmt.Sprintf("今日电影请求已达上限 (%d/%d)", quota.MovieUsed, quota.MovieLimit)
		}
		return true, ""
	} else if mediaType == "tv" {
		if quota.TVResetDate != today {
			quota.TVUsed = 0
			quota.TVResetDate = today
		}
		if quota.TVUsed >= quota.TVLimit {
			return false, fmt.Sprintf("今日剧集请求已达上限 (%d/%d)", quota.TVUsed, quota.TVLimit)
		}
		return true, ""
	}

	return false, "未知媒体类型"
}

// RecordRequest records a user's request
func (m *SmartSearchManager) RecordRequest(userID int64, mediaType string) {
	m.quotaMutex.Lock()
	defer m.quotaMutex.Unlock()

	today := time.Now().Format("2006-01-02")

	// Get or create quota
	quota, exists := m.userQuotas[userID]
	if !exists {
		movieLimit := 2
		tvLimit := 2
		quota = &UserQuota{
			UserID:          userID,
			MovieLimit:      movieLimit,
			MovieUsed:       0,
			MovieResetDate:  today,
			TVLimit:         tvLimit,
			TVUsed:          0,
			TVResetDate:     today,
			LastSyncDate:    time.Now().Format("2006-01-02"),
		}
	}

	if mediaType == "movie" {
		if quota.MovieResetDate != today {
			quota.MovieUsed = 0
			quota.MovieResetDate = today
		}
		quota.MovieUsed++
		m.userQuotas[userID] = quota
	} else if mediaType == "tv" {
		if quota.TVResetDate != today {
			quota.TVUsed = 0
			quota.TVResetDate = today
		}
		quota.TVUsed++
		m.userQuotas[userID] = quota
	}

	m.saveQuotasUnsafe()
}

// GetUserQuotaInfo returns user's quota information
func (m *SmartSearchManager) GetUserQuotaInfo(userID int64) string {
	quota := m.getUserQuota(userID)

	msg := "📊 *我的请求配额*\n\n"
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

	// Add server quota info if available
	if quota.ServerRequestCount > 0 {
		msg += fmt.Sprintf("\n\n📡 服务器总请求: %d", quota.ServerRequestCount)
	}

	return msg
}

// loadQuotas loads quotas from file
func (m *SmartSearchManager) loadQuotas() {
	m.quotaMutex.Lock()
	defer m.quotaMutex.Unlock()

	data, err := os.ReadFile(m.quotaFile)
	if err != nil {
		log.Printf("Quota file not found, starting fresh: %v", err)
		return
	}

	var quotas map[int64]*UserQuota
	if err := json.Unmarshal(data, &quotas); err != nil {
		log.Printf("Failed to load quotas: %v", err)
		return
	}

	m.userQuotas = quotas
	log.Printf("Loaded %d user quotas", len(quotas))
}

// saveQuotas saves quotas to file
func (m *SmartSearchManager) saveQuotas() {
	m.quotaMutex.Lock()
	defer m.quotaMutex.Unlock()
	m.saveQuotasUnsafe()
}

// saveQuotasUnsafe saves quotas without locking (caller must hold lock)
func (m *SmartSearchManager) saveQuotasUnsafe() {
	data, err := json.MarshalIndent(m.userQuotas, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal quotas: %v", err)
		return
	}

	if err := os.WriteFile(m.quotaFile, data, 0644); err != nil {
		log.Printf("Failed to save quotas: %v", err)
	}
}

// GetPopularSearches returns current popular searches
func (m *SmartSearchManager) GetPopularSearches() []string {
	m.popularMutex.RLock()
	defer m.popularMutex.RUnlock()

	result := make([]string, len(m.popularSearches))
	copy(result, m.popularSearches)

	return result
}

// FormatSearchResultsWithKeyboard formats search results with inline keyboard for one-click requests
func FormatSearchResultsWithKeyboard(ctx *SearchContext) (string, *TelegramInlineKeyboard) {
	if len(ctx.Results) == 0 {
		msg := fmt.Sprintf("🔍 *搜索结果*\n\n")
		msg += fmt.Sprintf("未找到 \"%s\" 相关内容", ctx.Query)
		if ctx.Params != nil {
			if ctx.Params.MediaType != "" {
				msg += fmt.Sprintf("\n\n📝 已筛选类型: %s", map[string]string{"movie": "电影", "tv": "剧集"}[ctx.Params.MediaType])
			}
			if ctx.Params.Year != "" {
				msg += fmt.Sprintf("\n📅 已筛选年份: %s", ctx.Params.Year)
			}
		}
		msg += fmt.Sprintf("\n\n💡 尝试使用更简短的关键词")
		return msg, nil
	}

	// Pagination settings
	pageSize := 8 // Show 8 results per page
	if ctx.PageSize > 0 {
		pageSize = ctx.PageSize
	}

	currentPage := ctx.Page
	if currentPage < 1 {
		currentPage = 1
	}

	// Calculate start and end indices
	startIdx := (currentPage - 1) * pageSize
	endIdx := startIdx + pageSize
	if endIdx > len(ctx.Results) {
		endIdx = len(ctx.Results)
	}

	totalPages := (len(ctx.Results) + pageSize - 1) / pageSize

	msg := fmt.Sprintf("🔍 *搜索结果: \"%s\"*\n\n", ctx.Query)
	msg += fmt.Sprintf("找到 %d 个结果 (第 %d/%d 页)\n\n", len(ctx.Results), currentPage, totalPages)

	// Show results for current page
	displayIdx := 0
	for i := startIdx; i < endIdx; i++ {
		result := ctx.Results[i]
		displayIdx++

		emoji := "🎬"
		if result.MediaType == "tv" {
			emoji = "📺"
		}

		title := result.Title
		if title == "" {
			title = result.OriginalTitle
		}

		// Simplified format: emoji, title, year, rating
		msg += fmt.Sprintf("%d. %s *%s*", displayIdx, emoji, title)

		if result.Year != "" {
			msg += fmt.Sprintf(" (%s)", result.Year)
		}

		// Rating only (simplified)
		if result.Rating > 0 {
			msg += fmt.Sprintf(" - %.1f分", result.Rating)
		}

		msg += "\n"
	}

	msg += "\n💡 点击下方按钮快速请求"

	// Create inline keyboard with request buttons
	keyboard := &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{},
	}

	// Add request buttons for current page
	for i := startIdx; i < endIdx; i++ {
		result := ctx.Results[i]

		title := result.Title
		if title == "" {
			title = result.OriginalTitle
		}

		// Truncate long titles
		if len(title) > 20 {
			title = title[:20] + "..."
		}

		emoji := "🎬"
		if result.MediaType == "tv" {
			emoji = "📺"
		}

		btnText := fmt.Sprintf("%s %s", emoji, title)
		callbackData := fmt.Sprintf("request_%d_%s", result.TmdbID, result.MediaType)

		row := []map[string]string{
			{"text": btnText, "callback_data": callbackData},
		}
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, row)
	}

	// Add pagination buttons
	if totalPages > 1 {
		paginationRow := []map[string]string{}

		// Encode query for callback data
		encodedQuery := url.QueryEscape(ctx.Query)

		// Previous page button
		if currentPage > 1 {
			prevData := fmt.Sprintf("page_%s_%d", encodedQuery, currentPage-1)
			paginationRow = append(paginationRow, map[string]string{"text": "⬅️ 上一页", "callback_data": prevData})
		}

		// Page indicator
		pageInfo := fmt.Sprintf("%d/%d", currentPage, totalPages)
		paginationRow = append(paginationRow, map[string]string{"text": pageInfo, "callback_data": "ignore"})

		// Next page button
		if currentPage < totalPages {
			nextData := fmt.Sprintf("page_%s_%d", encodedQuery, currentPage+1)
			paginationRow = append(paginationRow, map[string]string{"text": "下一页 ➡️", "callback_data": nextData})
		}

		if len(paginationRow) > 0 {
			keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, paginationRow)
		}
	}

	return msg, keyboard
}

// FormatQuickSearchMenu formats a quick search menu with popular suggestions
func FormatQuickSearchMenu(userID int64) (string, *TelegramInlineKeyboard) {
	msg := "🔍 *快速搜索*\n\n"
	msg += "点击下方关键词快速搜索，或输入你想找的内容：\n"

	keyboard := &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{},
	}

	// Get popular searches
	popular := []string{"最新电影", "热播剧集", "高分电影", "动漫新番"}
	if smartSearchMgr != nil {
		if ps := smartSearchMgr.GetPopularSearches(); len(ps) > 0 {
			popular = ps
		}
	}

	// Create buttons (2 per row)
	for i := 0; i < len(popular); i += 2 {
		row := []map[string]string{}

		btn1 := map[string]string{
			"text":          popular[i],
			"callback_data": fmt.Sprintf("search_%s", popular[i]),
		}
		row = append(row, btn1)

		if i+1 < len(popular) {
			btn2 := map[string]string{
				"text":          popular[i+1],
				"callback_data": fmt.Sprintf("search_%s", popular[i+1]),
			}
			row = append(row, btn2)
		}

		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, row)
	}

	// Add user's recent searches if available
	if smartSearchMgr != nil {
		history := smartSearchMgr.GetSearchHistory(userID)
		if len(history) > 0 {
			msg += fmt.Sprintf("\n*最近搜索:*\n")

			// Show up to 3 recent searches
			for i, h := range history {
				if i >= 3 {
					break
				}
				msg += fmt.Sprintf("• %s\n", h.Query)
			}
		}
	}

	return msg, keyboard
}

// HandleQuickSearchCallback handles search button callbacks
func HandleQuickSearchCallback(userID int64, query string) (string, *TelegramInlineKeyboard, error) {
	// Parse with NLP
	intent, params, err := ParseNLP(query)
	if err != nil {
		return "", nil, err
	}

	if intent != IntentSearch && intent != IntentMovie && intent != IntentTV {
		return "❓ 请输入要搜索的内容", nil, nil
	}

	if smartSearchMgr == nil {
		return "❌ 搜索功能暂不可用", nil, nil
	}

	// Create search context
	ctx := &SearchContext{
		UserID:   userID,
		Query:    query,
		Params:   params,
	}

	// Perform search
	if err := smartSearchMgr.Search(ctx); err != nil {
		log.Printf("Search error: %v", err)
		return "❌ 搜索失败，请稍后再试", nil, nil
	}

	// Format results
	msg, keyboard := FormatSearchResultsWithKeyboard(ctx)
	return msg, keyboard, nil
}

// CreateOneClickRequest creates a request with auto-detection
func (m *SmartSearchManager) CreateOneClickRequest(userID int64, tmdbID int, mediaType string) (string, error) {
	// Try to create request directly via Jellyseerr API
	return m.CreateRequest(userID, tmdbID, mediaType)
}

// generateRequestResponse generates the request response message
func (m *SmartSearchManager) generateRequestResponse(tmdbID int, mediaType string, title string, userID int64) (string, error) {
	mediaTypeName := "电影"
	if mediaType == "tv" {
		mediaTypeName = "剧集"
	}

	msg := fmt.Sprintf("📝 *快速请求*\n\n")

	if title != "" {
		msg += fmt.Sprintf("📦 %s\n", title)
	} else {
		msg += fmt.Sprintf("📦 %s (ID: %d)\n", mediaTypeName, tmdbID)
	}

	// Generate Jellyseerr request link
	mediaTypeStr := "movie"
	if mediaType == "tv" {
		mediaTypeStr = "tv"
	}
	requestURL := fmt.Sprintf("%s/%s/%d", m.jellyseerrURL, mediaTypeStr, tmdbID)

	msg += fmt.Sprintf("\n🔗 请点击下方链接前往 Jellyseerr 创建请求:\n")
	msg += fmt.Sprintf("👉 %s\n", requestURL)

	msg += fmt.Sprintf("\n💡 提示: 首次使用需要登录 Jellyseerr 账号")

	// Add quota info
	msg += fmt.Sprintf("\n\n---\n")
	msg += m.GetUserQuotaInfo(userID)

	// Record the request
	m.RecordRequest(userID, mediaType)

	return msg, nil
}

// syncUsersFromJellyseerr syncs users from Jellyseerr
func (m *SmartSearchManager) syncUsersFromJellyseerr() {
	if m.apiKey == "" {
		log.Println("User sync: Jellyseerr API not configured")
		return
	}

	log.Println("User sync: Starting...")

	// Fetch all users from Jellyseerr
	page := 1
	hasMore := true
	totalSynced := 0

	for hasMore {
		url := fmt.Sprintf("%s/api/v1/user?page=%d&take=50", m.jellyseerrURL, page)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			log.Printf("User sync: Failed to create request: %v", err)
			return
		}

		req.Header.Set("X-Api-Key", m.apiKey)

		resp, err := m.httpClient.Do(req)
		if err != nil {
			log.Printf("User sync: Request failed: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Printf("User sync: API returned status %d", resp.StatusCode)
			return
		}

		var response struct {
			PageInfo struct {
				Pages int `json:"pages"`
			} `json:"pageInfo"`
			Results []JellyseerrUser `json:"results"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			log.Printf("User sync: Failed to decode response: %v", err)
			return
		}

		// Process users
		m.mappingMutex.Lock()
		for _, user := range response.Results {
			// Try to find Telegram user by checking if they have a note with their Telegram ID
			telegramID := m.findTelegramUserIDByJellyfinID(user.JellyfinUserID)
			if telegramID > 0 {
				m.userMapping[telegramID] = int64(user.ID)
				log.Printf("User sync: Mapped Telegram user %d to Jellyseerr user %d (%s)", telegramID, user.ID, user.DisplayName)
				totalSynced++
			}
		}
		m.mappingMutex.Unlock()

		// Save mapping
		m.saveUserMapping()

		// Check if more pages
		if page >= response.PageInfo.Pages {
			hasMore = false
		} else {
			page++
		}
	}

	log.Printf("User sync: Synced %d users", totalSynced)
}

// findTelegramUserIDByJellyfinID finds Telegram user ID by Jellyfin user ID
// This would be stored in user notes or require manual mapping
func (m *SmartSearchManager) findTelegramUserIDByJellyfinID(jellyfinID string) int64 {
	// For now, we'll use a simple approach:
	// Check if there's a mapping file or try to match by username
	// In production, this should be configured by users

	// Try to find by matching usernames in admin list
	// This is a placeholder - you should implement proper user mapping
	return 0
}

// loadUserMapping loads user mapping from file
// Supports both formats: simple map and UserSyncManager format
func (m *SmartSearchManager) loadUserMapping() {
	m.mappingMutex.Lock()
	defer m.mappingMutex.Unlock()

	data, err := os.ReadFile(m.mappingFile)
	if err != nil {
		log.Printf("User mapping file not found, starting fresh: %v", err)
		return
	}

	// Try UserSyncManager format first (with telegramToJellyseerr field)
	var mappingData struct {
		TelegramToJellyseerr map[string]int64 `json:"telegramToJellyseerr"`
	}
	if err := json.Unmarshal(data, &mappingData); err == nil && len(mappingData.TelegramToJellyseerr) > 0 {
		// Convert string keys to int64
		m.userMapping = make(map[int64]int64)
		for k, v := range mappingData.TelegramToJellyseerr {
			telegramID, err := strconv.ParseInt(k, 10, 64)
			if err == nil {
				m.userMapping[telegramID] = v
			}
		}
		log.Printf("Loaded %d user mappings (UserSyncManager format)", len(m.userMapping))
		return
	}

	// Try simple format
	var mapping map[int64]int64
	if err := json.Unmarshal(data, &mapping); err != nil {
		log.Printf("Failed to load user mapping: %v", err)
		return
	}

	m.userMapping = mapping
	log.Printf("Loaded %d user mappings (simple format)", len(mapping))
}

// saveUserMapping saves user mapping to file
func (m *SmartSearchManager) saveUserMapping() {
	m.mappingMutex.Lock()
	defer m.mappingMutex.Unlock()

	data, err := json.MarshalIndent(m.userMapping, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal user mapping: %v", err)
		return
	}

	if err := os.WriteFile(m.mappingFile, data, 0644); err != nil {
		log.Printf("Failed to save user mapping: %v", err)
	}
}

// SetUserMapping sets a Telegram to Jellyseerr user mapping (for admin setup)
func (m *SmartSearchManager) SetUserMapping(telegramID int64, jellyseerrUserID int64) {
	m.mappingMutex.Lock()
	defer m.mappingMutex.Unlock()

	m.userMapping[telegramID] = jellyseerrUserID
	m.saveUserMappingUnsafe()
}

// saveUserMappingUnsafe saves mapping without lock (caller must hold lock)
func (m *SmartSearchManager) saveUserMappingUnsafe() {
	data, err := json.MarshalIndent(m.userMapping, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal user mapping: %v", err)
		return
	}

	if err := os.WriteFile(m.mappingFile, data, 0644); err != nil {
		log.Printf("Failed to save user mapping: %v", err)
	}
}

// GetJellyseerrUserID gets Jellyseerr user ID for a Telegram user
func (m *SmartSearchManager) GetJellyseerrUserID(telegramID int64) (int64, bool) {
	// First check local mapping
	m.mappingMutex.RLock()
	jellyseerrID, exists := m.userMapping[telegramID]
	m.mappingMutex.RUnlock()

	if exists {
		return jellyseerrID, true
	}

	// If not found locally, check UserSyncManager
	if userSyncMgr != nil {
		return userSyncMgr.GetJellyseerrUserID(telegramID)
	}

	return 0, false
}

// CreateRequest creates a request directly via Jellyseerr API
func (m *SmartSearchManager) CreateRequest(telegramID int64, tmdbID int, mediaType string) (string, error) {
	if m.apiKey == "" {
		return "", fmt.Errorf("Jellyseerr API not configured")
	}

	// Check if user is mapped
	jellyseerrUserID, exists := m.GetJellyseerrUserID(telegramID)
	log.Printf("CreateRequest: telegramID=%d, jellyseerrUserID=%d, exists=%v", telegramID, jellyseerrUserID, exists)

	if !exists {
		return m.generateUnlinkedUserResponse(tmdbID, mediaType, telegramID)
	}

	if jellyseerrUserID == 0 {
		return "", fmt.Errorf("无效的用户映射，Jellyseerr ID 为 0")
	}

	// Check server quota first to detect quota issues early
	canRequestServer, movieRemaining, tvRemaining, err := m.checkServerQuota(telegramID, mediaType)
	if err != nil {
		log.Printf("Warning: Failed to check server quota: %v", err)
		// Continue anyway, the server will validate
	} else if !canRequestServer {
		// Server says quota exceeded, provide helpful message
		quotaInfo := m.GetUserQuotaInfo(telegramID)
		msg := "🚫 *求片失败*\n\n"
		msg += "你的 Jellyseerr 账号的请求配额已用完\n\n"
		msg += quotaInfo + "\n\n"

		// Add server-specific info
		msg += "📊 服务器状态:\n"
		mediaName := "电影"
		if mediaType == "tv" {
			mediaName = "剧集"
		}
		remaining := movieRemaining
		if mediaType == "tv" {
			remaining = tvRemaining
		}
		msg += fmt.Sprintf("• %s 剩余配额: ~%d\n", mediaName, remaining)
		msg += "\n💡 解决方案:\n"
		msg += "• 联系管理员增加配额\n"
		msg += "• 或等待配额自动重置"
		return msg, nil
	}

	// Check local quota as well
	canRequest, quotaMsg := m.CanUserRequest(telegramID, mediaType)
	if !canRequest {
		return "🚫 " + quotaMsg + "\n\n" + m.GetUserQuotaInfo(telegramID), nil
	}

	// Create request via Jellyseerr API
	url := fmt.Sprintf("%s/api/v1/request", m.jellyseerrURL)

	payload := map[string]interface{}{
		"mediaId":   tmdbID,
		"mediaType": mediaType,
		"userId":    int(jellyseerrUserID),
	}

	log.Printf("Creating request: mediaId=%d, mediaType=%s, userId=%d", tmdbID, mediaType, jellyseerrUserID)

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Api-Key", m.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("Jellyseerr request API: status=%d, body=%s", resp.StatusCode, string(body))

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyStr := string(body)

		// Provide more helpful error messages
		if strings.Contains(bodyStr, "Quota exceeded") {
			quotaInfo := m.GetUserQuotaInfo(telegramID)
			msg := "🚫 *求片失败*\n\n"
			msg += "你的 Jellyseerr 账号的请求配额已用完\n\n"
			msg += quotaInfo + "\n\n"
			msg += "💡 解决方案:\n"
			msg += "• 联系管理员增加配额\n"
			msg += "• 或等待配额自动重置"
			return msg, nil
		}

		return "", fmt.Errorf("request failed: %s", bodyStr)
	}

	var result struct {
		ID          int              `json:"id"`
		Status      interface{}      `json:"status"` // 可以是字符串或数字
		Media       *JellyseerrMedia `json:"media"`
		CreatedAt   string           `json:"createdAt"`
		Requester   *JellyseerrUser `json:"requestedBy"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w, body: %s", err, string(body))
	}

	// Convert status to string
	statusStr := "unknown"
	switch v := result.Status.(type) {
	case string:
		statusStr = v
	case float64:
		switch int(v) {
		case 1:
			statusStr = "pending"
		case 2:
			statusStr = "approved"
		case 3:
			statusStr = "available"
		case 4:
			statusStr = "declined"
		}
	case int:
		switch v {
		case 1:
			statusStr = "pending"
		case 2:
			statusStr = "approved"
		case 3:
			statusStr = "available"
		case 4:
			statusStr = "declined"
		}
	}

	log.Printf("Request created: id=%d, status=%s", result.ID, statusStr)

	// Sync user quota from Jellyseerr server
	// This ensures bot quota matches server-side quota after request creation
	if err := m.syncUserQuotaFromServer(telegramID); err != nil {
		log.Printf("Warning: Failed to sync quota from server: %v", err)
		// Fall back to syncing from response data
		if result.Requester != nil {
			m.syncUserQuotaFromJellyseerrUser(telegramID, result.Requester)
		}
		// Still record locally
		m.RecordRequest(telegramID, mediaType)
	} else {
		// Successfully synced from server, no need to record locally
		log.Printf("User quota synced from server after request creation")
	}

	// Get media title
	title := fmt.Sprintf("媒体 #%d", tmdbID)
	if result.Media != nil {
		if result.Media.Title != "" {
			title = result.Media.Title
		}
	}

	// Handle different request statuses
	var msg string

	if statusStr == "declined" {
		// Check if media is already available
		mediaAvailable := false
		mediaStatus := ""
		if result.Media != nil {
			mediaStatus = result.Media.GetStatusString()
			if mediaStatus == "available" {
				mediaAvailable = true
			}
		}

		msg = "📋 *求片请求*\n\n"
		msg += fmt.Sprintf("📦 %s\n", title)
		msg += fmt.Sprintf("📝 请求ID: %d\n", result.ID)

		if mediaAvailable {
			msg += "\n\n✨ *好消息！\n\n"
			msg += "这部电影已经在库中了，可以直接观看 🎬\n\n"
			msg += "💡 由于媒体已存在，系统自动关闭了此请求"
		} else {
			msg += "\n\n❌ 请求已关闭\n\n"
			if mediaStatus == "approved" || mediaStatus == "processing" {
				msg += "💡 这部电影正在处理中，请耐心等待"
			} else if mediaStatus == "pending" {
				msg += "💡 这部电影已在请求队列中"
			} else {
				msg += "💡 可能媒体已存在或请求被管理员拒绝"
			}
		}
	} else {
		// Normal status handling (pending, approved, available)
		msg = fmt.Sprintf("✅ *请求已创建*\n\n")
		msg += fmt.Sprintf("📦 %s\n", title)
		msg += fmt.Sprintf("📝 请求ID: %d\n", result.ID)
		msg += fmt.Sprintf("📊 状态: %s\n", statusStr)

		if statusStr == "pending" {
			msg += "\n\n⏳ 等待管理员批准"
		} else if statusStr == "approved" {
			msg += "\n\n✅ 已批准，正在处理中"
		} else if statusStr == "available" {
			msg += "\n\n🎉 已可用，可以观看啦！"
		}
	}

	// Add quota info
	msg += fmt.Sprintf("\n\n---\n")
	msg += m.GetUserQuotaInfo(telegramID)

	return msg, nil
}

// generateUnlinkedUserResponse generates response for users not linked to Jellyseerr
func (m *SmartSearchManager) generateUnlinkedUserResponse(tmdbID int, mediaType string, telegramID int64) (string, error) {
	// Try to get media title
	title := fmt.Sprintf("媒体 #%d", tmdbID)

	msg := fmt.Sprintf("⚠️ *需要链接账号*\n\n")
	msg += fmt.Sprintf("📦 %s\n\n", title)
	msg += "这是您第一次使用求片功能，需要先链接您的 Jellyseerr 账号。\n\n"
	msg += "请按以下步骤操作：\n\n"
	msg += "1️⃣ 访问 Jellyseerr 网站\n"
	msg += fmt.Sprintf("👉 %s\n", m.jellyseerrURL)
	msg += "\n2️⃣ 登录您的账号\n"
	msg += "3️⃣ 完成登录后，发送 /link 命令给我\n"
	msg += "4️⃣ 我将帮您自动链接账号"

	// Add quota info
	msg += fmt.Sprintf("\n\n---\n")
	msg += m.GetUserQuotaInfo(telegramID)

	return msg, nil
}

// syncUserQuotaFromJellyseerrUser syncs user quota from Jellyseerr user info
func (m *SmartSearchManager) syncUserQuotaFromJellyseerrUser(telegramID int64, user *JellyseerrUser) {
	if user == nil {
		return
	}

	m.quotaMutex.Lock()
	defer m.quotaMutex.Unlock()

	today := time.Now().Format("2006-01-02")

	// Get or create quota
	quota, exists := m.userQuotas[telegramID]
	if !exists {
		quota = &UserQuota{
			UserID:        telegramID,
			MovieLimit:    2,
			MovieUsed:     0,
			MovieResetDate: today,
			TVLimit:       2,
			TVUsed:        0,
			TVResetDate:   today,
			LastSyncDate:  today,
		}
	}

	// Update limits from Jellyseerr user settings
	// These are the server-side quota limits
	if user.MovieQuotaLimit != nil && *user.MovieQuotaLimit > 0 {
		quota.MovieLimit = *user.MovieQuotaLimit
	}
	if user.TVQuotaLimit != nil && *user.TVQuotaLimit > 0 {
		quota.TVLimit = *user.TVQuotaLimit
	}

	// Sync used counts from Jellyseerr if available
	// The server tracks actual usage, so we should use that
	if user.MovieRequests >= 0 {
		// Reset if it's a new day
		if quota.MovieResetDate != today {
			quota.MovieUsed = 0
			quota.MovieResetDate = today
		}
		// Use server-side count if it's more recent
		// Note: MovieRequests from Jellyseerr is total count, not daily
		// So we keep local tracking for daily quota
	}
	if user.TVRequests >= 0 {
		if quota.TVResetDate != today {
			quota.TVUsed = 0
			quota.TVResetDate = today
		}
	}

	quota.LastSyncDate = today
	m.userQuotas[telegramID] = quota
	m.saveQuotasUnsafe()

	log.Printf("Synced user quota from server: userID=%d, movieLimit=%d, tvLimit=%d, movieUsed=%d, tvUsed=%d",
		telegramID, quota.MovieLimit, quota.TVLimit, quota.MovieUsed, quota.TVUsed)
}

// syncUserQuotaFromServer fetches and syncs user quota directly from Jellyseerr API
// This ensures bot quota matches server-side quota
func (m *SmartSearchManager) syncUserQuotaFromServer(telegramID int64) error {
	jellyseerrID, exists := m.GetJellyseerrUserID(telegramID)
	if !exists {
		return fmt.Errorf("user not mapped to Jellyseerr")
	}

	// Fetch user profile from Jellyseerr API
	url := fmt.Sprintf("%s/api/v1/user/%d", m.jellyseerrURL, jellyseerrID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("X-Api-Key", m.apiKey)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var user JellyseerrUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return fmt.Errorf("failed to decode user: %w", err)
	}

	// Sync quota using the fetched user data
	m.syncUserQuotaFromJellyseerrUser(telegramID, &user)
	return nil
}

// LinkUserAccount links a Telegram user to Jellyseerr account by username/email
func (m *SmartSearchManager) LinkUserAccount(telegramID int64, identifier string) (string, error) {
	if m.apiKey == "" {
		return "", fmt.Errorf("Jellyseerr API not configured")
	}

	// Search for user by username or email
	users, err := m.searchJellyseerrUser(identifier)
	if err != nil {
		return "", fmt.Errorf("搜索用户失败: %w", err)
	}

	if len(users) == 0 {
		return "❌ 未找到匹配的账号\n\n请检查:\n• 用户名/邮箱是否正确\n• 是否已在 Jellyseerr 注册", nil
	}

	if len(users) > 1 {
		msg := "找到多个匹配的账号，请选择:\n\n"
		for i, user := range users {
			msg += fmt.Sprintf("%d. %s", i+1, user.DisplayName)
			if user.Email != "" {
				msg += fmt.Sprintf(" (%s)", user.Email)
			}
			msg += "\n"
		}
		msg += "\n请使用 /link <序号> 选择"
		return msg, nil
	}

	// Only one user found, link them
	user := users[0]
	m.SetUserMapping(telegramID, int64(user.ID))

	return fmt.Sprintf("✅ *账号已链接*\n\n👤 %s\n\n现在您可以直接使用求片功能了！", user.DisplayName), nil
}

// searchJellyseerrUser searches for a user by username or email
func (m *SmartSearchManager) searchJellyseerrUser(identifier string) ([]JellyseerrUser, error) {
	url := fmt.Sprintf("%s/api/v1/user?take=10", m.jellyseerrURL)

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
		return nil, fmt.Errorf("search failed: %d", resp.StatusCode)
	}

	var response struct {
		Results []JellyseerrUser `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	return response.Results, nil
}
