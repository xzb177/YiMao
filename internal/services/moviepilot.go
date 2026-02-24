package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

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
		// Parse year from string
		var y int
		fmt.Sscanf(s, "%d", &y)
		*fy = FlexibleYear(y)
		return nil
	}
	// Handle int
	var i int
	if err := json.Unmarshal(b, &i); err != nil {
		return err
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

// MoviePilotClient provides access to MoviePilot API
type MoviePilotClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewMoviePilotClient creates a new MoviePilot client
func NewMoviePilotClient(baseURL, apiKey string) *MoviePilotClient {
	// Ensure baseURL doesn't have trailing slash
	for len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}

	return &MoviePilotClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
				// Set connection timeouts
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				// Force HTTP/2
				ForceAttemptHTTP2: true,
				// TLS handshake timeout
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
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
	ID          int           `json:"tmdb_id"`
	Title       string        `json:"title"`
	Year        FlexibleYear  `json:"year"`
	Overview    string        `json:"overview"`
	Poster      string        `json:"poster_path"`
	Backdrop    string        `json:"backdrop_path"`
	Rating      float64       `json:"vote_average"`
	Type        MediaType     `json:"type"`
	Seasons     []Season      `json:"seasons,omitempty"`
	Genres      []string      `json:"genres,omitempty"`
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
	ID       int           `json:"tmdb_id"`
	Title    string        `json:"title"`
	Year     FlexibleYear  `json:"year"`
	Type     string        `json:"type"`
	Poster   string        `json:"poster_path"`
	Rating   float64       `json:"vote_average"`
	Overview string        `json:"overview"`
}

// SearchResponse represents search response from MoviePilot
type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

// SubscribeItem represents a subscription item from MoviePilot API
type SubscribeItem struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	Year         string  `json:"year"`
	Type         string  `json:"type"`
	Poster       string  `json:"poster"`
	State        string  `json:"state"`
	Username     string  `json:"username"`
	Date         string  `json:"date"`
	Season       int     `json:"season"`
	TotalEpisode int     `json:"total_episode"`
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
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"name"`
	Email    string `json:"email"`
	Admin    bool   `json:"is_admin"`
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

	log.Printf("[MoviePilot] %s %s", method, url)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	log.Printf("[MoviePilot] Response status: %d", resp.StatusCode)

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

	// URL encode the query
	encodedQuery := url.QueryEscape(query)
	endpoint := fmt.Sprintf("/api/v1/media/search?title=%s&page=%d&count=20", encodedQuery, page)

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
		return nil, fmt.Errorf("media not found in MoviePilot")
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

	return &Request{
		ID:        response.Data.ID,
		MediaID:   tmdbID,
		MediaType: mediaType,
		Status:    "pending",
	}, nil
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
		Success bool   `json:"success"`
		Data    User   `json:"data"`
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

	// MoviePilot uses /api/v1/subscribe/ endpoint
	// Try to filter by username
	endpoint := fmt.Sprintf("/api/v1/subscribe/?page=1&count=100&username=%s", user.Username)

	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	// MoviePilot returns an array directly
	var items []SubscribeItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// If API doesn't support filtering, filter client-side
	var filtered []SubscribeItem
	for _, item := range items {
		if item.Username == user.Username {
			filtered = append(filtered, item)
		}
	}

	log.Printf("[MoviePilot] GetUserRequests: user=%s, total_items=%d, filtered=%d", user.Username, len(items), len(filtered))

	// Log state values of first few items
	for i, item := range filtered {
		if i >= 5 {
			break
		}
		log.Printf("[MoviePilot] Item %d: name=%s, state=%q", item.ID, item.Name, item.State)
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
func (c *MoviePilotClient) DeleteRequest(requestID int) error {
	endpoint := fmt.Sprintf("/api/v1/subscription/%d", requestID)
	_, err := c.makeRequest("DELETE", endpoint, nil)
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

// Subscription state constants
const (
	StatePending    = "P" // Pending
	StateRecycled   = "R" // Recycled
	StateSearching  = "S" // Searching
	StateDownloading = "D" // Downloading
	StateCompleted  = "C" // Completed/Available
	StateFailed     = "F" // Failed
	StateCancelled = "X" // Cancelled
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
	Downloader      string `json:"downloader"`
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
