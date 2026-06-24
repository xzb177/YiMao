package handlers

import (
	"fmt"
	"sync"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/richmessage"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/logger"
)

// ============================================================
//  求片大冒险 v2 — 上瘾引擎
// ============================================================

const (
	adventureMaxLevels = 5
	adventureMaxHP     = 100
	adventureBaseDmg   = 30  // 基础扣血
	adventureTrapDmg   = 45  // 陷阱扣血更多
	adventureBossDmg   = 50  // Boss关扣血
	adventureComboHeal = 5   // 连击回血
)

// AdventureState 冒险状态
type AdventureState struct {
	MovieInfo   *services.MovieInfo
	Level       int
	HP          int
	Combo       int
	MaxCombo    int
	Score       int
	History     []string
	Scene       *services.AdventureScene
	InProgress  bool
	TotalLevels int
	PerfectRun  bool  // 全程无伤
}

// AdventureHandler 冒险处理器
type AdventureHandler struct {
	adventureSvc *services.AdventureService
	tmdbClient   *services.TMDBClient
	sessionMgr   *session.Manager
	telegram     *services.TelegramClient
	userMapping  services.UserMappingStore
	groupChatID  int64

	mu         sync.Mutex
	generating map[int64]bool
}

// NewAdventureHandler 创建冒险处理器
func NewAdventureHandler(
	adventureSvc *services.AdventureService,
	tmdbClient *services.TMDBClient,
	sessionMgr *session.Manager,
	telegram *services.TelegramClient,
	userMapping services.UserMappingStore,
	groupChatID int64,
) *AdventureHandler {
	return &AdventureHandler{
		adventureSvc: adventureSvc,
		tmdbClient:   tmdbClient,
		sessionMgr:   sessionMgr,
		telegram:     telegram,
		userMapping:  userMapping,
		groupChatID:  groupChatID,
		generating:   make(map[int64]bool),
	}
}

// Handle 冒险回调路由
func (h *AdventureHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	action := string(ctx.Callback.Action)

	switch action {
	case "adventure_start":
		return h.handleStart(ctx)
	case "adventure_choice":
		return h.handleChoice(ctx)
	case "adventure_retry":
		return h.handleRetry(ctx)
	case "adventure_quit":
		return h.handleQuit(ctx)
	default:
		return nil, fmt.Errorf("unknown adventure action: %s", action)
	}
}

// HandleAdventureText 处理文本输入中的电影名
func (h *AdventureHandler) HandleAdventureText(userID int64, chatID int64, movieName string) bool {
	if h.adventureSvc == nil || h.sessionMgr == nil {
		return false
	}
	sess := h.sessionMgr.GetOrCreate(userID)
	if sess == nil {
		return false
	}
	if _, exists := sess.Get("pending_adventure_input"); !exists {
		return false
	}
	// 消耗 pending 状态（无论后续是否成功，都清除）
	sess.Delete("pending_adventure_input")

	// 二次校验：如果已经有进行中的冒险，不重复启动
	if state, ok := sess.Get("adventure_state"); ok {
		if advState, ok := state.(*AdventureState); ok && advState.InProgress {
			h.telegram.SendMessage(chatID, "⚠️ 你已经有一场进行中的冒险了", "", nil)
			return true
		}
	}

	go h.startAdventureAsync(userID, chatID, movieName)
	return true
}

// handleStart 点击"趣味求片"
func (h *AdventureHandler) handleStart(ctx *callback.Context) (*callback.Response, error) {
	if h.sessionMgr != nil {
		sess := h.sessionMgr.GetOrCreate(ctx.UserID)
		if sess != nil {
			// 清除所有旧的冒险状态（防止"已过期"残留）
			sess.Delete("adventure_state")
			sess.Set("pending_adventure_input", true)
		}
	}
	return &callback.Response{
		Text: "⚔️ **求片大冒险**\n\n请发送你想求的电影/剧集名称\n\n例如：`流浪地球` 或 `权力的游戏`\n\n⚠️ 只有通关才能提交求片请求\n大多数人会在第2关倒下",
	}, nil
}

// handleChoice 用户选择选项
func (h *AdventureHandler) handleChoice(ctx *callback.Context) (*callback.Response, error) {
	if h.sessionMgr == nil {
		return &callback.Response{CallbackMsg: "❌ 服务未就绪", ShowAlert: true}, nil
	}

	choiceIdx := -1
	if ctx.Callback != nil && ctx.Callback.Params != nil {
		if idxStr, ok := ctx.Callback.Params["idx"]; ok {
			fmt.Sscanf(idxStr, "%d", &choiceIdx)
		}
	}
	if choiceIdx < 0 {
		return &callback.Response{CallbackMsg: "❌ 无效选项", ShowAlert: true}, nil
	}

	sess := h.sessionMgr.GetOrCreate(ctx.UserID)
	if sess == nil {
		return &callback.Response{CallbackMsg: "❌ 会话异常", ShowAlert: true}, nil
	}

	state, ok := sess.Get("adventure_state")
	if !ok {
		return &callback.Response{CallbackMsg: "❌ 没有进行中的冒险，请先开始", ShowAlert: true}, nil
	}

	advState, ok := state.(*AdventureState)
	if !ok || !advState.InProgress {
		// 旧状态残留，自动清除，不让用户卡死
		sess.Delete("adventure_state")
		sess.Delete("pending_adventure_input")
		return &callback.Response{CallbackMsg: "❌ 冒险已结束，请重新开始", ShowAlert: true}, nil
	}

	scene := advState.Scene
	if scene == nil || choiceIdx >= len(scene.Choices) {
		return &callback.Response{CallbackMsg: "❌ 无效选项", ShowAlert: true}, nil
	}

	choice := scene.Choices[choiceIdx]
	advState.History = append(advState.History, fmt.Sprintf("L%d选[%s]", advState.Level, choice.Text))

	if choice.Correct {
		// ✅ 选择正确
		advState.Combo++
		if advState.Combo > advState.MaxCombo {
			advState.MaxCombo = advState.Combo
		}
		// 连击回血
		if advState.Combo >= 3 {
			heal := adventureComboHeal * (advState.Combo - 2)
			advState.HP += heal
			if advState.HP > adventureMaxHP {
				advState.HP = adventureMaxHP
			}
		}
		// 计分
		baseScore := advState.Level * 10
		comboBonus := advState.Combo * 5
		advState.Score += baseScore + comboBonus

		advState.Level++

		if advState.Level > advState.TotalLevels {
			// 🏆 通关！
			advState.InProgress = false
			advState.Score += advState.HP // 剩余HP作为额外分
			if advState.Score > 100 {
				advState.Score = 100
			}
			sess.Set("adventure_state", advState)
			go h.finishAdventureAsync(ctx.UserID, ctx.ChatID, advState, true)

			return &callback.Response{
				CallbackMsg: fmt.Sprintf("✅ %s\n🔥 连击 x%d！进入最终决战...", choice.Result, advState.Combo),
				ShowAlert:   false,
			}, nil
		}

		sess.Set("adventure_state", advState)

		// 显示连击卡片 + 生成下一关
		go h.handleCorrectChoice(ctx.UserID, ctx.ChatID, advState, choice.Result)
		return &callback.Response{
			CallbackMsg: fmt.Sprintf("✅ %s\n🔥 连击 x%d", choice.Result, advState.Combo),
			ShowAlert:   false,
		}, nil
	}

	// ❌ 选择错误
	// 计算扣血（陷阱扣更多）
	damage := adventureBaseDmg
	if choice.IsTrap {
		damage = adventureTrapDmg
		advState.PerfectRun = false
	} else {
		advState.PerfectRun = false
	}
	// Boss关扣血更多
	if advState.Level == advState.TotalLevels {
		damage = adventureBossDmg
	}
	// 自定义扣血
	if choice.HPChange != 0 {
		damage = choice.HPChange
	}

	advState.HP -= damage
	advState.Combo = 0 // 连击归零
	advState.History = append(advState.History, fmt.Sprintf("(-%dHP)", damage))

	if advState.HP <= 0 {
		// 💀 死亡
		advState.HP = 0
		advState.InProgress = false
		sess.Set("adventure_state", advState)
		go h.finishAdventureAsync(ctx.UserID, ctx.ChatID, advState, false)
		return &callback.Response{
			CallbackMsg: fmt.Sprintf("💀 %s\n生命耗尽...", choice.Result),
			ShowAlert:   false,
		}, nil
	}

	// 还活着
	sess.Set("adventure_state", advState)
	go h.sendDamageCard(ctx.UserID, ctx.ChatID, advState, damage, choice.Result)

	return &callback.Response{
		CallbackMsg: fmt.Sprintf("💥 %s\n❤️ -%d HP（剩余 %d%%）", choice.Result, damage, advState.HP),
		ShowAlert:   false,
	}, nil
}

// handleRetry 重新开始
func (h *AdventureHandler) handleRetry(ctx *callback.Context) (*callback.Response, error) {
	if h.sessionMgr == nil {
		return &callback.Response{CallbackMsg: "❌ 服务未就绪", ShowAlert: true}, nil
	}
	sess := h.sessionMgr.GetOrCreate(ctx.UserID)
	if sess == nil {
		return &callback.Response{CallbackMsg: "❌ 会话异常", ShowAlert: true}, nil
	}

	// 获取旧的电影信息用于重试
	var movieName string
	if state, ok := sess.Get("adventure_state"); ok {
		if advState, ok := state.(*AdventureState); ok && advState.MovieInfo != nil {
			movieName = advState.MovieInfo.Title
		}
	}

	// 无条件清除所有旧状态
	sess.Delete("adventure_state")
	sess.Delete("pending_adventure_input")

	if movieName == "" {
		return h.handleStart(ctx)
	}

	go h.startAdventureAsync(ctx.UserID, ctx.ChatID, movieName)
	return &callback.Response{
		CallbackMsg: fmt.Sprintf("🔄 重新挑战《%s》...", movieName),
		ShowAlert:   false,
	}, nil
}

// handleQuit 退出
func (h *AdventureHandler) handleQuit(ctx *callback.Context) (*callback.Response, error) {
	// 无条件清除所有冒险相关状态
	if h.sessionMgr != nil {
		sess := h.sessionMgr.GetOrCreate(ctx.UserID)
		if sess != nil {
			sess.Delete("adventure_state")
			sess.Delete("pending_adventure_input")
		}
	}

	// 群聊里直接返回toast，不发长消息
	if ctx.ChatType != "private" {
		return &callback.Response{
			CallbackMsg: "👋 已退出冒险",
			ShowAlert:   false,
		}, nil
	}

	return &callback.Response{
		Text: "👋 冒险已退出\n\n输入 /game 回到游戏中心",
	}, nil
}

// ============================================================
//  异步方法
// ============================================================

// startAdventureAsync 开始冒险
func (h *AdventureHandler) startAdventureAsync(userID int64, chatID int64, movieName string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Info("[Adventure] Panic for user %d: %v", userID, r)
			h.telegram.SendMessage(chatID, "❌ 冒险启动出错了，请稍后再试", "", nil)
		}
		h.mu.Lock()
		delete(h.generating, userID)
		h.mu.Unlock()
	}()

	h.mu.Lock()
	if h.generating[userID] {
		h.mu.Unlock()
		return
	}
	h.generating[userID] = true
	h.mu.Unlock()

	loadingMsg, _ := h.telegram.SendMessage(chatID, "⚔️ 正在进入「"+movieName+"」的世界...", "", nil)

	// 清除旧的冒险状态（防止"已过期"错误）
	if h.sessionMgr != nil {
		sess := h.sessionMgr.GetOrCreate(userID)
		if sess != nil {
			sess.Delete("adventure_state")
			sess.Delete("pending_adventure_input")
		}
	}

	movieInfo, err := h.adventureSvc.SearchMovieInfo(movieName)
	if err != nil {
		if loadingMsg != nil {
			h.telegram.DeleteMessage(chatID, loadingMsg.MessageID)
		}
		h.telegram.SendMessage(chatID, fmt.Sprintf("❌ 找不到这部电影\n%s\n\n试试其他名字？", err.Error()), "", nil)
		return
	}

	// 发送入口卡片
	entryCard := richmessage.BuildAdventureEntryCard(richmessage.AdventureEntryCardData{
		MovieTitle: movieInfo.Title,
		MovieYear:  movieInfo.Year,
		Genres:     movieInfo.Genres,
		Overview:   movieInfo.Overview,
		Rating:     movieInfo.Rating,
	})

	kb := services.NewKeyboardBuilder()
	kb.AddButton("⚔️ 接受挑战", "adventure_choice:idx:0") // 先占位，生成场景后替换
	kb.AddButton("❌ 退出", "adventure_quit")

	if loadingMsg != nil {
		h.telegram.DeleteMessage(chatID, loadingMsg.MessageID)
	}
	h.telegram.SendMessage(chatID, entryCard.Markdown, "Markdown", kb.Build())

	// 生成第一关
	scene, err := h.adventureSvc.GenerateScene(movieInfo, 1, adventureMaxLevels, nil, adventureMaxHP)
	if err != nil {
		logger.Info("[Adventure] AI scene gen failed, using fallback: %v", err)
		scene = h.adventureSvc.GenerateFallbackScene(movieInfo, 1, adventureMaxLevels)
	}
	scene.TotalLevels = adventureMaxLevels

	state := &AdventureState{
		MovieInfo:   movieInfo,
		Level:       1,
		HP:          adventureMaxHP,
		Combo:       0,
		MaxCombo:    0,
		Score:       0,
		History:     []string{},
		Scene:       scene,
		InProgress:  true,
		TotalLevels: adventureMaxLevels,
		PerfectRun:  true,
	}

	if h.sessionMgr != nil {
		sess := h.sessionMgr.GetOrCreate(userID)
		if sess != nil {
			sess.Set("adventure_state", state)
		}
	}

	h.sendSceneCard(chatID, state)
}

// handleCorrectChoice 选对后的处理 — 只发场景卡片，反馈内嵌
func (h *AdventureHandler) handleCorrectChoice(userID int64, chatID int64, state *AdventureState, choiceResult string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Info("[Adventure] Correct choice panic: %v", r)
		}
	}()

	// 生成下一关（不发单独的combo卡片，反馈直接写在场景卡片里）
	loadingMsg, _ := h.telegram.SendMessage(chatID, fmt.Sprintf("⏳ 正在构造第 %d 关...", state.Level), "", nil)

	scene, err := h.adventureSvc.GenerateScene(state.MovieInfo, state.Level, state.TotalLevels, state.History, state.HP)
	if err != nil {
		logger.Info("[Adventure] AI scene gen failed for level %d: %v", state.Level, err)
		scene = h.adventureSvc.GenerateFallbackScene(state.MovieInfo, state.Level, state.TotalLevels)
	}
	scene.TotalLevels = state.TotalLevels
	state.Scene = scene

	if h.sessionMgr != nil {
		sess := h.sessionMgr.GetOrCreate(userID)
		if sess != nil {
			sess.Set("adventure_state", state)
		}
	}

	if loadingMsg != nil {
		h.telegram.DeleteMessage(chatID, loadingMsg.MessageID)
	}

	h.sendSceneCard(chatID, state)
}

// sendDamageCard 发送受伤卡片 + 剩余选项
func (h *AdventureHandler) sendDamageCard(userID int64, chatID int64, state *AdventureState, damage int, choiceResult string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Info("[Adventure] Damage card panic: %v", r)
		}
	}()

	isDead := state.HP <= 0
	card := richmessage.BuildAdventureDamageCard(richmessage.AdventureDamageCardData{
		ChoiceResult: choiceResult,
		Damage:       damage,
		HP:           state.HP,
		Level:        state.Level,
		TotalLevels:  state.TotalLevels,
		Combo:        state.Combo,
		Score:        state.Score,
		IsDead:       isDead,
	})

	if isDead {
		kb := services.NewKeyboardBuilder()
		kb.AddButton("🔄 再来一次", "adventure_retry")
		kb.AddButton("🎬 换一部电影", "adventure_start")
		kb.NewRow()
		kb.AddButton("🎮 游戏中心", "game_menu")
		h.telegram.SendMessage(chatID, card.Markdown, "Markdown", kb.Build())
		return
	}

	// 还活着 — 显示剩余选项
	scene := state.Scene
	if scene == nil {
		return
	}

	kb := services.NewKeyboardBuilder()
	for i, choice := range scene.Choices {
		if choice.Correct || (!choice.Correct && choice.Text != "") {
			kb.AddButton(choice.Text, fmt.Sprintf("adventure_choice:idx:%d", i))
			if i == 1 {
				kb.NewRow()
			}
		}
	}

	h.telegram.SendMessage(chatID, card.Markdown, "Markdown", kb.Build())
}

// finishAdventureAsync 完成冒险
func (h *AdventureHandler) finishAdventureAsync(userID int64, chatID int64, state *AdventureState, success bool) {
	defer func() {
		if r := recover(); r != nil {
			logger.Info("[Adventure] Finish panic: %v", r)
			h.telegram.SendMessage(chatID, "❌ 结局生成出错了", "", nil)
		}
	}()

	loadingMsg, _ := h.telegram.SendMessage(chatID, "🎬 生成结局...", "", nil)

	result, err := h.adventureSvc.GenerateEndScene(
		state.MovieInfo, state.History, success, state.HP, state.MaxCombo, state.TotalLevels,
	)
	if err != nil {
		logger.Info("[Adventure] End scene AI failed: %v", err)
		result = h.adventureSvc.GenerateFallbackResult(state.MovieInfo, success, state.HP, state.Level-1, state.TotalLevels)
	}

	if loadingMsg != nil {
		h.telegram.DeleteMessage(chatID, loadingMsg.MessageID)
	}

	// 群通知：只有神仙操作才通报（稀缺性 = 更有面子）
	userName := h.getUserName(userID)
	if success {
		shouldNotify := false
		notifyMsg := ""
		switch {
		case result.Grade == "SSS":
			shouldNotify = true
			notifyMsg = fmt.Sprintf(`👑 ━━━ SSS ━━━ 👑

%s  ·  《%s》

🎯 %d分  ❤️ %d%%  🔥 x%d%s

%s`,
				userName, state.MovieInfo.Title,
				result.Score, state.HP, state.MaxCombo,
				boolStr(state.PerfectRun, "  ⚔️ 完美", ""),
				boolStr(state.PerfectRun, "全程无伤通关，无人能及", "传奇操作，群内首位通关者"))

		case result.Grade == "SS":
			shouldNotify = true
			notifyMsg = fmt.Sprintf(`💎 ━━━ SS ━━━ 💎

%s  ·  《%s》

🎯 %d分  ❤️ %d%%  🔥 x%d

距离完美，只差一步`,
				userName, state.MovieInfo.Title,
				result.Score, state.HP, state.MaxCombo)

		case state.PerfectRun:
			shouldNotify = true
			notifyMsg = fmt.Sprintf(`🛡️ ━━━ 无伤通关 ━━━ 🛡️

%s  ·  《%s》

🎯 %d分  全程零失误

五关全过，一滴血没掉`,
				userName, state.MovieInfo.Title, result.Score)

		case state.MaxCombo >= 4:
			shouldNotify = true
			notifyMsg = fmt.Sprintf(`🔥 ━━━ x%d 连击 ━━━ 🔥

%s  ·  《%s》

🎯 %d分  连续%d关一选即中

这部电影他真的看过`,
				state.MaxCombo, userName, state.MovieInfo.Title, result.Score, state.MaxCombo)
		}
		if shouldNotify {
			h.notifyGroup(userName, notifyMsg)
		}
	} else if state.Level-1 >= 4 {
		h.notifyGroup(userName, fmt.Sprintf(`💀 ━━━ 惜败 ━━━ 💀

%s  ·  《%s》

倒在第 %d/%d 关

差一步就通关了... 要不要帮他一把？`,
			userName, state.MovieInfo.Title, state.Level-1, state.TotalLevels))
	}

	// 发送结果卡片
	if success {
		card := richmessage.BuildAdventureSuccessCard(richmessage.AdventureSuccessCardData{
			MovieTitle: state.MovieInfo.Title,
			MovieYear:  state.MovieInfo.Year,
			Genres:     state.MovieInfo.Genres,
			Score:      result.Score,
			Grade:      result.Grade,
			FinalScene: result.FinalScene,
			EasterEgg:  result.EasterEgg,
			Stats:      result.Stats,
			HP:         state.HP,
			MaxCombo:   state.MaxCombo,
		})

		kb := services.NewKeyboardBuilder()
		kb.AddButton("🔄 再挑战一次", "adventure_retry")
		kb.AddButton("🎬 换一部电影", "adventure_start")
		kb.NewRow()
		kb.AddButton("🎮 游戏中心", "game_menu")

		h.telegram.SendMessage(chatID, card.Markdown, "Markdown", kb.Build())
	} else {
		card := richmessage.BuildAdventureFailCard(richmessage.AdventureFailCardData{
			MovieTitle:  state.MovieInfo.Title,
			MovieYear:   state.MovieInfo.Year,
			Genres:      state.MovieInfo.Genres,
			Level:       state.Level - 1,
			TotalLevels: state.TotalLevels,
			FinalScene:  result.FinalScene,
			DeathReason: result.DeathReason,
			Tips:        result.Tips,
			Score:       result.Score,
			Grade:       result.Grade,
			Stats:       result.Stats,
			MaxCombo:    state.MaxCombo,
			HP:          0,
		})

		kb := services.NewKeyboardBuilder()
		kb.AddButton("🔄 我知道答案了！", "adventure_retry")
		kb.AddButton("🎬 换一部电影", "adventure_start")
		kb.NewRow()
		kb.AddButton("🎮 游戏中心", "game_menu")

		h.telegram.SendMessage(chatID, card.Markdown, "Markdown", kb.Build())
	}
}

// sendSceneCard 发送场景卡片
func (h *AdventureHandler) sendSceneCard(chatID int64, state *AdventureState) {
	scene := state.Scene
	if scene == nil {
		return
	}

	var choices []richmessage.AdventureChoiceView
	for i, c := range scene.Choices {
		choices = append(choices, richmessage.AdventureChoiceView{Index: i, Text: c.Text})
	}

	// 构建内嵌反馈（只在第2关以后显示，表示上一关的结果）
	lastResult := ""
	if state.Level > 1 {
		switch {
		case state.Combo >= 5:
			lastResult = fmt.Sprintf("🔥🔥🔥 五连绝世！x%d 连击！", state.Combo)
		case state.Combo >= 4:
			lastResult = fmt.Sprintf("🔥🔥 四连超凡！x%d 连击", state.Combo)
		case state.Combo >= 3:
			lastResult = fmt.Sprintf("🔥 三连破敌！x%d 连击", state.Combo)
		case state.Combo >= 2:
			lastResult = fmt.Sprintf("⚡ 双连命中！x%d 连击", state.Combo)
		default:
			lastResult = "✅ 上一关正确"
		}
	}

	card := richmessage.BuildAdventureSceneCard(richmessage.AdventureSceneCardData{
		MovieTitle:   state.MovieInfo.Title,
		MovieYear:    state.MovieInfo.Year,
		Genres:       state.MovieInfo.Genres,
		Level:        state.Level,
		TotalLevels:  state.TotalLevels,
		StageName:    scene.StageName,
		SceneTitle:   scene.Title,
		Description:  scene.Description,
		Atmosphere:   scene.Atmosphere,
		Choices:      choices,
		Hint:         scene.Hint,
		HP:           state.HP,
		Combo:        state.Combo,
		Score:        state.Score,
		IsBoss:       state.Level == state.TotalLevels,
		LastResult:   lastResult,
	})

	kb := services.NewKeyboardBuilder()
	for i, choice := range scene.Choices {
		kb.AddButton(choice.Text, fmt.Sprintf("adventure_choice:idx:%d", i))
		if i == 1 || i == 3 {
			kb.NewRow()
		}
	}

	h.telegram.SendMessage(chatID, card.Markdown, "Markdown", kb.Build())
}

// boolStr 条件字符串
func boolStr(cond bool, trueVal, falseVal string) string {
	if cond {
		return trueVal
	}
	return falseVal
}

// getUserName 获取用户显示名
func (h *AdventureHandler) getUserName(userID int64) string {
	if h.userMapping != nil {
		if name, err := h.userMapping.GetMoviePilotUsername(userID); err == nil && name != "" {
			return name
		}
	}
	return fmt.Sprintf("用户%d", userID)
}

// notifyGroup 群通知（10分钟自毁）
func (h *AdventureHandler) notifyGroup(userName, message string) {
	if h.groupChatID == 0 || h.telegram == nil {
		return
	}
	go func() {
		sent, err := h.telegram.SendMessage(h.groupChatID, message, "", nil)
		if err != nil {
			return
		}
		go func(chatID int64, msgID int64) {
			time.Sleep(10 * time.Minute)
			_ = h.telegram.DeleteMessage(chatID, msgID)
		}(h.groupChatID, sent.MessageID)
	}()
}
