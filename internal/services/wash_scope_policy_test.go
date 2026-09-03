package services

import (
	"errors"
	"testing"
)

func TestSubmitTVWashRequiresExactlyOneEpisodeAndDedupesByEpisode(t *testing.T) {
	s := newSubmissionTestService(t, &fakeSubmissionQuota{}, nil)
	base := RequestSubmission{BusinessType: BusinessTypeWash, TelegramID: 1, TmdbID: 42, MediaType: MediaTypeTV, Season: 1, WashBaseline: []string{"old"}}
	for _, in := range []RequestSubmission{base, func() RequestSubmission { x := base; x.Episode = 0; return x }()} {
		if _, err := s.SubmitResult(in); !errors.Is(err, ErrInvalidSubmission) {
			t.Fatalf("scope=%+v err=%v, want invalid", in, err)
		}
	}
	first := base
	first.Episode = 1
	if got, err := s.SubmitResult(first); err != nil || got.Status != SubmissionCreated || got.Review.Episode != 1 {
		t.Fatalf("first=%+v err=%v", got, err)
	}
	otherEpisode := first
	otherEpisode.Episode = 2
	t.Logf("first review=%+v other input=%+v", first, otherEpisode)
	if got, err := s.SubmitResult(otherEpisode); err != nil || got.Status != SubmissionCreated {
		t.Fatalf("other episode=%+v err=%v", got, err)
	}
	if got, err := s.SubmitResult(first); err != nil || got.Status != SubmissionDuplicateOwn {
		t.Fatalf("same episode=%+v err=%v", got, err)
	}
}

func TestSubmitMovieWashRequiresZeroSeasonAndEpisode(t *testing.T) {
	s := newSubmissionTestService(t, &fakeSubmissionQuota{}, nil)
	for _, in := range []RequestSubmission{{BusinessType: BusinessTypeWash, TelegramID: 1, TmdbID: 42, MediaType: MediaTypeMovie, Season: 1}, {BusinessType: BusinessTypeWash, TelegramID: 1, TmdbID: 42, MediaType: MediaTypeMovie, Episode: 1}} {
		if _, err := s.SubmitResult(in); !errors.Is(err, ErrInvalidSubmission) {
			t.Fatalf("scope=%+v err=%v, want invalid", in, err)
		}
	}
}

func TestReviewServiceRejectsInvalidWashScopeOnDirectCreate(t *testing.T) {
	s := NewReviewService(t.TempDir(), false)
	for _, review := range []*ReviewRequest{
		{RequestID: "series", BusinessType: BusinessTypeWash, MediaType: MediaTypeTV, TmdbID: 1},
		{RequestID: "season", BusinessType: BusinessTypeWash, MediaType: MediaTypeTV, TmdbID: 1, Season: 1},
		{RequestID: "movie-episode", BusinessType: BusinessTypeWash, MediaType: MediaTypeMovie, TmdbID: 1, Episode: 1},
	} {
		if err := s.CreateRequest(review); err == nil {
			t.Fatalf("accepted invalid wash: %#v", review)
		}
	}
	valid := &ReviewRequest{RequestID: "episode", BusinessType: BusinessTypeWash, MediaType: MediaTypeTV, TmdbID: 1, Season: 1, Episode: 2}
	if err := s.CreateRequest(valid); err != nil {
		t.Fatalf("valid single episode rejected: %v", err)
	}
}
