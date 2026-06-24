package handlers

import (
	"fmt"
	"strings"
	"sync"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/richmessage"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/logger"
	"time"
)

// ============================================================
//  求片大冒险 — 回调处理器
// ============================================================

const (
	adventureMaxLevels = 3 // 总关卡数
	adventureMaxHP     = 100
	adventureWrongDmg  = 35 // 选错扣血
)

// AdventureState 冒险状态（存在 session 中）
type AdventureState struct {
	MovieInfo   *services.MovieInfo
	Level       int
	HP          int
	History     []string // 选择路径记录
	Scene       *services.AdventureScene // 当前场景
	InProgress  bool
	TotalLevels int
}

// AdventureHandler 冒险处理器
type AdventureHandler struct {
	adventureSvc *services.AdventureService
	tmdbClient   *services.TMDBClient
	sessionMgr   *session.Manager
	telegram     *services.TelegramClient
	userMapping  services.UserMappingStore
	groupChatID  int64

	// 缓存：防止并发生成同一用户多个场景
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

// HandleAdventureText 处理文本输入中的电影名（由 poll.go 调用）
func (h *AdventureHandler) HandleAdventureText(userID int64, chatID int64, movieName string) bool {
	if h.adventureSvc == nil || h.sessionMgr == nil {
		return false
	}

	sess := h.sessionMgr.GetOrCreate(userID)
	if sess == nil {
		return false
	}

	// 检查是否处于 pending 状态
	if _, exists := sess.Get("pending_adventure_input"); !exists {
		return false
	}

	// 清除 pending 状态
	sess.Delete("pending_adventure_input")

	// 异步生成第一关
	go h.startAdventureAsync(userID, chatID, movieName)

	return true
}

// handleStart 点击"开始冒险"按钮
func (h *AdventureHandler) handleStart(ctx *callback.Context) (*callback.Response, error) {
	// 设置 pending 状态，等待用户输入电影名
	if h.sessionMgr != nil {
		sess := h.sessionMgr.GetOrCreate(ctx.UserID)
		if sess != nil {
			sess.Set("pending_adventure_input", true)
		}
	}

	return &callback.Response{
		Text: "⚔️ **求片大冒险**\n\n请发送你想求的电影/剧集名称，我将以主角身份带你闯关！\n\n例如：`流浪地球` 或 `权力的游戏`\n\n💡 没看过？没关系，靠直觉也能试试～",
	}, nil
}

// handleChoice 用户选择了一个选项
func (h *AdventureHandler) handleChoice(ctx *callback.Context) (*callback.Response, error) {
	if h.sessionMgr == nil {
		return &callback.Response{CallbackMsg: "❌ 服务未就绪", ShowAlert: true}, nil
	}

	// 提取选项索引
	choiceIdx := -1
	if ctx.Callback != nil && ctx.Callback.Params != nil {
		if idxStr, ok := ctx.Callback.Params["idx"]; ok {
			fmt.Sscanf(idxStr, "%d", &choiceIdx)
		}
	}
	if choiceIdx < 0 {
		return &callback.Response{CallbackMsg: "❌ 无效选项", ShowAlert: true}, nil
	}

	// 获取冒险状态
	sess := h.sessionMgr.GetOrCreate(ctx.UserID)
	if sess == nil {
		return &callback.Response{CallbackMsg: "❌ 会话异常", ShowAlert: true}, nil
	}

	state, ok := sess.Get("adventure_state")
	if !ok {
		return &callback.Response{CallbackMsg: "❌ 冒险已过期，请重新开始", ShowAlert: true}, nil
	}

	advState, ok := state.(*AdventureState)
	if !ok || !advState.InProgress {
		return &callback.Response{CallbackMsg: "❌ 冒险已结束", ShowAlert: true}, nil
	}

	// 检查选项是否有效
	scene := advState.Scene
	if scene == nil || choiceIdx >= len(scene.Choices) {
		return &callback.Response{CallbackMsg: "❌ 无效选项", ShowAlert: true}, nil
	}

	choice := scene.Choices[choiceIdx]
	advState.History = append(advState.History, fmt.Sprintf("第%d关选择[%s]", advState.Level, choice.Text))

	if choice.Correct {
		// ✅ 选择正确
		advState.Level++

		if advState.Level > advState.TotalLevels {
			// 🏆 通关！
			advState.InProgress = false
			sess.Set("adventure_state", advState)

			go h.finishAdventureAsync(ctx.UserID, ctx.ChatID, advState, true)
			return &callback.Response{
				CallbackMsg: fmt.Sprintf("✅ %s\n进入下一关...", choice.Result),
				ShowAlert:   false,
			}, nil
		}

		// 还没通关，生成下一关
		sess.Set("adventure_state", advState)

		go h.generateNextSceneAsync(ctx.UserID, ctx.ChatID, advState, choice.Result)
		return &callback.Response{
			CallbackMsg: fmt.Sprintf("✅ %s\n进入第%d关...", choice.Result, advState.Level),
			ShowAlert:   false,
		}, nil
	}

	// ❌ 选择错误
	advState.HP -= adventureWrongDmg
	advState.History = append(advState.History, fmt.Sprintf("（受伤 -%dHP）", adventureWrongDmg))

	if advState.HP <= 0 {
		// 💀 死亡
		advState.InProgress = false
		sess.Set("adventure_state", advState)

		go h.finishAdventureAsync(ctx.UserID, ctx.ChatID, advState, false)
		return &callback.Response{
			CallbackMsg: fmt.Sprintf("💀 %s\n生命值耗尽...", choice.Result),
			ShowAlert:   false,
		}, nil
	}

	// 还活着，但受了伤 — 显示受伤结果，给机会继续
	sess.Set("adventure_state", advState)

	// 发送受伤通知 + 当前关卡的剩余选项（排除已选错的）
	go h.sendDamageNotification(ctx.UserID, ctx.ChatID, advState, choiceIdx, choice.Result)

	return &callback.Response{
		CallbackMsg: fmt.Sprintf("💥 %s\n❤️ -%d HP（剩余 %d%%）", choice.Result, adventureWrongDmg, advState.HP),
		ShowAlert:   false,
	}, nil
}

// handleRetry 重新开始冒险
func (h *AdventureHandler) handleRetry(ctx *callback.Context) (*callback.Response, error) {
	if h.sessionMgr == nil {
		return &callback.Response{CallbackMsg: "❌ 服务未就绪", ShowAlert: true}, nil
	}

	sess := h.sessionMgr.GetOrCreate(ctx.UserID)
	if sess == nil {
		return &callback.Response{CallbackMsg: "❌ 会话异常", ShowAlert: true}, nil
	}

	// 获取之前的电影信息
	state, ok := sess.Get("adventure_state")
	if !ok {
		// 没有之前的状态，走重新输入流程
		return h.handleStart(ctx)
	}

	advState, ok := state.(*AdventureState)
	if !ok || advState.MovieInfo == nil {
		return h.handleStart(ctx)
	}

	// 直接用同一部电影重新开始
	go h.startAdventureAsync(ctx.UserID, ctx.ChatID, advState.MovieInfo.Title)

	return &callback.Response{
		CallbackMsg: fmt.Sprintf("🔄 重新开始《%s》的冒险...", advState.MovieInfo.Title),
		ShowAlert:   false,
	}, nil
}

// handleQuit 退出冒险
func (h *AdventureHandler) handleQuit(ctx *callback.Context) (*callback.Response, error) {
	if h.sessionMgr != nil {
		sess := h.sessionMgr.GetOrCreate(ctx.UserID)
		if sess != nil {
			sess.Delete("adventure_state")
			sess.Delete("pending_adventure_input")
		}
	}

	return &callback.Response{
		Text: "👋 冒险已退出。\n\n🎬 没关系，随时可以再来挑战！\n输入 /game 回到游戏中心。",
	}, nil
}

// --- 异步方法 ---

// startAdventureAsync 异步开始冒险（生成第一关）
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

	// 防止重复生成
	h.mu.Lock()
	if h.generating[userID] {
		h.mu.Unlock()
		return
	}
	h.generating[userID] = true
	h.mu.Unlock()

	// 发送加载消息
	loadingMsg, err := h.telegram.SendMessage(chatID, "⚔️ 正在进入《"+movieName+"》的世界...", "", nil)
	if err != nil {
		logger.Info("[Adventure] Failed to send loading message: %v", err)
	}

	// 搜索电影信息
	movieInfo, err := h.adventureSvc.SearchMovieInfo(movieName)
	if err != nil {
		if loadingMsg != nil {
			h.telegram.DeleteMessage(chatID, loadingMsg.MessageID)
		}
		h.telegram.SendMessage(chatID, fmt.Sprintf("❌ 找不到这部电影：\n%s\n\n试试其他名字？", err.Error()), "", nil)
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
	kb.AddButton("⚔️ 开始冒险！", "adventure_start")
	kb.AddButton("❌ 退出", "adventure_quit")

	h.telegram.SendMessage(chatID, entryCard.Markdown, "Markdown", kb.Build())

	// 删除加载消息
	if loadingMsg != nil {
		h.telegram.DeleteMessage(chatID, loadingMsg.MessageID)
	}

	// 生成第一关
	scene, err := h.adventureSvc.GenerateFirstScene(movieInfo)
	if err != nil {
		logger.Info("[Adventure] Failed to generate first scene: %v", err)
		// 回退：使用模板场景
		scene = h.generateFallbackScene(movieInfo, 1)
	}

	// 保存状态
	state := &AdventureState{
		MovieInfo:   movieInfo,
		Level:       1,
		HP:          adventureMaxHP,
		History:     []string{},
		Scene:       scene,
		InProgress:  true,
		TotalLevels: adventureMaxLevels,
	}

	if h.sessionMgr != nil {
		sess := h.sessionMgr.GetOrCreate(userID)
		if sess != nil {
			sess.Set("adventure_state", state)
		}
	}

	// 发送第一关卡片
	h.sendSceneCard(chatID, state)
}

// generateNextSceneAsync 异步生成下一关
func (h *AdventureHandler) generateNextSceneAsync(userID int64, chatID int64, state *AdventureState, choiceResult string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Info("[Adventure] Next scene panic for user %d: %v", userID, r)
			h.telegram.SendMessage(chatID, "❌ 关卡生成出错了，请稍后再试", "", nil)
		}
	}()

	// 发送过渡消息
	loadingMsg, _ := h.telegram.SendMessage(chatID, fmt.Sprintf("🎬 %s\n\n⏳ 正在进入第%d关...", choiceResult, state.Level), "", nil)

	// 生成下一关
	scene, err := h.adventureSvc.GenerateNextScene(state.MovieInfo, state.History, state.Level)
	if err != nil {
		logger.Info("[Adventure] Failed to generate scene %d: %v", state.Level, err)
		scene = h.generateFallbackScene(state.MovieInfo, state.Level)
	}

	state.Scene = scene

	// 保存状态
	if h.sessionMgr != nil {
		sess := h.sessionMgr.GetOrCreate(userID)
		if sess != nil {
			sess.Set("adventure_state", state)
		}
	}

	// 删除过渡消息
	if loadingMsg != nil {
		h.telegram.DeleteMessage(chatID, loadingMsg.MessageID)
	}

	// 发送场景卡片
	h.sendSceneCard(chatID, state)
}

// finishAdventureAsync 异步完成冒险（生成结局）
func (h *AdventureHandler) finishAdventureAsync(userID int64, chatID int64, state *AdventureState, success bool) {
	defer func() {
		if r := recover(); r != nil {
			logger.Info("[Adventure] Finish panic for user %d: %v", userID, r)
			h.telegram.SendMessage(chatID, "❌ 结局生成出错了", "", nil)
		}
	}()

	// 发送加载消息
	loadingMsg, _ := h.telegram.SendMessage(chatID, "🎬 生成结局...", "", nil)

	// 生成结局
	result, err := h.adventureSvc.GenerateEndScene(state.MovieInfo, state.History, success)
	if err != nil {
		logger.Info("[Adventure] Failed to generate end scene: %v", err)
		// 回退
		if success {
			result = &services.AdventureResult{
				Success:    true,
				FinalScene: fmt.Sprintf("你以主角的身份，完美地走完了《%s》的每一个关键时刻。", state.MovieInfo.Title),
				Score:      75,
				EasterEgg:  "这部电影的主角差点由另一个演员出演。",
			}
		} else {
			result = &services.AdventureResult{
				Success:     false,
				FinalScene:  h.adventureSvc.GenerateDeathScene(state.MovieInfo, state.Level),
				DeathReason: "做出了一般人会做的选择",
				Score:       30,
				Tips:        "也许你应该更了解主角的思维方式",
			}
		}
	}

	// 删除加载消息
	if loadingMsg != nil {
		h.telegram.DeleteMessage(chatID, loadingMsg.MessageID)
	}

	// 群通知
	userName := h.getUserName(userID)
	if success {
		h.notifyGroup(userName, fmt.Sprintf("在《%s》大冒险中通关！得分 %d 🏆", state.MovieInfo.Title, result.Score))
	} else {
		h.notifyGroup(userName, fmt.Sprintf("在《%s》大冒险中倒在第%d关 💀", state.MovieInfo.Title, state.Level))
	}

	// 发送结果卡片
	if success {
		card := richmessage.BuildAdventureSuccessCard(richmessage.AdventureSuccessCardData{
			MovieTitle: state.MovieInfo.Title,
			MovieYear:  state.MovieInfo.Year,
			Genres:     state.MovieInfo.Genres,
			Score:      result.Score,
			FinalScene: result.FinalScene,
			EasterEgg:  result.EasterEgg,
			TotalHP:    state.HP,
		})

		kb := services.NewKeyboardBuilder()
		kb.AddButton("🔄 再来一次", "adventure_retry")
		kb.AddButton("🎬 换一部电影", "adventure_start")
		kb.NewRow()
		kb.AddButton("🎮 游戏中心", "game_menu")

		h.telegram.SendMessage(chatID, card.Markdown, "Markdown", kb.Build())
	} else {
		card := richmessage.BuildAdventureFailCard(richmessage.AdventureFailCardData{
			MovieTitle:  state.MovieInfo.Title,
			MovieYear:   state.MovieInfo.Year,
			Genres:      state.MovieInfo.Genres,
			Level:       state.Level,
			TotalLevels: state.TotalLevels,
			FinalScene:  result.FinalScene,
			DeathReason: result.DeathReason,
			Tips:        result.Tips,
			Score:       result.Score,
		})

		kb := services.NewKeyboardBuilder()
		kb.AddButton("🔄 再来一次", "adventure_retry")
		kb.AddButton("🎬 换一部电影", "adventure_start")
		kb.NewRow()
		kb.AddButton("🎮 游戏中心", "game_menu")

		h.telegram.SendMessage(chatID, card.Markdown, "Markdown", kb.Build())
	}
}

// sendDamageNotification 发送受伤通知 + 剩余选项
func (h *AdventureHandler) sendDamageNotification(userID int64, chatID int64, state *AdventureState, wrongIdx int, choiceResult string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Info("[Adventure] Damage notification panic: %v", r)
		}
	}()

	// 构建剩余选项的键盘
	scene := state.Scene
	if scene == nil {
		return
	}

	kb := services.NewKeyboardBuilder()
	for i, choice := range scene.Choices {
		if i == wrongIdx {
			continue // 跳过已选错的选项
		}
		kb.AddButton(choice.Text, fmt.Sprintf("adventure_choice:idx:%d", i))
	}

	// 发送受伤后的场景提示
	text := fmt.Sprintf("💥 %s\n\n❤️ -%d HP（剩余 %d%%）\n\n🎯 再试一次，仔细想想...", choiceResult, adventureWrongDmg, state.HP)

	h.telegram.SendMessage(chatID, text, "Markdown", kb.Build())
}

// sendSceneCard 发送场景卡片
func (h *AdventureHandler) sendSceneCard(chatID int64, state *AdventureState) {
	scene := state.Scene
	if scene == nil {
		return
	}

	var choices []richmessage.AdventureChoiceView
	for i, c := range scene.Choices {
		choices = append(choices, richmessage.AdventureChoiceView{
			Index: i,
			Text:  c.Text,
		})
	}

	card := richmessage.BuildAdventureSceneCard(richmessage.AdventureSceneCardData{
		MovieTitle:  state.MovieInfo.Title,
		MovieYear:   state.MovieInfo.Year,
		Genres:      state.MovieInfo.Genres,
		Level:       state.Level,
		TotalLevels: state.TotalLevels,
		SceneTitle:  scene.Title,
		Description: scene.Description,
		Choices:     choices,
		Hint:        scene.Hint,
		HP:          state.HP,
	})

	// 构建选项按钮
	kb := services.NewKeyboardBuilder()
	for i, choice := range scene.Choices {
		kb.AddButton(choice.Text, fmt.Sprintf("adventure_choice:idx:%d", i))
		if i == 1 || i == 3 { // 每行2个按钮
			kb.NewRow()
		}
	}

	h.telegram.SendMessage(chatID, card.Markdown, "Markdown", kb.Build())
}

// generateFallbackScene 生成模板场景（AI失败时的回退）
func (h *AdventureHandler) generateFallbackScene(info *services.MovieInfo, level int) *services.AdventureScene {
	scenes := map[int]*services.AdventureScene{
		1: {
			Level: 1,
			Title: "踏入未知",
			Description: fmt.Sprintf("你睁开眼睛，发现自己身处《%s》的世界。四周弥漫着%s的氛围。一个声音在你耳边响起：「你是这里的主角，但命运不会眷顾所有人。」", info.Title, strings.Join(info.Genres, "/")),
			Choices: []services.AdventureChoice{
				{Text: "小心翼翼地观察周围", Correct: false, Result: "你太谨慎了，错过了关键时机"},
				{Text: "大步向前走去", Correct: true, Result: "你展现出了主角的气魄！"},
				{Text: "找人问问情况", Correct: false, Result: "你问的人恰好是反派的眼线"},
			},
		},
		2: {
			Level: 2,
			Title: "危机降临",
			Description: fmt.Sprintf("在《%s》的世界里行进了一段路后，你遇到了第一个真正的挑战。空气都凝固了。", info.Title),
			Choices: []services.AdventureChoice{
				{Text: "正面迎战", Correct: false, Result: "冲动是魔鬼！"},
				{Text: "先撤退再想办法", Correct: false, Result: "你撤退的方向恰好是陷阱"},
				{Text: "利用环境制造机会", Correct: true, Result: "你巧妙地化险为夷！"},
			},
		},
		3: {
			Level: 3,
			Title: "终极对决",
			Description: fmt.Sprintf("终于到了最后关头。《%s》的命运就在此刻。你的每一个选择都关乎生死。", info.Title),
			Choices: []services.AdventureChoice{
				{Text: "孤注一掷", Correct: false, Result: "太冒险了！"},
				{Text: "等待最佳时机", Correct: true, Result: "你抓住了那个瞬间！"},
				{Text: "向同伴求助", Correct: false, Result: "你的同伴已经被策反了"},
			},
		},
	}

	if scene, ok := scenes[level]; ok {
		return scene
	}

	return scenes[1] // 默认返回第一关
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

// notifyGroup 发送群通知（10分钟后自毁）
func (h *AdventureHandler) notifyGroup(userName, message string) {
	if h.groupChatID == 0 || h.telegram == nil {
		return
	}
	go func() {
		text := fmt.Sprintf("⚔️ **%s** %s", userName, message)
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
