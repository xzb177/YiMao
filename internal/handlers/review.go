package handlers

import (
	"fmt"
	"strings"

	"github.com/xzb177/yimao/internal/callback"
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
}

func NewReviewHandler(
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	moviepilot *services.MoviePilotClient,
	adminService *services.AdminService,
	reviewService *services.ReviewService,
	quotaService *services.QuotaService,
	webhookService *services.WebhookService,
) *ReviewHandler {
	return &ReviewHandler{
		sessMgr:        sessMgr,
		telegram:       telegram,
		moviepilot:     moviepilot,
		adminService:   adminService,
		reviewService:  reviewService,
		quotaService:   quotaService,
		webhookService: webhookService,
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
				Text:        "✅ 此请求已被其他管理员批准",
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

	// 1) Emby existence check before creating subscription
	if h.webhookService != nil {
		embyType := services.MediaTypeMovie
		if review.MediaType == services.MediaTypeTV {
			embyType = services.MediaTypeTV
		}
		existingMedia, embyErr := h.webhookService.SearchEmbyMedia(review.MediaTitle, review.MediaYear, embyType)
		if embyErr == nil && existingMedia != nil {
			h.telegram.SendMessage(review.TelegramID,
				fmt.Sprintf("⚠️ 求片已自动拦截：媒体库已存在\n\n📺 %s", existingMedia.Title), "", nil)
			return &callback.Response{
				Text:        fmt.Sprintf("⚠️ 已拦截：Emby 已存在《%s》", review.MediaTitle),
				CallbackMsg: "媒体已存在",
				ShowAlert:   true,
				Edit:        true,
			}, nil
		}
	}

	// 2) MoviePilot duplicate subscription check
	if sub, found, mpErr := h.moviepilot.FindExistingSubscription(review.TmdbID, mpMediaType, season); mpErr == nil && found {
		stateText := services.GetStateText(sub.State)
		h.telegram.SendMessage(review.TelegramID,
			fmt.Sprintf("⚠️ 求片已自动拦截：MoviePilot 已有订阅\n\n📺 %s\n状态：%s", sub.Name, stateText), "", nil)
		// Sync review with existing subscription info when possible
		// Note: UpdateSubscriptionInfo failure is not critical here since we're returning an intercept response
		_ = h.reviewService.UpdateSubscriptionInfo(requestID, sub.ID, sub.State)
		return &callback.Response{
			Text:        fmt.Sprintf("⚠️ 已拦截：MP 已存在订阅（#%d）", sub.ID),
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
			logger.Info("[ReviewHandler] MarkStuck 失败: %v", merrr)
		}
		// Notify user about approval but submission failed
		h.telegram.SendMessage(review.TelegramID,
			fmt.Sprintf("✅ 《%s》已通过审核\n\n⚠️ 正在同步到下载器，稍等一下就好\n去「求片进度」查看状态", review.MediaTitle), "", nil)
		return &callback.Response{
			Text:        "⚠️ 已批准，但提交 MoviePilot 失败（已进入重试兜底，可在管理面板手动重试）",
			CallbackMsg: "已批准·待重试",
			ShowAlert:   true,
			Edit:        true,
		}, nil
	}

	logger.Info("[ReviewHandler] Submitted to MoviePilot: ID=%d", req.ID)

	// 提交成功，清除可能存在的 stuck 兜底状态
	if cerr := h.reviewService.ClearStuck(requestID); cerr != nil {
		logger.Info("[ReviewHandler] ClearStuck 失败: %v", cerr)
	}

	// Note: Quota was already deducted when user submitted the request
	// No need to deduct again here

	// Save subscription ID to review
	if err := h.reviewService.UpdateSubscriptionInfo(requestID, req.ID, "N"); err != nil {
		logger.Info("[ReviewHandler] Failed to update subscription info: %v", err)
	}

	// Notify user about approval
	h.telegram.SendMessage(review.TelegramID,
		fmt.Sprintf("✅ 《%s》已通过审核！\n\n📥 已提交下载，完成后会通知你\n去「求片进度」随时看状态",
			review.MediaTitle), "", nil)

	return &callback.Response{
		Text:        fmt.Sprintf("✅ 已批准并提交\n\n📺 %s", review.MediaTitle),
		CallbackMsg: "已批准",
		ShowAlert:   true,
		Edit:        true,
	}, nil
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

	// Restore quota since the request was rejected
	mediaType := "movie"
	if review.MediaType == services.MediaTypeTV {
		mediaType = "tv"
	}
	if err := h.quotaService.RestoreQuota(review.TelegramID, mediaType); err != nil {
		logger.Info("[ReviewHandler] Failed to restore quota for user %d: %v", review.TelegramID, err)
		// Don't fail the rejection, just log the error
	} else {
		logger.Info("[ReviewHandler] Quota restored for user %d, media type: %s", review.TelegramID, mediaType)
	}

	// Notify user about rejection
	h.telegram.SendMessage(review.TelegramID,
		fmt.Sprintf("❌ 《%s》未通过审核\n\n💡 已自动退还配额，换个片名再试？",
			review.MediaTitle), "", nil)

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

	// Restore quota since the user is cancelling their own pending request
	mediaType := "movie"
	if review.MediaType == services.MediaTypeTV {
		mediaType = "tv"
	}
	if err := h.quotaService.RestoreQuota(review.TelegramID, mediaType); err != nil {
		logger.Info("[ReviewHandler] Failed to restore quota for user %d: %v", review.TelegramID, err)
		// Don't fail the cancellation, just log the error
	} else {
		logger.Info("[ReviewHandler] Quota restored for user %d on cancel, media type: %s", review.TelegramID, mediaType)
	}

	// Delete the review
	if err := h.reviewService.DeleteRequest(requestID); err != nil {
		return &callback.Response{
			Text:        "❌ 操作失败，请稍后再试",
			CallbackMsg: "失败",
			ShowAlert:   true,
		}, err
	}

	return &callback.Response{
		Text:        "✅ 请求已取消，配额已恢复",
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
			Text: "📋 我的求片\n\n暂无求片记录\n\n使用 🔍 搜索功能来请求影片",
			Edit: true,
		}, nil
	}

	// Build message
	text := fmt.Sprintf("📋 我的求片 (%d 条)\n\n", len(reviews))

	for _, review := range reviews {
		statusIcon := "⏳"
		statusText := "待审核"
		switch review.Status {
		case "approved":
			statusIcon = "✅"
			statusText = "已批准"
			// Show subscription status if available
			if review.SubscriptionID > 0 {
				subState := review.SubscriptionState
				if subState == "" {
					subState = "N" // Default to new if not set
				}
				subStatusText := services.GetSubscriptionStateText(subState)
				statusText = fmt.Sprintf("%s\n   订阅: %s", subStatusText, statusText)
			}
		case "rejected":
			statusIcon = "❌"
			statusText = "已拒绝"
		}

		text += fmt.Sprintf("%s %s (%d)\n   状态: %s\n   时间: %s\n\n",
			statusIcon, review.MediaTitle, review.MediaYear, statusText,
			review.CreatedAt.Format("01-02 15:04"))
	}

	return &callback.Response{
		Text: text,
		Edit: true,
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

	if len(pending) == 0 {
		return &callback.Response{
			Text: "📋 待审核求片\n\n暂无待审核请求 ✨",
			Edit: true,
		}, nil
	}

	text := fmt.Sprintf("📋 待审核求片 (%d 条)\n\n", len(pending))

	for i, review := range pending {
		text += fmt.Sprintf("%d. %s (%d)\n   用户: %s\n   时间: %s\n\n",
			i+1, review.MediaTitle, review.MediaYear, review.TelegramName,
			review.CreatedAt.Format("01-02 15:04"))
	}

	return &callback.Response{
		Text: text,
		Edit: true,
	}, nil
}
