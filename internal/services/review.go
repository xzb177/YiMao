package services

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/xzb177/yimao/pkg/logger"
	"math/big"
	"os"
	"sync"
	"time"
)

// ReviewRequest represents a media request awaiting review
type ReviewRequest struct {
	RequestID       string            `json:"request_id"` // Unique ID for this review
	TelegramID      int64             `json:"telegram_id"`
	TelegramName    string            `json:"telegram_name"`
	MoviePilotID    int64             `json:"moviepilot_id"`
	TmdbID          int               `json:"tmdb_id"`
	MediaTitle      string            `json:"media_title"`
	MediaYear       int               `json:"media_year"`
	MediaType       MediaType         `json:"media_type"`
	Season          int               `json:"season,omitempty"` // Season number for TV shows (0 = all seasons)
	PosterPath      string            `json:"poster_path,omitempty"`
	Overview        string            `json:"overview,omitempty"`
	Status          string            `json:"status"`   // pending, approved, rejected
	Priority        string            `json:"priority"` // low, normal, high, urgent (default: normal)
	CreatedAt       time.Time         `json:"created_at"`
	ReviewedAt      time.Time         `json:"reviewed_at,omitempty"`
	ReviewedBy      int64             `json:"reviewed_by,omitempty"`
	RejectionReason string            `json:"rejection_reason,omitempty"`
	EmbyExists      bool              `json:"emby_exists,omitempty"`   // Media already exists in Emby
	EmbyInfo        *EmbySearchResult `json:"emby_info,omitempty"`     // Emby media info if exists
	ApproveToken    string            `json:"approve_token,omitempty"` // One-time token for approve action

	// MoviePilot subscription info
	SubscriptionID    int       `json:"subscription_id,omitempty"`     // MoviePilot subscription ID
	SubscriptionState string    `json:"subscription_state,omitempty"`  // N, R, S, D, C, F, X
	LastResubscribeAt time.Time `json:"last_resubscribe_at,omitempty"` // 上次自动重订阅时间

	// 审核通过后向 MoviePilot 提交订阅的兜底状态。
	// 当 Status=="approved" 但提交 MP 失败时，进入 stuck 兜底（而不是凭空消失），
	// 让管理员可见、可手动重试，用户在「我的请求」也能看到「同步中/重试」。
	RetryCount int    `json:"retry_count,omitempty"` // 已重试次数
	LastError  string `json:"last_error,omitempty"`  // 最近一次提交 MP 的错误
	Stuck      bool   `json:"stuck,omitempty"`       // 审核通过但提交 MP 失败，卡住待处理
}

// ReviewService manages review requests
type ReviewService struct {
	reviewsFile     string
	reviews         map[string]*ReviewRequest // requestID -> review
	mu              sync.RWMutex
	moviepilot      *MoviePilotClient // For updating subscription status
	autoResubscribe bool
	// OnSubscriptionComplete 订阅完成时的通知回调（由 main 注入）。
	// 参数：telegramID, mediaTitle, year, mediaType。
	// 解耦 ReviewService 与 Telegram client，Emby 不可用时用 MP 轮询触发此回调即可。
	OnSubscriptionComplete func(telegramID int64, title string, year int, mediaType string)

	// Alert 当 review 进入 stuck（MP 提交失败）时的告警回调（由 main 注入）。
	// 参数：requestID, mediaTitle, retryCount, lastError。
	Alert func(requestID, title string, retryCount int, lastError string)

	// ── 全量 MP 订阅完成检测（Issue #1）──
	// notifiedSubs 记录「已通知过的 MP 订阅 ID」，避免重复通知。
	notifiedSubsFile string
	notifiedSubs     map[int]bool
	// userMapping 用于 MP 用户名 → Telegram ID 反查。
	userMapping *UserMappingService
}

// NewReviewService creates a new review service
func NewReviewService(dataDir string, autoResubscribe bool) *ReviewService {
	reviewsFile := fmt.Sprintf("%s/review_requests.json", dataDir)

	service := &ReviewService{
		reviewsFile:      reviewsFile,
		reviews:          make(map[string]*ReviewRequest),
		autoResubscribe:  autoResubscribe,
		notifiedSubsFile: fmt.Sprintf("%s/notified_subs.json", dataDir),
		notifiedSubs:     make(map[int]bool),
	}

	service.load()
	service.loadNotifiedSubs()

	// Start cleanup routine for old reviews
	go service.cleanupRoutine()

	return service
}

// SetUserMapping 注入用户映射服务（用于 MP 用户名 → Telegram ID 反查）。
func (s *ReviewService) SetUserMapping(um *UserMappingService) {
	s.userMapping = um
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

	logger.Info("[ReviewService] Loaded %d review requests", len(s.reviews))
	return nil
}

// saveLocked saves reviews to file (must be called with lock held)
func (s *ReviewService) saveLocked() error {
	data, err := json.MarshalIndent(s.reviews, "", "  ")
	if err != nil {
		logger.Info("[ReviewService] 序列化失败: %v", err)
		return err
	}

	if err := atomicWriteFile(s.reviewsFile, data, 0644); err != nil {
		logger.Info("[ReviewService] 写入文件失败: %v", err)
		return err
	}

	logger.Info("[ReviewService] 保存 %d 条审核请求", len(s.reviews))
	return nil
}

// ── 全量 MP 订阅完成检测（Issue #1）──

// loadNotifiedSubs 从文件加载已通知的订阅 ID 集合。
func (s *ReviewService) loadNotifiedSubs() {
	data, err := os.ReadFile(s.notifiedSubsFile)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Info("[ReviewService] 加载 notified_subs 失败: %v", err)
		}
		return
	}
	var ids []int
	if err := json.Unmarshal(data, &ids); err != nil {
		logger.Info("[ReviewService] 解析 notified_subs 失败: %v", err)
		return
	}
	for _, id := range ids {
		s.notifiedSubs[id] = true
	}
	logger.Info("[ReviewService] 已加载 %d 条已通知订阅记录", len(ids))
}

// saveNotifiedSubs 持久化已通知的订阅 ID 集合。
func (s *ReviewService) saveNotifiedSubs() {
	ids := make([]int, 0, len(s.notifiedSubs))
	for id := range s.notifiedSubs {
		ids = append(ids, id)
	}
	data, err := json.MarshalIndent(ids, "", "  ")
	if err != nil {
		logger.Info("[ReviewService] 序列化 notified_subs 失败: %v", err)
		return
	}
	if err := atomicWriteFile(s.notifiedSubsFile, data, 0644); err != nil {
		logger.Info("[ReviewService] 写入 notified_subs 失败: %v", err)
	}
}

// checkAllNewCompletions 全量检测 MP 中新完成的订阅（不仅是 YiMao 发起的）。
// 流程：拉 MP 全部订阅 → 过滤 state=="C" → 去重（已通知的跳过）→ 用户名反查 TG ID → 触发通知。
func (s *ReviewService) checkAllNewCompletions() {
	if s.moviepilot == nil || s.userMapping == nil || s.OnSubscriptionComplete == nil {
		return
	}

	subs, err := s.moviepilot.GetAllSubscriptions()
	if err != nil {
		logger.Info("[ReviewService] 全量检测拉取 MP 订阅失败: %v", err)
		return
	}

	newCount := 0
	for i := range subs {
		sub := &subs[i]

		// 只关心已完成的
		if sub.State != StateCompleted {
			continue
		}

		// 已通知过的跳过
		if s.notifiedSubs[sub.ID] {
			continue
		}

		// 检查是否已有审核单跟踪（已有审核单的走 updateAllSubscriptionStatus，不重复通知）
		s.mu.RLock()
		alreadyTracked := false
		for _, rv := range s.reviews {
			if rv.SubscriptionID == sub.ID {
				alreadyTracked = true
				break
			}
		}
		s.mu.RUnlock()
		if alreadyTracked {
			// 标记已通知，避免下次重复检查
			s.notifiedSubs[sub.ID] = true
			continue
		}

		// MP 用户名 → Telegram ID
		tgID, found := s.userMapping.GetTelegramIDByMoviePilotUsername(sub.Username)
		if !found || tgID == 0 {
			// 找不到对应用户，标记已通知避免下次重复检查
			logger.Info("[ReviewService] 全量检测：订阅 %d (%s) 用户 %s 未绑定 TG，跳过", sub.ID, sub.Name, sub.Username)
			s.notifiedSubs[sub.ID] = true
			continue
		}

		// 解析年份
		year := 0
		fmt.Sscanf(sub.Year, "%d", &year)

		// 触发通知
		mediaType := sub.Type
		if mediaType == "" {
			mediaType = "movie"
		}

		logger.Info("[ReviewService] 全量检测：新完成订阅 %d (%s)，通知用户 %d", sub.ID, sub.Name, tgID)
		go s.OnSubscriptionComplete(tgID, sub.Name, year, mediaType)

		// 标记已通知
		s.notifiedSubs[sub.ID] = true
		newCount++
	}

	if newCount > 0 {
		s.saveNotifiedSubs()
		logger.Info("[ReviewService] 全量检测完成：新增通知 %d 条", newCount)
	}
}

// CreateRequest creates a new review request
func (s *ReviewService) CreateRequest(review *ReviewRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	review.CreatedAt = time.Now()
	review.Status = "pending"

	// Set default priority if not specified
	if review.Priority == "" {
		review.Priority = "normal"
	}

	// Generate approve token - one-time use token to prevent duplicate approvals
	review.ApproveToken = generateApproveToken()

	s.reviews[review.RequestID] = review

	// Map priority to Chinese for logging
	priorityText := map[string]string{
		"low":    "较低",
		"normal": "普通",
		"high":   "较高",
		"urgent": "紧急",
	}[review.Priority]
	if priorityText == "" {
		priorityText = review.Priority
	}

	logger.Info("[审核] 创建请求: %s, 用户: %d, 优先级: %s, 影片: %s, 令牌: %s",
		review.RequestID, review.TelegramID, priorityText, review.MediaTitle, review.ApproveToken)

	return s.saveLocked()
}

// generateApproveToken generates a unique token for approve action
func generateApproveToken() string {
	return fmt.Sprintf("%d_%s", time.Now().UnixNano(), randomString(8))
}

// randomString generates a cryptographically random string of given length
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	max := big.NewInt(int64(len(charset)))

	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			// Fallback to time-based if crypto fails
			b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
			continue
		}
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

// GetRequest retrieves a review request by ID
func (s *ReviewService) GetRequest(requestID string) (*ReviewRequest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	review, exists := s.reviews[requestID]
	return review, exists
}

// GetRequestByToken retrieves a review request by approve token
// Searches all requests (not just pending) to handle duplicate approval attempts
func (s *ReviewService) GetRequestByToken(token string) (*ReviewRequest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, review := range s.reviews {
		if review.ApproveToken == token {
			return review, true
		}
	}
	return nil, false
}

// GetPendingRequests returns all pending review requests sorted by created time
func (s *ReviewService) GetPendingRequests() []*ReviewRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var pending []*ReviewRequest
	for _, review := range s.reviews {
		if review.Status == "pending" {
			pending = append(pending, review)
		}
	}

	// Sort by created time desc (newer first)
	for i := 0; i < len(pending); i++ {
		for j := i + 1; j < len(pending); j++ {
			if pending[i].CreatedAt.Before(pending[j].CreatedAt) {
				pending[i], pending[j] = pending[j], pending[i]
			}
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

// HasActiveSimilarRequest checks if user already has a similar active request
func (s *ReviewService) HasActiveSimilarRequest(telegramID int64, tmdbID int, mediaType MediaType, season int) (*ReviewRequest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, review := range s.reviews {
		if review == nil {
			continue
		}
		if review.TelegramID != telegramID {
			continue
		}
		if review.TmdbID != tmdbID || review.MediaType != mediaType {
			continue
		}

		// TV requests should match season; movie season is always ignored
		if mediaType == MediaTypeTV && review.Season != season {
			continue
		}

		// Active statuses considered duplicate to prevent repeated submissions
		if review.Status == "pending" || review.Status == "approved" {
			return review, true
		}
	}

	return nil, false
}

// Approve approves a review request with token verification
func (s *ReviewService) Approve(requestID string, reviewedBy int64, token string) (*ReviewRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	review, exists := s.reviews[requestID]
	if !exists {
		return nil, fmt.Errorf("review request not found: %s", requestID)
	}

	// Check if request is still pending
	if review.Status != "pending" {
		// Return the review without error so caller can handle duplicate approval gracefully
		if review.Status == "approved" {
			logger.Info("[ReviewService] 请求已被批准: %s, 由: %d", requestID, review.ReviewedBy)
			return review, fmt.Errorf("already_approved")
		}
		return nil, fmt.Errorf("请求状态为 %s, 无法批准", review.Status)
	}

	// Verify token to prevent duplicate approvals
	if review.ApproveToken == "" || review.ApproveToken != token {
		logger.Info("[ReviewService] 无效的批准令牌")
		return nil, fmt.Errorf("invalid or expired approve token")
	}

	// Don't clear the token - keep it for status tracking
	// The status check above prevents duplicate approvals
	review.Status = "approved"
	review.ReviewedAt = time.Now()
	review.ReviewedBy = reviewedBy

	logger.Info("[ReviewService] Approved review request: %s by admin: %d", requestID, reviewedBy)

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

	logger.Info("[ReviewService] Updated subscription info for %s: ID=%d, State=%s", requestID, subscriptionID, state)

	return s.saveLocked()
}

// MarkStuck 记录「审核已通过但提交 MoviePilot 失败」的兜底状态。
// 不改变 Status（仍为 approved），仅累加 RetryCount + 记录 LastError + 置 Stuck，
// 这样请求不会凭空消失：管理员面板可见、可手动重试，用户在「我的请求」也能看到。
const MaxApproveRetry = 3

func (s *ReviewService) MarkStuck(requestID string, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	review, exists := s.reviews[requestID]
	if !exists {
		return fmt.Errorf("review request not found: %s", requestID)
	}

	review.RetryCount++
	review.LastError = errMsg
	review.Stuck = true

	logger.Info("[ReviewService] 请求提交 MP 失败进入 stuck 兜底: %s, 第 %d 次, err=%s",
		requestID, review.RetryCount, errMsg)

	// B4: 告警 — stuck 时通知管理员
	if s.Alert != nil {
		go s.Alert(requestID, review.MediaTitle, review.RetryCount, errMsg)
	}

	return s.saveLocked()
}

// ClearStuck 在提交 MoviePilot 成功后清除兜底状态。
func (s *ReviewService) ClearStuck(requestID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	review, exists := s.reviews[requestID]
	if !exists {
		return fmt.Errorf("review request not found: %s", requestID)
	}

	if !review.Stuck && review.LastError == "" {
		return nil
	}
	review.Stuck = false
	review.LastError = ""

	return s.saveLocked()
}

// GetStuckRequests 返回所有卡在「已审核但未成功提交 MP」的请求（供管理员面板/重试用）。
func (s *ReviewService) GetStuckRequests() []*ReviewRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var stuck []*ReviewRequest
	for _, review := range s.reviews {
		if review.Stuck {
			stuck = append(stuck, review)
		}
	}
	return stuck
}

// GetSubscriptionStateText returns user-friendly text for subscription state
func GetSubscriptionStateText(state string) string {
	switch state {
	case "N": // New
		return "⏳ 等待搜索"
	case "R": // Recycled/Running - 订阅进行中，等待资源或下载
		return "🔄 订阅中"
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

	logger.Info("[ReviewService] Rejected review request: %s, reason: %s", requestID, reason)

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

	logger.Info("[ReviewService] Deleted review request: %s", requestID)

	return s.saveLocked()
}

// CancelByUser 允许用户撤回自己 pending 状态的请求。
// 仅 pending 可撤回；已审核通过/已拒绝的不允许（防止撤回绕过审核）。
func (s *ReviewService) CancelByUser(requestID string, telegramID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	review, exists := s.reviews[requestID]
	if !exists {
		return fmt.Errorf("review request not found: %s", requestID)
	}

	if review.TelegramID != telegramID {
		return fmt.Errorf("permission denied: not your request")
	}

	if review.Status != "pending" {
		return fmt.Errorf("cannot cancel: status is %s, only pending can be cancelled", review.Status)
	}

	review.Status = "cancelled"
	review.RejectionReason = "用户主动撤回"
	review.ReviewedAt = time.Now()

	logger.Info("[ReviewService] 用户撤回请求: %s, 用户: %d, 影片: %s",
		requestID, telegramID, review.MediaTitle)

	return s.saveLocked()
}

// cleanupRoutine periodically removes old completed reviews
func (s *ReviewService) cleanupRoutine() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Info("[ReviewService] Panic in cleanup routine: %v, recovering...", r)
				}
			}()
			s.cleanup()
		}()
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
			logger.Info("[ReviewService] Cleaning up old approved review without subscription: %s", id)
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
		logger.Info("[ReviewService] Cleaned up %d old review requests", len(toDelete))
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
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Info("[ReviewService] Panic in initial subscription refresh: %v, recovering...", r)
			}
		}()
		s.updateAllSubscriptionStatus()
		s.checkAllNewCompletions()
	}()

	for range ticker.C {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Info("[ReviewService] Panic in subscription refresh: %v, recovering...", r)
				}
			}()
			s.updateAllSubscriptionStatus()
			// 全量检测：MP 中所有新完成的订阅（不仅是 YiMao 发起的）
			s.checkAllNewCompletions()
		}()
	}
}

// updateAllSubscriptionStatus updates subscription status for all approved reviews
func (s *ReviewService) updateAllSubscriptionStatus() {
	s.mu.RLock()
	var toUpdate []struct {
		requestID string
		subID     int
	}
	seenSubID := make(map[int]string)
	for _, review := range s.reviews {
		if review.Status == "approved" && review.SubscriptionID > 0 {
			if existedReqID, dup := seenSubID[review.SubscriptionID]; dup {
				logger.Info("[ReviewService] Skip duplicate subscription tracker: subID=%d, request=%s, existed=%s", review.SubscriptionID, review.RequestID, existedReqID)
				continue
			}
			seenSubID[review.SubscriptionID] = review.RequestID
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

	logger.Info("[ReviewService] Updating subscription status for %d requests", len(toUpdate))

	// Get all subscriptions from MoviePilot
	subs, err := s.moviepilot.GetAllSubscriptions()
	if err != nil {
		logger.Info("[ReviewService] Failed to get subscriptions: %v", err)
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

	// Track recycled subscriptions that need to be resubscribed
	var toResubscribe []string
	resubSeen := make(map[int]bool)
	const resubscribeCooldown = 30 * time.Minute

	for _, item := range toUpdate {
		if sub, exists := subMap[item.subID]; exists {
			if review, ok := s.reviews[item.requestID]; ok {
				// Determine the actual subscription state
				// MoviePilot doesn't have "C" (Completed) state, so we check lack_episode
				actualState := sub.State
				if sub.LackEpisode == 0 && sub.TotalEpisode > 0 {
					// All episodes downloaded - mark as completed
					actualState = "C"
				} else if sub.TotalEpisode == 0 {
					// Unknown total episodes, keep original state
					actualState = sub.State
				}

				if review.SubscriptionState != actualState {
					oldState := review.SubscriptionState
					review.SubscriptionState = actualState
					logger.Info("[ReviewService] Updated %s: %s -> %s (lack=%d/%d)", item.requestID, oldState, actualState, sub.LackEpisode, sub.TotalEpisode)

					// P1 通知：订阅完成时通知用户（替代 Emby webhook，用 MP 轮询触发）
					if actualState == "C" && oldState != "C" && s.OnSubscriptionComplete != nil {
						go func(r *ReviewRequest) {
							defer func() {
								if rec := recover(); rec != nil {
									logger.Info("[ReviewService] Panic in completion notification: %v", rec)
								}
							}()
							s.OnSubscriptionComplete(r.TelegramID, r.MediaTitle, r.MediaYear, string(r.MediaType))
						}(review)
					}

					// If state is "R" (Recycled), mark for resubscription (dedupe by subscription ID)
					// Only trigger resubscribe if not completed
					if sub.State == "R" && actualState != "C" {
						if !review.LastResubscribeAt.IsZero() && time.Since(review.LastResubscribeAt) < resubscribeCooldown {
							logger.Info("[ReviewService] Skip recycle trigger in cooldown: request=%s, last=%s", item.requestID, review.LastResubscribeAt.Format(time.RFC3339))
							continue
						}
						if !resubSeen[item.subID] {
							toResubscribe = append(toResubscribe, item.requestID)
							resubSeen[item.subID] = true
						} else {
							logger.Info("[ReviewService] Skip duplicate recycle trigger: subID=%d, request=%s", item.subID, item.requestID)
						}
					}
				}
			}
		} else {
			// Subscription not found in MoviePilot - it may have been deleted
			if review, ok := s.reviews[item.requestID]; ok {
				if review.SubscriptionState != "" {
					oldState := review.SubscriptionState
					// Mark as cancelled/removed
					review.SubscriptionState = "X"
					logger.Info("[ReviewService] Subscription %d not found in MoviePilot, marked as cancelled: %s (was: %s)",
						item.subID, item.requestID, oldState)
				}
			}
		}
	}

	s.saveLocked()

	// Resubscribe recycled subscriptions (在锁外执行避免死锁)
	if len(toResubscribe) > 0 {
		if s.autoResubscribe {
			go s.resubscribeRecycledRequests(toResubscribe)
		} else {
			logger.Info("[ReviewService] Auto resubscribe disabled, skipped %d recycled requests", len(toResubscribe))
		}
	}
}

// resubscribeRecycledRequests resubscribes requests that are in "R" (Recycled) state
func (s *ReviewService) resubscribeRecycledRequests(requestIDs []string) {
	processedSubID := make(map[int]bool)
	for _, requestID := range requestIDs {
		s.mu.RLock()
		review, exists := s.reviews[requestID]
		s.mu.RUnlock()

		if !exists {
			continue
		}

		if review.SubscriptionID > 0 {
			if processedSubID[review.SubscriptionID] {
				logger.Info("[ReviewService] Skip duplicate resubscribe for same subID=%d, request=%s", review.SubscriptionID, requestID)
				continue
			}
			processedSubID[review.SubscriptionID] = true
		}

		// Delete old subscription first
		if review.SubscriptionID > 0 {
			logger.Info("[ReviewService] Deleting old subscription %d for %s", review.SubscriptionID, requestID)
			if err := s.moviepilot.DeleteRequest(review.SubscriptionID); err != nil {
				logger.Info("[ReviewService] Failed to delete old subscription: %v", err)
			}
		}

		// Resubscribe
		logger.Info("[ReviewService] Resubscribing %s: %s (%d)", requestID, review.MediaTitle, review.TmdbID)
		mpMediaType := MediaTypeMovie
		if review.MediaType == MediaTypeTV {
			mpMediaType = MediaTypeTV
		}

		season := review.Season
		if season == 0 && review.MediaType == MediaTypeTV {
			season = 1
		}

		req, err := s.moviepilot.RequestMedia(
			review.MediaTitle,
			review.MediaYear,
			review.TmdbID,
			mpMediaType,
			season,
		)
		if err != nil {
			logger.Info("[ReviewService] Failed to resubscribe %s: %v", requestID, err)
			continue
		}

		// Update subscription info
		if err := s.UpdateSubscriptionInfo(requestID, req.ID, "N"); err != nil {
			logger.Info("[ReviewService] Failed to update subscription info: %v", err)
		}

		// Mark last resubscribe time to avoid frequent recycle loops
		s.mu.Lock()
		if r, ok := s.reviews[requestID]; ok {
			r.LastResubscribeAt = time.Now()
			s.saveLocked()
		}
		s.mu.Unlock()

		logger.Info("[ReviewService] Resubscribed %s: new subscription ID %d", requestID, req.ID)
	}
}
