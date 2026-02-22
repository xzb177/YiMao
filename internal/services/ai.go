package services

import (
	"fmt"
	"log"

	"emby-telegram-bot/internal/session"
)

// AIService handles AI-powered media recommendations
type AIService struct {
	moviepilot *MoviePilotClient
	sessMgr    *session.Manager
}

// NewAIService creates a new AI service
func NewAIService(moviepilot *MoviePilotClient, sessMgr *session.Manager) *AIService {
	return &AIService{
		moviepilot: moviepilot,
		sessMgr:    sessMgr,
	}
}

// TrendingResult represents trending media results
type TrendingResult struct {
	MediaType string    // "movie" or "tv"
	Items     []session.SearchItem
	Source    string    // "trending", "hot", "new"
}

// GetTrendingMovies gets trending movies using search fallback
func (s *AIService) GetTrendingMovies(userID int64, page int) (*TrendingResult, error) {
	log.Printf("[AIService] Getting trending movies, page=%d", page)

	// Use search as fallback - search for recent popular movies
	result, err := s.moviepilot.SearchMedia("2024", page)
	if err != nil {
		// Try another query
		result, err = s.moviepilot.SearchMedia("movie", page)
		if err != nil {
			return nil, fmt.Errorf("failed to get trending movies: %w", err)
		}
	}

	items := s.convertSearchResults(result.Results)
	s.cacheResults(userID, items, "ai_trending")

	return &TrendingResult{
		MediaType: "movie",
		Items:     items,
		Source:    "trending",
	}, nil
}

// GetHotTV gets hot TV shows using search fallback
func (s *AIService) GetHotTV(userID int64, page int) (*TrendingResult, error) {
	log.Printf("[AIService] Getting hot TV shows, page=%d", page)

	// Search for popular TV shows
	result, err := s.moviepilot.SearchMedia("tv", page)
	if err != nil {
		return nil, fmt.Errorf("failed to get hot TV: %w", err)
	}

	// Filter to TV shows only
	var tvItems []session.SearchItem
	for _, item := range result.Results {
		if item.Type == "电视剧" || item.Type == "tv" {
			tvItems = append(tvItems, s.convertSearchResultToSearchItem(item))
		}
	}

	s.cacheResults(userID, tvItems, "ai_hot")

	return &TrendingResult{
		MediaType: "tv",
		Items:     tvItems,
		Source:    "hot",
	}, nil
}

// GetNewMovies gets newly released movies
func (s *AIService) GetNewMovies(userID int64, page int) (*TrendingResult, error) {
	log.Printf("[AIService] Getting new movies, page=%d", page)

	// Search for 2024-2025 movies
	result, err := s.moviepilot.SearchMedia("2025", page)
	if err != nil {
		// Fallback to 2024
		result, err = s.moviepilot.SearchMedia("2024", page)
		if err != nil {
			return nil, fmt.Errorf("failed to get new movies: %w", err)
		}
	}

	// Filter to movies only
	var movieItems []session.SearchItem
	for _, item := range result.Results {
		if item.Type == "电影" || item.Type == "movie" {
			movieItems = append(movieItems, s.convertSearchResultToSearchItem(item))
		}
	}

	s.cacheResults(userID, movieItems, "ai_new")

	return &TrendingResult{
		MediaType: "movie",
		Items:     movieItems,
		Source:    "new",
	}, nil
}

// GetRandom gets random recommendations
func (s *AIService) GetRandom(userID int64, count int) (*TrendingResult, error) {
	log.Printf("[AIService] Getting random recommendations, count=%d", count)

	// Get trending movies as base
	result, err := s.moviepilot.SearchMedia("movie", 1)
	if err != nil {
		return nil, fmt.Errorf("failed to get random: %w", err)
	}

	items := s.convertSearchResults(result.Results)
	if len(items) > count {
		items = items[:count]
	}

	s.cacheResults(userID, items, "ai_random")

	return &TrendingResult{
		MediaType: "movie",
		Items:     items,
		Source:    "random",
	}, nil
}

// convertSearchResults converts MoviePilot search results to session.SearchItem
func (s *AIService) convertSearchResults(mediaList []SearchResult) []session.SearchItem {
	items := make([]session.SearchItem, len(mediaList))
	for i, media := range mediaList {
		items[i] = s.convertSearchResultToSearchItem(media)
	}
	return items
}

// convertSearchResultToSearchItem converts a single MoviePilot search result to session.SearchItem
func (s *AIService) convertSearchResultToSearchItem(media SearchResult) session.SearchItem {
	return session.SearchItem{
		ID:     fmt.Sprintf("%d", media.ID),
		Title:  media.Title,
		Year:   media.Year.Int(),
		Type:   string(media.Type),
		Poster: media.Poster,
		Rating: media.Rating,
	}
}

// cacheResults caches AI results in session
func (s *AIService) cacheResults(userID int64, items []session.SearchItem, source string) {
	sess := s.sessMgr.GetOrCreate(userID)

	// Cache each item as AI recommendation
	for _, item := range items {
		tmdbID := 0
		fmt.Sscanf(item.ID, "%d", &tmdbID)

		rec := &session.AIRecommendationItem{
			TmdbID:    tmdbID,
			Title:     item.Title,
			Year:      item.Year,
			Rating:    item.Rating,
			MediaType: item.Type,
			Reason:    s.generateReason(source),
		}

		sess.CacheAIItem(rec)
	}

	// Save navigation history
	sess.PushNavEntry("ai_"+source, "", "")
}

// generateReason generates an AI recommendation reason
func (s *AIService) generateReason(source string) string {
	switch source {
	case "ai_trending":
		return "🔥 当前热门影片，观众评分很高"
	case "ai_hot":
		return "📺 热播剧集，追剧达人推荐"
	case "ai_new":
		return "🆕 最新上映，值得一看"
	case "ai_random":
		return "🎲 为你随机挑选的佳作"
	default:
		return "💡 精选推荐"
	}
}
