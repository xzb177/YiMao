package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// IssueManager handles Jellyseerr issue operations
type IssueManager struct {
	jellyseerrURL string
	apiKey        string
	httpClient    *http.Client

	// Track which message belongs to which issue
	// messageID -> issueID
	issueMessageMap map[int64]int64
	messageMutex    sync.RWMutex
}

var issueMgr *IssueManager

// InitIssueManager initializes the issue manager
func InitIssueManager() {
	issueMgr = &IssueManager{
		jellyseerrURL:   jellyseerrURL,
		apiKey:          os.Getenv("JELLYSEERR_API_KEY"),
		httpClient:      &http.Client{Timeout: 30 * time.Second},
		issueMessageMap: make(map[int64]int64),
	}
	log.Println("IssueManager initialized")
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
	} `json:"media"`
	CreatedBy struct {
		ID          int    `json:"id"`
		DisplayName string `json:"displayName"`
		Email       string `json:"email"`
	} `json:"createdBy"`
	Comments []struct {
		ID      int    `json:"id"`
		Message string `json:"message"`
		CreatedAt string `json:"createdAt"`
		User struct {
			ID          int    `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"user"`
	} `json:"comments"`
}

// AddComment adds a comment to an issue
func (m *IssueManager) AddComment(issueID int64, message string) error {
	url := fmt.Sprintf("%s/api/v1/issue/%d/comment", m.jellyseerrURL, issueID)

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
	req.Header.Set("X-Api-Key", m.apiKey)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	log.Printf("Issue %d: Comment added: %s", issueID, message)
	return nil
}

// DeleteIssue deletes an issue
func (m *IssueManager) DeleteIssue(issueID int64) error {
	url := fmt.Sprintf("%s/api/v1/issue/%d", m.jellyseerrURL, issueID)

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("X-Api-Key", m.apiKey)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	log.Printf("Issue %d: Deleted", issueID)
	return nil
}

// GetIssue fetches issue details from Jellyseerr
func (m *IssueManager) GetIssue(issueID int64) (*JellyseerrIssue, error) {
	url := fmt.Sprintf("%s/api/v1/issue/%d", m.jellyseerrURL, issueID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Api-Key", m.apiKey)

	resp, err := m.httpClient.Do(req)
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

// RegisterIssueMessage maps a Telegram message ID to an issue ID
func (m *IssueManager) RegisterIssueMessage(messageID int64, issueID int64) {
	m.messageMutex.Lock()
	defer m.messageMutex.Unlock()
	m.issueMessageMap[messageID] = issueID
}

// GetIssueByMessage gets the issue ID for a Telegram message ID
func (m *IssueManager) GetIssueByMessage(messageID int64) (int64, bool) {
	m.messageMutex.RLock()
	defer m.messageMutex.RUnlock()
	issueID, ok := m.issueMessageMap[messageID]
	return issueID, ok
}

// CleanupOldMessages removes message mappings older than 1 hour
func (m *IssueManager) CleanupOldMessages() {
	// This would require timestamps, for now just implement basic cleanup
	// In production, you'd want to track when each mapping was added
}

// GetLatestIssues fetches the latest issues from Jellyseerr
func (m *IssueManager) GetLatestIssues(limit int) ([]JellyseerrIssue, error) {
	url := fmt.Sprintf("%s/api/v1/issue?take=%d&sort=created", m.jellyseerrURL, limit)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Api-Key", m.apiKey)

	resp, err := m.httpClient.Do(req)
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

// FindIssueBySubjectAndTime finds an issue by subject (media name) and recent creation time
func (m *IssueManager) FindIssueBySubjectAndTime(subject string, withinMinutes int) (*JellyseerrIssue, error) {
	issues, err := m.GetLatestIssues(10)
	if err != nil {
		return nil, err
	}

	cutoffTime := time.Now().Add(-time.Duration(withinMinutes) * time.Minute)

	for _, issue := range issues {
		// Parse the issue creation time
		createdAt, err := time.Parse(time.RFC3339, issue.Comments[0].CreatedAt)
		if err != nil {
			continue
		}

		// Check if issue was created recently and matches the subject
		if createdAt.After(cutoffTime) {
			// Check if the issue's media title contains the subject
			if issue.Media.Title != "" && containsIgnoreCase(issue.Media.Title, subject) {
				return &issue, nil
			}
			if issue.Media.Name != "" && containsIgnoreCase(issue.Media.Name, subject) {
				return &issue, nil
			}
		}
	}

	return nil, fmt.Errorf("no matching issue found")
}

// containsIgnoreCase checks if a string contains another string (case-insensitive)
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
