package services

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/xzb177/yimao/pkg/logger"
	"math/big"
	"os"
	"strings"
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
	SubscriptionID    int        `json:"subscription_id,omitempty"`     // MoviePilot subscription ID
	SubscriptionState string     `json:"subscription_state,omitempty"`  // N, R, S, D, C, F, X
	LastResubscribeAt time.Time  `json:"last_resubscribe_at,omitempty"` // 上次自动重订阅时间
	LibraryNotifiedAt *time.Time `json:"library_notified_at,omitempty"` // Emby 入库后已私聊通知时间，防重复通知

	// 审核通过后向 MoviePilot 提交订阅的兜底状态。
	// 当 Status=="approved" 但提交 MP 失败时，进入 stuck 兜底（而不是凭空消失），
	// 让管理员可见、可手动重试，用户在「我的请求」也能看到「同步中/重试」。
	RetryCount int    `json:"retry_count,omitempty"` // 已重试次数
	LastError  string `json:"last_error,omitempty"`  // 最近一次提交 MP 的错误
	Stuck      bool   `json:"stuck,omitempty"`       // 审核通过但提交 MP 失败，卡住待处理

	// OrphanRetryCount 订阅在 MP 中找不到时的重试次数（最多 3 次后标记取消）
	OrphanRetryCount int `json:"orphan_retry_count,omitempty"`

	// 求片路径追踪
	RequestOrigin  string `json:"request_origin,omitempty"`  // "normal" | "adventure"
	AdventureScore int    `json:"adventure_score,omitempty"` // 冒险得分
	AdventureGrade string `json:"adventure_grade,omitempty"` // 冒险评级 SSS-SS-S-A-B-C-D

	QuotaCost     int  `json:"quota_cost,omitempty"`     // 创建时实际扣除配额；0 表示旧 JSON
	QuotaRestored bool `json:"quota_restored,omitempty"` // 已返还，持久化保证幂等
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
	notifiedMu       sync.RWMutex // protects notifiedSubs map access
	// userMapping 用于 MP 用户名 → Telegram ID 反查。
	userMapping UserMappingStore

	// ── 每日汇总 ──
	dailyMu          sync.Mutex
	dailyCompletions []DailyCompletion // 今日新完成的订阅
	dailySummaryHour int               // 汇总发送时间（小时，24h 制）
	dailySummaryMin  int               // 汇总发送时间（分钟）
	// OnDailySummary 每日汇总回调（由 main 注入）。
	// 参数：用户 ID, 格式化后的汇总消息。
	OnDailySummary func(telegramID int64, message string)
}

// DailyCompletion 记录一条完成的订阅。
type DailyCompletion struct {
	TelegramID  int64
	Title       string
	Year        int
	MediaType   string
	CompletedAt time.Time
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
	service.dailySummaryHour = getDailySummaryHour() // 默认 21:00，可通过 DAILY_SUMMARY_HOUR 环境变量配置
	service.dailySummaryMin = 0

	// Start cleanup routine for old reviews
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("[ReviewService] cleanupRoutine panic: %v", r)
			}
		}()
		service.cleanupRoutine()
	}()
	// 启动每日汇总定时任务
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("[ReviewService] dailySummaryRoutine panic: %v", r)
			}
		}()
		service.dailySummaryRoutine()
	}()

	return service
}

// SetUserMapping 注入用户映射服务（用于 MP 用户名 → Telegram ID 反查）。
func (s *ReviewService) SetUserMapping(um UserMappingStore) {
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

// getDailySummaryHour 从环境变量读取每日汇总时间（小时），默认 21。
func getDailySummaryHour() int {
	h := 21
	if v := os.Getenv("DAILY_SUMMARY_HOUR"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &h); n == 1 && err == nil && h >= 0 && h <= 23 {
			return h
		}
	}
	return 21
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
	s.notifiedMu.Lock()
	defer s.notifiedMu.Unlock()
	for _, id := range ids {
		s.notifiedSubs[id] = true
	}
	logger.Info("[ReviewService] 已加载 %d 条已通知订阅记录", len(ids))
}

// saveNotifiedSubs 持久化已通知的订阅 ID 集合。
func (s *ReviewService) saveNotifiedSubs() {
	s.notifiedMu.RLock()
	ids := make([]int, 0, len(s.notifiedSubs))
	for id := range s.notifiedSubs {
		ids = append(ids, id)
	}
	s.notifiedMu.RUnlock()
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
		s.notifiedMu.RLock()
		alreadyNotified := s.notifiedSubs[sub.ID]
		s.notifiedMu.RUnlock()
		if alreadyNotified {
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
			s.notifiedMu.Lock()
			s.notifiedSubs[sub.ID] = true
			s.notifiedMu.Unlock()
			continue
		}

		// MP 用户名 → Telegram ID
		tgID, found := s.userMapping.GetTelegramIDByMoviePilotUsername(sub.Username)
		if !found || tgID == 0 {
			// 找不到对应用户，标记已通知避免下次重复检查
			logger.Info("[ReviewService] 全量检测：订阅 %d (%s) 用户 %s 未绑定 TG，跳过", sub.ID, sub.Name, sub.Username)
			s.notifiedMu.Lock()
			s.notifiedSubs[sub.ID] = true
			s.notifiedMu.Unlock()
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

		// 记录到每日汇总
		s.recordDailyCompletion(DailyCompletion{
			TelegramID:  tgID,
			Title:       sub.Name,
			Year:        year,
			MediaType:   mediaType,
			CompletedAt: time.Now(),
		})

		// 标记已通知
		s.notifiedMu.Lock()
		s.notifiedSubs[sub.ID] = true
		s.notifiedMu.Unlock()
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
	// 冒险通关不消耗配额；普通/旧入口缺失成本时按媒体类型补齐。
	if review.QuotaCost <= 0 && review.RequestOrigin != "adventure" {
		review.QuotaCost = 1
		if review.MediaType == MediaTypeTV {
			review.QuotaCost = 3
		}
	}

	// Set default priority if not specified
	if review.Priority == "" {
		review.Priority = "normal"
	}

	// Generate approve token - one-time use token to prevent duplicate approvals
	review.ApproveToken = generateApproveToken()

	previous, hadPrevious := s.reviews[review.RequestID]
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

	logger.Info("[审核] 创建请求: %s, 用户: %d, 优先级: %s, 影片: %s",
		review.RequestID, review.TelegramID, priorityText, review.MediaTitle)

	if err := s.saveLocked(); err != nil {
		if hadPrevious {
			s.reviews[review.RequestID] = previous
		} else {
			delete(s.reviews, review.RequestID)
		}
		return err
	}
	return nil
}

// RestoreQuotaOnce 按请求记录的实际成本返还，持久化标记保证重试及重启后幂等。
// 旧 JSON 缺少 quota_cost 时按历史规则推断：电影 1、剧集 3。
func (s *ReviewService) RestoreQuotaOnce(requestID string, quota *QuotaService) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	review, ok := s.reviews[requestID]
	if !ok {
		return false, fmt.Errorf("review request not found: %s", requestID)
	}
	if review.QuotaRestored {
		return false, nil
	}
	cost := review.QuotaCost
	if cost == 0 && review.RequestOrigin == "adventure" {
		review.QuotaRestored = true
		if err := s.saveLocked(); err != nil {
			review.QuotaRestored = false
			return false, err
		}
		return false, nil
	}
	if cost <= 0 {
		cost = 1
		if review.MediaType == MediaTypeTV {
			cost = 3
		}
	}
	if quota == nil {
		return false, fmt.Errorf("quota service not configured")
	}
	if err := quota.RestoreQuotaN(review.TelegramID, string(review.MediaType), cost); err != nil {
		return false, err
	}
	review.QuotaCost = cost
	review.QuotaRestored = true
	if err := s.saveLocked(); err != nil {
		review.QuotaRestored = false
		return false, err
	}
	return true, nil
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

// GetActiveUserIDs returns users who created review requests after the given time.
func (s *ReviewService) GetActiveUserIDs(since time.Time) []int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[int64]bool)
	var users []int64
	for _, review := range s.reviews {
		if review == nil || review.TelegramID == 0 {
			continue
		}
		if !since.IsZero() && review.CreatedAt.Before(since) {
			continue
		}
		if !seen[review.TelegramID] {
			seen[review.TelegramID] = true
			users = append(users, review.TelegramID)
		}
	}
	return users
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

// HasActiveSimilarContent checks active requests across users. Callers that need
// check+create atomicity provide their own transaction lock; no external I/O occurs here.
func (s *ReviewService) HasActiveSimilarContent(tmdbID int, mediaType MediaType, season int) (*ReviewRequest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, review := range s.reviews {
		if review == nil || review.TmdbID != tmdbID || review.MediaType != mediaType {
			continue
		}
		if mediaType == MediaTypeTV && review.Season != season {
			continue
		}
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

func isNonRetryableApproveError(errMsg string) bool {
	errMsg = strings.TrimSpace(errMsg)
	if errMsg == "" {
		return false
	}
	patterns := []string{
		"未获取到第 1 季的总集数",
		"未获取到第1季的总集数",
		"未获取到季的总集数",
		"tmdb id invalid",
		"invalid tmdb",
		"media not found",
	}
	lower := strings.ToLower(errMsg)
	for _, pattern := range patterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

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
	if isNonRetryableApproveError(errMsg) && review.RetryCount < MaxApproveRetry {
		review.RetryCount = MaxApproveRetry
	}

	logger.Info("[ReviewService] 请求提交 MP 失败进入 stuck 兜底: %s, 第 %d 次, err=%s",
		requestID, review.RetryCount, errMsg)

	// B4: 告警 — 首次或最终失败时通知管理员，避免每 5 分钟重复刷屏
	if s.Alert != nil && (review.RetryCount == 1 || review.RetryCount >= MaxApproveRetry) {
		go s.Alert(requestID, review.MediaTitle, review.RetryCount, errMsg)
	}

	return s.saveLocked()
}

// MarkLibraryNotifiedOnce 标记 Emby 入库私聊已发送，返回 true 表示本次应发送。
func (s *ReviewService) MarkLibraryNotifiedOnce(requestID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	review, exists := s.reviews[requestID]
	if !exists || review == nil {
		return false
	}
	if review.LibraryNotifiedAt != nil && !review.LibraryNotifiedAt.IsZero() {
		return false
	}
	now := time.Now()
	review.LibraryNotifiedAt = &now
	if err := s.saveLocked(); err != nil {
		logger.Info("[ReviewService] 保存入库通知标记失败: %v", err)
	}
	return true
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
	if review.Status != "pending" {
		return nil, fmt.Errorf("请求状态为 %s, 无法拒绝", review.Status)
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
			// 自动重试 stuck 请求
			s.retryStuckRequests()
		}()
	}
}

// retryStuckRequests 自动重试 stuck 的请求（审核通过但提交 MP 失败）。
// 每 5 分钟由 refreshSubscriptionStatus 调用，最多重试 MaxApproveRetry 次。
func (s *ReviewService) retryStuckRequests() {
	if s.moviepilot == nil {
		return
	}

	stuck := s.GetStuckRequests()
	if len(stuck) == 0 {
		return
	}

	for _, rv := range stuck {
		if rv.RetryCount >= MaxApproveRetry {
			continue
		}
		if isNonRetryableApproveError(rv.LastError) {
			logger.Info("[ReviewService] 跳过不可重试 stuck 请求: %s, err=%s", rv.RequestID, rv.LastError)
			_ = s.MarkStuck(rv.RequestID, rv.LastError)
			continue
		}

		logger.Info("[ReviewService] 自动重试 stuck 请求: %s, 第 %d 次, 片名: %s",
			rv.RequestID, rv.RetryCount+1, rv.MediaTitle)

		mpMediaType := MediaTypeMovie
		if rv.MediaType == MediaTypeTV {
			mpMediaType = MediaTypeTV
		}

		season := rv.Season
		if season == 0 && rv.MediaType == MediaTypeTV {
			season = 1
		}

		req, err := s.moviepilot.RequestMedia(
			rv.MediaTitle,
			rv.MediaYear,
			rv.TmdbID,
			mpMediaType,
			season,
		)
		if err != nil {
			logger.Info("[ReviewService] 自动重试失败: %s, err: %v", rv.RequestID, err)
			if merr := s.MarkStuck(rv.RequestID, err.Error()); merr != nil {
				logger.Info("[ReviewService] 自动重试失败后 MarkStuck 失败: %v", merr)
			}
			continue
		}

		// 成功：清除 stuck 状态 + 更新订阅信息
		if cerr := s.ClearStuck(rv.RequestID); cerr != nil {
			logger.Info("[ReviewService] ClearStuck 失败: %v", cerr)
		}
		if uerr := s.UpdateSubscriptionInfo(rv.RequestID, req.ID, "N"); uerr != nil {
			logger.Info("[ReviewService] UpdateSubscriptionInfo 失败: %v", uerr)
		}

		logger.Info("[ReviewService] 自动重试成功: %s, 新订阅 ID: %d", rv.RequestID, req.ID)

		// 通知用户
		if s.Alert != nil {
			go s.Alert(rv.RequestID, rv.MediaTitle, rv.RetryCount, "自动重试成功")
		}
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
		if review.Status == "approved" && review.SubscriptionID > 0 && review.SubscriptionState != "X" {
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

					// If subscription is more than 7 days old, cancel silently
					if time.Since(review.CreatedAt) > 7*24*time.Hour {
						review.SubscriptionState = "X"
						logger.Info("[ReviewService] Subscription %d not found in MP (age > 7 days), silently cancelled: %s (was: %s)",
							item.subID, item.requestID, oldState)
					} else if review.OrphanRetryCount >= 3 {
						// Retried 3 times, give up and mark as cancelled
						review.SubscriptionState = "X"
						logger.Info("[ReviewService] Subscription %d not found in MP (retry %d exhausted), marked as cancelled: %s (was: %s)",
							item.subID, review.OrphanRetryCount, item.requestID, oldState)
					} else {
						// Younger than 7 days, retry - bump counter and skip cancellation
						review.OrphanRetryCount++
						logger.Info("[ReviewService] Subscription %d not found in MP (retry %d/3): %s (was: %s)",
							item.subID, review.OrphanRetryCount, item.requestID, oldState)
					}
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

// ── 每日汇总 ──

// recordDailyCompletion 记录一条完成的订阅到今日汇总。
func (s *ReviewService) recordDailyCompletion(item DailyCompletion) {
	s.dailyMu.Lock()
	s.dailyCompletions = append(s.dailyCompletions, item)
	s.dailyMu.Unlock()
}

// dailySummaryRoutine 每天定时发送汇总（默认 21:00）。
func (s *ReviewService) dailySummaryRoutine() {
	for {
		now := time.Now()
		// 计算下次 21:00
		next := time.Date(now.Year(), now.Month(), now.Day(), s.dailySummaryHour, s.dailySummaryMin, 0, 0, now.Location())
		if now.After(next) {
			next = next.Add(24 * time.Hour)
		}
		timer := time.NewTimer(next.Sub(now))
		<-timer.C

		s.sendDailySummary()
	}
}

// sendDailySummary 发送每日汇总并清空当日记录。
func (s *ReviewService) sendDailySummary() {
	s.dailyMu.Lock()
	completions := s.dailyCompletions
	s.dailyCompletions = nil
	s.dailyMu.Unlock()

	if len(completions) == 0 {
		logger.Info("[ReviewService] 今日无新完成订阅，跳过汇总")
		return
	}

	if s.OnDailySummary == nil {
		return
	}

	// 按用户分组
	type userSummary struct {
		telegramID int64
		items      []DailyCompletion
	}
	userMap := make(map[int64]*userSummary)
	for _, c := range completions {
		if _, ok := userMap[c.TelegramID]; !ok {
			userMap[c.TelegramID] = &userSummary{telegramID: c.TelegramID}
		}
		userMap[c.TelegramID].items = append(userMap[c.TelegramID].items, c)
	}

	// 给每个用户发汇总
	today := time.Now().Format("01-02")
	for _, us := range userMap {
		msg := fmt.Sprintf("📊 %s 影片入库汇总\n\n", today)
		for _, item := range us.items {
			icon := "🎬"
			if item.MediaType == "tv" {
				icon = "📺"
			}
			yearStr := ""
			if item.Year > 0 {
				yearStr = fmt.Sprintf(" (%d)", item.Year)
			}
			msg += fmt.Sprintf("%s %s%s\n", icon, item.Title, yearStr)
		}
		msg += fmt.Sprintf("\n共 %d 部，快去 Emby 开刷吧～", len(us.items))

		go s.OnDailySummary(us.telegramID, msg)
	}

	logger.Info("[ReviewService] 每日汇总已发送：通知 %d 位用户，共 %d 部", len(userMap), len(completions))
}

// RequestStats summarizes review request health.
type RequestStats struct {
	Total            int
	Pending          int
	Approved         int
	Rejected         int
	Cancelled        int
	Completed        int
	Failed           int
	Stuck            int
	UniqueUsers      int
	AverageDoneHours int
}

// GetRequestStats returns high-level request funnel stats.
func (s *ReviewService) GetRequestStats() RequestStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := RequestStats{}
	seenUsers := make(map[int64]bool)
	totalDoneHours := 0
	doneCount := 0

	for _, review := range s.reviews {
		if review == nil {
			continue
		}
		stats.Total++
		seenUsers[review.TelegramID] = true
		switch review.Status {
		case "pending":
			stats.Pending++
		case "approved":
			stats.Approved++
		case "rejected":
			stats.Rejected++
		case "cancelled":
			stats.Cancelled++
		}
		if review.Stuck {
			stats.Stuck++
		}
		switch review.SubscriptionState {
		case StateCompleted:
			stats.Completed++
			if !review.CreatedAt.IsZero() && !review.ReviewedAt.IsZero() {
				hours := int(review.ReviewedAt.Sub(review.CreatedAt).Hours())
				if hours >= 0 {
					totalDoneHours += hours
					doneCount++
				}
			}
		case StateFailed, StateCancelled:
			stats.Failed++
		}
	}
	stats.UniqueUsers = len(seenUsers)
	if doneCount > 0 {
		stats.AverageDoneHours = totalDoneHours / doneCount
	}
	return stats
}

// FindActiveSimilarRequest finds any active request for the same media across all users.
func (s *ReviewService) FindActiveSimilarRequest(tmdbID int, mediaType MediaType, season int) (*ReviewRequest, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var first *ReviewRequest
	seenUsers := make(map[int64]bool)
	for _, review := range s.reviews {
		if review == nil || review.TmdbID != tmdbID || review.MediaType != mediaType {
			continue
		}
		if mediaType == MediaTypeTV && review.Season != season {
			continue
		}
		if review.Status != "pending" && review.Status != "approved" {
			continue
		}
		if first == nil || review.CreatedAt.Before(first.CreatedAt) {
			first = review
		}
		if review.TelegramID != 0 {
			seenUsers[review.TelegramID] = true
		}
	}
	return first, len(seenUsers)
}

// GetRequestUserCount returns the number of unique users who made requests
func (s *ReviewService) GetRequestUserCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := make(map[int64]bool)
	for _, r := range s.reviews {
		seen[r.TelegramID] = true
	}
	return len(seen)
}

// GetAllRequests returns all review requests
func (s *ReviewService) GetAllRequests() []*ReviewRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*ReviewRequest, 0, len(s.reviews))
	for _, r := range s.reviews {
		result = append(result, r)
	}
	return result
}
