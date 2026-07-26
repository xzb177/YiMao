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
	h := NewReviewHandler(nil, telegram, nil, services.NewAdminService(t.TempDir()), reviews, nil, nil, -100123)
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
