package services

import (
	"fmt"
	"log"
	"strconv"

	"emby-telegram-bot/internal/session"
)

// SearchService handles media search operations
type SearchService struct {
	moviepilot *MoviePilotClient
	sessMgr    *session.Manager
}

// NewSearchService creates a new search service
func NewSearchService(moviepilot *MoviePilotClient, sessMgr *session.Manager) *SearchService {
	return &SearchService{
		moviepilot: moviepilot,
		sessMgr:    sessMgr,
	}
}

// ExtendedSearchResult represents an extended search result with session items
type ExtendedSearchResult struct {
	Query   string
	Page    int
	Total   int
	Results []session.SearchItem
}

// Search searches for media by title
func (s *SearchService) Search(userID int64, query string, page int) (*ExtendedSearchResult, error) {
	log.Printf("[SearchService] Searching: query=%s, page=%d", query, page)

	// Call MoviePilot search API
	result, err := s.moviepilot.SearchMedia(query, page)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Convert results - MoviePilot returns array directly
	items := make([]session.SearchItem, len(result.Results))
	for i, media := range result.Results {
		// Map MoviePilot type to internal type
		mediaType := "movie"
		if media.Type == "电视剧" || media.Type == "tv" {
			mediaType = "tv"
		}

		items[i] = session.SearchItem{
			ID:       strconv.Itoa(media.ID),
			Title:    media.Title,
			Year:     media.Year.Int(),
			Type:     mediaType,
			Poster:   media.Poster,
			Rating:   media.Rating,
			Overview: media.Overview,
		}
	}

	// Store results in session for pagination
	sess := s.sessMgr.GetOrCreate(userID)
	sess.SetSearchResults(items, page, query)

	// Total is approximated as 20 per page * current page + current results
	total := page * 20
	if len(items) > 0 {
		total = (page-1)*20 + len(items)
	}

	return &ExtendedSearchResult{
		Query:   query,
		Page:    page,
		Total:   total,
		Results: items,
	}, nil
}

// GetCachedResults retrieves cached search results from session
func (s *SearchService) GetCachedResults(userID int64) ([]session.SearchItem, int, string, bool) {
	sess := s.sessMgr.GetOrCreate(userID)
	return sess.GetSearchResults()
}
