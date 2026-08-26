package services

import (
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
)

// defaultReviewOverdueThreshold is how long a request may wait for an admin
// decision before it is reported as overdue. Override with REVIEW_OVERDUE_HOURS.
const defaultReviewOverdueThreshold = 48 * time.Hour

// reviewOverdueStartupDelay lets main.go finish injecting callbacks before the
// first overdue sweep runs, so the first reminder is not silently dropped.
const reviewOverdueStartupDelay = 2 * time.Minute

// reviewOverdueThreshold reads the configured overdue threshold.
func reviewOverdueThreshold() time.Duration {
	raw := os.Getenv("REVIEW_OVERDUE_HOURS")
	if raw == "" {
		return defaultReviewOverdueThreshold
	}
	hours, err := strconv.Atoi(raw)
	if err != nil || hours <= 0 {
		return defaultReviewOverdueThreshold
	}
	return time.Duration(hours) * time.Hour
}

// OverduePendingRequests returns pending requests that have been waiting for an
// admin decision longer than the threshold and were never reported yet. Oldest
// first, because that is the order an administrator should work through.
func (s *ReviewService) OverduePendingRequests(threshold time.Duration) []*ReviewRequest {
	if threshold <= 0 {
		threshold = reviewOverdueThreshold()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	items := make([]*ReviewRequest, 0)
	for _, review := range s.reviews {
		if review == nil || review.Status != "pending" {
			continue
		}
		if review.OverdueRemindedAt != nil && !review.OverdueRemindedAt.IsZero() {
			continue // already reported once; never spam the same request again
		}
		if review.CreatedAt.IsZero() || now.Sub(review.CreatedAt) < threshold {
			continue
		}
		items = append(items, cloneReview(review))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	return items
}

// MarkOverdueReminded persists that an overdue reminder was delivered for these
// requests. Persisting the marker is what guarantees one reminder per request
// across restarts instead of an hourly repeat.
func (s *ReviewService) MarkOverdueReminded(requestIDs []string) error {
	if len(requestIDs) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	type undo struct {
		review *ReviewRequest
		before *time.Time
	}
	var changes []undo
	for _, id := range requestIDs {
		review, ok := s.reviews[id]
		if !ok || review == nil {
			continue
		}
		if review.OverdueRemindedAt != nil && !review.OverdueRemindedAt.IsZero() {
			continue
		}
		changes = append(changes, undo{review: review, before: review.OverdueRemindedAt})
		marked := now
		review.OverdueRemindedAt = &marked
	}
	if len(changes) == 0 {
		return nil
	}
	if err := s.saveLocked(); err != nil {
		for _, change := range changes {
			change.review.OverdueRemindedAt = change.before
		}
		return err
	}
	return nil
}

// remindOverduePendingReviews reports long-pending requests to administrators.
// The marker is only persisted after the callback confirms delivery, so a failed
// notification is retried on the next sweep instead of being lost.
func (s *ReviewService) remindOverduePendingReviews() {
	if s.OnOverdueReviews == nil {
		return
	}
	threshold := reviewOverdueThreshold()
	overdue := s.OverduePendingRequests(threshold)
	if len(overdue) == 0 {
		return
	}
	if !s.OnOverdueReviews(overdue, threshold) {
		logger.Info("[ReviewService] 超期待审核提醒未送达，%d 条保持未提醒状态，稍后重试", len(overdue))
		return
	}
	ids := make([]string, 0, len(overdue))
	for _, review := range overdue {
		ids = append(ids, review.RequestID)
	}
	if err := s.MarkOverdueReminded(ids); err != nil {
		logger.Info("[ReviewService] 超期提醒标记持久化失败: %v", err)
		return
	}
	logger.Info("[ReviewService] 已提醒管理员 %d 条超期待审核请求（阈值 %s）", len(ids), threshold)
}
