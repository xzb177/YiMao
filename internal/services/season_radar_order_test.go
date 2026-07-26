package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSeasonRadarNotifiesAiredSeasonsInOrderAndKeepsFuturePending(t *testing.T) {
	tmdb := NewTMDBClient("test")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(TVDetailsWithSeasons{ID: 321, Seasons: []TVSeason{
			{SeasonNumber: 3, AirDate: "2025-03-01"},
			{SeasonNumber: 4, AirDate: "2999-01-01"},
			{SeasonNumber: 2, AirDate: "2025-02-01"},
			{SeasonNumber: 1, AirDate: "2024-01-01"},
		}})
	}))
	defer srv.Close()
	tmdb.baseURL = srv.URL
	radar := NewSeasonRadarService(t.TempDir(), tmdb)
	var got []int
	radar.SetNotifier(func(_ int64, _ int, _ string, season TVSeason) bool {
		got = append(got, season.SeasonNumber)
		return true
	})
	radar.items[radarKey(7, 321)] = SeasonRadarItem{UserID: 7, TmdbID: 321, Title: "剧", KnownSeasons: 1}
	radar.Scan()
	if len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Fatalf("notifications=%v want [2 3]", got)
	}
	if known := radar.items[radarKey(7, 321)].KnownSeasons; known != 3 {
		t.Fatalf("baseline=%d want 3; future season 4 must stay pending", known)
	}
}
