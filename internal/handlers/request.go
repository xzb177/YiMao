package handlers

import (
	"fmt"
	"strings"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/richmessage"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/errors"
	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
)

// RequestHandler handles media request callbacks
type RequestHandler struct {
	sessMgr           *session.Manager
	telegram          *services.TelegramClient
	moviepilot        *services.MoviePilotClient
	tmdbClient        *services.TMDBClient
	adminService      *services.AdminService
	webhookService    *services.WebhookService
	userMapping       services.UserMappingStore
	quotaService      *services.QuotaService
	reviewService     *services.ReviewService
	carpoolService    *services.CarpoolService
	submissionService *services.RequestSubmissionService
	enableReview      bool // Enable review system
}

func (h *RequestHandler) SetRequestSubmissionService(s *services.RequestSubmissionService) {
	h.submissionService = s
}
func (h *RequestHandler) NotifyAdminsForReview(r *services.ReviewRequest) { h.notifyAdminsForReview(r) }

func NewRequestHandler(
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	moviepilot *services.MoviePilotClient,
	tmdbClient *services.TMDBClient,
	adminService *services.AdminService,
	webhookService *services.WebhookService,
	userMapping services.UserMappingStore,
	quotaService *services.QuotaService,
	reviewService *services.ReviewService,
) *RequestHandler {
	return &RequestHandler{
		sessMgr:        sessMgr,
		telegram:       telegram,
		moviepilot:     moviepilot,
		tmdbClient:     tmdbClient,
		adminService:   adminService,
		webhookService: webhookService,
		userMapping:    userMapping,
		quotaService:   quotaService,
		reviewService:  reviewService,
		enableReview:   true, // Enable review system by default
	}
}

func (h *RequestHandler) SetCarpoolService(carpool *services.CarpoolService) {
	h.carpoolService = carpool
}

func (h *RequestHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	logger.Info("[RequestHandler] Handle called: userID=%d, params=%v", ctx.UserID, ctx.Callback.Params)

	// Defensive check: ensure required services are available
	if h.userMapping == nil {
		logger.Info("[RequestHandler] ERROR: userMapping service is nil!")
		return &callback.Response{
			Text:        "❌ 服务配置错误",
			CallbackMsg: "配置错误",
			ShowAlert:   true,
		}, nil
	}
	if h.sessMgr == nil {
		logger.Info("[RequestHandler] ERROR: sessMgr service is nil!")
		return &callback.Response{
			Text:        "❌ 服务配置错误",
			CallbackMsg: "配置错误",
			ShowAlert:   true,
		}, nil
	}
	if h.reviewService == nil {
		logger.Info("[RequestHandler] ERROR: reviewService is nil!")
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

	logger.Info("[RequestHandler] Parsed: tmdbID=%d, mediaType=%s, season=%d", tmdbID, mediaType, season)

	// Get MoviePilot user ID from user mapping (with timeout protection)
	logger.Info("[RequestHandler] Getting user mapping for userID=%d...", ctx.UserID)
	moviepilotID, exists := h.userMapping.GetMoviePilotUserID(ctx.UserID)
	logger.Info("[RequestHandler] User mapping result: moviepilotID=%d, exists=%v", moviepilotID, exists)
	if !exists || moviepilotID == 0 {
		// Build link instructions message with button
		msg := services.NewMessageBuilder()
		msg.Bold("🔗 需要绑定账号").Newline()
		msg.Newline()
		msg.Text("求片功能需要绑定账号后才能使用哦").Newline()
		msg.Newline()
		msg.Text("📝 绑定方法：").Newline()
		msg.Code("/link 用户名").Newline()
		msg.Newline()
		msg.Italic("💡 不需要密码，首次会自动创建账号").Newline()

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
	// 如果 session 里没有真实名字，尝试从 Telegram API 获取
	if userName == "用户" {
		if displayName, err := h.telegram.GetUserDisplayName(ctx.UserID); err == nil && displayName != "" {
			userName = displayName
			sess.Set("user_name", displayName) // 缓存到 session
		}
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

	// Try TMDB API if still no title
	if mediaTitle == "" && h.tmdbClient != nil {
		logger.Info("[RequestHandler] Fetching media info from TMDB for ID: %d", tmdbID)
		mediaInfo, err := h.tmdbClient.GetMediaByType(tmdbID, mediaType)
		if err == nil && mediaInfo != nil {
			mediaTitle = mediaInfo.GetTitle()
			if mediaYear == 0 {
				mediaYear = mediaInfo.GetYear()
			}
			if overview == "" {
				overview = mediaInfo.Overview
			}
			if posterPath == "" {
				posterPath = mediaInfo.PosterPath
			}
			logger.Info("[RequestHandler] TMDB API returned: %s (%d)", mediaTitle, mediaYear)
		} else {
			logger.Info("[RequestHandler] TMDB API failed: %v", err)
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

	logger.Info("[RequestHandler] Checking Emby for: %s (%d) %s (5s timeout)", mediaTitle, mediaYear, embyType)
	existingMedia, err := h.webhookService.SearchEmbyMedia(mediaTitle, mediaYear, embyType)
	if err != nil {
		logger.Info("[RequestHandler] Emby search timeout/failed (continuing): %v", err)
		// 继续创建请求 - Emby 慢时不阻塞求片
	} else if existingMedia != nil {
		logger.Info("[求片] 媒体库已存在: %s", existingMedia.Title)

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

	// Check duplicate active requests before quota deduction
	reqMediaType := services.MediaTypeMovie
	if mediaType == "tv" {
		reqMediaType = services.MediaTypeTV
	}
	if existingReview, dup := h.reviewService.HasActiveSimilarRequest(ctx.UserID, tmdbID, reqMediaType, season); dup {
		statusText := "处理中"
		if existingReview.Status == "approved" {
			statusText = "已通过审核"
		}
		return &callback.Response{
			Text:        fmt.Sprintf("⚠️ 检测到重复请求\n\n《%s》已存在一条记录（状态：%s）\n请在“我的请求”查看进度。", existingReview.MediaTitle, statusText),
			CallbackMsg: "请勿重复提交",
			ShowAlert:   true,
		}, nil
	}
	if existingReview, people := h.reviewService.FindActiveSimilarRequest(tmdbID, reqMediaType, season); existingReview != nil {
		if h.carpoolService != nil {
			people = h.carpoolService.Add(tmdbID, mediaType, ctx.UserID)
		}
		if people < 1 {
			people = 1
		}
		return &callback.Response{
			Text:        fmt.Sprintf("🙋 已加入拼车\n\n《%s》已有用户提交求片，当前共 %d 人想看。\n审核和下载进度会跟随原请求推进，不重复扣配额。", existingReview.MediaTitle, people),
			CallbackMsg: "已加入拼车",
			ShowAlert:   true,
		}, nil
	}

	// Media doesn't exist in library (or check timed out), deduct quota before creating request
	if err := h.quotaService.UseQuota(ctx.UserID, mediaType); err != nil {
		logger.Info("[RequestHandler] Quota check failed for user %d: %v", ctx.UserID, err)

		// Check if it's a quota exceeded error
		if err.Error() == "TV quota exceeded" || err.Error() == "movie quota exceeded" {
			quotaText := h.quotaService.GetQuotaText(ctx.UserID)
			return &callback.Response{
				Text:        fmt.Sprintf("今天的求片次数用完啦～\n\n%s\n\n🎬 好好享受已入库的片子吧\n🕛 配额明天 00:00 刷新", quotaText),
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
	logger.Info("[RequestHandler] Quota deducted for user %d, media type: %s", ctx.UserID, mediaType)

	logger.Info("[RequestHandler] Creating review request...")
	// Create review request
	reviewID := fmt.Sprintf("review_%d_%d", ctx.UserID, time.Now().UnixNano())

	quotaMediaType := mediaType // Save for rollback
	_ = quotaMediaType

	review := &services.ReviewRequest{
		RequestID:     reviewID,
		TelegramID:    ctx.UserID,
		TelegramName:  userName,
		MoviePilotID:  moviepilotID,
		TmdbID:        tmdbID,
		MediaTitle:    mediaTitle,
		MediaYear:     mediaYear,
		MediaType:     services.MediaTypeMovie,
		Season:        season,
		PosterPath:    posterPath,
		Overview:      overview,
		RequestOrigin: "normal",
		QuotaCost:     map[bool]int{true: 3, false: 1}[mediaType == "tv"],
	}

	if mediaType == "tv" {
		review.MediaType = services.MediaTypeTV
	}

	if err := h.reviewService.CreateRequest(review); err != nil {
		logger.Info("[求片] 创建请求失败: %v", err)
		h.quotaService.RestoreQuota(ctx.UserID, quotaMediaType)
		return &callback.Response{
			Text:        "❌ 提交失败",
			CallbackMsg: "失败",
			ShowAlert:   true,
		}, err
	}

	// Notify admins about the review request
	go h.notifyAdminsForReview(review)

	// 发送求片回执消息
	receiptMsg := fmt.Sprintf(
		"✅ 求片已提交\n\n🎬 《%s}", review.MediaTitle)
	if review.MediaYear > 0 {
		receiptMsg += fmt.Sprintf(" (%d)", review.MediaYear)
	}
	receiptMsg += fmt.Sprintf(
		"\n📋 状态：⏳ 等待管理员审核\n\n审核通过后会自动下载，完成时会通知你")
	kb := services.NewKeyboardBuilder()
	kb.AddButton("📋 查看进度", "requests")
	kb.AddButton("⬅️ 继续搜片", "start")
	h.telegram.SendMessage(ctx.ChatID, receiptMsg, "", kb.Build())

	return &callback.Response{
		Text:        "✅ 求片已提交",
		CallbackMsg: "请求已提交",
		ShowAlert:   false,
	}, nil
}

// notifyAdminsForReview notifies all admins about a new review request
func (h *RequestHandler) notifyAdminsForReview(review *services.ReviewRequest) {
	adminIDs := h.adminService.GetAdminIDs()
	if len(adminIDs) == 0 {
		logger.Info("[RequestHandler] No admins to notify")
		return
	}

	logger.Info("[审核] 通知 %d 位管理员: %s, 令牌: %s", len(adminIDs), review.MediaTitle, review.ApproveToken)

	mediaTypeLabel := "电影"
	if review.MediaType == services.MediaTypeTV {
		mediaTypeLabel = "剧集"
	}

	mediaIcon := "🎬"
	if review.MediaType == services.MediaTypeTV {
		mediaIcon = "📺"
	}

	seasonText := ""
	if review.MediaType == services.MediaTypeTV && review.Season > 0 {
		seasonText = fmt.Sprintf("第%d季", review.Season)
	} else if review.MediaType == services.MediaTypeTV {
		seasonText = "全季"
	}

	var embyHours, embyMins int
	if review.EmbyExists && review.EmbyInfo != nil && review.EmbyInfo.RunTime > 0 {
		minutes := int(review.EmbyInfo.RunTime / 600000000)
		embyHours = minutes / 60
		embyMins = minutes % 60
	}

	notifyCard := richmessage.BuildReviewNotifyCard(richmessage.ReviewNotifyData{
		Title:      review.MediaTitle,
		Year:       review.MediaYear,
		MediaType:  mediaTypeLabel,
		MediaIcon:  mediaIcon,
		Season:     review.Season,
		SeasonText: seasonText,
		Overview:   review.Overview,
		UserName:   review.TelegramName,
		UserID:     review.TelegramID,
		EmbyExists: review.EmbyExists,
		EmbyHours:  embyHours,
		EmbyMins:   embyMins,
	})

	// Add action buttons with approve token
	for _, adminID := range adminIDs {
		keyboard := &types.TelegramInlineKeyboard{
			InlineKeyboard: [][]types.TelegramInlineKeyboardButton{
				{
					{Text: "✅ 批准", CallbackData: fmt.Sprintf("rv_a:%s", review.ApproveToken)},
					{Text: "❌ 拒绝", CallbackData: fmt.Sprintf("rv_r:%s", review.ApproveToken)},
				},
			},
		}
		if _, err := h.telegram.SendRichMessage(adminID, notifyCard.Markdown, keyboard); err != nil {
			logger.Info("[审核] Rich Message 通知管理员 %d 失败，回退到普通消息: %v", adminID, err)
			plainText := fmt.Sprintf("📋 新的求片审核\n\n🎬 《%s}", review.MediaTitle)
			if review.MediaYear > 0 {
				plainText += fmt.Sprintf(" (%d)", review.MediaYear)
			}
			plainText += fmt.Sprintf("\n👤 %s (ID: %d)", review.TelegramName, review.TelegramID)
			if review.MediaType == services.MediaTypeTV {
				plainText += "\n📺 剧集"
			} else {
				plainText += "\n🎬 电影"
			}
			if review.Season > 0 {
				plainText += fmt.Sprintf(" · 第%d季", review.Season)
			}
			plainText += "\n查看详情后决定批准或拒绝。"
			if _, msgErr := h.telegram.SendMessage(adminID, plainText, "", keyboard); msgErr != nil {
				logger.Info("[审核] 普通消息通知管理员 %d 也失败: %v", adminID, msgErr)
			}
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
	mediaIcon := "🎬"
	if mediaType == "tv" {
		mediaTypeLabel = "剧集"
		mediaIcon = "📺"
	}

	title := fmt.Sprintf("媒体 #%d", req.MediaID)
	if req.Media != nil && req.Media.Title != "" {
		title = req.Media.Title
	}

	notifyCard := richmessage.BuildReviewNotifyCard(richmessage.ReviewNotifyData{
		Title:     title,
		MediaType: mediaTypeLabel,
		MediaIcon: mediaIcon,
	})

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
		if _, err := h.telegram.SendRichMessage(adminID, notifyCard.Markdown, keyboard); err != nil {
			logger.Info("[RequestHandler] Rich Message 通知管理员 %d 失败: %v", adminID, err)
			plainText := fmt.Sprintf("📋 新的求片审核\n\n🎬 《%s}", title)
			if _, msgErr := h.telegram.SendMessage(adminID, plainText, "", keyboard); msgErr != nil {
				logger.Info("[RequestHandler] 普通消息通知管理员 %d 也失败: %v", adminID, msgErr)
			}
		}
	}
}

// HandleForceSubscribe handles user choosing to subscribe despite existing media
func (h *RequestHandler) HandleForceSubscribe(ctx *callback.Context) (*callback.Response, error) {
	logger.Info("[HandleForceSubscribe] Called: userID=%d, params=%v", ctx.UserID, ctx.Callback.Params)

	// Defensive checks
	if h.userMapping == nil || h.sessMgr == nil || h.reviewService == nil {
		logger.Info("[HandleForceSubscribe] ERROR: required service is nil!")
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

	logger.Info("[HandleForceSubscribe] Parsed: tmdbID=%d, mediaType=%s", tmdbID, mediaType)

	// Get MoviePilot user ID
	moviepilotID, exists := h.userMapping.GetMoviePilotUserID(ctx.UserID)
	logger.Info("[HandleForceSubscribe] User mapping: moviepilotID=%d, exists=%v", moviepilotID, exists)
	if !exists || moviepilotID == 0 {
		return &callback.Response{
			Text:        "🔗 请先绑定账号",
			CallbackMsg: "需要绑定账号",
			ShowAlert:   true,
		}, nil
	}

	// Quota is consumed only after session/media/duplicate validation below.

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

	// Try TMDB API if still no title
	if mediaTitle == "" && h.tmdbClient != nil {
		logger.Info("[HandleForceSubscribe] Fetching media info from TMDB for ID: %d", tmdbID)
		mediaInfo, err := h.tmdbClient.GetMediaByType(tmdbID, mediaType)
		if err == nil && mediaInfo != nil {
			mediaTitle = mediaInfo.GetTitle()
			if mediaYear == 0 {
				mediaYear = mediaInfo.GetYear()
			}
			logger.Info("[HandleForceSubscribe] TMDB API returned: %s (%d)", mediaTitle, mediaYear)
		} else {
			logger.Info("[HandleForceSubscribe] TMDB API failed: %v", err)
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
	// 如果 session 里没有真实名字，尝试从 Telegram API 获取
	if userName == "用户" {
		if displayName, err := h.telegram.GetUserDisplayName(ctx.UserID); err == nil && displayName != "" {
			userName = displayName
			sess.Set("user_name", displayName)
		}
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
		logger.Info("[HandleForceSubscribe] Media found in Emby: %s", existingMedia.Title)
	}

	// Prevent duplicate active requests before creating review
	reqMediaType := services.MediaTypeMovie
	if mediaType == "tv" {
		reqMediaType = services.MediaTypeTV
	}
	if existingReview, dup := h.reviewService.HasActiveSimilarRequest(ctx.UserID, tmdbID, reqMediaType, 0); dup {
		statusText := "处理中"
		if existingReview.Status == "approved" {
			statusText = "已通过审核"
		}
		return &callback.Response{
			Text:        fmt.Sprintf("⚠️ 你已提交过该内容\n\n《%s》当前状态：%s\n请到“我的请求”查看。", existingReview.MediaTitle, statusText),
			CallbackMsg: "请勿重复提交",
			ShowAlert:   true,
		}, nil
	}

	if err := h.quotaService.UseQuota(ctx.UserID, mediaType); err != nil {
		if err.Error() == "TV quota exceeded" || err.Error() == "movie quota exceeded" {
			return &callback.Response{Text: fmt.Sprintf("今天的求片次数用完啦～\n\n%s", h.quotaService.GetQuotaText(ctx.UserID)), CallbackMsg: "配额已用完", ShowAlert: true}, nil
		}
		return &callback.Response{Text: "❌ 配额操作失败，请稍后再试", CallbackMsg: "操作失败", ShowAlert: true}, nil
	}

	// Create review request
	reviewID := fmt.Sprintf("review_%d_%d", ctx.UserID, time.Now().UnixNano())

	review := &services.ReviewRequest{
		RequestID:     reviewID,
		TelegramID:    ctx.UserID,
		TelegramName:  userName,
		MoviePilotID:  moviepilotID,
		TmdbID:        tmdbID,
		MediaTitle:    mediaTitle,
		MediaYear:     mediaYear,
		MediaType:     services.MediaTypeMovie,
		EmbyExists:    embyInfo != nil,
		EmbyInfo:      embyInfo,
		RequestOrigin: "normal",
		QuotaCost:     map[bool]int{true: 3, false: 1}[mediaType == "tv"],
	}

	if mediaType == "tv" {
		review.MediaType = services.MediaTypeTV
	}

	if err := h.reviewService.CreateRequest(review); err != nil {
		h.quotaService.RestoreQuota(ctx.UserID, mediaType)
		return &callback.Response{
			Text:        "❌ 提交失败",
			CallbackMsg: "失败",
			ShowAlert:   true,
		}, err
	}

	go h.notifyAdminsForReview(review)

	// 发送求片回执消息
	receiptMsg := fmt.Sprintf(
		"✅ 求片已提交\n\n🎬 《%s}", review.MediaTitle)
	if review.MediaYear > 0 {
		receiptMsg += fmt.Sprintf(" (%d)", review.MediaYear)
	}
	if review.MediaType == services.MediaTypeTV {
		receiptMsg += "\n📺 剧集"
	}
	receiptMsg += "\n📋 状态：⏳ 等待管理员审核\n\n审核通过后会自动下载，完成时会通知你"
	kb := services.NewKeyboardBuilder()
	kb.AddButton("📋 查看进度", "requests")
	kb.AddButton("⬅️ 继续搜片", "start")
	h.telegram.SendMessage(ctx.ChatID, receiptMsg, "", kb.Build())

	return &callback.Response{
		Text:        "✅ 求片已提交",
		CallbackMsg: "请求已提交",
		ShowAlert:   false,
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
