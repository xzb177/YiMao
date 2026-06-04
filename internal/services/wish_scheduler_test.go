package services

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/xzb177/yimao/pkg/types"
)

// TestIsUnreachableErr 覆盖坑A 通知失败分流的纯函数逻辑：
//   - 403 / blocked / user not found → 明确不可达（ORPHANED）。
//   - 超时 / 5xx / 429 / 连接失败 → 临时错误（保留可重试）。
func TestIsUnreachableErr(t *testing.T) {
	unreachable := []error{
		&types.TelegramError{Code: 403, Message: "Forbidden: bot was blocked by the user"},
		&types.TelegramError{Code: 403, Message: "Forbidden: user is deactivated"},
		&types.TelegramError{Code: 400, Message: "Bad Request: chat not found"},
		&types.TelegramError{Code: 400, Message: "Bad Request: user not found"},
		errors.New("Forbidden: bot was blocked by the user"),
		errors.New("telegram: user is deactivated"),
	}
	for _, e := range unreachable {
		if !isUnreachableErr(e) {
			t.Errorf("expected unreachable for %v", e)
		}
	}

	transient := []error{
		nil,
		&types.TelegramError{Code: 500, Message: "Internal Server Error"},
		&types.TelegramError{Code: 502, Message: "Bad Gateway"},
		&types.TelegramError{Code: 429, Message: "Too Many Requests: retry after 5"},
		&types.TelegramError{Code: 400, Message: "Bad Request: message text is empty"},
		errors.New("request failed: dial tcp: i/o timeout"),
		fmt.Errorf("context deadline exceeded"),
		errors.New("connection reset by peer"),
	}
	for _, e := range transient {
		if isUnreachableErr(e) {
			t.Errorf("expected transient (retryable) for %v", e)
		}
	}
}

// TestWishAnnounceKey 覆盖 #2 群内公示去重 key：tmdb / imdb-only / 不同 season / 跨天 区分。
func TestWishAnnounceKey(t *testing.T) {
	s := &WishScheduler{announced: map[string]bool{}}
	day1, _ := time.Parse("2006-01-02", "2024-01-02")
	day2, _ := time.Parse("2006-01-02", "2024-01-03")

	tmdbItem := &WishItem{TmdbID: 100, MediaType: "movie", Season: 0}
	k1 := s.wishAnnounceKey(tmdbItem, day1)
	if k1 != "tmdb-100-movie-0|2024-01-02" {
		t.Errorf("tmdb key 错误: %q", k1)
	}

	// 同片同天 → 同 key。
	if k1b := s.wishAnnounceKey(tmdbItem, day1); k1b != k1 {
		t.Errorf("同片同天应同 key: %q vs %q", k1, k1b)
	}

	// 跨天 → 不同 key。
	if k2 := s.wishAnnounceKey(tmdbItem, day2); k2 == k1 {
		t.Errorf("跨天应不同 key: %q", k2)
	}

	// imdb-only。
	imdbItem := &WishItem{TmdbID: 0, ImdbID: "tt7", MediaType: "movie", Season: 0}
	ki := s.wishAnnounceKey(imdbItem, day1)
	if ki != "imdb-tt7-movie-0|2024-01-02" {
		t.Errorf("imdb key 错误: %q", ki)
	}

	// 不同 season 不同 key（剧集）。
	tvS1 := &WishItem{TmdbID: 200, MediaType: "tv", Season: 1}
	tvS2 := &WishItem{TmdbID: 200, MediaType: "tv", Season: 2}
	if s.wishAnnounceKey(tvS1, day1) == s.wishAnnounceKey(tvS2, day1) {
		t.Errorf("不同 season 应不同 key")
	}
}
