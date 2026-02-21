package chain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// ChainBase provides common functionality for all chains
type ChainBase struct {
	jellyseerrURL string
	jellyseerrAPIKey string
	httpClient    *http.Client
}

// NewChainBase creates a new chain base
func NewChainBase(jellyseerrURL, apiKey string) *ChainBase {
	return &ChainBase{
		jellyseerrURL: jellyseerrURL,
		jellyseerrAPIKey: apiKey,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
	}
}

// MediaInfo represents media information
type MediaInfo struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Name        string `json:"name"`
	OriginalTitle string `json:"original_title"`
	ReleaseDate string `json:"release_date"`
	PosterPath  string `json:"poster_path"`
	VoteAverage float64 `json:"vote_average"`
	Overview    string `json:"overview"`
	MediaType   string `json:"media_type"` // movie or tv
}

// TorrentInfo represents torrent information
type TorrentInfo struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Size     int64  `json:"size"`
	Seeders  int    `json:"seeders"`
	Leechers int    `json:"leechers"`
	URL      string `json:"url"`
	SiteName string `json:"site_name"`
}

// RequestInfo represents a request/subscriber info
type RequestInfo struct {
	ID          int    `json:"id"`
	MediaID     int    `json:"mediaId"`
	Status      string `json:"status"`
	RequestType string `json:"requestType"`
	CreatedAt   string `json:"createdAt"`
	User        struct {
		ID          int    `json:"id"`
		DisplayName string `json:"displayName"`
		Email       string `json:"email"`
	} `json:"user"`
	Media struct {
		ID         int    `json:"id"`
		MediaType  string `json:"mediaType"`
		Title      string `json:"title"`
		Name       string `json:"name"`
		ReleaseDate string `json:"releaseDate"`
		PosterPath  string `json:"poster_path"`
		Status     string `json:"status"`
	} `json:"media"`
}

// makeJellyseerrRequest makes a request to Jellyseerr API
func (c *ChainBase) makeJellyseerrRequest(endpoint string, result interface{}) error {
	url := c.jellyseerrURL + "/api/v1" + endpoint

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Api-Key", c.jellyseerrAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	return nil
}

// postJellyseerrRequest makes a POST request to Jellyseerr API
func (c *ChainBase) postJellyseerrRequest(endpoint string, payload, result interface{}) error {
	url := c.jellyseerrURL + "/api/v1" + endpoint

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Api-Key", c.jellyseerrAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for debugging
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		log.Printf("[ChainBase] API error: status=%d, body=%s", resp.StatusCode, string(body))
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	if result != nil {
		if err := json.Unmarshal(body, result); err != nil {
			return fmt.Errorf("failed to decode response: %w, body: %s", err, string(body))
		}
	}

	return nil
}
