package services

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newApprovedWashForWebhookTest(t *testing.T, baseline []string) *ReviewService {
	t.Helper()
	reviews := NewReviewService(t.TempDir(), false)
	review := &ReviewRequest{
		RequestID: "wash-auto", BusinessType: BusinessTypeWash, TelegramID: 7,
		TmdbID: 550, MediaType: MediaTypeMovie, MediaTitle: "Fight Club", WashBaseline: baseline,
	}
	if err := reviews.CreateRequest(review); err != nil {
		t.Fatal(err)
	}
	if _, err := reviews.Approve(review.RequestID, 99, review.ApproveToken); err != nil {
		t.Fatal(err)
	}
	return reviews
}

func newApprovedTVWashForWebhookTest(t *testing.T, baseline []string) *ReviewService {
	t.Helper()
	reviews := NewReviewService(t.TempDir(), false)
	review := &ReviewRequest{
		RequestID: "wash-tv-auto", BusinessType: BusinessTypeWash, TelegramID: 7,
		TmdbID: 1425, MediaType: MediaTypeTV, Season: 2, MediaTitle: "House of Cards", WashBaseline: baseline,
	}
	if err := reviews.CreateRequest(review); err != nil {
		t.Fatal(err)
	}
	if _, err := reviews.Approve(review.RequestID, 99, review.ApproveToken); err != nil {
		t.Fatal(err)
	}
	return reviews
}

func TestLibraryAddAutomaticallyCompletesVerifiedWash(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Users/user/Items/item-1" || r.Header.Get("X-Emby-Token") != "key" {
			t.Fatalf("unexpected request: path=%s token=%q", r.URL.Path, r.Header.Get("X-Emby-Token"))
		}
		_, _ = w.Write([]byte(`{"MediaSources":[{"Path":"/media/old.mkv"},{"Path":"/media/new.mkv"}]}`))
	}))
	defer api.Close()

	reviews := newApprovedWashForWebhookTest(t, []string{"/media/old.mkv"})
	webhook := &WebhookService{review: reviews, embyURL: api.URL, embyAPIKey: "key", embyUserID: "user"}
	webhook.completeWashOnLibraryAdd("550", "movie", 0, "item-1")

	got, _ := reviews.GetRequest("wash-auto")
	if got.Status != "completed" || got.ReviewedBy != 0 {
		t.Fatalf("review=%+v", got)
	}
	webhook.completeWashOnLibraryAdd("550", "movie", 0, "item-1")
}

func TestLibraryAddKeepsClaimedWashUnderAdministratorControl(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"MediaSources":[{"Path":"/media/old.mkv"},{"Path":"/media/new.mkv"}]}`))
	}))
	defer api.Close()

	reviews := newApprovedWashForWebhookTest(t, []string{"/media/old.mkv"})
	if _, err := reviews.ClaimWash("wash-auto", 99); err != nil {
		t.Fatal(err)
	}
	webhook := &WebhookService{review: reviews, embyURL: api.URL, embyAPIKey: "key", embyUserID: "user"}
	webhook.completeWashOnLibraryAdd("550", "movie", 0, "item-1")

	got, _ := reviews.GetRequest("wash-auto")
	if got.Status != "claimed" || got.WashClaimedBy != 99 {
		t.Fatalf("review=%+v", got)
	}
}

func TestLibraryAddKeepsWashApprovedWhenOldVersionIsMissing(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"MediaSources":[{"Path":"/media/new.mkv"}]}`))
	}))
	defer api.Close()

	reviews := newApprovedWashForWebhookTest(t, []string{"/media/old.mkv"})
	webhook := &WebhookService{review: reviews, embyURL: api.URL, embyAPIKey: "key", embyUserID: "user"}
	webhook.completeWashOnLibraryAdd("550", "movie", 0, "item-1")

	got, _ := reviews.GetRequest("wash-auto")
	if got.Status != "approved" {
		t.Fatalf("status=%q, want approved", got.Status)
	}
}

func TestLibraryAddKeepsWashApprovedWhenEmbyLookupFails(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	defer api.Close()

	reviews := newApprovedWashForWebhookTest(t, []string{"/media/old.mkv"})
	webhook := &WebhookService{review: reviews, embyURL: api.URL, embyAPIKey: "key", embyUserID: "user"}
	webhook.completeWashOnLibraryAdd("550", "movie", 0, "item-1")

	got, _ := reviews.GetRequest("wash-auto")
	if got.Status != "approved" {
		t.Fatalf("status=%q, want approved", got.Status)
	}
}

func TestItemAddedCompletesMovieWashBeforeNotificationPreferenceGate(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"MediaSources":[{"Path":"/media/old.mkv"},{"Path":"/media/new.mkv"}]}`))
	}))
	defer api.Close()

	reviews := newApprovedWashForWebhookTest(t, []string{"/media/old.mkv"})
	webhook := &WebhookService{
		review: reviews, embyURL: api.URL, embyAPIKey: "test-key", embyUserID: "user",
		mediaNotificationSvc: &MediaNotificationService{settings: map[int64]*AdminNotificationSettings{1: {SingleEnabled: false}}},
	}
	payload := EmbyWebhookPayload{Item: &EmbyItem{
		Id: "item-1", Type: "Movie", ProviderIds: map[string]string{"Tmdb": "550"},
	}}
	if err := webhook.handleItemAdded(payload); err != nil {
		t.Fatal(err)
	}
	got, _ := reviews.GetRequest("wash-auto")
	if got.Status != "completed" {
		t.Fatalf("status=%q, want completed", got.Status)
	}
}

func TestItemAddedCompletesEpisodeWashBeforeAggregation(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Users/user/Items/series-1":
			if r.Header.Get("X-Emby-Token") != "test-key" {
				t.Fatalf("missing Emby token")
			}
			_, _ = w.Write([]byte(`{"Id":"series-1","ProviderIds":{"Tmdb":"1425"}}`))
		case "/Users/user/Items":
			_, _ = w.Write([]byte(`{"Items":[{"Id":"series-1","ProviderIds":{"Tmdb":"1425"}}]}`))
		case "/Shows/series-1/Episodes":
			if r.URL.Query().Get("Season") != "2" {
				t.Fatalf("Season=%q, want 2", r.URL.Query().Get("Season"))
			}
			_, _ = w.Write([]byte(`{"Items":[{"ParentIndexNumber":2,"MediaSources":[{"Path":"/media/old-s02e01.mkv"},{"Path":"/media/new-s02e01.mkv"}]},{"ParentIndexNumber":2,"MediaSources":[{"Path":"/media/old-s02e02.mkv"}]}]}`))
		case "/Users/user/Items/episode-1", "/Users/test-key/Items/series-1", "/Users/test-key/Items/episode-1":
			_, _ = w.Write([]byte(`{"MediaSources":[{"Path":"/media/old-s02e01.mkv"},{"Path":"/media/new-s02e01.mkv"}]}`))
		default:
			t.Fatalf("unexpected Emby path %q", r.URL.Path)
		}
	}))
	defer api.Close()

	reviews := newApprovedTVWashForWebhookTest(t, []string{"/media/old-s02e01.mkv", "/media/old-s02e02.mkv"})
	webhook := &WebhookService{
		review: reviews, embyURL: api.URL, embyAPIKey: "test-key", embyUserID: "user",
		epAggregation: make(map[string]*EpisodeAggregation), aggregationDelay: time.Hour,
		tmdbClient:    http.DefaultClient,
		fileInfoCache: make(map[string]*cachedFileInfo), fileInfoCacheTTL: time.Hour,
		mediaNotificationSvc: &MediaNotificationService{settings: map[int64]*AdminNotificationSettings{1: {SingleEnabled: false}}},
	}
	season, episode := 2, 1
	payload := EmbyWebhookPayload{Item: &EmbyItem{
		Id: "episode-1", Type: "Episode", SeriesId: "series-1", SeriesName: "House of Cards",
		ParentIndexNumber: &season, IndexNumber: &episode,
	}}
	if err := webhook.handleItemAdded(payload); err != nil {
		t.Fatal(err)
	}
	got, _ := reviews.GetRequest("wash-tv-auto")
	if got.Status != "completed" {
		t.Fatalf("status=%q, want completed", got.Status)
	}
}
