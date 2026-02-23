package services

import (
	"fmt"
	"log"
	"time"

	"emby-telegram-bot/internal/session"
)

// AIService handles AI-powered media recommendations
type AIService struct {
	moviepilot *MoviePilotClient
	tmdb       *TMDBClient
	sessMgr    *session.Manager
}

// NewAIService creates a new AI service
func NewAIService(moviepilot *MoviePilotClient, sessMgr *session.Manager) *AIService {
	return &AIService{
		moviepilot: moviepilot,
		tmdb:       nil, // Will be set separately
		sessMgr:    sessMgr,
	}
}

// SetTMDBClient sets the TMDB client
func (s *AIService) SetTMDBClient(tmdb *TMDBClient) {
	s.tmdb = tmdb
}

// TrendingResult represents trending media results
type TrendingResult struct {
	MediaType string    // "movie" or "tv"
	Items     []session.SearchItem
	Source    string    // "trending", "hot", "new"
}

// GetTrendingMovies gets trending movies from TMDB
func (s *AIService) GetTrendingMovies(userID int64, page int) (*TrendingResult, error) {
	log.Printf("[AIService] Getting trending movies from TMDB, page=%d", page)

	// Try TMDB first
	if s.tmdb != nil {
		result, err := s.tmdb.GetTrendingMovies("week")
		if err == nil && len(result.Results) > 0 {
			items := s.convertTMDBResults(result.Results)
			s.cacheResults(userID, items, "ai_trending")
			return &TrendingResult{
				MediaType: "movie",
				Items:     items,
				Source:    "trending",
			}, nil
		}
		log.Printf("[AIService] TMDB trending failed: %v, using fallback", err)
	}

	// Fallback to MoviePilot search
	result, err := s.moviepilot.SearchMedia("2024", page)
	if err != nil {
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

// GetHotTV gets hot TV shows from TMDB
func (s *AIService) GetHotTV(userID int64, page int) (*TrendingResult, error) {
	log.Printf("[AIService] Getting hot TV shows from TMDB, page=%d", page)

	// Try TMDB first
	if s.tmdb != nil {
		result, err := s.tmdb.GetTrendingTV("week")
		if err == nil && len(result.Results) > 0 {
			items := s.convertTMDBResults(result.Results)
			s.cacheResults(userID, items, "ai_hot")
			return &TrendingResult{
				MediaType: "tv",
				Items:     items,
				Source:    "hot",
			}, nil
		}
		log.Printf("[AIService] TMDB trending TV failed: %v, using fallback", err)
	}

	// Fallback to MoviePilot search
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

// GetNewMovies gets newly released movies from TMDB
func (s *AIService) GetNewMovies(userID int64, page int) (*TrendingResult, error) {
	log.Printf("[AIService] Getting new movies from TMDB, page=%d", page)

	// Try TMDB first - use now_playing for newest
	if s.tmdb != nil {
		result, err := s.tmdb.GetNowPlayingMovies(page)
		if err == nil && len(result.Results) > 0 {
			items := s.convertTMDBPopularResults(result.Results)
			s.cacheResults(userID, items, "ai_new")
			return &TrendingResult{
				MediaType: "movie",
				Items:     items,
				Source:    "new",
			}, nil
		}
		log.Printf("[AIService] TMDB now playing failed: %v, trying upcoming", err)

		// Try upcoming as fallback
		upcomingResult, upcomingErr := s.tmdb.GetUpcomingMovies(page)
		if upcomingErr == nil && len(upcomingResult.Results) > 0 {
			items := s.convertTMDBPopularResults(upcomingResult.Results)
			s.cacheResults(userID, items, "ai_new")
			return &TrendingResult{
				MediaType: "movie",
				Items:     items,
				Source:    "new",
			}, nil
		}
		log.Printf("[AIService] TMDB upcoming failed: %v, using fallback", upcomingErr)
	}

	// Fallback to MoviePilot search
	result, err := s.moviepilot.SearchMedia("2025", page)
	if err != nil {
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

// GetTopRated gets top rated movies from TMDB
func (s *AIService) GetTopRated(userID int64, page int) (*TrendingResult, error) {
	log.Printf("[AIService] Getting top rated movies from TMDB, page=%d", page)

	if s.tmdb != nil {
		result, err := s.tmdb.GetTopRatedMovies(page)
		if err == nil && len(result.Results) > 0 {
			items := s.convertTMDBPopularResults(result.Results)
			s.cacheResults(userID, items, "ai_toprated")
			return &TrendingResult{
				MediaType: "movie",
				Items:     items,
				Source:    "toprated",
			}, nil
		}
		log.Printf("[AIService] TMDB top rated failed: %v, using fallback", err)
	}

	// Fallback
	return s.GetTrendingMovies(userID, page)
}

// GetRandom gets random recommendations from top rated
func (s *AIService) GetRandom(userID int64, count int) (*TrendingResult, error) {
	log.Printf("[AIService] Getting random recommendations from TMDB, count=%d", count)

	// Get top rated and shuffle
	if s.tmdb != nil {
		result, err := s.tmdb.GetTopRatedMovies(1)
		if err == nil && len(result.Results) > 0 {
			// Shuffle results for randomness
			items := s.convertTMDBPopularResults(result.Results)
			// Simple shuffle
			for i := len(items) - 1; i > 0; i-- {
				j := int(time.Now().UnixNano()) % (i + 1)
				items[i], items[j] = items[j], items[i]
			}
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
		log.Printf("[AIService] TMDB random failed: %v, using fallback", err)
	}

	// Fallback
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
	case "ai_trending", "trending":
		return "🔥 当前热门影片，观众评分很高"
	case "ai_hot", "hot":
		return "📺 热播剧集，追剧达人推荐"
	case "ai_new", "new":
		return "🆕 最新上映，值得一看"
	case "ai_random", "random":
		return "🎲 为你随机挑选的佳作"
	case "ai_toprated", "toprated":
		return "⭐ 高分佳作，必看经典"
	default:
		return "💡 精选推荐"
	}
}

// convertTMDBResults converts TMDB trending results to session.SearchItem
func (s *AIService) convertTMDBResults(results []TMDBTrendingMediaInfo) []session.SearchItem {
	items := make([]session.SearchItem, len(results))
	for i, media := range results {
		items[i] = s.convertTMDBToSearchItem(media)
	}
	return items
}

// convertTMDBPopularResults converts TMDB popular results to session.SearchItem
func (s *AIService) convertTMDBPopularResults(results []TMDBTrendingMediaInfo) []session.SearchItem {
	items := make([]session.SearchItem, len(results))
	for i, media := range results {
		items[i] = s.convertTMDBToSearchItem(media)
	}
	return items
}

// convertTMDBToSearchItem converts a TMDB media item to session.SearchItem
func (s *AIService) convertTMDBToSearchItem(media TMDBTrendingMediaInfo) session.SearchItem {
	// Get title
	title := media.Title
	if title == "" {
		title = media.Name
	}
	if title == "" {
		title = media.OriginalTitle
	}
	if title == "" {
		title = media.OriginalName
	}

	// Get year
	year := 0
	if media.ReleaseDate != "" && len(media.ReleaseDate) >= 4 {
		fmt.Sscanf(media.ReleaseDate[:4], "%d", &year)
	} else if media.FirstAirDate != "" && len(media.FirstAirDate) >= 4 {
		fmt.Sscanf(media.FirstAirDate[:4], "%d", &year)
	}

	// Get media type
	mediaType := "电影"
	if media.MediaType == "tv" {
		mediaType = "电视剧"
	}

	// Get poster
	poster := ""
	if media.PosterPath != "" {
		poster = "https://image.tmdb.org/t/p/w500" + media.PosterPath
	}

	return session.SearchItem{
		ID:     fmt.Sprintf("%d", media.ID),
		Title:  title,
		Year:   year,
		Type:   mediaType,
		Poster: poster,
		Rating: media.VoteAverage,
	}
}
