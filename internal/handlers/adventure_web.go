package handlers

import (
	"errors"
	"fmt"
	"strings"

	"github.com/xzb177/yimao/internal/session"
)

var (
	ErrAdventureNotFound = errors.New("adventure not found")
	ErrAdventureExpired  = errors.New("adventure turn expired")
	ErrAdventureBusy     = errors.New("adventure is generating")
)

// AdventureWebChoice intentionally omits correctness and server-only effects.
type AdventureWebChoice struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
	Tried bool   `json:"tried"`
}

type AdventureWebView struct {
	RunID       string               `json:"run_id,omitempty"`
	Phase       AdventurePhase       `json:"phase"`
	MovieTitle  string               `json:"movie_title,omitempty"`
	MovieYear   int                  `json:"movie_year,omitempty"`
	Backdrops   []string             `json:"backdrops,omitempty"`
	Level       int                  `json:"level,omitempty"`
	Turn        int                  `json:"turn,omitempty"`
	TotalLevels int                  `json:"total_levels,omitempty"`
	HP          int                  `json:"hp,omitempty"`
	MaxHP       int                  `json:"max_hp,omitempty"`
	Combo       int                  `json:"combo,omitempty"`
	Score       int                  `json:"score,omitempty"`
	SceneTitle  string               `json:"scene_title,omitempty"`
	StageName   string               `json:"stage_name,omitempty"`
	Description string               `json:"description,omitempty"`
	Atmosphere  string               `json:"atmosphere,omitempty"`
	Choices     []AdventureWebChoice `json:"choices,omitempty"`
	Hint        string               `json:"hint,omitempty"`
	Message     string               `json:"message,omitempty"`
	Success     bool                 `json:"success,omitempty"`
	Grade       string               `json:"grade,omitempty"`
}

func adventureWebView(state *AdventureState) *AdventureWebView {
	if state == nil {
		return nil
	}
	normalizeAdventureState(state)
	view := &AdventureWebView{
		RunID: state.RunID, Phase: state.Phase, Level: state.Level, Turn: state.Turn,
		TotalLevels: state.TotalLevels, HP: state.HP, MaxHP: state.MaxHP,
		Combo: state.Combo, Score: state.Score, Success: state.Success,
	}
	if state.MovieInfo != nil {
		view.MovieTitle, view.MovieYear = state.MovieInfo.Title, state.MovieInfo.Year
		view.Backdrops = append([]string(nil), state.MovieInfo.Backdrops...)
	}
	if state.Scene != nil {
		view.SceneTitle, view.StageName = state.Scene.Title, state.Scene.StageName
		view.Description, view.Atmosphere = state.Scene.CinematicDescription(), state.Scene.Atmosphere
		for i, choice := range state.Scene.Choices {
			view.Choices = append(view.Choices, AdventureWebChoice{Index: i, Text: choice.Text, Tried: state.TriedChoices[i]})
		}
	}
	if state.Phase == AdventurePhaseFinished {
		view.Grade = AdventureGrade(state.Score)
	}
	return view
}

func (h *AdventureHandler) WebCurrent(userID int64) *AdventureWebView {
	if h == nil || h.sessionMgr == nil {
		return nil
	}
	op := h.adventureOp(userID)
	op.Lock()
	defer op.Unlock()
	state := h.getOrRestoreState(userID, h.sessionMgr.GetOrCreate(userID))
	if state == nil {
		return nil
	}
	state.ChoiceLock.Lock()
	defer state.ChoiceLock.Unlock()
	return adventureWebView(state)
}

func (h *AdventureHandler) WebStart(userID int64, movieName string) (*AdventureWebView, error) {
	if h == nil || h.sessionMgr == nil || h.adventureSvc == nil {
		return nil, ErrAdventureNotFound
	}
	movieName = strings.TrimSpace(movieName)
	if movieName == "" || len([]rune(movieName)) > 80 {
		return nil, fmt.Errorf("invalid movie name")
	}

	op := h.adventureOp(userID)
	op.Lock()
	epoch := h.claimAdventureGeneration(userID)
	sess := h.sessionMgr.GetOrCreate(userID)
	if sess != nil {
		sess.Delete("adventure_state")
	}
	h.removeState(userID)
	op.Unlock()
	defer h.releaseAdventureGeneration(userID, epoch)

	movieInfo, err := h.adventureSvc.SearchMovieInfo(movieName)
	if err != nil || movieInfo == nil {
		return nil, fmt.Errorf("movie lookup failed: %w", err)
	}
	if !h.adventureEpochCurrent(userID, epoch) {
		return nil, ErrAdventureExpired
	}
	scene, err := h.adventureSvc.GenerateScene(movieInfo, 1, adventureMaxLevels, nil, adventureMaxHP)
	if err != nil {
		scene = h.adventureSvc.GenerateFallbackScene(movieInfo, 1, adventureMaxLevels)
	}
	if scene == nil {
		return nil, fmt.Errorf("scene generation failed")
	}
	scene.TotalLevels = adventureMaxLevels
	state := &AdventureState{
		RunID: newAdventureRunID(), MovieInfo: movieInfo, Level: 1, HP: adventureMaxHP,
		MaxHP: adventureMaxHP, History: []string{}, Scene: scene, InProgress: true,
		TotalLevels: adventureMaxLevels, PerfectRun: true, TriedChoices: make(map[int]bool),
		Phase: AdventurePhasePlaying, Turn: 1,
	}

	op.Lock()
	defer op.Unlock()
	if !h.adventureEpochCurrent(userID, epoch) {
		return nil, ErrAdventureExpired
	}
	sess = h.sessionMgr.GetOrCreate(userID)
	if sess == nil {
		return nil, ErrAdventureNotFound
	}
	sess.Set("adventure_state", state)
	h.saveState(userID, state)
	return adventureWebView(state), nil
}

func (h *AdventureHandler) WebChoice(userID int64, runID string, turn, choiceIdx int) (*AdventureWebView, error) {
	if h == nil || h.sessionMgr == nil {
		return nil, ErrAdventureNotFound
	}
	op := h.adventureOp(userID)
	op.Lock()
	sess := h.sessionMgr.GetOrCreate(userID)
	state := h.getOrRestoreState(userID, sess)
	if state == nil {
		op.Unlock()
		return nil, ErrAdventureNotFound
	}
	state.ChoiceLock.Lock()
	if !validateAdventureCallback(state, map[string]string{"run": runID, "turn": fmt.Sprint(turn)}, AdventurePhasePlaying) {
		state.ChoiceLock.Unlock()
		op.Unlock()
		return nil, ErrAdventureExpired
	}
	if state.Scene == nil || choiceIdx < 0 || choiceIdx >= len(state.Scene.Choices) {
		state.ChoiceLock.Unlock()
		op.Unlock()
		return nil, fmt.Errorf("invalid choice")
	}
	if state.TriedChoices[choiceIdx] {
		state.ChoiceLock.Unlock()
		op.Unlock()
		return nil, fmt.Errorf("choice already tried")
	}
	state.TriedChoices[choiceIdx] = true
	choice := state.Scene.Choices[choiceIdx]
	state.History = append(state.History, fmt.Sprintf("L%d选[%s]", state.Level, choice.Text))
	state.ResolvedTurn = state.Turn
	epoch := h.adventureEpochValue(userID)

	if choice.Correct {
		state.Combo++
		if state.Combo > state.MaxCombo {
			state.MaxCombo = state.Combo
		}
		state.Score += state.Level*10 + state.Combo*5
		if state.Score > 100 {
			state.Score = 100
		}
		if state.Level >= state.TotalLevels {
			if !claimAdventureFinish(state, true) {
				state.ChoiceLock.Unlock()
				op.Unlock()
				return nil, ErrAdventureExpired
			}
			sess.Set("adventure_state", state)
			h.saveState(userID, state)
			state.ChoiceLock.Unlock()
			op.Unlock()
			return h.finishAdventureWebClaimed(userID, state, true, epoch, choice.Result)
		}

		nextLevel := state.Level + 1
		totalLevels, hp := state.TotalLevels, state.HP
		movieInfo := state.MovieInfo
		history := append([]string(nil), state.History...)
		state.Phase = AdventurePhaseGenerating
		sess.Set("adventure_state", state)
		h.saveState(userID, state)
		state.ChoiceLock.Unlock()
		op.Unlock()

		scene, err := h.adventureSvc.GenerateScene(movieInfo, nextLevel, totalLevels, history, hp)
		if err != nil {
			scene = h.adventureSvc.GenerateFallbackScene(movieInfo, nextLevel, totalLevels)
		}
		if scene == nil {
			return nil, fmt.Errorf("next scene generation failed")
		}

		op.Lock()
		defer op.Unlock()
		if !h.adventureEpochCurrent(userID, epoch) {
			return nil, ErrAdventureExpired
		}
		currentValue, ok := sess.Get("adventure_state")
		current, ok := currentValue.(*AdventureState)
		if !ok || current != state {
			return nil, ErrAdventureExpired
		}
		state.ChoiceLock.Lock()
		defer state.ChoiceLock.Unlock()
		if state.RunID != runID || state.Turn != turn || state.Phase != AdventurePhaseGenerating {
			return nil, ErrAdventureExpired
		}
		state.Level, state.Turn, state.Scene = nextLevel, turn+1, scene
		state.TriedChoices = make(map[int]bool)
		state.HintUsed = false
		state.Phase = AdventurePhasePlaying
		sess.Set("adventure_state", state)
		h.saveState(userID, state)
		view := adventureWebView(state)
		view.Message = choice.Result
		return view, nil
	}

	damage := AdventureDamage(state.Level, state.TotalLevels, choice.IsTrap)
	state.HP -= damage
	state.Combo = 0
	state.PerfectRun = false
	state.Mistakes++
	if state.HP <= 0 {
		state.HP = 0
		if !claimAdventureFinish(state, false) {
			state.ChoiceLock.Unlock()
			op.Unlock()
			return nil, ErrAdventureExpired
		}
		sess.Set("adventure_state", state)
		h.saveState(userID, state)
		state.ChoiceLock.Unlock()
		op.Unlock()
		return h.finishAdventureWebClaimed(userID, state, false, epoch, choice.Result)
	}
	sess.Set("adventure_state", state)
	h.saveState(userID, state)
	view := adventureWebView(state)
	view.Message = choice.Result
	state.ChoiceLock.Unlock()
	op.Unlock()
	return view, nil
}

func (h *AdventureHandler) WebHint(userID int64, runID string, turn int) (*AdventureWebView, error) {
	if h == nil || h.sessionMgr == nil {
		return nil, ErrAdventureNotFound
	}
	op := h.adventureOp(userID)
	op.Lock()
	defer op.Unlock()
	sess := h.sessionMgr.GetOrCreate(userID)
	state := h.getOrRestoreState(userID, sess)
	if state == nil {
		return nil, ErrAdventureNotFound
	}
	state.ChoiceLock.Lock()
	defer state.ChoiceLock.Unlock()
	if !validateAdventureCallback(state, map[string]string{"run": runID, "turn": fmt.Sprint(turn)}, AdventurePhasePlaying) {
		return nil, ErrAdventureExpired
	}
	if state.HintUsed || state.Scene == nil {
		return nil, fmt.Errorf("hint unavailable")
	}
	newHP, ok := ApplyAdventureHint(state.HP, state.Level)
	if !ok {
		return nil, fmt.Errorf("not enough hp")
	}
	state.HP, state.HintUsed, state.PerfectRun = newHP, true, false
	state.HintsUsed++
	state.Score -= 5
	if state.Score < 0 {
		state.Score = 0
	}
	sess.Set("adventure_state", state)
	h.saveState(userID, state)
	view := adventureWebView(state)
	view.Hint = state.Scene.Hint
	return view, nil
}

func (h *AdventureHandler) WebQuit(userID int64) {
	if h == nil {
		return
	}
	op := h.adventureOp(userID)
	op.Lock()
	defer op.Unlock()
	var state *AdventureState
	var sess *session.Session
	if h.sessionMgr != nil {
		sess = h.sessionMgr.GetOrCreate(userID)
		if sess != nil {
			if value, ok := sess.Get("adventure_state"); ok {
				state, _ = value.(*AdventureState)
			}
		}
	}
	if state == nil {
		state = h.loadState(userID)
	}
	if state != nil {
		state.ChoiceLock.Lock()
		defer state.ChoiceLock.Unlock()
	}
	h.nextAdventureEpoch(userID)
	if state != nil {
		state.InProgress = false
		state.Phase = AdventurePhaseFinished
		// 先写终态；即使后续 DELETE 失败，旧局也不会在重启后恢复。
		h.saveState(userID, state)
	}
	if sess != nil {
		sess.Delete("adventure_state")
	}
	h.removeState(userID)
}

func (h *AdventureHandler) finishAdventureWebClaimed(userID int64, state *AdventureState, success bool, epoch uint64, message string) (*AdventureWebView, error) {
	result, err := h.adventureSvc.GenerateEndScene(state.MovieInfo, state.History, success, state.HP, state.MaxCombo, state.TotalLevels)
	if err != nil {
		result = h.adventureSvc.GenerateFallbackResult(state.MovieInfo, success, state.HP, state.Level-1, state.TotalLevels)
	}
	result = finalizeAdventureResult(state, success, result)

	op := h.adventureOp(userID)
	op.Lock()
	if !h.adventureEpochCurrent(userID, epoch) || h.sessionMgr == nil {
		op.Unlock()
		return nil, ErrAdventureExpired
	}
	sess := h.sessionMgr.GetOrCreate(userID)
	currentValue, ok := sess.Get("adventure_state")
	current, ok := currentValue.(*AdventureState)
	if !ok || current != state {
		op.Unlock()
		return nil, ErrAdventureExpired
	}
	state.ChoiceLock.Lock()
	if state.Phase != AdventurePhaseFinishing || !state.FinishClaimed {
		state.ChoiceLock.Unlock()
		op.Unlock()
		return nil, ErrAdventureExpired
	}
	state.Success, state.InProgress, state.Phase = success, false, AdventurePhaseFinished
	sess.Set("adventure_state", state)
	h.saveState(userID, state)
	view := adventureWebView(state)
	view.Message = message
	state.ChoiceLock.Unlock()
	op.Unlock()

	if h.socialDB != nil {
		_ = h.socialDB.SaveAdventureRecord(userID, h.getUserName(userID), state.MovieInfo.Title, state.MovieInfo.Year,
			result.Score, result.Grade, state.MaxCombo, state.HP, state.Level-1, state.TotalLevels, state.PerfectRun, success)
	}
	if success && h.onAdventureSuccess != nil {
		h.onAdventureSuccess(userID, userID, state.MovieInfo.Title, state.MovieInfo.Year, state.MovieInfo.TMDBID,
			normalizeAdventureMediaType(state.MovieInfo.MediaType), state.MovieInfo.Genres, result.Score, result.Grade)
	}
	return view, nil
}
