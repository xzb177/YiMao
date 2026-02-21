package aesthetic

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type AestheticHandler struct {
	db            *AestheticDB
	jellyseerrURL string
	jellyseerrKey string
	tmdbAPIKey    string
	botToken      string
}

func NewAestheticHandler(dbPath, botToken, jellyseerrURL, jellyseerrKey, tmdbKey string) *AestheticHandler {
	db, err := NewAestheticDB(dbPath)
	if err != nil {
		log.Printf("[Aesthetic] Failed to init DB: %v", err)
		return nil
	}

	return &AestheticHandler{
		db:            db,
		botToken:      botToken,
		jellyseerrURL: jellyseerrURL,
		jellyseerrKey:  jellyseerrKey,
		tmdbAPIKey:    tmdbKey,
	}
}

func (ah *AestheticHandler) GetOrCreateBinding(tgID int64) (*Binding, error) {
	return ah.db.GetOrCreateBinding(tgID)
}

func (ah *AestheticHandler) GetBinding(tgID int64) (*Binding, error) {
	return ah.db.GetOrCreateBinding(tgID)
}

func (ah *AestheticHandler) CreateWish(tgID int64, title string, category string, energy int, tmdbID int, mediaType string) error {
	wish := &Wish{
		TgID:      tgID,
		Title:     title,
		Category:  category,
		Energy:    energy,
		Status:    WishStatusDormant,
		TmdbID:    tmdbID,
		MediaType: mediaType,
	}

	return ah.db.CreateWish(wish)
}

func (ah *AestheticHandler) GetUserWishes(tgID int64) ([]Wish, error) {
	return ah.db.GetUserWishes(tgID)
}

func (ah *AestheticHandler) IgniteWish(tgID, wishID int) error {
	return ah.db.IgniteWish(wishID, tgID)
}

func (ah *AestheticHandler) RemoveWish(tgID, wishID int) error {
	wish, err := ah.db.GetWishByID(wishID)
	if err != nil {
		return err
	}

	if wish.TgID != tgID {
		return fmt.Errorf("not your wish")
	}

	return ah.db.DeleteWish(wishID)
}

func (ah *AestheticHandler) RestoreQuota(tgID int64) error {
	return ah.db.RestoreQuota(tgID)
}

func (ah *AestheticHandler) ConsumeQuota(tgID int, mediaType string) error {
	return ah.db.ConsumeQuota(tgID, mediaType)
}

func (ah *AestheticHandler) AccumulateEnergy(title string, delta int) error {
	return ah.db.AccumulateEnergy(title, delta)
}

func (ah *AestheticHandler) FindWishByTitle(tgID int64, title string) (*Wish, error) {
	return ah.db.FindWishByTitle(tgID, title)
}

func (ah *AestheticHandler) SearchTMDB(query string) (tmdbID int, mediaType string, title, overview string, year int, posterPath string, err error) {
	if ah.tmdbAPIKey == "" {
		return 0, "", "", "", "", "", fmt.Errorf("TMDB API key not configured")
	}

	baseURL := "https://api.themoviedb.org/3"

	searchURL := fmt.Sprintf("%s/search/multi?api_key=%s&query=%s&language=zh-CN",
		baseURL, ah.tmdbAPIKey, url.QueryEscape(query))

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(searchURL)
	if err != nil {
		return 0, "", "", "", "", "", err
	}
	defer resp.Body.Close()

	body, _ := ah.readResp(resp)

	var result struct {
		Results []json.RawMessage `json:"results"`
	}

	if err := json.Unmarshal(body, &result); err != nil || len(result.Results) == 0 {
		return 0, "", "", "", "", "", nil
	}

	var firstResult struct {
		ID          int     `json:"id"`
		Title       string  `json:"title"`
		Name        string  `json:"name"`
		Overview    string  `json:"overview"`
		ReleaseDate string  `json:"release_date"`
		PosterPath  string  `json:"poster_path"`
		MediaType   string  `json:"media_type"`
	}

	json.Unmarshal(result.Results[0], &firstResult)

	if firstResult.ID == 0 {
		return 0, "", "", "", "", "", nil
	}

	mediaType = "movie"
	if firstResult.MediaType == "tv" {
		mediaType = "tv"
	}

	year = 0
	if firstResult.ReleaseDate != "" && len(firstResult.ReleaseDate) >= 4 {
		year, _ = strconv.Atoi(firstResult.ReleaseDate[:4])
	}

	posterURL := ""
	if firstResult.PosterPath != "" {
		posterURL = "https://image.tmdb.org/t/p/w500" + firstResult.PosterPath
	}

	return firstResult.ID, mediaType, firstResult.Title, firstResult.Overview, year, posterURL, nil
}

func (ah *AestheticHandler) GetTMDBInfo(tmdbID int, mediaType string) (title, overview string, year int, posterPath string, err error) {
	if ah.tmdbAPIKey == "" {
		return "", "", 0, "", fmt.Errorf("TMDB API key not configured")
	}

	baseURL := "https://api.themoviedb.org/3"
	var endpoint string

	if mediaType == "tv" {
		endpoint = fmt.Sprintf("/tv/%d?api_key=%s&language=zh-CN", tmdbID, ah.tmdbAPIKey)
	} else {
		endpoint = fmt.Sprintf("/movie/%d?api_key=%s&language=zh-CN", tmdbID, ah.tmdbAPIKey)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(baseURL + endpoint)
	if err != nil {
		return "", "", 0, "", err
	}
	defer resp.Body.Close()

	body, _ := ah.readResp(resp)

	var info struct {
		Title       string `json:"title"`
		Name        string `json:"name"`
		Overview    string `json:"overview"`
		ReleaseDate string `json:"release_date"`
		PosterPath  string `json:"poster_path"`
		VoteAverage float64 `json:"vote_average"`
	}

	if err := json.Unmarshal(body, &info); err != nil {
		return "", "", 0, "", err
	}

	title = info.Title
	if title == "" {
		title = info.Name
	}

	year = 0
	if info.ReleaseDate != "" && len(info.ReleaseDate) >= 4 {
		year, _ = strconv.Atoi(info.ReleaseDate[:4])
	}

	if info.PosterPath != "" {
		posterPath = "https://image.tmdb.org/t/p/w500" + info.PosterPath
	}

	return title, info.Overview, year, posterPath, nil
}

func (ah *AestheticHandler) SendToJellyseerr(tgID int64, tmdbID int, mediaType string, title string) error {
	if ah.jellyseerrURL == "" || ah.jellyseerrKey == "" {
		return fmt.Errorf("Jellyseerr not configured")
	}

	mediaTypeForAPI := "movie"
	if mediaType == "tv" {
		mediaTypeForAPI = "tv"
	}

	requestURL := fmt.Sprintf("%s/api/v1/request", ah.jellyseerrURL)

	payload := map[string]interface{}{
		"mediaType":        mediaTypeForAPI,
		"mediaId":          tmdbID,
		"tmdbId":            tmdbID,
		"profileId":        "1",
		"userId":           fmt.Sprintf("%d", tgID),
		"searchResultOnly": false,
	}

	jsonPayload, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", requestURL, strings.NewReader(string(jsonPayload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", ah.jellyseerrKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("Jellyseerr error %d", resp.StatusCode)
	}

	return nil
}

func (ah *AestheticHandler) readResp(resp *http.Response) ([]byte, error) {
	body, err := resp.Body.ReadAll(resp.Body)
	return body, err
}

func (ah *AestheticHandler) SendPrivateMessage(tgID int64, text string, keyboard interface{}) error {
	if ah.botToken == "" {
		return fmt.Errorf("bot token not configured")
}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", ah.botToken)

	payload := map[string]interface{}{
		"chat_id":    tgID,
		"text":       text,
		"parse_mode": "HTML",
	}

	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}

	jsonPayload, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", url, strings.NewReader(string(jsonPayload)))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("telegram error %d", resp.StatusCode)
	}

	return nil
}

func (ah *AestheticHandler) EditMessage(tgID, messageID int, text string, keyboard interface{}) error {
	if ah.botToken == "" {
		return fmt.Errorf("bot token not configured")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/editMessageText", ah.botToken)

	payload := map[string]interface{}{
		"chat_id":    tgID,
		"message_id":  messageID,
		"text":       text,
		"parse_mode": "HTML",
	}

	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}

	jsonPayload, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", url, strings.NewReader(string(jsonPayload)))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("telegram error %d", resp.StatusCode)
	}

	return nil
}

func (ah *AestheticHandler) AnswerCallbackQuery(callbackID, text string, showAlert bool) error {
	if ah.botToken == "" {
		return fmt.Errorf("bot token not configured")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery", ah.botToken)

	payload := map[string]interface{}{
		"callback_query_id": callbackID,
		"text":             text,
		"show_alert":       showAlert,
	}

	jsonPayload, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", url, strings.NewReader(string(jsonPayload)))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func (ah *AestheticHandler) BuildInlineKeyboard(buttons []InlineButton) map[string]interface{} {
	rows := make([][]map[string]string, len(buttons))

	currentRow := -1
	itemsPerRow := 2

	for i, btn := range buttons {
		if i%itemsPerRow == 0 {
			currentRow++
			rows[currentRow] = []map[string]string{}
		}

		rows[currentRow] = append(rows[currentRow], map[string]string{
			"text":         btn.Text,
			"callback_data": btn.Data,
		})
	}

	return map[string]interface{}{
		"inline_keyboard": rows,
	}
}

type InlineButton struct {
	Text string
	Data string
}
