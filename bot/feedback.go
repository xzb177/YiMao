package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// FeedbackManager manages user feedback via Jellyseerr issues
type FeedbackManager struct {
	jellyseerrURL string
	apiKey        string
	httpClient    *http.Client

	// Track pending replies
	// userID -> issueID
	pendingReplies map[int64]int64
	replyMutex     sync.RWMutex

	// Issue type mappings
	issueTypes map[string]int // type name -> issue type ID
}

// IssueType represents Jellyseerr issue types
type IssueType struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// JellyseerrIssue represents an issue from Jellyseerr
type JellyseerrIssue struct {
	ID         int64  `json:"id"`
	IssueType  int    `json:"issueType"`
	Status     int    `json:"status"`
	ProblemSeason int `json:"problemSeason"`
	ProblemEpisode int `json:"problemEpisode"`
	Media      struct {
		ID         int    `json:"id"`
		MediaType  string `json:"mediaType"`
		Title      string `json:"title"`
		Name       string `json:"name"`
		TmdbID     int    `json:"tmdbId"`
		PosterPath string `json:"poster_path"`
	} `json:"media"`
	CreatedBy struct {
		ID          int    `json:"id"`
		DisplayName string `json:"displayName"`
		Email       string `json:"email"`
	} `json:"createdBy"`
	Comments []struct {
		ID        int    `json:"id"`
		Message   string `json:"message"`
		CreatedAt string `json:"createdAt"`
		User      struct {
			ID          int    `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"user"`
	} `json:"comments"`
}

// NewFeedbackManager creates a new feedback manager
func NewFeedbackManager(jellyseerrURL, apiKey string) *FeedbackManager {
	fm := &FeedbackManager{
		jellyseerrURL:  jellyseerrURL,
		apiKey:         apiKey,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		pendingReplies: make(map[int64]int64),
		issueTypes: map[string]int{
			"audio":      1, // Audio
			"subtitle":   2, // Subtitle
			"video":      3, // Video
			"other":      4, // Other
			"excess":     5, // Excess
			"mislabel": 6,  // Mislabel
		},
	}
	return fm
}

// CreateIssue creates a new issue in Jellyseerr
func (fm *FeedbackManager) CreateIssue(userID int64, userName, problemType, message string, mediaID int, mediaType string) (*JellyseerrIssue, error) {
	// Get Jellyseerr user ID from mapping
	jellyseerrUserID := fm.getJellyseerrUserID(userID)
	if jellyseerrUserID == 0 {
		return nil, fmt.Errorf("请先使用 /link 命令绑定账号")
	}

	// Map problem type to issue type
	issueType := fm.getIssueType(problemType)

	// Build payload
	payload := map[string]interface{}{
		"mediaId":   mediaID,
		"mediaType": mediaType,
		"issueType": issueType,
		"message":   message,
	}

	url := fmt.Sprintf("%s/api/v1/issue", fm.jellyseerrURL)

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", fm.apiKey)

	resp, err := fm.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var issue JellyseerrIssue
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return nil, err
	}

	log.Printf("[Feedback] Issue created: ID=%d, UserID=%d, Type=%s", issue.ID, userID, problemType)

	return &issue, nil
}

// GetIssue fetches issue details from Jellyseerr
func (fm *FeedbackManager) GetIssue(issueID int64) (*JellyseerrIssue, error) {
	url := fmt.Sprintf("%s/api/v1/issue/%d", fm.jellyseerrURL, issueID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Api-Key", fm.apiKey)

	resp, err := fm.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var issue JellyseerrIssue
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return nil, err
	}

	return &issue, nil
}

// AddComment adds a comment to an issue
func (fm *FeedbackManager) AddComment(issueID int64, message string) error {
	url := fmt.Sprintf("%s/api/v1/issue/%d/comment", fm.jellyseerrURL, issueID)

	payload := map[string]string{
		"message": message,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", fm.apiKey)

	resp, err := fm.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	log.Printf("[Feedback] Comment added to issue %d: %s", issueID, message)
	return nil
}

// GetMyIssues gets issues created by the user
func (fm *FeedbackManager) GetMyIssues(userID int64) ([]JellyseerrIssue, error) {
	jellyseerrUserID := fm.getJellyseerrUserID(userID)
	if jellyseerrUserID == 0 {
		return nil, fmt.Errorf("请先使用 /link 命令绑定账号")
	}

	url := fmt.Sprintf("%s/api/v1/issue?take=20&sort=created", fm.jellyseerrURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Api-Key", fm.apiKey)

	resp, err := fm.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var response struct {
		Results []JellyseerrIssue `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	// Filter by user
	var userIssues []JellyseerrIssue
	for _, issue := range response.Results {
		if issue.CreatedBy.ID == jellyseerrUserID {
			userIssues = append(userIssues, issue)
		}
	}

	return userIssues, nil
}

// GetAllIssues gets all issues (for admin)
func (fm *FeedbackManager) GetAllIssues(limit int) ([]JellyseerrIssue, error) {
	url := fmt.Sprintf("%s/api/v1/issue?take=%d&sort=created", fm.jellyseerrURL, limit)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Api-Key", fm.apiKey)

	resp, err := fm.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var response struct {
		Results []JellyseerrIssue `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	return response.Results, nil
}

// SetPendingReply sets a pending reply for a user
func (fm *FeedbackManager) SetPendingReply(userID int64, issueID int64) {
	fm.replyMutex.Lock()
	defer fm.replyMutex.Unlock()
	fm.pendingReplies[userID] = issueID
}

// GetPendingReply gets and clears the pending reply for a user
func (fm *FeedbackManager) GetPendingReply(userID int64) (int64, bool) {
	fm.replyMutex.Lock()
	defer fm.replyMutex.Unlock()
	issueID, ok := fm.pendingReplies[userID]
	if ok {
		delete(fm.pendingReplies, userID)
	}
	return issueID, ok
}

// getIssueType maps problem type to Jellyseerr issue type
func (fm *FeedbackManager) getIssueType(problemType string) int {
	if issueType, ok := fm.issueTypes[problemType]; ok {
		return issueType
	}
	return 4 // Default to "Other"
}

// getJellyseerrUserID gets Jellyseerr user ID from mapping file
func (fm *FeedbackManager) getJellyseerrUserID(telegramID int64) int {
	type MappingData struct {
		TelegramToJellyseerr map[string]int `json:"telegramToJellyseerr"`
	}

	data, err := os.ReadFile("user_mappings.json")
	if err != nil {
		return 0
	}

	var mappings MappingData
	if err := json.Unmarshal(data, &mappings); err != nil {
		return 0
	}

	telegramIDStr := strconv.FormatInt(telegramID, 10)
	if jid, exists := mappings.TelegramToJellyseerr[telegramIDStr]; exists {
		return jid
	}

	return 0
}

// FormatIssue formats an issue for display
func FormatIssue(issue *JellyseerrIssue) string {
	issueTypeName := getIssueTypeName(issue.IssueType)
	statusName := getStatusName(issue.Status)

	emoji := "📝"
	switch issue.IssueType {
	case 1:
		emoji = "🔊"
	case 2:
		emoji = "📝"
	case 3:
		emoji = "🎬"
	case 5:
		emoji = "📦"
	case 6:
		emoji = "🏷️"
	}

	msg := fmt.Sprintf("%s 问题 #%d\n", emoji, issue.ID)

	// Media info
	if issue.Media.Title != "" {
		msg += fmt.Sprintf("🎬 %s", issue.Media.Title)
	} else if issue.Media.Name != "" {
		msg += fmt.Sprintf("🎬 %s", issue.Media.Name)
	}

	if issue.ProblemSeason > 0 {
		msg += fmt.Sprintf(" S%02d", issue.ProblemSeason)
	}
	if issue.ProblemEpisode > 0 {
		msg += fmt.Sprintf("E%02d", issue.ProblemEpisode)
	}
	msg += "\n"

	msg += fmt.Sprintf("📋 类型: %s\n", issueTypeName)
	msg += fmt.Sprintf("📊 状态: %s\n", statusName)

	// Latest comment
	if len(issue.Comments) > 0 {
		latestComment := issue.Comments[len(issue.Comments)-1]
		msg += fmt.Sprintf("\n💬 %s", latestComment.Message)
	}

	return msg
}

// getIssueTypeName returns the name of an issue type
func getIssueTypeName(issueType int) string {
	names := map[int]string{
		1: "音频问题",
		2: "字幕问题",
		3: "视频问题",
		4: "其他问题",
		5: "多余内容",
		6: "标签错误",
	}
	if name, ok := names[issueType]; ok {
		return name
	}
	return "未知"
}

// getStatusName returns the name of a status
func getStatusName(status int) string {
	names := map[int]string{
		0: "待处理",
		1: "处理中",
		2: "已解决",
	}
	if name, ok := names[status]; ok {
		return name
	}
	return "未知"
}
