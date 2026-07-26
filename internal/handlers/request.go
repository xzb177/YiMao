package handlers

import (
	"fmt"
	"strconv"
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
	submissionService requestSubmitter
	fulfillmentStats  *services.FulfillmentStatsService
	seriesHandler     *SeriesHandler
	enableReview      bool // Enable review system
}

type requestSubmitter interface {
	SubmitResult(services.RequestSubmission) (services.SubmissionResult, error)
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

// SetFulfillmentStats 注入履约统计服务（求片回执展示 ETA 参考）。
func (h *RequestHandler) SetFulfillmentStats(fs *services.FulfillmentStatsService) {
	h.fulfillmentStats = fs
}

// SetSeriesHandler 注入系列补全处理器（电影回执异步发现系列缺片）。
func (h *RequestHandler) SetSeriesHandler(sh *SeriesHandler) {
	h.seriesHandler = sh
}

func (h *RequestHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	logger.Info("[RequestHandler] Handle called: userID=%d, params=%v", ctx.UserID, ctx.Callback.Params)

	// Defensive check: ensure required services are available
	if h.userMapping == nil {
		logger.Info("[RequestHandler] ERROR: userMapping service is nil!")
		return &callback.Response{
			Text:        "❌ 现在暂时不能求片，请稍后再试",
			CallbackMsg: "暂时不可用",
			ShowAlert:   true,
		}, nil
	}
	if h.sessMgr == nil {
		logger.Info("[RequestHandler] ERROR: sessMgr service is nil!")
		return &callback.Response{
			Text:        "❌ 现在暂时不能求片，请稍后再试",
			CallbackMsg: "暂时不可用",
			ShowAlert:   true,
		}, nil
	}
	if h.reviewService == nil {
		logger.Info("[RequestHandler] ERROR: reviewService is nil!")
		return &callback.Response{
			Text:        "❌ 现在暂时不能求片，请稍后再试",
			CallbackMsg: "暂时不可用",
			ShowAlert:   true,
		}, nil
	}
	if h.submissionService == nil {
		return serviceConfigurationError(), nil
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
		msg.Text("绑定方法：").Newline()
		msg.Code("/link 用户名 密码").Newline()
		msg.Newline()
		msg.Italic("没有账号也没关系，首次绑定会自动创建").Newline()

		kb := services.NewKeyboardBuilder()
		kb.AddButton("🔗 立即绑定", "link")
		kb.AddButton("🏠 主菜单", "start")

		return &callback.Response{
			Text:     msg.Build(),
			Edit:     true,
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	// Get media info from session for better display.
	// 会话过期不再是死路：GetOrCreate 重建空会话，媒体信息由下方
	// 搜索缓存 → AI 缓存 → TMDB API 三级兜底补齐，旧消息按钮永远可点。
	sess := h.sessMgr.GetOrCreate(ctx.UserID)

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

	// Full-show requests are materially broader than a single season. Require one
	// explicit confirmation before the existing quota/review/submission flow.
	if mediaType == "tv" && season == 0 && ctx.Callback.Params["confirm"] != "1" {
		message := fmt.Sprintf("📺 确认求全部季度\n\n《%s》", mediaTitle)
		if mediaYear > 0 {
			message += fmt.Sprintf(" (%d)", mediaYear)
		}
		message += "\n\n这会提交整部剧的全部季度，不是单独一季。"
		keyboard := &types.TelegramInlineKeyboard{InlineKeyboard: [][]types.TelegramInlineKeyboardButton{
			{{Text: "✅ 确认求全部季度", CallbackData: fmt.Sprintf("request:id:%d:type:tv:season:0:confirm:1", tmdbID)}},
			{{Text: "⬅️ 返回详情", CallbackData: fmt.Sprintf("detail:id:%d:type:tv:source:confirm", tmdbID)}},
		}}
		return &callback.Response{
			Text:     message,
			Edit:     true,
			Keyboard: convertKeyboard(keyboard),
		}, nil
	}

	// Check if media already exists in Emby library
	// 使用较短的超时 (5秒) 避免阻塞求片流程
	embyType := services.MediaTypeMovie
	if mediaType == "tv" {
		embyType = services.MediaTypeTV
	}

	logger.Info("[RequestHandler] Checking Emby by exact TMDB ID: %d %s (5s timeout)", tmdbID, embyType)
	existingMedia, err := h.webhookService.SearchEmbyMediaByTMDB(tmdbID, embyType)
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
					{Text: "👍 仍要订阅", CallbackData: fmt.Sprintf("force_subscribe:tmdb:%d:type:%s:season:%d", tmdbID, mediaType, season)},
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

	reqMediaType := services.MediaTypeMovie
	if mediaType == "tv" {
		reqMediaType = services.MediaTypeTV
	}
	result, submitErr := h.submissionService.SubmitResult(services.RequestSubmission{
		TelegramID: ctx.UserID, TelegramName: userName, TmdbID: tmdbID,
		MediaTitle: mediaTitle, MediaYear: mediaYear, MediaType: reqMediaType, Season: season,
		PosterPath: posterPath, Overview: overview, Origin: "normal", UseQuota: true,
	})
	if submitErr != nil && result.Status != services.SubmissionQuotaExceeded {
		logger.Info("[求片] 提交请求失败: %v", submitErr)
		return operationFailedResponse(), nil
	}
	if result.Status != services.SubmissionCreated {
		return h.mapSubmissionResult(result, ctx.UserID, tmdbID, mediaType, false), nil
	}
	review := result.Review
	if review == nil {
		return operationFailedResponse(), nil
	}

	// 发送求片回执消息
	receiptMsg := fmt.Sprintf(
		"✅ 求片已提交\n\n🎬 《%s》", review.MediaTitle)
	if review.MediaYear > 0 {
		receiptMsg += fmt.Sprintf(" (%d)", review.MediaYear)
	}
	receiptMsg += fmt.Sprintf(
		"\n📋 状态：⏳ 等待管理员审核\n\n审核通过后会自动下载，完成时会通知你")
	if h.fulfillmentStats != nil {
		if eta := h.fulfillmentStats.EstimateText(string(review.MediaType), review.MediaYear); eta != "" {
			receiptMsg += "\n" + eta
		}
	}
	kb := services.NewKeyboardBuilder()
	kb.AddButton("📊 求片进度", "requests")
	kb.AddButton("🏠 主菜单", "start")
	// 回执先返回；系列检查走后台，不让 TMDB/Emby 查询拖慢按钮响应。
	if review.MediaType == services.MediaTypeMovie {
		go h.sendSeriesSuggestion(ctx.UserID, tmdbID)
	}
	return &callback.Response{
		Text:        receiptMsg,
		CallbackMsg: "请求已提交",
		ShowAlert:   false,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
}

// sendSeriesSuggestion 异步检查电影系列缺片，不阻塞求片回执。
// 系列建议属于个人内容，只发送申请人私聊；私聊不可达时静默失败，
// 不允许回退到公开群聊。
func (h *RequestHandler) sendSeriesSuggestion(userID int64, tmdbID int) {
	if h.seriesHandler == nil || h.sessMgr == nil || userID <= 0 || tmdbID <= 0 {
		return
	}
	sess := h.sessMgr.GetOrCreate(userID)
	key := fmt.Sprintf("series_suggested_%d", tmdbID)
	if _, already := sess.GetString(key); already {
		return
	}
	text, button := h.seriesHandler.BuildSuggestion(tmdbID)
	if text == "" || button == nil {
		return
	}
	kb := services.NewKeyboardBuilder()
	kb.AddButton(button.Text, button.CallbackData)
	kb.NewRow()
	kb.AddButton("📊 求片进度", "requests")
	_, err := h.telegram.SendMessage(userID, text, "HTML", kb.Build())
	if err != nil {
		logger.Info("[RequestHandler] 系列缺片提示发送失败: %v", err)
		return
	}
	sess.Set(key, time.Now().UTC().Format(time.RFC3339))
	logger.Info("[RequestHandler] 已向用户 %d 发送系列缺片提示", userID)
}

// notifyAdminsForReview notifies all admins about a new review request
func (h *RequestHandler) notifyAdminsForReview(review *services.ReviewRequest) {
	adminIDs := h.adminService.GetAdminIDs()
	if len(adminIDs) == 0 {
		logger.Info("[RequestHandler] No admins to notify")
		return
	}

	logger.Info("[审核] 通知 %d 位管理员: %s", len(adminIDs), review.MediaTitle)

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

	notifyData := richmessage.ReviewNotifyData{
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
	}
	notifyCard := richmessage.BuildReviewNotifyCard(notifyData)
	if review.NormalizedBusinessType() == services.BusinessTypeWash {
		notifyCard = richmessage.BuildWashReviewNotifyCard(notifyData)
	}

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
			plainTitle := "📋 新的求片审核"
			if review.NormalizedBusinessType() == services.BusinessTypeWash {
				plainTitle = "♻️ 新洗版工单"
			}
			plainText := fmt.Sprintf("%s\n\n🎬 《%s》", plainTitle, review.MediaTitle)
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
			if review.NormalizedBusinessType() == services.BusinessTypeWash {
				plainText += "\n批准或拒绝前请核对目标；处理期间必须保留现有版本。"
			} else {
				plainText += "\n查看详情后决定批准或拒绝。"
			}
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
			plainText := fmt.Sprintf("📋 新的求片审核\n\n🎬 《%s》", title)
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
	if h.userMapping == nil || h.sessMgr == nil || h.reviewService == nil || h.submissionService == nil {
		logger.Info("[HandleForceSubscribe] ERROR: required service is nil!")
		return &callback.Response{
			Text:        "❌ 现在暂时不能求片，请稍后再试",
			CallbackMsg: "暂时不可用",
			ShowAlert:   true,
		}, nil
	}

	tmdbIDStr, hasID := ctx.Callback.Params["tmdb"]
	mediaType, hasType := ctx.Callback.Params["type"]

	if !hasID || !hasType {
		return &callback.Response{
			Text:        "❌ 这次操作没有成功，请重新打开详情页",
			CallbackMsg: "操作失败",
			ShowAlert:   true,
		}, nil
	}

	tmdbID := 0
	fmt.Sscanf(tmdbIDStr, "%d", &tmdbID)
	season := 0
	if seasonStr, ok := ctx.Callback.Params["season"]; ok {
		parsedSeason, err := strconv.Atoi(seasonStr)
		if err != nil || parsedSeason < 0 {
			return &callback.Response{Text: "❌ 没找到这一季，请重新选择", CallbackMsg: "操作失败", ShowAlert: true}, nil
		}
		season = parsedSeason
	}

	if tmdbID == 0 {
		return &callback.Response{
			Text:        "❌ 这部片的信息已失效，请重新搜索",
			CallbackMsg: "操作失败",
			ShowAlert:   true,
		}, nil
	}

	logger.Info("[HandleForceSubscribe] Parsed: tmdbID=%d, mediaType=%s", tmdbID, mediaType)

	// Get MoviePilot user ID
	moviepilotID, exists := h.userMapping.GetMoviePilotUserID(ctx.UserID)
	logger.Info("[HandleForceSubscribe] User mapping: moviepilotID=%d, exists=%v", moviepilotID, exists)
	if !exists || moviepilotID == 0 {
		return bindGuideResponse(), nil
	}

	// Quota is consumed only after session/media/duplicate validation below.

	// 会话过期不再拦路：重建空会话，标题走 TMDB 兜底（tmdbID 就在回调参数里）。
	sess := h.sessMgr.GetOrCreate(ctx.UserID)

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

	existingMedia, err := h.webhookService.SearchEmbyMediaByTMDB(tmdbID, embyType)
	if err == nil && existingMedia != nil {
		embyInfo = existingMedia
		logger.Info("[HandleForceSubscribe] Media found in Emby: %s", existingMedia.Title)
	}

	reqMediaType := services.MediaTypeMovie
	if mediaType == "tv" {
		reqMediaType = services.MediaTypeTV
	}
	result, submitErr := h.submissionService.SubmitResult(services.RequestSubmission{
		TelegramID: ctx.UserID, TelegramName: userName, TmdbID: tmdbID,
		MediaTitle: mediaTitle, MediaYear: mediaYear, MediaType: reqMediaType, Season: season,
		EmbyInfo: embyInfo, Origin: "normal", UseQuota: true,
	})
	if submitErr != nil && result.Status != services.SubmissionQuotaExceeded {
		logger.Info("[HandleForceSubscribe] Submit failed: %v", submitErr)
		return operationFailedResponse(), nil
	}
	if result.Status != services.SubmissionCreated {
		return h.mapSubmissionResult(result, ctx.UserID, tmdbID, mediaType, true), nil
	}
	review := result.Review
	if review == nil {
		return operationFailedResponse(), nil
	}

	// 发送求片回执消息
	receiptMsg := fmt.Sprintf(
		"✅ 求片已提交\n\n🎬 《%s》", review.MediaTitle)
	if review.MediaYear > 0 {
		receiptMsg += fmt.Sprintf(" (%d)", review.MediaYear)
	}
	if review.MediaType == services.MediaTypeTV {
		receiptMsg += "\n📺 剧集"
	}
	receiptMsg += "\n📋 状态：⏳ 等待管理员审核\n\n审核通过后会自动下载，完成时会通知你"
	if h.fulfillmentStats != nil {
		if eta := h.fulfillmentStats.EstimateText(string(review.MediaType), review.MediaYear); eta != "" {
			receiptMsg += "\n" + eta
		}
	}
	kb := services.NewKeyboardBuilder()
	kb.AddButton("📊 求片进度", "requests")
	kb.AddButton("🏠 主菜单", "start")
	return &callback.Response{
		Text:        receiptMsg,
		CallbackMsg: "请求已提交",
		ShowAlert:   false,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
}

func serviceConfigurationError() *callback.Response {
	return &callback.Response{Text: "❌ 现在暂时不能求片，请稍后再试", CallbackMsg: "暂时不可用", ShowAlert: true}
}

func operationFailedResponse() *callback.Response {
	return &callback.Response{Text: "❌ 求片没有提交成功，请稍后再试", CallbackMsg: "操作失败", ShowAlert: true}
}

func (h *RequestHandler) mapSubmissionResult(result services.SubmissionResult, userID int64, tmdbID int, mediaType string, force bool) *callback.Response {
	title, statusText := "", "处理中"
	if result.Review != nil {
		title = result.Review.MediaTitle
		if result.Review.Status == "approved" {
			statusText = "已通过审核"
		}
	}
	// 终态反馈统一带行动按钮：alert 弹窗几秒就消失，
	// 消息里必须留下一条能点的路（进度/绑定/许愿），不让用户自己找入口。
	progressKb := services.NewKeyboardBuilder()
	progressKb.AddButton("📊 查看进度", "requests")
	progressKb.AddButton("🏠 主菜单", "start")
	switch result.Status {
	case services.SubmissionDuplicateOwn:
		if force {
			return &callback.Response{Text: fmt.Sprintf("⚠️ 你已提交过该内容\n\n《%s》当前状态：%s", title, statusText), CallbackMsg: "请勿重复提交", ShowAlert: true, Keyboard: convertKeyboard(progressKb.Build())}
		}
		return &callback.Response{Text: fmt.Sprintf("⚠️ 检测到重复请求\n\n《%s》已存在一条记录（状态：%s）", title, statusText), CallbackMsg: "请勿重复提交", ShowAlert: true, Keyboard: convertKeyboard(progressKb.Build())}
	case services.SubmissionDuplicateOther:
		people := 1
		if h.carpoolService != nil {
			people = h.carpoolService.Add(tmdbID, mediaType, userID)
		}
		if people < 1 {
			people = 1
		}
		return &callback.Response{Text: fmt.Sprintf("🙋 已加入拼车\n\n《%s》已有用户提交求片，当前共 %d 人想看。\n审核和下载进度会跟随原请求推进，不重复扣配额。", title, people), CallbackMsg: "已加入拼车", ShowAlert: true, Keyboard: convertKeyboard(progressKb.Build())}
	case services.SubmissionNotBound:
		return bindGuideResponse()
	case services.SubmissionQuotaExceeded:
		quotaText := ""
		if h.quotaService != nil {
			quotaText = h.quotaService.GetQuotaText(userID)
		}
		// 配额用尽给出路：许愿不扣配额，出源后自动提醒。
		quotaKb := services.NewKeyboardBuilder()
		quotaKb.AddButton("✨ 去许愿池", "wish")
		quotaKb.AddButton("🏠 主菜单", "start")
		return &callback.Response{Text: fmt.Sprintf("今天的求片次数用完啦\n\n%s\n\n明天 00:00 会恢复。等不及的话可以先把片子放进许愿池，不占配额，出源后会提醒你。", quotaText), CallbackMsg: "今日求片次数已用完", ShowAlert: true, Keyboard: convertKeyboard(quotaKb.Build())}
	default:
		return operationFailedResponse()
	}
}

// bindGuideResponse 统一的「未绑定」引导：给按钮、给命令示例，
// 不再只丢一句"请先绑定账号"让用户自己摸索。
func bindGuideResponse() *callback.Response {
	msg := services.NewMessageBuilder()
	msg.Bold("🔗 需要绑定账号").Newline()
	msg.Newline()
	msg.Text("求片功能需要绑定账号后才能使用哦").Newline()
	msg.Newline()
	msg.Text("绑定方法：").Newline()
	msg.Code("/link 用户名 密码").Newline()
	msg.Newline()
	msg.Italic("没有账号也没关系，首次绑定会自动创建").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🔗 立即绑定", "link")
	kb.AddButton("🏠 主菜单", "start")
	return &callback.Response{
		Text:        msg.Build(),
		CallbackMsg: "需要绑定账号",
		Keyboard:    convertKeyboard(kb.Build()),
	}
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
