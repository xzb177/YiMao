package handlers

import (
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/xzb177/yimao/ai"
	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/config"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/internal/ui"
	"github.com/xzb177/yimao/pkg/errors"
	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
)

// Message formatting constants
const (
	MaxOverviewLength = 300
	MaxDisplayCount   = 8
	MaxSeasonsDisplay = 4
)

// extractSeasons extracts season list from MediaInfo
func extractSeasons(info *services.MediaInfo) []session.Season {
	// Try SeasonInfo first (preferred)
	if len(info.SeasonInfo) > 0 {
		seasons := make([]session.Season, len(info.SeasonInfo))
		for i, s := range info.SeasonInfo {
			seasons[i] = session.Season{
				SeasonNumber: s.SeasonNumber,
				EpisodeCount: s.EpisodeCount,
				Name:         s.Name,
			}
		}
		return seasons
	}

	// Try Seasons as map
	if info.Seasons != nil {
		if seasonsMap, ok := info.Seasons.(map[string]interface{}); ok {
			seasons := make([]session.Season, 0, len(seasonsMap))
			for key := range seasonsMap {
				var seasonNum int
				fmt.Sscanf(key, "%d", &seasonNum)
				if seasonNum > 0 {
					seasons = append(seasons, session.Season{
						SeasonNumber: seasonNum,
						EpisodeCount: 0,
						Name:         fmt.Sprintf("第%d季", seasonNum),
					})
				}
			}
			return seasons
		}
	}

	return nil
}

// getPosterURL converts a poster path to a full TMDB image URL
func getPosterURL(poster string) string {
	if poster == "" {
		return ""
	}
	if strings.HasPrefix(poster, "http") {
		return ui.EnsureSafePosterURL(poster)
	}
	if strings.HasPrefix(poster, "/") {
		return ui.EnsureSafePosterURL("https://image.tmdb.org/t/p/w500" + poster)
	}
	return ui.EnsureSafePosterURL("https://image.tmdb.org/t/p/w500/" + poster)
}

// cacheMediaInfo saves media info to session for later use (e.g., resource list)
func cacheMediaInfo(sess *session.Session, tmdbID int, title string, year int) {
	sess.Set(fmt.Sprintf("media_title_%d", tmdbID), title)
	sess.Set(fmt.Sprintf("media_year_%d", tmdbID), year)
}

// buildPlainCaption builds a plain text caption for photo messages (Telegram doesn't support Markdown in photo captions)
func buildPlainCaption(tmdbID int, title, firstAirDate string, voteAverage float64, genres []services.TMDBGenre, seasons, episodes int, overview string, mpNotAvailable bool) string {
	var caption strings.Builder

	// Title
	caption.WriteString(fmt.Sprintf("📺 %s\n\n", title))

	// Year and rating
	if firstAirDate != "" && len(firstAirDate) >= 4 {
		year := firstAirDate[:4]
		caption.WriteString(fmt.Sprintf("📅 %s年", year))
	}
	if voteAverage > 0 {
		if firstAirDate != "" && len(firstAirDate) >= 4 {
			caption.WriteString("  •  ")
		}
		caption.WriteString(fmt.Sprintf("⭐ %.1f分", voteAverage))
	}
	if firstAirDate != "" || voteAverage > 0 {
		caption.WriteString("\n")
	}

	// Genres
	if len(genres) > 0 {
		genreNames := make([]string, len(genres))
		for i, g := range genres {
			genreNames[i] = g.Name
		}
		caption.WriteString(fmt.Sprintf("🎭 %s\n", strings.Join(genreNames, "、")))
	}

	// Seasons and episodes
	if seasons > 0 {
		caption.WriteString(fmt.Sprintf("📺 共 %d 季 · %d 集\n", seasons, episodes))
	}

	caption.WriteString("\n")

	// Overview (truncate if needed, keep under 1024 chars total for photo caption)
	if overview != "" {
		maxOverview := 200 // Reserve space for other content
		if len(overview) > maxOverview {
			overview = overview[:maxOverview] + "..."
		}
		caption.WriteString(fmt.Sprintf("📖 剧情简介\n%s\n\n", overview))
	}

	// TMDB ID
	caption.WriteString(fmt.Sprintf("🆔 TMDB ID: %d", tmdbID))

	// Warning if MoviePilot doesn't have this media
	if mpNotAvailable {
		caption.WriteString("\n\n⚠️ 资源库暂无\n当前资源库中暂无此剧集，求片后将尝试自动搜索")
	}

	result := caption.String()
	// Telegram photo caption limit is 1024 characters
	if len(result) > 1024 {
		result = result[:1020] + "..."
	}

	return result
}

// buildPlainCaptionFromItem builds a plain text caption for photo messages from search item
func buildPlainCaptionFromItem(item session.SearchItem, mpNotAvailable bool) string {
	var caption strings.Builder

	// Type icon and label
	typeIcon := "🎬"
	typeLabel := "电影"
	isTV := item.Type == "tv" || item.Type == "电视剧"
	if isTV {
		typeIcon = "📺"
		typeLabel = "剧集"
	}

	// Title
	caption.WriteString(fmt.Sprintf("%s %s\n\n", typeIcon, item.Title))

	// Info section
	if item.Year > 0 {
		caption.WriteString(fmt.Sprintf("📅 %d年  ", item.Year))
	}
	if item.Rating > 0 {
		caption.WriteString(fmt.Sprintf("⭐ %.1f分  ", item.Rating))
	}
	caption.WriteString(fmt.Sprintf("🏷️ %s\n\n", typeLabel))

	// TV show seasons
	if isTV && len(item.Seasons) > 0 {
		caption.WriteString(fmt.Sprintf("📺 共 %d 季\n", len(item.Seasons)))
		for i, s := range item.Seasons {
			if i >= 3 {
				caption.WriteString(fmt.Sprintf("   ... 还有 %d 季\n", len(item.Seasons)-3))
				break
			}
			seasonName := fmt.Sprintf("第%d季", s.SeasonNumber)
			if s.Name != "" {
				seasonName = s.Name
			}
			caption.WriteString(fmt.Sprintf("   • %s (%d集)\n", seasonName, s.EpisodeCount))
		}
		caption.WriteString("\n")
	}

	// Overview
	if item.Overview != "" {
		maxOverview := 300
		overview := item.Overview
		if len(overview) > maxOverview {
			overview = overview[:maxOverview] + "..."
		}
		caption.WriteString(fmt.Sprintf("📖 剧情简介\n%s\n\n", overview))
	}

	caption.WriteString(fmt.Sprintf("🆔 TMDB ID: %s", item.ID))

	// Warning if MoviePilot doesn't have this media
	if mpNotAvailable {
		caption.WriteString("\n\n⚠️ 资源库暂无\n当前资源库中暂无此影片，求片后将尝试自动搜索")
	}

	result := caption.String()
	// Telegram photo caption limit is 1024 characters
	if len(result) > 1024 {
		result = result[:1020] + "..."
	}

	return result
}

// StartHandler handles start menu callbacks
type StartHandler struct {
	cfg             *config.Config
	sessMgr         *session.Manager
	telegram        *services.TelegramClient
	moviepilot      *services.MoviePilotClient
	adminService    *services.AdminService
	userMapping     *services.UserMappingService
	weeklyReportSvc *services.WeeklyReportService
}

func NewStartHandler(
	cfg *config.Config,
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	moviepilot *services.MoviePilotClient,
) *StartHandler {
	return &StartHandler{
		cfg:        cfg,
		sessMgr:    sessMgr,
		telegram:   telegram,
		moviepilot: moviepilot,
	}
}

// SetAdminService sets the admin service
func (h *StartHandler) SetAdminService(adminSvc *services.AdminService) {
	h.adminService = adminSvc
}

// SetUserMapping sets the user mapping service (设置页显示绑定状态用)
func (h *StartHandler) SetUserMapping(um *services.UserMappingService) {
	h.userMapping = um
}

func (h *StartHandler) SetWeeklyReportService(svc *services.WeeklyReportService) {
	h.weeklyReportSvc = svc
}

func (h *StartHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	action := ctx.Callback.Action

	switch action {
	case callback.ActionStart:
		return h.HandleStart(ctx)
	case callback.ActionSearch:
		return h.HandleSearch(ctx)
	case callback.ActionAI:
		return h.HandleAI(ctx)
	case callback.ActionMood:
		return h.HandleMood(ctx)
	case callback.ActionMoodPick:
		return h.HandleMoodPick(ctx)
	case "ai_chat":
		return h.HandleAIChat(ctx)
	case callback.ActionQuickPick:
		return h.HandleQuickPick(ctx)
	case callback.ActionHot:
		return h.HandleHot(ctx)
	case callback.ActionNew:
		return h.HandleNew(ctx)
	case callback.ActionSettings:
		return h.HandleSettings(ctx)
	case callback.ActionHelpTopic:
		return h.HandleHelpTopic(ctx)
	case "weekly_report":
		return h.HandleWeeklyReport(ctx)
	case "weekly_report_send":
		return h.HandleWeeklyReportSend(ctx)
	default:
		return nil, errors.CallbackInvalid(fmt.Sprintf("unknown start action: %s", action))
	}
}

func (h *StartHandler) HandleStart(ctx *callback.Context) (*callback.Response, error) {
	// 使用 UI 包构建主菜单消息（波普艺术风格）
	baseMsg := ui.BuildMenuWith(ui.StyleCard, "云海影视助手", "你的私人选片师")

	// 添加用户观影人格显示（如果有的话）
	isPrivateChat := ctx.ChatType == "private"
	if isPrivateChat {
		sess := h.sessMgr.GetOrCreate(ctx.UserID)
		// Clear AI chat mode when returning to main menu
		sess.Delete("ai_chat_mode")
		if moodVal, ok := sess.GetString("pref_mood_last"); ok && moodVal != "" {
			moodMap := map[string]string{
				"relax":     "解压轻松",
				"mindblow":  "烧脑刺激",
				"emotional": "情绪共鸣",
				"healing":   "治愈慢节奏",
				"random":    "随机盲选",
			}
			if label, exists := moodMap[moodVal]; exists {
				baseMsg += fmt.Sprintf("\n🧠 观影人格：%s", label)
			}
		}
	}

	// Check if user is admin to add admin menu button
	isAdmin := false
	if h.adminService != nil {
		isAdmin = h.adminService.IsAdmin(ctx.UserID)
	}

	keyboard := services.BuildStartKeyboardWithOptions(isAdmin, isPrivateChat && ai.GetManager().IsEnabled(), true)

	return &callback.Response{
		Text:     baseMsg,
		Edit:     true,
		Keyboard: convertKeyboard(keyboard),
	}, nil
}

func (h *StartHandler) HandleSearch(ctx *callback.Context) (*callback.Response, error) {
	// Clear AI chat mode when entering search mode
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	sess.Delete("ai_chat_mode")

	msg := services.NewMessageBuilder()
	msg.Bold("🔍 搜影片").Newline()
	msg.Newline()
	msg.Text("把片名发给我就行").Newline()
	msg.Newline()
	msg.Text("中英文、电影剧集都能搜").Newline()
	msg.Newline()
	msg.Italic("💡 直接发片名，不用加命令")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: &callback.Keyboard{},
	}, nil
}

func (h *StartHandler) HandleAI(ctx *callback.Context) (*callback.Response, error) {
	// Recommendation is only available in private chats
	if ctx.ChatType != "private" {
		return &callback.Response{
			Text:        "⚠️ 推荐功能仅在私聊中可用",
			CallbackMsg: "请私聊使用",
			ShowAlert:   true,
		}, nil
	}

	// Check if AI is enabled
	if !ai.GetManager().IsEnabled() {
		return &callback.Response{
			Text:        "🤖 AI 功能暂未启用\n\n💡 请联系管理员配置 ZHIPU_API_KEY",
			CallbackMsg: "AI 未启用",
			ShowAlert:   true,
		}, nil
	}

	// 直接进入 AI 聊天模式
	msg := services.NewMessageBuilder()
	msg.Bold("🎬 今晚看什么").Newline()
	msg.Newline()
	msg.Text("告诉我你的口味，帮你挑一部").Newline()
	msg.Newline()
	msg.Text("试试这样：").Newline()
	msg.Text("• 想看一部烧脑悬疑").Newline()
	msg.Text("• 推荐治愈系的").Newline()
	msg.Text("• 最近有什么好看的科幻").Newline()
	msg.Newline()
	msg.Italic("💡 发完会自动回到搜索模式")

	kb := services.NewKeyboardBuilder()
	kb.AddButton("⬅️ 返回主菜单", "start")

	// Set session mode to AI chat
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	sess.Set("ai_chat_mode", true)
	logger.Info("[HandleAI] Enabled AI chat mode for user %d", ctx.UserID)

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// HandleMood provides mood-based recommendation entry
func (h *StartHandler) HandleMood(ctx *callback.Context) (*callback.Response, error) {
	if ctx.ChatType != "private" {
		return &callback.Response{
			Text:        "⚠️ 推荐功能仅在私聊中可用",
			CallbackMsg: "请私聊使用",
			ShowAlert:   true,
		}, nil
	}

	msg := services.NewMessageBuilder()
	msg.Bold("😊 按心情选片").Newline()
	msg.Newline()
	msg.Text("你现在什么状态？").Newline()
	msg.Newline()
	msg.Italic("👇 选一个心情")

	kb := services.NewKeyboardBuilder()
	kb.AddButton("😌 解压轻松", "moodpick:type:hot:mood:relax")
	kb.AddButton("🤯 烧脑刺激", "moodpick:type:toprated:mood:mindblow")
	kb.NewRow()
	kb.AddButton("😭 情绪共鸣", "moodpick:type:trending:mood:emotional")
	kb.AddButton("🧘 治愈慢节奏", "moodpick:type:new:mood:healing")
	kb.NewRow()
	kb.AddButton("⬅️ 返回", "start_ai")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// HandleAIChat provides AI-powered chat recommendation entry
func (h *StartHandler) HandleAIChat(ctx *callback.Context) (*callback.Response, error) {
	if ctx.ChatType != "private" {
		return &callback.Response{
			Text:        "⚠️ AI 推荐功能仅在私聊中可用",
			CallbackMsg: "请私聊使用",
			ShowAlert:   true,
		}, nil
	}

	// Check if AI is enabled
	if !ai.GetManager().IsEnabled() {
		return &callback.Response{
			Text:        "🤖 AI 功能暂未启用\n\n💡 请联系管理员配置 ZHIPU_API_KEY",
			CallbackMsg: "AI 未启用",
			ShowAlert:   true,
		}, nil
	}

	msg := services.NewMessageBuilder()
	msg.Bold("🎬 今晚看什么").Newline()
	msg.Newline()
	msg.Text("告诉我你的口味，帮你挑一部").Newline()
	msg.Newline()
	msg.Text("试试这样：").Newline()
	msg.Text("• 想看一部烧脑悬疑").Newline()
	msg.Text("• 推荐治愈系的").Newline()
	msg.Text("• 最近有什么好看的科幻").Newline()
	msg.Newline()
	msg.Italic("💡 发完会自动回到搜索模式")

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🔥 热门", "search:type:trending")
	kb.AddButton("⭐ 高分", "search:type:toprated")
	kb.NewRow()
	kb.AddButton("😊 按心情", "start_mood")
	kb.AddButton("⬅️ 返回", "start_ai")

	// Set session mode to AI chat
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	sess.Set("ai_chat_mode", true)
	logger.Info("[HandleAIChat] Enabled AI chat mode for user %d", ctx.UserID)

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

func (h *StartHandler) HandleMoodPick(ctx *callback.Context) (*callback.Response, error) {
	if ctx.ChatType != "private" {
		return &callback.Response{
			Text:        "⚠️ 情绪选片仅在私聊中可用",
			CallbackMsg: "请私聊使用",
			ShowAlert:   true,
		}, nil
	}

	typeName := ctx.Callback.Params["type"]
	mood := ctx.Callback.Params["mood"]
	if typeName == "" {
		typeName = "random"
	}
	if mood == "" {
		mood = "random"
	}

	// 记录用户最近一次情绪偏好，用于后续个性化
	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	sess.Set("pref_mood_last", mood)
	sess.Set("pref_mood_last_type", typeName)

	moodLabel := map[string]string{
		"relax":     "解压轻松",
		"mindblow":  "烧脑刺激",
		"emotional": "情绪共鸣",
		"healing":   "治愈慢节奏",
		"random":    "随机盲选",
	}[mood]
	if moodLabel == "" {
		moodLabel = "随机盲选"
	}

	msg := services.NewMessageBuilder()
	msg.Bold("💫 情绪画像已记录").Newline()
	msg.Newline()
	msg.Textf("当前偏好：%s", moodLabel).Newline()
	msg.Italic("接下来会优先给你相近风格内容").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🎬 立即看推荐", fmt.Sprintf("search:type:%s:mood:%s", typeName, mood))
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:        msg.Build(),
		Edit:        true,
		Keyboard:    convertKeyboard(kb.Build()),
		CallbackMsg: "已记住你的口味",
	}, nil
}

func (h *StartHandler) HandleQuickPick(ctx *callback.Context) (*callback.Response, error) {
	if ctx.ChatType != "private" {
		return &callback.Response{
			Text:        "⚠️ 不纠结仅在私聊中可用",
			CallbackMsg: "请私聊使用",
			ShowAlert:   true,
		}, nil
	}

	sess := h.sessMgr.GetOrCreate(ctx.UserID)
	// Note: If mood preference is not set, prefMood will be empty and defaults will be used
	prefMood, _ := sess.GetString("pref_mood_last")

	// 默认心情映射
	steadyMood := "mindblow" // 稳妥高口碑 → 烧脑刺激（高评分）
	stimMood := "excited"    // 刺激高热度 → 兴奋（热门）
	nicheMood := "random"    // 冷门盲盒 → 随机

	// 根据用户心情偏好调整
	if prefMood == "relax" || prefMood == "healing" {
		steadyMood = "healing" // 治愈类用户 → 治愈推荐
		stimMood = "relax"     // 放松推荐
		nicheMood = "cozy"     // 温馨推荐
	} else if prefMood == "mindblow" || prefMood == "excited" {
		steadyMood = "mindblow" // 保持
		stimMood = "excited"
		nicheMood = "random"
	}

	msg := services.NewMessageBuilder()
	msg.Bold("🎯 不纠结 · 三选一").Newline()
	msg.Newline()
	msg.Text("给你三条完全不同的路：").Newline()
	msg.Text("A 稳妥高口碑  B 刺激高热度  C 冷门盲盒").Newline()
	msg.Italic("选一个直接开刷，不浪费时间").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🅰️ 稳妥", fmt.Sprintf("search:type:toprated:mood:%s", steadyMood))
	kb.AddButton("🅱️ 刺激", fmt.Sprintf("search:type:trending:mood:%s", stimMood))
	kb.NewRow()
	kb.AddButton("🅲 冷门盲盒", fmt.Sprintf("search:type:random:mood:%s", nicheMood))
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

func (h *StartHandler) HandleHot(ctx *callback.Context) (*callback.Response, error) {
	return &callback.Response{
		Text:        "📺 加载中...",
		CallbackMsg: "加载中",
		ShowAlert:   true,
	}, nil
}

func (h *StartHandler) HandleNew(ctx *callback.Context) (*callback.Response, error) {
	return &callback.Response{
		Text:        "🆕 加载中...",
		CallbackMsg: "加载中",
		ShowAlert:   true,
	}, nil
}

// HandleSettings shows the settings page
func (h *StartHandler) HandleSettings(ctx *callback.Context) (*callback.Response, error) {
	msg := services.NewMessageBuilder()
	msg.Bold("⚙️ 设置").Newline()
	msg.Newline()

	// 显示绑定状态
	if h.userMapping != nil {
		if _, exists := h.userMapping.GetMoviePilotUserID(ctx.UserID); exists {
			msg.Text("🔗 账号状态：已绑定 ✅").Newline()
		} else {
			msg.Text("🔗 账号状态：未绑定").Newline()
		}
	}

	msg.Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🔔 通知设置", "notify_settings")
	kb.AddButton("🔗 绑定账号", "start_link")
	kb.NewRow()
	kb.AddButton("🐞 我的反馈", "my_feedback")
	kb.AddButton("📊 观影周报", "weekly_report")
	kb.NewRow()
	kb.AddButton("❓ 帮助", "help")
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// HandleHelpTopic shows help topic details
func (h *StartHandler) HandleHelpTopic(ctx *callback.Context) (*callback.Response, error) {
	topic := ctx.Callback.Params["topic"]

	msg := services.NewMessageBuilder()

	switch topic {
	case "search":
		msg.Bold("🔍 怎么搜片").Newline()
		msg.Newline()
		msg.Text("1. 点「搜影片」").Newline()
		msg.Text("2. 直接发片名给我").Newline()
		msg.Text("3. 点结果里的数字看详情").Newline()
		msg.Text("4. 点「求片」或「订阅」").Newline()
		msg.Newline()
		msg.Italic("就这么简单 💡")

	case "link":
		msg.Bold("🔗 怎么绑定").Newline()
		msg.Newline()
		msg.Text("1. 点「绑定账号」").Newline()
		msg.Text("2. 输入你的 MoviePilot 用户名和密码").Newline()
		msg.Text("3. 绑定成功后可以查看请求进度").Newline()
		msg.Newline()
		msg.Italic("不绑定也能搜片和求片 👌")

	case "failed":
		msg.Bold("❌ 请求失败").Newline()
		msg.Newline()
		msg.Text("可能的原因：").Newline()
		msg.Text("• 资源确实没有种子").Newline()
		msg.Text("• 站点没有这个资源").Newline()
		msg.Text("• 搜索规则需要调整").Newline()
		msg.Newline()
		msg.Text("建议：").Newline()
		msg.Text("• 点「🔄 重新搜索」再试").Newline()
		msg.Text("• 或者联系管理员调整规则").Newline()

	case "notify":
		msg.Bold("🔔 没收到通知").Newline()
		msg.Newline()
		msg.Text("可能的原因：").Newline()
		msg.Text("• Telegram 没有给机器人发消息权限").Newline()
		msg.Text("• 绑定的账号和通知接收账号不一致").Newline()
		msg.Newline()
		msg.Text("解决方法：").Newline()
		msg.Text("• 私聊我发条消息，开启通知权限").Newline()
		msg.Text("• 确保在正确的 Telegram 账号下绑定").Newline()

	default:
		msg.Bold("📮 其他问题").Newline()
		msg.Newline()
		msg.Text("遇到其他问题了？").Newline()
		msg.Newline()
		msg.Text("请通过「我的反馈」功能提交问题，").Newline()
		msg.Text("管理员会尽快处理。")
	}

	kb := services.NewKeyboardBuilder()
	kb.AddButton("⬅️ 返回帮助", "help")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// HandleWeeklyReport shows the weekly report
func (h *StartHandler) HandleWeeklyReport(ctx *callback.Context) (*callback.Response, error) {
	if h.weeklyReportSvc == nil {
		return &callback.Response{
			Text:        "📊 周报服务未就绪，请稍后再试",
			CallbackMsg: "服务未就绪",
			ShowAlert:   true,
		}, nil
	}

	// 获取用户名
	userName := ctx.Callback.Params["name"]
	if userName == "" {
		userName = "影迷"
	}

	report, err := h.weeklyReportSvc.GenerateReport(ctx.UserID, userName)
	if err != nil {
		return &callback.Response{
			Text:        "📊 周报生成失败，请稍后再试",
			CallbackMsg: "生成失败",
			ShowAlert:   true,
		}, nil
	}

	reportText := h.weeklyReportSvc.FormatReport(report)

	kb := services.NewKeyboardBuilder()
	kb.AddButton("📬 推送到私聊", "weekly_report_send")
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:      reportText,
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
		ParseMode: "HTML",
	}, nil
}

// HandleWeeklyReportSend sends the weekly report to user's DM
func (h *StartHandler) HandleWeeklyReportSend(ctx *callback.Context) (*callback.Response, error) {
	if h.weeklyReportSvc == nil {
		return &callback.Response{CallbackMsg: "服务未就绪", ShowAlert: true}, nil
	}

	userName := "影迷"
	report, err := h.weeklyReportSvc.GenerateReport(ctx.UserID, userName)
	if err != nil {
		return &callback.Response{CallbackMsg: "📊 报告生成失败", ShowAlert: true}, nil
	}

	reportText := h.weeklyReportSvc.FormatReport(report)
	h.telegram.SendMessage(ctx.UserID, reportText, "HTML", nil)

	return &callback.Response{
		CallbackMsg: "📬 报告已推送到您的私聊！",
		ShowAlert:   true,
	}, nil
}

// DetailHandler handles media detail callbacks
type DetailHandler struct {
	sessMgr    *session.Manager
	telegram   *services.TelegramClient
	moviepilot *services.MoviePilotClient
	tmdb       *services.TMDBClient
}

func NewDetailHandler(
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	moviepilot *services.MoviePilotClient,
	tmdb *services.TMDBClient,
) *DetailHandler {
	return &DetailHandler{
		sessMgr:    sessMgr,
		telegram:   telegram,
		moviepilot: moviepilot,
		tmdb:       tmdb,
	}
}

func (h *DetailHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	// Check if this is a detail_seasons action
	if ctx.Callback.Action == callback.ActionDetailSeasons {
		return h.HandleSeasons(ctx)
	}

	// Get media ID and type from params
	mediaID, hasID := ctx.Callback.Params["id"]
	mediaType, hasType := ctx.Callback.Params["type"]

	if !hasID || !hasType {
		return nil, errors.InvalidInput("media ID and type are required")
	}

	// Parse TMDB ID
	tmdbID := 0
	fmt.Sscanf(mediaID, "%d", &tmdbID)
	if tmdbID == 0 {
		return nil, errors.InvalidInput("invalid media ID")
	}

	sess := h.sessMgr.GetOrCreate(ctx.UserID)

	// Check if we have cached data from search results first
	items, _, query, hasSearch := sess.GetSearchResults()
	if hasSearch {
		for _, item := range items {
			if item.ID == mediaID {
				// Push navigation entry before showing detail
				if isAIRecommendationQuery(query) {
					sess.PushNavEntry("ai_recommendation", query, query)
				} else {
					// For regular search, also record navigation history
					sess.PushNavEntry("search", query, query)
				}
				// Use search result data - it already has all we need
				logger.Info("[DetailHandler] Using search result info for: %s", item.Title)
				return h.buildDetailFromSearch(item, mediaType, sess), nil
			}
		}
	}

	// Check if we have cached data from AI
	if cachedItem := sess.GetCachedAIItem(tmdbID); cachedItem != nil {
		return h.buildDetailFromCache(cachedItem, sess), nil
	}

	// Try TMDB API for details
	if h.tmdb != nil {
		tmdbMedia, err := h.tmdb.GetMediaByType(tmdbID, mediaType)
		if err == nil && tmdbMedia != nil {
			logger.Info("[DetailHandler] Got media info from TMDB: %s", tmdbMedia.GetTitle())
			return h.buildDetailFromTMDB(tmdbMedia, sess), nil
		}
		logger.Info("[DetailHandler] TMDB API failed: %v", err)
	}

	// If all else fails, build a simple detail page
	return h.buildSimpleDetail(tmdbID, mediaType, sess), nil
}

// isAIRecommendationQuery checks if the query is from AI recommendation
func isAIRecommendationQuery(query string) bool {
	aiTypes := []string{"trending", "hot", "new", "toprated", "random"}
	for _, t := range aiTypes {
		if query == t {
			return true
		}
	}
	return false
}

func (h *DetailHandler) buildDetailFromCache(item *session.AIRecommendationItem, sess *session.Session) *callback.Response {
	safeTitle := html.EscapeString(item.Title)
	safeOverview := html.EscapeString(item.Overview)
	var sb strings.Builder

	typeIcon := "🎬"
	if item.MediaType == "tv" {
		typeIcon = "📺"
	}
	header := fmt.Sprintf("%s <b>%s</b>", typeIcon, safeTitle)
	if item.Year > 0 {
		header += fmt.Sprintf(" (%d)", item.Year)
	}
	sb.WriteString(header + "\n\n")

	if item.Rating > 0 {
		sb.WriteString(fmt.Sprintf("╭── 🍿 <b>影片信息</b> ────────────────╮\n"))
		sb.WriteString(fmt.Sprintf("│  ⭐️ 评分: <b>%.1f</b>/10\n", item.Rating))
		sb.WriteString("╰──────────────────────────────────────╯\n\n")
	}

	if safeOverview != "" {
		sb.WriteString("📖 <b>剧情简介</b>\n")
		if len([]rune(safeOverview)) > 200 {
			safeOverview = string([]rune(safeOverview)[:200]) + "..."
		}
		sb.WriteString(fmt.Sprintf("<blockquote>%s</blockquote>\n\n", safeOverview))
	}

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🎬 立即求片", fmt.Sprintf("request:id:%d:type:%s", item.TmdbID, item.MediaType))
	kb.AddButton("⬅️ 返回", "back")

	return &callback.Response{
		Text:      sb.String(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
		ParseMode: "HTML",
	}
}

func (h *DetailHandler) buildDetailFromTMDB(media *services.TMDBMediaInfo, sess *session.Session) *callback.Response {
	// Cache media info for resource list
	cacheMediaInfo(sess, media.ID, media.GetTitle(), media.GetYear())

	// For TV shows, fetch full details with seasons
	if media.MediaType == "tv" && h.tmdb != nil {
		posterURL := getPosterURL(media.PosterPath)
		return h.buildDetailFromTMDBTV(media.ID, media.GetTitle(), sess, false, posterURL)
	}

	safeTitle := html.EscapeString(media.GetTitle())
	safeOrgTitle := html.EscapeString(media.OriginalTitle)

	var sb strings.Builder

	// 1. Header
	year := media.GetYear()
	header := fmt.Sprintf("🎬 <b>%s</b>", safeTitle)
	if year > 0 {
		header += fmt.Sprintf(" (%d)", year)
	}
	sb.WriteString(header + "\n")
	if safeOrgTitle != "" && safeOrgTitle != safeTitle {
		sb.WriteString(fmt.Sprintf("<i>%s</i>\n", safeOrgTitle))
	}
	sb.WriteString("\n")

	// 2. Spec dashboard
	sb.WriteString("╭── 🍿 <b>影片信息</b> ────────────────╮\n")
	if media.VoteAverage > 0 {
		sb.WriteString(fmt.Sprintf("│  ⭐️ 评分: <b>%.1f</b>/10", media.VoteAverage))
		if media.VoteCount > 0 {
			sb.WriteString(fmt.Sprintf(" (%d票)", media.VoteCount))
		}
		sb.WriteString("\n")
	}
	genres := media.GetGenres()
	if genres != "" {
		sb.WriteString(fmt.Sprintf("│  🏷️ 类型: <code>%s</code>\n", html.EscapeString(genres)))
	}
	runtime := media.GetRuntime()
	if runtime > 0 {
		hours := runtime / 60
		mins := runtime % 60
		if hours > 0 {
			sb.WriteString(fmt.Sprintf("│  ⏱️ 时长: <code>%d小时%d分</code>\n", hours, mins))
		} else {
			sb.WriteString(fmt.Sprintf("│  ⏱️ 时长: <code>%d分钟</code>\n", runtime))
		}
	}
	sb.WriteString("╰──────────────────────────────────────╯\n\n")

	// 3. Overview
	if media.Overview != "" {
		sb.WriteString("📖 <b>剧情简介</b>\n")
		overview := html.EscapeString(media.Overview)
		if len([]rune(overview)) > 200 {
			overview = string([]rune(overview)[:200]) + "..."
		}
		sb.WriteString(fmt.Sprintf("<blockquote>%s</blockquote>\n\n", overview))
	}

	// 4. Keyboard
	kb := services.NewKeyboardBuilder()
	kb.AddButton("🎬 立即求片", fmt.Sprintf("request:id:%d:type:movie", media.ID))
	kb.AddButton("🙋 我也想看 +1", fmt.Sprintf("carpool:id:%d:type:movie", media.ID))
	kb.NewRow()
	kb.AddButton("🔍 候选列表", callback.BuildCallback(callback.ActionResourceList, map[string]string{"id": fmt.Sprintf("%d", media.ID), "type": "movie"}))
	kb.AddButton("🐛 反馈", fmt.Sprintf("feedback:id:%d:type:movie:title:%s", media.ID, media.Title))
	kb.NewRow()
	kb.AddButton("⬅️ 返回", "back")

	return &callback.Response{
		Text:      sb.String(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
		ParseMode: "HTML",
	}
}

// buildDetailFromTMDBTV builds detail page for TV show from TMDB with season info
func (h *DetailHandler) buildDetailFromTMDBTV(tmdbID int, title string, sess *session.Session, mpNotAvailable bool, posterURL string) *callback.Response {
	// Fetch TV details with seasons from TMDB
	tvDetails, err := h.tmdb.GetTVDetailsWithSeasons(tmdbID)
	if err != nil {
		logger.Info("[DetailHandler] Failed to get TV details from TMDB: %v", err)
		return h.buildSimpleTVDetail(tmdbID, title, sess, mpNotAvailable, posterURL)
	}

	// Cache media info for resource list
	year := 0
	if tvDetails.FirstAirDate != "" && len(tvDetails.FirstAirDate) >= 4 {
		fmt.Sscanf(tvDetails.FirstAirDate[:4], "%d", &year)
	}
	cacheMediaInfo(sess, tmdbID, title, year)

	safeTitle := html.EscapeString(title)
	var sb strings.Builder

	// 1. Header
	header := fmt.Sprintf("📺 <b>%s</b>", safeTitle)
	if year > 0 {
		header += fmt.Sprintf(" (%d)", year)
	}
	sb.WriteString(header + "\n\n")

	// 2. Spec dashboard
	sb.WriteString("╭── 🍿 <b>剧集信息</b> ────────────────╮\n")
	if tvDetails.VoteAverage > 0 {
		sb.WriteString(fmt.Sprintf("│  ⭐️ 评分: <b>%.1f</b>/10", tvDetails.VoteAverage))
		if tvDetails.VoteCount > 0 {
			sb.WriteString(fmt.Sprintf(" (%d票)", tvDetails.VoteCount))
		}
		sb.WriteString("\n")
	}
	if len(tvDetails.Genres) > 0 {
		genreNames := make([]string, 0)
		for _, g := range tvDetails.Genres {
			genreNames = append(genreNames, g.Name)
		}
		sb.WriteString(fmt.Sprintf("│  🏷️ 类型: <code>%s</code>\n", html.EscapeString(strings.Join(genreNames, "/"))))
	}
	regularSeasonCount := 0
	for _, s := range tvDetails.Seasons {
		if s.SeasonNumber > 0 {
			regularSeasonCount++
		}
	}
	if regularSeasonCount > 0 {
		sb.WriteString(fmt.Sprintf("│  📺 共 <b>%d 季</b> · %d 集\n", regularSeasonCount, tvDetails.NumberOfEpisodes))
	}
	sb.WriteString("╰──────────────────────────────────────╯\n\n")

	// 3. Overview
	if tvDetails.Overview != "" {
		sb.WriteString("📖 <b>剧情简介</b>\n")
		overview := html.EscapeString(tvDetails.Overview)
		if len([]rune(overview)) > 200 {
			overview = string([]rune(overview)[:200]) + "..."
		}
		sb.WriteString(fmt.Sprintf("<blockquote>%s</blockquote>\n\n", overview))
	}

	// 4. Warning if MP doesn't have this
	if mpNotAvailable {
		sb.WriteString("⚠️ <b>资源库暂无</b>，求片后将尝试自动搜索\n\n")
	}

	// 5. Season grid (buttons)
	kb := services.NewKeyboardBuilder()
	requestButtonText := "✅ 订阅全季"
	if mpNotAvailable {
		requestButtonText = "🔄 尝试求片"
	}
	kb.AddButton(requestButtonText, fmt.Sprintf("request:id:%d:type:tv:season:0", tvDetails.ID))
	kb.AddButton("🙋 我也想看 +1", fmt.Sprintf("carpool:id:%d:type:tv", tvDetails.ID))
	kb.NewRow()
	kb.AddButton("🔍 候选列表", callback.BuildCallback(callback.ActionResourceList, map[string]string{"id": fmt.Sprintf("%d", tvDetails.ID), "type": "tv"}))
	kb.AddButton("🐛 反馈", fmt.Sprintf("feedback:id:%d:type:tv", tvDetails.ID))
	kb.NewRow()
	kb.AddButton("⬅️ 返回", "back")
	if len(tvDetails.Seasons) > 9 {
		kb.AddButton(fmt.Sprintf("📺 全部 %d 季", regularSeasonCount), fmt.Sprintf("detail_seasons:id:%d", tvDetails.ID))
	}
	kb.NewRow()
	displayCount := len(tvDetails.Seasons)
	if displayCount > 9 {
		displayCount = 9
	}
	for i, s := range tvDetails.Seasons {
		if i >= displayCount {
			break
		}
		seasonName := fmt.Sprintf("S%d", s.SeasonNumber)
		if s.SeasonNumber == 0 {
			seasonName = "特别篇"
		}
		kb.AddButton(seasonName, fmt.Sprintf("request:id:%d:type:tv:season:%d", tvDetails.ID, s.SeasonNumber))
		if (i+1)%3 == 0 {
			kb.NewRow()
		}
	}

	if posterURL != "" {
		return &callback.Response{
			Photo:        posterURL,
			PhotoCaption: sb.String(),
			Edit:         false,
			Keyboard:     convertKeyboard(kb.Build()),
			ParseMode:    "HTML",
		}
	}

	return &callback.Response{
		Text:      sb.String(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
		ParseMode: "HTML",
	}
}

// buildSimpleTVDetail builds a simple TV detail page (fallback)
func (h *DetailHandler) buildSimpleTVDetail(tmdbID int, title string, sess *session.Session, mpNotAvailable bool, posterURL string) *callback.Response {
	safeTitle := html.EscapeString(title)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📺 <b>%s</b>\n\n", safeTitle))
	if mpNotAvailable {
		sb.WriteString("⚠️ <b>资源库暂无</b>，求片后将尝试自动搜索\n")
	} else {
		sb.WriteString("暂无法获取季数信息，请尝试直接订阅全季\n")
	}

	kb := services.NewKeyboardBuilder()
	requestButtonText := "✅ 订阅全季"
	if mpNotAvailable {
		requestButtonText = "🔄 尝试求片"
	}
	kb.AddButton(requestButtonText, fmt.Sprintf("request:id:%d:type:tv:season:0", tmdbID))
	kb.AddButton("🙋 我也想看 +1", fmt.Sprintf("carpool:id:%d:type:tv", tmdbID))
	kb.NewRow()
	kb.AddButton("⬅️ 返回", "back")

	if posterURL != "" {
		return &callback.Response{
			Photo:        posterURL,
			PhotoCaption: sb.String(),
			Edit:         false,
			Keyboard:     convertKeyboard(kb.Build()),
			ParseMode:    "HTML",
		}
	}
	return &callback.Response{
		Text:      sb.String(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
		ParseMode: "HTML",
	}
}

func (h *DetailHandler) buildDetailFromMedia(media *services.MediaInfo, sess *session.Session) *callback.Response {
	cacheMediaInfo(sess, media.ID, media.Title, media.Year.Int())

	isTV := media.Type == services.MediaTypeTV
	safeTitle := html.EscapeString(media.Title)
	var sb strings.Builder

	typeIcon := "🎬"
	if isTV {
		typeIcon = "📺"
	}
	header := fmt.Sprintf("%s <b>%s</b>", typeIcon, safeTitle)
	if media.Year > 0 {
		header += fmt.Sprintf(" (%d)", media.Year.Int())
	}
	sb.WriteString(header + "\n\n")

	sb.WriteString("╭── 🍿 <b>影片信息</b> ────────────────╮\n")
	if media.Rating > 0 {
		sb.WriteString(fmt.Sprintf("│  ⭐️ 评分: <b>%.1f</b>/10\n", media.Rating))
	}
	if len(media.Genres) > 0 {
		sb.WriteString(fmt.Sprintf("│  🏷️ 类型: <code>%s</code>\n", html.EscapeString(strings.Join(media.Genres, "/"))))
	}
	sb.WriteString("╰──────────────────────────────────────╯\n\n")

	if media.Overview != "" {
		sb.WriteString("📖 <b>剧情简介</b>\n")
		overview := html.EscapeString(media.Overview)
		if len([]rune(overview)) > 200 {
			overview = string([]rune(overview)[:200]) + "..."
		}
		sb.WriteString(fmt.Sprintf("<blockquote>%s</blockquote>\n\n", overview))
	}

	kb := services.NewKeyboardBuilder()
	if isTV {
		kb.AddButton("✅ 订阅全季", fmt.Sprintf("request:id:%d:type:tv:season:0", media.ID))
	} else {
		kb.AddButton("🎬 立即求片", fmt.Sprintf("request:id:%d:type:movie", media.ID))
	}
	kb.AddButton("⬅️ 返回", "back")

	return &callback.Response{
		Text:      sb.String(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
		ParseMode: "HTML",
	}
}

// buildDetailFromSearch builds detail page from search result item
func (h *DetailHandler) buildDetailFromSearch(item session.SearchItem, mediaType string, sess *session.Session) *callback.Response {
	// Try to get full media info from MoviePilot first
	// Note: If item.ID is not a valid integer, mediaID will be 0 and the check below will skip it
	mediaID, _ := strconv.Atoi(item.ID)
	if mediaID > 0 && h.moviepilot != nil {
		// Determine media type
		mpType := services.MediaTypeMovie
		isTV := mediaType == "tv" || item.Type == "tv" || item.Type == "电视剧"
		if isTV {
			mpType = services.MediaTypeTV
		}

		mediaInfo, err := h.moviepilot.GetMediaInfo(mediaID, mpType)
		if err == nil && mediaInfo != nil {
			// Get the query from session to determine back button behavior
			// Note: If GetSearchResults fails, query will be empty string (acceptable for back button)
			_, _, query, _ := sess.GetSearchResults()
			return h.buildDetailFromMediaInfo(mediaInfo, sess, query)
		}
		logger.Info("[DetailHandler] Failed to get media info from MoviePilot: %v", err)
		// Don't mark as unavailable - the search found this media, so it exists
		// The error might be due to type mismatch or API issue, but the media is in the system
		logger.Info("[DetailHandler] Media found in search but GetMediaInfo failed, treating as available (error: %v)", err)

		// Get poster URL from search item
		posterURL := getPosterURL(item.Poster)

		// For TV shows, fallback to TMDB for season info
		if isTV && h.tmdb != nil {
			logger.Info("[DetailHandler] Falling back to TMDB for TV show seasons: %s", item.Title)
			return h.buildDetailFromTMDBTV(mediaID, item.Title, sess, false, posterURL) // Treat as available
		}
	}

	// Fallback to basic detail view from session data
	// Note: If GetSearchResults fails, query will be empty string (acceptable for back button)
	_, _, query, _ := sess.GetSearchResults()

	// Cache media info for resource list before building basic detail
	tmdbID := 0
	fmt.Sscanf(item.ID, "%d", &tmdbID)
	if tmdbID > 0 {
		cacheMediaInfo(sess, tmdbID, item.Title, item.Year)
	}

	return h.buildBasicDetailFromSearch(item, mediaType, query, false) // Treat as available
}

// buildDetailFromMediaInfo builds rich detail page from MoviePilot media info
func (h *DetailHandler) buildDetailFromMediaInfo(info *services.MediaInfo, sess *session.Session, query string) *callback.Response {
	cacheMediaInfo(sess, info.ID, info.Title, info.Year.Int())

	isTV := info.Type == services.MediaTypeTV
	safeTitle := html.EscapeString(info.Title)
	var sb strings.Builder

	typeIcon := "🎬"
	if isTV {
		typeIcon = "📺"
	}
	header := fmt.Sprintf("%s <b>%s</b>", typeIcon, safeTitle)
	if info.Year > 0 {
		header += fmt.Sprintf(" (%d)", info.Year.Int())
	}
	sb.WriteString(header + "\n\n")

	// Spec dashboard
	sb.WriteString("╭── 🍿 <b>影片信息</b> ────────────────╮\n")
	if info.Rating > 0 {
		sb.WriteString(fmt.Sprintf("│  ⭐️ 评分: <b>%.1f</b>/10\n", info.Rating))
	}
	if len(info.Genres) > 0 {
		sb.WriteString(fmt.Sprintf("│  🏷️ 类型: <code>%s</code>\n", html.EscapeString(strings.Join(info.Genres, "/"))))
	}
	sb.WriteString("╰──────────────────────────────────────╯\n\n")

	// TV seasons
	if isTV && h.tmdb != nil && info.ID > 0 {
		tmdbDetails, err := h.tmdb.GetTVDetailsWithSeasons(info.ID)
		if err == nil && len(tmdbDetails.Seasons) > 0 {
			regularSeasonCount := 0
			for _, s := range tmdbDetails.Seasons {
				if s.SeasonNumber > 0 {
					regularSeasonCount++
				}
			}
			if regularSeasonCount > 0 {
				sb.WriteString(fmt.Sprintf("📺 共 <b>%d 季</b> · %d 集\n\n", regularSeasonCount, tmdbDetails.NumberOfEpisodes))
			}
		}
	}

	// Overview
	if info.Overview != "" {
		sb.WriteString("📖 <b>剧情简介</b>\n")
		overview := html.EscapeString(info.Overview)
		if len([]rune(overview)) > 200 {
			overview = string([]rune(overview)[:200]) + "..."
		}
		sb.WriteString(fmt.Sprintf("<blockquote>%s</blockquote>\n\n", overview))
	}

	// Keyboard
	kb := services.NewKeyboardBuilder()
	if isTV {
		kb.AddButton("✅ 订阅全季", fmt.Sprintf("request:id:%d:type:tv:season:0", info.ID))
		kb.AddButton("🙋 我也想看 +1", fmt.Sprintf("carpool:id:%d:type:tv", info.ID))
		kb.NewRow()
		if h.tmdb != nil {
			tmdbDetails, err := h.tmdb.GetTVDetailsWithSeasons(info.ID)
			if err == nil && len(tmdbDetails.Seasons) > 0 {
				displayCount := len(tmdbDetails.Seasons)
				if displayCount > 9 {
					displayCount = 9
				}
				for i, s := range tmdbDetails.Seasons {
					if i >= displayCount {
						break
					}
					seasonName := fmt.Sprintf("S%d", s.SeasonNumber)
					if s.SeasonNumber == 0 {
						seasonName = "特别篇"
					}
					kb.AddButton(seasonName, fmt.Sprintf("request:id:%d:type:tv:season:%d", info.ID, s.SeasonNumber))
					if (i+1)%3 == 0 {
						kb.NewRow()
					}
				}
			}
		}
	} else {
		kb.AddButton("🎬 立即求片", fmt.Sprintf("request:id:%d:type:movie", info.ID))
		kb.AddButton("🙋 我也想看 +1", fmt.Sprintf("carpool:id:%d:type:movie", info.ID))
	}
	kb.NewRow()
	kb.AddButton("🔍 候选列表", callback.BuildCallback(callback.ActionResourceList, map[string]string{"id": fmt.Sprintf("%d", info.ID), "type": string(info.Type)}))
	kb.AddButton("⬅️ 返回", "back")

	return &callback.Response{
		Text:      sb.String(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
		ParseMode: "HTML",
	}
}

// buildBasicDetailFromSearch builds basic detail page when MoviePilot API fails
func (h *DetailHandler) buildBasicDetailFromSearch(item session.SearchItem, mediaType string, query string, mpNotAvailable bool) *callback.Response {
	safeTitle := html.EscapeString(item.Title)
	var sb strings.Builder

	typeIcon := "🎬"
	if mediaType == "tv" || item.Type == "tv" {
		typeIcon = "📺"
	}
	header := fmt.Sprintf("%s <b>%s</b>", typeIcon, safeTitle)
	if item.Year > 0 {
		header += fmt.Sprintf(" (%d)", item.Year)
	}
	sb.WriteString(header + "\n\n")

	if item.Overview != "" {
		sb.WriteString("📖 <b>剧情简介</b>\n")
		overview := html.EscapeString(item.Overview)
		if len([]rune(overview)) > 200 {
			overview = string([]rune(overview)[:200]) + "..."
		}
		sb.WriteString(fmt.Sprintf("<blockquote>%s</blockquote>\n\n", overview))
	}

	if mpNotAvailable {
		sb.WriteString("⚠️ <b>资源库暂无</b>，求片后将尝试自动搜索\n\n")
	}

	tmdbID, _ := strconv.Atoi(item.ID)
	kb := services.NewKeyboardBuilder()
	kb.AddButton("🎬 立即求片", fmt.Sprintf("request:id:%d:type:%s", tmdbID, mediaType))
	kb.AddButton("⬅️ 返回", "back")

	return &callback.Response{
		Text:      sb.String(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
		ParseMode: "HTML",
	}
}

// buildSimpleDetail builds a simple detail page when API fails
func (h *DetailHandler) buildSimpleDetail(tmdbID int, mediaType string, sess *session.Session) *callback.Response {
	var sb strings.Builder
	header := "🎬"
	if mediaType == "tv" {
		header = "📺"
	}
	sb.WriteString(fmt.Sprintf("%s <b>影片详情</b>\n\n", header))
	sb.WriteString("暂无法获取详细信息，请尝试直接求片\n")

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🎬 立即求片", fmt.Sprintf("request:id:%d:type:%s", tmdbID, mediaType))
	kb.AddButton("⬅️ 返回", "back")

	return &callback.Response{
		Text:      sb.String(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
		ParseMode: "HTML",
	}
}

// HandleSeasons handles the detail_seasons action - shows all seasons for a TV show
func (h *DetailHandler) HandleSeasons(ctx *callback.Context) (*callback.Response, error) {
	// Get media ID from params
	mediaID, hasID := ctx.Callback.Params["id"]
	if !hasID {
		return &callback.Response{
			Text:        "❌ 请求无效",
			CallbackMsg: "参数错误",
			ShowAlert:   true,
		}, nil
	}

	sess := h.sessMgr.GetOrCreate(ctx.UserID)

	// Try to find the item in search results
	items, _, _, hasSearch := sess.GetSearchResults()
	if !hasSearch {
		return &callback.Response{
			Text:        "⏰ 搜索结果已过期，请重新搜索",
			CallbackMsg: "结果已过期",
			ShowAlert:   true,
		}, nil
	}

	var targetItem *session.SearchItem
	for i := range items {
		if items[i].ID == mediaID {
			targetItem = &items[i]
			break
		}
	}

	if targetItem == nil {
		return &callback.Response{
			Text:        "⏰ 未找到该媒体信息",
			CallbackMsg: "未找到",
			ShowAlert:   true,
		}, nil
	}

	// Check if it's a TV show with seasons
	if targetItem.Type != "tv" || len(targetItem.Seasons) == 0 {
		return &callback.Response{
			Text:        "⏰ 该媒体没有季信息",
			CallbackMsg: "无季信息",
			ShowAlert:   true,
		}, nil
	}

	// Build seasons list page
	msg := services.NewMessageBuilder()
	msg.Bold(fmt.Sprintf("📺 %s - 全部季", targetItem.Title)).Newline()
	msg.Newline()
	msg.Text(fmt.Sprintf("共 %d 季", len(targetItem.Seasons))).Newline()
	msg.Newline()

	kb := services.NewKeyboardBuilder()

	// List all seasons with buttons
	for i, season := range targetItem.Seasons {
		seasonName := fmt.Sprintf("第%d季", season.SeasonNumber)
		if season.SeasonNumber == 0 {
			seasonName = "特别篇"
		}
		if season.Name != "" && season.Name != seasonName {
			seasonName = fmt.Sprintf("%s - %s", seasonName, season.Name)
		}

		msg.Text(fmt.Sprintf("%d. %s (%d集)", i+1, seasonName, season.EpisodeCount)).Newline()

		// Add button for each season
		buttonLabel := fmt.Sprintf("📺 S%d", season.SeasonNumber)
		if season.SeasonNumber == 0 {
			buttonLabel = "📺 特别篇"
		}
		kb.AddButton(buttonLabel, fmt.Sprintf("request:id:%s:type:tv:season:%d", targetItem.ID, season.SeasonNumber))

		// Two buttons per row
		if (i+1)%2 == 0 {
			kb.NewRow()
		}
	}

	if len(targetItem.Seasons)%2 != 0 {
		kb.NewRow()
	}

	// Action row: subscribe + feedback
	kb.AddButton("✅ 订阅全季", fmt.Sprintf("request:id:%s:type:tv:season:0", targetItem.ID))
	kb.AddButton("🐛 反馈", fmt.Sprintf("feedback:id:%s:type:tv:title:%s", targetItem.ID, targetItem.Title))
	kb.NewRow()

	// Resource list button
	kb.AddButton("🔍 候选列表", callback.BuildCallback(callback.ActionResourceList, map[string]string{"id": targetItem.ID, "type": "tv"}))
	kb.NewRow()

	// Navigation row
	kb.AddButton("⬅️ 返回详情", fmt.Sprintf("detail:id:%s:type:tv", targetItem.ID))
	kb.AddButton("🏠 返回主菜单", "start")

	return &callback.Response{
		Text:      msg.Build(),
		Edit:      true,
		Keyboard:  convertKeyboard(kb.Build()),
		ParseMode: "HTML",
	}, nil
}

// BackHandler handles back navigation
type BackHandler struct {
	sessMgr      *session.Manager
	adminService *services.AdminService
}

func NewBackHandler(sessMgr *session.Manager) *BackHandler {
	return &BackHandler{sessMgr: sessMgr}
}

// SetAdminService sets the admin service
func (h *BackHandler) SetAdminService(adminSvc *services.AdminService) {
	h.adminService = adminSvc
}

func (h *BackHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	sess := h.sessMgr.GetOrCreate(ctx.UserID)

	entry, hasHistory := sess.PopNavEntry()
	if !hasHistory {
		// No history, show start menu using UI package
		baseMsg := ui.BuildMenuWith(ui.StyleCard, "云海影视助手", "你的私人选片师")

		// 添加用户观影人格显示（如果有的话）
		isPrivateChat := ctx.ChatType == "private"
		if isPrivateChat {
			if moodVal, ok := sess.GetString("pref_mood_last"); ok && moodVal != "" {
				moodMap := map[string]string{
					"relax":     "解压轻松",
					"mindblow":  "烧脑刺激",
					"emotional": "情绪共鸣",
					"healing":   "治愈慢节奏",
					"random":    "随机盲选",
				}
				if label, exists := moodMap[moodVal]; exists {
					baseMsg += fmt.Sprintf("\n🧠 观影人格：%s", label)
				}
			}
		}

		isAdmin := h.adminService != nil && h.adminService.IsAdmin(ctx.UserID)

		return &callback.Response{
			Text:     baseMsg,
			Edit:     true,
			Keyboard: convertKeyboard(services.BuildStartKeyboardWithOptions(isAdmin, isPrivateChat && ai.GetManager().IsEnabled(), true)),
		}, nil
	}

	// Restore previous view
	logger.Info("[BackHandler] Restoring view: source=%s, query=%s", entry.Source, entry.Query)

	// Based on source, restore appropriate view
	switch entry.Source {
	case "ai_recommendation":
		// Return to AI recommendation - restore the recommendation results page
		// entry.Query contains the recommendation type (hot, trending, toprated, new, random)
		tType := entry.Query
		if tType == "" {
			tType = "hot" // Default to hot TV shows
		}

		logger.Info("[BackHandler] Returning to AI recommendation: %s", tType)

		// Check if we have cached search results to restore
		items, page, query, hasSearch := sess.GetSearchResults()
		logger.Info("[BackHandler] Checking search results: hasSearch=%v, query=%s, tType=%s", hasSearch, query, tType)

		// If we have cached results AND they match the current type, restore them
		if hasSearch && query == tType && len(items) > 0 {
			logger.Info("[BackHandler] Restoring cached recommendation results: %d items", len(items))
			return h.restoreRecommendationResults(sess, tType, items, page)
		}

		// No cached results, show loading message with reload button
		logger.Info("[BackHandler] No cached results, showing reload prompt")
		msg := services.NewMessageBuilder()
		msg.Bold("🎬 精选推荐").Newline()
		msg.Newline()

		// Set title and subtitle based on type
		title := ""
		subtitle := ""
		switch tType {
		case "trending":
			title = "🔥 本周热门"
			subtitle = "大家都在看的好片"
		case "hot":
			title = "📺 热门剧集"
			subtitle = "追剧必看热门番"
		case "toprated":
			title = "⭐ 必看神作"
			subtitle = "高分经典，不容错过"
		case "new":
			title = "🆕 最新上映"
			subtitle = "刚上线的新鲜内容"
		case "random":
			title = "🎲 随机探索"
			subtitle = "发现未知的精彩"
		default:
			title = "🎬 精选推荐"
			subtitle = "为您推荐优质内容"
		}
		msg.Italic(title).Newline()
		msg.Text(subtitle).Newline()
		msg.Newline()
		msg.Italic("💫 点击下方按钮重新加载推荐")

		// Add keyboard with reload button
		kb := services.NewKeyboardBuilder()
		kb.AddButton("🔄 重新加载", fmt.Sprintf("search:type:%s", tType))
		kb.NewRow()
		kb.AddButton("⬅️ 返回主菜单", "start")
		kb.NewRow()
		kb.AddButton("🔥 热门电影", "search:type:trending")
		kb.AddButton("📺 热播剧集", "search:type:hot")
		kb.NewRow()
		kb.AddButton("⭐ 高分佳作", "search:type:toprated")
		kb.AddButton("🆕 最新上线", "search:type:new")
		kb.NewRow()
		kb.AddButton("🎲 随机发现", "search:type:random")

		// 从详情页图片返回，需要删除图片消息后发送文本
		return &callback.Response{
			Text:          msg.Build(),
			Edit:          false,
			DeleteMessage: true,
			Keyboard:      convertKeyboard(kb.Build()),
			ParseMode:     "HTML",
		}, nil

	case "search":
		// Return to regular search results
		// Try to restore search results from session
		return h.restoreSearchResults(sess, ctx)

	default:
		// For any other source, return to AI recommendation or start menu
		if isAIRecommendationQuery(entry.Source) || isAIRecommendationQuery(entry.Query) {
			return &callback.Response{
				Text:        "",
				CallbackMsg: "",
				ShowAlert:   false,
				Keyboard:    nil,
			}, nil
		}

		// Show start menu using UI package for any other source
		baseMsg := ui.BuildMenuWith(ui.StyleCard, "云海影视助手", "你的私人选片师")

		// 添加用户观影人格显示（如果有的话）
		isPrivateChat := ctx.ChatType == "private"
		if isPrivateChat {
			if moodVal, ok := sess.GetString("pref_mood_last"); ok && moodVal != "" {
				moodMap := map[string]string{
					"relax":     "解压轻松",
					"mindblow":  "烧脑刺激",
					"emotional": "情绪共鸣",
					"healing":   "治愈慢节奏",
					"random":    "随机盲选",
				}
				if label, exists := moodMap[moodVal]; exists {
					baseMsg += fmt.Sprintf("\n🧠 观影人格：%s", label)
				}
			}
		}

		isAdmin := h.adminService != nil && h.adminService.IsAdmin(ctx.UserID)

		return &callback.Response{
			Text:      baseMsg,
			Edit:      true,
			Keyboard:  convertKeyboard(services.BuildStartKeyboardWithOptions(isAdmin, isPrivateChat && ai.GetManager().IsEnabled(), true)),
			ParseMode: "HTML",
		}, nil
	}
}

// restoreSearchResults restores the search results from session
func (h *BackHandler) restoreSearchResults(sess *session.Session, ctx *callback.Context) (*callback.Response, error) {
	items, _, query, hasSearch := sess.GetSearchResults()
	logger.Info("[BackHandler] restoreSearchResults: hasSearch=%v, items=%d, query=%s", hasSearch, len(items), query)

	if !hasSearch || len(items) == 0 {
		// Search results expired, show start menu using UI package
		logger.Info("[BackHandler] Search results expired or empty, showing start menu")
		baseMsg := ui.BuildMenuWith(ui.StyleCard, "云海影视助手", "你的私人选片师") + "\n\n⏰ 搜索结果已过期，请重新搜索"

		isAdmin := h.adminService != nil && h.adminService.IsAdmin(ctx.UserID)
		isPrivateChat := ctx.ChatType == "private"

		return &callback.Response{
			Text:      baseMsg,
			Edit:      true,
			Keyboard:  convertKeyboard(services.BuildStartKeyboardWithOptions(isAdmin, isPrivateChat && ai.GetManager().IsEnabled(), true)),
			ParseMode: "HTML",
		}, nil
	}

	// Restore search results display
	text := fmt.Sprintf("🔍 搜索结果「%s」\n\n找到 %d 条结果\n\n", query, len(items))

	// Build keyboard with results
	var keyboardRows [][]types.TelegramInlineKeyboardButton
	var row []types.TelegramInlineKeyboardButton

	for i, item := range items {
		if i >= 8 {
			break
		}

		year := ""
		if item.Year > 0 {
			year = fmt.Sprintf("%d", item.Year)
		}

		rating := ""
		if item.Rating > 0 {
			rating = fmt.Sprintf(" ⭐%.1f", item.Rating)
		}

		mediaType := "🎬 电影"
		if item.Type == "tv" {
			mediaType = "📺 剧集"
		}
		text += fmt.Sprintf("%d. %s (%s) %s%s\n", i+1, item.Title, year, mediaType, rating)

		row = append(row, types.TelegramInlineKeyboardButton{
			Text:         fmt.Sprintf("%d", i+1),
			CallbackData: fmt.Sprintf("select:id:%s:type:%s", item.ID, item.Type),
		})

		if len(row) == 4 {
			keyboardRows = append(keyboardRows, row)
			row = []types.TelegramInlineKeyboardButton{}
		}
	}

	if len(row) > 0 {
		keyboardRows = append(keyboardRows, row)
	}

	// Navigation row
	navRow := []types.TelegramInlineKeyboardButton{
		{Text: "⬅️ 返回主菜单", CallbackData: "start"},
	}
	keyboardRows = append(keyboardRows, navRow)

	keyboard := &types.TelegramInlineKeyboard{
		InlineKeyboard: keyboardRows,
	}

	logger.Info("[BackHandler] Restoring search results: query=%s, items=%d", query, len(items))
	// Use DeleteMessage=true when returning from photo to text message
	return &callback.Response{
		Text:          text,
		Edit:          false,
		DeleteMessage: true,
		Keyboard:      convertKeyboard(keyboard),
		ParseMode:     "HTML",
	}, nil
}

// restoreRecommendationResults restores the AI recommendation results from session
func (h *BackHandler) restoreRecommendationResults(sess *session.Session, tType string, items []session.SearchItem, page int) (*callback.Response, error) {
	logger.Info("[BackHandler] restoreRecommendationResults: tType=%s, items=%d, page=%d", tType, len(items), page)

	// Build title and subtitle based on type
	title := ""
	subtitle := ""
	switch tType {
	case "trending":
		title = "🔥 本周热门"
		subtitle = "大家都在看的好片"
	case "hot":
		title = "📺 热门剧集"
		subtitle = "追剧必看热门番"
	case "toprated":
		title = "⭐ 必看神作"
		subtitle = "高分经典，不容错过"
	case "new":
		title = "🆕 最新上映"
		subtitle = "刚上线的新鲜内容"
	case "random":
		title = "🎲 随机探索"
		subtitle = "发现未知的精彩"
	default:
		title = "🎬 精选推荐"
		subtitle = "为您推荐优质内容"
	}

	// Build message with results
	msg := services.NewMessageBuilder()
	msg.Bold("🎬 精选推荐").Newline()
	msg.Newline()
	msg.Italic(title).Newline()
	msg.Text(subtitle).Newline()
	msg.Newline()

	// Display count of results
	displayCount := len(items)
	if displayCount > 8 {
		displayCount = 8
	}
	msg.Textf("找到 %d 条结果\n\n", len(items))

	// Add items to message
	for i, item := range items[:displayCount] {
		year := ""
		if item.Year > 0 {
			year = fmt.Sprintf(" (%d)", item.Year)
		}

		rating := ""
		if item.Rating > 0 {
			rating = fmt.Sprintf(" ⭐%.1f", item.Rating)
		}

		mediaType := "🎬"
		if item.Type == "tv" {
			mediaType = "📺"
		}

		msg.Textf("%d. %s%s%s%s\n", i+1, item.Title, year, mediaType, rating)
	}

	// Build keyboard with results
	var keyboardRows [][]types.TelegramInlineKeyboardButton
	var row []types.TelegramInlineKeyboardButton

	for i, item := range items {
		if i >= 8 {
			break
		}

		row = append(row, types.TelegramInlineKeyboardButton{
			Text:         fmt.Sprintf("%d", i+1),
			CallbackData: fmt.Sprintf("detail:id:%s:type:%s", item.ID, item.Type),
		})

		// New row every 4 items
		if (i+1)%4 == 0 || i == displayCount-1 {
			keyboardRows = append(keyboardRows, row)
			row = []types.TelegramInlineKeyboardButton{}
		}
	}

	// Add navigation row
	navRow := []types.TelegramInlineKeyboardButton{
		{Text: "🔄 换一批", CallbackData: fmt.Sprintf("search:type:%s", tType)},
		{Text: "⬅️ 返回主菜单", CallbackData: "start"},
	}
	keyboardRows = append(keyboardRows, navRow)

	// Add other recommendations row
	otherRow := []types.TelegramInlineKeyboardButton{
		{Text: "🤖 其他推荐", CallbackData: "ai"},
	}
	keyboardRows = append(keyboardRows, otherRow)

	keyboard := &types.TelegramInlineKeyboard{
		InlineKeyboard: keyboardRows,
	}

	logger.Info("[BackHandler] Restoring recommendation results: tType=%s, items=%d", tType, len(items))
	return &callback.Response{
		Text:          msg.Build(),
		Edit:          false,
		DeleteMessage: true,
		Keyboard:      convertKeyboard(keyboard),
		ParseMode:     "HTML",
	}, nil
}

// Helper function to convert keyboard types
func convertKeyboard(tk *types.TelegramInlineKeyboard) *callback.Keyboard {
	if tk == nil {
		return &callback.Keyboard{InlineKeyboard: [][]callback.Button{}}
	}

	buttons := make([][]callback.Button, len(tk.InlineKeyboard))
	for i, row := range tk.InlineKeyboard {
		buttons[i] = make([]callback.Button, len(row))
		for j, btn := range row {
			buttons[i][j] = callback.Button{
				Text:         btn.Text,
				CallbackData: btn.CallbackData,
				URL:          btn.URL,
			}
		}
	}

	return &callback.Keyboard{InlineKeyboard: buttons}
}

// CancelHandler handles cancel action
type CancelHandler struct{}

func NewCancelHandler() *CancelHandler {
	return &CancelHandler{}
}

func (h *CancelHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	return &callback.Response{
		Text:     "✖️ 已取消",
		Edit:     true,
		Keyboard: &callback.Keyboard{},
	}, nil
}
