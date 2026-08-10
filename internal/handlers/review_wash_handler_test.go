package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
)

func TestWashApprovalNotifiesRequesterAndConfiguredGroupWithoutPrivacyLeak(t *testing.T) {
	t.Setenv("ADMIN_USER_IDS", "99")
	var payloads []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		payloads = append(payloads, payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":1,"type":"private"}}}`))
	}))
	defer server.Close()
	telegram := services.NewTelegramClient("test")
	telegram.SetBaseURLForTest(server.URL, server.Client())
	reviews := services.NewReviewService(t.TempDir(), false)
	r := &services.ReviewRequest{RequestID: "wash-notify", BusinessType: services.BusinessTypeWash, TelegramID: 42, TelegramName: "private-user", TmdbID: 550, MediaTitle: "Fight Club", MediaType: services.MediaTypeMovie, WashBaseline: []string{"old"}}
	if err := reviews.CreateRequest(r); err != nil {
		t.Fatal(err)
	}
	var washPayload map[string]any
	moviepilot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/subscribe":
			_ = json.NewDecoder(r.Body).Decode(&washPayload)
			_, _ = w.Write([]byte(`{"success":true,"message":"新增订阅成功","data":{"id":77}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/subscribe/77":
			_, _ = w.Write([]byte(`{"id":77,"tmdbid":550,"type":"电影","best_version":1}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/subscribe/search/77":
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer moviepilot.Close()
	h := NewReviewHandler(nil, telegram, services.NewMoviePilotClient(moviepilot.URL, "", ""), services.NewAdminService(t.TempDir()), reviews, nil, nil, -100123)
	resp, err := h.Handle(&callback.Context{UserID: 99, Callback: &callback.Callback{Action: "review_approve", Params: map[string]string{"id": r.RequestID, "token": r.ApproveToken}}})
	if err != nil || resp == nil {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	if len(payloads) != 2 {
		t.Fatalf("sent %d notifications, want private + group", len(payloads))
	}
	chats := map[int64]string{}
	for _, payload := range payloads {
		chatID := int64(payload["chat_id"].(float64))
		rich := payload["rich_message"].(map[string]any)
		chats[chatID], _ = rich["markdown"].(string)
	}
	if chats[42] == "" || chats[-100123] == "" {
		t.Fatalf("chats=%v", chats)
	}
	if strings.Contains(chats[-100123], "private-user") || strings.Contains(chats[-100123], "申请人") {
		t.Fatalf("group leaked private data: %q", chats[-100123])
	}
	if washPayload["best_version"] != float64(1) {
		t.Fatalf("wash payload=%v", washPayload)
	}
	stored, _ := reviews.GetRequest(r.RequestID)
	if stored.Status != "approved" || stored.SubscriptionID != 77 || stored.Stuck {
		t.Fatalf("stored=%+v", stored)
	}
}

func TestWashApprovalMoviePilotFailureRemainsApprovedAndStuck(t *testing.T) {
	t.Setenv("ADMIN_USER_IDS", "99")
	moviepilot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer moviepilot.Close()
	reviews := services.NewReviewService(t.TempDir(), false)
	r := &services.ReviewRequest{RequestID: "wash-failure", BusinessType: services.BusinessTypeWash, TelegramID: 42, TmdbID: 550, MediaTitle: "Fight Club", MediaType: services.MediaTypeMovie, WashBaseline: []string{"old"}}
	if err := reviews.CreateRequest(r); err != nil {
		t.Fatal(err)
	}
	h := NewReviewHandler(nil, nil, services.NewMoviePilotClient(moviepilot.URL, "", ""), services.NewAdminService(t.TempDir()), reviews, nil, nil, 0)
	resp, err := h.Handle(&callback.Context{UserID: 99, Callback: &callback.Callback{Action: "review_approve", Params: map[string]string{"id": r.RequestID, "token": r.ApproveToken}}})
	if err != nil || resp == nil || !strings.Contains(resp.Text, "派发失败") {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
	stored, _ := reviews.GetRequest(r.RequestID)
	if stored.Status != "approved" || !stored.Stuck || stored.LastError == "" || stored.SubscriptionID != 0 {
		t.Fatalf("stored=%+v", stored)
	}
}

func TestCompleteLegacyWashFailsClosedWithRecoveryPath(t *testing.T) {
	t.Setenv("ADMIN_USER_IDS", "99")
	reviews := services.NewReviewService(t.TempDir(), false)
	r := &services.ReviewRequest{RequestID: "legacy-wash", BusinessType: services.BusinessTypeWash, TelegramID: 42, MediaTitle: "Legacy"}
	if err := reviews.CreateRequest(r); err != nil {
		t.Fatal(err)
	}
	if _, err := reviews.Approve(r.RequestID, 99, r.ApproveToken); err != nil {
		t.Fatal(err)
	}
	h := NewReviewHandler(nil, nil, nil, services.NewAdminService(t.TempDir()), reviews, nil, nil, 0)
	resp, err := h.Handle(&callback.Context{UserID: 99, Callback: &callback.Callback{Action: "review_complete_wash", Params: map[string]string{"id": r.RequestID}}})
	if err != nil || resp == nil {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	if !strings.Contains(resp.Text, "缺少创建时") || !strings.Contains(resp.Text, "重新创建") {
		t.Fatalf("missing recovery path: %q", resp.Text)
	}
	if got, _ := reviews.GetRequest(r.RequestID); got.Status != "approved" {
		t.Fatalf("status=%q, want approved", got.Status)
	}
}
