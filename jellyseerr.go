package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"
)

var (
	jellyseerrAPIKey string
	jellyseerrClient *JellyseerrClient
)

// JellyseerrClient handles interactions with Jellyseerr API
type JellyseerrClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// JellyseerrRequest represents a request in Jellyseerr
type JellyseerrRequest struct {
	ID           int       `json:"id"`
	Status       interface{} `json:"status"` // 可以是字符串或数字
	Type         string    `json:"type"`
	MediaID      int       `json:"mediaId"`
	Media         *JellyseerrMedia `json:"media"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	RequestedBy  *JellyseerrUser `json:"requestedBy"`
}

// JellyseerrMedia represents media info
type JellyseerrMedia struct {
	ID               int     `json:"id"`
	MediaType        string  `json:"mediaType"`
	Status           string  `json:"status"`
	TmdbID           int     `json:"tmdbId"`
	Title            string  `json:"title"`
	OriginalTitle    string  `json:"originalTitle"`
	ReleaseDate      string  `json:"releaseDate"`
	PosterPath       string  `json:"posterPath"`
	BackdropPath     string  `json:"backdropPath"`
	Overview         string  `json:"overview"`
}

// JellyseerrUser represents a user
type JellyseerrUser struct {
	ID               int     `json:"id"`
	Email            string  `json:"email"`
	Username         string  `json:"username"`
	DisplayName      string  `json:"displayName"`
	Avatar           string  `json:"avatar"`
	JellyfinUserID   string  `json:"jellyfinUserId"`
	TelegramID       string  `json:"telegramId"` // 自定义字段，存储 Telegram 用户 ID
	// Quota settings (optional, may not be present in all API responses)
	MovieQuotaLimit  *int    `json:"movieQuotaLimit,omitempty"` // Daily movie request limit
	TVQuotaLimit     *int    `json:"tvQuotaLimit,omitempty"`    // Daily TV request limit
	MovieQuotaDays   *int    `json:"movieQuotaDays,omitempty"`
	TVQuotaDays      *int    `json:"tvQuotaDays,omitempty"`
	// Request counters
	RequestCount     int     `json:"requestCount,omitempty"`
	MovieRequests    int     `json:"movieRequests,omitempty"`
	TVRequests       int     `json:"tvRequests,omitempty"`
	// User status
	IsActive         bool    `json:"isActive"`
	IsAdmin          bool    `json:"isAdmin"`
}

// JellyseerrSearchResult represents search results
type JellyseerrSearchResult struct {
	MediaType string `json:"mediaType"`
	TmdbID    int    `json:"id"`
	Title     string `json:"title"`
	Name      string `json:"name"`
	PosterPath string `json:"posterPath"`
	ReleaseDate string `json:"releaseDate"`
	Overview string `json:"overview"`
}

// InitJellyseerrClient initializes the Jellyseerr API client
func InitJellyseerrClient() {
	jellyseerrAPIKey = os.Getenv("JELLYSEERR_API_KEY")
	if jellyseerrAPIKey == "" {
		log.Println("Warning: JELLYSEERR_API_KEY not set, API features will be limited")
		return
	}

	jellyseerrClient = &JellyseerrClient{
		baseURL: jellyseerrURL,
		apiKey:  jellyseerrAPIKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	log.Println("Jellyseerr API client initialized")
}

// makeRequest makes an authenticated request to Jellyseerr API
func (c *JellyseerrClient) makeRequest(method, path string, body interface{}) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("Jellyseerr client not initialized")
	}

	url := c.baseURL + "/api/v1" + path

	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error: %d - %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// ApproveRequest approves a media request
func (c *JellyseerrClient) ApproveRequest(requestID int) error {
	path := "/request/" + strconv.Itoa(requestID) + "/approve"
	_, err := c.makeRequest("POST", path, nil)
	if err != nil {
		log.Printf("Error approving request %d: %v", requestID, err)
		return err
	}
	log.Printf("Request %d approved successfully", requestID)
	return nil
}

// DeclineRequest declines a media request
func (c *JellyseerrClient) DeclineRequest(requestID int) error {
	path := "/request/" + strconv.Itoa(requestID) + "/decline"
	_, err := c.makeRequest("POST", path, nil)
	if err != nil {
		log.Printf("Error declining request %d: %v", requestID, err)
		return err
	}
	log.Printf("Request %d declined successfully", requestID)
	return nil
}

// GetPendingRequests retrieves all pending requests
func (c *JellyseerrClient) GetPendingRequests() ([]JellyseerrRequest, error) {
	path := "/request?take=50&sort=added&filter=pending"
	body, err := c.makeRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}

	var results struct {
		Page      int                `json:"page"`
		Pages     int                `json:"pages"`
		PageSize  int                `json:"pageSize"`
		Total     int                `json:"total"`
		Results   []JellyseerrRequest `json:"results"`
	}

	if err := json.Unmarshal(body, &results); err != nil {
		return nil, err
	}

	return results.Results, nil
}

// GetRequest retrieves a specific request
func (c *JellyseerrClient) GetRequest(requestID int) (*JellyseerrRequest, error) {
	path := "/request/" + strconv.Itoa(requestID)
	body, err := c.makeRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}

	var request JellyseerrRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, err
	}

	return &request, nil
}

// SearchMedia searches for media by query
func (c *JellyseerrClient) SearchMedia(query string) ([]JellyseerrSearchResult, error) {
	path := "/search?query=" + url.QueryEscape(query)
	body, err := c.makeRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}

	// Jellyseerr API returns a wrapped response with "results" array
	var rawResponse struct {
		Results []JellyseerrSearchResult `json:"results"`
	}
	if err := json.Unmarshal(body, &rawResponse); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	return rawResponse.Results, nil
}

// RequestMedia creates a new media request
func (c *JellyseerrClient) RequestMedia(tmdbID int, mediaType string, userID int) error {
	payload := map[string]interface{}{
		"mediaType": mediaType,
		"mediaId":   tmdbID,
		"userId":    userID,
	}

	path := "/request"
	_, err := c.makeRequest("POST", path, payload)
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return err
	}

	log.Printf("Request created for media %d (type: %s)", tmdbID, mediaType)
	return nil
}

// FormatPendingRequests formats pending requests for display
func FormatPendingRequests(requests []JellyseerrRequest) string {
	if len(requests) == 0 {
		return "✅ *没有待处理的请求*\n\n所有请求都已处理完毕！"
	}

	msg := "📋 *待处理请求列表*\n\n"
	msg += fmt.Sprintf("共 %d 个请求等待处理\n\n", len(requests))

	for i, req := range requests {
		if i >= 20 { // Limit to 20 requests
			msg += fmt.Sprintf("\n... 还有 %d 个请求", len(requests)-20)
			break
		}

		emoji := "🎬"
		if req.Type == "tv" {
			emoji = "📺"
		}

		title := req.Media.Title
		if title == "" && req.Media != nil {
			// TV shows use Name field
			title = req.Media.Title
		}

		// 安全获取状态
		status := getStatusString(req.Status)

		msg += fmt.Sprintf("%s *#%d* %s\n", emoji, req.ID, title)
		msg += fmt.Sprintf("   状态: %s", status)
		msg += fmt.Sprintf(" | %s", map[string]string{"movie": "电影", "tv": "剧集"}[req.Type])

		if req.RequestedBy != nil {
			name := req.RequestedBy.DisplayName
			if name == "" {
				name = req.RequestedBy.Username
			}
			if name == "" {
				name = req.RequestedBy.Email
			}
			msg += fmt.Sprintf(" | 请求者: %s", name)
		}

		msg += fmt.Sprintf("\n   时间: %s\n\n", req.CreatedAt.Format("01-02 15:04"))
	}

	return msg
}

// getStatusString 将状态转换为字符串
func getStatusString(status interface{}) string {
	switch v := status.(type) {
	case string:
		return map[string]string{
			"pending":  "⏳ 待处理",
			"approved": "✅ 已批准",
			"available": "🎉 已可用",
			"declined": "❌ 已拒绝",
		}[v]
	case float64:
		switch int(v) {
		case 1:
			return "⏳ 待处理"
		case 2:
			return "✅ 已批准"
		case 3:
			return "🎉 已可用"
		case 4:
			return "❌ 已拒绝"
		default:
			return fmt.Sprintf("状态%d", int(v))
		}
	case int:
		switch v {
		case 1:
			return "⏳ 待处理"
		case 2:
			return "✅ 已批准"
		case 3:
			return "🎉 已可用"
		case 4:
			return "❌ 已拒绝"
		default:
			return fmt.Sprintf("状态%d", v)
		}
	default:
		return "未知"
	}
}

// FormatSearchResults formats search results for display
func FormatSearchResults(results []JellyseerrSearchResult, query string) string {
	if len(results) == 0 {
		return fmt.Sprintf("🔍 *搜索结果*\n\n未找到 \"%s\" 相关内容", query)
	}

	msg := fmt.Sprintf("🔍 *搜索结果: \"%s\"*\n\n", query)
	msg += fmt.Sprintf("找到 %d 个结果\n\n", len(results))

	for i, result := range results {
		emoji := "🎬"
		typeName := "电影"
		if result.MediaType == "tv" {
			emoji = "📺"
			typeName = "剧集"
		}

		title := result.Title
		if title == "" {
			title = result.Name
		}

		msg += fmt.Sprintf("%d. %s *%s*\n", i+1, emoji, title)

		year := ""
		if len(result.ReleaseDate) >= 4 {
			year = result.ReleaseDate[:4]
		}
		if year != "" {
			msg += fmt.Sprintf("   📅 %s | %s\n", year, typeName)
		}

		msg += fmt.Sprintf("   🆔 TMDB ID: `%d`\n\n", result.TmdbID)
	}

	msg += "💡 使用 /request <TMDB_ID> <类型> 发起请求"
	msg += "\n   类型: movie 或 tv"

	return msg
}

// AutoDetectMediaType attempts to auto-detect media type from search results
func AutoDetectMediaType(results []JellyseerrSearchResult, preferredType string) string {
	if preferredType != "" {
		return preferredType
	}

	if len(results) == 0 {
		return "movie" // Default to movie
	}

	// Count types
	movieCount := 0
	tvCount := 0

	for _, result := range results {
		if result.MediaType == "movie" {
			movieCount++
		} else if result.MediaType == "tv" {
			tvCount++
		}
	}

	// Return the more common type
	if movieCount >= tvCount {
		return "movie"
	}
	return "tv"
}

// CreateSimplifiedRequest creates a request with simplified parameters
func (c *JellyseerrClient) CreateSimplifiedRequest(tmdbID int, userID int) (string, error) {
	if c == nil {
		return "", fmt.Errorf("Jellyseerr client not initialized")
	}

	// First, try to get media info to detect type
	mediaInfo, err := c.GetMediaInfo(tmdbID)
	if err != nil {
		return "", err
	}

	// Create request with detected type
	err = c.RequestMedia(tmdbID, mediaInfo.MediaType, userID)
	if err != nil {
		return "", err
	}

	// Return success message
	emoji := "🎬"
	typeName := "电影"
	if mediaInfo.MediaType == "tv" {
		emoji = "📺"
		typeName = "剧集"
	}

	msg := fmt.Sprintf("✅ *请求已创建*\n\n")
	msg += fmt.Sprintf("%s %s\n", emoji, mediaInfo.Title)
	msg += fmt.Sprintf("\n📝 类型: %s\n", typeName)
	msg += "\n等待管理员批准"

	return msg, nil
}

// GetMediaInfo gets media information for auto-detection
func (c *JellyseerrClient) GetMediaInfo(tmdbID int) (*MediaInfoResult, error) {
	if c == nil {
		return nil, fmt.Errorf("Jellyseerr client not initialized")
	}

	// Try movie first
	path := "/movie/" + strconv.Itoa(tmdbID)
	body, err := c.makeRequest("GET", path, nil)
	if err == nil {
		var media MediaInfoResult
		if err := json.Unmarshal(body, &media); err == nil && media.Title != "" {
			media.MediaType = "movie"
			return &media, nil
		}
	}

	// Try TV
	path = "/tv/" + strconv.Itoa(tmdbID)
	body, err = c.makeRequest("GET", path, nil)
	if err == nil {
		var media MediaInfoResult
		if err := json.Unmarshal(body, &media); err == nil && media.Title != "" {
			media.MediaType = "tv"
			return &media, nil
		}
	}

	return nil, fmt.Errorf("media not found")
}

// MediaInfoResult represents media info result
type MediaInfoResult struct {
	TmdbID      int    `json:"id"`
	Title       string `json:"title"`
	Name        string `json:"name"`
	MediaType   string `json:"media_type"`
	ReleaseDate string `json:"release_date"`
	PosterPath  string `json:"poster_path"`
	Overview    string `json:"overview"`
}

// AggregationBuffer handles event aggregation
type AggregationBuffer struct {
	events map[string]*AggregatedEvent
	mutex  sync.RWMutex
	ticker *time.Ticker
	done   chan bool
}

// AggregatedEvent represents an aggregated event
type AggregatedEvent struct {
	Key         string
	MediaTitle  string
	MediaType   string
	ParentTitle string  // For episodes
	Episodes    []int   // Episode numbers
	Count       int
	FirstSeen   time.Time
	LastSeen    time.Time
	UserID      string
	Username    string
}

var aggregationBuffer *AggregationBuffer

// InitAggregation initializes the aggregation buffer
func InitAggregation() {
	aggregationBuffer = &AggregationBuffer{
		events: make(map[string]*AggregatedEvent),
		ticker: time.NewTicker(5 * time.Minute),
		done:   make(chan bool),
	}

	// Start aggregation flusher
	go func() {
		for {
			select {
			case <-aggregationBuffer.ticker.C:
				flushAggregatedEvents()
			case <-aggregationBuffer.done:
				return
			}
		}
	}()

	log.Println("Aggregation buffer initialized")
}

// AddEventToAggregation adds an event to the aggregation buffer
func AddEventToAggregation(eventType, mediaTitle, mediaType, parentTitle string, episodeNum int, userID, username string) string {
	if aggregationBuffer == nil {
		return "" // Aggregation not enabled
	}

	aggregationBuffer.mutex.Lock()
	defer aggregationBuffer.mutex.Unlock()

	// Create key for aggregation
	// For episodes, group by series + season
	// For movies, group by title
	var key string
	if mediaType == "Episode" && parentTitle != "" {
		// Group episodes by series (ignoring season for simplicity)
		key = "episode:" + parentTitle
	} else if mediaType == "Movie" {
		key = "movie:" + mediaTitle
	} else {
		// Don't aggregate other types
		return ""
	}

	event, exists := aggregationBuffer.events[key]
	if !exists {
		event = &AggregatedEvent{
			Key:         key,
			MediaTitle:  mediaTitle,
			MediaType:   mediaType,
			ParentTitle: parentTitle,
			Episodes:    []int{},
			Count:       0,
			FirstSeen:   time.Now(),
			UserID:      userID,
			Username:    username,
		}
		aggregationBuffer.events[key] = event
	}

	event.Count++
	event.LastSeen = time.Now()

	// Add episode number if it's an episode
	if mediaType == "Episode" && episodeNum > 0 {
		// Check if episode already exists
		found := false
		for _, ep := range event.Episodes {
			if ep == episodeNum {
				found = true
				break
			}
		}
		if !found {
			event.Episodes = append(event.Episodes, episodeNum)
		}
	}

	// Return info about aggregation status
	if event.Count == 1 {
		return "" // First event, no aggregation yet
	}

	return fmt.Sprintf(" (已聚合 %d 个事件)", event.Count)
}

// flushAggregatedEvents sends all aggregated events and clears buffer
func flushAggregatedEvents() {
	aggregationBuffer.mutex.Lock()
	defer aggregationBuffer.mutex.Unlock()

	if len(aggregationBuffer.events) == 0 {
		return
	}

	var texts []string

	for key, event := range aggregationBuffer.events {
		var msg string

		if event.MediaType == "Episode" {
			// Aggregate episodes
			msg = fmt.Sprintf("📺 *剧集批量入库*\n\n")
			msg += fmt.Sprintf("🎬 剧集: %s\n", event.ParentTitle)
			if len(event.Episodes) > 0 {
				// Sort and format episode numbers
				msg += fmt.Sprintf("🔢 集数: 共 %d 集\n", len(event.Episodes))
			}
			msg += fmt.Sprintf("📦 总数: %d 个文件\n", event.Count)
		} else if event.MediaType == "Movie" {
			msg = fmt.Sprintf("🎥 *电影批量入库*\n\n")
			msg += fmt.Sprintf("共 %d 部新电影入库\n", event.Count)
		}

		msg += fmt.Sprintf("\n🕐 %s", time.Now().Format("2006-01-02 15:04:05"))

		texts = append(texts, msg)
		delete(aggregationBuffer.events, key)
	}

	// Send aggregated messages
	for _, text := range texts {
		if err := sendTelegramMessage(text); err != nil {
			log.Printf("Error sending aggregated message: %v", err)
		}
	}

	log.Printf("Flushed %d aggregated events", len(texts))
}
