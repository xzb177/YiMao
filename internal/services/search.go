package services

import (
	"fmt"
	"strconv"

	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/logger"
)

// SearchService handles media search operations
type SearchService struct {
	moviepilot *MoviePilotClient
	sessMgr    *session.Manager
	tmdb       *TMDBClient
}

// NewSearchService creates a new search service
func NewSearchService(moviepilot *MoviePilotClient, sessMgr *session.Manager) *SearchService {
	return &SearchService{
		moviepilot: moviepilot,
		sessMgr:    sessMgr,
	}
}

// SetTMDBClient sets the TMDB client for fetching complete season info
func (s *SearchService) SetTMDBClient(tmdb *TMDBClient) {
	s.tmdb = tmdb
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
	logger.Info("[SearchService] Searching: query=%s, page=%d", query, page)

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

		item := session.SearchItem{
			ID:       strconv.Itoa(media.ID),
			Title:    media.Title,
			Year:     media.Year.Int(),
			Type:     mediaType,
			Poster:   media.Poster,
			Rating:   media.Rating,
			Overview: media.Overview,
		}

		// For TV shows, fetch season info asynchronously
		if mediaType == "tv" && media.ID > 0 {
			if seasons := s.fetchSeasons(media.ID); len(seasons) > 0 {
				item.Seasons = seasons
			}
		}

		items[i] = item
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

// fetchSeasons fetches season information for a TV show
// Prefers TMDB for complete season list, falls back to MoviePilot
func (s *SearchService) fetchSeasons(tmdbID int) []session.Season {
	// Try TMDB first for complete season list
	if s.tmdb != nil {
		tmdbDetails, err := s.tmdb.GetTVDetailsWithSeasons(tmdbID)
		if err == nil && len(tmdbDetails.Seasons) > 0 {
			seasons := make([]session.Season, len(tmdbDetails.Seasons))
			for i, season := range tmdbDetails.Seasons {
				seasons[i] = session.Season{
					SeasonNumber: season.SeasonNumber,
					EpisodeCount: season.EpisodeCount,
					Name:         season.Name,
				}
			}
			logger.Info("[SearchService] Using TMDB seasons for TMDB ID %d: %d seasons", tmdbID, len(seasons))
			return seasons
		}
	}

	// Fallback to MoviePilot
	mediaInfo, err := s.moviepilot.GetMediaInfo(tmdbID, MediaTypeTV)
	if err != nil {
		logger.Info("[SearchService] Failed to get media info for seasons: %v", err)
		return nil
	}

	// Convert seasons from SeasonInfo (preferred)
	if len(mediaInfo.SeasonInfo) > 0 {
		seasons := make([]session.Season, len(mediaInfo.SeasonInfo))
		for i, s := range mediaInfo.SeasonInfo {
			seasons[i] = session.Season{
				SeasonNumber: s.SeasonNumber,
				EpisodeCount: s.EpisodeCount,
				Name:         s.Name,
			}
		}
		logger.Info("[SearchService] Using MoviePilot seasons for TMDB ID %d: %d seasons", tmdbID, len(seasons))
		return seasons
	}

	// Try to parse seasons as a map/object
	if mediaInfo.Seasons != nil {
		// Seasons is a map, extract season numbers
		seasonsMap, ok := mediaInfo.Seasons.(map[string]interface{})
		if ok && len(seasonsMap) > 0 {
			seasons := make([]session.Season, 0, len(seasonsMap))
			for key := range seasonsMap {
				var seasonNum int
				fmt.Sscanf(key, "%d", &seasonNum)
				if seasonNum > 0 {
					seasons = append(seasons, session.Season{
						SeasonNumber: seasonNum,
						EpisodeCount: 0,
						Name:         fmt.Sprintf("第%d季", seasonNum),
					})
				}
			}
			logger.Info("[SearchService] Using MoviePilot seasons map for TMDB ID %d: %d seasons", tmdbID, len(seasons))
			return seasons
		}
	}

	return nil
}

// GetCachedResults retrieves cached search results from session
func (s *SearchService) GetCachedResults(userID int64) ([]session.SearchItem, int, string, bool) {
	sess := s.sessMgr.GetOrCreate(userID)
	return sess.GetSearchResults()
}
