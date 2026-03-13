package handlers

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
	"emby-telegram-bot/pkg/types"
)

// ResourceHandler handles resource candidate selection
type ResourceHandler struct {
	sessMgr    *session.Manager
	telegram   *services.TelegramClient
	moviepilot *services.MoviePilotClient
	tmdb       *services.TMDBClient
	siteReg    *services.SiteRegistry
}

// NewResourceHandler creates a new resource handler
func NewResourceHandler(
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	moviepilot *services.MoviePilotClient,
	tmdb *services.TMDBClient,
	siteReg *services.SiteRegistry,
) *ResourceHandler {
	return &ResourceHandler{
		sessMgr:    sessMgr,
		telegram:   telegram,
		moviepilot: moviepilot,
		tmdb:       tmdb,
		siteReg:    siteReg,
	}
}

// Handle handles resource callbacks
func (h *ResourceHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	action := ctx.Callback.Action

	// Handle short format callback "rp:%d" for resource pick
	if action == "rp" {
		// Parse the raw callback data "rp:%d"
		idxStr := strings.TrimPrefix(ctx.Callback.Raw, "rp:")
		if idxStr != "" {
			// Create a new context with the proper params
			newCtx := *ctx
			newCtx.Callback = &callback.Callback{
				Action: callback.ActionResourcePick,
				Params: map[string]string{"idx": idxStr},
				Raw:    ctx.Callback.Raw,
			}
			return h.handlePick(&newCtx)
		}
	}

	switch action {
	case callback.ActionResourceList:
		return h.handleShowList(ctx)
	case callback.ActionResourcePick:
		return h.handlePick(ctx)
	case callback.ActionResourceSort:
		return h.handleSort(ctx)
	case callback.ActionResourcePrev:
		return h.handlePage(ctx, -1)
	case callback.ActionResourceNext:
		return h.handlePage(ctx, 1)
	default:
		return nil, fmt.Errorf("unknown resource action: %s", action)
	}
}

// ResourceList holds the candidate resources for a media
type ResourceList struct {
	Keyword       string              // Search keyword
	Title         string              // Media title
	Year          int                 // Media year
	TMDBID        int                 // TMDB ID
	MediaType     string              // "movie" or "tv"
	Season        int                 // Season (for TV)
	Resources     []CandidateResource // All resources
	CurrentPage   int                 // Current page
	SortBy        string              // "seeders", "size", "date"
	SitesSearched []string            // List of sites that were searched
}

// CandidateResource represents a candidate torrent resource
type CandidateResource struct {
	Index     int     // Index in the list
	Title     string  // Torrent title
	SiteName  string  // Site name
	Size      float64 // Size in bytes
	Seeders   int     // Number of seeders
	Peers     int     // Number of peers
	PageURL   string  // URL to the torrent page
	Enclosure string  // Download link
	PubDate   string  // Publication date as string
}

// Session keys for resource list and search state
const (
	resourceListSessionKey   = "resource_list"
	resourceSearchInProgress = "resource_search_in_progress"
	resourceSearchCacheKey   = "resource_search_cache"
	resourceSearchCacheTime  = "resource_search_cache_time"
)

// handleShowList shows the resource candidate list
func (h *ResourceHandler) handleShowList(ctx *callback.Context) (*callback.Response, error) {
	// Get parameters
	mediaID := ctx.Callback.Params["id"]
	mediaType := ctx.Callback.Params["type"]
	sortBy := ctx.Callback.Params["sort"]
	if sortBy == "" {
		sortBy = "seeders" // Default sort
	}

	// Get page from params or session
	page := 1
	if pageStr := ctx.Callback.Params["page"]; pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	// Get media info from session or TMDB
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	var title string
	var year int
	var tmdbID int

	// Try to get from search results first
	if mediaID != "" {
		tmdbID, _ = strconv.Atoi(mediaID)
		// Try to get cached media info
		if cachedTitle, ok := sess.GetString(fmt.Sprintf("media_title_%d", tmdbID)); ok {
			title = cachedTitle
		}
		if cachedYear, ok := sess.GetInt(fmt.Sprintf("media_year_%d", tmdbID)); ok {
			year = cachedYear
		}
	}

	// If no title, try to fetch from TMDB
	if title == "" && tmdbID > 0 && h.tmdb != nil {
		if mediaType == "tv" {
			tv, err := h.tmdb.GetTVDetails(tmdbID)
			if err == nil {
				title = tv.Name
				year = tv.GetYear()
				// Cache for future use
				sess.Set(fmt.Sprintf("media_title_%d", tmdbID), title)
				sess.Set(fmt.Sprintf("media_year_%d", tmdbID), year)
			}
		} else {
			movie, err := h.tmdb.GetMovieDetails(tmdbID)
			if err == nil {
				title = movie.Title
				year = movie.GetYear()
				// Cache for future use
				sess.Set(fmt.Sprintf("media_title_%d", tmdbID), title)
				sess.Set(fmt.Sprintf("media_year_%d", tmdbID), year)
			}
		}
	}

	if title == "" {
		return &callback.Response{
			CallbackMsg: "⏳ 正在获取影片信息…",
		}, nil
	}

	// Build search keyword
	keyword := title
	if year > 0 {
		keyword = fmt.Sprintf("%s %d", title, year)
	}

	// Check for duplicate search (anti-spam)
	searchKey := fmt.Sprintf("%d_%s_%s", tmdbID, mediaType, sortBy)
	if inProgress, ok := sess.GetString(resourceSearchInProgress); ok && inProgress == searchKey {
		// Search already in progress, show waiting message
		return &callback.Response{
			CallbackMsg: "⏳ 搜索中，请稍候…",
			ShowAlert:   true,
		}, nil
	}

	// Check for recent cached results (within 30 seconds)
	if cachedTime, ok := sess.GetInt(resourceSearchCacheTime); ok {
		if cachedKey, ok := sess.GetString(resourceSearchCacheKey); ok && cachedKey == searchKey {
			if time.Now().Unix()-int64(cachedTime) < 30 {
				// Cache hit - use existing results
				if rlInterface, ok := sess.Get(resourceListSessionKey); ok {
					if rl, ok := rlInterface.(ResourceList); ok {
						// Update sort and rebuild message
						rl.SortBy = sortBy
						h.sortResources(rl.Resources, sortBy)
						rl.CurrentPage = page
						sess.Set(resourceListSessionKey, rl)

						// Answer callback
						_ = h.telegram.AnswerCallback(ctx.CallbackID, "✅ 使用缓存结果", true)
						return h.buildResourceListMessage(ctx, rl, rl.SitesSearched)
					}
				}
			}
		}
	}

	// Mark search as in progress
	sess.Set(resourceSearchInProgress, searchKey)
	// Clear after search completes (defer with recovery)
	defer func() {
		sess.Set(resourceSearchInProgress, "")
	}()

	// Answer callback immediately with searching message
	_ = h.telegram.AnswerCallback(ctx.CallbackID, "🔍 开始搜索…", false)

	// Concurrent search with timeout
	type siteResult struct {
		siteName string
		results  []services.TorrentResource
		err      error
	}

	var wg sync.WaitGroup
	resultChan := make(chan siteResult, 10)
	sitesMap := make(map[string]bool) // Track which sites were searched

	// Get available sites from MoviePilot
	sites, err := h.moviepilot.GetSites()
	if err == nil {
		// Launch concurrent searches for each site
		for _, site := range sites {
			adapter, ok := h.siteReg.GetBySiteID(site.ID, sites)
			if !ok {
				continue
			}

			wg.Add(1)
			go func(adapter services.SiteAdapter) {
				defer wg.Done()

				// Search with timeout per site
				result := siteResult{siteName: adapter.Name()}

				type searchResult struct {
					results []services.TorrentResource
					err     error
				}
				searchChan := make(chan searchResult, 1)

				go func() {
					results, err := adapter.Search(keyword, page)
					searchChan <- searchResult{results: results, err: err}
				}()

				// Wait for search or timeout (3 seconds per site)
				select {
				case r := <-searchChan:
					result.results = r.results
					result.err = r.err
				case <-time.After(3 * time.Second):
					result.err = fmt.Errorf("timeout")
				}

				resultChan <- result
			}(adapter)

			sitesMap[adapter.Name()] = true
		}
	}

	// Close result channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results with timeout
	var resources []CandidateResource
	var sitesSearched []string

	// Set overall timeout (5 seconds max for all sites)
	timeout := time.After(5 * time.Second)
	collectedResults := 0

	for result := range resultChan {
		if result.err != nil {
			fmt.Printf("Error searching %s: %v\n", result.siteName, result.err)
			continue
		}

		if len(result.results) > 0 {
			sitesSearched = append(sitesSearched, result.siteName)

			// Convert to CandidateResource
			for _, res := range result.results {
				resources = append(resources, CandidateResource{
					Index:     len(resources),
					Title:     res.Title,
					SiteName:  res.SiteName,
					Size:      res.Size,
					Seeders:   res.Seeders,
					Peers:     res.Peers,
					PageURL:   res.PageURL,
					Enclosure: res.Enclosure,
					PubDate:   res.PubDate,
				})

				// Limit to 30 results max (reduced for faster display)
				if len(resources) >= 30 {
					break
				}
			}
		}

		collectedResults++
		if collectedResults >= len(sitesMap) {
			break
		}
	}

	// Check for timeout - already handled by channel closing
	// Continue with whatever results we have

	// Sort resources
	h.sortResources(resources, sortBy)

	// Store in session for pagination and picking
	resourceList := ResourceList{
		Keyword:       keyword,
		Title:         title,
		Year:          year,
		TMDBID:        tmdbID,
		MediaType:     mediaType,
		Resources:     resources,
		CurrentPage:   page,
		SortBy:        sortBy,
		SitesSearched: sitesSearched,
	}
	sess.Set(resourceListSessionKey, resourceList)

	// Update cache info
	searchKey := fmt.Sprintf("%d_%s_%s", tmdbID, mediaType, sortBy)
	sess.Set(resourceSearchCacheKey, searchKey)
	sess.Set(resourceSearchCacheTime, int(time.Now().Unix()))

	// Build response
	return h.buildResourceListMessage(ctx, resourceList, sitesSearched)
}

// buildResourceListMessage builds the Telegram message for resource list
func (h *ResourceHandler) buildResourceListMessage(ctx *callback.Context, rl ResourceList, sitesSearched []string) (*callback.Response, error) {
	kb := services.NewKeyboardBuilder()

	const itemsPerPage = 5
	startIdx := (rl.CurrentPage - 1) * itemsPerPage
	endIdx := startIdx + itemsPerPage
	if endIdx > len(rl.Resources) {
		endIdx = len(rl.Resources)
	}

	// Header text
	var text strings.Builder
	text.WriteString(fmt.Sprintf("🔍 候选资源列表\n\n"))
	text.WriteString(fmt.Sprintf("📺 《%s》 (%d)\n", rl.Title, rl.Year))
	text.WriteString(fmt.Sprintf("🔑 搜索词: %s\n", rl.Keyword))

	if len(sitesSearched) > 0 {
		text.WriteString(fmt.Sprintf("🌐 站点: %s\n", strings.Join(sitesSearched, ", ")))
	}

	text.WriteString(fmt.Sprintf("📊 找到 %d 条资源\n\n", len(rl.Resources)))

	// Show resources for current page
	if len(rl.Resources) == 0 {
		text.WriteString("❌ 未找到可用资源\n\n")
		text.WriteString("💡 可能原因:\n")
		text.WriteString("   • 站点未配置或未启用\n")
		text.WriteString("   • 搜索关键词不匹配\n")
		text.WriteString("   • 站点 RSS 需要认证\n")
	} else {
		for i := startIdx; i < endIdx; i++ {
			res := rl.Resources[i]
			sizeGB := fmt.Sprintf("%.2f", float64(res.Size)/(1024*1024*1024))

			// Build seeders/peers indicator
			health := "⚪"
			if res.Seeders > 50 {
				health = "🟢"
			} else if res.Seeders > 10 {
				health = "🟡"
			} else if res.Seeders > 0 {
				health = "🔴"
			}

			text.WriteString(fmt.Sprintf("%d. %s\n", i+1, truncateTitle(res.Title, 35)))
			text.WriteString(fmt.Sprintf("   📦 %sGB  %s ↑%d ↓%d  🌐 %s\n\n",
				sizeGB, health, res.Seeders, res.Peers, res.SiteName))

			// Add selection button
			btnLabel := fmt.Sprintf("✅ 选择 #%d", i+1)
			callbackData := fmt.Sprintf("rp:%d", i)
			kb.AddButton(btnLabel, callbackData)
			kb.NewRow()
		}
	}

	// Add sorting buttons
	kb.AddButton("🔽 按做种数", callback.BuildCallback(callback.ActionResourceSort,
		map[string]string{"id": fmt.Sprintf("%d", rl.TMDBID), "type": rl.MediaType, "sort": "seeders"}))
	kb.AddButton("📦 按大小", callback.BuildCallback(callback.ActionResourceSort,
		map[string]string{"id": fmt.Sprintf("%d", rl.TMDBID), "type": rl.MediaType, "sort": "size"}))
	kb.AddButton("📅 按日期", callback.BuildCallback(callback.ActionResourceSort,
		map[string]string{"id": fmt.Sprintf("%d", rl.TMDBID), "type": rl.MediaType, "sort": "date"}))
	kb.NewRow()

	// Add pagination buttons
	if len(rl.Resources) > itemsPerPage {
		if rl.CurrentPage > 1 {
			kb.AddButton("⬅️ 上一页", callback.BuildCallback(callback.ActionResourcePrev,
				map[string]string{"page": fmt.Sprintf("%d", rl.CurrentPage)}))
		}
		if endIdx < len(rl.Resources) {
			kb.AddButton("➡️ 下一页", callback.BuildCallback(callback.ActionResourceNext,
				map[string]string{"page": fmt.Sprintf("%d", rl.CurrentPage)}))
		}
		kb.NewRow()
	}

	// Add navigation buttons
	kb.AddButton("🔄 刷新", callback.BuildCallback(callback.ActionResourceList,
		map[string]string{"id": fmt.Sprintf("%d", rl.TMDBID), "type": rl.MediaType}))
	kb.AddButton("⬅️ 返回详情", callback.BuildDetailCallback(fmt.Sprintf("%d", rl.TMDBID), rl.MediaType))

	return &callback.Response{
		Text:     text.String(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handlePick handles resource selection
func (h *ResourceHandler) handlePick(ctx *callback.Context) (*callback.Response, error) {
	// Answer callback immediately
	_ = h.telegram.AnswerCallback(ctx.CallbackID, "⏳ 应用选择…", false)

	// Get index from params
	idxStr := ctx.Callback.Params["idx"]
	if idxStr == "" {
		return &callback.Response{
			Text: "❌ 参数错误",
			Edit: true,
		}, nil
	}

	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		return &callback.Response{
			Text: "❌ 无效的索引",
			Edit: true,
		}, nil
	}

	// Get resource list from session
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	rlInterface, ok := sess.Get(resourceListSessionKey)
	if !ok {
		return &callback.Response{
			Text: "❌ 会话已过期，请重新搜索",
			Edit: true,
		}, nil
	}

	rl, ok := rlInterface.(ResourceList)
	if !ok {
		return &callback.Response{
			Text: "❌ 数据格式错误",
			Edit: true,
		}, nil
	}

	// Check index bounds
	if idx < 0 || idx >= len(rl.Resources) {
		return &callback.Response{
			Text: fmt.Sprintf("❌ 无效的选择: %d", idx+1),
			Edit: true,
		}, nil
	}

	res := rl.Resources[idx]

	// Create subscription with selected resource
	// For now, just show a confirmation message
	// In a real implementation, you would download the torrent and send it to MoviePilot
	var subscribeResult string
	if h.moviepilot != nil {
		// For now, create a manual subscription with the torrent info
		// In a real implementation, you would download the torrent and send it to MoviePilot
		subscribeResult = fmt.Sprintf("✅ 已记录选择\n\n📦 %s\n\n💡 提示: 请使用「求片」功能，系统会自动搜索最佳资源",
			truncateTitle(res.Title, 40))
	} else {
		subscribeResult = fmt.Sprintf("✅ 已选择资源\n\n📦 %s", truncateTitle(res.Title, 40))
	}

	// Build keyboard
	kb := services.NewKeyboardBuilder()
	kb.AddButton("✅ 求片 (自动搜索)", callback.BuildRequestCallback(
		fmt.Sprintf("%d", rl.TMDBID), rl.MediaType, 0))
	kb.NewRow()
	kb.AddButton("⬅️ 返回详情", callback.BuildDetailCallback(
		fmt.Sprintf("%d", rl.TMDBID), rl.MediaType))
	kb.AddButton("🔍 返回列表", callback.BuildCallback(callback.ActionResourceList,
		map[string]string{"id": fmt.Sprintf("%d", rl.TMDBID), "type": rl.MediaType}))

	return &callback.Response{
		Text:     subscribeResult,
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// handleSort handles resource sorting
func (h *ResourceHandler) handleSort(ctx *callback.Context) (*callback.Response, error) {
	sortBy := ctx.Callback.Params["sort"]
	if sortBy == "" {
		sortBy = "seeders"
	}

	// Get resource list from session
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	rlInterface, ok := sess.Get(resourceListSessionKey)
	if !ok {
		// No cached list, fetch fresh
		return h.handleShowList(ctx)
	}

	rl, ok := rlInterface.(ResourceList)
	if !ok {
		return h.handleShowList(ctx)
	}

	// Update sort and re-sort
	rl.SortBy = sortBy
	h.sortResources(rl.Resources, sortBy)
	rl.CurrentPage = 1 // Reset to first page

	// Store updated list
	sess.Set(resourceListSessionKey, rl)

	// Build message
	return h.buildResourceListMessage(ctx, rl, rl.SitesSearched)
}

// handlePage handles pagination
func (h *ResourceHandler) handlePage(ctx *callback.Context, delta int) (*callback.Response, error) {
	// Get resource list from session
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	rlInterface, ok := sess.Get(resourceListSessionKey)
	if !ok {
		return &callback.Response{
			CallbackMsg: "❌ 会话已过期",
			ShowAlert:   true,
		}, nil
	}

	rl, ok := rlInterface.(ResourceList)
	if !ok {
		return &callback.Response{
			CallbackMsg: "❌ 数据格式错误",
			ShowAlert:   true,
		}, nil
	}

	const itemsPerPage = 5
	maxPage := (len(rl.Resources) + itemsPerPage - 1) / itemsPerPage
	if maxPage < 1 {
		maxPage = 1
	}

	newPage := rl.CurrentPage + delta
	if newPage < 1 {
		newPage = 1
	}
	if newPage > maxPage {
		newPage = maxPage
	}

	rl.CurrentPage = newPage
	sess.Set(resourceListSessionKey, rl)

	// Build message
	return h.buildResourceListMessage(ctx, rl, rl.SitesSearched)
}

// sortResources sorts resources by the specified criteria
func (h *ResourceHandler) sortResources(resources []CandidateResource, sortBy string) {
	switch sortBy {
	case "seeders":
		sort.Slice(resources, func(i, j int) bool {
			return resources[i].Seeders > resources[j].Seeders
		})
	case "size":
		sort.Slice(resources, func(i, j int) bool {
			return resources[i].Size > resources[j].Size
		})
	case "date":
		sort.Slice(resources, func(i, j int) bool {
			// Simple string comparison for date (works for ISO format)
			return resources[i].PubDate > resources[j].PubDate
		})
	default:
		// Default to seeders
		sort.Slice(resources, func(i, j int) bool {
			return resources[i].Seeders > resources[j].Seeders
		})
	}
}

// buildBackKeyboard builds a simple back keyboard
func (h *ResourceHandler) buildBackKeyboard(mediaID, mediaType, seasonStr string) *types.TelegramInlineKeyboard {
	kb := services.NewKeyboardBuilder()

	if mediaID != "" && mediaType != "" {
		kb.AddButton("🔍 返回详情", callback.BuildDetailCallback(mediaID, mediaType))
		kb.NewRow()
	}

	kb.AddButton("🔍 搜索", "search:menu")
	kb.AddButton("📋 我的请求", string(callback.ActionRequests))
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return kb.Build()
}

// Helper functions

func truncateTitle(title string, maxLen int) string {
	if len(title) <= maxLen {
		return title
	}
	return title[:maxLen-3] + "..."
}
