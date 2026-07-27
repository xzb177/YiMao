package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/validation"
)

// FlexibleYear handles year fields that can be string, int, or null
type FlexibleYear int

func (fy *FlexibleYear) UnmarshalJSON(b []byte) error {
	// Handle null
	if len(b) == 0 || string(b) == "null" {
		*fy = 0
		return nil
	}
	// Handle string
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "" {
			*fy = 0
			return nil
		}
		// Parse year from string - validate it's actually a number
		var y int
		n, err := fmt.Sscanf(s, "%d", &y)
		if err != nil || n != 1 || y < 1800 || y > 2100 {
			// Invalid year - return 0 instead of panic
			*fy = 0
			return nil
		}
		*fy = FlexibleYear(y)
		return nil
	}
	// Handle int
	var i int
	if err := json.Unmarshal(b, &i); err != nil {
		return err
	}
	// Validate year range
	if i < 1800 || i > 2100 {
		*fy = 0
		return nil
	}
	*fy = FlexibleYear(i)
	return nil
}

func (fy FlexibleYear) Int() int {
	return int(fy)
}

func (fy FlexibleYear) IsZero() bool {
	return int(fy) == 0
}

// FlexibleInt64 handles int64 fields that can be string, int, float, or null
// Used for user_id fields that may come in various formats from MoviePilot API
type FlexibleInt64 int64

func (fi *FlexibleInt64) UnmarshalJSON(b []byte) error {
	// Handle null or empty
	if len(b) == 0 || string(b) == "null" {
		*fi = 0
		return nil
	}
	// Handle string
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "" {
			*fi = 0
			return nil
		}
		// Parse int64 from string
		i, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			// Invalid number - return 0 instead of error
			*fi = 0
			return nil
		}
		*fi = FlexibleInt64(i)
		return nil
	}
	// Handle float (some APIs return numbers as floats)
	if b[0] >= '0' && b[0] <= '9' || b[0] == '-' {
		var f float64
		if err := json.Unmarshal(b, &f); err != nil {
			return err
		}
		*fi = FlexibleInt64(int64(f))
		return nil
	}
	// Handle int
	var i int64
	if err := json.Unmarshal(b, &i); err != nil {
		return err
	}
	*fi = FlexibleInt64(i)
	return nil
}

func (fi FlexibleInt64) Int64() int64 {
	return int64(fi)
}

func (fi FlexibleInt64) IsZero() bool {
	return int64(fi) == 0
}

func (fi FlexibleInt64) String() string {
	return strconv.FormatInt(int64(fi), 10)
}

// MoviePilotClient provides access to MoviePilot API.
//
// The client handles authentication via API key and manages HTTP connection pooling
// for efficient API communication. All methods are thread-safe and can be called
// concurrently.
//
// API Base URL format: http://host:port (e.g., http://192.168.1.1:4500)
type MoviePilotClient struct {
	baseURL          string
	apiKey           string
	downloadSavePath string // Optional: download save path for subscriptions
	embyURL          string // Optional: Emby URL for checking media availability
	embyAPIKey       string // Optional: Emby API key for checking media availability
	embyUserID       string // Optional: Emby user ID for API calls
	embyUserMu       sync.Mutex
	httpClient       *http.Client
	retryConfig      *RetryConfig

	// 订阅列表缓存（避免每次请求都拉全量）
	subsCacheMu   sync.RWMutex
	subsCacheData []SubscribeItem
	subsCacheTime time.Time
	subsCacheTTL  time.Duration
}

type transferHistoryResponse struct {
	Success bool `json:"success"`
	Data    struct {
		List  []TransferHistoryItem `json:"list"`
		Total int                   `json:"total"`
	} `json:"data"`
}

// NewMoviePilotClient creates a new MoviePilot client with optimized HTTP settings.
//
// The client is configured with:
// - 30s request timeout
// - Connection pooling (100 max idle connections)
// - HTTP/2 support for better performance
// - Keep-alive connections (90s idle timeout)
// - Optional download save path for subscriptions
// - Optional Emby URL and API key for checking media availability
func NewMoviePilotClient(baseURL, apiKey, downloadSavePath string) *MoviePilotClient {
	// Ensure baseURL doesn't have trailing slash
	for len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}

	return &MoviePilotClient{
		baseURL:          baseURL,
		apiKey:           apiKey,
		downloadSavePath: downloadSavePath,
		subsCacheTTL:     5 * time.Minute, // 订阅列表缓存 5 分钟
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				// 连接池配置
				MaxIdleConns:        100,              // 最大空闲连接数
				MaxIdleConnsPerHost: 20,               // 每个主机的最大空闲连接数 (提升从10)
				MaxConnsPerHost:     0,                // 0 表示不限制 (新增)
				IdleConnTimeout:     90 * time.Second, // 空闲连接超时
				// 连接超时配置
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				// HTTP/2 和 TLS 配置
				ForceAttemptHTTP2:     true,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second, // 新增：响应头超时
				// 期望继续使用 100 Continue 状态
				ExpectContinueTimeout: 1 * time.Second,
			},
		},
		retryConfig: DefaultRetryConfig(),
	}
}

// SetEmbyConfig sets the Emby configuration for checking media availability
func (c *MoviePilotClient) SetEmbyConfig(embyURL, embyAPIKey string) {
	c.embyURL = embyURL
	c.embyAPIKey = embyAPIKey
}

// SetEmbyUserID sets the Emby user ID for API calls.
// If not set, EmbyMediaExists will use the first available user from the API.
func (c *MoviePilotClient) SetEmbyUserID(userID string) {
	c.embyUserID = userID
}

// MediaType represents media type (movie or tv)
type MediaType string

const (
	MediaTypeMovie MediaType = "movie"
	MediaTypeTV    MediaType = "tv"

	// Response limits prevent abnormal upstream payloads from causing OOM.
	maxResponseBodySize             = 10 * 1024 * 1024 // 10MB
	maxSubscriptionResponseBodySize = 32 * 1024 * 1024 // Large MP installations can exceed the generic limit.
)

// MediaInfo represents media information from MoviePilot
type MediaInfo struct {
	ID         int          `json:"tmdb_id"`
	Title      string       `json:"title"`
	Year       FlexibleYear `json:"year"`
	Overview   string       `json:"overview"`
	Poster     string       `json:"poster_path"`
	Backdrop   string       `json:"backdrop_path"`
	Rating     float64      `json:"vote_average"`
	Type       MediaType    `json:"type"`
	Seasons    interface{}  `json:"seasons,omitempty"` // Can be object or array
	SeasonInfo []SeasonInfo `json:"season_info,omitempty"`
	Genres     []string     `json:"genres,omitempty"`
}

// SeasonInfo represents season information from MoviePilot
type SeasonInfo struct {
	SeasonNumber int    `json:"season_number"`
	EpisodeCount int    `json:"episode_count"`
	Name         string `json:"name"`
}

// Genre represents a genre/category
type Genre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Season represents a TV season
type Season struct {
	SeasonNumber int    `json:"season_number"`
	EpisodeCount int    `json:"episode_count"`
	Name         string `json:"name"`
}

// SearchResult represents a search result from MoviePilot
type SearchResult struct {
	ID       int          `json:"tmdb_id"`
	Title    string       `json:"title"`
	Year     FlexibleYear `json:"year"`
	Type     string       `json:"type"`
	Poster   string       `json:"poster_path"`
	Rating   float64      `json:"vote_average"`
	Overview string       `json:"overview"`
}

// SearchResponse represents search response from MoviePilot
type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

// SubscribeItem represents a subscription item from MoviePilot API
type SubscribeItem struct {
	ID           int           `json:"id"`
	Name         string        `json:"name"`
	Year         string        `json:"year"`
	Type         string        `json:"type"`
	Poster       string        `json:"poster"`
	State        string        `json:"state"`
	Username     string        `json:"username"`
	UserID       FlexibleInt64 `json:"user_id"` // Use FlexibleInt64 for robust parsing
	Date         string        `json:"date"`
	Season       int           `json:"season"`
	TotalEpisode int           `json:"total_episode"`
	LackEpisode  int           `json:"lack_episode"` // Missing episodes
	TMDBID       int           `json:"tmdbid"`       // TMDB ID for Emby lookup
}

// Request represents a media request
type Request struct {
	ID          int        `json:"id"`
	MediaID     int        `json:"media_id"`
	MediaType   MediaType  `json:"media_type"`
	Media       *MediaInfo `json:"media,omitempty"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	RequestedBy int        `json:"requested_by"`
}

// User represents a MoviePilot user
// MoviePilot API may return different field names, so we support multiple tags
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"name"` // Primary: MoviePilot uses "name" for username
	Email    string `json:"email"`
	Admin    bool   `json:"is_admin"`
	// Additional fields for API compatibility - parsed if present
	UserNameAlt string `json:"username"`     // Alternative: some endpoints use "username"
	DisplayName string `json:"display_name"` // Alternative: display name
}

// makeRequest makes an API request to MoviePilot
func (c *MoviePilotClient) makeRequest(method, endpoint string, body interface{}) ([]byte, error) {
	return c.makeRequestWithLimit(method, endpoint, body, maxResponseBodySize)
}

func (c *MoviePilotClient) makeRequestWithLimit(method, endpoint string, body interface{}, maxBodySize int64) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	url := c.baseURL + endpoint
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	logger.Debug("[MoviePilot] %s %s", method, logger.Sanitize(url))

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.ContentLength > maxBodySize {
		return nil, fmt.Errorf("response body exceeds %d bytes: content-length %d", maxBodySize, resp.ContentLength)
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if int64(len(respBody)) > maxBodySize {
		return nil, fmt.Errorf("response body exceeds %d bytes", maxBodySize)
	}

	logger.Debug("[MoviePilot] Response status: %d", resp.StatusCode)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// GetSuccessfulTransferHistory returns all successful transfer records whose
// timestamps fall inside [start, end). MoviePilot returns newest records first;
// scanning stops once a page crosses the lower bound.
func (c *MoviePilotClient) GetSuccessfulTransferHistory(start, end time.Time) ([]TransferHistoryItem, error) {
	const pageSize = 200
	result := make([]TransferHistoryItem, 0)
	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("/api/v1/history/transfer?page=%d&count=%d&status=true", page, pageSize)
		body, err := c.makeRequest("GET", endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("get transfer history page %d: %w", page, err)
		}
		var response transferHistoryResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("decode transfer history page %d: %w", page, err)
		}
		if !response.Success {
			return nil, fmt.Errorf("transfer history API returned success=false")
		}
		if len(response.Data.List) == 0 {
			break
		}
		crossedStart := false
		for _, row := range response.Data.List {
			at, err := time.ParseInLocation(transferHistoryTimeLayout, row.Date, start.Location())
			if err != nil {
				continue
			}
			if at.Before(start) {
				crossedStart = true
				continue
			}
			if at.Before(end) {
				result = append(result, row)
			}
		}
		if crossedStart || page*pageSize >= response.Data.Total {
			break
		}
	}
	return result, nil
}

// SearchMedia searches for media by query using the general 20-item page size.
func (c *MoviePilotClient) SearchMedia(query string, page int) (*SearchResponse, error) {
	return c.SearchMediaWithCount(query, page, 20)
}

// SearchMediaWithCount searches with an explicit page size. Interactive Telegram
// results use 8 so every API item maps to one visible button without gaps.
func (c *MoviePilotClient) SearchMediaWithCount(query string, page, count int) (*SearchResponse, error) {
	query = validation.SanitizeSearchQuery(query)
	if query == "" {
		return nil, fmt.Errorf("search query cannot be empty")
	}
	if page < 1 {
		page = 1
	}
	if count < 1 || count > 50 {
		count = 20
	}

	encodedQuery := url.QueryEscape(query)
	endpoint := fmt.Sprintf("/api/v1/media/search?title=%s&page=%d&count=%d", encodedQuery, page, count)

	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	var response []SearchResult
	if err := json.Unmarshal(body, &response); err != nil {
		logger.Info("[MoviePilot] SearchMedia decode error: body=%s, err=%v", string(body), err)
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	logger.Info("[MoviePilot] SearchMedia: found %d results for query=%s (page %d)", len(response), query, page)

	return &SearchResponse{
		Results: response,
	}, nil
}

// GetMediaInfo retrieves detailed information about media
func (c *MoviePilotClient) GetMediaInfo(mediaID int, mediaType MediaType) (*MediaInfo, error) {
	// Map MediaType to MoviePilot's Chinese type names
	typeStr := "电影"
	if mediaType == MediaTypeTV {
		typeStr = "电视剧"
	}
	// URL encode the type string for Chinese characters
	endpoint := fmt.Sprintf("/api/v1/media/%s?tmdbid=%d&type_name=%s",
		url.QueryEscape(typeStr), mediaID, url.QueryEscape(typeStr))

	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var info MediaInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Check if the media actually exists (title should not be empty)
	if info.Title == "" && info.ID == 0 {
		// Try with opposite type (fallback)
		logger.Info("[MoviePilot] Media not found with type %s, trying opposite type", typeStr)
		oppTypeStr := "电视剧"
		if mediaType == MediaTypeTV {
			oppTypeStr = "电影"
		}
		endpoint = fmt.Sprintf("/api/v1/media/%s?tmdbid=%d&type_name=%s",
			url.QueryEscape(oppTypeStr), mediaID, url.QueryEscape(oppTypeStr))

		body, err = c.makeRequest("GET", endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("media not found in MoviePilot: %w", err)
		}

		var info2 MediaInfo
		if err := json.Unmarshal(body, &info2); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		// Check if media exists with opposite type
		if info2.Title != "" || info2.ID != 0 {
			logger.Info("[MoviePilot] Found media with opposite type %s", oppTypeStr)
			info2.Type = mediaType // Keep original type
			return &info2, nil
		}

		return nil, fmt.Errorf("media not found in MoviePilot (tried both types)")
	}

	// Set the media type from our parameter (more reliable than parsing response)
	info.Type = mediaType

	return &info, nil
}

// RequestMedia creates a new media request/subscription
// This function requires the media name and year to be passed in
func (c *MoviePilotClient) RequestMedia(name string, year int, tmdbID int, mediaType MediaType, season int) (*Request, error) {
	// Build subscription request
	mediaTypeStr := "电影"
	if mediaType == MediaTypeTV {
		mediaTypeStr = "电视剧"
	}

	yearStr := ""
	if year > 0 {
		yearStr = fmt.Sprintf("%d", year)
	}

	payload := map[string]interface{}{
		"name":   name,
		"year":   yearStr,
		"type":   mediaTypeStr,
		"tmdbid": tmdbID,
	}

	// Add season for TV shows
	if mediaType == MediaTypeTV && season > 0 {
		payload["season"] = season
	} else if mediaType == MediaTypeTV {
		// Default to season 1 if no seasons specified
		payload["season"] = 1
	}

	// Add save_path if configured (for multi-server deployments)
	if c.downloadSavePath != "" {
		payload["save_path"] = c.downloadSavePath
		logger.Info("[MoviePilot] Using custom save_path: %s", c.downloadSavePath)
	}

	endpoint := "/api/v1/subscribe"
	body, err := c.makeRequest("POST", endpoint, payload)
	if err != nil {
		return nil, err
	}

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			ID int `json:"id"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("subscription failed: %s", response.Message)
	}

	// 新订阅创建成功，刷新缓存
	c.InvalidateSubscriptionsCache()

	// 立即触发订阅搜索，确保订阅能开始下载
	subID := response.Data.ID
	go c.triggerSubscriptionSearch(subID)

	return &Request{
		ID:        response.Data.ID,
		MediaID:   tmdbID,
		MediaType: mediaType,
		Status:    "pending",
	}, nil
}

// triggerSubscriptionSearch 触发订阅搜索，确保订阅能开始下载
func (c *MoviePilotClient) triggerSubscriptionSearch(subID int) {
	// 等待一小段时间确保订阅已保存
	time.Sleep(500 * time.Millisecond)

	// 调用 MoviePilot 的订阅搜索 API，使用统一的重试机制
	endpoint := "/api/v1/subscribe/search"

	err := Retry(func() error {
		_, err := c.makeRequest("GET", endpoint, nil)
		return err
	}, &RetryConfig{MaxAttempts: 3, BaseDelay: 1 * time.Second, Multiplier: 1.5})

	if err == nil {
		logger.Info("[MoviePilot] 已触发订阅搜索，订阅 ID: %d", subID)
	} else {
		logger.Info("[MoviePilot] 触发订阅搜索失败，订阅 ID: %d: %v", subID, err)
	}
}

// RequestMediaBySearchResult creates a subscription from a search result
func (c *MoviePilotClient) RequestMediaBySearchResult(result SearchResult, season int) (*Request, error) {
	// Determine media type
	mediaType := MediaTypeMovie
	if result.Type == "电视剧" || result.Type == "tv" {
		mediaType = MediaTypeTV
	}

	return c.RequestMedia(result.Title, result.Year.Int(), result.ID, mediaType, season)
}

// GetUserByID retrieves a user by their ID
func (c *MoviePilotClient) GetUserByID(userID int64) (*User, error) {
	endpoint := fmt.Sprintf("/api/v1/user/%d", userID)

	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var user User
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &user, nil
}

// GetAllUsers retrieves all users from MoviePilot
func (c *MoviePilotClient) GetAllUsers() ([]User, error) {
	endpoint := "/api/v1/user/?page=1&count=100"

	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	// MoviePilot returns an array directly
	var users []User
	if err := json.Unmarshal(body, &users); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return users, nil
}

// GetUserByUsername retrieves a user by username
func (c *MoviePilotClient) GetUserByUsername(username string) (*User, error) {
	// MoviePilot uses pagination for user list
	endpoint := "/api/v1/user/?page=1&count=100"

	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	// MoviePilot returns an array directly
	var users []User
	if err := json.Unmarshal(body, &users); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Search for matching user
	for _, user := range users {
		if user.Username == username || user.Email == username {
			return &user, nil
		}
	}

	return nil, fmt.Errorf("user not found: %s", username)
}

// RegisterUserRequest represents the request to register a new user
type RegisterUserRequest struct {
	Username string `json:"name"`
	Password string `json:"password"`
	Email    string `json:"email,omitempty"`
}

// RegisterUser creates a new user in MoviePilot
func (c *MoviePilotClient) RegisterUser(username, password, email string) (*User, error) {
	endpoint := "/api/v1/user/"

	req := RegisterUserRequest{
		Username: username,
		Password: password,
		Email:    email,
	}

	body, err := c.makeRequest("POST", endpoint, req)
	if err != nil {
		return nil, err
	}

	// Parse response - MoviePilot returns {"success":true,"data":{...}}
	var response struct {
		Success bool `json:"success"`
		Data    User `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("registration failed")
	}

	// MoviePilot API doesn't return user ID on creation,
	// so we query the user list to find the newly created user
	users, err := c.GetAllUsers()
	if err != nil {
		return nil, fmt.Errorf("user created but failed to query ID: %w", err)
	}

	for _, u := range users {
		if u.Username == username {
			return &u, nil
		}
	}

	return nil, fmt.Errorf("user created but not found in user list")
}

// GetAllSubscriptions retrieves all subscriptions from MoviePilot
func (c *MoviePilotClient) GetAllSubscriptions() ([]SubscribeStatus, error) {
	endpoint := "/api/v1/subscribe/?page=1&count=1000"

	body, err := c.makeRequestWithLimit("GET", endpoint, nil, maxSubscriptionResponseBodySize)
	if err != nil {
		return nil, err
	}

	var items []SubscribeStatus
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return items, nil
}

// WarmupSubscriptionCache pre-loads the subscription cache on startup
// so the first "My Requests" click doesn't block for minutes.
func (c *MoviePilotClient) WarmupSubscriptionCache() {
	logger.Info("[MoviePilot] 预热订阅缓存...")
	if err := c.preWarmSubscriptionCache(); err != nil {
		logger.Info("[MoviePilot] 订阅缓存预热失败: %v", err)
		return
	}
	c.subsCacheMu.RLock()
	count := len(c.subsCacheData)
	c.subsCacheMu.RUnlock()
	logger.Info("[MoviePilot] 订阅缓存预热完成: %d 条", count)
}

// getCachedSubscriptions returns cached subscriptions if available.
// Returns (data, true) if cache hit, (nil, false) if cache miss (caller should trigger warmup).
func (c *MoviePilotClient) preWarmSubscriptionCache() error {
	c.subsCacheMu.Lock()
	defer c.subsCacheMu.Unlock()

	// Double-check: another goroutine may have filled it while we waited
	if c.subsCacheData != nil && time.Since(c.subsCacheTime) < c.subsCacheTTL {
		return nil
	}

	logger.Info("[MoviePilot] preWarmSubscriptionCache: 开始拉取全量订阅...")
	endpoint := "/api/v1/subscribe/?page=1&count=1000"
	body, err := c.makeRequestWithLimit("GET", endpoint, nil, maxSubscriptionResponseBodySize)
	if err != nil {
		return fmt.Errorf("拉取订阅列表失败: %w", err)
	}

	var items []SubscribeItem
	if err := json.Unmarshal(body, &items); err != nil {
		return fmt.Errorf("解析订阅列表失败: %w", err)
	}

	c.subsCacheData = items
	c.subsCacheTime = time.Now()
	logger.Info("[MoviePilot] preWarmSubscriptionCache: 完成，%d 条订阅", len(items))
	return nil
}

// IsSubscriptionCacheReady returns true if the subscription cache is loaded and fresh.
func (c *MoviePilotClient) IsSubscriptionCacheReady() bool {
	c.subsCacheMu.RLock()
	defer c.subsCacheMu.RUnlock()
	return c.subsCacheData != nil && time.Since(c.subsCacheTime) < c.subsCacheTTL
}

// MediaStatusKey returns a type-qualified TMDB key. Movie and TV identifiers
// live in separate TMDB namespaces and must never share a status entry.
func MediaStatusKey(tmdbID int, mediaType string) string {
	kind := "movie"
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "tv", "series", "电视剧":
		kind = "tv"
	}
	return fmt.Sprintf("%s:%d", kind, tmdbID)
}

func activeSubscriptionState(state string) bool {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case StatePending, StateRecycled, StateSearching, StateDownloading:
		return true
	default:
		return false
	}
}

// CachedSubscriptionMediaKeys returns fresh, active subscriptions keyed by
// both media type and TMDB ID.
func (c *MoviePilotClient) CachedSubscriptionMediaKeys() (map[string]struct{}, bool) {
	c.subsCacheMu.RLock()
	defer c.subsCacheMu.RUnlock()
	if c.subsCacheData == nil || time.Since(c.subsCacheTime) >= c.subsCacheTTL {
		return nil, false
	}
	keys := make(map[string]struct{}, len(c.subsCacheData))
	for _, item := range c.subsCacheData {
		if item.TMDBID > 0 && activeSubscriptionState(item.State) {
			keys[MediaStatusKey(item.TMDBID, item.Type)] = struct{}{}
		}
	}
	return keys, true
}

// CachedSubscriptionTMDBIDs returns a point-in-time index of fresh cached
// subscriptions. A positive hit means the title is being followed somewhere in
// this MoviePilot instance; cards must label it "站内追更", never imply that
// the current Telegram user personally subscribed. Every other case directs the
// user to details instead of guessing availability or request eligibility.
func (c *MoviePilotClient) CachedSubscriptionTMDBIDs() (map[int]struct{}, bool) {
	c.subsCacheMu.RLock()
	defer c.subsCacheMu.RUnlock()
	if c.subsCacheData == nil || time.Since(c.subsCacheTime) >= c.subsCacheTTL {
		return nil, false
	}
	ids := make(map[int]struct{}, len(c.subsCacheData))
	for _, item := range c.subsCacheData {
		if item.TMDBID > 0 {
			ids[item.TMDBID] = struct{}{}
		}
	}
	return ids, true
}

// getCachedSubscriptions returns cached subscriptions if available.
// Returns (data, true) if cache hit, (nil, false) if cache miss (caller should trigger warmup).
func (c *MoviePilotClient) getCachedSubscriptions(pageSize int) ([]SubscribeItem, bool) {
	c.subsCacheMu.RLock()
	defer c.subsCacheMu.RUnlock()
	if c.subsCacheData != nil && time.Since(c.subsCacheTime) < c.subsCacheTTL {
		return c.subsCacheData, true
	}
	return nil, false
}

// InvalidateSubscriptionsCache 强制刷新订阅缓存（订阅变更时调用）
func (c *MoviePilotClient) InvalidateSubscriptionsCache() {
	c.subsCacheMu.Lock()
	c.subsCacheData = nil
	c.subsCacheMu.Unlock()
}

// GetUserRequests retrieves all subscription requests for a user
func (c *MoviePilotClient) GetUserRequests(userID int64) ([]SubscribeItem, error) {
	// First get the user's username from MoviePilot
	user, err := c.GetUserByID(userID)
	if err != nil {
		// If GetUserByID fails, try GetUserByUsername as fallback
		// First we need to get the username from the user list
		users, err := c.GetAllUsers()
		if err != nil {
			return nil, fmt.Errorf("failed to get users: %w", err)
		}

		// Find user by ID
		for _, u := range users {
			if u.ID == userID {
				user = &u
				break
			}
		}

		if user == nil {
			return nil, fmt.Errorf("user not found: %d", userID)
		}
	}

	// Double-check user is not nil (shouldn't happen, but defensive)
	if user == nil {
		return nil, fmt.Errorf("user not found: %d", userID)
	}

	// Determine effective username - try alternative fields if primary is empty
	effectiveUsername := user.Username
	if effectiveUsername == "" || effectiveUsername == fmt.Sprintf("%d", userID) {
		// If primary username looks like an ID, try alternatives
		if user.UserNameAlt != "" {
			effectiveUsername = user.UserNameAlt
		} else if user.DisplayName != "" {
			effectiveUsername = user.DisplayName
		}
	}

	logger.Info("[MoviePilot] GetUserRequests: userID=%d, user.Username=%q, user.UserNameAlt=%q, effectiveUsername=%q",
		userID, user.Username, user.UserNameAlt, effectiveUsername)

	// 使用缓存的订阅列表（5 分钟 TTL）
	items, _ := c.getCachedSubscriptions(100)

	logger.Info("[MoviePilot] GetUserRequests: fetched %d total subscriptions (cached=%v)", len(items), c.subsCacheData != nil)

	// Client-side filtering with multiple matching strategies
	var filtered []SubscribeItem
	userIDStr := fmt.Sprintf("%d", userID)

	for i, item := range items {
		// Log first few items for debugging to understand the data format
		if i < 5 {
			logger.Info("[MoviePilot] Subscription item[%d]: id=%d, name=%s, username=%q, user_id=%d, state=%s",
				i, item.ID, item.Name, item.Username, item.UserID.Int64(), item.State)
		}

		// Multiple matching strategies - use OR logic, any match is enough
		matched := false
		var matchReason string

		// Strategy 1: Direct user_id match (most reliable if available)
		if !item.UserID.IsZero() && item.UserID.Int64() == userID {
			matched = true
			matchReason = "user_id"
		}

		// Strategy 2: username field matches effective username
		if !matched && effectiveUsername != "" && item.Username == effectiveUsername {
			matched = true
			matchReason = "username"
		}

		// Strategy 3: username field matches userID as string (some APIs store it this way)
		if !matched && item.Username == userIDStr {
			matched = true
			matchReason = "username_as_id"
		}

		// Strategy 4: Case-insensitive username match
		if !matched && effectiveUsername != "" &&
			strings.EqualFold(strings.TrimSpace(item.Username), strings.TrimSpace(effectiveUsername)) {
			matched = true
			matchReason = "username_casefold"
		}

		// Strategy 5: Check if UserNameAlt field matches
		if !matched && user.UserNameAlt != "" && item.Username == user.UserNameAlt {
			matched = true
			matchReason = "username_alt"
		}

		if matched {
			filtered = append(filtered, item)
			if len(filtered) <= 3 {
				logger.Info("[MoviePilot] Matched item id=%d (%s): %s", item.ID, matchReason, item.Name)
			}
		}
	}

	logger.Info("[MoviePilot] GetUserRequests: userID=%d, effectiveUsername=%q, total_items=%d, filtered=%d",
		userID, effectiveUsername, len(items), len(filtered))

	// Adjust state based on lack_episode, Emby availability, and subscription activity
	// MoviePilot doesn't have "C" (Completed) state for subscriptions
	completedCount := 0
	for i := range filtered {
		item := &filtered[i]
		originalState := item.State

		// For TV shows: if all episodes are downloaded, mark as completed
		if item.LackEpisode == 0 && item.TotalEpisode > 0 {
			item.State = StateCompleted
			completedCount++
		} else if item.TotalEpisode > 0 {
			// TV show with episodes - calculate progress and determine status
			// If lack_episode < total_episode, some episodes are downloaded - mark as downloading
			if item.LackEpisode < item.TotalEpisode {
				item.State = StateDownloading
			}
			// Otherwise keep R (searching/recycled) or S (searching)
		} else {
			// For movies (total_episode is 0/null): use MP state directly.
			// 跳过 Emby 逐条检查（58 条 × HTTP = 超时），仅用订阅状态。
			if item.Type == "电影" || item.Type == "movie" {
				// MP state S/P/R = 搜索中/排队中, D = 下载中, F = 失败, X = 取消
				// 超过 30 天仍为 S/P/R → 标记为失败
				if item.State == StateSearching || item.State == StatePending || item.State == StateRecycled {
					if item.Date != "" {
						subDate, err := time.Parse("2006-01-02 15:04:05", item.Date)
						if err == nil && int(time.Since(subDate).Hours()/24) > 30 {
							item.State = StateFailed
						}
					}
				}
			}
		}

		if item.State != originalState {
			logger.Info("[MoviePilot] State changed: %s (year:%s, was:%s, now:%s)",
				item.Name, item.Year, originalState, item.State)
		}
	}

	logger.Info("[MoviePilot] GetUserRequests: completed=%d/%d",
		completedCount, len(filtered))

	// Log state values of first few filtered items
	for i, item := range filtered {
		if i >= 5 {
			break
		}
		logger.Info("[MoviePilot] Filtered[%d]: id=%d, name=%s, state=%q, lack=%d/%d",
			i, item.ID, item.Name, item.State, item.LackEpisode, item.TotalEpisode)
	}

	return filtered, nil
}

// GetRequest retrieves a request by ID
func (c *MoviePilotClient) GetRequest(requestID int) (*Request, error) {
	endpoint := fmt.Sprintf("/api/v1/subscription/%d", requestID)

	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var request Request
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &request, nil
}

// DeleteRequest deletes a request
// Returns nil error if subscription doesn't exist (404), since the goal is achieved
func (c *MoviePilotClient) DeleteRequest(requestID int) error {
	endpoint := fmt.Sprintf("/api/v1/subscription/%d", requestID)
	_, err := c.makeRequest("DELETE", endpoint, nil)
	// If subscription doesn't exist (404), that's fine - goal achieved
	if err != nil && strings.Contains(err.Error(), "status 404") {
		logger.Info("[MoviePilot] Subscription %d not found (already deleted)", requestID)
		return nil
	}
	return err
}

// GetTrending retrieves trending media
//
// Deprecated: MoviePilot v2.13.5 没有 /api/v1/media/trending 端点（返回 Invalid HTTP request），
// 每日推荐已改用 TMDB 热门接口（见 scheduler.fetchTrendingMovies）。保留此方法仅为兼容，请勿在新代码中使用。
func (c *MoviePilotClient) GetTrending(mediaType MediaType, page int) (*SearchResponse, error) {
	typeStr := "电影"
	if mediaType == MediaTypeTV {
		typeStr = "电视剧"
	}
	endpoint := fmt.Sprintf("/api/v1/media/trending?type=%s&page=%d&count=20", typeStr, page)

	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var response []SearchResult
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &SearchResponse{
		Results: response,
	}, nil
}

// Helper function to convert int64 to int
func intToInt64(i int) int64 {
	return int64(i)
}

// Helper function to convert string to int64
func stringToInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

// FindExistingSubscription checks whether a similar subscription already exists in MoviePilot
// It returns the first matched subscription and true when found.
func (c *MoviePilotClient) FindExistingSubscription(tmdbID int, mediaType MediaType, season int) (*SubscribeStatus, bool, error) {
	items, err := c.GetAllSubscriptions()
	if err != nil {
		return nil, false, err
	}

	normalizeType := func(t string) string {
		t = strings.ToLower(strings.TrimSpace(t))
		switch t {
		case "tv", "series", "电视剧":
			return "tv"
		default:
			return "movie"
		}
	}

	targetType := "movie"
	if mediaType == MediaTypeTV {
		targetType = "tv"
	}

	for i := range items {
		it := &items[i]
		if it.MediaID != tmdbID {
			continue
		}
		if normalizeType(it.Type) != targetType {
			continue
		}
		// Ignore explicitly cancelled/failed subscriptions
		if it.State == StateCancelled || it.State == StateFailed {
			continue
		}
		// For TV season-specific requests, only block exact season match when season info exists
		if targetType == "tv" && season > 0 && it.TotalEpisode > 0 {
			// MoviePilot API payload doesn't always expose season directly in all versions,
			// so we keep this check permissive and still treat same TMDB TV as duplicate.
		}
		return it, true, nil
	}

	return nil, false, nil
}

// Subscription state constants
const (
	StatePending     = "P" // Pending
	StateRecycled    = "R" // Recycled
	StateSearching   = "S" // Searching
	StateDownloading = "D" // Downloading
	StateCompleted   = "C" // Completed/Available
	StateFailed      = "F" // Failed
	StateCancelled   = "X" // Cancelled
)

// GetStateText returns user-friendly state text
func GetStateText(state string) string {
	switch state {
	case StatePending:
		return "⏳ 排队中"
	case StateRecycled:
		return "🔄 重新搜索"
	case StateSearching:
		return "🔍 搜索中"
	case StateDownloading:
		return "📥 下载中"
	case StateCompleted:
		return "✅ 已完成"
	case StateFailed:
		return "❌ 失败"
	case StateCancelled:
		return "🚫 已取消"
	default:
		return "❓ 未知状态"
	}
}

// GetSubscriptionStatus gets the status of a subscription request
func (c *MoviePilotClient) GetSubscriptionStatus(requestID int) (*SubscribeStatus, error) {
	endpoint := fmt.Sprintf("/api/v1/subscription/%d", requestID)

	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var status SubscribeStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &status, nil
}

// ReshareSubscription triggers a reshare for a subscription
func (c *MoviePilotClient) ReshareSubscription(subscriptionID string) error {
	// MoviePilot API uses PUT /api/v1/subscription/{id} to update
	// To trigger reshare, we set the state to "R" (Recycled)
	endpoint := fmt.Sprintf("/api/v1/subscription/%s", subscriptionID)

	payload := map[string]interface{}{
		"state": "R", // StateRecycled - triggers reshare
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	_, err = c.makeRequest("PUT", endpoint, data)
	return err
}

// CancelSubscription cancels a MoviePilot subscription by setting its state to cancelled.
func (c *MoviePilotClient) CancelSubscription(subscriptionID string) error {
	endpoint := fmt.Sprintf("/api/v1/subscription/%s", subscriptionID)

	payload := map[string]interface{}{
		"state": StateCancelled,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	_, err = c.makeRequest("PUT", endpoint, data)
	return err
}

// SubscribeStatus represents the detailed status of a subscription
type SubscribeStatus struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	Year           string `json:"year"`
	Type           string `json:"type"`
	State          string `json:"state"`
	StatusText     string `json:"status_text,omitempty"`
	MediaID        int    `json:"media_id"`
	SavePath       string `json:"save_path"`
	Username       string `json:"username"`
	Downloader     string `json:"downloader"`
	TotalEpisode   int    `json:"total_episode"`
	CurrentEpisode int    `json:"current_episode"`
	LackEpisode    int    `json:"lack_episode"` // Missing episodes
	Percent        int    `json:"percent"`
	ErrorMessage   string `json:"error_message,omitempty"`
}

// NotifyUser sends a notification to a user about their request status update
func (c *MoviePilotClient) NotifyUser(telegramID int64, requestID int, status string) error {
	// This will be handled by the notification service
	return nil
}

// LoginUser verifies user credentials by calling MoviePilot's login endpoint.
// Returns a JWT access token on success, or an error if credentials are invalid.
func (c *MoviePilotClient) LoginUser(username, password string) (string, error) {
	endpoint := c.baseURL + "/api/v1/login/access-token"

	// MoviePilot expects form-urlencoded data for login
	formData := url.Values{}
	formData.Set("username", username)
	formData.Set("password", password)

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(formData.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.doRequest(req)
	if err != nil {
		return "", fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login failed: status %d", resp.StatusCode)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode login response: %w", err)
	}

	if result.AccessToken == "" {
		return "", fmt.Errorf("login failed: no access token returned")
	}

	return result.AccessToken, nil
}

// Authenticate verifies MoviePilot credentials and returns the user ID.
// Existing users must pass MoviePilot login verification; new users are registered
// with the provided password. Prefer explicit call sites over this legacy helper.
func (c *MoviePilotClient) Authenticate(username, password string) (int64, error) {
	user, err := c.GetUserByUsername(username)
	if err == nil && user != nil {
		if strings.TrimSpace(password) == "" {
			return 0, fmt.Errorf("password required for existing user")
		}
		if _, err := c.LoginUser(username, password); err != nil {
			return 0, fmt.Errorf("invalid credentials: %w", err)
		}
		return user.ID, nil
	}

	if strings.TrimSpace(password) == "" {
		return 0, fmt.Errorf("password required for new user")
	}
	newUser, err := c.RegisterUser(username, password, "")
	if err != nil {
		return 0, fmt.Errorf("registration failed: %w", err)
	}
	return newUser.ID, nil
}

// EnsureUser creates a MoviePilot user if it does not already exist, returns the user ID.
// Deprecated: existing users require explicit ownership proof; use RegisterUser for
// auto-created accounts and LoginUser for existing-account binding.
func (c *MoviePilotClient) EnsureUser(username string) (int64, error) {
	// Never silently bind an existing MoviePilot user.
	if user, err := c.GetUserByUsername(username); err == nil && user != nil {
		return 0, fmt.Errorf("user %s already exists; password verification is required", username)
	}

	// User doesn't exist, auto-register with random password
	randomPW, err := GenerateRandomPassword(16)
	if err != nil {
		return 0, fmt.Errorf("生成随机密码失败: %w", err)
	}

	newUser, err := c.RegisterUser(username, randomPW, username+"@auto.local")
	if err != nil {
		return 0, fmt.Errorf("自动注册失败: %w", err)
	}

	logger.Info("[EnsureUser] Auto-created MoviePilot user: %s (ID:%d)", username, newUser.ID)
	return newUser.ID, nil
}

// SiteInfo represents a torrent site in MoviePilot
type SiteInfo struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// GetSites retrieves all configured torrent sites from MoviePilot
func (c *MoviePilotClient) GetSites() ([]SiteInfo, error) {
	endpoint := "/api/v1/site/"

	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var sites []SiteInfo
	if err := json.Unmarshal(body, &sites); err != nil {
		return nil, fmt.Errorf("failed to decode sites response: %w", err)
	}

	return sites, nil
}

// TorrentResource represents a torrent resource from a site
type TorrentResource struct {
	SiteName  string   `json:"site_name"`
	Title     string   `json:"title"`
	Size      float64  `json:"size"` // May be float like 306683023.0
	Seeders   int      `json:"seeders"`
	Peers     int      `json:"peers"`
	Grabs     int      `json:"grabs"`
	PubDate   string   `json:"pubdate"`
	Enclosure string   `json:"enclosure"` // Torrent/magnet link
	PageURL   string   `json:"page_url"`
	Labels    []string `json:"labels"`
}

// GetSiteResources searches for resources on a specific site
// siteID: the site ID to search
// keyword: search keyword
// page: page number (starts from 1)
func (c *MoviePilotClient) GetSiteResources(siteID int, keyword string, page int) ([]TorrentResource, error) {
	endpoint := fmt.Sprintf("/api/v1/site/resource/%d?keyword=%s&page=%d",
		siteID, url.QueryEscape(keyword), page)

	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var resources []TorrentResource
	if err := json.Unmarshal(body, &resources); err != nil {
		return nil, fmt.Errorf("failed to decode resources response: %w", err)
	}

	return resources, nil
}

// SearchAllResources searches all sites for resources matching the keyword
func (c *MoviePilotClient) SearchAllResources(keyword string, page int) (map[string][]TorrentResource, error) {
	sites, err := c.GetSites()
	if err != nil {
		return nil, fmt.Errorf("failed to get sites: %w", err)
	}

	result := make(map[string][]TorrentResource)
	for _, site := range sites {
		resources, err := c.GetSiteResources(site.ID, keyword, page)
		if err != nil {
			logger.Info("[MoviePilot] Failed to get resources from site %s: %v", site.Name, err)
			continue
		}
		if len(resources) > 0 {
			result[site.Name] = resources
		}
	}

	return result, nil
}

// doRequest executes an HTTP request with retry logic
func (c *MoviePilotClient) doRequest(req *http.Request) (*http.Response, error) {
	ctx := context.Background()
	resp, err := RetryHTTP(ctx, c.httpClient, req, c.retryConfig)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// SetRetryConfig sets custom retry configuration
func (c *MoviePilotClient) SetRetryConfig(cfg *RetryConfig) {
	c.retryConfig = cfg
}

func (c *MoviePilotClient) resolveEmbyUserID(embyBaseURL string) (string, error) {
	c.embyUserMu.Lock()
	defer c.embyUserMu.Unlock()
	if c.embyUserID != "" {
		return c.embyUserID, nil
	}
	uid, err := c.getEmbyFirstUserID(embyBaseURL)
	if err != nil {
		return "", err
	}
	c.embyUserID = uid
	return uid, nil
}

// EmbyMediaAvailabilityByTMDB checks Emby by the exact TMDB provider ID.
// This avoids title/year fuzzy matches from overstating availability.
func (c *MoviePilotClient) EmbyMediaAvailabilityByTMDB(tmdbID int, mediaType MediaType) (bool, error) {
	if tmdbID <= 0 {
		return false, fmt.Errorf("invalid TMDB ID")
	}
	if c.embyURL == "" || c.embyAPIKey == "" {
		return false, fmt.Errorf("Emby availability is not configured")
	}
	embyBaseURL := strings.TrimRight(c.embyURL, "/")
	embyUserID, err := c.resolveEmbyUserID(embyBaseURL)
	if err != nil {
		return false, fmt.Errorf("cannot resolve Emby user: %w", err)
	}
	includeItemTypes := "Movie"
	if mediaType == MediaTypeTV {
		includeItemTypes = "Series"
	}
	values := url.Values{}
	values.Set("AnyProviderIdEquals", fmt.Sprintf("tmdb.%d", tmdbID))
	values.Set("IncludeItemTypes", includeItemTypes)
	values.Set("Recursive", "true")
	values.Set("Limit", "1")
	itemsURL := fmt.Sprintf("%s/Users/%s/Items?%s", embyBaseURL, url.PathEscape(embyUserID), values.Encode())
	req, err := http.NewRequest(http.MethodGet, itemsURL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("X-Emby-Token", c.embyAPIKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return false, fmt.Errorf("Emby items API returned status %d", resp.StatusCode)
	}
	var result struct {
		TotalRecordCount int `json:"TotalRecordCount"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBodySize)).Decode(&result); err != nil {
		return false, err
	}
	return result.TotalRecordCount > 0, nil
}

// EmbyMediaAvailability checks whether media exists in Emby and preserves
// lookup failures so callers can distinguish an unavailable service from a
// confirmed library miss.
func (c *MoviePilotClient) EmbyMediaAvailability(name string, year string, mediaType MediaType) (bool, error) {
	if c.embyURL == "" || c.embyAPIKey == "" {
		return false, fmt.Errorf("Emby availability is not configured")
	}

	// Normalize Emby URL
	embyBaseURL := c.embyURL
	for len(embyBaseURL) > 0 && embyBaseURL[len(embyBaseURL)-1] == '/' {
		embyBaseURL = embyBaseURL[:len(embyBaseURL)-1]
	}

	// Determine item type for Emby search
	includeItemTypes := "Movie"
	if mediaType == MediaTypeTV {
		includeItemTypes = "Series"
	}

	// Determine Emby user ID for API calls
	embyUserID := c.embyUserID
	if embyUserID == "" {
		// Fallback: try to get the first admin user from Emby
		if uid, err := c.getEmbyFirstUserID(embyBaseURL); err == nil {
			embyUserID = uid
		} else {
			logger.Debug("[MoviePilot] EmbyMediaAvailability: no user ID configured and cannot discover one: %v", err)
			return false, fmt.Errorf("cannot resolve Emby user: %w", err)
		}
	}

	// Use SearchHints endpoint which is more reliable for finding media
	searchURL := fmt.Sprintf("%s/Users/%s/Items?SearchTerm=%s&IncludeItemTypes=%s&Recursive=true&Limit=20",
		embyBaseURL, embyUserID, url.QueryEscape(name), includeItemTypes)

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return false, err
	}

	req.Header.Set("X-Emby-Token", c.embyAPIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("Emby items API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
	if err != nil {
		return false, err
	}

	// Parse response
	var result struct {
		TotalRecordCount int `json:"TotalRecordCount"`
		Items            []struct {
			Name           string `json:"Name"`
			Id             string `json:"Id"`
			Type           string `json:"Type"`
			ProductionYear int    `json:"ProductionYear"`
			PremiereDate   string `json:"PremiereDate"`
		} `json:"Items"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return false, err
	}

	if result.TotalRecordCount == 0 {
		return false, nil
	}

	// Try to find exact name match
	for _, item := range result.Items {
		// Exact name match (case-insensitive)
		if strings.EqualFold(item.Name, name) {
			// If year is provided, check if it matches
			if year != "" && year != "0" {
				yearInt := 0
				if y, err := strconv.Atoi(year); err == nil {
					yearInt = y
				}
				// Check ProductionYear or PremiereDate
				if item.ProductionYear == yearInt {
					return true, nil
				}
				if item.PremiereDate != "" && strings.HasPrefix(item.PremiereDate, year) {
					return true, nil
				}
				// Year mismatch, continue searching
				continue
			}
			// No year check needed or year matches
			return true, nil
		}
	}

	// No exact match - try fuzzy match for Chinese titles
	// If only 1-2 results and search term is Chinese, it's likely a match
	if result.TotalRecordCount <= 2 {
		// Check if search term contains Chinese characters
		hasChinese := false
		for _, r := range name {
			if r >= 0x4e00 && r <= 0x9fff {
				hasChinese = true
				break
			}
		}
		if hasChinese {
			return true, nil
		}
	}

	return false, nil
}

// EmbyMediaExists retains the historical bool-only API.
func (c *MoviePilotClient) EmbyMediaExists(name string, year string, mediaType MediaType) bool {
	exists, err := c.EmbyMediaAvailability(name, year, mediaType)
	return err == nil && exists
}

// getEmbyFirstUserID fetches the first admin user ID from Emby.
// Used as a fallback when embyUserID is not configured.
func (c *MoviePilotClient) getEmbyFirstUserID(embyBaseURL string) (string, error) {
	usersURL := embyBaseURL + "/Users?IsDisabled=false"
	req, err := http.NewRequest("GET", usersURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Emby-Token", c.embyAPIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return "", fmt.Errorf("Emby users API returned status %d", resp.StatusCode)
	}

	var users []struct {
		ID     string `json:"Id"`
		Name   string `json:"Name"`
		Policy struct {
			IsAdministrator bool `json:"IsAdministrator"`
		} `json:"Policy"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBodySize)).Decode(&users); err != nil {
		return "", fmt.Errorf("failed to decode Emby users: %w", err)
	}

	for _, u := range users {
		if u.Policy.IsAdministrator {
			return u.ID, nil
		}
	}
	return "", fmt.Errorf("no administrator user found in Emby")
}
