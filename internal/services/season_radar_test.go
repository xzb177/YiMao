package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSeasonRadarTracksBaselineAndNotifiesNewAiredSeason(t *testing.T) {
	seasonCount := 1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seasons := []TVSeason{{SeasonNumber: 1, AirDate: "2024-01-01"}}
		if seasonCount >= 2 {
			seasons = append(seasons, TVSeason{SeasonNumber: 2, AirDate: "2025-01-01"})
		}
		_ = json.NewEncoder(w).Encode(TVDetailsWithSeasons{ID: 123, Name: "测试剧", Seasons: seasons})
	}))
	defer srv.Close()

	tmdb := NewTMDBClient("test")
	tmdb.baseURL = srv.URL
	radar := NewSeasonRadarService(t.TempDir(), tmdb)
	notified := 0
	radar.SetNotifier(func(userID int64, tmdbID int, title string, season TVSeason) bool {
		notified++
		if userID != 42 || tmdbID != 123 || title != "测试剧" || season.SeasonNumber != 2 {
			t.Fatalf("notification=%d %d %q %+v", userID, tmdbID, title, season)
		}
		return true
	})

	radar.TrackTV(42, 123, "测试剧")
	if item := radar.items[radarKey(42, 123)]; item.KnownSeasons != 1 {
		t.Fatalf("baseline=%+v", item)
	}
	seasonCount = 2
	radar.items[radarKey(42, 123)] = SeasonRadarItem{
		UserID: 42, TmdbID: 123, Title: "测试剧", KnownSeasons: 1,
	}
	radar.Scan()
	if notified != 1 {
		t.Fatalf("notified=%d want 1", notified)
	}
	if item := radar.items[radarKey(42, 123)]; item.KnownSeasons != 2 {
		t.Fatalf("after scan=%+v", item)
	}
}

func TestSeasonRadarDisabledAdvancesBaselineWithoutNotification(t *testing.T) {
	tmdb := NewTMDBClient("test")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(TVDetailsWithSeasons{ID: 123, Seasons: []TVSeason{
			{SeasonNumber: 1, AirDate: "2024-01-01"},
			{SeasonNumber: 2, AirDate: "2025-01-01"},
			{SeasonNumber: 3, AirDate: "2999-01-01"},
		}})
	}))
	defer srv.Close()
	tmdb.baseURL = srv.URL
	radar := NewSeasonRadarService(t.TempDir(), tmdb)
	radar.SetNotifier(func(int64, int, string, TVSeason) bool { return false })
	radar.SetEnabled(func(int64) bool { return false })
	radar.items[radarKey(1, 123)] = SeasonRadarItem{UserID: 1, TmdbID: 123, Title: "剧", KnownSeasons: 1}
	radar.Scan()
	if got := radar.items[radarKey(1, 123)].KnownSeasons; got != 2 {
		t.Fatalf("disabled baseline=%d want latest aired 2 (future season must remain pending)", got)
	}
}
