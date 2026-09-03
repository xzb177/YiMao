package services

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newWashTargetService(t *testing.T, handler http.Handler) *WebhookService {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	return &WebhookService{
		embyURL:           server.URL,
		embyAPIKey:        "test-key",
		embyUserID:        "user-1",
		embySkipTLSVerify: true,
	}
}

func TestHasEmbyWashTargetMovie(t *testing.T) {
	svc := newWashTargetService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Users/user-1/Items" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Items":[{"Id":"movie-1","Name":"Fight Club","ProductionYear":1999,"Type":"Movie","ProviderIds":{"Tmdb":"550"}}],"TotalRecordCount":1}`))
	}))

	ok, err := svc.HasEmbyWashTarget(550, "Fight Club", 1999, MediaTypeMovie, 0, 0)
	if err != nil || !ok {
		t.Fatalf("got ok=%v err=%v", ok, err)
	}
	wrong, err := svc.HasEmbyWashTarget(551, "Fight Club", 1999, MediaTypeMovie, 0, 0)
	if err != nil || wrong {
		t.Fatalf("mismatched TMDB ID accepted: ok=%v err=%v", wrong, err)
	}
}

func TestHasEmbyWashTargetSelectsExactTMDBAcrossAllCandidates(t *testing.T) {
	svc := newWashTargetService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Items":[{"Id":"wrong","Name":"Exact Title","ProductionYear":2026,"Type":"Movie","ProviderIds":{"Tmdb":"999"}},{"Id":"right","Name":"Localized Alias","ProductionYear":2026,"Type":"Movie","ProviderIds":{"Tmdb":"550"}}],"TotalRecordCount":2}`))
	}))
	ok, err := svc.HasEmbyWashTarget(550, "Exact Title", 2026, MediaTypeMovie, 0, 0)
	if err != nil || !ok {
		t.Fatalf("exact TMDB candidate was not selected: ok=%v err=%v", ok, err)
	}
}

func TestHasEmbyWashTargetTVSeason(t *testing.T) {
	svc := newWashTargetService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/Users/user-1/Items":
			_, _ = w.Write([]byte(`{"Items":[{"Id":"series-1","Name":"House of Cards","ProductionYear":2013,"Type":"Series","ProviderIds":{"Tmdb":"1425"}}],"TotalRecordCount":1}`))
		case "/Shows/series-1/Episodes":
			_, _ = w.Write([]byte(`{"Items":[{"ParentIndexNumber":1,"IndexNumber":2,"MediaSources":[{"Path":"/tv/s01e02.mkv"}]},{"ParentIndexNumber":3,"IndexNumber":2,"MediaSources":[{"Path":"/tv/s03e02.mkv"}]}]}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	for _, tt := range []struct {
		season int
		want   bool
	}{{1, true}, {2, false}, {3, true}} {
		ok, err := svc.HasEmbyWashTarget(1425, "House of Cards", 2013, MediaTypeTV, tt.season, 2)
		if err != nil || ok != tt.want {
			t.Fatalf("season=%d got ok=%v err=%v want=%v", tt.season, ok, err, tt.want)
		}
	}
}

func TestHasEmbyWashTargetFailsClosed(t *testing.T) {
	svc := newWashTargetService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	ok, err := svc.HasEmbyWashTarget(999, "Missing", 2026, MediaTypeMovie, 0, 0)
	if err == nil || ok {
		t.Fatalf("got ok=%v err=%v, want fail closed", ok, err)
	}
}

func TestCaptureEmbyWashBaselineMovieSources(t *testing.T) {
	svc := newWashTargetService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Items":[{"Id":"movie-1","ProviderIds":{"Tmdb":"550"},"MediaSources":[{"Path":"/media/old-a.mkv"},{"Path":"/media/old-b.mkv"}]}]}`))
	}))
	baseline, err := svc.CaptureEmbyWashBaseline(550, MediaTypeMovie, 0, 0)
	if err != nil || len(baseline) != 2 || baseline[0] != "/media/old-a.mkv" || baseline[1] != "/media/old-b.mkv" {
		t.Fatalf("baseline=%v err=%v", baseline, err)
	}
}

func TestCaptureEmbyWashBaselineTVUsesExactSeasonEpisodes(t *testing.T) {
	svc := newWashTargetService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/Users/user-1/Items":
			_, _ = w.Write([]byte(`{"Items":[{"Id":"series-1","ProviderIds":{"Tmdb":"1425"}}]}`))
		case "/Shows/series-1/Episodes":
			if got := r.URL.Query().Get("Season"); got != "2" {
				t.Fatalf("Season=%q, want 2", got)
			}
			_, _ = w.Write([]byte(`{"Items":[{"Id":"ep-1","ParentIndexNumber":2,"IndexNumber":1,"MediaSources":[{"Path":"/tv/s02e01-old.mkv"}]},{"Id":"wrong-season","ParentIndexNumber":3,"IndexNumber":1,"MediaSources":[{"Path":"/tv/s03e01.mkv"}]}]}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	baseline, err := svc.CaptureEmbyWashBaseline(1425, MediaTypeTV, 2, 1)
	if err != nil || len(baseline) != 1 || baseline[0] != "/tv/s02e01-old.mkv" {
		t.Fatalf("baseline=%v err=%v", baseline, err)
	}
}
