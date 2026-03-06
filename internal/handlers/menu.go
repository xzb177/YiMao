package handlers

import (
	"fmt"
	"log"
	"strconv"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
	"emby-telegram-bot/pkg/errors"
)

// MyRequestsHandler handles "my requests" callbacks with pagination
type MyRequestsHandler struct {
	sessMgr     *session.Manager
	telegram    *services.TelegramClient
	moviepilot  *services.MoviePilotClient
	userMapping *services.UserMappingService
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
func (h *MyRequestsHandler) SetUserMapping(um *services.UserMappingService) {
	h.userMapping = um
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
			log.Printf("[MyRequestsHandler] Loaded moviepilot_id=%d from userMapping for user %d", id, ctx.UserID)
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

	// Fetch user requests
	requests, err := h.moviepilot.GetUserRequests(moviepilotID)
	if err != nil {
		return nil, errors.MoviePilotErr("failed to get requests", err)
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
	msg, keyboard := h.buildRequestsMessage(requests, page, totalPages, totalRequests)

	return &callback.Response{
		Text:     msg,
		Edit:     true,
		Keyboard: keyboard,
	}, nil
}

// buildRequestsMessage builds the paginated message with inline keyboard
func (h *MyRequestsHandler) buildRequestsMessage(requests []services.SubscribeItem, page, totalPages, totalRequests int) (string, *callback.Keyboard) {
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
		return msg.Build(), kb
	}

	// Header
	msg.Bold("我的请求").Newline()
	msg.Text(fmt.Sprintf("共 %d 条，第 %d/%d 页", totalRequests, page, totalPages)).Newline()
	msg.Text("—").Newline()
	msg.Newline()

	// Calculate slice bounds
	startIdx := (page - 1) * requestsPerPage
	endIdx := startIdx + requestsPerPage
	if endIdx > totalRequests {
		endIdx = totalRequests
	}

	// Build one-line format items
	for i := startIdx; i < endIdx; i++ {
		req := requests[i]
		line := h.buildRequestLine(i+1, req)
		msg.Text(line).Newline()
	}

	// Build keyboard
	kb := h.buildRequestsKeyboard(requests, page, totalPages, startIdx, endIdx)

	return msg.Build(), kb
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
	title := req.Name
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

	// Format: N. [Status] Title [Type] [Extra]
	line := fmt.Sprintf("%d. %s %s %s", index, statusEmoji, title, typeEmoji)
	if extraInfo != "" {
		line += fmt.Sprintf(" · %s", extraInfo)
	}

	return line
}

// getStateEmoji returns the emoji for a subscription state
func getStateEmoji(state string) string {
	switch state {
	case services.StatePending, services.StateRecycled:
		return "⏳"
	case services.StateSearching:
		return "🔍"
	case services.StateDownloading:
		return "⬇️"
	case services.StateCompleted:
		return "✅"
	case services.StateFailed:
		return "❌"
	default:
		return "❓"
	}
}

// buildRequestsKeyboard builds the inline keyboard for pagination and actions
func (h *MyRequestsHandler) buildRequestsKeyboard(requests []services.SubscribeItem, page, totalPages, startIdx, endIdx int) *callback.Keyboard {
	var rows [][]callback.Button

	// Row 1-2: Item action buttons (numbers 1-10 on current page)
	// Telegram inline keyboard max 5 buttons per row
	itemButtons := []callback.Button{}
	for i := startIdx; i < endIdx; i++ {
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

	// Row 3: Refresh button
	rows = append(rows, []callback.Button{{
		Text:         "刷新",
		CallbackData: callback.BuildCallback("myreqs_page", map[string]string{"page": strconv.Itoa(page)}),
	}})

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
		log.Printf("[MyRequestsHandler] Reshare failed for item %s: %v", itemID, err)
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

// handleCancel handles cancel subscription (placeholder)
func (h *MyRequestsHandler) handleCancel(ctx *callback.Context, itemID string, page int) (*callback.Response, error) {
	return &callback.Response{
		Text:        "💬 取消功能请联系管理员",
		CallbackMsg: "需要管理员处理",
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
	msg.Bold("帮助").Newline()
	msg.Newline()
	msg.Text("搜索：输入片名即可查询").Newline()
	msg.Text("推荐：浏览热门和高分内容").Newline()
	msg.Text("请求：提交后可查看处理进度").Newline()
	msg.Text("绑定：/link 用户名 密码").Newline()
	msg.Newline()
	msg.Bold("常用命令").Newline()
	msg.Text("/start  主菜单").Newline()
	msg.Text("/search 搜索").Newline()
	msg.Text("/ai     推荐").Newline()
	msg.Text("/requests 我的请求").Newline()
	msg.Newline()
	msg.Italic("如有问题请联系管理员").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("返回", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}
