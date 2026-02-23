package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"
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
	// Limit query length to prevent abuse
	if len(query) > 100 {
		query = query[:100]
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
	endpoint := fmt.Sprintf("/api/v1/media/%s?tmdbid=%d", mediaType, mediaID)

	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var info MediaInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

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

	return &response.Data, nil
}

// GetUserRequests retrieves all subscription requests for a user
func (c *MoviePilotClient) GetUserRequests(userID int64) ([]SubscribeItem, error) {
	// MoviePilot uses /api/v1/subscribe/ endpoint
	endpoint := fmt.Sprintf("/api/v1/subscribe/?page=1&count=100")

	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	// MoviePilot returns an array directly
	var items []SubscribeItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return items, nil
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

// Helper function to convert int64 to int
func int64ToInt(i int64) int {
	return int(i)
}
