package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SmartSearch handles intelligent search with filters
type SmartSearch struct {
	jellyseerrURL string
	apiKey        string
	httpClient    *http.Client
}

// MediaDetails represents detailed media information
type MediaDetails struct {
	TmdbID          int    `json:"id"`
	Title           string `json:"title"`
	Name            string `json:"name"`
	OriginalTitle   string `json:"original_title"`
	ReleaseDate     string `json:"release_date"`
	PosterPath      string `json:"poster_path"`
	BackdropPath    string `json:"backdrop_path"`
	Overview        string `json:"overview"`
	VoteAverage     float64 `json:"vote_average"`
	VoteCount       int    `json:"vote_count"`
	Genres          []Genre `json:"genres"`
	MediaType       string `json:"media_type"`
	Popularity      float64 `json:"popularity"`
	FirstAirDate    string `json:"first_air_date"`
	LastAirDate     string `json:"last_air_date"`
	NumberOfEpisodes int   `json:"number_of_episodes"`
	NumberOfSeasons int   `json:"number_of_seasons"`
	Status          string `json:"status"`
	ProductionCompanies []ProductionCompany `json:"production_companies"`
	SpokenLanguages []SpokenLanguage `json:"spoken_languages"`
}

type Genre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type ProductionCompany struct {
	ID            int    `json:"id"`
	LogoPath      string `json:"logo_path"`
	Name          string `json:"name"`
	OriginCountry string `json:"origin_country"`
}

type SpokenLanguage struct {
	EnglishName string `json:"english_name"`
	Iso6391     string `json:"iso_639_1"`
	Name        string `json:"name"`
}

// SearchFilter represents search filters
type SearchFilter struct {
	Year       string
	Genre      string
	MinRating  float64
	MediaType  string // "movie" or "tv"
	Language   string
}

// RequestTracker tracks request status and sends notifications
type RequestTracker struct {
	pendingRequests map[string]*TrackedRequest // requestID -> request
	requestMutex    sync.RWMutex
}

// TrackedRequest represents a request being tracked
type TrackedRequest struct {
	RequestID      string
	MediaTitle     string
	MediaType      string
	TmdbID         int
	RequesterID    string
	RequesterName  string
	RequestedAt    time.Time
	ApprovedAt     *time.Time
	AvailableAt    *time.Time
	NotifiedAt     *time.Time
	LastReminderAt *time.Time
	ReminderCount  int
}

// AutoReminder handles automatic reminders
type AutoReminder struct {
	checkInterval time.Duration
	remindAfter   time.Duration // How long to wait before reminder
	maxReminders  int
	stopChan      chan bool
}

var (
	smartSearch    *SmartSearch
	requestTracker *RequestTracker
	autoReminder   *AutoReminder
)

// InitSmartSearch initializes the smart search module
func InitSmartSearch() {
	if jellyseerrURL == "" || jellyseerrAPIKey == "" {
		log.Println("SmartSearch: Jellyseerr API not configured, limited functionality")
		smartSearch = &SmartSearch{
			jellyseerrURL: jellyseerrURL,
			apiKey:        "",
			httpClient:    &http.Client{Timeout: 30 * time.Second},
		}
		return
	}

	smartSearch = &SmartSearch{
		jellyseerrURL: jellyseerrURL,
		apiKey:        jellyseerrAPIKey,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
	}

	log.Println("SmartSearch initialized")
}

// InitRequestTracker initializes the request tracker
func InitRequestTracker() {
	requestTracker = &RequestTracker{
		pendingRequests: make(map[string]*TrackedRequest),
	}

	// Load tracked requests from file
	loadTrackedRequests()

	// Start auto reminder
	StartAutoReminder()

	log.Println("RequestTracker initialized")
}

// StartAutoReminder starts the automatic reminder routine
func StartAutoReminder() {
	autoReminder = &AutoReminder{
		checkInterval: 10 * time.Minute,
		remindAfter:   1 * time.Hour, // Remind after 1 hour of no action
		maxReminders:  3,
		stopChan:      make(chan bool),
	}

	go func() {
		ticker := time.NewTicker(autoReminder.checkInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				checkPendingRequests()
			case <-autoReminder.stopChan:
				return
			}
		}
	}()

	log.Println("AutoReminder started")
}

// SearchWithFilter searches media with filters
func (ss *SmartSearch) SearchWithFilter(query string, filter SearchFilter) ([]MediaDetails, error) {
	if ss.apiKey == "" {
		return nil, fmt.Errorf("Jellyseerr API not configured")
	}

	// Search using Jellyseerr (URL encode the query)
	url := fmt.Sprintf("%s/api/v1/search?query=%s&take=20", ss.jellyseerrURL, url.QueryEscape(query))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Api-Key", ss.apiKey)

	resp, err := ss.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search failed: %d", resp.StatusCode)
	}

	// Jellyseerr API returns a wrapped response with "results" array
	var rawResponse struct {
		Results []JellyseerrSearchResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rawResponse); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	results := rawResponse.Results

	// Convert to MediaDetails and apply filters
	var filtered []MediaDetails
	for _, result := range results {
		// Filter by media type
		if filter.MediaType != "" && result.MediaType != filter.MediaType {
			continue
		}

		// Get detailed info from TMDB for filtering
		details, err := ss.GetMediaDetails(result.TmdbID, result.MediaType)
		if err != nil {
			continue
		}

		// Filter by year
		if filter.Year != "" {
			year := ""
			if details.ReleaseDate != "" && len(details.ReleaseDate) >= 4 {
				year = details.ReleaseDate[:4]
			} else if details.FirstAirDate != "" && len(details.FirstAirDate) >= 4 {
				year = details.FirstAirDate[:4]
			}
			if year != filter.Year {
				continue
			}
		}

		// Filter by rating
		if filter.MinRating > 0 && details.VoteAverage < filter.MinRating {
			continue
		}

		// Filter by genre
		if filter.Genre != "" {
			found := false
			for _, g := range details.Genres {
				if strings.Contains(strings.ToLower(g.Name), strings.ToLower(filter.Genre)) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		filtered = append(filtered, *details)
	}

	return filtered, nil
}

// GetMediaDetails gets detailed media information
func (ss *SmartSearch) GetMediaDetails(tmdbID int, mediaType string) (*MediaDetails, error) {
	if ss.apiKey == "" {
		return nil, fmt.Errorf("Jellyseerr API not configured")
	}

	// Use Jellyseerr's media endpoint
	url := fmt.Sprintf("%s/api/v1/media/%d", ss.jellyseerrURL, tmdbID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Api-Key", ss.apiKey)

	resp, err := ss.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Try getting from TMDB directly
		return ss.getTMDBDetails(tmdbID, mediaType)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get media failed: %d", resp.StatusCode)
	}

	// Parse Jellyseerr response
	var jsMedia struct {
		MediaType string `json:"mediaType"`
		TmdbID    int    `json:"tmdbId"`
		Title     string `json:"title"`
		Name      string `json:"name"`
		PosterPath string `json:"posterPath"`
		BackdropPath string `json:"backdropPath"`
		Overview   string `json:"overview"`
		ReleaseDate string `json:"releaseDate"`
		Status     string `json:"status"`
		Rating     float64 `json:"rating"`
	}

	var details MediaDetails
	if err := json.NewDecoder(resp.Body).Decode(&jsMedia); err != nil {
		return nil, err
	}

	details.TmdbID = jsMedia.TmdbID
	details.Title = jsMedia.Title
	details.Name = jsMedia.Name
	details.PosterPath = jsMedia.PosterPath
	details.BackdropPath = jsMedia.BackdropPath
	details.Overview = jsMedia.Overview
	details.ReleaseDate = jsMedia.ReleaseDate
	details.Status = jsMedia.Status
	details.VoteAverage = jsMedia.Rating

	return &details, nil
}

// getTMDBDetails gets details from TMDB API
func (ss *SmartSearch) getTMDBDetails(tmdbID int, mediaType string) (*MediaDetails, error) {
	// This would require TMDB API key
	// For now, return basic info
	return &MediaDetails{
		TmdbID: tmdbID,
	}, nil
}

// FormatSearchResultsWithDetails formats search results with rich info
func FormatSearchResultsWithDetails(results []MediaDetails, query string) string {
	if len(results) == 0 {
		return fmt.Sprintf("🔍 *搜索结果: %s*\n\n未找到匹配的内容", query)
	}

	msg := fmt.Sprintf("🔍 *搜索结果: %s*\n\n", query)
	msg += fmt.Sprintf("找到 %d 个结果\n\n", len(results))

	for i, result := range results {
		if i >= 10 {
			msg += fmt.Sprintf("\n... 还有 %d 个结果", len(results)-10)
			break
		}

		emoji := "🎬"
		if result.MediaType == "tv" {
			emoji = "📺"
		}

		title := result.Title
		if title == "" {
			title = result.Name
		}

		msg += fmt.Sprintf("%d. %s *%s*", i+1, emoji, title)

		// Year
		year := ""
		if result.ReleaseDate != "" && len(result.ReleaseDate) >= 4 {
			year = result.ReleaseDate[:4]
		} else if result.FirstAirDate != "" && len(result.FirstAirDate) >= 4 {
			year = result.FirstAirDate[:4]
		}
		if year != "" {
			msg += fmt.Sprintf(" (%s)", year)
		}

		// Status
		if result.Status != "" {
			status := map[string]string{
				"Released": "已上映",
				"Returning Series": "续订中",
				"Ended": "已完结",
				"Planned": "计划中",
				"In Production": "制作中",
			}[result.Status]
			if status != "" {
				msg += fmt.Sprintf(" - %s", status)
			}
		}

		msg += "\n"

		// Rating
		if result.VoteAverage > 0 {
			stars := "★"
			if result.VoteAverage >= 7 {
				stars = "★★★"
			} else if result.VoteAverage >= 5 {
				stars = "★★"
			}
			msg += fmt.Sprintf("   %s %.1f/10", stars, result.VoteAverage)
		}

		// Genres
		if len(result.Genres) > 0 {
			genres := make([]string, 0, len(result.Genres))
			for _, g := range result.Genres {
				genres = append(genres, g.Name)
			}
			if len(genres) > 0 {
				msg += fmt.Sprintf(" | %s", strings.Join(genres[:3], "/"))
				if len(genres) > 3 {
					msg += "+"
				}
			}
		}

		msg += fmt.Sprintf("\n   🆔 TMDB: `%d`\n\n", result.TmdbID)
	}

	msg += "💡 使用 `/request <TMDB_ID> <类型>` 发起请求"

	return msg
}

// TrackRequest adds a request to tracking
func TrackRequest(requestID, mediaTitle, mediaType string, tmdbID int, requesterID, requesterName string) {
	if requestTracker == nil {
		return
	}

	requestTracker.requestMutex.Lock()
	defer requestTracker.requestMutex.Unlock()

	requestTracker.pendingRequests[requestID] = &TrackedRequest{
		RequestID:     requestID,
		MediaTitle:    mediaTitle,
		MediaType:     mediaType,
		TmdbID:        tmdbID,
		RequesterID:   requesterID,
		RequesterName: requesterName,
		RequestedAt:   time.Now(),
	}

	// Save without calling saveTrackedRequests to avoid deadlock
	data, err := json.MarshalIndent(requestTracker.pendingRequests, "", "  ")
	if err != nil {
		log.Printf("Error marshaling tracked requests: %v", err)
		return
	}

	err = os.WriteFile("/root/emby-telegram-bot/tracked_requests.json", data, 0644)
	if err != nil {
		log.Printf("Error saving tracked requests: %v", err)
		return
	}

	log.Printf("Tracking request %s: %s", requestID, mediaTitle)
}

// UpdateRequestStatus updates tracked request status
func UpdateTrackedRequestStatus(requestID, status string) {
	if requestTracker == nil {
		return
	}

	requestTracker.requestMutex.Lock()
	defer requestTracker.requestMutex.Unlock()

	req, exists := requestTracker.pendingRequests[requestID]
	if !exists {
		return
	}

	now := time.Now()

	switch status {
	case "approved":
		req.ApprovedAt = &now
	case "available":
		req.AvailableAt = &now
		// Notify requester
		notifyRequesterAvailable(req)
		// Remove from tracking
		delete(requestTracker.pendingRequests, requestID)
		saveTrackedRequests()
		return
	}

	saveTrackedRequests()
}

// notifyRequesterAvailable notifies the requester when media is available
func notifyRequesterAvailable(req *TrackedRequest) {
	// Convert requester ID to int64
	requesterIDInt, err := strconv.ParseInt(req.RequesterID, 10, 64)
	if err != nil {
		log.Printf("Error parsing requester ID: %v", err)
		return
	}

	msg := fmt.Sprintf("🎉 *您请求的内容已可用！*\n\n")
	msg += fmt.Sprintf("📦 %s\n", req.MediaTitle)
	msg += fmt.Sprintf("\n🎬 快去观看吧！")
	msg += fmt.Sprintf("\n🕐 %s", time.Now().Format("2006-01-02 15:04"))

	if err := sendPrivateMessage(requesterIDInt, msg, nil); err != nil {
		log.Printf("Error notifying requester: %v", err)
	}
}

// checkPendingRequests checks for stuck requests and sends reminders
func checkPendingRequests() {
	if requestTracker == nil || autoReminder == nil {
		return
	}

	requestTracker.requestMutex.Lock()
	defer requestTracker.requestMutex.Unlock()

	now := time.Now()
	needsReminder := []*TrackedRequest{}

	for _, req := range requestTracker.pendingRequests {
		// Check if request needs reminder
		timeSinceRequest := now.Sub(req.RequestedAt)
		timeSinceLastReminder := autoReminder.remindAfter

		if req.LastReminderAt != nil {
			timeSinceLastReminder = now.Sub(*req.LastReminderAt)
		}

		if timeSinceRequest > autoReminder.remindAfter && timeSinceLastReminder > autoReminder.remindAfter {
			if req.ReminderCount < autoReminder.maxReminders {
				needsReminder = append(needsReminder, req)
			}
		}
	}

	// Send reminders
	for _, req := range needsReminder {
		sendAdminReminder(req)
		req.LastReminderAt = &now
		req.ReminderCount++
	}

	if len(needsReminder) > 0 {
		saveTrackedRequests()
	}
}

// sendAdminReminder sends reminder to admins about pending request
func sendAdminReminder(req *TrackedRequest) {
	emoji := "🎬"
	if req.MediaType == "tv" {
		emoji = "📺"
	}

	msg := fmt.Sprintf("⏰ *待处理请求提醒*\n\n")
	msg += fmt.Sprintf("%s %s\n", emoji, req.MediaTitle)
	msg += fmt.Sprintf("👤 请求者: %s\n", req.RequesterName)
	msg += fmt.Sprintf("⏱️ 已等待: %s\n", formatDuration(time.Since(req.RequestedAt)))
	msg += fmt.Sprintf("🔔 提醒次数: %d/%d\n", req.ReminderCount+1, autoReminder.maxReminders)
	msg += fmt.Sprintf("\n⚠️ 请及时处理")

	// Notify all admins
	adminsMutex.RLock()
	for userID := range admins {
		userIDInt, _ := strconv.ParseInt(userID, 10, 64)
		if err := sendPrivateMessage(userIDInt, msg, nil); err != nil {
			log.Printf("Error sending reminder to admin %s: %v", userID, err)
		}
	}
	adminsMutex.RUnlock()
}

// formatDuration formats a duration in human readable format
func formatDuration(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%d分钟", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		mins := int(d.Minutes()) % 60
		return fmt.Sprintf("%d小时%d分钟", hours, mins)
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%d天%d小时", days, hours)
}

// saveTrackedRequests saves tracked requests to file
func saveTrackedRequests() {
	if requestTracker == nil {
		return
	}

	requestTracker.requestMutex.Lock()
	defer requestTracker.requestMutex.Unlock()

	data, err := json.MarshalIndent(requestTracker.pendingRequests, "", "  ")
	if err != nil {
		log.Printf("Error marshaling tracked requests: %v", err)
		return
	}

	err = os.WriteFile("/root/emby-telegram-bot/tracked_requests.json", data, 0644)
	if err != nil {
		log.Printf("Error saving tracked requests: %v", err)
	}
}

// loadTrackedRequests loads tracked requests from file
func loadTrackedRequests() {
	if requestTracker == nil {
		return
	}

	data, err := os.ReadFile("/root/emby-telegram-bot/tracked_requests.json")
	if err != nil {
		log.Println("No existing tracked requests found")
		return
	}

	err = json.Unmarshal(data, &requestTracker.pendingRequests)
	if err != nil {
		log.Printf("Error loading tracked requests: %v", err)
		return
	}

	log.Printf("Loaded %d tracked requests", len(requestTracker.pendingRequests))
}

// GetStuckRequests returns requests that need attention
func GetStuckRequests() []*TrackedRequest {
	if requestTracker == nil {
		return nil
	}

	requestTracker.requestMutex.RLock()
	defer requestTracker.requestMutex.RUnlock()

	var stuck []*TrackedRequest
	now := time.Now()

	for _, req := range requestTracker.pendingRequests {
		if now.Sub(req.RequestedAt) > 24*time.Hour {
			stuck = append(stuck, req)
		}
	}

	return stuck
}

// FormatStuckRequests formats stuck requests for display
func FormatStuckRequests(requests []*TrackedRequest) string {
	if len(requests) == 0 {
		return "✅ 没有长时间未处理的请求"
	}

	msg := "⚠️ *长时间未处理的请求*\n\n"

	for i, req := range requests {
		if i >= 10 {
			msg += fmt.Sprintf("\n... 还有 %d 个请求", len(requests)-10)
			break
		}

		emoji := "🎬"
		if req.MediaType == "tv" {
			emoji = "📺"
		}

		msg += fmt.Sprintf("%d. %s %s\n", i+1, emoji, req.MediaTitle)
		msg += fmt.Sprintf("   👤 %s | ⏱️ %s\n\n", req.RequesterName, formatDuration(time.Since(req.RequestedAt)))
	}

	return msg
}
