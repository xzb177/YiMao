package handlers

import (
	"fmt"
	"log"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
	"emby-telegram-bot/pkg/errors"
)

// RequestHandler handles media request callbacks
type RequestHandler struct {
	sessMgr    *session.Manager
	telegram   *services.TelegramClient
	jellyseerr *services.JellyseerrClient
}

func NewRequestHandler(
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	jellyseerr *services.JellyseerrClient,
) *RequestHandler {
	return &RequestHandler{
		sessMgr:    sessMgr,
		telegram:   telegram,
		jellyseerr: jellyseerr,
	}
}

func (h *RequestHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
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

	// Get Jellyseerr user ID from session
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	jellyseerrID := sess.GetJellyseerrUserID()
	if jellyseerrID == 0 {
		return &callback.Response{
			Text:        "❌ 请先使用 /link 命令绑定 Jellyseerr 账号",
			CallbackMsg: "需要绑定账号",
			ShowAlert:   true,
		}, nil
	}

	// Check quota
	quota, err := h.jellyseerr.GetUserQuota(jellyseerrID)
	if err != nil {
		log.Printf("[RequestHandler] Failed to get quota: %v", err)
	}

	// For TV shows, get season selection
	if mediaType == "tv" {
		return h.handleTVRequest(ctx, tmdbID, jellyseerrID, quota)
	}

	// For movies, request directly
	return h.handleMovieRequest(ctx, tmdbID, jellyseerrID, quota)
}

func (h *RequestHandler) handleMovieRequest(
	ctx *callback.Context,
	tmdbID int,
	jellyseerrID int64,
	quota *services.QuotaInfo,
) (*callback.Response, error) {
	// Check movie quota
	if quota != nil && quota.MovieRemaining == 0 {
		return &callback.Response{
			Text:        "❌ 今日电影配额已用完",
			CallbackMsg: "配额已用完",
			ShowAlert:   true,
		}, nil
	}

	// Create request
	req, err := h.jellyseerr.RequestMedia(jellyseerrID, tmdbID, "movie", nil)
	if err != nil {
		return nil, errors.JellyseerrErr("failed to create request", err)
	}

	log.Printf("[RequestHandler] Created request: ID=%d, MediaID=%d, Type=movie", req.ID, tmdbID)

	return &callback.Response{
		Text:        "✅ 请求已提交",
		CallbackMsg: "请求成功",
		ShowAlert:   true,
	}, nil
}

func (h *RequestHandler) handleTVRequest(
	ctx *callback.Context,
	tmdbID int,
	jellyseerrID int64,
	quota *services.QuotaInfo,
) (*callback.Response, error) {
	// Check TV quota
	if quota != nil && quota.TVRemaining == 0 {
		return &callback.Response{
			Text:        "❌ 今日剧集配额已用完",
			CallbackMsg: "配额已用完",
			ShowAlert:   true,
		}, nil
	}

	// For simplicity, request all seasons (can be enhanced later)
	req, err := h.jellyseerr.RequestMedia(jellyseerrID, tmdbID, "tv", nil)
	if err != nil {
		return nil, errors.JellyseerrErr("failed to create request", err)
	}

	log.Printf("[RequestHandler] Created request: ID=%d, MediaID=%d, Type=tv", req.ID, tmdbID)

	return &callback.Response{
		Text:        "✅ 请求已提交",
		CallbackMsg: "请求成功",
		ShowAlert:   true,
	}, nil
}

// SearchHandler handles search callbacks
type SearchHandler struct {
	sessMgr    *session.Manager
	telegram   *services.TelegramClient
	jellyseerr *services.JellyseerrClient
}

func NewSearchHandler(
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	jellyseerr *services.JellyseerrClient,
) *SearchHandler {
	return &SearchHandler{
		sessMgr:    sessMgr,
		telegram:   telegram,
		jellyseerr: jellyseerr,
	}
}

func (h *SearchHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	// Check if this is a search result selection
	if tmdbIDStr, hasID := ctx.Callback.Params["id"]; hasID {
		return h.handleSelect(ctx, tmdbIDStr)
	}

	// Check if this is a page navigation
	if pageStr, hasPage := ctx.Callback.Params["page"]; hasPage {
		return h.handlePage(ctx, pageStr)
	}

	// Otherwise, show search prompt
	return &callback.Response{
		Text:     "🔍 请输入影片名称进行搜索",
		Edit:     true,
		Keyboard: &callback.Keyboard{},
	}, nil
}

func (h *SearchHandler) handleSelect(ctx *callback.Context, tmdbIDStr string) (*callback.Response, error) {
	// Redirect to detail handler
	detailHandler := NewDetailHandler(h.sessMgr, h.telegram, h.jellyseerr)
	return detailHandler.Handle(ctx)
}

func (h *SearchHandler) handlePage(ctx *callback.Context, pageStr string) (*callback.Response, error) {
	// Get last search query from session
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	_, page, _, hasResults := sess.GetSearchResults()

	if !hasResults {
		return &callback.Response{
			Text:        "搜索结果已过期，请重新搜索",
			CallbackMsg: "结果已过期",
			ShowAlert:   true,
		}, nil
	}

	// TODO: Fetch new page from Jellyseerr
	return &callback.Response{
		Text:        fmt.Sprintf("📄 第 %d 页", page),
		CallbackMsg: "翻页功能开发中",
		ShowAlert:   true,
	}, nil
}
