package handlers

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
)

// WatchlistHandler handles watchlist-related operations
type WatchlistHandler struct {
	sessionMgr *session.Manager
	telegram   *services.TelegramClient
	watchlist  *services.WatchlistService
	tmdb       *services.TMDBClient
	// Cache for watchlist display to avoid repeated builds
	displayCache map[int64]*cachedDisplay
	cacheMu      sync.RWMutex
	cacheTTL     int64 // cache TTL in seconds
}

type cachedDisplay struct {
	text     string
	keyboard *callback.Keyboard
	expiry   int64
}

// NewWatchlistHandler creates a new watchlist handler
func NewWatchlistHandler(sessionMgr *session.Manager, telegram *services.TelegramClient, watchlist *services.WatchlistService, tmdb *services.TMDBClient) *WatchlistHandler {
	return &WatchlistHandler{
		sessionMgr:   sessionMgr,
		telegram:     telegram,
		watchlist:    watchlist,
		tmdb:         tmdb,
		displayCache: make(map[int64]*cachedDisplay),
		cacheTTL:     60, // 1 minute cache
	}
}

// Handle processes watchlist callbacks
func (h *WatchlistHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	switch ctx.Callback.Action {
	case "watchlist":
		return h.showWatchlist(ctx)
	case "watchlist_add":
		return h.addToWatchlist(ctx)
	case "watchlist_page":
		return h.showWatchlistPage(ctx)
	case "watchlist_remove":
		return h.removeFromWatchlist(ctx)
	case "watchlist_collections":
		return h.showCollections(ctx)
	case "watchlist_create_collection":
		return h.createCollection(ctx)
	case "watchlist_collection_items":
		return h.showCollectionItems(ctx)
	default:
		return &callback.Response{}, nil
	}
}

// showWatchlist displays the user's watchlist (page 1)
func (h *WatchlistHandler) showWatchlist(ctx *callback.Context) (*callback.Response, error) {
	return h.showWatchlistPage(ctx)
}

// showWatchlistPage displays a specific page of the watchlist
func (h *WatchlistHandler) showWatchlistPage(ctx *callback.Context) (*callback.Response, error) {
	page := 1
	if pageStr, ok := ctx.Callback.Params["page"]; ok {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	items := h.watchlist.GetWatchlist(ctx.UserID)

	if len(items) == 0 {
		return &callback.Response{
			Text: "📋 你的片单是空的\n\n💡 在搜索结果页点击「📎」按钮收藏影片",
			Edit: true,
			Keyboard: &callback.Keyboard{
				InlineKeyboard: [][]callback.Button{
					{{Text: "◀️ 返回", CallbackData: "start"}},
				},
			},
		}, nil
	}

	const itemsPerPage = 10
	totalPages := (len(items) + itemsPerPage - 1) / itemsPerPage
	if page > totalPages {
		page = totalPages
	}

	// Build message
	text := h.buildWatchlistText(items, page, itemsPerPage, totalPages)
	keyboard := h.buildWatchlistKeyboard(items, page, itemsPerPage, totalPages)

	return &callback.Response{
		Text:     text,
		Edit:     true,
		Keyboard: keyboard,
	}, nil
}

// buildWatchlistText builds the watchlist display text
func (h *WatchlistHandler) buildWatchlistText(items []*services.WatchlistItem, page, itemsPerPage, totalPages int) string {
	startIdx := (page - 1) * itemsPerPage
	endIdx := startIdx + itemsPerPage
	if endIdx > len(items) {
		endIdx = len(items)
	}

	var text strings.Builder
	text.WriteString(fmt.Sprintf("📋 我的片单 (%d/%d)\n\n", page, totalPages))

	for i := startIdx; i < endIdx; i++ {
		item := items[i]
		mediaIcon := "🎬"
		if item.MediaType == "tv" {
			mediaIcon = "📺"
		}

		text.WriteString(fmt.Sprintf("%d. %s %s", i-startIdx+1, mediaIcon, item.Title))
		if item.Year > 0 {
			text.WriteString(fmt.Sprintf(" (%d)", item.Year))
		}
		if item.Rating > 0 {
			text.WriteString(fmt.Sprintf(" ⭐%.1f", item.Rating))
		}
		text.WriteString("\n")
	}

	// Add stats summary at bottom
	movies, tvShows := 0, 0
	for _, item := range items {
		if item.MediaType == "movie" {
			movies++
		} else if item.MediaType == "tv" {
			tvShows++
		}
	}
	text.WriteString(fmt.Sprintf("\n📊 共%d部 | 🎬电影 %d | 📺剧集 %d", len(items), movies, tvShows))

	return text.String()
}

// buildWatchlistKeyboard builds the watchlist navigation keyboard
func (h *WatchlistHandler) buildWatchlistKeyboard(items []*services.WatchlistItem, page, itemsPerPage, totalPages int) *callback.Keyboard {
	startIdx := (page - 1) * itemsPerPage
	endIdx := startIdx + itemsPerPage
	if endIdx > len(items) {
		endIdx = len(items)
	}

	keyboard := &callback.Keyboard{InlineKeyboard: [][]callback.Button{}}

	// Add item removal buttons (2 per row)
	var row []callback.Button
	for i := startIdx; i < endIdx; i++ {
		item := items[i]
		btn := callback.Button{
			Text:         fmt.Sprintf("🗑️ %d.", i-startIdx+1),
			CallbackData: fmt.Sprintf("watchlist_remove:%d", item.TmdbID),
		}
		row = append(row, btn)
		if len(row) == 2 {
			keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, row)
			row = []callback.Button{}
		}
	}
	if len(row) > 0 {
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, row)
	}

	// Add navigation row
	navRow := []callback.Button{}
	if page > 1 {
		navRow = append(navRow, callback.Button{
			Text:         "⬅️ 上一页",
			CallbackData: "watchlist_page:page:" + strconv.Itoa(page-1),
		})
	}
	if page < totalPages {
		navRow = append(navRow, callback.Button{
			Text:         "➡️ 下一页",
			CallbackData: "watchlist_page:page:" + strconv.Itoa(page+1),
		})
	}
	if len(navRow) > 0 {
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, navRow)
	}

	// Bottom actions
	keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []callback.Button{
		{Text: "🔄 刷新", CallbackData: "watchlist"},
		{Text: "📁 收藏夹", CallbackData: "watchlist_collections"},
	})
	keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []callback.Button{
		{Text: "◀️ 返回", CallbackData: "start"},
	})

	return keyboard
}

// addToWatchlist directly adds item to watchlist (simplified - no confirmation)
func (h *WatchlistHandler) addToWatchlist(ctx *callback.Context) (*callback.Response, error) {
	// Get TMDB ID from params
	tmdbIDStr, hasID := ctx.Callback.Params["id"]
	if !hasID {
		// Try raw data format
		parts := strings.Split(ctx.Callback.Raw, ":")
		if len(parts) < 2 {
			return &callback.Response{
				Text:        "❌ 参数错误",
				CallbackMsg: "错误",
				ShowAlert:   true,
			}, nil
		}
		tmdbIDStr = parts[1]
	}

	tmdbID, err := strconv.Atoi(tmdbIDStr)
	if err != nil {
		return &callback.Response{
			Text:        "❌ 无效的影片ID",
			CallbackMsg: "错误",
			ShowAlert:   true,
		}, nil
	}

	// Check if already in watchlist
	if h.watchlist.IsInWatchlist(ctx.UserID, tmdbID) {
		return &callback.Response{
			Text:        "📋 已在片单中",
			CallbackMsg: "已在片单",
			ShowAlert:   false,
		}, nil
	}

	// Get media info from session first (faster than API call)
	sess := h.sessionMgr.Get(ctx.UserID)
	var title string

	// Try to get from session search results
	if sess != nil {
		searchResults, _, _, found := sess.GetSearchResults()
		if found {
			for _, item := range searchResults {
				itemID := item.ID
				if strings.HasPrefix(itemID, "id:") {
					itemID = strings.TrimPrefix(itemID, "id:")
				}
				itemTmdbID := 0
				fmt.Sscanf(itemID, "%d", &itemTmdbID)
				if itemTmdbID == tmdbID {
					title = item.Title
					break
				}
			}
		}

		// Try AI cache
		if title == "" {
			if cachedItem := sess.GetCachedAIItem(tmdbID); cachedItem != nil {
				title = cachedItem.Title
			}
		}
	}

	// Fetch from TMDB if not in session
	if title == "" {
		// Try movie first, then TV
		mediaInfo, err := h.tmdb.GetMovieDetails(tmdbID)
		if err == nil && mediaInfo != nil {
			title = mediaInfo.Title
		} else {
			tvInfo, err := h.tmdb.GetTVDetails(tmdbID)
			if err == nil && tvInfo != nil {
				title = tvInfo.Title
			}
		}
	}

	// If still no title, use generic
	if title == "" {
		title = fmt.Sprintf("TMDB:%d", tmdbID)
	}

	// Create watchlist item with minimal info (will be enriched on view)
	item := &services.WatchlistItem{
		TmdbID:    tmdbID,
		Title:     title,
		MediaType: "movie", // default, will be updated when needed
		AddedAt:   h.watchlist.Now(),
	}

	// Try to determine media type from session
	if sess != nil {
		if mediaTypeVal, ok := sess.Get("media_type"); ok {
			if mt := fmt.Sprintf("%v", mediaTypeVal); mt == "tv" || mt == "movie" {
				item.MediaType = mt
			}
		}
	}

	err = h.watchlist.AddToWatchlist(ctx.UserID, item)
	if err != nil {
		log.Printf("[Watchlist] Add failed: %v", err)
		return &callback.Response{
			Text:        "❌ 添加失败",
			CallbackMsg: "失败",
			ShowAlert:   true,
		}, nil
	}

	log.Printf("[Watchlist] Added: %s (TMDB:%d) for user %d", title, tmdbID, ctx.UserID)

	return &callback.Response{
		Text:        fmt.Sprintf("✅ 已添加「%s」到片单", title),
		CallbackMsg: "已添加",
		ShowAlert:   false,
	}, nil
}

// removeFromWatchlist removes item from watchlist
func (h *WatchlistHandler) removeFromWatchlist(ctx *callback.Context) (*callback.Response, error) {
	tmdbIDStr, hasID := ctx.Callback.Params["id"]
	if !hasID {
		parts := strings.Split(ctx.Callback.Raw, ":")
		if len(parts) < 2 {
			return &callback.Response{
				Text:        "❌ 参数错误",
				CallbackMsg: "错误",
				ShowAlert:   true,
			}, nil
		}
		tmdbIDStr = parts[1]
	}

	tmdbID, err := strconv.Atoi(tmdbIDStr)
	if err != nil {
		return &callback.Response{
			Text:        "❌ 无效的影片ID",
			CallbackMsg: "错误",
			ShowAlert:   true,
		}, nil
	}

	err = h.watchlist.RemoveFromWatchlist(ctx.UserID, tmdbID)
	if err != nil {
		return &callback.Response{
			Text:        fmt.Sprintf("❌ %v", err),
			CallbackMsg: "失败",
			ShowAlert:   true,
		}, nil
	}

	// Return updated watchlist
	return h.showWatchlistPage(ctx)
}

// showCollections displays user's collections
func (h *WatchlistHandler) showCollections(ctx *callback.Context) (*callback.Response, error) {
	collections := h.watchlist.GetCollections(ctx.UserID)

	if len(collections) == 0 {
		return &callback.Response{
			Text: "📁 暂无收藏夹\n\n💡 主片单已足够日常使用",
			Edit: true,
			Keyboard: &callback.Keyboard{
				InlineKeyboard: [][]callback.Button{
					{{Text: "➕ 创建收藏夹", CallbackData: "watchlist_create_collection"}},
					{{Text: "◀️ 返回片单", CallbackData: "watchlist"}},
				},
			},
		}, nil
	}

	var text strings.Builder
	text.WriteString(fmt.Sprintf("📁 我的收藏夹 (%d)\n\n", len(collections)))

	for _, col := range collections {
		text.WriteString(fmt.Sprintf("📂 %s — %d部\n", col.Name, len(col.Items)))
	}

	keyboard := &callback.Keyboard{}
	for _, col := range collections {
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []callback.Button{
			{Text: fmt.Sprintf("📂 %s (%d)", col.Name, len(col.Items)), CallbackData: fmt.Sprintf("watchlist_collection_items:%s", col.Name)},
		})
	}

	keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []callback.Button{
		{Text: "◀️ 返回片单", CallbackData: "watchlist"},
	})

	return &callback.Response{
		Text:     text.String(),
		Edit:     true,
		Keyboard: keyboard,
	}, nil
}

// createCollection prompts user to create a new collection
func (h *WatchlistHandler) createCollection(ctx *callback.Context) (*callback.Response, error) {
	sess := h.sessionMgr.Get(ctx.UserID)
	sess.Set("creating_collection", true)

	return &callback.Response{
		Text: "✏️ 请输入收藏夹名称\n\n发送「取消」退出",
		Edit: true,
	}, nil
}

// showCollectionItems displays items in a specific collection
func (h *WatchlistHandler) showCollectionItems(ctx *callback.Context) (*callback.Response, error) {
	parts := strings.Split(ctx.Callback.Raw, ":")
	if len(parts) < 3 {
		return &callback.Response{
			Text:        "❌ 参数错误",
			CallbackMsg: "错误",
			ShowAlert:   true,
		}, nil
	}

	collectionName := strings.Join(parts[2:], ":")
	collections := h.watchlist.GetCollections(ctx.UserID)

	var targetCollection *services.WatchlistCollection
	for _, col := range collections {
		if col.Name == collectionName {
			targetCollection = col
			break
		}
	}

	if targetCollection == nil {
		return &callback.Response{
			Text:        "❌ 收藏夹不存在",
			CallbackMsg: "错误",
			ShowAlert:   true,
		}, nil
	}

	items := make([]*services.WatchlistItem, 0, len(targetCollection.Items))
	for _, item := range targetCollection.Items {
		items = append(items, item)
	}

	if len(items) == 0 {
		return &callback.Response{
			Text: fmt.Sprintf("📂 %s\n\n收藏夹是空的", targetCollection.Name),
			Edit: true,
			Keyboard: &callback.Keyboard{
				InlineKeyboard: [][]callback.Button{
					{{Text: "◀️ 返回", CallbackData: "watchlist_collections"}},
				},
			},
		}, nil
	}

	var text strings.Builder
	text.WriteString(fmt.Sprintf("📂 %s (%d部)\n\n", targetCollection.Name, len(items)))

	// Show first 20 items
	for i, item := range items {
		if i >= 20 {
			text.WriteString(fmt.Sprintf("\n... 还有 %d 部影片", len(items)-20))
			break
		}

		mediaIcon := "🎬"
		if item.MediaType == "tv" {
			mediaIcon = "📺"
		}

		text.WriteString(fmt.Sprintf("%s %s", mediaIcon, item.Title))
		if item.Year > 0 {
			text.WriteString(fmt.Sprintf(" (%d)", item.Year))
		}
		text.WriteString("\n")
	}

	keyboard := &callback.Keyboard{
		InlineKeyboard: [][]callback.Button{
			{{Text: "◀️ 返回", CallbackData: "watchlist_collections"}},
		},
	}

	return &callback.Response{
		Text:     text.String(),
		Edit:     true,
		Keyboard: keyboard,
	}, nil
}

// HandleCreateCollectionFromMessage handles collection creation from text message
func (h *WatchlistHandler) HandleCreateCollectionFromMessage(userID, chatID int64, text string) *callback.Response {
	if text == "取消" {
		return &callback.Response{
			Text: "✅ 已取消",
		}
	}

	name := strings.TrimSpace(text)
	if name == "" {
		return &callback.Response{
			Text: "❌ 名称不能为空",
		}
	}

	collection, err := h.watchlist.CreateCollection(userID, name, "")
	if err != nil {
		log.Printf("[Watchlist] Create collection failed: %v", err)
		return &callback.Response{
			Text: "❌ 创建失败",
		}
	}

	return &callback.Response{
		Text: fmt.Sprintf("✅ 已创建收藏夹「%s」", collection.Name),
		Keyboard: &callback.Keyboard{
			InlineKeyboard: [][]callback.Button{
				{{Text: "◀️ 返回片单", CallbackData: "watchlist"}},
			},
		},
	}
}
