package aesthetic

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type TMDBInfo struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Name        string `json:"name"`
	Overview    string `json:"overview"`
	ReleaseDate string `json:"release_date"`
	PosterPath  string `json:"poster_path"`
	VoteAverage float64 `json:"vote_average"`
	FirstAirDate string `json:"first_air_date"`
}

type TMDBSearchResult struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Name        string `json:"name"`
	Overview    string `json:"overview"`
	ReleaseDate string `json:"release_date"`
	PosterPath  string `json:"poster_path"`
	MediaType   string `json:"media_type"`
}

func (as *AestheticSystem) searchTMDB(query string) (tmdbID int, mediaType string, overview string, year int) {
	if as.tmdbAPIKey == "" {
		return 0, "", "", 0
	}

	baseURL := "https://api.themoviedb.org/3"

	searchURL := fmt.Sprintf("%s/search/multi?api_key=%s&query=%s&language=zh-CN",
		baseURL, as.tmdbAPIKey, url.QueryEscape(query))

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(searchURL)
	if err != nil {
		log.Printf("[Aesthetic] TMDB search error: %v", err)
		return 0, "", "", 0
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Results []json.RawMessage `json:"results"`
	}

	if err := json.Unmarshal(body, &result); err != nil || len(result.Results) == 0 {
		return 0, "", "", 0
	}

	var firstResult struct {
		ID          int    `json:"id"`
		Title       string `json:"title"`
		Name        string `json:"name"`
		Overview    string `json:"overview"`
		ReleaseDate string `json:"release_date"`
		PosterPath  string `json:"poster_path"`
		MediaType   string `json:"media_type"`
	}

	json.Unmarshal(result.Results[0], &firstResult)

	if firstResult.ID == 0 {
		return 0, "", "", 0
	}

	mediaType = "movie"
	if firstResult.MediaType == "tv" {
		mediaType = "tv"
	}

	year = 0
	if firstResult.ReleaseDate != "" && len(firstResult.ReleaseDate) >= 4 {
		year, _ = strconv.Atoi(firstResult.ReleaseDate[:4])
	}

	return firstResult.ID, mediaType, firstResult.Overview, year
}

func (as *AestheticSystem) getTMDBInfo(tmdbID int, mediaType string) (*TMDBInfo, error) {
	if as.tmdbAPIKey == "" {
		return nil, fmt.Errorf("TMDB API key not configured")
	}

	baseURL := "https://api.themoviedb.org/3"
	var endpoint string

	if mediaType == "tv" {
		endpoint = fmt.Sprintf("/tv/%d?api_key=%s&language=zh-CN", tmdbID, as.tmdbAPIKey)
	} else {
		endpoint = fmt.Sprintf("/movie/%d?api_key=%s&language=zh-CN", tmdbID, as.tmdbAPIKey)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(baseURL + endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var info TMDBInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, err
	}

	return &info, nil
}

func (as *AestheticSystem) parseJellyfinLink(link string) (tmdbID int, mediaType string, title string) {
	re := regexp.MustCompile(`jellyfin.*?/metadata/(\d+)`)
	matches := re.FindStringSubmatch(link)

	if len(matches) < 2 {
		return 0, "", ""
	}

	tmdbID, _ = strconv.Atoi(matches[1])

	info, err := as.getTMDBInfo(tmdbID, "movie")
	if err == nil && info.ID == tmdbID {
		mediaType = "movie"
		title = info.Title
		if title == "" {
			title = info.Name
		}
		return tmdbID, mediaType, title
	}

	info, err = as.getTMDBInfo(tmdbID, "tv")
	if err == nil && info.ID == tmdbID {
		mediaType = "tv"
		title = info.Name
		if title == "" {
			title = info.Title
		}
		return tmdbID, mediaType, title
	}

	return tmdbID, "movie", fmt.Sprintf("TMDB:%d", tmdbID)
}

func (as *AestheticSystem) sendToJellyseerr(wish *Wish) error {
	if as.jellyseerrURL == "" || as.jellyseerrKey == "" {
		return fmt.Errorf("Jellyseerr not configured")
	}

	mediaType := "movie"
	if wish.MediaType == "tv" {
		mediaType = "tv"
	}

	requestURL := fmt.Sprintf("%s/api/v1/request", as.jellyseerrURL)

	payload := map[string]interface{}{
		"mediaType":        mediaType,
		"mediaId":          wish.TmdbID,
		"tmdbId":            wish.TmdbID,
		"profileId":        "1",
		"userId":           fmt.Sprintf("%d", wish.TgID),
		"searchResultOnly": false,
	}

	jsonPayload, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", requestURL, strings.NewReader(string(jsonPayload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", as.jellyseerrKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Jellyseerr error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (as *AestheticSystem) checkJellyseerrStatus() (map[string]interface{}, error) {
	if as.jellyseerrURL == "" || as.jellyseerrKey == "" {
		return nil, fmt.Errorf("Jellyseerr not configured")
	}

	req, _ := http.NewRequest("GET", as.jellyseerrURL+"/api/v1/status", nil)
	req.Header.Set("X-Api-Key", as.jellyseerrKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var status map[string]interface{}
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, err
	}

	return status, nil
}
