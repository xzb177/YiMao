package handlers

import (
	"fmt"
	"strings"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/richmessage"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/logger"
)

// GameHandler 游戏化功能回调处理器
type GameHandler struct {
	personalitySvc *services.PersonalityService
	narratorSvc    *services.NarratorService
	blindBoxSvc    *services.BlindBoxService
	socialDB       *services.SocialDB
	rouletteSvc    *services.RouletteService
	userMapping    services.UserMappingStore
	telegram       *services.TelegramClient
	sessionMgr     *session.Manager
	emotionSvc     *services.EmotionTimelineService
	viewingSvc     *services.ViewingHistoryService
	groupChatID    int64 // 群聊ID，用于发送群通知
}

// Close 清理资源
func (h *GameHandler) Close() {
	if h.socialDB != nil {
		if err := h.socialDB.Close(); err != nil {
			logger.Info("[Game] 关闭 SocialDB 出错: %v", err)
		}
	}
}

// NewGameHandler 创建游戏处理器
func NewGameHandler(
	personalitySvc *services.PersonalityService,
	narratorSvc *services.NarratorService,
	blindBoxSvc *services.BlindBoxService,
	socialDB *services.SocialDB,
	rouletteSvc *services.RouletteService,
	userMapping services.UserMappingStore,
	telegram *services.TelegramClient,
	sessionMgr *session.Manager,
	emotionSvc *services.EmotionTimelineService,
	groupChatID int64,
) *GameHandler {
	return &GameHandler{
		personalitySvc: personalitySvc,
		narratorSvc:    narratorSvc,
		blindBoxSvc:    blindBoxSvc,
		socialDB:       socialDB,
		rouletteSvc:    rouletteSvc,
		userMapping:    userMapping,
		telegram:       telegram,
		sessionMgr:     sessionMgr,
		emotionSvc:     emotionSvc,
		groupChatID:    groupChatID,
	}
}

// SetViewingHistoryService 注入观影历史服务
func (h *GameHandler) SetViewingHistoryService(svc *services.ViewingHistoryService) {
	h.viewingSvc = svc
}

// Handle 游戏中心路由
func (h *GameHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	action := string(ctx.Callback.Action)

	switch action {
	case "game_menu":
		return h.handleMenu(ctx)
	case "game_narrator":
		return h.handleNarratorEntry(ctx)
	case "game_narrate":
		return h.handleNarrate(ctx)
	case "game_blindbox":
		return h.handleBlindBox(ctx)
	case "game_blindbox_open":
		return h.handleBlindBoxOpen(ctx)
	case "game_blindbox_horror":
		return h.handleBlindBoxHorror(ctx)
	case "game_blindbox_personality":
		return h.handleEmotionProfile(ctx)
	case "game_roulette", "game_roulette_spin":
		return h.handleRoulette(ctx)
	case "game_compare", "game_emotion", "game_personality":
		return &callback.Response{CallbackMsg: "🧠 该功能已合并到主菜单「观影画像」", ShowAlert: true}, nil
	case "game_achievements":
		return &callback.Response{CallbackMsg: "📈 静态成就已下线", ShowAlert: true}, nil
	// 以下功能已废弃，返回友好提示
	case "game_social",
		"game_prescription", "game_time_machine",
		"game_review":
		return &callback.Response{CallbackMsg: "🚧 该功能已升级，请从游戏中心进入", ShowAlert: true}, nil
	case "game_contract":
		return &callback.Response{CallbackMsg: "契约玩法已下线", ShowAlert: true}, nil
	default:
		return nil, fmt.Errorf("unknown game action: %s", action)
	}
}

func (h *GameHandler) handleRoulette(ctx *callback.Context) (*callback.Response, error) {
	if h.rouletteSvc == nil {
		return &callback.Response{CallbackMsg: "轮盘服务未就绪", ShowAlert: true}, nil
	}
	result, err := h.rouletteSvc.Spin(ctx.UserID, "")
	if err != nil {
		return &callback.Response{Text: "❌ " + err.Error(), CallbackMsg: "轮盘暂不可用", ShowAlert: true}, nil
	}
	card := richmessage.BuildRouletteCard(richmessage.RouletteCardData{
		Title: result.Title, Year: result.Year, Rating: result.Rating, Overview: result.Overview,
		SpinCount: result.SpinCount, MaxSpins: result.MaxSpins,
	})
	kb := services.NewKeyboardBuilder()
	if result.SpinCount < result.MaxSpins {
		kb.AddButton("🎡 再转一次", "game_roulette")
	}
	kb.AddButton("🎮 游戏中心", "game_menu")
	kb.NewRow()
	kb.AddButton("🏠 主菜单", "start")
	return &callback.Response{RichMessage: card.Markdown, Keyboard: convertKeyboard(kb.Build())}, nil
}

// --- 游戏中心主菜单 ---

func (h *GameHandler) handleMenu(ctx *callback.Context) (*callback.Response, error) {
	card := richmessage.BuildGameCenterCard()

	return &callback.Response{
		RichMessage: card.Markdown,
		Keyboard:    convertKeyboard(services.BuildGameCenterKeyboard()),
	}, nil
}

// --- 性格测试 ---

// --- AI 解说员 ---

func (h *GameHandler) handleNarratorEntry(ctx *callback.Context) (*callback.Response, error) {
	// 设置 pending 状态，让下一条文本消息直接走解说流程
	if h.sessionMgr != nil {
		sess := h.sessionMgr.GetOrCreate(ctx.UserID)
		if sess != nil {
			// Some Telegram clients can emit the same tap twice. Once input mode is
			// active, acknowledge the duplicate without sending a second card.
			if _, exists := sess.Get("pending_narrate_input"); exists {
				return &callback.Response{CallbackMsg: "🎬 请直接发送电影名称"}, nil
			}
			sess.Set("pending_narrate_input", true)
		}
	}
	return &callback.Response{
		RichMessage: "## 🎬 AI 电影解说员\n\n请直接发送电影名称，我来给你讲讲这部电影。\n\n**例如**：`流浪地球` 或 `Inception`\n\n> 也可以使用命令：`/narrate 电影名`",
	}, nil
}

func (h *GameHandler) handleNarrate(ctx *callback.Context) (*callback.Response, error) {
	if h.narratorSvc == nil {
		return &callback.Response{CallbackMsg: "❌ AI 服务未就绪", ShowAlert: true}, nil
	}

	// 新按钮只携带模式，电影名保存在用户会话中，避免超过 Telegram
	// callback_data 的 64-byte 上限。旧版含 name 参数的按钮继续兼容。
	movieName := ""
	spoilerMode := false
	params := map[string]string{}
	if ctx.Callback != nil && ctx.Callback.Params != nil {
		params = ctx.Callback.Params
	}
	if name, ok := params["name"]; ok {
		movieName = name
	}
	if sp, ok := params["spoiler"]; ok && sp == "1" {
		spoilerMode = true
	}
	if movieName == "" && h.sessionMgr != nil {
		if sess := h.sessionMgr.GetOrCreate(ctx.UserID); sess != nil {
			ref := params["ref"]
			if ref != "" {
				movieName, _ = sess.GetString("narrate_movie_" + ref)
			} else {
				// Compatibility for short callbacks sent before per-card refs.
				movieName, _ = sess.GetString("narrate_movie_name")
			}
		}
	}
	if movieName == "" {
		return &callback.Response{CallbackMsg: "解说状态已过期，请重新发送电影名", ShowAlert: true}, nil
	}
	if h.sessionMgr != nil {
		sess := h.sessionMgr.GetOrCreate(ctx.UserID)
		ref := callback.ShortRef(movieName)
		sess.Set("narrate_movie_name", movieName)
		sess.Set("narrate_movie_"+ref, movieName)
	}

	// 异步生成：先返回"生成中"，后台完成后发新消息
	go h.generateNarrationAsync(ctx.UserID, ctx.ChatID, movieName, spoilerMode)

	return &callback.Response{
		CallbackMsg: "🎬 正在生成解说...",
		ShowAlert:   false,
	}, nil
}

// generateNarrationAsync 异步生成解说并发送结果
func (h *GameHandler) generateNarrationAsync(userID int64, chatID int64, movieName string, spoilerMode bool) {
	sender := newUserScopedSender(h.telegram, chatID, userID)
	defer func() {
		if r := recover(); r != nil {
			logger.Info("[Game] Narration panic for user %d: %v", userID, r)
			sender.SendMessage("❌ AI 解说出错了，请稍后再试", "", nil)
		}
	}()

	// 搜索电影信息
	title, year, genres, rating, err := h.narratorSvc.SearchMovie(movieName)
	if err != nil {
		title = movieName
	}

	// 生成解说
	result, err := h.narratorSvc.GenerateNarration(title, year, spoilerMode)
	if err != nil {
		logger.Info("[Game] Narration failed for user %d: %v", userID, err)
		sender.SendMessage("❌ AI 解说生成失败，请稍后再试", "", nil)
		return
	}
	result.Rating = rating
	result.Genres = genres

	card := richmessage.BuildNarratorCard(richmessage.NarratorCardData{
		Title:       result.Title,
		Year:        result.Year,
		Summary:     result.Summary,
		KeyPoints:   result.KeyPoints,
		Mood:        result.Mood,
		Similar:     result.Similar,
		Rating:      result.Rating,
		Genres:      result.Genres,
		SpoilerMode: result.SpoilerMode,
	})

	ref := callback.ShortRef(movieName)
	if h.sessionMgr != nil {
		sess := h.sessionMgr.GetOrCreate(userID)
		sess.Set("narrate_movie_name", movieName)
		sess.Set("narrate_movie_"+ref, movieName)
	}
	kb := services.NewKeyboardBuilder()
	if spoilerMode {
		kb.AddButton("🔇 无剧透版", "game_narrate:ref:"+ref+":spoiler:0")
	} else {
		kb.AddButton("🔥 剧透版", "game_narrate:ref:"+ref+":spoiler:1")
	}
	kb.AddButton("🎬 换一部", "game_narrator")
	kb.NewRow()
	kb.AddButton("🎮 游戏中心", "game_menu")

	sender.SendRichMessage(card.Markdown, kb.Build())
}

// HandleNarrateText 处理文本消息中的电影解说请求（由 poll.go 调用）
// 返回 true 表示已处理，false 表示不是解说请求
func (h *GameHandler) HandleNarrateText(userID int64, chatID int64, movieName string) bool {
	if h.narratorSvc == nil || h.sessionMgr == nil {
		return false
	}

	sess := h.sessionMgr.GetOrCreate(userID)
	if sess == nil {
		return false
	}

	// 检查是否处于 pending 状态
	if _, exists := sess.Get("pending_narrate_input"); !exists {
		return false
	}

	// 清除 pending 状态
	sess.Delete("pending_narrate_input")

	// 搜索电影信息
	title, year, genres, rating, err := h.narratorSvc.SearchMovie(movieName)
	if err != nil {
		title = movieName
	}

	// 生成解说
	result, err := h.narratorSvc.GenerateNarration(title, year, false)
	if err != nil {
		logger.Info("[Game] Narration failed for user %d: %v", userID, err)
		h.telegram.SendMessage(chatID, "❌ AI 解说生成失败，请稍后再试", "", nil)
		return true
	}
	result.Rating = rating
	result.Genres = genres

	card := richmessage.BuildNarratorCard(richmessage.NarratorCardData{
		Title:       result.Title,
		Year:        result.Year,
		Summary:     result.Summary,
		KeyPoints:   result.KeyPoints,
		Mood:        result.Mood,
		Similar:     result.Similar,
		Rating:      result.Rating,
		Genres:      result.Genres,
		SpoilerMode: result.SpoilerMode,
	})

	ref := callback.ShortRef(movieName)
	sess.Set("narrate_movie_name", movieName)
	sess.Set("narrate_movie_"+ref, movieName)
	kb := services.NewKeyboardBuilder()
	kb.AddButton("🔥 剧透版", "game_narrate:ref:"+ref+":spoiler:1")
	kb.AddButton("🎬 换一部", "game_narrator")
	kb.NewRow()
	kb.AddButton("🎮 游戏中心", "game_menu")

	newUserScopedSender(h.telegram, chatID, userID).SendRichMessage(card.Markdown, kb.Build())
	return true
}

// --- 工具函数 ---

// requirePrivate 要求私聊环境
func requirePrivate(ctx *callback.Context) bool {
	return ctx.ChatType == "private"
}

// --- 盲盒 ---

func (h *GameHandler) handleBlindBox(ctx *callback.Context) (*callback.Response, error) {
	card := richmessage.BuildBlindBoxCard(richmessage.BlindBoxCardData{
		Items: []richmessage.BlindBoxItemView{
			{Revealed: false, Rarity: "?"},
			{Revealed: false, Rarity: "?"},
			{Revealed: false, Rarity: "?"},
		},
	})

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🎰 开盲盒！", "game_blindbox_open")
	kb.AddButton("🎲 恐怖盲盒", "game_blindbox_horror")
	kb.NewRow()
	kb.AddButton("🎮 游戏中心", "game_menu")
	kb.AddButton("🏠 主菜单", "start")

	return &callback.Response{
		RichMessage: card.Markdown,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
}

func (h *GameHandler) handleBlindBoxOpen(ctx *callback.Context) (*callback.Response, error) {
	if h.blindBoxSvc == nil {
		return &callback.Response{CallbackMsg: "❌ 盲盒服务未就绪", ShowAlert: true}, nil
	}

	items, err := h.blindBoxSvc.OpenBlindBox("", 3)
	if err != nil {
		logger.Info("[Game] BlindBox open failed for user %d: %v", ctx.UserID, err)
		return &callback.Response{Text: "❌ 开盒失败，请稍后再试", CallbackMsg: "开盒失败", ShowAlert: true}, nil
	}

	var views []richmessage.BlindBoxItemView
	for _, item := range items {
		views = append(views, richmessage.BlindBoxItemView{
			Title:    item.Title,
			Year:     item.Year,
			Rating:   item.Rating,
			Rarity:   item.Rarity,
			Genres:   strings.Join(item.Genres, "/"),
			Overview: item.Overview,
			Revealed: true,
		})
	}

	card := richmessage.BuildBlindBoxCard(richmessage.BlindBoxCardData{Items: views})

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🎰 再开一组", "game_blindbox_open")
	kb.NewRow()
	kb.AddButton("🎮 游戏中心", "game_menu")

	return &callback.Response{
		RichMessage: card.Markdown,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
}

// --- 恐怖盲盒 ---

func (h *GameHandler) handleBlindBoxHorror(ctx *callback.Context) (*callback.Response, error) {
	if h.blindBoxSvc == nil {
		return &callback.Response{CallbackMsg: "❌ 盲盒服务未就绪", ShowAlert: true}, nil
	}
	items, err := h.blindBoxSvc.OpenBlindBox("恐怖", 3)
	if err != nil {
		logger.Info("[Game] Horror BlindBox open failed for user %d: %v", ctx.UserID, err)
		return &callback.Response{Text: "❌ 开盒失败，请稍后再试", CallbackMsg: "开盒失败", ShowAlert: true}, nil
	}
	views := make([]richmessage.BlindBoxItemView, 0, len(items))
	for _, item := range items {
		views = append(views, richmessage.BlindBoxItemView{
			Title: item.Title, Year: item.Year, Rating: item.Rating, Rarity: item.Rarity,
			Genres: strings.Join(item.Genres, "/"), Overview: item.Overview, Revealed: true,
		})
	}
	card := richmessage.BuildBlindBoxCard(richmessage.BlindBoxCardData{Items: views})
	kb := services.NewKeyboardBuilder()
	kb.AddButton("🎲 再来一组恐怖片", "game_blindbox_horror")
	kb.AddButton("🎰 普通盲盒", "game_blindbox_open")
	kb.NewRow()
	kb.AddButton("🎮 游戏中心", "game_menu")
	return &callback.Response{RichMessage: card.Markdown, Keyboard: convertKeyboard(kb.Build())}, nil
}

// --- 社交动态 ---

func (h *GameHandler) handleSocialFeed(ctx *callback.Context) (*callback.Response, error) {
	if !requirePrivate(ctx) {
		return &callback.Response{CallbackMsg: "🔒 影友圈需要私聊查看", ShowAlert: true}, nil
	}
	if h.socialDB == nil {
		return &callback.Response{CallbackMsg: "❌ 社交服务未就绪", ShowAlert: true}, nil
	}

	events, err := h.socialDB.GetRecentEvents(10)
	if err != nil {
		return &callback.Response{Text: "❌ 获取动态失败", CallbackMsg: "获取失败", ShowAlert: true}, nil
	}

	var views []richmessage.SocialEventView
	for _, e := range events {
		views = append(views, richmessage.SocialEventView{
			UserName:  e.UserName,
			EventType: e.EventType,
			Content:   e.Content,
			TimeAgo:   formatTimeAgo(e.CreatedAt),
		})
	}

	card := richmessage.BuildSocialFeedCard(richmessage.SocialFeedCardData{Events: views})

	kb := services.NewKeyboardBuilder()
	kb.AddButton("✍️ 写影评", "game_review")
	kb.AddButton("🔄 刷新", "game_social")
	kb.NewRow()
	kb.AddButton("🎮 游戏中心", "game_menu")
	kb.AddButton("🏠 主菜单", "start")

	return &callback.Response{
		RichMessage: card.Markdown,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
}

// --- 影评 ---

// --- 命运轮盘 ---

// --- 工具函数 ---

// --- 观影画像 ---

func (h *GameHandler) handleEmotionProfile(ctx *callback.Context) (*callback.Response, error) {
	if h.emotionSvc == nil || h.userMapping == nil || h.viewingSvc == nil {
		return &callback.Response{CallbackMsg: "❌ 服务未就绪", ShowAlert: true}, nil
	}
	if !requirePrivate(ctx) {
		return &callback.Response{CallbackMsg: "🔒 观影画像是你的私人数据，请私聊查看", ShowAlert: true}, nil
	}

	mpUsername, err := h.userMapping.GetMoviePilotUsername(ctx.UserID)
	if err != nil || mpUsername == "" {
		return &callback.Response{Text: "🔗 请先绑定账号（/link）", CallbackMsg: "请先绑定", ShowAlert: true}, nil
	}

	embyUserID, err := h.viewingSvc.FindEmbyUserByName(mpUsername)
	if err != nil {
		return &callback.Response{Text: "❌ 未找到 Emby 用户", CallbackMsg: "未找到用户", ShowAlert: true}, nil
	}

	profile, err := h.emotionSvc.BuildProfile(embyUserID, mpUsername)
	if err != nil {
		logger.Info("[Game] Emotion profile failed for user %d: %v", ctx.UserID, err)
		return &callback.Response{Text: "❌ 情绪分析失败，请稍后再试", CallbackMsg: "分析失败", ShowAlert: true}, nil
	}

	// 转换 types
	var topGenres []richmessage.GenreCountView
	for _, g := range profile.TopGenres {
		topGenres = append(topGenres, richmessage.GenreCountView{Genre: g.Genre, Count: g.Count})
	}
	var transitions []richmessage.GenreTransitionView
	for _, t := range profile.GenreTransitions {
		transitions = append(transitions, richmessage.GenreTransitionView{From: t.From, To: t.To, Direction: t.Direction})
	}

	card := richmessage.BuildEmotionProfileCard(richmessage.EmotionProfileCardData{
		UserName:           mpUsername,
		PersonalityTag:     profile.PersonalityTag,
		SignatureGenre:     profile.SignatureGenre,
		EmotionalIntensity: profile.EmotionalIntensity,
		EmotionTrend:       profile.EmotionTrend,
		CurrentMood:        profile.CurrentMood,
		LifePhase:          profile.LifePhase,
		TopGenres:          topGenres,
		MovieCount:         profile.MovieCount,
		SeriesCount:        profile.SeriesCount,
		WatchDays:          profile.WatchDays,
		WatchStreak:        profile.WatchStreak,
		Pattern: richmessage.ViewingPatternView{
			PeakHour:   profile.Pattern.PeakHour,
			PeakPeriod: profile.Pattern.PeakPeriod,
			WeekdayAvg: profile.Pattern.WeekdayAvg,
			WeekendAvg: profile.Pattern.WeekendAvg,
			IsNightOwl: profile.Pattern.IsNightOwl,
		},
		Transitions:    transitions,
		TasteSignature: profile.TasteSignature,
		RecentMovies:   profile.RecentMovies,
	})

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🎰 开盲盒", "game_blindbox")
	kb.NewRow()
	kb.AddButton("🎮 游戏中心", "game_menu")

	return &callback.Response{
		RichMessage: card.Markdown,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
}

// --- 时光放映机 ---

// --- 情绪处方 ---

// --- 命运契约 ---

// notifyGroup 发送群通知（异步，不阻塞主流程）
// notifyGroup 发送群通知（10分钟后自毁）
func (h *GameHandler) notifyGroup(userName, message string) {
	if h.groupChatID == 0 || h.telegram == nil {
		return
	}
	go func() {
		text := fmt.Sprintf("🎮 **%s** %s", userName, message)
		sent, err := h.telegram.SendMessage(h.groupChatID, text, "Markdown", nil)
		if err != nil {
			return
		}
		// 10分钟后自毁
		go func(chatID int64, msgID int64) {
			time.Sleep(10 * time.Minute)
			_ = h.telegram.DeleteMessage(chatID, msgID)
		}(h.groupChatID, sent.MessageID)
	}()
}

// getUserName 获取用户显示名
func (h *GameHandler) getUserName(userID int64) string {
	if h.userMapping != nil {
		if name, err := h.userMapping.GetMoviePilotUsername(userID); err == nil && name != "" {
			return name
		}
	}
	return fmt.Sprintf("用户%d", userID)
}

func formatTimeAgo(t time.Time) string {
	diff := time.Since(t)
	switch {
	case diff < time.Minute:
		return "刚刚"
	case diff < time.Hour:
		return fmt.Sprintf("%d分钟前", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%d小时前", int(diff.Hours()))
	case diff < 7*24*time.Hour:
		return fmt.Sprintf("%d天前", int(diff.Hours()/24))
	default:
		return t.Format("01-02")
	}
}

// --- 完成契约 ---

// handleCompareTaste 处理观影关系对比
func (h *GameHandler) handleCompareTaste(ctx *callback.Context) (*callback.Response, error) {
	if h.viewingSvc == nil {
		return &callback.Response{Text: "❌ 观影历史服务未就绪", CallbackMsg: "服务未就绪", ShowAlert: true}, nil
	}

	userName := h.getUserName(ctx.UserID)
	embyUserID, err := h.viewingSvc.FindEmbyUserByName(userName)
	if err != nil || embyUserID == "" {
		return &callback.Response{Text: "🔗 观影品味需要先绑定与 Emby 同名的账号（/link）", CallbackMsg: "请先绑定账号", ShowAlert: true}, nil
	}
	profile, err := h.viewingSvc.GetProfile(embyUserID, userName)
	if err != nil || profile == nil {
		return &callback.Response{Text: "📊 还没有观影数据，先看几部电影再来吧~", CallbackMsg: "暂无数据", ShowAlert: true}, nil
	}

	total := len(profile.Records)
	if total == 0 {
		return &callback.Response{Text: "📊 还没有观影数据，先看几部电影再来吧~", CallbackMsg: "暂无数据", ShowAlert: true}, nil
	}

	// 转换类型
	var genres []richmessage.ViewingGenreCount
	for _, g := range profile.TopGenres {
		if len(genres) >= 6 {
			break
		}
		genres = append(genres, richmessage.ViewingGenreCount{
			Genre: g.Genre,
			Count: g.Count,
		})
	}

	// 构建品味分析卡片
	card := richmessage.BuildTasteCard(richmessage.TasteCardData{
		UserName:   userName,
		TotalViews: total,
		TopGenres:  genres,
	})

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🎮 游戏中心", "game_menu")
	kb.AddButton("🧠 观影画像", "portrait")

	return &callback.Response{
		RichMessage: card.Markdown,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
}

// handleAchievements 处理成就系统
func (h *GameHandler) handleAchievements(ctx *callback.Context) (*callback.Response, error) {
	achievements := services.AllAchievements

	var lines []string
	lines = append(lines, "🏆 **成就殿堂**")
	lines = append(lines, "")

	// 按分类分组展示
	categories := map[string]string{
		"watch":     "🎬 观影之路",
		"social":    "👥 社交达人",
		"explore":   "🔍 探索发现",
		"challenge": "⚔️ 挑战极限",
	}

	displayed := 0
	for cat, label := range categories {
		lines = append(lines, label)
		count := 0
		for _, a := range achievements {
			if a.Category == cat {
				lines = append(lines, fmt.Sprintf("  %s %s — %s", a.Icon, a.Name, a.Description))
				count++
				displayed++
				if count >= 3 {
					break
				}
			}
		}
		lines = append(lines, "")
		if displayed >= 12 {
			break
		}
	}

	lines = append(lines, fmt.Sprintf("共 %d 个成就等待解锁", len(achievements)))

	text := strings.Join(lines, "\n")

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🎮 游戏中心", "game_menu")

	return &callback.Response{
		Text:     text,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}
