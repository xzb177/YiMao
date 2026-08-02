package miniapp

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/xzb177/yimao/internal/handlers"
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
	Carpool    *services.CarpoolService
	Submission *services.RequestSubmissionService
	Webhook    *services.WebhookService
	Issues     *services.IssueService
	Wishes     *services.WishService
	Adventure  *handlers.AdventureHandler
	Telegram   *services.TelegramClient
	Admins     *services.AdminService
	Assistant  *services.MiniAppAssistant
	MaxAuthAge time.Duration
}

type Server struct {
	deps               Deps
	dynamicMu          sync.Mutex
	dynamicAt          time.Time
	dynamicCache       map[string]any
	assistantMu        sync.Mutex
	assistantLimits    map[int64]assistantRateWindow
	assistantLastPurge time.Time
}

type assistantRateWindow struct {
	started time.Time
	count   int
}

type detailSeason struct {
	Number       int          `json:"number"`
	Name         string       `json:"name"`
	EpisodeCount int          `json:"episode_count"`
	AirDate      string       `json:"air_date,omitempty"`
	Poster       string       `json:"poster_path,omitempty"`
	Status       detailStatus `json:"status"`
}

type detailStatus struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

func seasonBaseStatus(airDate string, now time.Time) detailStatus {
	air, err := time.Parse("2006-01-02", airDate)
	if err != nil {
		return detailStatus{Code: "unknown", Text: "首播时间暂未确认"}
	}
	if air.After(now) {
		return detailStatus{Code: "upcoming", Text: "尚未播出"}
	}
	return detailStatus{Code: "available", Text: "可以求片"}
}

func applySeasonAvailability(seasons []detailSeason, available map[int]bool, availabilityErr error, activeRequest func(int) (*services.ReviewRequest, bool)) {
	for i := range seasons {
		// Emby 故障只影响已经播出且可求片的季，未来季的首播事实仍然有效。
		if availabilityErr != nil && seasons[i].Status.Code == "available" {
			seasons[i].Status = detailStatus{Code: "unknown", Text: "暂时无法确认"}
		} else if available[seasons[i].Number] {
			seasons[i].Status = detailStatus{Code: "in_library", Text: "已入库"}
		}
		if activeRequest == nil || seasons[i].Status.Code == "in_library" {
			continue
		}
		if own, found := activeRequest(seasons[i].Number); found {
			_, text, _ := userRequestStatus(own)
			seasons[i].Status = detailStatus{Code: "requested", Text: text}
		}
	}
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
	InWatchlist   bool           `json:"in_watchlist"`
}

type watchlistItemView struct {
	TMDBID int    `json:"tmdb_id"`
	Type   string `json:"type"`
	Title  string `json:"title,omitempty"`
	Year   string `json:"year,omitempty"`
	Poster string `json:"poster_path,omitempty"`
}

type progressEventView struct {
	Code string     `json:"code"`
	Text string     `json:"text"`
	At   *time.Time `json:"at,omitempty"`
}

type userRequestView struct {
	RequestID    string             `json:"request_id"`
	TmdbID       int                `json:"tmdb_id"`
	Title        string             `json:"media_title"`
	Year         int                `json:"media_year,omitempty"`
	Type         services.MediaType `json:"media_type"`
	Season       int                `json:"season,omitempty"`
	Poster       string             `json:"poster_path,omitempty"`
	Status       string             `json:"status"`
	StatusText   string             `json:"status_text"`
	Group        string             `json:"status_group"`
	CreatedAt    time.Time          `json:"created_at"`
	CanCancel    bool               `json:"can_cancel"`
	Note         string             `json:"note,omitempty"`
	BusinessType string             `json:"business_type"`
}

const (
	defaultSearchLimit         = 12
	maxSearchLimit             = 24
	maxSearchLookahead         = 3
	assistantRequestsPerMinute = 12
)

type searchResponseView struct {
	Results  []services.SearchResult `json:"results"`
	Page     int                     `json:"page"`
	Limit    int                     `json:"limit"`
	HasMore  bool                    `json:"has_more"`
	NextPage int                     `json:"next_page,omitempty"`
}

func NewServer(deps Deps) *Server {
	if deps.MaxAuthAge <= 0 {
		deps.MaxAuthAge = 24 * time.Hour
	}
	if deps.Assistant == nil {
		deps.Assistant = services.NewMiniAppAssistant(nil, 8*time.Second)
	}
	return &Server{deps: deps, assistantLimits: make(map[int64]assistantRateWindow)}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/miniapp", s.handleIndex)
	mux.HandleFunc("/miniapp/", s.handleIndex)
	mux.HandleFunc("/api/miniapp/v1/search", s.handleSearch)
	mux.HandleFunc("/api/miniapp/v1/assistant", s.handleAssistant)
	mux.HandleFunc("/api/miniapp/v1/detail", s.handleDetail)
	mux.HandleFunc("/api/miniapp/v1/discover", s.handleDiscover)
	mux.HandleFunc("/api/miniapp/v1/dynamic", s.handleDynamic)
	mux.HandleFunc("/api/miniapp/v1/me", s.handleMe)
	mux.HandleFunc("/api/miniapp/v1/watchlist", s.handleWatchlist)
	mux.HandleFunc("/api/miniapp/v1/progress", s.handleProgress)

	// All user submissions reuse the shared review transaction.
	mux.HandleFunc("/api/miniapp/v1/request", s.handleRequest)
	mux.HandleFunc("/api/miniapp/v1/wash", s.handleWash)
	mux.HandleFunc("/api/miniapp/v1/request/cancel", s.handleCancelRequest)
	mux.HandleFunc("/api/miniapp/v1/issues", s.handleIssues)
	mux.HandleFunc("/api/miniapp/v1/wishes", s.handleWishes)
	mux.HandleFunc("/api/miniapp/v1/adventure", s.handleAdventure)
	return mux
}

type assistantResponseView struct {
	services.MiniAppAssistantResult
	Items []services.SearchResult `json:"items"`
}

func (s *Server) handleAssistant(w http.ResponseWriter, r *http.Request) {
	user, ok := s.auth(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var input services.MiniAppAssistantInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	if err := decoder.Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "请求内容有误"})
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "请求内容有误"})
		return
	}
	input.Message = strings.TrimSpace(input.Message)
	if utf8.RuneCountInString(input.Message) < 1 || utf8.RuneCountInString(input.Message) > 500 || len(input.History) > 6 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "请输入 1 到 500 个字符，历史最多保留 6 条"})
		return
	}
	for index := range input.History {
		input.History[index].Role = strings.ToLower(strings.TrimSpace(input.History[index].Role))
		input.History[index].Content = strings.TrimSpace(input.History[index].Content)
		contentRunes := utf8.RuneCountInString(input.History[index].Content)
		if (input.History[index].Role != "user" && input.History[index].Role != "assistant") || contentRunes < 1 || contentRunes > 500 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "历史消息格式有误"})
			return
		}
	}
	if !s.allowAssistantRequest(user.ID, time.Now()) {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"message": "问得有点快，稍等一分钟再试"})
		return
	}

	result := s.deps.Assistant.Assist(r.Context(), input)
	response := assistantResponseView{MiniAppAssistantResult: result, Items: []services.SearchResult{}}
	if !result.Degraded && result.Query != "" && s.deps.MoviePilot != nil {
		search, err := s.deps.MoviePilot.SearchMediaWithCountContext(r.Context(), result.Query, 1, 6)
		if err == nil {
			response.Items = validAssistantSearchResults(search.Results, result.Type, 6)
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) allowAssistantRequest(userID int64, now time.Time) bool {
	const window = time.Minute
	s.assistantMu.Lock()
	defer s.assistantMu.Unlock()
	if s.assistantLimits == nil {
		s.assistantLimits = make(map[int64]assistantRateWindow)
	}
	if s.assistantLastPurge.IsZero() || now.Sub(s.assistantLastPurge) >= window {
		for id, entry := range s.assistantLimits {
			if now.Sub(entry.started) >= window {
				delete(s.assistantLimits, id)
			}
		}
		s.assistantLastPurge = now
	}
	entry := s.assistantLimits[userID]
	if entry.started.IsZero() || now.Sub(entry.started) >= window {
		s.assistantLimits[userID] = assistantRateWindow{started: now, count: 1}
		return true
	}
	if entry.count >= assistantRequestsPerMinute {
		return false
	}
	entry.count++
	s.assistantLimits[userID] = entry
	return true
}

func validAssistantSearchResults(results []services.SearchResult, requestedType string, limit int) []services.SearchResult {
	valid := make([]services.SearchResult, 0, min(limit, len(results)))
	for _, item := range results {
		if item.ID <= 0 {
			continue
		}
		typeName := ""
		switch strings.ToLower(strings.TrimSpace(item.Type)) {
		case "movie", "电影":
			typeName = "movie"
		case "tv", "电视剧":
			typeName = "tv"
		}
		if typeName == "" || (requestedType != "all" && typeName != requestedType) {
			continue
		}
		item.Type = typeName
		valid = append(valid, item)
		if len(valid) == limit {
			break
		}
	}
	return valid
}

func (s *Server) auth(w http.ResponseWriter, r *http.Request) (AuthUser, bool) {
	raw := r.Header.Get("X-Telegram-Init-Data")
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

type dynamicPosterLookup func(tmdbID int, mediaType string) (string, error)

func fillDynamicPoster(item *dynamicMedia, lookup dynamicPosterLookup) {
	if item == nil || item.Poster != "" || item.TMDBID <= 0 || lookup == nil {
		return
	}
	if poster, err := lookup(item.TMDBID, item.Type); err == nil {
		item.Poster = poster
	}
}

func (s *Server) fillDynamicPosters(items []dynamicMedia) {
	if s.deps.TMDB == nil || len(items) == 0 {
		return
	}
	lookup := func(tmdbID int, mediaType string) (string, error) {
		detail, err := s.deps.TMDB.GetMediaByType(tmdbID, mediaType)
		if err != nil || detail == nil {
			return "", err
		}
		return s.deps.TMDB.GetPosterURL(detail.PosterPath), nil
	}
	// Dynamic feeds are cached for two minutes. Limit concurrent TMDB reads so
	// one refresh stays fast without turning a missing-poster batch into a burst.
	type posterResult struct {
		index  int
		poster string
	}
	semaphore := make(chan struct{}, 3)
	results := make(chan posterResult, len(items))
	pending := 0
	for i := range items {
		if items[i].Poster != "" || items[i].TMDBID <= 0 {
			continue
		}
		pending++
		go func(index int, item dynamicMedia) {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			fillDynamicPoster(&item, lookup)
			results <- posterResult{index: index, poster: item.Poster}
		}(i, items[i])
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for pending > 0 {
		select {
		case result := <-results:
			items[result.index].Poster = result.poster
			pending--
		case <-deadline.C:
			return
		}
	}
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
		cached := s.dynamicCache
		writeJSON(w, http.StatusOK, cached)
		return
	}
	response := map[string]any{"recently_added": []dynamicMedia{}, "just_available": []dynamicMedia{}, "recent_requests": []dynamicMedia{}}
	if s.deps.MoviePilot != nil {
		if items, err := s.deps.MoviePilot.EmbyRecentlyAdded(8); err == nil {
			public := make([]dynamicMedia, 0, len(items))
			for _, item := range items {
				public = append(public, dynamicMedia{TMDBID: item.TMDBID, Type: item.Type, Title: item.Name, Year: item.Year, Rating: item.Rating})
			}
			s.fillDynamicPosters(public)
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
			if review == nil || review.NormalizedBusinessType() != services.BusinessTypeRequest || review.TmdbID <= 0 || !(review.EmbyExists || review.LibraryNotifiedAt != nil) {
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
		s.fillDynamicPosters(recent)
		s.fillDynamicPosters(done)
		response["recent_requests"], response["just_available"] = recent, done
	}
	s.dynamicAt, s.dynamicCache = time.Now(), response
	writeJSON(w, http.StatusOK, response)
}

func completionTime(review *services.ReviewRequest) time.Time {
	if review == nil {
		return time.Time{}
	}
	if review.LibraryNotifiedAt != nil {
		return *review.LibraryNotifiedAt
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
		if review == nil || (review.NormalizedBusinessType() != services.BusinessTypeRequest && review.NormalizedBusinessType() != services.BusinessTypeWash) {
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
		if review.NormalizedBusinessType() == services.BusinessTypeWash {
			if review.Status == "completed" {
				status, text, group = "completed", "洗版完成", "done"
			} else if review.Status == "approved" {
				status, text, group = "approved", "洗版处理中", "active"
			}
		}
		requests = append(requests, userRequestView{RequestID: review.RequestID, TmdbID: review.TmdbID, Title: review.MediaTitle, Year: review.MediaYear, Type: review.MediaType, Season: review.Season, Poster: review.PosterPath, Status: status, StatusText: text, Group: group, CreatedAt: review.CreatedAt, CanCancel: review.Status == "pending", Note: note, BusinessType: review.NormalizedBusinessType()})
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
	if review.EmbyExists || review.LibraryNotifiedAt != nil {
		return "completed", "已入库", "done"
	}
	if review.Status == "approved" {
		if review.SubscriptionState == services.StateCompleted {
			return "awaiting_library", "资源已齐，等待入库", "active"
		}
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
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = defaultSearchLimit
	} else if limit > maxSearchLimit {
		limit = maxSearchLimit
	}
	response := searchResponseView{Results: []services.SearchResult{}, Page: page, Limit: limit}
	if query == "" {
		writeJSON(w, http.StatusOK, response)
		return
	}
	if s.deps.MoviePilot == nil {
		http.Error(w, "搜索暂时不可用", http.StatusServiceUnavailable)
		return
	}
	result, err := s.deps.MoviePilot.SearchMediaWithCountContext(r.Context(), query, page, limit)
	if err != nil {
		http.Error(w, "搜索暂时不可用", http.StatusBadGateway)
		return
	}
	upstreamCount := len(result.Results)
	typeName := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type")))
	if typeName == "movie" || typeName == "tv" {
		result.Results = filterSearchResults(result.Results, typeName)
	} else {
		typeName = ""
	}
	response.Results = result.Results

	// A client page remains one upstream page. For typed searches, look ahead a
	// bounded number of pages and point the cursor at the next page that actually
	// contains the requested type, so "load more" never leads to a known-empty
	// page. Unfiltered searches only need the immediate next-page probe.
	if upstreamCount == limit {
		lookahead := 1
		if typeName != "" {
			lookahead = maxSearchLookahead
		}
		for nextPage := page + 1; nextPage <= page+lookahead; nextPage++ {
			next, err := s.deps.MoviePilot.SearchMediaWithCountContext(r.Context(), query, nextPage, limit)
			if err != nil {
				if r.Context().Err() != nil {
					return
				}
				// The current page is still valid. A failed lookahead must not turn
				// an otherwise successful search into an error response.
				break
			}
			nextCount := len(next.Results)
			if typeName != "" {
				next.Results = filterSearchResults(next.Results, typeName)
			}
			if len(next.Results) > 0 {
				response.HasMore = true
				response.NextPage = nextPage
				break
			}
			if nextCount < limit {
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func filterSearchResults(results []services.SearchResult, typeName string) []services.SearchResult {
	filtered := make([]services.SearchResult, 0, len(results))
	for _, item := range results {
		if typeName == "movie" && (item.Type == "电影" || item.Type == "movie") {
			filtered = append(filtered, item)
		}
		if typeName == "tv" && (item.Type == "电视剧" || item.Type == "tv") {
			filtered = append(filtered, item)
		}
	}
	return filtered
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
	season, _ := strconv.Atoi(r.URL.Query().Get("season"))
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
	wishJoined := false
	if s.deps.Carpool != nil {
		view.InWatchlist = s.deps.Carpool.Contains(id, kind, user.ID)
	}
	if s.deps.Wishes != nil {
		seasonScope := 0
		if kind == "tv" && season > 0 {
			seasonScope = season
		}
		if joined, err := s.deps.Wishes.ListForWisher(user.ID); err == nil {
			for _, item := range joined {
				if item.TmdbID == id && item.MediaType == kind && item.Season == seasonScope {
					wishJoined = true
					view.Status = detailStatus{Code: "wish_joined", Text: "已经在许愿池"}
					break
				}
			}
		}
	}
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
			status := seasonBaseStatus(season.AirDate, time.Now())
			view.Seasons = append(view.Seasons, detailSeason{Number: season.SeasonNumber, Name: season.Name, EpisodeCount: season.EpisodeCount, AirDate: season.AirDate, Poster: s.deps.TMDB.GetPosterURL(season.PosterPath), Status: status})
		}
		available := map[int]bool(nil)
		seasonErr := fmt.Errorf("Emby season lookup is unavailable")
		if s.deps.MoviePilot != nil {
			available, seasonErr = s.deps.MoviePilot.EmbyAvailableSeasonsByTMDB(id)
		}
		var activeRequest func(int) (*services.ReviewRequest, bool)
		if s.deps.Reviews != nil {
			activeRequest = func(season int) (*services.ReviewRequest, bool) {
				return s.deps.Reviews.HasActiveSimilarRequest(user.ID, id, mediaType, season)
			}
		}
		applySeasonAvailability(view.Seasons, available, seasonErr, activeRequest)
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
	if mediaType == services.MediaTypeMovie {
		exists, err := s.deps.MoviePilot.EmbyMediaAvailabilityByTMDB(id, mediaType)
		if err != nil {
			view.Status = detailStatus{Code: "unknown", Text: "媒体库状态暂时无法确认"}
		} else if exists {
			if !wishJoined {
				view.Status = detailStatus{Code: "in_library", Text: "库中可看"}
			}
			if s.deps.Quota != nil {
				view.Quota = publicQuota(s.deps.Quota.GetQuotaInfo(user.ID))
			}
			writeJSON(w, http.StatusOK, view)
			return
		}
	} else if season > 0 {
		for _, item := range view.Seasons {
			if item.Number == season {
				if !wishJoined {
					view.Status = item.Status
				}
				break
			}
		}
	}
	if view.Status.Code == "available" {
		if existing, found, ready := s.deps.MoviePilot.FindCachedSubscription(id, mediaType); !ready {
			view.Status = detailStatus{Code: "unknown", Text: "订阅状态暂时无法确认"}
		} else if found {
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
	}
	if s.deps.Quota != nil {
		view.Quota = publicQuota(s.deps.Quota.GetQuotaInfo(user.ID))
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleWatchlist(w http.ResponseWriter, r *http.Request) {
	user, ok := s.auth(w, r)
	if !ok {
		return
	}
	if s.deps.Carpool == nil {
		http.Error(w, "想看服务未就绪", http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodGet {
		items := s.deps.Carpool.ListForUser(user.ID)
		views := make([]watchlistItemView, len(items))
		semaphore := make(chan struct{}, 3)
		var wg sync.WaitGroup
		for i, item := range items {
			views[i] = watchlistItemView{TMDBID: item.TMDBID, Type: item.Type, Title: item.Title, Year: item.Year, Poster: item.Poster}
			if item.Title != "" || s.deps.TMDB == nil {
				continue
			}
			wg.Add(1)
			go func(index int, item services.CarpoolItem) {
				defer wg.Done()
				semaphore <- struct{}{}
				defer func() { <-semaphore }()
				detail, err := s.deps.TMDB.GetMediaByType(item.TMDBID, item.Type)
				if err != nil || detail == nil {
					return
				}
				view := &views[index]
				view.Title, view.Poster = detail.Title, s.deps.TMDB.GetPosterURL(detail.PosterPath)
				date := detail.ReleaseDate
				if item.Type == "tv" {
					view.Title, date = detail.Name, detail.FirstAirDate
				}
				if len(date) >= 4 {
					view.Year = date[:4]
				}
			}(i, item)
		}
		wg.Wait()
		writeJSON(w, http.StatusOK, map[string]any{"items": views})
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID   int    `json:"tmdb_id"`
		Type string `json:"type"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.ID <= 0 || (body.Type != "movie" && body.Type != "tv") {
		http.Error(w, "请求参数不完整", http.StatusBadRequest)
		return
	}
	if r.Method == http.MethodPost {
		if s.deps.TMDB == nil {
			http.Error(w, "影视信息服务未就绪", http.StatusServiceUnavailable)
			return
		}
		if len(s.deps.Carpool.ListForUser(user.ID)) >= 200 && !s.deps.Carpool.Contains(body.ID, body.Type, user.ID) {
			http.Error(w, "想看列表已达上限", http.StatusConflict)
			return
		}
		meta := services.CarpoolMetadata{AddedAt: time.Now().UTC()}
		detail, detailErr := s.deps.TMDB.GetMediaByType(body.ID, body.Type)
		if detailErr != nil || detail == nil {
			http.Error(w, "影视信息校验失败", http.StatusBadGateway)
			return
		}
		meta.Title, meta.Poster = detail.Title, s.deps.TMDB.GetPosterURL(detail.PosterPath)
		date := detail.ReleaseDate
		if body.Type == "tv" {
			meta.Title, date = detail.Name, detail.FirstAirDate
		}
		if strings.TrimSpace(meta.Title) == "" {
			http.Error(w, "影视信息校验失败", http.StatusBadGateway)
			return
		}
		if len(date) >= 4 {
			meta.Year = date[:4]
		}
		count, err := s.deps.Carpool.AddWithMetadataChecked(body.ID, body.Type, user.ID, meta)
		if err != nil {
			http.Error(w, "想看保存失败，请稍后再试", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "in_watchlist": true, "watcher_count": count})
		return
	}
	removed, err := s.deps.Carpool.RemoveChecked(body.ID, body.Type, user.ID)
	if err != nil {
		http.Error(w, "取消想看保存失败，请稍后再试", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "in_watchlist": false, "removed": removed})
}

func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	user, ok := s.auth(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.deps.Reviews == nil {
		http.Error(w, "进度服务未就绪", http.StatusServiceUnavailable)
		return
	}
	requestID := strings.TrimSpace(r.URL.Query().Get("request_id"))
	review, found := s.deps.Reviews.GetRequest(requestID)
	if requestID == "" || !found || review == nil || review.TelegramID != user.ID || (review.NormalizedBusinessType() != services.BusinessTypeRequest && review.NormalizedBusinessType() != services.BusinessTypeWash) {
		http.Error(w, "进度记录不存在", http.StatusNotFound)
		return
	}
	createdAt := review.CreatedAt
	createdText := "已提交求片"
	if review.NormalizedBusinessType() == services.BusinessTypeWash {
		createdText = "已提交洗版工单"
	}
	events := []progressEventView{{Code: "created", Text: createdText, At: &createdAt}}
	if !review.ReviewedAt.IsZero() {
		text := "审核已通过"
		code := "approved"
		if review.Status == "rejected" {
			code, text = "rejected", "审核未通过"
		} else if review.Status == "cancelled" {
			code, text = "cancelled", "已主动撤回"
		}
		reviewedAt := review.ReviewedAt
		events = append(events, progressEventView{Code: code, Text: text, At: &reviewedAt})
	}
	if review.DownloadNotified {
		// 旧数据只持久化“已通知”事实，不持久化独立时间，因此不伪造时间。
		events = append(events, progressEventView{Code: "downloading", Text: "已开始下载（时间未单独记录）"})
	}
	if review.CompletedNoticeAt != nil {
		events = append(events, progressEventView{Code: "download_complete", Text: "资源已齐，等待 Emby 入库确认", At: review.CompletedNoticeAt})
	}
	if review.LibraryNotifiedAt != nil {
		events = append(events, progressEventView{Code: "completed", Text: "已入库并通知", At: review.LibraryNotifiedAt})
	}
	if review.NormalizedBusinessType() == services.BusinessTypeWash && review.Status == "completed" {
		events = append(events, progressEventView{Code: "completed", Text: "新版本已验证，洗版完成"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"request_id": review.RequestID, "events": events})
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
		if body.Season <= 0 {
			http.Error(w, "请选择有效季号", http.StatusBadRequest)
			return
		}
	} else if body.Type != "movie" {
		http.Error(w, "媒体类型无效", http.StatusBadRequest)
		return
	} else if body.Season != 0 {
		http.Error(w, "电影请求不能包含季号", http.StatusBadRequest)
		return
	}
	if s.deps.Submission == nil || s.deps.TMDB == nil || s.deps.MoviePilot == nil {
		http.Error(w, "求片服务未就绪", http.StatusServiceUnavailable)
		return
	}
	if mediaType == services.MediaTypeMovie {
		exists, err := s.deps.MoviePilot.EmbyMediaAvailabilityByTMDB(body.ID, mediaType)
		if err != nil {
			http.Error(w, "暂时无法确认媒体库状态，请稍后再试", http.StatusServiceUnavailable)
			return
		}
		if exists {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "status": "in_library", "message": "库中已经有这部电影"})
			return
		}
	} else {
		exists, err := s.deps.MoviePilot.EmbyMediaAvailabilityByTMDBSeason(body.ID, body.Season)
		if err != nil {
			http.Error(w, "暂时无法确认这一季的媒体库状态，请稍后再试", http.StatusServiceUnavailable)
			return
		}
		if exists {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "status": "in_library", "message": "这一季已经入库"})
			return
		}
	}
	mediaTitle, mediaYear, posterPath, overview := "", 0, "", ""
	if body.Type == "tv" {
		detail, err := s.deps.TMDB.GetTVDetailsWithSeasons(body.ID)
		if err != nil || detail == nil || detail.Name == "" {
			http.Error(w, "影视信息校验失败", http.StatusBadGateway)
			return
		}
		seasonStatus := detailStatus{Code: "unknown", Text: "季信息暂时无法确认"}
		for _, season := range detail.Seasons {
			if season.SeasonNumber == body.Season {
				seasonStatus = seasonBaseStatus(season.AirDate, time.Now())
				break
			}
		}
		if seasonStatus.Code != "available" {
			status := http.StatusConflict
			if seasonStatus.Code == "unknown" {
				status = http.StatusServiceUnavailable
			}
			writeJSON(w, status, map[string]any{"ok": false, "status": seasonStatus.Code, "message": seasonStatus.Text})
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

func (s *Server) handleWash(w http.ResponseWriter, r *http.Request) {
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
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.ID <= 0 || (body.Type != "movie" && body.Type != "tv") || (body.Type == "movie" && body.Season != 0) || (body.Type == "tv" && body.Season <= 0) {
		http.Error(w, "洗版参数不完整", http.StatusBadRequest)
		return
	}
	if s.deps.Submission == nil || s.deps.TMDB == nil || s.deps.Webhook == nil {
		http.Error(w, "洗版服务未就绪", http.StatusServiceUnavailable)
		return
	}
	mediaType := services.MediaTypeMovie
	if body.Type == "tv" {
		mediaType = services.MediaTypeTV
	}
	media, err := s.deps.TMDB.GetMediaByType(body.ID, body.Type)
	if err != nil || media == nil {
		http.Error(w, "影视信息校验失败", http.StatusBadGateway)
		return
	}
	title, date := media.Title, media.ReleaseDate
	if body.Type == "tv" {
		title, date = media.Name, media.FirstAirDate
	}
	year := 0
	if len(date) >= 4 {
		year, _ = strconv.Atoi(date[:4])
	}
	exists, err := s.deps.Webhook.HasEmbyWashTarget(body.ID, title, year, mediaType, body.Season)
	if err != nil {
		http.Error(w, "暂时无法核验媒体库，请稍后再试", http.StatusServiceUnavailable)
		return
	}
	if !exists {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "status": "not_in_library", "message": "媒体库中没有当前版本，不能申请洗版"})
		return
	}
	baseline, err := s.deps.Webhook.CaptureEmbyWashBaseline(body.ID, mediaType, body.Season)
	if err != nil || len(baseline) == 0 {
		http.Error(w, "无法记录当前版本，已停止提交", http.StatusServiceUnavailable)
		return
	}
	result, err := s.deps.Submission.SubmitResult(services.RequestSubmission{
		BusinessType: services.BusinessTypeWash, TelegramID: user.ID, TelegramName: user.FirstName,
		TmdbID: body.ID, MediaTitle: title, MediaYear: year, MediaType: mediaType, Season: body.Season,
		PosterPath: s.deps.TMDB.GetPosterURL(media.PosterPath), Overview: media.Overview,
		Origin: "miniapp_wash", UseQuota: false, WashBaseline: baseline,
	})
	if err != nil {
		http.Error(w, "洗版工单提交失败，请稍后再试", http.StatusBadGateway)
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
	if !exists || review == nil || review.TelegramID != user.ID || (review.NormalizedBusinessType() != services.BusinessTypeRequest && review.NormalizedBusinessType() != services.BusinessTypeWash) {
		http.Error(w, "进度记录不存在", http.StatusNotFound)
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
	if review.NormalizedBusinessType() == services.BusinessTypeWash {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "cancelled", "quota_restored": false})
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
