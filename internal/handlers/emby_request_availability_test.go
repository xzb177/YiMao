package handlers

import (
	"errors"
	"testing"

	"github.com/xzb177/yimao/internal/services"
)

func TestRequestExistsInEmbyTVSeasonDoesNotBlockOnSeriesOnly(t *testing.T) {
	identityCalls, seasonCalls := 0, 0
	item, exists, err := requestExistsInEmby(131033, services.MediaTypeTV, 2,
		func(int, services.MediaType) (*services.EmbySearchResult, error) {
			identityCalls++
			return &services.EmbySearchResult{Title: "猥城琐事"}, nil
		},
		func(tmdbID, season int) (bool, error) {
			seasonCalls++
			if tmdbID != 131033 || season != 2 {
				t.Fatalf("lookup=%d season=%d", tmdbID, season)
			}
			return false, nil
		},
	)
	if err != nil || exists || item != nil || identityCalls != 0 || seasonCalls != 1 {
		t.Fatalf("item=%v exists=%v err=%v identity=%d season=%d", item, exists, err, identityCalls, seasonCalls)
	}
}

func TestRequestExistsInEmbyTVSeasonBlocksExactSeason(t *testing.T) {
	_, exists, err := requestExistsInEmby(131033, services.MediaTypeTV, 2,
		func(int, services.MediaType) (*services.EmbySearchResult, error) { return nil, nil },
		func(int, int) (bool, error) { return true, nil },
	)
	if err != nil || !exists {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
}

func TestRequestExistsInEmbyMovieUsesExactIdentity(t *testing.T) {
	want := &services.EmbySearchResult{Title: "蜘蛛侠：纵横宇宙"}
	item, exists, err := requestExistsInEmby(569094, services.MediaTypeMovie, 0,
		func(int, services.MediaType) (*services.EmbySearchResult, error) { return want, nil },
		func(int, int) (bool, error) { t.Fatal("movie must not use season lookup"); return false, nil },
	)
	if err != nil || !exists || item != want {
		t.Fatalf("item=%v exists=%v err=%v", item, exists, err)
	}
}

func TestRequestExistsInEmbySeasonFailureStaysUnknown(t *testing.T) {
	wantErr := errors.New("emby unavailable")
	_, exists, err := requestExistsInEmby(131033, services.MediaTypeTV, 2,
		func(int, services.MediaType) (*services.EmbySearchResult, error) { return nil, nil },
		func(int, int) (bool, error) { return false, wantErr },
	)
	if exists || !errors.Is(err, wantErr) {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
}
