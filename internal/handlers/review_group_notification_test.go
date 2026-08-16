package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
)

func TestOrdinaryReviewApprovalNotifiesRequesterAndConfiguredGroupOnce(t *testing.T) {
	t.Setenv("ADMIN_USER_IDS", "99")
	t.Setenv("ENABLE_RICH_MESSAGE", "true")
	const adminName = "confidential-reviewer"

	var moviePilotMu sync.Mutex
	moviePilotPosts := 0
	moviePilot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/subscribe/":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/subscribe":
			moviePilotMu.Lock()
			moviePilotPosts++
			moviePilotMu.Unlock()
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":777}}`))
		default:
			_, _ = w.Write([]byte(`{"success":true,"data":{}}`))
		}
	}))
	defer moviePilot.Close()

	type richPayload struct {
		ChatID          int64 `json:"chat_id"`
		MessageThreadID int64 `json:"message_thread_id"`
		RichMessage     struct {
			Markdown string `json:"markdown"`
		} `json:"rich_message"`
	}
	var telegramMu sync.Mutex
	var telegramPayloads []richPayload
	telegram := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sendRichMessage" {
			http.Error(w, `{"ok":false,"description":"unexpected method"}`, http.StatusBadRequest)
			return
		}
		var payload richPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"ok":false,"description":"invalid payload"}`, http.StatusBadRequest)
			return
		}
		telegramMu.Lock()
		telegramPayloads = append(telegramPayloads, payload)
		telegramMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":1,"type":"private"}}}`))
	}))
	defer telegram.Close()

	dataDir := t.TempDir()
	reviews := services.NewReviewService(dataDir, false)
	review := &services.ReviewRequest{
		RequestID:    "ordinary-group-notification",
		BusinessType: services.BusinessTypeRequest,
		TelegramID:   42,
		TelegramName: "requester-42",
		TmdbID:       550,
		MediaTitle:   "Ordinary Review Movie",
		MediaYear:    2026,
		MediaType:    services.MediaTypeMovie,
	}
	if err := reviews.CreateRequest(review); err != nil {
		t.Fatal(err)
	}

	tg := services.NewTelegramClient("test")
	tg.SetBaseURLForTest(telegram.URL, telegram.Client())
	adminService := services.NewAdminService(dataDir)
	if err := adminService.AddAdmin(99, adminName); err != nil {
		t.Fatal(err)
	}
	h := NewReviewHandler(
		nil,
		tg,
		services.NewMoviePilotClient(moviePilot.URL, "test", ""),
		adminService,
		reviews,
		nil,
		nil,
		-100123,
	)
	ctx := &callback.Context{
		UserID:          99,
		ChatID:          -100123,
		MessageThreadID: 321,
		Callback: &callback.Callback{
			Action: "review_approve",
			Params: map[string]string{"id": review.RequestID, "token": review.ApproveToken},
		},
	}

	resp, err := h.Handle(ctx)
	if err != nil || resp == nil || resp.CallbackMsg != "已批准" {
		t.Fatalf("first approval: resp=%+v err=%v", resp, err)
	}
	stored, ok := reviews.GetRequest(review.RequestID)
	if !ok || stored.Status != "approved" || stored.SubscriptionID != 777 || stored.SubscriptionState != "N" {
		t.Fatalf("subscription was not persisted: stored=%+v ok=%v", stored, ok)
	}

	telegramMu.Lock()
	firstPayloads := append([]richPayload(nil), telegramPayloads...)
	telegramMu.Unlock()
	moviePilotMu.Lock()
	firstMoviePilotPosts := moviePilotPosts
	moviePilotMu.Unlock()
	firstGroupNotifications := 0
	for _, payload := range firstPayloads {
		if payload.ChatID == -100123 {
			firstGroupNotifications++
		}
	}

	if secondResp, secondErr := h.Handle(ctx); secondErr != nil || secondResp == nil {
		t.Fatalf("duplicate approval: resp=%+v err=%v", secondResp, secondErr)
	}

	telegramMu.Lock()
	allPayloads := append([]richPayload(nil), telegramPayloads...)
	telegramMu.Unlock()
	moviePilotMu.Lock()
	allMoviePilotPosts := moviePilotPosts
	moviePilotMu.Unlock()
	allGroupNotifications := 0
	for _, payload := range allPayloads {
		if payload.ChatID == -100123 {
			allGroupNotifications++
		}
	}
	if allMoviePilotPosts != firstMoviePilotPosts {
		t.Fatalf("duplicate approval created another MoviePilot subscription: posts %d -> %d", firstMoviePilotPosts, allMoviePilotPosts)
	}
	if allGroupNotifications != firstGroupNotifications {
		t.Fatalf("duplicate approval sent another group notification: groups %d -> %d", firstGroupNotifications, allGroupNotifications)
	}

	if len(firstPayloads) != 2 {
		chatIDs := make([]int64, 0, len(firstPayloads))
		for _, payload := range firstPayloads {
			chatIDs = append(chatIDs, payload.ChatID)
		}
		t.Fatalf("first approval sent %d sendRichMessage payloads, want private + group (2); chat IDs=%v", len(firstPayloads), chatIDs)
	}
	byChat := make(map[int64]string, len(firstPayloads))
	for _, payload := range firstPayloads {
		byChat[payload.ChatID] = payload.RichMessage.Markdown
	}
	if byChat[42] == "" || byChat[-100123] == "" {
		t.Fatalf("sendRichMessage chats=%v, want requester 42 and group -100123", byChat)
	}
	for _, payload := range firstPayloads {
		if payload.ChatID == -100123 && payload.MessageThreadID != 321 {
			t.Fatalf("group message_thread_id=%d, want callback topic 321", payload.MessageThreadID)
		}
		if payload.ChatID == 42 && payload.MessageThreadID != 0 {
			t.Fatalf("private message unexpectedly inherited group topic %d", payload.MessageThreadID)
		}
	}
	groupCard := byChat[-100123]
	if !strings.Contains(groupCard, review.MediaTitle) {
		t.Fatalf("group card does not contain movie title %q: %q", review.MediaTitle, groupCard)
	}
	if strings.Contains(groupCard, "99") || strings.Contains(groupCard, adminName) || strings.Contains(groupCard, "审批人") || strings.Contains(groupCard, "批准人") {
		t.Fatalf("group card leaked administrator identity: %q", groupCard)
	}
}

func TestOrdinaryReviewFirstApprovalWithExistingSubscriptionNotifiesRequesterAndConfiguredGroupOnce(t *testing.T) {
	testOrdinaryReviewFirstApprovalWithExistingSubscriptionNotifiesRequesterAndConfiguredGroupOnce(t, 42)
}

func TestOrdinaryReviewFirstApprovalWithExistingSubscriptionNotifiesRequesterAdminAndConfiguredGroupOnce(t *testing.T) {
	testOrdinaryReviewFirstApprovalWithExistingSubscriptionNotifiesRequesterAndConfiguredGroupOnce(t, 99)
}

func testOrdinaryReviewFirstApprovalWithExistingSubscriptionNotifiesRequesterAndConfiguredGroupOnce(t *testing.T, requesterID int64) {
	t.Helper()
	t.Setenv("ADMIN_USER_IDS", "99")
	t.Setenv("ENABLE_RICH_MESSAGE", "true")

	var moviePilotMu sync.Mutex
	moviePilotPosts := 0
	moviePilot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/subscribe/":
			_, _ = w.Write([]byte(`[{"id":884,"name":"Existing Review Movie","type":"movie","state":"R","media_id":552}]`))
		case r.Method == http.MethodPost:
			moviePilotMu.Lock()
			moviePilotPosts++
			moviePilotMu.Unlock()
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":999}}`))
		default:
			http.Error(w, `{"success":false,"message":"unexpected request"}`, http.StatusBadRequest)
		}
	}))
	defer moviePilot.Close()

	type richPayload struct {
		ChatID          int64 `json:"chat_id"`
		MessageThreadID int64 `json:"message_thread_id"`
		RichMessage     struct {
			Markdown string `json:"markdown"`
		} `json:"rich_message"`
	}
	var telegramMu sync.Mutex
	var telegramPayloads []richPayload
	telegram := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sendRichMessage" {
			http.Error(w, `{"ok":false,"description":"unexpected method"}`, http.StatusBadRequest)
			return
		}
		var payload richPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"ok":false,"description":"invalid payload"}`, http.StatusBadRequest)
			return
		}
		telegramMu.Lock()
		telegramPayloads = append(telegramPayloads, payload)
		telegramMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":1,"type":"private"}}}`))
	}))
	defer telegram.Close()

	dataDir := t.TempDir()
	reviews := services.NewReviewService(dataDir, false)
	review := &services.ReviewRequest{
		RequestID:    "ordinary-existing-subscription-group-notification",
		BusinessType: services.BusinessTypeRequest,
		TelegramID:   requesterID,
		TelegramName: "requester",
		TmdbID:       552,
		MediaTitle:   "Existing Review Movie",
		MediaYear:    2026,
		MediaType:    services.MediaTypeMovie,
	}
	if err := reviews.CreateRequest(review); err != nil {
		t.Fatal(err)
	}

	tg := services.NewTelegramClient("test")
	tg.SetBaseURLForTest(telegram.URL, telegram.Client())
	h := NewReviewHandler(
		nil,
		tg,
		services.NewMoviePilotClient(moviePilot.URL, "test", ""),
		services.NewAdminService(dataDir),
		reviews,
		nil,
		nil,
		-100123,
	)
	ctx := &callback.Context{
		UserID:          99,
		ChatID:          -100123,
		ChatType:        "supergroup",
		MessageThreadID: 321,
		Callback: &callback.Callback{
			Action: "review_approve",
			Params: map[string]string{"id": review.RequestID, "token": review.ApproveToken},
		},
	}

	resp, err := h.Handle(ctx)
	if err != nil || resp == nil || resp.CallbackMsg != "已有订阅" {
		t.Fatalf("first approval: resp=%+v err=%v", resp, err)
	}
	stored, ok := reviews.GetRequest(review.RequestID)
	if !ok || stored.Status != "approved" || stored.SubscriptionID != 884 || stored.SubscriptionState != "R" {
		t.Fatalf("existing subscription was not persisted: stored=%+v ok=%v", stored, ok)
	}

	moviePilotMu.Lock()
	firstMoviePilotPosts := moviePilotPosts
	moviePilotMu.Unlock()
	if firstMoviePilotPosts != 0 {
		t.Fatalf("first approval issued %d MoviePilot POSTs, want 0", firstMoviePilotPosts)
	}
	telegramMu.Lock()
	firstPayloads := append([]richPayload(nil), telegramPayloads...)
	telegramMu.Unlock()

	secondResp, secondErr := h.Handle(ctx)
	if secondErr != nil || secondResp == nil || secondResp.CallbackMsg != "已被批准" {
		t.Fatalf("repeated approval: resp=%+v err=%v", secondResp, secondErr)
	}
	moviePilotMu.Lock()
	allMoviePilotPosts := moviePilotPosts
	moviePilotMu.Unlock()
	if allMoviePilotPosts != firstMoviePilotPosts {
		t.Fatalf("repeated approval created a MoviePilot side effect: posts %d -> %d", firstMoviePilotPosts, allMoviePilotPosts)
	}
	telegramMu.Lock()
	allPayloads := append([]richPayload(nil), telegramPayloads...)
	telegramMu.Unlock()
	if len(allPayloads) != len(firstPayloads) {
		t.Fatalf("repeated approval sent additional notifications: payloads %d -> %d", len(firstPayloads), len(allPayloads))
	}

	requesterMessages := 0
	groupMessages := 0
	for _, payload := range firstPayloads {
		if payload.RichMessage.Markdown == "" {
			t.Fatalf("chat %d received an empty Rich Message", payload.ChatID)
		}
		switch payload.ChatID {
		case requesterID:
			requesterMessages++
			if payload.MessageThreadID != 0 {
				t.Fatalf("requester private Rich Message inherited message_thread_id=%d", payload.MessageThreadID)
			}
		case -100123:
			groupMessages++
			if payload.MessageThreadID != 321 {
				t.Fatalf("configured-group Rich Message message_thread_id=%d, want 321", payload.MessageThreadID)
			}
		default:
			t.Fatalf("unexpected Rich Message chat_id=%d", payload.ChatID)
		}
	}
	if requesterMessages != 1 {
		t.Fatalf("first approval requester private Rich Message count=%d, want exactly 1", requesterMessages)
	}
	if groupMessages != 1 {
		t.Fatalf("first approval configured-group Rich Message count=%d, want exactly 1 (missing group notification)", groupMessages)
	}
}

func TestOrdinaryReviewApprovalSurfacesGroupNotificationFailureWithoutRetryingSubscription(t *testing.T) {
	t.Setenv("ADMIN_USER_IDS", "99")
	t.Setenv("ENABLE_RICH_MESSAGE", "true")

	var moviePilotMu sync.Mutex
	moviePilotPosts := 0
	moviePilot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/subscribe/":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/subscribe":
			moviePilotMu.Lock()
			moviePilotPosts++
			moviePilotMu.Unlock()
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":778}}`))
		default:
			_, _ = w.Write([]byte(`{"success":true,"data":{}}`))
		}
	}))
	defer moviePilot.Close()

	type richPayload struct {
		ChatID          int64 `json:"chat_id"`
		MessageThreadID int64 `json:"message_thread_id"`
	}
	var telegramMu sync.Mutex
	var telegramPayloads []richPayload
	telegram := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload richPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"ok":false,"description":"invalid payload"}`, http.StatusBadRequest)
			return
		}
		telegramMu.Lock()
		telegramPayloads = append(telegramPayloads, payload)
		telegramMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if payload.ChatID == -100123 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"ok":false,"description":"group unavailable"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":42,"type":"private"}}}`))
	}))
	defer telegram.Close()

	dataDir := t.TempDir()
	reviews := services.NewReviewService(dataDir, false)
	review := &services.ReviewRequest{
		RequestID:    "ordinary-group-notification-failure",
		BusinessType: services.BusinessTypeRequest,
		TelegramID:   42,
		TelegramName: "requester-42",
		TmdbID:       551,
		MediaTitle:   "Group Failure Movie",
		MediaYear:    2026,
		MediaType:    services.MediaTypeMovie,
	}
	if err := reviews.CreateRequest(review); err != nil {
		t.Fatal(err)
	}

	tg := services.NewTelegramClient("test")
	tg.SetBaseURLForTest(telegram.URL, telegram.Client())
	h := NewReviewHandler(
		nil,
		tg,
		services.NewMoviePilotClient(moviePilot.URL, "test", ""),
		services.NewAdminService(dataDir),
		reviews,
		nil,
		nil,
		-100123,
	)
	ctx := &callback.Context{
		UserID:          99,
		ChatID:          -100123,
		MessageThreadID: 654,
		Callback: &callback.Callback{
			Action: "review_approve",
			Params: map[string]string{"id": review.RequestID, "token": review.ApproveToken},
		},
	}

	resp, err := h.Handle(ctx)
	if err != nil {
		t.Fatalf("post-commit group failure returned retryable handler error: %v", err)
	}
	if resp == nil || resp.CallbackMsg != "群通知失败" || !resp.ShowAlert || !resp.Edit {
		t.Fatalf("group failure response=%+v, want explicit non-retryable warning", resp)
	}
	if !strings.Contains(resp.Text, "审核与订阅已成功") || !strings.Contains(resp.Text, "不要重复审批") {
		t.Fatalf("group failure response did not explain committed approval: %q", resp.Text)
	}
	stored, ok := reviews.GetRequest(review.RequestID)
	if !ok || stored.Status != "approved" || stored.SubscriptionID != 778 || stored.SubscriptionState != "N" {
		t.Fatalf("group failure rolled back persisted approval: stored=%+v ok=%v", stored, ok)
	}

	secondResp, secondErr := h.Handle(ctx)
	if secondErr != nil || secondResp == nil || secondResp.CallbackMsg != "已被批准" {
		t.Fatalf("duplicate approval after group failure: resp=%+v err=%v", secondResp, secondErr)
	}
	moviePilotMu.Lock()
	posts := moviePilotPosts
	moviePilotMu.Unlock()
	if posts != 1 {
		t.Fatalf("MoviePilot subscription posts=%d, want exactly 1", posts)
	}
	telegramMu.Lock()
	payloads := append([]richPayload(nil), telegramPayloads...)
	telegramMu.Unlock()
	groupAttempts := 0
	for _, payload := range payloads {
		if payload.ChatID == -100123 {
			groupAttempts++
			if payload.MessageThreadID != 654 {
				t.Fatalf("failed group attempt message_thread_id=%d, want 654", payload.MessageThreadID)
			}
		}
	}
	if groupAttempts != 1 {
		t.Fatalf("group notification attempts=%d, want exactly 1 after duplicate approval", groupAttempts)
	}
}
