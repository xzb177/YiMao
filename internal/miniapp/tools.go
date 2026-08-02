package miniapp

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xzb177/yimao/internal/handlers"
	"github.com/xzb177/yimao/internal/services"
)

type issueView struct {
	ID          int64                `json:"id"`
	Title       string               `json:"title"`
	Description string               `json:"description"`
	Status      services.IssueStatus `json:"status"`
	MediaTitle  string               `json:"media_title,omitempty"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
	Replies     []issueReplyView     `json:"replies,omitempty"`
}

type issueReplyView struct {
	AuthorName string    `json:"author_name"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

type wishView struct {
	ID          int64      `json:"id"`
	TMDBID      int        `json:"tmdb_id"`
	MediaType   string     `json:"media_type"`
	Title       string     `json:"title"`
	Year        int        `json:"year,omitempty"`
	Season      int        `json:"season,omitempty"`
	State       string     `json:"state"`
	WisherCount int        `json:"wisher_count"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	NotifiedAt  *time.Time `json:"notified_at,omitempty"`
}

func (s *Server) handleIssues(w http.ResponseWriter, r *http.Request) {
	user, ok := s.auth(w, r)
	if !ok {
		return
	}
	if s.deps.Issues == nil {
		http.Error(w, "问题反馈暂时不可用", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		issues := s.deps.Issues.GetUserIssues(user.ID)
		sort.Slice(issues, func(i, j int) bool { return issues[i].UpdatedAt.After(issues[j].UpdatedAt) })
		views := make([]issueView, 0, len(issues))
		for _, issue := range issues {
			replies := make([]issueReplyView, 0, len(issue.Replies))
			for _, reply := range issue.Replies {
				replies = append(replies, issueReplyView{AuthorName: reply.AuthorName, Content: reply.Content, CreatedAt: reply.CreatedAt})
			}
			views = append(views, issueView{ID: issue.ID, Title: issue.Title, Description: issue.Description,
				Status: issue.Status, MediaTitle: issue.MediaTitle, CreatedAt: issue.CreatedAt, UpdatedAt: issue.UpdatedAt,
				Replies: replies})
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": views})
	case http.MethodPost:
		var body struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			MediaType   string `json:"media_type"`
			MediaID     string `json:"media_id"`
			MediaTitle  string `json:"media_title"`
		}
		if decodeJSONBody(w, r, &body, 16<<10) != nil {
			http.Error(w, "反馈内容格式不正确", http.StatusBadRequest)
			return
		}
		body.Title, body.Description = strings.TrimSpace(body.Title), strings.TrimSpace(body.Description)
		if body.Title == "" || body.Description == "" || len([]rune(body.Title)) > 80 || len([]rune(body.Description)) > 2000 {
			http.Error(w, "请填写标题和问题描述", http.StatusBadRequest)
			return
		}
		issue, err := s.deps.Issues.CreateIssue(user.ID, displayName(user), body.Title, body.Description,
			strings.TrimSpace(body.MediaType), strings.TrimSpace(body.MediaID), strings.TrimSpace(body.MediaTitle))
		if err != nil {
			http.Error(w, "反馈提交失败，请稍后再试", http.StatusInternalServerError)
			return
		}
		s.notifyIssueAdmins(issue, user)
		writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "id": issue.ID, "status": issue.Status})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) notifyIssueAdmins(issue *services.Issue, user AuthUser) {
	if s.deps.Telegram == nil || s.deps.Admins == nil || issue == nil {
		return
	}
	message := fmt.Sprintf("问题反馈 #%d\n\n%s\n\n%s\n\n来自：%s", issue.ID, issue.Title, issue.Description, displayName(user))
	for _, adminID := range s.deps.Admins.GetAdminIDs() {
		_, _ = s.deps.Telegram.SendMessage(adminID, message, "", nil)
	}
}

func displayName(user AuthUser) string {
	name := strings.TrimSpace(strings.Join([]string{user.FirstName, user.LastName}, " "))
	if name == "" {
		name = user.Username
	}
	if name == "" {
		name = strconv.FormatInt(user.ID, 10)
	}
	return name
}

func (s *Server) handleWishes(w http.ResponseWriter, r *http.Request) {
	user, ok := s.auth(w, r)
	if !ok {
		return
	}
	if s.deps.Wishes == nil {
		http.Error(w, "许愿池暂时不可用", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := s.deps.Wishes.ListForWisher(user.ID)
		if err != nil {
			http.Error(w, "许愿记录暂时无法加载", http.StatusInternalServerError)
			return
		}
		views := make([]wishView, 0, len(items))
		for _, item := range items {
			key := services.CanonicalKey(item.TmdbID, item.ImdbID, item.MediaType, item.Season)
			views = append(views, wishView{ID: item.ID, TMDBID: item.TmdbID, MediaType: item.MediaType,
				Title: item.Title, Year: item.Year, Season: item.Season, State: item.State,
				WisherCount: s.deps.Wishes.CountWishers(key), CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, NotifiedAt: item.NotifiedAt})
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": views})
	case http.MethodPost:
		s.createWish(w, r, user)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) createWish(w http.ResponseWriter, r *http.Request, user AuthUser) {
	if s.deps.TMDB == nil || s.deps.MoviePilot == nil {
		http.Error(w, "许愿校验服务暂时不可用", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		TMDBID    int    `json:"tmdb_id"`
		MediaType string `json:"type"`
		Season    int    `json:"season"`
	}
	if decodeJSONBody(w, r, &body, 8<<10) != nil || body.TMDBID <= 0 ||
		(body.MediaType != "movie" && body.MediaType != "tv") || (body.MediaType == "movie" && body.Season != 0) ||
		(body.MediaType == "tv" && body.Season <= 0) {
		http.Error(w, "许愿参数不完整", http.StatusBadRequest)
		return
	}
	mediaType := services.MediaTypeMovie
	if body.MediaType == "tv" {
		mediaType = services.MediaTypeTV
	}
	_, exists, err := s.deps.MoviePilot.FindExistingSubscription(body.TMDBID, mediaType, body.Season)
	if err != nil {
		http.Error(w, "暂时无法核验已有订阅", http.StatusServiceUnavailable)
		return
	}
	if exists {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "status": "subscribed", "message": "已经在找这部了，不用重复许愿"})
		return
	}
	media, err := s.deps.TMDB.GetMediaByType(body.TMDBID, body.MediaType)
	if err != nil || media == nil {
		http.Error(w, "影视信息校验失败", http.StatusBadGateway)
		return
	}
	title, date := media.Title, media.ReleaseDate
	if body.MediaType == "tv" {
		title, date = media.Name, media.FirstAirDate
	}
	if title == "" {
		http.Error(w, "影视信息校验失败", http.StatusBadGateway)
		return
	}
	year := 0
	if len(date) >= 4 {
		year, _ = strconv.Atoi(date[:4])
	}
	item := &services.WishItem{UserID: user.ID, TmdbID: body.TMDBID, MediaType: body.MediaType,
		Title: title, Year: year, Season: body.Season}
	result, err := s.deps.Wishes.AddWish(item)
	if err != nil {
		http.Error(w, "许愿提交失败，请稍后再试", http.StatusInternalServerError)
		return
	}
	if result.OverGlobal || result.OverPerUser {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "status": "full", "message": "许愿池暂时满了"})
		return
	}
	key := services.CanonicalKey(body.TMDBID, "", body.MediaType, body.Season)
	status := "created"
	if result.Duplicate {
		status = "joined"
	}
	items, listErr := s.deps.Wishes.ListForWisher(user.ID)
	if listErr != nil {
		http.Error(w, "许愿记录暂时无法加载", http.StatusInternalServerError)
		return
	}
	visible := false
	for _, item := range items {
		if item.TmdbID == body.TMDBID && item.MediaType == body.MediaType && item.Season == body.Season {
			visible = true
			break
		}
	}
	if !visible {
		http.Error(w, "许愿已记录，但当前列表尚未同步，请重试", http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "status": status, "wisher_count": s.deps.Wishes.CountWishers(key)})
}

func (s *Server) handleAdventure(w http.ResponseWriter, r *http.Request) {
	user, ok := s.auth(w, r)
	if !ok {
		return
	}
	if s.deps.Adventure == nil {
		http.Error(w, "大冒险暂时不可用", http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{"state": s.deps.Adventure.WebCurrent(user.ID)})
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Method == http.MethodDelete {
		s.deps.Adventure.WebQuit(user.ID)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	var body struct {
		Action string `json:"action"`
		Movie  string `json:"movie"`
		RunID  string `json:"run_id"`
		Turn   int    `json:"turn"`
		Choice int    `json:"choice"`
	}
	if decodeJSONBody(w, r, &body, 8<<10) != nil {
		http.Error(w, "操作参数不完整", http.StatusBadRequest)
		return
	}
	var state *handlers.AdventureWebView
	var err error
	switch body.Action {
	case "start":
		state, err = s.deps.Adventure.WebStart(user.ID, body.Movie)
	case "choice":
		state, err = s.deps.Adventure.WebChoice(user.ID, body.RunID, body.Turn, body.Choice)
	case "hint":
		state, err = s.deps.Adventure.WebHint(user.ID, body.RunID, body.Turn)
	default:
		http.Error(w, "未知操作", http.StatusBadRequest)
		return
	}
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, handlers.ErrAdventureNotFound) {
			status = http.StatusNotFound
		} else if errors.Is(err, handlers.ErrAdventureBusy) {
			status = http.StatusTooManyRequests
		}
		http.Error(w, adventureErrorText(err), status)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": state})
}

func adventureErrorText(err error) string {
	switch {
	case errors.Is(err, handlers.ErrAdventureNotFound):
		return "没有进行中的冒险"
	case errors.Is(err, handlers.ErrAdventureExpired):
		return "这一幕已经结束，请刷新当前进度"
	case errors.Is(err, handlers.ErrAdventureBusy):
		return "正在生成场景，请稍等"
	default:
		return "冒险场景暂时生成失败，请稍后再试"
	}
}
