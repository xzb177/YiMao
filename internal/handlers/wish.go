package handlers

import (
	"fmt"
	"strings"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/logger"
)

// 本文件实现 Batch B #6「求片许愿池」的用户侧入口与回调：
//   - /wish <片名>：入池（坑1 TMDB 强校验 + 坑7 现有订阅去重 + 容量上限）。
//   - /wish（无参）：列出「我的许愿」。
//   - wish_request 回调：出源喜报「立即求片」按钮 → 走现有 request 流程 + 用户确认 → FULFILLED。
//
// 严格照 docs：出源后仅通知不自动求；点按钮才走 request（非自动入库）。

// WishHandler 处理许愿池命令与回调。
type WishHandler struct {
	wish       *services.WishService
	tmdb       *services.TMDBClient
	moviepilot *services.MoviePilotClient
	telegram   *services.TelegramClient
	sessMgr    *session.Manager
	reqHandler *RequestHandler // 复用现有求片流程（FULFILLED 时走它）
}

// NewWishHandler 创建许愿池处理器。
func NewWishHandler(
	wish *services.WishService,
	tmdb *services.TMDBClient,
	moviepilot *services.MoviePilotClient,
	telegram *services.TelegramClient,
	sessMgr *session.Manager,
	reqHandler *RequestHandler,
) *WishHandler {
	return &WishHandler{
		wish:       wish,
		tmdb:       tmdb,
		moviepilot: moviepilot,
		telegram:   telegram,
		sessMgr:    sessMgr,
		reqHandler: reqHandler,
	}
}

// BuildWishRequestButton 构造出源喜报「立即求片」按钮的回调串（注入给 WishScheduler）。
// 格式沿用项目 colon 编码：wish_request:id:<wishItemID>
func BuildWishRequestButton(item *services.WishItem) (string, string) {
	return "🎬 立即求片", fmt.Sprintf("wish_request:id:%d", item.ID)
}

// HandleCommand 处理 /wish 命令（由 bot.HandleCommand 调用）。
func (h *WishHandler) HandleCommand(chatID int64, userID int64, text string) {
	if h == nil || h.wish == nil {
		h.telegram.SendMessage(chatID, "❌ 许愿池服务未就绪", "", nil)
		return
	}

	// 解析片名（去掉 /wish 前缀）。
	parts := strings.Fields(text)
	var query string
	if len(parts) > 1 {
		query = strings.TrimSpace(strings.TrimPrefix(text, parts[0]))
	}

	// 无参数：列出「我的许愿」。
	if query == "" {
		h.listMyWishes(chatID, userID)
		return
	}

	// 坑1：入池前 TMDB 强校验（tmdb_id != 0 才入）。
	if h.tmdb == nil {
		h.telegram.SendMessage(chatID, "❌ TMDB 服务未配置，暂时无法许愿", "", nil)
		return
	}
	result, err := h.tmdb.SearchMedia(query, 1)
	if err != nil || result == nil || len(result.Results) == 0 {
		// 没找到 → 拒绝入池（不占容量不重搜）。
		h.telegram.SendMessage(chatID, "🔍 没找到这个片，换个关键词再试试～", "", nil)
		return
	}

	// 取首个有效结果（带 id 的 movie/tv），同时统计可选候选数 + 判断首条置信度。
	// B6：多个 TMDB 结果且首条不够置信时，不静默取首条——在入池确认消息里明确「匹配到的标题/年份」，
	// 并提示用户若选错可用更精确片名重搜，避免默默许愿到错误条目。
	var picked *services.TMDBMediaInfo
	viableCount := 0
	for i := range result.Results {
		r := &result.Results[i]
		mt := r.MediaType
		if mt != "movie" && mt != "tv" {
			continue
		}
		if r.ID == 0 {
			continue
		}
		viableCount++
		if picked == nil {
			picked = r
		}
	}
	if picked == nil {
		h.telegram.SendMessage(chatID, "🔍 没找到这个片，换个关键词再试试～", "", nil)
		return
	}
	// 首条是否「置信」：查询词与候选标题忽略大小写/空白后相等或互相包含视为高置信。
	confidentPick := isConfidentTitleMatch(query, picked.GetTitle())

	mediaType := picked.MediaType
	tmdbID := picked.ID

	// 取 imdb_id（canonical key 兜底用）。详情接口带 external_ids。
	imdbID := ""
	if detail, derr := h.tmdb.GetMediaByType(tmdbID, mediaType); derr == nil && detail != nil {
		imdbID = strings.TrimSpace(detail.ExternalIDs.IMDBID)
	}

	title := picked.GetTitle()
	year := picked.GetYear()
	season := 0 // v1：剧集默认整剧（season=0）。

	// 坑7：入池前查现有订阅/求片，命中则提示「已有人求过 / 出源自动通知」，不入池。
	if h.moviepilot != nil && tmdbID != 0 {
		mt := services.MediaTypeMovie
		if mediaType == "tv" {
			mt = services.MediaTypeTV
		}
		if _, found, ferr := h.moviepilot.FindExistingSubscription(tmdbID, mt, season); ferr == nil && found {
			h.telegram.SendMessage(chatID,
				fmt.Sprintf("📌 《%s》已在求片/订阅列表里，出源会自动通知，无需重复许愿～", title), "", nil)
			return
		}
	}

	// 入池。
	addRes, err := h.wish.AddWish(&services.WishItem{
		UserID:    userID,
		TmdbID:    tmdbID,
		ImdbID:    imdbID,
		MediaType: mediaType,
		Title:     title,
		Year:      year,
		Season:    season,
	})
	if err != nil {
		logger.Info("[wish] 入池失败 user=%d query=%q: %v", userID, query, err)
		h.telegram.SendMessage(chatID, "❌ 许愿失败，请稍后再试", "", nil)
		return
	}

	switch {
	case addRes.Duplicate:
		// 坑7：池内已存在同 canonical key。
		h.telegram.SendMessage(chatID,
			fmt.Sprintf("📌 《%s》已在许愿池里啦，出源会自动通知你～", title), "", nil)
	case addRes.OverPerUser:
		h.telegram.SendMessage(chatID,
			fmt.Sprintf("📦 你的许愿池已满（上限 %d 条），先等几部出源或移除一些吧～", services.WishMaxPerUser), "", nil)
	case addRes.OverGlobal:
		h.telegram.SendMessage(chatID, "📦 许愿池已满，暂时无法加入，请稍后再试～", "", nil)
	case addRes.Created:
		yearStr := ""
		if year > 0 {
			yearStr = fmt.Sprintf(" (%d)", year)
		}
		typeStr := "电影"
		if mediaType == "tv" {
			typeStr = "剧集"
		}
		// B6：确认消息明确展示「匹配到」的标题/年份/类型，让用户能看出是否选错。
		msg := fmt.Sprintf("✨ 已加入许愿池\n🎯 匹配到：《%s》%s · %s\n\n找到源后会第一时间私信通知你（约每天重搜一次）。",
			title, yearStr, typeStr)
		// 多个候选且首条置信度不高：提示用户若选错可用更精确片名（带年份）重搜。
		if viableCount > 1 && !confidentPick {
			msg += "\n\n⚠️ 这个片名有多个匹配结果，已按最接近的加入。若不是这部，请用更精确的片名（可带年份）重新 /wish。"
		}
		h.telegram.SendMessage(chatID, msg, "", nil)
	default:
		h.telegram.SendMessage(chatID, "✨ 已加入许愿池", "", nil)
	}
}

// listMyWishes 列出某用户的活跃许愿。
func (h *WishHandler) listMyWishes(chatID int64, userID int64) {
	items, err := h.wish.ListByUser(userID)
	if err != nil {
		logger.Info("[wish] 查询用户许愿失败 user=%d: %v", userID, err)
		h.telegram.SendMessage(chatID, "❌ 查询失败，请稍后再试", "", nil)
		return
	}
	if len(items) == 0 {
		h.telegram.SendMessage(chatID, "🌟 你的许愿池还是空的\n\n用 /wish 片名 加入无源想看的片，出源自动通知你～", "", nil)
		return
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🌟 我的许愿池（%d 部）\n\n", len(items)))
	for _, it := range items {
		icon := "🎬"
		if it.MediaType == "tv" {
			icon = "📺"
		}
		b.WriteString(fmt.Sprintf("%s %s", icon, it.Title))
		if it.Year > 0 {
			b.WriteString(fmt.Sprintf(" (%d)", it.Year))
		}
		b.WriteString("  " + wishStateText(it.State) + "\n")
	}
	b.WriteString("\n出源后会私信你「🎬 立即求片」按钮。")
	h.telegram.SendMessage(chatID, b.String(), "", nil)
}

// wishStateText 给用户看的状态文案。
func wishStateText(state string) string {
	switch state {
	case services.WishStatePending, services.WishStateSearching:
		return "🔎 找源中"
	case services.WishStateFound:
		return "🎯 已发现源"
	case services.WishStateNotified:
		return "📨 已通知你"
	default:
		return ""
	}
}

// Handle 处理 wish_request 回调（出源喜报「立即求片」按钮）。
func (h *WishHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	if h.wish == nil || h.reqHandler == nil {
		return &callback.Response{CallbackMsg: "服务未就绪", ShowAlert: true}, nil
	}

	idStr, ok := ctx.Callback.Params["id"]
	if !ok {
		return &callback.Response{CallbackMsg: "参数无效", ShowAlert: true}, nil
	}
	var wishID int64
	fmt.Sscanf(idStr, "%d", &wishID)
	if wishID == 0 {
		return &callback.Response{CallbackMsg: "无效的许愿ID", ShowAlert: true}, nil
	}

	item, err := h.wish.GetByID(wishID)
	if err != nil || item == nil {
		return &callback.Response{CallbackMsg: "许愿记录不存在", ShowAlert: true}, nil
	}
	// 仅发起人可点。
	if item.UserID != ctx.UserID {
		return &callback.Response{CallbackMsg: "这不是你的许愿哦", ShowAlert: true}, nil
	}
	// 状态须为 NOTIFIED/FOUND（已通知待求片）；其它状态视为已处理。
	if item.State != services.WishStateNotified && item.State != services.WishStateFound {
		return &callback.Response{CallbackMsg: "该许愿已处理过啦", ShowAlert: true}, nil
	}

	// 复用现有 request 流程：确保会话存在（RequestHandler 依赖 session，TMDB 兜底取标题）。
	if h.sessMgr != nil {
		h.sessMgr.GetOrCreate(ctx.UserID)
	}

	// 构造一个 request 动作的 Context，交给现有 RequestHandler（含用户绑定校验、配额、Emby 检查、管理员审核）。
	reqCtx := &callback.Context{
		UserID:     ctx.UserID,
		ChatID:     ctx.ChatID,
		ChatType:   ctx.ChatType,
		MessageID:  ctx.MessageID,
		CallbackID: ctx.CallbackID,
		Callback: &callback.Callback{
			Action: callback.ActionRequest,
			Params: map[string]string{
				"id":   fmt.Sprintf("%d", item.TmdbID),
				"type": item.MediaType,
			},
		},
	}
	if item.MediaType == "tv" && item.Season > 0 {
		reqCtx.Callback.Params["season"] = fmt.Sprintf("%d", item.Season)
	}

	resp, err := h.reqHandler.Handle(reqCtx)
	if err != nil {
		logger.Info("[wish] id=%d 走 request 流程失败: %v", wishID, err)
		return &callback.Response{CallbackMsg: "求片失败，请稍后再试", ShowAlert: true}, err
	}

	// request 已成功提交（进入审核）→ NOTIFIED→FULFILLED。
	// 注意：只有 request 真正提交成功（resp 不是错误提示）才置 FULFILLED。
	// RequestHandler 在配额/绑定缺失等情况下返回的是带提示的 Response（无 err），
	// 这些情况下不应置 FULFILLED，让用户绑定/等配额后可再点。
	if isWishRequestSubmitted(resp) {
		if _, ferr := h.wish.MarkFulfilled(wishID); ferr != nil {
			logger.Info("[wish] id=%d MarkFulfilled 失败: %v", wishID, ferr)
		}
	}

	return resp, nil
}

// isConfidentTitleMatch 判断查询词与候选标题是否高置信匹配（B6）。
// 规则：忽略大小写与首尾空白后，两者相等或互相包含即视为高置信。
// 用于决定多结果时是否提示用户「可能选错、请精确重搜」。
func isConfidentTitleMatch(query, title string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	t := strings.ToLower(strings.TrimSpace(title))
	if q == "" || t == "" {
		return false
	}
	if q == t {
		return true
	}
	// 互相包含（如查询「沙丘」匹配标题「沙丘」或「沙丘 2」）。
	return strings.Contains(t, q) || strings.Contains(q, t)
}

// isWishRequestSubmitted 判断 RequestHandler 返回是否为「已成功提交求片」。
// 提交成功的两种返回：审核提交成功 / 媒体已存在等确认场景仍算进入流程。
// 这里以「CallbackMsg == 请求已提交」为成功信号（与 RequestHandler 文案一致）。
func isWishRequestSubmitted(resp *callback.Response) bool {
	if resp == nil {
		return false
	}
	return strings.Contains(resp.CallbackMsg, "请求已提交")
}
