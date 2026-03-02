package handlers

import (
	"fmt"
	"log"
	"strings"
	"time"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
	"emby-telegram-bot/pkg/errors"
	"emby-telegram-bot/pkg/types"
)

// RequestHandler handles media request callbacks
type RequestHandler struct {
	sessMgr        *session.Manager
	telegram       *services.TelegramClient
	moviepilot     *services.MoviePilotClient
	adminService   *services.AdminService
	webhookService *services.WebhookService
	userMapping    *services.UserMappingService
	quotaService   *services.QuotaService
	reviewService  *services.ReviewService
	enableReview   bool // Enable review system
}

func NewRequestHandler(
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	moviepilot *services.MoviePilotClient,
	adminService *services.AdminService,
	webhookService *services.WebhookService,
	userMapping *services.UserMappingService,
	quotaService *services.QuotaService,
	reviewService *services.ReviewService,
) *RequestHandler {
	return &RequestHandler{
		sessMgr:        sessMgr,
		telegram:       telegram,
		moviepilot:     moviepilot,
		adminService:   adminService,
		webhookService: webhookService,
		userMapping:    userMapping,
		quotaService:   quotaService,
		reviewService:  reviewService,
		enableReview:   true, // Enable review system by default
	}
}

func (h *RequestHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	log.Printf("[RequestHandler] Handle called: userID=%d, params=%v", ctx.UserID, ctx.Callback.Params)

	// Defensive check: ensure required services are available
	if h.userMapping == nil {
		log.Printf("[RequestHandler] ERROR: userMapping service is nil!")
		return &callback.Response{
			Text:        "❌ 服务配置错误",
			CallbackMsg: "配置错误",
			ShowAlert:   true,
		}, nil
	}
	if h.sessMgr == nil {
		log.Printf("[RequestHandler] ERROR: sessMgr service is nil!")
		return &callback.Response{
			Text:        "❌ 服务配置错误",
			CallbackMsg: "配置错误",
			ShowAlert:   true,
		}, nil
	}
	if h.reviewService == nil {
		log.Printf("[RequestHandler] ERROR: reviewService is nil!")
		return &callback.Response{
			Text:        "❌ 服务配置错误",
			CallbackMsg: "配置错误",
			ShowAlert:   true,
		}, nil
	}

	// Get media ID and type from params
	mediaID, hasID := ctx.Callback.Params["id"]
	mediaType, hasType := ctx.Callback.Params["type"]

	if !hasID || !hasType {
		return nil, errors.InvalidInput("media ID and type are required")
	}

	// Parse TMDB ID
	tmdbID := 0
	fmt.Sscanf(mediaID, "%d", &tmdbID)
	if tmdbID == 0 {
		return nil, errors.InvalidInput("invalid media ID")
	}

	// Parse season parameter (for TV shows)
	season := 0
	if seasonStr, hasSeason := ctx.Callback.Params["season"]; hasSeason {
		fmt.Sscanf(seasonStr, "%d", &season)
	}

	log.Printf("[RequestHandler] Parsed: tmdbID=%d, mediaType=%s, season=%d", tmdbID, mediaType, season)

	// Get MoviePilot user ID from user mapping (with timeout protection)
	log.Printf("[RequestHandler] Getting user mapping for userID=%d...", ctx.UserID)
	moviepilotID, exists := h.userMapping.GetMoviePilotUserID(ctx.UserID)
	log.Printf("[RequestHandler] User mapping result: moviepilotID=%d, exists=%v", moviepilotID, exists)
	if !exists || moviepilotID == 0 {
		// Build link instructions message with button
		msg := services.NewMessageBuilder()
		msg.Bold("🔗 需要绑定账号").Newline()
		msg.Newline()
		msg.Text("求片功能需要绑定账号后才能使用哦").Newline()
		msg.Newline()
		msg.Text("📝 绑定方法：").Newline()
		msg.Code("/link 账号 密码").Newline()
		msg.Newline()
		msg.Italic("💡 新用户首次使用会自动创建账号").Newline()

		kb := services.NewKeyboardBuilder()
		kb.AddButton("🔗 立即绑定", "link")
		kb.AddButton("⬅️ 返回", "start")

		return &callback.Response{
			Text:     msg.Build(),
			Edit:     true,
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	// Get media info from session for better display
	sess := h.sessMgr.Get(ctx.UserID)
	if sess == nil {
		return &callback.Response{
			Text:        "⏰ 会话已过期，请重新搜索",
			CallbackMsg: "会话过期",
			ShowAlert:   true,
		}, nil
	}

	// Get user name from session
	userName := "用户"
	if name, ok := sess.GetString("user_name"); ok {
		userName = name
	}

	// Find media from recent search results or AI cache
	var mediaTitle string
	var mediaYear int
	var posterPath string
	var overview string

	// First try search results
	searchResults, _, _, found := sess.GetSearchResults()
	if found && len(searchResults) > 0 {
		for _, item := range searchResults {
			// Parse TMDB ID from SearchItem.ID (format: "id:123" or just "123")
			itemID := item.ID
			if strings.HasPrefix(itemID, "id:") {
				itemID = strings.TrimPrefix(itemID, "id:")
			}
			itemTmdbID := 0
			fmt.Sscanf(itemID, "%d", &itemTmdbID)

			if itemTmdbID == tmdbID {
				mediaTitle = item.Title
				mediaYear = item.Year
				posterPath = item.Poster
				overview = item.Overview
				break
			}
		}
	}

	// If not found in search results, try AI cache
	if mediaTitle == "" {
		if cachedItem := sess.GetCachedAIItem(tmdbID); cachedItem != nil {
			mediaTitle = cachedItem.Title
			mediaYear = cachedItem.Year
			overview = cachedItem.Overview
		}
	}

	// Final fallback
	if mediaTitle == "" {
		mediaTitle = fmt.Sprintf("TMDB:%d", tmdbID)
	}

	// Check if media already exists in Emby library
	// 使用较短的超时 (5秒) 避免阻塞求片流程
	embyType := services.MediaTypeMovie
	if mediaType == "tv" {
		embyType = services.MediaTypeTV
	}

	log.Printf("[RequestHandler] Checking Emby for: %s (%d) %s (5s timeout)", mediaTitle, mediaYear, embyType)
	existingMedia, err := h.webhookService.SearchEmbyMedia(mediaTitle, mediaYear, embyType)
	if err != nil {
		log.Printf("[RequestHandler] Emby search timeout/failed (continuing): %v", err)
		// 继续创建请求 - Emby 慢时不阻塞求片
	} else if existingMedia != nil {
		log.Printf("[求片] 媒体库已存在: %s", existingMedia.Title)

		// Build message showing media already exists
		typeIcon := "🎬"
		typeLabel := "电影"
		if existingMedia.Type == "Series" || existingMedia.Type == "Episode" {
			typeIcon = "📺"
			typeLabel = "剧集"
		}

		message := fmt.Sprintf("⚠️ 媒体库中已存在\n\n%s %s", typeIcon, existingMedia.Title)
		if existingMedia.Year > 0 {
			message += fmt.Sprintf(" (%d年)", existingMedia.Year)
		}
		message += fmt.Sprintf("\n🏷️ %s", typeLabel)

		message += "\n\n是否仍要订阅？"

		// Add buttons
		keyboard := &types.TelegramInlineKeyboard{
			InlineKeyboard: [][]types.TelegramInlineKeyboardButton{
				{
					{Text: "❌ 取消", CallbackData: "cancel_request"},
					{Text: "👍 仍要订阅", CallbackData: fmt.Sprintf("force_subscribe:tmdb:%d:type:%s", tmdbID, mediaType)},
				},
			},
		}

		return &callback.Response{
			Text:        message,
			CallbackMsg: "媒体已存在",
			ShowAlert:   false,
			Keyboard:    convertKeyboard(keyboard),
			Edit:        false,
		}, nil
	}

	// Media doesn't exist in library (or check timed out), deduct quota before creating request
	if err := h.quotaService.UseQuota(ctx.UserID, mediaType); err != nil {
		log.Printf("[RequestHandler] Quota check failed for user %d: %v", ctx.UserID, err)

		// Check if it's a quota exceeded error
		if err.Error() == "TV quota exceeded" || err.Error() == "movie quota exceeded" {
			quotaText := h.quotaService.GetQuotaText(ctx.UserID)
			return &callback.Response{
				Text:        fmt.Sprintf("📊 今日配额已用完\n\n%s", quotaText),
				CallbackMsg: "配额已用完",
				ShowAlert:   true,
			}, nil
		}

		// Other errors
		return &callback.Response{
			Text:        "❌ 配额操作失败，请稍后再试",
			CallbackMsg: "操作失败",
			ShowAlert:   true,
		}, nil
	}
	log.Printf("[RequestHandler] Quota deducted for user %d, media type: %s", ctx.UserID, mediaType)

	log.Printf("[RequestHandler] Creating review request...")
	// Create review request
	reviewID := fmt.Sprintf("review_%d_%d", ctx.UserID, time.Now().Unix())

	review := &services.ReviewRequest{
		RequestID:    reviewID,
		TelegramID:   ctx.UserID,
		TelegramName: userName,
		MoviePilotID: moviepilotID,
		TmdbID:       tmdbID,
		MediaTitle:   mediaTitle,
		MediaYear:    mediaYear,
		MediaType:    services.MediaTypeMovie,
		Season:       season,
		PosterPath:   posterPath,
		Overview:     overview,
	}

	if mediaType == "tv" {
		review.MediaType = services.MediaTypeTV
	}

	if err := h.reviewService.CreateRequest(review); err != nil {
		log.Printf("[求片] 创建请求失败: %v", err)
		return &callback.Response{
			Text:        "❌ 提交失败",
			CallbackMsg: "失败",
			ShowAlert:   true,
		}, err
	}

	// Notify admins about the review request
	go h.notifyAdminsForReview(review)

	return &callback.Response{
		Text:        "✅ 求片已提交，等待管理员审核",
		CallbackMsg: "请求已提交",
		ShowAlert:   true,
	}, nil
}


// notifyAdminsForReview notifies all admins about a new review request
func (h *RequestHandler) notifyAdminsForReview(review *services.ReviewRequest) {
	adminIDs := h.adminService.GetAdminIDs()
	if len(adminIDs) == 0 {
		log.Printf("[RequestHandler] No admins to notify")
		return
	}

	log.Printf("[审核] 通知 %d 位管理员: %s, 令牌: %s", len(adminIDs), review.MediaTitle, review.ApproveToken)

	mediaTypeLabel := "电影"
	if review.MediaType == services.MediaTypeTV {
		mediaTypeLabel = "剧集"
	}

	// Build message with poster if available
	message := fmt.Sprintf("🎬 新求片审核\n\n📺 %s", review.MediaTitle)
	if review.MediaYear > 0 {
		message += fmt.Sprintf(" (%d)", review.MediaYear)
	}
	message += fmt.Sprintf("\n🏷️ %s", mediaTypeLabel)

	// Add season info for TV shows
	if review.MediaType == services.MediaTypeTV && review.Season > 0 {
		message += fmt.Sprintf("\n📺 第%d季", review.Season)
	} else if review.MediaType == services.MediaTypeTV {
		message += "\n📺 全季订阅"
	}

	if review.Overview != "" && len(review.Overview) > 0 {
		// Truncate overview to 100 chars
		overview := review.Overview
		if len(overview) > 100 {
			overview = overview[:97] + "..."
		}
		message += fmt.Sprintf("\n\n📝 %s", overview)
	}

	// Add Emby exists warning
	if review.EmbyExists && review.EmbyInfo != nil {
		message += fmt.Sprintf("\n\n⚠️ 媒体库中已存在")
		if review.EmbyInfo.RunTime > 0 {
			minutes := review.EmbyInfo.RunTime / 600000000
			hours := minutes / 60
			mins := minutes % 60
			if hours > 0 {
				message += fmt.Sprintf("\n⏱️ 时长: %d小时%d分", hours, mins)
			} else {
				message += fmt.Sprintf("\n⏱️ 时长: %d分钟", mins)
			}
		}
	}

	message += fmt.Sprintf("\n\n👤 %s (ID: %d)", review.TelegramName, review.TelegramID)

	// Add action buttons with approve token
	// Use token as key to keep CallbackData under 64 bytes: "rv_a:TOKEN" or "rv_r:TOKEN"
	for _, adminID := range adminIDs {
		keyboard := &types.TelegramInlineKeyboard{
			InlineKeyboard: [][]types.TelegramInlineKeyboardButton{
				{
					{Text: "✅ 批准", CallbackData: fmt.Sprintf("rv_a:%s", review.ApproveToken)},
					{Text: "❌ 拒绝", CallbackData: fmt.Sprintf("rv_r:%s", review.ApproveToken)},
				},
			},
		}
		if _, err := h.telegram.SendMessage(adminID, message, "", keyboard); err != nil {
			log.Printf("[审核] 通知管理员失败 %d: %v", adminID, err)
		}
	}
}

// notifyAdmins notifies all admins about a new request (legacy, for direct submission)
func (h *RequestHandler) notifyAdmins(req *services.Request, mediaType string) {
	adminIDs := h.adminService.GetAdminIDs()
	if len(adminIDs) == 0 {
		return
	}

	mediaTypeLabel := "电影"
	if mediaType == "tv" {
		mediaTypeLabel = "剧集"
	}

	title := fmt.Sprintf("媒体 #%d", req.MediaID)
	if req.Media != nil && req.Media.Title != "" {
		title = req.Media.Title
	}

	message := fmt.Sprintf("🎬 新求片请求\n\n%s\n%s\n\n请求ID: %d", title, mediaTypeLabel, req.ID)

	// Add action buttons
	for _, adminID := range adminIDs {
		keyboard := &types.TelegramInlineKeyboard{
			InlineKeyboard: [][]types.TelegramInlineKeyboardButton{
				{
					{Text: "✅ 批准", CallbackData: fmt.Sprintf("admin_approve:id:%d", req.ID)},
					{Text: "❌ 拒绝", CallbackData: fmt.Sprintf("admin_decline:id:%d", req.ID)},
				},
			},
		}
		if _, err := h.telegram.SendMessage(adminID, message, "", keyboard); err != nil {
			log.Printf("[RequestHandler] Failed to notify admin %d: %v", adminID, err)
		}
	}
}

// HandleForceSubscribe handles user choosing to subscribe despite existing media
func (h *RequestHandler) HandleForceSubscribe(ctx *callback.Context) (*callback.Response, error) {
	log.Printf("[HandleForceSubscribe] Called: userID=%d, params=%v", ctx.UserID, ctx.Callback.Params)

	// Defensive checks
	if h.userMapping == nil || h.sessMgr == nil || h.reviewService == nil {
		log.Printf("[HandleForceSubscribe] ERROR: required service is nil!")
		return &callback.Response{
			Text:        "❌ 服务配置错误",
			CallbackMsg: "配置错误",
			ShowAlert:   true,
		}, nil
	}

	tmdbIDStr, hasID := ctx.Callback.Params["tmdb"]
	mediaType, hasType := ctx.Callback.Params["type"]

	if !hasID || !hasType {
		return &callback.Response{
			Text:        "❌ 参数无效",
			CallbackMsg: "错误",
			ShowAlert:   true,
		}, nil
	}

	tmdbID := 0
	fmt.Sscanf(tmdbIDStr, "%d", &tmdbID)

	if tmdbID == 0 {
		return &callback.Response{
			Text:        "❌ 无效的 TMDB ID",
			CallbackMsg: "错误",
			ShowAlert:   true,
		}, nil
	}

	log.Printf("[HandleForceSubscribe] Parsed: tmdbID=%d, mediaType=%s", tmdbID, mediaType)

	// Get MoviePilot user ID
	moviepilotID, exists := h.userMapping.GetMoviePilotUserID(ctx.UserID)
	log.Printf("[HandleForceSubscribe] User mapping: moviepilotID=%d, exists=%v", moviepilotID, exists)
	if !exists || moviepilotID == 0 {
		return &callback.Response{
			Text:        "🔗 请先绑定账号",
			CallbackMsg: "需要绑定账号",
			ShowAlert:   true,
		}, nil
	}

	// Deduct quota for force subscribe (user confirmed they want to subscribe despite existing media)
	if err := h.quotaService.UseQuota(ctx.UserID, mediaType); err != nil {
		log.Printf("[HandleForceSubscribe] Quota check failed for user %d: %v", ctx.UserID, err)

		// Check if it's a quota exceeded error
		if err.Error() == "TV quota exceeded" || err.Error() == "movie quota exceeded" {
			quotaText := h.quotaService.GetQuotaText(ctx.UserID)
			return &callback.Response{
				Text:        fmt.Sprintf("📊 今日配额已用完\n\n%s", quotaText),
				CallbackMsg: "配额已用完",
				ShowAlert:   true,
			}, nil
		}

		// Other errors
		return &callback.Response{
			Text:        "❌ 配额操作失败，请稍后再试",
			CallbackMsg: "操作失败",
			ShowAlert:   true,
		}, nil
	}
	log.Printf("[HandleForceSubscribe] Quota deducted for user %d, media type: %s", ctx.UserID, mediaType)

	// Get session info
	sess := h.sessMgr.Get(ctx.UserID)
	if sess == nil {
		return &callback.Response{
			Text:        "⏰ 会话已过期，请重新搜索",
			CallbackMsg: "会话过期",
			ShowAlert:   true,
		}, nil
	}

	// Get media info from session
	var mediaTitle string
	var mediaYear int
	searchResults, _, _, found := sess.GetSearchResults()
	if found && len(searchResults) > 0 {
		for _, item := range searchResults {
			itemID := item.ID
			if strings.HasPrefix(itemID, "id:") {
				itemID = strings.TrimPrefix(itemID, "id:")
			}
			itemTmdbID := 0
			fmt.Sscanf(itemID, "%d", &itemTmdbID)
			if itemTmdbID == tmdbID {
				mediaTitle = item.Title
				mediaYear = item.Year
				break
			}
		}
	}

	if mediaTitle == "" {
		if cachedItem := sess.GetCachedAIItem(tmdbID); cachedItem != nil {
			mediaTitle = cachedItem.Title
			mediaYear = cachedItem.Year
		}
	}

	if mediaTitle == "" {
		mediaTitle = fmt.Sprintf("TMDB:%d", tmdbID)
	}

	// Get user name from session
	userName := "用户"
	if name, ok := sess.GetString("user_name"); ok {
		userName = name
	}

	// Check Emby library for existing media (for admin info)
	var embyInfo *services.EmbySearchResult
	embyType := services.MediaTypeMovie
	if mediaType == "tv" {
		embyType = services.MediaTypeTV
	}

	existingMedia, err := h.webhookService.SearchEmbyMedia(mediaTitle, mediaYear, embyType)
	if err == nil && existingMedia != nil {
		embyInfo = existingMedia
		log.Printf("[HandleForceSubscribe] Media found in Emby: %s", existingMedia.Title)
	}

	// Create review request
	reviewID := fmt.Sprintf("review_%d_%d", ctx.UserID, time.Now().Unix())

	review := &services.ReviewRequest{
		RequestID:    reviewID,
		TelegramID:   ctx.UserID,
		TelegramName: userName,
		MoviePilotID: moviepilotID,
		TmdbID:       tmdbID,
		MediaTitle:   mediaTitle,
		MediaYear:    mediaYear,
		MediaType:    services.MediaTypeMovie,
		EmbyExists:   embyInfo != nil,
		EmbyInfo:     embyInfo,
	}

	if mediaType == "tv" {
		review.MediaType = services.MediaTypeTV
	}

	if err := h.reviewService.CreateRequest(review); err != nil {
		return &callback.Response{
			Text:        "❌ 提交失败",
			CallbackMsg: "失败",
			ShowAlert:   true,
		}, err
	}

	go h.notifyAdminsForReview(review)

	return &callback.Response{
		Text:        "✅ 求片已提交，等待管理员审核",
		CallbackMsg: "请求已提交",
		ShowAlert:   true,
	}, nil
}

// HandleCancelRequest handles cancel button
func (h *RequestHandler) HandleCancelRequest(ctx *callback.Context) (*callback.Response, error) {
	return &callback.Response{
		Text:        "✖️ 已取消",
		CallbackMsg: "已取消",
		ShowAlert:   false,
		Edit:        true,
	}, nil
}
