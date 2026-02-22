package handlers

import (
	"fmt"
	"log"

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
	case "review_approve":
		return h.handleApprove(ctx)
	case "review_reject":
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
func (h *ReviewHandler) handleApprove(ctx *callback.Context) (*callback.Response, error) {
	requestID := ctx.Callback.Params["id"]

	// Approve the review
	review, err := h.reviewService.Approve(requestID, ctx.UserID)
	if err != nil {
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

	req, err := h.moviepilot.RequestMedia(
		review.MediaTitle,
		review.MediaYear,
		review.TmdbID,
		mpMediaType,
		1, // Default season
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

	// Notify user about approval
	h.telegram.SendMessage(review.TelegramID,
		fmt.Sprintf("✅ 你的求片请求已批准并提交！\n\n📺 %s (%d)\n\n请求 ID: %d",
			review.MediaTitle, review.MediaYear, req.ID), "", nil)

	return &callback.Response{
		Text:        fmt.Sprintf("✅ 已批准并提交\n\n📺 %s", review.MediaTitle),
		CallbackMsg: "已批准",
		ShowAlert:   true,
		Edit:        true,
	}, nil
}

// handleReject handles reject callback
func (h *ReviewHandler) handleReject(ctx *callback.Context) (*callback.Response, error) {
	requestID := ctx.Callback.Params["id"]

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
