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
	rankSvc        *services.RankService
	personalitySvc *services.PersonalityService
	narratorSvc    *services.NarratorService
	blindBoxSvc    *services.BlindBoxService
	socialDB       *services.SocialDB
	rouletteSvc    *services.RouletteService
	userMapping    services.UserMappingStore
	telegram       *services.TelegramClient
	sessionMgr     *session.Manager
	emotionSvc     *services.EmotionTimelineService
	adventureHdl   *AdventureHandler // 求片大冒险
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
	rankSvc *services.RankService,
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
	adventureHdl *AdventureHandler,
) *GameHandler {
	return &GameHandler{
		rankSvc:        rankSvc,
		personalitySvc: personalitySvc,
		narratorSvc:    narratorSvc,
		blindBoxSvc:    blindBoxSvc,
		socialDB:       socialDB,
		rouletteSvc:    rouletteSvc,
		userMapping:    userMapping,
		telegram:       telegram,
		sessionMgr:     sessionMgr,
		emotionSvc:     emotionSvc,
		adventureHdl:   adventureHdl,
		groupChatID:    groupChatID,
	}
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
	case "game_adventure_rank":
		return h.handleAdventureRank(ctx)
	case "game_adventure_stats":
		return h.handleAdventureStats(ctx)
	case "game_daily_challenge":
		return h.handleDailyChallenge(ctx)
	// 以下功能已废弃，返回友好提示
	case "game_rank", "game_social", "game_emotion", "game_achievements",
		"game_contract", "game_prescription", "game_time_machine",
		"game_roulette", "game_review", "game_compare":
		return &callback.Response{CallbackMsg: "🚧 该功能已升级，请从游戏中心进入", ShowAlert: true}, nil
	default:
		return nil, fmt.Errorf("unknown game action: %s", action)
	}
}

// --- 游戏中心主菜单 ---

func (h *GameHandler) handleMenu(ctx *callback.Context) (*callback.Response, error) {
	card := richmessage.BuildGameCenterCard()
	kb := services.NewKeyboardBuilder()
	kb.AddButton("⚔️ 求片大冒险", "adventure_start")
	kb.AddButton("📖 情报站", "game_narrator")
	kb.NewRow()
	kb.AddButton("📊 冒险排行", "game_adventure_rank")
	kb.AddButton("🎯 每日挑战", "game_daily_challenge")
	kb.NewRow()
	kb.AddButton("🎰 通关盲盒", "game_blindbox")

	return &callback.Response{
		RichMessage: card.Markdown,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
}

// --- 段位系统 ---

func (h *GameHandler) handleRank(ctx *callback.Context) (*callback.Response, error) {
	if h.rankSvc == nil || h.userMapping == nil {
		return &callback.Response{CallbackMsg: "❌ 段位服务未就绪", ShowAlert: true}, nil
	}
	if !requirePrivate(ctx) {
		return &callback.Response{CallbackMsg: "🔒 段位是你的私人数据，请私聊查看", ShowAlert: true}, nil
	}

	mpUsername, err := h.userMapping.GetMoviePilotUsername(ctx.UserID)
	if err != nil || mpUsername == "" {
		return &callback.Response{Text: "🔗 请先绑定账号（/link）", CallbackMsg: "请先绑定", ShowAlert: true}, nil
	}

	embyUserID, err := h.rankSvc.FindEmbyUserByName(mpUsername)
	if err != nil {
		return &callback.Response{Text: "❌ 未找到 Emby 用户", CallbackMsg: "未找到用户", ShowAlert: true}, nil
	}

	result, err := h.rankSvc.CalculateRank(embyUserID, mpUsername)
	if err != nil {
		logger.Info("[Game] Rank calculation failed for user %d: %v", ctx.UserID, err)
		return &callback.Response{Text: "❌ 段位计算失败，请稍后再试", CallbackMsg: "计算失败", ShowAlert: true}, nil
	}

	card := richmessage.BuildRankCard(richmessage.RankCardData{
		UserName:     result.UserName,
		TierName:     result.Tier.Name,
		TierIcon:     result.Tier.Icon,
		Score:        result.Score,
		TotalMovies:  result.TotalMovies,
		TotalSeries:  result.TotalSeries,
		GenreCount:   result.GenreCount,
		AvgRating:    result.AvgRating,
		Badges:       result.Badges,
		NextTier:     result.NextTier,
		NextTierDiff: result.NextTierDiff,
		TopGenre:     result.TopGenre,
	})

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🪞 情绪画像", "game_emotion")
	kb.AddButton("💊 情绪处方", "game_prescription")
	kb.NewRow()
	kb.AddButton("📜 签契约", "game_contract")
	kb.AddButton("🎮 游戏中心", "game_menu")

	return &callback.Response{
		RichMessage: card.Markdown,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
}

// --- 性格测试 ---


// --- AI 解说员 ---

func (h *GameHandler) handleNarratorEntry(ctx *callback.Context) (*callback.Response, error) {
	// 设置 pending 状态，让下一条文本消息直接走解说流程
	if h.sessionMgr != nil {
		sess := h.sessionMgr.GetOrCreate(ctx.UserID)
		if sess != nil {
			sess.Set("pending_narrate_input", true)
		}
	}
	return &callback.Response{
		Text: "🎬 **AI 电影解说员**\n\n请直接发送电影名称，我来给你讲讲这部电影～\n\n例如：发送 `流浪地球` 或 `Inception`\n\n💡 也可以使用命令：`/narrate 电影名`",
	}, nil
}

func (h *GameHandler) handleNarrate(ctx *callback.Context) (*callback.Response, error) {
	if h.narratorSvc == nil {
		return &callback.Response{CallbackMsg: "❌ AI 服务未就绪", ShowAlert: true}, nil
	}

	// 从 callback params 中提取电影名和剧透模式
	movieName := ""
	spoilerMode := false
	if ctx.Callback != nil && ctx.Callback.Params != nil {
		if name, ok := ctx.Callback.Params["name"]; ok {
			movieName = name
		}
		if sp, ok := ctx.Callback.Params["spoiler"]; ok && sp == "1" {
			spoilerMode = true
		}
	}
	if movieName == "" {
		return &callback.Response{CallbackMsg: "❌ 缺少电影名", ShowAlert: true}, nil
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
	defer func() {
		if r := recover(); r != nil {
			logger.Info("[Game] Narration panic for user %d: %v", userID, r)
			h.telegram.SendMessage(chatID, "❌ AI 解说出错了，请稍后再试", "", nil)
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
		h.telegram.SendMessage(chatID, "❌ AI 解说生成失败，请稍后再试", "", nil)
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

	kb := services.NewKeyboardBuilder()
	if spoilerMode {
		kb.AddButton("🔇 无剧透版", fmt.Sprintf("game_narrate:name:%s", movieName))
	} else {
		kb.AddButton("🔥 剧透版", fmt.Sprintf("game_narrate:spoiler:1:name:%s", movieName))
	}
	kb.AddButton("🎬 换一部", "game_narrator")
	kb.NewRow()
	kb.AddButton("🎮 游戏中心", "game_menu")

	h.telegram.SendMessage(chatID, card.Markdown, "Markdown", kb.Build())
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

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🔥 剧透版", fmt.Sprintf("game_narrate:spoiler:1:name:%s", movieName))
	kb.AddButton("🎬 换一部", "game_narrator")
	kb.NewRow()
	kb.AddButton("🎮 游戏中心", "game_menu")

	h.telegram.SendMessage(chatID, card.Markdown, "Markdown", kb.Build())
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
	kb.AddButton("⬅️ 返回", "start")

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

	// 群通知：开出SSR/SR时通知群聊
	userName := h.getUserName(ctx.UserID)
	for _, item := range items {
		if item.Rarity == "SSR" {
			h.notifyGroup(userName, fmt.Sprintf("开出了🟡SSR盲盒：《%s》！恭喜！", item.Title))
		} else if item.Rarity == "SR" {
			h.notifyGroup(userName, fmt.Sprintf("开出了🟣SR盲盒：《%s》", item.Title))
		}
	}

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🎰 再开一组", "game_blindbox_open")
	kb.AddButton("📜 和它签契约", "game_contract")
	kb.NewRow()
	kb.AddButton("🎮 游戏中心", "game_menu")

	return &callback.Response{
		RichMessage: card.Markdown,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
}

// --- 恐怖盲盒 ---


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
	kb.AddButton("⬅️ 返回", "start")

	return &callback.Response{
		RichMessage: card.Markdown,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
}

// --- 影评 ---



// --- 命运轮盘 ---



// --- 工具函数 ---

// --- 情绪画像 ---

func (h *GameHandler) handleEmotionProfile(ctx *callback.Context) (*callback.Response, error) {
	if h.emotionSvc == nil || h.userMapping == nil || h.rankSvc == nil {
		return &callback.Response{CallbackMsg: "❌ 服务未就绪", ShowAlert: true}, nil
	}
	if !requirePrivate(ctx) {
		return &callback.Response{CallbackMsg: "🔒 情绪画像是你的私人数据，请私聊查看", ShowAlert: true}, nil
	}

	mpUsername, err := h.userMapping.GetMoviePilotUsername(ctx.UserID)
	if err != nil || mpUsername == "" {
		return &callback.Response{Text: "🔗 请先绑定账号（/link）", CallbackMsg: "请先绑定", ShowAlert: true}, nil
	}

	embyUserID, err := h.rankSvc.FindEmbyUserByName(mpUsername)
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
		Transitions: transitions,
		TasteSignature: profile.TasteSignature,
		RecentMovies:   profile.RecentMovies,
	})

	// 群通知：首次查看情绪画像时通知
	h.notifyGroup(mpUsername, fmt.Sprintf("解锁了观影人格：「%s」🪞", profile.PersonalityTag))

	kb := services.NewKeyboardBuilder()
	kb.AddButton("📽️ 时光放映机", "game_time_machine")
	kb.AddButton("💊 情绪处方", "game_prescription")
	kb.NewRow()
	kb.AddButton("📜 签一份契约", "game_contract")
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

// handleDailyChallenge 处理每日挑战

// handleDailyChallengeComplete 处理完成每日挑战

// handleAdventureStats 处理冒险统计
func (h *GameHandler) handleAdventureStats(ctx *callback.Context) (*callback.Response, error) {
	if !requirePrivate(ctx) {
		return &callback.Response{CallbackMsg: "🔒 冒险统计请私聊查看", ShowAlert: true}, nil
	}
	if h.socialDB == nil {
		return &callback.Response{CallbackMsg: "❌ 服务未就绪", ShowAlert: true}, nil
	}

	stats, err := h.socialDB.GetAdventureStats(ctx.UserID)
	if err != nil {
		return &callback.Response{Text: "❌ 获取统计失败", CallbackMsg: "获取失败", ShowAlert: true}, nil
	}

	// 获取连胜数据
	streak, _ := h.socialDB.GetAdventureStreak(ctx.UserID)

	userName := h.getUserName(ctx.UserID)

	var records []richmessage.AdventureRecordView
	for _, r := range stats.RecentRecords {
		records = append(records, richmessage.AdventureRecordView{
			MovieName: r.MovieName,
			MovieYear: r.MovieYear,
			Score:     r.Score,
			Grade:     r.Grade,
			Success:   r.Success,
			MaxCombo:  r.MaxCombo,
			TimeAgo:   formatTimeAgo(r.CreatedAt),
		})
	}

	card := richmessage.BuildAdventureStatsCard(richmessage.AdventureStatsCardData{
		UserName:        userName,
		TotalChallenges: stats.TotalChallenges,
		TotalSuccess:    stats.TotalSuccess,
		BestScore:       stats.BestScore,
		BestGrade:       stats.BestGrade,
		BestCombo:       stats.BestCombo,
		PerfectRuns:     stats.PerfectRuns,
		RecentRecords:   records,
		CurrentStreak:   streak.CurrentStreak,
		BestStreak:      streak.BestStreak,
	})

	kb := services.NewKeyboardBuilder()
	kb.AddButton("⚔️ 再来一局", "adventure_start")
	kb.AddButton("🎮 游戏中心", "game_menu")

	return &callback.Response{
		RichMessage: card.Markdown,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
}

// handleAchievements 处理成就系统
func (h *GameHandler) handleAchievements(ctx *callback.Context) (*callback.Response, error) {
	return &callback.Response{CallbackMsg: "🚧 成就系统升级中，请从游戏中心进入新功能", ShowAlert: true}, nil
}

// --- 冒险排行榜 ---

func (h *GameHandler) handleAdventureRank(ctx *callback.Context) (*callback.Response, error) {
	if h.socialDB == nil {
		return &callback.Response{CallbackMsg: "❌ 服务未就绪", ShowAlert: true}, nil
	}

	// 获取排行榜数据
	topEntries, err := h.socialDB.GetAdventureLeaderboard(10)
	if err != nil {
		return &callback.Response{Text: "❌ 获取排行榜失败", CallbackMsg: "获取失败", ShowAlert: true}, nil
	}

	// 转换类型
	var topPlayers []richmessage.AdventureRankPlayer
	for _, e := range topEntries {
		topPlayers = append(topPlayers, richmessage.AdventureRankPlayer{
			Rank:         0, // 由card builder分配
			UserName:     e.UserName,
			BestScore:    e.BestScore,
			BestGrade:    e.BestGrade,
			BestCombo:    e.BestCombo,
			TotalSuccess: e.TotalSuccess,
			PerfectRuns:  e.PerfectRuns,
		})
		// 分配rank
		topPlayers[len(topPlayers)-1].Rank = len(topPlayers)
	}

	userName := h.getUserName(ctx.UserID)

	card := richmessage.BuildAdventureRankCard(richmessage.AdventureRankCardData{
		UserName:    userName,
		TopPlayers:  topPlayers,
	})

	kb := services.NewKeyboardBuilder()
	kb.AddButton("⚔️ 挑战霸榜", "adventure_start")
	kb.AddButton("🎮 游戏中心", "game_menu")

	return &callback.Response{
		RichMessage: card.Markdown,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
}

// --- 每日挑战 ---

func (h *GameHandler) handleDailyChallenge(ctx *callback.Context) (*callback.Response, error) {
	// 每日挑战：基于日期生成一个固定的电影推荐
	challengeMovies := []struct {
		Title string
		Year  int
		Genre string
		Hint  string
	}{
		{"盗梦空间", 2010, "科幻/悬疑", "那个陀螺到底倒了没？"},
		{"肖申克的救赎", 1994, "剧情", "有些鸟是关不住的"},
		{"星际穿越", 2014, "科幻", "爱是唯一能穿越时空的力量"},
		{"搏击俱乐部", 1999, "悬疑/动作", "第一条规则：不要谈论搏击俱乐部"},
		{"寄生虫", 2019, "剧情/悬疑", "你闻到了什么味道？"},
		{"泰坦尼克号", 1997, "爱情/灾难", "你跳我也跳"},
		{"黑客帝国", 1999, "科幻/动作", "红药丸还是蓝药丸？"},
		{"千与千寻", 2001, "动画/奇幻", "不要忘记自己的名字"},
		{"让子弹飞", 2010, "喜剧/动作", "站着，还把钱挣了"},
		{"楚门的世界", 1998, "剧情", "如果一切都是假的呢？"},
		{"阿甘正传", 1994, "剧情", "人生就像一盒巧克力"},
		{"沉默的羔羊", 1991, "悬疑/惊悚", "你好吗，克拉丽斯？"},
		{"无间道", 2002, "犯罪/悬疑", "我想做个好人"},
		{"少年派的奇幻漂流", 2012, "奇幻/剧情", "你相信哪个故事？"},
		{"疯狂的麦克斯：狂暴之路", 2015, "动作/科幻", "见证我！"},
		{"布达佩斯大饭店", 2014, "喜剧/剧情", "在这个野蛮的酒店里保持文明"},
		{"禁闭岛", 2010, "悬疑/惊悚", "哪个才是真相？"},
		{"V字仇杀队", 2005, "动作/科幻", "面具下面是一个理念"},
		{"大话西游", 1995, "喜剧/爱情", "曾经有一份真诚的爱情"},
		{"电锯惊魂", 2004, "恐怖/悬疑", "你想玩个游戏吗？"},
	}

	// 用日期作为索引，每天不同
	dayIndex := time.Now().YearDay() % len(challengeMovies)
	challenge := challengeMovies[dayIndex]

	// 检查今天是否已挑战
	alreadyChallenged := false
	if h.socialDB != nil {
		alreadyChallenged, _ = h.socialDB.HasDailyChallenge(ctx.UserID, time.Now().Format("2006-01-02"))
	}

	card := richmessage.BuildDailyChallengeCard(richmessage.DailyChallengeCardData{
		MovieTitle: challenge.Title,
		MovieYear:  challenge.Year,
		Genre:      challenge.Genre,
		Hint:       challenge.Hint,
		Completed:  alreadyChallenged,
		DayStreak:  0, // TODO: 连续挑战天数
	})

	kb := services.NewKeyboardBuilder()
	if !alreadyChallenged {
		kb.AddButton("⚔️ 接受挑战", "adventure_start")
	} else {
		kb.AddButton("⚔️ 再来一局", "adventure_start")
	}
	kb.AddButton("🎮 游戏中心", "game_menu")

	return &callback.Response{
		RichMessage: card.Markdown,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
}
