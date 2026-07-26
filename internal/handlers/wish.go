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
	wish          *services.WishService
	tmdb          *services.TMDBClient
	moviepilot    *services.MoviePilotClient
	telegram      *services.TelegramClient
	sessMgr       *session.Manager
	submission    wishRequestSubmitter
	carpool       *services.CarpoolService
	searchHistory *services.SearchHistoryService
}

type wishRequestSubmitter interface {
	SubmitResult(services.RequestSubmission) (services.SubmissionResult, error)
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
	}
}

// SetRequestSubmissionService 注入 typed 求片提交服务。
func (h *WishHandler) SetRequestSubmissionService(s wishRequestSubmitter) { h.submission = s }

// SetCarpoolService 注入拼车服务。
func (h *WishHandler) SetCarpoolService(c *services.CarpoolService) { h.carpool = c }

// SetSearchHistoryService 注入搜索历史服务：session 丢词时兜底取最近搜索词。
func (h *WishHandler) SetSearchHistoryService(s *services.SearchHistoryService) { h.searchHistory = s }

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

	// 走统一入池流程；命令入口不需要尝试打通私信（用户已在私聊里发命令，通道天然存在）。
	h.addWishByQuery(chatID, userID, query, false)
}

// HandleAddFromSearch 处理 #1「🌟 加入许愿池」回调：从 session 取暂存的搜索词入池。
// 回调串不带超长片名（守 TG 64 字节上限），片名由搜索无结果分支预先存进 session。
func (h *WishHandler) HandleAddFromSearch(ctx *callback.Context) (*callback.Response, error) {
	if h == nil || h.wish == nil {
		return &callback.Response{CallbackMsg: "许愿池服务未就绪", ShowAlert: true}, nil
	}
	if h.sessMgr == nil {
		return &callback.Response{CallbackMsg: "许愿池服务未就绪，请稍后再试", ShowAlert: true}, nil
	}

	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	query, _ := sess.GetString(pendingWishQueryKey)
	query = strings.TrimSpace(query)
	if query == "" && h.searchHistory != nil {
		// session 过期丢词时兜底：搜索历史持久化在磁盘上，最近一条
		// 就是触发「无结果 → 许愿」按钮的那次搜索，别让用户重搜一遍。
		if entries := h.searchHistory.GetHistory(ctx.UserID); len(entries) > 0 {
			query = strings.TrimSpace(entries[0].Query)
		}
	}
	if query == "" {
		return &callback.Response{
			CallbackMsg: "没拿到要许愿的片名了，麻烦重新搜一次再点～",
			ShowAlert:   true,
		}, nil
	}
	// 用完即清，避免下次误用旧片名。
	sess.Delete(pendingWishQueryKey)

	// #1 通路缺口修复：点「加入许愿池」时尝试打通私聊通道（失败不阻断入池，仅记日志）。
	h.tryOpenDMChannel(ctx.UserID)

	// 走统一入池流程，回复发到当前会话（群内点按钮则发群里、私聊点则发私聊）。
	// fromSearch=true → 入池成功文案追加「记得和我私聊过哦」提示。
	h.addWishByQuery(ctx.ChatID, ctx.UserID, query, true)

	return &callback.Response{CallbackMsg: "已为你处理许愿～", ShowAlert: false}, nil
}

// pendingWishQueryKey 是 session 里暂存「待许愿搜索词」的 key（#1 搜索无结果按钮用）。
const pendingWishQueryKey = "pending_wish_query"

// addWishByQuery 是 /wish 命令与搜索无结果按钮共用的入池核心逻辑。
//   - chatID:    回复目标会话。
//   - userID:    许愿发起人。
//   - query:     待许愿的片名/关键词。
//   - fromSearch: 是否来自搜索无结果按钮（true 时入池成功文案追加私聊提示）。
func (h *WishHandler) addWishByQuery(chatID int64, userID int64, query string, fromSearch bool) {
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
	confidentPick := isConfidentTitleMatch(query, picked.GetTitle())

	mediaType := picked.MediaType
	tmdbID := picked.ID

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
		// 众筹：重复许愿同一片时，该 user 已被 AddWish 记进 wish_wishers（累计人数），
		// 这里查总人数给「已经在等这部啦，目前 N 人在等」的友好提示。
		ck := services.CanonicalKey(tmdbID, imdbID, mediaType, season)
		n := h.wish.CountWishers(ck)
		var msg string
		if n > 1 {
			msg = fmt.Sprintf("📌 《%s》已经在等啦，目前 %d 人在等，出源会自动通知你～", title, n)
		} else {
			msg = fmt.Sprintf("📌 《%s》已在许愿池里啦，出源会自动通知你～", title)
		}
		h.telegram.SendMessage(chatID, msg, "", nil)
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
		msg := fmt.Sprintf("✨ 已加入许愿池\n🎯 匹配到：《%s》%s · %s\n\n找到源后会第一时间私信通知你（约每天重搜一次）。",
			title, yearStr, typeStr)
		if viableCount > 1 && !confidentPick {
			msg += "\n\n⚠️ 这个片名有多个匹配结果，已按最接近的加入。若不是这部，请用更精确的片名（可带年份）重新 /wish。"
		}
		// #1：来自搜索无结果按钮时，额外提示「想被通知出源记得和我私聊过哦」。
		if fromSearch {
			msg += "\n\n💬 想被通知出源？记得和我私聊过哦（点一下我头像发条消息就行）。"
		}
		h.telegram.SendMessage(chatID, msg, "", nil)
	default:
		h.telegram.SendMessage(chatID, "✨ 已加入许愿池", "", nil)
	}
}

// tryOpenDMChannel 尽力打通用户与 bot 的私聊通道（#1 通路缺口修复）。
// 直接给用户发一条「打招呼」私信：
//   - 若此前从未私聊过 / 被封禁 → SendMessage 会失败（403 等），此时仅记日志、不阻断入池。
//   - 成功 → 通道已通，后续出源私信可达。
//
// 注意：该方法只为「探活 + 打招呼」，绝不能因失败影响主流程。
func (h *WishHandler) tryOpenDMChannel(userID int64) {
	if h.telegram == nil || userID == 0 {
		return
	}
	greeting := "👋 你好，我是云海求片助手。\n你的许愿已经记录；找到片源后，我会在这里通知你。"
	if _, err := h.telegram.SendMessage(userID, greeting, "", nil); err != nil {
		// 发不出说明用户还没和 bot 建立私聊（或封禁了），不阻断入池，仅记日志。
		logger.Info("[wish] 打招呼私信发送失败（用户可能未私聊过 bot，不影响入池）user=%d: %v", userID, err)
		return
	}
	logger.Info("[wish] 已向 user=%d 发送打招呼私信，私聊通道已打通", userID)
}

// listMyWishes 列出某用户的活跃许愿。
func (h *WishHandler) listMyWishes(chatID int64, userID int64) {
	items, err := h.wish.ListByUser(userID)
	if err != nil {
		logger.Warn("[wish] 查询用户许愿失败 user=%d: %v", userID, err)
		h.telegram.SendMessage(chatID, "❌ 查询失败，请稍后再试", "", nil)
		return
	}
	if len(items) == 0 {
		h.telegram.SendMessage(chatID, "🌟 你的许愿池还是空的\n\n用 /wish 片名 加入无源想看的片，出源自动通知你～", "", nil)
		return
	}

	// 分页显示，每页 10 条
	const pageSize = 10
	totalPages := (len(items) + pageSize - 1) / pageSize
	h.showWishPage(chatID, userID, items, 0, totalPages, pageSize)
}

// showWishPage 显示许愿池的某一页
func (h *WishHandler) showWishPage(chatID int64, userID int64, items []*services.WishItem, page int, totalPages int, pageSize int) {
	start := page * pageSize
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("🌟 我的许愿池（%d 部）\n", len(items)))
	if totalPages > 1 {
		b.WriteString(fmt.Sprintf("📄 第 %d/%d 页\n", page+1, totalPages))
	}
	b.WriteString("\n")

	for _, it := range items[start:end] {
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

	if totalPages > 1 {
		b.WriteString(fmt.Sprintf("\n💡 用 /wish %d 跳转页码", page+2))
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

// HandleEntry 许愿池入口（主菜单 start_wish 按钮，剥前缀后为 wish）。
func (h *WishHandler) HandleEntry(ctx *callback.Context) (*callback.Response, error) {
	if h.wish == nil {
		return &callback.Response{Text: "✨ 许愿池暂未开放", Edit: true}, nil
	}

	items, err := h.wish.ListByUser(ctx.UserID)
	if err != nil {
		return &callback.Response{Text: "❌ 查询失败，请稍后再试", Edit: true}, nil
	}

	var b strings.Builder
	if len(items) == 0 {
		b.WriteString("✨ 许愿池\n\n")
		b.WriteString("这里是你的专属许愿池～\n\n")
		b.WriteString("💡 搜不到的片？用 /wish 片名 许个愿\n")
		b.WriteString("找到源第一时间私信通知你\n")
	} else {
		b.WriteString(fmt.Sprintf("✨ 我的许愿（%d 部）\n\n", len(items)))
		kb := services.NewKeyboardBuilder()
		for _, it := range items {
			icon := "🎬"
			if it.MediaType == "tv" {
				icon = "📺"
			}
			title := it.Title
			if it.Year > 0 {
				title += fmt.Sprintf(" (%d)", it.Year)
			}
			stateText := ""
			if s := wishStateText(it.State); s != "" {
				stateText = " " + s
			}
			b.WriteString(fmt.Sprintf("%s %s%s\n", icon, title, stateText))
			kb.AddButton(fmt.Sprintf("撤回·%s", truncateTitle(it.Title, 8)),
				fmt.Sprintf("wish_cancel:id:%d", it.ID))
			kb.NewRow()
		}
		b.WriteString("\n💡 出源后会私信你「🎬 立即求片」按钮")
		kb.AddButton("🏠 主菜单", "start")
		return &callback.Response{
			Text:     b.String(),
			Edit:     true,
			Keyboard: convertKeyboard(kb.Build()),
		}, nil
	}

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🏠 主菜单", "start")

	return &callback.Response{
		Text:     b.String(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// HandleCancel 处理许愿撤回（wish_cancel 回调）。
func (h *WishHandler) HandleCancel(ctx *callback.Context) (*callback.Response, error) {
	if h.wish == nil {
		return &callback.Response{CallbackMsg: "服务未就绪", ShowAlert: true}, nil
	}

	idStr := ctx.Callback.Params["id"]
	var wishID int64
	fmt.Sscanf(idStr, "%d", &wishID)

	if wishID == 0 {
		return &callback.Response{CallbackMsg: "无效的许愿ID", ShowAlert: true}, nil
	}

	// 验证归属
	item, err := h.wish.GetByID(wishID)
	if err != nil || item == nil {
		return &callback.Response{CallbackMsg: "许愿记录不存在", ShowAlert: true}, nil
	}
	if item.UserID != ctx.UserID {
		return &callback.Response{CallbackMsg: "这不是你的许愿哦", ShowAlert: true}, nil
	}

	if err := h.wish.MarkExpired(wishID); err != nil {
		return &callback.Response{CallbackMsg: "取消失败，请稍后再试", ShowAlert: true}, nil
	}

	// 重新渲染列表
	return h.HandleEntry(ctx)
}

// Handle 处理 wish_request 回调（出源喜报「立即求片」按钮）。
func (h *WishHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	if h.wish == nil || h.submission == nil {
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
	if item.UserID != ctx.UserID {
		return &callback.Response{CallbackMsg: "这不是你的许愿哦", ShowAlert: true}, nil
	}
	if item.State != services.WishStateNotified && item.State != services.WishStateFound {
		return &callback.Response{CallbackMsg: "该许愿已处理过啦", ShowAlert: true}, nil
	}

	result, err := h.submission.SubmitResult(services.RequestSubmission{
		TelegramID: item.UserID,
		TmdbID:     item.TmdbID,
		MediaTitle: item.Title,
		MediaYear:  item.Year,
		MediaType:  services.MediaType(item.MediaType),
		Season:     item.Season,
		Origin:     "wish",
		UseQuota:   true,
	})
	if err != nil {
		logger.Info("[wish] id=%d 提交求片失败: %v", wishID, err)
		return &callback.Response{CallbackMsg: "求片失败，请稍后再试", ShowAlert: true}, err
	}

	markFulfilled := func() {
		if _, ferr := h.wish.MarkFulfilled(wishID); ferr != nil {
			logger.Info("[wish] id=%d MarkFulfilled 失败: %v", wishID, ferr)
		}
	}
	switch result.Status {
	case services.SubmissionCreated:
		markFulfilled()
		return &callback.Response{CallbackMsg: "请求已提交"}, nil
	case services.SubmissionDuplicateOwn:
		return &callback.Response{CallbackMsg: "你已有相同求片请求，请等待处理", ShowAlert: true}, nil
	case services.SubmissionDuplicateOther:
		if h.carpool == nil {
			return &callback.Response{CallbackMsg: "已有其他用户提交相同请求，请稍后再试", ShowAlert: true}, nil
		}
		people := h.carpool.Add(item.TmdbID, item.MediaType, item.UserID)
		markFulfilled()
		return &callback.Response{CallbackMsg: fmt.Sprintf("已加入拼车，共 %d 人想看", people)}, nil
	case services.SubmissionNotBound:
		return &callback.Response{
			Text:        "🔗 需要先绑定账号\n\n绑定后就能求片了，命令：/link 用户名 密码\n没有账号也没关系，首次绑定会自动创建。",
			CallbackMsg: "请先绑定账号后再求片",
			Keyboard: &callback.Keyboard{InlineKeyboard: [][]callback.Button{
				{{Text: "🔗 立即绑定", CallbackData: "link"}},
			}},
		}, nil
	case services.SubmissionQuotaExceeded:
		return &callback.Response{CallbackMsg: "求片额度不足，请稍后再试", ShowAlert: true}, nil
	default:
		return &callback.Response{CallbackMsg: "求片失败，请稍后再试", ShowAlert: true}, nil
	}
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
