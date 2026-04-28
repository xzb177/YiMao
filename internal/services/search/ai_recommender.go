// Package search provides AI-powered recommendation services.
package search

import (
	"fmt"
	"time"

	"github.com/xzb177/yimao/ai"
	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/internal/services"
)

// AIRecommender provides AI-powered mood-based recommendations.
type AIRecommender struct {
	tmdb        *services.TMDBClient
	mp          *services.MoviePilotClient
	recommender *Recommender
}

// NewAIRecommender creates a new AI recommender service.
func NewAIRecommender(tmdb *services.TMDBClient, mp *services.MoviePilotClient, recommender *Recommender) *AIRecommender {
	return &AIRecommender{
		tmdb:        tmdb,
		mp:          mp,
		recommender: recommender,
	}
}

// MoodRecommendation represents a mood-based recommendation result.
type MoodRecommendation struct {
	MoodLabel string
	Results   []services.SearchResult
}

// GetMoodRecommendations returns AI-powered mood-based recommendations.
func (r *AIRecommender) GetMoodRecommendations(mood string, count int) ([]services.SearchResult, error) {
	logger.Info("[AIRecommender] Calling AI mood recommendation: mood=%s", mood)

	// Use a channel to get results with timeout
	type aiResult struct {
		results []*ai.RecommendationResult
		err     error
	}
	resultChan := make(chan aiResult, 1)

	// Call AI in background
	go func() {
		aiResults, err := getAIRecommendations(mood, count)
		resultChan <- aiResult{results: aiResults, err: err}
	}()

	// Wait for AI with 5 second timeout
	select {
	case res := <-resultChan:
		if res.err != nil {
			logger.Info("[AIRecommender] AI recommendation failed: %v", res.err)
			return r.getTMDBBasedRecommendations(mood, count)
		}
		logger.Info("[AIRecommender] AI returned %d recommendations", len(res.results))

		// Convert AI results to TMDB search results for display
		var results []services.SearchResult
		for _, item := range res.results {
			searchResult, err := r.searchByTitleAndYear(item.Title, item.Year, item.MediaType)
			if err != nil {
				logger.Info("[AIRecommender] Failed to find TMDB entry for %s: %v", item.Title, err)
				continue
			}
			results = append(results, searchResult...)
		}

		// If AI didn't return enough results, supplement with TMDB
		if len(results) < count {
			fallbackResults, _ := r.getTMDBBasedRecommendations(mood, count-len(results))
			results = append(results, fallbackResults...)
		}

		// Shuffle and limit
		results = shuffleResults(results)

		if len(results) > count {
			results = results[:count]
		}

		return results, nil

	case <-time.After(5 * time.Second):
		logger.Info("[AIRecommender] AI timeout after 5s, using fallback")
		return r.getTMDBBasedRecommendations(mood, count)
	}
}

// getTMDBBasedRecommendations gets TMDB-based recommendations as fallback.
func (r *AIRecommender) getTMDBBasedRecommendations(mood string, count int) ([]services.SearchResult, error) {
	// Map mood to recommendation type
	typeMap := map[string]string{
		"放松":   "trending",
		"治愈":   "new",
		"烧脑":   "toprated",
		"感动":   "trending",
		"随机":   "random",
	}

	recType := typeMap[mood]
	if recType == "" {
		recType = "trending"
	}

	results, err := r.recommender.GetRecommendations(recType)
	if err != nil {
		return nil, err
	}

	if len(results) > count {
		results = results[:count]
	}

	return results, nil
}

// searchByTitleAndYear searches TMDB by title and year.
func (r *AIRecommender) searchByTitleAndYear(title string, year int, mediaType string) ([]services.SearchResult, error) {
	if r.tmdb == nil {
		return nil, fmt.Errorf("TMDB client not available")
	}

	query := fmt.Sprintf("%s %d", title, year)
	result, err := r.tmdb.SearchMedia(query, 1)
	if err != nil {
		return nil, err
	}

	// Filter by media type if specified
	var filtered []services.SearchResult
	for _, item := range result.Results {
		if mediaType == "movie" && item.MediaType == "movie" {
			filtered = append(filtered, services.SearchResult{
				ID:       item.ID,
				Title:    item.Title,
				Year:     services.FlexibleYear(year),
				Type:     "movie",
				Poster:   item.PosterPath,
				Rating:   item.VoteAverage,
				Overview: item.Overview,
			})
		} else if mediaType == "tv" && item.MediaType == "tv" {
			filtered = append(filtered, services.SearchResult{
				ID:       item.ID,
				Title:    item.Name,
				Year:     services.FlexibleYear(year),
				Type:     "tv",
				Poster:   item.PosterPath,
				Rating:   item.VoteAverage,
				Overview: item.Overview,
			})
		}
	}

	if len(filtered) == 0 && len(result.Results) > 0 {
		// Return first result if no match found
		item := result.Results[0]
		title := item.Title
		if title == "" {
			title = item.Name
		}
		return []services.SearchResult{{
			ID:       item.ID,
			Title:    title,
			Year:     services.FlexibleYear(year),
			Type:     item.MediaType,
			Poster:   item.PosterPath,
			Rating:   item.VoteAverage,
			Overview: item.Overview,
		}}, nil
	}

	return filtered, nil
}

// GetMoodLabel returns the display label for a mood.
func GetMoodLabel(mood string) string {
	moodLabels := map[string]string{
		"放松":   "😌 轻松治愈",
		"治愈":   "🧘 温暖治愈",
		"烧脑":   "🤯 烧脑刺激",
		"感动":   "😭 情绪共鸣",
		"随机":   "🎲 随机惊喜",
	}

	if label, ok := moodLabels[mood]; ok {
		return label
	}
	return "😌 轻松治愈"
}

// MapMoodKeyword maps English mood parameter to Chinese keywords.
func MapMoodKeyword(mood string) string {
	moodKeywords := map[string]string{
		"relax":     "放松",
		"healing":   "治愈",
		"mindblow":  "烧脑",
		"emotional": "感动",
		"random":    "随机",
	}

	if keyword, ok := moodKeywords[mood]; ok {
		return keyword
	}
	return "放松"
}

// shuffleResults shuffles search results.
func shuffleResults(results []services.SearchResult) []services.SearchResult {
	shuffled := make([]services.SearchResult, len(results))
	copy(shuffled, results)

	// Simple shuffle using time as seed
	seed := time.Now().UnixNano()
	for i := len(shuffled) - 1; i > 0; i-- {
		j := int(seed%int64(i+1))
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		seed = seed/7 + 1 // Simple pseudo-random progression
	}

	return shuffled
}

// getAIRecommendations calls the AI service for recommendations.
// This is a bridge function to the ai package.
func getAIRecommendations(mood string, count int) ([]*ai.RecommendationResult, error) {
	manager := ai.GetManager()
	if manager == nil || !manager.IsEnabled() {
		return nil, fmt.Errorf("AI service not enabled")
	}

	agent := manager.GetAgent()
	if agent == nil {
		return nil, fmt.Errorf("AI agent not available")
	}

	recommend := agent.GetRecommendation()
	if recommend == nil {
		return nil, fmt.Errorf("AI recommendation service not available")
	}

	return recommend.GetMoodBasedRecommendations(mood, count)
}
