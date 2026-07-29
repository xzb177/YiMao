package miniapp

import (
	"embed"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/pkg/logger"
)

//go:embed web/*
var webFS embed.FS

type Deps struct {
	BotToken   string
	MoviePilot *services.MoviePilotClient
	TMDB       *services.TMDBClient
	Reviews    *services.ReviewService
	Quota      *services.QuotaService
	Submission *services.RequestSubmissionService
	MaxAuthAge time.Duration
}

type Server struct{ deps Deps }

func NewServer(deps Deps) *Server {
	if deps.MaxAuthAge <= 0 {
		deps.MaxAuthAge = 24 * time.Hour
	}
	return &Server{deps: deps}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/miniapp", s.handleIndex)
	mux.HandleFunc("/miniapp/", s.handleIndex)
	mux.HandleFunc("/api/miniapp/v1/search", s.handleSearch)
	mux.HandleFunc("/api/miniapp/v1/detail", s.handleDetail)
	mux.HandleFunc("/api/miniapp/v1/discover", s.handleDiscover)
	mux.HandleFunc("/api/miniapp/v1/me", s.handleMe)
	// Request submission is intentionally not exposed until it uses the existing
	// quota/review transaction, not a direct MoviePilot write.
	mux.HandleFunc("/api/miniapp/v1/request", s.handleRequest)
	return mux
}

func (s *Server) auth(w http.ResponseWriter, r *http.Request) (AuthUser, bool) {
	raw := r.Header.Get("X-Telegram-Init-Data")
	if raw == "" {
		raw = r.URL.Query().Get("initData")
	}
	user, err := ValidateInitData(raw, s.deps.BotToken, s.deps.MaxAuthAge)
	if err != nil {
		logger.Info("[MiniApp] initData rejected: %v", err)
		http.Error(w, "Mini App 身份验证失败", http.StatusUnauthorized)
		return AuthUser{}, false
	}
	return user, true
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/miniapp" && r.URL.Path != "/miniapp/" {
		http.NotFound(w, r)
		return
	}
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "Mini App unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.auth(w, r); !ok {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.deps.TMDB == nil {
		http.Error(w, "发现页暂时不可用", http.StatusServiceUnavailable)
		return
	}
	movies, movieErr := s.deps.TMDB.GetTrendingMovies("week")
	tv, tvErr := s.deps.TMDB.GetTrendingTV("week")
	if movieErr != nil && tvErr != nil {
		http.Error(w, "发现页暂时不可用", http.StatusBadGateway)
		return
	}
	items := make([]services.TMDBTrendingMediaInfo, 0, 16)
	if movies != nil {
		items = append(items, movies.Results...)
	}
	if tv != nil {
		items = append(items, tv.Results...)
	}
	if len(items) > 16 {
		items = items[:16]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := s.auth(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	requests := []*services.ReviewRequest{}
	if s.deps.Reviews != nil {
		requests = s.deps.Reviews.GetUserRequests(user.ID)
	}
	if len(requests) > 20 {
		requests = requests[:20]
	}
	var quota *services.UserQuota
	if s.deps.Quota != nil {
		quota = s.deps.Quota.GetQuotaInfo(user.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "requests": requests, "quota": quota})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.auth(w, r); !ok {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	if query == "" {
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{}})
		return
	}
	result, err := s.deps.MoviePilot.SearchMediaWithCount(query, page, 12)
	if err != nil {
		http.Error(w, "搜索暂时不可用", http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleDetail(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.auth(w, r); !ok {
		return
	}
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	if id <= 0 {
		http.Error(w, "无效的影视 ID", http.StatusBadRequest)
		return
	}
	mediaType := services.MediaTypeMovie
	isTV := false
	if typeName := r.URL.Query().Get("type"); typeName == "tv" || typeName == "电视剧" {
		mediaType = services.MediaTypeTV
		isTV = true
	}
	info, err := s.deps.MoviePilot.GetMediaInfo(id, mediaType)
	if err == nil && info != nil && (info.Title != "" || info.ID != 0) {
		writeJSON(w, http.StatusOK, info)
		return
	}
	// MoviePilot can return an empty media shell for a valid TMDB result that
	// has not been indexed locally. Details browsing must still work for search
	// results, so use the already configured TMDB client as a read-only fallback.
	if s.deps.TMDB != nil {
		kind := "movie"
		if isTV {
			kind = "tv"
		}
		fallback, fallbackErr := s.deps.TMDB.GetMediaByType(id, kind)
		if fallbackErr == nil && fallback != nil && (fallback.Title != "" || fallback.Name != "") {
			title := fallback.Title
			if title == "" {
				title = fallback.Name
			}
			writeJSON(w, http.StatusOK, services.MediaInfo{
				ID: id, Title: title, Overview: fallback.Overview,
				Poster:   s.deps.TMDB.GetPosterURL(fallback.PosterPath),
				Backdrop: s.deps.TMDB.GetPosterURL(fallback.BackdropPath),
				Rating:   fallback.VoteAverage, Type: mediaType,
			})
			return
		}
	}
	if err != nil {
		logger.Info("[MiniApp] detail failed: id=%d type=%s err=%v", id, mediaType, err)
	}
	http.Error(w, "详情暂时不可用", http.StatusBadGateway)
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	user, ok := s.auth(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Name   string `json:"name"`
		Year   int    `json:"year"`
		ID     int    `json:"tmdb_id"`
		Type   string `json:"type"`
		Season int    `json:"season"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.ID <= 0 || strings.TrimSpace(body.Name) == "" {
		http.Error(w, "请求参数不完整", http.StatusBadRequest)
		return
	}
	mediaType := services.MediaTypeMovie
	if body.Type == "tv" {
		mediaType = services.MediaTypeTV
	} else if body.Type != "movie" {
		http.Error(w, "媒体类型无效", http.StatusBadRequest)
		return
	}
	if s.deps.Submission == nil {
		http.Error(w, "求片服务未就绪", http.StatusServiceUnavailable)
		return
	}
	result, err := s.deps.Submission.SubmitResult(services.RequestSubmission{
		BusinessType: "request", TelegramID: user.ID, TelegramName: user.FirstName,
		TmdbID: body.ID, MediaTitle: strings.TrimSpace(body.Name), MediaYear: body.Year,
		MediaType: mediaType, Season: body.Season, Origin: "miniapp", UseQuota: true,
	})
	if err != nil {
		http.Error(w, "提交失败，请稍后再试", http.StatusBadGateway)
		return
	}
	requestID := ""
	if result.Review != nil {
		requestID = result.Review.RequestID
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "status": result.Status, "request_id": requestID})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
