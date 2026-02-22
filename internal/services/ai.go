package services

import (
	"fmt"
	"log"

	"emby-telegram-bot/internal/session"
)

// AIService handles AI-powered media recommendations
type AIService struct {
	jellyseerr *JellyseerrClient
	sessMgr    *session.Manager
}

// NewAIService creates a new AI service
func NewAIService(jellyseerr *JellyseerrClient, sessMgr *session.Manager) *AIService {
	return &AIService{
		jellyseerr: jellyseerr,
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
	result, err := s.jellyseerr.Search("2024", page)
	if err != nil {
		// Try another query
		result, err = s.jellyseerr.Search("movie", page)
		if err != nil {
			return nil, fmt.Errorf("failed to get trending movies: %w", err)
		}
	}

	items := s.convertToSearchItems(result.Results)
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
	result, err := s.jellyseerr.Search("tv", page)
	if err != nil {
		return nil, fmt.Errorf("failed to get hot TV: %w", err)
	}

	// Filter to TV shows only
	var tvItems []session.SearchItem
	for _, item := range result.Results {
		if item.MediaType == "tv" {
			tvItems = append(tvItems, s.convertToSearchItem(item))
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
	result, err := s.jellyseerr.Search("2025", page)
	if err != nil {
		// Fallback to 2024
		result, err = s.jellyseerr.Search("2024", page)
		if err != nil {
			return nil, fmt.Errorf("failed to get new movies: %w", err)
		}
	}

	// Filter to movies only
	var movieItems []session.SearchItem
	for _, item := range result.Results {
		if item.MediaType == "movie" {
			movieItems = append(movieItems, s.convertToSearchItem(item))
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
	result, err := s.jellyseerr.Search("movie", 1)
	if err != nil {
		return nil, fmt.Errorf("failed to get random: %w", err)
	}

	items := s.convertToSearchItems(result.Results)
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

// convertToSearchItems converts Jellyseerr media results to session.SearchItem
func (s *AIService) convertToSearchItems(mediaList []MediaInfo) []session.SearchItem {
	items := make([]session.SearchItem, len(mediaList))
	for i, media := range mediaList {
		items[i] = s.convertToSearchItem(media)
	}
	return items
}

// convertToSearchItem converts a single Jellyseerr media result to session.SearchItem
func (s *AIService) convertToSearchItem(media MediaInfo) session.SearchItem {
	year := 0
	if media.ReleaseDate != "" && len(media.ReleaseDate) >= 4 {
		fmt.Sscanf(media.ReleaseDate[:4], "%d", &year)
	}

	title := media.Title
	if title == "" {
		title = media.Name
	}

	return session.SearchItem{
		ID:     fmt.Sprintf("%d", media.ID),
		Title:  title,
		Year:   year,
		Type:   media.MediaType,
		Poster: media.PosterPath,
		Rating: media.VoteAverage,
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
