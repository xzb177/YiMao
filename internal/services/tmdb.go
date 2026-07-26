package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/validation"
)

const (
	// TMDB API constants
	TMDBBaseURL      = "https://api.themoviedb.org/3"
	TMDBImageBaseURL = "https://image.tmdb.org/t/p"
	TMDBPosterSize   = "w500"
	TMDBBackdropSize = "original"

	// TMDB media types
	TMDBMediaTypeMovie = "movie"
	TMDBMediaTypeTV    = "tv"

	// TMDB API parameters
	TMDBDefaultLanguage = "zh-CN"
)

// TMDBClient provides access to TMDB API
type TMDBClient struct {
	apiKey      string
	baseURL     string
	httpClient  *http.Client
	retryConfig *RetryConfig

	// C4: 搜索结果内存缓存（减少重复 API 调用）
	cacheMu   sync.RWMutex
	cache     map[string]*tmdbCacheEntry
	cacheTTL  time.Duration
	cacheSize int
}

type tmdbCacheEntry struct {
	result    *TMDBSearchResult
	expiresAt time.Time
}

const tmdbCacheMaxSize = 500
const tmdbCacheDefaultTTL = 1 * time.Hour

// NewTMDBClient creates a new TMDB client
func NewTMDBClient(apiKey string) *TMDBClient {
	return &TMDBClient{
		apiKey:  apiKey,
		baseURL: TMDBBaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		retryConfig: DefaultRetryConfig(),
		cache:       make(map[string]*tmdbCacheEntry),
		cacheTTL:    tmdbCacheDefaultTTL,
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
	// BelongsToCollection 电影所属系列（TMDB collection），剧集无此字段。
	BelongsToCollection *TMDBCollectionRef `json:"belongs_to_collection,omitempty"`
}

// TMDBCollectionRef 电影详情里的系列引用。
type TMDBCollectionRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// TMDBCollectionPart 系列中的一部电影。
type TMDBCollectionPart struct {
	ID          int     `json:"id"`
	Title       string  `json:"title"`
	ReleaseDate string  `json:"release_date"`
	VoteAverage float64 `json:"vote_average"`
}

// TMDBCollection 系列详情（含全部影片）。
type TMDBCollection struct {
	ID    int                  `json:"id"`
	Name  string               `json:"name"`
	Parts []TMDBCollectionPart `json:"parts"`
}

// TMDBGenre represents a genre from TMDB
type TMDBGenre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// GetMovieDetails retrieves movie details from TMDB
func (c *TMDBClient) GetMovieDetails(tmdbID int) (*TMDBMediaInfo, error) {
	url := c.buildURL(fmt.Sprintf("/movie/%d", tmdbID))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("TMDB API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("TMDB API error: status %d", resp.StatusCode)
	}

	var media TMDBMediaInfo
	if err := json.NewDecoder(resp.Body).Decode(&media); err != nil {
		return nil, fmt.Errorf("failed to decode TMDB response: %w", err)
	}

	media.MediaType = TMDBMediaTypeMovie
	return &media, nil
}

// GetTVDetails retrieves TV show details from TMDB
func (c *TMDBClient) GetTVDetails(tmdbID int) (*TMDBMediaInfo, error) {
	url := c.buildURL(fmt.Sprintf("/tv/%d", tmdbID))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("TMDB API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("TMDB API error: status %d", resp.StatusCode)
	}

	var media TMDBMediaInfo
	if err := json.NewDecoder(resp.Body).Decode(&media); err != nil {
		return nil, fmt.Errorf("failed to decode TMDB response: %w", err)
	}

	media.MediaType = TMDBMediaTypeTV
	return &media, nil
}

// SearchMedia searches for media by title
func (c *TMDBClient) SearchMedia(query string, page int) (*TMDBSearchResult, error) {
	// Sanitize and validate input
	query = validation.SanitizeSearchQuery(query)
	if query == "" {
		return nil, fmt.Errorf("search query cannot be empty")
	}

	// C4: 缓存查询
	cacheKey := fmt.Sprintf("search:%s:%d", query, page)
	if cached := c.cacheGet(cacheKey); cached != nil {
		return cached, nil
	}

	encodedQuery := url.QueryEscape(query)
	url := c.buildURL("/search/multi", "query", encodedQuery, "page", fmt.Sprintf("%d", page))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("TMDB search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("TMDB search error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result TMDBSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode search results: %w", err)
	}

	// C4: 缓存写入
	c.cacheSet(cacheKey, &result)

	return &result, nil
}

// TMDBSearchResult represents TMDB search results
type TMDBSearchResult struct {
	Page         int             `json:"page"`
	Results      []TMDBMediaInfo `json:"results"`
	TotalPages   int             `json:"total_pages"`
	TotalResults int             `json:"total_results"`
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
	return fmt.Sprintf("%s/%s%s", TMDBImageBaseURL, TMDBPosterSize, posterPath)
}

// GetBackdropURL returns full backdrop URL
func (c *TMDBClient) GetBackdropURL(backdropPath string) string {
	if backdropPath == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s%s", TMDBImageBaseURL, TMDBBackdropSize, backdropPath)
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
	if m.MediaType == TMDBMediaTypeTV {
		mediaType = MediaTypeTV
	}

	// Build poster URL
	poster := ""
	if m.PosterPath != "" {
		poster = fmt.Sprintf("%s/%s%s", TMDBImageBaseURL, TMDBPosterSize, m.PosterPath)
	}

	backdrop := ""
	if m.BackdropPath != "" {
		backdrop = fmt.Sprintf("%s/%s%s", TMDBImageBaseURL, TMDBBackdropSize, m.BackdropPath)
	}

	// Get year from date
	year := 0
	if m.ReleaseDate != "" && len(m.ReleaseDate) >= 4 {
		fmt.Sscanf(m.ReleaseDate[:4], "%d", &year)
	} else if m.FirstAirDate != "" && len(m.FirstAirDate) >= 4 {
		fmt.Sscanf(m.FirstAirDate[:4], "%d", &year)
	}

	return &MediaInfo{
		ID:       m.ID,
		Title:    m.GetTitle(),
		Year:     FlexibleYear(year),
		Overview: m.Overview,
		Poster:   poster,
		Backdrop: backdrop,
		Rating:   m.VoteAverage,
		Type:     mediaType,
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
	// No default key — TMDB requires a valid API key from https://www.themoviedb.org/settings/api
	logger.Warn("[TMDB] No API key configured, TMDB features will be disabled")
	return ""
}

// NewTMDBClientWithDefaultKey creates a TMDB client with default or provided API key
func NewTMDBClientWithDefaultKey(apiKey string) *TMDBClient {
	key := SetAPIKeyFromEnv(apiKey)
	logger.Info("[TMDB] Client initialized")
	return NewTMDBClient(key)
}

// TMDBTrendingResult represents trending movies/TV shows
type TMDBTrendingResult struct {
	Page         int                     `json:"page"`
	Results      []TMDBTrendingMediaInfo `json:"results"`
	TotalPages   int                     `json:"total_pages"`
	TotalResults int                     `json:"total_results"`
}

// TMDBTrendingMediaInfo represents a media item in trending results
type TMDBTrendingMediaInfo struct {
	ID            int         `json:"id"`
	Title         string      `json:"title"`
	Name          string      `json:"name"`
	OriginalTitle string      `json:"original_title"`
	OriginalName  string      `json:"original_name"`
	PosterPath    string      `json:"poster_path"`
	BackdropPath  string      `json:"backdrop_path"`
	VoteAverage   float64     `json:"vote_average"`
	VoteCount     int         `json:"vote_count"`
	ReleaseDate   string      `json:"release_date"`
	FirstAirDate  string      `json:"first_air_date"`
	Genres        []TMDBGenre `json:"genres"`
	Overview      string      `json:"overview"`
	MediaType     string      `json:"media_type"`
	Popularity    float64     `json:"popularity"`
}

// TMDBPopularResult represents popular movies/TV shows
type TMDBPopularResult struct {
	Page         int                     `json:"page"`
	Results      []TMDBTrendingMediaInfo `json:"results"`
	TotalPages   int                     `json:"total_pages"`
	TotalResults int                     `json:"total_results"`
}

// GetTitle 返回热门条目的标题（优先中文标题，回退到原始标题/剧名）
func (m *TMDBTrendingMediaInfo) GetTitle() string {
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

// GetYear 从发行日期/首播日期解析出年份，解析失败返回 0
func (m *TMDBTrendingMediaInfo) GetYear() int {
	date := m.ReleaseDate
	if date == "" {
		date = m.FirstAirDate
	}
	if len(date) >= 4 {
		year := 0
		fmt.Sscanf(date[:4], "%d", &year)
		return year
	}
	return 0
}

// GetCollection 获取电影系列详情（含全部影片列表）。
func (c *TMDBClient) GetCollection(collectionID int) (*TMDBCollection, error) {
	url := c.buildURL(fmt.Sprintf("/collection/%d", collectionID))

	req, err := http.NewRequest(http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("TMDB collection request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("TMDB collection error: status %d", resp.StatusCode)
	}

	var collection TMDBCollection
	if err := json.NewDecoder(resp.Body).Decode(&collection); err != nil {
		return nil, fmt.Errorf("failed to decode collection: %w", err)
	}
	return &collection, nil
}

// GetTrendingMovies gets trending movies from TMDB
// timeWindow can be "day" or "week"
func (c *TMDBClient) GetTrendingMovies(timeWindow string) (*TMDBTrendingResult, error) {
	url := fmt.Sprintf("%s/trending/movie/%s?api_key=%s&language=zh-CN", c.baseURL, timeWindow, c.apiKey)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("TMDB trending request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("TMDB trending error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result TMDBTrendingResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode trending results: %w", err)
	}

	// Mark all as movies
	for i := range result.Results {
		result.Results[i].MediaType = "movie"
	}

	return &result, nil
}

// GetTrendingTV gets trending TV shows from TMDB
func (c *TMDBClient) GetTrendingTV(timeWindow string) (*TMDBTrendingResult, error) {
	url := fmt.Sprintf("%s/trending/tv/%s?api_key=%s&language=zh-CN", c.baseURL, timeWindow, c.apiKey)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("TMDB trending TV request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("TMDB trending TV error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result TMDBTrendingResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode trending TV results: %w", err)
	}

	// Mark all as TV
	for i := range result.Results {
		result.Results[i].MediaType = "tv"
	}

	return &result, nil
}

// GetPopularMovies gets popular movies from TMDB
func (c *TMDBClient) GetPopularMovies(page int) (*TMDBPopularResult, error) {
	url := fmt.Sprintf("%s/movie/popular?api_key=%s&language=zh-CN&page=%d", c.baseURL, c.apiKey, page)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("TMDB popular movies request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("TMDB popular movies error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result TMDBPopularResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode popular results: %w", err)
	}

	return &result, nil
}

// GetPopularTV gets popular TV shows from TMDB
func (c *TMDBClient) GetPopularTV(page int) (*TMDBPopularResult, error) {
	url := fmt.Sprintf("%s/tv/popular?api_key=%s&language=zh-CN&page=%d", c.baseURL, c.apiKey, page)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("TMDB popular TV request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("TMDB popular TV error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result TMDBPopularResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode popular TV results: %w", err)
	}

	return &result, nil
}

// GetNowPlayingMovies gets movies currently in theaters
func (c *TMDBClient) GetNowPlayingMovies(page int) (*TMDBPopularResult, error) {
	url := fmt.Sprintf("%s/movie/now_playing?api_key=%s&language=zh-CN&page=%d", c.baseURL, c.apiKey, page)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("TMDB now playing request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("TMDB now playing error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result TMDBPopularResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode now playing results: %w", err)
	}

	return &result, nil
}

// GetTopRatedMovies gets top rated movies from TMDB
func (c *TMDBClient) GetTopRatedMovies(page int) (*TMDBPopularResult, error) {
	url := fmt.Sprintf("%s/movie/top_rated?api_key=%s&language=zh-CN&page=%d", c.baseURL, c.apiKey, page)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("TMDB top rated request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("TMDB top rated error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result TMDBPopularResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode top rated results: %w", err)
	}

	return &result, nil
}

// GetTopRatedTV gets top rated TV shows from TMDB
func (c *TMDBClient) GetTopRatedTV(page int) (*TMDBPopularResult, error) {
	url := fmt.Sprintf("%s/tv/top_rated?api_key=%s&language=zh-CN&page=%d", c.baseURL, c.apiKey, page)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("TMDB top rated TV request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("TMDB top rated TV error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result TMDBPopularResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode top rated TV results: %w", err)
	}

	return &result, nil
}

// GetUpcomingMovies gets upcoming movies from TMDB
func (c *TMDBClient) GetUpcomingMovies(page int) (*TMDBPopularResult, error) {
	url := fmt.Sprintf("%s/movie/upcoming?api_key=%s&language=zh-CN&page=%d", c.baseURL, c.apiKey, page)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("TMDB upcoming request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("TMDB upcoming error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result TMDBPopularResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode upcoming results: %w", err)
	}

	return &result, nil
}

// TVSeason represents a TV season from TMDB
type TVSeason struct {
	SeasonNumber int    `json:"season_number"`
	EpisodeCount int    `json:"episode_count"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	PosterPath   string `json:"poster_path"`
	AirDate      string `json:"air_date"`
}

// TVDetailsWithSeasons represents TV show details with seasons
type TVDetailsWithSeasons struct {
	ID               int         `json:"id"`
	Name             string      `json:"name"`
	OriginalName     string      `json:"original_name"`
	Overview         string      `json:"overview"`
	FirstAirDate     string      `json:"first_air_date"`
	PosterPath       string      `json:"poster_path"`
	BackdropPath     string      `json:"backdrop_path"`
	VoteAverage      float64     `json:"vote_average"`
	VoteCount        int         `json:"vote_count"`
	Genres           []TMDBGenre `json:"genres"`
	NumberOfSeasons  int         `json:"number_of_seasons"`
	NumberOfEpisodes int         `json:"number_of_episodes"`
	Seasons          []TVSeason  `json:"seasons"`
}

// GetTVDetailsWithSeasons retrieves TV show details with season information from TMDB
func (c *TMDBClient) GetTVDetailsWithSeasons(tmdbID int) (*TVDetailsWithSeasons, error) {
	url := fmt.Sprintf("%s/tv/%d?api_key=%s&language=zh-CN", c.baseURL, tmdbID, c.apiKey)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("TMDB API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("TMDB API error: status %d", resp.StatusCode)
	}

	var details TVDetailsWithSeasons
	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return nil, fmt.Errorf("failed to decode TV details: %w", err)
	}

	return &details, nil
}

// buildURL constructs a TMDB API URL with authentication and language
func (c *TMDBClient) buildURL(endpoint string, params ...string) string {
	base := fmt.Sprintf("%s%s?api_key=%s&language=%s", c.baseURL, endpoint, c.apiKey, TMDBDefaultLanguage)
	if len(params) > 0 {
		for i := 0; i < len(params); i += 2 {
			if i+1 < len(params) {
				base += fmt.Sprintf("&%s=%s", params[i], params[i+1])
			}
		}
	}
	return base
}

// doRequest executes an HTTP request with retry logic
func (c *TMDBClient) doRequest(req *http.Request) (*http.Response, error) {
	ctx := context.Background()
	resp, err := RetryHTTP(ctx, c.httpClient, req, c.retryConfig)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// SetRetryConfig sets custom retry configuration
func (c *TMDBClient) SetRetryConfig(cfg *RetryConfig) {
	c.retryConfig = cfg
}

// C4: 缓存辅助方法

func (c *TMDBClient) cacheGet(key string) *TMDBSearchResult {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()

	entry, ok := c.cache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil
	}
	return entry.result
}

func (c *TMDBClient) cacheSet(key string, result *TMDBSearchResult) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	// 超过容量时清理最旧的
	if len(c.cache) >= tmdbCacheMaxSize {
		oldest := ""
		for k, v := range c.cache {
			if oldest == "" || v.expiresAt.Before(c.cache[oldest].expiresAt) {
				oldest = k
			}
		}
		if oldest != "" {
			delete(c.cache, oldest)
		}
	}

	c.cache[key] = &tmdbCacheEntry{
		result:    result,
		expiresAt: time.Now().Add(c.cacheTTL),
	}
}
