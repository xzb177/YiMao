package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

// JellyseerrClient provides access to Jellyseerr API
type JellyseerrClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewJellyseerrClient creates a new Jellyseerr client
func NewJellyseerrClient(baseURL, apiKey string) *JellyseerrClient {
	return &JellyseerrClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// MediaInfo represents media information from Jellyseerr
type MediaInfo struct {
	ID              int     `json:"id"`
	Title           string  `json:"title"`
	Name            string  `json:"name"`
	Overview        string  `json:"overview"`
	ReleaseDate     string  `json:"releaseDate"`
	PosterPath      string  `json:"posterPath"`
	BackdropPath    string  `json:"backdropPath"`
	MediaType       string  `json:"mediaType"`
	Status          string  `json:"status"`
	VoteAverage     float64 `json:"voteAverage"`
	VoteCount       int     `json:"voteCount"`
	Runtime         int     `json:"runtime"`
	Genres          []Genre `json:"genres"`
	FirstAirDate    string  `json:"firstAirDate"`
}

// Genre represents a genre
type Genre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// SearchResult represents a search result
type SearchResult struct {
	Page    int       `json:"page"`
	Results []MediaInfo `json:"results"`
	TotalPages int    `json:"totalPages"`
	TotalResults int   `json:"totalResults"`
}

// Request represents a media request
type Request struct {
	ID          int       `json:"id"`
	Status      string    `json:"status"`
	MediaID     int       `json:"mediaId"`
	MediaType   string    `json:"mediaType"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Media       *MediaInfo `json:"media,omitempty"`
}

// User represents a Jellyseerr user
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Username     string `json:"username"`
	JellyfinUserID string `json:"jellyfinUserId"`
}

// QuotaInfo represents user quota information
type QuotaInfo struct {
	MovieLimit     int `json:"movieLimit"`
	TVLimit        int `json:"tvLimit"`
	MovieRemaining int `json:"movieRemaining"`
	TVRemaining    int `json:"tvRemaining"`
}

// GetMediaInfo retrieves media information by TMDB ID
func (c *JellyseerrClient) GetMediaInfo(tmdbID int) (*MediaInfo, error) {
	url := fmt.Sprintf("%s/api/v1/media/%d", c.baseURL, tmdbID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get media info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get media info: status %d, body: %s", resp.StatusCode, string(body))
	}

	var media MediaInfo
	if err := json.NewDecoder(resp.Body).Decode(&media); err != nil {
		return nil, fmt.Errorf("failed to decode media info: %w", err)
	}

	return &media, nil
}

// Search searches for media by title
func (c *JellyseerrClient) Search(query string, page int) (*SearchResult, error) {
	url := fmt.Sprintf("%s/api/v1/search?query=%s&page=%d",
		c.baseURL, query, page)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode search results: %w", err)
	}

	return &result, nil
}

// RequestMedia creates a media request
func (c *JellyseerrClient) RequestMedia(userID int64, tmdbID int, mediaType string, seasons []int) (*Request, error) {
	url := fmt.Sprintf("%s/api/v1/request", c.baseURL)

	payload := map[string]interface{}{
		"mediaType": mediaType,
		"mediaId":   tmdbID,
		"seasons":   seasons,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	// Set user ID as header (Jellyseerr uses this for user context)
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	// Add user context via header or query param
	req.Header.Set("X-User-Id", strconv.FormatInt(userID, 10))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	var request Request
	if err := json.NewDecoder(resp.Body).Decode(&request); err != nil {
		return nil, fmt.Errorf("failed to decode request: %w", err)
	}

	return &request, nil
}

// GetUserQuota retrieves user quota information
func (c *JellyseerrClient) GetUserQuota(userID int64) (*QuotaInfo, error) {
	url := fmt.Sprintf("%s/api/v1/user/%d/quotas", c.baseURL, userID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get quota: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Some Jellyseerr versions don't have quota endpoint
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[Jellyseerr] Quota endpoint returned status %d: %s", resp.StatusCode, string(body))
		return &QuotaInfo{
			MovieLimit:     -1, // Unlimited
			TVLimit:        -1,
			MovieRemaining: -1,
			TVRemaining:    -1,
		}, nil
	}

	var quota QuotaInfo
	if err := json.NewDecoder(resp.Body).Decode(&quota); err != nil {
		return nil, fmt.Errorf("failed to decode quota: %w", err)
	}

	return &quota, nil
}

// GetUserRequests retrieves user's requests
func (c *JellyseerrClient) GetUserRequests(userID int64, page int) ([]Request, error) {
	url := fmt.Sprintf("%s/api/v1/user/%d/requests?take=20&skip=%d",
		c.baseURL, userID, (page-1)*20)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get requests: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get requests: status %d, body: %s", resp.StatusCode, string(body))
	}

	var requests []Request
	if err := json.NewDecoder(resp.Body).Decode(&requests); err != nil {
		return nil, fmt.Errorf("failed to decode requests: %w", err)
	}

	return requests, nil
}

// GetTrending retrieves trending media
func (c *JellyseerrClient) GetTrending(mediaType string, page int) (*SearchResult, error) {
	url := fmt.Sprintf("%s/api/v1/trending?type=%s&page=%d",
		c.baseURL, mediaType, page)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get trending: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get trending: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode trending: %w", err)
	}

	return &result, nil
}

// GetUserByUsername retrieves a user by username
func (c *JellyseerrClient) GetUserByUsername(username string) (*User, error) {
	url := fmt.Sprintf("%s/api/v1/user?query=%s", c.baseURL, username)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get user: status %d, body: %s", resp.StatusCode, string(body))
	}

	var users []User
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return nil, fmt.Errorf("failed to decode user: %w", err)
	}

	if len(users) == 0 {
		return nil, fmt.Errorf("user not found: %s", username)
	}

	return &users[0], nil
}

// GetUserInfo retrieves user information by ID
func (c *JellyseerrClient) GetUserInfo(userID int64) (*User, error) {
	url := fmt.Sprintf("%s/api/v1/user/%d", c.baseURL, userID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get user info: status %d, body: %s", resp.StatusCode, string(body))
	}

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode user info: %w", err)
	}

	return &user, nil
}
