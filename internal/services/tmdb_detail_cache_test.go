package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTMDBMovieDetailsCoalescesConcurrentRequestsAndCaches(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(40 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(TMDBMediaInfo{ID: 550, Title: "搏击俱乐部", PosterPath: "/poster.jpg"})
	}))
	defer srv.Close()
	client := NewTMDBClient("test")
	client.baseURL = srv.URL
	client.httpClient = srv.Client()

	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, err := client.GetMovieDetails(550)
			if err == nil && (item == nil || item.Title != "搏击俱乐部") {
				t.Errorf("unexpected item: %+v", item)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("upstream calls=%d want 1", got)
	}
	if _, err := client.GetMovieDetails(550); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("cache miss: calls=%d", got)
	}
}

func TestTMDBTVDetailsSharedAcrossBasicAndSeasonViews(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(TVDetailsWithSeasons{ID: 100, Name: "测试剧", PosterPath: "/tv.jpg", Seasons: []TVSeason{{SeasonNumber: 1, Name: "第一季"}}})
	}))
	defer srv.Close()
	client := NewTMDBClient("test")
	client.baseURL = srv.URL
	client.httpClient = srv.Client()

	basic, err := client.GetTVDetails(100)
	if err != nil || basic == nil || basic.Name != "测试剧" {
		t.Fatalf("basic=%+v err=%v", basic, err)
	}
	full, err := client.GetTVDetailsWithSeasons(100)
	if err != nil || full == nil || len(full.Seasons) != 1 {
		t.Fatalf("full=%+v err=%v", full, err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("upstream calls=%d want 1", got)
	}
}
