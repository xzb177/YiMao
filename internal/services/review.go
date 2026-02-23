package services

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// ReviewRequest represents a media request awaiting review
type ReviewRequest struct {
	RequestID      string    `json:"request_id"`      // Unique ID for this review
	TelegramID     int64     `json:"telegram_id"`
	TelegramName   string    `json:"telegram_name"`
	MoviePilotID   int64     `json:"moviepilot_id"`
	TmdbID         int       `json:"tmdb_id"`
	MediaTitle     string    `json:"media_title"`
	MediaYear      int       `json:"media_year"`
	MediaType      MediaType `json:"media_type"`
	Season         int       `json:"season,omitempty"` // Season number for TV shows (0 = all seasons)
	PosterPath     string    `json:"poster_path,omitempty"`
	Overview       string    `json:"overview,omitempty"`
	Status         string    `json:"status"`         // pending, approved, rejected
	CreatedAt      time.Time `json:"created_at"`
	ReviewedAt     time.Time `json:"reviewed_at,omitempty"`
	ReviewedBy     int64     `json:"reviewed_by,omitempty"`
	RejectionReason string   `json:"rejection_reason,omitempty"`
	EmbyExists     bool      `json:"emby_exists,omitempty"` // Media already exists in Emby
	EmbyInfo       *EmbySearchResult `json:"emby_info,omitempty"`   // Emby media info if exists

	// MoviePilot subscription info
	SubscriptionID    int    `json:"subscription_id,omitempty"`    // MoviePilot subscription ID
	SubscriptionState string `json:"subscription_state,omitempty"` // N, R, S, D, C, F, X
}

// ReviewService manages review requests
type ReviewService struct {
	reviewsFile  string
	reviews      map[string]*ReviewRequest // requestID -> review
	mu           sync.RWMutex
	moviepilot   *MoviePilotClient // For updating subscription status
}

// NewReviewService creates a new review service
func NewReviewService(dataDir string) *ReviewService {
	reviewsFile := fmt.Sprintf("%s/review_requests.json", dataDir)

	service := &ReviewService{
		reviewsFile: reviewsFile,
		reviews:     make(map[string]*ReviewRequest),
	}

	service.load()

	// Start cleanup routine for old reviews
	go service.cleanupRoutine()

	return service
}

// SetMoviePilotClient sets the MoviePilot client (called after initialization)
func (s *ReviewService) SetMoviePilotClient(mp *MoviePilotClient) {
	s.moviepilot = mp
	// Start subscription status refresh routine
	go s.refreshSubscriptionStatus()
}

// load loads reviews from file
func (s *ReviewService) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.reviewsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if err := json.Unmarshal(data, &s.reviews); err != nil {
		return err
	}

	log.Printf("[ReviewService] Loaded %d review requests", len(s.reviews))
	return nil
}

// saveLocked saves reviews to file (must be called with lock held)
func (s *ReviewService) saveLocked() error {
	log.Printf("[ReviewService] saveLocked: Starting marshal of %d reviews", len(s.reviews))
	data, err := json.MarshalIndent(s.reviews, "", "  ")
	if err != nil {
		log.Printf("[ReviewService] Failed to marshal reviews: %v", err)
		return err
	}
	log.Printf("[ReviewService] saveLocked: Marshaled %d bytes", len(data))

	log.Printf("[ReviewService] saveLocked: Writing to %s", s.reviewsFile)
	if err := os.WriteFile(s.reviewsFile, data, 0644); err != nil {
		log.Printf("[ReviewService] Failed to write reviews file %s: %v", s.reviewsFile, err)
		return err
	}

	log.Printf("[ReviewService] Saved %d reviews to %s", len(s.reviews), s.reviewsFile)
	return nil
}

// CreateRequest creates a new review request
func (s *ReviewService) CreateRequest(review *ReviewRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	review.CreatedAt = time.Now()
	review.Status = "pending"

	s.reviews[review.RequestID] = review

	log.Printf("[ReviewService] Created review request: %s for user %d", review.RequestID, review.TelegramID)
	log.Printf("[ReviewService] About to save %d reviews to %s", len(s.reviews), s.reviewsFile)

	err := s.saveLocked()
	if err != nil {
		log.Printf("[ReviewService] ERROR saving reviews: %v", err)
		return err
	}

	log.Printf("[ReviewService] Successfully saved review request")
	return nil
}

// GetRequest retrieves a review request by ID
func (s *ReviewService) GetRequest(requestID string) (*ReviewRequest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	review, exists := s.reviews[requestID]
	return review, exists
}

// GetPendingRequests returns all pending review requests
func (s *ReviewService) GetPendingRequests() []*ReviewRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var pending []*ReviewRequest
	for _, review := range s.reviews {
		if review.Status == "pending" {
			pending = append(pending, review)
		}
	}

	return pending
}

// GetUserRequests returns all review requests for a user
func (s *ReviewService) GetUserRequests(telegramID int64) []*ReviewRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var userReviews []*ReviewRequest
	for _, review := range s.reviews {
		if review.TelegramID == telegramID {
			userReviews = append(userReviews, review)
		}
	}

	// Sort by created time desc
	for i := 0; i < len(userReviews); i++ {
		for j := i + 1; j < len(userReviews); j++ {
			if userReviews[i].CreatedAt.Before(userReviews[j].CreatedAt) {
				userReviews[i], userReviews[j] = userReviews[j], userReviews[i]
			}
		}
	}

	return userReviews
}

// Approve approves a review request
func (s *ReviewService) Approve(requestID string, reviewedBy int64) (*ReviewRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	review, exists := s.reviews[requestID]
	if !exists {
		return nil, fmt.Errorf("review request not found: %s", requestID)
	}

	review.Status = "approved"
	review.ReviewedAt = time.Now()
	review.ReviewedBy = reviewedBy

	log.Printf("[ReviewService] Approved review request: %s", requestID)

	return review, s.saveLocked()
}

// UpdateSubscriptionInfo updates the MoviePilot subscription info for a review
func (s *ReviewService) UpdateSubscriptionInfo(requestID string, subscriptionID int, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	review, exists := s.reviews[requestID]
	if !exists {
		return fmt.Errorf("review request not found: %s", requestID)
	}

	review.SubscriptionID = subscriptionID
	review.SubscriptionState = state

	log.Printf("[ReviewService] Updated subscription info for %s: ID=%d, State=%s", requestID, subscriptionID, state)

	return s.saveLocked()
}

// GetSubscriptionStateText returns user-friendly text for subscription state
func GetSubscriptionStateText(state string) string {
	switch state {
	case "N": // New
		return "⏳ 等待搜索"
	case "R": // Recycled
		return "🔄 重新搜索"
	case "S": // Searching
		return "🔍 搜索中"
	case "D": // Downloading
		return "📥 下载中"
	case "C": // Completed
		return "✅ 已完成"
	case "F": // Failed
		return "❌ 失败"
	case "X": // Cancelled
		return "🚫 已取消"
	default:
		return "❓ 未知状态"
	}
}

// Reject rejects a review request
func (s *ReviewService) Reject(requestID string, reviewedBy int64, reason string) (*ReviewRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	review, exists := s.reviews[requestID]
	if !exists {
		return nil, fmt.Errorf("review request not found: %s", requestID)
	}

	review.Status = "rejected"
	review.ReviewedAt = time.Now()
	review.ReviewedBy = reviewedBy
	review.RejectionReason = reason

	log.Printf("[ReviewService] Rejected review request: %s, reason: %s", requestID, reason)

	return review, s.saveLocked()
}

// DeleteRequest deletes a review request
func (s *ReviewService) DeleteRequest(requestID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.reviews[requestID]; !exists {
		return fmt.Errorf("review request not found: %s", requestID)
	}

	delete(s.reviews, requestID)

	log.Printf("[ReviewService] Deleted review request: %s", requestID)

	return s.saveLocked()
}

// cleanupRoutine periodically removes old completed reviews
func (s *ReviewService) cleanupRoutine() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		s.cleanup()
	}
}

// cleanup removes reviews older than 7 days that are approved/rejected
func (s *ReviewService) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().AddDate(0, 0, -7)
	var toDelete []string

	for id, review := range s.reviews {
		// Also delete approved reviews without subscription ID (old data before tracking)
		if review.Status == "approved" && review.SubscriptionID == 0 && !review.ReviewedAt.IsZero() && review.ReviewedAt.Before(cutoff) {
			toDelete = append(toDelete, id)
			log.Printf("[ReviewService] Cleaning up old approved review without subscription: %s", id)
			continue
		}

		if (review.Status == "approved" || review.Status == "rejected") &&
			review.ReviewedAt.Before(cutoff) {
			toDelete = append(toDelete, id)
		}
	}

	for _, id := range toDelete {
		delete(s.reviews, id)
	}

	if len(toDelete) > 0 {
		log.Printf("[ReviewService] Cleaned up %d old review requests", len(toDelete))
		s.saveLocked()
	}
}

// GetStats returns review statistics
func (s *ReviewService) GetStats() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := map[string]int{
		"pending":  0,
		"approved": 0,
		"rejected": 0,
		"total":    len(s.reviews),
	}

	for _, review := range s.reviews {
		stats[review.Status]++
	}

	return stats
}

// refreshSubscriptionStatus periodically updates subscription status from MoviePilot
func (s *ReviewService) refreshSubscriptionStatus() {
	if s.moviepilot == nil {
		return
	}

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	// Initial refresh
	s.updateAllSubscriptionStatus()

	for range ticker.C {
		s.updateAllSubscriptionStatus()
	}
}

// updateAllSubscriptionStatus updates subscription status for all approved reviews
func (s *ReviewService) updateAllSubscriptionStatus() {
	s.mu.RLock()
	var toUpdate []struct {
		requestID string
		subID     int
	}
	for _, review := range s.reviews {
		if review.Status == "approved" && review.SubscriptionID > 0 {
			toUpdate = append(toUpdate, struct {
				requestID string
				subID     int
			}{
				requestID: review.RequestID,
				subID:     review.SubscriptionID,
			})
		}
	}
	s.mu.RUnlock()

	if len(toUpdate) == 0 {
		return
	}

	log.Printf("[ReviewService] Updating subscription status for %d requests", len(toUpdate))

	// Get all subscriptions from MoviePilot
	subs, err := s.moviepilot.GetAllSubscriptions()
	if err != nil {
		log.Printf("[ReviewService] Failed to get subscriptions: %v", err)
		return
	}

	// Create a map for quick lookup
	subMap := make(map[int]*SubscribeStatus)
	for i := range subs {
		subMap[subs[i].ID] = &subs[i]
	}

	// Update each review
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, item := range toUpdate {
		if sub, exists := subMap[item.subID]; exists {
			if review, ok := s.reviews[item.requestID]; ok {
				if review.SubscriptionState != sub.State {
					review.SubscriptionState = sub.State
					log.Printf("[ReviewService] Updated %s: %s -> %s", item.requestID, review.SubscriptionState, sub.State)
				}
			}
		}
	}

	s.saveLocked()
}
