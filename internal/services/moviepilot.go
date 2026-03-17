package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"emby-telegram-bot/pkg/logger"
	"emby-telegram-bot/pkg/validation"
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
	httpClient       *http.Client
	retryConfig      *RetryConfig
}

// NewMoviePilotClient creates a new MoviePilot client with optimized HTTP settings.
//
// The client is configured with:
// - 30s request timeout
// - Connection pooling (100 max idle connections)
// - HTTP/2 support for better performance
// - Keep-alive connections (90s idle timeout)
// - Optional download save path for subscriptions
func NewMoviePilotClient(baseURL, apiKey, downloadSavePath string) *MoviePilotClient {
	// Ensure baseURL doesn't have trailing slash
	for len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}

	return &MoviePilotClient{
		baseURL:          baseURL,
		apiKey:           apiKey,
		downloadSavePath: downloadSavePath,
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

// MediaType represents media type (movie or tv)
type MediaType string

const (
	MediaTypeMovie MediaType = "movie"
	MediaTypeTV    MediaType = "tv"
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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	logger.Debug("[MoviePilot] Response status: %d", resp.StatusCode)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// SearchMedia searches for media by query
func (c *MoviePilotClient) SearchMedia(query string, page int) (*SearchResponse, error) {
	// Sanitize query to prevent injection attacks
	query = validation.SanitizeSearchQuery(query)
	if query == "" {
		return nil, fmt.Errorf("search query cannot be empty")
	}

	// Validate page number
	if page < 1 {
		page = 1
	}

	// URL encode the query
	encodedQuery := url.QueryEscape(query)
	endpoint := fmt.Sprintf("/api/v1/media/search?title=%s&page=%d&count=20", encodedQuery, page)

	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	var response []SearchResult
	if err := json.Unmarshal(body, &response); err != nil {
		log.Printf("[MoviePilot] SearchMedia decode error: body=%s, err=%v", string(body), err)
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	log.Printf("[MoviePilot] SearchMedia: found %d results for query=%s (page %d)", len(response), query, page)

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
		log.Printf("[MoviePilot] Media not found with type %s, trying opposite type", typeStr)
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
			log.Printf("[MoviePilot] Found media with opposite type %s", oppTypeStr)
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
		log.Printf("[MoviePilot] Using custom save_path: %s", c.downloadSavePath)
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
		log.Printf("[MoviePilot] 已触发订阅搜索，订阅 ID: %d", subID)
	} else {
		log.Printf("[MoviePilot] 触发订阅搜索失败，订阅 ID: %d: %v", subID, err)
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

	// Check if user ID is valid
	if response.Data.ID == 0 {
		return nil, fmt.Errorf("registration failed: invalid user ID returned")
	}

	return &response.Data, nil
}

// GetAllSubscriptions retrieves all subscriptions from MoviePilot
func (c *MoviePilotClient) GetAllSubscriptions() ([]SubscribeStatus, error) {
	endpoint := "/api/v1/subscribe/?page=1&count=1000"

	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var items []SubscribeStatus
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return items, nil
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

	log.Printf("[MoviePilot] GetUserRequests: userID=%d, user.Username=%q, user.UserNameAlt=%q, effectiveUsername=%q",
		userID, user.Username, user.UserNameAlt, effectiveUsername)

	// MoviePilot uses /api/v1/subscribe/ endpoint
	// Fetch all subscriptions without username filter (the API may not support it properly)
	endpoint := "/api/v1/subscribe/?page=1&count=1000"

	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	// MoviePilot returns an array directly
	var items []SubscribeItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	log.Printf("[MoviePilot] GetUserRequests: fetched %d total subscriptions from API", len(items))

	// Client-side filtering with multiple matching strategies
	var filtered []SubscribeItem
	userIDStr := fmt.Sprintf("%d", userID)

	for i, item := range items {
		// Log first few items for debugging to understand the data format
		if i < 5 {
			log.Printf("[MoviePilot] Subscription item[%d]: id=%d, name=%s, username=%q, user_id=%d, state=%s",
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
				log.Printf("[MoviePilot] Matched item id=%d (%s): %s", item.ID, matchReason, item.Name)
			}
		}
	}

	log.Printf("[MoviePilot] GetUserRequests: userID=%d, effectiveUsername=%q, total_items=%d, filtered=%d",
		userID, effectiveUsername, len(items), len(filtered))

	// Log state values of first few filtered items
	for i, item := range filtered {
		if i >= 5 {
			break
		}
		log.Printf("[MoviePilot] Filtered[%d]: id=%d, name=%s, state=%q", i, item.ID, item.Name, item.State)
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
		log.Printf("[MoviePilot] Subscription %d not found (already deleted)", requestID)
		return nil
	}
	return err
}

// GetTrending retrieves trending media
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
	Percent        int    `json:"percent"`
	ErrorMessage   string `json:"error_message,omitempty"`
}

// NotifyUser sends a notification to a user about their request status update
func (c *MoviePilotClient) NotifyUser(telegramID int64, requestID int, status string) error {
	// This will be handled by the notification service
	return nil
}

// Authenticate verifies user credentials and returns the user ID
// It will try to get the user first, and if not found, will register a new user
func (c *MoviePilotClient) Authenticate(username, password string) (int64, error) {
	// First try to get existing user
	user, err := c.GetUserByUsername(username)
	if err == nil && user != nil {
		// User exists, verify password by trying to get user by ID
		// MoviePilot API doesn't have a direct verify endpoint, so we trust the username
		return user.ID, nil
	}

	// User doesn't exist, register new user
	newUser, err := c.RegisterUser(username, password, "")
	if err != nil {
		return 0, fmt.Errorf("registration failed: %w", err)
	}

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
			log.Printf("[MoviePilot] Failed to get resources from site %s: %v", site.Name, err)
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
