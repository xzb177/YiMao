package services

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrRequestUserNotBound = errors.New("request user is not bound")
	ErrDuplicateRequest     = errors.New("duplicate active request")
)

type RequestSubmission struct {
	TelegramID int64
	TelegramName string
	TmdbID int
	MediaTitle string
	MediaYear int
	MediaType MediaType
	Season int
	PosterPath string
	Overview string
	EmbyInfo *EmbySearchResult
	Origin string
	Priority string
	AdventureScore int
	AdventureGrade string
	UseQuota bool
}

type RequestSubmissionService struct {
	mapping UserMappingStore
	reviews *ReviewService
	quota *QuotaService
	notify func(*ReviewRequest)
}

func NewRequestSubmissionService(mapping UserMappingStore, reviews *ReviewService, quota *QuotaService, notify func(*ReviewRequest)) *RequestSubmissionService {
	return &RequestSubmissionService{mapping: mapping, reviews: reviews, quota: quota, notify: notify}
}

// Submit is the single creation path used by normal requests and adventure rewards.
// Validation and duplicate checks happen before quota consumption; creation failure rolls it back.
func (s *RequestSubmissionService) Submit(in RequestSubmission) (*ReviewRequest, error) {
	if s == nil || s.mapping == nil || s.reviews == nil {
		return nil, errors.New("request submission service is not configured")
	}
	mpID, ok := s.mapping.GetMoviePilotUserID(in.TelegramID)
	if !ok || mpID == 0 {
		return nil, ErrRequestUserNotBound
	}
	if existing, duplicate := s.reviews.HasActiveSimilarRequest(in.TelegramID, in.TmdbID, in.MediaType, in.Season); duplicate {
		return existing, ErrDuplicateRequest
	}
	mediaType := "movie"
	quotaCost := 1
	if in.MediaType == MediaTypeTV {
		mediaType, quotaCost = "tv", 3
	}
	if in.UseQuota {
		if s.quota == nil {
			return nil, errors.New("quota service is not configured")
		}
		if err := s.quota.UseQuota(in.TelegramID, mediaType); err != nil {
			return nil, err
		}
	}
	origin := in.Origin
	if origin == "" { origin = "normal" }
	review := &ReviewRequest{
		RequestID: fmt.Sprintf("review_%d_%d", in.TelegramID, time.Now().UnixNano()),
		TelegramID: in.TelegramID, TelegramName: in.TelegramName, MoviePilotID: mpID,
		TmdbID: in.TmdbID, MediaTitle: in.MediaTitle, MediaYear: in.MediaYear,
		MediaType: in.MediaType, Season: in.Season, PosterPath: in.PosterPath, Overview: in.Overview,
		EmbyExists: in.EmbyInfo != nil, EmbyInfo: in.EmbyInfo, RequestOrigin: origin,
		Priority: in.Priority, AdventureScore: in.AdventureScore, AdventureGrade: in.AdventureGrade,
	}
	if in.UseQuota { review.QuotaCost = quotaCost }
	if err := s.reviews.CreateRequest(review); err != nil {
		if in.UseQuota { s.quota.RestoreQuota(in.TelegramID, mediaType) }
		return nil, err
	}
	if s.notify != nil { go s.notify(review) }
	return review, nil
}
