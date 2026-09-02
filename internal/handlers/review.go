package handlers

import (
	"fmt"
	"strings"
	"sync"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/richmessage"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
)

// ReviewHandler handles review request callbacks
type ReviewHandler struct {
	sessMgr        *session.Manager
	telegram       *services.TelegramClient
	moviepilot     *services.MoviePilotClient
	adminService   *services.AdminService
	reviewService  *services.ReviewService
	quotaService   *services.QuotaService
	webhookService *services.WebhookService
	groupChatID    int64 // 群组 ChatID，审批通过时发送群通知；0=不发
	approvalOnce   sync.Once
	approvalLocks  *requestLockTable
	// OnCarpoolNotify 拼车用户通知回调（拒绝/撤回时触发）。
	// 参数：tmdbID, mediaType, title, reason
	OnCarpoolNotify func(tmdbID int, mediaType, title, reason string)
}

type requestLockEntry struct {
	mu   sync.Mutex
	refs int
}

type requestLockTable struct {
	mu      sync.Mutex
	entries map[string]*requestLockEntry
}

func newRequestLockTable() *requestLockTable {
	return &requestLockTable{entries: make(map[string]*requestLockEntry)}
}

func (t *requestLockTable) lock(key string) func() {
	t.mu.Lock()
	entry := t.entries[key]
	if entry == nil {
		entry = &requestLockEntry{}
		t.entries[key] = entry
	}
	entry.refs++
	t.mu.Unlock()

	entry.mu.Lock()
	var once sync.Once
	return func() {
		once.Do(func() {
			entry.mu.Unlock()

			t.mu.Lock()
			entry.refs--
			if entry.refs == 0 && t.entries[key] == entry {
				delete(t.entries, key)
			}
			t.mu.Unlock()
		})
	}
}

func (t *requestLockTable) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.entries)
}

func (t *requestLockTable) references(key string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if entry := t.entries[key]; entry != nil {
		return entry.refs
	}
	return 0
}

func (h *ReviewHandler) requestApprovalLocks() *requestLockTable {
	h.approvalOnce.Do(func() {
		h.approvalLocks = newRequestLockTable()
	})
	return h.approvalLocks
}

func (h *ReviewHandler) lockApprovalRequest(requestID string) func() {
	return h.requestApprovalLocks().lock(requestID)
}

func (h *ReviewHandler) approvalLockCount() int {
	return h.requestApprovalLocks().count()
}

func (h *ReviewHandler) approvalLockReferences(requestID string) int {
	return h.requestApprovalLocks().references(requestID)
}

func NewReviewHandler(
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	moviepilot *services.MoviePilotClient,
	adminService *services.AdminService,
	reviewService *services.ReviewService,
	quotaService *services.QuotaService,
	webhookService *services.WebhookService,
	groupChatID int64,
) *ReviewHandler {
	return &ReviewHandler{
		sessMgr:        sessMgr,
		telegram:       telegram,
		moviepilot:     moviepilot,
		adminService:   adminService,
		reviewService:  reviewService,
		quotaService:   quotaService,
		webhookService: webhookService,
		groupChatID:    groupChatID,
	}
}

func (h *ReviewHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	if ctx == nil || ctx.Callback == nil {
		return nil, nil
	}
	handler := h.handlerFor(ctx.Callback.Action)
	if handler == nil {
		return nil, nil
	}
	return handler(ctx)
}

// Supports reports whether this handler has a concrete dispatch path for the
// action. It shares the exact dispatch table used by Handle so wiring tests can
// verify allowlist -> registry -> handler consistency without invoking services.
func (h *ReviewHandler) Supports(action callback.Action) bool {
	return h.handlerFor(action) != nil
}

func (h *ReviewHandler) handlerFor(action callback.Action) callback.HandlerFunc {
	switch action {
	case "review_approve", "rv_a":
		return h.handleApprove
	case "review_reject", "rv_r":
		return h.handleReject
	case "review_cancel":
		return h.handleCancel
	case "review_complete_wash":
		return h.handleCompleteWash
	case "review_claim_wash":
		return h.handleClaimWash
	case "review_release_wash":
		return h.handleReleaseWash
	case "review_reopen_wash":
		return h.handleReopenWash
	case "review_retry_wash":
		return h.handleRetryWash
	case "review_detail_wash":
		return h.handleWashDetail
	case "my_reviews":
		return h.handleMyReviews
	case "review_list":
		return h.handleReviewList
	default:
		return nil
	}
}

func duplicateSubscriptionFeedback(ctx *callback.Context, requesterID int64) (*callback.Response, bool) {
	return &callback.Response{
		Text:        "⚠️ 已有相同求片正在处理\n\n这条求片已经在处理，不用重复提交。",
		CallbackMsg: "已有订阅",
		Edit:        true,
	}, ctx == nil || ctx.ChatID != requesterID
}

func (h *ReviewHandler) notifyDuplicateSubscriptionRequester(requesterID int64, markdown string) error {
	if h.telegram == nil {
		return fmt.Errorf("notify requester: telegram client is not configured")
	}
	if _, err := h.telegram.SendRichMessage(requesterID, markdown, nil); err != nil {
		return fmt.Errorf("notify requester: %w", err)
	}
	return nil
}

func duplicateSubscriptionPersistenceFailure() *callback.Response {
	return &callback.Response{
		Text:        "⚠️ 已找到现有订阅，但状态保存失败，未通知原请求人。",
		CallbackMsg: "保存失败",
		Edit:        true,
	}
}

func duplicateSubscriptionNotificationFailure() *callback.Response {
	return &callback.Response{
		Text:        "⚠️ 已有订阅，但没能通知原请求人，请手动告知。",
		CallbackMsg: "通知失败",
		Edit:        true,
	}
}

func (h *ReviewHandler) notifyConfiguredApprovalGroup(ctx *callback.Context, review *services.ReviewRequest, season int) error {
	if h.groupChatID == 0 {
		return nil
	}
	if h.telegram == nil {
		return fmt.Errorf("notify configured group: telegram client is not configured")
	}

	mediaIcon := "🎬"
	mediaTypeText := "电影"
	if review.MediaType == services.MediaTypeTV {
		mediaIcon = "📺"
		mediaTypeText = "剧集"
	}
	seasonText := ""
	if review.MediaType == services.MediaTypeTV {
		seasonText = fmt.Sprintf("第 %d 季", season)
	}
	groupCard := richmessage.BuildGroupApprovedCard(richmessage.GroupApprovedData{
		Title:      review.MediaTitle,
		Year:       review.MediaYear,
		MediaType:  mediaTypeText,
		MediaIcon:  mediaIcon,
		SeasonText: seasonText,
		Requester:  review.TelegramName,
		TMDBID:     review.TmdbID,
	})
	var options []*types.TelegramSendOptions
	if ctx.ChatID == h.groupChatID && ctx.MessageThreadID != 0 {
		options = append(options, &types.TelegramSendOptions{MessageThreadID: ctx.MessageThreadID})
	}
	if _, err := h.telegram.SendRichMessage(h.groupChatID, groupCard.Markdown, nil, options...); err != nil {
		return fmt.Errorf("notify configured group: %w", err)
	}
	return nil
}

func committedApprovalNotificationFailure(titleText string, requesterNotifyErr, groupNotifyErr error) *callback.Response {
	failureText := "申请人私聊通知失败"
	callbackMsg := "私聊通知失败"
	actionText := "请手动私聊申请人告知"
	if requesterNotifyErr == nil {
		failureText = "群通知失败"
		callbackMsg = "群通知失败"
		actionText = "请手动在群内告知"
	} else if groupNotifyErr != nil {
		failureText = "申请人私聊通知失败、群通知失败"
		callbackMsg = "私聊和群通知失败"
		actionText = "请手动联系申请人并在群内告知"
	}
	return &callback.Response{
		Text:        fmt.Sprintf("⚠️ 审核与订阅已成功，但%s\n\n%s\n\n%s，不要重复审批。", failureText, titleText, actionText),
		CallbackMsg: callbackMsg,
		ShowAlert:   true,
		Edit:        true,
	}
}

// handleApprove handles approve callback
// Supports two formats:
// - Legacy: "review_approve:id:xxx:token:yyy"
// - Short: "rv_a:TOKEN" (token uniquely identifies the request)
func (h *ReviewHandler) handleApprove(ctx *callback.Context) (*callback.Response, error) {
	// Check admin permission
	if !h.adminService.IsAdmin(ctx.UserID) {
		logger.Info("[ReviewHandler] 非管理员尝试批准请求: userID=%d", ctx.UserID)
		return &callback.Response{
			Text:        "❌ 此操作仅限管理员使用",
			CallbackMsg: "无权限",
			ShowAlert:   true,
		}, nil
	}

	var token string
	var requestID string
	shortFormat := ctx.Callback.Action == "rv_a"

	// Check format: legacy has "id" param, short format has token directly
	if shortFormat {
		// Short format: "rv_a:TOKEN" - token is after colon
		parts := strings.Split(ctx.Callback.Raw, ":")
		if len(parts) >= 2 {
			token = parts[1]
		}
		// Find request by token (searches ALL requests, not just pending)
		if token != "" {
			if review, found := h.reviewService.GetRequestByToken(token); found {
				requestID = review.RequestID
			}
		}
	} else {
		// Legacy format
		requestID = ctx.Callback.Params["id"]
		token = ctx.Callback.Params["token"]
	}

	if requestID == "" || token == "" {
		return &callback.Response{
			Text:        "❌ 无效的请求",
			CallbackMsg: "无效",
			ShowAlert:   true,
		}, nil
	}
	releaseApproval := h.lockApprovalRequest(requestID)
	defer releaseApproval()

	if shortFormat {
		if current, found := h.reviewService.GetRequestByToken(token); found && current.Status != "pending" {
			statusText := "已批准"
			if current.Status == "rejected" {
				statusText = "已拒绝"
			}
			logger.Info("[ReviewHandler] 请求已被处理: %s, 状态: %s", requestID, current.Status)
			// 重复点击只回一句短提示：不编辑任何消息，避免覆盖申请人进度卡。
			return &callback.Response{
				CallbackMsg: fmt.Sprintf("此请求%s，无需重复操作", statusText),
				ShowAlert:   true,
			}, nil
		}
	}

	// Approve the review with token verification
	review, err := h.reviewService.Approve(requestID, ctx.UserID, token)
	if err != nil {
		// Check if it's a duplicate approval (already approved by another admin)
		if err.Error() == "already_approved" {
			logger.Info("[ReviewHandler] 请求已被其他管理员批准: %s", requestID)
			// 重复点击只回短提示，不编辑任何消息：申请人的进度卡必须保持原样。
			return &callback.Response{
				CallbackMsg: "已被批准",
				ShowAlert:   true,
			}, nil
		}
		return &callback.Response{
			Text:        "❌ 操作失败，请稍后再试",
			CallbackMsg: "失败",
			ShowAlert:   true,
		}, err
	}

	// Wash approvals stay as local work orders. They must never create or
	// overwrite an ordinary MoviePilot subscription.
	if review.NormalizedBusinessType() == services.BusinessTypeWash {
		icon := "🎬"
		if review.MediaType == services.MediaTypeTV {
			icon = "📺"
		}
		privateCard := richmessage.BuildWashStatusCard(richmessage.WashStatusData{Title: review.MediaTitle, Year: review.MediaYear, MediaIcon: icon, Season: review.Season, Status: "approved"})
		if h.telegram != nil {
			if _, sendErr := h.telegram.SendRichMessage(review.TelegramID, privateCard.Markdown, nil); sendErr != nil {
				logger.Warn("[ReviewHandler] 洗版批准私聊通知发送失败 user=%d: %v", review.TelegramID, sendErr)
			}
			if h.groupChatID != 0 {
				publicCard := richmessage.BuildWashStatusCard(richmessage.WashStatusData{Title: review.MediaTitle, Year: review.MediaYear, MediaIcon: icon, Season: review.Season, Status: "approved", Public: true})
				if _, sendErr := h.telegram.SendRichMessage(h.groupChatID, publicCard.Markdown, nil); sendErr != nil {
					logger.Warn("[ReviewHandler] 洗版批准群通知发送失败 group=%d: %v", h.groupChatID, sendErr)
				}
			}
		}
		h.notifyOtherAdmins(ctx.UserID, fmt.Sprintf("✅ 《%s》的洗版工单已被批准", review.MediaTitle))
		return &callback.Response{Text: fmt.Sprintf("✅ 洗版工单已批准\n\n♻️ %s\n\n资源处理完成后，请先查看工单并确认安全核验。", review.MediaTitle), CallbackMsg: "已批准", ShowAlert: true, Edit: true, Keyboard: &callback.Keyboard{InlineKeyboard: [][]callback.Button{{{Text: "📋 查看洗版工单", CallbackData: callback.BuildCallback("review_detail_wash", map[string]string{"token": review.ApproveToken})}}}}}, nil
	}

	// Submit to MoviePilot
	mpMediaType := services.MediaTypeMovie
	if review.MediaType == services.MediaTypeTV {
		mpMediaType = services.MediaTypeTV
	}

	// Use the season from review (0 means all seasons)
	season := review.Season
	if season == 0 && review.MediaType == services.MediaTypeTV {
		season = 1 // Default to season 1 if not specified
	}

	// 1) Emby existence check before creating subscription. TV season requests
	// must be checked at season scope; the parent Series existing is not enough.
	if h.webhookService != nil {
		existingMedia, exists, embyErr := requestExistsInEmby(
			review.TmdbID,
			mpMediaType,
			season,
			h.webhookService.SearchEmbyMediaByTMDB,
			h.moviepilot.EmbyMediaAvailabilityByTMDBSeason,
		)
		if embyErr != nil {
			if requeueErr := h.reviewService.RequeueApprovedPreflightFailure(requestID, "Emby 状态暂时无法确认"); requeueErr != nil {
				return nil, fmt.Errorf("requeue after Emby lookup failure: %w", requeueErr)
			}
			return &callback.Response{
				Text:        "⚠️ 审核已通过，但媒体库状态暂时无法确认\n\n系统会保留这条请求，请稍后再试，不会直接重复下载。",
				CallbackMsg: "状态待确认",
				ShowAlert:   true,
				Edit:        true,
			}, nil
		}
		if exists {
			reasonText := "媒体库已存在该电影"
			if review.MediaType == services.MediaTypeTV {
				reasonText = fmt.Sprintf("媒体库已存在第 %d 季", season)
			}
			if rejectErr := h.reviewService.RejectApprovedPreflight(requestID, ctx.UserID, reasonText); rejectErr != nil {
				return nil, rejectErr
			}
			if _, restoreErr := h.reviewService.RestoreQuotaOnce(requestID, h.quotaService); restoreErr != nil {
				return nil, fmt.Errorf("restore quota after preflight reject: %w", restoreErr)
			}
			title := review.MediaTitle
			if existingMedia != nil && existingMedia.Title != "" {
				title = existingMedia.Title
			}
			reason := "⚠️ 媒体库已存在该电影"
			alert := fmt.Sprintf("⚠️ 已拦截：Emby 已存在《%s》", review.MediaTitle)
			if review.MediaType == services.MediaTypeTV {
				reason = fmt.Sprintf("⚠️ 媒体库已存在第 %d 季", season)
				alert = fmt.Sprintf("⚠️ 已拦截：Emby 已存在《%s》第 %d 季", review.MediaTitle, season)
			}
			blockedCard := richmessage.BuildReviewBlockedCard(title, reason, "")
			_, _ = h.telegram.SendStructuredRichMessage(review.TelegramID, blockedCard.Input(), nil)
			h.updateRequesterReceipt(review, "", richmessage.StatusInLibrary, "配额已退还，可直接观看。")
			return &callback.Response{
				Text:        alert,
				CallbackMsg: "媒体已存在",
				ShowAlert:   true,
				Edit:        true,
			}, nil
		}
	}

	// 2) MoviePilot duplicate subscription check
	if sub, found, mpErr := h.moviepilot.FindExistingSubscription(review.TmdbID, mpMediaType, season); mpErr == nil && found {
		stateText := services.GetStateText(sub.State)
		blockedCard := richmessage.BuildReviewBlockedCard(
			sub.Name,
			"⚠️ 已有相同求片正在处理",
			fmt.Sprintf("当前进度：%s", stateText),
		)
		resp, sendSeparate := duplicateSubscriptionFeedback(ctx, review.TelegramID)
		// Persist the authoritative subscription link before any best-effort
		// notification so an unreachable DM cannot leave an approved request stale.
		if err := h.reviewService.UpdateSubscriptionInfo(requestID, sub.ID, sub.State); err != nil {
			return duplicateSubscriptionPersistenceFailure(), fmt.Errorf("persist existing subscription link: %w", err)
		}
		h.updateRequesterReceipt(review, "", richmessage.StatusApproved, "相同求片处理中，完成后可播放。")
		var requesterNotifyErr error
		if sendSeparate {
			requesterNotifyErr = h.notifyDuplicateSubscriptionRequester(review.TelegramID, blockedCard.Markdown)
			if requesterNotifyErr != nil {
				logger.Warn("[ReviewHandler] Duplicate subscription requester notification failed: %v", requesterNotifyErr)
			}
		}
		groupNotifyErr := h.notifyConfiguredApprovalGroup(ctx, review, season)
		if groupNotifyErr != nil {
			logger.Warn("[ReviewHandler] 求片批准群通知发送失败 group=%d: %v", h.groupChatID, groupNotifyErr)
		}
		if requesterNotifyErr != nil || groupNotifyErr != nil {
			// Preserve the established duplicate-DM failure response when no
			// configured group notification is part of this approval.
			if h.groupChatID == 0 && requesterNotifyErr != nil {
				return duplicateSubscriptionNotificationFailure(), nil
			}
			return committedApprovalNotificationFailure(fmt.Sprintf("《%s》", review.MediaTitle), requesterNotifyErr, groupNotifyErr), nil
		}
		return resp, nil
	}

	req, err := h.moviepilot.RequestMedia(
		review.MediaTitle,
		review.MediaYear,
		review.TmdbID,
		mpMediaType,
		season,
	)
	if err != nil {
		logger.Info("[ReviewHandler] Failed to submit to MoviePilot: %v", err)
		// 关键兜底（修复「审核通过但 MP 失败请求凭空消失」的真 bug）：
		// 审核状态保持 approved，但标记 stuck + 记录错误，让请求可见、可重试。
		if merrr := h.reviewService.MarkStuck(requestID, err.Error()); merrr != nil {
			return nil, fmt.Errorf("persist MoviePilot failure state: %w", merrr)
		}
		// 最终/不可重试的审核提交失败不应消耗用户配额。
		if latest, ok := h.reviewService.GetRequest(requestID); ok && latest.RetryCount >= services.MaxApproveRetry {
			if _, rerr := h.reviewService.RestoreQuotaOnce(requestID, h.quotaService); rerr != nil {
				return nil, fmt.Errorf("restore quota after terminal MoviePilot failure: %w", rerr)
			}
		}
		// Notify user about approval but submission failed
		mediaIcon := "🎬"
		if review.MediaType == services.MediaTypeTV {
			mediaIcon = "📺"
		}
		stuckCard := richmessage.BuildReviewStuckCard(review.MediaTitle, review.MediaYear, mediaIcon)
		_, _ = h.telegram.SendStructuredRichMessage(review.TelegramID, stuckCard.Input(), nil)
		h.updateRequesterReceipt(review, "", richmessage.StatusApproved, "正在同步，可在求片进度查看。")
		return &callback.Response{
			Text:        fmt.Sprintf("✅ 审核已通过，正在找资源\n\n📺 %s\n\n系统会继续自动处理，请稍后查看求片进度。", review.MediaTitle),
			CallbackMsg: "审核已通过",
			ShowAlert:   true,
			Edit:        true,
		}, nil
	}

	logger.Info("[ReviewHandler] Submitted to MoviePilot: ID=%d", req.ID)

	// Persist the real MoviePilot ID before reporting success. A persistence
	// failure leaves the review recoverable and must not be presented as linked.
	if err := h.reviewService.LinkSubscription(requestID, req.ID, "N"); err != nil {
		logger.Info("[ReviewHandler] Failed to link created subscription %d: %v", req.ID, err)
		h.updateRequesterReceipt(review, "", richmessage.StatusApproved, "已提交下载，进度稍后可查。")
		_ = h.reviewService.MarkStuck(requestID, fmt.Sprintf("MoviePilot 订阅 %d 已创建，但本地关联失败: %v", req.ID, err))
		return &callback.Response{
			Text:        "⚠️ 订阅已创建，但进度关联暂时失败\n\n系统会继续恢复关联，请稍后查看求片进度。",
			CallbackMsg: "进度关联待恢复",
			ShowAlert:   true,
			Edit:        true,
		}, nil
	}

	mediaIcon := "🎬"
	mediaTypeText := "电影"
	if review.MediaType == services.MediaTypeTV {
		mediaIcon = "📺"
		mediaTypeText = "剧集"
	}
	seasonText := ""
	if review.MediaType == services.MediaTypeTV {
		seasonText = fmt.Sprintf("第 %d 季", season)
	}
	titleText := fmt.Sprintf("%s 《%s》", mediaIcon, review.MediaTitle)
	if review.MediaYear > 0 {
		titleText += fmt.Sprintf(" (%d)", review.MediaYear)
	}
	if seasonText != "" {
		titleText += " · " + seasonText
	}

	// The callback response already reaches the requester in a private chat. A
	// group/topic callback still needs a separate DM, even when the requester is
	// also the approving administrator.
	var requesterNotifyErr error
	if ctx.ChatID != review.TelegramID {
		approveCard := richmessage.BuildReviewApprovedCard(richmessage.ReviewApprovedData{
			Title:      review.MediaTitle,
			Year:       review.MediaYear,
			MediaType:  mediaTypeText,
			MediaIcon:  mediaIcon,
			SeasonText: seasonText,
		})
		_, requesterNotifyErr = h.telegram.SendStructuredRichMessage(review.TelegramID, approveCard.Input(), nil)
		if requesterNotifyErr != nil {
			logger.Warn("[ReviewHandler] 求片批准私聊通知发送失败 user=%d: %v", review.TelegramID, requesterNotifyErr)
		}
	}

	// 申请人那条「求片已提交」确认卡必须原地更新：批准的可见结果不能只落在
	// 管理员审核消息上，否则用户端会一直显示「状态：等待管理员审核」。
	h.updateRequesterReceipt(review, "", richmessage.StatusApproved, "匹配资源后自动下载，完成后可播放。")

	// 通知其他管理员：此请求已被处理
	h.notifyOtherAdmins(ctx.UserID, fmt.Sprintf("✅ 《%s》已被管理员批准", review.MediaTitle))

	groupNotifyErr := h.notifyConfiguredApprovalGroup(ctx, review, season)
	if groupNotifyErr != nil {
		logger.Warn("[ReviewHandler] 求片批准群通知发送失败 group=%d: %v", h.groupChatID, groupNotifyErr)
	}

	if requesterNotifyErr != nil || groupNotifyErr != nil {
		return committedApprovalNotificationFailure(titleText, requesterNotifyErr, groupNotifyErr), nil
	}

	return &callback.Response{
		CallbackMsg: richmessage.StatusApproved,
		ShowAlert:   false,
	}, nil
}

func (h *ReviewHandler) handleCompleteWash(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{CallbackMsg: "无权限", ShowAlert: true}, nil
	}
	review, requestID, resolveErr := h.washFromContext(ctx)
	if resolveErr != nil {
		return &callback.Response{Text: resolveErr.Error(), CallbackMsg: "无效工单", ShowAlert: true}, nil
	}
	if review.Status == "completed" {
		return &callback.Response{Text: fmt.Sprintf("✅ 洗版工单已完成\n\n♻️ %s", review.MediaTitle), CallbackMsg: "已完成", Edit: true}, nil
	}
	if len(review.WashBaseline) == 0 {
		_ = h.reviewService.RecordWashVerificationFailure(requestID, ctx.UserID, "缺少创建时基线：旧工单不能自动验证")
		return &callback.Response{Text: "⚠️ 这是旧版洗版工单，缺少创建时的媒体基线，无法安全验证完成。\n\n请保留当前资源，并让用户重新创建洗版工单；新工单会自动采集基线后再验证。", CallbackMsg: "缺少基线，已安全拒绝", ShowAlert: true}, nil
	}
	if review.Status == "approved" {
		claimed, err := h.reviewService.ClaimWash(requestID, ctx.UserID)
		if err != nil {
			return &callback.Response{Text: "⚠️ " + err.Error(), CallbackMsg: "认领失败", ShowAlert: true}, nil
		}
		review = claimed
	} else if review.Status != "claimed" || review.WashClaimedBy != ctx.UserID {
		return &callback.Response{Text: "⚠️ 洗版工单状态已变化，请返回工作台刷新。", CallbackMsg: "工单已变化", ShowAlert: true}, nil
	}
	if h.webhookService == nil {
		_ = h.reviewService.RecordWashVerificationFailure(requestID, ctx.UserID, "媒体库核验服务不可用")
		return &callback.Response{Text: "⚠️ 暂时无法连接媒体库核验，工单仍保持认领状态，请恢复 Emby 连接后重试。", CallbackMsg: "媒体库核验不可用", ShowAlert: true}, nil
	}
	currentSources, verifyErr := h.webhookService.CaptureEmbyWashBaseline(review.TmdbID, review.MediaType, review.Season)
	if verifyErr != nil {
		_ = h.reviewService.RecordWashVerificationFailure(requestID, ctx.UserID, verifyErr.Error())
		return &callback.Response{Text: "⚠️ 媒体库核验失败，未标记完成。请确认 Emby 已扫描新旧版本后重试。", CallbackMsg: "核验失败", ShowAlert: true}, nil
	}
	review, err := h.reviewService.CompleteWash(requestID, ctx.UserID, currentSources)
	if err != nil {
		_ = h.reviewService.RecordWashVerificationFailure(requestID, ctx.UserID, err.Error())
		return &callback.Response{Text: fmt.Sprintf("⚠️ 洗版尚未通过安全验证，未标记完成。\n\n%s\n\n请保留旧版、等待 Emby 扫描出新增版本后重试。", err.Error()), CallbackMsg: "验证未通过", ShowAlert: true}, nil
	}
	icon := "🎬"
	if review.MediaType == services.MediaTypeTV {
		icon = "📺"
	}
	card := richmessage.BuildWashStatusCard(richmessage.WashStatusData{Title: review.MediaTitle, Year: review.MediaYear, MediaIcon: icon, Season: review.Season, Status: "completed"})
	if h.telegram != nil {
		if _, sendErr := h.telegram.SendRichMessage(review.TelegramID, card.Markdown, nil); sendErr != nil {
			logger.Warn("[ReviewHandler] 洗版完成通知发送失败 user=%d: %v", review.TelegramID, sendErr)
		}
	}
	return &callback.Response{Text: fmt.Sprintf("✅ 洗版工单已完成\n\n♻️ %s\n\n已验证新增版本且旧版仍保留。", review.MediaTitle), CallbackMsg: "已完成", Edit: true}, nil
}

func (h *ReviewHandler) washFromContext(ctx *callback.Context) (*services.ReviewRequest, string, error) {
	if ctx == nil || ctx.Callback == nil {
		return nil, "", fmt.Errorf("缺少工单引用")
	}
	requestID := strings.TrimSpace(ctx.Callback.Params["id"])
	var review *services.ReviewRequest
	var ok bool
	if token := strings.TrimSpace(ctx.Callback.Params["token"]); token != "" {
		review, ok = h.reviewService.GetRequestByToken(token)
		if ok && review != nil {
			requestID = review.RequestID
		}
	} else if requestID != "" {
		review, ok = h.reviewService.GetRequest(requestID)
	} else {
		return nil, "", fmt.Errorf("缺少工单引用")
	}
	if !ok || review == nil || review.NormalizedBusinessType() != services.BusinessTypeWash {
		return nil, "", fmt.Errorf("找不到洗版工单")
	}
	return review, requestID, nil
}

func (h *ReviewHandler) handleClaimWash(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{CallbackMsg: "无权限", ShowAlert: true}, nil
	}
	_, requestID, err := h.washFromContext(ctx)
	if err != nil {
		return &callback.Response{Text: err.Error(), CallbackMsg: "无效工单", ShowAlert: true}, nil
	}
	review, err := h.reviewService.ClaimWash(requestID, ctx.UserID)
	if err != nil {
		return &callback.Response{Text: "⚠️ " + err.Error(), CallbackMsg: "认领失败", ShowAlert: true}, nil
	}
	return h.renderWashDetail(review, true), nil
}

func (h *ReviewHandler) handleReleaseWash(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{CallbackMsg: "无权限", ShowAlert: true}, nil
	}
	_, requestID, err := h.washFromContext(ctx)
	if err != nil {
		return &callback.Response{Text: err.Error(), CallbackMsg: "无效工单", ShowAlert: true}, nil
	}
	if err := h.reviewService.ReleaseWash(requestID, ctx.UserID); err != nil {
		return &callback.Response{Text: "⚠️ " + err.Error(), CallbackMsg: "释放失败", ShowAlert: true}, nil
	}
	review, _ := h.reviewService.GetRequest(requestID)
	return h.renderWashDetail(review, true), nil
}

// handleReopenWash returns a terminal-failed wash work order to the queue after
// an administrator confirmed the underlying problem is fixed.
func (h *ReviewHandler) handleReopenWash(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{CallbackMsg: "无权限", ShowAlert: true}, nil
	}
	_, requestID, err := h.washFromContext(ctx)
	if err != nil {
		return &callback.Response{Text: err.Error(), CallbackMsg: "无效工单", ShowAlert: true}, nil
	}
	review, err := h.reviewService.ReopenWash(requestID, ctx.UserID)
	if err != nil {
		return &callback.Response{Text: "重开失败：" + err.Error(), CallbackMsg: "重开失败", ShowAlert: true}, nil
	}
	return h.renderWashDetail(review, true), nil
}

func (h *ReviewHandler) handleRetryWash(ctx *callback.Context) (*callback.Response, error) {
	return h.handleCompleteWash(ctx)
}

func (h *ReviewHandler) handleWashDetail(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{CallbackMsg: "无权限", ShowAlert: true}, nil
	}
	review, _, err := h.washFromContext(ctx)
	if err != nil {
		return &callback.Response{Text: err.Error(), CallbackMsg: "无效工单", ShowAlert: true}, nil
	}
	if token := strings.TrimSpace(ctx.Callback.Params["token"]); token != "" && review.Status == "approved" {
		return h.renderWashCompletionConfirmation(review, token), nil
	}
	return h.renderWashDetail(review, true), nil
}

func (h *ReviewHandler) renderWashCompletionConfirmation(review *services.ReviewRequest, token string) *callback.Response {
	season := ""
	if review.Season > 0 {
		season = fmt.Sprintf(" S%02d", review.Season)
	}
	return &callback.Response{
		Text: fmt.Sprintf("♻️ 洗版完成确认\n\n%s%s\n\n请确认 Emby 已出现新版本且旧版仍保留。点击后系统会先认领工单，再核验 MediaSource；验证失败不会标记完成。", review.MediaTitle, season),
		Edit: true,
		Keyboard: &callback.Keyboard{InlineKeyboard: [][]callback.Button{{{
			Text:         "✅ 标记洗版完成",
			CallbackData: callback.BuildCallback("review_complete_wash", map[string]string{"token": token}),
		}}}},
	}
}

func washCallbackParams(review *services.ReviewRequest) map[string]string {
	if review != nil && review.ApproveToken != "" {
		return map[string]string{"token": review.ApproveToken}
	}
	return map[string]string{"id": review.RequestID}
}

func (h *ReviewHandler) renderWashDetail(review *services.ReviewRequest, edit bool) *callback.Response {
	if review == nil {
		return &callback.Response{Text: "找不到洗版工单", ShowAlert: true}
	}
	status := review.Status
	if review.WashLastError != "" {
		status += "\n最近失败：" + review.WashLastError
	}
	text := fmt.Sprintf("♻️ 洗版工单\n\n《%s》", review.MediaTitle)
	if review.MediaYear > 0 {
		text += fmt.Sprintf(" (%d)", review.MediaYear)
	}
	text += fmt.Sprintf("\n状态：%s\n申请人：%s\n创建时间：%s\n旧版基线：%d 个", status, review.TelegramName, review.CreatedAt.Format("2006-01-02 15:04"), len(review.WashBaseline))
	keyboard := &callback.Keyboard{}
	ref := washCallbackParams(review)
	if review.Status == "approved" {
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []callback.Button{{Text: "🙋 认领", CallbackData: callback.BuildCallback("review_claim_wash", ref)}})
	}
	if review.Status == "claimed" {
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []callback.Button{{Text: "✅ 核验完成", CallbackData: callback.BuildCallback("review_complete_wash", ref)}, {Text: "↩️ 释放", CallbackData: callback.BuildCallback("review_release_wash", ref)}})
	}
	if review.Status == "claimed" && review.WashLastError != "" {
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []callback.Button{{Text: "🔁 重新核验", CallbackData: callback.BuildCallback("review_retry_wash", ref)}})
	}
	if review.Status == services.WashStatusFailed {
		// Terminal failure stays visible in the workbench. Reopening is an
		// explicit administrator decision, never an automatic retry.
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []callback.Button{{Text: "🔄 重开工单（人工确认后）", CallbackData: callback.BuildCallback("review_reopen_wash", ref)}})
	}
	return &callback.Response{Text: text, Edit: edit, Keyboard: keyboard}
}

// handleReject handles reject callback
// Supports two formats:
// - Legacy: "review_reject:id:xxx"
// - Short: "rv_r:TOKEN" (token uniquely identifies the request)
func (h *ReviewHandler) handleReject(ctx *callback.Context) (*callback.Response, error) {
	// Check admin permission
	if !h.adminService.IsAdmin(ctx.UserID) {
		logger.Info("[ReviewHandler] 非管理员尝试拒绝请求: userID=%d", ctx.UserID)
		return &callback.Response{
			Text:        "❌ 此操作仅限管理员使用",
			CallbackMsg: "无权限",
			ShowAlert:   true,
		}, nil
	}

	var token string
	var requestID string

	// Check format
	if ctx.Callback.Action == "rv_r" {
		// Short format: "rv_r:TOKEN"
		parts := strings.Split(ctx.Callback.Raw, ":")
		if len(parts) >= 2 {
			token = parts[1]
		}
		// Find request by token (searches ALL requests, not just pending)
		if token != "" {
			if review, found := h.reviewService.GetRequestByToken(token); found {
				requestID = review.RequestID
				// Check if already processed
				if review.Status != "pending" {
					statusText := "已批准"
					if review.Status == "rejected" {
						statusText = "已拒绝"
					}
					logger.Info("[ReviewHandler] 请求已被处理: %s, 状态: %s", requestID, review.Status)
					// 重复点击只回短提示，不编辑消息，避免覆盖申请人进度卡。
					return &callback.Response{
						CallbackMsg: fmt.Sprintf("此请求%s，无需重复操作", statusText),
						ShowAlert:   true,
					}, nil
				}
			}
		}
	} else {
		// Legacy format
		requestID = ctx.Callback.Params["id"]
	}

	if requestID == "" {
		return &callback.Response{
			Text:        "❌ 无效的请求",
			CallbackMsg: "无效",
			ShowAlert:   true,
		}, nil
	}

	// Reject the review (no reason provided in quick reject)
	review, err := h.reviewService.Reject(requestID, ctx.UserID, "管理员拒绝了请求")
	if err != nil {
		return &callback.Response{
			Text:        "❌ 操作失败，请稍后再试",
			CallbackMsg: "失败",
			ShowAlert:   true,
		}, err
	}

	// Restore quota only for ordinary requests; wash work orders never consume it.
	if review.NormalizedBusinessType() == services.BusinessTypeRequest {
		if _, err := h.reviewService.RestoreQuotaOnce(requestID, h.quotaService); err != nil {
			logger.Info("[ReviewHandler] Failed to restore quota for user %d: %v", review.TelegramID, err)
		} else {
			logger.Info("[ReviewHandler] Quota restored for user %d, cost: %d", review.TelegramID, review.QuotaCost)
		}
	}

	// Notify user about rejection
	rejectMediaIcon := "🎬"
	if review.MediaType == services.MediaTypeTV {
		rejectMediaIcon = "📺"
	}
	rejectCard := richmessage.BuildReviewRejectedCard(review.MediaTitle, review.MediaYear, rejectMediaIcon)
	_, _ = h.telegram.SendStructuredRichMessage(review.TelegramID, rejectCard.Input(), nil)
	h.updateRequesterReceipt(review, "", richmessage.StatusRejected, "配额已退还，可换片名再试。")

	// 通知其他管理员：此请求已被处理
	h.notifyOtherAdmins(ctx.UserID, fmt.Sprintf("❌ 《%s》已被管理员拒绝", review.MediaTitle))

	// 通知拼车用户
	if h.OnCarpoolNotify != nil {
		go h.OnCarpoolNotify(review.TmdbID, string(review.MediaType), review.MediaTitle, "管理员拒绝")
	}

	return &callback.Response{
		CallbackMsg: richmessage.StatusRejected,
		ShowAlert:   false,
	}, nil
}

// handleCancel handles cancel callback (user cancelling their own request)
func (h *ReviewHandler) handleCancel(ctx *callback.Context) (*callback.Response, error) {
	requestID := ctx.Callback.Params["id"]

	// Get the review first
	review, exists := h.reviewService.GetRequest(requestID)
	if !exists {
		return &callback.Response{
			Text:        "❌ 请求不存在",
			CallbackMsg: "失败",
			ShowAlert:   true,
		}, nil
	}

	// Only the user who created the request can cancel it
	if review.TelegramID != ctx.UserID {
		return &callback.Response{
			Text:        "❌ 你只能取消自己的请求",
			CallbackMsg: "无权限",
			ShowAlert:   true,
		}, nil
	}

	// 服务层原子校验所有者与 pending 状态，并保留 cancelled 审计记录。
	if err := h.reviewService.CancelByUser(requestID, ctx.UserID); err != nil {
		return &callback.Response{
			Text:        "❌ 只能撤回本人尚未审核的请求",
			CallbackMsg: "无法撤回",
			ShowAlert:   true,
		}, nil
	}

	quotaRestored := false
	if h.quotaService != nil {
		_, err := h.reviewService.RestoreQuotaOnce(requestID, h.quotaService)
		quotaRestored = err == nil
		if err != nil {
			logger.Info("[ReviewHandler] 请求已撤回但配额返还失败: %v", err)
		}
	}

	text := "✅ 请求已取消"
	if quotaRestored {
		text += "，配额已恢复"
	} else {
		text += "\n\n⚠️ 配额返还异常，管理员会根据审核记录补偿处理"
	}
	return &callback.Response{
		Text:        text,
		CallbackMsg: "已取消",
		ShowAlert:   true,
		Edit:        true,
	}, nil
}

// handleMyReviews shows user's review requests
func (h *ReviewHandler) handleMyReviews(ctx *callback.Context) (*callback.Response, error) {
	reviews := h.reviewService.GetUserRequests(ctx.UserID)

	if len(reviews) == 0 {
		return &callback.Response{
			Text:        "求片进度\n\n暂无记录。发片名即可求片。",
			RichMessage: "求片进度\n\n暂无记录。发片名即可求片。",
			Edit:        true,
		}, nil
	}

	items := make([]richmessage.MyReviewItem, 0, len(reviews))
	for _, review := range reviews {
		subState := ""
		if review.SubscriptionID > 0 && review.SubscriptionState != "" {
			subState = services.GetSubscriptionStateText(review.SubscriptionState)
		}
		items = append(items, richmessage.MyReviewItem{
			Title:    review.MediaTitle,
			Year:     review.MediaYear,
			Status:   review.Status,
			SubState: subState,
			Time:     review.CreatedAt.Format("01-02 15:04"),
		})
	}
	card := richmessage.BuildMyReviewsCard(items)
	return &callback.Response{
		Text:                  card.Markdown,
		RichMessage:           card.Markdown,
		StructuredRichMessage: card.Input(),
		Edit:                  true,
	}, nil
}

// handleReviewList shows pending reviews for admins
func (h *ReviewHandler) handleReviewList(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{
			Text:        "❌ 此功能仅限管理员使用",
			CallbackMsg: "无权限",
			ShowAlert:   true,
		}, nil
	}

	pending := h.reviewService.GetPendingRequests()
	washQueue := h.reviewService.GetWashRequests()

	if len(pending) == 0 && len(washQueue) == 0 {
		return &callback.Response{
			Text:        "待审核\n\n暂无待审核请求。",
			RichMessage: "待审核\n\n暂无待审核请求。",
			Edit:        true,
		}, nil
	}

	items := make([]richmessage.PendingReviewItem, 0, len(pending))
	for i, review := range pending {
		items = append(items, richmessage.PendingReviewItem{
			Index: i + 1,
			Title: review.MediaTitle,
			Year:  review.MediaYear,
			User:  review.TelegramName,
			Time:  review.CreatedAt.Format("01-02 15:04"),
		})
	}
	card := richmessage.BuildPendingReviewsCard(items)
	keyboard := &callback.Keyboard{}
	if len(washQueue) > 0 {
		card.Markdown += fmt.Sprintf("\n\n♻️ **洗版工作台：%d 条**", len(washQueue))
		limit := len(washQueue)
		if limit > 10 {
			limit = 10
		}
		for _, review := range washQueue[:limit] {
			label := review.MediaTitle
			if review.Season > 0 {
				label += fmt.Sprintf(" S%02d", review.Season)
			}
			status := review.Status
			if review.WashClaimedBy != 0 {
				status += fmt.Sprintf(" · 管理员 %d", review.WashClaimedBy)
			}
			if review.WashLastError != "" {
				status = "⚠️ " + status
			}
			keyboard.InlineKeyboard = append(keyboard.InlineKeyboard,
				[]callback.Button{{Text: "♻️ " + label + " · " + status, CallbackData: callback.BuildCallback("review_detail_wash", washCallbackParams(review))}},
			)
		}
	}
	return &callback.Response{
		RichMessage: card.Markdown,
		Edit:        true,
		Keyboard:    keyboard,
	}, nil
}

// notifyOtherAdmins 通知除当前操作管理员以外的其他管理员。
func (h *ReviewHandler) notifyOtherAdmins(currentAdminID int64, message string) {
	if h.adminService == nil {
		return
	}
	adminIDs := h.adminService.GetAdminIDs()
	for _, adminID := range adminIDs {
		if adminID == currentAdminID {
			continue
		}
		go h.telegram.SendMessage(adminID, message, "", nil)
	}
}

func disabledReviewResultKeyboard(approved bool) *callback.Keyboard {
	label := "✅ 已批准"
	if !approved {
		label = "❌ 已拒绝"
	}
	return &callback.Keyboard{InlineKeyboard: [][]callback.Button{{{Text: label, Disabled: true}}}}
}
