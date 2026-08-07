package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
)

func TestDuplicateSubscriptionFeedbackUsesOneVisibleChannelInRequesterDM(t *testing.T) {
	resp, sendSeparate := duplicateSubscriptionFeedback(&callback.Context{ChatID: 42}, 42)
	if sendSeparate {
		t.Fatal("requester DM must not receive a second rich message")
	}
	if resp.ShowAlert {
		t.Fatal("duplicate subscription feedback must not open a blocking alert")
	}
	if !resp.Edit || resp.CallbackMsg != "已有订阅" {
		t.Fatalf("response=%+v", resp)
	}
}

func TestDuplicateSubscriptionFeedbackKeepsRequesterNotificationForOtherChat(t *testing.T) {
	resp, sendSeparate := duplicateSubscriptionFeedback(&callback.Context{UserID: 99, ChatID: 99}, 42)
	if !sendSeparate {
		t.Fatal("approval from another user must still notify the requester")
	}
	if resp.ShowAlert || !resp.Edit {
		t.Fatalf("response=%+v", resp)
	}
}

func TestDuplicateSubscriptionFeedbackUsesOneVisibleChannelWhenRequesterApprovesInGroup(t *testing.T) {
	resp, sendSeparate := duplicateSubscriptionFeedback(&callback.Context{UserID: 42, ChatID: -1001, ChatType: "supergroup"}, 42)
	if sendSeparate {
		t.Fatal("requester acting in a group must not receive a second DM")
	}
	if resp.ShowAlert || !resp.Edit || resp.CallbackMsg != "已有订阅" {
		t.Fatalf("response=%+v", resp)
	}
}

func TestDuplicateSubscriptionFeedbackNotifiesRequesterWhenOtherAdminApprovesInGroup(t *testing.T) {
	resp, sendSeparate := duplicateSubscriptionFeedback(&callback.Context{UserID: 99, ChatID: -1001, ChatType: "group"}, 42)
	if !sendSeparate {
		t.Fatal("another admin acting in a group must notify the requester")
	}
	if resp.ShowAlert || !resp.Edit {
		t.Fatalf("response=%+v", resp)
	}
}

func TestDuplicateSubscriptionFeedbackConservativelyNotifiesWithoutContext(t *testing.T) {
	_, sendSeparate := duplicateSubscriptionFeedback(nil, 42)
	if !sendSeparate {
		t.Fatal("missing callback context must preserve requester notification")
	}
}

func TestNotifyDuplicateSubscriptionRequesterRequiresTelegramClient(t *testing.T) {
	h := &ReviewHandler{}
	if err := h.notifyDuplicateSubscriptionRequester(42, "notice"); err == nil {
		t.Fatal("nil telegram client must return an error")
	}
}

func TestNotifyDuplicateSubscriptionRequesterReturnsSendError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"ok":false,"description":"send failed"}`, http.StatusBadGateway)
	}))
	defer server.Close()

	telegram := services.NewTelegramClient("test")
	telegram.SetBaseURLForTest(server.URL, server.Client())
	h := &ReviewHandler{telegram: telegram}
	if err := h.notifyDuplicateSubscriptionRequester(42, "notice"); err == nil || !strings.Contains(err.Error(), "notify requester") {
		t.Fatalf("err=%v, want wrapped notification error", err)
	}
}

func TestDuplicateSubscriptionNotificationFailureIsVisibleWithoutBlockingAlert(t *testing.T) {
	resp := duplicateSubscriptionNotificationFailure()
	if resp.Text == "" || !strings.Contains(resp.Text, "手动告知") || strings.Contains(resp.Text, "重试") || resp.CallbackMsg != "通知失败" || !resp.Edit || resp.ShowAlert {
		t.Fatalf("response=%+v", resp)
	}
}

func TestDuplicateSubscriptionPersistenceFailureIsRenderableWithoutPublicFallback(t *testing.T) {
	resp := duplicateSubscriptionPersistenceFailure()
	if resp.Text == "" || !strings.Contains(resp.Text, "状态保存失败") || !strings.Contains(resp.Text, "未通知") || resp.CallbackMsg != "保存失败" || !resp.Edit || resp.ShowAlert {
		t.Fatalf("response=%+v", resp)
	}
}

func TestDuplicateSubscriptionNotificationFailureStillPersistsExistingSubscription(t *testing.T) {
	t.Setenv("ADMIN_USER_IDS", "99")
	moviepilot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":77,"name":"Existing","type":"movie","state":"P","media_id":550}]`))
	}))
	defer moviepilot.Close()
	telegram := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"ok":false,"description":"send failed"}`, http.StatusBadGateway)
	}))
	defer telegram.Close()

	reviews := services.NewReviewService(t.TempDir(), false)
	review := &services.ReviewRequest{RequestID: "duplicate-notify-failure", TelegramID: 42, TmdbID: 550, MediaTitle: "Existing", MediaType: services.MediaTypeMovie}
	if err := reviews.CreateRequest(review); err != nil {
		t.Fatal(err)
	}
	tg := services.NewTelegramClient("test")
	tg.SetBaseURLForTest(telegram.URL, telegram.Client())
	h := NewReviewHandler(nil, tg, services.NewMoviePilotClient(moviepilot.URL, "", ""), services.NewAdminService(t.TempDir()), reviews, nil, nil, 0)
	ctx := &callback.Context{UserID: 99, ChatID: 99, Callback: &callback.Callback{Action: "review_approve", Params: map[string]string{"id": review.RequestID, "token": review.ApproveToken}}}
	resp, err := h.Handle(ctx)
	if err != nil || resp == nil || !strings.Contains(resp.Text, "手动告知") || resp.ShowAlert {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
	stored, ok := reviews.GetRequest(review.RequestID)
	if !ok || stored.Status != "approved" || stored.SubscriptionID != 77 || stored.SubscriptionState != "P" {
		t.Fatalf("stored=%+v ok=%v", stored, ok)
	}
}
