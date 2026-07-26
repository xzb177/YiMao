package services

import (
	"encoding/json"
	"fmt"
	"github.com/xzb177/yimao/pkg/logger"
	"os"
	"sync"
	"time"
)

// IssueStatus represents the status of an issue
type IssueStatus string

const (
	IssueStatusOpen       IssueStatus = "open"
	IssueStatusReply      IssueStatus = "reply"
	IssueStatusProcessing IssueStatus = "processing"
	IssueStatusFixed      IssueStatus = "fixed"
	IssueStatusClosed     IssueStatus = "closed"
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
	ID           int64         `json:"id"`
	UserID       int64         `json:"user_id"`
	UserName     string        `json:"user_name"`
	Title        string        `json:"title"`
	Description  string        `json:"description"`
	Status       IssueStatus   `json:"status"`
	Priority     IssuePriority `json:"priority"`
	MediaType    string        `json:"media_type"`
	MediaID      string        `json:"media_id"`
	MediaTitle   string        `json:"media_title"`
	TmdbID       int           `json:"tmdb_id"`
	PhotoFileID  string        `json:"photo_file_id,omitempty"` // 用户反馈附带的图片
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	Replies      []IssueReply  `json:"replies"`
	Satisfaction int           `json:"satisfaction,omitempty"` // 1-5 星评价
	ResolvedAt   *time.Time    `json:"resolved_at,omitempty"`  // 解决时间
}

// IssueReply represents a reply to an issue
type IssueReply struct {
	ID         int64     `json:"id"`
	IssueID    int64     `json:"issue_id"`
	AuthorID   int64     `json:"author_id"`
	AuthorName string    `json:"author_name"`
	Content    string    `json:"content"`
	Type       string    `json:"type"` // "template", "custom", "admin", "user"
	CreatedAt  time.Time `json:"created_at"`
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
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("[IssueService] cleanupRoutine panic: %v", r)
			}
		}()
		service.cleanupRoutine()
	}()

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

	logger.Info("[IssueService] Loaded %d issues", len(s.issues))
	return nil
}

// save saves issues to file
func (s *IssueService) save() error {
	data, err := json.Marshal(s.issues)
	if err != nil {
		return err
	}

	return atomicWriteFile(s.issuesFile, data, 0644)
}

// CreateIssue creates a new issue
func (s *IssueService) CreateIssue(userID int64, userName, title, description string, mediaType, mediaID, mediaTitle string) (*Issue, error) {
	return s.CreateIssueWithPhoto(userID, userName, title, description, mediaType, mediaID, mediaTitle, "")
}

// CreateIssueWithPhoto creates a new issue with photo attachment
func (s *IssueService) CreateIssueWithPhoto(userID int64, userName, title, description, mediaType, mediaID, mediaTitle, photoFileID string) (*Issue, error) {
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
		MediaType:   mediaType,
		MediaID:     mediaID,
		MediaTitle:  mediaTitle,
		PhotoFileID: photoFileID,
		CreatedAt:   now,
		UpdatedAt:   now,
		Replies:     []IssueReply{},
	}

	issueID := s.nextID
	s.issues[issueID] = issue
	s.nextID++

	if err := s.save(); err != nil {
		delete(s.issues, issueID)
		s.nextID = issueID
		return nil, err
	}

	logger.Info("[IssueService] Created issue #%d by user %d", issue.ID, userID)
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
		ID:         int64(len(issue.Replies) + 1),
		IssueID:    issueID,
		AuthorID:   authorID,
		AuthorName: authorName,
		Content:    content,
		Type:       replyType,
		CreatedAt:  now,
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
		logger.Info("[IssueService] Cleaned up %d old issues", removed)
	}

	return removed
}

// FeedbackStats represents feedback statistics
type FeedbackStats struct {
	Total          int            `json:"total"`
	Open           int            `json:"open"`
	Processing     int            `json:"processing"`
	Fixed          int            `json:"fixed"`
	Closed         int            `json:"closed"`
	ThisWeek       int            `json:"this_week"`
	ThisMonth      int            `json:"this_month"`
	ByType         map[string]int `json:"by_type"`
	AvgResolveTime float64        `json:"avg_resolve_time"` // Average resolution time in hours
}

// GetStats returns feedback statistics
func (s *IssueService) GetStats() *FeedbackStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := &FeedbackStats{
		ByType: make(map[string]int),
	}

	now := time.Now()
	weekAgo := now.AddDate(0, 0, -7)
	monthAgo := now.AddDate(0, -1, 0)

	var totalResolveTime float64
	var resolvedCount int

	for _, issue := range s.issues {
		stats.Total++

		// Count by status
		switch issue.Status {
		case IssueStatusOpen, IssueStatusReply:
			stats.Open++
		case IssueStatusProcessing:
			stats.Processing++
		case IssueStatusFixed:
			stats.Fixed++
		case IssueStatusClosed:
			stats.Closed++
		}

		// Count by time period
		if issue.CreatedAt.After(weekAgo) {
			stats.ThisWeek++
		}
		if issue.CreatedAt.After(monthAgo) {
			stats.ThisMonth++
		}

		// Count by type
		if issue.Title != "" {
			stats.ByType[issue.Title]++
		} else {
			stats.ByType["其他"]++
		}

		// Calculate average resolve time
		if issue.ResolvedAt != nil {
			duration := issue.ResolvedAt.Sub(issue.CreatedAt).Hours()
			totalResolveTime += duration
			resolvedCount++
		}
	}

	if resolvedCount > 0 {
		stats.AvgResolveTime = totalResolveTime / float64(resolvedCount)
	}

	return stats
}

// GetFilteredIssues returns issues filtered by status
func (s *IssueService) GetFilteredIssues(statuses []IssueStatus, limit int) []*Issue {
	s.mu.RLock()
	defer s.mu.RUnlock()

	statusMap := make(map[IssueStatus]bool)
	for _, status := range statuses {
		statusMap[status] = true
	}

	var filtered []*Issue
	for _, issue := range s.issues {
		if statusMap[issue.Status] {
			filtered = append(filtered, issue)
		}
	}

	// Sort by created date (newest first)
	for i := 0; i < len(filtered); i++ {
		for j := i + 1; j < len(filtered); j++ {
			if filtered[i].CreatedAt.Before(filtered[j].CreatedAt) {
				filtered[i], filtered[j] = filtered[j], filtered[i]
			}
		}
	}

	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}

	return filtered
}

// GetAllIssues returns all issues (for admin panel)
func (s *IssueService) GetAllIssues() []*Issue {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var all []*Issue
	for _, issue := range s.issues {
		all = append(all, issue)
	}

	// Sort by created date (newest first)
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[i].CreatedAt.Before(all[j].CreatedAt) {
				all[i], all[j] = all[j], all[i]
			}
		}
	}

	return all
}

// UpdatePriority updates the priority of an issue
func (s *IssueService) UpdatePriority(issueID int64, priority IssuePriority) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if issue, exists := s.issues[issueID]; exists {
		issue.Priority = priority
		issue.UpdatedAt = time.Now()
		return s.save()
	}

	return fmt.Errorf("issue not found: %d", issueID)
}

// RateSatisfaction records user satisfaction rating
// RateSatisfaction rates the satisfaction of an issue (only the creator can rate)
func (s *IssueService) RateSatisfaction(issueID int64, rating int, userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	issue, exists := s.issues[issueID]
	if !exists {
		return fmt.Errorf("issue not found: %d", issueID)
	}

	// Only the user who created the issue can rate it
	if issue.UserID != userID {
		return fmt.Errorf("user %d is not allowed to rate issue %d", userID, issueID)
	}

	issue.Satisfaction = rating
	issue.UpdatedAt = time.Now()
	return s.save()
}

// CloseByUser closes an issue by the user who reported it
func (s *IssueService) CloseByUser(issueID int64, userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	issue, exists := s.issues[issueID]
	if !exists {
		return fmt.Errorf("issue not found: %d", issueID)
	}

	// Only the user who created the issue can close it
	if issue.UserID != userID {
		return fmt.Errorf("user %d is not allowed to close issue %d", userID, issueID)
	}

	issue.Status = IssueStatusClosed
	issue.UpdatedAt = time.Now()
	return s.save()
}

// GetIssuesByStatus returns issues with a specific status
func (s *IssueService) GetIssuesByStatus(status IssueStatus) []*Issue {
	return s.GetFilteredIssues([]IssueStatus{status}, 0)
}

// UpdateStatusWithNotify updates status and sets resolved time if fixed/closed
func (s *IssueService) UpdateStatusWithNotify(issueID int64, status IssueStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	issue, exists := s.issues[issueID]
	if !exists {
		return fmt.Errorf("issue not found: %d", issueID)
	}

	previousStatus, previousUpdatedAt, previousResolvedAt := issue.Status, issue.UpdatedAt, issue.ResolvedAt
	issue.Status = status
	issue.UpdatedAt = time.Now()

	// Set resolved time for fixed or closed status
	if status == IssueStatusFixed || status == IssueStatusClosed {
		now := time.Now()
		issue.ResolvedAt = &now
	}

	if err := s.save(); err != nil {
		issue.Status, issue.UpdatedAt, issue.ResolvedAt = previousStatus, previousUpdatedAt, previousResolvedAt
		return err
	}
	return nil
}

// ReplyTemplate represents a quick reply template
type ReplyTemplate struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// GetReplyTemplates returns available reply templates
func GetReplyTemplates() []ReplyTemplate {
	return []ReplyTemplate{
		{"已收到", "感谢反馈，我们已收到并正在处理。"},
		{"需要信息", "请问您能提供更多细节吗？比如具体的剧集或时间点。"},
		{"已修复", "问题已修复，请重试。"},
		{"正在处理", "我们正在处理此问题，请耐心等待。"},
		{"需要更多时间", "此问题需要更多时间调查，我们会尽快更新进展。"},
		{"无法复现", "我们暂时无法复现此问题，请提供更多详细信息。"},
		{"版本更新", "请尝试更新到最新版本后重试。"},
	}
}
