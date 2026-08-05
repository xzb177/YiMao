package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbyMediaAvailabilityByTMDBSeason(t *testing.T) {
	tests := []struct {
		name   string
		season int
		want   bool
	}{
		{name: "existing season", season: 2, want: true},
		{name: "missing season", season: 3, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Emby-Token") != "emby-key" {
					t.Fatalf("missing Emby token")
				}
				if r.URL.Query().Get("AnyProviderIdEquals") == "tmdb.123" {
					_ = json.NewEncoder(w).Encode(map[string]any{"Items": []map[string]any{{"Id": "series-1"}}, "TotalRecordCount": 1})
					return
				}
				if r.URL.Query().Get("ParentId") == "series-1" {
					_ = json.NewEncoder(w).Encode(map[string]any{"Items": []map[string]any{{"IndexNumber": 1}, {"IndexNumber": 2}}, "TotalRecordCount": 2})
					return
				}
				http.Error(w, "unexpected request", http.StatusBadRequest)
			}))
			defer server.Close()

			client := NewMoviePilotClient("http://moviepilot.invalid", "mp-key", "")
			client.SetEmbyConfig(server.URL, "emby-key")
			client.SetEmbyUserID("user-1")
			got, err := client.EmbyMediaAvailabilityByTMDBSeason(123, tt.season)
			if err != nil {
				t.Fatalf("availability error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("availability = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEmbyMediaAvailabilityByTMDBSeasonRejectsInvalidInput(t *testing.T) {
	client := NewMoviePilotClient("http://moviepilot.invalid", "mp-key", "")
	if _, err := client.EmbyMediaAvailabilityByTMDBSeason(123, 0); err == nil {
		t.Fatal("expected invalid season error")
	}
}
