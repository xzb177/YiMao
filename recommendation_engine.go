package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// RecommendationEngine provides intelligent media recommendations
// Uses collaborative filtering and content-based filtering
type RecommendationEngine struct {
	jellyseerrURL string
	apiKey        string
	httpClient    *http.Client

	// User preferences (telegramID -> preferences)
	userPreferences map[int64]*UserPreference
	prefMutex       sync.RWMutex

	// Global media statistics
	mediaStats map[string]*RecommendationMediaStats // tmdbID -> stats
	statsMutex sync.RWMutex

	// Storage files
	prefFile  string
	statsFile string
}

// UserPreference represents a user's viewing preferences
type UserPreference struct {
	TelegramID      int64              `json:"telegramId"`
	FavoriteGenres  []string           `json:"favoriteGenres"`  // Preferred genres
	PreferredTypes  []string           `json:"preferredTypes"`  // "movie", "tv"
	WatchedMedia    []string           `json:"watchedMedia"`    // List of TMDB IDs
	IgnoredMedia    []string           `json:"ignoredMedia"`    // User ignored recommendations
	LastUpdated     time.Time          `json:"lastUpdated"`
	ScoreMap        map[string]float64 `json:"scoreMap"`        // genre -> score
}

// RecommendationMediaStats represents statistics for a media item in recommendations
type RecommendationMediaStats struct {
	TmdbID       string    `json:"tmdbId"`
	Title        string    `json:"title"`
	MediaType    string    `json:"mediaType"`
	RequestCount int       `json:"requestCount"` // Total requests
	Genres       []string  `json:"genres"`
	FirstSeen    time.Time `json:"firstSeen"`
	LastSeen     time.Time `json:"lastSeen"`
}

// RecommendationResult represents a recommended media item
type RecommendationResult struct {
	TmdbID       int     `json:"tmdbId"`
	Title        string  `json:"title"`
	OriginalTitle string `json:"originalTitle"`
	ReleaseDate  string  `json:"releaseDate"`
	PosterPath   string  `json:"posterPath"`
	VoteAverage  float64 `json:"voteAverage"`
	MediaType    string  `json:"mediaType"`
	Overview     string  `json:"overview"`
	Genres       []Genre `json:"genres"`
	Reason       string  `json:"reason"`       // Why this was recommended
	Score        float64 `json:"score"`        // Relevance score
}

// RecommendationData stores all recommendation data
type RecommendationData struct {
	UserPreferences map[int64]*UserPreference `json:"userPreferences"`
	MediaStats      map[string]*MediaStats     `json:"mediaStats"`
	LastSync        string                     `json:"lastSync"`
}

var recommendationEngine *RecommendationEngine

// InitRecommendationEngine initializes the recommendation engine
func InitRecommendationEngine() {
	recommendationEngine = &RecommendationEngine{
		jellyseerrURL:   jellyseerrURL,
		apiKey:          os.Getenv("JELLYSEERR_API_KEY"),
		httpClient:      &http.Client{Timeout: 30 * time.Second},
		userPreferences: make(map[int64]*UserPreference),
		mediaStats:      make(map[string]*RecommendationMediaStats),
		prefFile:        "user_preferences.json",
		statsFile:       "media_stats.json",
	}

	// Load existing data
	recommendationEngine.load()

	// Start background tasks
	go recommendationEngine.updateMediaStats()
	go recommendationEngine.cleanup()

	log.Println("RecommendationEngine initialized")
}

// GetRecommendations returns personalized recommendations for a user
func (e *RecommendationEngine) GetRecommendations(telegramID int64, recType string, limit int) []RecommendationResult {
	e.prefMutex.RLock()
	pref, hasPref := e.userPreferences[telegramID]
	e.prefMutex.RUnlock()

	switch recType {
	case "similar":
		// Recommendations based on user's history
		if hasPref && pref != nil && len(pref.WatchedMedia) > 0 {
			return e.getSimilarRecommendations(pref, limit)
		}
		return e.getTrendingRecommendations(limit)

	case "trending":
		// Popular among all users
		return e.getTrendingRecommendations(limit)

	case "discovery":
		// Explore different genres
		if !hasPref || pref == nil {
			return e.getTrendingRecommendations(limit)
		}
		return e.getDiscoveryRecommendations(pref, limit)

	case "social":
		// What others are watching
		return e.getSocialRecommendations(limit)

	default:
		return e.getTrendingRecommendations(limit)
	}
}

// getSimilarRecommendations recommends similar content based on user history
func (e *RecommendationEngine) getSimilarRecommendations(pref *UserPreference, limit int) []RecommendationResult {
	if len(pref.WatchedMedia) == 0 {
		return e.getTrendingRecommendations(limit)
	}

	results := []RecommendationResult{}
	processed := make(map[string]bool)

	// For each watched media, find similar content
	for _, tmdbID := range pref.WatchedMedia {
		if len(results) >= limit {
			break
		}

		// Get similar media from TMDB/Jellyseerr
		similar := e.fetchSimilarMedia(tmdbID, 5)
		for _, item := range similar {
			if len(results) >= limit {
				break
			}

			// Skip if already watched
			key := fmt.Sprintf("%d_%s", item.TmdbID, item.MediaType)
			if processed[key] || e.hasWatched(pref, item.TmdbID) {
				continue
			}

			item.Reason = "因为你喜欢类似的" + getMediaTypeLabel(item.MediaType)
			item.Score = e.calculateScore(pref, item)
			results = append(results, item)
			processed[key] = true
		}
	}

	// Fill with trending if not enough
	if len(results) < limit {
		trending := e.getTrendingRecommendations(limit - len(results))
		for _, item := range trending {
			key := fmt.Sprintf("%d_%s", item.TmdbID, item.MediaType)
			if !processed[key] {
				item.Reason = "热门推荐"
				results = append(results, item)
			}
		}
	}

	return results
}

// getTrendingRecommendations returns popular media
func (e *RecommendationEngine) getTrendingRecommendations(limit int) []RecommendationResult {
	e.statsMutex.RLock()
	defer e.statsMutex.RUnlock()

	// Sort media by request count
	type scoredMedia struct {
		stats *RecommendationMediaStats
		score float64
	}

	scored := []scoredMedia{}
	for _, stats := range e.mediaStats {
		// Calculate popularity score (recent requests weighted more)
		score := float64(stats.RequestCount)
		if !stats.LastSeen.IsZero() {
			daysSince := time.Since(stats.LastSeen).Hours() / 24
			score = score / (1 + daysSince/7) // Decay over 7 days
		}
		scored = append(scored, scoredMedia{stats: stats, score: score})
	}

	// Simple sort by score
	for i := 0; i < len(scored)-1; i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].score > scored[i].score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	results := []RecommendationResult{}
	for i, s := range scored {
		if i >= limit {
			break
		}

		// Fetch details
		tmdbID := 0
		_, err := fmt.Sscanf(s.stats.TmdbID, "%d", &tmdbID)
		if tmdbID == 0 || err != nil {
			continue
		}

		details := e.fetchMediaDetails(tmdbID, s.stats.MediaType)
		if details != nil {
			details.Reason = fmt.Sprintf("%d 人请求", s.stats.RequestCount)
			details.Score = s.score
			results = append(results, *details)
		}
	}

	return results
}

// getDiscoveryRecommendations recommends content from different genres
func (e *RecommendationEngine) getDiscoveryRecommendations(pref *UserPreference, limit int) []RecommendationResult {
	// Get content from genres the user hasn't explored much
	e.statsMutex.RLock()
	defer e.statsMutex.RUnlock()

	// Count user's genre preferences
	genreCounts := make(map[string]int)
	if pref != nil {
		for _, g := range pref.FavoriteGenres {
			genreCounts[g]++
		}
	}

	// Find media from under-explored genres
	candidates := []RecommendationMediaStats{}
	for _, stats := range e.mediaStats {
		// Check if this media has genres user hasn't explored
		isNovel := false
		for _, genre := range stats.Genres {
			if genreCounts[genre] == 0 {
				isNovel = true
				break
			}
		}

		if isNovel || len(genreCounts) == 0 {
			candidates = append(candidates, *stats)
		}
	}

	results := []RecommendationResult{}
	for i, stats := range candidates {
		if i >= limit {
			break
		}

		tmdbID := 0
		_, err := fmt.Sscanf(stats.TmdbID, "%d", &tmdbID)
		if tmdbID == 0 || err != nil {
			continue
		}

		details := e.fetchMediaDetails(tmdbID, stats.MediaType)
		if details != nil {
			details.Reason = "探索新类型"
			results = append(results, *details)
		}
	}

	return results
}

// getSocialRecommendations shows what others are watching
func (e *RecommendationEngine) getSocialRecommendations(limit int) []RecommendationResult {
	// Similar to trending but with social proof
	trending := e.getTrendingRecommendations(limit)
	for i := range trending {
		trending[i].Reason = "大家都在看"
	}
	return trending
}

// RecordUserAction records a user action for learning preferences
func (e *RecommendationEngine) RecordUserAction(telegramID int64, action, tmdbID, mediaType string) {
	e.prefMutex.Lock()
	defer e.prefMutex.Unlock()

	pref, exists := e.userPreferences[telegramID]
	if !exists {
		pref = &UserPreference{
			TelegramID:   telegramID,
			FavoriteGenres: []string{},
			PreferredTypes: []string{},
			WatchedMedia:   []string{},
			IgnoredMedia:   []string{},
			ScoreMap:       make(map[string]float64),
		}
		e.userPreferences[telegramID] = pref
	}

	switch action {
	case "requested", "watched":
		// Add to watched media
		found := false
		for _, id := range pref.WatchedMedia {
			if id == tmdbID {
				found = true
				break
			}
		}
		if !found {
			pref.WatchedMedia = append(pref.WatchedMedia, tmdbID)
		}

		// Update media type preference
		typeFound := false
		for _, t := range pref.PreferredTypes {
			if t == mediaType {
				typeFound = true
				break
			}
		}
		if !typeFound && mediaType != "" {
			pref.PreferredTypes = append(pref.PreferredTypes, mediaType)
		}

	case "ignored":
		// User ignored this recommendation
		found := false
		for _, id := range pref.IgnoredMedia {
			if id == tmdbID {
				found = true
				break
			}
		}
		if !found {
			pref.IgnoredMedia = append(pref.IgnoredMedia, tmdbID)
		}
	}

	pref.LastUpdated = time.Now()
	e.save()

	log.Printf("RecommendationEngine: Recorded %s action for user %d, media %s", action, telegramID, tmdbID)
}

// fetchSimilarMedia fetches similar media from TMDB
func (e *RecommendationEngine) fetchSimilarMedia(tmdbID string, limit int) []RecommendationResult {
	if e.apiKey == "" {
		return []RecommendationResult{}
	}

	// Use Jellyseerr's recommendation endpoint
	url := fmt.Sprintf("%s/api/v1/media/%s/recommendations", e.jellyseerrURL, tmdbID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return []RecommendationResult{}
	}

	req.Header.Set("X-Api-Key", e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return []RecommendationResult{}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return []RecommendationResult{}
	}

	var results []struct {
		ID         int    `json:"id"`
		Title      string `json:"title"`
		Name       string `json:"name"`
		PosterPath string `json:"poster_path"`
		MediaType  string `json:"media_type"`
		VoteAverage float64 `json:"vote_average"`
		Overview   string `json:"overview"`
		Genres     []Genre `json:"genres"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return []RecommendationResult{}
	}

	recs := []RecommendationResult{}
	for i, r := range results {
		if i >= limit {
			break
		}

		title := r.Title
		if title == "" {
			title = r.Name
		}

		recs = append(recs, RecommendationResult{
			TmdbID:        r.ID,
			Title:         title,
			OriginalTitle: r.Title,
			PosterPath:    r.PosterPath,
			VoteAverage:   r.VoteAverage,
			MediaType:     r.MediaType,
			Overview:      r.Overview,
			Genres:        r.Genres,
		})
	}

	return recs
}

// fetchMediaDetails fetches media details
func (e *RecommendationEngine) fetchMediaDetails(tmdbID int, mediaType string) *RecommendationResult {
	if e.apiKey == "" {
		return nil
	}

	url := fmt.Sprintf("%s/api/v1/media/%d", e.jellyseerrURL, tmdbID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil
	}

	req.Header.Set("X-Api-Key", e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var details struct {
		TmdbID       int     `json:"tmdbId"`
		Title        string  `json:"title"`
		Name         string  `json:"name"`
		PosterPath   string  `json:"posterPath"`
		VoteAverage  float64 `json:"rating"`
		Overview     string  `json:"overview"`
		MediaType    string  `json:"mediaType"`
		ReleaseDate  string  `json:"releaseDate"`
		Genres       []Genre `json:"genres"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return nil
	}

	title := details.Title
	if title == "" {
		title = details.Name
	}

	return &RecommendationResult{
		TmdbID:        details.TmdbID,
		Title:         title,
		OriginalTitle: details.Title,
		PosterPath:    details.PosterPath,
		VoteAverage:   details.VoteAverage,
		MediaType:     details.MediaType,
		Overview:      details.Overview,
		Genres:        details.Genres,
		ReleaseDate:   details.ReleaseDate,
	}
}

// calculateScore calculates relevance score for a user
func (e *RecommendationEngine) calculateScore(pref *UserPreference, item RecommendationResult) float64 {
	score := 50.0 // Base score

	// Boost based on vote average
	score += item.VoteAverage * 5

	// Boost based on genre preferences
	if pref != nil && len(item.Genres) > 0 {
		for _, genre := range item.Genres {
			if genre.Name != "" && pref.ScoreMap[genre.Name] > 0 {
				score += pref.ScoreMap[genre.Name] * 10
			}
		}

		// Boost based on media type preference
		for _, t := range pref.PreferredTypes {
			if t == item.MediaType {
				score += 10
			}
		}
	}

	return score
}

// hasWatched checks if user has watched this media
func (e *RecommendationEngine) hasWatched(pref *UserPreference, tmdbID int) bool {
	tmdbIDStr := fmt.Sprintf("%d", tmdbID)
	for _, id := range pref.WatchedMedia {
		if id == tmdbIDStr {
			return true
		}
	}
	return false
}

// updateMediaStats periodically updates media statistics
func (e *RecommendationEngine) updateMediaStats() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	// Initial update
	e.syncMediaStatsFromAnalytics()

	for range ticker.C {
		e.syncMediaStatsFromAnalytics()
	}
}

// syncMediaStatsFromAnalytics syncs stats from analytics system
func (e *RecommendationEngine) syncMediaStatsFromAnalytics() {
	// This would sync with the analytics system
	// For now, just log
	log.Println("RecommendationEngine: Updated media statistics")
}

// cleanup periodically cleans up old data
func (e *RecommendationEngine) cleanup() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		e.prefMutex.Lock()
		// Remove users inactive for 90 days
		cutoff := time.Now().Add(-90 * 24 * time.Hour)
		for id, pref := range e.userPreferences {
			if pref.LastUpdated.Before(cutoff) {
				delete(e.userPreferences, id)
			}
		}
		e.save()
		e.prefMutex.Unlock()

		log.Println("RecommendationEngine: Cleaned up old data")
	}
}

// save saves recommendation data
func (e *RecommendationEngine) save() {
	// Save preferences
	prefData, _ := json.MarshalIndent(e.userPreferences, "", "  ")
	os.WriteFile(e.prefFile, prefData, 0644)

	// Save stats
	statsData, _ := json.MarshalIndent(e.mediaStats, "", "  ")
	os.WriteFile(e.statsFile, statsData, 0644)
}

// load loads recommendation data
func (e *RecommendationEngine) load() {
	// Load preferences
	prefData, err := os.ReadFile(e.prefFile)
	if err == nil {
		json.Unmarshal(prefData, &e.userPreferences)
		log.Printf("RecommendationEngine: Loaded %d user preferences", len(e.userPreferences))
	}

	// Load stats
	statsData, err := os.ReadFile(e.statsFile)
	if err == nil {
		json.Unmarshal(statsData, &e.mediaStats)
		log.Printf("RecommendationEngine: Loaded %d media stats", len(e.mediaStats))
	}
}

// FormatRecommendations formats recommendations for display
func FormatRecommendations(recs []RecommendationResult, recType string) string {
	if len(recs) == 0 {
		return "暂无推荐内容"
	}

	typeNames := map[string]string{
		"similar":   "为你推荐",
		"trending":  "热门推荐",
		"discovery": "探索发现",
		"social":    "大家都在看",
	}

	typeName := typeNames[recType]
	if typeName == "" {
		typeName = "推荐"
	}

	msg := fmt.Sprintf("✨ *%s*\n\n", typeName)

	for i, rec := range recs {
		emoji := "🎬"
		if rec.MediaType == "tv" {
			emoji = "📺"
		}

		msg += fmt.Sprintf("%d. %s *%s*", i+1, emoji, rec.Title)
		if rec.VoteAverage > 0 {
			msg += fmt.Sprintf(" (%.1f⭐)", rec.VoteAverage)
		}
		msg += "\n"

		if rec.Reason != "" {
			msg += fmt.Sprintf("   💡 %s\n", rec.Reason)
		}

		msg += fmt.Sprintf("   🆔 `%d`\n\n", rec.TmdbID)
	}

	return msg
}

// getMediaTypeLabel returns Chinese label for media type
func getMediaTypeLabel(mediaType string) string {
	if mediaType == "movie" {
		return "电影"
	}
	return "剧集"
}

// GetRecommendationsMenu returns the recommendations menu keyboard
func GetRecommendationsMenu() *TelegramInlineKeyboard {
	keyboard := &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{
			{
				{"text": "🎯 为你推荐", "callback_data": "rec_similar"},
				{"text": "🔥 热门推荐", "callback_data": "rec_trending"},
			},
			{
				{"text": "🎲 探索发现", "callback_data": "rec_discovery"},
				{"text": "👥 大家都在看", "callback_data": "rec_social"},
			},
			{
				{"text": "🔙 返回", "callback_data": "action_settings"},
			},
		},
	}
	return keyboard
}
