package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
)

type approvalRichPayload struct {
	ChatID          int64 `json:"chat_id"`
	MessageThreadID int64 `json:"message_thread_id"`
	RichMessage     struct {
		Markdown string `json:"markdown"`
	} `json:"rich_message"`
}

type approvalHarness struct {
	handler         *ReviewHandler
	reviews         *services.ReviewService
	moviePilotPosts atomic.Int32
	payloadMu       sync.Mutex
	payloads        []approvalRichPayload
}

type approvalCallbackBarrier struct {
	ready   sync.WaitGroup
	release chan struct{}
	once    sync.Once
}

func newApprovalCallbackBarrier(parties int) *approvalCallbackBarrier {
	b := &approvalCallbackBarrier{release: make(chan struct{})}
	b.ready.Add(parties)
	return b
}

func (b *approvalCallbackBarrier) wait() {
	b.ready.Done()
	<-b.release
}

func (b *approvalCallbackBarrier) releaseAll() {
	b.ready.Wait()
	b.once.Do(func() { close(b.release) })
}

func (b *approvalCallbackBarrier) abort() {
	b.once.Do(func() { close(b.release) })
}

func newApprovalHarness(
	t *testing.T,
	adminID int64,
	groupChatID int64,
	onMoviePilotPost func(),
	telegramStatus func(approvalRichPayload) int,
) *approvalHarness {
	t.Helper()
	t.Setenv("ADMIN_USER_IDS", fmt.Sprintf("%d", adminID))
	t.Setenv("ENABLE_RICH_MESSAGE", "true")

	harness := &approvalHarness{}
	moviePilot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/subscribe/":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/subscribe":
			harness.moviePilotPosts.Add(1)
			if onMoviePilotPost != nil {
				onMoviePilotPost()
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":777}}`))
		default:
			http.Error(w, `{"success":false,"message":"unexpected request"}`, http.StatusBadRequest)
		}
	}))
	t.Cleanup(moviePilot.Close)

	telegram := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sendRichMessage" {
			http.Error(w, `{"ok":false,"description":"unexpected method"}`, http.StatusBadRequest)
			return
		}
		var payload approvalRichPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"ok":false,"description":"invalid payload"}`, http.StatusBadRequest)
			return
		}
		harness.payloadMu.Lock()
		harness.payloads = append(harness.payloads, payload)
		harness.payloadMu.Unlock()
		status := http.StatusOK
		if telegramStatus != nil {
			status = telegramStatus(payload)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status >= http.StatusBadRequest {
			_, _ = w.Write([]byte(`{"ok":false,"description":"forced notification failure"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":1,"type":"private"}}}`))
	}))
	t.Cleanup(telegram.Close)

	dataDir := t.TempDir()
	harness.reviews = services.NewReviewService(dataDir, false)
	tg := services.NewTelegramClient("test")
	tg.SetBaseURLForTest(telegram.URL, telegram.Client())
	harness.handler = NewReviewHandler(
		nil,
		tg,
		services.NewMoviePilotClient(moviePilot.URL, "test", ""),
		services.NewAdminService(dataDir),
		harness.reviews,
		nil,
		nil,
		groupChatID,
	)
	return harness
}

func (h *approvalHarness) addReview(t *testing.T, requestID string, telegramID int64, tmdbID int) *services.ReviewRequest {
	t.Helper()
	review := &services.ReviewRequest{
		RequestID:    requestID,
		BusinessType: services.BusinessTypeRequest,
		TelegramID:   telegramID,
		TelegramName: fmt.Sprintf("requester-%d", telegramID),
		TmdbID:       tmdbID,
		MediaTitle:   requestID,
		MediaYear:    2026,
		MediaType:    services.MediaTypeMovie,
	}
	if err := h.reviews.CreateRequest(review); err != nil {
		t.Fatal(err)
	}
	return review
}

func (h *approvalHarness) context(review *services.ReviewRequest, adminID, chatID, threadID int64) *callback.Context {
	return &callback.Context{
		UserID:          adminID,
		ChatID:          chatID,
		ChatType:        "supergroup",
		MessageThreadID: threadID,
		Callback: &callback.Callback{
			Action: "review_approve",
			Params: map[string]string{"id": review.RequestID, "token": review.ApproveToken},
		},
	}
}

func (h *approvalHarness) shortContext(review *services.ReviewRequest, adminID, chatID, threadID int64) *callback.Context {
	return &callback.Context{
		UserID:          adminID,
		ChatID:          chatID,
		ChatType:        "supergroup",
		MessageThreadID: threadID,
		Callback: &callback.Callback{
			Action: "rv_a",
			Raw:    "rv_a:" + review.ApproveToken,
		},
	}
}

func (h *approvalHarness) snapshotPayloads() []approvalRichPayload {
	h.payloadMu.Lock()
	defer h.payloadMu.Unlock()
	return append([]approvalRichPayload(nil), h.payloads...)
}

func countApprovalPayloads(payloads []approvalRichPayload, chatID int64) int {
	count := 0
	for _, payload := range payloads {
		if payload.ChatID == chatID {
			count++
		}
	}
	return count
}

type approvalLockTestAccess interface {
	lockApprovalRequest(requestID string) func()
	approvalLockCount() int
	approvalLockReferences(requestID string) int
}

func requireApprovalLockAccess(t *testing.T, handler *ReviewHandler) approvalLockTestAccess {
	t.Helper()
	access, ok := any(handler).(approvalLockTestAccess)
	if !ok {
		t.Fatal("ReviewHandler does not provide request-keyed approval locking")
	}
	return access
}

func waitForApprovalLockReferences(t *testing.T, access approvalLockTestAccess, requestID string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if access.approvalLockReferences(requestID) == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("approval lock references for %q = %d, want %d", requestID, access.approvalLockReferences(requestID), want)
}

func TestConcurrentApproveCallbacksAreLinearizedPerRequest(t *testing.T) {
	const (
		adminID = int64(99)
		userID  = int64(42)
		groupID = int64(-100123)
	)
	harness := newApprovalHarness(t, adminID, groupID, nil, nil)
	review := harness.addReview(t, "concurrent-same-request", userID, 550)

	barrier := newApprovalCallbackBarrier(2)
	type result struct {
		response *callback.Response
		err      error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		ctx := harness.shortContext(review, adminID, groupID, 321)
		go func() {
			barrier.wait()
			resp, err := harness.handler.Handle(ctx)
			results <- result{response: resp, err: err}
		}()
	}
	barrier.releaseAll()
	for i := 0; i < 2; i++ {
		got := <-results
		if got.err != nil || got.response == nil {
			t.Fatalf("concurrent approval %d: response=%+v err=%v", i, got.response, got.err)
		}
	}

	payloads := harness.snapshotPayloads()
	if got := harness.moviePilotPosts.Load(); got != 1 {
		t.Fatalf("MoviePilot POST count=%d, want exactly 1", got)
	}
	if got := countApprovalPayloads(payloads, userID); got != 1 {
		t.Fatalf("requester Rich Message count=%d, want exactly 1", got)
	}
	if got := countApprovalPayloads(payloads, groupID); got != 1 {
		t.Fatalf("group broadcast count=%d, want exactly 1", got)
	}
	access := requireApprovalLockAccess(t, harness.handler)
	if got := access.approvalLockCount(); got != 0 {
		t.Fatalf("approval lock table retained %d entries after callbacks", got)
	}
}

func TestApprovalLocksDoNotSerializeDifferentRequestIDs(t *testing.T) {
	const (
		adminID = int64(99)
		groupID = int64(-100123)
	)
	postBarrier := newApprovalCallbackBarrier(2)
	harness := newApprovalHarness(t, adminID, groupID, postBarrier.wait, nil)
	first := harness.addReview(t, "concurrent-request-a", 41, 551)
	second := harness.addReview(t, "concurrent-request-b", 42, 552)
	callbackBarrier := newApprovalCallbackBarrier(2)

	errs := make(chan error, 2)
	for _, review := range []*services.ReviewRequest{first, second} {
		ctx := harness.context(review, adminID, groupID, 321)
		go func() {
			callbackBarrier.wait()
			resp, err := harness.handler.Handle(ctx)
			if err == nil && (resp == nil || resp.CallbackMsg != "已批准") {
				err = fmt.Errorf("unexpected response: %+v", resp)
			}
			errs <- err
		}()
	}
	callbackBarrier.releaseAll()

	postBarrierDone := make(chan struct{})
	go func() {
		postBarrier.releaseAll()
		close(postBarrierDone)
	}()
	select {
	case <-postBarrierDone:
	case <-time.After(2 * time.Second):
		postBarrier.abort()
		t.Fatal("different request IDs did not reach MoviePilot concurrently; approval path appears globally locked")
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("approval %d: %v", i, err)
		}
	}
	if got := harness.moviePilotPosts.Load(); got != 2 {
		t.Fatalf("MoviePilot POST count=%d, want 2 independent requests", got)
	}
	access := requireApprovalLockAccess(t, harness.handler)
	if got := access.approvalLockCount(); got != 0 {
		t.Fatalf("approval lock table retained %d entries after independent callbacks", got)
	}
}

func TestApprovalLockTableKeepsWaitersOnOneMutexAndReleasesSafely(t *testing.T) {
	access := requireApprovalLockAccess(t, &ReviewHandler{})

	releaseHolder := access.lockApprovalRequest("same-request")
	waiterAcquired := make(chan struct{})
	releaseWaiter := make(chan struct{})
	waiterDone := make(chan struct{})
	go func() {
		release := access.lockApprovalRequest("same-request")
		close(waiterAcquired)
		<-releaseWaiter
		release()
		close(waiterDone)
	}()
	waitForApprovalLockReferences(t, access, "same-request", 2)
	releaseHolder()
	select {
	case <-waiterAcquired:
	case <-time.After(2 * time.Second):
		t.Fatal("registered waiter never acquired request lock")
	}

	contenderAcquired := make(chan func(), 1)
	go func() {
		contenderAcquired <- access.lockApprovalRequest("same-request")
	}()
	waitForApprovalLockReferences(t, access, "same-request", 2)
	select {
	case release := <-contenderAcquired:
		release()
		t.Fatal("a new mutex was created while the registered waiter held the request lock")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseWaiter)
	select {
	case <-waiterDone:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not release request lock")
	}
	var releaseContender func()
	select {
	case releaseContender = <-contenderAcquired:
	case <-time.After(2 * time.Second):
		t.Fatal("contender did not acquire request lock after waiter released it")
	}
	releaseContender()
	if got := access.approvalLockCount(); got != 0 {
		t.Fatalf("approval lock table retained %d entries, want 0", got)
	}

	releaseFirstRequest := access.lockApprovalRequest("request-a")
	secondRequestAcquired := make(chan func(), 1)
	go func() {
		secondRequestAcquired <- access.lockApprovalRequest("request-b")
	}()
	select {
	case release := <-secondRequestAcquired:
		release()
	case <-time.After(2 * time.Second):
		releaseFirstRequest()
		t.Fatal("request-b was blocked by a lock held for request-a")
	}
	releaseFirstRequest()
	if got := access.approvalLockCount(); got != 0 {
		t.Fatalf("approval lock table retained %d entries after different keys", got)
	}
}

func TestRequesterAdminApprovingInConfiguredTopicStillGetsPrivateNotification(t *testing.T) {
	const (
		requesterAdminID = int64(42)
		groupID          = int64(-100123)
		threadID         = int64(654)
	)
	harness := newApprovalHarness(t, requesterAdminID, groupID, nil, nil)
	review := harness.addReview(t, "requester-is-admin", requesterAdminID, 553)
	resp, err := harness.handler.Handle(harness.context(review, requesterAdminID, groupID, threadID))
	if err != nil || resp == nil || resp.CallbackMsg != "已批准" {
		t.Fatalf("approval: response=%+v err=%v", resp, err)
	}

	payloads := harness.snapshotPayloads()
	if got := countApprovalPayloads(payloads, requesterAdminID); got != 1 {
		t.Fatalf("requester private Rich Message count=%d, want exactly 1", got)
	}
	if got := countApprovalPayloads(payloads, groupID); got != 1 {
		t.Fatalf("group topic broadcast count=%d, want exactly 1", got)
	}
	for _, payload := range payloads {
		switch payload.ChatID {
		case requesterAdminID:
			if payload.MessageThreadID != 0 {
				t.Fatalf("private notification inherited message_thread_id=%d", payload.MessageThreadID)
			}
		case groupID:
			if payload.MessageThreadID != threadID {
				t.Fatalf("group message_thread_id=%d, want %d", payload.MessageThreadID, threadID)
			}
		}
	}
}

func TestRequesterPrivateFailureIsVisibleAndDoesNotSuppressGroupOrRetry(t *testing.T) {
	const (
		adminID = int64(99)
		userID  = int64(42)
		groupID = int64(-100123)
	)
	harness := newApprovalHarness(t, adminID, groupID, nil, func(payload approvalRichPayload) int {
		if payload.ChatID == userID {
			return http.StatusBadGateway
		}
		return http.StatusOK
	})
	review := harness.addReview(t, "private-notification-failure", userID, 554)
	ctx := harness.context(review, adminID, groupID, 777)
	resp, err := harness.handler.Handle(ctx)
	if err != nil {
		t.Fatalf("committed approval returned retryable error: %v", err)
	}
	if resp == nil || !resp.ShowAlert || !strings.Contains(resp.Text, "私聊通知失败") || !strings.Contains(resp.Text, "不要重复审批") {
		t.Fatalf("private failure response=%+v, want explicit committed partial-success warning", resp)
	}
	if strings.Contains(resp.Text, "群通知失败") {
		t.Fatalf("response reported successful group notification as failed: %q", resp.Text)
	}
	stored, ok := harness.reviews.GetRequest(review.RequestID)
	if !ok || stored.Status != "approved" || stored.SubscriptionID != 777 || stored.SubscriptionState != "N" {
		t.Fatalf("private failure changed committed approval: stored=%+v ok=%v", stored, ok)
	}
	if got := countApprovalPayloads(harness.snapshotPayloads(), groupID); got != 1 {
		t.Fatalf("group notification attempts=%d, want 1 despite private failure", got)
	}

	beforeDuplicate := len(harness.snapshotPayloads())
	duplicateResp, duplicateErr := harness.handler.Handle(ctx)
	if duplicateErr != nil || duplicateResp == nil || duplicateResp.CallbackMsg != "已被批准" {
		t.Fatalf("duplicate callback: response=%+v err=%v", duplicateResp, duplicateErr)
	}
	if after := len(harness.snapshotPayloads()); after != beforeDuplicate {
		t.Fatalf("duplicate callback retried notifications: attempts %d -> %d", beforeDuplicate, after)
	}
	if got := harness.moviePilotPosts.Load(); got != 1 {
		t.Fatalf("duplicate callback created subscription: MoviePilot POST count=%d", got)
	}
}

func TestBothNotificationFailuresAreVisibleTogetherAndNeverRetried(t *testing.T) {
	const (
		adminID = int64(99)
		userID  = int64(42)
		groupID = int64(-100123)
	)
	harness := newApprovalHarness(t, adminID, groupID, nil, func(approvalRichPayload) int {
		return http.StatusBadGateway
	})
	review := harness.addReview(t, "both-notifications-fail", userID, 555)
	ctx := harness.context(review, adminID, groupID, 888)
	resp, err := harness.handler.Handle(ctx)
	if err != nil {
		t.Fatalf("committed approval returned retryable error: %v", err)
	}
	if resp == nil || !resp.ShowAlert || !strings.Contains(resp.Text, "私聊通知失败") || !strings.Contains(resp.Text, "群通知失败") || !strings.Contains(resp.Text, "不要重复审批") {
		t.Fatalf("combined notification failure response=%+v", resp)
	}
	stored, ok := harness.reviews.GetRequest(review.RequestID)
	if !ok || stored.Status != "approved" || stored.SubscriptionID != 777 || stored.SubscriptionState != "N" {
		t.Fatalf("combined failure changed committed approval: stored=%+v ok=%v", stored, ok)
	}
	payloads := harness.snapshotPayloads()
	if got := countApprovalPayloads(payloads, userID); got != 1 {
		t.Fatalf("private notification attempts=%d, want exactly 1", got)
	}
	if got := countApprovalPayloads(payloads, groupID); got != 1 {
		t.Fatalf("group notification attempts=%d, want exactly 1", got)
	}

	if duplicateResp, duplicateErr := harness.handler.Handle(ctx); duplicateErr != nil || duplicateResp == nil {
		t.Fatalf("duplicate callback: response=%+v err=%v", duplicateResp, duplicateErr)
	}
	if after := len(harness.snapshotPayloads()); after != len(payloads) {
		t.Fatalf("duplicate callback retried failed notifications: attempts %d -> %d", len(payloads), after)
	}
	if got := harness.moviePilotPosts.Load(); got != 1 {
		t.Fatalf("duplicate callback created subscription: MoviePilot POST count=%d", got)
	}
}
