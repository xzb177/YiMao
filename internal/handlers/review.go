package handlers

import (
	"fmt"
	"log"
	"strings"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
)

// ReviewHandler handles review request callbacks
type ReviewHandler struct {
	sessMgr        *session.Manager
	telegram       *services.TelegramClient
	moviepilot     *services.MoviePilotClient
	adminService   *services.AdminService
	reviewService  *services.ReviewService
}

func NewReviewHandler(
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	moviepilot *services.MoviePilotClient,
	adminService *services.AdminService,
	reviewService *services.ReviewService,
) *ReviewHandler {
	return &ReviewHandler{
		sessMgr:       sessMgr,
		telegram:      telegram,
		moviepilot:    moviepilot,
		adminService:  adminService,
		reviewService: reviewService,
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
		log.Printf("[ReviewHandler] 非管理员尝试批准请求: userID=%d", ctx.UserID)
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
		// Find request by token
		if token != "" {
			// Need to find the request by token - ReviewService doesn't have this method yet
			// For now, get all pending requests and find matching token
			pending := h.reviewService.GetPendingRequests()
			for _, req := range pending {
				if req.ApproveToken == token {
					requestID = req.RequestID
					break
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
			log.Printf("[ReviewHandler] 请求已被其他管理员批准: %s", requestID)
			return &callback.Response{
				Text:        "✅ 此请求已被其他管理员批准",
				CallbackMsg: "已被批准",
				ShowAlert:   true,
				Edit:        true,
			}, nil
		}
		return &callback.Response{
			Text:        fmt.Sprintf("❌ 操作失败: %v", err),
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

	req, err := h.moviepilot.RequestMedia(
		review.MediaTitle,
		review.MediaYear,
		review.TmdbID,
		mpMediaType,
		season,
	)
	if err != nil {
		log.Printf("[ReviewHandler] Failed to submit to MoviePilot: %v", err)
		// Notify user about approval but submission failed
		h.telegram.SendMessage(review.TelegramID,
			fmt.Sprintf("✅ 你的求片请求已批准\n\n📺 %s\n\n但自动提交失败，请稍后再试", review.MediaTitle), "", nil)
		return &callback.Response{
			Text:        "✅ 已批准（但提交到 MoviePilot 失败）",
			CallbackMsg: "已批准",
			ShowAlert:   true,
			Edit:        true,
		}, nil
	}

	log.Printf("[ReviewHandler] Submitted to MoviePilot: ID=%d", req.ID)

	// Save subscription ID to review
	if err := h.reviewService.UpdateSubscriptionInfo(requestID, req.ID, "N"); err != nil {
		log.Printf("[ReviewHandler] Failed to update subscription info: %v", err)
	}

	// Get subscription status
	statusText := services.GetSubscriptionStateText("N")

	// Notify user about approval
	h.telegram.SendMessage(review.TelegramID,
		fmt.Sprintf("✅ 你的求片请求已批准并提交！\n\n📺 %s (%d)\n\n订阅 ID: %d\n状态: %s",
			review.MediaTitle, review.MediaYear, req.ID, statusText), "", nil)

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
		log.Printf("[ReviewHandler] 非管理员尝试拒绝请求: userID=%d", ctx.UserID)
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
		// Find request by token
		if token != "" {
			pending := h.reviewService.GetPendingRequests()
			for _, req := range pending {
				if req.ApproveToken == token {
					requestID = req.RequestID
					break
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
			Text:        fmt.Sprintf("❌ 操作失败: %v", err),
			CallbackMsg: "失败",
			ShowAlert:   true,
		}, err
	}

	// Notify user about rejection
	h.telegram.SendMessage(review.TelegramID,
		fmt.Sprintf("❌ 你的求片请求已被拒绝\n\n📺 %s (%d)\n\n如果需要帮助，请联系管理员",
			review.MediaTitle, review.MediaYear), "", nil)

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

	// Delete the review
	if err := h.reviewService.DeleteRequest(requestID); err != nil {
		return &callback.Response{
			Text:        fmt.Sprintf("❌ 操作失败: %v", err),
			CallbackMsg: "失败",
			ShowAlert:   true,
		}, err
	}

	return &callback.Response{
		Text:        "✅ 请求已取消",
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
			Text:        "📋 我的求片\n\n暂无求片记录\n\n使用 🔍 搜索功能来请求影片",
			Edit:        true,
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
			Text:        "📋 待审核求片\n\n暂无待审核请求 ✨",
			Edit:        true,
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
