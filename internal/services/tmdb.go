package services

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"
)

// TMDBClient provides access to TMDB API
type TMDBClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewTMDBClient creates a new TMDB client
func NewTMDBClient(apiKey string) *TMDBClient {
	return &TMDBClient{
		apiKey:  apiKey,
		baseURL: "https://api.themoviedb.org/3",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// TMDBMediaInfo represents media information from TMDB
type TMDBMediaInfo struct {
	ID             int         `json:"id"`
	Title          string      `json:"title"`
	Name           string      `json:"name"`
	OriginalTitle  string      `json:"original_title"`
	OriginalName   string      `json:"original_name"`
	Overview       string      `json:"overview"`
	ReleaseDate    string      `json:"release_date"`
	FirstAirDate   string      `json:"first_air_date"`
	PosterPath     string      `json:"poster_path"`
	BackdropPath   string      `json:"backdrop_path"`
	VoteAverage    float64     `json:"vote_average"`
	VoteCount      int         `json:"vote_count"`
	Runtime        int         `json:"runtime"`
	EpisodeRunTime []int       `json:"episode_run_time"`
	Genres         []TMDBGenre `json:"genres"`
	ExternalIDs    struct {
		IMDBID string `json:"imdb_id"`
	} `json:"external_ids"`
	MediaType string `json:"media_type"`
}

// TMDBGenre represents a genre from TMDB
type TMDBGenre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// GetMovieDetails retrieves movie details from TMDB
func (c *TMDBClient) GetMovieDetails(tmdbID int) (*TMDBMediaInfo, error) {
	url := fmt.Sprintf("%s/movie/%d?api_key=%s&language=zh-CN", c.baseURL, tmdbID, c.apiKey)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("TMDB API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TMDB API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var media TMDBMediaInfo
	if err := json.NewDecoder(resp.Body).Decode(&media); err != nil {
		return nil, fmt.Errorf("failed to decode TMDB response: %w", err)
	}

	media.MediaType = "movie"
	return &media, nil
}

// GetTVDetails retrieves TV show details from TMDB
func (c *TMDBClient) GetTVDetails(tmdbID int) (*TMDBMediaInfo, error) {
	url := fmt.Sprintf("%s/tv/%d?api_key=%s&language=zh-CN", c.baseURL, tmdbID, c.apiKey)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("TMDB API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TMDB API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var media TMDBMediaInfo
	if err := json.NewDecoder(resp.Body).Decode(&media); err != nil {
		return nil, fmt.Errorf("failed to decode TMDB response: %w", err)
	}

	media.MediaType = "tv"
	return &media, nil
}

// SearchMedia searches for media by title
func (c *TMDBClient) SearchMedia(query string, page int) (*TMDBSearchResult, error) {
	encodedQuery := url.QueryEscape(query)
	url := fmt.Sprintf("%s/search/multi?api_key=%s&language=zh-CN&query=%s&page=%d",
		c.baseURL, c.apiKey, encodedQuery, page)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("TMDB search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TMDB search error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result TMDBSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode search results: %w", err)
	}

	return &result, nil
}

// TMDBSearchResult represents TMDB search results
type TMDBSearchResult struct {
	Page         int              `json:"page"`
	Results      []TMDBMediaInfo  `json:"results"`
	TotalPages   int              `json:"total_pages"`
	TotalResults int              `json:"total_results"`
}

// GetMediaByType retrieves media info by type (movie or tv)
func (c *TMDBClient) GetMediaByType(tmdbID int, mediaType string) (*TMDBMediaInfo, error) {
	if mediaType == "tv" {
		return c.GetTVDetails(tmdbID)
	}
	return c.GetMovieDetails(tmdbID)
}

// GetPosterURL returns full poster URL
func (c *TMDBClient) GetPosterURL(posterPath string) string {
	if posterPath == "" {
		return ""
	}
	return "https://image.tmdb.org/t/p/w500" + posterPath
}

// GetBackdropURL returns full backdrop URL
func (c *TMDBClient) GetBackdropURL(backdropPath string) string {
	if backdropPath == "" {
		return ""
	}
	return "https://image.tmdb.org/t/p/original" + backdropPath
}

// GetTitle returns the title of the media
func (m *TMDBMediaInfo) GetTitle() string {
	if m.Title != "" {
		return m.Title
	}
	if m.Name != "" {
		return m.Name
	}
	if m.OriginalTitle != "" {
		return m.OriginalTitle
	}
	if m.OriginalName != "" {
		return m.OriginalName
	}
	return "未知标题"
}

// GetYear returns the release year
func (m *TMDBMediaInfo) GetYear() int {
	date := m.ReleaseDate
	if date == "" {
		date = m.FirstAirDate
	}
	if len(date) >= 4 {
		year := 0
		fmt.Sscanf(date, "%d", &year)
		return year
	}
	return 0
}

// GetRuntime returns the runtime in minutes
func (m *TMDBMediaInfo) GetRuntime() int {
	if m.Runtime > 0 {
		return m.Runtime
	}
	if len(m.EpisodeRunTime) > 0 && m.EpisodeRunTime[0] > 0 {
		return m.EpisodeRunTime[0]
	}
	return 0
}

// GetGenres returns comma-separated genre names
func (m *TMDBMediaInfo) GetGenres() string {
	if len(m.Genres) == 0 {
		return ""
	}
	result := m.Genres[0].Name
	for i := 1; i < len(m.Genres); i++ {
		result += " / " + m.Genres[i].Name
	}
	return result
}

// ConvertToMediaInfo converts TMDB info to MoviePilot MediaInfo format
func (m *TMDBMediaInfo) ConvertToMediaInfo() *MediaInfo {
	// Convert TMDBGenre to MoviePilot genre format
	genres := make([]string, len(m.Genres))
	for i, g := range m.Genres {
		genres[i] = g.Name
	}

	mediaType := MediaTypeMovie
	if m.MediaType == "tv" {
		mediaType = MediaTypeTV
	}

	// Build poster URL
	poster := ""
	if m.PosterPath != "" {
		poster = "https://image.tmdb.org/t/p/w500" + m.PosterPath
	}

	backdrop := ""
	if m.BackdropPath != "" {
		backdrop = "https://image.tmdb.org/t/p/original" + m.BackdropPath
	}

	// Get year from date
	year := 0
	if m.ReleaseDate != "" && len(m.ReleaseDate) >= 4 {
		fmt.Sscanf(m.ReleaseDate[:4], "%d", &year)
	} else if m.FirstAirDate != "" && len(m.FirstAirDate) >= 4 {
		fmt.Sscanf(m.FirstAirDate[:4], "%d", &year)
	}

	return &MediaInfo{
		ID:          m.ID,
		Title:       m.GetTitle(),
		Year:        FlexibleYear(year),
		Overview:    m.Overview,
		Poster:      poster,
		Backdrop:    backdrop,
		Rating:      m.VoteAverage,
		Type:        mediaType,
	}
}

// SetAPIKey sets a new API key
func (c *TMDBClient) SetAPIKey(apiKey string) {
	c.apiKey = apiKey
}

// SetAPIKeyFromEnv sets the API key from environment variable or uses a default
func SetAPIKeyFromEnv(apiKey string) string {
	if apiKey != "" {
		return apiKey
	}
	// Use a demo/read-access API key for basic functionality
	// Note: For production, you should get your own API key from https://www.themoviedb.org/settings/api
	return "2cafac5b00b310f21cf8ada8ef02760f"
}

// NewTMDBClientWithDefaultKey creates a TMDB client with default or provided API key
func NewTMDBClientWithDefaultKey(apiKey string) *TMDBClient {
	key := SetAPIKeyFromEnv(apiKey)
	log.Printf("[TMDB] Client initialized with API key")
	return NewTMDBClient(key)
}
