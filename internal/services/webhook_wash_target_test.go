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

	ok, err := svc.HasEmbyWashTarget(550, "Fight Club", 1999, MediaTypeMovie, 0)
	if err != nil || !ok {
		t.Fatalf("got ok=%v err=%v", ok, err)
	}
	wrong, err := svc.HasEmbyWashTarget(551, "Fight Club", 1999, MediaTypeMovie, 0)
	if err != nil || wrong {
		t.Fatalf("mismatched TMDB ID accepted: ok=%v err=%v", wrong, err)
	}
}

func TestHasEmbyWashTargetSelectsExactTMDBAcrossAllCandidates(t *testing.T) {
	svc := newWashTargetService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Items":[{"Id":"wrong","Name":"Exact Title","ProductionYear":2026,"Type":"Movie","ProviderIds":{"Tmdb":"999"}},{"Id":"right","Name":"Localized Alias","ProductionYear":2026,"Type":"Movie","ProviderIds":{"Tmdb":"550"}}],"TotalRecordCount":2}`))
	}))
	ok, err := svc.HasEmbyWashTarget(550, "Exact Title", 2026, MediaTypeMovie, 0)
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
		case "/Shows/series-1/Seasons":
			_, _ = w.Write([]byte(`{"Items":[{"IndexNumber":1},{"IndexNumber":3}]}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	for _, tt := range []struct {
		season int
		want   bool
	}{{1, true}, {2, false}, {3, true}} {
		ok, err := svc.HasEmbyWashTarget(1425, "House of Cards", 2013, MediaTypeTV, tt.season)
		if err != nil || ok != tt.want {
			t.Fatalf("season=%d got ok=%v err=%v want=%v", tt.season, ok, err, tt.want)
		}
	}
}

func TestHasEmbyWashTargetFailsClosed(t *testing.T) {
	svc := newWashTargetService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	ok, err := svc.HasEmbyWashTarget(999, "Missing", 2026, MediaTypeMovie, 0)
	if err == nil || ok {
		t.Fatalf("got ok=%v err=%v, want fail closed", ok, err)
	}
}
