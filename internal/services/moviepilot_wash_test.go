package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestWashCreatesVerifiedMoviePilotWashSubscription(t *testing.T) {
	for _, tc := range []struct {
		name            string
		mediaType       MediaType
		season          int
		wantSeason      float64
		wantFullVersion float64
	}{
		{name: "movie", mediaType: MediaTypeMovie},
		{name: "tv season", mediaType: MediaTypeTV, season: 2, wantSeason: 2, wantFullVersion: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var payload map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/api/v1/subscribe":
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					_, _ = w.Write([]byte(`{"success":true,"message":"新增订阅成功","data":{"id":77}}`))
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/subscribe/77":
					typeName := "电影"
					if tc.mediaType == MediaTypeTV {
						typeName = "电视剧"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"id": 77, "tmdbid": 123, "type": typeName, "season": tc.season, "best_version": 1, "best_version_full": 1})
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/subscribe/search/77":
					_, _ = w.Write([]byte(`{"success":true}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			client := NewMoviePilotClient(server.URL, "unused", "")
			req, err := client.RequestWash("Test", 2026, 123, tc.mediaType, tc.season)
			if err != nil || req == nil || req.ID != 77 {
				t.Fatalf("req=%+v err=%v", req, err)
			}
			if got := payload["best_version"]; got != float64(1) {
				t.Fatalf("best_version=%v, want 1", got)
			}
			if tc.mediaType == MediaTypeTV {
				if got := payload["season"]; got != tc.wantSeason {
					t.Fatalf("season=%v, want %v", got, tc.wantSeason)
				}
				if got := payload["best_version_full"]; got != tc.wantFullVersion {
					t.Fatalf("best_version_full=%v, want %v", got, tc.wantFullVersion)
				}
			}
		})
	}
}

func TestRequestWashRejectsExistingOrdinarySubscription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/subscribe":
			_, _ = w.Write([]byte(`{"success":true,"message":"订阅已存在","data":{"id":88}}`))
		case "/api/v1/subscribe/88":
			_, _ = w.Write([]byte(`{"id":88,"best_version":0}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewMoviePilotClient(server.URL, "unused", "")
	if req, err := client.RequestWash("Test", 2026, 123, MediaTypeMovie, 0); err == nil || req != nil {
		t.Fatalf("req=%+v err=%v, want fail closed", req, err)
	}
}

func TestRequestWashRejectsFailedSearchResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":77}}`))
		case r.URL.Path == "/api/v1/subscribe/77":
			_, _ = w.Write([]byte(`{"id":77,"tmdbid":550,"type":"电影","best_version":1}`))
		case r.URL.Path == "/api/v1/subscribe/search/77":
			_, _ = w.Write([]byte(`{"success":false,"message":"search rejected"}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewMoviePilotClient(server.URL, "unused", "")
	if _, err := client.RequestWash("Fight Club", 1999, 550, MediaTypeMovie, 0); err == nil {
		t.Fatal("RequestWash accepted failed search response")
	}
}
