package services

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/xzb177/yimao/pkg/logger"
	"math/big"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// ReviewRequest represents a media request awaiting review
type ReviewRequest struct {
	BusinessType    string            `json:"business_type,omitempty"` // request (legacy empty) | wash
	RequestID       string            `json:"request_id"`              // Unique ID for this review
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
	SubscriptionID              int        `json:"subscription_id,omitempty"`                // MoviePilot subscription ID
	SubscriptionState           string     `json:"subscription_state,omitempty"`             // N, R, S, D, C, F, X
	LastResubscribeAt           time.Time  `json:"last_resubscribe_at,omitempty"`            // 上次自动重订阅时间
	PendingDeleteSubscriptionID int        `json:"pending_delete_subscription_id,omitempty"` // 新订阅落盘后待清理的旧订阅
	LibraryNotifiedAt           *time.Time `json:"library_notified_at,omitempty"`            // Emby 入库后已私聊通知时间，防重复通知

	// 中间态通知去重标记（P1 体验：等待期不再是黑箱）。
	DownloadNotified bool `json:"download_notified,omitempty"` // 「开始下载」已通知
	StallNotified    bool `json:"stall_notified,omitempty"`    // 「暂未找到资源」已通知
	// CompletedNoticeAt 完成通知发出时间；入库回访（看完了吗）以此为基准。
	CompletedNoticeAt *time.Time `json:"completed_notice_at,omitempty"`
	// WatchFollowupSent 入库回访已发送（每请求一次）。
	WatchFollowupSent bool `json:"watch_followup_sent,omitempty"`

	// 审核通过后向 MoviePilot 提交订阅的兜底状态。
	// 当 Status=="approved" 但提交 MP 失败时，进入 stuck 兜底（而不是凭空消失），
	// 让管理员可见、可手动重试，用户在「求片进度」也能看到「同步中/重试」。
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

	// WashBaseline captures the exact Emby media-source paths present when a
	// wash work order is created. Legacy work orders intentionally have no
	// baseline and therefore cannot be marked complete without recovery.
	WashBaseline   []string   `json:"wash_baseline,omitempty"`
	WashClaimedBy  int64      `json:"wash_claimed_by,omitempty"`
	WashClaimedAt  *time.Time `json:"wash_claimed_at,omitempty"`
	WashLastError  string     `json:"wash_last_error,omitempty"`
	WashVerifiedAt *time.Time `json:"wash_verified_at,omitempty"`
}

const (
	BusinessTypeRequest = "request"
	BusinessTypeWash    = "wash"
)

// NormalizedBusinessType treats legacy records without business_type as requests.
func (r *ReviewRequest) NormalizedBusinessType() string {
	if r != nil && r.BusinessType == BusinessTypeWash {
		return BusinessTypeWash
	}
	return BusinessTypeRequest
}

func normalizeBusinessType(value string) string {
	if value == BusinessTypeWash {
		return BusinessTypeWash
	}
	return BusinessTypeRequest
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

	// OnDownloadStart 订阅从等待/搜索转入下载中（state → "D"）时的用户通知回调
	// （由 main 注入）。「已通过审核」到「已入库」之间可能隔数小时到数天，
	// 这个中间态推送让用户不必反复戳「求片进度」。每个请求只触发一次。
	OnDownloadStart func(telegramID int64, title string, year int, mediaType string)

	// OnSearchStall 订阅进入回收/长时间搜索（state → "R"）时的用户预期管理回调
	// （由 main 注入）。告知「暂时没找到资源，已转入持续搜索」。每个请求只触发一次。
	OnSearchStall func(telegramID int64, title string, year int, mediaType string)

	// Fulfillment 履约统计服务（可选注入）：完成时抽样记录 提交→完成 耗时，
	// 供求片回执展示 ETA 参考；同时驱动入库回访。
	Fulfillment *FulfillmentStatsService

	// OnWatchFollowup 入库回访回调（由 main 注入）：完成通知发出 3 天后
	// 问一句「看了吗」。返回 true 表示已发送或用户已关闭通知，可标记完成；
	// 返回 false 表示传输失败，下个小时自动重试。
	OnWatchFollowup func(telegramID int64, requestID, title string) bool

	// OnFulfillmentComplete 完成时写入长期账本（由 main 注入）。
	OnFulfillmentComplete func(requestID string, telegramID int64, title string, year int, mediaType string, completedAt time.Time)

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
	if review.QuotaCost <= 0 && review.RequestOrigin != "adventure" && review.NormalizedBusinessType() == BusinessTypeRequest {
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
	s.reviews[review.RequestID] = cloneReview(review)

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

// CreateRequestIfNoActiveSimilar atomically checks the business duplicate key
// and creates the request. This prevents concurrent Telegram callbacks from
// notifying administrators twice for the same work order.
func (s *ReviewService) CreateRequestIfNoActiveSimilar(review *ReviewRequest) (*ReviewRequest, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, current := range s.reviews {
		if current == nil || current.TmdbID != review.TmdbID || current.MediaType != review.MediaType || current.NormalizedBusinessType() != review.NormalizedBusinessType() {
			continue
		}
		if review.MediaType == MediaTypeTV && current.Season != review.Season {
			continue
		}
		if current.Status == "pending" || current.Status == "approved" {
			return cloneReview(current), false, nil
		}
	}
	review.CreatedAt = time.Now()
	review.Status = "pending"
	if review.Priority == "" {
		review.Priority = "normal"
	}
	review.ApproveToken = generateApproveToken()
	previous, existed := s.reviews[review.RequestID]
	s.reviews[review.RequestID] = cloneReview(review)
	if err := s.saveLocked(); err != nil {
		if existed {
			s.reviews[review.RequestID] = previous
		} else {
			delete(s.reviews, review.RequestID)
		}
		return nil, false, err
	}
	return cloneReview(review), true, nil
}

// CompleteWash closes an approved wash work order only after verifying that
// every baseline source is still present and at least one different source was
// added. A completed work order is returned unchanged to make retries safe.
func (s *ReviewService) CompleteWash(requestID string, adminID int64, currentSources []string) (*ReviewRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	review, ok := s.reviews[requestID]
	if !ok || review == nil {
		return nil, fmt.Errorf("review request not found")
	}
	if review.NormalizedBusinessType() != BusinessTypeWash {
		return nil, fmt.Errorf("wash work order is not completable")
	}
	if review.Status == "completed" {
		return cloneReview(review), nil
	}
	if review.Status != "claimed" {
		return nil, fmt.Errorf("洗版工单必须先认领后才能完成")
	}
	if review.WashClaimedBy != adminID {
		return nil, fmt.Errorf("洗版工单已由其他管理员认领")
	}
	if len(review.WashBaseline) == 0 {
		review.WashLastError = "缺少创建时基线：旧工单不能自动验证；请重新创建洗版工单以采集基线"
		_ = s.saveLocked()
		return nil, fmt.Errorf("%s", review.WashLastError)
	}
	current := make(map[string]struct{}, len(currentSources))
	for _, source := range currentSources {
		if source = strings.TrimSpace(source); source != "" {
			current[source] = struct{}{}
		}
	}
	baseline := make(map[string]struct{}, len(review.WashBaseline))
	for _, source := range review.WashBaseline {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		baseline[source] = struct{}{}
		if _, preserved := current[source]; !preserved {
			review.WashLastError = fmt.Sprintf("旧版未保留：缺少基线 MediaSource %q", source)
			_ = s.saveLocked()
			return nil, fmt.Errorf("%s", review.WashLastError)
		}
	}
	if len(baseline) == 0 {
		return nil, fmt.Errorf("缺少创建时基线：请重新创建洗版工单以采集基线")
	}
	newSource := false
	for source := range current {
		if _, existed := baseline[source]; !existed {
			newSource = true
			break
		}
	}
	if !newSource {
		review.WashLastError = "未发现新增的不同 MediaSource"
		_ = s.saveLocked()
		return nil, fmt.Errorf("%s", review.WashLastError)
	}

	now := time.Now()
	previousStatus, previousBy, previousAt := review.Status, review.ReviewedBy, review.ReviewedAt
	previousError, previousVerified := review.WashLastError, review.WashVerifiedAt
	review.Status, review.ReviewedBy, review.ReviewedAt = "completed", adminID, now
	review.WashLastError = ""
	review.WashVerifiedAt = &now
	if err := s.saveLocked(); err != nil {
		review.Status, review.ReviewedBy, review.ReviewedAt = previousStatus, previousBy, previousAt
		review.WashLastError, review.WashVerifiedAt = previousError, previousVerified
		return nil, err
	}
	return cloneReview(review), nil
}

// RecordWashVerificationFailure persists the latest verification failure without
// changing the workflow state, so the work order remains retryable and auditable.
func (s *ReviewService) RecordWashVerificationFailure(requestID string, adminID int64, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	review, ok := s.reviews[requestID]
	if !ok || review == nil || review.NormalizedBusinessType() != BusinessTypeWash {
		return fmt.Errorf("wash work order not found")
	}
	if review.Status != "claimed" || review.WashClaimedBy != adminID {
		return fmt.Errorf("只能记录自己认领的洗版工单")
	}
	previous := review.WashLastError
	review.WashLastError = strings.TrimSpace(reason)
	if err := s.saveLocked(); err != nil {
		review.WashLastError = previous
		return err
	}
	return nil
}

// ClaimWash atomically assigns an approved wash work order to one administrator.
func (s *ReviewService) ClaimWash(requestID string, adminID int64) (*ReviewRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	review, ok := s.reviews[requestID]
	if !ok || review == nil || review.NormalizedBusinessType() != BusinessTypeWash {
		return nil, fmt.Errorf("wash work order not found")
	}
	if review.Status == "claimed" {
		if review.WashClaimedBy == adminID {
			return cloneReview(review), nil
		}
		return nil, fmt.Errorf("洗版工单已由其他管理员认领")
	}
	if review.Status != "approved" {
		return nil, fmt.Errorf("当前状态为 %s，不能认领", review.Status)
	}
	now := time.Now()
	previousStatus, previousBy, previousAt := review.Status, review.WashClaimedBy, review.WashClaimedAt
	review.Status, review.WashClaimedBy, review.WashClaimedAt = "claimed", adminID, &now
	if err := s.saveLocked(); err != nil {
		review.Status, review.WashClaimedBy, review.WashClaimedAt = previousStatus, previousBy, previousAt
		return nil, err
	}
	return cloneReview(review), nil
}

// ReleaseWash returns a claimed wash work order to the approved queue.
func (s *ReviewService) ReleaseWash(requestID string, adminID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	review, ok := s.reviews[requestID]
	if !ok || review == nil || review.NormalizedBusinessType() != BusinessTypeWash {
		return fmt.Errorf("wash work order not found")
	}
	if review.Status != "claimed" || review.WashClaimedBy != adminID {
		return fmt.Errorf("只能释放自己认领的洗版工单")
	}
	previousStatus, previousBy, previousAt := review.Status, review.WashClaimedBy, review.WashClaimedAt
	review.Status, review.WashClaimedBy, review.WashClaimedAt = "approved", 0, nil
	if err := s.saveLocked(); err != nil {
		review.Status, review.WashClaimedBy, review.WashClaimedAt = previousStatus, previousBy, previousAt
		return err
	}
	return nil
}

// GetWashRequests returns all non-terminal wash work orders, newest first.
func (s *ReviewService) GetWashRequests() []*ReviewRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*ReviewRequest, 0)
	for _, review := range s.reviews {
		if review != nil && review.NormalizedBusinessType() == BusinessTypeWash && review.Status != "completed" && review.Status != "rejected" && review.Status != "cancelled" {
			items = append(items, cloneReview(review))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items
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
	// Work orders such as wash never consume request quota. Marking the no-op
	// persistently keeps retries idempotent and prevents legacy fallback costs.
	if review.NormalizedBusinessType() != BusinessTypeRequest {
		review.QuotaRestored = true
		if err := s.saveLocked(); err != nil {
			review.QuotaRestored = false
			return false, err
		}
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
	_, err := quota.RestoreQuotaForRequest(requestID, review.TelegramID, string(review.MediaType), cost)
	if err != nil {
		return false, err
	}
	review.QuotaCost = cost
	review.QuotaRestored = true
	if err := s.saveLocked(); err != nil {
		review.QuotaRestored = false
		return false, err
	}
	// If the quota ledger already contained this request, this call reconciled
	// the Review marker after an earlier partial failure. The user-facing result
	// is still "quota restored".
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
	return cloneReview(review), exists
}

// GetRequestByToken retrieves a review request by approve token
// Searches all requests (not just pending) to handle duplicate approval attempts
func (s *ReviewService) GetRequestByToken(token string) (*ReviewRequest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, review := range s.reviews {
		if review.ApproveToken == token {
			return cloneReview(review), true
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
			pending = append(pending, cloneReview(review))
		}
	}

	// Sort by created time desc (newer first)
	sort.Slice(pending, func(i, j int) bool { return pending[i].CreatedAt.After(pending[j].CreatedAt) })

	return pending
}

// GetApprovedWashRequests returns active wash work orders which administrators
// can reopen after the original approval message is no longer available.
func (s *ReviewService) GetApprovedWashRequests() []*ReviewRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var approved []*ReviewRequest
	for _, review := range s.reviews {
		if review != nil && review.NormalizedBusinessType() == BusinessTypeWash && review.Status == "approved" {
			approved = append(approved, cloneReview(review))
		}
	}
	sort.Slice(approved, func(i, j int) bool { return approved[i].ReviewedAt.After(approved[j].ReviewedAt) })
	return approved
}

// GetUserRequests returns all review requests for a user
func (s *ReviewService) GetUserRequests(telegramID int64) []*ReviewRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var userReviews []*ReviewRequest
	for _, review := range s.reviews {
		if review.TelegramID == telegramID {
			userReviews = append(userReviews, cloneReview(review))
		}
	}

	// Sort by created time desc
	sort.Slice(userReviews, func(i, j int) bool { return userReviews[i].CreatedAt.After(userReviews[j].CreatedAt) })

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
	return s.HasActiveSimilarRequestForBusiness(telegramID, tmdbID, mediaType, season, BusinessTypeRequest)
}

// HasActiveSimilarRequestForBusiness includes business type in the duplicate key.
func (s *ReviewService) HasActiveSimilarRequestForBusiness(telegramID int64, tmdbID int, mediaType MediaType, season int, businessType string) (*ReviewRequest, bool) {
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
		if review.NormalizedBusinessType() != normalizeBusinessType(businessType) {
			continue
		}

		// TV requests should match season; movie season is always ignored
		if mediaType == MediaTypeTV && review.Season != season {
			continue
		}

		// Active statuses considered duplicate to prevent repeated submissions
		if review.Status == "pending" || review.Status == "approved" {
			return cloneReview(review), true
		}
	}

	return nil, false
}

// HasActiveSimilarContent checks active requests across users. Callers that need
// check+create atomicity provide their own transaction lock; no external I/O occurs here.
func (s *ReviewService) HasActiveSimilarContent(tmdbID int, mediaType MediaType, season int) (*ReviewRequest, bool) {
	return s.HasActiveSimilarContentForBusiness(tmdbID, mediaType, season, BusinessTypeRequest)
}

// HasActiveSimilarContentForBusiness includes business type in the duplicate key.
func (s *ReviewService) HasActiveSimilarContentForBusiness(tmdbID int, mediaType MediaType, season int, businessType string) (*ReviewRequest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, review := range s.reviews {
		if review == nil || review.TmdbID != tmdbID || review.MediaType != mediaType {
			continue
		}
		if review.NormalizedBusinessType() != normalizeBusinessType(businessType) {
			continue
		}
		if mediaType == MediaTypeTV && review.Season != season {
			continue
		}
		if review.Status == "pending" || review.Status == "approved" {
			return cloneReview(review), true
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
			return cloneReview(review), fmt.Errorf("already_approved")
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
	previousStatus, previousReviewedAt, previousReviewedBy := review.Status, review.ReviewedAt, review.ReviewedBy
	review.Status = "approved"
	review.ReviewedAt = time.Now()
	review.ReviewedBy = reviewedBy

	logger.Info("[ReviewService] Approved review request: %s by admin: %d", requestID, reviewedBy)

	if err := s.saveLocked(); err != nil {
		review.Status, review.ReviewedAt, review.ReviewedBy = previousStatus, previousReviewedAt, previousReviewedBy
		return nil, err
	}
	return cloneReview(review), nil
}

// RequeueApprovedPreflightFailure returns an approved request to the review
// queue when a transient preflight dependency cannot confirm safety. It never
// changes quota accounting and only accepts records without an MP subscription.
func (s *ReviewService) RequeueApprovedPreflightFailure(requestID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	review, exists := s.reviews[requestID]
	if !exists {
		return fmt.Errorf("review request not found: %s", requestID)
	}
	if review.Status != "approved" || review.SubscriptionID != 0 {
		return fmt.Errorf("request is not requeueable: status=%s subscription=%d", review.Status, review.SubscriptionID)
	}
	previousStatus, previousAt, previousBy, previousReason := review.Status, review.ReviewedAt, review.ReviewedBy, review.RejectionReason
	review.Status = "pending"
	review.ReviewedAt = time.Time{}
	review.ReviewedBy = 0
	review.RejectionReason = strings.TrimSpace(reason)
	if err := s.saveLocked(); err != nil {
		review.Status, review.ReviewedAt, review.ReviewedBy, review.RejectionReason = previousStatus, previousAt, previousBy, previousReason
		return err
	}
	return nil
}

// RejectApprovedPreflight blocks an approved request that a final authoritative
// preflight proves is already fulfilled. The transition remains auditable and
// is limited to records that never created an MP subscription.
func (s *ReviewService) RejectApprovedPreflight(requestID string, reviewedBy int64, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	review, exists := s.reviews[requestID]
	if !exists {
		return fmt.Errorf("review request not found: %s", requestID)
	}
	if review.Status != "approved" || review.SubscriptionID != 0 {
		return fmt.Errorf("request is not preflight-rejectable: status=%s subscription=%d", review.Status, review.SubscriptionID)
	}
	previousStatus, previousAt, previousBy, previousReason := review.Status, review.ReviewedAt, review.ReviewedBy, review.RejectionReason
	review.Status = "rejected"
	review.ReviewedAt = time.Now()
	review.ReviewedBy = reviewedBy
	review.RejectionReason = strings.TrimSpace(reason)
	if err := s.saveLocked(); err != nil {
		review.Status, review.ReviewedAt, review.ReviewedBy, review.RejectionReason = previousStatus, previousAt, previousBy, previousReason
		return err
	}
	return nil
}

// UpdateSubscriptionInfo updates the MoviePilot subscription info for a review.
// Persistence failure restores the previous in-memory state.
func (s *ReviewService) UpdateSubscriptionInfo(requestID string, subscriptionID int, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	review, exists := s.reviews[requestID]
	if !exists {
		return fmt.Errorf("review request not found: %s", requestID)
	}

	previousID, previousState := review.SubscriptionID, review.SubscriptionState
	review.SubscriptionID = subscriptionID
	review.SubscriptionState = state
	if err := s.saveLocked(); err != nil {
		review.SubscriptionID, review.SubscriptionState = previousID, previousState
		return err
	}

	logger.Info("[ReviewService] Updated subscription info for %s: ID=%d, State=%s", requestID, subscriptionID, state)
	return nil
}

// LinkSubscription atomically persists a newly created MoviePilot subscription
// and clears any stuck marker. On failure, all in-memory fields are restored.
func (s *ReviewService) LinkSubscription(requestID string, subscriptionID int, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	review, exists := s.reviews[requestID]
	if !exists {
		return fmt.Errorf("review request not found: %s", requestID)
	}
	previousID, previousState := review.SubscriptionID, review.SubscriptionState
	previousStuck, previousError := review.Stuck, review.LastError
	review.SubscriptionID = subscriptionID
	review.SubscriptionState = state
	review.Stuck = false
	review.LastError = ""
	if err := s.saveLocked(); err != nil {
		review.SubscriptionID, review.SubscriptionState = previousID, previousState
		review.Stuck, review.LastError = previousStuck, previousError
		return err
	}
	logger.Info("[ReviewService] Linked subscription for %s: ID=%d, State=%s", requestID, subscriptionID, state)
	return nil
}

// MarkStuck 记录「审核已通过但提交 MoviePilot 失败」的兜底状态。
// 不改变 Status（仍为 approved），仅累加 RetryCount + 记录 LastError + 置 Stuck，
// 这样请求不会凭空消失：管理员面板可见、可手动重试，用户在「求片进度」也能看到。
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

	previousRetryCount, previousLastError, previousStuck := review.RetryCount, review.LastError, review.Stuck
	review.RetryCount++
	review.LastError = errMsg
	review.Stuck = true
	if isNonRetryableApproveError(errMsg) && review.RetryCount < MaxApproveRetry {
		review.RetryCount = MaxApproveRetry
	}

	logger.Info("[ReviewService] 请求提交 MP 失败进入 stuck 兜底: %s, 第 %d 次, err=%s",
		requestID, review.RetryCount, errMsg)

	if err := s.saveLocked(); err != nil {
		review.RetryCount, review.LastError, review.Stuck = previousRetryCount, previousLastError, previousStuck
		return err
	}

	// B4: 告警 — 只对已持久化的状态发送，避免写盘失败后误触发终态补偿。
	if s.Alert != nil && (review.RetryCount == 1 || review.RetryCount >= MaxApproveRetry) {
		go s.Alert(requestID, review.MediaTitle, review.RetryCount, errMsg)
	}
	return nil
}

// IsLibraryNotificationPending reports whether the request still needs a delivery attempt.
func (s *ReviewService) IsLibraryNotificationPending(requestID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	review, exists := s.reviews[requestID]
	return exists && review != nil && (review.LibraryNotifiedAt == nil || review.LibraryNotifiedAt.IsZero())
}

// MarkLibraryNotified persists a successful Emby library notification.
func (s *ReviewService) MarkLibraryNotified(requestID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	review, exists := s.reviews[requestID]
	if !exists || review == nil {
		return fmt.Errorf("review request not found: %s", requestID)
	}
	if review.LibraryNotifiedAt != nil && !review.LibraryNotifiedAt.IsZero() {
		return nil
	}
	now := time.Now()
	review.LibraryNotifiedAt = &now
	if err := s.saveLocked(); err != nil {
		review.LibraryNotifiedAt = nil
		return err
	}
	return nil
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
			stuck = append(stuck, cloneReview(review))
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

	previousStatus, previousReviewedAt, previousReviewedBy, previousReason := review.Status, review.ReviewedAt, review.ReviewedBy, review.RejectionReason
	review.Status = "rejected"
	review.ReviewedAt = time.Now()
	review.ReviewedBy = reviewedBy
	review.RejectionReason = reason

	logger.Info("[ReviewService] Rejected review request: %s, reason: %s", requestID, reason)

	if err := s.saveLocked(); err != nil {
		review.Status, review.ReviewedAt, review.ReviewedBy, review.RejectionReason = previousStatus, previousReviewedAt, previousReviewedBy, previousReason
		return nil, err
	}
	return cloneReview(review), nil
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

	previousStatus, previousReason, previousReviewedAt := review.Status, review.RejectionReason, review.ReviewedAt
	review.Status = "cancelled"
	review.RejectionReason = "用户主动撤回"
	review.ReviewedAt = time.Now()

	logger.Info("[ReviewService] 用户撤回请求: %s, 用户: %d, 影片: %s",
		requestID, telegramID, review.MediaTitle)

	if err := s.saveLocked(); err != nil {
		review.Status, review.RejectionReason, review.ReviewedAt = previousStatus, previousReason, previousReviewedAt
		return err
	}
	return nil
}

// watchFollowupDelay 完成通知 → 入库回访的间隔。
// 必须明显小于 cleanup 的 7 天保留期，否则单据先被清走。
const watchFollowupDelay = 3 * 24 * time.Hour

// sendWatchFollowups 给完成已满 3 天且未回访过的请求发「看了吗」回访。
func (s *ReviewService) sendWatchFollowups() {
	if s.OnWatchFollowup == nil {
		return
	}
	type followup struct {
		telegramID int64
		requestID  string
		title      string
	}
	var due []followup

	s.mu.RLock()
	for id, review := range s.reviews {
		if review.WatchFollowupSent || review.CompletedNoticeAt == nil || review.CompletedNoticeAt.IsZero() {
			continue
		}
		if time.Since(*review.CompletedNoticeAt) < watchFollowupDelay {
			continue
		}
		due = append(due, followup{review.TelegramID, id, review.MediaTitle})
	}
	s.mu.RUnlock()

	for _, f := range due {
		func(fu followup) {
			defer func() {
				if r := recover(); r != nil {
					logger.Info("[ReviewService] Panic in watch followup: %v", r)
				}
			}()
			if !s.OnWatchFollowup(fu.telegramID, fu.requestID, fu.title) {
				return
			}
			s.mu.Lock()
			if review, ok := s.reviews[fu.requestID]; ok && review != nil && !review.WatchFollowupSent {
				review.WatchFollowupSent = true
				if err := s.saveLocked(); err != nil {
					logger.Info("[ReviewService] 保存回访标记失败: %v", err)
				}
			}
			s.mu.Unlock()
		}(f)
	}
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
			s.sendWatchFollowups()
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
		// 已完成单据至少保留完成后 14 天：第 3 天发回访，给用户留足回答窗口。
		// 否则旧逻辑按 ReviewedAt 7 天清理，审批较早的单据可能在回访前消失。
		if review.CompletedNoticeAt != nil && !review.CompletedNoticeAt.IsZero() {
			if time.Since(*review.CompletedNoticeAt) < 14*24*time.Hour {
				continue
			}
			toDelete = append(toDelete, id)
			continue
		}
		// Approved wash work orders remain active until explicitly completed;
		// deleting them here would remove the only safe recovery path.
		if review.NormalizedBusinessType() == BusinessTypeWash && review.Status == "approved" {
			continue
		}
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

const subscriptionRecoveryGracePeriod = 6 * time.Minute

// getSubscriptionRecoveryCandidates includes persisted stuck requests and
// approved requests whose MoviePilot link may have been lost after a local
// write failure. Wash work orders never create MoviePilot subscriptions.
func (s *ReviewService) getSubscriptionRecoveryCandidates() []*ReviewRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	var candidates []*ReviewRequest
	for _, review := range s.reviews {
		if review.Status != "approved" || review.SubscriptionID > 0 || review.NormalizedBusinessType() == BusinessTypeWash {
			continue
		}
		if review.Stuck || (review.LastError == "" && !review.ReviewedAt.IsZero() && now.Sub(review.ReviewedAt) >= subscriptionRecoveryGracePeriod) {
			candidates = append(candidates, cloneReview(review))
		}
	}
	return candidates
}

// retryStuckRequests automatically recovers approved requests without a
// persisted MoviePilot link. This also covers a restart after both the link
// write and the follow-up stuck marker failed on the same broken filesystem.
// It runs every five minutes and stops after MaxApproveRetry attempts.
func (s *ReviewService) retryStuckRequests() {
	if s.moviepilot == nil {
		return
	}

	stuck := s.getSubscriptionRecoveryCandidates()
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

		// A previous create may have succeeded while the local link write failed.
		// Recover that real subscription before creating another one.
		if existing, found, findErr := s.moviepilot.FindExistingSubscription(rv.TmdbID, mpMediaType, season); findErr == nil && found {
			if linkErr := s.LinkSubscription(rv.RequestID, existing.ID, existing.State); linkErr != nil {
				logger.Info("[ReviewService] 恢复已有订阅关联失败: %s, sub=%d, err=%v", rv.RequestID, existing.ID, linkErr)
				continue
			}
			logger.Info("[ReviewService] 已恢复已有订阅关联: %s, sub=%d", rv.RequestID, existing.ID)
			continue
		} else if findErr != nil {
			logger.Info("[ReviewService] 查询已有订阅失败，暂缓新建: %s, err=%v", rv.RequestID, findErr)
			continue
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

		// 成功：原子写入订阅关联并清除 stuck 状态
		if uerr := s.LinkSubscription(rv.RequestID, req.ID, "N"); uerr != nil {
			logger.Info("[ReviewService] LinkSubscription 失败: %v", uerr)
			_ = s.MarkStuck(rv.RequestID, fmt.Sprintf("MoviePilot 订阅 %d 已创建，但本地关联失败: %v", req.ID, uerr))
			continue
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
		s.cleanupReplacedSubscriptions()
		return
	}

	// Finish any replacement cleanup left by an API error or process exit.
	s.cleanupReplacedSubscriptions()
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

					// 完成跃迁：统计/回访账本不应依赖用户是否开启通知。
					if actualState == "C" && oldState != "C" {
						// 履约样本：提交 → 完成 的耗时（驱动求片回执里的 ETA 参考）。
						if s.Fulfillment != nil && !review.CreatedAt.IsZero() {
							s.Fulfillment.AddSample(string(review.MediaType), review.MediaYear,
								int64(time.Since(review.CreatedAt).Seconds()))
						}
						// 记录完成时间，为入库回访与长期冷片盘点提供基准。
						now := time.Now()
						review.CompletedNoticeAt = &now
						if s.OnFulfillmentComplete != nil {
							s.OnFulfillmentComplete(review.RequestID, review.TelegramID, review.MediaTitle, review.MediaYear, string(review.MediaType), now)
						}
						if s.OnSubscriptionComplete != nil {
							go func(r *ReviewRequest) {
								defer func() {
									if rec := recover(); rec != nil {
										logger.Info("[ReviewService] Panic in completion notification: %v", rec)
									}
								}()
								s.OnSubscriptionComplete(r.TelegramID, r.MediaTitle, r.MediaYear, string(r.MediaType))
							}(review)
						}
					}

					// P1 中间态：开始下载（每请求一次）。完成态跳过（避免 C 前一刻的抖动）。
					if actualState == "D" && !review.DownloadNotified && s.OnDownloadStart != nil {
						review.DownloadNotified = true
						go func(r *ReviewRequest) {
							defer func() {
								if rec := recover(); rec != nil {
									logger.Info("[ReviewService] Panic in download-start notification: %v", rec)
								}
							}()
							s.OnDownloadStart(r.TelegramID, r.MediaTitle, r.MediaYear, string(r.MediaType))
						}(review)
					}

					// P1 中间态：进入回收/持续搜索，做预期管理（每请求一次）。
					if actualState == "R" && oldState != "" && !review.StallNotified && s.OnSearchStall != nil {
						review.StallNotified = true
						go func(r *ReviewRequest) {
							defer func() {
								if rec := recover(); rec != nil {
									logger.Info("[ReviewService] Panic in search-stall notification: %v", rec)
								}
							}()
							s.OnSearchStall(r.TelegramID, r.MediaTitle, r.MediaYear, string(r.MediaType))
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
		stored, exists := s.reviews[requestID]
		var review *ReviewRequest
		if exists {
			review = cloneReview(stored)
		}
		s.mu.RUnlock()

		if !exists || review == nil {
			continue
		}

		if review.SubscriptionID > 0 {
			if processedSubID[review.SubscriptionID] {
				logger.Info("[ReviewService] Skip duplicate resubscribe for same subID=%d, request=%s", review.SubscriptionID, requestID)
				continue
			}
			processedSubID[review.SubscriptionID] = true
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

		// Persist the new subscription and the old-ID cleanup intent atomically.
		s.mu.Lock()
		linked := false
		if r, ok := s.reviews[requestID]; ok {
			previousID, previousState := r.SubscriptionID, r.SubscriptionState
			previousAt := r.LastResubscribeAt
			previousPendingDeleteID := r.PendingDeleteSubscriptionID
			r.SubscriptionID = req.ID
			r.SubscriptionState = "N"
			r.LastResubscribeAt = time.Now()
			if previousID > 0 && previousID != req.ID {
				r.PendingDeleteSubscriptionID = previousID
			}
			if err := s.saveLocked(); err != nil {
				r.SubscriptionID, r.SubscriptionState = previousID, previousState
				r.LastResubscribeAt = previousAt
				r.PendingDeleteSubscriptionID = previousPendingDeleteID
				logger.Info("[ReviewService] Failed to persist resubscription link: %v", err)
			} else {
				linked = true
			}
		}
		s.mu.Unlock()
		if !linked {
			if err := s.moviepilot.DeleteRequest(req.ID); err != nil {
				logger.Info("[ReviewService] Failed to compensate unlinked subscription %d: %v", req.ID, err)
			}
			continue
		}

		if review.SubscriptionID > 0 && review.SubscriptionID != req.ID {
			logger.Info("[ReviewService] Deleting replaced subscription %d for %s", review.SubscriptionID, requestID)
			if err := s.moviepilot.DeleteRequest(review.SubscriptionID); err != nil {
				logger.Info("[ReviewService] Failed to delete replaced subscription: %v", err)
			} else if err := s.clearPendingDeleteSubscription(requestID, review.SubscriptionID); err != nil {
				logger.Info("[ReviewService] Failed to persist replacement cleanup: %v", err)
			}
		}

		logger.Info("[ReviewService] Resubscribed %s: new subscription ID %d", requestID, req.ID)
	}
}

func (s *ReviewService) clearPendingDeleteSubscription(requestID string, subscriptionID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	review, ok := s.reviews[requestID]
	if !ok || review.PendingDeleteSubscriptionID != subscriptionID {
		return nil
	}
	review.PendingDeleteSubscriptionID = 0
	if err := s.saveLocked(); err != nil {
		review.PendingDeleteSubscriptionID = subscriptionID
		return err
	}
	return nil
}

func (s *ReviewService) cleanupReplacedSubscriptions() {
	type pendingCleanup struct {
		requestID      string
		subscriptionID int
	}
	var pending []pendingCleanup
	s.mu.RLock()
	for requestID, review := range s.reviews {
		if review != nil && review.PendingDeleteSubscriptionID > 0 {
			pending = append(pending, pendingCleanup{requestID: requestID, subscriptionID: review.PendingDeleteSubscriptionID})
		}
	}
	s.mu.RUnlock()
	for _, item := range pending {
		if err := s.moviepilot.DeleteRequest(item.subscriptionID); err != nil {
			logger.Info("[ReviewService] Retry deleting replaced subscription %d: %v", item.subscriptionID, err)
			continue
		}
		if err := s.clearPendingDeleteSubscription(item.requestID, item.subscriptionID); err != nil {
			logger.Info("[ReviewService] Failed to clear replacement cleanup marker: %v", err)
		}
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

// GetAllRequests returns detached snapshots of all review requests.
func (s *ReviewService) GetAllRequests() []*ReviewRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*ReviewRequest, 0, len(s.reviews))
	for _, r := range s.reviews {
		if r == nil {
			continue
		}
		result = append(result, cloneReview(r))
	}
	return result
}
