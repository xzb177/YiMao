package services

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// IssueStatus represents the status of an issue
type IssueStatus string

const (
	IssueStatusOpen      IssueStatus = "open"
	IssueStatusReply     IssueStatus = "reply"
	IssueStatusProcessing IssueStatus = "processing"
	IssueStatusFixed     IssueStatus = "fixed"
	IssueStatusClosed    IssueStatus = "closed"
)

// IssuePriority represents the priority of an issue
type IssuePriority string

const (
	PriorityLow    IssuePriority = "low"
	PriorityMedium IssuePriority = "medium"
	PriorityHigh   IssuePriority = "high"
	PriorityUrgent IssuePriority = "urgent"
)

// Issue represents a user-reported issue
type Issue struct {
	ID          int64         `json:"id"`
	UserID      int64         `json:"user_id"`
	UserName    string        `json:"user_name"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Status      IssueStatus   `json:"status"`
	Priority    IssuePriority `json:"priority"`
	MediaType   string        `json:"media_type"`
	MediaID     string        `json:"media_id"`
	MediaTitle   string        `json:"media_title"`
	TmdbID      int           `json:"tmdb_id"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
	Replies     []IssueReply `json:"replies"`
}

// IssueReply represents a reply to an issue
type IssueReply struct {
	ID        int64     `json:"id"`
	IssueID   int64     `json:"issue_id"`
	AuthorID  int64     `json:"author_id"`
	AuthorName string    `json:"author_name"`
	Content   string    `json:"content"`
	Type      string    `json:"type"` // "template", "custom"
	CreatedAt time.Time `json:"created_at"`
}

// IssueService manages user-reported issues
type IssueService struct {
	issuesFile string
	issues     map[int64]*Issue
	nextID     int64
	mu         sync.RWMutex
}

// NewIssueService creates a new issue service
func NewIssueService(dataDir string) *IssueService {
	issuesFile := fmt.Sprintf("%s/feedback.json", dataDir)

	service := &IssueService{
		issuesFile: issuesFile,
		issues:     make(map[int64]*Issue),
		nextID:     1,
	}

	service.load()

	// Start cleanup routine
	go service.cleanupRoutine()

	return service
}

// load loads issues from file
func (s *IssueService) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.issuesFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if err := json.Unmarshal(data, &s.issues); err != nil {
		return err
	}

	// Find the next ID
	for id := range s.issues {
		if id >= s.nextID {
			s.nextID = id + 1
		}
	}

	log.Printf("[IssueService] Loaded %d issues", len(s.issues))
	return nil
}

// save saves issues to file
func (s *IssueService) save() error {
	data, err := json.Marshal(s.issues)
	if err != nil {
		return err
	}

	return os.WriteFile(s.issuesFile, data, 0644)
}

// CreateIssue creates a new issue
func (s *IssueService) CreateIssue(userID int64, userName, title, description string, mediaType, mediaID, mediaTitle string) (*Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	issue := &Issue{
		ID:          s.nextID,
		UserID:      userID,
		UserName:    userName,
		Title:       title,
		Description: description,
		Status:      IssueStatusOpen,
		Priority:    PriorityMedium,
		MediaType:    mediaType,
		MediaID:     mediaID,
		MediaTitle:   mediaTitle,
		CreatedAt:   now,
		UpdatedAt:   now,
		Replies:     []IssueReply{},
	}

	s.issues[s.nextID] = issue
	s.nextID++

	if err := s.save(); err != nil {
		return nil, err
	}

	log.Printf("[IssueService] Created issue #%d by user %d", issue.ID, userID)
	return issue, nil
}

// GetIssue gets an issue by ID
func (s *IssueService) GetIssue(issueID int64) (*Issue, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	issue, exists := s.issues[issueID]
	return issue, exists
}

// GetUserIssues gets all issues for a user
func (s *IssueService) GetUserIssues(userID int64) []*Issue {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var issues []*Issue
	for _, issue := range s.issues {
		if issue.UserID == userID {
			issues = append(issues, issue)
		}
	}

	return issues
}

// GetOpenIssues gets all open issues
func (s *IssueService) GetOpenIssues() []*Issue {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var openIssues []*Issue
	for _, issue := range s.issues {
		if issue.Status == IssueStatusOpen || issue.Status == IssueStatusReply {
			openIssues = append(openIssues, issue)
		}
	}

	return openIssues
}

// UpdateStatus updates the status of an issue
func (s *IssueService) UpdateStatus(issueID int64, status IssueStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if issue, exists := s.issues[issueID]; exists {
		issue.Status = status
		issue.UpdatedAt = time.Now()
		return s.save()
	}

	return fmt.Errorf("issue not found: %d", issueID)
}

// AddReply adds a reply to an issue
func (s *IssueService) AddReply(issueID int64, authorID int64, authorName, content, replyType string) (*IssueReply, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	issue, exists := s.issues[issueID]
	if !exists {
		return nil, fmt.Errorf("issue not found: %d", issueID)
	}

	now := time.Now()
	reply := IssueReply{
		ID:        int64(len(issue.Replies) + 1),
		IssueID:  issueID,
		AuthorID:  authorID,
		AuthorName: authorName,
		Content:   content,
		Type:      replyType,
		CreatedAt: now,
	}

	issue.Replies = append(issue.Replies, reply)
	issue.UpdatedAt = now

	if replyType != "template" {
		// If not a template reply, also update status
		if issue.Status == IssueStatusOpen {
			issue.Status = IssueStatusReply
		}
	}

	if err := s.save(); err != nil {
		return nil, err
	}

	return &reply, nil
}

// cleanupRoutine periodically cleans up old issues
func (s *IssueService) cleanupRoutine() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		s.cleanupOldIssues()
	}
}

// cleanupOldIssues removes issues older than 30 days
func (s *IssueService) cleanupOldIssues() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().AddDate(0, -30, 0)
	removed := 0

	for id, issue := range s.issues {
		if issue.UpdatedAt.Before(cutoff) && (issue.Status == IssueStatusClosed || issue.Status == IssueStatusFixed) {
			delete(s.issues, id)
			removed++
		}
	}

	if removed > 0 {
		s.save()
		log.Printf("[IssueService] Cleaned up %d old issues", removed)
	}

	return removed
}
