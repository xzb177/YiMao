package handlers

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/richmessage"
	"github.com/xzb177/yimao/internal/services"
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
	}
}

// Handle 游戏中心路由
func (h *GameHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	action := string(ctx.Callback.Action)

	switch {
	case action == "game_rank":
		return h.handleRank(ctx)
	case action == "game_personality":
		return h.handlePersonality(ctx)
	case action == "game_narrator":
		return h.handleNarratorEntry(ctx)
	case strings.HasPrefix(action, "game_narrate:"):
		return h.handleNarrate(ctx)
	case action == "game_blindbox":
		return h.handleBlindBox(ctx)
	case action == "game_blindbox_open":
		return h.handleBlindBoxOpen(ctx)
	case action == "game_social":
		return h.handleSocialFeed(ctx)
	case action == "game_review":
		return h.handleReviewEntry(ctx)
	case strings.HasPrefix(action, "game_review_rate:"):
		return h.handleReviewRate(ctx)
	case action == "game_roulette":
		return h.handleRoulette(ctx)
	case action == "game_roulette_spin":
		return h.handleRouletteSpin(ctx)
	case action == "game_menu":
		return h.handleMenu(ctx)
	default:
		return nil, fmt.Errorf("unknown game action: %s", action)
	}
}

// --- 游戏中心主菜单 ---

func (h *GameHandler) handleMenu(ctx *callback.Context) (*callback.Response, error) {
	card := richmessage.BuildGameCenterCard()
	kb := services.NewKeyboardBuilder()
	kb.AddButton("🏆 段位", "game_rank")
	kb.AddButton("🧠 性格", "game_personality")
	kb.AddButton("🎬 AI解说", "game_narrator")
	kb.NewRow()
	kb.AddButton("🎰 盲盒", "game_blindbox")
	kb.AddButton("📢 影友圈", "game_social")
	kb.AddButton("🎡 轮盘", "game_roulette")
	kb.NewRow()
	kb.AddButton("⬅️ 返回", "start")

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
	kb.AddButton("🔄 刷新段位", "game_rank")
	kb.AddButton("🧠 性格测试", "game_personality")
	kb.NewRow()
	kb.AddButton("🎮 游戏中心", "game_menu")
	kb.AddButton("⬅️ 返回", "start")

	return &callback.Response{
		RichMessage: card.Markdown,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
}

// --- 性格测试 ---

func (h *GameHandler) handlePersonality(ctx *callback.Context) (*callback.Response, error) {
	if h.personalitySvc == nil || h.userMapping == nil {
		return &callback.Response{CallbackMsg: "❌ 服务未就绪", ShowAlert: true}, nil
	}
	if !requirePrivate(ctx) {
		return &callback.Response{CallbackMsg: "🔒 性格测试需要私聊进行", ShowAlert: true}, nil
	}

	mpUsername, err := h.userMapping.GetMoviePilotUsername(ctx.UserID)
	if err != nil || mpUsername == "" {
		return &callback.Response{Text: "🔗 请先绑定账号（/link）", CallbackMsg: "请先绑定", ShowAlert: true}, nil
	}

	embyUserID, err := h.personalitySvc.FindEmbyUserByName(mpUsername)
	if err != nil {
		return &callback.Response{Text: "❌ 未找到 Emby 用户", CallbackMsg: "未找到用户", ShowAlert: true}, nil
	}

	result, err := h.personalitySvc.AnalyzePersonality(embyUserID, mpUsername)
	if err != nil {
		logger.Info("[Game] Personality analysis failed for user %d: %v", ctx.UserID, err)
		return &callback.Response{Text: "❌ 性格分析失败，请稍后再试", CallbackMsg: "分析失败", ShowAlert: true}, nil
	}

	var dims []richmessage.PDimensionView
	for _, d := range result.Dimensions {
		dims = append(dims, richmessage.PDimensionView{
			Name: d.Name, Left: d.Left, Right: d.Right,
			Score: d.Score, Result: d.Result, Icon: d.Icon,
		})
	}

	card := richmessage.BuildPersonalityCard(richmessage.PersonalityCardData{
		UserName:    result.UserName,
		Type:        result.Type,
		TypeName:    result.TypeName,
		Description: result.Description,
		Dimensions:  dims,
		TopTrait:    result.TopTrait,
	})

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🔄 重新测试", "game_personality")
	kb.AddButton("🏆 查看段位", "game_rank")
	kb.NewRow()
	kb.AddButton("🎮 游戏中心", "game_menu")
	kb.AddButton("⬅️ 返回", "start")

	return &callback.Response{
		RichMessage: card.Markdown,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
}

// --- AI 解说员 ---

func (h *GameHandler) handleNarratorEntry(ctx *callback.Context) (*callback.Response, error) {
	return &callback.Response{
		Text: "🎬 **AI 电影解说员**\n\n请直接发送电影名称，我来给你讲讲这部电影～\n\n例如：发送 `流浪地球` 或 `Inception`\n\n💡 也可以使用命令：`/narrate 电影名`",
	}, nil
}

func (h *GameHandler) handleNarrate(ctx *callback.Context) (*callback.Response, error) {
	if h.narratorSvc == nil {
		return &callback.Response{CallbackMsg: "❌ AI 服务未就绪", ShowAlert: true}, nil
	}

	// 从 callback params 中提取电影名
	movieName := ""
	if ctx.Callback != nil && ctx.Callback.Params != nil {
		if name, ok := ctx.Callback.Params["name"]; ok {
			movieName = name
		}
	}
	if movieName == "" {
		return &callback.Response{CallbackMsg: "❌ 缺少电影名", ShowAlert: true}, nil
	}

	// 搜索电影信息
	title, year, genres, rating, err := h.narratorSvc.SearchMovie(movieName)
	if err != nil {
		title = movieName
	}

	// 生成解说
	result, err := h.narratorSvc.GenerateNarration(title, year, false)
	if err != nil {
		logger.Info("[Game] Narration failed for user %d: %v", ctx.UserID, err)
		return &callback.Response{Text: "❌ AI 解说生成失败，请稍后再试", CallbackMsg: "生成失败", ShowAlert: true}, nil
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
	kb.AddButton("🔥 剧透版", fmt.Sprintf("game_narrate:name:%s", movieName))
	kb.AddButton("🎬 换一部", "game_narrator")
	kb.NewRow()
	kb.AddButton("🎮 游戏中心", "game_menu")

	return &callback.Response{
		RichMessage: card.Markdown,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
}

// --- 工具函数 ---

// requirePrivate 要求私聊环境
func requirePrivate(ctx *callback.Context) bool {
	return ctx.ChatType == "private"
}

// --- 盲盒 ---

func (h *GameHandler) handleBlindBox(ctx *callback.Context) (*callback.Response, error) {
	if !requirePrivate(ctx) {
		return &callback.Response{CallbackMsg: "🔒 盲盒需要私聊使用", ShowAlert: true}, nil
	}
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
	if !requirePrivate(ctx) {
		return &callback.Response{CallbackMsg: "🔒 盲盒需要私聊使用", ShowAlert: true}, nil
	}
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
	kb.AddButton("🎮 游戏中心", "game_menu")

	return &callback.Response{
		RichMessage: card.Markdown,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
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
	kb.AddButton("⬅️ 返回", "start")

	return &callback.Response{
		RichMessage: card.Markdown,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
}

// --- 影评 ---

func (h *GameHandler) handleReviewEntry(ctx *callback.Context) (*callback.Response, error) {
	return &callback.Response{
		Text: "✍️ **写影评**\n\n请按格式发送：\n`/review 电影名 评分(1-5) 评语`\n\n例如：`/review 流浪地球 5 特效炸裂，剧情紧凑`\n\n评分：⭐~⭐⭐⭐⭐⭐",
	}, nil
}

func (h *GameHandler) handleReviewRate(ctx *callback.Context) (*callback.Response, error) {
	// 从 callback params 中提取参数
	movieName := ""
	ratingStr := ""
	if ctx.Callback != nil && ctx.Callback.Params != nil {
		if name, ok := ctx.Callback.Params["movie"]; ok {
			movieName = name
		}
		if rate, ok := ctx.Callback.Params["rate"]; ok {
			ratingStr = rate
		}
	}
	if movieName == "" || ratingStr == "" {
		return &callback.Response{CallbackMsg: "❌ 参数错误", ShowAlert: true}, nil
	}

	rating, err := strconv.Atoi(ratingStr)
	if err != nil || rating < 1 || rating > 5 {
		return &callback.Response{CallbackMsg: "❌ 评分无效", ShowAlert: true}, nil
	}

	if h.socialDB == nil {
		return &callback.Response{CallbackMsg: "❌ 服务未就绪", ShowAlert: true}, nil
	}

	// 获取用户名
	mpUsername, _ := h.userMapping.GetMoviePilotUsername(ctx.UserID)
	if mpUsername == "" {
		mpUsername = fmt.Sprintf("用户%d", ctx.UserID)
	}

	err = h.socialDB.AddReview(ctx.UserID, mpUsername, movieName, 0, rating, "")
	if err != nil {
		logger.Info("[Game] Review rating failed for user %d: %v", ctx.UserID, err)
		return &callback.Response{Text: "❌ 评分失败，请稍后再试", CallbackMsg: "失败", ShowAlert: true}, nil
	}

	return &callback.Response{
		Text:        fmt.Sprintf("✅ 已为《%s》评分 %s", movieName, strings.Repeat("⭐", rating)),
		CallbackMsg: "评分成功！",
		ShowAlert:   true,
	}, nil
}

// --- 命运轮盘 ---

func (h *GameHandler) handleRoulette(ctx *callback.Context) (*callback.Response, error) {
	if !requirePrivate(ctx) {
		return &callback.Response{CallbackMsg: "🔒 轮盘需要私聊使用", ShowAlert: true}, nil
	}
	card := richmessage.BuildRouletteCard(richmessage.RouletteCardData{
		Title:    "等待命运的裁决...",
		SpinCount: 0,
		MaxSpins: 3,
	})

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🎡 转！","game_roulette_spin")
	kb.AddButton("🎮 游戏中心", "game_menu")
	kb.NewRow()
	kb.AddButton("⬅️ 返回", "start")

	return &callback.Response{
		RichMessage: card.Markdown,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
}

func (h *GameHandler) handleRouletteSpin(ctx *callback.Context) (*callback.Response, error) {
	if !requirePrivate(ctx) {
		return &callback.Response{CallbackMsg: "🔒 轮盘需要私聊使用", ShowAlert: true}, nil
	}
	if h.rouletteSvc == nil {
		return &callback.Response{CallbackMsg: "❌ 轮盘服务未就绪", ShowAlert: true}, nil
	}

	result, err := h.rouletteSvc.Spin(ctx.UserID, "")
	if err != nil {
		return &callback.Response{CallbackMsg: err.Error(), ShowAlert: true}, nil
	}

	card := richmessage.BuildRouletteCard(richmessage.RouletteCardData{
		Title:     result.Title,
		Year:      result.Year,
		Rating:    result.Rating,
		Overview:  result.Overview,
		SpinCount: result.SpinCount,
		MaxSpins:  result.MaxSpins,
	})

	kb := services.NewKeyboardBuilder()
	if result.SpinCount < result.MaxSpins {
		kb.AddButton("🎡 再转一次", "game_roulette_spin")
	}
	kb.AddButton("🎰 开盲盒", "game_blindbox_open")
	kb.NewRow()
	kb.AddButton("🎮 游戏中心", "game_menu")

	return &callback.Response{
		RichMessage: card.Markdown,
		Keyboard:    convertKeyboard(kb.Build()),
	}, nil
}

// --- 工具函数 ---

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
