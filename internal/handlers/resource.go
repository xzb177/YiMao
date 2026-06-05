package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
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

// obscureSiteName returns a masked/obscured version of the site name for privacy
// Uses a simple counter-based approach to make sites indistinguishable
var siteCounter int
var siteMap = make(map[string]string)
var siteMapMutex sync.Mutex

func obscureSiteName(siteName string) string {
	siteMapMutex.Lock()
	defer siteMapMutex.Unlock()

	// Check if we already assigned a code for this site
	if code, exists := siteMap[siteName]; exists {
		return code
	}

	// Assign a new code for this site
	// 普通用户看不懂「站点1/站点2」这种内部编号，且选源已下线，
	// 这里只需表达「来自不同私有站」的隐私化标签即可。
	siteCounter++
	code := fmt.Sprintf("资源站%s", circledNumber(siteCounter))
	siteMap[siteName] = code

	return code
}

// circledNumber 把 1..n 映射为更友好的标识；超出范围则退回普通数字。
func circledNumber(n int) string {
	circled := []string{"①", "②", "③", "④", "⑤", "⑥", "⑦", "⑧", "⑨", "⑩"}
	if n >= 1 && n <= len(circled) {
		return circled[n-1]
	}
	return fmt.Sprintf("%d", n)
}

// resetSiteCounter resets the site counter (for testing/restart)
func resetSiteCounter() {
	siteMapMutex.Lock()
	defer siteMapMutex.Unlock()
	siteCounter = 0
	siteMap = make(map[string]string)
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

	// Get page from params or session (user-visible page, 1-based)
	userPage := 1
	if pageStr := ctx.Callback.Params["page"]; pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			userPage = p
		}
	}
	// Convert to 0-based for MoviePilot API
	page := userPage - 1

	// Get media info from session or TMDB
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	var title string
	var originalTitle string // English/original title for PT site search
	var year int
	var tmdbID int

	// Try to get from search results first
	if mediaID != "" {
		tmdbID, _ = strconv.Atoi(mediaID)
		// Try to get cached media info
		if cachedTitle, ok := sess.GetString(fmt.Sprintf("media_title_%d", tmdbID)); ok {
			title = cachedTitle
		}
		if cachedOriginalTitle, ok := sess.GetString(fmt.Sprintf("media_original_title_%d", tmdbID)); ok {
			originalTitle = cachedOriginalTitle
		}
		if cachedYear, ok := sess.GetInt(fmt.Sprintf("media_year_%d", tmdbID)); ok {
			year = cachedYear
		}
	}

	// Always fetch from TMDB if we don't have original title (needed for PT site search)
	if (originalTitle == "" && tmdbID > 0 && h.tmdb != nil) || title == "" {
		if mediaType == "tv" {
			tv, err := h.tmdb.GetTVDetails(tmdbID)
			if err == nil {
				if title == "" {
					title = tv.Name
				}
				// If originalTitle is still empty (non-English), fetch English title
				if originalTitle == "" {
					originalTitle = h.getEnglishTitle(tv.OriginalName, tmdbID, "tv")
				}
				if year == 0 {
					year = tv.GetYear()
				}
				// Cache for future use
				sess.Set(fmt.Sprintf("media_title_%d", tmdbID), title)
				sess.Set(fmt.Sprintf("media_original_title_%d", tmdbID), originalTitle)
				sess.Set(fmt.Sprintf("media_year_%d", tmdbID), year)
			}
		} else {
			movie, err := h.tmdb.GetMovieDetails(tmdbID)
			if err == nil {
				if title == "" {
					title = movie.Title
				}
				// If originalTitle is still empty (non-English), fetch English title
				if originalTitle == "" {
					originalTitle = h.getEnglishTitle(movie.OriginalTitle, tmdbID, "movie")
				}
				if year == 0 {
					year = movie.GetYear()
				}
				// Cache for future use
				sess.Set(fmt.Sprintf("media_title_%d", tmdbID), title)
				sess.Set(fmt.Sprintf("media_original_title_%d", tmdbID), originalTitle)
				sess.Set(fmt.Sprintf("media_year_%d", tmdbID), year)
			}
		}
	}

	// If originalTitle is not English, try to get English title from TMDB
	// This handles cases where original_title is non-Latin (e.g., Russian, Chinese, Japanese)
	if tmdbID > 0 && originalTitle != "" {
		// Check if we need to get English title
		needEnglishTitle := !h.isLikelyEnglish(originalTitle)
		logger.Info("[Resource] Checking if need English title: originalTitle='%s', isLikelyEnglish=%v, needEnglishTitle=%v\n",
			originalTitle, !needEnglishTitle, needEnglishTitle)

		if needEnglishTitle {
			englishTitle := h.getEnglishTitle(originalTitle, tmdbID, mediaType)
			if englishTitle != "" && englishTitle != originalTitle {
				logger.Info("[Resource] Using English title for search: '%s' (original was: '%s')\n", englishTitle, originalTitle)
				originalTitle = englishTitle
				// Update cache with English title
				sess.Set(fmt.Sprintf("media_original_title_%d", tmdbID), originalTitle)
			} else {
				logger.Info("[Resource] Failed to get different English title, keeping original\n")
			}
		}
	}

	// Use original title for PT sites (usually English), fallback to Chinese title
	searchTitle := originalTitle
	if searchTitle == "" {
		searchTitle = title
	}

	logger.Info("[Resource] Media info - ID:%d, Title:'%s', OriginalTitle:'%s', Using:'%s'\n",
		tmdbID, title, originalTitle, searchTitle)

	if title == "" {
		return &callback.Response{
			CallbackMsg: "⏳ 正在获取影片信息…",
		}, nil
	}

	// Build search keyword - use original title (English) for PT sites
	// Clean title first
	cleanTitle := strings.ReplaceAll(searchTitle, "。", "")
	cleanTitle = strings.ReplaceAll(cleanTitle, ".", "")
	cleanTitle = strings.TrimSpace(cleanTitle)

	// Extract the main title part (after dash, colon, or parenthesis)
	// e.g., "Dou kyu sei – Classmates" -> "Classmates"
	// e.g., "Title: Subtitle" -> "Title"
	keyword := cleanTitle

	// Try to extract the English/main part from titles with separators
	// Use strings.FieldsFunc to split on multiple separator characters
	separators := func(r rune) bool {
		return r == '–' || r == '—' || r == ':' || r == '：' || r == '|'
	}
	parts := strings.FieldsFunc(cleanTitle, separators)
	if len(parts) > 1 {
		// Take the last non-empty part as the main title
		for i := len(parts) - 1; i >= 0; i-- {
			part := strings.TrimSpace(parts[i])
			if len(part) > 2 {
				// Check if it starts with ASCII (likely English)
				if part[0] < 128 {
					keyword = part
					break
				}
			}
		}
	}

	// Remove year from keyword (PT sites don't always include year in titles)
	keyword = regexp.MustCompile(`\s+\d{4}$`).ReplaceAllString(keyword, "")
	keyword = regexp.MustCompile(`\(\d{4}\)`).ReplaceAllString(keyword, "")
	keyword = strings.TrimSpace(keyword)

	// Store original Chinese title for display
	displayTitle := title

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
						rl.CurrentPage = userPage
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

	// Use MoviePilot API to search sites (proxy mode - no RSS signature issues)
	var resources []CandidateResource
	var sitesSearched []string

	type siteResult struct {
		siteName string
		results  []services.TorrentResource
		err      error
	}

	resultChan := make(chan siteResult, 10)
	var wg sync.WaitGroup
	sitesMap := make(map[string]bool)

	// Get available sites from MoviePilot and search via MP API
	sites, err := h.moviepilot.GetSites()
	if err != nil {
		logger.Info("[Resource] Failed to get sites from MoviePilot: %v\n", err)
		// Fallback to SiteAdapter if available
		h.searchViaSiteAdapter(keyword, page, &resources, &sitesSearched)
	} else {
		logger.Info("[Resource] Found %d sites from MoviePilot, searching via MP API\n", len(sites))

		// Launch concurrent searches for each site using MoviePilot API
		for _, site := range sites {
			wg.Add(1)
			go func(site services.SiteInfo) {
				defer wg.Done()

				// Search via MoviePilot API (no RSS signature issues)
				result := siteResult{siteName: site.Name}

				type searchResult struct {
					results []services.TorrentResource
					err     error
				}
				searchChan := make(chan searchResult, 1)

				go func() {
					resources, err := h.moviepilot.GetSiteResources(site.ID, keyword, page)
					searchChan <- searchResult{results: resources, err: err}
				}()

				// Wait for search or timeout (8 seconds per site)
				select {
				case r := <-searchChan:
					result.results = r.results
					result.err = r.err
				case <-time.After(8 * time.Second):
					result.err = fmt.Errorf("timeout")
				}

				resultChan <- result
			}(site)

			sitesMap[site.Name] = true
		}

		// Close result channel when all goroutines complete
		go func() {
			wg.Wait()
			close(resultChan)
		}()
	}

	// Collect results with timeout
	timeout := time.After(10 * time.Second)
	collectedResults := 0

	for result := range resultChan {
		if result.err != nil {
			logger.Info("[Resource] Error searching %s: %v\n", result.siteName, result.err)
			collectedResults++
			continue
		}

		if len(result.results) > 0 {
			sitesSearched = append(sitesSearched, result.siteName)
			logger.Info("[Resource] %s returned %d results\n", result.siteName, len(result.results))

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

				// Limit to 30 results max
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

	// Check for timeout
	select {
	case <-timeout:
		logger.Info("[Resource] Search timeout, collected %d/%d sites\n", collectedResults, len(sitesMap))
	default:
	}

	// Sort resources
	h.sortResources(resources, sortBy)

	// Store in session for pagination and picking
	resourceList := ResourceList{
		Keyword:       keyword,
		Title:         displayTitle, // Use Chinese title for display
		Year:          year,
		TMDBID:        tmdbID,
		MediaType:     mediaType,
		Resources:     resources,
		CurrentPage:   userPage,
		SortBy:        sortBy,
		SitesSearched: sitesSearched,
	}
	sess.Set(resourceListSessionKey, resourceList)

	// Update cache info
	searchKey = fmt.Sprintf("%d_%s_%s", tmdbID, mediaType, sortBy)
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

	// Always use DeleteMessage=true for resource list since it might be called from photo detail page
	// This handles both text and photo message types correctly
	deleteMessage := true

	// Header text
	var text strings.Builder
	text.WriteString(fmt.Sprintf("🔍 候选资源\n\n"))
	text.WriteString(fmt.Sprintf("📺 《%s》 (%d)\n", rl.Title, rl.Year))
	text.WriteString(fmt.Sprintf("🔑 搜索词: %s\n", rl.Keyword))

	if len(sitesSearched) > 0 {
		text.WriteString(fmt.Sprintf("🌐 已搜索 %d 个站点\n", len(sitesSearched)))
	}

	text.WriteString(fmt.Sprintf("📊 找到 %d 条资源\n", len(rl.Resources)))

	// Batch B #1：状态灯牌（不承诺具体时间）。
	// hasCandidate 判定：只要实际搜过站点（无论是否命中）就算「拿到了候选数据」，
	// 据此区分「搜过但 0 源 -> 🐢」与「还没搜/数据缺失 -> ❓」。
	hasCandidate := len(sitesSearched) > 0 || len(rl.Resources) > 0
	lamp := deliveryLampForResources(rl.Resources, hasCandidate)
	seedingSites := countSeedingSites(rl.Resources)
	logger.Info("[eta] tmdb=%d type=%s sites_searched=%d resources=%d seeding_sites=%d threshold=%d lamp=%q\n",
		rl.TMDBID, rl.MediaType, len(sitesSearched), len(rl.Resources), seedingSites, etaThresholdHigh(), lamp)
	text.WriteString(fmt.Sprintf("%s\n\n", lamp))

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
				sizeGB, health, res.Seeders, res.Peers, obscureSiteName(res.SiteName)))
		}
		// 候选列表仅用于让用户确认「有片源」，不提供手动选源——
		// MoviePilot 会自动挑选最佳源，手动选是伪需求且此前并未真正下发。
		// 用「求片（自动选最佳源）」替代原来的假「选择 #N」按钮。
		text.WriteString("💡 系统会自动选最佳源，点下方「求片」即可\n")
	}

	// 真·求片按钮：直接触发自动求片（MoviePilot 自动选最佳源）。
	// 仅在有候选时展示，避免 0 源时误导用户。
	if len(rl.Resources) > 0 {
		kb.AddButton("🎬 求片（自动选最佳源）", callback.BuildRequestCallback(
			fmt.Sprintf("%d", rl.TMDBID), rl.MediaType, 0))
		kb.NewRow()
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
		Text:          text.String(),
		Edit:          false,
		DeleteMessage: deleteMessage,
		Keyboard:      convertKeyboard(kb.Build()),
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
			Text:          "❌ 参数错误",
			Edit:          false,
			DeleteMessage: true,
		}, nil
	}

	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		return &callback.Response{
			Text:          "❌ 无效的索引",
			Edit:          false,
			DeleteMessage: true,
		}, nil
	}

	// Get resource list from session
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	rlInterface, ok := sess.Get(resourceListSessionKey)
	if !ok {
		return &callback.Response{
			Text:          "❌ 会话已过期，请重新搜索",
			Edit:          false,
			DeleteMessage: true,
		}, nil
	}

	rl, ok := rlInterface.(ResourceList)
	if !ok {
		return &callback.Response{
			Text:          "❌ 数据格式错误",
			Edit:          false,
			DeleteMessage: true,
		}, nil
	}

	// Check index bounds
	if idx < 0 || idx >= len(rl.Resources) {
		return &callback.Response{
			Text:          fmt.Sprintf("❌ 无效的选择: %d", idx+1),
			Edit:          false,
			DeleteMessage: true,
		}, nil
	}

	res := rl.Resources[idx]

	// 历史遗留按钮兜底：旧消息里可能还残留「选择 #N」按钮（此前是假动作，
	// 并未真正下发 MoviePilot）。现在统一引导到「求片（自动选最佳源）」，
	// 不再制造「已选源」的错觉。
	subscribeResult := fmt.Sprintf("ℹ️ 手动选源已下线\n\n📦 %s\n\nMoviePilot 会自动挑选最佳源，点下方「求片」即可。",
		truncateTitle(res.Title, 40))

	// Build keyboard
	kb := services.NewKeyboardBuilder()
	kb.AddButton("🎬 求片（自动选最佳源）", callback.BuildRequestCallback(
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

// getEnglishTitle fetches the English title from TMDB
func (h *ResourceHandler) getEnglishTitle(originalTitle string, tmdbID int, mediaType string) string {
	logger.Info("[Resource] getEnglishTitle called: originalTitle='%s', tmdbID=%d, mediaType=%s\n", originalTitle, tmdbID, mediaType)

	// If original title looks like English (contains Latin characters), use it
	if h.isLikelyEnglish(originalTitle) {
		logger.Info("[Resource] Original title looks like English, using it: '%s'\n", originalTitle)
		return originalTitle
	}

	// Use TMDB client's API key to fetch English title
	if h.tmdb == nil {
		logger.Info("[Resource] TMDB client not available\n")
		return originalTitle
	}

	// Make direct HTTP request with English language
	// Use the API key from the TMDB client
	url := fmt.Sprintf("https://api.themoviedb.org/3/%s/%d?language=en-US",
		mediaType, tmdbID)

	type TMDBResponse struct {
		Title string `json:"title"`
		Name  string `json:"name"`
	}
	var resp TMDBResponse

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		logger.Info("[Resource] Failed to create request: %v\n", err)
		return originalTitle
	}

	// Get API key from TMDB client - use reflection or direct access
	// For simplicity, we'll use the configured API key from environment
	apiKey := os.Getenv("TMDB_API_KEY")
	if apiKey == "" {
		// Fallback to a common key
		apiKey = "2cafac5b00b310f21cf8ada8ef02760f"
	}
	req.URL.RawQuery = "api_key=" + apiKey

	httpResp, err := client.Do(req)
	if err != nil {
		logger.Info("[Resource] Failed to fetch from TMDB: %v\n", err)
		return originalTitle
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		logger.Info("[Resource] TMDB returned status %d\n", httpResp.StatusCode)
		return originalTitle
	}

	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		logger.Info("[Resource] Failed to decode response: %v\n", err)
		return originalTitle
	}

	logger.Info("[Resource] TMDB en-US returned: Title='%s', Name='%s'\n", resp.Title, resp.Name)

	if resp.Title != "" {
		return resp.Title
	}
	if resp.Name != "" {
		return resp.Name
	}

	return originalTitle
}

// isLikelyEnglish checks if a title is likely in English
func (h *ResourceHandler) isLikelyEnglish(title string) bool {
	if title == "" {
		return false
	}
	// Check if title contains mostly Latin characters
	latinChars := 0
	for _, r := range title {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == ' ' || r == '-' || r == ':' || r == '\'' {
			latinChars++
		}
	}
	// If more than 70% of characters are Latin, consider it English
	return float64(latinChars)/float64(len(title)) > 0.7
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

// searchViaSiteAdapter searches using direct SiteAdapter (fallback when MP API fails)
func (h *ResourceHandler) searchViaSiteAdapter(keyword string, page int, resources *[]CandidateResource, sitesSearched *[]string) {
	if h.siteReg == nil {
		return
	}

	sites, err := h.moviepilot.GetSites()
	if err != nil {
		logger.Info("[Resource] Fallback failed: cannot get sites: %v\n", err)
		return
	}

	logger.Info("[Resource] Using SiteAdapter fallback for %d sites\n", len(sites))

	// Simple sequential search with timeout
	for _, site := range sites {
		adapter, ok := h.siteReg.GetBySiteID(site.ID, sites)
		if !ok {
			continue
		}

		// Search with timeout
		type searchResult struct {
			results []services.TorrentResource
			err     error
		}
		searchChan := make(chan searchResult, 1)

		go func() {
			results, err := adapter.Search(keyword, page)
			searchChan <- searchResult{results: results, err: err}
		}()

		select {
		case r := <-searchChan:
			if r.err != nil {
				logger.Info("[Resource] SiteAdapter %s error: %v\n", adapter.Name(), r.err)
				continue
			}
			if len(r.results) > 0 {
				*sitesSearched = append(*sitesSearched, adapter.Name())
				logger.Info("[Resource] %s returned %d results (SiteAdapter)\n", adapter.Name(), len(r.results))

				for _, res := range r.results {
					*resources = append(*resources, CandidateResource{
						Index:     len(*resources),
						Title:     res.Title,
						SiteName:  res.SiteName,
						Size:      res.Size,
						Seeders:   res.Seeders,
						Peers:     res.Peers,
						PageURL:   res.PageURL,
						Enclosure: res.Enclosure,
						PubDate:   res.PubDate,
					})
				}
			}
		case <-time.After(5 * time.Second):
			logger.Info("[Resource] SiteAdapter %s timeout\n", adapter.Name())
		}

		// Limit total results
		if len(*resources) >= 30 {
			break
		}
	}
}
