package handlers

import (
	"fmt"
	"strings"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/richmessage"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/logger"
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
	// OnCarpoolNotify 拼车用户通知回调（拒绝/撤回时触发）。
	// 参数：tmdbID, mediaType, title, reason
	OnCarpoolNotify func(tmdbID int, mediaType, title, reason string)
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
	action := ctx.Callback.Action

	switch action {
	case "review_approve", "rv_a":
		return h.handleApprove(ctx)
	case "review_reject", "rv_r":
		return h.handleReject(ctx)
	case "review_cancel":
		return h.handleCancel(ctx)
	case "review_complete_wash":
		return h.handleCompleteWash(ctx)
	case "my_reviews":
		return h.handleMyReviews(ctx)
	case "review_list":
		return h.handleReviewList(ctx)
	default:
		return nil, nil
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

	// Check format: legacy has "id" param, short format has token directly
	if ctx.Callback.Action == "rv_a" {
		// Short format: "rv_a:TOKEN" - token is after colon
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
					return &callback.Response{
						Text:        fmt.Sprintf("✅ 此请求已被%s", statusText),
						CallbackMsg: "已被处理",
						ShowAlert:   true,
						Edit:        true,
					}, nil
				}
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

	// Approve the review with token verification
	review, err := h.reviewService.Approve(requestID, ctx.UserID, token)
	if err != nil {
		// Check if it's a duplicate approval (already approved by another admin)
		if err.Error() == "already_approved" {
			// Get the current review state
			logger.Info("[ReviewHandler] 请求已被其他管理员批准: %s", requestID)
			return &callback.Response{
				Text:        "✅ 审核已处理\n\n这条求片已由其他管理员通过，无需重复操作。",
				CallbackMsg: "已被批准",
				ShowAlert:   true,
				Edit:        true,
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
		return &callback.Response{Text: fmt.Sprintf("✅ 洗版工单已批准\n\n♻️ %s\n\n资源处理并验证完成后，请点击下方按钮收口。", review.MediaTitle), CallbackMsg: "已批准", ShowAlert: true, Edit: true, Keyboard: &callback.Keyboard{InlineKeyboard: [][]callback.Button{{{Text: "✅ 标记洗版完成", CallbackData: callback.BuildCallback("review_complete_wash", map[string]string{"token": review.ApproveToken})}}}}}, nil
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
				Text:        "⚠️ 审核已通过，但媒体库状态暂时无法确认\n\n系统会保留这条请求，请稍后重试，不会直接重复下载。",
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
			h.telegram.SendRichMessage(review.TelegramID, blockedCard.Markdown, nil)
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
		h.telegram.SendRichMessage(review.TelegramID, blockedCard.Markdown, nil)
		// Sync review with existing subscription info when possible
		// Note: UpdateSubscriptionInfo failure is not critical here since we're returning an intercept response
		_ = h.reviewService.UpdateSubscriptionInfo(requestID, sub.ID, sub.State)
		return &callback.Response{
			Text:        "⚠️ 已有相同求片正在处理，无需重复提交",
			CallbackMsg: "已有订阅",
			ShowAlert:   true,
			Edit:        true,
		}, nil
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
		h.telegram.SendRichMessage(review.TelegramID, stuckCard.Markdown, nil)
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

	// Notify user about approval. 如果审批人就是求片人，避免同一聊天重复出现两条通过通知。
	if ctx.UserID != review.TelegramID {
		approveCard := richmessage.BuildReviewApprovedCard(richmessage.ReviewApprovedData{
			Title:      review.MediaTitle,
			Year:       review.MediaYear,
			MediaType:  mediaTypeText,
			MediaIcon:  mediaIcon,
			SeasonText: seasonText,
		})
		approveKb := services.NewKeyboardBuilder()
		approveKb.AddButton("📍 查看求片进度", "my_requests")
		h.telegram.SendRichMessage(review.TelegramID, approveCard.Markdown, approveKb.Build())
	}

	// 通知其他管理员：此请求已被处理
	h.notifyOtherAdmins(ctx.UserID, fmt.Sprintf("✅ 《%s》已被管理员批准", review.MediaTitle))

	return &callback.Response{
		Text:        fmt.Sprintf("✅ 审核已通过，正在找资源\n\n%s\n\n入库后会自动提醒，也可点「求片进度」查看状态。", titleText),
		CallbackMsg: "已批准",
		ShowAlert:   true,
		Edit:        true,
	}, nil
}

func (h *ReviewHandler) handleCompleteWash(ctx *callback.Context) (*callback.Response, error) {
	if !h.adminService.IsAdmin(ctx.UserID) {
		return &callback.Response{CallbackMsg: "无权限", ShowAlert: true}, nil
	}
	requestID := ctx.Callback.Params["id"]
	var review *services.ReviewRequest
	if token := ctx.Callback.Params["token"]; token != "" {
		if found, ok := h.reviewService.GetRequestByToken(token); ok {
			review, requestID = found, found.RequestID
		}
	}
	if review == nil {
		review, _ = h.reviewService.GetRequest(requestID)
	}
	if review == nil {
		return &callback.Response{CallbackMsg: "找不到洗版工单", ShowAlert: true}, nil
	}
	if review.Status == "completed" {
		return &callback.Response{Text: fmt.Sprintf("✅ 洗版工单已完成\n\n♻️ %s", review.MediaTitle), CallbackMsg: "已完成", Edit: true}, nil
	}
	if len(review.WashBaseline) == 0 {
		return &callback.Response{Text: "⚠️ 这是旧版洗版工单，缺少创建时的媒体基线，无法安全验证完成。\n\n请保留当前资源，并让用户重新创建洗版工单；新工单会自动采集基线后再验证。", CallbackMsg: "缺少基线，已安全拒绝", ShowAlert: true}, nil
	}
	if h.webhookService == nil {
		return &callback.Response{Text: "⚠️ 暂时无法连接媒体库核验，工单仍保持已批准状态，请恢复 Emby 连接后重试。", CallbackMsg: "媒体库核验不可用", ShowAlert: true}, nil
	}
	currentSources, verifyErr := h.webhookService.CaptureEmbyWashBaseline(review.TmdbID, review.MediaType, review.Season)
	if verifyErr != nil {
		return &callback.Response{Text: "⚠️ 媒体库核验失败，未标记完成。请确认 Emby 已扫描新旧版本后重试。", CallbackMsg: "核验失败", ShowAlert: true}, nil
	}
	review, err := h.reviewService.CompleteWash(requestID, ctx.UserID, currentSources)
	if err != nil {
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
					return &callback.Response{
						Text:        fmt.Sprintf("✅ 此请求已被%s", statusText),
						CallbackMsg: "已被处理",
						ShowAlert:   true,
						Edit:        true,
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
	rejectKb := services.NewKeyboardBuilder()
	rejectKb.AddButton("🏠 主菜单", "start")
	h.telegram.SendRichMessage(review.TelegramID, rejectCard.Markdown, rejectKb.Build())

	// 通知其他管理员：此请求已被处理
	h.notifyOtherAdmins(ctx.UserID, fmt.Sprintf("❌ 《%s》已被管理员拒绝", review.MediaTitle))

	// 通知拼车用户
	if h.OnCarpoolNotify != nil {
		go h.OnCarpoolNotify(review.TmdbID, string(review.MediaType), review.MediaTitle, "管理员拒绝")
	}

	return &callback.Response{
		Text:        fmt.Sprintf("❌ 已拒绝\n\n📺 %s", review.MediaTitle),
		CallbackMsg: "已拒绝",
		ShowAlert:   true,
		Edit:        true,
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
			RichMessage: "📊 求片进度\n\n暂无求片记录\n\n选择「🔍 搜索求片」即可添加",
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
		RichMessage: card.Markdown,
		Edit:        true,
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
	approvedWash := h.reviewService.GetApprovedWashRequests()

	if len(pending) == 0 && len(approvedWash) == 0 {
		return &callback.Response{
			RichMessage: "📋 待审核求片\n\n暂无待审核请求 ✨",
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
	if len(approvedWash) > 0 {
		card.Markdown += fmt.Sprintf("\n\n♻️ **待完成洗版：%d 条**", len(approvedWash))
		limit := len(approvedWash)
		if limit > 10 {
			limit = 10
		}
		for _, review := range approvedWash[:limit] {
			label := review.MediaTitle
			if review.Season > 0 {
				label += fmt.Sprintf(" S%02d", review.Season)
			}
			keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []callback.Button{{Text: "✅ 完成 " + label, CallbackData: callback.BuildCallback("review_complete_wash", map[string]string{"token": review.ApproveToken})}})
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
