package chain

import (
	"fmt"
	"log"
	"net/url"
	"strconv"
)

// SearchChain handles media search operations
type SearchChain struct {
	*ChainBase
}

// NewSearchChain creates a new search chain
func NewSearchChain(jellyseerrURL, apiKey string) *SearchChain {
	return &SearchChain{
		ChainBase: NewChainBase(jellyseerrURL, apiKey),
	}
}

// SearchResult represents a search result
type SearchResult struct {
	Query    string
	Page     int
	PageSize int
	Total    int
	Items    []SearchItem
}

// SearchItem represents a single search result
type SearchItem struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Name        string `json:"name"`
	ReleaseDate string `json:"release_date"`
	PosterPath  string `json:"poster_path"`
	VoteAverage float64 `json:"vote_average"`
	MediaType   string `json:"media_type"`
	Overview    string `json:"overview"`
}

// SearchByTitle searches for media by title
func (s *SearchChain) SearchByTitle(query string, page int) (*SearchResult, error) {
	// Build query parameters
	params := url.Values{}
	params.Set("query", query)
	params.Set("page", strconv.Itoa(page+1)) // Jellyseerr uses 1-based paging

	apiURL := fmt.Sprintf("/api/v1/search?%s", params.Encode())

	var response struct {
		PageInfo struct {
			Page      int `json:"page"`
			PageSize  int `json:"pageSize"`
			Pages     int `json:"pages"`
			Total     int `json:"total"`
		} `json:"pageInfo"`
		Results []SearchItem `json:"results"`
	}

	err := s.makeJellyseerrRequest(apiURL, &response)
	if err != nil {
		return nil, err
	}

	// Build result
	result := &SearchResult{
		Query:    query,
		Page:     page,
		PageSize: len(response.Results),
		Total:    response.PageInfo.Total,
		Items:    response.Results,
	}

	log.Printf("[SearchChain] Found %d results for '%s' (page %d, total %d)",
		len(response.Results), query, page, response.PageInfo.Total)

	return result, nil
}

// GetMediaDetails gets detailed information about a media item
func (s *SearchChain) GetMediaDetails(mediaID int, mediaType string) (*SearchItem, error) {
	var endpoint string
	if mediaType == "tv" {
		endpoint = fmt.Sprintf("/api/v1/tv/%d", mediaID)
	} else {
		endpoint = fmt.Sprintf("/api/v1/movie/%d", mediaID)
	}

	var item SearchItem
	err := s.makeJellyseerrRequest(endpoint, &item)
	if err != nil {
		return nil, err
	}

	return &item, nil
}

func (s *SearchItem) getTitle() string {
	if s.Title != "" {
		return s.Title
	}
	return s.Name
}
