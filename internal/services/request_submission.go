package services

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrRequestUserNotBound = errors.New("request user is not bound")
	ErrDuplicateRequest    = errors.New("duplicate active request")
	ErrInvalidSubmission   = errors.New("invalid request submission")
	ErrQuotaExceeded       = errors.New("request quota exceeded")
)

type SubmissionStatus string

const (
	SubmissionCreated        SubmissionStatus = "created"
	SubmissionDuplicateOwn   SubmissionStatus = "duplicate_own"
	SubmissionDuplicateOther SubmissionStatus = "duplicate_other"
	SubmissionNotBound       SubmissionStatus = "not_bound"
	SubmissionQuotaExceeded  SubmissionStatus = "quota_exceeded"
	SubmissionFailed         SubmissionStatus = "failed"
)

type SubmissionResult struct {
	Status SubmissionStatus
	Review *ReviewRequest
}

// SubmissionCompensationError preserves both the failed creation and failed refund
// for logging/auditing and supports errors.Is/errors.As through Unwrap.
type SubmissionCompensationError struct {
	Review       *ReviewRequest
	CreateError  error
	RestoreError error
}

func (e *SubmissionCompensationError) Error() string {
	return fmt.Sprintf("create request failed (%v) and quota compensation failed (%v); request_id=%s telegram_id=%d", e.CreateError, e.RestoreError, e.Review.RequestID, e.Review.TelegramID)
}
func (e *SubmissionCompensationError) Unwrap() []error { return []error{e.CreateError, e.RestoreError} }

type submissionQuota interface {
	UseQuota(int64, string) error
	RestoreQuota(int64, string) error
}

type RequestSubmission struct {
	TelegramID     int64
	TelegramName   string
	TmdbID         int
	MediaTitle     string
	MediaYear      int
	MediaType      MediaType
	Season         int
	PosterPath     string
	Overview       string
	EmbyInfo       *EmbySearchResult
	Origin         string
	Priority       string
	AdventureScore int
	AdventureGrade string
	UseQuota       bool
}

type RequestSubmissionService struct {
	mapping UserMappingStore
	reviews *ReviewService
	quota   submissionQuota
	notify  func(*ReviewRequest)
	mu      sync.Mutex // serializes content check, quota transaction, and create in this process
	create  func(*ReviewRequest) error
}

func NewRequestSubmissionService(mapping UserMappingStore, reviews *ReviewService, quota *QuotaService, notify func(*ReviewRequest)) *RequestSubmissionService {
	s := &RequestSubmissionService{mapping: mapping, reviews: reviews, quota: quota, notify: notify}
	if reviews != nil {
		s.create = reviews.CreateRequest
	}
	return s
}

// Submit retains the original API. New callers can use SubmitResult for typed semantics.
func (s *RequestSubmissionService) Submit(in RequestSubmission) (*ReviewRequest, error) {
	result, err := s.SubmitResult(in)
	if err != nil {
		return result.Review, err
	}
	switch result.Status {
	case SubmissionDuplicateOwn, SubmissionDuplicateOther:
		return result.Review, ErrDuplicateRequest
	case SubmissionNotBound:
		return nil, ErrRequestUserNotBound
	case SubmissionQuotaExceeded:
		return nil, ErrQuotaExceeded
	default:
		return result.Review, nil
	}
}

func (s *RequestSubmissionService) SubmitResult(in RequestSubmission) (SubmissionResult, error) {
	if s == nil || s.mapping == nil || s.reviews == nil || s.create == nil {
		return SubmissionResult{}, errors.New("request submission service is not configured")
	}
	if in.TmdbID <= 0 || (in.MediaType != MediaTypeMovie && in.MediaType != MediaTypeTV) || in.Season < 0 {
		return SubmissionResult{}, fmt.Errorf("%w: tmdb_id must be positive, media_type must be movie or tv, and season must be non-negative", ErrInvalidSubmission)
	}
	mpID, ok := s.mapping.GetMoviePilotUserID(in.TelegramID)
	if !ok || mpID == 0 {
		return SubmissionResult{Status: SubmissionNotBound}, nil
	}

	s.mu.Lock()
	result, err := s.submitLocked(in, mpID)
	s.mu.Unlock()
	if err == nil && result.Status == SubmissionCreated && s.notify != nil {
		notification := cloneReview(result.Review)
		go func() { defer func() { _ = recover() }(); s.notify(notification) }()
	}
	return result, err
}

func (s *RequestSubmissionService) submitLocked(in RequestSubmission, mpID int64) (SubmissionResult, error) {
	if existing, duplicate := s.reviews.HasActiveSimilarContent(in.TmdbID, in.MediaType, in.Season); duplicate {
		status := SubmissionDuplicateOther
		if existing.TelegramID == in.TelegramID {
			status = SubmissionDuplicateOwn
		}
		return SubmissionResult{Status: status, Review: cloneReview(existing)}, nil
	}
	mediaType, quotaCost := "movie", 1
	if in.MediaType == MediaTypeTV {
		mediaType, quotaCost = "tv", 3
	}
	if in.UseQuota {
		if s.quota == nil {
			return SubmissionResult{}, errors.New("quota service is not configured")
		}
		if err := s.quota.UseQuota(in.TelegramID, mediaType); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "quota exceeded") {
				return SubmissionResult{Status: SubmissionQuotaExceeded}, fmt.Errorf("%w: %v", ErrQuotaExceeded, err)
			}
			return SubmissionResult{Status: SubmissionFailed}, fmt.Errorf("use quota: %w", err)
		}
	}
	origin := in.Origin
	if origin == "" {
		origin = "normal"
	}
	review := &ReviewRequest{RequestID: fmt.Sprintf("review_%d_%d", in.TelegramID, time.Now().UnixNano()), TelegramID: in.TelegramID, TelegramName: in.TelegramName, MoviePilotID: mpID, TmdbID: in.TmdbID, MediaTitle: in.MediaTitle, MediaYear: in.MediaYear, MediaType: in.MediaType, Season: in.Season, PosterPath: in.PosterPath, Overview: in.Overview, EmbyExists: in.EmbyInfo != nil, EmbyInfo: cloneEmby(in.EmbyInfo), RequestOrigin: origin, Priority: in.Priority, AdventureScore: in.AdventureScore, AdventureGrade: in.AdventureGrade}
	if in.UseQuota {
		review.QuotaCost = quotaCost
	}
	if err := s.create(review); err != nil {
		if in.UseQuota {
			if restoreErr := s.quota.RestoreQuota(in.TelegramID, mediaType); restoreErr != nil {
				return SubmissionResult{}, &SubmissionCompensationError{Review: cloneReview(review), CreateError: err, RestoreError: restoreErr}
			}
		}
		return SubmissionResult{}, err
	}
	return SubmissionResult{Status: SubmissionCreated, Review: cloneReview(review)}, nil
}

func cloneEmby(in *EmbySearchResult) *EmbySearchResult {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
func cloneReview(in *ReviewRequest) *ReviewRequest {
	if in == nil {
		return nil
	}
	out := *in
	out.EmbyInfo = cloneEmby(in.EmbyInfo)
	return &out
}
