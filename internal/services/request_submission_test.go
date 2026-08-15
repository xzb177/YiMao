package services

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type submissionMapping struct{ UserMappingStore }

func (submissionMapping) GetMoviePilotUserID(id int64) (int64, bool) { return id + 100, id != 0 }

type fakeSubmissionQuota struct {
	mu                 sync.Mutex
	used, restored     int
	useErr, restoreErr error
}

func (q *fakeSubmissionQuota) UseQuota(_ int64, media string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.useErr != nil {
		return q.useErr
	}
	if media == "tv" {
		q.used += 3
	} else {
		q.used++
	}
	return nil
}
func (q *fakeSubmissionQuota) RestoreQuota(_ int64, media string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.restoreErr != nil {
		return q.restoreErr
	}
	if media == "tv" {
		q.restored += 3
	} else {
		q.restored++
	}
	return nil
}

func newSubmissionTestService(t *testing.T, q submissionQuota, notify func(*ReviewRequest)) *RequestSubmissionService {
	t.Helper()
	r := &ReviewService{reviewsFile: t.TempDir() + "/reviews.json", reviews: make(map[string]*ReviewRequest)}
	s := &RequestSubmissionService{mapping: submissionMapping{}, reviews: r, quota: q, notify: notify, create: r.CreateRequest}
	return s
}
func validSubmission(user int64, mt MediaType) RequestSubmission {
	return RequestSubmission{TelegramID: user, TmdbID: 42, MediaType: mt, Season: 0, UseQuota: true}
}

func TestSubmitConcurrentSameUserAtomic(t *testing.T) {
	q := &fakeSubmissionQuota{}
	s := newSubmissionTestService(t, q, nil)
	var wg sync.WaitGroup
	statuses := make(chan SubmissionStatus, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := s.SubmitResult(validSubmission(1, MediaTypeMovie))
			if e != nil {
				t.Errorf("SubmitResult: %v", e)
				return
			}
			statuses <- r.Status
		}()
	}
	wg.Wait()
	close(statuses)
	created := 0
	for st := range statuses {
		if st == SubmissionCreated {
			created++
		}
	}
	if created != 1 || len(s.reviews.reviews) != 1 || q.used != 1 {
		t.Fatalf("created=%d reviews=%d charged=%d", created, len(s.reviews.reviews), q.used)
	}
}

func TestSubmitConcurrentOtherUserCarpools(t *testing.T) {
	q := &fakeSubmissionQuota{}
	s := newSubmissionTestService(t, q, nil)
	var wg sync.WaitGroup
	statuses := make(chan SubmissionStatus, 20)
	for i := int64(1); i <= 20; i++ {
		wg.Add(1)
		go func(user int64) {
			defer wg.Done()
			r, e := s.SubmitResult(validSubmission(user, MediaTypeTV))
			if e != nil {
				t.Errorf("SubmitResult: %v", e)
				return
			}
			statuses <- r.Status
		}(i)
	}
	wg.Wait()
	close(statuses)
	created, other := 0, 0
	for st := range statuses {
		if st == SubmissionCreated {
			created++
		}
		if st == SubmissionDuplicateOther {
			other++
		}
	}
	if created != 1 || other != 19 || q.used != 3 {
		t.Fatalf("created=%d other=%d charged=%d", created, other, q.used)
	}
}

func TestSubmitCreateFailureCompensation(t *testing.T) {
	q := &fakeSubmissionQuota{}
	s := newSubmissionTestService(t, q, nil)
	createErr := errors.New("disk failed")
	s.create = func(*ReviewRequest) error { return createErr }
	_, err := s.SubmitResult(validSubmission(1, MediaTypeMovie))
	if !errors.Is(err, createErr) || q.restored != 1 {
		t.Fatalf("err=%v restored=%d", err, q.restored)
	}
}
func TestSubmitCompensationFailureIsCompound(t *testing.T) {
	restoreErr := errors.New("refund failed")
	createErr := errors.New("create failed")
	q := &fakeSubmissionQuota{restoreErr: restoreErr}
	s := newSubmissionTestService(t, q, nil)
	s.create = func(*ReviewRequest) error { return createErr }
	_, err := s.SubmitResult(validSubmission(1, MediaTypeMovie))
	var compound *SubmissionCompensationError
	if !errors.As(err, &compound) || !errors.Is(err, createErr) || !errors.Is(err, restoreErr) || compound.Review == nil {
		t.Fatalf("not auditable compound error: %#v", err)
	}
}
func TestSubmitInvalidInput(t *testing.T) {
	s := newSubmissionTestService(t, &fakeSubmissionQuota{}, nil)
	for _, in := range []RequestSubmission{{TelegramID: 1, TmdbID: 0, MediaType: MediaTypeMovie}, {TelegramID: 1, TmdbID: 1, MediaType: "music"}, {TelegramID: 1, TmdbID: 1, MediaType: MediaTypeTV, Season: -1}} {
		if _, err := s.SubmitResult(in); !errors.Is(err, ErrInvalidSubmission) {
			t.Errorf("input %#v err=%v", in, err)
		}
	}
}
func TestSubmitNotifyPanicIsolationCopy(t *testing.T) {
	q := &fakeSubmissionQuota{}
	got := make(chan *ReviewRequest, 1)
	s := newSubmissionTestService(t, q, func(r *ReviewRequest) { got <- r; r.MediaTitle = "mutated"; panic("boom") })
	in := validSubmission(1, MediaTypeMovie)
	in.MediaTitle = "original"
	result, err := s.SubmitResult(in)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("notify not called")
	}
	time.Sleep(10 * time.Millisecond)
	if q.used != 1 || result.Review.QuotaCost != 1 || result.Review.MediaTitle != "original" {
		t.Fatalf("used=%d result=%+v", q.used, result.Review)
	}
}
func TestSubmitWashSkipsBindingAndQuotaAndPersistsBaseline(t *testing.T) {
	q := &fakeSubmissionQuota{}
	r := &ReviewService{reviewsFile: t.TempDir() + "/reviews.json", reviews: make(map[string]*ReviewRequest)}
	s := &RequestSubmissionService{reviews: r, quota: q, create: r.CreateRequest}
	in := RequestSubmission{BusinessType: BusinessTypeWash, TelegramID: 99, TmdbID: 42, MediaType: MediaTypeMovie, UseQuota: false, WashBaseline: []string{"/media/old.mkv"}}
	result, err := s.SubmitResult(in)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != SubmissionCreated || result.Review == nil || result.Review.NormalizedBusinessType() != BusinessTypeWash || result.Review.MoviePilotID != 0 || result.Review.QuotaCost != 0 || q.used != 0 {
		t.Fatalf("unexpected wash result: %+v charged=%d", result, q.used)
	}
	if len(result.Review.WashBaseline) != 1 || result.Review.WashBaseline[0] != "/media/old.mkv" {
		t.Fatalf("wash baseline not persisted: %+v", result.Review.WashBaseline)
	}
}

func TestSubmitTVWashRequiresSeason(t *testing.T) {
	s := newSubmissionTestService(t, &fakeSubmissionQuota{}, nil)
	_, err := s.SubmitResult(RequestSubmission{BusinessType: BusinessTypeWash, TelegramID: 1, TmdbID: 42, MediaType: MediaTypeTV})
	if !errors.Is(err, ErrInvalidSubmission) {
		t.Fatalf("expected invalid TV wash, got %v", err)
	}
}

func TestSubmitTypedNotBoundAndQuotaExceeded(t *testing.T) {
	s := newSubmissionTestService(t, &fakeSubmissionQuota{}, nil)
	r, e := s.SubmitResult(validSubmission(0, MediaTypeMovie))
	if e != nil || r.Status != SubmissionNotBound {
		t.Fatalf("result=%+v err=%v", r, e)
	}
	q := &fakeSubmissionQuota{useErr: errors.New("movie quota exceeded")}
	s = newSubmissionTestService(t, q, nil)
	r, e = s.SubmitResult(validSubmission(1, MediaTypeMovie))
	if r.Status != SubmissionQuotaExceeded || !errors.Is(e, ErrQuotaExceeded) {
		t.Fatalf("result=%+v err=%v", r, e)
	}
}

func TestSubmitQuotaStorageFailureIsNotReportedAsExceeded(t *testing.T) {
	storageErr := errors.New("write quota file: disk full")
	s := newSubmissionTestService(t, &fakeSubmissionQuota{useErr: storageErr}, nil)
	result, err := s.SubmitResult(validSubmission(1, MediaTypeMovie))
	if result.Status != SubmissionFailed || !errors.Is(err, storageErr) || errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
