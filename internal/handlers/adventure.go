package handlers

import (
	cryptorand "crypto/rand"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/richmessage"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
)

// ============================================================
//  电影冒险 v2 — 电影互动处理器
// ============================================================

const (
	adventureMaxLevels = 5
	adventureMaxHP     = 100
	adventureBaseDmg   = 45 // 基础扣血（两次必死）
	adventureTrapDmg   = 60 // 陷阱扣血（一次半残）
	adventureBossDmg   = 70 // Boss关扣血（基本一击毙命）
	adventureComboHeal = 1  // 连击回血（微乎其微）
)

// AdventureState 冒险状态
type AdventureState struct {
	RunID              string                   `json:"run_id"`
	Success            bool                     `json:"success"`
	MovieInfo          *services.MovieInfo      `json:"movie_info"`
	Level              int                      `json:"level"`
	HP                 int                      `json:"hp"`
	MaxHP              int                      `json:"max_hp"` // 最大HP（含连胜加成）
	Combo              int                      `json:"combo"`
	MaxCombo           int                      `json:"max_combo"`
	Score              int                      `json:"score"`
	History            []string                 `json:"history"`
	Scene              *services.AdventureScene `json:"scene"`
	InProgress         bool                     `json:"in_progress"`
	TotalLevels        int                      `json:"total_levels"`
	PerfectRun         bool                     `json:"perfect_run"`
	TriedChoices       map[int]bool             `json:"tried_choices"`
	HintUsed           bool                     `json:"hint_used"`
	LastFreeReviveDate string                   `json:"last_free_revive_date"` // 上次免费复活日期 (YYYY-MM-DD)
	VengeanceActive    bool                     `json:"vengeance_active"`      // 继续挑战进度回馈（跳过第1关）
	StreakDays         int                      `json:"streak_days"`           // 连胜天数
	StreakRewards      *services.StreakRewards  `json:"-"`                     // 连胜奖励（不序列化）
	FreeSkipsUsed      int                      `json:"free_skips_used"`       // 已使用的免费跳过次数
	IsWeeklyBoss       bool                     `json:"is_weekly_boss"`        // 是否为本周挑战
	Phase              AdventurePhase           `json:"phase,omitempty"`
	Turn               int                      `json:"turn,omitempty"`
	ResolvedTurn       int                      `json:"resolved_turn,omitempty"`
	Mistakes           int                      `json:"mistakes,omitempty"`
	HintsUsed          int                      `json:"hints_used,omitempty"`
	ReviveCount        int                      `json:"revive_count,omitempty"`
	FinishClaimed      bool                     `json:"finish_claimed,omitempty"`
	ChoiceLock         sync.Mutex               `json:"-"` // 不序列化
}

func normalizeAdventureState(s *AdventureState) {
	if s == nil {
		return
	}
	if s.TriedChoices == nil {
		s.TriedChoices = make(map[int]bool)
	}
	if s.Turn <= 0 {
		s.Turn = s.Level
	}
	if s.Phase == "" {
		switch {
		case !s.InProgress:
			s.Phase = AdventurePhaseFinished
		case s.HP <= 0:
			s.Phase = AdventurePhaseRevive
		default:
			s.Phase = AdventurePhasePlaying
		}
	}
	s.InProgress = s.Phase == AdventurePhasePlaying || s.Phase == AdventurePhaseGenerating || s.Phase == AdventurePhaseRevive
}

func validateAdventureCallback(s *AdventureState, params map[string]string, phase AdventurePhase) bool {
	if s == nil || params == nil || params["run"] == "" || params["turn"] == "" || params["run"] != s.RunID || s.Phase != phase {
		return false
	}
	var turn int
	if _, err := fmt.Sscanf(params["turn"], "%d", &turn); err != nil {
		return false
	}
	return turn == s.Turn
}

func expiredAdventureCallback() *callback.Response {
	return &callback.Response{CallbackMsg: "这一幕已经结束", ShowAlert: true}
}

func finalizeAdventureResult(state *AdventureState, success bool, result *services.AdventureResult) *services.AdventureResult {
	if result == nil {
		result = &services.AdventureResult{}
	}
	result.Success = success
	result.Score = AdventureScore(success, state.PerfectRun, state.HP, state.TotalLevels, state.Mistakes, state.HintsUsed, state.ReviveCount, state.MaxCombo)
	result.Grade = AdventureGrade(result.Score)
	state.Score = result.Score
	return result
}

func claimAdventureFinish(state *AdventureState, success bool) bool {
	if state == nil || state.FinishClaimed {
		return false
	}
	state.FinishClaimed = true
	state.Success = success
	state.Phase = AdventurePhaseFinishing
	state.InProgress = false
	return true
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
	onAdventureSuccess func(userID int64, chatID int64, movieName string, movieYear int, tmdbID int, mediaType string, genres []string, score int, grade string)

	mu            sync.Mutex
	generating    map[int64]bool
	gambleStash   map[int64]*gambleStashEntry // 双倍或归零暂存 (userID → items+state)
	gambleStashMu sync.Mutex
	shareMu       sync.Mutex
	sharedRuns    map[string]struct{}
}

type gambleStashEntry struct {
	Items     []richmessage.BlindBoxItemView
	Grade     string
	MovieInfo *services.MovieInfo
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
		sharedRuns:   make(map[string]struct{}),
	}
}

func newAdventureRunID() string {
	var b [12]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b[:])
}

func canFreeRevive(s *AdventureState, today string) bool {
	return s != nil && s.HP <= 0 && s.Level >= 1 && s.Level <= 3 && s.LastFreeReviveDate != today
}

func canShareAdventure(s *AdventureState, runID string) bool {
	return s != nil && !s.InProgress && runID != "" && s.RunID == runID &&
		shouldBroadcastAdventure(s.Success, adventureGrade(s.Score), s.PerfectRun, s.MaxCombo, s.Level-1, s.TotalLevels)
}

func adventureGrade(score int) string {
	switch {
	case score >= 90:
		return "SSS"
	case score >= 80:
		return "SS"
	default:
		return ""
	}
}

func shouldBroadcastAdventure(success bool, grade string, perfect bool, combo, failedLevel, total int) bool {
	if success {
		return grade == "SSS" || grade == "SS" || perfect || combo >= 4
	}
	return total > 0 && failedLevel == total
}

func (h *AdventureHandler) claimAdventureShare(runID, requestID string) bool {
	if runID == "" || requestID == "" {
		return false
	}
	h.shareMu.Lock()
	defer h.shareMu.Unlock()
	if h.sharedRuns == nil {
		h.sharedRuns = make(map[string]struct{})
	}
	for _, k := range []string{"run:" + runID, "request:" + requestID} {
		if _, ok := h.sharedRuns[k]; ok {
			return false
		}
	}
	h.sharedRuns["run:"+runID] = struct{}{}
	h.sharedRuns["request:"+requestID] = struct{}{}
	return true
}

// SetSocialDB 注入社交数据库
func (h *AdventureHandler) SetSocialDB(db *services.SocialDB) {
	h.socialDB = db
}

// SetOnAdventureSuccess 注入冒险成功回调
func (h *AdventureHandler) SetOnAdventureSuccess(fn func(userID int64, chatID int64, movieName string, movieYear int, tmdbID int, mediaType string, genres []string, score int, grade string)) {
	h.onAdventureSuccess = fn
}

// HandleGoCommand 处理 /go 快捷命令（自动选片）
func (h *AdventureHandler) HandleGoCommand(telegram *services.TelegramClient, msg *types.TelegramMessage) {
	userID := msg.From.ID
	chatID := msg.Chat.ID
	sender := newUserScopedSender(telegram, chatID, userID)

	// 1. 优先：最近未完成的影片记录
	movieName := ""
	if h.socialDB != nil {
		movieName, _ = h.socialDB.GetTopNemesis(userID)
	}

	// 2. 其次：本周挑战
	if movieName == "" && h.socialDB != nil {
		if wb, err := h.socialDB.GetWeeklyBoss(); err == nil && wb != nil {
			movieName = wb.MovieName
		}
	}

	// 3. 最后：让用户输入
	if movieName == "" {
		if h.sessionMgr != nil {
			sess := h.sessionMgr.GetOrCreate(userID)
			if sess != nil {
				sess.Delete("adventure_state")
				sess.Set("pending_adventure_input", true)
			}
		}
		h.removeState(userID)
		_, _ = sender.SendMessage("⚔️ 暂无可继续的历史记录或本周片单。\n\n请发送你想挑战的电影名：", "", nil)
		return
	}

	go h.startAdventureAsync(userID, chatID, movieName)
}

// startWeeklyBossAsync 发起本周挑战
func (h *AdventureHandler) startWeeklyBossAsync(userID int64, chatID int64, wb *services.WeeklyBoss) {
	if wb == nil {
		_, _ = newUserScopedSender(h.telegram, chatID, userID).SendMessage("🎯 本周挑战尚未更新", "", nil)
		return
	}
	// 以梦魇模式启动冒险
	go func() {
		h.startAdventureAsyncLevel(userID, chatID, wb.MovieName, 1, "")
		// 标记为梦魇模式（需要修改 state 的 IsWeeklyBoss）
	}()
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
	normalizeAdventureState(&state)
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
	case "adventure_revive":
		return h.handleRevive(ctx)
	case "adventure_gamble":
		return h.handleGamble(ctx)
	case "adventure_gamble_safe":
		return h.handleGambleSafe(ctx)
	case "adventure_gamble_triple":
		return h.handleGambleTriple(ctx)
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
		newUserScopedSender(h.telegram, chatID, userID).SendMessage("❌ 电影名太长或为空，请重新输入", "", nil)
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
		newUserScopedSender(h.telegram, chatID, userID).SendMessage("⚠️ 你已经有一场进行中的冒险了", "", nil)
		return true
	}

	go h.startAdventureAsync(userID, chatID, movieName)
	return true
}

// handleStart 点击“电影冒险”或每日挑战启动
func (h *AdventureHandler) handleStart(ctx *callback.Context) (*callback.Response, error) {
	// 检查是否从每日挑战等入口带入了电影名
	movieName := ""
	if ctx.Callback != nil && ctx.Callback.Params != nil {
		if name, ok := ctx.Callback.Params["movie"]; ok && name != "" {
			if decoded, err := url.QueryUnescape(name); err == nil {
				movieName = decoded
			} else {
				movieName = name
			}
		}
	}

	// 如果有预填充电影名，直接启动冒险
	if movieName != "" {
		if h.sessionMgr != nil {
			sess := h.sessionMgr.GetOrCreate(ctx.UserID)
			if sess != nil {
				sess.Delete("adventure_state")
			}
		}
		h.removeState(ctx.UserID)
		go h.startAdventureAsync(ctx.UserID, ctx.ChatID, movieName)
		return &callback.Response{
			CallbackMsg: fmt.Sprintf("🎬 正在为「%s」生成冒险…", movieName),
		}, nil
	}

	if h.sessionMgr != nil {
		sess := h.sessionMgr.GetOrCreate(ctx.UserID)
		if sess != nil {
			// 清除所有旧的冒险状态（防止"已过期"残留）
			sess.Delete("adventure_state")
			sess.Set("pending_adventure_input", true)
		}
	}
	h.removeState(ctx.UserID) // 清除持久化
	// 动态获取真实通关率
	passRateText := ""
	if h.socialDB != nil {
		total, successCount, rate := h.socialDB.GetAdventureGlobalPassRate()
		if total >= 5 {
			passRateText = fmt.Sprintf("当前通关率 %.0f%%（%d/%d）", rate, successCount, total)
		}
	}

	return &callback.Response{
		RichMessage: fmt.Sprintf("## ⚔️ 电影冒险\n\n请发送你想求的电影或剧集名称。\n\n**例如**：`流浪地球` 或 `权力的游戏`\n\n> 这是可选的五关互动玩法。完成后会自动提交求片；如想直接求片，请返回主菜单选择「搜索求片」。\n\n%s", passRateText),
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
	if ctx.Callback == nil || !validateAdventureCallback(advState, ctx.Callback.Params, AdventurePhasePlaying) {
		return expiredAdventureCallback(), nil
	}

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
			maxHp := adventureMaxHP
			if advState.MaxHP > maxHp {
				maxHp = advState.MaxHP
			}
			if advState.HP > maxHp {
				advState.HP = maxHp
			}
		}
		// 背水一战：HP≤20时选对，额外回血+2
		lastStandBonus := 0
		if advState.HP <= 20+lastStandBonus {
			advState.HP += 2
			lastStandBonus = 2
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

		advState.ResolvedTurn = advState.Turn
		if advState.Level >= advState.TotalLevels {
			// 🏆 通关！先原子认领结算并持久化。
			if !claimAdventureFinish(advState, true) {
				return expiredAdventureCallback(), nil
			}
			sess.Set("adventure_state", advState)
			h.saveState(ctx.UserID, advState)
			go h.finishAdventureAsync(ctx.UserID, ctx.ChatID, advState, true)

			return &callback.Response{
				CallbackMsg: fmt.Sprintf("✅ %s\n🔥 连击 x%d！进入最终决战...", choice.Result, advState.Combo),
				ShowAlert:   false,
			}, nil
		}

		advState.Phase = AdventurePhaseGenerating
		sess.Set("adventure_state", advState)
		h.saveState(ctx.UserID, advState)
		go h.handleCorrectChoice(ctx.UserID, ctx.ChatID, advState, choice.Result)
		return &callback.Response{
			CallbackMsg: fmt.Sprintf("✅ %s\n🔥 连击 x%d", choice.Result, advState.Combo),
			ShowAlert:   false,
		}, nil
	}

	// ❌ 选择错误：伤害完全由服务端规则决定，忽略 AI HPChange。
	damage := AdventureDamage(advState.Level, advState.TotalLevels, choice.IsTrap)
	advState.PerfectRun = false
	advState.Mistakes++

	advState.HP -= damage
	advState.Combo = 0 // 连击归零
	advState.History = append(advState.History, fmt.Sprintf("(-%dHP)", damage))

	if advState.HP <= 0 {
		// 💀 死亡
		advState.HP = 0
		advState.InProgress = canFreeRevive(advState, time.Now().Format("2006-01-02"))
		sess.Set("adventure_state", advState)
		if advState.InProgress {
			advState.Phase = AdventurePhaseRevive
			h.saveState(ctx.UserID, advState)
			go h.sendDamageCard(ctx.UserID, ctx.ChatID, advState, damage, choice.Result)
		} else {
			if claimAdventureFinish(advState, false) {
				h.saveState(ctx.UserID, advState)
				go h.finishAdventureAsync(ctx.UserID, ctx.ChatID, advState, false)
			}
		}
		return &callback.Response{
			CallbackMsg: fmt.Sprintf("❤️ %s\n本次挑战结束", choice.Result),
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

// handleHint 「问导演」— 用服务端动态成本换一条精准线索

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
	if ctx.Callback == nil || !validateAdventureCallback(advState, ctx.Callback.Params, AdventurePhasePlaying) {
		return expiredAdventureCallback(), nil
	}

	// 检查是否已用过提示
	if advState.HintUsed {
		return &callback.Response{CallbackMsg: "🎬 导演已经给过你提示了，这关只能靠自己", ShowAlert: true}, nil
	}

	// 检查HP是否够；提示永不致死。
	cost := AdventureHintCost(advState.Level)
	newHP, ok := ApplyAdventureHint(advState.HP, advState.Level)
	if !ok {
		return &callback.Response{CallbackMsg: fmt.Sprintf("💔 生命值不足（需要%dHP），导演不敢再消耗你了", cost), ShowAlert: true}, nil
	}

	advState.HP = newHP
	advState.HintUsed = true
	advState.HintsUsed++
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

		hintMsg = fmt.Sprintf("🎬 导演的耳语（-%dHP）\n\n💡 %s%s", cost, scene.Hint, excludeMsg)
	} else {
		hintMsg = fmt.Sprintf("🎬 导演的耳语（-%dHP）\n\n💡 %s", cost, scene.Hint)
	}

	sess.Set("adventure_state", advState)
	h.saveState(ctx.UserID, advState) // 持久化

	// 发送提示 + 更新场景卡片
	go func() {
		newUserScopedSender(h.telegram, ctx.ChatID, ctx.UserID).SendMessage(hintMsg, "", nil)
		// 重新发送场景卡片（更新HP和按钮状态）
		h.sendSceneCard(ctx.UserID, ctx.ChatID, advState)
	}()

	return &callback.Response{
		CallbackMsg: fmt.Sprintf("🎬 导演给了提示\n❤️ -%dHP（剩余 %d%%）", cost, advState.HP),
		ShowAlert:   false,
	}, nil
}

// handleRetry 重新开始（含复仇模式：第3关后死亡 → 跳过第1关）
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
	var oldState *AdventureState
	if state, ok := sess.Get("adventure_state"); ok {
		if advState, ok := state.(*AdventureState); ok && advState.MovieInfo != nil {
			movieName = advState.MovieInfo.Title
			oldState = advState
		}
	}
	if movieName == "" {
		// 尝试从DB恢复电影名
		if restored := h.loadState(ctx.UserID); restored != nil && restored.MovieInfo != nil {
			movieName = restored.MovieInfo.Title
			oldState = restored
		}
	}

	// 继续挑战：上次到达第3关后结束，本次从第2关开始
	startLevel := 1
	vengeanceMsg := ""
	if oldState != nil && oldState.Level >= 3 && oldState.HP <= 0 && movieName != "" {
		// 再查DB确认：最近一次该电影是否失败且关卡>=3
		if h.socialDB != nil {
			if failedLevel := h.socialDB.GetLastFailedLevel(ctx.UserID, movieName); failedLevel >= 3 {
				startLevel = 2
				vengeanceMsg = fmt.Sprintf("↻ **继续挑战**\n\n你上次完成至第 %d 关，本次可从第 2 关开始。\n已为你保留一次进度回馈。", failedLevel)
			}
		}
	}

	// 无条件清除所有旧状态
	sess.Delete("adventure_state")
	sess.Delete("pending_adventure_input")
	h.removeState(ctx.UserID) // 清除持久化

	if movieName == "" {
		return h.handleStart(ctx)
	}

	go h.startAdventureAsyncLevel(ctx.UserID, ctx.ChatID, movieName, startLevel, vengeanceMsg)
	return &callback.Response{
		CallbackMsg: fmt.Sprintf("🔄 正在重新开始《%s》...", movieName),
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

	runID := ""
	if ctx.Callback != nil {
		runID = ctx.Callback.Params["run"]
	}
	if !canShareAdventure(advState, runID) {
		return &callback.Response{CallbackMsg: "这局战绩保持静默；仅 SSS、SS、无伤、x4+ 连击或终局惜败可分享", ShowAlert: true}, nil
	}
	if !h.claimAdventureShare(runID, ctx.CallbackID) {
		return &callback.Response{CallbackMsg: "✅ 这份战绩已经分享过了", ShowAlert: false}, nil
	}

	userName := h.getUserName(ctx.UserID)

	// 构建炫耀卡（不泄露Scene.Choices中的正确答案信息）
	shareCard := richmessage.BuildAdventureShareCard(richmessage.AdventureShareCardData{
		UserName:    userName,
		MovieTitle:  advState.MovieInfo.Title,
		MovieYear:   advState.MovieInfo.Year,
		Score:       advState.Score,
		HP:          advState.HP,
		MaxCombo:    advState.MaxCombo,
		PerfectRun:  advState.PerfectRun,
		Success:     true,
		Level:       advState.Level - 1,
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

// handleRevive 🩸 每日免费复活：HP恢复到30，继续当前关卡
func (h *AdventureHandler) handleRevive(ctx *callback.Context) (*callback.Response, error) {
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
	if ctx.Callback == nil || !validateAdventureCallback(advState, ctx.Callback.Params, AdventurePhaseRevive) {
		return expiredAdventureCallback(), nil
	}
	if !canFreeRevive(advState, time.Now().Format("2006-01-02")) {
		return &callback.Response{CallbackMsg: "❌ 当前没有可用的免费复活", ShowAlert: true}, nil
	}

	// 复活：HP = 30，标记今日已复活
	today := time.Now().Format("2006-01-02")
	advState.HP = adventureReviveHP
	advState.ReviveCount++
	advState.Combo = 0
	advState.Phase = AdventurePhasePlaying
	advState.InProgress = true
	advState.LastFreeReviveDate = today
	advState.PerfectRun = false // 受伤害了，不算完美

	if h.sessionMgr != nil {
		sess.Set("adventure_state", advState)
	}
	h.saveState(ctx.UserID, advState)

	// 继续机会通知
	_, _ = newUserScopedSender(h.telegram, ctx.ChatID, ctx.UserID).SendMessage(
		"❤️ **已恢复 30 HP**\n\n📖 可以继续当前关卡，请重新阅读线索后作答。\n\n⚠️ 今天的继续机会已使用",
		"Markdown", nil,
	)

	// 重新发送当前场景卡片
	h.sendSceneCard(ctx.UserID, ctx.ChatID, advState)

	return &callback.Response{CallbackMsg: "❤️ 已恢复 30 HP", ShowAlert: false}, nil
}

// handleGamble 尝试双倍奖励 — 50% 翻倍 / 50% 归零
func (h *AdventureHandler) handleGamble(ctx *callback.Context) (*callback.Response, error) {
	logger.Info("[Adventure] 🎰 Gamble callback from user %d", ctx.UserID)
	var items []richmessage.BlindBoxItemView
	var grade, movieTitle string

	// 优先从 DB 加载
	if h.socialDB != nil {
		itemsJSON, dbGrade, dbMovieName, _, _, _, _, err := h.socialDB.LoadGambleStash(ctx.UserID)
		if err == nil && itemsJSON != "" {
			json.Unmarshal([]byte(itemsJSON), &items)
			grade = dbGrade
			movieTitle = dbMovieName
			h.socialDB.DeleteGambleStash(ctx.UserID)
		}
	}

	// 回退到内存
	if len(items) == 0 {
		h.gambleStashMu.Lock()
		stash := h.gambleStash[ctx.UserID]
		delete(h.gambleStash, ctx.UserID)
		h.gambleStashMu.Unlock()
		if stash != nil {
			items = stash.Items
			grade = stash.Grade
			movieTitle = stash.MovieInfo.Title
		}
	}

	if len(items) == 0 {
		return &callback.Response{CallbackMsg: "🎁 奖励选择已过期，请重新通关获取盲盒", ShowAlert: true}, nil
	}

	// 50% 概率双倍
	won := rand.Intn(2) == 1

	if won {
		doubled := make([]richmessage.BlindBoxItemView, 0, len(items)*2)
		doubled = append(doubled, items...)
		doubled = append(doubled, items...)

		card := richmessage.BuildGambleResultCard(richmessage.GambleResultCardData{
			Grade:      grade,
			Items:      doubled,
			Won:        true,
			MovieTitle: movieTitle,
		})

		kb := services.NewKeyboardBuilder()
		kb.AddButton("🎰 再开一个", "game_blindbox")
		kb.AddButton("🎮 游戏中心", "game_menu")

		newUserScopedSender(h.telegram, ctx.ChatID, ctx.UserID).SendRichMessage(card.Markdown, kb.Build())
		return &callback.Response{CallbackMsg: "🎉 奖励已翻倍", ShowAlert: false}, nil
	}

	card := richmessage.BuildGambleResultCard(richmessage.GambleResultCardData{
		Grade:      grade,
		Items:      nil,
		Won:        false,
		MovieTitle: movieTitle,
	})

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🎁 再开一个盲盒", "game_blindbox")
	kb.AddButton("🎮 游戏中心", "game_menu")

	newUserScopedSender(h.telegram, ctx.ChatID, ctx.UserID).SendRichMessage(card.Markdown, kb.Build())
	return &callback.Response{CallbackMsg: "🎁 本次奖励归零", ShowAlert: false}, nil
}

// handleGambleSafe 保留当前奖励
func (h *AdventureHandler) handleGambleSafe(ctx *callback.Context) (*callback.Response, error) {
	logger.Info("[Adventure] 📦 GambleSafe callback from user %d", ctx.UserID)
	var items []richmessage.BlindBoxItemView
	var grade string

	// 优先从 DB 加载
	if h.socialDB != nil {
		itemsJSON, dbGrade, _, _, _, _, _, err := h.socialDB.LoadGambleStash(ctx.UserID)
		if err == nil && itemsJSON != "" {
			json.Unmarshal([]byte(itemsJSON), &items)
			grade = dbGrade
			h.socialDB.DeleteGambleStash(ctx.UserID)
		}
	}

	// 回退到内存
	if len(items) == 0 {
		h.gambleStashMu.Lock()
		stash := h.gambleStash[ctx.UserID]
		delete(h.gambleStash, ctx.UserID)
		h.gambleStashMu.Unlock()
		if stash != nil {
			items = stash.Items
			grade = stash.Grade
		}
	}

	if len(items) == 0 {
		return &callback.Response{CallbackMsg: "📦 奖励已领取或过期", ShowAlert: true}, nil
	}

	// 正常展示盲盒奖励
	rewardCard := richmessage.BuildBlindBoxRewardCard(richmessage.BlindBoxRewardCardData{
		Grade: grade,
		Items: items,
	})

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🎰 再开一个", "game_blindbox")
	kb.AddButton("🎮 游戏中心", "game_menu")

	newUserScopedSender(h.telegram, ctx.ChatID, ctx.UserID).SendMessage(rewardCard.Markdown, "Markdown", kb.Build())
	return &callback.Response{CallbackMsg: "📦 奖励已安全入袋", ShowAlert: false}, nil
}

// handleGambleTriple 尝试三倍奖励 — 30% 三倍 / 70% 减半
func (h *AdventureHandler) handleGambleTriple(ctx *callback.Context) (*callback.Response, error) {
	logger.Info("[Adventure] 💀 GambleTriple callback from user %d", ctx.UserID)
	var items []richmessage.BlindBoxItemView
	var grade, movieTitle string

	// 优先从 DB 加载
	if h.socialDB != nil {
		itemsJSON, dbGrade, dbMovieName, _, _, _, _, err := h.socialDB.LoadGambleStash(ctx.UserID)
		if err == nil && itemsJSON != "" {
			json.Unmarshal([]byte(itemsJSON), &items)
			grade = dbGrade
			movieTitle = dbMovieName
			h.socialDB.DeleteGambleStash(ctx.UserID)
		}
	}

	// 回退到内存
	if len(items) == 0 {
		h.gambleStashMu.Lock()
		stash := h.gambleStash[ctx.UserID]
		delete(h.gambleStash, ctx.UserID)
		h.gambleStashMu.Unlock()
		if stash != nil {
			items = stash.Items
			grade = stash.Grade
			movieTitle = stash.MovieInfo.Title
		}
	}

	if len(items) == 0 {
		return &callback.Response{CallbackMsg: "🎁 奖励选择已过期", ShowAlert: true}, nil
	}

	// 30% 概率三倍
	won := rand.Intn(100) < 30

	if won {
		tripled := make([]richmessage.BlindBoxItemView, 0, len(items)*3)
		for i := 0; i < 3; i++ {
			tripled = append(tripled, items...)
		}

		card := richmessage.BuildGambleResultCard(richmessage.GambleResultCardData{
			Grade:      grade,
			Items:      tripled,
			Won:        true,
			Multiplier: 3,
			MovieTitle: movieTitle,
		})

		kb := services.NewKeyboardBuilder()
		kb.AddButton("🎰 再开一个", "game_blindbox")
		kb.AddButton("🎮 游戏中心", "game_menu")

		newUserScopedSender(h.telegram, ctx.ChatID, ctx.UserID).SendRichMessage(card.Markdown, kb.Build())
		// 三倍成功 → 群通知
		userName := h.getUserName(ctx.UserID)
		h.notifyGroup(userName, fmt.Sprintf("✨ 三倍奖励命中\n\n%s 在《%s》的奖励选择中获得三倍盲盒。\n\n概率 30%% · 本次已结算", userName, movieTitle))
		return &callback.Response{CallbackMsg: "✨ 三倍奖励已到账", ShowAlert: false}, nil
	}

	// 未命中时保留一半奖励
	halved := items[:len(items)/2]
	if len(halved) == 0 {
		halved = nil
	}

	card := richmessage.BuildGambleResultCard(richmessage.GambleResultCardData{
		Grade:      grade,
		Items:      halved,
		Won:        false,
		MovieTitle: movieTitle,
	})

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🎁 再开一个盲盒", "game_blindbox")
	kb.AddButton("🎮 游戏中心", "game_menu")

	newUserScopedSender(h.telegram, ctx.ChatID, ctx.UserID).SendRichMessage(card.Markdown, kb.Build())
	callbackMsg := "🎁 本次奖励归零"
	if len(halved) > 0 {
		callbackMsg = "🎁 本次保留一半奖励"
	}
	return &callback.Response{CallbackMsg: callbackMsg, ShowAlert: false}, nil
}

// ============================================================
//  异步方法
// ============================================================

// startAdventureAsync 开始冒险（默认第1关）
func (h *AdventureHandler) startAdventureAsync(userID int64, chatID int64, movieName string) {
	h.startAdventureAsyncLevel(userID, chatID, movieName, 1, "")
}

// startAdventureAsyncLevel 开始冒险（指定起始关卡 + 复仇信息）
func (h *AdventureHandler) startAdventureAsyncLevel(userID int64, chatID int64, movieName string, startLevel int, preMsg string) {
	sender := newUserScopedSender(h.telegram, chatID, userID)
	defer func() {
		if r := recover(); r != nil {
			logger.Info("[Adventure] Panic for user %d: %v", userID, r)
			sender.SendMessage("❌ 冒险启动出错了，请稍后再试", "", nil)
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

	// 超时清理：120秒后强制清除generating标记
	go func(uid int64) {
		time.Sleep(120 * time.Second)
		h.mu.Lock()
		if h.generating[uid] {
			delete(h.generating, uid)
			logger.Info("[Adventure] Force cleaned generating flag for user %d (timeout)", uid)
		}
		h.mu.Unlock()
	}(userID)

	loadingMsg, _ := sender.SendMessage("⚔️ 正在进入「"+movieName+"」的世界...", "", nil)

	// 清除旧的冒险状态
	if h.sessionMgr != nil {
		sess := h.sessionMgr.GetOrCreate(userID)
		if sess != nil {
			sess.Delete("adventure_state")
			sess.Delete("pending_adventure_input")
		}
	}
	h.removeState(userID)

	movieInfo, err := h.adventureSvc.SearchMovieInfo(movieName)
	if err != nil {
		if loadingMsg != nil {
			sender.DeleteMessage(loadingMsg)
		}
		logger.Info("[冒险] 搜索电影失败: query=%q err=%v", movieName, err)
		if _, sendErr := sender.SendMessage(fmt.Sprintf("没搜到《%s》。换个中文名、英文名，或者带上年份再试。", movieName), "", nil); sendErr != nil {
			logger.Info("[冒险] 发送搜索失败提示失败: %v", sendErr)
		}
		return
	}

	// ☠️ 宿敌检查
	nemesisCount := 0
	if h.socialDB != nil {
		nemesisCount = h.socialDB.GetNemesisCount(userID, movieName)
	}

	// 发送入口卡片
	entryCard := richmessage.BuildAdventureEntryCard(richmessage.AdventureEntryCardData{
		MovieTitle:   movieInfo.Title,
		MovieYear:    movieInfo.Year,
		Genres:       movieInfo.Genres,
		Overview:     movieInfo.Overview,
		Rating:       movieInfo.Rating,
		NemesisCount: nemesisCount,
	})

	if loadingMsg != nil {
		sender.DeleteMessage(loadingMsg)
	}
	sender.SendRichMessage(entryCard.Markdown, nil)

	// 复仇模式提示消息
	if preMsg != "" {
		sender.SendMessage(preMsg, "Markdown", nil)
	}

	// 再次挑战提示
	if nemesisCount > 0 {
		var retryMsg string
		if nemesisCount == 1 {
			retryMsg = fmt.Sprintf("↻ **再次挑战《%s》**\n\n你曾在这里止步 1 次，这次可以带着上次的线索继续。", movieInfo.Title)
		} else {
			retryMsg = fmt.Sprintf("↻ **再次挑战《%s》**\n\n你已尝试过 %d 次，每一次记录都为你保留。", movieInfo.Title, nemesisCount)
		}
		_, _ = sender.SendMessage(retryMsg, "Markdown", nil)
	}

	// 生成起始关
	levelLabel := fmt.Sprintf("第 %d 关", startLevel)
	tip := randomMovieTip()
	tipMsg, _ := sender.SendMessage(fmt.Sprintf("⏳ 正在构造%s...\n\n%s", levelLabel, tip), "", nil)

	scene, err := h.adventureSvc.GenerateScene(movieInfo, startLevel, adventureMaxLevels, nil, adventureMaxHP)
	if err != nil {
		logger.Info("[Adventure] AI scene gen failed, using fallback: %v", err)
		scene = h.adventureSvc.GenerateFallbackScene(movieInfo, startLevel, adventureMaxLevels)
	}
	scene.TotalLevels = adventureMaxLevels

	if tipMsg != nil {
		sender.DeleteMessage(tipMsg)
	}

	vengeanceActive := startLevel > 1

	// 🔥 连胜加成
	streakDays := 0
	var streakRewards *services.StreakRewards
	if h.socialDB != nil {
		streak, _ := h.socialDB.GetAdventureStreak(userID)
		if streak != nil {
			streakDays = streak.CurrentStreak
			streakRewards = services.GetStreakRewards(streakDays)
		}
	}

	initialHP := adventureMaxHP
	if streakRewards != nil {
		initialHP = adventureMaxHP + streakRewards.BonusHP
	}

	state := &AdventureState{
		RunID:           newAdventureRunID(),
		MovieInfo:       movieInfo,
		Level:           startLevel,
		HP:              initialHP,
		MaxHP:           initialHP,
		Combo:           0,
		MaxCombo:        0,
		Score:           0,
		History:         []string{},
		Scene:           scene,
		InProgress:      true,
		TotalLevels:     adventureMaxLevels,
		PerfectRun:      !vengeanceActive,
		TriedChoices:    make(map[int]bool),
		VengeanceActive: vengeanceActive,
		StreakDays:      streakDays,
		Phase:           AdventurePhasePlaying,
		Turn:            startLevel,
		StreakRewards:   streakRewards,
	}

	if h.sessionMgr != nil {
		sess := h.sessionMgr.GetOrCreate(userID)
		if sess != nil {
			sess.Set("adventure_state", state)
		}
	}
	h.saveState(userID, state)

	h.sendSceneCard(userID, chatID, state)
}

// handleCorrectChoice 选对后的处理 — 只发场景卡片，反馈内嵌
func (h *AdventureHandler) handleCorrectChoice(userID int64, chatID int64, state *AdventureState, choiceResult string) {
	sender := newUserScopedSender(h.telegram, chatID, userID)
	defer func() {
		if r := recover(); r != nil {
			logger.Info("[Adventure] Correct choice panic: %v", r)
		}
	}()

	// 生成下一关（不发单独的combo卡片，反馈直接写在场景卡片里）
	tip := randomMovieTip()
	loadingMsg, _ := sender.SendMessage(fmt.Sprintf("⏳ 正在构造第 %d 关...\n\n%s", state.Level, tip), "", nil)

	scene, err := h.adventureSvc.GenerateScene(state.MovieInfo, state.Level+1, state.TotalLevels, state.History, state.HP)
	if err != nil {
		logger.Info("[Adventure] AI scene gen failed for level %d: %v", state.Level+1, err)
		scene = h.adventureSvc.GenerateFallbackScene(state.MovieInfo, state.Level+1, state.TotalLevels)
	}
	scene.TotalLevels = state.TotalLevels
	state.ChoiceLock.Lock()
	if state.Phase != AdventurePhaseGenerating || state.ResolvedTurn != state.Turn {
		state.ChoiceLock.Unlock()
		return
	}
	// 生成场景期间可能已退出或开启新局。重新读取当前状态，防止旧
	// goroutine 把上一局写回 session/DB。
	if h.sessionMgr == nil {
		state.ChoiceLock.Unlock()
		return
	}
	sess := h.sessionMgr.GetOrCreate(userID)
	current := h.getOrRestoreState(userID, sess)
	if current == nil || current.RunID != state.RunID || current.Turn != state.Turn || current.Phase != AdventurePhaseGenerating {
		state.ChoiceLock.Unlock()
		return
	}
	state.Level++
	state.Turn++
	state.Scene = scene
	state.TriedChoices = make(map[int]bool)
	state.HintUsed = false
	state.Phase = AdventurePhasePlaying
	state.ChoiceLock.Unlock()

	if sess != nil {
		sess.Set("adventure_state", state)
	}
	h.saveState(userID, state) // 持久化

	if loadingMsg != nil {
		sender.DeleteMessage(loadingMsg)
	}

	h.sendSceneCard(userID, chatID, state)
}

// sendDamageCard 发送受伤卡片 + 剩余选项（含每日免费复活检查）
func (h *AdventureHandler) sendDamageCard(userID int64, chatID int64, state *AdventureState, damage int, choiceResult string) {
	sender := newUserScopedSender(h.telegram, chatID, userID)
	defer func() {
		if r := recover(); r != nil {
			logger.Info("[Adventure] Damage card panic: %v", r)
		}
	}()

	isDead := state.HP <= 0

	// 🩸 每日免费复活：第1-3关死亡时触发
	if isDead && state.Level >= 1 && state.Level <= 3 {
		today := time.Now().Format("2006-01-02")
		if state.LastFreeReviveDate != today {
			// 还没用过今天的免费复活 → 展示复活卡片
			card := richmessage.BuildAdventureReviveCard(richmessage.AdventureReviveCardData{
				MovieTitle:    state.MovieInfo.Title,
				Level:         state.Level,
				TotalLevels:   state.TotalLevels,
				HP:            state.HP,
				Damage:        damage,
				CorrectAnswer: "",
			})
			kb := services.NewKeyboardBuilder()
			kb.AddButton("❤️ 恢复 30 HP 并继续", fmt.Sprintf("adventure_revive:run:%s:turn:%d", state.RunID, state.Turn))
			kb.NewRow()
			kb.AddButton("🔄 重新开始", "adventure_retry")
			kb.AddButton("🎮 游戏中心", "game_menu")
			sender.SendRichMessage(card.Markdown, kb.Build())
			return
		}
	}

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
		ChoiceResult:     choiceResult,
		Damage:           damage,
		HP:               state.HP,
		Level:            state.Level,
		TotalLevels:      state.TotalLevels,
		Combo:            state.Combo,
		Score:            state.Score,
		IsDead:           isDead,
		RemainingChoices: remainingChoices,
		TriedChoices:     state.TriedChoices,
		CorrectAnswer:    correctAnswer,
		CorrectReason:    correctReason,
	})

	if isDead {
		kb := services.NewKeyboardBuilder()
		kb.AddButton("🔄 重新开始", "adventure_retry")
		kb.AddButton("🎬 换一部电影", "adventure_start")
		kb.NewRow()
		kb.AddButton("🎮 游戏中心", "game_menu")
		sender.SendRichMessage(card.Markdown, kb.Build())
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
		kb.AddButton(num, fmt.Sprintf("adventure_choice:idx:%d:run:%s:turn:%d", i, state.RunID, state.Turn))
		hasRemaining = true
	}
	kb.NewRow()
	kb.AddButton("🚪 退出冒险", "adventure_quit")

	// 如果所有选项都试过了（理论上不可能，但防御性编程）
	if !hasRemaining {
		kb.AddButton("🔄 重新开始", "adventure_retry")
	}

	sender.SendRichMessage(card.Markdown, kb.Build())
}

func normalizeAdventureMediaType(mediaType string) string {
	if mediaType == "tv" {
		return "tv"
	}
	return "movie"
}

// finishAdventureAsync 完成冒险
func (h *AdventureHandler) finishAdventureAsync(userID int64, chatID int64, state *AdventureState, success bool) {
	sender := newUserScopedSender(h.telegram, chatID, userID)
	state.Success = success
	defer func() {
		if r := recover(); r != nil {
			logger.Info("[Adventure] Finish panic: %v", r)
			sender.SendMessage("❌ 结局生成出错了", "", nil)
		}
	}()

	loadingMsg, _ := sender.SendMessage("🎬 生成结局...", "", nil)

	result, err := h.adventureSvc.GenerateEndScene(
		state.MovieInfo, state.History, success, state.HP, state.MaxCombo, state.TotalLevels,
	)
	if err != nil {
		logger.Info("[Adventure] End scene AI failed: %v", err)
		result = h.adventureSvc.GenerateFallbackResult(state.MovieInfo, success, state.HP, state.Level-1, state.TotalLevels)
	}
	result = finalizeAdventureResult(state, success, result)

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

	// Update weekly leaderboard
	if success && h.socialDB != nil {
		if result.Grade == "SSS" {
			_ = h.socialDB.UpdateWeeklyLeaderboard(userID, "sss", 1)
		}
		_ = h.socialDB.UpdateWeeklyLeaderboard(userID, "score", result.Score)
	}

	// 通关 → 自动提交求片请求
	if success && h.onAdventureSuccess != nil {
		go h.onAdventureSuccess(
			userID, chatID,
			state.MovieInfo.Title, state.MovieInfo.Year,
			state.MovieInfo.TMDBID, normalizeAdventureMediaType(state.MovieInfo.MediaType), state.MovieInfo.Genres,
			result.Score, result.Grade,
		)
	}

	// 群通知：荣耀播报（稀缺性 = 更有面子）
	userName := h.getUserName(userID)
	if success {
		// HP 安全钳：防止显示异常值
		displayHP := state.HP
		if displayHP > state.MaxHP && state.MaxHP > 0 {
			displayHP = state.MaxHP
		} else if displayHP > 100 {
			displayHP = 100
		}

		shouldNotify := false
		notifyMsg := ""
		switch {
		case result.Grade == "SSS":
			shouldNotify = true
			if state.PerfectRun {
				notifyMsg = fmt.Sprintf("👑 电影冒险 · SSS\n\n%s 通关了《%s》(%d)\n\n🎯 %d分 · ❤️ 100%% · 🔥 x%d\n🛡️ 全程无伤", userName, state.MovieInfo.Title, state.MovieInfo.Year, result.Score, state.MaxCombo)
			} else {
				notifyMsg = fmt.Sprintf("👑 电影冒险 · SSS\n\n%s 通关了《%s》(%d)\n\n🎯 %d分 · ❤️ %d%% · 🔥 x%d", userName, state.MovieInfo.Title, state.MovieInfo.Year, result.Score, displayHP, state.MaxCombo)
			}
		case result.Grade == "SS":
			shouldNotify = true
			notifyMsg = fmt.Sprintf("💎 电影冒险 · SS\n\n%s 通关了《%s》(%d)\n\n🎯 %d分 · ❤️ %d%% · 🔥 x%d", userName, state.MovieInfo.Title, state.MovieInfo.Year, result.Score, displayHP, state.MaxCombo)
		case state.PerfectRun:
			shouldNotify = true
			notifyMsg = fmt.Sprintf("🛡️ 电影冒险 · 无伤通关\n\n%s 通关了《%s》(%d)\n\n🎯 %d分 · ❤️ 100%%", userName, state.MovieInfo.Title, state.MovieInfo.Year, result.Score)
		case state.MaxCombo >= 4:
			shouldNotify = true
			notifyMsg = fmt.Sprintf("🔥 电影冒险 · 连击记录\n\n%s 完成了《%s》(%d)\n\n🎯 %d分 · ❤️ %d%% · 🔥 x%d", userName, state.MovieInfo.Title, state.MovieInfo.Year, result.Score, displayHP, state.MaxCombo)
		}
		if shouldNotify && shouldBroadcastAdventure(true, result.Grade, state.PerfectRun, state.MaxCombo, 0, state.TotalLevels) {
			h.notifyGroup(userName, notifyMsg)
		}
	} else if shouldBroadcastAdventure(false, result.Grade, state.PerfectRun, state.MaxCombo, state.Level-1, state.TotalLevels) {
		h.notifyGroup(userName, fmt.Sprintf("🎬 电影冒险 · 本次记录\n\n%s 完成至《%s》(%d) 第 %d/%d 关\n\n本次成绩已保存。", userName, state.MovieInfo.Title, state.MovieInfo.Year, state.Level-1, state.TotalLevels))
	}

	// 发送结果卡片
	// 🌍 全球通关率
	globalPassRate := ""
	if h.socialDB != nil {
		total, successCount, rate := h.socialDB.GetMoviePassRate(state.MovieInfo.Title)
		if total >= 1 {
			if total < 5 {
				globalPassRate = "当前参与记录较少，暂不展示通关率"
			} else if success {
				globalPassRate = fmt.Sprintf("当前记录中，%.0f%% 的参与者通关了《%s》（%d/%d）", rate, state.MovieInfo.Title, successCount, total)
			} else {
				globalPassRate = fmt.Sprintf("已有 %.0f%% 的参与者尚未完成《%s》的五关挑战", 100-rate, state.MovieInfo.Title)
			}
		}
	}

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
			GlobalPassRate: globalPassRate,
		})

		kb := services.NewKeyboardBuilder()
		if shouldBroadcastAdventure(true, result.Grade, state.PerfectRun, state.MaxCombo, 0, state.TotalLevels) {
			kb.AddButton("📢 分享战绩", fmt.Sprintf("adventure_share:run:%s", state.RunID))
		}
		kb.AddButton("🔄 再挑战一次", "adventure_retry")
		kb.NewRow()
		kb.AddButton("🎰 通关盲盒", "game_blindbox")
		kb.AddButton("🎮 游戏中心", "game_menu")

		sender.SendRichMessage(card.Markdown, kb.Build())

		// 通关奖励：免费开盲盒
		go h.sendRewardBlindBox(userID, chatID, state, result.Grade)
	} else {
		card := richmessage.BuildAdventureFailCard(richmessage.AdventureFailCardData{
			MovieTitle:     state.MovieInfo.Title,
			MovieYear:      state.MovieInfo.Year,
			Genres:         state.MovieInfo.Genres,
			Level:          state.Level - 1,
			TotalLevels:    state.TotalLevels,
			FinalScene:     result.FinalScene,
			DeathReason:    result.DeathReason,
			Tips:           result.Tips,
			Score:          result.Score,
			Grade:          result.Grade,
			Stats:          result.Stats,
			MaxCombo:       state.MaxCombo,
			HP:             0,
			GlobalPassRate: globalPassRate,
		})

		kb := services.NewKeyboardBuilder()
		kb.AddButton("🔄 我知道答案了！", "adventure_retry")
		kb.NewRow()
		kb.AddButton("🎮 游戏中心", "game_menu")

		sender.SendRichMessage(card.Markdown, kb.Build())
	}
	state.ChoiceLock.Lock()
	state.Phase = AdventurePhaseFinished
	state.InProgress = false
	state.ChoiceLock.Unlock()
	if h.sessionMgr != nil {
		if sess := h.sessionMgr.GetOrCreate(userID); sess != nil {
			sess.Set("adventure_state", state)
		}
	}
	h.saveState(userID, state)
}

// sendSceneCard 发送场景卡片
func (h *AdventureHandler) sendSceneCard(userID int64, chatID int64, state *AdventureState) {
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

	// 仅保留明确标注为建议的节奏提示，不伪造玩家统计。
	_, _, timeUrgency := generatePsychoData(state.Level, state.TotalLevels, len(scene.Choices))

	card := richmessage.BuildAdventureSceneCard(richmessage.AdventureSceneCardData{
		MovieTitle:  state.MovieInfo.Title,
		MovieYear:   state.MovieInfo.Year,
		Genres:      state.MovieInfo.Genres,
		Level:       state.Level,
		TotalLevels: state.TotalLevels,
		StageName:   scene.StageName,
		SceneTitle:  scene.Title,
		Description: scene.Description,
		Atmosphere:  scene.Atmosphere,
		Choices:     choices,
		Hint:        "",
		HP:          state.HP,
		Combo:       state.Combo,
		Score:       state.Score,
		IsBoss:      state.Level == state.TotalLevels,
		LastResult:  lastResult,
		DeathRate:   "",
		OptionStats: "",
		TimeUrgency: timeUrgency,
	})

	kb := services.NewKeyboardBuilder()
	numbers := []string{"1️⃣", "2️⃣", "3️⃣", "4️⃣"}
	for i := range scene.Choices {
		num := fmt.Sprintf("#%d", i+1)
		if i < len(numbers) {
			num = numbers[i]
		}
		kb.AddButton(num, fmt.Sprintf("adventure_choice:idx:%d:run:%s:turn:%d", i, state.RunID, state.Turn))
	}
	kb.NewRow()
	// 问导演按钮（每关限用一次，花10HP）
	cost := AdventureHintCost(state.Level)
	if !state.HintUsed && state.HP > cost {
		kb.AddButton(fmt.Sprintf("🎬 问导演 (-%dHP)", cost), fmt.Sprintf("adventure_hint:run:%s:turn:%d", state.RunID, state.Turn))
	}
	kb.AddButton("🚪 退出", "adventure_quit")

	newUserScopedSender(h.telegram, chatID, userID).SendRichMessage(card.Markdown, kb.Build())
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

// generatePsychoData 生成中性的关卡难度与节奏提示。
// 保留原返回结构以兼容卡片渲染，但不展示模拟的玩家死亡率或选项分布。
func generatePsychoData(level, totalLevels, choiceCount int) (difficulty, optionStats, paceHint string) {
	if totalLevels <= 0 {
		totalLevels = adventureMaxLevels
	}
	if level < 1 {
		level = 1
	}
	if level > totalLevels {
		level = totalLevels
	}

	difficulty = fmt.Sprintf("🎬 关卡难度：%d/%d", level, totalLevels)
	optionStats = ""
	paceHint = "按自己的节奏阅读线索，不限作答时间"
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
		"🎬 彩蛋片单：《穆赫兰道》",
		"⚡ 本局高光已记录到冒险战绩",
		"🎭 彩蛋片单：《彗星来的那一夜》",
	}

	// 高评级和无伤通关只追加真实可见的战绩描述，不承诺未实现的道具。
	if grade == "SSS" {
		bonusPool = append(bonusPool, "👑 SSS 高光已进入冒险排行统计")
	}
	if perfectRun {
		bonusPool = append(bonusPool, "🛡️ 无伤通关已写入个人战绩")
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

func (h *AdventureHandler) sendRewardBlindBox(userID int64, chatID int64, state *AdventureState, grade string) {
	sender := newUserScopedSender(h.telegram, chatID, userID)
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

	// S 评级以上可选择保留当前奖励，或尝试倍率奖励（DB 持久化）。
	if grade == "SSS" || grade == "SS" || grade == "S" {
		// 暂存盲盒物品到 DB
		if h.socialDB != nil {
			itemsJSON, _ := json.Marshal(views)
			genresJSON, _ := json.Marshal(state.MovieInfo.Genres)
			_ = h.socialDB.SaveGambleStash(userID, string(itemsJSON), grade,
				state.MovieInfo.Title, state.MovieInfo.Year, state.MovieInfo.TMDBID, string(genresJSON))
		} else {
			// 回退到内存（socialDB未初始化时）
			h.gambleStashMu.Lock()
			if h.gambleStash == nil {
				h.gambleStash = make(map[int64]*gambleStashEntry)
			}
			h.gambleStash[userID] = &gambleStashEntry{Items: views, Grade: grade, MovieInfo: state.MovieInfo}
			h.gambleStashMu.Unlock()
			go func(cid int64) {
				time.Sleep(60 * time.Second)
				h.gambleStashMu.Lock()
				delete(h.gambleStash, cid)
				h.gambleStashMu.Unlock()
			}(userID)
		}

		// 发送倍率奖励选择卡片
		gambleCard := richmessage.BuildGambleOfferCard(richmessage.GambleOfferCardData{
			Grade:      grade,
			ItemCount:  len(views),
			MovieTitle: state.MovieInfo.Title,
		})

		kb := services.NewKeyboardBuilder()
		kb.AddButton("📦 稳妥收下", "adventure_gamble_safe")
		kb.NewRow()
		kb.AddButton("🎰 尝试双倍 (50%)", "adventure_gamble")
		kb.NewRow()
		kb.AddButton("✨ 尝试三倍 (30%)", "adventure_gamble_triple")

		sender.SendMessage(gambleCard.Markdown, "Markdown", kb.Build())
		return
	}

	// A级以下：直接发放
	rewardCard := richmessage.BuildBlindBoxRewardCard(richmessage.BlindBoxRewardCardData{
		Grade: grade,
		Items: views,
	})

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🎰 再开一个", "game_blindbox")
	kb.AddButton("🎮 游戏中心", "game_menu")

	sender.SendMessage(rewardCard.Markdown, "Markdown", kb.Build())
}
