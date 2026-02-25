package handlers

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/config"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
	"emby-telegram-bot/pkg/errors"
	"emby-telegram-bot/pkg/types"
)

// Message formatting constants
const (
	MaxOverviewLength = 300
	MaxDisplayCount  = 8
	MaxSeasonsDisplay = 4
)

// extractSeasons extracts season list from MediaInfo
func extractSeasons(info *services.MediaInfo) []session.Season {
	// Try SeasonInfo first (preferred)
	if len(info.SeasonInfo) > 0 {
		seasons := make([]session.Season, len(info.SeasonInfo))
		for i, s := range info.SeasonInfo {
			seasons[i] = session.Season{
				SeasonNumber: s.SeasonNumber,
				EpisodeCount: s.EpisodeCount,
				Name:         s.Name,
			}
		}
		return seasons
	}

	// Try Seasons as map
	if info.Seasons != nil {
		if seasonsMap, ok := info.Seasons.(map[string]interface{}); ok {
			seasons := make([]session.Season, 0, len(seasonsMap))
			for key := range seasonsMap {
				var seasonNum int
				fmt.Sscanf(key, "%d", &seasonNum)
				if seasonNum > 0 {
					seasons = append(seasons, session.Season{
						SeasonNumber: seasonNum,
						EpisodeCount: 0,
						Name:         fmt.Sprintf("第%d季", seasonNum),
					})
				}
			}
			return seasons
		}
	}

	return nil
}

// StartHandler handles start menu callbacks
type StartHandler struct {
	cfg         *config.Config
	sessMgr     *session.Manager
	telegram    *services.TelegramClient
	moviepilot  *services.MoviePilotClient
	adminService *services.AdminService
}

func NewStartHandler(
	cfg *config.Config,
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	moviepilot *services.MoviePilotClient,
) *StartHandler {
	return &StartHandler{
		cfg:        cfg,
		sessMgr:    sessMgr,
		telegram:   telegram,
		moviepilot: moviepilot,
	}
}

// SetAdminService sets the admin service
func (h *StartHandler) SetAdminService(adminSvc *services.AdminService) {
	h.adminService = adminSvc
}

func (h *StartHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	action := ctx.Callback.Action

	switch action {
	case callback.ActionStart:
		return h.HandleStart(ctx)
	case callback.ActionSearch:
		return h.HandleSearch(ctx)
	case callback.ActionAI:
		return h.HandleAI(ctx)
	case callback.ActionHot:
		return h.HandleHot(ctx)
	case callback.ActionNew:
		return h.HandleNew(ctx)
	default:
		return nil, errors.CallbackInvalid(fmt.Sprintf("unknown start action: %s", action))
	}
}

func (h *StartHandler) HandleStart(ctx *callback.Context) (*callback.Response, error) {
	msg := services.NewMessageBuilder()
	msg.Bold("👋 欢迎使用云海影视助手").Newline()
	msg.Newline()
	msg.Text("🔍 搜索影片 · 快速查找心仪内容").Newline()

	// Only show AI recommendation in private chats
	isPrivateChat := ctx.ChatType == "private"
	if isPrivateChat {
		msg.Text("🎬 精选推荐 · 发现优质内容").Newline()
	}

	msg.Text("📋 我的请求 · 跟踪求片进度").Newline()
	msg.Text("🐛 我的反馈 · 查看问题反馈").Newline()
	msg.Text("🔗 账号绑定 · 同步观影记录").Newline()
	msg.Newline()
	msg.Italic("💡 点击下方按钮开始探索").Newline()

	// Check if user is admin to add admin menu button
	isAdmin := false
	if h.adminService != nil {
		isAdmin = h.adminService.IsAdmin(ctx.UserID)
	}

	keyboard := services.BuildStartKeyboardWithOptions(isAdmin, isPrivateChat)

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(keyboard),
	}, nil
}

func (h *StartHandler) HandleSearch(ctx *callback.Context) (*callback.Response, error) {
	msg := services.NewMessageBuilder()
	msg.Bold("🔍 搜索影片").Newline()
	msg.Newline()
	msg.Text("请输入影片名称，支持中文/英文").Newline()
	msg.Newline()
	msg.Italic("💡 输入影片名称后自动搜索")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: &callback.Keyboard{},
	}, nil
}

func (h *StartHandler) HandleAI(ctx *callback.Context) (*callback.Response, error) {
	// Recommendation is only available in private chats
	if ctx.ChatType != "private" {
		return &callback.Response{
			Text:        "⚠️ 推荐功能仅在私聊中可用",
			CallbackMsg: "请私聊使用",
			ShowAlert:   true,
		}, nil
	}

	msg := services.NewMessageBuilder()
	msg.Bold("🎬 精选推荐").Newline()
	msg.Newline()
	msg.Italic("✨ 发现你喜欢的精彩内容").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🔥 本周热门", "search:type:trending")
	kb.AddButton("📺 热门剧集", "search:type:hot")
	kb.NewRow()
	kb.AddButton("⭐ 必看神作", "search:type:toprated")
	kb.AddButton("🆕 最新上映", "search:type:new")
	kb.NewRow()
	kb.AddButton("🎲 随机探索", "search:type:random")
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

func (h *StartHandler) HandleHot(ctx *callback.Context) (*callback.Response, error) {
	return &callback.Response{
		Text:        "📺 加载中...",
		CallbackMsg: "加载中",
		ShowAlert:   true,
	}, nil
}

func (h *StartHandler) HandleNew(ctx *callback.Context) (*callback.Response, error) {
	return &callback.Response{
		Text:        "🆕 加载中...",
		CallbackMsg: "加载中",
		ShowAlert:   true,
	}, nil
}

// DetailHandler handles media detail callbacks
type DetailHandler struct {
	sessMgr    *session.Manager
	telegram   *services.TelegramClient
	moviepilot *services.MoviePilotClient
	tmdb       *services.TMDBClient
}

func NewDetailHandler(
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	moviepilot *services.MoviePilotClient,
	tmdb *services.TMDBClient,
) *DetailHandler {
	return &DetailHandler{
		sessMgr:    sessMgr,
		telegram:   telegram,
		moviepilot: moviepilot,
		tmdb:       tmdb,
	}
}

func (h *DetailHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	// Check if this is a detail_seasons action
	if ctx.Callback.Action == callback.ActionDetailSeasons {
		return h.HandleSeasons(ctx)
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

	sess := h.sessMgr.GetOrCreate(ctx.UserID)

	// Check if we have cached data from search results first
	items, _, query, hasSearch := sess.GetSearchResults()
	if hasSearch {
		for _, item := range items {
			if item.ID == mediaID {
				// Push navigation entry before showing detail
				// If query is like "trending", "hot", "new", "toprated", "random", it's from AI recommendation
				if isAIRecommendationQuery(query) {
					sess.PushNavEntry("ai_recommendation", query, query)
				}
				// Use search result data - it already has all we need
				log.Printf("[DetailHandler] Using search result info for: %s", item.Title)
				return h.buildDetailFromSearch(item, mediaType, sess), nil
			}
		}
	}

	// Check if we have cached data from AI
	if cachedItem := sess.GetCachedAIItem(tmdbID); cachedItem != nil {
		return h.buildDetailFromCache(cachedItem, sess), nil
	}

	// Try TMDB API for details
	if h.tmdb != nil {
		tmdbMedia, err := h.tmdb.GetMediaByType(tmdbID, mediaType)
		if err == nil && tmdbMedia != nil {
			log.Printf("[DetailHandler] Got media info from TMDB: %s", tmdbMedia.GetTitle())
			return h.buildDetailFromTMDB(tmdbMedia, sess), nil
		}
		log.Printf("[DetailHandler] TMDB API failed: %v", err)
	}

	// If all else fails, build a simple detail page
	return h.buildSimpleDetail(tmdbID, mediaType, sess), nil
}

// isAIRecommendationQuery checks if the query is from AI recommendation
func isAIRecommendationQuery(query string) bool {
	aiTypes := []string{"trending", "hot", "new", "toprated", "random"}
	for _, t := range aiTypes {
		if query == t {
			return true
		}
	}
	return false
}

func (h *DetailHandler) buildDetailFromCache(item *session.AIRecommendationItem, sess *session.Session) *callback.Response {
	msg := services.NewMessageBuilder()

	// Type icon and label
	typeIcon := "🎬"
	typeLabel := "电影"
	if item.MediaType == "tv" {
		typeIcon = "📺"
		typeLabel = "剧集"
	}

	// Header with title
	msg.Bold(fmt.Sprintf("%s %s", typeIcon, item.Title)).Newline()

	// Year and rating on same line
	infoLine := ""
	if item.Year > 0 {
		infoLine = fmt.Sprintf("📅 %d年", item.Year)
	}
	if item.Rating > 0 {
		if infoLine != "" {
			infoLine += "  •  "
		}
		infoLine += fmt.Sprintf("⭐ %.1f分", item.Rating)
	}
	if infoLine != "" {
		msg.Text(infoLine).Newline()
	}

	// Media type badge
	msg.Text(fmt.Sprintf("🏷️ %s", typeLabel)).Newline()
	msg.Newline()

	// AI Reason
	if item.Reason != "" {
		msg.Bold("💭 推荐理由").Newline()
		msg.Text(item.Reason).Newline()
		msg.Newline()
	}

	// Overview (truncate if too long)
	if item.Overview != "" {
		overview := item.Overview
		if len(overview) > 150 {
			overview = overview[:150] + "..."
		}
		msg.Text(overview).Newline()
		msg.Newline()
	}

	// Request button label
	buttonLabel := "✅ 立即求片"
	if item.MediaType == "tv" {
		buttonLabel = "✅ 求剧集"
	}

	// Keyboard
	kb := services.NewKeyboardBuilder()
	kb.AddButton(buttonLabel, fmt.Sprintf("request:id:%d:type:%s", item.TmdbID, item.MediaType))
	kb.AddButton("🐛 反馈", fmt.Sprintf("feedback:id:%d:type:%s:title:%s", item.TmdbID, item.MediaType, item.Title))
	kb.NewRow()
	kb.AddButton("⬅️ 返回列表", "back")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}
}

func (h *DetailHandler) buildDetailFromTMDB(media *services.TMDBMediaInfo, sess *session.Session) *callback.Response {
	// For TV shows, fetch full details with seasons
	if media.MediaType == "tv" && h.tmdb != nil {
		// From TMDB directly, assume MoviePilot availability is unknown (default to false to show warning)
		return h.buildDetailFromTMDBTV(media.ID, media.GetTitle(), sess, false)
	}

	msg := services.NewMessageBuilder()

	// Type icon
	typeIcon := "🎬"

	// Header
	msg.Bold(fmt.Sprintf("%s %s", typeIcon, media.GetTitle())).Newline()

	// Year and rating
	infoLine := ""
	year := media.GetYear()
	if year > 0 {
		infoLine = fmt.Sprintf("📅 %d年", year)
	}
	if media.VoteAverage > 0 {
		if infoLine != "" {
			infoLine += "  •  "
		}
		infoLine += fmt.Sprintf("⭐ %.1f分", media.VoteAverage)
	}
	if infoLine != "" {
		msg.Text(infoLine).Newline()
	}

	// Runtime
	runtime := media.GetRuntime()
	if runtime > 0 {
		hours := runtime / 60
		mins := runtime % 60
		if hours > 0 {
			msg.Text(fmt.Sprintf("⏱️ 时长: %d小时%d分钟", hours, mins))
		} else {
			msg.Text(fmt.Sprintf("⏱️ 时长: %d分钟", runtime))
		}
		msg.Newline()
	}

	// Genres
	genres := media.GetGenres()
	if genres != "" {
		msg.Text(fmt.Sprintf("🎭 %s", genres)).Newline()
	}

	msg.Newline()

	// Overview (truncate if too long)
	if media.Overview != "" {
		overview := media.Overview
		if len(overview) > 200 {
			overview = overview[:200] + "..."
		}
		msg.Text(overview).Newline()
		msg.Newline()
	}

	// TMDB ID
	msg.Text(fmt.Sprintf("🆔 TMDB ID: %d", media.ID)).Newline()

	// Keyboard
	kb := services.NewKeyboardBuilder()
	kb.AddButton("✅ 立即求片", fmt.Sprintf("request:id:%d:type:movie", media.ID))
	kb.AddButton("🐛 反馈", fmt.Sprintf("feedback:id:%d:type:movie:title:%s", media.ID, media.Title))
	kb.NewRow()
	kb.AddButton("⬅️ 返回列表", "back")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}
}

// buildDetailFromTMDBTV builds detail page for TV show from TMDB with season info
func (h *DetailHandler) buildDetailFromTMDBTV(tmdbID int, title string, sess *session.Session, mpNotAvailable bool) *callback.Response {
	// Fetch TV details with seasons from TMDB
	tvDetails, err := h.tmdb.GetTVDetailsWithSeasons(tmdbID)
	if err != nil {
		log.Printf("[DetailHandler] Failed to get TV details from TMDB: %v", err)
		// Fallback to simple detail
		return h.buildSimpleTVDetail(tmdbID, title, sess, mpNotAvailable)
	}

	msg := services.NewMessageBuilder()

	// Header with title
	msg.Bold(fmt.Sprintf("📺 %s", title)).Newline()
	msg.Newline()

	// Year and rating
	infoLine := ""
	if tvDetails.FirstAirDate != "" && len(tvDetails.FirstAirDate) >= 4 {
		year := tvDetails.FirstAirDate[:4]
		infoLine = fmt.Sprintf("📅 %s年", year)
	}
	if tvDetails.VoteAverage > 0 {
		if infoLine != "" {
			infoLine += "  •  "
		}
		infoLine += fmt.Sprintf("⭐ %.1f分", tvDetails.VoteAverage)
	}
	if infoLine != "" {
		msg.Text(infoLine).Newline()
	}

	// Genres
	if len(tvDetails.Genres) > 0 {
		genreNames := make([]string, 0)
		for _, g := range tvDetails.Genres {
			genreNames = append(genreNames, g.Name)
		}
		if len(genreNames) > 0 {
			msg.Text(fmt.Sprintf("🎭 %s", strings.Join(genreNames, "、"))).Newline()
		}
	}

	// Show number of seasons and episodes
	if tvDetails.NumberOfSeasons > 0 {
		msg.Text(fmt.Sprintf("📺 共 %d 季 · %d 集", tvDetails.NumberOfSeasons, tvDetails.NumberOfEpisodes)).Newline()
	}

	msg.Newline()

	// Overview (truncate if too long)
	if tvDetails.Overview != "" {
		overview := tvDetails.Overview
		if len(overview) > 200 {
			overview = overview[:200] + "..."
		}
		msg.Italic("📖 剧情简介").Newline()
		msg.Text(overview).Newline()
		msg.Newline()
	}

	// TMDB ID
	msg.Text(fmt.Sprintf("🆔 TMDB ID: %d", tvDetails.ID)).Newline()

	// Warning if MoviePilot doesn't have this media
	if mpNotAvailable {
		msg.Newline()
		msg.Bold("⚠️ 资源库暂无").Newline()
		msg.Text("当前资源库中暂无此剧集，求片后将尝试自动搜索").Newline()
	}

	// Build keyboard with season buttons
	kb := services.NewKeyboardBuilder()

	// Change button text if MoviePilot doesn't have the media
	requestButtonText := "✅ 订阅全季"
	if mpNotAvailable {
		requestButtonText = "🔄 尝试求片"
	}

	// First row: main action buttons (subscribe + feedback)
	kb.AddButton(requestButtonText, fmt.Sprintf("request:id:%d:type:tv:season:0", tvDetails.ID))
	kb.AddButton("🐛 反馈", fmt.Sprintf("feedback:id:%d:type:tv:title:%s", tvDetails.ID, title))
	kb.NewRow()

	// Second row: navigation buttons
	kb.AddButton("⬅️ 返回列表", "start")
	if len(tvDetails.Seasons) > 6 {
		kb.AddButton(fmt.Sprintf("📺 全部 %d 季", len(tvDetails.Seasons)), fmt.Sprintf("detail_seasons:id:%d", tvDetails.ID))
	}
	kb.NewRow()

	// Show seasons in a clean grid layout (3 per row)
	displayCount := len(tvDetails.Seasons)
	if displayCount > 6 {
		displayCount = 6
	}
	for i, s := range tvDetails.Seasons {
		if i >= displayCount {
			break
		}
		seasonName := fmt.Sprintf("S%d", s.SeasonNumber)
		if s.SeasonNumber == 0 {
			seasonName = "特别篇"
		}
		kb.AddButton(seasonName, fmt.Sprintf("request:id:%d:type:tv:season:%d", tvDetails.ID, s.SeasonNumber))
		if (i+1)%3 == 0 {
			kb.NewRow()
		}
	}

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}
}

// buildSimpleTVDetail builds a simple TV detail page (fallback)
func (h *DetailHandler) buildSimpleTVDetail(tmdbID int, title string, sess *session.Session, mpNotAvailable bool) *callback.Response {
	msg := services.NewMessageBuilder()
	msg.Bold(fmt.Sprintf("📺 %s", title)).Newline()
	msg.Newline()
	msg.Italic("暂无法获取季数信息，请尝试直接订阅全季").Newline()

	// Warning if MoviePilot doesn't have this media
	if mpNotAvailable {
		msg.Newline()
		msg.Bold("⚠️ 资源库暂无").Newline()
		msg.Text("当前资源库中暂无此剧集，求片后将尝试自动搜索").Newline()
	}

	kb := services.NewKeyboardBuilder()

	// Change button text if MoviePilot doesn't have the media
	requestButtonText := "✅ 订阅全季"
	if mpNotAvailable {
		requestButtonText = "🔄 尝试求片"
	}

	// First row: main action buttons
	kb.AddButton(requestButtonText, fmt.Sprintf("request:id:%d:type:tv:season:0", tmdbID))
	kb.AddButton("🐛 反馈", fmt.Sprintf("feedback:id:%d:type:tv:title:%s", tmdbID, title))
	kb.NewRow()

	// Second row: navigation
	kb.AddButton("⬅️ 返回列表", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}
}

func (h *DetailHandler) buildDetailFromMedia(media *services.MediaInfo, sess *session.Session) *callback.Response {
	msg := services.NewMessageBuilder()

	// Get title
	title := media.Title

	// Type icon
	typeIcon := "🎬"
	if media.Type == services.MediaTypeTV {
		typeIcon = "📺"
	}

	// Header
	msg.Bold(fmt.Sprintf("%s %s", typeIcon, title)).Newline()

	// Year and rating
	infoLine := ""
	if media.Year > 0 {
		infoLine = fmt.Sprintf("📅 %d年", media.Year)
	}
	if media.Rating > 0 {
		if infoLine != "" {
			infoLine += "  •  "
		}
		infoLine += fmt.Sprintf("⭐ %.1f分", media.Rating)
	}
	if infoLine != "" {
		msg.Text(infoLine).Newline()
	}

	// Genres - not available in MoviePilot MediaInfo
	// Skip genre display for now

	msg.Newline()

	// Overview (truncate if too long)
	if media.Overview != "" {
		overview := media.Overview
		if len(overview) > 200 {
			overview = overview[:200] + "..."
		}
		msg.Text(overview).Newline()
		msg.Newline()
	}

	// TMDB ID
	msg.Text(fmt.Sprintf("🆔 TMDB ID: %d", media.ID)).Newline()

	// Request button label
	buttonLabel := "✅ 立即求片"
	if media.Type == services.MediaTypeTV {
		buttonLabel = "✅ 求剧集"
	}

	// Keyboard
	mediaTypeStr := "movie"
	if media.Type == services.MediaTypeTV {
		mediaTypeStr = "tv"
	}
	kb := services.NewKeyboardBuilder()
	kb.AddButton(buttonLabel, fmt.Sprintf("request:id:%d:type:%s", media.ID, mediaTypeStr))
	kb.AddButton("🐛 反馈", fmt.Sprintf("feedback:id:%d:type:%s:title:%s", media.ID, mediaTypeStr, media.Title))
	kb.NewRow()
	kb.AddButton("⬅️ 返回列表", "back")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}
}

// buildDetailFromSearch builds detail page from search result item
func (h *DetailHandler) buildDetailFromSearch(item session.SearchItem, mediaType string, sess *session.Session) *callback.Response {
	// Try to get full media info from MoviePilot first
	mediaID, _ := strconv.Atoi(item.ID)
	mpNotAvailable := false // Track if MoviePilot has the media
	if mediaID > 0 && h.moviepilot != nil {
		// Determine media type
		mpType := services.MediaTypeMovie
		isTV := mediaType == "tv" || item.Type == "tv" || item.Type == "电视剧"
		if isTV {
			mpType = services.MediaTypeTV
		}

		mediaInfo, err := h.moviepilot.GetMediaInfo(mediaID, mpType)
		if err == nil && mediaInfo != nil {
			// Get the query from session to determine back button behavior
			_, _, query, _ := sess.GetSearchResults()
			return h.buildDetailFromMediaInfo(mediaInfo, sess, query)
		}
		log.Printf("[DetailHandler] Failed to get media info from MoviePilot: %v", err)
		// Check if error is "not found" to warn user
		if err != nil && strings.Contains(err.Error(), "not found") {
			mpNotAvailable = true
		}

		// For TV shows, fallback to TMDB for season info
		if isTV && h.tmdb != nil {
			log.Printf("[DetailHandler] Falling back to TMDB for TV show seasons: %s", item.Title)
			return h.buildDetailFromTMDBTV(mediaID, item.Title, sess, mpNotAvailable)
		}
	}

	// Fallback to basic detail view from session data
	_, _, query, _ := sess.GetSearchResults()
	return h.buildBasicDetailFromSearch(item, mediaType, query, mpNotAvailable)
}

// buildDetailFromMediaInfo builds rich detail page from MoviePilot media info
func (h *DetailHandler) buildDetailFromMediaInfo(info *services.MediaInfo, sess *session.Session, query string) *callback.Response {
	msg := services.NewMessageBuilder()

	// Determine media type
	isTV := info.Type == services.MediaTypeTV
	typeIcon := "🎬"
	typeLabel := "电影"
	if isTV {
		typeIcon = "📺"
		typeLabel = "剧集"
	}

	// Title header
	msg.Bold(fmt.Sprintf("%s %s", typeIcon, info.Title)).Newline()
	msg.Newline()

	// Info section - Year, Rating, Type
	if info.Year > 0 {
		msg.Textf("📅 %d年  ", info.Year.Int())
	}
	if info.Rating > 0 {
		msg.Textf("⭐ %.1f分  ", info.Rating)
	}
	msg.Textf("🏷️ %s", typeLabel).Newline()
	msg.Newline()

	// Genres/Category
	if len(info.Genres) > 0 {
		msg.Textf("🎭 %s", strings.Join(info.Genres, "、")).Newline()
		msg.Newline()
	}

	// TV show seasons info
	seasons := extractSeasons(info)
	if isTV && len(seasons) > 0 {
		msg.Bold(fmt.Sprintf("📺 共 %d 季", len(seasons))).Newline()
		for i, s := range seasons {
			if i >= 3 {
				msg.Textf("   ... 还有 %d 季", len(seasons)-3).Newline()
				break
			}
			seasonName := fmt.Sprintf("第%d季", s.SeasonNumber)
			if s.Name != "" {
				seasonName = s.Name
			}
			msg.Text(fmt.Sprintf("   • %s (%d集)", seasonName, s.EpisodeCount)).Newline()
		}
		msg.Newline()
	}

	// Overview
	if info.Overview != "" {
		overview := info.Overview
		if len(overview) > MaxOverviewLength {
			overview = overview[:MaxOverviewLength] + "..."
		}
		msg.Italic("📖 剧情简介").Newline()
		msg.Text(overview).Newline()
		msg.Newline()
	}

	// TMDB ID at bottom
	msg.Text(fmt.Sprintf("🆔 TMDB ID: %d", info.ID)).Newline()

	// Build keyboard
	kb := services.NewKeyboardBuilder()

	if isTV && len(seasons) > 0 {
		// TV show - first row: main action buttons (subscribe + feedback)
		kb.AddButton("✅ 订阅全季", fmt.Sprintf("request:id:%d:type:tv:season:0", info.ID))
		kb.AddButton("🐛 反馈", fmt.Sprintf("feedback:id:%d:type:tv:title:%s", info.ID, info.Title))
		kb.NewRow()

		// Second row: navigation buttons
		if isAIRecommendationQuery(query) {
			kb.AddButton("⬅️ 返回", fmt.Sprintf("search:type:%s", query))
		} else {
			kb.AddButton("⬅️ 返回", "start")
		}
		if len(seasons) > 6 {
			kb.AddButton(fmt.Sprintf("📺 全部 %d 季", len(seasons)), fmt.Sprintf("detail_seasons:id:%d", info.ID))
		}
		kb.NewRow()

		// Show seasons in a clean grid layout (3 per row)
		displayCount := len(seasons)
		if displayCount > 6 {
			displayCount = 6
		}
		for i, s := range seasons {
			if i >= displayCount {
				break
			}
			seasonName := fmt.Sprintf("S%d", s.SeasonNumber)
			if s.SeasonNumber == 0 {
				seasonName = "特别篇"
			}
			kb.AddButton(seasonName, fmt.Sprintf("request:id:%d:type:tv:season:%d", info.ID, s.SeasonNumber))
			if (i+1)%3 == 0 {
				kb.NewRow()
			}
		}
	} else {
		// Movie - single subscribe button
		kb.AddButton("✅ 立即求片", fmt.Sprintf("request:id:%d:type:movie", info.ID))
		kb.NewRow()
		kb.AddButton("🐛 反馈", fmt.Sprintf("feedback:id:%d:type:movie:title:%s", info.ID, info.Title))
		kb.NewRow()
		// Back button - determine target based on query
		if isAIRecommendationQuery(query) {
			kb.AddButton("⬅️ 返回列表", fmt.Sprintf("search:type:%s", query))
		} else {
			kb.AddButton("⬅️ 返回主菜单", "start")
		}
	}

	// Check for poster image first
	photoURL := ""
	if info.Poster != "" {
		if strings.HasPrefix(info.Poster, "http") {
			photoURL = info.Poster
		} else {
			photoURL = "https://image.tmdb.org/t/p/w500" + info.Poster
		}
	}

	// Check for backdrop (higher quality image)
	if info.Backdrop != "" {
		if strings.HasPrefix(info.Backdrop, "http") {
			photoURL = info.Backdrop
		} else {
			photoURL = "https://image.tmdb.org/t/p/original" + info.Backdrop
		}
	}

	// If we have a photo, send it
	if photoURL != "" {
		return &callback.Response{
			Photo:        photoURL,
			PhotoCaption: msg.Build(),
			Edit:         false,
			Keyboard:     convertKeyboard(kb.Build()),
		}
	}

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}
}

// buildBasicDetailFromSearch builds basic detail page when MoviePilot API fails
func (h *DetailHandler) buildBasicDetailFromSearch(item session.SearchItem, mediaType string, query string, mpNotAvailable bool) *callback.Response {
	msg := services.NewMessageBuilder()

	// Type icon and label
	typeIcon := "🎬"
	typeLabel := "电影"
	isTV := mediaType == "tv" || item.Type == "tv"
	if isTV {
		typeIcon = "📺"
		typeLabel = "剧集"
	}

	// Header with title
	msg.Bold(fmt.Sprintf("%s %s", typeIcon, item.Title)).Newline()
	msg.Newline()

	// Info section
	if item.Year > 0 {
		msg.Textf("📅 %d年  ", item.Year)
	}
	if item.Rating > 0 {
		msg.Textf("⭐ %.1f分  ", item.Rating)
	}
	msg.Textf("🏷️ %s", typeLabel).Newline()
	msg.Newline()

	// TV show seasons
	if isTV && len(item.Seasons) > 0 {
		msg.Bold(fmt.Sprintf("📺 共 %d 季", len(item.Seasons))).Newline()
		for i, s := range item.Seasons {
			if i >= 3 {
				msg.Textf("   ... 还有 %d 季", len(item.Seasons)-3).Newline()
				break
			}
			seasonName := fmt.Sprintf("第%d季", s.SeasonNumber)
			if s.Name != "" {
				seasonName = s.Name
			}
			msg.Text(fmt.Sprintf("   • %s (%d集)", seasonName, s.EpisodeCount)).Newline()
		}
		msg.Newline()
	}

	// Overview
	if item.Overview != "" {
		overview := item.Overview
		if len(overview) > MaxOverviewLength {
			overview = overview[:MaxOverviewLength] + "..."
		}
		msg.Italic("📖 剧情简介").Newline()
		msg.Text(overview).Newline()
		msg.Newline()
	}

	msg.Text(fmt.Sprintf("🆔 TMDB ID: %s", item.ID)).Newline()

	// Warning if MoviePilot doesn't have this media
	if mpNotAvailable {
		msg.Newline()
		msg.Bold("⚠️ 资源库暂无").Newline()
		msg.Text("当前资源库中暂无此影片，求片后将尝试自动搜索").Newline()
	}

	// Build keyboard
	kb := services.NewKeyboardBuilder()

	// Change button text if MoviePilot doesn't have the media
	requestButtonText := "✅ 立即求片"
	if mpNotAvailable {
		requestButtonText = "🔄 尝试求片"
	}

	if isTV && len(item.Seasons) > 0 {
		// First row: main action buttons (subscribe + feedback)
		kb.AddButton(requestButtonText, fmt.Sprintf("request:id:%s:type:tv:season:0", item.ID))
		kb.AddButton("🐛 反馈", fmt.Sprintf("feedback:id:%s:type:tv:title:%s", item.ID, item.Title))
		kb.NewRow()

		// Second row: navigation
		if isAIRecommendationQuery(query) {
			kb.AddButton("⬅️ 返回列表", fmt.Sprintf("search:type:%s", query))
		} else {
			kb.AddButton("⬅️ 返回主菜单", "start")
		}
		if len(item.Seasons) > 4 {
			kb.AddButton(fmt.Sprintf("更多... (%d季)", len(item.Seasons)), fmt.Sprintf("detail_seasons:id:%s", item.ID))
		}
		kb.NewRow()

		// Season buttons (2 per row for compact display)
		for i, s := range item.Seasons {
			if i >= 4 {
				break
			}
			seasonName := fmt.Sprintf("S%d", s.SeasonNumber)
			if s.SeasonNumber == 0 {
				seasonName = "特别篇"
			}
			kb.AddButton(fmt.Sprintf("📺 %s", seasonName), fmt.Sprintf("request:id:%s:type:tv:season:%d", item.ID, s.SeasonNumber))
			if (i+1)%2 == 0 {
				kb.NewRow()
			}
		}
	} else {
		// Non-TV (movie or other): request + feedback + back
		kb.AddButton(requestButtonText, fmt.Sprintf("request:id:%s:type:%s", item.ID, item.Type))
		kb.AddButton("🐛 反馈", fmt.Sprintf("feedback:id:%s:type:%s:title:%s", item.ID, item.Type, item.Title))
		kb.NewRow()

		// Back button - determine target based on query
		if isAIRecommendationQuery(query) {
			kb.AddButton("⬅️ 返回列表", fmt.Sprintf("search:type:%s", query))
		} else {
			kb.AddButton("⬅️ 返回主菜单", "start")
		}
	}

	// Check for poster
	photoURL := ""
	if item.Poster != "" {
		if strings.HasPrefix(item.Poster, "http") {
			photoURL = item.Poster
		} else if strings.HasPrefix(item.Poster, "/") {
			photoURL = "https://image.tmdb.org/t/p/w500" + item.Poster
		} else {
			photoURL = "https://image.tmdb.org/t/p/w500/" + item.Poster
		}
	}

	if photoURL != "" {
		return &callback.Response{
			Photo:        photoURL,
			PhotoCaption: msg.Build(),
			Edit:         false,
			Keyboard:     convertKeyboard(kb.Build()),
		}
	}

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}
}

// buildSimpleDetail builds a simple detail page when API fails
func (h *DetailHandler) buildSimpleDetail(tmdbID int, mediaType string, sess *session.Session) *callback.Response {
	msg := services.NewMessageBuilder()

	// Type icon and label
	typeIcon := "🎬"
	typeLabel := "电影"
	if mediaType == "tv" {
		typeIcon = "📺"
		typeLabel = "剧集"
	}

	// Header
	msg.Bold(fmt.Sprintf("%s 影片详情", typeIcon)).Newline().Newline()

	// TMDB ID
	msg.Text(fmt.Sprintf("🆔 TMDB ID: %d", tmdbID)).Newline()
	msg.Text(fmt.Sprintf("🏷️ 类型: %s", typeLabel)).Newline()
	msg.Newline()

	// Note
	msg.Italic("💡 信息暂不可用，但仍可发起请求").Newline().Newline()

	// Request button label
	buttonLabel := "✅ 立即求片"
	if mediaType == "tv" {
		buttonLabel = "✅ 求剧集"
	}

	// Keyboard
	kb := services.NewKeyboardBuilder()
	kb.AddButton(buttonLabel, fmt.Sprintf("request:id:%d:type:%s", tmdbID, mediaType))
	kb.AddButton("🐛 反馈", fmt.Sprintf("feedback:id:%d:type:%s", tmdbID, mediaType))
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}
}

// HandleSeasons handles the detail_seasons action - shows all seasons for a TV show
func (h *DetailHandler) HandleSeasons(ctx *callback.Context) (*callback.Response, error) {
	// Get media ID from params
	mediaID, hasID := ctx.Callback.Params["id"]
	if !hasID {
		return &callback.Response{
			Text:        "❌ 请求无效",
			CallbackMsg: "参数错误",
			ShowAlert:   true,
		}, nil
	}

	sess := h.sessMgr.GetOrCreate(ctx.UserID)

	// Try to find the item in search results
	items, _, _, hasSearch := sess.GetSearchResults()
	if !hasSearch {
		return &callback.Response{
			Text:        "⏰ 搜索结果已过期，请重新搜索",
			CallbackMsg: "结果已过期",
			ShowAlert:   true,
		}, nil
	}

	var targetItem *session.SearchItem
	for i := range items {
		if items[i].ID == mediaID {
			targetItem = &items[i]
			break
		}
	}

	if targetItem == nil {
		return &callback.Response{
			Text:        "⏰ 未找到该媒体信息",
			CallbackMsg: "未找到",
			ShowAlert:   true,
		}, nil
	}

	// Check if it's a TV show with seasons
	if targetItem.Type != "tv" || len(targetItem.Seasons) == 0 {
		return &callback.Response{
			Text:        "⏰ 该媒体没有季信息",
			CallbackMsg: "无季信息",
			ShowAlert:   true,
		}, nil
	}

	// Build seasons list page
	msg := services.NewMessageBuilder()
	msg.Bold(fmt.Sprintf("📺 %s - 全部季", targetItem.Title)).Newline()
	msg.Newline()
	msg.Text(fmt.Sprintf("共 %d 季", len(targetItem.Seasons))).Newline()
	msg.Newline()

	kb := services.NewKeyboardBuilder()

	// List all seasons with buttons
	for i, season := range targetItem.Seasons {
		seasonName := fmt.Sprintf("第%d季", season.SeasonNumber)
		if season.SeasonNumber == 0 {
			seasonName = "特别篇"
		}
		if season.Name != "" && season.Name != seasonName {
			seasonName = fmt.Sprintf("%s - %s", seasonName, season.Name)
		}

		msg.Text(fmt.Sprintf("%d. %s (%d集)", i+1, seasonName, season.EpisodeCount)).Newline()

		// Add button for each season
		buttonLabel := fmt.Sprintf("📺 S%d", season.SeasonNumber)
		if season.SeasonNumber == 0 {
			buttonLabel = "📺 特别篇"
		}
		kb.AddButton(buttonLabel, fmt.Sprintf("request:id:%s:type:tv:season:%d", targetItem.ID, season.SeasonNumber))

		// Two buttons per row
		if (i+1)%2 == 0 {
			kb.NewRow()
		}
	}

	if len(targetItem.Seasons)%2 != 0 {
		kb.NewRow()
	}

	// Action row: subscribe + feedback
	kb.AddButton("✅ 订阅全季", fmt.Sprintf("request:id:%s:type:tv:season:0", targetItem.ID))
	kb.AddButton("🐛 反馈", fmt.Sprintf("feedback:id:%s:type:tv:title:%s", targetItem.ID, targetItem.Title))
	kb.NewRow()

	// Navigation row
	kb.AddButton("⬅️ 返回详情", fmt.Sprintf("detail:id:%s:type:tv", targetItem.ID))
	kb.AddButton("🏠 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// BackHandler handles back navigation
type BackHandler struct {
	sessMgr      *session.Manager
	adminService *services.AdminService
}

func NewBackHandler(sessMgr *session.Manager) *BackHandler {
	return &BackHandler{sessMgr: sessMgr}
}

// SetAdminService sets the admin service
func (h *BackHandler) SetAdminService(adminSvc *services.AdminService) {
	h.adminService = adminSvc
}

func (h *BackHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	sess := h.sessMgr.GetOrCreate(ctx.UserID)

	entry, hasHistory := sess.PopNavEntry()
	if !hasHistory {
		// No history, show start menu with full content
		msg := services.NewMessageBuilder()
		msg.Bold("👋 欢迎使用云海影视助手").Newline()
		msg.Newline()
		msg.Text("🔍 搜索影片 · 快速查找心仪内容").Newline()
		msg.Text("🎬 精选推荐 · 发现优质内容").Newline()
		msg.Text("📋 我的请求 · 跟踪求片进度").Newline()
		msg.Text("🐛 我的反馈 · 查看问题反馈").Newline()
		msg.Text("🔗 账号绑定 · 同步观影记录").Newline()
		msg.Newline()
		msg.Italic("💡 点击下方按钮开始探索").Newline()

		isAdmin := h.adminService != nil && h.adminService.IsAdmin(ctx.UserID)
		isPrivateChat := ctx.ChatType == "private"

		return &callback.Response{
			Text:     msg.Build(),
			Edit:     true,
			Keyboard: convertKeyboard(services.BuildStartKeyboardWithOptions(isAdmin, isPrivateChat)),
		}, nil
	}

	// Restore previous view
	log.Printf("[BackHandler] Restoring view: source=%s", entry.Source)

	// Based on source, restore appropriate view
	switch entry.Source {
	case "ai_trending", "trending":
		return &callback.Response{
			Text:        "🔄 加载中...",
			CallbackMsg: "加载中",
			ShowAlert:   true,
		}, nil
	case "ai_hot", "hot":
		return &callback.Response{
			Text:        "🔄 加载中...",
			CallbackMsg: "加载中",
			ShowAlert:   true,
		}, nil
	case "ai_new", "new":
		return &callback.Response{
			Text:        "🔄 加载中...",
			CallbackMsg: "加载中",
			ShowAlert:   true,
		}, nil
	default:
		// Show start menu with full content for any other source
		msg := services.NewMessageBuilder()
		msg.Bold("👋 欢迎使用云海影视助手").Newline()
		msg.Newline()
		msg.Text("🔍 搜索影片 · 快速查找心仪内容").Newline()
		msg.Text("🎬 精选推荐 · 发现优质内容").Newline()
		msg.Text("📋 我的请求 · 跟踪求片进度").Newline()
		msg.Text("🐛 我的反馈 · 查看问题反馈").Newline()
		msg.Text("🔗 账号绑定 · 同步观影记录").Newline()
		msg.Newline()
		msg.Italic("💡 点击下方按钮开始探索").Newline()

		isAdmin := h.adminService != nil && h.adminService.IsAdmin(ctx.UserID)
		isPrivateChat := ctx.ChatType == "private"

		return &callback.Response{
			Text:     msg.Build(),
			Edit:     true,
			Keyboard: convertKeyboard(services.BuildStartKeyboardWithOptions(isAdmin, isPrivateChat)),
		}, nil
	}
}

// Helper function to convert keyboard types
func convertKeyboard(tk *types.TelegramInlineKeyboard) *callback.Keyboard {
	if tk == nil {
		return &callback.Keyboard{InlineKeyboard: [][]callback.Button{}}
	}

	buttons := make([][]callback.Button, len(tk.InlineKeyboard))
	for i, row := range tk.InlineKeyboard {
		buttons[i] = make([]callback.Button, len(row))
		for j, btn := range row {
			buttons[i][j] = callback.Button{
				Text:         btn.Text,
				CallbackData: btn.CallbackData,
				URL:          btn.URL,
			}
		}
	}

	return &callback.Keyboard{InlineKeyboard: buttons}
}

// CancelHandler handles cancel action
type CancelHandler struct{}

func NewCancelHandler() *CancelHandler {
	return &CancelHandler{}
}

func (h *CancelHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	return &callback.Response{
		Text:     "✖️ 已取消",
		Edit:     true,
		Keyboard: &callback.Keyboard{},
	}, nil
}
