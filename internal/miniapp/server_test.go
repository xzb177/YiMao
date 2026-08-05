package miniapp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/services"
)

const miniAppTestToken = "123456:TEST_TOKEN_FOR_UNIT_TEST_ONLY"

type requestSubmitterFunc func(services.RequestSubmission) (services.SubmissionResult, error)

func (f requestSubmitterFunc) SubmitResult(input services.RequestSubmission) (services.SubmissionResult, error) {
	return f(input)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type tmdbRequestLog struct {
	mu    sync.Mutex
	paths []string
}

func (l *tmdbRequestLog) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	l.paths = append(l.paths, r.URL.Path)
	l.mu.Unlock()

	switch r.URL.Path {
	case "/3/movie/271413":
		_ = json.NewEncoder(w).Encode(map[string]string{"title": "test movie"})
	case "/3/tv/271413":
		_ = json.NewEncoder(w).Encode(map[string]string{"name": "test tv"})
	default:
		http.NotFound(w, r)
	}
}

func (l *tmdbRequestLog) Paths() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.paths...)
}

func newCountingTMDBClient(t *testing.T) (*services.TMDBClient, *tmdbRequestLog) {
	t.Helper()
	requests := &tmdbRequestLog{}
	upstream := httptest.NewServer(requests)
	t.Cleanup(upstream.Close)

	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	originalTransport := http.DefaultTransport
	http.DefaultTransport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "api.themoviedb.org" {
			return originalTransport.RoundTrip(request)
		}
		clone := request.Clone(request.Context())
		clone.URL.Scheme = upstreamURL.Scheme
		clone.URL.Host = upstreamURL.Host
		return originalTransport.RoundTrip(clone)
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	return services.NewTMDBClient("test"), requests
}

func signedRequest(t *testing.T, method, target, body string, userID int64) *http.Request {
	t.Helper()
	values := signedInitDataValues(userID)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("X-Telegram-Init-Data", values.Encode())
	req.Header.Set("Content-Type", "application/json")
	return req
}

func signedInitDataValues(userID int64) url.Values {
	values := url.Values{
		"auth_date": {strconv.FormatInt(time.Now().Unix(), 10)},
		"user":      {`{"id":` + strconv.FormatInt(userID, 10) + `,"first_name":"测试"}`},
	}
	values.Set("hash", signInitDataForTest(miniAppTestToken, values))
	return values
}

func TestAuthRejectsSignedInitDataInQueryString(t *testing.T) {
	handler := NewServer(Deps{BotToken: miniAppTestToken}).Handler()
	query := signedInitDataValues(101).Encode()
	request := httptest.NewRequest(http.MethodGet, "/api/miniapp/v1/watchlist?initData="+url.QueryEscape(query), nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("query-string initData must be rejected: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestIndexSetsBrowserSecurityHeaders(t *testing.T) {
	handler := NewServer(Deps{}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/miniapp/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	wants := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
		"Permissions-Policy":     "camera=(), microphone=(), geolocation=()",
	}
	for name, want := range wants {
		if got := response.Header().Get(name); got != want {
			t.Errorf("%s=%q want %q", name, got, want)
		}
	}
}

func TestSearchResultsExposeCanonicalMediaType(t *testing.T) {
	results := []services.SearchResult{
		{ID: 271413, Title: "完美世界剧场版", Type: "电视剧"},
		{ID: 1534911, Title: "完美世界：火之灰烬", Type: "电影"},
	}

	normalizeSearchResultTypes(results)

	if results[0].Type != "tv" {
		t.Fatalf("TV result must keep the TMDB TV namespace, got type=%q", results[0].Type)
	}
	if results[1].Type != "movie" {
		t.Fatalf("movie result must keep the TMDB movie namespace, got type=%q", results[1].Type)
	}
}

func TestSearchWithoutTypeCanonicalizesAndRejectsUnknownUpstreamTypes(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]services.SearchResult{
			{ID: 271413, Title: "完美世界剧场版", Type: " 电视剧 "},
			{ID: 999999, Title: "未知类型", Type: "UNKNOWN"},
		})
	}))
	defer upstream.Close()

	mp := services.NewMoviePilotClient(upstream.URL, "test", "")
	handler := NewServer(Deps{BotToken: miniAppTestToken, MoviePilot: mp}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, signedRequest(t, http.MethodGet, "/api/miniapp/v1/search?q=test", "", 101))

	var payload searchResponseView
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode search response: %v body=%s", err, response.Body.String())
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(payload.Results) != 1 || payload.Results[0].ID != 271413 || payload.Results[0].Type != "tv" {
		t.Fatalf("unexpected canonical search results: %+v", payload.Results)
	}
}

func TestDetailRejectsNonGETMethods(t *testing.T) {
	handler := NewServer(Deps{BotToken: miniAppTestToken}).Handler()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, signedRequest(t, method, "/api/miniapp/v1/detail?id=1&type=movie", `{}`, 101))
			if response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestDetailRejectsMissingOrUnknownMediaType(t *testing.T) {
	tmdb, requests := newCountingTMDBClient(t)
	handler := NewServer(Deps{BotToken: miniAppTestToken, TMDB: tmdb}).Handler()
	for _, target := range []string{
		"/api/miniapp/v1/detail?id=271413",
		"/api/miniapp/v1/detail?id=271413&type=unknown",
		"/api/miniapp/v1/detail?id=271413&type=%20TV%20",
	} {
		t.Run(target, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, signedRequest(t, http.MethodGet, target, "", 101))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("detail must fail closed instead of defaulting to movie: status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	if paths := requests.Paths(); len(paths) != 0 {
		t.Fatalf("invalid media types reached TMDB: paths=%v", paths)
	}
}

func TestDetailAcceptsCanonicalMediaTypes(t *testing.T) {
	for _, mediaType := range []string{"movie", "tv"} {
		t.Run(mediaType, func(t *testing.T) {
			tmdb, requests := newCountingTMDBClient(t)
			moviePilot := services.NewMoviePilotClient("", "", "")
			handler := NewServer(Deps{BotToken: miniAppTestToken, MoviePilot: moviePilot, TMDB: tmdb}).Handler()
			response := httptest.NewRecorder()
			target := "/api/miniapp/v1/detail?id=271413&type=" + mediaType
			handler.ServeHTTP(response, signedRequest(t, http.MethodGet, target, "", 101))
			if response.Code != http.StatusOK {
				t.Fatalf("canonical detail media type failed: type=%s status=%d body=%s", mediaType, response.Code, response.Body.String())
			}
			wantPath := "/3/" + mediaType + "/271413"
			if paths := requests.Paths(); len(paths) != 1 || paths[0] != wantPath {
				t.Fatalf("TMDB paths=%v want [%s]", paths, wantPath)
			}
		})
	}
}

func TestWriteEndpointsRejectTrailingJSON(t *testing.T) {
	carpool := services.NewCarpoolService(t.TempDir())
	handler := NewServer(Deps{BotToken: miniAppTestToken, Carpool: carpool}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, signedRequest(t, http.MethodDelete, "/api/miniapp/v1/watchlist", `{"tmdb_id":550,"type":"movie"}{"tmdb_id":551,"type":"movie"}`, 101))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestWriteEndpointsRejectOversizedBodies(t *testing.T) {
	carpool := services.NewCarpoolService(t.TempDir())
	handler := NewServer(Deps{BotToken: miniAppTestToken, Carpool: carpool}).Handler()
	body := `{"tmdb_id":550,"type":"movie","padding":"` + strings.Repeat("x", 9<<10) + `"}`
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, signedRequest(t, http.MethodDelete, "/api/miniapp/v1/watchlist", body, 101))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestWatchlistListAndExactRemove(t *testing.T) {
	carpool := services.NewCarpoolService(t.TempDir())
	handler := NewServer(Deps{BotToken: miniAppTestToken, Carpool: carpool}).Handler()

	carpool.Add(550, "movie", 101)
	carpool.Add(550, "movie", 202)

	list := httptest.NewRecorder()
	handler.ServeHTTP(list, signedRequest(t, http.MethodGet, "/api/miniapp/v1/watchlist", "", 101))
	var payload struct {
		Items []services.CarpoolItem `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &payload); err != nil || len(payload.Items) != 1 || payload.Items[0].TMDBID != 550 {
		t.Fatalf("unexpected list: status=%d body=%s err=%v", list.Code, list.Body.String(), err)
	}

	remove := httptest.NewRecorder()
	handler.ServeHTTP(remove, signedRequest(t, http.MethodDelete, "/api/miniapp/v1/watchlist", `{"tmdb_id":550,"type":"movie"}`, 101))
	if remove.Code != http.StatusOK || carpool.Contains(550, "movie", 101) || !carpool.Contains(550, "movie", 202) {
		t.Fatalf("exact remove failed: status=%d body=%s", remove.Code, remove.Body.String())
	}
}

func TestWatchlistAddFailsClosedWithoutTMDB(t *testing.T) {
	carpool := services.NewCarpoolService(t.TempDir())
	handler := NewServer(Deps{BotToken: miniAppTestToken, Carpool: carpool}).Handler()

	add := httptest.NewRecorder()
	handler.ServeHTTP(add, signedRequest(t, http.MethodPost, "/api/miniapp/v1/watchlist", `{"tmdb_id":550,"type":"movie"}`, 101))
	if add.Code != http.StatusServiceUnavailable || carpool.Contains(550, "movie", 101) {
		t.Fatalf("unverified media was persisted: status=%d body=%s", add.Code, add.Body.String())
	}
}

func TestProgressOnlyReturnsOwnersRealEvents(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	completed := services.NewReviewService(t.TempDir(), false)
	if err := completed.CreateRequest(&services.ReviewRequest{RequestID: "done", TelegramID: 101, TmdbID: 550, MediaTitle: "测试电影", MediaType: services.MediaTypeMovie, LibraryNotifiedAt: &now}); err != nil {
		t.Fatal(err)
	}
	handler := NewServer(Deps{BotToken: miniAppTestToken, Reviews: completed}).Handler()

	owner := httptest.NewRecorder()
	handler.ServeHTTP(owner, signedRequest(t, http.MethodGet, "/api/miniapp/v1/progress?request_id=done", "", 101))
	if owner.Code != http.StatusOK || !strings.Contains(owner.Body.String(), `"code":"created"`) || !strings.Contains(owner.Body.String(), `"code":"completed"`) {
		t.Fatalf("owner progress failed: status=%d body=%s", owner.Code, owner.Body.String())
	}

	other := httptest.NewRecorder()
	handler.ServeHTTP(other, signedRequest(t, http.MethodGet, "/api/miniapp/v1/progress?request_id=done", "", 202))
	if other.Code != http.StatusNotFound {
		t.Fatalf("cross-user progress leaked: status=%d body=%s", other.Code, other.Body.String())
	}
}

func TestProgressMPCompleteIsNotLibraryComplete(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	reviews := services.NewReviewService(t.TempDir(), false)
	if err := reviews.CreateRequest(&services.ReviewRequest{RequestID: "mp-done", TelegramID: 101, TmdbID: 550, MediaTitle: "测试电影", MediaType: services.MediaTypeMovie, CompletedNoticeAt: &now}); err != nil {
		t.Fatal(err)
	}
	handler := NewServer(Deps{BotToken: miniAppTestToken, Reviews: reviews}).Handler()
	owner := httptest.NewRecorder()
	handler.ServeHTTP(owner, signedRequest(t, http.MethodGet, "/api/miniapp/v1/progress?request_id=mp-done", "", 101))
	if owner.Code != http.StatusOK || !strings.Contains(owner.Body.String(), `"code":"download_complete"`) || strings.Contains(owner.Body.String(), `"code":"completed"`) {
		t.Fatalf("MP completion mislabeled: status=%d body=%s", owner.Code, owner.Body.String())
	}
}

func TestRequestHandlerSubmissionStatusContract(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Users/test-user/Items":
			_ = json.NewEncoder(w).Encode(map[string]any{"TotalRecordCount": 0, "Items": []any{}})
		case "/3/movie/42":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "title": "测试电影", "release_date": "2026-01-01"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	originalTransport := http.DefaultTransport
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	http.DefaultTransport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "api.themoviedb.org" {
			return originalTransport.RoundTrip(request)
		}
		clone := request.Clone(request.Context())
		clone.URL.Scheme = upstreamURL.Scheme
		clone.URL.Host = upstreamURL.Host
		return originalTransport.RoundTrip(clone)
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	moviePilot := services.NewMoviePilotClient(upstream.URL, "test", "")
	moviePilot.SetEmbyConfig(upstream.URL, "test")
	moviePilot.SetEmbyUserID("test-user")
	tmdb := services.NewTMDBClient("test")

	internalErr := errors.New("sensitive storage details")
	tests := []struct {
		name        string
		result      services.SubmissionResult
		err         error
		wantStatus  int
		wantOK      bool
		wantResult  services.SubmissionStatus
		wantMessage string
		wantID      string
	}{
		{name: "quota exceeded", result: services.SubmissionResult{Status: services.SubmissionQuotaExceeded}, err: errors.Join(services.ErrQuotaExceeded, internalErr), wantStatus: http.StatusTooManyRequests, wantResult: services.SubmissionQuotaExceeded, wantMessage: "今日求片次数已用完"},
		{name: "not bound", result: services.SubmissionResult{Status: services.SubmissionNotBound}, wantStatus: http.StatusForbidden, wantResult: services.SubmissionNotBound, wantMessage: "需要绑定账号"},
		{name: "explicit failed", result: services.SubmissionResult{Status: services.SubmissionFailed}, err: internalErr, wantStatus: http.StatusBadGateway, wantResult: services.SubmissionFailed, wantMessage: "提交失败，请稍后再试"},
		{name: "unexpected error", err: internalErr, wantStatus: http.StatusBadGateway, wantResult: services.SubmissionFailed, wantMessage: "提交失败，请稍后再试"},
		{name: "unknown status", result: services.SubmissionResult{Status: "unexpected"}, wantStatus: http.StatusBadGateway, wantResult: services.SubmissionFailed, wantMessage: "提交失败，请稍后再试"},
		{name: "created", result: services.SubmissionResult{Status: services.SubmissionCreated, Review: &services.ReviewRequest{RequestID: "new-request"}}, wantStatus: http.StatusCreated, wantOK: true, wantResult: services.SubmissionCreated, wantID: "new-request"},
		{name: "duplicate own", result: services.SubmissionResult{Status: services.SubmissionDuplicateOwn, Review: &services.ReviewRequest{RequestID: "existing-own"}}, wantStatus: http.StatusOK, wantOK: true, wantResult: services.SubmissionDuplicateOwn, wantID: "existing-own"},
		{name: "duplicate other", result: services.SubmissionResult{Status: services.SubmissionDuplicateOther, Review: &services.ReviewRequest{RequestID: "existing-other"}}, wantStatus: http.StatusOK, wantOK: true, wantResult: services.SubmissionDuplicateOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			submission := requestSubmitterFunc(func(input services.RequestSubmission) (services.SubmissionResult, error) {
				called = true
				if input.TmdbID != 42 || input.MediaType != services.MediaTypeMovie || !input.UseQuota {
					t.Fatalf("unexpected submission input: %+v", input)
				}
				return tt.result, tt.err
			})
			handler := NewServer(Deps{BotToken: miniAppTestToken, MoviePilot: moviePilot, TMDB: tmdb, Submission: submission}).Handler()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, signedRequest(t, http.MethodPost, "/api/miniapp/v1/request", `{"tmdb_id":42,"type":"movie","season":0}`, 101))

			var payload struct {
				OK        bool                      `json:"ok"`
				Status    services.SubmissionStatus `json:"status"`
				RequestID string                    `json:"request_id"`
				Message   string                    `json:"message"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode submission response: %v body=%s", err, response.Body.String())
			}
			if !called {
				t.Fatal("submission service was not called")
			}
			if response.Code != tt.wantStatus || payload.OK != tt.wantOK || payload.Status != tt.wantResult || payload.Message != tt.wantMessage {
				t.Fatalf("http=%d payload=%+v want_http=%d want_ok=%v want_status=%s want_message=%q", response.Code, payload, tt.wantStatus, tt.wantOK, tt.wantResult, tt.wantMessage)
			}
			if strings.Contains(response.Body.String(), internalErr.Error()) {
				t.Fatalf("response exposed internal error: %s", response.Body.String())
			}
			if payload.RequestID != tt.wantID {
				t.Fatalf("request_id=%q want %q", payload.RequestID, tt.wantID)
			}
		})
	}
}

func TestApplySeasonAvailabilityUsesEmbyThenOwnReview(t *testing.T) {
	seasons := []detailSeason{
		{Number: 1, Status: detailStatus{Code: "available", Text: "可以求片"}},
		{Number: 2, Status: detailStatus{Code: "available", Text: "可以求片"}},
		{Number: 3, Status: detailStatus{Code: "upcoming", Text: "尚未播出"}},
	}
	applySeasonAvailability(seasons, map[int]bool{1: true}, nil, func(season int) (*services.ReviewRequest, bool) {
		if season == 2 {
			return &services.ReviewRequest{Status: "pending"}, true
		}
		return nil, false
	})
	if seasons[0].Status.Code != "in_library" || seasons[1].Status.Code != "requested" || seasons[1].Status.Text != "待审核" || seasons[2].Status.Code != "upcoming" {
		t.Fatalf("unexpected season statuses: %+v", seasons)
	}
}

func TestApplySeasonAvailabilityPreservesDetailedReviewState(t *testing.T) {
	seasons := []detailSeason{{Number: 2, Status: detailStatus{Code: "available", Text: "可以求片"}}}
	applySeasonAvailability(seasons, map[int]bool{}, nil, func(season int) (*services.ReviewRequest, bool) {
		return &services.ReviewRequest{Status: "approved", SubscriptionState: services.StateRecycled}, true
	})
	if seasons[0].Status.Code != "requested" || seasons[0].Status.Text != "重新搜索" {
		t.Fatalf("season status=%+v", seasons[0].Status)
	}
}

func TestApplySeasonAvailabilityFailsClosedButKeepsRealReview(t *testing.T) {
	seasons := []detailSeason{
		{Number: 1, Status: detailStatus{Code: "available", Text: "可以求片"}},
		{Number: 2, Status: detailStatus{Code: "available", Text: "可以求片"}},
		{Number: 3, Status: detailStatus{Code: "upcoming", Text: "尚未播出"}},
	}
	applySeasonAvailability(seasons, nil, http.ErrServerClosed, func(season int) (*services.ReviewRequest, bool) {
		if season == 2 {
			return &services.ReviewRequest{Status: "approved", SubscriptionState: services.StateSearching}, true
		}
		return nil, false
	})
	if seasons[0].Status.Code != "unknown" || seasons[1].Status.Code != "requested" || seasons[1].Status.Text != "搜索中" || seasons[2].Status.Code != "upcoming" {
		t.Fatalf("unexpected degraded statuses: %+v", seasons)
	}
}

func TestCompletionTimePrefersConfirmedLibraryTime(t *testing.T) {
	downloaded := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	library := downloaded.Add(20 * time.Minute)
	got := completionTime(&services.ReviewRequest{CompletedNoticeAt: &downloaded, LibraryNotifiedAt: &library})
	if !got.Equal(library) {
		t.Fatalf("completion time=%s want library time=%s", got, library)
	}
}

func TestSeasonBaseStatusUsesFullAirDate(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	if got := seasonBaseStatus("2026-07-31", now); got.Code != "upcoming" {
		t.Fatalf("future season status=%+v", got)
	}
	if got := seasonBaseStatus("2026-07-30", now); got.Code != "available" {
		t.Fatalf("premiered season status=%+v", got)
	}
	for _, airDate := range []string{"", "not-a-date"} {
		if got := seasonBaseStatus(airDate, now); got.Code != "unknown" {
			t.Fatalf("unknown air date %q status=%+v", airDate, got)
		}
	}
}

func TestUserRequestStatusMPCompleteWaitsForEmby(t *testing.T) {
	now := time.Now()
	status, text, group := userRequestStatus(&services.ReviewRequest{Status: "approved", SubscriptionState: services.StateCompleted, CompletedNoticeAt: &now})
	if status != "awaiting_library" || text != "资源已齐，等待入库" || group != "active" {
		t.Fatalf("status=%q text=%q group=%q", status, text, group)
	}
	status, text, group = userRequestStatus(&services.ReviewRequest{Status: "approved", SubscriptionState: services.StateCompleted, LibraryNotifiedAt: &now})
	if status != "completed" || text != "已入库" || group != "done" {
		t.Fatalf("confirmed status=%q text=%q group=%q", status, text, group)
	}
}

func TestIssuesAreUserScopedAndRepliesHideInternalIDs(t *testing.T) {
	issues := services.NewIssueService(t.TempDir())
	owned, err := issues.CreateIssue(101, "测试", "播放有问题", "会卡住", "movie", "550", "测试电影")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := issues.AddReply(owned.ID, 9001, "管理员", "已经处理", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := issues.CreateIssue(202, "其他人", "别人的问题", "不可见", "", "", ""); err != nil {
		t.Fatal(err)
	}
	handler := NewServer(Deps{BotToken: miniAppTestToken, Issues: issues}).Handler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, signedRequest(t, http.MethodGet, "/api/miniapp/v1/issues", "", 101))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "播放有问题") || strings.Contains(response.Body.String(), "别人的问题") {
		t.Fatalf("issue isolation failed: status=%d body=%s", response.Code, response.Body.String())
	}
	for _, secretField := range []string{`"user_id"`, `"priority"`, `"author_id"`, `"issue_id"`, `"type"`} {
		if strings.Contains(response.Body.String(), secretField) {
			t.Fatalf("internal field %s leaked: %s", secretField, response.Body.String())
		}
	}
}

func TestIssueCreateDerivesOwnerFromSignedInitData(t *testing.T) {
	issues := services.NewIssueService(t.TempDir())
	handler := NewServer(Deps{BotToken: miniAppTestToken, Issues: issues}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, signedRequest(t, http.MethodPost, "/api/miniapp/v1/issues", `{"title":"字幕问题","description":"字幕不同步","user_id":202}`, 101))
	if response.Code != http.StatusCreated {
		t.Fatalf("create failed: status=%d body=%s", response.Code, response.Body.String())
	}
	if len(issues.GetUserIssues(101)) != 1 || len(issues.GetUserIssues(202)) != 0 {
		t.Fatalf("owner was not derived from initData")
	}
}

func TestSearchFilteredEmptyPageStillAdvertisesLaterUpstreamPage(t *testing.T) {
	pages := map[int][]services.SearchResult{
		1: {
			{ID: 1, Title: "电影一", Type: "电影"},
			{ID: 2, Title: "电影二", Type: "movie"},
			{ID: 3, Title: "电影三", Type: "电影"},
		},
		2: {
			{ID: 4, Title: "后页电影一", Type: "电影"},
			{ID: 5, Title: "后页电影二", Type: "movie"},
			{ID: 6, Title: "后页电影三", Type: "电影"},
		},
		3: {{ID: 7, Title: "更后页剧集", Type: "电视剧"}},
	}
	var mu sync.Mutex
	requestedPages := []int{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if got := r.URL.Query().Get("count"); got != "3" {
			t.Errorf("upstream count=%q want 3", got)
		}
		mu.Lock()
		requestedPages = append(requestedPages, page)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(pages[page])
	}))
	defer upstream.Close()

	mp := services.NewMoviePilotClient(upstream.URL, "test", "")
	handler := NewServer(Deps{BotToken: miniAppTestToken, MoviePilot: mp}).Handler()
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, signedRequest(t, http.MethodGet, "/api/miniapp/v1/search?q=test&type=tv&page=1&limit=3", "", 101))
	var firstPage searchResponseView
	if err := json.Unmarshal(first.Body.Bytes(), &firstPage); err != nil {
		t.Fatalf("decode first page: %v body=%s", err, first.Body.String())
	}
	if first.Code != http.StatusOK || len(firstPage.Results) != 0 || !firstPage.HasMore || firstPage.NextPage != 3 || firstPage.Page != 1 || firstPage.Limit != 3 {
		t.Fatalf("unexpected first page: status=%d payload=%+v", first.Code, firstPage)
	}
	mu.Lock()
	if got := append([]int(nil), requestedPages...); len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		mu.Unlock()
		t.Fatalf("bounded page probes=%v want [1 2 3]", got)
	}
	mu.Unlock()

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, signedRequest(t, http.MethodGet, "/api/miniapp/v1/search?q=test&type=tv&page=3&limit=3", "", 101))
	var secondPage searchResponseView
	if err := json.Unmarshal(second.Body.Bytes(), &secondPage); err != nil {
		t.Fatalf("decode second page: %v body=%s", err, second.Body.String())
	}
	if second.Code != http.StatusOK || len(secondPage.Results) != 1 || secondPage.Results[0].ID != 7 || secondPage.Results[0].Type != "tv" || secondPage.HasMore {
		t.Fatalf("unexpected second page: status=%d payload=%+v", second.Code, secondPage)
	}
}

func TestSearchCancellationStopsMoviePilotRequest(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
	}))
	defer upstream.Close()

	mp := services.NewMoviePilotClient(upstream.URL, "test", "")
	mp.SetRetryConfig(&services.RetryConfig{MaxAttempts: 1})
	handler := NewServer(Deps{BotToken: miniAppTestToken, MoviePilot: mp}).Handler()
	req := signedRequest(t, http.MethodGet, "/api/miniapp/v1/search?q=test", "", 101)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), req)
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("MoviePilot request did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("search handler did not stop after cancellation")
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("MoviePilot request context was not cancelled")
	}
}
