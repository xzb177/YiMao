package handlers

import (
	"errors"
	"fmt"
	"strings"
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
	state := h.getOrRestoreState(userID, h.sessionMgr.GetOrCreate(userID))
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
	h.mu.Lock()
	if h.generating[userID] {
		h.mu.Unlock()
		return nil, ErrAdventureBusy
	}
	h.generating[userID] = true
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.generating, userID)
		h.mu.Unlock()
	}()

	movieInfo, err := h.adventureSvc.SearchMovieInfo(movieName)
	if err != nil || movieInfo == nil {
		return nil, fmt.Errorf("movie lookup failed: %w", err)
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
	sess := h.sessionMgr.GetOrCreate(userID)
	sess.Set("adventure_state", state)
	h.saveState(userID, state)
	return adventureWebView(state), nil
}

func (h *AdventureHandler) WebChoice(userID int64, runID string, turn, choiceIdx int) (*AdventureWebView, error) {
	if h == nil || h.sessionMgr == nil {
		return nil, ErrAdventureNotFound
	}
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
	if state.Scene == nil || choiceIdx < 0 || choiceIdx >= len(state.Scene.Choices) {
		return nil, fmt.Errorf("invalid choice")
	}
	if state.TriedChoices[choiceIdx] {
		return nil, fmt.Errorf("choice already tried")
	}
	state.TriedChoices[choiceIdx] = true
	choice := state.Scene.Choices[choiceIdx]
	state.History = append(state.History, fmt.Sprintf("L%d选[%s]", state.Level, choice.Text))
	state.ResolvedTurn = state.Turn

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
			claimAdventureFinish(state, true)
			h.finishAdventureWeb(userID, state, true)
			sess.Set("adventure_state", state)
			h.saveState(userID, state)
			view := adventureWebView(state)
			view.Message = choice.Result
			return view, nil
		}
		nextLevel := state.Level + 1
		scene, err := h.adventureSvc.GenerateScene(state.MovieInfo, nextLevel, state.TotalLevels, state.History, state.HP)
		if err != nil {
			scene = h.adventureSvc.GenerateFallbackScene(state.MovieInfo, nextLevel, state.TotalLevels)
		}
		if scene == nil {
			return nil, fmt.Errorf("next scene generation failed")
		}
		state.Level, state.Turn, state.Scene = nextLevel, state.Turn+1, scene
		state.TriedChoices = make(map[int]bool)
		state.HintUsed = false
		state.Phase = AdventurePhasePlaying
	} else {
		damage := AdventureDamage(state.Level, state.TotalLevels, choice.IsTrap)
		state.HP -= damage
		state.Combo = 0
		state.PerfectRun = false
		state.Mistakes++
		if state.HP <= 0 {
			state.HP = 0
			claimAdventureFinish(state, false)
			h.finishAdventureWeb(userID, state, false)
		}
	}
	sess.Set("adventure_state", state)
	h.saveState(userID, state)
	view := adventureWebView(state)
	view.Message = choice.Result
	return view, nil
}

func (h *AdventureHandler) WebHint(userID int64, runID string, turn int) (*AdventureWebView, error) {
	if h == nil || h.sessionMgr == nil {
		return nil, ErrAdventureNotFound
	}
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
	if h.sessionMgr != nil {
		if sess := h.sessionMgr.GetOrCreate(userID); sess != nil {
			sess.Delete("adventure_state")
		}
	}
	h.removeState(userID)
}

func (h *AdventureHandler) finishAdventureWeb(userID int64, state *AdventureState, success bool) {
	result, err := h.adventureSvc.GenerateEndScene(state.MovieInfo, state.History, success, state.HP, state.MaxCombo, state.TotalLevels)
	if err != nil {
		result = h.adventureSvc.GenerateFallbackResult(state.MovieInfo, success, state.HP, state.Level-1, state.TotalLevels)
	}
	result = finalizeAdventureResult(state, success, result)
	state.Success, state.InProgress, state.Phase = success, false, AdventurePhaseFinished
	if h.socialDB != nil {
		_ = h.socialDB.SaveAdventureRecord(userID, h.getUserName(userID), state.MovieInfo.Title, state.MovieInfo.Year,
			result.Score, result.Grade, state.MaxCombo, state.HP, state.Level-1, state.TotalLevels, state.PerfectRun, success)
	}
	if success && h.onAdventureSuccess != nil {
		h.onAdventureSuccess(userID, userID, state.MovieInfo.Title, state.MovieInfo.Year, state.MovieInfo.TMDBID,
			normalizeAdventureMediaType(state.MovieInfo.MediaType), state.MovieInfo.Genres, result.Score, result.Grade)
	}
}
