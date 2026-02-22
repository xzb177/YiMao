package services

import (
	"fmt"
	"log"
	"strconv"

	"emby-telegram-bot/internal/session"
)

// SearchService handles media search operations
type SearchService struct {
	jellyseerr *JellyseerrClient
	sessMgr    *session.Manager
}

// NewSearchService creates a new search service
func NewSearchService(jellyseerr *JellyseerrClient, sessMgr *session.Manager) *SearchService {
	return &SearchService{
		jellyseerr: jellyseerr,
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

	// Call Jellyseerr search API
	result, err := s.jellyseerr.Search(query, page)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Convert results
	items := make([]session.SearchItem, len(result.Results))
	for i, media := range result.Results {
		year := 0
		if media.ReleaseDate != "" && len(media.ReleaseDate) >= 4 {
			fmt.Sscanf(media.ReleaseDate[:4], "%d", &year)
		}

		title := media.Title
		if title == "" {
			title = media.Name
		}

		items[i] = session.SearchItem{
			ID:     strconv.Itoa(media.ID),
			Title:  title,
			Year:   year,
			Type:   media.MediaType,
			Poster: media.PosterPath,
			Rating: media.VoteAverage,
		}
	}

	// Store results in session for pagination
	sess := s.sessMgr.GetOrCreate(userID)
	sess.SetSearchResults(items, page, query)

	return &ExtendedSearchResult{
		Query:   query,
		Page:    page,
		Total:   result.TotalResults,
		Results: items,
	}, nil
}

// GetCachedResults retrieves cached search results from session
func (s *SearchService) GetCachedResults(userID int64) ([]session.SearchItem, int, string, bool) {
	sess := s.sessMgr.GetOrCreate(userID)
	return sess.GetSearchResults()
}
