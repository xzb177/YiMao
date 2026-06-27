package handlers

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
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
	adventureBaseDmg   = 45  // 基础扣血（两次必死）
	adventureTrapDmg   = 60  // 陷阱扣血（一次半残）
	adventureBossDmg   = 70  // Boss关扣血（基本一击毙命）
	adventureComboHeal = 3   // 连击回血（微乎其微）
)

// AdventureState 冒险状态
type AdventureState struct {
	MovieInfo    *services.MovieInfo         `json:"movie_info"`
	Level        int                         `json:"level"`
	HP           int                         `json:"hp"`
	Combo        int                         `json:"combo"`
	MaxCombo     int                         `json:"max_combo"`
	Score        int                         `json:"score"`
	History      []string                    `json:"history"`
	Scene        *services.AdventureScene    `json:"scene"`
	InProgress   bool                        `json:"in_progress"`
	TotalLevels  int                         `json:"total_levels"`
	PerfectRun   bool                        `json:"perfect_run"`
	TriedChoices map[int]bool                `json:"tried_choices"`
	HintUsed     bool                        `json:"hint_used"`
	ChoiceLock   sync.Mutex                  `json:"-"` // 不序列化
}

// AdventureHandler 冒险处理器
type AdventureHandler struct {
	adventureSvc *services.AdventureService
	tmdbClient   *services.TMDBClient
	blindBoxSvc  *services.BlindBoxService
	viewingSvc   *services.ViewingHistoryService
	sessionMgr   *session.Manager
	telegram     *services.TelegramClient
	userMapping  services.UserMappingStore
	socialDB     *services.SocialDB
	groupChatID  int64

	// 冒险成功后的求片回调（由 main 注入，解耦 requestHandler）
	onAdventureSuccess func(userID int64, chatID int64, movieName string, movieYear int, tmdbID int, genres []string, score int, grade string)

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

// SetSocialDB 注入社交数据库
func (h *AdventureHandler) SetSocialDB(db *services.SocialDB) {
	h.socialDB = db
}

// SetOnAdventureSuccess 注入冒险成功回调
func (h *AdventureHandler) SetOnAdventureSuccess(fn func(userID int64, chatID int64, movieName string, movieYear int, tmdbID int, genres []string, score int, grade string)) {
	h.onAdventureSuccess = fn
}

// SetBlindBoxService 注入盲盒服务（用于通关奖励）
func (h *AdventureHandler) SetBlindBoxService(svc *services.BlindBoxService) {
	h.blindBoxSvc = svc
}

// SetViewingHistoryService 注入观影历史服务（用于个性化推荐）
func (h *AdventureHandler) SetViewingHistoryService(svc *services.ViewingHistoryService) {
	h.viewingSvc = svc
}

// ============================================================
//  持久化方法 — 冒险状态存取 SocialDB
// ============================================================

// saveState 持久化冒险状态到 SocialDB（每次状态变更时调用）
func (h *AdventureHandler) saveState(userID int64, state *AdventureState) {
	if h.socialDB == nil || state == nil {
		return
	}
	data, err := json.Marshal(state)
	if err != nil {
		logger.Info("[Adventure] 序列化状态失败 user=%d: %v", userID, err)
		return
	}
	movieName := ""
	if state.MovieInfo != nil {
		movieName = state.MovieInfo.Title
	}
	if err := h.socialDB.SaveAdventureSession(userID, string(data), movieName, state.Level, state.HP); err != nil {
		logger.Info("[Adventure] 持久化状态失败 user=%d: %v", userID, err)
	}
}

// loadState 从 SocialDB 恢复冒险状态（session 内存中没有时调用）
func (h *AdventureHandler) loadState(userID int64) *AdventureState {
	if h.socialDB == nil {
		return nil
	}
	stateJSON, err := h.socialDB.LoadAdventureSession(userID)
	if err != nil || stateJSON == "" {
		return nil
	}
	var state AdventureState
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		logger.Info("[Adventure] 反序列化状态失败 user=%d: %v", userID, err)
		return nil
	}
	// 重建 mutex（不可序列化）
	state.ChoiceLock = sync.Mutex{}
	// TriedChoices 可能是 nil
	if state.TriedChoices == nil {
		state.TriedChoices = make(map[int]bool)
	}
	return &state
}

// removeState 清除持久化的冒险状态（冒险结束/退出时调用）
func (h *AdventureHandler) removeState(userID int64) {
	if h.socialDB == nil {
		return
	}
	if err := h.socialDB.DeleteAdventureSession(userID); err != nil {
		logger.Info("[Adventure] 删除持久化状态失败 user=%d: %v", userID, err)
	}
}

// getOrRestoreState 优先从 session 内存获取，没有则从 DB 恢复
func (h *AdventureHandler) getOrRestoreState(userID int64, sess *session.Session) *AdventureState {
	if sess == nil {
		return h.loadState(userID)
	}
	// 先尝试内存
	if state, ok := sess.Get("adventure_state"); ok {
		if advState, ok := state.(*AdventureState); ok && advState.InProgress {
			return advState
		}
	}
	// 内存没有，尝试从 DB 恢复
	restored := h.loadState(userID)
	if restored != nil && restored.InProgress {
		// 写回 session
		sess.Set("adventure_state", restored)
		logger.Info("[Adventure] 从DB恢复冒险状态 user=%d movie=%s level=%d hp=%d",
			userID, restored.MovieInfo.Title, restored.Level, restored.HP)
		return restored
	}
	return nil
}

// Handle 冒险回调路由
func (h *AdventureHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	action := string(ctx.Callback.Action)

	switch action {
	case "adventure_start":
		return h.handleStart(ctx)
	case "adventure_choice":
		return h.handleChoice(ctx)
	case "adventure_hint":
		return h.handleHint(ctx)
	case "adventure_retry":
		return h.handleRetry(ctx)
	case "adventure_quit":
		return h.handleQuit(ctx)
	case "adventure_share":
		return h.handleShare(ctx)
	default:
		return nil, fmt.Errorf("unknown adventure action: %s", action)
	}
}

// HandleAdventureText 处理文本输入中的电影名
func (h *AdventureHandler) HandleAdventureText(userID int64, chatID int64, movieName string) bool {
	if h.adventureSvc == nil || h.sessionMgr == nil {
		return false
	}

	// 输入净化：限制长度，去除危险字符
	movieName = strings.TrimSpace(movieName)
	if len([]rune(movieName)) < 1 || len([]rune(movieName)) > 100 {
		h.telegram.SendMessage(chatID, "❌ 电影名太长或为空，请重新输入", "", nil)
		return true
	}
	// 去除可能导致prompt注入的字符
	movieName = strings.NewReplacer(
		"\n", " ", "\r", "", "	", " ",
		"```", "", "~~~", "",
		"{{", "", "}}", "",
	).Replace(movieName)

	sess := h.sessionMgr.GetOrCreate(userID)
	if sess == nil {
		return false
	}
	if _, exists := sess.Get("pending_adventure_input"); !exists {
		return false
	}
	// 消耗 pending 状态（无论后续是否成功，都清除）
	sess.Delete("pending_adventure_input")

	// 二次校验：如果已经有进行中的冒险，不重复启动（内存+DB双重检查）
	if advState := h.getOrRestoreState(userID, sess); advState != nil {
		h.telegram.SendMessage(chatID, "⚠️ 你已经有一场进行中的冒险了", "", nil)
		return true
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
	h.removeState(ctx.UserID) // 清除持久化
	return &callback.Response{
		Text: "⚔️ **求片大冒险**\n\n请发送你想求的电影/剧集名称\n\n例如：`流浪地球` 或 `权力的游戏`\n\n⚠️ 只有通关才能提交求片请求\n每关4个选项，两次失误即死\n通关率不到 10%",
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
	if choiceIdx < 0 || choiceIdx > 3 {
		return &callback.Response{CallbackMsg: "❌ 无效选项", ShowAlert: true}, nil
	}

	sess := h.sessionMgr.GetOrCreate(ctx.UserID)
	if sess == nil {
		return &callback.Response{CallbackMsg: "❌ 会话异常", ShowAlert: true}, nil
	}

	advState := h.getOrRestoreState(ctx.UserID, sess)
	if advState == nil {
		return &callback.Response{CallbackMsg: "❌ 没有进行中的冒险，请先开始", ShowAlert: true}, nil
	}

	// 防并发刷分
	advState.ChoiceLock.Lock()
	defer advState.ChoiceLock.Unlock()

	scene := advState.Scene
	if scene == nil || choiceIdx >= len(scene.Choices) {
		return &callback.Response{CallbackMsg: "❌ 无效选项", ShowAlert: true}, nil
	}

	// 防作弊：检查是否已经选过这个选项
	if advState.TriedChoices != nil && advState.TriedChoices[choiceIdx] {
		return &callback.Response{CallbackMsg: "⚠️ 你已经选过这个了，试试别的吧", ShowAlert: true}, nil
	}
	// 记录已试选项
	if advState.TriedChoices == nil {
		advState.TriedChoices = make(map[int]bool)
	}
	advState.TriedChoices[choiceIdx] = true

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
		// 背水一战：HP≤20时选对，额外回血+5
		lastStandBonus := 0
		if advState.HP <= 20+lastStandBonus {
			advState.HP += 5
			lastStandBonus = 5
			if advState.HP > adventureMaxHP {
				advState.HP = adventureMaxHP
			}
		}
		// 计分
		baseScore := advState.Level * 10
		comboBonus := advState.Combo * 5
		advState.Score += baseScore + comboBonus + lastStandBonus
		if advState.Score > 100 {
			advState.Score = 100
		}

		advState.Level++

		// 新关卡重置已试选项和提示状态
		advState.TriedChoices = make(map[int]bool)
		advState.HintUsed = false

		if advState.Level > advState.TotalLevels {
			// 🏆 通关！
			advState.InProgress = false
			advState.Score += advState.HP // 剩余HP作为额外分
			if advState.Score > 100 {
				advState.Score = 100
			}
			sess.Set("adventure_state", advState)
			h.removeState(ctx.UserID) // 通关，清除持久化
			go h.finishAdventureAsync(ctx.UserID, ctx.ChatID, advState, true)

			return &callback.Response{
				CallbackMsg: fmt.Sprintf("✅ %s\n🔥 连击 x%d！进入最终决战...", choice.Result, advState.Combo),
				ShowAlert:   false,
			}, nil
		}

		sess.Set("adventure_state", advState)
		h.saveState(ctx.UserID, advState) // 持久化

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
	// 背水一战：HP≤20时选错，直接毙命（不给苟延残喘的机会）
	if advState.HP <= 20 && damage < advState.HP {
		damage = advState.HP // 确保一击毙命
	}

	advState.HP -= damage
	advState.Combo = 0 // 连击归零
	advState.History = append(advState.History, fmt.Sprintf("(-%dHP)", damage))

	if advState.HP <= 0 {
		// 💀 死亡
		advState.HP = 0
		advState.InProgress = false
		sess.Set("adventure_state", advState)
		h.removeState(ctx.UserID) // 死亡，清除持久化
		go h.finishAdventureAsync(ctx.UserID, ctx.ChatID, advState, false)
		return &callback.Response{
			CallbackMsg: fmt.Sprintf("💀 %s\n生命耗尽...", choice.Result),
			ShowAlert:   false,
		}, nil
	}

	// 还活着
	sess.Set("adventure_state", advState)
	h.saveState(ctx.UserID, advState) // 持久化
	go h.sendDamageCard(ctx.UserID, ctx.ChatID, advState, damage, choice.Result)

	return &callback.Response{
		CallbackMsg: fmt.Sprintf("💥 %s\n❤️ -%d HP（剩余 %d%%）", choice.Result, damage, advState.HP),
		ShowAlert:   false,
	}, nil
}

// handleHint 「问导演」— 花10HP换一条精准线索
const hintCost = 10

func (h *AdventureHandler) handleHint(ctx *callback.Context) (*callback.Response, error) {
	if h.sessionMgr == nil {
		return &callback.Response{CallbackMsg: "❌ 服务未就绪", ShowAlert: true}, nil
	}

	sess := h.sessionMgr.GetOrCreate(ctx.UserID)
	if sess == nil {
		return &callback.Response{CallbackMsg: "❌ 会话异常", ShowAlert: true}, nil
	}

	advState := h.getOrRestoreState(ctx.UserID, sess)
	if advState == nil {
		return &callback.Response{CallbackMsg: "❌ 没有进行中的冒险", ShowAlert: true}, nil
	}

	advState.ChoiceLock.Lock()
	defer advState.ChoiceLock.Unlock()

	// 检查是否已用过提示
	if advState.HintUsed {
		return &callback.Response{CallbackMsg: "🎬 导演已经给过你提示了，这关只能靠自己", ShowAlert: true}, nil
	}

	// 检查HP是否够
	if advState.HP <= hintCost {
		return &callback.Response{CallbackMsg: fmt.Sprintf("💔 生命值不足（需要%dHP），导演不敢再消耗你了", hintCost), ShowAlert: true}, nil
	}

	// 扣HP
	advState.HP -= hintCost
	advState.HintUsed = true
	advState.PerfectRun = false // 用提示不算完美
	advState.Score -= 5         // 扣分
	if advState.Score < 0 {
		advState.Score = 0
	}

	// 生成导演提示
	scene := advState.Scene
	if scene == nil {
		return &callback.Response{CallbackMsg: "❌ 场景异常", ShowAlert: true}, nil
	}

	// 找到正确选项
	correctIdx := -1
	for i, c := range scene.Choices {
		if c.Correct {
			correctIdx = i
			break
		}
	}

	// 生成不同层次的提示（不直接给答案）
	var hintMsg string
	if correctIdx >= 0 {
		// 消除法：随机排除一个错误选项（非陷阱）
		var excludeList []int
		for i, c := range scene.Choices {
			if !c.Correct && !c.IsTrap && i != correctIdx {
				excludeList = append(excludeList, i)
			}
		}
		excludeMsg := ""
		if len(excludeList) > 0 {
			excludeIdx := excludeList[rand.Intn(len(excludeList))]
			numbers := []string{"1️⃣", "2️⃣", "3️⃣", "4️⃣"}
			excludeMsg = fmt.Sprintf("\n\n❌ 导演说：%s 可以排除", numbers[excludeIdx])
		}

		hintMsg = fmt.Sprintf("🎬 导演的耳语（-%dHP）\n\n💡 %s%s", hintCost, scene.Hint, excludeMsg)
	} else {
		hintMsg = fmt.Sprintf("🎬 导演的耳语（-%dHP）\n\n💡 %s", hintCost, scene.Hint)
	}

	sess.Set("adventure_state", advState)
	h.saveState(ctx.UserID, advState) // 持久化

	// 发送提示 + 更新场景卡片
	go func() {
		h.telegram.SendMessage(ctx.ChatID, hintMsg, "", nil)
		// 重新发送场景卡片（更新HP和按钮状态）
		h.sendSceneCard(ctx.ChatID, advState)
	}()

	return &callback.Response{
		CallbackMsg: fmt.Sprintf("🎬 导演给了提示\n❤️ -%dHP（剩余 %d%%）", hintCost, advState.HP),
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

	// 获取旧的电影信息用于重试（优先内存，其次DB）
	var movieName string
	if state, ok := sess.Get("adventure_state"); ok {
		if advState, ok := state.(*AdventureState); ok && advState.MovieInfo != nil {
			movieName = advState.MovieInfo.Title
		}
	}
	if movieName == "" {
		// 尝试从DB恢复电影名
		if restored := h.loadState(ctx.UserID); restored != nil && restored.MovieInfo != nil {
			movieName = restored.MovieInfo.Title
		}
	}

	// 无条件清除所有旧状态
	sess.Delete("adventure_state")
	sess.Delete("pending_adventure_input")
	h.removeState(ctx.UserID) // 清除持久化

	if movieName == "" {
		return h.handleStart(ctx)
	}

	go h.startAdventureAsync(ctx.UserID, ctx.ChatID, movieName)
	return &callback.Response{
		CallbackMsg: fmt.Sprintf("🔄 重新挑战《%s》...", movieName),
		ShowAlert:   false,
	}, nil
}

// handleShare 分享战绩到群
func (h *AdventureHandler) handleShare(ctx *callback.Context) (*callback.Response, error) {
	if h.sessionMgr == nil || h.groupChatID == 0 {
		return &callback.Response{CallbackMsg: "❌ 分享功能未就绪", ShowAlert: true}, nil
	}

	sess := h.sessionMgr.GetOrCreate(ctx.UserID)
	if sess == nil {
		return &callback.Response{CallbackMsg: "❌ 会话异常", ShowAlert: true}, nil
	}

	state, ok := sess.Get("adventure_state")
	if !ok {
		return &callback.Response{CallbackMsg: "❌ 没有冒险记录", ShowAlert: true}, nil
	}

	advState, ok := state.(*AdventureState)
	if !ok {
		return &callback.Response{CallbackMsg: "❌ 数据异常", ShowAlert: true}, nil
	}

	// 只有已结束的冒险才能分享
	if advState.InProgress {
		return &callback.Response{CallbackMsg: "⏳ 冒险还没结束呢，先通关再说！", ShowAlert: true}, nil
	}

	userName := h.getUserName(ctx.UserID)

	// 构建炫耀卡（不泄露Scene.Choices中的正确答案信息）
	shareCard := richmessage.BuildAdventureShareCard(richmessage.AdventureShareCardData{
		UserName:   userName,
		MovieTitle: advState.MovieInfo.Title,
		MovieYear:  advState.MovieInfo.Year,
		Score:      advState.Score,
		HP:         advState.HP,
		MaxCombo:   advState.MaxCombo,
		PerfectRun: advState.PerfectRun,
		Success:    !advState.InProgress && advState.HP > 0,
		Level:      advState.Level - 1,
		TotalLevels: advState.TotalLevels,
	})

	// 发送到群
	go func() {
		sent, err := h.telegram.SendMessage(h.groupChatID, shareCard.Markdown, "Markdown", nil)
		if err != nil {
			return
		}
		// 10分钟后自毁
		go func(chatID int64, msgID int64) {
			time.Sleep(10 * time.Minute)
			_ = h.telegram.DeleteMessage(chatID, msgID)
		}(h.groupChatID, sent.MessageID)
	}()

	return &callback.Response{
		CallbackMsg: "📢 战绩已分享到群！",
		ShowAlert:   false,
	}, nil
}
func (h *AdventureHandler) handleQuit(ctx *callback.Context) (*callback.Response, error) {
	// 无条件清除所有冒险相关状态
	if h.sessionMgr != nil {
		sess := h.sessionMgr.GetOrCreate(ctx.UserID)
		if sess != nil {
			sess.Delete("adventure_state")
			sess.Delete("pending_adventure_input")
		}
	}
	h.removeState(ctx.UserID) // 清除持久化

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

	// 超时清理：120秒后强制清除generating标记（90s超时+重试=最多180s，留余量）
	go func(uid int64) {
		time.Sleep(120 * time.Second)
		h.mu.Lock()
		if h.generating[uid] {
			delete(h.generating, uid)
			logger.Info("[Adventure] Force cleaned generating flag for user %d (timeout)", uid)
		}
		h.mu.Unlock()
	}(userID)

	loadingMsg, _ := h.telegram.SendMessage(chatID, "⚔️ 正在进入「"+movieName+"」的世界...", "", nil)

	// 清除旧的冒险状态（防止"已过期"错误）
	if h.sessionMgr != nil {
		sess := h.sessionMgr.GetOrCreate(userID)
		if sess != nil {
			sess.Delete("adventure_state")
			sess.Delete("pending_adventure_input")
		}
	}
	h.removeState(userID) // 清除旧的持久化记录

	movieInfo, err := h.adventureSvc.SearchMovieInfo(movieName)
	if err != nil {
		if loadingMsg != nil {
			h.telegram.DeleteMessage(chatID, loadingMsg.MessageID)
		}
		h.telegram.SendMessage(chatID, fmt.Sprintf("❌ 找不到这部电影\n%s\n\n试试其他名字？", err.Error()), "", nil)
		return
	}

	// 发送入口卡片（纯展示，无按钮）
	entryCard := richmessage.BuildAdventureEntryCard(richmessage.AdventureEntryCardData{
		MovieTitle: movieInfo.Title,
		MovieYear:  movieInfo.Year,
		Genres:     movieInfo.Genres,
		Overview:   movieInfo.Overview,
		Rating:     movieInfo.Rating,
	})

	if loadingMsg != nil {
		h.telegram.DeleteMessage(chatID, loadingMsg.MessageID)
	}
	h.telegram.SendMessage(chatID, entryCard.Markdown, "Markdown", nil)

	// 生成第一关（带随机冷知识加载提示）
	tip := randomMovieTip()
	tipMsg, _ := h.telegram.SendMessage(chatID, fmt.Sprintf("⏳ 正在构造第 1 关...\n\n%s", tip), "", nil)

	// 生成第一关
	scene, err := h.adventureSvc.GenerateScene(movieInfo, 1, adventureMaxLevels, nil, adventureMaxHP)
	if err != nil {
		logger.Info("[Adventure] AI scene gen failed, using fallback: %v", err)
		scene = h.adventureSvc.GenerateFallbackScene(movieInfo, 1, adventureMaxLevels)
	}
	scene.TotalLevels = adventureMaxLevels

	// 删除加载提示
	if tipMsg != nil {
		h.telegram.DeleteMessage(chatID, tipMsg.MessageID)
	}

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
		TriedChoices: make(map[int]bool),
	}

	if h.sessionMgr != nil {
		sess := h.sessionMgr.GetOrCreate(userID)
		if sess != nil {
			sess.Set("adventure_state", state)
		}
	}
	h.saveState(userID, state) // 持久化

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
	tip := randomMovieTip()
	loadingMsg, _ := h.telegram.SendMessage(chatID, fmt.Sprintf("⏳ 正在构造第 %d 关...\n\n%s", state.Level, tip), "", nil)

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
	h.saveState(userID, state) // 持久化

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

	// 收集剩余选项（包含正确答案，只排除已经选过的）
	var remainingChoices []richmessage.AdventureChoiceView
	if !isDead && state.Scene != nil {
		for i, c := range state.Scene.Choices {
			if c.Text != "" {
				remainingChoices = append(remainingChoices, richmessage.AdventureChoiceView{Index: i, Text: c.Text})
			}
		}
	}

	// 死亡时提取正确答案
	correctAnswer := ""
	correctReason := ""
	if isDead && state.Scene != nil {
		for _, c := range state.Scene.Choices {
			if c.Correct {
				correctAnswer = c.Text
				correctReason = c.Result
				break
			}
		}
	}

	card := richmessage.BuildAdventureDamageCard(richmessage.AdventureDamageCardData{
		ChoiceResult: choiceResult,
		Damage:       damage,
		HP:           state.HP,
		Level:        state.Level,
		TotalLevels:  state.TotalLevels,
		Combo:        state.Combo,
		Score:        state.Score,
		IsDead:       isDead,
		RemainingChoices: remainingChoices,
		TriedChoices: state.TriedChoices,
		CorrectAnswer: correctAnswer,
		CorrectReason: correctReason,
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

	// 还活着 — 显示剩余选项让用户继续选（排除已试过的）
	scene := state.Scene
	if scene == nil {
		return
	}

	kb := services.NewKeyboardBuilder()
	numbers := []string{"1️⃣", "2️⃣", "3️⃣", "4️⃣"}
	hasRemaining := false
	for i, c := range scene.Choices {
		if c.Text == "" {
			continue
		}
		// 跳过已经试过的选项
		if state.TriedChoices != nil && state.TriedChoices[i] {
			continue
		}
		num := fmt.Sprintf("#%d", i+1)
		if i < len(numbers) {
			num = numbers[i]
		}
		kb.AddButton(num, fmt.Sprintf("adventure_choice:idx:%d", i))
		hasRemaining = true
	}
	kb.NewRow()
	kb.AddButton("🚪 退出冒险", "adventure_quit")

	// 如果所有选项都试过了（理论上不可能，但防御性编程）
	if !hasRemaining {
		kb.AddButton("🔄 重新开始", "adventure_retry")
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

	// 保存冒险记录到数据库
	if h.socialDB != nil {
		userName := h.getUserName(userID)
		_ = h.socialDB.SaveAdventureRecord(
			userID, userName,
			state.MovieInfo.Title, state.MovieInfo.Year,
			result.Score, result.Grade,
			state.MaxCombo, state.HP,
			state.Level-1, state.TotalLevels,
			state.PerfectRun, success,
		)
	}

	// 通关 → 自动提交求片请求
	if success && h.onAdventureSuccess != nil {
		go h.onAdventureSuccess(
			userID, chatID,
			state.MovieInfo.Title, state.MovieInfo.Year,
			state.MovieInfo.TMDBID, state.MovieInfo.Genres,
			result.Score, result.Grade,
		)
	}

	// 群通知：荣耀播报（稀缺性 = 更有面子）
	userName := h.getUserName(userID)
	if success {
		shouldNotify := false
		notifyMsg := ""
		switch {
		case result.Grade == "SSS":
			shouldNotify = true
			if state.PerfectRun {
				notifyMsg = fmt.Sprintf(`👑━━━━━━━━━━━━━━━━━━━━━━━━━━👑

⚡ 传说诞生 ⚡

🏆 %s
🏆 《%s》(%d)

━━━━━━━━━━━━━━━━━━━━━━━━━━

🎯 %d分  ❤️ 满血  🔥 x%d 连击
🛡️ 全程无伤  ⚔️ 五关全通

━━━━━━━━━━━━━━━━━━━━━━━━━━

💯 SSS · 完美通关
零失误 · 零扣血 · 无可挑剔

👑━━━━━━━━━━━━━━━━━━━━━━━━━━👑`,
					userName, state.MovieInfo.Title, state.MovieInfo.Year,
					result.Score, state.MaxCombo)
			} else {
				notifyMsg = fmt.Sprintf(`👑━━━━━━━━━━━━━━━━━━━━━━━━━━👑

⚡ 传奇操作 ⚡

🏆 %s
🏆 《%s》(%d)

━━━━━━━━━━━━━━━━━━━━━━━━━━

🎯 %d分  ❤️ %d%%  🔥 x%d 连击

━━━━━━━━━━━━━━━━━━━━━━━━━━

💯 SSS · 近乎完美
这部电影他不只是看过——他活过

👑━━━━━━━━━━━━━━━━━━━━━━━━━━👑`,
					userName, state.MovieInfo.Title, state.MovieInfo.Year,
					result.Score, state.HP, state.MaxCombo)
			}

		case result.Grade == "SS":
			shouldNotify = true
			notifyMsg = fmt.Sprintf(`💎━━━━━━━━━━━━━━━━━━━━━━━━━━💎

⚡ 王者通关 ⚡

💎 %s
💎 《%s》(%d)

━━━━━━━━━━━━━━━━━━━━━━━━━━

🎯 %d分  ❤️ %d%%  🔥 x%d 连击

━━━━━━━━━━━━━━━━━━━━━━━━━━

🥈 SS · 距离传说一步之遥
五关险境，他几乎毫发无伤地走了出来

💎━━━━━━━━━━━━━━━━━━━━━━━━━━💎`,
				userName, state.MovieInfo.Title, state.MovieInfo.Year,
				result.Score, state.HP, state.MaxCombo)

		case state.PerfectRun:
			shouldNotify = true
			notifyMsg = fmt.Sprintf(`🛡️━━━━━━━━━━━━━━━━━━━━━━━━━━🛡️

⚡ 无伤传说 ⚡

🛡️ %s
🛡️ 《%s》(%d)

━━━━━━━━━━━━━━━━━━━━━━━━━━

🎯 %d分  ❤️ 100%%  🛡️ 全程零失误

━━━━━━━━━━━━━━━━━━━━━━━━━━

五关全过，一滴血没掉
这个人看电影是认真的

🛡️━━━━━━━━━━━━━━━━━━━━━━━━━━🛡️`,
				userName, state.MovieInfo.Title, state.MovieInfo.Year, result.Score)

		case state.MaxCombo >= 4:
			shouldNotify = true
			notifyMsg = fmt.Sprintf(`🔥━━━━━━━━━━━━━━━━━━━━━━━━━━🔥

⚡ 连击风暴 ⚡

🔥 %s
🔥 《%s》(%d)

━━━━━━━━━━━━━━━━━━━━━━━━━━

🎯 %d分  ❤️ %d%%  🔥 x%d 连击

━━━━━━━━━━━━━━━━━━━━━━━━━━

连续%d关一选即中
这部电影他真的看过

🔥━━━━━━━━━━━━━━━━━━━━━━━━━━🔥`,
				userName, state.MovieInfo.Title, state.MovieInfo.Year,
				result.Score, state.HP, state.MaxCombo, state.MaxCombo)

		// 低门槛触发：新人首通（优先级低于SSS/SS/无伤/x4+）
		case h.socialDB != nil && h.socialDB.IsFirstSuccess(userID):
			shouldNotify = true
			notifyMsg = fmt.Sprintf(`🌟━━━━━━━━━━━━━━━━━━━━━━━━━━🌟

⚡ 新星降临 ⚡

🌟 %s
🌟 《%s》(%d)

━━━━━━━━━━━━━━━━━━━━━━━━━━

🎯 %d分  ❤️ %d%%  🔥 x%d 连击

━━━━━━━━━━━━━━━━━━━━━━━━━━

🎬 首次通关，敲响求片之门
从此，冒险世界多了一位勇者

🌟━━━━━━━━━━━━━━━━━━━━━━━━━━🌟`,
				userName, state.MovieInfo.Title, state.MovieInfo.Year,
				result.Score, state.HP, state.MaxCombo)

		// 低门槛触发：本周首通（优先级低于新人首通）
		case h.socialDB != nil && h.socialDB.IsFirstSuccessThisWeek(userID):
			shouldNotify = true
			notifyMsg = fmt.Sprintf(`🌅━━━━━━━━━━━━━━━━━━━━━━━━━━🌅

⚡ 本周第一道曙光 ⚡

🌅 %s
🌅 《%s》(%d)

━━━━━━━━━━━━━━━━━━━━━━━━━━

🎯 %d分  ❤️ %d%%  🔥 x%d 连击

━━━━━━━━━━━━━━━━━━━━━━━━━━

打破沉默，这周的第一场胜利
冒险的火种重新点燃

🌅━━━━━━━━━━━━━━━━━━━━━━━━━━🌅`,
				userName, state.MovieInfo.Title, state.MovieInfo.Year,
				result.Score, state.HP, state.MaxCombo)
		}
		if shouldNotify {
			h.notifyGroup(userName, notifyMsg)
		}
	} else if state.Level-1 >= 4 {
		h.notifyGroup(userName, fmt.Sprintf(`💀━━━━━━━━━━━━━━━━━━━━━━━━━━💀

⚡ 惜败 · 差一步 ⚡

💀 %s
💀 《%s》(%d)

━━━━━━━━━━━━━━━━━━━━━━━━━━

倒在第 %d/%d 关

━━━━━━━━━━━━━━━━━━━━━━━━━━

他已经看到了终点的光...
却在最后一刻倒下了
要不要帮他一把？

💀━━━━━━━━━━━━━━━━━━━━━━━━━━💀`,
			userName, state.MovieInfo.Title, state.MovieInfo.Year,
			state.Level-1, state.TotalLevels))
	}

	// 发送结果卡片
	if success {
		// 随机彩蛋奖励
		bonusEffect := generateBonusEffect(result.Grade, state.PerfectRun)

		// 基于观影历史的个性化推荐
		recommendation := h.generateRecommendation(userID, state.MovieInfo)

		card := richmessage.BuildAdventureSuccessCard(richmessage.AdventureSuccessCardData{
			MovieTitle:     state.MovieInfo.Title,
			MovieYear:      state.MovieInfo.Year,
			Genres:         state.MovieInfo.Genres,
			Score:          result.Score,
			Grade:          result.Grade,
			FinalScene:     result.FinalScene,
			EasterEgg:      result.EasterEgg,
			Stats:          result.Stats,
			HP:             state.HP,
			MaxCombo:       state.MaxCombo,
			BonusEffect:    bonusEffect,
			Recommendation: recommendation,
		})

		kb := services.NewKeyboardBuilder()
		kb.AddButton("📢 分享战绩", "adventure_share")
		kb.AddButton("🔄 再挑战一次", "adventure_retry")
		kb.NewRow()
		kb.AddButton("🎰 通关盲盒", "game_blindbox")
		kb.AddButton("🎮 游戏中心", "game_menu")

		h.telegram.SendMessage(chatID, card.Markdown, "Markdown", kb.Build())

		// 通关奖励：免费开盲盒
		go h.sendRewardBlindBox(chatID, state, result.Grade)
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
		kb.AddButton("📢 分享战绩", "adventure_share")
		kb.AddButton("🔄 我知道答案了！", "adventure_retry")
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
		case state.Combo >= 6:
			lastResult = fmt.Sprintf("🔥🔥🔥🔥 六连神话！x%d 连击！你是怎么看的？！", state.Combo)
		case state.Combo == 5:
			lastResult = "🔥🔥🔥 五连绝世！这部电影你倒背如流吧？"
		case state.Combo == 4:
			lastResult = "🔥🔥 四连超凡！你是不是提前看了剧本？"
		case state.Combo == 3:
			lastResult = "🔥 三连破敌！手感来了！"
		case state.Combo == 2:
			lastResult = "⚡ 双连命中！继续保持"
		default:
			lastResult = "✅ 上一关正确"
		}
	}

	// 生成心理学数据（社交证明 + 时间压力）
	deathRate, optionStats, timeUrgency := generatePsychoData(state.Level, state.TotalLevels, len(scene.Choices))

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
		DeathRate:    deathRate,
		OptionStats:  optionStats,
		TimeUrgency:  timeUrgency,
	})

	kb := services.NewKeyboardBuilder()
	numbers := []string{"1️⃣", "2️⃣", "3️⃣", "4️⃣"}
	for i := range scene.Choices {
		num := fmt.Sprintf("#%d", i+1)
		if i < len(numbers) {
			num = numbers[i]
		}
		kb.AddButton(num, fmt.Sprintf("adventure_choice:idx:%d", i))
	}
	kb.NewRow()
	// 问导演按钮（每关限用一次，花10HP）
	if !state.HintUsed && state.HP > hintCost {
		kb.AddButton("🎬 问导演 (-10HP)", "adventure_hint")
	}
	kb.AddButton("🚪 退出", "adventure_quit")

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
		sent, err := h.telegram.SendMessage(h.groupChatID, message, "Markdown", nil)
		if err != nil {
			// Markdown 失败时回退纯文本
			sent, err = h.telegram.SendMessage(h.groupChatID, message, "", nil)
			if err != nil {
				return
			}
		}
		go func(chatID int64, msgID int64) {
			time.Sleep(10 * time.Minute)
			_ = h.telegram.DeleteMessage(chatID, msgID)
		}(h.groupChatID, sent.MessageID)
	}()
}

// sendRewardBlindBox 通关奖励：免费开盲盒

// generatePsychoData 生成心理学数据（社交证明 + 时间压力）
func generatePsychoData(level, totalLevels, choiceCount int) (deathRate, optionStats, timeUrgency string) {
	// 死亡率：关卡越高死亡率越高（模拟数据，但要看起来真实）
	deathRates := map[int]string{
		1: "💀 32% 的人死在这一关",
		2: "💀 51% 的人死在这一关",
		3: "💀 68% 的人死在这一关",
		4: "💀 81% 的人死在这一关",
		5: "💀 89% 的人死在这一关",
	}
	deathRate = deathRates[level]
	if deathRate == "" {
		deathRate = fmt.Sprintf("💀 %d%% 的人死在这一关", 50+level*8)
	}

	// 选项分布：正确选项的被选率最低（模拟数据）
	// 正确选项通常是第3个（index 2），被选率最低
	distributions := map[int]string{
		1: "📊 选项分布：A 35% | B 25% | C 15% | D 25%",
		2: "📊 选项分布：A 28% | B 32% | C 18% | D 22%",
		3: "📊 选项分布：A 25% | B 30% | C 20% | D 25%",
		4: "📊 选项分布：A 30% | B 25% | C 22% | D 23%",
		5: "📊 选项分布：A 28% | B 27% | C 23% | D 22%",
	}
	optionStats = distributions[level]

	// 时间压力：关卡越高时间越短
	timeLimits := map[int]string{
		1: "⏱️ 建议思考时间：60秒",
		2: "⏱️ 建议思考时间：45秒",
		3: "⚡ 建议思考时间：30秒",
		4: "⚡ 建议思考时间：20秒",
		5: "🔥 建议思考时间：15秒",
	}
	timeUrgency = timeLimits[level]

	return
}

// randomMovieTip 随机电影冷知识（加载时展示）
func randomMovieTip() string {
	tips := []string{
		"🎬 《盗梦空间》的陀螺其实有倒下的声音，诺兰故意没让观众听到",
		"🎬 《肖申克的救赎》原著小说只有100页，电影加了大量细节",
		"🎬 《泰坦尼克号》里老年Rose的照片全是导演老婆年轻时的照片",
		"🎬 《黑客帝国》的绿色代码其实是日本寿司食谱",
		"🎬 《星际穿越》的虫洞方程式是基普·索恩亲自算的",
		"🎬 《教父》马头场景用的是真马头，不是道具",
		"🎬 《阿甘正传》里所有历史影像都是后期合成的",
		"🎬 《搏击俱乐部》里每隔几分钟都会闪现一帧Tyler",
		"🎬 《沉默的羔羊》安东尼·霍普金斯只出场16分钟就拿了奥斯卡",
		"🎬 《楚门的世界》全片只有一个真实外景——最后的海",
		"🎬 90%的电影配乐师都说：最难配的不是恐怖片，而是喜剧片",
		"🎬 希区柯克在《惊魂记》里故意把巧克力酱当血浆用",
		"🎬 《星球大战》光剑的声音其实是电视机噪音+放映机马达声",
		"🎬 电影里打耳光的声音通常是拍手+拍大腿合成的",
		"🎬 《侏罗纪公园》恐龙的吼声是乌龟、老虎、大象混音出来的",
	}
	return tips[rand.Intn(len(tips))]
}

// generateBonusEffect 生成随机彩蛋奖励
func generateBonusEffect(grade string, perfectRun bool) string {
	// 基于评级的彩蛋池
	bonusPool := []string{
		"🎁 恭喜获得「剧情先知」称号！",
		"🎬 解锁隐藏电影推荐：《穆赫兰道》",
		"⚡ 下次冒险双倍积分已激活！",
		"🔮 你获得了「导演视角」——下次冒险可查看一关的正确答案",
		"🎭 解锁「电影达人」成就！",
		"💎 获得稀有盲盒券一张！",
		"🌟 你的名字将出现在本周冒险周报中",
		"🎬 解锁彩蛋电影：《彗星来的那一夜》",
		"⚡ 获得「连击大师」光环——下次冒险连击回血翻倍",
		"🔮 解锁「剧透之眼」——下次冒险每关多一次提示机会",
	}

	// SSS评级有特殊彩蛋
	if grade == "SSS" {
		ssrPool := []string{
			"👑 SSS专属：解锁「传说挑战者」称号，永久显示！",
			"👑 SSS专属：获得「无限盲盒」——下次通关可开5个盲盒！",
			"👑 SSS专属：你的通关记录将被刻入「冒险名人堂」！",
		}
		bonusPool = append(bonusPool, ssrPool...)
	}

	// 完美通关有额外彩蛋
	if perfectRun {
		perfectPool := []string{
			"🛡️ 完美通关专属：解锁「无伤战神」称号！",
			"🛡️ 完美通关专属：获得「金手指」——下次冒险可跳过一关！",
		}
		bonusPool = append(bonusPool, perfectPool...)
	}

	// 30%概率触发彩蛋
	if len(bonusPool) == 0 {
		return ""
	}
	if rand.Intn(100) >= 30 {
		return ""
	}

	return bonusPool[rand.Intn(len(bonusPool))]
}

// generateRecommendation 基于观影历史生成个性化推荐
func (h *AdventureHandler) generateRecommendation(userID int64, movieInfo *services.MovieInfo) string {
	if h.viewingSvc == nil || h.userMapping == nil {
		return ""
	}

	// 获取用户Emby名
	mpName, err := h.userMapping.GetMoviePilotUsername(userID)
	if err != nil || mpName == "" {
		return ""
	}

	// 获取Emby用户ID
	embyUserID, err := h.viewingSvc.FindEmbyUserByName(mpName)
	if err != nil || embyUserID == "" {
		return ""
	}

	// 获取观影画像
	profile, err := h.viewingSvc.GetProfile(embyUserID, mpName)
	if err != nil || len(profile.TopGenres) == 0 {
		return ""
	}

	// 生成推荐：基于用户最喜欢的类型 + 刚通关的电影类型
	topGenre := profile.TopGenres[0].Genre
	adventureGenre := ""
	if len(movieInfo.Genres) > 0 {
		adventureGenre = movieInfo.Genres[0]
	}

	if topGenre == adventureGenre {
		return fmt.Sprintf("你好像很喜欢%s片——试试同类型的其他经典？", topGenre)
	} else if topGenre != "" && adventureGenre != "" {
		return fmt.Sprintf("你平时爱看%s片，今天挑战了%s片——要不要试试两者结合的？", topGenre, adventureGenre)
	} else if topGenre != "" {
		return fmt.Sprintf("根据你的观影记录，你最喜欢%s片——下次挑战这个类型？", topGenre)
	}
	return ""
}

func (h *AdventureHandler) sendRewardBlindBox(chatID int64, state *AdventureState, grade string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Info("[Adventure] Reward blindbox panic: %v", r)
		}
	}()

	if h.blindBoxSvc == nil {
		return
	}

	// 根据评级决定开几个盲盒 + 稀有度保底
	boxCount := 1
	minRarity := "N"
	switch grade {
	case "SSS":
		boxCount = 3
		minRarity = "SR"
	case "SS":
		boxCount = 2
		minRarity = "R"
	case "S":
		boxCount = 2
		minRarity = "R"
	case "A":
		boxCount = 1
		minRarity = "N"
	default:
		boxCount = 1
		minRarity = "N"
	}

	// 用电影类型开盲盒
	genre := ""
	if len(state.MovieInfo.Genres) > 0 {
		genre = state.MovieInfo.Genres[0]
	}

	items, err := h.blindBoxSvc.OpenBlindBox(genre, boxCount)
	if err != nil {
		logger.Info("[Adventure] Reward blindbox failed: %v", err)
		return
	}

	// 保底稀有度：如果开出来的低于保底，升级
	rarityOrder := map[string]int{"N": 0, "R": 1, "SR": 2, "SSR": 3}
	minVal := rarityOrder[minRarity]
	for i := range items {
		if rarityOrder[items[i].Rarity] < minVal {
			items[i].Rarity = minRarity
		}
	}

	// 构建奖励卡片
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

	rewardCard := richmessage.BuildBlindBoxRewardCard(richmessage.BlindBoxRewardCardData{
		Grade: grade,
		Items: views,
	})

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🎰 再开一个", "game_blindbox")
	kb.AddButton("🎮 游戏中心", "game_menu")

	h.telegram.SendMessage(chatID, rewardCard.Markdown, "Markdown", kb.Build())
}
