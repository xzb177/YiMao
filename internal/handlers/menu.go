package handlers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/richmessage"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/errors"
	"github.com/xzb177/yimao/pkg/logger"
)

// MyRequestsHandler handles "my requests" callbacks with pagination
type MyRequestsHandler struct {
	sessMgr     *session.Manager
	telegram    *services.TelegramClient
	moviepilot  *services.MoviePilotClient
	userMapping services.UserMappingStore
	reviewSvc   *services.ReviewService
	quotaSvc    *services.QuotaService
	adminSvc    *services.AdminService
	// OnCarpoolNotify 拼车用户通知回调（撤回时触发）。
	OnCarpoolNotify func(tmdbID int, mediaType, title, reason string)
	wishSvc         *services.WishService
}

// SetAdminService 注入管理员服务（用于撤回时通知管理员）。
func (h *MyRequestsHandler) SetAdminService(svc *services.AdminService) {
	h.adminSvc = svc
}

const (
	requestsPerPage = 10 // 每页显示请求数
)

func NewMyRequestsHandler(
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	moviepilot *services.MoviePilotClient,
) *MyRequestsHandler {
	return &MyRequestsHandler{
		sessMgr:    sessMgr,
		telegram:   telegram,
		moviepilot: moviepilot,
	}
}

// SetUserMapping sets the user mapping service
func (h *MyRequestsHandler) SetUserMapping(um services.UserMappingStore) {
	h.userMapping = um
}

// SetReviewService 注入审核服务，用于把 pending/stuck 的审核单合并进「我的请求」，
// 修复「刚提交的请求在『我的请求』里看不到」（pending 存在 ReviewService，MP 还没有）。
func (h *MyRequestsHandler) SetReviewService(rs *services.ReviewService) {
	h.reviewSvc = rs
}

// SetQuotaService 注入配额服务（撤回请求时退配额）。
func (h *MyRequestsHandler) SetQuotaService(qs *services.QuotaService) {
	h.quotaSvc = qs
}

// SetWishService 注入许愿池服务（用于「我的请求」显示许愿列表）。
func (h *MyRequestsHandler) SetWishService(ws *services.WishService) {
	h.wishSvc = ws
}

// HandleCancelReview 处理用户撤回 pending 求片申请。
// 逻辑：查 ReviewService 找该用户最近一条 pending 请求 → CancelByUser → 退配额。
func (h *MyRequestsHandler) HandleCancelReview(ctx *callback.Context) (*callback.Response, error) {
	if h.reviewSvc == nil {
		return &callback.Response{
			Text:        "❌ 服务未就绪",
			CallbackMsg: "服务未就绪",
			ShowAlert:   true,
		}, nil
	}

	// 找该用户最近一条 pending 请求
	reviews := h.reviewSvc.GetUserRequests(ctx.UserID)
	var target *services.ReviewRequest
	for _, rv := range reviews {
		if rv.Status == "pending" {
			target = rv
			break // GetUserRequests 已按时间倒序，第一条就是最近的
		}
	}

	if target == nil {
		return &callback.Response{
			Text:        "📋 没有待审核的求片申请可以撤回",
			CallbackMsg: "没有可撤回的申请",
			ShowAlert:   true,
		}, nil
	}

	// 撤回
	if err := h.reviewSvc.CancelByUser(target.RequestID, ctx.UserID); err != nil {
		logger.Info("[MyRequestsHandler] 撤回失败: %v", err)
		return &callback.Response{
			Text:        "❌ 撤回失败，请稍后再试",
			CallbackMsg: "撤回失败",
			ShowAlert:   true,
		}, nil
	}

	// 退配额
	if h.quotaSvc != nil {
		_, _ = h.reviewSvc.RestoreQuotaOnce(target.RequestID, h.quotaSvc)
	}

	// 通知管理员：用户主动撤回了求片申请
	if h.adminSvc != nil {
		adminIDs := h.adminSvc.GetAdminIDs()
		notifyMsg := fmt.Sprintf("📋 用户撤回求片\n\n📺 %s\n👤 用户 %d", target.MediaTitle, ctx.UserID)
		for _, adminID := range adminIDs {
			go h.telegram.SendMessage(adminID, notifyMsg, "", nil)
		}
	}

	// 通知拼车用户
	if h.OnCarpoolNotify != nil {
		go h.OnCarpoolNotify(target.TmdbID, string(target.MediaType), target.MediaTitle, "发起人撤回")
	}

	// 通知用户
	text := fmt.Sprintf("✅ 已撤回「%s」的求片申请\n\n配额已退还，可以重新使用", target.MediaTitle)
	kb := services.NewKeyboardBuilder()
	kb.AddButton("📊 求片进度", "requests")
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     text,
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// Handle displays the first page of user requests
func (h *MyRequestsHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	return h.handleRequestsWithPage(ctx, 1)
}

// HandlePage handles pagination callbacks
func (h *MyRequestsHandler) HandlePage(ctx *callback.Context) (*callback.Response, error) {
	pageStr, hasPage := ctx.Callback.Params["page"]
	if !hasPage {
		return h.Handle(ctx)
	}
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}
	return h.handleRequestsWithPage(ctx, page)
}

// HandleItemAction handles individual item actions (reshare, cancel, etc.)
func (h *MyRequestsHandler) HandleItemAction(ctx *callback.Context) (*callback.Response, error) {
	action, hasAction := ctx.Callback.Params["action"]
	itemID, hasID := ctx.Callback.Params["id"]

	if !hasAction || !hasID {
		return &callback.Response{
			Text:        "❌ 参数无效",
			CallbackMsg: "参数错误",
			ShowAlert:   true,
		}, nil
	}

	// Get the current page to return to
	page := 1
	if pageStr, hasPage := ctx.Callback.Params["page"]; hasPage {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	switch action {
	case "reshare":
		// Trigger reshare for the subscription
		return h.handleReshare(ctx, itemID, page)
	case "cancel":
		// Cancel the subscription
		return h.handleCancel(ctx, itemID, page)
	case "info":
		// Show more info about the subscription
		return h.handleInfo(ctx, itemID, page)
	default:
		return &callback.Response{
			Text:        "❓ 未知操作",
			CallbackMsg: "未知操作",
			ShowAlert:   true,
		}, nil
	}
}

// BuildForCommand 为 /requests 命令构建「我的请求」首页（文本 + 键盘）。
// 复用与回调入口完全一致的聚合逻辑（含 ReviewService 合并），让命令不再踢皮球。
// resolveMoviePilotID 返回 0 表示用户未绑定。
func (h *MyRequestsHandler) BuildForCommand(telegramID int64) (string, *callback.Keyboard) {
	moviepilotID := int64(0)
	if h.userMapping != nil {
		if id, exists := h.userMapping.GetMoviePilotUserID(telegramID); exists {
			moviepilotID = id
		}
	}

	if moviepilotID == 0 {
		return "🔗 请先绑定账号后使用 /link", &callback.Keyboard{
			InlineKeyboard: [][]callback.Button{
				{{Text: "🔗 立即绑定", CallbackData: "link"}},
				{{Text: "⬅️ 返回", CallbackData: "start"}},
			},
		}
	}

	requests, err := h.moviepilot.GetUserRequests(moviepilotID)
	if err != nil {
		logger.Info("[MyRequestsHandler] /requests 拉取 MP 失败: %v", err)
		requests = nil
	}
	requests = h.mergePendingReviews(telegramID, requests)

	totalRequests := len(requests)
	totalPages := (totalRequests + requestsPerPage - 1) / requestsPerPage
	if totalPages == 0 {
		totalPages = 1
	}
	msg, _, kb := h.buildRequestsMessage(requests, 1, totalPages, totalRequests)
	return msg, kb
}

// handleRequestsWithPage builds the paginated requests list
func (h *MyRequestsHandler) handleRequestsWithPage(ctx *callback.Context, page int) (*callback.Response, error) {
	// Try to get MoviePilot user ID from session first
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	moviepilotID := sess.GetMoviePilotUserID()

	// If not in session, try to get from user mapping (persistent storage)
	if moviepilotID == 0 && h.userMapping != nil {
		if id, exists := h.userMapping.GetMoviePilotUserID(ctx.UserID); exists {
			moviepilotID = id
			sess.Set("moviepilot_id", int(id))
			logger.Info("[MyRequestsHandler] Loaded moviepilot_id=%d from userMapping for user %d", id, ctx.UserID)
		}
	}

	if moviepilotID == 0 {
		return &callback.Response{
			Text: "🔗 请先绑定账号后使用",
			Edit: true,
			Keyboard: &callback.Keyboard{
				InlineKeyboard: [][]callback.Button{
					{{Text: "🔗 立即绑定", CallbackData: "link"}},
					{{Text: "⬅️ 返回", CallbackData: "start"}},
				},
			},
		}, nil
	}

	// Fetch user requests — if subscription cache is not ready, return loading message
	if !h.moviepilot.IsSubscriptionCacheReady() {
		// Trigger background warmup (first call after startup)
		go h.moviepilot.WarmupSubscriptionCache()
		msg := services.NewMessageBuilder()
		msg.Bold("📋 我的请求").Newline()
		msg.Newline()
		msg.Text("⏳ 数据加载中，请稍候再试（首次加载约需 2-3 分钟）").Newline()
		msg.Newline()
		msg.Italic("💡 后续访问会很快")
		kb := services.NewKeyboardBuilder()
		kb.AddButton("🔄 刷新", "requests")
		kb.AddButton("⬅️ 返回", "start")
		return &callback.Response{
			Text:     msg.Build(),
			Edit:     true,
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}
	requests, err := h.moviepilot.GetUserRequests(moviepilotID)
	if err != nil {
		return nil, errors.MoviePilotErr("failed to get requests", err)
	}

	// 聚合：把 ReviewService 里「还没成为 MP 订阅」的审核单合并进来。
	// 主从规则：MP（执行层）离最终结果近，凡是已有 MP 订阅的请求以 MP 为准；
	// 仅当某审核单在 MP 里查不到对应订阅时，才用审核单的状态兜底显示，
	// 从而修复「pending / 已审核同步中(stuck) 在『我的请求』里看不到」。
	requests = h.mergePendingReviews(ctx.UserID, requests)

	// 合并许愿池：把用户的活跃许愿追加到列表末尾
	if h.wishSvc != nil {
		if wishes, err := h.wishSvc.ListByUser(ctx.UserID); err == nil && len(wishes) > 0 {
			for _, w := range wishes {
				typeStr := "电影"
				if w.MediaType == "tv" {
					typeStr = "电视剧"
				}
				requests = append(requests, services.SubscribeItem{
					ID:     0,
					Name:   w.Title,
					Year:   fmt.Sprintf("%d", w.Year),
					Type:   typeStr,
					State:  "WISH",
					Season: w.Season,
					TMDBID: w.TmdbID,
					Date:   w.CreatedAt.Format("2006-01-02"),
				})
			}
		}
	}

	// Calculate pagination
	totalRequests := len(requests)
	totalPages := (totalRequests + requestsPerPage - 1) / requestsPerPage
	if totalPages == 0 {
		totalPages = 1
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	// Build response message with new format
	msg, richMsg, keyboard := h.buildRequestsMessage(requests, page, totalPages, totalRequests)

	return &callback.Response{
		Text:        msg,
		RichMessage: richMsg,
		Edit:        true,
		Keyboard:    keyboard,
	}, nil
}

// buildRequestsMessage builds the paginated message with inline keyboard
func (h *MyRequestsHandler) buildRequestsMessage(requests []services.SubscribeItem, page, totalPages, totalRequests int) (string, string, *callback.Keyboard) {
	msg := services.NewMessageBuilder()

	if totalRequests == 0 {
		msg.Bold("我的请求").Newline()
		msg.Newline()
		msg.Text("暂无记录").Newline()
		msg.Newline()
		msg.Italic("搜索后点击“求片”即可添加")

		kb := &callback.Keyboard{
			InlineKeyboard: [][]callback.Button{
				{{Text: "刷新", CallbackData: callback.BuildCallback("myreqs_page", map[string]string{"page": "1"})}},
				{{Text: "返回", CallbackData: "start"}},
			},
		}
		return msg.Build(), "", kb
	}

	// Header
	msg.Bold("📋 我的请求").Newline()
	msg.Text(fmt.Sprintf("共 %d 条，第 %d/%d 页", totalRequests, page, totalPages)).Newline()
	msg.Textf("进行中 %d · 已完成 %d · 异常 %d", countStates(requests, []string{stateReviewing, stateStuck, services.StatePending, services.StateRecycled, services.StateSearching, services.StateDownloading}), countStates(requests, []string{services.StateCompleted}), countStates(requests, []string{services.StateFailed, services.StateCancelled})).Newline()
	msg.Text("────────").Newline()
	msg.Newline()

	// Calculate slice bounds
	startIdx := (page - 1) * requestsPerPage
	endIdx := startIdx + requestsPerPage
	if endIdx > totalRequests {
		endIdx = totalRequests
	}

	// Build grouped one-line items (当前页内按状态分组)
	groupOrder := []struct {
		Key    string
		Label  string
		States map[string]bool
	}{
		{Key: "processing", Label: "进行中", States: map[string]bool{stateReviewing: true, stateStuck: true, services.StatePending: true, services.StateRecycled: true, services.StateSearching: true, services.StateDownloading: true}},
		{Key: "wish", Label: "许愿中", States: map[string]bool{"WISH": true}},
		{Key: "done", Label: "已完成", States: map[string]bool{services.StateCompleted: true}},
		{Key: "failed", Label: "异常/失败", States: map[string]bool{services.StateFailed: true, services.StateCancelled: true}},
	}

	bucket := map[string][]int{
		"processing": {},
		"wish":       {},
		"done":       {},
		"failed":     {},
		"other":      {},
	}
	for i := startIdx; i < endIdx; i++ {
		state := requests[i].State
		matched := false
		for _, g := range groupOrder {
			if g.States[state] {
				bucket[g.Key] = append(bucket[g.Key], i)
				matched = true
				break
			}
		}
		if !matched {
			bucket["other"] = append(bucket["other"], i)
		}
	}

	for _, g := range groupOrder {
		idxList := bucket[g.Key]
		if len(idxList) == 0 {
			continue
		}
		msg.Textf("【%s】", g.Label).Newline()
		for _, idx := range idxList {
			req := requests[idx]
			line := h.buildRequestLine(idx+1, req)
			msg.Text(line).Newline()
		}
		msg.Newline()
	}
	if len(bucket["other"]) > 0 {
		msg.Text("【其他】").Newline()
		for _, idx := range bucket["other"] {
			req := requests[idx]
			line := h.buildRequestLine(idx+1, req)
			msg.Text(line).Newline()
		}
		msg.Newline()
	}

	// Build keyboard
	kb := h.buildRequestsKeyboard(requests, page, totalPages, startIdx, endIdx)
	richMsg := h.buildRequestsRichMessage(requests, page, totalPages, totalRequests, startIdx, endIdx)

	return msg.Build(), richMsg, kb
}

func (h *MyRequestsHandler) buildRequestsRichMessage(requests []services.SubscribeItem, page, totalPages, totalRequests, startIdx, endIdx int) string {
	items := make([]richmessage.RequestCardItem, 0, endIdx-startIdx)
	for i := startIdx; i < endIdx; i++ {
		req := requests[i]
		items = append(items, richmessage.RequestCardItem{
			Index:  i + 1,
			Title:  trimDisplayTitle(req.Name, 28),
			State:  req.State,
			Type:   req.Type,
			Year:   req.Year,
			Season: req.Season,
			Date:   req.Date,
		})
	}
	card := richmessage.BuildRequestProgressCard(richmessage.RequestCardData{
		Total:      totalRequests,
		Page:       page,
		TotalPages: totalPages,
		Running:    countStates(requests, []string{stateReviewing, stateStuck, services.StatePending, services.StateRecycled, services.StateSearching, services.StateDownloading, "WISH"}),
		Done:       countStates(requests, []string{services.StateCompleted}),
		Problem:    countStates(requests, []string{services.StateFailed, services.StateCancelled}),
		Items:      items,
	})
	return card.Markdown
}

// buildRequestLine builds a single-line request entry
func (h *MyRequestsHandler) buildRequestLine(index int, req services.SubscribeItem) string {
	// Status emoji
	statusEmoji := getStateEmoji(req.State)

	// Type emoji
	typeEmoji := "🎬"
	if req.Type == "电视剧" || req.Type == "tv" {
		typeEmoji = "📺"
	}

	// Build title with year
	title := trimDisplayTitle(req.Name, 32)
	if req.Year != "" && req.Year != "0" {
		title = fmt.Sprintf("%s (%s)", title, req.Year)
	}

	// Add season info for TV shows
	if req.Season > 0 {
		title = fmt.Sprintf("%s S%d", title, req.Season)
	}

	// Extra info (episode count for TV shows)
	extraInfo := ""
	if req.TotalEpisode > 0 {
		extraInfo = fmt.Sprintf("共 %d 集", req.TotalEpisode)
	}

	// C2: 下载进度展示（仅下载中状态，且有集数信息时）
	progressInfo := ""
	if req.State == "D" && req.TotalEpisode > 0 && req.LackEpisode >= 0 {
		downloaded := req.TotalEpisode - req.LackEpisode
		percent := downloaded * 100 / req.TotalEpisode
		barLen := 6
		filled := percent * barLen / 100
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barLen-filled)
		progressInfo = fmt.Sprintf(" %s %d%%", bar, percent)
	}

	// Format: N. [Status] Title [Type] [Extra]
	line := fmt.Sprintf("%d. %s %s %s", index, statusEmoji, title, typeEmoji)
	if progressInfo != "" {
		line += progressInfo
	} else if extraInfo != "" {
		line += fmt.Sprintf(" · %s", extraInfo)
	}
	if req.Date != "" {
		line += fmt.Sprintf(" · %s", trimDisplayDate(req.Date))
	}

	return line
}

// 合成状态：仅用于「我的请求」聚合视图，区分尚未进入 MP 的审核单。
const (
	stateReviewing = "REVIEWING" // ReviewService pending：审核中
	stateStuck     = "STUCK"     // 已审核但提交 MP 失败：同步中/重试
)

// mergePendingReviews 把 ReviewService 中尚未成为 MP 订阅的审核单合并进 MP 列表。
// 去重规则：若审核单已记录 SubscriptionID（即已成功提交 MP），则以 MP 为准、不重复加入；
// 否则按 (TMDBID, Season) 与 MP 列表比对，MP 已有就跳过，没有才用审核单兜底。
func (h *MyRequestsHandler) mergePendingReviews(telegramID int64, mpItems []services.SubscribeItem) []services.SubscribeItem {
	if h.reviewSvc == nil {
		return mpItems
	}

	// MP 已存在的 (tmdbid, season) 集合，用于去重
	existing := make(map[string]bool, len(mpItems))
	for _, it := range mpItems {
		existing[fmt.Sprintf("%d:%d", it.TMDBID, it.Season)] = true
	}

	var extra []services.SubscribeItem
	for _, rv := range h.reviewSvc.GetUserRequests(telegramID) {
		// 只关心还在流程内、且尚未真正落进 MP 的审核单
		if rv.SubscriptionID != 0 {
			continue // 已提交 MP，交给 MP 列表展示（执行层为准）
		}
		key := fmt.Sprintf("%d:%d", rv.TmdbID, rv.Season)
		if existing[key] {
			continue
		}

		var synthState string
		switch {
		case rv.Status == "pending":
			synthState = stateReviewing
		case rv.Status == "approved" && rv.Stuck:
			synthState = stateStuck
		default:
			// approved 但未 stuck 且无 SubscriptionID 的极短暂中间态，按同步中显示
			if rv.Status == "approved" {
				synthState = stateStuck
			} else {
				continue // rejected / 其他已终结状态不在「我的请求」进行中列表里展示
			}
		}

		typeStr := "电影"
		if rv.MediaType == services.MediaTypeTV {
			typeStr = "电视剧"
		}
		extra = append(extra, services.SubscribeItem{
			ID:     0, // 合成项无 MP ID
			Name:   rv.MediaTitle,
			Year:   fmt.Sprintf("%d", rv.MediaYear),
			Type:   typeStr,
			State:  synthState,
			Season: rv.Season,
			TMDBID: rv.TmdbID,
			Date:   rv.CreatedAt.Format("2006-01-02 15:04"),
		})
		existing[key] = true
	}

	// 审核中的放最前面（用户最关心刚提交的有没有进系统）
	return append(extra, mpItems...)
}

// getStateEmoji returns the emoji for a subscription state
func getStateEmoji(state string) string {
	switch state {
	case "WISH":
		return "✨"
	case stateReviewing:
		return "📝"
	case stateStuck:
		return "⚠️"
	case services.StatePending:
		return "⏳"
	case services.StateRecycled:
		return "🔄"
	case services.StateSearching:
		return "🔍"
	case services.StateDownloading:
		return "⬇️"
	case services.StateCompleted:
		return "✅"
	case services.StateFailed:
		return "❌"
	case services.StateCancelled:
		return "🚫"
	default:
		return "❓"
	}
}

func trimDisplayTitle(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if maxLen <= 0 || len([]rune(s)) <= maxLen {
		return s
	}
	r := []rune(s)
	return string(r[:maxLen-1]) + "…"
}

func trimDisplayDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if len(raw) >= 10 {
		return raw[:10]
	}
	return raw
}

func countStates(requests []services.SubscribeItem, states []string) int {
	if len(requests) == 0 || len(states) == 0 {
		return 0
	}
	stateSet := make(map[string]bool, len(states))
	for _, s := range states {
		stateSet[s] = true
	}
	count := 0
	for _, req := range requests {
		if stateSet[req.State] {
			count++
		}
	}
	return count
}

// buildRequestsKeyboard builds the inline keyboard for pagination and actions
func (h *MyRequestsHandler) buildRequestsKeyboard(requests []services.SubscribeItem, page, totalPages, startIdx, endIdx int) *callback.Keyboard {
	var rows [][]callback.Button

	// Row 1-2: Item action buttons (numbers 1-10 on current page)
	// Telegram inline keyboard max 5 buttons per row
	itemButtons := []callback.Button{}
	for i := startIdx; i < endIdx; i++ {
		// 合成项（审核中/同步中，MP 尚无订阅，ID==0）无可操作详情，跳过编号按钮，
		// 避免 handleInfo 按 MP ID 查不到导致「请求不存在」。
		if requests[i].ID == 0 {
			continue
		}
		itemNum := i + 1
		callbackData := callback.BuildCallback("myreqs_item", map[string]string{
			"action": "info",
			"id":     strconv.Itoa(requests[i].ID),
			"page":   strconv.Itoa(page),
			"num":    strconv.Itoa(itemNum),
		})
		itemButtons = append(itemButtons, callback.Button{
			Text:         fmt.Sprintf("[%d]", itemNum),
			CallbackData: callbackData,
		})
	}

	// Split buttons into rows of 5 (Telegram limit)
	const buttonsPerRow = 5
	for i := 0; i < len(itemButtons); i += buttonsPerRow {
		end := i + buttonsPerRow
		if end > len(itemButtons) {
			end = len(itemButtons)
		}
		rows = append(rows, itemButtons[i:end])
	}

	// Pagination controls row
	paginationButtons := []callback.Button{}

	// Previous button
	if page > 1 {
		paginationButtons = append(paginationButtons, callback.Button{
			Text:         "上一页",
			CallbackData: callback.BuildCallback("myreqs_page", map[string]string{"page": strconv.Itoa(page - 1)}),
		})
	}

	// Page indicator
	paginationButtons = append(paginationButtons, callback.Button{
		Text:         fmt.Sprintf("%d/%d", page, totalPages),
		CallbackData: callback.BuildCallback("myreqs_page", map[string]string{"page": strconv.Itoa(page)}),
	})

	// Next button
	if page < totalPages {
		paginationButtons = append(paginationButtons, callback.Button{
			Text:         "下一页",
			CallbackData: callback.BuildCallback("myreqs_page", map[string]string{"page": strconv.Itoa(page + 1)}),
		})
	}

	if len(paginationButtons) > 0 {
		rows = append(rows, paginationButtons)
	}

	// Row 3: Refresh + Cancel pending
	rows = append(rows, []callback.Button{
		{
			Text:         "刷新",
			CallbackData: callback.BuildCallback("myreqs_page", map[string]string{"page": strconv.Itoa(page)}),
		},
		{
			Text:         "↩️ 撤回申请",
			CallbackData: "myreq_cancel",
		},
	})

	// Row 4: Back button
	rows = append(rows, []callback.Button{{
		Text:         "返回主菜单",
		CallbackData: "start",
	}})

	return &callback.Keyboard{
		InlineKeyboard: rows,
	}
}

// handleInfo shows action menu for a specific item
func (h *MyRequestsHandler) handleInfo(ctx *callback.Context, itemID string, page int) (*callback.Response, error) {
	// Get MoviePilot user ID
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	moviepilotID := sess.GetMoviePilotUserID()

	requests, err := h.moviepilot.GetUserRequests(moviepilotID)
	if err != nil {
		return &callback.Response{
			Text:        "❌ 获取请求失败",
			CallbackMsg: "获取失败",
			ShowAlert:   true,
		}, err
	}

	// Find the item
	var item *services.SubscribeItem
	itemNum := 0
	for i, req := range requests {
		if fmt.Sprintf("%d", req.ID) == itemID {
			item = &requests[i]
			itemNum = i + 1
			break
		}
	}

	if item == nil {
		return &callback.Response{
			Text:        "❌ 请求不存在",
			CallbackMsg: "请求不存在",
			ShowAlert:   true,
		}, nil
	}

	// Build info message
	msg := services.NewMessageBuilder()
	msg.Bold(fmt.Sprintf("📋 请求 #%d", itemNum)).Newline()
	msg.Newline()

	statusText := services.GetStateText(item.State)
	msg.Textf("🎬 %s", item.Name)
	if item.Year != "" && item.Year != "0" {
		msg.Textf(" (%s)", item.Year)
	}
	msg.Newline()
	msg.Textf("📊 状态: %s", statusText).Newline()

	actionText := "等待系统处理"
	switch item.State {
	case services.StateCompleted:
		actionText = "可前往 Emby 观看"
	case services.StateFailed:
		actionText = "可点击“重新搜索”重试"
	case services.StateRecycled:
		actionText = "已加入重新搜索队列"
	case services.StateSearching, services.StateDownloading:
		actionText = "请稍后刷新查看最新进度"
	}
	msg.Textf("💡 建议: %s", actionText).Newline()
	if item.Season > 0 {
		msg.Textf("📺 季数: 第 %d 季", item.Season).Newline()
	}
	if item.TotalEpisode > 0 {
		msg.Textf("🎞️ 剧集: 共 %d 集", item.TotalEpisode).Newline()
	}
	if item.Date != "" {
		msg.Textf("📅 添加时间: %s", item.Date).Newline()
	}

	// Build action keyboard
	kb := &callback.Keyboard{
		InlineKeyboard: [][]callback.Button{
			{
				{
					Text:         "🔄 重新搜索",
					CallbackData: callback.BuildCallback("myreqs_item", map[string]string{"action": "reshare", "id": itemID, "page": strconv.Itoa(page)}),
				},
			},
			{
				{
					Text:         "🚫 取消订阅",
					CallbackData: callback.BuildCallback("myreqs_item", map[string]string{"action": "cancel", "id": itemID, "page": strconv.Itoa(page)}),
				},
			},
			{
				{
					Text:         "⬅️ 返回列表",
					CallbackData: callback.BuildCallback("myreqs_page", map[string]string{"page": strconv.Itoa(page)}),
				},
			},
		},
	}

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: kb,
	}, nil
}

// handleReshare triggers a reshare for a subscription
func (h *MyRequestsHandler) handleReshare(ctx *callback.Context, itemID string, page int) (*callback.Response, error) {
	// Attempt to trigger reshare via MoviePilot
	// Note: This requires MoviePilot API support
	err := h.moviepilot.ReshareSubscription(itemID)
	if err != nil {
		logger.Info("[MyRequestsHandler] Reshare failed for item %s: %v", itemID, err)
		return &callback.Response{
			Text:        "❌ 重新搜索失败",
			CallbackMsg: "操作失败",
			ShowAlert:   true,
		}, nil
	}

	return &callback.Response{
		Text:        "✅ 已触发重新搜索",
		CallbackMsg: "已触发",
		ShowAlert:   true,
	}, nil
}

// handleCancel cancels a MoviePilot subscription owned by the current user.
func (h *MyRequestsHandler) handleCancel(ctx *callback.Context, itemID string, page int) (*callback.Response, error) {
	if h.moviepilot == nil {
		return &callback.Response{
			Text:        "❌ MoviePilot 服务未就绪",
			CallbackMsg: "服务未就绪",
			ShowAlert:   true,
		}, nil
	}

	// Ownership check: only allow cancelling subscriptions visible in the user's own list.
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	moviepilotID := sess.GetMoviePilotUserID()
	requests, err := h.moviepilot.GetUserRequests(moviepilotID)
	if err != nil {
		logger.Info("[MyRequestsHandler] Load user requests before cancel failed user=%d item=%s: %v", ctx.UserID, itemID, err)
		return &callback.Response{
			Text:        "❌ 获取请求失败，请稍后再试",
			CallbackMsg: "获取失败",
			ShowAlert:   true,
		}, nil
	}

	owned := false
	for _, req := range requests {
		if fmt.Sprintf("%d", req.ID) == itemID {
			owned = true
			break
		}
	}
	if !owned {
		return &callback.Response{
			Text:        "❌ 请求不存在或不属于你",
			CallbackMsg: "无权限",
			ShowAlert:   true,
		}, nil
	}

	if err := h.moviepilot.CancelSubscription(itemID); err != nil {
		logger.Info("[MyRequestsHandler] Cancel failed for item %s: %v", itemID, err)
		return &callback.Response{
			Text:        "❌ 取消订阅失败，请稍后再试",
			CallbackMsg: "取消失败",
			ShowAlert:   true,
		}, nil
	}

	return &callback.Response{
		Text:        "✅ 已取消订阅\n\n刷新列表后可查看最新状态。",
		CallbackMsg: "已取消",
		ShowAlert:   true,
	}, nil
}

// HelpHandler handles help callbacks
type HelpHandler struct{}

func NewHelpHandler() *HelpHandler {
	return &HelpHandler{}
}

func (h *HelpHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	msg := services.NewMessageBuilder()
	msg.Bold("❓ 帮助").Newline()
	msg.Newline()
	msg.Text("遇到问题了？").Newline()
	msg.Newline()
	msg.Italic("👇 选一个问题看看")

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🔍 怎么搜片", "help_topic:topic:search")
	kb.AddButton("🔗 怎么绑定", "help_topic:topic:link")
	kb.NewRow()
	kb.AddButton("❌ 请求失败", "help_topic:topic:failed")
	kb.AddButton("🔔 没收到通知", "help_topic:topic:notify")
	kb.NewRow()
	kb.AddButton("📮 其他问题", "help_topic:topic:other")
	kb.NewRow()
	kb.AddButton("⬅️ 返回", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}
