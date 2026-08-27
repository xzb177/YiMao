package services

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testLandscapeJPEG(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 20, G: 12, B: 8, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
	return buf.Bytes()
}

func TestWelcomeHeroShanghaiDateCacheAndRefetch(t *testing.T) {
	jpegBytes := testLandscapeJPEG(1280, 720)
	var trending atomic.Int32
	var images atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/trending/movie/") {
			trending.Add(1)
			_ = json.NewEncoder(w).Encode(TMDBTrendingResult{Results: []TMDBTrendingMediaInfo{{ID: 550, Title: "Fight Club", BackdropPath: "/back.jpg"}}})
			return
		}
		if strings.Contains(r.URL.Path, "/trending/tv/") || strings.Contains(r.URL.Path, "/movie/popular") {
			_ = json.NewEncoder(w).Encode(TMDBTrendingResult{})
			return
		}
		if strings.Contains(r.URL.Path, "/w1280/back.jpg") || strings.HasSuffix(r.URL.Path, "/back.jpg") {
			images.Add(1)
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(jpegBytes)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	client := NewTMDBClient("test")
	client.baseURL = srv.URL
	client.httpClient = srv.Client()
	client.retryConfig = &RetryConfig{MaxAttempts: 1, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, Multiplier: 1}
	clock := time.Date(2026, 8, 27, 21, 0, 0, 0, time.FixedZone("CST", 8*3600))
	cache := NewWelcomeHeroCache(t.TempDir(), client, []byte("PNGFALLBACK"))
	cache.imageBase = srv.URL
	cache.httpClient = srv.Client()
	cache.now = func() time.Time { return clock }
	cache.pick = func(n int) int { return 0 }
	first := cache.Get()
	if first.Date != "2026-08-27" || first.TMDBID != 550 || first.Size != "w1280" {
		t.Fatalf("first=%+v", first)
	}
	if _, format, err := image.Decode(bytes.NewReader(first.Bytes)); err != nil || (format != "jpeg" && format != "jpg") {
		t.Fatalf("decode first: format=%s err=%v", format, err)
	}
	if trending.Load() != 1 || images.Load() != 1 {
		t.Fatalf("first-day calls trending=%d images=%d", trending.Load(), images.Load())
	}
	second := cache.Get()
	if second.TMDBID != 550 || trending.Load() != 1 || images.Load() != 1 {
		t.Fatalf("same-day reused? trending=%d images=%d id=%d", trending.Load(), images.Load(), second.TMDBID)
	}
	clock = clock.Add(24 * time.Hour)
	third := cache.Get()
	if third.Date != "2026-08-28" {
		t.Fatalf("next day date=%s", third.Date)
	}
	if trending.Load() != 2 {
		t.Fatalf("next day should refetch trending=%d", trending.Load())
	}
}

func TestWelcomeHeroTMDBFailUsesFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, "nope")
	}))
	defer srv.Close()
	client := NewTMDBClient("test")
	client.baseURL = srv.URL
	client.httpClient = srv.Client()
	client.retryConfig = &RetryConfig{MaxAttempts: 1, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, Multiplier: 1}
	cache := NewWelcomeHeroCache(t.TempDir(), client, []byte("PNGFALLBACK"))
	cache.imageBase = srv.URL
	cache.httpClient = srv.Client()
	hero := cache.Get()
	if string(hero.Bytes) != "PNGFALLBACK" || hero.Filename != "welcome_hero.png" {
		t.Fatalf("want fallback, got filename=%s bytes=%q", hero.Filename, hero.Bytes)
	}
}
