package services

import (
	"fmt"
	"sort"

	"github.com/xzb177/yimao/pkg/logger"
)

// WashStatusFailed is the terminal state for a wash work order that can never
// be verified automatically. It stops every automatic retry path while keeping
// the record in the wash workbench for manual handling. Records are never
// deleted by this transition.
const WashStatusFailed = "failed"

// FailWashPermanently moves a wash work order into the terminal failed state.
// The reason is preserved in wash_last_error so administrators see why the
// order stopped, and the legacy MoviePilot submission markers are cleared so
// the order can no longer be picked up as a retry candidate.
func (s *ReviewService) FailWashPermanently(requestID, reason string) (*ReviewRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	review, ok := s.reviews[requestID]
	if !ok || review == nil {
		return nil, fmt.Errorf("review request not found: %s", requestID)
	}
	if review.NormalizedBusinessType() != BusinessTypeWash {
		return nil, fmt.Errorf("not a wash work order: %s", requestID)
	}
	if review.Status == "completed" || review.Status == WashStatusFailed {
		return cloneReview(review), nil
	}
	if err := s.failWashPermanentlyLocked(review, reason); err != nil {
		return nil, err
	}
	return cloneReview(review), nil
}

func (s *ReviewService) failWashPermanentlyLocked(review *ReviewRequest, reason string) error {
	previousStatus, previousError := review.Status, review.WashLastError
	previousRetry, previousStuck, previousLast := review.RetryCount, review.Stuck, review.LastError
	previousClaimBy, previousClaimAt := review.WashClaimedBy, review.WashClaimedAt
	review.Status = WashStatusFailed
	review.WashLastError = reason
	// Legacy records carry MoviePilot submission retry markers. Clearing them
	// keeps the order out of every stuck/retry listing while the failed status
	// and wash_last_error keep it auditable.
	review.Stuck = false
	review.LastError = ""
	if review.RetryCount < MaxWashVerifyRetry {
		review.RetryCount = MaxWashVerifyRetry
	}
	review.WashClaimedBy, review.WashClaimedAt = 0, nil
	if err := s.saveLocked(); err != nil {
		review.Status, review.WashLastError = previousStatus, previousError
		review.RetryCount, review.Stuck, review.LastError = previousRetry, previousStuck, previousLast
		review.WashClaimedBy, review.WashClaimedAt = previousClaimBy, previousClaimAt
		return err
	}
	return nil
}

// terminateExhaustedWashOrders converts legacy wash work orders that already
// exceeded the automatic verification cap into the terminal failed state.
// Without this, records written before the cap existed stay in approved state
// forever: every Emby library event re-examines them, the admin workbench shows
// them as active work, and the requester timeline claims they are still being
// processed. Runs once at startup and is idempotent.
func (s *ReviewService) terminateExhaustedWashOrders() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, review := range s.reviews {
		if review == nil || review.NormalizedBusinessType() != BusinessTypeWash {
			continue
		}
		if review.Status != "approved" && review.Status != "claimed" {
			continue
		}
		if !washVerificationExhausted(review) {
			continue
		}
		reason := review.WashLastError
		if reason == "" {
			reason = review.LastError
		}
		if reason == "" {
			reason = "自动核验持续失败"
		}
		annotated := fmt.Sprintf("%s（已达自动核验上限 %d 次，已终止自动重试，等待人工处理）", reason, MaxWashVerifyRetry)
		if err := s.failWashPermanentlyLocked(review, annotated); err != nil {
			logger.Info("[ReviewService] 终止超限洗版工单失败: %s, err=%v", id, err)
			continue
		}
		logger.Info("[ReviewService] 洗版工单已终止自动重试并转入人工处理: %s, 片名=%s, 重试=%d, 原因=%s",
			id, review.MediaTitle, review.RetryCount, reason)
	}
}

// GetFailedWashRequests returns terminal-failed wash work orders, newest first,
// so the admin workbench can keep surfacing them for manual handling.
func (s *ReviewService) GetFailedWashRequests() []*ReviewRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*ReviewRequest, 0)
	for _, review := range s.reviews {
		if review != nil && review.NormalizedBusinessType() == BusinessTypeWash && review.Status == WashStatusFailed {
			items = append(items, cloneReview(review))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items
}

// ReopenWash returns a terminal-failed wash work order to the approved queue as
// an explicit administrator decision. Automatic retry counters are reset so the
// order gets a fresh verification budget; nothing reopens on its own.
func (s *ReviewService) ReopenWash(requestID string, adminID int64) (*ReviewRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	review, ok := s.reviews[requestID]
	if !ok || review == nil || review.NormalizedBusinessType() != BusinessTypeWash {
		return nil, fmt.Errorf("wash work order not found")
	}
	if review.Status != WashStatusFailed {
		return nil, fmt.Errorf("当前状态为 %s，只能重开已终止的洗版工单", review.Status)
	}
	if len(review.WashBaseline) == 0 {
		return nil, fmt.Errorf("缺少创建时基线，重开也无法通过核验；请让用户重新创建洗版工单")
	}
	previousStatus, previousError := review.Status, review.WashLastError
	previousRetry, previousBy := review.RetryCount, review.ReviewedBy
	review.Status = "approved"
	review.WashLastError = ""
	review.RetryCount = 0
	review.ReviewedBy = adminID
	if err := s.saveLocked(); err != nil {
		review.Status, review.WashLastError = previousStatus, previousError
		review.RetryCount, review.ReviewedBy = previousRetry, previousBy
		return nil, err
	}
	logger.Info("[ReviewService] 洗版工单已由管理员 %d 重开: %s", adminID, requestID)
	return cloneReview(review), nil
}
