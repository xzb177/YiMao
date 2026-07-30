package miniapp

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
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

type Server struct {
	deps         Deps
	dynamicMu    sync.Mutex
	dynamicAt    time.Time
	dynamicCache map[string]any
}

type detailSeason struct {
	Number       int    `json:"number"`
	Name         string `json:"name"`
	EpisodeCount int    `json:"episode_count"`
	AirDate      string `json:"air_date,omitempty"`
	Poster       string `json:"poster_path,omitempty"`
}

type detailStatus struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

type quotaView struct {
	MovieUsed  int `json:"movie_used"`
	MovieLimit int `json:"movie_limit"`
	TVUsed     int `json:"tv_used"`
	TVLimit    int `json:"tv_limit"`
}

type detailResponse struct {
	ID            int            `json:"tmdb_id"`
	Type          string         `json:"type"`
	Title         string         `json:"title"`
	OriginalTitle string         `json:"original_title,omitempty"`
	Year          string         `json:"year,omitempty"`
	ReleaseDate   string         `json:"release_date,omitempty"`
	Overview      string         `json:"overview,omitempty"`
	Poster        string         `json:"poster_path,omitempty"`
	Backdrop      string         `json:"backdrop_path,omitempty"`
	Rating        float64        `json:"vote_average,omitempty"`
	VoteCount     int            `json:"vote_count,omitempty"`
	Runtime       int            `json:"runtime,omitempty"`
	Genres        []string       `json:"genres,omitempty"`
	Seasons       []detailSeason `json:"seasons,omitempty"`
	Status        detailStatus   `json:"media_status"`
	Quota         *quotaView     `json:"quota,omitempty"`
}

type userRequestView struct {
	RequestID  string             `json:"request_id"`
	TmdbID     int                `json:"tmdb_id"`
	Title      string             `json:"media_title"`
	Year       int                `json:"media_year,omitempty"`
	Type       services.MediaType `json:"media_type"`
	Season     int                `json:"season,omitempty"`
	Poster     string             `json:"poster_path,omitempty"`
	Status     string             `json:"status"`
	StatusText string             `json:"status_text"`
	Group      string             `json:"status_group"`
	CreatedAt  time.Time          `json:"created_at"`
	CanCancel  bool               `json:"can_cancel"`
	Note       string             `json:"note,omitempty"`
}

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
	mux.HandleFunc("/api/miniapp/v1/dynamic", s.handleDynamic)
	mux.HandleFunc("/api/miniapp/v1/me", s.handleMe)
	// Request submission is intentionally not exposed until it uses the existing
	// quota/review transaction, not a direct MoviePilot write.
	mux.HandleFunc("/api/miniapp/v1/request", s.handleRequest)
	mux.HandleFunc("/api/miniapp/v1/request/cancel", s.handleCancelRequest)
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
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
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
	movieItems := []services.TMDBTrendingMediaInfo{}
	tvItems := []services.TMDBTrendingMediaInfo{}
	if movies != nil {
		movieItems = movies.Results
		if len(movieItems) > 10 {
			movieItems = movieItems[:10]
		}
	}
	if tv != nil {
		tvItems = tv.Results
		if len(tvItems) > 10 {
			tvItems = tvItems[:10]
		}
	}
	featured := make([]services.TMDBTrendingMediaInfo, 0, 2)
	if len(movieItems) > 0 {
		featured = append(featured, movieItems[0])
	}
	if len(tvItems) > 0 {
		featured = append(featured, tvItems[0])
	}
	writeJSON(w, http.StatusOK, map[string]any{"featured": featured, "movies": movieItems, "tv": tvItems})
}

type dynamicMedia struct {
	TMDBID int     `json:"tmdb_id"`
	Type   string  `json:"type"`
	Title  string  `json:"title"`
	Year   int     `json:"year,omitempty"`
	Season int     `json:"season,omitempty"`
	Poster string  `json:"poster_path,omitempty"`
	Rating float64 `json:"vote_average,omitempty"`
}

func (s *Server) handleDynamic(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.auth(w, r); !ok {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.dynamicMu.Lock()
	defer s.dynamicMu.Unlock()
	if time.Since(s.dynamicAt) < 2*time.Minute && s.dynamicCache != nil {
		writeJSON(w, http.StatusOK, s.dynamicCache)
		return
	}
	response := map[string]any{"recently_added": []dynamicMedia{}, "just_available": []dynamicMedia{}, "recent_requests": []dynamicMedia{}}
	if s.deps.MoviePilot != nil {
		if items, err := s.deps.MoviePilot.EmbyRecentlyAdded(8); err == nil {
			public := make([]dynamicMedia, 0, len(items))
			for _, item := range items {
				public = append(public, dynamicMedia{TMDBID: item.TMDBID, Type: item.Type, Title: item.Name, Year: item.Year, Rating: item.Rating})
			}
			response["recently_added"] = public
		}
	}
	if s.deps.Reviews != nil {
		reviews := s.deps.Reviews.GetAllRequests()
		sort.Slice(reviews, func(i, j int) bool { return reviews[i].CreatedAt.After(reviews[j].CreatedAt) })
		seenRecent := map[string]bool{}
		recent := []dynamicMedia{}
		for _, review := range reviews {
			if review == nil || review.NormalizedBusinessType() != services.BusinessTypeRequest || review.TmdbID <= 0 {
				continue
			}
			kind := "movie"
			if review.MediaType == services.MediaTypeTV {
				kind = "tv"
			}
			key := fmt.Sprintf("%s:%d:%d", kind, review.TmdbID, review.Season)
			item := dynamicMedia{TMDBID: review.TmdbID, Type: kind, Title: review.MediaTitle, Year: review.MediaYear, Season: review.Season, Poster: review.PosterPath}
			if !seenRecent[key] && len(recent) < 10 && review.Status != "cancelled" && review.Status != "rejected" {
				seenRecent[key] = true
				recent = append(recent, item)
			}
		}
		sort.Slice(reviews, func(i, j int) bool { return completionTime(reviews[i]).After(completionTime(reviews[j])) })
		seenDone := map[string]bool{}
		done := []dynamicMedia{}
		for _, review := range reviews {
			if review == nil || review.NormalizedBusinessType() != services.BusinessTypeRequest || review.TmdbID <= 0 || !(review.EmbyExists || review.SubscriptionState == services.StateCompleted || review.CompletedNoticeAt != nil) {
				continue
			}
			kind := "movie"
			if review.MediaType == services.MediaTypeTV {
				kind = "tv"
			}
			key := fmt.Sprintf("%s:%d:%d", kind, review.TmdbID, review.Season)
			if seenDone[key] || len(done) >= 8 {
				continue
			}
			seenDone[key] = true
			done = append(done, dynamicMedia{TMDBID: review.TmdbID, Type: kind, Title: review.MediaTitle, Year: review.MediaYear, Season: review.Season, Poster: review.PosterPath})
		}
		response["recent_requests"], response["just_available"] = recent, done
	}
	s.dynamicAt, s.dynamicCache = time.Now(), response
	writeJSON(w, http.StatusOK, response)
}

func completionTime(review *services.ReviewRequest) time.Time {
	if review == nil {
		return time.Time{}
	}
	if review.CompletedNoticeAt != nil {
		return *review.CompletedNoticeAt
	}
	if !review.ReviewedAt.IsZero() {
		return review.ReviewedAt
	}
	return review.CreatedAt
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
	reviews := []*services.ReviewRequest{}
	if s.deps.Reviews != nil {
		reviews = s.deps.Reviews.GetUserRequests(user.ID)
	}
	if len(reviews) > 20 {
		reviews = reviews[:20]
	}
	requests := make([]userRequestView, 0, len(reviews))
	for _, review := range reviews {
		if review == nil || review.NormalizedBusinessType() != services.BusinessTypeRequest {
			continue
		}
		status, text, group := userRequestStatus(review)
		note := ""
		if review.Status == "rejected" {
			note = "未通过审核，可稍后换个版本或作品重试"
		} else if review.Status == "cancelled" {
			note = "已由你主动撤回"
		} else if review.Stuck {
			note = "系统正在自动重试，无需重复提交"
		}
		requests = append(requests, userRequestView{RequestID: review.RequestID, TmdbID: review.TmdbID, Title: review.MediaTitle, Year: review.MediaYear, Type: review.MediaType, Season: review.Season, Poster: review.PosterPath, Status: status, StatusText: text, Group: group, CreatedAt: review.CreatedAt, CanCancel: review.Status == "pending", Note: note})
	}
	var quota *quotaView
	if s.deps.Quota != nil {
		quota = publicQuota(s.deps.Quota.GetQuotaInfo(user.ID))
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "requests": requests, "quota": quota})
}

func publicQuota(quota *services.UserQuota) *quotaView {
	if quota == nil {
		return nil
	}
	return &quotaView{MovieUsed: quota.MovieUsed, MovieLimit: quota.MovieLimit, TVUsed: quota.TVUsed, TVLimit: quota.TVLimit}
}

func userRequestStatus(review *services.ReviewRequest) (string, string, string) {
	if review.Stuck {
		return "stuck", "同步重试", "active"
	}
	if review.Status == "pending" {
		return "pending", "待审核", "pending"
	}
	if review.Status == "rejected" {
		return "rejected", "未通过", "done"
	}
	if review.Status == "cancelled" {
		return "cancelled", "已撤回", "done"
	}
	if review.EmbyExists || review.SubscriptionState == services.StateCompleted || review.CompletedNoticeAt != nil {
		return "completed", "已入库", "done"
	}
	if review.Status == "approved" {
		text := services.GetStateText(review.SubscriptionState)
		if review.SubscriptionState == "" {
			text = "处理中"
		}
		return "approved", strings.TrimSpace(strings.TrimLeft(text, "⏳🔄🔍📥✅❌🚫")), "active"
	}
	return review.Status, "处理中", "active"
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
	upstreamCount := len(result.Results)
	filter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type")))
	if filter == "movie" || filter == "tv" {
		filtered := make([]services.SearchResult, 0, len(result.Results))
		for _, item := range result.Results {
			itemType := "movie"
			if item.Type == "tv" || item.Type == "电视剧" {
				itemType = "tv"
			}
			if itemType == filter {
				filtered = append(filtered, item)
			}
		}
		result.Results = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": result.Results, "page": page, "has_more": upstreamCount == 12})
}

func (s *Server) handleDetail(w http.ResponseWriter, r *http.Request) {
	user, ok := s.auth(w, r)
	if !ok {
		return
	}
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	if id <= 0 {
		http.Error(w, "无效的影视 ID", http.StatusBadRequest)
		return
	}
	kind := "movie"
	mediaType := services.MediaTypeMovie
	if typeName := r.URL.Query().Get("type"); typeName == "tv" || typeName == "电视剧" {
		kind, mediaType = "tv", services.MediaTypeTV
	}
	if s.deps.TMDB == nil {
		http.Error(w, "详情暂时不可用", http.StatusServiceUnavailable)
		return
	}
	view := detailResponse{ID: id, Type: kind, Status: detailStatus{Code: "available", Text: "可以求片"}}
	if kind == "tv" {
		d, err := s.deps.TMDB.GetTVDetailsWithSeasons(id)
		if err != nil || d == nil || d.Name == "" {
			http.Error(w, "详情暂时不可用", http.StatusBadGateway)
			return
		}
		view.Title, view.OriginalTitle, view.ReleaseDate = d.Name, d.OriginalName, d.FirstAirDate
		view.Overview, view.Poster, view.Backdrop = d.Overview, s.deps.TMDB.GetPosterURL(d.PosterPath), s.deps.TMDB.GetPosterURL(d.BackdropPath)
		view.Rating, view.VoteCount = d.VoteAverage, d.VoteCount
		for _, g := range d.Genres {
			view.Genres = append(view.Genres, g.Name)
		}
		for _, season := range d.Seasons {
			if season.SeasonNumber <= 0 {
				continue
			}
			view.Seasons = append(view.Seasons, detailSeason{Number: season.SeasonNumber, Name: season.Name, EpisodeCount: season.EpisodeCount, AirDate: season.AirDate, Poster: s.deps.TMDB.GetPosterURL(season.PosterPath)})
		}
	} else {
		d, err := s.deps.TMDB.GetMovieDetails(id)
		if err != nil || d == nil || d.Title == "" {
			http.Error(w, "详情暂时不可用", http.StatusBadGateway)
			return
		}
		view.Title, view.OriginalTitle, view.ReleaseDate = d.Title, d.OriginalTitle, d.ReleaseDate
		view.Overview, view.Poster, view.Backdrop = d.Overview, s.deps.TMDB.GetPosterURL(d.PosterPath), s.deps.TMDB.GetPosterURL(d.BackdropPath)
		view.Rating, view.VoteCount, view.Runtime = d.VoteAverage, d.VoteCount, d.Runtime
		for _, g := range d.Genres {
			view.Genres = append(view.Genres, g.Name)
		}
	}
	if len(view.ReleaseDate) >= 4 {
		view.Year = view.ReleaseDate[:4]
	}
	season, _ := strconv.Atoi(r.URL.Query().Get("season"))
	if mediaType == services.MediaTypeMovie {
		if exists, err := s.deps.MoviePilot.EmbyMediaAvailabilityByTMDB(id, mediaType); err == nil && exists {
			view.Status = detailStatus{Code: "in_library", Text: "库中可看"}
			if s.deps.Quota != nil {
				view.Quota = publicQuota(s.deps.Quota.GetQuotaInfo(user.ID))
			}
			writeJSON(w, http.StatusOK, view)
			return
		}
	} else if season > 0 {
		if exists, err := s.deps.MoviePilot.EmbyMediaAvailabilityByTMDBSeason(id, season); err == nil && exists {
			view.Status = detailStatus{Code: "in_library", Text: "这一季已入库"}
			if s.deps.Quota != nil {
				view.Quota = publicQuota(s.deps.Quota.GetQuotaInfo(user.ID))
			}
			writeJSON(w, http.StatusOK, view)
			return
		}
	}
	if existing, found, err := s.deps.MoviePilot.FindExistingSubscription(id, mediaType, season); err == nil && found {
		if kind == "movie" {
			view.Status = detailStatus{Code: "subscribed", Text: services.GetStateText(existing.State)}
		} else {
			// MoviePilot does not expose reliable season identity on every version.
			// Keep this informational and never claim the selected season is covered.
			view.Status = detailStatus{Code: "available", Text: "该剧已有订阅，可继续选择目标季"}
		}
	} else if s.deps.Reviews != nil {
		if own, found := s.deps.Reviews.HasActiveSimilarRequest(user.ID, id, mediaType, season); found {
			view.Status = detailStatus{Code: "requested", Text: map[string]string{"pending": "等待审核", "approved": "处理中"}[own.Status]}
			if view.Status.Text == "" {
				view.Status.Text = "已有求片"
			}
		}
	}
	if s.deps.Quota != nil {
		view.Quota = publicQuota(s.deps.Quota.GetQuotaInfo(user.ID))
	}
	writeJSON(w, http.StatusOK, view)
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
		ID     int    `json:"tmdb_id"`
		Type   string `json:"type"`
		Season int    `json:"season"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.ID <= 0 {
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
	if s.deps.Submission == nil || s.deps.TMDB == nil || s.deps.MoviePilot == nil {
		http.Error(w, "求片服务未就绪", http.StatusServiceUnavailable)
		return
	}
	if mediaType == services.MediaTypeMovie {
		if exists, err := s.deps.MoviePilot.EmbyMediaAvailabilityByTMDB(body.ID, mediaType); err == nil && exists {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "status": "in_library", "message": "库中已经有这部电影"})
			return
		}
	} else if body.Season > 0 {
		if exists, err := s.deps.MoviePilot.EmbyMediaAvailabilityByTMDBSeason(body.ID, body.Season); err == nil && exists {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "status": "in_library", "message": "这一季已经入库"})
			return
		}
	}
	mediaTitle, mediaYear, posterPath, overview := "", 0, "", ""
	if body.Type == "tv" {
		detail, err := s.deps.TMDB.GetTVDetails(body.ID)
		if err != nil || detail == nil || detail.Name == "" {
			http.Error(w, "影视信息校验失败", http.StatusBadGateway)
			return
		}
		mediaTitle, posterPath, overview = detail.Name, s.deps.TMDB.GetPosterURL(detail.PosterPath), detail.Overview
		if len(detail.FirstAirDate) >= 4 {
			mediaYear, _ = strconv.Atoi(detail.FirstAirDate[:4])
		}
	} else {
		detail, err := s.deps.TMDB.GetMovieDetails(body.ID)
		if err != nil || detail == nil || detail.Title == "" {
			http.Error(w, "影视信息校验失败", http.StatusBadGateway)
			return
		}
		mediaTitle, posterPath, overview = detail.Title, s.deps.TMDB.GetPosterURL(detail.PosterPath), detail.Overview
		if len(detail.ReleaseDate) >= 4 {
			mediaYear, _ = strconv.Atoi(detail.ReleaseDate[:4])
		}
	}
	result, err := s.deps.Submission.SubmitResult(services.RequestSubmission{
		BusinessType: "request", TelegramID: user.ID, TelegramName: user.FirstName,
		TmdbID: body.ID, MediaTitle: mediaTitle, MediaYear: mediaYear,
		MediaType: mediaType, Season: body.Season, PosterPath: posterPath, Overview: overview,
		Origin: "miniapp", UseQuota: true,
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

func (s *Server) handleCancelRequest(w http.ResponseWriter, r *http.Request) {
	user, ok := s.auth(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.deps.Reviews == nil || s.deps.Quota == nil {
		http.Error(w, "撤回服务未就绪", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		RequestID string `json:"request_id"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || strings.TrimSpace(body.RequestID) == "" {
		http.Error(w, "请求参数不完整", http.StatusBadRequest)
		return
	}
	review, exists := s.deps.Reviews.GetRequest(body.RequestID)
	if !exists || review == nil || review.TelegramID != user.ID || review.NormalizedBusinessType() != services.BusinessTypeRequest {
		http.Error(w, "求片记录不存在", http.StatusNotFound)
		return
	}
	if review.Status != "pending" {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "status": "not_cancellable", "message": "该请求已进入处理阶段，不能撤回"})
		return
	}
	if err := s.deps.Reviews.CancelByUser(review.RequestID, user.ID); err != nil {
		http.Error(w, "撤回失败，请稍后再试", http.StatusConflict)
		return
	}
	restored, err := s.deps.Reviews.RestoreQuotaOnce(review.RequestID, s.deps.Quota)
	if err != nil {
		logger.Info("[MiniApp] request cancelled but quota restore failed: request=%s user=%d err=%v", review.RequestID, user.ID, err)
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "status": "cancelled_quota_pending", "quota_restored": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "cancelled", "quota_restored": restored})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
