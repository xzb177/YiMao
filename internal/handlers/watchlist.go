package handlers

import (
	"fmt"
	"log"
	"strconv"
	"strings"

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
}

// NewWatchlistHandler creates a new watchlist handler
func NewWatchlistHandler(sessionMgr *session.Manager, telegram *services.TelegramClient, watchlist *services.WatchlistService, tmdb *services.TMDBClient) *WatchlistHandler {
	return &WatchlistHandler{
		sessionMgr: sessionMgr,
		telegram:   telegram,
		watchlist:  watchlist,
		tmdb:       tmdb,
	}
}

// Handle processes watchlist callbacks
func (h *WatchlistHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	switch ctx.Callback.Action {
	case "watchlist":
		return h.showWatchlist(ctx)
	case "watchlist_add":
		return h.promptAddToWatchlist(ctx)
	case "watchlist_confirm_add":
		return h.confirmAddToWatchlist(ctx)
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

// showWatchlist displays the user's watchlist
func (h *WatchlistHandler) showWatchlist(ctx *callback.Context) (*callback.Response, error) {
	items := h.watchlist.GetWatchlist(ctx.UserID)

	if len(items) == 0 {
		return &callback.Response{
			Text: "📋 你的片单是空的\n\n在搜索或详情页面点击「加入片单」按钮来收藏你感兴趣的影片吧",
			Edit: true,
			Keyboard: &callback.Keyboard{
				InlineKeyboard: [][]callback.Button{
					{{Text: "◀️ 返回", CallbackData: "start"}},
				},
			},
		}, nil
	}

	// Build message with watchlist items
	var text strings.Builder
	text.WriteString("📋 我的片单\n\n")

	for i, item := range items {
		if i >= 20 { // Limit to 20 items
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
		if item.Rating > 0 {
			text.WriteString(fmt.Sprintf(" ⭐ %.1f", item.Rating))
		}
		text.WriteString("\n")
	}

	// Add stats
	stats := h.watchlist.GetWatchlistStats(ctx.UserID)
	text.WriteString(fmt.Sprintf("\n📊 统计: 共 %d 部影片 | 🎬 电影: %d | 📺 剧集: %d",
		stats["total_items"], stats["movies"], stats["tv_shows"]))

	// Build inline keyboard
	keyboard := &callback.Keyboard{
		InlineKeyboard: [][]callback.Button{
			{
				{Text: "🔄 刷新", CallbackData: "watchlist"},
				{Text: "📁 收藏夹", CallbackData: "watchlist_collections"},
			},
			{{Text: "◀️ 返回", CallbackData: "start"}},
		},
	}

	// Add item buttons (first 10)
	var itemButtons []callback.Button
	for i, item := range items {
		if i >= 10 {
			break
		}
		itemButtons = append(itemButtons, callback.Button{
			Text:         fmt.Sprintf("🗑️ %s", truncateString(item.Title, 20)),
			CallbackData: fmt.Sprintf("watchlist_remove:%d", item.TmdbID),
		})
	}
	if len(itemButtons) > 0 {
		keyboard.InlineKeyboard = append([][]callback.Button{itemButtons}, keyboard.InlineKeyboard...)
	}

	return &callback.Response{
		Text:     text.String(),
		Edit:     true,
		Keyboard: keyboard,
	}, nil
}

// promptAddToWatchlist shows prompt to add item to watchlist
func (h *WatchlistHandler) promptAddToWatchlist(ctx *callback.Context) (*callback.Response, error) {
	// Get TMDB ID from data
	parts := strings.Split(ctx.Callback.Raw, ":")
	if len(parts) < 2 {
		return &callback.Response{
			Text:     "❌ 参数错误",
			ShowAlert: true,
		}, nil
	}

	tmdbID, err := strconv.Atoi(parts[1])
	if err != nil {
		return &callback.Response{
			Text:     "❌ 无效的影片ID",
			ShowAlert: true,
		}, nil
	}

	// Store TMDB ID in session for confirmation
	sess := h.sessionMgr.Get(ctx.UserID)
	sess.Set("watchlist_add_tmdb", tmdbID)

	// Get collections for user
	collections := h.watchlist.GetCollections(ctx.UserID)

	keyboard := &callback.Keyboard{
		InlineKeyboard: [][]callback.Button{
			{{Text: "➕ 加入主片单", CallbackData: fmt.Sprintf("watchlist_confirm_add:%d:main", tmdbID)}},
		},
	}

	// Add collection buttons
	for _, col := range collections {
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []callback.Button{
			{Text: fmt.Sprintf("📁 %s", col.Name), CallbackData: fmt.Sprintf("watchlist_confirm_add:%d:collection:%s", tmdbID, col.Name)},
		})
	}

	keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []callback.Button{
		{Text: "✅ 创建新收藏夹", CallbackData: "watchlist_create_collection"},
		{Text: "◀️ 取消", CallbackData: "start"},
	})

	return &callback.Response{
		Text:     "📋 选择要添加到的位置",
		ShowAlert: true,
	}, nil
}

// confirmAddToWatchlist adds item to watchlist
func (h *WatchlistHandler) confirmAddToWatchlist(ctx *callback.Context) (*callback.Response, error) {
	parts := strings.Split(ctx.Callback.Raw, ":")
	if len(parts) < 3 {
		return &callback.Response{
			Text:     "❌ 参数错误",
			ShowAlert: true,
		}, nil
	}

	tmdbID, _ := strconv.Atoi(parts[1])
	target := parts[2]

	// Fetch media info from TMDB
	var mediaInfo *services.TMDBMediaInfo
	var err error

	// Check if we have media type in session
	sess := h.sessionMgr.Get(ctx.UserID)
	if mediaTypeVal, ok := sess.Get("media_type"); ok {
		mediaType := fmt.Sprintf("%v", mediaTypeVal)
		if mediaType == "tv" {
			mediaInfo, err = h.tmdb.GetTVDetails(tmdbID)
		} else {
			mediaInfo, err = h.tmdb.GetMovieDetails(tmdbID)
		}
	} else {
		// Try movie first
		mediaInfo, err = h.tmdb.GetMovieDetails(tmdbID)
		if err != nil {
			mediaInfo, err = h.tmdb.GetTVDetails(tmdbID)
		}
	}

	if err != nil || mediaInfo == nil {
		return &callback.Response{
			Text:     "❌ 无法获取影片信息",
			ShowAlert: true,
		}, nil
	}

	item := &services.WatchlistItem{
		TmdbID:    tmdbID,
		Title:     mediaInfo.Title,
		Year:      extractYear(mediaInfo.ReleaseDate, mediaInfo.FirstAirDate),
		MediaType: mediaInfo.MediaType,
		Poster:    mediaInfo.PosterPath,
		Overview:  mediaInfo.Overview,
		Rating:    mediaInfo.VoteAverage,
	}

	if target == "main" {
		err = h.watchlist.AddToWatchlist(ctx.UserID, item)
	} else if len(parts) >= 4 && parts[3] == "collection" {
		collectionName := parts[4]
		err = h.watchlist.AddToCollection(ctx.UserID, collectionName, item)
	}

	if err != nil {
		log.Printf("Error adding to watchlist: %v", err)
		return &callback.Response{
			Text:     "❌ 添加失败",
			ShowAlert: true,
		}, nil
	}

	return &callback.Response{
		Text:        fmt.Sprintf("✅ 已将「%s」添加到片单", item.Title),
		CallbackMsg: "已添加",
		ShowAlert:   true,
	}, nil
}

// removeFromWatchlist removes item from watchlist
func (h *WatchlistHandler) removeFromWatchlist(ctx *callback.Context) (*callback.Response, error) {
	parts := strings.Split(ctx.Callback.Raw, ":")
	if len(parts) < 2 {
		return &callback.Response{
			Text:     "❌ 参数错误",
			ShowAlert: true,
		}, nil
	}

	tmdbID, err := strconv.Atoi(parts[1])
	if err != nil {
		return &callback.Response{
			Text:     "❌ 无效的影片ID",
			ShowAlert: true,
		}, nil
	}

	err = h.watchlist.RemoveFromWatchlist(ctx.UserID, tmdbID)
	if err != nil {
		return &callback.Response{
			Text:     fmt.Sprintf("❌ %v", err),
			ShowAlert: true,
		}, nil
	}

	return &callback.Response{
		Text:     "✅ 已从片单中移除",
		Edit:     true,
	}, nil
}

// showCollections displays user's collections
func (h *WatchlistHandler) showCollections(ctx *callback.Context) (*callback.Response, error) {
	collections := h.watchlist.GetCollections(ctx.UserID)

	if len(collections) == 0 {
		return &callback.Response{
			Text: "📁 你还没有创建收藏夹\n\n点击下方按钮创建一个新收藏夹来组织你的片单",
			Edit: true,
			Keyboard: &callback.Keyboard{
				InlineKeyboard: [][]callback.Button{
					{{Text: "➕ 创建收藏夹", CallbackData: "watchlist_create_collection"}},
					{{Text: "◀️ 返回", CallbackData: "watchlist"}},
				},
			},
		}, nil
	}

	var text strings.Builder
	text.WriteString("📁 我的收藏夹\n\n")

	for _, col := range collections {
		text.WriteString(fmt.Sprintf("📂 %s\n", col.Name))
		if col.Description != "" {
			text.WriteString(fmt.Sprintf("   %s\n", col.Description))
		}
		text.WriteString(fmt.Sprintf("   %d 部影片\n\n", len(col.Items)))
	}

	keyboard := &callback.Keyboard{}
	for _, col := range collections {
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []callback.Button{
			{Text: fmt.Sprintf("📂 %s (%d)", col.Name, len(col.Items)), CallbackData: fmt.Sprintf("watchlist_collection_items:%s", col.Name)},
		})
	}

	keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []callback.Button{
		{Text: "➕ 创建收藏夹", CallbackData: "watchlist_create_collection"},
		{Text: "◀️ 返回", CallbackData: "watchlist"},
	})

	return &callback.Response{
		Text:     text.String(),
		Edit:     true,
		Keyboard: keyboard,
	}, nil
}

// createCollection prompts user to create a new collection
func (h *WatchlistHandler) createCollection(ctx *callback.Context) (*callback.Response, error) {
	// Set a flag in session to indicate we're waiting for collection name
	sess := h.sessionMgr.Get(ctx.UserID)
	sess.Set("creating_collection", true)

	return &callback.Response{
		Text: "✏️ 请输入收藏夹名称\n\n输入格式: <名称>[:<描述>]\n示例: \"科幻片:我最喜欢的科幻电影\"\n\n发送「取消」退出",
		Edit: true,
	}, nil
}

// showCollectionItems displays items in a specific collection
func (h *WatchlistHandler) showCollectionItems(ctx *callback.Context) (*callback.Response, error) {
	parts := strings.Split(ctx.Callback.Raw, ":")
	if len(parts) < 3 {
		return &callback.Response{
			Text:     "❌ 参数错误",
			ShowAlert: true,
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
			Text:     "❌ 收藏夹不存在",
			ShowAlert: true,
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
	text.WriteString(fmt.Sprintf("📂 %s\n\n", targetCollection.Name))
	if targetCollection.Description != "" {
		text.WriteString(fmt.Sprintf("%s\n\n", targetCollection.Description))
	}

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
			{{Text: "🔄 刷新", CallbackData: fmt.Sprintf("watchlist_collection_items:%s", collectionName)}},
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
			Text: "✅ 已取消创建收藏夹",
		}
	}

	parts := strings.SplitN(text, ":", 2)
	name := strings.TrimSpace(parts[0])
	description := ""
	if len(parts) > 1 {
		description = strings.TrimSpace(parts[1])
	}

	if name == "" {
		return &callback.Response{
			Text: "❌ 收藏夹名称不能为空",
		}
	}

	collection, err := h.watchlist.CreateCollection(userID, name, description)
	if err != nil {
		log.Printf("Error creating collection: %v", err)
		return &callback.Response{
			Text: "❌ 创建收藏夹失败",
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

// extractYear extracts year from release date or first air date
func extractYear(releaseDate, firstAirDate string) int {
	year := 0
	if releaseDate != "" {
		fmt.Sscanf(releaseDate, "%d-", &year)
	} else if firstAirDate != "" {
		fmt.Sscanf(firstAirDate, "%d-", &year)
	}
	return year
}
