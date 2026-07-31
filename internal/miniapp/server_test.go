package miniapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/services"
)

const miniAppTestToken = "123456:TEST_TOKEN_FOR_UNIT_TEST_ONLY"

func signedRequest(t *testing.T, method, target, body string, userID int64) *http.Request {
	t.Helper()
	values := url.Values{
		"auth_date": {strconv.FormatInt(time.Now().Unix(), 10)},
		"user":      {`{"id":` + strconv.FormatInt(userID, 10) + `,"first_name":"测试"}`},
	}
	values.Set("hash", signInitDataForTest(miniAppTestToken, values))
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("X-Telegram-Init-Data", values.Encode())
	req.Header.Set("Content-Type", "application/json")
	return req
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
