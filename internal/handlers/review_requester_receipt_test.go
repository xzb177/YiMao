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

type receiptEditPayload struct {
	ChatID    int64  `json:"chat_id"`
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
}

// TestApprovalUpdatesRequesterReceiptCardInPlace pins the user-visible outcome
// of an approval: the requester submission receipt must stop claiming the
// request is waiting for review, and a repeated admin click must not edit
// anything at all.
func TestApprovalUpdatesRequesterReceiptCardInPlace(t *testing.T) {
	t.Setenv("ADMIN_USER_IDS", "99")
	t.Setenv("ENABLE_RICH_MESSAGE", "true")

	moviePilot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/subscribe/":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/subscribe":
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":22981}}`))
		default:
			http.Error(w, `{"success":false}`, http.StatusBadRequest)
		}
	}))
	defer moviePilot.Close()

	var mu sync.Mutex
	var edits []receiptEditPayload
	telegram := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/editMessageText" {
			var payload receiptEditPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"ok":false}`, http.StatusBadRequest)
				return
			}
			mu.Lock()
			edits = append(edits, payload)
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":4321,"chat":{"id":42,"type":"private"}}}`))
	}))
	defer telegram.Close()

	dataDir := t.TempDir()
	reviews := services.NewReviewService(dataDir, false)
	review := &services.ReviewRequest{
		RequestID:    "receipt-update",
		BusinessType: services.BusinessTypeRequest,
		TelegramID:   42,
		TelegramName: "requester",
		TmdbID:       552,
		MediaTitle:   "魔女之旅",
		MediaYear:    2026,
		MediaType:    services.MediaTypeMovie,
	}
	if err := reviews.CreateRequest(review); err != nil {
		t.Fatal(err)
	}
	if err := reviews.SetRequesterReceipt(review.RequestID, 42, 4321); err != nil {
		t.Fatalf("record requester receipt: %v", err)
	}

	tg := services.NewTelegramClient("test")
	tg.SetBaseURLForTest(telegram.URL, telegram.Client())
	h := NewReviewHandler(nil, tg, services.NewMoviePilotClient(moviePilot.URL, "test", ""), services.NewAdminService(dataDir), reviews, nil, nil, 0)
	ctx := &callback.Context{
		UserID:    99,
		ChatID:    99,
		ChatType:  "private",
		MessageID: 777,
		Callback: &callback.Callback{
			Action: "rv_a",
			Raw:    "rv_a:" + review.ApproveToken,
		},
	}
	resp, err := h.Handle(ctx)
	if err != nil || resp == nil {
		t.Fatalf("approval failed: resp=%+v err=%v", resp, err)
	}

	mu.Lock()
	first := append([]receiptEditPayload(nil), edits...)
	mu.Unlock()
	var receiptEdit *receiptEditPayload
	for i := range first {
		if first[i].ChatID == 42 && first[i].MessageID == 4321 {
			receiptEdit = &first[i]
		}
	}
	if receiptEdit == nil {
		t.Fatalf("requester receipt card was never edited: edits=%+v", first)
	}
	if strings.Contains(receiptEdit.Text, "等待管理员审核") || strings.Contains(receiptEdit.Text, "求片已提交") {
		t.Fatalf("requester receipt still shows the pending card: %q", receiptEdit.Text)
	}
	if !strings.Contains(receiptEdit.Text, "审核已通过") || !strings.Contains(receiptEdit.Text, review.MediaTitle) {
		t.Fatalf("requester receipt lacks the approved outcome: %q", receiptEdit.Text)
	}

	secondResp, secondErr := h.Handle(ctx)
	if secondErr != nil || secondResp == nil {
		t.Fatalf("repeat click failed: resp=%+v err=%v", secondResp, secondErr)
	}
	if secondResp.Edit || secondResp.Text != "" {
		t.Fatalf("repeat click overwrote a message: %+v", secondResp)
	}
	if secondResp.CallbackMsg == "" || strings.Contains(secondResp.CallbackMsg, "已被已批准") {
		t.Fatalf("repeat click notice is missing or malformed: %q", secondResp.CallbackMsg)
	}
	mu.Lock()
	after := len(edits)
	mu.Unlock()
	if after != len(first) {
		t.Fatalf("repeat click issued extra edits: %d -> %d", len(first), after)
	}
}

func TestUpdateRequesterReceiptEditsEphemeralCard(t *testing.T) {

	var path string

	var payload map[string]any

	telegram := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		path = r.URL.Path

		_ = json.NewDecoder(r.Body).Decode(&payload)

		w.Header().Set("Content-Type", "application/json")

		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))

	}))

	defer telegram.Close()

	tg := services.NewTelegramClient("test")

	tg.SetBaseURLForTest(telegram.URL, telegram.Client())

	h := NewReviewHandler(nil, tg, nil, nil, nil, nil, nil, 0)

	review := &services.ReviewRequest{RequestID: "eph", TelegramID: 42, MediaTitle: "Test", RequesterChatID: -1001, RequesterReceiptEphemeralID: 77}

	h.updateRequesterReceipt(review, "approved headline", "status", "footer")

	if path != "/editEphemeralMessageText" {

		t.Fatalf("path=%s payload=%v", path, payload)

	}

	if payload["chat_id"] != float64(-1001) || payload["receiver_user_id"] != float64(42) || payload["ephemeral_message_id"] != float64(77) {

		t.Fatalf("payload=%v", payload)

	}

	text, _ := payload["text"].(string)

	if !strings.Contains(text, "approved headline") || strings.Contains(text, "waiting") {

		t.Fatalf("text=%q", text)

	}

}

func TestDisabledReviewResultKeyboard(t *testing.T) {

	kb := disabledReviewResultKeyboard(true)

	if kb == nil || !kb.InlineKeyboard[0][0].Disabled || kb.InlineKeyboard[0][0].CallbackData != "" {

		t.Fatalf("keyboard=%#v", kb)

	}

}
